//! The dashboard's month figures, ported from FinDesk's `DashboardViewModel`.
//!
//! The computation is pure: it takes repository data and returns numbers. There is no UI state and no
//! coroutine; callers choose the month and decide when to load.

use std::collections::BTreeMap;

use time::{Date, Duration, Month};

use crate::db::Database;
use crate::model::{Account, AccountId, AccountType, Category, CategoryId, TransactionFilter, TransactionItem};
use crate::money::{Currencies, Currency, Money};
use crate::recurring::{RecurringDetector, RecurringPayment};
use crate::repo::{self, Result};

/// Neutral grey for bookings without a category.
pub const UNCATEGORIZED_COLOR: &str = "#757575";

/// The uncategorized slice's label.
pub const UNCATEGORIZED_LABEL: &str = "Uncategorized";

/// A calendar month without a day.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct YearMonth {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
}

impl YearMonth {
    /// Builds a month.
    pub fn new(year: i32, month: u8) -> Self {
        assert!((1..=12).contains(&month), "month out of range");
        Self { year, month }
    }

    /// The month containing `date`.
    pub fn from_date(date: Date) -> Self {
        Self {
            year: date.year(),
            month: u8::from(date.month()),
        }
    }

    /// The month's length in days.
    pub fn length(self) -> u8 {
        Month::try_from(self.month).expect("month in range").length(self.year)
    }

    /// The first day of the month.
    pub fn start(self) -> Date {
        Date::from_calendar_date(self.year, Month::try_from(self.month).expect("month in range"), 1)
            .expect("valid date")
    }

    /// The last day of the month.
    pub fn end(self) -> Date {
        self.start()
            .checked_add(Duration::days(i64::from(self.length()) - 1))
            .expect("date overflow")
    }

    /// The month `months` before this one.
    pub fn minus_months(self, months: i32) -> Self {
        let total = self.year * 12 + i32::from(self.month) - 1 - months;
        Self {
            year: total.div_euclid(12),
            month: u8::try_from(total.rem_euclid(12) + 1).expect("month in range"),
        }
    }

    /// The month `months` after this one.
    pub fn plus_months(self, months: i32) -> Self {
        self.minus_months(-months)
    }
}

/// One slice of the spending split.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ChartSlice {
    /// The category's name, or [`UNCATEGORIZED_LABEL`].
    pub label: String,
    /// The slice's amount.
    pub amount: Money,
    /// The category's color.
    pub color: Option<String>,
    /// The category's id, or `None` for uncategorized.
    pub category_id: Option<CategoryId>,
}

/// One month in the six-month chart.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MonthBar {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
    /// Money in, transfers excluded.
    pub income: Money,
    /// Money out, transfers excluded, as a positive amount.
    pub expenses: Money,
    /// Whether this is still the running month.
    pub partial: bool,
}

/// A category's budget for the selected month.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetStatus {
    /// The category id.
    pub category_id: CategoryId,
    /// The category name.
    pub category_name: String,
    /// The category color.
    pub color: String,
    /// The monthly limit.
    pub limit: Money,
    /// What was spent this month (positive).
    pub spent: Money,
}

impl BudgetStatus {
    /// Spent divided by the limit, for a progress bar.
    pub fn ratio(&self) -> f64 {
        if self.limit.minor > 0 {
            self.spent.minor as f64 / self.limit.minor as f64
        } else {
            0.0
        }
    }

    /// The limit minus what was spent.
    pub fn remaining(&self) -> Money {
        Money {
            minor: self.limit.minor - self.spent.minor,
            currency: self.limit.currency,
        }
    }

    /// Whether spending exceeds the limit.
    pub fn is_over(&self) -> bool {
        self.spent.minor > self.limit.minor
    }
}

/// The figures one dashboard load publishes.
#[derive(Debug, Clone, PartialEq)]
pub struct DashboardFigures {
    /// Currency of every total and chart below.
    pub currency: Currency,
    /// The sum of every account's balance in the primary currency.
    pub total_balance: Money,
    /// Money on checking, savings and credit-card accounts.
    pub available_balance: Money,
    /// The sum of loan accounts' balances in the primary currency.
    pub loan_balance: Money,
    /// Money available at the end of the previous month, when the history reaches that far.
    pub carry_forward: Option<Money>,
    /// What the available accounts moved outside income and expenses.
    pub other_movements: Option<Money>,
    /// The day the available balance is stated for; `None` in the current month.
    pub balance_date: Option<Date>,
    /// Whether the accounts include loans.
    pub has_loans: bool,
    /// Balances of accounts in other currencies, never added to the totals.
    pub other_balances: BTreeMap<Currency, Money>,
    /// Income in the selected month, transfers excluded.
    pub monthly_income: Money,
    /// Spending in the selected month, transfers excluded, as a positive amount.
    pub monthly_expenses: Money,
    /// Uncategorized bookings in the selected month, in every currency.
    pub uncategorized_count: usize,
    /// The newest bookings, independent of the selected month.
    pub recent_transactions: Vec<TransactionItem>,
    /// Spending by category in the selected month.
    pub expenses_by_category: Vec<ChartSlice>,
    /// Tax-relevant spending from January 1 through the selected month.
    pub tax_deductions_by_category: Vec<ChartSlice>,
    /// The six months ending with the selected month.
    pub last_months: Vec<MonthBar>,
    /// Budgets for the selected month, most-used first.
    pub budgets: Vec<BudgetStatus>,
    /// Recurring bookings expected in the next 30 days.
    pub upcoming: Vec<RecurringPayment>,
    /// How far through the month we are, 0 to 1.
    pub month_progress: f64,
    /// Money moved out between the user's own accounts this month.
    pub transfers_this_month: Money,
    /// The month the figures belong to.
    pub selected_month: YearMonth,
    /// The month whose figures these are; equal to [`Self::selected_month`] after a load.
    pub loaded_month: YearMonth,
    /// The month of the oldest booking in the primary currency.
    pub earliest_month: YearMonth,
    /// Today's calendar month at load time.
    pub current_month: YearMonth,
    /// Whether there are any accounts.
    pub has_accounts: bool,
}

