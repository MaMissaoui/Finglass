package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/findeskimport"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const otherPassphrase = "another good passphrase"

// waitForSettingsState polls the published Settings state until predicate
// holds.
func waitForSettingsState(
	t *testing.T,
	session *Session,
	description string,
	predicate func(settingsview.SettingsState) bool,
) settingsview.SettingsState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := session.SettingsState()
		if predicate(state) {
			return state
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", description, state)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitForFindeskState polls the published FinDesk state until predicate holds.
func waitForFindeskState(
	t *testing.T,
	session *Session,
	description string,
	predicate func(findesk.FindeskImportState) bool,
) findesk.FindeskImportState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := session.FindeskState()
		if predicate(state) {
			return state
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", description, state)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func containsCategory(state settingsview.SettingsState, name string) bool {
	for _, category := range state.Categories {
		if category.Name == name {
			return true
		}
	}
	return false
}

// createBackupThroughDialog writes a backup of the open profile through the
// fake dialog and returns its path.
func createBackupThroughDialog(t *testing.T, session *Session) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "finglass-backup.zip")
	dialogs := &fakeFileDialogs{backupPath: target, backupResult: true}
	session.SetFileDialogs(dialogs)
	session.SettingsCreateBackup()
	waitForSettingsState(t, session, "the backup", func(state settingsview.SettingsState) bool {
		return state.Message != nil && strings.Contains(*state.Message, "Backup saved")
	})
	if dialogs.backupCalls != 1 {
		t.Fatalf("the save dialog was called %d times", dialogs.backupCalls)
	}
	if dialogs.backupTitle != i18n.Text(i18n.En, i18n.SaveBackup) {
		t.Fatalf("backup dialog title = %q", dialogs.backupTitle)
	}
	if !strings.HasPrefix(dialogs.backupName, "finglass-backup-") ||
		!strings.HasSuffix(dialogs.backupName, ".zip") {
		t.Fatalf("backup default name = %q", dialogs.backupName)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("backup file: %v", err)
	}
	return target
}

func TestBackupRestoreAndReopenBringTheDataBack(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	target := createBackupThroughDialog(t, session)

	// Change the data after the backup, so a restore is visible.
	session.SettingsOpenAddCategory()
	session.SettingsSaveCategory("Snacks", "#123456", "", false, false)
	waitForSettingsState(t, session, "the extra category", func(state settingsview.SettingsState) bool {
		return len(state.Categories) == 3
	})

	dialogs := &fakeFileDialogs{pickBackup: target, pickBackupResult: true}
	session.SetFileDialogs(dialogs)
	session.SettingsRestoreBackup()
	state := waitForSettingsState(t, session, "the restore candidate", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate != nil
	})
	if dialogs.pickBackupTitle != i18n.Text(i18n.En, i18n.RestoreBackup) {
		t.Fatalf("restore dialog title = %q", dialogs.pickBackupTitle)
	}
	if state.RestoreCandidate.Backup != target {
		t.Fatalf("the candidate is %q, want %q", state.RestoreCandidate.Backup, target)
	}

	session.SettingsConfirmRestore()
	waitForSettingsState(t, session, "the staged restore", func(state settingsview.SettingsState) bool {
		return state.HasPendingRestore
	})
	if session.SettingsState().RestoreCandidate != nil {
		t.Fatal("the confirmation dialog stayed open")
	}

	if err := session.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	restarted, _, refusal := Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("restart refused: %v", refusal)
	}
	t.Cleanup(func() { _ = restarted.Shutdown() })
	if restarted.RestoredFrom() == nil {
		t.Fatal("the restart did not record the pre-restore folder")
	}
	if err := restarted.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock after restart: %v", err)
	}
	waitForJobs(t, restarted)
	events, remove := listenForChanges(restarted)
	defer remove()
	restarted.Navigate("settings")
	state = waitForSettings(t, restarted, events, "the restored settings", func(state settingsview.SettingsState) bool {
		return !state.IsLoading && len(state.Categories) == 2
	})
	if containsCategory(state, "Snacks") {
		t.Fatal("the post-backup category survived the restore")
	}
	if !containsCategory(state, "Food") {
		t.Fatal("the backup's category is missing")
	}
}

