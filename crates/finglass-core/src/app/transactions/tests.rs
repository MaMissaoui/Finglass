//! Ported Kotlin tests for FinDesk's `TransactionsViewModel`.
//!
//! The tests use temporary databases and folders only. Coroutine timing/race tests from Kotlin are listed as skipped
//! in the task report; every sequential behaviour test lives here.

use std::collections::BTreeSet;
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, Ordering};

use time::macros::{date, datetime};
use time::{Date, OffsetDateTime};

use super::*;
use crate::app::account_panel::{self, AccountSelection};
use crate::documents::{DocumentService, OcrService};
use crate::i18n::{self, Arg, Language};
use crate::model::{AccountType, NewAccount, NewCategory, NewTransaction, RuleDirection};
use crate::money::Currency;
use crate::repo;
use crate::secret::{Passphrase, PassphrasePurpose};

static COUNTER: AtomicUsize = AtomicUsize::new(0);

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

fn money(minor: i64, code: &str) -> Money {
    Money {
        minor,
        currency: Currency::new(code).unwrap(),
    }
}

fn account(db: &Database, iban: &str, balance_minor: i64, currency: Currency, account_type: &str) -> AccountId {
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: None,
            iban: Some(iban.to_owned()),
            account_number: None,
            currency,
            account_type: AccountType::from_stored(account_type).unwrap(),
            balance: Money {
                minor: balance_minor,
                currency,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

fn category(db: &Database, name: &str, tax_relevant: bool, is_transfer: bool) -> CategoryId {
    repo::categories::create(
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

macro_rules! txn {
    ($db:expr, $account:expr, $day:expr, $amount:expr, $partner:expr, $purpose:expr, $category:expr, $is_new:expr $(,)?) => {
        txn_impl($db, $account, $day, $amount, ($partner, $purpose), $category, $is_new)
    };
}

fn txn_impl(
    db: &Database,
    account_id: AccountId,
    day: Date,
    amount: Money,
    names: (Option<&str>, Option<&str>),
    category_id: Option<CategoryId>,
    is_new: bool,
) -> TransactionId {
    let unique = COUNTER.fetch_add(1, Ordering::SeqCst);
    let hash = format!("test-{}-{unique}", account_id.0);
    repo::transactions::insert_planned(
        db,
        account_id,
        vec![NewTransaction {
            booking_date: day,
            amount,
            balance_after: None,
            partner_name: names.0.map(str::to_owned),
            purpose: names.1.map(str::to_owned),
            category_id,
            import_hash: hash.clone(),
            created_at: OffsetDateTime::now_utc(),
            is_new,
            tax_override: None,
        }],
    )
    .unwrap();
    repo::transactions::list_for_account(db, account_id, None, None)
        .unwrap()
        .into_iter()
        .find(|transaction| transaction.import_hash == hash)
        .unwrap()
        .id
}

fn view() -> TransactionsView {
    TransactionsView::default()
}

fn start_all(db: &Database, vm: &mut TransactionsView, today: Date) {
    vm.start(
        db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
}

fn start_account(db: &Database, vm: &mut TransactionsView, account_id: AccountId, today: Date) {
    vm.start(
        db,
        StartArgs {
            account_id: Some(account_id),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
}

fn partners(vm: &TransactionsView) -> Vec<String> {
    vm.state()
        .transactions
        .iter()
        .map(|transaction| transaction.partner_name.clone().unwrap_or_default())
        .collect()
}

fn ids(vm: &TransactionsView) -> Vec<TransactionId> {
    vm.state()
        .transactions
        .iter()
        .map(|transaction| transaction.id)
        .collect()
}

fn actions(db: &Database) -> Vec<String> {
    crate::audit::recent(db, 1000)
        .unwrap()
        .into_iter()
        .map(|entry| entry.action)
        .collect()
}

fn count_action(db: &Database, action: &str) -> usize {
    actions(db).into_iter().filter(|name| name == action).count()
}

fn standard() -> (tempfile::TempDir, Database, AccountId, CategoryId, CategoryId) {
    let (temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 150_000, Currency::EUR, "checking");
    let food = category(&db, "Food", false, false);
    let tax = category(&db, "Spenden", true, false);
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 02),
        eur(-3_000),
        Some("REWE"),
        None,
        Some(food),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 05),
        eur(-2_000),
        Some("Kiosk"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 01),
        eur(250_000),
        Some("Employer"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 01 - 15),
        eur(-10_000),
        Some("UNICEF"),
        None,
        Some(tax),
        false,
    );
    (temp, db, account_id, food, tax)
}

// ---------------------------------------------------------------------------
// ViewModelsTest.kt
// ---------------------------------------------------------------------------

#[test]
fn transactions_filters_apply_and_category_filter_is_honoured() {
    let (_temp, db, _account, food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    assert_eq!(vm.state().transactions.len(), 3, "this month");

    vm.update_direction(&db, Direction::Expenses, today).unwrap();
    assert_eq!(vm.state().transactions.len(), 2);

    vm.update_category_filter(&db, CategoryFilter::Only(food), today)
        .unwrap();
    assert_eq!(partners(&vm), vec!["REWE".to_owned()]);

    vm.update_category_filter(&db, CategoryFilter::None, today).unwrap();
    assert_eq!(partners(&vm), vec!["Kiosk".to_owned()]);

    vm.update_category_filter(&db, CategoryFilter::Any, today).unwrap();
    vm.update_period(&db, Period::ThisYear, today).unwrap();
    vm.update_search_query(&db, "unic", today).unwrap();
    assert_eq!(partners(&vm), vec!["UNICEF".to_owned()]);
}

#[test]
fn manual_transaction_gets_auto_categorized_and_an_identical_one_waits() {
    let (_temp, db, account_id, food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    repo::rules::create(
        &db,
        NewRule {
            pattern: "edeka".to_owned(),
            category_id: food,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
    let mut vm = view();
    vm.add_manual_transaction(
        &db,
        ManualTransaction {
            account_id,
            date: today,
            amount: eur(-1_234),
            partner_name: Some("EDEKA Center".to_owned()),
            purpose: None,
            category_id: None,
            confirmed_duplicate: false,
        },
        today,
    )
    .unwrap();
    let added = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| transaction.partner_name.as_deref() == Some("EDEKA Center"))
        .unwrap();
    assert_eq!(added.category_name.as_deref(), Some("Food"));

    vm.add_manual_transaction(
        &db,
        ManualTransaction {
            account_id,
            date: today,
            amount: eur(-1_234),
            partner_name: Some("EDEKA Center".to_owned()),
            purpose: None,
            category_id: None,
            confirmed_duplicate: false,
        },
        today,
    )
    .unwrap();
    assert_eq!(
        vm.state()
            .transactions
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("EDEKA Center"))
            .count(),
        1,
        "not added before confirming"
    );
    assert_eq!(vm.state().pending_duplicate.as_ref().unwrap().amount, eur(-1_234));

    vm.dismiss_duplicate();
    assert!(vm.state().pending_duplicate.is_none());
    assert_eq!(
        vm.state()
            .transactions
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("EDEKA Center"))
            .count(),
        1
    );
}

#[test]
fn a_manually_added_booking_is_never_marked_new() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.add_manual_transaction(
        &db,
        ManualTransaction {
            account_id,
            date: today,
            amount: eur(-1_234),
            partner_name: Some("EDEKA Center".to_owned()),
            purpose: None,
            category_id: None,
            confirmed_duplicate: false,
        },
        today,
    )
    .unwrap();
    assert!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .all(|transaction| !transaction.is_new)
    );
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
}

#[test]
fn a_confirmed_identical_manual_entry_is_added_as_a_second_booking() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    for _ in 0..2 {
        vm.add_manual_transaction(
            &db,
            ManualTransaction {
                account_id,
                date: today,
                amount: eur(-350),
                partner_name: Some("Café".to_owned()),
                purpose: Some("Coffee".to_owned()),
                category_id: None,
                confirmed_duplicate: false,
            },
            today,
        )
        .unwrap();
    }
    assert!(vm.confirm_duplicate(&db, today).unwrap());
    assert_eq!(
        vm.state()
            .transactions
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("Café"))
            .count(),
        2
    );
    assert!(vm.state().pending_duplicate.is_none());
    assert!(vm.state().error.is_none());
    let hashes: BTreeSet<String> = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
        .unwrap()
        .into_iter()
        .filter(|transaction| transaction.partner_name.as_deref() == Some("Café"))
        .map(|transaction| {
            repo::transactions::list_for_account(&db, transaction.account_id, None, None)
                .unwrap()
                .into_iter()
                .find(|row| row.id == transaction.id)
                .unwrap()
                .import_hash
        })
        .collect();
    assert_eq!(hashes.len(), 2, "each entry has its own hash");
    assert!(!vm.confirm_duplicate(&db, today).unwrap(), "nothing left to confirm");
}

#[test]
fn show_all_from_a_partner_clears_the_narrowing_filters() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    txn!(
        &db,
        other,
        date!(2025 - 11 - 03),
        eur(-1_200),
        Some("REWE"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.update_direction(&db, Direction::Income, today).unwrap();
    vm.update_account_filter(&db, Some(account_id), today).unwrap();
    vm.show_partner(&db, "REWE", today).unwrap();

    let state = vm.state();
    assert_eq!(
        state
            .transactions
            .iter()
            .map(|t| t.partner_name.clone())
            .collect::<Vec<_>>(),
        vec![Some("REWE".to_owned()), Some("REWE".to_owned())]
    );
    assert_eq!(state.period, Period::All);
    assert_eq!(state.direction, Direction::All);
    assert!(state.account_id.is_none());
}

#[test]
fn the_manual_duplicate_check_is_per_account() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    for (target, partner) in [(account_id, "Bäckerei"), (other, "Bäckerei")] {
        vm.add_manual_transaction(
            &db,
            ManualTransaction {
                account_id: target,
                date: today,
                amount: eur(-450),
                partner_name: Some(partner.to_owned()),
                purpose: Some("Brötchen".to_owned()),
                category_id: None,
                confirmed_duplicate: false,
            },
            today,
        )
        .unwrap();
    }
    assert!(vm.state().pending_duplicate.is_none());
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("Bäckerei"))
            .count(),
        2
    );

    vm.add_manual_transaction(
        &db,
        ManualTransaction {
            account_id,
            date: today,
            amount: eur(-450),
            partner_name: Some(" Bäckerei ".to_owned()),
            purpose: Some("Brötchen ".to_owned()),
            category_id: None,
            confirmed_duplicate: false,
        },
        today,
    )
    .unwrap();
    assert_eq!(vm.state().pending_duplicate.as_ref().unwrap().account_id, account_id);
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("Bäckerei"))
            .count(),
        2
    );
}

#[test]
fn statement_import_previews_picks_the_account_from_the_file_and_imports() {
    let (temp, db, _account_id, _food, _tax) = standard();
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    let file = temp.path().join("ing.csv");
    std::fs::write(
        &file,
        "IBAN;DE44 5001 0517 5407 3249 31\n\nBuchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n\
         02.10.2025;02.10.2025;Stadtwerke;Lastschrift;Strom;80,00;EUR;-20,00;EUR\n",
    )
    .unwrap();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.open_import(&db, &[file], None, today).unwrap();

    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(preview.account_id, Some(other), "matched by the IBAN in the file");
    assert_eq!(preview.entries[0].statement.bookings.len(), 1);
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("Stadtwerke"))
            .count(),
        0,
        "nothing stored before confirming"
    );

    vm.confirm_import(&db, true, today).unwrap();
    assert!(vm.state().import_preview.is_none());
    assert_eq!(vm.state().period, Period::All);
    assert_eq!(partners(&vm), vec!["Stadtwerke".to_owned()]);
    assert_eq!(repo::accounts::get(&db, other).unwrap().unwrap().balance, eur(8_000));
}

