//! Wiring between the Slint windows and `finglass-core`.
//!
//! Key derivation and every database call run on a background thread. Results come back through
//! `slint::invoke_from_event_loop`, so the event loop never waits for SQLCipher.

use std::sync::{Arc, Mutex};
use std::thread;

use finglass_core::app;
use finglass_core::app::dashboard::DashboardState;
use finglass_core::app::prefs::ThemePref;
use finglass_core::app::settings::SettingsState;
use finglass_core::app::transactions::{Period, StartArgs};
use finglass_core::app::unlock::{UnlockError, UnlockMode};
use finglass_core::db::Database;
use finglass_core::i18n::{self, Key, Language};
use finglass_core::model::{CategoryId, RuleId};
use finglass_core::paths::AppPaths;
use finglass_core::{APP_NAME, VERSION};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel, Weak};
use time::OffsetDateTime;
use zeroize::Zeroizing;

use crate::dashboard;
use crate::reports::ReportsController;
use crate::{AuditRow, MainWindow, Theme, Tr, UnlockWindow};

const SLUG: &str = "default";
const MIN_PASSPHRASE_LENGTH: i64 = 10;
const AUDIT_LIMIT: u32 = 100;

/// Shared, thread-safe state behind the two windows.
pub(crate) struct ShellState {
    pub(crate) language: Mutex<Language>,
    pub(crate) theme: Mutex<ThemePref>,
    pub(crate) db: Mutex<Option<Arc<Database>>>,
    pub(crate) dashboard: Mutex<DashboardState>,
    pub(crate) reports: Arc<ReportsController>,
    /// The filter the dashboard or a report last asked the Transactions page to show; M4c reads it.
    pub(crate) navigation: Mutex<StartArgs>,
    pub(crate) settings: Mutex<SettingsState>,
}

/// Runs the app shell against `paths`.
///
/// `main` resolves `AppPaths::from_env()` and passes the result here; tests can pass a temporary
/// root instead.
pub fn run(paths: AppPaths) -> Result<(), slint::PlatformError> {
    let initial_language = system_language();
    let today = OffsetDateTime::now_utc().date();
    let unlock = UnlockWindow::new()?;
    let main = MainWindow::new()?;
    let paths = Arc::new(paths);
    let reports = Arc::new(ReportsController::new(main.as_weak()));
    let state = Arc::new(ShellState {
        language: Mutex::new(initial_language),
        theme: Mutex::new(ThemePref::System),
        db: Mutex::new(None),
        dashboard: Mutex::new(DashboardState::new(initial_language, today)),
        reports,
        navigation: Mutex::new(StartArgs::default()),
        settings: Mutex::new(SettingsState::new(initial_language)),
    });

    install_translations_unlock(&unlock, initial_language);
    install_translations_main(&main, initial_language);
    apply_theme(&unlock, ThemePref::System);
    apply_theme(&main, ThemePref::System);

    configure_main_window(&main, initial_language);
    configure_unlock_window(&unlock, &paths, initial_language);

    wire_unlock(&unlock, &main, &paths, &state);
    wire_main(&main, &paths, &state);

    unlock.run()
}

fn configure_main_window(main: &MainWindow, language: Language) {
    main.set_app_name(APP_NAME.into());
    main.set_version(VERSION.into());
    main.set_about_version_text(about_version_text(language).into());
    main.set_current_screen("dashboard".into());
    main.set_theme_pref(ThemePref::System.code().into());
    main.set_language(language.code().into());
    main.set_status_text("".into());
    main.set_change_open(false);
    main.set_change_busy(false);
    main.set_change_error("".into());
    main.set_audit_rows(empty_audit_model());
    crate::settings::apply(main, &SettingsState::new(language));
}

fn configure_unlock_window(unlock: &UnlockWindow, paths: &AppPaths, language: Language) {
    let mode = paths
        .database_file(SLUG)
        .map(|file| app::unlock::mode_for(&file))
        .unwrap_or(UnlockMode::Create);

    unlock.set_mode(mode_name(mode).into());
    unlock.set_allow_change(mode == UnlockMode::Unlock);
    unlock.set_busy(false);
    unlock.set_error_text("".into());
    apply_language_unlock(unlock, language);
}

