package bind

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	coremigration "github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/app/profileswitch"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

const testPassphrase = "correct horse battery staple"

var testNow = time.Date(2026, 10, 15, 12, 34, 56, 0, time.UTC)

type shellFixture struct {
	paths    paths.AppPaths
	session  *shell.Session
	service  *Shell
	events   []ShellDTO
	opened   []string
	observed [][]byte
}

func newShellFixture(t *testing.T) *shellFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	session, offer, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	if offer != nil {
		t.Fatal("a nil FinDesk root offered a migration")
	}

	fixture := &shellFixture{paths: appPaths, session: session}
	fixture.service = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
		OpenURL: func(url string) {
			fixture.opened = append(fixture.opened, url)
		},
		Emit: func(dto ShellDTO) {
			fixture.events = append(fixture.events, dto)
		},
	})
	fixture.service.observePassphrase = func(value []byte) {
		fixture.observed = append(fixture.observed, value)
	}
	t.Cleanup(func() {
		_ = fixture.service.ServiceShutdown()
	})
	return fixture
}

func newOfferFixture(t *testing.T) *shellFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	findeskRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(findeskRoot, "findesk.db"), []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write FinDesk database: %v", err)
	}

	session, offer, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		&findeskRoot,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	if offer == nil {
		t.Fatal("the FinDesk root was not offered")
	}

	fixture := &shellFixture{paths: appPaths, session: session}
	fixture.service = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
		Emit: func(dto ShellDTO) {
			fixture.events = append(fixture.events, dto)
		},
	})
	t.Cleanup(func() {
		_ = fixture.service.ServiceShutdown()
	})
	return fixture
}

func (f *shellFixture) restart(t *testing.T) *shellFixture {
	t.Helper()
	if err := f.service.ServiceShutdown(); err != nil {
		t.Fatalf("shut down first service: %v", err)
	}

	session, offer, refusal := shell.Start(
		f.paths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("restart refused: %v", refusal)
	}
	if offer != nil {
		t.Fatal("a nil FinDesk root offered a migration")
	}

	restarted := &shellFixture{paths: f.paths, session: session}
	restarted.service = NewShell(session, offer, ShellOptions{
		Paths: f.paths,
		Clock: func() time.Time { return testNow },
		OpenURL: func(url string) {
			restarted.opened = append(restarted.opened, url)
		},
		Emit: func(dto ShellDTO) {
			restarted.events = append(restarted.events, dto)
		},
	})
	t.Cleanup(func() {
		_ = restarted.service.ServiceShutdown()
	})
	return restarted
}

func requireEvent(t *testing.T, fixture *shellFixture, dto ShellDTO) {
	t.Helper()
	if len(fixture.events) == 0 {
		t.Fatal("no shell:changed event")
	}
	if got := fixture.events[len(fixture.events)-1]; !reflect.DeepEqual(got, dto) {
		t.Fatalf("event DTO = %+v, want the returned DTO %+v", got, dto)
	}
}

