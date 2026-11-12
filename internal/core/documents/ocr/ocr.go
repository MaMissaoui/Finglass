// Package ocr extracts text from PDFs and images.
//
// PDFs with a text layer are read directly from the file (exact, confidence
// 100). Scanned PDFs and images are rendered and passed to the Engine, if one
// is available. This ports FinDesk's OcrService; PDFBox is replaced by
// documents/pdftext and pdftoppm, and tess4j by the Tesseract command-line
// program in this package.
package ocr

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/limits"
	"github.com/MaMissaoui/Finglass/internal/core/documents/pdftext"
	"github.com/MaMissaoui/Finglass/internal/core/documents/programs"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
)

// Recognition is one engine's recognition result.
type Recognition struct {
	// Text is the recognized text.
	Text string
	// Confidence is the confidence from 0 to 100, as Tesseract reports it.
	Confidence float64
}

// OcrError is everything that can go wrong while recognizing an image.
//
// The cause wraps an I/O error when the engine could not be started.
type OcrError struct {
	cause error
}

// Error implements error.
func (e *OcrError) Error() string {
	return e.cause.Error()
}

// Unwrap exposes the cause so errors.Is and errors.As keep working.
func (e *OcrError) Unwrap() error {
	return e.cause
}

// ErrCommandFailed means the engine ran but failed.
var ErrCommandFailed error = &OcrError{cause: errors.New("OCR command failed")}

// Engine recognizes text in a raster image.
//
// Implementations must be safe to share; the real engine serializes access
// itself, and fake engines in tests use atomics.
type Engine interface {
	// Recognize recognizes the image at image.
	Recognize(image string) (Recognition, error)
}

// Source says where an OCR result's text came from.
type Source int

const (
	// SourceTextLayer is the PDF's own text layer.
	SourceTextLayer Source = iota
	// SourceOCR is the OCR engine.
	SourceOCR
)

// String is the name used in audit details, matching FinDesk's enum names.
func (s Source) String() string {
	switch s {
	case SourceTextLayer:
		return "TEXT_LAYER"
	case SourceOCR:
		return "OCR"
	default:
		return ""
	}
}

// Result is a document's extracted text.
type Result struct {
	// Text is the extracted text.
	Text string
	// Language is the detected language, "de" or "en".
	Language string
	// Confidence is the confidence from 0 to 100.
	Confidence float64
	// PageCount is the document's page count.
	PageCount int
	// Source says where the text came from.
	Source Source
}

// SearchResult is a hit in stored OCR text.
type SearchResult struct {
	// DocumentID is the document.
	DocumentID int64
	// FileName is the document's file name.
	FileName string
	// MatchingText is a snippet around the first match.
	MatchingText string
	// Confidence is the document's OCR confidence, or zero.
	Confidence float64
}

// imageExtensions are the image extensions that go through the OCR engine.
var imageExtensions = map[string]struct{}{
	"jpg": {}, "jpeg": {}, "png": {}, "tiff": {}, "tif": {},
}

var germanWords = []string{
	"der", "die", "das", "und", "ist", "von", "zu", "mit", "für", "rechnung", "summe", "betrag",
}

var englishWords = []string{
	"the", "and", "is", "of", "to", "for", "with", "invoice", "total", "amount",
}

// Service extracts text from PDFs and images.
type Service struct {
	engine Engine
}

// NewService builds a service with an optional engine. A nil engine means no
// OCR, so only PDF text layers work.
func NewService(engine Engine) *Service {
	return &Service{engine: engine}
}

// WithoutOCR builds a service without OCR, so only PDF text layers work.
func WithoutOCR() *Service {
	return &Service{}
}

// IsOCRAvailable reports whether an OCR engine is available.
func (s *Service) IsOCRAvailable() bool {
	return s.engine != nil
}

// ProcessDocument processes a document.
//
// It returns nil for an unsupported file type, or when the file needs OCR and
// no engine is available. Processing errors also yield nil, as in FinDesk.
func (s *Service) ProcessDocument(file string) *Result {
	extension := extensionOf(file)
	switch extension {
	case "pdf":
		return s.processPDF(file)
	default:
		if _, ok := imageExtensions[extension]; ok {
			return s.processImage(file)
		}
		return nil
	}
}