func TestCancellingEachBackupDialogAndAPendingRestoreChangesNothing(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	// A cancelled save dialog writes nothing.
	dialogs := &fakeFileDialogs{backupResult: false}
	session.SetFileDialogs(dialogs)
	before := session.SettingsState()
	session.SettingsCreateBackup()
	waitForJobs(t, session)
	if dialogs.backupCalls != 1 {
		t.Fatalf("the save dialog was called %d times", dialogs.backupCalls)
	}
	afterCreate := session.SettingsState()
	if afterCreate.Message != nil || len(afterCreate.Categories) != len(before.Categories) {
		t.Fatalf("a cancelled save changed the state: %+v", afterCreate)
	}

	// A cancelled open dialog opens no confirmation.
	target := createBackupThroughDialog(t, session)
	dialogs = &fakeFileDialogs{pickBackup: target, pickBackupResult: false}
	session.SetFileDialogs(dialogs)
	session.SettingsRestoreBackup()
	waitForJobs(t, session)
	if session.SettingsState().RestoreCandidate != nil {
		t.Fatal("a cancelled open dialog opened the confirmation")
	}

	// A cancelled confirmation drops the candidate only.
	dialogs.pickBackupResult = true
	session.SettingsRestoreBackup()
	waitForSettingsState(t, session, "the restore candidate", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate != nil
	})
	session.SettingsCancelRestoreChoice()
	waitForSettingsState(t, session, "the cancelled choice", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate == nil && !state.HasPendingRestore
	})

	// A cancelled pending restore leaves no staged folder and no data change.
	dialogs.pickBackupResult = true
	session.SettingsRestoreBackup()
	waitForSettingsState(t, session, "the second restore candidate", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate != nil
	})
	session.SettingsConfirmRestore()
	waitForSettingsState(t, session, "the staged restore", func(state settingsview.SettingsState) bool {
		return state.HasPendingRestore
	})
	dir := defaultDataDir(t, appPaths)
	if !backup.NewService(dir, "").HasPendingRestore() {
		t.Fatal("the restore was not staged on disk")
	}
	session.SettingsCancelPendingRestore()
	waitForSettingsState(t, session, "the cancelled pending restore", func(state settingsview.SettingsState) bool {
		return !state.HasPendingRestore
	})
	if backup.NewService(dir, "").HasPendingRestore() {
		t.Fatal("the staged restore is still on disk")
	}
	if len(session.SettingsState().Categories) != 2 {
		t.Fatal("a cancelled pending restore changed the data")
	}

	// Cancelling the passphrase dialog of a foreign backup changes nothing.
	other := filepath.Join(t.TempDir(), "other.zip")
	writeOtherPassphraseBackup(t, other)
	chooseOtherPassphraseBackup(t, session, other)
	session.SettingsCancelRestorePassphrase()
	waitForSettingsState(t, session, "the cancelled passphrase dialog", func(state settingsview.SettingsState) bool {
		return state.RestorePassphraseFor == nil && !state.HasPendingRestore
	})
	if len(session.SettingsState().Categories) != 2 {
		t.Fatal("cancelling the passphrase dialog changed the data")
	}
}

// writeOtherPassphraseBackup creates a separate profile database under
// otherPassphrase, adds a category and writes its backup to target.
func writeOtherPassphraseBackup(t *testing.T, target string) {
	t.Helper()
	otherPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	database, err := unlock.Create(
		otherPaths,
		"default",
		[]byte(otherPassphrase),
		[]byte(otherPassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create other database: %v", err)
	}
	if _, err := categories.Create(context.Background(), database, model.NewCategory{
		Name:  "Imported",
		Color: "#445566",
	}); err != nil {
		t.Fatalf("create other category: %v", err)
	}
	dir, err := profiles.DataDir(otherPaths, "")
	if err != nil {
		t.Fatalf("other data dir: %v", err)
	}
	if _, err := backup.NewService(dir, "").CreateBackup(database, target); err != nil {
		t.Fatalf("create other backup: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close other database: %v", err)
	}
}

func TestARestoreUnderAnotherPassphraseAsksKeepsAndThenStages(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	target := filepath.Join(t.TempDir(), "other.zip")
	writeOtherPassphraseBackup(t, target)

	dialogs := &fakeFileDialogs{pickBackup: target, pickBackupResult: true}
	session.SetFileDialogs(dialogs)
	session.SettingsRestoreBackup()
	waitForSettingsState(t, session, "the restore candidate", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate != nil
	})
	session.SettingsConfirmRestore()
	state := waitForSettingsState(t, session, "the passphrase dialog", func(state settingsview.SettingsState) bool {
		return state.RestorePassphraseFor != nil && !state.RestorePassphraseWrong
	})
	if !strings.HasSuffix(*state.RestorePassphraseFor, "other.zip") {
		t.Fatalf("passphrase candidate = %q", *state.RestorePassphraseFor)
	}

	// A wrong passphrase keeps the dialog open and sets the wrong flag.
	session.SettingsConfirmRestorePassphrase([]byte("not the passphrase"))
	state = waitForSettingsState(t, session, "the wrong passphrase", func(state settingsview.SettingsState) bool {
		return state.RestorePassphraseWrong && state.RestorePassphraseFor != nil
	})
	if state.HasPendingRestore {
		t.Fatal("a wrong passphrase staged the restore")
	}

	// The right passphrase stages it and clears the wrong flag.
	session.SettingsConfirmRestorePassphrase([]byte(otherPassphrase))
	state = waitForSettingsState(t, session, "the staged restore", func(state settingsview.SettingsState) bool {
		return state.HasPendingRestore
	})
	if state.RestorePassphraseFor != nil || state.RestorePassphraseWrong {
		t.Fatalf("the passphrase dialog stayed open: %+v", state)
	}
}

