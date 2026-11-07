use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};

use rust_decimal::Decimal;
use time::UtcOffset;
use time::macros::date;

use crate::audit;
use crate::db::{Database, DbError};
use crate::i18n::Language;
use crate::import::dedup::{self, Candidate};
use crate::import::hash::transaction_hash;
use crate::model::AccountId;
use crate::money::Currency;
use crate::secret::{Passphrase, PassphrasePurpose};

use super::{FinDeskImportError, FinDeskImportReport, import_findesk};

const FIXTURE_PASSPHRASE: &str = "fixture passphrase 1";
const TARGET_PASSPHRASE: &str = "target passphrase 1";

struct ImportedFixture {
    // Declared before the target temporary folder so the connection closes before its file is removed.
    db: Database,
    _target_temp: tempfile::TempDir,
    documents_dir: PathBuf,
    _source_temp: tempfile::TempDir,
    _source_dir: PathBuf,
    report: FinDeskImportReport,
}

fn fixture_copy() -> (tempfile::TempDir, PathBuf) {
    let fixture = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/findesk");
    let temp = tempfile::tempdir().unwrap();
    let destination = temp.path().join("findesk");
    copy_tree(&fixture, &destination);
    (temp, destination)
}

fn copy_tree(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap();
    for entry in fs::read_dir(from).unwrap() {
        let entry = entry.unwrap();
        let target = to.join(entry.file_name());
        if entry.file_type().unwrap().is_dir() {
            copy_tree(&entry.path(), &target);
        } else {
            fs::copy(entry.path(), target).unwrap();
        }
    }
}

fn passphrase(value: &str, purpose: PassphrasePurpose) -> Passphrase {
    Passphrase::new(value, purpose).unwrap()
}

fn offset() -> UtcOffset {
    UtcOffset::from_hms(2, 0, 0).unwrap()
}

fn new_target() -> (tempfile::TempDir, Database, PathBuf) {
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("finglass.db");
    let pass = passphrase(TARGET_PASSPHRASE, PassphrasePurpose::New);
    let db = Database::create(&path, &pass).unwrap();
    let documents = temp.path().join("documents");
    (temp, db, documents)
}

fn import_into(
    db: &Database,
    documents_dir: &Path,
    source_dir: &Path,
) -> Result<FinDeskImportReport, FinDeskImportError> {
    import_findesk(
        db,
        documents_dir,
        source_dir,
        &passphrase(FIXTURE_PASSPHRASE, PassphrasePurpose::Unlock),
        offset(),
    )
}

fn imported_fixture() -> ImportedFixture {
    let (source_temp, source_dir) = fixture_copy();
    let (target_temp, db, documents_dir) = new_target();
    let report = import_into(&db, &documents_dir, &source_dir).unwrap();

    ImportedFixture {
        db,
        _target_temp: target_temp,
        documents_dir,
        _source_temp: source_temp,
        _source_dir: source_dir,
        report,
    }
}

fn scalar_i64(db: &Database, sql: &str) -> i64 {
    db.with_conn(|conn| conn.query_row(sql, [], |row| row.get(0)).map_err(DbError::from))
        .unwrap()
}

fn optional_text(db: &Database, sql: &str) -> Option<String> {
    db.with_conn(|conn| {
        conn.query_row(sql, [], |row| row.get::<_, Option<String>>(0))
            .map_err(DbError::from)
    })
    .unwrap()
}

fn table_exists(db: &Database, name: &str) -> bool {
    scalar_i64(
        db,
        &format!("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '{name}'"),
    ) > 0
}

#[derive(Debug)]
struct StoredTransaction {
    id: i64,
    date: String,
    amount_minor: i64,
    currency: String,
    partner: Option<String>,
    purpose: Option<String>,
    hash: String,
    balance_after_minor: Option<i64>,
    category_id: Option<i64>,
}

