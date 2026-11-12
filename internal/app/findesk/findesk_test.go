package findesk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	fixturePassphrase = "fixture passphrase 1"
	targetPassphrase  = "target passphrase 1"
)

func fixtureCopy(t *testing.T) string {
	t.Helper()
	fixture := filepath.Join("..", "..", "core", "findeskimport", "testdata", "findesk")
	root := filepath.Join(t.TempDir(), "findesk")
	copyTree(t, fixture, root)
	return root
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatalf("create %s: %v", to, err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		target := filepath.Join(to, entry.Name())
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", source, err)
		}
		if info.IsDir() {
			copyTree(t, source, target)
			continue
		}
		data, err := os.ReadFile(source) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil { // #nosec G703 -- target is inside the test's temporary copy.
			t.Fatalf("write %s: %v", target, err)
		}
	}
}

func fixturePass(t *testing.T) *secret.Passphrase {
	t.Helper()
	created, err := secret.NewPassphrase([]byte(fixturePassphrase), secret.Unlock)
	if err != nil {
		t.Fatalf("new FinDesk passphrase: %v", err)
	}
	t.Cleanup(created.Wipe)
	return created
}

func freshProfile(t *testing.T) (paths.AppPaths, *db.Database) {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	opened, err := unlock.Create(resolved, "default", []byte(targetPassphrase), []byte(targetPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return resolved, opened
}

func stringPointer(value string) *string {
	return &value
}

func TestTheStartFolderIsFindeskHomeOrTheHiddenHomeFolder(t *testing.T) {
	home := "/home/alice"
	if got := FindeskStartDir(home, nil); got != "/home/alice/.findesk" {
		t.Errorf("start dir = %q", got)
	}

	custom := "/data/findesk-data"
	if got := FindeskStartDir(home, stringPointer(custom)); got != custom {
		t.Errorf("start dir = %q, want %q", got, custom)
	}
}

func TestADataFolderADatabaseFileAndAProfileRootAreAccepted(t *testing.T) {
	temp := t.TempDir()
	data := filepath.Join(temp, "findesk")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatalf("create data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(data, "findesk.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}

	if got := ResolveFindeskDir(data); got == nil || *got != data {
		t.Errorf("folder = %v, want %q", got, data)
	}
	if got := ResolveFindeskDir(filepath.Join(data, "findesk.db")); got == nil || *got != data {
		t.Errorf("file = %v, want %q", got, data)
	}

	random := filepath.Join(temp, "random")
	if err := os.MkdirAll(random, 0o700); err != nil {
		t.Fatalf("create random: %v", err)
	}
	if got := ResolveFindeskDir(random); got != nil {
		t.Errorf("random = %v, want nil", *got)
	}
}

func TestARootWithProfilesUsesItsDefaultDatabaseOrItsOnlyProfile(t *testing.T) {
	temp := t.TempDir()
	root := filepath.Join(temp, "findesk")
	work := filepath.Join(root, "profiles", "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatalf("create work: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "findesk.db"), []byte("work"), 0o600); err != nil {
		t.Fatalf("write work database: %v", err)
	}

	if got := ResolveFindeskDir(root); got == nil || *got != work {
		t.Errorf("only profile = %v, want %q", got, work)
	}

	home := filepath.Join(root, "profiles", "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "findesk.db"), []byte("home"), 0o600); err != nil {
		t.Fatalf("write home database: %v", err)
	}
	if got := ResolveFindeskDir(root); got != nil {
		t.Errorf("two profiles = %v, want nil", *got)
	}
	if got := ResolveFindeskDir(home); got == nil || *got != home {
		t.Errorf("profile folder = %v, want %q", got, home)
	}

	if err := os.WriteFile(filepath.Join(root, "findesk.db"), []byte("default"), 0o600); err != nil {
		t.Fatalf("write default database: %v", err)
	}
	if got := ResolveFindeskDir(root); got == nil || *got != root {
		t.Errorf("default = %v, want %q", got, root)
	}
}

func TestTheStateImportsTheFixtureAndReportsTheCounts(t *testing.T) {
	sourceDir := fixtureCopy(t)
	appPaths, opened := freshProfile(t)

	state := NewFindeskImportState(i18n.En)
	if got := state.Choose(sourceDir); got == nil || *got != sourceDir {
		t.Fatalf("choose = %v, want %q", got, sourceDir)
	}
	report, err := state.ImportInProfile(opened, appPaths, nil, fixturePass(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if report.AccountGroups.Imported != 1 {
		t.Errorf("account groups = %d", report.AccountGroups.Imported)
	}
	if report.BankConnections.Imported != 2 {
		t.Errorf("bank connections = %d", report.BankConnections.Imported)
	}
	if report.Accounts.Imported != 3 {
		t.Errorf("accounts = %d", report.Accounts.Imported)
	}
	if report.Categories.Imported != 3 {
		t.Errorf("categories = %d", report.Categories.Imported)
	}
	if report.Rules.Imported != 2 {
		t.Errorf("rules = %d", report.Rules.Imported)
	}
	if report.Transactions.Imported != 8 {
		t.Errorf("transactions = %d", report.Transactions.Imported)
	}
	if report.Documents.Copied != 1 {
		t.Errorf("documents copied = %d", report.Documents.Copied)
	}
	if report.Documents.Missing != 2 {
		t.Errorf("documents missing = %d", report.Documents.Missing)
	}
	if report.Settings.Imported != 8 {
		t.Errorf("settings = %d", report.Settings.Imported)
	}
	if report.PaymentOrdersLeftBehind != 1 {
		t.Errorf("payment orders = %d", report.PaymentOrdersLeftBehind)
	}
	if state.Report != report {
		t.Error("state did not keep the report")
	}
	if state.Error != nil {
		t.Errorf("error = %q", *state.Error)
	}
	if state.PassphraseWrong {
		t.Error("passphrase marked wrong")
	}

	text := state.ReportText()
	if text == nil || !strings.Contains(*text, "payment orders") {
		t.Errorf("report text = %v", text)
	}
}

func TestANonEmptyProfileReportsTheNamedProfileHint(t *testing.T) {
	sourceDir := fixtureCopy(t)
	appPaths, opened := freshProfile(t)

	first := NewFindeskImportState(i18n.En)
	first.Choose(sourceDir)
	if _, err := first.ImportInProfile(opened, appPaths, nil, fixturePass(t)); err != nil {
		t.Fatalf("first import: %v", err)
	}

	second := NewFindeskImportState(i18n.En)
	second.Choose(sourceDir)
	_, err := second.ImportInProfile(opened, appPaths, nil, fixturePass(t))
	if !errorIs(err, findeskimport.ErrTargetNotEmpty) {
		t.Fatalf("error = %v, want target not empty", err)
	}
	if second.Error == nil {
		t.Fatal("no error text")
	}
	text := *second.Error
	if !strings.Contains(text, "new profile") || !strings.Contains(text, "Profiles") || !strings.Contains(text, "import") {
		t.Errorf("error text = %q", text)
	}
	if second.Report != nil {
		t.Error("a failed import kept a report")
	}
}

func TestAWrongPassphraseKeepsTheFolderForARetry(t *testing.T) {
	sourceDir := fixtureCopy(t)
	appPaths, opened := freshProfile(t)

	state := NewFindeskImportState(i18n.En)
	state.Choose(sourceDir)
	wrong, err := secret.NewPassphrase([]byte("not the fixture passphrase"), secret.Unlock)
	if err != nil {
		t.Fatalf("new wrong passphrase: %v", err)
	}
	defer wrong.Wipe()

	_, err = state.ImportInProfile(opened, appPaths, nil, wrong)
	if !errorIs(err, findeskimport.ErrWrongPassphrase) {
		t.Fatalf("error = %v, want a wrong passphrase", err)
	}
	if !state.PassphraseWrong {
		t.Error("passphrase not marked wrong")
	}
	if state.Error != nil {
		t.Errorf("error text = %q", *state.Error)
	}
	if state.SourceDir == nil || *state.SourceDir != sourceDir {
		t.Errorf("source dir = %v, want %q", state.SourceDir, sourceDir)
	}
	if state.IsImporting {
		t.Error("still importing")
	}
}

func TestReportTextBreaksDownEverySkippedRowOnce(t *testing.T) {
	report := &findeskimport.FinDeskImportReport{
		Transactions: findeskimport.ImportCounts{
			Imported:                4,
			Skipped:                 3,
			SkippedOrphan:           1,
			SkippedInvalid:          1,
			SkippedFractionalAmount: 1,
		},
		Documents: findeskimport.DocumentImportCounts{
			Copied:         2,
			Skipped:        3,
			Missing:        2,
			Refused:        1,
			SkippedInvalid: 0,
		},
		Settings: findeskimport.SettingsImportCounts{
			Imported: 5,
			Skipped:  1,
		},
		PaymentOrdersLeftBehind: 7,
	}

	text := ReportText(report, i18n.En)
	if !strings.Contains(text, "Imported 0 account groups") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "2 documents, 0 OCR texts and 5 settings") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "Skipped 7 rows, 2 documents with a missing file and 1 refused documents") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "Left 7 payment orders behind") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(
		text,
		"Skipped: 1 rows without their account, category or document, 1 invalid rows, "+
			"1 with a fractional cent amount, and 1 settings Finglass does not use.",
	) {
		t.Errorf("text = %q", text)
	}
	if lines := len(strings.Split(strings.TrimSuffix(text, "\n"), "\n")); lines != 2 {
		t.Errorf("lines = %d, text = %q", lines, text)
	}
}

// errorIs is a tiny alias so the assertions above read like the Rust tests.
func errorIs(err, target error) bool {
	return errors.Is(err, target)
}

func TestPassphrasesNeverFormatted(t *testing.T) {
	state := NewFindeskImportState(i18n.En)
	state.SourceDir = stringPointer("/findesk")
	message := "failed"
	state.Error = &message

	values := []any{
		state,
		&findeskimport.FinDeskImportError{Kind: findeskimport.KindWrongPassphrase},
		&findeskimport.FinDeskImportError{
			Kind: findeskimport.KindIO,
			Err:  errors.New("io failure"),
		},
	}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, fixturePassphrase) || strings.Contains(text, targetPassphrase) {
				t.Errorf("%s of %T contains a passphrase: %s", format, value, text)
			}
		}
	}
}
