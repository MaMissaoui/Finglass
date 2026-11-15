// Package perm provides the POSIX permission assertion shared by tests.
package perm

import (
	"os"
	"runtime"
	"testing"
)

// AssertMode checks that path has exactly the wanted POSIX permission bits.
//
// Windows has no POSIX permission bits (Go reports 0666/0777), so the mode
// assertion is skipped there. The rest of the test must keep running: call this
// from a subtest or as the last step.
func AssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows; the Windows ACL check is G11 'private data folder on Windows'")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != want {
		t.Errorf("%s mode = %#o, want %#o", path, mode, want)
	}
}
