//! The Documents page controller.
//!
//! It owns the UI-free [`DocumentsState`] plus the Slint-only pieces (the preview image and the picker
//! filter), maps the state to Slint models, and runs every database and OCR action on a background
//! thread. Results come back through `slint::invoke_from_event_loop`, as in the shell.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;

use finglass_core::app::documents::{DocumentTab, DocumentsState};
use finglass_core::db::Database;
use finglass_core::documents::{MatchSuggestion, OcrEngine, SUPPORTED_EXTENSIONS, TesseractCli, limits};
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{DocumentItem, TransactionId, TransactionItem};
use finglass_core::money::Money;
use finglass_core::paths::AppPaths;
use slint::{Image, ModelRc, VecModel, Weak};
use time::OffsetDateTime;

use crate::{DocumentLinkRow, DocumentRow, MainWindow};

/// The preview shown above the detail panel's OCR text.
#[derive(Clone)]
struct Preview {
    /// `"image"`, `"pdf"` or `"none"`.
    kind: &'static str,
    /// The image to decode in the event loop, after the size limit passed on the worker thread.
    path: Option<PathBuf>,
}

impl Default for Preview {
    fn default() -> Self {
        Self {
            kind: "none",
            path: None,
        }
    }
}

/// Orders background actions so a fast search cannot overtake an earlier one.
#[derive(Default)]
struct ActionOrder {
    next: u64,
    serving: u64,
}

/// Owns the Documents page state and keeps the Slint window in sync with it.
pub struct DocumentsController {
    ui: Weak<MainWindow>,
    paths: Arc<AppPaths>,
    profile: Mutex<String>,
    db: Mutex<Option<Arc<Database>>>,
    state: Mutex<DocumentsState>,
    engine: Mutex<Option<Arc<dyn OcrEngine>>>,
    preview: Mutex<Preview>,
    candidate_query: Mutex<String>,
    order: Mutex<ActionOrder>,
    order_ready: Condvar,
    started: AtomicBool,
    jobs: crate::jobs::JobTracker,
}

impl DocumentsController {
    /// Builds a controller for one main window and data folder.
    #[must_use]
    pub fn new(
        ui: Weak<MainWindow>,
        paths: Arc<AppPaths>,
        profile: impl Into<String>,
        jobs: crate::jobs::JobTracker,
    ) -> Self {
        Self {
            ui,
            paths,
            profile: Mutex::new(profile.into()),
            db: Mutex::new(None),
            state: Mutex::new(DocumentsState::new(Language::En, false)),
            engine: Mutex::new(None),
            preview: Mutex::new(Preview {
                kind: "none",
                path: None,
            }),
            candidate_query: Mutex::new(String::new()),
            order: Mutex::new(ActionOrder::default()),
            order_ready: Condvar::new(),
            started: AtomicBool::new(false),
            jobs,
        }
    }

    /// Re-points the controller at another profile's folder without rebuilding it.
    pub fn set_profile(&self, profile: impl Into<String>) {
        *self.profile.lock().expect("documents profile poisoned") = profile.into();
    }

    /// The profile slug key this controller currently uses, for the shell's switch test.
    pub(crate) fn profile_key(&self) -> String {
        self.profile.lock().expect("documents profile poisoned").clone()
    }

