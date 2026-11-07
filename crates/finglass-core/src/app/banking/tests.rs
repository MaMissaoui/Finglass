//! Behavioural tests for the bank-login state and the prompt bridge. No network, no OS credential store.

use std::cell::RefCell;
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration as StdDuration, Instant};

use secrecy::{ExposeSecret, SecretString};
use time::macros::date;
use time::{Date, Duration, OffsetDateTime};

use super::*;
use crate::db::Database;
use crate::model::{BankConnectionId, BankConnectionUpdate, NewBankConnection};
use crate::paths::AppPaths;
use crate::repo;
use crate::secret::{Passphrase, PassphrasePurpose};
use crate::sync::credentials::{CredentialStore, MemoryCredentialStore, NoCredentialStore, pin_key};
use crate::sync::{
    AccountSnapshot, BankingInteraction, BankingService, FetchOutcome, FinTsAccount, FinTsBalance, FinTsClient,
    FinTsConnection, FinTsGateway, GatewayError, UnavailableGateway,
};

fn database() -> (tempfile::TempDir, Database) {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("finglass.db");
    let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
    let db = Database::create(&path, &pass).unwrap();
    (temp, db)
}

fn data_dir(temp: &tempfile::TempDir) -> PathBuf {
    let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
    paths.profile_dir("default").unwrap()
}

fn service_with_store(
    gateway: Arc<TestGateway>,
    store: Arc<dyn CredentialStore>,
    dir: &std::path::Path,
    language: Language,
) -> BankingService {
    BankingService::new(FinTsClient::new(gateway, dir.to_path_buf(), "TEST-PRODUCT", store).with_language(language))
}

fn service_for(temp: &tempfile::TempDir, gateway: Arc<TestGateway>) -> (BankingService, Arc<MemoryCredentialStore>) {
    let store = Arc::new(MemoryCredentialStore::new());
    let service = service_with_store(gateway, store.clone(), &data_dir(temp), Language::En);
    (service, store)
}

fn online_bank(db: &Database, name: &str, user_id: &str) -> BankConnectionId {
    let bank = repo::bank_connections::create(
        db,
        NewBankConnection {
            bank_name: name.to_owned(),
            blz: "37040044".to_owned(),
            bic: Some("COLSDE33XXX".to_owned()),
            user_id: user_id.to_owned(),
        },
    )
    .unwrap()
    .id;
    repo::bank_connections::update_connection(
        db,
        bank,
        BankConnectionUpdate {
            server_url: Some("https://fints.example/pintan".to_owned()),
            ..BankConnectionUpdate::default()
        },
    )
    .unwrap();
    bank
}

fn offline_bank(db: &Database, name: &str) -> BankConnectionId {
    repo::bank_connections::create_offline(db, name).unwrap().id
}

fn bridge(timeout: StdDuration) -> Arc<PromptBridge> {
    Arc::new(PromptBridge::new(timeout, || {}))
}

fn wait_until(mut condition: impl FnMut() -> bool) {
    let deadline = Instant::now() + StdDuration::from_secs(2);
    while !condition() {
        assert!(Instant::now() < deadline, "condition did not become true in time");
        thread::sleep(StdDuration::from_millis(2));
    }
}

fn answer_pin_in_background(bridge: Arc<PromptBridge>, pin: &'static str, remember: bool) -> thread::JoinHandle<()> {
    thread::spawn(move || {
        let deadline = Instant::now() + StdDuration::from_secs(2);
        loop {
            if matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })) {
                bridge.answer_pin(Some(SecretString::from(pin)), remember);
                return;
            }
            assert!(Instant::now() < deadline, "no PIN prompt appeared");
            thread::sleep(StdDuration::from_millis(2));
        }
    })
}

fn cancel_at_pin_in_background(bridge: Arc<PromptBridge>) -> thread::JoinHandle<()> {
    thread::spawn(move || {
        let deadline = Instant::now() + StdDuration::from_secs(2);
        loop {
            if matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })) {
                bridge.cancel_pending();
                return;
            }
            assert!(Instant::now() < deadline, "no PIN prompt appeared");
            thread::sleep(StdDuration::from_millis(2));
        }
    })
}

// ---- Test gateway --------------------------------------------------------

