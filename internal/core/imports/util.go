// Text decoding, IBAN checks, amounts and dates shared by the statement
// parsers. Ports crates/finglass-core/src/import/util.rs.
package imports

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

var upperText = cases.Upper(language.Und)

// -- text decoding --------------------------------------------------------------------------------

type charsetKind int

const (
	charsetUTF8 charsetKind = iota
	charsetWindows1252
	charsetUTF16LE
	charsetUTF16BE
	charsetOther
)

type charset struct {
	kind     charsetKind
	name     string
	encoding encoding.Encoding
}

// windows1252High is encoding_rs's WHATWG windows-1252 table for 0x80..0xFF.

var windows1252High = [128]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160,
	0x2039, 0x0152, 0x008D, 0x017D, 0x008F, 0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022,
	0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178, 0x00A0,
	0x00A1, 0x00A2, 0x00A3, 0x00A4, 0x00A5, 0x00A6, 0x00A7, 0x00A8, 0x00A9, 0x00AA, 0x00AB,
	0x00AC, 0x00AD, 0x00AE, 0x00AF, 0x00B0, 0x00B1, 0x00B2, 0x00B3, 0x00B4, 0x00B5, 0x00B6,
	0x00B7, 0x00B8, 0x00B9, 0x00BA, 0x00BB, 0x00BC, 0x00BD, 0x00BE, 0x00BF, 0x00C0, 0x00C1,
	0x00C2, 0x00C3, 0x00C4, 0x00C5, 0x00C6, 0x00C7, 0x00C8, 0x00C9, 0x00CA, 0x00CB, 0x00CC,
	0x00CD, 0x00CE, 0x00CF, 0x00D0, 0x00D1, 0x00D2, 0x00D3, 0x00D4, 0x00D5, 0x00D6, 0x00D7,
	0x00D8, 0x00D9, 0x00DA, 0x00DB, 0x00DC, 0x00DD, 0x00DE, 0x00DF, 0x00E0, 0x00E1, 0x00E2,
	0x00E3, 0x00E4, 0x00E5, 0x00E6, 0x00E7, 0x00E8, 0x00E9, 0x00EA, 0x00EB, 0x00EC, 0x00ED,
	0x00EE, 0x00EF, 0x00F0, 0x00F1, 0x00F2, 0x00F3, 0x00F4, 0x00F5, 0x00F6, 0x00F7, 0x00F8,
	0x00F9, 0x00FA, 0x00FB, 0x00FC, 0x00FD, 0x00FE, 0x00FF,
}

// decodeWindows1252 decodes bytes with encoding_rs's windows-1252 table.
func decodeWindows1252(bytes []byte) string {
	var out strings.Builder
	for _, b := range bytes {
		if b < 0x80 {
			out.WriteRune(rune(b))
		} else {
			out.WriteRune(windows1252High[int(b)-0x80])
		}
	}
	return out.String()
}

// decodeCharSet decodes bytes with BOM sniffing, exactly like Encoding::decode.
func decodeCharSet(set charset, bytes []byte) string {
	if len(bytes) >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		return decodeUTF8Lossy(bytes[3:])
	}
	if len(bytes) >= 2 && bytes[0] == 0xFF && bytes[1] == 0xFE {
		return decodeUTF16(bytes[2:], false)
	}
	if len(bytes) >= 2 && bytes[0] == 0xFE && bytes[1] == 0xFF {
		return decodeUTF16(bytes[2:], true)
	}
	switch set.kind {
	case charsetUTF8:
		return decodeUTF8Lossy(bytes)
	case charsetWindows1252:
		return decodeWindows1252(bytes)
	case charsetUTF16LE:
		return decodeUTF16(bytes, false)
	case charsetUTF16BE:
		return decodeUTF16(bytes, true)
	default:
		if set.encoding == nil {
			return decodeWindows1252(bytes)
		}
		decoded, err := set.encoding.NewDecoder().Bytes(bytes)
		if err != nil {
			return decodeWindows1252(bytes)
		}
		return string(decoded)
	}
}

