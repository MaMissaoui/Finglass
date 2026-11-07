//! Repositories for the domain tables.
//!
//! Every mutating function runs in one transaction and writes exactly one audit entry in that transaction. Audit
//! details contain ids and names only, never IBANs, account numbers or purpose text.

pub mod account_groups;
pub mod accounts;
pub mod categories;
pub mod rules;
pub mod transactions;

use rusqlite::Connection;
use thiserror::Error;
use time::format_description::FormatItem;
use time::format_description::well_known::Rfc3339;
use time::macros::format_description;
use time::{Date, OffsetDateTime};

use crate::db::{Database, DbError};
use crate::money::{Currency, MoneyError};

pub use self::transactions::ImportOutcome;

/// Everything that can go wrong in a repository call.
#[derive(Debug, Error)]
pub enum RepoError {
    /// The requested row does not exist.
    #[error("record not found")]
    NotFound,
    /// A money value uses a different currency than the row it belongs to.
    #[error("currency mismatch")]
    CurrencyMismatch,
    /// Stored or supplied text is not valid for the column.
    #[error("invalid value: {0}")]
    Invalid(String),
    /// An error from the database layer.
    #[error(transparent)]
    Db(#[from] DbError),
    /// An error from a money value.
    #[error(transparent)]
    Money(#[from] MoneyError),
}

impl From<rusqlite::Error> for RepoError {
    fn from(error: rusqlite::Error) -> Self {
        Self::Db(DbError::from(error))
    }
}

/// A repository result.
pub type Result<T> = std::result::Result<T, RepoError>;

/// Runs repository work with the database's single connection.
fn in_conn<T>(db: &Database, work: impl FnOnce(&Connection) -> Result<T>) -> Result<T> {
    db.with_conn(|conn| Ok(work(conn)))?
}

const DATE_FORMAT: &[FormatItem<'static>] = format_description!("[year]-[month]-[day]");

/// Formats a date as `YYYY-MM-DD`.
pub(crate) fn format_date(date: Date) -> Result<String> {
    date.format(DATE_FORMAT)
        .map_err(|error| RepoError::Invalid(format!("invalid date: {error}")))
}

/// Parses a `YYYY-MM-DD` date.
pub(crate) fn parse_date(text: &str) -> Result<Date> {
    Date::parse(text, DATE_FORMAT).map_err(|error| RepoError::Invalid(format!("invalid date: {error}")))
}

/// Formats a timestamp as RFC 3339 UTC text.
pub(crate) fn format_timestamp(timestamp: OffsetDateTime) -> Result<String> {
    timestamp
        .format(&Rfc3339)
        .map_err(|error| RepoError::Invalid(format!("invalid timestamp: {error}")))
}

/// Parses an RFC 3339 timestamp.
pub(crate) fn parse_timestamp(text: &str) -> Result<OffsetDateTime> {
    OffsetDateTime::parse(text, &Rfc3339).map_err(|error| RepoError::Invalid(format!("invalid timestamp: {error}")))
}

/// Reads a currency from a stored text column.
pub(crate) fn parse_currency(text: &str) -> Result<Currency> {
    Ok(Currency::new(text)?)
}

/// Reads a boolean from its `INTEGER` 0/1 representation.
pub(crate) fn from_int_bool(value: i64) -> bool {
    value != 0
}

/// Converts a boolean to its `INTEGER` 0/1 representation.
pub(crate) fn to_int_bool(value: bool) -> i64 {
    i64::from(value)
}

/// Converts an optional boolean to its `INTEGER` representation.
pub(crate) fn to_optional_int_bool(value: Option<bool>) -> Option<i64> {
    value.map(to_int_bool)
}

/// The action prefix used when a repository writes an audit entry for an id and a name.
pub(crate) fn id_and_name(id: i64, name: &str) -> String {
    format!("id={id} name={name}")
}

/// The action prefix used when a repository writes an audit entry for an id only.
pub(crate) fn id_only(id: i64) -> String {
    format!("id={id}")
}

#[cfg(test)]
mod tests {
    use rusqlite::params;
    use time::macros::date;

    use super::*;
    use crate::audit;
    use crate::model::{
        AccountGroupId, AccountId, AccountType, NewAccount, NewAccountGroup, NewCategory, NewRule, NewTransaction,
        RuleDirection, RuleField,
    };
    use crate::money::{Currency, Money};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn eur(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn account(name: &str) -> NewAccount {
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some(name.to_owned()),
            iban: None,
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Checking,
            balance: eur(0),
            balance_updated_at: None,
            group_id: None,
        }
    }

    fn account_in(name: &str, group_id: Option<AccountGroupId>) -> NewAccount {
        let mut new = account(name);
        new.group_id = group_id;
        new
    }

    fn new_transaction(hash: &str, day: time::Date, minor: i64) -> NewTransaction {
        NewTransaction {
            booking_date: day,
            amount: eur(minor),
            balance_after: None,
            partner_name: Some("Partner".to_owned()),
            purpose: Some("Purpose".to_owned()),
            category_id: None,
            import_hash: hash.to_owned(),
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }
    }

    fn new_category(name: &str) -> NewCategory {
        NewCategory {
            name: name.to_owned(),
            color: "#3BC7B0".to_owned(),
            budget_limit: None,
            is_tax_relevant: false,
            is_transfer: false,
        }
    }

    fn audit_count(db: &Database) -> i64 {
        db.with_conn(|conn| {
            conn.query_row("SELECT count(*) FROM audit_log", [], |row| row.get(0))
                .map_err(DbError::from)
        })
        .unwrap()
    }

    fn expect_one_audit(db: &Database, before: i64) -> i64 {
        let after = audit_count(db);
        assert_eq!(after, before + 1, "expected exactly one audit entry");
        after
    }

    fn count(db: &Database, sql: &str) -> i64 {
        db.with_conn(|conn| conn.query_row(sql, [], |row| row.get(0)).map_err(DbError::from))
            .unwrap()
    }

    #[test]
    fn account_type_and_currency_checks_reject_bad_values() {
        let (_temp, db) = database();

        db.with_conn(|conn| {
            assert!(
                conn.execute("INSERT INTO accounts (account_type) VALUES ('bogus')", [])
                    .is_err()
            );
            assert!(
                conn.execute("INSERT INTO accounts (currency) VALUES ('EU')", [])
                    .is_err()
            );
            assert!(
                conn.execute("INSERT INTO accounts (currency) VALUES ('EURO')", [])
                    .is_err()
            );
            Ok(())
        })
        .unwrap();

        assert_eq!(count(&db, "SELECT count(*) FROM accounts"), 0);
    }

    #[test]
    fn accounts_crud_ordering_and_balance_currency() {
        let (_temp, db) = database();

        let late = account_groups::create(
            &db,
            NewAccountGroup {
                name: "Late".to_owned(),
                sort_order: 10,
            },
        )
        .unwrap();
        let early = account_groups::create(
            &db,
            NewAccountGroup {
                name: "Early".to_owned(),
                sort_order: 1,
            },
        )
        .unwrap();

        let bravo = accounts::create(&db, account_in("Bravo", Some(late))).unwrap();
        let beta = accounts::create(&db, account("Beta")).unwrap();
        let zulu = accounts::create(&db, account_in("Zulu", Some(early))).unwrap();
        let alpha = accounts::create(&db, account_in("Alpha", Some(early))).unwrap();
        let alpha_without_group = accounts::create(&db, account("Alpha")).unwrap();

        let ordered: Vec<AccountId> = accounts::list(&db).unwrap().iter().map(|account| account.id).collect();
        assert_eq!(
            ordered,
            vec![alpha, zulu, bravo, alpha_without_group, beta],
            "groups by sort order, then names, with ungrouped accounts last"
        );

        let loaded = accounts::get(&db, alpha).unwrap().unwrap();
        assert_eq!(loaded.name.as_deref(), Some("Alpha"));
        assert_eq!(loaded.currency, Currency::EUR);
        assert_eq!(loaded.account_type, AccountType::Checking);
        assert_eq!(loaded.balance, eur(0));

        accounts::rename(&db, alpha, Some("Renamed".to_owned())).unwrap();
        assert_eq!(
            accounts::get(&db, alpha).unwrap().unwrap().name.as_deref(),
            Some("Renamed")
        );
        accounts::rename(&db, alpha, None).unwrap();
        assert_eq!(accounts::get(&db, alpha).unwrap().unwrap().name, None);

        accounts::set_group(&db, bravo, Some(early)).unwrap();
        assert_eq!(accounts::get(&db, bravo).unwrap().unwrap().group_id, Some(early));
        accounts::set_group(&db, bravo, None).unwrap();
        assert_eq!(accounts::get(&db, bravo).unwrap().unwrap().group_id, None);

        let at = OffsetDateTime::now_utc();
        accounts::set_balance(&db, alpha, eur(1234), at).unwrap();
        let loaded = accounts::get(&db, alpha).unwrap().unwrap();
        assert_eq!(loaded.balance, eur(1234));
        assert_eq!(loaded.balance_updated_at, Some(at));

        let usd = Money {
            minor: 1,
            currency: Currency::new("USD").unwrap(),
        };
        let error = accounts::set_balance(&db, alpha, usd, at).unwrap_err();
        assert!(matches!(error, RepoError::CurrencyMismatch));
        assert_eq!(accounts::get(&db, alpha).unwrap().unwrap().balance, eur(1234));

        accounts::delete(&db, beta).unwrap();
        assert_eq!(accounts::get(&db, beta).unwrap(), None);
        assert!(matches!(accounts::delete(&db, beta).unwrap_err(), RepoError::NotFound));
    }

    #[test]
    fn deleting_a_group_keeps_its_accounts_without_a_group() {
        let (_temp, db) = database();
        let group = account_groups::create(
            &db,
            NewAccountGroup {
                name: "Group".to_owned(),
                sort_order: 0,
            },
        )
        .unwrap();
        let account_id = accounts::create(&db, account_in("Account", Some(group))).unwrap();

        account_groups::delete(&db, group).unwrap();

        let loaded = accounts::get(&db, account_id).unwrap().unwrap();
        assert_eq!(loaded.group_id, None);
    }

    #[test]
    fn deleting_an_account_cascades_but_keeps_documents() {
        let (_temp, db) = database();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        transactions::insert_imported(
            &db,
            account_id,
            vec![new_transaction("hash", date!(2024 - 01 - 01), -500)],
        )
        .unwrap();
        let transaction_id = transactions::list_for_account(&db, account_id, None, None).unwrap()[0].id;

        db.with_conn(|conn| {
            conn.execute(
                "INSERT INTO pending_bookings
                    (account_id, booking_date, amount_minor, currency, fetched_at)
                 VALUES (?1, '2024-01-02', 100, 'EUR', '2024-01-02T00:00:00Z')",
                [account_id.0],
            )?;
            conn.execute(
                "INSERT INTO deleted_bookings (account_id, import_hash, deleted_at)
                 VALUES (?1, 'gone', '2024-01-02T00:00:00Z')",
                [account_id.0],
            )?;
            conn.execute(
                "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)
                 VALUES ('a.pdf', 'a.pdf', 'application/pdf', '2024-01-02T00:00:00Z', ?1)",
                [transaction_id.0],
            )?;
            Ok(())
        })
        .unwrap();

        accounts::delete(&db, account_id).unwrap();

        assert_eq!(count(&db, "SELECT count(*) FROM transactions"), 0);
        assert_eq!(count(&db, "SELECT count(*) FROM pending_bookings"), 0);
        assert_eq!(count(&db, "SELECT count(*) FROM deleted_bookings"), 0);
        assert_eq!(count(&db, "SELECT count(*) FROM documents"), 1);

        let linked: Option<i64> = db
            .with_conn(|conn| {
                conn.query_row("SELECT transaction_id FROM documents", [], |row| row.get(0))
                    .map_err(DbError::from)
            })
            .unwrap();
        assert_eq!(linked, None);
    }

    #[test]
    fn deleting_a_category_uncategorizes_bookings_and_deletes_its_rules() {
        let (_temp, db) = database();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        let category = categories::create(&db, new_category("Food")).unwrap();
        let rule = rules::create(
            &db,
            NewRule {
                pattern: "market".to_owned(),
                category_id: category,
                priority: 0,
                auto_apply: true,
                match_field: RuleField::Partner,
                direction: RuleDirection::Expense,
                tax_relevant: None,
            },
        )
        .unwrap();
        let mut new_transaction = new_transaction("hash", date!(2024 - 01 - 01), -500);
        new_transaction.category_id = Some(category);
        transactions::insert_imported(&db, account_id, vec![new_transaction]).unwrap();

        categories::delete(&db, category).unwrap();

        assert_eq!(categories::get(&db, category).unwrap(), None);
        assert!(rules::list(&db).unwrap().is_empty());
        assert_eq!(rule.0, 1);

        let bookings = transactions::list_for_account(&db, account_id, None, None).unwrap();
        assert_eq!(bookings.len(), 1);
        assert_eq!(bookings[0].category_id, None);
    }

    #[test]
    fn categories_and_rules_crud_and_ordering() {
        let (_temp, db) = database();

        let zebra = categories::create(&db, new_category("zebra")).unwrap();
        let alpha = categories::create(&db, new_category("Alpha")).unwrap();
        let beta = categories::create(&db, new_category("beta")).unwrap();

        let names: Vec<String> = categories::list(&db)
            .unwrap()
            .into_iter()
            .map(|category| category.name)
            .collect();
        assert_eq!(names, vec!["Alpha", "beta", "zebra"]);

        let mut updated = new_category("Renamed");
        updated.color = "#000000".to_owned();
        updated.budget_limit = Some(eur(50_000));
        updated.is_tax_relevant = true;
        categories::update(&db, zebra, updated).unwrap();
        let loaded = categories::get(&db, zebra).unwrap().unwrap();
        assert_eq!(loaded.name, "Renamed");
        assert_eq!(loaded.color, "#000000");
        assert_eq!(loaded.budget_limit, Some(eur(50_000)));
        assert!(loaded.is_tax_relevant);

        let rule_low = rules::create(
            &db,
            NewRule {
                pattern: "low".to_owned(),
                category_id: alpha,
                priority: 1,
                auto_apply: false,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )
        .unwrap();
        let rule_high = rules::create(
            &db,
            NewRule {
                pattern: "high".to_owned(),
                category_id: beta,
                priority: 5,
                auto_apply: true,
                match_field: RuleField::Purpose,
                direction: RuleDirection::Income,
                tax_relevant: Some(false),
            },
        )
        .unwrap();

        let ordered: Vec<_> = rules::list(&db).unwrap().into_iter().map(|rule| rule.id).collect();
        assert_eq!(ordered, vec![rule_high, rule_low]);

        let mut updated_rule = rules::list(&db)
            .unwrap()
            .into_iter()
            .find(|rule| rule.id == rule_low)
            .unwrap();
        updated_rule.priority = 10;
        updated_rule.pattern = "changed".to_owned();
        updated_rule.match_field = RuleField::Partner;
        updated_rule.direction = RuleDirection::Expense;
        updated_rule.tax_relevant = Some(true);
        updated_rule.auto_apply = false;
        rules::update(
            &db,
            rule_low,
            NewRule {
                pattern: updated_rule.pattern.clone(),
                category_id: updated_rule.category_id,
                priority: updated_rule.priority,
                auto_apply: updated_rule.auto_apply,
                match_field: updated_rule.match_field,
                direction: updated_rule.direction,
                tax_relevant: updated_rule.tax_relevant,
            },
        )
        .unwrap();

        let rules_after = rules::list(&db).unwrap();
        assert_eq!(rules_after[0].id, rule_low);
        assert_eq!(rules_after[0].pattern, "changed");
        assert_eq!(rules_after[0].match_field, RuleField::Partner);
        assert_eq!(rules_after[0].direction, RuleDirection::Expense);
        assert_eq!(rules_after[0].tax_relevant, Some(true));

        rules::delete(&db, rule_high).unwrap();
        assert_eq!(rules::list(&db).unwrap().len(), 1);
    }

    #[test]
    fn import_counts_duplicates_deleted_and_same_batch_entries() {
        let (_temp, db) = database();
        let account_id = accounts::create(&db, account("Account")).unwrap();

        let first = transactions::insert_imported(
            &db,
            account_id,
            vec![new_transaction("dup", date!(2024 - 01 - 01), 100)],
        )
        .unwrap();
        assert_eq!(first.inserted, 1);

        db.with_conn(|conn| {
            conn.execute(
                "INSERT INTO deleted_bookings (account_id, import_hash, deleted_at)
                 VALUES (?1, 'gone', '2024-01-02T00:00:00Z')",
                params![account_id.0],
            )?;
            Ok(())
        })
        .unwrap();

        let outcome = transactions::insert_imported(
            &db,
            account_id,
            vec![
                new_transaction("dup", date!(2024 - 01 - 02), 200),
                new_transaction("gone", date!(2024 - 01 - 02), 300),
                new_transaction("same", date!(2024 - 01 - 03), 400),
                new_transaction("same", date!(2024 - 01 - 03), 400),
            ],
        )
        .unwrap();
        assert_eq!(
            outcome,
            ImportOutcome {
                inserted: 2,
                skipped_duplicate: 1,
                skipped_deleted: 1,
            }
        );

        let bookings = transactions::list_for_account(&db, account_id, None, None).unwrap();
        assert_eq!(bookings.len(), 3, "the same hash inside one batch is inserted twice");

        let between = transactions::list_for_account(
            &db,
            account_id,
            Some(date!(2024 - 01 - 02)),
            Some(date!(2024 - 01 - 03)),
        )
        .unwrap();
        assert_eq!(between.len(), 2);
        assert!(between.iter().all(|booking| booking.import_hash == "same"));
    }

    #[test]
    fn a_currency_mismatch_inserts_nothing() {
        let (_temp, db) = database();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        let usd = Currency::new("USD").unwrap();

        let mut bad = new_transaction("bad", date!(2024 - 01 - 01), 100);
        bad.amount = Money {
            minor: 100,
            currency: usd,
        };

        let error = transactions::insert_imported(&db, account_id, vec![bad]).unwrap_err();
        assert!(matches!(error, RepoError::CurrencyMismatch));
        assert_eq!(count(&db, "SELECT count(*) FROM transactions"), 0);
    }

    #[test]
    fn deleting_a_transaction_prevents_re_import() {
        let (_temp, db) = database();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        transactions::insert_imported(
            &db,
            account_id,
            vec![new_transaction("hash", date!(2024 - 01 - 01), 100)],
        )
        .unwrap();
        let booking = transactions::list_for_account(&db, account_id, None, None).unwrap()[0].clone();

        transactions::delete(&db, booking.id).unwrap();
        assert!(
            transactions::list_for_account(&db, account_id, None, None)
                .unwrap()
                .is_empty()
        );
        assert_eq!(count(&db, "SELECT count(*) FROM deleted_bookings"), 1);

        let outcome = transactions::insert_imported(
            &db,
            account_id,
            vec![new_transaction("hash", date!(2024 - 01 - 01), 100)],
        )
        .unwrap();
        assert_eq!(
            outcome,
            ImportOutcome {
                inserted: 0,
                skipped_duplicate: 0,
                skipped_deleted: 1,
            }
        );
        assert!(
            transactions::list_for_account(&db, account_id, None, None)
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn every_mutation_writes_exactly_one_audit_entry_without_ibans() {
        let (_temp, db) = database();
        let iban = "DE89370400440532013000";
        let mut before = audit_count(&db);

        let group = account_groups::create(
            &db,
            NewAccountGroup {
                name: "Group".to_owned(),
                sort_order: 0,
            },
        )
        .unwrap();
        before = expect_one_audit(&db, before);

        let other_group = account_groups::create(
            &db,
            NewAccountGroup {
                name: "Other".to_owned(),
                sort_order: 1,
            },
        )
        .unwrap();
        before = expect_one_audit(&db, before);

        account_groups::rename(&db, group, "Renamed group").unwrap();
        before = expect_one_audit(&db, before);

        let mut new_account = account("Audit account");
        new_account.iban = Some(iban.to_owned());
        new_account.group_id = Some(group);
        let account_id = accounts::create(&db, new_account).unwrap();
        before = expect_one_audit(&db, before);

        accounts::rename(&db, account_id, Some("Audit account 2".to_owned())).unwrap();
        before = expect_one_audit(&db, before);

        accounts::set_group(&db, account_id, Some(other_group)).unwrap();
        before = expect_one_audit(&db, before);

        accounts::set_balance(&db, account_id, eur(1234), OffsetDateTime::now_utc()).unwrap();
        before = expect_one_audit(&db, before);

        let category = categories::create(&db, new_category("Category")).unwrap();
        before = expect_one_audit(&db, before);

        categories::update(&db, category, new_category("Category 2")).unwrap();
        before = expect_one_audit(&db, before);

        let rule = rules::create(
            &db,
            NewRule {
                pattern: "pattern".to_owned(),
                category_id: category,
                priority: 0,
                auto_apply: true,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )
        .unwrap();
        before = expect_one_audit(&db, before);

        rules::update(
            &db,
            rule,
            NewRule {
                pattern: "pattern 2".to_owned(),
                category_id: category,
                priority: 1,
                auto_apply: false,
                match_field: RuleField::Partner,
                direction: RuleDirection::Expense,
                tax_relevant: Some(true),
            },
        )
        .unwrap();
        before = expect_one_audit(&db, before);

        transactions::insert_imported(
            &db,
            account_id,
            vec![new_transaction("hash", date!(2024 - 01 - 01), 100)],
        )
        .unwrap();
        before = expect_one_audit(&db, before);

        let transaction_id = transactions::list_for_account(&db, account_id, None, None).unwrap()[0].id;
        transactions::set_category(&db, transaction_id, Some(category)).unwrap();
        before = expect_one_audit(&db, before);

        transactions::delete(&db, transaction_id).unwrap();
        before = expect_one_audit(&db, before);

        rules::delete(&db, rule).unwrap();
        before = expect_one_audit(&db, before);

        categories::delete(&db, category).unwrap();
        before = expect_one_audit(&db, before);

        accounts::delete(&db, account_id).unwrap();
        before = expect_one_audit(&db, before);

        account_groups::delete(&db, other_group).unwrap();
        before = expect_one_audit(&db, before);

        account_groups::delete(&db, group).unwrap();
        expect_one_audit(&db, before);

        let entries = audit::recent(&db, 1000).unwrap();
        assert_eq!(entries.len() as i64, audit_count(&db));
        assert!(
            entries.iter().all(|entry| !entry.details.contains(iban)),
            "an audit entry leaked an IBAN"
        );
    }
}
