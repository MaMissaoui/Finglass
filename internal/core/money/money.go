// Package money represents amounts as integer minor units with an explicit
// currency.
//
// Amounts are never stored or calculated as floating point, and totals never
// mix or convert currencies. It ports crates/finglass-core/src/money.rs.
package money

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// ErrCurrencyMismatch reports operands with different currencies.
var ErrCurrencyMismatch = errors.New("currency mismatch")

// ErrOverflow reports a result that does not fit into int64 minor units.
var ErrOverflow = errors.New("money amount overflow")

// InvalidError reports text that is not a valid amount or currency code.
type InvalidError struct {
	Msg string
}

// Error implements error with Rust's MoneyError::Invalid display.
func (e InvalidError) Error() string {
	return "invalid money value: " + e.Msg
}

// Currency is an ISO 4217 currency code, stored as three ASCII letters. The
// zero value is invalid.
type Currency struct {
	code [3]byte
}

// EUR is the euro.
var EUR = Currency{code: [3]byte{'E', 'U', 'R'}}

// NewCurrency builds a currency code from exactly three ASCII letters,
// upper-casing them.
func NewCurrency(text string) (Currency, error) {
	if len(text) != 3 {
		return Currency{}, InvalidError{Msg: "invalid currency code " + rustQuote(text)}
	}
	var code [3]byte
	for index := 0; index < 3; index++ {
		character := text[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return Currency{}, InvalidError{Msg: "invalid currency code " + rustQuote(text)}
		}
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		code[index] = character
	}
	return Currency{code: code}, nil
}

// String returns the code as three upper-case letters.
func (c Currency) String() string {
	return string(c.code[:])
}

// MinorExponent returns the number of decimal digits used by the currency.
func (c Currency) MinorExponent() uint32 {
	switch c.String() {
	case "JPY", "KRW", "ISK", "CLP", "VND", "XOF", "XAF":
		return 0
	case "BHD", "KWD", "OMR", "JOD", "TND", "IQD", "LYD":
		return 3
	default:
		return 2
	}
}

// Money is an amount in integer minor units.
type Money struct {
	Minor    int64
	Currency Currency
}

// CheckedAdd adds two amounts of the same currency.
func (m Money) CheckedAdd(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	minor, ok := addInt64(m.Minor, other.Minor)
	if !ok {
		return Money{}, ErrOverflow
	}
	return Money{Minor: minor, Currency: m.Currency}, nil
}

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool {
	return m.Minor < 0
}

// Totals sums amounts per currency, never mixing or converting them.
type Totals struct {
	totals map[Currency]int64
}

// Add adds an amount to its currency's total.
func (t *Totals) Add(m Money) error {
	if t.totals == nil {
		t.totals = make(map[Currency]int64)
	}
	total, ok := addInt64(t.totals[m.Currency], m.Minor)
	if !ok {
		return ErrOverflow
	}
	t.totals[m.Currency] = total
	return nil
}

// Get returns the total for one currency, or zero when it has no entries.
func (t *Totals) Get(c Currency) int64 {
	if t.totals == nil {
		return 0
	}
	return t.totals[c]
}

// Each calls f for every currency total in currency order.
func (t *Totals) Each(f func(Currency, int64)) {
	currencies := make([]Currency, 0, len(t.totals))
	for currency := range t.totals {
		currencies = append(currencies, currency)
	}
	sort.Slice(currencies, func(left, right int) bool {
		return currencies[left].String() < currencies[right].String()
	})
	for _, currency := range currencies {
		f(currency, t.totals[currency])
	}
}

// PrimaryCurrencyKey is the settings key that holds the primary currency, as
// FinDesk stores it.
const PrimaryCurrencyKey = "money.primary_currency"

// DefaultPrimaryCurrency is the primary currency used when the setting is
// absent or unreadable.
var DefaultPrimaryCurrency = EUR

// Primary reads the primary currency from the settings, falling back to
// DefaultPrimaryCurrency.
func Primary(ctx context.Context, d *db.Database) (Currency, error) {
	stored, ok, err := settings.Get(ctx, d, PrimaryCurrencyKey)
	if err != nil {
		return Currency{}, err
	}
	if !ok || len(stored) != 3 {
		return DefaultPrimaryCurrency, nil
	}
	currency, err := NewCurrency(stored)
	if err != nil {
		return DefaultPrimaryCurrency, nil
	}
	return currency, nil
}

// CurrencyTotal is one currency's total in a SumByCurrency result.
type CurrencyTotal struct {
	Currency Currency
	Total    int64
}

// SumByCurrency sums amount per currency, primary currency first. The remaining
// currencies follow in code order.
func SumByCurrency[T any](items []T, primary Currency, currency func(T) Currency, amount func(T) int64) []CurrencyTotal {
	totals := make(map[Currency]int64)
	for _, item := range items {
		totals[currency(item)] += amount(item)
	}

	entries := make([]CurrencyTotal, 0, len(totals))
	for code, total := range totals {
		entries = append(entries, CurrencyTotal{Currency: code, Total: total})
	}
	sort.Slice(entries, func(left, right int) bool {
		leftPrimary := entries[left].Currency != primary
		rightPrimary := entries[right].Currency != primary
		if leftPrimary != rightPrimary {
			return !leftPrimary
		}
		return entries[left].Currency.String() < entries[right].Currency.String()
	})
	return entries
}

