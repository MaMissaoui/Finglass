// Converting FinDesk's stored values into Finglass's formats.
//
// Money leaves FinDesk as REAL or INTEGER. It is parsed through decimal, never
// through a float multiplication, and a value with more decimals than the
// currency allows is rejected instead of rounded.
package findeskimport

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/prefs"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
)

type convertErrorKind int

const (
	convertInvalid convertErrorKind = iota
	convertFractionalAmount
)

// convertError says why a source value could not be converted.
type convertError struct {
	kind convertErrorKind
}

func (e convertError) Error() string {
	if e.kind == convertFractionalAmount {
		return "FractionalAmount"
	}
	return "Invalid"
}

var (
	errInvalid          = convertError{kind: convertInvalid}
	errFractionalAmount = convertError{kind: convertFractionalAmount}
)

// sourceDecimal converts a source number the way Rust's decimal() does:
// INTEGER exactly, REAL through the shortest round-trip text, TEXT trimmed and
// parsed.
func sourceDecimal(value *sourceNumber) (decimal.Decimal, bool) {
	if value == nil {
		return decimal.Decimal{}, false
	}
	switch value.kind {
	case sourceInteger:
		return decimal.FromInt(value.integer), true
	case sourceReal:
		if math.IsNaN(value.real) || math.IsInf(value.real, 0) {
			return decimal.Decimal{}, false
		}
		parsed, err := decimal.Parse(strconv.FormatFloat(value.real, 'f', -1, 64))
		if err != nil {
			return decimal.Decimal{}, false
		}
		return parsed, true
	case sourceText:
		parsed, err := decimal.Parse(strings.TrimSpace(value.text))
		if err != nil {
			return decimal.Decimal{}, false
		}
		return parsed, true
	default:
		return decimal.Decimal{}, false
	}
}

func requiredMinor(value *sourceNumber, currency money.Currency) (int64, error) {
	if value == nil {
		return 0, errInvalid
	}
	amount, ok := sourceDecimal(value)
	if !ok {
		return 0, errInvalid
	}
	if int64(amount.Normalize().Scale()) > int64(currency.MinorExponent()) {
		return 0, errFractionalAmount
	}
	converted, err := imports.AmountToMoney(amount, currency)
	if err != nil {
		return 0, errInvalid
	}
	return converted.Minor, nil
}

func optionalMinor(value *sourceNumber, currency money.Currency) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := requiredMinor(value, currency)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func requiredDate(value *string) (civil.Date, error) {
	if value == nil {
		return civil.Date{}, errInvalid
	}
	date, err := civil.ParseISO(*value)
	if err != nil {
		return civil.Date{}, errInvalid
	}
	return date, nil
}

func optionalDate(value *string) (*civil.Date, error) {
	if value == nil {
		return nil, nil
	}
	date, err := requiredDate(value)
	if err != nil {
		return nil, err
	}
	return &date, nil
}

func dateText(date civil.Date) (string, error) {
	text, err := repo.FormatDate(date)
	if err != nil {
		return "", errInvalid
	}
	return text, nil
}

func requiredTimestamp(value *string, offset *time.Location) (string, error) {
	if value == nil {
		return "", errInvalid
	}
	timestamp, ok := parseTimestamp(*value, offset)
	if !ok {
		return "", errInvalid
	}
	text, err := repo.FormatTimestamp(timestamp)
	if err != nil {
		return "", errInvalid
	}
	return text, nil
}

func optionalTimestamp(value *string, offset *time.Location) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, err := requiredTimestamp(value, offset)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

// parseTimestamp parses `yyyy-MM-dd HH:mm:ss` with an optional fraction of 1 to
// 9 digits, without a zone.
func parseTimestamp(value string, offset *time.Location) (time.Time, bool) {
	datePart, timePart, found := strings.Cut(value, " ")
	if !found {
		return time.Time{}, false
	}
	date, err := civil.ParseISO(datePart)
	if err != nil {
		return time.Time{}, false
	}

	clock := timePart
	fraction := ""
	if before, after, hasFraction := strings.Cut(timePart, "."); hasFraction {
		clock = before
		fraction = after
	}

	parts := strings.Split(clock, ":")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	hour, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	minute, err := strconv.ParseUint(parts[1], 10, 8)
	if err != nil {
		return time.Time{}, false
	}
	second, err := strconv.ParseUint(parts[2], 10, 8)
	if err != nil {
		return time.Time{}, false
	}

	nanosecond := uint64(0)
	if fraction != "" {
		if len(fraction) > 9 {
			return time.Time{}, false
		}
		for index := 0; index < len(fraction); index++ {
			if fraction[index] < '0' || fraction[index] > '9' {
				return time.Time{}, false
			}
		}
		padded := fraction + strings.Repeat("0", 9-len(fraction))
		nanosecond, err = strconv.ParseUint(padded, 10, 32)
		if err != nil {
			return time.Time{}, false
		}
	}

	if hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}

	if offset == nil {
		offset = time.UTC
	}
	return time.Date(
		date.Year(),
		date.Month(),
		date.Day(),
		int(hour),
		int(minute),
		int(second),
		int(nanosecond),
		offset,
	), true
}

func requiredBool(value *int64) (bool, error) {
	if value == nil {
		return false, errInvalid
	}
	switch *value {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errInvalid
	}
}

func optionalBool(value *int64) (*bool, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := requiredBool(value)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func convertCurrency(value *string) (money.Currency, error) {
	if value == nil {
		return money.Currency{}, errInvalid
	}
	currency, err := money.NewCurrency(strings.TrimSpace(*value))
	if err != nil {
		return money.Currency{}, errInvalid
	}
	return currency, nil
}

func convertAccountType(value *string) (model.AccountType, error) {
	if value == nil {
		return "", errInvalid
	}
	accountType, ok := model.AccountTypeFromStored(*value)
	if !ok {
		return "", errInvalid
	}
	return accountType, nil
}

func convertRuleField(value *string) (model.RuleField, error) {
	if value == nil {
		return "", errInvalid
	}
	field, ok := model.RuleFieldFromStored(*value)
	if !ok {
		return "", errInvalid
	}
	return field, nil
}

func convertRuleDirection(value *string) (model.RuleDirection, error) {
	if value == nil {
		return "", errInvalid
	}
	direction, ok := model.RuleDirectionFromStored(*value)
	if !ok {
		return "", errInvalid
	}
	return direction, nil
}

func convertConfidence(value *sourceNumber) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	amount, ok := sourceDecimal(value)
	if !ok {
		return nil, errInvalid
	}
	confidence, err := strconv.ParseFloat(amount.String(), 64)
	if err != nil {
		return nil, errInvalid
	}
	return &confidence, nil
}

// convertTheme maps FinDesk's ui.dark_theme text to Finglass's theme code.
func convertTheme(value string) (string, bool) {
	switch {
	case equalASCIIFold(value, "true"):
		return prefs.ThemeDark.Code(), true
	case equalASCIIFold(value, "false"):
		return prefs.ThemeLight.Code(), true
	default:
		return "", false
	}
}

// equalASCIIFold reports whether a and b are equal, folding ASCII letters only.
func equalASCIIFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := 0; index < len(a); index++ {
		left := a[index]
		right := b[index]
		if left >= 'A' && left <= 'Z' {
			left += 'a' - 'A'
		}
		if right >= 'A' && right <= 'Z' {
			right += 'a' - 'A'
		}
		if left != right {
			return false
		}
	}
	return true
}
