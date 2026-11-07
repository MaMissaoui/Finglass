//! Separate data sets, as FinDesk's `ProfileStore` and `ProfileSwitch` path logic.
//!
//! The default profile is the data root itself, so data from before profiles existed stays where it is. Other
//! profiles live in `profiles/<slug>/`. The registry is `profiles.properties` in the root and is written atomically.

use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use thiserror::Error;
use time::OffsetDateTime;
use time::macros::format_description;

use crate::paths::{AppPaths, PathError};

/// The name shown for the default profile until the user renames it.
pub const DEFAULT_NAME: &str = "Personal";

/// The longest allowed profile name.
pub const MAX_NAME_LENGTH: usize = 40;

/// The folder that holds the non-default profiles.
pub const PROFILES_DIR: &str = "profiles";

/// A deleted profile is moved here instead of erased, so it can be recovered by hand.
pub const DELETED_DIR: &str = "profiles-deleted";

const FILE: &str = "profiles.properties";
const KEY_ACTIVE: &str = "active";
const NAME_PREFIX: &str = "name.";
const DEFAULT_KEY: &str = "default";

static CHANGES: AtomicU64 = AtomicU64::new(0);

/// One profile: a name and its data folder.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Profile {
    /// The slug, or `None` for the default profile.
    pub slug: Option<String>,
    /// The display name.
    pub name: String,
    /// The profile's data folder.
    pub dir: PathBuf,
}

impl Profile {
    /// Whether this is the default profile.
    pub fn is_default(&self) -> bool {
        self.slug.is_none()
    }
}

