//! Import and export of a reviewed rule set, ported from FinDesk's `importer/RuleImportService.kt`.
//!
//! A preview is read-only. Applying a confirmed preview creates the missing categories and the new rules and,
//! optionally, categorizes uncategorized bookings with the combined auto-apply rule set, all in one transaction with
//! one audit entry.

use std::collections::{HashMap, HashSet};
use std::path::Path;

use rusqlite::{Connection, params};
use rust_decimal::Decimal;
use thiserror::Error;

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::i18n::Language;
use crate::model::{CategoryId, RuleDirection, RuleField, TransactionFilter};
use crate::money::Money;
use crate::repo::categories;
use crate::repo::{self, RepoError};

use super::file::{ParsedRuleFile, RuleFile, RuleFileError, RuleFileItem, RuleFileRow};
use super::matcher::{RuleMatcher, RuleSpec};

/// Same pattern, text field and money flow as an imported row, but the file's category would sit beside another.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuleImportConflict {
    /// 1-based line of the file the new rule was read from.
    pub line: usize,
    /// The pattern.
    pub pattern: String,
    /// The category the file names.
    pub file_category: String,
    /// The category the existing rule uses.
    pub existing_category: String,
}

/// What an import of a parsed rule file would change; the preview dialog shows it before anything is written.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RuleImportPreview {
    /// Categories that do not exist yet, in file order, deduplicated case-insensitively (first spelling wins).
    pub categories_to_create: Vec<String>,
    /// Rows that create a new rule, in file order.
    pub new_rules: Vec<RuleFileRow>,
    /// Rows that would collide with an existing rule or an earlier row of the same file; they are skipped.
    pub conflicts: Vec<RuleImportConflict>,
    /// Rows that repeat an identical rule (same pattern, field, direction and category); they are skipped.
    pub duplicates: usize,
    /// How many currently uncategorized bookings the combined auto-apply rule set would categorize.
    pub match_count: usize,
    /// Row errors passed through from the parse.
    pub errors: Vec<RuleFileError>,
}

/// The counts an applied import changed.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct RuleImportResult {
    /// Categories created.
    pub categories: usize,
    /// Rules created.
    pub rules: usize,
    /// Bookings categorized.
    pub categorized: usize,
}

