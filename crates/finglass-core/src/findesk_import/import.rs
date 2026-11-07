//! The transactional part of the FinDesk import.
//!
//! Every insert runs through one target transaction. Rows that FinDesk allowed but Finglass rejects are skipped and
//! counted; none of them aborts the import.

use std::cell::RefCell;
use std::collections::HashSet;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use rusqlite::{Connection, params};
use time::UtcOffset;
use uuid::Uuid;

use crate::app::prefs::THEME_KEY;
use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::documents::limits::MAX_DOCUMENT_BYTES;
use crate::documents::service::sanitize_file_name;
use crate::model::{AccountType, RuleDirection, RuleField};
use crate::money::Currency;
use crate::paths::AppPaths;
use crate::secret::Passphrase;

use super::convert::{self, ConvertError};
use super::source::{
    AccountRow, BankAccessRow, CategoryRow, DeletedBookingRow, DocumentRow, FinDeskSource, OcrTextRow,
    PendingBookingRow, RuleRow, SettingRow, SourceData, TransactionRow,
};
use super::{FinDeskImportError, FinDeskImportReport};

/// Settings copied verbatim. Everything else is dropped; `ui.dark_theme` is translated separately.
const SETTINGS_ALLOWLIST: &[&str] = &[
    "money.primary_currency",
    "tax.suggestionKeywords",
    "transactions.selection",
    "accountGroups.collapsed",
    "accounts.collapsedBanks",
    "transactions.chartCollapsed",
    "ui.sidebarCollapsed",
];

const TARGET_TABLES: &str = "accounts, bank_connections, account_groups, rules, transactions, documents";

pub(crate) fn run(
    target: &Database,
    target_documents_dir: &Path,
    findesk_data_dir: &Path,
    passphrase: &Passphrase,
    offset: UtcOffset,
) -> Result<FinDeskImportReport, FinDeskImportError> {
    ensure_target_empty(target)?;

    let source = FinDeskSource::open(findesk_data_dir, passphrase)?;
    let data = source.read()?;

    // `with_conn` only passes database errors through. Keep the useful import error from inside the transaction.
    let failure = RefCell::new(None);
    let outcome = target.with_conn(|conn| {
        match import_in_connection(conn, &data, findesk_data_dir, target_documents_dir, offset) {
            Ok(report) => Ok(report),
            Err(error) => {
                *failure.borrow_mut() = Some(error);
                Err(DbError::Io(io::Error::other("FinDesk import failed")))
            }
        }
    });

    match outcome {
        Ok(report) => Ok(report),
        Err(error) => Err(failure.into_inner().unwrap_or(FinDeskImportError::Db(error))),
    }
}

fn ensure_target_empty(target: &Database) -> Result<(), FinDeskImportError> {
    let sql = format!(
        "SELECT {}",
        TARGET_TABLES
            .split(',')
            .map(|table| format!("(SELECT count(*) FROM {table})"))
            .collect::<Vec<_>>()
            .join(" + ")
    );

    let count: i64 = target
        .with_conn(|conn| conn.query_row(&sql, [], |row| row.get(0)).map_err(DbError::from))
        .map_err(FinDeskImportError::Db)?;

    if count > 0 {
        return Err(FinDeskImportError::TargetNotEmpty);
    }

    Ok(())
}

