//! Wiring between the Slint windows and `finglass-core`.
//!
//! Key derivation and every database call run on a background thread. Results come back through
//! `slint::invoke_from_event_loop`, so the event loop never waits for SQLCipher.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;

use finglass_core::app;
use finglass_core::app::accounts::{AccountsState, Busy};
use finglass_core::app::banking::BankingState;
use finglass_core::app::dashboard::{DashboardState, local_today};
use finglass_core::app::findesk::FindeskImportState;
use finglass_core::app::prefs::ThemePref;
use finglass_core::app::settings::SettingsState;
use finglass_core::app::transactions::{Period, StartArgs};
use finglass_core::app::unlock::{UnlockError, UnlockMode};
use finglass_core::db::Database;
use finglass_core::i18n::{self, Key, Language};
use finglass_core::instance::{InstanceError, InstanceLock};
use finglass_core::model::{CategoryId, RuleId};
use finglass_core::paths::AppPaths;
use finglass_core::profiles;
use finglass_core::sync::CredentialStore;
use finglass_core::sync::system_credential_store;
use finglass_core::{APP_NAME, VERSION};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel, Weak};
use time::OffsetDateTime;
use zeroize::Zeroizing;

use crate::accounts;
use crate::banking;
use crate::dashboard;
use crate::documents::DocumentsController;
use crate::jobs::JobTracker;
use crate::reports::ReportsController;
use crate::transactions::TransactionsController;
use crate::{AuditRow, MainWindow, Theme, Tr, UnlockWindow};

pub(crate) const MIN_PASSPHRASE_LENGTH: i64 = 10;
const AUDIT_LIMIT: u32 = 100;

/// Shared, thread-safe state behind the two windows.
pub(crate) struct ShellState {
    pub(crate) language: Mutex<Language>,
    pub(crate) theme: Mutex<ThemePref>,
    pub(crate) db: Mutex<Option<Arc<Database>>>,
    /// The one operating-system credential store for the whole app, created at startup.
    pub(crate) credentials: Arc<dyn CredentialStore>,
    pub(crate) dashboard: Mutex<DashboardState>,
    pub(crate) reports: Arc<ReportsController>,
    /// The filter the dashboard or a report last asked the Transactions page to show; M4c reads it.
    pub(crate) navigation: Mutex<StartArgs>,
    pub(crate) settings: Mutex<SettingsState>,
    /// The one-time FinDesk import's folder, passphrase prompt and result.
    pub(crate) findesk: Mutex<FindeskImportState>,
    pub(crate) accounts: Mutex<AccountsState>,
    /// The bank-login part of the Accounts page.
    pub(crate) banking: Mutex<BankingState>,
    /// The open profile's prompt bridge, replaced when the profile changes.
    pub(crate) banking_bridge: Mutex<Option<crate::banking::BridgeHandle>>,
    /// The one profile busy flag; the Accounts state and every sync share this handle.
    pub(crate) profile_busy: Mutex<Busy>,
    /// The data root, used to build a banking service for the open profile.
    pub(crate) paths: Arc<AppPaths>,
    /// The Accounts page's own work guard. It is separate from the profile's busy flag, which the core reserves for
    /// sync and import work (and reads before it allows a delete).
    pub(crate) accounts_work: Busy,
    pub(crate) documents: Arc<DocumentsController>,
    /// The profile opened now (`None` is the default profile). Read at call time, so a switch is visible everywhere.
    pub(crate) slug: Mutex<Option<String>>,
    /// The data a startup restore moved aside, shown in Settings.
    pub(crate) restored_from: Mutex<Option<PathBuf>>,
    pub(crate) transactions: Arc<TransactionsController>,
    /// The one shell-wide job counter every background action shares.
    pub(crate) jobs: JobTracker,
    /// Set synchronously before a switch starts, so a second request is refused immediately.
    pub(crate) switching: AtomicBool,
}

impl ShellState {
    /// The open profile's slug, read at call time.
    pub(crate) fn slug(&self) -> Option<String> {
        self.slug.lock().expect("profile state poisoned").clone()
    }

    /// The folder a restore moved aside, read at call time.
    pub(crate) fn restored_from(&self) -> Option<PathBuf> {
        self.restored_from.lock().expect("restore state poisoned").clone()
    }
}

/// Runs the app shell against `paths`.
///
/// `main` resolves `AppPaths::from_env()` and passes the result here; tests can pass a temporary
/// root instead.
pub fn run(paths: AppPaths) -> Result<(), slint::PlatformError> {
    let initial_language = system_language();
    let now = OffsetDateTime::now_utc();
    let today = local_today();

    // The instance lock comes first, before the staged restore and before any other file access.
    let startup = prepare_startup(&paths, now);

    let unlock = UnlockWindow::new()?;
    install_translations_unlock(&unlock, initial_language);
    apply_theme(&unlock, ThemePref::System);

    let (lock, startup) = match startup {
        Ok(startup) => startup,
        Err(refusal) => {
            configure_startup_error(&unlock, &paths, initial_language, &refusal.message(initial_language));
            return unlock.run();
        }
    };

    let main = MainWindow::new()?;
    let paths = Arc::new(paths);

    // Only `run` resolves the FinDesk root; tests and screenshots pass their own fixture roots.
    let migration_offer = app::findesk::default_findesk_start_dir()
        .and_then(|root| app::migration::discover(&root))
        .filter(|install| app::migration::should_offer(&paths, Some(install)));

    let jobs = JobTracker::new();
    let reports = Arc::new(ReportsController::new(main.as_weak(), jobs.clone()));
    let documents = Arc::new(DocumentsController::new(
        main.as_weak(),
        paths.clone(),
        app::startup::slug_key(startup.slug.as_deref()),
        jobs.clone(),
    ));
    let transactions = Arc::new(TransactionsController::new(
        main.as_weak(),
        paths.clone(),
        app::startup::slug_key(startup.slug.as_deref()),
        jobs.clone(),
    ));
    let profile_busy = Busy::default();
    transactions.set_profile_busy(profile_busy.clone());
    let state = Arc::new(ShellState {
        language: Mutex::new(initial_language),
        theme: Mutex::new(ThemePref::System),
        db: Mutex::new(None),
        credentials: system_credential_store(),
        dashboard: Mutex::new(DashboardState::new(initial_language, today)),
        reports,
        navigation: Mutex::new(StartArgs::default()),
        settings: Mutex::new(SettingsState::new(initial_language)),
        findesk: Mutex::new(FindeskImportState::new(initial_language)),
        accounts: Mutex::new(new_accounts_state(initial_language, profile_busy.clone())),
        banking: Mutex::new(BankingState::new(initial_language)),
        banking_bridge: Mutex::new(None),
        profile_busy: Mutex::new(profile_busy),
        paths: paths.clone(),
        accounts_work: Busy::default(),
        documents,
        slug: Mutex::new(startup.slug.clone()),
        restored_from: Mutex::new(startup.restored_from.clone()),
        transactions,
        jobs,
        switching: AtomicBool::new(false),
    });
    *state.banking_bridge.lock().expect("banking bridge poisoned") = Some(crate::banking::BridgeHandle::new(
        main.as_weak(),
        Arc::downgrade(&state),
    ));

    install_translations_main(&main, initial_language);
    apply_theme(&main, ThemePref::System);

    configure_main_window(&main, initial_language, startup.restored_from.as_deref());
    configure_unlock_window(&unlock, &paths, initial_language, &startup);

    wire_unlock(&unlock, &main, &paths, &state);
    if let Some(install) = migration_offer {
        crate::migration::wire(&unlock, &main, &paths, &state, startup.clone(), install);
    }
    wire_main(&main, &unlock, &paths, &state);

    {
        let state = state.clone();
        main.window().on_close_requested(move || {
            if let Some(bridge) = banking::current_bridge(&state) {
                bridge.cancel_pending();
            }
            slint::CloseRequestResponse::HideWindow
        });
    }

    let result = unlock.run();
    drop(lock);
    result
}

