package connectiontest

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/fintsprobe"
)

func TestSyncUnlockActionCoversEveryOutcomeKind(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		want    SyncUnlockAction
	}{
		{name: "GO", outcome: Outcome{Go: true}, want: SyncUnlockSet},
		{name: "GO through the strong sync", outcome: Outcome{Go: true, StrongSync: true}, want: SyncUnlockUnchanged},
		{name: "NO-GO after the strong sync", outcome: Outcome{Kind: fintsprobe.NoGoLoginRejected, StrongSync: true}, want: SyncUnlockClear},
		{name: "login rejected", outcome: Outcome{Kind: fintsprobe.NoGoLoginRejected}, want: SyncUnlockClear},
		{name: "TAN method unsupported", outcome: Outcome{Kind: fintsprobe.NoGoTanMethodUnsupported}, want: SyncUnlockClear},
		{name: "TAN timeout", outcome: Outcome{Kind: fintsprobe.NoGoTanTimeout}, want: SyncUnlockUnchanged},
		{name: "statements unavailable", outcome: Outcome{Kind: fintsprobe.NoGoStatementsUnavailable}, want: SyncUnlockClear},
		{name: "pin rejected", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "PinRejected"}, want: SyncUnlockClear},
		{name: "account locked", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "AccountLocked"}, want: SyncUnlockClear},
		{name: "TAN rejected", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "TanRejected"}, want: SyncUnlockClear},
		{name: "bank error", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "BankError"}, want: SyncUnlockClear},
		{name: "protocol error", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "Protocol"}, want: SyncUnlockClear},
		{name: "no accounts", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "NoAccounts"}, want: SyncUnlockClear},
		{name: "balance absent", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "BalanceAbsent"}, want: SyncUnlockClear},
		{name: "transport", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "Transport"}, want: SyncUnlockUnchanged},
		{name: "prompt failed", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "PromptFailed"}, want: SyncUnlockUnchanged},
		{name: "invalid state", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "InvalidState"}, want: SyncUnlockUnchanged},
		{name: "unknown", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "Unknown"}, want: SyncUnlockUnchanged},
		{name: "cancelled reason", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "Cancelled"}, want: SyncUnlockUnchanged},
		{name: "cancelled flag", outcome: Outcome{Cancelled: true}, want: SyncUnlockUnchanged},
		{name: "cancelled other", outcome: Outcome{Other: OtherCancelled, Cancelled: true}, want: SyncUnlockUnchanged},
		{name: "invalid login", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: OtherInvalidLogin}, want: SyncUnlockUnchanged},
		{name: "product id missing", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: OtherProductIDMissing}, want: SyncUnlockUnchanged},
		{name: "product id invalid", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: OtherProductIDInvalid}, want: SyncUnlockUnchanged},
		{name: "poll limit", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "PollLimit"}, want: SyncUnlockUnchanged},
		{name: "touchdown limit", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: "TouchdownLimit"}, want: SyncUnlockUnchanged},
		{name: "transport build", outcome: Outcome{Kind: fintsprobe.NoGoOther, Other: OtherTransport}, want: SyncUnlockUnchanged},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SyncUnlockActionFor(test.outcome); got != test.want {
				t.Fatalf("SyncUnlockActionFor(%+v) = %v, want %v", test.outcome, got, test.want)
			}
		})
	}
}

// TestEveryProducedOtherReasonHasAnUnlockDecision scans the producers' sources
// for NoGoOther reason names, so a new reason cannot fall through to the
// default without a decision in otherReasonActions.
func TestEveryProducedOtherReasonHasAnUnlockDecision(t *testing.T) {
	literal := regexp.MustCompile(`Other: *"([A-Za-z]+)"`)
	returned := regexp.MustCompile(`(?m)^\s*return "([A-Z][A-Za-z]+)"$`)
	constant := regexp.MustCompile(`(?m)^\s*Other[A-Za-z]+ *= *"([A-Za-z]+)"`)
	found := map[string]bool{}
	for _, dir := range []string{"../../core/fintsprobe", "."} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if regexp.MustCompile(`_test\.go$`).MatchString(file) {
				continue
			}
			//nolint:gosec // the path is a Go source file globbed from this repository's own packages.
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, pattern := range []*regexp.Regexp{literal, constant} {
				for _, match := range pattern.FindAllSubmatch(source, -1) {
					found[string(match[1])] = true
				}
			}
			if filepath.Base(file) == "summary.go" {
				for _, match := range returned.FindAllSubmatch(source, -1) {
					found[string(match[1])] = true
				}
			}
		}
	}
	if len(found) < 10 {
		t.Fatalf("found only %d reasons; the scan no longer matches the sources", len(found))
	}
	for reason := range found {
		if _, ok := otherReasonActions[reason]; !ok {
			t.Errorf("NoGoOther reason %q has no sync-unlock decision", reason)
		}
	}
}
