//! Golden outputs captured from the Rust behaviour, read by the Go tests in G2–G5.
//!
//! The generator runs by hand:
//!
//! ```text
//! cargo test -p finglass-core write_goldens -- --ignored
//! ```
//!
//! It reads `testdata/inputs/<area>/` and writes `testdata/golden/<area>/`. [`goldens_are_current`] regenerates
//! everything into a temporary directory and diffs it against the committed files, so a Rust change cannot leave a
//! stale golden behind.

use std::collections::BTreeMap;
use std::fs;
use std::io::{Cursor, Read, Write};
use std::path::{Path, PathBuf};

use rust_decimal::Decimal;
use time::macros::datetime;
use time::{Date, Month};

use crate::balance::{BalanceHistory, RunningBalance};
use crate::dashboard::{DashboardFigures, YearMonth};
use crate::db::Database;
use crate::i18n::{self, Key, Language};
use crate::import::hash::{hashes, import_hash, transaction_hash};
use crate::import::model::{ClosingBalance, CsvLayout, CsvMapping, ImportError, ImportedBooking, ParsedStatement};
use crate::import::{self, amount_to_money};
use crate::model::{
    AccountId, AccountType, CategoryId, NewAccount, NewCategory, NewRule, RuleDirection, RuleField, TaxSource,
    TransactionId, TransactionItem,
};
use crate::money::{self, Currency, Money};
use crate::receipt;
use crate::recurring::{RecurringDetector, RecurringPayment};
use crate::repo::{accounts, categories, rules as rules_repo, transactions};
use crate::report::csv as report_csv;
use crate::report::pdf_report::PdfReportExporter;
use crate::report::service::{
    self as report_service, CategoryShare, ExpenseItem, MonthlyData, MonthlyReport, TaxBookings, TaxGroup, TaxReport,
};
use crate::report::tax_export::TaxReportExporter;
use crate::rules::file::{RuleFile, RuleFileRow};
use crate::rules::matcher::{RuleMatcher, RuleSpec, compile};
use crate::rules::payment_providers::PaymentProviders;
use crate::secret::{Passphrase, PassphrasePurpose};

/// The fixed "today" for statement parsing and every dashboard/report load.
fn today() -> Date {
    time::macros::date!(2026 - 10 - 15)
}

/// The fixed "today" the receipt tests use.
fn receipt_today() -> Date {
    time::macros::date!(2026 - 11 - 07)
}

/// The test-only passphrase for the throwaway dashboard database.
const FIXTURE_PASSPHRASE: &str = "correct horse battery staple";

/// Every generated area. The top-level G0a/G0c goldens are left untouched.
const AREAS: &[&str] = &[
    "import",
    "hash",
    "dedup",
    "format",
    "iban",
    "rules",
    "bank_text",
    "balance",
    "recurring",
    "receipt",
    "dashboard",
    "report",
    "export",
    "i18n",
    "bank_directory",
];

// ---------------------------------------------------------------------------------------------------
// A tiny JSON writer. The crate does not depend on serde, so the generator writes the goldens itself.
// ---------------------------------------------------------------------------------------------------

enum Json {
    Null,
    Bool(bool),
    Raw(String),
    Text(String),
    Array(Vec<Json>),
    Object(Vec<(String, Json)>),
}

impl Json {
    fn pretty(&self) -> String {
        let mut out = String::new();
        self.write(&mut out, 0);
        out
    }

    fn write(&self, out: &mut String, indent: usize) {
        match self {
            Json::Null => out.push_str("null"),
            Json::Bool(value) => out.push_str(if *value { "true" } else { "false" }),
            Json::Raw(value) => out.push_str(value),
            Json::Text(value) => out.push_str(&json_string(value)),
            Json::Array(items) => {
                if items.is_empty() {
                    out.push_str("[]");
                    return;
                }
                out.push_str("[\n");
                for (index, item) in items.iter().enumerate() {
                    out.push_str(&"  ".repeat(indent + 1));
                    item.write(out, indent + 1);
                    if index + 1 < items.len() {
                        out.push(',');
                    }
                    out.push('\n');
                }
                out.push_str(&"  ".repeat(indent));
                out.push(']');
            }
            Json::Object(entries) => {
                if entries.is_empty() {
                    out.push_str("{}");
                    return;
                }
                out.push_str("{\n");
                for (index, (key, value)) in entries.iter().enumerate() {
                    out.push_str(&"  ".repeat(indent + 1));
                    out.push_str(&json_string(key));
                    out.push_str(": ");
                    value.write(out, indent + 1);
                    if index + 1 < entries.len() {
                        out.push(',');
                    }
                    out.push('\n');
                }
                out.push_str(&"  ".repeat(indent));
                out.push('}');
            }
        }
    }
}

fn object(entries: Vec<(&str, Json)>) -> Json {
    Json::Object(
        entries
            .into_iter()
            .map(|(key, value)| (key.to_owned(), value))
            .collect(),
    )
}

fn array(items: Vec<Json>) -> Json {
    Json::Array(items)
}

fn text(value: impl Into<String>) -> Json {
    Json::Text(value.into())
}

fn optional(value: Option<impl Into<String>>) -> Json {
    value.map_or(Json::Null, |value| Json::Text(value.into()))
}

fn number(value: i64) -> Json {
    Json::Raw(value.to_string())
}

fn float(value: f64) -> Json {
    if value.is_finite() {
        Json::Raw(format!("{value}"))
    } else {
        Json::Null
    }
}

fn boolean(value: bool) -> Json {
    Json::Bool(value)
}

fn json_string(value: &str) -> String {
    let mut out = String::with_capacity(value.len() + 2);
    out.push('"');
    for character in value.chars() {
        match character {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            character if (character as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", character as u32)),
            character => out.push(character),
        }
    }
    out.push('"');
    out
}

// ---------------------------------------------------------------------------------------------------
// Shared value helpers.
// ---------------------------------------------------------------------------------------------------

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(2)
        .expect("crates/finglass-core has a repository root")
        .to_path_buf()
}

fn inputs_dir() -> PathBuf {
    repo_root().join("testdata").join("inputs")
}

fn golden_dir() -> PathBuf {
    repo_root().join("testdata").join("golden")
}

fn read_input(area: &str, file: &str) -> Vec<u8> {
    let path = inputs_dir().join(area).join(file);
    fs::read(&path).unwrap_or_else(|error| panic!("read {}: {error}", path.display()))
}

fn input_files(area: &str) -> Vec<String> {
    let path = inputs_dir().join(area);
    let mut files: Vec<String> = fs::read_dir(&path)
        .unwrap_or_else(|error| panic!("read_dir {}: {error}", path.display()))
        .map(|entry| {
            entry
                .expect("a readable directory entry")
                .file_name()
                .to_string_lossy()
                .into_owned()
        })
        .filter(|name| !name.starts_with('.'))
        .collect();
    files.sort();
    files
}

fn money_json(value: Money) -> Json {
    object(vec![
        ("minor", number(value.minor)),
        ("currency", text(value.currency.as_str())),
        ("plain", text(money::format_plain(value))),
    ])
}

fn optional_money(value: Option<Money>) -> Json {
    value.map_or(Json::Null, money_json)
}

fn decimal_text(value: Decimal) -> String {
    value.to_string()
}

fn optional_decimal(value: Option<Decimal>) -> Json {
    value.map_or(Json::Null, |value| text(decimal_text(value)))
}

fn iso_date(value: Date) -> String {
    format!("{:04}-{:02}-{:02}", value.year(), value.month() as u8, value.day())
}

fn optional_date(value: Option<Date>) -> Json {
    value.map_or(Json::Null, |value| text(iso_date(value)))
}

fn year_month_json(value: YearMonth) -> Json {
    object(vec![
        ("year", number(i64::from(value.year))),
        ("month", number(i64::from(value.month))),
    ])
}

fn date_json(value: Date) -> Json {
    text(iso_date(value))
}

fn currency_of(value: Option<&str>) -> Currency {
    value.and_then(|code| Currency::new(code).ok()).unwrap_or(Currency::EUR)
}

fn booking_money(booking: &ImportedBooking) -> Money {
    let currency = currency_of(booking.currency.as_deref());
    let minor = amount_to_money(booking.amount, currency)
        .map(|money| money.minor)
        .unwrap_or(0);
    Money { minor, currency }
}

fn item_from_booking(id: i64, booking: &ImportedBooking, account_iban: Option<&str>) -> TransactionItem {
    let currency = currency_of(booking.currency.as_deref());
    TransactionItem {
        id: TransactionId(id),
        account_id: AccountId(1),
        account_iban: account_iban.map(str::to_owned),
        date: booking.date,
        amount: booking_money(booking),
        balance_after: booking.balance_after.map(|balance| Money {
            minor: amount_to_money(balance, currency).map(|money| money.minor).unwrap_or(0),
            currency,
        }),
        currency,
        partner_name: booking.partner_name.clone(),
        purpose: booking.purpose.clone(),
        category_id: None,
        category_name: None,
        category_color: None,
        is_tax_relevant: false,
        document_count: 0,
        is_new: false,
        is_transfer: false,
        tax_source: TaxSource::None,
        tax_rule_pattern: None,
        tax_override: None,
    }
}

fn item_json(item: &TransactionItem) -> Json {
    object(vec![
        ("id", number(item.id.0)),
        ("account_id", number(item.account_id.0)),
        ("account_iban", optional(item.account_iban.as_deref())),
        ("date", date_json(item.date)),
        ("amount", money_json(item.amount)),
        ("balance_after", optional_money(item.balance_after)),
        ("currency", text(item.currency.as_str())),
        ("partner_name", optional(item.partner_name.as_deref())),
        ("purpose", optional(item.purpose.as_deref())),
        ("category_id", item.category_id.map_or(Json::Null, |id| number(id.0))),
        ("category_name", optional(item.category_name.as_deref())),
        ("category_color", optional(item.category_color.as_deref())),
        ("is_tax_relevant", boolean(item.is_tax_relevant)),
        ("document_count", number(item.document_count as i64)),
        ("is_new", boolean(item.is_new)),
        ("is_transfer", boolean(item.is_transfer)),
        (
            "tax_source",
            text(match item.tax_source {
                TaxSource::Manual => "Manual",
                TaxSource::Rule => "Rule",
                TaxSource::Category => "Category",
                TaxSource::None => "None",
            }),
        ),
        ("tax_rule_pattern", optional(item.tax_rule_pattern.as_deref())),
        ("tax_override", item.tax_override.map_or(Json::Null, boolean)),
    ])
}

