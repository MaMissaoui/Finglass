//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirCreatesAPrivateFolder(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "nested", "profile")

	if err := EnsureDir(folder); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("mode = %#o, want 0700", mode)
	}
}

func TestEnsureDirLeavesAnExistingModeUnchanged(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(folder, 0o600); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(folder, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if err := EnsureDir(folder); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %#o, want 0600", mode)
	}
}
