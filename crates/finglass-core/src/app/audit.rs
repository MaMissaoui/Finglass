//! Audit log rows ready for the settings screen.

use time::OffsetDateTime;
use time::UtcOffset;
use time::format_description::well_known::Rfc3339;
use time::macros::format_description;

use crate::audit::{self, AuditEntry};
use crate::db::{Database, DbError};

/// One audit log row, with its timestamp already rendered in local time.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditRow {
    /// Local date and time, for example `2026-11-07 14:05`.
    pub time: String,
    /// The stored action name.
    pub action: String,
    /// The stored actor name.
    pub actor: String,
    /// The stored details.
    pub details: String,
}

impl From<AuditEntry> for AuditRow {
    fn from(entry: AuditEntry) -> Self {
        Self {
            time: local_time(&entry.at),
            action: entry.action,
            actor: entry.actor.as_str().to_owned(),
            details: entry.details,
        }
    }
}

/// Reads the newest `limit` entries, newest first.
pub fn recent(db: &Database, limit: u32) -> Result<Vec<AuditRow>, DbError> {
    Ok(audit::recent(db, limit)?.into_iter().map(AuditRow::from).collect())
}

fn local_time(timestamp: &str) -> String {
    let Ok(utc) = OffsetDateTime::parse(timestamp, &Rfc3339) else {
        return timestamp.to_owned();
    };
    let offset = UtcOffset::current_local_offset().unwrap_or(UtcOffset::UTC);
    let format = format_description!("[year]-[month]-[day] [hour]:[minute]");
    utc.to_offset(offset)
        .format(&format)
        .unwrap_or_else(|_| timestamp.to_owned())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::audit::{Actor, log};
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
    fn rows_keep_the_audit_order_and_values() {
        let (_temp, db) = database();
        log(&db, "first", Actor::User, "one").unwrap();
        log(&db, "second", Actor::Import, "two").unwrap();

        let rows = recent(&db, 100).unwrap();
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0].action, "second");
        assert_eq!(rows[0].actor, "import");
        assert_eq!(rows[0].details, "two");
        assert_eq!(rows[1].action, "first");
        assert_eq!(rows[1].actor, "user");
        assert!(!rows[0].time.is_empty());
    }

    #[test]
    fn local_time_keeps_an_unparseable_timestamp() {
        assert_eq!(local_time("not a time"), "not a time");
    }
}
