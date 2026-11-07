//! Bank connection repository.
//!
//! Finglass stores no HBCI version or bank passport file. An offline connection never goes online; its statements
//! are imported from files.

use rusqlite::{Connection, Row, params};
use time::{Date, OffsetDateTime};

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{BankAccessItem, BankConnection, BankConnectionId, BankConnectionUpdate, NewBankConnection};

use super::{
    RepoError, Result, format_date, format_timestamp, from_int_bool, id_and_name, id_only, in_conn, parse_date,
    parse_timestamp, to_int_bool,
};

/// Length of the `bank_name` column.
pub const MAX_BANK_NAME_LENGTH: usize = 255;

const COLUMNS: &str = "id, bank_name, blz, bic, user_id, server_url, tan_method, sync_from, last_sync, \
     capabilities, include_in_sync_all, offline";

fn from_row(row: &Row<'_>) -> Result<BankConnection> {
    let sync_from = row
        .get::<_, Option<String>>(7)?
        .map(|text| parse_date(&text))
        .transpose()?;
    let last_sync = row
        .get::<_, Option<String>>(8)?
        .map(|text| parse_timestamp(&text))
        .transpose()?;

    Ok(BankConnection {
        id: BankConnectionId(row.get(0)?),
        bank_name: row.get(1)?,
        blz: row.get(2)?,
        bic: row.get(3)?,
        user_id: row.get(4)?,
        server_url: row.get(5)?,
        tan_method: row.get(6)?,
        sync_from,
        last_sync,
        capabilities: row.get(9)?,
        include_in_sync_all: from_int_bool(row.get(10)?),
        offline: from_int_bool(row.get(11)?),
    })
}

fn to_item(connection: BankConnection) -> BankAccessItem {
    BankAccessItem {
        id: connection.id,
        bank_name: connection.bank_name,
        blz: connection.blz,
        bic: connection.bic,
        user_id: connection.user_id,
        tan_method: connection.tan_method,
        server_url: connection.server_url,
        last_sync: connection.last_sync,
        capabilities: connection.capabilities,
        sync_from: connection.sync_from,
        include_in_sync_all: connection.include_in_sync_all,
        offline: connection.offline,
    }
}

fn read_connection(conn: &Connection, id: i64) -> Result<Option<BankConnection>> {
    let sql = format!("SELECT {COLUMNS} FROM bank_connections WHERE id = ?1");
    let mut statement = conn.prepare(&sql)?;
    let mut rows = statement.query([id])?;

    match rows.next()? {
        Some(row) => Ok(Some(from_row(row)?)),
        None => Ok(None),
    }
}

fn read_item(conn: &Connection, id: i64) -> Result<Option<BankAccessItem>> {
    Ok(read_connection(conn, id)?.map(to_item))
}

fn truncate_name(name: &str) -> String {
    name.chars().take(MAX_BANK_NAME_LENGTH).collect()
}

fn insert(
    conn: &Connection,
    new: NewBankConnection,
    offline: bool,
    include_in_sync_all: bool,
    action: &str,
) -> Result<BankConnection> {
    conn.execute(
        "INSERT INTO bank_connections (
            bank_name, blz, bic, user_id, include_in_sync_all, offline
         ) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
        params![
            new.bank_name,
            new.blz,
            new.bic,
            new.user_id,
            to_int_bool(include_in_sync_all),
            to_int_bool(offline),
        ],
    )?;

    let id = conn.last_insert_rowid();
    audit::log_conn(conn, action, Actor::User, &id_and_name(id, &new.bank_name))?;
    read_connection(conn, id)?.ok_or(RepoError::NotFound)
}

/// Lists every connection by id.
pub fn list(db: &Database) -> Result<Vec<BankConnection>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!("SELECT {COLUMNS} FROM bank_connections ORDER BY id"))?;
        let mut rows = statement.query([])?;
        let mut connections = Vec::new();

        while let Some(row) = rows.next()? {
            connections.push(from_row(row)?);
        }

        Ok(connections)
    })
}

