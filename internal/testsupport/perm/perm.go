// Package perm provides the private-permission assertion shared by tests.
package perm

import (
	"os"
	"runtime"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/privatefs"
)

// AssertMode checks that path has exactly the wanted POSIX permission bits.
//
// Windows has no POSIX permission bits (Go reports 0666/0777), so there the
// assertion checks the owner-only DACL instead. The rest of the test must keep
// running: call this from a subtest or as the last step.
func AssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	if runtime.GOOS == "windows" {
		if err := privatefs.Check(path); err != nil {
			t.Fatalf("%s is not private: %v", path, err)
		}
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != want {
		t.Errorf("%s mode = %#o, want %#o", path, mode, want)
	}
}
