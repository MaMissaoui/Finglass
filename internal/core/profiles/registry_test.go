package profiles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// TestRegistryBytesMatchRust locks the exact bytes the Rust registry writer
// produces.
func TestRegistryBytesMatchRust(t *testing.T) {
	root := t.TempDir()
	p, err := paths.Resolve(root, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if _, err := Create(p, "Müller = Söhne: #1"); err != nil {
		t.Fatalf("Create Müller: %v", err)
	}
	if _, err := Create(p, `C:\Daten`); err != nil {
		t.Fatalf("Create C:\\Daten: %v", err)
	}
	if _, err := Create(p, "Tab\there"); err != nil {
		t.Fatalf("Create Tab: %v", err)
	}
	if err := Rename(p, "", "Privat"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := SetActive(p, "tab-here"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	// #nosec G304 -- test registry under t.TempDir().
	data, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("ReadFile registry: %v", err)
	}

	want := `# Finglass profiles
active=tab-here
name.c-daten=C\:\\Daten
name.default=Privat
name.muller-sohne-1=Müller \= Söhne\: #1
name.tab-here=Tab\there
`
	if got := string(data); got != want {
		t.Errorf("registry bytes = %q, want %q", got, want)
	}

	properties, err := load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	expected := map[string]string{
		"active":              "tab-here",
		"name.c-daten":        `C:\Daten`,
		"name.default":        "Privat",
		"name.muller-sohne-1": "Müller = Söhne: #1",
		"name.tab-here":       "Tab\there",
	}
	if !reflect.DeepEqual(properties, expected) {
		t.Errorf("parsed properties = %#v, want %#v", properties, expected)
	}

	assertNoTempFiles(t, root)
}

func TestParsePropertiesTable(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"leading and separating spaces", "  key = value", map[string]string{"key": "value"}},
		{"space separator", "key value", map[string]string{"key": "value"}},
		{"colon separator", "key:value", map[string]string{"key": "value"}},
		{"key only", "key", map[string]string{"key": ""}},
		{"trailing value spaces stay", "key  =  value  ", map[string]string{"key": "value  "}},
		{"escaped equals in key", `a\=b=c`, map[string]string{"a=b": "c"}},
		{"unicode escape", `k=M\u00fcller`, map[string]string{"k": "Müller"}},
		{"short unicode escape", `k=a\u00f`, map[string]string{"k": "a"}},
		{"invalid unicode escape", `k=a\uZZZZb`, map[string]string{"k": "ab"}},
		{"surrogate unicode escape", `k=a\uD800b`, map[string]string{"k": "ab"}},
		{"unknown escape", `k=a\qb`, map[string]string{"k": "aqb"}},
		{"trailing backslash", `k=trail\`, map[string]string{"k": `trail\`}},
		{"hash comment", "# c", map[string]string{}},
		{"bang comment", "! c", map[string]string{}},
		{"empty line", "", map[string]string{}},
		{"whitespace line", "   ", map[string]string{}},
		{"crlf lines", "a=1\r\nb=2\r\n", map[string]string{"a": "1", "b": "2"}},
		{"later key wins", "a=1\na=2\n", map[string]string{"a": "2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseProperties(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseProperties(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}
