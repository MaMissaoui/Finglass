//! Reports ported from FinDesk's `document/ReportService.kt`.
//!
//! The calculations are pure: they take already-loaded bookings and return report data with integer
//! minor-unit amounts. Totals never mix currencies; everything is in the report currency and bookings
//! in other currencies are only counted.

use std::collections::BTreeMap;
use std::sync::LazyLock;

use regex::Regex;
use rust_decimal::Decimal;
use rust_decimal::RoundingStrategy;
use time::{Date, Month};

use crate::dashboard::YearMonth;
use crate::db::Database;
use crate::i18n::{self, Key, Language};
use crate::model::{CategoryId, TransactionFilter, TransactionItem};
use crate::money::{Currencies, Currency, Money};
use crate::repo::{self, Result};
use crate::rules::payment_providers::PaymentProviders;

use super::format;

/// The neutral label for bookings without a category.
pub const UNCATEGORIZED: &str = "Uncategorized";

/// One share of the spending split.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CategoryShare {
    /// The category's id, or `None` for uncategorized.
    pub category_id: Option<CategoryId>,
    /// The display name.
    pub name: String,
    /// The amount accounted for.
    pub amount: Money,
    /// The category's color, or an empty string.
    pub color: String,
}

/// One month of the tax report.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MonthlyData {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
    /// Money in.
    pub income: Money,
    /// Money out, as a positive amount.
    pub expenses: Money,
}

impl MonthlyData {
    /// Income minus expenses.
    pub fn net(&self) -> Money {
        money(self.income.minor - self.expenses.minor, self.income.currency)
    }
}

/// The annual or range tax report.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaxReport {
    /// The start year.
    pub year: i32,
    /// The first day, inclusive.
    pub from: Date,
    /// The last day, inclusive.
    pub to: Date,
    /// Money in.
    pub total_income: Money,
    /// Money out, positive.
    pub total_expenses: Money,
    /// Income minus expenses.
    pub net_income: Money,
    /// Expenses grouped by category name, largest first.
    pub expenses_by_category: Vec<(String, Money)>,
    /// The Tax bookings tab's deduction groups: each category name at its absolute net.
    pub tax_relevant_expenses: Vec<(String, Money)>,
    /// The tax-relevant bookings.
    pub tax_relevant_transactions: Vec<TransactionItem>,
    /// Every month in the range.
    pub monthly_breakdown: Vec<MonthlyData>,
    /// The biggest vendors, largest first.
    pub top_vendors: Vec<(String, Money)>,
    /// How many bookings are in the report currency.
    pub transaction_count: usize,
    /// How many distinct categories the bookings use.
    pub categories_used: usize,
    /// The currency of every amount.
    pub currency: Currency,
    /// Bookings in the period in another currency, not included in any total.
    pub other_currency_bookings: usize,
    /// Money moved between the user's own accounts.
    pub transfer_total: Money,
    /// The spending split behind [`Self::expenses_by_category`].
    pub category_shares: Vec<CategoryShare>,
}

impl TaxReport {
    /// The sum of the deduction groups.
    pub fn tax_relevant_total(&self) -> Money {
        money(
            self.tax_relevant_expenses.iter().map(|(_, amount)| amount.minor).sum(),
            self.currency,
        )
    }

    /// Whether the report covers a full calendar year.
    pub fn is_calendar_year(&self) -> bool {
        self.from == Date::from_calendar_date(self.year, Month::January, 1).expect("valid date")
            && self.to == Date::from_calendar_date(self.year, Month::December, 31).expect("valid date")
    }

    /// The report title: the year, or the date range for a custom period.
    pub fn title(&self, _language: Language) -> String {
        if self.is_calendar_year() {
            self.year.to_string()
        } else {
            format!("{} – {}", format::date(self.from), format::date(self.to))
        }
    }

