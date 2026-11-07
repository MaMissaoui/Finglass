//! Headless rendering of the app shell with Slint's software renderer.
//!
//! `set_platform` can only be called once per process, so every render lives in this one test.

use std::collections::HashSet;
use std::io::BufWriter;
use std::path::Path;
use std::rc::Rc;

use finglass_core::app;
use finglass_core::app::dashboard::{DashboardState, DashboardView};
use finglass_core::app::prefs::ThemePref;
use finglass_core::app::reports::ReportsState;
use finglass_core::audit::{self, Actor};
use finglass_core::db::Database;
use finglass_core::i18n::{self, Key, Language};
use finglass_core::model::{
    AccountId, AccountType, CategoryId, NewAccount, NewCategory, NewDocument, NewTransaction, TransactionId,
};
use finglass_core::money::{Currency, Money};
use finglass_core::paths::AppPaths;
use finglass_core::repo::{accounts, categories, documents, transactions};
use slint::platform::software_renderer::{MinimalSoftwareWindow, RepaintBufferType};
use slint::platform::{Platform, PlatformError, WindowAdapter};
use slint::{Color, ComponentHandle, ModelRc, PhysicalSize, Rgb8Pixel, SharedPixelBuffer, SharedString, VecModel};
use time::macros::date;

slint::include_modules!();

#[allow(dead_code)]
#[path = "../src/reports.rs"]
mod reports;

const SLUG: &str = "default";

struct TestPlatform {
    window: Rc<MinimalSoftwareWindow>,
}

impl Platform for TestPlatform {
    fn create_window_adapter(&self) -> Result<Rc<dyn WindowAdapter>, PlatformError> {
        Ok(self.window.clone())
    }
}

#[test]
fn renders_the_app_shell() {
    let window = MinimalSoftwareWindow::new(RepaintBufferType::ReusedBuffer);
    slint::platform::set_platform(Box::new(TestPlatform { window: window.clone() }))
        .expect("test platform must be installable");

    let temp = tempfile::tempdir().unwrap();
    let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
    let db = app::unlock::create(
        &paths,
        SLUG,
        "correct horse battery staple",
        "correct horse battery staple",
        true,
    )
    .unwrap();
    audit::log(&db, "PROFILE_OPENED", Actor::User, "Profile default opened").unwrap();
    app::prefs::save_theme(&db, ThemePref::Dark).unwrap();
    app::prefs::save_language(&db, Language::De).unwrap();
    seed_dashboard(&db);
    let rows = app::audit::recent(&db, 100).unwrap();
    let today = date!(2026 - 10 - 15);

    {
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(&ui, Language::En, ThemePref::Light, "create", "");
        ui.show().unwrap();
        let buffer = render(&window, "unlock-create-en", 560, 760);
        assert_varied(&buffer, "unlock-create-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(
            &ui,
            Language::De,
            ThemePref::Light,
            "unlock",
            i18n::text(Language::De, Key::WrongPassphrase),
        );
        ui.show().unwrap();
        let buffer = render(&window, "unlock-wrong-de", 560, 700);
        assert_varied(&buffer, "unlock-wrong-de");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.show().unwrap();
        let buffer = render(&window, "dashboard-en-light", 1100, 720);
        assert_varied(&buffer, "dashboard-en-light");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::De, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.show().unwrap();
        let buffer = render(&window, "dashboard-de-dark", 1100, 720);
        assert_varied(&buffer, "dashboard-de-dark");
        let _ = ui.hide();
    }

    {
        let first_run_temp = tempfile::tempdir().unwrap();
        let first_run_paths = AppPaths::resolve(Some(first_run_temp.path().as_os_str().to_owned()), None).unwrap();
        let first_run_db = app::unlock::create(
            &first_run_paths,
            SLUG,
            "correct horse battery staple",
            "correct horse battery staple",
            true,
        )
        .unwrap();
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&first_run_db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.show().unwrap();
        let buffer = render(&window, "dashboard-first-run-en", 1100, 720);
        assert_varied(&buffer, "dashboard-first-run-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Dark, "settings", &rows);
        ui.show().unwrap();
        let buffer = render(&window, "settings-en-dark", 1100, 720);
        assert_varied(&buffer, "settings-en-dark");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::Fr, ThemePref::Light, "settings", &rows);
        ui.show().unwrap();
        let buffer = render(&window, "settings-fr-light", 1100, 720);
        assert_varied(&buffer, "settings-fr-light");
        let _ = ui.hide();
    }

    let report_temp = tempfile::tempdir().unwrap();
    let report_paths = AppPaths::resolve(Some(report_temp.path().as_os_str().to_owned()), None).unwrap();
    let report_db = app::unlock::create(
        &report_paths,
        SLUG,
        "correct horse battery staple",
        "correct horse battery staple",
        true,
    )
    .unwrap();
    seed_reports(&report_db);

    let reports_en = {
        let mut state = ReportsState::new(Language::En, today);
        state.load_tax_report(&report_db, 2026);
        state.load_recurring(&report_db, today);
        state
    };
    let reports_de = {
        let mut state = ReportsState::new(Language::De, today);
        state.load_tax_report(&report_db, 2026);
        state.load_recurring(&report_db, today);
        state
    };

    for (tab, name) in [
        ("summary", "reports-summary-en-light"),
        ("tax", "reports-tax-en-light"),
        ("monthly", "reports-monthly-en-light"),
        ("category", "reports-category-en-light"),
        ("recurring", "reports-recurring-en-light"),
    ] {
        render_reports(&window, &rows, &reports_en, tab, name, 1100, 720);
    }

    render_reports(
        &window,
        &rows,
        &reports_de,
        "summary",
        "reports-summary-de-dark",
        1100,
        720,
    );
    render_reports(&window, &rows, &reports_de, "tax", "reports-tax-de-dark", 1100, 720);

    let mut missing = reports_en.clone();
    missing.set_missing_receipts_only(true);
    render_reports(
        &window,
        &rows,
        &missing,
        "tax",
        "reports-tax-missing-en-light",
        1100,
        720,
    );
    render_reports(
        &window,
        &rows,
        &reports_en,
        "tax",
        "reports-tax-narrow-en-light",
        900,
        720,
    );
}

