package pdftext

// This file ports the text-decoding helpers from pdf-extract 0.12.1
// src/lib.rs (pdf_to_utf8, to_utf8, encoding_to_unicode_table) and the
// PDFDocEncoding table declared there.

// pdfToUTF8 decodes a PDF name or text string the way pdf-extract's
// pdf_to_utf8 does: a UTF-16BE BOM payload is decoded directly, otherwise
// each byte is looked up in PDFDocEncoding.
func pdfToUTF8(s []byte) string {
	if len(s) > 2 && s[0] == 0xfe && s[1] == 0xff {
		return utf16BEToString(s[2:])
	}
	units := make([]uint16, len(s))
	for i, b := range s {
		units[i] = pdfDocEncoding[b]
	}
	return utf16UnitsToString(units)
}

// encodingToUTF8 decodes bytes through a 256-entry code-to-Unicode table.
func encodingToUTF8(encoding []uint16, s []byte) string {
	if len(s) > 2 && s[0] == 0xfe && s[1] == 0xff {
		return utf16BEToString(s[2:])
	}
	units := make([]uint16, len(s))
	for i, b := range s {
		units[i] = encoding[b]
	}
	return utf16UnitsToString(units)
}

func utf16BEToString(s []byte) string {
	if len(s)%2 != 0 {
		panic("odd-length UTF-16BE string")
	}
	units := make([]uint16, len(s)/2)
	for i := range units {
		units[i] = uint16(s[2*i])<<8 | uint16(s[2*i+1])
	}
	return utf16UnitsToString(units)
}

// utf16UnitsToString mirrors String::from_utf16(...).unwrap(): unpaired
// surrogates panic, which Extract turns into ok=false just as Rust's
// catch_unwind does.
func utf16UnitsToString(units []uint16) string {
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xd800 && u <= 0xdbff:
			if i+1 >= len(units) {
				panic("invalid UTF-16 surrogate")
			}
			lo := units[i+1] // #nosec G602 -- i+1 is checked immediately above
			if lo < 0xdc00 || lo > 0xdfff {
				panic("invalid UTF-16 surrogate pair")
			}
			runes = append(runes, 0x10000+(rune(u-0xd800)<<10)+rune(lo-0xdc00))
			i++
		case u >= 0xdc00 && u <= 0xdfff:
			panic("invalid UTF-16 surrogate")
		default:
			runes = append(runes, rune(u))
		}
	}
	return string(runes)
}

// encodingToUnicodeTable is pdf-extract's encoding_to_unicode_table. Only
// these three names are accepted; every other /Encoding name panics there.
func encodingToUnicodeTable(name string) []uint16 {
	var table [256]uint16
	switch name {
	case "MacRomanEncoding":
		table = macRomanEncoding
	case "MacExpertEncoding":
		table = macExpertEncoding
	case "WinAnsiEncoding":
		table = winAnsiEncoding
	default:
		panic("unexpected encoding")
	}
	return append([]uint16(nil), table[:]...)
}

func pdfDocEncodingTable() []uint16 {
	return append([]uint16(nil), pdfDocEncoding[:]...)
}
