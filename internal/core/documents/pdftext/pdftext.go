// Package pdftext reads PDF page counts, page sizes and text layers.
//
// Text comes from the pure-Go github.com/ledongthuc/pdf reader. A malformed PDF
// must never take the app down, so every call recovers from a panic and reports
// ok=false instead. The Rust side does the same with catch_unwind, and a PDF
// that cannot be parsed simply has no text layer.
package pdftext

import (
	"math"
	"os"

	"github.com/ledongthuc/pdf"
)

// maxFileBytes is the largest file this package reads. Documents are untrusted
// input, so a crafted file must not be able to exhaust memory before parsing
// starts. Rust's document limit is 100 MiB for uploads; extraction stops much
// earlier because a PDF that large is already unusable for a receipt.
const maxFileBytes = 50 * 1024 * 1024

// Text is the text layer of a PDF.
type Text struct {
	// Text is the trimmed text of the first requested pages.
	Text string
	// PageCount is the PDF's total page count.
	PageCount int
}

// Page is one PDF page's size in points.
type Page struct {
	// WidthPt is the page width in points.
	WidthPt float64
	// HeightPt is the page height in points.
	HeightPt float64
}

// Info is the page geometry of a PDF.
type Info struct {
	// PageCount is the PDF's total page count.
	PageCount int
	// Pages holds the pages in order.
	Pages []Page
}

// Inspect reads the page count and page sizes without extracting any text.
//
// It returns ok=false when the PDF cannot be loaded or parsed, and never panics.
func Inspect(path string) (info *Info, ok bool) {
	defer func() {
		if recover() != nil {
			info = nil
			ok = false
		}
	}()

	if !withinSizeLimit(path) {
		return nil, false
	}

	file, reader, err := pdf.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	pageCount := reader.NumPage()
	pages := make([]Page, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		width, height := pageSize(reader.Page(page))
		pages = append(pages, Page{WidthPt: width, HeightPt: height})
	}

	return &Info{PageCount: pageCount, Pages: pages}, true
}

// withinSizeLimit reports whether path is a regular file within maxFileBytes.
func withinSizeLimit(path string) bool {
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() {
		return false
	}
	return stat.Size() <= maxFileBytes
}

// pageSize returns the page's crop box, falling back to its media box and then
// to inherited boxes. An unreadable size falls back to A4, as Rust does.
func pageSize(page pdf.Page) (float64, float64) {
	for _, key := range []string{"CropBox", "MediaBox"} {
		box := inherited(page.V, key, 0)
		if box.Kind() != pdf.Array || box.Len() < 4 {
			continue
		}
		x0 := box.Index(0).Float64()
		y0 := box.Index(1).Float64()
		x1 := box.Index(2).Float64()
		y1 := box.Index(3).Float64()
		width := math.Abs(x1 - x0)
		height := math.Abs(y1 - y0)
		if width > 0 && height > 0 {
			return width, height
		}
	}
	return 595.0, 842.0
}

// inherited finds key in the page dictionary or one of its parents.
func inherited(value pdf.Value, key string, depth int) pdf.Value {
	if depth > 16 || value.IsNull() {
		return pdf.Value{}
	}
	if found := value.Key(key); !found.IsNull() {
		return found
	}
	return inherited(value.Key("Parent"), key, depth+1)
}
