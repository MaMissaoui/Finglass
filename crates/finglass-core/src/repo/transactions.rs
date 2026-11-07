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

/// Whether the account already stores a booking with this import hash.
///
/// FinDesk checks a hash alone. Finglass' import hashes include the account's IBAN (as FinDesk's
/// file and FinTS hashes do too), so the lookup is scoped by account.
pub fn find_by_hash(db: &Database, account_id: AccountId, hash: &str) -> Result<bool> {
    in_conn(db, |conn| {
        let mut statement =
            conn.prepare("SELECT 1 FROM transactions WHERE account_id = ?1 AND import_hash = ?2 LIMIT 1")?;
        Ok(statement.exists(params![account_id.0, hash])?)
    })
}

/// The dedup hashes remembered as deleted for an account.
pub fn deleted_hashes(db: &Database, account_id: AccountId) -> Result<HashSet<String>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare("SELECT import_hash FROM deleted_bookings WHERE account_id = ?1")?;
        let rows = statement.query_map([account_id.0], |row| row.get::<_, String>(0))?;
        let mut hashes = HashSet::new();
        for row in rows {
            hashes.insert(row?);
        }
        Ok(hashes)
    })
}

/// Number of bookings on an account with this booking date and amount (in minor units).
pub fn count_same_booking(db: &Database, account_id: AccountId, date: Date, amount_minor: i64) -> Result<i64> {
    in_conn(db, |conn| {
        let date = format_date(date)?;
        Ok(conn.query_row(
            "SELECT count(*) FROM transactions
             WHERE account_id = ?1 AND booking_date = ?2 AND amount_minor = ?3",
            params![account_id.0, date, amount_minor],
            |row| row.get(0),
        )?)
    })
}

/// Inserts already-planned rows in one transaction, without duplicate or deletion filtering.
///
/// The import service writes the single `IMPORT_STATEMENT` audit entry after this, with FinDesk's
/// import detail wording, so this function deliberately does not add one of its own.
pub fn insert_planned(db: &Database, account_id: AccountId, rows: Vec<NewTransaction>) -> Result<usize> {
    if rows.is_empty() {
        return Ok(0);
    }

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

        for entry in &rows {
            if entry.amount.currency != account_currency
                || entry
                    .balance_after
                    .is_some_and(|balance| balance.currency != account_currency)
            {
                return Err(RepoError::CurrencyMismatch);
            }
        }

        for entry in &rows {
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
        }

        transaction.commit()?;
        Ok(rows.len())
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

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::model::{AccountType, NewAccount};
    use crate::money::Currency;
    use crate::repo::accounts;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: Some("DE89370400440532013000".to_owned()),
                account_number: None,
                currency: Currency::EUR,
                account_type: AccountType::Checking,
                balance: Money {
                    minor: 0,
                    currency: Currency::EUR,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn entry(hash: &str) -> NewTransaction {
        NewTransaction {
            booking_date: date!(2026 - 10 - 03),
            amount: Money {
                minor: -450,
                currency: Currency::EUR,
            },
            balance_after: None,
            partner_name: Some("Bäcker".to_owned()),
            purpose: Some("Kaffee".to_owned()),
            category_id: None,
            import_hash: hash.to_owned(),
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }
    }

    #[test]
    fn hash_and_duplicate_queries_are_scoped_by_account() {
        let (_temp, db) = database();
        let one = account(&db);
        let two = account(&db);

        insert_planned(&db, one, vec![entry("same-hash")]).unwrap();

        assert!(find_by_hash(&db, one, "same-hash").unwrap());
        assert!(!find_by_hash(&db, two, "same-hash").unwrap());
        assert_eq!(count_same_booking(&db, one, date!(2026 - 10 - 03), -450).unwrap(), 1);
        assert_eq!(count_same_booking(&db, two, date!(2026 - 10 - 03), -450).unwrap(), 0);

        let booking = list_for_account(&db, one, None, None).unwrap()[0].clone();
        delete(&db, booking.id).unwrap();
        assert!(deleted_hashes(&db, one).unwrap().contains("same-hash"));
        assert!(deleted_hashes(&db, two).unwrap().is_empty());
    }

    #[test]
    fn insert_planned_inserts_every_row_without_dedup_or_its_own_audit_entry() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let before = audit::recent(&db, 100).unwrap().len();

        assert_eq!(
            insert_planned(&db, account_id, vec![entry("same"), entry("same")]).unwrap(),
            2
        );
        assert_eq!(
            insert_planned(&db, account_id, vec![entry("same"), entry("same")]).unwrap(),
            2
        );
        assert_eq!(list_for_account(&db, account_id, None, None).unwrap().len(), 4);
        assert_eq!(
            audit::recent(&db, 100).unwrap().len(),
            before,
            "the import service writes the single import audit entry itself"
        );
    }
}
