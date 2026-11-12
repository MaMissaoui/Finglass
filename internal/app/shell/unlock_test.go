package shell

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/instance"
)

func TestTeardownClosesTheOldDatabaseAtOnceWhenNoJobRuns(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	old := sessionDatabase(t, session)

	session.mu.Lock()
	oldDocuments := session.documents
	oldTransactions := session.transactions
	oldDashboard := session.dashboard
	session.mu.Unlock()

	session.Teardown(startup.Startup{})

	requireClosed(t, old, "the old database")
	if session.closedDatabases.Load() != 1 {
		t.Fatalf("closed databases = %d, want 1", session.closedDatabases.Load())
	}
	if sessionDatabase(t, session) != nil {
		t.Fatal("the session still holds the old database")
	}
	session.mu.Lock()
	fresh := session.documents != oldDocuments &&
		session.transactions != oldTransactions &&
		session.dashboard != oldDashboard
	session.mu.Unlock()
	if !fresh {
		t.Fatal("teardown did not replace every page state")
	}
}

func TestTeardownClosesTheOldDatabaseWhenTheLastOldJobEnds(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	old := sessionDatabase(t, session)

	generation, ok := session.beginJob()
	if !ok {
		t.Fatal("the job did not begin")
	}
	session.Teardown(startup.Startup{})

	requireOpen(t, old, "the old database with a job in flight")
	if session.closedDatabases.Load() != 0 {
		t.Fatalf("closed databases = %d, want 0 before the job ends", session.closedDatabases.Load())
	}

	session.jobs.EndGeneration(generation)

	requireClosed(t, old, "the old database after its last job")
	if session.closedDatabases.Load() != 1 {
		t.Fatalf("closed databases = %d, want 1", session.closedDatabases.Load())
	}
}

func TestAnUnlockThatCannotLoadClosesItsDatabaseBeforeFailing(t *testing.T) {
	appPaths := testPaths(t)
	database := createTestDatabase(t, appPaths, "default")
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, "DROP TABLE audit_log")
		return err
	})
	if err != nil {
		t.Fatalf("drop audit table: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	session := newTestSession(t, appPaths)
	session.mu.Lock()
	session.unlockBusy = true
	generation := session.jobs.Begin()
	session.mu.Unlock()

	opened, err := unlock.Unlock(appPaths, "default", []byte(testPassphrase))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	err = session.finishOpen(generation, opened, i18n.En, nil)
	if !errors.Is(err, unlock.ErrFailed) {
		t.Fatalf("finishOpen error = %v, want ErrFailed", err)
	}
	screen := session.UnlockScreen()
	if screen.Busy {
		t.Fatal("the unlock screen is still busy")
	}
	if want := UnlockErrorText(i18n.En, unlock.ErrFailed); screen.ErrorText != want {
		t.Fatalf("error text = %q, want %q", screen.ErrorText, want)
	}
	requireClosed(t, opened, "the database that failed to load")
	if session.closedDatabases.Load() != 1 {
		t.Fatalf("closed databases = %d, want 1", session.closedDatabases.Load())
	}
}

func TestTwoConcurrentUnlocksOnlyOneProceeds(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newTestSession(t, appPaths)

	gate := make(chan struct{})
	claimed := make(chan struct{}, 1)
	session.unlockGate = func() {
		claimed <- struct{}{}
		<-gate
	}

	first := make(chan error, 1)
	go func() { first <- session.Unlock([]byte(testPassphrase)) }()
	<-claimed

	second := session.Unlock([]byte(testPassphrase))
	if !errors.Is(second, ErrUnlockBusy) {
		t.Fatalf("second Unlock error = %v, want ErrUnlockBusy", second)
	}

	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("first Unlock error = %v", err)
	}
	requireOpen(t, sessionDatabase(t, session), "the unlocked database")
}

func TestAJobCannotBeginWhileASwitchIsInProgress(t *testing.T) {
	session := newTestSession(t, testPaths(t))

	session.mu.Lock()
	session.switching = true
	session.mu.Unlock()
	if _, ok := session.beginJob(); ok {
		t.Fatal("a job began while a switch was in progress")
	}

	session.mu.Lock()
	session.switching = false
	session.mu.Unlock()
	generation, ok := session.beginJob()
	if !ok {
		t.Fatal("a job did not begin after the switch ended")
	}
	session.jobs.EndGeneration(generation)
}

