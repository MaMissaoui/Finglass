//! The Reports page controller.
//!
//! It owns the UI-free [`ReportsState`] and maps it to the Slint models. Every database action, file
//! dialog and export runs on a background thread; results come back through
//! `slint::invoke_from_event_loop`, like the rest of the shell.

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::thread;

use finglass_core::app::reports::ReportsState;
use finglass_core::dashboard::YearMonth;
use finglass_core::db::Database;
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{TaxSource, TransactionItem};
use finglass_core::money::{Currency, Money};
use finglass_core::recurring::{Cadence, RecurringPayment};
use finglass_core::report::format::month_name;
use finglass_core::report::{self, TaxBookings, TaxGroup, TaxReport};
use slint::{ModelRc, VecModel, Weak};
use time::{Date, OffsetDateTime};

use crate::{
    DashboardCategoryRow, MainWindow, ReportAmountRow, ReportMonthlyRow, ReportRecurringRow, ReportSuggestionRow,
    ReportTaxRow, ReportVendorRow,
};

/// The four files the Reports page writes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ExportFile {
    /// The tax summary PDF.
    ReportPdf,
    /// The transaction CSV.
    Csv,
    /// The tax bookings workbook.
    TaxXlsx,
    /// The tax bookings PDF.
    TaxPdf,
}

/// Owns the Reports page state and keeps the Slint window in sync with it.
pub struct ReportsController {
    ui: Weak<MainWindow>,
    db: Mutex<Option<Arc<Database>>>,
    state: Mutex<ReportsState>,
    jobs: crate::jobs::JobTracker,
}

impl ReportsController {
    /// Builds a controller for one main window.
    #[must_use]
    pub(crate) fn new(ui: Weak<MainWindow>, jobs: crate::jobs::JobTracker) -> Self {
        Self {
            ui,
            db: Mutex::new(None),
            state: Mutex::new(ReportsState::new(Language::En, OffsetDateTime::now_utc().date())),
            jobs,
        }
    }

