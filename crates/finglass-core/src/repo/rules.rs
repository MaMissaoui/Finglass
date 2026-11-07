//! Rule repository.

use rusqlite::{Row, params};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{CategoryId, NewRule, Rule, RuleDirection, RuleField, RuleId};

use super::{RepoError, Result, from_int_bool, id_only, in_conn, to_int_bool, to_optional_int_bool};

fn from_row(row: &Row<'_>) -> Result<Rule> {
    let match_field_text: String = row.get(5)?;
    let match_field = RuleField::from_stored(&match_field_text)
        .ok_or_else(|| RepoError::Invalid(format!("unknown rule match field {match_field_text:?}")))?;
    let direction_text: String = row.get(6)?;
    let direction = RuleDirection::from_stored(&direction_text)
        .ok_or_else(|| RepoError::Invalid(format!("unknown rule direction {direction_text:?}")))?;

    Ok(Rule {
        id: RuleId(row.get(0)?),
        pattern: row.get(1)?,
        category_id: CategoryId(row.get(2)?),
        priority: row.get(3)?,
        auto_apply: from_int_bool(row.get(4)?),
        match_field,
        direction,
        tax_relevant: row.get::<_, Option<i64>>(7)?.map(from_int_bool),
    })
}

/// Creates a rule.
pub fn create(db: &Database, new: NewRule) -> Result<RuleId> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute(
            "INSERT INTO rules (pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
            params![
                new.pattern,
                new.category_id.0,
                new.priority,
                to_int_bool(new.auto_apply),
                new.match_field.as_str(),
                new.direction.as_str(),
                to_optional_int_bool(new.tax_relevant),
            ],
        )?;

        let id = RuleId(transaction.last_insert_rowid());
        audit::log_conn(&transaction, "rule.create", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(id)
    })
}

/// Lists rules by priority, highest first, then id.
pub fn list(db: &Database) -> Result<Vec<Rule>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(
            "SELECT id, pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
             FROM rules ORDER BY priority DESC, id",
        )?;
        let mut rows = statement.query([])?;
        let mut rules = Vec::new();

        while let Some(row) = rows.next()? {
            rules.push(from_row(row)?);
        }

        Ok(rules)
    })
}

/// Replaces a rule's fields.
pub fn update(db: &Database, id: RuleId, update: NewRule) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute(
            "UPDATE rules
             SET pattern = ?1, category_id = ?2, priority = ?3, auto_apply = ?4,
                 match_field = ?5, direction = ?6, tax_relevant = ?7
             WHERE id = ?8",
            params![
                update.pattern,
                update.category_id.0,
                update.priority,
                to_int_bool(update.auto_apply),
                update.match_field.as_str(),
                update.direction.as_str(),
                to_optional_int_bool(update.tax_relevant),
                id.0,
            ],
        )?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "rule.update", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}

/// Deletes a rule.
pub fn delete(db: &Database, id: RuleId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM rules WHERE id = ?1", [id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "rule.delete", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}
