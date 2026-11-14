package fintsgateway

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// TestBankingServiceRequestsStatementsFromTheOverlapDate runs the real
// FinTsClient and BankingService against the scripted bank. The service's
// since closure must turn a known account's last sync into the overlap date,
// and the gateway must put it into the HKKAZ request.
func TestBankingServiceRequestsStatementsFromTheOverlapDate(t *testing.T) {
	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

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
		context.Background(), database, bank.ID,
		model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	iban := testIBAN
	if _, err := accounts.Create(context.Background(), database, model.NewAccount{
		BankConnectionID: &bank.ID,
		IBAN:             &iban,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Currency: money.EUR},
	}); err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}

	lastSync := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	if err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"UPDATE bank_connections SET last_sync = ?1 WHERE id = ?2",
			lastSync.Format(time.RFC3339Nano),
			int64(bank.ID),
		)
		return err
	}); err != nil {
		t.Fatalf("set last sync: %v", err)
	}

	account := hiuPd(9, "00000001", "1", testIBAN, "EUR", "Erika Musterfrau", "Girokonto", "HKSAL:1+HKKAZ:1")
	parameters := standardParameters(
		hitans(hitansMethod("942", "chipTAN", -1, 1, 1)),
		"942",
		"HKSAL:N:HKKAZ:N:HKCAZ:N:HKSPA:N",
		account,
	)
	bankFake := newFakeBank(t, standardHandler(
		parameters, parameters,
		func(string) string { return hikazPayload(mt940Fixture) },
		testIBAN,
	))
	gateway := newTestGateway(t, bankFake)
	client := banksync.NewFinTsClient(gateway, t.TempDir(), testProduct, banksync.NewMemoryCredentialStore())
	service := banksync.NewBankingService(client)

	today := date(2026, time.November, 14)
	if _, err := service.SyncBankConnection(
		context.Background(), database, bank.ID, newTestInteraction(testPIN), today,
	); err != nil {
		t.Fatalf("SyncBankConnection: %v", err)
	}

	want := "20261010"
	found := false
	for _, record := range bankFake.snapshotRecords() {
		segment, ok := record.segment("HKKAZ")
		if !ok || segment.DEG(1).Text(0) != testIBAN {
			continue
		}
		found = true
		if got := segment.DEG(3).Text(0); got != want {
			t.Fatalf("HKKAZ start = %q, want %q (10-day overlap)", got, want)
		}
	}
	if !found {
		t.Fatal("no HKKAZ was sent for the known account")
	}
}