    /// An empty report for a year, in euros like Kotlin's `TaxReport.empty`.
    pub fn empty(year: i32) -> Self {
        let from = Date::from_calendar_date(year, Month::January, 1).expect("valid date");
        let to = Date::from_calendar_date(year, Month::December, 31).expect("valid date");
        Self {
            year,
            from,
            to,
            total_income: money(0, Currency::EUR),
            total_expenses: money(0, Currency::EUR),
            net_income: money(0, Currency::EUR),
            expenses_by_category: Vec::new(),
            tax_relevant_expenses: Vec::new(),
            tax_relevant_transactions: Vec::new(),
            monthly_breakdown: Vec::new(),
            top_vendors: Vec::new(),
            transaction_count: 0,
            categories_used: 0,
            currency: Currency::EUR,
            other_currency_bookings: 0,
            transfer_total: money(0, Currency::EUR),
            category_shares: Vec::new(),
        }
    }
}

/// The monthly spending report.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MonthlyReport {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
    /// Money in.
    pub total_income: Money,
    /// Money out, positive.
    pub total_expenses: Money,
    /// Income minus expenses.
    pub net_amount: Money,
    /// Expenses by category name, sorted by name.
    pub by_category: BTreeMap<String, Money>,
    /// The ten biggest expenses.
    pub top_expenses: Vec<ExpenseItem>,
    /// How many bookings are in the month.
    pub transaction_count: usize,
}

/// One expense of the monthly report.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ExpenseItem {
    /// The display title.
    pub description: String,
    /// The absolute amount.
    pub amount: Money,
    /// The category name, or the uncategorized label.
    pub category: String,
    /// The booking date.
    pub date: Date,
}

/// One month of a category trend.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MonthlyAmount {
    /// The year.
    pub year: i32,
    /// The month, 1 to 12.
    pub month: u8,
    /// The absolute amount in that month.
    pub amount: Money,
}

/// A category's spending trend.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CategoryTrend {
    /// The category id.
    pub category_id: CategoryId,
    /// The category name, or the localized unknown label.
    pub category_name: String,
    /// Every requested month.
    pub monthly_data: Vec<MonthlyAmount>,
    /// The average monthly amount.
    pub average_monthly: Money,
    /// The latest month against the average, in percent, one decimal.
    pub trend: Decimal,
}

/// One category's tax bookings of a period.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaxGroup {
    /// The category id, or `None` for uncategorized.
    pub category_id: Option<CategoryId>,
    /// The display name.
    pub name: String,
    /// The category's color.
    pub color: String,
    /// The group's bookings, date ascending, then id.
    pub bookings: Vec<TransactionItem>,
    /// Money in, signed.
    pub income: Money,
    /// Money out, positive.
    pub expenses: Money,
}

impl TaxGroup {
    /// Income minus expenses.
    pub fn net(&self) -> Money {
        money(self.income.minor - self.expenses.minor, self.income.currency)
    }

    /// How many bookings carry at least one linked receipt.
    pub fn with_receipt(&self) -> usize {
        self.bookings
            .iter()
            .filter(|booking| booking.document_count > 0)
            .count()
    }
}

/// The Tax bookings view over a period.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaxBookings {
    /// The first day, inclusive.
    pub from: Date,
    /// The last day, inclusive.
    pub to: Date,
    /// The currency of every amount.
    pub currency: Currency,
    /// Categories whose net is non-negative.
    pub income_groups: Vec<TaxGroup>,
    /// Categories whose net is negative.
    pub deduction_groups: Vec<TaxGroup>,
    /// How many tax-relevant bookings are in the report currency.
    pub booking_count: usize,
    /// Tax-relevant bookings in another currency; not included in any total.
    pub other_currency_bookings: usize,
}

impl TaxBookings {
    /// The sum of the income groups' nets.
    pub fn income_total(&self) -> Money {
        money(
            self.income_groups.iter().map(|group| group.net().minor).sum(),
            self.currency,
        )
    }

    /// The sum of the deduction groups' absolute nets.
    pub fn deduction_total(&self) -> Money {
        money(
            self.deduction_groups
                .iter()
                .map(|group| i64::try_from(group.net().minor.unsigned_abs()).unwrap_or(i64::MAX))
                .sum(),
            self.currency,
        )
    }

