package bind

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const (
	restorePassphrase  = "restore passphrase 42"
	findeskPassphrase  = "fixture passphrase 1"
	corruptedZipMarker = "not a zip at all"
)

// bindFileDialogs is the bind tests' fake native-dialog seam.
type bindFileDialogs struct {
	mu sync.Mutex

	backupPath   string
	backupResult bool
	backupTitle  string
	backupName   string
	onBackup     func()

	pickBackup       string
	pickBackupResult bool

	findeskFolder string
	findeskResult bool
	findeskStart  string
	onFindesk     func()
}

func (f *bindFileDialogs) PickStatements(string) ([]string, bool) { return nil, false }

func (f *bindFileDialogs) PickReceipts(string) ([]string, bool) { return nil, false }

func (f *bindFileDialogs) SaveCSV(string, string) (string, bool) { return "", false }

func (f *bindFileDialogs) SaveFile(i18n.Key, string) (string, bool) { return "", false }

func (f *bindFileDialogs) SaveRules(string, string) (string, bool) { return "", false }

func (f *bindFileDialogs) PickRuleFile(string) (string, bool) { return "", false }

func (f *bindFileDialogs) SaveBackup(title string, defaultName string) (string, bool) {
	f.mu.Lock()
	f.backupTitle = title
	f.backupName = defaultName
	callback := f.onBackup
	path, result := f.backupPath, f.backupResult
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	return path, result
}

func (f *bindFileDialogs) PickBackup(string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pickBackup, f.pickBackupResult
}

func (f *bindFileDialogs) PickFindeskFolder(string, startDir string) (string, bool) {
	f.mu.Lock()
	f.findeskStart = startDir
	callback := f.onFindesk
	folder, result := f.findeskFolder, f.findeskResult
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	return folder, result
}

func (f *bindFileDialogs) setBackup(path string, result bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backupPath = path
	f.backupResult = result
}

func (f *bindFileDialogs) setPickBackup(path string, result bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pickBackup = path
	f.pickBackupResult = result
}

func (f *bindFileDialogs) setFindesk(folder string, result bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findeskFolder = folder
	f.findeskResult = result
}

// newSettingsDataFixture starts the Settings service on a fresh root with a
// fake dialog seam and a secret-observing hook.
func newSettingsDataFixture(t *testing.T) (*settingsBindFixture, *bindFileDialogs, *[]any) {
	t.Helper()
	fixture := newSettingsBindFixture(t)
	dialogs := &bindFileDialogs{}
	fixture.session.SetFileDialogs(dialogs)
	observed := make([]any, 0)
	var observedMu sync.Mutex
	fixture.service.observeSecret = func(value any) {
		observedMu.Lock()
		observed = append(observed, value)
		observedMu.Unlock()
	}
	return fixture, dialogs, &observed
}

func waitForDTO(
	t *testing.T,
	fixture *settingsBindFixture,
	description string,
	predicate func(SettingsDTO) bool,
) SettingsDTO {
	t.Helper()
	return fixture.wait(t, description, predicate)
}

func waitForValue(t *testing.T, description string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if predicate() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// otherPassphraseBackup writes a backup of a fresh profile database created
// under restorePassphrase.
func otherPassphraseBackup(t *testing.T) string {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	database, err := unlock.Create(
		appPaths,
		"default",
		[]byte(restorePassphrase),
		[]byte(restorePassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create other database: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Fatalf("close other database: %v", err)
		}
	}()
	if _, err := categories.Create(context.Background(), database, model.NewCategory{
		Name:  "Restored",
		Color: "#123456",
	}); err != nil {
		t.Fatalf("create other category: %v", err)
	}
	dir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("other data dir: %v", err)
	}
	target := filepath.Join(t.TempDir(), "restore-me.zip")
	if _, err := backup.NewService(dir, "").CreateBackup(database, target); err != nil {
		t.Fatalf("create other backup: %v", err)
	}
	return target
}

