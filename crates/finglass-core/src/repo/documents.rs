//! Document repository.
//!
//! Documents and their OCR texts live in separate tables. Deleting a document row removes its OCR texts through the
//! foreign key, but **never its file on disk**: removing the file is the document service's job.

use rusqlite::types::Value;
use rusqlite::{Connection, OptionalExtension, Row, params, params_from_iter};
use time::OffsetDateTime;

use crate::audit::{self, Actor};
use crate::db::Database;
use crate::model::{Document, DocumentItem, NewDocument, TransactionId};

use super::{RepoError, Result, format_timestamp, id_and_name, id_only, in_conn, parse_timestamp};

const COLUMNS: &str = "id, file_name, stored_path, mime_type, uploaded_at, transaction_id";

const ITEM_SELECT: &str = "SELECT d.id, d.file_name, d.stored_path, d.mime_type, d.uploaded_at, d.transaction_id,
            t.partner_name, t.purpose, t.booking_date,
            EXISTS (SELECT 1 FROM ocr_texts o WHERE o.document_id = d.id),
            (SELECT o.confidence FROM ocr_texts o WHERE o.document_id = d.id ORDER BY o.id LIMIT 1)
     FROM documents d
     LEFT JOIN transactions t ON t.id = d.transaction_id";

fn from_row(row: &Row<'_>) -> Result<Document> {
    Ok(Document {
        id: row.get(0)?,
        file_name: row.get(1)?,
        stored_path: row.get(2)?,
        mime_type: row.get(3)?,
        uploaded_at: parse_timestamp(&row.get::<_, String>(4)?)?,
        transaction_id: row.get::<_, Option<i64>>(5)?.map(TransactionId),
    })
}

fn read_document(conn: &Connection, id: i64) -> Result<Option<Document>> {
    let sql = format!("SELECT {COLUMNS} FROM documents WHERE id = ?1");
    let mut statement = conn.prepare(&sql)?;
    let mut rows = statement.query([id])?;

    match rows.next()? {
        Some(row) => Ok(Some(from_row(row)?)),
        None => Ok(None),
    }
}

/// Escapes SQL `LIKE` wildcards so user text is matched literally.
fn escape_like(text: &str) -> String {
    let mut escaped = String::with_capacity(text.len());
    for character in text.chars() {
        match character {
            '\\' | '%' | '_' => {
                escaped.push('\\');
                escaped.push(character);
            }
            _ => escaped.push(character),
        }
    }
    escaped
}

fn item_from_row(row: &Row<'_>) -> Result<DocumentItem> {
    let transaction_id = row.get::<_, Option<i64>>(5)?.map(TransactionId);
    let partner_name: Option<String> = row.get(6)?;
    let purpose: Option<String> = row.get(7)?;
    let booking_date: Option<String> = row.get(8)?;
    let transaction_label = if transaction_id.is_some() {
        booking_date.map(|date| {
            let title = partner_name.or(purpose).unwrap_or_else(|| "Transaction".to_owned());
            format!("{title} ({date})")
        })
    } else {
        None
    };

    Ok(DocumentItem {
        id: row.get(0)?,
        file_name: row.get(1)?,
        stored_path: row.get(2)?,
        mime_type: row.get(3)?,
        uploaded_at: parse_timestamp(&row.get::<_, String>(4)?)?,
        transaction_id,
        transaction_label,
        has_ocr: row.get::<_, i64>(9)? != 0,
        ocr_confidence: row.get(10)?,
    })
}

fn query_items(
    conn: &Connection,
    query: Option<&str>,
    unlinked_only: bool,
    transaction_id: Option<TransactionId>,
    only_id: Option<i64>,
) -> Result<Vec<DocumentItem>> {
    let mut sql = String::from(ITEM_SELECT);
    sql.push_str(" WHERE 1 = 1");
    let mut values: Vec<Value> = Vec::new();

    if let Some(text) = query.map(str::trim).filter(|text| !text.is_empty()) {
        let pattern = format!("%{}%", escape_like(text));
        sql.push_str(
            " AND (d.file_name LIKE ? ESCAPE '\\'
                   OR EXISTS (SELECT 1 FROM ocr_texts o WHERE o.document_id = d.id
                              AND o.content LIKE ? ESCAPE '\\'))",
        );
        values.push(Value::Text(pattern.clone()));
        values.push(Value::Text(pattern));
    }
    if unlinked_only {
        sql.push_str(" AND d.transaction_id IS NULL");
    }
    if let Some(transaction_id) = transaction_id {
        sql.push_str(" AND d.transaction_id = ?");
        values.push(Value::Integer(transaction_id.0));
    }
    if let Some(id) = only_id {
        sql.push_str(" AND d.id = ?");
        values.push(Value::Integer(id));
    }

    sql.push_str(" ORDER BY d.uploaded_at DESC, d.id DESC");

    let mut statement = conn.prepare(&sql)?;
    let mut rows = statement.query(params_from_iter(values.iter()))?;
    let mut items = Vec::new();

    while let Some(row) = rows.next()? {
        items.push(item_from_row(row)?);
    }

    Ok(items)
}