    /// Bookings with a receipt, across both sections.
    pub fn with_receipt(&self) -> usize {
        self.income_groups
            .iter()
            .chain(&self.deduction_groups)
            .map(TaxGroup::with_receipt)
            .sum()
    }

    /// Income total minus deduction total.
    pub fn net_total(&self) -> Money {
        money(self.income_total().minor - self.deduction_total().minor, self.currency)
    }

    /// "2026" for a full calendar year, otherwise the date range.
    pub fn title(&self, _language: Language) -> String {
        let calendar_year = self.from.year() == self.to.year()
            && self.from == Date::from_calendar_date(self.from.year(), Month::January, 1).expect("valid date")
            && self.to == Date::from_calendar_date(self.from.year(), Month::December, 31).expect("valid date");
        if calendar_year {
            self.from.year().to_string()
        } else {
            format!("{} – {}", format::date(self.from), format::date(self.to))
        }
    }
}

fn money(minor: i64, currency: Currency) -> Money {
    Money { minor, currency }
}

fn sum_signed(items: impl Iterator<Item = i64>) -> i64 {
    items.sum()
}

fn sum_abs(items: impl Iterator<Item = i64>) -> i64 {
    items
        .map(i64::unsigned_abs)
        .map(|value| i64::try_from(value).unwrap_or(i64::MAX))
        .sum()
}

/// Builds the annual or range tax report.
pub fn build_report(
    from: Date,
    to: Date,
    all: &[TransactionItem],
    currency: Currency,
    language: Language,
) -> TaxReport {
    let transactions: Vec<&TransactionItem> = all.iter().filter(|item| item.currency == currency).collect();
    let other_currency = all.len() - transactions.len();

    let income: Vec<&TransactionItem> = transactions
        .iter()
        .copied()
        .filter(|item| item.counts_as_income())
        .collect();
    let expenses: Vec<&TransactionItem> = transactions
        .iter()
        .copied()
        .filter(|item| item.counts_as_spending())
        .collect();

    let total_income = sum_signed(income.iter().map(|item| item.amount.minor));
    let total_expenses = sum_abs(expenses.iter().map(|item| item.amount.minor));

    let tax_relevant: Vec<TransactionItem> = transactions
        .iter()
        .filter(|item| item.is_expense() && item.is_tax_relevant)
        .map(|item| (*item).clone())
        .collect();

    let tax_relevant_expenses = build_tax_bookings(from, to, all, currency, language)
        .deduction_groups
        .into_iter()
        .map(|group| {
            let amount = i64::try_from(group.net().minor.unsigned_abs()).unwrap_or(i64::MAX);
            (group.name, money(amount, currency))
        })
        .collect();

    let start_month = YearMonth::from_date(from);
    let end_month = YearMonth::from_date(to);
    let mut monthly_breakdown = Vec::new();
    let mut month = start_month;
    while month <= end_month {
        let month_transactions = transactions
            .iter()
            .filter(|item| item.date.year() == month.year && u8::from(item.date.month()) == month.month);
        let month_income: Vec<&&TransactionItem> = month_transactions
            .clone()
            .filter(|item| item.counts_as_income())
            .collect();
        let month_expenses: Vec<&&TransactionItem> =
            month_transactions.filter(|item| item.counts_as_spending()).collect();
        monthly_breakdown.push(MonthlyData {
            year: month.year,
            month: month.month,
            income: money(sum_signed(month_income.iter().map(|item| item.amount.minor)), currency),
            expenses: money(sum_abs(month_expenses.iter().map(|item| item.amount.minor)), currency),
        });
        month = month.plus_months(1);
    }

    let categories_used = transactions
        .iter()
        .filter_map(|item| item.category_id)
        .collect::<std::collections::BTreeSet<_>>()
        .len();

    let transfer_total = sum_abs(
        transactions
            .iter()
            .filter(|item| item.is_transfer && item.amount.minor < 0)
            .map(|item| item.amount.minor),
    );

    TaxReport {
        year: from.year(),
        from,
        to,
        total_income: money(total_income, currency),
        total_expenses: money(total_expenses, currency),
        net_income: money(total_income - total_expenses, currency),
        expenses_by_category: sum_by_category_refs(&expenses, language),
        tax_relevant_expenses,
        tax_relevant_transactions: tax_relevant,
        monthly_breakdown,
        top_vendors: top_vendors_refs(&expenses, 5),
        transaction_count: transactions.len(),
        categories_used,
        currency,
        other_currency_bookings: other_currency,
        transfer_total: money(transfer_total, currency),
        category_shares: category_shares_refs(&expenses, language),
    }
}

