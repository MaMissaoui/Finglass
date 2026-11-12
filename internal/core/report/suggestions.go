// Tax-relevance suggestions from keywords, ported from FinDesk's
// document/TaxSuggestions.kt, via crates/finglass-core/src/report/suggestions.rs.
package report

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// KeyKeywords is the settings key with the user's own keywords, one per line.
const KeyKeywords = "tax.suggestionKeywords"

// DefaultKeywords are the built-in suggestion keywords.
var DefaultKeywords = []string{
	"Spende",
	"Handwerker",
	"Schornsteinfeger",
	"Steuerberater",
	"Lohnsteuerhilfe",
	"Kita",
	"Kindergarten",
	"Tagesmutter",
	"Schulgeld",
	"Krankenkasse",
	"Zahnarzt",
	"Apotheke",
	"Fortbildung",
	"Fachliteratur",
	"Gewerkschaft",
	"Kirchensteuer",
	"Riester",
	"Haftpflicht",
	"donation",
	"charity",
	"tax advisor",
	"tuition",
	"assurance",
}

// TaxSuggestion is a booking and the keyword that matched it.
type TaxSuggestion struct {
	// Booking is the suggested booking.
	Booking model.TransactionItem
	// Keyword is the keyword that matched.
	Keyword string
}

// Keywords returns the keywords to suggest with: the setting's trimmed lines,
// or the built-in defaults.
func Keywords(ctx context.Context, d *db.Database) ([]string, error) {
	stored, ok, err := settings.Get(ctx, d, KeyKeywords)
	if err != nil {
		return nil, err
	}
	if !ok {
		return append([]string(nil), DefaultKeywords...), nil
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(stored, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) == 0 {
		return append([]string(nil), DefaultKeywords...), nil
	}
	return lines, nil
}

// preparedKeyword is one trimmed keyword and its compiled pattern.
type preparedKeyword struct {
	keyword string
	pattern *regexp2.Regexp
}

// FindTaxSuggestions finds every booking a keyword hints at, newest first.
func FindTaxSuggestions(bookings []model.TransactionItem, keywords []string) []TaxSuggestion {
	prepared := make([]preparedKeyword, 0, len(keywords))
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if len([]rune(trimmed)) < 3 {
			continue
		}
		pattern := `(?i)(?<![\p{L}\p{N}])` + rustEscape(trimmed)
		compiled, err := regexp2.Compile(pattern, regexp2.Unicode)
		if err != nil {
			panic("the suggestion pattern is valid")
		}
		compiled.MatchTimeout = matchTimeout
		prepared = append(prepared, preparedKeyword{keyword: trimmed, pattern: compiled})
	}
	if len(prepared) == 0 {
		return []TaxSuggestion{}
	}

	suggestions := make([]TaxSuggestion, 0)
	for index := range bookings {
		if suggestion, matched := matchBooking(prepared, &bookings[index]); matched {
			suggestions = append(suggestions, suggestion)
		}
	}
	sort.SliceStable(suggestions, func(left, right int) bool {
		return suggestions[left].Booking.Date.After(suggestions[right].Booking.Date)
	})
	return suggestions
}

// matchTimeout bounds one suggestion pattern's match, like the rules matcher.
const matchTimeout = 100 * time.Millisecond

func matchBooking(prepared []preparedKeyword, booking *model.TransactionItem) (TaxSuggestion, bool) {
	if booking.IsTaxRelevant {
		return TaxSuggestion{}, false
	}
	if booking.TaxSource == model.TaxSourceManual {
		return TaxSuggestion{}, false
	}
	if booking.IsTransfer {
		return TaxSuggestion{}, false
	}

	for _, entry := range prepared {
		if matchesText(entry.pattern, booking.PartnerName) || matchesText(entry.pattern, booking.Purpose) {
			return TaxSuggestion{Booking: *booking, Keyword: entry.keyword}, true
		}
	}
	return TaxSuggestion{}, false
}

func matchesText(pattern *regexp2.Regexp, text *string) bool {
	if text == nil {
		return false
	}
	matched, err := pattern.MatchString(*text)
	return err == nil && matched
}

// rustEscape escapes a keyword exactly like Rust's regex::escape: only the
// regex meta characters are prefixed with a backslash.
func rustEscape(keyword string) string {
	var escaped strings.Builder
	escaped.Grow(len(keyword))
	for _, character := range keyword {
		if isRustMetaCharacter(character) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}

// isRustMetaCharacter mirrors regex_syntax::is_meta_character.
func isRustMetaCharacter(character rune) bool {
	switch character {
	case '\\', '.', '+', '*', '?', '(', ')', '|', '[', ']', '{', '}', '^', '$', '#', '&', '-', '~':
		return true
	default:
		return false
	}
}
