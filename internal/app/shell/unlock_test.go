package shell

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
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
	waitForJobs(t, session)
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
	waitForJobs(t, session)
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
	requireNoSecretIn(t, session, wrong, "a failed unlock")

	createClosedTestDatabase(t, appPaths, "default")
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	requireNoSecretIn(t, session, testPassphrase, "a successful unlock")
}

// requireNoSecretIn checks the session's %v/%+v/%#v output and, because those
// print nested pointers only as addresses, every string and byte slice reachable
// from it through pointers, structs, slices, arrays, maps and interfaces.
func requireNoSecretIn(t *testing.T, session *Session, secretText, after string) {
	t.Helper()
	// Formatting and reflection read the session's mutex internals, which a
	// running page job also touches. Let the jobs settle and take the session
	// lock so the secret check stays race-free without weakening it.
	waitForJobs(t, session)
	session.mu.Lock()
	defer session.mu.Unlock()
	formatted := fmt.Sprintf("%v %+v %#v", session, session, session)
	if strings.Contains(formatted, secretText) {
		t.Fatalf("%s left the passphrase in the session's format", after)
	}
	if path, found := findText(reflect.ValueOf(session), secretText, "session", map[uintptr]bool{}); found {
		t.Fatalf("%s left the passphrase reachable at %s", after, path)
	}
}

func findText(v reflect.Value, needle, path string, seen map[uintptr]bool) (string, bool) {
	switch v.Kind() {
	case reflect.String:
		return path, strings.Contains(v.String(), needle)
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "", false
		}
		if v.Kind() == reflect.Pointer {
			if seen[v.Pointer()] {
				return "", false
			}
			seen[v.Pointer()] = true
		}
		return findText(v.Elem(), needle, path, seen)
	case reflect.Struct:
		for i := range v.NumField() {
			if p, found := findText(v.Field(i), needle, path+"."+v.Type().Field(i).Name, seen); found {
				return p, true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return path, strings.Contains(string(v.Bytes()), needle)
		}
		for i := range v.Len() {
			if p, found := findText(v.Index(i), needle, fmt.Sprintf("%s[%d]", path, i), seen); found {
				return p, true
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if p, found := findText(iter.Key(), needle, path+"{key}", seen); found {
				return p, true
			}
			if p, found := findText(iter.Value(), needle, path+"{value}", seen); found {
				return p, true
			}
		}
	}
	return "", false
}

// A review addition: an open still in its key derivation when Shutdown starts
// must neither publish nor outlive the instance lock, and Shutdown is final.
func TestShutdownWaitsForAnOpenInFlightAndRefusesEverythingAfterwards(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session, _, refusal := Start(
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

	gate := make(chan struct{})
	claimed := make(chan struct{}, 1)
	session.unlockGate = func() {
		claimed <- struct{}{}
		<-gate
	}
	unlocked := make(chan error, 1)
	go func() { unlocked <- session.Unlock([]byte(testPassphrase)) }()
	<-claimed

	shutDown := make(chan error, 1)
	go func() { shutDown <- session.Shutdown() }()

	select {
	case err := <-shutDown:
		t.Fatalf("Shutdown returned (%v) while an open was still in flight", err)
	case <-time.After(300 * time.Millisecond):
	}
	if held, err := instance.Acquire(appPaths); err == nil {
		_ = held.Release()
		t.Fatal("the instance lock was released while an open was still in flight")
	}

	close(gate)
	if err := <-unlocked; !errors.Is(err, ErrStaleResult) {
		t.Fatalf("the in-flight Unlock = %v, want ErrStaleResult", err)
	}
	select {
	case err := <-shutDown:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not return after the open ended")
	}

	if got := session.closedDatabases.Load(); got != 1 {
		t.Fatalf("closed databases = %d, want 1 (the stale open's handle)", got)
	}
	if sessionDatabase(t, session) != nil {
		t.Fatal("the stale open installed its database after Shutdown")
	}
	held, err := instance.Acquire(appPaths)
	if err != nil {
		t.Fatalf("the instance lock is still held after Shutdown: %v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	session.unlockGate = nil
	if err := session.Unlock([]byte(testPassphrase)); !errors.Is(err, ErrShutDown) {
		t.Fatalf("Unlock after Shutdown = %v, want ErrShutDown", err)
	}
	if _, ok := session.beginJob(); ok {
		t.Fatal("a job began after Shutdown")
	}
	if err := session.SwitchProfile(0); !errors.Is(err, ErrShutDown) {
		t.Fatalf("SwitchProfile after Shutdown = %v, want ErrShutDown", err)
	}
}
