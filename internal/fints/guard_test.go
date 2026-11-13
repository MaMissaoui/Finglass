package fints

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The allowlist here is deliberately a second, independent copy: the test must
// notice when a forbidden type literal appears in the package.
var allowedSegmentTypeLiterals = map[string]struct{}{
	"HNHBK": {},
	"HNHBS": {},
	"HNVSK": {},
	"HNVSD": {},
	"HNSHK": {},
	"HNSHA": {},
	"HKIDN": {},
	"HKVVB": {},
	"HKSYN": {},
	"HKEND": {},
	"HKTAN": {},
	"HKTAB": {},
	"HKSPA": {},
	"HKSAL": {},
	"HKKAZ": {},
	"HKCAZ": {},
}

// Every order segment the read-only client must never be able to build.
var orderSegmentTypes = []string{
	"HKCCS", "HKCSE", "HKCSB", "HKCSL", "HKCSA",
	"HKCDE", "HKCDB", "HKCDL", "HKCDN",
	"HKCCM", "HKCME", "HKDSE", "HKDME", "HKDSC",
	"HKBSE", "HKBME", "HKCUM", "HKAUB",
	"HKIPZ", "HKIPM", "HKWPD", "HKWPK", "HKTSE",
}

func TestEncoderRefusesEveryOrderSegment(t *testing.T) {
	for _, segmentType := range orderSegmentTypes {
		t.Run(segmentType, func(t *testing.T) {
			// The allowlist check is the internal test hook.
			if err := checkOutgoingType(segmentType); err == nil {
				t.Fatalf("checkOutgoingType(%q) accepted an order segment", segmentType)
			}

			// The same check through the encoder.
			raw := Segment{degs: []DEG{segmentHeader(segmentType, 1, 1)}}
			if _, err := appendOutgoingSegment(nil, raw, 1); err == nil {
				t.Fatalf("appendOutgoingSegment accepted %q", segmentType)
			}

			// And through the message builder.
			if _, err := BuildMessage(messageInput(testPIN, ""), raw); err == nil {
				t.Fatalf("BuildMessage accepted %q", segmentType)
			}
		})
	}
}

func TestEncoderAcceptsEveryAllowedSegmentType(t *testing.T) {
	for segmentType := range allowedSegmentTypeLiterals {
		if err := checkOutgoingType(segmentType); err != nil {
			t.Fatalf("checkOutgoingType(%q) = %v", segmentType, err)
		}
	}
}

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

var segmentTypeLiteralPattern = regexp.MustCompile(`^HK[A-Z]{3}$`)

// TestOnlyAllowedSegmentTypeLiterals scans every non-test string literal. It
// fails on any HKxxx literal that is not on the read-only allowlist, so an
// order segment cannot appear even as an opaque string constant.
func TestOnlyAllowedSegmentTypeLiterals(t *testing.T) {
	fileSet, files, names := parsePackageSource(t)

	for index, file := range files {
		path := names[index]
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Errorf("%s: unquote %q: %v", path, literal.Value, err)
				return true
			}
			if !segmentTypeLiteralPattern.MatchString(value) {
				return true
			}
			if _, allowed := allowedSegmentTypeLiterals[value]; !allowed {
				position := fileSet.Position(literal.Pos())
				t.Errorf("%s: string literal %q is not an allowed segment type", position, value)
			}
			return true
		})
	}
}

// segmentTypeParameterNames is the heuristic this test documents: a parameter
// named like an outgoing segment type and typed string is a free segment-type
// string, which no exported function or method may take.
var segmentTypeParameterNames = map[string]struct{}{
	"segmentType": {},
	"segType":     {},
	"kind":        {},
}

// TestNoExportedFunctionTakesASegmentTypeString scans exported functions and
// methods. The heuristic is name-based: it catches parameters called
// segmentType, segType or kind that have type string. Unnamed string
// parameters are not flagged, because there is no name to judge.
func TestNoExportedFunctionTakesASegmentTypeString(t *testing.T) {
	fileSet, files, _ := parsePackageSource(t)

	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !function.Name.IsExported() {
				continue
			}
			for _, field := range function.Type.Params.List {
				identifier, ok := field.Type.(*ast.Ident)
				if !ok || identifier.Name != "string" {
					continue
				}
				for _, name := range field.Names {
					if _, suspect := segmentTypeParameterNames[name.Name]; suspect {
						position := fileSet.Position(name.Pos())
						t.Errorf("%s: exported %s takes a segment-type string parameter %q", position, function.Name.Name, name.Name)
					}
				}
			}
		}
	}
}

// TestNoNetworkImportsOrPrinting enforces the offline rule and the no-logging
// rule on the package's non-test files.
func TestNoNetworkImportsOrPrinting(t *testing.T) {
	fileSet, files, names := parsePackageSource(t)

	forbiddenImports := func(path string) bool {
		switch {
		case path == "net", strings.HasPrefix(path, "net/"):
			return true
		case path == "crypto/tls", path == "os/exec", path == "log", path == "log/slog":
			return true
		default:
			return false
		}
	}

	for index, file := range files {
		path := names[index]
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Errorf("%s: unquote import %q: %v", path, spec.Path.Value, err)
				continue
			}
			if forbiddenImports(importPath) {
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
			if !ok || identifier.Name != "fmt" {
				return true
			}
			switch selector.Sel.Name {
			case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
				position := fileSet.Position(call.Pos())
				t.Errorf("%s: forbidden call fmt.%s", position, selector.Sel.Name)
			}
			return true
		})
	}
}