/// Already-loaded repository data for one dashboard computation.
pub struct DashboardInput<'a> {
    /// The current date.
    pub today: Date,
    /// The requested month; computation clamps it to the data and today.
    pub requested_month: YearMonth,
    /// The primary currency.
    pub primary_currency: Currency,
    /// Every account.
    pub accounts: &'a [Account],
    /// Every category.
    pub categories: &'a [Category],
    /// The oldest booking date in the primary currency.
    pub oldest_date: Option<Date>,
    /// Every booking from the selected month's first day on, in every currency.
    pub month_bookings: &'a [TransactionItem],
    /// The bookings from the chart window through the selected month, in the primary currency.
    pub window: &'a [TransactionItem],
    /// The newest bookings, independent of the selected month.
    pub recent_transactions: &'a [TransactionItem],
    /// Each account's oldest booking date.
    pub oldest_by_account: &'a BTreeMap<AccountId, Date>,
    /// Count of uncategorized bookings in the selected month, in every currency.
    pub uncategorized_count: usize,
    /// Recurring payments expected soon.
    pub upcoming: Vec<RecurringPayment>,
}

/// The dashboard's pure computation.
pub struct Dashboard;

impl Dashboard {
    /// Computes the month figures from repository data.
    pub fn compute(input: DashboardInput<'_>) -> DashboardFigures {
        let now_month = YearMonth::from_date(input.today);
        let earliest_month = input
            .oldest_date
            .map(YearMonth::from_date)
            .map(|month| month.min(now_month))
            .unwrap_or(now_month);
        let month = input.requested_month.min(now_month).max(earliest_month);
        let month_start = month.start();
        let month_end = month.end();
        let six_months_start = month.minus_months(5).start();
        let year_start = Date::from_calendar_date(month.year, Month::January, 1).expect("valid date");
        let current_month = month == now_month;
        let currency = input.primary_currency;

        let mut month_bookings: BTreeMap<AccountId, Vec<&TransactionItem>> = BTreeMap::new();
        for transaction in input.month_bookings {
            month_bookings
                .entry(transaction.account_id)
                .or_default()
                .push(transaction);
        }

        let balance_at = |account: &Account| -> i64 {
            if account.account_type == AccountType::Depot || current_month {
                account.balance.minor
            } else {
                let after = month_bookings
                    .get(&account.id)
                    .map(|bookings| {
                        bookings
                            .iter()
                            .filter(|transaction| transaction.date > month_end)
                            .map(|transaction| transaction.amount.minor)
                            .sum::<i64>()
                    })
                    .unwrap_or(0);
                account.balance.minor - after
            }
        };
        let movement = |account: &Account| -> i64 {
            month_bookings
                .get(&account.id)
                .map(|bookings| {
                    bookings
                        .iter()
                        .filter(|transaction| current_month || transaction.date <= month_end)
                        .map(|transaction| transaction.amount.minor)
                        .sum::<i64>()
                })
                .unwrap_or(0)
        };

        let balances = Currencies::totals(input.accounts, currency, |account| account.currency, balance_at);
        let total_balance = balances
            .iter()
            .find(|(entry_currency, _)| *entry_currency == currency)
            .map(|(_, total)| *total)
            .unwrap_or(0);
        let other_balances = balances
            .iter()
            .filter(|(entry_currency, _)| *entry_currency != currency)
            .map(|(entry_currency, total)| {
                (
                    *entry_currency,
                    Money {
                        minor: *total,
                        currency: *entry_currency,
                    },
                )
            })
            .collect();

        let available_accounts: Vec<&Account> = input
            .accounts
            .iter()
            .filter(|account| {
                account.account_type != AccountType::Loan
                    && account.account_type != AccountType::Depot
                    && account.currency == currency
            })
            .collect();
        let loan_accounts: Vec<&Account> = input
            .accounts
            .iter()
            .filter(|account| account.account_type == AccountType::Loan)
            .collect();

        let available_total = Currencies::totals(
            &available_accounts,
            currency,
            |account| account.currency,
            |account| balance_at(account),
        )
        .iter()
        .find(|(entry_currency, _)| *entry_currency == currency)
        .map(|(_, total)| *total)
        .unwrap_or(0);
        let loan_total = Currencies::totals(
            &loan_accounts,
            currency,
            |account| account.currency,
            |account| balance_at(account),
        )
        .iter()
        .find(|(entry_currency, _)| *entry_currency == currency)
        .map(|(_, total)| *total)
        .unwrap_or(0);

        let window: Vec<&TransactionItem> = input.window.iter().collect();
        let this_month: Vec<&TransactionItem> = window
            .iter()
            .copied()
            .filter(|transaction| transaction.date >= month_start && transaction.date <= month_end)
            .collect();
        let this_month_expenses: Vec<&TransactionItem> = this_month
            .iter()
            .copied()
            .filter(|transaction| transaction.counts_as_spending())
            .collect();
        let monthly_income: i64 = this_month
            .iter()
            .filter(|transaction| transaction.counts_as_income())
            .map(|transaction| transaction.amount.minor)
            .sum();
        let monthly_expenses: i64 = this_month_expenses
            .iter()
            .map(|transaction| transaction.amount.minor.abs())
            .sum();

        let carry_forward_known = available_accounts.iter().all(|account| {
            input
                .oldest_by_account
                .get(&account.id)
                .is_none_or(|oldest| *oldest < month_start)
        });
        let carry_forward = carry_forward_known.then(|| {
            available_accounts
                .iter()
                .map(|account| balance_at(account) - movement(account))
                .sum::<i64>()
        });
        let available_movement: i64 = available_accounts.iter().map(|account| movement(account)).sum();
        let other_movements = carry_forward.map(|_| available_movement - (monthly_income - monthly_expenses));

        let last_months = (0..6)
            .map(|index| {
                let bar_month = YearMonth::from_date(six_months_start).plus_months(index);
                let transactions: Vec<&&TransactionItem> = window
                    .iter()
                    .filter(|transaction| {
                        transaction.date.year() == bar_month.year
                            && u8::from(transaction.date.month()) == bar_month.month
                    })
                    .collect();
                let income: i64 = transactions
                    .iter()
                    .filter(|transaction| transaction.counts_as_income())
                    .map(|transaction| transaction.amount.minor)
                    .sum();
                let expenses: i64 = transactions
                    .iter()
                    .filter(|transaction| transaction.counts_as_spending())
                    .map(|transaction| transaction.amount.minor.abs())
                    .sum();
                MonthBar {
                    year: bar_month.year,
                    month: bar_month.month,
                    income: money(income, currency),
                    expenses: money(expenses, currency),
                    partial: bar_month == now_month,
                }
            })
            .collect();

        let expenses_by_category = slices(&this_month_expenses, currency);
        let tax_deductions_by_category = slices(
            &window
                .iter()
                .copied()
                .filter(|transaction| {
                    transaction.is_expense() && transaction.is_tax_relevant && transaction.date >= year_start
                })
                .collect::<Vec<_>>(),
            currency,
        );

        let mut budgets: Vec<BudgetStatus> = input
            .categories
            .iter()
            .filter_map(|category| {
                let limit = category.budget_limit.filter(|limit| limit.minor > 0)?;
                let spent: i64 = this_month_expenses
                    .iter()
                    .filter(|transaction| transaction.category_id == Some(category.id))
                    .map(|transaction| transaction.amount.minor.abs())
                    .sum();
                Some(BudgetStatus {
                    category_id: category.id,
                    category_name: category.name.clone(),
                    color: category.color.clone(),
                    limit,
                    spent: money(spent, currency),
                })
            })
            .collect();
        budgets.sort_by(|left, right| right.ratio().total_cmp(&left.ratio()));

        let transfers_this_month: i64 = this_month
            .iter()
            .filter(|transaction| transaction.is_transfer && transaction.amount.minor < 0)
            .map(|transaction| transaction.amount.minor.abs())
            .sum();

        let month_progress = if current_month {
            f64::from(input.today.day()) / f64::from(month.length())
        } else {
            1.0
        };

        DashboardFigures {
            currency,
            total_balance: money(total_balance, currency),
            available_balance: money(available_total, currency),
            loan_balance: money(loan_total, currency),
            carry_forward: carry_forward.map(|minor| money(minor, currency)),
            other_movements: other_movements.map(|minor| money(minor, currency)),
            balance_date: (!current_month).then_some(month_end),
            has_loans: input
                .accounts
                .iter()
                .any(|account| account.account_type == AccountType::Loan),
            other_balances,
            monthly_income: money(monthly_income, currency),
            monthly_expenses: money(monthly_expenses, currency),
            uncategorized_count: input.uncategorized_count,
            recent_transactions: input.recent_transactions.to_vec(),
            expenses_by_category,
            tax_deductions_by_category,
            last_months,
            budgets,
            upcoming: input.upcoming,
            month_progress,
            transfers_this_month: money(transfers_this_month, currency),
            selected_month: month,
            loaded_month: month,
            earliest_month,
            current_month: now_month,
            has_accounts: !input.accounts.is_empty(),
        }
    }
}

