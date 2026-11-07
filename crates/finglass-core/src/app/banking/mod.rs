//! The bank-login part of FinDesk's `AccountsViewModel` and the bridge to the UI.
//!
//! This is core only: the state, the actions and the blocking prompt bridge. The Slint dialogs are M5c-3. Every action
//! takes `&Database` and the [`BankingService`]; no method holds a lock while a gateway call runs, because a dialog may
//! wait minutes for a PIN or TAN.
//!
//! The sync fields and actions were left out of [`crate::app::accounts`] (M5c-2) and live here instead, so the UI can
//! own a single [`BankingState`] next to the accounts state.

pub mod bridge;

#[cfg(test)]
mod tests;

pub use bridge::{BankingPrompt, PromptBridge};

use time::{Date, OffsetDateTime};

use crate::app::accounts::Busy;
use crate::audit::{self, Actor};
use crate::db::Database;
use crate::format;
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{BankAccessItem, BankConnectionId};
use crate::repo;
use crate::sync::{BankResult, BankingError, BankingService, FinTsConfig};

/// Progress of a "Sync all" run.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SyncAllProgress {
    /// The bank just started, 1-based.
    pub position: usize,
    /// How many banks the run includes.
    pub total: usize,
    /// The bank's display name.
    pub bank_name: String,
}

/// One bank connection as the bank-login dialogs need it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BankItem {
    /// The row id.
    pub id: BankConnectionId,
    /// The bank's display name.
    pub bank_name: String,
    /// The German bank code; empty for an offline connection.
    pub blz: String,
    /// The online-banking user id; empty for an offline connection.
    pub user_id: String,
    /// The FinTS server URL.
    pub server_url: Option<String>,
    /// Whether this connection never goes online.
    pub offline: bool,
    /// Whether "Sync all" includes this connection.
    pub include_in_sync_all: bool,
    /// The start date of the next history window.
    pub sync_from: Option<Date>,
    /// The last successful sync.
    pub last_sync: Option<OffsetDateTime>,
    /// Whether a PIN is remembered for this login.
    pub has_saved_pin: bool,
}

/// The bank-login state: validation, connection editing, PIN forgetting and sync orchestration.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BankingState {
    /// The language for user-visible texts.
    pub language: Language,
    /// The current error text, already localized.
    pub error: Option<String>,
    /// The current success text, already localized.
    pub message: Option<String>,
    /// The bank a single sync is running for.
    pub syncing_bank: Option<BankConnectionId>,
    /// The current step of a single sync, for example "Connecting…".
    pub sync_step: Option<String>,
    /// The progress of a "Sync all" run.
    pub sync_all: Option<SyncAllProgress>,
    /// Every bank connection, in bank-card order.
    pub banks: Vec<BankItem>,
    /// Whether the system offers a credential store, so the UI may offer to remember a PIN.
    pub can_remember_pin: bool,
}

