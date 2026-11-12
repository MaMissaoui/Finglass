// Package recurring finds subscriptions, rent, salary and other bookings that
// repeat on a regular schedule.
//
// It ports crates/finglass-core/src/recurring.rs, itself ported from FinDesk's
// `document/RecurringDetector.kt`. Detection is read-only: nothing here
// schedules, sends or stores a payment.
package recurring

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// Cadence is how often a recurring payment repeats. Its values are Rust's
// `Debug` names.
type Cadence string

// The cadences.
const (
	// Weekly repeats every week.
	Weekly Cadence = "Weekly"
	// Monthly repeats every month.
	Monthly Cadence = "Monthly"
	// Quarterly repeats every three months.
	Quarterly Cadence = "Quarterly"
	// Yearly repeats every year.
	Yearly Cadence = "Yearly"
)

// Days returns the model days between occurrences.
func (c Cadence) Days() int64 {
	switch c {
	case Weekly:
		return 7
	case Monthly:
		return 30
	case Quarterly:
		return 91
	case Yearly:
		return 365
	default:
		return 0
	}
}

// ToleranceDays returns how far an interval may deviate from [Cadence.Days]
// and still count.
func (c Cadence) ToleranceDays() int64 {
	switch c {
	case Weekly:
		return 2
	case Monthly:
		return 4
	case Quarterly:
		return 10
	case Yearly:
		return 15
	default:
		return 0
	}
}

// PerYear returns the occurrences in a year.
func (c Cadence) PerYear() int64 {
	switch c {
	case Weekly:
		return 52
	case Monthly:
		return 12
	case Quarterly:
		return 4
	case Yearly:
		return 1
	default:
		return 0
	}
}

// Next returns the next occurrence after from, clamped to the target month's
// length like `LocalDate.plusMonths`.
func (c Cadence) Next(from civil.Date) civil.Date {
	switch c {
	case Weekly:
		return checkedDay(from, 7)
	case Monthly:
		return addMonths(from, 1)
	case Quarterly:
		return addMonths(from, 3)
	case Yearly:
		return addMonths(from, 12)
	default:
		return from
	}
}

// RecurringPayment is one detected recurring payment.
type RecurringPayment struct {
	// Name is the display name.
	Name string
	// Cadence is how often it repeats.
	Cadence Cadence
	// TypicalAmount is the median amount (signed: negative for payments).
	TypicalAmount money.Money
	// FixedAmount says whether every occurrence had (almost) the same amount.
	FixedAmount bool
	// LastDate is the last occurrence.
	LastDate civil.Date
	// NextDate is the next expected occurrence.
	NextDate civil.Date
	// Occurrences is how many distinct days carried the payment.
	Occurrences int
	// CategoryName is the last booking's category name.
	CategoryName *string
	// Currency is the payment currency.
	Currency money.Currency
	// IsTransfer says whether the last booking moved between the user's own
	// accounts.
	IsTransfer bool
}

// IsIncome reports whether the typical amount is money in.
func (p RecurringPayment) IsIncome() bool {
	return p.TypicalAmount.Minor > 0
}

// YearlyAmount returns the typical amount scaled to a year.
func (p RecurringPayment) YearlyAmount() money.Money {
	return money.Money{
		Minor:    p.TypicalAmount.Minor * p.Cadence.PerYear(),
		Currency: p.Currency,
	}
}

// RecurringDetector finds recurring payments among bookings.
type RecurringDetector struct{}

// MinOccurrences is the least number of bookings that make a recurring
// payment (two for yearly, which rarely has more history).
const MinOccurrences = 3

// MinRegularity is the share of intervals that must match the cadence.
const MinRegularity = 0.75