fn money(minor: i64, currency: Currency) -> Money {
    Money { minor, currency }
}

fn slices(expenses: &[&TransactionItem], currency: Currency) -> Vec<ChartSlice> {
    let mut slices: Vec<ChartSlice> = Vec::new();
    for transaction in expenses {
        let label = transaction
            .category_name
            .clone()
            .unwrap_or_else(|| UNCATEGORIZED_LABEL.to_owned());
        let color = transaction.category_color.clone().or_else(|| {
            transaction
                .category_id
                .is_none()
                .then(|| UNCATEGORIZED_COLOR.to_owned())
        });

        if let Some(slice) = slices.iter_mut().find(|slice| slice.label == label) {
            slice.amount.minor += transaction.amount.minor.abs();
        } else {
            slices.push(ChartSlice {
                label,
                amount: money(transaction.amount.minor.abs(), currency),
                color,
                category_id: transaction.category_id,
            });
        }
    }
    slices.sort_by_key(|slice| std::cmp::Reverse(slice.amount.minor));
    slices
}

/// Loads the repository data and computes one dashboard state.
///
/// `requested_month` is clamped to the data's oldest month and to today, exactly as the view model does.
pub fn load_dashboard(db: &Database, today: Date, requested_month: YearMonth) -> Result<DashboardFigures> {
    let primary = Currencies::primary(db)?;
    let oldest_date = repo::transactions::oldest_date(db, Some(primary))?;
    let now_month = YearMonth::from_date(today);
    let earliest_month = oldest_date
        .map(YearMonth::from_date)
        .map(|month| month.min(now_month))
        .unwrap_or(now_month);
    let month = requested_month.min(now_month).max(earliest_month);

    let accounts = repo::accounts::list(db)?;
    let categories = repo::categories::list(db)?;
    let month_bookings = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(month.start()),
            ..TransactionFilter::default()
        },
        None,
    )?;
    let year_start = Date::from_calendar_date(month.year, Month::January, 1).expect("valid date");
    let window_start = month.minus_months(5).start().min(year_start);
    let window = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(window_start),
            end_date: Some(month.end()),
            currency: Some(primary),
            ..TransactionFilter::default()
        },
        None,
    )?;
    let recent_transactions = repo::transactions::find_items(db, &TransactionFilter::default(), Some(8))?;
    let oldest_by_account = repo::transactions::oldest_date_by_account(db)?;
    let uncategorized_count = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(month.start()),
            end_date: Some(month.end()),
            uncategorized_only: true,
            ..TransactionFilter::default()
        },
        None,
    )?
    .len();

    let recurring_window = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(today.checked_sub(Duration::days(400)).expect("date underflow")),
            end_date: Some(today),
            ..TransactionFilter::default()
        },
        None,
    )?;
    let recurring = RecurringDetector::detect(&recurring_window, today);
    let upcoming = RecurringDetector::upcoming(&recurring, today, 30);

    Ok(Dashboard::compute(DashboardInput {
        today,
        requested_month: month,
        primary_currency: primary,
        accounts: &accounts,
        categories: &categories,
        oldest_date,
        month_bookings: &month_bookings,
        window: &window,
        recent_transactions: &recent_transactions,
        oldest_by_account: &oldest_by_account,
        uncategorized_count,
        upcoming,
    }))
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::db::Database;
    use crate::model::{NewAccount, NewCategory, NewTransaction, TransactionId};
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

    fn eur_account(db: &Database, iban: &str, balance: i64) -> AccountId {
        account(db, iban, balance, Currency::EUR, AccountType::Checking)
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
        account_id: AccountId,
        currency: Currency,
        day: Date,
        minor: i64,
        partner: &str,
        category_id: Option<CategoryId>,
    ) -> TransactionId {
        transactions::insert_planned(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount: Money { minor, currency },
                balance_after: None,
                partner_name: Some(partner.to_owned()),
                purpose: None,
                category_id,
                import_hash: format!("{}-{}-{}-{}", account_id.0, day, minor, partner),
                created_at: time::OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        transactions::list_for_account(db, account_id, None, None)
            .unwrap()
            .into_iter()
            .find(|transaction| transaction.import_hash == format!("{}-{}-{}-{}", account_id.0, day, minor, partner))
            .unwrap()
            .id
    }

    fn month_fixtures(db: &Database) -> (AccountId, CategoryId, CategoryId, CategoryId) {
        let account = eur_account(db, "DE89370400440532013000", 100_000);
        let food = category(db, "Food", "#FF5722", false, false, None);
        let tax = category(db, "Donations", "#3BC7B0", true, false, None);
        let transfers = category(db, "Own transfers", "#4CAF50", false, true, None);
        categories::update(
            db,
            food,
            NewCategory {
                name: "Food".to_owned(),
                color: "#FF5722".to_owned(),
                budget_limit: Some(money(10_000)),
                is_tax_relevant: false,
                is_transfer: false,
            },
        )
        .unwrap();
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 03 - 05),
            -10_000,
            "MaerzJetzt",
            Some(tax),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 02),
            -12_000,
            "August Nazar",
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
            "Erika Musterfrau",
            Some(transfers),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 05),
            -1500,
            "Uncategorized August",
            None,
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 09 - 04),
            -6000,
            "September Nazar",
            Some(food),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 09 - 06),
            -5000,
            "September Spenden",
            Some(tax),
        );
        txn(
            db,
            account,
            Currency::EUR,
            date!(2026 - 10 - 02),
            -4000,
            "Oktober Nazar",
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
        (account, food, tax, transfers)
    }

    #[test]
    fn the_dashboard_opens_on_the_current_month() {
        let (_temp, db) = database();
        month_fixtures(&db);
        let today = date!(2026 - 10 - 15);
        let figures = load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.selected_month, YearMonth::new(2026, 10));
        assert_eq!(figures.loaded_month, figures.selected_month);
        assert_eq!(
            figures.earliest_month,
            YearMonth::new(2026, 3),
            "the month of the oldest booking"
        );
        assert!(figures.balance_date.is_none());
    }

    #[test]
    fn selecting_a_past_month_moves_every_month_figure_and_leaves_the_now_ones() {
        let (_temp, db) = database();
        let (_, food, _, _) = month_fixtures(&db);
        let today = date!(2026 - 10 - 15);
        let before = load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();

        let figures = load_dashboard(&db, today, YearMonth::new(2026, 8)).unwrap();

        assert_eq!(figures.selected_month, YearMonth::new(2026, 8));
        assert_eq!(figures.monthly_income, money(200_000), "August only");
        assert_eq!(figures.monthly_expenses, money(13_500), "August only");
        assert_eq!(
            figures
                .expenses_by_category
                .iter()
                .map(|slice| slice.label.as_str())
                .collect::<Vec<_>>(),
            vec!["Food", "Uncategorized"],
            "the August split"
        );
        assert_eq!(figures.transfers_this_month, money(20_000), "the August transfer");
        assert_eq!(
            figures.uncategorized_count, 2,
            "the uncategorized expense and salary in August"
        );
        let food_budget = figures
            .budgets
            .iter()
            .find(|budget| budget.category_id == food)
            .unwrap();
        assert_eq!(food_budget.spent, money(12_000), "budget spending is judged for August");
        assert_eq!(figures.month_progress, 1.0, "a past month is over");
        assert_eq!(
            figures.last_months.iter().map(|bar| bar.month).collect::<Vec<_>>(),
            vec![3, 4, 5, 6, 7, 8],
            "the six months ending with August"
        );
        assert_eq!(
            figures
                .last_months
                .iter()
                .find(|bar| bar.month == 8)
                .unwrap()
                .income
                .minor
                .signum(),
            1
        );
        assert_eq!(
            figures
                .tax_deductions_by_category
                .iter()
                .map(|slice| slice.label.as_str())
                .collect::<Vec<_>>(),
            vec!["Donations"],
            "January 1 to August 31"
        );
        assert_eq!(figures.tax_deductions_by_category[0].amount, money(10_000));
        assert_eq!(
            figures.total_balance,
            money(-35_000),
            "August's balance reconstructed from today's"
        );
        assert_eq!(figures.available_balance, money(-35_000));
        assert_eq!(before.loan_balance, figures.loan_balance);
        assert_eq!(
            before
                .recent_transactions
                .iter()
                .map(|item| item.id)
                .collect::<Vec<_>>(),
            figures
                .recent_transactions
                .iter()
                .map(|item| item.id)
                .collect::<Vec<_>>()
        );
    }

    #[test]
    fn the_selection_clamps_to_the_loaded_range() {
        let (_temp, db) = database();
        month_fixtures(&db);
        let today = date!(2026 - 10 - 15);

        assert_eq!(
            load_dashboard(&db, today, YearMonth::new(2026, 12))
                .unwrap()
                .selected_month,
            YearMonth::new(2026, 10),
            "a future month clamps to the current month"
        );
        assert_eq!(
            load_dashboard(&db, today, YearMonth::new(2026, 1))
                .unwrap()
                .selected_month,
            YearMonth::new(2026, 3),
            "before the oldest booking clamps to earliestMonth"
        );
        assert_eq!(
            load_dashboard(&db, today, YearMonth::new(2026, 10))
                .unwrap()
                .selected_month,
            YearMonth::new(2026, 10)
        );
    }

    #[test]
    fn a_reload_keeps_the_selection() {
        let (_temp, db) = database();
        month_fixtures(&db);
        let today = date!(2026 - 10 - 15);
        load_dashboard(&db, today, YearMonth::new(2026, 8)).unwrap();
        let again = load_dashboard(&db, today, YearMonth::new(2026, 8)).unwrap();

        assert_eq!(again.selected_month, YearMonth::new(2026, 8));
        assert_eq!(again.monthly_expenses, money(13_500), "August again");
    }

    #[test]
    fn without_bookings_every_month_is_the_current_month() {
        let (_temp, db) = database();
        eur_account(&db, "DE89370400440532013000", 0);
        let today = date!(2026 - 10 - 15);
        let figures = load_dashboard(&db, today, YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.earliest_month, YearMonth::new(2026, 10));
        assert_eq!(figures.selected_month, YearMonth::new(2026, 10));
    }

    #[test]
    fn a_month_selected_before_the_first_load_is_kept() {
        let (_temp, db) = database();
        let account = eur_account(&db, "DE89370400440532013000", 0);
        txn(&db, account, Currency::EUR, date!(2026 - 05 - 04), -6000, "May", None);
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 02),
            -12_000,
            "August",
            None,
        );

        let august = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 8)).unwrap();
        assert_eq!(august.selected_month, YearMonth::new(2026, 8));
        assert_eq!(august.monthly_expenses, money(12_000));

        let too_early = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2025, 1)).unwrap();
        assert_eq!(too_early.selected_month, YearMonth::new(2026, 5));
    }

    #[test]
    fn only_the_running_month_is_partial() {
        let (_temp, db) = database();
        let account = eur_account(&db, "DE89370400440532013000", 0);
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 08 - 02),
            -12_000,
            "August",
            None,
        );
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 10 - 03),
            -4000,
            "October",
            None,
        );

        let current = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 10)).unwrap();
        assert_eq!(
            current.last_months.iter().map(|bar| bar.month).collect::<Vec<_>>(),
            vec![5, 6, 7, 8, 9, 10]
        );
        assert_eq!(
            current
                .last_months
                .iter()
                .filter(|bar| bar.partial)
                .map(|bar| bar.month)
                .collect::<Vec<_>>(),
            vec![10],
            "exactly October is partial"
        );

        let past = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 8)).unwrap();
        assert_eq!(
            past.last_months.iter().map(|bar| bar.month).collect::<Vec<_>>(),
            vec![3, 4, 5, 6, 7, 8]
        );
        assert!(past.last_months.iter().all(|bar| !bar.partial));
    }

    #[test]
    fn available_balance_excludes_loans_and_depots_and_loan_balance_sums_loans() {
        let (_temp, db) = database();
        eur_account(&db, "DE89370400440532013001", 10_000);
        account(&db, "DE89370400440532013002", 5000, Currency::EUR, AccountType::Savings);
        account(
            &db,
            "DE89370400440532013003",
            -3000,
            Currency::EUR,
            AccountType::CreditCard,
        );
        account(
            &db,
            "DE89370400440532013004",
            -100_000,
            Currency::EUR,
            AccountType::Loan,
        );
        account(&db, "DE89370400440532013005", 50_000, Currency::EUR, AccountType::Depot);

        let figures = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.available_balance, money(12_000));
        assert_eq!(figures.loan_balance, money(-100_000));
        assert_eq!(figures.total_balance, money(-38_000));
        assert!(figures.has_loans);
    }

    #[test]
    fn without_loan_accounts_loan_balance_is_zero_and_has_loans_is_false() {
        let (_temp, db) = database();
        eur_account(&db, "DE89370400440532013001", 25_000);
        account(
            &db,
            "DE89370400440532013002",
            100_000,
            Currency::EUR,
            AccountType::Depot,
        );

        let figures = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.available_balance, money(25_000));
        assert_eq!(figures.loan_balance.minor, 0);
        assert!(!figures.has_loans);
    }

    fn carry_fixtures(db: &Database) -> (AccountId, AccountId, AccountId, CategoryId) {
        let checking = eur_account(db, "DE89370400440532013000", 100_000);
        let loan = account(db, "DE44500105175407324001", -500_000, Currency::EUR, AccountType::Loan);
        let depot = account(db, "DE02120300000000202051", 300_000, Currency::EUR, AccountType::Depot);
        let transfers = category(db, "Own transfers", "#4CAF50", false, true, None);
        txn(
            db,
            checking,
            Currency::EUR,
            date!(2026 - 08 - 01),
            -1000,
            "August",
            None,
        );
        txn(
            db,
            loan,
            Currency::EUR,
            date!(2026 - 08 - 02),
            -10_000,
            "Loan opening",
            None,
        );
        txn(
            db,
            depot,
            Currency::EUR,
            date!(2026 - 08 - 03),
            10_000,
            "Depot opening",
            None,
        );
        txn(
            db,
            checking,
            Currency::EUR,
            date!(2026 - 09 - 04),
            200_000,
            "Employer",
            None,
        );
        txn(
            db,
            checking,
            Currency::EUR,
            date!(2026 - 09 - 10),
            -230_000,
            "Rent",
            None,
        );
        txn(
            db,
            checking,
            Currency::EUR,
            date!(2026 - 10 - 03),
            25_000,
            "Refund",
            None,
        );
        txn(
            db,
            checking,
            Currency::EUR,
            date!(2026 - 10 - 05),
            -5000,
            "Bakery",
            None,
        );
        (checking, loan, depot, transfers)
    }

    fn load(db: &Database, month: YearMonth) -> DashboardFigures {
        load_dashboard(db, date!(2026 - 10 - 20), month).unwrap()
    }

    #[test]
    fn a_past_month_shows_its_own_balance_carried_over_from_the_previous_one() {
        let (_temp, db) = database();
        carry_fixtures(&db);

        let figures = load(&db, YearMonth::new(2026, 9));

        assert_eq!(figures.available_balance, money(80_000));
        assert_eq!(figures.balance_date, Some(date!(2026 - 09 - 30)));
        assert_eq!(figures.carry_forward, Some(money(110_000)));
        assert_eq!(figures.other_movements, Some(money(0)));
    }

    #[test]
    fn carry_forward_income_expenses_and_other_movements_add_up_exactly() {
        let (_temp, db) = database();
        carry_fixtures(&db);

        for month in [YearMonth::new(2026, 9), YearMonth::new(2026, 10)] {
            let figures = load(&db, month);
            let total = figures.carry_forward.unwrap().minor + figures.monthly_income.minor
                - figures.monthly_expenses.minor
                + figures.other_movements.unwrap().minor;
            assert_eq!(
                total, figures.available_balance.minor,
                "the identity holds for {month:?}"
            );
        }
    }

    #[test]
    fn a_loan_repayment_lowers_the_available_balance_but_is_not_an_expense() {
        let (_temp, db) = database();
        let (checking, loan, _, transfers) = carry_fixtures(&db);
        txn(
            &db,
            checking,
            Currency::EUR,
            date!(2026 - 09 - 15),
            -50_000,
            "Loan repayment",
            Some(transfers),
        );
        txn(
            &db,
            loan,
            Currency::EUR,
            date!(2026 - 09 - 15),
            50_000,
            "Loan repayment",
            Some(transfers),
        );

        let figures = load(&db, YearMonth::new(2026, 9));

        assert_eq!(figures.other_movements, Some(money(-50_000)));
        assert_eq!(figures.monthly_income, money(200_000));
        assert_eq!(figures.monthly_expenses, money(230_000));
    }

    #[test]
    fn loan_interest_is_an_expense_and_comes_back_as_an_other_movement() {
        let (_temp, db) = database();
        let (_, loan, _, _) = carry_fixtures(&db);
        txn(
            &db,
            loan,
            Currency::EUR,
            date!(2026 - 09 - 20),
            -4000,
            "Loan interest",
            None,
        );

        let figures = load(&db, YearMonth::new(2026, 9));

        assert_eq!(figures.monthly_expenses, money(234_000));
        assert_eq!(figures.other_movements, Some(money(4000)));
        let total = figures.carry_forward.unwrap().minor + figures.monthly_income.minor
            - figures.monthly_expenses.minor
            + figures.other_movements.unwrap().minor;
        assert_eq!(total, figures.available_balance.minor);
    }

    #[test]
    fn a_transfer_between_available_accounts_is_no_other_movement() {
        let (_temp, db) = database();
        let (checking, _, _, transfers) = carry_fixtures(&db);
        let savings = account(
            &db,
            "DE44500105175407324931",
            50_000,
            Currency::EUR,
            AccountType::Checking,
        );
        txn(&db, savings, Currency::EUR, date!(2026 - 08 - 01), 100, "August", None);
        txn(
            &db,
            checking,
            Currency::EUR,
            date!(2026 - 09 - 18),
            -10_000,
            "To savings",
            Some(transfers),
        );
        txn(
            &db,
            savings,
            Currency::EUR,
            date!(2026 - 09 - 18),
            10_000,
            "From checking",
            Some(transfers),
        );

        let figures = load(&db, YearMonth::new(2026, 9));

        assert_eq!(figures.other_movements, Some(money(0)));
        assert_eq!(figures.monthly_income, money(200_000));
        assert_eq!(figures.monthly_expenses, money(230_000));
    }

    #[test]
    fn an_incomplete_history_hides_the_carry_forward_until_the_month_is_covered() {
        let (_temp, db) = database();
        carry_fixtures(&db);
        let savings = account(
            &db,
            "DE44500105175407324931",
            50_000,
            Currency::EUR,
            AccountType::Checking,
        );
        txn(
            &db,
            savings,
            Currency::EUR,
            date!(2026 - 09 - 05),
            10_000,
            "First booking",
            None,
        );

        let september = load(&db, YearMonth::new(2026, 9));
        assert_eq!(september.carry_forward, None);
        assert_eq!(september.other_movements, None);

        let october = load(&db, YearMonth::new(2026, 10));
        assert!(october.carry_forward.is_some());
        assert!(october.other_movements.is_some());
    }

    #[test]
    fn the_current_month_keeps_todays_balance_even_with_a_future_dated_booking() {
        let (_temp, db) = database();
        let (checking, _, _, _) = carry_fixtures(&db);

        let figures = load(&db, YearMonth::new(2026, 10));
        assert_eq!(figures.balance_date, None);
        assert_eq!(figures.available_balance, money(100_000));

        txn(
            &db,
            checking,
            Currency::EUR,
            date!(2026 - 11 - 05),
            -10_000,
            "Future",
            None,
        );
        let figures = load(&db, YearMonth::new(2026, 10));
        assert_eq!(figures.available_balance, money(100_000));
        let total = figures.carry_forward.unwrap().minor + figures.monthly_income.minor
            - figures.monthly_expenses.minor
            + figures.other_movements.unwrap().minor;
        assert_eq!(total, figures.available_balance.minor);
    }

    #[test]
    fn a_depot_keeps_its_current_market_balance_for_a_past_month() {
        let (_temp, db) = database();
        let (_, _, depot, _) = carry_fixtures(&db);
        txn(
            &db,
            depot,
            Currency::EUR,
            date!(2026 - 09 - 12),
            70_000,
            "Depot buy",
            None,
        );

        let figures = load(&db, YearMonth::new(2026, 9));

        assert_eq!(figures.available_balance, money(80_000));
        assert_eq!(figures.loan_balance, money(-500_000));
        assert_eq!(
            figures.total_balance,
            money(-120_000),
            "the depot keeps its current balance"
        );
    }

    #[test]
    fn dashboard_totals_stay_in_the_primary_currency() {
        let (_temp, db) = database();
        let eur = eur_account(&db, "DE89370400440532013000", 100_000);
        let usd_currency = Currency::new("USD").unwrap();
        let usd = account(
            &db,
            "DE44500105175407324931",
            50_000,
            usd_currency,
            AccountType::Checking,
        );
        txn(
            &db,
            eur,
            Currency::EUR,
            date!(2026 - 10 - 02),
            200_000,
            "Employer",
            None,
        );
        txn(&db, usd, usd_currency, date!(2026 - 10 - 03), 80_000, "US client", None);
        txn(&db, usd, usd_currency, date!(2026 - 10 - 04), -4000, "US shop", None);

        let figures = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.currency, Currency::EUR);
        assert_eq!(
            figures.total_balance,
            money(100_000),
            "not 1500: USD isn't added to EUR"
        );
        assert_eq!(
            figures.other_balances.get(&usd_currency),
            Some(&Money {
                minor: 50_000,
                currency: usd_currency
            })
        );
        assert_eq!(figures.monthly_income, money(200_000));
        assert_eq!(figures.monthly_expenses.minor, 0);
        assert!(figures.expenses_by_category.is_empty());
        assert_eq!(
            figures.uncategorized_count, 3,
            "all uncategorized bookings, any currency"
        );
    }

    #[test]
    fn the_primary_currency_setting_switches_what_the_dashboard_totals() {
        let (_temp, db) = database();
        let eur = eur_account(&db, "DE89370400440532013000", 100_000);
        let usd_currency = Currency::new("USD").unwrap();
        let usd = account(
            &db,
            "DE44500105175407324931",
            50_000,
            usd_currency,
            AccountType::Checking,
        );
        txn(
            &db,
            eur,
            Currency::EUR,
            date!(2026 - 10 - 02),
            200_000,
            "Employer",
            None,
        );
        txn(&db, usd, usd_currency, date!(2026 - 10 - 03), 80_000, "US client", None);
        crate::settings::set_setting(&db, Currencies::KEY_PRIMARY, "USD").unwrap();

        let figures = load_dashboard(&db, date!(2026 - 10 - 15), YearMonth::new(2026, 10)).unwrap();

        assert_eq!(figures.currency, usd_currency);
        assert_eq!(figures.total_balance.minor, 50_000);
        assert_eq!(figures.monthly_income.minor, 80_000);
    }

    #[test]
    fn dashboard_and_bars_leave_transfers_out_of_income_and_spending() {
        let (_temp, db) = database();
        let today = date!(2026 - 06 - 15);
        let account = eur_account(&db, "DE89370400440532013000", 100_000);
        let second = eur_account(&db, "DE12500105170137075030", 50_000);
        let transfers = category(&db, "Own transfers", "#4CAF50", false, true, None);
        let groceries = category(&db, "Groceries", "#FF5722", false, false, None);
        txn(
            &db,
            account,
            Currency::EUR,
            today,
            -50_000,
            "Erika Musterfrau",
            Some(transfers),
        );
        txn(
            &db,
            second,
            Currency::EUR,
            today,
            50_000,
            "Erika Musterfrau",
            Some(transfers),
        );
        txn(
            &db,
            account,
            Currency::EUR,
            today.checked_add(Duration::days(1)).unwrap(),
            -4000,
            "BEISPIEL MARKT",
            Some(groceries),
        );
        txn(
            &db,
            account,
            Currency::EUR,
            today.checked_add(Duration::days(2)).unwrap(),
            100_000,
            "Erika Mustermann",
            None,
        );

        let figures = load_dashboard(&db, today, YearMonth::new(2026, 6)).unwrap();
        assert_eq!(figures.monthly_income, money(100_000));
        assert_eq!(figures.monthly_expenses, money(4000));
        assert_eq!(
            figures
                .expenses_by_category
                .iter()
                .map(|slice| slice.label.as_str())
                .collect::<Vec<_>>(),
            vec!["Groceries"]
        );
        assert_eq!(figures.transfers_this_month, money(50_000));
        let june = figures.last_months.iter().find(|bar| bar.month == 6).unwrap();
        assert_eq!(june.income, money(100_000));
        assert_eq!(june.expenses, money(4000));
    }

    #[test]
    fn budgets_leave_transfers_out_and_a_transfer_budget_stays_at_zero_spent() {
        let (_temp, db) = database();
        let today = date!(2026 - 06 - 15);
        let account = eur_account(&db, "DE89370400440532013000", 0);
        let second = eur_account(&db, "DE12500105170137075030", 0);
        let transfers = category(&db, "Own transfers", "#4CAF50", false, true, Some(50_000));
        let transport = category(&db, "Transport", "#FF5722", true, false, Some(10_000));
        txn(&db, account, Currency::EUR, today, -50_000, "Transfer", Some(transfers));
        txn(&db, second, Currency::EUR, today, 50_000, "Transfer", Some(transfers));
        txn(&db, account, Currency::EUR, today, -3000, "Train", Some(transport));

        let figures = load_dashboard(&db, today, YearMonth::new(2026, 6)).unwrap();
        let transport_budget = figures
            .budgets
            .iter()
            .find(|budget| budget.category_id == transport)
            .unwrap();
        assert_eq!(transport_budget.spent, money(3000));
        let transfer_budget = figures
            .budgets
            .iter()
            .find(|budget| budget.category_id == transfers)
            .unwrap();
        assert_eq!(transfer_budget.spent, money(0));
        assert_eq!(transfer_budget.remaining(), money(50_000));
    }

    #[test]
    fn dashboard_separates_income_from_expenses() {
        let (_temp, db) = database();
        let today = date!(2026 - 03 - 20);
        let account = eur_account(&db, "DE89370400440532013000", 150_000);
        let food = category(&db, "Food", "#4CAF50", false, false, None);
        let tax = category(&db, "Spenden", "#3BC7B0", true, false, None);
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 03 - 02),
            -3000,
            "REWE",
            Some(food),
        );
        txn(&db, account, Currency::EUR, date!(2026 - 03 - 05), -2000, "Kiosk", None);
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 03 - 01),
            250_000,
            "Employer",
            None,
        );
        txn(
            &db,
            account,
            Currency::EUR,
            date!(2026 - 01 - 15),
            -10_000,
            "UNICEF",
            Some(tax),
        );

        let figures = load_dashboard(&db, today, YearMonth::new(2026, 3)).unwrap();

        assert_eq!(figures.total_balance, money(150_000));
        assert_eq!(figures.monthly_income, money(250_000));
        assert_eq!(figures.monthly_expenses, money(5000));
        assert_eq!(figures.uncategorized_count, 2, "Kiosk and the uncategorized salary");
        assert_eq!(
            figures
                .expenses_by_category
                .iter()
                .map(|slice| slice.label.as_str())
                .collect::<Vec<_>>(),
            vec!["Food", "Uncategorized"]
        );
        assert_eq!(figures.expenses_by_category[0].color.as_deref(), Some("#4CAF50"));
        assert_eq!(
            figures.expenses_by_category[1].color.as_deref(),
            Some(UNCATEGORIZED_COLOR)
        );
        assert_eq!(
            figures
                .tax_deductions_by_category
                .iter()
                .map(|slice| slice.label.as_str())
                .collect::<Vec<_>>(),
            vec!["Spenden"]
        );
        assert_eq!(figures.last_months.len(), 6);
        assert_eq!(
            figures.last_months.iter().find(|bar| bar.month == 1).unwrap().expenses,
            money(10_000)
        );
    }
}
