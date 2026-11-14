package connectiontest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonTestFilesUseNoNetworkOSLoggingOrPrinting(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	parsed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed++
		for _, imported := range file.Imports {
			name := strings.Trim(imported.Path.Value, `"`)
			switch {
			case name == "net" || strings.HasPrefix(name, "net/"):
				t.Errorf("%s imports %q", path, name)
			case name == "crypto/tls", name == "os", name == "os/exec",
				name == "log", name == "log/slog", name == "runtime/debug":
				t.Errorf("%s imports %q", path, name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if identifier.Name == "fmt" && strings.HasPrefix(selector.Sel.Name, "Print") {
				t.Errorf("%s calls fmt.%s", path, selector.Sel.Name)
			}
			return true
		})
	}
	if parsed == 0 {
		t.Fatal("no non-test Go files")
	}
}
