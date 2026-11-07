//! Read-only live bank sync, ported from FinDesk's banking layer.
//!
//! The UI calls the functions in [`service`] (and [`client`]) for anything that touches a bank login; it never
//! calls the bank-connection or transaction repositories directly. That keeps the PIN lifecycle and the audit rules
//! in one place.
//!
//! The port is deliberately read-only. It knows balances, statements, the account list and TAN media only; there is
//! no payment code of any kind. The real FinTS adapter arrives in M5b and owns its async runtime; everything here is
//! synchronous and tested against fake gateways only.
//!
//! Not ported:
//!
//! - `Hbci4JavaGateway` and any order method (transfers, instant transfers, scheduled transfers, standing orders):
//!   Finglass is read-only, so these are out of scope by design.
//! - `CredentialSelfCheck`: the operating-system credential store is wired by the desktop app; core defines the
//!   [`credentials::CredentialStore`] port, an in-memory store, a no-op store and the macOS Keychain store.
//! - `ProtocolTrace`: that is M5b, together with the real adapter.
//! - `BankDirectory`: the bank-directory data licence is still pending, so a connection needs an explicit server URL.
//! - `BankingInteraction::confirm_payee`: Verification of Payee only exists for payments.
//!
//! Finglass has no HBCI4Java passport file. State the M5b adapter must persist (the system id and the bank parameter
//! data) belongs in `bank_connections`' server-state columns, which `repo::bank_connections::clear_login_state` resets
//! together with the TAN method and capabilities. It must never go into a side file under a `passports/` folder.

pub mod capabilities;
pub mod client;
pub mod config;
pub mod credentials;
pub mod failures;
pub mod gateway;
pub mod interaction;
pub mod keychain;
pub mod redactor;
pub mod service;

#[cfg(test)]
mod tests;

pub use capabilities::BankCapabilities;
pub use client::FinTsClient;
pub use config::FinTsConfig;
pub use credentials::{CredentialStore, MemoryCredentialStore, NoCredentialStore};
pub use failures::{BankResult, BankingError, GatewayError};
pub use gateway::{
    AccountSnapshot, FetchOutcome, FinTsAccount, FinTsBalance, FinTsConnection, FinTsGateway, FinTsTransaction,
    UnavailableGateway,
};
pub use interaction::{BankingInteraction, PinEntry, TanChallenge, TanMethod};
#[cfg(target_os = "macos")]
pub use keychain::KeychainCredentialStore;
pub use keychain::{KEYCHAIN_SERVICE, account_for, system_credential_store, system_credential_store_with};
pub use service::{BankingService, ImportResult, SyncReport};
