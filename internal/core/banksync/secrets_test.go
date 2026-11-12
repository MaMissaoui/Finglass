package banksync

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestSecretsNeverFormatted(t *testing.T) {
	const pin = "987654"
	const tan = "123456"

	values := []any{
		NewPinEntry(secret.NewText([]byte(pin)), true),
		TanChallenge{
			Message:     "Bitte bestätigen",
			Image:       []byte("image"),
			FlickerCode: stringPointer(tan),
		},
		NewMemoryCredentialStore().FailingReads(),
	}
	store := NewMemoryCredentialStore()
	if err := store.Write("key", []byte(pin)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	values = append(values, store)

	bankingErrors := []BankingError{
		NewBankingError(CodeSyncFailed, "Sync failed"),
		NewBankingError(CodeCancelled, "Cancelled by user"),
	}
	gatewayErrors := []GatewayError{
		NewGatewayNetworkError(),
		NewGatewayHTTPStatusError(403),
		NewGatewayBankError([]string{"997"}),
		NewGatewayPinRejectedError(),
		NewGatewayCancelledError(),
		NewGatewayOtherError("TAN " + tan),
	}
	for _, gatewayError := range gatewayErrors {
		values = append(values, gatewayError)
	}
	for _, bankingError := range bankingErrors {
		values = append(values, bankingError)
	}

	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			got := fmt.Sprintf(format, value)
			for _, known := range []string{pin, tan} {
				if strings.Contains(got, known) {
					t.Fatalf("%s of %T leaked %q: %q", format, value, known, got)
				}
			}
		}
	}
}
