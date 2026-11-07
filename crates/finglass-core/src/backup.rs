//! Backups and two-phase restore.
//!
//! A backup is a ZIP file with a consistent copy of the encrypted database, the files in `documents/` and a small
//! manifest. Writing starts at a private temporary file that is owner-only from the first byte and is moved into
//! place only once the backup is complete, so a failed run leaves no partial or world-readable ZIP behind.
//!
//! Restoring replaces the live database, so it happens in two phases: [`BackupService::stage_restore`] validates the
//! backup and unpacks it next to the data, and [`BackupService::apply_pending_restore`] moves the current data aside
//! into `pre-restore-<timestamp>/` and puts the staged data in place. The second phase must run before the database
//! is opened.

use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use thiserror::Error;
use time::OffsetDateTime;
use time::format_description::FormatItem;
use time::format_description::well_known::Rfc3339;
use time::macros::format_description;
use zip::read::ZipArchive;
use zip::result::ZipError;
use zip::write::SimpleFileOptions;
use zip::{CompressionMethod, ZipWriter};

use crate::db::{Database, DbError};
use crate::secret::Passphrase;

/// The database file inside a profile and inside a backup.
pub const DB_NAME: &str = "finglass.db";

/// The backup manifest.
pub const MANIFEST: &str = "finglass-backup.properties";

/// Holds a staged restore until the next start.
pub const PENDING_DIR: &str = "restore-pending";

/// `pre-restore-<timestamp>` folders hold the data a restore replaced.
pub const PRE_RESTORE_PREFIX: &str = "pre-restore-";

/// Upper bound for one entry, whatever the ZIP declares.
pub const MAX_ENTRY_BYTES: u64 = 2 * 1024 * 1024 * 1024;

/// Upper bound for everything a restore unpacks, whatever the ZIP declares.
pub const MAX_RESTORE_BYTES: u64 = 8 * 1024 * 1024 * 1024;

/// The content folders packed into a backup. Finglass has no `passports/` (no HBCI4Java); a later folder can be
/// added here without touching the rest of the module.
const CONTENT_DIRS: &[&str] = &["documents"];

/// Size of the copy buffer. Counts bytes while copying instead of trusting the ZIP's declared sizes.
const BUFFER_SIZE: usize = 8 * 1024;

/// The timestamp format used for `pre-restore-<timestamp>` folders.
const STAMP: &[FormatItem<'static>] = format_description!("[year][month][day]-[hour][minute][second]");

/// What a backup says about itself.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BackupInfo {
    /// When the backup was written, as text.
    pub created: String,
    /// The Finglass version that wrote it.
    pub app_version: String,
    /// The number of transactions in the database copy.
    pub transactions: i64,
    /// The number of files in the content folders.
    pub documents: usize,
}

/// Everything that can go wrong around a backup or a restore.
///
/// No variant carries a passphrase.
#[derive(Debug, Error)]
pub enum BackupError {
    /// The file is not a Finglass backup (no manifest).
    #[error("not a Finglass backup")]
    NotABackup,

    /// The backup has a manifest but no database entry.
    #[error("backup contains no database")]
    NoDatabase,

    /// A ZIP entry points outside the staging folder or at an unexpected location.
    #[error("unsafe path in backup: {0}")]
    UnsafePath(String),

    /// The backup is too large to restore.
    #[error("backup is too large to restore")]
    TooLarge,

    /// The backup was made with a passphrase other than the live database's.
    #[error("this backup was made with a different passphrase")]
    PassphraseRequired {
        /// Whether a passphrase was supplied and turned out to be wrong.
        wrong_passphrase_given: bool,
    },

    /// The backup comes from a newer Finglass version.
    #[error("backup database is newer than this version of Finglass")]
    TooNew,

