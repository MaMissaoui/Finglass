//! Category repository.

use rusqlite::{Row, params};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::i18n::{self, Key, Language};
use crate::model::{Category, CategoryId, NewCategory};
use crate::money::{Currency, Money};

use super::{RepoError, Result, from_int_bool, id_and_name, id_only, in_conn, to_int_bool};

/// The 24-colour category palette, in display order: six hues in four tones.
///
/// Neighbours differ in hue, and every colour sits in the lightness band that works on both the
/// light and the dark surface. Green and red are left out on purpose: green means money in, red means
/// an alert.
pub const COLORS: [&str; 24] = [
    "#E76530", // orange 4
    "#4693F3", // blue 4
    "#C38400", // yellow 4
    "#D56A93", // magenta 4
    "#867DFF", // violet 4
    "#14AC77", // aqua 4
    "#CF4E12", // orange 2
    "#2F7DDB", // blue 2
    "#A77105", // yellow 2
    "#BD547E", // magenta 2
    "#7364EC", // violet 2
    "#029465", // aqua 2
    "#DB5923", // orange 3
    "#3B88E7", // blue 3
    "#B57A03", // yellow 3
    "#C95F88", // magenta 3
    "#7C6FF8", // violet 3
    "#00A06E", // aqua 3
    "#BE4403", // orange 1
    "#2170CD", // blue 1
    "#986603", // yellow 1
    "#AF4872", // magenta 1
    "#6857DE", // violet 1
    "#07865C", // aqua 1
];

/// The default categories in display order: name key, colour and transfer flag.
///
/// Entry `i` is coloured [`COLORS`][i]. The names come from the string keys, so the current UI language decides what
/// is stored. Only "Own transfers" is a transfer: a card statement payment is the only record of card spending while
/// the card's own bookings come from no import, so counting it would erase the card's spending altogether.
pub const ENTRIES: [(Key, &str, bool); 24] = [
    (Key::DefaultCategoryGroceries, COLORS[0], false),
    (Key::DefaultCategoryDrugstore, COLORS[1], false),
    (Key::DefaultCategoryHousing, COLORS[2], false),
    (Key::DefaultCategoryEnergy, COLORS[3], false),
    (Key::DefaultCategoryPhoneInternetMedia, COLORS[4], false),
    (Key::DefaultCategoryInsurance, COLORS[5], false),
    (Key::DefaultCategoryLoans, COLORS[6], false),
    (Key::DefaultCategorySavings, COLORS[7], false),
    (Key::DefaultCategoryTransport, COLORS[8], false),
    (Key::DefaultCategoryHealth, COLORS[9], false),
    (Key::DefaultCategoryClothing, COLORS[10], false),
    (Key::DefaultCategoryOnlineShopping, COLORS[11], false),
    (Key::DefaultCategoryLeisure, COLORS[12], false),
    (Key::DefaultCategoryRestaurants, COLORS[13], false),
    (Key::DefaultCategoryTaxes, COLORS[14], false),
    (Key::DefaultCategoryCash, COLORS[15], false),
    // NOT a transfer: see the note above.
    (Key::DefaultCategoryCreditCard, COLORS[16], false),
    (Key::DefaultCategoryOwnTransfers, COLORS[17], true),
    (Key::DefaultCategorySalary, COLORS[18], false),
    (Key::DefaultCategoryChildBenefit, COLORS[19], false),
    (Key::DefaultCategoryInterestFees, COLORS[20], false),
    (Key::DefaultCategoryFamily, COLORS[21], false),
    (Key::DefaultCategoryChildrenSchool, COLORS[22], false),
    (Key::DefaultCategoryOtherIncome, COLORS[23], false),
];

/// The palette colour of the default category whose localized name equals `name`, or `None`.
pub fn default_colour_for(name: &str, lang: Language) -> Option<&'static str> {
    ENTRIES
        .iter()
        .find(|(key, _, _)| i18n::text(lang, *key).trim().eq_ignore_ascii_case(name.trim()))
        .map(|(_, color, _)| *color)
}

