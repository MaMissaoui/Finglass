package fintsgateway

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestFinTsClientRemembersTheTypedPinEndToEndAndDropsARejectedOne(t *testing.T) {
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
	)
	var reject atomic.Bool
	bank := newFakeBank(t, func(call int, request exchangeRecord) (string, error) {
		if request.has("HKEND") {
			return okResponse("dialog-1"), nil
		}
		if reject.Load() {
			return bankErrorResponse("dialog-1", "9942"), nil
		}
		return standardHandler(parameters, parameters, func(string) string {
			return hikazPayload(mt940Fixture)
		})(call, request)
	})
	gateway := newTestGateway(t, bank)

	database := newClientTestDatabase(t)
	bankID := newClientTestBank(t, database)
	dataDir := filepath.Join(t.TempDir(), "profile")
	store := banksync.NewMemoryCredentialStore()
	client := banksync.NewFinTsClient(gateway, dataDir, testProduct, store)
	interaction := newTestInteraction(testPIN)
	interaction.remember = true

	if _, err := client.FetchAll(
		context.Background(), database, clientTestBank(t, database, bankID), interaction,
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	); err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	key := banksync.PinKey(dataDir, int64(bankID))
	stored, found, err := store.Read(key)
	if err != nil {
		t.Fatalf("store read: %v", err)
	}
	if !found || string(stored) != testPIN {
		t.Fatalf("stored PIN = %q/%t, want the typed bytes", string(stored), found)
	}
	if !client.HasSavedPin(context.Background(), database, bankID) {
		t.Fatal("has saved PIN = false after a remembered success")
	}

	// The next dialog is rejected with 9942. The stored PIN is used and then
	// deleted; nothing is saved again.
	reject.Store(true)
	_, err = client.FetchAll(
		context.Background(), database, clientTestBank(t, database, bankID),
		newTestInteraction(testPIN),
		func(string) *civil.Date { return nil },
		date(2026, time.November, 14),
	)
	if err == nil {
		t.Fatal("the rejected dialog succeeded")
	}
	if store.Contains(key) {
		t.Fatal("the rejected PIN is still stored")
	}
	if client.HasSavedPin(context.Background(), database, bankID) {
		t.Fatal("has saved PIN = true after a rejection")
	}
}

func newClientTestDatabase(t *testing.T) *db.Database {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), passphrase)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newClientTestBank(t *testing.T, database *db.Database) model.BankConnectionID {
	t.Helper()
	bic := testBIC
	bank, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
		BankName: "Test Bank",
		BLZ:      testBLZ,
		BIC:      &bic,
		UserID:   testUser,
	})
	if err != nil {
		t.Fatalf("bankconnections.Create: %v", err)
	}
	serverURL := testURL
	if _, err := bankconnections.UpdateConnection(
		context.Background(),
		database,
		bank.ID,
		model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("bankconnections.UpdateConnection: %v", err)
	}
	return bank.ID
}

func clientTestBank(t *testing.T, database *db.Database, bankID model.BankConnectionID) model.BankAccessItem {
	t.Helper()
	bank, ok, err := bankconnections.GetItem(context.Background(), database, bankID)
	if err != nil || !ok {
		t.Fatalf("bankconnections.GetItem: ok=%t err=%v", ok, err)
	}
	return bank
}
