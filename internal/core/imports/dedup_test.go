package imports

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func decValue(value string) decimal.Decimal {
	parsed, err := decimal.Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func testNow() time.Time {
	return time.Now().UTC()
}

func g3iDatabase(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func g3iStringPointer(value string) *string {
	return &value
}

func g3iEUR(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func g3iAccount(
	t *testing.T,
	d *db.Database,
	iban *string,
	balanceMinor int64,
	accountType model.AccountType,
) model.AccountID {
	t.Helper()

	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		BankConnectionID: nil,
		Institution:      nil,
		Name:             g3iStringPointer("Account"),
		IBAN:             iban,
		AccountNumber:    nil,
		Currency:         money.EUR,
		AccountType:      accountType,
		Balance:          g3iEUR(balanceMinor),
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func g3iStoredRow(
	t *testing.T,
	day civil.Date,
	amount string,
	hash string,
) model.NewTransaction {
	t.Helper()

	converted, err := AmountToMoney(dec(t, amount), money.EUR)
	if err != nil {
		t.Fatalf("AmountToMoney(%s): %v", amount, err)
	}
	return model.NewTransaction{
		BookingDate:  day,
		Amount:       converted,
		BalanceAfter: nil,
		PartnerName:  g3iStringPointer("Stored"),
		Purpose:      nil,
		CategoryID:   nil,
		ImportHash:   hash,
		CreatedAt:    testNow(),
		IsNew:        false,
		TaxOverride:  nil,
	}
}

func g3iStore(t *testing.T, d *db.Database, accountID model.AccountID, day civil.Date, amount string, hash string) {
	t.Helper()
	if _, err := transactions.InsertImported(
		context.Background(), d, accountID, []model.NewTransaction{g3iStoredRow(t, day, amount, hash)},
	); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
}

func g3iStoreDirect(t *testing.T, d *db.Database, accountID model.AccountID, day civil.Date, amount string, hash string) {
	t.Helper()
	if _, err := transactions.InsertPlanned(
		context.Background(), d, accountID, []model.NewTransaction{g3iStoredRow(t, day, amount, hash)},
	); err != nil {
		t.Fatalf("InsertPlanned: %v", err)
	}
}

func g3iCandidate(item string, day civil.Date, amount string, hash string) Candidate[string] {
	return Candidate[string]{
		Item:   item,
		Date:   day,
		Amount: decValue(amount),
		Hash:   hash,
	}
}

func g3iItems[T any](candidates []Candidate[T]) []T {
	items := make([]T, len(candidates))
	for index, candidate := range candidates {
		items[index] = candidate.Item
	}
	return items
}

func TestAStoredHashIsAnExactDuplicate(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 3)
	g3iStore(t, d, accountID, day, "-5.00", "stored")

	plan, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{g3iCandidate("stored", day, "-5.00", "stored")},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}

	if plan.ExactDuplicates != 1 {
		t.Errorf("ExactDuplicates = %d, want 1", plan.ExactDuplicates)
	}
	if plan.ProbableDuplicates != 0 {
		t.Errorf("ProbableDuplicates = %d, want 0", plan.ProbableDuplicates)
	}
	if len(plan.ToImport) != 0 {
		t.Errorf("ToImport = %v, want empty", g3iItems(plan.ToImport))
	}
}

func TestProbableDuplicatesAreCountedPerDayAndAmount(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 3)
	g3iStore(t, d, accountID, day, "-4.50", "bank-1")
	g3iStore(t, d, accountID, day, "-4.50", "bank-2")

	plan, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{
			g3iCandidate("new-1", day, "-4.50", "file-1"),
			g3iCandidate("new-2", day, "-4.50", "file-2"),
			g3iCandidate("new-3", day, "-4.50", "file-3"),
			g3iCandidate("different", day, "-4.70", "file-4"),
		},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}

	if plan.ProbableDuplicates != 2 {
		t.Errorf("ProbableDuplicates = %d, want 2", plan.ProbableDuplicates)
	}
	got := g3iItems(plan.ToImport)
	want := []string{"new-3", "different"}
	if len(got) != len(want) {
		t.Fatalf("ToImport = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("ToImport[%d] = %q, want %q (one extra coffee stays, a different amount is always new)", index, got[index], want[index])
		}
	}
}

