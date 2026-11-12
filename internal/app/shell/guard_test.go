package shell

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestThePackageNeverReachesForTheClockEnvironmentHomeOrKeychain(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package folder: %v", err)
	}

	forbiddenCalls := []string{
		"os.Getenv(",
		"os.UserHomeDir(",
		"time.Now(",
		"SystemCredentialStore(",
		"NewSystemCredentialStore(",
		"keychain.",
	}
	forbiddenImports := []string{
		"net",
		"net/http",
		"crypto/tls",
		"os/exec",
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := fs.ReadFile(os.DirFS("."), name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range forbiddenCalls {
			if strings.Contains(string(source), forbidden) {
				t.Errorf("%s calls %s directly", name, forbidden)
			}
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			for _, forbidden := range forbiddenImports {
				if path == forbidden {
					t.Errorf("%s imports %s", name, path)
				}
			}
		}
	}
}