#[test]
fn running_balance_column_shows_for_one_checking_account_and_hides_for_all_or_a_depot() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert!(
        vm.state().balances.is_some(),
        "a single checking account shows the column"
    );
    assert_eq!(vm.state().current_balance, Some(eur(150_000)));

    vm.start(&db, StartArgs::default(), today).unwrap();
    assert!(vm.state().balances.is_none(), "All accounts never shows it");
    assert!(vm.state().current_balance.is_none());

    let depot = account(&db, "DE02120300000000202051", 500_000, Currency::EUR, "depot");
    start_account(&db, &mut vm, depot, today);
    assert!(vm.state().balances.is_none(), "a depot's balance is a market value");
}

#[test]
fn selecting_a_group_filters_to_its_accounts_and_an_empty_custom_group_shows_none() {
    let (_temp, db, _account_id, _food, _tax) = standard();
    let savings = account(&db, "DE44500105175407324931", 0, Currency::EUR, "savings");
    txn!(
        &db,
        savings,
        date!(2026 - 03 - 03),
        eur(-1_100),
        Some("Savings booking"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.update_period(&db, Period::All, today).unwrap();

    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("savings".to_owned())),
        today,
    )
    .unwrap();
    assert_eq!(partners(&vm), vec!["Savings booking".to_owned()]);

    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("checking".to_owned())),
        today,
    )
    .unwrap();
    assert_eq!(
        partners(&vm).into_iter().collect::<BTreeSet<_>>(),
        BTreeSet::from([
            "REWE".to_owned(),
            "Kiosk".to_owned(),
            "Employer".to_owned(),
            "UNICEF".to_owned()
        ])
    );

    let empty = repo::account_groups::create(
        &db,
        crate::model::NewAccountGroup {
            name: "Empty".to_owned(),
            sort_order: 10,
        },
    )
    .unwrap();
    vm.load(&db, today).unwrap();
    vm.select_accounts(&db, AccountSelection::Group(GroupKey::Custom(empty)), today)
        .unwrap();
    assert!(
        vm.state().transactions.is_empty(),
        "an empty custom group shows no bookings"
    );
}

#[test]
fn the_account_selection_persists_and_a_new_view_restores_it() {
    let (_temp, db, _account_id, _food, _tax) = standard();
    let savings = account(&db, "DE44500105175407324931", 0, Currency::EUR, "savings");
    txn!(
        &db,
        savings,
        date!(2026 - 03 - 03),
        eur(-1_100),
        Some("Savings booking"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("savings".to_owned())),
        today,
    )
    .unwrap();

    let mut restored = view();
    restored.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(
        restored.state().selection,
        AccountSelection::Group(GroupKey::Type("savings".to_owned()))
    );
    assert_eq!(partners(&restored), vec!["Savings booking".to_owned()]);
}

#[test]
fn the_panel_selection_is_shared_between_accounts_and_transactions() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let savings = account(&db, "DE44500105175407324931", 0, Currency::EUR, "savings");
    txn!(
        &db,
        savings,
        date!(2026 - 03 - 03),
        eur(-1_100),
        Some("Savings booking"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("savings".to_owned())),
        today,
    )
    .unwrap();

    let mut restored = view();
    restored.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(
        restored.state().selection,
        AccountSelection::Group(GroupKey::Type("savings".to_owned()))
    );
    restored
        .select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert_eq!(
        account_panel::load_selection(
            &db,
            &restored.state().groups,
            &grouping_accounts(&restored.state().accounts)
        )
        .unwrap(),
        AccountSelection::Account(account_id)
    );
}

#[test]
fn collapsed_groups_persist_and_a_refresh_never_undoes_a_toggle() {
    let (_temp, db, _account_id, _food, _tax) = standard();
    account(&db, "DE44500105175407324931", 0, Currency::EUR, "savings");
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.toggle_collapsed(&db, &GroupKey::Type("savings".to_owned())).unwrap();
    vm.load(&db, today).unwrap();
    assert_eq!(
        vm.state().collapsed,
        BTreeSet::from(["type:savings".to_owned()]),
        "a refresh keeps the toggled state"
    );

    let mut restored = view();
    restored.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(restored.state().collapsed, BTreeSet::from(["type:savings".to_owned()]));
    restored
        .toggle_collapsed(&db, &GroupKey::Type("savings".to_owned()))
        .unwrap();
    assert!(restored.state().collapsed.is_empty());
}

#[test]
fn a_stored_account_selection_whose_account_was_deleted_falls_back_to_all() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    repo::accounts::delete(&db, account_id).unwrap();

    let mut restored = view();
    restored.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(restored.state().selection, AccountSelection::All);
}

#[test]
fn start_with_an_account_overrides_the_stored_selection() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let savings = account(&db, "DE44500105175407324931", 0, Currency::EUR, "savings");
    txn!(
        &db,
        savings,
        date!(2026 - 03 - 03),
        eur(-1_100),
        Some("Savings booking"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("savings".to_owned())),
        today,
    )
    .unwrap();

    let mut restored = view();
    restored
        .start(
            &db,
            StartArgs {
                account_id: Some(account_id),
                ..StartArgs::default()
            },
            today,
        )
        .unwrap();
    assert_eq!(restored.state().selection, AccountSelection::Account(account_id));
    assert_eq!(restored.state().account_id, Some(account_id));
}

#[test]
fn the_balance_column_shows_for_an_account_selection_and_hides_for_a_group() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert!(vm.state().balances.is_some());

    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("checking".to_owned())),
        today,
    )
    .unwrap();
    assert!(vm.state().balances.is_none());
    assert!(vm.state().account_id.is_none());
}

#[test]
fn the_balance_chart_spans_the_shown_period_clamped_to_today() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    vm.update_period(&db, Period::ThisMonth, today).unwrap();

    let chart = vm.state().balance_chart.as_ref().expect("the chart shows");
    assert_eq!(chart.first().unwrap().0, date!(2026 - 03 - 01));
    assert_eq!(chart.last().unwrap().0, today);
    assert_eq!(chart.len(), 20);
    assert_eq!(chart.last().unwrap().1, eur(150_000));
}

#[test]
fn a_text_search_never_changes_the_balance_chart() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    vm.update_period(&db, Period::ThisMonth, today).unwrap();
    let before = vm.state().balance_chart.clone();
    assert!(before.is_some());

    vm.update_search_query(&db, "rewe", today).unwrap();
    assert_eq!(partners(&vm), vec!["REWE".to_owned()]);
    assert_eq!(vm.state().balance_chart, before);
}

#[test]
fn all_accounts_a_group_and_a_depot_show_no_balance_chart() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    assert!(vm.state().balance_chart.is_none(), "All accounts");

    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert!(vm.state().balance_chart.is_some(), "one non-depot account");

    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("checking".to_owned())),
        today,
    )
    .unwrap();
    assert!(vm.state().balance_chart.is_none(), "a group");

    let depot = account(&db, "DE02120300000000202051", 500_000, Currency::EUR, "depot");
    start_account(&db, &mut vm, depot, today);
    assert!(vm.state().balance_chart.is_none(), "a depot");
}

#[test]
fn the_collapsed_balance_chart_persists_and_a_refresh_never_undoes_a_toggle() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert!(!vm.state().chart_collapsed);

    vm.toggle_chart(&db).unwrap();
    assert!(vm.state().chart_collapsed);
    vm.load(&db, today).unwrap();
    assert!(vm.state().chart_collapsed, "a refresh keeps the toggled state");

    let mut restored = view();
    start_account(&db, &mut restored, account_id, today);
    assert!(restored.state().chart_collapsed, "the collapsed chart is restored");
    restored.toggle_chart(&db).unwrap();
    assert!(!restored.state().chart_collapsed);
}

#[test]
fn opening_an_account_clears_its_unread_count_but_keeps_its_bookings_bold() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let fresh = txn!(
        &db,
        account_id,
        date!(2026 - 03 - 03),
        eur(-1_200),
        Some("Fresh"),
        None,
        None,
        true,
    );
    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(account_id, 1)])
    );

    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert!(vm.state().bold_ids.contains(&fresh));
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
    assert_eq!(
        vm.state()
            .accounts
            .iter()
            .find(|account| account.id == account_id)
            .unwrap()
            .new_count,
        0
    );
    assert!(vm.state().error.is_none());
}

#[test]
fn a_new_booking_dated_last_month_stays_bold_after_opening_the_account() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let late = txn!(
        &db,
        account_id,
        date!(2026 - 02 - 27),
        eur(-800),
        Some("Late"),
        None,
        None,
        true,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert!(!ids(&vm).contains(&late), "this month's list doesn't show it");
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());

    vm.update_period(&db, Period::All, today).unwrap();
    assert!(ids(&vm).contains(&late));
    assert!(vm.state().bold_ids.contains(&late));
}

#[test]
fn selecting_another_account_replaces_the_bold_bookings() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    let first = txn!(
        &db,
        account_id,
        date!(2026 - 03 - 03),
        eur(-1_200),
        Some("Fresh"),
        None,
        None,
        true,
    );
    let second = txn!(
        &db,
        other,
        date!(2026 - 03 - 03),
        eur(-300),
        Some("OtherFresh"),
        None,
        None,
        true,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert_eq!(vm.state().bold_ids, BTreeSet::from([first]));

    vm.select_accounts(&db, AccountSelection::Account(other), today)
        .unwrap();
    assert_eq!(
        vm.state().bold_ids,
        BTreeSet::from([second]),
        "a changed selection replaces the bold set"
    );
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
}

#[test]
fn all_accounts_and_groups_never_clear_the_counter_automatically() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let first = txn!(
        &db,
        account_id,
        date!(2026 - 03 - 03),
        eur(-1_200),
        Some("Fresh"),
        None,
        None,
        true,
    );
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    let second = txn!(
        &db,
        other,
        date!(2026 - 03 - 03),
        eur(-300),
        Some("OtherFresh"),
        None,
        None,
        true,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.update_period(&db, Period::All, today).unwrap();

    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(account_id, 1), (other, 1)]),
        "the default All view clears nothing"
    );
    assert_eq!(vm.state().bold_ids, BTreeSet::from([first, second]));
    assert_eq!(
        vm.state().accounts.iter().map(|account| account.new_count).sum::<i64>(),
        2
    );

    vm.select_accounts(
        &db,
        AccountSelection::Group(GroupKey::Type("checking".to_owned())),
        today,
    )
    .unwrap();
    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(account_id, 1), (other, 1)]),
        "selecting a group clears nothing either"
    );
}

#[test]
fn mark_read_clears_a_groups_accounts_and_all_clears_everything() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let first = txn!(
        &db,
        account_id,
        date!(2026 - 03 - 03),
        eur(-1_200),
        Some("Fresh"),
        None,
        None,
        true,
    );
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    let second = txn!(
        &db,
        other,
        date!(2026 - 03 - 03),
        eur(-300),
        Some("OtherFresh"),
        None,
        None,
        true,
    );
    let savings = account(&db, "DE02120300000000202051", 0, Currency::EUR, "savings");
    txn!(
        &db,
        savings,
        date!(2026 - 03 - 04),
        eur(-100),
        Some("SavingsFresh"),
        None,
        None,
        true,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.update_period(&db, Period::All, today).unwrap();

    vm.mark_read(
        &db,
        AccountSelection::Group(GroupKey::Type("checking".to_owned())),
        today,
    )
    .unwrap();
    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(savings, 1)])
    );
    assert!(vm.state().bold_ids.contains(&first));
    assert!(vm.state().bold_ids.contains(&second));

    vm.mark_read(&db, AccountSelection::All, today).unwrap();
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
    assert!(vm.state().error.is_none());
}

