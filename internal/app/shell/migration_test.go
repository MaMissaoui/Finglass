package shell

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coremigration "github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/instance"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
)

func readMarker(t *testing.T, appPaths paths.AppPaths) string {
	t.Helper()
	data, err := fs.ReadFile(os.DirFS(appPaths.Root()), coremigration.MarkerFile)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return string(data)
}

func TestBothWelcomeActionsWriteTheMarker(t *testing.T) {
	appPaths := testPaths(t)
	if err := WelcomeAction(appPaths, true, testNow); err != nil {
		t.Fatalf("welcome migrate: %v", err)
	}
	if got := readMarker(t, appPaths); !strings.HasPrefix(got, "migrate ") {
		t.Fatalf("marker = %q", got)
	}

	if err := WelcomeAction(appPaths, false, testNow); err != nil {
		t.Fatalf("welcome skip: %v", err)
	}
	if got := readMarker(t, appPaths); !strings.HasPrefix(got, "skip ") {
		t.Fatalf("marker = %q", got)
	}
}

func TestEveryMigrationErrorHasText(t *testing.T) {
	migrationErrors := []*coremigration.MigrationError{
		{Kind: coremigration.MigrationTargetNotEmpty},
		{Kind: coremigration.MigrationFinDesk, Err: findeskimport.ErrSourceChanged},
		{Kind: coremigration.MigrationFinDesk, Err: findeskimport.ErrWrongPassphrase},
	}
	for _, language := range i18n.Languages {
		for _, migrationError := range migrationErrors {
			if migrationError.Message(language) == "" {
				t.Fatalf("empty migration error text in %s for %v", language.Code(), migrationError)
			}
		}
	}
}

func TestAnUnlockErrorHasTextInEveryLanguage(t *testing.T) {
	for _, language := range i18n.Languages {
		if UnlockErrorText(language, unlock.ErrMismatch) == "" {
			t.Fatalf("empty unlock error text in %s", language.Code())
		}
	}
}

func TestOnlyTheWelcomeActionsWriteTheMigrationMarker(t *testing.T) {
	appPaths := testPaths(t)
	findeskRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(findeskRoot, "findesk.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write findesk.db: %v", err)
	}

	install := coremigration.Discover(findeskRoot)
	if install == nil {
		t.Fatal("the FinDesk install was not discovered")
	}
	if !coremigration.ShouldOffer(appPaths, install) {
		t.Fatal("the migration offer is missing")
	}
	marker := filepath.Join(appPaths.Root(), coremigration.MarkerFile)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker exists before a welcome action: %v", err)
	}

	if _, err := startup.Prepare(appPaths, testNow); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovery and startup wrote the marker: %v", err)
	}

	if err := WelcomeAction(appPaths, true, testNow); err != nil {
		t.Fatalf("welcome migrate: %v", err)
	}
	if got := readMarker(t, appPaths); !strings.HasPrefix(got, "migrate ") {
		t.Fatalf("marker = %q", got)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if err := WelcomeAction(appPaths, false, testNow); err != nil {
		t.Fatalf("welcome skip: %v", err)
	}
	if got := readMarker(t, appPaths); !strings.HasPrefix(got, "skip ") {
		t.Fatalf("marker = %q", got)
	}
}

func TestStartOffersTheMigrationFromTheInjectedRoot(t *testing.T) {
	appPaths := testPaths(t)
	findeskRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(findeskRoot, "findesk.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write findesk.db: %v", err)
	}

	session, offer, refusal := Start(
		appPaths,
		banksync.NoCredentialStore{},
		&findeskRoot,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("Start refused: %+v", refusal)
	}
	if offer == nil {
		t.Fatal("Start did not offer the injected FinDesk install")
	}
	if session == nil {
		t.Fatal("Start returned no session")
	}
	t.Cleanup(func() { _ = session.Shutdown() })

	screen := session.UnlockScreen()
	if screen.Mode != "create" {
		t.Fatalf("unlock mode = %q, want create on a fresh root", screen.Mode)
	}
	if screen.AllowChange || screen.Restored || screen.Busy || screen.StartupError {
		t.Fatalf("unexpected fresh unlock screen: %+v", screen)
	}
	if _, err := os.Stat(filepath.Join(appPaths.Root(), coremigration.MarkerFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Start wrote the migration marker: %v", err)
	}
}

