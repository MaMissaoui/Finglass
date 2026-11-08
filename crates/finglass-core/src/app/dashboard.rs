//! The Dashboard page state, without any UI.
//!
//! This is the UI-free view of FinDesk's `DashboardViewModel`: the selected and loaded months, the
//! clock and first-run flags, and a [`DashboardView`] whose rows and texts are ready to render.
//! All figures come from [`crate::dashboard::load_dashboard`]; the page never recomputes money.

use time::{Date, OffsetDateTime, UtcOffset};

use crate::bank_text;
use crate::dashboard::{self, DashboardFigures, YearMonth};
use crate::db::Database;
use crate::format;
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{CategoryId, TransactionId};
use crate::money::{Currency, Money};
use crate::recurring::Cadence;
use crate::repo::RepoError;
use crate::report::format::month_name;

/// The local calendar date, falling back to UTC when the machine's offset cannot be determined.
///
/// The dashboard's "today" uses the local date: near midnight UTC would otherwise show the wrong month.
#[must_use]
pub fn local_today() -> Date {
    let offset = UtcOffset::current_local_offset().unwrap_or(UtcOffset::UTC);
    OffsetDateTime::now_utc().to_offset(offset).date()
}

/// How a balance should be coloured by the page.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BalanceTone {
    /// Money above zero.
    Positive,
    /// Exactly zero.
    Zero,
    /// Money below zero.
    Negative,
}

/// The filter a dashboard click asks the Transactions page to show.
///
/// M4c reads this after a click; the Transactions page is still a placeholder here.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct NavigationArgs {
    /// A specific past month, or `None` for the Transactions page's default period.
    pub month: Option<YearMonth>,
    /// One category, or `None` for every category.
    pub category_id: Option<CategoryId>,
    /// Only bookings without a category.
    pub uncategorized_only: bool,
    /// A free-text partner search.
    pub text: Option<String>,
}

impl NavigationArgs {
    /// A link with no filter: "view all" and ordinary navigation.
    #[must_use]
    pub fn all() -> Self {
        Self::default()
    }

    /// The `NavigationArgs` for one category row, as FinDesk's dashboard builds it.
    #[must_use]
    pub fn category(selected: YearMonth, current: YearMonth, category_id: CategoryId) -> Self {
        Self {
            month: past_month(selected, current),
            category_id: Some(category_id),
            uncategorized_only: false,
            text: None,
        }
    }

    /// The `NavigationArgs` for the pending-tasks card and the uncategorized slice.
    #[must_use]
    pub fn uncategorized(selected: YearMonth, current: YearMonth) -> Self {
        Self {
            month: past_month(selected, current),
            category_id: None,
            uncategorized_only: true,
            text: None,
        }
    }

    /// The `NavigationArgs` for "show all from this partner".
    #[must_use]
    pub fn partner(text: impl Into<String>) -> Self {
        Self {
            text: Some(text.into()),
            ..Self::default()
        }
    }
}

fn past_month(selected: YearMonth, current: YearMonth) -> Option<YearMonth> {
    (selected != current).then_some(selected)
}

/// One slice of the spending split or the tax chart.
#[derive(Debug, Clone, PartialEq)]
pub struct CategorySliceView {
    /// The display label, already translated.
    pub label: String,
    /// The slice's amount.
    pub amount: Money,
    /// The category colour, `#RRGGBB`.
    pub color: String,
    /// The category, or `None` for uncategorized.
    pub category_id: Option<CategoryId>,
    /// The slice's share of the total, for the split bar.
    pub fraction: f32,
}

/// One legend row of the spending split.
#[derive(Debug, Clone, PartialEq)]
pub struct CategoryRowView {
    /// The display label, already translated.
    pub label: String,
    /// The percentage text, e.g. "49%".
    pub percent_text: String,
    /// The formatted amount.
    pub amount_text: String,
    /// The category colour, `#RRGGBB`.
    pub color: String,
    /// The category, or `None` for uncategorized or the folded "more" row.
    pub category_id: Option<CategoryId>,
    /// The row folds the categories beyond the first eight.
    pub is_more: bool,
    /// The row's share of the split bar, for the folded row too.
    pub fraction: f32,
}

/// One month of the income-vs-expenses chart.
#[derive(Debug, Clone, PartialEq)]
pub struct MonthBarView {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
    /// The month's display label, e.g. "September".
    pub label: String,
    /// Money in, transfers excluded.
    pub income: Money,
    /// Money out, transfers excluded, as a positive amount.
    pub expenses: Money,
    /// The income bar's share of the chart's top, 0 to 1.
    pub income_fraction: f32,
    /// The expenses bar's share of the chart's top, 0 to 1.
    pub expenses_fraction: f32,
    /// Whether this is still the running month.
    pub partial: bool,
}

/// One budget row.
#[derive(Debug, Clone, PartialEq)]
pub struct BudgetRowView {
    /// The category.
    pub category_id: CategoryId,
    /// The category's name.
    pub name: String,
    /// The category's colour, `#RRGGBB`.
    pub color: String,
    /// The monthly limit.
    pub limit: Money,
    /// What was spent this month, as a positive amount.
    pub spent: Money,
    /// Spent divided by the limit, clamped for the progress bar.
    pub ratio: f32,
    /// Whether spending exceeds the limit.
    pub is_over: bool,
    /// Whether spending is ahead of the month's progress.
    pub warning: bool,
    /// The "spent of limit" line.
    pub amount_text: String,
    /// The "left" or "over budget" line.
    pub rest_text: String,
}

/// One recurring payment due in the next 30 days.
#[derive(Debug, Clone, PartialEq)]
pub struct UpcomingRowView {
    /// The display name, cleaned for the UI.
    pub name: String,
    /// The next expected date.
    pub next_date: Date,
    /// The formatted next date.
    pub date_text: String,
    /// The typical amount, signed.
    pub amount: Money,
    /// The formatted typical amount.
    pub amount_text: String,
    /// How often it repeats.
    pub cadence: Cadence,
    /// The second line: cadence, category and variable-amount note.
    pub meta: String,
    /// Whether the amount is money in.
    pub is_income: bool,
}

