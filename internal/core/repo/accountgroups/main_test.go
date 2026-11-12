package accountgroups

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// originalHome is the real home before TestMain points HOME at a temp folder.
// Tests that run the go command keep it, so the go command uses the shared
// module cache instead of creating a read-only one inside the temp folder.
var originalHome = os.Getenv("HOME")

func TestMain(m *testing.M) {
	_ = originalHome

	dir, err := os.MkdirTemp("", "finglass-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.Setenv("HOME", dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.Setenv("FINGLASS_HOME", filepath.Join(dir, "finglass-home")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