    /// An error from the database layer.
    #[error(transparent)]
    Db(#[from] DbError),

    /// An I/O error.
    #[error(transparent)]
    Io(#[from] io::Error),

    /// A malformed or unreadable ZIP archive.
    #[error(transparent)]
    Zip(#[from] ZipError),
}

/// Writes and validates backups for one profile.
pub struct BackupService {
    profile_dir: PathBuf,
    max_restore_bytes: u64,
}

impl BackupService {
    /// Creates a service for the profile at `profile_dir`.
    pub fn new(profile_dir: PathBuf) -> Self {
        Self {
            profile_dir,
            max_restore_bytes: MAX_RESTORE_BYTES,
        }
    }

    /// Overrides the total restore limit. Only useful for tests.
    pub fn with_max_restore_bytes(mut self, bytes: u64) -> Self {
        self.max_restore_bytes = bytes;
        self
    }

    /// Whether a staged restore is waiting for the next start.
    pub fn has_pending_restore(&self) -> bool {
        self.pending_dir().is_dir()
    }

    /// Writes a backup of the current data to `target`.
    pub fn create_backup(&self, db: &Database, target: &Path) -> Result<BackupInfo, BackupError> {
        let target_dir = target
            .parent()
            .filter(|parent| !parent.as_os_str().is_empty())
            .unwrap_or(Path::new("."));
        fs::create_dir_all(target_dir)?;

        let tmp_db = unique_temp_path(&self.profile_dir, "finglass-backup", ".db.tmp");
        let _ = fs::remove_file(&tmp_db);
        let tmp_zip = create_private_temp_file(target_dir, "finglass-backup", ".zip.tmp")?;

        let result = (|| -> Result<BackupInfo, BackupError> {
            // VACUUM INTO gives a consistent, compacted copy even while the database is open. The target path is
            // bound as a parameter, never formatted into SQL.
            db.vacuum_into(&tmp_db)?;

            let excluded = [canonical_or_absolute(&tmp_zip)?, canonical_or_absolute(target)?];
            let content = self.content_files(&excluded)?;

            // Refused up front rather than written: stage_restore would reject it, leaving a backup that cannot
            // be restored.
            let db_len = fs::metadata(&tmp_db)?.len();
            if db_len > MAX_ENTRY_BYTES {
                return Err(BackupError::TooLarge);
            }
            let mut total = db_len;
            for (_, path) in &content {
                let len = fs::metadata(path)?.len();
                if len > MAX_ENTRY_BYTES {
                    return Err(BackupError::TooLarge);
                }
                total = total.saturating_add(len);
            }
            if total > self.max_restore_bytes {
                return Err(BackupError::TooLarge);
            }

            let transactions = attached_transaction_count(db, &tmp_db)?;
            let info = BackupInfo {
                created: OffsetDateTime::now_utc()
                    .format(&Rfc3339)
                    .map_err(|error| BackupError::Io(io::Error::other(error)))?,
                app_version: crate::VERSION.to_owned(),
                transactions,
                documents: content.len(),
            };

            write_zip(&tmp_zip, &info, &tmp_db, &content)?;
            move_into_place(&tmp_zip, target)?;
            secure_file(target);
            Ok(info)
        })();

        let _ = fs::remove_file(&tmp_db);
        let _ = fs::remove_file(&tmp_zip);
        result
    }

    /// Reads the manifest of a backup without changing anything.
    ///
    /// Missing `created` and `appVersion` values become `"?"`, missing or unparsable counts become `0`.
    pub fn inspect(&self, backup: &Path) -> Result<BackupInfo, BackupError> {
        let mut zip = ZipArchive::new(File::open(backup)?)?;

        let mut manifest = String::new();
        match zip.by_name(MANIFEST) {
            Ok(mut entry) => {
                entry.read_to_string(&mut manifest)?;
            }
            Err(ZipError::FileNotFound) => return Err(BackupError::NotABackup),
            Err(err) => return Err(err.into()),
        }

        match zip.by_name(DB_NAME) {
            Ok(_) => {}
            Err(ZipError::FileNotFound) => return Err(BackupError::NoDatabase),
            Err(err) => return Err(err.into()),
        }

        let properties = parse_properties(&manifest);
        let get = |key: &str| properties.get(key).cloned().unwrap_or_else(|| "?".to_owned());
        let transactions = properties
            .get("transactions")
            .and_then(|value| value.parse::<i64>().ok())
            .unwrap_or(0);
        let documents = properties
            .get("documents")
            .and_then(|value| value.parse::<usize>().ok())
            .unwrap_or(0);

        Ok(BackupInfo {
            created: get("created"),
            app_version: get("appVersion"),
            transactions,
            documents,
        })
    }

    /// Validates `backup` and unpacks it into the pending-restore folder.
    ///
    /// Nothing live is touched. When the staged database cannot be opened with the live key, `backup_passphrase`
    /// is required: without one this returns [`BackupError::PassphraseRequired`] with `wrong_passphrase_given =
    /// false`, with a wrong one with `true`. A failed stage removes the staging folder again.
    ///
    /// Opening the staged copy with its own passphrase runs migrations **on the staged copy**; it never changes the
    /// live database.
    pub fn stage_restore(
        &self,
        db: &Database,
        backup: &Path,
        backup_passphrase: Option<&Passphrase>,
    ) -> Result<BackupInfo, BackupError> {
        let info = self.inspect(backup)?;
        let pending = self.pending_dir();
        remove_dir_all_if_exists(&pending)?;
        secure_directory(&pending)?;

        let result = (|| -> Result<(), BackupError> {
            let mut zip = ZipArchive::new(File::open(backup)?)?;
            let mut budget = self.max_restore_bytes;

            for index in 0..zip.len() {
                let mut entry = zip.by_index(index)?;
                if entry.is_dir() {
                    continue;
                }

                let name = entry.name()?.into_owned();
                if name == MANIFEST {
                    continue;
                }

                let target = safe_target(&pending, &name)?;

                // Kotlin rejects an entry whose declared size is too large; the real limit is enforced while
                // copying below, because the archive's own size is just a claim.
                if entry.size() > MAX_ENTRY_BYTES {
                    return Err(BackupError::TooLarge);
                }

                if let Some(parent) = target.parent() {
                    secure_directory(parent)?;
                }

                let limit = MAX_ENTRY_BYTES.min(budget);
                let written = copy_at_most(&mut entry, &target, limit)?;
                if written > limit {
                    return Err(BackupError::TooLarge);
                }
                budget -= written;
                secure_file(&target);
            }

            let staged_db = pending.join(DB_NAME);
            if !staged_db.is_file() {
                return Err(BackupError::NoDatabase);
            }

            let readable = match db.can_read_with_current_key(&staged_db) {
                Ok(readable) => readable,
                Err(DbError::TooNew { .. }) => return Err(BackupError::TooNew),
                Err(err) => return Err(BackupError::Db(err)),
            };

            if !readable {
                match backup_passphrase {
                    None => {
                        return Err(BackupError::PassphraseRequired {
                            wrong_passphrase_given: false,
                        });
                    }
                    Some(passphrase) => match Database::open(&staged_db, passphrase) {
                        // The staged copy is migrated here. That is accepted and cannot touch the live database.
                        Ok(staged) => drop(staged),
                        Err(DbError::WrongPassphraseOrDamaged) => {
                            return Err(BackupError::PassphraseRequired {
                                wrong_passphrase_given: true,
                            });
                        }
                        Err(DbError::TooNew { .. }) => return Err(BackupError::TooNew),
                        Err(err) => return Err(BackupError::Db(err)),
                    },
                }
            }

            Ok(())
        })();

        if result.is_err() {
            let _ = fs::remove_dir_all(&pending);
        }

        result.map(|()| info)
    }

    /// Removes a staged restore.
    pub fn cancel_pending_restore(&self) {
        let _ = fs::remove_dir_all(self.pending_dir());
    }

    /// If a restore is staged, moves the current data into `pre-restore-<timestamp>/` and puts the staged data in
    /// place. Must run before the database is opened. Returns the folder holding the previous data, or `None` when
    /// nothing was staged.
    pub fn apply_pending_restore(&self, now: OffsetDateTime) -> Result<Option<PathBuf>, BackupError> {
        let pending = self.pending_dir();

        if !pending.join(DB_NAME).is_file() {
            remove_dir_all_if_exists(&pending)?;
            return Ok(None);
        }

        let stamp = now
            .format(STAMP)
            .map_err(|error| BackupError::Io(io::Error::other(error)))?;
        let previous = self.profile_dir.join(format!("{PRE_RESTORE_PREFIX}{stamp}"));
        secure_directory(&previous)?;

        let mut current_names = vec![DB_NAME.to_owned()];
        for suffix in ["-wal", "-shm", "-journal"] {
            current_names.push(format!("{DB_NAME}{suffix}"));
        }
        current_names.extend(CONTENT_DIRS.iter().map(|dir| (*dir).to_owned()));

        for name in &current_names {
            let current = self.profile_dir.join(name);
            if current.exists() {
                fs::rename(&current, previous.join(name))?;
            }
        }

        let mut staged_names = vec![DB_NAME.to_owned()];
        staged_names.extend(CONTENT_DIRS.iter().map(|dir| (*dir).to_owned()));

        for name in &staged_names {
            let staged = pending.join(name);
            if staged.exists() {
                fs::rename(&staged, self.profile_dir.join(name))?;
            }
        }

        remove_dir_all_if_exists(&pending)?;
        Ok(Some(previous))
    }

    fn pending_dir(&self) -> PathBuf {
        self.profile_dir.join(PENDING_DIR)
    }

    /// Lists regular files in the content folders, sorted by name and excluding the backup and its temporary ZIP.
    ///
    /// Symlinks are skipped: following them could pack a file from outside the profile.
    fn content_files(&self, excluded: &[PathBuf]) -> Result<Vec<(String, PathBuf)>, BackupError> {
        let mut files = Vec::new();

        for dir_name in CONTENT_DIRS {
            let dir = self.profile_dir.join(dir_name);
            let mut entries = match fs::read_dir(&dir) {
                Ok(entries) => entries.collect::<Result<Vec<_>, _>>()?,
                Err(err) if err.kind() == io::ErrorKind::NotFound => continue,
                Err(err) => return Err(err.into()),
            };
            entries.sort_by_key(|entry| entry.file_name());

            for entry in entries {
                let path = entry.path();
                let metadata = fs::symlink_metadata(&path)?;
                if !metadata.file_type().is_file() {
                    continue;
                }

                let canonical = path.canonicalize()?;
                if excluded.iter().any(|excluded| excluded == &canonical) {
                    continue;
                }

                let name = entry.file_name().to_string_lossy().into_owned();
                files.push((format!("{dir_name}/{name}"), path));
            }
        }

        Ok(files)
    }
}

/// Resolves a ZIP entry inside `root`, rejecting path traversal ("zip slip") and unexpected locations.
fn safe_target(root: &Path, name: &str) -> Result<PathBuf, BackupError> {
    let normalized = name.replace('\\', "/");

    if normalized.starts_with('/') || normalized.split('/').any(|part| part == "..") {
        return Err(BackupError::UnsafePath(name.to_owned()));
    }

    let allowed = normalized == DB_NAME
        || CONTENT_DIRS.iter().any(|dir_name| {
            normalized
                .strip_prefix(dir_name)
                .and_then(|rest| rest.strip_prefix('/'))
                .is_some_and(|rest| !rest.is_empty() && !rest.contains('/') && rest != "." && rest != "..")
        });

    if !allowed {
        return Err(BackupError::UnsafePath(name.to_owned()));
    }

    let target = root.join(&normalized);
    if !target.starts_with(root) {
        return Err(BackupError::UnsafePath(name.to_owned()));
    }

    Ok(target)
}

/// Copies up to `limit` bytes and returns more than `limit` when the input had more, so the caller can reject it.
fn copy_at_most(input: &mut impl Read, target: &Path, limit: u64) -> Result<u64, BackupError> {
    let mut output = create_private_file(target)?;
    let mut buffer = [0u8; BUFFER_SIZE];
    let mut total = 0u64;

    let mut read = input.read(&mut buffer)?;
    while read > 0 && total + read as u64 <= limit {
        output.write_all(&buffer[..read])?;
        total += read as u64;
        read = input.read(&mut buffer)?;
    }

    if read > 0 { Ok(total + read as u64) } else { Ok(total) }
}

/// Writes the manifest, the database copy and the content files into an already-created ZIP file.
fn write_zip(
    tmp_zip: &Path,
    info: &BackupInfo,
    tmp_db: &Path,
    content: &[(String, PathBuf)],
) -> Result<(), BackupError> {
    let file = OpenOptions::new().write(true).truncate(true).open(tmp_zip)?;
    let mut zip = ZipWriter::new(file);
    let options = SimpleFileOptions::default()
        .compression_method(CompressionMethod::Deflated)
        .unix_permissions(0o600);

    let manifest = format!(
        "# Finglass backup\ncreated={}\nappVersion={}\ntransactions={}\ndocuments={}\n",
        info.created, info.app_version, info.transactions, info.documents
    );
    zip.start_file(MANIFEST, options)?;
    zip.write_all(manifest.as_bytes())?;

    zip.start_file(DB_NAME, options)?;
    io::copy(&mut File::open(tmp_db)?, &mut zip)?;

    for (name, path) in content {
        zip.start_file(name.as_str(), options)?;
        io::copy(&mut File::open(path)?, &mut zip)?;
    }

    let mut file = zip.finish()?;
    file.flush()?;
    Ok(())
}

/// Counts transactions in an attached copy of the database using the live connection's key.
fn attached_transaction_count(db: &Database, file: &Path) -> Result<i64, BackupError> {
    let file = file.to_string_lossy().into_owned();

    db.with_conn(|conn| {
        conn.execute("ATTACH DATABASE ?1 AS backup_copy", [file.as_str()])?;

        let result = conn.query_row("SELECT count(*) FROM backup_copy.transactions", [], |row| {
            row.get::<_, i64>(0)
        });
        let detached = conn.execute_batch("DETACH DATABASE backup_copy");

        match result {
            Ok(count) => {
                detached?;
                Ok(count)
            }
            Err(err) => {
                let _ = detached;
                Err(DbError::from(err))
            }
        }
    })
    .map_err(BackupError::from)
}

/// Parses `key=value` lines the way `java.util.Properties` does leniently enough for our own manifest.
fn parse_properties(text: &str) -> HashMap<String, String> {
    let mut properties = HashMap::new();

    for raw in text.lines() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with('!') {
            continue;
        }

        let (key, value) = if let Some(index) = line.find(['=', ':']) {
            (&line[..index], &line[index + 1..])
        } else if let Some(index) = line.find(char::is_whitespace) {
            (&line[..index], &line[index..])
        } else {
            (line, "")
        };

        properties.insert(key.trim().to_owned(), value.trim().to_owned());
    }

    properties
}

/// Builds a unique temporary path without creating it.
fn unique_temp_path(dir: &Path, prefix: &str, suffix: &str) -> PathBuf {
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos())
        .unwrap_or(0);
    let counter = COUNTER.fetch_add(1, Ordering::Relaxed);

    dir.join(format!("{prefix}-{}-{nanos}-{counter}{suffix}", std::process::id()))
}

/// Creates an empty temporary file that is owner-only from the first byte.
fn create_private_temp_file(dir: &Path, prefix: &str, suffix: &str) -> io::Result<PathBuf> {
    for _ in 0..100 {
        let path = unique_temp_path(dir, prefix, suffix);

        let mut options = OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;

            options.mode(0o600);
        }

        match options.open(&path) {
            Ok(_) => return Ok(path),
            Err(err) if err.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(err) => return Err(err),
        }
    }

