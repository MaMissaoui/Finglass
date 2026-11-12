package rules

import (
	"strings"
	"unicode"

	"github.com/dlclark/regexp2"
)

// ShopMatch is a shop extracted from a booking plus the provider view of that
// booking: the booking's text with the shop removed. RuleMatcher uses the view
// to recognise a provider rule.
type ShopMatch struct {
	// Shop is the shop's name.
	Shop string
	// ProviderPurpose is the provider purpose view, when the format supplies one.
	ProviderPurpose *string
	// ProviderPartner is the provider partner view, when the format supplies one.
	ProviderPartner *string
}

const (
	paymentProvider        = "PayPal"
	paymentProviderPurpose = "PAYPAL"
	maxShopLength          = 70
)

// sepaTagPattern lists SEPA structured-field tags, from FinDesk's BankText.TAG.
const sepaTagPattern = `(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\+`

// nonShops are the words that only look like a shop name.
var nonShops = map[string]struct{}{
	"ihr":       {},
	"einkauf":   {},
	"bei":       {},
	"abbuchung": {},
	"vom":       {},
	"transfer":  {},
}

func flexible(word string) string {
	parts := make([]string, 0, len(word))
	for _, character := range word {
		parts = append(parts, string(character))
	}
	return strings.Join(parts, `\s?`)
}

func ihr() string {
	return flexible("Ihr") + `\b`
}

func mustCompile(pattern string) *regexp2.Regexp {
	regex, err := regexp2.Compile(pattern, regexp2.Unicode)
	if err != nil {
		panic("invalid payment-provider pattern: " + err.Error())
	}
	regex.MatchTimeout = matchTimeout
	return regex
}

var (
	markerPattern   = mustCompile("(?i)" + flexible("Ihr") + `\s+` + flexible("Einkauf") + `\s+` + flexible("bei"))
	preCommaPattern = mustCompile(
		`(?i)PP\.\d+\.PP[A-Z]?\s*\.?\s*(.+?)\s*,\s*` + ihr(),
	)
	preCommaLeadingDotPattern = mustCompile(`(?i)^\.\s*(.+?)\s*,\s*` + ihr())
	ppIDInPurposePattern      = mustCompile(`(?i)PP\.\d+\.PP[A-Z]?`)
	cardStartPattern          = mustCompile(`(?i)^PAYPAL\s*\*`)
	cardStopPattern           = mustCompile(`(?i)(?:\s+|^)\d+(?=\s|$)|,\s*\d`)
	codeTailPattern           = mustCompile(`,\s*\S*\d\S*$`)
	topUpPattern              = mustCompile(
		`\b(?:` + flexible("ABBUCHUNG") + `\s+)?` + flexible("VOM") + `\b.*`,
	)
	ppIDPattern = mustCompile(`(?i)PP\.\d+\.PP`)
	tagPattern  = mustCompile(sepaTagPattern)
)

// runeSpan is a half-open range of rune offsets. Unlike Rust's fancy-regex,
// regexp2 reports match offsets in runes, never in bytes.
type runeSpan struct {
	start int
	end   int
}

func findSpan(regex *regexp2.Regexp, text string) (runeSpan, bool) {
	match, err := regex.FindStringMatch(text)
	if err != nil || match == nil {
		return runeSpan{}, false
	}
	return runeSpan{start: match.Index, end: match.Index + match.Length}, true
}

