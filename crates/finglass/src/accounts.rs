//! Renders `finglass_core::app::accounts` and `account_panel` into the Accounts page.
//!
//! All database work runs on a background thread and comes back through `slint::invoke_from_event_loop`, like the
//! rest of the shell. The adapter keeps the state's [`Busy`] handle taken while a load or action runs, so a second
//! click during the work is ignored.

use std::collections::BTreeSet;
use std::sync::Arc;
use std::thread;

use finglass_core::account_grouping::{
    self, AccountGroupView, AccountItem as GroupingAccount, BalanceTone, GroupKey, LOAN_TYPE,
};
use finglass_core::account_ids;
use finglass_core::app::account_panel::AccountSelection;
use finglass_core::app::accounts::{ACCOUNT_TYPE_CODES, AccountDetail, AccountItem, AccountsState, Busy};
use finglass_core::bank_directory::{self, BankDirectory};
use finglass_core::bank_logo::{bank_logo, monogram};
use finglass_core::db::Database;
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{AccountGroup, AccountGroupId, AccountId, BankAccessItem, BankConnectionId};
use finglass_core::money::{self, Currency, Money};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel};
use time::{Date, OffsetDateTime};

use crate::shell::ShellState;
use crate::{
    AccountAmount, AccountDaysChoice, AccountDetailRow, AccountGroupChoice, AccountPanelRow, AccountTypeChoice,
    AccountsBankCard, AccountsFigure, BankAccountRow, MainWindow,
};

/// The currency codes the add-account dialog offers. Finglass's core has no "common currencies" list; these are the
/// codes the app has always offered in its forms.
const CURRENCY_CODES: [&str; 6] = ["EUR", "USD", "GBP", "CHF", "JPY", "CAD"];

/// The windows FinDesk offers for the balance history.
const DETAIL_DAYS: [i64; 3] = [30, 90, 365];

/// Width of the balance-history chart in logical pixels; the adapter builds its path for this box.
const CHART_WIDTH: f32 = 700.0;
/// Height of the balance-history chart in logical pixels.
const CHART_HEIGHT: f32 = 200.0;
const CHART_PADDING: f32 = 6.0;

/// Ends the busy section even when the worker panics.
struct BusyGuard(Busy);

impl Drop for BusyGuard {
    fn drop(&mut self) {
        self.0.end();
    }
}

/// Applies `state` to the window without touching the database.
pub fn apply(main: &MainWindow, state: &AccountsState, banking: &finglass_core::app::banking::BankingState) {
    let language = state.language;
    let has_content = !state.bank_accesses.is_empty() || !state.accounts.is_empty();

    main.set_accounts_loading(state.is_loading);
    main.set_accounts_show_content(state.is_loading || has_content);
    main.set_accounts_has_content(has_content);
    main.set_accounts_error_text(state.error.clone().unwrap_or_default().into());
    main.set_accounts_message_text(state.message.clone().unwrap_or_default().into());

    main.set_accounts_figures(figures(state, language));
    main.set_accounts_bank_cards(ModelRc::new(VecModel::from(crate::banking::bank_cards(
        state, banking, language,
    ))));
    crate::banking::apply_props(main, state, banking);
    main.set_accounts_panel_rows(ModelRc::new(VecModel::from(panel_rows(state, language))));
    main.set_accounts_group_choices(ModelRc::new(VecModel::from(group_choices(state))));
    main.set_accounts_filter_text(state.account_filter_text.clone().into());
    main.set_accounts_shown_text(shown_text(state, language).into());
    main.set_accounts_type_choices(type_choices(language));
    main.set_accounts_currency_codes(ModelRc::new(VecModel::from(
        CURRENCY_CODES
            .iter()
            .map(|code| (*code).into())
            .collect::<Vec<slint::SharedString>>(),
    )));
    main.set_accounts_days_choices(ModelRc::new(VecModel::from(
        DETAIL_DAYS
            .iter()
            .map(|days| AccountDaysChoice {
                days: i32::try_from(*days).unwrap_or_default(),
                label: days_label(*days, language).into(),
            })
            .collect::<Vec<AccountDaysChoice>>(),
    )));
    main.set_accounts_day_labels(ModelRc::new(VecModel::from(
        DETAIL_DAYS
            .iter()
            .map(|days| SharedString::from(days_label(*days, language)))
            .collect::<Vec<SharedString>>(),
    )));

    apply_detail(main, state, language);
}

fn apply_detail(main: &MainWindow, state: &AccountsState, language: Language) {
    let Some(detail) = &state.detail else {
        main.set_accounts_detail_open(false);
        main.set_accounts_detail_rows(ModelRc::new(VecModel::from(Vec::<AccountDetailRow>::new())));
        main.set_accounts_detail_days(90);
        return;
    };

    main.set_accounts_detail_open(true);
    main.set_accounts_detail_title(detail_title(detail, language).into());
    let badge = account_badge(&detail.account, bank_for(state, &detail.account));
    main.set_accounts_detail_logo(badge.logo.into());
    main.set_accounts_detail_letters(badge.letters.into());
    main.set_accounts_detail_colour(badge.colour);
    main.set_accounts_detail_subtitle(account_identifier(&detail.account).into());
    main.set_accounts_detail_balance(format::money(detail.account.balance, language, false).into());
    main.set_accounts_detail_tone(account_tone(&detail.account).into());
    main.set_accounts_detail_account_id(id_i32(detail.account.id.0));
    main.set_accounts_detail_days(i32::try_from(detail.days).unwrap_or(90));
    main.set_accounts_detail_summary(
        i18n::format(
            language,
            Key::LastDaysSummary,
            &[
                detail.days.into(),
                format::money(detail.income_in_period, language, false).into(),
                format::money(detail.expenses_in_period, language, false).into(),
            ],
        )
        .into(),
    );

    let (path, axis_top, axis_bottom) = balance_path(&detail.history, language);
    main.set_accounts_detail_path(path.into());
    main.set_accounts_detail_axis_top(axis_top.into());
    main.set_accounts_detail_axis_bottom(axis_bottom.into());
    main.set_accounts_detail_rows(ModelRc::new(VecModel::from(detail_rows(detail, language))));
}

/// Loads the accounts, groups and totals on a background thread.
pub fn spawn_load(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(db) = current_db(state) else {
        return;
    };
    {
        let mut accounts = state.accounts.lock().expect("accounts state poisoned");
        accounts.close_detail();
    }
    let Some((mut pending, busy)) = begin_work(state) else {
        return;
    };
    pending.is_loading = true;
    main.set_accounts_dialog("".into());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_loading(true);
    let banking_pending = crate::banking::state_snapshot(state);
    apply(main, &pending, &banking_pending);

    let inputs = crate::banking::service_inputs(state, pending.language);
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let _guard = BusyGuard(busy);
        let (banking, accounts) = if let Some(inputs) = inputs {
            crate::banking::load(&db, &inputs.service(), banking_pending, pending)
        } else {
            let mut pending = pending;
            let _ = pending.load_accounts(&db);
            (banking_pending, pending)
        };
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                let banking = {
                    let mut live = state.banking.lock().expect("banking state poisoned");
                    // A sync may have claimed the state while this page load ran: keep its live fields and only
                    // merge the freshly loaded bank list, so the spinner and the progress are never clobbered.
                    live.language = banking.language;
                    live.banks = banking.banks;
                    live.can_remember_pin = banking.can_remember_pin;
                    live.clone()
                };
                *state.accounts.lock().expect("accounts state poisoned") = accounts.clone();
                apply(&ui, &accounts, &banking);
            }
        });
    });
}