/// Reads one connection.
pub fn get(db: &Database, id: BankConnectionId) -> Result<Option<BankConnection>> {
    in_conn(db, |conn| read_connection(conn, id.0))
}

/// Lists the connections with this bank code.
pub fn find_by_blz(db: &Database, blz: &str) -> Result<Vec<BankConnection>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!(
            "SELECT {COLUMNS} FROM bank_connections WHERE blz = ?1 ORDER BY id"
        ))?;
        let mut rows = statement.query([blz])?;
        let mut connections = Vec::new();

        while let Some(row) = rows.next()? {
            connections.push(from_row(row)?);
        }

        Ok(connections)
    })
}

/// Creates an online connection.
pub fn create(db: &Database, new: NewBankConnection) -> Result<BankConnection> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let connection = insert(&transaction, new, false, true, "bank_connection.create")?;
        transaction.commit()?;
        Ok(connection)
    })
}

/// Creates a bank that never goes online, for example a credit card whose statements are imported from files.
pub fn create_offline(db: &Database, bank_name: &str) -> Result<BankConnection> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let connection = insert(
            &transaction,
            NewBankConnection {
                bank_name: bank_name.to_owned(),
                blz: String::new(),
                bic: Some(String::new()),
                user_id: String::new(),
            },
            true,
            false,
            "bank_connection.create_offline",
        )?;
        transaction.commit()?;
        Ok(connection)
    })
}

/// Changes the display name and/or TAN method. `None` leaves a value unchanged.
pub fn update(
    db: &Database,
    id: BankConnectionId,
    bank_name: Option<String>,
    tan_method: Option<String>,
) -> Result<Option<BankConnection>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let Some(current) = read_connection(&transaction, id.0)? else {
            audit::log_conn(&transaction, "bank_connection.update", Actor::User, &id_only(id.0))?;
            transaction.commit()?;
            return Ok(None);
        };

        let bank_name = bank_name.unwrap_or(current.bank_name);
        let tan_method = tan_method.or(current.tan_method);
        transaction.execute(
            "UPDATE bank_connections SET bank_name = ?1, tan_method = ?2 WHERE id = ?3",
            params![bank_name, tan_method, id.0],
        )?;

        audit::log_conn(
            &transaction,
            "bank_connection.update",
            Actor::User,
            &id_and_name(id.0, &bank_name),
        )?;
        let updated = read_connection(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Stores the current time as the last successful sync.
pub fn update_last_sync(db: &Database, id: BankConnectionId) -> Result<Option<BankConnection>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let at = format_timestamp(OffsetDateTime::now_utc())?;
        transaction.execute(
            "UPDATE bank_connections SET last_sync = ?1 WHERE id = ?2",
            params![at, id.0],
        )?;
        audit::log_conn(
            &transaction,
            "bank_connection.update_last_sync",
            Actor::User,
            &id_only(id.0),
        )?;
        let updated = read_connection(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Lists the UI snapshots of every connection.
pub fn list_items(db: &Database) -> Result<Vec<BankAccessItem>> {
    Ok(list(db)?.into_iter().map(to_item).collect())
}

/// Reads one UI snapshot.
pub fn get_item(db: &Database, id: BankConnectionId) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| read_item(conn, id.0))
}

