//go:build unix

package startup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinksAreNotFollowed(t *testing.T) {
	appPaths := testPaths(t)
	outside := filepath.Join(appPaths.Root(), "outside")
	folderWithDocument(t, outside)

	if err := os.Symlink(outside, filepath.Join(appPaths.Root(), "pre-restore-20260101-000000")); err != nil {
		t.Fatalf("symlink folder: %v", err)
	}

	folder := filepath.Join(appPaths.Root(), "pre-restore-20260102-000000")
	if err := os.MkdirAll(filepath.Join(folder, "documents"), 0o700); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(outside, "documents", "receipt.pdf"),
		filepath.Join(folder, "documents", "receipt.pdf"),
	); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	if got := Leftovers(appPaths); len(got) != 0 {
		t.Fatalf("leftovers = %v, want none", got)
	}
}