/// One recent booking.
#[derive(Debug, Clone, PartialEq)]
pub struct TransactionRowView {
    /// The row id.
    pub id: TransactionId,
    /// The booking date.
    pub date: Date,
    /// The formatted booking date.
    pub date_text: String,
    /// The cleaned title.
    pub title: String,
    /// The category name, or the translated "Uncategorized".
    pub meta: String,
    /// The signed amount.
    pub amount: Money,
    /// The formatted signed amount.
    pub amount_text: String,
    /// Whether the amount is money out.
    pub is_expense: bool,
    /// The raw partner name, for "show all from this partner".
    pub partner: Option<String>,
}

/// The rows and strings the Dashboard page renders.
#[derive(Debug, Clone, PartialEq)]
pub struct DashboardView {
    /// The month the header shows.
    pub selected_month: YearMonth,
    /// The month the figures below belong to.
    pub loaded_month: YearMonth,
    /// The oldest month with a booking in the primary currency.
    pub earliest_month: YearMonth,
    /// Today's calendar month.
    pub current_month: YearMonth,
    /// Whether the selected month is the current one.
    pub is_current_month: bool,
    /// Whether the previous-month control is enabled.
    pub previous_enabled: bool,
    /// Whether the next-month control is enabled.
    pub next_enabled: bool,
    /// The selected month as "November 2026".
    pub month_label: String,
    /// The currency of every total and chart.
    pub currency: Currency,
    /// The sum of every account's balance in the primary currency.
    pub total_balance: Money,
    /// Money on checking, savings and credit-card accounts.
    pub available_balance: Money,
    /// The sum of loan accounts' balances.
    pub loan_balance: Money,
    /// Whether the accounts include loans.
    pub has_loans: bool,
    /// Money available at the end of the previous month, when known.
    pub carry_forward: Option<Money>,
    /// What the available accounts moved outside income and expenses.
    pub other_movements: Option<Money>,
    /// The day the available balance is stated for; `None` in the current month.
    pub balance_date: Option<Date>,
    /// Balances of accounts in other currencies, never added to the totals.
    pub other_balances: Vec<(Currency, Money)>,
    /// Income in the loaded month, transfers excluded.
    pub monthly_income: Money,
    /// Spending in the loaded month, transfers excluded, as a positive amount.
    pub monthly_expenses: Money,
    /// Uncategorized bookings in the loaded month, in every currency.
    pub uncategorized_count: usize,
    /// Tax-relevant spending from January 1 through the loaded month.
    pub tax_total: Money,
    /// Spending by category in the loaded month.
    pub expenses_by_category: Vec<CategorySliceView>,
    /// The legend rows, with categories beyond eight folded into "more".
    pub expense_rows: Vec<CategoryRowView>,
    /// Tax-relevant spending by category, raw and complete.
    pub tax_deductions_by_category: Vec<CategorySliceView>,
    /// The six months ending with the loaded month.
    pub month_bars: Vec<MonthBarView>,
    /// How far through the loaded month we are, 0 to 1.
    pub month_progress: f32,
    /// Money moved out between the user's own accounts this month.
    pub transfers_this_month: Money,
    /// Budgets for the loaded month, most-used first.
    pub budgets: Vec<BudgetRowView>,
    /// Recurring payments expected in the next 30 days.
    pub upcoming: Vec<UpcomingRowView>,
    /// The newest bookings, independent of the loaded month.
    pub recent_transactions: Vec<TransactionRowView>,
    /// The available card's title.
    pub available_title: String,
    /// The available card's formatted value.
    pub available_value: String,
    /// The available card's carry-forward, movement, loan and currency lines.
    pub available_subtitle: String,
    /// The available value's colour tone.
    pub available_tone: BalanceTone,
    /// The income card's title.
    pub income_title: String,
    /// The income card's formatted value.
    pub income_value: String,
    /// The expenses card's title.
    pub expenses_title: String,
    /// The expenses card's formatted value.
    pub expenses_value: String,
    /// The tax card's title.
    pub tax_title: String,
    /// The tax card's formatted total.
    pub tax_value: String,
    /// The tax card's hint when there is nothing to chart.
    pub tax_hint: String,
    /// The pending-tasks card's title.
    pub pending_title: String,
    /// The pending-tasks card's formatted count.
    pub pending_value: String,
    /// The pending-tasks card's subtitle.
    pub pending_subtitle: String,
    /// Whether the pending-tasks card opens the uncategorized list.
    pub pending_enabled: bool,
    /// The expenses-by-category card's title.
    pub expenses_section_title: String,
    /// The text shown when there are no expenses.
    pub expenses_empty_text: String,
    /// The transfers line below the split, when transfers happened.
    pub transfers_note: Option<String>,
    /// The income-vs-expenses card's title.
    pub chart_title: String,
    /// The text shown when the chart has no bookings.
    pub chart_empty_text: Option<String>,
    /// The chart's top axis label.
    pub chart_axis_top: String,
    /// The chart's middle axis label.
    pub chart_axis_mid: String,
    /// The chart's zero axis label.
    pub chart_axis_zero: String,
    /// The income legend label.
    pub income_legend: String,
    /// The expenses legend label.
    pub expenses_legend: String,
    /// The note explaining pale bars in the running month.
    pub partial_month_note: String,
    /// The budgets card's title.
    pub budgets_title: String,
    /// The upcoming card's title.
    pub upcoming_title: String,
    /// The recent transactions card's title.
    pub recent_title: String,
    /// The text shown when there are no recent bookings.
    pub recent_empty_text: String,
    /// The "view all" action label.
    pub view_all_label: String,
}