#[derive(Default)]
struct TestGateway {
    snapshots: Mutex<Vec<AccountSnapshot>>,
    failure: Mutex<Option<GatewayError>>,
    ask_pin: AtomicBool,
    calls: AtomicUsize,
    names: Mutex<Vec<String>>,
    since_seen: Mutex<HashMap<String, Option<Date>>>,
    cancel_on_call: AtomicUsize,
    busy: Mutex<Option<Busy>>,
}

impl TestGateway {
    fn set_snapshots(&self, snapshots: Vec<AccountSnapshot>) {
        *self.snapshots.lock().unwrap() = snapshots;
    }

    fn set_failure(&self, failure: Option<GatewayError>) {
        *self.failure.lock().unwrap() = failure;
    }

    fn set_ask_pin(&self, ask: bool) {
        self.ask_pin.store(ask, Ordering::SeqCst);
    }

    fn set_cancel_on_call(&self, call: usize) {
        self.cancel_on_call.store(call, Ordering::SeqCst);
    }

    fn set_busy(&self, busy: Busy) {
        *self.busy.lock().unwrap() = Some(busy);
    }

    fn names(&self) -> Vec<String> {
        self.names.lock().unwrap().clone()
    }

    fn since(&self, iban: &str) -> Option<Date> {
        self.since_seen.lock().unwrap().get(iban).copied().flatten()
    }
}

impl FinTsGateway for TestGateway {
    fn fetch_all(
        &self,
        connection: &FinTsConnection,
        interaction: &dyn crate::sync::BankingInteraction,
        since: &dyn Fn(&str) -> Option<Date>,
        _to: Date,
    ) -> Result<FetchOutcome, GatewayError> {
        let call = self.calls.fetch_add(1, Ordering::SeqCst) + 1;
        self.names.lock().unwrap().push(connection.bank_name.clone());

        if let Some(busy) = self.busy.lock().unwrap().clone()
            && !busy.active()
        {
            return Err(GatewayError::Other("profile Busy was not active".to_owned()));
        }

        let snapshots = self.snapshots.lock().unwrap().clone();
        for snapshot in &snapshots {
            self.since_seen
                .lock()
                .unwrap()
                .insert(snapshot.account.iban.clone(), since(&snapshot.account.iban));
        }

        if self.ask_pin.load(Ordering::SeqCst)
            && interaction
                .pin(&connection.bank_name, &connection.user_id, true)
                .is_none()
        {
            return Err(GatewayError::Cancelled);
        }
        if self.cancel_on_call.load(Ordering::SeqCst) == call {
            return Err(GatewayError::Cancelled);
        }
        if let Some(failure) = self.failure.lock().unwrap().clone() {
            return Err(failure);
        }

        Ok(FetchOutcome {
            snapshots,
            tan_method: None,
            capabilities: None,
        })
    }
}

fn account(iban: &str) -> FinTsAccount {
    FinTsAccount {
        iban: iban.to_owned(),
        bic: "COLSDE33XXX".to_owned(),
        account_number: "1234567890".to_owned(),
        sub_account_number: None,
        bank_name: "Sparkasse".to_owned(),
        blz: "37040044".to_owned(),
        account_type: "checking".to_owned(),
        currency: "EUR".to_owned(),
        product_name: None,
        currency_stated: true,
    }
}

fn snapshot(iban: &str, balance: &str) -> AccountSnapshot {
    let bank_account = account(iban);
    AccountSnapshot {
        balance: Some(FinTsBalance {
            account: bank_account.clone(),
            balance: balance.parse().unwrap(),
            available_balance: None,
            currency: "EUR".to_owned(),
            date: OffsetDateTime::now_utc(),
        }),
        account: bank_account,
        transactions: Vec::new(),
        statement_error: None,
        balance_code: None,
        pending: None,
    }
}

// ---- Bridge --------------------------------------------------------------

#[test]
fn cancel_pending_unblocks_a_waiting_pin_prompt() {
    let bridge = bridge(StdDuration::from_secs(30));
    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };

    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
    let started = Instant::now();
    bridge.cancel_pending();
    assert!(bridge.prompt().is_none());
    let answer = receiver
        .recv_timeout(StdDuration::from_secs(1))
        .expect("the worker was unblocked");
    assert!(answer.is_none());
    assert!(started.elapsed() < StdDuration::from_secs(1));
    worker.join().unwrap();
    assert!(bridge.prompt().is_none());
}

