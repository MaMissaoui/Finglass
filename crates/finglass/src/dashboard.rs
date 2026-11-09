//! Renders `finglass_core::app::dashboard` into the Slint page and loads it off the event loop.
//!
//! The database work runs on a background thread; the finished state comes back through
//! `slint::invoke_from_event_loop`, like the rest of the shell.

use std::sync::Arc;
use std::thread;

use finglass_core::app::dashboard::{
    BalanceTone, DashboardState, DashboardView, StatementLineKind, local_today, month_text,
};
use finglass_core::dashboard::YearMonth;
use slint::{ComponentHandle, ModelRc, SharedString, VecModel};
use time::Date;

use crate::shell::ShellState;
use crate::{
    DashboardBudgetRow, DashboardCategoryRow, DashboardMonthBar, DashboardStatementLine, DashboardTransactionRow,
    DashboardUpcomingRow, MainWindow,
};

/// Applies `state` to the window without touching the database.
pub fn apply(main: &MainWindow, state: &DashboardState) {
    main.set_dashboard_loading(state.is_loading);
    main.set_dashboard_first_run(state.is_first_run());
    main.set_dashboard_show_content(state.has_figures());
    main.set_dashboard_error_text(state.error.clone().unwrap_or_default().into());
    main.set_dashboard_month_options(month_option_model(state));
    main.set_dashboard_month_selected(selected_month_index(state));

    if let Some(view) = &state.view {
        apply_view(main, view);
    }
}

fn apply_view(main: &MainWindow, view: &DashboardView) {
    main.set_dashboard_month_label(view.month_label.clone().into());
    main.set_dashboard_previous_enabled(view.previous_enabled);
    main.set_dashboard_next_enabled(view.next_enabled);
    main.set_dashboard_is_current_month(view.is_current_month);

    main.set_dashboard_available_title(view.available_title.clone().into());
    main.set_dashboard_available_value(view.available_value.clone().into());
    main.set_dashboard_available_subtitle(view.available_subtitle.clone().into());
    main.set_dashboard_available_tone(tone_code(view.available_tone).into());
    main.set_dashboard_statement_lines(statement_model(&view.statement_lines));
    main.set_dashboard_income_title(view.income_title.clone().into());
    main.set_dashboard_income_value(view.income_value.clone().into());
    main.set_dashboard_expenses_title(view.expenses_title.clone().into());
    main.set_dashboard_expenses_value(view.expenses_value.clone().into());
    main.set_dashboard_pending_title(view.pending_title.clone().into());
    main.set_dashboard_pending_value(view.pending_value.clone().into());
    main.set_dashboard_pending_subtitle(view.pending_subtitle.clone().into());
    main.set_dashboard_pending_lines(ModelRc::new(VecModel::from(
        view.pending_lines
            .iter()
            .map(|line| SharedString::from(line.as_str()))
            .collect::<Vec<_>>(),
    )));
    main.set_dashboard_pending_enabled(view.pending_enabled);

    main.set_dashboard_expenses_section_title(view.expenses_section_title.clone().into());
    main.set_dashboard_expenses_empty_text(view.expenses_empty_text.clone().into());
    main.set_dashboard_expense_rows(category_model(&view.expense_rows));
    main.set_dashboard_transfers_note(view.transfers_note.clone().unwrap_or_default().into());

    main.set_dashboard_chart_title(view.chart_title.clone().into());
    main.set_dashboard_chart_empty_text(view.chart_empty_text.clone().unwrap_or_default().into());
    main.set_dashboard_chart_axis_top(view.chart_axis_top.clone().into());
    main.set_dashboard_chart_axis_mid(view.chart_axis_mid.clone().into());
    main.set_dashboard_chart_axis_zero(view.chart_axis_zero.clone().into());
    main.set_dashboard_month_bars(month_model(&view.month_bars));
    main.set_dashboard_income_legend(view.income_legend.clone().into());
    main.set_dashboard_expenses_legend(view.expenses_legend.clone().into());
    main.set_dashboard_partial_note(view.partial_month_note.clone().into());

    main.set_dashboard_budgets_title(view.budgets_title.clone().into());
    main.set_dashboard_budget_rows(budget_model(&view.budgets));
    main.set_dashboard_has_budgets(!view.budgets.is_empty());
    main.set_dashboard_month_progress(view.month_progress);

    main.set_dashboard_tax_title(view.tax_title.clone().into());
    main.set_dashboard_tax_hint(view.tax_hint.clone().into());
    main.set_dashboard_tax_value(view.tax_value.clone().into());
    main.set_dashboard_tax_rows(category_model(&view.tax_rows));

    main.set_dashboard_upcoming_title(view.upcoming_title.clone().into());
    main.set_dashboard_upcoming_rows(upcoming_model(&view.upcoming));
    main.set_dashboard_has_upcoming(!view.upcoming.is_empty());

    main.set_dashboard_recent_title(view.recent_title.clone().into());
    main.set_dashboard_recent_empty_text(view.recent_empty_text.clone().into());
    main.set_dashboard_recent_rows(transaction_model(&view.recent_transactions));
    main.set_dashboard_view_all_label(view.view_all_label.clone().into());
}

