//! Importing a parsed statement into the database, ported from FinDesk's `StatementImportService`.

use std::fmt::Write as _;

use rust_decimal::Decimal;
use thiserror::Error;
use time::{Date, Duration, OffsetDateTime};

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::iban;
use crate::model::{AccountId, NewTransaction};
use crate::money::{Currency, Money, MoneyError};
use crate::repo::{self, RepoError};
use crate::rules::{RuleMatcher, matcher};

use super::amount_to_money;
use super::dedup::{self, Candidate};
use super::hash;
use super::model::{ImportedBooking, ParsedStatement};

/// How recent a file must be to set a non-zero balance typed by hand: a monthly card statement is
/// usually imported within about six weeks.
pub const TYPED_BALANCE_RECENT_DAYS: i64 = 45;

/// Everything that can go wrong while importing a parsed statement.
#[derive(Debug, Error)]
pub enum ImportServiceError {
    /// A repository call failed.
    #[error(transparent)]
    Repo(#[from] RepoError),
    /// A money value could not be formed.
    #[error(transparent)]
    Money(#[from] MoneyError),
    /// The audit log could not be written.
    #[error(transparent)]
    Db(#[from] DbError),
    /// The account does not exist.
    #[error("Account not found")]
    AccountNotFound,
    /// The file names several accounts and must be split.
    #[error("{0}")]
    SeveralAccounts(String),
}

/// What an import did.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ImportReport {
    /// The rows inserted.
    pub imported: usize,
    /// Bookings already known by hash.
    pub duplicates: usize,
    /// Inserted rows that a rule categorized.
    pub categorized: usize,
    /// The balance now stored, when the file was allowed to set it.
    pub balance_updated: Option<Decimal>,
    /// Same account, date and amount as an existing booking from another source (sync, manual entry).
    pub probable_duplicates: usize,
    /// Bookings in another currency than the account; never mixed in, since amounts can't be converted.
    pub wrong_currency: usize,
    /// The account's ISO currency code.
    pub currency: Currency,
    /// Bookings the user deleted before; remembered, so they are skipped instead of coming back.
    pub previously_deleted: usize,
    /// Bookings whose amount has more decimal places than the account's currency allows.
    pub invalid_amount: usize,
}

fn plural<'a>(count: usize, singular: &'a str, plural: &'a str) -> &'a str {
    if count == 1 { singular } else { plural }
}

fn currency_symbol(currency: Currency) -> String {
    match currency.as_str() {
        "EUR" => "€".to_owned(),
        "USD" => "$".to_owned(),
        "GBP" => "£".to_owned(),
        other => other.to_owned(),
    }
}

fn format_report_money(amount: Decimal, currency: Currency) -> String {
    let minor = amount_to_money(amount, currency)
        .map(|money| money.minor)
        .unwrap_or_default();
    let plain = crate::money::format_plain(Money {
        minor: minor.abs(),
        currency,
    });
    let (integer, fraction) = plain.split_once('.').unwrap_or((&plain, ""));

    let mut reversed = String::new();
    for (index, character) in integer.chars().rev().enumerate() {
        if index > 0 && index % 3 == 0 {
            reversed.push(',');
        }
        reversed.push(character);
    }
    let grouped: String = reversed.chars().rev().collect();
    let sign = if minor < 0 { "-" } else { "" };
    let fraction = if fraction.is_empty() {
        String::new()
    } else {
        format!(".{fraction}")
    };

    format!("{sign}{}{grouped}{fraction}", currency_symbol(currency))
}

impl ImportReport {
    /// The summary for a single file.
    pub fn summary(&self) -> String {
        self.summary_files(1)
    }

    /// One file reads exactly as today; several files name the file count first.
    pub fn summary_files(&self, files: usize) -> String {
        let mut text = if files <= 1 {
            format!(
                "Imported {} {}",
                self.imported,
                plural(self.imported, "transaction", "transactions")
            )
        } else {
            format!("Imported {} transactions from {files} files", self.imported)
        };

        if self.categorized > 0 {
            let _ = write!(text, ", {} auto-categorized", self.categorized);
        }
        if self.duplicates > 0 {
            let _ = write!(text, ", {} already known", self.duplicates);
        }
        if self.probable_duplicates > 0 {
            let _ = write!(
                text,
                ", {} probable {} of existing bookings skipped",
                self.probable_duplicates,
                plural(self.probable_duplicates, "duplicate", "duplicates")
            );
        }
        if self.previously_deleted > 0 {
            let _ = write!(text, ", {} deleted earlier, skipped", self.previously_deleted);
        }
        if self.wrong_currency > 0 {
            let _ = write!(
                text,
                ", {} in another currency than the account skipped",
                self.wrong_currency
            );
        }
        if self.invalid_amount > 0 {
            let _ = write!(text, ", {} with too many decimal places skipped", self.invalid_amount);
        }
        if let Some(balance) = self.balance_updated {
            let _ = write!(text, ". Balance set to {}", format_report_money(balance, self.currency));
        }

        text
    }
}

/// The account whose IBAN the statement names, if it exists locally.
///
/// The statement's IBAN is normalised before the comparison, as FinDesk does.
pub fn match_account(db: &Database, statement: &ParsedStatement) -> Option<AccountId> {
    let iban = statement.account_iban.as_deref().map(iban::normalize)?;
    repo::accounts::list(db)
        .ok()?
        .into_iter()
        .find(|account| account.iban.as_deref() == Some(iban.as_str()))
        .map(|account| account.id)
}

fn require_single_account(statement: &ParsedStatement) -> Result<(), ImportServiceError> {
    if statement.accounts.len() > 1 {
        let accounts = statement
            .accounts
            .iter()
            .map(|account| iban::mask(account))
            .collect::<Vec<_>>()
            .join(", ");
        return Err(ImportServiceError::SeveralAccounts(format!(
            "This file holds bookings for {} accounts ({}). Export one file per account and import them one at a time.",
            statement.accounts.len(),
            accounts
        )));
    }
    Ok(())
}

/// Stores the bookings. If `update_balance` and the file reports running balances, the account
/// balance is set to the balance after the newest booking. A CAMT file without running balances but
/// with a reconciled closing balance sets the balance from it instead.
pub fn import(
    db: &Database,
    account_id: AccountId,
    statement: &ParsedStatement,
    update_balance: bool,
    today: Date,
) -> Result<ImportReport, ImportServiceError> {
    require_single_account(statement)?;

    let account = repo::accounts::get(db, account_id)?.ok_or(ImportServiceError::AccountNotFound)?;

    // Decided before inserting anything, so the balance check and dedup see the account as it was.
    let (bookings, wrong_currency): (Vec<_>, Vec<_>) = statement.bookings.iter().cloned().partition(|booking| {
        booking
            .currency
            .as_deref()
            .is_none_or(|currency| currency == account.currency.as_str())
    });
    let accepted_statement = ParsedStatement {
        bookings: bookings.clone(),
        ..statement.clone()
    };
    let balance_allowed = balance_update_allowed(db, account_id, &accepted_statement, today)?;

    // Many bank CSV exports (e.g. DKB) list the newest booking first, but the list orders same-day
    // bookings by insertion order (date DESC, id DESC), so same-day balances only chain if the rows
    // are inserted oldest first. Reversing a newest-first file and then stable-sorting by date makes
    // the insert order the booking order within a day too.
    let mut chronological = bookings;
    if is_newest_first(&chronological) {
        chronological.reverse();
    }
    chronological.sort_by_key(|booking| booking.date);

    // Amounts with more decimal places than the currency allows are skipped and counted, not rounded.
    let mut invalid_amount = 0;
    let mut valid = Vec::with_capacity(chronological.len());
    for booking in chronological {
        if amount_to_money(booking.amount, account.currency).is_ok() {
            valid.push(booking);
        } else {
            invalid_amount += 1;
        }
    }
    let chronological = valid;

    let latest_balance = chronological.iter().rev().find_map(|booking| booking.balance_after);

    let iban_text = account.iban.clone().unwrap_or_default();
    let hashes = hash::hashes(&iban_text, &chronological);
    let candidates = chronological
        .into_iter()
        .zip(hashes)
        .map(|(booking, hash)| Candidate {
            date: booking.date,
            amount: booking.amount,
            hash,
            item: booking,
        })
        .collect();
    let plan = dedup::plan(db, account_id, account.currency, candidates)?;

    let exact_duplicates = plan.exact_duplicates;
    let probable_duplicates = plan.probable_duplicates;
    let previously_deleted = plan.previously_deleted;

    let matcher: RuleMatcher = matcher(db)?;
    let mut rows = Vec::with_capacity(plan.to_import.len());
    for candidate in plan.to_import {
        let booking = candidate.item;
        let amount = amount_to_money(booking.amount, account.currency)?;
        let balance_after = booking
            .balance_after
            .map(|balance| amount_to_money(balance, account.currency))
            .transpose()?;
        let category_id = matcher.category_for(
            booking.purpose.as_deref(),
            booking.partner_name.as_deref(),
            Some(booking.amount),
        );
        rows.push(NewTransaction {
            booking_date: booking.date,
            amount,
            balance_after,
            partner_name: booking.partner_name.map(|name| name.chars().take(255).collect()),
            purpose: booking.purpose,
            category_id,
            import_hash: candidate.hash,
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        });
    }
    let categorized = rows.iter().filter(|row| row.category_id.is_some()).count();
    let imported = repo::transactions::insert_planned(db, account_id, rows)?;

    // The list is chronological now, so the newest booking with a balance is simply the last one.
    let new_balance = if !update_balance || !balance_allowed {
        None
    } else if let Some(latest_balance) = latest_balance {
        let money = amount_to_money(latest_balance, account.currency)?;
        repo::accounts::set_balance_for_import(db, account_id, money, OffsetDateTime::now_utc())?;
        Some(latest_balance)
    } else if let Some(closing) = &statement.closing_balance
        && closing
            .currency
            .as_deref()
            .is_none_or(|currency| currency == account.currency.as_str())
    {
        let money = amount_to_money(closing.amount, account.currency)?;
        repo::accounts::set_balance_for_import(db, account_id, money, OffsetDateTime::now_utc())?;
        Some(closing.amount)
    } else {
        None
    };

    let mut details = format!(
        "{} into {}: {} new, {} known, {} probable duplicates, {} deleted earlier",
        statement.format_label(),
        iban::mask(account.iban.as_deref().unwrap_or("")),
        imported,
        exact_duplicates,
        probable_duplicates,
        previously_deleted
    );
    if !wrong_currency.is_empty() {
        let _ = write!(details, ", {} in another currency skipped", wrong_currency.len());
    }
    audit::log(db, "IMPORT_STATEMENT", Actor::User, &details)?;

    Ok(ImportReport {
        imported,
        duplicates: exact_duplicates,
        categorized,
        balance_updated: new_balance,
        probable_duplicates,
        wrong_currency: wrong_currency.len(),
        currency: account.currency,
        previously_deleted,
        invalid_amount,
    })
}

/// The file's figures may only replace the account balance if the file is at least as recent as the
/// account's newest booking; back-filling an old statement must not reset today's balance.
///
/// An account without bookings has no statement history to compare against, so its own balance state
/// decides: if it was never set and the balance is zero, any file may set it; if it was typed by hand
/// and never updated, only a recent file may replace it; if it was updated, only a file at least as
/// recent as that update may replace it.
pub fn balance_update_allowed(
    db: &Database,
    account_id: AccountId,
    statement: &ParsedStatement,
    today: Date,
) -> Result<bool, ImportServiceError> {
    let has_running_balances = statement.bookings.iter().any(|booking| booking.balance_after.is_some());
    let Some(closing_balance) = &statement.closing_balance else {
        if !has_running_balances {
            return Ok(false);
        }
        return balance_update_allowed_for(
            db,
            account_id,
            statement
                .bookings
                .iter()
                .map(|booking| booking.date)
                .max()
                .expect("running balances imply at least one booking"),
            today,
        );
    };

    let file_newest = if has_running_balances {
        statement
            .bookings
            .iter()
            .map(|booking| booking.date)
            .max()
            .expect("running balances imply at least one booking")
    } else {
        closing_balance.date
    };
    balance_update_allowed_for(db, account_id, file_newest, today)
}

fn balance_update_allowed_for(
    db: &Database,
    account_id: AccountId,
    file_newest: Date,
    today: Date,
) -> Result<bool, ImportServiceError> {
    let account_newest = repo::transactions::list_for_account(db, account_id, None, None)?
        .first()
        .map(|transaction| transaction.booking_date);
    if let Some(account_newest) = account_newest {
        return Ok(file_newest >= account_newest);
    }

    let Some(account) = repo::accounts::get(db, account_id)? else {
        return Ok(false);
    };
    match account.balance_updated_at {
        None => {
            if account.balance.minor == 0 {
                Ok(true)
            } else {
                Ok(file_newest >= today - Duration::days(TYPED_BALANCE_RECENT_DAYS))
            }
        }
        Some(last_update) => Ok(file_newest >= last_update.date()),
    }
}

/// Whether the file lists the newest booking first. Its dates decide; a file covering a single day
/// has only its running balances to go by: newest first, each row's balance is the next row's balance
/// plus this row's amount.
pub fn is_newest_first(bookings: &[ImportedBooking]) -> bool {
    if bookings.len() < 2 {
        return false;
    }
    let first = bookings[0].date;
    let last = bookings[bookings.len() - 1].date;
    if first != last {
        return first > last;
    }

    let mut newest_first = 0;
    let mut oldest_first = 0;
    for pair in bookings.windows(2) {
        let (a, b) = (&pair[0], &pair[1]);
        if let (Some(a_balance), Some(b_balance)) = (a.balance_after, b.balance_after) {
            if a_balance == b_balance + a.amount {
                newest_first += 1;
            }
            if b_balance == a_balance + b.amount {
                oldest_first += 1;
            }
        }
    }
    newest_first > oldest_first
}

#[cfg(test)]
mod tests {
    use rusqlite::params;
    use time::Date;
    use time::macros::{date, datetime};

    use super::*;
    use crate::import::read;
    use crate::model::{AccountType, NewAccount, NewCategory, NewRule, RuleDirection, RuleField, Transaction};
    use crate::repo::{accounts, categories, rules, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database, iban: Option<&str>, balance_minor: i64, account_type: AccountType) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: iban.map(str::to_owned),
                account_number: None,
                currency: Currency::EUR,
                account_type,
                balance: Money {
                    minor: balance_minor,
                    currency: Currency::EUR,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn category(db: &Database, name: &str) -> crate::model::CategoryId {
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
        category_id: crate::model::CategoryId,
        priority: i64,
        match_field: RuleField,
        direction: RuleDirection,
    ) {
        rules::create(
            db,
            NewRule {
                pattern: pattern.to_owned(),
                category_id,
                priority,
                auto_apply: true,
                match_field,
                direction,
                tax_relevant: None,
            },
        )
        .unwrap();
    }

    fn booking(
        day: Date,
        amount: &str,
        partner: Option<&str>,
        purpose: Option<&str>,
        balance_after: Option<&str>,
        bank_reference: Option<&str>,
    ) -> ImportedBooking {
        ImportedBooking {
            date: day,
            value_date: None,
            amount: amount.parse().unwrap(),
            currency: Some("EUR".to_owned()),
            partner_name: partner.map(str::to_owned),
            partner_iban: None,
            purpose: purpose.map(str::to_owned),
            balance_after: balance_after.map(|balance| balance.parse().unwrap()),
            bank_reference: bank_reference.map(str::to_owned),
        }
    }

    fn statement(bookings: Vec<ImportedBooking>) -> ParsedStatement {
        ParsedStatement {
            format: crate::import::StatementFormat::Csv,
            bookings,
            account_iban: None,
            accounts: Vec::new(),
            skipped: Vec::new(),
            pending_skipped: 0,
            csv: None,
            format_detail: None,
            closing_balance: None,
        }
    }

    fn store(db: &Database, account_id: AccountId, day: Date, amount_minor: i64, hash: &str) {
        transactions::insert_imported(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount: Money {
                    minor: amount_minor,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: Some("Stored".to_owned()),
                purpose: Some("Stored booking".to_owned()),
                category_id: None,
                import_hash: hash.to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
    }

    fn stored(db: &Database, account_id: AccountId) -> Vec<Transaction> {
        transactions::list_for_account(db, account_id, None, None).unwrap()
    }

    fn stored_amounts_in_insert_order(db: &Database, account_id: AccountId) -> Vec<i64> {
        db.with_conn(|conn| {
            let mut statement =
                conn.prepare("SELECT amount_minor FROM transactions WHERE account_id = ?1 ORDER BY id")?;
            let rows = statement.query_map(params![account_id.0], |row| row.get(0))?;
            Ok(rows.collect::<rusqlite::Result<Vec<i64>>>()?)
        })
        .unwrap()
    }

    fn balance(db: &Database, account_id: AccountId) -> Money {
        accounts::get(db, account_id).unwrap().unwrap().balance
    }

    fn eur(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    #[test]
    fn re_importing_and_overlapping_exports_skip_known_bookings_but_keep_genuine_repeats() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let food = category(&db, "Food");
        rule(&db, "bäcker|kaffee", food, 0, RuleField::Any, RuleDirection::Any);

        let today = date!(2026 - 10 - 05);
        let csv = "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n\
                   03.10.2026;-4,50;Bäcker;Kaffee\n\
                   03.10.2026;-4,50;Bäcker;Kaffee\n\
                   02.10.2026;-10,00;Kiosk;Zeitung\n";
        let parsed = read("a.csv", csv.as_bytes(), None, today).unwrap();
        let report = import(&db, account_id, &parsed, true, today).unwrap();
        assert_eq!(report.imported, 3, "two identical coffees on one day are both real");
        assert_eq!(report.categorized, 2);

        let again = import(&db, account_id, &parsed, true, today).unwrap();
        assert_eq!(again.imported, 0);
        assert_eq!(again.duplicates, 3);

        let overlap = "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n\
                       04.10.2026;-2,00;Kiosk;Wasser\n\
                       03.10.2026;-4,50;Bäcker;Kaffee\n\
                       03.10.2026;-4,50;Bäcker;Kaffee\n";
        let parsed = read("b.csv", overlap.as_bytes(), None, today).unwrap();
        let report = import(&db, account_id, &parsed, true, today).unwrap();
        assert_eq!(report.imported, 1);
        assert_eq!(report.duplicates, 2);
        assert_eq!(stored(&db, account_id).len(), 4);
    }

    #[test]
    fn statement_iban_selects_the_account_and_balance_is_taken_from_the_newest_booking() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);

        let csv = "IBAN;DE89 3704 0044 0532 0130 00\n\n\
                   Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n\
                   02.10.2026;02.10.2026;AMAZON;Lastschrift;Monitor;910,01;EUR;-89,99;EUR\n\
                   01.10.2026;01.10.2026;Employer;Gehalt;Gehalt;1.000,00;EUR;1.000,00;EUR\n";
        let parsed = read("ing.csv", csv.as_bytes(), None, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(match_account(&db, &parsed), Some(account_id));

        let report = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(report.balance_updated, Some("910.01".parse().unwrap()));
        assert_eq!(balance(&db, account_id), eur(91_001));
        assert!(report.summary().contains("Balance set to €910.01"));
    }

    #[test]
    fn newest_first_files_with_running_balances_are_inserted_chronologically() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);

        let newest_first = statement(vec![
            booking(
                date!(2026 - 10 - 02),
                "-89.99",
                Some("AMAZON"),
                Some("Monitor"),
                Some("910.01"),
                None,
            ),
            booking(
                date!(2026 - 10 - 01),
                "1000.00",
                Some("Employer"),
                Some("Gehalt"),
                Some("1000.00"),
                None,
            ),
        ]);
        let report = import(&db, account_id, &newest_first, true, date!(2026 - 10 - 05)).unwrap();

        assert_eq!(report.imported, 2);
        assert_eq!(balance(&db, account_id), eur(91_001));
        assert_eq!(
            stored_amounts_in_insert_order(&db, account_id),
            vec![100_000, -8_999],
            "oldest booking first"
        );
    }

    #[test]
    fn oldest_first_files_still_import_the_same_way() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);

        let oldest_first = statement(vec![
            booking(
                date!(2026 - 10 - 01),
                "1000.00",
                Some("Employer"),
                Some("Gehalt"),
                Some("1000.00"),
                None,
            ),
            booking(
                date!(2026 - 10 - 02),
                "-89.99",
                Some("AMAZON"),
                Some("Monitor"),
                Some("910.01"),
                None,
            ),
        ]);
        let report = import(&db, account_id, &oldest_first, true, date!(2026 - 10 - 05)).unwrap();

        assert_eq!(report.imported, 2);
        assert_eq!(balance(&db, account_id), eur(91_001));
        assert_eq!(stored_amounts_in_insert_order(&db, account_id), vec![100_000, -8_999]);
    }