// decodeUTF8Lossy replaces each maximal invalid subpart with one U+FFFD.
func decodeUTF8Lossy(bytes []byte) string {
	var out strings.Builder
	index := 0
	for index < len(bytes) {
		b := bytes[index]
		if b < 0x80 {
			out.WriteByte(b)
			index++
			continue
		}
		length, lower, upper := utf8SequenceLength(b)
		if length == 0 {
			out.WriteRune(unicode.ReplacementChar)
			index++
			continue
		}
		valid := index+1 < len(bytes) && inRange(bytes[index+1], lower, upper)
		if !valid {
			out.WriteRune(unicode.ReplacementChar)
			index++
			continue
		}
		if length == 2 {
			out.WriteRune(rune(b&0x1F)<<6 | rune(bytes[index+1]&0x3F))
			index += 2
			continue
		}
		if index+2 >= len(bytes) || !inRange(bytes[index+2], 0x80, 0xBF) {
			out.WriteRune(unicode.ReplacementChar)
			index += 2
			continue
		}
		if length == 3 {
			out.WriteRune(rune(b&0x0F)<<12 | rune(bytes[index+1]&0x3F)<<6 | rune(bytes[index+2]&0x3F))
			index += 3
			continue
		}
		if index+3 >= len(bytes) || !inRange(bytes[index+3], 0x80, 0xBF) {
			out.WriteRune(unicode.ReplacementChar)
			index += 3
			continue
		}
		out.WriteRune(rune(b&0x07)<<18 | rune(bytes[index+1]&0x3F)<<12 | rune(bytes[index+2]&0x3F)<<6 | rune(bytes[index+3]&0x3F))
		index += 4
	}
	return out.String()
}

func utf8SequenceLength(b byte) (length int, lower byte, upper byte) {
	switch {
	case b >= 0xC2 && b <= 0xDF:
		return 2, 0x80, 0xBF
	case b == 0xE0:
		return 3, 0xA0, 0xBF
	case b >= 0xE1 && b <= 0xEC:
		return 3, 0x80, 0xBF
	case b == 0xED:
		return 3, 0x80, 0x9F
	case b >= 0xEE && b <= 0xEF:
		return 3, 0x80, 0xBF
	case b == 0xF0:
		return 4, 0x90, 0xBF
	case b >= 0xF1 && b <= 0xF3:
		return 4, 0x80, 0xBF
	case b == 0xF4:
		return 4, 0x80, 0x8F
	default:
		return 0, 0, 0
	}
}

func inRange(value, lower, upper byte) bool {
	return value >= lower && value <= upper
}

func decodeUTF16(bytes []byte, bigEndian bool) string {
	var out strings.Builder
	units := make([]uint16, 0, len(bytes)/2)
	for index := 0; index+1 < len(bytes); index += 2 {
		if bigEndian {
			units = append(units, uint16(bytes[index])<<8|uint16(bytes[index+1]))
		} else {
			units = append(units, uint16(bytes[index])|uint16(bytes[index+1])<<8)
		}
	}
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if index+1 < len(units) && units[index+1] >= 0xDC00 && units[index+1] <= 0xDFFF {
				code := rune(unit-0xD800)<<10 + rune(units[index+1]-0xDC00) + 0x10000
				out.WriteRune(code)
				index++
			} else {
				out.WriteRune(unicode.ReplacementChar)
			}
		case unit >= 0xDC00 && unit <= 0xDFFF:
			out.WriteRune(unicode.ReplacementChar)
		default:
			out.WriteRune(rune(unit))
		}
	}
	if len(bytes)%2 != 0 {
		out.WriteRune(unicode.ReplacementChar)
	}
	return out.String()
}

func stripBOM(text string) string {
	return strings.TrimPrefix(text, "\uFEFF")
}

