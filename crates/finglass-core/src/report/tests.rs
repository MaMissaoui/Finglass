//! Ports of FinDesk's report and export tests.

use std::collections::BTreeMap;
use std::io::{Cursor, Read};
use std::str::FromStr;
use std::sync::atomic::{AtomicU64, Ordering};

use roxmltree::{Document, Node};
use rust_decimal::Decimal;
use time::macros::date;
use time::{Date, Duration, OffsetDateTime};
use zip::ZipArchive;

use super::csv::{CsvFormat, export_to_csv, export_year_to_csv};
use super::pdf_report::PdfReportExporter;
use super::service::*;
use super::suggestions;
use super::tax_export::TaxReportExporter;
use crate::db::Database;
use crate::export::pdf::text_from_winansi;
use crate::i18n::{self, Key, Language};
use crate::import::csv as import_csv;
use crate::import::xlsx::first_sheet_rows;
use crate::model::{
    AccountId, AccountType, CategoryId, NewAccount, NewCategory, NewRule, NewTransaction, RuleDirection, RuleField,
    TaxSource, TransactionFilter, TransactionItem,
};
use crate::money::{Currency, Money, format_plain};
use crate::repo;
use crate::secret::{Passphrase, PassphrasePurpose};
use crate::settings;

fn eur(minor: i64) -> Money {
    Money {
        minor,
        currency: Currency::EUR,
    }
}

fn usd(minor: i64) -> Money {
    Money {
        minor,
        currency: Currency::new("USD").unwrap(),
    }
}

fn database() -> (tempfile::TempDir, Database) {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("finglass.db");
    let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
    let db = Database::create(&path, &pass).unwrap();
    (temp, db)
}

static ACCOUNT_NUMBER: AtomicU64 = AtomicU64::new(0);