func TestShutdownCancelsThePromptClosesTheDatabaseAndReleasesTheLock(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")

	session, offer, refusal := Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		testClock,
		testToday,
		testGetenv,
	)
	if refusal != nil {
		t.Fatalf("Start refused: %+v", refusal)
	}
	if offer != nil {
		t.Fatal("Start offered a migration without a FinDesk root")
	}
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		_ = session.Shutdown()
		t.Fatalf("unlock: %v", err)
	}
	database := sessionDatabase(t, session)

	bridge := session.Bridge()
	answer := make(chan *banksync.PinEntry, 1)
	go func() { answer <- bridge.Pin("Bank", "user", false) }()

	deadline := time.Now().Add(2 * time.Second)
	for bridge.Prompt() == nil {
		if time.Now().After(deadline) {
			_ = session.Shutdown()
			t.Fatal("the PIN prompt did not open")
		}
		time.Sleep(time.Millisecond)
	}

	if err := session.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case got := <-answer:
		if got != nil {
			t.Fatal("the pending prompt was answered instead of cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pending prompt did not return")
	}

	requireClosed(t, database, "the shut-down database")

	lock, err := instance.Acquire(appPaths)
	if err != nil {
		t.Fatalf("the instance lock was not released: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release the new lock: %v", err)
	}
}

func TestTheCallerMayWipeItsPassphraseBufferRightAfterUnlock(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newTestSession(t, appPaths)

	input := []byte(testPassphrase)
	err := session.Unlock(input)
	wipeBytes(input)

	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	requireOpen(t, sessionDatabase(t, session), "the unlocked database")
}

func TestCreateAndChangeThroughTheSession(t *testing.T) {
	appPaths := testPaths(t)

	creator := newTestSession(t, appPaths)
	passphrase := []byte(testPassphrase)
	if err := creator.Create(passphrase, passphrase, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	wipeBytes(passphrase)
	requireOpen(t, sessionDatabase(t, creator), "the created database")
	if err := creator.Shutdown(); err != nil {
		t.Fatalf("shutdown creator: %v", err)
	}

	changer := newTestSession(t, appPaths)
	current := []byte(testPassphrase)
	next := []byte("a different correct battery staple")
	if err := changer.Change(current, next, next, true); err != nil {
		t.Fatalf("change: %v", err)
	}
	wipeBytes(current)
	wipeBytes(next)
	requireOpen(t, sessionDatabase(t, changer), "the rekeyed database")
	if err := changer.Shutdown(); err != nil {
		t.Fatalf("shutdown changer: %v", err)
	}

	opener := newTestSession(t, appPaths)
	old := []byte(testPassphrase)
	if err := opener.Unlock(old); err == nil {
		wipeBytes(old)
		t.Fatal("the old passphrase still opens the database")
	}
	wipeBytes(old)

	fresh := []byte("a different correct battery staple")
	if err := opener.Unlock(fresh); err != nil {
		wipeBytes(fresh)
		t.Fatalf("unlock with the new passphrase: %v", err)
	}
	wipeBytes(fresh)
	requireOpen(t, sessionDatabase(t, opener), "the database opened with the new passphrase")
}

func TestFormattingTheSessionNeverContainsAPassphrase(t *testing.T) {
	appPaths := testPaths(t)
	session := newTestSession(t, appPaths)

	wrong := "wrong passphrase 2468"
	if err := session.Unlock([]byte(wrong)); err == nil {
		t.Fatal("the wrong passphrase was accepted")
	}
	formatted := fmt.Sprintf("%v %+v %#v", session, session, session)
	if strings.Contains(formatted, wrong) {
		t.Fatal("a failed unlock left the passphrase in the session's format")
	}

	createClosedTestDatabase(t, appPaths, "default")
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	formatted = fmt.Sprintf("%v %+v %#v", session, session, session)
	if strings.Contains(formatted, testPassphrase) {
		t.Fatal("a successful unlock left the passphrase in the session's format")
	}
}
