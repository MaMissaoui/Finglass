package banking

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func TestConnectionTestCloneCopiesTheCodesAndResult(t *testing.T) {
	state := New(i18n.En)
	state.ConnectionTest = &ConnectionTestResult{
		BankID:  7,
		Running: true,
		Go:      true,
		Reason:  "GO",
		Codes:   []string{"9942"},
		Messages: []BankMessage{
			{Code: "9210", Segment: "HKTAN", Text: "Invented bank message"},
		},
		Probed:  1,
		Skipped: 2,
	}
	state.LastTrace = &LastTrace{Kind: "test", BankID: 7, HasTrace: true}
	state.Banks = []BankItem{{ID: 7, CanTestConnection: true}}

	clone := state.Clone()
	if clone.ConnectionTest == state.ConnectionTest {
		t.Fatal("Clone kept the ConnectionTest pointer")
	}
	if clone.ConnectionTest.BankID != 7 || !clone.ConnectionTest.Running ||
		!clone.ConnectionTest.Go || clone.ConnectionTest.Reason != "GO" ||
		clone.ConnectionTest.Probed != 1 || clone.ConnectionTest.Skipped != 2 {
		t.Fatalf("cloned result = %+v", clone.ConnectionTest)
	}
	if clone.LastTrace == state.LastTrace || clone.LastTrace.Kind != "test" ||
		clone.LastTrace.BankID != 7 || !clone.LastTrace.HasTrace {
		t.Fatalf("cloned last trace = %+v", clone.LastTrace)
	}
	clone.ConnectionTest.Codes[0] = "0000"
	if state.ConnectionTest.Codes[0] != "9942" {
		t.Fatal("Clone shares the Codes slice")
	}
	if len(clone.ConnectionTest.Messages) != 1 ||
		clone.ConnectionTest.Messages[0].Code != "9210" ||
		clone.ConnectionTest.Messages[0].Segment != "HKTAN" {
		t.Fatalf("cloned messages = %+v", clone.ConnectionTest.Messages)
	}
	clone.ConnectionTest.Messages[0].Text = "changed"
	if state.ConnectionTest.Messages[0].Text != "Invented bank message" {
		t.Fatal("Clone shares the Messages slice")
	}
	if !state.IsTesting() || !state.IsBusy() {
		t.Fatal("IsTesting/IsBusy do not see the running test")
	}
	clone.ConnectionTest.Running = false
	if clone.IsTesting() || clone.IsBusy() {
		t.Fatal("a finished test still counts as running")
	}
}