impl DashboardView {
    /// Turns one dashboard computation into the rows and texts the page shows.
    #[must_use]
    ///
    /// `unlinked_documents` is the number of receipts not linked to a booking; pending tasks count them together with
    /// the uncategorized bookings, as FinDesk does.
    pub fn from_figures(figures: DashboardFigures, unlinked_documents: usize, language: Language) -> Self {
        let loaded = figures.loaded_month;
        let current_loaded = loaded == figures.current_month;
        let currency = figures.currency;

        let expense_slices = category_slices(&figures.expenses_by_category, language);
        let tax_slices = category_slices(&figures.tax_deductions_by_category, language);
        let tax_total = sum_money(tax_slices.iter().map(|slice| slice.amount), currency);
        let month_bars = month_bars(&figures.last_months, language);
        let chart_has_data = month_bars
            .iter()
            .any(|bar| bar.income.minor != 0 || bar.expenses.minor != 0);
        let chart_empty_text = (!chart_has_data).then(|| {
            if current_loaded {
                i18n::text(language, Key::NoBookingsSixMonths).to_owned()
            } else {
                i18n::format(
                    language,
                    Key::NoBookingsSixMonthsTo,
                    &[Arg::Str(month_text(loaded, language))],
                )
            }
        });

        let axis_top_minor = month_bars
            .iter()
            .flat_map(|bar| [bar.income.minor, bar.expenses.minor])
            .max()
            .map_or(0, nice_ceiling_minor);

        let pending = figures.uncategorized_count + unlinked_documents;
        let pending_subtitle = if pending == 0 {
            i18n::text(language, Key::NothingPending).to_owned()
        } else {
            i18n::format(
                language,
                Key::PendingSubtitle,
                &[Arg::from(figures.uncategorized_count), Arg::from(unlinked_documents)],
            )
        };

        let budgets = figures
            .budgets
            .iter()
            .map(|budget| budget_row(budget, language, figures.month_progress))
            .collect();

        let upcoming = figures
            .upcoming
            .iter()
            .map(|payment| UpcomingRowView {
                name: bank_text::display_partner(Some(&payment.name)).unwrap_or_else(|| payment.name.clone()),
                next_date: payment.next_date,
                date_text: format::date(payment.next_date),
                amount: payment.typical_amount,
                amount_text: format::money(payment.typical_amount, language, true),
                cadence: payment.cadence,
                meta: upcoming_meta(payment, language),
                is_income: payment.is_income(),
            })
            .collect();

        let recent_transactions = figures
            .recent_transactions
            .iter()
            .map(|transaction| TransactionRowView {
                id: transaction.id,
                date: transaction.date,
                date_text: format::date(transaction.date),
                title: bank_text::display_description(transaction),
                meta: transaction
                    .category_name
                    .clone()
                    .unwrap_or_else(|| i18n::text(language, Key::Uncategorized).to_owned()),
                amount: transaction.amount,
                amount_text: format::money(transaction.amount, language, true),
                is_expense: transaction.is_expense(),
                partner: transaction.partner_name.clone(),
            })
            .collect();

        Self {
            selected_month: figures.selected_month,
            loaded_month: loaded,
            earliest_month: figures.earliest_month,
            current_month: figures.current_month,
            is_current_month: figures.selected_month == figures.current_month,
            previous_enabled: figures.selected_month > figures.earliest_month,
            next_enabled: figures.selected_month < figures.current_month,
            month_label: month_text(figures.selected_month, language),
            currency,
            total_balance: figures.total_balance,
            available_balance: figures.available_balance,
            loan_balance: figures.loan_balance,
            has_loans: figures.has_loans,
            carry_forward: figures.carry_forward,
            other_movements: figures.other_movements,
            balance_date: figures.balance_date,
            other_balances: figures
                .other_balances
                .iter()
                .map(|(currency, amount)| (*currency, *amount))
                .collect(),
            monthly_income: figures.monthly_income,
            monthly_expenses: figures.monthly_expenses,
            uncategorized_count: figures.uncategorized_count,
            tax_total,
            expenses_by_category: expense_slices.clone(),
            expense_rows: category_rows(&expense_slices, language),
            tax_deductions_by_category: tax_slices,
            month_bars,
            month_progress: figures.month_progress as f32,
            transfers_this_month: figures.transfers_this_month,
            budgets,
            upcoming,
            recent_transactions,
            available_title: match figures.balance_date {
                Some(date) => i18n::format(language, Key::AvailableAsOf, &[Arg::Str(format::date(date))]),
                None => i18n::text(language, Key::AvailableBalance).to_owned(),
            },
            available_value: format::money(figures.available_balance, language, false),
            available_subtitle: available_subtitle(&figures, language),
            available_tone: balance_tone(figures.available_balance),
            income_title: month_title(
                current_loaded,
                Key::IncomeThisMonth,
                Key::IncomeInMonth,
                loaded,
                language,
            ),
            income_value: format::money(figures.monthly_income, language, false),
            expenses_title: month_title(
                current_loaded,
                Key::ExpensesThisMonth,
                Key::ExpensesInMonth,
                loaded,
                language,
            ),
            expenses_value: format::money(figures.monthly_expenses, language, false),
            tax_title: i18n::text(language, Key::TaxDeductibleYtd).to_owned(),
            tax_value: format::money(tax_total, language, false),
            tax_hint: i18n::text(language, Key::TaxDeductibleHint).to_owned(),
            pending_title: i18n::text(language, Key::PendingTasks).to_owned(),
            pending_value: pending.to_string(),
            pending_subtitle,
            pending_enabled: pending > 0,
            expenses_section_title: month_title(
                current_loaded,
                Key::ExpensesByCategory,
                Key::ExpensesByCategoryInMonth,
                loaded,
                language,
            ),
            expenses_empty_text: month_title(
                current_loaded,
                Key::NoExpensesThisMonth,
                Key::NoExpensesInMonth,
                loaded,
                language,
            ),
            transfers_note: (figures.transfers_this_month.minor > 0).then(|| {
                i18n::format(
                    language,
                    Key::CategorySplitTransfers,
                    &[Arg::Str(format::money(figures.transfers_this_month, language, false))],
                )
            }),
            chart_title: i18n::text(language, Key::IncomeVsExpenses).to_owned(),
            chart_empty_text,
            chart_axis_top: if axis_top_minor == 0 {
                String::new()
            } else {
                axis_label(axis_top_minor, currency, language)
            },
            chart_axis_mid: if axis_top_minor == 0 {
                String::new()
            } else {
                axis_label(axis_top_minor / 2, currency, language)
            },
            chart_axis_zero: axis_label(0, currency, language),
            income_legend: i18n::text(language, Key::ChartIncome).to_owned(),
            expenses_legend: i18n::text(language, Key::ChartExpenses).to_owned(),
            partial_month_note: i18n::text(language, Key::ChartPartialMonthNote).to_owned(),
            budgets_title: month_title(
                current_loaded,
                Key::BudgetsThisMonth,
                Key::BudgetsInMonth,
                loaded,
                language,
            ),
            upcoming_title: i18n::text(language, Key::Upcoming30Days).to_owned(),
            recent_title: i18n::text(language, Key::RecentTransactions).to_owned(),
            recent_empty_text: i18n::text(language, Key::NoTransactionsYet).to_owned(),
            view_all_label: i18n::text(language, Key::ViewAll).to_owned(),
        }
    }
}