// openRestorePassphraseDialog chooses target and confirms it, so the passphrase
// dialog is open.
func openRestorePassphraseDialog(t *testing.T, fixture *settingsBindFixture, dialogs *bindFileDialogs, target string) {
	t.Helper()
	dialogs.setPickBackup(target, true)
	if _, err := fixture.service.RestoreBackup(); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	waitForDTO(t, fixture, "the restore candidate", func(dto SettingsDTO) bool {
		return dto.RestoreOpen
	})
	if _, err := fixture.service.ConfirmRestore(); err != nil {
		t.Fatalf("ConfirmRestore: %v", err)
	}
	waitForDTO(t, fixture, "the passphrase dialog", func(dto SettingsDTO) bool {
		return dto.RestorePassphraseOpen
	})
}

func TestSettingsDTOShowsProfilesBackupAndFindesk(t *testing.T) {
	open := "work"
	next := "work"
	backupPath := filepath.Join("private", "folder", "finglass-backup-2026-11-13.zip")
	state := *settingsview.New(i18n.En)
	state.Profiles = []profiles.Profile{
		{Name: "Personal"},
		{Name: "Work", Slug: "work"},
	}
	state.OpenProfile = &open
	state.NextProfile = &next
	state.RestoreCandidate = &settingsview.RestoreCandidate{
		Backup: backupPath,
		Info:   backup.Info{Created: "2026-11-13T08:00:00Z", Transactions: 3, Documents: 2},
	}
	state.RestorePassphraseFor = &backupPath
	state.RestorePassphraseWrong = true
	restoredFrom := filepath.Join("data", "pre-restore-20261113-080000")
	dialog := shell.SettingsDialogState{
		ProfileOpen:           true,
		ProfileIndex:          1,
		ProfileTitle:          "Rename Work",
		ProfileName:           "Work",
		ProfileError:          "oops",
		ProfileDeleteOpen:     true,
		ProfileDeleteIndex:    1,
		ProfileDeleteTitle:    "Delete Work",
		ProfileDeleteText:     "moved",
		FindeskPassphraseOpen: true,
	}
	findeskState := findesk.FindeskImportState{
		Language:        i18n.En,
		SourceDir:       pointer("/tmp/findesk"),
		PassphraseWrong: true,
		IsImporting:     true,
		Error:           pointer("the import failed"),
	}

	dto := settingsDTO(state, dialog, 0, &restoredFrom, findeskState)

	if len(dto.ProfileRows) != 2 {
		t.Fatalf("profile rows = %+v", dto.ProfileRows)
	}
	if dto.ProfileRows[0].Open || !dto.ProfileRows[0].CanSwitch || dto.ProfileRows[0].CanDelete ||
		dto.ProfileRows[0].NextStart {
		t.Fatalf("default row = %+v", dto.ProfileRows[0])
	}
	if !dto.ProfileRows[1].Open || dto.ProfileRows[1].CanSwitch || dto.ProfileRows[1].CanDelete ||
		dto.ProfileRows[1].NextStart {
		t.Fatalf("open row = %+v", dto.ProfileRows[1])
	}
	if !dto.ProfileDialogOpen || dto.ProfileDialogIndex != 1 || dto.ProfileDialogError != "oops" {
		t.Fatalf("profile dialog = %+v", dto)
	}
	if !dto.ProfileDeleteOpen || dto.ProfileDeleteIndex != 1 {
		t.Fatalf("profile delete dialog = %+v", dto)
	}

	if !dto.RestoreOpen {
		t.Fatal("the restore confirmation is closed")
	}
	if !strings.Contains(dto.RestoreTitle, "finglass-backup-2026-11-13.zip") {
		t.Fatalf("restore title = %q", dto.RestoreTitle)
	}
	if strings.Contains(dto.RestoreTitle, "private") {
		t.Fatalf("the restore title leaks the folder: %q", dto.RestoreTitle)
	}
	if dto.RestorePassphraseFile != "finglass-backup-2026-11-13.zip" {
		t.Fatalf("passphrase file = %q", dto.RestorePassphraseFile)
	}
	if !dto.RestorePassphraseOpen || !dto.RestorePassphraseWrong {
		t.Fatalf("passphrase dialog = %+v", dto)
	}
	if !strings.Contains(dto.BackupRestoredText, restoredFrom) {
		t.Fatalf("restored text = %q", dto.BackupRestoredText)
	}

	if !dto.FindeskBusy || dto.FindeskFolder != "/tmp/findesk" {
		t.Fatalf("findesk flags = busy %v folder %q", dto.FindeskBusy, dto.FindeskFolder)
	}
	if !dto.FindeskPassphraseOpen || !dto.FindeskPassphraseWrong {
		t.Fatalf("findesk passphrase = %+v", dto)
	}
	if !dto.FindeskReportOpen || !dto.FindeskReportError {
		t.Fatalf("a failed import without a report must open the failure dialog: %+v", dto)
	}
}