// decodeStatement decodes a statement, detecting UTF-16 before the strict
// UTF-8 attempt and otherwise falling back to Windows-1252.
func decodeStatement(bytes []byte) (string, charset) {
	if set, ok := utf16Charset(bytes); ok {
		return stripBOM(decodeCharSet(set, bytes)), set
	}
	if text, valid := validUTF8(bytes); valid {
		return stripBOM(text), charset{kind: charsetUTF8, name: "UTF-8"}
	}
	set := charset{kind: charsetWindows1252, name: "windows-1252"}
	return stripBOM(decodeCharSet(set, bytes)), set
}

// decodeText decodes shared bank text: UTF-8 if valid, otherwise Windows-1252.
func decodeText(bytes []byte) string {
	if text, valid := validUTF8(bytes); valid {
		return stripBOM(text)
	}
	return stripBOM(decodeCharSet(charset{kind: charsetWindows1252, name: "windows-1252"}, bytes))
}

// decodeXML decodes an XML document, honouring UTF-16 BOMs and the encoding
// declaration, and otherwise falling back to Windows-1252.
func decodeXML(bytes []byte) string {
	if set, ok := utf16Charset(bytes); ok {
		return stripBOM(decodeCharSet(set, bytes))
	}
	head := decodeUTF8Lossy(bytes[:min(len(bytes), 512)])
	if label, ok := declaredEncoding(head); ok {
		if set, found := encodingForLabel(label); found {
			return stripBOM(decodeCharSet(set, bytes))
		}
	}
	return decodeText(bytes)
}

func declaredEncoding(head string) (string, bool) {
	declaration := strings.TrimLeftFunc(head, unicode.IsSpace)
	if !strings.HasPrefix(declaration, "<?xml") {
		return "", false
	}
	if end := strings.Index(declaration, "?>"); end >= 0 {
		declaration = declaration[:end]
	}
	lower := asciiLower(declaration)
	start := strings.Index(lower, "encoding")
	if start < 0 {
		return "", false
	}
	rest := declaration[start+len("encoding"):]
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	rest = strings.TrimLeftFunc(rest[1:], unicode.IsSpace)
	if rest == "" {
		return "", false
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	value := rest[1:]
	end := strings.IndexByte(value, quote)
	if end < 0 {
		return "", false
	}
	return value[:end], true
}

func encodingForLabel(label string) (charset, bool) {
	label = strings.Trim(label, " \t\n\f\r")
	for index := 0; index < len(label); index++ {
		if label[index] >= 0x80 {
			return charset{}, false
		}
	}
	enc, err := htmlindex.Get(label)
	if err != nil {
		return charset{}, false
	}
	switch enc {
	case textunicode.UTF8:
		return charset{kind: charsetUTF8, name: "UTF-8"}, true
	case charmap.Windows1252:
		return charset{kind: charsetWindows1252, name: "windows-1252"}, true
	case textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM):
		return charset{kind: charsetUTF16LE, name: "UTF-16LE"}, true
	case textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM):
		return charset{kind: charsetUTF16BE, name: "UTF-16BE"}, true
	default:
		name := ""
		if canonical, err := htmlindex.Name(enc); err == nil {
			name = encodingRustName(canonical)
		}
		return charset{kind: charsetOther, name: name, encoding: enc}, true
	}
}

func asciiLower(text string) string {
	bytes := []byte(text)
	for index, value := range bytes {
		if value >= 'A' && value <= 'Z' {
			bytes[index] = value + ('a' - 'A')
		}
	}
	return string(bytes)
}