    #[test]
    fn a_newest_first_file_covering_one_day_is_recognised_by_its_running_balances() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);

        let day = date!(2026 - 10 - 02);
        let one_day = statement(vec![
            booking(day, "-5.00", Some("Kiosk"), Some("Zeitung"), Some("985.00"), None),
            booking(day, "-10.00", Some("Bäcker"), Some("Brot"), Some("990.00"), None),
            booking(day, "1000.00", Some("Employer"), Some("Bonus"), Some("1000.00"), None),
        ]);
        let report = import(&db, account_id, &one_day, true, date!(2026 - 10 - 05)).unwrap();

        assert_eq!(report.imported, 3);
        assert_eq!(balance(&db, account_id), eur(98_500));
        assert_eq!(
            stored_amounts_in_insert_order(&db, account_id),
            vec![100_000, -1_000, -500]
        );
    }

    #[test]
    fn bookings_from_an_imported_file_are_never_marked_new() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);

        let parsed = statement(vec![
            booking(
                date!(2026 - 10 - 03),
                "-4.50",
                Some("Bäcker"),
                Some("Kaffee"),
                None,
                None,
            ),
            booking(
                date!(2026 - 10 - 02),
                "-10.00",
                Some("Kiosk"),
                Some("Zeitung"),
                None,
                None,
            ),
        ]);
        let report = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();

        assert_eq!(report.imported, 2);
        assert!(stored(&db, account_id).iter().all(|booking| !booking.is_new));
    }

    #[test]
    fn camt_files_are_detected_by_content() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let xml = "<?xml version=\"1.0\"?><Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.08\"><BkToCstmrStmt><Stmt><Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct><Ntry><Amt Ccy=\"EUR\">5.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts><Cd>BOOK</Cd></Sts><BookgDt><Dt>2026-10-01</Dt></BookgDt><AcctSvcrRef>X9</AcctSvcrRef></Ntry></Stmt></BkToCstmrStmt></Document>";
        let today = date!(2026 - 10 - 05);
        let parsed = read("export.dat", xml.as_bytes(), None, today).unwrap();
        assert_eq!(parsed.format, crate::import::StatementFormat::Camt053);
        assert_eq!(import(&db, account_id, &parsed, true, today).unwrap().imported, 1);
        assert_eq!(
            import(
                &db,
                account_id,
                &read("export.dat", xml.as_bytes(), None, today).unwrap(),
                true,
                today
            )
            .unwrap()
            .duplicates,
            1
        );
    }

    #[test]
    fn bookings_in_another_currency_are_skipped_and_counted() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let mut usd = booking(date!(2026 - 10 - 03), "-4.50", Some("Shop"), None, None, None);
        usd.currency = Some("USD".to_owned());
        let parsed = statement(vec![
            booking(date!(2026 - 10 - 03), "-4.50", Some("Shop"), None, None, None),
            usd,
        ]);

        let report = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(report.imported, 1);
        assert_eq!(report.wrong_currency, 1);
        assert_eq!(report.currency, Currency::EUR);
        assert!(
            report
                .summary()
                .contains("1 in another currency than the account skipped")
        );
    }

    #[test]
    fn an_amount_with_too_many_decimal_places_is_skipped_and_counted() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let parsed = statement(vec![
            booking(date!(2026 - 10 - 03), "-4.501", Some("Shop"), None, None, None),
            booking(date!(2026 - 10 - 03), "-4.50", Some("Shop"), None, None, None),
        ]);

        let report = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(report.imported, 1);
        assert_eq!(report.invalid_amount, 1);
        assert!(report.summary().contains("1 with too many decimal places skipped"));
    }

    #[test]
    fn an_import_writes_one_audit_entry_with_masked_iban_and_counts() {
        let (_temp, db) = database();
        let iban = "DE89370400440532013000";
        let account_id = account(&db, Some(iban), 0, AccountType::Checking);
        let parsed = statement(vec![booking(
            date!(2026 - 10 - 03),
            "-4.50",
            Some("Shop"),
            None,
            Some("100.00"),
            None,
        )]);

        import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();

        let entries = audit::recent(&db, 10).unwrap();
        let import_entry = entries
            .iter()
            .find(|entry| entry.action == "IMPORT_STATEMENT")
            .expect("an IMPORT_STATEMENT entry");
        assert_eq!(import_entry.actor, Actor::User);
        assert!(import_entry.details.contains("…3000"));
        assert!(!import_entry.details.contains(iban), "the full IBAN must not be logged");
        assert!(
            import_entry
                .details
                .contains("1 new, 0 known, 0 probable duplicates, 0 deleted earlier")
        );
        assert_eq!(
            entries
                .iter()
                .filter(|entry| entry.action == "IMPORT_STATEMENT")
                .count(),
            1
        );
        assert!(
            entries.iter().all(|entry| entry.action != "account.set_balance"),
            "the import's balance update is part of the single import audit entry"
        );
    }

    #[test]
    fn an_import_with_two_accounts_is_refused_and_stores_nothing() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let mut parsed = statement(vec![booking(
            date!(2026 - 10 - 01),
            "-10.00",
            Some("Partner"),
            Some("Test"),
            None,
            None,
        )]);
        parsed.accounts = vec!["DE89370400440532013000".to_owned(), "DE44500105175407324931".to_owned()];

        let error = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap_err();
        let message = error.to_string();
        assert!(message.contains('2'), "{message}");
        assert!(message.contains("…3000"), "{message}");
        assert!(message.contains("…4931"), "{message}");
        assert!(stored(&db, account_id).is_empty());
    }

    #[test]
    fn two_statement_blocks_for_the_same_account_are_accepted_and_import_all_bookings() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let mut parsed = statement(vec![
            booking(
                date!(2026 - 10 - 01),
                "-10.00",
                Some("Partner"),
                Some("Test"),
                None,
                None,
            ),
            booking(
                date!(2026 - 10 - 02),
                "-20.00",
                Some("Partner"),
                Some("Test"),
                None,
                None,
            ),
        ]);
        parsed.accounts = vec!["DE89370400440532013000".to_owned()];

        let report = import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(report.imported, 2);
        assert_eq!(stored(&db, account_id).len(), 2);
    }

    // RuleDirectionImportTest's `auto categorize via the view model` test is skipped: it drives
    // `TransactionsViewModel` from the UI crate, which M3b does not port. The rule itself is covered by
    // the import test below and by `rules::matcher`.

    #[test]
    fn an_expense_only_rule_categorizes_only_the_expense() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let groceries = category(&db, "Groceries");
        rule(
            &db,
            "Erika Mustermann",
            groceries,
            5,
            RuleField::Partner,
            RuleDirection::Expense,
        );

        let parsed = statement(vec![
            booking(
                date!(2026 - 10 - 03),
                "-45.67",
                Some("Erika Mustermann"),
                Some("Kartenzahlung"),
                None,
                None,
            ),
            booking(
                date!(2026 - 10 - 03),
                "1000.00",
                Some("Erika Mustermann"),
                Some("Rueckzahlung"),
                None,
                None,
            ),
        ]);
        import(&db, account_id, &parsed, true, date!(2026 - 10 - 05)).unwrap();

        let mut stored = stored(&db, account_id);
        stored.sort_by_key(|booking| booking.amount.minor);
        let expense = stored.iter().find(|booking| booking.amount.minor < 0).unwrap();
        let income = stored.iter().find(|booking| booking.amount.minor > 0).unwrap();
        assert_eq!(expense.category_id, Some(groceries));
        assert_eq!(income.category_id, None);
    }

    // DeletedBookingsTest's repository half (delete/deleteAll/forgetDeleted) and its AccountsViewModel
    // message test belong to other milestones; M3b only needs the dedup and import-skip path, covered
    // here and in `import::dedup`.

    #[test]
    fn a_remembered_deleted_booking_stays_out_of_a_re_import() {
        let (_temp, db) = database();
        let iban = "DE89370400440532013016";
        let account_id = account(&db, Some(iban), 0, AccountType::Checking);
        let day = date!(2026 - 10 - 05);
        let parsed = statement(vec![
            booking(day, "-5.00", Some("Cafe"), None, None, Some("REF-CAFE")),
            booking(
                day.previous_day().unwrap(),
                "-7.00",
                Some("Bakery"),
                None,
                None,
                Some("REF-BAKERY"),
            ),
            booking(
                day.previous_day().unwrap().previous_day().unwrap(),
                "-9.00",
                Some("Bookshop"),
                None,
                None,
                Some("REF-BOOKSHOP"),
            ),
        ]);

        assert_eq!(import(&db, account_id, &parsed, false, day).unwrap().imported, 3);

        let cafe = stored(&db, account_id)
            .into_iter()
            .find(|booking| booking.partner_name.as_deref() == Some("Cafe"))
            .unwrap();
        transactions::delete(&db, cafe.id).unwrap();

        let rerun = import(&db, account_id, &parsed, false, day).unwrap();
        assert_eq!(rerun.imported, 0, "the deleted booking stays out");
        assert_eq!(rerun.previously_deleted, 1);
        assert_eq!(rerun.duplicates, 2, "the two stored bookings count as known");
        assert!(rerun.summary().contains(", 1 deleted earlier, skipped"));
    }

    #[test]
    fn a_stored_booking_from_another_source_is_a_probable_duplicate_for_the_import() {
        let (_temp, db) = database();
        let iban = "DE89370400440532013000";
        let account_id = account(&db, Some(iban), 0, AccountType::Checking);
        let day = date!(2026 - 10 - 03);
        store(&db, account_id, day, -4_567, "bank-sync-hash");

        let parsed = statement(vec![booking(
            day,
            "-45.67",
            Some("REWE"),
            Some("Kartenzahlung"),
            None,
            None,
        )]);
        let report = import(&db, account_id, &parsed, true, day).unwrap();

        assert_eq!(report.imported, 0);
        assert_eq!(report.probable_duplicates, 1);
        assert!(report.summary().contains("1 probable duplicate"));
    }

    #[test]
    fn a_manual_entry_followed_by_an_import_of_the_same_booking_is_a_probable_duplicate() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let day = date!(2026 - 10 - 03);
        store(&db, account_id, day, -8_999, "manual");

        let parsed = statement(vec![booking(
            day,
            "-89.99",
            Some("AMAZON EU"),
            Some("Bestellung 303"),
            None,
            None,
        )]);
        let report = import(&db, account_id, &parsed, true, day).unwrap();

        assert_eq!(report.imported, 0);
        assert_eq!(report.probable_duplicates, 1);
    }

    #[test]
    fn an_old_statement_does_not_reset_a_newer_balance() {
        let (_temp, db) = database();
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        store(&db, account_id, date!(2026 - 10 - 04), -100, "newest");
        accounts::set_balance(&db, account_id, eur(500_000), datetime!(2026 - 10 - 05 12:00 UTC)).unwrap();

        let old = statement(vec![booking(
            date!(2025 - 01 - 15),
            "-10.00",
            Some("Old shop"),
            Some("Old"),
            Some("1234.00"),
            None,
        )]);
        assert!(!balance_update_allowed(&db, account_id, &old, date!(2026 - 10 - 05)).unwrap());

        let report = import(&db, account_id, &old, true, date!(2026 - 10 - 05)).unwrap();
        assert_eq!(report.imported, 1);
        assert_eq!(report.balance_updated, None);
        assert_eq!(balance(&db, account_id), eur(500_000));
    }

    // The two CrossSourceDedup tests that drive a real FinTS sync (`csv back-fill then sync` and
    // `genuine extra repeats and different amounts are still imported`) are skipped here: M3b has no
    // bank sync code. The import-side probable-duplicate behaviour they cover is tested above and in
    // `import::dedup`.

    #[test]
    fn an_old_file_never_replaces_a_balance_typed_on_an_account_without_bookings() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 50_000, AccountType::Checking);
        let old = statement(vec![booking(
            date!(2019 - 12 - 31),
            "10.00",
            None,
            None,
            Some("100.00"),
            None,
        )]);

        assert!(!balance_update_allowed(&db, account_id, &old, today).unwrap());

        let report = import(&db, account_id, &old, true, today).unwrap();
        assert_eq!(report.imported, 1);
        assert_eq!(report.balance_updated, None);
        assert_eq!(balance(&db, account_id), eur(50_000));
    }

    #[test]
    fn a_zero_balance_on_an_account_without_bookings_may_still_be_set() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        let old = statement(vec![booking(
            date!(2019 - 12 - 31),
            "10.00",
            None,
            None,
            Some("100.00"),
            None,
        )]);

        assert!(balance_update_allowed(&db, account_id, &old, today).unwrap());
    }

    #[test]
    fn a_zero_balance_set_today_is_not_empty_and_rejects_an_old_file() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 0, AccountType::Checking);
        accounts::set_balance(&db, account_id, eur(0), datetime!(2026 - 05 - 20 08:00 UTC)).unwrap();
        let old = statement(vec![booking(
            date!(2019 - 12 - 31),
            "10.00",
            None,
            None,
            Some("100.00"),
            None,
        )]);

        assert!(!balance_update_allowed(&db, account_id, &old, today).unwrap());
        let current = statement(vec![booking(today, "10.00", None, None, Some("100.00"), None)]);
        assert!(balance_update_allowed(&db, account_id, &current, today).unwrap());

        let report = import(&db, account_id, &old, true, today).unwrap();
        assert_eq!(report.balance_updated, None);
        assert_eq!(balance(&db, account_id).minor, 0);
    }

    #[test]
    fn a_balance_set_by_update_balance_only_accepts_a_file_at_least_as_recent_as_the_update() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 50_000, AccountType::Checking);
        accounts::set_balance(&db, account_id, eur(50_000), datetime!(2026 - 05 - 20 08:00 UTC)).unwrap();

        assert!(
            balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(today, "10.00", None, None, Some("100.00"), None)]),
                today,
            )
            .unwrap()
        );
        assert!(
            !balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    today.previous_day().unwrap(),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
    }

    #[test]
    fn a_typed_balance_accepts_a_file_45_days_old() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 50_000, AccountType::Checking);

        assert!(
            balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    date!(2026 - 04 - 05),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
    }

    #[test]
    fn a_typed_balance_rejects_a_file_46_days_old() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 50_000, AccountType::Checking);

        assert!(
            !balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    date!(2026 - 04 - 04),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
    }

    #[test]
    fn a_card_account_with_a_typed_balance_rejects_an_old_file() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, None, 10_000, AccountType::CreditCard);

        assert!(
            !balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    date!(2019 - 12 - 31),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
    }

    #[test]
    fn an_account_with_bookings_still_compares_the_file_with_its_newest_booking() {
        let (_temp, db) = database();
        let today = date!(2026 - 05 - 20);
        let account_id = account(&db, Some("DE89370400440532013000"), 100_000, AccountType::Checking);
        store(&db, account_id, date!(2026 - 10 - 01), -1_000, "existing");

        assert!(
            balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    date!(2026 - 10 - 01),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
        assert!(
            !balance_update_allowed(
                &db,
                account_id,
                &statement(vec![booking(
                    date!(2026 - 09 - 30),
                    "10.00",
                    None,
                    None,
                    Some("100.00"),
                    None
                )]),
                today,
            )
            .unwrap()
        );
    }
}
