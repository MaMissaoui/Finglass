package banking

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

func TestConnectionTestCloneCopiesTheCodesAndResult(t *testing.T) {
	state := New(i18n.En)
	state.ConnectionTest = &ConnectionTestResult{
		BankID:   7,
		Running:  true,
		Go:       true,
		Reason:   "GO",
		Codes:    []string{"9942"},
		Probed:   1,
		Skipped:  2,
		HasTrace: true,
	}
	state.Banks = []BankItem{{ID: 7, CanTestConnection: true}}

	clone := state.Clone()
	if clone.ConnectionTest == state.ConnectionTest {
		t.Fatal("Clone kept the ConnectionTest pointer")
	}
	if clone.ConnectionTest.BankID != 7 || !clone.ConnectionTest.Running ||
		!clone.ConnectionTest.Go || clone.ConnectionTest.Reason != "GO" ||
		clone.ConnectionTest.Probed != 1 || clone.ConnectionTest.Skipped != 2 ||
		!clone.ConnectionTest.HasTrace {
		t.Fatalf("cloned result = %+v", clone.ConnectionTest)
	}
	clone.ConnectionTest.Codes[0] = "0000"
	if state.ConnectionTest.Codes[0] != "9942" {
		t.Fatal("Clone shares the Codes slice")
	}
	if !state.IsTesting() || !state.IsBusy() {
		t.Fatal("IsTesting/IsBusy do not see the running test")
	}
	clone.ConnectionTest.Running = false
	if clone.IsTesting() || clone.IsBusy() {
		t.Fatal("a finished test still counts as running")
	}
}