/// The annual tax report.
pub fn build_tax_report(
    year: i32,
    transactions: &[TransactionItem],
    currency: Currency,
    language: Language,
) -> TaxReport {
    build_report(
        Date::from_calendar_date(year, Month::January, 1).expect("valid date"),
        Date::from_calendar_date(year, Month::December, 31).expect("valid date"),
        transactions,
        currency,
        language,
    )
}

/// The pure Tax bookings view over a period.
pub fn build_tax_bookings(
    from: Date,
    to: Date,
    all: &[TransactionItem],
    currency: Currency,
    language: Language,
) -> TaxBookings {
    let relevant: Vec<&TransactionItem> = all.iter().filter(|item| item.is_tax_relevant).collect();
    let in_currency: Vec<&TransactionItem> = relevant
        .iter()
        .copied()
        .filter(|item| item.currency == currency)
        .collect();
    let other_currency = relevant.len() - in_currency.len();

    let mut grouped: Vec<(Option<CategoryId>, Vec<&TransactionItem>)> = Vec::new();
    for booking in &in_currency {
        if let Some((_, bookings)) = grouped.iter_mut().find(|(id, _)| *id == booking.category_id) {
            bookings.push(booking);
        } else {
            grouped.push((booking.category_id, vec![booking]));
        }
    }

    let uncategorized = i18n::text(language, Key::Uncategorized).to_owned();
    let mut groups: Vec<TaxGroup> = grouped
        .into_iter()
        .map(|(category_id, bookings)| {
            let first = bookings.first().expect("a group has at least one booking");
            let name = first.category_name.clone().unwrap_or_else(|| uncategorized.clone());
            let color = first.category_color.clone().unwrap_or_default();
            let income = sum_signed(
                bookings
                    .iter()
                    .filter(|item| item.amount.minor > 0)
                    .map(|item| item.amount.minor),
            );
            let expenses = sum_abs(
                bookings
                    .iter()
                    .filter(|item| item.amount.minor < 0)
                    .map(|item| item.amount.minor),
            );
            let mut bookings: Vec<TransactionItem> = bookings.into_iter().cloned().collect();
            bookings.sort_by_key(|left| (left.date, left.id));
            TaxGroup {
                category_id,
                name,
                color,
                bookings,
                income: money(income, currency),
                expenses: money(expenses, currency),
            }
        })
        .collect();

    groups.sort_by(|left, right| {
        right
            .net()
            .minor
            .unsigned_abs()
            .cmp(&left.net().minor.unsigned_abs())
            .then_with(|| left.name.cmp(&right.name))
    });

    let (income_groups, deduction_groups) = groups.into_iter().partition(|group| group.net().minor >= 0);

    TaxBookings {
        from,
        to,
        currency,
        income_groups,
        deduction_groups,
        booking_count: in_currency.len(),
        other_currency_bookings: other_currency,
    }
}