/// The Dashboard page's state.
#[derive(Debug, Clone, PartialEq)]
pub struct DashboardState {
    /// The month the header shows; a load may clamp it.
    pub selected_month: YearMonth,
    /// The month whose figures are loaded; `None` until the first successful load.
    pub loaded_month: Option<YearMonth>,
    /// The oldest month with a booking in the primary currency.
    pub earliest_month: YearMonth,
    /// Today's calendar month at the last load.
    pub current_month: YearMonth,
    /// Every selectable month, newest first, from today's month back to the earliest booking.
    pub months: Vec<YearMonth>,
    /// Whether a load is running.
    pub is_loading: bool,
    /// The last error, already localized.
    pub error: Option<String>,
    /// Whether the profile has any account.
    pub has_accounts: bool,
    /// The rendered rows and texts once a load has published them.
    pub view: Option<DashboardView>,
    /// The language for user-visible texts.
    pub language: Language,
}

impl DashboardState {
    /// Builds an empty state for `language`, opening on `today`'s month.
    #[must_use]
    pub fn new(language: Language, today: Date) -> Self {
        let current = YearMonth::from_date(today);
        Self {
            selected_month: current,
            loaded_month: None,
            earliest_month: current,
            current_month: current,
            months: vec![current],
            is_loading: false,
            error: None,
            has_accounts: false,
            view: None,
            language,
        }
    }

    /// Whether the page shows the getting-started card: the first load finished without accounts.
    #[must_use]
    pub fn is_first_run(&self) -> bool {
        self.loaded_month.is_some() && !self.has_accounts
    }

    /// Whether the page has figures other than the first-run card or the busy indicator.
    #[must_use]
    pub fn has_figures(&self) -> bool {
        self.view.is_some() && !self.is_first_run()
    }

    /// Marks a load as running for `month` and updates the header immediately.
    ///
    /// `today` refreshes the current month first, so a stale [`Self::current_month`] from a previous
    /// load cannot clamp a newer month. Only the future is cut off here; the load clamps the oldest month.
    ///
    /// The loaded month's figures stay visible until the new load publishes, exactly like FinDesk.
    pub fn begin_load(&mut self, month: YearMonth, today: Date) {
        self.current_month = YearMonth::from_date(today);
        self.selected_month = month.min(self.current_month);
        self.months = crate::dashboard::month_options(self.earliest_month, self.current_month);
        self.is_loading = true;
        self.error = None;

        if let Some(view) = &mut self.view {
            view.selected_month = self.selected_month;
            view.current_month = self.current_month;
            view.month_label = month_text(view.selected_month, self.language);
            view.is_current_month = view.selected_month == view.current_month;
            view.previous_enabled = view.selected_month > view.earliest_month;
            view.next_enabled = view.selected_month < view.current_month;
        }
    }

    /// Opens the Dashboard on `today`'s month, as FinDesk's new view model does.
    pub fn open(&mut self, today: Date) {
        self.begin_load(YearMonth::from_date(today), today);
    }

    /// Loads the selected month and publishes its figures.
    pub fn load(&mut self, db: &Database, today: Date) -> Result<(), RepoError> {
        self.begin_load(self.selected_month, today);
        match dashboard::load_dashboard(db, today, self.selected_month) {
            Ok(figures) => {
                let unlinked_documents = match crate::repo::documents::find_items(db, None, true, None) {
                    Ok(items) => items.len(),
                    Err(error) => {
                        self.is_loading = false;
                        self.error = Some(i18n::text(self.language, Key::ErrorUnexpected).to_owned());
                        return Err(error);
                    }
                };
                self.selected_month = figures.selected_month;
                self.loaded_month = Some(figures.loaded_month);
                self.earliest_month = figures.earliest_month;
                self.current_month = figures.current_month;
                self.months = crate::dashboard::month_options(self.earliest_month, self.current_month);
                self.has_accounts = figures.has_accounts;
                self.view = Some(DashboardView::from_figures(figures, unlinked_documents, self.language));
                self.is_loading = false;
                Ok(())
            }
            Err(error) => {
                self.is_loading = false;
                self.error = Some(i18n::text(self.language, Key::ErrorUnexpected).to_owned());
                Err(error)
            }
        }
    }

    /// Selects `month` and loads it. Only the future is clamped here; the load clamps the oldest month.
    pub fn select_month(&mut self, db: &Database, today: Date, month: YearMonth) -> Result<(), RepoError> {
        self.begin_load(month, today);
        self.load(db, today)
    }

    /// Switches one month back and loads it.
    pub fn previous_month(&mut self, db: &Database, today: Date) -> Result<(), RepoError> {
        self.select_month(db, today, self.selected_month.minus_months(1))
    }

    /// Switches one month forward and loads it.
    pub fn next_month(&mut self, db: &Database, today: Date) -> Result<(), RepoError> {
        self.select_month(db, today, self.selected_month.plus_months(1))
    }

    /// Returns to today's month and loads it.
    pub fn current_month(&mut self, db: &Database, today: Date) -> Result<(), RepoError> {
        self.select_month(db, today, YearMonth::from_date(today))
    }
}

fn month_title(current_loaded: bool, current_key: Key, month_key: Key, month: YearMonth, language: Language) -> String {
    if current_loaded {
        i18n::text(language, current_key).to_owned()
    } else {
        i18n::format(language, month_key, &[Arg::Str(month_text(month, language))])
    }
}