// Detect finds recurring payments, sorted by next date and then name.
func (RecurringDetector) Detect(transactions []model.TransactionItem, today civil.Date) []RecurringPayment {
	groups := make(map[string][]*model.TransactionItem)
	for index := range transactions {
		transaction := &transactions[index]
		name := key(transaction)
		groups[name] = append(groups[name], transaction)
	}

	keys := make([]string, 0, len(groups))
	for name := range groups {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	detected := make([]RecurringPayment, 0, len(groups))
	for _, name := range keys {
		if name == "" {
			continue
		}
		if payment, ok := analyse(groups[name], today); ok {
			detected = append(detected, payment)
		}
	}
	sort.SliceStable(detected, func(left, right int) bool {
		if comparison := detected[left].NextDate.Compare(detected[right].NextDate); comparison != 0 {
			return comparison < 0
		}
		return detected[left].Name < detected[right].Name
	})
	return detected
}

// Upcoming returns recurring payments due in the next days days, earliest
// first.
func (RecurringDetector) Upcoming(recurring []RecurringPayment, today civil.Date, days int64) []RecurringPayment {
	limit := checkedDay(today, days)
	upcoming := make([]RecurringPayment, 0, len(recurring))
	for _, payment := range recurring {
		if !payment.NextDate.After(limit) {
			upcoming = append(upcoming, payment)
		}
	}
	return upcoming
}

func analyse(group []*model.TransactionItem, today civil.Date) (RecurringPayment, bool) {
	// One booking per day at most (split payments on the same day count once).
	byDate := append([]*model.TransactionItem(nil), group...)
	sort.SliceStable(byDate, func(left, right int) bool {
		return byDate[left].Date.Before(byDate[right].Date)
	})
	byDate = dedupByDate(byDate)

	if len(byDate) < 2 {
		return RecurringPayment{}, false
	}

	intervals := make([]int64, 0, len(byDate)-1)
	for index := 1; index < len(byDate); index++ {
		intervals = append(intervals, daysBetween(byDate[index-1].Date, byDate[index].Date))
	}
	sort.Slice(intervals, func(left, right int) bool { return intervals[left] < intervals[right] })
	median := intervals[len(intervals)/2]

	cadences := []Cadence{Weekly, Monthly, Quarterly, Yearly}
	var cadence Cadence
	found := false
	for _, candidate := range cadences {
		if absInt64(median-candidate.Days()) <= candidate.ToleranceDays() {
			cadence = candidate
			found = true
			break
		}
	}
	if !found {
		return RecurringPayment{}, false
	}

	minCount := MinOccurrences
	if cadence == Yearly {
		minCount = 2
	}
	if len(byDate) < minCount {
		return RecurringPayment{}, false
	}

	regular := 0
	for _, interval := range intervals {
		if absInt64(interval-cadence.Days()) <= cadence.ToleranceDays() {
			regular++
		}
	}
	if float64(regular)/float64(len(intervals)) < MinRegularity {
		return RecurringPayment{}, false
	}

	last := byDate[len(byDate)-1]
	next := cadence.Next(last.Date)
	// Stopped subscriptions: more than one full cycle overdue.
	if checkedDay(next, cadence.Days()).Before(today) {
		return RecurringPayment{}, false
	}
	// A booking a few days late is still "due", not skipped to the next cycle.
	overdueLimit := checkedDay(today, -cadence.ToleranceDays())
	for next.Before(overdueLimit) {
		next = cadence.Next(next)
	}

	amounts := make([]int64, 0, len(group))
	for _, transaction := range group {
		amounts = append(amounts, transaction.Amount.Minor)
	}
	sort.Slice(amounts, func(left, right int) bool { return amounts[left] < amounts[right] })
	medianAmount := amounts[len(amounts)/2]
	// Zero medians are booking artifacts (corrections, rejected debits), not a
	// subscription.
	if medianAmount == 0 {
		return RecurringPayment{}, false
	}

	tolerance := absInt64(medianAmount) / 100
	if tolerance < 1 {
		tolerance = 1
	}
	fixed := true
	for _, amount := range amounts {
		if absInt64(amount-medianAmount) > tolerance {
			fixed = false
			break
		}
	}

	name := ""
	if title := rules.TitleOf(last.Purpose, last.PartnerName); title != nil {
		name = *title
	} else {
		source := last.RawDescription()
		if last.PartnerName != nil {
			source = *last.PartnerName
		}
		name = strings.Join(strings.Fields(source), " ")
	}

	return RecurringPayment{
		Name:          name,
		Cadence:       cadence,
		TypicalAmount: money.Money{Minor: medianAmount, Currency: last.Amount.Currency},
		FixedAmount:   fixed,
		LastDate:      last.Date,
		NextDate:      next,
		Occurrences:   len(byDate),
		CategoryName:  cloneOptional(last.CategoryName),
		Currency:      last.Amount.Currency,
		IsTransfer:    last.IsTransfer,
	}, true
}

func dedupByDate(transactions []*model.TransactionItem) []*model.TransactionItem {
	unique := transactions[:0]
	for _, transaction := range transactions {
		if len(unique) == 0 || unique[len(unique)-1].Date != transaction.Date {
			unique = append(unique, transaction)
		}
	}
	return unique
}

// key builds the grouping key: same counterparty, direction and currency, with
// normalised names.
func key(transaction *model.TransactionItem) string {
	shop := rules.MerchantOf(transaction.Purpose, transaction.PartnerName)
	source := ""
	switch {
	case shop != nil:
		source = *shop
	case transaction.PartnerName != nil:
		source = *transaction.PartnerName
	case transaction.Purpose != nil:
		source = *transaction.Purpose
	}

	name := normaliseName(source)
	scoped := name
	if shop != nil && name != "" {
		scoped = "paypal:" + name
	}

	if scoped == "" {
		return ""
	}
	return scoped + "|" + formatSignum(transaction.Amount.Minor) + "|" + transaction.Amount.Currency.String()
}

func normaliseName(text string) string {
	lowered := cases.Lower(language.Und).String(text)
	var normalized strings.Builder
	normalized.Grow(len(lowered))
	for _, character := range lowered {
		switch {
		case character >= '0' && character <= '9':
			normalized.WriteRune(' ')
		case isAlphabetic(character) || character == ' ':
			normalized.WriteRune(character)
		default:
			normalized.WriteRune(' ')
		}
	}

	words := make([]string, 0, 3)
	for _, word := range strings.Fields(normalized.String()) {
		if len([]rune(word)) > 1 {
			words = append(words, word)
		}
		if len(words) == 3 {
			break
		}
	}
	return strings.Join(words, " ")
}

// isAlphabetic is Rust's char::is_alphabetic: the Unicode Alphabetic property
// (letters, letter numbers and Other_Alphabetic marks), wider than
// unicode.IsLetter.
func isAlphabetic(character rune) bool {
	return unicode.IsLetter(character) ||
		unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Other_Alphabetic, character)
}

func formatSignum(value int64) string {
	switch {
	case value < 0:
		return "-1"
	case value > 0:
		return "1"
	default:
		return "0"
	}
}

func checkedDay(date civil.Date, days int64) civil.Date {
	return date.AddDays(int(days))
}

func addMonths(date civil.Date, months int) civil.Date {
	total := date.Year()*12 + int(date.Month()) - 1 + months
	year := floorDiv(total, 12)
	monthNumber := floorMod(total, 12) + 1
	month := time.Month(monthNumber)
	day := date.Day()
	if limit := monthLength(year, month); day > limit {
		day = limit
	}
	result, ok := civil.New(year, month, day)
	if !ok {
		panic("date overflow")
	}
	return result
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

func floorDiv(left, right int) int {
	quotient := left / right
	remainder := left % right
	if remainder != 0 && (remainder < 0) != (right < 0) {
		quotient--
	}
	return quotient
}

func floorMod(left, right int) int {
	remainder := left % right
	if remainder != 0 && (remainder < 0) != (right < 0) {
		remainder += right
	}
	return remainder
}

func daysBetween(from, to civil.Date) int64 {
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int64(end.Sub(start) / (24 * time.Hour))
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func cloneOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
