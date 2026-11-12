package pendingbookings

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func database(t *testing.T) *db.Database {
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

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()

	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

func account(t *testing.T, d *db.Database, bankConnectionID *model.BankConnectionID) model.AccountID {
	t.Helper()

	name := "Account"
	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		BankConnectionID: bankConnectionID,
		Name:             &name,
		Currency:         money.EUR,
		AccountType:      model.AccountTypeChecking,
		Balance:          money.Money{Minor: 50_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func bank(t *testing.T, d *db.Database) model.BankConnection {
	t.Helper()

	bic := "COLSDE33XXX"
	connection, err := bankconnections.Create(context.Background(), d, model.NewBankConnection{
		BankName: "Beispiel Bank",
		BLZ:      "37050198",
		BIC:      &bic,
		UserID:   "demo-user",
	})
	if err != nil {
		t.Fatalf("bankconnections.Create: %v", err)
	}
	return connection
}

func item(t *testing.T, minor int64, partner *string) model.NewPendingBooking {
	t.Helper()

	valueDate := date(t, 2026, time.October, 2)
	purpose := "Musterzahlung"
	return model.NewPendingBooking{
		Date:        date(t, 2026, time.October, 3),
		ValueDate:   &valueDate,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		PartnerName: partner,
		Purpose:     &purpose,
	}
}

func fetchedAt() time.Time {
	return time.Date(2026, time.October, 4, 9, 30, 0, 0, time.UTC)
}

func pointer(value string) *string {
	return &value
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestReplaceForAccountDeletesAndInsertsInOneGoAndStampsTheFetchTime(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d, nil)
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -1000, pointer("Beispiel GmbH")),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}

	first, err := FindItems(ctx, d, accountID)
	if err != nil {
		t.Fatalf("FindItems: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first length = %d", len(first))
	}
	if first[0].Amount.Minor != -1000 {
		t.Errorf("amount = %d", first[0].Amount.Minor)
	}
	if first[0].Purpose == nil || *first[0].Purpose != "Musterzahlung" {
		t.Errorf("purpose = %v", first[0].Purpose)
	}
	wantValueDate := date(t, 2026, time.October, 2)
	if first[0].ValueDate == nil || *first[0].ValueDate != wantValueDate {
		t.Errorf("value date = %v", first[0].ValueDate)
	}
	if !first[0].FetchedAt.Equal(fetchedAt()) {
		t.Errorf("fetched at = %v, want %v", first[0].FetchedAt, fetchedAt())
	}

	// A second replace deletes the old row instead of adding a second one.
	later := fetchedAt().Add(5 * time.Minute)
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -2000, pointer("Andere GmbH")),
		item(t, -3000, nil),
	}, later); err != nil {
		t.Fatalf("second ReplaceForAccount: %v", err)
	}
	second, err := FindItems(ctx, d, accountID)
	if err != nil {
		t.Fatalf("FindItems: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("second length = %d", len(second))
	}
	for _, item := range second {
		if item.PartnerName != nil && *item.PartnerName == "Beispiel GmbH" {
			t.Error("the old row was kept")
		}
	}
	if !second[0].FetchedAt.Equal(later) {
		t.Errorf("fetched at = %v, want %v", second[0].FetchedAt, later)
	}
}

func TestReplaceForAccountAuditsTheAccountAndCountOnly(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d, nil)
	before := len(entries(t, d))

	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -1000, pointer("Beispiel GmbH")),
		item(t, -2000, nil),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}

	recent := entries(t, d)
	if len(recent) != before+1 {
		t.Fatalf("audit entries = %d, want %d", len(recent), before+1)
	}
	entry := recent[0]
	if entry.Action != "pending_booking.replace" || entry.Actor != audit.ActorUser {
		t.Errorf("audit entry = %+v", entry)
	}
	if want := "id=" + itoa(int64(accountID)) + " count=2"; entry.Details != want {
		t.Errorf("details = %q, want %q", entry.Details, want)
	}
}

func TestFindItemsListsTheNewestFirst(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d, nil)

	old := item(t, -1000, pointer("Alt GmbH"))
	old.Date = date(t, 2026, time.September, 1)
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		old,
		item(t, -2000, pointer("Neu GmbH")),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}

	items, err := FindItems(ctx, d, accountID)
	if err != nil {
		t.Fatalf("FindItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("length = %d", len(items))
	}
	want := []string{"Neu GmbH", "Alt GmbH"}
	got := make([]string, 0, len(items))
	for _, item := range items {
		if item.PartnerName == nil {
			t.Fatal("partner name missing")
		}
		got = append(got, *item.PartnerName)
	}
	if !slices.Equal(got, want) {
		t.Errorf("partner names = %v, want %v", got, want)
	}

	empty, err := FindItems(ctx, d, model.AccountID(999999))
	if err != nil || len(empty) != 0 {
		t.Errorf("FindItems for an unknown account = %v, %v", empty, err)
	}
}

func TestDeleteForAccountAndTheAccountCascadeRemoveTheRows(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	accountID := account(t, d, nil)
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -1000, pointer("Beispiel GmbH")),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}
	if items, err := FindItems(ctx, d, accountID); err != nil || len(items) == 0 {
		t.Fatalf("FindItems = %v, %v", items, err)
	}
	before := len(entries(t, d))

	if err := DeleteForAccount(ctx, d, accountID); err != nil {
		t.Fatalf("DeleteForAccount: %v", err)
	}
	if items, err := FindItems(ctx, d, accountID); err != nil || len(items) != 0 {
		t.Errorf("FindItems after DeleteForAccount = %v, %v", items, err)
	}
	recent := entries(t, d)
	if len(recent) != before+1 {
		t.Fatalf("audit entries = %d, want %d", len(recent), before+1)
	}
	if recent[0].Action != "pending_booking.delete" ||
		recent[0].Actor != audit.ActorUser ||
		recent[0].Details != "id="+itoa(int64(accountID)) {
		t.Errorf("delete audit = %+v", recent[0])
	}

	// An account delete cascades: pending rows are gone with the account.
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -1000, pointer("Beispiel GmbH")),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}
	if err := accounts.Delete(ctx, d, accountID); err != nil {
		t.Fatalf("accounts.Delete: %v", err)
	}
	if items, err := FindItems(ctx, d, accountID); err != nil || len(items) != 0 {
		t.Errorf("FindItems after the account cascade = %v, %v", items, err)
	}
}

func TestTheBankAccessCascadeRemovesItsAccountsPendingRows(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	connection := bank(t, d)
	accountID := account(t, d, &connection.ID)
	if err := ReplaceForAccount(ctx, d, accountID, []model.NewPendingBooking{
		item(t, -1000, pointer("Beispiel GmbH")),
	}, fetchedAt()); err != nil {
		t.Fatalf("ReplaceForAccount: %v", err)
	}

	deleted, err := bankconnections.DeleteWithAccounts(ctx, d, connection.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteWithAccounts = %v, %v", deleted, err)
	}
	if items, err := FindItems(ctx, d, accountID); err != nil || len(items) != 0 {
		t.Errorf("FindItems after the bank cascade = %v, %v", items, err)
	}
}
