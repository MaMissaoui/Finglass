package banksync

import (
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func TestASocketTextIsANetworkFailure(t *testing.T) {
	if !NewGatewayNetworkError().IsNetworkFailure() {
		t.Fatal("network error is not a network failure")
	}
	if !NewGatewayOtherError("java.net.ConnectException: Connection refused").IsNetworkFailure() {
		t.Fatal("ConnectException is not a network failure")
	}
	if NewGatewayOtherError("997 Falscher PIN").IsNetworkFailure() {
		t.Fatal("PIN text is a network failure")
	}
}

func TestABase64FailureNamesTheAnswerThatIsNotFinTS(t *testing.T) {
	message := NewGatewayOtherError("Illegal base64 character 3c").Describe(i18n.En, nil)
	if !strings.Contains(message, "Illegal base64 character 3c") {
		t.Fatalf("message = %q", message)
	}
}

func TestAnIllegalArgumentWithoutBase64IsNotCalledANonFinTSAnswer(t *testing.T) {
	message := NewGatewayOtherError("invalid value for BTG.value").Describe(i18n.En, nil)
	if strings.Contains(message, "isn't FinTS") {
		t.Fatalf("message = %q", message)
	}
	if message != "invalid value for BTG.value" {
		t.Fatalf("message = %q, want %q", message, "invalid value for BTG.value")
	}
}

func TestAnHTTPStatusWinsOverTheNetworkBranch(t *testing.T) {
	gatewayError := NewGatewayHTTPStatusError(403)
	status, ok := gatewayError.HTTPStatus()
	if !ok || status != 403 {
		t.Fatalf("HTTPStatus() = %d, %t, want 403, true", status, ok)
	}
	message := gatewayError.Describe(i18n.En, nil)
	if !strings.Contains(message, "HTTP 403") {
		t.Fatalf("message = %q", message)
	}
	if strings.Contains(message, "Could not reach") {
		t.Fatalf("message = %q", message)
	}
}

func TestABankMessageWinsOverTheGenericText(t *testing.T) {
	gatewayError := NewGatewayBankError([]string{"997"})
	message := gatewayError.Describe(i18n.En, []string{"997 Falscher PIN"})
	if message != "997 Falscher PIN" {
		t.Fatalf("message = %q, want %q", message, "997 Falscher PIN")
	}
}

func TestABankMessageIsRedacted(t *testing.T) {
	gatewayError := NewGatewayBankError([]string{"3016"})
	message := gatewayError.Describe(i18n.En, []string{"Konto DE89370400440532013000 unbekannt"})
	if strings.Contains(message, "DE89370400440532013000") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "…3000") {
		t.Fatalf("message = %q", message)
	}
}

func TestAGenericFallbackKeepsTheFirstLineOnly(t *testing.T) {
	message := NewGatewayOtherError("Dialog broke\nat org.example.Frame").Describe(i18n.En, nil)
	if message != "Dialog broke" {
		t.Fatalf("message = %q, want %q", message, "Dialog broke")
	}
}

func TestEmptyTextFallsBackToTheDialogFailedMessage(t *testing.T) {
	message := NewGatewayOtherError("\n").Describe(i18n.En, nil)
	if message != i18n.Text(i18n.En, i18n.ErrorDialogFailed) {
		t.Fatalf("message = %q, want %q", message, i18n.Text(i18n.En, i18n.ErrorDialogFailed))
	}
}

func TestANetworkFailureKeepsTheUnreachableMessage(t *testing.T) {
	message := NewGatewayNetworkError().NetworkMessage(i18n.En)
	if message != i18n.Text(i18n.En, i18n.ErrorBankUnreachable) {
		t.Fatalf("message = %q, want %q", message, i18n.Text(i18n.En, i18n.ErrorBankUnreachable))
	}
	detailed := NewGatewayOtherError("Connection reset").NetworkMessage(i18n.En)
	if !strings.HasSuffix(detailed, "(Connection reset)") {
		t.Fatalf("message = %q", detailed)
	}
}

func TestACancelledDialogUsesTheCancelledText(t *testing.T) {
	if got := NewGatewayCancelledError().Describe(i18n.En, nil); got != i18n.Text(i18n.En, i18n.ErrorCancelledByUser) {
		t.Fatalf("message = %q, want %q", got, i18n.Text(i18n.En, i18n.ErrorCancelledByUser))
	}
}

func TestAPinRejectionUsesThePinText(t *testing.T) {
	if got := NewGatewayPinRejectedError().Describe(i18n.En, nil); got != i18n.Text(i18n.En, i18n.ErrorBankRejectedPin) {
		t.Fatalf("message = %q, want %q", got, i18n.Text(i18n.En, i18n.ErrorBankRejectedPin))
	}
}

func TestABankingErrorKeepsOnlyMaskedBankText(t *testing.T) {
	bankingError := NewBankingError(CodeFintsError, "Konto DE89370400440532013000 unbekannt")
	if strings.Contains(bankingError.Message, "DE89370400440532013000") {
		t.Fatalf("message = %q", bankingError.Message)
	}
	if !strings.Contains(bankingError.Message, "…3000") {
		t.Fatalf("message = %q", bankingError.Message)
	}
}