/// Opens the add-offline-bank dialog.
pub fn open_add_bank(main: &MainWindow, state: &Arc<ShellState>) {
    clear_messages(main, state);
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("add-bank".into());
}

/// Opens the add-account dialog for one bank.
pub fn open_add_account(main: &MainWindow, state: &Arc<ShellState>, bank_id: i32) {
    let language = language(state);
    let (offline, bank_name) = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(bank) = accounts
            .bank_accesses
            .iter()
            .find(|bank| bank.id == BankConnectionId(i64::from(bank_id)))
        else {
            return;
        };
        (bank.offline, bank.bank_name.clone())
    };
    let account_type = if offline { "credit_card" } else { "checking" };

    clear_messages(main, state);
    main.set_accounts_dialog_title(i18n::format(language, Key::AddAccountAt, &[Arg::from(bank_name)]).into());
    main.set_accounts_dialog_text(String::new().into());
    main.set_accounts_dialog_offline(offline);
    main.set_accounts_dialog_bank_id(bank_id);
    main.set_accounts_dialog_account_id(-1);
    main.set_accounts_dialog_account_type(account_type.into());
    main.set_accounts_dialog_account_name("".into());
    main.set_accounts_dialog_balance("0".into());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("add-account".into());
}

/// Opens the combined balance-and-type dialog for one account.
pub fn open_balance(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    let (label, balance, account_type) = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(account) = accounts
            .accounts
            .iter()
            .find(|account| account.id == AccountId(i64::from(account_id)))
        else {
            return;
        };
        (
            account_identifier(account),
            money::format_plain(account.balance),
            account.account_type.clone(),
        )
    };

    clear_messages(main, state);
    main.set_accounts_dialog_account_id(account_id);
    main.set_accounts_dialog_account_label(label.into());
    main.set_accounts_dialog_balance(balance.into());
    main.set_accounts_dialog_account_type(account_type.into());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("balance".into());
}

/// Opens the rename dialog for one account.
pub fn open_rename(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    let name = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(account) = accounts
            .accounts
            .iter()
            .find(|account| account.id == AccountId(i64::from(account_id)))
        else {
            return;
        };
        account.name.clone().unwrap_or_default()
    };

    clear_messages(main, state);
    main.set_accounts_dialog_account_id(account_id);
    main.set_accounts_dialog_account_name(name.into());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("rename".into());
}

/// Opens the delete-account confirmation.
pub fn open_delete_account(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    let language = language(state);
    let label = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(account) = accounts
            .accounts
            .iter()
            .find(|account| account.id == AccountId(i64::from(account_id)))
        else {
            return;
        };
        account_identifier(account)
    };

    clear_messages(main, state);
    main.set_accounts_dialog_title(i18n::text(language, Key::DeleteAccountTitle).into());
    main.set_accounts_dialog_text(i18n::format(language, Key::DeleteAccountText, &[Arg::from(label)]).into());
    main.set_accounts_dialog_account_id(account_id);
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("delete-account".into());
}

/// Opens the delete-bank confirmation.
pub fn open_delete_bank(main: &MainWindow, state: &Arc<ShellState>, bank_id: i32) {
    let language = language(state);
    let name = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(bank) = accounts
            .bank_accesses
            .iter()
            .find(|bank| bank.id == BankConnectionId(i64::from(bank_id)))
        else {
            return;
        };
        bank.bank_name.clone()
    };

    clear_messages(main, state);
    main.set_accounts_dialog_title(i18n::format(language, Key::RemoveBankTitle, &[Arg::from(name)]).into());
    main.set_accounts_dialog_text(i18n::text(language, Key::RemoveBankText).into());
    main.set_accounts_dialog_bank_id(bank_id);
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("delete-bank".into());
}

/// Closes the current dialog and clears its error.
pub fn close_dialog(main: &MainWindow) {
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog("".into());
}

/// Creates the offline bank, or keeps the dialog open with the validation message.
pub fn submit_add_bank(main: &MainWindow, state: &Arc<ShellState>, name: &str) {
    let message = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        accounts.validate_offline_bank(name)
    };
    if let Some(message) = message {
        main.set_accounts_dialog_error(message.into());
        return;
    }

    let name = name.to_owned();
    close_dialog(main);
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.add_offline_bank(db, &name);
    });
}

/// Creates the account, or keeps the dialog open with the validation message.
pub struct AddAccountRequest<'a> {
    /// The bank the account is added to.
    pub bank_id: i32,
    /// The optional display name.
    pub name: &'a str,
    /// The IBAN or the card's last four digits.
    pub identifier: &'a str,
    /// The account type code.
    pub account_type: &'a str,
    /// The currency code.
    pub currency: &'a str,
    /// The starting balance as typed.
    pub balance: &'a str,
}

pub fn submit_add_account(main: &MainWindow, state: &Arc<ShellState>, request: &AddAccountRequest<'_>) {
    let bank_id = request.bank_id;
    let name = request.name;
    let identifier = request.identifier;
    let account_type = request.account_type;
    let currency_code = request.currency;
    let balance_text = request.balance;
    let language = language(state);
    let validation = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let offline = accounts
            .bank_accesses
            .iter()
            .find(|bank| bank.id == BankConnectionId(i64::from(bank_id)))
            .is_some_and(|bank| bank.offline);
        accounts.validate_account(identifier, offline)
    };
    if let Some(message) = validation {
        main.set_accounts_dialog_error(message.into());
        return;
    }

    let Ok(currency) = Currency::new(currency_code) else {
        main.set_accounts_dialog_error(i18n::text(language, Key::ErrorUnexpected).into());
        return;
    };
    let Ok(balance) = money::parse_decimal(balance_text, currency) else {
        main.set_accounts_dialog_error(i18n::text(language, Key::BalanceNotANumber).into());
        return;
    };

    let name = name.trim();
    let name = if name.is_empty() { None } else { Some(name.to_owned()) };
    let identifier = identifier.to_owned();
    let account_type = account_type.to_owned();
    close_dialog(main);
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.add_account(
            db,
            BankConnectionId(i64::from(bank_id)),
            &identifier,
            &account_type,
            balance,
            name.as_deref(),
        );
    });
}

/// Stores the balance and/or type, or keeps the dialog open with the validation message.
pub fn submit_balance(
    main: &MainWindow,
    state: &Arc<ShellState>,
    account_id: i32,
    balance_text: &str,
    account_type: &str,
) {
    let language = language(state);
    let (previous_balance, previous_type) = {
        let accounts = state.accounts.lock().expect("accounts state poisoned");
        let Some(account) = accounts
            .accounts
            .iter()
            .find(|account| account.id == AccountId(i64::from(account_id)))
        else {
            return;
        };
        (account.balance, account.account_type.clone())
    };

    let Ok(balance) = money::parse_decimal(balance_text, previous_balance.currency) else {
        main.set_accounts_dialog_error(i18n::text(language, Key::EnterValidBalance).into());
        return;
    };

    let account_type = account_type.to_owned();
    close_dialog(main);
    spawn_action(main, state, move |accounts, db| {
        let now = OffsetDateTime::now_utc();
        if balance != previous_balance {
            let _ = accounts.update_balance(db, AccountId(i64::from(account_id)), balance, now);
        }
        if account_type != previous_type {
            let _ = accounts.set_account_type(db, AccountId(i64::from(account_id)), &account_type);
        }
    });
}

/// Renames the account (a blank name clears it).
pub fn submit_rename(main: &MainWindow, state: &Arc<ShellState>, account_id: i32, name: &str) {
    let name = name.to_owned();
    close_dialog(main);
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.rename_account(db, AccountId(i64::from(account_id)), &name);
    });
}

