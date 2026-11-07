//! Account group repository.

use rusqlite::{Row, params};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{AccountGroup, AccountGroupId, NewAccountGroup};

use super::{RepoError, Result, id_and_name, id_only, in_conn};

fn from_row(row: &Row<'_>) -> Result<AccountGroup> {
    Ok(AccountGroup {
        id: AccountGroupId(row.get(0)?),
        name: row.get(1)?,
        sort_order: row.get(2)?,
    })
}

/// Creates an account group.
pub fn create(db: &Database, new: NewAccountGroup) -> Result<AccountGroupId> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute(
            "INSERT INTO account_groups (name, sort_order) VALUES (?1, ?2)",
            params![new.name, new.sort_order],
        )?;

        let id = AccountGroupId(transaction.last_insert_rowid());
        audit::log_conn(
            &transaction,
            "account_group.create",
            Actor::User,
            &id_and_name(id.0, &new.name),
        )?;
        transaction.commit()?;

        Ok(id)
    })
}

/// Lists account groups by sort order, then name.
pub fn list(db: &Database) -> Result<Vec<AccountGroup>> {
    in_conn(db, |conn| {
        let mut statement =
            conn.prepare("SELECT id, name, sort_order FROM account_groups ORDER BY sort_order, name")?;
        let mut rows = statement.query([])?;
        let mut groups = Vec::new();

        while let Some(row) = rows.next()? {
            groups.push(from_row(row)?);
        }

        Ok(groups)
    })
}

/// Changes an account group's name.
pub fn rename(db: &Database, id: AccountGroupId, name: &str) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("UPDATE account_groups SET name = ?1 WHERE id = ?2", params![name, id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(
            &transaction,
            "account_group.rename",
            Actor::User,
            &id_and_name(id.0, name),
        )?;
        transaction.commit()?;

        Ok(())
    })
}

/// Deletes an account group. Its accounts keep existing without a group.
pub fn delete(db: &Database, id: AccountGroupId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM account_groups WHERE id = ?1", [id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "account_group.delete", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}