// ParseDecimal parses a plain decimal amount using the currency's exponent.
//
// It accepts a leading sign, "." or "," as decimal separator, and thousands
// separators. When both separator characters appear, the last one is the
// decimal separator. With a single separator followed by exactly three digits,
// and a currency whose exponent is not three, the separator is treated as a
// thousands separator.
func ParseDecimal(text string, currency Currency) (Money, error) {
	trimmed := strings.TrimSpace(text)
	invalid := func() error {
		return InvalidError{Msg: "invalid amount " + rustQuote(text)}
	}

	if trimmed == "" {
		return Money{}, invalid()
	}

	negative := false
	unsigned := trimmed
	switch trimmed[0] {
	case '-':
		negative = true
		unsigned = trimmed[1:]
	case '+':
		unsigned = trimmed[1:]
	}
	if unsigned == "" {
		return Money{}, invalid()
	}

	for index := 0; index < len(unsigned); index++ {
		character := unsigned[index]
		if (character < '0' || character > '9') && character != '.' && character != ',' {
			return Money{}, invalid()
		}
	}

	exponent := currency.MinorExponent()
	dots := strings.Count(unsigned, ".")
	commas := strings.Count(unsigned, ",")

	var integerPart, fractionPart string
	hasThousands := false
	var thousands byte

	if dots > 0 && commas > 0 {
		lastDot := strings.LastIndexByte(unsigned, '.')
		lastComma := strings.LastIndexByte(unsigned, ',')
		decimalSeparator := byte(',')
		if lastDot > lastComma {
			decimalSeparator = '.'
		}
		thousands = ','
		if decimalSeparator == ',' {
			thousands = '.'
		}
		decimalIndex := strings.LastIndexByte(unsigned, decimalSeparator)
		integerPart = unsigned[:decimalIndex]
		fractionPart = unsigned[decimalIndex+1:]
		hasThousands = true
	} else if dots > 0 || commas > 0 {
		separator := byte('.')
		occurrences := dots
		if dots == 0 {
			separator = ','
			occurrences = commas
		}
		if occurrences > 1 {
			integerPart = unsigned
			hasThousands = true
			thousands = separator
		} else {
			separatorIndex := strings.IndexByte(unsigned, separator)
			fractionLength := len(unsigned) - separatorIndex - 1
			if fractionLength == 3 && exponent != 3 {
				integerPart = unsigned
				hasThousands = true
				thousands = separator
			} else {
				integerPart = unsigned[:separatorIndex]
				fractionPart = unsigned[separatorIndex+1:]
			}
		}
	} else {
		integerPart = unsigned
	}

	if fractionPart != "" {
		for index := 0; index < len(fractionPart); index++ {
			if fractionPart[index] < '0' || fractionPart[index] > '9' {
				return Money{}, invalid()
			}
		}
	}
	if len(fractionPart) > int(exponent) {
		return Money{}, InvalidError{Msg: "too many decimal places for " + currency.String()}
	}

	var integerDigits string
	if hasThousands {
		grouped, err := groupedIntegerDigits(integerPart, thousands, invalid)
		if err != nil {
			return Money{}, err
		}
		integerDigits = grouped
	} else if integerPart == "" {
		integerDigits = "0"
	} else {
		for index := 0; index < len(integerPart); index++ {
			if integerPart[index] < '0' || integerPart[index] > '9' {
				return Money{}, invalid()
			}
		}
		integerDigits = integerPart
	}

	fractionDigits := fractionPart
	for len(fractionDigits) < int(exponent) {
		fractionDigits += "0"
	}

	integer, ok := new(big.Int).SetString(integerDigits, 10)
	if !ok {
		return Money{}, ErrOverflow
	}
	fraction := new(big.Int)
	if fractionDigits != "" {
		fraction, ok = new(big.Int).SetString(fractionDigits, 10)
		if !ok {
			return Money{}, ErrOverflow
		}
	}

	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
	value := new(big.Int).Mul(integer, scale)
	value.Add(value, fraction)
	if negative {
		value.Neg(value)
	}
	if !value.IsInt64() {
		return Money{}, ErrOverflow
	}

	return Money{Minor: value.Int64(), Currency: currency}, nil
}

func groupedIntegerDigits(text string, separator byte, invalid func() error) (string, error) {
	groups := strings.Split(text, string(separator))
	if len(groups) == 0 {
		return "", invalid()
	}
	for index, group := range groups {
		if group == "" {
			return "", invalid()
		}
		for position := 0; position < len(group); position++ {
			if group[position] < '0' || group[position] > '9' {
				return "", invalid()
			}
		}
		if index == 0 {
			if len(group) > 3 || (len(groups) > 1 && group[0] == '0') {
				return "", invalid()
			}
		} else if len(group) != 3 {
			return "", invalid()
		}
	}
	return strings.Join(groups, ""), nil
}

// FormatPlain formats an amount as plain text with exactly the currency's
// number of decimals.
func FormatPlain(m Money) string {
	exponent := m.Currency.MinorExponent()
	if exponent == 0 {
		return strconv.FormatInt(m.Minor, 10)
	}

	scale := uint64(1)
	for index := uint32(0); index < exponent; index++ {
		scale *= 10
	}
	absolute := new(big.Int).Abs(big.NewInt(m.Minor)).Uint64()
	whole := absolute / scale
	fraction := absolute % scale
	sign := ""
	if m.Minor < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%0*d", sign, whole, int(exponent), fraction)
}

// rustQuote quotes a string as Rust's Debug formatting does.
func rustQuote(text string) string {
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

func addInt64(left, right int64) (int64, bool) {
	sum := left + right
	if (right > 0 && sum < left) || (right < 0 && sum > left) {
		return 0, false
	}
	return sum, true
}