/// Confirms deleting an account.
pub fn confirm_delete_account(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    close_dialog(main);
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.delete_account(db, AccountId(i64::from(account_id)));
    });
}

/// Confirms deleting a bank. Offline banks go through the accounts state; online banks through the banking state.
pub fn confirm_delete_bank(main: &MainWindow, state: &Arc<ShellState>, bank_id: i32) {
    let offline = state
        .accounts
        .lock()
        .expect("accounts state poisoned")
        .bank_accesses
        .iter()
        .find(|bank| bank.id == BankConnectionId(i64::from(bank_id)))
        .is_some_and(|bank| bank.offline);
    close_dialog(main);
    if offline {
        spawn_action(main, state, move |accounts, db| {
            let _ = accounts.delete_bank_access(db, BankConnectionId(i64::from(bank_id)));
        });
    } else {
        crate::banking::delete_bank(main, state, &state.paths, bank_id);
    }
}

/// Opens the detail view with FinDesk's default window.
pub fn open_detail(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    let today = OffsetDateTime::now_utc().date();
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.open_detail_default(db, AccountId(i64::from(account_id)), today);
    });
}

/// Reopens the detail view for another number of days.
pub fn select_days(main: &MainWindow, state: &Arc<ShellState>, account_id: i32, days: i32) {
    let today = OffsetDateTime::now_utc().date();
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.open_detail(db, AccountId(i64::from(account_id)), i64::from(days), today);
    });
}

/// Closes the detail view.
pub fn close_detail(main: &MainWindow, state: &Arc<ShellState>) {
    let accounts = {
        let mut accounts = state.accounts.lock().expect("accounts state poisoned");
        accounts.close_detail();
        accounts.clone()
    };
    let banking = crate::banking::state_snapshot(state);
    apply(main, &accounts, &banking);
}

/// Stores the panel's free-text filter and rerenders.
pub fn filter_changed(main: &MainWindow, state: &Arc<ShellState>, text: &str) {
    let accounts = {
        let mut accounts = state.accounts.lock().expect("accounts state poisoned");
        accounts.update_account_filter_text(text);
        accounts.clone()
    };
    let banking = crate::banking::state_snapshot(state);
    apply(main, &accounts, &banking);
}

/// Clears the error and success banner.
pub fn dismiss_message(main: &MainWindow, state: &Arc<ShellState>) {
    clear_messages(main, state);
}

/// Selects an account, group or all accounts in the shared panel.
pub fn select(main: &MainWindow, state: &Arc<ShellState>, kind: &str, group_kind: &str, id: i32, account_type: &str) {
    let Some(selection) = selection_from(kind, group_kind, id, account_type) else {
        return;
    };
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.select_accounts(db, selection);
    });
}

/// Collapses or expands one panel group.
pub fn toggle_group(
    main: &MainWindow,
    state: &Arc<ShellState>,
    kind: &str,
    group_kind: &str,
    id: i32,
    account_type: &str,
) {
    let Some(selection) = selection_from(kind, group_kind, id, account_type) else {
        return;
    };
    let AccountSelection::Group(key) = selection else {
        return;
    };
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.toggle_collapsed(db, &key);
    });
}

/// Marks the bookings behind one panel row as read.
pub fn mark_read(
    main: &MainWindow,
    state: &Arc<ShellState>,
    kind: &str,
    group_kind: &str,
    id: i32,
    account_type: &str,
) {
    let Some(selection) = selection_from(kind, group_kind, id, account_type) else {
        return;
    };
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.mark_read(db, &selection);
    });
}

/// Collapses or expands a bank's account rows.
pub fn toggle_bank(main: &MainWindow, state: &Arc<ShellState>, bank_id: i32) {
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.toggle_bank_collapsed(db, BankConnectionId(i64::from(bank_id)));
    });
}

/// Forgets the deleted bookings of one account.
pub fn forget_deleted(main: &MainWindow, state: &Arc<ShellState>, account_id: i32) {
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.forget_deleted_bookings(db, AccountId(i64::from(account_id)));
    });
}

/// Creates a custom group.
pub fn new_group(main: &MainWindow, state: &Arc<ShellState>, name: &str) {
    let name = name.to_owned();
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.create_group(db, &name);
    });
}

/// Renames a custom group.
pub fn rename_group(main: &MainWindow, state: &Arc<ShellState>, id: i32, name: &str) {
    let name = name.to_owned();
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.rename_group(db, AccountGroupId(i64::from(id)), &name);
    });
}

/// Deletes a custom group.
pub fn delete_group(main: &MainWindow, state: &Arc<ShellState>, id: i32) {
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.delete_group(db, AccountGroupId(i64::from(id)));
    });
}

/// Moves a custom group up or down.
pub fn move_group(main: &MainWindow, state: &Arc<ShellState>, id: i32, up: bool) {
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.move_group(db, AccountGroupId(i64::from(id)), up);
    });
}

/// Moves an account into a custom group, or to its type group with -1.
pub fn move_account(main: &MainWindow, state: &Arc<ShellState>, account_id: i32, group_id: i32) {
    let group = if group_id < 0 {
        None
    } else {
        Some(AccountGroupId(i64::from(group_id)))
    };
    spawn_action(main, state, move |accounts, db| {
        let _ = accounts.move_account_to_group(db, AccountId(i64::from(account_id)), group);
    });
}

fn spawn_action(
    main: &MainWindow,
    state: &Arc<ShellState>,
    work: impl FnOnce(&mut AccountsState, &Database) + Send + 'static,
) {
    let Some(db) = current_db(state) else {
        return;
    };
    let Some((mut pending, busy)) = begin_work(state) else {
        return;
    };

    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let _guard = BusyGuard(busy);
        work(&mut pending, &db);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                *state.accounts.lock().expect("accounts state poisoned") = pending.clone();
                let banking = {
                    let mut live = state.banking.lock().expect("banking state poisoned");
                    crate::banking::merge_banks(&mut live, &pending);
                    live.clone()
                };
                apply(&ui, &pending, &banking);
            }
        });
    });
}

fn begin_work(state: &Arc<ShellState>) -> Option<(AccountsState, Busy)> {
    let busy = state.accounts_work.clone();
    if busy.active() {
        return None;
    }
    busy.begin();

    let language = *state.language.lock().expect("language state poisoned");
    let accounts = state.accounts.lock().expect("accounts state poisoned");
    let mut pending = accounts.clone();
    pending.language = language;
    Some((pending, busy))
}

fn current_db(state: &Arc<ShellState>) -> Option<Arc<Database>> {
    state.db.lock().expect("database state poisoned").clone()
}

fn language(state: &Arc<ShellState>) -> Language {
    *state.language.lock().expect("language state poisoned")
}

fn clear_messages(main: &MainWindow, state: &Arc<ShellState>) {
    let accounts = {
        let mut accounts = state.accounts.lock().expect("accounts state poisoned");
        accounts.clear_messages();
        accounts.clone()
    };
    let banking = {
        let mut banking = state.banking.lock().expect("banking state poisoned");
        banking.clear_messages();
        banking.clone()
    };
    apply(main, &accounts, &banking);
}

fn selection_from(kind: &str, group_kind: &str, id: i32, account_type: &str) -> Option<AccountSelection> {
    match kind {
        "all" => Some(AccountSelection::All),
        "group" if group_kind == "custom" => {
            Some(AccountSelection::Group(GroupKey::Custom(AccountGroupId(i64::from(id)))))
        }
        "group" => Some(AccountSelection::Group(GroupKey::Type(account_type.to_owned()))),
        "account" => Some(AccountSelection::Account(AccountId(i64::from(id)))),
        _ => None,
    }
}

