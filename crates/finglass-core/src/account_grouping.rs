//! Account grouping and balance tones, ported from the non-UI part of FinDesk's `util/AccountGrouping.kt`.
//!
//! The Compose geometry (drag targets with `Offset` and `Rect`) has no counterpart here; only grouping, ordering,
//! totals and balance tones are part of the core.

use std::collections::{BTreeMap, HashMap};

use crate::model::{AccountGroup, AccountGroupId, AccountId};
use crate::money::{Currency, Money};

/// The account fields the grouping needs, with the unread-booking counter the account panel shows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountItem {
    /// The row id.
    pub id: AccountId,
    /// The user's own label, when set.
    pub name: Option<String>,
    /// The account currency.
    pub currency: Currency,
    /// The latest known balance.
    pub balance: Money,
    /// The account type text ("checking", "savings", "credit_card", "loan", "depot", or another type).
    pub account_type: String,
    /// The custom group the account belongs to; `None` keeps it in its account-type group.
    pub group_id: Option<AccountGroupId>,
    /// Bookings a bank sync brought in that the user has not read yet.
    pub new_count: i64,
}

/// What an [`AccountGroupView`] stands for: a built-in account-type group or one of the user's custom groups.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum GroupKey {
    /// A built-in account-type group.
    Type(String),
    /// A custom group.
    Custom(AccountGroupId),
}

impl GroupKey {
    /// The stable id used for the persisted selection and the collapsed state.
    pub fn id(&self) -> String {
        match self {
            Self::Type(account_type) => format!("type:{account_type}"),
            Self::Custom(group_id) => format!("custom:{}", group_id.0),
        }
    }
}

/// One entry in the account panel: its key, an optional custom name and the accounts with their per-currency totals.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountGroupView {
    /// The group key.
    pub key: GroupKey,
    /// The custom group's name, or `None` for a type group (the UI localizes those).
    pub name: Option<String>,
    /// The accounts in the group, in input order.
    pub accounts: Vec<AccountItem>,
    /// Balances per currency, primary first. Different currencies are never added up.
    pub totals: Vec<(Currency, i64)>,
    /// Sum of the accounts' unread bookings, for the panel's counter badge.
    pub new_count: i64,
}

/// The account type of a loan, whose balance is negative by nature.
pub const LOAN_TYPE: &str = "loan";

/// The account types FinDesk offers, in the order their groups are shown.
pub const ACCOUNT_TYPES: [&str; 5] = ["checking", "savings", "credit_card", LOAN_TYPE, "depot"];

/// Puts every account into its custom group when [`AccountItem::group_id`] names an existing group, and into its
/// account-type group otherwise (an unknown group id falls back to the type).
///
/// Type groups with no accounts are left out; custom groups are always shown so an empty one can receive accounts.
/// Accounts inside a group keep the input order, which is the repository order.
pub fn group(accounts: &[AccountItem], groups: &[AccountGroup], primary: Currency) -> Vec<AccountGroupView> {
    let known: HashMap<AccountGroupId, &AccountGroup> = groups.iter().map(|group| (group.id, group)).collect();
    let mut custom_accounts: HashMap<AccountGroupId, Vec<AccountItem>> =
        groups.iter().map(|group| (group.id, Vec::new())).collect();
    let mut type_accounts: HashMap<String, Vec<AccountItem>> = HashMap::new();

    for account in accounts {
        match account.group_id {
            Some(group_id) if known.contains_key(&group_id) => custom_accounts
                .get_mut(&group_id)
                .expect("every known group is initialised")
                .push(account.clone()),
            _ => type_accounts
                .entry(account.account_type.clone())
                .or_default()
                .push(account.clone()),
        }
    }

    let mut type_keys: Vec<String> = ACCOUNT_TYPES
        .iter()
        .map(|account_type| (*account_type).to_owned())
        .collect();
    let mut unknown: Vec<&String> = type_accounts
        .keys()
        .filter(|account_type| !ACCOUNT_TYPES.contains(&account_type.as_str()))
        .collect();
    unknown.sort();
    type_keys.extend(unknown.into_iter().cloned());

    let mut views = Vec::new();
    for account_type in type_keys {
        let accounts_in_group = type_accounts.remove(&account_type).unwrap_or_default();
        if accounts_in_group.is_empty() {
            continue;
        }
        let totals = totals(&accounts_in_group, primary);
        let new_count = accounts_in_group.iter().map(|account| account.new_count).sum();
        views.push(AccountGroupView {
            key: GroupKey::Type(account_type),
            name: None,
            accounts: accounts_in_group,
            totals,
            new_count,
        });
    }

    for custom in groups {
        let accounts_in_group = custom_accounts
            .remove(&custom.id)
            .expect("every custom group is initialised");
        let totals = totals(&accounts_in_group, primary);
        let new_count = accounts_in_group.iter().map(|account| account.new_count).sum();
        views.push(AccountGroupView {
            key: GroupKey::Custom(custom.id),
            name: Some(custom.name.clone()),
            accounts: accounts_in_group,
            totals,
            new_count,
        });
    }

    views
}

