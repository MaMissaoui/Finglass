package pdftext

import (
	"io"

	"github.com/ledongthuc/pdf"
)

// pdfFont is the subset of pdf-extract's PdfFont trait used by text
// extraction: widths, character splitting and Unicode decoding.
type pdfFont interface {
	getWidth(id uint32) float64
	nextChar(iter *charIter) (charCode uint32, length uint8, ok bool)
	decodeChar(char uint32) string
}

type charIter struct {
	data []byte
	pos  int
}

func (it *charIter) next() (byte, bool) {
	if it.pos >= len(it.data) {
		return 0, false
	}
	b := it.data[it.pos]
	it.pos++
	return b, true
}

// makeFont mirrors pdf-extract's make_font dispatch.
func makeFont(font pdf.Value) pdfFont {
	subtype := requiredName(font, "Subtype")
	switch subtype {
	case "Type0":
		return newCIDFont(font)
	case "Type3":
		return newType3Font(font)
	default:
		return newSimpleFont(font)
	}
}

type simpleFont struct {
	encoding     []uint16
	unicodeMap   map[uint32]string
	widths       map[uint32]float64
	missingWidth float64
}

type type3Font struct {
	encoding   []uint16
	unicodeMap map[uint32]string
	widths     map[uint32]float64
}

type cidFont struct {
	codespace    []codeRange
	cid          []cidRange
	toUnicode    map[uint32]string
	widths       map[uint32]float64
	defaultWidth float64
}

func newSimpleFont(font pdf.Value) *simpleFont {
	baseName := requiredName(font, "BaseFont")
	subtype := requiredName(font, "Subtype")
	encoding := font.Key("Encoding")

	var type1Encoding map[uint32][]byte

	descriptor := font.Key("FontDescriptor")
	if descriptor.Kind() == pdf.Dict {
		switch subtype {
		case "Type1":
			if file := descriptor.Key("FontFile"); file.Kind() == pdf.Stream {
				type1Encoding = readType1Encoding(readStream(file))
			}
		case "TrueType":
			// pdf-extract reads FontFile2 here but deliberately discards it.
		}

		if file3 := descriptor.Key("FontFile3"); file3.Kind() == pdf.Stream {
			// pdf-extract reads the subtype here and parses Type1C CFF
			// charsets. This port does not build a CFF-derived map and
			// falls back to the /Encoding rules below (a listed deviation).
			_ = requiredName(file3, "Subtype")
		}
	}

	// Rust extends any CFF map with the ToUnicode map, so ToUnicode wins.
	// No CFF map is built here, so the ToUnicode map is used directly.
	unicodeMap := getUnicodeMap(font)

	var encodingTable []uint16
	switch encoding.Kind() {
	case pdf.Null:
		if type1Encoding != nil {
			table := pdfDocEncodingTable()
			for code, name := range type1Encoding {
				if unicode, ok := glyphNameToUnicode[pdfToUTF8(name)]; ok {
					table[tableIndex(int64(code))] = unicode
				}
			}
			encodingTable = table
		} else if subtype == "TrueType" {
			encodingTable = append([]uint16(nil), winAnsiEncoding[:]...)
		}
	case pdf.Name:
		encodingTable = encodingToUnicodeTable(encoding.Name())
	case pdf.Dict:
		table := pdfDocEncodingTable()
		if base, ok := optionalName(encoding, "BaseEncoding"); ok {
			table = encodingToUnicodeTable(base)
		}
		if differences := encoding.Key("Differences"); differences.Kind() == pdf.Array {
			code := int64(0)
			for i := 0; i < differences.Len(); i++ {
				element := differences.Index(i)
				switch element.Kind() {
				case pdf.Integer:
					code = element.Int64()
				case pdf.Name:
					name := pdfToUTF8([]byte(element.Name()))
					unicode, ok := glyphNameToUnicode[name]
					if ok {
						table[tableIndex(code)] = unicode
						if unicodeMap != nil {
							if _, exists := unicodeMap[uint32FromInt64(code)]; !exists {
								unicodeMap[uint32FromInt64(code)] = utf16UnitsToString([]uint16{unicode})
							}
						}
					} else if unicodeMap != nil && containsFontAwesome(baseName) {
						if _, exists := unicodeMap[uint32FromInt64(code)]; exists {
							panic("unexpected entry in unicode map")
						}
						unicodeMap[uint32FromInt64(code)] = ""
					}
					code++
				default:
					panic("wrong type in Differences")
				}
			}
		}
		encodingTable = table
	default:
		panic("unsupported Encoding object")
	}

	widths := make(map[uint32]float64)
	firstChar, okFirst := optionalInteger(font, "FirstChar")
	lastChar, okLast := optionalInteger(font, "LastChar")
	widthValues, okWidths := optionalFloatArray(font.Key("Widths"))
	if okFirst && okLast && okWidths {
		var i int64
		for _, w := range widthValues {
			widths[uint32FromInt64(firstChar+i)] = w
			i++
		}
		if firstChar+i-1 != lastChar {
			panic("FirstChar/Widths/LastChar mismatch")
		}
	} else {
		for _, metrics := range coreFonts {
			if metrics.Name != baseName {
				continue
			}
			if encodingTable != nil {
				for _, entry := range metrics.Entries {
					unicode, ok := glyphNameToUnicode[entry.Name]
					if !ok {
						panic("unknown core-font glyph name")
					}
					for i := 0; i < len(encodingTable); i++ {
						if encodingTable[i] == unicode {
							widths[uint32(i)] = entry.Width
						}
					}
				}
			} else {
				table := make([]uint16, 256)
				for _, entry := range metrics.Entries {
					if entry.Code != -1 {
						if baseName == "ZapfDingbats" {
							unicode, ok := zapfGlyphNameToUnicode[entry.Name]
							if !ok {
								panic("unknown ZapfDingbats glyph name")
							}
							table[entry.Code] = unicode
						} else {
							unicode, ok := glyphNameToUnicode[entry.Name]
							if !ok {
								panic("unknown core-font glyph name")
							}
							table[entry.Code] = unicode
						}
					}
				}
				encodingTable = table
				for _, entry := range metrics.Entries {
					widths[uint32FromInt64(entry.Code)] = entry.Width
				}
			}
		}
	}

	missingWidth := 0.0
	if value, ok := optionalNumber(font.Key("MissingWidth")); ok {
		missingWidth = value
	}

	return &simpleFont{
		encoding:     encodingTable,
		unicodeMap:   unicodeMap,
		widths:       widths,
		missingWidth: missingWidth,
	}
}

