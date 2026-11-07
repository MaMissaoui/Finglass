//! The UI-free part of the live profile switch.
//!
//! A switch always goes through the target profile's unlock window. The order matters: prepare the target first, then
//! record the switch in the profile that is still open, then remember the target. Nothing is torn down here; the app
//! shell does that after this function has returned a [`startup::Startup`].

use thiserror::Error;
use time::OffsetDateTime;

use crate::app::startup::{self, Startup, StartupError};
use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::i18n::{self, Key, Language};
use crate::paths::AppPaths;
use crate::profiles::{self, ProfileError};

/// Everything that can go wrong while switching profiles.
///
/// No variant carries a passphrase or any other secret.
#[derive(Debug, Error)]
pub enum SwitchError {
    /// The target is the profile that is already open.
    #[error("the target profile is already open")]
    SameProfile,

    /// No profile is registered under that slug.
    #[error("unknown profile")]
    UnknownProfile,

    /// Another background job or a switch is already running.
    #[error("another task is still running")]
    Busy,

    /// A profile registry or folder failure.
    #[error(transparent)]
    Profile(#[from] ProfileError),

    /// Preparing the target profile failed.
    #[error(transparent)]
    Startup(#[from] StartupError),

    /// Writing the audit entry failed.
    #[error(transparent)]
    Db(#[from] DbError),
}

impl SwitchError {
    /// The localized text the Settings page shows for a refused switch.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        let key = match self {
            Self::SameProfile | Self::UnknownProfile => Key::ErrorUnexpected,
            Self::Busy => Key::ProfileSwitchBusy,
            Self::Profile(_) | Self::Startup(_) | Self::Db(_) => Key::ErrorUnexpected,
        };
        i18n::text(language, key).to_owned()
    }
}

/// Prepares `to`, records the switch in the old database and makes `to` the active profile.
///
/// `from` and `to` are profile slugs (`None` is the default profile). Nothing is torn down here: the caller closes the
/// old profile only after this returns successfully.
pub fn begin_switch(
    db: &Database,
    paths: &AppPaths,
    from: Option<&str>,
    to: Option<&str>,
    now: OffsetDateTime,
) -> Result<Startup, SwitchError> {
    if to == from {
        return Err(SwitchError::SameProfile);
    }

    if let Some(slug) = to {
        let known = profiles::list(paths)?
            .iter()
            .any(|profile| profile.slug.as_deref() == Some(slug));
        if !known {
            return Err(SwitchError::UnknownProfile);
        }
    }

    // The staged restore only moves the target folder's database files and documents, so the old database may stay
    // open while this runs. If it fails, the old profile stays open and nothing has been recorded.
    let startup = startup::prepare_profile(paths, to, now)?;

    // The entry belongs to the database being left, while it is still the open one. Slugs only: names and paths are
    // private and do not belong in the audit log.
    audit::log(
        db,
        "PROFILE_SWITCH",
        Actor::User,
        &format!("Switched from profile {} to {}", startup_slug(from), startup_slug(to)),
    )?;

    profiles::set_active(paths, to)?;

    Ok(startup)
}

fn startup_slug(slug: Option<&str>) -> &str {
    slug.unwrap_or("default")
}

#[cfg(test)]
mod tests {
    use std::fs;

    use time::macros::datetime;

    use super::*;
    use crate::backup::BackupService;
    use crate::model::{AccountId, AccountType, NewAccount};
    use crate::money::{Currency, Money};
    use crate::repo::accounts;
    use crate::secret::{Passphrase, PassphrasePurpose};

    const PASS: &str = "correct horse battery staple";

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    fn database(paths: &AppPaths, slug: &str) -> Database {
        let passphrase = Passphrase::new(PASS, PassphrasePurpose::New).unwrap();
        Database::create(&paths.database_file(slug).unwrap(), &passphrase).unwrap()
    }