/// The display label for `month`, e.g. "November 2026".
#[must_use]
pub fn month_text(month: YearMonth, language: Language) -> String {
    let name = month_name(language, month.month);
    let mut characters = name.chars();
    let capitalized = match characters.next() {
        Some(first) => first.to_uppercase().collect::<String>() + characters.as_str(),
        None => String::new(),
    };
    format!("{capitalized} {}", month.year)
}

fn balance_tone(money: Money) -> BalanceTone {
    if money.minor > 0 {
        BalanceTone::Positive
    } else if money.minor < 0 {
        BalanceTone::Negative
    } else {
        BalanceTone::Zero
    }
}

fn category_slices(slices: &[dashboard::ChartSlice], language: Language) -> Vec<CategorySliceView> {
    let total: i64 = slices.iter().map(|slice| slice.amount.minor).sum();
    slices
        .iter()
        .map(|slice| {
            let label = if slice.category_id.is_none() && slice.label == dashboard::UNCATEGORIZED_LABEL {
                i18n::text(language, Key::Uncategorized).to_owned()
            } else {
                slice.label.clone()
            };
            CategorySliceView {
                label,
                amount: slice.amount,
                color: slice
                    .color
                    .clone()
                    .unwrap_or_else(|| dashboard::UNCATEGORIZED_COLOR.to_owned()),
                category_id: slice.category_id,
                fraction: fraction(slice.amount.minor, total),
            }
        })
        .collect()
}

fn category_rows(slices: &[CategorySliceView], language: Language) -> Vec<CategoryRowView> {
    if slices.is_empty() {
        return Vec::new();
    }

    let total: i64 = slices.iter().map(|slice| slice.amount.minor).sum();
    let amounts: Vec<Money> = slices.iter().map(|slice| slice.amount).collect();
    let shares = format::shares(&amounts);
    let visible = slices.len().min(8);
    let mut rows: Vec<CategoryRowView> = slices
        .iter()
        .take(visible)
        .enumerate()
        .map(|(index, slice)| CategoryRowView {
            label: slice.label.clone(),
            percent_text: format::percent(shares[index] as f64 / 100.0, language),
            amount_text: format::money(slice.amount, language, false),
            color: slice.color.clone(),
            category_id: slice.category_id,
            is_more: false,
            fraction: fraction(slice.amount.minor, total),
        })
        .collect();

    if slices.len() > visible {
        let rest = &slices[visible..];
        let rest_total: i64 = rest.iter().map(|slice| slice.amount.minor).sum();
        let rest_percent: i64 = shares[visible..].iter().sum();
        let currency = rest[0].amount.currency;
        rows.push(CategoryRowView {
            label: i18n::format(language, Key::CategorySplitMore, &[Arg::from(rest.len())]),
            percent_text: format::percent(rest_percent as f64 / 100.0, language),
            amount_text: format::money(
                Money {
                    minor: rest_total,
                    currency,
                },
                language,
                false,
            ),
            color: "#9E9E9E".to_owned(),
            category_id: None,
            is_more: true,
            fraction: fraction(rest_total, total),
        });
    }

    rows
}

fn month_bars(months: &[dashboard::MonthBar], language: Language) -> Vec<MonthBarView> {
    let top = months
        .iter()
        .flat_map(|bar| [bar.income.minor, bar.expenses.minor])
        .max()
        .map_or(0, nice_ceiling_minor);

    months
        .iter()
        .map(|bar| MonthBarView {
            year: bar.year,
            month: bar.month,
            label: month_name(language, bar.month).to_owned(),
            income: bar.income,
            expenses: bar.expenses,
            income_fraction: fraction(bar.income.minor, top),
            expenses_fraction: fraction(bar.expenses.minor, top),
            partial: bar.partial,
        })
        .collect()
}

fn fraction(amount: i64, total: i64) -> f32 {
    if total <= 0 { 0.0 } else { amount as f32 / total as f32 }
}

fn nice_ceiling_minor(value: i64) -> i64 {
    if value <= 0 {
        return 0;
    }

    let mut magnitude = 1_i64;
    while magnitude <= value / 10 {
        magnitude *= 10;
    }

    [1, 2, 5, 10]
        .into_iter()
        .find_map(|factor| magnitude.checked_mul(factor).filter(|candidate| *candidate >= value))
        .unwrap_or(value)
}

fn axis_label(minor: i64, currency: Currency, language: Language) -> String {
    let money = Money { minor, currency };
    let unit = 10_i64.pow(currency.minor_exponent());
    if minor < 10 * unit {
        format::money(money, language, false)
    } else {
        format::whole_money(money, language)
    }
}

fn sum_money(amounts: impl Iterator<Item = Money>, currency: Currency) -> Money {
    Money {
        minor: amounts.map(|amount| amount.minor).sum(),
        currency,
    }
}

fn available_subtitle(figures: &DashboardFigures, language: Language) -> String {
    let mut lines = Vec::new();

    if let Some(carry_forward) = figures.carry_forward {
        lines.push(i18n::format(
            language,
            Key::CarryForwardFrom,
            &[
                Arg::Str(month_text(figures.loaded_month.minus_months(1), language)),
                Arg::Str(format::money(carry_forward, language, false)),
            ],
        ));
    }

    if let Some(other_movements) = figures.other_movements
        && other_movements.minor != 0
    {
        lines.push(i18n::format(
            language,
            Key::OtherMovements,
            &[Arg::Str(format::money(other_movements, language, true))],
        ));
    }

    if figures.has_loans {
        lines.push(i18n::format(
            language,
            Key::LoansSubtitle,
            &[
                Arg::Str(format::money(figures.loan_balance, language, false)),
                Arg::Str(format::money(figures.total_balance, language, false)),
            ],
        ));
    }

    if !figures.other_balances.is_empty() {
        let amounts = figures
            .other_balances
            .values()
            .map(|amount| format::money(*amount, language, false))
            .collect::<Vec<_>>()
            .join(" · ");
        lines.push(i18n::format(language, Key::PlusOtherCurrencies, &[Arg::Str(amounts)]));
    }

    lines.join("\n")
}