func pointer[T any](value T) *T {
	return &value
}

// newEmptySettingsBindFixture starts the Settings service on an empty database,
// so a FinDesk import can run.
func newEmptySettingsBindFixture(t *testing.T) *settingsBindFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	database, err := unlock.Create(
		appPaths,
		"default",
		[]byte(testPassphrase),
		[]byte(testPassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	session, offer, refusal := shell.Start(
		appPaths,
		nil,
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
	fixture := &settingsBindFixture{
		paths:   appPaths,
		session: session,
		events:  make(chan SettingsDTO, 256),
	}
	fixture.shell = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
	})
	fixture.service = NewSettings(session, SettingsOptions{
		Emit: func(dto SettingsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("settings", fixture.service)
	fixture.router.RegisterShell(fixture.shell)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.service.ServiceShutdown()
		_ = fixture.shell.ServiceShutdown()
	})
	return fixture
}

func loadEmptySettings(t *testing.T, fixture *settingsBindFixture) SettingsDTO {
	t.Helper()
	if _, err := fixture.shell.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, err := fixture.service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return waitForDTO(t, fixture, "the loaded settings", func(dto SettingsDTO) bool {
		return !dto.Loading
	})
}

// findeskFixtureCopy copies the findeskimport test fixture into a temp folder,
// as that package's tests do.
func findeskFixtureCopy(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "internal", "core", "findeskimport", "testdata", "findesk")
	destination := filepath.Join(t.TempDir(), "findesk")
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatalf("copy FinDesk fixture: %v", err)
	}
	return destination
}

func chooseFindeskFolder(t *testing.T, fixture *settingsBindFixture, dialogs *bindFileDialogs, folder string) {
	t.Helper()
	dialogs.setFindesk(folder, true)
	fixture.session.SetFindeskStartDir("/the/start/folder")
	if _, err := fixture.service.ImportFindesk(); err != nil {
		t.Fatalf("ImportFindesk: %v", err)
	}
	dto := waitForDTO(t, fixture, "the FinDesk passphrase dialog", func(dto SettingsDTO) bool {
		return dto.FindeskPassphraseOpen && dto.FindeskFolder != ""
	})
	if dto.FindeskFolder != folder {
		t.Fatalf("FinDesk folder = %q, want %q", dto.FindeskFolder, folder)
	}
	if dialogs.findeskStart != "/the/start/folder" {
		t.Fatalf("the picker received %q", dialogs.findeskStart)
	}
}

func observedSecrets(observed *[]any) []any {
	return append([]any(nil), (*observed)...)
}

// requireObservedWiped checks every secret the service created was wiped.
func requireObservedWiped(t *testing.T, observed *[]any, what string) {
	t.Helper()
	values := observedSecrets(observed)
	if len(values) == 0 {
		t.Fatalf("%s: the service did not expose a secret to observe", what)
	}
	for index, value := range values {
		switch secretValue := value.(type) {
		case []byte:
			if len(secretValue) == 0 {
				t.Fatalf("%s: secret slice %d is empty, the test needs non-empty input", what, index)
			}
			for _, character := range secretValue {
				if character != 0 {
					t.Fatalf("%s: secret slice %d was not wiped", what, index)
				}
			}
		case *secret.Passphrase:
			if err := secretValue.Use(func([]byte) error { return nil }); !errors.Is(err, secret.ErrWiped) {
				t.Fatalf("%s: passphrase %d was not wiped: %v", what, index, err)
			}
		default:
			t.Fatalf("%s: unexpected observed secret %T", what, value)
		}
	}
}

