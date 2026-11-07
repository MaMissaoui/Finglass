//! The Documents page state, without any UI or coroutine machinery.
//!
//! This ports FinDesk's `DocumentsViewModel`: the same tabs, search, counts, upload batch, selection, linking and OCR
//! actions, but synchronous. Every write goes through [`crate::documents::service`], which writes the audit log.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use crate::db::Database;
use crate::documents::ocr::{OcrEngine, OcrService};
use crate::documents::service::{DocumentError, DocumentService, MatchSuggestion};
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{DocumentItem, TransactionFilter, TransactionId, TransactionItem};
use crate::paths::{AppPaths, PathError};
use crate::repo;

/// The filter tabs of the Documents page.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum DocumentTab {
    /// Every document.
    All,
    /// Documents without a linked transaction.
    Unlinked,
    /// Documents with a linked transaction.
    Linked,
}

impl DocumentTab {
    /// All tabs, in the order the page shows them.
    pub const ALL: &[DocumentTab] = &[DocumentTab::All, DocumentTab::Unlinked, DocumentTab::Linked];

    /// The i18n key for this tab's label.
    #[must_use]
    pub fn label_key(self) -> Key {
        match self {
            Self::All => Key::TabAllDocuments,
            Self::Unlinked => Key::TabUnlinked,
            Self::Linked => Key::TabLinked,
        }
    }
}

/// How many documents each tab shows for the current search.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct DocumentCounts {
    /// Documents in [`DocumentTab::All`].
    pub all: usize,
    /// Documents in [`DocumentTab::Unlinked`].
    pub unlinked: usize,
    /// Documents in [`DocumentTab::Linked`].
    pub linked: usize,
}

impl DocumentCounts {
    /// The count for `tab`.
    #[must_use]
    pub fn get(self, tab: DocumentTab) -> usize {
        match tab {
            DocumentTab::All => self.all,
            DocumentTab::Unlinked => self.unlinked,
            DocumentTab::Linked => self.linked,
        }
    }
}

/// The Documents page state.
///
/// It mirrors FinDesk's `DocumentsState`; `profile` and `language` are kept here because the actions are synchronous
/// and take only the database and the data folder.
#[derive(Debug, Clone, PartialEq)]
pub struct DocumentsState {
    /// The documents shown for the current tab and search.
    pub documents: Vec<DocumentItem>,
    /// How many documents each tab shows for the current search.
    pub counts: DocumentCounts,
    /// The selected tab.
    pub tab: DocumentTab,
    /// The current search text.
    pub search_query: String,
    /// The selected document.
    pub selected_document: Option<DocumentItem>,
    /// The selected document's OCR text.
    pub selected_ocr_text: Option<String>,
    /// Transactions that plausibly match the selected document.
    pub suggestions: Vec<MatchSuggestion>,
    /// Transactions offered in the manual link picker.
    pub link_candidates: Vec<TransactionItem>,
    /// Whether an OCR engine is available.
    pub is_ocr_available: bool,
    /// Whether an upload batch is running.
    pub is_uploading: bool,
    /// Whether a refresh is running.
    pub is_loading: bool,
    /// How many files the current or last upload batch has processed.
    pub upload_done: usize,
    /// How many files the current or last upload batch contains.
    pub upload_total: usize,
    /// The last error, already localized.
    pub error: Option<String>,
    /// The last success message, already localized.
    pub message: Option<String>,
    /// The language for user-visible texts.
    pub language: Language,
    /// The profile whose documents folder the actions use.
    pub profile: String,
}

impl DocumentsState {
    /// Builds an empty state.
    #[must_use]
    pub fn new(language: Language, is_ocr_available: bool) -> Self {
        Self {
            documents: Vec::new(),
            counts: DocumentCounts::default(),
            tab: DocumentTab::All,
            search_query: String::new(),
            selected_document: None,
            selected_ocr_text: None,
            suggestions: Vec::new(),
            link_candidates: Vec::new(),
            is_ocr_available,
            is_uploading: false,
            is_loading: false,
            upload_done: 0,
            upload_total: 0,
            error: None,
            message: None,
            language,
            profile: "default".to_owned(),
        }
    }

