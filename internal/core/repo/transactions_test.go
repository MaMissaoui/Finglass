package repo_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

func repoAccount(t *testing.T, d *db.Database, name string) model.AccountID {
	t.Helper()

	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        pointer(name),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func repoTransaction(hash string, day civil.Date, minor int64) model.NewTransaction {
	return model.NewTransaction{
		BookingDate: day,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		PartnerName: pointer("Partner"),
		Purpose:     pointer("Purpose"),
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
		IsNew:       false,
	}
}

func repoCount(t *testing.T, d *db.Database, query string) int64 {
	t.Helper()

	var count int64
	if err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, query).Scan(&count)
	}); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

func TestImportCountsDuplicatesDeletedAndSameBatchEntries(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	accountID := repoAccount(t, d, "Account")

	first, err := transactions.InsertImported(ctx, d, accountID,
		[]model.NewTransaction{repoTransaction("dup", date(t, 2024, time.January, 1), 100)})
	if err != nil || first.Inserted != 1 {
		t.Fatalf("InsertImported = %+v, %v", first, err)
	}

	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO deleted_bookings (account_id, import_hash, deleted_at)\n"+
				"                 VALUES (?1, 'gone', '2024-01-02T00:00:00Z')",
			int64(accountID),
		)
		return err
	}); err != nil {
		t.Fatalf("seed deleted booking: %v", err)
	}

	outcome, err := transactions.InsertImported(ctx, d, accountID, []model.NewTransaction{
		repoTransaction("dup", date(t, 2024, time.January, 2), 200),
		repoTransaction("gone", date(t, 2024, time.January, 2), 300),
		repoTransaction("same", date(t, 2024, time.January, 3), 400),
		repoTransaction("same", date(t, 2024, time.January, 3), 400),
	})
	if err != nil {
		t.Fatalf("InsertImported batch: %v", err)
	}
	want := transactions.ImportOutcome{Inserted: 2, SkippedDuplicate: 1, SkippedDeleted: 1}
	if outcome != want {
		t.Errorf("outcome = %+v, want %+v", outcome, want)
	}

	bookings, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 3 {
		t.Errorf("bookings = %d, %v: the same hash inside one batch is inserted twice", len(bookings), err)
	}

	start := date(t, 2024, time.January, 2)
	end := date(t, 2024, time.January, 3)
	between, err := transactions.ListForAccount(ctx, d, accountID, &start, &end)
	if err != nil || len(between) != 2 {
		t.Errorf("between = %d, %v", len(between), err)
	}
	for _, booking := range between {
		if booking.ImportHash != "same" {
			t.Errorf("between booking hash = %q", booking.ImportHash)
		}
	}
}

func TestACurrencyMismatchInsertsNothing(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	accountID := repoAccount(t, d, "Account")
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}

	bad := repoTransaction("bad", date(t, 2024, time.January, 1), 100)
	bad.Amount = money.Money{Minor: 100, Currency: usd}

	_, err = transactions.InsertImported(ctx, d, accountID, []model.NewTransaction{bad})
	if !errors.Is(err, repo.ErrCurrencyMismatch) {
		t.Errorf("InsertImported = %v, want ErrCurrencyMismatch", err)
	}
	if count := repoCount(t, d, "SELECT count(*) FROM transactions"); count != 0 {
		t.Errorf("transactions = %d", count)
	}
}

