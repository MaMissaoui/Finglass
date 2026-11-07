// Hide the console window on Windows release builds.
#![cfg_attr(all(windows, not(debug_assertions)), windows_subsystem = "windows")]

slint::include_modules!();

fn main() -> Result<(), slint::PlatformError> {
    let window = AppWindow::new()?;
    window.set_app_name(finglass_core::APP_NAME.into());
    window.set_version(finglass_core::VERSION.into());
    window.run()
}
