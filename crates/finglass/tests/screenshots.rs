//! Headless rendering of the app shell with Slint's software renderer.
//!
//! `set_platform` installs the Slint platform for the calling thread, so each test sets up its own
//! software window for rendering.

use std::cell::{Cell, RefCell};
use std::collections::HashSet;
use std::io::BufWriter;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::{Mutex, MutexGuard};

use finglass_core::account_grouping::{self, AccountItem as GroupingAccount, BalanceTone, GroupKey, LOAN_TYPE};
use finglass_core::account_ids;
use finglass_core::app;
use finglass_core::app::accounts::{ACCOUNT_TYPE_CODES, AccountDetail, AccountItem, AccountsState};
use finglass_core::app::banking::{BankingState, SyncAllProgress};
use finglass_core::app::dashboard::{DashboardState, DashboardView, month_text};
use finglass_core::app::prefs::ThemePref;
use finglass_core::app::reports::ReportsState;
use finglass_core::app::settings::{RestoreCandidate, SettingsState};
use finglass_core::app::transactions::{Period, StartArgs, TransactionsState, TransactionsView};
use finglass_core::audit::{self, Actor};
use finglass_core::backup::BackupInfo;
use finglass_core::db::Database;
use finglass_core::findesk_import::FinDeskImportReport;
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{
    AccountId, AccountType, BankAccessItem, CategoryId, DocumentItem, NewAccount, NewAccountGroup, NewCategory,
    NewDocument, NewPendingBooking, NewRule, NewTransaction, RuleDirection, RuleField, TransactionFilter,
    TransactionId,
};
use finglass_core::money::{Currency, Money};
use finglass_core::paths::AppPaths;
use finglass_core::profiles;
use finglass_core::repo::{account_groups, accounts, bank_connections, categories, documents, rules, transactions};
use slint::platform::software_renderer::{MinimalSoftwareWindow, RepaintBufferType};
use slint::platform::{Platform, PlatformError, WindowAdapter};
use slint::{
    Color, ComponentHandle, Image, Model, ModelRc, PhysicalSize, Rgb8Pixel, SharedPixelBuffer, SharedString, VecModel,
};
use time::macros::date;
use time::{Date, OffsetDateTime};

use finglass::{reports, transactions as transactions_page};
use finglass_ui::*;

const SLUG: &str = "default";

// `set_platform` installs the platform on the calling thread. Each test thread keeps its own
// software window, and tests take turns so they never share one.
static TEST_SERIAL: Mutex<()> = Mutex::new(());

thread_local! {
    static TEST_WINDOW: RefCell<Option<Rc<MinimalSoftwareWindow>>> = const { RefCell::new(None) };
    static PLATFORM_READY: Cell<bool> = const { Cell::new(false) };
}

struct TestPlatform;

impl Platform for TestPlatform {
    fn create_window_adapter(&self) -> Result<Rc<dyn WindowAdapter>, PlatformError> {
        Ok(test_window())
    }
}

fn test_window() -> Rc<MinimalSoftwareWindow> {
    TEST_WINDOW.with(|slot| {
        slot.borrow_mut()
            .get_or_insert_with(|| MinimalSoftwareWindow::new(RepaintBufferType::ReusedBuffer))
            .clone()
    })
}

fn platform_window() -> Rc<MinimalSoftwareWindow> {
    PLATFORM_READY.with(|ready| {
        if !ready.replace(true) {
            slint::platform::set_platform(Box::new(TestPlatform)).expect("test platform must be installable");
        }
    });
    test_window()
}

fn serial_test() -> MutexGuard<'static, ()> {
    TEST_SERIAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