func containsFontAwesome(baseName string) bool {
	for i := 0; i+len("FontAwesome") <= len(baseName); i++ {
		if baseName[i:i+len("FontAwesome")] == "FontAwesome" {
			return true
		}
	}
	return false
}

func newType3Font(font pdf.Value) *type3Font {
	unicodeMap := getUnicodeMap(font)
	encoding := font.Key("Encoding")

	var encodingTable []uint16
	switch encoding.Kind() {
	case pdf.Name:
		encodingTable = encodingToUnicodeTable(encoding.Name())
	case pdf.Dict:
		table := pdfDocEncodingTable()
		if base, ok := optionalName(encoding, "BaseEncoding"); ok {
			table = encodingToUnicodeTable(base)
		}
		if differences := encoding.Key("Differences"); differences.Kind() == pdf.Array {
			code := int64(0)
			for i := 0; i < differences.Len(); i++ {
				element := differences.Index(i)
				switch element.Kind() {
				case pdf.Integer:
					code = element.Int64()
				case pdf.Name:
					if unicode, ok := glyphNameToUnicode[pdfToUTF8([]byte(element.Name()))]; ok {
						table[tableIndex(code)] = unicode
					}
					code++
				default:
					panic("wrong type in Differences")
				}
			}
		}
		encodingTable = table
	default:
		panic("unsupported Type3 Encoding")
	}

	firstChar := requiredInteger(font, "FirstChar")
	lastChar := requiredInteger(font, "LastChar")
	widthValues := requiredFloatArray(font.Key("Widths"))

	widths := make(map[uint32]float64)
	var i int64
	for _, w := range widthValues {
		widths[uint32FromInt64(firstChar+i)] = w
		i++
	}
	if firstChar+i-1 != lastChar {
		panic("FirstChar/Widths/LastChar mismatch")
	}

	return &type3Font{encoding: encodingTable, unicodeMap: unicodeMap, widths: widths}
}