fn figures(state: &AccountsState, language: Language) -> ModelRc<AccountsFigure> {
    let all_accounts: Vec<GroupingAccount> = state.accounts.iter().map(grouping).collect();
    let loans: Vec<GroupingAccount> = state
        .accounts
        .iter()
        .filter(|account| account.account_type == LOAN_TYPE)
        .map(grouping)
        .collect();

    let mut figures = Vec::new();
    if !state.accounts.is_empty() {
        let label_key = if loans.is_empty() {
            Key::AccountsTotalLabel
        } else {
            Key::AccountsNetLabel
        };
        figures.push(AccountsFigure {
            label: i18n::format(language, label_key, &[state.accounts.len().into()]).into(),
            values: amounts(&state.grand_total, &all_accounts, language, state.grand_total.len() > 1),
            secondary: false,
        });
    }
    if !loans.is_empty() {
        let totals = account_grouping::grand_total(&loans, state.primary_currency);
        figures.push(AccountsFigure {
            label: i18n::text(language, Key::AccountsLoansLabel).into(),
            values: amounts(&totals, &loans, language, true),
            secondary: true,
        });
    }

    ModelRc::new(VecModel::from(figures))
}

fn amounts(
    totals: &[(Currency, i64)],
    accounts: &[GroupingAccount],
    language: Language,
    plain: bool,
) -> ModelRc<AccountAmount> {
    let rows = totals
        .iter()
        .map(|(currency, minor)| AccountAmount {
            text: format::money(
                Money {
                    minor: *minor,
                    currency: *currency,
                },
                language,
                false,
            )
            .into(),
            tone: if plain {
                "plain".to_owned()
            } else {
                tone_code(account_grouping::balance_tone(accounts, *currency)).to_owned()
            }
            .into(),
        })
        .collect::<Vec<_>>();
    ModelRc::new(VecModel::from(rows))
}

/// The badge fields that the Slint row and card structs carry.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct BadgeFields {
    pub logo: String,
    pub letters: String,
    pub colour: i32,
}

impl BadgeFields {
    pub(crate) fn empty() -> Self {
        Self {
            logo: String::new(),
            letters: String::new(),
            colour: 0,
        }
    }

    fn for_name(name: &str, blz: Option<&str>, bic: Option<&str>) -> Self {
        let monogram = monogram(name);
        Self {
            logo: bank_logo(name, blz, bic)
                .map(|logo| logo.id().to_owned())
                .unwrap_or_default(),
            letters: monogram.letters,
            colour: i32::from(monogram.colour),
        }
    }
}

/// The bank connection of an account, when it has one.
fn bank_for<'a>(state: &'a AccountsState, account: &AccountItem) -> Option<&'a BankAccessItem> {
    account
        .bank_access_id
        .and_then(|id| state.bank_accesses.iter().find(|bank| bank.id == id))
}

/// The badge of an account: from its connection, or from its German IBAN's bank directory entry.
pub(crate) fn account_badge(account: &AccountItem, bank: Option<&BankAccessItem>) -> BadgeFields {
    if let Some(bank) = bank {
        // Offline connections have no BLZ or BIC, so they match by name only.
        let (blz, bic) = if bank.offline {
            (None, None)
        } else {
            (
                Some(bank.blz.as_str()).filter(|blz| !blz.trim().is_empty()),
                bank.bic.as_deref().filter(|bic| !bic.trim().is_empty()),
            )
        };
        return BadgeFields::for_name(&bank.bank_name, blz, bic);
    }
    if !account.bank_name.trim().is_empty() {
        // Offline accounts match by name only; their bank connection has no BLZ or BIC.
        return BadgeFields::for_name(&account.bank_name, None, None);
    }
    let Some(blz) = iban_blz(&account.iban) else {
        return BadgeFields::empty();
    };
    match BankDirectory::bundled().lookup(&blz) {
        Some(entry) => BadgeFields::for_name(&entry.name, Some(&blz), Some(&entry.bic)),
        None => BadgeFields::empty(),
    }
}

/// The BLZ from a German IBAN (characters 5-12), when the identifier is a German IBAN.
fn iban_blz(iban: &str) -> Option<String> {
    let chars: Vec<char> = iban.chars().filter(|character| !character.is_whitespace()).collect();
    if chars.len() < 12 || chars[0] != 'D' || chars[1] != 'E' {
        return None;
    }
    let blz: String = chars[4..12].iter().collect();
    if blz.chars().all(|character| character.is_ascii_digit()) {
        Some(blz)
    } else {
        None
    }
}

/// The badge of a group row, but only when every account in it stands for the same bank.
fn group_badge(state: &AccountsState, group: &AccountGroupView) -> BadgeFields {
    if matches!(group.key, GroupKey::Custom(_)) {
        return BadgeFields::empty();
    }
    let mut first: Option<BadgeFields> = None;
    for grouping in &group.accounts {
        let Some(account) = state.accounts.iter().find(|account| account.id == grouping.id) else {
            continue;
        };
        let badge = account_badge(account, bank_for(state, account));
        match &first {
            Some(existing) if *existing != badge => return BadgeFields::empty(),
            Some(_) => {}
            None => first = Some(badge),
        }
    }
    first.unwrap_or_else(BadgeFields::empty)
}

pub(crate) fn bank_cards(state: &AccountsState, language: Language) -> Vec<AccountsBankCard> {
    let mut cards = Vec::new();
    for bank in &state.bank_accesses {
        let accounts: Vec<&AccountItem> = state
            .accounts
            .iter()
            .filter(|account| account.bank_access_id == Some(bank.id))
            .collect();
        cards.push(bank_card(
            state,
            BankCardSource {
                bank_id: bank.id.0,
                name: &bank.bank_name,
                blz: &bank.blz,
                bic: bank.bic.as_deref(),
                subtitle: bank_subtitle(bank, language),
                central_blz_hint: central_blz_hint(bank, language),
                offline: bank.offline,
                collapsed: state.collapsed_banks.contains(&bank.id),
                accounts: &accounts,
            },
            language,
        ));
    }

    let without_bank: Vec<&AccountItem> = state
        .accounts
        .iter()
        .filter(|account| account.bank_access_id.is_none())
        .collect();
    if !without_bank.is_empty() {
        cards.push(bank_card(
            state,
            BankCardSource {
                bank_id: -1,
                name: i18n::text(language, Key::AccountsWithoutBank),
                blz: "",
                bic: None,
                subtitle: String::new(),
                central_blz_hint: String::new(),
                offline: false,
                collapsed: false,
                accounts: &without_bank,
            },
            language,
        ));
    }

    cards
}

struct BankCardSource<'a> {
    bank_id: i64,
    name: &'a str,
    blz: &'a str,
    bic: Option<&'a str>,
    subtitle: String,
    central_blz_hint: String,
    offline: bool,
    collapsed: bool,
    accounts: &'a [&'a AccountItem],
}

