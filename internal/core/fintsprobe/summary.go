package fintsprobe

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

// StatementFormat is the parsed statement format in a summary line.
type StatementFormat uint8

// The statement formats.
const (
	// StatementMT940 is an MT940 statement.
	StatementMT940 StatementFormat = iota
	// StatementCamt is a camt statement.
	StatementCamt
)

// Label returns the format's summary label.
func (f StatementFormat) Label() string {
	if f == StatementCamt {
		return "camt"
	}
	return "MT940"
}

// StatementState is the statement result for one account.
//
// The zero value is unavailable. Available is true for a successful request,
// including one with zero bookings.
type StatementState struct {
	// Available reports whether the statement request succeeded.
	Available bool
	// BookingCount is the number of booked entries.
	BookingCount int
	// Earliest is the earliest booking date, YYYY-MM-DD, or "" when none.
	Earliest string
	// Latest is the latest booking date, YYYY-MM-DD, or "" when none.
	Latest string
	// Format is the parsed statement format.
	Format StatementFormat
}

// IsAvailable reports whether the statement request succeeded.
func (s StatementState) IsAvailable() bool {
	return s.Available
}

// UnavailableStatements returns the state of a failed statement request.
func UnavailableStatements() StatementState {
	return StatementState{}
}

// BookedStatements returns an available state for count booked entries.
func BookedStatements(count int, earliest, latest string, format StatementFormat) StatementState {
	return StatementState{
		Available:    true,
		BookingCount: count,
		Earliest:     earliest,
		Latest:       latest,
		Format:       format,
	}
}

// AccountSummary is the plain, printable summary of one account. It stores only
// a masked IBAN.
type AccountSummary struct {
	// IBANMasked is the masked IBAN.
	IBANMasked string
	// Currency is the account or balance currency.
	Currency string
	// BalancePresent reports whether the bank sent a balance.
	BalancePresent bool
	// Statement is the statement result.
	Statement StatementState
}

// NewAccountSummary builds a summary and masks the IBAN.
func NewAccountSummary(iban, currency string, balancePresent bool, statement StatementState) AccountSummary {
	return AccountSummary{
		IBANMasked:     MaskIBAN(iban),
		Currency:       currency,
		BalancePresent: balancePresent,
		Statement:      statement,
	}
}

// SummaryLine formats one account line. The IBAN is already masked here, and
// no amount or bank text is ever read.
func SummaryLine(summary AccountSummary) string {
	balance := "absent"
	if summary.BalancePresent {
		balance = "present"
	}

	currency := summary.Currency
	if currency == "" {
		currency = "-"
	}

	statements := "statements unavailable"
	if summary.Statement.IsAvailable() {
		format := summary.Statement.Format.Label()
		if summary.Statement.BookingCount == 0 {
			statements = fmt.Sprintf("statements 0 bookings (no bookings) %s", format)
		} else {
			statements = fmt.Sprintf(
				"statements %d bookings (%s..%s) %s",
				summary.Statement.BookingCount,
				summary.Statement.Earliest,
				summary.Statement.Latest,
				format,
			)
		}
	}

	return fmt.Sprintf(
		"IBAN %s | currency %s | balance %s | %s",
		summary.IBANMasked, currency, balance, statements,
	)
}

// NoGoKind is the category of a NO-GO reason.
type NoGoKind uint8

// The NO-GO categories.
const (
	// NoGoLoginRejected is a rejected PIN or a locked account.
	NoGoLoginRejected NoGoKind = iota
	// NoGoTanMethodUnsupported is an optical or photographic TAN method.
	NoGoTanMethodUnsupported
	// NoGoTanTimeout is a poll limit or the five-minute TAN deadline.
	NoGoTanTimeout
	// NoGoStatementsUnavailable is a failed statement request or parse.
	NoGoStatementsUnavailable
	// NoGoOther is any other failure; Other names its kind.
	NoGoOther
)

// NoGoReason is a NO-GO category with the bank codes that caused it, when any.
//
// Only four-digit return codes are kept. Bank message texts are never stored.
type NoGoReason struct {
	// Kind is the category.
	Kind NoGoKind
	// Other is the kind name for NoGoOther, for example "Transport".
	Other string
	// Codes are the bank return codes that caused the failure.
	Codes []string
}

// Label returns the reason without codes.
func (r NoGoReason) Label() string {
	switch r.Kind {
	case NoGoLoginRejected:
		return "login rejected"
	case NoGoTanMethodUnsupported:
		return "TAN method unsupported"
	case NoGoTanTimeout:
		return "TAN timeout"
	case NoGoStatementsUnavailable:
		return "statements unavailable"
	default:
		return "other: " + r.Other
	}
}

