package fints

import (
	"fmt"
	"strings"
	"testing"
)

func TestTypedResponsesFormattingNeverLeaksBankData(t *testing.T) {
	const (
		sensitiveIBAN      = "DE00999999990000000001"
		sensitiveAccount   = "999900004242"
		sensitiveOwner     = "Erika Musterfrau-Geheim"
		sensitiveChallenge = "Bitte TAN für Konto DE00999999990000000001 eingeben"
	)

	forbidden := []string{sensitiveIBAN, sensitiveAccount, sensitiveOwner, sensitiveChallenge}

	account := Account{
		IBAN:          sensitiveIBAN,
		BIC:           testBIC,
		AccountNumber: sensitiveAccount,
		SubAccount:    "0001",
		BLZ:           testBLZ,
		Currency:      "EUR",
		AccountType:   "G",
		owner:         sensitiveOwner,
		productName:   sensitiveChallenge,
	}
	method := TanMethod{
		SecurityFunction:          "940",
		TanProcess:                TanProcessTwoStep,
		TechnicalID:               "TECH-ID",
		DKID:                      "DKID",
		DKVersion:                 "1.0",
		MediumName:                MediumRequired,
		DecoupledMaxPolls:         60,
		WaitBeforeFirstPoll:       5,
		WaitBeforeNextPoll:        2,
		ManualConfirmationAllowed: true,
		AutomatedPollingAllowed:   true,
		HKTANVersion:              7,
		name:                      sensitiveOwner,
		decoupledMaxPollsPresent:  true,
	}
	values := []any{
		ReturnCode{Code: "9942", RefElement: sensitiveIBAN, Params: []string{sensitiveAccount}, text: sensitiveOwner},
		ReturnCodeKindAccountLocked,
		method,
		TanProcessTwoStep,
		BusinessHKSAL,
		account,
		TanChallenge{TaskReference: sensitiveAccount, challenge: sensitiveChallenge, hhduc: []byte(sensitiveIBAN)},
		TanMedia{name: sensitiveOwner},
		Balance{Currency: "EUR"},
		MT940Statement{Booked: []byte(sensitiveIBAN + sensitiveOwner), Pending: []byte(sensitiveChallenge)},
		CamtStatement{
			Booked:  [][]byte{[]byte(sensitiveIBAN)},
			Pending: [][]byte{[]byte(sensitiveChallenge)},
		},
	}

	for index, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(format, value)
			for _, secret := range forbidden {
				if strings.Contains(got, secret) {
					t.Fatalf("value %d with %s leaked %q: %q", index, format, secret, got)
				}
			}
		}
	}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, method)
		for _, visible := range []string{
			"940", "TECH-ID", "DKID", "1.0", "required",
			"maxPolls:60", "waitFirst:5", "waitNext:2",
			"manual:true", "automated:true", "isDecoupled:true",
		} {
			if !strings.Contains(got, visible) {
				t.Fatalf("%s did not include %q: %q", format, visible, got)
			}
		}
		if strings.Contains(got, sensitiveOwner) {
			t.Fatalf("%s leaked the TAN method name: %q", format, got)
		}
	}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, account)
		if !strings.Contains(got, "…0001") {
			t.Fatalf("%s did not mask the IBAN to its last four characters: %q", format, got)
		}
		if !strings.Contains(got, "…4242") {
			t.Fatalf("%s did not mask the account number to its last four characters: %q", format, got)
		}
	}

	errors := []error{
		&ParseError{Type: "HITAN", Number: 2, Offset: 3, Reason: reasonUnterminated},
		errInvalidDate,
		errEmptyIBAN,
		errEmptyBIC,
		errUnknownTanReference,
	}
	for index, err := range errors {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(format, err)
			for _, secret := range forbidden {
				if strings.Contains(got, secret) {
					t.Fatalf("error %d with %s leaked %q: %q", index, format, secret, got)
				}
			}
		}
	}
}