/// Loads the selected month on a background thread and publishes the result on the event loop.
///
/// `None` keeps the selected month, so a reload while the Dashboard stays open does not jump to today.
pub fn spawn_load(main: &MainWindow, state: &Arc<ShellState>, month: Option<YearMonth>) {
    let today = local_today();
    let pending = prepare_load(state, today, month);
    spawn_pending(main, state, pending, today);
}

/// Opens the Dashboard on today's month, reading the local date at this moment.
pub fn spawn_open(main: &MainWindow, state: &Arc<ShellState>) {
    let today = local_today();
    let pending = prepare_open(state, today);
    spawn_pending(main, state, pending, today);
}

/// Builds the state a load starts from, preserving the selected month when `month` is `None`.
pub(crate) fn prepare_load(state: &Arc<ShellState>, today: Date, month: Option<YearMonth>) -> DashboardState {
    let language = *state.language.lock().expect("language state poisoned");
    let mut pending = state.dashboard.lock().expect("dashboard state poisoned").clone();
    pending.language = language;
    pending.begin_load(month.unwrap_or(pending.selected_month), today);
    pending
}

/// Builds the state that resets the Dashboard to `today`'s month, as FinDesk's new view model does.
pub(crate) fn prepare_open(state: &Arc<ShellState>, today: Date) -> DashboardState {
    let language = *state.language.lock().expect("language state poisoned");
    let mut pending = state.dashboard.lock().expect("dashboard state poisoned").clone();
    pending.language = language;
    pending.open(today);
    pending
}

fn spawn_pending(main: &MainWindow, state: &Arc<ShellState>, mut pending: DashboardState, today: Date) {
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };

    *state.dashboard.lock().expect("dashboard state poisoned") = pending.clone();
    apply(main, &pending);

    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let _ = pending.load(&db, today);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                *state.dashboard.lock().expect("dashboard state poisoned") = pending.clone();
                apply(&ui, &pending);
            }
        });
    });
}

/// Opens one month from the header's drop-down.
pub fn select_month(main: &MainWindow, state: &Arc<ShellState>, index: i32) {
    let month = {
        let dashboard = state.dashboard.lock().expect("dashboard state poisoned");
        usize::try_from(index)
            .ok()
            .and_then(|index| dashboard.months.get(index).copied())
    };
    if let Some(month) = month {
        spawn_load(main, state, Some(month));
    }
}

/// Opens the previous month.
pub fn previous_month(main: &MainWindow, state: &Arc<ShellState>) {
    let selected = state.dashboard.lock().expect("dashboard state poisoned").selected_month;
    spawn_load(main, state, Some(selected.minus_months(1)));
}

/// Opens the next month.
pub fn next_month(main: &MainWindow, state: &Arc<ShellState>) {
    let selected = state.dashboard.lock().expect("dashboard state poisoned").selected_month;
    spawn_load(main, state, Some(selected.plus_months(1)));
}

/// Opens today's month.
pub fn current_month(main: &MainWindow, state: &Arc<ShellState>) {
    spawn_open(main, state);
}

