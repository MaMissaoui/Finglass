//! The bank port, ported from FinDesk's `FinTSGateway.kt`.
//!
//! Only the read half exists: one dialog fetches balances, bookings and pending lines. M5b implements this trait over
//! the real FinTS library; tests and this milestone use fakes. It is synchronous on purpose, so the adapter owns any
//! async runtime instead of leaking one into core.

use rust_decimal::Decimal;
use time::{Date, OffsetDateTime};

use super::capabilities::BankCapabilities;
use super::failures::GatewayError;
use super::interaction::BankingInteraction;

/// Everything needed to open a FinTS PIN/TAN dialog with one bank login.
#[derive(Clone, PartialEq, Eq)]
pub struct FinTsConnection {
    /// The bank's display name.
    pub bank_name: String,
    /// The German bank code.
    pub blz: String,
    /// The online-banking login.
    pub user_id: String,
    /// The customer id; defaults to the user id when the bank does not distinguish them.
    pub customer_id: String,
    /// The FinTS server URL.
    pub server_url: String,
    /// A remembered TAN procedure id, if any.
    pub tan_method: Option<String>,
    /// The FinTS product registration id, supplied by the caller.
    pub product_id: String,
}

impl std::fmt::Debug for FinTsConnection {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("FinTsConnection")
            .field("bank_name", &self.bank_name)
            .field("blz", &self.blz)
            .field("user_id", &"***")
            .field("customer_id", &"***")
            .field("server_url", &self.server_url)
            .field("tan_method", &self.tan_method)
            .field("product_id", &self.product_id)
            .finish()
    }
}

/// A bank account as the bank reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinTsAccount {
    /// The IBAN, possibly empty.
    pub iban: String,
    /// The BIC reported by the bank.
    pub bic: String,
    /// The account number.
    pub account_number: String,
    /// A sub-account number, when the bank uses one.
    pub sub_account_number: Option<String>,
    /// The bank's name.
    pub bank_name: String,
    /// The bank code.
    pub blz: String,
    /// The account type code translated by the adapter (checking, savings, depot, loan, credit_card).
    pub account_type: String,
    /// The currency as stated by the bank.
    pub currency: String,
    /// A product name, when the bank reports one.
    pub product_name: Option<String>,
    /// Whether the bank stated the currency rather than the adapter falling back to EUR.
    pub currency_stated: bool,
}

/// A balance reported by the bank.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinTsBalance {
    /// The account the balance belongs to.
    pub account: FinTsAccount,
    /// The amount as delivered. Converted to minor units at the insert; never rounded.
    pub balance: Decimal,
    /// An available balance, when the bank reports one.
    pub available_balance: Option<Decimal>,
    /// The balance's currency as stated by the bank.
    pub currency: String,
    /// When the bank reported the balance.
    pub date: OffsetDateTime,
}

/// One booking or pending line as the bank reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinTsTransaction {
    /// The account the line belongs to.
    pub account: FinTsAccount,
    /// The booking date.
    pub date: Date,
    /// The value date.
    pub value_date: Date,
    /// The signed amount as delivered by the bank.
    pub amount: Decimal,
    /// The amount's currency.
    pub currency: String,
    /// The other party.
    pub partner_name: Option<String>,
    /// The other party's IBAN.
    pub partner_iban: Option<String>,
    /// The other party's BIC.
    pub partner_bic: Option<String>,
    /// The purpose text.
    pub purpose: Option<String>,
    /// The bank's transaction code.
    pub transaction_code: Option<String>,
    /// The bank's reference.
    pub reference: Option<String>,
    /// The prima-nota number, when the bank reports one.
    pub prima_nota: Option<String>,
    /// The running balance after this booking, when reported.
    pub balance_after: Option<Decimal>,
    /// A bank-side booking id (for example CAMT).
    pub bank_id: Option<String>,
}

/// One account's state as reported in a sync.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountSnapshot {
    /// The account.
    pub account: FinTsAccount,
    /// The balance, when the bank sent one.
    pub balance: Option<FinTsBalance>,
    /// Booked lines.
    pub transactions: Vec<FinTsTransaction>,
    /// Why statements could not be fetched, if they could not.
    pub statement_error: Option<String>,
    /// The bank's return code(s) when it sent no balance. Never its message text.
    pub balance_code: Option<String>,
    /// Pending lines the statement job reported. `None` means not fetched (keep what is stored); an empty list means
    /// fetched and none pending (clear what is stored).
    pub pending: Option<Vec<FinTsTransaction>>,
}

/// The result of one dialog: everything the caller must persist, without reading state off a shared gateway.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FetchOutcome {
    /// Accounts, balances and bookings.
    pub snapshots: Vec<AccountSnapshot>,
    /// The TAN method chosen during the dialog, so it can be remembered.
    pub tan_method: Option<String>,
    /// The login's supported business transactions, detected during the dialog.
    pub capabilities: Option<BankCapabilities>,
}

/// Port to the bank. Tests use fakes; the real adapter is M5b.
pub trait FinTsGateway: Send + Sync {
    /// Fetches all accounts of the login with balances and bookings from `since` (per IBAN; `None` skips statements
    /// for that account) up to `to`, within a single dialog.
    ///
    /// The chosen TAN method and the detected capabilities travel in the returned [`FetchOutcome`], so overlapping
    /// syncs never read another dialog's state off the gateway.
    fn fetch_all(
        &self,
        connection: &FinTsConnection,
        interaction: &dyn BankingInteraction,
        since: &dyn Fn(&str) -> Option<Date>,
        to: Date,
    ) -> Result<FetchOutcome, GatewayError>;
}