fn stored_transactions(db: &Database) -> Vec<StoredTransaction> {
    db.with_conn(|conn| {
        let mut statement = conn
            .prepare(
                "SELECT id, booking_date, amount_minor, currency, partner_name, purpose, import_hash,
                        balance_after_minor, category_id
                 FROM transactions ORDER BY id",
            )
            .map_err(DbError::from)?;
        statement
            .query_map([], |row| {
                Ok(StoredTransaction {
                    id: row.get(0)?,
                    date: row.get(1)?,
                    amount_minor: row.get(2)?,
                    currency: row.get(3)?,
                    partner: row.get(4)?,
                    purpose: row.get(5)?,
                    hash: row.get(6)?,
                    balance_after_minor: row.get(7)?,
                    category_id: row.get(8)?,
                })
            })
            .map_err(DbError::from)?
            .collect::<rusqlite::Result<Vec<_>>>()
            .map_err(DbError::from)
    })
    .unwrap()
}

fn decimal_from_minor(minor: i64, currency: &str) -> Decimal {
    Decimal::new(minor, Currency::new(currency).unwrap().minor_exponent())
}

fn snapshot_files(root: &Path) -> BTreeMap<PathBuf, Vec<u8>> {
    fn walk(root: &Path, current: &Path, files: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for entry in fs::read_dir(current).unwrap() {
            let entry = entry.unwrap();
            let path = entry.path();
            if entry.file_type().unwrap().is_dir() {
                walk(root, &path, files);
            } else {
                let relative = path.strip_prefix(root).unwrap().to_path_buf();
                files.insert(relative, fs::read(path).unwrap());
            }
        }
    }

    let mut files = BTreeMap::new();
    walk(root, root, &mut files);
    files
}

#[test]
fn the_fixture_imports_with_the_expected_counts() {
    let fixture = imported_fixture();
    let report = &fixture.report;

    assert_eq!(report.account_groups.imported, 1);
    assert_eq!(report.bank_connections.imported, 2);
    assert_eq!(report.accounts.imported, 3);
    assert_eq!(report.categories.imported, 3);
    assert_eq!(report.rules.imported, 2);
    assert_eq!(report.rules.skipped, 2);
    assert_eq!(report.rules.skipped_orphan, 1);
    assert_eq!(report.rules.skipped_invalid, 1);
    assert_eq!(report.transactions.imported, 8);
    assert_eq!(report.transactions.skipped, 2);
    assert_eq!(report.transactions.skipped_orphan, 1);
    assert_eq!(report.transactions.skipped_fractional_amount, 1);
    assert_eq!(report.deleted_bookings.imported, 1);
    assert_eq!(report.pending_bookings.imported, 1);
    assert_eq!(report.documents.copied, 1);
    assert_eq!(report.documents.missing, 2);
    assert_eq!(report.documents.refused, 0);
    assert_eq!(report.ocr_texts.imported, 1);
    assert_eq!(report.settings.imported, 8);
    assert_eq!(report.settings.skipped, 2);
    assert_eq!(report.payment_orders_left_behind, 1);

    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM account_groups"), 1);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM bank_connections"), 2);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM accounts"), 3);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM categories"), 3);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM rules"), 2);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM transactions"), 8);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM deleted_bookings"), 1);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM pending_bookings"), 1);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM documents"), 1);
    assert_eq!(scalar_i64(&fixture.db, "SELECT count(*) FROM ocr_texts"), 1);

    let summary = report.summary(Language::En);
    assert!(summary.starts_with("Imported 1 account groups"), "{summary}");
    assert_ne!(summary, report.summary(Language::De));
    assert_eq!(report.total_skipped(), 2 + 2 + 2 + 2);
}

