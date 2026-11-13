package httptransport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// packageSourceFiles lists the package's non-test Go files.
func packageSourceFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, name)
	}
	if len(files) == 0 {
		t.Fatal("no non-test Go files found")
	}
	return files
}

func parsePackageSource(t *testing.T) (*token.FileSet, []*ast.File, []string) {
	t.Helper()
	files := packageSourceFiles(t)
	fileSet := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, name := range files {
		// #nosec G304 -- name comes from the fixed package directory listing.
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed = append(parsed, file)
	}
	return fileSet, parsed, files
}

// TestNonTestFilesNeverSkipVerification scans the raw source, so even a
// constant or a comment cannot smuggle the switch past the guard.
func TestNonTestFilesNeverSkipVerification(t *testing.T) {
	for _, name := range packageSourceFiles(t) {
		// #nosec G304 -- name comes from the fixed package directory listing.
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(source), "InsecureSkipVerify") {
			t.Errorf("%s mentions InsecureSkipVerify", name)
		}
	}
}

// TestNonTestFilesObeyImportsAndCalls enforces the package hygiene rule: no
// logging, no request dumping, no cookies, no environment reads and no print
// calls.
func TestNonTestFilesObeyImportsAndCalls(t *testing.T) {
	fileSet, files, names := parsePackageSource(t)

	forbiddenImports := map[string]struct{}{
		"log":                {},
		"log/slog":           {},
		"net/http/httputil":  {},
		"net/http/cookiejar": {},
		"os":                 {},
		"os/exec":            {},
	}

	for index, file := range files {
		name := names[index]
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Errorf("%s: unquote import %q: %v", name, spec.Path.Value, err)
				continue
			}
			if _, forbidden := forbiddenImports[importPath]; forbidden {
				position := fileSet.Position(spec.Pos())
				t.Errorf("%s: forbidden import %q", position, importPath)
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
			position := fileSet.Position(call.Pos())
			switch identifier.Name {
			case "fmt":
				switch selector.Sel.Name {
				case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
					t.Errorf("%s: forbidden call fmt.%s", position, selector.Sel.Name)
				}
			case "os":
				switch selector.Sel.Name {
				case "Getenv", "LookupEnv", "Environ", "ExpandEnv":
					t.Errorf("%s: forbidden call os.%s", position, selector.Sel.Name)
				}
			}
			return true
		})
	}
}

// TestNonTestFilesKeepDefaultProxy proves the package keeps
// http.ProxyFromEnvironment and never installs its own proxy function.
func TestNonTestFilesKeepDefaultProxy(t *testing.T) {
	fileSet, files, _ := parsePackageSource(t)

	usesDefaultProxy := false
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok || identifier.Name != "http" {
				return true
			}
			switch selector.Sel.Name {
			case "ProxyFromEnvironment":
				usesDefaultProxy = true
			case "ProxyURL":
				position := fileSet.Position(selector.Pos())
				t.Errorf("%s: forbidden proxy override http.ProxyURL", position)
			}
			return true
		})
	}
	if !usesDefaultProxy {
		t.Fatal("the transport no longer uses http.ProxyFromEnvironment")
	}
}
