package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasSQLiteHeader(t *testing.T) {
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain.db")
	if err := os.WriteFile(plain, append([]byte("SQLite format 3\x00"), make([]byte, 16)...), 0o600); err != nil {
		t.Fatalf("write plain file: %v", err)
	}

	encrypted := filepath.Join(dir, "encrypted.db")
	if err := os.WriteFile(encrypted, []byte("not the plain header at all"), 0o600); err != nil {
		t.Fatalf("write encrypted file: %v", err)
	}

	short := filepath.Join(dir, "short.db")
	if err := os.WriteFile(short, []byte("SQL"), 0o600); err != nil {
		t.Fatalf("write short file: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"plain", plain, true},
		{"encrypted", encrypted, false},
		{"short", short, false},
	} {
		got, err := hasSQLiteHeader(tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: hasSQLiteHeader = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTempDatabasePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "finglass.db")

	first := tempDatabasePath(path)
	second := tempDatabasePath(path)

	if first == second {
		t.Fatal("two temporary paths are identical")
	}
	if filepath.Dir(first) != dir {
		t.Fatalf("temporary path %q is not next to the database", first)
	}
	if !strings.HasPrefix(filepath.Base(first), "finglass.db.creating-") {
		t.Fatalf("temporary name %q does not use the expected prefix", filepath.Base(first))
	}
}
