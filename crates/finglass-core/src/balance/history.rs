//! End-of-day balance points for charts.

use std::collections::BTreeMap;

use time::{Date, Duration};

use crate::model::{TransactionId, TransactionItem};
use crate::money::Money;

use super::running::Entry;

/// Builds balance history from bookings and their running balances.
pub struct BalanceHistory;

impl BalanceHistory {
    /// End-of-day balances from `from` to `to`, reconstructed backwards from `current_balance`.
    ///
    /// Bookings dated after `to` are undone first.
    pub fn compute(
        current_balance: Money,
        transactions: &[TransactionItem],
        from: Date,
        to: Date,
    ) -> Vec<(Date, Money)> {
        if from > to {
            return Vec::new();
        }

        let mut by_day: BTreeMap<Date, i64> = BTreeMap::new();
        for transaction in transactions {
            *by_day.entry(transaction.date).or_default() += transaction.amount.minor;
        }
        let after_to: i64 = transactions
            .iter()
            .filter(|transaction| transaction.date > to)
            .map(|transaction| transaction.amount.minor)
            .sum();
        let mut balance = current_balance.minor - after_to;
        let currency = current_balance.currency;

        let mut points = Vec::new();
        let mut day = to;
        loop {
            points.push((
                day,
                Money {
                    minor: balance,
                    currency,
                },
            ));
            balance -= by_day.get(&day).copied().unwrap_or(0);
            if day == from {
                break;
            }
            day = day.previous_day().expect("date range underflow");
        }
        points.reverse();

        points
    }

    /// One balance point per day from `from` to `to`, oldest first, read from the running balances.
    ///
    /// `newest_first` is every booking of one account in list order (date descending, id descending);
    /// `running` is [`super::RunningBalance::compute`] for that same list.
    pub fn from_running(
        newest_first: &[TransactionItem],
        running: &BTreeMap<TransactionId, Entry>,
        from: Date,
        to: Date,
    ) -> Vec<(Date, Money)> {
        if from > to || newest_first.is_empty() {
            return Vec::new();
        }

        let ascending: Vec<&TransactionItem> = newest_first.iter().rev().collect();
        let oldest = ascending[0];
        let currency = oldest.amount.currency;
        let mut current = running
            .get(&oldest.id)
            .map(|entry| entry.balance.minor - oldest.amount.minor);
        let mut points = Vec::new();
        let mut index = 0;
        let mut day = from;

        loop {
            while index < ascending.len() && ascending[index].date <= day {
                if let Some(entry) = running.get(&ascending[index].id) {
                    current = Some(entry.balance.minor);
                }
                index += 1;
            }
            if let Some(balance) = current {
                points.push((
                    day,
                    Money {
                        minor: balance,
                        currency,
                    },
                ));
            }
            if day == to {
                break;
            }
            day = day.checked_add(Duration::days(1)).expect("date range overflow");
        }

        points
    }
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::balance::RunningBalance;
    use crate::model::{AccountId, TaxSource};

    fn money(minor: i64) -> Money {
        Money {
            minor,
            currency: crate::money::Currency::EUR,
        }
    }

    fn txn(id: i64, day: time::Date, minor: i64, balance_after: Option<i64>) -> TransactionItem {
        TransactionItem {
            id: TransactionId(id),
            account_id: AccountId(1),
            account_iban: Some("DE1".to_owned()),
            date: day,
            amount: money(minor),
            balance_after: balance_after.map(money),
            currency: crate::money::Currency::EUR,
            partner_name: None,
            purpose: None,
            category_id: None,
            category_name: None,
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

    fn chart(
        newest_first: &[TransactionItem],
        current_balance: i64,
        from: time::Date,
        to: time::Date,
    ) -> Vec<(time::Date, Money)> {
        let running = RunningBalance::compute(money(current_balance), newest_first);
        BalanceHistory::from_running(newest_first, &running, from, to)
    }

    #[test]
    fn days_between_bookings_carry_the_previous_value_and_several_bookings_on_one_day_give_the_last_balance() {
        let same_day_older = txn(1, date!(2026 - 03 - 03), -2000, None);
        let newest = txn(2, date!(2026 - 03 - 03), -1000, None);
        let oldest = txn(3, date!(2026 - 03 - 01), 5000, None);
        let points = chart(
            &[newest, same_day_older, oldest],
            10_000,
            date!(2026 - 03 - 01),
            date!(2026 - 03 - 04),
        );

        assert_eq!(
            points,
            vec![
                (date!(2026 - 03 - 01), money(13_000)),
                (date!(2026 - 03 - 02), money(13_000)),
                (date!(2026 - 03 - 03), money(10_000)),
                (date!(2026 - 03 - 04), money(10_000)),
            ]
        );
    }

    #[test]
    fn a_day_before_the_oldest_booking_gives_the_balance_before_it() {
        let newest = txn(2, date!(2026 - 03 - 03), -1000, None);
        let oldest = txn(1, date!(2026 - 03 - 01), 5000, None);
        let points = chart(&[newest, oldest], 10_000, date!(2026 - 02 - 27), date!(2026 - 03 - 02));

        assert_eq!(
            points,
            vec![
                (date!(2026 - 02 - 27), money(6000)),
                (date!(2026 - 02 - 28), money(6000)),
                (date!(2026 - 03 - 01), money(11_000)),
                (date!(2026 - 03 - 02), money(11_000)),
            ]
        );
    }

    #[test]
    fn a_booking_dated_after_to_does_not_change_the_values_up_to_to() {
        let future = txn(2, date!(2026 - 04 - 01), -5000, None);
        let today = txn(1, date!(2026 - 03 - 01), 2000, None);
        let list = [future, today];
        let entries = RunningBalance::compute(money(20_000), &list);
        let up_to = BalanceHistory::from_running(&list, &entries, date!(2026 - 03 - 01), date!(2026 - 03 - 05));

        assert_eq!(up_to.len(), 5);
        assert!(
            up_to.iter().all(|(_, balance)| balance.minor == 25_000),
            "the future booking is ignored before its own date"
        );

        let past = BalanceHistory::from_running(&list, &entries, date!(2026 - 04 - 01), date!(2026 - 04 - 01));
        assert_eq!(past, vec![(date!(2026 - 04 - 01), money(20_000))]);
    }

    #[test]
    fn a_bank_balance_wins_over_the_reconstructed_chain_same_as_the_column() {
        let newest = txn(2, date!(2026 - 03 - 03), -3000, Some(7000));
        let older = txn(1, date!(2026 - 03 - 02), 5000, None);
        let points = chart(&[newest, older], 10_000, date!(2026 - 03 - 02), date!(2026 - 03 - 03));

        assert_eq!(
            points,
            vec![
                (date!(2026 - 03 - 02), money(10_000)),
                (date!(2026 - 03 - 03), money(7000))
            ]
        );
    }

    #[test]
    fn no_bookings_and_a_from_after_to_give_an_empty_list() {
        assert!(
            BalanceHistory::from_running(&[], &BTreeMap::new(), date!(2026 - 03 - 01), date!(2026 - 03 - 31))
                .is_empty()
        );
        let transaction = txn(1, date!(2026 - 03 - 01), 1000, None);
        let entries = RunningBalance::compute(money(1000), std::slice::from_ref(&transaction));
        assert!(
            BalanceHistory::from_running(&[transaction], &entries, date!(2026 - 03 - 05), date!(2026 - 03 - 01))
                .is_empty()
        );
    }
}
