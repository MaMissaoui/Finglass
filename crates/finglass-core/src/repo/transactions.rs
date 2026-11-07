//! Transaction repository.

use std::collections::{BTreeMap, BTreeSet, HashSet};

use rusqlite::types::Value;
use rusqlite::{OptionalExtension, Row, params, params_from_iter};
use time::{Date, OffsetDateTime};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{
    AccountId, CategoryId, Direction, NewTransaction, RuleDirection, RuleField, TaxSource, Transaction,
    TransactionFilter, TransactionId, TransactionItem,
};
use crate::money::Money;
use crate::rules::matcher::{CompiledPattern, RuleMatcher, RuleSpec, compile};
use crate::rules::payment_providers::PaymentProviders;

use super::{
    RepoError, Result, format_date, format_timestamp, id_only, in_conn, parse_currency, parse_timestamp, to_int_bool,
    to_optional_int_bool,
};

/// Batch chunk size: SQLite's bound-parameter limit is at least 999, so 500 ids per statement is safe.
const CHUNK: usize = 500;

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

/// One auto-apply rule with a tax flag, loaded once per [`find_items`] call.
struct TaxRuleSpec {
    spec: RuleSpec,
    tax_relevant: bool,
    compiled: CompiledPattern,
}

fn tax_rules(conn: &rusqlite::Connection) -> Result<Vec<TaxRuleSpec>> {
    let mut statement = conn.prepare(
        "SELECT pattern, category_id, priority, auto_apply, match_field, direction, tax_relevant
         FROM rules
         WHERE auto_apply = 1 AND tax_relevant IS NOT NULL
         ORDER BY priority DESC, id ASC",
    )?;
    let mut rows = statement.query([])?;
    let mut rules = Vec::new();

    while let Some(row) = rows.next()? {
        let pattern: String = row.get(0)?;
        let match_field_text: String = row.get(4)?;
        let direction_text: String = row.get(5)?;
        let match_field = RuleField::from_stored(&match_field_text)
            .ok_or_else(|| RepoError::Invalid(format!("unknown rule match field {match_field_text:?}")))?;
        let direction = RuleDirection::from_stored(&direction_text)
            .ok_or_else(|| RepoError::Invalid(format!("unknown rule direction {direction_text:?}")))?;

        rules.push(TaxRuleSpec {
            compiled: compile(&pattern),
            spec: RuleSpec {
                pattern,
                category_id: CategoryId(row.get(1)?),
                field: match_field,
                direction,
            },
            tax_relevant: super::from_int_bool(row.get(6)?),
        });
    }

    Ok(rules)
}

/// Escapes SQL `LIKE` wildcards so user text is matched literally.
fn escape_like(text: &str) -> String {
    let mut escaped = String::with_capacity(text.len());
    for character in text.chars() {
        match character {
            '\\' | '%' | '_' => {
                escaped.push('\\');
                escaped.push(character);
            }
            _ => escaped.push(character),
        }
    }
    escaped
}

fn placeholders(count: usize) -> String {
    let mut text = String::from("?");
    for _ in 1..count {
        text.push_str(", ?");
    }
    text
}

fn document_counts(conn: &rusqlite::Connection, ids: &[TransactionId]) -> Result<BTreeMap<i64, usize>> {
    let mut counts = BTreeMap::new();
    for chunk in ids.chunks(CHUNK) {
        let sql = format!(
            "SELECT transaction_id FROM documents WHERE transaction_id IN ({})",
            placeholders(chunk.len())
        );
        let mut statement = conn.prepare(&sql)?;
        let mut rows = statement.query(params_from_iter(chunk.iter().map(|id| id.0)))?;

        while let Some(row) = rows.next()? {
            *counts.entry(row.get::<_, i64>(0)?).or_insert(0) += 1;
        }
    }
    Ok(counts)
}

/// Filtered, newest-first snapshots safe to use outside a transaction.
///
/// The text search matches partner name and purpose literally (LIKE wildcards in the user text are
/// escaped), and the tax-relevant filter uses the effective value: override, rule, then category.
/// When `tax_relevant_only` is set, `limit` applies after that filter.
pub fn find_items(db: &Database, filter: &TransactionFilter, limit: Option<usize>) -> Result<Vec<TransactionItem>> {
    in_conn(db, |conn| {
        let mut sql = String::from(
            "SELECT t.id, t.account_id, t.booking_date, t.amount_minor, t.currency, t.balance_after_minor,
                    t.partner_name, t.purpose, t.category_id, t.import_hash, t.created_at, t.is_new, t.tax_override,
                    a.iban, c.name, c.color, c.is_tax_relevant, c.is_transfer
             FROM transactions t
             JOIN accounts a ON a.id = t.account_id
             LEFT JOIN categories c ON c.id = t.category_id
             WHERE 1 = 1",
        );
        let mut values: Vec<Value> = Vec::new();

        if let Some(start) = filter.start_date {
            sql.push_str(" AND t.booking_date >= ?");
            values.push(Value::Text(format_date(start)?));
        }
        if let Some(end) = filter.end_date {
            sql.push_str(" AND t.booking_date <= ?");
            values.push(Value::Text(format_date(end)?));
        }
        if let Some(account_id) = filter.account_id {
            sql.push_str(" AND t.account_id = ?");
            values.push(Value::Integer(account_id.0));
        }
        if let Some(account_ids) = &filter.account_ids {
            if account_ids.is_empty() {
                sql.push_str(" AND 1 = 0");
            } else {
                sql.push_str(&format!(" AND t.account_id IN ({})", placeholders(account_ids.len())));
                values.extend(account_ids.iter().map(|id| Value::Integer(id.0)));
            }
        }
        if let Some(category_id) = filter.category_id {
            sql.push_str(" AND t.category_id = ?");
            values.push(Value::Integer(category_id.0));
        }
        if let Some(currency) = filter.currency {
            sql.push_str(" AND t.currency = ?");
            values.push(Value::Text(currency.as_str().to_owned()));
        }
        if filter.uncategorized_only {
            sql.push_str(" AND t.category_id IS NULL");
        }
        match filter.direction {
            Direction::Income => sql.push_str(" AND t.amount_minor > 0"),
            Direction::Expenses => sql.push_str(" AND t.amount_minor < 0"),
            Direction::All => {}
        }
        if let Some(min) = filter.min_amount {
            sql.push_str(" AND abs(t.amount_minor) >= ?");
            values.push(Value::Integer(
                i64::try_from(min.minor.unsigned_abs()).unwrap_or(i64::MAX),
            ));
        }
        if let Some(max) = filter.max_amount {
            sql.push_str(" AND abs(t.amount_minor) <= ?");
            values.push(Value::Integer(
                i64::try_from(max.minor.unsigned_abs()).unwrap_or(i64::MAX),
            ));
        }
        if let Some(text) = filter.text.as_deref().map(str::trim).filter(|text| !text.is_empty()) {
            let pattern = format!("%{}%", escape_like(text));
            sql.push_str(" AND (t.partner_name LIKE ? ESCAPE '\\' OR t.purpose LIKE ? ESCAPE '\\')");
            values.push(Value::Text(pattern.clone()));
            values.push(Value::Text(pattern));
        }

        sql.push_str(" ORDER BY t.booking_date DESC, t.id DESC");
        if let Some(limit) = limit
            && !filter.tax_relevant_only
        {
            sql.push_str(" LIMIT ?");
            values.push(Value::Integer(i64::try_from(limit).unwrap_or(i64::MAX)));
        }

        let tax_rules = tax_rules(conn)?;
        let mut statement = conn.prepare(&sql)?;
        let mut rows = statement.query(params_from_iter(values.iter()))?;

        struct Base {
            txn: Transaction,
            iban: Option<String>,
            category_name: Option<String>,
            category_color: Option<String>,
            category_tax_relevant: bool,
            is_transfer: bool,
        }

        let mut bases = Vec::new();
        while let Some(row) = rows.next()? {
            let currency_text: String = row.get(4)?;
            let currency = parse_currency(&currency_text)?;
            bases.push(Base {
                txn: Transaction {
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
                },
                iban: row.get(13)?,
                category_name: row.get(14)?,
                category_color: row.get(15)?,
                category_tax_relevant: row
                    .get::<_, Option<i64>>(16)?
                    .map(super::from_int_bool)
                    .unwrap_or(false),
                is_transfer: row
                    .get::<_, Option<i64>>(17)?
                    .map(super::from_int_bool)
                    .unwrap_or(false),
            });
        }

        let ids: Vec<TransactionId> = bases.iter().map(|base| base.txn.id).collect();
        let counts = document_counts(conn, &ids)?;

        let mut items = Vec::with_capacity(bases.len());
        for base in bases {
            let txn = base.txn;
            let category_rule: Option<(bool, String)> = if txn.tax_override.is_none() {
                let category_rules: Vec<&TaxRuleSpec> = tax_rules
                    .iter()
                    .filter(|rule| Some(rule.spec.category_id) == txn.category_id)
                    .collect();
                let specs: Vec<RuleSpec> = category_rules.iter().map(|rule| rule.spec.clone()).collect();
                let tests: Vec<CompiledPattern> = category_rules.iter().map(|rule| rule.compiled.clone()).collect();
                let shop_match = PaymentProviders::shop_match(txn.purpose.as_deref(), txn.partner_name.as_deref());
                RuleMatcher::select(
                    &specs,
                    &tests,
                    txn.purpose.as_deref(),
                    txn.partner_name.as_deref(),
                    Some(rust_decimal::Decimal::from(txn.amount.minor)),
                    shop_match.as_ref(),
                )
                .map(|index| {
                    (
                        category_rules[index].tax_relevant,
                        category_rules[index].spec.pattern.clone(),
                    )
                })
            } else {
                None
            };

            let (is_tax_relevant, tax_source, tax_rule_pattern) =
                match (txn.tax_override, category_rule, txn.category_id) {
                    (Some(value), _, _) => (value, TaxSource::Manual, None),
                    (None, Some((tax_relevant, pattern)), _) => (tax_relevant, TaxSource::Rule, Some(pattern)),
                    (None, None, Some(_)) => (base.category_tax_relevant, TaxSource::Category, None),
                    (None, None, None) => (false, TaxSource::None, None),
                };

            items.push(TransactionItem {
                id: txn.id,
                account_id: txn.account_id,
                account_iban: base.iban,
                date: txn.booking_date,
                amount: txn.amount,
                balance_after: txn.balance_after,
                currency: txn.amount.currency,
                partner_name: txn.partner_name,
                purpose: txn.purpose,
                category_id: txn.category_id,
                category_name: base.category_name,
                category_color: base.category_color,
                is_tax_relevant,
                document_count: counts.get(&txn.id.0).copied().unwrap_or(0),
                is_new: txn.is_new,
                is_transfer: base.is_transfer,
                tax_source,
                tax_rule_pattern,
                tax_override: txn.tax_override,
            });
        }

        if filter.tax_relevant_only {
            items.retain(|item| item.is_tax_relevant);
            if let Some(limit) = limit {
                items.truncate(limit);
            }
        }

        Ok(items)
    })
}

