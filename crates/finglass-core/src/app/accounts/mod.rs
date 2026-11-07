//! The Accounts view's state without any bank login.
//!
//! This ports the offline part of FinDesk's `AccountsViewModel`: accounts, bank-card groups, selection, collapsed
//! banks and groups, group actions, the account form's validation, balance and type changes, renames, deletes,
//! "allow deleted bookings again" and the account detail. Everything that needs a bank connection is left out and
//! comes in M5c on top of `sync::service`.
//!
//! The sync fields of FinDesk's `AccountsState` are deliberately absent:
//! `syncingBankId`, `syncAll`, `syncStep`, `savedPins` and `canRememberPin`.

pub mod busy;

pub use busy::Busy;

use std::collections::{BTreeMap, BTreeSet};

use time::{Date, Duration, OffsetDateTime};

use crate::account_grouping::{self, ACCOUNT_TYPES, AccountGroupView, AccountItem as GroupingAccount, GroupKey};
use crate::account_ids;
use crate::app::account_panel::{self, AccountGroupActions};
pub use crate::app::account_panel::{AccountSelection, AppError};
use crate::balance::BalanceHistory;
use crate::db::Database;
use crate::i18n::{self, Arg, Key, Language};
use crate::iban;
use crate::model::{
    AccountGroup, AccountGroupId, AccountId, AccountType, BankAccessItem, BankConnectionId, NewAccount,
    TransactionFilter, TransactionItem,
};
use crate::money::{Currencies, Currency, Money};
use crate::repo;

/// The settings key holding the collapsed bank connection ids, as FinDesk stores it.
pub const COLLAPSED_BANKS_KEY: &str = "accounts.collapsedBanks";

/// Length of FinDesk's account name column.
pub const MAX_ACCOUNT_NAME_LENGTH: usize = 255;

/// One account as the Accounts page shows it, with the bank name and the unread counter.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountItem {
    /// The row id.
    pub id: AccountId,
    /// The bank connection, when this is an online account.
    pub bank_access_id: Option<BankConnectionId>,
    /// The bank connection's display name; empty when there is none.
    pub bank_name: String,
    /// The IBAN or the synthetic card id; empty when the account has neither.
    pub iban: String,
    /// The account number, when known.
    pub account_number: Option<String>,
    /// The latest known balance.
    pub balance: Money,
    /// The account currency.
    pub currency: Currency,
    /// The account type text.
    pub account_type: String,
    /// When the balance was last updated.
    pub last_update: Option<OffsetDateTime>,
    /// The user's own label, when set.
    pub name: Option<String>,
    /// The custom group, when the account sits in one.
    pub group_id: Option<AccountGroupId>,
    /// Bookings a bank sync brought in that the user has not read yet.
    pub new_count: i64,
}

impl AccountItem {
    /// The grouping item with the same identity and unread counter.
    fn grouping(&self) -> GroupingAccount {
        GroupingAccount {
            id: self.id,
            name: self.name.clone(),
            currency: self.currency,
            balance: self.balance,
            account_type: self.account_type.clone(),
            group_id: self.group_id,
            new_count: self.new_count,
        }
    }

    /// Whether this account matches the panel's free-text filter.
    pub fn matches_filter(&self, query: &str) -> bool {
        account_panel::matches_filter(self.name.as_deref(), &self.iban, &self.bank_name, query)
    }
}

/// The detail view: balance history over a window plus recent bookings and their period totals.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountDetail {
    /// The account as it was loaded.
    pub account: AccountItem,
    /// The window length in days.
    pub days: i64,
    /// End-of-day balances from the first day of the window through today.
    pub history: Vec<(Date, Money)>,
    /// Up to 15 newest bookings inside the window.
    pub recent: Vec<TransactionItem>,
    /// Money in during the window.
    pub income_in_period: Money,
    /// Money out during the window, as a positive amount.
    pub expenses_in_period: Money,
}

