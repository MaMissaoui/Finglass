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
pub(crate) const MIGRATIONS: &[Migration] = &[Migration {
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
}];

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
    fn fresh_database_ends_at_version_1() {
        let mut conn = Connection::open_in_memory().unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();

        assert_eq!(version(&conn), 1);
        assert!(table_exists(&conn, "settings"));
        assert!(table_exists(&conn, "audit_log"));
    }

    #[test]
    fn running_again_is_a_no_op() {
        let mut conn = Connection::open_in_memory().unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();
        migrate(&mut conn, MIGRATIONS).unwrap();
        assert_eq!(version(&conn), 1);
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
                supported: 1
            }
        ));
        assert_eq!(version(&conn), 99);
        assert!(!table_exists(&conn, "settings"));
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