fn bank_card(state: &AccountsState, source: BankCardSource<'_>, language: Language) -> AccountsBankCard {
    let grouping_accounts: Vec<GroupingAccount> = source.accounts.iter().map(|account| grouping(account)).collect();
    let totals = account_grouping::grand_total(&grouping_accounts, state.primary_currency);
    let rows = source
        .accounts
        .iter()
        .map(|account| bank_account_row(state, account, language))
        .collect::<Vec<BankAccountRow>>();
    let badge = if source.bank_id < 0 {
        // The card groups accounts from several banks, so it has no single badge.
        BadgeFields::empty()
    } else if source.offline {
        // Offline connections have no BLZ or BIC, so they match by name only.
        BadgeFields::for_name(source.name, None, None)
    } else {
        BadgeFields::for_name(
            source.name,
            Some(source.blz).filter(|blz| !blz.trim().is_empty()),
            source.bic.filter(|bic| !bic.trim().is_empty()),
        )
    };

    AccountsBankCard {
        bank_id: id_i32(source.bank_id),
        name: source.name.into(),
        subtitle: source.subtitle.into(),
        central_blz_hint: source.central_blz_hint.into(),
        offline: source.offline,
        collapsed: source.collapsed,
        account_count_text: i18n::format(language, Key::AccountCount, &[source.accounts.len().into()]).into(),
        totals: amounts(&totals, &grouping_accounts, language, false),
        accounts: ModelRc::new(VecModel::from(rows)),
        syncing: false,
        sync_step: "".into(),
        sync_enabled: true,
        sync_tooltip: "".into(),
        pin_text: "".into(),
        menu: ModelRc::new(VecModel::from(Vec::<crate::BankMenuItem>::new())),
        logo: badge.logo.into(),
        logo_letters: badge.letters.into(),
        logo_colour: badge.colour,
    }
}

fn bank_subtitle(bank: &BankAccessItem, language: Language) -> String {
    if bank.offline {
        return i18n::text(language, Key::BankOfflineHint).to_owned();
    }

    let mut parts = vec![i18n::format(language, Key::BlzPrefix, &[Arg::from(bank.blz.as_str())])];
    if let Some(bic) = bank.bic.as_deref().map(str::trim).filter(|bic| !bic.is_empty()) {
        parts.push(i18n::format(language, Key::BicPrefix, &[Arg::from(bic)]));
    }
    if let Some(last_sync) = bank.last_sync {
        parts.push(i18n::format(
            language,
            Key::SyncedPrefix,
            &[Arg::from(format::date_time(last_sync))],
        ));
    }
    if let Some(sync_from) = bank.sync_from {
        parts.push(i18n::format(
            language,
            Key::NextSyncFrom,
            &[Arg::from(format::date(sync_from))],
        ));
    }
    if !bank.include_in_sync_all {
        parts.push(i18n::text(language, Key::NotInSyncAll).to_owned());
    }
    parts.join(" · ")
}

fn central_blz_hint(bank: &BankAccessItem, language: Language) -> String {
    if bank.offline || bank.blz.trim().is_empty() {
        return String::new();
    }
    let mapped = bank_directory::fints_blz(BankDirectory::bundled(), &bank.blz);
    if mapped == bank.blz.trim() {
        String::new()
    } else {
        i18n::format(language, Key::CentralBlzHint, &[Arg::from(mapped)])
    }
}

fn bank_account_row(state: &AccountsState, account: &AccountItem, language: Language) -> BankAccountRow {
    let badge = account_badge(account, bank_for(state, account));
    BankAccountRow {
        account_id: id_i32(account.id.0),
        name: account_label(account, language).into(),
        subtitle: account_subtitle(account, language).into(),
        balance_text: format::money(account.balance, language, false).into(),
        tone: account_tone(account).into(),
        new_count: i32::try_from(account.new_count).unwrap_or(i32::MAX),
        logo: badge.logo.into(),
        logo_letters: badge.letters.into(),
        logo_colour: badge.colour,
    }
}

fn panel_rows(state: &AccountsState, language: Language) -> Vec<AccountPanelRow> {
    let visible: BTreeSet<AccountId> = state
        .filtered_accounts()
        .into_iter()
        .map(|account| account.id)
        .collect();
    let filtering = !state.account_filter_text.trim().is_empty();
    let all_accounts: Vec<GroupingAccount> = state.accounts.iter().map(grouping).collect();
    let all_new: i64 = state.accounts.iter().map(|account| account.new_count).sum();

    let mut rows = vec![AccountPanelRow {
        kind: "all".into(),
        group_kind: "".into(),
        account_type: "".into(),
        id: -1,
        label: i18n::text(language, Key::AllAccounts).into(),
        secondary: "".into(),
        detail: "".into(),
        name: "".into(),
        delete_text: "".into(),
        selected: state.selection == AccountSelection::All,
        collapsed: false,
        new_count: i32::try_from(all_new).unwrap_or(i32::MAX),
        totals: amounts(&state.grand_total, &all_accounts, language, state.grand_total.len() > 1),
        indent: false,
        logo: "".into(),
        logo_letters: "".into(),
        logo_colour: 0,
    }];

    for group in &state.groups {
        let matching: Vec<&GroupingAccount> = group
            .accounts
            .iter()
            .filter(|account| visible.contains(&account.id))
            .collect();
        if filtering && matching.is_empty() {
            continue;
        }

        let collapsed = state.collapsed.contains(&group.key.id());
        let selected = match &state.selection {
            AccountSelection::Group(key) => key.id() == group.key.id(),
            _ => false,
        };
        let (group_kind, account_type, id, name) = match &group.key {
            GroupKey::Type(account_type) => ("type", account_type.as_str(), -1, String::new()),
            GroupKey::Custom(group_id) => ("custom", "", id_i32(group_id.0), group.name.clone().unwrap_or_default()),
        };
        let label = group
            .name
            .clone()
            .unwrap_or_else(|| account_type_label(account_type, language));
        let delete_text = if group_kind == "custom" {
            i18n::format(language, Key::DeleteGroupText, &[Arg::from(name.as_str())])
        } else {
            String::new()
        };
        let badge = group_badge(state, group);

        rows.push(AccountPanelRow {
            kind: "group".into(),
            group_kind: group_kind.into(),
            account_type: account_type.into(),
            id,
            label: label.into(),
            secondary: "".into(),
            detail: "".into(),
            name: name.into(),
            delete_text: delete_text.into(),
            selected,
            collapsed,
            new_count: i32::try_from(group.new_count).unwrap_or(i32::MAX),
            totals: amounts(&group.totals, &group.accounts, language, false),
            indent: false,
            logo: badge.logo.into(),
            logo_letters: badge.letters.into(),
            logo_colour: badge.colour,
        });

        if collapsed {
            continue;
        }
        for account in matching {
            let Some(full) = state.accounts.iter().find(|item| item.id == account.id) else {
                continue;
            };
            rows.push(panel_account_row(state, group, account, full, language));
        }
    }

    rows
}

fn panel_account_row(
    state: &AccountsState,
    group: &AccountGroupView,
    account: &GroupingAccount,
    full: &AccountItem,
    language: Language,
) -> AccountPanelRow {
    let secondary = if full.name.is_some() {
        account_digits(full)
            .map(|digits| format!("…{digits}"))
            .unwrap_or_default()
    } else {
        String::new()
    };
    let detail = full
        .last_update
        .map(|updated| i18n::format(language, Key::LastFetched, &[Arg::from(format::date(updated.date()))]))
        .unwrap_or_default();
    let badge = account_badge(full, bank_for(state, full));

    AccountPanelRow {
        kind: "account".into(),
        group_kind: "".into(),
        account_type: account.account_type.clone().into(),
        id: id_i32(account.id.0),
        label: account_label(full, language).into(),
        secondary: secondary.into(),
        detail: detail.into(),
        name: "".into(),
        delete_text: "".into(),
        selected: state.selection == AccountSelection::Account(account.id),
        collapsed: false,
        new_count: i32::try_from(account.new_count).unwrap_or(i32::MAX),
        totals: ModelRc::new(VecModel::from(vec![AccountAmount {
            text: format::money(account.balance, language, false).into(),
            tone: account_group_tone(group, account.balance.currency).into(),
        }])),
        indent: true,
        logo: badge.logo.into(),
        logo_letters: badge.letters.into(),
        logo_colour: badge.colour,
    }
}