fn import_in_connection(
    conn: &Connection,
    data: &SourceData,
    findesk_data_dir: &Path,
    target_documents_dir: &Path,
    offset: UtcOffset,
) -> Result<FinDeskImportReport, FinDeskImportError> {
    let mut report = FinDeskImportReport {
        payment_orders_left_behind: data.payment_orders,
        ..FinDeskImportReport::default()
    };
    let mut copied = CopiedFiles::default();
    let transaction = conn.unchecked_transaction().map_err(db_error)?;

    // Seeded categories are replaced, not merged.
    transaction.execute("DELETE FROM categories", []).map_err(db_error)?;

    let primary_currency = primary_currency(&data.settings);
    let mut imported_groups = HashSet::new();
    let mut imported_banks = HashSet::new();
    let mut imported_accounts = HashSet::new();
    let mut imported_categories = HashSet::new();
    let mut imported_transactions = HashSet::new();
    let mut copied_documents = HashSet::new();

    for row in &data.account_groups {
        let (Some(name), Some(sort_order)) = (row.name.as_deref(), row.sort_order) else {
            report.account_groups.record_invalid();
            continue;
        };

        transaction
            .execute(
                "INSERT INTO account_groups (id, name, sort_order) VALUES (?1, ?2, ?3)",
                params![row.id, name, sort_order],
            )
            .map_err(db_error)?;
        imported_groups.insert(row.id);
        report.account_groups.record_import();
    }

    for row in &data.bank_access {
        let values = match bank_values(row, offset) {
            Ok(values) => values,
            Err(_) => {
                report.bank_connections.record_invalid();
                continue;
            }
        };

        transaction
            .execute(
                "INSERT INTO bank_connections (
                    id, bank_name, blz, bic, user_id, server_url, tan_method, sync_from, last_sync,
                    capabilities, include_in_sync_all, offline
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, NULL, ?10, ?11)",
                params![
                    row.id,
                    values.bank_name,
                    values.blz,
                    values.bic,
                    values.user_id,
                    values.server_url,
                    values.tan_method,
                    values.sync_from,
                    values.last_sync,
                    i64::from(values.include_in_sync_all),
                    i64::from(values.offline),
                ],
            )
            .map_err(db_error)?;
        imported_banks.insert(row.id);
        report.bank_connections.record_import();
    }

    for row in &data.accounts {
        let values = match account_values(row, offset) {
            Ok(values) => values,
            Err(_) => {
                report.accounts.record_invalid();
                continue;
            }
        };

        let bank_connection_id = values.bank_connection_id.filter(|id| imported_banks.contains(id));
        let group_id = values.group_id.filter(|id| imported_groups.contains(id));

        transaction
            .execute(
                "INSERT INTO accounts (
                    id, bank_connection_id, institution, name, iban, account_number, currency, account_type,
                    balance_minor, balance_updated_at, group_id
                 ) VALUES (?1, ?2, NULL, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
                params![
                    row.id,
                    bank_connection_id,
                    values.name,
                    values.iban,
                    values.account_number,
                    values.currency.as_str(),
                    values.account_type.as_str(),
                    values.balance_minor,
                    values.balance_updated_at,
                    group_id,
                ],
            )
            .map_err(db_error)?;
        imported_accounts.insert(row.id);
        report.accounts.record_import();
    }

    for row in &data.categories {
        let values = match category_values(row, primary_currency) {
            Ok(values) => values,
            Err(ConvertError::FractionalAmount) => {
                report.categories.record_fractional();
                continue;
            }
            Err(ConvertError::Invalid) => {
                report.categories.record_invalid();
                continue;
            }
        };

        transaction
            .execute(
                "INSERT INTO categories (
                    id, name, color, budget_limit_minor, is_tax_relevant, is_transfer
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params![
                    row.id,
                    values.name,
                    values.color,
                    values.budget_limit_minor,
                    i64::from(values.is_tax_relevant),
                    i64::from(values.is_transfer),
                ],
            )
            .map_err(db_error)?;
        imported_categories.insert(row.id);
        report.categories.record_import();
    }

    for row in &data.rules {
        let values = match rule_values(row) {
            Ok(values) => values,
            Err(_) => {
                report.rules.record_invalid();
                continue;
            }
        };

        if !imported_categories.contains(&values.category_id) {
            report.rules.record_orphan();
            continue;
        }

        transaction
            .execute(
                "INSERT INTO rules (
                    id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    row.id,
                    values.pattern,
                    values.category_id,
                    values.priority,
                    i64::from(values.auto_apply),
                    values.match_field.as_str(),
                    values.direction.as_str(),
                    values.tax_relevant.map(i64::from),
                ],
            )
            .map_err(db_error)?;
        report.rules.record_import();
    }

    for row in &data.transactions {
        let values = match transaction_values(row, offset) {
            Ok(values) => values,
            Err(ConvertError::FractionalAmount) => {
                report.transactions.record_fractional();
                continue;
            }
            Err(ConvertError::Invalid) => {
                report.transactions.record_invalid();
                continue;
            }
        };

        if !imported_accounts.contains(&values.account_id) {
            report.transactions.record_orphan();
            continue;
        }

        let category_id = values.category_id.filter(|id| imported_categories.contains(id));

        transaction
            .execute(
                "INSERT INTO transactions (
                    id, account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name,
                    purpose, category_id, import_hash, created_at, is_new, tax_override
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13)",
                params![
                    row.id,
                    values.account_id,
                    values.booking_date,
                    values.amount_minor,
                    values.currency.as_str(),
                    values.balance_after_minor,
                    values.partner_name,
                    values.purpose,
                    category_id,
                    values.import_hash,
                    values.created_at,
                    i64::from(values.is_new),
                    values.tax_override.map(i64::from),
                ],
            )
            .map_err(db_error)?;
        imported_transactions.insert(row.id);
        report.transactions.record_import();
    }

    for row in &data.deleted_bookings {
        let values = match deleted_values(row, offset) {
            Ok(values) => values,
            Err(_) => {
                report.deleted_bookings.record_invalid();
                continue;
            }
        };

        if !imported_accounts.contains(&values.account_id) {
            report.deleted_bookings.record_orphan();
            continue;
        }

        transaction
            .execute(
                "INSERT INTO deleted_bookings (id, account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3, ?4)",
                params![row.id, values.account_id, values.import_hash, values.deleted_at],
            )
            .map_err(db_error)?;
        report.deleted_bookings.record_import();
    }

    for row in &data.pending_bookings {
        let values = match pending_values(row, offset) {
            Ok(values) => values,
            Err(ConvertError::FractionalAmount) => {
                report.pending_bookings.record_fractional();
                continue;
            }
            Err(ConvertError::Invalid) => {
                report.pending_bookings.record_invalid();
                continue;
            }
        };

        if !imported_accounts.contains(&values.account_id) {
            report.pending_bookings.record_orphan();
            continue;
        }

        transaction
            .execute(
                "INSERT INTO pending_bookings (
                    id, account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose,
                    fetched_at
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)",
                params![
                    row.id,
                    values.account_id,
                    values.booking_date,
                    values.value_date,
                    values.amount_minor,
                    values.currency.as_str(),
                    values.partner_name,
                    values.purpose,
                    values.fetched_at,
                ],
            )
            .map_err(db_error)?;
        report.pending_bookings.record_import();
    }

    let documents_root = findesk_data_dir.join("documents").canonicalize().ok();

    for row in &data.documents {
        let values = match document_values(row, offset) {
            Ok(values) => values,
            Err(_) => {
                report.documents.skipped += 1;
                report.documents.skipped_invalid += 1;
                continue;
            }
        };

        let file = match resolve_document_file(documents_root.as_deref(), row.file_path.as_deref()) {
            DocumentFile::Found(file) => file,
            DocumentFile::Missing => {
                report.documents.skipped += 1;
                report.documents.missing += 1;
                continue;
            }
            DocumentFile::Refused => {
                report.documents.skipped += 1;
                report.documents.refused += 1;
                continue;
            }
        };

        AppPaths::ensure_dir(target_documents_dir)?;
        let destination =
            target_documents_dir.join(format!("{}_{}", Uuid::new_v4(), sanitize_file_name(&values.file_name)));
        fs::copy(&file, &destination)?;
        copied.paths.push(destination.clone());
        secure_file(&destination)?;

        let transaction_id = values
            .linked_transaction_id
            .filter(|id| imported_transactions.contains(id));

        transaction
            .execute(
                "INSERT INTO documents (
                    id, file_name, stored_path, mime_type, uploaded_at, transaction_id
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params![
                    row.id,
                    values.file_name,
                    destination.to_string_lossy().into_owned(),
                    values.mime_type,
                    values.uploaded_at,
                    transaction_id,
                ],
            )
            .map_err(db_error)?;
        copied_documents.insert(row.id);
        report.documents.copied += 1;
    }

    for row in &data.ocr_texts {
        let values = match ocr_values(row) {
            Ok(values) => values,
            Err(_) => {
                report.ocr_texts.record_invalid();
                continue;
            }
        };

        let document_id = match values.document_id {
            Some(id) if copied_documents.contains(&id) => id,
            _ => {
                report.ocr_texts.record_orphan();
                continue;
            }
        };

        transaction
            .execute(
                "INSERT INTO ocr_texts (id, document_id, content, language, confidence)
                 VALUES (?1, ?2, ?3, ?4, ?5)",
                params![row.id, document_id, values.content, values.language, values.confidence,],
            )
            .map_err(db_error)?;
        report.ocr_texts.record_import();
    }

    for row in &data.settings {
        let (Some(key), Some(value)) = (row.key.as_deref(), row.value.as_deref()) else {
            report.settings.skipped += 1;
            continue;
        };

        let target = if key == "ui.dark_theme" {
            match convert::theme(value) {
                Some(theme) => (THEME_KEY, theme),
                None => {
                    report.settings.skipped += 1;
                    continue;
                }
            }
        } else if SETTINGS_ALLOWLIST.contains(&key) {
            (key, value)
        } else {
            report.settings.skipped += 1;
            continue;
        };

        transaction
            .execute(
                "INSERT INTO settings (key, value) VALUES (?1, ?2)
                 ON CONFLICT (key) DO UPDATE SET value = excluded.value",
                params![target.0, target.1],
            )
            .map_err(db_error)?;
        report.settings.imported += 1;
    }

    let details = format!(
        "account_groups={} bank_connections={} accounts={} categories={} rules={} transactions={} \
         deleted_bookings={} pending_bookings={} documents={} ocr_texts={} settings={} skipped={} \
         documents_missing={} documents_refused={} payment_orders_left={}",
        report.account_groups.imported,
        report.bank_connections.imported,
        report.accounts.imported,
        report.categories.imported,
        report.rules.imported,
        report.transactions.imported,
        report.deleted_bookings.imported,
        report.pending_bookings.imported,
        report.documents.copied,
        report.ocr_texts.imported,
        report.settings.imported,
        report.total_skipped(),
        report.documents.missing,
        report.documents.refused,
        report.payment_orders_left_behind,
    );
    audit::log_conn(&transaction, "FINDESK_IMPORTED", Actor::Import, &details)?;

    transaction.commit().map_err(db_error)?;
    copied.keep();

    Ok(report)
}