    /// Selects the profile whose documents folder the actions use.
    pub fn set_profile(&mut self, profile: impl Into<String>) {
        self.profile = profile.into();
    }

    /// Reloads the list and counts for the current tab and search.
    pub fn load(&mut self, db: &Database, paths: &AppPaths) {
        self.is_loading = true;
        self.error = None;
        self.refresh_from(db, paths);
    }

    /// Selects a tab and reloads the list.
    pub fn select_tab(&mut self, tab: DocumentTab, db: &Database, paths: &AppPaths) {
        self.tab = tab;
        self.load(db, paths);
    }

    /// Changes the search and reloads the list synchronously.
    ///
    /// FinDesk debounces this call; the synchronous state has nothing to debounce.
    pub fn search(&mut self, query: &str, db: &Database, paths: &AppPaths) {
        self.search_query = query.to_owned();
        self.refresh_from(db, paths);
    }

    /// Uploads a batch of files.
    ///
    /// A failing file is reported in [`Self::error`] and the rest of the batch is still uploaded, exactly as FinDesk
    /// does. `progress` is called after every file with the number of processed files and the batch size.
    pub fn upload(
        &mut self,
        files: &[PathBuf],
        transaction_id: Option<TransactionId>,
        db: &Database,
        paths: &AppPaths,
        engine: Option<Arc<dyn OcrEngine>>,
        mut progress: impl FnMut(usize, usize),
    ) {
        self.is_uploading = true;
        self.error = None;
        self.upload_done = 0;
        self.upload_total = files.len();

        let outcome = with_service(db, paths, self.profile.clone(), engine, |service| {
            let mut failures = Vec::new();
            let mut with_text = 0_usize;

            for (index, file) in files.iter().enumerate() {
                match service.upload_document(file, transaction_id, true) {
                    Ok(result) => {
                        if result.ocr.as_ref().is_some_and(|ocr| !ocr.text.trim().is_empty()) {
                            with_text += 1;
                        }
                    }
                    Err(error) => failures.push(format!("{}: {}", file_name(file), error.message(self.language))),
                }

                self.upload_done = index + 1;
                progress(index + 1, files.len());
            }

            let refreshed = self.refresh(service);
            (failures, with_text, refreshed)
        });

        match outcome {
            Ok((failures, with_text, refreshed)) => {
                if let Err(error) = refreshed {
                    self.fail_document(&error);
                    return;
                }

                let uploaded = files.len().saturating_sub(failures.len());
                self.is_uploading = false;
                self.message = (uploaded > 0).then(|| {
                    i18n::format(
                        self.language,
                        Key::MessageUploaded,
                        &[Arg::Int(uploaded as i64), Arg::Int(with_text as i64)],
                    )
                });
                self.error = (!failures.is_empty()).then(|| failures.join("\n"));
            }
            Err(error) => self.fail_path(&error),
        }
    }

