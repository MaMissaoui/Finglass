//! Duplicate detection shared by FinTS sync and file import, ported from FinDesk's `BookingDedup`.
//!
//! Exact: a booking whose hash is already stored. Probable: the same booking from a different source
//! (FinTS vs CSV vs CAMT vs manual), whose partner/purpose text and therefore hash differ. For those,
//! bookings with the same account, booking date and amount are compared by count: if 2 such bookings
//! exist and 3 arrive, 1 is new. Genuine repeats (two coffees) are kept, and a different amount on the
//! same day is always new.
//!
//! A booking the user deleted is remembered by hash ([`crate::repo::transactions::deleted_hashes`]):
//! such a candidate has no stored row but is counted in [`Plan::previously_deleted`] and skipped,
//! unless a row with the same hash is stored again — a stored row always wins over a remembered
//! deletion.

use std::collections::HashMap;

use rust_decimal::Decimal;
use time::Date;

use crate::db::Database;
use crate::model::AccountId;
use crate::money::Currency;
use crate::repo::{self, RepoError, Result};

use super::amount_to_money;

/// One booking to plan, with the fields duplicate detection compares.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Candidate<T> {
    /// The booking itself.
    pub item: T,
    /// Its booking date.
    pub date: Date,
    /// Its signed amount.
    pub amount: Decimal,
    /// Its dedup hash (see [`super::hash`]).
    pub hash: String,
}

/// What [`plan`] decided.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Plan<T> {
    /// The candidates to insert, in their original order.
    pub to_import: Vec<Candidate<T>>,
    /// Candidates whose hash is already stored.
    pub exact_duplicates: usize,
    /// Candidates skipped as probable duplicates of an existing same-day, same-amount booking.
    pub probable_duplicates: usize,
    /// Candidates skipped because the user deleted a booking with that hash earlier.
    pub previously_deleted: usize,
}

type DayAmount = (Date, Decimal);
type IndexedCandidate<T> = (usize, Candidate<T>);
type CandidateGroups<T> = HashMap<DayAmount, Vec<IndexedCandidate<T>>>;

fn key(candidate: &Candidate<impl Sized>) -> DayAmount {
    (candidate.date, candidate.amount.normalize())
}

/// Plans which candidates to import, preserving their order.
pub fn plan<T>(
    db: &Database,
    account_id: AccountId,
    currency: Currency,
    candidates: Vec<Candidate<T>>,
) -> Result<Plan<T>> {
    // Indexed so identical bookings (equal candidates) keep their own identity and the file order.
    let mut known = Vec::new();
    let mut rest = Vec::new();
    for (index, candidate) in candidates.into_iter().enumerate() {
        if repo::transactions::find_by_hash(db, account_id, &candidate.hash)? {
            known.push((index, candidate));
        } else {
            rest.push((index, candidate));
        }
    }

    // Remembered deletions are read once per plan; they are skipped, not counted as known or probable.
    let deleted = repo::transactions::deleted_hashes(db, account_id)?;
    let mut previously_deleted = Vec::new();
    let mut unknown = Vec::new();
    for item in rest {
        if deleted.contains(&item.1.hash) {
            previously_deleted.push(item);
        } else {
            unknown.push(item);
        }
    }

    let mut known_per_day: HashMap<DayAmount, usize> = HashMap::new();
    for (_, candidate) in &known {
        *known_per_day.entry(key(candidate)).or_insert(0) += 1;
    }

    let mut groups: CandidateGroups<T> = HashMap::new();
    for item in unknown {
        let group_key = key(&item.1);
        groups.entry(group_key).or_default().push(item);
    }

    let mut to_import = Vec::new();
    let mut probable_duplicates = 0;
    for ((date, amount), group) in groups {
        // Existing bookings that this batch's exact matches don't already account for.
        let amount_minor = amount_to_money(group[0].1.amount, currency)
            .map_err(RepoError::Money)?
            .minor;
        let existing = repo::transactions::count_same_booking(db, account_id, date, amount_minor)?;
        let already_known = known_per_day.get(&(date, amount)).copied().unwrap_or(0) as i64;
        let unmatched_existing = (existing - already_known).max(0) as usize;
        let skip = unmatched_existing.min(group.len());
        probable_duplicates += skip;
        to_import.extend(group.into_iter().skip(skip));
    }

    to_import.sort_by_key(|(index, _)| *index);
    let to_import = to_import.into_iter().map(|(_, candidate)| candidate).collect();

    Ok(Plan {
        to_import,
        exact_duplicates: known.len(),
        probable_duplicates,
        previously_deleted: previously_deleted.len(),
    })
}

#[cfg(test)]
mod tests {
    use time::OffsetDateTime;
    use time::macros::date;

    use super::*;
    use crate::model::{AccountType, NewAccount, NewTransaction};
    use crate::money::Money;
    use crate::repo::{accounts, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: Some("DE89370400440532013000".to_owned()),
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
        .unwrap()
    }