fn primary_currency(settings: &[SettingRow]) -> Currency {
    settings
        .iter()
        .find(|row| row.key.as_deref() == Some("money.primary_currency"))
        .and_then(|row| row.value.as_deref())
        .and_then(|value| Currency::new(value.trim()).ok())
        .unwrap_or(Currency::EUR)
}

struct BankValues {
    bank_name: String,
    blz: String,
    bic: Option<String>,
    user_id: String,
    tan_method: Option<String>,
    server_url: Option<String>,
    sync_from: Option<String>,
    last_sync: Option<String>,
    include_in_sync_all: bool,
    offline: bool,
}

fn bank_values(row: &BankAccessRow, offset: UtcOffset) -> Result<BankValues, ConvertError> {
    Ok(BankValues {
        bank_name: row.bank_name.clone().ok_or(ConvertError::Invalid)?,
        blz: row.blz.clone().ok_or(ConvertError::Invalid)?,
        bic: row.bic.clone(),
        user_id: row.user_id.clone().ok_or(ConvertError::Invalid)?,
        tan_method: row.tan_method.clone(),
        server_url: row.server_url.clone(),
        sync_from: optional_date_text(row.sync_from.as_deref())?,
        last_sync: convert::optional_timestamp(row.last_sync.as_deref(), offset)?,
        include_in_sync_all: convert::required_bool(row.include_in_sync_all)?,
        offline: convert::required_bool(row.offline)?,
    })
}

