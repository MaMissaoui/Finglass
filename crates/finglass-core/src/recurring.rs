//! Finds subscriptions, rent, salary and other bookings that repeat on a regular schedule.
//!
//! Ported from FinDesk's `document/RecurringDetector.kt`.

use std::collections::BTreeMap;

use time::{Date, Duration, Month};

use crate::model::TransactionItem;
use crate::money::{Currency, Money};
use crate::rules::payment_providers::PaymentProviders;

/// How often a recurring payment repeats.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Cadence {
    /// Every week.
    Weekly,
    /// Every month.
    Monthly,
    /// Every three months.
    Quarterly,
    /// Every year.
    Yearly,
}

impl Cadence {
    /// The model days between occurrences.
    pub const fn days(self) -> i64 {
        match self {
            Self::Weekly => 7,
            Self::Monthly => 30,
            Self::Quarterly => 91,
            Self::Yearly => 365,
        }
    }

    /// How far an interval may deviate from [`Self::days`] and still count.
    pub const fn tolerance_days(self) -> i64 {
        match self {
            Self::Weekly => 2,
            Self::Monthly => 4,
            Self::Quarterly => 10,
            Self::Yearly => 15,
        }
    }

    /// Occurrences in a year.
    pub const fn per_year(self) -> i64 {
        match self {
            Self::Weekly => 52,
            Self::Monthly => 12,
            Self::Quarterly => 4,
            Self::Yearly => 1,
        }
    }

    /// The next occurrence after `from`, clamped to the target month's length like `LocalDate.plusMonths`.
    pub fn next(self, from: Date) -> Date {
        match self {
            Self::Weekly => checked_day(from, 7),
            Self::Monthly => add_months(from, 1),
            Self::Quarterly => add_months(from, 3),
            Self::Yearly => add_months(from, 12),
        }
    }
}

/// One detected recurring payment.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecurringPayment {
    /// The display name.
    pub name: String,
    /// How often it repeats.
    pub cadence: Cadence,
    /// The median amount (signed: negative for payments).
    pub typical_amount: Money,
    /// Whether every occurrence had (almost) the same amount.
    pub fixed_amount: bool,
    /// The last occurrence.
    pub last_date: Date,
    /// The next expected occurrence.
    pub next_date: Date,
    /// How many distinct days carried the payment.
    pub occurrences: usize,
    /// The last booking's category name.
    pub category_name: Option<String>,
    /// The payment currency.
    pub currency: Currency,
    /// Whether the last booking moved between the user's own accounts.
    pub is_transfer: bool,
}

impl RecurringPayment {
    /// Whether the typical amount is money in.
    pub fn is_income(&self) -> bool {
        self.typical_amount.minor > 0
    }

    /// The typical amount scaled to a year.
    pub fn yearly_amount(&self) -> Money {
        Money {
            minor: self.typical_amount.minor * self.cadence.per_year(),
            currency: self.currency,
        }
    }
}

/// Finds recurring payments among bookings.
pub struct RecurringDetector;

/// At least this many bookings (two for yearly, which rarely has more history).
const MIN_OCCURRENCES: usize = 3;

/// Share of intervals that must match the cadence.
const MIN_REGULARITY: f64 = 0.75;

impl RecurringDetector {
    /// Detects recurring payments, sorted by next date and then name.
    pub fn detect(transactions: &[TransactionItem], today: Date) -> Vec<RecurringPayment> {
        let mut groups: BTreeMap<String, Vec<&TransactionItem>> = BTreeMap::new();
        for transaction in transactions {
            groups.entry(key(transaction)).or_default().push(transaction);
        }

        let mut detected: Vec<RecurringPayment> = groups
            .into_iter()
            .filter(|(key, _)| !key.is_empty())
            .filter_map(|(_, group)| analyse(&group, today))
            .collect();
        detected
            .sort_by(|left, right| (left.next_date, left.name.as_str()).cmp(&(right.next_date, right.name.as_str())));
        detected
    }