/// Why the startup path did not reach the normal unlock gate.
enum StartupRefusal {
    /// Another Finglass process holds this data root's lock.
    AlreadyRunning,
    /// The lock could not be taken, or the profile's data could not be prepared.
    Failed,
}

impl StartupRefusal {
    fn message(&self, language: Language) -> String {
        let key = match self {
            Self::AlreadyRunning => Key::ErrorAlreadyRunning,
            Self::Failed => Key::ErrorStartupFailed,
        };
        i18n::text(language, key).to_owned()
    }
}

/// Takes the instance lock and prepares the active profile, in that order.
///
/// `run` keeps the returned lock alive for the whole event loop. Tests call this directly so they can check the
/// refusal and the untouched staged restore without starting the event loop.
fn prepare_startup(
    paths: &AppPaths,
    now: OffsetDateTime,
) -> Result<(InstanceLock, app::startup::Startup), StartupRefusal> {
    let lock = match InstanceLock::acquire(paths) {
        Ok(lock) => lock,
        Err(InstanceError::AlreadyRunning) => return Err(StartupRefusal::AlreadyRunning),
        Err(InstanceError::Io(_)) => return Err(StartupRefusal::Failed),
    };

    match app::startup::prepare(paths, now) {
        Ok(startup) => Ok((lock, startup)),
        Err(_) => Err(StartupRefusal::Failed),
    }
}

fn configure_startup_error(unlock: &UnlockWindow, paths: &AppPaths, language: Language, message: &str) {
    unlock.set_mode("unlock".into());
    unlock.set_allow_change(false);
    unlock.set_busy(false);
    unlock.set_restored(false);
    unlock.set_startup_error(true);
    unlock.set_error_text(message.into());
    apply_language_unlock(unlock, paths, language, None);
    unlock.set_profile_label("".into());
}

fn configure_main_window(main: &MainWindow, language: Language, restored_from: Option<&Path>) {
    main.set_app_name(APP_NAME.into());
    main.set_version(VERSION.into());
    main.set_about_version_text(about_version_text(language).into());
    main.set_current_screen("dashboard".into());
    main.set_theme_pref(ThemePref::System.code().into());
    main.set_language(language.code().into());
    main.set_sidebar_collapsed(false);
    main.set_language_menu_open(false);
    main.set_dashboard_month_menu_open(false);
    main.set_status_text("".into());
    main.set_change_open(false);
    main.set_change_busy(false);
    main.set_change_error("".into());
    main.set_audit_rows(empty_audit_model());
    main.set_profile_menu_open(false);
    main.set_profile_dialog_open(false);
    main.set_profile_delete_open(false);
    main.set_profile_dialog_error("".into());
    main.set_profile_rows(ModelRc::new(VecModel::from(Vec::<crate::ProfileRow>::new())));
    main.set_current_profile_name("".into());
    crate::settings::apply(main, &SettingsState::new(language), restored_from);
    crate::settings::apply_findesk(main, &FindeskImportState::new(language));
    main.set_findesk_passphrase_open(false);
    main.set_findesk_passphrase("".into());
    main.set_accounts_dialog("".into());
    main.set_accounts_dialog_error("".into());
    main.set_accounts_dialog_online(false);
    main.set_accounts_add_bank_search("".into());
    main.set_accounts_add_bank_central_note("".into());
    main.set_accounts_detail_open(false);
}