#[test]
fn amounts_dates_booleans_and_tax_overrides_match_the_source() {
    let fixture = imported_fixture();
    let db = &fixture.db;

    assert_eq!(
        scalar_i64(db, "SELECT balance_minor FROM accounts WHERE id = 1"),
        -123_456
    );
    assert_eq!(
        scalar_i64(db, "SELECT balance_minor FROM accounts WHERE id = 2"),
        15_000
    );
    assert_eq!(
        scalar_i64(db, "SELECT balance_minor FROM accounts WHERE id = 3"),
        -20_000_000
    );
    assert_eq!(
        scalar_i64(db, "SELECT budget_limit_minor FROM categories WHERE id = 1"),
        25_050
    );

    let transactions = stored_transactions(db);
    let by_id = |id: i64| transactions.iter().find(|transaction| transaction.id == id).unwrap();

    assert_eq!(by_id(1).amount_minor, -10, "-0.10 EUR");
    assert_eq!(by_id(2).amount_minor, -123_456_789, "-1234567.89 EUR");
    assert_eq!(by_id(3).amount_minor, 10_000, "100.00 EUR");
    assert_eq!(by_id(4).amount_minor, -2_500, "-25.00 EUR");
    assert_eq!(by_id(5).amount_minor, -1_230, "-12.30 EUR");
    assert_eq!(by_id(6).amount_minor, -1_500, "-1500 JPY");
    assert_eq!(by_id(7).amount_minor, 50_000, "500.00 EUR");
    assert_eq!(by_id(5).balance_after_minor, Some(98_770), "987.70 EUR");
    assert_eq!(by_id(8).amount_minor, -300, "the dangling-category row is imported");
    assert_eq!(by_id(8).category_id, None, "the dangling category becomes NULL");

    assert!(transactions.iter().all(|transaction| transaction.date == "2026-10-01"));
    assert_eq!(
        optional_text(db, "SELECT created_at FROM transactions WHERE id = 1").as_deref(),
        Some("2026-10-18T09:30:15.123+02:00")
    );
    assert_eq!(
        optional_text(db, "SELECT balance_updated_at FROM accounts WHERE id = 1").as_deref(),
        Some("2026-10-18T09:30:15.123+02:00")
    );
    assert_eq!(
        optional_text(db, "SELECT last_sync FROM bank_connections WHERE id = 1").as_deref(),
        Some("2026-10-18T09:30:15.123+02:00")
    );
    assert_eq!(
        optional_text(db, "SELECT sync_from FROM bank_connections WHERE id = 1").as_deref(),
        Some("2025-01-01")
    );

    assert_eq!(scalar_i64(db, "SELECT is_new FROM transactions WHERE id = 1"), 1);
    assert_eq!(scalar_i64(db, "SELECT is_new FROM transactions WHERE id = 2"), 0);
    assert_eq!(
        scalar_i64(
            db,
            "SELECT count(*) FROM transactions WHERE id = 1 AND tax_override IS NULL"
        ),
        1
    );
    assert_eq!(scalar_i64(db, "SELECT tax_override FROM transactions WHERE id = 2"), 0);
    assert_eq!(scalar_i64(db, "SELECT tax_override FROM transactions WHERE id = 3"), 1);

    assert_eq!(
        scalar_i64(db, "SELECT include_in_sync_all FROM bank_connections WHERE id = 1"),
        0
    );
    assert_eq!(scalar_i64(db, "SELECT offline FROM bank_connections WHERE id = 1"), 0);
    assert_eq!(scalar_i64(db, "SELECT offline FROM bank_connections WHERE id = 2"), 1);
    assert_eq!(
        scalar_i64(
            db,
            "SELECT count(*) FROM bank_connections WHERE capabilities IS NOT NULL"
        ),
        0
    );
    assert_eq!(
        scalar_i64(db, "SELECT count(*) FROM accounts WHERE institution IS NOT NULL"),
        0
    );

    assert_eq!(scalar_i64(db, "SELECT is_tax_relevant FROM categories WHERE id = 2"), 1);
    assert_eq!(scalar_i64(db, "SELECT is_transfer FROM categories WHERE id = 3"), 1);
    assert_eq!(
        optional_text(db, "SELECT match_field FROM rules WHERE id = 1").as_deref(),
        Some("partner")
    );
    assert_eq!(
        optional_text(db, "SELECT direction FROM rules WHERE id = 1").as_deref(),
        Some("expense")
    );
    assert_eq!(scalar_i64(db, "SELECT auto_apply FROM rules WHERE id = 2"), 0);
    assert_eq!(scalar_i64(db, "SELECT tax_relevant FROM rules WHERE id = 2"), 1);
    assert_eq!(
        optional_text(db, "SELECT value FROM settings WHERE key = 'ui.theme'").as_deref(),
        Some("dark")
    );
    assert_eq!(
        optional_text(db, "SELECT value FROM settings WHERE key = 'accounts.collapsedBanks'").as_deref(),
        Some("2")
    );

    assert_eq!(
        scalar_i64(db, "SELECT amount_minor FROM pending_bookings WHERE id = 1"),
        -999
    );
    assert!(optional_text(db, "SELECT value_date FROM pending_bookings WHERE id = 1").is_none());
    assert_eq!(
        optional_text(db, "SELECT fetched_at FROM pending_bookings WHERE id = 1").as_deref(),
        Some("2026-10-18T09:30:15.123+02:00")
    );
    assert_eq!(
        optional_text(db, "SELECT deleted_at FROM deleted_bookings WHERE id = 1").as_deref(),
        Some("2026-10-18T09:30:15.123+02:00")
    );
}