func findLastSpan(regex *regexp2.Regexp, text string) (runeSpan, bool) {
	match, err := regex.FindStringMatch(text)
	var last runeSpan
	found := false
	for err == nil && match != nil {
		last = runeSpan{start: match.Index, end: match.Index + match.Length}
		found = true
		match, err = regex.FindNextMatch(match)
	}
	return last, found
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// TitleOf returns the row title for a shop found by [MerchantOf]:
// "PayPal · <shop>", or nil without a shop.
func TitleOf(purpose *string, partnerName *string) *string {
	shop := MerchantOf(purpose, partnerName)
	if shop == nil {
		return nil
	}
	title := paymentProvider + " · " + *shop
	return &title
}

// MerchantOf returns the shop the raw purpose buys from, or nil when the
// booking names no shop.
func MerchantOf(purpose *string, partnerName *string) *string {
	matched := ShopMatchOf(purpose, partnerName)
	if matched == nil {
		return nil
	}
	shop := matched.Shop
	return &shop
}

// ShopMatchOf returns the shop named by purpose for the PayPal partnerName,
// with the provider view that RuleMatcher uses to recognise a provider rule.
//
// Formats are tried in this order: the "Ihr Einkauf bei" marker, a pre-comma
// name without that marker, and a card descriptor ("PAYPAL *<SHOP>"). The
// marker format prefers the pre-comma name when it is the fuller one.
func ShopMatchOf(purpose *string, partnerName *string) *ShopMatch {
	partnerHasPaypal := partnerName != nil && strings.Contains(lower(*partnerName), "paypal")
	text := ""
	if purpose != nil {
		text = collapseWhitespace(*purpose)
	}
	runes := []rune(text)

	// 1. Marker format, e.g. "PP.0471.PPX . <Shop>, Ihr Einkauf bei <Shop>"
	var marker runeSpan
	markerFound := false
	if partnerHasPaypal {
		marker, markerFound = findSpan(markerPattern, text)
	}
	if markerFound {
		tail := runes[marker.end:]
		tailText := string(tail)
		nextTag, tagFound := findSpan(tagPattern, tailText)
		postRaw := tailText
		if tagFound {
			postRaw = string(tail[:nextTag.start])
		}
		preRaw := preCommaName(runes, marker.start)
		post := sanitize(optionalString(cutTopUp(postRaw)))
		pre := sanitize(preRaw)
		var shop *string
		switch {
		case pre == nil:
			shop = post
		case post == nil:
			shop = pre
		default:
			shop = optionalString(preferred(*pre, *post))
		}
		if shop != nil {
			return &ShopMatch{
				Shop:            *shop,
				ProviderPurpose: nil,
				ProviderPartner: cloneOptional(partnerName),
			}
		}
	}

	// 2. A pre-comma name without the marker, e.g. "PP.1234.PP . <Shop>, Ihr Abo"
	if partnerHasPaypal && !markerFound {
		capture, found := captureSpan(preCommaPattern, text, 1)
		if !found {
			capture, found = captureSpan(preCommaLeadingDotPattern, text, 1)
		}
		if found {
			captureText := string(runes[capture.start:capture.end])
			if shop := sanitize(&captureText); shop != nil {
				return &ShopMatch{
					Shop:            *shop,
					ProviderPurpose: nil,
					ProviderPartner: cloneOptional(partnerName),
				}
			}
		}
	}

	// 3. Card format, e.g. "PAYPAL *<SHOP>" with a blank or PayPal partner
	cardStart, cardFound := findSpan(cardStartPattern, text)
	partnerBlank := partnerName == nil || strings.TrimSpace(*partnerName) == ""
	if cardFound && (partnerBlank || partnerHasPaypal) {
		afterStar := runes[cardStart.end:]
		afterStarText := string(afterStar)
		stop, stopFound := findSpan(cardStopPattern, afterStarText)
		shopRaw := afterStarText
		if stopFound {
			shopRaw = string(afterStar[:stop.start])
		}
		if shop := sanitize(&shopRaw); shop != nil {
			providerPartner := (*string)(nil)
			if partnerHasPaypal {
				value := paymentProviderPurpose
				providerPartner = &value
			}
			providerPurpose := paymentProviderPurpose
			return &ShopMatch{
				Shop:            *shop,
				ProviderPurpose: &providerPurpose,
				ProviderPartner: providerPartner,
			}
		}
	}

	return nil
}

// captureSpan returns the rune span of group group in text.
func captureSpan(regex *regexp2.Regexp, text string, group int) (runeSpan, bool) {
	match, err := regex.FindStringMatch(text)
	if err != nil || match == nil {
		return runeSpan{}, false
	}
	capture := match.GroupByNumber(group)
	if capture == nil {
		return runeSpan{}, false
	}
	return runeSpan{start: capture.Index, end: capture.Index + capture.Length}, true
}

// preCommaName returns the name between a PP.<n>.PP id and the comma
// immediately before the marker, or nil. markerStart is a rune offset.
func preCommaName(text []rune, markerStart int) *string {
	prefix := text[:markerStart]
	prefixText := string(prefix)

	var rest string
	if id, found := findLastSpan(ppIDInPurposePattern, prefixText); found {
		after := strings.TrimSpace(string(prefix[id.end:]))
		after = strings.TrimPrefix(after, ".")
		rest = strings.TrimSpace(after)
	} else {
		if !strings.HasPrefix(prefixText, ".") {
			return nil
		}
		trimmed := strings.TrimSpace(prefixText)
		trimmed = strings.TrimPrefix(trimmed, ".")
		rest = strings.TrimSpace(trimmed)
	}
	if !strings.HasSuffix(rest, ",") {
		return nil
	}
	name := strings.TrimSpace(rest[:len(rest)-1])
	if name == "" {
		return nil
	}
	return &name
}

// cutTopUp cuts an upper-case top-up note from the post-marker text, unless
// nothing would remain before it.
func cutTopUp(text string) string {
	note, found := findSpan(topUpPattern, text)
	if !found {
		return text
	}
	runes := []rune(text)
	before := string(runes[:note.start])
	if strings.TrimSpace(before) == "" {
		return text
	}
	return before
}

// preferred returns the fuller of the pre-comma name and the post-marker shop
// when both pass the gate.
func preferred(pre string, post string) string {
	normalizedPre := normalizeShop(pre)
	normalizedPost := normalizeShop(post)
	switch {
	case normalizedPre == normalizedPost:
		if whitespaceCount(pre) < whitespaceCount(post) {
			return pre
		}
		return post
	case strings.HasPrefix(normalizedPre, normalizedPost):
		if len([]rune(normalizedPre)) > len([]rune(normalizedPost)) {
			return pre
		}
		return post
	case strings.HasPrefix(normalizedPost, normalizedPre):
		if len([]rune(normalizedPost)) > len([]rune(normalizedPre)) {
			return post
		}
		return pre
	default:
		return post
	}
}

func normalizeShop(shop string) string {
	withoutSpaces := strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, shop)
	return lower(withoutSpaces)
}

