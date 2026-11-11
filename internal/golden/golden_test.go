// Package golden is a placeholder for the per-area golden tests added in G2–G5.
//
// G1 records the Rust outputs under testdata/golden. This test only proves that every recorded file is valid,
// non-empty JSON; the package-specific tests will read the same files later.
package golden

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenDir is the repository's recorded Rust output directory, relative to this package.
const goldenDir = "../../testdata/golden"

func TestEveryGoldenFileParses(t *testing.T) {
	t.Parallel()

	var count int
	err := filepath.WalkDir(goldenDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G304 G122 -- fixed golden directory below the repository root
		if err != nil {
			return err
		}
		if len(data) == 0 {
			t.Errorf("%s is empty", path)
			return nil
		}

		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Errorf("%s is not valid JSON: %v", path, err)
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", goldenDir, err)
	}
	if count == 0 {
		t.Fatalf("no golden JSON files found under %s", goldenDir)
	}
}
