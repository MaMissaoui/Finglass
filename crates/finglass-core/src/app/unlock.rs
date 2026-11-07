//! Unlocking, creating and rekeying the encrypted database.
//!
//! Finglass has no plaintext databases, so there is no "encrypt plaintext" mode: a profile is either new or already
//! encrypted.

use std::path::Path;

use thiserror::Error;
use unicode_normalization::UnicodeNormalization;
use zeroize::Zeroizing;

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::paths::AppPaths;
use crate::secret::{Passphrase, PassphraseError, PassphrasePurpose};

/// What the unlock window should offer for a profile.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UnlockMode {
    /// No database yet: choose a passphrase and create one.
    Create,
    /// An encrypted database exists: enter its passphrase.
    Unlock,
    /// An open database: replace its passphrase.
    Change,
}

/// Why an unlock action failed.
///
/// No variant carries the passphrase or any other secret.
#[derive(Debug, Error, PartialEq, Eq)]
pub enum UnlockError {
    /// The passphrase is wrong (or the database is damaged).
    #[error("wrong passphrase")]
    WrongPassphrase,

    /// A new passphrase had fewer than the minimum number of characters.
    #[error("passphrase is too short")]
    TooShort,

    /// A new passphrase starts or ends with whitespace.
    #[error("passphrase has surrounding spaces")]
    Spaces,

    /// The two new passphrases differ.
    #[error("passphrases do not match")]
    Mismatch,

    /// The user did not confirm that a forgotten passphrase cannot be recovered.
    #[error("recovery was not acknowledged")]
    NotAcknowledged,

    /// The file is a plaintext SQLite database, which Finglass does not open.
    #[error("database is not encrypted")]
    NotEncrypted,

    /// The database was written by a newer version of Finglass.
    #[error("database is too new")]
    TooNew,

    /// Any other database, path or audit error.
    #[error("unlock failed")]
    Failed,
}

/// The mode for a database file: unlock it when it exists, otherwise create it.
#[must_use]
pub fn mode_for(db_file: &Path) -> UnlockMode {
    if db_file.exists() {
        UnlockMode::Unlock
    } else {
        UnlockMode::Create
    }
}

/// Validates a new passphrase in FinDesk's order: rules, then the repeat, then the acknowledgement.
///
/// Returns the normalized passphrase on success. The repeat is normalized for comparison but is not itself required
/// to satisfy the length rule; a short repeat is reported as a mismatch, exactly as in FinDesk.
pub fn validate_new(passphrase: &str, repeat: &str, acknowledged: bool) -> Result<Passphrase, UnlockError> {
    let passphrase = Passphrase::new(passphrase, PassphrasePurpose::New).map_err(|error| match error {
        PassphraseError::Empty | PassphraseError::TooShort { .. } => UnlockError::TooShort,
        PassphraseError::SurroundingWhitespace => UnlockError::Spaces,
    })?;

    let normalized_repeat: Zeroizing<String> = Zeroizing::new(repeat.nfc().collect());
    if passphrase.expose() != normalized_repeat.as_str() {
        return Err(UnlockError::Mismatch);
    }

    if !acknowledged {
        return Err(UnlockError::NotAcknowledged);
    }

    Ok(passphrase)
}

/// Opens an existing database.
pub fn unlock(paths: &AppPaths, slug: &str, input: &str) -> Result<Database, UnlockError> {
    let passphrase = Passphrase::new(input, PassphrasePurpose::Unlock).map_err(|error| match error {
        // An empty passphrase can never be correct for a database Finglass created.
        PassphraseError::Empty => UnlockError::WrongPassphrase,
        PassphraseError::TooShort { .. } | PassphraseError::SurroundingWhitespace => UnlockError::WrongPassphrase,
    })?;

    let path = paths.database_file(slug).map_err(|_| UnlockError::Failed)?;
    Database::open(&path, &passphrase).map_err(map_db_error)
}

/// Creates a new encrypted database for `slug`.
pub fn create(
    paths: &AppPaths,
    slug: &str,
    passphrase: &str,
    repeat: &str,
    acknowledged: bool,
) -> Result<Database, UnlockError> {
    let passphrase = validate_new(passphrase, repeat, acknowledged)?;

    let profile_dir = paths.profile_dir(slug).map_err(|_| UnlockError::Failed)?;
    AppPaths::ensure_dir(&profile_dir).map_err(|_| UnlockError::Failed)?;

    let path = paths.database_file(slug).map_err(|_| UnlockError::Failed)?;
    let db = Database::create(&path, &passphrase).map_err(map_db_error)?;

    audit::log(&db, "DATABASE_CREATED", Actor::User, "New encrypted database").map_err(|_| UnlockError::Failed)?;

    Ok(db)
}

