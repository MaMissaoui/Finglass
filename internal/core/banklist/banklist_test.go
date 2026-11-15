package banklist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/testsupport/perm"
)

func TestParseAcceptsTheFormsOfTheCSV(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantBLZ    string
		wantURL    string
		wantName   string
		wantLoaded int
	}{
		{
			name: "UTF-8 with BOM",
			data: []byte("\xEF\xBB\xBFInstitut;BLZ;PIN/TAN URL\n" +
				"München Bank;99999901;https://fints.example.test/bom\n"),
			wantBLZ:    "99999901",
			wantURL:    "https://fints.example.test/bom",
			wantName:   "München Bank",
			wantLoaded: 1,
		},
		{
			name: "Windows-1252 umlaut",
			data: []byte("Institut;BLZ;PIN/TAN URL\n" +
				"M\xfcnchen Bank;99999902;https://fints.example.test/win1252\n"),
			wantBLZ:    "99999902",
			wantURL:    "https://fints.example.test/win1252",
			wantName:   "München Bank",
			wantLoaded: 1,
		},
		{
			name: "UTF-8 umlaut",
			data: []byte("Institut;BLZ;PIN/TAN URL\n" +
				"München Bank;99999903;https://fints.example.test/utf8\n"),
			wantBLZ:    "99999903",
			wantURL:    "https://fints.example.test/utf8",
			wantName:   "München Bank",
			wantLoaded: 1,
		},
		{
			name: "header on the third line",
			data: []byte("DK FinTS list\n" +
				"downloaded 2026-11-15\n" +
				"Institut;BLZ;PIN/TAN URL\n" +
				"Beispiel Bank;99999904;https://fints.example.test/third\n"),
			wantBLZ:    "99999904",
			wantURL:    "https://fints.example.test/third",
			wantName:   "Beispiel Bank",
			wantLoaded: 1,
		},
		{
			name: "quoted field with a semicolon",
			data: []byte("Institut;BLZ;PIN/TAN URL\n" +
				"\"Bank; Süd\";99999905;https://fints.example.test/quoted\n"),
			wantBLZ:    "99999905",
			wantURL:    "https://fints.example.test/quoted",
			wantName:   "Bank; Süd",
			wantLoaded: 1,
		},
		{
			name: "normalised header names",
			data: []byte("Kreditinstitut;Bankleitzahl;PIN/TAN-Zugang\n" +
				"Normalisiert;99999906;https://fints.example.test/normal\n"),
			wantBLZ:    "99999906",
			wantURL:    "https://fints.example.test/normal",
			wantName:   "Normalisiert",
			wantLoaded: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			list, report, err := Parse(test.data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if report.Loaded != test.wantLoaded {
				t.Fatalf("Loaded = %d, want %d", report.Loaded, test.wantLoaded)
			}
			entry, ok := list.Lookup(test.wantBLZ)
			if !ok {
				t.Fatalf("%s is missing from %+v", test.wantBLZ, list)
			}
			if entry.URL != test.wantURL || entry.Name != test.wantName {
				t.Fatalf("entry = %+v, want url %q name %q", entry, test.wantURL, test.wantName)
			}
		})
	}
}

func TestParseSkipsRowsForItsReasons(t *testing.T) {
	data := []byte("Institut;BLZ;PIN/TAN URL\n" +
		"First;99999910;http://fints.example.test/http\n" +
		"No scheme;99999911;fints.example.test/no-scheme\n" +
		"Empty;99999912;\n" +
		"Short;9999991;https://fints.example.test/short\n" +
		"Duplicate first;99999913;https://fints.example.test/first\n" +
		"Duplicate second;99999913;https://fints.example.test/second\n" +
		"Invalid;99999914;https://\n")
	list, report, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if report.Loaded != 1 {
		t.Fatalf("Loaded = %d, want 1: %+v", report.Loaded, list)
	}
	entry, ok := list.Lookup("99999913")
	if !ok || entry.URL != "https://fints.example.test/first" || entry.Name != "Duplicate first" {
		t.Fatalf("duplicate kept %+v, want the first URL", entry)
	}
	wantSkipped := map[string]int{
		"not https": 1,
		"no scheme": 1,
		"empty":     1,
		"blz":       1,
		"duplicate": 1,
		"invalid":   1,
	}
	for reason, want := range wantSkipped {
		if report.Skipped[reason] != want {
			t.Errorf("Skipped[%q] = %d, want %d", reason, report.Skipped[reason], want)
		}
	}
}

