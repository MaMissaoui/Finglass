//! UI-free state used by the app shell.
//!
//! The Slint layer renders this state and sends user actions back here, so unlocking, preferences and the audit view
//! stay testable without a window.

pub mod account_panel;
pub mod accounts;
pub mod audit;
pub mod documents;
pub mod prefs;
pub mod reports;
pub mod settings;
pub mod transactions;
pub mod unlock;

pub use account_panel::{AccountSelection, AppError};
pub use accounts::{AccountDetail, AccountItem, AccountsState, Busy};
pub use audit::AuditRow;
pub use documents::{DocumentCounts, DocumentTab, DocumentsState};
pub use prefs::{Prefs, ThemePref};
pub use reports::ReportsState;
pub use settings::{RestoreCandidate, SettingsError, SettingsState};
pub use unlock::{UnlockError, UnlockMode, change, create, mode_for, unlock, validate_new};