#[test]
fn stored_hashes_are_copied_verbatim_and_dedup_finds_nothing_new() {
    let fixture = imported_fixture();
    let transactions = stored_transactions(&fixture.db);
    let day = date!(2026 - 10 - 01);

    for transaction in transactions
        .iter()
        .filter(|transaction| [1, 2, 3, 6, 7].contains(&transaction.id))
    {
        let amount = decimal_from_minor(transaction.amount_minor, &transaction.currency);
        assert_eq!(
            transaction_hash(
                day,
                amount,
                transaction.partner.as_deref(),
                transaction.purpose.as_deref()
            ),
            transaction.hash,
            "id {}",
            transaction.id
        );
    }

    let bank_id = transactions.iter().find(|transaction| transaction.id == 4).unwrap();
    let bank_id_purpose = format!("{}#DE02120300000000202051#B-77", bank_id.purpose.as_deref().unwrap());
    assert_eq!(
        transaction_hash(
            day,
            decimal_from_minor(bank_id.amount_minor, &bank_id.currency),
            bank_id.partner.as_deref(),
            Some(&bank_id_purpose)
        ),
        bank_id.hash
    );

    let balance = transactions.iter().find(|transaction| transaction.id == 5).unwrap();
    let balance_purpose = format!("{}#DE02120300000000202051#987.70", balance.purpose.as_deref().unwrap());
    assert_eq!(
        transaction_hash(
            day,
            decimal_from_minor(balance.amount_minor, &balance.currency),
            balance.partner.as_deref(),
            Some(&balance_purpose)
        ),
        balance.hash
    );

    let candidates: Vec<Candidate<i64>> = transactions
        .iter()
        .filter(|transaction| [1, 2, 3, 4, 5, 8].contains(&transaction.id))
        .map(|transaction| Candidate {
            item: transaction.id,
            date: day,
            amount: decimal_from_minor(transaction.amount_minor, &transaction.currency),
            hash: transaction.hash.clone(),
        })
        .collect();

    let plan = dedup::plan(&fixture.db, AccountId(1), Currency::EUR, candidates).unwrap();
    assert!(plan.to_import.is_empty(), "the imported bookings are all known");
    assert_eq!(plan.exact_duplicates, 6);
    assert_eq!(plan.probable_duplicates, 0);
    assert_eq!(plan.previously_deleted, 0);
}

#[test]
fn a_wrong_passphrase_leaves_the_target_unchanged() {
    let (source_temp, source_dir) = fixture_copy();
    let (_target_temp, target, documents_dir) = new_target();
    target
        .with_conn(|conn| {
            conn.execute(
                "INSERT INTO categories (id, name, color) VALUES (1, 'Seed', '#3BC7B0')",
                [],
            )
            .map_err(DbError::from)?;
            Ok(())
        })
        .unwrap();

    let error = import_findesk(
        &target,
        &documents_dir,
        &source_dir,
        &passphrase("wrong passphrase", PassphrasePurpose::Unlock),
        offset(),
    )
    .unwrap_err();

    assert!(matches!(error, FinDeskImportError::WrongPassphrase));
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM categories"), 1);
    assert_eq!(
        optional_text(&target, "SELECT name FROM categories WHERE id = 1").as_deref(),
        Some("Seed")
    );
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM accounts"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM bank_connections"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM transactions"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM documents"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM audit_log"), 0);
    assert!(!documents_dir.exists());
    assert!(source_temp.path().join("findesk/findesk.db").is_file());
}

