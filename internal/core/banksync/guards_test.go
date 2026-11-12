package banksync_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// TestBanksyncImportsNoNetworkOrProcessPackages proves the package and its
// tests never reach a bank or a child process. The test itself needs os/exec
// for go list, which is why it lives in the external test package and checks
// the non-test package's dependencies only.
func TestBanksyncImportsNoNetworkOrProcessPackages(t *testing.T) {
	root := repoRoot(t)
	// #nosec G204 -- the command name and arguments are constants.
	cmd := exec.Command("go", "list", "-deps", "./internal/core/banksync")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./internal/core/banksync: %v\n%s", err, output)
	}

	forbidden := map[string]struct{}{
		"net":        {},
		"net/http":   {},
		"crypto/tls": {},
		"os/exec":    {},
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if _, bad := forbidden[line]; bad {
			t.Errorf("forbidden package %q is reachable from internal/core/banksync", line)
		}
	}
}

// TestNoOrderSegmentsInBanksync rejects every FinTS order segment, even in a
// comment. The identifiers are assembled from parts so this test does not trip
// itself.
func TestNoOrderSegmentsInBanksync(t *testing.T) {
	prefixes := []string{"HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK", "HK"}
	suffixes := []string{
		"CCS", "CCM", "IPZ", "IPM", "CSE", "CSB", "CSL", "CSA", "CDE", "CDB",
		"CDL", "CDN", "UEB", "DAE", "DSE", "DME", "BSE", "TUE", "AUE", "VPP", "VPA",
	}
	forbidden := make([]string, 0, len(suffixes))
	for index, suffix := range suffixes {
		forbidden = append(forbidden, prefixes[index]+suffix)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		path := filepath.Join(".", entry.Name())
		data, err := os.ReadFile(path) // #nosec G304 -- scanning this package's own .go files
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, identifier := range forbidden {
			if strings.Contains(text, identifier) {
				t.Errorf("%s contains the order segment %q", entry.Name(), identifier)
			}
		}
	}
}