#[test]
fn renders_the_app_shell() {
    let _serial = serial_test();
    let window = platform_window();

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
    seed_settings(&db);
    let rows = app::audit::recent(&db, 100).unwrap();

    let documents = seed_documents(&db, temp.path());
    let preview_path = temp.path().join("kassenbon.png");
    write_preview_png(&preview_path);
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
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(&ui, Language::En, ThemePref::Light, "unlock", "");
        ui.set_restored(true);
        ui.show().unwrap();
        let buffer = render(&window, "unlock-restored-en", 560, 760);
        assert_varied(&buffer, "unlock-restored-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(&ui, Language::En, ThemePref::Light, "unlock", "");
        ui.set_leftover_lines(ModelRc::new(VecModel::from(vec![
            SharedString::from(format!(
                "{} · pre-restore-20261015-123456",
                i18n::text(Language::En, Key::PlaintextLeftoverPreRestore)
            )),
            SharedString::from(format!(
                "{} · profiles-deleted/business-20261015-123456",
                i18n::text(Language::En, Key::PlaintextLeftoverDeletedProfile)
            )),
        ])));
        ui.show().unwrap();
        let buffer = render(&window, "unlock-leftovers-en", 560, 800);
        assert_eq!(buffer.width(), 560);
        assert_eq!(buffer.height(), 800);
        assert_varied(&buffer, "unlock-leftovers-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(
            &ui,
            Language::En,
            ThemePref::Light,
            "unlock",
            i18n::text(Language::En, Key::ErrorAlreadyRunning),
        );
        ui.set_allow_change(false);
        ui.set_startup_error(true);
        ui.show().unwrap();
        let buffer = render(&window, "unlock-already-running-en", 560, 700);
        assert_varied(&buffer, "unlock-already-running-en");
        let _ = ui.hide();
    }

    // M8g: the first-run migration from FinDesk.
    {
        let ui = UnlockWindow::new().unwrap();
        configure_migration(&ui, Language::En, ThemePref::Light, "welcome");
        ui.show().unwrap();
        let buffer = render(&window, "migration-welcome-en", 560, 800);
        assert_varied(&buffer, "migration-welcome-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_migration(&ui, Language::En, ThemePref::Light, "profile");
        ui.show().unwrap();
        let buffer = render(&window, "migration-profile-en", 560, 800);
        assert_varied(&buffer, "migration-profile-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_migration(&ui, Language::De, ThemePref::Dark, "profile");
        ui.set_error_text(i18n::text(Language::De, Key::WrongPassphrase).into());
        ui.show().unwrap();
        let buffer = render(&window, "migration-wrong-passphrase-de-dark", 560, 800);
        assert_varied(&buffer, "migration-wrong-passphrase-de-dark");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_migration(&ui, Language::En, ThemePref::Light, "profile");
        ui.set_migration_same_passphrase(false);
        ui.set_migration_new_required(true);
        ui.show().unwrap();
        let buffer = render(&window, "migration-profile-new-passphrase-en", 560, 800);
        assert_varied(&buffer, "migration-profile-new-passphrase-en");
        let _ = ui.hide();
    }

    {
        let ui = UnlockWindow::new().unwrap();
        configure_migration(&ui, Language::En, ThemePref::Light, "summary");
        let report = FinDeskImportReport::default();
        ui.set_migration_summary_lines(ModelRc::new(VecModel::from(vec![
            SharedString::from(i18n::format(
                Language::En,
                Key::MigrationProfileMigrated,
                &["Personal".into(), report.summary(Language::En).into()],
            )),
            SharedString::from(i18n::format(
                Language::En,
                Key::MigrationProfileSkipped,
                &["Business".into()],
            )),
        ])));
        ui.show().unwrap();
        let buffer = render(&window, "migration-summary-en", 560, 800);
        assert_varied(&buffer, "migration-summary-en");
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
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_dashboard_month_menu_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "dashboard-month-menu-en-light", 1100, 720);
        assert_varied(&buffer, "dashboard-month-menu-en-light");
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

    // M2c: the top bar and the collapsible sidebar.
    {
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(false);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-sidebar-expanded-en-light", 1100, 720);
        assert_varied(&buffer, "topbar-sidebar-expanded-en-light");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-sidebar-collapsed-en-light", 1100, 720);
        assert_varied(&buffer, "topbar-sidebar-collapsed-en-light");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::De, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-sidebar-collapsed-de-dark", 1100, 720);
        assert_varied(&buffer, "topbar-sidebar-collapsed-de-dark");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(false);
        ui.set_language_menu_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-language-menu-en-light", 1100, 720);
        assert_varied(&buffer, "topbar-language-menu-en-light");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::De, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(false);
        ui.set_language_menu_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-language-menu-de-dark", 1100, 720);
        assert_varied(&buffer, "topbar-language-menu-de-dark");
        let _ = ui.hide();
    }

    {
        let mut dashboard = DashboardState::new(Language::En, today);
        dashboard.load(&db, today).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_dashboard(&ui, &dashboard);
        ui.set_sidebar_collapsed(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-sidebar-collapsed-narrow-en-light", 900, 720);
        assert_varied(&buffer, "topbar-sidebar-collapsed-narrow-en-light");
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

    let business = profiles::create(&paths, "Business").unwrap();
    let business_slug = business.slug.clone().unwrap();

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(0); // Appearance
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-currency-en-light", 1100, 720);
        assert_varied(&buffer, "settings-currency-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(1); // Profiles
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-profiles-en-light", 1100, 720);
        assert_varied(&buffer, "settings-profiles-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(1); // Profiles
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-profiles-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-profiles-de-dark");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        apply_settings(&ui, &settings);
        ui.set_profile_menu_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "topbar-profiles-en-light", 1100, 720);
        assert_varied(&buffer, "topbar-profiles-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(1); // Profiles
        ui.set_profile_delete_open(true);
        ui.set_profile_delete_index(1);
        ui.set_profile_delete_title(i18n::format(Language::En, Key::DeleteProfile, &[Arg::from("Business")]).into());
        ui.set_profile_delete_text(i18n::format(Language::En, Key::DeleteProfileText, &[Arg::from("Business")]).into());
        ui.show().unwrap();
        let buffer = render(&window, "settings-profile-delete-en", 1100, 720);
        assert_varied(&buffer, "settings-profile-delete-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(0); // Appearance
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-currency-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-currency-de-dark");
        assert_dark_widget(&buffer, 800, 255, "settings-currency-de-dark currency ComboBox");
        let _ = ui.hide();
    }

    // M2d: the fluent widgets follow the app's theme, not only the page palettes.
    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(0); // Appearance
        ui.set_settings_scroll_y(0.0);

        let buffer = render(&window, "theme-switch-dark", 1100, 720);
        assert_varied(&buffer, "theme-switch-dark");
        assert_dark_widget(&buffer, 800, 255, "theme-switch-dark currency ComboBox");

        sync_theme(&ui, ThemePref::System);
        let theme = ui.global::<Theme>();
        // Whatever the captured OS scheme is, Theme.dark and the fluent widgets must agree.
        let system_dark = theme.get_dark();
        let buffer = render(&window, "theme-switch-system", 1100, 720);
        assert_varied(&buffer, "theme-switch-system");
        assert_widget_tone(&buffer, 800, 255, system_dark, "theme-switch-system currency ComboBox");

        sync_theme(&ui, ThemePref::Light);
        assert!(!theme.get_dark(), "theme-switch-light: Theme.dark is light");
        let buffer = render(&window, "theme-switch-light", 1100, 720);
        assert_varied(&buffer, "theme-switch-light");
        assert_light_widget(&buffer, 800, 255, "theme-switch-light currency ComboBox");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(2); // Categories
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-categories-en-light", 1100, 720);
        assert_varied(&buffer, "settings-categories-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(2); // Categories
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-categories-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-categories-de-dark");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(2); // Categories, scrolled to the keywords card
        ui.set_settings_scroll_y(-320.0);
        let buffer = render(&window, "settings-tax-keywords-en-light", 1100, 720);
        assert_varied(&buffer, "settings-tax-keywords-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(2); // Categories, scrolled to the keywords card
        ui.set_settings_scroll_y(-320.0);
        let buffer = render(&window, "settings-tax-keywords-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-tax-keywords-de-dark");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(3); // Auto-categorization rules
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-rules-en-light", 1100, 720);
        assert_varied(&buffer, "settings-rules-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(3); // Auto-categorization rules
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-rules-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-rules-de-dark");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(5); // Backup & restore
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-backup-en-light", 1100, 720);
        assert_varied(&buffer, "settings-backup-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(5); // Backup & restore
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-backup-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-backup-de-dark");
        let _ = ui.hide();
    }

    {
        let mut settings = settings_state(&db, &paths, Language::En);
        settings.has_pending_restore = true;
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(5); // Backup & restore
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-backup-pending-en", 1100, 720);
        assert_varied(&buffer, "settings-backup-pending-en");
        let _ = ui.hide();
    }

    // M8d-3: the one-time FinDesk import in Settings and its dialogs.
    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(6); // Import from FinDesk
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-findesk-import-en-light", 1100, 720);
        assert_varied(&buffer, "settings-findesk-import-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::De);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(6); // Import from FinDesk
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-findesk-import-de-dark", 1100, 720);
        assert_varied(&buffer, "settings-findesk-import-de-dark");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(8); // About
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-about-bank-codes-en", 1100, 720);
        assert_varied(&buffer, "settings-about-bank-codes-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(6); // Import from FinDesk
        ui.set_findesk_folder("/Users/example/.findesk".into());
        ui.set_findesk_passphrase_wrong(false);
        ui.set_findesk_passphrase("".into());
        ui.set_findesk_passphrase_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "settings-findesk-passphrase-en", 1100, 720);
        assert_varied(&buffer, "settings-findesk-passphrase-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let report = finglass_core::findesk_import::FinDeskImportReport {
            account_groups: finglass_core::findesk_import::ImportCounts {
                imported: 1,
                ..Default::default()
            },
            bank_connections: finglass_core::findesk_import::ImportCounts {
                imported: 2,
                ..Default::default()
            },
            accounts: finglass_core::findesk_import::ImportCounts {
                imported: 3,
                ..Default::default()
            },
            categories: finglass_core::findesk_import::ImportCounts {
                imported: 4,
                ..Default::default()
            },
            rules: finglass_core::findesk_import::ImportCounts {
                imported: 5,
                ..Default::default()
            },
            transactions: finglass_core::findesk_import::ImportCounts {
                imported: 42,
                skipped: 3,
                skipped_orphan: 1,
                skipped_invalid: 1,
                skipped_fractional_amount: 1,
            },
            documents: finglass_core::findesk_import::DocumentImportCounts {
                copied: 2,
                skipped: 3,
                missing: 2,
                refused: 1,
                skipped_invalid: 0,
            },
            settings: finglass_core::findesk_import::SettingsImportCounts {
                imported: 8,
                skipped: 2,
            },
            payment_orders_left_behind: 1,
            ..FinDeskImportReport::default()
        };
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(6); // Import from FinDesk
        ui.set_findesk_report_title(i18n::text(Language::En, Key::FinDeskImportReportTitle).into());
        ui.set_findesk_report_text(app::findesk::report_text(&report, Language::En).into());
        ui.set_findesk_report_error(false);
        ui.set_findesk_report_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "settings-findesk-report-en", 1100, 720);
        assert_varied(&buffer, "settings-findesk-report-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(6); // Import from FinDesk
        ui.set_findesk_report_title(i18n::text(Language::En, Key::FinDeskImportFailedTitle).into());
        ui.set_findesk_report_text(
            finglass_core::findesk_import::FinDeskImportError::TargetNotEmpty
                .message(Language::En)
                .into(),
        );
        ui.set_findesk_report_error(true);
        ui.set_findesk_report_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "settings-findesk-target-not-empty-en", 1100, 720);
        assert_varied(&buffer, "settings-findesk-target-not-empty-en");
        let _ = ui.hide();
    }

    {
        let mut settings = settings_state(&db, &paths, Language::En);
        settings.restore_candidate = Some(RestoreCandidate {
            backup: PathBuf::from("/tmp/finglass-backup-2026-10-15.zip"),
            info: BackupInfo {
                created: "2026-10-15T12:34:56Z".to_owned(),
                app_version: finglass_core::VERSION.to_owned(),
                transactions: 42,
                documents: 3,
            },
        });
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(5); // Backup & restore, where the restore dialog belongs
        ui.show().unwrap();
        let buffer = render(&window, "settings-restore-dialog-en", 1100, 720);
        assert_varied(&buffer, "settings-restore-dialog-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::Fr);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::Fr, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.show().unwrap();
        ui.set_settings_section(0); // Appearance
        ui.set_settings_scroll_y(0.0);
        let buffer = render(&window, "settings-fr-light", 1100, 720);
        assert_varied(&buffer, "settings-fr-light");
        let _ = ui.hide();
    }

    // The narrow window shows the section drop-down instead of the 240 px list.
    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(0); // Appearance
        ui.set_settings_section_menu_open(false);
        ui.show().unwrap();
        let buffer = render(&window, "settings-narrow-en-light", 880, 720);
        assert_varied(&buffer, "settings-narrow-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(0); // Appearance
        ui.set_settings_section_menu_open(true);
        ui.show().unwrap();
        let buffer = render(&window, "settings-section-menu-en-light", 880, 720);
        assert_varied(&buffer, "settings-section-menu-en-light");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let food = settings
            .categories
            .iter()
            .find(|category| category.name == "Food")
            .unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(2); // Categories
        ui.set_category_dialog_open(true);
        ui.set_category_dialog_id(i32::try_from(food.id.0).unwrap());
        ui.set_category_dialog_name(food.name.clone().into());
        ui.set_category_dialog_colour(food.color.clone().into());
        ui.set_category_dialog_budget("300.00".into());
        ui.set_category_dialog_tax(true);
        ui.set_category_dialog_transfer(false);
        ui.set_category_dialog_error("".into());
        ui.show().unwrap();
        let buffer = render(&window, "settings-category-dialog-en", 1100, 720);
        assert_varied(&buffer, "settings-category-dialog-en");
        let _ = ui.hide();
    }

    {
        let settings = settings_state(&db, &paths, Language::En);
        let rule = settings
            .rules
            .iter()
            .find(|rule| rule.pattern == "Example Market")
            .unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(3); // Auto-categorization rules
        ui.set_rule_dialog_open(true);
        ui.set_rule_dialog_id(i32::try_from(rule.id.0).unwrap());
        ui.set_rule_dialog_pattern(rule.pattern.clone().into());
        ui.set_rule_dialog_category_index(
            settings
                .categories
                .iter()
                .position(|category| category.id == rule.category_id)
                .map_or(0, |index| i32::try_from(index).unwrap()),
        );
        ui.set_rule_dialog_priority("5".into());
        ui.set_rule_dialog_auto_apply(true);
        ui.set_rule_dialog_field_index(1);
        ui.set_rule_dialog_direction_index(1);
        ui.set_rule_dialog_tax_index(1);
        ui.set_rule_dialog_error("".into());
        ui.show().unwrap();
        let buffer = render(&window, "settings-rule-dialog-en", 1100, 720);
        assert_varied(&buffer, "settings-rule-dialog-en");
        let _ = ui.hide();
    }

    {
        let mut settings = settings_state(&db, &paths, Language::En);
        let rule_file = temp.path().join("preview-rules.tsv");
        std::fs::write(
            &rule_file,
            concat!(
                "# FinDesk rules v1\n",
                "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\n",
                "Household\trewe|edeka\tany\texpense\t50\ttrue\n",
                "Household\trewe|edeka\tany\texpense\t50\ttrue\n",
                "Household\tcorner kiosk\tpartner\texpense\t90\ttrue\n",
                "Salary\tbeispiel gmbh\tpartner\tincome\t70\ttrue\n",
                "Household\tExample Market\tpartner\texpense\t80\ttrue\n",
                "Broken\trow\n"
            ),
        )
        .unwrap();
        settings.choose_rule_import(&db, &rule_file).unwrap();
        assert!(settings.rule_import.is_some(), "the preview is prepared");

        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "settings", &rows);
        apply_settings(&ui, &settings);
        ui.set_settings_section(3); // Auto-categorization rules
        ui.set_rule_import_categorize(true);
        ui.show().unwrap();
        let buffer = render(&window, "settings-rule-import-en", 1100, 720);
        assert_varied(&buffer, "settings-rule-import-en");
        let _ = ui.hide();
    }

    {
        app::unlock::create(
            &paths,
            &business_slug,
            "correct horse battery staple",
            "correct horse battery staple",
            true,
        )
        .unwrap();
        let startup =
            app::switch::begin_switch(&db, &paths, None, Some(&business_slug), OffsetDateTime::now_utc()).unwrap();
        let ui = UnlockWindow::new().unwrap();
        configure_unlock(&ui, Language::En, ThemePref::Light, "unlock", "");
        ui.set_profile_label(i18n::format(Language::En, Key::UnlockProfile, &["Business".into()]).into());
        ui.set_restored(startup.restored_from.is_some());
        ui.set_passphrase("".into());
        assert_eq!(ui.get_passphrase(), "");
        ui.show().unwrap();
        let buffer = render(&window, "unlock-target-after-switch-en", 560, 760);
        assert_varied(&buffer, "unlock-target-after-switch-en");
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
    let accounts_temp = tempfile::tempdir().unwrap();
    let accounts_paths = AppPaths::resolve(Some(accounts_temp.path().as_os_str().to_owned()), None).unwrap();
    let accounts_db = app::unlock::create(
        &accounts_paths,
        SLUG,
        "correct horse battery staple",
        "correct horse battery staple",
        true,
    )
    .unwrap();
    let seeded = seed_accounts(&accounts_db);
    let mut accounts_state = AccountsState::new(Language::En);
    accounts_state.load_accounts(&accounts_db).unwrap();

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.show().unwrap();
        let buffer = render(&window, "accounts-en-light", 1100, 720);
        assert_varied(&buffer, "accounts-en-light");
        let _ = ui.hide();
    }

    {
        let mut german = accounts_state.clone();
        german.language = Language::De;
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "accounts", &rows);
        apply_accounts(&ui, &german, &banking_state(&german));
        ui.show().unwrap();
        let buffer = render(&window, "accounts-de-dark", 1100, 720);
        assert_varied(&buffer, "accounts-de-dark");
        let _ = ui.hide();
    }

    {
        let mut panel_state = accounts_state.clone();
        panel_state.language = Language::En;
        if let Some(group) = panel_state
            .groups
            .iter()
            .find(|group| group.key == GroupKey::Type("checking".to_owned()))
        {
            panel_state.collapsed.insert(group.key.id());
        }
        let panel_rows = accounts_panel_rows(&panel_state, Language::En);
        assert!(
            panel_rows.iter().any(|row| row.kind == "group" && row.collapsed),
            "the panel screenshot shows a collapsed group"
        );
        assert!(
            panel_rows.iter().any(|row| row.kind == "all" && row.new_count > 0),
            "the panel screenshot shows unread badges"
        );
        assert!(
            panel_rows
                .iter()
                .any(|row| row.kind == "all" && row.totals.row_count() == 2),
            "the panel screenshot shows two currencies"
        );

        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &panel_state, &banking_state(&panel_state));
        ui.set_accounts_panel_preview(true);
        ui.show().unwrap();
        let buffer = render(&window, "account-panel-en-light", 360, 720);
        assert_varied(&buffer, "account-panel-en-light");
        let _ = ui.hide();
    }

    {
        let bank = accounts_state.bank_accesses[0].clone();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.show().unwrap();
        ui.set_accounts_dialog_title(
            i18n::format(Language::En, Key::AddAccountAt, &[Arg::from(bank.bank_name.as_str())]).into(),
        );
        ui.set_accounts_dialog_offline(bank.offline);
        ui.set_accounts_dialog_bank_id(i32::try_from(bank.id.0).unwrap_or(-1));
        ui.set_accounts_dialog_account_id(-1);
        ui.set_accounts_dialog_account_type("credit_card".into());
        ui.set_accounts_dialog_error("".into());
        ui.set_accounts_dialog("add-account".into());
        let buffer = render(&window, "accounts-add-account-en", 1100, 720);
        assert_varied(&buffer, "accounts-add-account-en");
        let _ = ui.hide();
    }

    {
        let mut detail = accounts_state.clone();
        detail.language = Language::En;
        detail
            .open_detail_default(&accounts_db, seeded.checking, today)
            .unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &detail, &banking_state(&detail));
        ui.show().unwrap();
        let buffer = render(&window, "accounts-detail-en-light", 1100, 720);
        assert_varied(&buffer, "accounts-detail-en-light");
        let _ = ui.hide();
    }

    {
        let empty_temp = tempfile::tempdir().unwrap();
        let empty_paths = AppPaths::resolve(Some(empty_temp.path().as_os_str().to_owned()), None).unwrap();
        let empty_db = app::unlock::create(
            &empty_paths,
            SLUG,
            "correct horse battery staple",
            "correct horse battery staple",
            true,
        )
        .unwrap();
        let mut empty = AccountsState::new(Language::En);
        empty.load_accounts(&empty_db).unwrap();
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &empty, &banking_state(&empty));
        ui.show().unwrap();
        let buffer = render(&window, "accounts-empty-en", 1100, 720);
        assert_varied(&buffer, "accounts-empty-en");
        let _ = ui.hide();
    }

    {
        let mut banking = banking_state(&accounts_state);
        banking.syncing_bank = Some(seeded.online);
        banking.sync_step = Some(i18n::text(Language::En, Key::SyncConnecting).to_owned());
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking);
        ui.show().unwrap();
        let buffer = render(&window, "accounts-syncing-en", 1100, 720);
        assert_varied(&buffer, "accounts-syncing-en");
        let _ = ui.hide();
    }

    {
        let mut banking = banking_state(&accounts_state);
        banking.sync_all = Some(SyncAllProgress {
            position: 1,
            total: 2,
            bank_name: "Sparkasse".to_owned(),
        });
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking);
        ui.show().unwrap();
        let buffer = render(&window, "accounts-sync-all-en", 1100, 720);
        assert_varied(&buffer, "accounts-sync-all-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_online(true);
        ui.set_accounts_dialog("add-bank".into());
        ui.set_accounts_dialog_error(i18n::text(Language::En, Key::BlzMustBe8Digits).into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-add-online-bank-en", 1100, 720);
        assert_varied(&buffer, "accounts-add-online-bank-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_online(true);
        ui.set_accounts_add_bank_search("hypo".into());
        ui.set_accounts_add_bank_suggestions(bank_suggestion_rows("hypo"));
        ui.set_accounts_dialog("add-bank".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-add-online-bank-search-en", 1100, 720);
        assert_varied(&buffer, "accounts-add-online-bank-search-en");
        let _ = ui.hide();
    }

    {
        let directory = finglass_core::bank_directory::BankDirectory::bundled();
        let entry = directory.lookup("10020890").expect("a HypoVereinsbank branch");
        let mapped = finglass_core::bank_directory::fints_blz(directory, &entry.blz);
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_online(true);
        ui.set_accounts_add_bank_name(entry.name.clone().into());
        ui.set_accounts_add_bank_blz(mapped.clone().into());
        ui.set_accounts_add_bank_bic(entry.bic.clone().into());
        ui.set_accounts_add_bank_search("".into());
        ui.set_accounts_add_bank_suggestions(ModelRc::new(VecModel::from(Vec::<BankSuggestionRow>::new())));
        ui.set_accounts_add_bank_central_note(
            i18n::format(
                Language::En,
                Key::CentralBlzNote,
                &[Arg::Str(mapped), Arg::Str(entry.blz.clone())],
            )
            .into(),
        );
        ui.set_accounts_dialog("add-bank".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-add-online-bank-central-note-en", 1100, 720);
        assert_varied(&buffer, "accounts-add-online-bank-central-note-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_online(true);
        ui.set_accounts_add_bank_search("zzzzzz".into());
        ui.set_accounts_add_bank_suggestions(ModelRc::new(VecModel::from(Vec::<BankSuggestionRow>::new())));
        ui.set_accounts_dialog("add-bank".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-add-online-bank-no-match-de-dark", 1100, 720);
        assert_varied(&buffer, "accounts-add-online-bank-no-match-de-dark");
        let _ = ui.hide();
    }

    {
        let bank = accounts_state
            .bank_accesses
            .iter()
            .find(|bank| !bank.offline)
            .expect("the seeded online bank");
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_title(
            i18n::format(
                Language::En,
                Key::OnlineBankingFor,
                &[Arg::from(bank.bank_name.as_str())],
            )
            .into(),
        );
        ui.set_accounts_dialog_account_name(bank.bank_name.clone().into());
        ui.set_accounts_connection_name(bank.bank_name.clone().into());
        ui.set_accounts_dialog_bank_id(i32::try_from(bank.id.0).unwrap_or(-1));
        ui.set_accounts_dialog_offline(false);
        ui.set_accounts_connection_blz(bank.blz.clone().into());
        ui.set_accounts_connection_user_id(bank.user_id.clone().into());
        ui.set_accounts_connection_server_url(bank.server_url.clone().unwrap_or_default().into());
        ui.set_accounts_connection_pin_text(i18n::text(Language::En, Key::PinSavedForSyncs).into());
        ui.set_accounts_connection_has_pin(true);
        ui.set_accounts_dialog("connection".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-connection-en", 1100, 720);
        assert_varied(&buffer, "accounts-connection-en");
        let _ = ui.hide();
    }

    {
        let bank = accounts_state
            .bank_accesses
            .iter()
            .find(|bank| !bank.offline)
            .expect("the seeded online bank");
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_accounts_dialog_title(
            i18n::format(
                Language::En,
                Key::FetchHistoryFor,
                &[Arg::from(bank.bank_name.as_str())],
            )
            .into(),
        );
        ui.set_accounts_dialog_bank_id(i32::try_from(bank.id.0).unwrap_or(-1));
        ui.set_accounts_history_date("01.01.2026".into());
        ui.set_accounts_history_has_start(true);
        ui.set_accounts_dialog("history".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-history-en", 1100, 720);
        assert_varied(&buffer, "accounts-history-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_banking_prompt("pin".into());
        ui.set_banking_prompt_title("PIN for Sparkasse".into());
        ui.set_banking_prompt_text("Enter the PIN for meinuser.".into());
        ui.set_banking_prompt_label(i18n::text(Language::En, Key::Pin).into());
        ui.set_banking_prompt_remember_visible(true);
        ui.show().unwrap();
        let buffer = render(&window, "accounts-pin-remember-en", 1100, 720);
        assert_varied(&buffer, "accounts-pin-remember-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_banking_prompt("pin".into());
        ui.set_banking_prompt_title("PIN for Sparkasse".into());
        ui.set_banking_prompt_text("Enter the PIN for meinuser.".into());
        ui.set_banking_prompt_label(i18n::text(Language::En, Key::Pin).into());
        ui.set_banking_prompt_remember_visible(false);
        ui.show().unwrap();
        let buffer = render(&window, "accounts-pin-en", 1100, 720);
        assert_varied(&buffer, "accounts-pin-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_banking_prompt("choice".into());
        ui.set_banking_prompt_title(i18n::text(Language::En, Key::ChooseTanProcedure).into());
        ui.set_banking_prompt_choices(ModelRc::new(VecModel::from(vec![
            BankingPromptChoice {
                id: "942".into(),
                label: "chipTAN optisch".into(),
            },
            BankingPromptChoice {
                id: "946".into(),
                label: "pushTAN".into(),
            },
        ])));
        ui.show().unwrap();
        let buffer = render(&window, "accounts-tan-method-en", 1100, 720);
        assert_varied(&buffer, "accounts-tan-method-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_banking_prompt("tan".into());
        ui.set_banking_prompt_title(i18n::text(Language::En, Key::TanRequired).into());
        ui.set_banking_prompt_label(i18n::text(Language::En, Key::TanLabel).into());
        ui.set_banking_prompt_challenge("Bitte bestätigen Sie den Abruf der Umsätze.".into());
        ui.set_banking_prompt_show_image(true);
        ui.set_banking_prompt_image(slint::Image::load_from_path(&preview_path).unwrap());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-tan-photo-en", 1100, 720);
        assert_varied(&buffer, "accounts-tan-photo-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "accounts", &rows);
        apply_accounts(&ui, &accounts_state, &banking_state(&accounts_state));
        ui.set_banking_prompt("decoupled".into());
        ui.set_banking_prompt_title(i18n::text(Language::En, Key::ConfirmInBankingApp).into());
        ui.set_banking_prompt_text("Confirm the login in your banking app.".into());
        ui.show().unwrap();
        let buffer = render(&window, "accounts-decoupled-en", 1100, 720);
        assert_varied(&buffer, "accounts-decoupled-en");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "documents", &rows);
        configure_documents(&ui, Language::En, &documents, Some(&preview_path));
        ui.show().unwrap();
        let buffer = render(&window, "documents-en-light", 1100, 720);
        assert_varied(&buffer, "documents-en-light");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::De, ThemePref::Dark, "documents", &rows);
        configure_documents(&ui, Language::De, &documents, Some(&preview_path));
        ui.show().unwrap();
        let buffer = render(&window, "documents-de-dark", 1100, 720);
        assert_varied(&buffer, "documents-de-dark");
        let _ = ui.hide();
    }

    {
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "documents", &rows);
        configure_documents(&ui, Language::En, &[], None);
        ui.show().unwrap();
        let buffer = render(&window, "documents-empty-en", 1100, 720);
        assert_varied(&buffer, "documents-empty-en");
        let _ = ui.hide();
    }

    let transactions_temp = tempfile::tempdir().unwrap();
    let transactions_paths = AppPaths::resolve(Some(transactions_temp.path().as_os_str().to_owned()), None).unwrap();
    let transactions_db = app::unlock::create(
        &transactions_paths,
        SLUG,
        "correct horse battery staple",
        "correct horse battery staple",
        true,
    )
    .unwrap();
    let seeded_transactions = seed_transactions(&transactions_db);
    let transactions_en = transactions_state(&transactions_db, Language::En, StartArgs::default(), today);
    let transactions_de = transactions_state(&transactions_db, Language::De, StartArgs::default(), today);

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-en-light",
        &transactions_en,
        |_| {},
    );
    let dark_transactions = render_transactions(
        &window,
        Language::De,
        ThemePref::Dark,
        "transactions-de-dark",
        &transactions_de,
        |_| {},
    );
    assert_dark_widget(
        &dark_transactions,
        380,
        100,
        "transactions-de-dark account filter field",
    );
    assert_dark_widget(&dark_transactions, 1120, 172, "transactions-de-dark period ComboBox");

    let account_state = transactions_state(
        &transactions_db,
        Language::En,
        StartArgs {
            account_id: Some(seeded_transactions.checking),
            period: Some(Period::All),
            ..Default::default()
        },
        today,
    );
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-chart-en",
        &account_state,
        |_| {},
    );
    let mut collapsed = account_state.clone();
    collapsed.chart_collapsed = true;
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-chart-collapsed-en",
        &collapsed,
        |_| {},
    );

    let mut selection = transactions_en.clone();
    selection.selected_ids = selection
        .transactions
        .iter()
        .take(3)
        .map(|transaction| transaction.id)
        .collect();
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-selection-en",
        &selection,
        |_| {},
    );

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-multi-delete-en",
        &selection,
        |ui| open_multi_delete_for_test(ui, &selection),
    );

    let mut detail = account_state.clone();
    let detail_transaction = detail
        .transactions
        .iter()
        .find(|transaction| {
            detail
                .balances
                .as_ref()
                .and_then(|balances| balances.get(&transaction.id))
                .is_some_and(|entry| entry.bank_difference.is_some())
        })
        .or_else(|| {
            detail
                .transactions
                .iter()
                .find(|transaction| transaction.is_tax_relevant)
        })
        .or_else(|| detail.transactions.first())
        .cloned()
        .unwrap();
    detail.selected = Some(detail_transaction);
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-detail-en",
        &detail,
        |_| {},
    );

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-add-en",
        &transactions_en,
        |ui| {
            ui.set_transactions_add_open(true);
            ui.set_transactions_add_date(format::date(today).into());
            ui.set_transactions_add_account_options(ModelRc::new(VecModel::from(
                transactions_en
                    .accounts
                    .iter()
                    .map(|account| {
                        SharedString::from(format!(
                            "{} · {}",
                            accounts_label(account, Language::En),
                            account.currency.as_str()
                        ))
                    })
                    .collect::<Vec<_>>(),
            )));
            ui.set_transactions_add_account_index(
                transactions_en
                    .accounts
                    .iter()
                    .position(|account| account.id == seeded_transactions.checking)
                    .map_or(0, |index| i32::try_from(index).unwrap_or(0)),
            );
            ui.set_transactions_add_category_names(ModelRc::new(VecModel::from(
                std::iter::once(SharedString::from(i18n::text(Language::En, Key::AutoRules)))
                    .chain(
                        transactions_en
                            .categories
                            .iter()
                            .map(|category| SharedString::from(category.name.clone())),
                    )
                    .collect::<Vec<_>>(),
            )));
            ui.set_transactions_add_amount_label(
                i18n::format(
                    Language::En,
                    Key::AmountIn,
                    &[Arg::Str(format::currency_symbol(Currency::EUR))],
                )
                .into(),
            );
        },
    );

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-duplicate-en",
        &transactions_en,
        |ui| {
            ui.set_transactions_duplicate_open(true);
            ui.set_transactions_duplicate_title(i18n::text(Language::En, Key::DuplicateTransactionTitle).into());
            ui.set_transactions_duplicate_text(
                i18n::format(
                    Language::En,
                    Key::DuplicateTransactionText,
                    &[
                        Arg::from(format::date(today)),
                        Arg::from(format::money(
                            Money {
                                minor: -1_200,
                                currency: Currency::EUR,
                            },
                            Language::En,
                            false,
                        )),
                    ],
                )
                .into(),
            );
        },
    );

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-custom-range-en",
        &transactions_en,
        |ui| {
            ui.set_transactions_range_open(true);
            ui.set_transactions_range_from("01.08.2026".into());
            ui.set_transactions_range_to("31.10.2026".into());
            ui.set_transactions_range_error("".into());
        },
    );

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-narrow-en",
        &transactions_en,
        |_| {},
    );

    let import_temp = tempfile::tempdir().unwrap();
    let new_file = write_import_csv(
        import_temp.path(),
        "november.csv",
        "DE89370400440532013000",
        &[
            ("05.11.2026", "Arbeitgeber GmbH", "Gehalt November", "300,00", "120,00"),
            ("09.11.2026", "Buchhandlung", "Bücher", "276,00", "-24,00"),
        ],
    );
    let partly_duplicate = write_import_csv(
        import_temp.path(),
        "oktober.csv",
        "DE89370400440532013000",
        &[
            ("02.08.2026", "Example Market", "groceries", "0,00", "-120,00"),
            ("21.10.2026", "Bäckerei", "Frühstück", "-8,00", "-8,00"),
        ],
    );
    let unreadable = import_temp.path().join("missing.csv");
    let import_files = vec![new_file, partly_duplicate, unreadable];
    let mut import_view = TransactionsView::new(Language::En);
    import_view
        .start(&transactions_db, StartArgs::default(), today)
        .unwrap();
    import_view
        .open_import(&transactions_db, &import_files, None, today)
        .unwrap();
    let import_en = import_view.state().clone();
    assert!(import_en.import_preview.is_some(), "the import preview is prepared");
    let mut import_de_view = TransactionsView::new(Language::De);
    import_de_view
        .start(&transactions_db, StartArgs::default(), today)
        .unwrap();
    import_de_view
        .open_import(&transactions_db, &import_files, None, today)
        .unwrap();
    let import_de = import_de_view.state().clone();

    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-import-en-light",
        &import_en,
        |_| {},
    );
    render_transactions(
        &window,
        Language::De,
        ThemePref::Dark,
        "transactions-import-de-dark",
        &import_de,
        |_| {},
    );
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-import-csv-en",
        &import_en,
        |_| {},
    );

    let health_category = transactions_en
        .categories
        .iter()
        .find(|category| category.name == "Health")
        .map(|category| category.id)
        .unwrap();
    rules::create(
        &transactions_db,
        NewRule {
            pattern: "Example Market".to_owned(),
            category_id: health_category,
            priority: 5,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
    let mut reapply_view = TransactionsView::new(Language::En);
    reapply_view
        .start(&transactions_db, StartArgs::default(), today)
        .unwrap();
    reapply_view.preview_reapply_rules(&transactions_db, today).unwrap();
    let reapply = reapply_view.state().clone();
    assert!(reapply.reapply_preview.is_some(), "the re-apply preview is prepared");
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-reapply-en",
        &reapply,
        |_| {},
    );

    let rule_state = detail.clone();
    let rule_categories = rule_state
        .categories
        .iter()
        .map(|category| SharedString::from(category.name.clone()))
        .collect::<Vec<_>>();
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-rule-dialog-en",
        &rule_state,
        move |ui| {
            ui.set_transactions_rule_open(true);
            ui.set_transactions_rule_pattern("Example Market".into());
            ui.set_transactions_rule_categories(ModelRc::new(VecModel::from(rule_categories)));
            ui.set_transactions_rule_category_index(0);
            ui.set_transactions_rule_error("".into());
        },
    );

    let mut menu_state = transactions_en.clone();
    menu_state.can_undo_reapply = true;
    render_transactions(
        &window,
        Language::En,
        ThemePref::Light,
        "transactions-more-menu-en",
        &menu_state,
        |ui| {
            ui.set_transactions_more_menu_open(true);
        },
    );
}

/// A mouse click lands on the control the first time, even when another control had focus first.
#[test]
fn one_click_activates_a_control() {
    let _serial = serial_test();
    let window = platform_window();

    let ui = MainWindow::new().unwrap();
    configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &[]);

    let navigated = Rc::new(RefCell::new(Vec::<String>::new()));
    {
        let navigated = navigated.clone();
        ui.on_navigate(move |screen| navigated.borrow_mut().push(screen.to_string()));
    }
    let themes = Rc::new(RefCell::new(Vec::<String>::new()));
    {
        let themes = themes.clone();
        ui.on_set_theme(move |theme| themes.borrow_mut().push(theme.to_string()));
    }

    ui.show().unwrap();
    // Settle the layout at the usual window size before using its coordinates.
    render(&window, "one-click-activates-a-control", 1100, 720);

    // The theme button must act on its own first click: on the unfixed code its FocusScope eats
    // the press and only gains focus.
    click(&window, 1058.0, 32.0);
    assert_eq!(themes.borrow().len(), 1, "one click toggles the theme exactly once");
    assert_eq!(themes.borrow()[0], "dark");

    // The theme click above is also the "click another control first" case. One click on the
    // Accounts sidebar item (240px sidebar, 16px padding, 56px logo, 20px gap, 40px Dashboard
    // item, 4px spacing) must now navigate to that page.
    click(&window, 120.0, 164.0);
    assert_eq!(
        navigated.borrow().len(),
        1,
        "one click navigates exactly once, after another control had focus"
    );
    assert_eq!(navigated.borrow()[0], "accounts");
    assert_eq!(themes.borrow().len(), 1, "the sidebar click does not toggle the theme");

    let _ = ui.hide();
}