#[test]
fn after_cancel_every_new_ask_returns_none_at_once() {
    let bridge = bridge(StdDuration::from_secs(30));
    bridge.cancel_pending();

    assert!(bridge.pin("Bank", "user", true).is_none());
    assert!(bridge.select_tan_method(&[]).is_none());
    assert!(bridge.select_tan_medium(&[]).is_none());
    assert!(
        bridge
            .tan(&crate::sync::TanChallenge {
                message: "Challenge".to_owned(),
                image: None,
                image_mime_type: None,
                flicker_code: None,
            })
            .is_none()
    );
    assert!(!bridge.confirm_decoupled("Confirm"));
}

#[test]
fn an_unanswered_prompt_times_out_as_cancelled() {
    let bridge = bridge(StdDuration::from_millis(40));
    let started = Instant::now();
    assert!(bridge.pin("Bank", "user", true).is_none());
    assert!(started.elapsed() < StdDuration::from_secs(1));
    assert!(bridge.prompt().is_none());
}

#[test]
fn an_answered_pin_arrives_with_remember() {
    let bridge = bridge(StdDuration::from_secs(5));
    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };

    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
    bridge.answer_pin(Some(SecretString::from("12345")), true);
    let entry = receiver
        .recv_timeout(StdDuration::from_secs(1))
        .expect("the worker received an answer")
        .expect("an entry");
    assert_eq!(entry.pin().expose_secret(), "12345");
    assert!(entry.remember());
    worker.join().unwrap();
    assert!(bridge.prompt().is_none());
}

#[test]
fn an_empty_pin_cancels() {
    let bridge = bridge(StdDuration::from_secs(5));
    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };

    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
    bridge.answer_pin(Some(SecretString::from("")), false);
    assert!(
        receiver
            .recv_timeout(StdDuration::from_secs(1))
            .expect("the worker was unblocked")
            .is_none()
    );
    worker.join().unwrap();
}

#[test]
fn a_wrong_kind_answer_is_ignored() {
    let bridge = bridge(StdDuration::from_secs(5));
    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };

    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
    bridge.answer_choice(Some("942".to_owned()));
    bridge.answer_tan(Some(SecretString::from("999999")));
    bridge.answer_decoupled(true);
    assert!(matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));

    bridge.answer_pin(Some(SecretString::from("12345")), false);
    let entry = receiver
        .recv_timeout(StdDuration::from_secs(1))
        .expect("the worker received an answer")
        .expect("an entry");
    assert_eq!(entry.pin().expose_secret(), "12345");
    assert!(!entry.remember());
    worker.join().unwrap();
}

#[test]
fn on_change_fires_when_the_prompt_opens_and_closes() {
    let changes = Arc::new(AtomicUsize::new(0));
    let counter = changes.clone();
    let bridge = Arc::new(PromptBridge::new(StdDuration::from_secs(5), move || {
        counter.fetch_add(1, Ordering::SeqCst);
    }));
    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };

    wait_until(|| changes.load(Ordering::SeqCst) >= 1);
    bridge.answer_pin(Some(SecretString::from("12345")), false);
    let _ = receiver.recv_timeout(StdDuration::from_secs(1)).unwrap();
    worker.join().unwrap();
    assert!(changes.load(Ordering::SeqCst) >= 2);
}

