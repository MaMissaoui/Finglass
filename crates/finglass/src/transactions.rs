//! The Transactions page controller.
//!
//! It owns the UI-free [`TransactionsState`] and maps it to the Slint models. Every database action runs on a
//! background thread and comes back through `slint::invoke_from_event_loop`; a [`Busy`] handle ignores a second
//! click while work is running. All behaviour lives in `finglass_core::app::transactions`; this module only
//! renders the state and calls its methods.

#![allow(clippy::too_many_arguments)]

use std::collections::BTreeSet;
use std::rc::Rc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use finglass_core::account_grouping::{self, AccountItem as GroupingAccount, BalanceTone, GroupKey, LOAN_TYPE};
use finglass_core::account_ids;
use finglass_core::app::account_panel::{self, AccountSelection};
use finglass_core::app::accounts::{AccountItem, Busy};
use finglass_core::app::transactions::{
    CategoryFilter, ImportPreview, ManualTransaction, Period, ReapplyKey, ReapplyPreview, SEARCH_DEBOUNCE_MS,
    StartArgs, TransactionsState, TransactionsView,
};
use finglass_core::bank_text;
use finglass_core::db::Database;
use finglass_core::documents::{DocumentService, OcrService, SUPPORTED_EXTENSIONS};
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::import::{CsvLayout, CsvMapping};
use finglass_core::model::{
    AccountGroupId, AccountId, CategoryId, Direction, PendingBookingItem, TaxSource, TransactionId, TransactionItem,
};
use finglass_core::money::{self, Currencies, Currency, Money};
use finglass_core::paths::AppPaths;
use finglass_core::report::format::month_name;
use finglass_core::rules::payment_providers::PaymentProviders;
use slint::{Color, ModelRc, SharedString, Timer, TimerMode, VecModel, Weak};
use time::{Date, OffsetDateTime};

use crate::{
    AccountAmount, AccountGroupChoice, AccountPanelRow, ImportCsvField, ImportFileRow, MainWindow, ReapplyGroupRow,
    TransactionRow,
};

/// The balance chart is drawn in this logical box and scaled by its viewbox.
const CHART_WIDTH: f32 = 700.0;
/// Height of the balance chart's drawing box.
const CHART_HEIGHT: f32 = 200.0;
/// Padding inside the balance chart.
const CHART_PADDING: f32 = 8.0;

/// The statement formats the import file dialog offers.
const STATEMENT_EXTENSIONS: &[&str] = &["csv", "sta", "mt940", "xml", "zip", "ofx", "qfx", "qif", "xlsx"];

/// Ends the busy section even when the worker panics.
struct BusyGuard(Busy);

impl Drop for BusyGuard {
    fn drop(&mut self) {
        self.0.end();
    }
}

/// The newest search query typed while a job was running, kept until the job has finished.
#[derive(Debug, Default, PartialEq, Eq)]
struct PendingSearch {
    query: Option<String>,
}

impl PendingSearch {
    /// Keeps `query`, replacing an older one so only the latest typed text survives.
    fn defer(&mut self, query: &str) {
        self.query = Some(query.to_owned());
    }

    /// Takes the query to run, if any.
    fn take(&mut self) -> Option<String> {
        self.query.take()
    }

    /// Puts a query back after a failed start without replacing a newer one.
    fn restore(&mut self, query: String) {
        if self.query.is_none() {
            self.query = Some(query);
        }
    }
}

/// Owns the Transactions page state and keeps the Slint window in sync with it.
pub struct TransactionsController {
    ui: Weak<MainWindow>,
    paths: Arc<AppPaths>,
    /// The open profile's slug key, for its `documents/` folder. Read at call time so a switch is visible.
    profile: Mutex<String>,
    db: Mutex<Option<Arc<Database>>>,
    /// The persistent view: its private undo record must survive individual actions.
    view: Mutex<Option<TransactionsView>>,
    busy: Busy,
    pending_search: Mutex<PendingSearch>,
    multi_delete_ids: Mutex<BTreeSet<TransactionId>>,
    jobs: crate::jobs::JobTracker,
    reapply_excluded: Mutex<BTreeSet<ReapplyKey>>,
}