/// Balances per currency, primary first; different currencies are never added up.
pub fn grand_total(accounts: &[AccountItem], primary: Currency) -> Vec<(Currency, i64)> {
    totals(accounts, primary)
}

/// Balances per currency, primary first.
fn totals(accounts: &[AccountItem], primary: Currency) -> Vec<(Currency, i64)> {
    let mut sums: BTreeMap<Currency, i64> = BTreeMap::new();
    for account in accounts {
        *sums.entry(account.currency).or_default() += account.balance.minor;
    }

    let mut entries: Vec<(Currency, i64)> = sums.into_iter().collect();
    entries
        .sort_by(|(left, _), (right, _)| (left != &primary, left.as_str()).cmp(&(right != &primary, right.as_str())));
    entries
}

/// Whether a total of `accounts`' balances in `currency` warns red.
///
/// The total is below zero **and** the same total without the loan accounts is also below zero. A loan is negative by
/// nature, so red there is noise and a total negative only because of loans stays in the plain text colour. Red is
/// kept for overdrawn checking accounts and credit cards that are due. One account reduces to the one-account rule:
/// red unless it is a loan's.
pub fn is_warning_balance(accounts: &[AccountItem], currency: Currency) -> bool {
    let in_currency: Vec<&AccountItem> = accounts.iter().filter(|account| account.currency == currency).collect();
    if in_currency.iter().map(|account| account.balance.minor).sum::<i64>() >= 0 {
        return false;
    }

    let without_loans: i64 = in_currency
        .iter()
        .filter(|account| account.account_type != LOAN_TYPE)
        .map(|account| account.balance.minor)
        .sum();
    without_loans < 0
}

/// How an account balance or a total of balances is coloured.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BalanceTone {
    /// Above zero.
    Positive,
    /// Below zero and a warning.
    Negative,
    /// Below zero only because of loans.
    Loan,
    /// Exactly zero.
    Zero,
}

/// Tone of `accounts`' total in `currency`; the red/grey split follows [`is_warning_balance`].
pub fn balance_tone(accounts: &[AccountItem], currency: Currency) -> BalanceTone {
    let total: i64 = accounts
        .iter()
        .filter(|account| account.currency == currency)
        .map(|account| account.balance.minor)
        .sum();
    if total > 0 {
        BalanceTone::Positive
    } else if total == 0 {
        BalanceTone::Zero
    } else if is_warning_balance(accounts, currency) {
        BalanceTone::Negative
    } else {
        BalanceTone::Loan
    }
}

/// Tone of one balance that is not a list of accounts (a single account's running balance, a dashboard figure).
pub fn balance_tone_value(value: Money, is_loan: bool) -> BalanceTone {
    if value.minor > 0 {
        BalanceTone::Positive
    } else if value.minor == 0 {
        BalanceTone::Zero
    } else if is_loan {
        BalanceTone::Loan
    } else {
        BalanceTone::Negative
    }
}

