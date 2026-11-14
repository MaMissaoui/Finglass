package fintsgateway

import (
	"context"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/fints"
)

// TestDialogEndIsANoOpAfterItEnded pins the rule the adapter relies on: an
// explicit End after the child context was cancelled must never send a second
// HKEND when the poll already ended the dialog.
func TestDialogEndIsANoOpAfterItEnded(t *testing.T) {
	bank := newFakeBank(t, func(_ int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		return initBody(nil), nil
	})
	dialog, err := fints.NewDialog(fints.DialogConfig{
		BLZ:            testBLZ,
		UserID:         testUser,
		CustomerID:     testUser,
		ProductID:      testProduct,
		ProductVersion: productVersion,
		PIN:            secret.NewText([]byte(testPIN)),
		Transport:      bank,
		Sleeper:        noopSleeper{},
	})
	if err != nil {
		t.Fatalf("NewDialog: %v", err)
	}
	if _, err := dialog.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := dialog.End(context.Background()); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := dialog.End(context.Background()); err != nil {
		t.Fatalf("second End: %v", err)
	}
	if got := bank.countSegments("HKEND"); got != 1 {
		t.Fatalf("HKEND = %d, want 1", got)
	}
}