/// Everything that can go wrong in a rule import or export.
#[derive(Debug, Error)]
pub enum RuleImportError {
    /// A repository call failed.
    #[error(transparent)]
    Repo(#[from] RepoError),
    /// A database operation failed.
    #[error(transparent)]
    Db(#[from] DbError),
    /// Writing the rule file failed.
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// A rule import error result.
pub type Result<T> = std::result::Result<T, RuleImportError>;

/// Moves a tested rule set between profiles and installations.
pub struct RuleImportService;

impl RuleImportService {
    /// Read-only: classifies every usable row against the existing data, leaving categories and rules untouched.
    pub fn preview(db: &Database, parsed: &ParsedRuleFile) -> Result<RuleImportPreview> {
        let existing_categories = categories::list(db)?;
        let existing_keys: HashSet<String> = existing_categories
            .iter()
            .map(|category| category.name.trim().to_lowercase())
            .collect();

        let mut seen_in_file = HashSet::new();
        let mut categories_to_create = Vec::new();
        for row in &parsed.rows {
            let key = row.category.trim().to_lowercase();
            if !existing_keys.contains(&key) && seen_in_file.insert(key) {
                categories_to_create.push(row.category.trim().to_owned());
            }
        }

        // Existing rules indexed by pattern, field and direction; the first kept is like the file's earliest row.
        let mut existing_by_key: HashMap<(String, RuleField, RuleDirection), String> = HashMap::new();
        for item in repo::rules::list(db)? {
            let key = (item.pattern.clone(), item.match_field, item.direction);
            existing_by_key.entry(key).or_insert_with(|| {
                existing_categories
                    .iter()
                    .find(|category| category.id == item.category_id)
                    .map(|category| category.name.clone())
                    .unwrap_or_default()
            });
        }

        // An earlier row of the same file counts like an existing rule: a later identical row is a duplicate,
        // a row with the same key but another category a conflict. That earlier row may itself have been skipped.
        let mut earlier_by_key: HashMap<(String, RuleField, RuleDirection), (String, String)> = HashMap::new();
        let mut new_rules = Vec::new();
        let mut conflicts = Vec::new();
        let mut duplicates = 0_usize;

        for row in &parsed.rows {
            let key = (row.pattern.clone(), row.field, row.direction);
            let category_key = row.category.trim().to_lowercase();

            if let Some((earlier_key, earlier_category)) = earlier_by_key.get(&key) {
                if *earlier_key == category_key {
                    duplicates += 1;
                } else {
                    conflicts.push(RuleImportConflict {
                        line: row.line,
                        pattern: row.pattern.clone(),
                        file_category: row.category.clone(),
                        existing_category: earlier_category.clone(),
                    });
                }
                continue;
            }

            if let Some(existing_category) = existing_by_key.get(&key) {
                if existing_category.trim().to_lowercase() == category_key {
                    duplicates += 1;
                } else {
                    conflicts.push(RuleImportConflict {
                        line: row.line,
                        pattern: row.pattern.clone(),
                        file_category: row.category.clone(),
                        existing_category: existing_category.clone(),
                    });
                }
                continue;
            }

            new_rules.push(row.clone());
            earlier_by_key.insert(key, (category_key, row.category.clone()));
        }

        let auto_apply_rows: Vec<RuleFileRow> = new_rules.iter().filter(|row| row.auto_apply).cloned().collect();
        let match_count = match_count(db, &auto_apply_rows, &categories_to_create)?;

        Ok(RuleImportPreview {
            categories_to_create,
            new_rules,
            conflicts,
            duplicates,
            match_count,
            errors: parsed.errors.clone(),
        })
    }

    /// Applies the confirmed `preview` in one transaction: categories, rules and, on request, the categorization.
    pub fn apply(
        db: &Database,
        preview: &RuleImportPreview,
        categorize: bool,
        lang: Language,
    ) -> Result<RuleImportResult> {
        let categories_to_create = preview.categories_to_create.clone();
        let new_rules = preview.new_rules.clone();

        db.with_conn(|conn| apply_conn(conn, &categories_to_create, &new_rules, categorize, lang))
            .map_err(RuleImportError::Db)
    }

    /// Writes the current rules (auto-apply and manual) to `file` in the reviewable rule-file format.
    pub fn export(db: &Database, file: &Path) -> Result<usize> {
        let rules = repo::rules::list(db)?;
        let categories = categories::list(db)?;
        let names: HashMap<CategoryId, String> = categories
            .into_iter()
            .map(|category| (category.id, category.name))
            .collect();

        let items: Vec<RuleFileItem> = rules
            .iter()
            .map(|rule| RuleFileItem {
                pattern: rule.pattern.clone(),
                category_name: names.get(&rule.category_id).cloned().unwrap_or_default(),
                priority: rule.priority,
                auto_apply: rule.auto_apply,
                match_field: rule.match_field,
                direction: rule.direction,
            })
            .collect();

        let (text, skipped) = RuleFile::write(&items);
        std::fs::write(file, text.as_bytes())?;

        let written = items.len() - skipped;
        let details = if skipped > 0 {
            format!("Exported {written} rules, skipped {skipped}")
        } else {
            format!("Exported {written} rules")
        };
        audit::log(db, "EXPORT_RULES", Actor::User, &details)?;

        Ok(written)
    }
}

/// How many currently uncategorized bookings the combined auto-apply rule set would categorize.
fn match_count(db: &Database, auto_apply_rows: &[RuleFileRow], categories_to_create: &[String]) -> Result<usize> {
    let bookings = repo::transactions::find_items(
        db,
        &TransactionFilter {
            uncategorized_only: true,
            ..TransactionFilter::default()
        },
        None,
    )?;
    if bookings.is_empty() {
        return Ok(0);
    }

    let existing_keys: HashMap<String, i64> = categories::list(db)?
        .into_iter()
        .map(|category| (category.name.trim().to_lowercase(), category.id.0))
        .collect();
    let temp_ids: HashMap<String, i64> = categories_to_create
        .iter()
        .enumerate()
        .map(|(index, name)| (name.trim().to_lowercase(), -1 - index as i64))
        .collect();

    let category_id_of = |name: &str| -> i64 {
        let key = name.trim().to_lowercase();
        existing_keys
            .get(&key)
            .or_else(|| temp_ids.get(&key))
            .copied()
            .expect("every file category exists or is created")
    };

    // Existing rules first, then the file's rows, all merged by priority; the file keeps its own order on ties.
    let mut merged: Vec<(i64, u8, usize, RuleSpec)> = Vec::new();
    for (index, rule) in repo::rules::list(db)?
        .into_iter()
        .filter(|rule| rule.auto_apply)
        .enumerate()
    {
        merged.push((
            rule.priority,
            0,
            index,
            RuleSpec {
                pattern: rule.pattern,
                category_id: rule.category_id,
                field: rule.match_field,
                direction: rule.direction,
            },
        ));
    }
    for (index, row) in auto_apply_rows.iter().enumerate() {
        merged.push((
            row.priority,
            1,
            index,
            RuleSpec {
                pattern: row.pattern.clone(),
                category_id: CategoryId(category_id_of(&row.category)),
                field: row.field,
                direction: row.direction,
            },
        ));
    }
    merged.sort_by(|left, right| {
        right
            .0
            .cmp(&left.0)
            .then(left.1.cmp(&right.1))
            .then(left.2.cmp(&right.2))
    });

    let specs = merged.into_iter().map(|(_, _, _, spec)| spec).collect();
    let matcher = RuleMatcher::new(specs);
    Ok(bookings
        .iter()
        .filter(|booking| {
            matcher
                .category_for(
                    booking.purpose.as_deref(),
                    booking.partner_name.as_deref(),
                    Some(money_to_decimal(booking.amount)),
                )
                .is_some()
        })
        .count())
}

fn money_to_decimal(money: Money) -> Decimal {
    Decimal::new(money.minor, money.currency.minor_exponent())
}

fn apply_conn(
    conn: &Connection,
    categories_to_create: &[String],
    new_rules: &[RuleFileRow],
    categorize: bool,
    lang: Language,
) -> std::result::Result<RuleImportResult, DbError> {
    let transaction = conn.unchecked_transaction()?;

    // The existing spelling and id win: everything is resolved case-insensitively by trimmed name.
    let mut resolved: HashMap<String, i64> = HashMap::new();
    let existing_count = {
        let mut count = 0_usize;
        let mut statement = transaction.prepare("SELECT id, name FROM categories ORDER BY name COLLATE NOCASE")?;
        let rows = statement.query_map([], |row| Ok((row.get::<_, i64>(0)?, row.get::<_, String>(1)?)))?;
        for row in rows {
            let (id, name) = row?;
            resolved.entry(name.trim().to_lowercase()).or_insert(id);
            count += 1;
        }
        count
    };
    let mut created: HashMap<String, i64> = HashMap::new();

    // The preview's list is in file order; every name must resolve before any step writes.
    for name in categories_to_create {
        resolve_category(&transaction, &mut resolved, &mut created, existing_count, name, lang)?;
    }

    for row in new_rules {
        let category_id = resolve_category(
            &transaction,
            &mut resolved,
            &mut created,
            existing_count,
            &row.category,
            lang,
        )?;
        transaction.execute(
            "INSERT INTO rules (pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, NULL)",
            params![
                row.pattern,
                category_id,
                row.priority,
                i64::from(row.auto_apply),
                row.field.as_str(),
                row.direction.as_str(),
            ],
        )?;
    }

    let mut categorized = 0_usize;
    if categorize && !new_rules.is_empty() {
        let matcher = load_matcher(&transaction)?;
        let bookings: Vec<(i64, Option<String>, Option<String>, Money)> = {
            let mut statement = transaction.prepare(
                "SELECT id, partner_name, purpose, amount_minor, currency
                 FROM transactions WHERE category_id IS NULL ORDER BY booking_date DESC, id DESC",
            )?;
            let rows = statement.query_map([], |row| {
                let minor: i64 = row.get(3)?;
                let currency: String = row.get(4)?;
                Ok((
                    row.get::<_, i64>(0)?,
                    row.get::<_, Option<String>>(1)?,
                    row.get::<_, Option<String>>(2)?,
                    (minor, currency),
                ))
            })?;
            let mut bookings = Vec::new();
            for row in rows {
                let (id, partner_name, purpose, (minor, currency)) = row?;
                let currency = crate::money::Currency::new(&currency)
                    .map_err(|error| DbError::Io(std::io::Error::other(error)))?;
                bookings.push((id, partner_name, purpose, Money { minor, currency }));
            }
            bookings
        };

        for (id, partner_name, purpose, amount) in bookings {
            let Some(category_id) = matcher.category_for(
                purpose.as_deref(),
                partner_name.as_deref(),
                Some(money_to_decimal(amount)),
            ) else {
                continue;
            };
            let changed = transaction.execute(
                "UPDATE transactions SET category_id = ?1 WHERE id = ?2 AND category_id IS NULL",
                params![category_id.0, id],
            )?;
            if changed > 0 {
                categorized += 1;
            }
        }
    }

    let result = RuleImportResult {
        categories: categories_to_create.len(),
        rules: new_rules.len(),
        categorized,
    };
    audit::log_conn(
        &transaction,
        "IMPORT_RULES",
        Actor::User,
        &format!(
            "Imported {} rules, created {} categories, categorized {} transactions",
            result.rules, result.categories, result.categorized
        ),
    )?;
    transaction.commit()?;

    Ok(result)
}

/// Resolves one category name, creating it with its default or palette colour when it is new.
fn resolve_category(
    conn: &Connection,
    resolved: &mut HashMap<String, i64>,
    created: &mut HashMap<String, i64>,
    existing_count: usize,
    name: &str,
    lang: Language,
) -> std::result::Result<i64, DbError> {
    let key = name.trim().to_lowercase();
    if let Some(id) = resolved.get(&key) {
        return Ok(*id);
    }
    if let Some(id) = created.get(&key) {
        return Ok(*id);
    }

    let ordinal = existing_count + created.len();
    let color = categories::default_colour_for(name, lang)
        .unwrap_or_else(|| categories::COLORS[ordinal % categories::COLORS.len()]);
    conn.execute(
        "INSERT INTO categories (name, color, budget_limit_minor, is_tax_relevant, is_transfer)
         VALUES (?1, ?2, NULL, 0, 0)",
        params![name, color],
    )?;
    let id = conn.last_insert_rowid();
    created.insert(key, id);
    Ok(id)
}

fn load_matcher(conn: &Connection) -> std::result::Result<RuleMatcher, DbError> {
    let mut statement = conn.prepare(
        "SELECT pattern, category_id, match_field, direction
         FROM rules WHERE auto_apply = 1 ORDER BY priority DESC, id",
    )?;
    let rows = statement.query_map([], |row| {
        Ok((
            row.get::<_, String>(0)?,
            row.get::<_, i64>(1)?,
            row.get::<_, String>(2)?,
            row.get::<_, String>(3)?,
        ))
    })?;

    let mut specs = Vec::new();
    for row in rows {
        let (pattern, category_id, field, direction) = row?;
        specs.push(RuleSpec {
            pattern,
            category_id: CategoryId(category_id),
            field: RuleField::from_stored(&field).ok_or_else(|| invalid_column(2, "match_field", &field))?,
            direction: RuleDirection::from_stored(&direction)
                .ok_or_else(|| invalid_column(3, "direction", &direction))?,
        });
    }

    Ok(RuleMatcher::new(specs))
}

fn invalid_column(index: usize, name: &str, value: &str) -> DbError {
    DbError::Io(std::io::Error::other(format!(
        "unknown {name} {value:?} in column {index}"
    )))
}

#[cfg(test)]
mod tests {
    use time::macros::date;
    use time::{Date, OffsetDateTime};

    use super::*;
    use crate::model::{AccountType, NewAccount, NewCategory, NewTransaction};
    use crate::money::{Currency, Money};
    use crate::repo::categories;
    use crate::secret::{Passphrase, PassphrasePurpose};

    const HEADER: &str = "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply";

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn parse(lines: &[&str]) -> ParsedRuleFile {
        RuleFile::parse(lines.join("\n").as_bytes(), Language::En).unwrap()
    }

    fn row(
        category: &str,
        pattern: &str,
        search_in: &str,
        applies_to: &str,
        priority: &str,
        auto_apply: &str,
    ) -> String {
        [category, pattern, search_in, applies_to, priority, auto_apply].join("\t")
    }

    fn category(db: &Database, name: &str) -> CategoryId {
        categories::create(
            db,
            NewCategory {
                name: name.to_owned(),
                color: "#3BC7B0".to_owned(),
                budget_limit: None,
                is_tax_relevant: false,
                is_transfer: false,
            },
        )
        .unwrap()
    }

    fn rule(
        db: &Database,
        pattern: &str,
        category_id: CategoryId,
        priority: i64,
        field: RuleField,
        direction: RuleDirection,
    ) {
        rule_with(db, pattern, category_id, priority, true, field, direction);
    }

    fn rule_with(
        db: &Database,
        pattern: &str,
        category_id: CategoryId,
        priority: i64,
        auto_apply: bool,
        field: RuleField,
        direction: RuleDirection,
    ) {
        repo::rules::create(
            db,
            crate::model::NewRule {
                pattern: pattern.to_owned(),
                category_id,
                priority,
                auto_apply,
                match_field: field,
                direction,
                tax_relevant: None,
            },
        )
        .unwrap();
    }

    fn account(db: &Database) -> crate::model::AccountId {
        repo::accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: None,
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

    #[allow(clippy::too_many_arguments)]
    fn txn(
        db: &Database,
        account_id: crate::model::AccountId,
        day: Date,
        amount: &str,
        partner: Option<&str>,
        purpose: Option<&str>,
        category_id: Option<CategoryId>,
    ) -> crate::model::TransactionId {
        static NEXT_HASH: std::sync::atomic::AtomicI64 = std::sync::atomic::AtomicI64::new(0);
        let hash = format!("hash-{}", NEXT_HASH.fetch_add(1, std::sync::atomic::Ordering::Relaxed));
        let amount = crate::money::parse_decimal(amount, Currency::EUR).unwrap();
        repo::transactions::insert_imported(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount,
                balance_after: None,
                partner_name: partner.map(str::to_owned),
                purpose: purpose.map(str::to_owned),
                category_id,
                import_hash: hash,
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        repo::transactions::list_for_account(db, account_id, None, None)
            .unwrap()
            .into_iter()
            .max_by_key(|transaction| transaction.id.0)
            .unwrap()
            .id
    }

    fn audit_details(db: &Database, action: &str) -> Vec<String> {
        audit::recent(db, 1000)
            .unwrap()
            .into_iter()
            .filter(|entry| entry.action == action)
            .map(|entry| entry.details)
            .collect()
    }

    fn preview(db: &Database, parsed: &ParsedRuleFile) -> RuleImportPreview {
        RuleImportService::preview(db, parsed).unwrap()
    }

    fn apply(db: &Database, preview: &RuleImportPreview, categorize: bool) -> RuleImportResult {
        RuleImportService::apply(db, preview, categorize, Language::En).unwrap()
    }

    #[test]
    fn existing_categories_are_matched_case_insensitively_and_nothing_is_created_for_them() {
        let (_temp, db) = database();
        category(&db, "groceries");

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Groceries", "beispiel markt", "any", "any", "50", "true"),
                &row("  Groceries  ", "musterladen", "any", "any", "50", "true"),
            ]),
        );

        assert_eq!(
            preview.categories_to_create,
            Vec::<String>::new(),
            "Groceries exists, in another spelling, so it is not created again"
        );
        assert_eq!(preview.new_rules.len(), 2);
        apply(&db, &preview, false);
        assert_eq!(
            categories::list(&db)
                .unwrap()
                .into_iter()
                .map(|category| category.name)
                .collect::<Vec<_>>(),
            vec!["groceries"],
            "the existing spelling wins"
        );
    }

    #[test]
    fn a_duplicate_of_an_existing_rule_is_skipped() {
        let (_temp, db) = database();
        let groceries = category(&db, "Groceries");
        rule(
            &db,
            "beispiel markt",
            groceries,
            30,
            RuleField::Partner,
            RuleDirection::Expense,
        );

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Groceries", "beispiel markt", "partner", "expense", "30", "false"),
            ]),
        );
        let result = apply(&db, &preview, false);