    fn account(db: &Database) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: None,
                account_number: None,
                currency: Currency::EUR,
                account_type: AccountType::Checking,
                balance: Money {
                    minor: 0,
                    currency: Currency::EUR,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn switch_entries(db: &Database) -> Vec<String> {
        audit::recent(db, 100)
            .unwrap()
            .into_iter()
            .filter(|entry| entry.action == "PROFILE_SWITCH")
            .map(|entry| entry.details)
            .collect()
    }

    #[test]
    fn a_switch_writes_one_entry_into_the_old_database_and_activates_the_target() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let old = database(&paths, "default");
        let target = profiles::create(&paths, "Business GmbH").unwrap();
        let slug = target.slug.clone().unwrap();
        assert!(profiles::active(&paths).unwrap().slug.is_none());

        let startup = begin_switch(&old, &paths, None, Some(&slug), datetime!(2026 - 10 - 15 12:34:56 UTC)).unwrap();

        assert_eq!(startup.slug.as_deref(), Some(slug.as_str()));
        assert_eq!(startup.restored_from, None);
        assert_eq!(profiles::active(&paths).unwrap().slug.as_deref(), Some(slug.as_str()));

        let entries = switch_entries(&old);
        assert_eq!(entries.len(), 1, "{entries:?}");
        assert!(
            entries[0].contains("default") && entries[0].contains(&slug),
            "the entry names both slugs: {}",
            entries[0]
        );
        assert!(
            !entries[0].contains("Business GmbH"),
            "the display name is not audited: {}",
            entries[0]
        );
        assert!(
            !entries[0].contains(temp.path().to_string_lossy().as_ref()),
            "the path is not audited: {}",
            entries[0]
        );
    }

    #[test]
    fn a_staged_restore_in_the_target_folder_is_applied_before_the_switch_finishes() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let old = database(&paths, "default");
        let target = profiles::create(&paths, "Business").unwrap();
        let slug = target.slug.clone().unwrap();

        let target_db = database(&paths, &slug);
        let account_id = account(&target_db);
        let service = BackupService::new(profiles::data_dir(&paths, Some(&slug)).unwrap());
        let backup = temp.path().join("target.zip");
        service.create_backup(&target_db, &backup).unwrap();
        service.stage_restore(&target_db, &backup, None).unwrap();
        drop(target_db);

        let startup = begin_switch(&old, &paths, None, Some(&slug), datetime!(2026 - 10 - 15 12:34:56 UTC)).unwrap();

        assert_eq!(startup.slug.as_deref(), Some(slug.as_str()));
        let previous = startup.restored_from.expect("the target's restore moved data aside");
        assert_eq!(previous.parent(), Some(target.dir.as_path()));
        assert!(!service.has_pending_restore(), "nothing waits for the next start");
        assert_eq!(switch_entries(&old).len(), 1);
        let _ = account_id;
    }

    #[test]
    fn an_unknown_target_is_refused_and_nothing_changes() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let old = database(&paths, "default");

        let error = begin_switch(&old, &paths, None, Some("missing"), OffsetDateTime::now_utc()).unwrap_err();

        assert!(
            matches!(error, SwitchError::UnknownProfile),
            "unexpected error: {error:?}"
        );
        assert!(profiles::active(&paths).unwrap().slug.is_none());
        assert!(switch_entries(&old).is_empty());
    }

    #[test]
    fn switching_to_the_open_profile_is_refused_and_nothing_changes() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let old = database(&paths, "default");

        let error = begin_switch(&old, &paths, None, None, OffsetDateTime::now_utc()).unwrap_err();

        assert!(matches!(error, SwitchError::SameProfile), "unexpected error: {error:?}");
        assert!(profiles::active(&paths).unwrap().slug.is_none());
        assert!(switch_entries(&old).is_empty());
    }

    #[test]
    fn a_target_whose_restore_cannot_be_applied_is_refused_and_nothing_changes() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let old = database(&paths, "default");
        let target = profiles::create(&paths, "Business").unwrap();
        let slug = target.slug.clone().unwrap();

        let target_db = database(&paths, &slug);
        let service = BackupService::new(profiles::data_dir(&paths, Some(&slug)).unwrap());
        let backup = temp.path().join("target.zip");
        service.create_backup(&target_db, &backup).unwrap();
        service.stage_restore(&target_db, &backup, None).unwrap();
        drop(target_db);

        fs::write(target.dir.join("pre-restore-20261015-123456"), b"not a folder").unwrap();

        let error = begin_switch(&old, &paths, None, Some(&slug), datetime!(2026 - 10 - 15 12:34:56 UTC)).unwrap_err();

        assert!(matches!(error, SwitchError::Startup(_)), "unexpected error: {error:?}");
        assert!(
            profiles::active(&paths).unwrap().slug.is_none(),
            "the old profile stays active"
        );
        assert!(switch_entries(&old).is_empty(), "nothing was audited");
        assert!(service.has_pending_restore(), "the staged restore is untouched");
    }
}
