//! Versioned schema migrations.
//!
//! The version lives in `PRAGMA user_version`. Versions start at 1, rise by one, and **are never edited after
//! release**: a released migration is history, so fixes go into a new migration.

use rusqlite::Connection;

use super::error::DbError;

/// One schema step. Released steps must never be edited.
pub(crate) struct Migration {
    pub(crate) version: u32,
    #[allow(dead_code)]
    pub(crate) name: &'static str,
    pub(crate) sql: &'static str,
}

/// The migrations this build knows about.
pub(crate) const MIGRATIONS: &[Migration] = &[
    Migration {
        version: 1,
        name: "initial",
        sql: "CREATE TABLE settings (
        key   TEXT PRIMARY KEY NOT NULL,
        value TEXT NOT NULL
    ) STRICT;
    CREATE TABLE audit_log (
        id      INTEGER PRIMARY KEY,
        at      TEXT NOT NULL,
        action  TEXT NOT NULL,
        actor   TEXT NOT NULL,
        details TEXT NOT NULL
    ) STRICT;
    CREATE INDEX audit_log_at ON audit_log (at);",
    },
    Migration {
        version: 2,
        name: "domain",
        sql: "CREATE TABLE account_groups (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE bank_connections (
    id                  INTEGER PRIMARY KEY,
    bank_name           TEXT NOT NULL,
    blz                 TEXT NOT NULL,
    bic                 TEXT,
    user_id             TEXT NOT NULL,
    server_url          TEXT,
    tan_method          TEXT,
    sync_from           TEXT,
    last_sync           TEXT,
    capabilities        TEXT,
    include_in_sync_all INTEGER NOT NULL DEFAULT 1 CHECK (include_in_sync_all IN (0, 1))
) STRICT;

CREATE TABLE accounts (
    id                 INTEGER PRIMARY KEY,
    bank_connection_id INTEGER REFERENCES bank_connections (id) ON DELETE CASCADE,
    institution        TEXT,
    name               TEXT,
    iban               TEXT,
    account_number     TEXT,
    currency           TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    account_type       TEXT NOT NULL DEFAULT 'checking'
                       CHECK (account_type IN ('checking', 'savings', 'credit_card', 'loan', 'depot')),
    balance_minor      INTEGER NOT NULL DEFAULT 0,
    balance_updated_at TEXT,
    group_id           INTEGER REFERENCES account_groups (id) ON DELETE SET NULL
) STRICT;
CREATE INDEX accounts_iban ON accounts (iban);

CREATE TABLE categories (
    id                 INTEGER PRIMARY KEY,
    name               TEXT NOT NULL,
    color              TEXT NOT NULL DEFAULT '#3BC7B0',
    budget_limit_minor INTEGER,
    is_tax_relevant    INTEGER NOT NULL DEFAULT 0 CHECK (is_tax_relevant IN (0, 1)),
    is_transfer        INTEGER NOT NULL DEFAULT 0 CHECK (is_transfer IN (0, 1))
) STRICT;

CREATE TABLE rules (
    id           INTEGER PRIMARY KEY,
    pattern      TEXT NOT NULL,
    category_id  INTEGER NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    priority     INTEGER NOT NULL DEFAULT 0,
    auto_apply   INTEGER NOT NULL DEFAULT 1 CHECK (auto_apply IN (0, 1)),
    match_field  TEXT NOT NULL DEFAULT 'any' CHECK (match_field IN ('any', 'partner', 'purpose')),
    direction    TEXT NOT NULL DEFAULT 'any' CHECK (direction IN ('any', 'expense', 'income')),
    tax_relevant INTEGER CHECK (tax_relevant IN (0, 1))
) STRICT;

CREATE TABLE transactions (
    id                  INTEGER PRIMARY KEY,
    account_id          INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    booking_date        TEXT NOT NULL,
    amount_minor        INTEGER NOT NULL,
    currency            TEXT NOT NULL CHECK (length(currency) = 3),
    balance_after_minor INTEGER,
    partner_name        TEXT,
    purpose             TEXT,
    category_id         INTEGER REFERENCES categories (id) ON DELETE SET NULL,
    import_hash         TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    is_new              INTEGER NOT NULL DEFAULT 0 CHECK (is_new IN (0, 1)),
    tax_override        INTEGER CHECK (tax_override IN (0, 1))
) STRICT;
CREATE INDEX transactions_account_date ON transactions (account_id, booking_date);
CREATE INDEX transactions_account_hash ON transactions (account_id, import_hash);
CREATE INDEX transactions_category ON transactions (category_id);

CREATE TABLE deleted_bookings (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    import_hash TEXT NOT NULL,
    deleted_at  TEXT NOT NULL
) STRICT;
CREATE INDEX deleted_bookings_account_hash ON deleted_bookings (account_id, import_hash);

