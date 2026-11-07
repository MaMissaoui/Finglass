//! Pending booking repository.
//!
//! The bank's pending lines are fetched apart from the real bookings. Nothing of the booking pipeline reads them.

use rusqlite::{Row, params};
use time::OffsetDateTime;

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{AccountId, NewPendingBooking, PendingBookingItem};
use crate::money::Money;

use super::{Result, format_date, format_timestamp, in_conn, parse_currency, parse_date, parse_timestamp};

const COLUMNS: &str = "id, account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose, \
     fetched_at";

fn from_row(row: &Row<'_>) -> Result<PendingBookingItem> {
    let currency = parse_currency(&row.get::<_, String>(5)?)?;

    Ok(PendingBookingItem {
        account_id: AccountId(row.get(1)?),
        date: parse_date(&row.get::<_, String>(2)?)?,
        value_date: row
            .get::<_, Option<String>>(3)?
            .map(|text| parse_date(&text))
            .transpose()?,
        amount: Money {
            minor: row.get(4)?,
            currency,
        },
        partner_name: row.get(6)?,
        purpose: row.get(7)?,
        fetched_at: parse_timestamp(&row.get::<_, String>(8)?)?,
    })
}

/// Replaces the account's stored pending rows with `items`, stamped with `fetched_at`, in one transaction.
///
/// Called with an empty list when the bank reported none pending; a failed snapshot never calls this, so what is
/// stored stays.
pub fn replace_for_account(
    db: &Database,
    account_id: AccountId,
    items: Vec<NewPendingBooking>,
    fetched_at: OffsetDateTime,
) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute("DELETE FROM pending_bookings WHERE account_id = ?1", [account_id.0])?;

        let fetched_at = format_timestamp(fetched_at)?;
        for item in &items {
            let date = format_date(item.date)?;
            let value_date = item.value_date.map(format_date).transpose()?;
            transaction.execute(
                "INSERT INTO pending_bookings (
                    account_id, booking_date, value_date, amount_minor, currency, partner_name, purpose, fetched_at
                 ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    account_id.0,
                    date,
                    value_date,
                    item.amount.minor,
                    item.amount.currency.as_str(),
                    item.partner_name,
                    item.purpose,
                    fetched_at,
                ],
            )?;
        }

        audit::log_conn(
            &transaction,
            "pending_booking.replace",
            Actor::User,
            &format!("id={} count={}", account_id.0, items.len()),
        )?;
        transaction.commit()?;

        Ok(())
    })
}

/// The account's stored pending lines, newest first.
pub fn find_items(db: &Database, account_id: AccountId) -> Result<Vec<PendingBookingItem>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!(
            "SELECT {COLUMNS} FROM pending_bookings
             WHERE account_id = ?1
             ORDER BY booking_date DESC, id DESC"
        ))?;
        let mut rows = statement.query([account_id.0])?;
        let mut items = Vec::new();

        while let Some(row) = rows.next()? {
            items.push(from_row(row)?);
        }

        Ok(items)
    })
}

/// Removes the account's stored pending lines.
pub fn delete_for_account(db: &Database, account_id: AccountId) -> Result<()> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute("DELETE FROM pending_bookings WHERE account_id = ?1", [account_id.0])?;
        audit::log_conn(
            &transaction,
            "pending_booking.delete",
            Actor::User,
            &format!("id={}", account_id.0),
        )?;
        transaction.commit()?;

        Ok(())
    })
}

#[cfg(test)]
mod tests {
    use time::macros::{date, datetime};