func newCIDFont(font pdf.Value) *cidFont {
	_ = requiredName(font, "BaseFont")

	descendants := font.Key("DescendantFonts")
	if descendants.Kind() != pdf.Array {
		panic("Descendant fonts required")
	}
	cidDict := descendants.Index(0)
	if cidDict.Kind() != pdf.Dict {
		panic("should be CID dict")
	}

	encoding := font.Key("Encoding")
	var codespace []codeRange
	var cidRanges []cidRange
	switch encoding.Kind() {
	case pdf.Name:
		name := pdfToUTF8([]byte(encoding.Name()))
		if name != "Identity-H" && name != "Identity-V" {
			panic("unsupported CID encoding")
		}
		codespace = []codeRange{{width: 2, start: 0, end: 0xffff}}
		cidRanges = []cidRange{{srcCodeLo: 0, srcCodeHi: 0xffff, dstCIDLo: 0}}
	case pdf.Stream:
		mapping := parseByteMapping(readStream(encoding))
		codespace = mapping.codespace
		cidRanges = mapping.cid
	default:
		panic("Encoding required in type0 fonts")
	}

	toUnicode := getUnicodeMap(font)

	if cidDict.Key("FontDescriptor").Kind() != pdf.Dict {
		panic("FontDescriptor required")
	}

	defaultWidth := int64(1000)
	if dw := cidDict.Key("DW"); dw.Kind() == pdf.Integer {
		defaultWidth = dw.Int64()
	}

	widths := make(map[uint32]float64)
	w := cidDict.Key("W")
	if w.Kind() == pdf.Array {
		i := 0
		for i < w.Len() {
			if i+1 >= w.Len() {
				panic("malformed W array")
			}
			next := w.Index(i + 1)
			if next.Kind() == pdf.Array {
				cid := requiredIntValue(w.Index(i))
				for j := 0; j < next.Len(); j++ {
					widths[uint32FromInt64(cid+int64(j))] = asNum(next.Index(j))
				}
				i += 2
			} else {
				// pdf-extract 0.12.1 reads w[i] for first, last and width in
				// this branch (not w[i+1]/w[i+2]), and its range is exclusive
				// of the upper bound. Preserve that behaviour exactly.
				cFirst := requiredIntValue(w.Index(i))
				cLast := requiredIntValue(w.Index(i))
				cWidth := asNum(w.Index(i))
				for id := cFirst; id < cLast; id++ {
					widths[uint32FromInt64(id)] = cWidth
				}
				i += 3
			}
		}
	}

	return &cidFont{
		codespace:    codespace,
		cid:          cidRanges,
		toUnicode:    toUnicode,
		widths:       widths,
		defaultWidth: float64(defaultWidth),
	}
}

func (f *simpleFont) getWidth(id uint32) float64 {
	if width, ok := f.widths[id]; ok {
		return width
	}
	return f.missingWidth
}

func (f *simpleFont) nextChar(iter *charIter) (uint32, uint8, bool) {
	b, ok := iter.next()
	if !ok {
		return 0, 0, false
	}
	return uint32(b), 1, true
}

func (f *simpleFont) decodeChar(char uint32) string {
	slice := []byte{byteFromUint32(char)}
	if f.unicodeMap != nil {
		if s, ok := f.unicodeMap[char]; ok {
			return s
		}
		if f.encoding == nil {
			panic("missing unicode map and encoding")
		}
		return encodingToUTF8(f.encoding, slice)
	}
	encoding := f.encoding
	if encoding == nil {
		encoding = pdfDocEncoding[:]
	}
	return encodingToUTF8(encoding, slice)
}