    Err(io::Error::new(
        io::ErrorKind::AlreadyExists,
        "could not create a temporary file",
    ))
}

/// Creates a file that is owner-only from the first byte.
fn create_private_file(path: &Path) -> io::Result<File> {
    let mut options = OpenOptions::new();
    options.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;

        options.mode(0o600);
    }
    options.open(path)
}

/// Creates `path` and restricts it to the owner. Permission failures are best effort.
fn secure_directory(path: &Path) -> io::Result<()> {
    fs::create_dir_all(path)?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;

        let _ = fs::set_permissions(path, fs::Permissions::from_mode(0o700));
    }

    Ok(())
}

/// Restricts an existing file to the owner. Permission failures are best effort.
fn secure_file(path: &Path) {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;

        let _ = fs::set_permissions(path, fs::Permissions::from_mode(0o600));
    }

    #[cfg(not(unix))]
    {
        let _ = path;
    }
}

/// Canonicalizes a path that may not exist yet by canonicalizing its parent and keeping the file name.
fn canonical_or_absolute(path: &Path) -> io::Result<PathBuf> {
    match path.canonicalize() {
        Ok(canonical) => Ok(canonical),
        Err(_) => {
            let parent = path
                .parent()
                .filter(|parent| !parent.as_os_str().is_empty())
                .unwrap_or(Path::new("."));
            let name = path
                .file_name()
                .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "path has no file name"))?;
            Ok(parent.canonicalize()?.join(name))
        }
    }
}

