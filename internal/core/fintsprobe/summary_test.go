package fintsprobe

import (
	"errors"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

func booked(count int, earliest, latest string, format StatementFormat) StatementState {
	return BookedStatements(count, earliest, latest, format)
}

func TestGoWhenEveryAccountHasBalanceAndStatements(t *testing.T) {
	summaries := []AccountSummary{
		NewAccountSummary(
			germanIBAN,
			"EUR",
			true,
			booked(3, "2024-01-01", "2024-01-31", StatementMT940),
		),
		NewAccountSummary(
			austrianIBAN,
			"EUR",
			true,
			booked(0, "", "", StatementMT940),
		),
	}
	if decision := Decide(summaries, nil); !decision.Go {
		t.Fatalf("decision = %+v, want GO", decision)
	}
}

func TestNoGoForEachReasonCategory(t *testing.T) {
	ok := NewAccountSummary(
		germanIBAN,
		"EUR",
		true,
		booked(0, "", "", StatementMT940),
	)

	cases := []NoGoReason{
		{Kind: NoGoLoginRejected},
		{Kind: NoGoTanMethodUnsupported},
		{Kind: NoGoTanTimeout},
		{Kind: NoGoOther, Other: "Transport"},
	}
	for _, reason := range cases {
		decision := Decide([]AccountSummary{ok}, &reason)
		if decision.Go || decision.Reason.Label() != reason.Label() {
			t.Fatalf("decision = %+v, want NO-GO %q", decision, reason.Label())
		}
	}

	noStatements := NewAccountSummary(germanIBAN, "EUR", true, UnavailableStatements())
	decision := Decide([]AccountSummary{noStatements}, nil)
	if decision.Go || decision.Reason.Label() != "statements unavailable" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestNoGoWhenBalanceIsAbsent(t *testing.T) {
	summary := NewAccountSummary(
		germanIBAN,
		"EUR",
		false,
		booked(1, "2024-02-01", "2024-02-01", StatementMT940),
	)
	decision := Decide([]AccountSummary{summary}, nil)
	if decision.Go || decision.Reason.Label() != "other: BalanceAbsent" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSummaryNeverContainsAFullIBAN(t *testing.T) {
	summary := NewAccountSummary(
		germanIBAN,
		"EUR",
		true,
		booked(3, "2024-01-01", "2024-01-31", StatementMT940),
	)
	line := SummaryLine(summary)
	if strings.Contains(line, germanIBAN) || strings.Contains(line, "DE89370400440532013000") {
		t.Fatalf("summary leaked the IBAN: %q", line)
	}
	want := "IBAN DE** **** **** **** **30 00 | currency EUR | balance present | statements 3 bookings (2024-01-01..2024-01-31) MT940"
	if line != want {
		t.Fatalf("SummaryLine =\n%q\nwant\n%q", line, want)
	}
}

func TestSummaryReportsZeroBookingsAndUnavailableStatements(t *testing.T) {
	zero := NewAccountSummary(
		austrianIBAN,
		"EUR",
		true,
		booked(0, "", "", StatementMT940),
	)
	zeroLine := SummaryLine(zero)
	if !strings.Contains(zeroLine, "0 bookings (no bookings) MT940") {
		t.Fatalf("zero line = %q", zeroLine)
	}
	if strings.Contains(zeroLine, "no bookings (no bookings)") {
		t.Fatalf("zero line = %q", zeroLine)
	}

	unavailable := NewAccountSummary(austrianIBAN, "", false, UnavailableStatements())
	unavailableLine := SummaryLine(unavailable)
	if !strings.Contains(unavailableLine, "currency -") {
		t.Fatalf("currency fallback missing: %q", unavailableLine)
	}
	if !strings.Contains(unavailableLine, "balance absent") {
		t.Fatalf("unavailable line = %q", unavailableLine)
	}
	if !strings.Contains(unavailableLine, "statements unavailable") {
		t.Fatalf("unavailable line = %q", unavailableLine)
	}
}

func TestSummaryUsesTheCamtLabel(t *testing.T) {
	summary := NewAccountSummary(
		austrianIBAN,
		"EUR",
		true,
		booked(2, "2026-01-02", "2026-01-03", StatementCamt),
	)
	line := SummaryLine(summary)
	if !strings.Contains(line, "(2026-01-02..2026-01-03) camt") {
		t.Fatalf("summary line = %q", line)
	}
}

func TestDetectsOpticalTanMethods(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"chipTAN flicker", true},
		{"photoTAN", true},
		{"chipTAN-QR", true},
		{"chipTAN optisch", true},
		{"chipTAN manuell", false},
		{"chipTAN manual", false},
		{"pushTAN 2.0", false},
		{"SMS-TAN", false},
	}
	for _, test := range cases {
		if got := isUnsupportedTanMethod(test.name); got != test.want {
			t.Fatalf("isUnsupportedTanMethod(%q) = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestNoGoReasonAppendsOnlyFourDigitCodes(t *testing.T) {
	reason := NoGoReason{
		Kind:  NoGoLoginRejected,
		Codes: []string{"9942"},
	}
	if got := reason.String(); got != "login rejected: 9942" {
		t.Fatalf("String = %q", got)
	}

	reason = NoGoReason{
		Kind:  NoGoOther,
		Other: "BankError",
		Codes: []string{"9010", "9050"},
	}
	if got := reason.String(); got != "other: BankError: 9010, 9050" {
		t.Fatalf("String = %q", got)
	}

	reason = NoGoReason{Kind: NoGoOther, Other: "BankError", Codes: []string{"bank text"}}
	if got := reason.String(); got != "other: BankError" {
		t.Fatalf("String = %q", got)
	}
}

func TestClassifyErrorUsesKindsAndSafeCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "PIN rejected",
			err:  &fints.DialogError{Kind: fints.ErrorKindPinRejected, Codes: []string{"9942"}},
			want: "login rejected: 9942",
		},
		{
			name: "locked",
			err:  &fints.DialogError{Kind: fints.ErrorKindAccountLocked, Codes: []string{"3938"}},
			want: "login rejected: 3938",
		},
		{
			name: "TAN rejected",
			err:  &fints.DialogError{Kind: fints.ErrorKindTanRejected, Codes: []string{"9010"}},
			want: "other: TanRejected: 9010",
		},
		{
			name: "poll limit",
			err:  &fints.DialogError{Kind: fints.ErrorKindPollLimit},
			want: "TAN timeout",
		},
		{
			name: "transport",
			err:  &fints.DialogError{Kind: fints.ErrorKindTransport},
			want: "other: Transport",
		},
		{
			name: "bank message is not printed",
			err:  &fints.DialogError{Kind: fints.ErrorKindBankError, Codes: []string{"9010", "secret bank text"}},
			want: "other: BankError: 9010",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reason := classifyError(test.err)
			if got := reason.String(); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}

	reason := classifyError(errors.New("plain error"))
	if got := reason.String(); got != "other: Unknown" {
		t.Fatalf("plain error reason = %q", got)
	}
}

func TestStatementErrorsBecomeUnavailableExceptLoginAndTimeout(t *testing.T) {
	unavailable := classifyStatementsError(&fints.DialogError{
		Kind:  fints.ErrorKindBankError,
		Codes: []string{"9010"},
	})
	if got := unavailable.String(); got != "statements unavailable: 9010" {
		t.Fatalf("bank error reason = %q", got)
	}

	tanRejected := classifyStatementsError(&fints.DialogError{Kind: fints.ErrorKindTanRejected})
	if got := tanRejected.String(); got != "statements unavailable" {
		t.Fatalf("TAN rejected reason = %q", got)
	}

	login := classifyStatementsError(&fints.DialogError{
		Kind:  fints.ErrorKindPinRejected,
		Codes: []string{"9942"},
	})
	if got := login.String(); got != "login rejected: 9942" {
		t.Fatalf("login reason = %q", got)
	}

	timeout := classifyStatementsError(&fints.DialogError{Kind: fints.ErrorKindPollLimit})
	if got := timeout.String(); got != "TAN timeout" {
		t.Fatalf("timeout reason = %q", got)
	}
}

// No account at all is never a GO.
func TestNoGoWhenNoAccountWasChecked(t *testing.T) {
	decision := Decide(nil, nil)
	if decision.Go {
		t.Fatal("Decide reported GO without any account")
	}
	if got := decision.Reason.String(); got != "other: NoAccounts" {
		t.Fatalf("reason = %q, want other: NoAccounts", got)
	}
	if decision.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", decision.ExitCode())
	}
}