fn wire_unlock(unlock: &UnlockWindow, main: &MainWindow, paths: &Arc<AppPaths>, state: &Arc<ShellState>) {
    let unlock_weak = unlock.as_weak();
    let main_weak = main.as_weak();

    {
        let unlock_weak = unlock_weak.clone();
        let main_weak = main_weak.clone();
        let paths = paths.clone();
        let state = state.clone();
        unlock.on_unlock(move |input| {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            if ui.get_busy() {
                return;
            }
            ui.set_busy(true);
            ui.set_error_text("".into());

            let paths = paths.clone();
            let language = current_language(&state);
            let input = Zeroizing::new(input.to_string());
            spawn_open_work(
                move || app::unlock::unlock(&paths, SLUG, input.as_str()),
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                state.clone(),
            );
        });
    }

    {
        let unlock_weak = unlock_weak.clone();
        let main_weak = main_weak.clone();
        let paths = paths.clone();
        let state = state.clone();
        unlock.on_create(move |passphrase, repeat, acknowledged| {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            if ui.get_busy() {
                return;
            }
            ui.set_busy(true);
            ui.set_error_text("".into());

            let paths = paths.clone();
            let language = current_language(&state);
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            spawn_open_work(
                move || app::unlock::create(&paths, SLUG, passphrase.as_str(), repeat.as_str(), acknowledged),
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                state.clone(),
            );
        });
    }

    {
        let unlock_weak = unlock_weak.clone();
        let main_weak = main_weak.clone();
        let paths = paths.clone();
        let state = state.clone();
        unlock.on_change(move |current, passphrase, repeat, acknowledged| {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            if ui.get_busy() {
                return;
            }
            ui.set_busy(true);
            ui.set_error_text("".into());

            let paths = paths.clone();
            let language = current_language(&state);
            let current = Zeroizing::new(current.to_string());
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            spawn_open_work(
                move || {
                    let db = app::unlock::unlock(&paths, SLUG, current.as_str())?;
                    app::unlock::change(&db, passphrase.as_str(), repeat.as_str(), acknowledged)?;
                    Ok(db)
                },
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                state.clone(),
            );
        });
    }

    {
        let unlock_weak = unlock_weak.clone();
        unlock.on_show_change(move |change| {
            if let Some(ui) = unlock_weak.upgrade() {
                ui.set_mode(if change { "change" } else { "unlock" }.into());
                ui.set_error_text("".into());
            }
        });
    }
}