/// Everything that can go wrong while managing profiles.
#[derive(Debug, Error)]
pub enum ProfileError {
    /// The name is empty after trimming.
    #[error("name is required")]
    EmptyName,
    /// The name is longer than [`MAX_NAME_LENGTH`].
    #[error("name is too long (max. 40 characters)")]
    NameTooLong,
    /// No profile with this slug is registered.
    #[error("unknown profile: {0}")]
    Unknown(String),
    /// The profile is currently open and cannot be deleted.
    #[error("the open profile can't be deleted")]
    OpenProfile,
    /// The default profile cannot be deleted.
    #[error("the default profile can't be deleted")]
    DefaultProfile,
    /// The slug is not valid.
    #[error(transparent)]
    Path(#[from] PathError),
    /// Reading or writing the registry failed.
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// A profile result.
pub type Result<T> = std::result::Result<T, ProfileError>;

/// Increments after every create, rename and delete, so views with their own call notice the change.
pub fn changes() -> u64 {
    CHANGES.load(Ordering::Relaxed)
}

fn bump_changes() {
    CHANGES.fetch_add(1, Ordering::Relaxed);
}

/// The folder of a non-default profile.
///
/// `"default"` is rejected here, as in FinDesk's `ProfileStore.profileDir`; use [`AppPaths::profile_dir`] when the
/// default profile's root is wanted.
pub fn profile_dir(paths: &AppPaths, slug: &str) -> Result<PathBuf> {
    if slug == DEFAULT_KEY {
        return Err(ProfileError::DefaultProfile);
    }

    Ok(paths.profile_dir(slug)?)
}

/// The data folder for a profile slug; `None` is the default profile.
pub fn data_dir(paths: &AppPaths, slug: Option<&str>) -> Result<PathBuf> {
    match slug {
        None => Ok(paths.root().to_path_buf()),
        Some(slug) => profile_dir(paths, slug),
    }
}

/// The default profile first, then the others ordered by name and slug.
pub fn list(paths: &AppPaths) -> Result<Vec<Profile>> {
    let properties = load(paths)?;
    Ok(build_list(&properties, paths))
}

/// The profile to open, or the default one when the stored profile's folder is gone.
pub fn active(paths: &AppPaths) -> Result<Profile> {
    let properties = load(paths)?;
    let slug = properties
        .get(KEY_ACTIVE)
        .map(String::as_str)
        .filter(|slug| !slug.is_empty());
    let fallback = default_profile(&properties, paths);
    let chosen = build_list(&properties, paths)
        .into_iter()
        .find(|profile| profile.slug.as_deref() == slug);
    Ok(chosen.unwrap_or(fallback))
}

/// Creates a profile and its data folder, returning it.
pub fn create(paths: &AppPaths, name: &str) -> Result<Profile> {
    let clean = valid_name(name)?;
    let mut properties = load(paths)?;
    let base = base_slug(&clean);
    let taken: BTreeSet<String> = properties
        .keys()
        .filter_map(|key| key.strip_prefix(NAME_PREFIX).map(str::to_owned))
        .collect();
    let slug = unique_slug(paths, &base, &taken)?;

    let dir = paths.profile_dir(&slug)?;
    AppPaths::ensure_dir(&dir)?;

    properties.insert(format!("{NAME_PREFIX}{slug}"), clean.clone());
    save(paths, &properties)?;
    bump_changes();

    Ok(Profile {
        slug: Some(slug),
        name: clean,
        dir,
    })
}

/// Renames the default profile (`None`) or a stored profile.
pub fn rename(paths: &AppPaths, slug: Option<&str>, name: &str) -> Result<()> {
    let clean = valid_name(name)?;
    let mut properties = load(paths)?;
    let key = format!("{NAME_PREFIX}{}", slug.unwrap_or(DEFAULT_KEY));

    if let Some(slug) = slug
        && !properties.contains_key(&key)
    {
        return Err(ProfileError::Unknown(slug.to_owned()));
    }

    properties.insert(key, clean);
    save(paths, &properties)?;
    bump_changes();
    Ok(())
}

/// Remembers the profile to open. It takes effect at the next start.
pub fn set_active(paths: &AppPaths, slug: Option<&str>) -> Result<()> {
    let mut properties = load(paths)?;

    if let Some(slug) = slug {
        let known = build_list(&properties, paths)
            .iter()
            .any(|profile| profile.slug.as_deref() == Some(slug));
        if !known {
            return Err(ProfileError::Unknown(slug.to_owned()));
        }
    }

    properties.insert(KEY_ACTIVE.to_owned(), slug.unwrap_or("").to_owned());
    save(paths, &properties)?;
    Ok(())
}

/// Moves a profile's folder to `profiles-deleted/` (nothing is erased) and returns where it went.
///
/// The default profile and the one currently open cannot be deleted.
pub fn delete(paths: &AppPaths, slug: &str, open_slug: Option<&str>) -> Result<PathBuf> {
    let mut properties = load(paths)?;
    let key = format!("{NAME_PREFIX}{slug}");

    if open_slug == Some(slug) {
        return Err(ProfileError::OpenProfile);
    }
    if slug == DEFAULT_KEY {
        return Err(ProfileError::DefaultProfile);
    }
    if !properties.contains_key(&key) {
        return Err(ProfileError::Unknown(slug.to_owned()));
    }

    let source = profile_dir(paths, slug)?;
    let deleted_root = paths.root().join(DELETED_DIR);
    let target = deleted_root.join(format!("{slug}-{}", timestamp()));

    if source.exists() {
        AppPaths::ensure_dir(&deleted_root)?;
        fs::rename(&source, &target)?;
    }

    properties.remove(&key);
    if properties.get(KEY_ACTIVE).map(String::as_str) == Some(slug) {
        properties.insert(KEY_ACTIVE.to_owned(), String::new());
    }
    save(paths, &properties)?;
    bump_changes();

    Ok(target)
}

fn default_profile(properties: &BTreeMap<String, String>, paths: &AppPaths) -> Profile {
    let name = properties
        .get(&format!("{NAME_PREFIX}{DEFAULT_KEY}"))
        .cloned()
        .unwrap_or_else(|| DEFAULT_NAME.to_owned());

    Profile {
        slug: None,
        name,
        dir: paths.root().to_path_buf(),
    }
}

fn build_list(properties: &BTreeMap<String, String>, paths: &AppPaths) -> Vec<Profile> {
    let mut others: Vec<Profile> = properties
        .iter()
        .filter_map(|(key, name)| {
            let slug = key.strip_prefix(NAME_PREFIX)?;
            if slug.is_empty() || slug == DEFAULT_KEY {
                return None;
            }

            let dir = paths.profile_dir(slug).ok()?;
            if !dir.is_dir() {
                return None;
            }

            Some(Profile {
                slug: Some(slug.to_owned()),
                name: name.clone(),
                dir,
            })
        })
        .collect();

    others.sort_by(|left, right| {
        left.name
            .to_lowercase()
            .cmp(&right.name.to_lowercase())
            .then_with(|| left.slug.cmp(&right.slug))
    });

    let mut profiles = vec![default_profile(properties, paths)];
    profiles.extend(others);
    profiles
}

fn valid_name(name: &str) -> Result<String> {
    let clean = name.trim();
    if clean.is_empty() {
        return Err(ProfileError::EmptyName);
    }
    if clean.chars().count() > MAX_NAME_LENGTH {
        return Err(ProfileError::NameTooLong);
    }
    Ok(clean.to_owned())
}

/// Turns a display name into FinDesk's slug: lower case, umlauts expanded, runs of other characters collapsed.
fn base_slug(name: &str) -> String {
    let lowered = name
        .to_lowercase()
        .replace('ä', "a")
        .replace('ö', "o")
        .replace('ü', "u")
        .replace('ß', "ss");

    let mut base = String::with_capacity(lowered.len());
    for character in lowered.chars() {
        if character.is_ascii_lowercase() || character.is_ascii_digit() {
            base.push(character);
        } else if !base.ends_with('-') {
            base.push('-');
        }
    }

    let base = base.trim_matches('-');
    let base = if base.is_empty() { "profile" } else { base };
    let truncated: String = base.chars().take(MAX_NAME_LENGTH).collect();
    truncated.trim_matches('-').to_owned()
}

fn unique_slug(paths: &AppPaths, base: &str, taken: &BTreeSet<String>) -> Result<String> {
    let mut suffix = 0_u64;

    loop {
        suffix += 1;
        let candidate = if suffix == 1 {
            base.to_owned()
        } else {
            with_suffix(base, suffix)
        };

        if taken.contains(&candidate) || candidate == DEFAULT_KEY {
            continue;
        }

        match paths.profile_dir(&candidate) {
            Ok(dir) if !dir.exists() => return Ok(candidate),
            Ok(_) => continue,
            Err(_) => continue,
        }
    }
}

/// Appends `-N` while keeping the slug inside [`MAX_NAME_LENGTH`] and valid.
fn with_suffix(base: &str, suffix: u64) -> String {
    let suffix = format!("-{suffix}");
    let room = MAX_NAME_LENGTH.saturating_sub(suffix.len());
    let stem = base.chars().take(room).collect::<String>();
    let stem = stem.trim_end_matches('-');
    format!("{stem}{suffix}")
}

fn timestamp() -> String {
    const STAMP: &[time::format_description::FormatItem<'static>] =
        format_description!("[year][month][day]-[hour][minute][second]");
    OffsetDateTime::now_utc()
        .format(STAMP)
        .unwrap_or_else(|_| "unknown".to_owned())
}

fn registry_path(paths: &AppPaths) -> PathBuf {
    paths.root().join(FILE)
}

fn load(paths: &AppPaths) -> Result<BTreeMap<String, String>> {
    let path = registry_path(paths);
    let text = match fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(BTreeMap::new()),
        Err(error) => return Err(error.into()),
    };

    Ok(parse_properties(&text))
}

fn save(paths: &AppPaths, properties: &BTreeMap<String, String>) -> Result<()> {
    AppPaths::ensure_dir(paths.root())?;
    let target = registry_path(paths);
    let temp = temp_path(paths.root());

    let write = || -> std::io::Result<()> {
        let mut options = OpenOptions::new();
        options.write(true).create_new(true);

        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }

        let mut file = options.open(&temp)?;
        writeln!(file, "# Finglass profiles")?;
        for (key, value) in properties {
            writeln!(file, "{}={}", escape(key), escape(value))?;
        }
        file.flush()?;
        file.sync_all()?;
        Ok(())
    };

    if let Err(error) = write() {
        let _ = fs::remove_file(&temp);
        return Err(error.into());
    }

    #[cfg(windows)]
    if target.exists() {
        fs::remove_file(&target)?;
    }

    if let Err(error) = fs::rename(&temp, &target) {
        let _ = fs::remove_file(&temp);
        return Err(error.into());
    }

    Ok(())
}

fn temp_path(root: &Path) -> PathBuf {
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos())
        .unwrap_or(0);
    let counter = COUNTER.fetch_add(1, Ordering::Relaxed);
    root.join(format!("profiles.{}-{nanos}-{counter}.tmp", std::process::id()))
}

