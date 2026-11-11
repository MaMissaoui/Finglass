//! The encrypted database.
//!
//! Finglass deliberately keeps **one connection** per open database, guarded by a mutex. The passphrase is dropped
//! right after opening, so it does not stay in memory for the whole session. FinDesk uses a connection pool; that is
//! not worth the extra copies of the key here, where the app is a single local process with light database work.
//!
//! Known limit: `PRAGMA key` is sent through `rusqlite::Connection::pragma_update`, which builds the statement text
//! (including the key) in memory. The code cannot wipe that buffer. Never install `trace`, `profile` or any other SQL
//! logging hook on the connection.

mod error;
#[cfg(test)]
mod go_fixtures;
pub(crate) mod migrations;

use std::fs::{self, File};
use std::io::Read;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use rusqlite::{Connection, ErrorCode};

pub use self::error::DbError;
use self::migrations::{MIGRATIONS, Migration, migrate};
use crate::secret::Passphrase;

/// The plain SQLite file header. A SQLCipher file never starts with it.
const SQLITE_HEADER: &[u8; 16] = b"SQLite format 3\0";

/// An open encrypted database with one connection.
pub struct Database {
    conn: Mutex<Connection>,
}

impl Database {
    /// Creates a new encrypted database at `path`.
    ///
    /// The database is built in a temporary file next to `path`, migrated and then renamed into place, so a failure
    /// leaves nothing behind.
    pub fn create(path: &Path, passphrase: &Passphrase) -> Result<Self, DbError> {
        Self::create_with_migrations(path, passphrase, MIGRATIONS)
    }

    /// Opens an existing encrypted database at `path`.
    pub fn open(path: &Path, passphrase: &Passphrase) -> Result<Self, DbError> {
        Self::open_with_migrations(path, passphrase, MIGRATIONS)
    }

    /// Changes the passphrase. The caller has already verified the current passphrase by opening the database.
    pub fn change_passphrase(&self, new: &Passphrase) -> Result<(), DbError> {
        self.with_conn(|conn| {
            conn.pragma_update(None, "rekey", new.expose())?;
            Ok(())
        })
    }

    /// The only access to the connection.
    pub fn with_conn<T>(&self, f: impl FnOnce(&Connection) -> Result<T, DbError>) -> Result<T, DbError> {
        let guard = self.conn.lock().map_err(|_| DbError::Poisoned)?;
        f(&guard)
    }

    fn create_with_migrations(path: &Path, passphrase: &Passphrase, migrations: &[Migration]) -> Result<Self, DbError> {
        if path.exists() {
            return Err(DbError::AlreadyExists);
        }

        let temp = temp_database_path(path);

        let build = || -> Result<(), DbError> {
            let mut conn = Connection::open(&temp)?;
            conn.pragma_update(None, "key", passphrase.expose())?;
            conn.pragma_update(None, "foreign_keys", "ON")?;
            migrate(&mut conn, migrations)?;
            drop(conn);
            // Owner-only before it gets its final name, not just after the first open.
            restrict_file_permissions(&temp)?;
            fs::rename(&temp, path)?;
            Ok(())
        };

        if let Err(err) = build() {
            cleanup_temp(&temp);
            return Err(err);
        }

        Self::open_with_migrations(path, passphrase, migrations)
    }

    /// Writes a consistent, compacted copy of the database to `target`.
    ///
    /// `VACUUM INTO` runs through the live connection, so it uses the database's key and never sees the passphrase.
    /// The target path is bound as a parameter, never formatted into SQL, so quotes in the path are harmless.
    pub fn vacuum_into(&self, target: &Path) -> Result<(), DbError> {
        let target = target.to_string_lossy().into_owned();

        self.with_conn(|conn| {
            conn.execute("VACUUM INTO ?1", [target.as_str()])?;
            Ok(())
        })
    }

