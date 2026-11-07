//! Behavioural tests for the read-only sync port. Synthetic data only; no network.

use std::collections::{BTreeMap, HashMap};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};

use secrecy::{ExposeSecret, SecretString};
use time::macros::{date, datetime};
use time::{Date, Duration, OffsetDateTime};

use super::service::*;
use super::*;
use crate::audit;
use crate::db::Database;
use crate::i18n::{self, Arg, Key, Language};
use crate::import::service::import;
use crate::model::{
    AccountId, AccountType, BankConnectionId, BankConnectionUpdate, CategoryId, NewAccount, NewBankConnection,
    NewCategory, NewPendingBooking, NewRule, NewTransaction, RuleDirection, RuleField, Transaction,
};
use crate::money::{Currency, Money};
use crate::paths::AppPaths;
use crate::repo;
use crate::repo::categories;
use crate::secret::{Passphrase, PassphrasePurpose};
use crate::settings;
use crate::sync::capabilities::BankCapabilities;
use crate::sync::client::pin_flag_key;
use crate::sync::config::FinTsConfig;
use crate::sync::credentials::{MemoryCredentialStore, NoCredentialStore, pin_key};
use crate::sync::failures::GatewayError;
use crate::sync::gateway::{FinTsAccount, FinTsBalance, FinTsConnection, FinTsGateway, FinTsTransaction};
use crate::sync::interaction::{BankingInteraction, PinEntry, TanChallenge, TanMethod};

fn database() -> (tempfile::TempDir, Database) {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("finglass.db");
    let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
    let db = Database::create(&path, &pass).unwrap();
    (temp, db)
}

#[derive(Default)]
struct FakeGateway {
    snapshots: Mutex<Vec<AccountSnapshot>>,
    fail_with: Mutex<Option<GatewayError>>,
    pin_calls: AtomicUsize,
    since_requests: Mutex<HashMap<String, Option<Date>>>,
    capabilities: Mutex<Option<BankCapabilities>>,
}

impl FakeGateway {
    fn set_snapshots(&self, snapshots: Vec<AccountSnapshot>) {
        *self.snapshots.lock().unwrap() = snapshots;
    }

    fn set_failure(&self, failure: Option<GatewayError>) {
        *self.fail_with.lock().unwrap() = failure;
    }

    fn set_pin_calls(&self, calls: usize) {
        self.pin_calls.store(calls, Ordering::SeqCst);
    }

    fn set_capabilities(&self, capabilities: Option<BankCapabilities>) {
        *self.capabilities.lock().unwrap() = capabilities;
    }

    fn since(&self, iban: &str) -> Option<Date> {
        self.since_requests.lock().unwrap().get(iban).copied().flatten()
    }
}

impl FinTsGateway for FakeGateway {
    fn fetch_all(
        &self,
        connection: &FinTsConnection,
        interaction: &dyn BankingInteraction,
        since: &dyn Fn(&str) -> Option<Date>,
        _to: Date,
    ) -> Result<FetchOutcome, GatewayError> {
        for _ in 0..self.pin_calls.load(Ordering::SeqCst).max(1) {
            if interaction
                .pin(&connection.bank_name, &connection.user_id, true)
                .is_none()
            {
                return Err(GatewayError::Cancelled);
            }
        }
        if let Some(failure) = self.fail_with.lock().unwrap().clone() {
            return Err(failure);
        }
        let snapshots = self.snapshots.lock().unwrap().clone();
        for snapshot in &snapshots {
            self.since_requests
                .lock()
                .unwrap()
                .insert(snapshot.account.iban.clone(), since(&snapshot.account.iban));
        }
        Ok(FetchOutcome {
            snapshots,
            tan_method: Some("921".to_owned()),
            capabilities: self.capabilities.lock().unwrap().clone(),
        })
    }
}

struct ScriptedInteraction {
    pin_value: Option<String>,
    remember: bool,
    pin_asked: AtomicUsize,
    offered_remember: Mutex<Vec<bool>>,
    pin_bank_names: Mutex<Vec<String>>,
}

impl ScriptedInteraction {
    fn new(pin_value: Option<&str>, remember: bool) -> Self {
        Self {
            pin_value: pin_value.map(str::to_owned),
            remember,
            pin_asked: AtomicUsize::new(0),
            offered_remember: Mutex::new(Vec::new()),
            pin_bank_names: Mutex::new(Vec::new()),
        }
    }

    fn pin_asked(&self) -> usize {
        self.pin_asked.load(Ordering::SeqCst)
    }

    fn offered_remember(&self) -> Vec<bool> {
        self.offered_remember.lock().unwrap().clone()
    }

    fn pin_bank_names(&self) -> Vec<String> {
        self.pin_bank_names.lock().unwrap().clone()
    }
}

impl BankingInteraction for ScriptedInteraction {
    fn pin(&self, bank_name: &str, user_id: &str, offer_remember: bool) -> Option<PinEntry> {
        self.pin_asked.fetch_add(1, Ordering::SeqCst);
        self.offered_remember.lock().unwrap().push(offer_remember);
        self.pin_bank_names.lock().unwrap().push(bank_name.to_owned());
        let _ = user_id;
        self.pin_value
            .as_ref()
            .map(|pin| PinEntry::new(SecretString::from(pin.clone()), self.remember))
    }

    fn select_tan_method(&self, options: &[TanMethod]) -> Option<String> {
        options.first().map(|method| method.id.clone())
    }

    fn select_tan_medium(&self, options: &[String]) -> Option<String> {
        options.last().cloned()
    }

    fn tan(&self, _challenge: &TanChallenge) -> Option<SecretString> {
        Some(SecretString::from("999999"))
    }

    fn confirm_decoupled(&self, _message: &str) -> bool {
        true
    }
}

struct Harness {
    temp: tempfile::TempDir,
    db: Database,
    data_dir: std::path::PathBuf,
    gateway: Arc<FakeGateway>,
    store: Arc<MemoryCredentialStore>,
    service: BankingService,
    bank: BankConnectionId,
    today: Date,
}

impl Harness {
    fn new() -> Self {
        Self::with_today(date!(2026 - 10 - 04))
    }

    fn with_today(today: Date) -> Self {
        let (temp, db) = database();
        let data_dir = temp.path().join("profile");
        let gateway = Arc::new(FakeGateway::default());
        let store = Arc::new(MemoryCredentialStore::new());
        let service = BankingService::new(FinTsClient::new(
            gateway.clone(),
            data_dir.clone(),
            "TEST-PRODUCT",
            store.clone(),
        ));
        let bank = create_bank(&db, "Sparkasse KölnBonn", "37050198", "demo-user", true);
        Self {
            temp,
            db,
            data_dir,
            gateway,
            store,
            service,
            bank,
            today,
        }
    }

    fn sync(&self, interaction: &dyn BankingInteraction) -> BankResult<SyncReport> {
        self.service
            .sync_bank_connection(&self.db, self.bank, interaction, self.today)
    }

    fn accounts(&self) -> Vec<crate::model::Account> {
        repo::accounts::list(&self.db).unwrap()
    }

    fn assert_audit_has_no(&self, forbidden: &[&str]) {
        for entry in audit::recent(&self.db, 10_000).unwrap() {
            let haystack = format!("{} {} {}", entry.at, entry.action, entry.details);
            for value in forbidden {
                assert!(!haystack.contains(value), "audit entry leaked {value:?}: {haystack}");
            }
        }
    }
}

