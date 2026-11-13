package fints

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestDialogAndResultsFormattingNeverLeakSecrets(t *testing.T) {
	const (
		sensitiveChallenge = "Bitte TAN fuer Konto DE00999999990000000001 eingeben"
		sensitiveOwner     = "Erika Musterfrau-Geheim"
		sensitiveTouchdown = "touchdown-DE00999999990000000001-Erika"
	)

	dialog := newTestDialog(t, newFakeBank(t))
	dialog.params.Accounts = []Account{{
		IBAN:          testIBAN,
		AccountNumber: "999900004242",
		owner:         sensitiveOwner,
		productName:   sensitiveChallenge,
	}}
	dialog.params.TANMethods = []TanMethod{{SecurityFunction: "942", name: sensitiveOwner}}
	dialog.challenge = TanChallenge{TaskReference: "task-1", challenge: sensitiveChallenge}
	dialog.dialogID = "dialog-secret-identifier"
	dialog.systemID = "system-secret-identifier"

	values := []any{
		dialog,
		*dialog,
		InitResult{Challenge: TanChallenge{challenge: sensitiveChallenge}, Decoupled: true},
		SendResult{
			Kind:      SendNeedTAN,
			Response:  &Response{},
			Challenge: TanChallenge{challenge: sensitiveChallenge},
			Touchdown: sensitiveTouchdown,
		},
		PollResult{Confirmed: true, Response: &Response{}},
		Statements{Kind: StatementCamt},
	}

	errorKinds := []ErrorKind{
		ErrorKindPinRejected,
		ErrorKindAccountLocked,
		ErrorKindBankError,
		ErrorKindTanRejected,
		ErrorKindTransport,
		ErrorKindProtocol,
		ErrorKindCancelled,
		ErrorKindPollLimit,
		ErrorKindTouchdownLimit,
		ErrorKindInvalidState,
	}
	for _, kind := range errorKinds {
		values = append(values, &DialogError{Kind: kind, Codes: []string{"9340"}})
		values = append(values, &DialogError{
			Kind:  kind,
			Codes: []string{"9931"},
			cause: errors.New(sensitiveChallenge),
		})
	}
	values = append(values, newTransportError(errors.New(sensitiveChallenge+" "+sensitiveOwner)))

	for valueIndex, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(format, value)
			for _, forbidden := range []string{
				testPIN, testTAN, sensitiveChallenge, testIBAN,
				sensitiveOwner, sensitiveTouchdown,
				"dialog-secret-identifier", "system-secret-identifier",
			} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("value %d with %s leaked %q: %q", valueIndex, format, forbidden, got)
				}
			}
		}
	}
}

func TestDialogErrorShowsKindAndCodesOnly(t *testing.T) {
	dialogError := &DialogError{Kind: ErrorKindBankError, Codes: []string{"9010", "9050"}}
	if got := dialogError.Error(); got != "fints: bank rejected the request (9010, 9050)" {
		t.Fatalf("Error() = %q", got)
	}

	transportError := newTransportError(errors.New("secret transport text"))
	if got := transportError.Error(); got != "fints: transport failed (*errors.errorString)" {
		t.Fatalf("transport Error() = %q", got)
	}
	if strings.Contains(transportError.Error(), "secret transport text") {
		t.Fatal("transport error contains the transport text")
	}
}

func TestDialogErrorKindNames(t *testing.T) {
	kinds := map[ErrorKind]string{
		ErrorKindPinRejected:    "pin-rejected",
		ErrorKindAccountLocked:  "account-locked",
		ErrorKindBankError:      "bank-error",
		ErrorKindTanRejected:    "tan-rejected",
		ErrorKindTransport:      "transport",
		ErrorKindProtocol:       "protocol",
		ErrorKindCancelled:      "cancelled",
		ErrorKindPollLimit:      "poll-limit",
		ErrorKindTouchdownLimit: "touchdown-limit",
		ErrorKindInvalidState:   "invalid-state",
	}
	for kind, want := range kinds {
		if got := kind.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", kind, got, want)
		}
	}
	if got := ErrorKind(200).String(); got != "unknown" {
		t.Fatalf("unknown kind = %q", got)
	}
}

type panickingTransport struct {
	request []byte
}

func (p *panickingTransport) Exchange(_ context.Context, request []byte) ([]byte, error) {
	p.request = request
	panic("transport panic")
}

func TestRequestIsWipedWhenTransportPanics(t *testing.T) {
	transport := &panickingTransport{}
	dialog, err := NewDialog(DialogConfig{
		BLZ:            testBLZ,
		UserID:         testUserID,
		CustomerID:     testCustomerID,
		ProductID:      testProductID,
		ProductVersion: "1.0",
		PIN:            secret.NewText([]byte(testPIN)),
		Transport:      transport,
		Source:         fixedSource(),
	})
	if err != nil {
		t.Fatalf("NewDialog: %v", err)
	}

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("Sync did not propagate the transport panic")
			}
		}()
		_ = dialog.Sync(context.Background())
	}()

	if len(transport.request) == 0 {
		t.Fatal("the transport did not receive the request")
	}
	for index, value := range transport.request {
		if value != 0 {
			t.Fatalf("request byte %d = %d after the transport panic, want 0", index, value)
		}
	}
}