    use super::*;
    use crate::model::{AccountType, NewAccount, NewBankConnection};
    use crate::money::Currency;
    use crate::repo::{accounts, bank_connections};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database, bank_connection_id: Option<crate::model::BankConnectionId>) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id,
                institution: None,
                name: Some("Account".to_owned()),
                iban: None,
                account_number: None,
                currency: Currency::EUR,
                account_type: AccountType::Checking,
                balance: Money {
                    minor: 50_000,
                    currency: Currency::EUR,
                },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn bank(db: &Database) -> crate::model::BankConnection {
        bank_connections::create(
            db,
            NewBankConnection {
                bank_name: "Beispiel Bank".to_owned(),
                blz: "37050198".to_owned(),
                bic: Some("COLSDE33XXX".to_owned()),
                user_id: "demo-user".to_owned(),
            },
        )
        .unwrap()
    }

    fn item(minor: i64, partner: Option<&str>) -> NewPendingBooking {
        NewPendingBooking {
            date: date!(2026 - 10 - 03),
            value_date: Some(date!(2026 - 10 - 02)),
            amount: Money {
                minor,
                currency: Currency::EUR,
            },
            partner_name: partner.map(str::to_owned),
            purpose: Some("Musterzahlung".to_owned()),
        }
    }

    fn fetched_at() -> OffsetDateTime {
        datetime!(2026-10-04 09:30 UTC)
    }

    #[test]
    fn replace_for_account_deletes_and_inserts_in_one_go_and_stamps_the_fetch_time() {
        let (_temp, db) = database();
        let account_id = account(&db, None);
        replace_for_account(&db, account_id, vec![item(-1000, Some("Beispiel GmbH"))], fetched_at()).unwrap();

        let first = find_items(&db, account_id).unwrap();
        assert_eq!(first.len(), 1);
        assert_eq!(first[0].amount.minor, -1000);
        assert_eq!(first[0].purpose.as_deref(), Some("Musterzahlung"));
        assert_eq!(first[0].value_date, Some(date!(2026 - 10 - 02)));
        assert_eq!(first[0].fetched_at, fetched_at(), "replaced rows carry the fetch stamp");

        // A second replace deletes the old row instead of adding a second one.
        replace_for_account(
            &db,
            account_id,
            vec![item(-2000, Some("Andere GmbH")), item(-3000, None)],
            fetched_at() + time::Duration::minutes(5),
        )
        .unwrap();
        let second = find_items(&db, account_id).unwrap();
        assert_eq!(second.len(), 2);
        assert!(
            second
                .iter()
                .all(|item| item.partner_name.as_deref() != Some("Beispiel GmbH"))
        );
        assert_eq!(second[0].fetched_at, fetched_at() + time::Duration::minutes(5));
    }

    #[test]
    fn find_items_lists_the_newest_first() {
        let (_temp, db) = database();
        let account_id = account(&db, None);

        let mut old = item(-1000, Some("Alt GmbH"));
        old.date = date!(2026 - 09 - 01);
        replace_for_account(&db, account_id, vec![old, item(-2000, Some("Neu GmbH"))], fetched_at()).unwrap();

        assert_eq!(
            find_items(&db, account_id)
                .unwrap()
                .into_iter()
                .map(|item| item.partner_name)
                .collect::<Vec<_>>(),
            vec![Some("Neu GmbH".to_owned()), Some("Alt GmbH".to_owned())]
        );
    }

    #[test]
    fn delete_for_account_and_the_account_cascade_remove_the_rows() {
        let (_temp, db) = database();
        let account_id = account(&db, None);
        replace_for_account(&db, account_id, vec![item(-1000, Some("Beispiel GmbH"))], fetched_at()).unwrap();
        assert!(!find_items(&db, account_id).unwrap().is_empty());

        delete_for_account(&db, account_id).unwrap();
        assert!(
            find_items(&db, account_id).unwrap().is_empty(),
            "delete_for_account clears them"
        );

        // An account delete cascades: pending rows are gone with the account.
        replace_for_account(&db, account_id, vec![item(-1000, Some("Beispiel GmbH"))], fetched_at()).unwrap();
        accounts::delete(&db, account_id).unwrap();
        assert!(
            find_items(&db, account_id).unwrap().is_empty(),
            "the account cascade removes the pending rows"
        );
    }

    #[test]
    fn the_bank_access_cascade_removes_its_accounts_pending_rows() {
        let (_temp, db) = database();
        let bank = bank(&db);
        let account_id = account(&db, Some(bank.id));
        replace_for_account(&db, account_id, vec![item(-1000, Some("Beispiel GmbH"))], fetched_at()).unwrap();

        assert!(bank_connections::delete_with_accounts(&db, bank.id).unwrap());
        assert!(
            find_items(&db, account_id).unwrap().is_empty(),
            "the bank access cascade removes the pending rows too"
        );
    }
}