func (s *Service) processPDF(file string) *Result {
	extracted, ok := pdftext.Extract(file, limits.MaxTextPages)
	if ok && utf8.RuneCountInString(extracted.Text) >= limits.MinTextLayerChars {
		return &Result{
			Text:       extracted.Text,
			Language:   DetectLanguage(extracted.Text),
			Confidence: 100.0,
			PageCount:  extracted.PageCount,
			Source:     SourceTextLayer,
		}
	}

	if s.engine == nil {
		return nil
	}

	info, ok := pdftext.Inspect(file)
	if !ok {
		return nil
	}

	temporary, err := os.MkdirTemp("", "finglass-ocr-")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	recognitions := make([]Recognition, 0, limits.MaxOCRPages)
	for index, page := range info.Pages {
		if index >= limits.MaxOCRPages {
			break
		}

		scale := limits.CappedScale(float32(page.WidthPt), float32(page.HeightPt), limits.RenderDPI/72.0)
		// Capped below 72 DPI the page is too coarse for OCR, so it is skipped.
		if scale < 1.0 {
			continue
		}
		dpi := uint32(math.Floor(float64(scale * 72.0)))
		if dpi == 0 {
			continue
		}

		prefix := filepath.Join(temporary, "page-"+strconv.Itoa(index+1))
		args := []string{
			"-f", strconv.Itoa(index + 1),
			"-l", strconv.Itoa(index + 1),
			"-r", strconv.FormatUint(uint64(dpi), 10),
			"-gray",
			"-png",
			"-singlefile",
			file,
			prefix,
		}

		pdftoppm, found := programs.Resolve("pdftoppm")
		if !found {
			return nil
		}
		output, err := runWithTimeout(pdftoppm, args, CommandTimeout)
		if err != nil {
			// Without poppler a scanned PDF simply has no OCR, like FinDesk
			// without Tesseract.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			continue
		}
		if !output.Success {
			continue
		}

		image := prefix + ".png"
		if _, err := os.Stat(image); err != nil {
			continue
		}
		recognition, err := s.engine.Recognize(image)
		if err != nil {
			return nil
		}
		recognitions = append(recognitions, recognition)
	}

	texts := make([]string, 0, len(recognitions))
	for _, recognition := range recognitions {
		trimmed := strings.TrimSpace(recognition.Text)
		if trimmed != "" {
			texts = append(texts, trimmed)
		}
	}
	combined := strings.TrimSpace(strings.Join(texts, "\n\n"))

	return &Result{
		Text:       combined,
		Language:   DetectLanguage(combined),
		Confidence: averageConfidence(recognitions),
		PageCount:  info.PageCount,
		Source:     SourceOCR,
	}
}

func (s *Service) processImage(file string) *Result {
	if s.engine == nil {
		return nil
	}
	if limits.CheckImage(file) != nil {
		return nil
	}
	recognition, err := s.engine.Recognize(file)
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(recognition.Text)
	return &Result{
		Text:       text,
		Language:   DetectLanguage(text),
		Confidence: averageConfidence([]Recognition{recognition}),
		PageCount:  1,
		Source:     SourceOCR,
	}
}

// SearchOCRText searches stored OCR text and returns a snippet around the first
// match per document.
func (s *Service) SearchOCRText(ctx context.Context, d *db.Database, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	found, err := documents.FindItems(ctx, d, &query, false, nil)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(found))
	for _, document := range found {
		content, ok, err := documents.GetOCRText(ctx, d, document.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		snippet, found := snippetAround(content, query)
		if !found {
			continue
		}
		confidence := 0.0
		if document.OCRConfidence != nil {
			confidence = *document.OCRConfidence
		}
		results = append(results, SearchResult{
			DocumentID:   document.ID,
			FileName:     document.FileName,
			MatchingText: snippet,
			Confidence:   confidence,
		})
	}

	return results, nil
}

// DetectLanguage detects "de" or "en" by counting common whole words, as
// FinDesk does.
func DetectLanguage(text string) string {
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(character rune) bool {
		return !isAlphabetic(character)
	})

	german := 0
	english := 0
	for _, word := range words {
		for _, candidate := range germanWords {
			if word == candidate {
				german++
				break
			}
		}
		for _, candidate := range englishWords {
			if word == candidate {
				english++
				break
			}
		}
	}

	if english > german {
		return "en"
	}
	return "de"
}

// isAlphabetic is Rust's char::is_alphabetic: the Unicode Alphabetic property.
func isAlphabetic(character rune) bool {
	return unicode.IsLetter(character) ||
		unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Other_Alphabetic, character)
}

func averageConfidence(results []Recognition) float64 {
	if len(results) == 0 {
		return 0.0
	}
	average := 0.0
	for _, result := range results {
		average += result.Confidence
	}
	average /= float64(len(results))
	return math.Round(average*100) / 100
}

func extensionOf(file string) string {
	base := filepath.Base(file)
	index := strings.LastIndexByte(base, '.')
	if index <= 0 {
		return ""
	}
	return strings.ToLower(base[index+1:])
}

func snippetAround(content, query string) (string, bool) {
	characters := []rune(content)
	needle := []rune(query)
	index, found := findIgnoreCase(characters, needle)
	if !found {
		return "", false
	}
	start := index - 40
	if start < 0 {
		start = 0
	}
	end := index + len(needle) + 40
	if end > len(characters) {
		end = len(characters)
	}
	snippet := string(characters[start:end])
	return collapseWhitespace(snippet), true
}

func findIgnoreCase(haystack, needle []rune) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	if len(needle) > len(haystack) {
		return 0, false
	}
	for index := 0; index+len(needle) <= len(haystack); index++ {
		matches := true
		for offset, expected := range needle {
			if !charsEqual(haystack[index+offset], expected) {
				matches = false
				break
			}
		}
		if matches {
			return index, true
		}
	}
	return 0, false
}

var lowerCaser = cases.Lower(language.Und)

func charsEqual(left, right rune) bool {
	if left == right {
		return true
	}
	if left < utf8.RuneSelf && right < utf8.RuneSelf {
		return unicode.ToLower(left) == unicode.ToLower(right)
	}
	return lowerCaser.String(string(left)) == lowerCaser.String(string(right))
}

func collapseWhitespace(text string) string {
	var collapsed strings.Builder
	collapsed.Grow(len(text))
	previousWhitespace := false
	for _, character := range text {
		if unicode.IsSpace(character) {
			if !previousWhitespace {
				collapsed.WriteByte(' ')
			}
			previousWhitespace = true
		} else {
			collapsed.WriteRune(character)
			previousWhitespace = false
		}
	}
	return collapsed.String()
}
