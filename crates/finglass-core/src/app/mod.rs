//! UI-free state used by the app shell.
//!
//! The Slint layer renders this state and sends user actions back here, so unlocking, preferences and the audit view
//! stay testable without a window.

pub mod account_panel;
pub mod accounts;
pub mod audit;
pub mod banking;
pub mod dashboard;
pub mod documents;
pub mod findesk;
pub mod prefs;
pub mod reports;
pub mod settings;
pub mod startup;
pub mod switch;
pub mod transactions;
pub mod unlock;

pub use account_panel::{AccountSelection, AppError};
pub use accounts::{AccountDetail, AccountItem, AccountsState, Busy};
pub use audit::AuditRow;
pub use banking::{BankItem, BankingPrompt, BankingState, PromptBridge, SyncAllProgress};
pub use dashboard::{
    BalanceTone, BudgetRowView, CategoryRowView, CategorySliceView, DashboardState, DashboardView, MonthBarView,
    NavigationArgs, TransactionRowView, UpcomingRowView,
};
pub use documents::{DocumentCounts, DocumentTab, DocumentsState};
pub use findesk::{FindeskImportState, findesk_start_dir, resolve_findesk_dir};
pub use prefs::{Prefs, ThemePref};
pub use reports::ReportsState;
pub use settings::{RestoreCandidate, SettingsError, SettingsState};
pub use switch::{SwitchError, begin_switch};
pub use unlock::{UnlockError, UnlockMode, change, create, mode_for, unlock, validate_new};
