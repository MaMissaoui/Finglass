//! Reads a copied FinDesk database read-only.
//!
//! The selected FinDesk folder is never written to: `findesk.db` and its side files (`-wal`, `-shm`, `-journal`) are
//! copied into a temporary folder first, and only that copy is opened.

use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};

use rusqlite::types::{FromSql, FromSqlError, FromSqlResult, ValueRef};
use rusqlite::{Connection, ErrorCode, OpenFlags};
use tempfile::TempDir;

use crate::db::DbError;
use crate::secret::Passphrase;

use super::FinDeskImportError;

/// The tables every FinDesk database has had since its first release.
const REQUIRED_TABLES: &[&str] = &[
    "bank_access",
    "accounts",
    "categories",
    "rules",
    "transactions",
    "documents",
    "ocr_texts",
    "settings",
];

// Tables later FinDesk versions added (`account_groups`, `deleted_bookings`, `pending_bookings`,
// `payment_orders`) are optional: a database without one simply has no such rows.

/// A number as FinDesk stores monetary values: `INTEGER` or `REAL` (SQLite `NUMERIC` affinity).
#[derive(Debug, Clone, PartialEq)]
pub(crate) enum SourceNumber {
    Integer(i64),
    Real(f64),
    Text(String),
}

impl FromSql for SourceNumber {
    fn column_result(value: ValueRef<'_>) -> FromSqlResult<Self> {
        match value {
            ValueRef::Integer(value) => Ok(Self::Integer(value)),
            ValueRef::Real(value) => Ok(Self::Real(value)),
            ValueRef::Text(bytes) => Ok(Self::Text(String::from_utf8_lossy(bytes).into_owned())),
            ValueRef::Null => Err(FromSqlError::InvalidType),
            ValueRef::Blob(_) => Err(FromSqlError::InvalidType),
        }
    }
}