func TestARestorePassphraseIsWipedOnEveryPath(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		appPaths := testPaths(t)
		seedSettingsDatabase(t, appPaths)
		session := newLoadedSettingsSession(t, appPaths)
		target := filepath.Join(t.TempDir(), "other.zip")
		writeOtherPassphraseBackup(t, target)
		chooseOtherPassphraseBackup(t, session, target)

		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		waitForSettingsState(t, session, "the staged restore", func(state settingsview.SettingsState) bool {
			return state.HasPendingRestore
		})
		requireZeroed(t, passphrase, "the successful restore passphrase")
	})

	t.Run("wrong", func(t *testing.T) {
		appPaths := testPaths(t)
		seedSettingsDatabase(t, appPaths)
		session := newLoadedSettingsSession(t, appPaths)
		target := filepath.Join(t.TempDir(), "other.zip")
		writeOtherPassphraseBackup(t, target)
		chooseOtherPassphraseBackup(t, session, target)

		passphrase := []byte("a wrong passphrase")
		session.SettingsConfirmRestorePassphrase(passphrase)
		waitForSettingsState(t, session, "the wrong passphrase", func(state settingsview.SettingsState) bool {
			return state.RestorePassphraseWrong
		})
		requireZeroed(t, passphrase, "the wrong restore passphrase")
	})

	t.Run("failure", func(t *testing.T) {
		appPaths := testPaths(t)
		seedSettingsDatabase(t, appPaths)
		session := newLoadedSettingsSession(t, appPaths)
		corrupt := filepath.Join(t.TempDir(), "corrupt.zip")
		if err := os.WriteFile(corrupt, []byte("not a zip"), 0o600); err != nil {
			t.Fatalf("write corrupt backup: %v", err)
		}
		// ChooseRestore refuses the file, so set the passphrase target by hand
		// through the real state: a candidate that Inspect accepts is needed.
		target := filepath.Join(t.TempDir(), "other.zip")
		writeOtherPassphraseBackup(t, target)
		chooseOtherPassphraseBackup(t, session, target)
		// Replace the staged file behind the state's back: the session still
		// owns the passphrase slice and must wipe it when the stage fails.
		session.mu.Lock()
		session.settings.RestorePassphraseFor = &corrupt
		session.mu.Unlock()

		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		waitForSettingsState(t, session, "the failed restore", func(state settingsview.SettingsState) bool {
			return state.Error != nil
		})
		requireZeroed(t, passphrase, "the failed restore passphrase")
	})

	t.Run("refused while busy", func(t *testing.T) {
		appPaths := testPaths(t)
		createClosedTestDatabase(t, appPaths, "default")
		session := newLoadedSettingsSession(t, appPaths)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		session.pageLoadGate = func(page string) {
			if page != "settings" {
				return
			}
			entered <- struct{}{}
			<-release
		}
		session.SettingsSaveTaxKeywords("first")
		<-entered

		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		requireZeroed(t, passphrase, "the refused restore passphrase")
		close(release)
		waitForJobs(t, session)
	})

	t.Run("no open database", func(t *testing.T) {
		session := newTestSession(t, testPaths(t))
		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		requireZeroed(t, passphrase, "the restore passphrase without a database")
	})

	t.Run("no chosen backup", func(t *testing.T) {
		appPaths := testPaths(t)
		createClosedTestDatabase(t, appPaths, "default")
		session := newLoadedSettingsSession(t, appPaths)
		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		waitForJobs(t, session)
		requireZeroed(t, passphrase, "the restore passphrase without a chosen backup")
	})

	t.Run("teardown during the job", func(t *testing.T) {
		appPaths := testPaths(t)
		seedSettingsDatabase(t, appPaths)
		session := newLoadedSettingsSession(t, appPaths)
		target := filepath.Join(t.TempDir(), "other.zip")
		writeOtherPassphraseBackup(t, target)
		chooseOtherPassphraseBackup(t, session, target)

		entered := make(chan chan struct{}, 1)
		session.pageLoadGate = func(page string) {
			if page != "settings" {
				return
			}
			gate := make(chan struct{})
			entered <- gate
			<-gate
		}
		passphrase := []byte(otherPassphrase)
		session.SettingsConfirmRestorePassphrase(passphrase)
		gate := <-entered
		generation := session.jobs.Generation()
		done := make(chan struct{})
		session.jobs.WhenIdle(generation, func() { close(done) })
		session.Teardown(startup.Startup{})
		close(gate)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the restore job did not end")
		}
		requireZeroed(t, passphrase, "the torn-down restore passphrase")
		if session.SettingsState().HasPendingRestore {
			t.Fatal("a torn-down restore was published")
		}
	})
}

// chooseOtherPassphraseBackup opens the restore confirmation for target and
// confirms it, so the passphrase dialog is open.
func chooseOtherPassphraseBackup(t *testing.T, session *Session, target string) {
	t.Helper()
	session.SetFileDialogs(&fakeFileDialogs{pickBackup: target, pickBackupResult: true})
	session.SettingsRestoreBackup()
	waitForSettingsState(t, session, "the restore candidate", func(state settingsview.SettingsState) bool {
		return state.RestoreCandidate != nil
	})
	session.SettingsConfirmRestore()
	waitForSettingsState(t, session, "the passphrase dialog", func(state settingsview.SettingsState) bool {
		return state.RestorePassphraseFor != nil
	})
}

