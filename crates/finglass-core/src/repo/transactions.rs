//! Transaction repository.

use std::collections::HashSet;

use rusqlite::{OptionalExtension, Row, params};
use time::{Date, OffsetDateTime};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{AccountId, CategoryId, NewTransaction, Transaction, TransactionId};
use crate::money::Money;

use super::{
    RepoError, Result, format_date, format_timestamp, id_only, in_conn, parse_currency, parse_timestamp, to_int_bool,
    to_optional_int_bool,
};

const COLUMNS: &str = "id, account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name, \
     purpose, category_id, import_hash, created_at, is_new, tax_override";

fn from_row(row: &Row<'_>) -> Result<Transaction> {
    let currency_text: String = row.get(4)?;
    let currency = parse_currency(&currency_text)?;

    Ok(Transaction {
        id: TransactionId(row.get(0)?),
        account_id: AccountId(row.get(1)?),
        booking_date: super::parse_date(&row.get::<_, String>(2)?)?,
        amount: Money {
            minor: row.get(3)?,
            currency,
        },
        balance_after: row.get::<_, Option<i64>>(5)?.map(|minor| Money { minor, currency }),
        partner_name: row.get(6)?,
        purpose: row.get(7)?,
        category_id: row.get::<_, Option<i64>>(8)?.map(CategoryId),
        import_hash: row.get(9)?,
        created_at: parse_timestamp(&row.get::<_, String>(10)?)?,
        is_new: super::from_int_bool(row.get(11)?),
        tax_override: row.get::<_, Option<i64>>(12)?.map(super::from_int_bool),
    })
}

/// What happened during an import batch.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct ImportOutcome {
    /// Rows that were inserted.
    pub inserted: usize,
    /// Rows skipped because the account already has the same import hash.
    pub skipped_duplicate: usize,
    /// Rows skipped because the account already recorded the booking as deleted.
    pub skipped_deleted: usize,
}

/// Imports a batch of transactions into one account.
///
/// Entries whose hash already exists for the account are skipped as duplicates. Entries whose hash is recorded as
/// deleted are skipped as deleted. Duplicate hashes inside the same batch are all inserted, because identical
/// same-day bookings are legitimate.
pub fn insert_imported(db: &Database, account_id: AccountId, entries: Vec<NewTransaction>) -> Result<ImportOutcome> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let currency_text: Option<String> = transaction
            .query_row("SELECT currency FROM accounts WHERE id = ?1", [account_id.0], |row| {
                row.get(0)
            })
            .optional()?;
        let Some(currency_text) = currency_text else {
            return Err(RepoError::NotFound);
        };
        let account_currency = parse_currency(&currency_text)?;

        for entry in &entries {
            if entry.amount.currency != account_currency {
                return Err(RepoError::CurrencyMismatch);
            }
            if let Some(balance_after) = entry.balance_after
                && balance_after.currency != account_currency
            {
                return Err(RepoError::CurrencyMismatch);
            }
        }

        let mut existing_hashes = HashSet::new();
        {
            let mut statement = transaction.prepare("SELECT import_hash FROM transactions WHERE account_id = ?1")?;
            let rows = statement.query_map([account_id.0], |row| row.get::<_, String>(0))?;
            for row in rows {
                existing_hashes.insert(row?);
            }
        }

        let mut deleted_hashes = HashSet::new();
        {
            let mut statement =
                transaction.prepare("SELECT import_hash FROM deleted_bookings WHERE account_id = ?1")?;
            let rows = statement.query_map([account_id.0], |row| row.get::<_, String>(0))?;
            for row in rows {
                deleted_hashes.insert(row?);
            }
        }

        let mut outcome = ImportOutcome::default();

        for entry in entries {
            if deleted_hashes.contains(&entry.import_hash) {
                outcome.skipped_deleted += 1;
                continue;
            }
            if existing_hashes.contains(&entry.import_hash) {
                outcome.skipped_duplicate += 1;
                continue;
            }

            let booking_date = format_date(entry.booking_date)?;
            let created_at = format_timestamp(entry.created_at)?;
            transaction.execute(
                "INSERT INTO transactions (
                    account_id, booking_date, amount_minor, currency, balance_after_minor, partner_name,
                    purpose, category_id, import_hash, created_at, is_new, tax_override
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12)",
                params![
                    account_id.0,
                    booking_date,
                    entry.amount.minor,
                    entry.amount.currency.as_str(),
                    entry.balance_after.map(|balance| balance.minor),
                    entry.partner_name,
                    entry.purpose,
                    entry.category_id.map(|id| id.0),
                    entry.import_hash,
                    created_at,
                    to_int_bool(entry.is_new),
                    to_optional_int_bool(entry.tax_override),
                ],
            )?;
            outcome.inserted += 1;
        }

        let details = format!(
            "inserted={} skipped_duplicate={} skipped_deleted={}",
            outcome.inserted, outcome.skipped_duplicate, outcome.skipped_deleted
        );
        audit::log_conn(&transaction, "transactions.import", Actor::User, &details)?;
        transaction.commit()?;

        Ok(outcome)
    })
}

/// Lists an account's transactions by booking date, newest first, then id, newest first.
pub fn list_for_account(
    db: &Database,
    account_id: AccountId,
    from: Option<Date>,
    to: Option<Date>,
) -> Result<Vec<Transaction>> {
    in_conn(db, |conn| {
        let from = from.map(format_date).transpose()?;
        let to = to.map(format_date).transpose()?;
        let mut statement = conn.prepare(&format!(
            "SELECT {COLUMNS}
             FROM transactions
             WHERE account_id = ?1
               AND (?2 IS NULL OR booking_date >= ?2)
               AND (?3 IS NULL OR booking_date <= ?3)
             ORDER BY booking_date DESC, id DESC"
        ))?;
        let mut rows = statement.query(params![account_id.0, from, to])?;
        let mut transactions = Vec::new();

        while let Some(row) = rows.next()? {
            transactions.push(from_row(row)?);
        }

        Ok(transactions)
    })
}

/// Assigns a category to a transaction, or removes it.
pub fn set_category(db: &Database, id: TransactionId, category_id: Option<CategoryId>) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute(
            "UPDATE transactions SET category_id = ?1 WHERE id = ?2",
            params![category_id.map(|category| category.0), id.0],
        )?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "transaction.set_category", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}

/// Deletes a transaction and records it so a re-import does not bring it back.
pub fn delete(db: &Database, id: TransactionId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let row: Option<(i64, String)> = transaction
            .query_row(
                "SELECT account_id, import_hash FROM transactions WHERE id = ?1",
                [id.0],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?;
        let Some((account_id, import_hash)) = row else {
            return Err(RepoError::NotFound);
        };

        let deleted_at = format_timestamp(OffsetDateTime::now_utc())?;
        transaction.execute(
            "INSERT INTO deleted_bookings (account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3)",
            params![account_id, import_hash, deleted_at],
        )?;
        transaction.execute("DELETE FROM transactions WHERE id = ?1", [id.0])?;

        audit::log_conn(&transaction, "transaction.delete", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}