#[test]
fn a_restored_account_selection_and_a_deep_link_clear_the_count_like_a_click() {
    let (_temp, db, account_id, _food, _tax) = standard();
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 03),
        eur(-1_200),
        Some("Fresh"),
        None,
        None,
        true,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());

    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 05),
        eur(-200),
        Some("Restored"),
        None,
        None,
        true,
    );
    let mut restored = view();
    restored.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(restored.state().selection, AccountSelection::Account(account_id));
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());

    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 06),
        eur(-200),
        Some("Linked"),
        None,
        None,
        true,
    );
    let mut linked = view();
    start_account(&db, &mut linked, account_id, today);
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
}

#[test]
fn deleting_the_selected_group_falls_back_to_all() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let group = repo::account_groups::create(
        &db,
        crate::model::NewAccountGroup {
            name: "Reisen".to_owned(),
            sort_order: 1,
        },
    )
    .unwrap();
    repo::accounts::set_group(&db, account_id, Some(group)).unwrap();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.select_accounts(&db, AccountSelection::Group(GroupKey::Custom(group)), today)
        .unwrap();
    assert_eq!(vm.state().selection, AccountSelection::Group(GroupKey::Custom(group)));

    vm.delete_group(&db, group, today).unwrap();
    assert_eq!(vm.state().selection, AccountSelection::All);
}

#[test]
fn group_actions_are_audited_and_a_blank_name_changes_nothing() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();

    vm.create_group(&db, "Reisen", today).unwrap();
    assert_eq!(count_action(&db, "account_group.create"), 1);
    assert_eq!(vm.state().custom_groups.len(), 1);
    let group = vm.state().custom_groups[0].id;

    vm.rename_group(&db, group, "Trips", today).unwrap();
    assert_eq!(count_action(&db, "account_group.rename"), 1);
    assert_eq!(vm.state().custom_groups[0].name, "Trips");

    vm.move_group(&db, group, true, today).unwrap();
    assert!(count_action(&db, "REORDER_ACCOUNT_GROUP") > 0);

    vm.move_account_to_group(&db, account_id, Some(group), today).unwrap();
    assert_eq!(count_action(&db, "account.set_group"), 1);
    assert_eq!(
        vm.state()
            .accounts
            .iter()
            .find(|account| account.id == account_id)
            .unwrap()
            .group_id,
        Some(group)
    );

    vm.create_group(&db, "   ", today).unwrap();
    assert_eq!(
        vm.state().error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorNameRequired))
    );
    assert_eq!(vm.state().custom_groups.len(), 1);
    assert_eq!(count_action(&db, "account_group.create"), 1);

    vm.delete_group(&db, group, today).unwrap();
    assert_eq!(count_action(&db, "account_group.delete"), 1);
}

#[test]
fn a_filter_hides_rows_but_the_remaining_balances_are_unchanged() {
    let (_temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    let all = vm.state().balances.clone().unwrap();
    let rewe = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| transaction.partner_name.as_deref() == Some("REWE"))
        .unwrap()
        .id;

    vm.update_search_query(&db, "rewe", today).unwrap();
    assert_eq!(vm.state().transactions.len(), 1);
    assert_eq!(vm.state().balances.as_ref().unwrap()[&rewe], all[&rewe]);
}

#[test]
fn export_csv_writes_the_balance_column_only_when_it_is_shown() {
    let (temp, db, account_id, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    let with_balance = temp.path().join("with-balance.csv");
    vm.export_csv(&db, &with_balance).unwrap();
    let first = std::fs::read_to_string(&with_balance)
        .unwrap()
        .lines()
        .next()
        .unwrap()
        .to_owned();
    assert!(first.ends_with(",Balance,Shop"), "{first}");

    let without_balance = temp.path().join("without-balance.csv");
    vm.start(&db, StartArgs::default(), today).unwrap();
    vm.export_csv(&db, &without_balance).unwrap();
    let first = std::fs::read_to_string(&without_balance)
        .unwrap()
        .lines()
        .next()
        .unwrap()
        .to_owned();
    assert!(!first.contains("Balance"), "hidden column stays out");
}

#[test]
fn amount_range_matches_absolute_values_in_both_directions() {
    let (_temp, db, _account, _food, _tax) = standard();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.update_period(&db, Period::ThisYear, today).unwrap();
    vm.update_amount_range(&db, Some(eur(2_500)), Some(eur(15_000)), today)
        .unwrap();
    assert_eq!(
        partners(&vm).into_iter().collect::<BTreeSet<_>>(),
        BTreeSet::from(["REWE".to_owned(), "UNICEF".to_owned()])
    );
    vm.update_amount_range(&db, Some(eur(100_000)), None, today).unwrap();
    assert_eq!(partners(&vm), vec!["Employer".to_owned()]);
}

#[test]
fn rule_from_a_transaction_escapes_regex_and_drops_reference_numbers() {
    let (_temp, db, account_id, food, _tax) = standard();
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 06),
        eur(-1_299),
        Some("NETFLIX.COM 4711-2026"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 02 - 06),
        eur(-1_299),
        Some("NETFLIX.COM 4711-2025"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    let netflix = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| {
            transaction
                .partner_name
                .as_deref()
                .is_some_and(|name| name.starts_with("NETFLIX"))
        })
        .unwrap();
    let pattern = vm.rule_pattern_for(netflix);
    assert_eq!(pattern, "NETFLIX\\.COM");

    vm.create_rule(&db, &pattern, food, today).unwrap();
    assert_eq!(
        vm.state().message.as_deref(),
        Some("Rule created and applied to 2 uncategorized transactions")
    );
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .filter(|transaction| {
                transaction
                    .partner_name
                    .as_deref()
                    .is_some_and(|name| name.starts_with("NETFLIX"))
                    && transaction.category_id == Some(food)
            })
            .count(),
        2
    );
}

#[test]
fn rule_from_a_paypal_transaction_suggests_the_shop() {
    let (_temp, db, account_id, _food, _tax) = standard();
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 06),
        eur(-1_299),
        Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
        Some("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
        None,
        false,
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    let paypal = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| {
            transaction
                .partner_name
                .as_deref()
                .is_some_and(|name| name.starts_with("PayPal"))
        })
        .unwrap();
    assert_eq!(vm.rule_pattern_for(paypal), "Beispiel Shop GmbH");
}

#[test]
fn receipts_attached_from_the_detail_panel_are_linked_to_the_transaction() {
    let (temp, db, _account, _food, _tax) = standard();
    let docs_dir = temp.path().join("docs");
    let service = DocumentService::new(&db, OcrService::without_ocr(), &docs_dir);
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    let rewe = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| transaction.partner_name.as_deref() == Some("REWE"))
        .unwrap()
        .clone();
    let png = temp.path().join("bon.png");
    std::fs::write(&png, b"not really a png").unwrap();

    vm.select_transaction(&db, &service, Some(rewe.clone())).unwrap();
    assert!(vm.state().selected_documents.is_empty());
    vm.attach_documents(&db, &service, rewe.id, &[png], today).unwrap();

    assert_eq!(
        vm.state()
            .selected_documents
            .iter()
            .map(|document| document.file_name.clone())
            .collect::<Vec<_>>(),
        vec!["bon.png".to_owned()]
    );
    assert_eq!(
        vm.state()
            .transactions
            .iter()
            .find(|transaction| transaction.id == rewe.id)
            .unwrap()
            .document_count,
        1
    );
    assert_eq!(vm.state().selected.as_ref().unwrap().document_count, 1);
}

#[test]
fn auto_categorize_applies_rules_to_uncategorized_transactions() {
    let (_temp, db, _account, food, _tax) = standard();
    repo::rules::create(
        &db,
        NewRule {
            pattern: "kiosk".to_owned(),
            category_id: food,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.auto_categorize(&db, today).unwrap();
    assert_eq!(
        vm.state().message.as_deref(),
        Some("Categorized 1 of 2 uncategorized transactions")
    );
}

// ---------------------------------------------------------------------------
// TransactionsViewModelPendingTest.kt
// ---------------------------------------------------------------------------

fn pending_item(account_id: AccountId) -> PendingBookingItem {
    PendingBookingItem {
        account_id,
        date: date!(2026 - 10 - 03),
        value_date: Some(date!(2026 - 10 - 02)),
        amount: eur(-4_980),
        partner_name: Some("Beispiel Tank GmbH".to_owned()),
        purpose: Some("Kraftstoff".to_owned()),
        fetched_at: datetime!(2026-10-04 09:30 UTC),
    }
}

#[test]
fn pending_rows_are_in_the_state_for_one_account_and_absent_for_all_accounts() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(-12_000),
        Some("Beispiel AG"),
        Some("Gehalt"),
        None,
        false,
    );
    repo::pending_bookings::replace_for_account(
        &db,
        account_id,
        vec![crate::model::NewPendingBooking {
            date: date!(2026 - 10 - 03),
            value_date: Some(date!(2026 - 10 - 02)),
            amount: eur(-4_980),
            partner_name: Some("Beispiel Tank GmbH".to_owned()),
            purpose: Some("Kraftstoff".to_owned()),
        }],
        datetime!(2026-10-04 09:30 UTC),
    )
    .unwrap();
    let today = date!(2026 - 10 - 04);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert_eq!(vm.state().pending_bookings, vec![pending_item(account_id)]);

    vm.select_accounts(&db, AccountSelection::All, today).unwrap();
    assert!(vm.state().pending_bookings.is_empty(), "all accounts shows none");
}

#[test]
fn pending_rows_stay_out_of_the_counted_bookings() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(-12_000),
        Some("Beispiel AG"),
        Some("Gehalt"),
        None,
        false,
    );
    repo::pending_bookings::replace_for_account(
        &db,
        account_id,
        vec![crate::model::NewPendingBooking {
            date: date!(2026 - 10 - 03),
            value_date: None,
            amount: eur(-4_980),
            partner_name: Some("Beispiel Tank GmbH".to_owned()),
            purpose: Some("Kraftstoff".to_owned()),
        }],
        datetime!(2026-10-04 09:30 UTC),
    )
    .unwrap();
    let today = date!(2026 - 10 - 04);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert_eq!(vm.state().transactions.len(), 1, "the list counts only the booking");
    assert_eq!(vm.state().pending_bookings.len(), 1);
    assert_eq!(vm.state().total_expenses, vec![(Currency::EUR, 12_000)]);
}

