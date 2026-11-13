package fintsprobe

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

func workflowForVersions(t *testing.T, versions map[fints.BusinessSegment]uint16) *fints.Workflow {
	t.Helper()
	params := fints.NewBankParams()
	for business, version := range versions {
		params.SegmentVersions[business] = version
	}
	dialog, err := fints.NewDialog(fints.DialogConfig{
		BLZ:            testBLZ,
		UserID:         testUser,
		CustomerID:     testUser,
		ProductID:      testProductID,
		ProductVersion: "0.4",
		PIN:            secret.NewText([]byte(testPIN)),
		Transport:      newFakeTransport(t),
		Params:         params,
	})
	if err != nil {
		t.Fatalf("NewDialog: %v", err)
	}
	return fints.NewWorkflow(dialog, nil)
}

func TestSkipAccount(t *testing.T) {
	const iban = "DE00999999990000000001"
	const bic = "TESTDEFFXXX"
	const accountNumber = "00000001"

	v7 := workflowForVersions(t, map[fints.BusinessSegment]uint16{
		fints.BusinessHKSAL: 7,
		fints.BusinessHKKAZ: 7,
	})
	v6 := workflowForVersions(t, map[fints.BusinessSegment]uint16{
		fints.BusinessHKSAL: 6,
		fints.BusinessHKKAZ: 6,
	})

	t.Run("v7", func(t *testing.T) {
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
			if got := skipAccount(v7, testCase.account); got != testCase.skip {
				t.Errorf("%s: skipAccount = %t, want %t", testCase.name, got, testCase.skip)
			}
		}
	})

	t.Run("v6", func(t *testing.T) {
		cases := []struct {
			name    string
			account fints.Account
			skip    bool
		}{
			{"usable with national identifiers", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ}, false},
			{"no account number", fints.Account{IBAN: iban, BIC: bic,
				BLZ: testBLZ}, true},
			{"no BLZ", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber}, true},
			{"no BIC but national identifiers", fints.Account{IBAN: iban,
				AccountNumber: accountNumber, BLZ: testBLZ}, false},
			{"no IBAN with national identifiers", fints.Account{BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ}, true},
			{"balance and MT940 allowed", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ,
				AllowedBusinessTransactions: []string{"HKSAL", "HKKAZ"}}, false},
			{"balance and camt allowed", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ,
				AllowedBusinessTransactions: []string{"HKCAZ", "HKSAL"}}, false},
			{"no balance allowed", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ,
				AllowedBusinessTransactions: []string{"HKKAZ"}}, true},
			{"no statements allowed", fints.Account{IBAN: iban, BIC: bic,
				AccountNumber: accountNumber, BLZ: testBLZ,
				AllowedBusinessTransactions: []string{"HKSAL"}}, true},
			{"no account number with allowed transactions", fints.Account{IBAN: iban, BIC: bic,
				BLZ:                         testBLZ,
				AllowedBusinessTransactions: []string{"HKSAL", "HKKAZ"}}, true},
		}
		for _, testCase := range cases {
			if got := skipAccount(v6, testCase.account); got != testCase.skip {
				t.Errorf("%s: skipAccount = %t, want %t", testCase.name, got, testCase.skip)
			}
		}
	})
}