// encodingRustName maps x/text's canonical names to encoding_rs's name() values.
func encodingRustName(canonical string) string {
	names := map[string]string{
		"ibm866":         "IBM866",
		"iso-8859-2":     "ISO-8859-2",
		"iso-8859-3":     "ISO-8859-3",
		"iso-8859-4":     "ISO-8859-4",
		"iso-8859-5":     "ISO-8859-5",
		"iso-8859-6":     "ISO-8859-6",
		"iso-8859-7":     "ISO-8859-7",
		"iso-8859-8":     "ISO-8859-8",
		"iso-8859-8-i":   "ISO-8859-8-I",
		"iso-8859-10":    "ISO-8859-10",
		"iso-8859-13":    "ISO-8859-13",
		"iso-8859-14":    "ISO-8859-14",
		"iso-8859-15":    "ISO-8859-15",
		"iso-8859-16":    "ISO-8859-16",
		"koi8-r":         "KOI8-R",
		"koi8-u":         "KOI8-U",
		"macintosh":      "macintosh",
		"windows-874":    "windows-874",
		"windows-1250":   "windows-1250",
		"windows-1251":   "windows-1251",
		"windows-1252":   "windows-1252",
		"windows-1253":   "windows-1253",
		"windows-1254":   "windows-1254",
		"windows-1255":   "windows-1255",
		"windows-1256":   "windows-1256",
		"windows-1257":   "windows-1257",
		"windows-1258":   "windows-1258",
		"x-mac-cyrillic": "x-mac-cyrillic",
		"gbk":            "GBK",
		"gb18030":        "gb18030",
		"big5":           "Big5",
		"euc-jp":         "EUC-JP",
		"iso-2022-jp":    "ISO-2022-JP",
		"shift_jis":      "Shift_JIS",
		"euc-kr":         "EUC-KR",
		"replacement":    "replacement",
		"x-user-defined": "x-user-defined",
	}
	if name, ok := names[canonical]; ok {
		return name
	}
	return canonical
}

func validUTF8(bytes []byte) (string, bool) {
	return string(bytes), isValidUTF8(bytes)
}

func isValidUTF8(bytes []byte) bool {
	index := 0
	for index < len(bytes) {
		b := bytes[index]
		if b < 0x80 {
			index++
			continue
		}
		length, lower, upper := utf8SequenceLength(b)
		if length == 0 || index+length > len(bytes) || !inRange(bytes[index+1], lower, upper) {
			return false
		}
		if length >= 3 && !inRange(bytes[index+2], 0x80, 0xBF) {
			return false
		}
		if length == 4 && !inRange(bytes[index+3], 0x80, 0xBF) {
			return false
		}
		index += length
	}
	return true
}

func utf16Charset(bytes []byte) (charset, bool) {
	if len(bytes) >= 2 {
		if bytes[0] == 0xFF && bytes[1] == 0xFE {
			return charset{kind: charsetUTF16LE, name: "UTF-16LE"}, true
		}
		if bytes[0] == 0xFE && bytes[1] == 0xFF {
			return charset{kind: charsetUTF16BE, name: "UTF-16BE"}, true
		}
	}
	n := min(len(bytes), 512)
	if n < 2 {
		return charset{}, false
	}
	evenZeros := 0
	oddZeros := 0
	for i := 0; i < n; i++ {
		if bytes[i] == 0 {
			if i%2 == 0 {
				evenZeros++
			} else {
				oddZeros++
			}
		}
	}
	evenTotal := n - n/2
	oddTotal := n / 2
	lowHalfZero := oddZeros*100 >= 30*oddTotal && evenZeros*100 < 5*evenTotal
	mirrorZero := evenZeros*100 >= 30*evenTotal && oddZeros*100 < 5*oddTotal
	if lowHalfZero {
		return charset{kind: charsetUTF16LE, name: "UTF-16LE"}, true
	}
	if mirrorZero {
		return charset{kind: charsetUTF16BE, name: "UTF-16BE"}, true
	}
	return charset{}, false
}

// -- IBAN -----------------------------------------------------------------------------------------

// normalizeIBAN strips spaces and upper-cases.
func normalizeIBAN(iban string) string {
	return upperText.String(strings.ReplaceAll(iban, " ", ""))
}