#[test]
fn pending_rows_show_only_unfiltered_for_a_period_that_includes_today() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    let category_id = category(&db, "Beispiel Haushalt", false, false);
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(-12_000),
        Some("Beispiel AG"),
        Some("Gehalt"),
        None,
        false,
    );
    repo::pending_bookings::replace_for_account(
        &db,
        account_id,
        vec![crate::model::NewPendingBooking {
            date: date!(2026 - 10 - 03),
            value_date: Some(date!(2026 - 10 - 02)),
            amount: eur(-4_980),
            partner_name: Some("Beispiel Tank GmbH".to_owned()),
            purpose: Some("Kraftstoff".to_owned()),
        }],
        datetime!(2026-10-04 09:30 UTC),
    )
    .unwrap();
    let today = date!(2026 - 10 - 04);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    assert_eq!(vm.state().pending_bookings, vec![pending_item(account_id)]);

    vm.update_period(&db, Period::ThisMonth, today).unwrap();
    assert_eq!(vm.state().pending_bookings, vec![pending_item(account_id)]);

    vm.update_category_filter(&db, CategoryFilter::Only(category_id), today)
        .unwrap();
    assert!(vm.state().pending_bookings.is_empty());
    vm.update_category_filter(&db, CategoryFilter::Any, today).unwrap();
    assert_eq!(vm.state().pending_bookings, vec![pending_item(account_id)]);

    vm.start(
        &db,
        StartArgs {
            account_id: Some(account_id),
            uncategorized_only: true,
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert!(vm.state().pending_bookings.is_empty());

    vm.start(
        &db,
        StartArgs {
            account_id: Some(account_id),
            period: Some(Period::LastMonth),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert!(vm.state().pending_bookings.is_empty());

    start_account(&db, &mut vm, account_id, today);
    vm.update_search_query(&db, "Gehalt", today).unwrap();
    assert!(vm.state().pending_bookings.is_empty());
}

#[test]
fn pending_rows_show_only_while_their_fetch_stamp_is_within_the_max_age() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(-12_000),
        Some("Beispiel AG"),
        Some("Gehalt"),
        None,
        false,
    );
    let hold = crate::model::NewPendingBooking {
        date: date!(2026 - 09 - 04),
        value_date: None,
        amount: eur(-4_980),
        partner_name: Some("Beispiel Tank GmbH".to_owned()),
        purpose: Some("Kraftstoff".to_owned()),
    };
    let today = date!(2026 - 10 - 04);
    let mut vm = view();

    for (stamp, expected) in [
        (datetime!(2026-10-04 12:00 UTC), true),
        (datetime!(2026-09-28 12:00 UTC), true),
        (datetime!(2026-09-27 12:00 UTC), true),
        (datetime!(2026-09-26 12:00 UTC), false),
    ] {
        repo::pending_bookings::replace_for_account(&db, account_id, vec![hold.clone()], stamp).unwrap();
        start_account(&db, &mut vm, account_id, today);
        assert_eq!(!vm.state().pending_bookings.is_empty(), expected, "stamp {stamp}");
    }
}

#[test]
fn pending_rows_never_change_the_totals_or_the_csv_export() {
    let (temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 01),
        eur(-12_000),
        Some("Beispiel AG"),
        Some("Gehalt"),
        None,
        false,
    );
    let today = date!(2026 - 10 - 04);
    let mut vm = view();
    start_account(&db, &mut vm, account_id, today);
    let totals_before = vm.state().selected_totals();

    repo::pending_bookings::replace_for_account(
        &db,
        account_id,
        vec![crate::model::NewPendingBooking {
            date: date!(2026 - 10 - 03),
            value_date: None,
            amount: eur(-4_980),
            partner_name: Some("Beispiel Tank GmbH".to_owned()),
            purpose: Some("Kraftstoff".to_owned()),
        }],
        datetime!(2026-10-04 09:30 UTC),
    )
    .unwrap();
    vm.load(&db, today).unwrap();
    assert_eq!(vm.state().selected_totals(), totals_before);

    let csv = temp.path().join("pending-export.csv");
    vm.export_csv(&db, &csv).unwrap();
    let text = std::fs::read_to_string(csv).unwrap();
    assert!(text.contains("Beispiel AG"));
    assert!(!text.contains("Beispiel Tank GmbH"));
}

// ---------------------------------------------------------------------------
// TransactionsImportPeriodTest.kt
// ---------------------------------------------------------------------------

fn csv_file(temp: &tempfile::TempDir, name: &str, iban: &str, rows: &[&str]) -> PathBuf {
    let path = temp.path().join(name);
    std::fs::write(
        &path,
        format!(
            "IBAN;{iban}\n\nBuchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n{}",
            rows.join("")
        ),
    )
    .unwrap();
    path
}

#[test]
fn an_import_clears_the_exact_month_and_shows_bookings_outside_it() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    account(&db, iban, 0, Currency::EUR, "checking");
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            month: Some(crate::dashboard::YearMonth::new(2026, 1)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert_eq!(vm.state().month, Some(crate::dashboard::YearMonth::new(2026, 1)));

    let file = csv_file(
        &temp,
        "october.csv",
        iban,
        &["02.10.2025;02.10.2025;Stadtwerke;Lastschrift;Import;80,00;EUR;-20,00;EUR\n"],
    );
    vm.open_import(&db, &[file], None, today).unwrap();
    vm.confirm_import(&db, false, today).unwrap();

    assert!(vm.state().month.is_none());
    assert!(vm.state().custom_range.is_none());
    assert_eq!(vm.state().period, Period::All);
    assert_eq!(partners(&vm), vec!["Stadtwerke".to_owned()]);
}

#[test]
fn an_import_clears_a_custom_range_and_shows_bookings_outside_it() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    let account_id = account(&db, iban, 0, Currency::EUR, "checking");
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();
    assert_eq!(
        vm.state().custom_range,
        Some((date!(2025 - 01 - 01), date!(2025 - 03 - 31)))
    );

    let file = csv_file(
        &temp,
        "october.csv",
        iban,
        &["02.10.2025;02.10.2025;Stadtwerke;Lastschrift;Import;80,00;EUR;-20,00;EUR\n"],
    );
    vm.open_import(&db, &[file], None, today).unwrap();
    vm.confirm_import(&db, false, today).unwrap();

    assert!(vm.state().custom_range.is_none());
    assert!(vm.state().month.is_none());
    assert_eq!(vm.state().period, Period::All);
    assert_eq!(partners(&vm), vec!["Stadtwerke".to_owned()]);
    assert_eq!(vm.state().account_id, Some(account_id));
}

// ---------------------------------------------------------------------------
// TransactionsCustomRangeTest.kt
// ---------------------------------------------------------------------------

fn custom_range_fixture() -> (
    tempfile::TempDir,
    Database,
    TransactionId,
    TransactionId,
    TransactionId,
    TransactionId,
) {
    let (temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let year_end = txn!(
        &db,
        account_id,
        date!(2024 - 12 - 31),
        eur(-1_000),
        Some("Year End"),
        None,
        None,
        false,
    );
    let new_year = txn!(
        &db,
        account_id,
        date!(2025 - 01 - 01),
        eur(-2_000),
        Some("New Year"),
        None,
        None,
        false,
    );
    let march_end = txn!(
        &db,
        account_id,
        date!(2025 - 03 - 31),
        eur(-3_000),
        Some("March End"),
        None,
        None,
        false,
    );
    let april_start = txn!(
        &db,
        account_id,
        date!(2025 - 04 - 01),
        eur(-4_000),
        Some("April Start"),
        None,
        None,
        false,
    );
    (temp, db, year_end, new_year, march_end, april_start)
}

#[test]
fn a_custom_range_shows_exactly_the_bookings_inside_it_bounds_inclusive() {
    let (_temp, db, _year_end, new_year, march_end, _april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();

    assert_eq!(ids(&vm), vec![march_end, new_year]);
    assert_eq!(
        vm.state().custom_range,
        Some((date!(2025 - 01 - 01), date!(2025 - 03 - 31)))
    );
    assert!(vm.state().month.is_none());
}

#[test]
fn a_reversed_range_changes_nothing() {
    let (_temp, db, year_end, new_year, march_end, april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 03 - 31), date!(2025 - 01 - 01), today)
        .unwrap();

    assert!(vm.state().custom_range.is_none());
    assert_eq!(ids(&vm), vec![april_start, march_end, new_year, year_end]);
}

#[test]
fn setting_a_custom_range_clears_an_active_selection() {
    let (_temp, db, _year_end, new_year, _march_end, _april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.toggle_selection(new_year);
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();
    assert!(vm.state().selection_anchor_id.is_none());
    assert!(vm.state().selected_ids.is_empty());
}

#[test]
fn picking_a_period_clears_the_custom_range() {
    let (_temp, db, year_end, new_year, march_end, april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();
    vm.update_period(&db, Period::All, today).unwrap();
    assert!(vm.state().custom_range.is_none());
    assert_eq!(ids(&vm), vec![april_start, march_end, new_year, year_end]);
}

#[test]
fn a_month_deep_link_clears_the_custom_range() {
    let (_temp, db, year_end, _new_year, _march_end, _april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();
    vm.start(
        &db,
        StartArgs {
            month: Some(crate::dashboard::YearMonth::new(2024, 12)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert!(vm.state().custom_range.is_none());
    assert_eq!(vm.state().month, Some(crate::dashboard::YearMonth::new(2024, 12)));
    assert_eq!(ids(&vm), vec![year_end]);
}

#[test]
fn a_plain_start_keeps_an_existing_custom_range() {
    let (_temp, db, _year_end, new_year, march_end, _april_start) = custom_range_fixture();
    let today = date!(2025 - 06 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::All),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    vm.set_custom_range(&db, date!(2025 - 01 - 01), date!(2025 - 03 - 31), today)
        .unwrap();
    vm.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(
        vm.state().custom_range,
        Some((date!(2025 - 01 - 01), date!(2025 - 03 - 31)))
    );
    assert_eq!(ids(&vm), vec![march_end, new_year]);
}

// ---------------------------------------------------------------------------
// ReapplyRulesTest.kt
// ---------------------------------------------------------------------------

fn rule(db: &Database, pattern: &str, category_id: CategoryId, priority: i64, direction: RuleDirection) {
    repo::rules::create(
        db,
        NewRule {
            pattern: pattern.to_owned(),
            category_id,
            priority,
            auto_apply: true,
            match_field: RuleField::Any,
            direction,
            tax_relevant: None,
        },
    )
    .unwrap();
}

#[test]
fn a_higher_priority_rule_moves_a_booking_to_the_rules_category() {
    let (_temp, db) = database();
    let loans = category(&db, "Loans", false, false);
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "SPARKASSE", loans, 1, RuleDirection::Any);
    rule(&db, "SPARKASSE", groceries, 5, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let booking = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-990),
        Some("Sparkasse"),
        None,
        Some(loans),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    let preview = vm.state().reapply_preview.clone().unwrap();
    assert_eq!(
        preview.changes.first().copied(),
        Some(ReapplyChange {
            transaction_id: booking,
            from_category_id: Some(loans),
            to_category_id: groceries
        })
    );
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    let row = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
        .unwrap()
        .into_iter()
        .find(|transaction| transaction.id == booking)
        .unwrap();
    assert_eq!(row.category_id, Some(groceries));
}

#[test]
fn a_booking_no_rule_matches_keeps_its_category_and_never_becomes_null() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let booking = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-320),
        Some("Wonder.Category"),
        None,
        Some(groceries),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    assert!(vm.state().reapply_preview.is_none());
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.id == booking)
            .unwrap()
            .category_id,
        Some(groceries)
    );
}

#[test]
fn a_booking_whose_matched_category_equals_its_own_is_not_a_change() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(groceries),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    assert!(vm.state().reapply_preview.is_none());
    assert_eq!(count_action(&db, "transaction.update_categories"), 0);
}

#[test]
fn an_expense_only_rule_does_not_change_an_income_booking() {
    let (_temp, db) = database();
    let salary = category(&db, "Salary", false, false);
    rule(&db, "PARKING", salary, 1, RuleDirection::Expense);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let booking = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(1_000),
        Some("Parking fee"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    assert!(vm.state().reapply_preview.is_none());
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.id == booking)
            .unwrap()
            .category_id,
        None
    );
}

#[test]
fn bookings_outside_the_current_filter_are_untouched() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let inside = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix Shops"),
        None,
        None,
        false,
    );
    let outside = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-700),
        Some("Netflix Vacations"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.update_search_query(&db, "Vacations", today).unwrap();
    vm.preview_reapply_rules(&db, today).unwrap();
    let preview = vm.state().reapply_preview.clone().unwrap();
    assert_eq!(preview.changes.len(), 1);
    assert_eq!(preview.changes[0].transaction_id, outside);

    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&inside], None);
    assert_eq!(rows[&outside], Some(groceries));
}

#[test]
fn confirm_applies_exactly_the_previewed_set_even_when_a_new_rule_matches_more() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let previewed = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        None,
        false,
    );
    let extra = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-1_500),
        Some("Disneyplus"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    let preview = vm.state().reapply_preview.clone().unwrap();
    assert_eq!(preview.changes.len(), 1);
    assert_eq!(preview.changes[0].transaction_id, previewed);
    assert_eq!(preview.shown_count, 2);

    rule(&db, "DISNEYPLUS", groceries, 9, RuleDirection::Any);
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&previewed], Some(groceries));
    assert_eq!(rows[&extra], None);
    assert_eq!(count_action(&db, "transaction.update_categories"), 1);
}

