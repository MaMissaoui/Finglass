//! Document management: storage, OCR, linking and transaction suggestions.
//!
//! Documents live in `<profile dir>/documents/`. A stored file is named `<uuid>_<sanitized name>` and is made
//! owner-only on Unix. Deleting a document removes its stored file only when the canonical path is inside the
//! documents folder. This ports FinDesk's `DocumentService` without the loading/error state flows.

use std::cmp::Ordering;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use thiserror::Error;
use time::{Duration, OffsetDateTime};
use uuid::Uuid;

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{DocumentItem, NewDocument, TransactionFilter, TransactionId, TransactionItem};
use crate::money::Currency;
use crate::paths::{AppPaths, PathError};
use crate::receipt;
use crate::repo::{self, RepoError};

use super::limits::{LimitError, MAX_DOCUMENT_BYTES};
use super::ocr::{OcrResult, OcrService};

/// File extensions a document may have.
pub const SUPPORTED_EXTENSIONS: &[&str] = &["pdf", "jpg", "jpeg", "png", "tif", "tiff"];

/// How many days around a receipt date matching transactions are considered.
const MATCH_WINDOW_DAYS: i64 = 14;

/// Everything that can go wrong around a document.
#[derive(Debug, Error)]
pub enum DocumentError {
    /// The source file does not exist; holds the file name.
    #[error("{}", message(Language::En, Key::ErrorFileDoesNotExist, &[Arg::Str(.0.clone())]))]
    FileDoesNotExist(String),
    /// The file is larger than [`MAX_DOCUMENT_BYTES`]; holds the file name.
    #[error("{}", too_large(Language::En, .0))]
    DocumentTooLarge(String),
    /// The file type is not supported; holds the extension.
    #[error("{}", message(Language::En, Key::ErrorUnsupportedFileType, &[Arg::Str(.0.clone())]))]
    UnsupportedFileType(String),
    /// The image is too large to read.
    #[error(transparent)]
    Limit(#[from] LimitError),
    /// A repository call failed.
    #[error(transparent)]
    Repository(#[from] RepoError),
    /// The audit log could not be written.
    #[error(transparent)]
    Db(#[from] DbError),
    /// The stored document row disappeared.
    #[error("document not found")]
    NotFound,
    /// A file operation failed.
    #[error(transparent)]
    Io(#[from] io::Error),
}

impl DocumentError {
    /// The user text in `language`. Technical failures (database, file system) keep their English description.
    #[must_use]
    pub fn message(&self, language: Language) -> String {
        match self {
            Self::FileDoesNotExist(name) => message(language, Key::ErrorFileDoesNotExist, &[Arg::Str(name.clone())]),
            Self::DocumentTooLarge(name) => too_large(language, name),
            Self::UnsupportedFileType(extension) => {
                message(language, Key::ErrorUnsupportedFileType, &[Arg::Str(extension.clone())])
            }
            Self::Limit(error) => error.message(language),
            other => other.to_string(),
        }
    }
}

fn too_large(language: Language, name: &str) -> String {
    message(
        language,
        Key::ErrorDocumentTooLarge,
        &[
            Arg::Str(name.to_owned()),
            Arg::Int((MAX_DOCUMENT_BYTES / (1024 * 1024)) as i64),
        ],
    )
}

/// The result of an upload.
#[derive(Debug, Clone, PartialEq)]
pub struct UploadResult {
    /// The stored document.
    pub document: DocumentItem,
    /// The extracted text, when there is any.
    pub ocr: Option<OcrResult>,
}

/// A transaction that plausibly matches a receipt, with a 0..1 score.
#[derive(Debug, Clone, PartialEq)]
pub struct MatchSuggestion {
    /// The candidate transaction.
    pub transaction: TransactionItem,
    /// The match score from 0 to 1.
    pub score: f64,
}

/// Document storage and lookup.
pub struct DocumentService<'a> {
    db: &'a Database,
    ocr: OcrService,
    documents_dir: PathBuf,
}

impl<'a> DocumentService<'a> {
    /// Builds a service for one database and documents folder.
    #[must_use]
    pub fn new(db: &'a Database, ocr: OcrService, documents_dir: impl Into<PathBuf>) -> Self {
        Self {
            db,
            ocr,
            documents_dir: documents_dir.into(),
        }
    }

    /// The folder for a profile's documents, as M8a backs it up.
    pub fn default_documents_directory(paths: &AppPaths, slug: &str) -> Result<PathBuf, PathError> {
        Ok(paths.profile_dir(slug)?.join("documents"))
    }

    /// The folder this service stores documents in.
    #[must_use]
    pub fn documents_dir(&self) -> &Path {
        &self.documents_dir
    }

    /// Uploads a document: copies it into the documents folder, records it, and extracts text.
    pub fn upload_document(
        &self,
        source: &Path,
        transaction_id: Option<TransactionId>,
        perform_ocr: bool,
    ) -> Result<UploadResult, DocumentError> {
        let file_name = file_name_of(source);

        if !source.is_file() {
            return Err(DocumentError::FileDoesNotExist(file_name));
        }

        if fs::metadata(source)?.len() > MAX_DOCUMENT_BYTES {
            return Err(DocumentError::DocumentTooLarge(file_name));
        }

        let mime_type = detect_mime_type(source);
        if mime_type == "application/octet-stream" {
            let extension = extension_of(source);
            return Err(DocumentError::UnsupportedFileType(extension));
        }

        AppPaths::ensure_dir(&self.documents_dir)?;
        let stored_name = format!("{}_{}", Uuid::new_v4(), sanitize_file_name(&file_name));
        let destination = self.documents_dir.join(stored_name);
        fs::copy(source, &destination)?;
        secure_file(&destination)?;

        let created = repo::documents::create(
            self.db,
            NewDocument {
                file_name: file_name.clone(),
                stored_path: destination.to_string_lossy().into_owned(),
                mime_type: mime_type.to_owned(),
                transaction_id,
            },
        )?;

        let ocr = if perform_ocr {
            self.ocr.process_document(&destination)
        } else {
            None
        };

        if let Some(result) = &ocr
            && !result.text.trim().is_empty()
        {
            repo::documents::save_ocr_text(
                self.db,
                created.id,
                &result.text,
                &result.language,
                Some(result.confidence),
            )?;
            audit::log(
                self.db,
                "OCR_PROCESSED",
                Actor::System,
                &format!("Text extracted ({}) for: {file_name}", result.source.as_str()),
            )?;
        }

        audit::log(
            self.db,
            "DOCUMENT_UPLOADED",
            Actor::User,
            &format!("Uploaded: {file_name}"),
        )?;

        let document = repo::documents::get_item(self.db, created.id)?.ok_or(DocumentError::NotFound)?;
        Ok(UploadResult { document, ocr })
    }

    /// Re-runs text extraction for an existing document.
    pub fn reprocess_ocr(&self, document_id: i64) -> Result<Option<OcrResult>, DocumentError> {
        let Some(document) = repo::documents::get(self.db, document_id)? else {
            return Ok(None);
        };

        let result = self.ocr.process_document(Path::new(&document.stored_path));
        if let Some(result) = &result
            && !result.text.trim().is_empty()
        {
            repo::documents::save_ocr_text(
                self.db,
                document_id,
                &result.text,
                &result.language,
                Some(result.confidence),
            )?;
            audit::log(
                self.db,
                "OCR_PROCESSED",
                Actor::User,
                &format!(
                    "Text extracted again ({}) for: {}",
                    result.source.as_str(),
                    document.file_name
                ),
            )?;
        }

        Ok(result)
    }

    /// Links a document to a transaction.
    pub fn link_to_transaction(&self, document_id: i64, transaction_id: TransactionId) -> Result<bool, DocumentError> {
        let linked = repo::documents::link_to_transaction(self.db, document_id, transaction_id)?.is_some();
        if linked {
            audit::log(
                self.db,
                "DOCUMENT_LINKED",
                Actor::User,
                &format!("Linked document {document_id} to transaction {}", transaction_id.0),
            )?;
        }
        Ok(linked)
    }

    /// Removes a document's transaction link.
    pub fn unlink_from_transaction(&self, document_id: i64) -> Result<bool, DocumentError> {
        let unlinked = repo::documents::unlink_from_transaction(self.db, document_id)?.is_some();
        if unlinked {
            audit::log(
                self.db,
                "DOCUMENT_UNLINKED",
                Actor::User,
                &format!("Unlinked document {document_id}"),
            )?;
        }
        Ok(unlinked)
    }

    /// Searches by file name or OCR text.
    pub fn search_documents(&self, query: &str) -> Result<Vec<DocumentItem>, DocumentError> {
        Ok(repo::documents::find_items(self.db, Some(query), false, None)?)
    }

    /// Lists documents, optionally filtered by text or by missing transaction links.
    pub fn list_documents(&self, query: Option<&str>, unlinked_only: bool) -> Result<Vec<DocumentItem>, DocumentError> {
        Ok(repo::documents::find_items(self.db, query, unlinked_only, None)?)
    }

    /// Lists documents without a linked transaction.
    pub fn get_unlinked_documents(&self) -> Result<Vec<DocumentItem>, DocumentError> {
        Ok(repo::documents::find_items(self.db, None, true, None)?)
    }

    /// Lists documents linked to one transaction.
    pub fn get_documents_for_transaction(
        &self,
        transaction_id: TransactionId,
    ) -> Result<Vec<DocumentItem>, DocumentError> {
        Ok(repo::documents::find_items(self.db, None, false, Some(transaction_id))?)
    }

    /// The stored OCR text of a document.
    pub fn get_ocr_text(&self, document_id: i64) -> Result<Option<String>, DocumentError> {
        Ok(repo::documents::get_ocr_text(self.db, document_id)?)
    }

    /// Suggests transactions for a document based on the total and date found in its text.
    ///
    /// An expense with the same absolute amount scores highest; closer dates score higher.
    pub fn suggest_transactions(&self, document_id: i64, limit: usize) -> Result<Vec<MatchSuggestion>, DocumentError> {
        let Some(text) = repo::documents::get_ocr_text(self.db, document_id)? else {
            return Ok(Vec::new());
        };

        let fields = receipt::parse(&text, Currency::EUR, OffsetDateTime::now_utc().date());
        if fields.amounts.is_empty() {
            return Ok(Vec::new());
        }

        let anchor = fields.date;
        let filter = TransactionFilter {
            start_date: anchor.map(|date| date - Duration::days(MATCH_WINDOW_DAYS)),
            end_date: anchor.map(|date| date + Duration::days(MATCH_WINDOW_DAYS)),
            ..TransactionFilter::default()
        };
        let candidates = repo::transactions::find_items(self.db, &filter, None)?;

        let mut suggestions = Vec::new();
        for transaction in candidates {
            let amount = i64::try_from(transaction.amount.minor.unsigned_abs()).unwrap_or(i64::MAX);
            let amount_score = if let Some(total) = fields.total {
                if total.minor == amount {
                    1.0
                } else if fields.amounts.iter().any(|candidate| candidate.minor == amount) {
                    0.6
                } else {
                    continue;
                }
            } else if fields.amounts.iter().any(|candidate| candidate.minor == amount) {
                0.6
            } else {
                continue;
            };

            let date_score = anchor.map_or(0.5, |date| {
                let days = (transaction.date - date).whole_days().abs() as f64;
                1.0 - (days / f64::from(MATCH_WINDOW_DAYS as i32 + 1))
            });

            suggestions.push(MatchSuggestion {
                transaction,
                score: amount_score * 0.7 + date_score * 0.3,
            });
        }

        suggestions.sort_by(|left, right| right.score.partial_cmp(&left.score).unwrap_or(Ordering::Equal));
        suggestions.truncate(limit);
        Ok(suggestions)
    }

    /// Deletes a document record and its stored file.
    ///
    /// Only files whose canonical path is inside the documents folder are deleted.
    pub fn delete_document(&self, document_id: i64) -> Result<bool, DocumentError> {
        let Some(document) = repo::documents::get(self.db, document_id)? else {
            return Ok(false);
        };

        let file = Path::new(&document.stored_path);
        if file.exists()
            && let (Ok(file), Ok(folder)) = (file.canonicalize(), self.documents_dir.canonicalize())
            && file.starts_with(folder)
        {
            fs::remove_file(file)?;
        }

        repo::documents::delete(self.db, document_id)?;
        audit::log(
            self.db,
            "DOCUMENT_DELETED",
            Actor::User,
            &format!("Deleted document: {}", document.file_name),
        )?;
        Ok(true)
    }
}

/// The MIME type for a supported file extension.
#[must_use]
pub fn detect_mime_type(file: &Path) -> &'static str {
    let extension = extension_of(file);
    match extension.as_str() {
        "pdf" => "application/pdf",
        "jpg" | "jpeg" => "image/jpeg",
        "png" => "image/png",
        "tiff" | "tif" => "image/tiff",
        _ => "application/octet-stream",
    }
}

/// Makes a user-supplied file name safe to use on disk.
///
/// Keeps the last path segment, drops control characters and bounds the length. The display name is stored
/// separately and unchanged.
#[must_use]
pub fn sanitize_file_name(name: &str) -> String {
    let base = name.rsplit(['/', '\\']).next().unwrap_or(name);
    let cleaned: String = base.chars().filter(|character| !character.is_control()).collect();
    let cleaned = cleaned.trim();
    let chosen = if cleaned.is_empty() { "document" } else { cleaned };
    chosen.chars().take(150).collect()
}

fn extension_of(file: &Path) -> String {
    file.extension()
        .map(|extension| extension.to_string_lossy().into_owned())
        .unwrap_or_default()
}

fn file_name_of(file: &Path) -> String {
    file.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "document".to_owned())
}

fn message(language: Language, key: Key, args: &[Arg]) -> String {
    i18n::format(language, key, args)
}

fn secure_file(path: &Path) -> io::Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))
    }

    #[cfg(not(unix))]
    {
        let _ = path;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::fs::OpenOptions;

    use time::macros::date;

    use super::*;
    use crate::documents::ocr::{OcrEngine, Recognition, Source};
    use crate::documents::test_support::{crafted_png, empty_pdf, text_pdf};
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

    fn transaction(
        db: &Database,
        account_id: crate::model::AccountId,
        day: time::Date,
        minor: i64,
        partner: &str,
        hash: &str,
    ) -> TransactionId {
        transactions::insert_planned(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount: Money {
                    minor,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: Some(partner.to_owned()),
                purpose: Some("Receipt".to_owned()),
                category_id: None,
                import_hash: hash.to_owned(),
                created_at: OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        transactions::list_for_account(db, account_id, None, None)
            .unwrap()
            .into_iter()
            .find(|row| row.import_hash == hash)
            .unwrap()
            .id
    }

    fn service<'a>(db: &'a Database, documents_dir: PathBuf) -> DocumentService<'a> {
        DocumentService::new(db, OcrService::without_ocr(), documents_dir)
    }

    fn audit_actions(db: &Database, action: &str) -> Vec<crate::audit::AuditEntry> {
        audit::recent(db, 1000)
            .unwrap()
            .into_iter()
            .filter(|entry| entry.action == action)
            .collect()
    }

    #[test]
    fn upload_copies_file_extracts_pdf_text_and_makes_it_searchable() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let docs_dir = temp.path().join("documents");
        let service = service(&db, docs_dir.clone());
        let pdf = temp.path().join("invoice.pdf");
        text_pdf(
            &pdf,
            &["Rechnung Nr. 4711", "Datum: 12.03.2026", "Gesamtbetrag 89,99 EUR"],
        );

        let result = service.upload_document(&pdf, None, true).unwrap();

        assert_eq!(result.ocr.as_ref().map(|ocr| ocr.source), Some(Source::TextLayer));
        assert!(result.document.has_ocr);
        assert!(Path::new(&result.document.stored_path).starts_with(&docs_dir));
        assert_eq!(
            service
                .search_documents("4711")
                .unwrap()
                .iter()
                .map(|doc| doc.id)
                .collect::<Vec<_>>(),
            vec![result.document.id]
        );
        assert_eq!(
            service
                .search_documents("invoice")
                .unwrap()
                .iter()
                .map(|doc| doc.id)
                .collect::<Vec<_>>(),
            vec![result.document.id]
        );
        assert!(service.search_documents("nothing-like-this").unwrap().is_empty());
    }

    #[test]
    fn suggests_the_transaction_with_the_receipt_total_near_the_receipt_date() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let account_id = account(&db);
        let matching = transaction(&db, account_id, date!(2026 - 03 - 13), -8999, "Amazon", "match");
        transaction(&db, account_id, date!(2026 - 03 - 13), -1200, "Other", "other");
        transaction(&db, account_id, date!(2025 - 03 - 13), -8999, "Year earlier", "year");

        let docs_dir = temp.path().join("documents");
        let service = service(&db, docs_dir);
        let pdf = temp.path().join("r.pdf");
        text_pdf(
            &pdf,
            &["Datum: 12.03.2026", "Zwischensumme 75,62", "Gesamtbetrag 89,99 EUR"],
        );
        let document = service.upload_document(&pdf, None, true).unwrap().document;

        let suggestions = service.suggest_transactions(document.id, 5).unwrap();

        assert_eq!(suggestions.first().unwrap().transaction.id, matching);
        assert!(
            suggestions
                .iter()
                .all(|suggestion| suggestion.transaction.partner_name.as_deref() != Some("Year earlier"))
        );
        assert!(
            suggestions
                .iter()
                .all(|suggestion| suggestion.transaction.partner_name.as_deref() != Some("Other"))
        );
    }

    #[test]
    fn link_unlink_and_delete() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let account_id = account(&db);
        let transaction_id = transaction(&db, account_id, date!(2026 - 03 - 13), -1000, "Shop", "shop");
        let service = service(&db, temp.path().join("documents"));
        let pdf = temp.path().join("r.pdf");
        text_pdf(&pdf, &["Some receipt text here that is long enough"]);
        let document = service.upload_document(&pdf, None, true).unwrap().document;

        assert!(service.link_to_transaction(document.id, transaction_id).unwrap());
        let linked = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|row| row.id == transaction_id)
            .unwrap();
        assert_eq!(linked.document_count, 1);
        assert!(service.get_unlinked_documents().unwrap().is_empty());

        assert!(service.unlink_from_transaction(document.id).unwrap());
        assert_eq!(service.get_unlinked_documents().unwrap().len(), 1);

        assert!(service.delete_document(document.id).unwrap());
        assert!(!Path::new(&document.stored_path).exists());
        assert!(service.list_documents(None, false).unwrap().is_empty());
        assert_eq!(service.get_ocr_text(document.id).unwrap(), None);
    }

    #[test]
    fn rejects_unsupported_and_missing_files() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = service(&db, temp.path().join("documents"));
        let txt = temp.path().join("notes.txt");
        fs::write(&txt, "hi").unwrap();

        assert!(matches!(
            service.upload_document(&txt, None, true).unwrap_err(),
            DocumentError::UnsupportedFileType(_)
        ));
        assert!(matches!(
            service
                .upload_document(&temp.path().join("missing.pdf"), None, true)
                .unwrap_err(),
            DocumentError::FileDoesNotExist(_)
        ));
    }

    #[test]
    fn rejects_files_larger_than_the_document_size_limit() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let docs_dir = temp.path().join("documents");
        let service = service(&db, docs_dir.clone());
        let file = temp.path().join("big.pdf");
        OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .open(&file)
            .unwrap()
            .set_len(MAX_DOCUMENT_BYTES + 1)
            .unwrap();

        let docs_before = fs::read_dir(&docs_dir).map_or(0, |entries| entries.count());
        let repo_before = service.list_documents(None, false).unwrap().len();

        let error = service.upload_document(&file, None, true).unwrap_err();
        assert!(matches!(error, DocumentError::DocumentTooLarge(_)));
        let english = error.message(Language::En);
        let german = error.message(Language::De);
        assert!(
            english.contains("big.pdf") && german.contains("big.pdf"),
            "{english} / {german}"
        );
        assert_ne!(english, german, "the message follows the language");
        assert_eq!(error.to_string(), english);
        assert_eq!(
            fs::read_dir(&docs_dir).map_or(0, |entries| entries.count()),
            docs_before
        );
        assert_eq!(service.list_documents(None, false).unwrap().len(), repo_before);
    }

    #[test]
    fn images_without_an_ocr_engine_are_stored_without_text() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = service(&db, temp.path().join("documents"));
        let png = temp.path().join("scan.png");
        crafted_png(&png, 10, 10);

        let result = service.upload_document(&png, None, true).unwrap();

        assert_eq!(result.ocr, None);
        assert!(!result.document.has_ocr);
    }

    #[test]
    fn reprocessing_ocr_is_audited_only_when_text_is_extracted() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = service(&db, temp.path().join("documents"));

        let with_text = temp.path().join("reprocess.pdf");
        text_pdf(
            &with_text,
            &["Rechnung Nr. 4711", "Datum: 12.03.2026", "Gesamtbetrag 89,99 EUR"],
        );
        let with_text = service.upload_document(&with_text, None, false).unwrap().document;
        assert!(audit_actions(&db, "OCR_PROCESSED").is_empty(), "upload skipped OCR");

        service.reprocess_ocr(with_text.id).unwrap();

        let entries = audit_actions(&db, "OCR_PROCESSED");
        assert_eq!(entries.len(), 1);
        assert!(entries[0].details.contains(&with_text.file_name));

        let empty = temp.path().join("empty.pdf");
        empty_pdf(&empty);
        let without_text = service.upload_document(&empty, None, false).unwrap().document;
        assert_eq!(
            service.reprocess_ocr(without_text.id).unwrap(),
            None,
            "nothing extracted"
        );
        assert_eq!(audit_actions(&db, "OCR_PROCESSED").len(), 1, "nothing was logged");
    }

    #[test]
    fn receipt_parser_handles_german_and_english_formats() {
        let german = receipt::parse(
            "EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95",
            Currency::EUR,
            date!(2026 - 11 - 07),
        );
        assert_eq!(german.total.map(|total| total.minor), Some(129_995));
        assert_eq!(german.date, Some(date!(2026 - 10 - 03)));
        assert!(
            !german.amounts.iter().any(|amount| amount.minor == 310),
            "date fragments are not amounts"
        );

        let english = receipt::parse(
            "Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00",
            Currency::EUR,
            date!(2026 - 11 - 07),
        );
        assert_eq!(english.total.map(|total| total.minor), Some(119_000));
        assert_eq!(english.date, Some(date!(2026 - 01 - 05)));

        assert_eq!(
            receipt::parse("no numbers here", Currency::EUR, date!(2026 - 11 - 07)).total,
            None
        );
    }

    #[test]
    fn a_file_name_is_reduced_to_a_safe_last_segment() {
        assert_eq!(sanitize_file_name("../evil\\receipt\n.pdf"), "receipt.pdf");
        assert_eq!(sanitize_file_name("  invoice\u{0}2026.pdf  "), "invoice2026.pdf");
        assert_eq!(sanitize_file_name(""), "document");
        assert_eq!(sanitize_file_name(".../\u{7f}"), "document");
        assert_eq!(sanitize_file_name(&"a".repeat(200)).chars().count(), 150);
    }

    #[test]
    fn a_control_character_file_name_is_stored_inside_the_documents_folder() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let docs_dir = temp.path().join("documents");
        let service = service(&db, docs_dir.clone());
        let source = temp.path().join("invoice\n2026.pdf");
        text_pdf(&source, &["Some receipt text here that is long enough"]);

        let document = service.upload_document(&source, None, true).unwrap().document;

        assert_eq!(document.file_name, "invoice\n2026.pdf", "the display name is unchanged");
        let stored = Path::new(&document.stored_path);
        assert_eq!(stored.parent(), Some(docs_dir.as_path()));
        assert!(stored.exists());
        assert!(
            stored
                .file_name()
                .unwrap()
                .to_string_lossy()
                .ends_with("_invoice2026.pdf")
        );
    }

    #[test]
    fn deleting_a_document_outside_the_documents_folder_leaves_the_file_alone() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let docs_dir = temp.path().join("documents");
        let service = service(&db, docs_dir);
        let outside = temp.path().join("outside.pdf");
        fs::write(&outside, "keep me").unwrap();
        let document = repo::documents::create(
            &db,
            NewDocument {
                file_name: "outside.pdf".to_owned(),
                stored_path: outside.to_string_lossy().into_owned(),
                mime_type: "application/pdf".to_owned(),
                transaction_id: None,
            },
        )
        .unwrap();

        assert!(service.delete_document(document.id).unwrap());

        assert!(outside.exists(), "a file outside documents/ must never be deleted");
        assert!(service.list_documents(None, false).unwrap().is_empty());
    }

    #[test]
    fn a_truncated_pdf_gives_no_text_and_no_panic() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = service(&db, temp.path().join("documents"));
        let pdf = temp.path().join("truncated.pdf");
        text_pdf(&pdf, &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"]);
        let bytes = fs::read(&pdf).unwrap();
        fs::write(&pdf, &bytes[..bytes.len() / 2]).unwrap();

        let result = service.upload_document(&pdf, None, true).unwrap();

        assert_eq!(result.ocr, None);
        assert!(!result.document.has_ocr);
    }

    #[test]
    fn audit_entries_contain_file_names_and_ids_but_never_ocr_text() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = service(&db, temp.path().join("documents"));
        let pdf = temp.path().join("confidential.pdf");
        text_pdf(
            &pdf,
            &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR", "Bankleitzahl 12345678"],
        );

        let document = service.upload_document(&pdf, None, true).unwrap().document;

        let entries = audit::recent(&db, 1000).unwrap();
        assert!(entries.iter().any(|entry| entry.action == "DOCUMENT_UPLOADED"));
        assert!(entries.iter().any(|entry| entry.action == "OCR_PROCESSED"));
        for entry in &entries {
            assert!(!entry.details.contains("Gesamtbetrag"), "{:?}", entry.details);
            assert!(!entry.details.contains("4711"), "{:?}", entry.details);
            assert!(!entry.details.contains("12345678"), "{:?}", entry.details);
        }
        assert!(
            entries
                .iter()
                .filter(|entry| entry.action == "DOCUMENT_UPLOADED")
                .any(|entry| entry.details.contains(&document.file_name))
        );
    }

    struct CountingEngine;

    impl OcrEngine for CountingEngine {
        fn recognize(&self, _image: &Path) -> Result<Recognition, crate::documents::ocr::OcrError> {
            Ok(Recognition {
                text: String::new(),
                confidence: 0.0,
            })
        }
    }

    #[test]
    fn an_oversized_image_is_stored_without_calling_the_ocr_engine() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let service = DocumentService::new(
            &db,
            OcrService::new(Some(std::sync::Arc::new(CountingEngine))),
            temp.path().join("documents"),
        );
        let png = temp.path().join("huge.png");
        crafted_png(&png, 60_000, 60_000);

        let result = service.upload_document(&png, None, true).unwrap();

        assert_eq!(result.ocr, None);
        assert!(!result.document.has_ocr);
    }
}