#[test]
fn prompts_and_state_debug_never_contain_a_pin_or_tan() {
    let bridge = bridge(StdDuration::from_secs(5));
    let pin = "PIN-SECRET-123";
    let tan = "TAN-SECRET-456";

    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.pin("Bank", "user", true));
        })
    };
    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Pin { .. })));
    let debug = format!("{:?}", bridge.prompt().unwrap());
    assert!(!debug.contains(pin), "{debug}");
    bridge.answer_pin(Some(SecretString::from(pin)), false);
    let _ = receiver.recv_timeout(StdDuration::from_secs(1)).unwrap();
    worker.join().unwrap();

    let (sender, receiver) = mpsc::channel();
    let worker = {
        let bridge = bridge.clone();
        thread::spawn(move || {
            let _ = sender.send(bridge.tan(&crate::sync::TanChallenge {
                message: "Enter the TAN".to_owned(),
                image: None,
                image_mime_type: None,
                flicker_code: None,
            }));
        })
    };
    wait_until(|| matches!(bridge.prompt(), Some(BankingPrompt::Tan(_))));
    let debug = format!("{:?}", bridge.prompt().unwrap());
    assert!(!debug.contains(tan), "{debug}");
    bridge.answer_tan(Some(SecretString::from(tan)));
    let _ = receiver.recv_timeout(StdDuration::from_secs(1)).unwrap();
    worker.join().unwrap();

    let state = BankingState::new(Language::En);
    let debug = format!("{state:?}");
    assert!(!debug.contains(pin), "{debug}");
    assert!(!debug.contains(tan), "{debug}");
}

// ---- State ---------------------------------------------------------------

#[test]
fn add_bank_access_validates_name_blz_and_an_https_url() {
    let (temp, db) = database();
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(
        state
            .add_bank_access(&db, &service, "  ", "37040044", "", "user", "https://fints.example")
            .is_err()
    );
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::BankNameRequired))
    );

    assert!(
        state
            .add_bank_access(&db, &service, "Bank", "1234", "", "user", "https://fints.example")
            .is_err()
    );
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::BlzMustBe8Digits))
    );

    assert!(
        state
            .add_bank_access(&db, &service, "Bank", "37040044", "", "user", "  ")
            .is_err()
    );
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorServerUrlRequired))
    );

    assert!(
        state
            .add_bank_access(&db, &service, "Bank", "37040044", "", "user", "http://fints.example")
            .is_err()
    );
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorServerUrlMustBeHttps))
    );

    state
        .add_bank_access(
            &db,
            &service,
            "Sparkasse",
            "37040044",
            "colsde33xxx",
            "user",
            "https://fints.example/pintan",
        )
        .unwrap();
    assert_eq!(state.banks.len(), 1);
    assert_eq!(state.banks[0].bank_name, "Sparkasse");
    assert_eq!(
        state.banks[0].server_url.as_deref(),
        Some("https://fints.example/pintan")
    );
    assert!(state.can_remember_pin);
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageAdded, &[Arg::Str("Sparkasse".to_owned())]).as_str())
    );
}

#[test]
fn an_offline_bank_only_renames_and_every_edit_is_refused_while_busy() {
    let (temp, db) = database();
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway);
    let bank = offline_bank(&db, "Old card");
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    // The invalid BLZ and URL are deliberately ignored for an offline bank: only the label changes.
    state
        .update_connection(&db, &service, bank, "New card", "not-a-blz", "x", "http://invalid")
        .unwrap();
    let stored = repo::bank_connections::get_item(&db, bank).unwrap().unwrap();
    assert_eq!(stored.bank_name, "New card");
    assert_eq!(stored.blz, "");
    assert!(stored.server_url.is_none());
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageConnectionSaved))
    );

    state.sync_all = Some(SyncAllProgress {
        position: 0,
        total: 0,
        bank_name: String::new(),
    });
    let error = state
        .update_connection(&db, &service, bank, "Newest card", "", "", "")
        .unwrap_err();
    assert_eq!(error.message, i18n::text(Language::En, Key::ErrorConnectionBusy));
    assert_eq!(
        repo::bank_connections::get_item(&db, bank).unwrap().unwrap().bank_name,
        "New card"
    );

    state.sync_all = None;
    state.syncing_bank = Some(bank);
    assert!(
        state
            .update_connection(&db, &service, bank, "Newest card", "", "", "")
            .is_err()
    );
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorConnectionBusy))
    );
}