impl BankingState {
    /// Builds empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            language,
            error: None,
            message: None,
            syncing_bank: None,
            sync_step: None,
            sync_all: None,
            banks: Vec::new(),
            can_remember_pin: false,
        }
    }

    /// Whether a single sync or "Sync all" is running.
    #[must_use]
    pub fn is_syncing(&self) -> bool {
        self.syncing_bank.is_some() || self.sync_all.is_some()
    }

    /// Reloads the bank list, the remembered-PIN flags and the credential-store availability.
    pub fn load(&mut self, db: &Database, service: &BankingService) -> BankResult<()> {
        self.refresh(db, service)
    }

    /// Reloads the bank list, the remembered-PIN flags and the credential-store availability.
    pub fn refresh(&mut self, db: &Database, service: &BankingService) -> BankResult<()> {
        let banks = repo::bank_connections::list_items(db).map_err(|_| {
            BankingError::new(
                "SYNC_FAILED",
                i18n::text(self.language, Key::ErrorUnexpected).to_owned(),
            )
        })?;
        self.can_remember_pin = service.can_remember_pin();
        self.banks = banks
            .into_iter()
            .map(|bank| BankItem {
                id: bank.id,
                bank_name: bank.bank_name,
                blz: bank.blz,
                user_id: bank.user_id,
                server_url: bank.server_url,
                offline: bank.offline,
                include_in_sync_all: bank.include_in_sync_all,
                sync_from: bank.sync_from,
                last_sync: bank.last_sync,
                has_saved_pin: !bank.offline && service.has_saved_pin(db, bank.id),
            })
            .collect();
        Ok(())
    }

    /// Validates a bank's name and BLZ; returns an error text or `None`.
    #[must_use]
    pub fn validate_bank_access(&self, name: &str, blz: &str) -> Option<String> {
        if name.trim().is_empty() {
            return Some(i18n::text(self.language, Key::BankNameRequired).to_owned());
        }
        if !is_eight_digits(blz.trim()) {
            return Some(i18n::text(self.language, Key::BlzMustBe8Digits).to_owned());
        }
        None
    }

    /// Validates the FinTS server URL: it is required and must use HTTPS.
    #[must_use]
    pub fn validate_server_url(&self, server_url: &str) -> Option<String> {
        let url = server_url.trim();
        if url.is_empty() {
            return Some(i18n::text(self.language, Key::ErrorServerUrlRequired).to_owned());
        }
        let lower = url.to_ascii_lowercase();
        if !lower.starts_with("https://") || lower.len() == "https://".len() {
            return Some(i18n::text(self.language, Key::ErrorServerUrlMustBeHttps).to_owned());
        }
        None
    }

    /// Adds an online bank login. The server URL is required because Finglass has no bank directory yet.
    #[allow(clippy::too_many_arguments)] // Mirrors FinDesk's addBankAccess fields one to one.
    pub fn add_bank_access(
        &mut self,
        db: &Database,
        service: &BankingService,
        name: &str,
        blz: &str,
        bic: &str,
        user_id: &str,
        server_url: &str,
    ) -> BankResult<()> {
        if let Some(message) = self.validate_bank_access(name, blz) {
            return Err(self.record_error_message(message));
        }
        if let Some(message) = self.validate_server_url(server_url) {
            return Err(self.record_error_message(message));
        }

        let name = name.trim();
        let mut config = FinTsConfig::new(
            name,
            blz.trim(),
            bic.trim().to_ascii_uppercase(),
            user_id.trim(),
            String::new(),
        );
        config.server_url = Some(server_url.trim().to_owned());

        if let Err(error) = service.add_bank_connection(db, &config) {
            return Err(self.record_service_error(error));
        }
        let _ = self.refresh(db, service);
        self.error = None;
        self.message = Some(i18n::format(
            self.language,
            Key::MessageAdded,
            &[Arg::Str(name.to_owned())],
        ));
        Ok(())
    }

    /// Saves a connection's settings. An offline bank only gets a new name; any sync in progress refuses the edit.
    ///
    /// The service resets the login (and forgets its PIN) when the BLZ, user id or server URL changed.
    #[allow(clippy::too_many_arguments)] // Mirrors FinDesk's updateConnection fields one to one.
    pub fn update_connection(
        &mut self,
        db: &Database,
        service: &BankingService,
        id: BankConnectionId,
        name: &str,
        blz: &str,
        user_id: &str,
        server_url: &str,
    ) -> BankResult<()> {
        if self.sync_all.is_some() || self.syncing_bank == Some(id) {
            return Err(self.record_error_key(Key::ErrorConnectionBusy));
        }

        let bank = match repo::bank_connections::get_item(db, id) {
            Ok(Some(bank)) => bank,
            Ok(None) => return Err(self.record_error_key(Key::ErrorBankConnectionNotFound)),
            Err(_) => return Err(self.record_error_key(Key::ErrorUnexpected)),
        };

        if bank.offline {
            if name.trim().is_empty() {
                return Err(self.record_error_key(Key::BankNameRequired));
            }
            return self.finish_update(
                db,
                service,
                id,
                &bank,
                service.update_connection(
                    db,
                    id,
                    name,
                    &bank.blz,
                    &bank.user_id,
                    bank.server_url.as_deref().unwrap_or(""),
                ),
            );
        }

        if let Some(message) = self.validate_bank_access(name, blz) {
            return Err(self.record_error_message(message));
        }
        if let Some(message) = self.validate_server_url(server_url) {
            return Err(self.record_error_message(message));
        }

        let result = service.update_connection(db, id, name, blz, user_id, server_url);
        self.finish_update(db, service, id, &bank, result)
    }

    fn finish_update(
        &mut self,
        db: &Database,
        service: &BankingService,
        id: BankConnectionId,
        previous: &BankAccessItem,
        result: BankResult<BankAccessItem>,
    ) -> BankResult<()> {
        let updated = match result {
            Ok(updated) => updated,
            Err(error) => return Err(self.record_service_error(error)),
        };

        // Audit details carry ids only; the old and new label are the one exception, as in FinDesk.
        if previous.offline {
            if updated.bank_name != previous.bank_name {
                let _ = audit::log(
                    db,
                    "RENAME_BANK_ACCESS",
                    Actor::User,
                    &format!(
                        "Renamed bank access {} from {} to {}",
                        id.0, previous.bank_name, updated.bank_name
                    ),
                );
            }
        } else {
            let _ = audit::log(
                db,
                "UPDATE_BANK_CONNECTION",
                Actor::User,
                &format!("Updated connection settings of bank access {}", id.0),
            );
            if updated.bank_name != previous.bank_name {
                let _ = audit::log(
                    db,
                    "RENAME_BANK_ACCESS",
                    Actor::User,
                    &format!(
                        "Renamed bank access {} from {} to {}",
                        id.0, previous.bank_name, updated.bank_name
                    ),
                );
            }
        }

        let _ = self.refresh(db, service);
        self.error = None;
        self.message = Some(i18n::text(self.language, Key::MessageConnectionSaved).to_owned());
        Ok(())
    }

    /// Removes the PIN remembered for `id`.
    pub fn forget_pin(&mut self, db: &Database, service: &BankingService, id: BankConnectionId) -> BankResult<()> {
        service.forget_pin(db, id);
        self.refresh(db, service)
    }

    /// Deletes an online bank login and its accounts.
    ///
    /// Refused while this state syncs (the sync still inserts rows) or while the profile is busy (an import may do
    /// the same). The service forgets the remembered PIN and the FinTS login before it deletes the row.
    pub fn remove_bank(
        &mut self,
        db: &Database,
        service: &BankingService,
        busy: &Busy,
        id: BankConnectionId,
    ) -> BankResult<()> {
        if self.is_syncing() {
            return Err(self.record_error_key(Key::ErrorConnectionBusy));
        }
        if busy.active() {
            return Err(self.record_error_key(Key::ErrorDeleteWhileBusy));
        }
        if let Err(error) = service.remove_bank(db, id) {
            return Err(self.record_service_error(error));
        }
        let _ = self.refresh(db, service);
        self.error = None;
        self.message = Some(i18n::text(self.language, Key::MessageBankRemoved).to_owned());
        Ok(())
    }

    /// Validates a history start typed as "dd.MM.yyyy"; returns an error text or `None`.
    #[must_use]
    pub fn validate_sync_from(&self, input: &str, today: Date) -> Option<String> {
        let Some(date) = format::parse_date(input) else {
            return Some(i18n::text(self.language, Key::ErrorSyncFromDateInvalid).to_owned());
        };
        if date > today {
            return Some(i18n::text(self.language, Key::ErrorSyncFromDateFuture).to_owned());
        }
        None
    }

    /// Claims the state for a history sync of `id` from `date`. Call it on the UI thread before spawning
    /// [`Self::run_history_from`], like [`Self::begin_sync`], so a second click is refused at once.
    pub fn begin_history_from(&mut self, id: BankConnectionId, date: Date, today: Date) -> bool {
        if self.is_syncing() {
            self.error = Some(i18n::text(self.language, Key::ErrorConnectionBusy).to_owned());
            return false;
        }
        if date > today {
            self.error = Some(i18n::text(self.language, Key::ErrorSyncFromDateFuture).to_owned());
            return false;
        }
        self.begin_sync(id)
    }

    /// Stores `date` as the start of the next sync's history window, then runs that sync for `id` under the
    /// profile's `busy`. Requires a successful [`Self::begin_history_from`].
    #[allow(clippy::too_many_arguments)] // The same inputs as `run_sync`, plus the date.
    pub fn run_history_from(
        &mut self,
        db: &Database,
        service: &BankingService,
        bridge: &PromptBridge,
        busy: &Busy,
        id: BankConnectionId,
        date: Date,
        today: Date,
    ) {
        if repo::bank_connections::set_sync_from(db, id, Some(date)).is_err() {
            self.syncing_bank = None;
            self.sync_step = None;
            self.error = Some(i18n::text(self.language, Key::ErrorUnexpected).to_owned());
            return;
        }
        let _ = audit::log(
            db,
            "SET_SYNC_FROM",
            Actor::User,
            &format!("Bank access {}: history from {}", id.0, format::date(date)),
        );
        self.run_sync(db, service, bridge, busy, id, today);
    }

    /// Drops a stored history start so the next sync uses the normal delta window again.
    pub fn clear_sync_from(&mut self, db: &Database, service: &BankingService, id: BankConnectionId) -> BankResult<()> {
        if repo::bank_connections::set_sync_from(db, id, None).is_err() {
            return Err(self.record_error_key(Key::ErrorUnexpected));
        }
        self.refresh(db, service)
    }

    /// Sets whether "Sync all" includes `id`; refused while any sync runs, because the list is read at run start.
    pub fn set_include_in_sync_all(
        &mut self,
        db: &Database,
        service: &BankingService,
        id: BankConnectionId,
        include: bool,
    ) -> BankResult<()> {
        if self.is_syncing() {
            return Err(self.record_error_key(Key::ErrorConnectionBusy));
        }
        if repo::bank_connections::set_include_in_sync_all(db, id, include).is_err() {
            return Err(self.record_error_key(Key::ErrorUnexpected));
        }
        self.refresh(db, service)
    }

    /// Claims the state for a single sync on the calling thread. Returns `false` when a sync already runs or the bank
    /// is offline, with the error text published in [`Self::error`].
    pub fn begin_sync(&mut self, id: BankConnectionId) -> bool {
        if self.is_syncing() {
            self.error = Some(i18n::text(self.language, Key::ErrorConnectionBusy).to_owned());
            return false;
        }
        let Some(bank) = self.banks.iter().find(|bank| bank.id == id) else {
            self.error = Some(i18n::text(self.language, Key::ErrorBankConnectionNotFound).to_owned());
            return false;
        };
        if bank.offline {
            self.error = Some(i18n::text(self.language, Key::ErrorBankOffline).to_owned());
            return false;
        }

        self.syncing_bank = Some(id);
        self.sync_step = Some(i18n::text(self.language, Key::SyncConnecting).to_owned());
        self.error = None;
        self.message = None;
        true
    }

    /// Runs a single sync on the worker, inside the profile's [`Busy`] section, and publishes the outcome.
    pub fn run_sync(
        &mut self,
        db: &Database,
        service: &BankingService,
        bridge: &PromptBridge,
        busy: &Busy,
        id: BankConnectionId,
        today: Date,
    ) {
        match self.sync_one(db, service, bridge, busy, id, today) {
            SyncOutcome::Success { summary, warning } => {
                self.message = Some(summary);
                self.error = warning;
            }
            SyncOutcome::Failed(message) => {
                self.message = None;
                self.error = Some(message);
            }
            SyncOutcome::Cancelled => {
                self.message = None;
                self.error = None;
            }
        }
    }

    /// Claims the state for a "Sync all" run on the calling thread. Returns `false` while a sync already runs.
    pub fn begin_sync_all(&mut self) -> bool {
        if self.is_syncing() {
            self.error = Some(i18n::text(self.language, Key::ErrorConnectionBusy).to_owned());
            return false;
        }
        self.sync_all = Some(SyncAllProgress {
            position: 0,
            total: 0,
            bank_name: String::new(),
        });
        self.error = None;
        self.message = None;
        true
    }

    /// Runs "Sync all": included, non-offline banks in bank-card order, one dialog after another.
    ///
    /// `on_progress` is called after each progress update so the caller can publish the state from the worker; the
    /// state itself is not shared. A cancelled bank stops the run with [`Key::SyncAllStopped`].
    pub fn run_sync_all(
        &mut self,
        db: &Database,
        service: &BankingService,
        bridge: &PromptBridge,
        busy: &Busy,
        today: Date,
        on_progress: &dyn Fn(&BankingState),
    ) {
        let included = match repo::bank_connections::list_items(db) {
            Ok(banks) => banks
                .into_iter()
                .filter(|bank| bank.include_in_sync_all && !bank.offline)
                .collect::<Vec<_>>(),
            Err(_) => {
                self.sync_all = None;
                self.error = Some(i18n::text(self.language, Key::ErrorUnexpected).to_owned());
                return;
            }
        };

        if included.is_empty() {
            self.sync_all = None;
            self.message = None;
            self.error = Some(i18n::text(self.language, Key::SyncAllNothingIncluded).to_owned());
            return;
        }

        let ids = included
            .iter()
            .map(|bank| bank.id.0.to_string())
            .collect::<Vec<_>>()
            .join(", ");
        let _ = audit::log(db, "SYNC_ALL", Actor::User, &format!("Sync all: bank access ids {ids}"));

        let total = included.len();
        let mut messages = Vec::new();
        let mut errors = Vec::new();
        let mut stopped_at = None;

        for (index, bank) in included.iter().enumerate() {
            self.sync_all = Some(SyncAllProgress {
                position: index + 1,
                total,
                bank_name: bank.bank_name.clone(),
            });
            on_progress(self);

            match self.sync_one(db, service, bridge, busy, bank.id, today) {
                SyncOutcome::Success { summary, warning } => {
                    messages.push(format!("{}: {summary}", bank.bank_name));
                    if let Some(warning) = warning {
                        errors.push(format!("{}: {warning}", bank.bank_name));
                    }
                }
                SyncOutcome::Failed(message) => {
                    errors.push(format!("{}: {message}", bank.bank_name));
                }
                SyncOutcome::Cancelled => {
                    stopped_at = Some(bank.bank_name.clone());
                    break;
                }
            }
        }

        if let Some(bank_name) = stopped_at {
            messages.push(i18n::format(self.language, Key::SyncAllStopped, &[Arg::Str(bank_name)]));
        }
        self.message = (!messages.is_empty()).then(|| messages.join("\n"));
        self.error = (!errors.is_empty()).then(|| errors.join("\n"));
        self.sync_all = None;
    }

    /// Clears the current error and success text.
    pub fn clear_messages(&mut self) {
        self.error = None;
        self.message = None;
    }

    /// Runs one bank's dialog and reports how it ended. The caller turns that into screen messages.
    fn sync_one(
        &mut self,
        db: &Database,
        service: &BankingService,
        bridge: &PromptBridge,
        busy: &Busy,
        id: BankConnectionId,
        today: Date,
    ) -> SyncOutcome {
        self.syncing_bank = Some(id);
        self.sync_step = Some(i18n::text(self.language, Key::SyncConnecting).to_owned());

        let result = busy.run(|| service.sync_bank_connection(db, id, bridge, today));
        self.syncing_bank = None;
        self.sync_step = None;

        let outcome = match result {
            Ok(report) => {
                let mut summary = report.summary_in(self.language);
                if service.pin_not_saved() {
                    summary.push('\n');
                    summary.push_str(i18n::text(self.language, Key::PinNotSaved));
                }
                let warning = (!report.warnings.is_empty()).then(|| report.warnings.join("\n"));
                SyncOutcome::Success { summary, warning }
            }
            Err(error) if error.code == "CANCELLED" => SyncOutcome::Cancelled,
            Err(error) => SyncOutcome::Failed(error.message),
        };

        let _ = self.refresh(db, service);
        outcome
    }

    fn record_error_key(&mut self, key: Key) -> BankingError {
        self.record_error_message(i18n::text(self.language, key).to_owned())
    }

    fn record_error_message(&mut self, message: String) -> BankingError {
        self.error = Some(message.clone());
        BankingError::new("INVALID", message)
    }

    fn record_service_error(&mut self, error: BankingError) -> BankingError {
        self.error = Some(error.message.clone());
        error
    }
}

impl Default for BankingState {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

enum SyncOutcome {
    Success { summary: String, warning: Option<String> },
    Failed(String),
    Cancelled,
}

fn is_eight_digits(text: &str) -> bool {
    text.len() == 8 && text.bytes().all(|byte| byte.is_ascii_digit())
}
