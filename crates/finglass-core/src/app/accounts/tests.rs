use time::macros::date;
use time::{Date, OffsetDateTime};

use super::*;
use crate::model::{BankAccessItem, NewBankConnection, NewTransaction, TransactionId};
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

fn online_bank(db: &Database, name: &str) -> BankAccessItem {
    let created = repo::bank_connections::create(
        db,
        NewBankConnection {
            bank_name: name.to_owned(),
            blz: "37040044".to_owned(),
            bic: Some("COBADEFFXXX".to_owned()),
            user_id: "user".to_owned(),
        },
    )
    .unwrap();
    repo::bank_connections::get_item(db, created.id).unwrap().unwrap()
}

fn account(db: &Database, bank: BankConnectionId, iban: &str, account_type: &str, minor: i64) -> AccountId {
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: Some(bank),
            institution: None,
            name: None,
            iban: Some(iban.to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::from_stored(account_type).unwrap(),
            balance: eur(minor),
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

fn transaction(db: &Database, account_id: AccountId, day: Date, minor: i64, new: bool) -> TransactionId {
    repo::transactions::insert_planned(
        db,
        account_id,
        vec![NewTransaction {
            booking_date: day,
            amount: eur(minor),
            balance_after: None,
            partner_name: Some("Partner".to_owned()),
            purpose: None,
            category_id: None,
            import_hash: format!("hash-{}-{minor}-{day}", account_id.0),
            created_at: OffsetDateTime::now_utc(),
            is_new: new,
            tax_override: None,
        }],
    )
    .unwrap();
    repo::transactions::list_for_account(db, account_id, None, None).unwrap()[0].id
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

#[test]
fn load_accounts_builds_rows_groups_and_per_currency_totals() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let checking = account(&db, bank.id, "DE89370400440532013000", "checking", 100_000);
    account(&db, bank.id, "DE44500105175407324931", "savings", 25_000);
    transaction(&db, checking, date!(2026 - 03 - 01), -500, true);

    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    assert_eq!(state.accounts.len(), 2);
    assert_eq!(state.accounts[0].id, checking);
    assert_eq!(state.accounts[0].bank_name, "Test Bank");
    assert_eq!(state.accounts[0].iban, "DE89370400440532013000");
    assert_eq!(state.accounts[0].new_count, 1);
    assert_eq!(state.primary_currency, Currency::EUR);
    assert_eq!(state.grand_total, vec![(Currency::EUR, 125_000)]);
    assert_eq!(state.totals(), vec![(Currency::EUR, 125_000)]);
    assert_eq!(
        state.groups.iter().map(|group| group.key.clone()).collect::<Vec<_>>(),
        vec![
            GroupKey::Type("checking".to_owned()),
            GroupKey::Type("savings".to_owned()),
        ]
    );
    assert!(!state.is_loading);
}

#[test]
fn selecting_an_account_persists_and_keeps_its_bookings_unread() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let first = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let second = account(&db, bank.id, "DE44500105175407324931", "checking", 0);
    transaction(&db, second, date!(2026 - 03 - 01), -500, true);

    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();
    state.select_accounts(&db, AccountSelection::Account(second)).unwrap();

    assert_eq!(state.selection, AccountSelection::Account(second));
    assert_eq!(
        account_panel::visible_accounts(&state.selection, &state.groups),
        Some(BTreeSet::from([second]))
    );
    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(second, 1)]),
        "the Accounts view never marks bookings read"
    );
    assert_eq!(
        state.accounts.iter().find(|item| item.id == second).unwrap().new_count,
        1
    );

    let mut restored = AccountsState::default();
    restored.load_accounts(&db).unwrap();
    assert_eq!(restored.selection, AccountSelection::Account(second));

    repo::accounts::delete(&db, second).unwrap();
    let mut after_delete = AccountsState::default();
    after_delete.load_accounts(&db).unwrap();
    assert_eq!(
        after_delete.selection,
        AccountSelection::All,
        "a deleted account falls back to All"
    );
    assert_eq!(after_delete.accounts.len(), 1);
    assert_eq!(after_delete.accounts[0].id, first);
}