/// Applies every given category in one batch and returns how many rows actually changed.
///
/// A booking already sitting on its target category is skipped, not overwritten.
pub fn update_categories(db: &Database, changes: &[(TransactionId, Option<CategoryId>)]) -> Result<usize> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let mut updated = 0;

        for (id, category_id) in changes {
            let current: Option<Option<i64>> = transaction
                .query_row("SELECT category_id FROM transactions WHERE id = ?1", [id.0], |row| {
                    row.get(0)
                })
                .optional()?;
            let Some(current) = current else {
                continue;
            };

            let target = category_id.map(|category| category.0);
            if current != target {
                transaction.execute(
                    "UPDATE transactions SET category_id = ?1 WHERE id = ?2",
                    params![target, id.0],
                )?;
                updated += 1;
            }
        }

        audit::log_conn(
            &transaction,
            "transaction.update_categories",
            Actor::User,
            &format!("count={updated}"),
        )?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Deletes every given booking, remembering each hash so a re-import cannot bring it back.
///
/// Documents linked to a deleted booking are unlinked, not deleted. Unknown ids are ignored.
pub fn delete_all(db: &Database, ids: &[TransactionId]) -> Result<usize> {
    if ids.is_empty() {
        return Ok(0);
    }

    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let mut deleted = 0;

        for chunk in ids.chunks(CHUNK) {
            let select_sql = format!(
                "SELECT account_id, import_hash FROM transactions WHERE id IN ({})",
                placeholders(chunk.len())
            );
            let mut statement = transaction.prepare(&select_sql)?;
            let mut rows = statement.query(params_from_iter(chunk.iter().map(|id| id.0)))?;
            let mut pairs: HashSet<(i64, String)> = HashSet::new();
            while let Some(row) = rows.next()? {
                pairs.insert((row.get(0)?, row.get(1)?));
            }

            for (account_id, import_hash) in pairs {
                let already: Option<i64> = transaction
                    .query_row(
                        "SELECT 1 FROM deleted_bookings WHERE account_id = ?1 AND import_hash = ?2 LIMIT 1",
                        params![account_id, import_hash],
                        |row| row.get(0),
                    )
                    .optional()?;
                if already.is_none() {
                    let deleted_at = format_timestamp(OffsetDateTime::now_utc())?;
                    transaction.execute(
                        "INSERT INTO deleted_bookings (account_id, import_hash, deleted_at) VALUES (?1, ?2, ?3)",
                        params![account_id, import_hash, deleted_at],
                    )?;
                }
            }

            let ids_sql = format!("({})", placeholders(chunk.len()));
            let update_sql = format!("UPDATE documents SET transaction_id = NULL WHERE transaction_id IN {ids_sql}");
            transaction.execute(&update_sql, params_from_iter(chunk.iter().map(|id| id.0)))?;

            let delete_sql = format!("DELETE FROM transactions WHERE id IN {ids_sql}");
            deleted += transaction.execute(&delete_sql, params_from_iter(chunk.iter().map(|id| id.0)))?;
        }

        audit::log_conn(
            &transaction,
            "transaction.delete_all",
            Actor::User,
            &format!("count={deleted}"),
        )?;
        transaction.commit()?;

        Ok(deleted)
    })
}

/// Sets the manual tax override of every given booking and returns how many rows changed.
///
/// `value = None` resets them to automatic. Unknown ids are ignored.
pub fn set_tax_override(db: &Database, ids: &[TransactionId], value: Option<bool>) -> Result<usize> {
    if ids.is_empty() {
        return Ok(0);
    }

    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let mut changed = 0;

        for chunk in ids.chunks(CHUNK) {
            let sql = format!(
                "UPDATE transactions SET tax_override = ?1 WHERE id IN ({})",
                placeholders(chunk.len())
            );
            let mut values: Vec<Value> = vec![match value {
                Some(value) => Value::Integer(to_int_bool(value)),
                None => Value::Null,
            }];
            values.extend(chunk.iter().map(|id| Value::Integer(id.0)));
            changed += transaction.execute(&sql, params_from_iter(values.iter()))?;
        }

        audit::log_conn(
            &transaction,
            "transaction.set_tax_override",
            Actor::User,
            &format!("count={changed} value={value:?}"),
        )?;
        transaction.commit()?;

        Ok(changed)
    })
}

