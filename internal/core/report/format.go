// Package report formats amounts, dates and month names for the report
// exports.
//
// Ported from FinDesk's `util/Formats.kt` and the number handling of
// `TaxReportExporter.kt`, via crates/finglass-core/src/report/format.rs.
// Reports use the app language's separators; dates always use `dd.MM.yyyy`.
package report

import (
	"math"
	"strconv"

	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// MonthName returns the full month name in the app language, like Java's
// Month.getDisplayName(TextStyle.FULL, locale).
func MonthName(lang i18n.Language, month uint8) string {
	en := [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	de := [12]string{
		"Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember",
	}
	fr := [12]string{
		"janvier", "février", "mars", "avril", "mai", "juin",
		"juillet", "août", "septembre", "octobre", "novembre", "décembre",
	}

	var names [12]string
	switch lang {
	case i18n.De:
		names = de
	case i18n.Fr:
		names = fr
	default:
		names = en
	}
	return names[monthIndex(month)]
}

// MonthShortName returns the short month name in the app language, like Java's
// Month.getDisplayName(TextStyle.SHORT, locale), which FinDesk's charts use.
func MonthShortName(lang i18n.Language, month uint8) string {
	en := [12]string{
		"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
	}
	de := [12]string{
		"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni",
		"Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez.",
	}
	fr := [12]string{
		"janv.", "févr.", "mars", "avr.", "mai", "juin",
		"juil.", "août", "sept.", "oct.", "nov.", "déc.",
	}

	var names [12]string
	switch lang {
	case i18n.De:
		names = de
	case i18n.Fr:
		names = fr
	default:
		names = en
	}
	return names[monthIndex(month)]
}

// monthIndex clamps a month to 1..12 and returns its 0-based array index.
func monthIndex(month uint8) int {
	if month < 1 {
		return 0
	}
	if month > 12 {
		return 11
	}
	return int(month) - 1
}

// Money formats a currency symbol followed by the grouped amount, two
// decimals; German puts the number first with a no-break space before the
// trimmed symbol.
func Money(amount money.Money, lang i18n.Language, signed bool) string {
	sign := ""
	if amount.Minor < 0 {
		sign = "-"
	} else if signed && amount.Minor > 0 {
		sign = "+"
	}

	if lang == i18n.De {
		return sign + grouped(amount, lang) + string(format.NoBreakSpace) + format.CurrencySymbol(amount.Currency)
	}
	return sign + format.CurrencySymbol(amount.Currency) + grouped(amount, lang)
}

// PDFAmount returns the tax-booking PDF amount without a currency symbol; same
// separators as Money.
func PDFAmount(amount money.Money, lang i18n.Language, signed bool) string {
	text := ""
	if amount.Minor < 0 {
		text = "-"
	} else if signed && amount.Minor > 0 {
		text = "+"
	}
	return text + grouped(amount, lang)
}

// grouped groups the absolute value with the language's separators, always
// with two decimal places.
func grouped(amount money.Money, lang i18n.Language) string {
	exponent := amount.Currency.MinorExponent()
	absolute := uint64(0)
	if amount.Minor < 0 {
		absolute = uint64(-(amount.Minor + 1)) + 1 // #nosec G115 -- two's-complement absolute value of i64::MIN fits uint64.
	} else {
		absolute = uint64(amount.Minor)
	}

	// Kotlin's exports use "#,##0.00" after setScale(2), so the result always
	// has two decimals.
	var cents uint64
	switch exponent {
	case 0:
		cents = saturatingMul(absolute, 100)
	case 1:
		cents = saturatingMul(absolute, 10)
	case 2:
		cents = absolute
	case 3:
		cents = saturatingAdd(absolute, 5) / 10
	default:
		divisor := saturatingPow10(exponent - 2)
		cents = saturatingAdd(absolute, divisor/2) / divisor
	}

	whole := cents / 100
	fraction := cents % 100
	var group, decimal rune
	switch lang {
	case i18n.De:
		group, decimal = '.', ','
	case i18n.Fr:
		group, decimal = '\u202f', ','
	default:
		group, decimal = ',', '.'
	}

	digits := strconv.FormatUint(whole, 10)
	var builder []rune
	builder = make([]rune, 0, len(digits)+len(digits)/3+4)
	for index, character := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			builder = append(builder, group)
		}
		builder = append(builder, character)
	}

	return string(builder) + string(decimal) + twoDigits(fraction)
}

// twoDigits formats a value 0..99 with exactly two decimal digits.
func twoDigits(value uint64) string {
	if value < 10 {
		return "0" + strconv.FormatUint(value, 10)
	}
	return strconv.FormatUint(value, 10)
}

func saturatingAdd(left, right uint64) uint64 {
	sum := left + right
	if sum < left {
		return math.MaxUint64
	}
	return sum
}

func saturatingMul(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > math.MaxUint64/right {
		return math.MaxUint64
	}
	return left * right
}

func saturatingPow10(exponent uint32) uint64 {
	result := uint64(1)
	for index := uint32(0); index < exponent; index++ {
		result = saturatingMul(result, 10)
	}
	return result
}
