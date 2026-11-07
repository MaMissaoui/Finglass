//! Which profile opens, and the restore that must happen before the database does.
//!
//! FinDesk applies a staged restore in `main()` before the unlock gate looks at the database. Finglass does the same
//! here, so the rest of the shell never has to know whether a restore was pending. The profile's folder is created
//! before the restore is applied, because that folder is where the restore puts its `pre-restore-<timestamp>` data.

use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use thiserror::Error;
use time::OffsetDateTime;

use crate::backup::{BackupError, BackupService, PENDING_DIR, PRE_RESTORE_PREFIX};
use crate::i18n::{self, Key, Language};
use crate::paths::AppPaths;
use crate::profiles::{self, ProfileError};

/// The `&str` form of a profile slug, with `"default"` for the default profile.
///
/// [`profiles::Profile::slug`] is `None` for the default profile, while [`AppPaths::profile_dir`] and the unlock
/// functions take a slug with `"default"`. This is the one conversion between the two.
#[must_use]
pub fn slug_key(slug: Option<&str>) -> &str {
    slug.unwrap_or("default")
}

/// The profile opened at this start and the data a restore moved aside, if any.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Startup {
    /// The active profile's slug; `None` is the default profile.
    pub slug: Option<String>,
    /// The `pre-restore-<timestamp>` folder holding the data the restore replaced.
    pub restored_from: Option<PathBuf>,
}

/// Why a folder with unencrypted documents is still on disk.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LeftoverKind {
    /// The data a restore replaced.
    PreRestore,
    /// The data folder of a deleted profile.
    DeletedProfile,
    /// A staged restore waiting for the next start.
    PendingRestore,
}

/// A folder that still holds unencrypted documents.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Leftover {
    /// Where the folder came from.
    pub kind: LeftoverKind,
    /// The folder relative to the data root, with `/` separators, for display.
    pub path: String,
}

/// The folders under `paths` that still hold unencrypted documents, sorted by path.
///
/// Finglass never writes a plaintext database, but the folders it moves aside keep a `documents/` folder of
/// unencrypted files. Only a folder with at least one regular file under `documents/` is listed. Symlinks are never
/// followed and no file contents are read.
#[must_use]
pub fn leftovers(paths: &AppPaths) -> Vec<Leftover> {
    let root = paths.root();
    let mut found = Vec::new();

    // The default profile is the data root itself.
    scan_profile_dir(root, root, &mut found);

    let profiles_root = root.join(profiles::PROFILES_DIR);
    if is_real_dir(&profiles_root) {
        for dir in subdirectories(&profiles_root) {
            scan_profile_dir(&dir, root, &mut found);
        }
    }

    let deleted_root = root.join(profiles::DELETED_DIR);
    if is_real_dir(&deleted_root) {
        for dir in subdirectories(&deleted_root) {
            if has_documents(&dir) {
                found.push(Leftover {
                    kind: LeftoverKind::DeletedProfile,
                    path: relative_path(root, &dir),
                });
            }
        }
    }

    found.sort_by(|left, right| left.path.cmp(&right.path));
    found
}

fn scan_profile_dir(dir: &Path, root: &Path, found: &mut Vec<Leftover>) {
    let pending = dir.join(PENDING_DIR);
    if has_documents(&pending) {
        found.push(Leftover {
            kind: LeftoverKind::PendingRestore,
            path: relative_path(root, &pending),
        });
    }

    for folder in subdirectories(dir) {
        let starts_with_prefix = folder
            .file_name()
            .and_then(|name| name.to_str())
            .is_some_and(|name| name.starts_with(PRE_RESTORE_PREFIX));
        if starts_with_prefix && has_documents(&folder) {
            found.push(Leftover {
                kind: LeftoverKind::PreRestore,
                path: relative_path(root, &folder),
            });
        }
    }
}

/// The real subdirectories of `dir`, without following symlinks.
fn subdirectories(dir: &Path) -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    let Ok(entries) = fs::read_dir(dir) else {
        return dirs;
    };

    for entry in entries.flatten() {
        if entry.file_type().is_ok_and(|file_type| file_type.is_dir()) {
            dirs.push(entry.path());
        }
    }

    dirs
}

/// Whether `path` is a directory itself, not a symlink to one.
fn is_real_dir(path: &Path) -> bool {
    fs::symlink_metadata(path).is_ok_and(|metadata| metadata.file_type().is_dir())
}

/// Whether `dir/documents` holds at least one regular file, directly or in a real subfolder.
fn has_documents(dir: &Path) -> bool {
    has_regular_file(&dir.join("documents"))
}

