//! The audit log.
//!
//! Every user-visible change is recorded here. Details must never contain secrets or bank message text.

use time::OffsetDateTime;
use time::format_description::well_known::Rfc3339;

use crate::db::{Database, DbError};

/// Who caused an audit entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Actor {
    /// The person using the app.
    User,
    /// A file import.
    Import,
    /// A bank sync.
    Sync,
    /// The app itself, for example a migration or cleanup.
    System,
}

impl Actor {
    pub(crate) const fn as_str(self) -> &'static str {
        match self {
            Self::User => "user",
            Self::Import => "import",
            Self::Sync => "sync",
            Self::System => "system",
        }
    }

    fn from_stored(value: &str) -> Self {
        match value {
            "user" => Self::User,
            "import" => Self::Import,
            "sync" => Self::Sync,
            _ => Self::System,
        }
    }
}

/// One audit log entry, as read back from the database.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditEntry {
    /// The row id.
    pub id: i64,
    /// When it happened, as RFC 3339 in UTC.
    pub at: String,
    /// What happened.
    pub action: String,
    /// Who did it.
    pub actor: Actor,
    /// Extra context. Must never contain secrets or bank message text.
    pub details: String,
}

/// Appends an entry with the current UTC time.
///
/// `details` must never contain secrets or bank message text.
pub fn log(db: &Database, action: &str, actor: Actor, details: &str) -> Result<(), DbError> {
    let now = OffsetDateTime::now_utc()
        .format(&Rfc3339)
        .map_err(|err| DbError::Io(std::io::Error::other(err)))?;

    db.with_conn(|conn| {
        conn.execute(
            "INSERT INTO audit_log (at, action, actor, details) VALUES (?1, ?2, ?3, ?4)",
            (now, action, actor.as_str(), details),
        )?;
        Ok(())
    })
}

/// Reads the newest entries first.
pub fn recent(db: &Database, limit: u32) -> Result<Vec<AuditEntry>, DbError> {
    db.with_conn(|conn| {
        let mut statement =
            conn.prepare("SELECT id, at, action, actor, details FROM audit_log ORDER BY id DESC LIMIT ?1")?;
        let rows = statement.query_map([limit], |row| {
            let actor: String = row.get(3)?;
            Ok(AuditEntry {
                id: row.get(0)?,
                at: row.get(1)?,
                action: row.get(2)?,
                actor: Actor::from_stored(&actor),
                details: row.get(4)?,
            })
        })?;

        let mut entries = Vec::new();
        for row in rows {
            entries.push(row?);
        }
        Ok(entries)
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::Database;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    #[test]
    fn recent_returns_the_newest_entries_first() {
        let (_temp, db) = database();

        log(&db, "first", Actor::User, "one").unwrap();
        log(&db, "second", Actor::Import, "two").unwrap();
        log(&db, "third", Actor::Sync, "three").unwrap();

        let entries = recent(&db, 2).unwrap();
        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].action, "third");
        assert_eq!(entries[0].actor, Actor::Sync);
        assert_eq!(entries[0].details, "three");
        assert_eq!(entries[1].action, "second");
        assert_eq!(entries[1].actor, Actor::Import);
        assert_eq!(entries[1].details, "two");
        assert_eq!(entries[0].actor.as_str(), "sync");
    }

    #[test]
    fn an_entry_has_a_rfc3339_utc_timestamp() {
        let (_temp, db) = database();
        log(&db, "opened", Actor::System, "").unwrap();

        let entry = &recent(&db, 1).unwrap()[0];
        assert!(entry.at.len() >= 20, "unexpected timestamp {}", entry.at);
        assert!(entry.at.ends_with('Z'));
        assert_eq!(&entry.at[4..5], "-");
        assert_eq!(&entry.at[7..8], "-");
        assert_eq!(&entry.at[10..11], "T");
        assert_eq!(&entry.at[13..14], ":");
        assert_eq!(&entry.at[16..17], ":");
    }

    #[test]
    fn actors_are_stored_as_lowercase_text() {
        let (_temp, db) = database();
        for actor in [Actor::User, Actor::Import, Actor::Sync, Actor::System] {
            log(&db, "check", actor, "").unwrap();
        }

        db.with_conn(|conn| {
            let mut statement = conn.prepare("SELECT actor FROM audit_log ORDER BY id")?;
            let stored: Vec<String> = statement
                .query_map([], |row| row.get(0))?
                .collect::<rusqlite::Result<_>>()?;
            assert_eq!(stored, ["user", "import", "sync", "system"]);
            Ok(())
        })
        .unwrap();
    }
}