#[test]
fn sync_success_sets_the_summary_and_the_profile_busy_is_active_during_the_dialog() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let busy = Busy::default();
    let gateway = Arc::new(TestGateway::default());
    gateway.set_busy(busy.clone());
    let (service, _) = service_for(&temp, gateway.clone());
    let bridge = bridge(StdDuration::from_secs(5));
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(bank));
    assert!(state.is_syncing());
    assert_eq!(
        state.sync_step.as_deref(),
        Some(i18n::text(Language::En, Key::SyncConnecting))
    );
    assert!(!busy.active(), "claiming the state does not start the busy section");

    state.run_sync(&db, &service, &bridge, &busy, bank, date!(2026 - 11 - 07));

    assert!(state.syncing_bank.is_none());
    assert!(state.sync_step.is_none());
    assert!(state.error.is_none());
    let message = state.message.as_deref().expect("a summary");
    assert!(message.contains("Synced"), "{message}");
    assert!(!busy.active(), "the busy section ended with the sync");
    assert_eq!(gateway.names(), vec!["Sparkasse".to_owned()]);
    assert!(state.banks[0].last_sync.is_some());
}

#[test]
fn cancelling_at_the_pin_prompt_clears_the_error() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let gateway = Arc::new(TestGateway::default());
    gateway.set_ask_pin(true);
    let (service, _) = service_for(&temp, gateway.clone());
    let bridge = bridge(StdDuration::from_secs(30));
    let canceller = cancel_at_pin_in_background(bridge.clone());
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(bank));
    state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));
    canceller.join().unwrap();

    assert!(state.error.is_none());
    assert!(state.message.is_none());
    assert!(state.syncing_bank.is_none());
}

#[test]
fn a_failed_sync_keeps_its_error_message() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let gateway = Arc::new(TestGateway::default());
    gateway.set_failure(Some(GatewayError::Other("Dialog broke".to_owned())));
    let (service, _) = service_for(&temp, gateway);
    let bridge = bridge(StdDuration::from_secs(5));
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(bank));
    state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));

    assert_eq!(state.error.as_deref(), Some("Dialog broke"));
    assert!(state.message.is_none());
    assert!(state.syncing_bank.is_none());
}

#[test]
fn begin_sync_refuses_during_a_sync_and_for_an_offline_bank() {
    let (temp, db) = database();
    let online = online_bank(&db, "Sparkasse", "user");
    let offline = offline_bank(&db, "Card");
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(online));
    assert!(!state.begin_sync(online));
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorConnectionBusy))
    );

    state.syncing_bank = None;
    assert!(!state.begin_sync(offline));
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorBankOffline))
    );
}

#[test]
fn sync_all_runs_in_card_order_reports_progress_and_stops_on_cancel() {
    let (temp, db) = database();
    online_bank(&db, "A Bank", "a");
    online_bank(&db, "B Bank", "b");
    online_bank(&db, "C Bank", "c");
    let gateway = Arc::new(TestGateway::default());
    gateway.set_cancel_on_call(2);
    let (service, _) = service_for(&temp, gateway.clone());
    let bridge = bridge(StdDuration::from_secs(5));
    let busy = Busy::default();
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync_all());
    let progress = RefCell::new(Vec::new());
    state.run_sync_all(&db, &service, &bridge, &busy, date!(2026 - 11 - 07), &|state| {
        progress.borrow_mut().push(state.sync_all.clone().expect("progress"))
    });

    assert_eq!(gateway.names(), vec!["A Bank".to_owned(), "B Bank".to_owned()]);
    let progress = progress.into_inner();
    assert_eq!(progress.len(), 2);
    assert_eq!(progress[0].position, 1);
    assert_eq!(progress[0].total, 3);
    assert_eq!(progress[0].bank_name, "A Bank");
    assert_eq!(progress[1].position, 2);
    assert_eq!(progress[1].total, 3);
    assert_eq!(progress[1].bank_name, "B Bank");

    let message = state.message.as_deref().expect("messages");
    assert!(message.contains("A Bank:"), "{message}");
    assert!(
        message.contains(&i18n::format(
            Language::En,
            Key::SyncAllStopped,
            &[Arg::Str("B Bank".to_owned())]
        )),
        "{message}"
    );
    assert!(state.error.is_none());
    assert!(state.sync_all.is_none());
    assert!(!busy.active());
}

#[test]
fn sync_all_with_nothing_included_reports_the_error_and_clears_the_progress() {
    let (temp, db) = database();
    let bank = online_bank(&db, "A Bank", "a");
    repo::bank_connections::set_include_in_sync_all(&db, bank, false).unwrap();
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway.clone());
    let bridge = bridge(StdDuration::from_secs(5));
    let busy = Busy::default();
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync_all());
    state.run_sync_all(&db, &service, &bridge, &busy, date!(2026 - 11 - 07), &|_| {});

    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::SyncAllNothingIncluded))
    );
    assert!(state.message.is_none());
    assert!(state.sync_all.is_none());
    assert!(gateway.names().is_empty());
}

