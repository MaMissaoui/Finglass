//! The Transactions view's state and actions, without any UI.
//!
//! This is the UI-free port of FinDesk's `TransactionsViewModel`. The Slint page (M4d) renders [`TransactionsState`]
//! and calls the synchronous methods on [`TransactionsView`] with the database. Every method takes `today` (and
//! sometimes `now`) explicitly; nothing here reads the clock or an environment variable.
//!
//! The account panel is shared with the Accounts view through [`crate::app::account_panel`]; this module only wires
//! it into the transaction list.

use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};

use rust_decimal::Decimal;
use time::{Date, Duration, OffsetDateTime};

use crate::account_grouping::{self, AccountGroupView, AccountItem as GroupingAccount, GroupKey, LOAN_TYPE};
use crate::app::account_panel::{self, AccountGroupActions, AccountSelection, AppError};
use crate::app::accounts::{AccountItem, Busy};
use crate::balance::{BalanceHistory, Entry as RunningBalanceEntry, RunningBalance};
use crate::db::Database;
use crate::documents::service::DocumentService;
use crate::i18n::{self, Arg, Key, Language};
use crate::import::{
    self, CsvLayout, CsvMapping, ImportError, ImportReport, ImportServiceError, ParsedStatement,
    balance_update_allowed, match_account,
};
use crate::model::{
    AccountGroup, AccountGroupId, AccountId, Category, CategoryId, Direction, DocumentItem, NewRule, NewTransaction,
    PendingBookingItem, RuleDirection, RuleField, TransactionFilter, TransactionId, TransactionItem,
};
use crate::money::{Currencies, Currency, Money};
use crate::repo;
use crate::rules::matcher::RuleMatcher;
use crate::rules::payment_providers::PaymentProviders;

/// FinDesk's search debounce. A synchronous caller may use it before calling
/// [`TransactionsView::update_search_query`]; the method itself applies the query at once.
pub const SEARCH_DEBOUNCE_MS: u64 = 250;
/// How many statement files may be selected at once.
pub const MAX_IMPORT_FILES: usize = 240;
/// The total size of the files selected at once.
pub const MAX_IMPORT_TOTAL_BYTES: u64 = 200 * 1024 * 1024;
/// The MB the total-size limit names in the user message.
pub const MAX_IMPORT_TOTAL_MB: u64 = MAX_IMPORT_TOTAL_BYTES / (1024 * 1024);
/// Pending rows whose fetch stamp is older than this many days are hidden.
pub const PENDING_MAX_AGE_DAYS: i64 = 7;
/// Whether the balance chart is folded away, per profile.
pub const CHART_COLLAPSED_KEY: &str = "transactions.chartCollapsed";
/// The settings key holding the panel's selection.
pub const SELECTION_KEY: &str = account_panel::SELECTION_KEY;
/// The settings key holding the panel's collapsed groups.
pub const COLLAPSED_KEY: &str = account_panel::COLLAPSED_KEY;

/// The transaction list's period menu.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum Period {
    /// From the first day of this month through today's month end.
    #[default]
    ThisMonth,
    /// The whole previous calendar month.
    LastMonth,
    /// The previous two months plus this month.
    Last3Months,
    /// January 1 through December 31 of this year.
    ThisYear,
    /// The whole previous calendar year.
    LastYear,
    /// No date restriction.
    All,
}

impl Period {
    /// The inclusive date range, or `(None, None)` for all time.
    #[must_use]
    pub fn range(self, today: Date) -> (Option<Date>, Option<Date>) {
        let month = crate::dashboard::YearMonth::from_date(today);
        match self {
            Self::ThisMonth => (Some(month.start()), Some(month.end())),
            Self::LastMonth => {
                let last = month.minus_months(1);
                (Some(last.start()), Some(last.end()))
            }
            Self::Last3Months => (Some(month.minus_months(2).start()), Some(month.end())),
            Self::ThisYear => {
                let start = Date::from_calendar_date(today.year(), time::Month::January, 1).expect("valid date");
                let end = Date::from_calendar_date(today.year(), time::Month::December, 31).expect("valid date");
                (Some(start), Some(end))
            }
            Self::LastYear => {
                let year = today.year() - 1;
                let start = Date::from_calendar_date(year, time::Month::January, 1).expect("valid date");
                let end = Date::from_calendar_date(year, time::Month::December, 31).expect("valid date");
                (Some(start), Some(end))
            }
            Self::All => (None, None),
        }
    }
}

/// The category filter: everything, only uncategorized, or one category.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum CategoryFilter {
    /// Every category, including uncategorized bookings.
    #[default]
    Any,
    /// Only bookings without a category.
    None,
    /// Exactly one category.
    Only(CategoryId),
}

/// The navigation context a screen passes to [`TransactionsView::start`].
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct StartArgs {
    /// Open one account.
    pub account_id: Option<AccountId>,
    /// Show only uncategorized bookings.
    pub uncategorized_only: bool,
    /// Search for one partner.
    pub partner: Option<String>,
    /// Show one category.
    pub category_id: Option<CategoryId>,
    /// Override the period.
    pub period: Option<Period>,
    /// Show exactly one month (wins over the period).
    pub month: Option<crate::dashboard::YearMonth>,
}

/// One selected statement file that was read successfully.
#[derive(Debug, Clone, PartialEq)]
pub struct ImportEntry {
    /// The source file.
    pub file: PathBuf,
    /// The parsed statement.
    pub statement: ParsedStatement,
}

/// A selected file that could not be read, with the reason shown to the user.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ImportFailure {
    /// The file's display name.
    pub file_name: String,
    /// The user-visible reason.
    pub message: String,
}

/// The selected statement files, read and waiting for the user to pick the account and confirm.
#[derive(Debug, Clone, PartialEq)]
pub struct ImportPreview {
    /// Every selected file, in selection order: a column remap re-reads all of them.
    pub files: Vec<PathBuf>,
    /// The readable files in import order. Never empty.
    pub entries: Vec<ImportEntry>,
    /// The files that could not be read.
    pub failures: Vec<ImportFailure>,
    /// The account the user picked or the file matched.
    pub account_id: Option<AccountId>,
    /// The newest entry's balance may replace the account balance.
    pub balance_update_allowed: bool,
}

impl ImportPreview {
    /// The entry imported last: the file with the greatest latest-booking date.
    #[must_use]
    pub fn newest(&self) -> &ImportEntry {
        self.entries.last().expect("a preview always has one readable entry")
    }

    /// The number of bookings across all readable files.
    #[must_use]
    pub fn booking_count(&self) -> usize {
        self.entries.iter().map(|entry| entry.statement.bookings.len()).sum()
    }
}

/// One booking the rules would recategorize.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct ReapplyChange {
    /// The booking.
    pub transaction_id: TransactionId,
    /// Its category at preview time, `None` when it was uncategorized.
    pub from_category_id: Option<CategoryId>,
    /// The category the highest-priority matching rule assigns.
    pub to_category_id: CategoryId,
}

/// One checkbox row of the re-apply preview dialog.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReapplyGroup {
    /// The category the bookings leave.
    pub from_category_id: Option<CategoryId>,
    /// Its display name, or the localized "Uncategorized".
    pub from_name: String,
    /// The category they move to.
    pub to_category_id: CategoryId,
    /// Its display name.
    pub to_name: String,
    /// How many bookings this row covers.
    pub count: usize,
    /// The sum of their amounts, or `None` when they do not all use the primary currency.
    pub sum: Option<Money>,
    /// The primary currency.
    pub currency: Currency,
}

/// The exact change list a re-apply preview computed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReapplyPreview {
    /// The changes, in list order.
    pub changes: Vec<ReapplyChange>,
    /// The dialog's checkbox rows.
    pub groups: Vec<ReapplyGroup>,
    /// The bookings the preview checked (everything the list showed).
    pub shown_count: usize,
}

/// A manual booking waiting for the user to confirm it as a genuine repeat.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ManualEntry {
    /// The account.
    pub account_id: AccountId,
    /// The booking date.
    pub date: Date,
    /// The amount in the account's currency.
    pub amount: Money,
    /// The account currency (kept for the dialog).
    pub currency: Currency,
    /// The trimmed partner.
    pub partner_name: Option<String>,
    /// The trimmed purpose.
    pub purpose: Option<String>,
    /// The chosen category.
    pub category_id: Option<CategoryId>,
}

/// The arguments of [`TransactionsView::add_manual_transaction`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ManualTransaction {
    /// The account.
    pub account_id: AccountId,
    /// The booking date.
    pub date: Date,
    /// The amount in the account's currency.
    pub amount: Money,
    /// The partner name as typed.
    pub partner_name: Option<String>,
    /// The purpose as typed.
    pub purpose: Option<String>,
    /// The chosen category, or `None` to match a rule.
    pub category_id: Option<CategoryId>,
    /// Whether the user already confirmed an identical repeat.
    pub confirmed_duplicate: bool,
}