// isValidIBAN runs the ISO 13616 mod-97 check.
func isValidIBAN(iban string) bool {
	s := normalizeIBAN(iban)
	characters := []rune(s)
	if len(characters) < 15 || len(characters) > 34 {
		return false
	}
	for _, character := range characters {
		if !isAlphanumeric(character) {
			return false
		}
	}
	if !isAlphabetic(characters[0]) || !isAlphabetic(characters[1]) ||
		!isASCIIDigit(characters[2]) || !isASCIIDigit(characters[3]) {
		return false
	}
	if strings.HasPrefix(s, "DE") && len(characters) != 22 {
		return false
	}
	ordered := append(append([]rune{}, characters[4:]...), characters[:4]...)
	remainder := uint32(0)
	for _, character := range ordered {
		if digit, ok := asciiDigit(character); ok {
			remainder = (remainder*10 + digit) % 97
		} else if character >= 'A' && character <= 'Z' {
			value := uint32(character-'A') + 10
			remainder = (remainder*100 + value) % 97
		} else {
			return false
		}
	}
	return remainder == 1
}

func isAlphanumeric(character rune) bool {
	return isAlphabetic(character) || unicode.IsNumber(character)
}

func isAlphabetic(character rune) bool {
	return unicode.IsLetter(character)
}

func isASCIIDigit(character rune) bool {
	return character >= '0' && character <= '9'
}

func asciiDigit(character rune) (uint32, bool) {
	if character < '0' || character > '9' {
		return 0, false
	}
	return uint32(character - '0'), true
}

// findIBANs finds IBAN-shaped runs in a preamble cell, like Kotlin's
// IBAN_PATTERN.
func findIBANs(text string) []string {
	upper := upperText.String(text)
	characters := []rune(upper)
	var result []string
	start := 0
	for start+4 <= len(characters) {
		if characters[start] < 'A' || characters[start] > 'Z' ||
			characters[start+1] < 'A' || characters[start+1] > 'Z' ||
			!isASCIIDigit(characters[start+2]) || !isASCIIDigit(characters[start+3]) {
			start++
			continue
		}
		position := start + 4
		items := 0
		for position < len(characters) && items < 30 {
			next := position
			if characters[next] == ' ' {
				next++
			}
			if next < len(characters) && isASCIIAlphanumeric(characters[next]) {
				position = next + 1
				items++
			} else {
				break
			}
		}
		if items >= 11 {
			var candidate strings.Builder
			for _, character := range characters[start:position] {
				if character != ' ' {
					candidate.WriteRune(character)
				}
			}
			result = append(result, candidate.String())
		}
		if position > start+1 {
			start = position
		} else {
			start++
		}
	}
	return result
}

func isASCIIAlphanumeric(character rune) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		isASCIIDigit(character)
}

// -- amounts --------------------------------------------------------------------------------------

// parseDecimal parses a decimal out of untrusted file text, with FinDesk's
// import bounds: at most 15 integer digits, at most 10 decimal places and at
// most 64 input characters.
func parseDecimal(text string) (decimal.Decimal, bool) {
	if len([]rune(text)) > 64 {
		return decimal.Decimal{}, false
	}
	trimmed := strings.TrimSpace(text)
	value, err := decimal.Parse(trimmed)
	if err != nil {
		return decimal.Decimal{}, false
	}
	scale := value.Scale()
	if scale > 10 {
		return decimal.Decimal{}, false
	}
	mantissa := value.Mantissa()
	mantissa.Abs(mantissa)
	mantissaDigits := len(mantissa.String())
	integerDigits := mantissaDigits - int(scale)
	if integerDigits > 15 {
		return decimal.Decimal{}, false
	}
	return value, true
}

// parseAmount parses "1.234,56", "1,234.56", "-12.5" or "12" and rounds to two
// places.
func parseAmount(input string) (decimal.Decimal, bool) {
	s := strings.TrimSpace(input)
	s = strings.NewReplacer("€", "", " ", "", "\u00A0", "").Replace(s)
	if s == "" {
		return decimal.Decimal{}, false
	}
	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")
	if (lastComma >= 0 && lastDot >= 0 && lastComma > lastDot) || (lastComma >= 0 && lastDot < 0) {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	} else {
		s = strings.ReplaceAll(s, ",", "")
	}
	value, ok := parseDecimal(s)
	if !ok {
		return decimal.Decimal{}, false
	}
	value = value.RoundDP(2).Rescale(2)
	return value, true
}

