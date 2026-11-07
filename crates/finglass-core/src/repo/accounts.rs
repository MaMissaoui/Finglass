//! Account repository.

use rusqlite::{OptionalExtension, Row, params};
use time::OffsetDateTime;

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{Account, AccountGroupId, AccountId, AccountType, BankConnectionId, NewAccount};
use crate::money::{Currency, Money};

use super::{RepoError, Result, format_timestamp, id_and_name, id_only, in_conn, parse_currency, parse_timestamp};

const COLUMNS: &str = "a.id, a.bank_connection_id, a.institution, a.name, a.iban, a.account_number, a.currency, \
     a.account_type, a.balance_minor, a.balance_updated_at, a.group_id";

fn from_row(row: &Row<'_>) -> Result<Account> {
    let currency_text: String = row.get(6)?;
    let currency = parse_currency(&currency_text)?;
    let account_type_text: String = row.get(7)?;
    let account_type = AccountType::from_stored(&account_type_text)
        .ok_or_else(|| RepoError::Invalid(format!("unknown account type {account_type_text:?}")))?;
    let balance_updated_at = row
        .get::<_, Option<String>>(9)?
        .map(|text| parse_timestamp(&text))
        .transpose()?;

    Ok(Account {
        id: AccountId(row.get(0)?),
        bank_connection_id: row.get::<_, Option<i64>>(1)?.map(BankConnectionId),
        institution: row.get(2)?,
        name: row.get(3)?,
        iban: row.get(4)?,
        account_number: row.get(5)?,
        currency,
        account_type,
        balance: Money {
            minor: row.get(8)?,
            currency,
        },
        balance_updated_at,
        group_id: row.get::<_, Option<i64>>(10)?.map(AccountGroupId),
    })
}

/// Creates an account.
pub fn create(db: &Database, new: NewAccount) -> Result<AccountId> {
    in_conn(db, |conn| {
        if new.balance.currency != new.currency {
            return Err(RepoError::CurrencyMismatch);
        }

        let transaction = conn.unchecked_transaction()?;
        let balance_updated_at = new.balance_updated_at.map(format_timestamp).transpose()?;

        transaction.execute(
            "INSERT INTO accounts (
                bank_connection_id, institution, name, iban, account_number, currency, account_type,
                balance_minor, balance_updated_at, group_id
             ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
            params![
                new.bank_connection_id.map(|id| id.0),
                new.institution,
                new.name,
                new.iban,
                new.account_number,
                new.currency.as_str(),
                new.account_type.as_str(),
                new.balance.minor,
                balance_updated_at,
                new.group_id.map(|id| id.0),
            ],
        )?;

        let id = AccountId(transaction.last_insert_rowid());
        let details = match &new.name {
            Some(name) => id_and_name(id.0, name),
            None => id_only(id.0),
        };
        audit::log_conn(&transaction, "account.create", Actor::User, &details)?;
        transaction.commit()?;

        Ok(id)
    })
}

/// Reads one account.
pub fn get(db: &Database, id: AccountId) -> Result<Option<Account>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!("SELECT {COLUMNS} FROM accounts a WHERE a.id = ?1"))?;
        let mut rows = statement.query([id.0])?;

        match rows.next()? {
            Some(row) => Ok(Some(from_row(row)?)),
            None => Ok(None),
        }
    })
}

/// Lists accounts by group sort order, then name, then id. Accounts without a group come last.
pub fn list(db: &Database) -> Result<Vec<Account>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!(
            "SELECT {COLUMNS}
             FROM accounts a
             LEFT JOIN account_groups g ON g.id = a.group_id
             ORDER BY CASE WHEN a.group_id IS NULL THEN 1 ELSE 0 END,
                      g.sort_order,
                      a.name,
                      a.id"
        ))?;
        let mut rows = statement.query([])?;
        let mut accounts = Vec::new();

        while let Some(row) = rows.next()? {
            accounts.push(from_row(row)?);
        }

        Ok(accounts)
    })
}

