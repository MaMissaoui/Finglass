// Package banktext is display-only cleanup of raw bank text for lists.
//
// It ports crates/finglass-core/src/bank_text.rs, itself ported from FinDesk's
// `util/BankText.kt`. The stored `purpose` and `partner_name` never change:
// rules, search and duplicate detection work on the raw text. These functions
// only render the readable part of it.
package banktext

import (
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// matchTimeout bounds one regular-expression match, as in internal/core/rules.
const matchTimeout = 100 * time.Millisecond

// Tag matches SEPA structured-field tags: an upper-case code of 4 letters
// (`BIC` is 3) directly followed by `+`.
//
// Banks write them in upper case, but they are matched case-insensitively to
// be robust.
const tagPattern = `(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\+`

// Placeholder is shown when a booking has no readable text at all.
const Placeholder = "—"

var (
	tagRegex     = mustCompile(tagPattern)
	paddingRegex = mustCompile(`\s{3,}`)
)

func mustCompile(pattern string) *regexp2.Regexp {
	regex, err := regexp2.Compile(pattern, regexp2.Unicode)
	if err != nil {
		panic("invalid bank-text pattern: " + err.Error())
	}
	regex.MatchTimeout = matchTimeout
	return regex
}

// runeSpan is a half-open range of rune offsets. regexp2 reports match offsets
// in runes, never in bytes.
type runeSpan struct {
	start int
	end   int
}

func findMatches(regex *regexp2.Regexp, text string) []runeSpan {
	matches := make([]runeSpan, 0)
	match, err := regex.FindStringMatch(text)
	for err == nil && match != nil {
		matches = append(matches, runeSpan{start: match.Index, end: match.Index + match.Length})
		match, err = regex.FindNextMatch(match)
	}
	return matches
}

// collapseWhitespace collapses any whitespace run to one space.
func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// DisplayPartner returns the readable partner: it splits on the bank's runs of
// 3 or more padding characters and keeps the first non-blank part; any
// remaining whitespace run collapses to one space. Blank resolves to nil.
func DisplayPartner(raw *string) *string {
	if raw == nil {
		return nil
	}

	runes := []rune(*raw)
	start := 0
	parts := make([]string, 0)
	for _, matched := range findMatches(paddingRegex, *raw) {
		parts = append(parts, string(runes[start:matched.start]))
		start = matched.end
	}
	parts = append(parts, string(runes[start:]))

	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		collapsed := strings.TrimSpace(collapseWhitespace(part))
		if collapsed == "" {
			return nil
		}
		return &collapsed
	}
	return nil
}

// DisplayPurpose returns the readable purpose: if an `SVWZ+` field exists, its
// text up to the next tag; otherwise the text outside all tagged fields (each
// tagged field runs from its tag to the next tag). A text with no tag at all is
// returned with whitespace collapsed and no other change. Blank resolves to
// nil.
func DisplayPurpose(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil
	}

	matches := findMatches(tagRegex, trimmed)
	var result *string
	switch {
	case len(matches) == 0:
		value := trimmed
		result = &value
	default:
		runes := []rune(trimmed)
		hasSVWZ := false
		for _, matched := range matches {
			if isSVWZ(string(runes[matched.start:matched.end])) {
				hasSVWZ = true
				break
			}
		}
		if hasSVWZ {
			for _, field := range substrings(runes, matches) {
				if isSVWZ(field.tag) {
					value := field.text
					result = &value
					break
				}
			}
		} else {
			// Only the text before the first tag is outside the tagged fields.
			value := string(runes[:matches[0].start])
			result = &value
		}
	}

	if result == nil {
		return nil
	}
	collapsed := strings.TrimSpace(collapseWhitespace(*result))
	if collapsed == "" {
		return nil
	}
	return &collapsed
}

func isSVWZ(tag string) bool {
	return len(tag) >= 4 && strings.EqualFold(tag[:4], "SVWZ")
}

type taggedField struct {
	tag  string
	text string
}

// substrings returns each tagged field as (tag, text up to the next tag or the
// end); text before the first tag is dropped.
func substrings(text []rune, matches []runeSpan) []taggedField {
	fields := make([]taggedField, 0, len(matches))
	for index, matched := range matches {
		end := len(text)
		if index+1 < len(matches) {
			end = matches[index+1].start
		}
		fields = append(fields, taggedField{
			tag:  string(text[matched.start:matched.end]),
			text: string(text[matched.end:end]),
		})
	}
	return fields
}

// DisplayDescription returns the readable title on every screen: for a PayPal
// payment "PayPal · <shop>", else the cleaned partner, else the cleaned purpose,
// else a placeholder.
func DisplayDescription(item *model.TransactionItem) string {
	if title := rules.TitleOf(item.Purpose, item.PartnerName); title != nil {
		return *title
	}
	if partner := DisplayPartner(item.PartnerName); partner != nil {
		return *partner
	}
	if purpose := DisplayPurpose(item.Purpose); purpose != nil {
		return *purpose
	}
	return Placeholder
}

// SecondaryLine returns the row's second line: nil when [rules.MerchantOf]
// names a shop (the title already names the provider and the shop, and the full
// purpose stays in the details dialog), else the cleaned purpose when the
// booking has a partner.
func SecondaryLine(item *model.TransactionItem) *string {
	if rules.MerchantOf(item.Purpose, item.PartnerName) != nil {
		return nil
	}
	if item.PartnerName == nil {
		return nil
	}
	return DisplayPurpose(item.Purpose)
}
