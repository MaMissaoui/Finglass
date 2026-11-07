//! Resolving external helper programs to absolute paths.
//!
//! OCR runs the `tesseract` and `pdftoppm` command-line programs. An app started from Finder or the Dock gets
//! launchd's minimal `PATH` (`/usr/bin:/bin:/usr/sbin:/sbin`), which does not contain Homebrew's or MacPorts'
//! install folders. Resolving to the absolute path here keeps the helpers reachable from a GUI launch.
//!
//! The result is cached once per process and name: a helper that the user installs while Finglass is running is
//! found only after a restart.

use std::collections::HashMap;
use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, OnceLock};

#[cfg(target_os = "macos")]
const FALLBACK_DIRS: &[&str] = &["/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"];

/// Additional folders searched after `PATH` on macOS, in order.
///
/// Homebrew uses `/opt/homebrew/bin` on Apple silicon and `/usr/local/bin` on Intel; `/opt/local/bin` is
/// MacPorts. Other systems search only `PATH`.
fn search_dirs() -> Vec<PathBuf> {
    let path = std::env::var_os("PATH");
    search_dirs_with(path.as_deref())
}

fn search_dirs_with(path: Option<&OsStr>) -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    if let Some(path) = path {
        // Only absolute folders: an empty or relative entry such as `.` would run a program from the current folder.
        dirs.extend(std::env::split_paths(path).filter(|dir| dir.is_absolute()));
    }

    #[cfg(target_os = "macos")]
    dirs.extend(FALLBACK_DIRS.iter().copied().map(PathBuf::from));

    dirs
}

/// Resolves `name` to an absolute path, searching once per process.
///
/// The result is cached for the life of the process, so installing a helper while Finglass runs takes effect only
/// after a restart.
pub(crate) fn resolve(name: &str) -> Option<PathBuf> {
    type Cache = Mutex<HashMap<String, Option<PathBuf>>>;

    static CACHE: OnceLock<Cache> = OnceLock::new();

    let cache = CACHE.get_or_init(|| Mutex::new(HashMap::new()));
    let mut cache = cache.lock().unwrap_or_else(std::sync::PoisonError::into_inner);

    cache
        .entry(name.to_owned())
        .or_insert_with(|| resolve_in(name, search_dirs()))
        .clone()
}

/// Resolves `name` against `dirs` only, without touching the process environment.
///
/// `name` must be a bare file name. The first candidate that is a regular file with an execute bit wins;
/// symlinks are followed.
fn resolve_in(name: &str, dirs: impl IntoIterator<Item = PathBuf>) -> Option<PathBuf> {
    if name.is_empty() || name.contains('/') || name.contains('\\') {
        return None;
    }

    dirs.into_iter()
        .map(|dir| dir.join(name))
        .find(|candidate| is_executable_file(candidate))
}

fn is_executable_file(path: &Path) -> bool {
    let Ok(metadata) = std::fs::metadata(path) else {
        return false;
    };
    metadata.is_file() && is_executable(&metadata)
}

#[cfg(unix)]
fn is_executable(metadata: &std::fs::Metadata) -> bool {
    use std::os::unix::fs::PermissionsExt;

    metadata.permissions().mode() & 0o111 != 0
}

#[cfg(not(unix))]
fn is_executable(_metadata: &std::fs::Metadata) -> bool {
    true
}

#[cfg(all(test, unix))]
mod tests {
    use std::os::unix::fs::{PermissionsExt, symlink};

    use super::*;

    fn write_file(path: &Path, executable: bool) {
        std::fs::write(path, b"#!/bin/sh\n").unwrap();
        let mode = if executable { 0o755 } else { 0o644 };
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode)).unwrap();
    }

    #[test]
    fn finds_an_executable_regular_file() {
        let temp = tempfile::tempdir().unwrap();
        let program = temp.path().join("tesseract");
        write_file(&program, true);

        assert_eq!(resolve_in("tesseract", vec![temp.path().to_path_buf()]), Some(program));
    }

    #[test]
    fn skips_non_executable_files_and_directories() {
        let temp = tempfile::tempdir().unwrap();
        let program = temp.path().join("tesseract");
        write_file(&program, false);

        assert_eq!(resolve_in("tesseract", vec![temp.path().to_path_buf()]), None);

        std::fs::remove_file(&program).unwrap();
        std::fs::create_dir(&program).unwrap();

        assert_eq!(resolve_in("tesseract", vec![temp.path().to_path_buf()]), None);
    }

    #[test]
    fn follows_a_symlink_to_an_executable() {
        let temp = tempfile::tempdir().unwrap();
        let target = temp.path().join("real-tesseract");
        write_file(&target, true);
        let link = temp.path().join("tesseract");
        symlink(&target, &link).unwrap();

        assert_eq!(resolve_in("tesseract", vec![temp.path().to_path_buf()]), Some(link));
    }

    #[test]
    fn the_first_folder_wins() {
        let temp = tempfile::tempdir().unwrap();
        let first = temp.path().join("first");
        let second = temp.path().join("second");
        std::fs::create_dir(&first).unwrap();
        std::fs::create_dir(&second).unwrap();
        write_file(&first.join("tesseract"), true);
        write_file(&second.join("tesseract"), true);

        assert_eq!(
            resolve_in("tesseract", vec![first.clone(), second]),
            Some(first.join("tesseract"))
        );
    }

    #[test]
    fn rejects_names_with_a_separator_and_missing_programs() {
        let temp = tempfile::tempdir().unwrap();
        write_file(&temp.path().join("tesseract"), true);

        assert_eq!(
            resolve_in("nested/tesseract", vec![temp.path().to_path_buf()]),
            None,
            "a name with a separator is not a bare file name"
        );
        assert_eq!(resolve_in("missing", vec![temp.path().to_path_buf()]), None);
        assert_eq!(resolve_in("missing", Vec::new()), None);
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn macos_fallbacks_come_after_path() {
        let dirs = search_dirs_with(Some(OsStr::new("/custom/bin:/usr/bin")));

        assert_eq!(
            dirs,
            [
                PathBuf::from("/custom/bin"),
                PathBuf::from("/usr/bin"),
                PathBuf::from("/opt/homebrew/bin"),
                PathBuf::from("/usr/local/bin"),
                PathBuf::from("/opt/local/bin"),
            ]
        );
    }

    #[test]
    fn relative_path_entries_are_ignored() {
        let dirs = search_dirs_with(Some(OsStr::new(".:relative/bin::/usr/bin")));
        assert_eq!(dirs.first(), Some(&PathBuf::from("/usr/bin")));
        assert!(dirs.iter().all(|dir| dir.is_absolute()));
    }
}