func TestARestorePassphraseIsWipedThroughTheService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fixture, dialogs, observed := newSettingsDataFixture(t)
		fixture.load(t)
		openRestorePassphraseDialog(t, fixture, dialogs, otherPassphraseBackup(t))

		if _, err := fixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		waitForDTO(t, fixture, "the staged restore", func(dto SettingsDTO) bool {
			return dto.HasPendingRestore
		})
		requireObservedWiped(t, observed, "the successful restore passphrase")
	})

	t.Run("wrong", func(t *testing.T) {
		fixture, dialogs, observed := newSettingsDataFixture(t)
		fixture.load(t)
		openRestorePassphraseDialog(t, fixture, dialogs, otherPassphraseBackup(t))

		if _, err := fixture.service.ConfirmRestorePassphrase("a wrong passphrase"); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		waitForDTO(t, fixture, "the wrong passphrase", func(dto SettingsDTO) bool {
			return dto.RestorePassphraseWrong && dto.RestorePassphraseOpen
		})
		requireObservedWiped(t, observed, "the wrong restore passphrase")
	})

	t.Run("failure", func(t *testing.T) {
		fixture, dialogs, observed := newSettingsDataFixture(t)
		fixture.load(t)
		target := otherPassphraseBackup(t)
		openRestorePassphraseDialog(t, fixture, dialogs, target)
		if err := os.WriteFile(target, []byte(corruptedZipMarker), 0o600); err != nil {
			t.Fatalf("corrupt the backup: %v", err)
		}

		if _, err := fixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		waitForDTO(t, fixture, "the failed restore", func(dto SettingsDTO) bool {
			return dto.ErrorText != ""
		})
		requireObservedWiped(t, observed, "the failed restore passphrase")
	})

	t.Run("refused while busy", func(t *testing.T) {
		fixture, dialogs, observed := newSettingsDataFixture(t)
		fixture.load(t)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		dialogs.onBackup = func() {
			entered <- struct{}{}
			<-release
		}
		// A failed assertion must still release the gated backup, or the
		// fixture cleanup waits for it forever.
		var releaseOnce sync.Once
		free := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(free)
		dialogs.setBackup(filepath.Join(t.TempDir(), "backup.zip"), true)
		if _, err := fixture.service.CreateBackup(); err != nil {
			t.Fatalf("CreateBackup: %v", err)
		}
		<-entered

		if _, err := fixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		requireObservedWiped(t, observed, "the refused restore passphrase")
		free()
		waitForValue(t, "the backup job", func() bool {
			return fixture.session.SettingsState().Message != nil
		})
	})

	t.Run("no open database", func(t *testing.T) {
		fixture, _, observed := newSettingsDataFixture(t)
		if _, err := fixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		requireObservedWiped(t, observed, "the restore passphrase without a database")
	})

	t.Run("no chosen backup", func(t *testing.T) {
		fixture, _, observed := newSettingsDataFixture(t)
		fixture.load(t)

		// The job publishes the settings twice: when it starts and when it
		// ends. Waiting for the second one guarantees the job wiped the slice,
		// so the assertion below never races the wipe.
		changes := make(chan struct{}, 8)
		remove := fixture.session.AddListener(func(change shell.Change) {
			if change.Page == "settings" {
				changes <- struct{}{}
			}
		})
		defer remove()
		if _, err := fixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
			t.Fatalf("ConfirmRestorePassphrase: %v", err)
		}
		for index := 0; index < 2; index++ {
			select {
			case <-changes:
			case <-time.After(10 * time.Second):
				t.Fatal("the restore job did not finish")
			}
		}
		requireObservedWiped(t, observed, "the restore passphrase without a chosen backup")
	})
}