// isDecimalSyntax runs Java BigDecimal's syntax check, without FinDesk's size
// limits.
func isDecimalSyntax(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}
	bytes := []byte(s)
	i := 0
	if bytes[i] == '+' || bytes[i] == '-' {
		i++
		if i == len(bytes) {
			return false
		}
	}
	digits := 0
	for i < len(bytes) && bytes[i] >= '0' && bytes[i] <= '9' {
		i++
		digits++
	}
	if i < len(bytes) && bytes[i] == '.' {
		i++
		for i < len(bytes) && bytes[i] >= '0' && bytes[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return false
	}
	if i < len(bytes) && (bytes[i] == 'e' || bytes[i] == 'E') {
		i++
		if i < len(bytes) && (bytes[i] == '+' || bytes[i] == '-') {
			i++
		}
		exponentDigits := 0
		for i < len(bytes) && bytes[i] >= '0' && bytes[i] <= '9' {
			i++
			exponentDigits++
		}
		if exponentDigits == 0 {
			return false
		}
	}
	return i == len(bytes)
}

// -- integer parsing ------------------------------------------------------------------------------

// parseUnsigned follows Rust's str::parse::<uN>: an optional '+', then ASCII
// digits, nothing else, failing on overflow.
func parseUnsigned(text string, bits int) (uint64, bool) {
	if text == "" {
		return 0, false
	}
	digits := text
	if digits[0] == '+' {
		digits = digits[1:]
	}
	if digits == "" {
		return 0, false
	}
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(digits, 10, bits)
	if err != nil {
		return 0, false
	}
	return value, true
}