func TestFullShellFlow(t *testing.T) {
	fixture := newShellFixture(t)

	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.Mode != "create" {
		t.Fatalf("initial mode = %q, want create", state.Mode)
	}
	if state.ShellOpen {
		t.Fatal("the shell is open before the database exists")
	}
	if state.Screen != "dashboard" || state.ScreenTitle != shell.Title("dashboard", i18n.En) {
		t.Fatalf("initial screen = %q / %q", state.Screen, state.ScreenTitle)
	}
	if state.Version != Version || state.AboutVersion != shell.AboutVersionText(i18n.En, Version) {
		t.Fatalf("version = %q, about text = %q", state.Version, state.AboutVersion)
	}
	if len(fixture.events) != 0 {
		t.Fatalf("State emitted %d events", len(fixture.events))
	}

	short := "short"
	tooShort, err := fixture.service.Create(short, short, true)
	if err != nil {
		t.Fatalf("Create(short): %v", err)
	}
	wantShort := i18n.Format(i18n.En, i18n.PassphraseTooShort, i18n.Int(shell.MinPassphraseLength))
	if tooShort.ErrorText != wantShort {
		t.Fatalf("too-short error = %q, want %q", tooShort.ErrorText, wantShort)
	}
	if tooShort.ShellOpen {
		t.Fatal("a refused Create opened the shell")
	}
	requireEvent(t, fixture, tooShort)

	created, err := fixture.service.Create(testPassphrase, testPassphrase, true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.ShellOpen {
		t.Fatal("a valid Create did not open the shell")
	}
	if created.Screen != "dashboard" || created.ScreenTitle != shell.Title("dashboard", i18n.En) {
		t.Fatalf("opened screen = %q / %q", created.Screen, created.ScreenTitle)
	}
	if created.CurrentProfileName == "" {
		t.Fatal("the open profile has no name")
	}
	requireEvent(t, fixture, created)

	navigated, err := fixture.service.Navigate("transactions")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if navigated.Screen != "transactions" || navigated.ScreenTitle != shell.Title("transactions", i18n.En) {
		t.Fatalf("navigated screen = %q / %q", navigated.Screen, navigated.ScreenTitle)
	}
	if !navigated.FindEnabled {
		t.Fatal("Find is not enabled on Transactions")
	}
	requireEvent(t, fixture, navigated)

	// A link leaves a back entry; Back reopens the page it left.
	fixture.session.FollowLink("reports", shell.PageArgs{})
	linked, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State after link: %v", err)
	}
	if linked.BackTitle != shell.Title("transactions", i18n.En) {
		t.Fatalf("back title = %q, want Transactions", linked.BackTitle)
	}
	back, err := fixture.service.Back()
	if err != nil {
		t.Fatalf("Back: %v", err)
	}
	if back.Screen != "transactions" {
		t.Fatalf("Back opened %q, want transactions", back.Screen)
	}
	if back.BackTitle != "" {
		t.Fatalf("back title after Back = %q, want empty", back.BackTitle)
	}
	requireEvent(t, fixture, back)

	german, err := fixture.service.SetLanguage("de")
	if err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if german.Language != "de" || german.ScreenTitle != shell.Title("transactions", i18n.De) {
		t.Fatalf("German state = %q / %q", german.Language, german.ScreenTitle)
	}
	requireEvent(t, fixture, german)

	dark, err := fixture.service.SetTheme("dark")
	if err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if dark.Theme != "dark" {
		t.Fatalf("theme = %q, want dark", dark.Theme)
	}
	requireEvent(t, fixture, dark)

	collapsed, err := fixture.service.SetSidebarCollapsed(true)
	if err != nil {
		t.Fatalf("SetSidebarCollapsed: %v", err)
	}
	if !collapsed.SidebarCollapsed {
		t.Fatal("the sidebar is not collapsed")
	}
	requireEvent(t, fixture, collapsed)

	restarted := fixture.restart(t)
	locked, err := restarted.service.State()
	if err != nil {
		t.Fatalf("restarted State: %v", err)
	}
	if locked.Mode != "unlock" {
		t.Fatalf("restarted mode = %q, want unlock", locked.Mode)
	}
	if locked.ShellOpen {
		t.Fatal("the restarted service is open before unlocking")
	}

	wrong, err := restarted.service.Unlock("this is not the passphrase")
	if err != nil {
		t.Fatalf("Unlock(wrong): %v", err)
	}
	if wrong.ErrorText != i18n.Text(i18n.En, i18n.WrongPassphrase) {
		t.Fatalf("wrong-passphrase error = %q", wrong.ErrorText)
	}
	if wrong.ShellOpen {
		t.Fatal("a wrong passphrase opened the shell")
	}
	requireEvent(t, restarted, wrong)

	opened, err := restarted.service.Unlock(testPassphrase)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if !opened.ShellOpen || opened.Screen != "dashboard" {
		t.Fatalf("opened state = %+v", opened)
	}
	if opened.Language != "de" || opened.Theme != "dark" {
		t.Fatalf("saved preferences = language %q, theme %q", opened.Language, opened.Theme)
	}
	if !opened.SidebarCollapsed {
		t.Fatal("the saved collapsed sidebar was not restored on unlock")
	}
	requireEvent(t, restarted, opened)
}

