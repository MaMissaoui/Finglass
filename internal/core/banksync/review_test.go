package banksync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// scriptedErrorGateway asks for the PIN twice (the stored one, then the
// user's), runs a hook in between, and fails with err.
type scriptedErrorGateway struct {
	between func()
	err     error
}

func (g scriptedErrorGateway) FetchAll(
	_ context.Context,
	connection FinTsConnection,
	interaction BankingInteraction,
	_ func(accountNumber string) *civil.Date,
	_ civil.Date,
) (FetchOutcome, error) {
	_ = interaction.Pin(connection.BankName, connection.UserID, true)
	if g.between != nil {
		g.between()
	}
	_ = interaction.Pin(connection.BankName, connection.UserID, true)
	return FetchOutcome{}, g.err
}

// A Go gateway may return an error that is not a GatewayError; Rust's cannot.
// Its text may carry bank text, so the account number must come back masked.
func TestANonGatewayErrorIsMasked(t *testing.T) {
	h := newHarness(t)
	const iban = "DE89370400440532013000"
	gateway := scriptedErrorGateway{err: errors.New("bank said: account " + iban + " is locked")}
	client := NewFinTsClient(gateway, h.dataDir, "TEST-PRODUCT", h.store)
	bank, ok, err := bankconnections.GetItem(context.Background(), h.db, h.bank)
	if err != nil || !ok {
		t.Fatalf("GetItem: ok=%v err=%v", ok, err)
	}

	_, err = client.FetchAll(context.Background(), h.db, bank, newScriptedInteraction(stringPointer("12345"), false), nil, h.today)
	var bankingError BankingError
	if !errors.As(err, &bankingError) {
		t.Fatalf("error = %T %v, want BankingError", err, err)
	}
	if bankingError.Code != CodeFintsError {
		t.Fatalf("code = %q, want %q", bankingError.Code, CodeFintsError)
	}
	if strings.Contains(bankingError.Message, iban) || !strings.Contains(bankingError.Message, "3000") {
		t.Fatalf("message = %q, want the IBAN masked to its last four", bankingError.Message)
	}
}

// The caller's context is cancelled mid-dialog, after the bank rejected the
// stored PIN. The flag delete and the PIN_FORGOTTEN entry must still happen.
func TestARejectedPinIsForgottenEvenWhenTheContextIsCancelled(t *testing.T) {
	h := newHarness(t)
	background := context.Background()
	bank, ok, err := bankconnections.GetItem(background, h.db, h.bank)
	if err != nil || !ok {
		t.Fatalf("GetItem: ok=%v err=%v", ok, err)
	}
	if err := settings.Set(background, h.db, PinFlagKey(h.bank), LoginSignature(bank)); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := h.store.Write(PinKey(h.dataDir, int64(h.bank)), []byte("99999")); err != nil {
		t.Fatalf("store write: %v", err)
	}

	ctx, cancel := context.WithCancel(background)
	defer cancel()
	gateway := scriptedErrorGateway{between: cancel, err: NewGatewayPinRejectedError()}
	client := NewFinTsClient(gateway, h.dataDir, "TEST-PRODUCT", h.store)

	if _, err := client.FetchAll(ctx, h.db, bank, newScriptedInteraction(stringPointer("12345"), false), nil, h.today); err == nil {
		t.Fatal("FetchAll succeeded, want the rejection")
	}

	if client.HasSavedPin(background, h.db, h.bank) {
		t.Fatal("the flag of the rejected PIN survived a cancelled context")
	}
	if h.store.Contains(PinKey(h.dataDir, int64(h.bank))) {
		t.Fatal("the rejected PIN is still in the store")
	}
	if got := len(auditEntriesWithAction(t, h.db, CodePinForgotten)); got != 1 {
		t.Fatalf("PIN_FORGOTTEN entries = %d, want 1", got)
	}
}
