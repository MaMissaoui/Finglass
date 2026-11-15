package bind

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	bankingcore "github.com/MaMissaoui/Finglass/internal/app/banking"
	"github.com/MaMissaoui/Finglass/internal/app/connectiontest"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
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
			Reason:  i18n.Text(i18n.En, i18n.TestConnectionTransport),
			Kind:    bankingcore.ConnectionTestKindOther,
			Other:   connectiontest.OtherTransport,
			Codes:   []string{"9942"},
			Probed:  1,
			Skipped: 1,
		},
		Traces: []bankingcore.LastTrace{{Kind: "test", BankID: 7, HasTrace: true}},
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
	if len(dto.Traces) != 1 || dto.Traces[0].Kind != "test" ||
		dto.Traces[0].BankID != 7 || !dto.Traces[0].HasTrace {
		t.Fatalf("trace blocks = %+v", dto.Traces)
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

func TestConnectionTestDTOMapsBankMessages(t *testing.T) {
	const bankText = "Bank message with an invented account DE00 0000 0000 0000 0000 01"
	state := bankingcore.BankingState{
		Language: i18n.En,
		ConnectionTest: &bankingcore.ConnectionTestResult{
			BankID: 7,
			Codes:  []string{"9210"},
			Messages: []bankingcore.BankMessage{
				{Code: "9050", Segment: "", Text: bankText},
				{Code: "9210", Segment: "HKTAN", Text: bankText},
			},
		},
	}

	dto := bankingDTO(state, i18n.En)
	if dto.ConnectionTest == nil || len(dto.ConnectionTest.Messages) != 2 {
		t.Fatalf("connection test messages = %+v", dto.ConnectionTest)
	}
	if dto.ConnectionTest.Messages[0].Code != "9050" ||
		dto.ConnectionTest.Messages[0].Segment != "" ||
		dto.ConnectionTest.Messages[0].Text != bankText {
		t.Fatalf("first message = %+v", dto.ConnectionTest.Messages[0])
	}
	if dto.ConnectionTest.Messages[1].Code != "9210" ||
		dto.ConnectionTest.Messages[1].Segment != "HKTAN" ||
		dto.ConnectionTest.Messages[1].Text != bankText {
		t.Fatalf("second message = %+v", dto.ConnectionTest.Messages[1])
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal banking DTO: %v", err)
	}
	if !strings.Contains(string(data), `"messages":[{"code":"9050","segment":"","text":"`+bankText+`"}`) {
		t.Fatalf("JSON messages are wrong: %s", data)
	}
	for _, rendered := range []string{
		fmt.Sprintf("%v", dto),
		fmt.Sprintf("%+v", dto),
		fmt.Sprintf("%#v", dto),
		fmt.Sprintf("%s", dto.ConnectionTest.Messages[0]),
		fmt.Sprintf("%v %+v %#v", dto.ConnectionTest.Messages[0], dto.ConnectionTest.Messages[0], dto.ConnectionTest.Messages[0]),
	} {
		if strings.Contains(rendered, bankText) {
			t.Fatalf("formatting leaked the bank text: %s", rendered)
		}
	}
}

func TestConnectionTestDTOSerializesAnEmptyMessageList(t *testing.T) {
	state := bankingcore.BankingState{
		Language:       i18n.En,
		ConnectionTest: &bankingcore.ConnectionTestResult{BankID: 7, Codes: []string{}},
	}
	dto := bankingDTO(state, i18n.En)
	if dto.ConnectionTest == nil || dto.ConnectionTest.Messages == nil {
		t.Fatalf("empty messages are nil: %+v", dto.ConnectionTest)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal banking DTO: %v", err)
	}
	if !strings.Contains(string(data), `"messages":[]`) {
		t.Fatalf("JSON does not carry an empty message list: %s", data)
	}
}

func TestConnectionTestDTOCarriesTheVerdictAndNextStep(t *testing.T) {
	tests := []struct {
		name         string
		result       bankingcore.ConnectionTestResult
		wantGo       bool
		wantStep     string
		wantVerdict  string
		wantVerdictV string
		noProduct    bool
	}{
		{
			name:         "GO on the normal path",
			result:       bankingcore.ConnectionTestResult{Go: true, Verdict: bankingcore.ConnectionTestVerdictGo},
			wantGo:       true,
			wantVerdict:  "GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionSyncNow),
		},
		{
			name:         "GO in a build without online sync promises no sync",
			result:       bankingcore.ConnectionTestResult{Go: true, Verdict: bankingcore.ConnectionTestVerdictGo},
			wantGo:       true,
			wantVerdict:  "GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictGo,
			wantStep:     "",
			noProduct:    true,
		},
		{
			name: "GO through the strong sync",
			result: bankingcore.ConnectionTestResult{
				Go:         true,
				Verdict:    bankingcore.ConnectionTestVerdictGo,
				StrongSync: true,
			},
			wantGo:       true,
			wantVerdict:  "GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionStrongSyncNoUnlock),
		},
		{
			name: "running still shows the general help",
			result: bankingcore.ConnectionTestResult{
				Running: true,
			},
			wantVerdict:  "",
			wantVerdictV: "",
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionHelp),
		},
		{
			name: "no bank verdict with no reason information asks for the trace",
			result: bankingcore.ConnectionTestResult{
				Go:      false,
				Verdict: bankingcore.ConnectionTestVerdictNone,
			},
			wantVerdict:  i18n.Text(i18n.En, i18n.TestConnectionNotCompleted),
			wantVerdictV: bankingcore.ConnectionTestVerdictNone,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepTrace),
		},
		{
			name: "NO-GO login rejected checks the login",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindLoginRejected,
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepCheckLogin),
		},
		{
			name: "invalid login without a bank verdict checks the login",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   connectiontest.OtherInvalidLogin,
				Verdict: bankingcore.ConnectionTestVerdictNone,
			},
			wantVerdict:  i18n.Text(i18n.En, i18n.TestConnectionNotCompleted),
			wantVerdictV: bankingcore.ConnectionTestVerdictNone,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepCheckLogin),
		},
		{
			name: "NO-GO missing registration waits",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   "BankError",
				Codes:   []string{"3079", "9078", "9800"},
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepRegistration),
		},
		{
			name: "transport without a bank verdict checks the internet",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   connectiontest.OtherTransport,
				Verdict: bankingcore.ConnectionTestVerdictNone,
			},
			wantVerdict:  i18n.Text(i18n.En, i18n.TestConnectionNotCompleted),
			wantVerdictV: bankingcore.ConnectionTestVerdictNone,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepTransport),
		},
		{
			name: "cancelled",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   connectiontest.OtherCancelled,
				Verdict: bankingcore.ConnectionTestVerdictNone,
			},
			wantGo:       false,
			wantVerdict:  i18n.Text(i18n.En, i18n.TestConnectionNotCompleted),
			wantVerdictV: bankingcore.ConnectionTestVerdictNone,
			wantStep:     "",
		},
		{
			name: "NO-GO account locked asks to unlock",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   "AccountLocked",
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepUnlock),
		},
		{
			name: "NO-GO locked account code asks to unlock",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindLoginRejected,
				Codes:   []string{"3938"},
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepUnlock),
		},
		{
			name: "NO-GO TAN rejected asks to approve in time",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   "TanRejected",
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepApprove),
		},
		{
			name: "approval timeout without a bank verdict asks to approve in time",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindTanTimeout,
				Verdict: bankingcore.ConnectionTestVerdictNone,
			},
			wantVerdict:  i18n.Text(i18n.En, i18n.TestConnectionNotCompleted),
			wantVerdictV: bankingcore.ConnectionTestVerdictNone,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepApprove),
		},
		{
			name: "NO-GO protocol asks for the trace",
			result: bankingcore.ConnectionTestResult{
				Kind:    bankingcore.ConnectionTestKindOther,
				Other:   "Protocol",
				Verdict: bankingcore.ConnectionTestVerdictNoGo,
			},
			wantVerdict:  "NO-GO",
			wantVerdictV: bankingcore.ConnectionTestVerdictNoGo,
			wantStep:     i18n.Text(i18n.En, i18n.TestConnectionNextStepTrace),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := shell.SetOnlineSyncProductPresentForTest(!test.noProduct)
			defer restore()
			state := bankingcore.BankingState{
				Language:       i18n.En,
				ConnectionTest: &test.result,
			}
			dto := bankingDTO(state, i18n.En)
			if dto.ConnectionTest == nil {
				t.Fatal("connection test DTO is missing")
			}
			if dto.ConnectionTest.VerdictText != test.wantVerdict {
				t.Fatalf("verdict = %q, want %q", dto.ConnectionTest.VerdictText, test.wantVerdict)
			}
			if dto.ConnectionTest.Verdict != test.wantVerdictV {
				t.Fatalf("verdict value = %q, want %q", dto.ConnectionTest.Verdict, test.wantVerdictV)
			}
			if dto.ConnectionTest.NextStep != test.wantStep {
				t.Fatalf("next step = %q, want %q", dto.ConnectionTest.NextStep, test.wantStep)
			}
			if dto.ConnectionTest.Go != test.wantGo {
				t.Fatalf("go = %t, want %t", dto.ConnectionTest.Go, test.wantGo)
			}
		})
	}
}

func TestConnectionTestDTOCarriesTheStrongSyncPath(t *testing.T) {
	state := bankingcore.BankingState{
		Language: i18n.En,
		ConnectionTest: &bankingcore.ConnectionTestResult{
			BankID:        7,
			Go:            true,
			StrongSync:    true,
			StrongSyncTAN: "typed",
		},
	}
	dto := bankingDTO(state, i18n.En)
	if dto.ConnectionTest == nil || !dto.ConnectionTest.StrongSync ||
		dto.ConnectionTest.StrongSyncTAN != "typed" {
		t.Fatalf("connection test DTO = %+v", dto.ConnectionTest)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal banking DTO: %v", err)
	}
	if !strings.Contains(string(data), `"strongSync":true`) ||
		!strings.Contains(string(data), `"strongSyncTAN":"typed"`) {
		t.Fatalf("JSON does not carry the strong-sync path: %s", data)
	}
}