struct AccountValues {
    bank_connection_id: Option<i64>,
    name: Option<String>,
    iban: Option<String>,
    account_number: Option<String>,
    currency: Currency,
    account_type: AccountType,
    balance_minor: i64,
    balance_updated_at: Option<String>,
    group_id: Option<i64>,
}

fn account_values(row: &AccountRow, offset: UtcOffset) -> Result<AccountValues, ConvertError> {
    let currency = convert::currency(row.currency.as_deref())?;

    Ok(AccountValues {
        bank_connection_id: row.bank_access_id,
        name: row.name.clone(),
        iban: row.iban.clone(),
        account_number: row.account_number.clone(),
        currency,
        account_type: convert::account_type(row.account_type.as_deref())?,
        balance_minor: convert::required_minor(row.balance.as_ref(), currency)?,
        balance_updated_at: convert::optional_timestamp(row.last_update.as_deref(), offset)?,
        group_id: row.group_id,
    })
}

struct CategoryValues {
    name: String,
    color: String,
    budget_limit_minor: Option<i64>,
    is_tax_relevant: bool,
    is_transfer: bool,
}

fn category_values(row: &CategoryRow, currency: Currency) -> Result<CategoryValues, ConvertError> {
    Ok(CategoryValues {
        name: row.name.clone().ok_or(ConvertError::Invalid)?,
        color: row.color.clone().ok_or(ConvertError::Invalid)?,
        // A budget that isn't whole at the primary currency's exponent is dropped, never the category itself
        // (that would also drop its rules and uncategorise its bookings).
        budget_limit_minor: convert::optional_minor(row.budget_limit.as_ref(), currency).unwrap_or(None),
        is_tax_relevant: convert::required_bool(row.is_tax_relevant)?,
        is_transfer: convert::required_bool(row.is_transfer)?,
    })
}