fn render_reports(
    window: &Rc<MinimalSoftwareWindow>,
    rows: &[app::AuditRow],
    state: &ReportsState,
    tab: &str,
    name: &str,
    width: u32,
    height: u32,
) {
    let theme = if name.ends_with("dark") {
        ThemePref::Dark
    } else {
        ThemePref::Light
    };
    let ui = MainWindow::new().unwrap();
    configure_main(&ui, state.language, theme, "reports", rows);
    reports::apply(&ui, state);
    ui.set_reports_tab(tab.into());
    ui.show().unwrap();
    let buffer = render(window, name, width, height);
    assert_varied(&buffer, name);
    let _ = ui.hide();
}

fn seed_reports(db: &Database) {
    let eur = account(
        db,
        "DE89370400440532013000",
        500_000,
        Currency::EUR,
        AccountType::Checking,
    );
    let usd_currency = Currency::new("USD").unwrap();
    let usd = account(
        db,
        "DE44500105175407324931",
        50_000,
        usd_currency,
        AccountType::Checking,
    );

    let salary = category(db, "Salary", "#4CAF50", true, false, None);
    let donations = category(db, "Donations", "#9C27B0", true, false, None);
    let health = category(db, "Health", "#2196F3", true, false, None);
    let handwerk = category(db, "Handwerk", "#FF5722", true, false, None);
    let food = category(db, "Food", "#FFC107", false, false, Some(30_000));
    let travel = category(db, "Travel", "#00BCD4", true, false, None);

    for month in 1..=10 {
        let day = time::Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 1).unwrap();
        txn(db, eur, Currency::EUR, day, 250_000, "Employer GmbH", Some(salary));
    }

    for month in 5..=9 {
        let day = time::Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 10).unwrap();
        txn(db, eur, Currency::EUR, day, -999, "Spotify", None);
        txn(db, usd, usd_currency, day, -1_199, "Cloud Service", None);
    }

    let donation = txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 03 - 05),
        -10_000,
        "Beispielhilfe e. V.",
        Some(donations),
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 09 - 06),
        -5_000,
        "Beispielhilfe e. V.",
        Some(donations),
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 04 - 12),
        -8_900,
        "Beispiel-Krankenkasse",
        Some(health),
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 08 - 20),
        -2_500,
        "Apotheke am Ring",
        Some(health),
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 06 - 20),
        -25_000,
        "Beispiel Werk GmbH",
        Some(handwerk),
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 10 - 02),
        -12_000,
        "Example Market",
        Some(food),
    );
    txn(
        db,
        usd,
        usd_currency,
        date!(2026 - 07 - 07),
        -4_500,
        "US Travel",
        Some(travel),
    );
    txn(db, usd, usd_currency, date!(2026 - 10 - 06), -3_000, "US shop", None);
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 09 - 28),
        -2_550,
        "Apotheke am Ring",
        None,
    );
    txn(
        db,
        eur,
        Currency::EUR,
        date!(2026 - 10 - 08),
        -9_890,
        "Beispiel-Krankenkasse",
        None,
    );

    documents::create(
        db,
        NewDocument {
            file_name: "spende.pdf".to_owned(),
            stored_path: "documents/spende.pdf".to_owned(),
            mime_type: "application/pdf".to_owned(),
            transaction_id: Some(donation),
        },
    )
    .unwrap();
}

