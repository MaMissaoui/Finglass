package credentials

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// guardedOperatingSystems are the systems Finglass builds for.
var guardedOperatingSystems = []string{"darwin", "linux", "windows"}

// goList returns the dependencies of patterns for one GOOS, using go list with
// cgo off (enough to inspect the import graph).
func goList(t *testing.T, root, goos string, patterns ...string) string {
	t.Helper()

	// #nosec G204 -- the command name and arguments are constants.
	cmd := exec.Command("go", append([]string{"list", "-deps"}, patterns...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GOOS=%s go list -deps %v: %v\n%s", goos, patterns, err, output)
	}
	return string(output)
}

// containsDependency reports whether the go list output contains pkg or a
// package below it.
func containsDependency(output, pkg string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == pkg || strings.HasPrefix(line, pkg+"/") {
			return true
		}
	}
	return false
}

// TestNoZalandoKeyring guards the argv rule: zalando/go-keyring passes a secret
// to the security(1) command, so it must never be reachable from our packages
// on any system Finglass builds for. Wails's CLI tool depends on it, hence
// checking the packages rather than the module graph.
func TestNoZalandoKeyring(t *testing.T) {
	root := repoRoot(t)
	patterns := []string{"./internal/..."}
	if _, err := os.Stat(filepath.Join(root, "cmd")); err == nil {
		patterns = append(patterns, "./cmd/...")
	}

	const forbidden = "github.com/zalando/go-keyring"
	for _, goos := range guardedOperatingSystems {
		output := goList(t, root, goos, patterns...)
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line == forbidden || strings.HasPrefix(line, forbidden+"/") {
				t.Errorf("GOOS=%s: forbidden keyring module %q is reachable from %v", goos, line, patterns)
			}
		}
	}
}

// TestCoreNeverReachesPlatformStores keeps core free of the OS stores and of
// the packages they pull in (x/sys/windows and the D-Bus clients reach net).
// core defines the credentials.Store port; only internal/platform/credstore
// builds a real store.
func TestCoreNeverReachesPlatformStores(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"github.com/MaMissaoui/Finglass/internal/platform/credstore",
		"golang.org/x/sys/windows",
		"github.com/godbus/dbus",
		"github.com/keybase/dbus",
		"github.com/danieljoos/wincred",
		"github.com/keybase/go-keychain",
	}

	for _, goos := range guardedOperatingSystems {
		for _, pkg := range []string{"./internal/core/banksync", "./internal/core/credentials"} {
			output := goList(t, root, goos, pkg)
			for _, dependency := range forbidden {
				if containsDependency(output, dependency) {
					t.Errorf("GOOS=%s: %s is reachable from %s", goos, dependency, pkg)
				}
			}
		}
	}
}
