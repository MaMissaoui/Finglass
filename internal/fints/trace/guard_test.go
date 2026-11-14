package trace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

const fintsImport = "github.com/MaMissaoui/Finglass/internal/fints"

func TestPackageImportsOnlyStandardLibraryAndFints(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// #nosec G304 -- name comes from the fixed package directory listing.
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %q: %v", name, spec.Path.Value, err)
			}
			if importPath == fintsImport || isStandardLibrary(importPath) {
				if isForbiddenImport(importPath) {
					t.Errorf("%s: forbidden import %q", name, importPath)
				}
				continue
			}
			t.Errorf("%s: import %q is not the standard library or internal/fints", name, importPath)
		}
	}
}

func isStandardLibrary(path string) bool {
	first := path
	if index := strings.IndexByte(path, '/'); index >= 0 {
		first = path[:index]
	}
	return !strings.Contains(first, ".")
}

func isForbiddenImport(path string) bool {
	switch {
	case path == "os", path == "log", path == "log/slog", path == "os/exec",
		path == "crypto/tls":
		return true
	case path == "net", strings.HasPrefix(path, "net/"):
		return true
	default:
		return false
	}
}

func TestPackageDoesNotPrint(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// #nosec G304 -- name comes from the fixed package directory listing.
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
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
			switch selector.Sel.Name {
			case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
				t.Errorf("%s: forbidden call fmt.%s", fileSet.Position(call.Pos()), selector.Sel.Name)
			}
			return true
		})
	}
}
