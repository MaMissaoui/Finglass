package fintsprobe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonTestFilesUseNoNetworkOSOrPrinting(t *testing.T) {
	assertDirectoryGuard(t, ".", func(path string, file *ast.File) {
		for _, imported := range file.Imports {
			name := strings.Trim(imported.Path.Value, `"`)
			if forbiddenProbeImport(name) {
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
				t.Errorf("%s calls fmt.%s; output must go through the injected Terminal", path, selector.Sel.Name)
			}
			return true
		})
	})
}

func TestCommandUsesNoLoggingOrBuildInfo(t *testing.T) {
	commandDirectory := filepath.Join("..", "..", "..", "cmd", "finglass-probe")
	assertDirectoryGuard(t, commandDirectory, func(path string, file *ast.File) {
		for _, imported := range file.Imports {
			name := strings.Trim(imported.Path.Value, `"`)
			switch name {
			case "log", "log/slog", "runtime/debug":
				t.Errorf("%s imports %q", path, name)
			}
		}
	})
}

func forbiddenProbeImport(name string) bool {
	if name == "net" || strings.HasPrefix(name, "net/") {
		return true
	}
	switch name {
	case "crypto/tls", "os", "os/exec", "log", "log/slog", "runtime/debug":
		return true
	default:
		return false
	}
}

func assertDirectoryGuard(t *testing.T, directory string, inspect func(string, *ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", directory, err)
	}
	parsed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed++
		inspect(path, file)
	}
	if parsed == 0 {
		t.Fatalf("no non-test Go files in %s", directory)
	}
}
