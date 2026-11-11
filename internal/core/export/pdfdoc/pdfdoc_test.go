package pdfdoc

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/documents/pdftext"
)

var pdfOut = flag.String("pdf-out", "", "write the test PDF to this directory for manual inspection")

// repoRoot finds the checkout root from this package's test directory.
func repoRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("go.mod not found above the test directory")
		}
		directory = parent
	}
}

func readFont(t *testing.T, root, name string) []byte {
	t.Helper()

	candidates := []string{
		filepath.Join(root, "frontend", "public", "fonts", name),
		filepath.Join(root, "crates", "finglass-ui", "ui", "fonts", name),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
		if err == nil {
			return data
		}
	}
	t.Fatalf("font %s not found in %v", name, candidates)
	return nil
}

func TestWriterProducesReadableMultipageA4(t *testing.T) {
	root := repoRoot(t)
	regular := readFont(t, root, "IBMPlexSans-Regular.ttf")
	bold := readFont(t, root, "IBMPlexSans-Bold.ttf")

	doc, err := New(regular, bold)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const heading = "Steuerübersicht 2026 – Müller"
	const paragraph = "Gebühren für Straße, Café und «Anführungszeichen» in € und ß."
	doc.Heading(heading)
	doc.Paragraph(paragraph)

	header := []string{"Datum", "Beschreibung", "Betrag"}
	rows := make([][]string, 0, 120)
	for row := 1; row <= 120; row++ {
		rows = append(rows, []string{
			fmt.Sprintf("%02d.01.2026", (row-1)%28+1),
			fmt.Sprintf("Zeile %03d", row),
			fmt.Sprintf("1.%03d,56 €", row),
		})
	}
	doc.Table(header, rows, []bool{false, false, true})

	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("not a PDF: %q", data[:min(len(data), 8)])
	}

	path := filepath.Join(t.TempDir(), "writer.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write PDF: %v", err)
	}
	if *pdfOut != "" {
		if err := os.MkdirAll(*pdfOut, 0o750); err != nil {
			t.Fatalf("create -pdf-out directory: %v", err)
		}
		manual := filepath.Join(*pdfOut, "pdfdoc-test.pdf")
		if err := os.WriteFile(manual, data, 0o600); err != nil {
			t.Fatalf("write manual PDF: %v", err)
		}
	}

	info, ok := pdftext.Inspect(path)
	if !ok {
		t.Fatal("Inspect could not read the written PDF")
	}
	if info.PageCount < 3 {
		t.Fatalf("page count = %d, want at least 3", info.PageCount)
	}
	for index, page := range info.Pages {
		if math.Abs(page.WidthPt-pageWidthPt) > 0.5 {
			t.Errorf("page %d width = %f, want %f", index+1, page.WidthPt, pageWidthPt)
		}
		if math.Abs(page.HeightPt-pageHeightPt) > 0.5 {
			t.Errorf("page %d height = %f, want %f", index+1, page.HeightPt, pageHeightPt)
		}
	}

	allText, ok := pdftext.Extract(path, info.PageCount)
	if !ok {
		t.Fatal("Extract could not read the written PDF")
	}
	if !strings.Contains(allText.Text, heading) {
		t.Errorf("heading missing from %q", allText.Text)
	}
	for _, word := range []string{"Gebühren", "Straße", "Café", "«Anführungszeichen»", "€", "ß"} {
		if !strings.Contains(allText.Text, word) {
			t.Errorf("paragraph word %q missing from %q", word, allText.Text)
		}
	}
	for _, row := range []string{"Zeile 001", "Zeile 060", "Zeile 120"} {
		if !strings.Contains(allText.Text, row) {
			t.Errorf("table row %q missing from %q", row, allText.Text)
		}
	}
	if !strings.Contains(allText.Text, "1.120,56 €") {
		t.Errorf("formatted amount missing from %q", allText.Text)
	}

	// Read the pages one at a time so each footer and header is checked on its
	// own page rather than merely somewhere in the document.
	previous := ""
	for page := 1; page <= info.PageCount; page++ {
		cumulative, ok := pdftext.Extract(path, page)
		if !ok {
			t.Fatalf("Extract through page %d failed", page)
		}
		pageText := strings.TrimPrefix(cumulative.Text, previous)
		if pageText == cumulative.Text && page > 1 {
			t.Fatalf("page %d text does not extend the previous pages", page)
		}
		previous = cumulative.Text

		footer := fmt.Sprintf("%d / %d", page, info.PageCount)
		if !strings.Contains(pageText, footer) {
			t.Errorf("page %d text %q lacks footer %q", page, pageText, footer)
		}
		for _, column := range header {
			if !strings.Contains(pageText, column) {
				t.Errorf("page %d text %q lacks repeated header %q", page, pageText, column)
			}
		}
	}
}

func TestNewRejectsMissingFonts(t *testing.T) {
	if _, err := New(nil, []byte("x")); err == nil {
		t.Fatal("New accepted a missing regular font")
	}
	if _, err := New([]byte("x"), nil); err == nil {
		t.Fatal("New accepted a missing bold font")
	}
}
