//! First-run migration from a FinDesk install.
//!
//! The migration discovers FinDesk's profiles, copies each selected profile into a fresh Finglass profile and records
//! the user's choice in a marker file, so the offer is never shown twice. FinDesk's data is only read: every profile
//! is verified and imported from a temporary copy by [`crate::findesk_import`].

use std::collections::BTreeMap;
use std::fs;
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use thiserror::Error;
use time::OffsetDateTime;
use time::UtcOffset;
use time::format_description::well_known::Rfc3339;

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::findesk_import::{self, FinDeskImportError, FinDeskImportReport};
use crate::i18n::{self, Key, Language};
use crate::paths::AppPaths;
use crate::profiles::{self, ProfileError};
use crate::secret::Passphrase;

/// The marker that records that the first-run offer was handled.
pub const MARKER_FILE: &str = "findesk-migration";

/// The folder inside the Finglass root where a profile is prepared before it is moved into place.
pub const STAGING_DIR: &str = ".findesk-migration-staging";

const NAME_PREFIX: &str = "name.";
const DEFAULT_KEY: &str = "default";

static STAGING_COUNTER: AtomicU64 = AtomicU64::new(0);
static MARKER_COUNTER: AtomicU64 = AtomicU64::new(0);

/// One profile of a FinDesk install.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinDeskProfile {
    /// FinDesk's slug, or `None` for its default profile.
    pub slug: Option<String>,
    /// The display name.
    pub name: String,
    /// The folder holding `findesk.db` and `documents/`.
    pub data_dir: PathBuf,
}

/// A FinDesk install found on this Mac.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinDeskInstall {
    /// FinDesk's data root.
    pub root: PathBuf,
    /// The profiles with a database, the default profile first.
    pub profiles: Vec<FinDeskProfile>,
    /// FinDesk's `active` profile slug, when one is recorded.
    pub active_slug: Option<String>,
}

/// A profile that was migrated.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MigratedProfile {
    /// The Finglass slug, or `None` for Finglass's default profile.
    pub finglass_slug: Option<String>,
    /// What the import did.
    pub report: FinDeskImportReport,
}