func TestAFindeskPassphraseIsWipedThroughTheService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fixture := newEmptySettingsBindFixture(t)
		loadEmptySettings(t, fixture)
		dialogs := &bindFileDialogs{}
		fixture.session.SetFileDialogs(dialogs)
		observed := make([]any, 0)
		fixture.service.observeSecret = func(value any) { observed = append(observed, value) }
		chooseFindeskFolder(t, fixture, dialogs, findeskFixtureCopy(t))

		if _, err := fixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		dto := waitForDTO(t, fixture, "the import report", func(dto SettingsDTO) bool {
			return dto.FindeskReportOpen && !dto.FindeskBusy
		})
		if dto.FindeskPassphraseOpen {
			t.Fatal("a successful import kept the dialog open")
		}
		if dto.FindeskReportError {
			t.Fatalf("the right passphrase gave a failure report: %q", dto.FindeskReportText)
		}
		requireObservedWiped(t, &observed, "the successful FinDesk passphrase")
	})

	t.Run("wrong", func(t *testing.T) {
		fixture := newEmptySettingsBindFixture(t)
		loadEmptySettings(t, fixture)
		dialogs := &bindFileDialogs{}
		fixture.session.SetFileDialogs(dialogs)
		observed := make([]any, 0)
		fixture.service.observeSecret = func(value any) { observed = append(observed, value) }
		chooseFindeskFolder(t, fixture, dialogs, findeskFixtureCopy(t))

		if _, err := fixture.service.SubmitFindeskPassphrase("a wrong passphrase"); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		dto := waitForDTO(t, fixture, "the wrong passphrase", func(dto SettingsDTO) bool {
			return dto.FindeskPassphraseWrong
		})
		if !dto.FindeskPassphraseOpen || dto.FindeskReportOpen {
			t.Fatalf("a wrong passphrase must keep the dialog: %+v", dto)
		}
		requireObservedWiped(t, &observed, "the wrong FinDesk passphrase")
	})

	t.Run("failure", func(t *testing.T) {
		fixture := newEmptySettingsBindFixture(t)
		loadEmptySettings(t, fixture)
		dialogs := &bindFileDialogs{}
		fixture.session.SetFileDialogs(dialogs)
		observed := make([]any, 0)
		fixture.service.observeSecret = func(value any) { observed = append(observed, value) }
		folder := findeskFixtureCopy(t)
		chooseFindeskFolder(t, fixture, dialogs, folder)
		if err := os.RemoveAll(folder); err != nil {
			t.Fatalf("remove source: %v", err)
		}

		if _, err := fixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		waitForDTO(t, fixture, "the failed import", func(dto SettingsDTO) bool {
			return dto.FindeskReportOpen && dto.FindeskReportError
		})
		requireObservedWiped(t, &observed, "the failed FinDesk passphrase")
	})

	t.Run("refused while busy", func(t *testing.T) {
		fixture := newEmptySettingsBindFixture(t)
		loadEmptySettings(t, fixture)
		dialogs := &bindFileDialogs{}
		fixture.session.SetFileDialogs(dialogs)
		observed := make([]any, 0)
		fixture.service.observeSecret = func(value any) { observed = append(observed, value) }
		chooseFindeskFolder(t, fixture, dialogs, findeskFixtureCopy(t))

		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		dialogs.onBackup = func() {
			entered <- struct{}{}
			<-release
		}
		// A failed assertion must still release the gated backup, or the
		// fixture cleanup waits for it forever.
		var releaseOnce sync.Once
		free := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(free)
		dialogs.setBackup(filepath.Join(t.TempDir(), "backup.zip"), true)
		if _, err := fixture.service.CreateBackup(); err != nil {
			t.Fatalf("CreateBackup: %v", err)
		}
		<-entered

		if _, err := fixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		requireObservedWiped(t, &observed, "the refused FinDesk passphrase")
		free()
		waitForValue(t, "the backup job", func() bool {
			return fixture.session.SettingsState().Message != nil
		})
	})

	t.Run("no open database", func(t *testing.T) {
		fixture, _, observed := newSettingsDataFixture(t)
		if _, err := fixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		requireObservedWiped(t, observed, "the FinDesk passphrase without a database")
	})

	t.Run("no chosen folder", func(t *testing.T) {
		fixture := newEmptySettingsBindFixture(t)
		loadEmptySettings(t, fixture)
		observed := make([]any, 0)
		fixture.service.observeSecret = func(value any) { observed = append(observed, value) }
		if _, err := fixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
			t.Fatalf("SubmitFindeskPassphrase: %v", err)
		}
		requireObservedWiped(t, &observed, "the FinDesk passphrase without a folder")
	})
}

