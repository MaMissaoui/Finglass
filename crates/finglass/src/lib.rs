//! The Finglass desktop app: a read-only, local-first view of your finances.
//!
//! This library holds the page controllers that render core state into the generated Slint UI
//! from `finglass-ui`. The binary in `main.rs` only handles `--version` and hands the paths to
//! [`run`].

mod accounts;
mod banking;
mod dashboard;
mod documents;
mod jobs;
mod migration;
pub mod reports;
mod settings;
mod shell;
pub mod transactions;

pub use finglass_ui::*;

/// Starts the app shell with the resolved data-folder paths.
pub fn run(paths: finglass_core::paths::AppPaths) -> Result<(), slint::PlatformError> {
    shell::run(paths)
}
