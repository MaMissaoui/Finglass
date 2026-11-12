// Package format formats amounts, dates and numbers per language, ported from
// crates/finglass-core/src/format.rs (itself ported from FinDesk's
// util/Formats.kt).
//
// Money values are money.Money in integer minor units and use the currency's
// exponent. No money is ever formatted through float64.
package format

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/iban"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// NoBreakSpace keeps a number and its unit together.
const NoBreakSpace = '\u00a0'

// separators returns the decimal and grouping separators of lang.
func separators(lang i18n.Language) (rune, rune) {
	switch lang {
	case i18n.De:
		return ',', '.'
	case i18n.Fr:
		// Java 21's French locale groups with a narrow no-break space.
		return ',', '\u202f'
	default:
		return '.', ','
	}
}

// symbolFor returns the currency symbol as money shows it, including the
// trailing space of an unknown code.
func symbolFor(currency money.Currency) string {
	switch currency.String() {
	case "AUD":
		return "A$"
	case "BRL":
		return "R$"
	case "CAD":
		return "CA$"
	case "CNY":
		return "CN¥"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "HKD":
		return "HK$"
	case "ILS":
		return "₪"
	case "INR":
		return "₹"
	case "JPY":
		return "¥"
	case "KRW":
		return "₩"
	case "MXN":
		return "MX$"
	case "NZD":
		return "NZ$"
	case "PHP":
		return "₱"
	case "TWD":
		return "NT$"
	case "USD":
		return "$"
	case "VND":
		return "₫"
	case "XAF":
		return "FCFA"
	case "XCD":
		return "EC$"
	case "XOF":
		return "F\u202fCFA"
	case "XPF":
		return "CFPF"
	case "XXX":
		return "¤"
	}
	code := currency.String()
	if index := sort.SearchStrings(javaCurrencyCodes, code); index < len(javaCurrencyCodes) && javaCurrencyCodes[index] == code {
		return code
	}
	return code + " "
}

// CurrencySymbol returns the currency's symbol without the trailing space of an
// unknown code.
func CurrencySymbol(currency money.Currency) string {
	return strings.TrimSpace(symbolFor(currency))
}

// groupDigits groups a non-empty digit string in blocks of three from the
// right.
func groupDigits(digits string, separator rune) string {
	var grouped strings.Builder
	grouped.Grow(len(digits) + len(digits)/3)
	for index, character := range []rune(digits) {
		if index > 0 && (len([]rune(digits))-index)%3 == 0 {
			grouped.WriteRune(separator)
		}
		grouped.WriteRune(character)
	}
	return grouped.String()
}

// Money formats "€1,234.56" / "-€1,234.56"; with signed, positive amounts get a
// leading "+".
//
// German puts the number first, separated from the trimmed symbol by a no-break
// space: "2.450,37 €".
func Money(amount money.Money, lang i18n.Language, signed bool) string {
	decimalSeparator, groupingSeparator := separators(lang)
	exponent := amount.Currency.MinorExponent()
	absolute := absoluteMinor(amount.Minor)
	scale := uint64(1)
	for index := uint32(0); index < exponent; index++ {
		scale *= 10
	}
	whole := absolute / scale
	fraction := absolute % scale
	digits := groupDigits(strconv.FormatUint(whole, 10), groupingSeparator)

	formatted := digits
	if exponent != 0 {
		formatted = fmt.Sprintf("%s%c%0*d", digits, decimalSeparator, int(exponent), fraction)
	}
	sign := ""
	if amount.Minor < 0 {
		sign = "-"
	} else if signed && amount.Minor > 0 {
		sign = "+"
	}
	symbol := symbolFor(amount.Currency)

	if lang == i18n.De {
		return sign + formatted + string(NoBreakSpace) + strings.TrimSpace(symbol)
	}
	return sign + symbol + formatted
}

// WholeMoney formats whole currency units for chart axes: "€4,000" / "€4.000";
// German puts the number first: "4.000 €".
func WholeMoney(amount money.Money, lang i18n.Language) string {
	_, groupingSeparator := separators(lang)
	exponent := amount.Currency.MinorExponent()
	absolute := absoluteMinor(amount.Minor)
	scale := uint64(1)
	for index := uint32(0); index < exponent; index++ {
		scale *= 10
	}
	rounded := absolute
	if exponent != 0 {
		rounded = (absolute + scale/2) / scale
	}
	digits := groupDigits(strconv.FormatUint(rounded, 10), groupingSeparator)
	sign := ""
	if amount.Minor < 0 {
		sign = "-"
	}
	symbol := symbolFor(amount.Currency)

	if lang == i18n.De {
		return sign + digits + string(NoBreakSpace) + strings.TrimSpace(symbol)
	}
	return sign + symbol + digits
}