func (f *type3Font) getWidth(id uint32) float64 {
	width, ok := f.widths[id]
	if !ok {
		panic("missing Type3 width")
	}
	return width
}

func (f *type3Font) nextChar(iter *charIter) (uint32, uint8, bool) {
	b, ok := iter.next()
	if !ok {
		return 0, 0, false
	}
	return uint32(b), 1, true
}

func (f *type3Font) decodeChar(char uint32) string {
	slice := []byte{byteFromUint32(char)}
	if f.unicodeMap != nil {
		if s, ok := f.unicodeMap[char]; ok {
			return s
		}
		if f.encoding == nil {
			panic("missing unicode map and encoding")
		}
		return encodingToUTF8(f.encoding, slice)
	}
	encoding := f.encoding
	if encoding == nil {
		encoding = pdfDocEncoding[:]
	}
	return encodingToUTF8(encoding, slice)
}

func (f *cidFont) getWidth(id uint32) float64 {
	if width, ok := f.widths[id]; ok {
		return width
	}
	return f.defaultWidth
}

func (f *cidFont) nextChar(iter *charIter) (uint32, uint8, bool) {
	next, ok := iter.next()
	if !ok {
		return 0, 0, false
	}
	c := uint32(next)
	var code uint32
	var length uint8
	found := false
	for width := 1; width <= 4; width++ {
		for _, r := range f.codespace {
			if c >= r.start && c <= r.end && r.width == uint32(width) {
				code = c
				length = uint8(width)
				found = true
				break
			}
		}
		if found {
			break
		}
		next, ok := iter.next()
		if !ok {
			return 0, 0, false
		}
		c = c<<8 | uint32(next)
	}
	if !found {
		return 0, 0, false
	}
	for _, r := range f.cid {
		if code >= r.srcCodeLo && code <= r.srcCodeHi {
			return code + r.dstCIDLo, length, true
		}
	}
	return 0, 0, false
}

func (f *cidFont) decodeChar(char uint32) string {
	if f.toUnicode != nil {
		if s, ok := f.toUnicode[char]; ok {
			return s
		}
	}
	return ""
}

// --- value helpers -------------------------------------------------------

func optionalName(value pdf.Value, key string) (string, bool) {
	found := value.Key(key)
	if found.Kind() != pdf.Name {
		return "", false
	}
	return pdfToUTF8([]byte(found.Name())), true
}

func requiredName(value pdf.Value, key string) string {
	name, ok := optionalName(value, key)
	if !ok {
		panic("expected name " + key)
	}
	return name
}

func optionalInteger(value pdf.Value, key string) (int64, bool) {
	found := value.Key(key)
	if found.Kind() != pdf.Integer {
		return 0, false
	}
	return found.Int64(), true
}

func requiredInteger(value pdf.Value, key string) int64 {
	number, ok := optionalInteger(value, key)
	if !ok {
		panic("expected integer " + key)
	}
	return number
}

func requiredIntValue(value pdf.Value) int64 {
	if value.Kind() != pdf.Integer {
		panic("expected integer")
	}
	return value.Int64()
}

func optionalNumber(value pdf.Value) (float64, bool) {
	switch value.Kind() {
	case pdf.Integer:
		return float64(value.Int64()), true
	case pdf.Real:
		return value.Float64(), true
	default:
		return 0, false
	}
}

func asNum(value pdf.Value) float64 {
	switch value.Kind() {
	case pdf.Integer:
		return float64(value.Int64())
	case pdf.Real:
		return value.Float64()
	default:
		panic("not a number")
	}
}

func optionalFloatArray(value pdf.Value) ([]float64, bool) {
	if value.Kind() != pdf.Array {
		return nil, false
	}
	values := make([]float64, value.Len())
	for i := range values {
		values[i] = asNum(value.Index(i))
	}
	return values, true
}

func requiredFloatArray(value pdf.Value) []float64 {
	values, ok := optionalFloatArray(value)
	if !ok {
		panic("expected array")
	}
	return values
}

func readStream(value pdf.Value) []byte {
	reader := value.Reader()
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	return data
}
