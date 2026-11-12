// Package pdf writes the low-level PDF reports.
//
// The layout mirrors FinDesk's PDFBox exporters: A4 pages, the Standard-14
// Helvetica and Helvetica-Bold fonts with WinAnsiEncoding, 50 pt margins,
// flowing text and per-page footers. Content streams are left uncompressed so
// the tests can inspect them.
package pdf

import (
	"math"
	"strings"
	"unicode"
)

// A4 width in points.
const PageWidth float32 = 595.0

// A4 height in points.
const PageHeight float32 = 842.0

// The page margin in points.
const Margin float32 = 50.0

// The footer baseline in points.
const FooterY float32 = 28.0

// The footer font size.
const FooterSize float32 = 8.0

// Ellipsis is used when fitted text is cut.
const Ellipsis = "…"

// Font is one of the two Standard-14 fonts the exports use.
type Font int

const (
	// Regular is Helvetica.
	Regular Font = iota
	// Bold is Helvetica-Bold.
	Bold
)

// resource is the font's name in the page resource dictionary.
func (f Font) resource() string {
	if f == Bold {
		return "F2"
	}
	return "F1"
}

// Sanitize replaces characters Standard-14 WinAnsi fonts cannot encode, exactly
// like FinDesk's sanitize: newlines, carriage returns and tabs become spaces,
// everything else unencodable becomes "?".
func Sanitize(text string) string {
	var result strings.Builder
	result.Grow(len(text))

	for _, character := range text {
		// Rust iterates UTF-16 code units. A supplementary character is two
		// surrogate units, and char::from_u32 rejects both, so it becomes two
		// question marks.
		if character > 0xffff {
			result.WriteString("??")
			continue
		}

		switch character {
		case '\n', '\r', '\t':
			character = ' '
		}

		if _, ok := encodeWinAnsi(character); ok {
			result.WriteRune(character)
		} else {
			result.WriteByte('?')
		}
	}

	return result.String()
}

// WinAnsiBytes encodes sanitized text as WinAnsi bytes for a PDF string literal.
func WinAnsiBytes(text string) []byte {
	bytes := make([]byte, 0, len(text))
	for _, character := range text {
		if byte, ok := encodeWinAnsi(character); ok {
			bytes = append(bytes, byte)
		} else {
			bytes = append(bytes, '?')
		}
	}
	return bytes
}

// TextFromWinAnsi decodes WinAnsi bytes back to text, used by the structural
// PDF tests.
func TextFromWinAnsi(bytes []byte) string {
	var result strings.Builder
	result.Grow(len(bytes))
	for _, byte := range bytes {
		result.WriteRune(decodeWinAnsi(byte))
	}
	return result.String()
}

func decodeWinAnsi(byte byte) rune {
	if (byte >= 0x20 && byte <= 0x7e) || byte >= 0xa0 {
		return rune(byte)
	}
	switch byte {
	case 0x80:
		return '€'
	case 0x82:
		return '‚'
	case 0x83:
		return 'ƒ'
	case 0x84:
		return '„'
	case 0x85:
		return '…'
	case 0x86:
		return '†'
	case 0x87:
		return '‡'
	case 0x88:
		return 'ˆ'
	case 0x89:
		return '‰'
	case 0x8a:
		return 'Š'
	case 0x8b:
		return '‹'
	case 0x8c:
		return 'Œ'
	case 0x8e:
		return 'Ž'
	case 0x91:
		return '‘'
	case 0x92:
		return '’'
	case 0x93:
		return '“'
	case 0x94:
		return '”'
	case 0x95:
		return '•'
	case 0x96:
		return '–'
	case 0x97:
		return '—'
	case 0x98:
		return '˜'
	case 0x99:
		return '™'
	case 0x9a:
		return 'š'
	case 0x9b:
		return '›'
	case 0x9c:
		return 'œ'
	case 0x9e:
		return 'ž'
	case 0x9f:
		return 'Ÿ'
	default:
		return '?'
	}
}