/// The monthly spending report over already-filtered bookings.
pub fn monthly_report(
    year: i32,
    month: u8,
    transactions: &[TransactionItem],
    currency: Currency,
    language: Language,
) -> MonthlyReport {
    let income: Vec<&TransactionItem> = transactions.iter().filter(|item| item.counts_as_income()).collect();
    let expense_transactions: Vec<&TransactionItem> =
        transactions.iter().filter(|item| item.counts_as_spending()).collect();

    let total_income = sum_signed(income.iter().map(|item| item.amount.minor));
    let total_expenses = sum_abs(expense_transactions.iter().map(|item| item.amount.minor));

    let mut sorted = expense_transactions.clone();
    sorted.sort_by_key(|left| left.amount.minor);
    let uncategorized = i18n::text(language, Key::Uncategorized).to_owned();
    let top_expenses = sorted
        .into_iter()
        .take(10)
        .map(|item| ExpenseItem {
            description: display_description(item),
            amount: money(
                item.amount.minor.unsigned_abs().try_into().unwrap_or(i64::MAX),
                item.currency,
            ),
            category: item.category_name.clone().unwrap_or_else(|| uncategorized.clone()),
            date: item.date,
        })
        .collect();

    let mut by_category: BTreeMap<String, Money> = BTreeMap::new();
    for (name, amount) in sum_by_category_refs(&expense_transactions, language) {
        by_category.insert(name, amount);
    }

    MonthlyReport {
        year,
        month,
        total_income: money(total_income, currency),
        total_expenses: money(total_expenses, currency),
        net_amount: money(total_income - total_expenses, currency),
        by_category,
        top_expenses,
        transaction_count: transactions.len(),
    }
}

/// Groups expenses by category name, largest first.
pub fn sum_by_category(transactions: &[TransactionItem], language: Language) -> Vec<(String, Money)> {
    let refs: Vec<&TransactionItem> = transactions.iter().collect();
    sum_by_category_refs(&refs, language)
}

fn sum_by_category_refs(transactions: &[&TransactionItem], language: Language) -> Vec<(String, Money)> {
    let uncategorized = i18n::text(language, Key::Uncategorized).to_owned();
    let mut groups: Vec<(String, i64, Currency)> = Vec::new();
    for item in transactions {
        let name = item.category_name.clone().unwrap_or_else(|| uncategorized.clone());
        let amount = i64::try_from(item.amount.minor.unsigned_abs()).unwrap_or(i64::MAX);
        if let Some(group) = groups.iter_mut().find(|(existing, _, _)| *existing == name) {
            group.1 = group.1.saturating_add(amount);
        } else {
            groups.push((name, amount, item.currency));
        }
    }
    groups.sort_by_key(|left| std::cmp::Reverse(left.1));
    groups
        .into_iter()
        .map(|(name, amount, currency)| (name, money(amount, currency)))
        .collect()
}

/// The spending split: one share per category, largest first.
pub fn category_shares(spending: &[TransactionItem], language: Language) -> Vec<CategoryShare> {
    let refs: Vec<&TransactionItem> = spending.iter().collect();
    category_shares_refs(&refs, language)
}

fn category_shares_refs(spending: &[&TransactionItem], language: Language) -> Vec<CategoryShare> {
    let uncategorized = i18n::text(language, Key::Uncategorized).to_owned();
    let mut groups: Vec<CategoryShare> = Vec::new();
    for item in spending {
        let amount = i64::try_from(item.amount.minor.unsigned_abs()).unwrap_or(i64::MAX);
        if let Some(group) = groups.iter_mut().find(|group| group.category_id == item.category_id) {
            group.amount.minor = group.amount.minor.saturating_add(amount);
        } else {
            groups.push(CategoryShare {
                category_id: item.category_id,
                name: item.category_name.clone().unwrap_or_else(|| uncategorized.clone()),
                amount: money(amount, item.currency),
                color: item.category_color.clone().unwrap_or_default(),
            });
        }
    }
    groups.sort_by_key(|left| std::cmp::Reverse(left.amount.minor));
    groups
}

/// The period's expenses grouped into vendors, largest first.
pub fn top_vendors(expenses: &[TransactionItem], limit: usize) -> Vec<(String, Money)> {
    let refs: Vec<&TransactionItem> = expenses.iter().collect();
    top_vendors_refs(&refs, limit)
}

