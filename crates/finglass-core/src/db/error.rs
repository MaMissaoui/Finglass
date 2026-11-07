//! Errors from opening, creating and using the encrypted database.

use thiserror::Error;

/// Everything that can go wrong around the database.
///
/// No variant carries a passphrase or SQL text that contains one.
#[derive(Debug, Error)]
pub enum DbError {
    /// The database file does not exist.
    #[error("database file not found")]
    NotFound,

    /// The database file already exists.
    #[error("database file already exists")]
    AlreadyExists,

    /// The file has a plain SQLite header instead of an encrypted SQLCipher file.
    #[error("database file is not encrypted")]
    NotEncrypted,

    /// The passphrase is wrong, or the file is damaged. SQLCipher cannot tell the two apart.
    #[error("wrong passphrase or damaged database")]
    WrongPassphraseOrDamaged,

    /// The connection mutex was poisoned.
    #[error("database connection is unavailable")]
    Poisoned,

    /// The database was created by a newer version of Finglass.
    #[error("database version {found} is newer than supported version {supported}")]
    TooNew {
        /// The version found in the file.
        found: u32,
        /// The newest version this build knows about.
        supported: u32,
    },

    /// A migration step failed.
    #[error("migration {version} failed")]
    Migration {
        /// The migration version that failed.
        version: u32,
        /// The underlying SQLite error.
        source: rusqlite::Error,
    },

    /// An I/O error, for example while creating the folder or moving the temporary file.
    #[error("database I/O error")]
    Io(#[from] std::io::Error),

    /// A SQLite error.
    #[error("database error")]
    Sqlite(#[from] rusqlite::Error),
}