#[test]
fn a_non_empty_target_is_refused_and_seeded_categories_are_replaced() {
    let (source_temp, source_dir) = fixture_copy();

    let (_target_temp, target, documents_dir) = new_target();
    target
        .with_conn(|conn| {
            conn.execute("INSERT INTO accounts (currency) VALUES ('EUR')", [])
                .map_err(DbError::from)?;
            Ok(())
        })
        .unwrap();

    let error = import_into(&target, &documents_dir, &source_dir).unwrap_err();
    assert!(matches!(error, FinDeskImportError::TargetNotEmpty));
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM accounts"), 1);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM categories"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM audit_log"), 0);
    assert!(!documents_dir.exists());

    let (_seeded_temp, seeded, seeded_documents) = new_target();
    seeded
        .with_conn(|conn| {
            conn.execute(
                "INSERT INTO categories (id, name, color) VALUES (1, 'Seeded default', '#3BC7B0')",
                [],
            )
            .map_err(DbError::from)?;
            Ok(())
        })
        .unwrap();

    let report = import_into(&seeded, &seeded_documents, &source_dir).unwrap();
    assert_eq!(report.categories.imported, 3);
    let names: Vec<String> = seeded
        .with_conn(|conn| {
            let mut statement = conn.prepare("SELECT name FROM categories ORDER BY id")?;
            statement
                .query_map([], |row| row.get::<_, String>(0))?
                .collect::<rusqlite::Result<_>>()
                .map_err(DbError::from)
        })
        .unwrap();
    assert_eq!(names, ["Food", "Donations", "Own transfers"]);
    assert!(!names.iter().any(|name| name == "Seeded default"));
    drop(source_temp);
}

#[test]
fn no_payment_data_reaches_the_target() {
    let fixture = imported_fixture();
    let db = &fixture.db;

    assert!(!table_exists(db, "payment_orders"));
    assert_eq!(fixture.report.payment_orders_left_behind, 1);

    let entries = db
        .with_conn(|conn| {
            let mut statement = conn.prepare("SELECT key, value FROM settings")?;
            let rows = statement
                .query_map([], |row| Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?)))?
                .collect::<rusqlite::Result<Vec<_>>>()?;
            Ok(rows)
        })
        .unwrap();

    assert!(!entries.iter().any(|(key, _)| key.starts_with("fints.pinSaved.")));
    for (key, value) in entries {
        for forbidden in ["Payee", "TRANSFER", "fixture-user"] {
            assert!(!key.contains(forbidden), "setting key leaked {forbidden}");
            assert!(!value.contains(forbidden), "setting value leaked {forbidden}");
        }
    }

    let audit_entries = audit::recent(db, 100).unwrap();
    assert_eq!(audit_entries.len(), 1);
    for entry in &audit_entries {
        for forbidden in ["Payee", "TRANSFER", "fixture-user"] {
            assert!(!entry.details.contains(forbidden), "audit leaked {forbidden}");
        }
    }
}