func TestTheWizardAdvancesThroughEveryProfile(t *testing.T) {
	install := coremigration.FinDeskInstall{Profiles: []coremigration.FinDeskProfile{
		{Name: "Personal"},
		{Name: "Business"},
	}}
	wizard := NewMigrationSession(install, startup.Startup{})
	if err := wizard.BeginMigration(i18n.En, testPaths(t), testNow, true); err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if wizard.Step() != MigrationProfile || wizard.Index() != 0 {
		t.Fatalf("step = %v, index = %d", wizard.Step(), wizard.Index())
	}
	if heading := wizard.ProfileHeading(i18n.En); heading == "" {
		t.Fatal("the profile heading is empty")
	}

	wizard.SkipCurrent()
	if wizard.Step() != MigrationProfile || wizard.Index() != 1 {
		t.Fatalf("step = %v, index = %d", wizard.Step(), wizard.Index())
	}
	wizard.SkipCurrent()

	if wizard.Step() != MigrationSummary {
		t.Fatalf("step = %v, want summary", wizard.Step())
	}
	if got := len(wizard.Results()); got != 2 {
		t.Fatalf("results = %d, want 2", got)
	}
	if lines := wizard.SummaryLines(i18n.En); len(lines) != 2 || lines[0] == "" {
		t.Fatalf("summary lines = %v", lines)
	}
	pairs, active, err := wizard.Continue()
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if len(pairs) != 0 || active != nil {
		t.Fatalf("pairs = %v, active = %v", pairs, active)
	}
	wizard.FinishContinue()
	if wizard.Busy() {
		t.Fatal("the wizard is still busy")
	}
}

func TestASecondInstanceDoesNotApplyAStagedRestore(t *testing.T) {
	appPaths := testPaths(t)
	database := createTestDatabase(t, appPaths, "default")

	name := "Account"
	if _, err := accounts.Create(context.Background(), database, model.NewAccount{
		Name:        &name,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	service := backup.NewService(defaultDataDir(t, appPaths), "")
	backupFile := filepath.Join(t.TempDir(), "startup.zip")
	if _, err := service.CreateBackup(database, backupFile); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if _, err := service.StageRestore(database, backupFile, nil); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	held, err := instance.Acquire(appPaths)
	if err != nil {
		t.Fatalf("hold the lock: %v", err)
	}
	defer func() { _ = held.Release() }()

	session, offer, refusal := Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal == nil {
		t.Fatal("a second instance was not refused")
	}
	if refusal.Kind != StartupAlreadyRunning {
		t.Fatalf("refusal kind = %d, want AlreadyRunning", refusal.Kind)
	}
	if offer != nil {
		t.Fatal("a refused startup offered a migration")
	}
	if session == nil {
		t.Fatal("the refusal did not return a session for the unlock screen")
	}
	screen := session.UnlockScreen()
	if !screen.StartupError {
		t.Fatal("the unlock screen does not show the startup error")
	}
	if want := i18n.Text(i18n.En, i18n.ErrorAlreadyRunning); screen.ErrorText != want {
		t.Fatalf("error text = %q, want %q", screen.ErrorText, want)
	}
	_ = session.Shutdown()

	if !service.HasPendingRestore() {
		t.Fatal("the staged restore is no longer pending")
	}
	if preRestores := preRestoreFolders(t, appPaths.Root()); len(preRestores) != 0 {
		t.Fatalf("a refused startup moved data aside: %v", preRestores)
	}
}