func TestProfileListSwitchAndRefusedBusySwitch(t *testing.T) {
	fixture := newShellFixture(t)

	if _, err := profiles.Create(fixture.paths, "Business"); err != nil {
		t.Fatalf("create Business profile: %v", err)
	}
	if _, err := fixture.service.Create(testPassphrase, testPassphrase, true); err != nil {
		t.Fatalf("Create: %v", err)
	}

	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(state.Profiles) != 2 {
		t.Fatalf("profiles = %+v, want two", state.Profiles)
	}
	if state.Profiles[0].Name != "Personal" || !state.Profiles[0].Open || state.Profiles[0].Slug != "" {
		t.Fatalf("default profile = %+v", state.Profiles[0])
	}
	if state.Profiles[1].Name != "Business" || state.Profiles[1].Open || state.Profiles[1].Slug == "" {
		t.Fatalf("Business profile = %+v", state.Profiles[1])
	}

	// The menu lists the loaded Settings rows SwitchProfile indexes, so a
	// profile created on disk meanwhile cannot shift the indexes.
	if _, err := profiles.Create(fixture.paths, "Aardvark"); err != nil {
		t.Fatalf("create a later profile: %v", err)
	}
	if later, err := fixture.service.State(); err != nil || !reflect.DeepEqual(later.Profiles, state.Profiles) {
		t.Fatalf("profiles after a disk change = %+v (%v), want the loaded %+v", later.Profiles, err, state.Profiles)
	}

	// The unlock started the dashboard's first load, and a switch is refused
	// while a job runs (as Rust's), so let that load finish first.
	waitForDashboardLoad(t, fixture.session)

	switched, err := fixture.service.SwitchProfile(1)
	if err != nil {
		t.Fatalf("SwitchProfile: %v", err)
	}
	if switched.ShellOpen {
		t.Fatal("the shell is open after switching profiles")
	}
	if switched.Mode != "create" {
		t.Fatalf("target mode = %q, want create", switched.Mode)
	}
	if switched.ProfileName != "Business" || switched.CurrentProfileName != "Business" {
		t.Fatalf("target profile = %q / %q", switched.ProfileName, switched.CurrentProfileName)
	}
	for _, profile := range switched.Profiles {
		if profile.Open {
			t.Fatalf("profile %q is marked open after the teardown", profile.Name)
		}
	}
	requireEvent(t, fixture, switched)

	busyFixture := newShellFixture(t)
	if _, err := profiles.Create(busyFixture.paths, "Business"); err != nil {
		t.Fatalf("create Business profile: %v", err)
	}
	if _, err := busyFixture.service.Create(testPassphrase, testPassphrase, true); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, err := busyFixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	events := len(busyFixture.events)
	busyFixture.service.switchProfile = func(int) error {
		return &profileswitch.SwitchError{Kind: profileswitch.SwitchBusy}
	}

	refused, err := busyFixture.service.SwitchProfile(1)
	if err == nil {
		t.Fatal("a busy switch was accepted")
	}
	if !reflect.DeepEqual(refused, before) {
		t.Fatalf("a refused switch changed the DTO: %+v, want %+v", refused, before)
	}
	if len(busyFixture.events) != events {
		t.Fatal("a refused switch emitted an event")
	}
	after, err := busyFixture.service.State()
	if err != nil {
		t.Fatalf("State after refusal: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("a refused switch changed the state: %+v, want %+v", after, before)
	}
}

func TestManageProfilesOpensSettings(t *testing.T) {
	fixture := newShellFixture(t)
	if _, err := fixture.service.Create(testPassphrase, testPassphrase, true); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dto, err := fixture.service.ManageProfiles()
	if err != nil {
		t.Fatalf("ManageProfiles: %v", err)
	}
	if dto.Screen != "settings" || dto.ScreenTitle != shell.Title("settings", i18n.En) {
		t.Fatalf("screen = %q / %q", dto.Screen, dto.ScreenTitle)
	}
	// Rust's navigate_to_section is a plain navigation: no back entry.
	if dto.BackTitle != "" {
		t.Fatalf("back title = %q after Manage profiles, want none", dto.BackTitle)
	}
	requireEvent(t, fixture, dto)
}

func TestEveryPassphraseSliceIsWiped(t *testing.T) {
	fixture := newShellFixture(t)

	if _, err := fixture.service.Create(testPassphrase, testPassphrase, true); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := fixture.service.Change(testPassphrase, "another good passphrase", "another good passphrase", true); err != nil {
		t.Fatalf("Change: %v", err)
	}
	if _, err := fixture.service.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	fixture.service.wizard = shell.NewMigrationSession(coremigration.FinDeskInstall{
		Profiles: []coremigration.FinDeskProfile{{Name: "Personal"}},
	}, startup.Startup{})
	fixture.service.active.Store(true)
	if _, err := fixture.service.MigrationPassphraseChanged(testPassphrase); err != nil {
		t.Fatalf("MigrationPassphraseChanged: %v", err)
	}

	// A FinDesk profile in an empty temp folder: the migration itself fails,
	// but the three slices are created and must still be wiped.
	findeskRoot := t.TempDir()
	fixture.service.wizard = shell.NewMigrationSession(coremigration.FinDeskInstall{
		Root:     findeskRoot,
		Profiles: []coremigration.FinDeskProfile{{Name: "Personal", DataDir: findeskRoot}},
	}, startup.Startup{})
	before := len(fixture.observed)
	if _, err := fixture.service.MigrationSubmit(testPassphrase, "another good passphrase", "another good passphrase", false); err != nil {
		t.Fatalf("MigrationSubmit: %v", err)
	}
	if got := len(fixture.observed) - before; got != 3 {
		t.Fatalf("MigrationSubmit created %d observed slices, want 3", got)
	}

	if len(fixture.observed) < 9 {
		t.Fatalf("observed %d passphrase slices, want at least 9", len(fixture.observed))
	}
	for index, value := range fixture.observed {
		for _, b := range value {
			if b != 0 {
				t.Fatalf("passphrase slice %d was not wiped: %v", index, value)
			}
		}
	}
}

func TestShellDTOJSONNeverContainsThePassphrase(t *testing.T) {
	fixture := newShellFixture(t)
	dto, err := fixture.service.Create(testPassphrase, testPassphrase, true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	if strings.Contains(string(encoded), testPassphrase) {
		t.Fatalf("the DTO JSON contains the passphrase: %s", encoded)
	}

	body := string(encoded)
	for _, forbidden := range []string{`"passphrase":`, `"currentPassphrase":`, `"newPassphrase":`, `"repeat":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the DTO JSON has a passphrase field %s: %s", forbidden, body)
		}
	}
}

func TestUnknownCodesAreRefusedWithoutAStateChange(t *testing.T) {
	fixture := newShellFixture(t)
	before, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	events := len(fixture.events)

	checks := []struct {
		name string
		call func() (ShellDTO, error)
	}{
		{"language", func() (ShellDTO, error) { return fixture.service.SetLanguage("xx") }},
		{"theme", func() (ShellDTO, error) { return fixture.service.SetTheme("neon") }},
		{"screen", func() (ShellDTO, error) { return fixture.service.Navigate("nowhere") }},
		{"menu index", func() (ShellDTO, error) { return fixture.service.NavigateMenuItem(99) }},
	}
	for _, check := range checks {
		dto, err := check.call()
		if err == nil {
			t.Fatalf("%s was accepted", check.name)
		}
		if !reflect.DeepEqual(dto, before) {
			t.Fatalf("%s changed the state: %+v, want %+v", check.name, dto, before)
		}
		if len(fixture.events) != events {
			t.Fatalf("%s emitted an event", check.name)
		}
	}

	after, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed: %+v, want %+v", after, before)
	}
}

func TestOpenHelpUsesTheInjectedOpener(t *testing.T) {
	fixture := newShellFixture(t)

	if _, err := fixture.service.OpenGitHub(); err != nil {
		t.Fatalf("OpenGitHub: %v", err)
	}
	if _, err := fixture.service.ReportProblem(); err != nil {
		t.Fatalf("ReportProblem: %v", err)
	}

	want := []string{shell.GithubURL, shell.ReportProblemURL}
	if !reflect.DeepEqual(fixture.opened, want) {
		t.Fatalf("opened URLs = %v, want %v", fixture.opened, want)
	}
}

func TestShowChangeSwitchesTheUnlockMode(t *testing.T) {
	fixture := newShellFixture(t)

	dto, err := fixture.service.ShowChange(true)
	if err != nil {
		t.Fatalf("ShowChange(true): %v", err)
	}
	if dto.Mode != "change" {
		t.Fatalf("mode = %q, want change", dto.Mode)
	}
	requireEvent(t, fixture, dto)

	dto, err = fixture.service.ShowChange(false)
	if err != nil {
		t.Fatalf("ShowChange(false): %v", err)
	}
	if dto.Mode != "unlock" {
		t.Fatalf("mode = %q, want unlock", dto.Mode)
	}
	requireEvent(t, fixture, dto)
}

func TestMigrationWizardStateFollowsTheSession(t *testing.T) {
	fixture := newOfferFixture(t)

	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.Mode != "migrate" {
		t.Fatalf("mode = %q, want migrate", state.Mode)
	}
	if state.MigrationStep != "welcome" {
		t.Fatalf("step = %q, want welcome", state.MigrationStep)
	}
	if state.MigrationProfileCount != 1 || !reflect.DeepEqual(state.MigrationProfileNames, []string{"Personal"}) {
		t.Fatalf("profiles = %d / %v", state.MigrationProfileCount, state.MigrationProfileNames)
	}
	if !state.MigrationSamePassphrase || state.MigrationNewPassphraseRequired {
		t.Fatalf("passphrase flags = same %v, new required %v", state.MigrationSamePassphrase, state.MigrationNewPassphraseRequired)
	}

	unacknowledged, err := fixture.service.MigrationMigrate(false)
	if err != nil {
		t.Fatalf("MigrationMigrate(false): %v", err)
	}
	if unacknowledged.MigrationStep != "welcome" {
		t.Fatalf("step = %q without the acknowledgement, want welcome", unacknowledged.MigrationStep)
	}
	requireEvent(t, fixture, unacknowledged)

	migrating, err := fixture.service.MigrationMigrate(true)
	if err != nil {
		t.Fatalf("MigrationMigrate: %v", err)
	}
	if migrating.MigrationStep != "profile" {
		t.Fatalf("step = %q, want profile", migrating.MigrationStep)
	}
	wantHeading := i18n.Format(
		i18n.En,
		i18n.MigrationProfileHeading,
		i18n.Int(1),
		i18n.Int(1),
		i18n.Str("Personal"),
	)
	if migrating.MigrationProfileHeading != wantHeading {
		t.Fatalf("heading = %q, want %q", migrating.MigrationProfileHeading, wantHeading)
	}
	requireEvent(t, fixture, migrating)

	changed, err := fixture.service.MigrationPassphraseChanged("short")
	if err != nil {
		t.Fatalf("MigrationPassphraseChanged: %v", err)
	}
	if !changed.MigrationNewPassphraseRequired {
		t.Fatal("a short FinDesk passphrase does not require a new Finglass passphrase")
	}
	requireEvent(t, fixture, changed)

	skipped, err := fixture.service.MigrationSkip()
	if err != nil {
		t.Fatalf("MigrationSkip: %v", err)
	}
	if skipped.MigrationStep != "summary" {
		t.Fatalf("step = %q, want summary", skipped.MigrationStep)
	}
	if len(skipped.MigrationSummaryLines) != 1 || !strings.Contains(skipped.MigrationSummaryLines[0], "Personal") {
		t.Fatalf("summary lines = %v", skipped.MigrationSummaryLines)
	}
	requireEvent(t, fixture, skipped)
}

func TestMigrationContinueShowsTheUnlockViewOfTheActiveProfile(t *testing.T) {
	fixture := newOfferFixture(t)

	if _, err := fixture.service.MigrationMigrate(true); err != nil {
		t.Fatalf("MigrationMigrate: %v", err)
	}
	if _, err := fixture.service.MigrationSkip(); err != nil {
		t.Fatalf("MigrationSkip: %v", err)
	}
	dto, err := fixture.service.MigrationContinue()
	if err != nil {
		t.Fatalf("MigrationContinue: %v", err)
	}
	if dto.Mode != "create" || dto.MigrationStep != "" || dto.ShellOpen || dto.Busy || dto.ErrorText != "" {
		t.Fatalf("state after continue = %+v, want the create view of the default profile", dto)
	}
	requireEvent(t, fixture, dto)
}

func TestMigrationStartEmptyLeavesTheWizard(t *testing.T) {
	fixture := newOfferFixture(t)

	dto, err := fixture.service.MigrationStartEmpty()
	if err != nil {
		t.Fatalf("MigrationStartEmpty: %v", err)
	}
	if dto.Mode == "migrate" {
		t.Fatal("the wizard is still showing after StartEmpty")
	}
	if dto.MigrationStep != "" {
		t.Fatalf("migration step = %q after leaving the wizard", dto.MigrationStep)
	}
	requireEvent(t, fixture, dto)
}

func TestMigrationWithoutAnOfferIsRefused(t *testing.T) {
	fixture := newShellFixture(t)

	if _, err := fixture.service.MigrationMigrate(true); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationMigrate error = %v, want ErrNoMigration", err)
	}
	if _, err := fixture.service.MigrationStartEmpty(); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationStartEmpty error = %v, want ErrNoMigration", err)
	}
	if _, err := fixture.service.MigrationSubmit("a", "b", "b", true); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationSubmit error = %v, want ErrNoMigration", err)
	}
	if _, err := fixture.service.MigrationSkip(); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationSkip error = %v, want ErrNoMigration", err)
	}
	if _, err := fixture.service.MigrationContinue(); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationContinue error = %v, want ErrNoMigration", err)
	}
	if _, err := fixture.service.MigrationPassphraseChanged("a"); !errors.Is(err, ErrNoMigration) {
		t.Fatalf("MigrationPassphraseChanged error = %v, want ErrNoMigration", err)
	}
}

// waitForDashboardLoad waits until the dashboard's load has published. A page
// job ends its generation before it notifies, so after that notification no
// dashboard job runs any more.
func waitForDashboardLoad(t *testing.T, session *shell.Session) {
	t.Helper()
	done := make(chan struct{}, 1)
	remove := session.AddListener(func(change shell.Change) {
		if change.Page == "dashboard" && !session.DashboardState().IsLoading {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	defer remove()
	if !session.DashboardState().IsLoading {
		return
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the dashboard load did not finish")
	}
}