fn create_bank(db: &Database, name: &str, blz: &str, user_id: &str, with_server: bool) -> BankConnectionId {
    let bank = repo::bank_connections::create(
        db,
        NewBankConnection {
            bank_name: name.to_owned(),
            blz: blz.to_owned(),
            bic: Some("COLSDE33XXX".to_owned()),
            user_id: user_id.to_owned(),
        },
    )
    .unwrap()
    .id;
    if with_server {
        repo::bank_connections::update_connection(
            db,
            bank,
            BankConnectionUpdate {
                server_url: Some("https://fints.example/pintan".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap();
    }
    bank
}

fn set_last_sync(db: &Database, bank: BankConnectionId, at: OffsetDateTime) {
    db.with_conn(|conn| {
        conn.execute(
            "UPDATE bank_connections SET last_sync = ?1 WHERE id = ?2",
            (
                at.format(&time::format_description::well_known::Rfc3339).unwrap(),
                bank.0,
            ),
        )?;
        Ok(())
    })
    .unwrap();
}

fn account(iban: &str) -> FinTsAccount {
    FinTsAccount {
        iban: iban.to_owned(),
        bic: "COLSDE33XXX".to_owned(),
        account_number: iban.chars().rev().take(10).collect::<String>().chars().rev().collect(),
        sub_account_number: None,
        bank_name: "Sparkasse".to_owned(),
        blz: "37050198".to_owned(),
        account_type: "checking".to_owned(),
        currency: "EUR".to_owned(),
        product_name: None,
        currency_stated: true,
    }
}

fn booking(
    iban: &str,
    day: Date,
    amount: &str,
    partner: Option<&str>,
    purpose: Option<&str>,
    balance_after: Option<&str>,
) -> FinTsTransaction {
    FinTsTransaction {
        account: account(iban),
        date: day,
        value_date: day,
        amount: amount.parse().unwrap(),
        currency: "EUR".to_owned(),
        partner_name: partner.map(str::to_owned),
        partner_iban: None,
        partner_bic: None,
        purpose: purpose.map(str::to_owned),
        transaction_code: None,
        reference: None,
        prima_nota: None,
        balance_after: balance_after.map(|value| value.parse().unwrap()),
        bank_id: None,
    }
}

fn snapshot(iban: &str, balance: &str, transactions: Vec<FinTsTransaction>) -> AccountSnapshot {
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
        transactions,
        statement_error: None,
        balance_code: None,
        pending: None,
    }
}

fn category(db: &Database, name: &str) -> CategoryId {
    categories::create(
        db,
        NewCategory {
            name: name.to_owned(),
            color: "#3BC7B0".to_owned(),
            budget_limit: None,
            is_tax_relevant: false,
            is_transfer: false,
        },
    )
    .unwrap()
}

fn rule(db: &Database, pattern: &str, category_id: CategoryId) {
    repo::rules::create(
        db,
        NewRule {
            pattern: pattern.to_owned(),
            category_id,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
}

fn transactions(db: &Database, account_id: AccountId) -> Vec<Transaction> {
    repo::transactions::list_for_account(db, account_id, None, None).unwrap()
}

fn manual_account(db: &Database, iban: &str) -> AccountId {
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Account".to_owned()),
            iban: Some(iban.to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Checking,
            balance: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

// ---- BankingSyncTest -----------------------------------------------------

#[test]
fn first_sync_creates_accounts_sets_balances_imports_and_categorizes() {
    let harness = Harness::new();
    let food = category(&harness.db, "Groceries");
    rule(&harness.db, "rewe", food);
    let giro = "DE89370400440532013000";
    let savings = "DE44500105175407324931";
    harness.gateway.set_snapshots(vec![
        snapshot(
            giro,
            "1234.56",
            vec![
                booking(
                    giro,
                    harness.today - Duration::days(3),
                    "-4.50",
                    Some("REWE"),
                    Some("Kaffee"),
                    Some("1239.06"),
                ),
                booking(
                    giro,
                    harness.today - Duration::days(3),
                    "-4.50",
                    Some("REWE"),
                    Some("Kaffee"),
                    Some("1234.56"),
                ),
            ],
        ),
        snapshot(savings, "10000.00", Vec::new()),
    ]);
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    let report = harness.sync(&interaction).unwrap();

    assert_eq!(report.accounts_synced, 2);
    assert_eq!(report.accounts_added, 2);
    assert_eq!(report.imported, 2);
    assert_eq!(report.categorized, 2);
    assert_eq!(interaction.pin_asked(), 1, "one dialog, one PIN prompt");
    assert_eq!(interaction.offered_remember(), vec![true]);
    assert_eq!(harness.gateway.since(giro), Some(harness.today - Duration::days(90)));

    let accounts = harness.accounts();
    let giro_account = accounts
        .iter()
        .find(|account| account.iban.as_deref() == Some(giro))
        .unwrap();
    assert_eq!(giro_account.balance.minor, 123_456);
    assert_eq!(
        transactions(&harness.db, giro_account.id)
            .iter()
            .filter(|transaction| transaction.category_id == Some(food))
            .count(),
        2
    );
    let bank = repo::bank_connections::get_item(&harness.db, harness.bank)
        .unwrap()
        .unwrap();
    assert!(bank.last_sync.is_some());
    assert_eq!(bank.tan_method.as_deref(), Some("921"));
    harness.assert_audit_has_no(&["12345", "999999", "demo-user", giro]);
}

#[test]
fn the_first_import_into_an_empty_account_marks_no_booking_new() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "980.00",
        vec![booking(
            giro,
            harness.today - Duration::days(3),
            "-20.00",
            Some("Shop"),
            Some("A"),
            Some("980.00"),
        )],
    )]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let account = harness.accounts().into_iter().next().unwrap();
    assert!(
        transactions(&harness.db, account.id)
            .iter()
            .all(|transaction| !transaction.is_new)
    );
    assert!(repo::transactions::count_new(&harness.db).unwrap().is_empty());
}

#[test]
fn a_second_import_marks_exactly_the_new_bookings_and_never_re_flags_duplicates() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let existing = booking(
        giro,
        harness.today - Duration::days(3),
        "-20.00",
        Some("Shop"),
        Some("A"),
        Some("980.00"),
    );
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "980.00", vec![existing.clone()])]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "945.00",
        vec![
            existing,
            booking(
                giro,
                harness.today - Duration::days(1),
                "-5.00",
                Some("Shop"),
                Some("B"),
                Some("975.00"),
            ),
            booking(giro, harness.today, "-30.00", Some("Shop"), Some("C"), Some("945.00")),
        ],
    )]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.imported, 2);
    assert_eq!(report.duplicates, 1);
    let account_id = harness.accounts().into_iter().next().unwrap().id;
    assert_eq!(
        repo::transactions::count_new(&harness.db).unwrap(),
        BTreeMap::from([(account_id, 2)])
    );
    let purposes = transactions(&harness.db, account_id)
        .into_iter()
        .filter(|transaction| transaction.is_new)
        .filter_map(|transaction| transaction.purpose)
        .collect::<Vec<_>>();
    assert_eq!(purposes.len(), 2);
    assert!(purposes.contains(&"B".to_owned()));
    assert!(purposes.contains(&"C".to_owned()));
}

#[test]
fn same_day_bookings_keep_their_running_balances_in_order() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "40.00",
        vec![
            booking(giro, harness.today, "-10.00", Some("Shop"), Some("A"), Some("90.00")),
            booking(giro, harness.today, "-20.00", Some("Shop"), Some("B"), Some("70.00")),
            booking(giro, harness.today, "-30.00", Some("Shop"), Some("C"), Some("40.00")),
        ],
    )]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let account_id = harness.accounts().into_iter().next().unwrap().id;
    let balances = transactions(&harness.db, account_id)
        .into_iter()
        .map(|transaction| transaction.balance_after.unwrap().minor)
        .collect::<Vec<_>>();
    assert_eq!(balances, vec![4_000, 7_000, 9_000]);
}

#[test]
fn sync_stores_the_logins_supported_transactions() {
    let harness = Harness::new();
    harness.gateway.set_capabilities(Some(BankCapabilities {
        jobs: [BankCapabilities::JOB_BALANCE, BankCapabilities::JOB_STATEMENTS_MT940]
            .into_iter()
            .map(str::to_owned)
            .collect(),
    }));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = BankCapabilities::parse(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .capabilities
            .as_deref(),
    )
    .unwrap();
    assert!(stored.supports_balance());
    assert!(stored.supports_statements());
}

#[test]
fn second_sync_overlaps_the_last_one_and_skips_known_bookings() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let first = booking(
        giro,
        harness.today - Duration::days(3),
        "-20.00",
        Some("Shop"),
        Some("A"),
        Some("980.00"),
    );
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "980.00", vec![first.clone()])]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 09 - 29 12:00 UTC));
    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "950.00",
        vec![
            first,
            booking(giro, harness.today, "-30.00", Some("Shop"), Some("B"), Some("950.00")),
        ],
    )]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(harness.gateway.since(giro), Some(date!(2026 - 09 - 19)));
    assert_eq!(report.imported, 1);
    assert_eq!(report.duplicates, 1);
    assert_eq!(report.accounts_added, 0);
    assert_eq!(harness.accounts().into_iter().next().unwrap().balance.minor, 95_000);
}

