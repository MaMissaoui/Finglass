package iban

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIbanChecksum(t *testing.T) {
	if !IsValid("DE89 3704 0044 0532 0130 00") {
		t.Error("valid German IBAN rejected")
	}
	if !IsValid("GB82WEST12345698765432") {
		t.Error("valid British IBAN rejected")
	}
	if IsValid("DE89 3704 0044 0532 0130 01") {
		t.Error("wrong check digit accepted")
	}
	if IsValid("DE8937040044053201300") {
		t.Error("German IBAN with 21 characters accepted")
	}
	if IsValid("not an iban") {
		t.Error("non-IBAN text accepted")
	}
}

func TestANorwegianIbanIsValidAndNormalizesUnchanged(t *testing.T) {
	// Synthetic identifier from the ISO 3116 test set, no real account.
	if !IsValid("NO9386011117947") {
		t.Error("valid Norwegian IBAN rejected")
	}
	if got := Normalize("NO93 8601 1117 947"); got != "NO9386011117947" {
		t.Errorf("Normalize = %q", got)
	}
}

func TestACardIdentifierIsNotAnIban(t *testing.T) {
	if IsValid("CARD4321") {
		t.Error("a card id has no IBAN length")
	}
	if got := Normalize("CARD4321"); got != "CARD4321" {
		t.Errorf("Normalize(card) = %q", got)
	}
}

func TestAnIbanIsGroupedInFoursForDisplay(t *testing.T) {
	if got := Format("DE89370400440532013000"); got != "DE89 3704 0044 0532 0130 00" {
		t.Errorf("Format = %q", got)
	}
	if got := Format("NO93 8601 1117 947"); got != "NO93 8601 1117 947" {
		t.Errorf("Format = %q", got)
	}
}

func TestAnIbanIsMaskedToItsLastFourCharacters(t *testing.T) {
	if got := Mask("DE89370400440532013000"); got != "…3000" {
		t.Errorf("Mask = %q", got)
	}
	if got := Mask("CARD4321"); got != "…4321" {
		t.Errorf("Mask(card) = %q", got)
	}
}

func TestNormalizeUsesFullUnicodeUppercase(t *testing.T) {
	if got := Normalize("de89 ß"); got != "DE89SS" {
		t.Errorf("Normalize(\"de89 ß\") = %q, want DE89SS", got)
	}
}

func TestIbanGolden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "golden", "iban", "ibans.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}

	var rows []struct {
		Input     string `json:"input"`
		Normalize string `json:"normalize"`
		IsValid   bool   `json:"is_valid"`
		Format    string `json:"format"`
		Mask      string `json:"mask"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the IBAN golden is empty")
	}

	for _, row := range rows {
		if got := Normalize(row.Input); got != row.Normalize {
			t.Errorf("Normalize(%q) = %q, want %q", row.Input, got, row.Normalize)
		}
		if got := IsValid(row.Input); got != row.IsValid {
			t.Errorf("IsValid(%q) = %v, want %v", row.Input, got, row.IsValid)
		}
		if got := Format(row.Input); got != row.Format {
			t.Errorf("Format(%q) = %q, want %q", row.Input, got, row.Format)
		}
		if got := Mask(row.Input); got != row.Mask {
			t.Errorf("Mask(%q) = %q, want %q", row.Input, got, row.Mask)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
