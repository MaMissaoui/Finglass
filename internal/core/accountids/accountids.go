// Package accountids handles the identifiers stored in accounts.iban, ported
// from crates/finglass-core/src/account_ids.rs.
//
// An offline bank's account may name a card instead of an IBAN: the synthetic
// identifier "CARD" plus exactly the card's last 4 digits (for example
// "CARD4321"). A full card number is never stored.
package accountids

import (
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/iban"
)

// CardPrefix is the prefix of the synthetic identifier of a card account.
const CardPrefix = "CARD"

// Card returns the synthetic account identifier of a card account.
func Card(last4 string) string {
	return CardPrefix + strings.TrimSpace(last4)
}

// IsCard reports whether id names a card account ("CARD" plus 4 digits) instead
// of an IBAN.
func IsCard(id string) bool {
	tail, ok := strings.CutPrefix(id, CardPrefix)
	if !ok {
		return false
	}
	runes := []rune(tail)
	if len(runes) != 4 {
		return false
	}
	for _, character := range runes {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// Display returns an account identifier's display text: a card's last 4 digits,
// otherwise the formatted IBAN.
func Display(id string) string {
	if IsCard(id) {
		return "•••• " + lastFour(id)
	}
	return iban.Format(id)
}

func lastFour(text string) string {
	runes := []rune(text)
	start := len(runes) - 4
	if start < 0 {
		start = 0
	}
	return string(runes[start:])
}
