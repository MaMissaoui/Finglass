package fintsgateway

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonTestFilesUseNoLoggingPrintingOrOS(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(".", name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed++
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			switch importPath {
			case "log", "log/slog", "os":
				t.Errorf("%s imports %q", path, importPath)
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
			if !ok || identifier.Name != "fmt" {
				return true
			}
			if strings.HasPrefix(selector.Sel.Name, "Print") {
				t.Errorf("%s calls fmt.%s", path, selector.Sel.Name)
			}
			return true
		})
	}
	if parsed == 0 {
		t.Fatal("no non-test Go files in the package")
	}
}