/// Lets the bookings deleted in an account be imported again, returning how many were remembered.
pub fn forget_deleted(db: &Database, account_id: AccountId) -> Result<usize> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let removed = transaction.execute("DELETE FROM deleted_bookings WHERE account_id = ?1", [account_id.0])?;
        audit::log_conn(
            &transaction,
            "transaction.forget_deleted",
            Actor::User,
            &format!("id={} count={removed}", account_id.0),
        )?;
        transaction.commit()?;

        Ok(removed)
    })
}

/// Lets an account's remembered deleted bookings be imported again, as the Accounts view does.
///
/// Ported from FinDesk's `AccountsViewModel.forgetDeletedBookings`: one transaction deletes the remembered pairs and
/// names the count in one audit entry with the account's last four characters. When nothing is remembered, nothing is
/// deleted and no audit entry is written; the caller shows the "nothing remembered" message.
pub fn forget_deleted_bookings(db: &Database, account_id: AccountId) -> Result<usize> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let removed = transaction.execute("DELETE FROM deleted_bookings WHERE account_id = ?1", [account_id.0])?;

        if removed > 0 {
            let iban: Option<String> = transaction
                .query_row("SELECT iban FROM accounts WHERE id = ?1", [account_id.0], |row| {
                    row.get(0)
                })
                .optional()?;
            let last4 = iban
                .as_deref()
                .map(|iban| {
                    iban.chars()
                        .rev()
                        .take(4)
                        .collect::<String>()
                        .chars()
                        .rev()
                        .collect::<String>()
                })
                .unwrap_or_default();
            audit::log_conn(
                &transaction,
                "FORGET_DELETED_BOOKINGS",
                Actor::User,
                &format!("Account {last4}: {removed} deleted bookings may be imported again"),
            )?;
        }

        transaction.commit()?;
        Ok(removed)
    })
}

/// Whether an account has any booking yet.
pub fn has_bookings(db: &Database, account_id: AccountId) -> Result<bool> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare("SELECT 1 FROM transactions WHERE account_id = ?1 LIMIT 1")?;
        Ok(statement.exists([account_id.0])?)
    })
}

/// New (unread) bookings per account; accounts with none are absent from the map.
pub fn count_new(db: &Database) -> Result<BTreeMap<AccountId, i64>> {
    in_conn(db, |conn| {
        let mut statement =
            conn.prepare("SELECT account_id, count(*) FROM transactions WHERE is_new = 1 GROUP BY account_id")?;
        let mut rows = statement.query([])?;
        let mut counts = BTreeMap::new();

        while let Some(row) = rows.next()? {
            counts.insert(AccountId(row.get(0)?), row.get(1)?);
        }

        Ok(counts)
    })
}

/// Marks every booking of `account_ids` as read (`None` = all accounts; an empty set does nothing).
pub fn mark_read(db: &Database, account_ids: Option<&BTreeSet<AccountId>>) -> Result<()> {
    if account_ids.is_some_and(BTreeSet::is_empty) {
        return Ok(());
    }

    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = if let Some(account_ids) = account_ids {
            let sql = format!(
                "UPDATE transactions SET is_new = 0 WHERE is_new = 1 AND account_id IN ({})",
                placeholders(account_ids.len())
            );
            transaction.execute(&sql, params_from_iter(account_ids.iter().map(|id| id.0)))?
        } else {
            transaction.execute("UPDATE transactions SET is_new = 0 WHERE is_new = 1", [])?
        };

        audit::log_conn(
            &transaction,
            "transaction.mark_read",
            Actor::User,
            &format!("count={changed}"),
        )?;
        transaction.commit()?;

        Ok(())
    })
}

/// The oldest booking date, optionally only in one currency. `None` when there is no booking.
pub fn oldest_date(db: &Database, currency: Option<crate::money::Currency>) -> Result<Option<Date>> {
    in_conn(db, |conn| {
        let oldest: Option<String> = if let Some(currency) = currency {
            conn.query_row(
                "SELECT min(booking_date) FROM transactions WHERE currency = ?1",
                [currency.as_str()],
                |row| row.get(0),
            )?
        } else {
            conn.query_row("SELECT min(booking_date) FROM transactions", [], |row| row.get(0))?
        };

        oldest.map(|text| super::parse_date(&text)).transpose()
    })
}

