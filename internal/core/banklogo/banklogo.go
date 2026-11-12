// Package banklogo chooses which bundled bank logo an account shows, and the
// monogram for banks without one. Ported from
// crates/finglass-core/src/bank_logo.rs.
//
// The matching rules are shared with FinDesk. They only inspect the bank's
// name, BLZ and BIC, so the function is pure and has no UI types. The SVG
// assets live in the UI crate; the id of a Logo is the SVG's file stem there.
package banklogo

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Logo is one of the nine bundled bank logos.
type Logo string

// The bundled bank logos, valued by the file stem of their SVG.
const (
	// HypoVereinsbank is HypoVereinsbank / UniCredit.
	HypoVereinsbank Logo = "hypovereinsbank"
	// DKB is Deutsche Kreditbank.
	DKB Logo = "dkb"
	// Postbank is Postbank.
	Postbank Logo = "postbank"
	// DeutscheBank is Deutsche Bank.
	DeutscheBank Logo = "deutsche-bank"
	// Commerzbank is Commerzbank.
	Commerzbank Logo = "commerzbank"
	// Santander is Santander.
	Santander Logo = "santander"
	// N26 is N26.
	N26 Logo = "n26"
	// Sparkasse is the Sparkassen.
	Sparkasse Logo = "sparkasse"
	// Volksbank is the Volksbanken Raiffeisenbanken.
	Volksbank Logo = "volksbank"
)

// ID returns the file stem of the bundled SVG in frontend/src/assets/banks/.
func (l Logo) ID() string {
	return string(l)
}

// For returns the logo for a bank, or false when it gets a monogram.
//
// Matching is case-insensitive and trims the inputs. The first matching rule
// wins; the order is part of the shared rules because several brands use
// another bank's BLZ range or BIC.
func For(name string, blz, bic *string) (Logo, bool) {
	name = cases.Lower(language.Und).String(strings.TrimSpace(name))
	blzValue := ""
	if blz != nil {
		blzValue = strings.TrimSpace(*blz)
	}
	bicValue := ""
	if bic != nil {
		bicValue = asciiUpperCase(strings.TrimSpace(*bic))
	}

	// 1. These brands use another bank's BIC or BLZ range but have their own look.
	if containsAny(name, "comdirect", "consors", "targo", "sparda", "psd", "apobank", "apotheker") {
		return "", false
	}
	// 2. HypoVereinsbank / UniCredit.
	if containsAny(name, "hypovereinsbank", "hvb", "unicredit") || strings.HasPrefix(bicValue, "HYVE") {
		return HypoVereinsbank, true
	}
	// 3. DKB.
	if containsAny(name, "dkb", "deutsche kreditbank") ||
		strings.HasPrefix(bicValue, "BYLADEM1001") ||
		blzValue == "12030000" {
		return DKB, true
	}
	// 4. Postbank.
	if strings.Contains(name, "postbank") || strings.HasPrefix(bicValue, "PBNK") {
		return Postbank, true
	}
	// 5. Deutsche Bank.
	if strings.Contains(name, "deutsche bank") || strings.HasPrefix(bicValue, "DEUT") {
		return DeutscheBank, true
	}
	// 6. Commerzbank.
	if strings.Contains(name, "commerzbank") || strings.HasPrefix(bicValue, "COBA") {
		return Commerzbank, true
	}
	// 7. Santander.
	if strings.Contains(name, "santander") || strings.HasPrefix(bicValue, "SCFB") {
		return Santander, true
	}
	// 8. N26.
	if strings.Contains(name, "n26") || strings.HasPrefix(bicValue, "NTSB") {
		return N26, true
	}
	// 9. Sparkasse: by name, or by the BLZ's fourth digit.
	if strings.Contains(name, "sparkasse") || (len(blzValue) >= 4 && blzValue[3] == '5') {
		return Sparkasse, true
	}
	// 10. Volksbank / Raiffeisenbank.
	if containsAny(name, "volksbank", "raiffeisenbank", "vr bank", "vr-bank", "vrbank") ||
		strings.HasPrefix(bicValue, "GENO") {
		return Volksbank, true
	}
	// 11. Everything else gets a monogram.
	return "", false
}

// Monogram is the monogram fallback: up to three letters and a stable colour
// index in 0..8.
type Monogram struct {
	// Letters are drawn on the tile.
	Letters string
	// Colour is the index into the theme's eight monogram colours.
	Colour uint8
}

// MonogramFor builds the monogram for a bank name.
//
// A short all-uppercase word such as "HVB", "DKB" or "ING" wins; otherwise the
// initials of the first two words, or the first two letters of a single word.
// The colour is a sum of the lower-cased name's UTF-8 bytes modulo eight, so
// the same name always gets the same tile.
func MonogramFor(name string) Monogram {
	letters := monogramLetters(name)
	total := 0
	for _, value := range []byte(cases.Lower(language.Und).String(name)) {
		total += int(value)
	}
	return Monogram{Letters: letters, Colour: uint8(total % 8)}
}

func monogramLetters(name string) string {
	words := strings.Fields(name)

	for _, word := range words {
		first, _ := utf8.DecodeRuneInString(word)
		length := utf8.RuneCountInString(word)
		if length >= 2 && length <= 4 && isAlphabetic(first) && allUppercase(word) {
			characters := []rune(word)
			return string(characters[:min(3, len(characters))])
		}
	}

	startingWithLetter := make([]string, 0, len(words))
	for _, word := range words {
		first, _ := utf8.DecodeRuneInString(word)
		if isAlphabetic(first) {
			startingWithLetter = append(startingWithLetter, word)
		}
	}

	switch len(startingWithLetter) {
	case 0:
		return ""
	case 1:
		var letters strings.Builder
		count := 0
		for _, character := range startingWithLetter[0] {
			if !isAlphabetic(character) {
				continue
			}
			letters.WriteRune(uppercase(character))
			count++
			if count == 2 {
				break
			}
		}
		return letters.String()
	default:
		var letters strings.Builder
		for _, word := range startingWithLetter[:2] {
			for _, character := range word {
				if isAlphabetic(character) {
					letters.WriteRune(uppercase(character))
					break
				}
			}
		}
		return letters.String()
	}
}

// uppercase returns the first character of the full Unicode uppercase mapping,
// like Rust's char::to_uppercase().next().
func uppercase(character rune) rune {
	for _, upper := range cases.Upper(language.Und).String(string(character)) {
		return upper
	}
	return character
}

// isAlphabetic reports the Unicode Alphabetic property, as Rust's
// char::is_alphabetic does.
func isAlphabetic(character rune) bool {
	return unicode.Is(unicode.L, character) ||
		unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Other_Alphabetic, character)
}

// isUppercase reports the Unicode Uppercase property, as Rust's
// char::is_uppercase does.
func isUppercase(character rune) bool {
	return unicode.Is(unicode.Upper, character) ||
		unicode.Is(unicode.Other_Uppercase, character)
}

func allUppercase(value string) bool {
	for _, character := range value {
		if !isUppercase(character) {
			return false
		}
	}
	return true
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// asciiUpperCase upper-cases ASCII letters only, like Rust's
// str::to_ascii_uppercase.
func asciiUpperCase(value string) string {
	characters := []byte(value)
	for index, character := range characters {
		if character >= 'a' && character <= 'z' {
			characters[index] = character - ('a' - 'A')
		}
	}
	return string(characters)
}
