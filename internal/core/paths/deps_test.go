package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoUIImport guards the core's independence from the UI: no Wails package
// may be reachable from internal/core.
func TestNoUIImport(t *testing.T) {
	// #nosec G204 -- the command name and arguments are constants.
	cmd := exec.Command("go", "list", "-deps", "./internal/core/...")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "HOME="+originalHome)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./internal/core/...: %v\n%s", err, output)
	}

	const forbidden = "github.com/wailsapp/"
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, forbidden) {
			t.Errorf("forbidden UI module %q is reachable from internal/core", line)
		}
	}
}

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