/// Sends one left press and release at a logical window position.
fn click(window: &MinimalSoftwareWindow, x: f32, y: f32) {
    let position = slint::LogicalPosition { x, y };
    window.dispatch_event(slint::platform::WindowEvent::PointerPressed {
        position,
        button: slint::platform::PointerEventButton::Left,
    });
    window.dispatch_event(slint::platform::WindowEvent::PointerReleased {
        position,
        button: slint::platform::PointerEventButton::Left,
    });
}

/// Writes one small synthetic CSV statement for the import screenshots.
fn write_import_csv(directory: &Path, name: &str, iban: &str, rows: &[(&str, &str, &str, &str, &str)]) -> PathBuf {
    let path = directory.join(name);
    let mut text = format!("IBAN;{iban}\n\n");
    text.push_str("Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n");
    for (date, partner, purpose, saldo, amount) in rows {
        text.push_str(&format!(
            "{date};{date};{partner};Lastschrift;{purpose};{saldo};EUR;{amount};EUR\n"
        ));
    }
    std::fs::write(&path, text).unwrap();
    path
}

/// Three documents: one linked, one with OCR text, one selected. Returns them oldest first, as the
/// page lists them in the seeded screenshot.
fn seed_documents(db: &finglass_core::db::Database, temp: &Path) -> Vec<DocumentItem> {
    let account = accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Checking".to_owned()),
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

    transactions::insert_planned(
        db,
        account,
        vec![NewTransaction {
            booking_date: time::Date::from_calendar_date(2026, time::Month::March, 13).unwrap(),
            amount: Money {
                minor: -378,
                currency: Currency::EUR,
            },
            balance_after: None,
            partner_name: Some("REWE Markt GmbH".to_owned()),
            purpose: Some("Wocheneinkauf".to_owned()),
            category_id: None,
            import_hash: "screenshot-rewe".to_owned(),
            created_at: time::OffsetDateTime::from_unix_timestamp(1_760_000_000).unwrap(),
            is_new: false,
            tax_override: None,
        }],
    )
    .unwrap();
    let transaction = transactions::find_items(db, &TransactionFilter::default(), None)
        .unwrap()
        .into_iter()
        .next()
        .unwrap()
        .id;

    documents::create(
        db,
        NewDocument {
            file_name: "baeckerei-beleg.png".to_owned(),
            stored_path: temp
                .join("documents/baeckerei-beleg.png")
                .to_string_lossy()
                .into_owned(),
            mime_type: "image/png".to_owned(),
            transaction_id: None,
        },
    )
    .unwrap();
    let linked = documents::create(
        db,
        NewDocument {
            file_name: "stadtwerke-rechnung.pdf".to_owned(),
            stored_path: temp
                .join("documents/stadtwerke-rechnung.pdf")
                .to_string_lossy()
                .into_owned(),
            mime_type: "application/pdf".to_owned(),
            transaction_id: Some(transaction),
        },
    )
    .unwrap();
    documents::save_ocr_text(
        db,
        linked.id,
        "Stadtwerke Musterstadt\nAbschlag März 2026\nBetrag 37,80 EUR",
        "deu+eng",
        Some(91.0),
    )
    .unwrap();

    let selected = documents::create(
        db,
        NewDocument {
            file_name: "kassenbon.jpg".to_owned(),
            stored_path: temp.join("kassenbon.png").to_string_lossy().into_owned(),
            mime_type: "image/jpeg".to_owned(),
            transaction_id: None,
        },
    )
    .unwrap();
    documents::save_ocr_text(
        db,
        selected.id,
        "REWE Markt GmbH\nDatum: 13.03.2026\nSUMME EUR 3,78",
        "deu+eng",
        Some(84.0),
    )
    .unwrap();

    let mut items = documents::find_items(db, None, false, None).unwrap();
    items.sort_by_key(|document| document.id);
    items
}

