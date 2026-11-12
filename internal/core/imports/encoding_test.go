package imports

import (
	"strings"
	"testing"
	"unicode"
)

func windows1252Expected() []rune {
	table := []rune{
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
	expected := make([]rune, 256)
	for index := 0; index < 128; index++ {
		expected[index] = rune(index)
	}
	copy(expected[128:], table)
	return expected
}

func TestWindows1252MatchesEncodingRSTable(t *testing.T) {
	all := make([]byte, 256)
	for index := range all {
		all[index] = byte(index)
	}
	expected := windows1252Expected()
	got := []rune(decodeWindows1252(all))
	if len(got) != len(expected) {
		t.Fatalf("decoded %d runes, want %d", len(got), len(expected))
	}
	for index := range expected {
		if got[index] != expected[index] {
			t.Errorf("byte 0x%02X decoded to U+%04X, want U+%04X", index, got[index], expected[index])
		}
	}
}

func TestWindows1252KeepsTheFiveC1Bytes(t *testing.T) {
	for _, value := range []byte{0x81, 0x8D, 0x8F, 0x90, 0x9D} {
		decoded := []rune(decodeWindows1252([]byte{value}))
		if len(decoded) != 1 || decoded[0] != rune(value) {
			t.Errorf("byte 0x%02X decoded to %U, want U+%04X", value, decoded, value)
		}
	}
}

func TestEncodingLabelsUseTheWHATGMLookup(t *testing.T) {
	tests := []struct {
		label string
		name  string
		kind  charsetKind
	}{
		{"iso-8859-1", "windows-1252", charsetWindows1252},
		{"latin1", "windows-1252", charsetWindows1252},
		{"us-ascii", "windows-1252", charsetWindows1252},
		{"windows-1252", "windows-1252", charsetWindows1252},
		{"utf-8", "UTF-8", charsetUTF8},
		{"UTF-8", "UTF-8", charsetUTF8},
		{"utf-16", "UTF-16LE", charsetUTF16LE},
		{"utf-16le", "UTF-16LE", charsetUTF16LE},
		{"utf-16be", "UTF-16BE", charsetUTF16BE},
	}
	for _, test := range tests {
		set, ok := encodingForLabel(test.label)
		if !ok {
			t.Errorf("encodingForLabel(%q) = none, want %s", test.label, test.name)
			continue
		}
		if set.name != test.name || set.kind != test.kind {
			t.Errorf("encodingForLabel(%q) = %s/%d, want %s/%d", test.label, set.name, set.kind, test.name, test.kind)
		}
	}
	if _, ok := encodingForLabel("not-an-encoding"); ok {
		t.Error("encodingForLabel accepted an unknown label")
	}
}

func TestDecodeSniffsAndRemovesBOMs(t *testing.T) {
	utf8BOM := []byte{0xEF, 0xBB, 0xBF, 'a', 'b', 'c'}
	if got := decodeCharSet(charset{kind: charsetWindows1252}, utf8BOM); got != "abc" {
		t.Errorf("UTF-8 BOM through windows-1252 = %q, want abc", got)
	}
	utf16leBOM := append([]byte{0xFF, 0xFE}, utf16le("abc")...)
	if got := decodeCharSet(charset{kind: charsetUTF8}, utf16leBOM); got != "abc" {
		t.Errorf("UTF-16LE BOM through UTF-8 = %q, want abc", got)
	}
	utf16beBOM := append([]byte{0xFE, 0xFF}, utf16be("abc")...)
	if got := decodeCharSet(charset{kind: charsetUTF8}, utf16beBOM); got != "abc" {
		t.Errorf("UTF-16BE BOM through UTF-8 = %q, want abc", got)
	}
}

func TestUTF8LossyUsesMaximalSubparts(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"truncated three-byte sequence", []byte{0xE2, 0x82, 'A'}, "\uFFFDA"},
		{"truncated four-byte sequence", []byte{0xF0, 0x9F, 0x98}, "\uFFFD"},
		{"overlong two-byte sequence", []byte{0xC0, 0x80}, "\uFFFD\uFFFD"},
		{"surrogate", []byte{0xED, 0xA0, 0x80}, "\uFFFD\uFFFD\uFFFD"},
		{"above the Unicode range", []byte{0xF4, 0x90, 0x80, 0x80}, "\uFFFD\uFFFD\uFFFD\uFFFD"},
	}
	for _, test := range tests {
		if got := decodeUTF8Lossy(test.bytes); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestUTF16ReplacesUnpairedSurrogatesAndAnOddTrailingByte(t *testing.T) {
	high := []byte{0x00, 0xD8, 0x41, 0x00}
	if got := decodeUTF16(high, false); got != "\uFFFDA" {
		t.Errorf("unpaired high surrogate = %q, want %q", got, "\uFFFDA")
	}
	low := []byte{0x00, 0xDC}
	if got := decodeUTF16(low, false); got != "\uFFFD" {
		t.Errorf("unpaired low surrogate = %q, want %q", got, "\uFFFD")
	}
	oddLE := append(utf16le("A"), 0x00)
	if got := decodeUTF16(oddLE, false); got != "A\uFFFD" {
		t.Errorf("odd trailing LE byte = %q, want %q", got, "A\uFFFD")
	}
	oddBE := append(utf16be("A"), 0x00)
	if got := decodeUTF16(oddBE, true); got != "A\uFFFD" {
		t.Errorf("odd trailing BE byte = %q, want %q", got, "A\uFFFD")
	}
}

func TestDecodeStatementReportsTheRustCharsetNames(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"UTF-8", []byte("plain"), "UTF-8"},
		{"windows-1252", cp1252(t, "Bäcker"), "windows-1252"},
		{"UTF-16LE", utf16le("plain"), "UTF-16LE"},
		{"UTF-16BE", utf16be("plain"), "UTF-16BE"},
		{"UTF-16LE BOM", append([]byte{0xFF, 0xFE}, utf16le("plain")...), "UTF-16LE"},
		{"UTF-16BE BOM", append([]byte{0xFE, 0xFF}, utf16be("plain")...), "UTF-16BE"},
	}
	for _, test := range tests {
		if _, set := decodeStatement(test.bytes); set.name != test.want {
			t.Errorf("%s: charset name = %q, want %q", test.name, set.name, test.want)
		}
	}
}