#[test]
fn a_new_rule_added_after_the_preview_does_not_sneak_in() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let previewed = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    rule(&db, "NETFLIX", loans, 5, RuleDirection::Any);
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    let row = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
        .unwrap()
        .into_iter()
        .find(|transaction| transaction.id == previewed)
        .unwrap();
    assert_eq!(row.category_id, Some(groceries));
}

#[test]
fn excluding_a_group_leaves_its_bookings_unchanged() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    rule(&db, "SPARKASSE", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let from_category = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let from_uncategorized = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-785),
        Some("Sparkasse"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    assert_eq!(vm.state().reapply_preview.as_ref().unwrap().groups.len(), 2);
    vm.confirm_reapply_rules(&db, &BTreeSet::from([(Some(loans), groceries)]), today)
        .unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&from_category], Some(loans));
    assert_eq!(rows[&from_uncategorized], Some(groceries));
}

#[test]
fn a_booking_changed_by_hand_between_preview_and_confirm_is_skipped() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    let salary = category(&db, "Salary", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let booking = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    vm.update_transaction_category(&db, booking, Some(salary), today)
        .unwrap();
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.id == booking)
            .unwrap()
            .category_id,
        Some(salary)
    );
}

#[test]
fn undo_restores_previous_values_including_null_and_then_cannot_undo_again() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    rule(&db, "SPARKASSE", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let from_category = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let from_uncategorized = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-785),
        Some("Sparkasse"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert!(vm.state().can_undo_reapply);

    vm.undo_reapply_rules(&db, today).unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&from_category], Some(loans));
    assert_eq!(rows[&from_uncategorized], None);
    assert!(!vm.state().can_undo_reapply);
    assert_eq!(count_action(&db, "transaction.update_categories"), 2);
}

#[test]
fn undo_keeps_a_booking_that_was_moved_by_hand_after_the_reapply() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    let salary = category(&db, "Salary", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let moved_again = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let untouched = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-800),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    vm.update_transaction_category(&db, moved_again, Some(salary), today)
        .unwrap();
    vm.undo_reapply_rules(&db, today).unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&moved_again], Some(salary));
    assert_eq!(rows[&untouched], Some(loans));
    assert!(!vm.state().can_undo_reapply);
}

#[test]
fn a_confirm_that_changes_nothing_keeps_the_previous_undo() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let first = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert!(vm.state().can_undo_reapply);

    let second = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-500),
        Some("Netflix"),
        None,
        Some(loans),
        false,
    );
    vm.load(&db, today).unwrap();
    vm.preview_reapply_rules(&db, today).unwrap();
    vm.update_transaction_category(&db, second, Some(groceries), today)
        .unwrap();
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert!(vm.state().can_undo_reapply, "the first re-apply can still be undone");

    vm.undo_reapply_rules(&db, today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.id == first)
            .unwrap()
            .category_id,
        Some(loans)
    );
}

#[test]
fn no_audit_entry_and_no_dialog_when_nothing_would_change() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        Some(groceries),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-320),
        Some("Wonder.Category"),
        None,
        Some(groceries),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    assert!(!vm.state().can_undo_reapply);
    vm.confirm_reapply_rules(&db, &BTreeSet::new(), today).unwrap();
    assert_eq!(count_action(&db, "transaction.update_categories"), 0);
}

#[test]
fn the_preview_groups_by_from_to_pair_with_a_sum_only_in_the_primary_currency() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let eur_account = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let usd_account = account(
        &db,
        "DE44500105175407324931",
        0,
        Currency::new("USD").unwrap(),
        "checking",
    );
    let euro = txn!(
        &db,
        eur_account,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        None,
        false,
    );
    let dollar = txn!(
        &db,
        usd_account,
        date!(2026 - 06 - 13),
        money(-500, "USD"),
        Some("Netflix USD"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.preview_reapply_rules(&db, today).unwrap();
    let preview = vm.state().reapply_preview.clone().unwrap();
    assert_eq!(
        preview.changes.iter().copied().collect::<BTreeSet<_>>(),
        BTreeSet::from([
            ReapplyChange {
                transaction_id: euro,
                from_category_id: None,
                to_category_id: groceries
            },
            ReapplyChange {
                transaction_id: dollar,
                from_category_id: None,
                to_category_id: groceries
            }
        ])
    );
    assert_eq!(preview.groups.len(), 1);
    assert_eq!(preview.groups[0].count, 2);
    assert!(preview.groups[0].sum.is_none());
    assert_eq!(preview.groups[0].currency, Currency::EUR);
}

#[test]
fn direction_and_category_filters_limit_the_preview_to_the_shown_bookings() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    rule(&db, "NETFLIX", groceries, 0, RuleDirection::Any);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let expense = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_200),
        Some("Netflix"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(800),
        Some("Netflix plus"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.update_direction(&db, Direction::Expenses, today).unwrap();
    vm.preview_reapply_rules(&db, today).unwrap();
    let preview = vm.state().reapply_preview.clone().unwrap();
    assert_eq!(preview.changes.len(), 1);
    assert_eq!(preview.changes[0].transaction_id, expense);
    assert_eq!(preview.shown_count, 1);
}

// ---------------------------------------------------------------------------
// TaxOverrideTest.kt (transaction view model parts)
// ---------------------------------------------------------------------------

#[test]
fn set_tax_override_for_selection_changes_only_selected_and_shown_bookings() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("Secret Partner"),
        Some("Secret purpose"),
        Some(normal),
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Other Partner"),
        None,
        Some(normal),
        false,
    );
    let c = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-3_000),
        Some("Third Partner"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(a);
    vm.toggle_selection(b);
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));

    vm.set_tax_override_for_selection(&db, Some(true), today).unwrap();
    let rows: BTreeMap<TransactionId, TransactionItem> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction))
            .collect();
    for id in [a, b] {
        assert!(rows[&id].is_tax_relevant);
        assert_eq!(rows[&id].tax_source, crate::model::TaxSource::Manual);
        assert_eq!(rows[&id].tax_override, Some(true));
    }
    assert!(!rows[&c].is_tax_relevant);
    assert_eq!(rows[&c].tax_source, crate::model::TaxSource::Category);
    assert_eq!(rows[&c].tax_override, None);
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));

    let entries = crate::audit::recent(&db, 1000).unwrap();
    let tax_entries: Vec<_> = entries
        .iter()
        .filter(|entry| entry.action == "transaction.set_tax_override")
        .collect();
    assert_eq!(tax_entries.len(), 1);
    let details = &tax_entries[0].details;
    assert!(details.contains("count=2"), "{details}");
    assert!(details.contains("true"), "{details}");
    assert!(!details.contains("Secret"));
    assert!(!details.contains("purpose"));
    assert!(!details.contains("-10.00"));
    assert_eq!(
        vm.state().message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageTaxOverrideSet, &[Arg::from(2)]).as_str())
    );

    vm.set_tax_override_for_selection(&db, None, today).unwrap();
    let rows: BTreeMap<TransactionId, TransactionItem> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction))
            .collect();
    for id in [a, b] {
        assert!(!rows[&id].is_tax_relevant);
        assert_eq!(rows[&id].tax_source, crate::model::TaxSource::Category);
        assert_eq!(rows[&id].tax_override, None);
    }
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));
}

#[test]
fn set_tax_override_for_selection_never_touches_a_booking_the_list_does_not_show() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-1_000),
        Some("One"),
        None,
        Some(normal),
        false,
    );
    let hidden = txn!(
        &db,
        account_id,
        date!(2027 - 01 - 15),
        eur(-9_900),
        Some("Far Future"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(ids(&vm), vec![a]);
    vm.toggle_selection(a);

    vm.set_tax_override_for_selection(&db, Some(true), today).unwrap();
    let rows: BTreeMap<TransactionId, TransactionItem> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction))
            .collect();
    assert_eq!(rows[&a].tax_source, crate::model::TaxSource::Manual);
    assert_eq!(rows[&hidden].tax_override, None);
    let tax_entries = crate::audit::recent(&db, 1000)
        .unwrap()
        .into_iter()
        .filter(|entry| entry.action == "transaction.set_tax_override")
        .count();
    assert_eq!(tax_entries, 1);
    assert!(crate::audit::recent(&db, 1).unwrap()[0].details.contains("count=1"));
}

#[test]
fn set_tax_override_from_the_details_path_updates_the_item_the_state_shows() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_000),
        Some("One"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    let selected = vm.state().transactions.iter().find(|t| t.id == a).unwrap().clone();
    let service = DocumentService::new(&db, OcrService::without_ocr(), "unused-docs");
    vm.select_transaction(&db, &service, Some(selected)).unwrap();
    assert_eq!(vm.state().selected.as_ref().unwrap().id, a);

    vm.set_tax_override(&db, a, Some(false), today).unwrap();
    let selected = vm.state().selected.as_ref().unwrap();
    assert_eq!(selected.id, a);
    assert!(!selected.is_tax_relevant);
    assert_eq!(selected.tax_source, crate::model::TaxSource::Manual);
    assert_eq!(selected.tax_override, Some(false));
    let details = crate::audit::recent(&db, 1).unwrap()[0].details.clone();
    assert!(details.contains("count=1"), "{details}");
    assert!(details.contains("false"), "{details}");
    assert!(!details.contains("One"));
}

#[test]
fn a_rule_flag_alone_changes_the_shown_bookings_without_touching_the_override() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_000),
        Some("Beispiel Shop"),
        None,
        Some(normal),
        false,
    );
    repo::rules::create(
        &db,
        NewRule {
            pattern: "Beispiel".to_owned(),
            category_id: normal,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: Some(true),
        },
    )
    .unwrap();
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    let shown = vm
        .state()
        .transactions
        .iter()
        .find(|transaction| transaction.id == a)
        .unwrap();
    assert!(shown.is_tax_relevant);
    assert_eq!(shown.tax_source, crate::model::TaxSource::Rule);
    assert_eq!(shown.tax_rule_pattern.as_deref(), Some("Beispiel"));
    assert_eq!(shown.tax_override, None);
}

// ---------------------------------------------------------------------------
// DashboardMonthTest.kt (transactions parts)
// ---------------------------------------------------------------------------

fn dashboard_month_fixture() -> (tempfile::TempDir, Database, AccountId, CategoryId) {
    let (temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 100_000, Currency::EUR, "checking");
    let food = category(&db, "Food", false, false);
    let tax = category(&db, "Donations", true, false);
    let transfers = category(&db, "Own transfers", false, true);
    txn!(
        &db,
        account_id,
        date!(2026 - 03 - 05),
        eur(-10_000),
        Some("MaerzJetzt"),
        None,
        Some(tax),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 08 - 02),
        eur(-12_000),
        Some("August Nazar"),
        None,
        Some(food),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 08 - 03),
        eur(200_000),
        Some("Employer"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 08 - 04),
        eur(-20_000),
        Some("Erika Musterfrau"),
        None,
        Some(transfers),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 08 - 05),
        eur(-1_500),
        None,
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 09 - 04),
        eur(-6_000),
        Some("September Nazar"),
        None,
        Some(food),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 09 - 06),
        eur(-5_000),
        Some("September Spenden"),
        None,
        Some(tax),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 02),
        eur(-4_000),
        Some("Oktober Nazar"),
        None,
        Some(food),
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(150_000),
        Some("Employer"),
        None,
        None,
        false,
    );
    (temp, db, account_id, food)
}