pub(crate) fn parse_properties(text: &str) -> BTreeMap<String, String> {
    let mut properties = BTreeMap::new();

    for line in text.lines() {
        let line = line.trim_start();
        if line.is_empty() || line.starts_with('#') || line.starts_with('!') {
            continue;
        }

        let split = separator_index(line);
        let (key, value) = match split {
            Some(index) => {
                let (key, rest) = line.split_at(index);
                let rest = rest.trim_start();
                let value = rest
                    .strip_prefix('=')
                    .or_else(|| rest.strip_prefix(':'))
                    .unwrap_or(rest)
                    .trim_start();
                (key.trim_end(), value)
            }
            None => (line.trim_end(), ""),
        };

        properties.insert(unescape(key), unescape(value));
    }

    properties
}

/// Finds the first unescaped `=`, `:` or whitespace that separates a key from its value.
fn separator_index(line: &str) -> Option<usize> {
    let mut escaped = false;

    for (index, character) in line.char_indices() {
        if escaped {
            escaped = false;
            continue;
        }
        if character == '\\' {
            escaped = true;
            continue;
        }
        if character == '=' || character == ':' || character.is_whitespace() {
            return Some(index);
        }
    }

    None
}

fn escape(text: &str) -> String {
    let mut escaped = String::with_capacity(text.len());

    for character in text.chars() {
        match character {
            '\\' => escaped.push_str("\\\\"),
            '\n' => escaped.push_str("\\n"),
            '\r' => escaped.push_str("\\r"),
            '\t' => escaped.push_str("\\t"),
            '\u{0C}' => escaped.push_str("\\f"),
            '=' => escaped.push_str("\\="),
            ':' => escaped.push_str("\\:"),
            _ => escaped.push(character),
        }
    }

    escaped
}