fn seed_dashboard(db: &Database) {
    let checking = account(
        db,
        "DE89370400440532013000",
        245_037,
        Currency::EUR,
        AccountType::Checking,
    );
    let usd_currency = Currency::new("USD").unwrap();
    let dollar = account(
        db,
        "DE44500105175407324931",
        50_000,
        usd_currency,
        AccountType::Checking,
    );
    let loan = account(db, "DE44500105175407324930", -500_000, Currency::EUR, AccountType::Loan);
    let food = category(db, "Food", "#FF5722", false, false, Some(30_000));
    let donations = category(db, "Donations", "#3BC7B0", true, false, None);
    let transfers = category(db, "Own transfers", "#4CAF50", false, true, None);

    for month in 6..=10 {
        let day = time::Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 2).unwrap();
        txn(db, checking, Currency::EUR, day, -12_000, "Example Market", Some(food));
        let payday = time::Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 3).unwrap();
        txn(
            db,
            checking,
            Currency::EUR,
            payday,
            150_000 + i64::from(month) * 1_000,
            "Employer GmbH",
            None,
        );
    }

    txn(
        db,
        checking,
        Currency::EUR,
        date!(2026 - 03 - 05),
        -10_000,
        "Example Aid",
        Some(donations),
    );
    txn(
        db,
        checking,
        Currency::EUR,
        date!(2026 - 09 - 06),
        -5_000,
        "Example Aid",
        Some(donations),
    );
    txn(
        db,
        checking,
        Currency::EUR,
        date!(2026 - 10 - 04),
        -20_000,
        "Own transfer",
        Some(transfers),
    );
    txn(
        db,
        checking,
        Currency::EUR,
        date!(2026 - 10 - 05),
        -1_500,
        "Corner Kiosk",
        None,
    );
    txn(db, dollar, usd_currency, date!(2026 - 10 - 06), -3_000, "US shop", None);
    txn(db, loan, Currency::EUR, date!(2026 - 10 - 07), -1_000, "Interest", None);

    for month in 6..=9 {
        let day = time::Date::from_calendar_date(2026, time::Month::try_from(month).unwrap(), 10).unwrap();
        txn(db, checking, Currency::EUR, day, -999, "Spotify", None);
    }
}