func TestDeletingATransactionPreventsReImport(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	accountID := repoAccount(t, d, "Account")
	if _, err := transactions.InsertImported(ctx, d, accountID,
		[]model.NewTransaction{repoTransaction("hash", date(t, 2024, time.January, 1), 100)}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
	bookings, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}

	if err := transactions.Delete(ctx, d, bookings[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	remaining, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(remaining) != 0 {
		t.Errorf("after Delete = %d, %v", len(remaining), err)
	}
	if count := repoCount(t, d, "SELECT count(*) FROM deleted_bookings"); count != 1 {
		t.Errorf("deleted bookings = %d", count)
	}

	outcome, err := transactions.InsertImported(ctx, d, accountID,
		[]model.NewTransaction{repoTransaction("hash", date(t, 2024, time.January, 1), 100)})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	want := transactions.ImportOutcome{Inserted: 0, SkippedDuplicate: 0, SkippedDeleted: 1}
	if outcome != want {
		t.Errorf("outcome = %+v, want %+v", outcome, want)
	}
	remaining, err = transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(remaining) != 0 {
		t.Errorf("after re-import = %d, %v", len(remaining), err)
	}
}

func TestTransactionMutationsWriteExactlyOneAuditEntryWithoutIBANs(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	iban := "DE89370400440532013000"

	accountID, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        pointer("Audit account"),
		IBAN:        pointer(iban),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	category, err := categories.Create(ctx, d, model.NewCategory{Name: "Category", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	before := len(recentAuditEntries(t, d))

	if _, err := transactions.InsertImported(ctx, d, accountID,
		[]model.NewTransaction{repoTransaction("hash", date(t, 2024, time.January, 1), 100)}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
	before = expectOneTransactionAudit(t, d, before)

	bookings, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}
	if err := transactions.SetCategory(ctx, d, bookings[0].ID, &category); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}
	before = expectOneTransactionAudit(t, d, before)

	if err := transactions.Delete(ctx, d, bookings[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	expectOneTransactionAudit(t, d, before)

	for _, entry := range recentAuditEntries(t, d) {
		if strings.Contains(entry.Details, iban) {
			t.Errorf("an audit entry leaked an IBAN: %q", entry.Details)
		}
		if strings.Contains(entry.Details, "Partner") || strings.Contains(entry.Details, "Purpose") {
			t.Errorf("an audit entry leaked partner or purpose text: %q", entry.Details)
		}
	}
}

func TestDeletingAnAccountCascadesUsingTheTransactionsRepository(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	accountID := repoAccount(t, d, "Account")
	if _, err := transactions.InsertImported(ctx, d, accountID,
		[]model.NewTransaction{repoTransaction("hash", date(t, 2024, time.January, 1), -500)}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
	bookings, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}

	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(
			ctx,
			"INSERT INTO pending_bookings\n"+
				"                    (account_id, booking_date, amount_minor, currency, fetched_at)\n"+
				"                 VALUES (?1, '2024-01-02', 100, 'EUR', '2024-01-02T00:00:00Z')",
			int64(accountID),
		); err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			"INSERT INTO deleted_bookings (account_id, import_hash, deleted_at)\n"+
				"                 VALUES (?1, 'gone', '2024-01-02T00:00:00Z')",
			int64(accountID),
		); err != nil {
			return err
		}
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO documents (file_name, stored_path, mime_type, uploaded_at, transaction_id)\n"+
				"                 VALUES ('a.pdf', 'a.pdf', 'application/pdf', '2024-01-02T00:00:00Z', ?1)",
			int64(bookings[0].ID),
		)
		return err
	}); err != nil {
		t.Fatalf("seed related rows: %v", err)
	}

	if err := accounts.Delete(ctx, d, accountID); err != nil {
		t.Fatalf("accounts.Delete: %v", err)
	}

	if got := repoCount(t, d, "SELECT count(*) FROM transactions"); got != 0 {
		t.Errorf("transactions = %d", got)
	}
	if got := repoCount(t, d, "SELECT count(*) FROM pending_bookings"); got != 0 {
		t.Errorf("pending bookings = %d", got)
	}
	if got := repoCount(t, d, "SELECT count(*) FROM deleted_bookings"); got != 0 {
		t.Errorf("deleted bookings = %d", got)
	}
	if got := repoCount(t, d, "SELECT count(*) FROM documents"); got != 1 {
		t.Errorf("documents = %d", got)
	}
	var linked *int64
	if err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT transaction_id FROM documents").Scan(&linked)
	}); err != nil {
		t.Fatalf("read linked transaction: %v", err)
	}
	if linked != nil {
		t.Errorf("linked transaction = %d, want nil", *linked)
	}
}

func TestDeletingACategoryUncategorizesBookingsUsingTheTransactionsRepository(t *testing.T) {
	d := newDatabase(t)
	ctx := context.Background()
	accountID := repoAccount(t, d, "Account")
	category, err := categories.Create(ctx, d, model.NewCategory{Name: "Food", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	if _, err := rules.Create(ctx, d, model.NewRule{
		Pattern:    "market",
		CategoryID: category,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldPartner,
		Direction:  model.RuleDirectionExpense,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
	transaction := repoTransaction("hash", date(t, 2024, time.January, 1), -500)
	transaction.CategoryID = &category
	if _, err := transactions.InsertImported(ctx, d, accountID, []model.NewTransaction{transaction}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}

	if err := categories.Delete(ctx, d, category); err != nil {
		t.Fatalf("categories.Delete: %v", err)
	}
	listedRules, err := rules.List(ctx, d)
	if err != nil || len(listedRules) != 0 {
		t.Errorf("rules = %d, %v", len(listedRules), err)
	}
	bookings, err := transactions.ListForAccount(ctx, d, accountID, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}
	if bookings[0].CategoryID != nil {
		t.Errorf("category id = %d, want nil", *bookings[0].CategoryID)
	}
}

func recentAuditEntries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	entries, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return entries
}

func expectOneTransactionAudit(t *testing.T, d *db.Database, before int) int {
	t.Helper()

	after := len(recentAuditEntries(t, d))
	if after != before+1 {
		t.Fatalf("audit entries = %d, want exactly one more than %d", after, before)
	}
	return after
}