/// Changes an account's display name.
pub fn rename(db: &Database, id: AccountId, name: Option<String>) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("UPDATE accounts SET name = ?1 WHERE id = ?2", params![name, id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        let details = match &name {
            Some(name) => id_and_name(id.0, name),
            None => id_only(id.0),
        };
        audit::log_conn(&transaction, "account.rename", Actor::User, &details)?;
        transaction.commit()?;

        Ok(())
    })
}

/// Moves an account into a group, or removes it from its group.
pub fn set_group(db: &Database, id: AccountId, group_id: Option<AccountGroupId>) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute(
            "UPDATE accounts SET group_id = ?1 WHERE id = ?2",
            params![group_id.map(|group| group.0), id.0],
        )?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "account.set_group", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}

/// Sets an account's balance. The currency must match the account's currency.
pub fn set_balance(db: &Database, id: AccountId, balance: Money, at: OffsetDateTime) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let currency_text: Option<String> = transaction
            .query_row("SELECT currency FROM accounts WHERE id = ?1", [id.0], |row| row.get(0))
            .optional()?;
        let Some(currency_text) = currency_text else {
            return Err(RepoError::NotFound);
        };

        if parse_currency(&currency_text)? != balance.currency {
            return Err(RepoError::CurrencyMismatch);
        }

        let at = format_timestamp(at)?;
        transaction.execute(
            "UPDATE accounts SET balance_minor = ?1, balance_updated_at = ?2 WHERE id = ?3",
            params![balance.minor, at, id.0],
        )?;

        audit::log_conn(&transaction, "account.set_balance", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}

/// Sets an account's balance for the import service, without writing its own audit entry.
///
/// The import service writes the single `IMPORT_STATEMENT` audit entry after the balance update, with
/// FinDesk's import detail wording. [`set_balance`] keeps its own audit entry for direct user changes.
pub(crate) fn set_balance_for_import(db: &Database, id: AccountId, balance: Money, at: OffsetDateTime) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let currency_text: Option<String> = transaction
            .query_row("SELECT currency FROM accounts WHERE id = ?1", [id.0], |row| row.get(0))
            .optional()?;
        let Some(currency_text) = currency_text else {
            return Err(RepoError::NotFound);
        };

        if parse_currency(&currency_text)? != balance.currency {
            return Err(RepoError::CurrencyMismatch);
        }

        let at = format_timestamp(at)?;
        transaction.execute(
            "UPDATE accounts SET balance_minor = ?1, balance_updated_at = ?2 WHERE id = ?3",
            params![balance.minor, at, id.0],
        )?;
        transaction.commit()?;

        Ok(())
    })
}

/// Changes an account's type and returns whether the stored type changed.
///
/// Ported from FinDesk's `AccountsViewModel.setAccountType` and its repository `update(accountType)`: an unchanged
/// type stores nothing and writes no audit entry. The details use FinDesk's wording, because the Accounts view shows
/// them in the audit log.
pub fn set_account_type(db: &Database, id: AccountId, account_type: AccountType) -> Result<bool> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let previous: Option<String> = transaction
            .query_row("SELECT account_type FROM accounts WHERE id = ?1", [id.0], |row| {
                row.get(0)
            })
            .optional()?;
        let Some(previous) = previous else {
            return Err(RepoError::NotFound);
        };

        if previous == account_type.as_str() {
            return Ok(false);
        }

        transaction.execute(
            "UPDATE accounts SET account_type = ?1 WHERE id = ?2",
            params![account_type.as_str(), id.0],
        )?;
        audit::log_conn(
            &transaction,
            "CHANGE_ACCOUNT_TYPE",
            Actor::User,
            &format!("Account {}: {previous} -> {}", id.0, account_type.as_str()),
        )?;
        transaction.commit()?;

        Ok(true)
    })
}

