//! PIN lifecycle and the fetch entry point, ported from FinDesk's `FinTSClient.kt`.
//!
//! A stored PIN is used at most once per dialog, only for the login it was saved for, and is dropped before the user
//! is asked again after a rejection. A PIN is saved only when the user ticked "remember" and the dialog succeeded.

use std::cell::{Cell, RefCell};
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};

use secrecy::{ExposeSecret, SecretString};
use time::Date;

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{BankAccessItem, BankConnectionId, BankConnectionUpdate};
use crate::paths::AppPaths;
use crate::repo;
use crate::settings;

pub use super::credentials::pin_key;
use super::credentials::{CredentialStore, pin_marker_path};
use super::failures::{BankResult, BankingError, GatewayError};
use super::gateway::{AccountSnapshot, FinTsConnection, FinTsGateway};
use super::interaction::{BankingInteraction, PinEntry};

/// Turns a stored bank access into a FinTS connection and runs the PIN lifecycle around one read-only dialog.
pub struct FinTsClient {
    gateway: Arc<dyn FinTsGateway>,
    data_dir: PathBuf,
    product_id: String,
    credentials: Arc<dyn CredentialStore>,
    language: Language,
    pin_not_saved: AtomicBool,
}

impl FinTsClient {
    /// Builds a client. `product_id` is the caller's FinTS registration id; core never hard-codes it.
    ///
    /// `data_dir` must be the profile's canonical folder, as built by [`AppPaths::profile_dir`]; remembered PINs are
    /// scoped by that folder, and [`super::credentials::forget_pins_for_profile`] uses the same value.
    pub fn new(
        gateway: Arc<dyn FinTsGateway>,
        data_dir: impl Into<PathBuf>,
        product_id: impl Into<String>,
        credentials: Arc<dyn CredentialStore>,
    ) -> Self {
        Self {
            gateway,
            data_dir: data_dir.into(),
            product_id: product_id.into(),
            credentials,
            language: Language::En,
            pin_not_saved: AtomicBool::new(false),
        }
    }

    /// Sets the language used for user-visible error text.
    pub fn with_language(mut self, language: Language) -> Self {
        self.language = language;
        self
    }

    /// The language used for user-visible error text.
    pub fn language(&self) -> Language {
        self.language
    }

    /// The profile's data directory, which scopes remembered PINs.
    pub fn data_dir(&self) -> &std::path::Path {
        &self.data_dir
    }

    /// The configured FinTS product id.
    pub fn product_id(&self) -> &str {
        &self.product_id
    }

    /// Whether the system offers a credential store, so the UI may offer to remember the PIN.
    pub fn can_remember_pin(&self) -> bool {
        self.credentials.is_available()
    }

    /// Set when the last sync's opted-in PIN could not be saved. The view model shows a short, non-secret note.
    pub fn pin_not_saved(&self) -> bool {
        self.pin_not_saved.load(Ordering::SeqCst)
    }

    /// Builds the connection for a stored bank access.
    ///
    /// An offline bank never reaches FinTS. A missing server URL is an error here: the bank directory is not ported
    /// (the data licence is still pending), so the caller must have set the URL.
    pub fn connection_for(&self, _db: &Database, bank: &BankAccessItem) -> BankResult<FinTsConnection> {
        if bank.offline {
            return Err(BankingError::new(
                "OFFLINE",
                i18n::text(self.language, Key::ErrorBankOffline),
            ));
        }
        let server_url = bank.server_url.as_deref().map(str::trim).filter(|url| !url.is_empty());
        let Some(server_url) = server_url else {
            return Err(BankingError::new(
                "NO_SERVER",
                i18n::format(self.language, Key::ErrorNoFintsServer, &[Arg::Str(bank.blz.clone())]),
            ));
        };
        if bank.user_id.trim().is_empty() {
            return Err(BankingError::new(
                "NO_USER",
                i18n::format(
                    self.language,
                    Key::ErrorEnterUserId,
                    &[Arg::Str(bank.bank_name.clone())],
                ),
            ));
        }

        Ok(FinTsConnection {
            bank_name: bank.bank_name.clone(),
            blz: bank.blz.clone(),
            user_id: bank.user_id.clone(),
            customer_id: bank.user_id.clone(),
            server_url: server_url.to_owned(),
            tan_method: bank.tan_method.clone(),
            product_id: self.product_id.clone(),
        })
    }