#[test]
fn group_actions_change_the_database_and_write_one_audit_entry_each() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.create_group(&db, "Reisen").unwrap();
    let first = state.custom_groups[0].id;
    state.create_group(&db, "Haushalt").unwrap();
    state.rename_group(&db, first, "Urlaub").unwrap();
    state.move_group(&db, first, false).unwrap();
    state.move_account_to_group(&db, account_id, Some(first)).unwrap();

    assert_eq!(
        state
            .custom_groups
            .iter()
            .map(|group| group.name.as_str())
            .collect::<Vec<_>>(),
        vec!["Haushalt", "Urlaub"]
    );
    assert_eq!(
        state
            .accounts
            .iter()
            .find(|item| item.id == account_id)
            .unwrap()
            .group_id,
        Some(first)
    );
    assert_eq!(
        state
            .groups
            .iter()
            .find(|group| group.key == GroupKey::Custom(first))
            .unwrap()
            .accounts
            .iter()
            .map(|account| account.id)
            .collect::<Vec<_>>(),
        vec![account_id]
    );

    state
        .select_accounts(&db, AccountSelection::Group(GroupKey::Custom(first)))
        .unwrap();
    state.delete_group(&db, first).unwrap();
    assert_eq!(
        state.selection,
        AccountSelection::All,
        "the deleted selected group falls back to All"
    );
    assert_eq!(
        crate::settings::get_setting(&db, account_panel::SELECTION_KEY)
            .unwrap()
            .as_deref(),
        Some("all")
    );
    assert_eq!(
        state
            .accounts
            .iter()
            .find(|item| item.id == account_id)
            .unwrap()
            .group_id,
        None,
        "the account returns to its type group"
    );

    state.create_group(&db, "   ").unwrap();
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorNameRequired))
    );

    assert_eq!(count_action(&db, "account_group.create"), 2);
    assert_eq!(count_action(&db, "account_group.rename"), 1);
    assert_eq!(count_action(&db, "REORDER_ACCOUNT_GROUP"), 1);
    assert_eq!(count_action(&db, "account.set_group"), 1);
    assert_eq!(count_action(&db, "account_group.delete"), 1);
}

#[test]
fn visible_accounts_follow_the_selection() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let first = account(&db, bank.id, "DE89370400440532013000", "checking", 100_000);
    let second = account(&db, bank.id, "DE44500105175407324931", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    assert_eq!(
        account_panel::visible_accounts(&AccountSelection::All, &state.groups),
        None
    );
    assert_eq!(
        account_panel::visible_accounts(&AccountSelection::Account(second), &state.groups),
        Some(BTreeSet::from([second]))
    );
    assert_eq!(
        account_panel::visible_accounts(
            &AccountSelection::Group(GroupKey::Type("checking".to_owned())),
            &state.groups
        ),
        Some(BTreeSet::from([first, second]))
    );
    state.create_group(&db, "Leer").unwrap();
    let empty = state.custom_groups[0].id;
    assert_eq!(
        account_panel::visible_accounts(&AccountSelection::Group(GroupKey::Custom(empty)), &state.groups),
        Some(BTreeSet::new())
    );
    assert_eq!(
        account_panel::visible_accounts(
            &AccountSelection::Group(GroupKey::Type("depot".to_owned())),
            &state.groups
        ),
        Some(BTreeSet::new()),
        "a vanished group shows nothing, not everything"
    );
}

#[test]
fn collapsed_groups_and_banks_persist_and_refresh_keeps_the_toggle() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    account(&db, bank.id, "DE89370400440532013000", "savings", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state
        .toggle_collapsed(&db, &GroupKey::Type("savings".to_owned()))
        .unwrap();
    state.refresh(&db).unwrap();
    assert_eq!(state.collapsed, BTreeSet::from(["type:savings".to_owned()]));
    assert_eq!(state.collapsed_banks, BTreeSet::new());

    state.toggle_bank_collapsed(&db, bank.id).unwrap();
    assert_eq!(state.collapsed_banks, BTreeSet::from([bank.id]));

    let mut restored = AccountsState::default();
    restored.load_accounts(&db).unwrap();
    assert_eq!(restored.collapsed, BTreeSet::from(["type:savings".to_owned()]));
    assert_eq!(restored.collapsed_banks, BTreeSet::from([bank.id]));

    restored
        .toggle_collapsed(&db, &GroupKey::Type("savings".to_owned()))
        .unwrap();
    restored.toggle_bank_collapsed(&db, bank.id).unwrap();
    assert!(restored.collapsed.is_empty());
    assert!(restored.collapsed_banks.is_empty());
}