#[derive(Debug, Clone)]
pub(crate) struct AccountGroupRow {
    pub id: i64,
    pub name: Option<String>,
    pub sort_order: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct BankAccessRow {
    pub id: i64,
    pub bank_name: Option<String>,
    pub blz: Option<String>,
    pub bic: Option<String>,
    pub user_id: Option<String>,
    pub tan_method: Option<String>,
    pub server_url: Option<String>,
    pub sync_from: Option<String>,
    pub last_sync: Option<String>,
    pub include_in_sync_all: Option<i64>,
    pub offline: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct AccountRow {
    pub id: i64,
    pub bank_access_id: Option<i64>,
    pub iban: Option<String>,
    pub account_number: Option<String>,
    pub balance: Option<SourceNumber>,
    pub currency: Option<String>,
    pub account_type: Option<String>,
    pub name: Option<String>,
    pub last_update: Option<String>,
    pub group_id: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct CategoryRow {
    pub id: i64,
    pub name: Option<String>,
    pub color: Option<String>,
    pub budget_limit: Option<SourceNumber>,
    pub is_tax_relevant: Option<i64>,
    pub is_transfer: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct RuleRow {
    pub id: i64,
    pub pattern: Option<String>,
    pub category_id: Option<i64>,
    pub priority: Option<i64>,
    pub auto_apply: Option<i64>,
    pub match_field: Option<String>,
    pub direction: Option<String>,
    pub tax_relevant: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct TransactionRow {
    pub id: i64,
    pub account_id: Option<i64>,
    pub date: Option<String>,
    pub amount: Option<SourceNumber>,
    pub balance_after: Option<SourceNumber>,
    pub currency: Option<String>,
    pub partner_name: Option<String>,
    pub purpose: Option<String>,
    pub category_id: Option<i64>,
    pub hash: Option<String>,
    pub created_at: Option<String>,
    pub is_new: Option<i64>,
    pub tax_override: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct DeletedBookingRow {
    pub id: i64,
    pub account_id: Option<i64>,
    pub hash: Option<String>,
    pub deleted_at: Option<String>,
}

#[derive(Debug, Clone)]
pub(crate) struct PendingBookingRow {
    pub id: i64,
    pub account_id: Option<i64>,
    pub date: Option<String>,
    pub value_date: Option<String>,
    pub amount: Option<SourceNumber>,
    pub currency: Option<String>,
    pub partner_name: Option<String>,
    pub purpose: Option<String>,
    pub fetched_at: Option<String>,
}

#[derive(Debug, Clone)]
pub(crate) struct DocumentRow {
    pub id: i64,
    pub file_name: Option<String>,
    pub file_path: Option<String>,
    pub mime_type: Option<String>,
    pub upload_date: Option<String>,
    pub linked_transaction_id: Option<i64>,
}

#[derive(Debug, Clone)]
pub(crate) struct OcrTextRow {
    pub id: i64,
    pub document_id: Option<i64>,
    pub content: Option<String>,
    pub language: Option<String>,
    pub confidence: Option<SourceNumber>,
}

#[derive(Debug, Clone)]
pub(crate) struct SettingRow {
    pub key: Option<String>,
    pub value: Option<String>,
}

/// Everything read from the FinDesk copy.
#[derive(Debug, Clone, Default)]
pub(crate) struct SourceData {
    pub account_groups: Vec<AccountGroupRow>,
    pub bank_access: Vec<BankAccessRow>,
    pub accounts: Vec<AccountRow>,
    pub categories: Vec<CategoryRow>,
    pub rules: Vec<RuleRow>,
    pub transactions: Vec<TransactionRow>,
    pub deleted_bookings: Vec<DeletedBookingRow>,
    pub pending_bookings: Vec<PendingBookingRow>,
    pub documents: Vec<DocumentRow>,
    pub ocr_texts: Vec<OcrTextRow>,
    pub settings: Vec<SettingRow>,
    pub payment_orders: usize,
}

/// A FinDesk database copy plus the temporary folder that holds it.
pub(crate) struct FinDeskSource {
    conn: Connection,
    /// Dropped last, which deletes the copy also when reading or importing fails.
    _temporary: TempDir,
}

impl FinDeskSource {
    /// Copies the database (and WAL side files) into a temporary folder and opens the copy read-only.
    pub(crate) fn open(data_dir: &Path, passphrase: &Passphrase) -> Result<Self, FinDeskImportError> {
        let database = data_dir.join("findesk.db");
        if !database.is_file() {
            return Err(FinDeskImportError::NotFound);
        }

        let temporary = tempfile::tempdir()?;
        for name in ["findesk.db", "findesk.db-wal", "findesk.db-shm", "findesk.db-journal"] {
            let source: PathBuf = data_dir.join(name);
            if source.is_file() {
                fs::copy(&source, temporary.path().join(name))?;
            }
        }

        let copy = temporary.path().join("findesk.db");
        // Read-write on the throwaway copy, so SQLite can roll back a hot journal (or replay a WAL) left by a FinDesk
        // that crashed or was still writing; a read-only connection would fail or read half-written pages instead.
        // The selected FinDesk files are never opened.
        let conn = Connection::open_with_flags(&copy, OpenFlags::SQLITE_OPEN_READ_WRITE).map_err(db_error)?;

        // The key must be the first statement.
        conn.pragma_update(None, "key", passphrase.expose()).map_err(db_error)?;

        // Reading the schema forces SQLCipher to decrypt page 1, which is how a wrong key surfaces.
        match conn.query_row("SELECT count(*) FROM sqlite_master", [], |row| row.get::<_, i64>(0)) {
            Ok(_) => {}
            Err(rusqlite::Error::SqliteFailure(error, _)) if error.code == ErrorCode::NotADatabase => {
                return Err(FinDeskImportError::WrongPassphrase);
            }
            Err(error) => return Err(db_error(error)),
        }

        Ok(Self {
            conn,
            _temporary: temporary,
        })
    }

    /// Reads every table that the import needs.
    pub(crate) fn read(&self) -> Result<SourceData, FinDeskImportError> {
        let tables = self.require_tables()?;
        let has = |table: &str| tables.contains(table);
        let payment_orders = if has("payment_orders") {
            self.payment_order_count()?
        } else {
            0
        };

        Ok(SourceData {
            account_groups: self.query_if(
                has("account_groups"),
                "SELECT id, name, sort_order FROM account_groups ORDER BY id",
                |row| {
                    Ok(AccountGroupRow {
                        id: row.get(0)?,
                        name: row.get(1)?,
                        sort_order: row.get(2)?,
                    })
                },
            )?,
            bank_access: self.query(
                "SELECT id, bank_name, blz, bic, user_id, tan_method, server_url, sync_from, last_sync,
                        include_in_sync_all, offline
                 FROM bank_access ORDER BY id",
                |row| {
                    Ok(BankAccessRow {
                        id: row.get(0)?,
                        bank_name: row.get(1)?,
                        blz: row.get(2)?,
                        bic: row.get(3)?,
                        user_id: row.get(4)?,
                        tan_method: row.get(5)?,
                        server_url: row.get(6)?,
                        sync_from: row.get(7)?,
                        last_sync: row.get(8)?,
                        include_in_sync_all: row.get(9)?,
                        offline: row.get(10)?,
                    })
                },
            )?,
            accounts: self.query(
                "SELECT id, bank_access_id, iban, account_number, balance, currency, account_type, name,
                        last_update, group_id
                 FROM accounts ORDER BY id",
                |row| {
                    Ok(AccountRow {
                        id: row.get(0)?,
                        bank_access_id: row.get(1)?,
                        iban: row.get(2)?,
                        account_number: row.get(3)?,
                        balance: row.get(4)?,
                        currency: row.get(5)?,
                        account_type: row.get(6)?,
                        name: row.get(7)?,
                        last_update: row.get(8)?,
                        group_id: row.get(9)?,
                    })
                },
            )?,
            categories: self.query(
                "SELECT id, name, color, budget_limit, is_tax_relevant, is_transfer FROM categories ORDER BY id",
                |row| {
                    Ok(CategoryRow {
                        id: row.get(0)?,
                        name: row.get(1)?,
                        color: row.get(2)?,
                        budget_limit: row.get(3)?,
                        is_tax_relevant: row.get(4)?,
                        is_transfer: row.get(5)?,
                    })
                },
            )?,
            rules: self.query(
                "SELECT id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
                 FROM rules ORDER BY id",
                |row| {
                    Ok(RuleRow {
                        id: row.get(0)?,
                        pattern: row.get(1)?,
                        category_id: row.get(2)?,
                        priority: row.get(3)?,
                        auto_apply: row.get(4)?,
                        match_field: row.get(5)?,
                        direction: row.get(6)?,
                        tax_relevant: row.get(7)?,
                    })
                },
            )?,
            transactions: self.query(
                "SELECT id, account_id, date, amount, balance_after, currency, partner_name, purpose, category_id,
                        hash, created_at, is_new, tax_override
                 FROM transactions ORDER BY id",
                |row| {
                    Ok(TransactionRow {
                        id: row.get(0)?,
                        account_id: row.get(1)?,
                        date: row.get(2)?,
                        amount: row.get(3)?,
                        balance_after: row.get(4)?,
                        currency: row.get(5)?,
                        partner_name: row.get(6)?,
                        purpose: row.get(7)?,
                        category_id: row.get(8)?,
                        hash: row.get(9)?,
                        created_at: row.get(10)?,
                        is_new: row.get(11)?,
                        tax_override: row.get(12)?,
                    })
                },
            )?,
            deleted_bookings: self.query_if(
                has("deleted_bookings"),
                "SELECT id, account_id, hash, deleted_at FROM deleted_bookings ORDER BY id",
                |row| {
                    Ok(DeletedBookingRow {
                        id: row.get(0)?,
                        account_id: row.get(1)?,
                        hash: row.get(2)?,
                        deleted_at: row.get(3)?,
                    })
                },
            )?,
            pending_bookings: self.query_if(
                has("pending_bookings"),
                "SELECT id, account_id, date, value_date, amount, currency, partner_name, purpose, fetched_at
                 FROM pending_bookings ORDER BY id",
                |row| {
                    Ok(PendingBookingRow {
                        id: row.get(0)?,
                        account_id: row.get(1)?,
                        date: row.get(2)?,
                        value_date: row.get(3)?,
                        amount: row.get(4)?,
                        currency: row.get(5)?,
                        partner_name: row.get(6)?,
                        purpose: row.get(7)?,
                        fetched_at: row.get(8)?,
                    })
                },
            )?,
            documents: self.query(
                "SELECT id, file_name, file_path, mime_type, upload_date, linked_transaction_id
                 FROM documents ORDER BY id",
                |row| {
                    Ok(DocumentRow {
                        id: row.get(0)?,
                        file_name: row.get(1)?,
                        file_path: row.get(2)?,
                        mime_type: row.get(3)?,
                        upload_date: row.get(4)?,
                        linked_transaction_id: row.get(5)?,
                    })
                },
            )?,
            ocr_texts: self.query(
                "SELECT id, document_id, content, language, confidence FROM ocr_texts ORDER BY id",
                |row| {
                    Ok(OcrTextRow {
                        id: row.get(0)?,
                        document_id: row.get(1)?,
                        content: row.get(2)?,
                        language: row.get(3)?,
                        confidence: row.get(4)?,
                    })
                },
            )?,
            settings: self.query("SELECT key, value FROM settings ORDER BY id", |row| {
                Ok(SettingRow {
                    key: row.get(0)?,
                    value: row.get(1)?,
                })
            })?,
            payment_orders,
        })
    }

    fn require_tables(&self) -> Result<HashSet<String>, FinDeskImportError> {
        let mut statement = self
            .conn
            .prepare("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
            .map_err(db_error)?;
        let names: HashSet<String> = statement
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(db_error)?
            .collect::<rusqlite::Result<_>>()
            .map_err(db_error)?;

        if REQUIRED_TABLES.iter().any(|table| !names.contains(*table)) {
            return Err(FinDeskImportError::NotFinDesk);
        }

        Ok(names)
    }

    /// Runs `sql` only when its optional table exists; a missing optional table has no rows.
    fn query_if<T>(
        &self,
        present: bool,
        sql: &str,
        map: impl FnMut(&rusqlite::Row<'_>) -> rusqlite::Result<T>,
    ) -> Result<Vec<T>, FinDeskImportError> {
        if present { self.query(sql, map) } else { Ok(Vec::new()) }
    }

    fn payment_order_count(&self) -> Result<usize, FinDeskImportError> {
        let count: i64 = self
            .conn
            .query_row("SELECT count(*) FROM payment_orders", [], |row| row.get(0))
            .map_err(db_error)?;
        Ok(usize::try_from(count).unwrap_or(0))
    }

    fn query<T>(
        &self,
        sql: &str,
        map: impl FnMut(&rusqlite::Row<'_>) -> rusqlite::Result<T>,
    ) -> Result<Vec<T>, FinDeskImportError> {
        let mut statement = self.conn.prepare(sql).map_err(db_error)?;
        let rows = statement.query_map([], map).map_err(db_error)?;
        rows.collect::<rusqlite::Result<Vec<_>>>().map_err(db_error)
    }
}

fn db_error(error: rusqlite::Error) -> FinDeskImportError {
    FinDeskImportError::Db(DbError::from(error))
}