struct RuleValues {
    pattern: String,
    category_id: i64,
    priority: i64,
    auto_apply: bool,
    match_field: RuleField,
    direction: RuleDirection,
    tax_relevant: Option<bool>,
}

fn rule_values(row: &RuleRow) -> Result<RuleValues, ConvertError> {
    Ok(RuleValues {
        pattern: row.pattern.clone().ok_or(ConvertError::Invalid)?,
        category_id: row.category_id.ok_or(ConvertError::Invalid)?,
        priority: row.priority.ok_or(ConvertError::Invalid)?,
        auto_apply: convert::required_bool(row.auto_apply)?,
        match_field: convert::rule_field(row.match_field.as_deref())?,
        direction: convert::rule_direction(row.direction.as_deref())?,
        tax_relevant: convert::optional_bool(row.tax_relevant)?,
    })
}

struct TransactionValues {
    account_id: i64,
    booking_date: String,
    amount_minor: i64,
    currency: Currency,
    balance_after_minor: Option<i64>,
    partner_name: Option<String>,
    purpose: Option<String>,
    category_id: Option<i64>,
    import_hash: String,
    created_at: String,
    is_new: bool,
    tax_override: Option<bool>,
}

fn transaction_values(row: &TransactionRow, offset: UtcOffset) -> Result<TransactionValues, ConvertError> {
    let currency = convert::currency(row.currency.as_deref())?;
    let date = convert::required_date(row.date.as_deref())?;

    Ok(TransactionValues {
        account_id: row.account_id.ok_or(ConvertError::Invalid)?,
        booking_date: convert::date_text(date)?,
        amount_minor: convert::required_minor(row.amount.as_ref(), currency)?,
        currency,
        balance_after_minor: convert::optional_minor(row.balance_after.as_ref(), currency)?,
        partner_name: row.partner_name.clone(),
        purpose: row.purpose.clone(),
        category_id: row.category_id,
        import_hash: row.hash.clone().ok_or(ConvertError::Invalid)?,
        created_at: convert::required_timestamp(row.created_at.as_deref(), offset)?,
        is_new: convert::required_bool(row.is_new)?,
        tax_override: convert::optional_bool(row.tax_override)?,
    })
}

struct DeletedValues {
    account_id: i64,
    import_hash: String,
    deleted_at: String,
}

fn deleted_values(row: &DeletedBookingRow, offset: UtcOffset) -> Result<DeletedValues, ConvertError> {
    Ok(DeletedValues {
        account_id: row.account_id.ok_or(ConvertError::Invalid)?,
        import_hash: row.hash.clone().ok_or(ConvertError::Invalid)?,
        deleted_at: convert::required_timestamp(row.deleted_at.as_deref(), offset)?,
    })
}

