package fints

import "testing"

func TestWorkflowCanRequest(t *testing.T) {
	const accountNumber = "1234567"
	complete := Account{
		IBAN:          testIBAN,
		BIC:           testBIC,
		AccountNumber: accountNumber,
		BLZ:           testBLZ,
	}

	cases := []struct {
		name       string
		bankParams []string
		account    Account
		want       bool
	}{
		{
			name:       "v6 complete national identifiers",
			bankParams: []string{hisals(6), hikazs(6)},
			account:    complete,
			want:       true,
		},
		{
			name:       "v6 without account number",
			bankParams: []string{hisals(6), hikazs(6)},
			account: Account{
				IBAN: testIBAN,
				BIC:  testBIC,
				BLZ:  testBLZ,
			},
			want: false,
		},
		{
			name:       "v6 without BLZ",
			bankParams: []string{hisals(6), hikazs(6)},
			account: Account{
				IBAN:          testIBAN,
				BIC:           testBIC,
				AccountNumber: accountNumber,
			},
			want: false,
		},
		{
			name:       "v6 national identifiers without IBAN or BIC",
			bankParams: []string{hisals(6), hikazs(6)},
			account: Account{
				AccountNumber: accountNumber,
				BLZ:           testBLZ,
			},
			want: true,
		},
		{
			name:       "v7 without BIC",
			bankParams: []string{hisals(7), hikazs(7)},
			account: Account{
				IBAN:          testIBAN,
				AccountNumber: accountNumber,
				BLZ:           testBLZ,
			},
			want: false,
		},
		{
			name:       "v7 without account number",
			bankParams: []string{hisals(7), hikazs(7)},
			account: Account{
				IBAN: testIBAN,
				BIC:  testBIC,
				BLZ:  testBLZ,
			},
			want: true,
		},
		{
			name: "v6 camt without BIC",
			bankParams: []string{
				hisals(6),
				hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
			},
			account: Account{
				AccountNumber: accountNumber,
				BLZ:           testBLZ,
			},
			want: false,
		},
		{
			name: "v6 camt complete",
			bankParams: []string{
				hisals(6),
				hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
			},
			account: complete,
			want:    true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			bank := newFakeBank(t)
			dialog := newSyncedDialog(t, bank, syncResponse(testCase.bankParams...))
			workflow := NewWorkflow(dialog, nil)

			if got := workflow.CanRequest(testCase.account); got != testCase.want {
				t.Fatalf("CanRequest = %t, want %t", got, testCase.want)
			}
			if len(bank.records) != 0 {
				t.Fatalf("CanRequest sent %d requests, want 0", len(bank.records))
			}
		})
	}
}

func TestWorkflowCanRequestWithoutDialog(t *testing.T) {
	account := Account{
		IBAN:          testIBAN,
		BIC:           testBIC,
		AccountNumber: "1234567",
		BLZ:           testBLZ,
	}

	var nilWorkflow *Workflow
	if nilWorkflow.CanRequest(account) {
		t.Fatal("nil workflow CanRequest = true, want false")
	}
	if NewWorkflow(nil, nil).CanRequest(account) {
		t.Fatal("workflow without a dialog CanRequest = true, want false")
	}
}
