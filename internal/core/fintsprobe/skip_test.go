package fintsprobe

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

func TestSkipAccount(t *testing.T) {
	const iban = "DE00999999990000000001"
	const bic = "TESTDEFFXXX"
	cases := []struct {
		name    string
		account fints.Account
		skip    bool
	}{
		{"usable and unrestricted", fints.Account{IBAN: iban, BIC: bic}, false},
		{"no IBAN", fints.Account{BIC: bic}, true},
		{"no BIC", fints.Account{IBAN: iban}, true},
		{"balance and MT940 allowed", fints.Account{IBAN: iban, BIC: bic,
			AllowedBusinessTransactions: []string{"HKSAL", "HKKAZ"}}, false},
		{"balance and camt allowed", fints.Account{IBAN: iban, BIC: bic,
			AllowedBusinessTransactions: []string{"HKCAZ", "HKSAL"}}, false},
		{"no balance allowed", fints.Account{IBAN: iban, BIC: bic,
			AllowedBusinessTransactions: []string{"HKKAZ"}}, true},
		{"no statements allowed", fints.Account{IBAN: iban, BIC: bic,
			AllowedBusinessTransactions: []string{"HKSAL"}}, true},
	}
	for _, testCase := range cases {
		if got := skipAccount(testCase.account); got != testCase.skip {
			t.Errorf("%s: skipAccount = %t, want %t", testCase.name, got, testCase.skip)
		}
	}
}