    /// Checks whether `file` can be read with the key of this live connection.
    ///
    /// The file is attached **without** a `KEY` clause, so SQLCipher tries the main database's key. A `NOTADB`
    /// error means the key does not fit (or the file is not a database) and yields `false`. The attachment is
    /// always removed, also when reading fails. A database whose schema is newer than this build is refused with
    /// [`DbError::TooNew`].
    pub fn can_read_with_current_key(&self, file: &Path) -> Result<bool, DbError> {
        let file = file.to_string_lossy().into_owned();

        self.with_conn(|conn| {
            match conn.execute("ATTACH DATABASE ?1 AS staged", [file.as_str()]) {
                Ok(_) => {}
                // SQLCipher can reject the key already while attaching.
                Err(rusqlite::Error::SqliteFailure(err, _)) if err.code == ErrorCode::NotADatabase => {
                    return Ok(false);
                }
                Err(err) => return Err(DbError::Sqlite(err)),
            }

            let result = (|| -> Result<bool, DbError> {
                match conn.query_row("SELECT count(*) FROM staged.sqlite_master", [], |row| {
                    row.get::<_, i64>(0)
                }) {
                    Ok(_) => {
                        let found: u32 = conn.query_row("PRAGMA staged.user_version", [], |row| row.get(0))?;
                        let supported = MIGRATIONS.iter().map(|migration| migration.version).max().unwrap_or(0);

                        if found > supported {
                            return Err(DbError::TooNew { found, supported });
                        }

                        Ok(true)
                    }
                    Err(rusqlite::Error::SqliteFailure(err, _)) if err.code == ErrorCode::NotADatabase => Ok(false),
                    Err(err) => Err(DbError::Sqlite(err)),
                }
            })();

            let detached = conn.execute_batch("DETACH DATABASE staged");

            match result {
                Ok(readable) => {
                    detached?;
                    Ok(readable)
                }
                Err(err) => {
                    // The original error is the useful one; a detach failure must not mask it.
                    let _ = detached;
                    Err(err)
                }
            }
        })
    }

    fn open_with_migrations(path: &Path, passphrase: &Passphrase, migrations: &[Migration]) -> Result<Self, DbError> {
        if !path.exists() {
            return Err(DbError::NotFound);
        }

        if has_sqlite_header(path)? {
            return Err(DbError::NotEncrypted);
        }

        let mut conn = Connection::open(path)?;

        // The key must be the first statement on the connection.
        conn.pragma_update(None, "key", passphrase.expose())?;

        // Reading the schema forces SQLCipher to decrypt page 1, which is how a wrong key surfaces.
        match conn.query_row("SELECT count(*) FROM sqlite_master", [], |row| row.get::<_, i64>(0)) {
            Ok(_) => {}
            Err(rusqlite::Error::SqliteFailure(err, _)) if err.code == ErrorCode::NotADatabase => {
                return Err(DbError::WrongPassphraseOrDamaged);
            }
            Err(err) => return Err(DbError::Sqlite(err)),
        }

        conn.pragma_update(None, "foreign_keys", "ON")?;
        migrate(&mut conn, migrations)?;
        restrict_file_permissions(path)?;

        Ok(Self { conn: Mutex::new(conn) })
    }
}

/// Reads the first 16 bytes and checks for the plain SQLite header.
fn has_sqlite_header(path: &Path) -> Result<bool, DbError> {
    let mut file = File::open(path)?;
    let mut header = [0u8; 16];
    let mut filled = 0;

    while filled < header.len() {
        let read = file.read(&mut header[filled..])?;
        if read == 0 {
            break;
        }
        filled += read;
    }

    Ok(filled == header.len() && &header == SQLITE_HEADER)
}

/// Builds a unique temporary path in the same folder as the final file.
fn temp_database_path(path: &Path) -> PathBuf {
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    let file_name = path.file_name().and_then(|name| name.to_str()).unwrap_or("finglass.db");
    let parent = path.parent().unwrap_or_else(|| Path::new("."));
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos())
        .unwrap_or(0);
    let counter = COUNTER.fetch_add(1, Ordering::Relaxed);

    parent.join(format!("{file_name}.creating-{}-{nanos}-{counter}", std::process::id()))
}

/// Removes a temporary database and the side files SQLite may have made.
fn cleanup_temp(path: &Path) {
    let _ = fs::remove_file(path);

    for suffix in ["-journal", "-wal", "-shm"] {
        let mut side = path.as_os_str().to_os_string();
        side.push(suffix);
        let _ = fs::remove_file(PathBuf::from(side));
    }
}

/// Tightens the file mode to 0600 when it is readable by group or others.
#[cfg(unix)]
fn restrict_file_permissions(path: &Path) -> Result<(), DbError> {
    use std::os::unix::fs::PermissionsExt;

    let metadata = fs::metadata(path)?;
    let mode = metadata.permissions().mode() & 0o777;

    if mode & 0o077 != 0 {
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))?;
    }

    Ok(())
}

