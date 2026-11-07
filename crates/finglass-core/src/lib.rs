//! Finglass core: everything except the UI.
//!
//! The UI crate renders state that comes from here and sends user actions back; it holds no logic of its own,
//! so everything that matters is testable without a window.

pub mod account_grouping;
pub mod account_ids;
pub mod app;
pub mod audit;
pub mod backup;
pub mod balance;
pub mod bank_text;
pub mod dashboard;
pub mod db;
pub mod documents;
pub mod export;
pub mod findesk_import;
pub mod format;
pub mod i18n;
pub mod iban;
pub mod model;
pub mod money;
pub mod paths;
pub mod profiles;
pub mod receipt;
pub mod recurring;
pub mod repo;
pub mod report;
pub mod rules;
pub mod secret;
pub mod settings;
pub mod sync;

/// The product name, as shown in window titles and the About dialog.
pub const APP_NAME: &str = "Finglass";

/// The app version, from the workspace manifest.
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

pub mod import;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn version_comes_from_the_manifest() {
        assert_eq!(VERSION, env!("CARGO_PKG_VERSION"));
        assert_eq!(
            VERSION.split('.').count(),
            3,
            "semantic version expected, got {VERSION}"
        );
    }

    #[test]
    fn app_name_is_finglass() {
        assert_eq!(APP_NAME, "Finglass");
    }
}