/// The Accounts page state.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountsState {
    /// Every account, in repository id order.
    pub accounts: Vec<AccountItem>,
    /// Every bank connection.
    pub bank_accesses: Vec<BankAccessItem>,
    /// Whether a load is running.
    pub is_loading: bool,
    /// The open detail view, if any.
    pub detail: Option<AccountDetail>,
    /// The current error text.
    pub error: Option<String>,
    /// The current success text.
    pub message: Option<String>,
    /// The profile's primary currency.
    pub primary_currency: Currency,
    /// Bank ids whose account rows are collapsed.
    pub collapsed_banks: BTreeSet<BankConnectionId>,
    /// What the account panel filters by; shared with Transactions through the stored selection.
    pub selection: AccountSelection,
    /// The account panel's groups, in display order.
    pub groups: Vec<AccountGroupView>,
    /// The grand total of every account, per currency, primary first.
    pub grand_total: Vec<(Currency, i64)>,
    /// The ids of the collapsed panel groups.
    pub collapsed: BTreeSet<String>,
    /// The free-text filter over the panel's account rows.
    pub account_filter_text: String,
    /// The custom groups as loaded, for rename, move and delete.
    pub custom_groups: Vec<AccountGroup>,
    /// The language of the state's user-visible texts.
    pub language: Language,
    /// The profile's busy flag; the app replaces it with its shared one (see [`Busy`]).
    pub busy: Busy,
    /// Whether the panel's settings were read already.
    panel_loaded: bool,
    /// Whether the collapsed bank ids were read already.
    collapsed_banks_loaded: bool,
}