fn account(db: &Database, iban: &str, balance: i64, currency: Currency, account_type: AccountType) -> AccountId {
    accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: None,
            iban: Some(iban.to_owned()),
            account_number: None,
            currency,
            account_type,
            balance: Money {
                minor: balance,
                currency,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

fn category(
    db: &Database,
    name: &str,
    color: &str,
    tax_relevant: bool,
    is_transfer: bool,
    budget: Option<i64>,
) -> CategoryId {
    categories::create(
        db,
        NewCategory {
            name: name.to_owned(),
            color: color.to_owned(),
            budget_limit: budget.map(|minor| Money {
                minor,
                currency: Currency::EUR,
            }),
            is_tax_relevant: tax_relevant,
            is_transfer,
        },
    )
    .unwrap()
}

fn txn(
    db: &Database,
    account: AccountId,
    currency: Currency,
    day: time::Date,
    minor: i64,
    partner: &str,
    category: Option<CategoryId>,
) -> TransactionId {
    let hash = format!("{}-{}-{}-{}", account.0, day, minor, partner);
    transactions::insert_planned(
        db,
        account,
        vec![NewTransaction {
            booking_date: day,
            amount: Money { minor, currency },
            balance_after: None,
            partner_name: Some(partner.to_owned()),
            purpose: None,
            category_id: category,
            import_hash: hash.clone(),
            created_at: time::OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }],
    )
    .unwrap();
    transactions::list_for_account(db, account, None, None)
        .unwrap()
        .into_iter()
        .find(|transaction| transaction.import_hash == hash)
        .expect("the inserted booking")
        .id
}

fn apply_dashboard(ui: &MainWindow, state: &DashboardState) {
    ui.set_dashboard_loading(state.is_loading);
    ui.set_dashboard_first_run(state.is_first_run());
    ui.set_dashboard_show_content(state.has_figures());
    ui.set_dashboard_error_text(state.error.clone().unwrap_or_default().into());

    if let Some(view) = &state.view {
        apply_dashboard_view(ui, view, state.language);
    }
}

fn apply_dashboard_view(ui: &MainWindow, view: &DashboardView, language: Language) {
    ui.set_dashboard_month_label(view.month_label.clone().into());
    ui.set_dashboard_previous_enabled(view.previous_enabled);
    ui.set_dashboard_next_enabled(view.next_enabled);
    ui.set_dashboard_is_current_month(view.is_current_month);
    ui.set_dashboard_available_title(view.available_title.clone().into());
    ui.set_dashboard_available_value(view.available_value.clone().into());
    ui.set_dashboard_available_subtitle(view.available_subtitle.clone().into());
    ui.set_dashboard_available_tone(
        match view.available_tone {
            finglass_core::app::BalanceTone::Positive => "positive",
            finglass_core::app::BalanceTone::Zero => "zero",
            finglass_core::app::BalanceTone::Negative => "negative",
        }
        .into(),
    );
    ui.set_dashboard_income_title(view.income_title.clone().into());
    ui.set_dashboard_income_value(view.income_value.clone().into());
    ui.set_dashboard_expenses_title(view.expenses_title.clone().into());
    ui.set_dashboard_expenses_value(view.expenses_value.clone().into());
    ui.set_dashboard_pending_title(view.pending_title.clone().into());
    ui.set_dashboard_pending_value(view.pending_value.clone().into());
    ui.set_dashboard_pending_subtitle(view.pending_subtitle.clone().into());
    ui.set_dashboard_pending_enabled(view.pending_enabled);
    ui.set_dashboard_expenses_section_title(view.expenses_section_title.clone().into());
    ui.set_dashboard_expenses_empty_text(view.expenses_empty_text.clone().into());
    ui.set_dashboard_expense_rows(category_model(&view.expense_rows));
    ui.set_dashboard_transfers_note(view.transfers_note.clone().unwrap_or_default().into());
    ui.set_dashboard_chart_title(view.chart_title.clone().into());
    ui.set_dashboard_chart_empty_text(view.chart_empty_text.clone().unwrap_or_default().into());
    ui.set_dashboard_chart_axis_top(view.chart_axis_top.clone().into());
    ui.set_dashboard_chart_axis_mid(view.chart_axis_mid.clone().into());
    ui.set_dashboard_chart_axis_zero(view.chart_axis_zero.clone().into());
    ui.set_dashboard_month_bars(month_model(view));
    ui.set_dashboard_income_legend(view.income_legend.clone().into());
    ui.set_dashboard_expenses_legend(view.expenses_legend.clone().into());
    ui.set_dashboard_partial_note(view.partial_month_note.clone().into());
    ui.set_dashboard_budgets_title(view.budgets_title.clone().into());
    ui.set_dashboard_budget_rows(budget_model(view));
    ui.set_dashboard_has_budgets(!view.budgets.is_empty());
    ui.set_dashboard_month_progress(view.month_progress);
    ui.set_dashboard_tax_title(view.tax_title.clone().into());
    ui.set_dashboard_tax_hint(view.tax_hint.clone().into());
    ui.set_dashboard_tax_value(view.tax_value.clone().into());
    ui.set_dashboard_tax_slices(tax_model(view, language));
    ui.set_dashboard_upcoming_title(view.upcoming_title.clone().into());
    ui.set_dashboard_upcoming_rows(upcoming_model(view));
    ui.set_dashboard_has_upcoming(!view.upcoming.is_empty());
    ui.set_dashboard_recent_title(view.recent_title.clone().into());
    ui.set_dashboard_recent_empty_text(view.recent_empty_text.clone().into());
    ui.set_dashboard_recent_rows(transaction_model(view));
    ui.set_dashboard_view_all_label(view.view_all_label.clone().into());
}

fn category_model(rows: &[finglass_core::app::CategoryRowView]) -> ModelRc<DashboardCategoryRow> {
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

fn month_model(view: &DashboardView) -> ModelRc<DashboardMonthBar> {
    ModelRc::new(VecModel::from(
        view.month_bars
            .iter()
            .map(|bar| DashboardMonthBar {
                label: bar.label.clone().into(),
                income_fraction: bar.income_fraction,
                expenses_fraction: bar.expenses_fraction,
                partial: bar.partial,
            })
            .collect::<Vec<_>>(),
    ))
}

fn budget_model(view: &DashboardView) -> ModelRc<DashboardBudgetRow> {
    ModelRc::new(VecModel::from(
        view.budgets
            .iter()
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

fn upcoming_model(view: &DashboardView) -> ModelRc<DashboardUpcomingRow> {
    ModelRc::new(VecModel::from(
        view.upcoming
            .iter()
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

fn transaction_model(view: &DashboardView) -> ModelRc<DashboardTransactionRow> {
    ModelRc::new(VecModel::from(
        view.recent_transactions
            .iter()
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

fn tax_model(view: &DashboardView, language: Language) -> ModelRc<DashboardTaxSlice> {
    let mut slices = view.tax_deductions_by_category.clone();
    if slices.len() > 6 {
        let rest = slices.split_off(5);
        let minor: i64 = rest.iter().map(|slice| slice.amount.minor).sum();
        let currency = rest[0].amount.currency;
        slices.push(finglass_core::app::CategorySliceView {
            label: i18n::text(language, Key::ChartOther).to_owned(),
            amount: Money { minor, currency },
            color: "#9E9E9E".to_owned(),
            category_id: None,
            fraction: 0.0,
        });
    }

    let total: i64 = slices.iter().map(|slice| slice.amount.minor).sum();
    let mut start = -90.0_f32;
    let rows = slices
        .into_iter()
        .map(|slice| {
            let sweep = if total > 0 {
                (slice.amount.minor as f32 / total as f32 * 360.0 - 1.0).max(0.5)
            } else {
                0.0
            };
            let arc = arc_commands(start, sweep);
            start += slice.amount.minor as f32 / total.max(1) as f32 * 360.0;
            DashboardTaxSlice {
                label: slice.label.into(),
                amount_text: finglass_core::format::money(slice.amount, language, false).into(),
                colour: color(&slice.color),
                arc: arc.into(),
            }
        })
        .collect::<Vec<_>>();

    ModelRc::new(VecModel::from(rows))
}

fn arc_commands(start_degrees: f32, sweep_degrees: f32) -> String {
    const CENTER: f32 = 80.0;
    const RADIUS: f32 = 70.0;
    let start = start_degrees.to_radians();
    let end = (start_degrees + sweep_degrees).to_radians();
    let x1 = CENTER + RADIUS * start.cos();
    let y1 = CENTER + RADIUS * start.sin();
    let x2 = CENTER + RADIUS * end.cos();
    let y2 = CENTER + RADIUS * end.sin();
    let large_arc = if sweep_degrees > 180.0 { 1 } else { 0 };
    format!("M {x1:.2} {y1:.2} A {RADIUS:.2} {RADIUS:.2} 0 {large_arc} 1 {x2:.2} {y2:.2}")
}

fn color(hex: &str) -> Color {
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

    Color::from_rgb_u8(117, 117, 117)
}

fn configure_unlock(ui: &UnlockWindow, language: Language, theme: ThemePref, mode: &str, error: &str) {
    install_translations_unlock(ui, language);
    sync_theme(ui, theme);
    ui.set_mode(mode.into());
    ui.set_allow_change(mode == "unlock");
    ui.set_busy(false);
    ui.set_error_text(error.into());
    ui.set_profile_label(i18n::format(language, Key::UnlockProfile, &[SLUG.into()]).into());
    ui.set_passphrase_hint(i18n::format(language, Key::PassphraseHint, &[10i64.into()]).into());
}

fn configure_main(ui: &MainWindow, language: Language, theme: ThemePref, screen: &str, rows: &[app::AuditRow]) {
    install_translations_main(ui, language);
    sync_theme(ui, theme);
    ui.set_app_name("Finglass".into());
    ui.set_version(finglass_core::VERSION.into());
    ui.set_about_version_text(i18n::format(language, Key::AboutVersion, &[finglass_core::VERSION.into()]).into());
    ui.set_current_screen(screen.into());
    ui.set_theme_pref(theme.code().into());
    ui.set_language(language.code().into());
    ui.set_status_text("".into());
    ui.set_change_open(false);
    ui.set_change_busy(false);
    ui.set_change_error("".into());
    ui.set_audit_rows(audit_model(rows));
}

fn install_translations_unlock(ui: &UnlockWindow, language: Language) {
    let tr = ui.global::<Tr>();
    tr.on_text(translate);
    tr.set_language(language.code().into());
}

fn install_translations_main(ui: &MainWindow, language: Language) {
    let tr = ui.global::<Tr>();
    tr.on_text(translate);
    tr.set_language(language.code().into());
}

fn sync_theme(ui: &impl ThemeMode, theme: ThemePref) {
    ui.set_theme_mode(theme);
}

trait ThemeMode {
    fn set_theme_mode(&self, theme: ThemePref);
}

impl ThemeMode for UnlockWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        self.global::<Theme>().set_mode(theme.code().into());
    }
}

impl ThemeMode for MainWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        self.global::<Theme>().set_mode(theme.code().into());
    }
}

fn translate(key: SharedString, language: SharedString) -> SharedString {
    let language = Language::from_code(language.as_str()).unwrap_or(Language::En);
    match Key::from_name(key.as_str()) {
        Some(key) => i18n::text(language, key).into(),
        None => key,
    }
}

fn audit_model(rows: &[app::AuditRow]) -> ModelRc<AuditRow> {
    ModelRc::new(VecModel::from(
        rows.iter()
            .map(|row| AuditRow {
                time: row.time.clone().into(),
                action: row.action.clone().into(),
                actor: row.actor.clone().into(),
                details: row.details.clone().into(),
            })
            .collect::<Vec<_>>(),
    ))
}

fn render(window: &Rc<MinimalSoftwareWindow>, name: &str, width: u32, height: u32) -> SharedPixelBuffer<Rgb8Pixel> {
    window.set_size(PhysicalSize::new(width, height));
    window.request_redraw();

    let mut buffer = SharedPixelBuffer::<Rgb8Pixel>::new(width, height);
    let rendered = window.draw_if_needed(|renderer| {
        renderer.render(buffer.make_mut_slice(), width as usize);
    });
    assert!(rendered, "{name}: the software renderer did not draw");

    assert_eq!(buffer.width(), width, "{name}: width");
    assert_eq!(buffer.height(), height, "{name}: height");

    if let Some(directory) = std::env::var_os("FINGLASS_SCREENSHOTS") {
        write_png(Path::new(&directory), name, &buffer);
    }

    buffer
}

fn assert_varied(buffer: &SharedPixelBuffer<Rgb8Pixel>, name: &str) {
    let colours: HashSet<(u8, u8, u8)> = buffer
        .as_slice()
        .iter()
        .map(|pixel| (pixel.r, pixel.g, pixel.b))
        .collect();
    assert!(
        colours.len() > 20,
        "{name}: rendered one flat colour ({} unique colours)",
        colours.len()
    );
}

fn write_png(directory: &Path, name: &str, buffer: &SharedPixelBuffer<Rgb8Pixel>) {
    let _ = std::fs::create_dir_all(directory);
    let path = directory.join(format!("{name}.png"));
    let file = std::fs::File::create(&path).expect("create screenshot");
    let mut encoder = png::Encoder::new(BufWriter::new(file), buffer.width(), buffer.height());
    encoder.set_color(png::ColorType::Rgb);
    encoder.set_depth(png::BitDepth::Eight);
    let mut writer = encoder.write_header().expect("png header");
    writer.write_image_data(buffer.as_bytes()).expect("png data");
}
