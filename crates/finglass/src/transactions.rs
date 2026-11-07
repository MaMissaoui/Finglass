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
    CategoryFilter, ManualTransaction, Period, SEARCH_DEBOUNCE_MS, StartArgs, TransactionsState, TransactionsView,
};
use finglass_core::bank_text;
use finglass_core::db::Database;
use finglass_core::documents::{DocumentService, OcrService};
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{
    AccountGroupId, AccountId, Direction, PendingBookingItem, TaxSource, TransactionId, TransactionItem,
};
use finglass_core::money::{self, Currencies, Currency, Money};
use finglass_core::paths::AppPaths;
use finglass_core::report::format::month_name;
use finglass_core::rules::payment_providers::PaymentProviders;
use slint::{Color, ModelRc, SharedString, Timer, TimerMode, VecModel, Weak};
use time::{Date, OffsetDateTime};

use crate::{AccountAmount, AccountGroupChoice, AccountPanelRow, MainWindow, TransactionRow};

/// The balance chart is drawn in this logical box and scaled by its viewbox.
const CHART_WIDTH: f32 = 700.0;
/// Height of the balance chart's drawing box.
const CHART_HEIGHT: f32 = 200.0;
/// Padding inside the balance chart.
const CHART_PADDING: f32 = 8.0;

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
    /// The open profile's slug key, for its `documents/` folder.
    profile: String,
    db: Mutex<Option<Arc<Database>>>,
    state: Mutex<TransactionsState>,
    busy: Busy,
    pending_search: Mutex<PendingSearch>,
    multi_delete_ids: Mutex<BTreeSet<TransactionId>>,
}

impl TransactionsController {
    /// Builds a controller for one main window and data folder.
    #[must_use]
    pub fn new(ui: Weak<MainWindow>, paths: Arc<AppPaths>, profile: impl Into<String>) -> Self {
        Self {
            ui,
            paths,
            profile: profile.into(),
            db: Mutex::new(None),
            state: Mutex::new(TransactionsState::new(Language::En)),
            busy: Busy::default(),
            pending_search: Mutex::new(PendingSearch::default()),
            multi_delete_ids: Mutex::new(BTreeSet::new()),
        }
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
                let documents = documents_dir(&this.paths, &this.profile);
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
        let mut state = self.state.lock().expect("transactions state poisoned");
        *state = TransactionsState::new(language);
        let snapshot = state.clone();
        drop(state);
        self.publish(snapshot);
    }

    /// Clears the page, for when the profile is locked.
    pub fn reset(&self) {
        *self.db.lock().expect("transactions database poisoned") = None;
        let language = self.state.lock().expect("transactions state poisoned").language;
        let mut state = self.state.lock().expect("transactions state poisoned");
        *state = TransactionsState::new(language);
        let snapshot = state.clone();
        drop(state);
        self.publish(snapshot);
    }

    /// Applies a language change to the page's own texts.
    pub fn set_language(&self, language: Language) {
        let mut state = self.state.lock().expect("transactions state poisoned");
        state.language = language;
        let snapshot = state.clone();
        drop(state);
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
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let initial = state
            .custom_range
            .or_else(|| state.month.map(|month| (month.start(), month.end())))
            .or_else(|| {
                let (from, to) = state.period.range(today);
                from.zip(to)
            })
            .unwrap_or_else(|| {
                let year = Date::from_calendar_date(today.year(), time::Month::January, 1).expect("valid date");
                (year, today)
            });
        drop(state);
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
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let currency = state.primary_currency;
        let language = state.language;
        ui.set_transactions_amount_min(state.min_amount.map(money::format_plain).unwrap_or_default().into());
        ui.set_transactions_amount_max(state.max_amount.map(money::format_plain).unwrap_or_default().into());
        drop(state);
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
            .state
            .try_lock()
            .map_or(Currencies::DEFAULT, |state| state.primary_currency);
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
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let Some(transaction) = state.transactions.iter().find(|transaction| transaction.id == id) else {
            return;
        };
        let language = state.language;
        let text = format!(
            "{} {} {}",
            format::date(transaction.date),
            bank_text::display_description(transaction),
            format::money(transaction.amount, language, false),
        );
        drop(state);
        ui.set_transactions_delete_id(i32::try_from(id.0).unwrap_or(-1));
        ui.set_transactions_delete_title(i18n::text(language, Key::DeleteTransactionTitle).into());
        ui.set_transactions_delete_text(text.into());
        ui.set_transactions_delete_open(true);
    }