fn has_regular_file(dir: &Path) -> bool {
    if !is_real_dir(dir) {
        return false;
    }
    let Ok(entries) = fs::read_dir(dir) else {
        return false;
    };

    for entry in entries.flatten() {
        let Ok(file_type) = entry.file_type() else {
            continue;
        };
        if file_type.is_file() {
            return true;
        }
        if file_type.is_dir() && has_regular_file(&entry.path()) {
            return true;
        }
    }

    false
}

/// `path` relative to `root` with `/` separators, for display.
fn relative_path(root: &Path, path: &Path) -> String {
    path.strip_prefix(root)
        .unwrap_or(path)
        .components()
        .map(|component| component.as_os_str().to_string_lossy())
        .collect::<Vec<_>>()
        .join("/")
}

/// Everything that can go wrong while preparing the startup profile.
///
/// No variant carries a passphrase.
#[derive(Debug, Error)]
pub enum StartupError {
    /// The profile could not be resolved.
    #[error(transparent)]
    Profile(#[from] ProfileError),

    /// The staged restore could not be applied.
    #[error(transparent)]
    Backup(#[from] BackupError),

    /// The profile folder could not be created.
    #[error(transparent)]
    Io(#[from] io::Error),
}

impl StartupError {
    /// The localized text shown by the unlock window when startup preparation fails.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        i18n::text(language, Key::ErrorStartupFailed).to_owned()
    }
}

/// Resolves the active profile and applies a staged restore before the database is opened.
///
/// A leftover migration staging folder is removed first: after a crash it can hold unencrypted document copies.
pub fn prepare(paths: &AppPaths, now: OffsetDateTime) -> Result<Startup, StartupError> {
    remove_migration_staging(paths);
    let profile = profiles::active(paths)?;
    prepare_profile(paths, profile.slug.as_deref(), now)
}

fn remove_migration_staging(paths: &AppPaths) {
    let staging = paths.root().join(crate::app::migration::STAGING_DIR);
    if fs::symlink_metadata(&staging).is_ok_and(|metadata| metadata.file_type().is_dir()) {
        let _ = fs::remove_dir_all(&staging);
    }
}

/// Prepares `slug` (or the default profile) and applies its staged restore.
///
/// This is also what a live profile switch will call (M8d-2).
pub fn prepare_profile(paths: &AppPaths, slug: Option<&str>, now: OffsetDateTime) -> Result<Startup, StartupError> {
    let dir = profiles::data_dir(paths, slug)?;
    AppPaths::ensure_dir(&dir)?;
    let restored_from = BackupService::new(dir).apply_pending_restore(now)?;

    Ok(Startup {
        slug: slug.map(str::to_owned),
        restored_from,
    })
}

#[cfg(test)]
mod tests {
    use std::fs;
    use std::path::Path;

    use time::macros::{date, datetime};

    use super::*;
    use crate::db::Database;
    use crate::model::{AccountId, AccountType, NewAccount, NewTransaction};
    use crate::money::{Currency, Money};
    use crate::repo::{accounts, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    const PASS: &str = "correct horse battery staple";

    fn passphrase() -> Passphrase {
        Passphrase::new(PASS, PassphrasePurpose::New).unwrap()
    }

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
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

    fn add_transaction(db: &Database, account_id: AccountId, partner: &str, hash: &str) {
        transactions::insert_imported(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: date!(2026 - 01 - 01),
                amount: Money {
                    minor: -1_000,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: Some(partner.to_owned()),
                purpose: None,
                category_id: None,
                import_hash: hash.to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
    }

    fn partner_names(db: &Database) -> Vec<String> {
        db.with_conn(|conn| {
            let mut statement = conn.prepare("SELECT partner_name FROM transactions ORDER BY id")?;
            let names = statement
                .query_map([], |row| row.get::<_, Option<String>>(0))?
                .collect::<Result<Vec<_>, _>>()?
                .into_iter()
                .flatten()
                .collect();
            Ok(names)
        })
        .unwrap()
    }

    fn pre_restore_folders(dir: &Path) -> Vec<PathBuf> {
        fs::read_dir(dir)
            .unwrap()
            .filter_map(Result::ok)
            .filter(|entry| {
                entry
                    .file_name()
                    .to_string_lossy()
                    .starts_with(crate::backup::PRE_RESTORE_PREFIX)
            })
            .map(|entry| entry.path())
            .collect()
    }

    fn folder_with_document(dir: &Path) {
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("receipt.pdf"), b"unencrypted").unwrap();
    }

    #[test]
    fn a_fresh_root_has_no_leftovers() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        assert!(leftovers(&paths).is_empty());
    }

    #[test]
    fn pre_restore_folders_with_documents_in_the_root_and_a_profile_are_listed() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        folder_with_document(&temp.path().join("pre-restore-20260101-000000"));

        let profile = profiles::create(&paths, "Business").unwrap();
        let slug = profile.slug.clone().unwrap();
        folder_with_document(&profile.dir.join("pre-restore-20260102-000000"));

        assert_eq!(
            leftovers(&paths),
            vec![
                Leftover {
                    kind: LeftoverKind::PreRestore,
                    path: "pre-restore-20260101-000000".to_owned(),
                },
                Leftover {
                    kind: LeftoverKind::PreRestore,
                    path: format!("profiles/{slug}/pre-restore-20260102-000000"),
                },
            ]
        );
    }

    #[test]
    fn a_deleted_profile_with_a_document_is_listed() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        folder_with_document(&temp.path().join("profiles-deleted/business-20260101-000000"));

        assert_eq!(
            leftovers(&paths),
            vec![Leftover {
                kind: LeftoverKind::DeletedProfile,
                path: "profiles-deleted/business-20260101-000000".to_owned(),
            }]
        );
    }

    #[test]
    fn a_pending_restore_with_a_document_is_listed() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        folder_with_document(&temp.path().join(PENDING_DIR));

        assert_eq!(
            leftovers(&paths),
            vec![Leftover {
                kind: LeftoverKind::PendingRestore,
                path: PENDING_DIR.to_owned(),
            }]
        );
    }