/// The Transactions view state.
#[derive(Debug, Clone, PartialEq)]
pub struct TransactionsState {
    /// The filtered bookings, newest first.
    pub transactions: Vec<TransactionItem>,
    /// Every category.
    pub categories: Vec<Category>,
    /// Every account, with the panel's unread counters.
    pub accounts: Vec<AccountItem>,
    /// The search text.
    pub search_query: String,
    /// The selected period.
    pub period: Period,
    /// An exact month: wins over the period.
    pub month: Option<crate::dashboard::YearMonth>,
    /// An arbitrary inclusive range: after an exact month, before the period.
    pub custom_range: Option<(Date, Date)>,
    /// The money flow.
    pub direction: Direction,
    /// The category filter.
    pub category_filter: CategoryFilter,
    /// The selected single account, derived from [`Self::selection`].
    pub account_id: Option<AccountId>,
    /// What the account panel filters by.
    pub selection: AccountSelection,
    /// The panel's groups in display order.
    pub groups: Vec<AccountGroupView>,
    /// The grand total, per currency, primary first.
    pub grand_total: Vec<(Currency, i64)>,
    /// The ids of the collapsed groups.
    pub collapsed: BTreeSet<String>,
    /// The ids rendered bold: bookings a sync brought in.
    pub bold_ids: BTreeSet<TransactionId>,
    /// The free-text filter over the panel's account rows.
    pub account_filter_text: String,
    /// The custom groups as loaded.
    pub custom_groups: Vec<AccountGroup>,
    /// The running balance after each shown booking, when one non-depot account is selected.
    pub balances: Option<BTreeMap<TransactionId, RunningBalanceEntry>>,
    /// The selected account's balance per day over the shown period.
    pub balance_chart: Option<Vec<(Date, Money)>>,
    /// Whether the chart card is folded away.
    pub chart_collapsed: bool,
    /// The selected account's current balance.
    pub current_balance: Option<Money>,
    /// The selected account's currency.
    pub balance_currency: Option<Currency>,
    /// Whether the balance belongs to a loan.
    pub current_balance_is_loan: bool,
    /// Income per currency.
    pub total_income: Vec<(Currency, i64)>,
    /// Expenses per currency, as positive amounts.
    pub total_expenses: Vec<(Currency, i64)>,
    /// The profile's primary currency.
    pub primary_currency: Currency,
    /// The open import preview.
    pub import_preview: Option<ImportPreview>,
    /// The smallest absolute amount.
    pub min_amount: Option<Money>,
    /// The largest absolute amount.
    pub max_amount: Option<Money>,
    /// The open detail panel.
    pub selected: Option<TransactionItem>,
    /// The detail panel's documents.
    pub selected_documents: Vec<DocumentItem>,
    /// A manual entry waiting for confirmation.
    pub pending_duplicate: Option<ManualEntry>,
    /// Whether an import is running.
    pub is_importing: bool,
    /// The selected account's pending bank lines.
    pub pending_bookings: Vec<PendingBookingItem>,
    /// The rules' preview of the shown bookings.
    pub reapply_preview: Option<ReapplyPreview>,
    /// Whether the last re-apply can still be undone.
    pub can_undo_reapply: bool,
    /// The selected bookings.
    pub selected_ids: BTreeSet<TransactionId>,
    /// The anchor of the next Shift range.
    pub selection_anchor_id: Option<TransactionId>,
    /// Whether a load is running.
    pub is_loading: bool,
    /// The current error text.
    pub error: Option<String>,
    /// The current success text.
    pub message: Option<String>,
    /// The language of the state's user-visible texts.
    pub language: Language,
}

impl TransactionsState {
    /// Builds empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            transactions: Vec::new(),
            categories: Vec::new(),
            accounts: Vec::new(),
            search_query: String::new(),
            period: Period::default(),
            month: None,
            custom_range: None,
            direction: Direction::All,
            category_filter: CategoryFilter::Any,
            account_id: None,
            selection: AccountSelection::All,
            groups: Vec::new(),
            grand_total: Vec::new(),
            collapsed: BTreeSet::new(),
            bold_ids: BTreeSet::new(),
            account_filter_text: String::new(),
            custom_groups: Vec::new(),
            balances: None,
            balance_chart: None,
            chart_collapsed: false,
            current_balance: None,
            balance_currency: None,
            current_balance_is_loan: false,
            total_income: Vec::new(),
            total_expenses: Vec::new(),
            primary_currency: Currencies::DEFAULT,
            import_preview: None,
            min_amount: None,
            max_amount: None,
            selected: None,
            selected_documents: Vec::new(),
            pending_duplicate: None,
            is_importing: false,
            pending_bookings: Vec::new(),
            reapply_preview: None,
            can_undo_reapply: false,
            selected_ids: BTreeSet::new(),
            selection_anchor_id: None,
            is_loading: false,
            error: None,
            message: None,
            language,
        }
    }

    /// The selected bookings among the currently shown list, in list order.
    #[must_use]
    pub fn selected_shown(&self) -> Vec<&TransactionItem> {
        self.transactions
            .iter()
            .filter(|transaction| self.selected_ids.contains(&transaction.id))
            .collect()
    }

    /// Totals of the selected bookings per currency; currencies are never added together.
    #[must_use]
    pub fn selected_totals(&self) -> Vec<(Currency, i64)> {
        let shown: Vec<&TransactionItem> = self.selected_shown();
        let items: Vec<TransactionItem> = shown.into_iter().cloned().collect();
        Currencies::totals(
            &items,
            Currencies::DEFAULT,
            |transaction| transaction.currency,
            |transaction| transaction.amount.minor,
        )
    }
}

