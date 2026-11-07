// Hide the console window on Windows release builds.
#![cfg_attr(all(windows, not(debug_assertions)), windows_subsystem = "windows")]

mod accounts;
mod dashboard;
mod documents;
mod reports;
mod settings;
mod shell;

slint::include_modules!();

/// Whether `--version` appears among the command-line arguments.
///
/// Parsed by hand so the version query stays dependency-free and, more importantly, so it can run
/// before Slint or the data folder is touched.
fn version_requested(args: &[impl AsRef<str>]) -> bool {
    args.iter().any(|arg| arg.as_ref() == "--version")
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if version_requested(&args) {
        println!("{} {}", finglass_core::APP_NAME, finglass_core::VERSION);
        return Ok(());
    }

    let paths = finglass_core::paths::AppPaths::from_env()?;
    shell::run(paths)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::version_requested;

    #[test]
    fn version_flag_is_recognized() {
        assert!(version_requested(&["--version"]));
        assert!(version_requested(&["ignored", "--version"]));
        assert!(!version_requested(&["--help"]));
        assert!(!version_requested(&["--version=1"]));

        let no_args: &[&str] = &[];
        assert!(!version_requested(no_args));
    }
}