#[test]
fn include_in_sync_all_is_refused_while_a_sync_runs() {
    let (temp, db) = database();
    let bank = online_bank(&db, "A Bank", "a");
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    state.syncing_bank = Some(bank);
    assert!(state.set_include_in_sync_all(&db, &service, bank, false).is_err());
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorConnectionBusy))
    );
    assert!(
        repo::bank_connections::get_item(&db, bank)
            .unwrap()
            .unwrap()
            .include_in_sync_all
    );

    state.syncing_bank = None;
    state.set_include_in_sync_all(&db, &service, bank, false).unwrap();
    assert!(!state.banks[0].include_in_sync_all);
}

#[test]
fn remove_bank_is_refused_while_syncing_and_while_the_profile_is_busy() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let gateway = Arc::new(TestGateway::default());
    let (service, _) = service_for(&temp, gateway);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();
    let busy = Busy::default();

    state.syncing_bank = Some(bank);
    let error = state.remove_bank(&db, &service, &busy, bank).unwrap_err();
    assert_eq!(error.message, i18n::text(Language::En, Key::ErrorConnectionBusy));
    assert!(repo::bank_connections::get_item(&db, bank).unwrap().is_some());

    state.syncing_bank = None;
    busy.begin();
    let error = state.remove_bank(&db, &service, &busy, bank).unwrap_err();
    assert_eq!(error.message, i18n::text(Language::En, Key::ErrorDeleteWhileBusy));
    assert!(repo::bank_connections::get_item(&db, bank).unwrap().is_some());
    busy.end();

    state.remove_bank(&db, &service, &busy, bank).unwrap();
    assert!(repo::bank_connections::get_item(&db, bank).unwrap().is_none());
    assert!(state.banks.is_empty());
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageBankRemoved))
    );
}

#[test]
fn history_from_stores_the_date_audits_it_and_syncs_with_it() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let iban = "DE89370400440532013000";
    let gateway = Arc::new(TestGateway::default());
    gateway.set_snapshots(vec![snapshot(iban, "12.34")]);
    gateway.set_failure(Some(GatewayError::Other("stop".to_owned())));
    let (service, _) = service_for(&temp, gateway.clone());
    let bridge = bridge(StdDuration::from_secs(5));
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();
    let today = date!(2026 - 11 - 07);
    let from = today - Duration::days(120);

    assert!(state.begin_history_from(bank, from, today));
    let busy = Busy::default();
    state.run_history_from(&db, &service, &bridge, &busy, bank, from, today);

    assert_eq!(gateway.since(iban), Some(from));
    assert_eq!(
        repo::bank_connections::get_item(&db, bank).unwrap().unwrap().sync_from,
        Some(from),
        "a failed sync keeps the history start for the next attempt"
    );
    let actions = crate::audit::recent(&db, 1000)
        .unwrap()
        .into_iter()
        .map(|entry| entry.action)
        .collect::<Vec<_>>();
    assert!(actions.iter().any(|action| action == "SET_SYNC_FROM"));
    assert_eq!(state.error.as_deref(), Some("stop"));
    assert_eq!(state.syncing_bank, None);
}

#[test]
fn a_history_sync_is_claimed_before_it_runs() {
    let mut state = BankingState::new(Language::En);
    let today = date!(2026 - 11 - 07);
    state.banks.push(BankItem {
        id: BankConnectionId(7),
        bank_name: "Sparkasse".to_owned(),
        blz: "12345678".to_owned(),
        user_id: "user".to_owned(),
        server_url: Some("https://bank.example".to_owned()),
        offline: false,
        include_in_sync_all: true,
        sync_from: None,
        last_sync: None,
        has_saved_pin: false,
    });

    assert!(!state.begin_history_from(BankConnectionId(7), today + Duration::days(1), today));
    assert_eq!(state.syncing_bank, None);
    assert!(state.begin_history_from(BankConnectionId(7), today - Duration::days(30), today));
    assert_eq!(state.syncing_bank, Some(BankConnectionId(7)));
    assert!(
        !state.begin_history_from(BankConnectionId(7), today - Duration::days(30), today),
        "a second click while the first history sync runs is refused"
    );
}