func requireZeroed(t *testing.T, value []byte, what string) {
	t.Helper()
	nonEmpty := false
	for _, character := range value {
		if character != 0 {
			nonEmpty = true
			break
		}
	}
	if nonEmpty {
		t.Fatalf("%s was not wiped: %v", what, value)
	}
}

// findeskFixtureCopy copies the findeskimport test fixture into a temp folder,
// as that package's own tests do.
func findeskFixtureCopy(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "..", "internal", "core", "findeskimport", "testdata", "findesk")
	destination := filepath.Join(t.TempDir(), "findesk")
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatalf("copy FinDesk fixture: %v", err)
	}
	return destination
}

const findeskFixturePassphrase = "fixture passphrase 1"

func newEmptySettingsSession(t *testing.T, appPaths paths.AppPaths) *Session {
	t.Helper()
	createClosedTestDatabase(t, appPaths, "default")
	return newLoadedSettingsSession(t, appPaths)
}

func chooseFindeskFolder(t *testing.T, session *Session, folder string, startDir string) *fakeFileDialogs {
	t.Helper()
	dialogs := &fakeFileDialogs{
		findeskFolder: folder,
		findeskResult: true,
	}
	session.SetFileDialogs(dialogs)
	session.SetFindeskStartDir(startDir)
	session.SettingsImportFindesk()
	waitForFindeskState(t, session, "the chosen FinDesk folder", func(state findesk.FindeskImportState) bool {
		return state.SourceDir != nil
	})
	if !session.SettingsDialog().FindeskPassphraseOpen {
		t.Fatal("the passphrase dialog did not open")
	}
	if dialogs.findeskCalls != 1 {
		t.Fatalf("the folder dialog was called %d times", dialogs.findeskCalls)
	}
	if dialogs.findeskTitle != i18n.Text(i18n.En, i18n.FinDeskImportChooseFolder) {
		t.Fatalf("folder dialog title = %q", dialogs.findeskTitle)
	}
	if dialogs.findeskStart != startDir {
		t.Fatalf("the picker received %q, want %q", dialogs.findeskStart, startDir)
	}
	return dialogs
}

func findeskPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte(value), secret.Unlock)
	if err != nil {
		t.Fatalf("build passphrase: %v", err)
	}
	return passphrase
}

func TestFindeskImportWrongThenRightPassphraseHoldsTheProfileBusy(t *testing.T) {
	appPaths := testPaths(t)
	session := newEmptySettingsSession(t, appPaths)
	folder := findeskFixtureCopy(t)
	chooseFindeskFolder(t, session, folder, "/a/start/folder")

	session.SettingsSubmitFindeskPassphrase(findeskPassphrase(t, "wrong passphrase"))
	state := waitForFindeskState(t, session, "the wrong passphrase", func(state findesk.FindeskImportState) bool {
		return state.PassphraseWrong && !state.IsImporting
	})
	if !session.SettingsDialog().FindeskPassphraseOpen {
		t.Fatal("the wrong passphrase closed the dialog")
	}
	if state.SourceDir == nil || state.Report != nil {
		t.Fatalf("wrong passphrase state = %+v", state)
	}

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.findeskImportGate = func() {
		if !session.profileBusy.Active() {
			t.Error("the import runs outside the profile busy section")
		}
		entered <- struct{}{}
		<-release
	}
	passphrase := findeskPassphrase(t, findeskFixturePassphrase)
	session.SettingsSubmitFindeskPassphrase(passphrase)
	<-entered
	if !session.profileBusy.Active() {
		t.Fatal("the profile busy section is not active during the import")
	}
	close(release)

	state = waitForFindeskState(t, session, "the import report", func(state findesk.FindeskImportState) bool {
		return state.Report != nil && !state.IsImporting
	})
	if state.PassphraseWrong || state.Error != nil {
		t.Fatalf("import state = %+v", state)
	}
	if session.profileBusy.Active() {
		t.Fatal("the profile busy section did not end")
	}
	if session.SettingsDialog().FindeskPassphraseOpen {
		t.Fatal("a successful import kept the passphrase dialog open")
	}
	if session.CurrentScreen() != "settings" {
		t.Fatalf("the reload left the screen on %q", session.CurrentScreen())
	}
	if text := state.ReportText(); text == nil || *text == "" {
		t.Fatal("the report text is empty")
	}

	// The report closes without forgetting the import.
	session.SettingsCloseFindeskReport()
	waitForFindeskState(t, session, "the closed report", func(state findesk.FindeskImportState) bool {
		return state.Report == nil && state.Error == nil
	})
	if state := session.FindeskState(); state.SourceDir == nil {
		t.Fatal("closing the report forgot the folder")
	}
}

