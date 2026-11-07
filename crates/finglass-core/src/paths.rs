//! Where Finglass keeps its data on disk.
//!
//! The root folder is chosen by [`AppPaths::resolve`]. Only [`AppPaths::from_env`] reads the environment, which keeps
//! the rest of the module pure and easy to test.

use std::ffi::OsString;
use std::io;
use std::path::{Path, PathBuf};

use thiserror::Error;

/// Errors when resolving or using the data folder.
#[derive(Debug, Error)]
pub enum PathError {
    /// Neither `FINGLASS_HOME` nor a home folder is available.
    #[error("no home folder is available to place the Finglass data in")]
    NoHome,

    /// The profile slug does not match the allowed pattern.
    #[error("invalid profile slug")]
    InvalidSlug,
}

/// The root of the Finglass data folder, plus helpers for folders inside it.
#[derive(Debug)]
pub struct AppPaths {
    root: PathBuf,
}

impl AppPaths {
    /// Resolves the data root from an optional `FINGLASS_HOME` value and an optional home folder.
    ///
    /// A non-empty `FINGLASS_HOME` wins. Otherwise the root is `<home>/.finglass`.
    pub fn resolve(finglass_home: Option<OsString>, home_dir: Option<PathBuf>) -> Result<Self, PathError> {
        if let Some(home) = finglass_home
            && !home.is_empty()
        {
            return Ok(Self {
                root: PathBuf::from(home),
            });
        }

        match home_dir {
            Some(home) => Ok(Self {
                root: home.join(".finglass"),
            }),
            None => Err(PathError::NoHome),
        }
    }

    /// Resolves the data root from the environment.
    ///
    /// This is the only function in this module that reads the environment.
    pub fn from_env() -> Result<Self, PathError> {
        Self::resolve(std::env::var_os("FINGLASS_HOME"), dirs::home_dir())
    }

    /// The data root folder.
    pub fn root(&self) -> &Path {
        &self.root
    }

    /// The folder for a profile. `"default"` maps to the root itself, other valid slugs to `root/profiles/<slug>`.
    pub fn profile_dir(&self, slug: &str) -> Result<PathBuf, PathError> {
        if slug == "default" {
            return Ok(self.root.clone());
        }

        if !is_valid_slug(slug) {
            return Err(PathError::InvalidSlug);
        }

        Ok(self.root.join("profiles").join(slug))
    }

    /// The database file for a profile.
    pub fn database_file(&self, slug: &str) -> Result<PathBuf, PathError> {
        Ok(self.profile_dir(slug)?.join("finglass.db"))
    }

    /// Creates a folder and its missing parents.
    ///
    /// On Unix a newly created folder gets mode 0700. The mode of an existing folder is left unchanged.
    pub fn ensure_dir(path: &Path) -> io::Result<()> {
        #[cfg(unix)]
        {
            use std::os::unix::fs::DirBuilderExt;

            let mut builder = std::fs::DirBuilder::new();
            builder.recursive(true).mode(0o700);
            builder.create(path)
        }

        #[cfg(not(unix))]
        {
            std::fs::create_dir_all(path)
        }
    }
}

/// A valid slug is 1 to 40 characters: a lowercase letter or digit first, then lowercase letters, digits or hyphens.
fn is_valid_slug(slug: &str) -> bool {
    let mut chars = slug.chars();

    let Some(first) = chars.next() else {
        return false;
    };

    if !(first.is_ascii_lowercase() || first.is_ascii_digit()) {
        return false;
    }

    let mut len = 1;
    for ch in chars {
        if !(ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-') {
            return false;
        }
        len += 1;
        if len > 40 {
            return false;
        }
    }

    true
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsString;

    fn home() -> PathBuf {
        PathBuf::from("/home/alice")
    }

    #[test]
    fn env_wins_over_home() {
        let paths = AppPaths::resolve(Some(OsString::from("/data/finglass")), Some(home())).unwrap();
        assert_eq!(paths.root(), Path::new("/data/finglass"));
    }

    #[test]
    fn empty_env_falls_back_to_home() {
        let paths = AppPaths::resolve(Some(OsString::new()), Some(home())).unwrap();
        assert_eq!(paths.root(), Path::new("/home/alice/.finglass"));
    }

    #[test]
    fn no_env_and_no_home_is_an_error() {
        let err = AppPaths::resolve(None, None).unwrap_err();
        assert!(matches!(err, PathError::NoHome));
        assert_eq!(
            format!("{err}"),
            "no home folder is available to place the Finglass data in"
        );
    }

    #[test]
    fn default_slug_maps_to_the_root() {
        let paths = AppPaths::resolve(None, Some(home())).unwrap();
        assert_eq!(paths.profile_dir("default").unwrap(), home().join(".finglass"));
    }

    #[test]
    fn other_slug_maps_to_profiles_folder() {
        let paths = AppPaths::resolve(None, Some(home())).unwrap();
        assert_eq!(
            paths.profile_dir("business").unwrap(),
            home().join(".finglass").join("profiles").join("business")
        );
        assert_eq!(
            paths.database_file("business").unwrap(),
            home()
                .join(".finglass")
                .join("profiles")
                .join("business")
                .join("finglass.db")
        );
    }

    #[test]
    fn rejects_invalid_slugs() {
        let paths = AppPaths::resolve(None, Some(home())).unwrap();
        let long = "a".repeat(41);

        for slug in ["..", "a/b", "A", "", long.as_str(), "-x"] {
            let err = paths.profile_dir(slug).unwrap_err();
            assert!(
                matches!(err, PathError::InvalidSlug),
                "slug {slug:?} should be rejected"
            );
        }
    }

    #[test]
    fn accepts_a_40_character_slug() {
        let paths = AppPaths::resolve(None, Some(home())).unwrap();
        let slug = "a".repeat(40);
        assert!(paths.profile_dir(&slug).is_ok());
    }

    #[cfg(unix)]
    #[test]
    fn ensure_dir_creates_a_private_folder() {
        use std::os::unix::fs::PermissionsExt;

        let temp = tempfile::tempdir().unwrap();
        let folder = temp.path().join("nested").join("profile");

        AppPaths::ensure_dir(&folder).unwrap();

        let mode = std::fs::metadata(&folder).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o700);
    }

    #[cfg(unix)]
    #[test]
    fn ensure_dir_leaves_an_existing_mode_unchanged() {
        use std::os::unix::fs::PermissionsExt;

        let temp = tempfile::tempdir().unwrap();
        let folder = temp.path().join("existing");
        std::fs::create_dir(&folder).unwrap();
        std::fs::set_permissions(&folder, std::fs::Permissions::from_mode(0o755)).unwrap();

        AppPaths::ensure_dir(&folder).unwrap();

        let mode = std::fs::metadata(&folder).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o755);
    }
}