impl AccountsState {
    /// Builds empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            accounts: Vec::new(),
            bank_accesses: Vec::new(),
            is_loading: false,
            detail: None,
            error: None,
            message: None,
            primary_currency: Currencies::DEFAULT,
            collapsed_banks: BTreeSet::new(),
            selection: AccountSelection::All,
            groups: Vec::new(),
            grand_total: Vec::new(),
            collapsed: BTreeSet::new(),
            account_filter_text: String::new(),
            custom_groups: Vec::new(),
            language,
            busy: Busy::default(),
            panel_loaded: false,
            collapsed_banks_loaded: false,
        }
    }

    /// Balances per currency, primary first; different currencies are never added up.
    #[must_use]
    pub fn totals(&self) -> Vec<(Currency, i64)> {
        Currencies::totals(
            &self.accounts,
            self.primary_currency,
            |account| account.currency,
            |account| account.balance.minor,
        )
    }

    /// The account rows the free-text filter leaves visible.
    #[must_use]
    pub fn filtered_accounts(&self) -> Vec<&AccountItem> {
        self.accounts
            .iter()
            .filter(|account| account.matches_filter(&self.account_filter_text))
            .collect()
    }

    /// Loads the accounts, groups and totals, like FinDesk's `loadAccounts`.
    pub fn load_accounts(&mut self, db: &Database) -> Result<(), AppError> {
        self.is_loading = true;
        self.error = None;
        match self.refresh(db) {
            Ok(()) => Ok(()),
            Err(error) => {
                self.record_error(&error);
                Err(error)
            }
        }
    }

    /// Reloads the repository data. The first load reads the panel settings; later loads keep the in-memory values.
    pub fn refresh(&mut self, db: &Database) -> Result<(), AppError> {
        let mut accounts = repo::accounts::list(db)?;
        accounts.sort_by_key(|account| account.id.0);
        let bank_accesses = repo::bank_connections::list_items(db)?;
        let mut custom_groups = repo::account_groups::list(db)?;
        custom_groups.sort_by_key(|group| (group.sort_order, group.id.0));
        let primary = Currencies::primary(db)?;
        let new_counts = repo::transactions::count_new(db)?;

        let bank_names: BTreeMap<BankConnectionId, String> = bank_accesses
            .iter()
            .map(|bank| (bank.id, bank.bank_name.clone()))
            .collect();

        let accounts: Vec<AccountItem> = accounts
            .into_iter()
            .map(|account| AccountItem {
                id: account.id,
                bank_access_id: account.bank_connection_id,
                bank_name: account
                    .bank_connection_id
                    .and_then(|id| bank_names.get(&id).cloned())
                    .unwrap_or_default(),
                iban: account.iban.unwrap_or_default(),
                account_number: account.account_number,
                balance: account.balance,
                currency: account.currency,
                account_type: account.account_type.as_str().to_owned(),
                last_update: account.balance_updated_at,
                name: account.name,
                group_id: account.group_id,
                new_count: new_counts.get(&account.id).copied().unwrap_or(0),
            })
            .collect();
        let grouping_accounts: Vec<GroupingAccount> = accounts.iter().map(AccountItem::grouping).collect();
        let groups = account_grouping::group(&grouping_accounts, &custom_groups, primary);
        let grand_total = Currencies::totals(
            &accounts,
            primary,
            |account| account.currency,
            |account| account.balance.minor,
        );

        let first_panel_load = !self.panel_loaded;
        self.panel_loaded = true;
        let candidate = if first_panel_load {
            account_panel::load_selection(db, &groups, &grouping_accounts)?
        } else {
            self.selection.clone()
        };
        let selection = account_panel::resolve(&candidate, &groups, &grouping_accounts);
        let loaded_collapsed = if first_panel_load {
            Some(account_panel::load_collapsed(db)?)
        } else {
            None
        };
        let loaded_collapsed_banks = if self.collapsed_banks_loaded {
            None
        } else {
            self.collapsed_banks_loaded = true;
            Some(load_collapsed_banks(db, &bank_accesses)?)
        };

        self.accounts = accounts;
        self.bank_accesses = bank_accesses;
        self.custom_groups = custom_groups;
        self.groups = groups;
        self.grand_total = grand_total;
        self.selection = selection;
        if let Some(collapsed) = loaded_collapsed {
            self.collapsed = collapsed;
        }
        if let Some(collapsed_banks) = loaded_collapsed_banks {
            self.collapsed_banks = collapsed_banks;
        }
        self.primary_currency = primary;
        self.is_loading = false;
        Ok(())
    }

    /// Collapses or expands a bank connection's account rows; view state only, persisted but not audited.
    pub fn toggle_bank_collapsed(&mut self, db: &Database, bank_access_id: BankConnectionId) -> Result<(), AppError> {
        let next = if self.collapsed_banks.contains(&bank_access_id) {
            self.collapsed_banks
                .iter()
                .copied()
                .filter(|id| *id != bank_access_id)
                .collect()
        } else {
            self.collapsed_banks
                .iter()
                .copied()
                .chain(std::iter::once(bank_access_id))
                .collect()
        };
        self.collapsed_banks = next;
        self.store_collapsed_banks(db)
    }

    /// Selects an account, group or all accounts and stores it for both screens. Never marks bookings read.
    pub fn select_accounts(&mut self, db: &Database, selection: AccountSelection) -> Result<(), AppError> {
        self.selection = selection;
        match account_panel::save_selection(db, &self.selection) {
            Ok(()) => Ok(()),
            Err(error) => {
                self.record_error(&error);
                Err(error)
            }
        }
    }

    /// Collapses or expands a panel group; view state only, persisted but not audited.
    pub fn toggle_collapsed(&mut self, db: &Database, key: &GroupKey) -> Result<(), AppError> {
        let id = key.id();
        if !self.collapsed.remove(&id) {
            self.collapsed.insert(id);
        }
        match account_panel::save_collapsed(db, &self.collapsed) {
            Ok(()) => Ok(()),
            Err(error) => {
                self.record_error(&error);
                Err(error)
            }
        }
    }

    /// Filters the panel's account rows by name, identifier or bank.
    pub fn update_account_filter_text(&mut self, text: &str) {
        self.account_filter_text = text.to_owned();
    }

    /// Creates a custom group and reloads the state.
    pub fn create_group(&mut self, db: &Database, name: &str) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.create(db, name) {
            Ok(_) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// Renames a custom group and reloads the state.
    pub fn rename_group(&mut self, db: &Database, id: AccountGroupId, name: &str) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.rename(db, id, name) {
            Ok(()) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// Deletes a group, resets the selection to All when it was selected, and reloads the state.
    pub fn delete_group(&mut self, db: &Database, id: AccountGroupId) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.delete(db, id, &self.selection) {
            Ok(true) => {
                self.selection = AccountSelection::All;
                self.refresh(db)
            }
            Ok(false) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// Moves a group up or down and reloads the state.
    pub fn move_group(&mut self, db: &Database, id: AccountGroupId, up: bool) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.move_up_or_down(db, id, up) {
            Ok(_) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// Moves an account into a custom group, or back to its type group with `None`.
    pub fn move_account_to_group(
        &mut self,
        db: &Database,
        account_id: AccountId,
        group_id: Option<AccountGroupId>,
    ) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.move_account(db, account_id, group_id) {
            Ok(()) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// "Mark as read" from the panel's context menu; the repository records the change and the state reloads.
    pub fn mark_read(&mut self, db: &Database, selection: &AccountSelection) -> Result<(), AppError> {
        let groups = self.groups.clone();
        let actions = AccountGroupActions::new();
        match actions.mark_read(db, selection, &groups) {
            Ok(()) => self.refresh(db),
            Err(error) => self.action_error(error),
        }
    }

    /// Validates an offline bank's name; the only field an offline bank has.
    #[must_use]
    pub fn validate_offline_bank(&self, name: &str) -> Option<String> {
        if name.trim().is_empty() {
            Some(i18n::text(self.language, Key::BankNameRequired).to_owned())
        } else {
            None
        }
    }

    /// Adds a bank that never goes online.
    pub fn add_offline_bank(&mut self, db: &Database, name: &str) -> Result<(), AppError> {
        if let Some(message) = self.validate_offline_bank(name) {
            self.error = Some(message);
            return Ok(());
        }
        let name = name.trim();
        match repo::bank_connections::create_offline(db, name) {
            Ok(_) => {
                self.refresh(db)?;
                self.message = Some(i18n::format(self.language, Key::MessageAdded, &[Arg::from(name)]));
                self.error = None;
                Ok(())
            }
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Validates an account identifier.
    ///
    /// For an online bank a valid IBAN is required. For an offline bank the identifier may also be the card's last 4
    /// digits; more than 4 digits (a pasted card number) is refused, never truncated.
    #[must_use]
    pub fn validate_account(&self, identifier: &str, offline: bool) -> Option<String> {
        let normalized = iban::normalize(identifier);
        let duplicate = |id: &str| self.accounts.iter().any(|account| account.iban == id);

        if iban::is_valid(&normalized) {
            return if duplicate(&normalized) {
                Some(i18n::text(self.language, Key::AccountIbanExists).to_owned())
            } else {
                None
            };
        }

        if !offline {
            return if normalized.is_empty() {
                Some(i18n::text(self.language, Key::IbanRequired).to_owned())
            } else {
                Some(i18n::text(self.language, Key::IbanChecksumFailed).to_owned())
            };
        }

        let digits = identifier.trim();
        if normalized.is_empty() {
            Some(i18n::text(self.language, Key::IbanRequired).to_owned())
        } else if digits.chars().count() == 4 && digits.chars().all(|character| character.is_ascii_digit()) {
            if duplicate(&account_ids::card(digits)) {
                Some(i18n::text(self.language, Key::AccountIbanExists).to_owned())
            } else {
                None
            }
        } else {
            Some(i18n::text(self.language, Key::CardLast4Only).to_owned())
        }
    }

    /// Creates an account; `name` is an optional display name, already trimmed and capped by the repository column.
    pub fn add_account(
        &mut self,
        db: &Database,
        bank_access_id: BankConnectionId,
        identifier: &str,
        account_type: &str,
        balance: Money,
        name: Option<&str>,
    ) -> Result<(), AppError> {
        let offline = self
            .bank_accesses
            .iter()
            .find(|bank| bank.id == bank_access_id)
            .is_some_and(|bank| bank.offline);
        if let Some(message) = self.validate_account(identifier, offline) {
            self.error = Some(message);
            return Ok(());
        }

        let normalized = iban::normalize(identifier);
        let id = if offline && !iban::is_valid(&normalized) {
            account_ids::card(identifier)
        } else {
            normalized
        };
        let Some(account_type) = AccountType::from_stored(account_type) else {
            self.error = Some(i18n::text(self.language, Key::ErrorAccountTypeInvalid).to_owned());
            return Ok(());
        };

        match repo::accounts::create(
            db,
            NewAccount {
                bank_connection_id: Some(bank_access_id),
                institution: None,
                name: normalize_account_name(name),
                iban: Some(id),
                account_number: None,
                currency: balance.currency,
                account_type,
                balance,
                balance_updated_at: None,
                group_id: None,
            },
        ) {
            Ok(_) => {
                self.refresh(db)?;
                self.message = Some(i18n::text(self.language, Key::MessageAccountAdded).to_owned());
                self.error = None;
                Ok(())
            }
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Updates an account's balance and its update time.
    pub fn update_balance(
        &mut self,
        db: &Database,
        account_id: AccountId,
        balance: Money,
        now: OffsetDateTime,
    ) -> Result<(), AppError> {
        match repo::accounts::set_balance(db, account_id, balance, now) {
            Ok(()) => self.refresh(db),
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Changes an account's type by hand; only one of [`ACCOUNT_TYPES`] is accepted.
    pub fn set_account_type(
        &mut self,
        db: &Database,
        account_id: AccountId,
        account_type: &str,
    ) -> Result<(), AppError> {
        if !ACCOUNT_TYPES.contains(&account_type) {
            self.error = Some(i18n::text(self.language, Key::ErrorAccountTypeInvalid).to_owned());
            return Ok(());
        }
        if !self.accounts.iter().any(|account| account.id == account_id) {
            self.error = Some(i18n::text(self.language, Key::ErrorAccountNotFound).to_owned());
            return Ok(());
        }
        let Some(account_type) = AccountType::from_stored(account_type) else {
            self.error = Some(i18n::text(self.language, Key::ErrorAccountTypeInvalid).to_owned());
            return Ok(());
        };

        match repo::accounts::set_account_type(db, account_id, account_type) {
            Ok(_) => self.refresh(db),
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Sets the account's display name (blank clears it). Only a change to the stored name is audit-logged.
    pub fn rename_account(&mut self, db: &Database, account_id: AccountId, name: &str) -> Result<(), AppError> {
        let previous = repo::accounts::get(db, account_id)?.and_then(|account| account.name);
        let normalized = normalize_account_name(Some(name));
        if normalized != previous {
            repo::accounts::rename(db, account_id, normalized)?;
        }
        self.refresh(db)?;

        let updated = self.accounts.iter().find(|account| account.id == account_id).cloned();
        if let (Some(detail), Some(updated)) = (self.detail.as_mut(), updated) {
            detail.account = updated;
        }
        Ok(())
    }

    /// Deletes an account and its bookings, unless work that may still insert rows is running.
    pub fn delete_account(&mut self, db: &Database, account_id: AccountId) -> Result<(), AppError> {
        if self.busy.active() {
            self.error = Some(i18n::text(self.language, Key::ErrorDeleteWhileBusy).to_owned());
            return Ok(());
        }
        match repo::accounts::delete(db, account_id) {
            Ok(()) => {
                self.refresh(db)?;
                self.message = Some(i18n::text(self.language, Key::MessageAccountDeleted).to_owned());
                self.error = None;
                Ok(())
            }
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Forgets the bookings of `account_id` remembered as deleted, so a later import may bring them back.
    pub fn forget_deleted_bookings(&mut self, db: &Database, account_id: AccountId) -> Result<(), AppError> {
        let count = match repo::transactions::forget_deleted_bookings(db, account_id) {
            Ok(count) => count,
            Err(error) => return self.action_error(AppError::from(error)),
        };
        self.refresh(db)?;
        if count > 0 {
            self.message = Some(i18n::format(
                self.language,
                Key::MessageDeletedForgotten,
                &[Arg::from(count)],
            ));
        } else {
            self.message = Some(i18n::text(self.language, Key::MessageNoDeletedRemembered).to_owned());
        }
        self.error = None;
        Ok(())
    }

    /// Deletes an offline bank and its accounts. Online connections belong to M5c.
    pub fn delete_bank_access(&mut self, db: &Database, bank_access_id: BankConnectionId) -> Result<(), AppError> {
        let Some(bank) = self.bank_accesses.iter().find(|bank| bank.id == bank_access_id) else {
            return Ok(());
        };
        if !bank.offline {
            return Ok(());
        }
        if self.busy.active() {
            self.error = Some(i18n::text(self.language, Key::ErrorDeleteWhileBusy).to_owned());
            return Ok(());
        }

        match repo::bank_connections::delete_with_accounts(db, bank_access_id) {
            Ok(_) => {
                self.refresh(db)?;
                self.message = Some(i18n::text(self.language, Key::MessageBankRemoved).to_owned());
                self.error = None;
                Ok(())
            }
            Err(error) => self.action_error(AppError::from(error)),
        }
    }

    /// Opens the detail view: balance history over `days` days plus recent bookings.
    pub fn open_detail(
        &mut self,
        db: &Database,
        account_id: AccountId,
        days: i64,
        today: Date,
    ) -> Result<(), AppError> {
        let Some(account) = self.accounts.iter().find(|account| account.id == account_id).cloned() else {
            return Ok(());
        };
        let from = today - Duration::days(days - 1);
        let filter = TransactionFilter {
            start_date: Some(from),
            account_id: Some(account_id),
            ..TransactionFilter::default()
        };
        let transactions = repo::transactions::find_items(db, &filter, None)?;
        let history = BalanceHistory::compute(account.balance, &transactions, from, today);
        let in_window: Vec<&TransactionItem> = transactions
            .iter()
            .filter(|transaction| transaction.date <= today)
            .collect();
        let currency = account.balance.currency;
        let income: i64 = in_window
            .iter()
            .filter(|transaction| transaction.amount.minor > 0)
            .map(|transaction| transaction.amount.minor)
            .sum();
        let expenses: i64 = in_window
            .iter()
            .filter(|transaction| transaction.amount.minor < 0)
            .map(|transaction| transaction.amount.minor.abs())
            .sum();

        self.detail = Some(AccountDetail {
            account,
            days,
            history,
            recent: in_window.into_iter().take(15).cloned().collect(),
            income_in_period: Money {
                minor: income,
                currency,
            },
            expenses_in_period: Money {
                minor: expenses,
                currency,
            },
        });
        Ok(())
    }

    /// Opens the detail view with FinDesk's default window of 90 days.
    pub fn open_detail_default(&mut self, db: &Database, account_id: AccountId, today: Date) -> Result<(), AppError> {
        self.open_detail(db, account_id, 90, today)
    }

    /// Closes the detail view.
    pub fn close_detail(&mut self) {
        self.detail = None;
    }

    /// Clears the error and success message.
    pub fn clear_messages(&mut self) {
        self.error = None;
        self.message = None;
    }

    fn store_collapsed_banks(&mut self, db: &Database) -> Result<(), AppError> {
        let value = self
            .collapsed_banks
            .iter()
            .map(|id| id.0.to_string())
            .collect::<Vec<_>>()
            .join(",");
        match crate::settings::set_setting(db, COLLAPSED_BANKS_KEY, &value) {
            Ok(()) => Ok(()),
            Err(error) => {
                let error = AppError::from(error);
                self.record_error(&error);
                Err(error)
            }
        }
    }

    fn action_error(&mut self, error: AppError) -> Result<(), AppError> {
        if let AppError::Message(key) = &error {
            self.error = Some(i18n::text(self.language, *key).to_owned());
            return Ok(());
        }
        self.record_error(&error);
        Err(error)
    }

    fn record_error(&mut self, error: &AppError) {
        self.is_loading = false;
        let key = match error {
            AppError::Message(key) => *key,
            AppError::Repo(_) | AppError::Db(_) => Key::ErrorUnexpected,
        };
        self.error = Some(i18n::text(self.language, key).to_owned());
    }
}

impl Default for AccountsState {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

/// Reads the stored collapsed bank ids, minus any that no longer name a bank.
fn load_collapsed_banks(
    db: &Database,
    bank_accesses: &[BankAccessItem],
) -> Result<BTreeSet<BankConnectionId>, AppError> {
    let Some(raw) = crate::settings::get_setting(db, COLLAPSED_BANKS_KEY)? else {
        return Ok(BTreeSet::new());
    };
    let known: BTreeSet<BankConnectionId> = bank_accesses.iter().map(|bank| bank.id).collect();
    Ok(raw
        .split(',')
        .filter_map(|value| value.trim().parse::<i64>().ok())
        .map(BankConnectionId)
        .filter(|id| known.contains(id))
        .collect())
}

/// Trims and caps an account name; a blank name clears it.
fn normalize_account_name(name: Option<&str>) -> Option<String> {
    name.map(str::trim)
        .filter(|name| !name.is_empty())
        .map(|name| name.chars().take(MAX_ACCOUNT_NAME_LENGTH).collect())
}

/// Re-exported for callers that only need the account-type list.
pub use crate::account_grouping::ACCOUNT_TYPES as ACCOUNT_TYPE_CODES;

#[cfg(test)]
mod tests;
