package db

import (
	"os/exec"
	"strings"
	"testing"
)

// TestOnlyOneSQLite guards against a second SQLite or SQLCipher module sneaking
// into the module graph. The gate is one driver, exactly.
func TestOnlyOneSQLite(t *testing.T) {
	forbidden := []string{
		"github.com/mattn/go-sqlite3",
		"modernc.org/sqlite",
		"github.com/ncruces/go-sqlite3",
		"github.com/mutecomm/go-sqlcipher",
		"zombiezen.com/go/sqlite",
		"crawshaw.io/sqlite",
	}

	// #nosec G204 -- the command name and arguments are constants.
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	cmd.Dir = repoRoot(t)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps -test ./...: %v\n%s", err, output)
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		for _, module := range forbidden {
			if line == module || strings.HasPrefix(line, module+"/") {
				t.Errorf("forbidden SQLite module %q is in the module graph", line)
			}
		}
	}
}