#[cfg(not(unix))]
fn restrict_file_permissions(_path: &Path) -> Result<(), DbError> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::secret::PassphrasePurpose;

    fn passphrase(value: &str) -> Passphrase {
        Passphrase::new(value, PassphrasePurpose::New).unwrap()
    }

    fn unlock(value: &str) -> Passphrase {
        Passphrase::new(value, PassphrasePurpose::Unlock).unwrap()
    }

    fn expect_err<T>(result: Result<T, DbError>) -> DbError {
        match result {
            Ok(_) => panic!("expected an error"),
            Err(err) => err,
        }
    }

    fn table_exists(db: &Database, name: &str) -> bool {
        db.with_conn(|conn| {
            conn.query_row(
                "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?1",
                [name],
                |row| row.get::<_, i64>(0),
            )
            .map(|count| count > 0)
            .map_err(DbError::from)
        })
        .unwrap()
    }

    #[test]
    fn create_then_open_with_the_same_passphrase() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = passphrase("correct horse battery staple");

        let db = Database::create(&path, &pass).unwrap();
        assert!(table_exists(&db, "settings"));
        assert!(table_exists(&db, "audit_log"));
        drop(db);

        let db = Database::open(&path, &pass).unwrap();
        assert!(table_exists(&db, "settings"));
    }

    #[test]
    fn the_file_is_encrypted_on_disk() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        Database::create(&path, &passphrase("correct horse battery staple")).unwrap();

        let bytes = fs::read(&path).unwrap();
        assert!(bytes.len() >= 16);
        assert_ne!(&bytes[..16], SQLITE_HEADER.as_slice());
    }

    #[test]
    fn create_refuses_an_existing_path() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = passphrase("correct horse battery staple");

        Database::create(&path, &pass).unwrap();
        let err = expect_err(Database::create(&path, &pass));
        assert!(matches!(err, DbError::AlreadyExists));
    }

    #[test]
    fn a_wrong_passphrase_is_reported() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        Database::create(&path, &passphrase("correct horse battery staple")).unwrap();

        let err = expect_err(Database::open(&path, &unlock("wrong passphrase")));
        assert!(matches!(err, DbError::WrongPassphraseOrDamaged));
    }

    #[test]
    fn a_plain_sqlite_file_is_not_encrypted() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        {
            let conn = Connection::open(&path).unwrap();
            conn.execute_batch("CREATE TABLE plain (id INTEGER);").unwrap();
        }

        let err = expect_err(Database::open(&path, &unlock("whatever")));
        assert!(matches!(err, DbError::NotEncrypted));
    }

    #[test]
    fn a_missing_file_is_not_found() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("missing.db");

        let err = expect_err(Database::open(&path, &unlock("whatever")));
        assert!(matches!(err, DbError::NotFound));
    }

    #[test]
    fn change_passphrase_rekeys_the_database() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let old = passphrase("correct horse battery staple");
        let new = passphrase("another very long passphrase");

        let db = Database::create(&path, &old).unwrap();
        db.change_passphrase(&new).unwrap();
        drop(db);

        let err = expect_err(Database::open(&path, &old));
        assert!(matches!(err, DbError::WrongPassphraseOrDamaged));

        let db = Database::open(&path, &new).unwrap();
        assert!(table_exists(&db, "settings"));
    }

    #[test]
    fn errors_never_contain_the_passphrase() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let secret = "correct horse battery staple";
        let pass = passphrase(secret);

        let errors = vec![
            expect_err(Database::open(&temp.path().join("missing.db"), &unlock(secret))),
            {
                Database::create(&path, &pass).unwrap();
                expect_err(Database::create(&path, &pass))
            },
            expect_err(Database::open(&path, &unlock("wrong passphrase"))),
            {
                let plain = temp.path().join("plain.db");
                Connection::open(&plain)
                    .unwrap()
                    .execute_batch("CREATE TABLE plain (id INTEGER);")
                    .unwrap();
                expect_err(Database::open(&plain, &unlock(secret)))
            },
        ];

        for err in errors {
            let text = format!("{err} / {err:?}");
            assert!(!text.contains(secret), "error leaked the passphrase: {text}");
        }
    }

    #[cfg(unix)]
    #[test]
    fn the_database_file_is_private() {
        use std::os::unix::fs::PermissionsExt;

        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        Database::create(&path, &passphrase("correct horse battery staple")).unwrap();

        let mode = fs::metadata(&path).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    #[test]
    fn a_failed_create_leaves_nothing_behind() {
        const OK_V1: Migration = Migration {
            version: 1,
            name: "ok",
            sql: "CREATE TABLE first (id INTEGER);",
        };
        const FAIL_V2: Migration = Migration {
            version: 2,
            name: "failing",
            sql: "CREATE TABLE second (id INTEGER); THIS IS NOT VALID SQL;",
        };

        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = passphrase("correct horse battery staple");

        let err = expect_err(Database::create_with_migrations(&path, &pass, &[OK_V1, FAIL_V2]));
        assert!(
            matches!(err, DbError::Migration { version: 2, .. }),
            "expected migration 2 to fail"
        );

        assert!(!path.exists());
        assert_eq!(fs::read_dir(temp.path()).unwrap().count(), 0);
    }
}