/// Stores (or clears, with `None`) the start date the next sync fetches history from.
pub fn set_sync_from(db: &Database, id: BankConnectionId, date: Option<Date>) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let date = date.map(format_date).transpose()?;
        transaction.execute(
            "UPDATE bank_connections SET sync_from = ?1 WHERE id = ?2",
            params![date, id.0],
        )?;
        audit::log_conn(
            &transaction,
            "bank_connection.set_sync_from",
            Actor::User,
            &id_only(id.0),
        )?;
        let updated = read_item(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Stores whether "Sync all" syncs this connection.
pub fn set_include_in_sync_all(db: &Database, id: BankConnectionId, include: bool) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute(
            "UPDATE bank_connections SET include_in_sync_all = ?1 WHERE id = ?2",
            params![to_int_bool(include), id.0],
        )?;
        audit::log_conn(
            &transaction,
            "bank_connection.set_include_in_sync_all",
            Actor::User,
            &id_only(id.0),
        )?;
        let updated = read_item(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Clears the login's capabilities and remembered TAN method, without its own audit entry.
fn clear_login_state_conn(conn: &Connection, id: i64) -> Result<()> {
    conn.execute(
        "UPDATE bank_connections SET capabilities = NULL, tan_method = NULL WHERE id = ?1",
        [id],
    )?;
    Ok(())
}

/// Clears the login's capabilities and remembered TAN method after the login changed.
pub fn clear_login_state(db: &Database, id: BankConnectionId) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        clear_login_state_conn(&transaction, id.0)?;
        audit::log_conn(
            &transaction,
            "bank_connection.clear_login_state",
            Actor::User,
            &id_only(id.0),
        )?;
        let updated = read_item(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Stores the login's supported business transactions. A blank value clears the column.
pub fn update_capabilities(db: &Database, id: BankConnectionId, capabilities: &str) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let capabilities = capabilities.trim();
        let capabilities = if capabilities.is_empty() {
            None
        } else {
            Some(capabilities)
        };
        transaction.execute(
            "UPDATE bank_connections SET capabilities = ?1 WHERE id = ?2",
            params![capabilities, id.0],
        )?;
        audit::log_conn(
            &transaction,
            "bank_connection.update_capabilities",
            Actor::User,
            &id_only(id.0),
        )?;
        let updated = read_item(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

/// Stores the changed connection settings, clearing the login state when BLZ, user id or server URL changed.
///
/// The name is only a label: renaming changes nothing about the connection. A blank BIC stores an empty BIC;
/// a blank `bank_name` leaves the stored name alone.
pub fn update_connection(
    db: &Database,
    id: BankConnectionId,
    update: BankConnectionUpdate,
) -> Result<Option<BankAccessItem>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let Some(current) = read_connection(&transaction, id.0)? else {
            audit::log_conn(
                &transaction,
                "bank_connection.update_connection",
                Actor::User,
                &id_only(id.0),
            )?;
            transaction.commit()?;
            return Ok(None);
        };

        let bank_name = match update.bank_name {
            Some(name) => {
                let name = name.trim();
                if name.is_empty() {
                    current.bank_name.clone()
                } else {
                    truncate_name(name)
                }
            }
            None => current.bank_name.clone(),
        };
        let user_id = update
            .user_id
            .map(|value| value.trim().to_owned())
            .unwrap_or_else(|| current.user_id.clone());
        let server_url = match update.server_url {
            Some(value) => {
                let value = value.trim();
                if value.is_empty() { None } else { Some(value.to_owned()) }
            }
            None => current.server_url.clone(),
        };
        let blz = update
            .blz
            .map(|value| value.trim().to_owned())
            .unwrap_or_else(|| current.blz.clone());
        let bic = match update.bic {
            Some(value) => Some(value.trim().to_owned()),
            None => current.bic.clone(),
        };

        let login_changed = current.blz != blz || current.user_id != user_id || current.server_url != server_url;
        if login_changed {
            clear_login_state_conn(&transaction, id.0)?;
        }

        let tan_method = if login_changed {
            None
        } else {
            update.tan_method.or(current.tan_method)
        };
        let capabilities = if login_changed { None } else { current.capabilities };

        transaction.execute(
            "UPDATE bank_connections
             SET bank_name = ?1, blz = ?2, bic = ?3, user_id = ?4, server_url = ?5, tan_method = ?6,
                 capabilities = ?7
             WHERE id = ?8",
            params![bank_name, blz, bic, user_id, server_url, tan_method, capabilities, id.0,],
        )?;
        audit::log_conn(
            &transaction,
            "bank_connection.update_connection",
            Actor::User,
            &id_and_name(id.0, &bank_name),
        )?;
        let updated = read_item(&transaction, id.0)?;
        transaction.commit()?;

        Ok(updated)
    })
}

fn delete_conn(db: &Database, id: BankConnectionId, action: &str) -> Result<bool> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM bank_connections WHERE id = ?1", [id.0])?;
        audit::log_conn(&transaction, action, Actor::User, &id_only(id.0))?;
        transaction.commit()?;

        Ok(changed > 0)
    })
}

