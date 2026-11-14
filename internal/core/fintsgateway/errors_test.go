package fintsgateway

import (
	"context"
	"errors"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

func TestEveryDialogErrorKindMaps(t *testing.T) {
	tests := []struct {
		name      string
		dialog    *fints.DialogError
		kind      banksync.GatewayErrorKind
		codes     string
		status    uint16
		otherText string
	}{
		{
			name:   "pin rejected",
			dialog: &fints.DialogError{Kind: fints.ErrorKindPinRejected, Codes: []string{"9942"}},
			kind:   banksync.GatewayErrorPinRejected,
		},
		{
			name:   "init pin code outside the pin set",
			dialog: &fints.DialogError{Kind: fints.ErrorKindPinRejected, Codes: []string{"9010"}},
			kind:   banksync.GatewayErrorBank,
			codes:  "9010",
		},
		{
			name:   "account locked",
			dialog: &fints.DialogError{Kind: fints.ErrorKindAccountLocked, Codes: []string{"3938"}},
			kind:   banksync.GatewayErrorBank,
			codes:  "3938",
		},
		{
			name:   "bank error",
			dialog: &fints.DialogError{Kind: fints.ErrorKindBankError, Codes: []string{"9010", "9011"}},
			kind:   banksync.GatewayErrorBank,
			codes:  "9010,9011",
		},
		{
			name:   "tan rejected",
			dialog: &fints.DialogError{Kind: fints.ErrorKindTanRejected},
			kind:   banksync.GatewayErrorBank,
		},
		{
			name:   "transport with status",
			dialog: &fints.DialogError{Kind: fints.ErrorKindTransport, HTTPStatus: 502},
			kind:   banksync.GatewayErrorHTTPStatus,
			status: 502,
		},
		{
			name:   "transport without status",
			dialog: &fints.DialogError{Kind: fints.ErrorKindTransport},
			kind:   banksync.GatewayErrorNetwork,
		},
		{
			name:      "protocol",
			dialog:    &fints.DialogError{Kind: fints.ErrorKindProtocol},
			kind:      banksync.GatewayErrorOther,
			otherText: "protocol",
		},
		{
			name:   "cancelled",
			dialog: &fints.DialogError{Kind: fints.ErrorKindCancelled},
			kind:   banksync.GatewayErrorCancelled,
		},
		{
			name:      "poll limit",
			dialog:    &fints.DialogError{Kind: fints.ErrorKindPollLimit},
			kind:      banksync.GatewayErrorOther,
			otherText: "poll-limit",
		},
		{
			name:      "touchdown limit",
			dialog:    &fints.DialogError{Kind: fints.ErrorKindTouchdownLimit},
			kind:      banksync.GatewayErrorOther,
			otherText: "touchdown-limit",
		},
		{
			name:      "invalid state",
			dialog:    &fints.DialogError{Kind: fints.ErrorKindInvalidState},
			kind:      banksync.GatewayErrorOther,
			otherText: "invalid-state",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapError(test.dialog)
			if mapped.Kind() != test.kind {
				t.Fatalf("kind = %v, want %v", mapped.Kind(), test.kind)
			}
			if got := joinCodes(mapped.Codes()); got != test.codes {
				t.Fatalf("codes = %q, want %q", got, test.codes)
			}
			if test.status != 0 {
				status, ok := mapped.HTTPStatus()
				if !ok || status != test.status {
					t.Fatalf("status = %d, %t, want %d", status, ok, test.status)
				}
			}
			if test.otherText != "" {
				if got := mapped.Describe(i18n.En, nil); got != test.otherText {
					t.Fatalf("text = %q, want %q", got, test.otherText)
				}
			}
		})
	}
}

func TestCancelledContextMapsToCancelled(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if mapped := mapError(err); mapped.Kind() != banksync.GatewayErrorCancelled {
			t.Fatalf("mapError(%v) = %v", err, mapped.Kind())
		}
	}
}

func TestUnknownErrorMapsToOther(t *testing.T) {
	mapped := mapError(errors.New("unexpected"))
	if mapped.Kind() != banksync.GatewayErrorOther {
		t.Fatalf("kind = %v, want other", mapped.Kind())
	}
	if got := mapped.Describe(i18n.En, nil); got != kindDialogue {
		t.Fatalf("text = %q, want %q", got, kindDialogue)
	}
}

func joinCodes(codes []string) string {
	joined := ""
	for index, code := range codes {
		if index > 0 {
			joined += ","
		}
		joined += code
	}
	return joined
}