fn top_vendors_refs(expenses: &[&TransactionItem], limit: usize) -> Vec<(String, Money)> {
    fn key(name: &str) -> String {
        name.split_whitespace().collect::<Vec<_>>().join(" ").to_lowercase()
    }

    fn label(named: &[(&TransactionItem, String)]) -> String {
        let mut counts: BTreeMap<&str, usize> = BTreeMap::new();
        for (_, name) in named {
            *counts.entry(name.as_str()).or_default() += 1;
        }
        let most_used = counts.values().copied().max().unwrap_or(0);
        let candidates: Vec<&str> = counts
            .iter()
            .filter(|(_, count)| **count == most_used)
            .map(|(name, _)| *name)
            .collect();
        if let [only] = candidates.as_slice() {
            return (*only).to_owned();
        }

        named
            .iter()
            .filter(|(_, name)| candidates.contains(&name.as_str()))
            .max_by(|(left, _), (right, _)| (left.date, left.id).cmp(&(right.date, right.id)))
            .map(|(_, name)| name.clone())
            .unwrap_or_default()
    }

    let mut groups: Vec<(String, Vec<(&TransactionItem, String)>)> = Vec::new();
    for item in expenses {
        let name = display_description(item);
        let vendor_key = key(&name);
        if let Some((_, named)) = groups.iter_mut().find(|(existing, _)| *existing == vendor_key) {
            named.push((item, name));
        } else {
            groups.push((vendor_key, vec![(item, name)]));
        }
    }

    let mut vendors: Vec<(String, Money)> = groups
        .into_iter()
        .map(|(_, named)| {
            let name = label(&named);
            let amount = named
                .iter()
                .map(|(item, _)| i64::try_from(item.amount.minor.unsigned_abs()).unwrap_or(i64::MAX))
                .sum();
            let currency = named.first().map(|(item, _)| item.currency).unwrap_or(Currency::EUR);
            (name, money(amount, currency))
        })
        .collect();
    vendors.sort_by_key(|left| std::cmp::Reverse(left.1.minor));
    vendors.truncate(limit);
    vendors
}

/// The category spending trends over the last `months` months, including the current one.
pub fn get_category_trends(
    db: &Database,
    category_id: CategoryId,
    months: usize,
    today: Date,
    language: Language,
) -> Result<CategoryTrend> {
    let category = repo::categories::get(db, category_id)?;
    let currency = Currencies::primary(db)?;
    let first_month =
        YearMonth::from_date(today).minus_months(i32::try_from(months.saturating_sub(1)).unwrap_or(i32::MAX));
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(first_month.start()),
            end_date: Some(today),
            category_id: Some(category_id),
            currency: Some(currency),
            ..Default::default()
        },
        None,
    )?;

    let monthly_data: Vec<MonthlyAmount> = (0..months)
        .map(|index| {
            let month = first_month.plus_months(i32::try_from(index).unwrap_or(i32::MAX));
            let amount: i64 = transactions
                .iter()
                .filter(|item| item.date.year() == month.year && u8::from(item.date.month()) == month.month)
                .map(|item| i64::try_from(item.amount.minor.unsigned_abs()).unwrap_or(i64::MAX))
                .sum();
            MonthlyAmount {
                year: month.year,
                month: month.month,
                amount: money(amount, currency),
            }
        })
        .collect();

    let total: i64 = monthly_data.iter().map(|month| month.amount.minor).sum();
    let average_minor = if monthly_data.is_empty() || total <= 0 {
        0
    } else {
        let count = i64::try_from(monthly_data.len()).expect("month count");
        // Kotlin divides with scale 2 and HALF_UP.
        (total.saturating_mul(2).saturating_add(count)) / count.saturating_mul(2)
    };
    let average = money(average_minor, currency);
    let latest = monthly_data.last().map(|month| month.amount.minor).unwrap_or(0);
    let trend = if average_minor > 0 {
        let difference = Decimal::from(latest - average_minor);
        (difference * Decimal::from(100) / Decimal::from(average_minor))
            .round_dp_with_strategy(1, RoundingStrategy::MidpointAwayFromZero)
    } else {
        Decimal::ZERO
    };

    Ok(CategoryTrend {
        category_id,
        category_name: category.map_or_else(
            || i18n::text(language, Key::ReportUnknownCategory).to_owned(),
            |category| category.name,
        ),
        monthly_data,
        average_monthly: average,
        trend,
    })
}

