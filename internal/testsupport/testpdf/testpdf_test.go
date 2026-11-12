package testpdf_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/documents/pdftext"
	"github.com/MaMissaoui/Finglass/internal/testsupport/testpdf"
)

func TestTextPDFRoundTripsThroughThePDFTextReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.pdf")
	lines := []string{
		"REWE Markt GmbH",
		"Datum: 03.10.2026",
		"SUMME EUR 3,78",
	}

	testpdf.TextPDF(path, lines)

	text, ok := pdftext.Extract(path, 500)
	if !ok {
		t.Fatal("the test PDF did not parse")
	}
	for _, line := range lines {
		if !containsLine(text.Text, line) {
			t.Fatalf("extracted text %q does not contain %q", text.Text, line)
		}
	}
	if text.PageCount != 1 {
		t.Fatalf("page count = %d, want 1", text.PageCount)
	}
}

func TestEmptyPDFHasNoText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pdf")

	testpdf.EmptyPDF(path)

	text, ok := pdftext.Extract(path, 500)
	if !ok {
		t.Fatal("the empty test PDF did not parse")
	}
	if text.Text != "" {
		t.Fatalf("text = %q, want empty", text.Text)
	}
}

func containsLine(text, line string) bool {
	for _, candidate := range strings.Split(text, "\n") {
		if candidate == line {
			return true
		}
	}
	return false
}