#[test]
fn a_collapsed_bank_whose_bank_was_deleted_is_ignored_on_load() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Gone Bank");
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();
    state.toggle_bank_collapsed(&db, bank.id).unwrap();
    repo::bank_connections::delete_with_accounts(&db, bank.id).unwrap();

    let mut restored = AccountsState::default();
    restored.load_accounts(&db).unwrap();
    assert!(restored.collapsed_banks.is_empty());
    assert!(restored.error.is_none());
}

#[test]
fn an_offline_bank_account_accepts_an_iban_or_exactly_four_digits() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let grocery = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();
    let offline = repo::bank_connections::create_offline(&db, "Norwegian Card").unwrap();
    state.refresh(&db).unwrap();

    assert_eq!(state.validate_account("4321", true), None);
    assert_eq!(state.validate_account(" 4321 ", true), None);
    assert_eq!(state.validate_account("NO9386011117947", true), None);
    assert_eq!(state.validate_account("DE44500105175407324931", true), None);
    assert_eq!(
        state.validate_account("5555 44xx xxxx 4321", true),
        Some(i18n::text(Language::En, Key::CardLast4Only).to_owned())
    );
    assert_eq!(
        state.validate_account("1234567812345678", true),
        Some(i18n::text(Language::En, Key::CardLast4Only).to_owned())
    );
    assert_eq!(
        state.validate_account("4321", false),
        Some(i18n::text(Language::En, Key::IbanChecksumFailed).to_owned())
    );
    assert_eq!(
        state.validate_account("DE89370400440532013000", false),
        Some(i18n::text(Language::En, Key::AccountIbanExists).to_owned())
    );
    assert!(state.validate_offline_bank("").is_some());
    assert!(state.validate_offline_bank("   ").is_some());
    assert_eq!(state.validate_offline_bank("Card"), None);
    assert_eq!(offline.bank_name, "Norwegian Card");
    assert!(grocery.0 > 0);
}

#[test]
fn adding_a_card_account_stores_the_synthetic_identifier() {
    let (_temp, db) = database();
    let offline = repo::bank_connections::create_offline(&db, "Norwegian Card").unwrap();
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state
        .add_account(&db, offline.id, " 4321 ", "credit_card", eur(5_000), None)
        .unwrap();

    let account = state.accounts.first().unwrap();
    assert_eq!(account.iban, "CARD4321");
    assert!(account_ids::is_card(&account.iban));
    assert_eq!(account.balance, eur(5_000));
    assert_eq!(state.message.as_deref(), Some("Account added"));
    assert_eq!(count_action(&db, "account.create"), 1);
    assert_eq!(
        state.validate_account("4321", true),
        Some(i18n::text(Language::En, Key::AccountIbanExists).to_owned())
    );

    state
        .add_account(&db, offline.id, "1234567812345678", "credit_card", eur(0), None)
        .unwrap();
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::CardLast4Only))
    );
    assert_eq!(state.accounts.len(), 1);
}

#[test]
fn adding_an_offline_bank_creates_it_offline_without_sync_participation() {
    let (_temp, db) = database();
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.add_offline_bank(&db, "Bank Norwegian").unwrap();
    let bank = state.bank_accesses.first().unwrap();
    assert!(bank.offline);
    assert!(!bank.include_in_sync_all);
    assert_eq!(bank.bank_name, "Bank Norwegian");
    assert_eq!(state.message.as_deref(), Some("Added Bank Norwegian"));
    assert_eq!(count_action(&db, "bank_connection.create_offline"), 1);

    state.add_offline_bank(&db, "   ").unwrap();
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::BankNameRequired))
    );
}

