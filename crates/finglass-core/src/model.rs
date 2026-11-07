//! Plain domain data. Nothing here holds a database connection.

use time::{Date, OffsetDateTime};

use crate::money::{Currency, Money};

macro_rules! id_type {
    ($name:ident) => {
        /// A database row id.
        #[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
        pub struct $name(pub i64);
    };
}

id_type!(AccountId);
id_type!(AccountGroupId);
id_type!(BankConnectionId);
id_type!(CategoryId);
id_type!(RuleId);
id_type!(TransactionId);

/// How an account is used.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum AccountType {
    /// A normal checking account.
    Checking,
    /// A savings account.
    Savings,
    /// A credit card account.
    CreditCard,
    /// A loan account.
    Loan,
    /// A securities depot.
    Depot,
}

impl AccountType {
    /// The text stored in the database.
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Checking => "checking",
            Self::Savings => "savings",
            Self::CreditCard => "credit_card",
            Self::Loan => "loan",
            Self::Depot => "depot",
        }
    }

    /// Reads the text stored in the database.
    pub fn from_stored(text: &str) -> Option<Self> {
        match text {
            "checking" => Some(Self::Checking),
            "savings" => Some(Self::Savings),
            "credit_card" => Some(Self::CreditCard),
            "loan" => Some(Self::Loan),
            "depot" => Some(Self::Depot),
            _ => None,
        }
    }
}

/// Which field a rule matches.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum RuleField {
    /// Match any field.
    Any,
    /// Match the partner name.
    Partner,
    /// Match the purpose text.
    Purpose,
}

impl RuleField {
    /// The text stored in the database.
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Any => "any",
            Self::Partner => "partner",
            Self::Purpose => "purpose",
        }
    }

    /// Reads the text stored in the database.
    pub fn from_stored(text: &str) -> Option<Self> {
        match text {
            "any" => Some(Self::Any),
            "partner" => Some(Self::Partner),
            "purpose" => Some(Self::Purpose),
            _ => None,
        }
    }
}

/// Which direction a rule applies to.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum RuleDirection {
    /// Both expenses and income.
    Any,
    /// Expenses only.
    Expense,
    /// Income only.
    Income,
}

impl RuleDirection {
    /// The text stored in the database.
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Any => "any",
            Self::Expense => "expense",
            Self::Income => "income",
        }
    }

    /// Reads the text stored in the database.
    pub fn from_stored(text: &str) -> Option<Self> {
        match text {
            "any" => Some(Self::Any),
            "expense" => Some(Self::Expense),
            "income" => Some(Self::Income),
            _ => None,
        }
    }
}

/// A group that accounts can be sorted into.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountGroup {
    /// The row id.
    pub id: AccountGroupId,
    /// The display name.
    pub name: String,
    /// The position in account lists.
    pub sort_order: i64,
}

/// A connection to a bank.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BankConnection {
    /// The row id.
    pub id: BankConnectionId,
    /// The bank's name.
    pub bank_name: String,
    /// The German bank code.
    pub blz: String,
    /// The BIC, when known.
    pub bic: Option<String>,
    /// The bank's user id.
    pub user_id: String,
    /// The FinTS server URL.
    pub server_url: Option<String>,
    /// The TAN method.
    pub tan_method: Option<String>,
    /// The first date to sync.
    pub sync_from: Option<Date>,
    /// The last successful sync.
    pub last_sync: Option<OffsetDateTime>,
    /// The bank's advertised capabilities.
    pub capabilities: Option<String>,
    /// Whether the connection is part of "sync all".
    pub include_in_sync_all: bool,
}

/// A bank account.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Account {
    /// The row id.
    pub id: AccountId,
    /// The bank connection, when this is an online account.
    pub bank_connection_id: Option<BankConnectionId>,
    /// The institution name.
    pub institution: Option<String>,
    /// The display name.
    pub name: Option<String>,
    /// The IBAN.
    pub iban: Option<String>,
    /// The account number.
    pub account_number: Option<String>,
    /// The account currency.
    pub currency: Currency,
    /// How the account is used.
    pub account_type: AccountType,
    /// The latest known balance.
    pub balance: Money,
    /// When the balance was last updated.
    pub balance_updated_at: Option<OffsetDateTime>,
    /// The account group.
    pub group_id: Option<AccountGroupId>,
}

/// A spending category.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Category {
    /// The row id.
    pub id: CategoryId,
    /// The display name.
    pub name: String,
    /// The display color.
    pub color: String,
    /// The optional monthly budget.
    ///
    /// The schema stores only minor units, so the base currency is used.
    pub budget_limit: Option<Money>,
    /// Whether bookings in this category are tax relevant.
    pub is_tax_relevant: bool,
    /// Whether this category marks transfers.
    pub is_transfer: bool,
}

/// A categorization rule.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Rule {
    /// The row id.
    pub id: RuleId,
    /// The text pattern to match.
    pub pattern: String,
    /// The category to assign.
    pub category_id: CategoryId,
    /// The priority, highest first.
    pub priority: i64,
    /// Whether matching bookings are categorized automatically.
    pub auto_apply: bool,
    /// The field the pattern is matched against.
    pub match_field: RuleField,
    /// The booking direction the rule applies to.
    pub direction: RuleDirection,
    /// Whether matching bookings are tax relevant, when set.
    pub tax_relevant: Option<bool>,
}

