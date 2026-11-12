package auditview

import (
	"context"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()

	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	file, err := resolved.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}

	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	database, err := db.Create(file, passphrase)
	passphrase.Wipe()
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestRowsKeepTheAuditOrderAndValues(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()
	if err := audit.Log(ctx, database, "first", audit.ActorUser, "one"); err != nil {
		t.Fatalf("log first: %v", err)
	}
	if err := audit.Log(ctx, database, "second", audit.ActorImport, "two"); err != nil {
		t.Fatalf("log second: %v", err)
	}

	rows, err := Recent(ctx, database, 100)
	if err != nil {
		t.Fatalf("recent = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Action != "second" {
		t.Fatalf("rows[0].Action = %q", rows[0].Action)
	}
	if rows[0].Actor != "import" {
		t.Fatalf("rows[0].Actor = %q", rows[0].Actor)
	}
	if rows[0].Details != "two" {
		t.Fatalf("rows[0].Details = %q", rows[0].Details)
	}
	if rows[1].Action != "first" {
		t.Fatalf("rows[1].Action = %q", rows[1].Action)
	}
	if rows[1].Actor != "user" {
		t.Fatalf("rows[1].Actor = %q", rows[1].Actor)
	}
	if rows[0].Time == "" {
		t.Fatal("rows[0].Time is empty")
	}
}

func TestLocalTimeKeepsAnUnparseableTimestamp(t *testing.T) {
	if got := localTime("not a time"); got != "not a time" {
		t.Fatalf("localTime = %q, want %q", got, "not a time")
	}
}