func TestFindeskImportCancelAndNotAFolder(t *testing.T) {
	appPaths := testPaths(t)
	session := newEmptySettingsSession(t, appPaths)
	folder := findeskFixtureCopy(t)
	chooseFindeskFolder(t, session, folder, "")

	session.SettingsCancelFindeskPassphrase()
	state := waitForFindeskState(t, session, "the cancelled folder", func(state findesk.FindeskImportState) bool {
		return state.SourceDir == nil
	})
	if state.PassphraseWrong || state.Report != nil || state.Error != nil {
		t.Fatalf("cancelled state = %+v", state)
	}
	if session.SettingsDialog().FindeskPassphraseOpen {
		t.Fatal("cancel kept the passphrase dialog open")
	}

	// A folder that is not a FinDesk folder is refused with the localized text.
	session.SetFileDialogs(&fakeFileDialogs{
		findeskFolder: t.TempDir(),
		findeskResult: true,
	})
	session.SettingsImportFindesk()
	waitForSettingsState(t, session, "the not-a-folder error", func(state settingsview.SettingsState) bool {
		return state.Error != nil
	})
	if got := *session.SettingsState().Error; got != i18n.Text(i18n.En, i18n.ErrorFinDeskNotAFolder) {
		t.Fatalf("not-a-folder error = %q", got)
	}
	if session.FindeskState().SourceDir != nil {
		t.Fatal("a foreign folder was remembered")
	}

	// The folder dialog may be cancelled without a change.
	session.SetFileDialogs(&fakeFileDialogs{findeskResult: false})
	session.SettingsImportFindesk()
	waitForJobs(t, session)
	if session.FindeskState().SourceDir != nil || session.SettingsDialog().FindeskPassphraseOpen {
		t.Fatal("a cancelled folder dialog changed the FinDesk state")
	}
}

func TestAFindeskPassphraseIsWipedOnEveryPath(t *testing.T) {
	newChosen := func(t *testing.T) *Session {
		t.Helper()
		session := newEmptySettingsSession(t, testPaths(t))
		chooseFindeskFolder(t, session, findeskFixtureCopy(t), "")
		return session
	}

	t.Run("success", func(t *testing.T) {
		session := newChosen(t)
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		waitForFindeskState(t, session, "the import report", func(state findesk.FindeskImportState) bool {
			return state.Report != nil
		})
		requirePassphraseWiped(t, passphrase, "the successful FinDesk passphrase")
	})

	t.Run("wrong", func(t *testing.T) {
		session := newChosen(t)
		passphrase := findeskPassphrase(t, "wrong passphrase")
		session.SettingsSubmitFindeskPassphrase(passphrase)
		waitForFindeskState(t, session, "the wrong passphrase", func(state findesk.FindeskImportState) bool {
			return state.PassphraseWrong
		})
		requirePassphraseWiped(t, passphrase, "the wrong FinDesk passphrase")
	})

	t.Run("failure", func(t *testing.T) {
		session := newChosen(t)
		// Break the source after choosing it: the import fails without a
		// passphrase error.
		folder := *session.FindeskState().SourceDir
		if err := os.RemoveAll(folder); err != nil {
			t.Fatalf("remove source: %v", err)
		}
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		waitForFindeskState(t, session, "the failed import", func(state findesk.FindeskImportState) bool {
			return state.Error != nil
		})
		requirePassphraseWiped(t, passphrase, "the failed FinDesk passphrase")
	})

	t.Run("refused while busy", func(t *testing.T) {
		session := newChosen(t)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		session.pageLoadGate = func(page string) {
			if page != "settings" {
				return
			}
			entered <- struct{}{}
			<-release
		}
		session.SettingsSaveTaxKeywords("first")
		<-entered

		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		stuck := session.FindeskState().IsImporting
		close(release)
		waitForJobs(t, session)
		requirePassphraseWiped(t, passphrase, "the refused FinDesk passphrase")
		if stuck {
			t.Fatal("a refused FinDesk import left the page marked as importing")
		}
	})

	t.Run("no open database", func(t *testing.T) {
		session := newTestSession(t, testPaths(t))
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		requirePassphraseWiped(t, passphrase, "the FinDesk passphrase without a database")
	})

	t.Run("no chosen folder", func(t *testing.T) {
		appPaths := testPaths(t)
		session := newEmptySettingsSession(t, appPaths)
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		requirePassphraseWiped(t, passphrase, "the FinDesk passphrase without a folder")
	})

	t.Run("a Settings load during the import", func(t *testing.T) {
		session := newChosen(t)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		session.findeskImportGate = func() {
			entered <- struct{}{}
			<-release
		}
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		<-entered
		// Opening Settings again while the import runs must not drop the
		// import's result.
		session.SettingsLoad()
		close(release)
		state := waitForFindeskState(t, session, "the import report after a load", func(state findesk.FindeskImportState) bool {
			return state.Report != nil
		})
		waitForJobs(t, session)
		if state.IsImporting || session.FindeskState().IsImporting {
			t.Fatal("a load during the import left the page marked as importing")
		}
		if session.SettingsDialog().FindeskPassphraseOpen {
			t.Fatal("a load during the import kept the passphrase dialog open")
		}
		requirePassphraseWiped(t, passphrase, "the FinDesk passphrase of an import overlapped by a load")
	})

	t.Run("teardown during the job", func(t *testing.T) {
		session := newChosen(t)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		session.findeskImportGate = func() {
			entered <- struct{}{}
			<-release
		}
		passphrase := findeskPassphrase(t, findeskFixturePassphrase)
		session.SettingsSubmitFindeskPassphrase(passphrase)
		<-entered
		generation := session.jobs.Generation()
		done := make(chan struct{})
		session.jobs.WhenIdle(generation, func() { close(done) })
		session.Teardown(startup.Startup{})
		close(release)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the import job did not end")
		}
		requirePassphraseWiped(t, passphrase, "the torn-down FinDesk passphrase")
		if state := session.FindeskState(); state.Report != nil {
			t.Fatal("a torn-down import published its report")
		}
	})
}