pub(crate) fn configure_unlock_window(
    unlock: &UnlockWindow,
    paths: &AppPaths,
    language: Language,
    startup: &app::startup::Startup,
) {
    let slug = app::startup::slug_key(startup.slug.as_deref());
    let mode = paths
        .database_file(slug)
        .map(|file| app::unlock::mode_for(&file))
        .unwrap_or(UnlockMode::Create);

    unlock.set_mode(mode_name(mode).into());
    unlock.set_allow_change(mode == UnlockMode::Unlock);
    unlock.set_busy(false);
    unlock.set_restored(startup.restored_from.is_some());
    unlock.set_startup_error(false);
    unlock.set_error_text("".into());
    apply_language_unlock(unlock, paths, language, startup.slug.as_deref());
    apply_leftovers(unlock, paths, language);
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

            let unlock_paths = paths.clone();
            let language = current_language(&state);
            let slug = state.slug();
            let input = Zeroizing::new(input.to_string());
            spawn_open_work(
                move || app::unlock::unlock(&unlock_paths, app::startup::slug_key(slug.as_deref()), input.as_str()),
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                paths.clone(),
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

            let unlock_paths = paths.clone();
            let language = current_language(&state);
            let slug = state.slug();
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            spawn_open_work(
                move || {
                    app::unlock::create(
                        &unlock_paths,
                        app::startup::slug_key(slug.as_deref()),
                        passphrase.as_str(),
                        repeat.as_str(),
                        acknowledged,
                    )
                },
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                paths.clone(),
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

            let unlock_paths = paths.clone();
            let language = current_language(&state);
            let slug = state.slug();
            let current = Zeroizing::new(current.to_string());
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            spawn_open_work(
                move || {
                    let db =
                        app::unlock::unlock(&unlock_paths, app::startup::slug_key(slug.as_deref()), current.as_str())?;
                    app::unlock::change(&db, passphrase.as_str(), repeat.as_str(), acknowledged)?;
                    Ok(db)
                },
                language,
                unlock_weak.clone(),
                main_weak.clone(),
                paths.clone(),
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

fn wire_main(main: &MainWindow, unlock: &UnlockWindow, paths: &Arc<AppPaths>, state: &Arc<ShellState>) {
    state.reports.wire(main);
    state.documents.wire(main);
    state.transactions.wire(main);

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_navigate(move |screen| {
            if let Some(ui) = main_weak.upgrade() {
                let dashboard = screen.as_str() == "dashboard";
                let screen_is_documents = screen.as_str() == "documents";
                let reports = screen.as_str() == "reports";
                let settings = screen.as_str() == "settings";
                let accounts_screen = screen.as_str() == "accounts";
                let transactions_screen = screen.as_str() == "transactions";
                ui.set_current_screen(screen);
                if dashboard {
                    dashboard::spawn_open(&ui, &state);
                }
                if screen_is_documents {
                    state.documents.open_page();
                }
                if reports {
                    ui.set_reports_tab("summary".into());
                    state.reports.open_page();
                }
                if settings {
                    crate::settings::spawn_load(&ui, &state, &paths);
                }
                if accounts_screen {
                    accounts::spawn_load(&ui, &state);
                }
                if transactions_screen {
                    let args = std::mem::take(&mut *state.navigation.lock().expect("navigation state poisoned"));
                    state.transactions.open_page(args);
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
        main.on_dashboard_select_month(move |index| {
            if let Some(ui) = main_weak.upgrade() {
                dashboard::select_month(&ui, &state, index);
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
            ui.set_current_screen("transactions".into());
            state.transactions.open_page(args);
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
            ui.set_current_screen("transactions".into());
            state.transactions.open_page(args);
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_all(move || {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            ui.set_current_screen("transactions".into());
            state.transactions.open_page(StartArgs::default());
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_partner(move |partner| {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            ui.set_current_screen("transactions".into());
            state.transactions.open_page(StartArgs {
                partner: Some(partner.to_string()),
                ..Default::default()
            });
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_dashboard_open_accounts(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_current_screen("accounts".into());
                accounts::spawn_load(&ui, &state);
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
            ui.set_current_screen("transactions".into());
            state.transactions.open_page(args);
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
        let state = state.clone();
        main.on_accounts_open_add_bank(move || {
            if let Some(ui) = main_weak.upgrade() {
                banking::open_add_bank(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_accounts_bank_search_changed(move |query| {
            if let Some(ui) = main_weak.upgrade() {
                banking::update_bank_search(&ui, query.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_pick_bank_suggestion(move |name, blz, bic| {
            if let Some(ui) = main_weak.upgrade() {
                banking::pick_bank_suggestion(&ui, &state, name.as_str(), blz.as_str(), bic.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_bank_blz_changed(move |name, blz, bic| {
            if let Some(ui) = main_weak.upgrade() {
                banking::update_bank_blz(&ui, &state, name.as_str(), blz.as_str(), bic.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_add_online_bank(move |name, blz, bic, user_id, server_url| {
            if let Some(ui) = main_weak.upgrade() {
                banking::submit_add_online_bank(
                    &ui,
                    &state,
                    name.as_str(),
                    blz.as_str(),
                    bic.as_str(),
                    user_id.as_str(),
                    server_url.as_str(),
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_connection(move |bank_id, name, blz, user_id, server_url| {
            if let Some(ui) = main_weak.upgrade() {
                banking::submit_connection(
                    &ui,
                    &state,
                    bank_id,
                    name.as_str(),
                    blz.as_str(),
                    user_id.as_str(),
                    server_url.as_str(),
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_accounts_forget_pin(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                banking::forget_pin(&ui, &state, &paths, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_accounts_submit_history(move |bank_id, date| {
            if let Some(ui) = main_weak.upgrade() {
                banking::submit_history(&ui, &state, &paths, bank_id, date.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_accounts_clear_history(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                banking::clear_history(&ui, &state, &paths, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_accounts_sync_all(move || {
            if let Some(ui) = main_weak.upgrade() {
                banking::start_sync_all(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_accounts_bank_menu(move |bank_id, action| {
            if let Some(ui) = main_weak.upgrade() {
                banking::handle_bank_menu(&ui, &state, &paths, bank_id, action.as_str());
            }
        });
    }

    {
        let state = state.clone();
        main.on_banking_answer_pin(move |value, remember| {
            if let Some(bridge) = banking::current_bridge(&state) {
                banking::answer_pin(&bridge, Some(value.as_str()), remember);
            }
        });
    }

    {
        let state = state.clone();
        main.on_banking_answer_choice(move |value| {
            if let Some(bridge) = banking::current_bridge(&state) {
                banking::answer_choice(&bridge, Some(value.as_str()));
            }
        });
    }

    {
        let state = state.clone();
        main.on_banking_answer_tan(move |value| {
            if let Some(bridge) = banking::current_bridge(&state) {
                banking::answer_tan(&bridge, Some(value.as_str()));
            }
        });
    }

    {
        let state = state.clone();
        main.on_banking_answer_decoupled(move |confirmed| {
            if let Some(bridge) = banking::current_bridge(&state) {
                banking::answer_decoupled(&bridge, confirmed);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_banking_answer_cancel(move || {
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            if let Some(bridge) = banking::current_bridge(&state) {
                banking::cancel_prompt(&bridge, ui.get_banking_prompt().as_str());
            }
        });
    }

    {
        let state = state.clone();
        main.on_banking_clear_info(move || {
            if let Some(bridge) = banking::current_bridge(&state) {
                bridge.clear_info();
            }
        });
    }

    {
        let main_weak = main.as_weak();
        main.on_accounts_close_dialog(move || {
            if let Some(ui) = main_weak.upgrade() {
                accounts::close_dialog(&ui);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_add_bank(move |name| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::submit_add_bank(&ui, &state, name.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_add_account(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_add_account(&ui, &state, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_add_account(move |bank_id, name, identifier, account_type, currency, balance| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::submit_add_account(
                    &ui,
                    &state,
                    &accounts::AddAccountRequest {
                        bank_id,
                        name: name.as_str(),
                        identifier: identifier.as_str(),
                        account_type: account_type.as_str(),
                        currency: currency.as_str(),
                        balance: balance.as_str(),
                    },
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_balance(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_balance(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_balance(move |account_id, balance, account_type| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::submit_balance(&ui, &state, account_id, balance.as_str(), account_type.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_rename(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_rename(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_submit_rename(move |account_id, name| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::submit_rename(&ui, &state, account_id, name.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_delete_account(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_delete_account(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_confirm_delete_account(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::confirm_delete_account(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_delete_bank(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_delete_bank(&ui, &state, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_confirm_delete_bank(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::confirm_delete_bank(&ui, &state, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_close_detail(move || {
            if let Some(ui) = main_weak.upgrade() {
                accounts::close_detail(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_select_days(move |account_id, days| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::select_days(&ui, &state, account_id, days);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_select(move |kind, group_kind, id, account_type| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::select(
                    &ui,
                    &state,
                    kind.as_str(),
                    group_kind.as_str(),
                    id,
                    account_type.as_str(),
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_toggle_group(move |kind, group_kind, id, account_type| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::toggle_group(
                    &ui,
                    &state,
                    kind.as_str(),
                    group_kind.as_str(),
                    id,
                    account_type.as_str(),
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_mark_read(move |kind, group_kind, id, account_type| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::mark_read(
                    &ui,
                    &state,
                    kind.as_str(),
                    group_kind.as_str(),
                    id,
                    account_type.as_str(),
                );
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_toggle_bank(move |bank_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::toggle_bank(&ui, &state, bank_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_forget_deleted(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::forget_deleted(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_new_group(move |name| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::new_group(&ui, &state, name.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_rename_group(move |id, name| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::rename_group(&ui, &state, id, name.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_delete_group(move |id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::delete_group(&ui, &state, id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_move_group(move |id, up| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::move_group(&ui, &state, id, up);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_move_account(move |account_id, group_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::move_account(&ui, &state, account_id, group_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_filter_changed(move |text| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::filter_changed(&ui, &state, text.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_dismiss_message(move || {
            if let Some(ui) = main_weak.upgrade() {
                accounts::dismiss_message(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_detail(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                accounts::open_detail(&ui, &state, account_id);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_accounts_open_transactions(move |account_id| {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_current_screen("transactions".into());
                state.transactions.open_page(StartArgs {
                    account_id: Some(finglass_core::model::AccountId(i64::from(account_id))),
                    ..Default::default()
                });
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
        let paths = paths.clone();
        main.on_settings_create_backup(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::create_backup(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_restore_backup(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::restore_backup(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_cancel_pending_restore(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_pending_restore(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_confirm_restore(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::confirm_restore(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_cancel_restore(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_restore_choice(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_confirm_restore_passphrase(move |passphrase| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::confirm_restore_passphrase(&ui, &state, &paths, passphrase.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_cancel_restore_passphrase(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_restore_passphrase(&ui, &state);
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
        main.on_settings_import_findesk(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::import_findesk(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_findesk_passphrase_submit(move |passphrase| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::submit_findesk_passphrase(&ui, &state, &paths, passphrase.as_str());
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_findesk_passphrase_cancel(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_findesk_passphrase(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_findesk_report_close(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::close_findesk_report(&ui, &state);
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
            let jobs = state.jobs.clone();
            let generation = jobs.begin();
            let main_weak = main_weak.clone();
            thread::spawn(move || {
                let result = app::prefs::save_theme(&db, theme).and_then(|()| app::audit::recent(&db, AUDIT_LIMIT));
                let error_text = result
                    .as_ref()
                    .err()
                    .map(|_| i18n::text(language, Key::ErrorUnexpected).to_owned());
                let _ = slint::invoke_from_event_loop(move || {
                    jobs.end();
                    if !jobs.is_current(generation) {
                        return;
                    }
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
            let findesk = {
                let mut findesk = state.findesk.lock().expect("findesk state poisoned");
                findesk.language = language;
                findesk.clone()
            };
            crate::settings::apply_findesk(&ui, &findesk);
            if ui.get_current_screen() == "settings" {
                crate::settings::spawn_load(&ui, &state, &paths);
            }
            state.documents.set_language(language);
            state.transactions.set_language(language);

            let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
                return;
            };
            let jobs = state.jobs.clone();
            let generation = jobs.begin();
            let main_weak = main_weak.clone();
            thread::spawn(move || {
                let result =
                    app::prefs::save_language(&db, language).and_then(|()| app::audit::recent(&db, AUDIT_LIMIT));
                let error_text = i18n::text(language, Key::ErrorUnexpected).to_owned();
                let _ = slint::invoke_from_event_loop(move || {
                    jobs.end();
                    if !jobs.is_current(generation) {
                        return;
                    }
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
        let state = state.clone();
        main.on_sidebar_collapsed_changed(move |collapsed| {
            let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
                return;
            };
            let language = current_language(&state);
            let jobs = state.jobs.clone();
            let generation = jobs.begin();
            let main_weak = main_weak.clone();
            thread::spawn(move || {
                let result = app::prefs::save_sidebar_collapsed(&db, collapsed)
                    .and_then(|()| app::audit::recent(&db, AUDIT_LIMIT));
                let error_text = i18n::text(language, Key::ErrorUnexpected).to_owned();
                let _ = slint::invoke_from_event_loop(move || {
                    jobs.end();
                    if !jobs.is_current(generation) {
                        return;
                    }
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
            let jobs = state.jobs.clone();
            let generation = jobs.begin();
            let main_weak = main_weak.clone();
            let slug = state.slug();
            let current = Zeroizing::new(current.to_string());
            let passphrase = Zeroizing::new(passphrase.to_string());
            let repeat = Zeroizing::new(repeat.to_string());
            thread::spawn(move || {
                let result = app::unlock::unlock(&paths, app::startup::slug_key(slug.as_deref()), current.as_str())
                    .and_then(|verified| {
                        drop(verified);
                        app::unlock::change(&db, passphrase.as_str(), repeat.as_str(), acknowledged)
                    })
                    .and_then(|()| app::audit::recent(&db, AUDIT_LIMIT).map_err(|_| UnlockError::Failed));
                let error_text = result.as_ref().err().map(|error| unlock_error_text(language, error));
                let _ = slint::invoke_from_event_loop(move || {
                    jobs.end();
                    if !jobs.is_current(generation) {
                        return;
                    }
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

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        let unlock_weak = unlock.as_weak();
        main.on_profile_switch(move |index| {
            if let (Some(ui), Some(unlock)) = (main_weak.upgrade(), unlock_weak.upgrade()) {
                switch_profile(&ui, &unlock, &paths, &state, index);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        let unlock_weak = unlock.as_weak();
        main.on_settings_switch_profile(move |index| {
            if let (Some(ui), Some(unlock)) = (main_weak.upgrade(), unlock_weak.upgrade()) {
                switch_profile(&ui, &unlock, &paths, &state, index);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_new_profile(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_new_profile(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_rename_profile(move |index| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::open_rename_profile(&ui, &state, &paths, index);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_profile_dialog_close(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::close_profile_dialog(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_profile_dialog_save(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::save_profile(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_delete_profile(move |index| {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::request_delete_profile(&ui, &state, &paths, index);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_settings_confirm_delete_profile(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::confirm_delete_profile(&ui, &state, &paths);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        main.on_settings_cancel_delete_profile(move || {
            if let Some(ui) = main_weak.upgrade() {
                crate::settings::cancel_delete_profile(&ui, &state);
            }
        });
    }

    {
        let main_weak = main.as_weak();
        let state = state.clone();
        let paths = paths.clone();
        main.on_profile_manage(move || {
            if let Some(ui) = main_weak.upgrade() {
                ui.set_profile_menu_open(false);
                ui.set_current_screen("settings".into());
                crate::settings::spawn_load(&ui, &state, &paths);
            }
        });
    }
}

/// Reloads everything the imported database changed, as a fresh unlock would: theme, language, primary currency,
/// settings and every page's state.
pub(crate) fn reload_after_findesk_import(
    main: &MainWindow,
    state: &Arc<ShellState>,
    paths: &Arc<AppPaths>,
) -> Option<Language> {
    let db = state.db.lock().expect("database state poisoned").clone()?;
    let fallback = current_language(state);
    let prefs = app::prefs::load(&db, fallback).ok()?;
    let rows = app::audit::recent(&db, AUDIT_LIMIT).unwrap_or_default();
    let slug = state.slug();
    let mut settings = SettingsState::new(prefs.language);
    let _ = settings.load(&db, paths, slug.as_deref());

    *state.language.lock().expect("language state poisoned") = prefs.language;
    *state.theme.lock().expect("theme state poisoned") = prefs.theme;
    *state.settings.lock().expect("settings state poisoned") = settings.clone();
    *state.dashboard.lock().expect("dashboard state poisoned") = DashboardState::new(prefs.language, local_today());
    let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();
    *state.accounts.lock().expect("accounts state poisoned") = new_accounts_state(prefs.language, busy);
    *state.banking.lock().expect("banking state poisoned") = BankingState::new(prefs.language);
    *state.navigation.lock().expect("navigation state poisoned") = StartArgs::default();

    state.reports.reset();
    state.documents.reset();
    state.transactions.reset();

    main.set_current_screen("settings".into());
    apply_language_main(main, prefs.language);
    apply_theme(main, prefs.theme);
    main.set_theme_pref(prefs.theme.code().into());
    main.set_language(prefs.language.code().into());
    main.set_sidebar_collapsed(prefs.sidebar_collapsed);
    main.set_audit_rows(audit_model(rows));
    crate::settings::apply(main, &settings, state.restored_from().as_deref());

    state.reports.attach(db.clone(), prefs.language);
    state.documents.attach(db.clone(), prefs.language);
    state.transactions.attach(db.clone(), prefs.language);
    dashboard::spawn_load(main, state, None);

    Some(prefs.language)
}

fn spawn_open_work(
    work: impl FnOnce() -> Result<Database, UnlockError> + Send + 'static,
    language: Language,
    unlock_weak: Weak<UnlockWindow>,
    main_weak: Weak<MainWindow>,
    paths: Arc<AppPaths>,
    state: Arc<ShellState>,
) {
    state.reports.reset();
    state.documents.reset();
    state.transactions.reset();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let slug = state.slug();
        let outcome = work().and_then(|db| {
            let db = Arc::new(db);
            let prefs = app::prefs::load(&db, system_language()).map_err(|_| UnlockError::Failed)?;
            let rows = app::audit::recent(&db, AUDIT_LIMIT).map_err(|_| UnlockError::Failed)?;
            let mut settings = SettingsState::new(prefs.language);
            settings
                .load(&db, &paths, slug.as_deref())
                .map_err(|_| UnlockError::Failed)?;
            Ok((db, prefs, rows, settings))
        });
        let error_text = outcome.as_ref().err().map(|error| unlock_error_text(language, error));

        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            ui.set_busy(false);

            match outcome {
                Ok((db, prefs, rows, settings)) => {
                    let Some(main) = main_weak.upgrade() else {
                        return;
                    };
                    *state.language.lock().expect("language state poisoned") = prefs.language;
                    *state.theme.lock().expect("theme state poisoned") = prefs.theme;
                    *state.settings.lock().expect("settings state poisoned") = settings.clone();
                    *state.findesk.lock().expect("findesk state poisoned") = FindeskImportState::new(prefs.language);
                    let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();
                    *state.accounts.lock().expect("accounts state poisoned") = new_accounts_state(prefs.language, busy);
                    *state.banking.lock().expect("banking state poisoned") = BankingState::new(prefs.language);

                    main.set_current_screen("dashboard".into());
                    main.set_profile_menu_open(false);
                    main.set_profile_dialog_open(false);
                    main.set_profile_delete_open(false);
                    main.set_change_open(false);
                    main.set_change_busy(false);
                    main.set_change_error("".into());
                    main.set_category_dialog_open(false);
                    main.set_rule_dialog_open(false);
                    main.set_category_delete_open(false);
                    main.set_rule_delete_open(false);
                    main.set_rule_import_open(false);
                    main.set_settings_restore_open(false);
                    main.set_settings_restore_passphrase_open(false);
                    main.set_findesk_passphrase_open(false);
                    main.set_findesk_passphrase("".into());
                    main.set_accounts_dialog("".into());
                    main.set_accounts_dialog_online(false);
                    main.set_accounts_detail_open(false);

                    apply_language_main(&main, prefs.language);
                    apply_language_unlock(&ui, &paths, prefs.language, slug.as_deref());
                    apply_theme(&main, prefs.theme);
                    main.set_theme_pref(prefs.theme.code().into());
                    main.set_language(prefs.language.code().into());
                    main.set_sidebar_collapsed(prefs.sidebar_collapsed);
                    main.set_audit_rows(audit_model(rows));
                    crate::settings::apply(&main, &settings, state.restored_from().as_deref());
                    crate::settings::apply_findesk(&main, &FindeskImportState::new(prefs.language));
                    state.reports.attach(db.clone(), prefs.language);
                    state.documents.attach(db.clone(), prefs.language);
                    state.transactions.attach(db.clone(), prefs.language);
                    *state.db.lock().expect("database state poisoned") = Some(db);

                    dashboard::spawn_open(&main, &state);
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

/// Starts a live switch to the profile at `index` in the loaded Settings list.
fn switch_profile(
    main: &MainWindow,
    unlock: &UnlockWindow,
    paths: &Arc<AppPaths>,
    state: &Arc<ShellState>,
    index: i32,
) {
    main.set_profile_menu_open(false);
    let target = {
        let settings = state.settings.lock().expect("settings state poisoned");
        settings
            .profiles
            .get(usize::try_from(index).unwrap_or(usize::MAX))
            .cloned()
    };
    let Some(target) = target else {
        set_settings_error(state, main, Key::ErrorUnexpected);
        return;
    };

    if state.switching.swap(true, Ordering::SeqCst) || state.jobs.active() {
        state.switching.store(false, Ordering::SeqCst);
        let text = app::SwitchError::Busy.message(current_language(state));
        set_settings_error_text(state, main, text);
        return;
    }

    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        state.switching.store(false, Ordering::SeqCst);
        set_settings_error(state, main, Key::ErrorUnexpected);
        return;
    };
    let from = state.slug();
    let to = target.slug.clone();
    let settings = state.settings.lock().expect("settings state poisoned").clone();
    let language = current_language(state);
    let paths = paths.clone();
    let state = state.clone();
    let main_weak = main.as_weak();
    let unlock_weak = unlock.as_weak();
    let generation = state.jobs.begin();

    thread::spawn(move || {
        let result = settings
            .switch_profile(&paths, to.as_deref())
            .map_err(|_| app::SwitchError::UnknownProfile)
            .and_then(|slug| {
                app::switch::begin_switch(&db, &paths, from.as_deref(), slug.as_deref(), OffsetDateTime::now_utc())
            });
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            state.switching.store(false, Ordering::SeqCst);
            if !state.jobs.is_current(generation) {
                return;
            }
            let Some(main) = main_weak.upgrade() else {
                return;
            };

            match result {
                Ok(startup) => {
                    teardown_profile(&state, Some(&main), &startup);
                    let Some(unlock) = unlock_weak.upgrade() else {
                        return;
                    };
                    configure_unlock_window(&unlock, &paths, current_language(&state), &startup);
                    unlock.set_passphrase("".into());
                    unlock.set_repeat("".into());
                    unlock.set_current("".into());
                    unlock.set_show_passphrase(false);
                    unlock.set_acknowledged(false);
                    // Show the unlock window first: hiding the last visible window quits the event loop.
                    let _ = unlock.show();
                    let _ = main.hide();
                }
                Err(error) => {
                    set_settings_error_text(&state, &main, error.message(language));
                }
            }
        });
    });
}

fn set_settings_error(state: &Arc<ShellState>, main: &MainWindow, key: Key) {
    let text = i18n::text(current_language(state), key).to_owned();
    set_settings_error_text(state, main, text);
}

fn set_settings_error_text(state: &Arc<ShellState>, main: &MainWindow, text: String) {
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.error = Some(text);
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    crate::settings::apply(main, &pending, state.restored_from().as_deref());
}

/// Closes the profile that is open and prepares the target's unlock window.
///
/// This is the only place that tears a profile down: the generation changes first, then the database and every page
/// state or controller holding it are cleared, and the main window is hidden until the target unlocks.
pub(crate) fn teardown_profile(state: &Arc<ShellState>, main: Option<&MainWindow>, startup: &app::startup::Startup) {
    let language = current_language(state);
    let today = local_today();
    let target = app::startup::slug_key(startup.slug.as_deref()).to_owned();

    state.jobs.bump();
    *state.db.lock().expect("database state poisoned") = None;
    *state.slug.lock().expect("profile state poisoned") = startup.slug.clone();
    *state.restored_from.lock().expect("restore state poisoned") = startup.restored_from.clone();
    *state.dashboard.lock().expect("dashboard state poisoned") = DashboardState::new(language, today);
    *state.settings.lock().expect("settings state poisoned") = SettingsState::new(language);
    *state.findesk.lock().expect("findesk state poisoned") = FindeskImportState::new(language);
    let busy = Busy::default();
    *state.profile_busy.lock().expect("profile busy poisoned") = busy.clone();
    state.transactions.set_profile_busy(busy.clone());
    *state.accounts.lock().expect("accounts state poisoned") = new_accounts_state(language, busy);
    *state.banking.lock().expect("banking state poisoned") = BankingState::new(language);
    *state.navigation.lock().expect("navigation state poisoned") = StartArgs::default();

    state.reports.reset();
    state.documents.set_profile(target.clone());
    state.documents.reset();
    state.transactions.set_profile(target);
    state.transactions.reset();

    // M5c-3: cancel the old profile's prompts and give the next profile its own bridge.
    crate::banking::replace_bridge(
        state,
        main.map(MainWindow::as_weak).unwrap_or_default(),
        Arc::downgrade(state),
    );

    if let Some(main) = main {
        crate::settings::apply(main, &SettingsState::new(language), startup.restored_from.as_deref());
        crate::settings::apply_findesk(main, &FindeskImportState::new(language));
        main.set_findesk_passphrase_open(false);
        main.set_findesk_passphrase("".into());
        dashboard::apply(main, &DashboardState::new(language, today));
        let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();
        let banking = state.banking.lock().expect("banking state poisoned").clone();
        accounts::apply(main, &new_accounts_state(language, busy), &banking);
        crate::transactions::apply(main, &app::transactions::TransactionsState::new(language));
        // The caller hides the main window only after the unlock window is shown: Slint quits the event loop as
        // soon as no window is visible.
    }
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

fn apply_language_unlock(unlock: &UnlockWindow, paths: &AppPaths, language: Language, slug: Option<&str>) {
    unlock.global::<Tr>().set_language(language.code().into());
    unlock.set_profile_label(profile_label(language, paths, slug).into());
    unlock.set_passphrase_hint(passphrase_hint(language).into());
}

fn apply_leftovers(unlock: &UnlockWindow, paths: &AppPaths, language: Language) {
    let lines: Vec<SharedString> = app::startup::leftovers(paths)
        .into_iter()
        .map(|leftover| {
            let kind = match leftover.kind {
                app::startup::LeftoverKind::PreRestore => Key::PlaintextLeftoverPreRestore,
                app::startup::LeftoverKind::DeletedProfile => Key::PlaintextLeftoverDeletedProfile,
                app::startup::LeftoverKind::PendingRestore => Key::PlaintextLeftoverPendingRestore,
            };
            format!("{} · {}", i18n::text(language, kind), leftover.path).into()
        })
        .collect();
    unlock.set_leftover_lines(ModelRc::new(VecModel::from(lines)));
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
        let theme_global = self.global::<Theme>();
        theme_global.set_mode(theme.code().into());
        // The widgets own Palette.color-scheme; Theme applies the choice to it.
        theme_global.invoke_apply_scheme();
    }
}

impl HasThemeMode for MainWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        let theme_global = self.global::<Theme>();
        theme_global.set_mode(theme.code().into());
        theme_global.invoke_apply_scheme();
    }
}

fn profile_label(language: Language, paths: &AppPaths, slug: Option<&str>) -> String {
    let name = profiles::list(paths)
        .ok()
        .and_then(|profiles| profiles.into_iter().find(|profile| profile.slug.as_deref() == slug))
        .map(|profile| profile.name)
        .unwrap_or_else(|| app::startup::slug_key(slug).to_owned());
    i18n::format(language, Key::UnlockProfile, &[name.into()])
}

fn passphrase_hint(language: Language) -> String {
    i18n::format(language, Key::PassphraseHint, &[MIN_PASSPHRASE_LENGTH.into()])
}

fn about_version_text(language: Language) -> String {
    i18n::format(language, Key::AboutVersion, &[VERSION.into()])
}

pub(crate) fn unlock_error_text(language: Language, error: &UnlockError) -> String {
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

/// Builds an Accounts state that shares the profile's one busy flag.
fn new_accounts_state(language: Language, busy: Busy) -> AccountsState {
    let mut accounts = AccountsState::new(language);
    accounts.busy = busy;
    accounts
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
    use std::sync::mpsc;

    use super::*;
    use finglass_core::app::settings::SettingsState;
    use finglass_core::app::startup::Startup;
    use finglass_core::dashboard::YearMonth;
    use finglass_core::profiles;
    use time::macros::date;

    fn shell_paths(temp: &tempfile::TempDir) -> Arc<AppPaths> {
        Arc::new(AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap())
    }

    fn shell_state(paths: &Arc<AppPaths>, db: Option<Arc<Database>>, slug: Option<&str>) -> Arc<ShellState> {
        let jobs = JobTracker::new();
        let ui = slint::Weak::<MainWindow>::default();
        let reports = Arc::new(ReportsController::new(ui.clone(), jobs.clone()));
        let documents = Arc::new(DocumentsController::new(
            ui.clone(),
            paths.clone(),
            app::startup::slug_key(slug),
            jobs.clone(),
        ));
        let transactions = Arc::new(TransactionsController::new(
            ui,
            paths.clone(),
            app::startup::slug_key(slug),
            jobs.clone(),
        ));
        Arc::new(ShellState {
            language: Mutex::new(Language::En),
            theme: Mutex::new(ThemePref::System),
            db: Mutex::new(db),
            credentials: Arc::new(finglass_core::sync::credentials::NoCredentialStore),
            dashboard: Mutex::new(DashboardState::new(Language::En, local_today())),
            reports,
            navigation: Mutex::new(StartArgs::default()),
            settings: Mutex::new(SettingsState::new(Language::En)),
            findesk: Mutex::new(FindeskImportState::new(Language::En)),
            accounts: Mutex::new(new_accounts_state(Language::En, Busy::default())),
            banking: Mutex::new(BankingState::new(Language::En)),
            banking_bridge: Mutex::new(None),
            profile_busy: Mutex::new(Busy::default()),
            paths: paths.clone(),
            accounts_work: Busy::default(),
            documents,
            slug: Mutex::new(slug.map(str::to_owned)),
            restored_from: Mutex::new(None),
            transactions,
            jobs,
            switching: AtomicBool::new(false),
        })
    }

    #[test]
    fn navigating_back_to_the_dashboard_resets_it_to_todays_month() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let state = shell_state(&paths, None, None);
        {
            let mut dashboard = state.dashboard.lock().expect("dashboard state poisoned");
            dashboard.open(date!(2026 - 10 - 15));
            assert_eq!(dashboard.selected_month, YearMonth::new(2026, 10));
        }

        let pending = crate::dashboard::prepare_open(&state, date!(2026 - 11 - 08));
        *state.dashboard.lock().expect("dashboard state poisoned") = pending.clone();

        assert_eq!(pending.selected_month, YearMonth::new(2026, 11));
        assert_eq!(pending.current_month, YearMonth::new(2026, 11));
        assert_eq!(
            state.dashboard.lock().expect("dashboard state poisoned").selected_month,
            YearMonth::new(2026, 11)
        );
    }

    #[test]
    fn a_reload_on_the_dashboard_keeps_the_selected_month() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let state = shell_state(&paths, None, None);
        {
            let mut dashboard = state.dashboard.lock().expect("dashboard state poisoned");
            dashboard.begin_load(YearMonth::new(2026, 9), date!(2026 - 10 - 15));
            dashboard.selected_month = YearMonth::new(2026, 9);
        }

        let pending = crate::dashboard::prepare_load(&state, date!(2026 - 11 - 08), None);

        assert_eq!(pending.selected_month, YearMonth::new(2026, 9));
        assert_eq!(pending.current_month, YearMonth::new(2026, 11));
    }

    #[test]
    fn the_profile_busy_is_shared_when_the_accounts_state_is_rebuilt() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let state = shell_state(&paths, None, None);
        let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();

        let rebuilt = new_accounts_state(Language::En, busy.clone());
        *state.accounts.lock().expect("accounts state poisoned") = rebuilt;

        busy.begin();
        assert!(
            state.accounts.lock().expect("accounts state poisoned").busy.active(),
            "an active profile busy is seen through the rebuilt Accounts state"
        );
        assert!(state.profile_busy.lock().expect("profile busy poisoned").active());
        busy.end();
        assert!(!state.accounts.lock().expect("accounts state poisoned").busy.active());
    }

    #[test]
    fn a_finished_banking_action_keeps_a_sync_claimed_meanwhile() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let state = shell_state(&paths, None, None);
        // The action's snapshot was taken before the sync was claimed.
        let mut result = state.banking.lock().expect("banking state poisoned").clone();
        {
            let mut live = state.banking.lock().expect("banking state poisoned");
            live.syncing_bank = Some(finglass_core::model::BankConnectionId(7));
            live.sync_step = Some("Connecting".to_owned());
        }

        crate::banking::keep_live_sync_fields(&mut result, &state);

        assert_eq!(result.syncing_bank, Some(finglass_core::model::BankConnectionId(7)));
        assert_eq!(result.sync_step.as_deref(), Some("Connecting"));
        assert!(result.is_syncing());
    }

    #[test]
    fn teardown_drops_every_arc_database() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let db = Arc::new(
            app::unlock::create(
                &paths,
                "default",
                "correct horse battery staple",
                "correct horse battery staple",
                true,
            )
            .unwrap(),
        );
        let state = shell_state(&paths, Some(db.clone()), None);
        state.documents.attach(db.clone(), Language::En);
        state.transactions.attach(db.clone(), Language::En);
        state.reports.attach(db.clone(), Language::En);

        let weak = Arc::downgrade(&db);
        drop(db);
        assert!(weak.upgrade().is_some(), "the pages hold the open database");

        teardown_profile(
            &state,
            None,
            &Startup {
                slug: None,
                restored_from: None,
            },
        );

        assert!(
            weak.upgrade().is_none(),
            "teardown must drop the shell, page states and controllers that hold the database"
        );
    }

    #[test]
    fn a_late_result_from_before_the_teardown_is_dropped() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let state = shell_state(&paths, None, None);
        let worker_state = state.clone();
        let (send, receive) = mpsc::channel::<&'static str>();
        let generation = worker_state.jobs.begin();
        let worker = thread::spawn(move || {
            let result = receive.recv().expect("the test sends a result");
            let accepted = worker_state.jobs.is_current(generation);
            worker_state.jobs.end();
            (result, accepted)
        });

        teardown_profile(
            &state,
            None,
            &Startup {
                slug: None,
                restored_from: None,
            },
        );
        send.send("late result").unwrap();
        let (result, accepted) = worker.join().unwrap();

        assert_eq!(result, "late result");
        assert!(!accepted, "the generation check drops the old profile's result");
        assert!(!state.jobs.active(), "the job released its slot");
    }

    #[test]
    fn after_a_switch_the_controllers_and_unlock_use_the_target_folder() {
        use time::macros::datetime;

        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);
        let old = app::unlock::create(
            &paths,
            "default",
            "correct horse battery staple",
            "correct horse battery staple",
            true,
        )
        .unwrap();
        let target = profiles::create(&paths, "Business").unwrap();
        let slug = target.slug.clone().unwrap();
        let target_db = app::unlock::create(
            &paths,
            &slug,
            "correct horse battery staple",
            "correct horse battery staple",
            true,
        )
        .unwrap();
        drop(target_db);

        let state = shell_state(&paths, Some(Arc::new(old)), None);
        let startup = app::switch::begin_switch(
            state.db.lock().expect("database state poisoned").as_ref().unwrap(),
            &paths,
            None,
            Some(&slug),
            datetime!(2026 - 10 - 15 12:34:56 UTC),
        )
        .unwrap();

        teardown_profile(&state, None, &startup);

        assert_eq!(state.slug().as_deref(), Some(slug.as_str()));
        assert_eq!(state.documents.profile_key(), slug);
        assert_eq!(state.transactions.profile_key(), slug);
        let target_file = paths
            .database_file(app::startup::slug_key(state.slug().as_deref()))
            .unwrap();
        assert_eq!(target_file, target.dir.join("finglass.db"));
        assert_eq!(app::unlock::mode_for(&target_file), UnlockMode::Unlock);
    }

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
    fn the_unlock_label_shows_the_profile_name() {
        let temp = tempfile::tempdir().unwrap();
        let paths = shell_paths(&temp);

        assert_eq!(profile_label(Language::En, &paths, None), "Profile: Personal");

        let business = profiles::create(&paths, "Business").unwrap();
        assert_eq!(
            profile_label(Language::En, &paths, business.slug.as_deref()),
            "Profile: Business"
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

    #[test]
    fn only_the_welcome_actions_write_the_migration_marker() {
        use std::fs;

        use time::macros::datetime;

        let temp = tempfile::tempdir().unwrap();
        let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
        let findesk_root = temp.path().join("findesk");
        fs::create_dir_all(&findesk_root).unwrap();
        fs::write(findesk_root.join("findesk.db"), b"db").unwrap();

        let install = app::migration::discover(&findesk_root).unwrap();
        assert!(app::migration::should_offer(&paths, Some(&install)));
        assert!(!paths.root().join(app::migration::MARKER_FILE).exists());

        app::startup::prepare(&paths, datetime!(2026-10-15 12:34:56 UTC)).unwrap();
        assert!(
            !paths.root().join(app::migration::MARKER_FILE).exists(),
            "discovery and startup never write the marker"
        );

        let now = datetime!(2026-10-15 12:34:56 UTC);
        crate::migration::welcome_action(&paths, true, now).unwrap();
        assert!(
            fs::read_to_string(paths.root().join(app::migration::MARKER_FILE))
                .unwrap()
                .starts_with("migrate ")
        );

        fs::remove_file(paths.root().join(app::migration::MARKER_FILE)).unwrap();
        crate::migration::welcome_action(&paths, false, now).unwrap();
        assert!(
            fs::read_to_string(paths.root().join(app::migration::MARKER_FILE))
                .unwrap()
                .starts_with("skip ")
        );
    }

    #[test]
    fn a_second_instance_does_not_apply_a_staged_restore() {
        use std::fs;

        use finglass_core::backup::{BackupService, PRE_RESTORE_PREFIX};
        use finglass_core::model::{AccountType, NewAccount};
        use finglass_core::money::{Currency, Money};
        use finglass_core::profiles;
        use finglass_core::repo::accounts;
        use time::macros::datetime;

        let temp = tempfile::tempdir().unwrap();
        let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
        let passphrase = "correct horse battery staple";
        let db = app::unlock::create(&paths, app::startup::slug_key(None), passphrase, passphrase, true).unwrap();
        accounts::create(
            &db,
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
        .unwrap();

        let service = BackupService::new(profiles::data_dir(&paths, None).unwrap());
        let backup = temp.path().join("startup.zip");
        service.create_backup(&db, &backup).unwrap();
        service.stage_restore(&db, &backup, None).unwrap();
        drop(db);

        let held = InstanceLock::acquire(&paths).unwrap();
        let refusal = prepare_startup(&paths, datetime!(2026-10-15 12:34:56 UTC)).unwrap_err();

        assert!(
            matches!(refusal, StartupRefusal::AlreadyRunning),
            "a held lock must refuse the second startup"
        );
        assert!(
            service.has_pending_restore(),
            "the staged restore must still be pending"
        );
        let pre_restores = fs::read_dir(paths.root())
            .unwrap()
            .filter_map(Result::ok)
            .filter(|entry| entry.file_name().to_string_lossy().starts_with(PRE_RESTORE_PREFIX))
            .count();
        assert_eq!(pre_restores, 0, "no data was moved aside");
        drop(held);
    }
}