/// Replaces the passphrase of an already open database.
///
/// The caller has opened the database with the current passphrase, so the current passphrase is not needed here.
pub fn change(db: &Database, passphrase: &str, repeat: &str, acknowledged: bool) -> Result<(), UnlockError> {
    let passphrase = validate_new(passphrase, repeat, acknowledged)?;

    db.change_passphrase(&passphrase).map_err(map_db_error)?;

    audit::log(
        db,
        "DATABASE_PASSPHRASE_CHANGED",
        Actor::User,
        "Database passphrase changed",
    )
    .map_err(|_| UnlockError::Failed)?;

    Ok(())
}

fn map_db_error(error: DbError) -> UnlockError {
    match error {
        DbError::WrongPassphraseOrDamaged => UnlockError::WrongPassphrase,
        DbError::NotEncrypted => UnlockError::NotEncrypted,
        DbError::TooNew { .. } => UnlockError::TooNew,
        _ => UnlockError::Failed,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::audit::recent;
    use crate::secret::PassphrasePurpose;

    const SLUG: &str = "default";
    const PASSPHRASE: &str = "correct horse battery staple";
    const NEW_PASSPHRASE: &str = "another very long passphrase";

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    fn direct_passphrase(value: &str) -> Passphrase {
        Passphrase::new(value, PassphrasePurpose::New).unwrap()
    }

    fn expect_err<T>(result: Result<T, UnlockError>) -> UnlockError {
        match result {
            Ok(_) => panic!("expected an error"),
            Err(error) => error,
        }
    }

    #[test]
    fn mode_depends_on_the_database_file() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let file = paths.database_file(SLUG).unwrap();

        assert_eq!(mode_for(&file), UnlockMode::Create);
        Database::create(&file, &direct_passphrase(PASSPHRASE)).unwrap();
        assert_eq!(mode_for(&file), UnlockMode::Unlock);
    }

    #[test]
    fn validate_new_reports_every_error() {
        assert_eq!(validate_new("short", "short", true).unwrap_err(), UnlockError::TooShort);
        assert_eq!(
            validate_new(" correct horse battery ", " correct horse battery ", true).unwrap_err(),
            UnlockError::Spaces
        );
        assert_eq!(
            validate_new(PASSPHRASE, NEW_PASSPHRASE, true).unwrap_err(),
            UnlockError::Mismatch
        );
        assert_eq!(
            validate_new(PASSPHRASE, PASSPHRASE, false).unwrap_err(),
            UnlockError::NotAcknowledged
        );
        assert!(validate_new(PASSPHRASE, PASSPHRASE, true).is_ok());
    }

    #[test]
    fn create_then_unlock_works_and_a_wrong_passphrase_is_reported() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let db = create(&paths, SLUG, PASSPHRASE, PASSPHRASE, true).unwrap();
        drop(db);

        let db = unlock(&paths, SLUG, PASSPHRASE).unwrap();
        drop(db);

        let error = expect_err(unlock(&paths, SLUG, "not the passphrase"));
        assert_eq!(error, UnlockError::WrongPassphrase);
    }

    #[test]
    fn change_then_reopen_with_the_new_passphrase() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let db = create(&paths, SLUG, PASSPHRASE, PASSPHRASE, true).unwrap();
        change(&db, NEW_PASSPHRASE, NEW_PASSPHRASE, true).unwrap();
        drop(db);

        assert_eq!(
            expect_err(unlock(&paths, SLUG, PASSPHRASE)),
            UnlockError::WrongPassphrase
        );
        let db = unlock(&paths, SLUG, NEW_PASSPHRASE).unwrap();
        drop(db);
    }

    #[test]
    fn audit_entries_exist_and_never_contain_a_passphrase() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let db = create(&paths, SLUG, PASSPHRASE, PASSPHRASE, true).unwrap();
        change(&db, NEW_PASSPHRASE, NEW_PASSPHRASE, true).unwrap();

        let entries = recent(&db, 10).unwrap();
        let actions: Vec<&str> = entries.iter().map(|entry| entry.action.as_str()).collect();
        assert!(actions.contains(&"DATABASE_CREATED"));
        assert!(actions.contains(&"DATABASE_PASSPHRASE_CHANGED"));

        for entry in entries {
            let text = format!("{} / {} / {:?}", entry.action, entry.details, entry.actor);
            assert!(!text.contains(PASSPHRASE), "audit entry leaked the passphrase");
            assert!(!text.contains(NEW_PASSPHRASE), "audit entry leaked the new passphrase");
        }
    }

    #[test]
    fn create_makes_the_profile_folder() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        create(&paths, "business", PASSPHRASE, PASSPHRASE, true).unwrap();

        assert!(paths.profile_dir("business").unwrap().is_dir());
    }
}