// ---------------------------------------------------------------------------------------------------
// import/
// ---------------------------------------------------------------------------------------------------

struct ImportFile {
    name: String,
    statement: Result<ParsedStatement, String>,
}

fn read_imports() -> Vec<ImportFile> {
    input_files("import")
        .into_iter()
        .map(|name| {
            let path = inputs_dir().join("import").join(&name);
            let bytes = fs::read(&path).unwrap_or_else(|error| panic!("read {}: {error}", path.display()));
            let statement = import::read(&name, &bytes, None, today()).map_err(|error: ImportError| error.to_string());
            ImportFile { name, statement }
        })
        .collect()
}

fn csv_mapping_json(mapping: &CsvMapping) -> Json {
    object(vec![
        ("date", mapping.date.map_or(Json::Null, |value| number(value as i64))),
        (
            "value_date",
            mapping.value_date.map_or(Json::Null, |value| number(value as i64)),
        ),
        (
            "amount",
            mapping.amount.map_or(Json::Null, |value| number(value as i64)),
        ),
        ("debit", mapping.debit.map_or(Json::Null, |value| number(value as i64))),
        (
            "credit",
            mapping.credit.map_or(Json::Null, |value| number(value as i64)),
        ),
        (
            "partner",
            mapping.partner.map_or(Json::Null, |value| number(value as i64)),
        ),
        ("payer", mapping.payer.map_or(Json::Null, |value| number(value as i64))),
        (
            "partner_iban",
            mapping.partner_iban.map_or(Json::Null, |value| number(value as i64)),
        ),
        (
            "purpose",
            mapping.purpose.map_or(Json::Null, |value| number(value as i64)),
        ),
        (
            "currency",
            mapping.currency.map_or(Json::Null, |value| number(value as i64)),
        ),
        (
            "balance",
            mapping.balance.map_or(Json::Null, |value| number(value as i64)),
        ),
    ])
}

fn csv_layout_json(layout: &CsvLayout) -> Json {
    object(vec![
        ("delimiter", text(layout.delimiter.to_string())),
        ("charset_name", text(layout.charset_name.clone())),
        ("header_line", number(layout.header_line as i64)),
        (
            "headers",
            array(layout.headers.iter().map(|value| text(value.clone())).collect()),
        ),
        ("mapping", csv_mapping_json(&layout.mapping)),
    ])
}

fn closing_balance_json(balance: &ClosingBalance) -> Json {
    object(vec![
        ("amount", text(decimal_text(balance.amount))),
        ("date", date_json(balance.date)),
        ("currency", optional(balance.currency.clone())),
        ("pending_excluded", number(i64::from(balance.pending_excluded))),
    ])
}

fn imported_booking_json(booking: &ImportedBooking) -> Json {
    object(vec![
        ("date", date_json(booking.date)),
        ("value_date", optional_date(booking.value_date)),
        ("amount", text(decimal_text(booking.amount))),
        ("currency", optional(booking.currency.clone())),
        ("partner_name", optional(booking.partner_name.clone())),
        ("partner_iban", optional(booking.partner_iban.clone())),
        ("purpose", optional(booking.purpose.clone())),
        ("balance_after", optional_decimal(booking.balance_after)),
        ("bank_reference", optional(booking.bank_reference.clone())),
    ])
}

fn statement_json(statement: &ParsedStatement) -> Json {
    object(vec![
        ("format", text(format!("{:?}", statement.format))),
        ("format_label", text(statement.format_label())),
        ("format_detail", optional(statement.format_detail.clone())),
        ("account_iban", optional(statement.account_iban.clone())),
        (
            "accounts",
            array(statement.accounts.iter().map(|value| text(value.clone())).collect()),
        ),
        ("pending_skipped", number(i64::from(statement.pending_skipped))),
        (
            "skipped",
            array(statement.skipped.iter().map(|value| text(value.clone())).collect()),
        ),
        ("csv", statement.csv.as_ref().map_or(Json::Null, csv_layout_json)),
        (
            "closing_balance",
            statement
                .closing_balance
                .as_ref()
                .map_or(Json::Null, closing_balance_json),
        ),
        (
            "bookings",
            array(statement.bookings.iter().map(imported_booking_json).collect()),
        ),
    ])
}

fn import_json(imports: &[ImportFile]) -> Json {
    array(
        imports
            .iter()
            .map(|file| match &file.statement {
                Ok(statement) => object(vec![
                    ("file", text(file.name.clone())),
                    ("ok", boolean(true)),
                    ("statement", statement_json(statement)),
                ]),
                Err(error) => object(vec![
                    ("file", text(file.name.clone())),
                    ("ok", boolean(false)),
                    ("error", text(error.clone())),
                ]),
            })
            .collect(),
    )
}

// ---------------------------------------------------------------------------------------------------
// hash/
// ---------------------------------------------------------------------------------------------------

fn hash_json(imports: &[ImportFile]) -> Json {
    type HashCase = (
        &'static str,
        Date,
        &'static str,
        Option<&'static str>,
        Option<&'static str>,
    );

    let files: Vec<Json> = imports
        .iter()
        .filter_map(|file| {
            let statement = file.statement.as_ref().ok()?;
            let iban = statement.account_iban.clone().unwrap_or_default();
            let file_hashes = hashes(&iban, &statement.bookings);
            let bookings: Vec<Json> = statement
                .bookings
                .iter()
                .enumerate()
                .map(|(index, booking)| {
                    object(vec![
                        ("index", number(index as i64)),
                        (
                            "transaction_hash",
                            text(transaction_hash(
                                booking.date,
                                booking.amount,
                                booking.partner_name.as_deref(),
                                booking.purpose.as_deref(),
                            )),
                        ),
                        (
                            "import_hash",
                            text(import_hash(
                                booking.date,
                                booking.amount,
                                booking.partner_name.as_deref(),
                                booking.purpose.as_deref(),
                                &iban,
                                booking.bank_reference.as_deref(),
                                booking.balance_after,
                                None,
                            )),
                        ),
                    ])
                })
                .collect();
            Some(object(vec![
                ("file", text(file.name.clone())),
                ("iban", text(iban)),
                ("file_hashes", array(file_hashes.into_iter().map(Json::Text).collect())),
                ("bookings", array(bookings)),
            ]))
        })
        .collect();

    let cases: Vec<HashCase> = vec![
        (
            "empty partner and purpose",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            None,
            None,
        ),
        (
            "empty partner",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            None,
            Some("Kaffee"),
        ),
        (
            "empty purpose",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Bäcker"),
            None,
        ),
        (
            "empty purpose string",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Bäcker"),
            Some(""),
        ),
        (
            "umlauts",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Bäcker"),
            Some("Kaffee"),
        ),
        (
            "sharp s",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Straße"),
            Some("Grüße"),
        ),
        (
            "nfc",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Müller"),
            Some("Überweisung"),
        ),
        (
            "nfd",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("Mu\u{0308}ller"),
            Some("U\u{0308}berweisung"),
        ),
        (
            "trailing spaces",
            time::macros::date!(2026 - 10 - 03),
            "10.50",
            Some("REWE "),
            Some("Kaffee  "),
        ),
        (
            "negative zero",
            time::macros::date!(2026 - 10 - 03),
            "-0.00",
            Some("Zero"),
            None,
        ),
        (
            "negative zero with purpose",
            time::macros::date!(2026 - 10 - 03),
            "-0.0",
            None,
            Some("Zero"),
        ),
        ("0.10", time::macros::date!(2026 - 10 - 03), "0.10", Some("Shop"), None),
        ("0.1", time::macros::date!(2026 - 10 - 03), "0.1", Some("Shop"), None),
        ("100.00", time::macros::date!(2026 - 10 - 03), "100.00", None, None),
        ("1E+2", time::macros::date!(2026 - 10 - 03), "1E+2", None, None),
        (
            "negative amount",
            time::macros::date!(2026 - 10 - 01),
            "-49.99",
            Some("REWE"),
            Some("Shopping"),
        ),
        (
            "newline in text",
            time::macros::date!(2026 - 10 - 03),
            "1.00",
            Some("A\nB"),
            Some("C\tD"),
        ),
    ];

    let edge_cases: Vec<Json> = cases
        .iter()
        .map(|(name, date, amount, partner, purpose)| {
            let amount = amount.parse::<Decimal>().expect("a valid Decimal input");
            object(vec![
                ("name", text(*name)),
                ("date", date_json(*date)),
                ("amount", text(decimal_text(amount))),
                ("partner", optional(*partner)),
                ("purpose", optional(*purpose)),
                (
                    "transaction_hash",
                    text(transaction_hash(*date, amount, *partner, *purpose)),
                ),
            ])
        })
        .collect();

    object(vec![("files", array(files)), ("edge_cases", array(edge_cases))])
}

// ---------------------------------------------------------------------------------------------------
// dedup/
// ---------------------------------------------------------------------------------------------------