struct PendingValues {
    account_id: i64,
    booking_date: String,
    value_date: Option<String>,
    amount_minor: i64,
    currency: Currency,
    partner_name: Option<String>,
    purpose: Option<String>,
    fetched_at: String,
}

fn pending_values(row: &PendingBookingRow, offset: UtcOffset) -> Result<PendingValues, ConvertError> {
    let currency = convert::currency(row.currency.as_deref())?;
    let date = convert::required_date(row.date.as_deref())?;

    Ok(PendingValues {
        account_id: row.account_id.ok_or(ConvertError::Invalid)?,
        booking_date: convert::date_text(date)?,
        value_date: optional_date_text(row.value_date.as_deref())?,
        amount_minor: convert::required_minor(row.amount.as_ref(), currency)?,
        currency,
        partner_name: row.partner_name.clone(),
        purpose: row.purpose.clone(),
        fetched_at: convert::required_timestamp(row.fetched_at.as_deref(), offset)?,
    })
}

struct DocumentValues {
    file_name: String,
    mime_type: String,
    uploaded_at: String,
    linked_transaction_id: Option<i64>,
}

fn document_values(row: &DocumentRow, offset: UtcOffset) -> Result<DocumentValues, ConvertError> {
    Ok(DocumentValues {
        file_name: row.file_name.clone().ok_or(ConvertError::Invalid)?,
        mime_type: row.mime_type.clone().ok_or(ConvertError::Invalid)?,
        uploaded_at: convert::required_timestamp(row.upload_date.as_deref(), offset)?,
        linked_transaction_id: row.linked_transaction_id,
    })
}

struct OcrValues {
    document_id: Option<i64>,
    content: String,
    language: String,
    confidence: Option<f64>,
}

fn ocr_values(row: &OcrTextRow) -> Result<OcrValues, ConvertError> {
    Ok(OcrValues {
        document_id: row.document_id,
        content: row.content.clone().ok_or(ConvertError::Invalid)?,
        language: row.language.clone().ok_or(ConvertError::Invalid)?,
        confidence: convert::confidence(row.confidence.as_ref())?,
    })
}

fn optional_date_text(value: Option<&str>) -> Result<Option<String>, ConvertError> {
    convert::optional_date(value)?.map(convert::date_text).transpose()
}

enum DocumentFile {
    Found(PathBuf),
    Missing,
    Refused,
}

/// Resolves a stored FinDesk path by its base name inside the FinDesk documents folder only.
fn resolve_document_file(documents_root: Option<&Path>, stored_path: Option<&str>) -> DocumentFile {
    let Some(root) = documents_root else {
        return DocumentFile::Missing;
    };
    let Some(base_name) = stored_path.and_then(base_name) else {
        return DocumentFile::Missing;
    };

    let Ok(candidate) = root.join(base_name).canonicalize() else {
        return DocumentFile::Missing;
    };

    if !candidate.starts_with(root) || !candidate.is_file() {
        return DocumentFile::Missing;
    }

    match fs::metadata(&candidate) {
        Ok(metadata) if metadata.len() > MAX_DOCUMENT_BYTES => DocumentFile::Refused,
        Ok(_) => DocumentFile::Found(candidate),
        Err(_) => DocumentFile::Missing,
    }
}

fn base_name(path: &str) -> Option<&str> {
    path.rsplit(['/', '\\'])
        .next()
        .filter(|name| !name.is_empty() && *name != "." && *name != "..")
}

fn secure_file(path: &Path) -> io::Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))
    }

    #[cfg(not(unix))]
    {
        let _ = path;
        Ok(())
    }
}

#[derive(Default)]
struct CopiedFiles {
    paths: Vec<PathBuf>,
    keep: bool,
}

impl CopiedFiles {
    fn keep(&mut self) {
        self.keep = true;
        self.paths.clear();
    }
}

impl Drop for CopiedFiles {
    fn drop(&mut self) {
        if self.keep {
            return;
        }

        for path in &self.paths {
            let _ = fs::remove_file(path);
        }
    }
}

fn db_error(error: rusqlite::Error) -> FinDeskImportError {
    FinDeskImportError::Db(DbError::from(error))
}
