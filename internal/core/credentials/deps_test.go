package credentials

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoZalandoKeyring guards the argv rule: zalando/go-keyring passes a secret
// to the security(1) command, so it must never be reachable from our packages.
// Wails's CLI tool depends on it, hence checking the packages rather than the
// module graph.
func TestNoZalandoKeyring(t *testing.T) {
	root := repoRoot(t)
	patterns := []string{"./internal/..."}
	if _, err := os.Stat(filepath.Join(root, "cmd")); err == nil {
		patterns = append(patterns, "./cmd/...")
	}

	// #nosec G204 -- the command name and arguments are constants.
	cmd := exec.Command("go", append([]string{"list", "-deps"}, patterns...)...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %v: %v\n%s", patterns, err, output)
	}

	const forbidden = "github.com/zalando/go-keyring"
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == forbidden || strings.HasPrefix(line, forbidden+"/") {
			t.Errorf("forbidden keyring module %q is reachable from %v", line, patterns)
		}
	}
}