/// Deletes the connection. Its accounts and their transactions go through the foreign keys.
pub fn delete(db: &Database, id: BankConnectionId) -> Result<bool> {
    delete_conn(db, id, "bank_connection.delete")
}

/// Deletes the connection together with its accounts and their transactions.
pub fn delete_with_accounts(db: &Database, id: BankConnectionId) -> Result<bool> {
    delete_conn(db, id, "bank_connection.delete_with_accounts")
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::audit;
    use crate::model::AccountType;
    use crate::money::{Currency, Money};
    use crate::repo::accounts;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn new_connection() -> NewBankConnection {
        NewBankConnection {
            bank_name: "Deutsche Bank".to_owned(),
            blz: "37040044".to_owned(),
            bic: Some("DEUTDEDBXXX".to_owned()),
            user_id: "testuser123".to_owned(),
        }
    }

    fn audit_count(db: &Database) -> i64 {
        db.with_conn(|conn| {
            conn.query_row("SELECT count(*) FROM audit_log", [], |row| row.get(0))
                .map_err(crate::db::DbError::from)
        })
        .unwrap()
    }

    #[test]
    fn create_bank_access() {
        let (_temp, db) = database();
        let connection = create(&db, new_connection()).unwrap();

        assert_eq!(connection.bank_name, "Deutsche Bank");
        assert_eq!(connection.blz, "37040044");
        assert_eq!(connection.bic.as_deref(), Some("DEUTDEDBXXX"));
        assert_eq!(connection.user_id, "testuser123");
        assert!(!connection.offline);
        assert!(connection.include_in_sync_all);

        assert_eq!(get(&db, connection.id).unwrap(), Some(connection));
    }

    #[test]
    fn find_by_id_and_by_blz() {
        let (_temp, db) = database();
        let created = create(
            &db,
            NewBankConnection {
                bank_name: "Commerzbank".to_owned(),
                blz: "37040044".to_owned(),
                bic: Some("COBADEFFXXX".to_owned()),
                user_id: "user456".to_owned(),
            },
        )
        .unwrap();

        assert_eq!(
            get(&db, created.id).unwrap().map(|bank| bank.bank_name),
            Some("Commerzbank".to_owned())
        );

        create(
            &db,
            NewBankConnection {
                bank_name: "Sparkasse".to_owned(),
                blz: "37050198".to_owned(),
                bic: Some("COLSDE33XXX".to_owned()),
                user_id: "user789".to_owned(),
            },
        )
        .unwrap();
        let found = find_by_blz(&db, "37050198").unwrap();
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].bank_name, "Sparkasse");
    }

    #[test]
    fn update_bank_access_and_last_sync() {
        let (_temp, db) = database();
        let created = create(
            &db,
            NewBankConnection {
                bank_name: "Volksbank".to_owned(),
                blz: "37060190".to_owned(),
                bic: Some("GENODED1BRS".to_owned()),
                user_id: "user101".to_owned(),
            },
        )
        .unwrap();

        let updated = update(&db, created.id, None, Some("942".to_owned())).unwrap().unwrap();
        assert_eq!(updated.tan_method.as_deref(), Some("942"));

        let synced = update_last_sync(&db, created.id).unwrap().unwrap();
        assert!(synced.last_sync.is_some());
    }

    #[test]
    fn delete_bank_access() {
        let (_temp, db) = database();
        let created = create(&db, new_connection()).unwrap();

        assert!(delete(&db, created.id).unwrap());
        assert_eq!(get(&db, created.id).unwrap(), None);
        assert!(!delete(&db, created.id).unwrap(), "a second delete finds nothing");
    }

    #[test]
    fn find_all_lists_every_connection() {
        let (_temp, db) = database();
        create(&db, new_connection()).unwrap();
        create(
            &db,
            NewBankConnection {
                bank_name: "Bank2".to_owned(),
                blz: "22222222".to_owned(),
                bic: Some("BANK2DEFF".to_owned()),
                user_id: "u2".to_owned(),
            },
        )
        .unwrap();

        assert_eq!(list(&db).unwrap().len(), 2);
        assert_eq!(list_items(&db).unwrap().len(), 2);
    }

    #[test]
    fn update_capabilities_and_clear_a_blank_value() {
        let (_temp, db) = database();
        let created = create(&db, new_connection()).unwrap();
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().capabilities, None);

        let updated = update_capabilities(&db, created.id, "SaldoReq,UebSEPA")
            .unwrap()
            .unwrap();
        assert_eq!(updated.capabilities.as_deref(), Some("SaldoReq,UebSEPA"));
        assert_eq!(
            get_item(&db, created.id).unwrap().unwrap().capabilities.as_deref(),
            Some("SaldoReq,UebSEPA")
        );

        update_capabilities(&db, created.id, "  ").unwrap();
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().capabilities, None);
    }

    #[test]
    fn update_connection_stores_blz_and_bic() {
        let (_temp, db) = database();
        let created = create(
            &db,
            NewBankConnection {
                bank_name: "HVB".to_owned(),
                blz: "79020076".to_owned(),
                bic: Some("HYVEDEMM455".to_owned()),
                user_id: "u4".to_owned(),
            },
        )
        .unwrap();

        let updated = update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                blz: Some("70020270".to_owned()),
                bic: Some("HYVEDEMMXXX".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap()
        .unwrap();
        assert_eq!(updated.blz, "70020270");
        assert_eq!(updated.bic.as_deref(), Some("HYVEDEMMXXX"));

        // A blank BIC stores "" (an unknown BIC); a null leaves the stored one alone.
        let blank = update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                bic: Some("  ".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap()
        .unwrap();
        assert_eq!(blank.bic.as_deref(), Some(""));

        let renamed = update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                blz: Some("12030000".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap()
        .unwrap();
        assert_eq!(renamed.bic.as_deref(), Some(""));
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().blz, "12030000");
    }

    #[test]
    fn clear_login_state_drops_capabilities_and_tan_method() {
        let (_temp, db) = database();
        let created = create(&db, new_connection()).unwrap();
        update_capabilities(&db, created.id, "SaldoReq,UebSEPA").unwrap();
        update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                tan_method: Some("942".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap();
        assert_eq!(
            get_item(&db, created.id).unwrap().unwrap().capabilities.as_deref(),
            Some("SaldoReq,UebSEPA")
        );
        assert_eq!(
            get_item(&db, created.id).unwrap().unwrap().tan_method.as_deref(),
            Some("942")
        );

        let cleared = clear_login_state(&db, created.id).unwrap().unwrap();

        assert_eq!(cleared.capabilities, None);
        assert_eq!(cleared.tan_method, None);
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().capabilities, None);
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().tan_method, None);
    }

    #[test]
    fn changing_the_login_clears_the_login_state_but_renaming_does_not() {
        let (_temp, db) = database();
        let created = create(&db, new_connection()).unwrap();
        update_capabilities(&db, created.id, "SaldoReq").unwrap();
        update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                tan_method: Some("942".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap();

        let renamed = update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                bank_name: Some("Renamed".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap()
        .unwrap();
        assert_eq!(renamed.bank_name, "Renamed");
        assert_eq!(renamed.capabilities.as_deref(), Some("SaldoReq"));
        assert_eq!(renamed.tan_method.as_deref(), Some("942"));

        let changed = update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                user_id: Some("other-user".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap()
        .unwrap();
        assert_eq!(changed.capabilities, None);
        assert_eq!(changed.tan_method, None);
    }

    #[test]
    fn offline_banks_never_go_online_or_join_sync_all() {
        let (_temp, db) = database();
        let created = create_offline(&db, "Credit card").unwrap();

        assert_eq!(created.bank_name, "Credit card");
        assert_eq!(created.blz, "");
        assert_eq!(created.bic.as_deref(), Some(""));
        assert_eq!(created.user_id, "");
        assert!(created.offline);
        assert!(!created.include_in_sync_all);
        assert!(get_item(&db, created.id).unwrap().unwrap().offline);
    }

    #[test]
    fn delete_with_accounts_cascades_through_the_accounts() {
        let (_temp, db) = database();
        let bank = create(&db, new_connection()).unwrap();
        let account = accounts::create(
            &db,
            crate::model::NewAccount {
                bank_connection_id: Some(bank.id),
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
        .unwrap();

        assert!(delete_with_accounts(&db, bank.id).unwrap());
        assert_eq!(get(&db, bank.id).unwrap(), None);
        assert_eq!(accounts::get(&db, account).unwrap(), None);
    }

    #[test]
    fn set_sync_from_and_include_in_sync_all() {
        let (_temp, db) = database();
        let created = create(&db, new_connection()).unwrap();

        let dated = set_sync_from(&db, created.id, Some(date!(2026 - 01 - 01)))
            .unwrap()
            .unwrap();
        assert_eq!(dated.sync_from, Some(date!(2026 - 01 - 01)));

        let cleared = set_sync_from(&db, created.id, None).unwrap().unwrap();
        assert_eq!(cleared.sync_from, None);

        let excluded = set_include_in_sync_all(&db, created.id, false).unwrap().unwrap();
        assert!(!excluded.include_in_sync_all);
    }

    #[test]
    fn every_mutation_writes_one_audit_entry_without_the_user_id() {
        let (_temp, db) = database();
        let before = audit_count(&db);
        let mut expected = before;

        let created = create(
            &db,
            NewBankConnection {
                bank_name: "Audit Bank".to_owned(),
                blz: "37040044".to_owned(),
                bic: Some("DEUTDEDBXXX".to_owned()),
                user_id: "secret-user".to_owned(),
            },
        )
        .unwrap();
        expected += 1;

        update(&db, created.id, Some("Audit Bank 2".to_owned()), None).unwrap();
        expected += 1;
        update_last_sync(&db, created.id).unwrap();
        expected += 1;
        set_sync_from(&db, created.id, Some(date!(2026 - 01 - 01))).unwrap();
        expected += 1;
        set_include_in_sync_all(&db, created.id, false).unwrap();
        expected += 1;
        update_capabilities(&db, created.id, "SaldoReq").unwrap();
        expected += 1;
        update_connection(
            &db,
            created.id,
            BankConnectionUpdate {
                blz: Some("12030000".to_owned()),
                ..BankConnectionUpdate::default()
            },
        )
        .unwrap();
        expected += 1;
        clear_login_state(&db, created.id).unwrap();
        expected += 1;
        delete_with_accounts(&db, created.id).unwrap();
        expected += 1;

        assert_eq!(audit_count(&db), expected);
        let entries = audit::recent(&db, 1000).unwrap();
        assert!(
            entries.iter().all(|entry| !entry.details.contains("secret-user")),
            "an audit entry leaked the bank login"
        );
    }
}