#[test]
fn failed_statements_keep_the_old_sync_date_so_the_window_is_fetched_again() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    let previous = datetime!(2026 - 09 - 01 08:00 UTC);
    set_last_sync(&harness.db, harness.bank, previous);

    let mut failed = snapshot(giro, "100.00", Vec::new());
    failed.statement_error = Some("9010 Auftrag abgelehnt".to_owned());
    harness.gateway.set_snapshots(vec![failed]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.warnings, vec!["3000: 9010 Auftrag abgelehnt"]);
    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .last_sync,
        Some(previous)
    );
}

#[test]
fn a_chosen_history_start_reaches_known_and_new_accounts_and_is_cleared_after_success() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let savings = "DE44500105175407324931";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let from = harness.today - Duration::days(400);
    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(from)).unwrap();
    harness.gateway.set_snapshots(vec![
        snapshot(
            giro,
            "95.00",
            vec![booking(
                giro,
                from + Duration::days(1),
                "-5.00",
                Some("Old Shop"),
                Some("A"),
                Some("95.00"),
            )],
        ),
        snapshot(
            savings,
            "200.00",
            vec![booking(
                savings,
                from + Duration::days(2),
                "-2.00",
                Some("Old Shop"),
                Some("B"),
                Some("198.00"),
            )],
        ),
    ]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(harness.gateway.since(giro), Some(from));
    assert_eq!(harness.gateway.since(savings), Some(from));
    assert_eq!(report.history_from, Some(from));
    assert_eq!(report.imported, 2);
    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .sync_from,
        None
    );
}

#[test]
fn a_chosen_history_start_after_the_last_sync_never_narrows_the_window() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 09 - 04 12:00 UTC));
    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(harness.today - Duration::days(1))).unwrap();
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(harness.gateway.since(giro), Some(date!(2026 - 08 - 25)));
}

#[test]
fn re_fetching_bookings_that_were_imported_before_imports_none_of_them() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let existing = booking(
        giro,
        harness.today - Duration::days(3),
        "-20.00",
        Some("Shop"),
        Some("A"),
        Some("980.00"),
    );
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "980.00", vec![existing.clone()])]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(harness.today - Duration::days(400)))
        .unwrap();
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "980.00", vec![existing])]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.imported, 0);
    assert_eq!(report.duplicates, 1);
}

#[test]
fn a_warning_an_error_or_a_cancel_keeps_the_chosen_history_start_for_a_retry() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let from = harness.today - Duration::days(400);
    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(from)).unwrap();

    let mut with_warning = snapshot(giro, "100.00", Vec::new());
    with_warning.statement_error = Some("9010 Auftrag abgelehnt".to_owned());
    harness.gateway.set_snapshots(vec![with_warning]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .sync_from,
        Some(from),
        "a statement warning keeps it"
    );

    harness.gateway.set_snapshots(Vec::new());
    harness
        .gateway
        .set_failure(Some(GatewayError::Other("9931 PIN gesperrt".to_owned())));
    harness
        .sync(&ScriptedInteraction::new(Some("12345"), false))
        .unwrap_err();
    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .sync_from,
        Some(from),
        "an error keeps it"
    );

    harness.gateway.set_failure(None);
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(None, false)).unwrap_err();
    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .sync_from,
        Some(from),
        "a cancel keeps it"
    );
}

#[test]
fn cancelled_pin_and_bank_errors_are_reported_and_nothing_is_imported() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "1.00",
        vec![booking(giro, harness.today, "-1.00", Some("X"), Some("Y"), None)],
    )]);

    let cancelled = harness.sync(&ScriptedInteraction::new(None, false)).unwrap_err();
    assert_eq!(cancelled.code, "CANCELLED");

    harness
        .gateway
        .set_failure(Some(GatewayError::Other("9931 PIN gesperrt".to_owned())));
    let failed = harness
        .sync(&ScriptedInteraction::new(Some("12345"), false))
        .unwrap_err();
    assert_eq!(failed.code, "FINTS_ERROR");
    assert_eq!(failed.message, "9931 PIN gesperrt");
    assert!(harness.accounts().is_empty());
    assert!(
        repo::transactions::find_items(&harness.db, &Default::default(), None)
            .unwrap()
            .is_empty()
    );
}

#[test]
fn sync_needs_a_user_id_and_a_server_url() {
    let harness = Harness::new();
    let no_user = create_bank(&harness.db, "Sparkasse", "37050198", "", true);
    let error = harness
        .service
        .sync_bank_connection(
            &harness.db,
            no_user,
            &ScriptedInteraction::new(Some("1"), false),
            harness.today,
        )
        .unwrap_err();
    assert_eq!(error.code, "NO_USER");

    let no_server = create_bank(&harness.db, "Tiny Bank", "99999999", "u", false);
    let error = harness
        .service
        .sync_bank_connection(
            &harness.db,
            no_server,
            &ScriptedInteraction::new(Some("1"), false),
            harness.today,
        )
        .unwrap_err();
    assert_eq!(error.code, "NO_SERVER");

    repo::bank_connections::update_connection(
        &harness.db,
        no_server,
        BankConnectionUpdate {
            server_url: Some("https://fints.tiny.example/pintan".to_owned()),
            ..BankConnectionUpdate::default()
        },
    )
    .unwrap();
    harness.gateway.set_snapshots(Vec::new());
    assert!(
        harness
            .service
            .sync_bank_connection(
                &harness.db,
                no_server,
                &ScriptedInteraction::new(Some("1"), false),
                harness.today,
            )
            .is_ok()
    );
}

#[test]
fn a_renamed_bank_connection_still_syncs_and_prompts_with_the_new_name() {
    let harness = Harness::new();
    repo::bank_connections::update(&harness.db, harness.bank, Some("DKB Business".to_owned()), None).unwrap();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "500.00",
        vec![booking(
            giro,
            harness.today,
            "-30.00",
            Some("Shop"),
            Some("B"),
            Some("470.00"),
        )],
    )]);
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    let report = harness.sync(&interaction).unwrap();

    assert_eq!(report.accounts_added, 1);
    assert_eq!(report.imported, 1);
    assert_eq!(interaction.pin_bank_names(), vec!["DKB Business".to_owned()]);
}

#[test]
fn a_loan_without_a_balance_is_created_as_a_loan_and_noted_without_leaking_the_iban() {
    let harness = Harness::new();
    let loan_iban = "DE89370400440532013040";
    let giro = "DE89370400440532013000";
    let mut loan_account = account(loan_iban);
    loan_account.account_type = "loan".to_owned();
    harness.gateway.set_snapshots(vec![
        AccountSnapshot {
            account: loan_account,
            balance: None,
            transactions: Vec::new(),
            statement_error: None,
            balance_code: Some("3016".to_owned()),
            pending: None,
        },
        snapshot(giro, "1234.56", Vec::new()),
    ]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.accounts_added, 2);
    let accounts = harness.accounts();
    assert_eq!(
        accounts
            .iter()
            .find(|a| a.iban.as_deref() == Some(loan_iban))
            .unwrap()
            .account_type,
        AccountType::Loan
    );
    assert_eq!(report.notes.len(), 1);
    let note = &report.notes[0];
    assert!(note.contains("3040"), "{note}");
    assert!(note.contains("3016"), "{note}");
    assert!(!note.contains(loan_iban));
    assert!(report.warnings.is_empty());
    assert!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .last_sync
            .is_some()
    );
    let audit = audit::recent(&harness.db, 100).unwrap();
    assert!(
        audit.iter().all(|entry| !entry.details.contains(loan_iban)),
        "the full IBAN must not be audited"
    );

    let loan_id = accounts
        .iter()
        .find(|a| a.iban.as_deref() == Some(loan_iban))
        .unwrap()
        .id;
    repo::accounts::set_balance(
        &harness.db,
        loan_id,
        Money {
            minor: -1_200_000,
            currency: Currency::EUR,
        },
        OffsetDateTime::now_utc(),
    )
    .unwrap();
    let second = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    assert!(second.notes.is_empty());
}

#[test]
fn an_old_last_sync_notes_the_skipped_window_once_and_still_advances() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    let old_sync = datetime!(2026 - 01 - 01 12:00 UTC);
    set_last_sync(&harness.db, harness.bank, old_sync);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncGapNotFetched,
            &[
                Arg::Str("22.12.2025".to_owned()),
                Arg::Str("14.01.2026".to_owned()),
                Arg::from(INITIAL_SYNC_DAYS),
            ],
        )]
    );
    assert_eq!(harness.gateway.since(giro), Some(date!(2026 - 01 - 15)));
    assert!(report.warnings.is_empty());
    assert!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .last_sync
            .unwrap()
            > old_sync
    );
}