func TestTheOriginalFileOrderIsKept(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	plan, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{
			g3iCandidate("late", testDate(t, 2026, 10, 5), "-3.00", "a"),
			g3iCandidate("early", testDate(t, 2026, 10, 1), "-2.00", "b"),
			g3iCandidate("middle", testDate(t, 2026, 10, 3), "-1.00", "c"),
		},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}

	got := g3iItems(plan.ToImport)
	want := []string{"late", "early", "middle"}
	if len(got) != len(want) {
		t.Fatalf("ToImport = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("ToImport[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestADeletedCandidateIsSkippedAndCountedButAStoredRowWins(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 5)

	g3iStore(t, d, accountID, day, "-1.00", "deleted-hash")
	stored, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored = %d, want 1", len(stored))
	}
	if err := transactions.Delete(context.Background(), d, stored[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	plannedDeleted, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{g3iCandidate("deleted", day, "-1.00", "deleted-hash")},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}
	if plannedDeleted.PreviouslyDeleted != 1 {
		t.Errorf("PreviouslyDeleted = %d, want 1", plannedDeleted.PreviouslyDeleted)
	}
	if len(plannedDeleted.ToImport) != 0 {
		t.Errorf("ToImport = %v, want empty", g3iItems(plannedDeleted.ToImport))
	}

	// A stored row with the same hash is an exact duplicate, regardless of the
	// remembered deletion.
	g3iStoreDirect(t, d, accountID, day, "-1.00", "deleted-hash")
	planned, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{g3iCandidate("again", day, "-1.00", "deleted-hash")},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}
	if planned.ExactDuplicates != 1 {
		t.Errorf("ExactDuplicates = %d, want 1", planned.ExactDuplicates)
	}
	if planned.PreviouslyDeleted != 0 {
		t.Errorf("PreviouslyDeleted = %d, want 0", planned.PreviouslyDeleted)
	}
	if len(planned.ToImport) != 0 {
		t.Errorf("ToImport = %v, want empty", g3iItems(planned.ToImport))
	}
}

func TestADeletedCandidateDoesNotAccountForASameDayStoredBooking(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 5)
	g3iStore(t, d, accountID, day, "-5.00", "stored")
	g3iStore(t, d, accountID, day, "-1.00", "deleted-hash")
	stored, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	var deletedID model.TransactionID
	foundDeleted := false
	for _, booking := range stored {
		if booking.ImportHash == "deleted-hash" {
			deletedID = booking.ID
			foundDeleted = true
			break
		}
	}
	if !foundDeleted {
		t.Fatal("the deleted booking is missing")
	}
	if err := transactions.Delete(context.Background(), d, deletedID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	plan, err := PlanDuplicates(
		context.Background(),
		d,
		accountID,
		money.EUR,
		[]Candidate[string]{
			g3iCandidate("rememberedDeleted", day, "-5.00", "deleted-hash"),
			g3iCandidate("unknown", day, "-5.00", "unknown"),
		},
	)
	if err != nil {
		t.Fatalf("PlanDuplicates: %v", err)
	}

	if plan.ExactDuplicates != 0 {
		t.Errorf("ExactDuplicates = %d, want 0", plan.ExactDuplicates)
	}
	if plan.PreviouslyDeleted != 1 {
		t.Errorf("PreviouslyDeleted = %d, want 1", plan.PreviouslyDeleted)
	}
	if plan.ProbableDuplicates != 1 {
		t.Errorf("ProbableDuplicates = %d, want 1", plan.ProbableDuplicates)
	}
	if len(plan.ToImport) != 0 {
		t.Errorf("ToImport = %v, want empty: the stored row accounts for one same-day booking", g3iItems(plan.ToImport))
	}
}