// encodeWinAnsi returns the WinAnsi byte for a character, when the encoding
// carries one.
func encodeWinAnsi(character rune) (byte, bool) {
	if character >= 0x20 && character <= 0x7e {
		return byte(character), true // #nosec G115 -- checked to fit in a byte above.
	}
	if character >= 0xa0 && character <= 0xff {
		return byte(character), true // #nosec G115 -- checked to fit in a byte above.
	}

	switch character {
	case '\u20ac': // €
		return 0x80, true
	case '\u201a': // ‚
		return 0x82, true
	case '\u0192': // ƒ
		return 0x83, true
	case '\u201e': // „
		return 0x84, true
	case '\u2026': // …
		return 0x85, true
	case '\u2020': // †
		return 0x86, true
	case '\u2021': // ‡
		return 0x87, true
	case '\u02c6': // ˆ
		return 0x88, true
	case '\u2030': // ‰
		return 0x89, true
	case '\u0160': // Š
		return 0x8a, true
	case '\u2039': // ‹
		return 0x8b, true
	case '\u0152': // Œ
		return 0x8c, true
	case '\u017d': // Ž
		return 0x8e, true
	case '\u2018': // ‘
		return 0x91, true
	case '\u2019': // ’
		return 0x92, true
	case '\u201c': // “
		return 0x93, true
	case '\u201d': // ”
		return 0x94, true
	case '\u2022': // •
		return 0x95, true
	case '\u2013': // –
		return 0x96, true
	case '\u2014': // —
		return 0x97, true
	case '\u02dc': // ˜
		return 0x98, true
	case '\u2122': // ™
		return 0x99, true
	case '\u0161': // š
		return 0x9a, true
	case '\u203a': // ›
		return 0x9b, true
	case '\u0153': // œ
		return 0x9c, true
	case '\u017e': // ž
		return 0x9e, true
	case '\u0178': // Ÿ
		return 0x9f, true
	default:
		return 0, false
	}
}

// WidthOf returns the drawn width of text in a Standard-14 font, in points.
func WidthOf(text string, font Font, size float32) float32 {
	// Rust's Sum for f32 starts at -0.0, which is what an empty string keeps.
	sum := math.Float32frombits(0x80000000)
	for _, character := range text {
		term := float32(widthOfCharacter(character, font)) / 1000.0
		sum = float32(sum + term)
	}
	return float32(sum * size)
}

// Fit cuts text to a column width with an ellipsis, measured with the same font.
func Fit(value string, font Font, size, width float32) string {
	text := Sanitize(value)
	if WidthOf(text, font, size) <= width {
		return text
	}

	characters := []rune(text)
	for end := len(characters) - 1; end >= 0; end-- {
		prefix := strings.TrimRightFunc(string(characters[:end]), unicode.IsSpace)
		candidate := prefix + Ellipsis
		if WidthOf(candidate, font, size) <= width {
			return candidate
		}
	}
	return Ellipsis
}

// widthOfCharacter returns the Helvetica AFM width of a character in 1/1000 em;
// the euro uses its real WinAnsi width.
func widthOfCharacter(character rune, font Font) uint16 {
	if character == '€' {
		return 556
	}
	byte, ok := encodeWinAnsi(character)
	if !ok {
		byte = '?'
	}
	if byte >= 0x20 && byte <= 0x7e {
		index := int(byte - 0x20)
		if font == Bold {
			return helveticaBold[index]
		}
		return helvetica[index]
	}

	switch byte {
	case 0x85, 0x97: // ellipsis, em dash
		return 1000
	case 0x91, 0x92:
		if font == Bold {
			return 278
		}
		return 222
	case 0x93, 0x94:
		if font == Bold {
			return 500
		}
		return 333
	case 0x95:
		return 350
	case 0x96:
		return 556
	case 0x99:
		return 1000
	case 0xb7:
		return 278
	default:
		return 556
	}
}