/// A booked transaction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Transaction {
    /// The row id.
    pub id: TransactionId,
    /// The account the booking belongs to.
    pub account_id: AccountId,
    /// The booking date.
    pub booking_date: Date,
    /// The signed amount.
    pub amount: Money,
    /// The balance after the booking, when known.
    pub balance_after: Option<Money>,
    /// The other party.
    pub partner_name: Option<String>,
    /// The purpose text.
    pub purpose: Option<String>,
    /// The assigned category.
    pub category_id: Option<CategoryId>,
    /// The hash that identifies the booking for imports.
    pub import_hash: String,
    /// When the row was created.
    pub created_at: OffsetDateTime,
    /// Whether the booking is still new to the user.
    pub is_new: bool,
    /// An explicit tax flag, overriding the category's.
    pub tax_override: Option<bool>,
}

/// A booking that was deleted and must not be imported again.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DeletedBooking {
    /// The row id.
    pub id: i64,
    /// The account the booking belonged to.
    pub account_id: AccountId,
    /// The import hash of the deleted booking.
    pub import_hash: String,
    /// When it was deleted.
    pub deleted_at: OffsetDateTime,
}

/// A pending (not yet booked) transaction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PendingBooking {
    /// The row id.
    pub id: i64,
    /// The account the booking belongs to.
    pub account_id: AccountId,
    /// The booking date.
    pub booking_date: Date,
    /// The value date, when known.
    pub value_date: Option<Date>,
    /// The signed amount.
    pub amount: Money,
    /// The other party.
    pub partner_name: Option<String>,
    /// The purpose text.
    pub purpose: Option<String>,
    /// When the row was fetched.
    pub fetched_at: OffsetDateTime,
}

/// A stored document.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Document {
    /// The row id.
    pub id: i64,
    /// The original file name.
    pub file_name: String,
    /// The path relative to the data folder.
    pub stored_path: String,
    /// The MIME type.
    pub mime_type: String,
    /// When the document was uploaded.
    pub uploaded_at: OffsetDateTime,
    /// The linked transaction.
    pub transaction_id: Option<TransactionId>,
}

/// Text extracted from a document.
#[derive(Debug, Clone, PartialEq)]
pub struct OcrText {
    /// The row id.
    pub id: i64,
    /// The document it belongs to.
    pub document_id: i64,
    /// The extracted text.
    pub content: String,
    /// The OCR language.
    pub language: String,
    /// The OCR confidence, when known.
    pub confidence: Option<f64>,
}

/// The values needed to create an account.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewAccount {
    /// The bank connection, when this is an online account.
    pub bank_connection_id: Option<BankConnectionId>,
    /// The institution name.
    pub institution: Option<String>,
    /// The display name.
    pub name: Option<String>,
    /// The IBAN.
    pub iban: Option<String>,
    /// The account number.
    pub account_number: Option<String>,
    /// The account currency.
    pub currency: Currency,
    /// How the account is used.
    pub account_type: AccountType,
    /// The starting balance, in the account's currency.
    pub balance: Money,
    /// When the balance was last updated.
    pub balance_updated_at: Option<OffsetDateTime>,
    /// The account group.
    pub group_id: Option<AccountGroupId>,
}

/// The values needed to create an account group.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewAccountGroup {
    /// The display name.
    pub name: String,
    /// The position in account lists.
    pub sort_order: i64,
}

/// The values needed to create or update a category.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewCategory {
    /// The display name.
    pub name: String,
    /// The display color.
    pub color: String,
    /// The optional monthly budget, in the base currency.
    pub budget_limit: Option<Money>,
    /// Whether bookings in this category are tax relevant.
    pub is_tax_relevant: bool,
    /// Whether this category marks transfers.
    pub is_transfer: bool,
}

/// The values needed to create or update a rule.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewRule {
    /// The text pattern to match.
    pub pattern: String,
    /// The category to assign.
    pub category_id: CategoryId,
    /// The priority, highest first.
    pub priority: i64,
    /// Whether matching bookings are categorized automatically.
    pub auto_apply: bool,
    /// The field the pattern is matched against.
    pub match_field: RuleField,
    /// The booking direction the rule applies to.
    pub direction: RuleDirection,
    /// Whether matching bookings are tax relevant, when set.
    pub tax_relevant: Option<bool>,
}

/// The values needed to import one transaction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NewTransaction {
    /// The booking date.
    pub booking_date: Date,
    /// The signed amount, in the account's currency.
    pub amount: Money,
    /// The balance after the booking, when known.
    pub balance_after: Option<Money>,
    /// The other party.
    pub partner_name: Option<String>,
    /// The purpose text.
    pub purpose: Option<String>,
    /// The category to assign.
    pub category_id: Option<CategoryId>,
    /// The hash that identifies the booking for imports.
    pub import_hash: String,
    /// When the row is created.
    pub created_at: OffsetDateTime,
    /// Whether the booking is still new to the user.
    pub is_new: bool,
    /// An explicit tax flag, overriding the category's.
    pub tax_override: Option<bool>,
}
