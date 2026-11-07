//! The account panel's stored state and group actions, shared by the Accounts and Transactions views.
//!
//! This is the UI-free port of FinDesk's `AccountPanelSupport.kt`: one selection and one set of collapsed groups per
//! profile, the group actions with their audit entries, and the free-text account filter. Nothing here is specific to
//! the Accounts page; the Transactions view (M4c) reuses it.

use std::collections::BTreeSet;

use thiserror::Error;

use crate::account_grouping::{AccountGroupView, GroupKey};
use crate::db::{Database, DbError};
use crate::i18n::Key;
use crate::model::{AccountGroupId, AccountId, NewAccountGroup};
use crate::repo::{self, RepoError};
use crate::settings;

/// The settings key holding the panel's selection, as FinDesk stores it.
pub const SELECTION_KEY: &str = "transactions.selection";
/// The settings key holding the panel's collapsed groups, as FinDesk stores it.
pub const COLLAPSED_KEY: &str = "accountGroups.collapsed";
/// Length of FinDesk's account-group name column.
pub const MAX_GROUP_NAME_LENGTH: usize = 100;

/// What the account panel filters by.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum AccountSelection {
    /// Every account.
    All,
    /// One built-in or custom group.
    Group(GroupKey),
    /// One account.
    Account(AccountId),
}

