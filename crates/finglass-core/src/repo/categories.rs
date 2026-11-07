//! Category repository.

use rusqlite::{Row, params};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{Category, CategoryId, NewCategory};
use crate::money::{Currency, Money};

use super::{RepoError, Result, from_int_bool, id_and_name, id_only, in_conn, to_int_bool};

fn from_row(row: &Row<'_>) -> Result<Category> {
    let budget_limit = row.get::<_, Option<i64>>(3)?.map(|minor| Money {
        minor,
        currency: Currency::EUR,
    });

    Ok(Category {
        id: CategoryId(row.get(0)?),
        name: row.get(1)?,
        color: row.get(2)?,
        budget_limit,
        is_tax_relevant: from_int_bool(row.get(4)?),
        is_transfer: from_int_bool(row.get(5)?),
    })
}

fn check_budget_currency(category: &NewCategory) -> Result<()> {
    if let Some(budget) = category.budget_limit
        && budget.currency != Currency::EUR
    {
        return Err(RepoError::CurrencyMismatch);
    }

    Ok(())
}

/// Creates a category.
pub fn create(db: &Database, new: NewCategory) -> Result<CategoryId> {
    in_conn(db, |conn| {
        check_budget_currency(&new)?;

        let transaction = conn.unchecked_transaction()?;
        transaction.execute(
            "INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                new.name,
                new.color,
                new.budget_limit.map(|budget| budget.minor),
                to_int_bool(new.is_tax_relevant),
                to_int_bool(new.is_transfer),
            ],
        )?;

        let id = CategoryId(transaction.last_insert_rowid());
        audit::log_conn(
            &transaction,
            "category.create",
            Actor::User,
            &id_and_name(id.0, &new.name),
        )?;
        transaction.commit()?;

        Ok(id)
    })
}

/// Reads one category.
pub fn get(db: &Database, id: CategoryId) -> Result<Option<Category>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(
            "SELECT id, name, color, budget_limit_minor, is_tax_relevant, is_transfer FROM categories WHERE id = ?1",
        )?;
        let mut rows = statement.query([id.0])?;

        match rows.next()? {
            Some(row) => Ok(Some(from_row(row)?)),
            None => Ok(None),
        }
    })
}

/// Lists categories by name, case-insensitively.
pub fn list(db: &Database) -> Result<Vec<Category>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(
            "SELECT id, name, color, budget_limit_minor, is_tax_relevant, is_transfer
             FROM categories ORDER BY name COLLATE NOCASE",
        )?;
        let mut rows = statement.query([])?;
        let mut categories = Vec::new();

        while let Some(row) = rows.next()? {
            categories.push(from_row(row)?);
        }

        Ok(categories)
    })
}

/// Replaces a category's fields.
pub fn update(db: &Database, id: CategoryId, update: NewCategory) -> Result<()> {
    in_conn(db, |conn| {
        check_budget_currency(&update)?;

        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute(
            "UPDATE categories
             SET name = ?1, color = ?2, budget_limit_minor = ?3, is_tax_relevant = ?4, is_transfer = ?5
             WHERE id = ?6",
            params![
                update.name,
                update.color,
                update.budget_limit.map(|budget| budget.minor),
                to_int_bool(update.is_tax_relevant),
                to_int_bool(update.is_transfer),
                id.0,
            ],
        )?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(
            &transaction,
            "category.update",
            Actor::User,
            &id_and_name(id.0, &update.name),
        )?;
        transaction.commit()?;

        Ok(())
    })
}

/// Deletes a category. Its bookings stay, uncategorized, and its rules are removed.
pub fn delete(db: &Database, id: CategoryId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM categories WHERE id = ?1", [id.0])?;

        if changed == 0 {
            return Err(RepoError::NotFound);
        }

        audit::log_conn(&transaction, "category.delete", Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(())
    })
}
