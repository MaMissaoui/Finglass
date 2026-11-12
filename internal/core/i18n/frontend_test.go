package i18n

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFrontendFilesAreCurrent(t *testing.T) {
	generated := t.TempDir()
	if err := WriteFrontend(generated); err != nil {
		t.Fatalf("WriteFrontend: %v", err)
	}
	names := []string{"keys.ts", "en.json", "de.json", "fr.json"}
	for _, name := range names {
		// #nosec G304 -- path is the test's temporary directory.
		got, err := os.ReadFile(filepath.Join(generated, name))
		if err != nil {
			t.Fatalf("read generated %s: %v", name, err)
		}
		path := filepath.Join("..", "..", "..", "frontend", "src", "lib", "generated", "i18n", name)
		// #nosec G304 -- fixed path below the repository root.
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read checked-in %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/core/i18n/...", path)
		}
	}
}
