package ocr

import (
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/documents/internal/testpdf"
	"github.com/MaMissaoui/Finglass/internal/core/documents/programs"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/receipt"
)

func tsvRow(level, page, block, paragraph, line, confidence, text string) string {
	return strings.Join([]string{
		level, page, block, paragraph, line, "1", "0", "0", "0", "0", confidence, text,
	}, "\t")
}

func TestRecognitionFromTSV(t *testing.T) {
	rows := []string{
		"level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext",
		tsvRow("4", "1", "1", "1", "1", "99", "IGNORED"),
		tsvRow("5", "1", "1", "1", "1", "80", "AB"),
		"5\t1\t1",
		tsvRow("5", "1", "1", "1", "2", "20", "DE"),
		tsvRow("5", "1", "1", "1", "1", "50", "C"),
		tsvRow("5", "1", "1", "1", "1", "oops", "BAD"),
		tsvRow("5", "1", "1", "1", "1", "60", "F"),
		tsvRow("5", "1", "1", "1", "2", "99", ""),
		tsvRow("5", "x", "1", "1", "1", "99", "SKIPPED"),
		tsvRow("5", "2", "1", "1", "1", "10", "XY   "),
	}

	result := recognitionFromTSV([]byte(strings.Join(rows, "\n")))

	if result.Text != "AB C F\nDE\nXY" {
		t.Fatalf("text = %q, want %q", result.Text, "AB C F\nDE\nXY")
	}
	// AB=2, C=1, F=1, DE=2, XY=2 characters; weighted confidence
	// (80*2 + 50*1 + 60*1 + 20*2 + 10*2) / 8 = 41.25.
	if math.Abs(result.Confidence-41.25) > 1e-9 {
		t.Fatalf("confidence = %v, want 41.25", result.Confidence)
	}
}

func toolAvailable(program string) bool {
	resolved, ok := programs.Resolve(program)
	if !ok {
		return false
	}
	// #nosec G204 -- test-only probe of a program resolved by programs.Resolve.
	command := exec.Command(resolved, "-v")
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return false
	}
	_ = command.Wait()
	return true
}

func renderFirstPage(t *testing.T, pdf, png string) {
	t.Helper()

	pdftoppm, ok := programs.Resolve("pdftoppm")
	if !ok {
		t.Fatal("pdftoppm resolves")
	}
	// #nosec G204 -- test-only rendering with a resolved program and fixed args.
	command := exec.Command(
		pdftoppm,
		"-r", "300",
		"-gray",
		"-png",
		"-singlefile",
		pdf,
		strings.TrimSuffix(png, filepath.Ext(png)),
	)
	command.Stdin = nil
	if err := command.Run(); err != nil {
		t.Fatalf("pdftoppm failed: %v", err)
	}
}

func TestRecognizesARenderedReceiptAndParserFindsTotalAndDate(t *testing.T) {
	engine := CreateOrNone()
	if engine == nil || !toolAvailable("pdftoppm") {
		t.Skip("skipping: tesseract with deu+eng or pdftoppm is not installed")
	}

	directory := t.TempDir()
	pdf := filepath.Join(directory, "receipt.pdf")
	testpdf.TextPDF(pdf, []string{
		"REWE Markt GmbH",
		"Datum: 03.10.2026",
		"Milch 1,29",
		"Brot 2,49",
		"SUMME EUR 3,78",
	})
	png := filepath.Join(directory, "receipt.png")
	renderFirstPage(t, pdf, png)

	result, err := engine.Recognize(png)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}

	if !strings.Contains(strings.ToLower(result.Text), "rewe") {
		t.Fatalf("recognized text: %q", result.Text)
	}
	if result.Confidence <= 50.0 {
		t.Fatalf("confidence %v", result.Confidence)
	}

	today, ok := civil.New(2026, 11, 7)
	if !ok {
		t.Fatal("invalid test date")
	}
	fields := receipt.Parse(result.Text, money.EUR, today)
	if fields.Total == nil || fields.Total.Minor != 378 {
		t.Fatalf("total = %+v, want 378 minor; text: %q", fields.Total, result.Text)
	}
	wantDate, ok := civil.New(2026, 10, 3)
	if !ok {
		t.Fatal("invalid expected date")
	}
	if fields.Date == nil || *fields.Date != wantDate {
		t.Fatalf("date = %+v, want %v; text: %q", fields.Date, wantDate, result.Text)
	}
}