// Percent formats whole percentages of one share.
//
// Tiny shares between 0 and 0.5 % read "<1%" / "<1 %"; a zero share shows "0".
func Percent(share float64, lang i18n.Language) string {
	separator := ""
	if lang != i18n.En {
		separator = string(NoBreakSpace)
	}
	var text string
	switch {
	case share <= 0:
		text = "0"
	case share < 0.005:
		text = "<1"
	default:
		percent := share * 100
		if math.IsNaN(percent) {
			percent = 0
		}
		percent = math.Max(0, math.Min(100, percent))
		text = strconv.FormatInt(int64(math.Round(percent)), 10)
	}
	return text + separator + "%"
}

// Shares returns whole percentages of amounts against their sum, rounded by the
// largest remainder, so the list always sums to exactly 100 when it shows every
// share ([1, 1, 1] → 34, 33, 33). Empty or all-zero lists give all zeros.
func Shares(amounts []money.Money) []int64 {
	if len(amounts) == 0 {
		return []int64{}
	}

	total := new(big.Int)
	for _, amount := range amounts {
		total.Add(total, big.NewInt(amount.Minor))
	}
	if total.Sign() == 0 {
		return make([]int64, len(amounts))
	}

	scale := big.NewInt(10_000_000_000)
	remainders := make([]*big.Int, len(amounts))
	floors := make([]*big.Int, len(amounts))
	sumFloors := new(big.Int)
	for index, amount := range amounts {
		raw := new(big.Int).Mul(big.NewInt(amount.Minor), big.NewInt(100))
		raw.Mul(raw, scale)
		raw.Quo(raw, total)
		floor := new(big.Int).Div(raw, scale)
		remainder := new(big.Int).Sub(raw, new(big.Int).Mul(floor, scale))
		remainders[index] = remainder
		floors[index] = floor
		sumFloors.Add(sumFloors, floor)
	}
	missing := new(big.Int).Sub(big.NewInt(100), sumFloors)

	order := make([]int, len(amounts))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(left, right int) bool {
		comparison := remainders[order[left]].Cmp(remainders[order[right]])
		if comparison != 0 {
			return comparison > 0
		}
		return order[left] < order[right]
	})

	result := make([]int64, len(amounts))
	for index, floor := range floors {
		result[index] = floor.Int64()
	}
	if missing.Sign() > 0 && missing.IsInt64() {
		remaining := missing.Int64()
		for _, index := range order {
			if remaining <= 0 {
				break
			}
			result[index]++
			remaining--
		}
	}
	return result
}

// Date formats "dd.MM.yyyy".
func Date(date civil.Date) string {
	return fmt.Sprintf("%02d.%02d.%04d", date.Day(), int(date.Month()), date.Year())
}

// ParseDate parses what users type in date fields: "05.11.2026" or "5.11.2026".
// It is strict, so an impossible day such as 31.02. is rejected instead of
// silently becoming 28.02.
func ParseDate(input string) (civil.Date, bool) {
	parts := strings.Split(strings.TrimSpace(input), ".")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	day, ok := oneOrTwoDigits(parts[0])
	if !ok {
		return civil.Date{}, false
	}
	month, ok := oneOrTwoDigits(parts[1])
	if !ok {
		return civil.Date{}, false
	}
	year, ok := fourDigits(parts[2])
	if !ok {
		return civil.Date{}, false
	}
	return civil.New(year, time.Month(month), day)
}

func oneOrTwoDigits(part string) (int, bool) {
	if len(part) < 1 || len(part) > 2 {
		return 0, false
	}
	value := 0
	for index := 0; index < len(part); index++ {
		if part[index] < '0' || part[index] > '9' {
			return 0, false
		}
		value = value*10 + int(part[index]-'0')
	}
	return value, true
}

func fourDigits(part string) (int, bool) {
	if len(part) != 4 {
		return 0, false
	}
	value := 0
	for index := 0; index < len(part); index++ {
		if part[index] < '0' || part[index] > '9' {
			return 0, false
		}
		value = value*10 + int(part[index]-'0')
	}
	return value, true
}

// DateTime formats "dd.MM.yyyy HH:mm" using t's own hour and minute.
func DateTime(t time.Time) string {
	return fmt.Sprintf("%s %02d:%02d", Date(civil.FromTime(t)), t.Hour(), t.Minute())
}

// IBAN groups an IBAN in blocks of four: "DE89 3704 0044 ...".
func IBAN(s string) string {
	return iban.Format(s)
}