    fn open_multi_delete(&self) {
        let Some(ui) = self.ui.upgrade() else {
            return;
        };
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let language = state.language;
        let bookings = state.selected_shown();
        if bookings.is_empty() {
            return;
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
        drop(state);
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
        let Ok(state) = self.state.try_lock() else {
            return;
        };
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
        drop(state);
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
        let (account_id, currency) = {
            let Ok(state) = self.state.try_lock() else {
                return;
            };
            let Some(account) = state
                .accounts
                .get(usize::try_from(account_index).unwrap_or(0))
                .or_else(|| state.accounts.first())
            else {
                return;
            };
            (account.id, account.currency)
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
        let transaction = {
            let Ok(state) = self.state.try_lock() else {
                return;
            };
            state
                .transactions
                .iter()
                .find(|transaction| transaction.id == id)
                .cloned()
        };
        let Some(transaction) = transaction else {
            return;
        };
        let documents = documents_dir(&self.paths, &self.profile);
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
        let Ok(state) = self.state.try_lock() else {
            return false;
        };
        let mut view = TransactionsView::new(state.language);
        *view.state_mut() = state.clone();
        drop(state);

        self.busy.begin();
        let this = Arc::clone(self);
        let busy = self.busy.clone();
        thread::spawn(move || {
            let guard = BusyGuard(busy);
            action(&mut view, &db);
            let snapshot = view.state().clone();
            this.publish(snapshot);
            drop(guard);
            this.run_pending_search();
        });
        true
    }

    fn update_view(&self, action: impl FnOnce(&mut TransactionsView)) {
        let Ok(state) = self.state.try_lock() else {
            return;
        };
        let mut view = TransactionsView::new(state.language);
        *view.state_mut() = state.clone();
        drop(state);
        action(&mut view);
        self.publish(view.state().clone());
    }

    fn publish(&self, state: TransactionsState) {
        {
            let mut current = self.state.lock().expect("transactions state poisoned");
            *current = state.clone();
        }
        let ui = self.ui.clone();
        let _ = slint::invoke_from_event_loop(move || {
            if let Some(ui) = ui.upgrade() {
                apply(&ui, &state);
            }
        });
    }

    fn database(&self) -> Option<Arc<Database>> {
        self.db.lock().expect("transactions database poisoned").clone()
    }

    fn language(&self) -> Language {
        self.state.try_lock().map_or(Language::En, |state| state.language)
    }

    fn selected_id(&self) -> Option<TransactionId> {
        self.state
            .try_lock()
            .ok()
            .and_then(|state| state.selected.as_ref().map(|transaction| transaction.id))
    }

    fn category_filter_at(&self, index: i32) -> Option<CategoryFilter> {
        match index {
            0 => Some(CategoryFilter::Any),
            1 => Some(CategoryFilter::None),
            _ => {
                let state = self.state.try_lock().ok()?;
                let category = state.categories.get(usize::try_from(index - 2).ok()?)?;
                Some(CategoryFilter::Only(category.id))
            }
        }
    }

    fn category_id_at(&self, index: i32) -> Option<finglass_core::model::CategoryId> {
        if index <= 0 {
            return None;
        }
        self.state.try_lock().ok().and_then(|state| {
            state
                .categories
                .get(usize::try_from(index - 1).ok()?)
                .map(|category| category.id)
        })
    }

    fn selection_at(&self, index: i32) -> Option<AccountSelection> {
        let state = self.state.try_lock().ok()?;
        let choices = selection_choices(&state);
        choices.get(usize::try_from(index).ok()?).cloned()
    }
}

/// Applies `state` to the window without touching the database.
pub fn apply(main: &MainWindow, state: &TransactionsState) {
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
    use finglass_core::model::{Category, CategoryId, TransactionId};

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
}
