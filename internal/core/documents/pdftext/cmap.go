package pdftext

import "github.com/ledongthuc/pdf"

// This file ports the parts of adobe-cmap-parser 0.4.1 used by
// pdf-extract 0.12.1: get_unicode_map, get_byte_mapping and the small
// PostScript-subset tokenizer both rely on.

type codeRange struct {
	width uint32
	start uint32
	end   uint32
}

type cidRange struct {
	srcCodeLo uint32
	srcCodeHi uint32
	dstCIDLo  uint32
}

type byteMapping struct {
	codespace []codeRange
	cid       []cidRange
}

func getUnicodeMap(font pdf.Value) map[uint32]string {
	toUnicode := font.Key("ToUnicode")
	switch toUnicode.Kind() {
	case pdf.Null:
		return nil
	case pdf.Stream:
		raw := parseUnicodeCMap(readStream(toUnicode))
		unicode := make(map[uint32]string, len(raw))
		for code, value := range raw {
			if len(value)%2 != 0 {
				panic("odd-length ToUnicode value")
			}
			units := make([]uint16, len(value)/2)
			for i := range units {
				units[i] = uint16(value[2*i])<<8 | uint16(value[2*i+1])
			}
			// pdf-extract ignores a lone low/high surrogate as a single unit.
			if len(units) == 1 && units[0] >= 0xd800 && units[0] <= 0xdfff {
				continue
			}
			unicode[code] = utf16UnitsToString(units)
		}
		return unicode
	case pdf.Name:
		if pdfToUTF8([]byte(toUnicode.Name())) != "Identity-H" {
			panic("unsupported ToUnicode name")
		}
		return nil
	default:
		panic("unsupported cmap")
	}
}

func parseUnicodeCMap(data []byte) map[uint32][]byte {
	tokens := scanCMap(data)
	result := make(map[uint32][]byte)
	i := 0
	for i < len(tokens) {
		token := tokens[i]
		if token.kind != cmapOperator {
			i++
			continue
		}
		switch token.op {
		case "beginbfchar":
			count := cmapCount(tokens, i)
			i++
			for n := int64(0); n < count; n++ {
				if i+1 >= len(tokens) {
					panic("truncated bfchar")
				}
				code, ok := tokens[i].stringValue()
				if !ok {
					panic("beginbfchar expected hexstring")
				}
				value, ok := tokens[i+1].stringValue()
				if !ok {
					panic("beginbfchar expected hexstring")
				}
				result[asCode(code)] = append([]byte(nil), value...)
				i += 2
			}
			i++
		case "beginbfrange":
			count := cmapCount(tokens, i)
			i++
			for n := int64(0); n < count; n++ {
				if i+2 >= len(tokens) {
					panic("truncated bfrange")
				}
				lowerBytes, ok := tokens[i].stringValue()
				if !ok {
					panic("beginbfrange expected hexstring")
				}
				upperBytes, ok := tokens[i+1].stringValue()
				if !ok {
					panic("beginbfrange expected hexstring")
				}
				lower := asCode(lowerBytes)
				upper := asCode(upperBytes)
				switch target := tokens[i+2]; target.kind {
				case cmapString:
					switch len(target.str) {
					case 4:
						value := asCode(target.str)
						for c := lower; lower <= upper; c++ {
							result[c] = be32(value + (c - lower))
							if c == upper {
								break
							}
						}
					case 2:
						value := uint16FromUint32(asCode(target.str))
						for c := lower; lower <= upper; c++ {
							code := value + uint16FromUint32(c-lower)
							result[c] = []byte{byteFromUint32(uint32(code >> 8)), byteFromUint32(uint32(code))}
							if c == upper {
								break
							}
						}
					default:
						panic("bad length of hexstring")
					}
				case cmapArray:
					if upper-lower+1 != uint32FromInt(len(target.array)) {
						panic("bad length of array")
					}
					for index, element := range target.array {
						value, ok := element.stringValue()
						if !ok {
							panic("beginbfrange expected hexstring")
						}
						result[lower+uint32(index)] = append([]byte(nil), value...)
					}
				default:
					panic("beginbfrange expected array or literal")
				}
				i += 3
			}
			i++
		default:
			i++
		}
	}
	return result
}

