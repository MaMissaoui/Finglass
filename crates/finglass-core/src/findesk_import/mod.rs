//! One-time import of a FinDesk profile into an empty Finglass profile.
//!
//! The import copies the FinDesk database into a temporary folder first and opens that copy read-only. It never writes
//! to the selected FinDesk folder. All target inserts run in one transaction with one audit entry; copied document
//! files are removed again when the transaction fails.
//!
//! FinDesk's payment orders are never imported. The only access to that table is a row count for the report.

mod convert;
mod import;
mod source;

use std::io;
use std::path::Path;

use thiserror::Error;
use time::UtcOffset;

use crate::db::{Database, DbError};
use crate::i18n::{self, Arg, Key, Language};
use crate::secret::Passphrase;

/// How many rows of one source table were imported or skipped.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct ImportCounts {
    /// Rows inserted into the target.
    pub imported: usize,
    /// Rows skipped for any reason.
    pub skipped: usize,
    /// Rows skipped because a referenced account, category or document does not exist.
    pub skipped_orphan: usize,
    /// Rows skipped because a value is missing or fails a Finglass rule.
    pub skipped_invalid: usize,
    /// Rows skipped because a monetary value is not whole at the currency's exponent.
    pub skipped_fractional_amount: usize,
}

impl ImportCounts {
    fn record_import(&mut self) {
        self.imported += 1;
    }

    fn record_orphan(&mut self) {
        self.skipped += 1;
        self.skipped_orphan += 1;
    }

    fn record_invalid(&mut self) {
        self.skipped += 1;
        self.skipped_invalid += 1;
    }

    fn record_fractional(&mut self) {
        self.skipped += 1;
        self.skipped_fractional_amount += 1;
    }
}

/// How many documents were copied, and why the others were left behind.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct DocumentImportCounts {
    /// Documents whose file was copied and whose row was inserted.
    pub copied: usize,
    /// Documents skipped for any reason.
    pub skipped: usize,
    /// Documents whose file was not found inside the FinDesk documents folder.
    pub missing: usize,
    /// Documents whose file is larger than [`crate::documents::limits::MAX_DOCUMENT_BYTES`].
    pub refused: usize,
    /// Documents skipped because their row could not be converted.
    pub skipped_invalid: usize,
}

/// Settings imported or dropped by the allow-list.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct SettingsImportCounts {
    /// Settings written to the target.
    pub imported: usize,
    /// Settings dropped because they are not on the allow-list.
    pub skipped: usize,
}

/// What one FinDesk import did, as counts only.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct FinDeskImportReport {
    /// Account groups.
    pub account_groups: ImportCounts,
    /// Bank connections (login state and capabilities are not copied).
    pub bank_connections: ImportCounts,
    /// Accounts.
    pub accounts: ImportCounts,
    /// Categories (existing Finglass categories are replaced).
    pub categories: ImportCounts,
    /// Rules.
    pub rules: ImportCounts,
    /// Transactions.
    pub transactions: ImportCounts,
    /// Deleted bookings remembered for duplicate detection.
    pub deleted_bookings: ImportCounts,
    /// Pending bookings.
    pub pending_bookings: ImportCounts,
    /// Documents and their files.
    pub documents: DocumentImportCounts,
    /// OCR texts.
    pub ocr_texts: ImportCounts,
    /// Settings kept by the allow-list.
    pub settings: SettingsImportCounts,
    /// FinDesk payment orders that were deliberately left behind.
    pub payment_orders_left_behind: usize,
}

impl FinDeskImportReport {
    /// Rows skipped across all imported tables, documents and settings.
    #[must_use]
    pub fn total_skipped(&self) -> usize {
        [
            self.account_groups.skipped,
            self.bank_connections.skipped,
            self.accounts.skipped,
            self.categories.skipped,
            self.rules.skipped,
            self.transactions.skipped,
            self.deleted_bookings.skipped,
            self.pending_bookings.skipped,
            self.documents.skipped,
            self.ocr_texts.skipped,
            self.settings.skipped,
        ]
        .into_iter()
        .sum()
    }

    /// A one-line, user-visible summary in `language`.
    #[must_use]
    pub fn summary(&self, language: Language) -> String {
        i18n::format(
            language,
            Key::FinDeskImportSummary,
            &[
                Arg::from(self.account_groups.imported),
                Arg::from(self.bank_connections.imported),
                Arg::from(self.accounts.imported),
                Arg::from(self.categories.imported),
                Arg::from(self.rules.imported),
                Arg::from(self.transactions.imported),
                Arg::from(self.deleted_bookings.imported),
                Arg::from(self.pending_bookings.imported),
                Arg::from(self.documents.copied),
                Arg::from(self.ocr_texts.imported),
                Arg::from(self.settings.imported),
                Arg::from(self.total_skipped()),
                Arg::from(self.documents.missing),
                Arg::from(self.documents.refused),
                Arg::from(self.payment_orders_left_behind),
            ],
        )
    }
}

/// Everything that can go wrong while importing a FinDesk profile.
#[derive(Debug, Error)]
pub enum FinDeskImportError {
    /// No `findesk.db` exists in the selected folder.
    #[error("{}", message(Language::En, Key::ErrorFinDeskNotFound))]
    NotFound,
    /// The FinDesk passphrase does not open the database.
    #[error("{}", message(Language::En, Key::ErrorFinDeskWrongPassphrase))]
    WrongPassphrase,
    /// The database does not hold FinDesk's tables.
    #[error("{}", message(Language::En, Key::ErrorFinDeskNotFinDesk))]
    NotFinDesk,
    /// The target profile already holds data.
    #[error("{}", message(Language::En, Key::ErrorFinDeskTargetNotEmpty))]
    TargetNotEmpty,
    /// A database operation failed.
    #[error(transparent)]
    Db(#[from] DbError),
    /// A file operation failed.
    #[error(transparent)]
    Io(#[from] io::Error),
}

impl FinDeskImportError {
    /// The user text in `language`. Technical failures keep their English description.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        match self {
            Self::NotFound => message(language, Key::ErrorFinDeskNotFound),
            Self::WrongPassphrase => message(language, Key::ErrorFinDeskWrongPassphrase),
            Self::NotFinDesk => message(language, Key::ErrorFinDeskNotFinDesk),
            Self::TargetNotEmpty => message(language, Key::ErrorFinDeskTargetNotEmpty),
            Self::Db(error) => error.to_string(),
            Self::Io(error) => error.to_string(),
        }
    }
}

fn message(language: Language, key: Key) -> String {
    i18n::format(language, key, &[])
}

/// Imports a FinDesk profile into an empty Finglass profile database.
///
/// `target_documents_dir` is `<Finglass profile>/documents`; `findesk_data_dir` is the folder holding FinDesk's
/// `findesk.db` and `documents/`. `offset` is used for FinDesk's zone-less dates and datetimes.
pub fn import_findesk(
    target: &Database,
    target_documents_dir: &Path,
    findesk_data_dir: &Path,
    passphrase: &Passphrase,
    offset: UtcOffset,
) -> Result<FinDeskImportReport, FinDeskImportError> {
    import::run(target, target_documents_dir, findesk_data_dir, passphrase, offset)
}

#[cfg(test)]
mod tests;