#[test]
fn a_month_navigation_lists_exactly_that_months_bookings_and_a_picked_period_ends_it() {
    let (_temp, db, _account_id, food) = dashboard_month_fixture();
    let today = date!(2026 - 10 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            category_id: Some(food),
            month: Some(crate::dashboard::YearMonth::new(2026, 8)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert_eq!(vm.state().month, Some(crate::dashboard::YearMonth::new(2026, 8)));
    assert_eq!(partners(&vm), vec!["August Nazar".to_owned()]);
    assert!(vm.state().error.is_none());

    vm.update_period(&db, Period::ThisMonth, today).unwrap();
    assert!(vm.state().month.is_none());
    assert_eq!(partners(&vm), vec!["Oktober Nazar".to_owned()]);
}

#[test]
fn the_current_months_category_link_lists_only_that_months_bookings() {
    use crate::app::dashboard::NavigationArgs;

    let (_temp, db, _account_id, food) = dashboard_month_fixture();
    let today = date!(2026 - 10 - 15);
    let current = crate::dashboard::YearMonth::from_date(today);
    let mut vm = view();
    vm.start(
        &db,
        NavigationArgs::category(current, current, food).start_args(),
        today,
    )
    .unwrap();

    assert_eq!(vm.state().period, Period::ThisMonth);
    assert!(
        vm.state().month.is_none(),
        "the current month is a period, not an exact month"
    );
    assert_eq!(
        partners(&vm),
        vec!["Oktober Nazar".to_owned()],
        "August's booking stays out of the current month's list"
    );
}

#[test]
fn a_past_months_category_and_uncategorized_links_list_exactly_that_months_bookings() {
    use crate::app::dashboard::NavigationArgs;

    let (_temp, db, _account_id, food) = dashboard_month_fixture();
    let today = date!(2026 - 10 - 15);
    let current = crate::dashboard::YearMonth::from_date(today);
    let august = crate::dashboard::YearMonth::new(2026, 8);

    let mut vm = view();
    vm.start(&db, NavigationArgs::category(august, current, food).start_args(), today)
        .unwrap();
    assert_eq!(vm.state().month, Some(august));
    assert_eq!(partners(&vm), vec!["August Nazar".to_owned()]);

    let mut vm = view();
    vm.start(&db, NavigationArgs::uncategorized(august, current).start_args(), today)
        .unwrap();
    assert_eq!(vm.state().month, Some(august));
    assert_eq!(vm.state().category_filter, CategoryFilter::None);
    assert_eq!(
        vm.state().transactions.len(),
        2,
        "August's two uncategorized bookings, not September's or October's"
    );
    assert!(
        vm.state()
            .transactions
            .iter()
            .all(|transaction| transaction.date.year() == 2026 && u8::from(transaction.date.month()) == 8)
    );
}

#[test]
fn with_a_month_set_and_one_account_the_balance_chart_covers_only_that_month() {
    let (_temp, db, account_id, _food) = dashboard_month_fixture();
    let today = date!(2026 - 10 - 15);
    let mut vm = view();
    vm.start(
        &db,
        StartArgs {
            account_id: Some(account_id),
            month: Some(crate::dashboard::YearMonth::new(2026, 8)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let chart = vm.state().balance_chart.as_ref().unwrap();
    assert_eq!(chart.first().unwrap().0, date!(2026 - 08 - 01));
    assert_eq!(chart.last().unwrap().0, date!(2026 - 08 - 31));
    assert_eq!(chart.len(), 31);
}

// ---------------------------------------------------------------------------
// MultiDeleteTest.kt
// ---------------------------------------------------------------------------

#[test]
fn toggle_selection_twice_removes_again_and_sets_the_anchor() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let one = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-1_000),
        Some("One"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(one);
    assert_eq!(vm.state().selected_ids, BTreeSet::from([one]));
    assert_eq!(vm.state().selection_anchor_id, Some(one));
    vm.toggle_selection(one);
    assert!(vm.state().selected_ids.is_empty());
    assert_eq!(vm.state().selection_anchor_id, Some(one));
}

#[test]
fn select_range_selects_the_shown_rows_between_anchor_and_target() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let one = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 11),
        eur(-100),
        Some("One"),
        None,
        None,
        false,
    );
    let two = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-200),
        Some("Two"),
        None,
        None,
        false,
    );
    let three = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-300),
        Some("Three"),
        None,
        None,
        false,
    );
    let four = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-400),
        Some("Four"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    assert_eq!(ids(&vm), vec![four, three, two, one]);

    vm.toggle_selection(two);
    vm.select_range(four);
    assert_eq!(vm.state().selected_ids, BTreeSet::from([two, three, four]));
    assert_eq!(vm.state().selection_anchor_id, Some(four));

    let mut other = view();
    start_all(&db, &mut other, today);
    other.toggle_selection(three);
    other.select_range(one);
    assert_eq!(other.state().selected_ids, BTreeSet::from([one, two, three]));
}

#[test]
fn select_range_without_an_anchor_behaves_like_a_toggle() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let one = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-100),
        Some("One"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.select_all_shown();
    assert!(vm.state().selection_anchor_id.is_none());
    vm.select_range(one);
    assert!(vm.state().selected_ids.is_empty());
    assert_eq!(vm.state().selection_anchor_id, Some(one));
}

#[test]
fn select_all_shown_selects_only_what_the_current_filter_shows() {
    let (_temp, db) = database();
    let groceries = category(&db, "Groceries", false, false);
    let loans = category(&db, "Loans", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let in_category = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_000),
        Some("InCategory"),
        None,
        Some(groceries),
        false,
    );
    let other_category = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("OtherCategory"),
        None,
        Some(loans),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.update_category_filter(&db, CategoryFilter::Only(groceries), today)
        .unwrap();
    assert_eq!(ids(&vm), vec![in_category]);
    vm.select_all_shown();
    assert_eq!(vm.state().selected_ids, BTreeSet::from([in_category]));
    assert!(!vm.state().selected_ids.contains(&other_category));
}

#[test]
fn every_filter_change_clears_the_selection() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let other = account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-100),
        Some("One"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        other,
        date!(2026 - 06 - 14),
        eur(-200),
        Some("Two"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.select_all_shown();
    assert!(!vm.state().selected_ids.is_empty());

    vm.update_period(&db, Period::LastMonth, today).unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.update_search_query(&db, "One", today).unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.update_direction(&db, Direction::Expenses, today).unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.update_amount_range(&db, Some(eur(500)), None, today).unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.update_category_filter(&db, CategoryFilter::None, today).unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.select_accounts(&db, AccountSelection::Account(account_id), today)
        .unwrap();
    assert!(vm.state().selected_ids.is_empty());

    vm.select_all_shown();
    vm.start(
        &db,
        StartArgs {
            month: Some(crate::dashboard::YearMonth::from_date(today)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert!(vm.state().selected_ids.is_empty());
}

#[test]
fn delete_selected_deletes_exactly_the_confirmed_bookings_with_one_count_only_entry() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("Secret Partner"),
        Some("Secret purpose"),
        None,
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Other Partner"),
        None,
        None,
        false,
    );
    let c = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(3_000),
        Some("Third"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(a);
    vm.toggle_selection(b);
    vm.delete_selected(&db, &BTreeSet::from([a, b]), today).unwrap();

    let rows = ids_from_db(&db);
    assert!(!rows.contains(&a));
    assert!(!rows.contains(&b));
    assert!(rows.contains(&c));
    assert!(vm.state().selected_ids.is_empty());
    let entries: Vec<_> = crate::audit::recent(&db, 1000)
        .unwrap()
        .into_iter()
        .filter(|entry| entry.action == "transaction.delete_all")
        .collect();
    assert_eq!(entries.len(), 1);
    let details = &entries[0].details;
    assert!(details.contains("count=2"), "{details}");
    assert!(!details.contains("Secret"));
    assert!(!details.contains("purpose"));
    assert_eq!(
        vm.state().message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageTransactionsDeleted, &[Arg::from(2)]).as_str())
    );
}

fn ids_from_db(db: &Database) -> BTreeSet<TransactionId> {
    repo::transactions::find_items(db, &TransactionFilter::default(), None)
        .unwrap()
        .into_iter()
        .map(|transaction| transaction.id)
        .collect()
}

#[test]
fn a_selected_id_that_disappears_on_refresh_is_dropped() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("One"),
        None,
        None,
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Two"),
        None,
        None,
        false,
    );
    let c = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-3_000),
        Some("Three"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    for id in [a, b, c] {
        vm.toggle_selection(id);
    }
    repo::transactions::delete(&db, c).unwrap();
    vm.load(&db, today).unwrap();
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));
}

#[test]
fn delete_selected_never_deletes_a_hidden_or_unpassed_booking() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("Alpha"),
        None,
        None,
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Beta"),
        None,
        None,
        false,
    );
    let hidden = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-3_000),
        Some("Gamma"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.update_search_query(&db, "Alph", today).unwrap();
    assert_eq!(ids(&vm), vec![a]);
    vm.toggle_selection(a);
    vm.delete_selected(&db, &BTreeSet::from([a, hidden]), today).unwrap();
    let rows = ids_from_db(&db);
    assert!(!rows.contains(&a));
    assert!(rows.contains(&hidden));
    assert!(rows.contains(&b));
}

#[test]
fn selected_totals_keep_currencies_separate() {
    let (_temp, db) = database();
    let euro_account = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let dollar_account = account(
        &db,
        "DE44500105175407324931",
        0,
        Currency::new("USD").unwrap(),
        "checking",
    );
    txn!(
        &db,
        euro_account,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("One"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        euro_account,
        date!(2026 - 06 - 13),
        eur(-250),
        Some("Two"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        dollar_account,
        date!(2026 - 06 - 14),
        money(500, "USD"),
        Some("USD Partner"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.select_all_shown();
    assert_eq!(vm.state().selected_ids.len(), 3);
    let totals = vm.state().selected_totals();
    assert_eq!(totals.len(), 2);
    assert_eq!(
        totals
            .iter()
            .find(|(currency, _)| *currency == Currency::EUR)
            .unwrap()
            .1,
        -1_250
    );
    assert_eq!(
        totals
            .iter()
            .find(|(currency, _)| *currency == Currency::new("USD").unwrap())
            .unwrap()
            .1,
        500
    );
}

// ---------------------------------------------------------------------------
// SetCategoryForSelectionTest.kt
// ---------------------------------------------------------------------------

#[test]
fn set_category_for_selection_changes_only_selected_and_shown_bookings() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let office = category(&db, "Office Supplies", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 12),
        eur(-1_000),
        Some("Secret Partner"),
        Some("Secret purpose"),
        Some(normal),
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Other Partner"),
        None,
        Some(normal),
        false,
    );
    let c = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-3_000),
        Some("Third Partner"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(a);
    vm.toggle_selection(b);
    vm.set_category_for_selection(&db, Some(office), today).unwrap();

    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&a], Some(office));
    assert_eq!(rows[&b], Some(office));
    assert_eq!(rows[&c], Some(normal));
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));

    let entries: Vec<_> = crate::audit::recent(&db, 1000)
        .unwrap()
        .into_iter()
        .filter(|entry| entry.action == "transaction.update_categories")
        .collect();
    assert_eq!(entries.len(), 1);
    let details = &entries[0].details;
    assert!(details.contains("count=2"), "{details}");
    assert!(!details.contains("Secret"));
    assert!(!details.contains("purpose"));
    assert!(!details.contains("-10.00"));
    assert_eq!(
        vm.state().message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageCategorySet, &[Arg::from(2)]).as_str())
    );
}

#[test]
fn set_category_for_selection_never_touches_a_booking_the_list_does_not_show() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let office = category(&db, "Office Supplies", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-1_000),
        Some("One"),
        None,
        Some(normal),
        false,
    );
    let hidden = txn!(
        &db,
        account_id,
        date!(2027 - 01 - 15),
        eur(-9_900),
        Some("Far Future"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(ids(&vm), vec![a]);
    vm.toggle_selection(a);
    vm.set_category_for_selection(&db, Some(office), today).unwrap();

    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&a], Some(office));
    assert_eq!(rows[&hidden], Some(normal));
    let entries = crate::audit::recent(&db, 1).unwrap();
    assert!(entries[0].details.contains("count=1"));
}