#[test]
fn a_last_sync_85_days_back_notes_the_window_with_the_default_end() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 01 - 20 12:00 UTC));

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncGapNotFetched,
            &[
                Arg::Str("10.01.2026".to_owned()),
                Arg::Str("14.01.2026".to_owned()),
                Arg::from(INITIAL_SYNC_DAYS),
            ],
        )]
    );
}

#[test]
fn a_history_start_inside_the_gap_ends_the_note_the_day_before_it() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 01 - 01 12:00 UTC));
    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(date!(2026 - 01 - 05))).unwrap();

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncGapNotFetched,
            &[
                Arg::Str("22.12.2025".to_owned()),
                Arg::Str("04.01.2026".to_owned()),
                Arg::from(INITIAL_SYNC_DAYS),
            ],
        )]
    );
}

#[test]
fn a_last_sync_inside_the_initial_window_does_not_note_a_gap() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 03 - 16 12:00 UTC));

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(report.notes.is_empty());
}

#[test]
fn a_chosen_history_start_before_the_gap_suppresses_the_gap_note() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 01 - 01 12:00 UTC));
    repo::bank_connections::set_sync_from(&harness.db, harness.bank, Some(date!(2025 - 12 - 01))).unwrap();

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(report.notes.is_empty());
    assert_eq!(harness.gateway.since(giro), Some(date!(2025 - 12 - 01)));
}

#[test]
fn an_old_last_sync_with_only_a_new_account_does_not_note_a_gap() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let savings = "DE44500105175407324931";
    set_last_sync(&harness.db, harness.bank, datetime!(2026 - 01 - 01 12:00 UTC));

    harness
        .gateway
        .set_snapshots(vec![snapshot(savings, "100.00", Vec::new())]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(report.notes.is_empty());
    assert_eq!(report.accounts_added, 1);
}

#[test]
fn a_first_sync_does_not_note_a_gap() {
    let harness = Harness::with_today(date!(2026 - 04 - 15));
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(report.notes.is_empty());
}

#[test]
fn an_account_without_an_iban_is_noted_without_exposing_its_number() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let number = "4111111111111111";
    let mut without_iban = account("");
    without_iban.account_number = number.to_owned();
    harness.gateway.set_snapshots(vec![
        snapshot(giro, "100.00", Vec::new()),
        AccountSnapshot {
            account: without_iban,
            balance: Some(FinTsBalance {
                account: account(""),
                balance: "5.00".parse().unwrap(),
                available_balance: None,
                currency: "EUR".to_owned(),
                date: OffsetDateTime::now_utc(),
            }),
            transactions: Vec::new(),
            statement_error: None,
            balance_code: None,
            pending: None,
        },
    ]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.accounts_synced, 1);
    assert_eq!(report.accounts_added, 1);
    assert_eq!(harness.accounts().len(), 1);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncAccountWithoutIban,
            &[Arg::Str("…1111".to_owned())],
        )]
    );
    assert!(!report.notes.join("\n").contains(number));
    assert!(!report.summary().contains(number));
    assert!(
        audit::recent(&harness.db, 100)
            .unwrap()
            .iter()
            .all(|entry| !entry.details.contains(number))
    );
}

#[test]
fn an_account_without_an_iban_or_number_is_noted_by_its_product_name() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let mut without_iban = account("");
    without_iban.account_number.clear();
    without_iban.product_name = Some("Girokonto".to_owned());
    harness.gateway.set_snapshots(vec![
        snapshot(giro, "100.00", Vec::new()),
        AccountSnapshot {
            account: without_iban,
            balance: None,
            transactions: Vec::new(),
            statement_error: None,
            balance_code: None,
            pending: None,
        },
    ]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(report.notes[0].starts_with("Girokonto:"), "{}", report.notes[0]);
}

fn foreign_snapshot(giro: &str, balance_currency: &str) -> AccountSnapshot {
    let mut bank_account = account(giro);
    bank_account.currency = "CHF".to_owned();
    bank_account.currency_stated = false;
    AccountSnapshot {
        account: bank_account.clone(),
        balance: Some(FinTsBalance {
            account: bank_account,
            balance: "88.50".parse().unwrap(),
            available_balance: None,
            currency: balance_currency.to_owned(),
            date: OffsetDateTime::now_utc(),
        }),
        transactions: Vec::new(),
        statement_error: None,
        balance_code: None,
        pending: None,
    }
}