/// Writes a small image so the detail panel's image preview has something to show.
fn write_preview_png(path: &Path) {
    let width = 120_u32;
    let height = 80_u32;
    let mut data = Vec::with_capacity((width * height * 3) as usize);
    for y in 0..height {
        for x in 0..width {
            let border = x < 6 || y < 6 || x >= width - 6 || y >= height - 6;
            let (red, green, blue) = if border { (13, 148, 136) } else { (245, 247, 250) };
            data.extend_from_slice(&[red, green, blue]);
        }
    }

    let file = std::fs::File::create(path).unwrap();
    let mut encoder = png::Encoder::new(BufWriter::new(file), width, height);
    encoder.set_color(png::ColorType::Rgb);
    encoder.set_depth(png::BitDepth::Eight);
    let mut writer = encoder.write_header().unwrap();
    writer.write_image_data(&data).unwrap();
}

fn configure_documents(ui: &MainWindow, language: Language, items: &[DocumentItem], preview: Option<&Path>) {
    let selected = items.iter().find(|document| document.file_name == "kassenbon.jpg");
    let linked = items.len();
    let unlinked = items
        .iter()
        .filter(|document| document.transaction_id.is_none())
        .count();
    let linked_count = linked.saturating_sub(unlinked);
    let tab = |key: Key, count: usize| {
        i18n::format(
            language,
            Key::TabWithCount,
            &[i18n::text(language, key).into(), Arg::Int(count as i64)],
        )
    };

    ui.set_document_rows(ModelRc::new(VecModel::from(
        items
            .iter()
            .map(|document| DocumentRow {
                id: i32::try_from(document.id).unwrap_or(i32::MAX),
                name: document.file_name.clone().into(),
                meta: format!(
                    "{} · {}",
                    document_date(document.uploaded_at),
                    document
                        .transaction_label
                        .clone()
                        .unwrap_or_else(|| { i18n::text(language, Key::NotLinked).to_owned() })
                )
                .into(),
                is_linked: document.transaction_id.is_some(),
                has_ocr: document.has_ocr,
                is_pdf: document.is_pdf(),
                selected: selected.is_some_and(|selected| selected.id == document.id),
            })
            .collect::<Vec<_>>(),
    )));
    ui.set_documents_tab("all".into());
    ui.set_documents_tab_all_label(tab(Key::TabAllDocuments, items.len()).into());
    ui.set_documents_tab_unlinked_label(tab(Key::TabUnlinked, unlinked).into());
    ui.set_documents_tab_linked_label(tab(Key::TabLinked, linked_count).into());
    ui.set_documents_search_query("".into());
    ui.set_documents_ocr_available(true);
    ui.set_documents_ocr_hint("".into());
    ui.set_documents_uploading(false);
    ui.set_documents_progress_text("".into());
    ui.set_documents_loading(false);
    ui.set_documents_error("".into());
    ui.set_documents_message("".into());
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
    ui.set_documents_selected_ocr_heading(
        selected
            .and_then(|document| document.ocr_confidence)
            .map_or_else(
                || i18n::text(language, Key::ExtractedText).to_owned(),
                |confidence| {
                    i18n::format(
                        language,
                        Key::ExtractedTextConfidence,
                        &[Arg::Int(confidence.round() as i64)],
                    )
                },
            )
            .into(),
    );
    ui.set_documents_selected_ocr_text(
        if selected.is_some() {
            "REWE Markt GmbH\nDatum: 13.03.2026\nSUMME EUR 3,78"
        } else {
            ""
        }
        .into(),
    );
    ui.set_documents_preview_kind(if preview.is_some() { "image" } else { "none" }.into());
    ui.set_documents_preview(
        preview
            .and_then(|path| Image::load_from_path(path).ok())
            .unwrap_or_default(),
    );
    ui.set_documents_suggestions(ModelRc::new(VecModel::from(if selected.is_some() {
        vec![DocumentLinkRow {
            id: 1,
            title: "REWE Markt GmbH".into(),
            meta: "13.03.2026 · -€3.78".into(),
            badge: i18n::format(
                language,
                Key::MatchPercent,
                &[Arg::Str(if language == Language::En {
                    "80%".to_owned()
                } else {
                    "80\u{a0}%".to_owned()
                })],
            )
            .into(),
        }]
    } else {
        Vec::new()
    })));
    ui.set_documents_candidates(ModelRc::new(VecModel::from(Vec::<DocumentLinkRow>::new())));
    ui.set_documents_candidate_query("".into());
    ui.set_documents_delete_text(
        selected
            .map(|document| {
                i18n::format(
                    language,
                    Key::DeleteDocumentText,
                    &[Arg::Str(document.file_name.clone())],
                )
            })
            .unwrap_or_default()
            .into(),
    );
}