fn add_account(db: &Database, currency: Currency) -> AccountId {
    let number = ACCOUNT_NUMBER.fetch_add(1, Ordering::Relaxed);
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some(format!("Account {number}")),
            iban: None,
            account_number: None,
            currency,
            account_type: AccountType::Checking,
            balance: Money { minor: 0, currency },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

fn add_category(db: &Database, name: &str, color: &str, tax_relevant: bool, transfer: bool) -> CategoryId {
    repo::categories::create(
        db,
        NewCategory {
            name: name.to_owned(),
            color: color.to_owned(),
            budget_limit: None,
            is_tax_relevant: tax_relevant,
            is_transfer: transfer,
        },
    )
    .unwrap()
}

static TRANSACTION_NUMBER: AtomicU64 = AtomicU64::new(0);

fn add_transaction(
    db: &Database,
    account: AccountId,
    day: Date,
    amount: Money,
    partner: Option<&str>,
    purpose: Option<&str>,
    category: Option<CategoryId>,
) {
    let number = TRANSACTION_NUMBER.fetch_add(1, Ordering::Relaxed);
    repo::transactions::insert_imported(
        db,
        account,
        vec![NewTransaction {
            booking_date: day,
            amount,
            balance_after: None,
            partner_name: partner.map(str::to_owned),
            purpose: purpose.map(str::to_owned),
            category_id: category,
            import_hash: format!("test-hash-{number}"),
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }],
    )
    .unwrap();
}

fn items(db: &Database) -> Vec<TransactionItem> {
    repo::transactions::find_items(db, &TransactionFilter::default(), None).unwrap()
}

#[derive(Clone)]
struct Item {
    id: i64,
    minor: i64,
    date: Date,
    category_id: Option<CategoryId>,
    category_name: Option<String>,
    category_color: Option<String>,
    tax_relevant: bool,
    tax_source: TaxSource,
    currency: Currency,
    document_count: usize,
    partner_name: Option<String>,
    purpose: Option<String>,
    is_transfer: bool,
    tax_override: Option<bool>,
}

fn item(id: i64, minor: i64) -> Item {
    Item {
        id,
        minor,
        date: date!(2026 - 06 - 01),
        category_id: None,
        category_name: None,
        category_color: None,
        tax_relevant: false,
        tax_source: TaxSource::None,
        currency: Currency::EUR,
        document_count: 0,
        partner_name: None,
        purpose: None,
        is_transfer: false,
        tax_override: None,
    }
}

impl Item {
    fn date(mut self, date: Date) -> Self {
        self.date = date;
        self
    }

    fn category(mut self, id: i64, name: &str, color: &str) -> Self {
        self.category_id = Some(CategoryId(id));
        self.category_name = Some(name.to_owned());
        self.category_color = Some(color.to_owned());
        self
    }

    fn tax(mut self, source: TaxSource) -> Self {
        self.tax_relevant = true;
        self.tax_source = source;
        self
    }

    fn tax_relevant(mut self) -> Self {
        self.tax_relevant = true;
        if self.tax_source == TaxSource::None {
            self.tax_source = TaxSource::Category;
        }
        self
    }

    fn source(mut self, source: TaxSource) -> Self {
        self.tax_source = source;
        self
    }

    fn currency(mut self, currency: Currency) -> Self {
        self.currency = currency;
        self
    }

    fn documents(mut self, count: usize) -> Self {
        self.document_count = count;
        self
    }

    fn partner(mut self, partner: &str) -> Self {
        self.partner_name = Some(partner.to_owned());
        self
    }

    fn purpose(mut self, purpose: &str) -> Self {
        self.purpose = Some(purpose.to_owned());
        self
    }

    fn transfer(mut self) -> Self {
        self.is_transfer = true;
        self
    }

    fn override_tax(mut self, value: bool) -> Self {
        self.tax_override = Some(value);
        self
    }

    fn build(self) -> TransactionItem {
        TransactionItem {
            id: crate::model::TransactionId(self.id),
            account_id: AccountId(1),
            account_iban: Some("DE89370400440532013000".to_owned()),
            date: self.date,
            amount: Money {
                minor: self.minor,
                currency: self.currency,
            },
            balance_after: None,
            currency: self.currency,
            partner_name: self.partner_name,
            purpose: self.purpose,
            category_id: self.category_id,
            category_name: self.category_name,
            category_color: self.category_color,
            is_tax_relevant: self.tax_relevant,
            document_count: self.document_count,
            is_new: false,
            is_transfer: self.is_transfer,
            tax_source: self.tax_source,
            tax_rule_pattern: None,
            tax_override: self.tax_override,
        }
    }
}

fn items_of(values: Vec<Item>) -> Vec<TransactionItem> {
    values.into_iter().map(Item::build).collect()
}

fn category_amount(pairs: &[(String, Money)], name: &str) -> Option<Money> {
    pairs
        .iter()
        .find(|(category, _)| category == name)
        .map(|(_, amount)| *amount)
}

fn single<T>(items: &[T]) -> &T {
    assert_eq!(items.len(), 1, "expected exactly one item");
    &items[0]
}

// ---------------------------------------------------------------------------------------------
// ReportServiceTest
// ---------------------------------------------------------------------------------------------

#[test]
fn tax_report_data_class() {
    let mut report = TaxReport::empty(2026);
    report.total_income = eur(3_840_000);
    report.total_expenses = eur(2_580_000);
    report.net_income = eur(1_260_000);
    report.expenses_by_category = vec![("Groceries".to_owned(), eur(624_000))];
    report.tax_relevant_expenses = vec![("Transport".to_owned(), eur(360_000))];
    report.transaction_count = 100;

    assert_eq!(report.year, 2026);
    assert_eq!(report.total_income, eur(3_840_000));
    assert_eq!(report.net_income, eur(1_260_000));
}

#[test]
fn tax_report_empty_factory() {
    let report = TaxReport::empty(2025);
    assert_eq!(report.year, 2025);
    assert_eq!(report.total_income, eur(0));
    assert_eq!(report.total_expenses, eur(0));
    assert_eq!(report.transaction_count, 0);
}

#[test]
fn monthly_report_data_class() {
    let mut by_category = BTreeMap::new();
    by_category.insert("Groceries".to_owned(), eur(50_000));
    let report = MonthlyReport {
        year: 2026,
        month: 10,
        total_income: eur(320_000),
        total_expenses: eur(210_000),
        net_amount: eur(110_000),
        by_category,
        top_expenses: Vec::new(),
        transaction_count: 50,
    };
    assert_eq!(report.year, 2026);
    assert_eq!(report.month, 10);
}

#[test]
fn category_trend_data_class() {
    let trend = CategoryTrend {
        category_id: CategoryId(1),
        category_name: "Groceries".to_owned(),
        monthly_data: vec![
            MonthlyAmount {
                year: 2026,
                month: 1,
                amount: eur(50_000),
            },
            MonthlyAmount {
                year: 2026,
                month: 2,
                amount: eur(55_000),
            },
        ],
        average_monthly: eur(52_500),
        trend: Decimal::from_str("4.8").unwrap(),
    };
    assert_eq!(trend.category_name, "Groceries");
    assert_eq!(trend.monthly_data.len(), 2);
}

// ---------------------------------------------------------------------------------------------
// ReportsAndExportTest
// ---------------------------------------------------------------------------------------------

fn report_fixture() -> (tempfile::TempDir, Database) {
    let (temp, db) = database();
    let account = add_account(&db, Currency::EUR);
    let food = add_category(&db, "Food", "#FF5722", false, false);
    let office = add_category(&db, "Arbeitsmittel", "#2196F3", true, false);
    add_transaction(
        &db,
        account,
        date!(2026 - 01 - 10),
        eur(-4_000),
        Some("REWE"),
        None,
        Some(food),
    );
    add_transaction(
        &db,
        account,
        date!(2026 - 02 - 10),
        eur(-25_000),
        Some("=HYPERLINK(\"http://evil\")"),
        Some("Monitor, 27\""),
        Some(office),
    );
    add_transaction(
        &db,
        account,
        date!(2026 - 02 - 28),
        eur(300_000),
        Some("Employer GmbH"),
        None,
        None,
    );
    add_transaction(
        &db,
        account,
        date!(2025 - 12 - 31),
        eur(-100),
        Some("Last year"),
        None,
        None,
    );
    (temp, db)
}

#[test]
fn tax_report_includes_categorized_transactions_regression() {
    let (_temp, db) = report_fixture();
    let report = generate_tax_report(&db, 2026, Language::En).unwrap();

    assert_eq!(report.transaction_count, 3);
    assert_eq!(report.total_income, eur(300_000));
    assert_eq!(report.total_expenses, eur(29_000));
    assert_eq!(report.net_income, eur(271_000));
    assert_eq!(
        report.tax_relevant_expenses,
        vec![("Arbeitsmittel".to_owned(), eur(25_000))]
    );
    assert_eq!(report.tax_relevant_total(), eur(25_000));
    assert_eq!(report.monthly_breakdown[1].expenses, eur(25_000));
    assert_eq!(report.monthly_breakdown[1].income, eur(300_000));
    assert_eq!(report.categories_used, 2);
}

#[test]
fn category_trend_averages_over_the_requested_months() {
    let (_temp, db) = report_fixture();
    let food = repo::categories::list(&db)
        .unwrap()
        .into_iter()
        .find(|category| category.name == "Food")
        .unwrap()
        .id;
    let trend = get_category_trends(&db, food, 3, date!(2026 - 03 - 15), Language::En).unwrap();
    assert_eq!(
        trend.monthly_data.iter().map(|month| month.month).collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
    assert_eq!(trend.monthly_data[0].amount, eur(4_000));
    assert_eq!(trend.average_monthly, eur(1_333));
}

#[test]
fn csv_export_quotes_fields_and_neutralises_formulas() {
    let (temp, db) = report_fixture();
    let file = temp.path().join("out.csv");
    export_year_to_csv(&db, 2026, &file, Language::En).unwrap();

    let bytes = std::fs::read(&file).unwrap();
    assert_eq!(&bytes[..3], &[0xef, 0xbb, 0xbf], "UTF-8 BOM");
    let text = String::from_utf8(bytes[3..].to_vec()).unwrap();
    assert!(text.starts_with("Date,Partner,Purpose"));
    assert!(
        text.lines().next().unwrap().ends_with(",Shop"),
        "the Shop column is last: {text}"
    );
    assert!(text.contains("\"'=HYPERLINK(\"\"http://evil\"\")\""), "{text}");
    assert!(text.contains("\"Monitor, 27\"\"\""), "{text}");
    assert!(text.contains(",-250.00,EUR,"), "amounts keep their sign: {text}");
    assert_eq!(text.trim().lines().count(), 4, "header + 3 rows");
}

#[test]
fn csv_export_neutralises_account_iban_formulas() {
    let (temp, _db) = database();
    let file = temp.path().join("out.csv");
    let mut transaction = item(1, -1_000).partner("Shop").purpose("Purpose").build();
    transaction.account_iban = Some("@TACKER-123".to_owned());
    export_to_csv(&[transaction], &file, None, Language::En).unwrap();

    let text = std::fs::read_to_string(&file).unwrap();
    assert!(
        text.contains(",'@TACKER-123"),
        "account IBAN formula is neutralised: {text}"
    );
    assert_eq!(CsvFormat::text("=1+1"), "'=1+1");
}

#[test]
fn pdf_export_writes_a_readable_report() {
    let (temp, db) = report_fixture();
    let report = generate_tax_report(&db, 2026, Language::En).unwrap();
    let file = temp.path().join("report.pdf");
    PdfReportExporter
        .export(&report, &file, date!(2026 - 11 - 07), Language::En)
        .unwrap();

    let bytes = std::fs::read(&file).unwrap();
    assert_pdf(&bytes);
    assert!(pdf_contains(&bytes, "Finglass Tax Report 2026"));
    assert!(pdf_contains(&bytes, "Arbeitsmittel"));
    assert!(pdf_contains(&bytes, "€3,000.00"));
}

#[test]
fn german_pdf_export_uses_the_german_money_convention() {
    let (_temp, db) = report_fixture();
    let report = generate_tax_report(&db, 2026, Language::De).unwrap();
    let bytes = PdfReportExporter.build_pdf(&report, date!(2026 - 11 - 07), Language::De);

    assert_pdf(&bytes);
    assert!(pdf_contains(&bytes, "Arbeitsmittel"));
    assert!(
        pdf_contains(&bytes, "3.000,00\u{a0}€"),
        "the PDF font encodes the no-break space and the euro glyph"
    );
}

#[test]
fn csv_export_adds_the_shop_last_and_keeps_the_raw_partner() {
    let (temp, _db) = database();
    let file = temp.path().join("shop.csv");
    let paypal = paypal_booking(1);
    let plain = plain_booking(2);
    export_to_csv(&[paypal, plain], &file, None, Language::En).unwrap();

    let text = std::fs::read_to_string(&file).unwrap();
    let records = import_csv::split_records(text.trim_start_matches('\u{feff}'), ',', usize::MAX);
    assert_eq!(
        records[0].join(","),
        "Date,Partner,Purpose,Category,Amount,Currency,Account,Shop"
    );
    assert_eq!(
        records[1][1], "PayPal Europe S.a.r.l. et Cie S.C.A",
        "the raw partner stays in column 2"
    );
    assert_eq!(
        records[1].last().unwrap(),
        "Beispiel Shop GmbH",
        "the shop is the last field"
    );
    assert_eq!(records[2].last().unwrap(), "", "a non-PayPal booking has no shop");
}

#[test]
fn csv_export_puts_the_shop_after_the_balance() {
    let (temp, _db) = database();
    let file = temp.path().join("shop-balances.csv");
    let paypal = paypal_booking(1);
    let plain = plain_booking(2);
    let balances = BTreeMap::from([(paypal.id, eur(10_000)), (plain.id, eur(9_000))]);
    export_to_csv(&[paypal, plain], &file, Some(&balances), Language::En).unwrap();

    let text = std::fs::read_to_string(&file).unwrap();
    let records = import_csv::split_records(text.trim_start_matches('\u{feff}'), ',', usize::MAX);
    assert_eq!(
        records[0].join(","),
        "Date,Partner,Purpose,Category,Amount,Currency,Account,Balance,Shop"
    );
    assert_eq!(
        records[1][records[1].len() - 2],
        "100.00",
        "the balance stays before the shop"
    );
    assert_eq!(records[1].last().unwrap(), "Beispiel Shop GmbH");
    assert_eq!(records[2][records[2].len() - 2], "90.00");
    assert_eq!(records[2].last().unwrap(), "");
}

#[test]
fn localized_shop_headers_never_map_to_the_partner_or_purpose_column() {
    let (temp, _db) = database();
    let paypal = paypal_booking(1);
    for language in Language::ALL {
        let file = temp.path().join(format!("shop-{}.csv", language.code()));
        export_to_csv(std::slice::from_ref(&paypal), &file, None, *language).unwrap();
        let text = std::fs::read_to_string(&file).unwrap();
        let header = import_csv::split_records(text.trim_start_matches('\u{feff}'), ',', usize::MAX)
            .into_iter()
            .next()
            .unwrap();
        let mapping = import_csv::detect_mapping(&header);

        assert_eq!(
            header.last().unwrap(),
            i18n::text(*language, Key::ColShop),
            "{}: the Shop header",
            language.code()
        );
        assert_eq!(
            import_csv::detect_mapping(&header[..header.len() - 1]),
            mapping,
            "{}: mapping without vs. with Shop",
            language.code()
        );
        assert_eq!(mapping.date, Some(0), "{}: date", language.code());
        assert_ne!(
            Some(header.len() - 1),
            mapping.partner,
            "{}: the Shop column is not the partner",
            language.code()
        );
        assert_ne!(
            Some(header.len() - 1),
            mapping.purpose,
            "{}: the Shop column is not the purpose",
            language.code()
        );
        assert_ne!(
            Some(header.len() - 1),
            mapping.payer,
            "{}: the Shop column is not the payer",
            language.code()
        );
    }
}

#[test]
fn the_shop_header_leaves_recognized_import_columns_where_they_were() {
    let english = import_csv::detect_mapping(&[
        "Date".to_owned(),
        "Payee".to_owned(),
        "Purpose".to_owned(),
        "Amount".to_owned(),
        "Shop".to_owned(),
    ]);
    assert_eq!(english.partner, Some(1), "EN partner stays on its column");
    assert_eq!(english.purpose, Some(2), "EN purpose stays on its column");
    assert_ne!(english.partner, Some(4));
    assert_ne!(english.purpose, Some(4));

    let german = import_csv::detect_mapping(&[
        "Buchungstag".to_owned(),
        "Zahlungsempfänger".to_owned(),
        "Verwendungszweck".to_owned(),
        "Betrag".to_owned(),
        "Händler".to_owned(),
    ]);
    assert_eq!(german.partner, Some(1), "DE partner stays on its column");
    assert_eq!(german.purpose, Some(2), "DE purpose stays on its column");
    assert_ne!(german.partner, Some(4));
    assert_ne!(german.purpose, Some(4));
}

#[test]
fn csv_format_escapes_only_when_needed() {
    assert_eq!(CsvFormat::escape("plain"), "plain");
    assert_eq!(CsvFormat::escape("a,b"), "\"a,b\"");
    assert_eq!(CsvFormat::text("+49 123"), "'+49 123");
    assert_eq!(CsvFormat::text(""), "");
}

fn paypal_booking(id: i64) -> TransactionItem {
    let mut item = item(id, -1_999)
        .date(date!(2026 - 03 - 10))
        .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
        .purpose("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH")
        .build();
    item.account_iban = Some("DE89370400440532013000".to_owned());
    item
}

fn plain_booking(id: i64) -> TransactionItem {
    let mut item = item(id, -450)
        .date(date!(2026 - 03 - 11))
        .partner("Beispiel Bäckerei")
        .purpose("Brötchen")
        .build();
    item.account_iban = Some("DE89370400440532013000".to_owned());
    item
}

// ---------------------------------------------------------------------------------------------
// ReportsFixesTest (report parts)
// ---------------------------------------------------------------------------------------------

#[test]
fn a_refund_lowers_the_tax_relevant_expenses_to_the_tax_bookings_deduction_total() {
    let txns = items_of(vec![
        item(1, -10_000).category(1, "Handwerk", "").tax_relevant(),
        item(2, 3_000)
            .date(date!(2026 - 06 - 02))
            .category(1, "Handwerk", "")
            .tax_relevant(),
    ]);
    let report = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    assert_eq!(
        category_amount(&report.tax_relevant_expenses, "Handwerk"),
        Some(eur(7_000)),
        "the refund lowers its category"
    );
    assert_eq!(
        build_tax_bookings(
            date!(2026 - 01 - 01),
            date!(2026 - 12 - 31),
            &txns,
            Currency::EUR,
            Language::En
        )
        .deduction_total(),
        report.tax_relevant_total()
    );
}

#[test]
fn a_tax_relevant_income_category_is_not_among_the_tax_relevant_expenses() {
    let txns = items_of(vec![
        item(1, 50_000).category(2, "Vermietung", "").tax_relevant(),
        item(2, -5_000)
            .date(date!(2026 - 06 - 02))
            .category(2, "Vermietung", "")
            .tax_relevant(),
    ]);
    let report = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    assert!(
        report.tax_relevant_expenses.is_empty(),
        "money out on a net-income category is no deduction"
    );
    assert_eq!(report.tax_relevant_total(), eur(0));
}

// ---------------------------------------------------------------------------------------------
// TopVendorsShopTest
// ---------------------------------------------------------------------------------------------

#[test]
fn paypal_purchases_group_by_the_shop_named_in_the_purpose() {
    let txns = items_of(vec![
        item(1, -1_000)
            .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
            .purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
        item(2, -2_000)
            .date(date!(2026 - 06 - 02))
            .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
            .purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
        item(3, -4_000)
            .date(date!(2026 - 06 - 03))
            .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
            .purpose("Ihr Einkauf bei Anderer Shop GmbH"),
        item(4, -3_000).date(date!(2026 - 06 - 04)).partner("Beispiel Inc."),
    ]);
    let vendors = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    )
    .top_vendors;

    assert_eq!(
        vendors,
        vec![
            ("PayPal · Anderer Shop GmbH".to_owned(), eur(4_000)),
            ("PayPal · Beispiel Shop GmbH".to_owned(), eur(3_000)),
            ("Beispiel Inc.".to_owned(), eur(3_000)),
        ],
        "the two Beispiel Shop purchases are summed, Anderer Shop stays separate"
    );
}

#[test]
fn vendor_names_group_case_and_whitespace_insensitively() {
    let txns = items_of(vec![
        item(1, -1_000).partner("BEISPIEL MARKT"),
        item(2, -2_000).date(date!(2026 - 06 - 02)).partner("Beispiel Markt"),
        item(3, -500).date(date!(2026 - 06 - 03)).partner("Beispiel  Markt   "),
    ]);
    let vendors = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    )
    .top_vendors;

    assert_eq!(
        vendors,
        vec![("Beispiel Markt".to_owned(), eur(3_500))],
        "the three spellings are one vendor, shown with the spelling two bookings use"
    );
}