func requirePassphraseWiped(t *testing.T, passphrase *secret.Passphrase, what string) {
	t.Helper()
	if err := passphrase.Use(func([]byte) error { return nil }); !errors.Is(err, secret.ErrWiped) {
		t.Fatalf("%s was not wiped: %v", what, err)
	}
}

func TestProfileDialogsCreateRenameAndRefuseTheOpenProfile(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newLoadedSettingsSession(t, appPaths)

	session.SettingsOpenNewProfile()
	dialog := session.SettingsDialog()
	if !dialog.ProfileOpen || dialog.ProfileIndex != -1 ||
		dialog.ProfileTitle != i18n.Text(i18n.En, i18n.NewProfile) {
		t.Fatalf("new profile dialog = %+v", dialog)
	}

	session.SettingsSaveProfile("")
	waitForProfileError(t, session)
	if dialog := session.SettingsDialog(); !dialog.ProfileOpen || dialog.ProfileError == "" {
		t.Fatalf("a blank name did not stay in the dialog: %+v", dialog)
	}
	if got := session.SettingsDialog().ProfileError; got != i18n.Text(i18n.En, i18n.ErrorNameRequired) {
		t.Fatalf("blank name error = %q", got)
	}

	session.SettingsCloseProfileDialog()
	session.SettingsOpenNewProfile()
	session.SettingsSaveProfile("Work")
	state := waitForSettingsState(t, session, "the created profile", func(state settingsview.SettingsState) bool {
		return len(state.Profiles) == 2
	})
	if dialog := session.SettingsDialog(); dialog.ProfileOpen || dialog.ProfileError != "" {
		t.Fatalf("the created profile kept the dialog: %+v", dialog)
	}
	workIndex := -1
	defaultIndex := -1
	for index, profile := range state.Profiles {
		switch profile.Name {
		case "Work":
			workIndex = index
		case "Personal":
			defaultIndex = index
		}
	}
	if workIndex < 0 || defaultIndex < 0 {
		t.Fatalf("profiles = %+v", state.Profiles)
	}

	// Renaming validates through the localized text.
	session.SettingsOpenRenameProfile(workIndex)
	if dialog := session.SettingsDialog(); dialog.ProfileName != "Work" ||
		dialog.ProfileTitle != i18n.Format(i18n.En, i18n.RenameProfile, i18n.Str("Work")) {
		t.Fatalf("rename dialog = %+v", dialog)
	}
	session.SettingsSaveProfile("")
	waitForSettingsState(t, session, "the rename error", func(settingsview.SettingsState) bool {
		return session.SettingsDialog().ProfileError != ""
	})
	if got := session.SettingsDialog().ProfileError; got != i18n.Text(i18n.En, i18n.ErrorNameRequired) {
		t.Fatalf("rename blank error = %q", got)
	}
	session.SettingsSaveProfile("Business")
	waitForSettingsState(t, session, "the renamed profile", func(state settingsview.SettingsState) bool {
		for _, profile := range state.Profiles {
			if profile.Name == "Business" {
				return true
			}
		}
		return false
	})

	// The default profile has no slug, so a confirmation cannot reach it; Rust
	// returns silently there.
	session.SettingsRequestDeleteProfile(defaultIndex)
	if dialog := session.SettingsDialog(); !dialog.ProfileDeleteOpen ||
		!strings.Contains(dialog.ProfileDeleteTitle, "Personal") {
		t.Fatalf("delete dialog = %+v", dialog)
	}
	session.SettingsConfirmDeleteProfile()
	waitForJobs(t, session)
	if session.SettingsDialog().ProfileDeleteOpen {
		t.Fatal("the delete dialog stayed open")
	}
	if len(session.SettingsState().Profiles) != 2 {
		t.Fatal("the default profile was deleted")
	}

	// Cancelling the confirmation changes nothing.
	session.SettingsRequestDeleteProfile(workIndex)
	session.SettingsCancelDeleteProfile()
	if session.SettingsDialog().ProfileDeleteOpen {
		t.Fatal("cancel kept the delete dialog open")
	}

	// The open named profile is refused with the localized error.
	workDatabase, err := unlock.Create(
		appPaths,
		"work",
		[]byte(testPassphrase),
		[]byte(testPassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create Work database: %v", err)
	}
	if err := workDatabase.Close(); err != nil {
		t.Fatalf("close Work database: %v", err)
	}
	if err := session.SwitchProfile(workIndex); err != nil {
		t.Fatalf("switch to Work: %v", err)
	}
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock Work: %v", err)
	}
	waitForJobs(t, session)
	events, remove := listenForChanges(session)
	defer remove()
	session.Navigate("settings")
	state = waitForSettings(t, session, events, "the Work settings", func(state settingsview.SettingsState) bool {
		return !state.IsLoading && len(state.Profiles) == 2
	})
	workIndex = -1
	for index, profile := range state.Profiles {
		if profile.Slug == "work" {
			workIndex = index
		}
	}
	session.SettingsRequestDeleteProfile(workIndex)
	session.SettingsConfirmDeleteProfile()
	waitForSettingsState(t, session, "the refused delete", func(state settingsview.SettingsState) bool {
		return state.Error != nil
	})
	if len(session.SettingsState().Profiles) != 2 {
		t.Fatal("the open profile was deleted")
	}
	if session.SettingsDialog().ProfileDeleteOpen {
		t.Fatal("the delete dialog stayed open")
	}
}