impl Default for TransactionsState {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

/// The exact key of one re-apply checkbox row.
pub type ReapplyKey = (Option<CategoryId>, CategoryId);

/// The Transactions view: state plus the synchronous actions that change it.
#[derive(Debug)]
pub struct TransactionsView {
    state: TransactionsState,
    restore_selection: bool,
    collapsed_loaded: bool,
    chart_collapsed_loaded: bool,
    last_refreshed_selection: Option<AccountSelection>,
    reapply_undo: Vec<ReapplyChange>,
    busy: Busy,
}

impl TransactionsView {
    /// Builds a view with empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            state: TransactionsState::new(language),
            restore_selection: false,
            collapsed_loaded: false,
            chart_collapsed_loaded: false,
            last_refreshed_selection: None,
            reapply_undo: Vec::new(),
            busy: Busy::default(),
        }
    }

    /// The current state.
    #[must_use]
    pub fn state(&self) -> &TransactionsState {
        &self.state
    }

    /// The current state, mutably. The page only reads it; actions live on this view.
    pub fn state_mut(&mut self) -> &mut TransactionsState {
        &mut self.state
    }

    /// The shared busy flag.
    #[must_use]
    pub fn busy(&self) -> &Busy {
        &self.busy
    }

    /// Replacement for an externally shared busy flag.
    pub fn set_busy(&mut self, busy: Busy) {
        self.busy = busy;
    }

    /// The filter the current state describes.
    #[must_use]
    pub fn current_filter(&self, today: Date) -> TransactionFilter {
        let (start, end) = self.date_range(today);
        let (account_id, account_ids) = match &self.state.selection {
            AccountSelection::Account(id) => (Some(*id), None),
            AccountSelection::Group(key) => (
                None,
                Some(
                    self.state
                        .groups
                        .iter()
                        .find(|group| group.key.id() == key.id())
                        .map(|group| group.accounts.iter().map(|account| account.id).collect())
                        .unwrap_or_default(),
                ),
            ),
            AccountSelection::All => (None, None),
        };

        TransactionFilter {
            start_date: start,
            end_date: end,
            account_id,
            account_ids,
            category_id: match self.state.category_filter {
                CategoryFilter::Only(id) => Some(id),
                _ => None,
            },
            uncategorized_only: self.state.category_filter == CategoryFilter::None,
            direction: self.state.direction,
            text: Some(self.state.search_query.clone()).filter(|text| !text.trim().is_empty()),
            min_amount: self.state.min_amount,
            max_amount: self.state.max_amount,
            ..TransactionFilter::default()
        }
    }

    /// The inclusive date range behind the month, the custom range or the period.
    #[must_use]
    pub fn date_range(&self, today: Date) -> (Option<Date>, Option<Date>) {
        if let Some(month) = self.state.month {
            (Some(month.start()), Some(month.end()))
        } else if let Some((from, to)) = self.state.custom_range {
            (Some(from), Some(to))
        } else {
            self.state.period.range(today)
        }
    }

    /// Applies navigation context (e.g. "transactions of this account") and loads.
    pub fn start(&mut self, db: &Database, args: StartArgs, today: Date) -> Result<(), AppError> {
        if let Some(partner) = args.partner {
            return self.show_partner(db, &partner, today);
        }
        if let Some(category_id) = args.category_id {
            return self.show_category(db, category_id, args.period, args.month, today);
        }
        if args.uncategorized_only {
            return self.show_uncategorized_link(db, args.account_id, args.period, args.month, today);
        }

        self.restore_selection = args.account_id.is_none();
        let navigated = args.period.is_some() || args.month.is_some() || args.account_id.is_some();
        let effective_period = match args.period {
            Some(period) => period,
            None if args.account_id.is_some() => Period::All,
            None => self.state.period,
        };
        self.state.account_id = args.account_id;
        if let Some(id) = args.account_id {
            self.state.selection = AccountSelection::Account(id);
        }
        self.state.category_filter = CategoryFilter::Any;
        self.state.period = effective_period;
        self.state.month = args.month;
        if navigated {
            self.state.custom_range = None;
        }
        self.clear_selection();
        self.load(db, today)
    }

    /// Every booking of one category over `period` (default all time) or exactly `month`.
    fn show_category(
        &mut self,
        db: &Database,
        category_id: CategoryId,
        period: Option<Period>,
        month: Option<crate::dashboard::YearMonth>,
        today: Date,
    ) -> Result<(), AppError> {
        self.restore_selection = false;
        self.reset_for_category_link(CategoryFilter::Only(category_id));
        self.state.period = period.unwrap_or(Period::All);
        self.state.month = month;
        self.load(db, today)
    }

    /// The dashboard's uncategorized link.
    fn show_uncategorized_link(
        &mut self,
        db: &Database,
        account_id: Option<AccountId>,
        period: Option<Period>,
        month: Option<crate::dashboard::YearMonth>,
        today: Date,
    ) -> Result<(), AppError> {
        self.restore_selection = false;
        self.reset_for_category_link(CategoryFilter::None);
        self.state.period = period.unwrap_or(Period::All);
        self.state.month = month;
        if let Some(id) = account_id {
            self.state.selection = AccountSelection::Account(id);
            self.state.account_id = Some(id);
        }
        self.load(db, today)
    }

    fn reset_for_category_link(&mut self, category_filter: CategoryFilter) {
        self.state.search_query.clear();
        self.state.direction = Direction::All;
        self.state.category_filter = category_filter;
        self.state.selection = AccountSelection::All;
        self.state.account_id = None;
        self.state.min_amount = None;
        self.state.max_amount = None;
        self.state.custom_range = None;
        self.clear_selection();
    }

    /// Every booking from `partner`, any time, account, category, direction or amount.
    pub fn show_partner(&mut self, db: &Database, partner: &str, today: Date) -> Result<(), AppError> {
        self.restore_selection = false;
        self.state.search_query = partner.to_owned();
        self.state.period = Period::All;
        self.state.month = None;
        self.state.direction = Direction::All;
        self.state.category_filter = CategoryFilter::Any;
        self.state.selection = AccountSelection::All;
        self.state.account_id = None;
        self.state.min_amount = None;
        self.state.max_amount = None;
        self.state.custom_range = None;
        self.clear_selection();
        self.load(db, today)
    }

    /// Reloads the list, its figures and the panel data.
    pub fn load(&mut self, db: &Database, today: Date) -> Result<(), AppError> {
        self.state.is_loading = true;
        self.state.error = None;
        match self.refresh(db, today) {
            Ok(()) => Ok(()),
            Err(error) => self.record_error(error),
        }
    }

    fn refresh(&mut self, db: &Database, today: Date) -> Result<(), AppError> {
        let previous_selection = self.last_refreshed_selection.clone();
        let mut accounts = load_account_items(db)?;
        let custom_groups = repo::account_groups::list(db)?;
        let primary = Currencies::primary(db)?;
        let mut groups = group_accounts(&accounts, &custom_groups, primary);
        let candidate = if self.restore_selection {
            account_panel::load_selection(db, &groups, &grouping_accounts(&accounts))?
        } else {
            self.state.selection.clone()
        };
        let selection = account_panel::resolve(&candidate, &groups, &grouping_accounts(&accounts));
        self.restore_selection = false;
        self.last_refreshed_selection = Some(selection.clone());

        self.state.accounts = accounts.clone();
        self.state.custom_groups = custom_groups.clone();
        self.state.groups = groups.clone();
        self.state.grand_total = account_grouping::grand_total(&grouping_accounts(&accounts), primary);
        self.state.primary_currency = primary;
        if !self.collapsed_loaded {
            self.state.collapsed = account_panel::load_collapsed(db)?;
            self.collapsed_loaded = true;
        }
        if !self.chart_collapsed_loaded {
            self.state.chart_collapsed = load_chart_collapsed(db)?;
            self.chart_collapsed_loaded = true;
        }
        self.state.selection = selection.clone();
        self.state.account_id = match &selection {
            AccountSelection::Account(id) => Some(*id),
            _ => None,
        };

        let transactions = repo::transactions::find_items(db, &self.current_filter(today), None)?;
        let shown_new: BTreeSet<TransactionId> = transactions
            .iter()
            .filter(|transaction| transaction.is_new)
            .map(|transaction| transaction.id)
            .collect();
        // Bold rows survive a refresh so clearing a counter cannot unbold them, but a selection change replaces them.
        let mut bold_ids = if previous_selection.as_ref() == Some(&selection) {
            self.state.bold_ids.union(&shown_new).copied().collect()
        } else {
            shown_new
        };

        // Opening a single account marks its bookings read, however it was selected. View state only.
        if let AccountSelection::Account(id) = &selection
            && self
                .state
                .accounts
                .iter()
                .find(|account| account.id == *id)
                .is_some_and(|account| account.new_count > 0)
        {
            let all_for_account = repo::transactions::find_items(
                db,
                &TransactionFilter {
                    account_id: Some(*id),
                    ..TransactionFilter::default()
                },
                None,
            )?;
            bold_ids.extend(
                all_for_account
                    .iter()
                    .filter(|transaction| transaction.is_new)
                    .map(|transaction| transaction.id),
            );
            repo::transactions::mark_read(db, Some(&BTreeSet::from([*id])))?;
            accounts = load_account_items(db)?;
            groups = group_accounts(&accounts, &custom_groups, primary);
        }

        // Keep the open detail panel in sync with edits.
        if let Some(selected) = self.state.selected.clone() {
            if let Some(still_shown) = transactions.iter().find(|transaction| transaction.id == selected.id) {
                self.state.selected = Some(still_shown.clone());
            } else if !transaction_exists(db, selected.id)? {
                self.state.selected = None;
                self.state.selected_documents.clear();
            }
        }

        let categories = repo::categories::list(db)?;
        let pending_bookings = match &selection {
            AccountSelection::Account(id) if self.shows_pending_rows(today) => {
                let rows = repo::pending_bookings::find_items(db, *id)?;
                if self.pending_rows_are_fresh(&rows, today) {
                    rows
                } else {
                    Vec::new()
                }
            }
            _ => Vec::new(),
        };

        let balance_account = self
            .state
            .account_id
            .and_then(|id| self.state.accounts.iter().find(|account| account.id == id))
            .filter(|account| account.account_type != "depot")
            .cloned();
        let account_bookings = balance_account
            .as_ref()
            .map(|account| {
                repo::transactions::find_items(
                    db,
                    &TransactionFilter {
                        account_id: Some(account.id),
                        ..TransactionFilter::default()
                    },
                    None,
                )
            })
            .transpose()?;
        let balances = balance_account
            .as_ref()
            .zip(account_bookings.as_ref())
            .map(|(account, bookings)| RunningBalance::compute(account.balance, bookings));
        let balance_chart = match (balance_account.as_ref(), account_bookings.as_ref(), balances.as_ref()) {
            (Some(_), Some(bookings), Some(running)) => Some(self.balance_chart_points(bookings, running, today)),
            _ => None,
        };

        let income: Vec<&TransactionItem> = transactions
            .iter()
            .filter(|transaction| transaction.amount.minor > 0)
            .collect();
        let expenses: Vec<&TransactionItem> = transactions
            .iter()
            .filter(|transaction| transaction.is_expense())
            .collect();
        let total_income = Currencies::totals(
            &income,
            Currencies::DEFAULT,
            |transaction| transaction.currency,
            |transaction| transaction.amount.minor,
        );
        let total_expenses = Currencies::totals(
            &expenses,
            Currencies::DEFAULT,
            |transaction| transaction.currency,
            |transaction| transaction.amount.minor.abs(),
        );
        let shown_ids: BTreeSet<TransactionId> = transactions.iter().map(|transaction| transaction.id).collect();

        self.state.transactions = transactions;
        self.state.categories = categories;
        self.state.pending_bookings = pending_bookings;
        self.state.accounts = accounts;
        self.state.groups = groups;
        self.state.grand_total = account_grouping::grand_total(&grouping_accounts(&self.state.accounts), primary);
        self.state.primary_currency = primary;
        self.state.bold_ids = bold_ids;
        self.state.balances = balances;
        self.state.balance_chart = balance_chart;
        self.state.current_balance = balance_account.as_ref().map(|account| account.balance);
        self.state.balance_currency = balance_account.as_ref().map(|account| account.currency);
        self.state.current_balance_is_loan = balance_account
            .as_ref()
            .is_some_and(|account| account.account_type == LOAN_TYPE);
        self.state.total_income = total_income;
        self.state.total_expenses = total_expenses;
        self.state
            .selected_ids
            .retain(|transaction_id| shown_ids.contains(transaction_id));
        self.state.is_loading = false;
        Ok(())
    }

    /// Whether pending rows may show: one account, a range including today, and no narrowing filter.
    fn shows_pending_rows(&self, today: Date) -> bool {
        if !matches!(self.state.selection, AccountSelection::Account(_)) {
            return false;
        }
        let (start, end) = self.date_range(today);
        let includes_today = start.is_none_or(|start| start <= today) && end.is_none_or(|end| end >= today);
        includes_today
            && self.state.search_query.trim().is_empty()
            && self.state.direction == Direction::All
            && self.state.min_amount.is_none()
            && self.state.max_amount.is_none()
            && self.state.category_filter == CategoryFilter::Any
    }

    fn pending_rows_are_fresh(&self, rows: &[PendingBookingItem], today: Date) -> bool {
        let cutoff = today - Duration::days(PENDING_MAX_AGE_DAYS);
        rows.iter().all(|row| row.fetched_at.date() >= cutoff)
    }

    fn balance_chart_points(
        &self,
        bookings: &[TransactionItem],
        running: &BTreeMap<TransactionId, RunningBalanceEntry>,
        today: Date,
    ) -> Vec<(Date, Money)> {
        let (start, end) = self.date_range(today);
        let Some(from) = start.or_else(|| bookings.iter().map(|booking| booking.date).min()) else {
            return Vec::new();
        };
        let to = match end {
            Some(end) if end <= today => end,
            _ => today,
        };
        BalanceHistory::from_running(bookings, running, from, to)
    }

    /// Debouncing is meaningless in a synchronous API: the query is applied at once.
    pub fn update_search_query(&mut self, db: &Database, query: &str, today: Date) -> Result<(), AppError> {
        self.state.search_query = query.to_owned();
        self.clear_selection();
        self.load(db, today)
    }

    /// Applies one period; it ends the exact-month navigation and the custom range.
    pub fn update_period(&mut self, db: &Database, period: Period, today: Date) -> Result<(), AppError> {
        self.state.period = period;
        self.state.month = None;
        self.state.custom_range = None;
        self.clear_selection();
        self.load(db, today)
    }

    /// Applies an arbitrary inclusive From/To range. A reversed range changes nothing.
    pub fn set_custom_range(&mut self, db: &Database, from: Date, to: Date, today: Date) -> Result<(), AppError> {
        if from > to {
            return Ok(());
        }
        self.state.custom_range = Some((from, to));
        self.state.month = None;
        self.clear_selection();
        self.load(db, today)
    }

    /// Applies the money-flow filter.
    pub fn update_direction(&mut self, db: &Database, direction: Direction, today: Date) -> Result<(), AppError> {
        self.state.direction = direction;
        self.clear_selection();
        self.load(db, today)
    }

    /// Applies the category filter.
    pub fn update_category_filter(
        &mut self,
        db: &Database,
        filter: CategoryFilter,
        today: Date,
    ) -> Result<(), AppError> {
        self.state.category_filter = filter;
        self.clear_selection();
        self.load(db, today)
    }

    /// Selects an account, group or all accounts, persists the choice, and reloads.
    pub fn select_accounts(&mut self, db: &Database, selection: AccountSelection, today: Date) -> Result<(), AppError> {
        self.state.selection = selection.clone();
        self.state.account_id = match &selection {
            AccountSelection::Account(id) => Some(*id),
            _ => None,
        };
        self.clear_selection();
        self.state.is_loading = true;
        self.state.error = None;
        match account_panel::save_selection(db, &selection) {
            Ok(()) => self.refresh(db, today),
            Err(error) => self.record_error(error),
        }
    }

    /// Selects one account, or all accounts for `None`.
    pub fn update_account_filter(
        &mut self,
        db: &Database,
        account_id: Option<AccountId>,
        today: Date,
    ) -> Result<(), AppError> {
        self.select_accounts(
            db,
            account_id.map_or(AccountSelection::All, AccountSelection::Account),
            today,
        )
    }

    /// Marks the bookings behind `selection` as read on the user's request.
    pub fn mark_read(&mut self, db: &Database, selection: AccountSelection, today: Date) -> Result<(), AppError> {
        self.state.is_loading = true;
        self.state.error = None;
        let actions = AccountGroupActions::new();
        match actions.mark_read(db, &selection, &self.state.groups) {
            Ok(()) => self.refresh(db, today),
            Err(error) => self.record_error(error),
        }
    }

    /// Collapses or expands a group; view state only, persisted but not audited.
    pub fn toggle_collapsed(&mut self, db: &Database, key: &GroupKey) -> Result<(), AppError> {
        let id = key.id();
        if !self.state.collapsed.remove(&id) {
            self.state.collapsed.insert(id);
        }
        match account_panel::save_collapsed(db, &self.state.collapsed) {
            Ok(()) => Ok(()),
            Err(error) => self.record_error(error),
        }
    }

    /// Collapses or expands the balance chart; view state only, persisted but not audited.
    pub fn toggle_chart(&mut self, db: &Database) -> Result<(), AppError> {
        self.state.chart_collapsed = !self.state.chart_collapsed;
        match crate::settings::set_setting(db, CHART_COLLAPSED_KEY, &self.state.chart_collapsed.to_string()) {
            Ok(()) => Ok(()),
            Err(error) => self.record_error(error.into()),
        }
    }

    /// Filters the panel's account rows by name, IBAN or bank; the booking list is unaffected.
    pub fn update_account_filter_text(&mut self, text: &str) {
        self.state.account_filter_text = text.to_owned();
    }

    /// Creates a group and reloads.
    pub fn create_group(&mut self, db: &Database, name: &str, today: Date) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.create(db, name) {
            Ok(_) => self.refresh(db, today),
            Err(AppError::Message(key)) => {
                self.state.error = Some(i18n::text(self.state.language, key).to_owned());
                Ok(())
            }
            Err(error) => self.record_error(error),
        }
    }

    /// Renames a group and reloads.
    pub fn rename_group(&mut self, db: &Database, id: AccountGroupId, name: &str, today: Date) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.rename(db, id, name) {
            Ok(()) => self.refresh(db, today),
            Err(AppError::Message(key)) => {
                self.state.error = Some(i18n::text(self.state.language, key).to_owned());
                Ok(())
            }
            Err(error) => self.record_error(error),
        }
    }

    /// Deletes a group, resetting the selection to All when it was selected, then reloads.
    pub fn delete_group(&mut self, db: &Database, id: AccountGroupId, today: Date) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.delete(db, id, &self.state.selection) {
            Ok(true) => {
                self.state.selection = AccountSelection::All;
                self.state.account_id = None;
                self.refresh(db, today)
            }
            Ok(false) => self.refresh(db, today),
            Err(error) => self.record_error(error),
        }
    }

    /// Moves a group up or down and reloads.
    pub fn move_group(&mut self, db: &Database, id: AccountGroupId, up: bool, today: Date) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.move_up_or_down(db, id, up) {
            Ok(_) => self.refresh(db, today),
            Err(error) => self.record_error(error),
        }
    }

    /// Moves an account into a custom group, or back to its type group, then reloads.
    pub fn move_account_to_group(
        &mut self,
        db: &Database,
        account_id: AccountId,
        group_id: Option<AccountGroupId>,
        today: Date,
    ) -> Result<(), AppError> {
        let actions = AccountGroupActions::new();
        match actions.move_account(db, account_id, group_id) {
            Ok(()) => self.refresh(db, today),
            Err(error) => self.record_error(error),
        }
    }

    /// Applies the absolute amount bounds.
    pub fn update_amount_range(
        &mut self,
        db: &Database,
        min: Option<Money>,
        max: Option<Money>,
        today: Date,
    ) -> Result<(), AppError> {
        self.state.min_amount = min;
        self.state.max_amount = max;
        self.clear_selection();
        self.load(db, today)
    }

    /// Opens the detail panel with the transaction's documents.
    pub fn select_transaction(
        &mut self,
        db: &Database,
        service: &DocumentService<'_>,
        transaction: Option<TransactionItem>,
    ) -> Result<(), AppError> {
        self.state.selected = transaction.clone();
        self.state.selected_documents.clear();
        let Some(transaction) = transaction else {
            return Ok(());
        };
        let documents = match service.get_documents_for_transaction(transaction.id) {
            Ok(documents) => documents,
            Err(error) => {
                self.state.error = Some(error.message(self.state.language));
                return Ok(());
            }
        };
        if self
            .state
            .selected
            .as_ref()
            .is_some_and(|selected| selected.id == transaction.id)
        {
            self.state.selected_documents = documents;
        }
        let _ = db;
        Ok(())
    }

    /// Uploads the files to the transaction and refreshes its document list.
    pub fn attach_documents(
        &mut self,
        db: &Database,
        service: &DocumentService<'_>,
        transaction_id: TransactionId,
        files: &[PathBuf],
        today: Date,
    ) -> Result<(), AppError> {
        let language = self.state.language;
        let failures = self.busy.run(|| {
            let mut failures = Vec::new();
            for file in files {
                if let Err(error) = service.upload_document(file, Some(transaction_id), true) {
                    let name = file.file_name().map_or_else(
                        || file.to_string_lossy().into_owned(),
                        |name| name.to_string_lossy().into_owned(),
                    );
                    failures.push(format!("{name}: {}", error.message(language)));
                }
            }
            failures
        });
        let uploaded = match service.get_documents_for_transaction(transaction_id) {
            Ok(documents) => documents,
            Err(upload_error) => {
                self.state.error = Some(upload_error.message(language));
                return Ok(());
            }
        };
        let error = (!failures.is_empty()).then(|| failures.join("\n"));
        if self
            .state
            .selected
            .as_ref()
            .is_some_and(|selected| selected.id == transaction_id)
        {
            self.state.selected_documents = uploaded;
            self.state.error = error;
        } else {
            self.state.error = error;
        }
        self.refresh(db, today)
    }

    /// The suggested rule pattern for a transaction.
    #[must_use]
    pub fn rule_pattern_for(&self, transaction: &TransactionItem) -> String {
        let raw = PaymentProviders::merchant_of(transaction.purpose.as_deref(), transaction.partner_name.as_deref())
            .or_else(|| transaction.partner_name.clone())
            .or_else(|| transaction.purpose.clone())
            .unwrap_or_default();
        let words: Vec<&str> = raw
            .split_whitespace()
            .filter(|word| !word.is_empty() && word.chars().filter(char::is_ascii_digit).count() < 3)
            .take(3)
            .collect();
        let joined = words.join(" ");
        let mut escaped = String::with_capacity(joined.len());
        for character in joined.chars() {
            if matches!(
                character,
                '\\' | '.' | '^' | '$' | '|' | '?' | '*' | '+' | '(' | ')' | '[' | ']' | '{' | '}'
            ) {
                escaped.push('\\');
            }
            escaped.push(character);
        }
        escaped
    }

    /// Creates an auto-categorization rule and applies it to all uncategorized transactions.
    pub fn create_rule(
        &mut self,
        db: &Database,
        pattern: &str,
        category_id: CategoryId,
        today: Date,
    ) -> Result<(), AppError> {
        repo::rules::create(
            db,
            NewRule {
                pattern: pattern.to_owned(),
                category_id,
                priority: 5,
                auto_apply: true,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )?;
        let uncategorized = repo::transactions::find_items(
            db,
            &TransactionFilter {
                uncategorized_only: true,
                ..TransactionFilter::default()
            },
            None,
        )?;
        let matcher = RuleMatcher::of(&[(pattern, category_id)]);
        let changes: Vec<(TransactionId, Option<CategoryId>)> = uncategorized
            .iter()
            .filter(|transaction| {
                matcher
                    .category_for(
                        transaction.purpose.as_deref(),
                        transaction.partner_name.as_deref(),
                        Some(decimal_amount(transaction.amount)),
                    )
                    .is_some()
            })
            .map(|transaction| (transaction.id, Some(category_id)))
            .collect();
        let applied = if changes.is_empty() {
            0
        } else {
            repo::transactions::update_categories(db, &changes)?
        };
        self.refresh(db, today)?;
        self.state.message = Some(i18n::format(
            self.state.language,
            Key::MessageRuleCreated,
            &[Arg::from(applied)],
        ));
        Ok(())
    }

    /// Changes one booking's category.
    pub fn update_transaction_category(
        &mut self,
        db: &Database,
        transaction_id: TransactionId,
        category_id: Option<CategoryId>,
        today: Date,
    ) -> Result<(), AppError> {
        repo::transactions::set_category(db, transaction_id, category_id)?;
        self.refresh(db, today)
    }

    /// Adds a manual booking, asking first when an identical one already exists that day.
    pub fn add_manual_transaction(
        &mut self,
        db: &Database,
        entry: ManualTransaction,
        today: Date,
    ) -> Result<(), AppError> {
        if entry.amount.minor == 0 {
            return self.user_error(Key::ErrorAmountNotZero);
        }
        let Some(account) = repo::accounts::get(db, entry.account_id)? else {
            return self.user_error(Key::ErrorAccountNotFound);
        };
        let partner = entry
            .partner_name
            .as_deref()
            .map(str::trim)
            .filter(|text| !text.is_empty())
            .map(str::to_owned);
        let text = entry
            .purpose
            .as_deref()
            .map(str::trim)
            .filter(|text| !text.is_empty())
            .map(str::to_owned);
        let identical = repo::transactions::find_items(
            db,
            &TransactionFilter {
                start_date: Some(entry.date),
                end_date: Some(entry.date),
                account_id: Some(entry.account_id),
                ..TransactionFilter::default()
            },
            None,
        )?
        .iter()
        .filter(|transaction| {
            transaction.amount.minor == entry.amount.minor
                && transaction.partner_name == partner
                && transaction.purpose == text
        })
        .count();

        if identical > 0 && !entry.confirmed_duplicate {
            self.state.pending_duplicate = Some(ManualEntry {
                account_id: entry.account_id,
                date: entry.date,
                amount: entry.amount,
                currency: account.currency,
                partner_name: partner,
                purpose: text,
                category_id: entry.category_id,
            });
            return Ok(());
        }

        let repeat = if identical > 0 {
            format!("#{identical}")
        } else {
            String::new()
        };
        let hash = import::transaction_hash(
            entry.date,
            decimal_amount(entry.amount),
            partner.as_deref(),
            Some(&format!(
                "{}#{}#manual{repeat}",
                text.as_deref().unwrap_or_default(),
                account.iban.as_deref().unwrap_or_default()
            )),
        );
        let resolved_category = match entry.category_id {
            Some(category_id) => Some(category_id),
            None => crate::rules::matcher(db)?.category_for(
                entry.purpose.as_deref(),
                entry.partner_name.as_deref(),
                Some(decimal_amount(entry.amount)),
            ),
        };
        repo::transactions::insert_imported(
            db,
            entry.account_id,
            vec![NewTransaction {
                booking_date: entry.date,
                amount: entry.amount,
                balance_after: None,
                partner_name: partner,
                purpose: text,
                category_id: resolved_category,
                import_hash: hash,
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )?;
        self.refresh(db, today)?;
        self.state.message = Some(i18n::text(self.state.language, Key::MessageTransactionAdded).to_owned());
        Ok(())
    }

    /// Adds the pending identical entry after the user confirmed it is a second, real booking.
    ///
    /// Returns `false` when nothing was waiting for confirmation.
    pub fn confirm_duplicate(&mut self, db: &Database, today: Date) -> Result<bool, AppError> {
        let Some(entry) = self.state.pending_duplicate.take() else {
            return Ok(false);
        };
        self.add_manual_transaction(
            db,
            ManualTransaction {
                account_id: entry.account_id,
                date: entry.date,
                amount: entry.amount,
                partner_name: entry.partner_name,
                purpose: entry.purpose,
                category_id: entry.category_id,
                confirmed_duplicate: true,
            },
            today,
        )?;
        Ok(true)
    }

    /// Closes the duplicate confirmation without adding anything.
    pub fn dismiss_duplicate(&mut self) {
        self.state.pending_duplicate = None;
    }

    /// Deletes one booking.
    pub fn delete_transaction(
        &mut self,
        db: &Database,
        transaction_id: TransactionId,
        today: Date,
    ) -> Result<(), AppError> {
        repo::transactions::delete(db, transaction_id)?;
        self.refresh(db, today)
    }

    /// Toggles one booking in the multi-selection and makes it the range anchor.
    pub fn toggle_selection(&mut self, id: TransactionId) {
        if !self.state.selected_ids.remove(&id) {
            self.state.selected_ids.insert(id);
        }
        self.state.selection_anchor_id = Some(id);
    }

    /// Shift-click: selects every shown booking between the anchor and `id`, inclusive.
    pub fn select_range(&mut self, id: TransactionId) {
        let Some(anchor) = self.state.selection_anchor_id else {
            self.toggle_selection(id);
            return;
        };
        let from = self
            .state
            .transactions
            .iter()
            .position(|transaction| transaction.id == anchor);
        let to = self
            .state
            .transactions
            .iter()
            .position(|transaction| transaction.id == id);
        match (from, to) {
            (Some(from), Some(to)) => {
                let (low, high) = if from <= to { (from, to) } else { (to, from) };
                self.state.selected_ids.extend(
                    self.state.transactions[low..=high]
                        .iter()
                        .map(|transaction| transaction.id),
                );
                self.state.selection_anchor_id = Some(id);
            }
            _ => self.toggle_selection(id),
        }
    }

    /// Selects every booking the current filters show.
    pub fn select_all_shown(&mut self) {
        self.state.selected_ids = self
            .state
            .transactions
            .iter()
            .map(|transaction| transaction.id)
            .collect();
    }

    /// Empties the multi-selection.
    pub fn clear_selection(&mut self) {
        self.state.selected_ids.clear();
        self.state.selection_anchor_id = None;
    }

    /// Deletes exactly the passed ids that are selected and still shown.
    pub fn delete_selected(
        &mut self,
        db: &Database,
        ids: &BTreeSet<TransactionId>,
        today: Date,
    ) -> Result<(), AppError> {
        if ids.is_empty() {
            return Ok(());
        }
        let shown: BTreeSet<TransactionId> = self
            .state
            .transactions
            .iter()
            .map(|transaction| transaction.id)
            .collect();
        let targets: Vec<TransactionId> = ids
            .intersection(&self.state.selected_ids)
            .filter(|id| shown.contains(id))
            .copied()
            .collect();
        if !targets.is_empty() {
            let deleted = repo::transactions::delete_all(db, &targets)?;
            if deleted > 0 {
                self.state.message = Some(i18n::format(
                    self.state.language,
                    Key::MessageTransactionsDeleted,
                    &[Arg::from(deleted)],
                ));
            }
        }
        self.clear_selection();
        self.refresh(db, today)
    }

    /// Sets the manual tax override of the selected bookings that are still shown.
    pub fn set_tax_override_for_selection(
        &mut self,
        db: &Database,
        value: Option<bool>,
        today: Date,
    ) -> Result<(), AppError> {
        let shown: BTreeSet<TransactionId> = self
            .state
            .transactions
            .iter()
            .map(|transaction| transaction.id)
            .collect();
        let targets: Vec<TransactionId> = self
            .state
            .selected_ids
            .iter()
            .filter(|id| shown.contains(id))
            .copied()
            .collect();
        let changed = repo::transactions::set_tax_override(db, &targets, value)?;
        if changed > 0 {
            self.state.message = Some(i18n::format(
                self.state.language,
                Key::MessageTaxOverrideSet,
                &[Arg::from(changed)],
            ));
        }
        self.refresh(db, today)
    }

    /// Sets the category of the selected bookings that are still shown.
    pub fn set_category_for_selection(
        &mut self,
        db: &Database,
        category_id: Option<CategoryId>,
        today: Date,
    ) -> Result<(), AppError> {
        // Only bookings that actually move are written, so an all-already-there selection writes no audit entry.
        let targets: Vec<(TransactionId, Option<CategoryId>)> = self
            .state
            .transactions
            .iter()
            .filter(|transaction| self.state.selected_ids.contains(&transaction.id))
            .filter(|transaction| transaction.category_id != category_id)
            .map(|transaction| (transaction.id, category_id))
            .collect();
        let changed = if targets.is_empty() {
            0
        } else {
            repo::transactions::update_categories(db, &targets)?
        };
        if changed > 0 {
            self.state.message = Some(i18n::format(
                self.state.language,
                Key::MessageCategorySet,
                &[Arg::from(changed)],
            ));
        }
        self.refresh(db, today)
    }

    /// Sets the manual tax override of one booking from the details dialog.
    pub fn set_tax_override(
        &mut self,
        db: &Database,
        transaction_id: TransactionId,
        value: Option<bool>,
        today: Date,
    ) -> Result<(), AppError> {
        let changed = repo::transactions::set_tax_override(db, &[transaction_id], value)?;
        if changed > 0 {
            self.state.message = Some(i18n::format(
                self.state.language,
                Key::MessageTaxOverrideSet,
                &[Arg::from(changed)],
            ));
        }
        self.refresh(db, today)
    }

    /// Applies the auto-apply rules to every uncategorized booking.
    pub fn auto_categorize(&mut self, db: &Database, today: Date) -> Result<(), AppError> {
        let uncategorized = repo::transactions::find_items(
            db,
            &TransactionFilter {
                uncategorized_only: true,
                ..TransactionFilter::default()
            },
            None,
        )?;
        let matcher = crate::rules::matcher(db)?;
        let changes: Vec<(TransactionId, Option<CategoryId>)> = uncategorized
            .iter()
            .filter_map(|transaction| {
                matcher
                    .category_for(
                        transaction.purpose.as_deref(),
                        transaction.partner_name.as_deref(),
                        Some(decimal_amount(transaction.amount)),
                    )
                    .map(|category_id| (transaction.id, Some(category_id)))
            })
            .collect();
        let updated = if changes.is_empty() {
            0
        } else {
            repo::transactions::update_categories(db, &changes)?
        };
        self.refresh(db, today)?;
        self.state.message = Some(i18n::format(
            self.state.language,
            Key::MessageAutoCategorized,
            &[Arg::from(updated), Arg::from(uncategorized.len())],
        ));
        Ok(())
    }

    /// Computes the re-apply preview for exactly the bookings the current filters show.
    pub fn preview_reapply_rules(&mut self, db: &Database, today: Date) -> Result<(), AppError> {
        let items = repo::transactions::find_items(db, &self.current_filter(today), None)?;
        let matcher = crate::rules::matcher(db)?;
        let changes: Vec<ReapplyChange> = items
            .iter()
            .filter_map(|transaction| {
                let matched = matcher.category_for(
                    transaction.purpose.as_deref(),
                    transaction.partner_name.as_deref(),
                    Some(decimal_amount(transaction.amount)),
                )?;
                (Some(matched) != transaction.category_id).then_some(ReapplyChange {
                    transaction_id: transaction.id,
                    from_category_id: transaction.category_id,
                    to_category_id: matched,
                })
            })
            .collect();
        if changes.is_empty() {
            self.state.reapply_preview = None;
            self.state.message = Some(i18n::text(self.state.language, Key::MessageReapplyNothing).to_owned());
            return Ok(());
        }
        let primary = Currencies::primary(db)?;
        let names: BTreeMap<CategoryId, String> = repo::categories::list(db)?
            .into_iter()
            .map(|category| (category.id, category.name))
            .collect();
        let uncategorized_name = i18n::text(self.state.language, Key::Uncategorized).to_owned();
        let by_id: BTreeMap<TransactionId, &TransactionItem> =
            items.iter().map(|transaction| (transaction.id, transaction)).collect();

        let mut grouped: BTreeMap<ReapplyKey, Vec<&ReapplyChange>> = BTreeMap::new();
        for change in &changes {
            grouped
                .entry((change.from_category_id, change.to_category_id))
                .or_default()
                .push(change);
        }
        let mut groups: Vec<ReapplyGroup> = grouped
            .into_iter()
            .map(|((from_category_id, to_category_id), same)| {
                let bookings: Vec<&TransactionItem> = same
                    .iter()
                    .filter_map(|change| by_id.get(&change.transaction_id).copied())
                    .collect();
                let sum = bookings
                    .iter()
                    .all(|booking| booking.currency == primary)
                    .then(|| Money {
                        minor: bookings.iter().map(|booking| booking.amount.minor).sum(),
                        currency: primary,
                    });
                ReapplyGroup {
                    from_category_id,
                    from_name: from_category_id
                        .map(|id| names.get(&id).cloned().unwrap_or_else(|| id.0.to_string()))
                        .unwrap_or_else(|| uncategorized_name.clone()),
                    to_category_id,
                    to_name: names
                        .get(&to_category_id)
                        .cloned()
                        .unwrap_or_else(|| to_category_id.0.to_string()),
                    count: same.len(),
                    sum,
                    currency: primary,
                }
            })
            .collect();
        groups.sort_by(|left, right| {
            right
                .count
                .cmp(&left.count)
                .then_with(|| left.from_name.cmp(&right.from_name))
                .then_with(|| left.to_name.cmp(&right.to_name))
        });
        self.state.reapply_preview = Some(ReapplyPreview {
            changes,
            groups,
            shown_count: items.len(),
        });
        Ok(())
    }

    /// Closes the preview dialog without changing anything.
    pub fn cancel_reapply_rules(&mut self) {
        self.state.reapply_preview = None;
    }

    /// Applies exactly the stored preview's changes minus the excluded groups.
    pub fn confirm_reapply_rules(
        &mut self,
        db: &Database,
        excluded: &BTreeSet<ReapplyKey>,
        today: Date,
    ) -> Result<(), AppError> {
        let Some(preview) = self.state.reapply_preview.take() else {
            return Ok(());
        };
        let current: BTreeMap<TransactionId, Option<CategoryId>> =
            repo::transactions::find_items(db, &TransactionFilter::default(), None)?
                .into_iter()
                .map(|transaction| (transaction.id, transaction.category_id))
                .collect();
        let changes: Vec<ReapplyChange> = preview
            .changes
            .into_iter()
            .filter(|change| !excluded.contains(&(change.from_category_id, change.to_category_id)))
            .filter(|change| current.get(&change.transaction_id) == Some(&change.from_category_id))
            .collect();
        let count = if changes.is_empty() {
            0
        } else {
            repo::transactions::update_categories(
                db,
                &changes
                    .iter()
                    .map(|change| (change.transaction_id, Some(change.to_category_id)))
                    .collect::<Vec<_>>(),
            )?
        };
        if count > 0 {
            self.reapply_undo = changes;
        }
        self.refresh(db, today)?;
        self.state.message = Some(i18n::format(
            self.state.language,
            Key::MessageReapplied,
            &[Arg::from(count)],
        ));
        self.state.can_undo_reapply = !self.reapply_undo.is_empty();
        Ok(())
    }

    /// Restores the previous categories of the last re-apply.
    pub fn undo_reapply_rules(&mut self, db: &Database, today: Date) -> Result<(), AppError> {
        if self.reapply_undo.is_empty() {
            return Ok(());
        }
        let current: BTreeMap<TransactionId, Option<CategoryId>> =
            repo::transactions::find_items(db, &TransactionFilter::default(), None)?
                .into_iter()
                .map(|transaction| (transaction.id, transaction.category_id))
                .collect();
        let previous: Vec<(TransactionId, Option<CategoryId>)> = self
            .reapply_undo
            .iter()
            .filter(|change| current.get(&change.transaction_id) == Some(&Some(change.to_category_id)))
            .map(|change| (change.transaction_id, change.from_category_id))
            .collect();
        let count = if previous.is_empty() {
            0
        } else {
            repo::transactions::update_categories(db, &previous)?
        };
        self.reapply_undo.clear();
        self.refresh(db, today)?;
        self.state.message = Some(i18n::format(
            self.state.language,
            Key::MessageReapplyUndone,
            &[Arg::from(count)],
        ));
        self.state.can_undo_reapply = false;
        Ok(())
    }

    /// Exports exactly what is currently shown.
    pub fn export_csv(&mut self, db: &Database, file: &Path) -> Result<(), AppError> {
        let balances: Option<BTreeMap<TransactionId, Money>> = self
            .state
            .balances
            .as_ref()
            .map(|entries| entries.iter().map(|(id, entry)| (*id, entry.balance)).collect());
        let name = file.file_name().map_or_else(
            || file.to_string_lossy().into_owned(),
            |name| name.to_string_lossy().into_owned(),
        );
        match crate::report::csv::export_to_csv(&self.state.transactions, file, balances.as_ref(), self.state.language)
        {
            Ok(()) => {
                self.state.message = Some(i18n::format(
                    self.state.language,
                    Key::MessageExportedCount,
                    &[Arg::from(self.state.transactions.len()), Arg::from(name)],
                ));
                let _ = db;
                Ok(())
            }
            Err(_) => {
                self.state.error = Some(i18n::text(self.state.language, Key::ErrorExportFailed).to_owned());
                let _ = db;
                Ok(())
            }
        }
    }

    /// Reads the selected statement files and opens the import preview (nothing is stored yet).
    pub fn open_import(
        &mut self,
        db: &Database,
        files: &[PathBuf],
        mapping: Option<&CsvMapping>,
        today: Date,
    ) -> Result<(), AppError> {
        self.read_import_files(db, files, |_| mapping.cloned(), today)
    }

    fn read_import_files(
        &mut self,
        db: &Database,
        files: &[PathBuf],
        mapping_for: impl Fn(&Path) -> Option<CsvMapping>,
        today: Date,
    ) -> Result<(), AppError> {
        if files.is_empty() {
            return Ok(());
        }
        if files.len() > MAX_IMPORT_FILES {
            self.state.error = Some(i18n::format(
                self.state.language,
                Key::ErrorImportTooManyFiles,
                &[Arg::from(MAX_IMPORT_FILES)],
            ));
            return Ok(());
        }
        let total_bytes: u64 = files
            .iter()
            .map(|file| std::fs::metadata(file).map_or(0, |metadata| metadata.len()))
            .sum();
        if total_bytes > MAX_IMPORT_TOTAL_BYTES {
            self.state.error = Some(i18n::format(
                self.state.language,
                Key::ErrorImportFilesTooLarge,
                &[Arg::from(i64::try_from(MAX_IMPORT_TOTAL_MB).unwrap_or(i64::MAX))],
            ));
            return Ok(());
        }

        let mut entries = Vec::new();
        let mut failures = Vec::new();
        for file in files {
            match read_statement(file, mapping_for(file.as_path()), today, self.state.language) {
                Ok(statement) => entries.push(ImportEntry {
                    file: file.clone(),
                    statement,
                }),
                Err(message) => failures.push(ImportFailure {
                    file_name: display_name(file),
                    message,
                }),
            }
        }
        if entries.is_empty() {
            self.state.import_preview = None;
            self.state.error = Some(if failures.len() == 1 {
                failures[0].message.clone()
            } else {
                let joined = failures
                    .iter()
                    .map(|failure| format!("{}: {}", failure.file_name, failure.message))
                    .collect::<Vec<_>>()
                    .join(", ");
                i18n::format(
                    self.state.language,
                    Key::ErrorImportNoReadableFile,
                    &[Arg::from(joined)],
                )
            });
            return Ok(());
        }

        let mut indexed: Vec<(usize, ImportEntry)> = entries.into_iter().enumerate().collect();
        indexed.sort_by_key(|(index, entry)| {
            let bookings = &entry.statement.bookings;
            (
                !bookings.is_empty(),
                bookings.iter().map(|booking| booking.date).max().unwrap_or(Date::MIN),
                bookings.iter().map(|booking| booking.date).min().unwrap_or(Date::MIN),
                *index,
            )
        });
        let ordered: Vec<ImportEntry> = indexed.into_iter().map(|(_, entry)| entry).collect();

        let mut ibans: Vec<String> = Vec::new();
        for entry in &ordered {
            if let Some(iban) = entry.statement.account_iban.as_deref().map(crate::iban::normalize)
                && !ibans.contains(&iban)
            {
                ibans.push(iban);
            }
        }
        if ibans.len() > 1 {
            self.state.error = Some(i18n::format(
                self.state.language,
                Key::ErrorImportDifferentAccounts,
                &[Arg::from(
                    ibans
                        .iter()
                        .map(|iban| crate::iban::mask(iban))
                        .collect::<Vec<_>>()
                        .join(", "),
                )],
            ));
            return Ok(());
        }

        let statement = ordered
            .iter()
            .find(|entry| entry.statement.account_iban.is_some())
            .map_or_else(
                || &ordered.last().expect("entries are not empty").statement,
                |entry| &entry.statement,
            );
        let account_id = match_account(db, statement)
            .or_else(|| {
                self.state
                    .import_preview
                    .as_ref()
                    .and_then(|preview| preview.account_id)
            })
            .or(self.state.account_id)
            .or_else(|| {
                self.state
                    .accounts
                    .first()
                    .map(|account| account.id)
                    .filter(|_| self.state.accounts.len() == 1)
            });
        let newest = &ordered.last().expect("entries are not empty").statement;
        let allowed = account_id
            .and_then(|id| balance_update_allowed(db, id, newest, today).ok())
            .unwrap_or(false);
        self.state.import_preview = Some(ImportPreview {
            files: files.to_vec(),
            entries: ordered,
            failures,
            account_id,
            balance_update_allowed: allowed,
        });
        self.state.error = None;
        Ok(())
    }

    /// Re-reads the CSV files with a user-adjusted column mapping.
    pub fn remap_import(&mut self, db: &Database, mapping: CsvMapping, today: Date) -> Result<(), AppError> {
        let Some(preview) = self.state.import_preview.clone() else {
            return Ok(());
        };
        let first_csv: Option<CsvLayout> = preview
            .entries
            .iter()
            .find(|entry| entry.statement.csv.is_some())
            .and_then(|entry| entry.statement.csv.clone());
        let mapping = mapping.clone();
        self.read_import_files(
            db,
            &preview.files,
            |file| {
                let entry = preview.entries.iter().find(|entry| entry.file == file);
                match (first_csv.as_ref(), entry.and_then(|entry| entry.statement.csv.as_ref())) {
                    (Some(first), Some(csv)) if csv.headers == first.headers => Some(mapping.clone()),
                    _ => None,
                }
            },
            today,
        )
    }

    /// Picks the account for the open preview.
    pub fn select_import_account(&mut self, db: &Database, account_id: AccountId, today: Date) -> Result<(), AppError> {
        if let Some(preview) = self.state.import_preview.as_mut() {
            preview.account_id = Some(account_id);
            preview.balance_update_allowed = false;
        }
        let Some(preview) = self.state.import_preview.clone() else {
            return Ok(());
        };
        let allowed = balance_update_allowed(db, account_id, &preview.newest().statement, today).unwrap_or(false);
        if let Some(current) = self.state.import_preview.as_mut()
            && current.account_id == Some(account_id)
        {
            current.balance_update_allowed = allowed;
        }
        Ok(())
    }

    /// Closes the import preview.
    pub fn cancel_import(&mut self) {
        self.state.import_preview = None;
    }

    /// Imports the previewed entries in order, each with its own dedup hashing and audit entry.
    pub fn confirm_import(&mut self, db: &Database, update_balance: bool, today: Date) -> Result<(), AppError> {
        let Some(preview) = self.state.import_preview.clone() else {
            return Ok(());
        };
        let Some(account_id) = preview.account_id else {
            return Ok(());
        };
        self.state.is_importing = true;
        let newest_index = preview.entries.len() - 1;
        let mut aggregate: Option<ImportReport> = None;
        let mut imported_files = 0usize;
        for (index, entry) in preview.entries.iter().enumerate() {
            let should_update_balance = update_balance && index == newest_index;
            let result = self
                .busy
                .run(|| import::import(db, account_id, &entry.statement, should_update_balance, today));
            match result {
                Ok(report) => {
                    aggregate = Some(match aggregate {
                        Some(previous) => combine_reports(previous, report),
                        None => report,
                    });
                    imported_files += 1;
                }
                Err(error) => {
                    let failed_name = display_name(&entry.file);
                    let failed_message = import_error_message(&error, self.state.language);
                    self.state.import_preview = None;
                    self.state.is_importing = false;
                    self.state.error = Some(i18n::format(
                        self.state.language,
                        Key::ErrorImportStopped,
                        &[
                            Arg::from(failed_name),
                            Arg::from(failed_message),
                            Arg::from(imported_files),
                        ],
                    ));
                    self.refresh(db, today)?;
                    return Ok(());
                }
            }
        }
        let Some(report) = aggregate else {
            self.state.is_importing = false;
            return Ok(());
        };
        self.state.import_preview = None;
        self.state.is_importing = false;
        self.state.period = Period::All;
        self.state.month = None;
        self.state.custom_range = None;
        self.state.selection = AccountSelection::Account(account_id);
        self.state.account_id = Some(account_id);
        self.state.message = Some(import_summary(self.state.language, &report, imported_files));
        self.clear_selection();
        self.refresh(db, today)
    }

    /// Clears the error and success message.
    pub fn clear_messages(&mut self) {
        self.state.error = None;
        self.state.message = None;
    }

    fn user_error(&mut self, key: Key) -> Result<(), AppError> {
        self.state.error = Some(i18n::text(self.state.language, key).to_owned());
        Ok(())
    }

    fn record_error(&mut self, error: AppError) -> Result<(), AppError> {
        self.state.is_loading = false;
        let key = match &error {
            AppError::Message(key) => *key,
            AppError::Repo(_) | AppError::Db(_) => Key::ErrorUnexpected,
        };
        self.state.error = Some(i18n::text(self.state.language, key).to_owned());
        Err(error)
    }
}

impl Default for TransactionsView {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

/// Converts a money amount to the decimal the rule matcher and hashes expect.
fn decimal_amount(money: Money) -> Decimal {
    Decimal::new(money.minor, money.currency.minor_exponent())
}

/// Whether `id` still exists, without a dedicated repository lookup.
fn transaction_exists(db: &Database, id: TransactionId) -> Result<bool, AppError> {
    Ok(repo::transactions::find_items(db, &TransactionFilter::default(), None)?
        .iter()
        .any(|transaction| transaction.id == id))
}

/// The grouping form of an account row.
fn grouping_account(account: &AccountItem) -> GroupingAccount {
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

fn grouping_accounts(accounts: &[AccountItem]) -> Vec<GroupingAccount> {
    accounts.iter().map(grouping_account).collect()
}

fn group_accounts(
    accounts: &[AccountItem],
    custom_groups: &[AccountGroup],
    primary: Currency,
) -> Vec<AccountGroupView> {
    account_grouping::group(&grouping_accounts(accounts), custom_groups, primary)
}

/// Loads every account row with its bank name and unread counter, like the Accounts view.
fn load_account_items(db: &Database) -> Result<Vec<AccountItem>, AppError> {
    let accounts = repo::accounts::list(db)?;
    let bank_accesses = repo::bank_connections::list_items(db)?;
    let new_counts = repo::transactions::count_new(db)?;
    let bank_names: BTreeMap<_, _> = bank_accesses
        .iter()
        .map(|bank| (bank.id, bank.bank_name.clone()))
        .collect();
    Ok(accounts
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
        .collect())
}

fn load_chart_collapsed(db: &Database) -> Result<bool, AppError> {
    let stored = crate::settings::get_setting(db, CHART_COLLAPSED_KEY)?;
    Ok(stored
        .as_deref()
        .and_then(|value| match value {
            "true" => Some(true),
            "false" => Some(false),
            _ => None,
        })
        .unwrap_or(false))
}

fn display_name(file: &Path) -> String {
    file.file_name().map_or_else(
        || file.to_string_lossy().into_owned(),
        |name| name.to_string_lossy().into_owned(),
    )
}

/// Reads one statement, returning FinDesk's user-facing reason on failure.
fn read_statement(
    file: &Path,
    mapping: Option<CsvMapping>,
    today: Date,
    language: Language,
) -> Result<ParsedStatement, String> {
    if !file.is_file() {
        return Err(i18n::format(
            language,
            Key::ErrorFileNotFound,
            &[Arg::from(display_name(file))],
        ));
    }
    let length = std::fs::metadata(file).map_or(0, |metadata| metadata.len());
    if length > import::MAX_FILE_BYTES {
        return Err(i18n::text(language, Key::ErrorFileTooLarge).to_owned());
    }
    let bytes = std::fs::read(file).map_err(|_| i18n::text(language, Key::ErrorUnexpected).to_owned())?;
    import::read(&display_name(file), &bytes, mapping.as_ref(), today)
        .map_err(|error| import_error_text(&error, language))
}

fn import_error_text(error: &ImportError, language: Language) -> String {
    match error {
        ImportError::FileTooLarge => i18n::text(language, Key::ErrorFileTooLarge).to_owned(),
        ImportError::XlsxTooLarge => i18n::text(language, Key::ErrorXlsxTooLarge).to_owned(),
        ImportError::XlsxUnreadable => i18n::text(language, Key::ErrorXlsxUnreadable).to_owned(),
        // Parser diagnostics carry the specific reason; the surrounding UI labels it as unreadable.
        ImportError::Invalid(detail) => detail.clone(),
    }
}

fn import_error_message(error: &ImportServiceError, language: Language) -> String {
    match error {
        ImportServiceError::AccountNotFound => i18n::text(language, Key::ErrorAccountNotFound).to_owned(),
        ImportServiceError::SeveralAccounts(detail) => detail.clone(),
        ImportServiceError::Repo(_) | ImportServiceError::Money(_) | ImportServiceError::Db(_) => {
            i18n::text(language, Key::ErrorUnexpected).to_owned()
        }
    }
}

fn combine_reports(mut previous: ImportReport, next: ImportReport) -> ImportReport {
    previous.imported += next.imported;
    previous.duplicates += next.duplicates;
    previous.categorized += next.categorized;
    previous.balance_updated = next.balance_updated;
    previous.probable_duplicates += next.probable_duplicates;
    previous.wrong_currency += next.wrong_currency;
    previous.currency = next.currency;
    previous.previously_deleted += next.previously_deleted;
    previous.invalid_amount += next.invalid_amount;
    previous
}

fn import_summary(language: Language, report: &ImportReport, files: usize) -> String {
    let mut text = if files <= 1 {
        i18n::format(language, Key::ImportSummary, &[Arg::from(report.imported)])
    } else {
        i18n::format(
            language,
            Key::ImportSummaryFiles,
            &[Arg::from(report.imported), Arg::from(files)],
        )
    };
    if report.categorized > 0 {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryAuto,
            &[Arg::from(report.categorized)],
        ));
    }
    if report.duplicates > 0 {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryKnown,
            &[Arg::from(report.duplicates)],
        ));
    }
    if report.probable_duplicates > 0 {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryProbable,
            &[Arg::from(report.probable_duplicates)],
        ));
    }
    if report.previously_deleted > 0 {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryDeleted,
            &[Arg::from(report.previously_deleted)],
        ));
    }
    if report.wrong_currency > 0 {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryWrongCurrency,
            &[Arg::from(report.wrong_currency)],
        ));
    }
    if let Some(balance) = report.balance_updated
        && let Some(money) = decimal_to_money(balance, report.currency)
    {
        text.push_str(&i18n::format(
            language,
            Key::ImportSummaryBalance,
            &[Arg::from(crate::format::money(money, language, false))],
        ));
    }
    text
}

fn decimal_to_money(value: Decimal, currency: Currency) -> Option<Money> {
    let normalized = value.normalize();
    let exponent = i32::try_from(currency.minor_exponent()).ok()?;
    let scale = i32::try_from(normalized.scale()).ok()?;
    if scale > exponent {
        return None;
    }
    let factor = 10_i128.checked_pow(u32::try_from(exponent - scale).ok()?)?;
    let minor = normalized.mantissa().checked_mul(factor)?;
    i64::try_from(minor).ok().map(|minor| Money { minor, currency })
}

#[cfg(test)]
mod tests;
