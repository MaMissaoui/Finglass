//! User interface preferences stored in the encrypted settings table.

use crate::audit::{self, Actor};
use crate::db::{Database, DbError};
use crate::i18n::Language;
use crate::settings::{get_setting, set_setting};

/// The setting key for the theme choice.
pub const THEME_KEY: &str = "ui.theme";
/// The setting key for the language choice.
pub const LANGUAGE_KEY: &str = "ui.language";
/// The setting key for the sidebar's collapsed state.
pub const SIDEBAR_COLLAPSED_KEY: &str = "ui.sidebarCollapsed";

/// The theme choice, independent of the current system colour scheme.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ThemePref {
    /// Follow the operating system.
    System,
    /// Always use the light palette.
    Light,
    /// Always use the dark palette.
    Dark,
}

impl ThemePref {
    /// All choices, in the order the settings screen shows them.
    pub const ALL: &[ThemePref] = &[ThemePref::System, ThemePref::Light, ThemePref::Dark];

    /// The value stored in the settings table.
    #[must_use]
    pub fn code(self) -> &'static str {
        match self {
            ThemePref::System => "system",
            ThemePref::Light => "light",
            ThemePref::Dark => "dark",
        }
    }

    /// Parses a stored value, ignoring case.
    #[must_use]
    pub fn from_code(code: &str) -> Option<Self> {
        if code.eq_ignore_ascii_case("system") {
            Some(ThemePref::System)
        } else if code.eq_ignore_ascii_case("light") {
            Some(ThemePref::Light)
        } else if code.eq_ignore_ascii_case("dark") {
            Some(ThemePref::Dark)
        } else {
            None
        }
    }
}

/// The appearance preferences loaded from a database.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Prefs {
    /// The selected theme.
    pub theme: ThemePref,
    /// The selected language.
    pub language: Language,
    /// Whether the sidebar shows icons only.
    pub sidebar_collapsed: bool,
}

/// Loads the preferences, falling back to the system theme, the given system language and an
/// expanded sidebar.
pub fn load(db: &Database, system_language: Language) -> Result<Prefs, DbError> {
    let theme = get_setting(db, THEME_KEY)?
        .as_deref()
        .and_then(ThemePref::from_code)
        .unwrap_or(ThemePref::System);

    let language = get_setting(db, LANGUAGE_KEY)?
        .as_deref()
        .and_then(Language::from_code)
        .unwrap_or(system_language);

    let sidebar_collapsed = get_setting(db, SIDEBAR_COLLAPSED_KEY)?
        .as_deref()
        .map(|value| value.eq_ignore_ascii_case("true"))
        .unwrap_or(false);

    Ok(Prefs {
        theme,
        language,
        sidebar_collapsed,
    })
}

/// Saves the theme and records the change.
pub fn save_theme(db: &Database, theme: ThemePref) -> Result<(), DbError> {
    set_setting(db, THEME_KEY, theme.code())?;
    audit::log(
        db,
        "UI_THEME_CHANGED",
        Actor::User,
        &format!("Theme set to {}", theme.code()),
    )
}

/// Saves the language and records the change.
pub fn save_language(db: &Database, language: Language) -> Result<(), DbError> {
    set_setting(db, LANGUAGE_KEY, language.code())?;
    audit::log(
        db,
        "UI_LANGUAGE_CHANGED",
        Actor::User,
        &format!("Language set to {}", language.code()),
    )
}

/// Saves whether the sidebar is collapsed and records the change.
pub fn save_sidebar_collapsed(db: &Database, collapsed: bool) -> Result<(), DbError> {
    set_setting(db, SIDEBAR_COLLAPSED_KEY, if collapsed { "true" } else { "false" })?;
    audit::log(
        db,
        "UI_SIDEBAR_CHANGED",
        Actor::User,
        if collapsed {
            "Sidebar collapsed"
        } else {
            "Sidebar expanded"
        },
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::Database;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    #[test]
    fn defaults_when_unset() {
        let (_temp, db) = database();
        let prefs = load(&db, Language::Fr).unwrap();
        assert_eq!(prefs.theme, ThemePref::System);
        assert_eq!(prefs.language, Language::Fr);
        assert!(!prefs.sidebar_collapsed, "the sidebar starts expanded");
    }

    #[test]
    fn preferences_round_trip() {
        let (_temp, db) = database();

        save_theme(&db, ThemePref::Dark).unwrap();
        save_language(&db, Language::De).unwrap();
        save_sidebar_collapsed(&db, true).unwrap();

        let prefs = load(&db, Language::En).unwrap();
        assert_eq!(prefs.theme, ThemePref::Dark);
        assert_eq!(prefs.language, Language::De);
        assert!(prefs.sidebar_collapsed);
    }

    #[test]
    fn sidebar_flag_round_trips() {
        let (_temp, db) = database();

        save_sidebar_collapsed(&db, true).unwrap();
        assert!(load(&db, Language::En).unwrap().sidebar_collapsed);

        save_sidebar_collapsed(&db, false).unwrap();
        assert!(!load(&db, Language::En).unwrap().sidebar_collapsed);
    }

    #[test]
    fn unknown_sidebar_value_falls_back_to_expanded() {
        let (_temp, db) = database();
        set_setting(&db, SIDEBAR_COLLAPSED_KEY, "sometimes").unwrap();

        assert!(!load(&db, Language::En).unwrap().sidebar_collapsed);
    }

    #[test]
    fn every_change_writes_an_audit_entry() {
        let (_temp, db) = database();

        save_theme(&db, ThemePref::Light).unwrap();
        save_language(&db, Language::Fr).unwrap();
        save_sidebar_collapsed(&db, true).unwrap();

        let entries = audit::recent(&db, 10).unwrap();
        assert!(entries.iter().any(|entry| entry.action == "UI_THEME_CHANGED"));
        assert!(entries.iter().any(|entry| entry.action == "UI_LANGUAGE_CHANGED"));
        assert!(entries.iter().any(|entry| entry.action == "UI_SIDEBAR_CHANGED"));
        assert!(entries.iter().all(|entry| entry.actor == Actor::User));
    }

    #[test]
    fn unknown_stored_values_fall_back() {
        let (_temp, db) = database();
        set_setting(&db, THEME_KEY, "sepia").unwrap();
        set_setting(&db, LANGUAGE_KEY, "es").unwrap();

        let prefs = load(&db, Language::En).unwrap();
        assert_eq!(prefs.theme, ThemePref::System);
        assert_eq!(prefs.language, Language::En);
    }

    #[test]
    fn codes_round_trip() {
        for &theme in ThemePref::ALL {
            assert_eq!(ThemePref::from_code(theme.code()), Some(theme));
        }
        assert_eq!(ThemePref::from_code("SYSTEM"), Some(ThemePref::System));
        assert_eq!(ThemePref::from_code("sepia"), None);
    }
}