func TestIntegerParsingFollowsRust(t *testing.T) {
	if value, ok := parseUsize("+5"); !ok || value != 5 {
		t.Errorf("parseUsize(+5) = %d, %v", value, ok)
	}
	if _, ok := parseUsize("-0"); ok {
		t.Error("parseUsize(-0) succeeded")
	}
	if _, ok := parseUsize(" 5"); ok {
		t.Error("parseUsize( 5) succeeded")
	}
	if _, ok := parseUsize("5_0"); ok {
		t.Error("parseUsize(5_0) succeeded")
	}
	if _, ok := parseUsize(""); ok {
		t.Error("parseUsize() succeeded")
	}
	if _, ok := parseUsize("18446744073709551616"); ok {
		t.Error("parseUsize accepted an overflowing value")
	}
	if value, ok := parseI64("-0"); !ok || value != 0 {
		t.Errorf("parseI64(-0) = %d, %v", value, ok)
	}
	if value, ok := parseI64("+5"); !ok || value != 5 {
		t.Errorf("parseI64(+5) = %d, %v", value, ok)
	}
	if _, ok := parseI64(" 5"); ok {
		t.Error("parseI64( 5) succeeded")
	}
	if _, ok := parseI64("5_0"); ok {
		t.Error("parseI64(5_0) succeeded")
	}
	if _, ok := parseI64(""); ok {
		t.Error("parseI64() succeeded")
	}
}

func TestUTF16HeuristicDoesNotClaimPlainASCII(t *testing.T) {
	if _, ok := utf16Charset([]byte("plain ascii text")); ok {
		t.Error("utf16Charset claimed plain ASCII")
	}
	if _, ok := utf16Charset([]byte{0x41, 0x00, 0x42, 0x00}); !ok {
		t.Error("utf16Charset did not detect UTF-16LE")
	}
}

func TestWindows1252TableHasNoReplacementRune(t *testing.T) {
	for index, character := range windows1252Expected() {
		if character == unicode.ReplacementChar {
			t.Errorf("byte 0x%02X maps to U+FFFD", index)
		}
	}
	if strings.ContainsRune(decodeWindows1252([]byte{0x81, 0x8D, 0x8F, 0x90, 0x9D}), unicode.ReplacementChar) {
		t.Error("the five C1 bytes decoded to U+FFFD")
	}
}
