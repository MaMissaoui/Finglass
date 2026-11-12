package startup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/migration"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
)

func TestSharedNamesAreLiterals(t *testing.T) {
	if backup.PendingDir != "restore-pending" {
		t.Errorf("backup.PendingDir = %q", backup.PendingDir)
	}
	if backup.PreRestorePrefix != "pre-restore-" {
		t.Errorf("backup.PreRestorePrefix = %q", backup.PreRestorePrefix)
	}
	if migration.StagingDir != ".findesk-migration-staging" {
		t.Errorf("migration.StagingDir = %q", migration.StagingDir)
	}
	if migration.MarkerFile != "findesk-migration" {
		t.Errorf("migration.MarkerFile = %q", migration.MarkerFile)
	}

	// The three names must still be the ones Leftovers recognizes.
	appPaths := testPaths(t)
	folderWithDocument(t, filepath.Join(appPaths.Root(), backup.PendingDir))
	folderWithDocument(t, filepath.Join(appPaths.Root(), backup.PreRestorePrefix+"20260101-000000"))

	found := Leftovers(appPaths)
	if len(found) != 2 {
		t.Fatalf("leftovers = %v, want the pending and pre-restore folders", found)
	}
	if found[0].Kind != LeftoverPreRestore || found[1].Kind != LeftoverPendingRestore {
		t.Errorf("kinds = %v, %v", found[0].Kind, found[1].Kind)
	}

	staging := filepath.Join(appPaths.Root(), migration.StagingDir)
	if err := os.MkdirAll(filepath.Join(staging, "1"), 0o700); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	if _, err := Prepare(appPaths, time.Date(2026, time.October, 15, 12, 34, 56, 0, time.UTC)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging still exists: %v", err)
	}
}
