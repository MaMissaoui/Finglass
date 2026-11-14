// Package debugquote formats values in the quoted debug form used by
// Finglass's error texts.
package debugquote

import (
	"strconv"
	"strings"
	"unicode"
)

// Quote quotes text in the quoted debug form used by Finglass's error texts.
func Quote(text string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, character := range text {
		switch character {
		case '\\':
			quoted.WriteString(`\\`)
		case '"':
			quoted.WriteString(`\"`)
		case '\n':
			quoted.WriteString(`\n`)
		case '\r':
			quoted.WriteString(`\r`)
		case '\t':
			quoted.WriteString(`\t`)
		case '\x00':
			quoted.WriteString(`\0`)
		default:
			if !strconv.IsPrint(character) || isCombiningMark(character) {
				quoted.WriteString(`\u{`)
				quoted.WriteString(strconv.FormatInt(int64(character), 16))
				quoted.WriteByte('}')
			} else {
				quoted.WriteRune(character)
			}
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}

func isCombiningMark(character rune) bool {
	return unicode.Is(unicode.Mn, character) ||
		unicode.Is(unicode.Me, character) ||
		unicode.Is(unicode.Other_Grapheme_Extend, character)
}
