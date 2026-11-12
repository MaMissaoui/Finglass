//go:build !windows

package xlsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheFinishedFileHasMode0600(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mode.xlsx")
	if err := WriteAtomically(target, func(tempPath string) error {
		return os.WriteFile(tempPath, []byte("content"), 0o600)
	}); err != nil {
		t.Fatalf("the file is written: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("the file exists: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %04o, want 0600", got)
	}
}