    /// Fetches accounts, balances and bookings for one bank access in a single dialog.
    ///
    /// Only this call may use a stored PIN. It belongs to the current login and the credential store still has it.
    pub fn fetch_all(
        &self,
        db: &Database,
        bank: &BankAccessItem,
        interaction: &dyn BankingInteraction,
        since: &dyn Fn(&str) -> Option<Date>,
        to: Date,
    ) -> BankResult<Vec<AccountSnapshot>> {
        self.pin_not_saved.store(false, Ordering::SeqCst);

        let stored = self.stored_pin_for(db, bank);
        let on_stored_rejected = || self.forget_pin(db, bank.id);
        let remembering = RememberingInteraction {
            inner: interaction,
            stored,
            on_stored_rejected: &on_stored_rejected,
            stored_used: Cell::new(false),
            stored_dropped: Cell::new(false),
            last_entry: RefCell::new(None),
        };

        let connection = self.connection_for(db, bank)?;
        let result = self.gateway.fetch_all(&connection, &remembering, since, to);

        let outcome = match result {
            Ok(outcome) => Ok(outcome),
            Err(error) => Err(self.map_gateway_error(error)),
        };

        self.finish_fetch_all(db, bank.id, &remembering, outcome.as_ref().err());

        match outcome {
            Ok(outcome) => {
                if let Some(method) = &outcome.tan_method
                    && bank.tan_method.as_deref() != Some(method.as_str())
                {
                    let _ = repo::bank_connections::update_connection(
                        db,
                        bank.id,
                        BankConnectionUpdate {
                            tan_method: Some(method.clone()),
                            ..BankConnectionUpdate::default()
                        },
                    );
                }
                if let Some(capabilities) = &outcome.capabilities {
                    let _ = repo::bank_connections::update_capabilities(db, bank.id, &capabilities.serialize());
                }
                Ok(outcome.snapshots)
            }
            Err(error) => Err(error),
        }
    }

    /// Settles the PIN after a sync dialog: save an opted-in PIN after success, drop a stored PIN the bank rejected.
    /// Cancellation keeps the stored PIN (nothing changed at the bank), and a store failure never fails the sync.
    fn finish_fetch_all(
        &self,
        db: &Database,
        bank_id: BankConnectionId,
        remembering: &RememberingInteraction<'_>,
        error: Option<&BankingError>,
    ) {
        let entered = remembering.take_last_entry();
        match error {
            None => {
                if let Some(entry) = entered
                    && entry.remember()
                    && !entry.is_empty()
                    && self.save_pin(db, bank_id, &entry).is_err()
                {
                    self.pin_not_saved.store(true, Ordering::SeqCst);
                }
            }
            Some(error) => {
                if error.code != "CANCELLED" && remembering.stored_used() {
                    self.forget_pin(db, bank_id);
                }
            }
        }
    }

    /// The stored PIN for the current login, or `None`.
    ///
    /// A stale flag (another login) forgets it; a missing entry clears the flag. A store that cannot be read is not
    /// used and nothing is deleted, so a locked keychain never costs the user the entry.
    fn stored_pin_for(&self, db: &Database, bank: &BankAccessItem) -> Option<SecretString> {
        let flag_key = pin_flag_key(bank.id);
        let flag = settings::get_setting(db, &flag_key).ok().flatten()?;
        if flag != login_signature(bank) {
            // The PIN belongs to another login: using it would only lock this one.
            self.forget_pin(db, bank.id);
            return None;
        }
        let stored = match self.credentials.read(&pin_key(&self.data_dir, bank.id.0)) {
            Ok(stored) => stored,
            // Locked or denied store: ask the user, but don't touch the entry or the flag.
            Err(_) => return None,
        };
        match stored {
            Some(secret) if !secret.expose_secret().is_empty() => Some(secret),
            _ => {
                let _ = delete_setting(db, &flag_key);
                None
            }
        }
    }

    fn save_pin(&self, db: &Database, bank_id: BankConnectionId, entry: &PinEntry) -> BankResult<()> {
        let Some(bank) = repo::bank_connections::get_item(db, bank_id).map_err(internal_error)? else {
            return Ok(());
        };
        // The marker goes first. If it cannot be written, the PIN is not saved at all and the store stays untouched.
        let marker = pin_marker_path(&self.data_dir, bank_id.0);
        let marker_result = marker
            .parent()
            .map_or(Ok(()), AppPaths::ensure_dir)
            .and_then(|()| std::fs::File::create(&marker).map(|_| ()));
        marker_result.map_err(|error| BankingError::new("PIN_NOT_SAVED", error.to_string()))?;
        self.credentials
            .write(&pin_key(&self.data_dir, bank_id.0), entry.pin())
            .map_err(|error| BankingError::new("PIN_NOT_SAVED", error.to_string()))?;
        settings::set_setting(db, &pin_flag_key(bank_id), &login_signature(&bank)).map_err(internal_error)?;
        let _ = audit::log(
            db,
            "PIN_SAVED",
            Actor::User,
            &format!("Saved PIN for bank access {}", bank_id.0),
        );
        Ok(())
    }

    /// Deletes a stored PIN and its flag. The flag goes first, so a store failure can never keep a rejected PIN in use.
    pub fn forget_pin(&self, db: &Database, bank_id: BankConnectionId) {
        let flag_key = pin_flag_key(bank_id);
        let was_saved = settings::get_setting(db, &flag_key).ok().flatten().is_some();
        let _ = delete_setting(db, &flag_key);
        // Also without a flag: an entry whose flag write failed must not linger in the store. The marker stays when the
        // store delete fails, so a later attempt still knows the entry exists.
        if self.credentials.delete(&pin_key(&self.data_dir, bank_id.0)).is_ok() {
            let _ = std::fs::remove_file(pin_marker_path(&self.data_dir, bank_id.0));
        }
        // Logged only when a PIN was remembered, not on every bank removal or user-id change.
        if was_saved {
            let _ = audit::log(
                db,
                "PIN_FORGOTTEN",
                Actor::User,
                &format!("Removed stored PIN of bank access {}", bank_id.0),
            );
        }
    }