// parseSigned follows Rust's str::parse::<iN>: an optional '+' or '-', then
// ASCII digits, nothing else, failing on overflow.
func parseSigned(text string, bits int) (int64, bool) {
	if text == "" {
		return 0, false
	}
	start := 0
	if text[0] == '+' || text[0] == '-' {
		start = 1
	}
	if start == len(text) {
		return 0, false
	}
	for index := start; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(text, 10, bits)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseUsize(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	digits := text
	if digits[0] == '+' {
		digits = digits[1:]
	}
	if digits == "" {
		return 0, false
	}
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseU32(text string) (uint32, bool) {
	value, ok := parseUnsigned(text, 32)
	if !ok || value > 1<<32-1 {
		return 0, false
	}
	return uint32(value), true
}

func parseU8(text string) (uint8, bool) {
	value, ok := parseUnsigned(text, 8)
	if !ok || value > 1<<8-1 {
		return 0, false
	}
	return uint8(value), true
}

func parseI64(text string) (int64, bool) {
	return parseSigned(text, 64)
}

// -- dates ----------------------------------------------------------------------------------------

func dateFromYMD(year int, month int, day int) (civil.Date, bool) {
	if month < 1 || month > 12 {
		return civil.Date{}, false
	}
	return civil.New(year, time.Month(month), day)
}

func parseCSVDate(value *string, today civil.Date) *civil.Date {
	if value == nil {
		return nil
	}
	s := *value
	if strings.TrimSpace(s) == "" {
		return nil
	}
	fields := strings.Fields(s)
	if len(fields) == 0 || fields[0] == "" {
		return nil
	}
	token := fields[0]
	if date, ok := parseWithFormats(token); ok {
		return datePointer(date)
	}
	if date, ok := parseTwoDigitDate(token, today); ok {
		return datePointer(date)
	}
	characters := []rune(token)
	if len(characters) > 10 {
		if date, ok := parseWithFormats(string(characters[:10])); ok {
			return datePointer(date)
		}
	}
	return nil
}

func parseWithFormats(candidate string) (civil.Date, bool) {
	if date, ok := parseDDMMYYYY(candidate); ok {
		return date, true
	}
	if date, ok := parseDMYYYY(candidate); ok {
		return date, true
	}
	if date, ok := parseYYYYMMDD(candidate); ok {
		return date, true
	}
	if date, ok := parseSlash(candidate, false); ok {
		return date, true
	}
	return parseSlash(candidate, true)
}

func parseTwoDigitDate(token string, today civil.Date) (civil.Date, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	day, ok := twoDigits(parts[0])
	if !ok {
		return civil.Date{}, false
	}
	month, ok := twoDigits(parts[1])
	if !ok {
		return civil.Date{}, false
	}
	if len(parts[2]) != 2 || !allASCIIDigits(parts[2]) {
		return civil.Date{}, false
	}
	yy, ok := parseU8(parts[2])
	if !ok {
		return civil.Date{}, false
	}
	year, err := ResolveTwoDigitYear(yy, today)
	if err != nil {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func twoDigits(part string) (uint32, bool) {
	if len(part) == 2 && allASCIIDigits(part) {
		return parseU32(part)
	}
	return 0, false
}

func twoOrOneDigits(part string) (uint32, bool) {
	if len(part) >= 1 && len(part) <= 2 && allASCIIDigits(part) {
		return parseU32(part)
	}
	return 0, false
}

func fourDigits(part string) (int, bool) {
	if len(part) == 4 && allASCIIDigits(part) {
		value, ok := parseI64(part)
		return int(value), ok
	}
	return 0, false
}

func allASCIIDigits(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return text != ""
}

func parseDDMMYYYY(candidate string) (civil.Date, bool) {
	parts := strings.Split(candidate, ".")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	year, yearOK := fourDigits(parts[2])
	month, monthOK := twoDigits(parts[1])
	day, dayOK := twoDigits(parts[0])
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func parseDMYYYY(candidate string) (civil.Date, bool) {
	parts := strings.Split(candidate, ".")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	year, yearOK := fourDigits(parts[2])
	month, monthOK := twoOrOneDigits(parts[1])
	day, dayOK := twoOrOneDigits(parts[0])
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func parseYYYYMMDD(candidate string) (civil.Date, bool) {
	parts := strings.Split(candidate, "-")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	year, yearOK := fourDigits(parts[0])
	month, monthOK := twoDigits(parts[1])
	day, dayOK := twoDigits(parts[2])
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func parseSlash(candidate string, monthFirst bool) (civil.Date, bool) {
	parts := strings.Split(candidate, "/")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	first, firstOK := twoDigits(parts[0])
	second, secondOK := twoDigits(parts[1])
	year, yearOK := fourDigits(parts[2])
	if !firstOK || !secondOK || !yearOK {
		return civil.Date{}, false
	}
	month, day := first, second
	if !monthFirst {
		month, day = second, first
	}
	return dateFromYMD(year, int(month), int(day))
}

// parseISODate parses an ISO yyyy-mm-dd date, as used by CAMT and XML OFX.
func parseISODate(text string) (civil.Date, bool) {
	characters := []rune(strings.TrimSpace(text))
	prefix := characters
	if len(prefix) > 10 {
		prefix = prefix[:10]
	}
	parts := strings.Split(string(prefix), "-")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	year, yearOK := fourDigits(parts[0])
	month, monthOK := twoDigits(parts[1])
	day, dayOK := twoDigits(parts[2])
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func formatISODate(date civil.Date) string {
	return date.String()
}

func requireNoDoctype(text string) error {
	if strings.Contains(text, "<!DOCTYPE") {
		return Invalid("DOCTYPE is not allowed")
	}
	return nil
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// textLines follows Kotlin String.lines(): split on CRLF, LF or CR, without a
// trailing empty line.
func textLines(text string) []string {
	var lines []string
	bytes := []byte(text)
	start := 0
	i := 0
	for i < len(bytes) {
		if bytes[i] == '\r' || bytes[i] == '\n' {
			lines = append(lines, text[start:i])
			if bytes[i] == '\r' && i+1 < len(bytes) && bytes[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
		i++
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