/// Moves the finished ZIP into place, replacing an earlier backup.
fn move_into_place(tmp_zip: &Path, target: &Path) -> io::Result<()> {
    match fs::rename(tmp_zip, target) {
        Ok(()) => Ok(()),
        Err(err) => {
            // Windows cannot rename over an existing file; mirror FinDesk's replace fallback there.
            if target.exists() {
                fs::remove_file(target)?;
                fs::rename(tmp_zip, target)
            } else {
                Err(err)
            }
        }
    }
}

/// Removes a folder when it exists.
fn remove_dir_all_if_exists(path: &Path) -> io::Result<()> {
    match fs::remove_dir_all(path) {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(err) => Err(err),
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use time::macros::date;

    use super::*;
    use crate::model::{AccountId, AccountType, NewAccount, NewTransaction};
    use crate::money::{Currency, Money};
    use crate::repo::{accounts, transactions};
    use crate::secret::PassphrasePurpose;

    const PASS: &str = "correct horse battery staple";

    // Not ported from FinDesk's BackupServiceTest:
    // - "documents still work after restoring into a different data directory": Finglass core has no document
    //   repository yet and no `findesk.home` system property; stored-path resolution belongs to a later task.
    // - Passport assertions in the round-trip and permission tests: Finglass has no `passports/` folder because it
    //   has no HBCI4Java.
    // - Plaintext backup/restore cases: Finglass has no plaintext databases.
    // - The shared-temp-dir connection injection: Finglass copies the database through the live connection with
    //   `VACUUM INTO`; the temporary copy is always inside the profile folder. The leak check is ported below.

    fn passphrase(value: &str) -> Passphrase {
        Passphrase::new(value, PassphrasePurpose::New).unwrap()
    }

    fn profile() -> (tempfile::TempDir, PathBuf, Database) {
        let temp = tempfile::tempdir().unwrap();
        let dir = temp.path().join("data");
        fs::create_dir_all(&dir).unwrap();
        let db = Database::create(&dir.join(DB_NAME), &passphrase(PASS)).unwrap();
        (temp, dir, db)
    }

    fn account(name: &str) -> NewAccount {
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some(name.to_owned()),
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
        }
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

    fn count_rows(db: &Database, table: &str) -> i64 {
        db.with_conn(|conn| {
            conn.query_row(&format!("SELECT count(*) FROM {table}"), [], |row| row.get(0))
                .map_err(DbError::from)
        })
        .unwrap()
    }

    fn write_zip(path: &Path, entries: &[(&str, &[u8])]) {
        let mut zip = ZipWriter::new(File::create(path).unwrap());
        let options = SimpleFileOptions::default().compression_method(CompressionMethod::Stored);
        for (name, data) in entries {
            zip.start_file(*name, options).unwrap();
            zip.write_all(data).unwrap();
        }
        zip.finish().unwrap();
    }

    fn zip_names(path: &Path) -> BTreeSet<String> {
        let mut zip = ZipArchive::new(File::open(path).unwrap()).unwrap();
        (0..zip.len())
            .map(|index| zip.by_index(index).unwrap().name().unwrap().into_owned())
            .collect()
    }

    fn zip_entry(path: &Path, name: &str) -> Vec<u8> {
        let mut zip = ZipArchive::new(File::open(path).unwrap()).unwrap();
        let mut entry = zip.by_name(name).unwrap();
        let mut bytes = Vec::new();
        entry.read_to_end(&mut bytes).unwrap();
        bytes
    }

    fn dir_has_suffix(dir: &Path, suffix: &str) -> bool {
        fs::read_dir(dir)
            .unwrap()
            .filter_map(Result::ok)
            .any(|entry| entry.file_name().to_string_lossy().ends_with(suffix))
    }

    #[cfg(unix)]
    fn mode(path: &Path) -> u32 {
        use std::os::unix::fs::PermissionsExt;

        fs::metadata(path).unwrap().permissions().mode() & 0o777
    }

    #[cfg(unix)]
    fn assert_owner_only(path: &Path) {
        assert_eq!(mode(path) & 0o077, 0, "{} is not owner-only", path.display());
    }

    #[test]
    fn the_database_name_matches_app_paths() {
        let paths = crate::paths::AppPaths::resolve(None, Some(PathBuf::from("/home/alice"))).unwrap();
        assert_eq!(paths.database_file("default").unwrap().file_name().unwrap(), DB_NAME);
    }

    #[test]
    fn backup_and_restore_round_trip_keeps_the_replaced_data() {
        let (temp, dir, db) = profile();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        add_transaction(&db, account_id, "Before backup", "before");

        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("receipt.pdf"), b"pdf-bytes").unwrap();

        let service = BackupService::new(dir.clone());
        let backup = temp.path().join("backup.zip");
        let info = service.create_backup(&db, &backup).unwrap();
        assert_eq!(info.transactions, 1);
        assert_eq!(info.documents, 1);
        assert_eq!(service.inspect(&backup).unwrap().transactions, 1);

        // Changes after the backup.
        add_transaction(&db, account_id, "After backup", "after");
        fs::write(documents.join("new.pdf"), b"new").unwrap();

        service.stage_restore(&db, &backup, None).unwrap();
        assert!(service.has_pending_restore());
        assert_eq!(partner_names(&db).len(), 2, "nothing changes before restart");

        // Restart.
        drop(db);
        let previous = service
            .apply_pending_restore(OffsetDateTime::now_utc())
            .unwrap()
            .expect("a restore was pending");
        assert!(!service.has_pending_restore());

        let reopened = Database::open(&dir.join(DB_NAME), &passphrase(PASS)).unwrap();
        assert_eq!(partner_names(&reopened), vec!["Before backup"]);
        assert_eq!(fs::read_to_string(documents.join("receipt.pdf")).unwrap(), "pdf-bytes");
        assert!(!documents.join("new.pdf").exists());
        assert!(
            previous.join("documents").join("new.pdf").exists(),
            "replaced data is kept"
        );
        assert!(previous.join(DB_NAME).is_file());
    }

    #[test]
    fn restore_moves_a_hot_rollback_journal_aside() {
        let (temp, dir, db) = profile();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        add_transaction(&db, account_id, "Before backup", "before");

        let service = BackupService::new(dir.clone());
        let backup = temp.path().join("journal.zip");
        service.create_backup(&db, &backup).unwrap();
        service.stage_restore(&db, &backup, None).unwrap();

        let journal = dir.join(format!("{DB_NAME}-journal"));
        let journal_bytes = [0x01, 0x02, 0x03];
        fs::write(&journal, journal_bytes).unwrap();

        drop(db);
        let previous = service
            .apply_pending_restore(OffsetDateTime::now_utc())
            .unwrap()
            .expect("a restore was pending");

        assert!(
            !journal.exists(),
            "the old journal must not stay next to the restored database"
        );
        assert_eq!(
            fs::read(previous.join(format!("{DB_NAME}-journal"))).unwrap(),
            journal_bytes
        );

        let reopened = Database::open(&dir.join(DB_NAME), &passphrase(PASS)).unwrap();
        assert_eq!(partner_names(&reopened), vec!["Before backup"]);
    }

    #[test]
    fn a_backup_saved_inside_documents_does_not_include_itself() {
        let (_temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("receipt.pdf"), b"pdf-bytes").unwrap();

        let service = BackupService::new(dir.clone());
        let target = documents.join("backup.zip");

        for _ in 0..2 {
            service.create_backup(&db, &target).unwrap();

            let entries = zip_names(&target);
            assert_eq!(
                entries,
                BTreeSet::from([
                    MANIFEST.to_owned(),
                    DB_NAME.to_owned(),
                    "documents/receipt.pdf".to_owned(),
                ])
            );
            assert!(entries.iter().all(|name| !name.ends_with(".zip.tmp")));
            assert!(!entries.contains("documents/backup.zip"));
            assert!(
                !dir_has_suffix(&documents, ".zip.tmp"),
                "no temporary ZIP left in documents/"
            );
        }
    }

    #[cfg(unix)]
    #[test]
    fn restored_files_and_directories_are_owner_only() {
        let (_temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("receipt.pdf"), b"pdf-bytes").unwrap();

        let service = BackupService::new(dir.clone());
        let backup = dir.parent().unwrap().join("restore-owner.zip");
        service.create_backup(&db, &backup).unwrap();

        service.stage_restore(&db, &backup, None).unwrap();
        let pending = dir.join(PENDING_DIR);
        assert_owner_only(&pending);
        assert_owner_only(&pending.join("documents"));
        assert_owner_only(&pending.join("documents").join("receipt.pdf"));
        assert_owner_only(&pending.join(DB_NAME));

        drop(db);
        service.apply_pending_restore(OffsetDateTime::now_utc()).unwrap();

        assert_owner_only(&documents);
        assert_owner_only(&documents.join("receipt.pdf"));
        assert_owner_only(&dir.join(DB_NAME));
    }

    #[test]
    fn rejects_foreign_zips() {
        let (temp, dir, _db) = profile();
        let service = BackupService::new(dir);

        let foreign = temp.path().join("random.zip");
        write_zip(&foreign, &[("a.txt", b"x")]);
        assert!(matches!(service.inspect(&foreign), Err(BackupError::NotABackup)));

        let no_database = temp.path().join("no-database.zip");
        write_zip(&no_database, &[(MANIFEST, b"created=x")]);
        assert!(matches!(service.inspect(&no_database), Err(BackupError::NoDatabase)));
    }

    #[test]
    fn rejects_zip_slip_and_unexpected_paths() {
        let (temp, dir, db) = profile();
        let service = BackupService::new(dir.clone());

        let cases = [
            ("../escape.txt", "escape.txt"),
            ("/abs.txt", "abs.txt"),
            ("documents/../../escape.txt", "escape.txt"),
            ("documents/a/b.txt", "b.txt"),
            ("other.txt", "other.txt"),
            ("..\\escape.txt", "escape.txt"),
        ];

        for (index, (evil, escaped_name)) in cases.iter().enumerate() {
            let path = temp.path().join(format!("evil-{index}.zip"));
            write_zip(&path, &[(MANIFEST, b"created=x"), (DB_NAME, b"x"), (evil, b"pwned")]);

            let result = service.stage_restore(&db, &path, None);
            assert!(
                matches!(result, Err(BackupError::UnsafePath(_))),
                "case {evil:?} should be rejected, got {result:?}"
            );
            assert!(!service.has_pending_restore(), "case {evil:?} left a staging folder");
            assert!(
                !temp.path().join(escaped_name).exists(),
                "case {evil:?} wrote outside the staging folder"
            );
            assert!(!dir.join(escaped_name).exists(), "case {evil:?} wrote into the profile");
        }
    }

    #[test]
    fn a_corrupt_backup_is_rejected_and_leaves_nothing_staged() {
        let (temp, dir, db) = profile();
        let service = BackupService::new(dir);

        let corrupt = temp.path().join("corrupt.zip");
        write_zip(&corrupt, &[(MANIFEST, b"created=x"), (DB_NAME, b"not a database")]);

        assert!(service.stage_restore(&db, &corrupt, None).is_err());
        assert!(!service.has_pending_restore());
    }

    #[test]
    fn a_staged_restore_can_be_cancelled() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();

        let service = BackupService::new(dir.clone());
        let backup = temp.path().join("cancel.zip");
        service.create_backup(&db, &backup).unwrap();
        service.stage_restore(&db, &backup, None).unwrap();
        service.cancel_pending_restore();

        assert!(!service.has_pending_restore());
        drop(db);
        assert!(
            service
                .apply_pending_restore(OffsetDateTime::now_utc())
                .unwrap()
                .is_none()
        );
    }

    #[test]
    fn a_restore_that_unpacks_more_than_the_cap_is_rejected() {
        let (temp, dir, db) = profile();
        let first = vec![b'a'; 600];
        let second = vec![b'b'; 600];
        let big = temp.path().join("big.zip");
        write_zip(
            &big,
            &[
                (MANIFEST, b"created=x"),
                (DB_NAME, b"x"),
                ("documents/a.pdf", &first),
                ("documents/b.pdf", &second),
            ],
        );

        let service = BackupService::new(dir.clone()).with_max_restore_bytes(1_000);
        let error = service.stage_restore(&db, &big, None).unwrap_err();

        assert!(matches!(error, BackupError::TooLarge));
        assert!(
            !BackupService::new(dir).has_pending_restore(),
            "failed staging leaves nothing behind"
        );
    }

    #[test]
    fn an_entry_larger_than_the_cap_is_rejected_while_copying() {
        let (temp, dir, db) = profile();
        let large = vec![b'x'; 2_000];
        let backup = temp.path().join("large-entry.zip");
        write_zip(
            &backup,
            &[
                (MANIFEST, b"created=x"),
                (DB_NAME, b"x"),
                ("documents/large.bin", &large),
            ],
        );

        let service = BackupService::new(dir.clone()).with_max_restore_bytes(1_000);
        let error = service.stage_restore(&db, &backup, None).unwrap_err();

        assert!(matches!(error, BackupError::TooLarge));
        assert!(
            !BackupService::new(dir).has_pending_restore(),
            "staging folder must be removed"
        );
    }

    #[test]
    fn a_backup_that_could_not_be_restored_is_refused() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        fs::write(documents.join("scan.pdf"), vec![0u8; 64 * 1024]).unwrap();

        let target = temp.path().join("too-large.zip");
        let service = BackupService::new(dir.clone()).with_max_restore_bytes(32 * 1024);
        let error = service.create_backup(&db, &target).unwrap_err();

        assert!(matches!(error, BackupError::TooLarge));
        assert!(!target.exists());
        assert!(!dir_has_suffix(&dir, ".db.tmp"), "temporary copy removed");
    }

    #[test]
    fn no_temporary_database_copy_is_left_behind() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();

        let target = temp.path().join("temporary.zip");
        BackupService::new(dir.clone()).create_backup(&db, &target).unwrap();

        assert!(target.is_file());
        assert!(!dir_has_suffix(&dir, ".db.tmp"), "temporary copy removed");
        assert!(!dir_has_suffix(&dir, ".zip.tmp"), "temporary ZIP removed");
    }

    #[cfg(unix)]
    #[test]
    fn the_backup_file_is_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();

        let target = temp.path().join("owner-existing.zip");
        fs::write(&target, b"old").unwrap();
        fs::set_permissions(&target, fs::Permissions::from_mode(0o644)).unwrap();

        BackupService::new(dir).create_backup(&db, &target).unwrap();

        assert_eq!(mode(&target), 0o600);
    }

    #[test]
    fn the_backups_database_entry_is_encrypted() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();

        let backup = temp.path().join("encrypted.zip");
        BackupService::new(dir).create_backup(&db, &backup).unwrap();

        let bytes = zip_entry(&backup, DB_NAME);
        assert!(bytes.len() >= 16);
        assert_ne!(
            &bytes[..16],
            b"SQLite format 3\0",
            "the backup database must stay encrypted"
        );

        let extracted = temp.path().join("extracted.db");
        fs::write(&extracted, &bytes).unwrap();

        drop(Database::open(&extracted, &passphrase(PASS)).unwrap());
        let error = match Database::open(&extracted, &passphrase("another very long passphrase")) {
            Ok(_) => panic!("a different passphrase must not open the backup database"),
            Err(error) => error,
        };
        assert!(matches!(error, DbError::WrongPassphraseOrDamaged));
    }

    #[test]
    fn a_backup_after_a_passphrase_change_on_the_same_connection_uses_the_new_key() {
        // The session keeps using the re-keyed connection after "change passphrase"; its backups must be encrypted
        // with the new passphrase, or the user would find out only when restoring.
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let new_passphrase = passphrase("another very long passphrase");
        db.change_passphrase(&new_passphrase).unwrap();

        let service = BackupService::new(dir);
        let backup = temp.path().join("after-change.zip");
        service.create_backup(&db, &backup).unwrap();

        let extracted = temp.path().join("after-change.db");
        fs::write(&extracted, zip_entry(&backup, DB_NAME)).unwrap();
        drop(Database::open(&extracted, &new_passphrase).unwrap());
        assert!(matches!(
            Database::open(&extracted, &passphrase(PASS)),
            Err(DbError::WrongPassphraseOrDamaged)
        ));

        service.stage_restore(&db, &backup, None).unwrap();
        assert!(service.has_pending_restore());
    }

    #[test]
    fn a_backup_made_before_a_passphrase_change_asks_for_the_old_one() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();

        let service = BackupService::new(dir);
        let backup = temp.path().join("old-passphrase.zip");
        service.create_backup(&db, &backup).unwrap();

        let new_passphrase = passphrase("another very long passphrase");
        db.change_passphrase(&new_passphrase).unwrap();

        let error = service.stage_restore(&db, &backup, None).unwrap_err();
        assert!(
            matches!(
                error,
                BackupError::PassphraseRequired {
                    wrong_passphrase_given: false
                }
            ),
            "unexpected error: {error:?}"
        );
        assert!(!service.has_pending_restore());

        let error = service
            .stage_restore(&db, &backup, Some(&passphrase("a completely wrong passphrase")))
            .unwrap_err();
        assert!(matches!(
            error,
            BackupError::PassphraseRequired {
                wrong_passphrase_given: true
            }
        ));
        assert!(!service.has_pending_restore());

        let info = service.stage_restore(&db, &backup, Some(&passphrase(PASS))).unwrap();
        assert_eq!(info.transactions, 0);
        assert!(service.has_pending_restore());
    }

    #[test]
    fn staging_does_not_change_the_live_database() {
        let (temp, dir, db) = profile();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        add_transaction(&db, account_id, "Before backup", "before");

        let service = BackupService::new(dir.clone());
        let backup = temp.path().join("unchanged.zip");
        service.create_backup(&db, &backup).unwrap();
        service.stage_restore(&db, &backup, None).unwrap();

        assert_eq!(partner_names(&db), vec!["Before backup"]);
        assert_eq!(count_rows(&db, "transactions"), 1);

        drop(db);
        let reopened = Database::open(&dir.join(DB_NAME), &passphrase(PASS)).unwrap();
        assert_eq!(partner_names(&reopened), vec!["Before backup"]);
    }

    #[test]
    fn a_staged_database_with_a_newer_schema_is_rejected() {
        let (temp, dir, db) = profile();
        db.with_conn(|conn| {
            conn.pragma_update(None, "user_version", 99u32)?;
            Ok(())
        })
        .unwrap();

        let service = BackupService::new(dir.clone());
        let backup = temp.path().join("too-new.zip");
        service.create_backup(&db, &backup).unwrap();

        let error = service.stage_restore(&db, &backup, None).unwrap_err();
        assert!(matches!(error, BackupError::TooNew));
        assert!(!service.has_pending_restore());
    }

    #[test]
    fn a_target_path_with_a_single_quote_works() {
        let (temp, dir, db) = profile();
        let account_id = accounts::create(&db, account("Account")).unwrap();
        add_transaction(&db, account_id, "Before backup", "before");

        let target = temp.path().join("O'Brien backup.zip");
        let info = BackupService::new(dir.clone()).create_backup(&db, &target).unwrap();

        assert_eq!(info.transactions, 1);
        assert!(target.is_file());
        assert_eq!(BackupService::new(dir).inspect(&target).unwrap().transactions, 1);
    }

    #[cfg(unix)]
    #[test]
    fn symlinks_in_content_directories_are_skipped() {
        use std::os::unix::fs::symlink;

        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();

        let outside = temp.path().join("outside-secret.txt");
        fs::write(&outside, b"secret").unwrap();
        if symlink(&outside, documents.join("link.txt")).is_err() {
            return;
        }
        fs::write(documents.join("regular.txt"), b"regular").unwrap();

        let target = temp.path().join("symlink.zip");
        let info = BackupService::new(dir).create_backup(&db, &target).unwrap();

        assert_eq!(info.documents, 1);
        let entries = zip_names(&target);
        assert!(entries.contains("documents/regular.txt"));
        assert!(!entries.contains("documents/link.txt"));
    }

    #[cfg(unix)]
    #[test]
    fn a_failed_backup_leaves_no_target_and_no_temp_zip() {
        use std::os::unix::fs::PermissionsExt;

        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let documents = dir.join("documents");
        fs::create_dir_all(&documents).unwrap();
        let bad = documents.join("unreadable.pdf");
        fs::write(&bad, b"x").unwrap();

        fs::set_permissions(&bad, fs::Permissions::from_mode(0o000)).unwrap();
        if fs::read(&bad).is_ok() {
            // Running as root: file permissions do not fail, so there is nothing to assert.
            fs::set_permissions(&bad, fs::Permissions::from_mode(0o600)).unwrap();
            return;
        }

        let target = temp.path().join("fail.zip");
        let result = BackupService::new(dir).create_backup(&db, &target);
        fs::set_permissions(&bad, fs::Permissions::from_mode(0o600)).unwrap();

        assert!(result.is_err());
        assert!(!target.exists(), "target must not exist on failure");
        assert!(
            !dir_has_suffix(target.parent().unwrap(), ".zip.tmp"),
            "no temp ZIP left behind"
        );
    }

    #[test]
    fn the_manifest_is_read_leniently() {
        let (temp, dir, db) = profile();
        accounts::create(&db, account("Account")).unwrap();
        let service = BackupService::new(dir);

        let backup = temp.path().join("manifest.zip");
        write_zip(
            &backup,
            &[
                (
                    MANIFEST,
                    b"# Finglass backup\ncreated=2026-01-01T00:00:00Z\nappVersion=9.9.9\ntransactions=?\ndocuments=not-a-number\n",
                ),
                (DB_NAME, b"x"),
            ],
        );

        let info = service.inspect(&backup).unwrap();
        assert_eq!(info.created, "2026-01-01T00:00:00Z");
        assert_eq!(info.app_version, "9.9.9");
        assert_eq!(info.transactions, 0);
        assert_eq!(info.documents, 0);
    }
}