    #[test]
    fn a_pre_restore_folder_without_documents_is_not_listed() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let folder = temp.path().join("pre-restore-20260101-000000");
        fs::create_dir_all(&folder).unwrap();
        fs::write(folder.join(crate::backup::DB_NAME), b"encrypted").unwrap();
        fs::create_dir_all(folder.join("documents")).unwrap();

        assert!(leftovers(&paths).is_empty());
    }

    #[cfg(unix)]
    #[test]
    fn symlinks_are_not_followed() {
        use std::os::unix::fs::symlink;

        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let outside = temp.path().join("outside");
        folder_with_document(&outside);

        symlink(&outside, temp.path().join("pre-restore-20260101-000000")).unwrap();

        let folder = temp.path().join("pre-restore-20260102-000000");
        fs::create_dir_all(folder.join("documents")).unwrap();
        symlink(
            outside.join("documents/receipt.pdf"),
            folder.join("documents/receipt.pdf"),
        )
        .unwrap();

        assert!(leftovers(&paths).is_empty());
    }

    #[test]
    fn leftovers_are_sorted_by_path() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        folder_with_document(&temp.path().join(PENDING_DIR));
        folder_with_document(&temp.path().join("profiles-deleted/business-20260101-000000"));
        let profile = profiles::create(&paths, "Business").unwrap();
        let slug = profile.slug.clone().unwrap();
        folder_with_document(&profile.dir.join("pre-restore-20260101-000000"));
        folder_with_document(&temp.path().join("pre-restore-20260101-000000"));

        let listed = leftovers(&paths)
            .into_iter()
            .map(|leftover| leftover.path)
            .collect::<Vec<_>>();

        assert_eq!(
            listed,
            vec![
                "pre-restore-20260101-000000".to_owned(),
                "profiles-deleted/business-20260101-000000".to_owned(),
                format!("profiles/{slug}/pre-restore-20260101-000000"),
                PENDING_DIR.to_owned(),
            ]
        );
    }

    #[test]
    fn slug_key_matches_the_profile_folder_helpers() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        assert_eq!(slug_key(None), "default");
        assert_eq!(slug_key(Some("business")), "business");
        assert_eq!(
            profiles::data_dir(&paths, None).unwrap(),
            paths.profile_dir(slug_key(None)).unwrap()
        );
        assert_eq!(
            profiles::data_dir(&paths, Some("business")).unwrap(),
            paths.profile_dir(slug_key(Some("business"))).unwrap()
        );
    }

    #[test]
    fn a_staged_restore_is_applied_before_the_database_can_open() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let db = crate::app::unlock::create(&paths, slug_key(None), PASS, PASS, true).unwrap();
        let account_id = account(&db);
        add_transaction(&db, account_id, "Before backup", "before");

        let service = BackupService::new(profiles::data_dir(&paths, None).unwrap());
        let backup = temp.path().join("startup.zip");
        service.create_backup(&db, &backup).unwrap();
        add_transaction(&db, account_id, "After backup", "after");
        service.stage_restore(&db, &backup, None).unwrap();
        drop(db);

        let now = datetime!(2026-10-15 12:34:56 UTC);
        let startup = prepare(&paths, now).unwrap();

        assert_eq!(startup.slug, None);
        let previous = startup.restored_from.expect("the restore moved the live data aside");
        assert_eq!(
            previous.file_name().unwrap().to_string_lossy(),
            "pre-restore-20261015-123456"
        );
        assert_eq!(previous.parent(), Some(temp.path()));
        assert!(previous.join(crate::backup::DB_NAME).is_file());
        assert!(!service.has_pending_restore());

        let reopened = crate::app::unlock::unlock(&paths, slug_key(None), PASS).unwrap();
        assert_eq!(partner_names(&reopened), vec!["Before backup"]);
    }

    #[test]
    fn prepare_removes_a_leftover_migration_staging_folder() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let staging = temp.path().join(crate::app::migration::STAGING_DIR).join("1");
        fs::create_dir_all(staging.join("documents")).unwrap();
        fs::write(staging.join("documents/copy.pdf"), b"unencrypted").unwrap();
        fs::write(staging.join("finglass.db"), b"staged").unwrap();

        prepare(&paths, datetime!(2026-10-15 12:34:56 UTC)).unwrap();

        assert!(!temp.path().join(crate::app::migration::STAGING_DIR).exists());
    }

    #[test]
    fn without_a_staged_restore_nothing_moves() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        drop(crate::app::unlock::create(&paths, slug_key(None), PASS, PASS, true).unwrap());

        let startup = prepare(&paths, datetime!(2026-10-15 12:34:56 UTC)).unwrap();

        assert_eq!(startup.slug, None);
        assert_eq!(startup.restored_from, None);
        assert!(temp.path().join("finglass.db").is_file());
        assert!(pre_restore_folders(temp.path()).is_empty());
    }

    #[test]
    fn the_active_named_profile_is_prepared_in_its_own_folder() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let profile = profiles::create(&paths, "Business").unwrap();
        let slug = profile.slug.clone().unwrap();
        profiles::set_active(&paths, Some(&slug)).unwrap();
        fs::write(temp.path().join("default-data.txt"), b"default").unwrap();

        let db = crate::app::unlock::create(&paths, &slug, PASS, PASS, true).unwrap();
        let account_id = account(&db);
        add_transaction(&db, account_id, "Before backup", "before");
        let service = BackupService::new(profiles::data_dir(&paths, Some(&slug)).unwrap());
        let backup = temp.path().join("named.zip");
        service.create_backup(&db, &backup).unwrap();
        add_transaction(&db, account_id, "After backup", "after");
        service.stage_restore(&db, &backup, None).unwrap();
        drop(db);

        let startup = prepare(&paths, datetime!(2026-10-15 12:34:56 UTC)).unwrap();

        assert_eq!(startup.slug.as_deref(), Some(slug.as_str()));
        let previous = startup.restored_from.expect("the named profile's restore was applied");
        assert_eq!(previous.parent(), Some(profile.dir.as_path()));
        assert_eq!(
            fs::read_to_string(temp.path().join("default-data.txt")).unwrap(),
            "default"
        );
        assert!(
            pre_restore_folders(temp.path()).is_empty(),
            "the default profile is untouched"
        );

        let reopened = crate::app::unlock::unlock(&paths, &slug, PASS).unwrap();
        assert_eq!(partner_names(&reopened), vec!["Before backup"]);
    }

    #[test]
    fn a_failing_restore_returns_an_error_and_leaves_the_live_database_alone() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let db = crate::app::unlock::create(&paths, slug_key(None), PASS, PASS, true).unwrap();
        let account_id = account(&db);
        add_transaction(&db, account_id, "Before backup", "before");

        let service = BackupService::new(profiles::data_dir(&paths, None).unwrap());
        let backup = temp.path().join("failing.zip");
        service.create_backup(&db, &backup).unwrap();
        add_transaction(&db, account_id, "After backup", "after");
        service.stage_restore(&db, &backup, None).unwrap();
        drop(db);

        let now = datetime!(2026-10-15 12:34:56 UTC);
        // A file where the pre-restore folder must go makes creating that folder fail.
        fs::write(temp.path().join("pre-restore-20261015-123456"), b"not a folder").unwrap();

        let error = prepare(&paths, now).unwrap_err();
        assert!(matches!(error, StartupError::Backup(_)), "unexpected error: {error:?}");
        assert!(service.has_pending_restore(), "nothing was applied");

        // The live database still holds the changed data; startup never opened it.
        let live = Database::open(&paths.database_file(slug_key(None)).unwrap(), &passphrase()).unwrap();
        assert_eq!(partner_names(&live), vec!["Before backup", "After backup"]);
    }
}