#[test]
fn a_remembered_pin_is_offered_only_when_a_store_is_available() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let dir = data_dir(&temp);
    let gateway = Arc::new(TestGateway::default());
    gateway.set_ask_pin(true);
    let store = Arc::new(MemoryCredentialStore::new());
    let service = service_with_store(gateway.clone(), store.clone(), &dir, Language::En);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();
    assert!(state.can_remember_pin);

    let bridge = bridge(StdDuration::from_secs(5));
    let answerer = answer_pin_in_background(bridge.clone(), "12345", true);
    assert!(state.begin_sync(bank));
    state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));
    answerer.join().unwrap();

    assert!(service.has_saved_pin(&db, bank));
    assert!(store.contains(&pin_key(&dir, bank.0)));
    assert!(state.banks[0].has_saved_pin);

    let no_store = service_with_store(gateway, Arc::new(NoCredentialStore), &dir, Language::En);
    let mut without = BankingState::new(Language::En);
    without.load(&db, &no_store).unwrap();
    assert!(!without.can_remember_pin);
}

#[test]
fn a_pin_the_store_refuses_is_reported_with_the_pin_not_saved_note() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let gateway = Arc::new(TestGateway::default());
    gateway.set_ask_pin(true);
    let store = Arc::new(MemoryCredentialStore::new().failing_writes());
    let service = service_with_store(gateway.clone(), store, &data_dir(&temp), Language::En);
    let bridge = bridge(StdDuration::from_secs(5));
    let answerer = answer_pin_in_background(bridge.clone(), "12345", true);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(bank));
    state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));
    answerer.join().unwrap();

    assert!(state.error.is_none());
    let message = state.message.as_deref().expect("a summary");
    assert!(
        message.contains(i18n::text(Language::En, Key::PinNotSaved)),
        "{message}"
    );
}

#[test]
fn no_audit_entry_contains_bank_message_text_or_a_pin() {
    let (temp, db) = database();
    let bank = online_bank(&db, "Sparkasse", "user");
    let gateway = Arc::new(TestGateway::default());
    gateway.set_ask_pin(true);
    gateway.set_failure(Some(GatewayError::Other("BANKMESSAGE 1234567890 gesperrt".to_owned())));
    let (service, _) = service_for(&temp, gateway);
    let bridge = bridge(StdDuration::from_secs(5));
    let answerer = answer_pin_in_background(bridge.clone(), "246810", false);
    let mut state = BankingState::new(Language::En);
    state.load(&db, &service).unwrap();

    assert!(state.begin_sync(bank));
    state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));
    answerer.join().unwrap();

    assert!(state.error.as_deref().unwrap().contains("…7890"));
    for entry in crate::audit::recent(&db, 10_000).unwrap() {
        let haystack = format!("{} {} {}", entry.at, entry.action, entry.details);
        for forbidden in ["246810", "1234567890", "BANKMESSAGE", "gesperrt"] {
            assert!(
                !haystack.contains(forbidden),
                "an audit entry leaked {forbidden:?}: {haystack}"
            );
        }
    }
}

#[test]
fn the_unavailable_gateway_fails_with_the_new_text_in_every_language() {
    for &language in Language::ALL {
        let (temp, db) = database();
        let bank = online_bank(&db, "Sparkasse", "user");
        let dir = data_dir(&temp);
        let gateway = Arc::new(UnavailableGateway::new(language));
        let service = BankingService::new(
            FinTsClient::new(gateway, dir, "TEST-PRODUCT", Arc::new(MemoryCredentialStore::new()))
                .with_language(language),
        );
        let bridge = bridge(StdDuration::from_secs(5));
        let mut state = BankingState::new(language);
        state.load(&db, &service).unwrap();

        assert!(state.begin_sync(bank));
        state.run_sync(&db, &service, &bridge, &Busy::default(), bank, date!(2026 - 11 - 07));

        assert_eq!(
            state.error.as_deref(),
            Some(i18n::text(language, Key::ErrorOnlineSyncUnavailable))
        );
    }
}