fn wire_main(main: &MainWindow, paths: &Arc<AppPaths>, state: &Arc<ShellState>) {
    state.reports.wire(main);

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_navigate(move |screen| {
            if let Some(ui) = main_weak.upgrade() {
                let dashboard = screen.as_str() == "dashboard";
                let reports = screen.as_str() == "reports";
                let settings = screen.as_str() == "settings";
                ui.set_current_screen(screen);
                if dashboard {
                    dashboard::spawn_load(&ui, &state, None);
                }
                if reports {
                    ui.set_reports_tab("summary".into());
                    state.reports.open_page();
                }
                if settings {
                    crate::settings::spawn_load(&ui, &state, &paths);
                }
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_previous_month(move || {
            if let Some(ui) = main_weak.upgrade() {
                dashboard::previous_month(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_next_month(move || {
            if let Some(ui) = main_weak.upgrade() {
                dashboard::next_month(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_current_month(move || {
            if let Some(ui) = main_weak.upgrade() {
                dashboard::current_month(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_category(move |category_id| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let dashboard = state.dashboard.lock().expect("dashboard state poisoned");
            let month = (dashboard.selected_month != dashboard.current_month).then_some(dashboard.selected_month);
            let args = if category_id < 0 {
                StartArgs {
                    month,
                    uncategorized_only: true,
                    ..Default::default()
                }
            } else {
                StartArgs {
                    month,
                    category_id: Some(CategoryId(i64::from(category_id))),
                    ..Default::default()
                }
            };
            drop(dashboard);
            *state.navigation.lock().expect("navigation state poisoned") = args;
            ui.set_current_screen("transactions".into());
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_uncategorized(move || {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let dashboard = state.dashboard.lock().expect("dashboard state poisoned");
            let args = StartArgs {
                month: (dashboard.selected_month != dashboard.current_month).then_some(dashboard.selected_month),
                uncategorized_only: true,
                ..Default::default()
            };
            drop(dashboard);
            *state.navigation.lock().expect("navigation state poisoned") = args;
            ui.set_current_screen("transactions".into());
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_all(move || {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            *state.navigation.lock().expect("navigation state poisoned") = StartArgs::default();
            ui.set_current_screen("transactions".into());
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_partner(move |partner| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            *state.navigation.lock().expect("navigation state poisoned") = StartArgs {
                partner: Some(partner.to_string()),
                ..Default::default()
            };
            ui.set_current_screen("transactions".into());
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_dashboard_open_accounts(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_current_screen("accounts".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_dashboard_open_settings(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_current_screen("settings".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_reports_open_category(move |category_id| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let today = OffsetDateTime::now_utc().date();
            let Some(snapshot) = state.reports.snapshot() else {
                return;
            };
            let Some(report) = snapshot.tax_report.as_ref() else {
                return;
            };
            if !crate::reports::linkable(report, today) {
                return;
            }

            let period = if report.from.year() == today.year() {
                Period::ThisYear
            } else {
                Period::LastYear
            };
            let args = if category_id < 0 {
                StartArgs {
                    period: Some(period),
                    uncategorized_only: true,
                    ..Default::default()
                }
            } else {
                StartArgs {
                    period: Some(period),
                    category_id: Some(CategoryId(i64::from(category_id))),
                    ..Default::default()
                }
            };
            *state.navigation.lock().expect("navigation state poisoned") = args;
            ui.set_current_screen("transactions".into());
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_dismiss_error(move || {
            if let Some(ui) = main_weak.upgrade() {
                let mut dashboard = state.dashboard.lock().expect("dashboard state poisoned");
                dashboard.error = None;
                dashboard::apply(&ui, &dashboard);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_open_change(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_change_error("".into());
                ui.set_change_open(true);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_close_change(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_change_open(false);
                ui.set_change_error("".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_dismiss_status(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_status_text("".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_dismiss_message(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::dismiss_messages(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_set_primary_currency(move |value| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::set_primary_currency(&ui, &state, value.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_add_category(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_add_category(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_edit_category(move |id| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_edit_category(&ui, &state, CategoryId(i64::from(id)));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_delete_category(move |id| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::request_delete_category(&ui, &state, CategoryId(i64::from(id)));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_add_default_categories(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::add_default_categories(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_fix_duplicate_colours(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::fix_duplicate_colours(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_add_rule(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_add_rule(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_edit_rule(move |id| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_edit_rule(&ui, &state, RuleId(i64::from(id)));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_delete_rule(move |id| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::request_delete_rule(&ui, &state, RuleId(i64::from(id)));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_import_rules(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::import_rules(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_export_rules(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::export_rules(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_save_tax_keywords(move |text| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::save_tax_keywords(&ui, &state, text.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_reset_tax_keywords(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::reset_tax_keywords(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_category_changed(move |name, colour| {
            if let Some(ui) = main_weak.upgrade() {
                let id = ui.get_category_dialog_id();
                crate::settings::validate_category(&ui, &state, name.as_str(), colour.as_str(), id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_rule_changed(move |pattern, priority| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::validate_rule(&ui, &state, pattern.as_str(), priority.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_category_dialog_save(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::save_category(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_settings_category_dialog_close(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_category_dialog_open(false);
                ui.set_category_dialog_error("".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_confirm_delete_category(move || {
            if let Some(ui) = main_weak.upgrade() {
                let id = i64::from(ui.get_category_delete_id());
                crate::settings::delete_category(&ui, &state, CategoryId(id));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_settings_cancel_delete_category(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_category_delete_open(false);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_rule_dialog_save(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::save_rule(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_settings_rule_dialog_close(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_rule_dialog_open(false);
                ui.set_rule_dialog_error("".into());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_confirm_delete_rule(move || {
            if let Some(ui) = main_weak.upgrade() {
                let id = i64::from(ui.get_rule_delete_id());
                crate::settings::delete_rule(&ui, &state, RuleId(id));
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_settings_cancel_delete_rule(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_rule_delete_open(false);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_confirm_rule_import(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::confirm_rule_import(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_cancel_rule_import(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_rule_import(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_set_theme(move |value| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let Some(theme) = ThemePref::from_code(value.as_str()) else {
                return;
            };

            *state.theme.lock().expect("theme state poisoned") = theme;
            apply_theme(&ui, theme);
            ui.set_theme_pref(theme.code().into());

            let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
                return;
            };
            let language = current_language(&state);
            let main_weak = main_weak.clone();
            thread::spawn(move || {
                let result = app::prefs::save_theme(&db, theme).and_then(|()| app::audit::recent(&db, AUDIT_LIMIT));
                let error_text = result
                    .as_ref()
                    .err()
                    .map(|_| i18n::text(language, Key::ErrorUnexpected).to_owned());
                let _ = slint::invoke_from_event_loop(move || {
                    if let Some(ui) = main_weak.upgrade() {
                        match result {
                            Ok(rows) => ui.set_audit_rows(audit_model(rows)),
                            Err(_) => {
                                if let Some(text) = error_text {
                                    ui.set_status_text(text.into());
                                }
                            }
                        }
                    }
                });
            });
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_set_language(move |value| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let Some(language) = Language::from_code(value.as_str()) else {
                return;
            };

            *state.language.lock().expect("language state poisoned") = language;
            apply_language_main(&ui, language);
            dashboard::spawn_load(&ui, &state, None);
            state.reports.set_language(language);
            if ui.get_current_screen() == "settings" {
                crate::settings::spawn_load(&ui, &state, &paths);
            }

            let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
                return;
            };
            let main_weak = main_weak.clone();
            thread::spawn(move || {
                let result =
                    app::prefs::save_language(&db, language).and_then(|()| app::audit::recent(&db, AUDIT_LIMIT));
                let error_text = i18n::text(language, Key::ErrorUnexpected).to_owned();
                let _ = slint::invoke_from_event_loop(move || {
                    if let Some(ui) = main_weak.upgrade() {
                        match result {
                            Ok(rows) => ui.set_audit_rows(audit_model(rows)),
                            Err(_) => ui.set_status_text(error_text.into()),
                        }
                    }
                });
            });
        });
    }

    {
        let main_weak = main.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        main.on_change_passphrase(move |current, passphrase, repeat, acknowledged| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            if ui.get_change_busy() {
                return;
            }
            let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
                return;
            };

            ui.set_change_busy(true);
            ui.set_change_error("".into());

            let language = current_language(&state);
            let paths = paths.clone();
            let main_weak = main_weak.clone();
            let current = Zeroizing::new(current.to_string());
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            thread::spawn(move || {
                let result = app::unlock::unlock(&paths, SLUG, current.as_str())
                    .and_then(|verified| {
                        drop(verified);
                        app::unlock::change(&db, passphrase.as_str(), repeat.as_str(), acknowledged)
                    })
                    .and_then(|()| app::audit::recent(&db, AUDIT_LIMIT).map_err(|_| UnlockError::Failed));
                let error_text = result.as_ref().err().map(|error| unlock_error_text(language, error));
                let _ = slint::invoke_from_event_loop(move || {
                    if let Some(ui) = main_weak.upgrade() {
                        ui.set_change_busy(false);
                        match result {
                            Ok(rows) => {
                                ui.set_change_open(false);
                                ui.set_audit_rows(audit_model(rows));
                            }
                            Err(_) => {
                                if let Some(text) = error_text {
                                    ui.set_change_error(text.into());
                                }
                            }
                        }
                    }
                });
            });
        });
    }
}

fn spawn_open_work(
    work: impl FnOnce() -> Result<Database, UnlockError> + Send + 'static,
    language: Language,
    unlock_weak: Weak<UnlockWindow>,
    main_weak: Weak<MainWindow>,
    state: Arc<ShellState>,
) {
    state.reports.reset();
    thread::spawn(move || {
        let outcome = work().and_then(|db| {
            let db = Arc::new(db);
            let prefs = app::prefs::load(&db, system_language()).map_err(|_| UnlockError::Failed)?;
            let rows = app::audit::recent(&db, AUDIT_LIMIT).map_err(|_| UnlockError::Failed)?;
            Ok((db, prefs, rows))
        });
        let error_text = outcome.as_ref().err().map(|error| unlock_error_text(language, error));

        let _ = slint::invoke_from_event_loop(move || {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            ui.set_busy(false);

            match outcome {
                Ok((db, prefs, rows)) => {
                    let Some(main) = main_weak.upgrade() else {
                        return;
                    };
                    *state.language.lock().expect("language state poisoned") = prefs.language;
                    *state.theme.lock().expect("theme state poisoned") = prefs.theme;

                    apply_language_main(&main, prefs.language);
                    apply_language_unlock(&ui, prefs.language);
                    apply_theme(&main, prefs.theme);
                    main.set_theme_pref(prefs.theme.code().into());
                    main.set_language(prefs.language.code().into());
                    main.set_audit_rows(audit_model(rows));
                    state.reports.attach(db.clone(), prefs.language);
                    *state.db.lock().expect("database state poisoned") = Some(db);

                    dashboard::spawn_load(&main, &state, None);
                    let _ = main.show();
                    let _ = ui.hide();
                }
                Err(_) => {
                    if let Some(text) = error_text {
                        ui.set_error_text(text.into());
                    }
                }
            }
        });
    });
}

fn install_translations_unlock(unlock: &UnlockWindow, language: Language) {
    let tr = unlock.global::<Tr>();
    tr.on_text(|key, language| translate(&key, &language));
    tr.set_language(language.code().into());
}

fn install_translations_main(main: &MainWindow, language: Language) {
    let tr = main.global::<Tr>();
    tr.on_text(|key, language| translate(&key, &language));
    tr.set_language(language.code().into());
}

fn translate(key: &SharedString, language: &SharedString) -> SharedString {
    let language = Language::from_code(language.as_str()).unwrap_or(Language::En);
    match Key::from_name(key.as_str()) {
        Some(key) => i18n::text(language, key).into(),
        // Showing the name itself makes a missing key obvious instead of hiding it.
        None => key.clone(),
    }
}

fn apply_language_unlock(unlock: &UnlockWindow, language: Language) {
    unlock.global::<Tr>().set_language(language.code().into());
    unlock.set_profile_label(profile_label(language, SLUG).into());
    unlock.set_passphrase_hint(passphrase_hint(language).into());
}

fn apply_language_main(main: &MainWindow, language: Language) {
    main.global::<Tr>().set_language(language.code().into());
    main.set_language(language.code().into());
    main.set_about_version_text(about_version_text(language).into());
}

fn apply_theme(window: &impl HasThemeMode, theme: ThemePref) {
    window.set_theme_mode(theme);
}

trait HasThemeMode {
    fn set_theme_mode(&self, theme: ThemePref);
}

impl HasThemeMode for UnlockWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        self.global::<Theme>().set_mode(theme.code().into());
    }
}

impl HasThemeMode for MainWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        self.global::<Theme>().set_mode(theme.code().into());
    }
}

fn profile_label(language: Language, slug: &str) -> String {
    i18n::format(language, Key::UnlockProfile, &[slug.into()])
}

fn passphrase_hint(language: Language) -> String {
    i18n::format(language, Key::PassphraseHint, &[MIN_PASSPHRASE_LENGTH.into()])
}

fn about_version_text(language: Language) -> String {
    i18n::format(language, Key::AboutVersion, &[VERSION.into()])
}

fn unlock_error_text(language: Language, error: &UnlockError) -> String {
    match error {
        UnlockError::WrongPassphrase => i18n::text(language, Key::WrongPassphrase).to_owned(),
        UnlockError::TooShort => i18n::format(language, Key::PassphraseTooShort, &[MIN_PASSPHRASE_LENGTH.into()]),
        UnlockError::Spaces => i18n::text(language, Key::PassphraseSpaces).to_owned(),
        UnlockError::Mismatch => i18n::text(language, Key::PassphraseMismatch).to_owned(),
        UnlockError::NotAcknowledged => i18n::text(language, Key::PassphraseAcknowledge).to_owned(),
        UnlockError::NotEncrypted => i18n::text(language, Key::ErrorNotEncrypted).to_owned(),
        UnlockError::TooNew => i18n::text(language, Key::ErrorDatabaseTooNew).to_owned(),
        UnlockError::Failed => i18n::format(
            language,
            Key::UnlockFailed,
            &[i18n::text(language, Key::ErrorUnexpected).into()],
        ),
    }
}

fn mode_name(mode: UnlockMode) -> &'static str {
    match mode {
        UnlockMode::Create => "create",
        UnlockMode::Unlock => "unlock",
        UnlockMode::Change => "change",
    }
}

fn audit_model(rows: Vec<app::AuditRow>) -> ModelRc<AuditRow> {
    ModelRc::new(VecModel::from(
        rows.into_iter()
            .map(|row| AuditRow {
                time: row.time.into(),
                action: row.action.into(),
                actor: row.actor.into(),
                details: row.details.into(),
            })
            .collect::<Vec<_>>(),
    ))
}

fn empty_audit_model() -> ModelRc<AuditRow> {
    ModelRc::new(VecModel::from(Vec::<AuditRow>::new()))
}

fn current_language(state: &ShellState) -> Language {
    *state.language.lock().expect("language state poisoned")
}

/// The system language from the usual POSIX locale variables, falling back to English.
fn system_language() -> Language {
    let locale = std::env::var("LC_ALL")
        .ok()
        .filter(|value| !value.is_empty())
        .or_else(|| std::env::var("LC_MESSAGES").ok().filter(|value| !value.is_empty()))
        .or_else(|| std::env::var("LANG").ok().filter(|value| !value.is_empty()));

    locale.as_deref().map(language_from_locale).unwrap_or(Language::En)
}

fn language_from_locale(locale: &str) -> Language {
    let locale = locale.to_ascii_lowercase();
    if locale.starts_with("de") {
        Language::De
    } else if locale.starts_with("fr") {
        Language::Fr
    } else {
        Language::En
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_unlock_error_has_text_in_every_language() {
        for &language in Language::ALL {
            for error in [
                UnlockError::WrongPassphrase,
                UnlockError::TooShort,
                UnlockError::Spaces,
                UnlockError::Mismatch,
                UnlockError::NotAcknowledged,
                UnlockError::NotEncrypted,
                UnlockError::TooNew,
                UnlockError::Failed,
            ] {
                assert!(
                    !unlock_error_text(language, &error).is_empty(),
                    "{language:?} / {error:?}"
                );
            }
        }
    }

    #[test]
    fn unknown_translation_keys_fall_back_to_the_name() {
        let key = SharedString::from("NoSuchKey");
        assert_eq!(translate(&key, &SharedString::from("en")), key);
    }

    #[test]
    fn known_translation_keys_use_the_language() {
        let key = SharedString::from("NavDashboard");
        assert_eq!(
            translate(&key, &SharedString::from("de")),
            SharedString::from(i18n::text(Language::De, Key::NavDashboard))
        );
    }

    #[test]
    fn locale_detection_recognizes_supported_languages() {
        assert_eq!(language_from_locale("de_DE.UTF-8"), Language::De);
        assert_eq!(language_from_locale("fr-FR"), Language::Fr);
        assert_eq!(language_from_locale("en_US"), Language::En);
        assert_eq!(language_from_locale("es_ES"), Language::En);
        assert_eq!(language_from_locale(""), Language::En);
    }
}