// String returns the reason and, when present, its bank codes. It never prints
// a bank message.
func (r NoGoReason) String() string {
	label := r.Label()
	codes := safeCodes(r.Codes)
	if len(codes) == 0 {
		return label
	}
	return label + ": " + strings.Join(codes, ", ")
}

// Decision is the final probe decision.
type Decision struct {
	// Go reports whether every account had a balance and statements.
	Go bool
	// Reason is set when Go is false.
	Reason NoGoReason
}

// ExitCode returns 0 for GO and 1 for NO-GO.
func (d Decision) ExitCode() int {
	if d.Go {
		return 0
	}
	return 1
}

// Decide chooses GO/NO-GO from the summaries and an optional failure.
//
// A failure always wins. Otherwise every account must have a balance and an
// available statement. A missing balance without a more specific error is
// reported as "other: BalanceAbsent".
func Decide(summaries []AccountSummary, failure *NoGoReason) Decision {
	if failure != nil {
		return Decision{Reason: *failure}
	}
	// No account checked is never a GO: the login saw nothing to read.
	if len(summaries) == 0 {
		return Decision{Reason: NoGoReason{Kind: NoGoOther, Other: "NoAccounts"}}
	}
	for _, summary := range summaries {
		if !summary.BalancePresent {
			return Decision{Reason: NoGoReason{Kind: NoGoOther, Other: "BalanceAbsent"}}
		}
		if !summary.Statement.IsAvailable() {
			return Decision{Reason: NoGoReason{Kind: NoGoStatementsUnavailable}}
		}
	}
	return Decision{Go: true}
}

// isUnsupportedTanMethod reports whether a bank TAN method name needs an
// optical or photographic process the probe cannot render. The name is bank
// text: it is only matched here and never printed.
func isUnsupportedTanMethod(name string) bool {
	lower := strings.ToLower(name)
	optical := strings.Contains(lower, "flicker") || strings.Contains(lower, "photo")
	chip := strings.Contains(lower, "chiptan") &&
		!strings.Contains(lower, "manual") &&
		!strings.Contains(lower, "manuell")
	return optical || chip
}

func errorKindName(kind fints.ErrorKind) string {
	switch kind {
	case fints.ErrorKindPinRejected:
		return "PinRejected"
	case fints.ErrorKindAccountLocked:
		return "AccountLocked"
	case fints.ErrorKindBankError:
		return "BankError"
	case fints.ErrorKindTanRejected:
		return "TanRejected"
	case fints.ErrorKindTransport:
		return "Transport"
	case fints.ErrorKindProtocol:
		return "Protocol"
	case fints.ErrorKindCancelled:
		return "Cancelled"
	case fints.ErrorKindPollLimit:
		return "PollLimit"
	case fints.ErrorKindTouchdownLimit:
		return "TouchdownLimit"
	case fints.ErrorKindInvalidState:
		return "InvalidState"
	default:
		return "Unknown"
	}
}

// classifyError maps a dialog failure to a NO-GO reason.
func classifyError(err error) NoGoReason {
	var dialogError *fints.DialogError
	if !errors.As(err, &dialogError) {
		return NoGoReason{Kind: NoGoOther, Other: "Unknown"}
	}
	codes := safeCodes(dialogError.Codes)
	switch dialogError.Kind {
	case fints.ErrorKindPinRejected, fints.ErrorKindAccountLocked:
		return NoGoReason{Kind: NoGoLoginRejected, Codes: codes}
	case fints.ErrorKindPollLimit:
		return NoGoReason{Kind: NoGoTanTimeout}
	case fints.ErrorKindTanRejected:
		return NoGoReason{Kind: NoGoOther, Other: "TanRejected", Codes: codes}
	default:
		return NoGoReason{
			Kind:  NoGoOther,
			Other: errorKindName(dialogError.Kind),
			Codes: codes,
		}
	}
}

// classifyStatementsError maps a statement-step failure. Login and TAN timeout
// stay specific; every other failure means the statements are unavailable.
func classifyStatementsError(err error) NoGoReason {
	reason := classifyError(err)
	switch reason.Kind {
	case NoGoLoginRejected, NoGoTanTimeout:
		return reason
	default:
		return NoGoReason{Kind: NoGoStatementsUnavailable, Codes: reason.Codes}
	}
}

// safeCodes keeps only four-digit bank return codes, so no bank text can reach
// a NO-GO line.
func safeCodes(codes []string) []string {
	safe := make([]string, 0, len(codes))
	for _, code := range codes {
		if len(code) != 4 {
			continue
		}
		digits := true
		for index := 0; index < len(code); index++ {
			if code[index] < '0' || code[index] > '9' {
				digits = false
				break
			}
		}
		if digits {
			safe = append(safe, code)
		}
	}
	return safe
}