// ParseDecimal parses a decimal out of untrusted file text, with FinDesk's
// import bounds: values with more than 15 integer digits or more than 10
// decimal places, or an input longer than 64 characters, are rejected.
func ParseDecimal(text string) (decimal.Decimal, bool) {
	if utf8.RuneCountInString(text) > 64 {
		return decimal.Decimal{}, false
	}
	value, err := decimal.Parse(strings.TrimSpace(text))
	if err != nil {
		return decimal.Decimal{}, false
	}
	scale := int64(value.Scale())
	if scale > 10 {
		return decimal.Decimal{}, false
	}
	mantissa := new(big.Int).Abs(value.Mantissa())
	mantissaDigits := int64(len(mantissa.String()))
	if mantissa.Sign() == 0 {
		mantissaDigits = 1
	}
	if mantissaDigits-scale > 15 {
		return decimal.Decimal{}, false
	}
	return value, true
}

// ParseAmount parses user input like "1.234,56", "1,234.56", "-12.5" or "12".
// It reports false if it isn't a number.
func ParseAmount(input string) (decimal.Decimal, bool) {
	text := strings.TrimSpace(input)
	text = strings.NewReplacer("€", "", " ", "", string(NoBreakSpace), "").Replace(text)
	if text == "" {
		return decimal.Decimal{}, false
	}

	lastComma := strings.LastIndex(text, ",")
	lastDot := strings.LastIndex(text, ".")
	if (lastComma >= 0 && lastDot >= 0 && lastComma > lastDot) || (lastComma >= 0 && lastDot < 0) {
		text = strings.ReplaceAll(text, ".", "")
		text = strings.ReplaceAll(text, ",", ".")
	} else {
		text = strings.ReplaceAll(text, ",", "")
	}

	value, ok := ParseDecimal(text)
	if !ok {
		return decimal.Decimal{}, false
	}
	value = value.RoundDP(2)
	value = value.Rescale(2)
	return value, true
}

func absoluteMinor(minor int64) uint64 {
	return new(big.Int).Abs(big.NewInt(minor)).Uint64()
}

// javaCurrencyCodes is every ISO 4217 code Java 21 knows, sorted for the binary
// search in symbolFor.
var javaCurrencyCodes = []string{
	"ADP", "AED", "AFA", "AFN", "ALL", "AMD", "ANG", "AOA", "ARS", "ATS", "AUD", "AWG",
	"AYM", "AZM", "AZN", "BAM", "BBD", "BDT", "BEF", "BGL", "BGN", "BHD", "BIF", "BMD",
	"BND", "BOB", "BOV", "BRL", "BSD", "BTN", "BWP", "BYB", "BYN", "BYR", "BZD", "CAD",
	"CDF", "CHE", "CHF", "CHW", "CLF", "CLP", "CNY", "COP", "COU", "CRC", "CSD", "CUC",
	"CUP", "CVE", "CYP", "CZK", "DEM", "DJF", "DKK", "DOP", "DZD", "EEK", "EGP", "ERN",
	"ESP", "ETB", "EUR", "FIM", "FJD", "FKP", "FRF", "GBP", "GEL", "GHC", "GHS", "GIP",
	"GMD", "GNF", "GRD", "GTQ", "GWP", "GYD", "HKD", "HNL", "HRK", "HTG", "HUF", "IDR",
	"IEP", "ILS", "INR", "IQD", "IRR", "ISK", "ITL", "JMD", "JOD", "JPY", "KES", "KGS",
	"KHR", "KMF", "KPW", "KRW", "KWD", "KYD", "KZT", "LAK", "LBP", "LKR", "LRD", "LSL",
	"LTL", "LUF", "LVL", "LYD", "MAD", "MDL", "MGA", "MGF", "MKD", "MMK", "MNT", "MOP",
	"MRO", "MRU", "MTL", "MUR", "MVR", "MWK", "MXN", "MXV", "MYR", "MZM", "MZN", "NAD",
	"NGN", "NIO", "NLG", "NOK", "NPR", "NZD", "OMR", "PAB", "PEN", "PGK", "PHP", "PKR",
	"PLN", "PTE", "PYG", "QAR", "ROL", "RON", "RSD", "RUB", "RUR", "RWF", "SAR", "SBD",
	"SCR", "SDD", "SDG", "SEK", "SGD", "SHP", "SIT", "SKK", "SLE", "SLL", "SOS", "SRD",
	"SRG", "SSP", "STD", "STN", "SVC", "SYP", "SZL", "THB", "TJS", "TMM", "TMT", "TND",
	"TOP", "TPE", "TRL", "TRY", "TTD", "TWD", "TZS", "UAH", "UGX", "USD", "USN", "USS",
	"UYI", "UYU", "UZS", "VEB", "VED", "VEF", "VES", "VND", "VUV", "WST", "XAD", "XAF",
	"XAG", "XAU", "XBA", "XBB", "XBC", "XBD", "XCD", "XCG", "XDR", "XFO", "XFU", "XOF",
	"XPD", "XPF", "XPT", "XSU", "XTS", "XUA", "XXX", "YER", "YUM", "ZAR", "ZMK", "ZMW",
	"ZWD", "ZWG", "ZWL", "ZWN", "ZWR",
}
