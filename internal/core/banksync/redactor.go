package banksync

import (
	"sort"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/dlclark/regexp2"
)

// Mask replaces every masked value; a single X, as Hibiscus' support
// instructions suggest.
const Mask = "X"

// minSecretLength is the shortest value worth masking; below this a "secret" is
// more likely to damage readable text.
const minSecretLength = 4

// redactorMatchTimeout bounds one regular-expression match, like the rules
// package.
const redactorMatchTimeout = 100 * time.Millisecond

var (
	ibanPattern        = mustCompileRedactor(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`)
	fintsSyntaxPattern = mustCompileRedactor(`[+:'?@]`)
	spacedIBANPattern  = mustCompileRedactor(
		`(?i)\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b`,
	)
	longDigitsPattern      = mustCompileRedactor(`(?<![\dA-Za-z])\d{6,}(?![\dA-Za-z])`)
	labelledAccountPattern = mustCompileRedactor(
		`(?i)((?:konto(?:nummer|nr)?|\bkto|\baccount(?: no\.?| number)?|\bacct)(?:\.?-?nr\.?)?:?\s+)(\d{1,12}(?: \d{1,12})*)(?![,.]?\d)`,
	)
	returnCodePattern = mustCompileRedactor(`(?<![\w…])[39]\d{3}(?!\w)`)
)

func mustCompileRedactor(pattern string) *regexp2.Regexp {
	regex, err := regexp2.Compile(pattern, regexp2.Unicode)
	if err != nil {
		panic("invalid banksync redactor pattern: " + err.Error())
	}
	regex.MatchTimeout = redactorMatchTimeout
	return regex
}

// replaceAllRunes rewrites every match in text. regexp2 reports rune offsets, so
// the replacement is assembled over []rune and no Go string is sliced with a
// rune offset.
func replaceAllRunes(regex *regexp2.Regexp, text string, replace func(match *regexp2.Match) string) string {
	runes := []rune(text)
	var out strings.Builder
	out.Grow(len(text))

	last := 0
	match, err := regex.FindStringMatch(text)
	for err == nil && match != nil {
		start := match.Index
		end := match.Index + match.Length
		out.WriteString(string(runes[last:start]))
		out.WriteString(replace(match))
		last = end
		match, err = regex.FindNextMatch(match)
	}
	if err != nil {
		// A match that timed out fails closed: the text not yet searched could
		// still hold an account number, so it is masked rather than kept.
		out.WriteString(Mask)
		return out.String()
	}
	out.WriteString(string(runes[last:]))
	return out.String()
}

// ReturnCodes returns FinTS return codes (3xxx warnings, 9xxx errors) in
// already masked bank text.
//
// A code standing right after the mask is the kept tail of an account number
// ("...3000"), not a code.
func ReturnCodes(maskedText *string) []string {
	codes := []string{}
	if maskedText == nil {
		return codes
	}

	match, err := returnCodePattern.FindStringMatch(*maskedText)
	for err == nil && match != nil {
		value := match.String()
		if !containsString(codes, value) {
			codes = append(codes, value)
		}
		match, err = returnCodePattern.FindNextMatch(match)
	}
	return codes
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// Redact masks known secrets and IBAN-looking tokens.
func Redact(text string, secrets []string) string {
	result := text

	// Longest first, so a value that contains another is masked in one go
	// instead of leaving fragments.
	values := make([]string, 0, len(secrets)*2)
	for _, secret := range secrets {
		if len(secret) < minSecretLength || strings.TrimSpace(secret) == "" {
			continue
		}
		values = append(values, secret, FintsEscaped(secret))
	}
	sort.SliceStable(values, func(left, right int) bool {
		return len(values[left]) > len(values[right])
	})
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		if len(deduped) == 0 || value != deduped[len(deduped)-1] {
			deduped = append(deduped, value)
		}
	}
	for _, value := range deduped {
		result = strings.ReplaceAll(result, value, Mask)
	}

	result = replaceAllRunes(ibanPattern, result, func(*regexp2.Match) string {
		return Mask
	})
	// Spaced or lower-case IBANs only when the check digits match, so ids and
	// payloads stay readable.
	return replaceAllRunes(spacedIBANPattern, result, func(match *regexp2.Match) string {
		return maskSpacedIBAN(match.String())
	})
}

// MaskAccountNumbers masks account identifiers in bank text before it is
// stored, keeping the last four characters.
func MaskAccountNumbers(text string) string {
	result := replaceAllRunes(spacedIBANPattern, text, func(match *regexp2.Match) string {
		matched := match.String()
		if length, ok := validIBANPrefixLength(matched); ok {
			prefix := matched[:length]
			tail := lastFour(iban.Normalize(prefix))
			return "…" + tail + matched[length:]
		}
		// No valid IBAN: fail closed, whatever the match contains could still be
		// an account number.
		return "…" + lastFour(strings.ReplaceAll(matched, " ", ""))
	})

	result = replaceAllRunes(labelledAccountPattern, result, func(match *regexp2.Match) string {
		label := ""
		digits := ""
		if group := match.GroupByNumber(1); group != nil {
			label = group.String()
		}
		if group := match.GroupByNumber(2); group != nil {
			digits = strings.ReplaceAll(group.String(), " ", "")
		}
		if len(digits) >= 6 {
			return label + "…" + lastFour(digits)
		}
		return match.String()
	})

	return replaceAllRunes(longDigitsPattern, result, func(match *regexp2.Match) string {
		return "…" + lastFour(match.String())
	})
}

// FintsEscaped returns the form a value takes inside a raw FinTS message, where
// the syntax characters + : ' ? @ are escaped with ?.
func FintsEscaped(value string) string {
	return replaceAllRunes(fintsSyntaxPattern, value, func(match *regexp2.Match) string {
		return "?" + match.String()
	})
}

// validIBANPrefixLength returns the byte length of the longest prefix of a
// spaced-IBAN match that is a valid IBAN, or false when none is.
func validIBANPrefixLength(matched string) (int, bool) {
	if iban.IsValid(matched) {
		return len(matched), true
	}
	cut := strings.LastIndex(matched, " ")
	for cut >= 0 {
		if iban.IsValid(matched[:cut]) {
			return cut, true
		}
		cut = strings.LastIndex(matched[:cut], " ")
	}
	return 0, false
}

func maskSpacedIBAN(matched string) string {
	length, ok := validIBANPrefixLength(matched)
	if !ok {
		return matched
	}
	return Mask + matched[length:]
}

func lastFour(text string) string {
	characters := []rune(text)
	start := len(characters) - 4
	if start < 0 {
		start = 0
	}
	return string(characters[start:])
}