/// Everything that can go wrong while migrating a profile.
///
/// No variant carries a passphrase.
#[derive(Debug, Error)]
pub enum MigrationError {
    /// Reading FinDesk's database failed.
    #[error(transparent)]
    FinDesk(#[from] FinDeskImportError),
    /// Creating or renaming the Finglass profile failed.
    #[error(transparent)]
    Profile(#[from] ProfileError),
    /// Creating the Finglass database failed.
    #[error(transparent)]
    Db(#[from] DbError),
    /// A file operation failed.
    #[error(transparent)]
    Io(#[from] io::Error),
    /// The Finglass target already holds data.
    #[error("{}", i18n::text(Language::En, Key::ErrorMigrationTargetNotEmpty))]
    TargetNotEmpty,
}

impl MigrationError {
    /// The user text in `language`. Technical failures keep their English description.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        match self {
            Self::FinDesk(error) => error.message(language),
            Self::TargetNotEmpty => i18n::text(language, Key::ErrorMigrationTargetNotEmpty).to_owned(),
            Self::Profile(_) | Self::Db(_) | Self::Io(_) => i18n::text(language, Key::ErrorUnexpected).to_owned(),
        }
    }
}

/// Finds the FinDesk profiles under `findesk_root` that have a database.
///
/// This is read-only: it never follows symlinks and never writes or creates anything in the FinDesk folder. A missing
/// `profiles.properties` means the default profile only, named "Personal".
#[must_use]
pub fn discover(findesk_root: &Path) -> Option<FinDeskInstall> {
    let properties = read_properties(findesk_root);

    let default_name = properties
        .get(&format!("{NAME_PREFIX}{DEFAULT_KEY}"))
        .cloned()
        .unwrap_or_else(|| profiles::DEFAULT_NAME.to_owned());

    let mut profiles = Vec::new();

    if is_regular_file(&findesk_root.join("findesk.db")) {
        profiles.push(FinDeskProfile {
            slug: None,
            name: default_name,
            data_dir: findesk_root.to_path_buf(),
        });
    }

    let mut named: Vec<FinDeskProfile> = properties
        .iter()
        .filter_map(|(key, name)| {
            let slug = key.strip_prefix(NAME_PREFIX)?;
            if slug.is_empty() || slug == DEFAULT_KEY || !is_valid_slug(slug) {
                return None;
            }

            let data_dir = findesk_root.join(profiles::PROFILES_DIR).join(slug);
            if !is_regular_file(&data_dir.join("findesk.db")) {
                return None;
            }

            Some(FinDeskProfile {
                slug: Some(slug.to_owned()),
                name: name.clone(),
                data_dir,
            })
        })
        .collect();

    named.sort_by(|left, right| {
        left.name
            .to_lowercase()
            .cmp(&right.name.to_lowercase())
            .then_with(|| left.slug.cmp(&right.slug))
    });
    profiles.extend(named);

    if profiles.is_empty() {
        return None;
    }

    let active_slug = properties
        .get("active")
        .map(String::as_str)
        .filter(|slug| !slug.is_empty())
        .map(str::to_owned);

    Some(FinDeskInstall {
        root: findesk_root.to_path_buf(),
        profiles,
        active_slug,
    })
}

/// Whether the unlock window should offer the migration.
///
/// All four conditions must hold: no Finglass default database, no named Finglass profile, no marker, and a FinDesk
/// install with at least one profile that has a database.
#[must_use]
pub fn should_offer(paths: &AppPaths, install: Option<&FinDeskInstall>) -> bool {
    let Some(install) = install else {
        return false;
    };
    if install.profiles.is_empty() {
        return false;
    }
    if paths.database_file(DEFAULT_KEY).is_ok_and(|file| file.exists()) {
        return false;
    }
    if !lists_no_named_profiles(paths) {
        return false;
    }
    !paths.root().join(MARKER_FILE).exists()
}

/// Writes the marker atomically, recording whether the user migrated or chose an empty start.
pub fn write_marker(paths: &AppPaths, migrate: bool, now: OffsetDateTime) -> io::Result<()> {
    AppPaths::ensure_dir(paths.root())?;

    let action = if migrate { "migrate" } else { "skip" };
    let timestamp = now
        .format(&Rfc3339)
        .map_err(|_| io::Error::other("could not format the migration time"))?;

    let target = paths.root().join(MARKER_FILE);
    let counter = MARKER_COUNTER.fetch_add(1, Ordering::Relaxed);
    let temp = paths
        .root()
        .join(format!(".findesk-migration-{}-{counter}.tmp", std::process::id()));

    let write = || -> io::Result<()> {
        let mut file = fs::File::create(&temp)?;
        writeln!(file, "{action} {timestamp}")?;
        file.flush()?;
        file.sync_all()?;
        drop(file);

        #[cfg(windows)]
        if target.exists() {
            fs::remove_file(&target)?;
        }

        fs::rename(&temp, &target)
    };

    if let Err(error) = write() {
        let _ = fs::remove_file(&temp);
        return Err(error);
    }

    Ok(())
}

/// The Finglass slug that should be active after the migration.
///
/// `migrated` holds FinDesk slug → Finglass slug pairs. When FinDesk's active profile was not migrated, the default
/// Finglass profile (`None`) is chosen. A FinDesk slug is never reused as a Finglass slug.
#[must_use]
pub fn active_after(install: &FinDeskInstall, migrated: &[(Option<String>, Option<String>)]) -> Option<String> {
    let active = install.active_slug.as_deref();
    migrated
        .iter()
        .find(|(findesk, _)| findesk.as_deref() == active)
        .and_then(|(_, finglass)| finglass.clone())
}

/// Verifies the FinDesk passphrase, imports the profile into a fresh Finglass profile and moves it into place.
///
/// The staging folder is always removed, also when an error occurs. Nothing is written to FinDesk's folder.
pub fn migrate_profile(
    paths: &AppPaths,
    profile: &FinDeskProfile,
    findesk: &Passphrase,
    finglass: &Passphrase,
    offset: UtcOffset,
) -> Result<MigratedProfile, MigrationError> {
    findesk_import::verify_passphrase(&profile.data_dir, findesk)?;

    let staging = create_staging(paths)?;
    let result = migrate_in_staging(paths, profile, findesk, finglass, offset, &staging);
    let _ = fs::remove_dir_all(&staging);
    // Also drop the now empty parent; a crash before this leaves it for startup to remove.
    if let Some(parent) = staging.parent() {
        let _ = fs::remove_dir(parent);
    }
    result
}

fn migrate_in_staging(
    paths: &AppPaths,
    profile: &FinDeskProfile,
    findesk: &Passphrase,
    finglass: &Passphrase,
    offset: UtcOffset,
    staging: &Path,
) -> Result<MigratedProfile, MigrationError> {
    let staging_db = staging.join("finglass.db");
    let staging_documents = staging.join("documents");

    // The database handle is dropped before anything is moved.
    let report = {
        let db = Database::create(&staging_db, finglass)?;
        audit::log(&db, "DATABASE_CREATED", Actor::User, "New encrypted database")?;
        let report = findesk_import::import_findesk(&db, &staging_documents, &profile.data_dir, findesk, offset)?;
        drop(db);
        report
    };

    match profile.slug.as_deref() {
        None => {
            let target = paths.root();
            let database_exists = paths.database_file(DEFAULT_KEY).is_ok_and(|file| file.exists());
            if database_exists
                || target.join("findesk.db").exists()
                || !documents_absent_or_empty(&target.join("documents"))
            {
                return Err(MigrationError::TargetNotEmpty);
            }

            move_into_place(staging, target)?;

            let current_name = profiles::list(paths)
                .ok()
                .and_then(|listed| listed.into_iter().next())
                .map(|current| current.name);
            if current_name.as_deref() != Some(profile.name.as_str()) {
                profiles::rename(paths, None, &profile.name)?;
            }

            Ok(MigratedProfile {
                finglass_slug: None,
                report,
            })
        }
        Some(_) => {
            let created = profiles::create(paths, &profile.name)?;
            move_into_place(staging, &created.dir)?;
            Ok(MigratedProfile {
                finglass_slug: created.slug,
                report,
            })
        }
    }
}

fn create_staging(paths: &AppPaths) -> io::Result<PathBuf> {
    let root = paths.root().join(STAGING_DIR);
    AppPaths::ensure_dir(&root)?;

    loop {
        let number = STAGING_COUNTER.fetch_add(1, Ordering::Relaxed);
        let candidate = root.join(number.to_string());
        match fs::create_dir(&candidate) {
            Ok(()) => return Ok(candidate),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
}

/// Moves the documents first, then the database's side files and the database itself last, so a profile folder never
/// holds a database whose documents or WAL are still in staging.
fn move_into_place(staging: &Path, target: &Path) -> io::Result<()> {
    // The database must be there: a profile without one would be reported as migrated but empty.
    let database = staging.join("finglass.db");
    if !database.is_file() {
        return Err(io::Error::new(
            io::ErrorKind::NotFound,
            "the staged database is missing",
        ));
    }
    let staging_documents = staging.join("documents");
    if staging_documents.exists() {
        let target_documents = target.join("documents");
        if target_documents.exists() {
            // Only an empty folder can be replaced here; a non-empty one was refused before.
            fs::remove_dir(&target_documents)?;
        }
        fs::rename(&staging_documents, &target_documents)?;
    }

    for name in ["finglass.db-wal", "finglass.db-shm"] {
        let source = staging.join(name);
        if source.exists() {
            fs::rename(&source, target.join(name))?;
        }
    }
    fs::rename(&database, target.join("finglass.db"))?;

    Ok(())
}

fn documents_absent_or_empty(path: &Path) -> bool {
    match fs::symlink_metadata(path) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => true,
        Ok(metadata) if metadata.file_type().is_dir() => fs::read_dir(path)
            .map(|mut entries| entries.next().is_none())
            .unwrap_or(false),
        _ => false,
    }
}

fn lists_no_named_profiles(paths: &AppPaths) -> bool {
    let text = fs::read_to_string(paths.root().join("profiles.properties")).unwrap_or_default();
    let properties = profiles::parse_properties(&text);

    !properties.keys().any(|key| {
        key.strip_prefix(NAME_PREFIX)
            .is_some_and(|slug| !slug.is_empty() && slug != DEFAULT_KEY)
    })
}

fn read_properties(root: &Path) -> BTreeMap<String, String> {
    match fs::read_to_string(root.join("profiles.properties")) {
        Ok(text) => profiles::parse_properties(&text),
        Err(_) => BTreeMap::new(),
    }
}

fn is_regular_file(path: &Path) -> bool {
    fs::symlink_metadata(path).is_ok_and(|metadata| metadata.file_type().is_file())
}

/// FinDesk's slug rule: 1 to 40 characters, a lowercase letter or digit first, then lowercase letters, digits or
/// hyphens.
fn is_valid_slug(slug: &str) -> bool {
    let mut characters = slug.chars();

    let Some(first) = characters.next() else {
        return false;
    };
    if !(first.is_ascii_lowercase() || first.is_ascii_digit()) {
        return false;
    }

    let mut length = 1;
    for character in characters {
        if !(character.is_ascii_lowercase() || character.is_ascii_digit() || character == '-') {
            return false;
        }
        length += 1;
        if length > 40 {
            return false;
        }
    }

    true
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;
    use std::collections::hash_map::DefaultHasher;
    use std::fs;
    use std::hash::{Hash, Hasher};
    use std::path::{Path, PathBuf};

    use time::macros::datetime;

    use super::*;
    use crate::secret::PassphrasePurpose;

    const FIXTURE_PASSPHRASE: &str = "fixture passphrase 1";
    const TARGET_PASSPHRASE: &str = "target passphrase 1";

    fn fixture_copy() -> (tempfile::TempDir, PathBuf) {
        let fixture = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/findesk");
        let temp = tempfile::tempdir().unwrap();
        let destination = temp.path().join("findesk");
        copy_tree(&fixture, &destination);
        (temp, destination)
    }

    fn copy_tree(from: &Path, to: &Path) {
        fs::create_dir_all(to).unwrap();
        for entry in fs::read_dir(from).unwrap() {
            let entry = entry.unwrap();
            let target = to.join(entry.file_name());
            if entry.file_type().unwrap().is_dir() {
                copy_tree(&entry.path(), &target);
            } else {
                fs::copy(entry.path(), target).unwrap();
            }
        }
    }

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    fn findesk_passphrase() -> Passphrase {
        Passphrase::new(FIXTURE_PASSPHRASE, PassphrasePurpose::Unlock).unwrap()
    }

    fn target_passphrase() -> Passphrase {
        Passphrase::new(TARGET_PASSPHRASE, PassphrasePurpose::New).unwrap()
    }

    fn offset() -> UtcOffset {
        UtcOffset::from_hms(2, 0, 0).unwrap()
    }

    fn hashes(root: &Path) -> BTreeMap<PathBuf, u64> {
        fn walk(dir: &Path, root: &Path, out: &mut BTreeMap<PathBuf, u64>) {
            for entry in fs::read_dir(dir).unwrap() {
                let entry = entry.unwrap();
                let file_type = entry.file_type().unwrap();
                if file_type.is_dir() {
                    walk(&entry.path(), root, out);
                } else if file_type.is_file() {
                    let relative = entry.path().strip_prefix(root).unwrap().to_path_buf();
                    let mut hasher = DefaultHasher::new();
                    relative.hash(&mut hasher);
                    fs::read(entry.path()).unwrap().hash(&mut hasher);
                    out.insert(relative, hasher.finish());
                }
            }
        }

        let mut out = BTreeMap::new();
        walk(root, root, &mut out);
        out
    }

    fn write_named_profile(root: &Path, slug: &str) {
        let dir = root.join(profiles::PROFILES_DIR).join(slug);
        fs::create_dir_all(&dir).unwrap();
        fs::copy(root.join("findesk.db"), dir.join("findesk.db")).unwrap();
        copy_tree(&root.join("documents"), &dir.join("documents"));
    }

    fn assert_fixture_counts(report: &FinDeskImportReport) {
        assert_eq!(report.account_groups.imported, 1);
        assert_eq!(report.bank_connections.imported, 2);
        assert_eq!(report.accounts.imported, 3);
        assert_eq!(report.categories.imported, 3);
        assert_eq!(report.rules.imported, 2);
        assert_eq!(report.transactions.imported, 8);
        assert_eq!(report.documents.copied, 1);
        assert_eq!(report.documents.missing, 2);
        assert_eq!(report.settings.imported, 8);
        assert_eq!(report.payment_orders_left_behind, 1);
    }

    #[test]
    fn a_root_with_only_a_database_is_the_default_profile() {
        let (_temp, root) = fixture_copy();

        let install = discover(&root).unwrap();

        assert_eq!(install.root, root);
        assert_eq!(install.profiles.len(), 1);
        assert_eq!(
            install.profiles[0],
            FinDeskProfile {
                slug: None,
                name: "Personal".to_owned(),
                data_dir: root.clone(),
            }
        );
        assert_eq!(install.active_slug, None);
    }

    #[test]
    fn a_properties_file_names_the_profiles_and_their_active_one() {
        let (_temp, root) = fixture_copy();
        write_named_profile(&root, "work");
        fs::write(
            root.join("profiles.properties"),
            "name.default=Personal\nname.work=M\\u00fcller\nactive=work\n",
        )
        .unwrap();

        let install = discover(&root).unwrap();

        assert_eq!(install.profiles.len(), 2);
        assert_eq!(install.profiles[0].name, "Personal");
        assert_eq!(install.profiles[1].slug.as_deref(), Some("work"));
        assert_eq!(install.profiles[1].name, "Müller");
        assert_eq!(install.active_slug.as_deref(), Some("work"));
    }

    #[test]
    fn a_named_profile_without_a_database_is_left_out() {
        let (_temp, root) = fixture_copy();
        fs::create_dir_all(root.join("profiles/empty")).unwrap();
        fs::write(
            root.join("profiles.properties"),
            "name.default=Personal\nname.empty=Empty\n",
        )
        .unwrap();

        let install = discover(&root).unwrap();

        assert_eq!(install.profiles.len(), 1);
        assert_eq!(install.profiles[0].slug, None);
    }

    #[cfg(unix)]
    #[test]
    fn a_symlinked_database_is_left_out() {
        use std::os::unix::fs::symlink;

        let (temp, root) = fixture_copy();
        let linked = temp.path().join("linked-findesk");
        fs::create_dir_all(&linked).unwrap();
        symlink(root.join("findesk.db"), linked.join("findesk.db")).unwrap();

        assert_eq!(discover(&linked), None);
    }

    #[test]
    fn an_empty_folder_has_no_install() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("findesk");
        fs::create_dir_all(&root).unwrap();

        assert_eq!(discover(&root), None);
    }

    #[test]
    fn discovery_leaves_the_fin_desk_folder_byte_identical() {
        let (_temp, root) = fixture_copy();
        write_named_profile(&root, "work");
        fs::write(
            root.join("profiles.properties"),
            "name.default=Personal\nname.work=Work\nactive=work\n",
        )
        .unwrap();

        let before = hashes(&root);
        let install = discover(&root).unwrap();
        let after = hashes(&root);

        assert_eq!(install.profiles.len(), 2);
        assert_eq!(before, after);
    }

    #[test]
    fn the_offer_needs_a_fresh_finglass_root_and_an_install() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();

        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        assert!(should_offer(&target, Some(&install)));

        assert!(!should_offer(&target, None));
    }

    #[test]
    fn the_marker_stops_the_offer() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();
        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);

