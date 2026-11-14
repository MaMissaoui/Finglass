package bind

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

const (
	promptedProductID = "PROMPTEDPRODUCT1234567890"
	traceLeakMarker   = "TRACE-MUST-NEVER-APPEAR"
	bankTextMarker    = "BANK-TEXT-MUST-NEVER-APPEAR"
	userIDMarker      = "USER-ID-MUST-NEVER-APPEAR"
)

func TestConnectionTestDTOsCarryNoTraceProductIDOrBankText(t *testing.T) {
	state := bankingcore.BankingState{
		Language: i18n.En,
		ConnectionTest: &bankingcore.ConnectionTestResult{
			BankID:  7,
			Go:      false,
			Reason:  i18n.Format(i18n.En, i18n.TestConnectionOther, i18n.Str("Transport")),
			Codes:   []string{"9942"},
			Probed:  1,
			Skipped: 1,
		},
		LastTrace: &bankingcore.LastTrace{Kind: "test", BankID: 7, HasTrace: true},
		Banks: []bankingcore.BankItem{{
			ID:                7,
			BankName:          bankTextMarker,
			UserID:            userIDMarker,
			CanTestConnection: true,
		}},
	}
	dto := bankingDTO(state, i18n.En)
	productPrompt := promptDTO(&bankingcore.BankingPrompt{ID: 3, Kind: bankingcore.ProductID}, i18n.En, true)

	rendered := []string{
		fmt.Sprintf("%v", dto),
		fmt.Sprintf("%+v", dto),
		fmt.Sprintf("%#v", dto),
		fmt.Sprintf("%v", productPrompt),
		fmt.Sprintf("%+v", productPrompt),
		fmt.Sprintf("%#v", productPrompt),
	}
	bankingJSON, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal banking DTO: %v", err)
	}
	promptJSON, err := json.Marshal(productPrompt)
	if err != nil {
		t.Fatalf("marshal prompt DTO: %v", err)
	}
	rendered = append(rendered, string(bankingJSON), string(promptJSON))

	for _, value := range rendered {
		for _, marker := range []string{traceLeakMarker, promptedProductID, bankTextMarker, userIDMarker} {
			if strings.Contains(value, marker) {
				t.Fatalf("DTO leaked %q: %s", marker, value)
			}
		}
	}
	if dto.ConnectionTest == nil || dto.ConnectionTest.BankID != 7 ||
		len(dto.ConnectionTest.Codes) != 1 {
		t.Fatalf("connection test block = %+v", dto.ConnectionTest)
	}
	if dto.LastTrace == nil || dto.LastTrace.Kind != "test" ||
		dto.LastTrace.BankID != 7 || !dto.LastTrace.HasTrace {
		t.Fatalf("last trace block = %+v", dto.LastTrace)
	}
	if len(dto.Banks) != 1 || !dto.Banks[0].CanTestConnection {
		t.Fatalf("bank DTO = %+v", dto.Banks)
	}
}

func TestAnswerProductIDWipesTheAnsweredBytes(t *testing.T) {
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, nil)
	var captured []byte
	fixture.banking.answerSeam = func(raw []byte) { captured = raw }

	result := make(chan string, 1)
	go func() {
		text := fixture.session.Bridge().AskProductID()
		if text == nil {
			result <- ""
			return
		}
		value := ""
		_ = text.Use(func(bytes []byte) error {
			value = string(bytes)
			return nil
		})
		text.Wipe()
		result <- value
	}()

	prompt := fixture.waitPrompt(t, "product-id")
	if err := fixture.banking.AnswerProductID(prompt.ID, promptedProductID); err != nil {
		t.Fatalf("AnswerProductID: %v", err)
	}
	if got := <-result; got != promptedProductID {
		t.Fatalf("product id = %q, want %q", got, promptedProductID)
	}
	for index, value := range captured {
		if value != 0 {
			t.Fatalf("answer byte %d = %d after the call, want 0", index, value)
		}
	}
}