/// Adds the default categories that don't exist yet, in one transaction with one audit entry, as FinDesk's
/// "Add default categories" does.
///
/// A default is skipped when a category with the same name (trimmed, case-insensitive) exists; existing categories are
/// never changed. The audit entry is written also when nothing was added. Returns how many categories were added.
pub fn seed_defaults(db: &Database, lang: Language) -> Result<usize> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let existing: std::collections::HashSet<String> = {
            let mut statement = transaction.prepare("SELECT name FROM categories")?;
            let names = statement
                .query_map([], |row| row.get::<_, String>(0))?
                .collect::<rusqlite::Result<Vec<_>>>()?;
            names.into_iter().map(|name| name.trim().to_lowercase()).collect()
        };

        let mut added = 0_usize;
        for (key, color, is_transfer) in ENTRIES {
            let name = i18n::text(lang, key).trim();
            if existing.contains(&name.to_lowercase()) {
                continue;
            }
            transaction.execute(
                "INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)
                 VALUES (?1, ?2, NULL, 0, ?3)",
                params![name, color, to_int_bool(is_transfer)],
            )?;
            added += 1;
        }

        audit::log_conn(
            &transaction,
            "ADD_DEFAULT_CATEGORIES",
            Actor::User,
            &format!("Added {added} default categories"),
        )?;
        transaction.commit()?;

        Ok(added)
    })
}

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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::Database;
    use crate::i18n::Language;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn audit_actions(db: &Database) -> Vec<(String, String)> {
        crate::audit::recent(db, 1000)
            .unwrap()
            .into_iter()
            .map(|entry| (entry.action, entry.details))
            .collect()
    }

    #[test]
    fn the_palette_has_24_distinct_colours_and_the_defaults_use_them_in_order() {
        assert_eq!(COLORS.len(), 24);
        assert_eq!(
            COLORS
                .iter()
                .map(|color| color.to_uppercase())
                .collect::<std::collections::BTreeSet<_>>()
                .len(),
            24,
            "the colours must be distinct"
        );

        assert_eq!(ENTRIES.len(), 24);
        assert_eq!(
            ENTRIES
                .iter()
                .map(|(_, color, _)| *color)
                .collect::<std::collections::BTreeSet<_>>()
                .len(),
            24,
            "no cycling: every category needs its own colour"
        );
        assert_eq!(
            ENTRIES.iter().map(|(_, color, _)| *color).collect::<Vec<_>>(),
            COLORS.to_vec()
        );
    }

    #[test]
    fn the_palette_stays_in_the_documented_order() {
        assert_eq!(COLORS[..6].concat(), "#E76530#4693F3#C38400#D56A93#867DFF#14AC77");
        assert_eq!(COLORS[6..12].concat(), "#CF4E12#2F7DDB#A77105#BD547E#7364EC#029465");
        assert_eq!(COLORS[12..18].concat(), "#DB5923#3B88E7#B57A03#C95F88#7C6FF8#00A06E");
        assert_eq!(COLORS[18..].concat(), "#BE4403#2170CD#986603#AF4872#6857DE#07865C");
    }

    #[test]
    fn only_own_transfers_is_a_transfer() {
        let transfers: Vec<_> = ENTRIES.iter().filter(|(_, _, is_transfer)| *is_transfer).collect();
        assert_eq!(transfers.len(), 1);
        assert_eq!(transfers[0].0, Key::DefaultCategoryOwnTransfers);
        assert!(
            !ENTRIES
                .iter()
                .any(|(key, _, is_transfer)| { *key == Key::DefaultCategoryCreditCard && *is_transfer })
        );
    }

    #[test]
    fn seeding_an_empty_database_adds_all_24_defaults_and_audits_once() {
        let (_temp, db) = database();

        let added = seed_defaults(&db, Language::En).unwrap();

        assert_eq!(added, 24);
        let names: Vec<String> = list(&db).unwrap().into_iter().map(|category| category.name).collect();
        assert_eq!(names.len(), 24);
        assert!(names.contains(&"Groceries".to_owned()));
        assert!(names.contains(&"Drugstore & household".to_owned()));
        assert!(names.contains(&"Other income".to_owned()));

        let entries = audit_actions(&db);
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].0, "ADD_DEFAULT_CATEGORIES");
        assert_eq!(entries[0].1, "Added 24 default categories");
    }

    #[test]
    fn seeding_stores_the_transfer_flag() {
        let (_temp, db) = database();
        seed_defaults(&db, Language::En).unwrap();

        let saved: std::collections::HashMap<String, bool> = list(&db)
            .unwrap()
            .into_iter()
            .map(|category| (category.name, category.is_transfer))
            .collect();
        assert!(!saved["Groceries"]);
        assert!(!saved["Credit card"]);
        assert!(saved["Own transfers"]);
    }

    #[test]
    fn seeding_uses_the_language_names_and_default_colours() {
        let (_temp, db) = database();
        seed_defaults(&db, Language::De).unwrap();

        let groceries = list(&db)
            .unwrap()
            .into_iter()
            .find(|category| category.name == "Lebensmittel")
            .unwrap();
        assert_eq!(groceries.color, "#E76530");
    }

    #[test]
    fn existing_categories_are_kept_and_matched_case_insensitively() {
        let (_temp, db) = database();
        let existing = create(
            &db,
            NewCategory {
                name: "groceries".to_owned(),
                color: "#123456".to_owned(),
                budget_limit: None,
                is_tax_relevant: false,
                is_transfer: false,
            },
        )
        .unwrap();

        assert_eq!(seed_defaults(&db, Language::En).unwrap(), 23);
        let categories = list(&db).unwrap();
        assert_eq!(categories.len(), 24);
        let groceries = categories.iter().find(|category| category.id == existing).unwrap();
        assert_eq!(groceries.name, "groceries", "existing categories are never changed");
        assert_eq!(groceries.color, "#123456");

        assert_eq!(seed_defaults(&db, Language::En).unwrap(), 0);
        assert_eq!(list(&db).unwrap().len(), 24);
    }

    #[test]
    fn two_seed_calls_create_the_defaults_once() {
        let (_temp, db) = database();

        assert_eq!(seed_defaults(&db, Language::En).unwrap(), 24);
        assert_eq!(seed_defaults(&db, Language::En).unwrap(), 0);

        let names: Vec<String> = list(&db).unwrap().into_iter().map(|category| category.name).collect();
        assert_eq!(names.len(), 24);
        let unique: std::collections::BTreeSet<&String> = names.iter().collect();
        assert_eq!(unique.len(), names.len(), "no category is created twice");
        assert_eq!(audit_actions(&db).len(), 2, "each call is audited, as in FinDesk");
    }

    #[test]
    fn default_colours_match_localized_default_names() {
        assert_eq!(default_colour_for("  groceries ", Language::En), Some("#E76530"));
        assert_eq!(default_colour_for("Lebensmittel", Language::De), Some("#E76530"));
        assert_eq!(default_colour_for("not a default", Language::En), None);
    }
}