    /// Selects a document and loads its OCR text, matching suggestions and link candidates.
    pub fn select_document(&mut self, document: Option<DocumentItem>, db: &Database, paths: &AppPaths) {
        self.selected_document = document.clone();
        self.selected_ocr_text = None;
        self.suggestions.clear();
        self.link_candidates.clear();

        let Some(document) = document else {
            return;
        };

        let outcome = with_service(db, paths, self.profile.clone(), None, |service| {
            let text = service.get_ocr_text(document.id)?;
            let suggestions = service.suggest_transactions(document.id, 5)?;
            let candidates = repo::transactions::find_items(db, &TransactionFilter::default(), Some(200))
                .map_err(DocumentError::from)?;
            Ok::<_, DocumentError>((text, suggestions, candidates))
        });

        match outcome {
            Ok(Ok((text, suggestions, candidates))) => {
                if self
                    .selected_document
                    .as_ref()
                    .is_some_and(|selected| selected.id == document.id)
                {
                    self.selected_ocr_text = text;
                    self.suggestions = suggestions;
                    self.link_candidates = candidates;
                }
            }
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    /// Links a document to a transaction, reloads the list and reports success.
    pub fn link_to_transaction(
        &mut self,
        document_id: i64,
        transaction_id: TransactionId,
        db: &Database,
        paths: &AppPaths,
    ) {
        let outcome = with_service(db, paths, self.profile.clone(), None, |service| {
            service.link_to_transaction(document_id, transaction_id)?;
            self.refresh(service)
        });

        match outcome {
            Ok(Ok(())) => self.message = Some(i18n::text(self.language, Key::MessageDocumentLinked).to_owned()),
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    /// Removes a document's transaction link and reloads the list.
    pub fn unlink(&mut self, document_id: i64, db: &Database, paths: &AppPaths) {
        let outcome = with_service(db, paths, self.profile.clone(), None, |service| {
            service.unlink_from_transaction(document_id)?;
            self.refresh(service)
        });

        match outcome {
            Ok(Ok(())) => {}
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    /// Runs OCR again for a document, reloads the list and reports whether text was extracted.
    pub fn reprocess_ocr(
        &mut self,
        document_id: i64,
        db: &Database,
        paths: &AppPaths,
        engine: Option<Arc<dyn OcrEngine>>,
    ) {
        let outcome = with_service(db, paths, self.profile.clone(), engine, |service| {
            let result = service.reprocess_ocr(document_id)?;
            self.refresh(service)?;
            Ok::<_, DocumentError>(result.is_some_and(|ocr| !ocr.text.trim().is_empty()))
        });

        match outcome {
            Ok(Ok(has_text)) => {
                if let Some(selected) = self
                    .selected_document
                    .clone()
                    .filter(|document| document.id == document_id)
                {
                    self.select_document(Some(selected), db, paths);
                }
                let key = if has_text {
                    Key::TextExtracted
                } else {
                    Key::MessageNoTextExtracted
                };
                self.message = Some(i18n::text(self.language, key).to_owned());
            }
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    /// Deletes a document, clears it from the selection and reloads the list.
    pub fn delete(&mut self, document_id: i64, db: &Database, paths: &AppPaths) {
        let outcome = with_service(db, paths, self.profile.clone(), None, |service| {
            service.delete_document(document_id)?;
            if self
                .selected_document
                .as_ref()
                .is_some_and(|document| document.id == document_id)
            {
                self.selected_document = None;
                self.selected_ocr_text = None;
                self.suggestions.clear();
            }
            self.refresh(service)
        });

        match outcome {
            Ok(Ok(())) => self.message = Some(i18n::text(self.language, Key::MessageDocumentDeleted).to_owned()),
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    /// Clears the error and success message.
    pub fn clear_messages(&mut self) {
        self.error = None;
        self.message = None;
    }

    fn refresh_from(&mut self, db: &Database, paths: &AppPaths) {
        let outcome = with_service(db, paths, self.profile.clone(), None, |service| self.refresh(service));

        match outcome {
            Ok(Ok(())) => {}
            Ok(Err(error)) => self.fail_document(&error),
            Err(error) => self.fail_path(&error),
        }
    }

    fn refresh(&mut self, service: &DocumentService<'_>) -> Result<(), DocumentError> {
        let query = self.search_query.clone();
        let all = service.list_documents(Some(&query), false)?;
        let unlinked = service.list_documents(Some(&query), true)?;
        let linked = all
            .iter()
            .filter(|document| document.transaction_id.is_some())
            .cloned()
            .collect::<Vec<_>>();

        let counts = DocumentCounts {
            all: all.len(),
            unlinked: unlinked.len(),
            linked: linked.len(),
        };
        let documents = match self.tab {
            DocumentTab::All => all,
            DocumentTab::Unlinked => unlinked,
            DocumentTab::Linked => linked,
        };
        let selected_document = self
            .selected_document
            .as_ref()
            .and_then(|selected| documents.iter().find(|document| document.id == selected.id).cloned());

        self.documents = documents;
        self.counts = counts;
        self.selected_document = selected_document;
        self.is_loading = false;

        Ok(())
    }

    fn fail_document(&mut self, error: &DocumentError) {
        self.is_loading = false;
        self.is_uploading = false;
        self.error = Some(error.message(self.language));
    }

    fn fail_path(&mut self, error: &PathError) {
        self.is_loading = false;
        self.is_uploading = false;
        self.error = Some(error.to_string());
    }
}

impl Default for DocumentsState {
    fn default() -> Self {
        Self::new(Language::En, false)
    }
}

fn file_name(file: &Path) -> String {
    file.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| "document".to_owned())
}

fn with_service<T>(
    db: &Database,
    paths: &AppPaths,
    profile: String,
    engine: Option<Arc<dyn OcrEngine>>,
    action: impl FnOnce(&DocumentService<'_>) -> T,
) -> Result<T, PathError> {
    let documents_dir = DocumentService::default_documents_directory(paths, &profile)?;
    let service = DocumentService::new(db, OcrService::new(engine), documents_dir);
    Ok(action(&service))
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use time::macros::date;

    use super::*;
    use crate::documents::ocr::{OcrError, Recognition};
    use crate::documents::test_support::crafted_png;
    use crate::model::{AccountId, AccountType, NewAccount, NewDocument, NewTransaction};
    use crate::money::{Currency, Money};
    use crate::repo::{accounts, documents, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    fn account(db: &Database) -> AccountId {
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
        account_id: AccountId,
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
                created_at: time::OffsetDateTime::now_utc(),
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

    fn document(db: &Database, name: &str, transaction_id: Option<TransactionId>) -> DocumentItem {
        let created = documents::create(
            db,
            NewDocument {
                file_name: name.to_owned(),
                stored_path: format!("documents/{name}"),
                mime_type: "application/pdf".to_owned(),
                transaction_id,
            },
        )
        .unwrap();
        documents::get_item(db, created.id).unwrap().unwrap()
    }

    struct CountingEngine {
        calls: AtomicUsize,
    }

    impl OcrEngine for CountingEngine {
        fn recognize(&self, _image: &Path) -> Result<Recognition, OcrError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Ok(Recognition {
                text: String::new(),
                confidence: 0.0,
            })
        }
    }

    #[test]
    fn the_whole_batch_uploads_with_progress_and_ends_when_done() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let first = temp.path().join("first.png");
        let second = temp.path().join("second.png");
        crafted_png(&first, 2, 2);
        crafted_png(&second, 2, 2);
        let engine = Arc::new(CountingEngine {
            calls: AtomicUsize::new(0),
        });
        let mut state = DocumentsState::new(Language::En, true);
        let mut progress = Vec::new();

        state.upload(
            &[first, second],
            None,
            &db,
            &paths,
            Some(engine.clone() as Arc<dyn OcrEngine>),
            |done, total| progress.push((done, total)),
        );

        assert_eq!(engine.calls.load(Ordering::SeqCst), 2);
        assert_eq!(progress, vec![(1, 2), (2, 2)]);
        assert!(!state.is_uploading, "the busy section ends with the batch");
        assert_eq!(state.upload_done, 2);
        assert_eq!(state.upload_total, 2);
        assert_eq!(state.documents.len(), 2);
        assert_eq!(
            state.message.as_deref(),
            Some("Uploaded 2 files, text extracted from 0")
        );
        assert_eq!(state.error, None);
    }

    #[test]
    fn a_failed_file_is_reported_and_the_rest_still_uploads() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let png = temp.path().join("bon.png");
        let text = temp.path().join("notes.txt");
        crafted_png(&png, 2, 2);
        std::fs::write(&text, "not a document").unwrap();
        let mut state = DocumentsState::new(Language::En, false);

        state.upload(&[png, text], None, &db, &paths, None, |_, _| {});

        assert_eq!(state.documents.len(), 1);
        assert_eq!(state.documents[0].file_name, "bon.png");
        assert!(
            state.error.as_deref().is_some_and(|error| error.contains("notes.txt")),
            "{:?}",
            state.error
        );
        assert_eq!(state.message.as_deref(), Some("Uploaded 1 file, text extracted from 0"));
    }

    #[test]
    fn refresh_counts_every_filter_for_the_current_search() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let account = account(&db);
        let linked_transaction = transaction(&db, account, date!(2026 - 03 - 13), -1000, "Shop", "shop");
        document(&db, "alpha.pdf", None);
        document(&db, "beta.pdf", None);
        document(&db, "linked.pdf", Some(linked_transaction));
        let mut state = DocumentsState::new(Language::En, false);

        state.load(&db, &paths);

        assert_eq!(
            state.counts,
            DocumentCounts {
                all: 3,
                unlinked: 2,
                linked: 1,
            }
        );
        assert_eq!(state.documents.len(), 3, "the ALL list still shows every document");

        state.search("beta", &db, &paths);

        assert_eq!(
            state.counts,
            DocumentCounts {
                all: 1,
                unlinked: 1,
                linked: 0,
            }
        );
        assert_eq!(
            state
                .documents
                .iter()
                .map(|item| item.file_name.as_str())
                .collect::<Vec<_>>(),
            vec!["beta.pdf"]
        );
    }

    #[test]
    fn changing_tabs_lists_the_latest_tab_and_counts_every_filter() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let account = account(&db);
        let linked_transaction = transaction(&db, account, date!(2026 - 03 - 13), -1000, "Shop", "shop");
        document(&db, "unlinked.pdf", None);
        document(&db, "linked.pdf", Some(linked_transaction));
        let mut state = DocumentsState::new(Language::En, false);

        state.load(&db, &paths);
        state.select_tab(DocumentTab::Unlinked, &db, &paths);
        assert_eq!(
            state
                .documents
                .iter()
                .map(|item| item.file_name.as_str())
                .collect::<Vec<_>>(),
            vec!["unlinked.pdf"]
        );

        state.select_tab(DocumentTab::Linked, &db, &paths);

        assert_eq!(state.tab, DocumentTab::Linked);
        assert_eq!(
            state
                .documents
                .iter()
                .map(|item| item.file_name.as_str())
                .collect::<Vec<_>>(),
            vec!["linked.pdf"]
        );
        assert_eq!(
            state.counts,
            DocumentCounts {
                all: 2,
                unlinked: 1,
                linked: 1,
            }
        );
    }

    #[test]
    fn receipts_attached_from_the_detail_panel_are_linked_to_the_transaction() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let account = account(&db);
        let rewe = transaction(&db, account, date!(2026 - 03 - 13), -1000, "REWE", "rewe");
        let png = temp.path().join("bon.png");
        crafted_png(&png, 5, 5);
        let mut state = DocumentsState::new(Language::En, false);
        state.load(&db, &paths);

        assert!(state.documents.is_empty());
        state.upload(&[png], Some(rewe), &db, &paths, None, |_, _| {});

        assert_eq!(
            state
                .documents
                .iter()
                .map(|item| item.file_name.as_str())
                .collect::<Vec<_>>(),
            vec!["bon.png"]
        );
        assert_eq!(state.documents[0].transaction_id, Some(rewe));
        let refreshed = transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == rewe)
            .unwrap();
        assert_eq!(refreshed.document_count, 1);
    }

    #[test]
    fn link_unlink_and_delete_update_the_list_and_messages() {
        let temp = tempfile::tempdir().unwrap();
        let (_db_temp, db) = database();
        let paths = paths(&temp);
        let account = account(&db);
        let linked_transaction = transaction(&db, account, date!(2026 - 03 - 13), -1000, "Shop", "shop");
        let png = temp.path().join("receipt.png");
        crafted_png(&png, 2, 2);
        let mut state = DocumentsState::new(Language::En, false);
        state.upload(&[png], None, &db, &paths, None, |_, _| {});
        let document = state.documents[0].clone();
        state.select_document(Some(document.clone()), &db, &paths);

        state.link_to_transaction(document.id, linked_transaction, &db, &paths);

        assert_eq!(state.message.as_deref(), Some("Document linked"));
        assert_eq!(state.documents[0].transaction_id, Some(linked_transaction));

        state.unlink(document.id, &db, &paths);
        assert_eq!(state.documents[0].transaction_id, None);

        state.delete(document.id, &db, &paths);
        assert_eq!(state.message.as_deref(), Some("Document deleted"));
        assert!(state.documents.is_empty());
        assert_eq!(state.selected_document, None);
        assert_eq!(state.selected_ocr_text, None);
        assert!(state.suggestions.is_empty());
    }

    #[test]
    fn clear_messages_clears_the_error_and_message() {
        let mut state = DocumentsState::new(Language::En, false);
        state.error = Some("error".to_owned());
        state.message = Some("message".to_owned());

        state.clear_messages();

        assert_eq!(state.error, None);
        assert_eq!(state.message, None);
    }
}
