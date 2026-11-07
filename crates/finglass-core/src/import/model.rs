//! Data model shared by every statement parser.

use rust_decimal::Decimal;
use time::Date;

/// One booking read from a statement file, before it is stored.
#[derive(Debug, Clone, PartialEq)]
pub struct ImportedBooking {
    pub date: Date,
    pub value_date: Option<Date>,
    pub amount: Decimal,
    /// ISO code, or `None` when the file doesn't say (the booking then takes the account's currency).
    pub currency: Option<String>,
    pub partner_name: Option<String>,
    pub partner_iban: Option<String>,
    pub purpose: Option<String>,
    /// Running balance after the booking, if the file has it.
    pub balance_after: Option<Decimal>,
    /// Bank reference (e.g. CAMT AcctSvcrRef), if any.
    pub bank_reference: Option<String>,
}

/// The statement formats Finglass can read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StatementFormat {
    Csv,
    Camt053,
    Mt940,
    Ofx,
    Qif,
    Xlsx,
}

impl StatementFormat {
    /// The format's display label.
    pub fn label(self) -> &'static str {
        match self {
            Self::Csv => "CSV",
            Self::Camt053 => "CAMT XML",
            Self::Mt940 => "MT940",
            Self::Ofx => "OFX",
            Self::Qif => "QIF",
            Self::Xlsx => "XLSX",
        }
    }
}

/// The statement's closing balance, already checked against its entries.
#[derive(Debug, Clone, PartialEq)]
pub struct ClosingBalance {
    pub amount: Decimal,
    pub date: Date,
    /// ISO code from the balance, or `None` when the file doesn't say.
    pub currency: Option<String>,
    /// Number of pending entries whose sum was taken out of the bank's figure (0 when none).
    pub pending_excluded: u32,
}

/// Result of reading a file: bookings plus what could be detected about it.
#[derive(Debug, Clone, PartialEq)]
pub struct ParsedStatement {
    pub format: StatementFormat,
    pub bookings: Vec<ImportedBooking>,
    /// IBAN of the statement's own account, if the file names it.
    pub account_iban: Option<String>,
    /// Distinct accounts the file's statements belong to (IBAN, or the raw account id when it isn't
    /// one), in file order. More than one is refused by [`super::read`].
    pub accounts: Vec<String>,
    /// Rows that couldn't be read, with the reason (1-based line/entry number).
    pub skipped: Vec<String>,
    /// Pending (not yet booked) entries that were left out; they'd be imported twice once booked.
    pub pending_skipped: u32,
    /// CSV only: header and detected mapping, so the user can adjust it.
    pub csv: Option<CsvLayout>,
    /// Message type for display, e.g. "CAMT.052" (`None` when the format has no variants).
    pub format_detail: Option<String>,
    /// CAMT only: the report's closing balance when it matches the file's entries.
    pub closing_balance: Option<ClosingBalance>,
}

impl ParsedStatement {
    /// Format label for the preview and the audit log, e.g. "CAMT.052 XML" or "CSV".
    pub fn format_label(&self) -> String {
        match &self.format_detail {
            Some(detail) => format!("{detail} XML"),
            None => self.format.label().to_owned(),
        }
    }
}

/// Which CSV column holds which field (indexes into the headers).
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct CsvMapping {
    pub date: Option<usize>,
    pub value_date: Option<usize>,
    pub amount: Option<usize>,
    /// Some banks split amounts into debit/credit columns instead of a signed amount.
    pub debit: Option<usize>,
    pub credit: Option<usize>,
    pub partner: Option<usize>,
    /// Separate payer column (DKB): used as partner for incoming money, `partner` for outgoing.
    pub payer: Option<usize>,
    pub partner_iban: Option<usize>,
    pub purpose: Option<usize>,
    pub currency: Option<usize>,
    pub balance: Option<usize>,
}

impl CsvMapping {
    /// Whether the mapping names a date and at least one amount column.
    pub fn is_complete(&self) -> bool {
        self.date.is_some() && (self.amount.is_some() || self.debit.is_some() || self.credit.is_some())
    }
}

/// How a CSV file was laid out.
#[derive(Debug, Clone, PartialEq)]
pub struct CsvLayout {
    pub delimiter: char,
    pub charset_name: String,
    pub header_line: usize,
    pub headers: Vec<String>,
    pub mapping: CsvMapping,
}

/// Errors that stop a whole file from being read.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum ImportError {
    /// The file cannot be read as the expected format.
    #[error("{0}")]
    Invalid(String),
    /// The file is larger than the import limit.
    #[error("File is too large")]
    FileTooLarge,
    /// The XLSX package exceeds the size, entry or cell limits.
    #[error("The Excel file is too large or damaged.")]
    XlsxTooLarge,
    /// The XLSX package is malformed or unreadable.
    #[error("The Excel file could not be read.")]
    XlsxUnreadable,
}

impl ImportError {
    pub(crate) fn invalid(message: impl Into<String>) -> Self {
        Self::Invalid(message.into())
    }
}