fn document_date(value: time::OffsetDateTime) -> String {
    format!(
        "{:02}.{:02}.{:04} {:02}:{:02}",
        value.day(),
        value.month() as u8,
        value.year(),
        value.hour(),
        value.minute()
    )
}

struct SeededTransactions {
    checking: AccountId,
}

fn transactions_state(db: &Database, language: Language, args: StartArgs, today: Date) -> TransactionsState {
    let mut view = TransactionsView::new(language);
    view.start(db, args, today).unwrap();
    view.update_period(db, Period::All, today).unwrap();
    view.state().clone()
}

fn render_transactions(
    window: &Rc<MinimalSoftwareWindow>,
    language: Language,
    theme: ThemePref,
    name: &str,
    state: &TransactionsState,
    configure: impl FnOnce(&MainWindow),
) -> SharedPixelBuffer<Rgb8Pixel> {
    let width = if name.ends_with("narrow-en") { 900 } else { 1440 };
    let ui = MainWindow::new().unwrap();
    configure_main(&ui, language, theme, "transactions", &[]);
    ui.set_transactions_show_panel(width >= 1000);
    transactions_page::apply(&ui, state);
    configure(&ui);
    ui.show().unwrap();
    let buffer = render(window, name, width, 720);
    assert_varied(&buffer, name);
    let _ = ui.hide();
    buffer
}

fn open_multi_delete_for_test(ui: &MainWindow, state: &TransactionsState) {
    let bookings = state.selected_shown();
    let from = bookings.iter().map(|transaction| transaction.date).min().unwrap();
    let to = bookings.iter().map(|transaction| transaction.date).max().unwrap();
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
                Language::En,
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
                finglass_core::bank_text::display_description(transaction),
                format::money(transaction.amount, Language::En, false),
            )
        })
        .collect::<Vec<_>>();
    let more = bookings.len().saturating_sub(5);
    ui.set_transactions_multi_delete_title(
        i18n::format(Language::En, Key::DeleteTransactionsTitle, &[Arg::from(bookings.len())]).into(),
    );
    ui.set_transactions_multi_delete_range(range.into());
    ui.set_transactions_multi_delete_totals(totals.into());
    ui.set_transactions_multi_delete_lines(ModelRc::new(VecModel::from(
        lines.into_iter().map(SharedString::from).collect::<Vec<_>>(),
    )));
    ui.set_transactions_multi_delete_more(
        if more > 0 {
            i18n::format(Language::En, Key::AndMore, &[Arg::from(more)])
        } else {
            String::new()
        }
        .into(),
    );
    ui.set_transactions_multi_delete_open(true);
}