fn unescape(text: &str) -> String {
    let mut result = String::with_capacity(text.len());
    let mut characters = text.chars();

    while let Some(character) = characters.next() {
        if character != '\\' {
            result.push(character);
            continue;
        }

        match characters.next() {
            Some('t') => result.push('\t'),
            Some('n') => result.push('\n'),
            Some('r') => result.push('\r'),
            Some('f') => result.push('\u{0C}'),
            Some('u') => {
                let digits: String = characters.by_ref().take(4).collect();
                if digits.len() == 4
                    && let Ok(code) = u32::from_str_radix(&digits, 16)
                    && let Some(character) = char::from_u32(code)
                {
                    result.push(character);
                }
            }
            Some(other) => result.push(other),
            None => result.push('\\'),
        }
    }

    result
}

#[cfg(test)]
mod tests {
    use std::sync::Mutex;

    use super::*;

    static TEST_LOCK: Mutex<()> = Mutex::new(());

    fn lock() -> std::sync::MutexGuard<'static, ()> {
        TEST_LOCK.lock().unwrap_or_else(|error| error.into_inner())
    }

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    #[test]
    fn default_profile_is_the_root_and_others_get_unique_slugs() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        assert_eq!(
            list(&paths).unwrap(),
            vec![Profile {
                slug: None,
                name: DEFAULT_NAME.to_owned(),
                dir: temp.path().to_path_buf(),
            }]
        );

        let business = create(&paths, "  Business GmbH ").unwrap();
        let second = create(&paths, "business gmbh").unwrap();
        let umlaut = create(&paths, "Müller & Söhne").unwrap();

        assert_eq!(business.slug.as_deref(), Some("business-gmbh"));
        assert_eq!(business.name, "Business GmbH");
        assert_eq!(second.slug.as_deref(), Some("business-gmbh-2"));
        assert_eq!(umlaut.slug.as_deref(), Some("muller-sohne"));
        assert!(business.dir.is_dir());
        assert_eq!(business.dir, temp.path().join("profiles/business-gmbh"));
        assert_eq!(
            list(&paths)
                .unwrap()
                .into_iter()
                .map(|profile| profile.slug)
                .collect::<Vec<_>>(),
            vec![
                None,
                Some("business-gmbh".to_owned()),
                Some("business-gmbh-2".to_owned()),
                Some("muller-sohne".to_owned())
            ],
            "default first, then by name"
        );
    }

    #[test]
    fn names_are_validated_and_slugs_cannot_escape_the_profiles_folder() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        assert!(matches!(create(&paths, "   ").unwrap_err(), ProfileError::EmptyName));
        assert!(matches!(
            create(&paths, &"x".repeat(41)).unwrap_err(),
            ProfileError::NameTooLong
        ));
        assert_eq!(
            create(&paths, "../../etc/passwd").unwrap().slug.as_deref(),
            Some("etc-passwd")
        );
        assert!(profile_dir(&paths, "../escape").is_err());
        assert!(profile_dir(&paths, "a/b").is_err());
        assert!(profile_dir(&paths, "default").is_err());
        assert!(matches!(
            set_active(&paths, Some("../escape")).unwrap_err(),
            ProfileError::Unknown(_)
        ));
    }

    #[test]
    fn the_active_profile_is_remembered_and_falls_back_when_its_folder_is_gone() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let business = create(&paths, "Business").unwrap();
        let slug = business.slug.clone().unwrap();
        set_active(&paths, Some(&slug)).unwrap();
        assert_eq!(
            active(&paths).unwrap().slug.as_deref(),
            Some(slug.as_str()),
            "persisted across calls"
        );

        rename(&paths, None, "Privat").unwrap();
        rename(&paths, Some(&slug), "Firma").unwrap();
        assert_eq!(
            list(&paths)
                .unwrap()
                .into_iter()
                .map(|profile| profile.name)
                .collect::<Vec<_>>(),
            vec!["Privat".to_owned(), "Firma".to_owned()]
        );
        assert_eq!(
            active(&paths).unwrap().slug.as_deref(),
            Some(slug.as_str()),
            "rename keeps the choice"
        );

        fs::remove_dir_all(&business.dir).unwrap();
        assert_eq!(active(&paths).unwrap().slug, None);
    }

    #[test]
    fn create_rename_and_delete_increment_the_process_wide_change_counter() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let before = changes();

        let business = create(&paths, "Business").unwrap();
        assert_eq!(changes(), before + 1);

        let slug = business.slug.clone().unwrap();
        rename(&paths, Some(&slug), "Firma").unwrap();
        assert_eq!(changes(), before + 2, "a rename through another call is visible too");

        delete(&paths, &slug, None).unwrap();
        assert_eq!(changes(), before + 3);
    }

    #[test]
    fn delete_moves_the_folder_aside_and_refuses_the_default_and_the_open_profile() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let business = create(&paths, "Business").unwrap();
        let slug = business.slug.clone().unwrap();
        fs::write(business.dir.join("finglass.db"), "data").unwrap();
        set_active(&paths, Some(&slug)).unwrap();

        assert!(matches!(
            delete(&paths, &slug, Some(&slug)).unwrap_err(),
            ProfileError::OpenProfile
        ));
        assert!(matches!(
            delete(&paths, "default", None).unwrap_err(),
            ProfileError::DefaultProfile
        ));

        let moved = delete(&paths, &slug, None).unwrap();

        assert!(!business.dir.exists());
        assert_eq!(fs::read_to_string(moved.join("finglass.db")).unwrap(), "data");
        assert_eq!(moved.parent(), Some(temp.path().join(DELETED_DIR).as_path()));
        assert_eq!(
            list(&paths)
                .unwrap()
                .into_iter()
                .map(|profile| profile.slug)
                .collect::<Vec<_>>(),
            vec![None]
        );
        assert_eq!(
            active(&paths).unwrap().slug,
            None,
            "a deleted next-start choice falls back to the default"
        );
    }

    #[test]
    fn profile_switch_data_dir_is_the_root_for_the_default_profile() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let business = create(&paths, "Business").unwrap();

        assert_eq!(data_dir(&paths, None).unwrap(), temp.path());
        assert_eq!(data_dir(&paths, business.slug.as_deref()).unwrap(), business.dir);
    }

    #[cfg(unix)]
    #[test]
    fn the_registry_file_is_private() {
        use std::os::unix::fs::PermissionsExt;

        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        create(&paths, "Business").unwrap();

        let mode = fs::metadata(registry_path(&paths)).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    #[test]
    fn registry_round_trips_names_with_separators_and_umlauts() {
        let _guard = lock();
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);

        let profile = create(&paths, "Müller = Söhne: #1").unwrap();
        let properties = load(&paths).unwrap();

        assert_eq!(
            properties.get(&format!("name.{}", profile.slug.as_deref().unwrap())),
            Some(&"Müller = Söhne: #1".to_owned())
        );
    }

    #[test]
    fn parse_properties_accepts_java_style_escapes() {
        let parsed = parse_properties(
            "# comment\nactive=business\nname.default=Privat\nname.business=M\\u00fcller\\: S\\u00f6hne\n",
        );

        assert_eq!(parsed.get("active").map(String::as_str), Some("business"));
        assert_eq!(parsed.get("name.business").map(String::as_str), Some("Müller: Söhne"));
    }
}
