package shell

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
)

const testPassphrase = "correct horse battery staple"

var testNow = time.Date(2026, 10, 15, 12, 34, 56, 0, time.UTC)

func testPaths(t *testing.T) paths.AppPaths {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	return resolved
}

func testClock() time.Time {
	return testNow
}

func testToday() civil.Date {
	return civil.FromTime(testNow)
}

func testGetenv(string) (string, bool) {
	return "", false
}

func newTestSession(t *testing.T, appPaths paths.AppPaths) *Session {
	t.Helper()
	return newTestSessionAt(t, appPaths, testToday(), testNow)
}

func newTestSessionAt(t *testing.T, appPaths paths.AppPaths, today civil.Date, now time.Time) *Session {
	t.Helper()
	session := newSession(
		appPaths,
		banksync.NoCredentialStore{},
		func() time.Time { return now },
		func() civil.Date { return today },
		testGetenv,
		i18n.En,
	)
	t.Cleanup(func() { _ = session.Shutdown() })
	return session
}

func createTestDatabase(t *testing.T, appPaths paths.AppPaths, slug string) *db.Database {
	t.Helper()
	opened, err := unlock.Create(appPaths, slug, []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	return opened
}

func createClosedTestDatabase(t *testing.T, appPaths paths.AppPaths, slug string) {
	t.Helper()
	opened := createTestDatabase(t, appPaths, slug)
	if err := opened.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func sessionDatabase(t *testing.T, session *Session) *db.Database {
	t.Helper()
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.db
}

func requireOpen(t *testing.T, database *db.Database, what string) {
	t.Helper()
	err := database.WithConn(context.Background(), func(context.Context, *sql.Conn) error {
		return nil
	})
	if err != nil {
		t.Fatalf("%s is not open: %v", what, err)
	}
}

// waitForJobs waits until no job of the session's current generation runs. The
// Dashboard starts a load after an unlock, so a test that tears the profile
// down must let that load finish first.
func waitForJobs(t *testing.T, session *Session) {
	t.Helper()
	generation := session.jobs.Generation()
	idle := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(idle) })
	select {
	case <-idle:
	case <-time.After(10 * time.Second):
		t.Fatal("the session's jobs did not finish")
	}
}

func requireClosed(t *testing.T, database *db.Database, what string) {
	t.Helper()
	err := database.WithConn(context.Background(), func(context.Context, *sql.Conn) error {
		return nil
	})
	if !errors.Is(err, db.ErrUnavailable) {
		t.Fatalf("%s is not closed: %v", what, err)
	}
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func preRestoreFolders(t *testing.T, root string) []string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(root, "pre-restore-*"))
	if err != nil {
		t.Fatalf("glob pre-restore folders: %v", err)
	}
	return entries
}

func defaultDataDir(t *testing.T, appPaths paths.AppPaths) string {
	t.Helper()
	dir, err := profiles.DataDir(appPaths, "")
	if err != nil {
		t.Fatalf("default data dir: %v", err)
	}
	return dir
}