fn group_choices(state: &AccountsState) -> Vec<AccountGroupChoice> {
    state
        .custom_groups
        .iter()
        .map(|group: &AccountGroup| AccountGroupChoice {
            id: id_i32(group.id.0),
            name: group.name.clone().into(),
        })
        .collect()
}

fn shown_text(state: &AccountsState, language: Language) -> String {
    if state.account_filter_text.trim().is_empty() {
        return String::new();
    }
    i18n::format(
        language,
        Key::AccountsShown,
        &[state.filtered_accounts().len().into(), state.accounts.len().into()],
    )
}

fn detail_rows(detail: &AccountDetail, language: Language) -> Vec<AccountDetailRow> {
    detail
        .recent
        .iter()
        .map(|transaction| AccountDetailRow {
            date_text: format::date(transaction.date).into(),
            title: transaction.raw_description().into(),
            amount_text: format::money(transaction.amount, language, true).into(),
            expense: transaction.amount.minor < 0,
        })
        .collect()
}

fn detail_title(detail: &AccountDetail, language: Language) -> String {
    let label = account_label(&detail.account, language);
    if detail.account.bank_name.is_empty() {
        label
    } else {
        format!("{} · {label}", detail.account.bank_name)
    }
}

/// Builds the line path and the two axis labels for the balance history.
fn balance_path(history: &[(Date, Money)], language: Language) -> (String, String, String) {
    if history.is_empty() {
        return (String::new(), String::new(), String::new());
    }

    let low = history.iter().map(|(_, money)| money.minor).min().unwrap_or_default();
    let high = history.iter().map(|(_, money)| money.minor).max().unwrap_or_default();
    let currency = history[0].1.currency;
    let span = high - low;

    let mut path = String::new();
    for (index, (_, money)) in history.iter().enumerate() {
        let x = if history.len() > 1 {
            index as f32 / (history.len() - 1) as f32 * CHART_WIDTH
        } else {
            CHART_WIDTH / 2.0
        };
        let ratio = if span == 0 {
            0.5
        } else {
            (money.minor - low) as f32 / span as f32
        };
        let y = CHART_PADDING + (1.0 - ratio) * (CHART_HEIGHT - 2.0 * CHART_PADDING);
        if index == 0 {
            path.push_str(&format!("M {x:.2} {y:.2}"));
        } else {
            path.push_str(&format!(" L {x:.2} {y:.2}"));
        }
    }

    let high_money = Money { minor: high, currency };
    let low_money = Money { minor: low, currency };
    let axis_top = format::whole_money(high_money, language);
    // A flat line has no range: one label is enough, and it sits at the top like every other chart.
    let axis_bottom = if high == low {
        String::new()
    } else {
        format::whole_money(low_money, language)
    };
    (path, axis_top, axis_bottom)
}

fn grouping(account: &AccountItem) -> GroupingAccount {
    GroupingAccount {
        id: account.id,
        name: account.name.clone(),
        currency: account.currency,
        balance: account.balance,
        account_type: account.account_type.clone(),
        group_id: account.group_id,
        new_count: account.new_count,
    }
}

fn account_label(account: &AccountItem, language: Language) -> String {
    if let Some(name) = account.name.as_deref().map(str::trim).filter(|name| !name.is_empty()) {
        return name.to_owned();
    }

    let account_type = account_type_label(&account.account_type, language);
    match account_digits(account) {
        Some(digits) => format!("{account_type} …{digits}"),
        None => account_type,
    }
}

fn account_digits(account: &AccountItem) -> Option<String> {
    let source = if !account.iban.trim().is_empty() {
        account.iban.trim()
    } else {
        account.account_number.as_deref().unwrap_or("").trim()
    };
    if source.is_empty() {
        return None;
    }
    let count = source.chars().count();
    Some(source.chars().skip(count.saturating_sub(4)).collect())
}

fn account_subtitle(account: &AccountItem, language: Language) -> String {
    let display = if !account.iban.trim().is_empty() {
        account_ids::display(account.iban.trim())
    } else {
        account
            .account_number
            .as_deref()
            .map(str::trim)
            .filter(|number| !number.is_empty())
            .map(str::to_owned)
            .unwrap_or_default()
    };
    let mut subtitle = match (account.name.is_some(), display.is_empty()) {
        (true, false) => format!("{} · {display}", account_type_label(&account.account_type, language)),
        (true, true) => account_type_label(&account.account_type, language),
        (false, _) => display,
    };
    if let Some(updated) = account.last_update {
        let fetched = i18n::format(language, Key::LastFetched, &[Arg::from(format::date(updated.date()))]);
        if subtitle.is_empty() {
            subtitle = fetched;
        } else {
            subtitle.push_str(" · ");
            subtitle.push_str(&fetched);
        }
    }
    subtitle
}

fn account_identifier(account: &AccountItem) -> String {
    if !account.iban.trim().is_empty() {
        account_ids::display(account.iban.trim())
    } else {
        account
            .account_number
            .as_deref()
            .map(str::trim)
            .filter(|number| !number.is_empty())
            .map(str::to_owned)
            .unwrap_or_default()
    }
}

fn account_type_key(account_type: &str) -> Option<Key> {
    match account_type {
        "checking" => Some(Key::TypeChecking),
        "savings" => Some(Key::TypeSavings),
        "credit_card" => Some(Key::TypeCreditCard),
        LOAN_TYPE => Some(Key::TypeLoan),
        "depot" => Some(Key::TypeDepot),
        _ => None,
    }
}

fn account_type_label(account_type: &str, language: Language) -> String {
    account_type_key(account_type)
        .map(|key| i18n::text(language, key).to_owned())
        .unwrap_or_else(|| account_type.to_owned())
}

fn account_tone(account: &AccountItem) -> &'static str {
    tone_code(account_grouping::balance_tone_value(
        account.balance,
        account.account_type == LOAN_TYPE,
    ))
}

fn account_group_tone(group: &AccountGroupView, currency: Currency) -> &'static str {
    tone_code(account_grouping::balance_tone(&group.accounts, currency))
}

fn tone_code(tone: BalanceTone) -> &'static str {
    match tone {
        BalanceTone::Positive => "positive",
        BalanceTone::Negative => "negative",
        BalanceTone::Loan => "loan",
        BalanceTone::Zero => "zero",
    }
}

fn type_choices(language: Language) -> ModelRc<AccountTypeChoice> {
    ModelRc::new(VecModel::from(
        ACCOUNT_TYPE_CODES
            .iter()
            .map(|code| AccountTypeChoice {
                code: (*code).into(),
                label: account_type_label(code, language).into(),
            })
            .collect::<Vec<AccountTypeChoice>>(),
    ))
}

fn days_label(days: i64, language: Language) -> String {
    if days == 365 {
        i18n::text(language, Key::OneYear).to_owned()
    } else {
        i18n::format(language, Key::DaysCount, &[days.into()])
    }
}

fn id_i32(id: i64) -> i32 {
    i32::try_from(id).unwrap_or(-1)
}

#[cfg(test)]
mod tests {
    use super::*;
    use finglass_core::model::{TransactionId, TransactionItem};
    use slint::Model;
    use time::macros::{date, datetime};

    fn model_rows<T: Clone>(model: &ModelRc<T>) -> Vec<T> {
        (0..model.row_count())
            .filter_map(|index| model.row_data(index))
            .collect()
    }

    fn currency(code: &str) -> Currency {
        Currency::new(code).unwrap()
    }

    fn money(minor: i64, currency: Currency) -> Money {
        Money { minor, currency }
    }