/// Changes an account's currency and its balance in one transaction, with one audit entry.
///
/// The sync uses this when the old EUR fallback stored the wrong currency. An unchanged currency stores nothing and
/// writes no audit entry. The details use the account id only, never an IBAN.
pub fn set_currency(
    db: &Database,
    id: AccountId,
    currency: Currency,
    balance: Money,
    at: OffsetDateTime,
) -> Result<bool> {
    if balance.currency != currency {
        return Err(RepoError::CurrencyMismatch);
    }
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let previous: Option<String> = transaction
            .query_row("SELECT currency FROM accounts WHERE id = ?1", [id.0], |row| row.get(0))
            .optional()?;
        let Some(previous) = previous else {
            return Err(RepoError::NotFound);
        };

        if parse_currency(&previous)? == currency {
            return Ok(false);
        }

        let at = format_timestamp(at)?;
        transaction.execute(
            "UPDATE accounts SET currency = ?1, balance_minor = ?2, balance_updated_at = ?3 WHERE id = ?4",
            params![currency.as_str(), balance.minor, at, id.0],
        )?;
        audit::log_conn(
            &transaction,
            "CORRECT_ACCOUNT_CURRENCY",
            Actor::User,
            &format!("Account {}: {previous} -> {}", id.0, currency.as_str()),
        )?;
        transaction.commit()?;

        Ok(true)
    })
}

/// Deletes an account and, through the foreign keys, its bookings.
pub fn delete(db: &Database, id: AccountId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM accounts WHERE id = ?1", [id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "account.delete", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::DbError;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database, account_type: AccountType) -> AccountId {
        create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: None,
                account_number: None,
                currency: crate::money::Currency::EUR,
                account_type,
                balance: Money {
                    minor: 0,
                    currency: crate::money::Currency::EUR,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn audit_count(db: &Database) -> i64 {
        db.with_conn(|conn| {
            conn.query_row("SELECT count(*) FROM audit_log", [], |row| row.get(0))
                .map_err(DbError::from)
        })
        .unwrap()
    }

    #[test]
    fn set_account_type_changes_the_stored_type_and_audits_only_a_change() {
        let (_temp, db) = database();
        let id = account(&db, AccountType::Checking);
        let before = audit_count(&db);

        assert!(set_account_type(&db, id, AccountType::Loan).unwrap());
        assert_eq!(get(&db, id).unwrap().unwrap().account_type, AccountType::Loan);
        assert_eq!(audit_count(&db), before + 1);

        assert!(
            !set_account_type(&db, id, AccountType::Loan).unwrap(),
            "the same type changes nothing"
        );
        assert_eq!(audit_count(&db), before + 1, "and writes no second audit entry");

        let entry = crate::audit::recent(&db, 1).unwrap().remove(0);
        assert_eq!(entry.action, "CHANGE_ACCOUNT_TYPE");
        assert_eq!(entry.details, format!("Account {}: checking -> loan", id.0));
    }

    #[test]
    fn set_account_type_rejects_an_unknown_account() {
        let (_temp, db) = database();
        let error = set_account_type(&db, AccountId(999_999), AccountType::Loan).unwrap_err();
        assert!(matches!(error, RepoError::NotFound));
    }

    #[test]
    fn set_currency_changes_currency_and_balance_and_audits_it_once() {
        let (_temp, db) = database();
        let id = account(&db, AccountType::Checking);
        let before = audit_count(&db);
        let chf = Currency::new("CHF").unwrap();

        assert!(
            set_currency(
                &db,
                id,
                chf,
                Money {
                    minor: 8_850,
                    currency: chf,
                },
                OffsetDateTime::now_utc(),
            )
            .unwrap()
        );

        let stored = get(&db, id).unwrap().unwrap();
        assert_eq!(stored.currency, chf);
        assert_eq!(stored.balance.minor, 8_850);
        assert_eq!(audit_count(&db), before + 1);
        let entry = crate::audit::recent(&db, 1).unwrap().remove(0);
        assert_eq!(entry.action, "CORRECT_ACCOUNT_CURRENCY");
        assert_eq!(entry.details, format!("Account {}: EUR -> CHF", id.0));

        assert!(
            !set_currency(
                &db,
                id,
                chf,
                Money {
                    minor: 8_850,
                    currency: chf,
                },
                OffsetDateTime::now_utc(),
            )
            .unwrap(),
            "the same currency changes nothing"
        );
        assert_eq!(audit_count(&db), before + 1, "and writes no second audit entry");
    }
}