/// The custom group `key` stands for, or `None` for a type group.
pub fn group_id_for(key: &GroupKey) -> Option<AccountGroupId> {
    match key {
        GroupKey::Custom(group_id) => Some(*group_id),
        GroupKey::Type(_) => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn currency(code: &str) -> Currency {
        Currency::new(code).unwrap()
    }

    fn account(
        id: i64,
        account_type: &str,
        currency: Currency,
        balance: i64,
        group_id: Option<i64>,
        new_count: i64,
    ) -> AccountItem {
        AccountItem {
            id: AccountId(id),
            name: Some(format!("Account {id}")),
            currency,
            balance: Money {
                minor: balance,
                currency,
            },
            account_type: account_type.to_owned(),
            group_id: group_id.map(AccountGroupId),
            new_count,
        }
    }

    fn simple(id: i64, account_type: &str) -> AccountItem {
        account(id, account_type, Currency::EUR, 0, None, 0)
    }

    fn custom_group(id: i64, name: &str) -> AccountGroup {
        AccountGroup {
            id: AccountGroupId(id),
            name: name.to_owned(),
            sort_order: id,
        }
    }

    #[test]
    fn groups_by_type_in_the_fixed_order() {
        let accounts = vec![
            simple(1, "depot"),
            simple(2, "checking"),
            simple(3, "savings"),
            simple(4, "credit_card"),
        ];
        let views = group(&accounts, &[], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![
                GroupKey::Type("checking".to_owned()),
                GroupKey::Type("savings".to_owned()),
                GroupKey::Type("credit_card".to_owned()),
                GroupKey::Type("depot".to_owned()),
            ]
        );
        assert_eq!(
            views[0].accounts.iter().map(|account| account.id.0).collect::<Vec<_>>(),
            vec![2]
        );
    }

    #[test]
    fn a_loan_group_sorts_after_credit_card_and_before_depot() {
        let accounts = vec![
            simple(1, "depot"),
            simple(2, "loan"),
            simple(3, "credit_card"),
            simple(4, "checking"),
            simple(5, "savings"),
        ];
        let views = group(&accounts, &[], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![
                GroupKey::Type("checking".to_owned()),
                GroupKey::Type("savings".to_owned()),
                GroupKey::Type("credit_card".to_owned()),
                GroupKey::Type("loan".to_owned()),
                GroupKey::Type("depot".to_owned()),
            ]
        );
        assert_eq!(
            views
                .iter()
                .find(|view| view.key == GroupKey::Type("loan".to_owned()))
                .unwrap()
                .accounts
                .iter()
                .map(|account| account.id.0)
                .collect::<Vec<_>>(),
            vec![2]
        );
    }

    #[test]
    fn an_unknown_type_follows_the_fixed_ones_alphabetically() {
        let accounts = vec![simple(1, "checking"), simple(2, "zebra"), simple(3, "apple")];
        let views = group(&accounts, &[], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![
                GroupKey::Type("checking".to_owned()),
                GroupKey::Type("apple".to_owned()),
                GroupKey::Type("zebra".to_owned()),
            ]
        );
    }

    #[test]
    fn a_custom_group_overrides_the_account_type() {
        let accounts = vec![account(1, "savings", Currency::EUR, 0, Some(10), 0)];
        let views = group(&accounts, &[custom_group(10, "Reisen")], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![GroupKey::Custom(AccountGroupId(10))]
        );
        assert_eq!(views[0].name.as_deref(), Some("Reisen"));
        assert_eq!(
            views[0].accounts.iter().map(|account| account.id.0).collect::<Vec<_>>(),
            vec![1]
        );
        assert!(!views.iter().any(|view| matches!(view.key, GroupKey::Type(_))));
    }

    #[test]
    fn an_unknown_group_id_falls_back_to_the_type() {
        let accounts = vec![account(1, "checking", Currency::EUR, 0, Some(99), 0)];
        let views = group(&accounts, &[custom_group(10, "Reisen")], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![
                GroupKey::Type("checking".to_owned()),
                GroupKey::Custom(AccountGroupId(10)),
            ]
        );
        assert_eq!(
            views[0].accounts.iter().map(|account| account.id.0).collect::<Vec<_>>(),
            vec![1]
        );
        assert!(views[1].accounts.is_empty(), "the custom group stays empty");
    }

    #[test]
    fn empty_type_groups_are_hidden_but_empty_custom_groups_are_shown() {
        let accounts = vec![simple(1, "checking")];
        let views = group(&accounts, &[custom_group(10, "Reisen")], Currency::EUR);
        assert_eq!(
            views.iter().map(|view| view.key.clone()).collect::<Vec<_>>(),
            vec![
                GroupKey::Type("checking".to_owned()),
                GroupKey::Custom(AccountGroupId(10)),
            ]
        );
        assert!(
            views
                .iter()
                .find(|view| view.key == GroupKey::Custom(AccountGroupId(10)))
                .unwrap()
                .accounts
                .is_empty(),
            "an empty custom group can receive accounts"
        );
    }

    #[test]
    fn mixed_currency_totals_are_never_summed_and_the_primary_currency_comes_first() {
        let accounts = vec![
            account(1, "checking", currency("USD"), 5_000, None, 0),
            account(2, "checking", currency("EUR"), 10_000, None, 0),
            account(3, "depot", currency("EUR"), 200_000, None, 0),
        ];
        let views = group(&accounts, &[], Currency::EUR);
        let checking = views
            .iter()
            .find(|view| view.key == GroupKey::Type("checking".to_owned()))
            .unwrap();
        assert_eq!(
            checking
                .totals
                .iter()
                .map(|(currency, _)| *currency)
                .collect::<Vec<_>>(),
            vec![Currency::EUR, currency("USD")],
            "primary first"
        );
        assert_eq!(checking.totals[0].1, 10_000);
        assert_eq!(checking.totals[1].1, 5_000);
        assert_eq!(checking.totals.len(), 2, "EUR and USD are never added up");

        let depot = views
            .iter()
            .find(|view| view.key == GroupKey::Type("depot".to_owned()))
            .unwrap();
        assert_eq!(depot.totals[0].1, 200_000, "a depot is included");
    }

    #[test]
    fn the_grand_total_sums_every_account_per_currency() {
        let accounts = vec![
            account(1, "checking", currency("EUR"), 10_000, None, 0),
            account(2, "savings", currency("EUR"), 2_500, None, 0),
            account(3, "depot", currency("USD"), 1_000, None, 0),
        ];
        let total = grand_total(&accounts, Currency::EUR);
        assert_eq!(
            total.iter().map(|(currency, _)| *currency).collect::<Vec<_>>(),
            vec![Currency::EUR, currency("USD")]
        );
        assert_eq!(total[0].1, 12_500);
        assert_eq!(total[1].1, 1_000);
        assert!(grand_total(&[], Currency::EUR).is_empty());
    }

    #[test]
    fn group_key_ids_are_stable() {
        assert_eq!(GroupKey::Type("checking".to_owned()).id(), "type:checking");
        assert_eq!(GroupKey::Custom(AccountGroupId(7)).id(), "custom:7");
    }

    #[test]
    fn a_groups_new_count_is_the_sum_of_its_accounts() {
        let accounts = vec![
            account(1, "checking", Currency::EUR, 0, None, 2),
            account(2, "checking", Currency::EUR, 0, None, 3),
            simple(3, "savings"),
        ];
        let views = group(&accounts, &[], Currency::EUR);
        assert_eq!(
            views
                .iter()
                .find(|view| view.key == GroupKey::Type("checking".to_owned()))
                .unwrap()
                .new_count,
            5
        );
        assert_eq!(
            views
                .iter()
                .find(|view| view.key == GroupKey::Type("savings".to_owned()))
                .unwrap()
                .new_count,
            0,
            "nothing new stays zero"
        );
    }

    #[test]
    fn a_custom_groups_new_count_sums_the_accounts_moved_into_it() {
        let accounts = vec![
            account(1, "savings", Currency::EUR, 0, Some(10), 4),
            account(2, "savings", Currency::EUR, 0, Some(10), 1),
        ];
        let views = group(&accounts, &[custom_group(10, "Reisen")], Currency::EUR);
        assert_eq!(views[0].new_count, 5);
    }

    #[test]
    fn one_account_follows_the_sign_and_the_loan_rule() {
        assert_eq!(
            balance_tone(&[account(1, "checking", Currency::EUR, 500, None, 0)], Currency::EUR),
            BalanceTone::Positive
        );
        assert_eq!(
            balance_tone(&[account(1, "checking", Currency::EUR, 0, None, 0)], Currency::EUR),
            BalanceTone::Zero
        );
        assert_eq!(
            balance_tone(&[account(1, "checking", Currency::EUR, -1_000, None, 0)], Currency::EUR),
            BalanceTone::Negative
        );
        assert_eq!(
            balance_tone(
                &[account(1, LOAN_TYPE, Currency::EUR, -9_000_000, None, 0)],
                Currency::EUR
            ),
            BalanceTone::Loan
        );
    }

    #[test]
    fn a_total_negative_only_because_of_loans_is_a_loan_tone() {
        let accounts = vec![
            account(1, "checking", Currency::EUR, 245_000, None, 0),
            account(2, LOAN_TYPE, Currency::EUR, -9_650_000, None, 0),
        ];
        assert_eq!(balance_tone(&accounts, Currency::EUR), BalanceTone::Loan);
    }

    #[test]
    fn an_overdrawn_total_stays_a_warning_even_with_loans_in_it() {
        let accounts = vec![
            account(1, "checking", Currency::EUR, -10_000, None, 0),
            account(2, LOAN_TYPE, Currency::EUR, -100_000, None, 0),
        ];
        assert_eq!(balance_tone(&accounts, Currency::EUR), BalanceTone::Negative);
    }

    #[test]
    fn a_positive_total_outweighing_a_loan_is_positive() {
        let accounts = vec![
            account(1, "checking", Currency::EUR, 10_000, None, 0),
            account(2, LOAN_TYPE, Currency::EUR, -5_000, None, 0),
        ];
        assert_eq!(balance_tone(&accounts, Currency::EUR), BalanceTone::Positive);
    }

    #[test]
    fn currencies_are_judged_separately() {
        let accounts = vec![
            account(1, "checking", Currency::EUR, -1_000, None, 0),
            account(2, "checking", currency("USD"), 10_000, None, 0),
        ];
        assert_eq!(
            balance_tone(&accounts, Currency::EUR),
            BalanceTone::Negative,
            "the euro total ignores the positive dollar account"
        );
        assert_eq!(
            balance_tone(&accounts, currency("USD")),
            BalanceTone::Positive,
            "the dollar total ignores the negative euro account"
        );
    }

    #[test]
    fn one_balance_has_all_four_tones() {
        assert_eq!(
            balance_tone_value(
                Money {
                    minor: 500,
                    currency: Currency::EUR
                },
                false
            ),
            BalanceTone::Positive
        );
        assert_eq!(
            balance_tone_value(
                Money {
                    minor: 0,
                    currency: Currency::EUR
                },
                false
            ),
            BalanceTone::Zero
        );
        assert_eq!(
            balance_tone_value(
                Money {
                    minor: -1_000,
                    currency: Currency::EUR
                },
                false
            ),
            BalanceTone::Negative
        );
        assert_eq!(
            balance_tone_value(
                Money {
                    minor: -9_000_000,
                    currency: Currency::EUR
                },
                true
            ),
            BalanceTone::Loan
        );
    }

    #[test]
    fn a_negative_list_tone_is_exactly_the_warning_flag() {
        let fixtures = vec![
            vec![account(1, "checking", Currency::EUR, -1_000, None, 0)],
            vec![account(1, LOAN_TYPE, Currency::EUR, -9_000_000, None, 0)],
            vec![
                account(1, "checking", Currency::EUR, 50_000, None, 0),
                account(2, LOAN_TYPE, Currency::EUR, -9_000_000, None, 0),
            ],
            vec![
                account(1, "checking", Currency::EUR, -2_000, None, 0),
                account(2, LOAN_TYPE, Currency::EUR, -9_000_000, None, 0),
            ],
            vec![
                account(1, "credit_card", Currency::EUR, -30_000, None, 0),
                account(2, "savings", Currency::EUR, 100_000, None, 0),
                account(3, LOAN_TYPE, Currency::EUR, -5_000_000, None, 0),
            ],
            vec![account(1, "checking", Currency::EUR, 500, None, 0)],
            vec![account(1, "checking", Currency::EUR, 0, None, 0)],
        ];
        for accounts in fixtures {
            assert_eq!(
                is_warning_balance(&accounts, Currency::EUR),
                balance_tone(&accounts, Currency::EUR) == BalanceTone::Negative,
                "tone and warning flag disagree for {accounts:?}"
            );
        }
    }

    #[test]
    fn a_warning_free_negative_total_is_never_a_warning() {
        let loan = vec![account(1, LOAN_TYPE, Currency::EUR, -100, None, 0)];
        assert!(!is_warning_balance(&loan, Currency::EUR));
        assert_ne!(balance_tone(&loan, Currency::EUR), BalanceTone::Negative);
    }

    #[test]
    fn group_id_for_maps_a_custom_group_to_its_id_and_a_type_group_to_none() {
        assert_eq!(
            group_id_for(&GroupKey::Custom(AccountGroupId(10))),
            Some(AccountGroupId(10))
        );
        assert_eq!(group_id_for(&GroupKey::Type("checking".to_owned())), None);
    }
}
