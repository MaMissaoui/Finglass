package ocr

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/documents/internal/testpdf"
)

type countingEngine struct {
	calls atomic.Int64
}

func (e *countingEngine) Recognize(_ string) (Recognition, error) {
	e.calls.Add(1)
	return Recognition{Text: "", Confidence: 0}, nil
}

func TestOCRResultDataClass(t *testing.T) {
	result := Result{
		Text:       "Sample OCR text",
		Language:   "de",
		Confidence: 85.5,
		PageCount:  1,
		Source:     SourceOCR,
	}

	if result.Text != "Sample OCR text" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Language != "de" {
		t.Errorf("language = %q", result.Language)
	}
	if result.Confidence != 85.5 {
		t.Errorf("confidence = %v", result.Confidence)
	}
	if result.PageCount != 1 {
		t.Errorf("page count = %d", result.PageCount)
	}
	if result.Source != SourceOCR {
		t.Errorf("source = %v", result.Source)
	}
}

func TestSearchResultDataClass(t *testing.T) {
	result := SearchResult{
		DocumentID:   1,
		FileName:     "test.pdf",
		MatchingText: "matching text",
		Confidence:   90.0,
	}

	if result.DocumentID != 1 {
		t.Errorf("document id = %d", result.DocumentID)
	}
	if result.FileName != "test.pdf" {
		t.Errorf("file name = %q", result.FileName)
	}
	if result.MatchingText != "matching text" {
		t.Errorf("matching text = %q", result.MatchingText)
	}
	if result.Confidence != 90.0 {
		t.Errorf("confidence = %v", result.Confidence)
	}
}

func TestProcessDocumentReturnsNoneWithoutCallingTheEngineForAnOversizedImage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "huge.png")
	testpdf.CraftedPNG(file, 60_000, 60_000)
	engine := &countingEngine{}
	service := NewService(engine)

	if result := service.ProcessDocument(file); result != nil {
		t.Fatalf("ProcessDocument = %+v, want nil", result)
	}
	if calls := engine.calls.Load(); calls != 0 {
		t.Fatalf("engine calls = %d, want 0", calls)
	}
}

func TestDetectLanguageCountsCommonWholeWords(t *testing.T) {
	if got := DetectLanguage("Rechnung Summe Betrag und die"); got != "de" {
		t.Fatalf("German text detected as %q", got)
	}
	if got := DetectLanguage("Invoice total amount for the and"); got != "en" {
		t.Fatalf("English text detected as %q", got)
	}
	if got := DetectLanguage(""); got != "de" {
		t.Fatalf("empty text detected as %q, want the German default", got)
	}
}

func TestSnippetsSurroundTheFirstMatchAndCollapseWhitespace(t *testing.T) {
	content := strings.Repeat("a", 50) + " Rechnung   Nr. 4711 " + strings.Repeat("b", 50)
	snippet, ok := snippetAround(content, "rechnung")
	if !ok {
		t.Fatal("snippetAround found no match")
	}
	if !strings.Contains(snippet, "Rechnung Nr. 4711") {
		t.Fatalf("snippet %q does not contain the collapsed match", snippet)
	}
	if strings.Contains(snippet, "  ") {
		t.Fatalf("whitespace should be collapsed: %q", snippet)
	}
	if _, ok := snippetAround("nothing", "4711"); ok {
		t.Fatal("snippetAround found a match that is not there")
	}
}