// Widths for ASCII 0x20..=0x7E.
var helvetica = [95]uint16{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
	556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833,
	722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556,
	556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334,
	260, 334, 584,
}

// Widths for ASCII 0x20..=0x7E.
var helveticaBold = [95]uint16{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556,
	556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833,
	722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611,
	556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389,
	280, 389, 584,
}

// Builder is a flowing multi-page PDF under construction.
type Builder struct {
	pages [][]byte
	y     float32
}

// NewBuilder starts a document with its first page.
func NewBuilder() *Builder {
	builder := &Builder{}
	builder.newPage()
	return builder
}

// PageCount reports how many pages have been started.
func (b *Builder) PageCount() int {
	return len(b.pages)
}

// Y is the current text baseline.
func (b *Builder) Y() float32 {
	return b.y
}

func (b *Builder) newPage() {
	b.pages = append(b.pages, nil)
	b.y = float32(PageHeight - Margin)
}

// EnsureSpace makes room for height measured from the current line, turning the
// page when needed.
func (b *Builder) EnsureSpace(height float32) {
	if float32(b.y-height) < Margin {
		b.newPage()
	}
}

// Gap moves the baseline down without a page check.
func (b *Builder) Gap(height float32) {
	b.y = float32(b.y - height)
}

// Advance moves the baseline down without a page check.
func (b *Builder) Advance(height float32) {
	b.y = float32(b.y - height)
}

// Text draws one flowing text line.
func (b *Builder) Text(value string, font Font, size float32) {
	line := float32(size + 4.0)
	b.EnsureSpace(line)
	b.y = float32(b.y - line)
	b.Draw(value, font, size, Margin)
}

// Heading draws a bold section heading.
func (b *Builder) Heading(value string) {
	b.Text(value, Bold, 12.0)
	b.Gap(2.0)
}

// Row draws a label at the left margin and its value at the right margin.
func (b *Builder) Row(label, value string, font Font, size float32) {
	line := float32(size + 5.0)
	b.EnsureSpace(line)
	b.y = float32(b.y - line)
	b.Draw(label, font, size, Margin)
	safe := Sanitize(value)
	width := WidthOf(safe, font, size)
	b.Draw(safe, font, size, float32(float32(PageWidth-Margin)-width))
}

// Draw draws text at the current baseline and a given x position.
func (b *Builder) Draw(value string, font Font, size, x float32) {
	drawOn(&b.pages[len(b.pages)-1], value, font, size, x, b.y)
}

// DrawFitted draws text fitted to a column.
func (b *Builder) DrawFitted(value string, font Font, size, x, width float32) {
	value = Fit(value, font, size, width)
	b.Draw(value, font, size, x)
}

// DrawRightFitted draws text right-aligned in a column.
func (b *Builder) DrawRightFitted(value string, font Font, size, rightEdge, width float32) {
	value = Fit(value, font, size, width)
	x := float32(rightEdge - WidthOf(value, font, size))
	b.Draw(value, font, size, x)
}

// AddFooter draws the page footer at the bottom right of one page.
func (b *Builder) AddFooter(pageIndex int, value string) {
	safe := Sanitize(value)
	width := WidthOf(safe, Regular, FooterSize)
	x := float32(float32(PageWidth-Margin) - width)
	drawOn(&b.pages[pageIndex], safe, Regular, FooterSize, x, FooterY)
}

func drawOn(page *[]byte, value string, font Font, size, x, y float32) {
	bytes := WinAnsiBytes(Sanitize(value))
	appendOperation(page, "BT")
	appendOperation(page, "/"+font.resource()+" "+numberString(size)+" Tf")
	appendOperation(page, numberString(x)+" "+numberString(y)+" Td")
	appendOperation(page, literalString(bytes)+" Tj")
	appendOperation(page, "ET")
}

func appendOperation(page *[]byte, operation string) {
	if len(*page) != 0 {
		*page = append(*page, '\n')
	}
	*page = append(*page, operation...)
}
