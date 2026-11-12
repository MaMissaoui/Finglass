package banksync

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestPinEntryDebugNeverContainsThePin(t *testing.T) {
	entry := NewPinEntry(secret.NewText([]byte("987654")), true)
	debug := fmt.Sprintf("%v", entry)
	if debug != "PinEntry(remember=true)" {
		t.Fatalf("debug = %q, want %q", debug, "PinEntry(remember=true)")
	}
	if strings.Contains(debug, "987654") {
		t.Fatalf("debug contains the PIN: %q", debug)
	}
}

func TestATanMethodKeepsItsData(t *testing.T) {
	method := TanMethod{
		ID:         "942",
		Name:       "chipTAN optisch",
		MediumName: stringPointer("TAN-Generator"),
		TANFormat:  nil,
	}
	if method.ID != "942" {
		t.Fatalf("id = %q, want %q", method.ID, "942")
	}
	if method.Name != "chipTAN optisch" {
		t.Fatalf("name = %q, want %q", method.Name, "chipTAN optisch")
	}
	if method.MediumName == nil || *method.MediumName != "TAN-Generator" {
		t.Fatalf("medium name = %v, want %q", method.MediumName, "TAN-Generator")
	}
}
