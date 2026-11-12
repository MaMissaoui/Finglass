// Package prefs stores the user interface preferences in the encrypted
// settings table. It ports crates/finglass-core/src/app/prefs.rs.
package prefs

import (
	"context"
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// ThemeKey is the setting key for the theme choice.
const ThemeKey = "ui.theme"

// LanguageKey is the setting key for the language choice.
const LanguageKey = "ui.language"

// SidebarCollapsedKey is the setting key for the sidebar's collapsed state.
const SidebarCollapsedKey = "ui.sidebarCollapsed"

// ThemePref is the theme choice, independent of the current system colour
// scheme.
type ThemePref int

const (
	// ThemeSystem follows the operating system.
	ThemeSystem ThemePref = iota
	// ThemeLight always uses the light palette.
	ThemeLight
	// ThemeDark always uses the dark palette.
	ThemeDark
)

// AllThemes lists every choice, in the order the settings screen shows them.
var AllThemes = []ThemePref{ThemeSystem, ThemeLight, ThemeDark}

// Code returns the value stored in the settings table.
func (t ThemePref) Code() string {
	switch t {
	case ThemeSystem:
		return "system"
	case ThemeLight:
		return "light"
	case ThemeDark:
		return "dark"
	default:
		return ""
	}
}

// ThemeFromCode parses a stored value, ignoring ASCII case.
func ThemeFromCode(code string) (ThemePref, bool) {
	switch {
	case equalASCIIFold(code, "system"):
		return ThemeSystem, true
	case equalASCIIFold(code, "light"):
		return ThemeLight, true
	case equalASCIIFold(code, "dark"):
		return ThemeDark, true
	default:
		return ThemeSystem, false
	}
}

// Prefs is the appearance preferences loaded from a database.
type Prefs struct {
	// Theme is the selected theme.
	Theme ThemePref
	// Language is the selected language.
	Language i18n.Language
	// SidebarCollapsed says whether the sidebar shows icons only.
	SidebarCollapsed bool
}

// Load loads the preferences, falling back to the system theme, the given
// system language and an expanded sidebar.
func Load(ctx context.Context, d *db.Database, systemLanguage i18n.Language) (Prefs, error) {
	theme, ok, err := settings.Get(ctx, d, ThemeKey)
	if err != nil {
		return Prefs{}, err
	}
	selectedTheme := ThemeSystem
	if ok {
		if parsed, valid := ThemeFromCode(theme); valid {
			selectedTheme = parsed
		}
	}

	language, ok, err := settings.Get(ctx, d, LanguageKey)
	if err != nil {
		return Prefs{}, err
	}
	selectedLanguage := systemLanguage
	if ok {
		if parsed, valid := i18n.LanguageFromCode(language); valid {
			selectedLanguage = parsed
		}
	}

	sidebar, ok, err := settings.Get(ctx, d, SidebarCollapsedKey)
	if err != nil {
		return Prefs{}, err
	}

	return Prefs{
		Theme:            selectedTheme,
		Language:         selectedLanguage,
		SidebarCollapsed: ok && equalASCIIFold(sidebar, "true"),
	}, nil
}

// SaveTheme saves the theme and records the change.
func SaveTheme(ctx context.Context, d *db.Database, theme ThemePref) error {
	if err := settings.Set(ctx, d, ThemeKey, theme.Code()); err != nil {
		return err
	}
	return audit.Log(
		ctx,
		d,
		"UI_THEME_CHANGED",
		audit.ActorUser,
		fmt.Sprintf("Theme set to %s", theme.Code()),
	)
}

// SaveLanguage saves the language and records the change.
func SaveLanguage(ctx context.Context, d *db.Database, language i18n.Language) error {
	if err := settings.Set(ctx, d, LanguageKey, language.Code()); err != nil {
		return err
	}
	return audit.Log(
		ctx,
		d,
		"UI_LANGUAGE_CHANGED",
		audit.ActorUser,
		fmt.Sprintf("Language set to %s", language.Code()),
	)
}

// SaveSidebarCollapsed saves whether the sidebar is collapsed and records the
// change.
func SaveSidebarCollapsed(ctx context.Context, d *db.Database, collapsed bool) error {
	value := "false"
	details := "Sidebar expanded"
	if collapsed {
		value = "true"
		details = "Sidebar collapsed"
	}
	if err := settings.Set(ctx, d, SidebarCollapsedKey, value); err != nil {
		return err
	}
	return audit.Log(ctx, d, "UI_SIDEBAR_CHANGED", audit.ActorUser, details)
}

// equalASCIIFold reports whether a and b are equal, folding ASCII letters only.
func equalASCIIFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