/// Lists every document.
pub fn list(db: &Database) -> Result<Vec<Document>> {
    in_conn(db, |conn| {
        let mut statement = conn.prepare(&format!("SELECT {COLUMNS} FROM documents ORDER BY id"))?;
        let mut rows = statement.query([])?;
        let mut documents = Vec::new();

        while let Some(row) = rows.next()? {
            documents.push(from_row(row)?);
        }

        Ok(documents)
    })
}

/// Reads one document.
pub fn get(db: &Database, id: i64) -> Result<Option<Document>> {
    in_conn(db, |conn| read_document(conn, id))
}

/// Creates a document row and stamps it with the current time.
///
/// The file itself is copied by the document service; this only stores the row.
pub fn create(db: &Database, new: NewDocument) -> Result<Document> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let uploaded_at = OffsetDateTime::now_utc();
        let uploaded_at_text = format_timestamp(uploaded_at)?;

        transaction.execute(
            "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                new.file_name,
                new.stored_path,
                new.mime_type,
                uploaded_at_text,
                new.transaction_id.map(|id| id.0),
            ],
        )?;

        let id = transaction.last_insert_rowid();
        audit::log_conn(
            &transaction,
            "document.create",
            Actor::User,
            &id_and_name(id, &new.file_name),
        )?;
        transaction.commit()?;

        Ok(Document {
            id,
            file_name: new.file_name,
            stored_path: new.stored_path,
            mime_type: new.mime_type,
            uploaded_at,
            transaction_id: new.transaction_id,
        })
    })
}

/// Links a document to a transaction.
pub fn link_to_transaction(db: &Database, id: i64, transaction_id: TransactionId) -> Result<Option<Document>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute(
            "UPDATE documents SET transaction_id = ?1 WHERE id = ?2",
            params![transaction_id.0, id],
        )?;
        audit::log_conn(&transaction, "document.link_to_transaction", Actor::User, &id_only(id))?;
        let document = read_document(&transaction, id)?;
        transaction.commit()?;

        Ok(document)
    })
}

/// Removes a document's transaction link.
pub fn unlink_from_transaction(db: &Database, id: i64) -> Result<Option<Document>> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        transaction.execute("UPDATE documents SET transaction_id = NULL WHERE id = ?1", [id])?;
        audit::log_conn(
            &transaction,
            "document.unlink_from_transaction",
            Actor::User,
            &id_only(id),
        )?;
        let document = read_document(&transaction, id)?;
        transaction.commit()?;

        Ok(document)
    })
}

/// Deletes a document row and its OCR texts.
///
/// The file on disk is left alone; deleting it is the document service's job.
pub fn delete(db: &Database, id: i64) -> Result<bool> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let changed = transaction.execute("DELETE FROM documents WHERE id = ?1", [id])?;
        audit::log_conn(&transaction, "document.delete", Actor::User, &id_only(id))?;
        transaction.commit()?;

        Ok(changed > 0)
    })
}

/// Newest-first snapshots. `query` matches the file name or the OCR text.
pub fn find_items(
    db: &Database,
    query: Option<&str>,
    unlinked_only: bool,
    transaction_id: Option<TransactionId>,
) -> Result<Vec<DocumentItem>> {
    in_conn(db, |conn| query_items(conn, query, unlinked_only, transaction_id, None))
}

/// Stores (or replaces) the OCR text for a document and returns the new OCR row id.
pub fn save_ocr_text(
    db: &Database,
    document_id: i64,
    content: &str,
    language: &str,
    confidence: Option<f64>,
) -> Result<i64> {
    in_conn(db, |conn| {
        let transaction = conn.unchecked_transaction()?;
        let exists: Option<i64> = transaction
            .query_row("SELECT 1 FROM documents WHERE id = ?1", [document_id], |row| row.get(0))
            .optional()?;
        if exists.is_none() {
            return Err(RepoError::NotFound);
        }

        transaction.execute("DELETE FROM ocr_texts WHERE document_id = ?1", [document_id])?;
        transaction.execute(
            "INSERT INTO ocr_texts (document_id, content, language, confidence) VALUES (?1, ?2, ?3, ?4)",
            params![document_id, content, language, confidence],
        )?;
        let id = transaction.last_insert_rowid();
        audit::log_conn(
            &transaction,
            "document.save_ocr_text",
            Actor::User,
            &id_only(document_id),
        )?;
        transaction.commit()?;

        Ok(id)
    })
}