fn month_option_model(state: &DashboardState) -> ModelRc<SharedString> {
    ModelRc::new(VecModel::from(
        state
            .months
            .iter()
            .map(|month| SharedString::from(month_text(*month, state.language)))
            .collect::<Vec<_>>(),
    ))
}

fn selected_month_index(state: &DashboardState) -> i32 {
    state
        .months
        .iter()
        .position(|month| *month == state.selected_month)
        .and_then(|index| i32::try_from(index).ok())
        .unwrap_or(0)
}

fn tone_code(tone: BalanceTone) -> &'static str {
    match tone {
        BalanceTone::Positive => "positive",
        BalanceTone::Zero => "zero",
        BalanceTone::Negative => "negative",
    }
}

fn statement_model(lines: &[finglass_core::app::dashboard::StatementLineView]) -> ModelRc<DashboardStatementLine> {
    ModelRc::new(VecModel::from(
        lines
            .iter()
            .map(|line| DashboardStatementLine {
                label: line.label.clone().into(),
                amount_text: line.amount_text.clone().into(),
                kind: match line.kind {
                    StatementLineKind::Carry => "carry",
                    StatementLineKind::In => "in",
                    StatementLineKind::Out => "out",
                    StatementLineKind::Movement => "movement",
                    StatementLineKind::Result => "result",
                }
                .into(),
            })
            .collect::<Vec<_>>(),
    ))
}

fn category_model(rows: &[finglass_core::app::dashboard::CategoryRowView]) -> ModelRc<DashboardCategoryRow> {
    ModelRc::new(VecModel::from(
        rows.iter()
            .map(|row| DashboardCategoryRow {
                label: row.label.clone().into(),
                percent_text: row.percent_text.clone().into(),
                amount_text: row.amount_text.clone().into(),
                colour: color(&row.color),
                category_id: row.category_id.map_or(-1, |id| i32::try_from(id.0).unwrap_or(-1)),
                is_more: row.is_more,
                fraction: row.fraction,
            })
            .collect::<Vec<_>>(),
    ))
}

fn month_model(bars: &[finglass_core::app::dashboard::MonthBarView]) -> ModelRc<DashboardMonthBar> {
    ModelRc::new(VecModel::from(
        bars.iter()
            .map(|bar| DashboardMonthBar {
                label: bar.label.clone().into(),
                title: bar.title.clone().into(),
                income_text: bar.income_text.clone().into(),
                expenses_text: bar.expenses_text.clone().into(),
                income_fraction: bar.income_fraction,
                expenses_fraction: bar.expenses_fraction,
                partial: bar.partial,
            })
            .collect::<Vec<_>>(),
    ))
}

fn budget_model(rows: &[finglass_core::app::dashboard::BudgetRowView]) -> ModelRc<DashboardBudgetRow> {
    ModelRc::new(VecModel::from(
        rows.iter()
            .map(|row| DashboardBudgetRow {
                name: row.name.clone().into(),
                amount_text: row.amount_text.clone().into(),
                rest_text: row.rest_text.clone().into(),
                ratio: row.ratio,
                over: row.is_over,
                warning: row.warning,
                colour: color(&row.color),
            })
            .collect::<Vec<_>>(),
    ))
}

fn upcoming_model(rows: &[finglass_core::app::dashboard::UpcomingRowView]) -> ModelRc<DashboardUpcomingRow> {
    ModelRc::new(VecModel::from(
        rows.iter()
            .map(|row| DashboardUpcomingRow {
                date_text: row.date_text.clone().into(),
                title: row.name.clone().into(),
                meta: row.meta.clone().into(),
                amount_text: row.amount_text.clone().into(),
                expense: !row.is_income,
            })
            .collect::<Vec<_>>(),
    ))
}

fn transaction_model(rows: &[finglass_core::app::dashboard::TransactionRowView]) -> ModelRc<DashboardTransactionRow> {
    ModelRc::new(VecModel::from(
        rows.iter()
            .map(|row| DashboardTransactionRow {
                date_text: row.date_text.clone().into(),
                title: row.title.clone().into(),
                meta: row.meta.clone().into(),
                amount_text: row.amount_text.clone().into(),
                expense: row.is_expense,
                partner: row.partner.clone().unwrap_or_default().into(),
            })
            .collect::<Vec<_>>(),
    ))
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