func TestNoDTOEventOrLogCarriesAPassphrase(t *testing.T) {
	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previousLogger)

	var records []string
	var recordsMu sync.Mutex
	record := func(dto SettingsDTO) {
		encoded, err := json.Marshal(dto)
		if err != nil {
			t.Errorf("marshal SettingsDTO: %v", err)
			return
		}
		recordsMu.Lock()
		records = append(records, string(encoded))
		recordsMu.Unlock()
	}

	restoreFixture, dialogs, _ := newSettingsDataFixture(t)
	restoreFixture.service.emit = func(dto SettingsDTO) {
		record(dto)
		restoreFixture.events <- dto
	}
	restoreFixture.load(t)
	openRestorePassphraseDialog(t, restoreFixture, dialogs, otherPassphraseBackup(t))
	if _, err := restoreFixture.service.ConfirmRestorePassphrase("a wrong passphrase"); err != nil {
		t.Fatalf("wrong restore passphrase: %v", err)
	}
	waitForDTO(t, restoreFixture, "the wrong passphrase", func(dto SettingsDTO) bool {
		return dto.RestorePassphraseWrong
	})
	if _, err := restoreFixture.service.ConfirmRestorePassphrase(restorePassphrase); err != nil {
		t.Fatalf("right restore passphrase: %v", err)
	}
	waitForDTO(t, restoreFixture, "the staged restore", func(dto SettingsDTO) bool {
		return dto.HasPendingRestore
	})

	findeskFixture := newEmptySettingsBindFixture(t)
	findeskDialogs := &bindFileDialogs{}
	findeskFixture.session.SetFileDialogs(findeskDialogs)
	findeskFixture.service.emit = func(dto SettingsDTO) {
		record(dto)
		findeskFixture.events <- dto
	}
	loadEmptySettings(t, findeskFixture)
	chooseFindeskFolder(t, findeskFixture, findeskDialogs, findeskFixtureCopy(t))
	if _, err := findeskFixture.service.SubmitFindeskPassphrase("a wrong passphrase"); err != nil {
		t.Fatalf("wrong FinDesk passphrase: %v", err)
	}
	waitForDTO(t, findeskFixture, "the wrong FinDesk passphrase", func(dto SettingsDTO) bool {
		return dto.FindeskPassphraseWrong
	})
	if _, err := findeskFixture.service.SubmitFindeskPassphrase(findeskPassphrase); err != nil {
		t.Fatalf("right FinDesk passphrase: %v", err)
	}
	waitForDTO(t, findeskFixture, "the import report", func(dto SettingsDTO) bool {
		return dto.FindeskReportOpen && !dto.FindeskBusy
	})

	recordsMu.Lock()
	haystack := strings.Join(records, "\n")
	recordsMu.Unlock()
	for _, forbidden := range []string{restorePassphrase, findeskPassphrase, "a wrong passphrase"} {
		if strings.Contains(haystack, forbidden) {
			t.Fatalf("a DTO or event carries %q", forbidden)
		}
		if strings.Contains(logOutput.String(), forbidden) {
			t.Fatalf("a log line carries %q", forbidden)
		}
	}
}