fn dedup_json() -> Json {
    let temp = tempfile::tempdir().expect("a temp directory");
    let passphrase = Passphrase::new(FIXTURE_PASSPHRASE, PassphrasePurpose::New).expect("the fixture passphrase");
    let db = Database::create(&temp.path().join("finglass.db"), &passphrase).expect("the fixture database");
    let account = accounts::create(
        &db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Checking".to_owned()),
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
    .expect("the fixture account");
    let food = categories::create(
        &db,
        NewCategory {
            name: "Food".to_owned(),
            color: "#3BC7B0".to_owned(),
            budget_limit: None,
            is_tax_relevant: false,
            is_transfer: false,
        },
    )
    .expect("the fixture category");
    rules_repo::create(
        &db,
        NewRule {
            pattern: "bäcker|kaffee".to_owned(),
            category_id: food,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .expect("the fixture rule");

    let a = import::read(
        "dedup_a.csv",
        &read_input("dedup", "dedup_a.csv"),
        None,
        time::macros::date!(2026 - 10 - 05),
    )
    .expect("the first statement");
    let b = import::read(
        "dedup_b.csv",
        &read_input("dedup", "dedup_b.csv"),
        None,
        time::macros::date!(2026 - 10 - 05),
    )
    .expect("the overlapping statement");
    let c = import::read(
        "dedup_c.csv",
        &read_input("dedup", "dedup_c.csv"),
        None,
        time::macros::date!(2026 - 10 - 05),
    )
    .expect("the deletable statement");

    let mut steps = Vec::new();
    let record = |name: &str, statement: &ParsedStatement, steps: &mut Vec<Json>| {
        let report = import::import(&db, account, statement, true, time::macros::date!(2026 - 10 - 05))
            .expect("the import succeeds");
        steps.push(object(vec![
            ("step", text(name)),
            ("imported", number(report.imported as i64)),
            ("duplicates", number(report.duplicates as i64)),
            ("categorized", number(report.categorized as i64)),
            ("probable_duplicates", number(report.probable_duplicates as i64)),
            ("previously_deleted", number(report.previously_deleted as i64)),
            ("invalid_amount", number(report.invalid_amount as i64)),
            ("wrong_currency", number(report.wrong_currency as i64)),
            ("balance_updated", optional_decimal(report.balance_updated)),
            ("summary", text(report.summary())),
        ]));
    };

    record("first import of a", &a, &mut steps);
    record("re-import of a", &a, &mut steps);
    record("overlapping b", &b, &mut steps);
    record("first import of c", &c, &mut steps);

    // Delete one stored booking, then re-import c: the deleted hash stays out.
    let stored = transactions::list_for_account(&db, account, None, None).expect("stored bookings");
    let cafe = stored
        .iter()
        .find(|booking| booking.partner_name.as_deref() == Some("Cafe"))
        .expect("the Cafe booking");
    transactions::delete(&db, cafe.id).expect("delete the Cafe booking");
    record("re-import of c after deleting Cafe", &c, &mut steps);

    let stored_json: Vec<Json> = transactions::list_for_account(&db, account, None, None)
        .expect("stored bookings")
        .into_iter()
        .map(|booking| {
            object(vec![
                ("date", date_json(booking.booking_date)),
                ("amount", money_json(booking.amount)),
                ("partner_name", optional(booking.partner_name.clone())),
                ("purpose", optional(booking.purpose.clone())),
                ("import_hash", text(booking.import_hash.clone())),
                ("is_new", boolean(booking.is_new)),
            ])
        })
        .collect();

    object(vec![("steps", array(steps)), ("stored", array(stored_json))])
}

// ---------------------------------------------------------------------------------------------------
// format/
// ---------------------------------------------------------------------------------------------------

fn format_json() -> Json {
    let currencies: Vec<Currency> = ["EUR", "USD", "CHF", "JPY", "KWD", "GBP", "QQQ"]
        .iter()
        .map(|code| Currency::new(code).expect("a valid test currency"))
        .collect();
    let minors: Vec<i64> = vec![
        0, 1, -1, 5, 50, 4_567, 12_345, 43_100, 123_450, 245_037, -400_049, 400_050, 3_200_000,
    ];

    let mut money_rows = Vec::new();
    let mut whole_rows = Vec::new();
    for currency in &currencies {
        for minor in &minors {
            let value = Money {
                minor: *minor,
                currency: *currency,
            };
            money_rows.push(object(vec![
                ("minor", number(*minor)),
                ("currency", text(currency.as_str())),
                ("en", text(crate::format::money(value, Language::En, false))),
                ("de", text(crate::format::money(value, Language::De, false))),
                ("fr", text(crate::format::money(value, Language::Fr, false))),
                ("en_signed", text(crate::format::money(value, Language::En, true))),
                ("de_signed", text(crate::format::money(value, Language::De, true))),
                ("fr_signed", text(crate::format::money(value, Language::Fr, true))),
            ]));
            whole_rows.push(object(vec![
                ("minor", number(*minor)),
                ("currency", text(currency.as_str())),
                ("en", text(crate::format::whole_money(value, Language::En))),
                ("de", text(crate::format::whole_money(value, Language::De))),
                ("fr", text(crate::format::whole_money(value, Language::Fr))),
            ]));
        }
    }

    let percents = [
        0.0, -0.1, 0.0001, 0.004, 0.0049, 0.005, 0.03, 0.1234, 0.49, 0.5, 0.999, 1.0, 1.5,
    ];
    let percent_rows: Vec<Json> = percents
        .iter()
        .map(|share| {
            object(vec![
                ("share", float(*share)),
                ("en", text(crate::format::percent(*share, Language::En))),
                ("de", text(crate::format::percent(*share, Language::De))),
                ("fr", text(crate::format::percent(*share, Language::Fr))),
            ])
        })
        .collect();

    let eur = |minor: i64| Money {
        minor,
        currency: Currency::EUR,
    };
    let share_sets: Vec<Vec<Money>> = vec![
        Vec::new(),
        vec![eur(0)],
        vec![eur(1), eur(1), eur(1)],
        vec![eur(3_000), eur(7_000)],
        (1..=10).map(eur).collect(),
        (1..=10).rev().map(eur).collect(),
        vec![eur(-100), eur(200), eur(-50)],
        vec![eur(0), eur(0), eur(0)],
    ];
    let share_rows: Vec<Json> = share_sets
        .iter()
        .map(|set| {
            object(vec![
                ("amounts", array(set.iter().map(|value| number(value.minor)).collect())),
                (
                    "shares",
                    array(crate::format::shares(set).into_iter().map(number).collect()),
                ),
            ])
        })
        .collect();

    let dates = [
        time::macros::date!(2026 - 01 - 01),
        time::macros::date!(2026 - 10 - 15),
        time::macros::date!(2024 - 02 - 29),
        time::macros::date!(1999 - 12 - 31),
        time::macros::date!(2100 - 12 - 31),
    ];
    let date_rows: Vec<Json> = dates
        .iter()
        .map(|value| {
            object(vec![
                ("date", date_json(*value)),
                ("formatted", text(crate::format::date(*value))),
            ])
        })
        .collect();

    let date_times = [
        datetime!(2026 - 01 - 01 00:00 UTC),
        datetime!(2026 - 10 - 15 13:07 UTC),
        datetime!(2024 - 02 - 29 23:59 UTC),
        datetime!(1999 - 12 - 31 06:05 UTC),
    ];
    let date_time_rows: Vec<Json> = date_times
        .iter()
        .map(|value| {
            object(vec![
                ("date", date_json(value.date())),
                ("hour", number(i64::from(value.hour()))),
                ("minute", number(i64::from(value.minute()))),
                ("formatted", text(crate::format::date_time(*value))),
            ])
        })
        .collect();

    let iban_rows: Vec<Json> = [
        "DE89370400440532013000",
        "DE89 3704 0044 0532 0130 00",
        "de89370400440532013000",
        "NO9386011117947",
        "CARD4321",
        "",
    ]
    .iter()
    .map(|value| {
        object(vec![
            ("input", text(*value)),
            ("formatted", text(crate::format::iban(value))),
        ])
    })
    .collect();

    let parse_dates = [
        "5.11.2026",
        " 05.11.2026 ",
        "29.02.2028",
        "28.02.2026",
        "31.02.2026",
        "29.02.2027",
        "32.01.2026",
        "05.11.26",
        "2026-11-05",
        "",
        "5.11",
        "5.11.20260",
    ];
    let parse_date_rows: Vec<Json> = parse_dates
        .iter()
        .map(|value| {
            object(vec![
                ("input", text(*value)),
                (
                    "parsed",
                    crate::format::parse_date(value).map_or(Json::Null, |date| text(iso_date(date))),
                ),
            ])
        })
        .collect();

    let parse_decimals = [
        "1.234,56",
        "1,234.56",
        "-12.34",
        "99999999999999.99",
        "1E15",
        "1000000000000000",
        "1E999999999",
        "-1E999999999",
        "1E-999999999",
        "0.12345678901",
        "12345678901234567890123456789012345678901234567890123456789012345",
        "abc",
        "",
        " 12.5 ",
    ];
    let parse_decimal_rows: Vec<Json> = parse_decimals
        .iter()
        .map(|value| {
            object(vec![
                ("input", text(*value)),
                ("parsed", optional_decimal(crate::format::parse_decimal(value))),
            ])
        })
        .collect();

    let parse_amounts = [
        "1.234,56",
        "1,234.56",
        "-12,5",
        "12 €",
        "2.450,37 €",
        "2.450,37\u{a0}€",
        "€2.450,37",
        "abc",
        "",
        "1E999999999",
        "0.12345678901",
        "99999999999999.99",
    ];
    let parse_amount_rows: Vec<Json> = parse_amounts
        .iter()
        .map(|value| {
            object(vec![
                ("input", text(*value)),
                ("parsed", optional_decimal(crate::format::parse_amount(value))),
            ])
        })
        .collect();

    let money_inputs = [
        "12.345", "1.234,56", "1,234.56", "-12.34", "+5", "0", "", "abc", "1.2.3", "0.001", "500",
    ];
    let mut money_decimal_rows = Vec::new();
    let mut format_plain_rows = Vec::new();
    for currency in ["EUR", "JPY", "KWD", "USD"] {
        let code = Currency::new(currency).expect("a valid test currency");
        for input in money_inputs {
            let parsed = crate::money::parse_decimal(input, code);
            let ok = parsed.is_ok();
            let minor = parsed.as_ref().ok().map_or(Json::Null, |money| number(money.minor));
            let plain = parsed
                .as_ref()
                .ok()
                .map_or(Json::Null, |money| text(money::format_plain(*money)));
            let error = parsed
                .as_ref()
                .err()
                .map_or(Json::Null, |error| text(error.to_string()));
            money_decimal_rows.push(object(vec![
                ("currency", text(code.as_str())),
                ("input", text(input)),
                ("ok", boolean(ok)),
                ("minor", minor),
                ("plain", plain),
                ("error", error),
            ]));
        }
        for minor in [0, 1, -1, 5, 100, -12_345, 123_450, 12_345_678] {
            let value = Money { minor, currency: code };
            format_plain_rows.push(object(vec![
                ("minor", number(minor)),
                ("currency", text(code.as_str())),
                ("plain", text(money::format_plain(value))),
            ]));
        }
    }

    object(vec![
        ("money", array(money_rows)),
        ("whole_money", array(whole_rows)),
        ("percent", array(percent_rows)),
        ("shares", array(share_rows)),
        ("date", array(date_rows)),
        ("date_time", array(date_time_rows)),
        ("iban", array(iban_rows)),
        ("parse_date", array(parse_date_rows)),
        ("parse_decimal", array(parse_decimal_rows)),
        ("parse_amount", array(parse_amount_rows)),
        ("money_parse_decimal", array(money_decimal_rows)),
        ("format_plain", array(format_plain_rows)),
    ])
}

// ---------------------------------------------------------------------------------------------------
// iban/
// ---------------------------------------------------------------------------------------------------

fn iban_json() -> Json {
    let content = String::from_utf8(read_input("iban", "ibans.txt")).expect("the IBAN list is UTF-8");
    array(
        content
            .lines()
            .map(|value| {
                object(vec![
                    ("input", text(value)),
                    ("normalize", text(crate::iban::normalize(value))),
                    ("is_valid", boolean(crate::iban::is_valid(value))),
                    ("format", text(crate::iban::format(value))),
                    ("mask", text(crate::iban::mask(value))),
                ])
            })
            .collect(),
    )
}

// ---------------------------------------------------------------------------------------------------
// rules/
// ---------------------------------------------------------------------------------------------------

fn rule_row_json(row: &RuleFileRow) -> Json {
    object(vec![
        ("line", number(row.line as i64)),
        ("category", text(row.category.clone())),
        ("pattern", text(row.pattern.clone())),
        ("field", text(row.field.as_str())),
        ("direction", text(row.direction.as_str())),
        ("priority", number(row.priority)),
        ("auto_apply", boolean(row.auto_apply)),
    ])
}

fn parsed_rule_file_json(
    file: &str,
    parsed: &Result<crate::rules::file::ParsedRuleFile, crate::rules::file::RuleFileParseError>,
) -> Json {
    match parsed {
        Ok(parsed) => {
            let items: Vec<crate::rules::file::RuleFileItem> = parsed
                .rows
                .iter()
                .map(|row| crate::rules::file::RuleFileItem {
                    pattern: row.pattern.clone(),
                    category_name: row.category.clone(),
                    priority: row.priority,
                    auto_apply: row.auto_apply,
                    match_field: row.field,
                    direction: row.direction,
                })
                .collect();
            let (rewritten, skipped) = RuleFile::write(&items);
            object(vec![
                ("file", text(file)),
                ("ok", boolean(true)),
                ("rows", array(parsed.rows.iter().map(rule_row_json).collect())),
                (
                    "errors",
                    array(
                        parsed
                            .errors
                            .iter()
                            .map(|error| {
                                object(vec![
                                    ("line", number(error.line as i64)),
                                    ("reason", text(error.reason.clone())),
                                ])
                            })
                            .collect(),
                    ),
                ),
                (
                    "rewritten",
                    object(vec![("text", text(rewritten)), ("skipped", number(skipped as i64))]),
                ),
            ])
        }
        Err(error) => object(vec![
            ("file", text(file)),
            ("ok", boolean(false)),
            ("error", text(error.message.clone())),
        ]),
    }
}

fn default_specs() -> (Vec<RuleSpec>, BTreeMap<CategoryId, String>) {
    let bytes = read_input("rules", "default_rules.tsv");
    let parsed = RuleFile::parse(&bytes, Language::En).expect("the default rules file parses");
    let mut names = BTreeMap::new();
    let specs = parsed
        .rows
        .iter()
        .enumerate()
        .map(|(index, row)| {
            let category_id = CategoryId(index as i64 + 1);
            names.insert(category_id, row.category.clone());
            RuleSpec {
                pattern: row.pattern.clone(),
                category_id,
                field: row.field,
                direction: row.direction,
            }
        })
        .collect();
    (specs, names)
}

fn shop_cases() -> Vec<Json> {
    const PAYPAL_PARTNER: &str = "PayPal Europe S.a.r.l. et Cie S.C.A";
    let cases: Vec<(&str, String, Option<&str>, Vec<RuleSpec>)> = vec![
        (
            "a partner rule for the shop beats the paypal rule whatever the rule order",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![
                RuleSpec {
                    pattern: "PayPal".to_owned(),
                    category_id: CategoryId(2),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
                RuleSpec {
                    pattern: "Beispiel Shop".to_owned(),
                    category_id: CategoryId(1),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
            ],
        ),
        (
            "with no shop rule the paypal rule applies through the original view",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![RuleSpec {
                pattern: "PayPal".to_owned(),
                category_id: CategoryId(2),
                field: RuleField::Partner,
                direction: RuleDirection::Any,
            }],
        ),
        (
            "a purpose only rule keeps winning for a paypal booking",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![
                RuleSpec {
                    pattern: "Ihr Einkauf".to_owned(),
                    category_id: CategoryId(3),
                    field: RuleField::Purpose,
                    direction: RuleDirection::Any,
                },
                RuleSpec {
                    pattern: "Beispiel".to_owned(),
                    category_id: CategoryId(1),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
            ],
        ),
        (
            "an income only rule does not match an expense via the shop view",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![RuleSpec {
                pattern: "Beispiel".to_owned(),
                category_id: CategoryId(4),
                field: RuleField::Partner,
                direction: RuleDirection::Income,
            }],
        ),
        (
            "a shop rule applies when no other rule matches",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![RuleSpec {
                pattern: "Beispiel Shop".to_owned(),
                category_id: CategoryId(20),
                field: RuleField::Partner,
                direction: RuleDirection::Any,
            }],
        ),
        (
            "a shop rule beats the paypal rule for a card booking with a paypal partner",
            "PAYPAL *BEISPIELSHOP".to_owned(),
            Some("PAYPAL *BEISPIELSHOP"),
            vec![
                RuleSpec {
                    pattern: "PayPal".to_owned(),
                    category_id: CategoryId(10),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
                RuleSpec {
                    pattern: "BEISPIELSHOP".to_owned(),
                    category_id: CategoryId(20),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
            ],
        ),
        (
            "a non paypal booking is matched exactly as before",
            "Kauf bei Beispiel".to_owned(),
            Some("Beispiel Shop GmbH"),
            vec![RuleSpec {
                pattern: "Beispiel".to_owned(),
                category_id: CategoryId(5),
                field: RuleField::Partner,
                direction: RuleDirection::Any,
            }],
        ),
        (
            "a refund purpose rule keeps winning over a shop rule",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Rückerstattung, Ihr Einkauf bei Beispiel Shop GmbH"
                .to_owned(),
            Some(PAYPAL_PARTNER),
            vec![
                RuleSpec {
                    pattern: "Rückerstattung|Storno".to_owned(),
                    category_id: CategoryId(10),
                    field: RuleField::Purpose,
                    direction: RuleDirection::Any,
                },
                RuleSpec {
                    pattern: "Beispiel Shop".to_owned(),
                    category_id: CategoryId(20),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
            ],
        ),
        (
            "a shop rule only displaces a provider rule of the same or any direction",
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH".to_owned(),
            Some(PAYPAL_PARTNER),
            vec![
                RuleSpec {
                    pattern: "PayPal".to_owned(),
                    category_id: CategoryId(10),
                    field: RuleField::Partner,
                    direction: RuleDirection::Income,
                },
                RuleSpec {
                    pattern: "Beispiel Shop".to_owned(),
                    category_id: CategoryId(20),
                    field: RuleField::Partner,
                    direction: RuleDirection::Income,
                },
            ],
        ),
        (
            "card format rules behave like the marker format",
            "PAYPAL *BEISPIELSHOP 12345678 RETURN".to_owned(),
            Some("PAYPAL *BEISPIELSHOP"),
            vec![
                RuleSpec {
                    pattern: "RETURN".to_owned(),
                    category_id: CategoryId(10),
                    field: RuleField::Purpose,
                    direction: RuleDirection::Any,
                },
                RuleSpec {
                    pattern: "BEISPIELSHOP".to_owned(),
                    category_id: CategoryId(20),
                    field: RuleField::Partner,
                    direction: RuleDirection::Any,
                },
            ],
        ),
    ];

    cases
        .into_iter()
        .map(|(name, purpose, partner, specs)| {
            let patterns: Vec<_> = specs.iter().map(|spec| compile(&spec.pattern)).collect();
            let shop_match = PaymentProviders::shop_match(Some(&purpose), partner);
            let amount = Decimal::new(-1000, 2);
            let index = RuleMatcher::select(
                &specs,
                &patterns,
                Some(&purpose),
                partner,
                Some(amount),
                shop_match.as_ref(),
            );
            object(vec![
                ("name", text(name)),
                ("purpose", text(purpose)),
                ("partner", optional(partner)),
                (
                    "rules",
                    array(
                        specs
                            .iter()
                            .map(|spec| {
                                object(vec![
                                    ("pattern", text(spec.pattern.clone())),
                                    ("field", text(spec.field.as_str())),
                                    ("direction", text(spec.direction.as_str())),
                                    ("category_id", number(spec.category_id.0)),
                                ])
                            })
                            .collect(),
                    ),
                ),
                ("selected_index", index.map_or(Json::Null, |value| number(value as i64))),
                (
                    "selected_category_id",
                    index.map_or(Json::Null, |value| number(specs[value].category_id.0)),
                ),
            ])
        })
        .collect()
}

fn payment_provider_cases() -> Vec<Json> {
    const PAYPAL_PARTNER: &str = "PayPal Europe S.a.r.l. et Cie S.C.A";
    let cases = [
        (
            "marker with sepa tags and padding",
            "EREF+REF001 SVWZ+PP.0471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
            Some(PAYPAL_PARTNER),
        ),
        (
            "marker without the pre-comma name",
            "Ihr Einkauf bei Beispiel Shop GmbH",
            Some(PAYPAL_PARTNER),
        ),
        (
            "pre-comma name without the marker",
            "PP.1234.PP . Beispiel Abo GmbH, Ihr Abo",
            Some(PAYPAL_PARTNER),
        ),
        ("card descriptor", "PAYPAL *BEISPIELSHOP 12345678 RETURN", None),
        (
            "card descriptor with a paypal partner",
            "PAYPAL *BEISPIELSHOP 12345678 RETURN",
            Some(PAYPAL_PARTNER),
        ),
        (
            "top-up note",
            "Ihr Einkauf bei PayPal ABBUCHUNG VOM GIROKONTO",
            Some(PAYPAL_PARTNER),
        ),
        ("no shop", "Kartenzahlung", Some(PAYPAL_PARTNER)),
        (
            "non paypal partner",
            "Ihr Einkauf bei Beispiel Shop GmbH",
            Some("Beispiel Shop GmbH"),
        ),
        ("plain purpose", "Wocheneinkauf", Some("REWE Markt GmbH")),
        ("empty", "", None),
        ("only a code tail", "PAYPAL *SHOP, AB12CD34", None),
        ("marker words only", "Ihr Einkauf bei Transfer", Some(PAYPAL_PARTNER)),
    ];

    cases
        .iter()
        .map(|(name, purpose, partner)| {
            let matched = PaymentProviders::shop_match(Some(purpose), *partner);
            object(vec![
                ("name", text(*name)),
                ("purpose", text(*purpose)),
                ("partner", optional(*partner)),
                (
                    "shop_match",
                    matched.as_ref().map_or(Json::Null, |matched| {
                        object(vec![
                            ("shop", text(matched.shop.clone())),
                            ("provider_purpose", optional(matched.provider_purpose.clone())),
                            ("provider_partner", optional(matched.provider_partner.clone())),
                        ])
                    }),
                ),
                ("title", optional(PaymentProviders::title_of(Some(purpose), *partner))),
                (
                    "merchant",
                    optional(PaymentProviders::merchant_of(Some(purpose), *partner)),
                ),
            ])
        })
        .collect()
}

fn rules_matcher_json() -> Json {
    let (specs, names) = default_specs();
    let patterns: Vec<_> = specs.iter().map(|spec| compile(&spec.pattern)).collect();
    let matcher = RuleMatcher::new(specs.clone());

    let bytes = read_input("rules", "bookings.csv");
    let statement = import::read("bookings.csv", &bytes, None, today()).expect("the rule bookings parse");

    let bookings: Vec<Json> = statement
        .bookings
        .iter()
        .enumerate()
        .map(|(index, booking)| {
            let purpose = booking.purpose.as_deref();
            let partner = booking.partner_name.as_deref();
            let amount = Some(booking.amount);
            let shop_match = PaymentProviders::shop_match(purpose, partner);
            let selected = RuleMatcher::select(&specs, &patterns, purpose, partner, amount, shop_match.as_ref());
            let category = matcher.category_for(purpose, partner, amount);
            object(vec![
                ("index", number(index as i64)),
                ("date", date_json(booking.date)),
                ("amount", text(decimal_text(booking.amount))),
                ("partner_name", optional(partner)),
                ("purpose", optional(purpose)),
                (
                    "shop",
                    shop_match
                        .as_ref()
                        .map_or(Json::Null, |matched| text(matched.shop.clone())),
                ),
                (
                    "selected_index",
                    selected.map_or(Json::Null, |value| number(value as i64)),
                ),
                (
                    "selected_pattern",
                    selected.map_or(Json::Null, |value| text(specs[value].pattern.clone())),
                ),
                ("category_id", category.map_or(Json::Null, |value| number(value.0))),
                (
                    "category",
                    category.map_or(Json::Null, |value| text(names.get(&value).cloned().unwrap_or_default())),
                ),
            ])
        })
        .collect();

    let default_rules =
        RuleFile::parse(&read_input("rules", "default_rules.tsv"), Language::En).expect("the default rules parse");
    object(vec![
        (
            "default_rules",
            array(
                default_rules
                    .rows
                    .iter()
                    .enumerate()
                    .map(|(index, row)| {
                        object(vec![
                            ("index", number(index as i64)),
                            ("category_id", number(index as i64 + 1)),
                            ("category", text(row.category.clone())),
                            ("pattern", text(row.pattern.clone())),
                            ("field", text(row.field.as_str())),
                            ("direction", text(row.direction.as_str())),
                            ("priority", number(row.priority)),
                        ])
                    })
                    .collect(),
            ),
        ),
        ("bookings", array(bookings)),
        ("shop_cases", array(shop_cases())),
    ])
}

fn rules_json() -> Json {
    let files: Vec<Json> = input_files("rules")
        .into_iter()
        .filter(|name| name.ends_with(".tsv"))
        .map(|name| {
            let bytes = read_input("rules", &name);
            parsed_rule_file_json(&name, &RuleFile::parse(&bytes, Language::En))
        })
        .collect();
    object(vec![("files", array(files))])
}

// ---------------------------------------------------------------------------------------------------
// bank_text/
// ---------------------------------------------------------------------------------------------------

fn bank_text_json(imports: &[ImportFile]) -> Json {
    let mut rows = Vec::new();
    let mut next_id = 1_i64;
    for file in imports {
        let Ok(statement) = &file.statement else {
            continue;
        };
        for (index, booking) in statement.bookings.iter().enumerate() {
            let item = item_from_booking(next_id, booking, statement.account_iban.as_deref());
            next_id += 1;
            rows.push(object(vec![
                ("file", text(file.name.clone())),
                ("index", number(index as i64)),
                ("partner_name", optional(booking.partner_name.clone())),
                ("purpose", optional(booking.purpose.clone())),
                (
                    "display_partner",
                    optional(crate::bank_text::display_partner(item.partner_name.as_deref())),
                ),
                (
                    "display_purpose",
                    optional(crate::bank_text::display_purpose(item.purpose.as_deref())),
                ),
                (
                    "display_description",
                    text(crate::bank_text::display_description(&item)),
                ),
                ("secondary_line", optional(crate::bank_text::secondary_line(&item))),
            ]));
        }
    }
    array(rows)
}

// ---------------------------------------------------------------------------------------------------
// balance/
// ---------------------------------------------------------------------------------------------------

fn balance_json(imports: &[ImportFile]) -> Json {
    let files = [
        "balance_checking_2026.csv",
        "balance_savings_2026.csv",
        "balance_card_2026.csv",
    ];
    let mut results = Vec::new();
    for name in files {
        let file = imports
            .iter()
            .find(|file| file.name == name)
            .unwrap_or_else(|| panic!("{name} is imported"));
        let statement = file.statement.as_ref().expect("the balance statement parses");
        let mut items: Vec<TransactionItem> = statement
            .bookings
            .iter()
            .enumerate()
            .map(|(index, booking)| item_from_booking(index as i64 + 1, booking, statement.account_iban.as_deref()))
            .collect();
        items.sort_by_key(|item| (item.date, item.id));

        let current_minor = items
            .iter()
            .rev()
            .find_map(|item| item.balance_after.map(|balance| balance.minor))
            .unwrap_or_else(|| items.iter().map(|item| item.amount.minor).sum());
        let current = Money {
            minor: current_minor,
            currency: Currency::EUR,
        };
        let newest_first: Vec<TransactionItem> = items.iter().rev().cloned().collect();
        let running = RunningBalance::compute(current, &newest_first);
        let from = items.first().expect("bookings").date;
        let to = items.last().expect("bookings").date;

        let entries: Vec<Json> = newest_first
            .iter()
            .filter_map(|item| {
                let entry = running.get(&item.id)?;
                Some(object(vec![
                    ("id", number(item.id.0)),
                    ("date", date_json(item.date)),
                    ("amount", money_json(item.amount)),
                    ("balance", money_json(entry.balance)),
                    ("bank_difference", optional_money(entry.bank_difference)),
                ]))
            })
            .collect();

        let history: Vec<Json> = BalanceHistory::from_running(&newest_first, &running, from, to)
            .into_iter()
            .map(|(date, balance)| object(vec![("date", date_json(date)), ("balance", money_json(balance))]))
            .collect();
        let computed: Vec<Json> = BalanceHistory::compute(current, &newest_first, from, to)
            .into_iter()
            .map(|(date, balance)| object(vec![("date", date_json(date)), ("balance", money_json(balance))]))
            .collect();

        results.push(object(vec![
            ("file", text(name)),
            ("current_balance", money_json(current)),
            ("from", date_json(from)),
            ("to", date_json(to)),
            ("running", array(entries)),
            ("history", array(history)),
            ("history_compute", array(computed)),
        ]));
    }
    array(results)
}

// ---------------------------------------------------------------------------------------------------
// recurring/
// ---------------------------------------------------------------------------------------------------

fn recurring_json() -> Json {
    let files = ["monthly.csv", "quarterly.csv", "irregular.csv"];
    let mut results = Vec::new();
    for name in files {
        let bytes = read_input("recurring", name);
        let statement = import::read(name, &bytes, None, today()).expect("the recurring statement parses");
        let items: Vec<TransactionItem> = statement
            .bookings
            .iter()
            .enumerate()
            .map(|(index, booking)| {
                let mut item = item_from_booking(index as i64 + 1, booking, None);
                let category = recurring_category(booking.partner_name.as_deref());
                if let Some(category) = category {
                    item.category_id = Some(CategoryId(index as i64 + 1));
                    item.category_name = Some(category.to_owned());
                }
                item
            })
            .collect();
        let detected = RecurringDetector::detect(&items, today());
        let upcoming = RecurringDetector::upcoming(&detected, today(), 30);
        results.push(object(vec![
            ("file", text(name)),
            ("today", date_json(today())),
            ("detected", array(detected.iter().map(recurring_payment_json).collect())),
            (
                "upcoming_30_days",
                array(upcoming.iter().map(recurring_payment_json).collect()),
            ),
        ]));
    }
    array(results)
}

fn recurring_category(partner: Option<&str>) -> Option<&'static str> {
    let partner = partner?;
    if partner.contains("NETFLIX") || partner.contains("Spotify") {
        Some("Phone, internet & media")
    } else if partner.contains("Employer") {
        Some("Salary")
    } else if partner.contains("Stadtwerke") {
        Some("Energy")
    } else if partner.contains("HUK") {
        Some("Insurance")
    } else if partner.contains("ADAC") {
        Some("Leisure & clubs")
    } else if partner.contains("REWE") {
        Some("Groceries")
    } else {
        None
    }
}

fn recurring_payment_json(payment: &RecurringPayment) -> Json {
    object(vec![
        ("name", text(payment.name.clone())),
        ("cadence", text(format!("{:?}", payment.cadence))),
        ("typical_amount", money_json(payment.typical_amount)),
        ("fixed_amount", boolean(payment.fixed_amount)),
        ("last_date", date_json(payment.last_date)),
        ("next_date", date_json(payment.next_date)),
        ("occurrences", number(payment.occurrences as i64)),
        ("category_name", optional(payment.category_name.clone())),
        ("currency", text(payment.currency.as_str())),
        ("is_transfer", boolean(payment.is_transfer)),
        ("is_income", boolean(payment.is_income())),
        ("yearly_amount", money_json(payment.yearly_amount())),
    ])
}

// ---------------------------------------------------------------------------------------------------
// receipt/
// ---------------------------------------------------------------------------------------------------

fn receipt_json() -> Json {
    array(
        input_files("receipt")
            .into_iter()
            .filter(|name| name.ends_with(".txt"))
            .map(|name| {
                let content = String::from_utf8(read_input("receipt", &name)).expect("the receipt is UTF-8");
                let fields = receipt::parse(&content, Currency::EUR, receipt_today());
                object(vec![
                    ("file", text(name)),
                    ("total", optional_money(fields.total)),
                    ("date", optional_date(fields.date)),
                    (
                        "amounts",
                        array(fields.amounts.iter().map(|amount| money_json(*amount)).collect()),
                    ),
                ])
            })
            .collect(),
    )
}

// ---------------------------------------------------------------------------------------------------
// dashboard/, report/ and export/ share one throwaway encrypted database.
// ---------------------------------------------------------------------------------------------------

struct DashboardFixture {
    _temp: tempfile::TempDir,
    db: Database,
}

fn fixture_passphrase() -> Passphrase {
    Passphrase::new(FIXTURE_PASSPHRASE, PassphrasePurpose::New).expect("the fixture passphrase is valid")
}

fn category_by_name(db: &Database, name: &str) -> CategoryId {
    categories::list(db)
        .expect("categories list")
        .into_iter()
        .find(|category| category.name == name)
        .unwrap_or_else(|| panic!("the category {name:?} exists"))
        .id
}

fn dashboard_fixture() -> DashboardFixture {
    let temp = tempfile::tempdir().expect("a temp directory");
    let db = Database::create(&temp.path().join("finglass.db"), &fixture_passphrase()).expect("the fixture database");

    let checking = accounts::create(
        &db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Checking".to_owned()),
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
    .expect("the checking account");
    let savings = accounts::create(
        &db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Savings".to_owned()),
            iban: Some("DE44500105175407324931".to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Savings,
            balance: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .expect("the savings account");
    let card = accounts::create(
        &db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Credit card".to_owned()),
            iban: None,
            account_number: Some("5412345678901234".to_owned()),
            currency: Currency::EUR,
            account_type: AccountType::CreditCard,
            balance: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .expect("the card account");

    categories::seed_defaults(&db, Language::En).expect("the default categories");

    for (name, minor) in [("Groceries", 40_000_i64), ("Restaurants & cafés", 15_000)] {
        let id = category_by_name(&db, name);
        let mut category = categories::get(&db, id)
            .expect("category get")
            .expect("the category exists");
        category.budget_limit = Some(Money {
            minor,
            currency: Currency::EUR,
        });
        categories::update(
            &db,
            id,
            NewCategory {
                name: category.name,
                color: category.color,
                budget_limit: category.budget_limit,
                is_tax_relevant: category.is_tax_relevant,
                is_transfer: category.is_transfer,
            },
        )
        .expect("the budget");
    }
    for name in ["Health", "Taxes & fees"] {
        let id = category_by_name(&db, name);
        let category = categories::get(&db, id)
            .expect("category get")
            .expect("the category exists");
        categories::update(
            &db,
            id,
            NewCategory {
                name: category.name,
                color: category.color,
                budget_limit: category.budget_limit,
                is_tax_relevant: true,
                is_transfer: category.is_transfer,
            },
        )
        .expect("the tax flag");
    }

    let parsed_rules =
        RuleFile::parse(&read_input("rules", "default_rules.tsv"), Language::En).expect("the default rules parse");
    for row in &parsed_rules.rows {
        rules_repo::create(
            &db,
            NewRule {
                pattern: row.pattern.clone(),
                category_id: category_by_name(&db, &row.category),
                priority: row.priority,
                auto_apply: row.auto_apply,
                match_field: row.field,
                direction: row.direction,
                tax_relevant: None,
            },
        )
        .expect("the default rule");
    }

    for (file, account) in [
        ("dashboard_checking.csv", checking),
        ("dashboard_savings.csv", savings),
        ("dashboard_card.csv", card),
    ] {
        let bytes = read_input("import", file);
        let statement = import::read(file, &bytes, None, today()).expect("the dashboard statement parses");
        import::import(&db, account, &statement, true, today()).expect("the dashboard statement imports");
    }

    DashboardFixture { _temp: temp, db }
}

fn dashboard_json(fixture: &DashboardFixture) -> Json {
    let months = [(2026, 10), (2026, 9), (2026, 7), (2026, 1)];
    array(
        months
            .iter()
            .map(|(year, month)| {
                let figures = crate::dashboard::load_dashboard(&fixture.db, today(), YearMonth::new(*year, *month))
                    .expect("the dashboard loads");
                object(vec![
                    ("requested_month", year_month_json(YearMonth::new(*year, *month))),
                    ("figures", dashboard_figures_json(&figures)),
                ])
            })
            .collect(),
    )
}

fn dashboard_figures_json(figures: &DashboardFigures) -> Json {
    object(vec![
        ("currency", text(figures.currency.as_str())),
        ("total_balance", money_json(figures.total_balance)),
        ("available_balance", money_json(figures.available_balance)),
        ("loan_balance", money_json(figures.loan_balance)),
        ("carry_forward", optional_money(figures.carry_forward)),
        ("other_movements", optional_money(figures.other_movements)),
        ("balance_date", optional_date(figures.balance_date)),
        ("has_loans", boolean(figures.has_loans)),
        (
            "other_balances",
            array(
                figures
                    .other_balances
                    .iter()
                    .map(|(currency, amount)| {
                        object(vec![
                            ("currency", text(currency.as_str())),
                            ("amount", money_json(*amount)),
                        ])
                    })
                    .collect(),
            ),
        ),
        ("monthly_income", money_json(figures.monthly_income)),
        ("monthly_expenses", money_json(figures.monthly_expenses)),
        ("uncategorized_count", number(figures.uncategorized_count as i64)),
        (
            "recent_transactions",
            array(figures.recent_transactions.iter().map(item_json).collect()),
        ),
        (
            "expenses_by_category",
            array(figures.expenses_by_category.iter().map(chart_slice_json).collect()),
        ),
        (
            "tax_deductions_by_category",
            array(
                figures
                    .tax_deductions_by_category
                    .iter()
                    .map(chart_slice_json)
                    .collect(),
            ),
        ),
        (
            "last_months",
            array(figures.last_months.iter().map(month_bar_json).collect()),
        ),
        ("budgets", array(figures.budgets.iter().map(budget_json).collect())),
        (
            "upcoming",
            array(figures.upcoming.iter().map(recurring_payment_json).collect()),
        ),
        ("month_progress", float(figures.month_progress)),
        ("transfers_this_month", money_json(figures.transfers_this_month)),
        ("selected_month", year_month_json(figures.selected_month)),
        ("loaded_month", year_month_json(figures.loaded_month)),
        ("earliest_month", year_month_json(figures.earliest_month)),
        ("current_month", year_month_json(figures.current_month)),
        ("has_accounts", boolean(figures.has_accounts)),
    ])
}

fn chart_slice_json(slice: &crate::dashboard::ChartSlice) -> Json {
    object(vec![
        ("label", text(slice.label.clone())),
        ("amount", money_json(slice.amount)),
        ("color", optional(slice.color.clone())),
        ("category_id", slice.category_id.map_or(Json::Null, |id| number(id.0))),
    ])
}

fn month_bar_json(bar: &crate::dashboard::MonthBar) -> Json {
    object(vec![
        ("year", number(i64::from(bar.year))),
        ("month", number(i64::from(bar.month))),
        ("income", money_json(bar.income)),
        ("expenses", money_json(bar.expenses)),
        ("partial", boolean(bar.partial)),
    ])
}

fn budget_json(budget: &crate::dashboard::BudgetStatus) -> Json {
    object(vec![
        ("category_id", number(budget.category_id.0)),
        ("category_name", text(budget.category_name.clone())),
        ("color", text(budget.color.clone())),
        ("limit", money_json(budget.limit)),
        ("spent", money_json(budget.spent)),
        ("ratio", float(budget.ratio())),
        ("remaining", money_json(budget.remaining())),
        ("is_over", boolean(budget.is_over())),
    ])
}

fn all_items(db: &Database) -> Vec<TransactionItem> {
    transactions::find_items(db, &crate::model::TransactionFilter::default(), None).expect("all transactions")
}

fn report_json(fixture: &DashboardFixture) -> Json {
    let items = all_items(&fixture.db);
    let years = [2025_i32, 2026];
    let mut reports = Vec::new();
    let mut tax_reports = Vec::new();
    let mut tax_bookings = Vec::new();
    let mut monthly = Vec::new();

    for year in years {
        let from = Date::from_calendar_date(year, Month::January, 1).expect("a valid date");
        let to = Date::from_calendar_date(year, Month::December, 31).expect("a valid date");
        let year_items: Vec<TransactionItem> = items.iter().filter(|item| item.date.year() == year).cloned().collect();
        let report = report_service::build_report(from, to, &year_items, Currency::EUR, Language::En);
        reports.push(object(vec![
            ("year", number(i64::from(year))),
            ("report", tax_report_json(&report)),
        ]));

        let tax_report = report_service::build_tax_report(year, &year_items, Currency::EUR, Language::En);
        tax_reports.push(object(vec![
            ("year", number(i64::from(year))),
            ("report", tax_report_json(&tax_report)),
        ]));

        let bookings = report_service::build_tax_bookings(from, to, &year_items, Currency::EUR, Language::En);
        tax_bookings.push(object(vec![
            ("year", number(i64::from(year))),
            ("bookings", tax_bookings_json(&bookings)),
        ]));
    }

    for (year, month) in [(2025_i32, 12_u8), (2026, 10)] {
        let month_items: Vec<TransactionItem> = items
            .iter()
            .filter(|item| item.date.year() == year && u8::from(item.date.month()) == month)
            .cloned()
            .collect();
        let report = report_service::monthly_report(year, month, &month_items, Currency::EUR, Language::En);
        monthly.push(object(vec![
            ("year", number(i64::from(year))),
            ("month", number(i64::from(month))),
            ("report", monthly_report_json(&report)),
        ]));
    }

    let spending: Vec<TransactionItem> = items.iter().filter(|item| item.counts_as_spending()).cloned().collect();
    let mut categories_en = Json::Null;
    let mut categories_de = Json::Null;
    let mut shares_en = Json::Null;
    let mut shares_de = Json::Null;
    for language in [Language::En, Language::De] {
        let categories = array(
            report_service::sum_by_category(&spending, language)
                .into_iter()
                .map(|(name, amount)| object(vec![("name", text(name)), ("amount", money_json(amount))]))
                .collect(),
        );
        let shares = array(
            report_service::category_shares(&spending, language)
                .iter()
                .map(category_share_json)
                .collect(),
        );
        if language == Language::En {
            categories_en = categories;
            shares_en = shares;
        } else {
            categories_de = categories;
            shares_de = shares;
        }
    }

    object(vec![
        ("reports", array(reports)),
        ("tax_reports", array(tax_reports)),
        ("tax_bookings", array(tax_bookings)),
        ("monthly_reports", array(monthly)),
        (
            "sum_by_category",
            object(vec![("en", categories_en), ("de", categories_de)]),
        ),
        ("category_shares", object(vec![("en", shares_en), ("de", shares_de)])),
    ])
}

fn tax_report_json(report: &TaxReport) -> Json {
    object(vec![
        ("year", number(i64::from(report.year))),
        ("from", date_json(report.from)),
        ("to", date_json(report.to)),
        ("total_income", money_json(report.total_income)),
        ("total_expenses", money_json(report.total_expenses)),
        ("net_income", money_json(report.net_income)),
        (
            "expenses_by_category",
            array(
                report
                    .expenses_by_category
                    .iter()
                    .map(|(name, amount)| object(vec![("name", text(name.clone())), ("amount", money_json(*amount))]))
                    .collect(),
            ),
        ),
        (
            "tax_relevant_expenses",
            array(
                report
                    .tax_relevant_expenses
                    .iter()
                    .map(|(name, amount)| object(vec![("name", text(name.clone())), ("amount", money_json(*amount))]))
                    .collect(),
            ),
        ),
        (
            "tax_relevant_transactions",
            array(report.tax_relevant_transactions.iter().map(item_json).collect()),
        ),
        (
            "monthly_breakdown",
            array(report.monthly_breakdown.iter().map(monthly_data_json).collect()),
        ),
        (
            "top_vendors",
            array(
                report
                    .top_vendors
                    .iter()
                    .map(|(name, amount)| object(vec![("name", text(name.clone())), ("amount", money_json(*amount))]))
                    .collect(),
            ),
        ),
        ("transaction_count", number(report.transaction_count as i64)),
        ("categories_used", number(report.categories_used as i64)),
        ("currency", text(report.currency.as_str())),
        ("other_currency_bookings", number(report.other_currency_bookings as i64)),
        ("transfer_total", money_json(report.transfer_total)),
        (
            "category_shares",
            array(report.category_shares.iter().map(category_share_json).collect()),
        ),
        ("tax_relevant_total", money_json(report.tax_relevant_total())),
        ("is_calendar_year", boolean(report.is_calendar_year())),
        ("title", text(report.title(Language::En))),
    ])
}

fn monthly_data_json(month: &MonthlyData) -> Json {
    object(vec![
        ("year", number(i64::from(month.year))),
        ("month", number(i64::from(month.month))),
        ("income", money_json(month.income)),
        ("expenses", money_json(month.expenses)),
        ("net", money_json(month.net())),
    ])
}

fn category_share_json(share: &CategoryShare) -> Json {
    object(vec![
        ("category_id", share.category_id.map_or(Json::Null, |id| number(id.0))),
        ("name", text(share.name.clone())),
        ("amount", money_json(share.amount)),
        ("color", text(share.color.clone())),
    ])
}

fn tax_bookings_json(bookings: &TaxBookings) -> Json {
    object(vec![
        ("from", date_json(bookings.from)),
        ("to", date_json(bookings.to)),
        ("currency", text(bookings.currency.as_str())),
        (
            "income_groups",
            array(bookings.income_groups.iter().map(tax_group_json).collect()),
        ),
        (
            "deduction_groups",
            array(bookings.deduction_groups.iter().map(tax_group_json).collect()),
        ),
        ("booking_count", number(bookings.booking_count as i64)),
        (
            "other_currency_bookings",
            number(bookings.other_currency_bookings as i64),
        ),
        ("income_total", money_json(bookings.income_total())),
        ("deduction_total", money_json(bookings.deduction_total())),
        ("net_total", money_json(bookings.net_total())),
        ("with_receipt", number(bookings.with_receipt() as i64)),
        ("title", text(bookings.title(Language::En))),
    ])
}

fn tax_group_json(group: &TaxGroup) -> Json {
    object(vec![
        ("category_id", group.category_id.map_or(Json::Null, |id| number(id.0))),
        ("name", text(group.name.clone())),
        ("color", text(group.color.clone())),
        ("bookings", array(group.bookings.iter().map(item_json).collect())),
        ("income", money_json(group.income)),
        ("expenses", money_json(group.expenses)),
        ("net", money_json(group.net())),
        ("with_receipt", number(group.with_receipt() as i64)),
    ])
}

fn monthly_report_json(report: &MonthlyReport) -> Json {
    object(vec![
        ("year", number(i64::from(report.year))),
        ("month", number(i64::from(report.month))),
        ("total_income", money_json(report.total_income)),
        ("total_expenses", money_json(report.total_expenses)),
        ("net_amount", money_json(report.net_amount)),
        (
            "by_category",
            array(
                report
                    .by_category
                    .iter()
                    .map(|(name, amount)| object(vec![("name", text(name.clone())), ("amount", money_json(*amount))]))
                    .collect(),
            ),
        ),
        (
            "top_expenses",
            array(report.top_expenses.iter().map(expense_item_json).collect()),
        ),
        ("transaction_count", number(report.transaction_count as i64)),
    ])
}

fn expense_item_json(item: &ExpenseItem) -> Json {
    object(vec![
        ("description", text(item.description.clone())),
        ("amount", money_json(item.amount)),
        ("category", text(item.category.clone())),
        ("date", date_json(item.date)),
    ])
}

fn export_json(fixture: &DashboardFixture) -> Json {
    let temp = tempfile::tempdir().expect("a temp directory");
    let from = Date::from_calendar_date(2026, Month::January, 1).expect("a valid date");
    let to = Date::from_calendar_date(2026, Month::December, 31).expect("a valid date");

    let mut csv_files = Vec::new();
    for language in [Language::En, Language::De] {
        let path = temp.path().join(format!("transactions-{}.csv", language.code()));
        report_csv::export_range_to_csv(&fixture.db, from, to, &path, language).expect("the CSV export");
        let bytes = fs::read(&path).expect("the CSV file");
        csv_files.push(object(vec![
            ("language", text(language.code())),
            ("text", text(String::from_utf8(bytes).expect("the CSV is UTF-8"))),
        ]));
    }

    let items = all_items(&fixture.db);
    let year_items: Vec<TransactionItem> = items.iter().filter(|item| item.date.year() == 2026).cloned().collect();
    let bookings = report_service::build_tax_bookings(from, to, &year_items, Currency::EUR, Language::En);
    let exporter = TaxReportExporter;

    let xlsx_path = temp.path().join("tax-bookings.xlsx");
    exporter
        .export_xlsx(&bookings, &xlsx_path, Language::En)
        .expect("the tax XLSX export");
    let xlsx_bytes = fs::read(&xlsx_path).expect("the tax XLSX file");
    let sheets = vec![
        xlsx_sheet_rows(&xlsx_bytes, 0).expect("the overview sheet"),
        xlsx_sheet_rows(&xlsx_bytes, 1).expect("the bookings sheet"),
    ];

    let pdf_bytes = exporter.build_pdf(&bookings, today(), Language::En);
    let pdf_path = temp.path().join("tax-bookings.pdf");
    fs::write(&pdf_path, &pdf_bytes).expect("the tax PDF file");
    let pdf_text = crate::documents::pdf_text::extract_text(&pdf_path, 100);
    let pdf_info = crate::documents::pdf_text::inspect(&pdf_path);

    let report = report_service::build_report(from, to, &year_items, Currency::EUR, Language::En);
    let report_pdf = PdfReportExporter.build_pdf(&report, today(), Language::En);
    let report_pdf_path = temp.path().join("report.pdf");
    fs::write(&report_pdf_path, &report_pdf).expect("the report PDF file");
    let report_text = crate::documents::pdf_text::extract_text(&report_pdf_path, 100);
    let report_info = crate::documents::pdf_text::inspect(&report_pdf_path);

    object(vec![
        ("csv", array(csv_files)),
        (
            "tax_xlsx",
            object(vec![(
                "sheets",
                array(
                    sheets
                        .into_iter()
                        .map(|rows| {
                            array(
                                rows.into_iter()
                                    .map(|row| array(row.into_iter().map(Json::Text).collect()))
                                    .collect(),
                            )
                        })
                        .collect(),
                ),
            )]),
        ),
        (
            "tax_pdf",
            object(vec![
                (
                    "text",
                    pdf_text.map_or(Json::Null, |extracted| {
                        object(vec![
                            ("text", text(extracted.text.clone())),
                            ("page_count", number(extracted.page_count as i64)),
                        ])
                    }),
                ),
                (
                    "info",
                    pdf_info.map_or(Json::Null, |info| {
                        object(vec![
                            ("page_count", number(info.page_count as i64)),
                            (
                                "pages",
                                array(
                                    info.pages
                                        .iter()
                                        .map(|page| {
                                            object(vec![
                                                ("width_pt", float(f64::from(page.width_pt))),
                                                ("height_pt", float(f64::from(page.height_pt))),
                                            ])
                                        })
                                        .collect(),
                                ),
                            ),
                        ])
                    }),
                ),
            ]),
        ),
        (
            "report_pdf",
            object(vec![
                (
                    "text",
                    report_text.map_or(Json::Null, |extracted| {
                        object(vec![
                            ("text", text(extracted.text.clone())),
                            ("page_count", number(extracted.page_count as i64)),
                        ])
                    }),
                ),
                (
                    "info",
                    report_info.map_or(Json::Null, |info| {
                        object(vec![("page_count", number(info.page_count as i64))])
                    }),
                ),
            ]),
        ),
    ])
}

/// Reads one worksheet of an XLSX file by pointing the first relationship at that sheet and reusing the
/// production reader. The writer always emits `worksheets/sheetN.xml`.
fn xlsx_sheet_rows(bytes: &[u8], sheet: usize) -> Result<Vec<Vec<String>>, String> {
    if sheet == 0 {
        return crate::import::xlsx::first_sheet_rows(bytes).map_err(|error| error.to_string());
    }
    let mut entries: Vec<(String, Vec<u8>)> = Vec::new();
    let mut archive = zip::ZipArchive::new(Cursor::new(bytes)).map_err(|error| error.to_string())?;
    for index in 0..archive.len() {
        let mut entry = archive.by_index(index).map_err(|error| error.to_string())?;
        let name = entry.name().map(|name| name.into_owned()).unwrap_or_default();
        let mut content = Vec::new();
        entry.read_to_end(&mut content).map_err(|error| error.to_string())?;
        entries.push((name, content));
    }
    for (name, content) in &mut entries {
        if name == "xl/_rels/workbook.xml.rels" {
            let text = String::from_utf8_lossy(content).into_owned();
            *content = text
                .replace(
                    "Target=\"worksheets/sheet1.xml\"",
                    &format!("Target=\"worksheets/sheet{}.xml\"", sheet + 1),
                )
                .into_bytes();
        }
    }
    let mut writer = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for (name, content) in entries {
        writer
            .start_file(name, zip::write::SimpleFileOptions::default())
            .map_err(|error| error.to_string())?;
        writer.write_all(&content).map_err(|error| error.to_string())?;
    }
    let rewritten = writer.finish().map_err(|error| error.to_string())?.into_inner();
    crate::import::xlsx::first_sheet_rows(&rewritten).map_err(|error| error.to_string())
}

// ---------------------------------------------------------------------------------------------------
// i18n/
// ---------------------------------------------------------------------------------------------------

fn i18n_json() -> Json {
    let mut entries: Vec<(String, Json)> = Key::ALL
        .iter()
        .map(|key| {
            (
                format!("{key:?}"),
                object(vec![
                    ("en", text(i18n::text(Language::En, *key))),
                    ("de", text(i18n::text(Language::De, *key))),
                    ("fr", text(i18n::text(Language::Fr, *key))),
                ]),
            )
        })
        .collect();
    entries.sort_by(|left, right| left.0.cmp(&right.0));
    Json::Object(entries)
}

// ---------------------------------------------------------------------------------------------------
// bank_directory/
// ---------------------------------------------------------------------------------------------------

fn bank_directory_json() -> Json {
    let entries = crate::bank_directory::parse(&read_input("bank_directory", "blz-test.txt"));
    let directory = crate::bank_directory::BankDirectory::from_entries(entries.clone());
    let lookups = [
        "12345678",
        " 12345678 ",
        "10020890",
        "70020270",
        "80331000",
        "99999999",
        "",
    ];
    object(vec![
        (
            "entries",
            array(
                entries
                    .iter()
                    .map(|entry| {
                        object(vec![
                            ("blz", text(entry.blz.clone())),
                            ("name", text(entry.name.clone())),
                            ("postal_code", text(entry.postal_code.clone())),
                            ("city", text(entry.city.clone())),
                            ("bic", text(entry.bic.clone())),
                        ])
                    })
                    .collect(),
            ),
        ),
        (
            "fints_blz",
            array(
                lookups
                    .iter()
                    .map(|blz| {
                        object(vec![
                            ("input", text(*blz)),
                            ("blz", text(crate::bank_directory::fints_blz(&directory, blz))),
                        ])
                    })
                    .collect(),
            ),
        ),
    ])
}

// ---------------------------------------------------------------------------------------------------
// Writing and the freshness test.
// ---------------------------------------------------------------------------------------------------

fn write_file(root: &Path, relative: &str, value: &Json) {
    let path = root.join(relative);
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).unwrap_or_else(|error| panic!("create {}: {error}", parent.display()));
    }
    let mut content = value.pretty();
    content.push('\n');
    fs::write(&path, content).unwrap_or_else(|error| panic!("write {}: {error}", path.display()));
}

fn write_all(root: &Path) {
    for area in AREAS {
        fs::create_dir_all(root.join(area)).unwrap_or_else(|error| panic!("create {area}: {error}"));
    }

    let imports = read_imports();
    write_file(root, "import/statements.json", &import_json(&imports));
    write_file(root, "hash/hashes.json", &hash_json(&imports));
    write_file(root, "dedup/reports.json", &dedup_json());
    write_file(root, "format/formats.json", &format_json());
    write_file(root, "iban/ibans.json", &iban_json());
    write_file(root, "rules/files.json", &rules_json());
    write_file(root, "rules/matcher.json", &rules_matcher_json());
    write_file(root, "rules/payment_providers.json", &array(payment_provider_cases()));
    write_file(root, "bank_text/bookings.json", &bank_text_json(&imports));
    write_file(root, "balance/charts.json", &balance_json(&imports));
    write_file(root, "recurring/detections.json", &recurring_json());
    write_file(root, "receipt/receipts.json", &receipt_json());

    let fixture = dashboard_fixture();
    write_file(root, "dashboard/months.json", &dashboard_json(&fixture));
    write_file(root, "report/reports.json", &report_json(&fixture));
    write_file(root, "export/exports.json", &export_json(&fixture));

    write_file(root, "i18n/keys.json", &i18n_json());
    write_file(root, "bank_directory/lookups.json", &bank_directory_json());
}

fn collect_files(root: &Path) -> Vec<(String, Vec<u8>)> {
    fn walk(base: &Path, current: &Path, files: &mut Vec<(String, Vec<u8>)>) {
        let mut entries: Vec<_> = fs::read_dir(current)
            .unwrap_or_else(|error| panic!("read_dir {}: {error}", current.display()))
            .map(|entry| entry.expect("a readable directory entry").path())
            .collect();
        entries.sort();
        for path in entries {
            if path.is_dir() {
                walk(base, &path, files);
            } else {
                let relative = path
                    .strip_prefix(base)
                    .expect("a path below the root")
                    .to_string_lossy()
                    .into_owned();
                files.push((
                    relative,
                    fs::read(&path).unwrap_or_else(|error| panic!("read {}: {error}", path.display())),
                ));
            }
        }
    }
    let mut files = Vec::new();
    for area in AREAS {
        walk(root, &root.join(area), &mut files);
    }
    files
}

/// Writes every golden. Run by hand; not part of the normal test run.
#[test]
#[ignore]
fn write_goldens() {
    write_all(&golden_dir());
}

/// The committed goldens must not go stale, and two runs must produce the same bytes.
#[test]
fn goldens_are_current() {
    let first = tempfile::tempdir().expect("a temp directory");
    write_all(first.path());

    let second = tempfile::tempdir().expect("a second temp directory");
    write_all(second.path());

    let generated = collect_files(first.path());
    let repeated = collect_files(second.path());
    assert_eq!(generated, repeated, "the generator is not deterministic");

    let committed = collect_files(&golden_dir());
    assert_eq!(
        generated, committed,
        "testdata/golden is stale; run `cargo test -p finglass-core write_goldens -- --ignored`"
    );
}