#[test]
fn null_makes_the_selected_bookings_uncategorized() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let a = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-1_000),
        Some("One"),
        None,
        Some(normal),
        false,
    );
    let b = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-2_000),
        Some("Two"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(a);
    vm.toggle_selection(b);
    vm.set_category_for_selection(&db, None, today).unwrap();
    let rows: BTreeMap<TransactionId, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.id, transaction.category_id))
            .collect();
    assert_eq!(rows[&a], None);
    assert_eq!(rows[&b], None);
    assert_eq!(vm.state().selected_ids, BTreeSet::from([a, b]));
    // Repository audit details name the changed count only; no free text can leak.
    let details = crate::audit::recent(&db, 1).unwrap()[0].details.clone();
    assert!(details.contains("count=2"), "{details}");
}

#[test]
fn bookings_already_on_the_target_category_are_not_counted() {
    let (_temp, db) = database();
    let normal = category(&db, "Groceries", false, false);
    let office = category(&db, "Office Supplies", false, false);
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let already = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 13),
        eur(-1_000),
        Some("One"),
        None,
        Some(office),
        false,
    );
    let other = txn!(
        &db,
        account_id,
        date!(2026 - 06 - 14),
        eur(-2_000),
        Some("Two"),
        None,
        Some(normal),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    start_all(&db, &mut vm, today);
    vm.toggle_selection(already);
    vm.toggle_selection(other);
    vm.set_category_for_selection(&db, Some(office), today).unwrap();
    assert_eq!(
        vm.state().message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageCategorySet, &[Arg::from(1)]).as_str())
    );
    assert_eq!(count_action(&db, "transaction.update_categories"), 1);

    vm.clear_selection();
    vm.toggle_selection(already);
    vm.set_category_for_selection(&db, Some(office), today).unwrap();
    assert_eq!(count_action(&db, "transaction.update_categories"), 1);
    assert_eq!(
        vm.state().message.as_deref(),
        Some(i18n::format(Language::En, Key::MessageCategorySet, &[Arg::from(1)]).as_str())
    );
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.id == already)
            .unwrap()
            .category_id,
        Some(office)
    );
}

// ---------------------------------------------------------------------------
// TransactionsUncategorizedLinkTest.kt
// ---------------------------------------------------------------------------

#[test]
fn the_uncategorized_link_shows_both_accounts_and_clears_the_filters() {
    let (_temp, db) = database();
    let acc1 = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let acc2 = account(&db, "DE625 Casablanca 02011002 071903", 0, Currency::EUR, "checking");
    let unc1 = txn!(&db, acc1, date!(2026 - 06 - 05), eur(500), None, None, None, false);
    let unc2 = txn!(&db, acc2, date!(2026 - 06 - 07), eur(700), None, None, None, false);
    let groceries = category(&db, "Groceries", false, false);
    txn!(
        &db,
        acc1,
        date!(2026 - 06 - 08),
        eur(900),
        None,
        None,
        Some(groceries),
        false,
    );
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.select_accounts(&db, AccountSelection::Account(acc1), today).unwrap();
    vm.update_search_query(&db, "no such partner", today).unwrap();
    vm.update_direction(&db, Direction::Income, today).unwrap();
    vm.update_amount_range(&db, Some(eur(1_000)), None, today).unwrap();

    vm.start(
        &db,
        StartArgs {
            uncategorized_only: true,
            period: Some(Period::ThisMonth),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let state = vm.state();
    assert_eq!(
        state.transactions.iter().map(|t| t.id).collect::<BTreeSet<_>>(),
        BTreeSet::from([unc1, unc2])
    );
    assert_eq!(state.search_query, "");
    assert_eq!(state.direction, Direction::All);
    assert!(state.min_amount.is_none());
    assert!(state.max_amount.is_none());
    assert_eq!(state.selection, AccountSelection::All);
    assert!(state.account_id.is_none());
    assert_eq!(state.period, Period::ThisMonth);
}

#[test]
fn the_uncategorized_link_with_an_exact_month_shows_that_month_on_all_accounts() {
    let (_temp, db) = database();
    let acc1 = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let acc2 = account(&db, "DE625 Casablanca 02011002 071903", 0, Currency::EUR, "checking");
    let may1 = txn!(&db, acc1, date!(2026 - 05 - 03), eur(100), None, None, None, false);
    let may2 = txn!(&db, acc2, date!(2026 - 05 - 20), eur(200), None, None, None, false);
    txn!(&db, acc1, date!(2026 - 06 - 05), eur(300), None, None, None, false);
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.update_search_query(&db, "no such partner", today).unwrap();
    vm.update_direction(&db, Direction::Income, today).unwrap();
    vm.update_amount_range(&db, Some(eur(1_000)), None, today).unwrap();

    vm.start(
        &db,
        StartArgs {
            uncategorized_only: true,
            month: Some(crate::dashboard::YearMonth::new(2026, 5)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let state = vm.state();
    assert_eq!(
        state.transactions.iter().map(|t| t.id).collect::<BTreeSet<_>>(),
        BTreeSet::from([may1, may2])
    );
    assert_eq!(state.search_query, "");
    assert_eq!(state.direction, Direction::All);
    assert!(state.min_amount.is_none());
    assert!(state.max_amount.is_none());
    assert_eq!(state.selection, AccountSelection::All);
    assert!(state.account_id.is_none());
    assert_eq!(state.period, Period::All);
}

#[test]
fn the_uncategorized_link_ignores_a_saved_panel_selection() {
    let (_temp, db) = database();
    let acc1 = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let acc2 = account(&db, "DE625 Casablanca 02011002 071903", 0, Currency::EUR, "checking");
    let unc1 = txn!(&db, acc1, date!(2026 - 06 - 05), eur(500), None, None, None, false);
    let unc2 = txn!(&db, acc2, date!(2026 - 06 - 07), eur(700), None, None, None, false);
    let today = date!(2026 - 06 - 15);
    let mut previous = view();
    previous.load(&db, today).unwrap();
    previous
        .select_accounts(&db, AccountSelection::Account(acc1), today)
        .unwrap();

    let mut vm = view();
    vm.start(&db, StartArgs::default(), today).unwrap();
    assert_eq!(vm.state().selection, AccountSelection::Account(acc1));
    vm.start(
        &db,
        StartArgs {
            uncategorized_only: true,
            period: Some(Period::ThisMonth),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let state = vm.state();
    assert_eq!(
        state.transactions.iter().map(|t| t.id).collect::<BTreeSet<_>>(),
        BTreeSet::from([unc1, unc2])
    );
    assert_eq!(state.selection, AccountSelection::All);
    assert!(state.account_id.is_none());
}

// ---------------------------------------------------------------------------
// TransactionsStartCategoryTest.kt
// ---------------------------------------------------------------------------

#[test]
fn start_with_a_category_sets_the_filters_and_clears_a_previous_search() {
    let (_temp, db) = database();
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.update_search_query(&db, "Erika Mustermann", today).unwrap();
    vm.start(
        &db,
        StartArgs {
            period: Some(Period::ThisMonth),
            category_id: Some(CategoryId(7)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let state = vm.state();
    assert_eq!(state.search_query, "");
    assert_eq!(state.category_filter, CategoryFilter::Only(CategoryId(7)));
    assert_eq!(state.period, Period::ThisMonth);
    assert_eq!(state.direction, Direction::All);
    assert_eq!(state.selection, AccountSelection::All);
    assert!(state.account_id.is_none());
    assert!(state.min_amount.is_none());
    assert!(state.max_amount.is_none());
}

#[test]
fn a_category_link_clears_a_direction_filter_and_an_amount_range() {
    let (_temp, db) = database();
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.update_direction(&db, Direction::Income, today).unwrap();
    vm.update_amount_range(&db, Some(eur(1_200)), Some(eur(10_000)), today)
        .unwrap();
    vm.update_category_filter(&db, CategoryFilter::None, today).unwrap();
    vm.start(
        &db,
        StartArgs {
            category_id: Some(CategoryId(3)),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    let state = vm.state();
    assert_eq!(state.category_filter, CategoryFilter::Only(CategoryId(3)));
    assert_eq!(state.direction, Direction::All);
    assert!(state.min_amount.is_none());
    assert!(state.max_amount.is_none());
    assert_eq!(state.period, Period::All);
}

#[test]
fn uncategorized_only_together_with_a_period_uses_that_period_instead_of_all() {
    let (_temp, db) = database();
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    vm.start(
        &db,
        StartArgs {
            uncategorized_only: true,
            period: Some(Period::ThisMonth),
            ..StartArgs::default()
        },
        today,
    )
    .unwrap();
    assert_eq!(vm.state().category_filter, CategoryFilter::None);
    assert_eq!(vm.state().period, Period::ThisMonth);
}

// ---------------------------------------------------------------------------
// MultiFileImportTest.kt
// ---------------------------------------------------------------------------

fn multi_csv(temp: &tempfile::TempDir, name: &str, iban_line: Option<&str>, rows: &[&str]) -> PathBuf {
    let path = temp.path().join(name);
    let mut text = String::new();
    if let Some(iban) = iban_line {
        text.push_str(&format!("IBAN;{iban}\n\n"));
    }
    text.push_str("Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n");
    text.push_str(&rows.join(""));
    std::fs::write(path, text).unwrap();
    path_placeholder(temp, name)
}

fn path_placeholder(temp: &tempfile::TempDir, name: &str) -> PathBuf {
    temp.path().join(name)
}

fn import_row(date: &str, partner: &str, purpose: &str, saldo: &str, amount: &str) -> String {
    format!("{date};{date};{partner};Lastschrift;{purpose};{saldo};EUR;{amount};EUR\n")
}

#[test]
fn two_files_are_previewed_in_booking_order_and_imported_with_the_newest_balance() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    let account_id = account(&db, iban, 0, Currency::EUR, "checking");
    let older = multi_csv(
        &temp,
        "october.csv",
        Some(iban),
        &[
            &import_row("02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00"),
            &import_row("15.10.2025", "Obststand", "Obst", "50,00", "-30,00"),
        ],
    );
    let newer = multi_csv(
        &temp,
        "november.csv",
        Some(iban),
        &[
            &import_row("05.11.2025", "Arbeitgeber", "Gehalt", "150,00", "100,00"),
            &import_row("20.11.2025", "Kino", "Tickets", "140,00", "-10,00"),
        ],
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[newer.clone(), older.clone()], None, today)
        .unwrap();
    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(preview.entries.len(), 2);
    assert_eq!(preview.entries[0].file, older, "imported oldest first");
    assert_eq!(preview.entries[1].file, newer);
    assert_eq!(preview.newest().file, newer);
    assert_eq!(preview.booking_count(), 4);
    assert_eq!(preview.account_id, Some(account_id));
    assert!(preview.balance_update_allowed);
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .len(),
        0,
        "nothing is stored before confirming"
    );

    vm.confirm_import(&db, true, today).unwrap();
    assert!(vm.state().import_preview.is_none());
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .len(),
        4
    );
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().balance,
        eur(14_000)
    );
    assert!(
        vm.state()
            .message
            .as_deref()
            .is_some_and(|message| message.contains(&i18n::format(
                Language::En,
                Key::ImportSummaryFiles,
                &[Arg::from(4), Arg::from(2)]
            )))
    );
}

#[test]
fn importing_the_same_two_files_again_stores_no_duplicates() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    account(&db, iban, 0, Currency::EUR, "checking");
    let older = multi_csv(
        &temp,
        "october.csv",
        Some(iban),
        &[&import_row("02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00")],
    );
    let newer = multi_csv(
        &temp,
        "november.csv",
        Some(iban),
        &[&import_row("20.11.2025", "Kino", "Tickets", "140,00", "-10,00")],
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[older.clone(), newer.clone()], None, today)
        .unwrap();
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .len(),
        2
    );

    vm.open_import(&db, &[newer, older], None, today).unwrap();
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .len(),
        2
    );
    assert!(
        vm.state()
            .message
            .as_deref()
            .is_some_and(|message| message.contains(&i18n::format(
                Language::En,
                Key::ImportSummaryKnown,
                &[Arg::from(2)]
            )))
    );
}

#[test]
fn the_balance_comes_only_from_the_newest_file_and_only_when_update_balance_is_true() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    let account_id = account(&db, iban, 0, Currency::EUR, "checking");
    let older = multi_csv(
        &temp,
        "october.csv",
        Some(iban),
        &[&import_row("02.10.2025", "Stadtwerke", "Strom", "50,00", "-20,00")],
    );
    let newer = multi_csv(
        &temp,
        "november.csv",
        Some(iban),
        &[&import_row("20.11.2025", "Kino", "Tickets", "140,00", "-10,00")],
    );
    repo::accounts::set_balance(&db, account_id, eur(100_000), OffsetDateTime::now_utc()).unwrap();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[older, newer.clone()], None, today).unwrap();
    vm.confirm_import(&db, false, today).unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().balance,
        eur(100_000)
    );

    let older_again = multi_csv(
        &temp,
        "october2.csv",
        Some(iban),
        &[&import_row("02.10.2025", "Stadtwerke", "Strom", "50,00", "-20,00")],
    );
    vm.open_import(&db, &[newer, older_again], None, today).unwrap();
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().balance,
        eur(14_000)
    );
}

#[test]
fn a_good_file_next_to_an_unreadable_one_still_imports() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    account(&db, iban, 0, Currency::EUR, "checking");
    let good = multi_csv(
        &temp,
        "good.csv",
        Some(iban),
        &[&import_row("02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00")],
    );
    let ghost = temp.path().join("ghost.csv");
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[good.clone(), ghost.clone()], None, today)
        .unwrap();
    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(preview.files, vec![good, ghost]);
    assert_eq!(preview.entries.len(), 1);
    assert_eq!(preview.failures.len(), 1);
    assert_eq!(preview.failures[0].file_name, "ghost.csv");
    assert_eq!(preview.booking_count(), 1);
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .iter()
            .filter(|transaction| transaction.partner_name.as_deref() == Some("Stadtwerke"))
            .count(),
        1
    );
    assert!(vm.state().error.is_none());
}