#[test]
fn only_the_receipt_document_is_copied_and_its_ocr_text_is_kept() {
    let fixture = imported_fixture();
    let db = &fixture.db;

    let document = db
        .with_conn(|conn| {
            conn.query_row(
                "SELECT id, file_name, stored_path, mime_type, uploaded_at, transaction_id
                 FROM documents WHERE id = 1",
                [],
                |row| {
                    Ok((
                        row.get::<_, i64>(0)?,
                        row.get::<_, String>(1)?,
                        row.get::<_, String>(2)?,
                        row.get::<_, String>(3)?,
                        row.get::<_, String>(4)?,
                        row.get::<_, Option<i64>>(5)?,
                    ))
                },
            )
            .map_err(DbError::from)
        })
        .unwrap();

    assert_eq!(document.0, 1);
    assert_eq!(document.1, "receipt.pdf");
    assert_eq!(document.3, "application/pdf");
    assert_eq!(document.4, "2026-10-18T09:30:15.123+02:00");
    assert_eq!(document.5, Some(1));

    let stored = Path::new(&document.2);
    assert_eq!(stored.parent(), Some(fixture.documents_dir.as_path()));
    assert!(stored.is_file());
    assert!(stored.file_name().unwrap().to_string_lossy().ends_with("_receipt.pdf"));
    assert_ne!(
        stored.file_name().unwrap().to_string_lossy(),
        "11111111-2222-3333-4444-555555555555_receipt.pdf"
    );

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = fs::metadata(stored).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    assert_eq!(
        optional_text(db, "SELECT content FROM ocr_texts WHERE document_id = 1").as_deref(),
        Some("REWE Summe 0,10")
    );
    assert_eq!(
        optional_text(db, "SELECT language FROM ocr_texts WHERE document_id = 1").as_deref(),
        Some("de")
    );
    let confidence: f64 = db
        .with_conn(|conn| {
            conn.query_row("SELECT confidence FROM ocr_texts WHERE document_id = 1", [], |row| {
                row.get(0)
            })
            .map_err(DbError::from)
        })
        .unwrap();
    assert!((confidence - 87.5).abs() < f64::EPSILON);

    assert_eq!(fs::read_dir(&fixture.documents_dir).unwrap().count(), 1);
    assert_eq!(fixture.report.documents.copied, 1);
    assert_eq!(fixture.report.documents.missing, 2);
    assert_eq!(fixture.report.ocr_texts.imported, 1);
}

#[test]
fn the_audit_log_gains_exactly_one_entry_without_personal_data() {
    let fixture = imported_fixture();
    let entries = audit::recent(&fixture.db, 100).unwrap();

    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].action, "FINDESK_IMPORTED");
    assert_eq!(entries[0].actor, crate::audit::Actor::Import);

    for forbidden in [
        "DE02120300000000202051",
        "fixture-user",
        "Fixture Bank",
        "Cash box",
        "REWE Markt",
        "Kassenbon",
        "REWE Summe",
        "receipt.pdf",
        "findesk",
    ] {
        assert!(
            !entries[0].details.contains(forbidden),
            "audit details leaked {forbidden}: {}",
            entries[0].details
        );
    }
}

#[test]
fn the_source_folder_is_byte_identical_after_the_import() {
    let (source_temp, source_dir) = fixture_copy();
    let before = snapshot_files(&source_dir);

    let (_target_temp, target, documents_dir) = new_target();
    import_into(&target, &documents_dir, &source_dir).unwrap();

    let after = snapshot_files(&source_dir);
    assert_eq!(before, after);
    assert!(source_dir.join("outside-secret.txt").is_file());
    assert!(source_temp.path().join("findesk/documents").is_dir());
}

#[test]
fn a_failed_transaction_removes_copied_files() {
    let (_source_temp, source_dir) = fixture_copy();
    let (_target_temp, target, documents_dir) = new_target();

    target
        .with_conn(|conn| {
            conn.execute_batch(
                "CREATE TRIGGER fail_audit BEFORE INSERT ON audit_log
                 BEGIN SELECT RAISE(ABORT, 'blocked'); END;",
            )
            .map_err(DbError::from)
        })
        .unwrap();

    let error = import_into(&target, &documents_dir, &source_dir).unwrap_err();
    assert!(matches!(error, FinDeskImportError::Db(_)));

    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM accounts"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM transactions"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM documents"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM ocr_texts"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM categories"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM audit_log"), 0);
    assert_eq!(fs::read_dir(&documents_dir).unwrap().count(), 0);
}

#[test]
fn the_error_messages_follow_the_language() {
    for error in [
        FinDeskImportError::NotFound,
        FinDeskImportError::WrongPassphrase,
        FinDeskImportError::NotFinDesk,
        FinDeskImportError::TargetNotEmpty,
    ] {
        let english = error.message(Language::En);
        let german = error.message(Language::De);
        let french = error.message(Language::Fr);
        assert!(!english.is_empty());
        assert_ne!(english, german);
        assert_ne!(english, french);
    }
}