func TestParseRefusesMissingColumnsAndBinaryFiles(t *testing.T) {
	t.Run("missing URL column", func(t *testing.T) {
		_, _, err := Parse([]byte("Institut;BLZ\nBeispiel;99999920\n"))
		var missing *MissingColumnError
		if !errors.As(err, &missing) || missing.Column != "URL" {
			t.Fatalf("error = %v, want a missing URL column", err)
		}
	})

	t.Run("missing BLZ column", func(t *testing.T) {
		_, _, err := Parse([]byte("Institut;PIN/TAN URL\nBeispiel;https://fints.example.test/x\n"))
		var missing *MissingColumnError
		if !errors.As(err, &missing) || missing.Column != "BLZ" {
			t.Fatalf("error = %v, want a missing BLZ column", err)
		}
	})

	t.Run("xls magic", func(t *testing.T) {
		_, _, err := Parse([]byte{0xD0, 0xCF, 0x11, 0xE0, 0x00})
		if !errors.Is(err, ErrUseCSV) {
			t.Fatalf("error = %v, want ErrUseCSV", err)
		}
	})

	t.Run("zip magic", func(t *testing.T) {
		_, _, err := Parse([]byte{'P', 'K', 0x03, 0x04})
		if !errors.Is(err, ErrUseCSV) {
			t.Fatalf("error = %v, want ErrUseCSV", err)
		}
	})

	t.Run("over 10 MB", func(t *testing.T) {
		_, _, err := Parse(make([]byte, MaxBytes+1))
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("error = %v, want ErrTooLarge", err)
		}
	})
}

func TestSaveAndLoadKeepOnlyTheThreeFields(t *testing.T) {
	root := t.TempDir()
	loadedOn, _ := civil.New(2026, time.November, 15)
	list, _, err := Parse([]byte("Institut;BLZ;HBCI-Version;PIN/TAN URL;Sicherheitsverfahren\n" +
		"Nur Drei Felder;99999930;3.0;https://fints.example.test/only;901\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := Save(root, list, loadedOn); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, FileName)
	perm.AssertMode(t, path, 0o600)

	data, err := os.ReadFile(path) // #nosec G304 -- the test's own temporary file.
	if err != nil {
		t.Fatalf("read saved JSON: %v", err)
	}
	var saved struct {
		Version  int                          `json:"version"`
		LoadedOn string                       `json:"loadedOn"`
		Entries  map[string]map[string]string `json:"entries"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved JSON: %v", err)
	}
	if saved.Version != 1 || saved.LoadedOn != "2026-11-15" {
		t.Fatalf("saved header = %+v", saved)
	}
	row, ok := saved.Entries["99999930"]
	if !ok {
		t.Fatalf("saved entries = %+v", saved.Entries)
	}
	if len(row) != 2 || row["url"] != list["99999930"].URL || row["name"] != list["99999930"].Name {
		t.Fatalf("saved row = %+v, want only url and name", row)
	}
	if strings.Contains(string(data), "HBCI") || strings.Contains(string(data), "Sicherheitsverfahren") {
		t.Fatal("the saved JSON kept a column the list does not need")
	}

	loaded, date, present, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !present || date.String() != "2026-11-15" {
		t.Fatalf("present = %t, date = %v", present, date)
	}
	if entry, ok := loaded.Lookup(" 99999930 "); !ok || entry != list["99999930"] {
		t.Fatalf("loaded entry = %+v, %t", entry, ok)
	}
}

func TestRemoveAndCorruptionTreatTheListAsAbsent(t *testing.T) {
	root := t.TempDir()
	loadedOn, _ := civil.New(2026, time.November, 15)
	if err := Save(root, List{"99999940": {URL: "https://fints.example.test/x"}}, loadedOn); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _, present, err := Load(root)
	if err != nil {
		t.Fatalf("Load after Remove: %v", err)
	}
	if present || list != nil {
		t.Fatalf("a removed list is present: %+v", list)
	}
	if err := Remove(root); err != nil {
		t.Fatalf("Remove an absent list: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	list, _, present, err = Load(root)
	if err != nil {
		t.Fatalf("Load corrupt file: %v", err)
	}
	if present || list != nil {
		t.Fatalf("a corrupt file is present: %+v", list)
	}
}
