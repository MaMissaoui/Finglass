package main

import (
	"errors"
	"testing"

	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
)

func TestStatementFilterPatternListsEveryStatementExtension(t *testing.T) {
	want := "*.csv;*.sta;*.mt940;*.xml;*.zip;*.ofx;*.qfx;*.qif;*.xlsx"
	if statementExtensions != want {
		t.Fatalf("statement extensions = %q, want %q", statementExtensions, want)
	}
}

func TestReceiptFilterPatternUsesTheDocumentExtensions(t *testing.T) {
	want := filterPattern(documentsservice.SupportedExtensions)
	if want != "*.pdf;*.jpg;*.jpeg;*.png;*.tif;*.tiff" {
		t.Fatalf("receipt pattern = %q", want)
	}
}

func TestFilterPatternTrimsDotsAndEmptyExtensions(t *testing.T) {
	got := filterPattern([]string{".pdf", " jpg ", "", "."})
	if got != "*.pdf;*.jpg" {
		t.Fatalf("pattern = %q", got)
	}
}

func TestExportFilterPatternUsesTheSuggestedNamesExtension(t *testing.T) {
	cases := map[string]string{
		"report-2026.pdf":          "*.pdf",
		"finglass-2026.csv":        "*.csv",
		"tax-bookings-2026.xlsx":   "*.xlsx",
		"tax-bookings-2025-12.xpd": "*.xpd",
		"no-extension":             "",
	}
	for name, want := range cases {
		if got := exportFilterPattern(name); got != want {
			t.Fatalf("exportFilterPattern(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSelectedPathsTreatsCancelErrorAndEmptySelectionAlike(t *testing.T) {
	if paths, ok := selectedPaths([]string{"/tmp/a.csv"}, nil); !ok || len(paths) != 1 {
		t.Fatalf("selected paths = %v, %v", paths, ok)
	}
	if _, ok := selectedPaths(nil, nil); ok {
		t.Fatal("an empty selection was accepted")
	}
	if _, ok := selectedPaths([]string{"/tmp/a.csv"}, errors.New("cancelled")); ok {
		t.Fatal("an error was accepted")
	}
}
