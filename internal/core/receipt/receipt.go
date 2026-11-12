// Package receipt heuristically extracts the total amount and date from
// receipt/invoice text (German and English).
//
// It ports crates/finglass-core/src/receipt.rs. The rules mirror the Kotlin
// `ReceiptParser`: every line's amounts are collected, the last line carrying a
// total keyword decides the total (otherwise the largest amount wins), and
// dates are tried in a fixed order. Amounts are integer minor units in the
// currency the caller passes in; nothing is rounded.
package receipt

import (
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// matchTimeout bounds one regular-expression match, as in internal/core/rules.
const matchTimeout = 100 * time.Millisecond

// amountPattern matches 1.234,56 | 1234,56 | 1,234.56 | 1234.56  (exactly two
// decimals).
const amountPattern = `(?<![\d.,])(\d{1,3}(?:[.,\s]\d{3})+|\d+)([.,])(\d{2})(?!\d)(?![.,]\d)`

const totalKeywordPattern = `(?i)\b(gesamtbetrag|gesamtsumme|gesamt|summe|zu zahlen|endbetrag|rechnungsbetrag|total|amount due|betrag)\b`

const (
	dateDMYYYYPattern   = `\b(\d{1,2})\.(\d{1,2})\.(\d{4})\b`
	dateYYYYMMDDPattern = `\b(\d{4})-(\d{2})-(\d{2})\b`
	dateDMYYPattern     = `\b(\d{1,2})\.(\d{1,2})\.(\d{2})\b`
	dateDMYYYYSlash     = `\b(\d{1,2})/(\d{1,2})/(\d{4})\b`
)

var (
	amountRegex       = mustCompile(amountPattern)
	totalKeywordRegex = mustCompile(totalKeywordPattern)
	dateDMYYYYRegex   = mustCompile(dateDMYYYYPattern)
	dateYYYYMMDDRegex = mustCompile(dateYYYYMMDDPattern)
	dateDMYYRegex     = mustCompile(dateDMYYPattern)
	dateSlashRegex    = mustCompile(dateDMYYYYSlash)
)

func mustCompile(pattern string) *regexp2.Regexp {
	regex, err := regexp2.Compile(pattern, regexp2.Unicode)
	if err != nil {
		panic("invalid receipt pattern: " + err.Error())
	}
	regex.MatchTimeout = matchTimeout
	return regex
}

// Fields is what the parser found in a piece of receipt text.
type Fields struct {
	// Total is the last keyword line's last amount, or the largest amount in
	// the text.
	Total *money.Money
	// Date is the first valid date in the supported range, or nil.
	Date *civil.Date
	// Amounts is every distinct amount, in the order it first appeared.
	Amounts []money.Money
}

// Parse parses receipt text into amounts, total and date.
//
// currency decides how many decimals an amount may have (and therefore which
// candidates count as amounts), and today is the anchor for two-digit years.
// Nothing reads the clock.
func Parse(text string, currency money.Currency, today civil.Date) Fields {
	amounts := make([]money.Money, 0)
	seen := make(map[money.Money]struct{})
	var keywordTotal *money.Money

	for _, line := range splitLines(text) {
		lineAmounts := amountMatches(line, currency)
		for _, amount := range lineAmounts {
			if _, exists := seen[amount]; !exists {
				seen[amount] = struct{}{}
				amounts = append(amounts, amount)
			}
		}
		if matched, err := totalKeywordRegex.MatchString(line); err == nil && matched && len(lineAmounts) > 0 {
			// The last keyword line wins (totals tend to come after subtotals).
			last := lineAmounts[len(lineAmounts)-1]
			keywordTotal = &last
		}
	}

	var total *money.Money
	if keywordTotal != nil {
		value := *keywordTotal
		total = &value
	} else if len(amounts) > 0 {
		largest := amounts[0]
		for _, amount := range amounts[1:] {
			if moneyGreater(amount, largest) {
				largest = amount
			}
		}
		total = &largest
	}

	return Fields{
		Total:   total,
		Date:    findDate(text, today),
		Amounts: amounts,
	}
}

// splitLines splits on both newline characters like Rust's
// `text.split(['\n', '\r'])`, keeping empty lines.
func splitLines(text string) []string {
	lines := make([]string, 0)
	start := 0
	for index, character := range text {
		if character == '\n' || character == '\r' {
			lines = append(lines, text[start:index])
			start = index + len(string(character))
		}
	}
	return append(lines, text[start:])
}

func amountMatches(line string, currency money.Currency) []money.Money {
	found := make([]money.Money, 0)
	for _, groups := range captureGroups(amountRegex, line) {
		if amount, ok := toAmount(groups, currency); ok {
			found = append(found, amount)
		}
	}
	return found
}

func toAmount(groups []string, currency money.Currency) (money.Money, bool) {
	if len(groups) < 4 {
		return money.Money{}, false
	}
	intPart := groups[1]
	separator := groups[2]
	decimals := groups[3]

	// The grouping separator must differ from the decimal separator
	// ("1.234,56" or "1,234.56").
	if strings.Contains(intPart, separator) {
		return money.Money{}, false
	}

	var digits strings.Builder
	digits.Grow(len(intPart))
	for _, character := range intPart {
		if character == '.' || character == ',' || character == ' ' {
			continue
		}
		digits.WriteRune(character)
	}
	amount, err := money.ParseDecimal(digits.String()+"."+decimals, currency)
	if err != nil {
		return money.Money{}, false
	}
	return amount, true
}

// captureGroups returns the captured groups of every match, with group 0
// first. regexp2 reports offsets in runes, but the group text values are used
// directly here.
func captureGroups(regex *regexp2.Regexp, text string) [][]string {
	results := make([][]string, 0)
	match, err := regex.FindStringMatch(text)
	for err == nil && match != nil {
		groups := match.Groups()
		values := make([]string, 0, len(groups))
		for _, group := range groups {
			values = append(values, group.String())
		}
		results = append(results, values)
		match, err = regex.FindNextMatch(match)
	}
	return results
}

func findDate(text string, today civil.Date) *civil.Date {
	for _, groups := range captureGroups(dateDMYYYYRegex, text) {
		date, ok := smartDate(groups[3], groups[2], groups[1])
		if ok && inSupportedRange(date) {
			return &date
		}
	}

	for _, groups := range captureGroups(dateYYYYMMDDRegex, text) {
		date, ok := smartDate(groups[1], groups[2], groups[3])
		if ok && inSupportedRange(date) {
			return &date
		}
	}

	for _, groups := range captureGroups(dateDMYYRegex, text) {
		day, dayOK := parseDigits(groups[1])
		month, monthOK := parseDigits(groups[2])
		yy, yyOK := parseDigits(groups[3])
		if !dayOK || !monthOK || !yyOK || yy > 99 {
			// Rust's `?` ends the whole search when a matched date cannot be
			// parsed, even if a later pattern would have found a date.
			return nil
		}
		year, err := imports.ResolveTwoDigitYear(uint8(yy), today) // #nosec G115 -- yy is checked to be 0..=99 above.
		if err != nil {
			return nil
		}
		date, ok := strictDate(year, month, day)
		if ok && inSupportedRange(date) {
			return &date
		}
	}

	for _, groups := range captureGroups(dateSlashRegex, text) {
		first, firstOK := parseDigits(groups[1])
		second, secondOK := parseDigits(groups[2])
		year, yearOK := parseDigits(groups[3])
		if !firstOK || !secondOK || !yearOK {
			// As above: Rust's `?` aborts the date search on a parse failure.
			return nil
		}
		date, ok := slashDate(year, first, second)
		if ok && inSupportedRange(date) {
			return &date
		}
	}

	return nil
}

func inSupportedRange(date civil.Date) bool {
	return date.Year() >= 1990 && date.Year() <= 2100
}

// smartDate reads a four-digit-year pattern as Java's SMART resolver does: a
// day above the month's length is clamped to that length, while a day outside
// 1..=31 or a month outside 1..=12 is invalid.
func smartDate(yearText, monthText, dayText string) (civil.Date, bool) {
	year, yearOK := parseDigits(yearText)
	month, monthOK := parseDigits(monthText)
	day, dayOK := parseDigits(dayText)
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return civil.Date{}, false
	}
	if limit := monthLength(year, time.Month(month)); day > limit {
		day = limit
	}
	return civil.New(year, time.Month(month), day)
}

// strictDate is a strict calendar date, like `LocalDate.of`: no clamping of an
// invalid day.
func strictDate(year, month, day int) (civil.Date, bool) {
	return civil.New(year, time.Month(month), day)
}

// slashDate reads d/M/yyyy day-first (`01/05/2026` is 1 May 2026). Only when
// that is invalid does it fall back to month/day, so `05/13/2026` is 13 May
// 2026.
func slashDate(year, first, second int) (civil.Date, bool) {
	if date, ok := strictDate(year, second, first); ok {
		return date, true
	}
	return strictDate(year, first, second)
}

func parseDigits(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	value := 0
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
		value = value*10 + int(text[index]-'0')
	}
	return value, true
}

func monthLength(year int, month time.Month) int {
	switch month {
	case time.January, time.March, time.May, time.July, time.August, time.October, time.December:
		return 31
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 0
	}
}

// moneyGreater follows Rust's derived Ord for Money: minor units first, then
// the currency code.
func moneyGreater(left, right money.Money) bool {
	if left.Minor != right.Minor {
		return left.Minor > right.Minor
	}
	return left.Currency.String() > right.Currency.String()
}