    /// Installs the page's callbacks on the main window.
    pub fn wire(self: &Arc<Self>, main: &MainWindow) {
        {
            let this = Arc::clone(self);
            main.on_reports_set_custom(move |custom| {
                this.clear_range_error();
                if !custom {
                    this.change_year(0);
                }
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_change_year(move |delta| this.change_year(delta));
        }

        {
            let this = Arc::clone(self);
            main.on_reports_apply_range(move |from, to| this.apply_range(&from, &to));
        }

        {
            let this = Arc::clone(self);
            main.on_reports_export_report_pdf(move || {
                this.spawn_export(Key::ExportReport, ExportFile::ReportPdf, |state, db, path| {
                    state.export_pdf(db, path, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_export_csv(move || {
                this.spawn_export(Key::ExportTransactions, ExportFile::Csv, |state, db, path| {
                    state.export_csv(db, path);
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_export_tax_xlsx(move || {
                this.spawn_export(Key::ExportExcel, ExportFile::TaxXlsx, |state, db, path| {
                    state.export_tax_xlsx(db, path, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_export_tax_pdf(move || {
                this.spawn_export(Key::ExportPdf, ExportFile::TaxPdf, |state, db, path| {
                    state.export_tax_pdf(db, path, OffsetDateTime::now_utc().date());
                });
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_print_tax(move || this.print_tax());
        }

        {
            let this = Arc::clone(self);
            main.on_reports_set_missing_only(move |only| {
                let Ok(mut state) = this.state.try_lock() else {
                    return;
                };
                state.set_missing_receipts_only(only);
                let snapshot = state.clone();
                drop(state);
                this.publish(snapshot);
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_accept_suggestion(move |id| {
                let id = finglass_core::model::TransactionId(i64::from(id));
                this.spawn_action(move |state, db| state.accept_suggestion(db, id));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_dismiss_suggestion(move |id| {
                let id = finglass_core::model::TransactionId(i64::from(id));
                this.spawn_action(move |state, db| state.dismiss_suggestion(db, id));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_mark_not_tax_relevant(move |id| {
                let id = finglass_core::model::TransactionId(i64::from(id));
                this.spawn_action(move |state, db| state.mark_not_tax_relevant(db, id));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_reset_tax_automatic(move |id| {
                let id = finglass_core::model::TransactionId(i64::from(id));
                this.spawn_action(move |state, db| state.reset_tax_automatic(db, id));
            });
        }

        {
            let this = Arc::clone(self);
            main.on_reports_dismiss(move || {
                let Ok(mut state) = this.state.try_lock() else {
                    return;
                };
                state.clear_messages();
                let snapshot = state.clone();
                drop(state);
                this.publish(snapshot);
            });
        }
    }

    /// Makes the controller use `db` and starts from a clean page for a profile.
    pub fn attach(self: &Arc<Self>, db: Arc<Database>, language: Language) {
        *self.db.lock().expect("reports database poisoned") = Some(db);
        let today = OffsetDateTime::now_utc().date();
        let snapshot = {
            let mut state = self.state.lock().expect("reports state poisoned");
            *state = ReportsState::new(language, today);
            state.clone()
        };
        self.publish(snapshot);
        self.set_default_range(today);
    }

    /// Clears the page, for when the profile is locked.
    pub fn reset(&self) {
        *self.db.lock().expect("reports database poisoned") = None;
        let language = self.state.lock().expect("reports state poisoned").language;
        let today = OffsetDateTime::now_utc().date();
        let snapshot = {
            let mut state = self.state.lock().expect("reports state poisoned");
            *state = ReportsState::new(language, today);
            state.clone()
        };
        self.publish(snapshot);
        self.set_default_range(today);
    }

    /// Applies a language change to the page's own texts.
    pub fn set_language(self: &Arc<Self>, language: Language) {
        if let Ok(mut state) = self.state.try_lock() {
            state.language = language;
            let snapshot = state.clone();
            drop(state);
            self.publish(snapshot);
        } else {
            let this = Arc::clone(self);
            let generation = self.jobs.begin();
            thread::spawn(move || {
                let snapshot = {
                    let mut state = this.state.lock().expect("reports state poisoned");
                    state.language = language;
                    state.clone()
                };
                this.publish_job(snapshot, generation);
            });
        }
        self.clear_range_error();
    }

    /// Loads the selected period and the recurring payments when the page opens.
    ///
    /// `tax_year` opens the page on the Tax bookings tab for that year, as the dashboard's tax card
    /// does; `None` keeps the screen's own year and the summary tab.
    pub fn open_page(self: &Arc<Self>, tax_year: Option<i32>) {
        let Some(db) = self.database() else {
            return;
        };
        let today = OffsetDateTime::now_utc().date();

        if let Some(year) = tax_year {
            // Apply the link's year at once when the state is free; the worker applies it again so a
            // long-running load cannot swallow the link.
            if let Ok(mut state) = self.state.try_lock() {
                state.selected_year = year;
                state.custom_range = None;
                let snapshot = state.clone();
                drop(state);
                self.publish(snapshot);
            }
        }

        if tax_year.is_some() || self.state.try_lock().is_ok_and(|state| state.custom_range.is_none()) {
            self.set_default_range(today);
        }

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let snapshot = {
                let mut state = this.state.lock().expect("reports state poisoned");
                if let Some(year) = tax_year {
                    state.selected_year = year;
                    state.custom_range = None;
                }
                if let Some((from, to)) = state.custom_range {
                    state.set_custom_range(&db, from, to);
                } else {
                    let year = state.selected_year;
                    state.update_year(&db, year);
                }
                state.load_recurring(&db, today);
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    fn change_year(self: &Arc<Self>, delta: i32) {
        let Some(db) = self.database() else {
            return;
        };
        let today = OffsetDateTime::now_utc().date();
        self.set_default_range(today);

        let year = {
            // A click while work runs is ignored: the worker holds the state, and the UI thread never waits for it.
            let Ok(mut state) = self.state.try_lock() else {
                return;
            };
            let year = state.selected_year.saturating_add(delta);
            state.custom_range = None;
            state.selected_year = year;
            state.is_loading = true;
            state.error = None;
            state.clone()
        };
        self.publish(year.clone());

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let snapshot = {
                let mut state = this.state.lock().expect("reports state poisoned");
                state.update_year(&db, year.selected_year);
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    fn apply_range(self: &Arc<Self>, from_text: &str, to_text: &str) {
        let Ok(language) = self.state.try_lock().map(|state| state.language) else {
            return;
        };
        let Some(from) = format::parse_date(from_text) else {
            self.set_range_error(i18n::text(language, Key::EnterValidDate));
            return;
        };
        let Some(to) = format::parse_date(to_text) else {
            self.set_range_error(i18n::text(language, Key::EnterValidDate));
            return;
        };
        if from > to {
            self.set_range_error(i18n::text(language, Key::InvalidDateRange));
            return;
        }
        let Some(db) = self.database() else {
            return;
        };

        self.clear_range_error();
        let pending = {
            let Ok(mut state) = self.state.try_lock() else {
                return;
            };
            state.custom_range = Some((from, to));
            state.is_loading = true;
            state.error = None;
            state.clone()
        };
        self.publish(pending);

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let snapshot = {
                let mut state = this.state.lock().expect("reports state poisoned");
                state.set_custom_range(&db, from, to);
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    fn spawn_action<F>(self: &Arc<Self>, action: F)
    where
        F: FnOnce(&mut ReportsState, &Database) + Send + 'static,
    {
        let Some(db) = self.database() else {
            return;
        };
        let pending = {
            let Ok(mut state) = self.state.try_lock() else {
                return;
            };
            state.is_loading = true;
            state.error = None;
            state.clone()
        };
        self.publish(pending);

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let snapshot = {
                let mut state = this.state.lock().expect("reports state poisoned");
                action(&mut state, &db);
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    fn spawn_export<F>(self: &Arc<Self>, title: Key, file: ExportFile, action: F)
    where
        F: FnOnce(&mut ReportsState, &Database, &Path) + Send + 'static,
    {
        let Some(db) = self.database() else {
            return;
        };
        let Ok(mut state) = self.state.try_lock() else {
            return;
        };
        let (language, file_name) = (state.language, default_file_name(&state, file));
        state.is_exporting = true;
        state.error = None;
        state.message = None;
        let pending = state.clone();
        drop(state);
        self.publish(pending);

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            let path = rfd::FileDialog::new()
                .set_title(i18n::text(language, title))
                .set_file_name(&file_name)
                .save_file();
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let snapshot = {
                let mut state = this.state.lock().expect("reports state poisoned");
                match path {
                    Some(path) => action(&mut state, &db, &path),
                    None => state.is_exporting = false,
                }
                state.clone()
            };
            this.publish_job(snapshot, generation);
        });
    }

    fn print_tax(self: &Arc<Self>) {
        let Some(db) = self.database() else {
            return;
        };
        let today = OffsetDateTime::now_utc().date();
        let pending = {
            let Ok(mut state) = self.state.try_lock() else {
                return;
            };
            state.is_exporting = true;
            state.error = None;
            state.message = None;
            state.clone()
        };
        self.publish(pending);

        let this = Arc::clone(self);
        let generation = self.jobs.begin();
        thread::spawn(move || {
            if !this.jobs.is_current(generation) {
                this.jobs.end();
                return;
            }
            let path = {
                let mut state = this.state.lock().expect("reports state poisoned");
                state.print_tax(&db, today)
            };
            if let Some(path) = path {
                let _ = open::that_detached(path);
            }
            let snapshot = this.state.lock().expect("reports state poisoned").clone();
            this.publish_job(snapshot, generation);
        });
    }

    /// One snapshot of the page state, for the shell's category navigation.
    pub fn snapshot(&self) -> Option<ReportsState> {
        self.state.try_lock().ok().map(|state| state.clone())
    }

    fn database(&self) -> Option<Arc<Database>> {
        self.db.lock().expect("reports database poisoned").clone()
    }

    fn publish(&self, state: ReportsState) {
        let ui = self.ui.clone();
        let _ = slint::invoke_from_event_loop(move || {
            if let Some(ui) = ui.upgrade() {
                apply(&ui, &state);
            }
        });
    }

    /// Publishes a worker's result unless the profile has been switched in the meantime.
    fn publish_job(&self, state: ReportsState, generation: u64) {
        if !self.jobs.is_current(generation) {
            self.jobs.end();
            return;
        }
        let ui = self.ui.clone();
        let jobs = self.jobs.clone();
        let _ = slint::invoke_from_event_loop(move || {
            jobs.end();
            if !jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = ui.upgrade() {
                apply(&ui, &state);
            }
        });
    }

    fn set_default_range(&self, today: Date) {
        if let Some(ui) = self.ui.upgrade() {
            let from = YearMonth::from_date(today).minus_months(2).start();
            ui.set_reports_range_from(format::date(from).into());
            ui.set_reports_range_to(format::date(today).into());
        }
    }

    fn set_range_error(&self, text: &str) {
        if let Some(ui) = self.ui.upgrade() {
            ui.set_reports_range_error(text.into());
        }
    }

    fn clear_range_error(&self) {
        self.set_range_error("");
    }
}

/// Writes one state snapshot to the Slint window on the event loop.
pub fn apply(ui: &MainWindow, state: &ReportsState) {
    let language = state.language;
    let report = state.tax_report.as_ref();

    ui.set_reports_loading(state.is_loading);
    ui.set_reports_exporting(state.is_exporting);
    ui.set_reports_error_text(state.error.clone().unwrap_or_default().into());
    ui.set_reports_message_text(state.message.clone().unwrap_or_default().into());
    ui.set_reports_has_report(report.is_some());
    ui.set_reports_empty(report.is_some_and(|report| report.transaction_count == 0));
    ui.set_reports_custom(state.custom_range.is_some());
    ui.set_reports_year_label(state.selected_year.to_string().into());
    ui.set_reports_can_export_report(report.is_some_and(|report| report.transaction_count > 0));
    ui.set_reports_export_disabled_reason(
        report
            .filter(|report| report.transaction_count == 0)
            .map_or_else(String::new, |report| {
                i18n::format(language, Key::NothingToExport, &[Arg::Str(report.title(language))])
            })
            .into(),
    );
    ui.set_reports_can_export_tax(
        state
            .tax_bookings
            .as_ref()
            .is_some_and(|bookings| bookings.booking_count > 0),
    );
    ui.set_reports_linkable(report.is_some_and(|report| linkable(report, OffsetDateTime::now_utc().date())));

    if let Some((from, to)) = state.custom_range {
        ui.set_reports_range_from(format::date(from).into());
        ui.set_reports_range_to(format::date(to).into());
    }

    let (empty_title, empty_hint) = report.map_or_else(
        || (String::new(), String::new()),
        |report| empty_texts(report, language),
    );
    ui.set_reports_empty_title(empty_title.into());
    ui.set_reports_empty_hint(empty_hint.into());

    if let Some(report) = report {
        ui.set_reports_other_currency_hint(other_currency_hint(report, language).into());
        ui.set_reports_income_value(format::money(report.total_income, language, false).into());
        ui.set_reports_expenses_value(format::money(report.total_expenses, language, false).into());
        ui.set_reports_net_value(format::money(report.net_income, language, false).into());
        ui.set_reports_tax_value(format::money(report.tax_relevant_total(), language, false).into());
        ui.set_reports_net_tone(tone(report.net_income).into());
        ui.set_reports_tax_relevant_rows(ModelRc::new(VecModel::from(amount_rows(
            &report.tax_relevant_expenses,
            language,
        ))));
        ui.set_reports_tax_relevant_empty(
            if report.tax_relevant_expenses.is_empty() {
                i18n::text(language, Key::NoTaxRelevantExpenses)
            } else {
                ""
            }
            .into(),
        );
        ui.set_reports_stat_transactions(report.transaction_count.to_string().into());
        ui.set_reports_stat_categories(report.categories_used.to_string().into());
        ui.set_reports_stat_average(format::money(average_monthly(report), language, false).into());
        ui.set_reports_stat_highest(highest_month(report, language).into());
        ui.set_reports_monthly_title(
            i18n::format(
                language,
                Key::MonthlyIncomeExpenses,
                &[Arg::Str(report.title(language))],
            )
            .into(),
        );
        ui.set_reports_monthly_rows(ModelRc::new(VecModel::from(monthly_rows(report, language))));
        ui.set_reports_monthly_total_income(format::money(report.total_income, language, false).into());
        ui.set_reports_monthly_total_expenses(format::money(report.total_expenses, language, false).into());
        ui.set_reports_monthly_total_net(format::money(report.net_income, language, true).into());
        ui.set_reports_monthly_total_tone(tone(report.net_income).into());
        ui.set_reports_monthly_total_income_zero(report.total_income.minor == 0);
        ui.set_reports_monthly_total_expenses_zero(report.total_expenses.minor == 0);
        ui.set_reports_monthly_total_net_zero(report.net_income.minor == 0);

        let (category_rows, transfers_note) = category_rows(report, language);
        ui.set_reports_category_rows(ModelRc::new(VecModel::from(category_rows)));
        ui.set_reports_transfers_note(transfers_note.into());
        ui.set_reports_vendor_rows(ModelRc::new(VecModel::from(vendor_rows(report, language))));
    }

    apply_recurring(ui, state, language);
    apply_tax_bookings(ui, state, language);
}

fn apply_recurring(ui: &MainWindow, state: &ReportsState, language: Language) {
    let payments = &state.recurring;
    let count = payments.iter().filter(|payment| !payment.is_income()).count();
    let totals = recurring_cost_totals(payments, state.primary_currency);

    ui.set_reports_recurring_empty(payments.is_empty());
    ui.set_reports_recurring_count(count.to_string().into());
    ui.set_reports_recurring_yearly(yearly_cost_text(&totals, language).into());
    ui.set_reports_recurring_monthly(monthly_cost_text(&totals, language).into());
    ui.set_reports_recurring_rows(ModelRc::new(VecModel::from(recurring_rows(payments, language))));
}

fn apply_tax_bookings(ui: &MainWindow, state: &ReportsState, language: Language) {
    let bookings = state.tax_bookings.as_ref();
    ui.set_reports_has_tax(bookings.is_some());
    ui.set_reports_missing_receipts_only(state.missing_receipts_only);

    let Some(bookings) = bookings else {
        ui.set_reports_tax_empty(false);
        ui.set_reports_tax_empty_title(String::new().into());
        ui.set_reports_tax_empty_hint(String::new().into());
        ui.set_reports_tax_rows(ModelRc::new(VecModel::from(Vec::<ReportTaxRow>::new())));
        ui.set_reports_tax_suggestions(ModelRc::new(VecModel::from(Vec::<ReportSuggestionRow>::new())));
        return;
    };

    let missing = bookings.booking_count.saturating_sub(bookings.with_receipt());
    let receiving = missing > 0;
    ui.set_reports_tax_empty(bookings.booking_count == 0);
    ui.set_reports_tax_empty_title(
        i18n::format(language, Key::NoTaxBookingsIn, &[Arg::Str(bookings.title(language))]).into(),
    );
    ui.set_reports_tax_empty_hint(i18n::text(language, Key::TaxBookingsHowTo).into());
    ui.set_reports_tax_income_value(format::money(bookings.income_total(), language, false).into());
    ui.set_reports_tax_deduction_value(format::money(bookings.deduction_total(), language, false).into());
    ui.set_reports_tax_net_value(format::money(bookings.net_total(), language, false).into());
    ui.set_reports_tax_net_tone(tone(bookings.net_total()).into());
    ui.set_reports_tax_receipts_missing(receiving);
    ui.set_reports_tax_receipts_value(
        if receiving {
            i18n::format(language, Key::ReceiptsMissing, &[Arg::Int(missing as i64)])
        } else {
            i18n::text(language, Key::ReceiptsComplete).to_owned()
        }
        .into(),
    );
    ui.set_reports_tax_receipts_subtitle(
        i18n::format(
            language,
            Key::TaxReceiptsOf,
            &[
                Arg::Int(bookings.with_receipt() as i64),
                Arg::Int(bookings.booking_count as i64),
            ],
        )
        .into(),
    );
    ui.set_reports_tax_other_hint(other_currency_hint_bookings(bookings, language).into());
    ui.set_reports_tax_suggestions_title(
        i18n::format(
            language,
            Key::TaxSuggestionsTitle,
            &[Arg::Int(state.suggestions.len() as i64)],
        )
        .into(),
    );
    ui.set_reports_tax_suggestions_hint(i18n::text(language, Key::TaxSuggestionsHint).into());
    ui.set_reports_tax_suggestions(ModelRc::new(VecModel::from(suggestion_rows(
        &state.suggestions,
        language,
    ))));
    ui.set_reports_tax_rows(ModelRc::new(VecModel::from(tax_rows(
        bookings,
        state.missing_receipts_only,
        language,
    ))));
}

fn empty_texts(report: &TaxReport, language: Language) -> (String, String) {
    let title = i18n::format(language, Key::NoTransactionsIn, &[Arg::Str(report.title(language))]);
    let hint = if report.other_currency_bookings > 0 {
        i18n::format(
            language,
            Key::OtherCurrenciesExcluded,
            &[
                Arg::Str(report.currency.as_str().to_owned()),
                Arg::Int(report.other_currency_bookings as i64),
            ],
        )
    } else {
        i18n::text(language, Key::ReportsBuiltFrom).to_owned()
    };
    (title, hint)
}

fn other_currency_hint(report: &TaxReport, language: Language) -> String {
    if report.other_currency_bookings == 0 {
        return String::new();
    }
    i18n::format(
        language,
        Key::OtherCurrenciesExcluded,
        &[
            Arg::Str(report.currency.as_str().to_owned()),
            Arg::Int(report.other_currency_bookings as i64),
        ],
    )
}

fn other_currency_hint_bookings(bookings: &TaxBookings, language: Language) -> String {
    if bookings.other_currency_bookings == 0 {
        return String::new();
    }
    i18n::format(
        language,
        Key::OtherCurrenciesExcluded,
        &[
            Arg::Str(bookings.currency.as_str().to_owned()),
            Arg::Int(bookings.other_currency_bookings as i64),
        ],
    )
}

fn amount_rows(rows: &[(String, Money)], language: Language) -> Vec<ReportAmountRow> {
    rows.iter()
        .map(|(label, amount)| ReportAmountRow {
            label: label.clone().into(),
            amount_text: format::money(*amount, language, false).into(),
        })
        .collect()
}

fn vendor_rows(report: &TaxReport, language: Language) -> Vec<ReportVendorRow> {
    report
        .top_vendors
        .iter()
        .enumerate()
        .map(|(index, (name, amount))| ReportVendorRow {
            rank: format!("{}.", index + 1).into(),
            name: name.clone().into(),
            amount_text: format::money(*amount, language, false).into(),
        })
        .collect()
}

fn monthly_rows(report: &TaxReport, language: Language) -> Vec<ReportMonthlyRow> {
    report
        .monthly_breakdown
        .iter()
        .map(|month| {
            let label = if report.is_calendar_year() {
                month_name(language, month.month).to_owned()
            } else {
                format!("{} {}", month_name(language, month.month), month.year)
            };
            let net = month.net();
            ReportMonthlyRow {
                month: label.into(),
                income_text: format::money(month.income, language, false).into(),
                expenses_text: format::money(month.expenses, language, false).into(),
                net_text: format::money(net, language, net.minor != 0).into(),
                income_zero: month.income.minor == 0,
                expenses_zero: month.expenses.minor == 0,
                net_zero: net.minor == 0,
                net_tone: tone(net).into(),
            }
        })
        .collect()
}

fn average_monthly(report: &TaxReport) -> Money {
    let active: Vec<&finglass_core::report::MonthlyData> = report
        .monthly_breakdown
        .iter()
        .filter(|month| month.expenses.minor > 0)
        .collect();
    if active.is_empty() {
        return Money {
            minor: 0,
            currency: report.currency,
        };
    }

    let total: i128 = active.iter().map(|month| i128::from(month.expenses.minor)).sum();
    let count = active.len() as i128;
    let rounded = (total + count / 2) / count;
    Money {
        minor: i64::try_from(rounded).unwrap_or(i64::MAX),
        currency: report.currency,
    }
}

fn highest_month(report: &TaxReport, language: Language) -> String {
    report
        .monthly_breakdown
        .iter()
        .filter(|month| month.expenses.minor > 0)
        .max_by_key(|month| month.expenses.minor)
        .map_or_else(
            || "—".to_owned(),
            |month| {
                format!(
                    "{} {}",
                    month_name(language, month.month),
                    format::money(month.expenses, language, false)
                )
            },
        )
}

fn category_rows(report: &TaxReport, language: Language) -> (Vec<DashboardCategoryRow>, String) {
    let shares = &report.category_shares;
    if shares.is_empty() {
        return (Vec::new(), String::new());
    }

    let total: i64 = shares.iter().map(|share| share.amount.minor).sum();
    let amounts: Vec<Money> = shares.iter().map(|share| share.amount).collect();
    let percentages = format::shares(&amounts);
    let visible = shares.len().min(12);

    let mut rows: Vec<DashboardCategoryRow> = shares
        .iter()
        .take(visible)
        .enumerate()
        .map(|(index, share)| DashboardCategoryRow {
            label: share.name.clone().into(),
            percent_text: format::percent(percentages[index].clamp(0, 100) as f64 / 100.0, language).into(),
            amount_text: format::money(share.amount, language, false).into(),
            colour: color(&share.color),
            category_id: share.category_id.map_or(-1, |id| i32::try_from(id.0).unwrap_or(-1)),
            is_more: false,
            fraction: fraction(share.amount.minor, total),
        })
        .collect();

    let rest = &shares[visible..];
    if !rest.is_empty() {
        let rest_total: i64 = rest.iter().map(|share| share.amount.minor).sum();
        let rest_percent: i64 = percentages[visible..].iter().sum();
        rows.push(DashboardCategoryRow {
            label: i18n::format(language, Key::CategorySplitMore, &[Arg::Int(rest.len() as i64)]).into(),
            percent_text: format::percent(rest_percent.clamp(0, 100) as f64 / 100.0, language).into(),
            amount_text: format::money(
                Money {
                    minor: rest_total,
                    currency: report.currency,
                },
                language,
                false,
            )
            .into(),
            colour: color("#9E9E9E"),
            category_id: -1,
            is_more: true,
            fraction: fraction(rest_total, total),
        });
    }

    let transfers_note = if report.transfer_total.minor > 0 {
        i18n::format(
            language,
            Key::CategorySplitTransfers,
            &[Arg::Str(format::money(report.transfer_total, language, false))],
        )
    } else {
        String::new()
    };

    (rows, transfers_note)
}

fn recurring_rows(payments: &[RecurringPayment], language: Language) -> Vec<ReportRecurringRow> {
    let mut sorted: Vec<&RecurringPayment> = payments.iter().collect();
    sorted.sort_by_key(|payment| std::cmp::Reverse(payment.yearly_amount().minor.unsigned_abs()));

    sorted
        .into_iter()
        .map(|payment| {
            let amount = if payment.fixed_amount {
                format::money(payment.typical_amount, language, true)
            } else {
                format!("≈ {}", format::money(payment.typical_amount, language, true))
            };
            ReportRecurringRow {
                name: payment.name.clone().into(),
                meta: recurring_meta(payment, language).into(),
                cadence: cadence_text(payment.cadence, language).into(),
                next_text: format::date(payment.next_date).into(),
                amount_text: amount.into(),
                yearly_text: format::money(
                    Money {
                        minor: i64::try_from(payment.yearly_amount().minor.unsigned_abs()).unwrap_or(i64::MAX),
                        currency: payment.currency,
                    },
                    language,
                    false,
                )
                .into(),
                income: payment.is_income(),
            }
        })
        .collect()
}

fn recurring_meta(payment: &RecurringPayment, language: Language) -> String {
    let mut meta = i18n::format(language, Key::TimesSeen, &[Arg::Int(payment.occurrences as i64)]);
    if let Some(category) = &payment.category_name {
        meta.push_str(" · ");
        meta.push_str(category);
    }
    if !payment.fixed_amount {
        meta.push_str(" · ");
        meta.push_str(i18n::text(language, Key::AmountVaries));
    }
    meta
}

fn cadence_text(cadence: Cadence, language: Language) -> String {
    let key = match cadence {
        Cadence::Weekly => Key::CadenceWeekly,
        Cadence::Monthly => Key::CadenceMonthly,
        Cadence::Quarterly => Key::CadenceQuarterly,
        Cadence::Yearly => Key::CadenceYearly,
    };
    i18n::text(language, key).to_owned()
}

/// The yearly cost per currency, primary currency first and then by code. Income and transfers never
/// count, and currencies are never mixed.
pub(crate) fn recurring_cost_totals(payments: &[RecurringPayment], primary: Currency) -> Vec<(Currency, i64)> {
    let mut totals: BTreeMap<Currency, i64> = BTreeMap::new();
    for payment in payments
        .iter()
        .filter(|payment| !payment.is_income() && !payment.is_transfer)
    {
        let amount = i64::try_from(payment.yearly_amount().minor.unsigned_abs()).unwrap_or(i64::MAX);
        *totals.entry(payment.currency).or_default() = totals
            .get(&payment.currency)
            .copied()
            .unwrap_or(0)
            .saturating_add(amount);
    }

    let mut rows: Vec<(Currency, i64)> = totals.into_iter().collect();
    rows.sort_by_key(|(currency, _)| (*currency != primary, currency.as_str().to_owned()));
    rows
}

fn yearly_cost_text(totals: &[(Currency, i64)], language: Language) -> String {
    totals
        .iter()
        .map(|(currency, minor)| {
            format::money(
                Money {
                    minor: *minor,
                    currency: *currency,
                },
                language,
                false,
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn monthly_cost_text(totals: &[(Currency, i64)], language: Language) -> String {
    totals
        .iter()
        .map(|(currency, minor)| {
            format::money(
                Money {
                    minor: divide_round(*minor, 12),
                    currency: *currency,
                },
                language,
                false,
            )
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn divide_round(minor: i64, divisor: i64) -> i64 {
    let value = i128::from(minor);
    let divisor = i128::from(divisor);
    let rounded = if value >= 0 {
        (value + divisor / 2) / divisor
    } else {
        -((-value + divisor / 2) / divisor)
    };
    i64::try_from(rounded).unwrap_or(if rounded < 0 { i64::MIN } else { i64::MAX })
}

/// The flattened Tax bookings rows: section labels, group headers and the visible bookings.
pub(crate) fn tax_rows(bookings: &TaxBookings, missing_only: bool, language: Language) -> Vec<ReportTaxRow> {
    let mut rows = Vec::new();
    let visible = |booking: &TransactionItem| !missing_only || booking.document_count == 0;

    if bookings
        .income_groups
        .iter()
        .any(|group| group.bookings.iter().any(visible))
    {
        rows.push(section_row(Key::TaxSectionIncome, language));
        for group in &bookings.income_groups {
            push_group(&mut rows, group, bookings.currency, &visible, language);
        }
    }

    if bookings
        .deduction_groups
        .iter()
        .any(|group| group.bookings.iter().any(visible))
    {
        rows.push(section_row(Key::TaxSectionDeductions, language));
        for group in &bookings.deduction_groups {
            push_group(&mut rows, group, bookings.currency, &visible, language);
        }
    }

    rows
}

fn section_row(key: Key, language: Language) -> ReportTaxRow {
    ReportTaxRow {
        kind: "section".into(),
        title: i18n::text(language, key).into(),
        subtitle: String::new().into(),
        colour: color("#000000"),
        summary: String::new().into(),
        id: 0,
        date_text: String::new().into(),
        account_text: String::new().into(),
        amount_text: String::new().into(),
        expense: false,
        has_receipt: false,
        missing_receipt: false,
        can_reset: false,
    }
}

fn push_group(
    rows: &mut Vec<ReportTaxRow>,
    group: &TaxGroup,
    currency: Currency,
    visible: &impl Fn(&TransactionItem) -> bool,
    language: Language,
) {
    if !group.bookings.iter().any(visible) {
        return;
    }

    let summary = i18n::format(
        language,
        Key::BookingsCountTotal,
        &[
            Arg::Int(group.bookings.len() as i64),
            Arg::Str(format::money(group.net(), language, true)),
        ],
    );
    rows.push(ReportTaxRow {
        kind: "group".into(),
        title: group.name.clone().into(),
        subtitle: String::new().into(),
        colour: color(&group.color),
        summary: summary.into(),
        id: 0,
        date_text: String::new().into(),
        account_text: String::new().into(),
        amount_text: format::money(group.net(), language, true).into(),
        expense: group.net().minor < 0,
        has_receipt: false,
        missing_receipt: false,
        can_reset: false,
    });

    for booking in group.bookings.iter().filter(|booking| visible(booking)) {
        rows.push(booking_row(booking, currency, language));
    }
}

fn booking_row(booking: &TransactionItem, currency: Currency, language: Language) -> ReportTaxRow {
    let account = booking
        .account_iban
        .as_deref()
        .filter(|iban| !iban.is_empty())
        .map_or_else(String::new, |iban| {
            let tail: String = iban
                .chars()
                .rev()
                .take(4)
                .collect::<Vec<_>>()
                .into_iter()
                .rev()
                .collect();
            format!("…{tail}")
        });
    let amount = Money {
        minor: booking.amount.minor,
        currency,
    };
    ReportTaxRow {
        kind: "booking".into(),
        title: report::display_description(booking).into(),
        subtitle: tax_source_text(booking, language).into(),
        colour: color("#000000"),
        summary: String::new().into(),
        id: i32::try_from(booking.id.0).unwrap_or(i32::MAX),
        date_text: format::date(booking.date).into(),
        account_text: account.into(),
        amount_text: format::money(amount, language, true).into(),
        expense: booking.amount.minor < 0,
        has_receipt: booking.document_count > 0,
        missing_receipt: booking.document_count == 0,
        can_reset: booking.tax_override.is_some(),
    }
}

fn tax_source_text(booking: &TransactionItem, language: Language) -> String {
    if booking.tax_source == TaxSource::Category {
        return String::new();
    }
    let base = if booking.is_tax_relevant {
        i18n::text(language, Key::TaxRelevantChoice)
    } else {
        i18n::text(language, Key::TaxNotRelevantChoice)
    };
    match booking.tax_source {
        TaxSource::Manual => i18n::format(language, Key::TaxSetByYou, &[Arg::Str(base.to_owned())]),
        TaxSource::Rule => i18n::format(
            language,
            Key::TaxFromRule,
            &[
                Arg::Str(base.to_owned()),
                Arg::Str(booking.tax_rule_pattern.clone().unwrap_or_default()),
            ],
        ),
        TaxSource::Category => String::new(),
        TaxSource::None => base.to_owned(),
    }
}

fn suggestion_rows(
    suggestions: &[finglass_core::report::TaxSuggestion],
    language: Language,
) -> Vec<ReportSuggestionRow> {
    suggestions
        .iter()
        .map(|suggestion| {
            let booking = &suggestion.booking;
            ReportSuggestionRow {
                id: i32::try_from(booking.id.0).unwrap_or(i32::MAX),
                date_text: format::date(booking.date).into(),
                title: report::display_description(booking).into(),
                keyword_text: i18n::format(language, Key::MatchesKeyword, &[Arg::Str(suggestion.keyword.clone())])
                    .into(),
                amount_text: format::money(booking.amount, language, true).into(),
                expense: booking.amount.minor < 0,
            }
        })
        .collect()
}

fn tone(money: Money) -> &'static str {
    if money.minor < 0 {
        "negative"
    } else if money.minor > 0 {
        "positive"
    } else {
        "zero"
    }
}

fn fraction(amount: i64, total: i64) -> f32 {
    if total <= 0 { 0.0 } else { amount as f32 / total as f32 }
}

fn color(hex: &str) -> slint::Color {
    let text = hex.trim().trim_start_matches('#');
    if text.len() == 6
        && let Ok(value) = u32::from_str_radix(text, 16)
    {
        return slint::Color::from_rgb_u8(
            ((value >> 16) & 0xFF) as u8,
            ((value >> 8) & 0xFF) as u8,
            (value & 0xFF) as u8,
        );
    }

    slint::Color::from_rgb_u8(117, 117, 117)
}

/// Whether the report covers exactly this or last calendar year, the only periods FinDesk's category rows
/// link into Transactions with.
pub(crate) fn linkable(report: &TaxReport, today: Date) -> bool {
    let this_year = (report.from.year(), report.to.year()) == (today.year(), today.year());
    let last_year = (report.from.year(), report.to.year()) == (today.year() - 1, today.year() - 1);
    (this_year || last_year) && report.is_calendar_year()
}

/// The suffix FinDesk uses in the default file names: the year, or the ISO range.
pub(crate) fn file_suffix(state: &ReportsState) -> String {
    state
        .custom_range
        .map_or_else(|| state.selected_year.to_string(), |(from, to)| format!("{from}_{to}"))
}

/// The save dialog's suggested name.
pub(crate) fn default_file_name(state: &ReportsState, file: ExportFile) -> String {
    let suffix = file_suffix(state);
    match file {
        ExportFile::ReportPdf => format!("report-{suffix}.pdf"),
        ExportFile::Csv => format!("finglass-{suffix}.csv"),
        ExportFile::TaxXlsx => format!("tax-bookings-{suffix}.xlsx"),
        ExportFile::TaxPdf => format!("tax-bookings-{suffix}.pdf"),
    }
}

#[cfg(test)]
mod tests {
    use finglass_core::model::{AccountId, CategoryId, TransactionId};
    use finglass_core::money::Currency;
    use time::macros::date;

    use super::*;

    fn transaction(id: i64, minor: i64, document_count: usize, override_value: Option<bool>) -> TransactionItem {
        TransactionItem {
            id: TransactionId(id),
            account_id: AccountId(1),
            account_iban: Some("DE89370400440532013000".to_owned()),
            date: date!(2026 - 03 - 01),
            amount: Money {
                minor,
                currency: Currency::EUR,
            },
            balance_after: None,
            currency: Currency::EUR,
            partner_name: Some("Beispiel Apotheke".to_owned()),
            purpose: None,
            category_id: Some(CategoryId(7)),
            category_name: Some("Health".to_owned()),
            category_color: Some("#123456".to_owned()),
            is_tax_relevant: true,
            document_count,
            is_new: false,
            is_transfer: false,
            tax_source: TaxSource::Manual,
            tax_rule_pattern: None,
            tax_override: override_value,
        }
    }

    fn bookings() -> TaxBookings {
        let with_receipt = transaction(1, -2_000, 1, Some(false));
        let without_receipt = transaction(2, -3_000, 0, None);
        TaxBookings {
            from: date!(2026 - 01 - 01),
            to: date!(2026 - 12 - 31),
            currency: Currency::EUR,
            income_groups: Vec::new(),
            deduction_groups: vec![TaxGroup {
                category_id: Some(CategoryId(7)),
                name: "Health".to_owned(),
                color: "#123456".to_owned(),
                bookings: vec![with_receipt, without_receipt],
                income: Money {
                    minor: 0,
                    currency: Currency::EUR,
                },
                expenses: Money {
                    minor: 5_000,
                    currency: Currency::EUR,
                },
            }],
            booking_count: 2,
            other_currency_bookings: 0,
        }
    }

    fn payment(name: &str, currency: Currency, minor: i64, transfer: bool) -> RecurringPayment {
        RecurringPayment {
            name: name.to_owned(),
            cadence: Cadence::Monthly,
            typical_amount: Money { minor, currency },
            fixed_amount: true,
            last_date: date!(2026 - 09 - 10),
            next_date: date!(2026 - 10 - 10),
            occurrences: 4,
            category_name: Some("Media".to_owned()),
            currency,
            is_transfer: transfer,
        }
    }

    #[test]
    fn tax_rows_flatten_sections_groups_and_the_missing_receipts_filter() {
        let all = tax_rows(&bookings(), false, Language::En);
        assert_eq!(all[0].kind, "section");
        assert_eq!(all[0].title, "Deductions");
        assert_eq!(all[1].kind, "group");
        assert_eq!(all[1].title, "Health");
        assert_eq!(all[1].summary, "2 bookings · -€50.00");
        assert_eq!(all.len(), 4, "section + group + two bookings");
        assert_eq!(all[2].kind, "booking");
        assert_eq!(all[2].account_text, "…3000");
        assert_eq!(all[2].amount_text, "-€20.00");
        assert!(all[2].has_receipt);
        assert!(!all[2].missing_receipt);
        assert!(all[2].can_reset, "the stored override can be reset");
        assert!(!all[3].can_reset);

        let missing = tax_rows(&bookings(), true, Language::En);
        assert_eq!(missing.len(), 3, "section + group + the one booking without a receipt");
        assert_eq!(missing[2].id, 2);
        assert!(missing[2].missing_receipt);
        assert_eq!(
            missing[1].summary, "2 bookings · -€50.00",
            "group totals stay whole-period"
        );
    }

    #[test]
    fn recurring_totals_are_per_currency_with_the_primary_first() {
        let usd = Currency::new("USD").unwrap();
        let payments = vec![
            payment("Spotify", Currency::EUR, -999, false),
            payment("Rent", usd, -1_200, false),
            payment("Salary", Currency::EUR, 300_000, false),
            payment("Transfer", Currency::EUR, -50_000, true),
        ];

        let totals = recurring_cost_totals(&payments, usd);
        assert_eq!(totals, vec![(usd, 14_400), (Currency::EUR, 11_988)]);

        let totals = recurring_cost_totals(&payments, Currency::EUR);
        assert_eq!(totals, vec![(Currency::EUR, 11_988), (usd, 14_400)]);
        assert_eq!(yearly_cost_text(&totals, Language::En), "€119.88\n$144.00");
        assert_eq!(monthly_cost_text(&totals, Language::En), "€9.99\n$12.00");
    }

    #[test]
    fn file_names_use_the_year_or_the_iso_custom_range() {
        let mut state = ReportsState::new(Language::En, date!(2026 - 10 - 15));
        state.selected_year = 2026;
        assert_eq!(file_suffix(&state), "2026");
        assert_eq!(default_file_name(&state, ExportFile::ReportPdf), "report-2026.pdf");
        assert_eq!(default_file_name(&state, ExportFile::Csv), "finglass-2026.csv");
        assert_eq!(default_file_name(&state, ExportFile::TaxXlsx), "tax-bookings-2026.xlsx");
        assert_eq!(default_file_name(&state, ExportFile::TaxPdf), "tax-bookings-2026.pdf");

        state.custom_range = Some((date!(2025 - 12 - 01), date!(2026 - 01 - 31)));
        assert_eq!(file_suffix(&state), "2025-12-01_2026-01-31");
        assert_eq!(
            default_file_name(&state, ExportFile::ReportPdf),
            "report-2025-12-01_2026-01-31.pdf"
        );
    }

    #[test]
    fn category_links_only_cover_this_or_last_calendar_year() {
        let today = date!(2026 - 10 - 15);
        assert!(linkable(&TaxReport::empty(2026), today));
        assert!(linkable(&TaxReport::empty(2025), today));
        assert!(!linkable(&TaxReport::empty(2024), today));

        let mut partial = TaxReport::empty(2026);
        partial.from = date!(2026 - 02 - 01);
        assert!(!linkable(&partial, today), "a custom range does not link");
    }

    #[test]
    fn monthly_rows_use_the_core_month_names_and_zero_tones() {
        let report = TaxReport {
            year: 2026,
            from: date!(2026 - 01 - 01),
            to: date!(2026 - 03 - 31),
            total_income: Money {
                minor: 10_000,
                currency: Currency::EUR,
            },
            total_expenses: Money {
                minor: 5_000,
                currency: Currency::EUR,
            },
            net_income: Money {
                minor: 5_000,
                currency: Currency::EUR,
            },
            expenses_by_category: Vec::new(),
            tax_relevant_expenses: Vec::new(),
            tax_relevant_transactions: Vec::new(),
            monthly_breakdown: vec![
                finglass_core::report::MonthlyData {
                    year: 2026,
                    month: 1,
                    income: Money {
                        minor: 0,
                        currency: Currency::EUR,
                    },
                    expenses: Money {
                        minor: 0,
                        currency: Currency::EUR,
                    },
                },
                finglass_core::report::MonthlyData {
                    year: 2026,
                    month: 2,
                    income: Money {
                        minor: 10_000,
                        currency: Currency::EUR,
                    },
                    expenses: Money {
                        minor: 5_000,
                        currency: Currency::EUR,
                    },
                },
            ],
            top_vendors: Vec::new(),
            transaction_count: 2,
            categories_used: 1,
            currency: Currency::EUR,
            other_currency_bookings: 0,
            transfer_total: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            category_shares: Vec::new(),
        };

        let rows = monthly_rows(&report, Language::De);
        assert_eq!(rows[0].month, "Januar 2026");
        assert!(rows[0].income_zero);
        assert!(rows[0].net_zero);
        assert_eq!(rows[0].net_tone, "zero");
        assert_eq!(rows[1].month, "Februar 2026");
        assert_eq!(rows[1].income_text, "100,00\u{a0}€");
        assert_eq!(rows[1].net_text, "+50,00\u{a0}€");
        assert_eq!(rows[1].net_tone, "positive");
        assert_eq!(average_monthly(&report).minor, 5_000);
        assert_eq!(highest_month(&report, Language::En), "February €50.00");
    }
}