func whitespaceCount(text string) int {
	count := 0
	for _, character := range text {
		if unicode.IsSpace(character) {
			count++
		}
	}
	return count
}

// sanitize cleans a candidate shop and rejects text that only looks like one:
// ids, codes, provider names and the marker words themselves. The code tail is
// stripped before leading punctuation, so ", AB12CD34" leaves nothing rather
// than a bare code.
func sanitize(raw *string) *string {
	if raw == nil {
		return nil
	}
	candidate := collapseWhitespace(*raw)
	runes := []rune(candidate)

	if tail, found := findSpan(codeTailPattern, candidate); found {
		candidate = string(runes[:tail.start])
	}
	candidate = strings.TrimLeftFunc(candidate, func(character rune) bool {
		return !isAlphabetic(character) && !unicode.IsNumber(character)
	})
	candidate = strings.TrimRightFunc(candidate, func(character rune) bool {
		return character == ',' || character == ';' || character == ':' || unicode.IsSpace(character)
	})
	runes = []rune(candidate)
	if len(runes) > maxShopLength {
		candidate = string(runes[:maxShopLength])
	}
	candidate = strings.TrimRightFunc(candidate, unicode.IsSpace)

	runes = []rune(candidate)
	if len(runes) < 2 {
		return nil
	}
	hasLetter := false
	allASCIIDigits := true
	for _, character := range runes {
		if isAlphabetic(character) {
			hasLetter = true
		}
		if character < '0' || character > '9' {
			allASCIIDigits = false
		}
	}
	if !hasLetter {
		return nil
	}
	if allASCIIDigits {
		return nil
	}
	lowered := lower(candidate)
	if strings.Contains(lowered, "paypal") {
		return nil
	}
	if matched, err := ppIDPattern.MatchString(candidate); err == nil && matched {
		return nil
	}
	if _, isNonShop := nonShops[lowered]; isNonShop {
		return nil
	}
	return &candidate
}

// isAlphabetic is Rust's char::is_alphabetic: the Unicode Alphabetic property
// (letters, letter numbers and Other_Alphabetic marks), wider than
// unicode.IsLetter.
func isAlphabetic(character rune) bool {
	return unicode.IsLetter(character) ||
		unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Other_Alphabetic, character)
}

func optionalString(value string) *string {
	return &value
}

func cloneOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
