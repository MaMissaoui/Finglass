//! The account balance after each booking, reconstructed backwards from the account's current balance.

use std::collections::BTreeMap;

use time::Date;

use crate::model::{TransactionId, TransactionItem};
use crate::money::Money;

/// The reconstructed balance of one booking.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    /// The balance after the booking.
    pub balance: Money,
    /// The bank balance minus the computed balance, when they differ.
    pub bank_difference: Option<Money>,
}

/// Reconstructs the running balance of one account.
pub struct RunningBalance;

impl RunningBalance {
    /// The balance after each booking of one account, by transaction id.
    ///
    /// `newest_first` is every booking of the account in list order (date descending, id descending);
    /// `current_balance` is the account's balance after all bookings, future-dated ones included.
    pub fn compute(current_balance: Money, newest_first: &[TransactionItem]) -> BTreeMap<TransactionId, Entry> {
        let currency = current_balance.currency;
        let mut result = BTreeMap::new();
        let mut end_of_day = current_balance.minor;

        // Encounter order of the date-descending list: days newest first, each day's rows in list order.
        let mut days: Vec<(Date, Vec<&TransactionItem>)> = Vec::new();
        for transaction in newest_first {
            match days.last_mut() {
                Some((date, rows)) if *date == transaction.date => rows.push(transaction),
                _ => days.push((transaction.date, vec![transaction])),
            }
        }

        for (_, rows) in days {
            let start_of_day = rows
                .iter()
                .fold(end_of_day, |balance, transaction| balance - transaction.amount.minor);

            let has_bank = rows
                .iter()
                .filter(|transaction| transaction.balance_after.is_some())
                .count();
            let reconciles = if has_bank == 0 {
                true
            } else if has_bank == rows.len() {
                rows.iter().any(|transaction| {
                    transaction
                        .balance_after
                        .is_some_and(|balance| balance.minor == end_of_day)
                }) && rows.iter().any(|transaction| {
                    transaction
                        .balance_after
                        .is_some_and(|balance| balance.minor - transaction.amount.minor == start_of_day)
                })
            } else {
                rows.iter().any(|transaction| {
                    transaction
                        .balance_after
                        .is_some_and(|balance| balance.minor == end_of_day)
                }) || rows.iter().any(|transaction| {
                    transaction
                        .balance_after
                        .is_some_and(|balance| balance.minor - transaction.amount.minor == start_of_day)
                })
            };

            let mut balance = end_of_day;
            if reconciles {
                for transaction in rows {
                    result.insert(
                        transaction.id,
                        Entry {
                            balance: Money {
                                minor: balance,
                                currency,
                            },
                            bank_difference: None,
                        },
                    );
                    balance -= transaction.amount.minor;
                }
            } else {
                for transaction in rows {
                    let computed = balance;
                    if let Some(bank) = transaction.balance_after {
                        let difference = (bank.minor != computed).then(|| Money {
                            minor: bank.minor - computed,
                            currency,
                        });
                        result.insert(
                            transaction.id,
                            Entry {
                                balance: Money {
                                    minor: bank.minor,
                                    currency,
                                },
                                bank_difference: difference,
                            },
                        );
                        balance = bank.minor;
                    } else {
                        result.insert(
                            transaction.id,
                            Entry {
                                balance: Money {
                                    minor: computed,
                                    currency,
                                },
                                bank_difference: None,
                            },
                        );
                    }
                    balance -= transaction.amount.minor;
                }
            }

            end_of_day = balance;
        }

        result
    }
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::model::AccountId;
    use crate::money::Currency;