fn upcoming_meta(payment: &crate::recurring::RecurringPayment, language: Language) -> String {
    let cadence = match payment.cadence {
        Cadence::Weekly => Key::CadenceWeekly,
        Cadence::Monthly => Key::CadenceMonthly,
        Cadence::Quarterly => Key::CadenceQuarterly,
        Cadence::Yearly => Key::CadenceYearly,
    };
    let mut meta = i18n::text(language, cadence).to_owned();
    if let Some(category) = &payment.category_name {
        meta.push_str(" · ");
        meta.push_str(category);
    }
    if !payment.fixed_amount {
        meta.push_str(" · ");
        meta.push_str(i18n::text(language, Key::AmountVaries));
    }
    meta
}

fn budget_row(budget: &dashboard::BudgetStatus, language: Language, month_progress: f64) -> BudgetRowView {
    let over = budget.is_over();
    let ratio = budget.ratio();
    let rest = if over {
        Money {
            minor: budget.spent.minor - budget.limit.minor,
            currency: budget.limit.currency,
        }
    } else {
        budget.remaining()
    };
    let rest_text = if over {
        i18n::format(
            language,
            Key::OverBudget,
            &[Arg::Str(format::money(rest, language, false))],
        )
    } else {
        i18n::format(
            language,
            Key::LeftBudget,
            &[Arg::Str(format::money(rest, language, false))],
        )
    };

    BudgetRowView {
        category_id: budget.category_id,
        name: budget.category_name.clone(),
        color: budget.color.clone(),
        limit: budget.limit,
        spent: budget.spent,
        ratio: ratio.clamp(0.0, 1.0) as f32,
        is_over: over,
        warning: !over && ratio > month_progress + 0.1,
        amount_text: i18n::format(
            language,
            Key::SpentOfLimit,
            &[
                Arg::Str(format::money(budget.spent, language, false)),
                Arg::Str(format::money(budget.limit, language, false)),
            ],
        ),
        rest_text,
    }
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::model::{AccountId, AccountType, NewAccount, NewCategory, NewTransaction};
    use crate::money::Currency;
    use crate::repo::{accounts, categories, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn money(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn account(db: &Database, iban: &str, balance: i64, currency: Currency, account_type: AccountType) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: None,
                iban: Some(iban.to_owned()),
                account_number: None,
                currency,
                account_type,
                balance: Money {
                    minor: balance,
                    currency,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn category(
        db: &Database,
        name: &str,
        color: &str,
        tax_relevant: bool,
        is_transfer: bool,
        budget: Option<i64>,
    ) -> CategoryId {
        categories::create(
            db,
            NewCategory {
                name: name.to_owned(),
                color: color.to_owned(),
                budget_limit: budget.map(money),
                is_tax_relevant: tax_relevant,
                is_transfer,
            },
        )
        .unwrap()
    }

    fn txn(
        db: &Database,
        account: AccountId,
        currency: Currency,
        day: Date,
        minor: i64,
        partner: &str,
        category: Option<CategoryId>,
    ) -> TransactionId {
        transactions::insert_planned(
            db,
            account,
            vec![NewTransaction {
                booking_date: day,
                amount: Money { minor, currency },
                balance_after: None,
                partner_name: Some(partner.to_owned()),
                purpose: None,
                category_id: category,
                import_hash: format!("{}-{}-{}-{}", account.0, day, minor, partner),
                created_at: time::OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        transactions::list_for_account(db, account, None, None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.import_hash == format!("{}-{}-{}-{}", account.0, day, minor, partner))
            .unwrap()
            .id
    }

    fn seed_months(db: &Database) -> (AccountId, CategoryId, CategoryId, CategoryId) {
        let account = account(
            db,
            "DE89370400440532013000",
            100_000,
            Currency::EUR,
            AccountType::Checking,
        );
        let food = category(db, "Food", "#FF5722", false, false, Some(10_000));
        let donations = category(db, "Donations", "#3BC7B0", true, false, None);
        let transfers = category(db, "Own transfers", "#4CAF50", false, true, None);

        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 03 - 05),
            -10_000,
            "March donation",
            Some(donations),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 02),
            -12_000,
            "August food",
            Some(food),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 03),
            200_000,
            "Employer",
            None,
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 04),
            -20_000,
            "Own transfer",
            Some(transfers),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 05),
            -1500,
            "August uncategorized",
            None,
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 09 - 04),
            -6000,
            "September food",
            Some(food),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 09 - 06),
            -5000,
            "September donation",
            Some(donations),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 10 - 02),
            -4000,
            "October food",
            Some(food),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 10 - 03),
            150_000,
            "Employer",
            None,
        );

        (account, food, donations, transfers)
    }

    #[test]
    fn nothing_loaded_shows_no_figures() {
        let (_temp, db) = database();
        let state = DashboardState::new(Language::En, date!(2026 - 10 - 15));

        assert_eq!(state.selected_month, YearMonth::new(2026, 10));
        assert_eq!(state.loaded_month, None);
        assert!(!state.is_loading);
        assert!(state.view.is_none());
        assert!(!state.has_figures());

        let mut state = state;
        state.load(&db, date!(2026 - 10 - 15)).unwrap();
        assert_eq!(state.loaded_month, Some(YearMonth::new(2026, 10)));
        assert!(state.view.is_some());
        assert!(
            !state.has_figures(),
            "an account-less profile shows the getting-started card instead"
        );
    }

    #[test]
    fn first_run_is_detected_after_a_load_without_accounts() {
        let (_temp, db) = database();
        let mut state = DashboardState::new(Language::En, date!(2026 - 10 - 15));
        assert!(!state.is_first_run(), "before the first load the page is busy");

        state.load(&db, date!(2026 - 10 - 15)).unwrap();

        assert!(state.is_first_run());
        assert!(!state.has_figures());
        assert!(!state.has_accounts);
    }

    #[test]
    fn month_navigation_and_clamping_follow_findesk() {
        let (_temp, db) = database();
        seed_months(&db);
        let today = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, today);

        state.load(&db, today).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 10));
        assert_eq!(state.earliest_month, YearMonth::new(2026, 3));
        let view = state.view.as_ref().unwrap();
        assert!(view.previous_enabled);
        assert!(!view.next_enabled);
        assert_eq!(view.month_label, "October 2026");

        state.previous_month(&db, today).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 9));
        assert_eq!(state.loaded_month, Some(YearMonth::new(2026, 9)));
        assert!(state.view.as_ref().unwrap().next_enabled);

        for _ in 0..6 {
            state.previous_month(&db, today).unwrap();
        }
        assert_eq!(state.selected_month, YearMonth::new(2026, 3));
        state.previous_month(&db, today).unwrap();
        assert_eq!(
            state.selected_month,
            YearMonth::new(2026, 3),
            "the oldest booked month is the floor"
        );

        state.select_month(&db, today, YearMonth::new(2026, 12)).unwrap();
        assert_eq!(
            state.selected_month,
            YearMonth::new(2026, 10),
            "a future month clamps to the current month"
        );

        state.select_month(&db, today, YearMonth::new(2025, 1)).unwrap();
        assert_eq!(
            state.selected_month,
            YearMonth::new(2026, 3),
            "a month before the first booking clamps to it"
        );

        state.current_month(&db, today).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 10));
        assert!(state.view.as_ref().unwrap().is_current_month);
    }

    #[test]
    fn opening_again_after_a_month_change_shows_todays_month() {
        let (_temp, db) = database();
        seed_months(&db);
        let october = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, october);
        state.load(&db, october).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 10));

        let november = date!(2026 - 11 - 08);
        state.open(november);
        state.load(&db, november).unwrap();

        let view = state.view.as_ref().unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 11));
        assert_eq!(state.current_month, YearMonth::new(2026, 11));
        assert_eq!(view.selected_month, YearMonth::new(2026, 11));
        assert_eq!(view.current_month, YearMonth::new(2026, 11));
        assert_eq!(view.month_label, "November 2026");
        assert!(view.is_current_month);
        assert!(!view.next_enabled, "next is disabled in today's month");
    }

    #[test]
    fn a_reload_while_open_keeps_the_selected_past_month() {
        let (_temp, db) = database();
        seed_months(&db);
        let october = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, october);
        state.load(&db, october).unwrap();
        state.select_month(&db, october, YearMonth::new(2026, 9)).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 9));

        let november = date!(2026 - 11 - 08);
        state.load(&db, november).unwrap();

        assert_eq!(state.selected_month, YearMonth::new(2026, 9));
        assert_eq!(state.loaded_month, Some(YearMonth::new(2026, 9)));
        assert_eq!(state.current_month, YearMonth::new(2026, 11));
        let view = state.view.as_ref().unwrap();
        assert_eq!(view.month_label, "September 2026");
        assert!(view.next_enabled, "a past month can move towards today");
    }

    #[test]
    fn the_month_list_runs_from_today_back_to_the_earliest_booking() {
        let earliest = YearMonth::new(2026, 8);
        let current = YearMonth::new(2026, 11);
        assert_eq!(
            crate::dashboard::month_options(earliest, current),
            vec![
                YearMonth::new(2026, 11),
                YearMonth::new(2026, 10),
                YearMonth::new(2026, 9),
                YearMonth::new(2026, 8),
            ],
            "newest first"
        );
        assert_eq!(
            crate::dashboard::month_options(current, earliest),
            vec![YearMonth::new(2026, 8)],
            "crossed bounds clamp to the current month"
        );
    }

    #[test]
    fn selecting_before_the_earliest_or_after_the_current_month_is_clamped() {
        let (_temp, db) = database();
        seed_months(&db);
        let today = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, today);
        state.load(&db, today).unwrap();

        state.select_month(&db, today, YearMonth::new(2026, 1)).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 3));

        state.select_month(&db, today, YearMonth::new(2027, 1)).unwrap();
        assert_eq!(state.selected_month, YearMonth::new(2026, 10));
    }

    #[test]
    fn begin_load_refreshes_a_stale_current_month_before_clamping() {
        let (_temp, db) = database();
        seed_months(&db);
        let october = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, october);
        state.load(&db, october).unwrap();
        assert_eq!(state.current_month, YearMonth::new(2026, 10));

        let november = date!(2026 - 11 - 08);
        state.begin_load(YearMonth::new(2026, 11), november);

        assert_eq!(state.current_month, YearMonth::new(2026, 11));
        assert_eq!(
            state.selected_month,
            YearMonth::new(2026, 11),
            "the old October current month must not clamp November"
        );
        assert_eq!(state.months.first(), Some(&YearMonth::new(2026, 11)));
    }

    #[test]
    fn unlinked_receipts_count_as_pending_tasks() {
        let (_temp, db) = database();
        seed_months(&db);
        let today = date!(2026 - 10 - 15);
        for name in ["a.pdf", "b.pdf"] {
            crate::repo::documents::create(
                &db,
                crate::model::NewDocument {
                    file_name: name.to_owned(),
                    stored_path: format!("documents/{name}"),
                    mime_type: "application/pdf".to_owned(),
                    transaction_id: None,
                },
            )
            .unwrap();
        }
        let mut state = DashboardState::new(Language::En, today);
        state.load(&db, today).unwrap();

        let figures = dashboard::load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();
        let view = state.view.as_ref().unwrap();
        assert_eq!(view.pending_value, (figures.uncategorized_count + 2).to_string());
        assert!(
            view.pending_subtitle.contains("2 unlinked docs"),
            "{}",
            view.pending_subtitle
        );
    }

    #[test]
    fn the_seeded_view_matches_the_dashboard_figures() {
        let (_temp, db) = database();
        let (_, food, _, _) = seed_months(&db);
        let today = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, today);
        state.load(&db, today).unwrap();

        let figures = dashboard::load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();
        let view = state.view.as_ref().unwrap();

        assert_eq!(view.available_balance, figures.available_balance);
        assert_eq!(view.total_balance, figures.total_balance);
        assert_eq!(view.loan_balance, figures.loan_balance);
        assert_eq!(view.monthly_income, figures.monthly_income);
        assert_eq!(view.monthly_expenses, figures.monthly_expenses);
        assert_eq!(view.uncategorized_count, figures.uncategorized_count);
        assert_eq!(view.transfers_this_month, figures.transfers_this_month);
        assert_eq!(view.expenses_by_category.len(), figures.expenses_by_category.len());
        assert_eq!(view.month_bars.len(), 6);
        assert_eq!(view.recent_transactions.len(), figures.recent_transactions.len());

        let food_budget = view.budgets.iter().find(|budget| budget.category_id == food).unwrap();
        assert_eq!(food_budget.spent, money(4000), "October's food booking");
        assert_eq!(food_budget.limit, money(10_000));
        assert!(!food_budget.is_over);
        assert_eq!(food_budget.rest_text, "€60.00 left");

        assert_eq!(view.income_title, "Income this month");
        assert_eq!(view.income_value, "€1,500.00");
        assert_eq!(view.expenses_value, "€40.00");
        assert_eq!(view.pending_value, "1", "October's income has no category");
        assert_eq!(view.pending_subtitle, "1 uncategorized · 0 unlinked docs");
        assert_eq!(view.expense_rows[0].label, "Food");
        assert_eq!(view.expense_rows[0].percent_text, "100%");
        assert_eq!(view.tax_total, money(15_000));
        assert_eq!(view.tax_deductions_by_category[0].label, "Donations");
        assert!(!view.available_subtitle.is_empty());
        assert_eq!(view.chart_axis_zero, "€0.00");
        assert!(!view.view_all_label.is_empty());
    }

    #[test]
    fn two_currencies_a_transfer_a_budget_and_a_loan_keep_their_figures() {
        let (_temp, db) = database();
        let eur = account(
            &db,
            "DE89370400440532013000",
            100_000,
            Currency::EUR,
            AccountType::Checking,
        );
        let usd_currency = Currency::new("USD").unwrap();
        let usd = account(
            &db,
            "DE44500105175407324931",
            50_000,
            usd_currency,
            AccountType::Checking,
        );
        let loan = account(
            &db,
            "DE44500105175407324930",
            -500_000,
            Currency::EUR,
            AccountType::Loan,
        );
        let food = category(&db, "Food", "#FF5722", false, false, Some(10_000));
        let transfers = category(&db, "Own transfers", "#4CAF50", false, true, None);
        txn(
            &db,
            eur,
            Currency::EUR,
            date!(2026 - 10 - 02),
            -4000,
            "Bakery",
            Some(food),
        );
        txn(
            &db,
            eur,
            Currency::EUR,
            date!(2026 - 10 - 03),
            150_000,
            "Employer",
            None,
        );
        txn(
            &db,
            eur,
            Currency::EUR,
            date!(2026 - 10 - 04),
            -20_000,
            "Own transfer",
            Some(transfers),
        );
        txn(&db, usd, usd_currency, date!(2026 - 10 - 05), -3000, "US shop", None);
        txn(&db, loan, Currency::EUR, date!(2026 - 10 - 06), -1000, "Interest", None);

        let today = date!(2026 - 10 - 15);
        let mut state = DashboardState::new(Language::En, today);
        state.load(&db, today).unwrap();
        let figures = dashboard::load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();
        let view = state.view.as_ref().unwrap();

        assert_eq!(view.currency, Currency::EUR);
        assert_eq!(view.available_balance, figures.available_balance);
        assert_eq!(view.total_balance, figures.total_balance);
        assert_eq!(view.loan_balance, money(-500_000));
        assert!(view.has_loans);
        assert_eq!(
            view.other_balances,
            vec![(
                usd_currency,
                Money {
                    minor: 50_000,
                    currency: usd_currency
                }
            )]
        );
        assert!(view.available_subtitle.contains("Loans"));
        assert!(view.available_subtitle.contains("other currencies"));
        assert_eq!(view.transfers_this_month, money(20_000));
        assert_eq!(view.budgets[0].spent, money(4000));
        assert_eq!(view.monthly_income, money(150_000));
        assert_eq!(view.monthly_expenses, money(5000));
        assert_eq!(view.uncategorized_count, 3);
    }

    #[test]
    fn navigation_args_carry_the_filter_findesk_uses() {
        let current = YearMonth::new(2026, 10);
        let past = YearMonth::new(2026, 8);
        let category = CategoryId(7);

        assert_eq!(NavigationArgs::all(), NavigationArgs::default());
        assert_eq!(
            NavigationArgs::category(current, current, category),
            NavigationArgs {
                month: None,
                category_id: Some(category),
                uncategorized_only: false,
                text: None,
            }
        );
        assert_eq!(
            NavigationArgs::category(past, current, category),
            NavigationArgs {
                month: Some(past),
                category_id: Some(category),
                uncategorized_only: false,
                text: None,
            }
        );
        assert_eq!(
            NavigationArgs::uncategorized(past, current),
            NavigationArgs {
                month: Some(past),
                category_id: None,
                uncategorized_only: true,
                text: None,
            }
        );
        assert_eq!(
            NavigationArgs::partner("Bakery"),
            NavigationArgs {
                month: None,
                category_id: None,
                uncategorized_only: false,
                text: Some("Bakery".to_owned()),
            }
        );
    }

    #[test]
    fn upcoming_recurring_payments_become_rows() {
        let (_temp, db) = database();
        let account = account(
            &db,
            "DE89370400440532013000",
            100_000,
            Currency::EUR,
            AccountType::Checking,
        );
        for month in 5..=7 {
            let day = Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 10).unwrap();
            txn(&db, account, Currency::EUR, day, -999, "Spotify", None);
        }

        let today = date!(2026 - 07 - 15);
        let mut state = DashboardState::new(Language::En, today);
        state.load(&db, today).unwrap();
        let view = state.view.as_ref().unwrap();

        assert_eq!(view.upcoming.len(), 1);
        let payment = &view.upcoming[0];
        assert_eq!(payment.name, "Spotify");
        assert_eq!(payment.cadence, Cadence::Monthly);
        assert!(payment.meta.contains("Monthly"));
        assert_eq!(payment.amount_text, "-€9.99");
        assert!(payment.date_text.ends_with(".2026"));
    }
}