        write_marker(&target, true, datetime!(2026 - 10 - 15 12:34:56 UTC)).unwrap();

        assert!(!should_offer(&target, Some(&install)));
    }

    #[test]
    fn an_existing_database_stops_the_offer() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();
        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        fs::write(target.database_file(DEFAULT_KEY).unwrap(), b"db").unwrap();

        assert!(!should_offer(&target, Some(&install)));
    }

    #[test]
    fn a_second_finglass_profile_stops_the_offer() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();
        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        profiles::create(&target, "Business").unwrap();

        assert!(!should_offer(&target, Some(&install)));
    }

    #[test]
    fn the_marker_records_the_choice_as_rfc_3339() {
        let temp = tempfile::tempdir().unwrap();
        let target = paths(&temp);
        let now = datetime!(2026 - 10 - 15 12:34:56 UTC);

        write_marker(&target, true, now).unwrap();
        assert_eq!(
            fs::read_to_string(target.root().join(MARKER_FILE)).unwrap(),
            "migrate 2026-10-15T12:34:56Z\n"
        );

        write_marker(&target, false, now).unwrap();
        assert_eq!(
            fs::read_to_string(target.root().join(MARKER_FILE)).unwrap(),
            "skip 2026-10-15T12:34:56Z\n"
        );
    }

    #[test]
    fn the_default_profile_is_migrated_into_the_root() {
        let (source_temp, root) = fixture_copy();
        fs::write(root.join("profiles.properties"), "name.default=Privat\n").unwrap();
        let install = discover(&root).unwrap();
        let profile = install.profiles[0].clone();
        let before = hashes(&root);

        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        let migrated =
            migrate_profile(&target, &profile, &findesk_passphrase(), &target_passphrase(), offset()).unwrap();

        assert_eq!(migrated.finglass_slug, None);
        assert_fixture_counts(&migrated.report);

        let db = Database::open(&target.database_file(DEFAULT_KEY).unwrap(), &target_passphrase()).unwrap();
        drop(db);
        assert_eq!(
            profiles::list(&target).unwrap()[0].name,
            "Privat",
            "FinDesk's default name is used"
        );

        let documents = target.root().join("documents");
        assert_eq!(fs::read_dir(&documents).unwrap().count(), 1);
        assert!(!target.root().join(STAGING_DIR).exists());
        assert_eq!(hashes(&root), before, "FinDesk's folder was only read");
        drop(source_temp);
    }

    #[test]
    fn a_named_profile_gets_its_own_finglass_slug() {
        let (_source_temp, root) = fixture_copy();
        write_named_profile(&root, "work");
        fs::write(
            root.join("profiles.properties"),
            "name.default=Personal\nname.work=M\\u00fcller\nactive=work\n",
        )
        .unwrap();
        let install = discover(&root).unwrap();
        let profile = install
            .profiles
            .iter()
            .find(|profile| profile.slug.as_deref() == Some("work"))
            .unwrap()
            .clone();
        let before = hashes(&root);

        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        let migrated =
            migrate_profile(&target, &profile, &findesk_passphrase(), &target_passphrase(), offset()).unwrap();

        assert_eq!(migrated.finglass_slug.as_deref(), Some("muller"));
        assert_fixture_counts(&migrated.report);

        let slug = migrated.finglass_slug.as_deref().unwrap();
        let db = Database::open(&target.database_file(slug).unwrap(), &target_passphrase()).unwrap();
        drop(db);
        let listed = profiles::list(&target).unwrap();
        assert_eq!(listed.len(), 2);
        assert_eq!(listed[1].name, "Müller");
        assert_eq!(listed[1].slug.as_deref(), Some(slug));
        assert_eq!(
            fs::read_dir(target.profile_dir(slug).unwrap().join("documents"))
                .unwrap()
                .count(),
            1
        );
        assert!(!target.root().join(STAGING_DIR).exists());
        assert_eq!(hashes(&root), before);
    }

    #[test]
    fn a_wrong_passphrase_creates_nothing() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();
        let profile = install.profiles[0].clone();

        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        let wrong = Passphrase::new("not the fixture passphrase", PassphrasePurpose::Unlock).unwrap();

        let error = migrate_profile(&target, &profile, &wrong, &target_passphrase(), offset()).unwrap_err();

        assert!(matches!(
            error,
            MigrationError::FinDesk(FinDeskImportError::WrongPassphrase)
        ));
        assert!(!target.root().join("finglass.db").exists());
        assert!(!target.root().join(STAGING_DIR).exists());
        assert_eq!(profiles::list(&target).unwrap().len(), 1);
    }

    #[test]
    fn a_non_empty_default_target_moves_nothing() {
        let (_source_temp, root) = fixture_copy();
        let install = discover(&root).unwrap();
        let profile = install.profiles[0].clone();

        let target_temp = tempfile::tempdir().unwrap();
        let target = paths(&target_temp);
        let documents = target.root().join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("existing.pdf"), b"keep me").unwrap();

        let error =
            migrate_profile(&target, &profile, &findesk_passphrase(), &target_passphrase(), offset()).unwrap_err();

        assert!(matches!(error, MigrationError::TargetNotEmpty));
        assert!(!target.root().join("finglass.db").exists());
        assert_eq!(fs::read(documents.join("existing.pdf")).unwrap(), b"keep me");
        assert!(!target.root().join(STAGING_DIR).exists());
    }

    #[test]
    fn active_after_prefers_the_migrated_active_profile() {
        let install = FinDeskInstall {
            root: PathBuf::from("/findesk"),
            profiles: Vec::new(),
            active_slug: Some("work".to_owned()),
        };

        assert_eq!(
            active_after(&install, &[(Some("work".to_owned()), Some("muller".to_owned()))]).as_deref(),
            Some("muller")
        );
        assert_eq!(
            active_after(&install, &[]),
            None,
            "a skipped profile falls back to the default"
        );
    }

    #[test]
    fn active_after_falls_back_to_the_default_profile() {
        let install = FinDeskInstall {
            root: PathBuf::from("/findesk"),
            profiles: Vec::new(),
            active_slug: None,
        };

        assert_eq!(active_after(&install, &[(None, None)]), None);
        assert_eq!(
            active_after(&install, &[(Some("work".to_owned()), Some("muller".to_owned()))]),
            None,
            "another profile being migrated does not make it active"
        );
    }

    #[test]
    fn a_staging_folder_without_a_database_moves_nothing() {
        let temp = tempfile::tempdir().unwrap();
        let staging = temp.path().join("staging");
        let target = temp.path().join("target");
        fs::create_dir_all(staging.join("documents")).unwrap();
        fs::write(staging.join("documents/receipt.pdf"), b"pdf").unwrap();
        fs::create_dir_all(&target).unwrap();

        assert!(move_into_place(&staging, &target).is_err());
        assert!(staging.join("documents/receipt.pdf").is_file());
        assert!(!target.join("documents").exists());
    }
}