#[test]
fn deleting_an_account_is_refused_while_work_is_busy() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.busy.begin();
    let outcome = state.delete_account(&db, account_id);
    state.busy.end();

    outcome.unwrap();
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorDeleteWhileBusy))
    );
    assert!(repo::accounts::get(&db, account_id).unwrap().is_some());

    state.delete_account(&db, account_id).unwrap();
    assert!(repo::accounts::get(&db, account_id).unwrap().is_none());
    assert_eq!(state.message.as_deref(), Some("Account deleted"));
}

#[test]
fn deleting_an_offline_bank_removes_its_accounts() {
    let (_temp, db) = database();
    let offline = repo::bank_connections::create_offline(&db, "Card").unwrap();
    let account_id = account(&db, offline.id, "CARD4321", "credit_card", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.delete_bank_access(&db, offline.id).unwrap();
    assert!(state.bank_accesses.is_empty());
    assert!(state.accounts.is_empty());
    assert!(repo::accounts::get(&db, account_id).unwrap().is_none());
    assert_eq!(state.message.as_deref(), Some("Bank removed"));
}

#[test]
fn rename_account_stores_the_name_audits_only_a_change_and_updates_the_detail() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.rename_account(&db, account_id, " Haushalt ").unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().name.as_deref(),
        Some("Haushalt")
    );
    assert_eq!(count_action(&db, "account.rename"), 1, "one entry for the first name");

    state.rename_account(&db, account_id, "Haushalt").unwrap();
    assert_eq!(
        count_action(&db, "account.rename"),
        1,
        "renaming to the same name writes nothing"
    );

    state.rename_account(&db, account_id, "   ").unwrap();
    assert_eq!(repo::accounts::get(&db, account_id).unwrap().unwrap().name, None);
    assert_eq!(count_action(&db, "account.rename"), 2, "clearing writes one entry");

    state
        .open_detail_default(&db, account_id, date!(2026 - 03 - 31))
        .unwrap();
    state.rename_account(&db, account_id, "Reisekasse").unwrap();
    assert_eq!(
        state.detail.as_ref().unwrap().account.name.as_deref(),
        Some("Reisekasse"),
        "an open detail view shows the new name"
    );
}

#[test]
fn set_account_type_stores_and_audits_a_valid_type_and_refuses_an_unknown_one() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.set_account_type(&db, account_id, "loan").unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().account_type,
        AccountType::Loan
    );
    let entry = crate::audit::recent(&db, 1).unwrap().remove(0);
    assert_eq!(entry.action, "CHANGE_ACCOUNT_TYPE");
    assert!(
        entry
            .details
            .contains(&format!("Account {}: checking -> loan", account_id.0))
    );

    state.set_account_type(&db, account_id, "zebra").unwrap();
    assert_eq!(
        repo::accounts::get(&db, account_id).unwrap().unwrap().account_type,
        AccountType::Loan,
        "an unknown type changes nothing"
    );
    assert_eq!(count_action(&db, "CHANGE_ACCOUNT_TYPE"), 1, "and writes no audit entry");
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorAccountTypeInvalid))
    );
}

#[test]
fn forgetting_deleted_bookings_audits_the_count_and_messages_the_zero_case() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013020", "checking", 0);
    let booking = transaction(&db, account_id, date!(2026 - 10 - 05), -500, false);
    repo::transactions::delete_all(&db, &[booking]).unwrap();

    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();
    state.forget_deleted_bookings(&db, account_id).unwrap();
    let entry = crate::audit::recent(&db, 1).unwrap().remove(0);
    assert_eq!(entry.action, "FORGET_DELETED_BOOKINGS");
    assert_eq!(entry.details, "Account 3020: 1 deleted bookings may be imported again");
    let expected = i18n::format(Language::En, Key::MessageDeletedForgotten, &[Arg::from(1)]);
    assert_eq!(state.message.as_deref(), Some(expected.as_str()));

    let other = account(&db, bank.id, "DE89370400440532013021", "checking", 0);
    state.refresh(&db).unwrap();
    state.forget_deleted_bookings(&db, other).unwrap();
    assert_eq!(
        count_action(&db, "FORGET_DELETED_BOOKINGS"),
        1,
        "no audit entry when nothing was remembered"
    );
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageNoDeletedRemembered))
    );
}