    #[allow(clippy::too_many_arguments)]
    fn account(
        id: i64,
        name: Option<&str>,
        iban: &str,
        bank: Option<BankConnectionId>,
        account_type: &str,
        currency: Currency,
        balance: i64,
        group_id: Option<AccountGroupId>,
        new_count: i64,
    ) -> AccountItem {
        AccountItem {
            id: AccountId(id),
            bank_access_id: bank,
            bank_name: bank.map(|_| "Test Bank".to_owned()).unwrap_or_default(),
            iban: iban.to_owned(),
            account_number: None,
            balance: money(balance, currency),
            currency,
            account_type: account_type.to_owned(),
            last_update: None,
            name: name.map(str::to_owned),
            group_id,
            new_count,
        }
    }

    fn bank(id: i64, name: &str, offline: bool) -> BankAccessItem {
        BankAccessItem {
            id: BankConnectionId(id),
            bank_name: name.to_owned(),
            blz: if offline { String::new() } else { "37040044".to_owned() },
            bic: if offline {
                Some(String::new())
            } else {
                Some("COBADEFFXXX".to_owned())
            },
            user_id: String::new(),
            tan_method: None,
            server_url: None,
            last_sync: None,
            capabilities: None,
            sync_from: None,
            include_in_sync_all: false,
            offline,
        }
    }

    fn state_with(accounts: Vec<AccountItem>, banks: Vec<BankAccessItem>, groups: Vec<AccountGroup>) -> AccountsState {
        let mut state = AccountsState::new(Language::En);
        state.grand_total =
            account_grouping::grand_total(&accounts.iter().map(grouping).collect::<Vec<_>>(), Currency::EUR);
        state.groups = account_grouping::group(
            &accounts.iter().map(grouping).collect::<Vec<_>>(),
            &groups,
            Currency::EUR,
        );
        state.custom_groups = groups;
        state.accounts = accounts;
        state.bank_accesses = banks;
        state
    }

    fn transaction(day: Date, minor: i64, currency: Currency) -> TransactionItem {
        TransactionItem {
            id: TransactionId(1),
            account_id: AccountId(1),
            account_iban: Some("DE89370400440532013000".to_owned()),
            date: day,
            amount: money(minor, currency),
            balance_after: None,
            currency,
            partner_name: Some("Example Market".to_owned()),
            purpose: None,
            category_id: None,
            category_name: None,
            category_color: None,
            is_tax_relevant: false,
            document_count: 0,
            is_new: false,
            is_transfer: false,
            tax_source: finglass_core::model::TaxSource::None,
            tax_rule_pattern: None,
            tax_override: None,
        }
    }

    #[test]
    fn bank_cards_group_by_connection_and_add_the_accounts_without_a_bank() {
        let offline = BankConnectionId(7);
        let state = state_with(
            vec![
                account(
                    1,
                    None,
                    "DE89370400440532013000",
                    Some(offline),
                    "checking",
                    Currency::EUR,
                    1_000,
                    None,
                    0,
                ),
                account(
                    2,
                    Some("Dollar"),
                    "DE44500105175407324931",
                    Some(offline),
                    "savings",
                    currency("USD"),
                    500,
                    None,
                    0,
                ),
                account(
                    3,
                    None,
                    "DE02120300000000202051",
                    None,
                    "loan",
                    Currency::EUR,
                    -9_000,
                    None,
                    0,
                ),
            ],
            vec![bank(7, "Test Bank", true)],
            vec![],
        );

        let cards = bank_cards(&state, Language::En);

        assert_eq!(cards.len(), 2);
        assert_eq!(cards[0].bank_id, 7);
        assert_eq!(cards[0].name, "Test Bank");
        assert!(cards[0].offline);
        let bank_accounts = model_rows(&cards[0].accounts);
        let bank_totals = model_rows(&cards[0].totals);
        assert_eq!(bank_accounts.len(), 2);
        assert_eq!(bank_totals.len(), 2, "EUR and USD stay apart");
        assert_eq!(bank_totals[0].text, "€10.00");
        assert_eq!(bank_totals[1].text, "$5.00");
        assert_eq!(cards[1].bank_id, -1);
        assert_eq!(cards[1].name, i18n::text(Language::En, Key::AccountsWithoutBank));
        assert_eq!(model_rows(&cards[1].accounts).len(), 1);
    }

    #[test]
    fn bank_badges_follow_the_connection_and_the_iban_directory() {
        let mut sparkasse = bank(8, "Sparkasse", false);
        sparkasse.blz = "37040044".to_owned();
        sparkasse.bic = Some("COLSDE33XXX".to_owned());
        let dkb = bank(9, "DKB Privat", true);
        let norwegian = bank(10, "Bank Norwegian", true);
        let state = state_with(
            vec![
                account(
                    1,
                    Some("Girokonto"),
                    "DE89370400440532013001",
                    Some(sparkasse.id),
                    "checking",
                    Currency::EUR,
                    100,
                    None,
                    0,
                ),
                account(
                    2,
                    Some("Haushalt"),
                    "DE02120300000000202051",
                    Some(dkb.id),
                    "checking",
                    Currency::EUR,
                    200,
                    None,
                    0,
                ),
                account(
                    3,
                    Some("Karte"),
                    "DE02120300000000202051",
                    Some(norwegian.id),
                    "credit_card",
                    Currency::EUR,
                    300,
                    None,
                    0,
                ),
                account(
                    4,
                    Some("Ohne Bank"),
                    "DE02120300000000202051",
                    None,
                    "loan",
                    Currency::EUR,
                    400,
                    None,
                    0,
                ),
            ],
            vec![sparkasse, dkb, norwegian],
            vec![],
        );

        let cards = bank_cards(&state, Language::En);
        let sparkasse_card = cards.iter().find(|card| card.bank_id == 8).unwrap();
        assert_eq!(sparkasse_card.logo, "sparkasse");
        assert_eq!(sparkasse_card.logo_letters, "SP");
        assert_eq!(model_rows(&sparkasse_card.accounts)[0].logo, "sparkasse");

        let dkb_card = cards.iter().find(|card| card.bank_id == 9).unwrap();
        assert_eq!(dkb_card.logo, "dkb");
        assert_eq!(dkb_card.logo_letters, "DKB");

        let norwegian_card = cards.iter().find(|card| card.bank_id == 10).unwrap();
        assert_eq!(norwegian_card.logo, "", "offline banks match by name only");
        assert_eq!(norwegian_card.logo_letters, "BN");

        let without_bank = cards.iter().find(|card| card.bank_id == -1).unwrap();
        assert_eq!(without_bank.logo, "", "a mixed card has no single bank");
        let row = &model_rows(&without_bank.accounts)[0];
        assert_eq!(row.logo, "dkb", "the account falls back to its IBAN's BLZ");

        let rows = panel_rows(&state, Language::En);
        let panel_account = rows.iter().find(|row| row.kind == "account" && row.id == 1).unwrap();
        assert_eq!(panel_account.logo, "sparkasse");
        assert_eq!(panel_account.logo_letters, "SP");
    }

    #[test]
    fn bank_subtitle_lists_each_sync_detail_only_when_it_applies() {
        let mut online = bank(7, "Test Bank", false);
        // The fixture starts outside Sync all, so the hint shows even before a sync ran.
        assert_eq!(
            bank_subtitle(&online, Language::En),
            "BLZ 37040044 · BIC COBADEFFXXX · not in Sync all"
        );
        online.last_sync = Some(datetime!(2026 - 11 - 01 09:05 UTC));
        online.sync_from = Some(date!(2026 - 01 - 01));
        online.include_in_sync_all = true;
        assert_eq!(
            bank_subtitle(&online, Language::En),
            "BLZ 37040044 · BIC COBADEFFXXX · synced 01.11.2026 09:05 · next sync from 01.01.2026"
        );
        online.include_in_sync_all = false;
        online.bic = Some(String::new());
        assert_eq!(
            bank_subtitle(&online, Language::En),
            "BLZ 37040044 · synced 01.11.2026 09:05 · next sync from 01.01.2026 · not in Sync all"
        );
    }