#[test]
fn all_files_unreadable_sets_an_error_and_no_preview() {
    let (temp, db) = database();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(
        &db,
        &[temp.path().join("ghost1.csv"), temp.path().join("ghost2.csv")],
        None,
        today,
    )
    .unwrap();
    assert!(vm.state().import_preview.is_none());
    assert_eq!(
        vm.state().error.as_deref(),
        Some(
            i18n::format(
                Language::En,
                Key::ErrorImportNoReadableFile,
                &[Arg::from(
                    "ghost1.csv: File not found: ghost1.csv, ghost2.csv: File not found: ghost2.csv"
                )]
            )
            .as_str()
        )
    );
}

#[test]
fn files_for_different_accounts_are_refused() {
    let (temp, db) = database();
    account(&db, "DE44500105175407324931", 0, Currency::EUR, "checking");
    account(&db, "DE02120300000000202051", 0, Currency::EUR, "checking");
    let one = multi_csv(
        &temp,
        "one.csv",
        Some("DE44500105175407324931"),
        &[&import_row("02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00")],
    );
    let two = multi_csv(
        &temp,
        "two.csv",
        Some("DE02120300000000202051"),
        &[&import_row("02.11.2025", "Kino", "Tickets", "90,00", "-10,00")],
    );
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[one, two], None, today).unwrap();
    assert!(vm.state().import_preview.is_none());
    let error = vm.state().error.as_deref().unwrap();
    assert!(error.contains("…4931") && error.contains("…2051"), "{error}");
    assert!(
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .is_empty()
    );
}

#[test]
fn too_many_files_sets_the_limit_error_without_reading_any_file() {
    let (temp, db) = database();
    let files: Vec<PathBuf> = (1..=241)
        .map(|index| temp.path().join(format!("missing-{index}.csv")))
        .collect();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &files, None, today).unwrap();
    assert!(vm.state().import_preview.is_none());
    assert_eq!(
        vm.state().error.as_deref(),
        Some(
            i18n::format(
                Language::En,
                Key::ErrorImportTooManyFiles,
                &[Arg::from(MAX_IMPORT_FILES)]
            )
            .as_str()
        )
    );
}

#[test]
fn a_card_statement_without_an_iban_line_picks_the_selected_card_account() {
    let (temp, db) = database();
    let card = account(&db, "CARD1234", 10_000, Currency::EUR, "credit_card");
    let file = multi_csv(
        &temp,
        "card.csv",
        None,
        &[&import_row("05.05.2026", "Restaurant", "Essen", "90,00", "-10,00")],
    );
    let today = date!(2026 - 05 - 20);
    let mut vm = view();
    start_account(&db, &mut vm, card, today);
    vm.open_import(&db, &[file], None, today).unwrap();
    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(preview.account_id, Some(card));
    assert!(preview.balance_update_allowed);
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(repo::accounts::get(&db, card).unwrap().unwrap().balance, eur(9_000));
}

#[test]
fn a_column_remap_re_reads_only_the_files_with_the_same_header_layout() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    account(&db, iban, 0, Currency::EUR, "checking");
    let a1 = multi_csv(
        &temp,
        "a1.csv",
        Some(iban),
        &[&import_row("05.01.2026", "A1Partner", "A1Purpose", "100,00", "-10,00")],
    );
    let a2 = multi_csv(
        &temp,
        "a2.csv",
        Some(iban),
        &[&import_row("05.02.2026", "A2Partner", "A2Purpose", "90,00", "-10,00")],
    );
    let other = temp.path().join("other.csv");
    std::fs::write(
        &other,
        format!(
            "IBAN;{iban}\n\nBuchung;Betrag;Verwendungszweck;Empfänger;Saldo;Währung\n05.03.2026;-10,00;OtherPurpose;OtherPartner;80,00;EUR\n"
        ),
    )
    .unwrap();
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[a1.clone(), a2.clone(), other.clone()], None, today)
        .unwrap();
    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(
        preview
            .entries
            .iter()
            .map(|entry| entry.file.clone())
            .collect::<Vec<_>>(),
        vec![a1, a2, other]
    );
    assert_eq!(
        preview
            .entries
            .iter()
            .map(|entry| entry.statement.bookings[0].purpose.clone().unwrap())
            .collect::<Vec<_>>(),
        vec![
            "A1Purpose".to_owned(),
            "A2Purpose".to_owned(),
            "OtherPurpose".to_owned()
        ]
    );

    vm.remap_import(
        &db,
        CsvMapping {
            date: Some(0),
            amount: Some(7),
            balance: Some(5),
            partner: Some(2),
            purpose: Some(3),
            ..CsvMapping::default()
        },
        today,
    )
    .unwrap();
    let remapped = vm.state().import_preview.clone().unwrap();
    assert_eq!(
        remapped
            .entries
            .iter()
            .map(|entry| entry.statement.bookings[0].purpose.clone().unwrap())
            .collect::<Vec<_>>(),
        vec![
            "Lastschrift".to_owned(),
            "Lastschrift".to_owned(),
            "OtherPurpose".to_owned()
        ],
        "the other layout keeps its auto-detected mapping"
    );
}

#[test]
fn ordering_puts_an_empty_file_first_and_the_quarter_file_newest() {
    let (temp, db) = database();
    let iban = "DE44500105175407324931";
    let account_id = account(&db, iban, 0, Currency::EUR, "checking");
    let quarter = multi_csv(
        &temp,
        "quarter.csv",
        Some(iban),
        &[
            &import_row("10.01.2026", "QShop", "Q", "290,00", "-10,00"),
            &import_row("10.02.2026", "QShop", "Q", "280,00", "-10,00"),
            &import_row("10.03.2026", "QLast", "Q", "270,00", "-10,00"),
        ],
    );
    let january = multi_csv(
        &temp,
        "january.csv",
        Some(iban),
        &[&import_row("20.01.2026", "JanShop", "J", "95,00", "-5,00")],
    );
    let empty = multi_csv(&temp, "empty.csv", Some(iban), &[]);
    let today = date!(2026 - 03 - 20);
    let mut vm = view();
    vm.open_import(&db, &[january.clone(), quarter.clone(), empty.clone()], None, today)
        .unwrap();
    let preview = vm.state().import_preview.clone().unwrap();
    assert_eq!(
        preview.entries[0].file, empty,
        "an empty file imports nothing, comes first"
    );
    assert_eq!(preview.entries[1].file, january);
    assert_eq!(preview.newest().file, quarter);
    assert!(preview.balance_update_allowed);
    vm.confirm_import(&db, true, today).unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().balance,
        eur(27_000)
    );
}

// ---------------------------------------------------------------------------
// MultiCurrencyTest.kt (transactions part)
// ---------------------------------------------------------------------------

#[test]
fn empty_totals_fall_back_to_the_primary_currency() {
    let (_temp, db) = database();
    crate::settings::set_setting(&db, crate::money::PRIMARY_CURRENCY_KEY, "USD").unwrap();
    let today = date!(2026 - 06 - 15);
    let mut vm = view();
    vm.load(&db, today).unwrap();
    assert_eq!(vm.state().primary_currency, Currency::new("USD").unwrap());
}

// ---------------------------------------------------------------------------
// RuleDirectionImportTest.kt (view model part)
// ---------------------------------------------------------------------------

#[test]
fn auto_categorize_via_the_view_model_matches_only_the_expense() {
    let (_temp, db) = database();
    let account_id = account(&db, "DE89370400440532013000", 0, Currency::EUR, "checking");
    let groceries = category(&db, "Groceries", false, false);
    repo::rules::create(
        &db,
        NewRule {
            pattern: "Erika Mustermann".to_owned(),
            category_id: groceries,
            priority: 5,
            auto_apply: true,
            match_field: RuleField::Partner,
            direction: RuleDirection::Expense,
            tax_relevant: None,
        },
    )
    .unwrap();
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(-4_567),
        Some("Erika Mustermann"),
        None,
        None,
        false,
    );
    txn!(
        &db,
        account_id,
        date!(2026 - 10 - 03),
        eur(100_000),
        Some("Erika Mustermann"),
        None,
        None,
        false,
    );
    let today = date!(2026 - 10 - 15);
    let mut vm = view();
    vm.auto_categorize(&db, today).unwrap();
    let rows: BTreeMap<i32, Option<CategoryId>> =
        repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .map(|transaction| (transaction.amount.minor.signum() as i32, transaction.category_id))
            .collect();
    assert_eq!(rows[&-1], Some(groceries), "the expense gets the rule's category");
    assert_eq!(rows[&1], None, "the income does not match an EXPENSE-only rule");
}

// ---------------------------------------------------------------------------
// Coroutine timing/race tests deliberately skipped (synchronous API)
// ---------------------------------------------------------------------------
//
// - TransactionsSettingsOrderTest: serializes asynchronous settings writes; synchronous calls already write in
//   order, so the race cannot exist.
// - TransactionsAttachDocumentsTest (both tests): block inside an async upload and switch the panel while it runs;
//   the synchronous API completes the upload before the next call. The sequential panel/document behaviour is covered
//   by `receipts_attached_from_the_detail_panel_are_linked_to_the_transaction`.
// - MultiFileImportTest `a failing file stops the import...`: subclasses the Kotlin import service; the Rust import
//   is a free function without an injection point for a synthetic per-file failure.
// - ViewModelsTest `the panel selection is shared between Accounts and Transactions` uses the Accounts view model;
//   the shared-settings half is covered by `the_panel_selection_is_shared_between_accounts_and_transactions`.
// - UI tests (FiguresHierarchyTest, ScreenshotTest, TransactionSelectionUiTest) exercise the Compose UI, not the
//   state machine; the Slint page is M4d.
