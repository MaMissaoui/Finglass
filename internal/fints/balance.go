// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

// Balance is a booked account balance from HISAL.
type Balance struct {
	// Amount is the booked amount, negative for a debit.
	Amount decimal.Decimal
	// Currency is the balance currency, or the header currency when the
	// balance group has none.
	Currency string
	// CurrencyStated reports whether the bank stated the currency (in the
	// balance or the segment header) rather than the EUR fallback fints-rs uses.
	CurrencyStated bool
	// Date is the balance date. It is the zero Date unless HasDate is true.
	Date Date
	// HasDate reports whether the bank sent a valid balance date.
	HasDate bool
	// PendingAmount is the pending (vorgemerkter) amount, when present.
	PendingAmount *decimal.Decimal
	// PendingDate is the pending amount's date. It is the zero Date unless
	// HasPendingDate is true.
	PendingDate Date
	// HasPendingDate reports whether the bank sent a valid pending date.
	HasPendingDate bool
	// CreditLine is the credit line, when present.
	CreditLine *decimal.Decimal
	// Available is the available amount, when present.
	Available *decimal.Decimal
}

// String returns the numeric fields only, never bank text.
func (b Balance) String() string {
	return fmt.Sprintf(
		"fints.Balance{amount:%s currency:%s date:%s pendingAmount:%s pendingDate:%s creditLine:%s available:%s}",
		b.Amount, b.Currency, b.dateText(), decimalText(b.PendingAmount),
		b.pendingDateText(), decimalText(b.CreditLine), decimalText(b.Available),
	)
}

// GoString returns the numeric fields only, never bank text.
func (b Balance) GoString() string {
	return b.String()
}

// Format writes the numeric fields only, never bank text, for every fmt verb.
func (b Balance) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(b.String()))
}

func (b Balance) dateText() string {
	if !b.HasDate {
		return ""
	}
	return b.Date.String()
}

func (b Balance) pendingDateText() string {
	if !b.HasPendingDate {
		return ""
	}
	return b.PendingDate.String()
}

func decimalText(value *decimal.Decimal) string {
	if value == nil {
		return ""
	}
	return value.String()
}

// ParseBalance ports fints-rs's parse_hisal. It reports false when the booked
// amount is missing or malformed, or when the balance group is too short.
func ParseBalance(segment ParsedSegment) (Balance, bool) {
	const balanceIndex = 4

	balanceGroup := segment.DEG(balanceIndex)
	if balanceGroup.Len() < 3 {
		return Balance{}, false
	}

	amount, ok := parseAmount(balanceGroup.Text(1))
	if !ok {
		return Balance{}, false
	}
	if balanceGroup.Text(0) == "D" {
		amount = amount.Neg()
	}

	currency := balanceGroup.Text(2)
	if currency == "" {
		currency = segment.DEG(3).Text(0)
	}
	stated := currency != ""
	if !stated {
		currency = "EUR"
	}

	balance := Balance{Amount: amount, Currency: currency, CurrencyStated: stated}
	if date, ok := parseFinTSDate(balanceGroup.Text(3)); ok {
		balance.Date = date
		balance.HasDate = true
	}

	pendingGroup := segment.DEG(balanceIndex + 1)
	if pendingGroup.Len() >= 2 {
		if pendingAmount, ok := parseAmount(pendingGroup.Text(1)); ok {
			if pendingGroup.Text(0) == "D" {
				pendingAmount = pendingAmount.Neg()
			}
			balance.PendingAmount = &pendingAmount
		}
		if pendingDate, ok := parseFinTSDate(pendingGroup.Text(3)); ok {
			balance.PendingDate = pendingDate
			balance.HasPendingDate = true
		}
	}

	creditGroup := segment.DEG(balanceIndex + 2)
	if creditGroup.Len() >= 1 {
		if creditLine, ok := parseAmount(creditGroup.Text(0)); ok {
			balance.CreditLine = &creditLine
		}
	}

	availableGroup := segment.DEG(balanceIndex + 3)
	if availableGroup.Len() >= 1 {
		if available, ok := parseAmount(availableGroup.Text(0)); ok {
			balance.Available = &available
		}
	}

	return balance, true
}

// ParseHISAL is the segment-named form of [ParseBalance].
func ParseHISAL(segment ParsedSegment) (Balance, bool) {
	return ParseBalance(segment)
}

// Balance returns the first HISAL balance, when there is one.
func (r Response) Balance() (Balance, bool) {
	segment, ok := r.findSegment("HISAL")
	if !ok {
		return Balance{}, false
	}
	return ParseBalance(segment)
}

func parseAmount(value string) (decimal.Decimal, bool) {
	if value == "" {
		return decimal.Zero, false
	}
	parsed, err := decimal.Parse(strings.ReplaceAll(value, ",", "."))
	if err != nil {
		return decimal.Zero, false
	}
	return parsed, true
}

func parseFinTSDate(value string) (Date, bool) {
	if len(value) != 8 {
		return Date{}, false
	}
	digits := make([]int, 8)
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return Date{}, false
		}
		digits[index] = int(value[index] - '0')
	}
	date := Date{
		Year:  digits[0]*1000 + digits[1]*100 + digits[2]*10 + digits[3],
		Month: digits[4]*10 + digits[5],
		Day:   digits[6]*10 + digits[7],
	}
	if !date.valid() || date.Day > daysInMonth(date.Year, date.Month) {
		return Date{}, false
	}
	return date, true
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 0
	}
}