#[test]
fn a_balance_in_another_currency_is_not_stored_and_is_noted() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(giro, "50.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let mut foreign = foreign_snapshot(giro, "USD");
    foreign.account.currency = "EUR".to_owned();
    foreign.account.currency_stated = true;
    if let Some(balance) = &mut foreign.balance {
        balance.account.currency = "EUR".to_owned();
    }
    harness.gateway.set_snapshots(vec![foreign]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(harness.accounts().into_iter().next().unwrap().balance.minor, 5_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("USD".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
}

#[test]
fn a_balance_in_the_account_currency_is_stored_without_a_note() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness.gateway.set_snapshots(vec![snapshot(giro, "50.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(harness.accounts().into_iter().next().unwrap().balance.minor, 10_000);
    assert!(report.notes.is_empty());
}

#[test]
fn a_new_account_with_a_balance_in_another_currency_starts_at_zero_and_is_noted() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let mut foreign = foreign_snapshot(giro, "USD");
    foreign.account.currency = "EUR".to_owned();
    foreign.account.currency_stated = true;
    if let Some(balance) = &mut foreign.balance {
        balance.account.currency = "EUR".to_owned();
    }
    harness.gateway.set_snapshots(vec![foreign]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::EUR);
    assert_eq!(stored.balance.minor, 0);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("USD".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
}

#[test]
fn a_sync_updates_the_balance_and_imports_bookings_but_keeps_the_account_name() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    let account_id = harness.accounts().into_iter().next().unwrap().id;
    repo::accounts::rename(&harness.db, account_id, Some("Haushalt".to_owned())).unwrap();

    harness.gateway.set_snapshots(vec![snapshot(
        giro,
        "120.00",
        vec![booking(
            giro,
            harness.today,
            "-5.00",
            Some("Shop"),
            Some("A"),
            Some("120.00"),
        )],
    )]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(report.imported, 1);
    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.name.as_deref(), Some("Haushalt"));
    assert_eq!(stored.balance.minor, 12_000);
}

// ---- Pending bookings ----------------------------------------------------

#[test]
fn a_sync_stores_pending_lines_apart_from_the_bookings_and_clears_them_once_booked() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let employer = "Beispiel AG";
    let tank = "Beispiel Tank GmbH";
    let booked = booking(
        giro,
        harness.today - Duration::days(3),
        "-120.00",
        Some(employer),
        Some("Gehalt"),
        None,
    );
    let pending_line = booking(giro, harness.today, "-49.80", Some(tank), Some("Kraftstoff"), None);
    let mut first = snapshot(giro, "1000.00", vec![booked.clone()]);
    first.pending = Some(vec![pending_line.clone()]);
    harness.gateway.set_snapshots(vec![first]);

    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let account_id = harness.accounts().into_iter().next().unwrap().id;
    let items = transactions(&harness.db, account_id);
    assert_eq!(items.len(), 1, "only the booked line is a booking");
    assert_eq!(items[0].amount.minor, -12_000);
    let pending = repo::pending_bookings::find_items(&harness.db, account_id).unwrap();
    assert_eq!(pending.len(), 1);
    assert_eq!(pending[0].amount.minor, -4_980);
    assert_eq!(pending[0].partner_name.as_deref(), Some(tank));

    let mut second = snapshot(giro, "950.20", vec![booked, pending_line]);
    second.pending = Some(Vec::new());
    harness.gateway.set_snapshots(vec![second]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let items = transactions(&harness.db, account_id);
    assert_eq!(items.len(), 2);
    assert_eq!(
        items
            .iter()
            .filter(|item| item.amount.minor == -4_980 && item.partner_name.as_deref() == Some(tank))
            .count(),
        1
    );
    assert!(
        repo::pending_bookings::find_items(&harness.db, account_id)
            .unwrap()
            .is_empty()
    );

    let kept = NewPendingBooking {
        date: harness.today + Duration::days(2),
        value_date: Some(harness.today + Duration::days(2)),
        amount: Money {
            minor: -1_000,
            currency: Currency::EUR,
        },
        partner_name: Some(tank.to_owned()),
        purpose: Some("Kraftstoff".to_owned()),
    };
    repo::pending_bookings::replace_for_account(&harness.db, account_id, vec![kept], OffsetDateTime::now_utc())
        .unwrap();
    let mut third = snapshot(giro, "950.20", vec![]);
    third.pending = None;
    harness.gateway.set_snapshots(vec![third]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();
    let stored = repo::pending_bookings::find_items(&harness.db, account_id).unwrap();
    assert_eq!(stored.len(), 1);
    assert_eq!(stored[0].amount.minor, -1_000);
}

// ---- CrossSourceDedupTest -----------------------------------------------

fn csv_statement(text: &str, today: Date) -> crate::import::ParsedStatement {
    crate::import::read("statement.csv", text.as_bytes(), None, today).unwrap()
}

fn fints_line(iban: &str, day: Date, amount: &str, partner: &str, purpose: &str, balance: &str) -> FinTsTransaction {
    booking(iban, day, amount, Some(partner), Some(purpose), Some(balance))
}

#[test]
fn csv_back_fill_then_sync_of_the_same_bookings_adds_nothing() {
    let harness = Harness::new();
    let iban = "DE89370400440532013000";
    let account = manual_account(&harness.db, iban);
    let day = date!(2026 - 10 - 03);
    let csv = csv_statement(
        "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n\
         03.10.2026;-45,67;REWE;Kartenzahlung\n\
         03.10.2026;-4,50;Bäcker;Kaffee\n",
        day,
    );
    import(&harness.db, account, &csv, false, day).unwrap();

    let result = harness
        .service
        .import_transactions(
            &harness.db,
            account,
            &[
                fints_line(iban, day, "-45.67", "REWE Markt GmbH", "REWE SAGT DANKE 1234", "954.33"),
                fints_line(iban, day, "-4.50", "BAECKEREI SCHMIDT", "KARTENZAHLUNG 03.10", "949.83"),
            ],
        )
        .unwrap();

    assert_eq!(result.imported, 0);
    assert_eq!(transactions(&harness.db, account).len(), 2);
}

#[test]
fn genuine_extra_repeats_and_different_amounts_are_still_imported() {
    let harness = Harness::new();
    let iban = "DE89370400440532013000";
    let account = manual_account(&harness.db, iban);
    let day = date!(2026 - 10 - 03);
    let csv = csv_statement(
        "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n\
         03.10.2026;-4,50;Bäcker;Kaffee\n\
         03.10.2026;-4,50;Bäcker;Kaffee\n",
        day,
    );
    import(&harness.db, account, &csv, false, day).unwrap();

    let result = harness
        .service
        .import_transactions(
            &harness.db,
            account,
            &[
                fints_line(iban, day, "-4.50", "BAECKEREI", "Kaffee", "100.00"),
                fints_line(iban, day, "-4.50", "BAECKEREI", "Kaffee", "95.50"),
                fints_line(iban, day, "-4.50", "BAECKEREI", "Kaffee", "91.00"),
                fints_line(iban, day, "-4.70", "BAECKEREI", "Kaffee gross", "86.30"),
            ],
        )
        .unwrap();

    assert_eq!(result.imported, 2, "one extra coffee + the different amount");
    assert_eq!(transactions(&harness.db, account).len(), 4);
}

// ---- SavedPinTest (sync half) -------------------------------------------

fn store_pin(harness: &Harness) -> String {
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), true)).unwrap();
    let key = pin_key(&harness.data_dir, harness.bank.0);
    assert_eq!(
        harness.store.read(&key).unwrap().unwrap().expose_secret(),
        "12345",
        "precondition: the PIN was kept"
    );
    key
}

#[test]
fn remembering_the_pin_stores_it_and_audits_it_without_the_secret() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), true)).unwrap();

    let key = pin_key(&harness.data_dir, harness.bank.0);
    assert_eq!(harness.store.read(&key).unwrap().unwrap().expose_secret(), "12345");
    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));
    assert_eq!(
        settings::get_setting(&harness.db, &pin_flag_key(harness.bank))
            .unwrap()
            .as_deref(),
        Some("37050198|demo-user")
    );
    assert_eq!(
        audit::recent(&harness.db, 100)
            .unwrap()
            .iter()
            .filter(|entry| entry.action == "PIN_SAVED")
            .count(),
        1
    );
    assert!(report.warnings.is_empty());
    assert!(!harness.service.pin_not_saved());

    let haystack = format!(
        "{}\n{}\n{:?}",
        report.summary(),
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .map(|entry| format!("{} {}", entry.action, entry.details))
            .collect::<Vec<_>>()
            .join("\n"),
        PinEntry::new(SecretString::from("12345"), true),
    );
    assert!(!haystack.contains("12345"), "no PIN in the report, audit log or Debug");
    assert!(!haystack.contains("demo-user"));
    assert!(!haystack.contains(giro));
}

#[test]
fn a_stored_pin_answers_the_next_sync_without_asking() {
    let harness = Harness::new();
    store_pin(&harness);
    let second = ScriptedInteraction::new(Some("99999"), false);

    harness.sync(&second).unwrap();

    assert_eq!(second.pin_asked(), 0, "the stored PIN answers the dialog");
    assert_eq!(
        harness
            .store
            .read(&pin_key(&harness.data_dir, harness.bank.0))
            .unwrap()
            .unwrap()
            .expose_secret(),
        "12345"
    );
}

#[test]
fn without_the_opt_in_nothing_is_stored() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_rejected_stored_pin_is_deleted_and_the_user_is_asked_again() {
    let harness = Harness::new();
    store_pin(&harness);
    harness.gateway.set_pin_calls(2);
    let second = ScriptedInteraction::new(Some("54321"), false);

    let report = harness.sync(&second).unwrap();

    assert_eq!(second.pin_asked(), 1, "only the re-prompt reaches the user");
    assert!(harness.store.is_empty(), "the rejected PIN is gone");
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(report.warnings.is_empty());
    assert_eq!(
        audit::recent(&harness.db, 100)
            .unwrap()
            .iter()
            .filter(|entry| entry.action == "PIN_FORGOTTEN")
            .count(),
        1,
        "forgotten once, not per prompt"
    );
}

#[test]
fn a_new_pin_is_saved_only_after_the_dialog_succeeds() {
    let harness = Harness::new();
    store_pin(&harness);
    harness.gateway.set_pin_calls(2);
    let retry = ScriptedInteraction::new(Some("54321"), true);

    harness.sync(&retry).unwrap();

    let key = pin_key(&harness.data_dir, harness.bank.0);
    assert_eq!(harness.store.read(&key).unwrap().unwrap().expose_secret(), "54321");
    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));

    harness.gateway.set_pin_calls(1);
    let next = ScriptedInteraction::new(Some("99999"), false);
    harness.sync(&next).unwrap();
    assert_eq!(next.pin_asked(), 0);
}

