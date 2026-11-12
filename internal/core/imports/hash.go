// Booking hashes, ported from FinDesk's transactionHash/importHash and
// StatementImportService.hashes. Ports
// crates/finglass-core/src/import/hash.rs.
package imports

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

// ISODate formats a date as the ISO text FinDesk's hash uses.
func ISODate(date civil.Date) string {
	return date.String()
}

// PlainAmount formats an amount like Java's stripTrailingZeros().toPlainString().
func PlainAmount(amount decimal.Decimal) string {
	return amount.Normalize().String()
}

// TransactionHash is SHA-256 over the fields that identify a booking; used for
// duplicate detection.
func TransactionHash(date civil.Date, amount decimal.Decimal, partner *string, purpose *string) string {
	partnerText := ""
	if partner != nil {
		partnerText = *partner
	}
	purposeText := ""
	if purpose != nil {
		purposeText = *purpose
	}
	input := fmt.Sprintf("%s|%s|%s|%s", ISODate(date), PlainAmount(amount), partnerText, purposeText)
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}

// ImportHash is the hash for bank-imported bookings. The discriminator is the
// bank's booking id, else the running balance after
// stripTrailingZeros().toPlainString(), else the prima-nota number, else empty.
func ImportHash(
	date civil.Date,
	amount decimal.Decimal,
	partnerName *string,
	purpose *string,
	iban string,
	bankID *string,
	balanceAfter *decimal.Decimal,
	primaNota *string,
) string {
	var discriminator string
	switch {
	case bankID != nil:
		discriminator = *bankID
	case balanceAfter != nil:
		discriminator = PlainAmount(*balanceAfter)
	case primaNota != nil:
		discriminator = *primaNota
	}
	purposeText := ""
	if purpose != nil {
		purposeText = *purpose
	}
	combined := purposeText + "#" + iban + "#" + discriminator
	return TransactionHash(date, amount, partnerName, &combined)
}

// Hashes returns dedup hashes for one file. It identifies a booking by its
// fields plus the bank reference or running balance; without either, by its
// position among identical bookings that day.
func Hashes(iban string, bookings []ImportedBooking) []string {
	seen := map[string]int{}
	result := make([]string, 0, len(bookings))
	for _, booking := range bookings {
		partnerText := ""
		if booking.PartnerName != nil {
			partnerText = *booking.PartnerName
		}
		purposeText := ""
		if booking.Purpose != nil {
			purposeText = *booking.Purpose
		}
		key := fmt.Sprintf("%s|%s|%s|%s", ISODate(booking.Date), PlainAmount(booking.Amount), partnerText, purposeText)
		var discriminator string
		switch {
		case booking.BankReference != nil:
			discriminator = *booking.BankReference
		case booking.BalanceAfter != nil:
			discriminator = PlainAmount(*booking.BalanceAfter)
		default:
			seen[key]++
			discriminator = fmt.Sprintf("occ%d", seen[key])
		}
		combined := purposeText + "#" + iban + "#file:" + discriminator
		result = append(result, TransactionHash(
			booking.Date,
			booking.Amount,
			booking.PartnerName,
			&combined,
		))
	}
	return result
}