        assert_eq!(preview.duplicates, 1);
        assert!(preview.new_rules.is_empty() && preview.conflicts.is_empty());
        assert_eq!(result.rules, 0, "nothing new is created");
        assert_eq!(repo::rules::list(&db).unwrap().len(), 1);
    }

    #[test]
    fn a_conflict_is_reported_and_the_existing_rule_stays_unchanged() {
        let (_temp, db) = database();
        let groceries = category(&db, "Groceries");
        rule(&db, "beispiel markt", groceries, 30, RuleField::Any, RuleDirection::Any);

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Household", "beispiel markt", "any", "any", "50", "true"),
            ]),
        );
        let result = apply(&db, &preview, false);

        assert_eq!(
            preview.conflicts,
            vec![RuleImportConflict {
                line: 3,
                pattern: "beispiel markt".to_owned(),
                file_category: "Household".to_owned(),
                existing_category: "Groceries".to_owned(),
            }]
        );
        assert!(preview.new_rules.is_empty());
        assert_eq!(result.rules, 0, "existing rules are never changed");
        let rules = repo::rules::list(&db).unwrap();
        assert_eq!(rules.len(), 1);
        assert_eq!(rules[0].category_id, groceries);
        assert_eq!(
            preview.categories_to_create,
            vec!["Household".to_owned()],
            "the file's category is created but no rule points at it"
        );
    }

    #[test]
    fn identical_rows_of_the_same_file_are_duplicates_rows_with_another_category_conflict_with_them() {
        let (_temp, db) = database();
        let file = parse(&[
            "# FinDesk rules v1",
            HEADER,
            &row("Groceries", "rewe", "any", "any", "50", "true"),
            &row("Groceries", "rewe", "any", "any", "50", "true"),
            &row("Cash", "rewe", "any", "any", "50", "true"),
            &row("Groceries", "beispiel markt", "any", "any", "50", "true"),
        ]);

        let preview = preview(&db, &file);

        assert_eq!(preview.duplicates, 1);
        assert_eq!(
            preview.conflicts,
            vec![RuleImportConflict {
                line: 5,
                pattern: "rewe".to_owned(),
                file_category: "Cash".to_owned(),
                existing_category: "Groceries".to_owned(),
            }]
        );
        assert_eq!(
            preview.new_rules.iter().map(|row| row.line).collect::<Vec<_>>(),
            vec![3, 6]
        );
    }

    #[test]
    fn match_count_respects_priority_order_search_in_applies_to_the_existing_rules_and_manual_rules() {
        let (_temp, db) = database();
        let salary = category(&db, "Salary");
        rule(
            &db,
            "beispielpay",
            salary,
            5,
            RuleField::Partner,
            RuleDirection::Expense,
        );
        let account = account(&db);
        let today = date!(2026 - 10 - 24);
        // Matched by the existing rule (partner, expense)
        txn(
            &db,
            account,
            today,
            "-4.50",
            Some("BeispielPay"),
            Some("Kartenzahlung"),
            None,
        );
        // Matched by the file's income rule (partner, income)
        txn(
            &db,
            account,
            today,
            "40.00",
            Some("Muster GmbH"),
            Some("Rueckzahlung"),
            None,
        );
        // Not matched: the INCOME rule ignores expenses
        txn(&db, account, today, "-40.00", Some("Muster GmbH"), None, None);
        // Matched by the file's purpose rule only (purpose, income); a partner rule does not match it
        txn(
            &db,
            account,
            today,
            "2500.00",
            Some("BeispielPay"),
            Some("Gehalt Beispiel GmbH"),
            None,
        );
        // Matched only by the file's manual rule, which never takes part in the simulation
        txn(&db, account, today, "-9.99", Some("Musterladen"), None, None);
        // Uncategorized income that no rule matches
        txn(&db, account, today, "1.23", Some("BeispielPay"), None, None);

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Salary", "muster gmbh", "partner", "income", "90", "true"),
                &row("Other income", "gehalt", "purpose", "income", "40", "true"),
                &row("Groceries", "musterladen", "partner", "any", "60", "false"),
            ]),
        );

        assert_eq!(
            preview.match_count, 3,
            "the partner expense (expense-only rule) and the unmatched income are not counted"
        );
        assert_eq!(
            preview.new_rules.len(),
            3,
            "rules are previewed even when they do not participate in the match"
        );
    }

    #[test]
    fn applying_creates_the_categories_and_rules_with_all_their_fields_and_categorizes_only_uncategorized_bookings() {
        let (_temp, db) = database();
        let groceries = category(&db, "Groceries");
        let stuff = category(&db, "Stuff");
        let account = account(&db);
        let today = date!(2026 - 10 - 24);
        txn(&db, account, today, "-42.00", Some("Musterladen"), None, None);
        let pre_categorized = txn(&db, account, today, "-7.77", Some("Musterladen"), None, Some(stuff));
        txn(&db, account, today, "1500.00", Some("Erika Mustermann"), None, None);
        txn(&db, account, today, "-0.01", Some("Nirwana"), None, None);

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Groceries", "musterladen", "partner", "expense", "60", "true"),
                &row("Salary", "erika mustermann", "partner", "income", "70", "true"),
            ]),
        );
        let result = apply(&db, &preview, true);

        assert_eq!(result.categories, 1);
        assert_eq!(result.rules, 2);
        assert_eq!(result.categorized, 2);

        let salary = categories::list(&db)
            .unwrap()
            .into_iter()
            .find(|category| category.name == "Salary")
            .unwrap();
        assert_eq!(
            salary.color,
            categories::ENTRIES
                .iter()
                .find(|(key, _, _)| *key == crate::i18n::Key::DefaultCategorySalary)
                .unwrap()
                .1,
            "a file category named like a default category in the UI language gets that default colour"
        );

        let rules = repo::rules::list(&db).unwrap();
        assert_eq!(rules.len(), 2);
        let salary_rule = rules.iter().find(|rule| rule.pattern == "erika mustermann").unwrap();
        assert_eq!(salary_rule.category_id, salary.id);
        assert_eq!(salary_rule.priority, 70);
        assert!(salary_rule.auto_apply);
        assert_eq!(salary_rule.match_field, RuleField::Partner);
        assert_eq!(salary_rule.direction, RuleDirection::Income);
        assert_eq!(
            rules
                .iter()
                .find(|rule| rule.pattern == "musterladen")
                .unwrap()
                .priority,
            60
        );

        let transactions: HashMap<crate::model::TransactionId, crate::model::Transaction> =
            repo::transactions::list_for_account(&db, account, None, None)
                .unwrap()
                .into_iter()
                .map(|transaction| (transaction.id, transaction))
                .collect();
        assert_eq!(transactions.len(), 4);
        assert_eq!(
            transactions
                .values()
                .find(|transaction| transaction.id != pre_categorized
                    && transaction.partner_name.as_deref() == Some("Musterladen"))
                .unwrap()
                .category_id,
            Some(groceries),
            "the uncategorized expense gets the rule's category"
        );
        assert_eq!(
            transactions.get(&pre_categorized).unwrap().category_id,
            Some(stuff),
            "a booking that already has a category keeps it"
        );
        assert_eq!(
            transactions
                .values()
                .find(|transaction| transaction.partner_name.as_deref() == Some("Erika Mustermann"))
                .unwrap()
                .category_id,
            Some(salary.id)
        );
        assert_eq!(
            transactions
                .values()
                .find(|transaction| transaction.partner_name.as_deref() == Some("Nirwana"))
                .unwrap()
                .category_id,
            None,
            "an unmatched booking stays uncategorized"
        );
    }

    #[test]
    fn categorize_false_creates_categories_and_rules_but_touches_no_booking() {
        let (_temp, db) = database();
        category(&db, "Groceries");
        let account = account(&db);
        txn(
            &db,
            account,
            date!(2026 - 10 - 24),
            "-42.00",
            Some("Musterladen"),
            None,
            None,
        );

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Groceries", "musterladen", "partner", "expense", "60", "true"),
            ]),
        );
        let result = apply(&db, &preview, false);

        assert_eq!(result.categorized, 0);
        let bookings = repo::transactions::list_for_account(&db, account, None, None).unwrap();
        assert_eq!(bookings.len(), 1);
        assert_eq!(bookings[0].category_id, None);
        assert_eq!(repo::rules::list(&db).unwrap().len(), 1);
    }

    #[test]
    fn importing_the_same_file_twice_adds_nothing_the_second_time() {
        let (_temp, db) = database();
        category(&db, "Groceries");
        let file = format!(
            "# FinDesk rules v1\n{HEADER}\n{}\n",
            row("Groceries", "musterladen", "partner", "expense", "60", "true")
        );
        let parsed = RuleFile::parse(file.as_bytes(), Language::En).unwrap();

        let first = apply(&db, &preview(&db, &parsed), true);
        assert_eq!(first.rules, 1);

        let second_preview = preview(&db, &parsed);
        assert!(second_preview.new_rules.is_empty() && second_preview.categories_to_create.is_empty());
        assert_eq!(second_preview.duplicates, 1);
        let second = apply(&db, &second_preview, true);
        assert_eq!(second.categories, 0);
        assert_eq!(second.rules, 0);
        assert_eq!(second.categorized, 0);
        assert_eq!(repo::rules::list(&db).unwrap().len(), 1);
        assert_eq!(categories::list(&db).unwrap().len(), 1);
    }

    #[test]
    fn import_and_export_leave_audit_entries_with_counts_only() {
        let (temp, db) = database();
        let groceries = category(&db, "Groceries");
        txn(
            &db,
            account(&db),
            date!(2026 - 10 - 24),
            "-4.50",
            Some("Musterladen"),
            None,
            None,
        );

        let preview = preview(
            &db,
            &parse(&[
                "# FinDesk rules v1",
                HEADER,
                &row("Groceries", "musterladen", "partner", "expense", "60", "true"),
            ]),
        );
        apply(&db, &preview, true);

        assert_eq!(
            audit_details(&db, "IMPORT_RULES"),
            vec!["Imported 1 rules, created 0 categories, categorized 1 transactions".to_owned()]
        );

        // A rule whose pattern holds a tab cannot be written and is counted
        rule(&db, "bad\tstock", groceries, 0, RuleField::Any, RuleDirection::Any);
        let written = RuleImportService::export(&db, &temp.path().join("rules.tsv")).unwrap();
        assert_eq!(written, 1);
        assert_eq!(
            audit_details(&db, "EXPORT_RULES"),
            vec!["Exported 1 rules, skipped 1".to_owned()]
        );
    }

    #[test]
    fn export_into_a_fresh_database_reproduces_the_same_rules() {
        let (temp, db) = database();
        let groceries = category(&db, "Groceries");
        let salary = category(&db, "Salary");
        let cash = category(&db, "Cash");
        rule(
            &db,
            "beispiel.?markt|musterladen",
            groceries,
            60,
            RuleField::Any,
            RuleDirection::Any,
        );
        rule(&db, "beispiel gmbh", cash, 20, RuleField::Partner, RuleDirection::Any);
        rule(
            &db,
            "beispiel gmbh",
            salary,
            70,
            RuleField::Partner,
            RuleDirection::Income,
        );
        rule_with(&db, "raw cash", cash, 0, false, RuleField::Purpose, RuleDirection::Any);
        // Cannot be written: pattern with a tab
        rule(&db, "bad\tstock", cash, 100, RuleField::Any, RuleDirection::Any);

        let original: Vec<_> = repo::rules::list(&db)
            .unwrap()
            .into_iter()
            .filter(|rule| rule.pattern != "bad\tstock")
            .collect();
        let file = temp.path().join("findesk-rules-2026-10-24.tsv");
        assert_eq!(RuleImportService::export(&db, &file).unwrap(), 4);

        // Fresh database: only the importer sees the exported file
        let (_fresh_temp, fresh) = database();
        let written = RuleFile::parse(&std::fs::read(&file).unwrap(), Language::En).unwrap();
        let preview = preview(&fresh, &written);
        assert_eq!(preview.new_rules.len(), 4);
        apply(&fresh, &preview, false);

        let restored: HashMap<(String, i64), crate::model::Rule> = repo::rules::list(&fresh)
            .unwrap()
            .into_iter()
            .map(|rule| ((rule.pattern.clone(), rule.priority), rule))
            .collect();
        for original in original {
            let copy = restored.get(&(original.pattern.clone(), original.priority)).unwrap();
            let original_name = categories::get(&db, original.category_id).unwrap().unwrap().name;
            let copy_name = categories::get(&fresh, copy.category_id).unwrap().unwrap().name;
            assert_eq!(original_name, copy_name);
            assert_eq!(original.priority, copy.priority);
            assert_eq!(original.auto_apply, copy.auto_apply);
            assert_eq!(original.match_field, copy.match_field);
            assert_eq!(original.direction, copy.direction);
        }
    }
}