/// Date of each account's oldest booking, by account id; accounts without bookings are absent.
pub fn oldest_date_by_account(db: &Database) -> Result<BTreeMap<AccountId, Date>> {
    in_conn(db, |conn| {
        let mut statement =
            conn.prepare("SELECT account_id, min(booking_date) FROM transactions GROUP BY account_id")?;
        let mut rows = statement.query([])?;
        let mut dates = BTreeMap::new();

        while let Some(row) = rows.next()? {
            dates.insert(AccountId(row.get(0)?), super::parse_date(&row.get::<_, String>(1)?)?);
        }

        Ok(dates)
    })
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use time::macros::date;

    use super::*;
    use crate::model::{
        AccountType, NewAccount, NewCategory, NewRule, RuleDirection as ModelRuleDirection, RuleField as ModelRuleField,
    };
    use crate::money::Currency;
    use crate::repo::{accounts, categories, rules};
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

    fn money(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
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

    fn category(db: &Database, name: &str, tax_relevant: bool, is_transfer: bool) -> CategoryId {
        categories::create(
            db,
            NewCategory {
                name: name.to_owned(),
                color: "#FF5722".to_owned(),
                budget_limit: None,
                is_tax_relevant: tax_relevant,
                is_transfer,
            },
        )
        .unwrap()
    }

    #[allow(clippy::too_many_arguments)]
    fn add_full(
        db: &Database,
        account_id: AccountId,
        day: Date,
        minor: i64,
        partner: Option<&str>,
        purpose: Option<&str>,
        category_id: Option<CategoryId>,
        hash: &str,
        is_new: bool,
        tax_override: Option<bool>,
    ) -> TransactionId {
        insert_planned(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount: Money {
                    minor,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: partner.map(str::to_owned),
                purpose: purpose.map(str::to_owned),
                category_id,
                import_hash: hash.to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new,
                tax_override,
            }],
        )
        .unwrap();
        list_for_account(db, account_id, None, None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.import_hash == hash)
            .unwrap()
            .id
    }

    #[allow(clippy::too_many_arguments)]
    fn add(
        db: &Database,
        account_id: AccountId,
        day: Date,
        minor: i64,
        partner: Option<&str>,
        purpose: Option<&str>,
        category_id: Option<CategoryId>,
        hash: &str,
    ) -> TransactionId {
        add_full(
            db,
            account_id,
            day,
            minor,
            partner,
            purpose,
            category_id,
            hash,
            false,
            None,
        )
    }

    #[allow(clippy::too_many_arguments)]
    fn add_rule(
        db: &Database,
        pattern: &str,
        category_id: CategoryId,
        priority: i64,
        auto_apply: bool,
        match_field: ModelRuleField,
        direction: ModelRuleDirection,
        tax_relevant: Option<bool>,
    ) -> crate::model::RuleId {
        rules::create(
            db,
            NewRule {
                pattern: pattern.to_owned(),
                category_id,
                priority,
                auto_apply,
                match_field,
                direction,
                tax_relevant,
            },
        )
        .unwrap()
    }

    fn set_rule_tax(db: &Database, id: crate::model::RuleId, tax_relevant: Option<bool>) {
        let rule = rules::list(db).unwrap().into_iter().find(|rule| rule.id == id).unwrap();
        rules::update(
            db,
            id,
            NewRule {
                pattern: rule.pattern,
                category_id: rule.category_id,
                priority: rule.priority,
                auto_apply: rule.auto_apply,
                match_field: rule.match_field,
                direction: rule.direction,
                tax_relevant,
            },
        )
        .unwrap();
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

    #[test]
    fn items_expose_category_data_and_are_newest_first() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let food = category(&db, "Food", false, false);
        let office = category(&db, "Office", true, false);
        add(
            &db,
            account_id,
            date!(2026 - 03 - 01),
            -1000,
            Some("REWE"),
            Some("Groceries"),
            Some(food),
            "items-a",
        );
        add(
            &db,
            account_id,
            date!(2026 - 03 - 15),
            -9900,
            Some("Staples"),
            Some("Printer paper"),
            Some(office),
            "items-b",
        );
        add(
            &db,
            account_id,
            date!(2026 - 03 - 31),
            300_000,
            Some("Employer GmbH"),
            Some("Gehalt März"),
            None,
            "items-c",
        );
        add(
            &db,
            account_id,
            date!(2026 - 04 - 01),
            -500,
            None,
            Some("Kartenzahlung Kiosk"),
            None,
            "items-d",
        );

        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let rewe = items
            .iter()
            .find(|item| item.partner_name.as_deref() == Some("REWE"))
            .unwrap();
        assert_eq!(rewe.category_name.as_deref(), Some("Food"));
        assert!(!rewe.is_tax_relevant);
        assert_eq!(rewe.tax_source, TaxSource::Category);
        assert_eq!(items.first().unwrap().date, date!(2026 - 04 - 01), "newest first");
    }

    #[test]
    fn filters_by_date_direction_category_text_uncategorized_and_tax() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let food = category(&db, "Food", false, false);
        let office = category(&db, "Office", true, false);
        add(
            &db,
            account_id,
            date!(2026 - 03 - 01),
            -1000,
            Some("REWE"),
            Some("Groceries"),
            Some(food),
            "filter-a",
        );
        add(
            &db,
            account_id,
            date!(2026 - 03 - 15),
            -9900,
            Some("Staples"),
            Some("Printer paper"),
            Some(office),
            "filter-b",
        );
        add(
            &db,
            account_id,
            date!(2026 - 03 - 31),
            300_000,
            Some("Employer GmbH"),
            Some("Gehalt März"),
            None,
            "filter-c",
        );
        add(
            &db,
            account_id,
            date!(2026 - 04 - 01),
            -500,
            None,
            Some("Kartenzahlung Kiosk"),
            None,
            "filter-d",
        );

        let march = TransactionFilter {
            start_date: Some(date!(2026 - 03 - 01)),
            end_date: Some(date!(2026 - 03 - 31)),
            ..TransactionFilter::default()
        };
        assert_eq!(find_items(&db, &march, None).unwrap().len(), 3);
        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    direction: Direction::Income,
                    ..march.clone()
                },
                None
            )
            .unwrap()
            .len(),
            1
        );
        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    direction: Direction::Expenses,
                    ..march
                },
                None
            )
            .unwrap()
            .len(),
            2
        );
        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    category_id: Some(office),
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .iter()
            .map(|item| item.partner_name.clone())
            .collect::<Vec<_>>(),
            vec![Some("Staples".to_owned())]
        );
        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    uncategorized_only: true,
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .len(),
            2
        );
        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    tax_relevant_only: true,
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .len(),
            1
        );

        for text in ["kiosk", "printer", "KIOSK", "Printer"] {
            assert_eq!(
                find_items(
                    &db,
                    &TransactionFilter {
                        text: Some(text.to_owned()),
                        ..TransactionFilter::default()
                    },
                    None
                )
                .unwrap()
                .len(),
                1,
                "text {text:?} searches partner and purpose case-insensitively"
            );
        }

        let limited = find_items(&db, &TransactionFilter::default(), Some(2)).unwrap();
        assert_eq!(limited.len(), 2);
        assert_eq!(limited[0].amount.minor, -500, "limit keeps the newest rows");
    }

    #[test]
    fn amount_bounds_are_absolute_and_currency_keeps_totals_apart() {
        let (_temp, db) = database();
        let account_id = account(&db);
        add(
            &db,
            account_id,
            date!(2026 - 05 - 01),
            -500,
            Some("Small expense"),
            None,
            None,
            "amount-small",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 02),
            -1500,
            Some("Medium expense"),
            None,
            None,
            "amount-medium",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 03),
            2000,
            Some("Medium income"),
            None,
            None,
            "amount-income",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 04),
            5000,
            Some("Large income"),
            None,
            None,
            "amount-large",
        );

        let matched = find_items(
            &db,
            &TransactionFilter {
                min_amount: Some(money(1000)),
                max_amount: Some(money(2500)),
                ..TransactionFilter::default()
            },
            None,
        )
        .unwrap();
        assert_eq!(
            matched
                .iter()
                .map(|item| item.partner_name.clone().unwrap())
                .collect::<BTreeSet<_>>(),
            BTreeSet::from(["Medium expense".to_owned(), "Medium income".to_owned()]),
            "bounds compare absolute amounts, so income and expenses match alike"
        );

        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    max_amount: Some(money(500)),
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .len(),
            1
        );

        let usd = Currency::new("USD").unwrap();
        let dollar = accounts::create(
            &db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Dollar".to_owned()),
                iban: Some("DE44500105175407324931".to_owned()),
                account_number: None,
                currency: usd,
                account_type: AccountType::Checking,
                balance: Money {
                    minor: 0,
                    currency: usd,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap();
        insert_planned(
            &db,
            dollar,
            vec![NewTransaction {
                booking_date: date!(2026 - 05 - 05),
                amount: Money {
                    minor: -700,
                    currency: usd,
                },
                balance_after: None,
                partner_name: Some("US shop".to_owned()),
                purpose: None,
                category_id: None,
                import_hash: "amount-usd".to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();

        let only_usd = find_items(
            &db,
            &TransactionFilter {
                currency: Some(usd),
                ..TransactionFilter::default()
            },
            None,
        )
        .unwrap();
        assert_eq!(only_usd.len(), 1);
        assert_eq!(only_usd[0].currency, usd);
    }

    #[test]
    fn text_search_matches_like_wildcards_literally() {
        let (_temp, db) = database();
        let account_id = account(&db);
        add(
            &db,
            account_id,
            date!(2026 - 05 - 01),
            -100,
            Some("100% Shop"),
            None,
            None,
            "like-percent",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 02),
            -200,
            Some("100x Shop"),
            None,
            None,
            "like-x",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 03),
            -300,
            Some("under_score"),
            None,
            None,
            "like-underscore",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 04),
            -400,
            Some("underXscore"),
            None,
            None,
            "like-other",
        );
        add(
            &db,
            account_id,
            date!(2026 - 05 - 05),
            -500,
            Some("O'Brien"),
            None,
            None,
            "like-quote",
        );

        for (text, expected) in [
            ("%", vec!["like-percent"]),
            ("_", vec!["like-underscore"]),
            ("'", vec!["like-quote"]),
            ("O'Brien", vec!["like-quote"]),
        ] {
            let found = find_items(
                &db,
                &TransactionFilter {
                    text: Some(text.to_owned()),
                    ..TransactionFilter::default()
                },
                None,
            )
            .unwrap();
            let expected_hashes: Vec<&str> = expected;
            assert_eq!(
                found.iter().map(|item| item.id).collect::<BTreeSet<_>>(),
                expected_hashes
                    .iter()
                    .map(|hash| {
                        list_for_account(&db, account_id, None, None)
                            .unwrap()
                            .into_iter()
                            .find(|transaction| transaction.import_hash == *hash)
                            .unwrap()
                            .id
                    })
                    .collect::<BTreeSet<_>>(),
                "text {text:?} is matched literally"
            );
        }
    }

    #[test]
    fn account_ids_null_returns_all_a_set_returns_exactly_those_and_an_empty_set_none() {
        let (_temp, db) = database();
        let first = account(&db);
        let second = account(&db);
        add(
            &db,
            first,
            date!(2026 - 01 - 01),
            -100,
            Some("One"),
            None,
            None,
            "ids-a",
        );
        add(
            &db,
            second,
            date!(2026 - 01 - 02),
            -200,
            Some("Two"),
            None,
            None,
            "ids-b",
        );

        let all = find_items(&db, &TransactionFilter::default(), None).unwrap();
        assert_eq!(
            all.len(),
            find_items(
                &db,
                &TransactionFilter {
                    account_ids: None,
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .len(),
            "null means no restriction"
        );

        let only_second = find_items(
            &db,
            &TransactionFilter {
                account_ids: Some(BTreeSet::from([second])),
                ..TransactionFilter::default()
            },
            None,
        )
        .unwrap();
        assert_eq!(only_second.len(), 1);
        assert!(only_second.iter().all(|item| item.account_id == second));

        assert_eq!(
            find_items(
                &db,
                &TransactionFilter {
                    account_ids: Some(BTreeSet::from([first, second])),
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .len(),
            2
        );
        assert!(
            find_items(
                &db,
                &TransactionFilter {
                    account_ids: Some(BTreeSet::new()),
                    ..TransactionFilter::default()
                },
                None
            )
            .unwrap()
            .is_empty(),
            "an empty set matches nothing"
        );
    }

    #[test]
    fn count_new_is_per_account_and_mark_read_clears_the_requested_scope() {
        let (_temp, db) = database();
        let first = account(&db);
        let second = account(&db);
        add_full(
            &db,
            first,
            date!(2020 - 01 - 01),
            -100,
            Some("Old"),
            None,
            None,
            "new-old",
            true,
            None,
        );
        add_full(
            &db,
            first,
            date!(2026 - 01 - 01),
            -200,
            Some("Now"),
            None,
            None,
            "new-now",
            true,
            None,
        );
        add_full(
            &db,
            second,
            date!(2026 - 01 - 01),
            -300,
            Some("Other"),
            None,
            None,
            "new-other",
            true,
            None,
        );
        add(
            &db,
            second,
            date!(2026 - 01 - 01),
            -400,
            Some("Read"),
            None,
            None,
            "new-read",
        );

        let counts = count_new(&db).unwrap();
        assert_eq!(counts.get(&first), Some(&2));
        assert_eq!(counts.get(&second), Some(&1));

        mark_read(&db, Some(&BTreeSet::new())).unwrap();
        assert_eq!(count_new(&db).unwrap(), counts, "an empty set clears nothing");

        mark_read(&db, Some(&BTreeSet::from([first]))).unwrap();
        let after_first = count_new(&db).unwrap();
        assert_eq!(after_first.get(&first), None);
        assert_eq!(after_first.get(&second), Some(&1), "only the named account is cleared");

        mark_read(&db, None).unwrap();
        assert!(count_new(&db).unwrap().is_empty(), "null clears every account");
    }

    #[test]
    fn delete_all_deletes_exactly_the_ids_unlinks_documents_and_ignores_unknown_ids() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let keep = add(
            &db,
            account_id,
            date!(2026 - 02 - 01),
            -100,
            Some("Keep"),
            None,
            None,
            "delete-keep",
        );
        let gone = add(
            &db,
            account_id,
            date!(2026 - 02 - 02),
            -200,
            Some("Gone"),
            None,
            None,
            "delete-gone",
        );
        let other_gone = add(
            &db,
            account_id,
            date!(2026 - 02 - 03),
            -300,
            Some("Other"),
            None,
            None,
            "delete-other",
        );
        db.with_conn(|conn| {
            conn.execute(
                "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)
                 VALUES ('gone.pdf', 'gone.pdf', 'application/pdf', '2026-02-02T00:00:00Z', ?1),
                        ('keep.pdf', 'keep.pdf', 'application/pdf', '2026-02-02T00:00:00Z', ?2)",
                rusqlite::params![gone.0, keep.0],
            )?;
            Ok(())
        })
        .unwrap();

        assert_eq!(
            delete_all(&db, &[gone, other_gone, TransactionId(999_999)]).unwrap(),
            2,
            "the unknown id is ignored"
        );
        assert!(
            list_for_account(&db, account_id, None, None)
                .unwrap()
                .iter()
                .all(|txn| txn.id == keep)
        );
        let linked: Vec<Option<i64>> = db
            .with_conn(|conn| {
                let mut statement = conn.prepare("SELECT transaction_id FROM documents ORDER BY file_name")?;
                let rows = statement.query_map([], |row| row.get(0))?;
                let mut values = Vec::new();
                for row in rows {
                    values.push(row?);
                }
                Ok(values)
            })
            .unwrap();
        assert_eq!(
            linked,
            vec![None, Some(keep.0)],
            "the deleted booking's receipt is unlinked, not gone"
        );
        assert_eq!(delete_all(&db, &[]).unwrap(), 0, "an empty list does nothing");
    }

    #[test]
    fn delete_all_chunks_more_than_the_parameter_limit_in_one_call() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let mut ids = Vec::new();
        for index in 0..1_250 {
            ids.push(add(
                &db,
                account_id,
                date!(2026 - 01 - 01),
                -100,
                Some("Chunk"),
                None,
                None,
                &format!("chunk-{index}"),
            ));
        }
        let survivor = add(
            &db,
            account_id,
            date!(2026 - 01 - 02),
            -500,
            Some("Survivor"),
            None,
            None,
            "chunk-survivor",
        );

        assert_eq!(delete_all(&db, &ids).unwrap(), 1_250);
        let remaining: Vec<TransactionId> = list_for_account(&db, account_id, None, None)
            .unwrap()
            .into_iter()
            .map(|transaction| transaction.id)
            .collect();
        assert_eq!(remaining, vec![survivor]);
    }

    #[test]
    fn oldest_date_by_account_returns_each_accounts_oldest_date() {
        let (_temp, db) = database();
        let first = account(&db);
        let second = account(&db);
        let empty = account(&db);
        add(
            &db,
            first,
            date!(2026 - 05 - 10),
            -100,
            Some("One"),
            None,
            None,
            "oldest-a",
        );
        add(
            &db,
            first,
            date!(2026 - 03 - 02),
            -200,
            Some("Two"),
            None,
            None,
            "oldest-b",
        );
        add(
            &db,
            second,
            date!(2026 - 07 - 04),
            -300,
            Some("Three"),
            None,
            None,
            "oldest-c",
        );

        let oldest = oldest_date_by_account(&db).unwrap();
        assert_eq!(oldest.get(&first), Some(&date!(2026 - 03 - 02)));
        assert_eq!(oldest.get(&second), Some(&date!(2026 - 07 - 04)));
        assert!(!oldest.contains_key(&empty), "an account without bookings is absent");
        assert!(has_bookings(&db, first).unwrap());
        assert!(!has_bookings(&db, empty).unwrap());
    }

    #[test]
    fn update_categories_updates_all_given_rows_and_returns_the_count() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let groceries = category(&db, "Groceries", false, false);
        let loans = category(&db, "Loans", false, false);
        let transfer = category(&db, "Transfer", false, true);
        let first = add(
            &db,
            account_id,
            date!(2026 - 06 - 01),
            -990,
            Some("Store"),
            None,
            Some(groceries),
            "update-a",
        );
        let second = add(
            &db,
            account_id,
            date!(2026 - 06 - 02),
            -4500,
            Some("Sparkasse"),
            None,
            None,
            "update-b",
        );
        let third = add(
            &db,
            account_id,
            date!(2026 - 06 - 03),
            -12_000,
            Some("Loan"),
            None,
            Some(loans),
            "update-c",
        );

        assert_eq!(
            update_categories(
                &db,
                &[(first, Some(transfer)), (second, Some(groceries)), (third, None),]
            )
            .unwrap(),
            3
        );
        let rows = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let row = |id| rows.iter().find(|item| item.id == id).unwrap();
        assert_eq!(row(first).category_id, Some(transfer));
        assert_eq!(row(second).category_id, Some(groceries));
        assert_eq!(row(third).category_id, None, "null restores the uncategorized state");
    }

    #[test]
    fn update_categories_skips_targets_already_set_and_unknown_ids() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let groceries = category(&db, "Groceries", false, false);
        let loans = category(&db, "Loans", false, false);
        let already = add(
            &db,
            account_id,
            date!(2026 - 06 - 01),
            -990,
            Some("Store"),
            None,
            Some(groceries),
            "same-a",
        );
        let to_move = add(
            &db,
            account_id,
            date!(2026 - 06 - 02),
            -4500,
            Some("Bank"),
            None,
            Some(loans),
            "same-b",
        );

        assert_eq!(
            update_categories(&db, &[(already, Some(groceries)), (to_move, Some(groceries))]).unwrap(),
            1
        );
        assert_eq!(
            update_categories(
                &db,
                &[(already, Some(groceries)), (TransactionId(99_999), Some(groceries))]
            )
            .unwrap(),
            0,
            "unknown ids are skipped"
        );
    }

    #[test]
    fn delete_all_remembers_each_deleted_booking_once_and_forget_deleted_clears_them() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let first = add(
            &db,
            account_id,
            date!(2026 - 10 - 05),
            -500,
            Some("Cafe"),
            None,
            None,
            "remember-a",
        );
        let second = add(
            &db,
            account_id,
            date!(2026 - 10 - 05),
            -700,
            Some("Bakery"),
            None,
            None,
            "remember-b",
        );

        delete_all(&db, &[first, second]).unwrap();
        assert_eq!(
            deleted_hashes(&db, account_id).unwrap(),
            HashSet::from(["remember-a".to_owned(), "remember-b".to_owned()])
        );
        delete_all(&db, &[first, second]).unwrap();
        assert_eq!(
            deleted_hashes(&db, account_id).unwrap().len(),
            2,
            "a repeated delete does not record the pairs twice"
        );

        assert_eq!(forget_deleted(&db, account_id).unwrap(), 2);
        assert!(deleted_hashes(&db, account_id).unwrap().is_empty());
        assert_eq!(forget_deleted(&db, account_id).unwrap(), 0);
    }

    #[test]
    fn oldest_date_is_optional_and_filtered_by_currency() {
        let (_temp, db) = database();
        assert_eq!(oldest_date(&db, None).unwrap(), None, "an empty table");
        assert_eq!(oldest_date(&db, Some(Currency::EUR)).unwrap(), None);

        let euro = account(&db);
        let dollar = accounts::create(
            &db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Dollar".to_owned()),
                iban: Some("DE44500105175407324931".to_owned()),
                account_number: None,
                currency: Currency::new("USD").unwrap(),
                account_type: AccountType::Checking,
                balance: Money {
                    minor: 0,
                    currency: Currency::new("USD").unwrap(),
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap();
        add(
            &db,
            euro,
            date!(2026 - 08 - 02),
            -500,
            Some("Euro"),
            None,
            None,
            "oldest-eur",
        );
        insert_planned(
            &db,
            dollar,
            vec![NewTransaction {
                booking_date: date!(2026 - 01 - 10),
                amount: Money {
                    minor: -500,
                    currency: Currency::new("USD").unwrap(),
                },
                balance_after: None,
                partner_name: Some("Dollar".to_owned()),
                purpose: None,
                category_id: None,
                import_hash: "oldest-usd".to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();

        assert_eq!(oldest_date(&db, None).unwrap(), Some(date!(2026 - 01 - 10)));
        assert_eq!(
            oldest_date(&db, Some(Currency::EUR)).unwrap(),
            Some(date!(2026 - 08 - 02))
        );
        assert_eq!(oldest_date(&db, Some(Currency::new("GBP").unwrap())).unwrap(), None);
    }

    #[test]
    fn find_items_merges_document_counts_across_chunks() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let mut ids = Vec::new();
        for index in 0..1_250 {
            ids.push(add(
                &db,
                account_id,
                date!(2026 - 01 - 01),
                -100,
                Some("Shop"),
                None,
                None,
                &format!("count-{index}"),
            ));
        }
        for (name, id) in [
            ("first-a", ids[0]),
            ("first-b", ids[0]),
            ("middle", ids[600]),
            ("last", ids[1_249]),
        ] {
            db.with_conn(|conn| {
                conn.execute(
                    "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)
                     VALUES (?1, ?1, 'application/pdf', '2026-01-01T00:00:00Z', ?2)",
                    rusqlite::params![name, id.0],
                )?;
                Ok(())
            })
            .unwrap();
        }

        let items = find_items(
            &db,
            &TransactionFilter {
                account_id: Some(account_id),
                ..TransactionFilter::default()
            },
            None,
        )
        .unwrap();
        assert_eq!(items.len(), 1_250);
        let counts: BTreeMap<TransactionId, usize> = items.iter().map(|item| (item.id, item.document_count)).collect();
        assert_eq!(counts[&ids[0]], 2);
        assert_eq!(counts[&ids[600]], 1);
        assert_eq!(counts[&ids[1_249]], 1);
        assert_eq!(counts.values().sum::<usize>(), 4);
    }

    #[test]
    fn the_category_layer_decides_when_no_rule_or_override_does() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let tax_cat = category(&db, "Office supplies", true, false);
        let normal_cat = category(&db, "Groceries", false, false);
        let in_tax = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(tax_cat),
            "tax-cat-a",
        );
        let in_normal = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Anderer Shop"),
            None,
            Some(normal_cat),
            "tax-cat-b",
        );
        let uncategorized = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Dritter Shop"),
            None,
            None,
            "tax-cat-c",
        );

        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let item = |id| items.iter().find(|item| item.id == id).unwrap();
        assert!(item(in_tax).is_tax_relevant);
        assert_eq!(item(in_tax).tax_source, TaxSource::Category);
        assert_eq!(item(in_tax).tax_override, None);
        assert_eq!(item(in_tax).tax_rule_pattern, None);
        assert!(!item(in_normal).is_tax_relevant);
        assert_eq!(item(in_normal).tax_source, TaxSource::Category);
        assert!(
            !item(uncategorized).is_tax_relevant,
            "no category means not tax relevant"
        );
        assert_eq!(item(uncategorized).tax_source, TaxSource::None);
    }

    #[test]
    fn a_rule_with_a_tax_flag_decides_at_read_time_for_bookings_of_its_own_category() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let tax_cat = category(&db, "Office supplies", true, false);
        let normal_cat = category(&db, "Groceries", false, false);
        let flagged = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-rule-a",
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let unflagged = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -2000,
            Some("Anderer Shop"),
            None,
            Some(tax_cat),
            "tax-rule-b",
        );
        add_rule(
            &db,
            "Anderer",
            tax_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(false),
        );

        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let item = |id| items.iter().find(|item| item.id == id).unwrap();
        assert!(
            item(flagged).is_tax_relevant,
            "a true flag makes a booking of a non-tax category relevant"
        );
        assert_eq!(item(flagged).tax_source, TaxSource::Rule);
        assert_eq!(item(flagged).tax_rule_pattern.as_deref(), Some("Beispiel"));
        assert!(
            !item(unflagged).is_tax_relevant,
            "a false flag makes a booking of a tax category not relevant"
        );
        assert_eq!(item(unflagged).tax_source, TaxSource::Rule);
        assert_eq!(item(unflagged).tax_rule_pattern.as_deref(), Some("Anderer"));
    }

    #[test]
    fn a_rule_of_another_category_a_manual_rule_and_a_flag_less_rule_have_no_effect() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let tax_cat = category(&db, "Office supplies", true, false);
        let normal_cat = category(&db, "Groceries", false, false);

        let other_category = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-other-a",
        );
        add_rule(
            &db,
            "Beispiel",
            tax_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == other_category)
            .unwrap();
        assert_eq!(item.tax_source, TaxSource::Category);
        assert!(!item.is_tax_relevant);

        let manual_only = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -2000,
            Some("Erika Mustermann"),
            None,
            Some(normal_cat),
            "tax-other-b",
        );
        add_rule(
            &db,
            "Erika",
            normal_cat,
            10,
            false,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == manual_only)
            .unwrap();
        assert_eq!(item.tax_source, TaxSource::Category, "autoApply = false has no effect");

        let later_wins = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -3000,
            Some("Beispiel Vertrieb"),
            None,
            Some(normal_cat),
            "tax-other-c",
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            10,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            None,
        );
        add_rule(
            &db,
            "Vertrieb",
            normal_cat,
            5,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == later_wins)
            .unwrap();
        assert!(item.is_tax_relevant, "a flag-less rule is skipped");
        assert_eq!(item.tax_source, TaxSource::Rule);
        assert_eq!(item.tax_rule_pattern.as_deref(), Some("Vertrieb"));
    }

    #[test]
    fn two_matching_tax_rules_pick_the_higher_priority_or_the_lower_id_on_ties() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let high_priority = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-prio-a",
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            10,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(false),
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            20,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == high_priority)
            .unwrap();
        assert!(item.is_tax_relevant, "priority 20 beats priority 10");
        assert_eq!(item.tax_source, TaxSource::Rule);

        let same_priority = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -2000,
            Some("Erika Mustermann"),
            None,
            Some(normal_cat),
            "tax-prio-b",
        );
        add_rule(
            &db,
            "Erika",
            normal_cat,
            5,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(false),
        );
        add_rule(
            &db,
            "Erika",
            normal_cat,
            5,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == same_priority)
            .unwrap();
        assert!(!item.is_tax_relevant, "on equal priority the lower id wins");
        assert_eq!(item.tax_source, TaxSource::Rule);
        assert_eq!(item.tax_rule_pattern.as_deref(), Some("Erika"));
    }

    #[test]
    fn the_rules_text_field_and_direction_are_honoured() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let partner_only = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Buchhaltung GmbH"),
            Some("Beispiel Shop Rechnung"),
            Some(normal_cat),
            "tax-field-a",
        );
        add_rule(
            &db,
            "Beispiel Shop",
            normal_cat,
            0,
            true,
            ModelRuleField::Partner,
            ModelRuleDirection::Any,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == partner_only)
            .unwrap();
        assert!(!item.is_tax_relevant, "the PARTNER rule only searches the partner name");
        assert_eq!(item.tax_source, TaxSource::Category);

        let income = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            10_000,
            Some("Beispiel Laden"),
            None,
            Some(normal_cat),
            "tax-field-b",
        );
        add_rule(
            &db,
            "Beispiel Laden",
            normal_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Expense,
            Some(true),
        );
        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == income)
            .unwrap();
        assert!(
            !item.is_tax_relevant,
            "the EXPENSE rule never matches an income booking"
        );
        assert_eq!(item.tax_source, TaxSource::Category);

        let partner_match = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -2000,
            Some("Beispiel Shop GmbH"),
            None,
            Some(normal_cat),
            "tax-field-c",
        );
        let expense_match = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -3000,
            Some("Beispiel Laden"),
            None,
            Some(normal_cat),
            "tax-field-d",
        );
        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let item = |id| items.iter().find(|item| item.id == id).unwrap();
        assert!(item(partner_match).is_tax_relevant);
        assert_eq!(item(partner_match).tax_source, TaxSource::Rule);
        assert!(item(expense_match).is_tax_relevant);
        assert_eq!(item(expense_match).tax_source, TaxSource::Rule);
    }

    #[test]
    fn setting_a_rules_tax_flag_changes_existing_bookings_without_any_reapply() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-later",
        );
        let rule = add_rule(
            &db,
            "Beispiel",
            normal_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            None,
        );

        let source = |db: &Database| {
            find_items(db, &TransactionFilter::default(), None)
                .unwrap()
                .into_iter()
                .find(|item| item.id == booking)
                .unwrap()
        };
        assert!(!source(&db).is_tax_relevant);
        assert_eq!(source(&db).tax_source, TaxSource::Category);

        set_rule_tax(&db, rule, Some(true));
        let item = source(&db);
        assert!(item.is_tax_relevant, "the flag applies the moment it is saved");
        assert_eq!(item.tax_source, TaxSource::Rule);
        assert_eq!(item.tax_rule_pattern.as_deref(), Some("Beispiel"));

        set_rule_tax(&db, rule, None);
        let item = source(&db);
        assert!(!item.is_tax_relevant, "resetting to null restores the category layer");
        assert_eq!(item.tax_source, TaxSource::Category);
        assert_eq!(item.tax_rule_pattern, None);
    }

    #[test]
    fn the_override_wins_over_rule_and_category_and_null_returns_to_automatic() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-override",
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );

        let item = |db: &Database| {
            find_items(db, &TransactionFilter::default(), None)
                .unwrap()
                .into_iter()
                .find(|item| item.id == booking)
                .unwrap()
        };
        assert!(item(&db).is_tax_relevant, "the rule makes it relevant first");

        assert_eq!(set_tax_override(&db, &[booking], Some(false)).unwrap(), 1);
        let overridden = item(&db);
        assert!(!overridden.is_tax_relevant, "the override beats the rule");
        assert_eq!(overridden.tax_source, TaxSource::Manual);
        assert_eq!(overridden.tax_override, Some(false));
        assert_eq!(
            overridden.tax_rule_pattern, None,
            "no rule pattern when the override decides"
        );

        assert_eq!(set_tax_override(&db, &[booking], Some(true)).unwrap(), 1);
        assert!(item(&db).is_tax_relevant);
        assert_eq!(item(&db).tax_source, TaxSource::Manual);

        assert_eq!(set_tax_override(&db, &[booking], None).unwrap(), 1);
        let automatic = item(&db);
        assert!(automatic.is_tax_relevant, "null returns to the automatic result");
        assert_eq!(automatic.tax_source, TaxSource::Rule);
        assert_eq!(automatic.tax_override, None);
    }

    #[test]
    fn the_override_survives_a_category_change() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let tax_cat = category(&db, "Office supplies", true, false);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("Shop"),
            None,
            Some(normal_cat),
            "tax-survive",
        );

        assert_eq!(set_tax_override(&db, &[booking], Some(true)).unwrap(), 1);
        set_category(&db, booking, Some(tax_cat)).unwrap();

        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == booking)
            .unwrap();
        assert!(item.is_tax_relevant);
        assert_eq!(item.tax_source, TaxSource::Manual);
        assert_eq!(item.tax_override, Some(true));
    }

    #[test]
    fn set_tax_override_chunks_and_ignores_empty_and_unknown_ids() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let mut ids = Vec::new();
        for index in 1..=1_300 {
            ids.push(add(
                &db,
                account_id,
                date!(2026 - 06 - 14),
                -index,
                Some("Shop"),
                None,
                Some(normal_cat),
                &format!("tax-chunk-{index}"),
            ));
        }
        let unknown = [TransactionId(999_001), TransactionId(999_002), TransactionId(999_003)];
        let mut with_unknown = ids.clone();
        with_unknown.extend_from_slice(&unknown);

        assert_eq!(set_tax_override(&db, &with_unknown, Some(true)).unwrap(), 1_300);
        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        assert_eq!(
            items
                .iter()
                .filter(|item| item.tax_source == TaxSource::Manual && item.tax_override == Some(true))
                .count(),
            1_300
        );
        assert_eq!(set_tax_override(&db, &[], Some(true)).unwrap(), 0);
        assert_eq!(set_tax_override(&db, &unknown, Some(true)).unwrap(), 0);
        assert_eq!(
            find_items(&db, &TransactionFilter::default(), None)
                .unwrap()
                .iter()
                .filter(|item| item.tax_source == TaxSource::Manual)
                .count(),
            1_300
        );
    }

    #[test]
    fn tax_relevant_only_returns_exactly_the_effective_bookings_with_the_limit_after_the_filter() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let tax_cat = category(&db, "Office supplies", true, false);
        let normal_cat = category(&db, "Groceries", false, false);
        let by_category = add(
            &db,
            account_id,
            date!(2026 - 06 - 11),
            -1000,
            Some("Steuer Shop"),
            None,
            Some(tax_cat),
            "tax-only-a",
        );
        let by_rule = add(
            &db,
            account_id,
            date!(2026 - 06 - 12),
            -2000,
            Some("Beispiel Shop"),
            None,
            Some(normal_cat),
            "tax-only-b",
        );
        add_rule(
            &db,
            "Beispiel",
            normal_cat,
            0,
            true,
            ModelRuleField::Any,
            ModelRuleDirection::Any,
            Some(true),
        );
        let overridden_false = add(
            &db,
            account_id,
            date!(2026 - 06 - 13),
            -3000,
            Some("Widerspruch Shop"),
            None,
            Some(tax_cat),
            "tax-only-c",
        );
        let plain = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -4000,
            Some("Normaler Shop"),
            None,
            Some(normal_cat),
            "tax-only-d",
        );
        set_tax_override(&db, &[overridden_false], Some(false)).unwrap();

        let relevant = find_items(
            &db,
            &TransactionFilter {
                tax_relevant_only: true,
                ..TransactionFilter::default()
            },
            None,
        )
        .unwrap();
        assert_eq!(
            relevant.iter().map(|item| item.id).collect::<BTreeSet<_>>(),
            BTreeSet::from([by_category, by_rule]),
            "rule-flagged in, overridden-to-false out"
        );
        assert!(!relevant.iter().any(|item| item.id == plain));

        let limited = find_items(
            &db,
            &TransactionFilter {
                tax_relevant_only: true,
                ..TransactionFilter::default()
            },
            Some(1),
        )
        .unwrap();
        assert_eq!(limited.len(), 1, "the limit applies after the filter");
        assert_eq!(limited[0].id, relevant[0].id, "the newest relevant booking");
    }

    #[test]
    fn a_tax_flagged_shop_rule_makes_paypal_bookings_of_its_category_relevant() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let partner = "PayPal Europe S.a.r.l. et Cie S.C.A";
        let by_rule = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some(partner),
            Some("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
            Some(normal_cat),
            "tax-paypal-a",
        );
        let other_shop = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -2000,
            Some(partner),
            Some("EREF+R2 SVWZ+PP.472.PPX . Anderer Shop GmbH, Ihr Einkauf bei Anderer Shop GmbH"),
            Some(normal_cat),
            "tax-paypal-b",
        );
        add_rule(
            &db,
            "Beispiel Shop",
            normal_cat,
            0,
            true,
            ModelRuleField::Partner,
            ModelRuleDirection::Any,
            Some(true),
        );

        let items = find_items(&db, &TransactionFilter::default(), None).unwrap();
        let item = |id| items.iter().find(|item| item.id == id).unwrap();
        assert!(
            item(by_rule).is_tax_relevant,
            "the PARTNER rule matches the shop extracted from the purpose"
        );
        assert_eq!(item(by_rule).tax_source, TaxSource::Rule);
        assert_eq!(item(by_rule).tax_rule_pattern.as_deref(), Some("Beispiel Shop"));
        assert!(!item(other_shop).is_tax_relevant);
        assert_eq!(item(other_shop).tax_source, TaxSource::Category);
    }

    #[test]
    fn a_provider_rule_gives_way_to_a_same_category_shop_tax_rule() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let tax_cat = category(&db, "Office supplies", true, false);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
            Some("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
            Some(tax_cat),
            "tax-paypal-provider",
        );
        add_rule(
            &db,
            "PayPal",
            tax_cat,
            10,
            true,
            ModelRuleField::Partner,
            ModelRuleDirection::Any,
            Some(false),
        );
        add_rule(
            &db,
            "Beispiel Shop",
            tax_cat,
            0,
            true,
            ModelRuleField::Partner,
            ModelRuleDirection::Any,
            Some(true),
        );

        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == booking)
            .unwrap();
        assert!(
            item.is_tax_relevant,
            "the shop rule, not the PayPal rule, decides the tax flag"
        );
        assert_eq!(item.tax_source, TaxSource::Rule);
        assert_eq!(item.tax_rule_pattern.as_deref(), Some("Beispiel Shop"));
    }

    #[test]
    fn a_tax_flagged_shop_rule_applies_to_a_blank_partner_card_booking() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let normal_cat = category(&db, "Groceries", false, false);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 06 - 14),
            -1000,
            None,
            Some("PAYPAL *BEISPIELSHOP"),
            Some(normal_cat),
            "tax-paypal-card",
        );
        add_rule(
            &db,
            "BEISPIELSHOP",
            normal_cat,
            0,
            true,
            ModelRuleField::Partner,
            ModelRuleDirection::Any,
            Some(true),
        );

        let item = find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == booking)
            .unwrap();
        assert!(
            item.is_tax_relevant,
            "the PARTNER rule matches the shop in the card descriptor"
        );
        assert_eq!(item.tax_source, TaxSource::Rule);
        assert_eq!(item.tax_rule_pattern.as_deref(), Some("BEISPIELSHOP"));
    }

    #[test]
    fn the_new_mutations_write_exactly_one_audit_entry_each() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let category = category(&db, "Food", false, false);
        let first = add(
            &db,
            account_id,
            date!(2026 - 06 - 01),
            -100,
            Some("One"),
            None,
            None,
            "audit-new-a",
        );
        let second = add(
            &db,
            account_id,
            date!(2026 - 06 - 02),
            -200,
            Some("Two"),
            None,
            None,
            "audit-new-b",
        );
        let before = audit::recent(&db, 100).unwrap().len();

        update_categories(&db, &[(first, Some(category))]).unwrap();
        let mut expected = before + 1;
        assert_eq!(audit::recent(&db, 100).unwrap().len(), expected);

        set_tax_override(&db, &[first], Some(true)).unwrap();
        expected += 1;
        assert_eq!(audit::recent(&db, 100).unwrap().len(), expected);

        mark_read(&db, None).unwrap();
        expected += 1;
        assert_eq!(audit::recent(&db, 100).unwrap().len(), expected);

        delete_all(&db, &[first, second]).unwrap();
        expected += 1;
        assert_eq!(audit::recent(&db, 100).unwrap().len(), expected);

        forget_deleted(&db, account_id).unwrap();
        expected += 1;
        assert_eq!(audit::recent(&db, 100).unwrap().len(), expected);

        assert!(
            audit::recent(&db, 100)
                .unwrap()
                .iter()
                .all(|entry| !entry.details.contains("One") && !entry.details.contains("Two"))
        );
    }

    #[test]
    fn forget_deleted_bookings_names_the_count_and_writes_nothing_when_none_are_remembered() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let booking = add(
            &db,
            account_id,
            date!(2026 - 10 - 05),
            -500,
            Some("Cafe"),
            None,
            None,
            "forget-view-a",
        );
        delete_all(&db, &[booking]).unwrap();
        let before = audit::recent(&db, 100).unwrap().len();

        assert_eq!(forget_deleted_bookings(&db, account_id).unwrap(), 1);
        let entries = audit::recent(&db, 100).unwrap();
        assert_eq!(entries.len(), before + 1);
        assert_eq!(entries[0].action, "FORGET_DELETED_BOOKINGS");
        assert_eq!(
            entries[0].details,
            "Account 3000: 1 deleted bookings may be imported again"
        );

        assert_eq!(forget_deleted_bookings(&db, account_id).unwrap(), 0);
        assert_eq!(
            audit::recent(&db, 100).unwrap().len(),
            before + 1,
            "nothing remembered means no mutation and no audit entry"
        );
    }
}