func waitForProfileError(t *testing.T, session *Session) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if session.SettingsDialog().ProfileError != "" {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for the profile dialog error: %+v", session.SettingsDialog())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDeletingAProfileForgetsExactlyItsRememberedPins(t *testing.T) {
	appPaths := testPaths(t)
	store := banksync.NewMemoryCredentialStore()
	gateway := newBankingTestGateway()
	gateway.askTan = false
	gateway.setSnapshots([]banksync.AccountSnapshot{bankingSnapshot()})

	// Default profile: sync once with "remember PIN", the code path a real
	// sync uses to store one.
	rememberPinThroughSync(t, appPaths, "", store, gateway)

	// A second profile with its own database and remembered PIN.
	work, err := profiles.Create(appPaths, "Work")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	rememberPinThroughSync(t, appPaths, work.Slug, store, gateway)

	defaultDir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("default dir: %v", err)
	}
	workDir, err := profiles.ProfileDir(appPaths, work.Slug)
	if err != nil {
		t.Fatalf("work dir: %v", err)
	}
	if store.Len() != 2 {
		t.Fatalf("credential store holds %d entries, want 2", store.Len())
	}

	session := newSession(
		appPaths,
		store,
		func() time.Time { return testNow },
		func() civil.Date { return testToday() },
		testGetenv,
		i18n.En,
	)
	t.Cleanup(func() { _ = session.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	events, remove := listenForChanges(session)
	defer remove()
	session.Navigate("settings")
	state := waitForSettings(t, session, events, "the loaded profiles", func(state settingsview.SettingsState) bool {
		return !state.IsLoading && len(state.Profiles) == 2
	})

	workIndex := -1
	for index, profile := range state.Profiles {
		if profile.Slug == work.Slug {
			workIndex = index
		}
	}
	if workIndex < 0 {
		t.Fatalf("the Work profile is missing from %+v", state.Profiles)
	}
	session.SettingsRequestDeleteProfile(workIndex)
	session.SettingsConfirmDeleteProfile()
	waitForSettings(t, session, events, "the deleted profile", func(state settingsview.SettingsState) bool {
		return len(state.Profiles) == 1
	})

	defaultKey := banksync.PinKey(defaultDir, 1)
	if !store.Contains(defaultKey) {
		t.Fatal("deleting Work forgot the default profile's PIN")
	}
	if store.Contains(banksync.PinKey(workDir, 1)) {
		t.Fatal("deleting Work kept its remembered PIN")
	}
	if _, err := os.Stat(banksync.PinMarkerPath(workDir, 1)); err == nil {
		t.Fatal("the deleted profile's PIN marker is still there")
	}
	if _, err := os.Stat(banksync.PinMarkerPath(defaultDir, 1)); err != nil {
		t.Fatalf("the default profile's PIN marker: %v", err)
	}
}

// rememberPinThroughSync creates slug's database and bank access, then runs one
// shell-equivalent sync that stores the PIN. Nothing here builds a credential
// key by hand: the sync's own savePin path does.
func rememberPinThroughSync(
	t *testing.T,
	appPaths paths.AppPaths,
	slug string,
	store *banksync.MemoryCredentialStore,
	gateway *bankingTestGateway,
) {
	t.Helper()
	database, err := unlock.Create(
		appPaths,
		startup.SlugKey(slugOrNil(slug)),
		[]byte(testPassphrase),
		[]byte(testPassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create %q database: %v", slug, err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Fatalf("close %q database: %v", slug, err)
		}
	}()

	bic := "COLSDE33XXX"
	created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: "Sparkasse",
		BLZ:      "37040044",
		BIC:      &bic,
		UserID:   "user",
	})
	if err != nil {
		t.Fatalf("create bank: %v", err)
	}
	serverURL := "https://fints.example/pintan"
	if _, err := bankconnections.UpdateConnection(
		context.Background(),
		database,
		created.ID,
		model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("update bank: %v", err)
	}
	bank, ok, err := bankconnections.GetItem(context.Background(), database, created.ID)
	if err != nil || !ok {
		t.Fatalf("get bank: ok=%t err=%v", ok, err)
	}
	dir, err := profiles.DataDir(appPaths, slug)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	client := banksync.NewFinTsClient(gateway, dir, "test-product", store)
	if _, err := client.FetchAll(
		context.Background(),
		database,
		bank,
		rememberPinInteraction{},
		func(string) *civil.Date { return nil },
		civil.FromTime(testNow),
	); err != nil {
		t.Fatalf("sync for %q: %v", slug, err)
	}
}