fn seed_transactions(db: &Database) -> SeededTransactions {
    let checking = accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Haushalt".to_owned()),
            iban: Some("DE89370400440532013000".to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Checking,
            balance: Money {
                minor: 245_037,
                currency: Currency::EUR,
            },
            balance_updated_at: Some(OffsetDateTime::now_utc()),
            group_id: None,
        },
    )
    .unwrap();
    let savings = accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Rücklage".to_owned()),
            iban: Some("DE44500105175407324930".to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Savings,
            balance: Money {
                minor: 800_000,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap();
    let usd = Currency::new("USD").unwrap();
    let dollar = accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Dollar".to_owned()),
            iban: Some("DE44500105175407324931".to_owned()),
            account_number: None,
            currency: usd,
            account_type: AccountType::Savings,
            balance: Money {
                minor: 50_000,
                currency: usd,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap();
    let group = account_groups::create(
        db,
        NewAccountGroup {
            name: "Reisen".to_owned(),
            sort_order: 1,
        },
    )
    .unwrap();
    accounts::set_group(db, savings, Some(group)).unwrap();
    accounts::set_group(db, dollar, Some(group)).unwrap();

    let food = category(db, "Food", "#FFC107", false, false, None);
    let salary = category(db, "Salary", "#4CAF50", false, false, None);
    let health = category(db, "Health", "#2196F3", true, false, None);
    let office = category(db, "Office", "#FF5722", true, false, None);

    let mut counter = 0_i64;
    for month in 8..=10 {
        let month = time::Month::try_from(month).unwrap();
        for (day, minor, partner, category_id, new) in [
            (2_u8, -12_000_i64, "Example Market", Some(food), true),
            (3, 150_000, "Employer GmbH", Some(salary), true),
            (5, -1_500, "Corner Kiosk", None, false),
            (8, -8_900, "Beispiel-Krankenkasse", Some(health), false),
            (10, -999, "Spotify", None, false),
            (12, -3_200, "Bürobedarf GmbH", Some(office), true),
            (15, -2_550, "Apotheke am Ring", Some(health), false),
            (18, -4_500, "Example Market", Some(food), false),
            (22, 25_000, "Beispielhilfe e. V.", None, false),
            (26, -6_000, "Bürobedarf GmbH", Some(office), true),
        ] {
            let date = Date::from_calendar_date(2026, month, day).unwrap();
            let back = (counter == 4).then_some(245_037);
            seed_booking(
                db,
                checking,
                Currency::EUR,
                date,
                minor,
                partner,
                category_id,
                new,
                back,
                None,
            );
            counter += 1;
        }
    }

    for (day, minor) in [
        (time::macros::date!(2026 - 08 - 14), -1_199_i64),
        (time::macros::date!(2026 - 09 - 14), -1_199),
        (time::macros::date!(2026 - 10 - 14), -1_199),
    ] {
        seed_booking(db, dollar, usd, day, minor, "Cloud Service", None, false, None, None);
    }
    seed_booking(
        db,
        savings,
        Currency::EUR,
        time::macros::date!(2026 - 09 - 28),
        100_000,
        "Transfer from Haushalt",
        None,
        false,
        None,
        None,
    );

    finglass_core::repo::pending_bookings::replace_for_account(
        db,
        checking,
        vec![
            NewPendingBooking {
                date: time::macros::date!(2026 - 10 - 14),
                value_date: None,
                amount: Money {
                    minor: -2_000,
                    currency: Currency::EUR,
                },
                partner_name: Some("Bäckerei Muster".to_owned()),
                purpose: Some("Brötchen".to_owned()),
            },
            NewPendingBooking {
                date: time::macros::date!(2026 - 10 - 15),
                value_date: None,
                amount: Money {
                    minor: 12_000,
                    currency: Currency::EUR,
                },
                partner_name: Some("Beispiel AG".to_owned()),
                purpose: Some("Gehalt Oktober".to_owned()),
            },
        ],
        OffsetDateTime::now_utc(),
    )
    .unwrap();

    SeededTransactions { checking }
}

#[allow(clippy::too_many_arguments)]
fn seed_booking(
    db: &Database,
    account: AccountId,
    currency: Currency,
    day: Date,
    minor: i64,
    partner: &str,
    category: Option<CategoryId>,
    is_new: bool,
    balance_after: Option<i64>,
    tax_override: Option<bool>,
) -> TransactionId {
    let hash = format!("tx-{}-{}-{}-{}", account.0, day, minor, partner);
    transactions::insert_planned(
        db,
        account,
        vec![NewTransaction {
            booking_date: day,
            amount: Money { minor, currency },
            balance_after: balance_after.map(|minor| Money { minor, currency }),
            partner_name: Some(partner.to_owned()),
            purpose: None,
            category_id: category,
            import_hash: hash.clone(),
            created_at: OffsetDateTime::now_utc(),
            is_new,
            tax_override,
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

struct SeededAccounts {
    checking: AccountId,
    online: finglass_core::model::BankConnectionId,
}

fn seed_accounts(db: &Database) -> SeededAccounts {
    let offline = bank_connections::create_offline(db, "Bank Norwegian").unwrap();
    let online = bank_connections::create(
        db,
        finglass_core::model::NewBankConnection {
            bank_name: "Sparkasse".to_owned(),
            blz: "37040044".to_owned(),
            bic: Some("COLSDE33XXX".to_owned()),
            user_id: "meinuser".to_owned(),
        },
    )
    .unwrap();
    bank_connections::update_connection(
        db,
        online.id,
        finglass_core::model::BankConnectionUpdate {
            server_url: Some("https://fints.example/pintan".to_owned()),
            ..Default::default()
        },
    )
    .unwrap();
    for (name, iban, minor) in [
        ("Girokonto", "DE89370400440532013001", 312_540),
        ("Tagesgeld", "DE44500105175407324932", 800_000),
    ] {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: Some(online.id),
                institution: None,
                name: Some(name.to_owned()),
                iban: Some(iban.to_owned()),
                account_number: None,
                currency: Currency::EUR,
                account_type: AccountType::Checking,
                balance: Money {
                    minor,
                    currency: Currency::EUR,
                },
                balance_updated_at: Some(OffsetDateTime::now_utc()),
                group_id: None,
            },
        )
        .unwrap();
    }
    let usd = Currency::new("USD").unwrap();

    let checking = accounts::create(
        db,
        NewAccount {
            bank_connection_id: Some(offline.id),
            institution: None,
            name: Some("Haushalt".to_owned()),
            iban: Some("DE89370400440532013000".to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Checking,
            balance: Money {
                minor: 245_037,
                currency: Currency::EUR,
            },
            balance_updated_at: Some(OffsetDateTime::now_utc()),
            group_id: None,
        },
    )
    .unwrap();

    let dollar = accounts::create(
        db,
        NewAccount {
            bank_connection_id: Some(offline.id),
            institution: None,
            name: Some("Dollar".to_owned()),
            iban: Some("DE44500105175407324931".to_owned()),
            account_number: None,
            currency: usd,
            account_type: AccountType::Savings,
            balance: Money {
                minor: 50_000,
                currency: usd,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap();

    accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: None,
            iban: Some("DE44500105175407324930".to_owned()),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Loan,
            balance: Money {
                minor: -500_000,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap();

    accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: None,
            iban: Some(account_ids::card("4321")),
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::CreditCard,
            balance: Money {
                minor: -4_510,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap();

    let group = account_groups::create(
        db,
        NewAccountGroup {
            name: "Reisen".to_owned(),
            sort_order: 1,
        },
    )
    .unwrap();
    accounts::set_group(db, dollar, Some(group)).unwrap();

    for (day, minor, partner, new) in [
        (date!(2026 - 09 - 02), 150_000, "Employer GmbH", false),
        (date!(2026 - 09 - 12), -12_000, "Example Market", true),
        (date!(2026 - 10 - 02), 150_000, "Employer GmbH", false),
        (date!(2026 - 10 - 05), -1_500, "Corner Kiosk", false),
        (date!(2026 - 10 - 09), -3_200, "Example Market", false),
    ] {
        transactions::insert_planned(
            db,
            checking,
            vec![NewTransaction {
                booking_date: day,
                amount: Money {
                    minor,
                    currency: Currency::EUR,
                },
                balance_after: None,
                partner_name: Some(partner.to_owned()),
                purpose: None,
                category_id: None,
                import_hash: format!("{}-{day}-{minor}", checking.0),
                created_at: OffsetDateTime::now_utc(),
                is_new: new,
                tax_override: None,
            }],
        )
        .unwrap();
    }

    SeededAccounts {
        checking,
        online: online.id,
    }
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

fn seed_settings(db: &Database) {
    let all = categories::list(db).unwrap();
    let by_name = |name: &str| {
        all.iter()
            .find(|category| category.name == name)
            .map(|category| category.id)
            .expect("the dashboard seed created the category")
    };
    let food = by_name("Food");
    let donations = by_name("Donations");
    let health = category(db, "Health", "#E91E63", true, false, Some(20_000));

    for (pattern, category_id, priority, auto_apply, field, direction, tax) in [
        (
            "Example Market",
            food,
            60,
            true,
            RuleField::Partner,
            RuleDirection::Expense,
            None,
        ),
        (
            "Example Aid",
            donations,
            45,
            true,
            RuleField::Any,
            RuleDirection::Any,
            Some(true),
        ),
        (
            "Apotheke|Arzt",
            health,
            20,
            false,
            RuleField::Purpose,
            RuleDirection::Any,
            Some(false),
        ),
    ] {
        rules::create(
            db,
            NewRule {
                pattern: pattern.to_owned(),
                category_id,
                priority,
                auto_apply,
                match_field: field,
                direction,
                tax_relevant: tax,
            },
        )
        .unwrap();
    }

    finglass_core::settings::set_setting(db, app::settings::TAX_KEYWORDS_KEY, "arzt\napotheke\nhandwerker").unwrap();
}

fn settings_state(db: &Database, paths: &AppPaths, language: Language) -> SettingsState {
    let mut state = SettingsState::new(language);
    state.load(db, paths, None).unwrap();
    state
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
    ui.set_dashboard_month_options(ModelRc::new(VecModel::from(
        state
            .months
            .iter()
            .map(|month| SharedString::from(month_text(*month, state.language)))
            .collect::<Vec<_>>(),
    )));
    ui.set_dashboard_month_selected(
        state
            .months
            .iter()
            .position(|month| *month == state.selected_month)
            .and_then(|index| i32::try_from(index).ok())
            .unwrap_or(0),
    );

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

fn apply_settings(ui: &MainWindow, state: &SettingsState) {
    ui.set_settings_error(state.error.clone().unwrap_or_default().into());
    ui.set_settings_message(state.message.clone().unwrap_or_default().into());
    ui.set_settings_categories_empty(state.categories.is_empty());
    ui.set_settings_has_duplicate_colours(has_duplicate_colours(state));
    ui.set_settings_profile_rows(settings_profile_model(state));
    ui.set_profile_rows(settings_profile_model(state));
    ui.set_current_profile_name(
        state
            .profiles
            .iter()
            .find(|profile| profile.slug == state.open_profile)
            .or_else(|| state.profiles.first())
            .map(|profile| profile.name.clone())
            .unwrap_or_default()
            .into(),
    );
    ui.set_settings_currency_options(string_model(
        state.currency_options.iter().map(|currency| currency.as_str()),
    ));
    ui.set_settings_primary_currency(state.primary_currency.as_str().into());
    ui.set_settings_primary_currency_index(
        state
            .currency_options
            .iter()
            .position(|currency| *currency == state.primary_currency)
            .map_or(0, |index| i32::try_from(index).unwrap()),
    );
    ui.set_settings_category_rows(settings_category_model(state));
    ui.set_settings_rule_rows(settings_rule_model(state));
    ui.set_settings_tax_keywords(state.tax_keywords.clone().into());
    let keyword_count = state
        .tax_keywords
        .lines()
        .filter(|line| !line.trim().is_empty())
        .count();
    ui.set_settings_tax_keywords_count(
        i18n::format(state.language, Key::TaxKeywordsCount, &[Arg::from(keyword_count)]).into(),
    );

    ui.set_settings_backup_busy(state.is_busy);
    ui.set_settings_has_pending_restore(state.has_pending_restore);
    ui.set_settings_backup_restored_text("".into());
    match &state.restore_candidate {
        Some(candidate) => {
            ui.set_settings_restore_title(
                i18n::format(
                    state.language,
                    Key::RestoreFileTitle,
                    &[Arg::from(file_name(&candidate.backup))],
                )
                .into(),
            );
            let created = i18n::format(
                state.language,
                Key::RestoreCreated,
                &[
                    Arg::from(candidate.info.created.clone()),
                    Arg::from(candidate.info.transactions),
                    Arg::from(candidate.info.documents),
                ],
            );
            let explanation = i18n::text(state.language, Key::RestoreExplanation);
            ui.set_settings_restore_text(format!("{created}\n\n{explanation}").into());
            ui.set_settings_restore_open(true);
        }
        None => ui.set_settings_restore_open(false),
    }
    ui.set_settings_restore_passphrase_wrong(state.restore_passphrase_wrong);
    match &state.restore_passphrase_for {
        Some(backup) => {
            ui.set_settings_restore_passphrase_file(file_name(backup).into());
            ui.set_settings_restore_passphrase_open(true);
        }
        None => {
            ui.set_settings_restore_passphrase_open(false);
            ui.set_settings_restore_passphrase("".into());
        }
    }

    ui.set_category_dialog_swatches(colour_model());
    ui.set_rule_dialog_categories(string_model(
        state.categories.iter().map(|category| category.name.as_str()),
    ));
    ui.set_rule_dialog_field_options(string_model(
        [
            i18n::text(state.language, Key::RuleFieldAny),
            i18n::text(state.language, Key::RuleFieldPartner),
            i18n::text(state.language, Key::RuleFieldPurpose),
        ]
        .into_iter(),
    ));
    ui.set_rule_dialog_direction_options(string_model(
        [
            i18n::text(state.language, Key::RuleDirectionAny),
            i18n::text(state.language, Key::RuleDirectionExpense),
            i18n::text(state.language, Key::RuleDirectionIncome),
        ]
        .into_iter(),
    ));
    ui.set_rule_dialog_tax_options(string_model(
        [
            i18n::text(state.language, Key::RuleTaxAsCategory),
            i18n::text(state.language, Key::TaxRelevantChoice),
            i18n::text(state.language, Key::TaxNotRelevantChoice),
        ]
        .into_iter(),
    ));

    apply_rule_import(ui, state);
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| path.display().to_string())
}

fn has_duplicate_colours(state: &SettingsState) -> bool {
    let mut seen = std::collections::BTreeMap::<String, usize>::new();
    for category in &state.categories {
        *seen.entry(category.color.to_uppercase()).or_default() += 1;
    }
    seen.values().any(|count| *count > 1)
}

fn settings_profile_model(state: &SettingsState) -> ModelRc<ProfileRow> {
    ModelRc::new(VecModel::from(
        state
            .profiles
            .iter()
            .enumerate()
            .map(|(index, profile)| {
                let open = profile.slug == state.open_profile;
                ProfileRow {
                    index: i32::try_from(index).unwrap_or(-1),
                    name: profile.name.clone().into(),
                    open,
                    next_start: !open && profile.slug == state.next_profile,
                    can_switch: !open,
                    can_delete: !profile.is_default() && !open,
                }
            })
            .collect::<Vec<_>>(),
    ))
}

fn settings_category_model(state: &SettingsState) -> ModelRc<CategoryRow> {
    ModelRc::new(VecModel::from(
        state
            .categories
            .iter()
            .map(|category| {
                let usage = state.category_usage.get(&category.id).copied().unwrap_or(0);
                CategoryRow {
                    id: i32::try_from(category.id.0).unwrap(),
                    name: category.name.clone().into(),
                    colour: color(&category.color),
                    budget_text: category
                        .budget_limit
                        .map(|budget| {
                            i18n::format(
                                state.language,
                                Key::BudgetAmount,
                                &[Arg::from(finglass_core::format::money(budget, state.language, false))],
                            )
                        })
                        .unwrap_or_default()
                        .into(),
                    usage_text: i18n::format(state.language, Key::CategoryUsage, &[Arg::from(usage)]).into(),
                    tax_relevant: category.is_tax_relevant,
                    transfer: category.is_transfer,
                }
            })
            .collect::<Vec<_>>(),
    ))
}

fn settings_rule_model(state: &SettingsState) -> ModelRc<RuleRow> {
    ModelRc::new(VecModel::from(
        state
            .rules
            .iter()
            .map(|rule| {
                let category_name = state
                    .categories
                    .iter()
                    .find(|category| category.id == rule.category_id)
                    .map(|category| category.name.clone())
                    .unwrap_or_default();
                let mut category_text = category_name;
                match rule.tax_relevant {
                    Some(true) => {
                        category_text.push_str(" · ");
                        category_text.push_str(i18n::text(state.language, Key::RuleTaxMarkerYes));
                    }
                    Some(false) => {
                        category_text.push_str(" · ");
                        category_text.push_str(i18n::text(state.language, Key::RuleTaxMarkerNo));
                    }
                    None => {}
                }

                let mut restrictions = Vec::new();
                match rule.match_field {
                    RuleField::Any => {}
                    RuleField::Partner => restrictions.push(i18n::text(state.language, Key::RuleFieldPartner)),
                    RuleField::Purpose => restrictions.push(i18n::text(state.language, Key::RuleFieldPurpose)),
                }
                match rule.direction {
                    RuleDirection::Any => {}
                    RuleDirection::Expense => {
                        restrictions.push(i18n::text(state.language, Key::RuleDirectionExpense));
                    }
                    RuleDirection::Income => {
                        restrictions.push(i18n::text(state.language, Key::RuleDirectionIncome));
                    }
                }
                let mut prio_text = i18n::format(state.language, Key::Prio, &[Arg::from(rule.priority)]);
                if !rule.auto_apply {
                    prio_text.push_str(" · ");
                    prio_text.push_str(i18n::text(state.language, Key::Manual));
                }

                RuleRow {
                    id: i32::try_from(rule.id.0).unwrap(),
                    pattern: rule.pattern.clone().into(),
                    category_text: category_text.into(),
                    detail: restrictions.join(" · ").into(),
                    prio_text: prio_text.into(),
                }
            })
            .collect::<Vec<_>>(),
    ))
}

fn colour_model() -> ModelRc<ColourSwatch> {
    ModelRc::new(VecModel::from(
        finglass_core::repo::categories::COLORS
            .iter()
            .map(|hex| ColourSwatch {
                hex: (*hex).into(),
                colour: color(hex),
            })
            .collect::<Vec<_>>(),
    ))
}

fn string_model<'a>(values: impl Iterator<Item = &'a str>) -> ModelRc<SharedString> {
    ModelRc::new(VecModel::from(values.map(SharedString::from).collect::<Vec<_>>()))
}

fn apply_rule_import(ui: &MainWindow, state: &SettingsState) {
    let Some(preview) = &state.rule_import else {
        ui.set_rule_import_open(false);
        return;
    };
    let language = state.language;
    if !ui.get_rule_import_open() {
        ui.set_rule_import_categorize(true);
    }
    ui.set_rule_import_open(true);
    ui.set_rule_import_busy(state.is_importing_rules);
    ui.set_rule_import_new_rules_text(
        i18n::format(language, Key::RuleImportNewRules, &[Arg::from(preview.new_rules.len())]).into(),
    );
    ui.set_rule_import_categories_text(
        if preview.categories_to_create.is_empty() {
            String::new()
        } else {
            i18n::format(
                language,
                Key::RuleImportCategories,
                &[Arg::from(preview.categories_to_create.len())],
            )
        }
        .into(),
    );
    let mut categories = preview
        .categories_to_create
        .iter()
        .take(20)
        .cloned()
        .collect::<Vec<_>>()
        .join(", ");
    let more = preview.categories_to_create.len().saturating_sub(20);
    if more > 0 {
        if !categories.is_empty() {
            categories.push_str(", ");
        }
        categories.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    ui.set_rule_import_categories_list(categories.into());
    ui.set_rule_import_duplicates_text(
        if preview.duplicates == 0 {
            String::new()
        } else {
            i18n::format(language, Key::RuleImportDuplicates, &[Arg::from(preview.duplicates)])
        }
        .into(),
    );
    ui.set_rule_import_conflicts_text(
        if preview.conflicts.is_empty() {
            String::new()
        } else {
            i18n::format(
                language,
                Key::RuleImportConflicts,
                &[Arg::from(preview.conflicts.len())],
            )
        }
        .into(),
    );
    let mut conflicts = preview
        .conflicts
        .iter()
        .take(20)
        .map(|conflict| {
            i18n::format(
                language,
                Key::RuleImportConflictLine,
                &[
                    Arg::from(conflict.line),
                    Arg::from(conflict.pattern.clone()),
                    Arg::from(conflict.file_category.clone()),
                    Arg::from(conflict.existing_category.clone()),
                ],
            )
        })
        .collect::<Vec<_>>()
        .join("\n");
    let more = preview.conflicts.len().saturating_sub(20);
    if more > 0 {
        if !conflicts.is_empty() {
            conflicts.push('\n');
        }
        conflicts.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    ui.set_rule_import_conflicts_list(conflicts.into());
    ui.set_rule_import_errors_text(
        if preview.errors.is_empty() {
            String::new()
        } else {
            i18n::format(language, Key::RuleImportErrors, &[Arg::from(preview.errors.len())])
        }
        .into(),
    );
    let mut errors = preview
        .errors
        .iter()
        .take(20)
        .map(|error| {
            i18n::format(
                language,
                Key::RuleImportErrorLine,
                &[Arg::from(error.line), Arg::from(error.reason.clone())],
            )
        })
        .collect::<Vec<_>>()
        .join("\n");
    let more = preview.errors.len().saturating_sub(20);
    if more > 0 {
        if !errors.is_empty() {
            errors.push('\n');
        }
        errors.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    ui.set_rule_import_errors_list(errors.into());
    ui.set_rule_import_show_categorize(preview.match_count > 0);
    ui.set_rule_import_categorize_text(
        i18n::format(language, Key::RuleImportCategorize, &[Arg::from(preview.match_count)]).into(),
    );
    ui.set_rule_import_confirm_enabled(
        !state.is_importing_rules && (!preview.new_rules.is_empty() || !preview.categories_to_create.is_empty()),
    );
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
    ui.set_profile_label(
        i18n::format(
            language,
            Key::UnlockProfile,
            &[finglass_core::profiles::DEFAULT_NAME.into()],
        )
        .into(),
    );
    ui.set_passphrase_hint(i18n::format(language, Key::PassphraseHint, &[10i64.into()]).into());
}

fn configure_migration(ui: &UnlockWindow, language: Language, theme: ThemePref, step: &str) {
    configure_unlock(ui, language, theme, "migrate", "");
    ui.set_migration_step(step.into());
    ui.set_migration_profile_names(ModelRc::new(VecModel::from(vec![
        SharedString::from("Personal"),
        SharedString::from("Business"),
    ])));
    ui.set_migration_profile_count(2);
    ui.set_migration_profile_name("Business".into());
    ui.set_migration_profile_heading(
        i18n::format(
            language,
            Key::MigrationProfileHeading,
            &[2.into(), 2.into(), "Business".into()],
        )
        .into(),
    );
    ui.set_migration_same_passphrase(true);
    ui.set_migration_new_required(false);
    ui.set_migration_summary_lines(ModelRc::new(VecModel::from(Vec::<SharedString>::new())));
    ui.set_acknowledged(false);
    ui.set_migration_passphrase("".into());
    ui.set_migration_new_passphrase("".into());
    ui.set_migration_repeat("".into());
}

fn configure_main(ui: &MainWindow, language: Language, theme: ThemePref, screen: &str, rows: &[app::AuditRow]) {
    install_translations_main(ui, language);
    sync_theme(ui, theme);
    ui.set_app_name("Finglass".into());
    ui.set_version(finglass_core::VERSION.into());
    ui.set_about_version_text(i18n::format(language, Key::AboutVersion, &[finglass_core::VERSION.into()]).into());
    ui.set_current_screen(screen.into());
    ui.set_theme_pref(theme.code().into());
    // The software renderer has no native menu bar and would draw Slint's fallback bar in the window.
    ui.set_menu_bar_visible(false);
    ui.set_language(language.code().into());
    ui.set_sidebar_collapsed(false);
    ui.set_language_menu_open(false);
    ui.set_dashboard_month_menu_open(false);
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
        let theme_global = self.global::<Theme>();
        theme_global.set_mode(theme.code().into());
        theme_global.invoke_apply_scheme();
    }
}

impl ThemeMode for MainWindow {
    fn set_theme_mode(&self, theme: ThemePref) {
        let theme_global = self.global::<Theme>();
        theme_global.set_mode(theme.code().into());
        theme_global.invoke_apply_scheme();
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

fn pixel_at(buffer: &SharedPixelBuffer<Rgb8Pixel>, x: u32, y: u32) -> Rgb8Pixel {
    let index = (y * buffer.width() + x) as usize;
    buffer.as_slice()[index]
}

fn luminance(pixel: Rgb8Pixel) -> f32 {
    (0.2126 * f32::from(pixel.r) + 0.7152 * f32::from(pixel.g) + 0.0722 * f32::from(pixel.b)) / 255.0
}

fn assert_dark_widget(buffer: &SharedPixelBuffer<Rgb8Pixel>, x: u32, y: u32, name: &str) {
    let pixel = pixel_at(buffer, x, y);
    let luminance = luminance(pixel);
    assert!(
        luminance < 0.35,
        "{name}: widget pixel at ({x}, {y}) has luminance {luminance:.3} ({pixel:?}), expected a dark widget"
    );
}

fn assert_light_widget(buffer: &SharedPixelBuffer<Rgb8Pixel>, x: u32, y: u32, name: &str) {
    let pixel = pixel_at(buffer, x, y);
    let luminance = luminance(pixel);
    assert!(
        luminance > 0.8,
        "{name}: widget pixel at ({x}, {y}) has luminance {luminance:.3} ({pixel:?}), expected a light widget"
    );
}

fn assert_widget_tone(buffer: &SharedPixelBuffer<Rgb8Pixel>, x: u32, y: u32, dark: bool, name: &str) {
    if dark {
        assert_dark_widget(buffer, x, y, name);
    } else {
        assert_light_widget(buffer, x, y, name);
    }
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

const ACCOUNTS_CURRENCY_CODES: [&str; 6] = ["EUR", "USD", "GBP", "CHF", "JPY", "CAD"];
const ACCOUNTS_DETAIL_DAYS: [i64; 3] = [30, 90, 365];

fn apply_accounts(ui: &MainWindow, state: &AccountsState, banking: &BankingState) {
    let language = state.language;
    let has_content = !state.bank_accesses.is_empty() || !state.accounts.is_empty();

    ui.set_accounts_loading(state.is_loading);
    ui.set_accounts_show_content(state.is_loading || has_content);
    ui.set_accounts_has_content(has_content);
    ui.set_accounts_error_text(state.error.clone().unwrap_or_default().into());
    ui.set_accounts_message_text(state.message.clone().unwrap_or_default().into());
    ui.set_accounts_banking_busy(banking.is_syncing());
    ui.set_accounts_sync_all_progress(accounts_sync_all_text(banking).into());
    ui.set_accounts_sync_all_visible(banking.banks.iter().any(|bank| !bank.offline));
    ui.set_accounts_sync_all_enabled(
        !banking.is_syncing()
            && banking
                .banks
                .iter()
                .any(|bank| bank.include_in_sync_all && !bank.offline),
    );
    ui.set_accounts_figures(accounts_figures(state, language));
    ui.set_accounts_bank_cards(ModelRc::new(VecModel::from(accounts_bank_cards(
        state, banking, language,
    ))));
    ui.set_accounts_panel_rows(ModelRc::new(VecModel::from(accounts_panel_rows(state, language))));
    ui.set_accounts_group_choices(ModelRc::new(VecModel::from(
        state
            .custom_groups
            .iter()
            .map(|group| AccountGroupChoice {
                id: i32::try_from(group.id.0).unwrap_or(-1),
                name: group.name.clone().into(),
            })
            .collect::<Vec<AccountGroupChoice>>(),
    )));
    ui.set_accounts_filter_text(state.account_filter_text.clone().into());
    ui.set_accounts_shown_text(accounts_shown_text(state, language).into());
    ui.set_accounts_type_choices(ModelRc::new(VecModel::from(
        ACCOUNT_TYPE_CODES
            .iter()
            .map(|code| AccountTypeChoice {
                code: (*code).into(),
                label: accounts_type_label(code, language).into(),
            })
            .collect::<Vec<AccountTypeChoice>>(),
    )));
    ui.set_accounts_currency_codes(ModelRc::new(VecModel::from(
        ACCOUNTS_CURRENCY_CODES
            .iter()
            .map(|code| (*code).into())
            .collect::<Vec<SharedString>>(),
    )));
    ui.set_accounts_days_choices(ModelRc::new(VecModel::from(
        ACCOUNTS_DETAIL_DAYS
            .iter()
            .map(|days| AccountDaysChoice {
                days: i32::try_from(*days).unwrap_or_default(),
                label: accounts_days_label(*days, language).into(),
            })
            .collect::<Vec<AccountDaysChoice>>(),
    )));
    ui.set_accounts_day_labels(ModelRc::new(VecModel::from(
        ACCOUNTS_DETAIL_DAYS
            .iter()
            .map(|days| SharedString::from(accounts_days_label(*days, language)))
            .collect::<Vec<SharedString>>(),
    )));

    apply_accounts_detail(ui, state, language);
}

fn apply_accounts_detail(ui: &MainWindow, state: &AccountsState, language: Language) {
    let Some(detail) = &state.detail else {
        ui.set_accounts_detail_open(false);
        ui.set_accounts_detail_rows(ModelRc::new(VecModel::from(Vec::<AccountDetailRow>::new())));
        ui.set_accounts_detail_days(90);
        return;
    };

    ui.set_accounts_detail_open(true);
    ui.set_accounts_detail_title(accounts_detail_title(detail, language).into());
    ui.set_accounts_detail_subtitle(accounts_identifier(&detail.account).into());
    ui.set_accounts_detail_balance(format::money(detail.account.balance, language, false).into());
    ui.set_accounts_detail_tone(accounts_tone(&detail.account).into());
    ui.set_accounts_detail_account_id(i32::try_from(detail.account.id.0).unwrap_or(-1));
    ui.set_accounts_detail_days(i32::try_from(detail.days).unwrap_or(90));
    ui.set_accounts_detail_summary(
        i18n::format(
            language,
            Key::LastDaysSummary,
            &[
                detail.days.into(),
                format::money(detail.income_in_period, language, false).into(),
                format::money(detail.expenses_in_period, language, false).into(),
            ],
        )
        .into(),
    );
    let (path, top, bottom) = accounts_balance_path(&detail.history, language);
    ui.set_accounts_detail_path(path.into());
    ui.set_accounts_detail_axis_top(top.into());
    ui.set_accounts_detail_axis_bottom(bottom.into());
    ui.set_accounts_detail_rows(ModelRc::new(VecModel::from(
        detail
            .recent
            .iter()
            .map(|transaction| AccountDetailRow {
                date_text: format::date(transaction.date).into(),
                title: transaction.raw_description().into(),
                amount_text: format::money(transaction.amount, language, true).into(),
                expense: transaction.amount.minor < 0,
            })
            .collect::<Vec<AccountDetailRow>>(),
    )));
}

fn accounts_figures(state: &AccountsState, language: Language) -> ModelRc<AccountsFigure> {
    let all_accounts: Vec<GroupingAccount> = state.accounts.iter().map(accounts_grouping).collect();
    let loans: Vec<GroupingAccount> = state
        .accounts
        .iter()
        .filter(|account| account.account_type == LOAN_TYPE)
        .map(accounts_grouping)
        .collect();

    let mut figures = Vec::new();
    if !state.accounts.is_empty() {
        let label_key = if loans.is_empty() {
            Key::AccountsTotalLabel
        } else {
            Key::AccountsNetLabel
        };
        figures.push(AccountsFigure {
            label: i18n::format(language, label_key, &[state.accounts.len().into()]).into(),
            values: accounts_amounts(&state.grand_total, &all_accounts, language, state.grand_total.len() > 1),
            secondary: false,
        });
    }
    if !loans.is_empty() {
        let totals = account_grouping::grand_total(&loans, state.primary_currency);
        figures.push(AccountsFigure {
            label: i18n::text(language, Key::AccountsLoansLabel).into(),
            values: accounts_amounts(&totals, &loans, language, true),
            secondary: true,
        });
    }
    ModelRc::new(VecModel::from(figures))
}

fn accounts_amounts(
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
                    accounts_tone_code(account_grouping::balance_tone(accounts, *currency)).to_owned()
                }
                .into(),
            })
            .collect::<Vec<AccountAmount>>(),
    ))
}

fn banking_state(state: &AccountsState) -> BankingState {
    let mut banking = BankingState::new(state.language);
    banking.can_remember_pin = true;
    banking.banks = state
        .bank_accesses
        .iter()
        .map(|bank| finglass_core::app::banking::BankItem {
            id: bank.id,
            bank_name: bank.bank_name.clone(),
            blz: bank.blz.clone(),
            user_id: bank.user_id.clone(),
            server_url: bank.server_url.clone(),
            offline: bank.offline,
            include_in_sync_all: bank.include_in_sync_all,
            sync_from: bank.sync_from,
            last_sync: bank.last_sync,
            has_saved_pin: false,
        })
        .collect();
    banking
}

fn bank_suggestion_rows(query: &str) -> ModelRc<BankSuggestionRow> {
    let rows = finglass_core::bank_directory::BankDirectory::bundled()
        .search(query, 15)
        .into_iter()
        .map(|entry| {
            let detail = [entry.blz.as_str(), entry.city.as_str(), entry.bic.as_str()]
                .into_iter()
                .filter(|part| !part.trim().is_empty())
                .collect::<Vec<_>>()
                .join(" · ");
            BankSuggestionRow {
                name: entry.name.clone().into(),
                detail: detail.into(),
                blz: entry.blz.clone().into(),
                bic: entry.bic.clone().into(),
            }
        })
        .collect::<Vec<_>>();
    ModelRc::new(VecModel::from(rows))
}

fn accounts_bank_cards(state: &AccountsState, banking: &BankingState, language: Language) -> Vec<AccountsBankCard> {
    let mut cards = Vec::new();
    for bank in &state.bank_accesses {
        let in_bank: Vec<&AccountItem> = state
            .accounts
            .iter()
            .filter(|account| account.bank_access_id == Some(bank.id))
            .collect();
        let item = banking.banks.iter().find(|item| item.id == bank.id);
        cards.push(accounts_bank_card(
            bank.id.0,
            &bank.bank_name,
            accounts_bank_subtitle(bank, language),
            bank.offline,
            state.collapsed_banks.contains(&bank.id),
            &in_bank,
            state,
            item,
            banking,
            language,
        ));
    }

    let without_bank: Vec<&AccountItem> = state
        .accounts
        .iter()
        .filter(|account| account.bank_access_id.is_none())
        .collect();
    if !without_bank.is_empty() {
        cards.push(accounts_bank_card(
            -1,
            i18n::text(language, Key::AccountsWithoutBank),
            String::new(),
            false,
            false,
            &without_bank,
            state,
            None,
            banking,
            language,
        ));
    }
    cards
}

#[allow(clippy::too_many_arguments)]
fn accounts_bank_card(
    bank_id: i64,
    name: &str,
    subtitle: String,
    offline: bool,
    collapsed: bool,
    in_bank: &[&AccountItem],
    state: &AccountsState,
    item: Option<&finglass_core::app::banking::BankItem>,
    banking: &BankingState,
    language: Language,
) -> AccountsBankCard {
    let grouping_accounts: Vec<GroupingAccount> = in_bank.iter().map(|account| accounts_grouping(account)).collect();
    let totals = account_grouping::grand_total(&grouping_accounts, state.primary_currency);
    let central_blz_hint = match item {
        Some(item) if !item.offline && !item.blz.trim().is_empty() => {
            let mapped = finglass_core::bank_directory::fints_blz(
                finglass_core::bank_directory::BankDirectory::bundled(),
                &item.blz,
            );
            if mapped == item.blz.trim() {
                String::new()
            } else {
                i18n::format(language, Key::CentralBlzHint, &[Arg::Str(mapped)])
            }
        }
        _ => String::new(),
    };
    AccountsBankCard {
        bank_id: i32::try_from(bank_id).unwrap_or(-1),
        name: name.into(),
        subtitle: subtitle.into(),
        central_blz_hint: central_blz_hint.into(),
        offline,
        collapsed,
        account_count_text: i18n::format(language, Key::AccountCount, &[in_bank.len().into()]).into(),
        totals: accounts_amounts(&totals, &grouping_accounts, language, false),
        accounts: ModelRc::new(VecModel::from(
            in_bank
                .iter()
                .map(|account| BankAccountRow {
                    account_id: i32::try_from(account.id.0).unwrap_or(-1),
                    name: accounts_label(account, language).into(),
                    subtitle: accounts_subtitle(account, language).into(),
                    balance_text: format::money(account.balance, language, false).into(),
                    tone: accounts_tone(account).into(),
                    new_count: i32::try_from(account.new_count).unwrap_or(i32::MAX),
                })
                .collect::<Vec<BankAccountRow>>(),
        )),
        syncing: item.is_some_and(|item| banking.syncing_bank == Some(item.id)),
        sync_step: item
            .and_then(|_| banking.sync_step.clone())
            .unwrap_or_else(|| i18n::text(language, Key::Syncing).to_owned())
            .into(),
        sync_enabled: !banking.is_syncing(),
        pin_text: if item.is_none() || offline {
            String::new()
        } else if item.is_some_and(|item| item.has_saved_pin) {
            i18n::text(language, Key::PinSavedForSyncs).to_owned()
        } else if banking.can_remember_pin {
            i18n::text(language, Key::PinAskedEverySync).to_owned()
        } else {
            i18n::text(language, Key::PinNeverStored).to_owned()
        }
        .into(),
        menu: ModelRc::new(VecModel::from(
            item.map(|item| accounts_bank_menu(item, banking, state, language))
                .unwrap_or_default(),
        )),
    }
}

fn accounts_sync_all_text(banking: &BankingState) -> String {
    banking.sync_all.as_ref().map_or_else(String::new, |progress| {
        if progress.position == 0 {
            String::new()
        } else {
            i18n::format(
                banking.language,
                Key::SyncAllProgress,
                &[
                    progress.position.into(),
                    progress.total.into(),
                    Arg::Str(progress.bank_name.clone()),
                ],
            )
        }
    })
}

fn accounts_bank_menu(
    bank: &finglass_core::app::banking::BankItem,
    banking: &BankingState,
    state: &AccountsState,
    language: Language,
) -> Vec<BankMenuItem> {
    let enabled = !banking.is_syncing();
    let mut items = Vec::new();
    if !bank.offline {
        for (action, key) in [("sync", Key::SyncNow), ("history", Key::FetchHistory)] {
            items.push(BankMenuItem {
                action: action.into(),
                label: i18n::text(language, key).into(),
                checked: false,
                danger: false,
                enabled,
            });
        }
        if bank.sync_from.is_some() {
            items.push(BankMenuItem {
                action: "clear-history".into(),
                label: i18n::text(language, Key::ClearHistoryStart).into(),
                checked: false,
                danger: false,
                enabled,
            });
        }
        items.push(BankMenuItem {
            action: "include".into(),
            label: i18n::text(language, Key::IncludeInSyncAll).into(),
            checked: bank.include_in_sync_all,
            danger: false,
            enabled,
        });
        items.push(BankMenuItem {
            action: "connection".into(),
            label: i18n::text(language, Key::ConnectionSettings).into(),
            checked: false,
            danger: false,
            enabled,
        });
        if bank.has_saved_pin {
            items.push(BankMenuItem {
                action: "forget-pin".into(),
                label: i18n::text(language, Key::ForgetPin).into(),
                checked: false,
                danger: false,
                enabled,
            });
        }
    } else {
        items.push(BankMenuItem {
            action: "connection".into(),
            label: i18n::text(language, Key::RenameConnection).into(),
            checked: false,
            danger: false,
            enabled,
        });
    }
    items.push(BankMenuItem {
        action: "delete".into(),
        label: i18n::text(language, Key::RemoveBank).into(),
        checked: false,
        danger: true,
        enabled: enabled && !state.busy.active(),
    });
    items
}

fn accounts_bank_subtitle(bank: &BankAccessItem, language: Language) -> String {
    if bank.offline {
        return i18n::text(language, Key::BankOfflineHint).to_owned();
    }
    let mut parts = vec![i18n::format(language, Key::BlzPrefix, &[Arg::from(bank.blz.as_str())])];
    if let Some(bic) = bank.bic.as_deref().map(str::trim).filter(|bic| !bic.is_empty()) {
        parts.push(i18n::format(language, Key::BicPrefix, &[Arg::from(bic)]));
    }
    parts.join(" · ")
}

fn accounts_panel_rows(state: &AccountsState, language: Language) -> Vec<AccountPanelRow> {
    let visible: std::collections::BTreeSet<AccountId> = state
        .filtered_accounts()
        .into_iter()
        .map(|account| account.id)
        .collect();
    let filtering = !state.account_filter_text.trim().is_empty();
    let all_accounts: Vec<GroupingAccount> = state.accounts.iter().map(accounts_grouping).collect();
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
        selected: state.selection == app::AccountSelection::All,
        collapsed: false,
        new_count: i32::try_from(all_new).unwrap_or(i32::MAX),
        totals: accounts_amounts(&state.grand_total, &all_accounts, language, state.grand_total.len() > 1),
        indent: false,
    }];

    for group in &state.groups {
        let matching: Vec<&GroupingAccount> = group
            .accounts
            .iter()
            .filter(|account| visible.contains(&account.id))
            .collect();
        if filtering && matching.is_empty() {
            continue;
        }
        let collapsed = state.collapsed.contains(&group.key.id());
        let selected = match &state.selection {
            app::AccountSelection::Group(key) => key.id() == group.key.id(),
            _ => false,
        };
        let (group_kind, account_type, id, name) = match &group.key {
            GroupKey::Type(account_type) => ("type", account_type.as_str(), -1, String::new()),
            GroupKey::Custom(group_id) => (
                "custom",
                "",
                i32::try_from(group_id.0).unwrap_or(-1),
                group.name.clone().unwrap_or_default(),
            ),
        };
        let label = group
            .name
            .clone()
            .unwrap_or_else(|| accounts_type_label(account_type, language));
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
            totals: accounts_amounts(&group.totals, &group.accounts, language, false),
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
                accounts_digits(full)
                    .map(|digits| format!("…{digits}"))
                    .unwrap_or_default()
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
                id: i32::try_from(account.id.0).unwrap_or(-1),
                label: accounts_label(full, language).into(),
                secondary: secondary.into(),
                detail: detail.into(),
                name: "".into(),
                delete_text: "".into(),
                selected: state.selection == app::AccountSelection::Account(account.id),
                collapsed: false,
                new_count: i32::try_from(account.new_count).unwrap_or(i32::MAX),
                totals: ModelRc::new(VecModel::from(vec![AccountAmount {
                    text: format::money(account.balance, language, false).into(),
                    tone: accounts_tone_code(account_grouping::balance_tone(
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

fn accounts_shown_text(state: &AccountsState, language: Language) -> String {
    if state.account_filter_text.trim().is_empty() {
        return String::new();
    }
    i18n::format(
        language,
        Key::AccountsShown,
        &[state.filtered_accounts().len().into(), state.accounts.len().into()],
    )
}

fn accounts_detail_title(detail: &AccountDetail, language: Language) -> String {
    let label = accounts_label(&detail.account, language);
    if detail.account.bank_name.is_empty() {
        label
    } else {
        format!("{} · {label}", detail.account.bank_name)
    }
}

fn accounts_balance_path(history: &[(Date, Money)], language: Language) -> (String, String, String) {
    if history.is_empty() {
        return (String::new(), String::new(), String::new());
    }
    let low = history.iter().map(|(_, money)| money.minor).min().unwrap_or_default();
    let high = history.iter().map(|(_, money)| money.minor).max().unwrap_or_default();
    let currency = history[0].1.currency;
    let span = high - low;
    let mut path = String::new();
    for (index, (_, money)) in history.iter().enumerate() {
        let x = if history.len() > 1 {
            index as f32 / (history.len() - 1) as f32 * 700.0
        } else {
            350.0
        };
        let ratio = if span == 0 {
            0.5
        } else {
            (money.minor - low) as f32 / span as f32
        };
        let y = 6.0 + (1.0 - ratio) * 188.0;
        if index == 0 {
            path.push_str(&format!("M {x:.2} {y:.2}"));
        } else {
            path.push_str(&format!(" L {x:.2} {y:.2}"));
        }
    }
    (
        path,
        format::whole_money(Money { minor: high, currency }, language),
        format::whole_money(Money { minor: low, currency }, language),
    )
}

fn accounts_grouping(account: &AccountItem) -> GroupingAccount {
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

fn accounts_label(account: &AccountItem, language: Language) -> String {
    if let Some(name) = account.name.as_deref().map(str::trim).filter(|name| !name.is_empty()) {
        return name.to_owned();
    }
    let account_type = accounts_type_label(&account.account_type, language);
    match accounts_digits(account) {
        Some(digits) => format!("{account_type} …{digits}"),
        None => account_type,
    }
}

fn accounts_digits(account: &AccountItem) -> Option<String> {
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

fn accounts_subtitle(account: &AccountItem, language: Language) -> String {
    let display = if !account.iban.trim().is_empty() {
        account_ids::display(account.iban.trim())
    } else {
        account
            .account_number
            .as_deref()
            .map(str::trim)
            .filter(|number| !number.is_empty())
            .map(str::to_owned)
            .unwrap_or_default()
    };
    let mut subtitle = match (account.name.is_some(), display.is_empty()) {
        (true, false) => format!("{} · {display}", accounts_type_label(&account.account_type, language)),
        (true, true) => accounts_type_label(&account.account_type, language),
        (false, _) => display,
    };
    if let Some(updated) = account.last_update {
        let fetched = i18n::format(language, Key::LastFetched, &[Arg::from(format::date(updated.date()))]);
        if subtitle.is_empty() {
            subtitle = fetched;
        } else {
            subtitle.push_str(" · ");
            subtitle.push_str(&fetched);
        }
    }
    subtitle
}

fn accounts_identifier(account: &AccountItem) -> String {
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

fn accounts_type_label(account_type: &str, language: Language) -> String {
    let key = match account_type {
        "checking" => Some(Key::TypeChecking),
        "savings" => Some(Key::TypeSavings),
        "credit_card" => Some(Key::TypeCreditCard),
        LOAN_TYPE => Some(Key::TypeLoan),
        "depot" => Some(Key::TypeDepot),
        _ => None,
    };
    key.map(|key| i18n::text(language, key).to_owned())
        .unwrap_or_else(|| account_type.to_owned())
}

fn accounts_tone(account: &AccountItem) -> &'static str {
    accounts_tone_code(account_grouping::balance_tone_value(
        account.balance,
        account.account_type == LOAN_TYPE,
    ))
}

fn accounts_tone_code(tone: BalanceTone) -> &'static str {
    match tone {
        BalanceTone::Positive => "positive",
        BalanceTone::Negative => "negative",
        BalanceTone::Loan => "loan",
        BalanceTone::Zero => "zero",
    }
}

fn accounts_days_label(days: i64, language: Language) -> String {
    if days == 365 {
        i18n::text(language, Key::OneYear).to_owned()
    } else {
        i18n::format(language, Key::DaysCount, &[days.into()])
    }
}
