package shell

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

func TestPageItemsFollowTheSidebarOrder(t *testing.T) {
	want := []string{"dashboard", "accounts", "transactions", "documents", "reports", "settings"}
	for index, screen := range want {
		got, ok := PageTarget(index)
		if !ok || got != screen {
			t.Fatalf("PageTarget(%d) = %q, %v; want %q", index, got, ok, screen)
		}
	}
}

func TestPageItemsOutsideTheMenuHaveNoTarget(t *testing.T) {
	for _, index := range []int{-1, 6, int(^uint(0) >> 1)} {
		if got, ok := PageTarget(index); ok {
			t.Fatalf("PageTarget(%d) = %q, true; want no target", index, got)
		}
	}
}

func TestHelpLinksPointAtTheProjectRepository(t *testing.T) {
	if GithubURL != "https://github.com/MaMissaoui/Finglass" {
		t.Fatalf("GithubURL = %q", GithubURL)
	}
	if ReportProblemURL != "https://github.com/MaMissaoui/Finglass/issues/new" {
		t.Fatalf("ReportProblemURL = %q", ReportProblemURL)
	}
}

func TestVersionFlagIsRecognized(t *testing.T) {
	if !VersionRequested([]string{"--version"}) {
		t.Fatal("--version was not recognized")
	}
	if !VersionRequested([]string{"ignored", "--version"}) {
		t.Fatal("--version among other arguments was not recognized")
	}
	if VersionRequested([]string{"--help"}) {
		t.Fatal("--help is not --version")
	}
	if VersionRequested([]string{"--version=1"}) {
		t.Fatal("--version=1 is not --version")
	}
	if VersionRequested(nil) {
		t.Fatal("no arguments requested the version")
	}
}

func TestFindIsEnabledOnlyOnPagesWithASearchField(t *testing.T) {
	for _, screen := range []string{"transactions", "documents"} {
		if !FindEnabled(screen, false) {
			t.Fatalf("%s has a search field", screen)
		}
	}
	for _, screen := range []string{"dashboard", "accounts", "reports", "settings"} {
		if FindEnabled(screen, false) {
			t.Fatalf("%s has no page search field", screen)
		}
	}
	for _, screen := range []string{"transactions", "documents"} {
		if FindEnabled(screen, true) {
			t.Fatalf("an open dialog disables Find on %s", screen)
		}
	}
}

func TestTheFindMenuItemHasTextInEveryLanguage(t *testing.T) {
	for _, language := range i18n.Languages {
		if i18n.Text(language, i18n.MenuFind) == "" {
			t.Fatalf("empty Find text in %s", language.Code())
		}
	}
}

func TestSettingsLinksOpenTheirSectionAndPlainNavigationOpensAppearance(t *testing.T) {
	appearance := SettingsSectionFor("settings", nil)
	if appearance == nil || *appearance != SettingsAppearance {
		t.Fatalf("plain navigation section = %v, want Appearance", appearance)
	}

	categories := SettingsSectionFor("settings", intPointer(SettingsCategories))
	if categories == nil || *categories != SettingsCategories {
		t.Fatalf("categories link section = %v", categories)
	}
	profiles := SettingsSectionFor("settings", intPointer(SettingsProfiles))
	if profiles == nil || *profiles != SettingsProfiles {
		t.Fatalf("profiles link section = %v", profiles)
	}

	if SettingsSectionFor("dashboard", nil) != nil {
		t.Fatal("dashboard has no settings section")
	}
	if SettingsSectionFor("dashboard", intPointer(SettingsCategories)) != nil {
		t.Fatal("a non-settings screen never opens a section")
	}
}

func TestEveryUnlockErrorHasTextInEveryLanguage(t *testing.T) {
	errors := []error{
		unlock.ErrWrongPassphrase,
		unlock.ErrTooShort,
		unlock.ErrSpaces,
		unlock.ErrMismatch,
		unlock.ErrNotAcknowledged,
		unlock.ErrNotEncrypted,
		unlock.ErrTooNew,
		unlock.ErrFailed,
	}
	for _, language := range i18n.Languages {
		for _, err := range errors {
			if UnlockErrorText(language, err) == "" {
				t.Fatalf("empty unlock error text in %s for %v", language.Code(), err)
			}
		}
	}
}

func TestLocaleDetectionRecognizesSupportedLanguages(t *testing.T) {
	cases := map[string]i18n.Language{
		"de_DE.UTF-8": i18n.De,
		"fr-FR":       i18n.Fr,
		"en_US":       i18n.En,
		"es_ES":       i18n.En,
		"":            i18n.En,
	}
	for locale, want := range cases {
		if got := LanguageFromLocale(locale); got != want {
			t.Fatalf("LanguageFromLocale(%q) = %s, want %s", locale, got.Code(), want.Code())
		}
	}
}

func TestUnknownTranslationKeysFallBackToTheName(t *testing.T) {
	if got := Translate("NoSuchKey", "en"); got != "NoSuchKey" {
		t.Fatalf("Translate = %q, want the key name", got)
	}
}

func TestKnownTranslationKeysUseTheLanguage(t *testing.T) {
	want := i18n.Text(i18n.De, i18n.NavDashboard)
	if got := Translate("NavDashboard", "de"); got != want {
		t.Fatalf("Translate = %q, want %q", got, want)
	}
}

func TestAboutVersionTextUsesTheVersionArgument(t *testing.T) {
	want := i18n.Format(i18n.En, i18n.AboutVersion, i18n.Str("9.8.7"))
	if got := AboutVersionText(i18n.En, "9.8.7"); got != want {
		t.Fatalf("AboutVersionText = %q, want %q", got, want)
	}
}

func TestPassphraseHintUsesTheMinimumLength(t *testing.T) {
	if MinPassphraseLength != 10 {
		t.Fatalf("MinPassphraseLength = %d", MinPassphraseLength)
	}
	want := i18n.Format(i18n.En, i18n.PassphraseHint, i18n.Int(MinPassphraseLength))
	if got := PassphraseHint(i18n.En); got != want {
		t.Fatalf("PassphraseHint = %q, want %q", got, want)
	}
}

func TestTheUnlockLabelShowsTheProfileName(t *testing.T) {
	appPaths := testPaths(t)

	if got := ProfileLabel(i18n.En, appPaths, nil); got != "Profile: Personal" {
		t.Fatalf("default profile label = %q", got)
	}

	business, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if got := ProfileLabel(i18n.En, appPaths, &business.Slug); got != "Profile: Business" {
		t.Fatalf("named profile label = %q", got)
	}
}

func TestSystemLanguageUsesTheInjectedLookup(t *testing.T) {
	values := map[string]string{"LC_MESSAGES": "fr_FR.UTF-8", "LANG": "de_DE.UTF-8"}
	getenv := func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
	if got := SystemLanguage(getenv); got != i18n.Fr {
		t.Fatalf("SystemLanguage = %s, want French", got.Code())
	}

	values["LC_ALL"] = "de_DE.UTF-8"
	if got := SystemLanguage(getenv); got != i18n.De {
		t.Fatalf("SystemLanguage = %s, want German", got.Code())
	}

	empty := func(string) (string, bool) { return "", false }
	if got := SystemLanguage(empty); got != i18n.En {
		t.Fatalf("SystemLanguage = %s, want English", got.Code())
	}
}
