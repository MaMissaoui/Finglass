// Package iban normalises and validates IBANs, ported from
// crates/finglass-core/src/iban.rs.
package iban

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var upper = cases.Upper(language.Und)

// Normalize strips ASCII spaces and upper-cases with full Unicode rules.
func Normalize(iban string) string {
	return upper.String(strings.ReplaceAll(iban, " ", ""))
}

// IsValid runs the ISO 13616 mod-97 check.
func IsValid(iban string) bool {
	normalised := Normalize(iban)
	characters := []rune(normalised)

	if len(characters) < 15 || len(characters) > 34 {
		return false
	}
	for _, character := range characters {
		if !isAlphanumeric(character) {
			return false
		}
	}
	if !isAlphabetic(characters[0]) ||
		!isAlphabetic(characters[1]) ||
		!isASCIIDigit(characters[2]) ||
		!isASCIIDigit(characters[3]) {
		return false
	}
	if strings.HasPrefix(normalised, "DE") && len(characters) != 22 {
		return false
	}

	var remainder int64
	ordered := append(append([]rune{}, characters[4:]...), characters[:4]...)
	for _, character := range ordered {
		if digit, ok := asciiDigit(character); ok {
			remainder = (remainder*10 + digit) % 97
		} else {
			value := int64(character)
			if value < 'A' {
				value = 0
			} else {
				value -= 'A'
			}
			value += 10
			remainder = (remainder*100 + value) % 97
		}
	}
	return remainder == 1
}

// Format groups an IBAN in blocks of four, as FinDesk displays it.
func Format(iban string) string {
	characters := []rune(Normalize(iban))
	groups := make([]string, 0, (len(characters)+3)/4)
	for index := 0; index < len(characters); index += 4 {
		end := index + 4
		if end > len(characters) {
			end = len(characters)
		}
		groups = append(groups, string(characters[index:end]))
	}
	return strings.Join(groups, " ")
}

// Mask masks an IBAN (or a synthetic account id) as "…" plus its last four
// characters.
func Mask(iban string) string {
	characters := []rune(iban)
	start := len(characters) - 4
	if start < 0 {
		start = 0
	}
	return "…" + string(characters[start:])
}

func isAlphabetic(character rune) bool {
	return unicode.Is(unicode.L, character) ||
		unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Other_Alphabetic, character)
}

func isAlphanumeric(character rune) bool {
	return isAlphabetic(character) ||
		unicode.Is(unicode.Nd, character) ||
		unicode.Is(unicode.No, character)
}

func isASCIIDigit(character rune) bool {
	return character >= '0' && character <= '9'
}

func asciiDigit(character rune) (int64, bool) {
	if !isASCIIDigit(character) {
		return 0, false
	}
	return int64(character - '0'), true
}