    /// Recurring payments due in the next `days` days, earliest first.
    pub fn upcoming(recurring: &[RecurringPayment], today: Date, days: i64) -> Vec<RecurringPayment> {
        let limit = checked_day(today, days);
        recurring
            .iter()
            .filter(|payment| payment.next_date <= limit)
            .cloned()
            .collect()
    }
}

fn analyse(group: &[&TransactionItem], today: Date) -> Option<RecurringPayment> {
    // One booking per day at most (split payments on the same day count once).
    let mut by_date: Vec<&TransactionItem> = group.to_vec();
    by_date.sort_by_key(|transaction| transaction.date);
    by_date.dedup_by_key(|transaction| transaction.date);

    if by_date.len() < 2 {
        return None;
    }

    let mut intervals: Vec<i64> = by_date
        .windows(2)
        .map(|pair| (pair[1].date - pair[0].date).whole_days())
        .collect();
    intervals.sort_unstable();
    let median = intervals[intervals.len() / 2];
    let cadence = [Cadence::Weekly, Cadence::Monthly, Cadence::Quarterly, Cadence::Yearly]
        .into_iter()
        .find(|cadence| (median - cadence.days()).abs() <= cadence.tolerance_days())?;

    let min_count = if cadence == Cadence::Yearly { 2 } else { MIN_OCCURRENCES };
    if by_date.len() < min_count {
        return None;
    }

    let regular = intervals
        .iter()
        .filter(|interval| (**interval - cadence.days()).abs() <= cadence.tolerance_days())
        .count();
    if regular as f64 / (intervals.len() as f64) < MIN_REGULARITY {
        return None;
    }

    let last = by_date.last()?;
    let mut next = cadence.next(last.date);
    // Stopped subscriptions: more than one full cycle overdue.
    if checked_day(next, cadence.days()) < today {
        return None;
    }
    // A booking a few days late is still "due", not skipped to the next cycle.
    let overdue_limit = checked_day(today, -cadence.tolerance_days());
    while next < overdue_limit {
        next = cadence.next(next);
    }

    let mut amounts: Vec<i64> = group.iter().map(|transaction| transaction.amount.minor).collect();
    amounts.sort_unstable();
    let median_amount = amounts[amounts.len() / 2];
    // Zero medians are booking artifacts (corrections, rejected debits), not a subscription.
    if median_amount == 0 {
        return None;
    }

    let tolerance = (median_amount.abs() / 100).max(1);
    let fixed = amounts.iter().all(|amount| (amount - median_amount).abs() <= tolerance);

    let name = PaymentProviders::title_of(last.purpose.as_deref(), last.partner_name.as_deref()).unwrap_or_else(|| {
        last.partner_name
            .clone()
            .unwrap_or_else(|| last.raw_description())
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ")
    });

    Some(RecurringPayment {
        name,
        cadence,
        typical_amount: Money {
            minor: median_amount,
            currency: last.amount.currency,
        },
        fixed_amount: fixed,
        last_date: last.date,
        next_date: next,
        occurrences: by_date.len(),
        category_name: last.category_name.clone(),
        currency: last.amount.currency,
        is_transfer: last.is_transfer,
    })
}

/// Same counterparty, direction and currency, with normalised names.
fn key(transaction: &TransactionItem) -> String {
    let shop = PaymentProviders::merchant_of(transaction.purpose.as_deref(), transaction.partner_name.as_deref());
    let source = shop
        .clone()
        .or_else(|| transaction.partner_name.clone())
        .or_else(|| transaction.purpose.clone())
        .unwrap_or_default();
    let name = normalise_name(&source);
    let scoped = if shop.is_none() || name.is_empty() {
        name
    } else {
        format!("paypal:{name}")
    };

    if scoped.is_empty() {
        String::new()
    } else {
        format!(
            "{scoped}|{}|{}",
            transaction.amount.minor.signum(),
            transaction.amount.currency
        )
    }
}