    fn stored_row(day: Date, amount: &str, hash: &str) -> NewTransaction {
        let minor = crate::import::amount_to_money(amount.parse().unwrap(), Currency::EUR)
            .unwrap()
            .minor;
        NewTransaction {
            booking_date: day,
            amount: Money {
                minor,
                currency: Currency::EUR,
            },
            balance_after: None,
            partner_name: Some("Stored".to_owned()),
            purpose: None,
            category_id: None,
            import_hash: hash.to_owned(),
            created_at: OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }
    }

    fn stored(db: &Database, account_id: AccountId, day: Date, amount: &str, hash: &str) {
        transactions::insert_imported(db, account_id, vec![stored_row(day, amount, hash)]).unwrap();
    }

    fn stored_direct(db: &Database, account_id: AccountId, day: Date, amount: &str, hash: &str) {
        transactions::insert_planned(db, account_id, vec![stored_row(day, amount, hash)]).unwrap();
    }

    fn candidate(item: &str, day: Date, amount: &str, hash: &str) -> Candidate<String> {
        Candidate {
            item: item.to_owned(),
            date: day,
            amount: amount.parse().unwrap(),
            hash: hash.to_owned(),
        }
    }

    #[test]
    fn a_stored_hash_is_an_exact_duplicate() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let day = date!(2026 - 10 - 03);
        stored(&db, account_id, day, "-5.00", "stored");

        let plan = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![candidate("stored", day, "-5.00", "stored")],
        )
        .unwrap();

        assert_eq!(plan.exact_duplicates, 1);
        assert_eq!(plan.probable_duplicates, 0);
        assert!(plan.to_import.is_empty());
    }

    #[test]
    fn probable_duplicates_are_counted_per_day_and_amount() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let day = date!(2026 - 10 - 03);
        stored(&db, account_id, day, "-4.50", "bank-1");
        stored(&db, account_id, day, "-4.50", "bank-2");

        let plan = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![
                candidate("new-1", day, "-4.50", "file-1"),
                candidate("new-2", day, "-4.50", "file-2"),
                candidate("new-3", day, "-4.50", "file-3"),
                candidate("different", day, "-4.70", "file-4"),
            ],
        )
        .unwrap();

        assert_eq!(plan.probable_duplicates, 2);
        assert_eq!(
            plan.to_import
                .iter()
                .map(|candidate| candidate.item.as_str())
                .collect::<Vec<_>>(),
            vec!["new-3", "different"],
            "one extra coffee stays, a different amount is always new"
        );
    }

    #[test]
    fn the_original_file_order_is_kept() {
        let (_temp, db) = database();
        let account_id = account(&db);

        let plan = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![
                candidate("late", date!(2026 - 10 - 05), "-3.00", "a"),
                candidate("early", date!(2026 - 10 - 01), "-2.00", "b"),
                candidate("middle", date!(2026 - 10 - 03), "-1.00", "c"),
            ],
        )
        .unwrap();

        assert_eq!(
            plan.to_import
                .iter()
                .map(|candidate| candidate.item.as_str())
                .collect::<Vec<_>>(),
            vec!["late", "early", "middle"]
        );
    }

    #[test]
    fn a_deleted_candidate_is_skipped_and_counted_but_a_stored_row_wins() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let day = date!(2026 - 10 - 05);

        stored(&db, account_id, day, "-1.00", "deleted-hash");
        let booking = transactions::list_for_account(&db, account_id, None, None).unwrap()[0].clone();
        transactions::delete(&db, booking.id).unwrap();

        let planned_deleted = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![candidate("deleted", day, "-1.00", "deleted-hash")],
        )
        .unwrap();
        assert_eq!(planned_deleted.previously_deleted, 1);
        assert!(planned_deleted.to_import.is_empty());

        // A stored row with the same hash is an exact duplicate, regardless of the remembered deletion.
        stored_direct(&db, account_id, day, "-1.00", "deleted-hash");
        let planned = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![candidate("again", day, "-1.00", "deleted-hash")],
        )
        .unwrap();
        assert_eq!(planned.exact_duplicates, 1);
        assert_eq!(planned.previously_deleted, 0);
        assert!(planned.to_import.is_empty());
    }

    #[test]
    fn a_deleted_candidate_does_not_account_for_a_same_day_stored_booking() {
        let (_temp, db) = database();
        let account_id = account(&db);
        let day = date!(2026 - 10 - 05);
        stored(&db, account_id, day, "-5.00", "stored");
        stored(&db, account_id, day, "-1.00", "deleted-hash");
        let deleted = transactions::list_for_account(&db, account_id, None, None)
            .unwrap()
            .into_iter()
            .find(|booking| booking.import_hash == "deleted-hash")
            .unwrap();
        transactions::delete(&db, deleted.id).unwrap();

        let plan = plan(
            &db,
            account_id,
            Currency::EUR,
            vec![
                candidate("rememberedDeleted", day, "-5.00", "deleted-hash"),
                candidate("unknown", day, "-5.00", "unknown"),
            ],
        )
        .unwrap();

        assert_eq!(plan.exact_duplicates, 0);
        assert_eq!(plan.previously_deleted, 1);
        assert_eq!(plan.probable_duplicates, 1);
        assert!(
            plan.to_import.is_empty(),
            "the stored row accounts for one same-day booking"
        );
    }
}