func parseByteMapping(data []byte) byteMapping {
	tokens := scanCMap(data)
	var result byteMapping
	i := 0
	for i < len(tokens) {
		token := tokens[i]
		if token.kind != cmapOperator {
			i++
			continue
		}
		switch token.op {
		case "begincodespacerange":
			count := cmapCount(tokens, i)
			i++
			for n := int64(0); n < count; n++ {
				if i+1 >= len(tokens) {
					panic("truncated codespacerange")
				}
				start, ok := tokens[i].stringValue()
				if !ok {
					panic("begincodespacerange expected hexstring")
				}
				end, ok := tokens[i+1].stringValue()
				if !ok {
					panic("begincodespacerange expected hexstring")
				}
				if len(start) != len(end) {
					panic("codespace range length mismatch")
				}
				result.codespace = append(result.codespace, codeRange{
					width: uint32FromInt(len(start) / 2),
					start: asCode(start),
					end:   asCode(end),
				})
				i += 2
			}
			i++
		case "begincidrange":
			count := cmapCount(tokens, i)
			i++
			for n := int64(0); n < count; n++ {
				if i+2 >= len(tokens) {
					panic("truncated cidrange")
				}
				start, ok := tokens[i].stringValue()
				if !ok {
					panic("begincidrange expected hexstring")
				}
				end, ok := tokens[i+1].stringValue()
				if !ok {
					panic("begincidrange expected hexstring")
				}
				if tokens[i+2].kind != cmapInteger {
					panic("begincidrange expected int")
				}
				result.cid = append(result.cid, cidRange{
					srcCodeLo: asCode(start),
					srcCodeHi: asCode(end),
					dstCIDLo:  uint32FromInt64(tokens[i+2].integer),
				})
				// adobe-cmap-parser 0.4.1 advances by two here, not three.
				i += 2
			}
			i++
		default:
			i++
		}
	}
	return result
}

func cmapCount(tokens []cmapToken, operator int) int64 {
	if operator == 0 || tokens[operator-1].kind != cmapInteger {
		panic("expected integer before cmap operator")
	}
	return tokens[operator-1].integer
}

func asCode(data []byte) uint32 {
	var code uint32
	for _, b := range data {
		code = code<<8 | uint32(b)
	}
	return code
}

func be32(value uint32) []byte {
	return []byte{
		byteFromUint32(value >> 24),
		byteFromUint32(value >> 16),
		byteFromUint32(value >> 8),
		byteFromUint32(value),
	}
}

// --- PostScript-subset tokenizer ----------------------------------------

type cmapKind int

const (
	cmapInteger cmapKind = iota
	cmapNumber
	cmapName
	cmapString
	cmapArray
	cmapOperator
	cmapBool
	cmapDictionary
	cmapProcedure
)

type cmapToken struct {
	kind    cmapKind
	integer int64
	number  string
	name    []byte
	str     []byte
	array   []cmapToken
	op      string
}

func (t cmapToken) stringValue() ([]byte, bool) {
	if t.kind != cmapString {
		return nil, false
	}
	return t.str, true
}

type cmapScanner struct {
	data  []byte
	pos   int
	depth int
}

func scanCMap(data []byte) []cmapToken {
	scanner := &cmapScanner{data: data}
	var tokens []cmapToken
	for {
		scanner.skipSpace()
		if scanner.pos >= len(scanner.data) {
			break
		}
		tokens = append(tokens, scanner.parseValue())
	}
	if len(tokens) == 0 {
		panic("empty CMap")
	}
	return tokens
}

func (s *cmapScanner) skipSpace() {
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0 {
			s.pos++
			continue
		}
		if c == '%' {
			for s.pos < len(s.data) && s.data[s.pos] != '\n' && s.data[s.pos] != '\r' {
				s.pos++
			}
			continue
		}
		break
	}
}

func (s *cmapScanner) parseValue() cmapToken {
	s.depth++
	if s.depth > 1000 {
		panic("CMap nesting too deep")
	}
	defer func() { s.depth-- }()

	s.skipSpace()
	if s.pos >= len(s.data) {
		panic("unexpected end of CMap")
	}
	switch c := s.data[s.pos]; c {
	case '<':
		if s.pos+1 < len(s.data) && s.data[s.pos+1] == '<' {
			return s.parseDictionary()
		}
		return cmapToken{kind: cmapString, str: s.parseHexString()}
	case '(':
		return cmapToken{kind: cmapString, str: s.parseLiteralString()}
	case '[':
		return s.parseArray('[', ']', cmapArray)
	case '{':
		return s.parseArray('{', '}', cmapProcedure)
	case '/':
		return cmapToken{kind: cmapName, name: s.parseName()}
	case ']', '>', '}':
		panic("unexpected CMap delimiter")
	default:
		if isDigit(c) || c == '+' || c == '-' || c == '.' {
			return s.parseNumber()
		}
		if isAlpha(c) || c == '*' || c == '\'' || c == '"' {
			return cmapToken{kind: cmapOperator, op: s.parseOperator()}
		}
		panic("unexpected CMap token")
	}
}

func (s *cmapScanner) parseDictionary() cmapToken {
	s.pos += 2 // <<
	for {
		s.skipSpace()
		if s.pos >= len(s.data) {
			panic("unterminated CMap dictionary")
		}
		if s.data[s.pos] == '>' && s.pos+1 < len(s.data) && s.data[s.pos+1] == '>' {
			s.pos += 2
			return cmapToken{kind: cmapDictionary}
		}
		_ = s.parseValue()
	}
}