#[test]
fn a_spelling_tie_shows_the_most_recent_bookings_spelling() {
    let later_beispiel = items_of(vec![
        item(1, -1_000).partner("BEISPIEL MARKT"),
        item(2, -2_000).date(date!(2026 - 06 - 02)).partner("Beispiel Markt"),
    ]);
    let later_upper = items_of(vec![
        item(1, -1_000).date(date!(2026 - 06 - 02)).partner("BEISPIEL MARKT"),
        item(2, -2_000).partner("Beispiel Markt"),
    ]);

    assert_eq!(
        build_report(
            date!(2026 - 01 - 01),
            date!(2026 - 12 - 31),
            &later_beispiel,
            Currency::EUR,
            Language::En
        )
        .top_vendors
        .first()
        .unwrap()
        .0,
        "Beispiel Markt"
    );
    assert_eq!(
        build_report(
            date!(2026 - 01 - 01),
            date!(2026 - 12 - 31),
            &later_upper,
            Currency::EUR,
            Language::En
        )
        .top_vendors
        .first()
        .unwrap()
        .0,
        "BEISPIEL MARKT"
    );
}

#[test]
fn a_paypal_card_booking_and_a_marker_booking_group_under_one_shop_ignoring_case() {
    let txns = items_of(vec![
        item(1, -1_000).purpose("PAYPAL *BEISPIELSHOP"),
        item(2, -2_000)
            .date(date!(2026 - 06 - 02))
            .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
            .purpose("Ihr Einkauf bei Beispielshop"),
    ]);
    let vendors = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    )
    .top_vendors;

    assert_eq!(
        vendors,
        vec![("PayPal · Beispielshop".to_owned(), eur(3_000))],
        "the card descriptor and the marker name the same shop and are summed"
    );
}

#[test]
fn a_paypal_booking_whose_purpose_has_no_shop_stays_under_its_description() {
    let partner = "PayPal Europe S.a.r.l. et Cie S.C.A";
    let txns = items_of(vec![
        item(1, -1_500).partner(partner).purpose("PP.1.PP . 1234"),
        item(2, -3_000)
            .date(date!(2026 - 06 - 02))
            .partner(partner)
            .purpose("Ihr Einkauf bei Beispiel Shop GmbH"),
    ]);
    let vendors = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    )
    .top_vendors;

    assert_eq!(
        vendors,
        vec![
            ("PayPal · Beispiel Shop GmbH".to_owned(), eur(3_000)),
            (partner.to_owned(), eur(1_500)),
        ],
        "the shopless PayPal booking keeps its raw description as before"
    );
}

// ---------------------------------------------------------------------------------------------
// ShopNamesTest report parts
// ---------------------------------------------------------------------------------------------

#[test]
fn monthly_report_titles_a_paypal_top_expense_with_the_shop() {
    let txns = items_of(vec![
        item(1, -25_000)
            .partner("PayPal Europe S.a.r.l. et Cie S.C.A")
            .purpose("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH")
            .category(5, "Online shopping", ""),
        item(2, -1_000).partner("Beispiel Bäckerei").purpose("Brötchen"),
    ]);
    let report = monthly_report(2026, 6, &txns, Currency::EUR, Language::En);
    assert_eq!(
        report.top_expenses.first().unwrap().description,
        "PayPal · Beispiel Shop GmbH"
    );
}

// ---------------------------------------------------------------------------------------------
// TransferExclusionTest report parts
// ---------------------------------------------------------------------------------------------

#[test]
fn dashboard_and_report_agree_and_leave_the_transfer_out_of_the_totals() {
    let txns = items_of(vec![
        item(1, -50_000)
            .partner("Erika Musterfrau")
            .category(9, "Own transfers", "#4CAF50")
            .transfer(),
        item(2, 50_000)
            .partner("Erika Musterfrau")
            .category(9, "Own transfers", "#4CAF50")
            .transfer(),
        item(3, -4_000)
            .date(date!(2026 - 06 - 16))
            .partner("BEISPIEL MARKT")
            .category(10, "Groceries", "#FF5722"),
        item(4, 100_000).date(date!(2026 - 06 - 17)).partner("Erika Mustermann"),
    ]);
    let report = build_report(
        date!(2026 - 06 - 01),
        date!(2026 - 06 - 30),
        &txns,
        Currency::EUR,
        Language::En,
    );

    assert_eq!(report.total_income, eur(100_000));
    assert_eq!(report.total_expenses, eur(4_000));
    assert_eq!(report.net_income, eur(96_000));
    assert_eq!(report.transfer_total, eur(50_000));
    assert_eq!(
        report
            .top_vendors
            .iter()
            .map(|(name, _)| name.as_str())
            .collect::<Vec<_>>(),
        vec!["BEISPIEL MARKT"],
        "the transfer partner is not a vendor"
    );
    assert_eq!(
        report
            .expenses_by_category
            .iter()
            .map(|(name, _)| name.as_str())
            .collect::<Vec<_>>(),
        vec!["Groceries"]
    );
    let month = single(&report.monthly_breakdown);
    assert_eq!(month.income, eur(100_000));
    assert_eq!(month.expenses, eur(4_000));
    let share = single(&report.category_shares);
    assert_eq!(share.name, "Groceries");
    assert_eq!(share.category_id, Some(CategoryId(10)));
    assert_eq!(share.amount, eur(4_000));
    assert_eq!(share.color, "#FF5722");
}

