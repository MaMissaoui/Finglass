package fintsprobe

import "unicode"

// MaskIBAN masks an IBAN to its last four non-space characters, keeping the
// two-letter country prefix and the original grouping spaces. Short values are
// fully masked.
func MaskIBAN(iban string) string {
	total := 0
	leadingLetters := 0
	stillLeading := true
	for _, character := range iban {
		if unicode.IsSpace(character) {
			continue
		}
		total++
		if stillLeading {
			if isASCIIAlpha(character) {
				leadingLetters++
			} else {
				stillLeading = false
			}
		}
	}

	keepFrom := total
	if total >= 4 {
		keepFrom = total - 4
	}
	prefixCount := 0
	if total > 4 && leadingLetters < keepFrom {
		prefixCount = leadingLetters
	}

	var masked []rune
	masked = make([]rune, 0, len(iban))
	seen := 0
	for _, character := range iban {
		if unicode.IsSpace(character) {
			masked = append(masked, character)
			continue
		}
		if seen < prefixCount || seen >= keepFrom {
			masked = append(masked, character)
		} else {
			masked = append(masked, '*')
		}
		seen++
	}
	return string(masked)
}

func isASCIIAlpha(character rune) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z')
}