/// Generates the annual tax report from the database.
pub fn generate_tax_report(db: &Database, year: i32, language: Language) -> Result<TaxReport> {
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(Date::from_calendar_date(year, Month::January, 1).expect("valid date")),
            end_date: Some(Date::from_calendar_date(year, Month::December, 31).expect("valid date")),
            ..Default::default()
        },
        None,
    )?;
    Ok(build_tax_report(
        year,
        &transactions,
        Currencies::primary(db)?,
        language,
    ))
}

/// Generates the tax report for an inclusive date range.
pub fn generate_range_report(db: &Database, from: Date, to: Date, language: Language) -> Result<TaxReport> {
    if from > to {
        return Err(repo::RepoError::Invalid(
            i18n::text(language, Key::InvalidDateRange).to_owned(),
        ));
    }
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(from),
            end_date: Some(to),
            ..Default::default()
        },
        None,
    )?;
    Ok(build_report(
        from,
        to,
        &transactions,
        Currencies::primary(db)?,
        language,
    ))
}

/// Generates the Tax bookings view from the database.
pub fn generate_tax_bookings(db: &Database, from: Date, to: Date, language: Language) -> Result<TaxBookings> {
    if from > to {
        return Err(repo::RepoError::Invalid(
            i18n::text(language, Key::InvalidDateRange).to_owned(),
        ));
    }
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(from),
            end_date: Some(to),
            ..Default::default()
        },
        None,
    )?;
    Ok(build_tax_bookings(
        from,
        to,
        &transactions,
        Currencies::primary(db)?,
        language,
    ))
}

/// Generates the monthly spending report from the database.
pub fn generate_monthly_report(db: &Database, year: i32, month: u8, language: Language) -> Result<MonthlyReport> {
    let start = YearMonth::new(year, month).start();
    let end = YearMonth::new(year, month).end();
    let currency = Currencies::primary(db)?;
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(start),
            end_date: Some(end),
            currency: Some(currency),
            ..Default::default()
        },
        None,
    )?;
    Ok(monthly_report(year, month, &transactions, currency, language))
}

static PADDING: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\s{3,}").expect("padding pattern is valid"));
static SEPPA_TAG: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\+")
        .expect("tag pattern is valid")
});
static WHITESPACE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\s+").expect("whitespace pattern is valid"));

/// The readable partner, as FinDesk's `BankText.displayPartner` renders it.
fn display_partner(raw: Option<&str>) -> Option<String> {
    let raw = raw?;
    let first = PADDING.split(raw).find(|part| !part.trim().is_empty())?;
    let cleaned = WHITESPACE.replace_all(first, " ").trim().to_owned();
    (!cleaned.is_empty()).then_some(cleaned)
}

/// The readable purpose, as FinDesk's `BankText.displayPurpose` renders it.
fn display_purpose(raw: Option<&str>) -> Option<String> {
    let raw = raw?.trim();
    if raw.is_empty() {
        return None;
    }
    let matches: Vec<(usize, usize, String)> = SEPPA_TAG
        .find_iter(raw)
        .map(|found| (found.start(), found.end(), found.as_str().to_owned()))
        .collect();
    let result = if matches.is_empty() {
        raw.to_owned()
    } else if let Some((_, end, _)) = matches
        .iter()
        .find(|(_, _, tag)| tag.to_lowercase().starts_with("svwz"))
    {
        let index = matches
            .iter()
            .position(|(_, _, tag)| tag.to_lowercase().starts_with("svwz"))
            .expect("found above");
        let next = matches.get(index + 1).map(|(start, _, _)| *start).unwrap_or(raw.len());
        raw[*end..next].to_owned()
    } else {
        raw[..matches[0].0].to_owned()
    };
    let cleaned = WHITESPACE.replace_all(&result, " ").trim().to_owned();
    (!cleaned.is_empty()).then_some(cleaned)
}

/// What every screen and export shows as a booking's title.
pub fn display_description(item: &TransactionItem) -> String {
    PaymentProviders::title_of(item.purpose.as_deref(), item.partner_name.as_deref())
        .or_else(|| display_partner(item.partner_name.as_deref()))
        .or_else(|| display_purpose(item.purpose.as_deref()))
        .unwrap_or_else(|| "—".to_owned())
}