    /// Installs the page's callbacks on the main window.
    pub fn wire(self: &Arc<Self>, main: &MainWindow) {
        {
            let this = Arc::clone(self);
            main.on_documents_set_tab(move |value| {
                let tab = document_tab(value.as_str());
                this.spawn_action(move |state, db, paths| state.select_tab(tab, db, paths));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_search(move |value| {
                let query = value.to_string();
                this.spawn_action(move |state, db, paths| state.search(&query, db, paths));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_select(move |id| {
                let id = i64::from(id);
                this.spawn_action(move |state, db, paths| {
                    let document = state.documents.iter().find(|document| document.id == id).cloned();
                    state.select_document(document, db, paths);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_close_detail(move || {
                this.spawn_action(|state, db, paths| state.select_document(None, db, paths));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_link(move |transaction_id| {
                let transaction_id = i64::from(transaction_id);
                this.spawn_action(move |state, db, paths| {
                    let Some(document) = state.selected_document.clone() else {
                        return;
                    };
                    state.link_to_transaction(document.id, TransactionId(transaction_id), db, paths);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_unlink(move || {
                this.spawn_action(|state, db, paths| {
                    let Some(document) = state.selected_document.clone() else {
                        return;
                    };
                    state.unlink(document.id, db, paths);
                    if let Some(selected) = state.selected_document.clone() {
                        state.select_document(Some(selected), db, paths);
                    }
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_reprocess(move || {
                let engine = this.engine.lock().expect("documents engine poisoned").clone();
                this.spawn_action(move |state, db, paths| {
                    let Some(document) = state.selected_document.clone() else {
                        return;
                    };
                    state.reprocess_ocr(document.id, db, paths, engine);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_confirm_delete(move || {
                this.spawn_action(|state, db, paths| {
                    let Some(document) = state.selected_document.clone() else {
                        return;
                    };
                    state.delete(document.id, db, paths);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_upload(move || this.upload());
        }

        {
            let this = Arc::clone(self);
            main.on_documents_open(move || this.open_selected());
        }

        {
            let this = Arc::clone(self);
            main.on_documents_candidate_query_changed(move |value| {
                *this.candidate_query.lock().expect("documents picker poisoned") = value.to_string();
                // While work holds the state (an upload with OCR can take long), the UI thread never waits: the
                // worker's own publish picks up the new query.
                let Ok(state) = this.state.try_lock() else {
                    return;
                };
                let snapshot = state.clone();
                drop(state);
                this.publish(snapshot);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_documents_dismiss(move || {
                let snapshot = {
                    let Ok(mut state) = this.state.try_lock() else {
                        return;
                    };
                    state.clear_messages();
                    state.clone()
                };
                this.publish(snapshot);
            });
        }
    }

    /// Makes the controller use `db` and starts from a clean page for a profile.
    pub fn attach(self: &Arc<Self>, db: Arc<Database>, language: Language) {
        *self.db.lock().expect("documents database poisoned") = Some(db);
        let is_ocr_available = self
            .engine
            .lock()
            .expect("documents engine poisoned")
            .as_ref()
            .is_some();
        let snapshot = {
            let mut state = self.state.lock().expect("documents state poisoned");
            *state = DocumentsState::new(language, is_ocr_available);
            state.set_profile(self.profile_key());
            state.clone()
        };
        *self.preview.lock().expect("documents preview poisoned") = Preview::default();
        self.publish(snapshot);

        if self.started.load(Ordering::SeqCst) {
            self.reload();
        }
    }

    /// Clears the page, for when the profile is locked.
    pub fn reset(&self) {
        *self.db.lock().expect("documents database poisoned") = None;
        let language = self.state.lock().expect("documents state poisoned").language;
        let is_ocr_available = self
            .engine
            .lock()
            .expect("documents engine poisoned")
            .as_ref()
            .is_some();
        let snapshot = {
            let mut state = self.state.lock().expect("documents state poisoned");
            *state = DocumentsState::new(language, is_ocr_available);
            state.set_profile(self.profile_key());
            state.clone()
        };
        *self.preview.lock().expect("documents preview poisoned") = Preview::default();
        *self.candidate_query.lock().expect("documents picker poisoned") = String::new();
        self.publish(snapshot);
    }

    /// Applies a language change to the page's own texts.
    pub fn set_language(self: &Arc<Self>, language: Language) {
        if let Ok(mut state) = self.state.try_lock() {
            state.language = language;
            let snapshot = state.clone();
            drop(state);
            self.publish(snapshot);
            return;
        }
        // Work holds the state: apply the language once it is done, off the UI thread.
        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            let snapshot = {
                let mut state = this.state.lock().expect("documents state poisoned");
                state.language = language;
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    /// Loads the page the first time it is shown, and detects Tesseract once on a background thread.
    pub fn open_page(self: &Arc<Self>) {
        if self.started.swap(true, Ordering::SeqCst) {
            self.reload();
            return;
        }

        // Load the list right away; the Tesseract probe runs beside it and updated the hint when done.
        self.reload();

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            let engine = TesseractCli::create_or_none().map(|engine| Arc::new(engine) as Arc<dyn OcrEngine>);
            let is_ocr_available = engine.is_some();
            if this.jobs.is_current(generation) {
                *this.engine.lock().expect("documents engine poisoned") = engine;

                {
                    let mut state = this.state.lock().expect("documents state poisoned");
                    state.is_ocr_available = is_ocr_available;
                }
                this.reload();
            }
            this.jobs.end();
        });
    }

    fn reload(self: &Arc<Self>) {
        self.spawn_action(|state, db, paths| state.load(db, paths));
    }

    /// Runs a synchronous [`DocumentsState`] action on a background thread and publishes the result.
    fn spawn_action<F>(self: &Arc<Self>, action: F)
    where
        F: FnOnce(&mut DocumentsState, &Database, &AppPaths) + Send + 'static,
    {
        let Some(db) = self.database() else {
            return;
        };
        let ticket = self.ticket();
        let profile = self.profile_key();
        let generation = self.jobs.begin();
        let this = Arc::clone(self);
        thread::spawn(move || {
            this.wait_ticket(ticket);

            if !this.jobs.is_current(generation) {
                this.complete_ticket();
                this.jobs.end();
                return;
            }

            let snapshot = {
                let mut state = this.state.lock().expect("documents state poisoned");
                action(&mut state, &db, &this.paths);
                state.clone()
            };
            *this.preview.lock().expect("documents preview poisoned") =
                preview_for(snapshot.selected_document.as_ref(), &this.paths, &profile);

            this.complete_ticket();
            this.publish_job(snapshot, generation);
        });
    }

    fn ticket(&self) -> u64 {
        let mut order = self.order.lock().expect("documents order poisoned");
        let ticket = order.next;
        order.next += 1;
        ticket
    }

    fn wait_ticket(&self, ticket: u64) {
        let mut order = self.order.lock().expect("documents order poisoned");
        while order.serving != ticket {
            order = self.order_ready.wait(order).expect("documents order poisoned");
        }
    }

    fn complete_ticket(&self) {
        let mut order = self.order.lock().expect("documents order poisoned");
        order.serving += 1;
        self.order_ready.notify_all();
    }

    fn upload(self: &Arc<Self>) {
        let Some(_db) = self.database() else {
            return;
        };
        // Ignored while work runs, instead of blocking the UI thread on the state.
        let Ok(language) = self.state.try_lock().map(|state| state.language) else {
            return;
        };
        let Some(files) = rfd::FileDialog::new()
            .add_filter(i18n::text(language, Key::UploadDocuments), SUPPORTED_EXTENSIONS)
            .pick_files()
        else {
            return;
        };
        if files.is_empty() {
            return;
        }

        let Some(db) = self.database() else {
            return;
        };
        let engine = self.engine.lock().expect("documents engine poisoned").clone();
        let ticket = self.ticket();
        let profile = self.profile_key();
        let generation = self.jobs.begin();
        let this = Arc::clone(self);
        thread::spawn(move || {
            this.wait_ticket(ticket);
            if !this.jobs.is_current(generation) {
                this.complete_ticket();
                this.jobs.end();
                return;
            }
            let progress_ui = this.ui.clone();
            let jobs = this.jobs.clone();
            let snapshot = {
                let mut state = this.state.lock().expect("documents state poisoned");
                state.upload(&files, None, &db, &this.paths, engine, |done, total| {
                    if !jobs.is_current(generation) {
                        return;
                    }
                    let text = i18n::format(
                        language,
                        Key::UploadProgress,
                        &[Arg::Int(done as i64), Arg::Int(total as i64)],
                    );
                    let ui = progress_ui.clone();
                    let _ = slint::invoke_from_event_loop(move || {
                        if let Some(ui) = ui.upgrade() {
                            ui.set_documents_uploading(true);
                            ui.set_documents_progress_text(text.into());
                        }
                    });
                });
                state.clone()
            };
            *this.preview.lock().expect("documents preview poisoned") =
                preview_for(snapshot.selected_document.as_ref(), &this.paths, &profile);
            this.complete_ticket();
            this.publish_job(snapshot, generation);
        });
    }

    fn open_selected(&self) {
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let Some(document) = state.selected_document.as_ref() else {
            return;
        };
        let path = document_path(&self.paths, &self.profile_key(), &document.stored_path);
        drop(state);

        if path.is_file() {
            let jobs = self.jobs.clone();
            jobs.begin();
            thread::spawn(move || {
                let _ = open::that_detached(path);
                jobs.end();
            });
        }
    }

    fn database(&self) -> Option<Arc<Database>> {
        self.db.lock().expect("documents database poisoned").clone()
    }

    fn publish(&self, state: DocumentsState) {
        let ui = self.ui.clone();
        let preview = self.preview.lock().expect("documents preview poisoned").clone();
        let query = self.candidate_query.lock().expect("documents picker poisoned").clone();

        let _ = slint::invoke_from_event_loop(move || {
            if let Some(ui) = ui.upgrade() {
                apply(&ui, &state, &preview, &query);
            }
        });
    }

    /// Publishes a worker's result unless the profile has been switched in the meantime.
    fn publish_job(&self, state: DocumentsState, generation: u64) {
        if !self.jobs.is_current(generation) {
            self.jobs.end();
            return;
        }
        let ui = self.ui.clone();
        let preview = self.preview.lock().expect("documents preview poisoned").clone();
        let query = self.candidate_query.lock().expect("documents picker poisoned").clone();
        let jobs = self.jobs.clone();

        let _ = slint::invoke_from_event_loop(move || {
            jobs.end();
            if !jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = ui.upgrade() {
                apply(&ui, &state, &preview, &query);
            }
        });
    }
}

/// Writes one state snapshot to the Slint window on the event loop.
fn apply(ui: &MainWindow, state: &DocumentsState, preview: &Preview, query: &str) {
    let language = state.language;
    let (all, unlinked, linked) = tab_labels(state);
    let selected = state.selected_document.as_ref();
    let (preview_kind, preview_image) = match preview.kind {
        "image" => preview
            .path
            .as_ref()
            .and_then(|path| Image::load_from_path(path).ok())
            .map_or(("none", Image::default()), |image| ("image", image)),
        "pdf" => ("pdf", Image::default()),
        _ => ("none", Image::default()),
    };

    ui.set_document_rows(ModelRc::new(VecModel::from(document_rows(state))));
    ui.set_documents_tab(tab_code(state.tab).into());
    ui.set_documents_tab_all_label(all.into());
    ui.set_documents_tab_unlinked_label(unlinked.into());
    ui.set_documents_tab_linked_label(linked.into());
    ui.set_documents_search_query(state.search_query.clone().into());
    ui.set_documents_ocr_available(state.is_ocr_available);
    ui.set_documents_ocr_hint(ocr_hint(language, state.is_ocr_available).into());
    ui.set_documents_uploading(state.is_uploading);
    ui.set_documents_progress_text(progress_text(state).into());
    ui.set_documents_loading(state.is_loading);
    ui.set_documents_error(state.error.clone().unwrap_or_default().into());
    ui.set_documents_message(state.message.clone().unwrap_or_default().into());
    ui.set_documents_has_selection(selected.is_some());
    ui.set_documents_selected_name(
        selected
            .map(|document| document.file_name.clone())
            .unwrap_or_default()
            .into(),
    );
    ui.set_documents_selected_linked(selected.is_some_and(|document| document.transaction_id.is_some()));
    ui.set_documents_selected_link_label(
        selected
            .and_then(|document| document.transaction_label.clone())
            .unwrap_or_default()
            .into(),
    );
    ui.set_documents_selected_ocr_heading(ocr_heading(state).into());
    ui.set_documents_selected_ocr_text(state.selected_ocr_text.clone().unwrap_or_default().into());
    ui.set_documents_preview_kind(preview_kind.into());
    ui.set_documents_preview(preview_image);
    ui.set_documents_suggestions(ModelRc::new(VecModel::from(suggestion_rows(
        &state.suggestions,
        language,
    ))));
    ui.set_documents_candidates(ModelRc::new(VecModel::from(candidate_rows(
        &state.link_candidates,
        query,
        language,
    ))));
    ui.set_documents_candidate_query(query.to_owned().into());
    ui.set_documents_delete_text(delete_text(state).into());
}

/// Maps a Slint tab name to the state's tab.
fn document_tab(value: &str) -> DocumentTab {
    match value {
        "unlinked" => DocumentTab::Unlinked,
        "linked" => DocumentTab::Linked,
        _ => DocumentTab::All,
    }
}

/// Maps a state tab to the Slint tab name.
fn tab_code(tab: DocumentTab) -> &'static str {
    match tab {
        DocumentTab::All => "all",
        DocumentTab::Unlinked => "unlinked",
        DocumentTab::Linked => "linked",
    }
}

/// The three tab labels, each with the count for the current search.
fn tab_labels(state: &DocumentsState) -> (String, String, String) {
    let label = |tab: DocumentTab| {
        i18n::format(
            state.language,
            Key::TabWithCount,
            &[
                i18n::text(state.language, tab.label_key()).into(),
                Arg::Int(state.counts.get(tab) as i64),
            ],
        )
    };
    (
        label(DocumentTab::All),
        label(DocumentTab::Unlinked),
        label(DocumentTab::Linked),
    )
}

/// One list row per document.
fn document_rows(state: &DocumentsState) -> Vec<DocumentRow> {
    state
        .documents
        .iter()
        .map(|document| DocumentRow {
            id: i32::try_from(document.id).unwrap_or(i32::MAX),
            name: document.file_name.clone().into(),
            meta: format!(
                "{} · {}",
                date_time_text(document.uploaded_at),
                document
                    .transaction_label
                    .clone()
                    .unwrap_or_else(|| i18n::text(state.language, Key::NotLinked).to_owned())
            )
            .into(),
            is_linked: document.transaction_id.is_some(),
            has_ocr: document.has_ocr,
            is_pdf: document.is_pdf(),
            selected: state
                .selected_document
                .as_ref()
                .is_some_and(|selected| selected.id == document.id),
        })
        .collect()
}

/// The detail panel's OCR heading, with the confidence when the state knows it.
fn ocr_heading(state: &DocumentsState) -> String {
    let Some(document) = state.selected_document.as_ref() else {
        return String::new();
    };
    match document.ocr_confidence {
        Some(confidence) => i18n::format(
            state.language,
            Key::ExtractedTextConfidence,
            &[Arg::Int(confidence.round() as i64)],
        ),
        None => i18n::text(state.language, Key::ExtractedText).to_owned(),
    }
}

/// The delete confirmation's text.
fn delete_text(state: &DocumentsState) -> String {
    state.selected_document.as_ref().map_or_else(String::new, |document| {
        i18n::format(
            state.language,
            Key::DeleteDocumentText,
            &[Arg::Str(document.file_name.clone())],
        )
    })
}

/// The upload progress line.
fn progress_text(state: &DocumentsState) -> String {
    if !state.is_uploading || state.upload_total == 0 {
        return String::new();
    }
    i18n::format(
        state.language,
        Key::UploadProgress,
        &[Arg::Int(state.upload_done as i64), Arg::Int(state.upload_total as i64)],
    )
}

/// The OCR-unavailable hint, with the Settings path FinDesk names.
fn ocr_hint(language: Language, is_ocr_available: bool) -> String {
    if is_ocr_available {
        return String::new();
    }
    let settings = format!(
        "{} → {}",
        i18n::text(language, Key::NavSettings),
        i18n::text(language, Key::DataAndOcr)
    );
    i18n::format(language, Key::OcrNotInstalled, &[Arg::Str(settings)])
}

/// A whole-percent text in the language's own convention, as FinDesk's `Formats.percent` does.
fn percent_text(language: Language, score: f64) -> String {
    let separator = if language == Language::En { "" } else { "\u{a0}" };
    let value = if score <= 0.0 {
        "0".to_owned()
    } else if score < 0.005 {
        "<1".to_owned()
    } else {
        (score * 100.0).round().clamp(0.0, 100.0).to_string()
    };
    format!("{value}{separator}%")
}

/// The badge text for a match suggestion.
fn match_badge(language: Language, score: f64) -> String {
    i18n::format(language, Key::MatchPercent, &[Arg::Str(percent_text(language, score))])
}

/// Rows for the suggestion list, with the match score badge.
fn suggestion_rows(suggestions: &[MatchSuggestion], language: Language) -> Vec<DocumentLinkRow> {
    suggestions
        .iter()
        .map(|suggestion| DocumentLinkRow {
            id: i32::try_from(suggestion.transaction.id.0).unwrap_or(i32::MAX),
            title: suggestion.transaction.raw_description().into(),
            meta: format!(
                "{} · {}",
                date_text(suggestion.transaction.date),
                money_text(suggestion.transaction.amount, language)
            )
            .into(),
            badge: match_badge(language, suggestion.score).into(),
        })
        .collect()
}

/// The link candidates for the detail panel's picker, filtered by the typed query.
fn candidate_rows(candidates: &[TransactionItem], query: &str, language: Language) -> Vec<DocumentLinkRow> {
    let needle = query.trim().to_lowercase();
    if needle.is_empty() {
        return Vec::new();
    }
    candidates
        .iter()
        .filter(|transaction| {
            transaction.raw_description().to_lowercase().contains(&needle)
                || transaction
                    .purpose
                    .as_deref()
                    .is_some_and(|purpose| purpose.to_lowercase().contains(&needle))
                || money_text(transaction.amount, language)
                    .to_lowercase()
                    .contains(&needle)
        })
        .take(8)
        .map(|transaction| DocumentLinkRow {
            id: i32::try_from(transaction.id.0).unwrap_or(i32::MAX),
            title: transaction.raw_description().into(),
            meta: format!(
                "{} · {}",
                date_text(transaction.date),
                money_text(transaction.amount, language)
            )
            .into(),
            badge: String::new().into(),
        })
        .collect()
}

/// `dd.MM.yyyy HH:mm`, the format FinDesk's Documents rows use.
fn date_time_text(value: OffsetDateTime) -> String {
    format!(
        "{:02}.{:02}.{:04} {:02}:{:02}",
        value.day(),
        value.month() as u8,
        value.year(),
        value.hour(),
        value.minute()
    )
}

/// `dd.MM.yyyy`, the format FinDesk's document rows use for transaction dates.
fn date_text(value: time::Date) -> String {
    format!("{:02}.{:02}.{:04}", value.day(), value.month() as u8, value.year())
}

/// An amount in the app's money format, as the Documents page shows it.
fn money_text(money: Money, language: Language) -> String {
    format::money(money, language, false)
}

/// Builds the preview for a selected document. Images are decoded only after the size limit passed.
fn preview_for(document: Option<&DocumentItem>, paths: &AppPaths, profile: &str) -> Preview {
    let Some(document) = document else {
        return Preview::default();
    };
    if document.is_pdf() {
        return Preview {
            kind: "pdf",
            path: None,
        };
    }
    if !document.is_image() {
        return Preview::default();
    }

    let path = document_path(paths, profile, &document.stored_path);
    if limits::check_image(&path).is_err() {
        return Preview::default();
    }
    Preview {
        kind: "image",
        path: Some(path),
    }
}

/// Resolves a stored document path, which may be relative to the data folder.
fn document_path(paths: &AppPaths, profile: &str, stored_path: &str) -> PathBuf {
    let path = Path::new(stored_path);
    if path.is_absolute() {
        return path.to_owned();
    }
    paths
        .profile_dir(profile)
        .map_or_else(|_| paths.root().join(path), |dir| dir.join(path))
}

#[cfg(test)]
mod tests {
    use finglass_core::app::documents::DocumentCounts;
    use finglass_core::money::Currency;
    use slint::SharedString;
    use time::{Date, Month};

    use super::*;

    fn item(id: i64, name: &str, pdf: bool, linked: Option<TransactionId>, has_ocr: bool) -> DocumentItem {
        DocumentItem {
            id,
            file_name: name.to_owned(),
            stored_path: format!("documents/{name}"),
            mime_type: if pdf {
                "application/pdf".to_owned()
            } else {
                "image/png".to_owned()
            },
            uploaded_at: OffsetDateTime::from_unix_timestamp(1_760_000_000).unwrap(),
            transaction_id: linked,
            transaction_label: linked.map(|_| "Shop (2026-03-13)".to_owned()),
            has_ocr,
            ocr_confidence: has_ocr.then_some(88.0),
        }
    }

    fn state() -> DocumentsState {
        let mut state = DocumentsState::new(Language::En, true);
        state.documents = vec![
            item(1, "bon.png", false, None, false),
            item(2, "rechnung.pdf", true, Some(TransactionId(7)), true),
            item(3, "kassenbon.jpg", false, None, true),
        ];
        state.counts = DocumentCounts {
            all: 3,
            unlinked: 2,
            linked: 1,
        };
        state.selected_document = Some(state.documents[2].clone());
        state.selected_ocr_text = Some("SUMME 3,78".to_owned());
        state
    }

    #[test]
    fn rows_show_the_name_date_link_state_and_ocr_badge() {
        let rows = document_rows(&state());

        assert_eq!(rows.len(), 3);
        assert_eq!(rows[0].name, SharedString::from("bon.png"));
        assert!(rows[0].meta.contains("not linked"), "{}", rows[0].meta);
        assert!(!rows[0].is_pdf);
        assert!(!rows[0].has_ocr);
        assert_eq!(rows[1].name, SharedString::from("rechnung.pdf"));
        assert!(rows[1].meta.contains("Shop (2026-03-13)"), "{}", rows[1].meta);
        assert!(rows[1].is_pdf);
        assert!(rows[1].has_ocr);
        assert!(rows[1].is_linked);
        assert!(!rows[0].selected);
        assert!(rows[2].selected, "the state's selected document is marked");
    }

    #[test]
    fn tabs_carry_the_counts_of_every_filter() {
        let state = state();

        assert_eq!(
            tab_labels(&state),
            (
                "All documents (3)".to_owned(),
                "Unlinked (2)".to_owned(),
                "Linked to transaction (1)".to_owned()
            )
        );
        assert_eq!(tab_code(state.tab), "all");
        assert_eq!(document_tab("unlinked"), DocumentTab::Unlinked);
        assert_eq!(document_tab("linked"), DocumentTab::Linked);
        assert_eq!(document_tab("anything else"), DocumentTab::All);
    }

    #[test]
    fn match_badges_use_the_language_percent_convention() {
        assert_eq!(match_badge(Language::En, 0.8), "match 80%");
        assert_eq!(match_badge(Language::De, 0.8), "Treffer 80\u{a0}%");
        assert_eq!(match_badge(Language::Fr, 0.0), "correspondance 0\u{a0}%");
    }

    #[test]
    fn ocr_heading_and_delete_text_follow_the_selection() {
        let state = state();

        assert_eq!(ocr_heading(&state), "Extracted text (confidence 88%)");
        assert_eq!(
            delete_text(&state),
            "kassenbon.jpg and its stored copy will be deleted."
        );

        let mut empty = DocumentsState::new(Language::En, false);
        empty.selected_document = Some(item(4, "scan.pdf", true, None, false));
        assert_eq!(ocr_heading(&empty), "Extracted text");
        assert_eq!(empty.documents.len(), 0);
    }

    #[test]
    fn date_and_money_text_match_the_page_format() {
        let date = Date::from_calendar_date(2026, Month::March, 13).unwrap();
        assert_eq!(date_text(date), "13.03.2026");
        assert_eq!(
            money_text(
                Money {
                    minor: -1234,
                    currency: Currency::EUR,
                },
                Language::En
            ),
            "-€12.34"
        );
        assert_eq!(
            money_text(
                Money {
                    minor: 500,
                    currency: Currency::new("USD").unwrap(),
                },
                Language::En
            ),
            "$5.00"
        );
        assert_eq!(
            money_text(
                Money {
                    minor: -1234,
                    currency: Currency::EUR,
                },
                Language::De
            ),
            "-12,34\u{a0}€"
        );
    }

    #[test]
    fn ocr_hint_names_settings_and_data_and_ocr() {
        assert_eq!(ocr_hint(Language::En, true), "");
        let hint = ocr_hint(Language::En, false);
        assert!(hint.contains("Tesseract"), "{hint}");
        assert!(hint.contains("Settings → Data & OCR"), "{hint}");
    }
}
