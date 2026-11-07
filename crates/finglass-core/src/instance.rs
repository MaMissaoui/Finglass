//! The single-instance lock for a data folder.
//!
//! Two Finglass processes on the same data root are unsafe: startup can apply a staged restore while the other
//! process still has the database open. [`InstanceLock::acquire`] takes an exclusive lock on `<root>/finglass.lock`
//! before startup touches anything, and the lock lives as long as the returned value.

use std::fs::File;
use std::io;

use thiserror::Error;

use crate::paths::AppPaths;

/// The name of the lock file inside the data root.
pub const LOCK_FILE: &str = "finglass.lock";

/// Errors when taking the single-instance lock.
#[derive(Debug, Error)]
pub enum InstanceError {
    /// Another process, or another handle in this process, already holds the lock.
    #[error("another Finglass instance is already using this data folder")]
    AlreadyRunning,

    /// The lock file could not be created or opened.
    #[error(transparent)]
    Io(#[from] io::Error),
}

/// An exclusive lock on a data root.
///
/// The lock is released when the value is dropped. The lock file stays on disk; it holds no data.
#[derive(Debug)]
pub struct InstanceLock {
    _file: File,
}

impl InstanceLock {
    /// Creates the data root if needed, opens `<root>/finglass.lock` and tries to take an exclusive lock.
    ///
    /// The lock covers every profile in the data root, so a second process on the same `FINGLASS_HOME` fails with
    /// [`InstanceError::AlreadyRunning`] even while the first one is only showing the unlock window.
    pub fn acquire(paths: &AppPaths) -> Result<Self, InstanceError> {
        AppPaths::ensure_dir(paths.root())?;

        let file = File::options()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(paths.root().join(LOCK_FILE))?;

        match file.try_lock() {
            Ok(()) => Ok(Self { _file: file }),
            Err(std::fs::TryLockError::WouldBlock) => Err(InstanceError::AlreadyRunning),
            Err(std::fs::TryLockError::Error(error)) => Err(InstanceError::Io(error)),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::*;

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    fn file_names(root: &Path) -> Vec<String> {
        let mut names = std::fs::read_dir(root)
            .unwrap()
            .map(|entry| entry.unwrap().file_name().to_string_lossy().into_owned())
            .collect::<Vec<_>>();
        names.sort();
        names
    }

    #[test]
    fn acquire_creates_the_lock_file_in_a_fresh_root() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let lock = InstanceLock::acquire(&paths).unwrap();

        assert!(paths.root().join(LOCK_FILE).is_file());
        assert_eq!(
            file_names(paths.root()),
            vec![LOCK_FILE.to_owned()],
            "the lock file is the only file created"
        );
        drop(lock);
    }

    #[test]
    fn a_second_acquire_fails_while_the_first_is_alive() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let first = InstanceLock::acquire(&paths).unwrap();

        let error = InstanceLock::acquire(&paths).unwrap_err();

        assert!(
            matches!(error, InstanceError::AlreadyRunning),
            "unexpected error: {error:?}"
        );
        drop(first);
    }

    #[test]
    fn acquiring_works_again_after_the_first_is_dropped() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let first = InstanceLock::acquire(&paths).unwrap();
        drop(first);

        let second = InstanceLock::acquire(&paths).unwrap();

        drop(second);
    }

    #[test]
    fn different_roots_can_be_locked_at_the_same_time() {
        let first_temp = tempfile::tempdir().unwrap();
        let second_temp = tempfile::tempdir().unwrap();

        let first = InstanceLock::acquire(&paths(&first_temp)).unwrap();
        let second = InstanceLock::acquire(&paths(&second_temp)).unwrap();

        drop((first, second));
    }
}