#[test]
fn cancelling_the_re_prompt_after_a_rejected_stored_pin_forgets_it() {
    let harness = Harness::new();
    store_pin(&harness);
    harness.gateway.set_pin_calls(2);

    let result = harness.sync(&ScriptedInteraction::new(None, false));

    assert_eq!(result.unwrap_err().code, "CANCELLED");
    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_dialog_failure_after_a_stored_pin_was_used_forgets_it() {
    let harness = Harness::new();
    store_pin(&harness);
    harness
        .gateway
        .set_failure(Some(GatewayError::Other("connection lost".to_owned())));

    let result = harness.sync(&ScriptedInteraction::new(Some("12345"), false));

    assert_eq!(result.unwrap_err().code, "FINTS_ERROR");
    assert!(harness.store.is_empty(), "a re-prompt is cheap, a locked login is not");
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_cancel_while_the_stored_pin_was_fine_keeps_it() {
    let harness = Harness::new();
    store_pin(&harness);
    harness.gateway.set_failure(Some(GatewayError::Cancelled));

    let result = harness.sync(&ScriptedInteraction::new(Some("12345"), false));

    assert_eq!(result.unwrap_err().code, "CANCELLED");
    assert_eq!(
        harness
            .store
            .read(&pin_key(&harness.data_dir, harness.bank.0))
            .unwrap()
            .unwrap()
            .expose_secret(),
        "12345",
        "the PIN was not the problem, so it stays"
    );
    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_flag_for_another_login_forgets_the_entry_and_asks_again() {
    let harness = Harness::new();
    store_pin(&harness);
    settings::set_setting(&harness.db, &pin_flag_key(harness.bank), "37050198|someone-else").unwrap();
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    harness.sync(&interaction).unwrap();

    assert_eq!(interaction.pin_asked(), 1);
    assert!(
        harness.store.is_empty(),
        "the entry belongs to the old login and is removed"
    );
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_flag_whose_store_entry_is_gone_is_cleared_and_the_user_asked() {
    let harness = Harness::new();
    settings::set_setting(&harness.db, &pin_flag_key(harness.bank), "37050198|demo-user").unwrap();
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    harness.sync(&interaction).unwrap();

    assert_eq!(interaction.pin_asked(), 1);
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn a_store_that_cannot_be_read_is_not_used_and_nothing_is_deleted() {
    let harness = Harness::new();
    store_pin(&harness);
    harness.store.set_failing_reads(true);
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    let report = harness.sync(&interaction).unwrap();

    assert_eq!(interaction.pin_asked(), 1);
    harness.store.set_failing_reads(false);
    assert_eq!(
        harness
            .store
            .read(&pin_key(&harness.data_dir, harness.bank.0))
            .unwrap()
            .unwrap()
            .expose_secret(),
        "12345",
        "a read failure must not delete the entry"
    );
    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(report.warnings.is_empty());
}

#[test]
fn a_store_that_cannot_be_written_does_not_fail_the_sync() {
    let harness = Harness::new();
    harness.store.set_failing_writes(true);
    let giro = "DE89370400440532013000";
    harness
        .gateway
        .set_snapshots(vec![snapshot(giro, "100.00", Vec::new())]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), true)).unwrap();

    assert!(report.warnings.is_empty());
    assert!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .last_sync
            .is_some()
    );
    assert!(harness.service.pin_not_saved());
    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn removing_the_bank_forgets_the_stored_pin_and_its_flag() {
    let harness = Harness::new();
    store_pin(&harness);

    harness.service.remove_bank(&harness.db, harness.bank).unwrap();

    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn removing_a_bank_without_a_remembered_pin_logs_no_forgotten_pin() {
    let harness = Harness::new();

    harness.service.remove_bank(&harness.db, harness.bank).unwrap();

    assert!(
        audit::recent(&harness.db, 100)
            .unwrap()
            .iter()
            .all(|entry| entry.action != "PIN_FORGOTTEN")
    );
}

#[test]
fn saving_the_same_user_id_with_surrounding_spaces_keeps_the_pin() {
    let harness = Harness::new();
    store_pin(&harness);

    harness
        .service
        .update_connection(
            &harness.db,
            harness.bank,
            "Sparkasse",
            "37050198",
            " demo-user ",
            "https://fints.example/pintan",
        )
        .unwrap();

    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));
    assert_eq!(
        harness
            .store
            .read(&pin_key(&harness.data_dir, harness.bank.0))
            .unwrap()
            .unwrap()
            .expose_secret(),
        "12345"
    );
}

#[test]
fn changing_the_user_id_forgets_the_pin() {
    let harness = Harness::new();
    store_pin(&harness);

    harness
        .service
        .update_connection(&harness.db, harness.bank, "Sparkasse", "37050198", "other-user", "")
        .unwrap();

    assert_eq!(
        repo::bank_connections::get_item(&harness.db, harness.bank)
            .unwrap()
            .unwrap()
            .user_id,
        "other-user"
    );
    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
}

#[test]
fn deleting_a_profile_forgets_the_pins_remembered_in_it() {
    let harness = Harness::new();
    store_pin(&harness);
    let marker = crate::sync::credentials::pin_marker_path(&harness.data_dir, harness.bank.0);
    assert!(marker.exists());

    crate::sync::credentials::forget_pins_for_profile(&*harness.store, &harness.data_dir);

    assert!(harness.store.is_empty());
    assert!(!marker.exists());
}

#[test]
fn two_profiles_with_the_same_bank_id_do_not_share_a_credential_key() {
    let harness = Harness::new();
    let first_key = pin_key(&harness.data_dir, harness.bank.0);
    let other = harness.temp.path().join("business");
    let second_key = pin_key(&other, harness.bank.0);
    assert_ne!(first_key, second_key);

    harness.store.write(&first_key, &SecretString::from("12345")).unwrap();
    assert!(harness.store.read(&second_key).unwrap().is_none());
}

#[test]
fn forgetting_a_named_profile_removes_only_its_pin_and_marker() {
    let temp = tempfile::tempdir().unwrap();
    let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
    let work_dir = paths.profile_dir("work").unwrap();
    let default_dir = paths.profile_dir("default").unwrap();
    let store = Arc::new(MemoryCredentialStore::new());

    let (_temp_default, default_db) = database();
    let (_temp_work, work_db) = database();
    let default_bank = create_bank(&default_db, "Default Bank", "12030000", "default-user", true);
    let work_bank = create_bank(&work_db, "Work Bank", "12030000", "work-user", true);
    assert_eq!(default_bank.0, 1);
    assert_eq!(work_bank.0, 1);

    let default_gateway = Arc::new(FakeGateway::default());
    let default_service = BankingService::new(FinTsClient::new(
        default_gateway,
        default_dir.clone(),
        "TEST-PRODUCT",
        store.clone(),
    ));
    default_service
        .sync_bank_connection(
            &default_db,
            default_bank,
            &ScriptedInteraction::new(Some("11111"), true),
            date!(2026 - 10 - 04),
        )
        .unwrap();

    let work_gateway = Arc::new(FakeGateway::default());
    let work_service = BankingService::new(FinTsClient::new(
        work_gateway,
        work_dir.clone(),
        "TEST-PRODUCT",
        store.clone(),
    ));
    work_service
        .sync_bank_connection(
            &work_db,
            work_bank,
            &ScriptedInteraction::new(Some("22222"), true),
            date!(2026 - 10 - 04),
        )
        .unwrap();

    let default_key = pin_key(&default_dir, 1);
    let work_key = pin_key(&work_dir, 1);
    assert_ne!(default_key, work_key, "the root prefix never matches a named profile");
    assert!(store.contains(&default_key));
    assert!(store.contains(&work_key));
    let default_marker = crate::sync::credentials::pin_marker_path(&default_dir, 1);
    let work_marker = crate::sync::credentials::pin_marker_path(&work_dir, 1);
    assert!(default_marker.exists());
    assert!(work_marker.exists());

    crate::sync::credentials::forget_pins_for_profile(&*store, &work_dir);

    assert!(!store.contains(&work_key));
    assert!(!work_marker.exists());
    assert!(store.contains(&default_key), "the default profile keeps its PIN");
    assert!(default_marker.exists());
}

#[test]
fn a_marker_write_failure_saves_no_pin() {
    let harness = Harness::new();
    std::fs::create_dir_all(&harness.data_dir).unwrap();
    std::fs::write(harness.data_dir.join("credentials"), "not a folder").unwrap();
    harness
        .gateway
        .set_snapshots(vec![snapshot("DE89370400440532013000", "100.00", Vec::new())]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), true)).unwrap();

    assert!(harness.service.pin_not_saved());
    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(report.warnings.is_empty());
}

#[test]
fn a_store_delete_failure_keeps_the_marker() {
    let harness = Harness::new();
    store_pin(&harness);
    let marker = crate::sync::credentials::pin_marker_path(&harness.data_dir, harness.bank.0);
    assert!(marker.exists());
    harness.store.set_failing_deletes(true);

    harness.service.forget_pin(&harness.db, harness.bank);

    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(harness.store.contains(&pin_key(&harness.data_dir, harness.bank.0)));
    assert!(marker.exists(), "a later attempt must still find the entry");
}

#[test]
fn debug_redacts_the_user_and_customer_ids() {
    let harness = Harness::new();
    let bank_id = create_bank(&harness.db, "Bank", "12030000", "secret-user-42", true);
    let bank = repo::bank_connections::get_item(&harness.db, bank_id).unwrap().unwrap();
    let connection = harness.service.client().connection_for(&harness.db, &bank).unwrap();

    let connection_debug = format!("{connection:?}");
    let item_debug = format!("{bank:?}");
    for debug in [&connection_debug, &item_debug] {
        assert!(!debug.contains("secret-user-42"));
        assert!(!debug.contains("12030000|secret-user-42"));
        assert!(debug.contains("***"));
    }
}