#[test]
fn tax_relevant_totals_and_budgets_stay_as_they_were() {
    let txns = items_of(vec![
        item(1, -50_000).category(9, "Own transfers", "#4CAF50").transfer(),
        item(2, 50_000).category(9, "Own transfers", "#4CAF50").transfer(),
        item(3, -3_000).category(11, "Transport", "#2196F3").tax_relevant(),
    ]);
    let report = build_report(
        date!(2026 - 06 - 01),
        date!(2026 - 06 - 30),
        &txns,
        Currency::EUR,
        Language::En,
    );
    assert_eq!(report.tax_relevant_total(), eur(3_000));
    assert_eq!(report.tax_relevant_expenses, vec![("Transport".to_owned(), eur(3_000))]);
}

// ---------------------------------------------------------------------------------------------
// MultiCurrencyTest report parts
// ---------------------------------------------------------------------------------------------

#[test]
fn reports_count_bookings_in_other_currencies_instead_of_adding_them() {
    let (_temp, db) = database();
    let eur_account = add_account(&db, Currency::EUR);
    let usd_account = add_account(&db, Currency::new("USD").unwrap());
    add_transaction(
        &db,
        eur_account,
        date!(2026 - 03 - 01),
        eur(-3_000),
        Some("REWE"),
        None,
        None,
    );
    add_transaction(
        &db,
        usd_account,
        date!(2026 - 03 - 02),
        usd(-7_000),
        Some("Walmart"),
        None,
        None,
    );

    let report = build_tax_report(2026, &items(&db), Currency::EUR, Language::En);
    assert_eq!(report.currency, Currency::EUR);
    assert_eq!(report.total_expenses, eur(3_000));
    assert_eq!(report.other_currency_bookings, 1);
    assert_eq!(report.transaction_count, 1);
}

#[test]
fn primary_currency_setting_switches_what_the_reports_total() {
    let (_temp, db) = database();
    let eur_account = add_account(&db, Currency::EUR);
    let usd_account = add_account(&db, Currency::new("USD").unwrap());
    add_transaction(
        &db,
        eur_account,
        date!(2026 - 06 - 02),
        eur(200_000),
        Some("Employer"),
        None,
        None,
    );
    add_transaction(
        &db,
        usd_account,
        date!(2026 - 06 - 03),
        usd(80_000),
        Some("US client"),
        None,
        None,
    );
    settings::set_setting(&db, crate::money::PRIMARY_CURRENCY_KEY, "USD").unwrap();

    let report = generate_tax_report(&db, 2026, Language::En).unwrap();
    assert_eq!(report.currency.as_str(), "USD");
    assert_eq!(report.total_income, usd(80_000));
    assert_eq!(report.other_currency_bookings, 1);
}

// ---------------------------------------------------------------------------------------------
// TaxRelevanceTest report part
// ---------------------------------------------------------------------------------------------

#[test]
fn the_report_counts_a_rule_flagged_booking_of_a_non_tax_category_among_the_tax_relevant_expenses() {
    let (_temp, db) = database();
    let account = add_account(&db, Currency::EUR);
    let category = add_category(&db, "Groceries", "#FF5722", false, false);
    add_transaction(
        &db,
        account,
        date!(2026 - 06 - 14),
        eur(-10_000),
        Some("Beispiel Shop"),
        None,
        Some(category),
    );
    repo::rules::create(
        &db,
        NewRule {
            pattern: "Beispiel".to_owned(),
            category_id: category,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: Some(true),
        },
    )
    .unwrap();

    let report = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &items(&db),
        Currency::EUR,
        Language::En,
    );
    assert!(
        report
            .tax_relevant_transactions
            .iter()
            .any(|item| item.partner_name.as_deref() == Some("Beispiel Shop")),
        "the report reads the effective value"
    );
    assert_eq!(report.tax_relevant_total(), eur(10_000));
}

// ---------------------------------------------------------------------------------------------
// TaxBookingsTest
// ---------------------------------------------------------------------------------------------