    fn money(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
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
            currency: Currency::EUR,
            partner_name: None,
            purpose: None,
            category_id: None,
            category_name: None,
            category_color: None,
            is_tax_relevant: false,
            document_count: 0,
            is_new: false,
            is_transfer: false,
            tax_source: crate::model::TaxSource::None,
            tax_rule_pattern: None,
            tax_override: None,
        }
    }

    #[test]
    fn newest_row_equals_the_current_balance_and_every_row_chains_to_the_older_one() {
        let newest = txn(3, date!(2026 - 03 - 03), -3000, None);
        let middle = txn(2, date!(2026 - 03 - 02), 10_000, None);
        let oldest = txn(1, date!(2026 - 03 - 01), -5000, None);

        let entries = RunningBalance::compute(money(12_000), &[newest.clone(), middle.clone(), oldest.clone()]);

        assert_eq!(entries[&newest.id].balance, money(12_000));
        assert_eq!(entries[&middle.id].balance, money(15_000));
        assert_eq!(entries[&oldest.id].balance, money(5000));
        assert_eq!(
            entries[&middle.id].balance.minor,
            entries[&newest.id].balance.minor - newest.amount.minor
        );
        assert_eq!(
            entries[&oldest.id].balance.minor,
            entries[&middle.id].balance.minor - middle.amount.minor
        );
        assert!(entries.values().all(|entry| entry.bank_difference.is_none()));
    }

    #[test]
    fn same_day_bookings_chain_by_id_order() {
        let oldest = txn(1, date!(2026 - 03 - 01), -1000, None);
        let middle = txn(2, date!(2026 - 03 - 01), -2000, None);
        let newest = txn(3, date!(2026 - 03 - 01), -3000, None);

        let entries = RunningBalance::compute(money(4000), &[newest.clone(), middle.clone(), oldest.clone()]);

        assert_eq!(entries[&newest.id].balance, money(4000));
        assert_eq!(entries[&middle.id].balance, money(7000));
        assert_eq!(entries[&oldest.id].balance, money(9000));
    }

    #[test]
    fn a_future_dated_booking_is_the_newest_row_and_still_starts_at_the_current_balance() {
        let future = txn(2, date!(2026 - 12 - 31), -5000, None);
        let today = txn(1, date!(2026 - 03 - 01), 2000, None);

        let entries = RunningBalance::compute(money(20_000), &[future.clone(), today.clone()]);

        assert_eq!(
            entries[&future.id].balance,
            money(20_000),
            "the account balance includes future bookings"
        );
        assert_eq!(entries[&today.id].balance, money(25_000));
    }

    #[test]
    fn a_bank_balance_is_preferred_and_only_its_own_row_is_marked() {
        let newest = txn(2, date!(2026 - 03 - 03), -3000, Some(10_000));
        let older = txn(1, date!(2026 - 03 - 02), 5000, None);

        let entries = RunningBalance::compute(money(15_000), &[newest.clone(), older.clone()]);

        assert_eq!(entries[&newest.id].balance, money(10_000));
        assert_eq!(entries[&newest.id].bank_difference, Some(money(-5000)));
        assert_eq!(entries[&older.id].balance, money(13_000));
        assert_eq!(entries[&older.id].bank_difference, None);
    }

    #[test]
    fn an_equal_bank_balance_is_not_a_mismatch() {
        let newest = txn(1, date!(2026 - 03 - 03), -3000, Some(15_050));

        let entries = RunningBalance::compute(money(15_050), std::slice::from_ref(&newest));

        assert_eq!(entries[&newest.id].balance, money(15_050));
        assert_eq!(entries[&newest.id].bank_difference, None);
    }

    #[test]
    fn without_bank_values_the_per_day_balances_match_balance_history() {
        let d1 = date!(2026 - 03 - 01);
        let d2 = date!(2026 - 03 - 02);
        let d3 = date!(2026 - 03 - 03);
        let newest_first = vec![
            txn(4, d3, -3000, None),
            txn(3, d3, -2000, None),
            txn(2, d2, 10_000, None),
            txn(1, d1, -5000, None),
        ];
        let current = money(12_000);
        let entries = RunningBalance::compute(current, &newest_first);

        for day in [d1, d2, d3] {
            let newest_that_day = newest_first.iter().find(|transaction| transaction.date == day).unwrap();
            let end_of_day = crate::balance::BalanceHistory::compute(current, &newest_first, day, day)[0].1;
            assert_eq!(entries[&newest_that_day.id].balance, end_of_day, "end of {day}");
        }
    }

    #[test]
    fn a_day_whose_list_order_differs_from_the_banks_booking_order_is_placed_arithmetically() {
        let sep30 = date!(2026 - 09 - 30);
        let oct1 = date!(2026 - 10 - 01);
        let minus1000 = txn(1, sep30, -100_000, Some(300_000));
        let minus500 = txn(2, oct1, -50_000, Some(248_000));
        let minus700 = txn(3, oct1, -70_000, Some(178_000));
        let minus20 = txn(4, oct1, -2000, Some(298_000));
        let newest_first = vec![minus20.clone(), minus700.clone(), minus500.clone(), minus1000.clone()];

        let entries = RunningBalance::compute(money(178_000), &newest_first);

        assert!(entries.values().all(|entry| entry.bank_difference.is_none()));
        assert_eq!(entries[&minus20.id].balance, money(178_000));
        assert_eq!(entries[&minus700.id].balance, money(180_000));
        assert_eq!(entries[&minus500.id].balance, money(250_000));
        assert_eq!(entries[&minus1000.id].balance, money(300_000));
        assert_eq!(
            crate::balance::BalanceHistory::from_running(&newest_first, &entries, sep30, oct1),
            vec![(sep30, money(300_000)), (oct1, money(178_000))]
        );
    }

    #[test]
    fn a_day_reconciles_from_its_start_balance_even_when_its_end_balance_matches_no_bank_value() {
        let sep30 = date!(2026 - 09 - 30);
        let oct1 = date!(2026 - 10 - 01);
        let minus1000 = txn(1, sep30, -100_000, Some(300_000));
        let minus500 = txn(2, oct1, -50_000, Some(248_000));
        let minus700 = txn(3, oct1, -70_000, Some(178_000));
        let minus20 = txn(4, oct1, -2000, Some(298_000));
        let plus100 = txn(5, oct1, 10_000, None);

        let entries = RunningBalance::compute(
            money(188_000),
            &[
                plus100.clone(),
                minus20.clone(),
                minus700.clone(),
                minus500.clone(),
                minus1000.clone(),
            ],
        );

        assert!(entries.values().all(|entry| entry.bank_difference.is_none()));
        assert_eq!(entries[&plus100.id].balance, money(188_000));
        assert_eq!(entries[&minus20.id].balance, money(178_000));
        assert_eq!(entries[&minus700.id].balance, money(180_000));
        assert_eq!(entries[&minus500.id].balance, money(250_000));
        assert_eq!(entries[&minus1000.id].balance, money(300_000));
    }

    #[test]
    fn a_day_reconciles_when_a_bank_value_equals_its_end_balance() {
        let sep30 = date!(2026 - 09 - 30);
        let oct1 = date!(2026 - 10 - 01);
        let minus1000 = txn(1, sep30, -100_000, Some(300_000));
        let plus100 = txn(2, oct1, 10_000, None);
        let minus500 = txn(3, oct1, -50_000, Some(258_000));
        let minus700 = txn(4, oct1, -70_000, Some(188_000));
        let minus20 = txn(5, oct1, -2000, Some(308_000));

        let entries = RunningBalance::compute(
            money(188_000),
            &[
                minus20.clone(),
                minus700.clone(),
                minus500.clone(),
                plus100.clone(),
                minus1000.clone(),
            ],
        );

        assert!(entries.values().all(|entry| entry.bank_difference.is_none()));
        assert_eq!(entries[&minus1000.id].balance, money(300_000));
    }

    #[test]
    fn a_fully_bank_covered_day_with_a_real_gap_no_longer_reconciles_from_one_matching_end() {
        let sep30 = date!(2026 - 09 - 30);
        let oct1 = date!(2026 - 10 - 01);
        let minus1000 = txn(1, sep30, -100_000, Some(300_000));
        let minus700 = txn(2, oct1, -70_000, Some(178_000));
        let minus20 = txn(3, oct1, -2000, Some(298_000));

        let entries = RunningBalance::compute(money(178_000), &[minus20.clone(), minus700.clone(), minus1000.clone()]);

        assert!(
            [&minus20, &minus700]
                .iter()
                .any(|transaction| entries[&transaction.id].bank_difference.is_some()),
            "at least one 10-01 row is flagged with a bank difference"
        );
        let sum: i64 = entries
            .values()
            .map(|entry| entry.bank_difference.map(|difference| difference.minor).unwrap_or(0))
            .sum();
        assert_eq!(sum, 50_000);
    }

    #[test]
    fn a_fully_bank_covered_day_where_only_the_end_matches_keeps_a_bank_difference() {
        let minus20 = txn(2, date!(2026 - 10 - 01), -2000, Some(15_000));
        let minus10 = txn(1, date!(2026 - 10 - 01), -1000, Some(10_000));

        let entries = RunningBalance::compute(money(10_000), &[minus10, minus20]);

        assert!(entries.values().any(|entry| entry.bank_difference.is_some()));
    }
}
