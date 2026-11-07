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

/// Swaps an account group's sort order with its neighbour and returns whether it moved.
///
/// Ported from FinDesk's `AccountGroupRepository.move`: the groups are ordered by sort order, then id; at either end
/// there is no neighbour and nothing changes. The audit entry is written even for a no-op, like FinDesk's view action.
pub fn move_group(db: &Database, id: AccountGroupId, up: bool) -> Result<bool> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let mut groups = Vec::new();
        {
            let mut statement =
                transaction.prepare("SELECT id, name, sort_order FROM account_groups ORDER BY sort_order, id")?;
            let mut rows = statement.query([])?;
            while let Some(row) = rows.next()? {
                groups.push(from_row(row)?);
            }
        }

        let index = groups.iter().position(|group| group.id == id);
        let neighbour = index
            .and_then(|index| if up { index.checked_sub(1) } else { Some(index + 1) })
            .filter(|neighbour| *neighbour < groups.len());

        let moved = if let (Some(index), Some(neighbour)) = (index, neighbour) {
            let current_order = groups[index].sort_order;
            let neighbour_order = groups[neighbour].sort_order;
            transaction.execute(
                "UPDATE account_groups SET sort_order = ?1 WHERE id = ?2",
                params![neighbour_order, groups[index].id.0],
            )?;
            transaction.execute(
                "UPDATE account_groups SET sort_order = ?1 WHERE id = ?2",
                params![current_order, groups[neighbour].id.0],
            )?;
            true
        } else {
            false
        };

        audit::log_conn(
            &transaction,
            "REORDER_ACCOUNT_GROUP",
            Actor::User,
            &format!("Moved account group {} {}", id.0, if up { "up" } else { "down" }),
        )?;
        transaction.commit()?;

        Ok(moved)
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

    fn group(db: &Database, name: &str, sort_order: i64) -> AccountGroupId {
        create(
            db,
            NewAccountGroup {
                name: name.to_owned(),
                sort_order,
            },
        )
        .unwrap()
    }

    fn names(db: &Database) -> Vec<String> {
        list(db).unwrap().into_iter().map(|group| group.name).collect()
    }

    fn audit_count(db: &Database) -> i64 {
        db.with_conn(|conn| {
            conn.query_row("SELECT count(*) FROM audit_log", [], |row| row.get(0))
                .map_err(DbError::from)
        })
        .unwrap()
    }

    #[test]
    fn move_swaps_with_the_neighbour_and_is_a_no_op_at_the_ends() {
        let (_temp, db) = database();
        let first = group(&db, "A", 1);
        let second = group(&db, "B", 2);
        let third = group(&db, "C", 3);

        assert!(move_group(&db, third, true).unwrap());
        assert_eq!(names(&db), vec!["A", "C", "B"]);

        assert!(!move_group(&db, first, true).unwrap(), "the first group cannot move up");
        assert_eq!(names(&db), vec!["A", "C", "B"]);

        assert!(move_group(&db, first, false).unwrap());
        assert_eq!(names(&db), vec!["C", "A", "B"]);

        assert!(
            !move_group(&db, second, false).unwrap(),
            "the last group cannot move down"
        );
        assert_eq!(names(&db), vec!["C", "A", "B"]);
    }

    #[test]
    fn every_move_writes_one_audit_entry_even_when_it_does_nothing() {
        let (_temp, db) = database();
        let first = group(&db, "A", 1);
        let before = audit_count(&db);

        move_group(&db, first, true).unwrap();
        assert_eq!(audit_count(&db), before + 1);

        let entry = crate::audit::recent(&db, 1).unwrap().remove(0);
        assert_eq!(entry.action, "REORDER_ACCOUNT_GROUP");
        assert_eq!(entry.details, format!("Moved account group {} up", first.0));
    }
}