#[test]
fn only_effectively_tax_relevant_bookings_of_the_primary_currency_are_included() {
    let txns = items_of(vec![
        item(1, -3_000).category(10, "Spenden", "#9C27B0").tax_relevant(),
        item(2, -1_000).category(11, "Lebensmittel", ""),
        item(3, -4_500)
            .category(12, "Spenden", "#9C27B0")
            .tax_relevant()
            .currency(Currency::new("USD").unwrap()),
    ]);
    let report = build_tax_bookings(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    assert_eq!(report.booking_count, 1);
    assert_eq!(report.other_currency_bookings, 1);
    assert_eq!(report.deduction_total(), eur(3_000));
    assert_eq!(report.income_total(), eur(0));
    assert_eq!(
        report
            .deduction_groups
            .iter()
            .map(|group| group.category_id)
            .collect::<Vec<_>>(),
        vec![Some(CategoryId(10))]
    );
}

#[test]
fn a_salary_category_lands_in_income_and_an_expense_category_in_deductions() {
    let txns = items_of(vec![
        item(1, 200_000)
            .date(date!(2026 - 05 - 29))
            .category(10, "Gehalt", "#4CAF50")
            .tax_relevant(),
        item(2, 220_000).category(10, "Gehalt", "#4CAF50").tax_relevant(),
        item(3, -30_000).category(20, "Handwerk", "#FF5722").tax_relevant(),
        item(4, -8_000)
            .date(date!(2026 - 06 - 06))
            .category(20, "Handwerk", "#FF5722")
            .tax_relevant(),
    ]);
    let report = build_tax_bookings(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    let salary = single(&report.income_groups);
    assert_eq!(salary.category_id, Some(CategoryId(10)));
    assert_eq!(salary.name, "Gehalt");
    assert_eq!(salary.color, "#4CAF50");
    assert_eq!(salary.income, eur(420_000));
    assert_eq!(salary.expenses, eur(0));
    assert_eq!(salary.net(), eur(420_000));

    let handwerk = single(&report.deduction_groups);
    assert_eq!(handwerk.category_id, Some(CategoryId(20)));
    assert_eq!(handwerk.income, eur(0));
    assert_eq!(handwerk.expenses, eur(38_000));
    assert_eq!(handwerk.net(), eur(-38_000));

    assert_eq!(report.income_total(), salary.net());
    assert_eq!(report.deduction_total(), eur(38_000));
    assert_eq!(report.booking_count, 4);
    assert_eq!(
        salary.bookings.iter().map(|booking| booking.id.0).collect::<Vec<_>>(),
        vec![1, 2],
        "salary bookings date ascending"
    );
    assert_eq!(
        handwerk.bookings.iter().map(|booking| booking.id.0).collect::<Vec<_>>(),
        vec![3, 4],
        "deduction bookings date ascending"
    );
}

#[test]
fn a_refund_lowers_a_deduction_category_and_a_category_whose_refunds_exceed_its_costs_moves_to_income() {
    let txns = items_of(vec![
        item(1, -30_000).category(20, "Handwerk", "#FF5722").tax_relevant(),
        item(2, -8_000).category(20, "Handwerk", "#FF5722").tax_relevant(),
        item(3, 10_000)
            .date(date!(2026 - 05 - 22))
            .category(20, "Handwerk", "#FF5722")
            .tax_relevant(),
        item(4, -10_000).category(30, "Kleidung", "").tax_relevant(),
        item(5, 25_000)
            .date(date!(2026 - 05 - 31))
            .category(30, "Kleidung", "")
            .tax_relevant(),
    ]);
    let report = build_tax_bookings(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    let handwerk = single(&report.deduction_groups);
    assert_eq!(
        handwerk.income,
        eur(10_000),
        "the refund's income is inside the category"
    );
    assert_eq!(handwerk.expenses, eur(38_000));
    assert_eq!(handwerk.net(), eur(-28_000));

    let kleidung = single(&report.income_groups);
    assert_eq!(kleidung.category_id, Some(CategoryId(30)));
    assert_eq!(kleidung.income, eur(25_000));
    assert_eq!(kleidung.expenses, eur(10_000));
    assert_eq!(kleidung.net(), eur(15_000));

    assert_eq!(report.deduction_total(), eur(28_000));
    assert_eq!(report.income_total(), eur(15_000));
    assert_eq!(report.booking_count, 5);
}

#[test]
fn sections_sort_by_absolute_net_then_name_and_with_receipt_counts_per_group_and_in_total() {
    let txns = items_of(vec![
        item(1, -40_000)
            .category(20, "Handwerk", "#FF5722")
            .tax_relevant()
            .documents(1),
        item(2, -40_000).category(21, "Zahnarzt", "").tax_relevant(),
        item(3, -5_000).category(22, "Krankenhaus", "").tax_relevant(),
        item(4, 120_000).category(10, "Gehalt", "#4CAF50").tax_relevant(),
        item(5, 120_000)
            .date(date!(2026 - 06 - 21))
            .category(10, "Gehalt", "#4CAF50")
            .tax_relevant(),
        item(6, 90_000)
            .date(date!(2026 - 06 - 02))
            .category(40, "Abmahnkosten", "")
            .tax_relevant()
            .documents(1),
    ]);
    let report = build_tax_bookings(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    assert_eq!(
        report
            .deduction_groups
            .iter()
            .map(|group| group.category_id)
            .collect::<Vec<_>>(),
        vec![Some(CategoryId(20)), Some(CategoryId(21)), Some(CategoryId(22))],
        "by absolute net, then name"
    );
    assert_eq!(
        report
            .income_groups
            .iter()
            .map(|group| group.category_id)
            .collect::<Vec<_>>(),
        vec![Some(CategoryId(10)), Some(CategoryId(40))]
    );
    assert_eq!(
        report
            .income_groups
            .iter()
            .find(|group| group.category_id == Some(CategoryId(40)))
            .unwrap()
            .with_receipt(),
        1
    );
    assert_eq!(
        report
            .deduction_groups
            .iter()
            .find(|group| group.category_id == Some(CategoryId(20)))
            .unwrap()
            .with_receipt(),
        1
    );
    assert_eq!(report.with_receipt(), 2, "one receipt in income plus one in deductions");
    assert_eq!(report.booking_count, 6);
}

#[test]
fn uncategorized_tax_relevant_bookings_form_their_own_group_with_the_localized_label() {
    let txns = items_of(vec![
        item(1, -4_900).tax(TaxSource::Manual).override_tax(true),
        item(2, 1_500)
            .date(date!(2026 - 06 - 02))
            .tax(TaxSource::Manual)
            .override_tax(true),
    ]);
    let report = build_tax_bookings(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );

    let uncategorized = single(&report.deduction_groups);
    assert_eq!(uncategorized.category_id, None);
    assert_eq!(uncategorized.name, i18n::text(Language::En, Key::Uncategorized));
    assert_eq!(
        uncategorized
            .bookings
            .iter()
            .map(|booking| booking.id.0)
            .collect::<Vec<_>>(),
        vec![1, 2],
        "date ascending, then id"
    );
    assert_eq!(report.booking_count, 2);
}

#[test]
fn find_suggests_keyword_matches_on_partner_or_purpose_and_skips_decided_and_transfer_bookings() {
    let keywords = ["Spende", "Handwerker", "Kita"]
        .into_iter()
        .map(str::to_owned)
        .collect::<Vec<_>>();
    let txns = items_of(vec![
        item(1, -3_000).purpose("Spende für Beispielprojekt"),
        item(2, -20_000)
            .date(date!(2026 - 06 - 02))
            .partner("Beispiel-Handwerker GmbH"),
        item(3, -3_000).partner("Spendenaktion").tax_relevant(),
        item(4, -3_000)
            .partner("Spendenaktion")
            .source(TaxSource::Manual)
            .override_tax(false),
        item(5, -3_000).partner("Spendenaktion").transfer(),
        item(6, -900).partner("Bäckerei Beispiel"),
        item(7, -28_000)
            .date(date!(2026 - 04 - 22))
            .purpose("Handwerkerleistung Bad"),
    ]);
    let suggestions = suggestions::find(&txns, &keywords);

    assert_eq!(
        suggestions
            .iter()
            .map(|suggestion| suggestion.booking.id.0)
            .collect::<Vec<_>>(),
        vec![2, 1, 7],
        "date descending"
    );
    assert_eq!(
        suggestions
            .iter()
            .find(|suggestion| suggestion.booking.id.0 == 1)
            .unwrap()
            .keyword,
        "Spende",
        "the first keyword (list order) wins"
    );
    assert_eq!(
        suggestions
            .iter()
            .find(|suggestion| suggestion.booking.id.0 == 2)
            .unwrap()
            .keyword,
        "Handwerker"
    );
    assert_eq!(
        suggestions
            .iter()
            .find(|suggestion| suggestion.booking.id.0 == 7)
            .unwrap()
            .keyword,
        "Handwerker"
    );
}

#[test]
fn find_matches_a_keyword_only_at_the_start_of_a_word() {
    let txns = items_of(vec![
        item(1, -3_000).partner("Beispiel Kita-Beitrag"),
        item(2, -3_000).date(date!(2026 - 06 - 02)).partner("Akita Import"),
        item(3, -3_000).date(date!(2026 - 06 - 03)).purpose("SPENDEN Aktion"),
        item(4, -3_000).date(date!(2026 - 06 - 04)).purpose("Gegenspende"),
    ]);
    let suggestions = suggestions::find(&txns, &["Kita".to_owned(), "Spende".to_owned()]);
    assert_eq!(
        suggestions
            .iter()
            .map(|suggestion| suggestion.booking.id.0)
            .collect::<Vec<_>>(),
        vec![3, 1],
        "word starts match in any case; inside a word never"
    );
}

#[test]
fn find_ignores_blank_and_short_keywords_and_needs_a_keyword_of_at_least_three_characters() {
    let txns = items_of(vec![item(1, -3_000).partner("a b Kitax t").purpose("relevant later")]);
    let suggestions = suggestions::find(
        &txns,
        &["  ".to_owned(), String::new(), "ab".to_owned(), "Kitax".to_owned()],
    );
    assert_eq!(
        suggestions
            .iter()
            .map(|suggestion| suggestion.booking.id.0)
            .collect::<Vec<_>>(),
        vec![1],
        "'Kitax' matches, blanks and sub-3-char words are ignored"
    );
    assert!(
        suggestions::find(
            &items_of(vec![item(1, -3_000).partner("a b c")]),
            &["  ".to_owned(), String::new(), "ab".to_owned()]
        )
        .is_empty(),
        "no usable keyword, no suggestion"
    );
}

#[test]
fn keywords_returns_the_defaults_without_the_setting_and_the_stored_list_with_it() {
    let (_temp, db) = database();
    let defaults: Vec<String> = suggestions::DEFAULT_KEYWORDS
        .iter()
        .map(|keyword| (*keyword).to_owned())
        .collect();
    assert_eq!(suggestions::keywords(&db).unwrap(), defaults);

    settings::set_setting(&db, suggestions::KEY_KEYWORDS, "Steuerberater\n\n Küche \nKita").unwrap();
    assert_eq!(
        suggestions::keywords(&db).unwrap(),
        vec!["Steuerberater".to_owned(), "Küche".to_owned(), "Kita".to_owned()],
        "trimmed, blank lines dropped, one per line"
    );
    assert!(!suggestions::keywords(&db).unwrap().contains(&"Handwerker".to_owned()));

    let stored = suggestions::keywords(&db).unwrap();
    let suggestions = suggestions::find(
        &items_of(vec![
            item(1, -3_000).partner("Spendenbeispiel"),
            item(2, -900).partner("Bäckerei Beispiel"),
        ]),
        &stored,
    );
    assert!(
        suggestions.is_empty(),
        "no stored keyword matches the Spende partner any more"
    );
}

// Helpers for the exporter tests.
fn exporter_item(
    id: i64,
    minor: i64,
    day: Date,
    category: (i64, &str),
    partner: Option<&str>,
    purpose: Option<&str>,
    documents: usize,
) -> TransactionItem {
    let (category_id, category_name) = category;
    let mut item = item(id, minor)
        .date(day)
        .category(category_id, category_name, "#9C27B0")
        .tax_relevant();
    if let Some(partner) = partner {
        item = item.partner(partner);
    }
    if let Some(purpose) = purpose {
        item = item.purpose(purpose);
    }
    item.documents(documents).build()
}

fn group(category_id: i64, name: &str, bookings: Vec<TransactionItem>, income: bool) -> TaxGroup {
    let income_minor: i64 = if income {
        bookings.iter().map(|booking| booking.amount.minor).sum()
    } else {
        0
    };
    let expenses_minor: i64 = if income {
        0
    } else {
        bookings
            .iter()
            .map(|booking| i64::try_from(booking.amount.minor.unsigned_abs()).unwrap_or(i64::MAX))
            .sum()
    };
    TaxGroup {
        category_id: Some(CategoryId(category_id)),
        name: name.to_owned(),
        color: "#9C27B0".to_owned(),
        bookings,
        income: Money {
            minor: income_minor,
            currency: Currency::EUR,
        },
        expenses: Money {
            minor: expenses_minor,
            currency: Currency::EUR,
        },
    }
}

fn exporter_bookings(
    income_count: i64,
    deduction_count: i64,
    long_description: bool,
    unencodable_partner: bool,
    other_currency: usize,
) -> TaxBookings {
    let day = date!(2026 - 06 - 01);
    let salary = (1..=income_count)
        .map(|id| {
            exporter_item(
                id,
                120_000,
                day - Duration::days(income_count - id),
                (10, "Gehalt"),
                None,
                Some(&format!("Gehalt {id}")),
                0,
            )
        })
        .collect::<Vec<_>>();
    let royalties = (1..=income_count)
        .map(|id| {
            exporter_item(
                100 + id,
                4_550,
                day - Duration::days(id),
                (11, "Tantiemen"),
                None,
                Some(&format!("Auszahlung {id}")),
                0,
            )
        })
        .collect::<Vec<_>>();
    let donations = (1..=deduction_count)
        .map(|id| {
            exporter_item(
                200 + id,
                -3_000,
                day - Duration::days(id),
                (20, "Spenden"),
                Some(if unencodable_partner && id == 1 {
                    "Beispielhilfe ✓ e. V."
                } else {
                    "Beispielhilfe e. V."
                }),
                Some(&format!("Spende {id}")),
                usize::from(id == 1),
            )
        })
        .collect::<Vec<_>>();
    let craft = (1..=deduction_count)
        .map(|id| {
            exporter_item(
                300 + id,
                -8_520,
                day - Duration::days(deduction_count - id),
                (21, "Handwerk"),
                Some(if long_description {
                    "Beispielhaft lang benannte Handwerksfirma mit sehr ausführlichem Namen und Ort"
                } else {
                    "Beispiel Werk GmbH"
                }),
                Some(&format!("Renovierung {id}")),
                0,
            )
        })
        .collect::<Vec<_>>();
    let booking_count = salary.len() + royalties.len() + donations.len() + craft.len();
    let mut all = salary
        .iter()
        .chain(&royalties)
        .chain(&donations)
        .chain(&craft)
        .cloned()
        .collect::<Vec<_>>();
    for index in 0..other_currency {
        all.push(
            exporter_item(
                1_000 + i64::try_from(index).unwrap(),
                -1_000,
                day,
                (20, "Spenden"),
                Some("Beispielhilfe e. V."),
                Some("Spende"),
                0,
            )
            .with_currency(Currency::new("USD").unwrap()),
        );
    }

    TaxBookings {
        from: date!(2026 - 01 - 01),
        to: date!(2026 - 12 - 31),
        currency: Currency::EUR,
        income_groups: vec![
            group(10, "Gehalt", salary, true),
            group(11, "Tantiemen", royalties, true),
        ],
        deduction_groups: vec![
            group(20, "Spenden", donations, false),
            group(21, "Handwerk", craft, false),
        ],
        booking_count,
        other_currency_bookings: other_currency,
    }
}

trait WithCurrency {
    fn with_currency(self, currency: Currency) -> Self;
}

impl WithCurrency for TransactionItem {
    fn with_currency(mut self, currency: Currency) -> Self {
        self.amount.currency = currency;
        self.currency = currency;
        self
    }
}

fn paypal_export_bookings() -> TaxBookings {
    let paypal = exporter_item(
        1,
        -1_999,
        date!(2026 - 06 - 01),
        (20, "Spenden"),
        Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
        Some("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
        0,
    );
    let plain = exporter_item(
        2,
        -450,
        date!(2026 - 06 - 01),
        (20, "Spenden"),
        Some("Beispiel Bäckerei"),
        Some("Brötchen"),
        0,
    );
    TaxBookings {
        from: date!(2026 - 01 - 01),
        to: date!(2026 - 12 - 31),
        currency: Currency::EUR,
        income_groups: Vec::new(),
        deduction_groups: vec![group(20, "Spenden", vec![paypal, plain], false)],
        booking_count: 2,
        other_currency_bookings: 0,
    }
}

// ---------------------------------------------------------------------------------------------
// TaxReportExporterTest
// ---------------------------------------------------------------------------------------------

fn zip_entry(bytes: &[u8], name: &str) -> Vec<u8> {
    let mut archive = ZipArchive::new(Cursor::new(bytes)).unwrap();
    let mut entry = archive.by_name(name).unwrap();
    let mut content = Vec::new();
    entry.read_to_end(&mut content).unwrap();
    content
}

fn xml_elements<'a, 'input>(node: Node<'a, 'input>) -> Vec<Node<'a, 'input>> {
    node.descendants().filter(|child| child.is_element()).collect()
}

fn cell_node<'a, 'input>(sheet: &'a Document<'input>, reference: &str) -> Node<'a, 'input> {
    xml_elements(sheet.root_element())
        .into_iter()
        .find(|element| element.tag_name().name() == "c" && element.attribute("r") == Some(reference))
        .expect("the cell exists")
}

fn cell_text(sheet: &Document<'_>, reference: &str) -> String {
    cell_node(sheet, reference)
        .descendants()
        .find(|child| child.tag_name().name() == "t")
        .map(|text| text.text().unwrap_or("").to_owned())
        .unwrap_or_default()
}

fn cell_value(sheet: &Document<'_>, reference: &str) -> String {
    cell_node(sheet, reference)
        .descendants()
        .find(|child| child.tag_name().name() == "v")
        .map(|text| text.text().unwrap_or("").to_owned())
        .unwrap_or_default()
}

fn cell_style(sheet: &Document<'_>, reference: &str) -> String {
    cell_node(sheet, reference)
        .attribute("s")
        .unwrap_or_default()
        .to_owned()
}

fn date_cell(sheet: &Document<'_>, reference: &str) -> String {
    let serial = cell_value(sheet, reference).parse::<i64>().unwrap();
    (date!(1899 - 12 - 30) + Duration::days(serial)).to_string()
}

#[test]
fn excel_has_one_overview_row_per_group_plus_totals_and_one_bookings_row_per_booking() {
    let info = exporter_bookings(2, 3, false, false, 2);
    let (temp, _db) = database();
    let file = temp.path().join("tax.xlsx");
    TaxReportExporter.export_xlsx(&info, &file, Language::En).unwrap();

    let bytes = std::fs::read(&file).unwrap();
    let rows = first_sheet_rows(&bytes).unwrap();
    assert_eq!(rows[0][0], "Tax-relevant bookings 2026");
    assert_eq!(rows[1][0], "Currency: EUR");
    assert_eq!(
        rows[2],
        vec![
            "Section",
            "Category",
            "Bookings",
            "Income",
            "Expenses",
            "Net",
            "With receipt"
        ]
    );
    assert_eq!(rows[3], vec!["Income", "Gehalt", "2", "2400", "0", "2400", "0"]);
    assert_eq!(rows[4], vec!["Income", "Tantiemen", "2", "91", "0", "91", "0"]);
    assert_eq!(rows[5], vec!["Deductions", "Spenden", "3", "0", "90", "-90", "1"]);
    assert_eq!(
        rows[6],
        vec!["Deductions", "Handwerk", "3", "0", "255.6", "-255.6", "0"]
    );
    assert_eq!(rows[7][1], "Tax-relevant income");
    assert_eq!(rows[7][3], "2491");
    assert_eq!(rows[8][1], "Deductions");
    assert_eq!(rows[8][4], "345.6");
    assert_eq!(rows[9][1], "Net income");
    assert_eq!(rows[9][5], "2145.4");
    assert_eq!(rows[10][1], "Receipts");
    assert_eq!(rows[10][2], "1 of 10");
    assert!(
        rows[11][0].starts_with("All amounts in EUR"),
        "the other-currency hint: {}",
        rows[11][0]
    );

    let sheet_bytes = zip_entry(&bytes, "xl/worksheets/sheet2.xml");
    let sheet = Document::parse(std::str::from_utf8(&sheet_bytes).unwrap()).unwrap();
    let sheet_rows: Vec<_> = xml_elements(sheet.root_element())
        .into_iter()
        .filter(|element| element.tag_name().name() == "row")
        .collect();
    assert_eq!(sheet_rows.len(), 1 + info.booking_count);
    assert_eq!(
        (b'A'..=b'K')
            .map(|column| cell_text(&sheet, &format!("{}{}", char::from(column), "1")))
            .collect::<Vec<_>>(),
        vec![
            "Section", "Category", "Date", "Partner", "Shop", "Purpose", "Account", "Amount", "Currency", "Receipt",
            "Source"
        ],
        "the Shop column sits directly after the Partner"
    );
    let first_donation = 2 + 4;
    assert_eq!(cell_text(&sheet, &format!("A{first_donation}")), "Deductions");
    assert_eq!(cell_text(&sheet, &format!("B{first_donation}")), "Spenden");
    assert_eq!(
        date_cell(&sheet, &format!("C{first_donation}")),
        "2026-05-31",
        "the date is a real date cell"
    );
    assert_eq!(
        cell_style(&sheet, &format!("C{first_donation}")),
        "3",
        "the date format"
    );
    assert_eq!(cell_text(&sheet, &format!("D{first_donation}")), "Beispielhilfe e. V.");
    assert_eq!(
        cell_text(&sheet, &format!("F{first_donation}")),
        "Spende 1",
        "the raw purpose keeps its column"
    );
    assert_eq!(
        cell_value(&sheet, &format!("H{first_donation}")),
        "-30.00",
        "signed amount as a number"
    );
    assert_eq!(
        cell_style(&sheet, &format!("H{first_donation}")),
        "2",
        "the money format"
    );
    assert_eq!(cell_text(&sheet, &format!("I{first_donation}")), "EUR");
    assert_eq!(
        cell_text(&sheet, &format!("J{first_donation}")),
        "Yes",
        "the first donation carries a receipt"
    );
    assert_eq!(
        cell_text(&sheet, &format!("G{first_donation}")),
        "DE89370400440532013000",
        "the full IBAN as text"
    );
    assert!(
        cell_text(&sheet, &format!("K{first_donation}")).contains("from category"),
        "the source names the deciding layer"
    );
    let last = sheet_rows.len();
    assert_eq!(
        cell_text(&sheet, &format!("A{last}")),
        "Deductions",
        "the deductions come after the income"
    );
    let filter = xml_elements(sheet.root_element())
        .into_iter()
        .find(|element| element.tag_name().name() == "autoFilter")
        .unwrap();
    let expected = format!("A1:K{last}");
    assert_eq!(
        filter.attribute("ref"),
        Some(expected.as_str()),
        "the filter spans header and data"
    );
    let pane = xml_elements(sheet.root_element())
        .into_iter()
        .find(|element| element.tag_name().name() == "pane")
        .unwrap();
    assert_eq!(pane.attribute("state"), Some("frozen"));
}

#[test]
fn excel_shows_the_shop_after_the_partner_and_keeps_the_raw_partner() {
    let info = paypal_export_bookings();
    let (temp, _db) = database();
    let file = temp.path().join("tax-shop.xlsx");
    TaxReportExporter.export_xlsx(&info, &file, Language::En).unwrap();

    let bytes = std::fs::read(&file).unwrap();
    let sheet_bytes = zip_entry(&bytes, "xl/worksheets/sheet2.xml");
    let sheet = Document::parse(std::str::from_utf8(&sheet_bytes).unwrap()).unwrap();
    assert_eq!(
        cell_text(&sheet, "D2"),
        "PayPal Europe S.a.r.l. et Cie S.C.A",
        "the raw partner"
    );
    assert_eq!(
        cell_text(&sheet, "E2"),
        "Beispiel Shop GmbH",
        "the shop follows the partner"
    );
    assert_eq!(cell_text(&sheet, "E3"), "", "a non-PayPal booking has an empty shop");
    let filter = xml_elements(sheet.root_element())
        .into_iter()
        .find(|element| element.tag_name().name() == "autoFilter")
        .unwrap();
    assert_eq!(filter.attribute("ref"), Some("A1:K3"));
}

#[test]
fn pdf_titles_a_paypal_booking_with_the_provider_and_the_shop() {
    let info = paypal_export_bookings();
    let bytes = TaxReportExporter.build_pdf(&info, date!(2026 - 11 - 07), Language::En);
    assert_pdf(&bytes);
    assert!(
        pdf_contains(&bytes, "PayPal · Beispiel Shop GmbH"),
        "the WinAnsi periodcentered survives"
    );
}

#[test]
fn excel_sheet_names_are_localized_in_german() {
    let info = exporter_bookings(1, 1, false, false, 0);
    let (temp, _db) = database();
    let file = temp.path().join("tax-de.xlsx");
    TaxReportExporter.export_xlsx(&info, &file, Language::De).unwrap();

    let bytes = std::fs::read(&file).unwrap();
    let workbook_bytes = zip_entry(&bytes, "xl/workbook.xml");
    let workbook = Document::parse(std::str::from_utf8(&workbook_bytes).unwrap()).unwrap();
    let names: Vec<String> = xml_elements(workbook.root_element())
        .into_iter()
        .filter(|element| element.tag_name().name() == "sheet")
        .filter_map(|element| element.attribute("name").map(str::to_owned))
        .collect();
    assert_eq!(names, vec!["Übersicht", "Buchungen"]);
    assert_eq!(
        first_sheet_rows(&bytes).unwrap()[0][0],
        "Steuerrelevante Buchungen 2026"
    );
}

#[test]
fn pdf_paginates_with_footers_sections_and_fitted_descriptions() {
    let info = exporter_bookings(40, 55, true, true, 0);
    let bytes = TaxReportExporter.build_pdf(&info, date!(2026 - 11 - 07), Language::En);
    let pages = pdf_page_count(&bytes);
    assert!(pages >= 3, "190 bookings fill several pages, was {pages}");

    assert!(pdf_contains(&bytes, "Tax-relevant bookings 2026"), "the title");
    assert!(pdf_contains(&bytes, "Income"));
    assert!(pdf_contains(&bytes, "Deductions"));
    for group in info.income_groups.iter().chain(&info.deduction_groups) {
        assert!(pdf_contains(&bytes, &group.name), "every group header: {}", group.name);
    }
    for page in 1..=pages {
        assert!(
            pdf_contains(&bytes, &format!("Page {page} of {pages}")),
            "the footer of page {page}"
        );
    }
    assert!(
        pdf_contains(&bytes, "…"),
        "the long description is cut with an ellipsis"
    );
    assert!(
        pdf_contains(&bytes, "Beispielhilfe ? e. V."),
        "characters outside WinAnsi become ? without throwing"
    );
}

#[test]
fn export_pdf_writes_a_loadable_file_with_the_same_page_count() {
    let info = exporter_bookings(30, 30, false, false, 0);
    let (temp, _db) = database();
    let file = temp.path().join("tax.pdf");
    let pages = pdf_page_count(&TaxReportExporter.build_pdf(&info, date!(2026 - 11 - 07), Language::En));
    TaxReportExporter
        .export_pdf(&info, &file, date!(2026 - 11 - 07), Language::En)
        .unwrap();

    let bytes = std::fs::read(&file).unwrap();
    assert_pdf(&bytes);
    assert_eq!(pdf_page_count(&bytes), pages);
}

// ---------------------------------------------------------------------------------------------
// PdfReportExporterTitleTest
// ---------------------------------------------------------------------------------------------

#[test]
fn pdf_titles_a_tax_relevant_paypal_booking_with_the_shop() {
    let (_temp, db) = database();
    let account = add_account(&db, Currency::EUR);
    let online = add_category(&db, "Online shopping", "#FF5722", true, false);
    add_transaction(
        &db,
        account,
        date!(2026 - 06 - 15),
        eur(-25_000),
        Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
        Some("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
        Some(online),
    );
    let report = generate_tax_report(&db, 2026, Language::En).unwrap();
    let (temp, _db2) = database();
    let file = temp.path().join("tax-title.pdf");
    PdfReportExporter
        .export(&report, &file, date!(2026 - 11 - 07), Language::En)
        .unwrap();

    let bytes = std::fs::read(&file).unwrap();
    assert_pdf(&bytes);
    assert!(
        pdf_contains(&bytes, "PayPal · Beispiel Shop GmbH"),
        "the tax booking row uses the display title"
    );
}

#[test]
fn pdf_report_contains_the_title_and_sanitized_text() {
    let txns = items_of(vec![
        item(1, -3_000)
            .category(20, "Spenden", "")
            .tax_relevant()
            .partner("Beispielhilfe ✓ e. V.")
            .purpose("Spende"),
    ]);
    let report = build_report(
        date!(2026 - 01 - 01),
        date!(2026 - 12 - 31),
        &txns,
        Currency::EUR,
        Language::En,
    );
    let bytes = PdfReportExporter.build_pdf(&report, date!(2026 - 11 - 07), Language::En);
    assert_pdf(&bytes);
    assert!(pdf_contains(&bytes, "Finglass Tax Report 2026"));
    assert!(pdf_contains(&bytes, "Beispielhilfe ? e. V."));
}

// ---------------------------------------------------------------------------------------------
// CsvOwnExportReimportTest
// ---------------------------------------------------------------------------------------------

fn round_trip_bookings() -> Vec<TransactionItem> {
    vec![
        round_trip_item(1, date!(2026 - 10 - 01), 260_000, "Beispiel AG", "Gehalt Oktober"),
        round_trip_item(2, date!(2026 - 10 - 02), -4_250, "Beispiel Markt", "Einkauf 4711"),
        round_trip_item(
            3,
            date!(2026 - 10 - 03),
            -725,
            "Beispiel Laden",
            "Einkauf \"Spezial\", Teil 2",
        ),
    ]
}

fn round_trip_item(id: i64, day: Date, minor: i64, partner: &str, purpose: &str) -> TransactionItem {
    let mut transaction = item(id, minor).date(day).partner(partner).purpose(purpose).build();
    transaction.account_iban = Some("DE89370400440532013000".to_owned());
    transaction
}

fn decimal_of(minor: i64) -> Decimal {
    Decimal::from_str(&format_plain(eur(minor))).unwrap()
}

#[test]
fn own_export_round_trips_in_every_language() {
    let bookings = round_trip_bookings();
    for language in Language::ALL {
        let (temp, _db) = database();
        let file = temp.path().join(format!("own-{}.csv", language.code()));
        export_to_csv(&bookings, &file, None, *language).unwrap();
        let parsed = import_csv::parse(&std::fs::read(&file).unwrap(), None).unwrap();

        assert_eq!(parsed.bookings.len(), 3, "{}: {:?}", language.code(), parsed.skipped);
        assert!(parsed.skipped.is_empty(), "{}: {:?}", language.code(), parsed.skipped);
        for (exported, reimported) in bookings.iter().zip(&parsed.bookings) {
            assert_eq!(exported.date, reimported.date, "{} date", language.code());
            assert_eq!(
                decimal_of(exported.amount.minor),
                reimported.amount,
                "{} amount",
                language.code()
            );
            assert_eq!(
                Some(exported.currency.as_str()),
                reimported.currency.as_deref(),
                "{} currency",
                language.code()
            );
            assert_eq!(
                exported.partner_name.as_deref(),
                reimported.partner_name.as_deref(),
                "{} partner",
                language.code()
            );
            assert_eq!(
                exported.purpose.as_deref(),
                reimported.purpose.as_deref(),
                "{} purpose",
                language.code()
            );
        }
    }
}

#[test]
fn own_export_round_trips_the_balance_column_in_every_language() {
    let bookings = round_trip_bookings();
    let balances = BTreeMap::from([
        (bookings[0].id, eur(260_000)),
        (bookings[1].id, eur(255_750)),
        (bookings[2].id, eur(255_025)),
    ]);
    for language in Language::ALL {
        let (temp, _db) = database();
        let file = temp.path().join(format!("own-balances-{}.csv", language.code()));
        export_to_csv(&bookings, &file, Some(&balances), *language).unwrap();
        let parsed = import_csv::parse(&std::fs::read(&file).unwrap(), None).unwrap();

        assert_eq!(parsed.bookings.len(), 3, "{}: {:?}", language.code(), parsed.skipped);
        for (exported, reimported) in bookings.iter().zip(&parsed.bookings) {
            let parsed_balance = reimported.balance_after.as_ref().expect("the balance was parsed");
            assert_eq!(
                decimal_of(balances.get(&exported.id).unwrap().minor),
                *parsed_balance,
                "{} balance",
                language.code()
            );
        }
    }
}

#[test]
fn own_export_round_trips_text_that_the_spreadsheet_guard_prefixes() {
    let guarded = vec![
        round_trip_item(4, date!(2026 - 10 - 04), -100, "+Shop", "- Rate 3/12"),
        round_trip_item(5, date!(2026 - 10 - 05), -200, "@home", "=SUM(A1)"),
    ];
    let (temp, _db) = database();
    let file = temp.path().join("own-guarded.csv");
    export_to_csv(&guarded, &file, None, Language::En).unwrap();
    let parsed = import_csv::parse(&std::fs::read(&file).unwrap(), None).unwrap();

    assert_eq!(parsed.bookings.len(), 2, "{:?}", parsed.skipped);
    for (exported, reimported) in guarded.iter().zip(&parsed.bookings) {
        assert_eq!(exported.partner_name.as_deref(), reimported.partner_name.as_deref());
        assert_eq!(exported.purpose.as_deref(), reimported.purpose.as_deref());
    }
}

#[test]
fn only_the_spreadsheet_guard_apostrophe_is_removed_from_text() {
    let csv = "Datum;Betrag;Partner;Verwendungszweck\n03.10.2026;-1,00;'abc;'abc\n";
    let parsed = import_csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(parsed.bookings.len(), 1);
    assert_eq!(parsed.bookings[0].partner_name.as_deref(), Some("'abc"));
    assert_eq!(parsed.bookings[0].purpose.as_deref(), Some("'abc"));
}

#[test]
fn bank_headers_keep_their_mapping() {
    let mapping = import_csv::detect_mapping(&[
        "Buchungsdatum".to_owned(),
        "Wertstellung".to_owned(),
        "Status".to_owned(),
        "Zahlungspflichtige*r".to_owned(),
        "Zahlungsempfänger*in".to_owned(),
        "Verwendungszweck".to_owned(),
        "Umsatztyp".to_owned(),
        "IBAN".to_owned(),
        "Betrag (€)".to_owned(),
        "Gläubiger-ID".to_owned(),
        "Mandatsreferenz".to_owned(),
        "Kundenreferenz".to_owned(),
    ]);
    assert_eq!(mapping.date, Some(0));
    assert_eq!(mapping.value_date, Some(1));
    assert_eq!(mapping.amount, Some(8));
    assert_eq!(mapping.partner, Some(4));
    assert_eq!(mapping.payer, Some(3));
    assert_eq!(mapping.partner_iban, Some(7));
    assert_eq!(mapping.purpose, Some(5));

    let mapping = import_csv::detect_mapping(&[
        "Datum".to_owned(),
        "Name".to_owned(),
        "Partner".to_owned(),
        "Verwendungszweck".to_owned(),
        "Betrag".to_owned(),
    ]);
    assert_eq!(mapping.date, Some(0));
    assert_eq!(mapping.amount, Some(4));
    assert_eq!(mapping.partner, Some(1));
    assert_eq!(mapping.purpose, Some(3));

    let mapping = import_csv::detect_mapping(&[
        "Date".to_owned(),
        "Payee".to_owned(),
        "Purpose".to_owned(),
        "Amount".to_owned(),
        "Currency".to_owned(),
        "Partner".to_owned(),
    ]);
    assert_eq!(mapping.date, Some(0));
    assert_eq!(mapping.amount, Some(3));
    assert_eq!(mapping.partner, Some(1));
    assert_eq!(mapping.purpose, Some(2));
    assert_eq!(mapping.currency, Some(4));
}

#[test]
fn fallbacks_are_exact_match_only() {
    assert_eq!(
        import_csv::detect_mapping(&["Datum".to_owned(), "Partnerbank".to_owned(), "Betrag".to_owned()]).partner,
        None
    );
    assert_eq!(
        import_csv::detect_mapping(&[
            "Date".to_owned(),
            "Montants".to_owned(),
            "Libellé".to_owned(),
            "Amount".to_owned(),
        ])
        .amount,
        Some(3)
    );
}

#[test]
fn bank_norwegian_rule_still_clears_the_currency() {
    let mapping = import_csv::detect_mapping(&[
        "BookDate".to_owned(),
        "Amount".to_owned(),
        "Currency Amount".to_owned(),
        "Currency".to_owned(),
        "Devise".to_owned(),
    ]);
    assert_eq!(mapping.currency, None);
}

// ---------------------------------------------------------------------------------------------
// PDF test helpers
// ---------------------------------------------------------------------------------------------

fn assert_pdf(bytes: &[u8]) {
    assert!(bytes.starts_with(b"%PDF-"), "a PDF header");
    let end = bytes.strip_suffix(b"\n").unwrap_or(bytes);
    assert!(end.ends_with(b"%%EOF"), "a PDF trailer");
}

fn pdf_contains(bytes: &[u8], text: &str) -> bool {
    pdf_string_text(bytes).contains(text)
}

/// Decodes literal and hex strings in the (uncompressed) PDF content streams.
fn pdf_string_text(bytes: &[u8]) -> String {
    let mut out = String::new();
    let mut index = 0;
    while index < bytes.len() {
        match bytes[index] {
            b'(' => {
                index += 1;
                let mut literal = Vec::new();
                while index < bytes.len() {
                    match bytes[index] {
                        b')' => {
                            index += 1;
                            break;
                        }
                        b'\\' => {
                            index += 1;
                            if index >= bytes.len() {
                                break;
                            }
                            match bytes[index] {
                                b'n' => literal.push(b'\n'),
                                b'r' => literal.push(b'\r'),
                                b't' => literal.push(b'\t'),
                                b'b' => literal.push(0x08),
                                b'f' => literal.push(0x0c),
                                b'0'..=b'7' => {
                                    let mut value = 0u16;
                                    let mut digits = 0;
                                    while digits < 3 && index < bytes.len() && (b'0'..=b'7').contains(&bytes[index]) {
                                        value = value * 8 + u16::from(bytes[index] - b'0');
                                        index += 1;
                                        digits += 1;
                                    }
                                    literal.push(u8::try_from(value & 0xff).unwrap_or(0));
                                    continue;
                                }
                                other => literal.push(other),
                            }
                        }
                        byte => literal.push(byte),
                    }
                    index += 1;
                }
                out.push_str(&text_from_winansi(&literal));
            }
            b'<' if bytes.get(index + 1) != Some(&b'<') => {
                index += 1;
                let mut digits = Vec::new();
                while index < bytes.len() && bytes[index] != b'>' {
                    if bytes[index].is_ascii_hexdigit() {
                        digits.push(bytes[index]);
                    }
                    index += 1;
                }
                if digits.len() % 2 == 1 {
                    digits.push(b'0');
                }
                let decoded: Vec<u8> = digits
                    .chunks(2)
                    .filter_map(|pair| {
                        let text = std::str::from_utf8(pair).ok()?;
                        u8::from_str_radix(text, 16).ok()
                    })
                    .collect();
                out.push_str(&text_from_winansi(&decoded));
                index += 1;
            }
            _ => index += 1,
        }
    }
    out
}

fn pdf_page_count(bytes: &[u8]) -> usize {
    fn count(haystack: &[u8], needle: &[u8]) -> usize {
        haystack
            .windows(needle.len())
            .filter(|window| *window == needle)
            .count()
    }
    count(bytes, b"/Type /Page") - count(bytes, b"/Type /Pages")
}