CREATE TABLE pending_bookings (
    id           INTEGER PRIMARY KEY,
    account_id   INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    booking_date TEXT NOT NULL,
    value_date   TEXT,
    amount_minor INTEGER NOT NULL,
    currency     TEXT NOT NULL CHECK (length(currency) = 3),
    partner_name TEXT,
    purpose      TEXT,
    fetched_at   TEXT NOT NULL
) STRICT;

CREATE TABLE documents (
    id             INTEGER PRIMARY KEY,
    file_name      TEXT NOT NULL,
    stored_path    TEXT NOT NULL,
    mime_type      TEXT NOT NULL,
    uploaded_at    TEXT NOT NULL,
    transaction_id INTEGER REFERENCES transactions (id) ON DELETE SET NULL
) STRICT;
CREATE INDEX documents_transaction ON documents (transaction_id);

CREATE TABLE ocr_texts (
    id          INTEGER PRIMARY KEY,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT 'de',
    confidence  REAL
) STRICT;",
    },
];

/// Applies every migration newer than the database's `user_version`.
///
/// Each step and its version bump commit together. A failing step is rolled back on its own; earlier steps stay
/// committed.
pub(crate) fn migrate(conn: &mut Connection, migrations: &[Migration]) -> Result<(), DbError> {
    let current: u32 = conn.query_row("PRAGMA user_version", [], |row| row.get(0))?;
    let supported = migrations.iter().map(|m| m.version).max().unwrap_or(0);

    if current > supported {
        return Err(DbError::TooNew {
            found: current,
            supported,
        });
    }

    for migration in migrations.iter().filter(|m| m.version > current) {
        let version = migration.version;
        let tx = conn.transaction()?;

        if let Err(err) = tx.execute_batch(migration.sql) {
            return Err(DbError::Migration { version, source: err });
        }

        if let Err(err) = tx.pragma_update(None, "user_version", version) {
            return Err(DbError::Migration { version, source: err });
        }

        if let Err(err) = tx.commit() {
            return Err(DbError::Migration { version, source: err });
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::Connection;

    fn version(conn: &Connection) -> u32 {
        conn.query_row("PRAGMA user_version", [], |row| row.get(0)).unwrap()
    }

    fn table_exists(conn: &Connection, name: &str) -> bool {
        conn.query_row(
            "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?1",
            [name],
            |row| row.get::<_, i64>(0),
        )
        .unwrap()
            > 0
    }

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

    #[test]
    fn fresh_database_ends_at_version_2() {
        let mut conn = Connection::open_in_memory().unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();

        assert_eq!(version(&conn), 2);
        assert!(table_exists(&conn, "settings"));
        assert!(table_exists(&conn, "audit_log"));
        assert!(table_exists(&conn, "accounts"));
        assert!(table_exists(&conn, "transactions"));
    }

    #[test]
    fn running_again_is_a_no_op() {
        let mut conn = Connection::open_in_memory().unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();
        assert_eq!(version(&conn), 2);
    }

    #[test]
    fn a_newer_database_is_refused_unchanged() {
        let mut conn = Connection::open_in_memory().unwrap();
        conn.pragma_update(None, "user_version", 99).unwrap();

        let err = migrate(&mut conn, MIGRATIONS).unwrap_err();
        assert!(matches!(
            err,
            DbError::TooNew {
                found: 99,
                supported: 2
            }
        ));
        assert_eq!(version(&conn), 99);
        assert!(!table_exists(&conn, "settings"));
    }

    #[test]
    fn a_version_1_database_migrates_to_version_2_on_open() {
        use crate::db::Database;
        use crate::secret::{Passphrase, PassphrasePurpose};

        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();

        let db = Database::create_with_migrations(&path, &pass, &MIGRATIONS[..1]).unwrap();
        db.with_conn(|conn| {
            assert_eq!(version(conn), 1);
            assert!(!table_exists(conn, "accounts"));
            Ok(())
        })
        .unwrap();
        drop(db);

        let db = Database::open(&path, &pass).unwrap();
        db.with_conn(|conn| {
            assert_eq!(version(conn), 2);
            assert!(table_exists(conn, "accounts"));
            Ok(())
        })
        .unwrap();
    }

    #[test]
    fn a_failing_step_rolls_back_only_itself() {
        let mut conn = Connection::open_in_memory().unwrap();

        let err = migrate(&mut conn, &[OK_V1, FAIL_V2]).unwrap_err();
        assert!(
            matches!(err, DbError::Migration { version: 2, .. }),
            "expected migration 2 to fail"
        );

        assert_eq!(version(&conn), 1);
        assert!(table_exists(&conn, "first"));
        assert!(!table_exists(&conn, "second"));
    }
}