impl TransactionsController {
    /// Builds a controller for one main window and data folder.
    #[must_use]
    pub(crate) fn new(
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
            view: Mutex::new(Some(TransactionsView::new(Language::En))),
            busy: Busy::default(),
            pending_search: Mutex::new(PendingSearch::default()),
            multi_delete_ids: Mutex::new(BTreeSet::new()),
            jobs,
            reapply_excluded: Mutex::new(BTreeSet::new()),
        }
    }

    /// Re-points the controller at another profile's folder without rebuilding it.
    pub fn set_profile(&self, profile: impl Into<String>) {
        *self.profile.lock().expect("transactions profile poisoned") = profile.into();
    }

    /// The profile slug key this controller currently uses, for the shell's switch test.
    pub(crate) fn profile_key(&self) -> String {
        self.profile.lock().expect("transactions profile poisoned").clone()
    }

    /// Installs the page's callbacks on the main window.
    pub fn wire(self: &Arc<Self>, main: &MainWindow) {
        {
            let this = Arc::clone(self);
            let timer = Rc::new(Timer::default());
            let pending = Arc::new(Mutex::new(String::new()));
            main.on_transactions_search(move |text| {
                *pending.lock().expect("transactions search poisoned") = text.to_string();
                let this = Arc::clone(&this);
                let pending = Arc::clone(&pending);
                timer.start(
                    TimerMode::SingleShot,
                    Duration::from_millis(SEARCH_DEBOUNCE_MS),
                    move || {
                        let query = pending.lock().expect("transactions search poisoned").clone();
                        this.defer_search(&query);
                    },
                );
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_period_changed(move |index| {
                if let Some(period) = period_at(index) {
                    this.spawn(move |view, db| {
                        let _ = view.update_period(db, period, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_range(move || {
                this.open_range();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_apply_range(move |from, to| {
                this.apply_range(&from, &to);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_close_range(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_range_open(false);
                    ui.set_transactions_range_error("".into());
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_category_changed(move |index| {
                if let Some(filter) = this.category_filter_at(index) {
                    this.spawn(move |view, db| {
                        let _ = view.update_category_filter(db, filter, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_direction_changed(move |index| {
                let direction = match index {
                    1 => Direction::Income,
                    2 => Direction::Expenses,
                    _ => Direction::All,
                };
                this.spawn(move |view, db| {
                    let _ = view.update_direction(db, direction, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_account_changed(move |index| {
                if let Some(selection) = this.selection_at(index) {
                    this.select_accounts(selection);
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_amount(move || {
                this.open_amount();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_apply_amount(move |minimum, maximum| {
                this.apply_amount(&minimum, &maximum);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_reset_amount(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_amount_open(false);
                    ui.set_transactions_amount_error("".into());
                }
                this.spawn(|view, db| {
                    let _ = view.update_amount_range(db, None, None, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_close_amount(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_amount_open(false);
                    ui.set_transactions_amount_error("".into());
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_toggle_chart(move || {
                this.spawn(|view, db| {
                    let _ = view.toggle_chart(db);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_auto_categorize(move || {
                this.spawn(|view, db| {
                    let _ = view.auto_categorize(db, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_import(move || {
                let language = this.language();
                this.spawn(move |view, db| {
                    let Some(files) = rfd::FileDialog::new()
                        .add_filter(i18n::text(language, Key::ImportBankStatement), STATEMENT_EXTENSIONS)
                        .pick_files()
                    else {
                        return;
                    };
                    if files.is_empty() {
                        return;
                    }
                    let _ = view.open_import(db, &files, None, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_import_account_changed(move |index| {
                let account = this.with_state(|state| {
                    state
                        .accounts
                        .get(usize::try_from(index).ok()?)
                        .map(|account| account.id)
                });
                if let Some(Some(account_id)) = account {
                    this.spawn(move |view, db| {
                        let _ = view.select_import_account(db, account_id, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_import_csv_changed(move |field, value| {
                let field = field.to_string();
                let value = value.to_string();
                let mapping = this.with_state(|state| {
                    let preview = state.import_preview.as_ref()?;
                    let csv = preview.entries.iter().find_map(|entry| entry.statement.csv.as_ref())?;
                    let options = import_csv_options(csv, state.language);
                    let index = options
                        .iter()
                        .position(|option| option.as_str() == value)
                        .map_or(0, |index| i32::try_from(index).unwrap_or(0));
                    Some(remap_mapping(&csv.mapping, &field, index))
                });
                if let Some(Some(mapping)) = mapping {
                    this.spawn(move |view, db| {
                        let _ = view.remap_import(db, mapping, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_confirm_import(move |update_balance| {
                this.spawn(move |view, db| {
                    let _ = view.confirm_import(db, update_balance, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_cancel_import(move || {
                this.update_view(TransactionsView::cancel_import);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_reapply_rules(move || {
                this.reapply_excluded
                    .lock()
                    .expect("reapply exclusions poisoned")
                    .clear();
                this.spawn(|view, db| {
                    let _ = view.preview_reapply_rules(db, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_toggle_reapply_group(move |from, to| {
                let key = reapply_key(from, to);
                {
                    let mut excluded = this.reapply_excluded.lock().expect("reapply exclusions poisoned");
                    if !excluded.remove(&key) {
                        excluded.insert(key);
                    }
                }
                this.republish();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_confirm_reapply_rules(move || {
                let excluded = std::mem::take(&mut *this.reapply_excluded.lock().expect("reapply exclusions poisoned"));
                this.spawn(move |view, db| {
                    let _ = view.confirm_reapply_rules(db, &excluded, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_cancel_reapply_rules(move || {
                this.reapply_excluded
                    .lock()
                    .expect("reapply exclusions poisoned")
                    .clear();
                this.update_view(TransactionsView::cancel_reapply_rules);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_undo_reapply(move || {
                this.reapply_excluded
                    .lock()
                    .expect("reapply exclusions poisoned")
                    .clear();
                this.spawn(|view, db| {
                    let _ = view.undo_reapply_rules(db, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_export_csv(move || {
                let language = this.language();
                let today = OffsetDateTime::now_utc().date();
                let file_name = format!(
                    "transactions-{:04}-{:02}-{:02}.csv",
                    today.year(),
                    u8::from(today.month()),
                    today.day()
                );
                this.spawn(move |view, db| {
                    let Some(path) = rfd::FileDialog::new()
                        .add_filter(i18n::text(language, Key::ExportTransactions), &["csv"])
                        .set_file_name(&file_name)
                        .save_file()
                    else {
                        return;
                    };
                    let _ = view.export_csv(db, &path);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_attach_receipt(move || {
                let Some(id) = this.selected_id() else {
                    return;
                };
                let language = this.language();
                let documents = documents_dir(&this.paths, &this.profile_key());
                this.spawn(move |view, db| {
                    let Some(files) = rfd::FileDialog::new()
                        .add_filter(i18n::text(language, Key::AttachReceipt), SUPPORTED_EXTENSIONS)
                        .pick_files()
                    else {
                        return;
                    };
                    if files.is_empty() {
                        return;
                    }
                    let service = DocumentService::new(db, OcrService::without_ocr(), documents);
                    let _ = view.attach_documents(db, &service, id, &files, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_create_rule(move || {
                this.open_rule_dialog();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_rule_close(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_rule_open(false);
                    ui.set_transactions_rule_error("".into());
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_rule_save(move || {
                let Some(ui) = this.ui.upgrade() else {
                    return;
                };
                let language = this.language();
                let pattern = ui.get_transactions_rule_pattern().to_string();
                if pattern.trim().is_empty() {
                    ui.set_transactions_rule_error(i18n::text(language, Key::ErrorPatternRequired).into());
                    return;
                }
                let Some(category_id) = this.rule_category_at(ui.get_transactions_rule_category_index()) else {
                    return;
                };
                ui.set_transactions_rule_open(false);
                ui.set_transactions_rule_error("".into());
                this.spawn(move |view, db| {
                    let _ = view.create_rule(db, &pattern, category_id, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_back(move || {
                this.update_search_query("");
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_detail(move |id| {
                this.open_detail(TransactionId(i64::from(id)));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_toggle_select(move |id| {
                this.update_view(move |view| view.toggle_selection(TransactionId(i64::from(id))));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_select_range(move |id| {
                this.update_view(move |view| view.select_range(TransactionId(i64::from(id))));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_select_all(move || {
                this.update_view(TransactionsView::select_all_shown);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_clear_selection(move || {
                this.update_view(TransactionsView::clear_selection);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_show_partner(move |partner| {
                let partner = partner.to_string();
                this.spawn(move |view, db| {
                    let _ = view.show_partner(db, &partner, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_delete_row(move |id| {
                this.open_delete(TransactionId(i64::from(id)));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_confirm_delete(move |id| {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_delete_open(false);
                }
                this.spawn(move |view, db| {
                    let _ = view.delete_transaction(db, TransactionId(i64::from(id)), OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_cancel_delete(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_delete_open(false);
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_multi_delete(move || {
                this.open_multi_delete();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_confirm_multi_delete(move || {
                let ids = std::mem::take(&mut *this.multi_delete_ids.lock().expect("transactions delete ids poisoned"));
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_multi_delete_open(false);
                }
                this.spawn(move |view, db| {
                    let _ = view.delete_selected(db, &ids, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_cancel_multi_delete(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_multi_delete_open(false);
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_batch_category(move |index| {
                let category = this.category_id_at(index);
                this.spawn(move |view, db| {
                    let _ = view.set_category_for_selection(db, category, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_batch_tax(move |index| {
                let value = match index {
                    1 => Some(true),
                    2 => Some(false),
                    _ => None,
                };
                this.spawn(move |view, db| {
                    let _ = view.set_tax_override_for_selection(db, value, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_open_add(move || {
                this.open_add();
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_add_submit(move |account, expense, date, amount, partner, purpose, category| {
                this.add_transaction(account, expense, &date, &amount, &partner, &purpose, category);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_close_add(move || {
                if let Some(ui) = this.ui.upgrade() {
                    ui.set_transactions_add_open(false);
                    ui.set_transactions_add_error("".into());
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_confirm_duplicate(move || {
                this.spawn(|view, db| {
                    let _ = view.confirm_duplicate(db, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_dismiss_duplicate(move || {
                this.update_view(TransactionsView::dismiss_duplicate);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_close(move || {
                let documents = documents_dir(&this.paths, &this.profile_key());
                this.spawn(move |view, db| {
                    let service = DocumentService::new(db, OcrService::without_ocr(), documents);
                    let _ = view.select_transaction(db, &service, None);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_category(move |index| {
                let category = this.category_id_at(index);
                let id = this.selected_id();
                if let Some(id) = id {
                    this.spawn(move |view, db| {
                        let _ = view.update_transaction_category(db, id, category, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_tax(move |index| {
                let value = match index {
                    1 => Some(true),
                    2 => Some(false),
                    _ => None,
                };
                let id = this.selected_id();
                if let Some(id) = id {
                    this.spawn(move |view, db| {
                        let _ = view.set_tax_override(db, id, value, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_detail_show_partner(move |partner| {
                let partner = partner.to_string();
                this.spawn(move |view, db| {
                    let _ = view.show_partner(db, &partner, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_dismiss_message(move || {
                this.update_view(|view| {
                    view.state_mut().error = None;
                    view.state_mut().message = None;
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_select(move |kind, group_kind, id, account_type| {
                if let Some(selection) = selection_from(&kind, &group_kind, id, &account_type) {
                    this.select_accounts(selection);
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_toggle(move |kind, group_kind, id, account_type| {
                if let Some(AccountSelection::Group(key)) = selection_from(&kind, &group_kind, id, &account_type) {
                    this.spawn(move |view, db| {
                        let _ = view.toggle_collapsed(db, &key);
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_mark_read(move |kind, group_kind, id, account_type| {
                if let Some(selection) = selection_from(&kind, &group_kind, id, &account_type) {
                    this.spawn(move |view, db| {
                        let _ = view.mark_read(db, selection, OffsetDateTime::now_utc().date());
                    });
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_new_group(move |name| {
                let name = name.to_string();
                this.spawn(move |view, db| {
                    let _ = view.create_group(db, &name, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_rename_group(move |id, name| {
                let name = name.to_string();
                this.spawn(move |view, db| {
                    let _ = view.rename_group(
                        db,
                        AccountGroupId(i64::from(id)),
                        &name,
                        OffsetDateTime::now_utc().date(),
                    );
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_delete_group(move |id| {
                this.spawn(move |view, db| {
                    let _ = view.delete_group(db, AccountGroupId(i64::from(id)), OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_move_group(move |id, up| {
                this.spawn(move |view, db| {
                    let _ = view.move_group(db, AccountGroupId(i64::from(id)), up, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_move_account(move |account, group| {
                let group = (group >= 0).then_some(AccountGroupId(i64::from(group)));
                this.spawn(move |view, db| {
                    let _ = view.move_account_to_group(
                        db,
                        AccountId(i64::from(account)),
                        group,
                        OffsetDateTime::now_utc().date(),
                    );
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_transactions_panel_filter_changed(move |text| {
                let text = text.to_string();
                this.update_view(move |view| view.update_account_filter_text(&text));
            });
        }
    }

    /// Makes the controller use `db` and starts from a clean page for a profile.
    pub fn attach(&self, db: Arc<Database>, language: Language) {
        *self.db.lock().expect("transactions database poisoned") = Some(db);
        let mut view = self.view.lock().expect("transactions view poisoned");
        *view = Some(TransactionsView::new(language));
        let snapshot = view.as_ref().expect("a view was just installed").state().clone();
        drop(view);
        self.reapply_excluded
            .lock()
            .expect("reapply exclusions poisoned")
            .clear();
        self.publish(snapshot);
    }

    /// Clears the page, for when the profile is locked.
    pub fn reset(&self) {
        *self.db.lock().expect("transactions database poisoned") = None;
        let language = self
            .view
            .lock()
            .expect("transactions view poisoned")
            .as_ref()
            .map_or(Language::En, |view| view.state().language);
        let mut view = self.view.lock().expect("transactions view poisoned");
        *view = Some(TransactionsView::new(language));
        let snapshot = view.as_ref().expect("a view was just installed").state().clone();
        drop(view);
        self.reapply_excluded
            .lock()
            .expect("reapply exclusions poisoned")
            .clear();
        self.publish(snapshot);
    }

    /// Applies a language change to the page's own texts.
    pub fn set_language(&self, language: Language) {
        let mut view = self.view.lock().expect("transactions view poisoned");
        if let Some(view) = view.as_mut() {
            view.state_mut().language = language;
        } else {
            *view = Some(TransactionsView::new(language));
        }
        let snapshot = view.as_ref().expect("a view is installed").state().clone();
        drop(view);
        self.publish(snapshot);
    }

    /// Opens the page, applying navigation context when one is given.
    pub fn open_page(self: &Arc<Self>, args: StartArgs) {
        self.spawn(move |view, db| {
            let _ = view.start(db, args, OffsetDateTime::now_utc().date());
        });
    }

    fn update_search_query(self: &Arc<Self>, query: &str) -> bool {
        let query = query.to_string();
        self.spawn(move |view, db| {
            let _ = view.update_search_query(db, &query, OffsetDateTime::now_utc().date());
        })
    }

    /// Keeps a debounced query that fired while a job was running and runs it as soon as possible.
    fn defer_search(self: &Arc<Self>, query: &str) {
        self.pending_search
            .lock()
            .expect("transactions search poisoned")
            .defer(query);
        self.run_pending_search();
    }

    /// Starts the latest deferred query when no job is running, keeping it otherwise.
    fn run_pending_search(self: &Arc<Self>) {
        if self.busy.active() {
            return;
        }
        let query = self.pending_search.lock().expect("transactions search poisoned").take();
        let Some(query) = query else {
            return;
        };
        if !self.update_search_query(&query) {
            self.pending_search
                .lock()
                .expect("transactions search poisoned")
                .restore(query);
        }
    }

    fn select_accounts(self: &Arc<Self>, selection: AccountSelection) {
        self.spawn(move |view, db| {
            let _ = view.select_accounts(db, selection, OffsetDateTime::now_utc().date());
        });
    }

    fn open_range(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let today = OffsetDateTime::now_utc().date();
        let Some(initial) = self.with_state(|state| {
            state
                .custom_range
                .or_else(|| state.month.map(|month| (month.start(), month.end())))
                .or_else(|| {
                    let (from, to) = state.period.range(today);
                    from.zip(to)
                })
                .unwrap_or_else(|| {
                    let year = Date::from_calendar_date(today.year(), time::Month::January, 1).expect("valid date");
                    (year, today)
                })
        }) else {
            return;
        };
        ui.set_transactions_range_from(format::date(initial.0).into());
        ui.set_transactions_range_to(format::date(initial.1).into());
        ui.set_transactions_range_error("".into());
        ui.set_transactions_range_open(true);
    }

    fn apply_range(self: &Arc<Self>, from_text: &str, to_text: &str) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let language = self.language();
        let Some(from) = format::parse_date(from_text) else {
            ui.set_transactions_range_error(i18n::text(language, Key::EnterValidDate).into());
            return;
        };
        let Some(to) = format::parse_date(to_text) else {
            ui.set_transactions_range_error(i18n::text(language, Key::EnterValidDate).into());
            return;
        };
        if from > to {
            ui.set_transactions_range_error(i18n::text(language, Key::InvalidDateRange).into());
            return;
        }
        ui.set_transactions_range_open(false);
        ui.set_transactions_range_error("".into());
        self.spawn(move |view, db| {
            let _ = view.set_custom_range(db, from, to, OffsetDateTime::now_utc().date());
        });
    }

    fn open_amount(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Some((currency, language, min, max)) = self.with_state(|state| {
            (
                state.primary_currency,
                state.language,
                state.min_amount.map(money::format_plain).unwrap_or_default(),
                state.max_amount.map(money::format_plain).unwrap_or_default(),
            )
        }) else {
            return;
        };
        ui.set_transactions_amount_min(min.into());
        ui.set_transactions_amount_max(max.into());
        let symbol = format::currency_symbol(currency);
        ui.set_transactions_amount_min_label(
            i18n::format(language, Key::MinAmountIn, &[Arg::Str(symbol.clone())]).into(),
        );
        ui.set_transactions_amount_max_label(i18n::format(language, Key::MaxAmountIn, &[Arg::Str(symbol)]).into());
        ui.set_transactions_amount_error("".into());
        ui.set_transactions_amount_open(true);
    }

    fn apply_amount(self: &Arc<Self>, minimum: &str, maximum: &str) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let language = self.language();
        let currency = self
            .with_state(|state| state.primary_currency)
            .unwrap_or(Currencies::DEFAULT);
        let min = parse_bound(minimum, currency);
        let max = parse_bound(maximum, currency);
        if !minimum.trim().is_empty() && min.is_none() {
            ui.set_transactions_amount_error(i18n::text(language, Key::EnterValidAmount).into());
            return;
        }
        if !maximum.trim().is_empty() && max.is_none() {
            ui.set_transactions_amount_error(i18n::text(language, Key::EnterValidAmount).into());
            return;
        }
        ui.set_transactions_amount_open(false);
        ui.set_transactions_amount_error("".into());
        self.spawn(move |view, db| {
            let _ = view.update_amount_range(db, min, max, OffsetDateTime::now_utc().date());
        });
    }

    fn open_delete(&self, id: TransactionId) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Some(Some((language, text))) = self.with_state(|state| {
            let transaction = state.transactions.iter().find(|transaction| transaction.id == id)?;
            let language = state.language;
            Some((
                language,
                format!(
                    "{} {} {}",
                    format::date(transaction.date),
                    bank_text::display_description(transaction),
                    format::money(transaction.amount, language, false),
                ),
            ))
        }) else {
            return;
        };
        ui.set_transactions_delete_id(i32::try_from(id.0).unwrap_or(-1));
        ui.set_transactions_delete_title(i18n::text(language, Key::DeleteTransactionTitle).into());
        ui.set_transactions_delete_text(text.into());
        ui.set_transactions_delete_open(true);
    }

    fn open_multi_delete(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Some(Some((language, ids, count, range, totals, lines, more_text))) = self.with_state(|state| {
            let language = state.language;
            let bookings = state.selected_shown();
            if bookings.is_empty() {
                return None;
            }
            let count = bookings.len();
            let ids: BTreeSet<TransactionId> = bookings.iter().map(|transaction| transaction.id).collect();
            let from = bookings
                .iter()
                .map(|transaction| transaction.date)
                .min()
                .expect("non-empty");
            let to = bookings
                .iter()
                .map(|transaction| transaction.date)
                .max()
                .expect("non-empty");
            let range = if from == to {
                format::date(from)
            } else {
                format!("{}–{}", format::date(from), format::date(to))
            };
            let totals = state
                .selected_totals()
                .iter()
                .map(|(currency, minor)| {
                    format::money(
                        Money {
                            minor: *minor,
                            currency: *currency,
                        },
                        language,
                        true,
                    )
                })
                .collect::<Vec<_>>()
                .join(" · ");
            let lines = bookings
                .iter()
                .take(5)
                .map(|transaction| {
                    format!(
                        "{} {} {}",
                        format::date(transaction.date),
                        bank_text::display_description(transaction),
                        format::money(transaction.amount, language, false),
                    )
                })
                .collect::<Vec<_>>();
            let more = bookings.len().saturating_sub(5);
            let more_text = if more > 0 {
                i18n::format(language, Key::AndMore, &[Arg::from(more)])
            } else {
                String::new()
            };
            Some((language, ids, count, range, totals, lines, more_text))
        }) else {
            return;
        };
        *self.multi_delete_ids.lock().expect("transactions delete ids poisoned") = ids;
        ui.set_transactions_multi_delete_title(
            i18n::format(language, Key::DeleteTransactionsTitle, &[Arg::from(count)]).into(),
        );
        ui.set_transactions_multi_delete_range(range.into());
        ui.set_transactions_multi_delete_totals(totals.into());
        ui.set_transactions_multi_delete_lines(ModelRc::new(VecModel::from(
            lines.into_iter().map(SharedString::from).collect::<Vec<_>>(),
        )));
        ui.set_transactions_multi_delete_more(more_text.into());
        ui.set_transactions_multi_delete_open(true);
    }

    fn open_add(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Some((account_options, index, amount_label)) = self.with_state(|state| {
            let language = state.language;
            let account_options = state
                .accounts
                .iter()
                .map(|account| account_label(account, language))
                .collect::<Vec<_>>();
            let selected_account = state
                .account_id
                .and_then(|id| state.accounts.iter().find(|account| account.id == id))
                .map_or_else(|| state.primary_currency, |account| account.currency);
            let index = state
                .account_id
                .and_then(|id| state.accounts.iter().position(|account| account.id == id))
                .map_or(0, |index| i32::try_from(index).unwrap_or(0));
            let symbol = format::currency_symbol(selected_account);
            let amount_label = i18n::format(language, Key::AmountIn, &[Arg::Str(symbol)]);
            (account_options, index, amount_label)
        }) else {
            return;
        };
        ui.set_transactions_add_account_options(ModelRc::new(VecModel::from(
            account_options.into_iter().map(SharedString::from).collect::<Vec<_>>(),
        )));
        ui.set_transactions_add_account_index(index);
        ui.set_transactions_add_date(format::date(OffsetDateTime::now_utc().date()).into());
        ui.set_transactions_add_amount_label(amount_label.into());
        ui.set_transactions_add_error("".into());
        ui.set_transactions_add_open(true);
    }

    fn add_transaction(
        self: &Arc<Self>,
        account_index: i32,
        expense: bool,
        date_text: &str,
        amount_text: &str,
        partner: &str,
        purpose: &str,
        category_index: i32,
    ) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let language = self.language();
        let Some(Some((account_id, currency))) = self.with_state(|state| {
            state
                .accounts
                .get(usize::try_from(account_index).unwrap_or(0))
                .or_else(|| state.accounts.first())
                .map(|account| (account.id, account.currency))
        }) else {
            return;
        };
        let Some(date) = format::parse_date(date_text) else {
            ui.set_transactions_add_error(i18n::text(language, Key::EnterValidDate).into());
            return;
        };
        let Ok(parsed) = money::parse_decimal(amount_text, currency) else {
            ui.set_transactions_add_error(i18n::text(language, Key::EnterValidAmount).into());
            return;
        };
        let amount = Money {
            minor: if expense {
                -parsed.minor.abs()
            } else {
                parsed.minor.abs()
            },
            currency,
        };
        let category_id = self.category_id_at(category_index);
        let partner = Some(partner.trim().to_owned()).filter(|text| !text.is_empty());
        let purpose = Some(purpose.trim().to_owned()).filter(|text| !text.is_empty());
        ui.set_transactions_add_open(false);
        ui.set_transactions_add_error("".into());
        self.spawn(move |view, db| {
            let _ = view.add_manual_transaction(
                db,
                ManualTransaction {
                    account_id,
                    date,
                    amount,
                    partner_name: partner,
                    purpose,
                    category_id,
                    confirmed_duplicate: false,
                },
                OffsetDateTime::now_utc().date(),
            );
        });
    }

    fn open_detail(self: &Arc<Self>, id: TransactionId) {
        let Some(Some(transaction)) = self.with_state(|state| {
            state
                .transactions
                .iter()
                .find(|transaction| transaction.id == id)
                .cloned()
        }) else {
            return;
        };
        let documents = documents_dir(&self.paths, &self.profile_key());
        self.spawn(move |view, db| {
            let service = DocumentService::new(db, OcrService::without_ocr(), documents.clone());
            let _ = view.select_transaction(db, &service, Some(transaction));
        });
    }

    fn spawn<F>(self: &Arc<Self>, action: F) -> bool
    where
        F: FnOnce(&mut TransactionsView, &Database) + Send + 'static,
    {
        let Some(db) = self.database() else {
            return false;
        };
        if self.busy.active() {
            return false;
        }
        let Some(mut view) = self.take_view() else {
            return false;
        };

        self.busy.begin();
        let generation = self.jobs.begin();
        let this = Arc::clone(self);
        let busy = self.busy.clone();
        thread::spawn(move || {
            let guard = BusyGuard(busy);
            action(&mut view, &db);
            let snapshot = view.state().clone();
            this.publish_job(snapshot, generation, view);
            drop(guard);
            this.run_pending_search();
        });
        true
    }

    fn update_view(&self, action: impl FnOnce(&mut TransactionsView)) {
        let Ok(mut slot) = self.view.lock() else {
            return;
        };
        let Some(view) = slot.as_mut() else {
            return;
        };
        action(view);
        let snapshot = view.state().clone();
        drop(slot);
        self.publish(snapshot);
    }

    fn take_view(&self) -> Option<TransactionsView> {
        self.view.lock().ok()?.take()
    }

    fn put_view(&self, view: TransactionsView) {
        if let Ok(mut slot) = self.view.lock()
            && slot.is_none()
        {
            *slot = Some(view);
        }
    }

    fn republish(&self) {
        if let Some(state) = self.state_snapshot() {
            self.publish(state);
        }
    }

    fn state_snapshot(&self) -> Option<TransactionsState> {
        self.with_state(TransactionsState::clone)
    }

    fn with_state<T>(&self, f: impl FnOnce(&TransactionsState) -> T) -> Option<T> {
        let slot = self.view.try_lock().ok()?;
        let view = slot.as_ref()?;
        Some(f(view.state()))
    }

    fn publish(&self, state: TransactionsState) {
        let excluded = self
            .reapply_excluded
            .lock()
            .map(|excluded| excluded.clone())
            .unwrap_or_default();
        let ui = self.ui.clone();
        let _ = slint::invoke_from_event_loop(move || {
            if let Some(ui) = ui.upgrade() {
                apply_with_excluded(&ui, &state, &excluded);
            }
        });
    }

    /// Returns a worker's view and publishes its result, unless the profile has been switched in the meantime: then
    /// `reset`/`attach` has already installed a fresh view and the old one, with the old profile's data, is dropped.
    fn publish_job(&self, state: TransactionsState, generation: u64, view: TransactionsView) {
        if !self.jobs.is_current(generation) {
            self.jobs.end();
            return;
        }
        // `put_view` never replaces a view that `reset`/`attach` installed after the check above.
        self.put_view(view);
        let excluded = self
            .reapply_excluded
            .lock()
            .map(|excluded| excluded.clone())
            .unwrap_or_default();
        let ui = self.ui.clone();
        let jobs = self.jobs.clone();
        let _ = slint::invoke_from_event_loop(move || {
            jobs.end();
            if !jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = ui.upgrade() {
                apply_with_excluded(&ui, &state, &excluded);
            }
        });
    }

    fn database(&self) -> Option<Arc<Database>> {
        self.db.lock().expect("transactions database poisoned").clone()
    }

    fn language(&self) -> Language {
        self.with_state(|state| state.language).unwrap_or(Language::En)
    }

    fn selected_id(&self) -> Option<TransactionId> {
        self.with_state(|state| state.selected.as_ref().map(|transaction| transaction.id))
            .flatten()
    }

    fn category_filter_at(&self, index: i32) -> Option<CategoryFilter> {
        match index {
            0 => Some(CategoryFilter::Any),
            1 => Some(CategoryFilter::None),
            _ => self.with_state(|state| {
                state
                    .categories
                    .get(usize::try_from(index - 2).ok()?)
                    .map(|category| CategoryFilter::Only(category.id))
            })?,
        }
    }

    fn category_id_at(&self, index: i32) -> Option<CategoryId> {
        if index <= 0 {
            return None;
        }
        self.with_state(|state| {
            state
                .categories
                .get(usize::try_from(index - 1).ok()?)
                .map(|category| category.id)
        })?
    }

    fn rule_category_at(&self, index: i32) -> Option<CategoryId> {
        self.with_state(|state| {
            state
                .categories
                .get(usize::try_from(index).ok()?)
                .map(|category| category.id)
        })?
    }

    fn open_rule_dialog(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Some(Some((pattern, category_index, categories))) = self.with_state(|state| {
            let transaction = state.selected.as_ref()?;
            let language = state.language;
            let pattern = TransactionsView::new(language).rule_pattern_for(transaction);
            let category_index = transaction
                .category_id
                .and_then(|id| state.categories.iter().position(|category| category.id == id))
                .map_or(0, |index| i32::try_from(index).unwrap_or(0));
            let categories = state
                .categories
                .iter()
                .map(|category| SharedString::from(category.name.clone()))
                .collect::<Vec<_>>();
            Some((pattern, category_index, categories))
        }) else {
            return;
        };
        ui.set_transactions_rule_open(true);
        ui.set_transactions_rule_pattern(pattern.into());
        ui.set_transactions_rule_categories(ModelRc::new(VecModel::from(categories)));
        ui.set_transactions_rule_category_index(category_index);
        ui.set_transactions_rule_error("".into());
    }

    fn selection_at(&self, index: i32) -> Option<AccountSelection> {
        self.with_state(|state| {
            let choices = selection_choices(state);
            choices.get(usize::try_from(index).ok()?).cloned()
        })?
    }
}

/// Applies `state` to the window without touching the database.
#[allow(dead_code)]
pub fn apply(main: &MainWindow, state: &TransactionsState) {
    apply_with_excluded(main, state, &BTreeSet::new());
}

/// Applies `state`, keeping the re-apply groups outside `excluded` ticked.
fn apply_with_excluded(main: &MainWindow, state: &TransactionsState, excluded: &BTreeSet<ReapplyKey>) {
    let language = state.language;
    main.set_transactions_loading(state.is_loading);
    main.set_transactions_has_accounts(!state.accounts.is_empty());
    main.set_transactions_error_text(state.error.clone().unwrap_or_default().into());
    main.set_transactions_message_text(state.message.clone().unwrap_or_default().into());

    main.set_transactions_count_text(state.transactions.len().to_string().into());
    main.set_transactions_income_amounts(ModelRc::new(VecModel::from(amount_rows(
        &state.total_income,
        "positive",
        language,
    ))));
    main.set_transactions_expense_amounts(ModelRc::new(VecModel::from(amount_rows(
        &state.total_expenses,
        "negative",
        language,
    ))));
    main.set_transactions_has_balance(state.current_balance.is_some());
    if let (Some(balance), Some(currency)) = (state.current_balance, state.balance_currency) {
        let _ = currency;
        main.set_transactions_balance_text(format::money(balance, language, false).into());
        main.set_transactions_balance_tone(
            tone_code(account_grouping::balance_tone_value(
                balance,
                state.current_balance_is_loan,
            ))
            .into(),
        );
    }

    let show_back = !state.search_query.trim().is_empty()
        && state.account_id.is_none()
        && state.period == Period::All
        && state.direction == Direction::All
        && state.category_filter == CategoryFilter::Any
        && state.custom_range.is_none()
        && state.min_amount.is_none()
        && state.max_amount.is_none();
    main.set_transactions_show_back(show_back);

    main.set_transactions_period_options(string_model(period_options(language)));
    main.set_transactions_period_index(period_index(state.period));
    main.set_transactions_period_extra(period_extra(state, language).into());
    main.set_transactions_category_options(string_model(category_filter_options(state, language)));
    main.set_transactions_category_index(category_filter_index(state));
    main.set_transactions_direction_options(string_model(direction_options(language)));
    main.set_transactions_direction_index(direction_index(state.direction));
    main.set_transactions_amount_label(amount_label(state, language).into());
    let choices = account_choices(state, language);
    main.set_transactions_account_options(string_model(
        choices
            .iter()
            .map(|(_, label)| SharedString::from(label.clone()))
            .collect(),
    ));
    main.set_transactions_account_index(
        choices
            .iter()
            .position(|(selection, _)| *selection == state.selection)
            .map_or(0, |index| i32::try_from(index).unwrap_or(0)),
    );

    main.set_transactions_panel_rows(ModelRc::new(VecModel::from(panel_rows(state, language))));
    main.set_transactions_group_choices(ModelRc::new(VecModel::from(
        state
            .custom_groups
            .iter()
            .map(|group| AccountGroupChoice {
                id: id_i32(group.id.0),
                name: group.name.clone().into(),
            })
            .collect::<Vec<_>>(),
    )));
    main.set_transactions_panel_filter_text(state.account_filter_text.clone().into());
    main.set_transactions_panel_shown_text(panel_shown_text(state, language).into());

    main.set_transactions_pending_header(pending_header(state, language).into());
    main.set_transactions_rows(ModelRc::new(VecModel::from(transaction_rows(state, language))));
    main.set_transactions_has_transactions(!state.transactions.is_empty());
    main.set_transactions_has_balance_column(state.balances.is_some());

    main.set_transactions_add_category_names(string_model(add_category_names(state, language)));
    main.set_transactions_add_date(format::date(OffsetDateTime::now_utc().date()).into());
    main.set_transactions_add_amount_label(
        i18n::format(
            language,
            Key::AmountIn,
            &[Arg::Str(format::currency_symbol(state.primary_currency))],
        )
        .into(),
    );

    apply_chart(main, state, language);
    apply_selection(main, state, language);
    apply_detail(main, state, language);
    apply_import(main, state, language);
    apply_reapply(main, state, language, excluded);
}

fn apply_import(main: &MainWindow, state: &TransactionsState, language: Language) {
    let Some(preview) = &state.import_preview else {
        main.set_transactions_import_open(false);
        return;
    };
    let title = if preview.files.len() <= 1 {
        let name = preview
            .files
            .first()
            .map_or_else(String::new, |file| display_name(file));
        i18n::format(language, Key::ImportFile, &[Arg::from(name)])
    } else {
        i18n::format(language, Key::ImportFiles, &[Arg::from(preview.files.len())])
    };
    let summary = if preview.files.len() > 1 {
        i18n::format(language, Key::BookingCount, &[Arg::from(preview.booking_count())])
    } else {
        String::new()
    };
    let selected_account = preview
        .account_id
        .and_then(|id| state.accounts.iter().find(|account| account.id == id));
    let account_options = state
        .accounts
        .iter()
        .map(|account| SharedString::from(account_label(account, language)))
        .collect::<Vec<_>>();
    let account_index = preview
        .account_id
        .and_then(|id| state.accounts.iter().position(|account| account.id == id))
        .map_or(0, |index| i32::try_from(index).unwrap_or(0));
    let csv_fields = import_csv_fields(preview, language);
    let csv_hint = import_csv_hint(preview, language);
    let newer_text = if import_has_balances(preview) && !preview.balance_update_allowed {
        i18n::text(language, Key::NewerBookingsUnchanged).to_owned()
    } else {
        String::new()
    };

    main.set_transactions_import_open(true);
    main.set_transactions_import_title(title.into());
    main.set_transactions_import_summary(summary.into());
    main.set_transactions_import_files(ModelRc::new(VecModel::from(import_file_rows(
        preview,
        language,
        selected_account,
    ))));
    main.set_transactions_import_csv_fields(ModelRc::new(VecModel::from(csv_fields)));
    main.set_transactions_import_csv_hint(csv_hint.into());
    main.set_transactions_import_account_options(string_model(account_options));
    main.set_transactions_import_account_index(account_index);
    main.set_transactions_import_balance_allowed(preview.balance_update_allowed);
    main.set_transactions_import_update_balance(preview.balance_update_allowed);
    main.set_transactions_import_balance_text(i18n::text(language, Key::SetAccountBalance).to_owned().into());
    main.set_transactions_import_newer_text(newer_text.into());
    main.set_transactions_importing(state.is_importing);
    main.set_transactions_import_confirm_text(
        if state.is_importing {
            i18n::text(language, Key::Importing).to_owned()
        } else {
            i18n::format(language, Key::ImportCount, &[Arg::from(preview.booking_count())])
        }
        .into(),
    );
    main.set_transactions_import_confirm_enabled(
        !state.is_importing && preview.account_id.is_some() && preview.booking_count() > 0,
    );
}

fn apply_reapply(main: &MainWindow, state: &TransactionsState, language: Language, excluded: &BTreeSet<ReapplyKey>) {
    main.set_transactions_can_undo_reapply(state.can_undo_reapply);
    let Some(preview) = &state.reapply_preview else {
        main.set_transactions_reapply_open(false);
        return;
    };
    let groups = reapply_group_rows(preview, excluded, language);
    let ticked: usize = preview
        .groups
        .iter()
        .filter(|group| !excluded.contains(&(group.from_category_id, group.to_category_id)))
        .map(|group| group.count)
        .sum();
    main.set_transactions_reapply_open(true);
    main.set_transactions_reapply_summary(
        i18n::format(
            language,
            Key::ReapplyRulesSummary,
            &[Arg::from(preview.changes.len()), Arg::from(preview.shown_count)],
        )
        .into(),
    );
    main.set_transactions_reapply_warning(i18n::text(language, Key::ReapplyRulesWarning).into());
    main.set_transactions_reapply_groups(ModelRc::new(VecModel::from(groups)));
    main.set_transactions_reapply_confirm_text(
        i18n::format(language, Key::ReapplyRulesConfirm, &[Arg::from(ticked)]).into(),
    );
    main.set_transactions_reapply_confirm_enabled(ticked > 0);
}

fn apply_chart(main: &MainWindow, state: &TransactionsState, language: Language) {
    let Some(points) = state.balance_chart.as_ref().filter(|points| points.len() >= 2) else {
        main.set_transactions_has_chart(false);
        return;
    };
    let Some(chart) = chart_data(points, state.current_balance, language) else {
        main.set_transactions_has_chart(false);
        return;
    };
    main.set_transactions_has_chart(true);
    main.set_transactions_chart_collapsed(state.chart_collapsed);
    main.set_transactions_chart_line(chart.line.into());
    main.set_transactions_chart_area(chart.area.into());
    main.set_transactions_chart_axis_top(chart.axis_top.into());
    main.set_transactions_chart_axis_bottom(chart.axis_bottom.into());
    main.set_transactions_chart_axis_start(chart.axis_start.into());
    main.set_transactions_chart_axis_mid(chart.axis_mid.into());
    main.set_transactions_chart_axis_end(chart.axis_end.into());
    main.set_transactions_chart_value(chart.value.into());
    main.set_transactions_chart_not_from_zero(chart.not_from_zero);
}

fn apply_selection(main: &MainWindow, state: &TransactionsState, language: Language) {
    main.set_transactions_selection_visible(!state.selected_ids.is_empty());
    main.set_transactions_selection_count_text(
        i18n::format(language, Key::SelectedCount, &[Arg::from(state.selected_ids.len())]).into(),
    );
    let totals = state
        .selected_totals()
        .iter()
        .map(|(currency, minor)| {
            format::money(
                Money {
                    minor: *minor,
                    currency: *currency,
                },
                language,
                true,
            )
        })
        .collect::<Vec<_>>()
        .join(" · ");
    main.set_transactions_selection_totals_text(totals.into());
    main.set_transactions_category_names(string_model(category_names(state, language)));
    main.set_transactions_tax_options(string_model(tax_options(language)));
}

fn apply_detail(main: &MainWindow, state: &TransactionsState, language: Language) {
    main.set_transactions_detail_category_index(0);
    main.set_transactions_detail_tax_index(0);
    main.set_transactions_detail_can_create_rule(!state.categories.is_empty());
    let Some(transaction) = &state.selected else {
        main.set_transactions_detail_open(false);
        main.set_transactions_duplicate_open(state.pending_duplicate.is_some());
        apply_duplicate(main, state, language);
        return;
    };

    let account = state
        .accounts
        .iter()
        .find(|account| account.id == transaction.account_id);
    let category_index = transaction
        .category_id
        .and_then(|id| state.categories.iter().position(|category| category.id == id))
        .map_or(0, |index| i32::try_from(index + 1).unwrap_or(0));
    let tax_index = match transaction.tax_override {
        Some(true) => 1,
        Some(false) => 2,
        None => 0,
    };
    let partner_link = transaction.partner_name.as_ref().map_or_else(String::new, |partner| {
        i18n::format(language, Key::ShowAllFromPartner, &[Arg::from(partner.as_str())])
    });
    let mismatch = state
        .balances
        .as_ref()
        .and_then(|balances| balances.get(&transaction.id))
        .and_then(|entry| entry.bank_difference.map(|difference| (entry, difference)))
        .map_or_else(String::new, |(entry, difference)| {
            balance_mismatch_text(language, entry.balance, difference)
        });

    main.set_transactions_detail_open(true);
    main.set_transactions_detail_title(bank_text::display_description(transaction).into());
    main.set_transactions_detail_amount(format::money(transaction.amount, language, true).into());
    main.set_transactions_detail_expense(transaction.is_expense());
    main.set_transactions_detail_date(format::date(transaction.date).into());
    main.set_transactions_detail_account(
        account
            .map_or_else(
                || {
                    transaction
                        .account_iban
                        .as_deref()
                        .map(format::iban)
                        .unwrap_or_default()
                },
                |account| account_label(account, language),
            )
            .into(),
    );
    main.set_transactions_detail_partner(transaction.partner_name.clone().unwrap_or_default().into());
    main.set_transactions_detail_shop(
        PaymentProviders::merchant_of(transaction.purpose.as_deref(), transaction.partner_name.as_deref())
            .unwrap_or_default()
            .into(),
    );
    main.set_transactions_detail_purpose(transaction.purpose.clone().unwrap_or_default().into());
    main.set_transactions_detail_category_index(category_index);
    main.set_transactions_detail_tax_relevant(transaction.is_tax_relevant);
    main.set_transactions_detail_tax_state(tax_state_line(transaction, language).into());
    main.set_transactions_detail_tax_index(tax_index);
    main.set_transactions_detail_has_mismatch(!mismatch.is_empty());
    main.set_transactions_detail_mismatch(mismatch.into());
    main.set_transactions_detail_partner_link(partner_link.into());
    main.set_transactions_duplicate_open(state.pending_duplicate.is_some());
    apply_duplicate(main, state, language);
}

fn apply_duplicate(main: &MainWindow, state: &TransactionsState, language: Language) {
    if let Some(entry) = &state.pending_duplicate {
        main.set_transactions_duplicate_title(i18n::text(language, Key::DuplicateTransactionTitle).into());
        main.set_transactions_duplicate_text(
            i18n::format(
                language,
                Key::DuplicateTransactionText,
                &[
                    Arg::from(format::date(entry.date)),
                    Arg::from(format::money(entry.amount, language, false)),
                ],
            )
            .into(),
        );
    }
}

/// The import dialog's file rows, including the unreadable selections.
fn import_file_rows(
    preview: &ImportPreview,
    language: Language,
    selected_account: Option<&AccountItem>,
) -> Vec<ImportFileRow> {
    let mut rows = Vec::with_capacity(preview.entries.len() + preview.failures.len());
    let multi = preview.files.len() > 1;
    for entry in &preview.entries {
        let bookings = &entry.statement.bookings;
        let from = bookings.iter().map(|booking| booking.date).min();
        let to = bookings.iter().map(|booking| booking.date).max();
        let period_text = match (from, to) {
            (Some(from), Some(to)) if from == to => format::date(from),
            (Some(from), Some(to)) => format!("{}–{}", format::date(from), format::date(to)),
            _ => String::new(),
        };
        let mut warnings = Vec::new();
        if entry.statement.pending_skipped > 0 {
            warnings.push(i18n::format(
                language,
                Key::PendingLeftOut,
                &[Arg::from(i64::from(entry.statement.pending_skipped))],
            ));
        }
        if !entry.statement.skipped.is_empty() {
            warnings.push(i18n::format(
                language,
                Key::UnreadableCount,
                &[Arg::from(entry.statement.skipped.len())],
            ));
        }
        if let (Some(account), Some(file_iban)) = (selected_account, entry.statement.account_iban.as_deref())
            && finglass_core::iban::normalize(&account.iban) != finglass_core::iban::normalize(file_iban)
        {
            warnings.push(if multi {
                i18n::format(
                    language,
                    Key::FileBelongsToNamed,
                    &[Arg::from(display_name(&entry.file)), Arg::from(format::iban(file_iban))],
                )
            } else {
                i18n::format(language, Key::FileBelongsTo, &[Arg::from(format::iban(file_iban))])
            });
        }
        rows.push(ImportFileRow {
            name: display_name(&entry.file).into(),
            count_text: i18n::format(language, Key::BookingCount, &[Arg::from(bookings.len())]).into(),
            period_text: period_text.into(),
            warning_text: warnings.join(" · ").into(),
            failure: false,
        });
    }
    for failure in &preview.failures {
        rows.push(ImportFileRow {
            name: failure.file_name.clone().into(),
            count_text: String::new().into(),
            period_text: String::new().into(),
            warning_text: failure.message.clone().into(),
            failure: true,
        });
    }
    rows
}

/// The CSV mapping editor's rows, empty when the preview has no CSV file.
/// The choices of one CSV column picker: "none" and the file's headers.
fn import_csv_options(csv: &CsvLayout, language: Language) -> Vec<SharedString> {
    let mut options = vec![SharedString::from(i18n::text(language, Key::NoneDash))];
    options.extend(csv.headers.iter().enumerate().map(|(index, header)| {
        if header.trim().is_empty() {
            SharedString::from(i18n::format(language, Key::ColumnN, &[Arg::from(index + 1)]))
        } else {
            SharedString::from(header.clone())
        }
    }));
    options
}

/// The CSV mapping editor's rows, empty when the preview has no CSV file.
fn import_csv_fields(preview: &ImportPreview, language: Language) -> Vec<ImportCsvField> {
    let Some(csv) = preview.entries.iter().find_map(|entry| entry.statement.csv.as_ref()) else {
        return Vec::new();
    };
    let options = import_csv_options(csv, language);
    let fields = [
        ("date", Key::ColDate, csv.mapping.date),
        ("amount", Key::ColAmount, csv.mapping.amount),
        ("debit", Key::ColDebit, csv.mapping.debit),
        ("credit", Key::ColCredit, csv.mapping.credit),
        ("partner", Key::ColPartner, csv.mapping.partner),
        ("purpose", Key::ColPurpose, csv.mapping.purpose),
        ("balance", Key::ColBalance, csv.mapping.balance),
    ];
    fields
        .into_iter()
        .map(|(field, label, index)| ImportCsvField {
            field: field.into(),
            label: i18n::text(language, label).into(),
            options: ModelRc::new(VecModel::from(options.clone())),
            index: index.map_or(0, |index| i32::try_from(index + 1).unwrap_or(0)),
        })
        .collect()
}

/// How many of the selected CSV files the shown columns apply to.
fn import_csv_hint(preview: &ImportPreview, language: Language) -> String {
    let Some(csv) = preview.entries.iter().find_map(|entry| entry.statement.csv.as_ref()) else {
        return String::new();
    };
    let same = preview
        .entries
        .iter()
        .filter(|entry| {
            entry
                .statement
                .csv
                .as_ref()
                .is_some_and(|other| other.headers == csv.headers)
        })
        .count();
    if preview.entries.len() > 1 {
        i18n::format(language, Key::ColumnsApplyToSame, &[Arg::from(same)])
    } else {
        String::new()
    }
}

/// Whether the newest file carries balances that could update the account.
fn import_has_balances(preview: &ImportPreview) -> bool {
    let newest = &preview.newest().statement;
    newest.bookings.iter().any(|booking| booking.balance_after.is_some()) || newest.closing_balance.is_some()
}

/// The re-apply dialog's checkbox rows.
fn reapply_group_rows(
    preview: &ReapplyPreview,
    excluded: &BTreeSet<ReapplyKey>,
    language: Language,
) -> Vec<ReapplyGroupRow> {
    preview
        .groups
        .iter()
        .map(|group| {
            let sum = group.sum.map(|money| format::money(money, language, true)).into_iter();
            let detail = std::iter::once(group.count.to_string())
                .chain(sum)
                .collect::<Vec<_>>()
                .join(" · ");
            ReapplyGroupRow {
                from_id: group.from_category_id.map_or(-1, |id| id_i32(id.0)),
                to_id: id_i32(group.to_category_id.0),
                title: format!("{} → {}", group.from_name, group.to_name).into(),
                detail: detail.into(),
                ticked: !excluded.contains(&(group.from_category_id, group.to_category_id)),
            }
        })
        .collect()
}

/// Applies one CSV mapping field change from the dialog.
fn remap_mapping(mapping: &CsvMapping, field: &str, index: i32) -> CsvMapping {
    let index = usize::try_from(index).ok().and_then(|index| index.checked_sub(1));
    let mut mapping = mapping.clone();
    match field {
        "date" => mapping.date = index,
        "amount" => {
            mapping.amount = index;
            if index.is_some() {
                mapping.debit = None;
                mapping.credit = None;
            }
        }
        "debit" => {
            mapping.debit = index;
            if index.is_some() {
                mapping.amount = None;
            }
        }
        "credit" => {
            mapping.credit = index;
            if index.is_some() {
                mapping.amount = None;
            }
        }
        "partner" => mapping.partner = index,
        "purpose" => mapping.purpose = index,
        "balance" => mapping.balance = index,
        _ => {}
    }
    mapping
}

/// The key of one re-apply group from the dialog's two ids.
fn reapply_key(from: i32, to: i32) -> ReapplyKey {
    let from = (from >= 0).then_some(CategoryId(i64::from(from)));
    (from, CategoryId(i64::from(to)))
}

/// One row of the booking list, including the pending bank lines above it.
pub fn transaction_rows(state: &TransactionsState, language: Language) -> Vec<TransactionRow> {
    let mut rows = Vec::with_capacity(state.pending_bookings.len() + state.transactions.len());
    for line in &state.pending_bookings {
        rows.push(pending_row(line, language));
    }
    for transaction in &state.transactions {
        rows.push(booking_row(state, transaction, language));
    }
    rows
}

fn booking_row(state: &TransactionsState, transaction: &TransactionItem, language: Language) -> TransactionRow {
    let balance = state
        .balances
        .as_ref()
        .and_then(|balances| balances.get(&transaction.id));
    let balance_text = balance.map_or_else(String::new, |entry| format::money(entry.balance, language, false));
    let balance_tone = balance.map_or("zero", |entry| {
        tone_code(account_grouping::balance_tone_value(
            entry.balance,
            state.current_balance_is_loan,
        ))
    });
    let mismatch = balance
        .and_then(|entry| entry.bank_difference.map(|difference| (entry, difference)))
        .map_or_else(String::new, |(entry, difference)| {
            balance_mismatch_text(language, entry.balance, difference)
        });
    let partner = transaction
        .partner_name
        .as_deref()
        .map(str::trim)
        .filter(|partner| !partner.is_empty())
        .unwrap_or_default();
    let partner_link = if partner.is_empty() {
        String::new()
    } else {
        i18n::format(language, Key::ShowAllFromPartner, &[Arg::from(partner)])
    };
    TransactionRow {
        pending: false,
        id: id_i32(transaction.id.0),
        date_text: format::date(transaction.date).into(),
        title: bank_text::display_description(transaction).into(),
        subtitle: bank_text::secondary_line(transaction).unwrap_or_default().into(),
        category_text: transaction
            .category_name
            .clone()
            .unwrap_or_else(|| i18n::text(language, Key::Uncategorized).to_owned())
            .into(),
        category_colour: color(transaction.category_color.as_deref()),
        partner: partner.into(),
        partner_link_text: partner_link.into(),
        amount_text: format::money(transaction.amount, language, true).into(),
        expense: transaction.is_expense(),
        is_new: state.bold_ids.contains(&transaction.id),
        selected: state.selected_ids.contains(&transaction.id),
        has_documents: transaction.document_count > 0,
        documents_text: i18n::format(language, Key::DocumentsCount, &[Arg::from(transaction.document_count)]).into(),
        balance_text: balance_text.into(),
        balance_tone: balance_tone.into(),
        balance_warning: !mismatch.is_empty(),
        balance_mismatch: mismatch.into(),
    }
}

fn pending_row(line: &PendingBookingItem, language: Language) -> TransactionRow {
    let title = bank_text::display_partner(line.partner_name.as_deref())
        .or_else(|| bank_text::display_purpose(line.purpose.as_deref()))
        .unwrap_or_else(|| "—".to_owned());
    let subtitle = if line.partner_name.is_some() {
        bank_text::display_purpose(line.purpose.as_deref()).unwrap_or_default()
    } else {
        String::new()
    };
    TransactionRow {
        pending: true,
        id: -1,
        date_text: format::date(line.date).into(),
        title: title.into(),
        subtitle: subtitle.into(),
        category_text: String::new().into(),
        category_colour: color(None),
        partner: String::new().into(),
        partner_link_text: String::new().into(),
        amount_text: format::money(line.amount, language, true).into(),
        expense: line.amount.minor < 0,
        is_new: false,
        selected: false,
        has_documents: false,
        documents_text: String::new().into(),
        balance_text: String::new().into(),
        balance_tone: "zero".into(),
        balance_warning: false,
        balance_mismatch: String::new().into(),
    }
}

fn pending_header(state: &TransactionsState, language: Language) -> String {
    state.pending_bookings.first().map_or_else(String::new, |line| {
        i18n::format(
            language,
            Key::PendingHeader,
            &[Arg::from(format::date_time(line.fetched_at))],
        )
    })
}

fn panel_rows(state: &TransactionsState, language: Language) -> Vec<AccountPanelRow> {
    let filtering = !state.account_filter_text.trim().is_empty();
    let all_accounts: Vec<GroupingAccount> = state.accounts.iter().map(grouping).collect();
    let all_new: i64 = state.accounts.iter().map(|account| account.new_count).sum();
    let mut rows = vec![AccountPanelRow {
        kind: "all".into(),
        group_kind: "".into(),
        account_type: "".into(),
        id: -1,
        label: i18n::text(language, Key::AllAccounts).into(),
        secondary: "".into(),
        detail: "".into(),
        name: "".into(),
        delete_text: "".into(),
        selected: state.selection == AccountSelection::All,
        collapsed: false,
        new_count: i32::try_from(all_new).unwrap_or(i32::MAX),
        totals: amounts(&state.grand_total, &all_accounts, language, state.grand_total.len() > 1),
        indent: false,
    }];

    for group in &state.groups {
        let matching: Vec<&GroupingAccount> = group
            .accounts
            .iter()
            .filter(|account| {
                state.accounts.iter().any(|full| {
                    full.id == account.id
                        && account_panel::matches_filter(
                            full.name.as_deref(),
                            &identifier(full),
                            &full.bank_name,
                            &state.account_filter_text,
                        )
                })
            })
            .collect();
        if filtering && matching.is_empty() {
            continue;
        }

        let collapsed = state.collapsed.contains(&group.key.id());
        let selected = matches!(&state.selection, AccountSelection::Group(key) if key.id() == group.key.id());
        let (group_kind, account_type, id, name) = match &group.key {
            GroupKey::Type(account_type) => ("type", account_type.as_str(), -1, String::new()),
            GroupKey::Custom(group_id) => ("custom", "", id_i32(group_id.0), group.name.clone().unwrap_or_default()),
        };
        let label = group
            .name
            .clone()
            .unwrap_or_else(|| account_type_label(account_type, language));
        let delete_text = if group_kind == "custom" {
            i18n::format(language, Key::DeleteGroupText, &[Arg::from(name.as_str())])
        } else {
            String::new()
        };
        rows.push(AccountPanelRow {
            kind: "group".into(),
            group_kind: group_kind.into(),
            account_type: account_type.into(),
            id,
            label: label.into(),
            secondary: "".into(),
            detail: "".into(),
            name: name.into(),
            delete_text: delete_text.into(),
            selected,
            collapsed,
            new_count: i32::try_from(group.new_count).unwrap_or(i32::MAX),
            totals: amounts(&group.totals, &group.accounts, language, false),
            indent: false,
        });

        if collapsed {
            continue;
        }
        for account in matching {
            let Some(full) = state.accounts.iter().find(|item| item.id == account.id) else {
                continue;
            };
            let secondary = if full.name.is_some() {
                digits(full).map(|digits| format!("…{digits}")).unwrap_or_default()
            } else {
                String::new()
            };
            let detail = full
                .last_update
                .map(|updated| i18n::format(language, Key::LastFetched, &[Arg::from(format::date(updated.date()))]))
                .unwrap_or_default();
            rows.push(AccountPanelRow {
                kind: "account".into(),
                group_kind: "".into(),
                account_type: account.account_type.clone().into(),
                id: id_i32(account.id.0),
                label: account_label(full, language).into(),
                secondary: secondary.into(),
                detail: detail.into(),
                name: "".into(),
                delete_text: "".into(),
                selected: state.selection == AccountSelection::Account(account.id),
                collapsed: false,
                new_count: i32::try_from(account.new_count).unwrap_or(i32::MAX),
                totals: ModelRc::new(VecModel::from(vec![AccountAmount {
                    text: format::money(account.balance, language, false).into(),
                    tone: tone_code(account_grouping::balance_tone(
                        &group.accounts,
                        account.balance.currency,
                    ))
                    .into(),
                }])),
                indent: true,
            });
        }
    }
    rows
}

fn panel_shown_text(state: &TransactionsState, language: Language) -> String {
    if state.account_filter_text.trim().is_empty() {
        return String::new();
    }
    let shown = state
        .accounts
        .iter()
        .filter(|account| {
            account_panel::matches_filter(
                account.name.as_deref(),
                &identifier(account),
                &account.bank_name,
                &state.account_filter_text,
            )
        })
        .count();
    i18n::format(
        language,
        Key::AccountsShown,
        &[Arg::from(shown), Arg::from(state.accounts.len())],
    )
}

fn amounts(
    totals: &[(Currency, i64)],
    accounts: &[GroupingAccount],
    language: Language,
    plain: bool,
) -> ModelRc<AccountAmount> {
    ModelRc::new(VecModel::from(
        totals
            .iter()
            .map(|(currency, minor)| AccountAmount {
                text: format::money(
                    Money {
                        minor: *minor,
                        currency: *currency,
                    },
                    language,
                    false,
                )
                .into(),
                tone: if plain {
                    "plain".to_owned()
                } else {
                    tone_code(account_grouping::balance_tone(accounts, *currency)).to_owned()
                }
                .into(),
            })
            .collect::<Vec<_>>(),
    ))
}

fn amount_rows(totals: &[(Currency, i64)], tone: &str, language: Language) -> Vec<AccountAmount> {
    totals
        .iter()
        .map(|(currency, minor)| AccountAmount {
            text: format::money(
                Money {
                    minor: *minor,
                    currency: *currency,
                },
                language,
                false,
            )
            .into(),
            tone: tone.to_owned().into(),
        })
        .collect()
}

fn amount_label(state: &TransactionsState, language: Language) -> String {
    match (state.min_amount, state.max_amount) {
        (Some(min), Some(max)) => format!(
            "{}–{}",
            format::money(min, language, false),
            format::money(max, language, false)
        ),
        (Some(min), None) => format!("≥ {}", format::money(min, language, false)),
        (None, Some(max)) => format!("≤ {}", format::money(max, language, false)),
        (None, None) => i18n::text(language, Key::AmountFilterAny).to_owned(),
    }
}

fn period_options(language: Language) -> Vec<SharedString> {
    [
        Key::PeriodThisMonth,
        Key::PeriodLastMonth,
        Key::PeriodLast3Months,
        Key::PeriodThisYear,
        Key::PeriodLastYear,
        Key::PeriodAllTime,
    ]
    .into_iter()
    .map(|key| i18n::text(language, key).into())
    .collect()
}

fn period_at(index: i32) -> Option<Period> {
    match index {
        0 => Some(Period::ThisMonth),
        1 => Some(Period::LastMonth),
        2 => Some(Period::Last3Months),
        3 => Some(Period::ThisYear),
        4 => Some(Period::LastYear),
        5 => Some(Period::All),
        _ => None,
    }
}

fn period_index(period: Period) -> i32 {
    match period {
        Period::ThisMonth => 0,
        Period::LastMonth => 1,
        Period::Last3Months => 2,
        Period::ThisYear => 3,
        Period::LastYear => 4,
        Period::All => 5,
    }
}

fn period_extra(state: &TransactionsState, language: Language) -> String {
    if let Some(month) = state.month {
        let name = month_name(language, month.month);
        let mut characters = name.chars();
        let capitalized = match characters.next() {
            Some(first) => first.to_uppercase().collect::<String>() + characters.as_str(),
            None => String::new(),
        };
        return format!("{capitalized} {}", month.year);
    }
    state.custom_range.map_or_else(String::new, |(from, to)| {
        format!("{} – {}", format::date(from), format::date(to))
    })
}

fn direction_options(language: Language) -> Vec<SharedString> {
    [Key::DirectionAnyLabel, Key::DirectionIncome, Key::DirectionExpenses]
        .into_iter()
        .map(|key| i18n::text(language, key).into())
        .collect()
}

fn direction_index(direction: Direction) -> i32 {
    match direction {
        Direction::All => 0,
        Direction::Income => 1,
        Direction::Expenses => 2,
    }
}

fn category_filter_options(state: &TransactionsState, language: Language) -> Vec<SharedString> {
    let mut options = vec![
        i18n::text(language, Key::AllCategories).into(),
        i18n::text(language, Key::Uncategorized).into(),
    ];
    options.extend(
        state
            .categories
            .iter()
            .map(|category| SharedString::from(category.name.clone())),
    );
    options
}

fn category_filter_index(state: &TransactionsState) -> i32 {
    match state.category_filter {
        CategoryFilter::Any => 0,
        CategoryFilter::None => 1,
        CategoryFilter::Only(id) => state
            .categories
            .iter()
            .position(|category| category.id == id)
            .map_or(0, |index| i32::try_from(index + 2).unwrap_or(0)),
    }
}

fn category_names(state: &TransactionsState, language: Language) -> Vec<SharedString> {
    let mut names = vec![i18n::text(language, Key::Uncategorized).into()];
    names.extend(state.categories.iter().map(|category| {
        if category.is_tax_relevant {
            format!("{} ({})", category.name, i18n::text(language, Key::Tax)).into()
        } else {
            SharedString::from(category.name.clone())
        }
    }));
    names
}

fn add_category_names(state: &TransactionsState, language: Language) -> Vec<SharedString> {
    let mut names = vec![i18n::text(language, Key::AutoRules).into()];
    names.extend(
        state
            .categories
            .iter()
            .map(|category| SharedString::from(category.name.clone())),
    );
    names
}

fn tax_options(language: Language) -> Vec<SharedString> {
    [Key::TaxAutomatic, Key::TaxRelevantChoice, Key::TaxNotRelevantChoice]
        .into_iter()
        .map(|key| i18n::text(language, key).into())
        .collect()
}

fn tax_state_line(transaction: &TransactionItem, language: Language) -> String {
    let base = if transaction.is_tax_relevant {
        i18n::text(language, Key::TaxRelevantChoice)
    } else {
        i18n::text(language, Key::TaxNotRelevantChoice)
    };
    match transaction.tax_source {
        TaxSource::Manual => i18n::format(language, Key::TaxSetByYou, &[Arg::from(base)]),
        TaxSource::Rule => i18n::format(
            language,
            Key::TaxFromRule,
            &[
                Arg::from(base),
                Arg::from(transaction.tax_rule_pattern.clone().unwrap_or_default()),
            ],
        ),
        TaxSource::Category => i18n::format(
            language,
            Key::TaxFromCategory,
            &[
                Arg::from(base),
                Arg::from(
                    transaction
                        .category_name
                        .clone()
                        .unwrap_or_else(|| i18n::text(language, Key::Uncategorized).to_owned()),
                ),
            ],
        ),
        TaxSource::None => base.to_owned(),
    }
}

fn balance_mismatch_text(language: Language, bank: Money, difference: Money) -> String {
    i18n::format(
        language,
        Key::BalanceBankMismatch,
        &[
            Arg::from(format::money(bank, language, false)),
            Arg::from(format::money(
                Money {
                    minor: -difference.minor,
                    currency: difference.currency,
                },
                language,
                true,
            )),
        ],
    )
}

fn account_choices(state: &TransactionsState, language: Language) -> Vec<(AccountSelection, String)> {
    let mut choices = vec![(AccountSelection::All, i18n::text(language, Key::AllAccounts).to_owned())];
    for group in &state.groups {
        let label = group.name.clone().unwrap_or_else(|| match &group.key {
            GroupKey::Type(account_type) => account_type_label(account_type, language),
            GroupKey::Custom(id) => id.0.to_string(),
        });
        choices.push((AccountSelection::Group(group.key.clone()), label));
    }
    for account in &state.accounts {
        choices.push((AccountSelection::Account(account.id), account_label(account, language)));
    }
    choices
}

fn selection_choices(state: &TransactionsState) -> Vec<AccountSelection> {
    let mut choices = vec![AccountSelection::All];
    choices.extend(
        state
            .groups
            .iter()
            .map(|group| AccountSelection::Group(group.key.clone())),
    );
    choices.extend(
        state
            .accounts
            .iter()
            .map(|account| AccountSelection::Account(account.id)),
    );
    choices
}

struct ChartData {
    line: String,
    area: String,
    axis_top: String,
    axis_bottom: String,
    axis_start: String,
    axis_mid: String,
    axis_end: String,
    value: String,
    not_from_zero: bool,
}

fn chart_data(points: &[(Date, Money)], current: Option<Money>, language: Language) -> Option<ChartData> {
    if points.len() < 2 {
        return None;
    }
    let low = points.iter().map(|(_, money)| money.minor).min()?;
    let high = points.iter().map(|(_, money)| money.minor).max()?;
    let currency = points[0].1.currency;
    let span = high - low;
    let mut line = String::new();
    for (index, (_, money)) in points.iter().enumerate() {
        let x = index as f32 / (points.len() - 1) as f32 * CHART_WIDTH;
        let ratio = if span == 0 {
            0.5
        } else {
            (money.minor - low) as f32 / span as f32
        };
        let y = CHART_PADDING + (1.0 - ratio) * (CHART_HEIGHT - 2.0 * CHART_PADDING);
        if index == 0 {
            line.push_str(&format!("M {x:.2} {y:.2}"));
        } else {
            line.push_str(&format!(" L {x:.2} {y:.2}"));
        }
    }
    let area = format!("{line} L {CHART_WIDTH:.2} {CHART_HEIGHT:.2} L 0 {CHART_HEIGHT:.2} Z");
    let middle = points.len() / 2;
    Some(ChartData {
        line,
        area,
        axis_top: format::whole_money(Money { minor: high, currency }, language),
        axis_bottom: format::whole_money(Money { minor: low, currency }, language),
        axis_start: format::date(points[0].0),
        axis_mid: format::date(points[middle].0),
        axis_end: format::date(points[points.len() - 1].0),
        value: current.map_or_else(String::new, |balance| format::money(balance, language, false)),
        not_from_zero: low > 0 || high < 0,
    })
}

fn parse_bound(text: &str, currency: Currency) -> Option<Money> {
    let text = text.trim();
    if text.is_empty() {
        return None;
    }
    money::parse_decimal(text, currency).ok().map(|money| Money {
        minor: money.minor.abs(),
        currency,
    })
}

fn selection_from(kind: &str, group_kind: &str, id: i32, account_type: &str) -> Option<AccountSelection> {
    match kind {
        "all" => Some(AccountSelection::All),
        "group" if group_kind == "custom" => {
            Some(AccountSelection::Group(GroupKey::Custom(AccountGroupId(i64::from(id)))))
        }
        "group" => Some(AccountSelection::Group(GroupKey::Type(account_type.to_owned()))),
        "account" => Some(AccountSelection::Account(AccountId(i64::from(id)))),
        _ => None,
    }
}

fn grouping(account: &AccountItem) -> GroupingAccount {
    GroupingAccount {
        id: account.id,
        name: account.name.clone(),
        currency: account.currency,
        balance: account.balance,
        account_type: account.account_type.clone(),
        group_id: account.group_id,
        new_count: account.new_count,
    }
}

fn account_label(account: &AccountItem, language: Language) -> String {
    if let Some(name) = account.name.as_deref().map(str::trim).filter(|name| !name.is_empty()) {
        return name.to_owned();
    }
    let account_type = account_type_label(&account.account_type, language);
    match digits(account) {
        Some(digits) => format!("{account_type} …{digits}"),
        None => account_type,
    }
}

fn account_type_key(account_type: &str) -> Option<Key> {
    match account_type {
        "checking" => Some(Key::TypeChecking),
        "savings" => Some(Key::TypeSavings),
        "credit_card" => Some(Key::TypeCreditCard),
        LOAN_TYPE => Some(Key::TypeLoan),
        "depot" => Some(Key::TypeDepot),
        _ => None,
    }
}

fn account_type_label(account_type: &str, language: Language) -> String {
    account_type_key(account_type)
        .map(|key| i18n::text(language, key).to_owned())
        .unwrap_or_else(|| account_type.to_owned())
}

fn digits(account: &AccountItem) -> Option<String> {
    let source = if !account.iban.trim().is_empty() {
        account.iban.trim()
    } else {
        account.account_number.as_deref().unwrap_or("").trim()
    };
    if source.is_empty() {
        return None;
    }
    let count = source.chars().count();
    Some(source.chars().skip(count.saturating_sub(4)).collect())
}

fn identifier(account: &AccountItem) -> String {
    if !account.iban.trim().is_empty() {
        account_ids::display(account.iban.trim())
    } else {
        account
            .account_number
            .as_deref()
            .map(str::trim)
            .filter(|number| !number.is_empty())
            .map(str::to_owned)
            .unwrap_or_default()
    }
}

/// The name a selected file shows in the import dialog.
fn display_name(file: &std::path::Path) -> String {
    file.file_name().map_or_else(
        || file.to_string_lossy().into_owned(),
        |name| name.to_string_lossy().into_owned(),
    )
}

fn documents_dir(paths: &AppPaths, profile: &str) -> std::path::PathBuf {
    paths.profile_dir(profile).map_or_else(
        |_| paths.root().join(profile).join("documents"),
        |dir| dir.join("documents"),
    )
}

fn tone_code(tone: BalanceTone) -> &'static str {
    match tone {
        BalanceTone::Positive => "positive",
        BalanceTone::Negative => "negative",
        BalanceTone::Loan => "loan",
        BalanceTone::Zero => "zero",
    }
}

fn color(hex: Option<&str>) -> Color {
    let Some(hex) = hex else {
        return Color::from_rgb_u8(158, 158, 158);
    };
    let text = hex.trim().trim_start_matches('#');
    if text.len() == 6
        && let Ok(value) = u32::from_str_radix(text, 16)
    {
        return Color::from_rgb_u8(
            ((value >> 16) & 0xFF) as u8,
            ((value >> 8) & 0xFF) as u8,
            (value & 0xFF) as u8,
        );
    }
    Color::from_rgb_u8(158, 158, 158)
}

fn string_model(values: Vec<SharedString>) -> ModelRc<SharedString> {
    ModelRc::new(VecModel::from(values))
}

fn id_i32(id: i64) -> i32 {
    i32::try_from(id).unwrap_or(-1)
}

#[cfg(test)]
mod tests {
    use super::*;
    use finglass_core::app::transactions::{ImportEntry, ImportFailure, ReapplyChange, ReapplyGroup};
    use finglass_core::import::{ImportedBooking, ParsedStatement, StatementFormat};
    use finglass_core::model::{Category, CategoryId, TransactionId};
    use slint::Model as _;
    use std::path::PathBuf;

    fn money(minor: i64, currency: Currency) -> Money {
        Money { minor, currency }
    }

    fn transaction(id: i64, date: Date, minor: i64, currency: Currency, new: bool) -> TransactionItem {
        TransactionItem {
            id: TransactionId(id),
            account_id: AccountId(1),
            account_iban: Some("DE89370400440532013000".to_owned()),
            date,
            amount: money(minor, currency),
            balance_after: None,
            currency,
            partner_name: Some(format!("Partner {id}")),
            purpose: Some("Purpose".to_owned()),
            category_id: None,
            category_name: None,
            category_color: None,
            is_tax_relevant: false,
            document_count: 0,
            is_new: new,
            is_transfer: false,
            tax_source: TaxSource::None,
            tax_rule_pattern: None,
            tax_override: None,
        }
    }

    fn pending(date: Date, minor: i64) -> PendingBookingItem {
        PendingBookingItem {
            account_id: AccountId(1),
            date,
            value_date: None,
            amount: money(minor, Currency::EUR),
            partner_name: Some("Pending partner".to_owned()),
            purpose: Some("Pending purpose".to_owned()),
            fetched_at: time::macros::datetime!(2026 - 11 - 07 12:00 UTC),
        }
    }

    #[test]
    fn rows_put_pending_lines_first_and_keep_the_list_order() {
        let mut state = TransactionsState::new(Language::En);
        state.transactions = vec![
            transaction(2, time::macros::date!(2026 - 11 - 06), -100, Currency::EUR, false),
            transaction(1, time::macros::date!(2026 - 11 - 05), 200, Currency::EUR, false),
        ];
        state.pending_bookings = vec![pending(time::macros::date!(2026 - 11 - 07), -50)];

        let rows = transaction_rows(&state, Language::En);

        assert_eq!(rows.len(), 3);
        assert!(rows[0].pending);
        assert_eq!(rows[0].title, "Pending partner");
        assert_eq!(rows[0].amount_text, "-€0.50");
        assert_eq!(rows[1].title, "Partner 2");
        assert_eq!(rows[2].title, "Partner 1");
    }

    #[test]
    fn bold_rows_come_from_the_state_bold_ids_not_the_row_flag() {
        let mut state = TransactionsState::new(Language::En);
        state.transactions = vec![
            transaction(1, time::macros::date!(2026 - 11 - 05), -100, Currency::EUR, false),
            transaction(2, time::macros::date!(2026 - 11 - 04), -100, Currency::EUR, true),
        ];
        state.bold_ids.insert(TransactionId(1));

        let rows = transaction_rows(&state, Language::En);

        assert!(rows[0].is_new, "the state's bold set marks the row");
        assert!(!rows[1].is_new, "the row's own flag is not used for display");
    }

    #[test]
    fn per_currency_totals_keep_the_primary_first_order() {
        let usd = Currency::new("USD").unwrap();
        let rows = amount_rows(&[(Currency::EUR, 1_000), (usd, 500)], "positive", Language::En);

        assert_eq!(rows[0].text, "€10.00");
        assert_eq!(rows[1].text, "$5.00");
    }

    #[test]
    fn category_and_tax_indexes_map_to_the_core_values() {
        let mut state = TransactionsState::new(Language::En);
        state.categories = vec![Category {
            id: CategoryId(7),
            name: "Food".to_owned(),
            color: "#FF0000".to_owned(),
            budget_limit: None,
            is_tax_relevant: true,
            is_transfer: false,
        }];

        assert_eq!(category_filter_index(&state), 0);
        assert_eq!(category_names(&state, Language::En)[1], "Food (tax)");
        assert_eq!(add_category_names(&state, Language::En)[0], "Auto (rules)");
    }

    #[test]
    fn pending_rows_have_no_balance_or_selection() {
        let mut state = TransactionsState::new(Language::En);
        state.pending_bookings = vec![pending(time::macros::date!(2026 - 11 - 07), -50)];
        state.balances = Some(std::collections::BTreeMap::new());
        state.selected_ids.insert(TransactionId(1));

        let rows = transaction_rows(&state, Language::En);

        assert!(rows[0].pending);
        assert_eq!(rows[0].balance_text, "");
        assert!(!rows[0].selected);
        assert!(!rows[0].balance_warning);
    }

    #[test]
    fn a_search_typed_during_a_job_is_kept_as_the_latest_and_consumed_afterwards() {
        let mut pending = PendingSearch::default();

        pending.defer("first");
        pending.defer("second");
        assert_eq!(pending.take().as_deref(), Some("second"));
        assert_eq!(pending.take(), None);

        pending.defer("old");
        let old = pending.take().expect("the old query");
        pending.defer("new");
        pending.restore(old);
        assert_eq!(pending.take().as_deref(), Some("new"), "a newer query wins");
    }

    #[test]
    fn the_chart_path_uses_the_state_points_and_reports_a_non_zero_scale() {
        let points = vec![
            (time::macros::date!(2026 - 11 - 01), money(100, Currency::EUR)),
            (time::macros::date!(2026 - 11 - 02), money(300, Currency::EUR)),
        ];
        let chart = chart_data(&points, Some(money(300, Currency::EUR)), Language::En).unwrap();

        assert!(chart.line.starts_with("M 0.00"));
        assert!(chart.line.contains("L 700.00"));
        assert!(chart.not_from_zero);
        assert_eq!(chart.value, "€3.00");
        assert_eq!(chart.axis_start, "01.11.2026");
        assert_eq!(chart.axis_end, "02.11.2026");
    }

    fn imported(date: Date, minor: i64, partner: &str) -> ImportedBooking {
        ImportedBooking {
            date,
            value_date: None,
            amount: rust_decimal::Decimal::new(minor, 2),
            currency: Some("EUR".to_owned()),
            partner_name: Some(partner.to_owned()),
            partner_iban: None,
            purpose: Some("Purpose".to_owned()),
            balance_after: None,
            bank_reference: None,
        }
    }

    fn preview_with(entries: Vec<ImportEntry>, failures: Vec<ImportFailure>) -> ImportPreview {
        ImportPreview {
            files: entries.iter().map(|entry| entry.file.clone()).collect(),
            entries,
            failures,
            account_id: Some(AccountId(1)),
            balance_update_allowed: false,
        }
    }

    #[test]
    fn import_file_rows_show_each_file_period_warning_and_failure() {
        let csv = CsvLayout {
            delimiter: ';',
            charset_name: "UTF-8".to_owned(),
            header_line: 1,
            headers: vec!["Buchung".to_owned(), "Betrag".to_owned()],
            mapping: CsvMapping {
                date: Some(0),
                amount: Some(1),
                ..CsvMapping::default()
            },
        };
        let statement = ParsedStatement {
            format: StatementFormat::Csv,
            bookings: vec![
                imported(time::macros::date!(2026 - 10 - 02), -2_000, "Stadtwerke"),
                imported(time::macros::date!(2026 - 10 - 15), -3_000, "Obststand"),
            ],
            account_iban: Some("DE89370400440532013000".to_owned()),
            accounts: vec!["DE89370400440532013000".to_owned()],
            skipped: vec!["line 3".to_owned()],
            pending_skipped: 2,
            csv: Some(csv),
            format_detail: None,
            closing_balance: None,
        };
        let preview = preview_with(
            vec![ImportEntry {
                file: PathBuf::from("/tmp/oktober.csv"),
                statement,
            }],
            vec![ImportFailure {
                file_name: "kaputt.csv".to_owned(),
                message: "Unreadable".to_owned(),
            }],
        );
        let rows = import_file_rows(&preview, Language::En, None);

        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0].name, "oktober.csv");
        assert_eq!(rows[0].count_text, "2 bookings");
        assert_eq!(rows[0].period_text, "02.10.2026–15.10.2026");
        assert!(rows[0].warning_text.contains("2 pending left out"));
        assert!(rows[0].warning_text.contains("1 unreadable"));
        assert!(!rows[0].failure);
        assert!(rows[1].failure);
        assert_eq!(rows[1].warning_text, "Unreadable");
        assert!(!import_has_balances(&preview));
    }

    #[test]
    fn import_csv_fields_use_the_headers_and_one_based_indices() {
        let csv = CsvLayout {
            delimiter: ';',
            charset_name: "UTF-8".to_owned(),
            header_line: 1,
            headers: vec!["Buchung".to_owned(), "".to_owned(), "Betrag".to_owned()],
            mapping: CsvMapping {
                date: Some(0),
                amount: Some(2),
                ..CsvMapping::default()
            },
        };
        let statement = ParsedStatement {
            format: StatementFormat::Csv,
            bookings: vec![imported(time::macros::date!(2026 - 10 - 02), -2_000, "Stadtwerke")],
            account_iban: None,
            accounts: Vec::new(),
            skipped: Vec::new(),
            pending_skipped: 0,
            csv: Some(csv),
            format_detail: None,
            closing_balance: None,
        };
        let preview = preview_with(
            vec![ImportEntry {
                file: PathBuf::from("/tmp/a.csv"),
                statement,
            }],
            Vec::new(),
        );
        let fields = import_csv_fields(&preview, Language::En);

        assert_eq!(fields.len(), 7);
        assert_eq!(fields[0].field, "date");
        assert_eq!(fields[0].index, 1);
        let options = fields[0]
            .options
            .iter()
            .map(|option| option.to_string())
            .collect::<Vec<_>>();
        assert_eq!(options[0], "— none —");
        assert_eq!(options[1], "Buchung");
        assert_eq!(options[2], "Column 2");
        assert_eq!(fields[1].field, "amount");
        assert_eq!(fields[1].index, 3);
    }

    #[test]
    fn reapply_group_rows_keep_the_tick_state_and_format_the_sum() {
        let preview = ReapplyPreview {
            changes: vec![ReapplyChange {
                transaction_id: TransactionId(1),
                from_category_id: None,
                to_category_id: CategoryId(7),
            }],
            groups: vec![ReapplyGroup {
                from_category_id: None,
                from_name: "Uncategorized".to_owned(),
                to_category_id: CategoryId(7),
                to_name: "Food".to_owned(),
                count: 2,
                sum: Some(money(-4_200, Currency::EUR)),
                currency: Currency::EUR,
            }],
            shown_count: 3,
        };
        let excluded = BTreeSet::from([(None, CategoryId(7))]);
        let rows = reapply_group_rows(&preview, &excluded, Language::En);

        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].title, "Uncategorized → Food");
        assert_eq!(rows[0].detail, "2 · -€42.00");
        assert!(!rows[0].ticked);
        assert_eq!(rows[0].from_id, -1);
        assert_eq!(rows[0].to_id, 7);
        assert_eq!(reapply_key(-1, 7), (None, CategoryId(7)));
    }

    #[test]
    fn remapping_one_csv_field_clears_the_amount_alternatives() {
        let mapping = CsvMapping {
            amount: Some(2),
            ..CsvMapping::default()
        };
        let debit = remap_mapping(&mapping, "debit", 4);
        assert_eq!(debit.debit, Some(3));
        assert_eq!(debit.amount, None);

        let amount = remap_mapping(&debit, "amount", 2);
        assert_eq!(amount.amount, Some(1));
        assert_eq!(amount.debit, None);
        assert_eq!(amount.credit, None);

        let none = remap_mapping(&mapping, "amount", 0);
        assert_eq!(none.amount, None);
    }
}