#[test]
fn a_missing_source_is_not_found_and_a_foreign_database_is_not_findesk() {
    let (_target_temp, target, documents_dir) = new_target();

    let missing = tempfile::tempdir().unwrap();
    let error = import_into(&target, &documents_dir, missing.path()).unwrap_err();
    assert!(matches!(error, FinDeskImportError::NotFound));

    let foreign = tempfile::tempdir().unwrap();
    let path = foreign.path().join("findesk.db");
    let pass = passphrase(FIXTURE_PASSPHRASE, PassphrasePurpose::New);
    let _foreign_db = Database::create(&path, &pass).unwrap();

    let error = import_into(&target, &documents_dir, foreign.path()).unwrap_err();
    assert!(matches!(error, FinDeskImportError::NotFinDesk));
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM audit_log"), 0);
    assert_eq!(scalar_i64(&target, "SELECT count(*) FROM accounts"), 0);
}

#[test]
fn a_database_from_before_groups_pending_bookings_and_payments_still_imports() {
    let (_source_temp, source_dir) = fixture_copy();
    {
        let conn = rusqlite::Connection::open(source_dir.join("findesk.db")).unwrap();
        conn.pragma_update(None, "key", FIXTURE_PASSPHRASE).unwrap();
        conn.execute_batch(
            "UPDATE accounts SET group_id = NULL;
             DROP TABLE account_groups; DROP TABLE deleted_bookings;
             DROP TABLE pending_bookings; DROP TABLE payment_orders;",
        )
        .unwrap();
    }
    let (_target_temp, db, documents_dir) = new_target();

    let report = import_into(&db, &documents_dir, &source_dir).unwrap();

    assert_eq!(report.account_groups.imported, 0);
    assert_eq!(report.deleted_bookings.imported, 0);
    assert_eq!(report.pending_bookings.imported, 0);
    assert_eq!(report.payment_orders_left_behind, 0);
    assert_eq!(report.accounts.imported, 3);
    assert!(report.transactions.imported > 0);
}

#[test]
fn the_source_snapshot_comparison_detects_a_change() {
    use std::time::{Duration, UNIX_EPOCH};

    use super::source::{FileStamp, SourceSnapshot, source_changed};

    let stamp = |size, seconds| FileStamp {
        size,
        modified: Some(UNIX_EPOCH + Duration::from_secs(seconds)),
    };
    let before = SourceSnapshot {
        database: Some(stamp(100, 10)),
        wal: Some(stamp(20, 11)),
    };

    assert!(!source_changed(&before, &before.clone()));

    let grown = SourceSnapshot {
        database: Some(stamp(101, 10)),
        ..before.clone()
    };
    assert!(source_changed(&before, &grown));

    let touched = SourceSnapshot {
        wal: Some(stamp(20, 12)),
        ..before.clone()
    };
    assert!(source_changed(&before, &touched));

    let disappeared = SourceSnapshot {
        wal: None,
        ..before.clone()
    };
    assert!(source_changed(&before, &disappeared));
}

#[test]
fn a_budget_with_too_many_decimals_keeps_its_category() {
    let (_source_temp, source_dir) = fixture_copy();
    {
        let conn = rusqlite::Connection::open(source_dir.join("findesk.db")).unwrap();
        conn.pragma_update(None, "key", FIXTURE_PASSPHRASE).unwrap();
        conn.execute("UPDATE categories SET budget_limit = 250.505 WHERE id = 1", [])
            .unwrap();
    }
    let (_target_temp, db, documents_dir) = new_target();

    let report = import_into(&db, &documents_dir, &source_dir).unwrap();

    assert_eq!(report.categories.imported, 3);
    assert_eq!(
        optional_text(
            &db,
            "SELECT CAST(budget_limit_minor AS TEXT) FROM categories WHERE id = 1"
        ),
        None
    );
    assert_eq!(scalar_i64(&db, "SELECT count(*) FROM rules WHERE category_id = 1"), 1);
}