#[test]
fn open_detail_shows_history_recent_bookings_and_period_totals() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let account_id = account(&db, bank.id, "DE89370400440532013000", "checking", 11_500);
    transaction(&db, account_id, date!(2026 - 03 - 29), 2_000, false);
    transaction(&db, account_id, date!(2026 - 03 - 30), -500, false);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.open_detail(&db, account_id, 5, date!(2026 - 03 - 31)).unwrap();
    let detail = state.detail.as_ref().unwrap();
    assert_eq!(detail.account.id, account_id);
    assert_eq!(detail.days, 5);
    assert_eq!(detail.history.len(), 5);
    assert_eq!(detail.recent.len(), 2);
    assert_eq!(detail.income_in_period, eur(2_000));
    assert_eq!(detail.expenses_in_period, eur(500));
    assert_eq!(
        detail.history.last().unwrap().1,
        eur(11_500),
        "the last point is today's balance"
    );

    state.close_detail();
    assert!(state.detail.is_none());
    state.message = Some("kept".to_owned());
    state.error = Some("kept".to_owned());
    state.clear_messages();
    assert!(state.message.is_none());
    assert!(state.error.is_none());
}

#[test]
fn account_filter_uses_the_trimmed_query() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let first = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    account(&db, bank.id, "DE44500105175407324931", "savings", 0);
    repo::accounts::rename(&db, first, Some("Haushalt".to_owned())).unwrap();
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    assert_eq!(state.filtered_accounts().len(), 2);
    state.update_account_filter_text(" haus ");
    assert_eq!(state.filtered_accounts().len(), 1);
    state.update_account_filter_text("Test Bank");
    assert_eq!(state.filtered_accounts().len(), 2);
    state.update_account_filter_text("4450");
    assert_eq!(state.filtered_accounts().len(), 1);
}

#[test]
fn collapsing_an_offline_bank_is_not_audited() {
    let (_temp, db) = database();
    let offline = repo::bank_connections::create_offline(&db, "Card").unwrap();
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();
    let before = actions(&db).len();
    state.toggle_bank_collapsed(&db, offline.id).unwrap();
    assert_eq!(actions(&db).len(), before);
}

#[test]
fn mark_read_clears_a_groups_accounts_and_all_clears_everything() {
    let (_temp, db) = database();
    let bank = online_bank(&db, "Test Bank");
    let checking = account(&db, bank.id, "DE89370400440532013000", "checking", 0);
    let savings = account(&db, bank.id, "DE44500105175407324931", "savings", 0);
    transaction(&db, checking, date!(2026 - 03 - 01), -100, true);
    transaction(&db, savings, date!(2026 - 03 - 02), -200, true);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state
        .mark_read(&db, &AccountSelection::Group(GroupKey::Type("checking".to_owned())))
        .unwrap();
    assert_eq!(
        repo::transactions::count_new(&db).unwrap(),
        BTreeMap::from([(savings, 1)]),
        "only the group's checking accounts are cleared"
    );
    assert_eq!(
        state
            .accounts
            .iter()
            .find(|item| item.id == checking)
            .unwrap()
            .new_count,
        0
    );

    state.mark_read(&db, &AccountSelection::All).unwrap();
    assert!(repo::transactions::count_new(&db).unwrap().is_empty());
    assert!(state.error.is_none());
    assert!(count_action(&db, "transaction.mark_read") > 0);
}

#[test]
fn deleting_an_offline_bank_is_refused_while_work_is_busy() {
    let (_temp, db) = database();
    let offline = repo::bank_connections::create_offline(&db, "Card").unwrap();
    let account_id = account(&db, offline.id, "CARD4321", "credit_card", 0);
    let mut state = AccountsState::default();
    state.load_accounts(&db).unwrap();

    state.busy.begin();
    let outcome = state.delete_bank_access(&db, offline.id);
    state.busy.end();

    outcome.unwrap();
    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorDeleteWhileBusy))
    );
    assert!(repo::accounts::get(&db, account_id).unwrap().is_some());
    assert_eq!(state.bank_accesses.len(), 1);

    state.delete_bank_access(&db, offline.id).unwrap();
    assert!(state.bank_accesses.is_empty());
}