#[test]
fn bank_text_never_reaches_the_audit_log_and_stored_messages_are_masked() {
    let harness = Harness::new();
    let iban = "DE89370400440532013000";
    harness
        .gateway
        .set_failure(Some(GatewayError::Other(format!("Konto {iban} gesperrt"))));
    let error = harness
        .sync(&ScriptedInteraction::new(Some("12345"), false))
        .unwrap_err();
    assert!(error.message.contains("…3000"), "{}", error.message);
    assert!(!error.message.contains(iban));

    harness.gateway.set_failure(None);
    let mut failed = snapshot(
        iban,
        "100.00",
        vec![booking(
            iban,
            harness.today,
            "-1.00",
            Some(&format!("Konto {iban} gesperrt")),
            Some(&format!("Konto {iban} gesperrt")),
            None,
        )],
    );
    failed.statement_error = Some(format!("Konto {iban} gesperrt"));
    harness.gateway.set_snapshots(vec![failed]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert!(!report.warnings.join("\n").contains(iban));
    assert!(report.warnings.join("\n").contains("…3000"));
    let account_id = harness.accounts().into_iter().next().unwrap().id;
    let stored = transactions(&harness.db, account_id);
    assert!(!stored.is_empty());
    // Booking texts are the user's own data and stay as the bank sent them (as FinDesk and the CSV import).
    for transaction in stored {
        for text in [transaction.partner_name, transaction.purpose].into_iter().flatten() {
            assert_eq!(text, format!("Konto {iban} gesperrt"));
        }
    }
    harness.assert_audit_has_no(&[iban, "gesperrt", "demo-user"]);
}

#[test]
fn changing_only_the_url_resets_the_registration_and_forgets_the_pin() {
    // This deliberately differs from FinDesk, which kept the PIN on a URL change. The PIN flag is `<blz>|<userId>`,
    // so a kept PIN would be sent to whatever host the user typed, including a mistyped one.
    let harness = Harness::new();
    store_pin(&harness);
    repo::bank_connections::update_capabilities(&harness.db, harness.bank, "SaldoReq,KUmsAll").unwrap();
    repo::bank_connections::update(&harness.db, harness.bank, None, Some("942".to_owned())).unwrap();

    harness
        .service
        .update_connection(
            &harness.db,
            harness.bank,
            "Sparkasse",
            "37050198",
            "demo-user",
            "https://new.example/fints",
        )
        .unwrap();

    let bank = repo::bank_connections::get_item(&harness.db, harness.bank)
        .unwrap()
        .unwrap();
    assert_eq!(bank.capabilities, None);
    assert_eq!(bank.tan_method, None);
    assert_eq!(bank.server_url.as_deref(), Some("https://new.example/fints"));
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(harness.store.is_empty());
    assert_eq!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .filter(|entry| entry.action == "RESET_FINTS_LOGIN")
            .count(),
        1
    );
}

#[test]
fn changing_the_user_id_resets_the_registration_and_forgets_the_pin() {
    let harness = Harness::new();
    store_pin(&harness);

    harness
        .service
        .update_connection(&harness.db, harness.bank, "Sparkasse", "37050198", "new-user", "")
        .unwrap();

    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert_eq!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .filter(|entry| entry.action == "RESET_FINTS_LOGIN")
            .count(),
        1
    );
}

#[test]
fn changing_the_blz_resets_forgets_the_pin_and_keeps_the_accounts() {
    let harness = Harness::new();
    store_pin(&harness);
    let giro = "DE89370400440532013000";
    let account_id = harness.accounts().into_iter().next().unwrap().id;
    let mut booked = snapshot(
        giro,
        "100.00",
        vec![booking(
            giro,
            harness.today - Duration::days(1),
            "-5.00",
            Some("Shop"),
            Some("A"),
            Some("100.00"),
        )],
    );
    booked.pending = None;
    harness.gateway.set_snapshots(vec![booked]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    harness
        .service
        .update_connection(&harness.db, harness.bank, "Sparkasse", "70020270", "demo-user", "")
        .unwrap();

    assert!(harness.store.is_empty());
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    let bank = repo::bank_connections::get_item(&harness.db, harness.bank)
        .unwrap()
        .unwrap();
    assert_eq!(bank.blz, "70020270");
    assert!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .any(|entry| entry.action == "CHANGE_BANK_CODE" && entry.details.contains("37050198 -> 70020270"))
    );
    assert!(harness.accounts().iter().any(|account| account.id == account_id));
    assert!(!transactions(&harness.db, account_id).is_empty());
}

#[test]
fn saving_unchanged_values_resets_nothing() {
    let harness = Harness::new();
    store_pin(&harness);

    harness
        .service
        .update_connection(
            &harness.db,
            harness.bank,
            "Sparkasse KölnBonn",
            "37050198",
            "demo-user",
            "https://fints.example/pintan",
        )
        .unwrap();

    assert!(!harness.store.is_empty());
    assert!(harness.service.has_saved_pin(&harness.db, harness.bank));
    let entries = audit::recent(&harness.db, 1000).unwrap();
    assert!(entries.iter().all(|entry| entry.action != "RESET_FINTS_LOGIN"));
    assert!(entries.iter().all(|entry| entry.action != "CHANGE_BANK_CODE"));
}

#[test]
fn tests_never_reach_a_real_credential_store() {
    assert!(!NoCredentialStore.is_available());
    assert!(NoCredentialStore.read("key").unwrap().is_none());
}

// ---- read-only guard -----------------------------------------------------

#[test]
fn an_offline_bank_never_reaches_the_gateway() {
    let harness = Harness::new();
    let offline = repo::bank_connections::create_offline(&harness.db, "Norwegian Card").unwrap();
    let interaction = ScriptedInteraction::new(Some("12345"), false);

    let error = harness
        .service
        .sync_bank_connection(&harness.db, offline.id, &interaction, harness.today)
        .unwrap_err();

    assert_eq!(error.code, "OFFLINE");
    assert_eq!(interaction.pin_asked(), 0);
    assert!(harness.gateway.since_requests.lock().unwrap().is_empty());
}

#[test]
fn only_offline_banks_are_refused_locally_an_online_bank_still_syncs() {
    let harness = Harness::new();
    harness.gateway.set_snapshots(Vec::new());

    assert!(harness.sync(&ScriptedInteraction::new(Some("12345"), false)).is_ok());
}

#[test]
fn an_offline_bank_update_only_changes_the_name() {
    let harness = Harness::new();
    let offline = repo::bank_connections::create_offline(&harness.db, "Norwegian Card").unwrap();

    let updated = harness
        .service
        .update_connection(
            &harness.db,
            offline.id,
            "Norwegian Visa",
            "12345678",
            "user",
            "https://attacker.example/fints",
        )
        .unwrap();

    assert_eq!(updated.bank_name, "Norwegian Visa");
    assert_eq!(updated.blz, "");
    assert_eq!(updated.user_id, "");
    assert_eq!(updated.server_url, None);
    assert!(updated.offline);
}

#[test]
fn adding_a_bank_connection_stores_the_server_url_and_audits_it() {
    let harness = Harness::new();
    let config = FinTsConfig {
        bank_name: "Neue Bank".to_owned(),
        blz: "12030000".to_owned(),
        bic: "BYLADEM1001".to_owned(),
        user_id: "new-user".to_owned(),
        customer_id: None,
        hbci_version: "300".to_owned(),
        server_url: Some("https://fints.example/new".to_owned()),
        tan_method: None,
        product_id: "TEST-PRODUCT".to_owned(),
    };

    let created = harness.service.add_bank_connection(&harness.db, &config).unwrap();

    assert_eq!(created.bank_name, "Neue Bank");
    assert_eq!(created.server_url.as_deref(), Some("https://fints.example/new"));
    assert!(
        audit::recent(&harness.db, 100)
            .unwrap()
            .iter()
            .any(|entry| entry.action == "ADD_BANK")
    );
}

// ---- account currency and type correction --------------------------------

fn bank_account(
    db: &Database,
    bank: BankConnectionId,
    iban: &str,
    balance: Money,
    account_type: AccountType,
) -> AccountId {
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: Some(bank),
            institution: Some("Sparkasse".to_owned()),
            name: Some("Account".to_owned()),
            iban: Some(iban.to_owned()),
            account_number: None,
            currency: balance.currency,
            account_type,
            balance,
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

#[test]
fn a_sync_corrects_an_account_the_old_currency_fallback_created_as_eur() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "CHF")]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::new("CHF").unwrap());
    assert_eq!(stored.balance.minor, 8_850);
    let audit = audit::recent(&harness.db, 1000).unwrap();
    let correction = audit
        .iter()
        .find(|entry| entry.action == "CORRECT_ACCOUNT_CURRENCY")
        .expect("a currency correction entry");
    assert!(correction.details.contains("EUR -> CHF"), "{}", correction.details);
    assert!(!correction.details.contains(giro));
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncCurrencyCorrected,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("EUR".to_owned()),
                Arg::Str("CHF".to_owned()),
            ],
        )]
    );
}

