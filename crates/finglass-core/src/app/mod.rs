//! UI-free state used by the app shell.
//!
//! The Slint layer renders this state and sends user actions back here, so unlocking, preferences and the audit view
//! stay testable without a window.

pub mod audit;
pub mod documents;
pub mod prefs;
pub mod unlock;

pub use audit::AuditRow;
pub use documents::{DocumentCounts, DocumentTab, DocumentsState};
pub use prefs::{Prefs, ThemePref};
pub use unlock::{UnlockError, UnlockMode, change, create, mode_for, unlock, validate_new};
