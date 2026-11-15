package shell

import (
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/connectiontest"
	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func TestConnectionTestReasonNamesEveryOtherReason(t *testing.T) {
	want := map[string]i18n.Key{
		"PinRejected":                        i18n.TestConnectionLoginRejected,
		"AccountLocked":                      i18n.TestConnectionAccountLocked,
		"TanRejected":                        i18n.TestConnectionTanRejected,
		"BankError":                          i18n.TestConnectionBankError,
		"Protocol":                           i18n.TestConnectionProtocol,
		"NoAccounts":                         i18n.TestConnectionNoAccounts,
		"BalanceAbsent":                      i18n.TestConnectionBalanceAbsent,
		"Transport":                          i18n.TestConnectionTransport,
		"Cancelled":                          i18n.TestConnectionCancelled,
		"PollLimit":                          i18n.TestConnectionApprovalTimedOut,
		"TouchdownLimit":                     i18n.TestConnectionApprovalTimedOut,
		"InvalidState":                       i18n.TestConnectionUnexpected,
		"PromptFailed":                       i18n.TestConnectionUnexpected,
		"Unknown":                            i18n.TestConnectionUnexpected,
		connectiontest.OtherInvalidLogin:     i18n.TestConnectionInvalidLogin,
		connectiontest.OtherProductIDMissing: i18n.TestConnectionProductID,
		connectiontest.OtherProductIDInvalid: i18n.TestConnectionProductID,
	}
	fallback := map[string]bool{
		"InvalidState": true,
		"PromptFailed": true,
		"Unknown":      true,
	}

	names := connectiontest.OtherReasonNames()
	if len(names) != len(want) {
		t.Fatalf("OtherReasonNames has %d names, want %d", len(names), len(want))
	}
	for _, name := range names {
		key, ok := want[name]
		if !ok {
			t.Fatalf("OtherReasonNames has an unexpected reason %q", name)
		}
		for _, language := range i18n.Languages {
			got := connectionTestReason(language, connectiontest.Outcome{
				Kind:  fintsprobe.NoGoOther,
				Other: name,
			})
			if got == "" {
				t.Errorf("%s %s: reason is empty", language.Code(), name)
			}
			if wantText := i18n.Text(language, key); got != wantText {
				t.Errorf("%s %s: reason = %q, want %q", language.Code(), name, got, wantText)
			}
			if !fallback[name] && got == i18n.Text(language, i18n.TestConnectionUnexpected) {
				t.Errorf("%s %s: reason fell back to the unexpected-test text", language.Code(), name)
			}
			if strings.Contains(got, name) {
				t.Errorf("%s %s: reason contains the internal kind name: %q", language.Code(), name, got)
			}
		}
	}
}

func TestConnectionTestVerdictDistinguishesNoBankVerdict(t *testing.T) {
	goOutcome := connectiontest.Outcome{Go: true}
	if got := connectionTestVerdictFor(goOutcome); got != "go" {
		t.Fatalf("GO verdict = %q, want go", got)
	}
	strongSync := connectiontest.Outcome{Go: true, StrongSync: true}
	if got := connectionTestVerdictFor(strongSync); got != "go" {
		t.Fatalf("strong-sync GO verdict = %q, want go", got)
	}
	for _, name := range []string{
		"Transport",
		"PollLimit",
		"TouchdownLimit",
		"InvalidState",
		"PromptFailed",
		"Unknown",
		connectiontest.OtherInvalidLogin,
		connectiontest.OtherProductIDMissing,
		connectiontest.OtherProductIDInvalid,
		connectiontest.OtherCancelled,
	} {
		outcome := connectiontest.Outcome{Kind: fintsprobe.NoGoOther, Other: name}
		if got := connectionTestVerdictFor(outcome); got != "none" {
			t.Errorf("%s verdict = %q, want none", name, got)
		}
	}
	cancelled := connectiontest.Outcome{Cancelled: true}
	if got := connectionTestVerdictFor(cancelled); got != "none" {
		t.Fatalf("cancel verdict = %q, want none", got)
	}
	if got := connectionTestVerdictFor(connectiontest.Outcome{Kind: fintsprobe.NoGoTanTimeout}); got != "none" {
		t.Fatalf("TAN timeout verdict = %q, want none", got)
	}
	for _, name := range []string{"PinRejected", "AccountLocked", "TanRejected", "BankError", "Protocol", "NoAccounts", "BalanceAbsent"} {
		outcome := connectiontest.Outcome{Kind: fintsprobe.NoGoOther, Other: name}
		if got := connectionTestVerdictFor(outcome); got != "nogo" {
			t.Errorf("%s verdict = %q, want nogo", name, got)
		}
	}
	if got := connectionTestVerdictFor(connectiontest.Outcome{Kind: fintsprobe.NoGoLoginRejected}); got != "nogo" {
		t.Fatalf("login-rejected verdict = %q, want nogo", got)
	}
}

func TestConnectionTestReasonSeparatesBankErrorFromMissingRegistration(t *testing.T) {
	for _, language := range i18n.Languages {
		plain := connectionTestReason(language, connectiontest.Outcome{
			Kind:  fintsprobe.NoGoOther,
			Other: "BankError",
			Codes: []string{"9942"},
		})
		if want := i18n.Text(language, i18n.TestConnectionBankError); plain != want {
			t.Errorf("%s plain BankError = %q, want %q", language.Code(), plain, want)
		}
		missing := connectionTestReason(language, connectiontest.Outcome{
			Kind:  fintsprobe.NoGoOther,
			Other: "BankError",
			Codes: []string{"3079", "9078", "9800"},
		})
		if want := i18n.Text(language, i18n.TestConnectionRegistrationMissing); missing != want {
			t.Errorf("%s BankError 9078 = %q, want %q", language.Code(), missing, want)
		}
		for _, reason := range []string{plain, missing} {
			if strings.Contains(reason, "BankError") {
				t.Errorf("%s: reason contains the internal kind name: %q", language.Code(), reason)
			}
		}
	}
}