// rememberPinInteraction answers the PIN dialog with "Remember PIN" ticked and
// never asks for a TAN.
type rememberPinInteraction struct{}

func (rememberPinInteraction) Pin(string, string, bool) *banksync.PinEntry {
	return pointerTo(banksync.NewPinEntry(secret.NewText([]byte("24680")), true))
}

func (rememberPinInteraction) SelectTanMethod([]banksync.TanMethod) *string {
	return nil
}

func (rememberPinInteraction) SelectTanMedium([]string) *string {
	return nil
}

func (rememberPinInteraction) Tan(banksync.TanChallenge) *secret.Text {
	return secret.NewText([]byte("123456"))
}

func (rememberPinInteraction) ConfirmDecoupled(string) bool {
	return true
}

func (rememberPinInteraction) Info(string) {}

func pointerTo[T any](value T) *T {
	return &value
}

func TestTheProfileBusySectionIsActiveDuringWorkAndReleasedAfterwards(t *testing.T) {
	busy := accountsview.NewBusy()
	activeDuring := false
	runWhileProfileBusy(busy, func() {
		activeDuring = busy.Active()
	})
	if !activeDuring {
		t.Fatal("the work ran outside the busy section")
	}
	if busy.Active() {
		t.Fatal("the section did not end after the work")
	}
}

func findeskTarget(t *testing.T) (paths.AppPaths, *db.Database) {
	t.Helper()
	appPaths := testPaths(t)
	database, err := unlock.Create(
		appPaths,
		"default",
		[]byte("target passphrase 1"),
		[]byte("target passphrase 1"),
		true,
	)
	if err != nil {
		t.Fatalf("create target database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return appPaths, database
}

func TestTheFindeskImportReleasesTheProfileBusyAfterSuccess(t *testing.T) {
	appPaths, database := findeskTarget(t)
	busy := accountsview.NewBusy()
	pending := findesk.NewFindeskImportState(i18n.En)
	source := findeskFixtureCopy(t)
	pending.SourceDir = &source
	passphrase := findeskPassphrase(t, findeskFixturePassphrase)
	defer passphrase.Wipe()

	if err := importWithProfileBusy(pending, database, appPaths, nil, passphrase, busy, nil); err != nil {
		t.Fatalf("import: %v", err)
	}
	if pending.Report == nil {
		t.Fatal("the import stored no report")
	}
	if busy.Active() {
		t.Fatal("the profile busy section did not end")
	}
}

func TestTheFindeskImportReleasesTheProfileBusyAfterAnError(t *testing.T) {
	appPaths, database := findeskTarget(t)
	busy := accountsview.NewBusy()
	pending := findesk.NewFindeskImportState(i18n.En)
	source := findeskFixtureCopy(t)
	pending.SourceDir = &source
	passphrase := findeskPassphrase(t, "wrong passphrase")
	defer passphrase.Wipe()

	err := importWithProfileBusy(pending, database, appPaths, nil, passphrase, busy, nil)
	if !errors.Is(err, findeskimport.ErrWrongPassphrase) {
		t.Fatalf("import error = %v, want a wrong passphrase", err)
	}
	if !pending.PassphraseWrong {
		t.Fatal("the wrong passphrase flag is not set")
	}
	if busy.Active() {
		t.Fatal("the profile busy section did not end")
	}
}

func TestASettingsLoadDuringABackupDoesNotLeaveThePageBusy(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	target := filepath.Join(t.TempDir(), "finglass-backup.zip")
	session.SetFileDialogs(&fakeFileDialogs{backupPath: target, backupResult: true})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	session.pageLoadGate = func(page string) {
		if page != "settings" {
			return
		}
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
	}
	session.SettingsCreateBackup()
	<-entered
	// Opening Settings again while the backup runs must not drop its result.
	session.SettingsLoad()
	close(release)
	waitForJobs(t, session)

	state := session.SettingsState()
	if state.IsBusy {
		t.Fatal("a load during the backup left the page busy")
	}
	if state.Message == nil || !strings.Contains(*state.Message, "Backup saved") {
		t.Fatalf("a load during the backup dropped its message: %+v", state.Message)
	}
}