/// The document's OCR text, when one is stored.
pub fn get_ocr_text(db: &Database, document_id: i64) -> Result<Option<String>> {
    in_conn(db, |conn| {
        Ok(conn
            .query_row(
                "SELECT content FROM ocr_texts WHERE document_id = ?1 ORDER BY id LIMIT 1",
                [document_id],
                |row| row.get(0),
            )
            .optional()?)
    })
}

/// Reads one document snapshot.
pub fn get_item(db: &Database, id: i64) -> Result<Option<DocumentItem>> {
    in_conn(db, |conn| {
        Ok(query_items(conn, None, false, None, Some(id))?.into_iter().next())
    })
}

#[cfg(test)]
mod tests {
    use time::macros::date;

    use super::*;
    use crate::model::{AccountType, NewAccount, NewTransaction};
    use crate::money::{Currency, Money};
    use crate::repo::{accounts, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database) -> crate::model::AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
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
        .unwrap()
    }

    fn transaction(db: &Database) -> TransactionId {
        let account_id = account(db);
        transactions::insert_planned(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: date!(2026 - 03 - 13),
                amount: Money {
                    minor: -8999,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: Some("Shop".to_owned()),
                purpose: Some("Receipt".to_owned()),
                category_id: None,
                import_hash: "doc-transaction".to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        transactions::list_for_account(db, account_id, None, None).unwrap()[0].id
    }

    fn document(db: &Database, name: &str, transaction_id: Option<TransactionId>) -> Document {
        create(
            db,
            NewDocument {
                file_name: name.to_owned(),
                stored_path: format!("documents/{name}"),
                mime_type: "application/pdf".to_owned(),
                transaction_id,
            },
        )
        .unwrap()
    }

    #[test]
    fn create_stores_the_document_and_its_upload_time() {
        let (_temp, db) = database();
        let created = document(&db, "invoice.pdf", None);

        assert_eq!(created.file_name, "invoice.pdf");
        assert_eq!(created.stored_path, "documents/invoice.pdf");
        assert_eq!(created.mime_type, "application/pdf");
        assert_eq!(created.transaction_id, None);
        assert_eq!(get(&db, created.id).unwrap(), Some(created.clone()));
        assert_eq!(list(&db).unwrap(), vec![created]);
    }

    #[test]
    fn link_unlink_and_delete() {
        let (_temp, db) = database();
        let transaction_id = transaction(&db);
        let created = document(&db, "receipt.pdf", None);

        let linked = link_to_transaction(&db, created.id, transaction_id).unwrap().unwrap();
        assert_eq!(linked.transaction_id, Some(transaction_id));

        let item = get_item(&db, created.id).unwrap().unwrap();
        assert_eq!(item.transaction_id, Some(transaction_id));
        assert_eq!(item.transaction_label.as_deref(), Some("Shop (2026-03-13)"));
        assert!(!item.has_ocr);
        assert_eq!(item.ocr_confidence, None);
        assert!(item.is_pdf());
        assert!(!item.is_image());

        let unlinked = unlink_from_transaction(&db, created.id).unwrap().unwrap();
        assert_eq!(unlinked.transaction_id, None);
        assert_eq!(get_item(&db, created.id).unwrap().unwrap().transaction_label, None);

        assert!(delete(&db, created.id).unwrap());
        assert_eq!(get(&db, created.id).unwrap(), None);
        assert!(!delete(&db, created.id).unwrap(), "a second delete finds nothing");
    }

    #[test]
    fn find_items_searches_file_name_and_ocr_text_and_filters() {
        let (_temp, db) = database();
        let transaction_id = transaction(&db);
        let invoice = document(&db, "invoice.pdf", None);
        let notes = document(&db, "notes.pdf", None);
        let linked = document(&db, "linked.pdf", Some(transaction_id));
        save_ocr_text(&db, notes.id, "Rechnung Nr. 4711", "de", Some(0.94)).unwrap();

        let by_ocr = find_items(&db, Some("4711"), false, None).unwrap();
        assert_eq!(
            by_ocr.into_iter().map(|item| item.id).collect::<Vec<_>>(),
            vec![notes.id]
        );

        let by_name = find_items(&db, Some("invoice"), false, None).unwrap();
        assert_eq!(
            by_name.into_iter().map(|item| item.id).collect::<Vec<_>>(),
            vec![invoice.id]
        );
        assert!(
            find_items(&db, Some("nothing-like-this"), false, None)
                .unwrap()
                .is_empty()
        );

        let unlinked = find_items(&db, None, true, None).unwrap();
        assert_eq!(unlinked.len(), 2);
        assert!(unlinked.iter().all(|item| item.transaction_id.is_none()));

        let only_linked = find_items(&db, None, false, Some(transaction_id)).unwrap();
        assert_eq!(
            only_linked.into_iter().map(|item| item.id).collect::<Vec<_>>(),
            vec![linked.id]
        );

        let notes_item = get_item(&db, notes.id).unwrap().unwrap();
        assert!(notes_item.has_ocr);
        assert_eq!(notes_item.ocr_confidence, Some(0.94));
    }

    #[test]
    fn find_items_returns_the_newest_first() {
        let (_temp, db) = database();
        db.with_conn(|conn| {
            conn.execute(
                "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at)
                 VALUES ('old.pdf', 'old.pdf', 'application/pdf', '2026-01-01T00:00:00Z')",
                [],
            )?;
            conn.execute(
                "INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at)
                 VALUES ('new.pdf', 'new.pdf', 'application/pdf', '2026-02-01T00:00:00Z')",
                [],
            )?;
            Ok(())
        })
        .unwrap();

        assert_eq!(
            find_items(&db, None, false, None)
                .unwrap()
                .into_iter()
                .map(|item| item.file_name)
                .collect::<Vec<_>>(),
            vec!["new.pdf".to_owned(), "old.pdf".to_owned()]
        );
    }

    #[test]
    fn search_matches_like_wildcards_literally() {
        let (_temp, db) = database();
        document(&db, "100% Shop.pdf", None);
        document(&db, "100x Shop.pdf", None);
        document(&db, "under_score.pdf", None);
        document(&db, "underXscore.pdf", None);

        assert_eq!(
            find_items(&db, Some("100%"), false, None)
                .unwrap()
                .into_iter()
                .map(|item| item.file_name)
                .collect::<Vec<_>>(),
            vec!["100% Shop.pdf".to_owned()]
        );
        assert_eq!(
            find_items(&db, Some("under_score"), false, None)
                .unwrap()
                .into_iter()
                .map(|item| item.file_name)
                .collect::<Vec<_>>(),
            vec!["under_score.pdf".to_owned()]
        );
    }

    #[test]
    fn save_ocr_text_replaces_and_delete_cascades_but_leaves_the_file() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let file = temp.path().join("receipt.pdf");
        std::fs::write(&file, "pdf bytes").unwrap();
        let created = create(
            &db,
            NewDocument {
                file_name: "receipt.pdf".to_owned(),
                stored_path: file.to_string_lossy().into_owned(),
                mime_type: "application/pdf".to_owned(),
                transaction_id: None,
            },
        )
        .unwrap();

        save_ocr_text(&db, created.id, "first", "de", Some(0.5)).unwrap();
        save_ocr_text(&db, created.id, "second", "en", None).unwrap();

        assert_eq!(get_ocr_text(&db, created.id).unwrap().as_deref(), Some("second"));
        let item = get_item(&db, created.id).unwrap().unwrap();
        assert!(item.has_ocr);
        assert_eq!(item.ocr_confidence, None);
        let ocr_rows: i64 = db
            .with_conn(|conn| {
                conn.query_row(
                    "SELECT count(*) FROM ocr_texts WHERE document_id = ?1",
                    [created.id],
                    |row| row.get(0),
                )
                .map_err(crate::db::DbError::from)
            })
            .unwrap();
        assert_eq!(ocr_rows, 1, "saving replaces the previous OCR text");

        assert!(delete(&db, created.id).unwrap());
        let ocr_after: i64 = db
            .with_conn(|conn| {
                conn.query_row("SELECT count(*) FROM ocr_texts", [], |row| row.get(0))
                    .map_err(crate::db::DbError::from)
            })
            .unwrap();
        assert_eq!(ocr_after, 0, "the foreign key removes the OCR text");
        assert!(file.exists(), "deleting a row never deletes the file");
    }

    #[test]
    fn unknown_documents_are_not_found() {
        let (_temp, db) = database();

        assert_eq!(get(&db, 999).unwrap(), None);
        assert_eq!(get_item(&db, 999).unwrap(), None);
        assert!(matches!(
            save_ocr_text(&db, 999, "text", "de", None).unwrap_err(),
            RepoError::NotFound
        ));
    }
}