func (s *cmapScanner) parseArray(open, close byte, kind cmapKind) cmapToken {
	s.pos++ // opening delimiter
	var values []cmapToken
	for {
		s.skipSpace()
		if s.pos >= len(s.data) {
			panic("unterminated CMap array")
		}
		if s.data[s.pos] == close {
			s.pos++
			return cmapToken{kind: kind, array: values}
		}
		values = append(values, s.parseValue())
	}
}

func (s *cmapScanner) parseHexString() []byte {
	s.pos++ // '<'
	var digits []byte
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		s.pos++
		if c == '>' {
			break
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0 {
			continue
		}
		if !isHex(c) {
			panic("bad hex digit")
		}
		digits = append(digits, c)
	}
	if len(digits)%2 != 0 {
		panic("odd hex string")
	}
	result := make([]byte, len(digits)/2)
	for i := range result {
		result[i] = unhex(digits[2*i])<<4 | unhex(digits[2*i+1])
	}
	return result
}

func (s *cmapScanner) parseLiteralString() []byte {
	s.pos++ // '('
	var result []byte
	for {
		if s.pos >= len(s.data) {
			panic("unterminated literal string")
		}
		c := s.data[s.pos]
		s.pos++
		switch c {
		case ')':
			return result
		case '\\':
			s.parseEscape(&result)
		case '(':
			// The Rust parser keeps the parentheses of a nested string.
			s.pos--
			nested := s.parseLiteralString()
			result = append(result, '(')
			result = append(result, nested...)
			result = append(result, ')')
		default:
			result = append(result, c)
		}
	}
}

func (s *cmapScanner) parseEscape(result *[]byte) {
	if s.pos >= len(s.data) {
		return
	}
	c := s.data[s.pos]
	s.pos++
	switch c {
	case '\\', '(', ')':
		*result = append(*result, c)
	case 'n':
		*result = append(*result, '\n')
	case 'r':
		*result = append(*result, '\r')
	case 't':
		*result = append(*result, '\t')
	case 'b':
		*result = append(*result, '\b')
	case 'f':
		*result = append(*result, '\f')
	case '\r':
		if s.pos < len(s.data) && s.data[s.pos] == '\n' {
			s.pos++
		}
	case '\n':
		// line continuation
	default:
		if c >= '0' && c <= '7' {
			value := int(c - '0')
			for n := 1; n < 4 && s.pos < len(s.data); n++ {
				next := s.data[s.pos]
				if next < '0' || next > '7' {
					break
				}
				value = value*8 + int(next-'0')
				s.pos++
			}
			*result = append(*result, byteFromInt(value))
		} else {
			// Rust's escape parser leaves an unrecognised character to be
			// read again as a normal literal byte.
			*result = append(*result, c)
		}
	}
}

func (s *cmapScanner) parseName() []byte {
	s.pos++ // '/'
	var result []byte
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0 ||
			c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' ||
			c == '{' || c == '}' || c == '/' || c == '%' {
			break
		}
		s.pos++
		if c == '#' {
			if s.pos+1 >= len(s.data) || !isHex(s.data[s.pos]) || !isHex(s.data[s.pos+1]) {
				panic("bad name escape")
			}
			result = append(result, unhex(s.data[s.pos])<<4|unhex(s.data[s.pos+1]))
			s.pos += 2
			continue
		}
		result = append(result, c)
	}
	return result
}

func (s *cmapScanner) parseNumber() cmapToken {
	start := s.pos
	if s.data[s.pos] == '+' || s.data[s.pos] == '-' {
		s.pos++
	}
	isReal := false
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if isDigit(c) {
			s.pos++
			continue
		}
		if c == '.' && !isReal {
			isReal = true
			s.pos++
			continue
		}
		break
	}
	text := string(s.data[start:s.pos])
	if text == "" || text == "+" || text == "-" || text == "." {
		panic("bad number")
	}
	if !isReal {
		var value int64
		negative := false
		index := 0
		if text[0] == '+' || text[0] == '-' {
			negative = text[0] == '-'
			index = 1
		}
		for ; index < len(text); index++ {
			value = value*10 + int64(text[index]-'0')
		}
		if negative {
			value = -value
		}
		return cmapToken{kind: cmapInteger, integer: value, number: text}
	}
	return cmapToken{kind: cmapNumber, number: text}
}

func (s *cmapScanner) parseOperator() string {
	start := s.pos
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if isAlpha(c) || c == '*' || c == '\'' || c == '"' {
			s.pos++
			continue
		}
		break
	}
	return string(s.data[start:s.pos])
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isHex(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}