#[test]
fn a_second_sync_of_a_corrected_account_writes_no_further_correction() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "CHF")]);
    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let mut second = foreign_snapshot(giro, "CHF");
    if let Some(balance) = &mut second.balance {
        balance.balance = "90.00".parse().unwrap();
    }
    harness.gateway.set_snapshots(vec![second]);
    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    assert_eq!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .filter(|entry| entry.action == "CORRECT_ACCOUNT_CURRENCY")
            .count(),
        1
    );
    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::new("CHF").unwrap());
    assert_eq!(stored.balance.minor, 9_000);
    assert!(report.notes.is_empty(), "{:?}", report.notes);
}

#[test]
fn an_account_with_an_eur_booking_keeps_its_currency_and_the_other_currency_note() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    let account = bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    repo::transactions::insert_imported(
        &harness.db,
        account,
        vec![NewTransaction {
            booking_date: harness.today - Duration::days(5),
            amount: Money {
                minor: -500,
                currency: Currency::EUR,
            },
            balance_after: None,
            partner_name: Some("Shop".to_owned()),
            purpose: Some("Old EUR booking".to_owned()),
            category_id: None,
            import_hash: "old-eur".to_owned(),
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }],
    )
    .unwrap();
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "CHF")]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::EUR);
    assert_eq!(stored.balance.minor, 10_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("CHF".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
    assert!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .all(|entry| entry.action != "CORRECT_ACCOUNT_CURRENCY")
    );
}

#[test]
fn an_account_whose_bank_states_its_currency_is_not_corrected() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    let mut foreign = foreign_snapshot(giro, "CHF");
    foreign.account.currency_stated = true;
    harness.gateway.set_snapshots(vec![foreign]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::EUR);
    assert_eq!(stored.balance.minor, 10_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("CHF".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
    assert!(
        audit::recent(&harness.db, 1000)
            .unwrap()
            .iter()
            .all(|entry| entry.action != "CORRECT_ACCOUNT_CURRENCY")
    );
}

#[test]
fn a_balance_currency_that_is_not_three_letters_never_corrects_the_account() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "CH")]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::EUR);
    assert_eq!(stored.balance.minor, 10_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("CH".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
}

#[test]
fn a_blank_balance_currency_never_corrects_the_account() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::EUR,
        },
        AccountType::Checking,
    );
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "")]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::EUR);
    assert_eq!(stored.balance.minor, 10_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("".to_owned()),
                Arg::Str("EUR".to_owned()),
            ],
        )]
    );
}

#[test]
fn an_account_kept_in_another_currency_than_eur_is_not_corrected() {
    let harness = Harness::new();
    let giro = "DE89370400440532013000";
    bank_account(
        &harness.db,
        harness.bank,
        giro,
        Money {
            minor: 10_000,
            currency: Currency::new("USD").unwrap(),
        },
        AccountType::Checking,
    );
    harness.gateway.set_snapshots(vec![foreign_snapshot(giro, "CHF")]);

    let report = harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let stored = harness.accounts().into_iter().next().unwrap();
    assert_eq!(stored.currency, Currency::new("USD").unwrap());
    assert_eq!(stored.balance.minor, 10_000);
    assert_eq!(
        report.notes,
        vec![i18n::format(
            Language::En,
            Key::SyncBalanceOtherCurrency,
            &[
                Arg::Str("3000".to_owned()),
                Arg::Str("CHF".to_owned()),
                Arg::Str("USD".to_owned()),
            ],
        )]
    );
}

#[test]
fn a_bank_typed_loan_corrects_only_a_stored_depot_never_a_type_the_user_chose() {
    let harness = Harness::new();
    let depot_iban = "DE89370400440532013041";
    let savings_iban = "DE89370400440532013042";
    bank_account(
        &harness.db,
        harness.bank,
        depot_iban,
        Money {
            minor: 0,
            currency: Currency::EUR,
        },
        AccountType::Depot,
    );
    bank_account(
        &harness.db,
        harness.bank,
        savings_iban,
        Money {
            minor: 0,
            currency: Currency::EUR,
        },
        AccountType::Savings,
    );
    let mut depot = account(depot_iban);
    depot.account_type = "loan".to_owned();
    let mut savings = account(savings_iban);
    savings.account_type = "loan".to_owned();
    harness.gateway.set_snapshots(vec![
        snapshot(depot_iban, "1.00", Vec::new()).with_account(depot),
        snapshot(savings_iban, "2.00", Vec::new()).with_account(savings),
    ]);

    harness.sync(&ScriptedInteraction::new(Some("12345"), false)).unwrap();

    let accounts = harness.accounts();
    assert_eq!(
        accounts
            .iter()
            .find(|a| a.iban.as_deref() == Some(depot_iban))
            .unwrap()
            .account_type,
        AccountType::Loan
    );
    assert_eq!(
        accounts
            .iter()
            .find(|a| a.iban.as_deref() == Some(savings_iban))
            .unwrap()
            .account_type,
        AccountType::Savings,
        "a type the user chose is left alone"
    );
    let corrections = audit::recent(&harness.db, 1000).unwrap();
    let corrections = corrections
        .iter()
        .filter(|entry| entry.action == "CHANGE_ACCOUNT_TYPE")
        .collect::<Vec<_>>();
    assert_eq!(corrections.len(), 1);
    assert!(corrections[0].details.contains("depot -> loan"));
    assert_eq!(
        corrections[0].details,
        format!(
            "Account {}: depot -> loan",
            accounts
                .iter()
                .find(|a| a.iban.as_deref() == Some(depot_iban))
                .unwrap()
                .id
                .0
        )
    );
}

impl AccountSnapshot {
    fn with_account(mut self, account: FinTsAccount) -> Self {
        if let Some(balance) = &mut self.balance {
            balance.account = account.clone();
        }
        self.account = account;
        self
    }
}

#[test]
fn connection_for_refuses_an_offline_bank() {
    let harness = Harness::new();
    let offline = repo::bank_connections::create_offline(&harness.db, "Norwegian Card").unwrap();
    let bank = repo::bank_connections::get_item(&harness.db, offline.id)
        .unwrap()
        .unwrap();

    let error = harness.service.client().connection_for(&harness.db, &bank).unwrap_err();

    assert_eq!(error.code, "OFFLINE");
}

#[test]
fn remembering_or_forgetting_a_pin_for_an_offline_bank_touches_no_credentials() {
    let harness = Harness::new();
    let offline = repo::bank_connections::create_offline(&harness.db, "Norwegian Card").unwrap();

    harness.service.forget_pin(&harness.db, offline.id);

    assert!(!harness.service.has_saved_pin(&harness.db, offline.id));
}

#[test]
fn a_failed_login_reset_keeps_the_old_settings_and_still_forgets_the_pin() {
    let harness = Harness::new();
    store_pin(&harness);
    repo::bank_connections::update_capabilities(&harness.db, harness.bank, "SaldoReq,KUmsAll").unwrap();
    // Make clearing the login state fail, as a full disk or a locked database would.
    harness
        .db
        .with_conn(|conn| {
            conn.execute_batch(
                "CREATE TRIGGER refuse_clear BEFORE UPDATE OF capabilities ON bank_connections \
                 WHEN NEW.capabilities IS NULL BEGIN SELECT RAISE(ABORT, 'refused'); END;",
            )?;
            Ok(())
        })
        .unwrap();

    let result = harness.service.update_connection(
        &harness.db,
        harness.bank,
        "Sparkasse",
        "37050198",
        "demo-user",
        "https://new.example/fints",
    );

    assert!(result.is_err());
    let bank = repo::bank_connections::get_item(&harness.db, harness.bank)
        .unwrap()
        .unwrap();
    assert_ne!(
        bank.server_url.as_deref(),
        Some("https://new.example/fints"),
        "the edit is not saved"
    );
    assert!(!harness.service.has_saved_pin(&harness.db, harness.bank));
    assert!(harness.store.is_empty());
}
