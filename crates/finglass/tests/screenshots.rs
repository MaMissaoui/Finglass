//! Headless rendering of the app shell with Slint's software renderer.
//!
//! `set_platform` can only be called once per process, so every render lives in this one test.

use std::collections::HashSet;
use std::io::BufWriter;
use std::path::Path;
use std::rc::Rc;

use finglass_core::app;
use finglass_core::app::prefs::ThemePref;
use finglass_core::audit::{self, Actor};
use finglass_core::i18n::{self, Key, Language};
use finglass_core::paths::AppPaths;
use slint::platform::software_renderer::{MinimalSoftwareWindow, RepaintBufferType};
use slint::platform::{Platform, PlatformError, WindowAdapter};
use slint::{ComponentHandle, ModelRc, PhysicalSize, Rgb8Pixel, SharedPixelBuffer, SharedString, VecModel};

slint::include_modules!();

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
    let rows = app::audit::recent(&db, 100).unwrap();

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
        let ui = MainWindow::new().unwrap();
        configure_main(&ui, Language::En, ThemePref::Light, "dashboard", &rows);
        ui.show().unwrap();
        let buffer = render(&window, "main-dashboard-en-light", 1100, 720);
        assert_varied(&buffer, "main-dashboard-en-light");
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