/// Why a panel action failed.
///
/// User-visible validation failures carry their [`Key`]; callers render them in their own language. Repository and
/// database failures stay errors and are not shown verbatim.
#[derive(Debug, Error)]
pub enum AppError {
    /// A user-visible message that the caller should render from the i18n table.
    #[error("user-visible message")]
    Message(Key),
    /// A repository failure.
    #[error(transparent)]
    Repo(#[from] RepoError),
    /// A settings/database failure.
    #[error(transparent)]
    Db(#[from] DbError),
}

/// Reads the stored selection, falling back to [`AccountSelection::All`] when it is absent or names a group or
/// account that no longer exists.
pub fn load_selection(
    db: &Database,
    groups: &[AccountGroupView],
    accounts: &[crate::account_grouping::AccountItem],
) -> Result<AccountSelection, AppError> {
    let Some(raw) = settings::get_setting(db, SELECTION_KEY)? else {
        return Ok(AccountSelection::All);
    };

    if let Some(key_id) = raw.strip_prefix("group:") {
        let key = if let Some(account_type) = key_id.strip_prefix("type:") {
            Some(GroupKey::Type(account_type.to_owned()))
        } else if let Some(group_id) = key_id.strip_prefix("custom:") {
            group_id
                .parse::<i64>()
                .ok()
                .map(|group_id| GroupKey::Custom(AccountGroupId(group_id)))
        } else {
            None
        };
        return Ok(match key {
            Some(key) if groups.iter().any(|group| group.key.id() == key.id()) => AccountSelection::Group(key),
            _ => AccountSelection::All,
        });
    }

    if let Some(id) = raw.strip_prefix("account:") {
        return Ok(match id.parse::<i64>() {
            Ok(id) if accounts.iter().any(|account| account.id == AccountId(id)) => {
                AccountSelection::Account(AccountId(id))
            }
            _ => AccountSelection::All,
        });
    }

    Ok(AccountSelection::All)
}

/// Stores the selection for both screens.
pub fn save_selection(db: &Database, selection: &AccountSelection) -> Result<(), AppError> {
    settings::set_setting(db, SELECTION_KEY, &selection_value(selection))?;
    Ok(())
}

/// Stores the selection for both screens, under the name the task uses.
pub fn store_selection(db: &Database, selection: &AccountSelection) -> Result<(), AppError> {
    save_selection(db, selection)
}

/// Falls back to All when the selected account or group no longer exists.
pub fn resolve(
    selection: &AccountSelection,
    groups: &[AccountGroupView],
    accounts: &[crate::account_grouping::AccountItem],
) -> AccountSelection {
    match selection {
        AccountSelection::All => AccountSelection::All,
        AccountSelection::Account(id) => {
            if accounts.iter().any(|account| account.id == *id) {
                selection.clone()
            } else {
                AccountSelection::All
            }
        }
        AccountSelection::Group(key) => {
            if groups.iter().any(|group| group.key.id() == key.id()) {
                selection.clone()
            } else {
                AccountSelection::All
            }
        }
    }
}

/// The stored text of a selection.
pub fn selection_value(selection: &AccountSelection) -> String {
    match selection {
        AccountSelection::All => "all".to_owned(),
        AccountSelection::Group(key) => format!("group:{}", key.id()),
        AccountSelection::Account(id) => format!("account:{}", id.0),
    }
}

/// The collapsed panel groups stored for this profile.
pub fn load_collapsed(db: &Database) -> Result<BTreeSet<String>, AppError> {
    let Some(raw) = settings::get_setting(db, COLLAPSED_KEY)? else {
        return Ok(BTreeSet::new());
    };
    Ok(raw
        .split(',')
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .collect())
}

/// Stores the collapsed panel groups, sorted and comma-separated like FinDesk.
pub fn save_collapsed(db: &Database, collapsed: &BTreeSet<String>) -> Result<(), AppError> {
    let value = collapsed.iter().cloned().collect::<Vec<_>>().join(",");
    settings::set_setting(db, COLLAPSED_KEY, &value)?;
    Ok(())
}

/// The account IDs [`selection`] shows, or `None` for every account.
///
/// An empty set is an empty custom group, or a selection whose group no longer has accounts: it shows nothing rather
/// than everything.
pub fn visible_accounts(selection: &AccountSelection, groups: &[AccountGroupView]) -> Option<BTreeSet<AccountId>> {
    match selection {
        AccountSelection::All => None,
        AccountSelection::Account(id) => Some(BTreeSet::from([*id])),
        AccountSelection::Group(key) => Some(
            groups
                .iter()
                .find(|group| group.key.id() == key.id())
                .map(|group| group.accounts.iter().map(|account| account.id).collect())
                .unwrap_or_default(),
        ),
    }
}

/// Whether an account row matches the panel's free-text filter.
///
/// FinDesk searches the display name, the identifier and the bank name, case-insensitively; an empty query matches.
pub fn matches_filter(name: Option<&str>, identifier: &str, bank_name: &str, query: &str) -> bool {
    let query = query.trim();
    if query.is_empty() {
        return true;
    }
    let query = query.to_lowercase();
    name.unwrap_or_default().to_lowercase().contains(&query)
        || identifier.to_lowercase().contains(&query)
        || bank_name.to_lowercase().contains(&query)
}

/// The account panel's group actions.
///
/// Each call is synchronous and takes the database; the caller refreshes its state afterwards. Every mutation goes
/// through the repository layer, which writes exactly one audit entry in the same transaction.
#[derive(Debug, Default, Clone, Copy)]
pub struct AccountGroupActions;

impl AccountGroupActions {
    /// Builds the action set.
    #[must_use]
    pub fn new() -> Self {
        Self
    }

    /// Creates a group after the existing ones. A blank name is a user-visible error.
    pub fn create(&self, db: &Database, name: &str) -> Result<AccountGroupId, AppError> {
        let name = normalize_group_name(name)?;
        let sort_order = repo::account_groups::list(db)?
            .into_iter()
            .map(|group| group.sort_order)
            .max()
            .unwrap_or(0)
            + 1;
        Ok(repo::account_groups::create(db, NewAccountGroup { name, sort_order })?)
    }

    /// Renames a group. A blank name is a user-visible error; an unknown id changes nothing.
    pub fn rename(&self, db: &Database, id: AccountGroupId, name: &str) -> Result<(), AppError> {
        let name = normalize_group_name(name)?;
        if !repo::account_groups::list(db)?.iter().any(|group| group.id == id) {
            return Ok(());
        }
        repo::account_groups::rename(db, id, &name)?;
        Ok(())
    }

    /// Deletes a group; its accounts return to their type groups.
    ///
    /// Returns `true` when `current` was that group. The stored selection is then reset to All, and the caller shows
    /// All too, exactly like FinDesk.
    pub fn delete(&self, db: &Database, id: AccountGroupId, current: &AccountSelection) -> Result<bool, AppError> {
        repo::account_groups::delete(db, id)?;
        let was_selected = matches!(current, AccountSelection::Group(GroupKey::Custom(selected)) if *selected == id);
        if was_selected {
            save_selection(db, &AccountSelection::All)?;
        }
        Ok(was_selected)
    }

    /// Moves a group up or down. At either end this is a no-op, but it still writes FinDesk's audit entry.
    pub fn move_up_or_down(&self, db: &Database, id: AccountGroupId, up: bool) -> Result<bool, AppError> {
        Ok(repo::account_groups::move_group(db, id, up)?)
    }

    /// Moves an account into a custom group, or back to its type group with `None`.
    pub fn move_account(
        &self,
        db: &Database,
        account_id: AccountId,
        group_id: Option<AccountGroupId>,
    ) -> Result<(), AppError> {
        repo::accounts::set_group(db, account_id, group_id)?;
        Ok(())
    }

    /// Marks the bookings behind `selection` as read: All clears everything, a group its accounts, an account its own.
    ///
    /// Like FinDesk's action, this is view state; the repository's existing `mark_read` records the change.
    pub fn mark_read(
        &self,
        db: &Database,
        selection: &AccountSelection,
        groups: &[AccountGroupView],
    ) -> Result<(), AppError> {
        let account_ids = visible_accounts(selection, groups);
        repo::transactions::mark_read(db, account_ids.as_ref())?;
        Ok(())
    }
}

/// Trims and caps a group name, rejecting a blank one.
pub fn normalize_group_name(name: &str) -> Result<String, AppError> {
    let clean: String = name.trim().chars().take(MAX_GROUP_NAME_LENGTH).collect();
    if clean.is_empty() {
        return Err(AppError::Message(Key::ErrorNameRequired));
    }
    Ok(clean)
}

#[cfg(test)]
mod tests {
    use crate::account_grouping::{AccountItem, GroupKey, group};
    use crate::db::Database;
    use crate::model::{AccountGroup, AccountGroupId, AccountId};
    use crate::money::{Currency, Money};
    use crate::secret::{Passphrase, PassphrasePurpose};
    use crate::settings;

    use super::*;

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(id: i64, account_type: &str) -> AccountItem {
        AccountItem {
            id: AccountId(id),
            name: None,
            currency: Currency::EUR,
            balance: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            account_type: account_type.to_owned(),
            group_id: None,
            new_count: 0,
        }
    }

    fn custom_group(id: i64, name: &str) -> AccountGroup {
        AccountGroup {
            id: AccountGroupId(id),
            name: name.to_owned(),
            sort_order: id,
        }
    }

    #[test]
    fn selection_values_round_trip_like_findesk() {
        assert_eq!(selection_value(&AccountSelection::All), "all");
        assert_eq!(
            selection_value(&AccountSelection::Group(GroupKey::Type("savings".to_owned()))),
            "group:type:savings"
        );
        assert_eq!(
            selection_value(&AccountSelection::Group(GroupKey::Custom(AccountGroupId(7)))),
            "group:custom:7"
        );
        assert_eq!(selection_value(&AccountSelection::Account(AccountId(3))), "account:3");
    }

    #[test]
    fn a_stored_selection_loads_and_falls_back_when_its_target_is_gone() {
        let (_temp, db) = database();
        let accounts = vec![account(1, "checking")];
        let groups = group(&accounts, &[custom_group(7, "Reisen")], Currency::EUR);

        assert_eq!(
            load_selection(&db, &groups, &accounts).unwrap(),
            AccountSelection::All,
            "nothing stored means All"
        );

        for (stored, expected) in [
            ("all", AccountSelection::All),
            (
                "group:type:checking",
                AccountSelection::Group(GroupKey::Type("checking".to_owned())),
            ),
            (
                "group:custom:7",
                AccountSelection::Group(GroupKey::Custom(AccountGroupId(7))),
            ),
            ("account:1", AccountSelection::Account(AccountId(1))),
            ("group:type:depot", AccountSelection::All),
            ("group:custom:99", AccountSelection::All),
            ("account:2", AccountSelection::All),
            ("nonsense", AccountSelection::All),
        ] {
            settings::set_setting(&db, SELECTION_KEY, stored).unwrap();
            assert_eq!(load_selection(&db, &groups, &accounts).unwrap(), expected, "{stored}");
        }
    }

    #[test]
    fn saving_a_selection_stores_it_for_both_screens() {
        let (_temp, db) = database();
        let selection = AccountSelection::Group(GroupKey::Type("savings".to_owned()));
        save_selection(&db, &selection).unwrap();

        assert_eq!(
            settings::get_setting(&db, SELECTION_KEY).unwrap().as_deref(),
            Some("group:type:savings")
        );

        store_selection(&db, &AccountSelection::Account(AccountId(9))).unwrap();
        assert_eq!(
            settings::get_setting(&db, SELECTION_KEY).unwrap().as_deref(),
            Some("account:9")
        );
    }

    #[test]
    fn resolve_falls_back_to_all_for_a_deleted_account_or_group() {
        let accounts = vec![account(1, "checking")];
        let groups = group(&accounts, &[], Currency::EUR);
        assert_eq!(
            resolve(&AccountSelection::Account(AccountId(1)), &groups, &accounts),
            AccountSelection::Account(AccountId(1))
        );
        assert_eq!(
            resolve(&AccountSelection::Account(AccountId(2)), &groups, &accounts),
            AccountSelection::All
        );
        assert_eq!(
            resolve(
                &AccountSelection::Group(GroupKey::Type("checking".to_owned())),
                &groups,
                &accounts
            ),
            AccountSelection::Group(GroupKey::Type("checking".to_owned()))
        );
        assert_eq!(
            resolve(
                &AccountSelection::Group(GroupKey::Type("depot".to_owned())),
                &groups,
                &accounts
            ),
            AccountSelection::All
        );
    }

    #[test]
    fn visible_accounts_returns_none_for_all_and_nothing_for_a_vanished_group() {
        let accounts = vec![account(1, "checking"), account(2, "checking")];
        let groups = group(&accounts, &[custom_group(7, "Leer")], Currency::EUR);

        assert_eq!(visible_accounts(&AccountSelection::All, &groups), None);
        assert_eq!(
            visible_accounts(&AccountSelection::Account(AccountId(2)), &groups),
            Some(BTreeSet::from([AccountId(2)]))
        );
        assert_eq!(
            visible_accounts(&AccountSelection::Group(GroupKey::Type("checking".to_owned())), &groups),
            Some(BTreeSet::from([AccountId(1), AccountId(2)]))
        );
        assert_eq!(
            visible_accounts(&AccountSelection::Group(GroupKey::Custom(AccountGroupId(7))), &groups),
            Some(BTreeSet::new())
        );
        assert_eq!(
            visible_accounts(&AccountSelection::Group(GroupKey::Type("depot".to_owned())), &groups),
            Some(BTreeSet::new())
        );
    }

    #[test]
    fn collapsed_groups_round_trip_sorted_and_reject_empty_pieces() {
        let (_temp, db) = database();
        assert!(load_collapsed(&db).unwrap().is_empty());

        settings::set_setting(&db, COLLAPSED_KEY, " type:b ,, custom:7 ").unwrap();
        assert_eq!(
            load_collapsed(&db).unwrap(),
            BTreeSet::from(["custom:7".to_owned(), "type:b".to_owned()])
        );

        let collapsed = BTreeSet::from(["type:z".to_owned(), "type:a".to_owned()]);
        save_collapsed(&db, &collapsed).unwrap();
        assert_eq!(
            settings::get_setting(&db, COLLAPSED_KEY).unwrap().as_deref(),
            Some("type:a,type:z")
        );
    }

    #[test]
    fn the_account_filter_searches_name_identifier_and_bank() {
        assert!(matches_filter(Some("Haushalt"), "DE89", "Test Bank", ""));
        assert!(matches_filter(Some("Haushalt"), "DE89", "Test Bank", "haus"));
        assert!(matches_filter(None, "DE89370400440532013000", "Test Bank", "3201"));
        assert!(matches_filter(None, "CARD4321", "Test Bank", "test"));
        assert!(!matches_filter(None, "CARD4321", "Test Bank", "reisen"));
    }
}

#[cfg(test)]
mod name_tests {
    use super::*;

    #[test]
    fn group_names_are_trimmed_capped_and_never_blank() {
        assert_eq!(normalize_group_name("  Reisen  ").unwrap(), "Reisen");
        assert_eq!(
            normalize_group_name(&"x".repeat(150)).unwrap().chars().count(),
            MAX_GROUP_NAME_LENGTH
        );
        assert!(matches!(
            normalize_group_name("   "),
            Err(AppError::Message(Key::ErrorNameRequired))
        ));
    }
}
