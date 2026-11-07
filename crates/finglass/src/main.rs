// Hide the console window on Windows release builds.
#![cfg_attr(all(windows, not(debug_assertions)), windows_subsystem = "windows")]

mod shell;

slint::include_modules!();

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let paths = finglass_core::paths::AppPaths::from_env()?;
    shell::run(paths)?;
    Ok(())
}
