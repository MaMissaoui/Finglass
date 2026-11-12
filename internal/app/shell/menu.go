package shell

import (
	"errors"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

// GithubURL is the project's repository, opened by Help > Finglass on GitHub.
const GithubURL = "https://github.com/MaMissaoui/Finglass"

// ReportProblemURL is the repository's issue form, opened by Help > Report a
// problem.
const ReportProblemURL = "https://github.com/MaMissaoui/Finglass/issues/new"

// MinPassphraseLength is the minimum length of a new passphrase, as the unlock
// window's hint shows it.
const MinPassphraseLength = 10

// AuditLimit is how many audit rows the shell reads.
const AuditLimit uint32 = 100

// pageTargets are the View menu's pages, in sidebar order.
var pageTargets = []string{
	"dashboard",
	"accounts",
	"transactions",
	"documents",
	"reports",
	"settings",
}

// PageTarget returns the screen a View menu item opens, by its position in the
// menu.
func PageTarget(index int) (string, bool) {
	if index < 0 || index >= len(pageTargets) {
		return "", false
	}
	return pageTargets[index], true
}

// VersionRequested reports whether --version appears among the command-line
// arguments.
func VersionRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--version" {
			return true
		}
	}
	return false
}

// ProfileLabel returns the unlock window's "Profile: ..." label.
func ProfileLabel(language i18n.Language, appPaths paths.AppPaths, slug *string) string {
	return i18n.Format(language, i18n.UnlockProfile, i18n.Str(ProfileName(appPaths, slug)))
}

// ProfileName returns the profile's display name, falling back to its slug key.
func ProfileName(appPaths paths.AppPaths, slug *string) string {
	listed, err := profiles.List(appPaths)
	if err == nil {
		wanted := startup.SlugKey(slug)
		for _, profile := range listed {
			if profileKey(profile) == wanted {
				return profile.Name
			}
		}
	}
	return startup.SlugKey(slug)
}

// profileKey turns a profile's slug into the session's nil/default form.
func profileKey(profile profiles.Profile) string {
	if profile.Slug == "" {
		return "default"
	}
	return profile.Slug
}

// PassphraseHint returns the unlock window's hint for a new passphrase.
func PassphraseHint(language i18n.Language) string {
	return i18n.Format(language, i18n.PassphraseHint, i18n.Int(MinPassphraseLength))
}

// AboutVersionText returns the About box's version line. The version is passed
// in; cmd/finglass wires bind.Version later.
func AboutVersionText(language i18n.Language, version string) string {
	return i18n.Format(language, i18n.AboutVersion, i18n.Str(version))
}

// UnlockErrorText returns the user-visible text of every unlock error.
func UnlockErrorText(language i18n.Language, err error) string {
	switch {
	case errors.Is(err, unlock.ErrWrongPassphrase):
		return i18n.Text(language, i18n.WrongPassphrase)
	case errors.Is(err, unlock.ErrTooShort):
		return i18n.Format(language, i18n.PassphraseTooShort, i18n.Int(MinPassphraseLength))
	case errors.Is(err, unlock.ErrSpaces):
		return i18n.Text(language, i18n.PassphraseSpaces)
	case errors.Is(err, unlock.ErrMismatch):
		return i18n.Text(language, i18n.PassphraseMismatch)
	case errors.Is(err, unlock.ErrNotAcknowledged):
		return i18n.Text(language, i18n.PassphraseAcknowledge)
	case errors.Is(err, unlock.ErrNotEncrypted):
		return i18n.Text(language, i18n.ErrorNotEncrypted)
	case errors.Is(err, unlock.ErrTooNew):
		return i18n.Text(language, i18n.ErrorDatabaseTooNew)
	default:
		return i18n.Format(
			language,
			i18n.UnlockFailed,
			i18n.Str(i18n.Text(language, i18n.ErrorUnexpected)),
		)
	}
}

// ModeName returns the unlock window's mode value.
func ModeName(mode unlock.UnlockMode) string {
	switch mode {
	case unlock.ModeCreate:
		return "create"
	case unlock.ModeUnlock:
		return "unlock"
	case unlock.ModeChange:
		return "change"
	default:
		return ""
	}
}

// LanguageFromLocale maps a POSIX locale to a supported language, falling back
// to English.
func LanguageFromLocale(locale string) i18n.Language {
	lower := strings.ToLower(locale)
	switch {
	case strings.HasPrefix(lower, "de"):
		return i18n.De
	case strings.HasPrefix(lower, "fr"):
		return i18n.Fr
	default:
		return i18n.En
	}
}

// SystemLanguage returns the system language from the usual POSIX locale
// variables, falling back to English.
//
// getenv is injected so the package never reads the process environment
// itself.
func SystemLanguage(getenv func(string) (string, bool)) i18n.Language {
	locale := ""
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value, ok := getenv(name)
		if ok && value != "" {
			locale = value
			break
		}
	}
	return LanguageFromLocale(locale)
}

// Translate returns the text of a translation key by name, as the Slint
// translation callback does. An unknown key falls back to its own name, so a
// missing key stays obvious.
func Translate(keyName, languageCode string) string {
	language, ok := i18n.LanguageFromCode(languageCode)
	if !ok {
		language = i18n.En
	}
	key, ok := i18n.KeyFromName(keyName)
	if !ok {
		return keyName
	}
	return i18n.Text(language, key)
}

// LeftoverText returns one unlock window leftover line: the kind's translated
// text and the folder, exactly as the Rust window shows it.
func LeftoverText(language i18n.Language, leftover startup.Leftover) string {
	var key i18n.Key
	switch leftover.Kind {
	case startup.LeftoverPreRestore:
		key = i18n.PlaintextLeftoverPreRestore
	case startup.LeftoverDeletedProfile:
		key = i18n.PlaintextLeftoverDeletedProfile
	default:
		key = i18n.PlaintextLeftoverPendingRestore
	}
	return i18n.Text(language, key) + " · " + leftover.Path
}