    #[test]
    fn account_labels_follow_findesk_and_use_the_type_name_without_a_name() {
        let unnamed = account(
            1,
            None,
            "DE89370400440532013000",
            None,
            "checking",
            Currency::EUR,
            0,
            None,
            0,
        );
        let named = account(
            2,
            Some("Haushalt"),
            "DE44500105175407324931",
            None,
            "savings",
            Currency::EUR,
            0,
            None,
            0,
        );

        assert_eq!(account_label(&unnamed, Language::En), "Checking …3000");
        assert_eq!(account_label(&named, Language::En), "Haushalt");
        assert_eq!(
            account_subtitle(&named, Language::En),
            "Savings · DE44 5001 0517 5407 3249 31"
        );
        assert_eq!(account_tone(&unnamed), "zero");
    }

    #[test]
    fn panel_rows_show_all_groups_accounts_and_their_totals() {
        let group = AccountGroup {
            id: AccountGroupId(4),
            name: "Reisen".to_owned(),
            sort_order: 1,
        };
        let mut state = state_with(
            vec![
                account(
                    1,
                    Some("Haushalt"),
                    "DE89370400440532013000",
                    None,
                    "checking",
                    Currency::EUR,
                    1_000,
                    None,
                    2,
                ),
                account(
                    2,
                    None,
                    "DE44500105175407324931",
                    None,
                    "savings",
                    Currency::EUR,
                    2_000,
                    Some(AccountGroupId(4)),
                    0,
                ),
            ],
            vec![],
            vec![group],
        );
        state.selection = AccountSelection::Account(AccountId(2));

        let rows = panel_rows(&state, Language::En);

        assert_eq!(rows[0].kind, "all");
        assert_eq!(rows[0].label, "All accounts");
        assert_eq!(rows[0].new_count, 2);
        assert_eq!(model_rows(&rows[0].totals)[0].text, "€30.00");
        assert!(rows.iter().any(|row| row.kind == "group" && row.label == "Reisen"));
        let account_row = rows.iter().find(|row| row.kind == "account" && row.id == 2).unwrap();
        assert!(account_row.selected);
        assert!(account_row.indent);
        assert_eq!(account_row.label, "Savings …4931");
        assert_eq!(model_rows(&account_row.totals)[0].text, "€20.00");
    }

    #[test]
    fn the_filter_hides_group_rows_without_a_match() {
        let mut state = state_with(
            vec![
                account(
                    1,
                    Some("Haushalt"),
                    "DE89370400440532013000",
                    None,
                    "checking",
                    Currency::EUR,
                    0,
                    None,
                    0,
                ),
                account(
                    2,
                    None,
                    "DE44500105175407324931",
                    None,
                    "savings",
                    Currency::EUR,
                    0,
                    None,
                    0,
                ),
            ],
            vec![],
            vec![],
        );
        state.update_account_filter_text("haus");

        let rows = panel_rows(&state, Language::En);

        assert!(rows.iter().any(|row| row.kind == "account" && row.id == 1));
        assert!(!rows.iter().any(|row| row.kind == "account" && row.id == 2));
        assert!(
            !rows
                .iter()
                .any(|row| row.kind == "group" && row.account_type == "savings")
        );
        assert_eq!(shown_text(&state, Language::En), "1 of 2");
    }

    #[test]
    fn the_detail_rows_and_chart_map_the_core_state() {
        let eur = Currency::EUR;
        let mut state = state_with(
            vec![account(
                1,
                None,
                "DE89370400440532013000",
                Some(BankConnectionId(7)),
                "checking",
                eur,
                11_500,
                None,
                0,
            )],
            vec![bank(7, "Test Bank", true)],
            vec![],
        );
        let mut detail = AccountDetail {
            account: state.accounts[0].clone(),
            days: 5,
            history: vec![
                (time::macros::date!(2026 - 03 - 27), money(10_000, eur)),
                (time::macros::date!(2026 - 03 - 29), money(9_000, eur)),
                (time::macros::date!(2026 - 03 - 31), money(11_500, eur)),
            ],
            recent: vec![transaction(time::macros::date!(2026 - 03 - 29), -500, eur)],
            income_in_period: money(2_000, eur),
            expenses_in_period: money(500, eur),
        };
        detail.account.name = Some("Haushalt".to_owned());
        state.detail = Some(detail);

        let rows = detail_rows(state.detail.as_ref().unwrap(), Language::En);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].date_text, "29.03.2026");
        assert_eq!(rows[0].title, "Example Market");
        assert_eq!(rows[0].amount_text, "-€5.00");
        assert!(rows[0].expense);

        let (path, top, bottom) = balance_path(&state.detail.as_ref().unwrap().history, Language::En);
        assert!(path.starts_with('M'));
        assert_eq!(path.matches('L').count(), 2);
        assert_eq!(path.matches('M').count(), 1);
        assert_eq!(top, "€115");
        assert_eq!(bottom, "€90");
        assert_eq!(
            detail_title(state.detail.as_ref().unwrap(), Language::En),
            "Test Bank · Haushalt"
        );
    }

    #[test]
    fn a_flat_balance_history_has_one_axis_label_and_a_centred_line() {
        let eur = Currency::EUR;
        let history = vec![
            (date!(2026 - 09 - 01), money(100, eur)),
            (date!(2026 - 09 - 15), money(100, eur)),
            (date!(2026 - 09 - 30), money(100, eur)),
        ];

        let (path, top, bottom) = balance_path(&history, Language::De);

        assert!(!top.is_empty(), "the flat history still labels its balance");
        assert_eq!(bottom, "", "a flat history does not repeat the same label");
        let middle = CHART_PADDING + 0.5 * (CHART_HEIGHT - 2.0 * CHART_PADDING);
        assert!(
            path.matches(&format!(" {middle:.2}")).count() == history.len(),
            "the flat line stays in the vertical middle: {path}"
        );
    }

    #[test]
    fn selection_from_round_trips_the_panel_row_kinds() {
        assert_eq!(selection_from("all", "", -1, ""), Some(AccountSelection::All));
        assert_eq!(
            selection_from("group", "type", -1, "savings"),
            Some(AccountSelection::Group(GroupKey::Type("savings".to_owned())))
        );
        assert_eq!(
            selection_from("group", "custom", 4, ""),
            Some(AccountSelection::Group(GroupKey::Custom(AccountGroupId(4))))
        );
        assert_eq!(
            selection_from("account", "", 9, ""),
            Some(AccountSelection::Account(AccountId(9)))
        );
        assert_eq!(selection_from("nonsense", "", 0, ""), None);
    }

    #[test]
    fn type_choices_and_day_labels_follow_the_dialog_choices() {
        let choices = type_choices(Language::De);
        let rows: Vec<AccountTypeChoice> = (0..choices.row_count())
            .filter_map(|index| choices.row_data(index))
            .collect();
        assert_eq!(rows.len(), ACCOUNT_TYPE_CODES.len());
        assert_eq!(rows[2].code, "credit_card");
        assert_eq!(rows[2].label, "Kreditkarte");
        assert_eq!(days_label(30, Language::En), "30 days");
        assert_eq!(days_label(365, Language::En), "1 year");
        assert_eq!(account_type_label("credit_card", Language::De), "Kreditkarte");
    }
}