fn normalise_name(text: &str) -> String {
    let mut normalized = String::with_capacity(text.len());
    for character in text.to_lowercase().chars() {
        if character.is_ascii_digit() {
            normalized.push(' ');
        } else if character.is_alphabetic() || character == ' ' {
            normalized.push(character);
        } else {
            normalized.push(' ');
        }
    }

    normalized
        .split_whitespace()
        .filter(|word| word.chars().count() > 1)
        .take(3)
        .collect::<Vec<_>>()
        .join(" ")
}

fn checked_day(date: Date, days: i64) -> Date {
    date.checked_add(Duration::days(days)).expect("date overflow")
}

fn add_months(date: Date, months: i32) -> Date {
    let total = date.year() * 12 + i32::from(date.month() as u8) - 1 + months;
    let year = total.div_euclid(12);
    let month_number = u8::try_from(total.rem_euclid(12) + 1).expect("month in range");
    let month = Month::try_from(month_number).expect("month in range");
    let day = date.day().min(month.length(year));
    Date::from_calendar_date(year, month, day).expect("valid date")
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::model::{AccountId, TaxSource, TransactionId};

    fn today() -> Date {
        date!(2026 - 10 - 04)
    }

    fn money(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn transaction(
        id: i64,
        day: Date,
        minor: i64,
        partner: &str,
        purpose: Option<&str>,
        category: Option<&str>,
    ) -> TransactionItem {
        TransactionItem {
            id: TransactionId(id),
            account_id: AccountId(1),
            account_iban: Some("DE1".to_owned()),
            date: day,
            amount: money(minor),
            balance_after: None,
            currency: Currency::EUR,
            partner_name: Some(partner.to_owned()),
            purpose: purpose.map(str::to_owned),
            category_id: None,
            category_name: category.map(str::to_owned),
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

    fn monthly(
        start: Date,
        count: usize,
        amount: impl Fn(usize) -> i64,
        partner: impl Fn(usize) -> String,
        jitter: impl Fn(usize) -> i64,
    ) -> Vec<TransactionItem> {
        (0..count)
            .map(|index| {
                let day = add_months(start, index as i32);
                let day = day.checked_add(Duration::days(jitter(index))).unwrap();
                transaction(index as i64 + 1, day, amount(index), &partner(index), None, None)
            })
            .collect()
    }

    fn paypal(id: i64, day: Date, minor: i64, shop: Option<&str>) -> TransactionItem {
        let purpose = match shop {
            Some(shop) => format!("PP.1234.PP . {shop}, Ihr Einkauf bei {shop}"),
            None => "PP.1234.PP . Lastschrift".to_owned(),
        };
        transaction(
            id,
            day,
            minor,
            "PayPal Europe S.a.r.l. et Cie S.C.A",
            Some(&purpose),
            None,
        )
    }

    #[test]
    fn detects_subscriptions_salary_and_varying_bills() {
        let mut transactions = Vec::new();
        transactions.extend(monthly(
            date!(2026 - 04 - 15),
            6,
            |_| -1299,
            |index| format!("NETFLIX.COM {}", 1000 + index),
            |_| 0,
        ));
        transactions.extend(monthly(
            date!(2026 - 04 - 30),
            6,
            |_| 320_000,
            |_| "Employer GmbH".to_owned(),
            |index| if index % 2 == 0 { 0 } else { -2 },
        ));
        let power = [-6120, -5890, -6400, -6010, -5950, -6230];
        transactions.extend(monthly(
            date!(2026 - 04 - 05),
            6,
            |index| power[index],
            |_| "Stadtwerke Köln".to_owned(),
            |_| 0,
        ));
        transactions.push(transaction(
            100,
            date!(2025 - 11 - 01),
            -24_000,
            "HUK Coburg",
            None,
            None,
        ));
        transactions.push(transaction(
            101,
            date!(2024 - 11 - 02),
            -24_000,
            "HUK Coburg",
            None,
            None,
        ));

        let found = RecurringDetector::detect(&transactions, today());
        let by_prefix = |prefix: &str| found.iter().find(|payment| payment.name.starts_with(prefix)).unwrap();

        let netflix = by_prefix("NETFLIX.COM");
        assert_eq!(netflix.cadence, Cadence::Monthly);
        assert!(netflix.fixed_amount);
        assert_eq!(netflix.next_date, date!(2026 - 10 - 15));
        assert_eq!(netflix.yearly_amount(), money(-15588));

        assert!(by_prefix("Employer").is_income());
        assert!(!by_prefix("Stadtwerke").fixed_amount);
        assert_eq!(by_prefix("Stadtwerke").cadence, Cadence::Monthly);
        assert_eq!(by_prefix("HUK").cadence, Cadence::Yearly);
        assert_eq!(by_prefix("HUK").next_date, date!(2026 - 11 - 01));
    }

    #[test]
    fn irregular_too_rare_and_cancelled_bookings_are_not_recurring() {
        let groceries: Vec<TransactionItem> = [1, 3, 9, 10, 22, 40, 41, 70]
            .iter()
            .enumerate()
            .map(|(index, days)| {
                transaction(
                    index as i64 + 1,
                    today().checked_sub(Duration::days(*days)).unwrap(),
                    -2300,
                    "REWE",
                    None,
                    None,
                )
            })
            .collect();
        let twice = monthly(date!(2026 - 08 - 01), 2, |_| -999, |_| "Spotify".to_owned(), |_| 0);
        let cancelled = monthly(date!(2026 - 01 - 10), 4, |_| -799, |_| "Disney Plus".to_owned(), |_| 0);

        let names: Vec<String> = RecurringDetector::detect(
            &groceries.into_iter().chain(twice).chain(cancelled).collect::<Vec<_>>(),
            today(),
        )
        .into_iter()
        .map(|payment| payment.name)
        .collect();

        assert!(!names.contains(&"REWE".to_owned()));
        assert!(
            !names.contains(&"Spotify".to_owned()),
            "two monthly bookings aren't enough"
        );
        assert!(!names.contains(&"Disney Plus".to_owned()), "stopped months ago");
    }

    #[test]
    fn a_late_booking_stays_due_and_upcoming_lists_the_next_30_days() {
        let mut transactions = monthly(date!(2026 - 05 - 01), 5, |_| 300_000, |_| "Employer".to_owned(), |_| 0);
        transactions.extend(monthly(
            date!(2026 - 05 - 20),
            5,
            |_| -90_000,
            |_| "Vermieter".to_owned(),
            |_| 0,
        ));
        transactions.push(transaction(100, date!(2025 - 03 - 01), -10_000, "ADAC", None, None));
        transactions.push(transaction(101, date!(2024 - 03 - 01), -10_000, "ADAC", None, None));

        let recurring = RecurringDetector::detect(&transactions, today());
        assert_eq!(
            recurring
                .iter()
                .find(|payment| payment.name == "Employer")
                .unwrap()
                .next_date,
            date!(2026 - 10 - 01)
        );

        let upcoming: Vec<String> = RecurringDetector::upcoming(&recurring, today(), 30)
            .into_iter()
            .map(|payment| payment.name)
            .collect();
        assert_eq!(upcoming, vec!["Employer", "Vermieter"]);
    }

    #[test]
    fn a_paypal_subscription_is_detected_by_its_shop_despite_other_purchases() {
        let mut transactions: Vec<TransactionItem> = (0..5)
            .map(|index| {
                let day = add_months(date!(2026 - 05 - 10), index);
                paypal(index as i64 + 1, day, -999, Some("Beispiel Abo GmbH"))
            })
            .collect();
        for (index, days) in [63, 40, 30, 2].into_iter().enumerate() {
            transactions.push(paypal(
                index as i64 + 20,
                today().checked_sub(Duration::days(days)).unwrap(),
                [-2400, -5900, -3100, -1800][index],
                Some("Beispiel Shop GmbH"),
            ));
        }
        for (index, days) in [80, 45, 20].into_iter().enumerate() {
            transactions.push(paypal(
                index as i64 + 30,
                today().checked_sub(Duration::days(days)).unwrap(),
                [-1200, -4400, -700][index],
                Some("Anderer Shop GmbH"),
            ));
        }
        for (index, days) in [70, 10].into_iter().enumerate() {
            transactions.push(paypal(
                index as i64 + 40,
                today().checked_sub(Duration::days(days)).unwrap(),
                [-500, -600][index],
                Some("Dritter Laden GmbH"),
            ));
        }

        let found = RecurringDetector::detect(&transactions, today());

        let abo = found.single();
        assert_eq!(abo.name, "PayPal · Beispiel Abo GmbH");
        assert_eq!(abo.cadence, Cadence::Monthly);
        assert_eq!(abo.occurrences, 5);
        assert_eq!(abo.typical_amount, money(-999));
    }

    #[test]
    fn a_partner_named_like_a_paypal_shop_stays_a_separate_payment() {
        let mut transactions: Vec<TransactionItem> = (0..5)
            .map(|index| {
                let day = add_months(date!(2026 - 05 - 10), index);
                paypal(index as i64 + 1, day, -999, Some("Beispiel Abo GmbH"))
            })
            .collect();
        transactions.extend(monthly(
            date!(2026 - 05 - 12),
            5,
            |_| -999,
            |_| "Beispiel Abo GmbH".to_owned(),
            |_| 0,
        ));

        let found = RecurringDetector::detect(&transactions, today());
        let mut names: Vec<String> = found.iter().map(|payment| payment.name.clone()).collect();
        names.sort();
        assert_eq!(names, vec!["Beispiel Abo GmbH", "PayPal · Beispiel Abo GmbH"]);
        assert!(found.iter().all(|payment| payment.occurrences == 5));
    }

    #[test]
    fn filters_and_recurring_payments_are_per_currency() {
        let usd = Currency::new("USD").unwrap();
        let mut transactions = Vec::new();
        for month in 6..=9 {
            transactions.push(transaction(
                month,
                date!(2026 - 06 - 05)
                    .checked_add(Duration::days(30 * (month - 6)))
                    .unwrap(),
                -999,
                "Spotify",
                None,
                None,
            ));
            let mut dollar = transaction(
                month + 10,
                date!(2026 - 06 - 05)
                    .checked_add(Duration::days(30 * (month - 6)))
                    .unwrap(),
                -1199,
                "Spotify",
                None,
                None,
            );
            dollar.amount.currency = usd;
            dollar.currency = usd;
            transactions.push(dollar);
        }

        let recurring = RecurringDetector::detect(&transactions, date!(2026 - 09 - 20));
        let payments: std::collections::BTreeSet<(Currency, i64)> = recurring
            .iter()
            .map(|payment| (payment.currency, payment.typical_amount.minor))
            .collect();

        assert_eq!(
            payments,
            std::collections::BTreeSet::from([(Currency::EUR, -999), (usd, -1199)]),
            "two recurring payments, not one with a mixed median"
        );
    }

    #[test]
    fn paypal_payments_without_a_shop_keep_grouping_under_the_partner() {
        let payments: Vec<TransactionItem> = (0..5)
            .map(|index| {
                let day = add_months(date!(2026 - 05 - 10), index);
                paypal(index as i64 + 1, day, -450, None)
            })
            .collect();

        let recurring = RecurringDetector::detect(&payments, today());
        let payment = recurring.single();

        assert_eq!(payment.name, "PayPal Europe S.a.r.l. et Cie S.C.A");
        assert_eq!(payment.cadence, Cadence::Monthly);
        assert_eq!(payment.occurrences, 5);
    }

    trait Single<T> {
        fn single(self) -> T;
    }

    impl<T> Single<T> for Vec<T> {
        fn single(mut self) -> T {
            assert_eq!(self.len(), 1, "expected exactly one element");
            self.pop().unwrap()
        }
    }
}
