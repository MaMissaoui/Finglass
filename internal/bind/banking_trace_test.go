package bind

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/testsupport/perm"
)

func TestBankingDTOListsEachBanksTraceAndSavesTheChosenOne(t *testing.T) {
	const (
		secondBLZ  = "70020270"
		secondName = "Zweite Bank"
	)
	var secondID model.BankConnectionID
	fixture := newBankingFixture(t, banksync.NoCredentialStore{}, func(database *db.Database, _ model.BankConnectionID) {
		bic := "TESTDEFFXXX"
		created, err := bankconnections.Create(context.Background(), database, model.NewBankConnection{
			BankName: secondName,
			BLZ:      secondBLZ,
			BIC:      &bic,
			UserID:   "user-b",
		})
		if err != nil {
			t.Fatalf("create second bank: %v", err)
		}
		secondID = created.ID
	})
	if secondID == 0 {
		t.Fatal("the second bank was not seeded")
	}

	if _, err := fixture.banking.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	fixture.waitBanking(t, "both banks", func(dto BankingDTO) bool {
		return len(dto.Banks) == 2
	})

	traceA := []byte("BANK-A-TRACE")
	traceB := []byte("BANK-B-TRACE")
	fixture.session.SetConnectionTraceForTest(int(fixture.bankID), traceA, "37040044", "sync")
	fixture.session.SetConnectionTraceForTest(int(secondID), traceB, secondBLZ, "sync")

	dto, err := fixture.banking.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(dto.Traces) != 2 {
		t.Fatalf("traces = %+v, want one per bank", dto.Traces)
	}
	for _, want := range []struct {
		bankID int
		kind   string
	}{{int(fixture.bankID), "sync"}, {int(secondID), "sync"}} {
		found := false
		for _, trace := range dto.Traces {
			if trace.BankID == want.bankID && trace.Kind == want.kind && trace.HasTrace {
				found = true
			}
		}
		if !found {
			t.Fatalf("bank %d has no trace in %+v", want.bankID, dto.Traces)
		}
	}

	path := filepath.Join(t.TempDir(), "chosen-trace.txt")
	dialogs := &bindFileDialogs{tracePath: path, traceResult: true}
	fixture.session.SetFileDialogs(dialogs)
	if _, err := fixture.banking.SaveConnectionTrace(int(fixture.bankID)); err != nil {
		t.Fatalf("SaveConnectionTrace: %v", err)
	}
	fixture.waitBanking(t, "the saved trace", func(dto BankingDTO) bool {
		return dto.MessageText == i18n.Text(i18n.En, i18n.MessageConnectionTraceSaved)
	})

	// #nosec G304 -- path is a test temporary file.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved trace: %v", err)
	}
	if string(saved) != string(traceA) {
		t.Fatalf("saved trace = %q, want bank A's bytes", string(saved))
	}
	if strings.Contains(string(saved), string(traceB)) {
		t.Fatal("the saved file contains bank B's trace")
	}
	perm.AssertMode(t, path, 0o600)
	if !strings.HasPrefix(dialogs.traceName, "finglass-trace-37040044-") {
		t.Fatalf("default name = %q, want bank A's BLZ", dialogs.traceName)
	}
	if dialogs.traceTitle != i18n.Text(i18n.En, i18n.SaveConnectionTrace) {
		t.Fatalf("dialog title = %q", dialogs.traceTitle)
	}
}