    /// A saved PIN exists for `bank_id`; reads only the settings flag, never the credential store.
    pub fn has_saved_pin(&self, db: &Database, bank_id: BankConnectionId) -> bool {
        settings::get_setting(db, &pin_flag_key(bank_id))
            .ok()
            .flatten()
            .is_some()
    }

    /// Removes the bank's stored registration and any remembered PIN; call when the bank access is deleted.
    pub fn delete_stored_credentials(&self, db: &Database, bank_id: BankConnectionId) {
        // The connection row is deleted right after, so a failed clear changes nothing; the PIN is what matters here.
        let _ = self.reset_login(db, bank_id, true);
    }

    /// Forgets everything tied to the old login after a login field (BLZ, user id or server URL) changed.
    ///
    /// The server state lives in the connection rows (TAN method, capabilities, server-state columns), so clearing it
    /// through the repository also clears whatever M5b stores there; there is no side file to remove.
    ///
    /// A failed clear is an error: saving new login fields over the old server state would make the edit silently
    /// ineffective. Forgetting the PIN stays best-effort (the marker keeps a failed store delete findable).
    pub fn reset_login(&self, db: &Database, bank_id: BankConnectionId, forget_pin: bool) -> BankResult<()> {
        let cleared = repo::bank_connections::clear_login_state(db, bank_id);
        // The PIN goes even when the clear failed: a removed or changed login must never keep a usable secret.
        if forget_pin {
            self.forget_pin(db, bank_id);
        }
        cleared.map(|_| ()).map_err(internal_error)
    }

    fn map_gateway_error(&self, error: GatewayError) -> BankingError {
        match error {
            GatewayError::Cancelled => {
                BankingError::new("CANCELLED", i18n::text(self.language, Key::ErrorCancelledByUser))
            }
            other => BankingError::new("FINTS_ERROR", other.describe(self.language, &[])),
        }
    }
}

/// The settings flag key for a bank access's remembered PIN.
pub fn pin_flag_key(bank_id: BankConnectionId) -> String {
    format!("fints.pinSaved.{}", bank_id.0)
}

/// The login signature stored in the flag: a stored PIN is never used for another login.
pub fn login_signature(bank: &BankAccessItem) -> String {
    format!("{}|{}", bank.blz, bank.user_id)
}

fn delete_setting(db: &Database, key: &str) -> Result<(), DbError> {
    db.with_conn(|conn| {
        conn.execute("DELETE FROM settings WHERE key = ?1", [key])?;
        Ok(())
    })
}

fn internal_error(error: impl std::fmt::Display) -> BankingError {
    BankingError::new("SYNC_FAILED", error.to_string())
}

/// Wraps the caller's interaction for a sync: a stored PIN answers the first prompt, everything else is delegated,
/// and after the bank rejects it the entry is dropped before the user is asked again.
struct RememberingInteraction<'a> {
    inner: &'a dyn BankingInteraction,
    stored: Option<SecretString>,
    on_stored_rejected: &'a dyn Fn(),
    stored_used: Cell<bool>,
    stored_dropped: Cell<bool>,
    last_entry: RefCell<Option<PinEntry>>,
}

impl<'a> RememberingInteraction<'a> {
    fn stored_used(&self) -> bool {
        self.stored_used.get()
    }

    fn take_last_entry(&self) -> Option<PinEntry> {
        self.last_entry.borrow_mut().take()
    }
}

impl BankingInteraction for RememberingInteraction<'_> {
    fn pin(&self, bank_name: &str, user_id: &str, offer_remember: bool) -> Option<PinEntry> {
        if let Some(saved) = &self.stored
            && !self.stored_used.get()
        {
            self.stored_used.set(true);
            return Some(PinEntry::new(saved.clone(), false));
        }
        // A later call means the bank rejected the PIN: forget it before asking again, so cancelling at the re-prompt
        // (or a timeout) can never keep a PIN the bank refuses.
        if self.stored_used.get() && !self.stored_dropped.get() {
            self.stored_dropped.set(true);
            (self.on_stored_rejected)();
        }
        let entry = self.inner.pin(bank_name, user_id, offer_remember);
        *self.last_entry.borrow_mut() = entry.clone();
        entry
    }

    fn select_tan_method(&self, options: &[super::interaction::TanMethod]) -> Option<String> {
        self.inner.select_tan_method(options)
    }

    fn select_tan_medium(&self, options: &[String]) -> Option<String> {
        self.inner.select_tan_medium(options)
    }

    fn tan(&self, challenge: &super::interaction::TanChallenge) -> Option<SecretString> {
        self.inner.tan(challenge)
    }

    fn confirm_decoupled(&self, message: &str) -> bool {
        self.inner.confirm_decoupled(message)
    }

    fn info(&self, message: &str) {
        self.inner.info(message);
    }
}
