//! Key/value settings stored in the encrypted database.

use crate::db::{Database, DbError};

/// Reads a setting, or `None` when the key is absent.
pub fn get_setting(db: &Database, key: &str) -> Result<Option<String>, DbError> {
    db.with_conn(|conn| {
        let mut statement = conn.prepare("SELECT value FROM settings WHERE key = ?1")?;
        let mut rows = statement.query([key])?;

        match rows.next()? {
            Some(row) => Ok(Some(row.get(0)?)),
            None => Ok(None),
        }
    })
}

/// Writes a setting, replacing an existing value.
pub fn set_setting(db: &Database, key: &str, value: &str) -> Result<(), DbError> {
    db.with_conn(|conn| {
        conn.execute(
            "INSERT INTO settings (key, value) VALUES (?1, ?2)
             ON CONFLICT (key) DO UPDATE SET value = excluded.value",
            (key, value),
        )?;
        Ok(())
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
    fn a_setting_round_trips_and_can_be_overwritten() {
        let (_temp, db) = database();

        set_setting(&db, "language", "en").unwrap();
        assert_eq!(get_setting(&db, "language").unwrap(), Some("en".to_owned()));

        set_setting(&db, "language", "de").unwrap();
        assert_eq!(get_setting(&db, "language").unwrap(), Some("de".to_owned()));
    }

    #[test]
    fn a_missing_key_is_none() {
        let (_temp, db) = database();
        assert_eq!(get_setting(&db, "missing").unwrap(), None);
    }
}
