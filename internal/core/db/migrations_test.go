package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

var okV1 = Migration{Version: 1, Name: "ok", SQL: "CREATE TABLE first (id INTEGER);"}

var failV2 = Migration{Version: 2, Name: "failing", SQL: "CREATE TABLE second (id INTEGER); THIS IS NOT VALID SQL;"}

func openMemoryConnection(t *testing.T) *sql.Conn {
	t.Helper()

	sqlDB, err := sql.Open(driverName, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("sql.Conn: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

func versionOf(t *testing.T, conn *sql.Conn) int {
	t.Helper()

	var version int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

func tableExistsOn(t *testing.T, conn *sql.Conn, name string) bool {
	t.Helper()

	var count int
	err := conn.QueryRowContext(
		context.Background(),
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("table %q: %v", name, err)
	}
	return count > 0
}

func TestFreshDatabaseEndsAtVersion3(t *testing.T) {
	conn := openMemoryConnection(t)

	if err := migrate(context.Background(), conn, Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if version := versionOf(t, conn); version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
	for _, name := range []string{"settings", "audit_log", "accounts", "transactions", "bank_connections"} {
		if !tableExistsOn(t, conn, name) {
			t.Fatalf("table %q is missing", name)
		}
	}
}

func TestRunningAgainIsANoOp(t *testing.T) {
	conn := openMemoryConnection(t)

	if err := migrate(context.Background(), conn, Migrations); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(context.Background(), conn, Migrations); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	if version := versionOf(t, conn); version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
}

func TestANewerDatabaseIsRefusedUnchanged(t *testing.T) {
	conn := openMemoryConnection(t)

	if _, err := conn.ExecContext(context.Background(), "PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	err := migrate(context.Background(), conn, Migrations)
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("migrate = %v, want *TooNewError", err)
	}
	if tooNew.Found != 99 || tooNew.Supported != 3 {
		t.Fatalf("TooNewError = %+v, want {Found: 99, Supported: 3}", tooNew)
	}

	if version := versionOf(t, conn); version != 99 {
		t.Fatalf("user_version = %d, want 99", version)
	}
	if tableExistsOn(t, conn, "settings") {
		t.Fatal("settings table exists although the database is newer than this build")
	}
}

func TestAVersion1DatabaseMigratesToVersion3OnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	pass := newPassphrase(t, "correct horse battery staple")

	d, err := createWithMigrations(path, pass, Migrations[:1])
	if err != nil {
		t.Fatalf("createWithMigrations: %v", err)
	}
	if version := userVersion(t, d); version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}
	if tableExists(t, d, "accounts") {
		t.Fatal("accounts table exists at version 1")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d, err = Open(path, pass)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()
	if version := userVersion(t, d); version != 3 {
		t.Fatalf("user_version = %d, want 3 after open", version)
	}
	if !tableExists(t, d, "accounts") {
		t.Fatal("accounts table is missing after migration")
	}
}

func TestAFailingStepRollsBackOnlyItself(t *testing.T) {
	conn := openMemoryConnection(t)

	err := migrate(context.Background(), conn, []Migration{okV1, failV2})
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || migrationErr.Version != 2 {
		t.Fatalf("migrate = %v, want MigrationError for version 2", err)
	}

	if version := versionOf(t, conn); version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}
	if !tableExistsOn(t, conn, "first") {
		t.Fatal("table first is missing")
	}
	if tableExistsOn(t, conn, "second") {
		t.Fatal("table second exists although migration 2 failed")
	}
}
