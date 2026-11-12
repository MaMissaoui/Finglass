// Ported Kotlin tests for FinDesk's TransactionsViewModel.
//
// The tests use temporary databases and folders only. Coroutine timing/race
// tests from Kotlin are listed as skipped in the Rust file; every sequential
// behaviour test lives here.
package transactionsview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/ocr"
	documentsservice "github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

var ctx = context.Background()

var counter atomic.Uint64

func createTestDatabase(t *testing.T, file string) *db.Database {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	database, err := db.Create(file, passphrase)
	passphrase.Wipe()
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	file, err := resolved.DatabaseFile("default")
	if err != nil {
		t.Fatalf("database file: %v", err)
	}
	return createTestDatabase(t, file)
}

func assertEqual(t *testing.T, got any, want any, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", what, got, want)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func testMoney(t *testing.T, minor int64, code string) money.Money {
	t.Helper()
	currency, err := money.NewCurrency(code)
	if err != nil {
		t.Fatalf("currency %q: %v", code, err)
	}
	return money.Money{Minor: minor, Currency: currency}
}

func date(t *testing.T, text string) civil.Date {
	t.Helper()
	parsed, err := civil.ParseISO(text)
	if err != nil {
		t.Fatalf("parse date %q: %v", text, err)
	}
	return parsed
}

func utc(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func sp(text string) *string { return &text }

func cp(id model.CategoryID) *model.CategoryID { return &id }

func ap(id model.AccountID) *model.AccountID { return &id }

func tp(id model.TransactionID) *model.TransactionID { return &id }

func pp(period Period) *Period { return &period }

func mp(month dashboard.YearMonth) *dashboard.YearMonth { return &month }

func bp(value bool) *bool { return &value }

func createAccount(
	t *testing.T,
	database *db.Database,
	ibanText string,
	balanceMinor int64,
	currency money.Currency,
	accountType string,
) model.AccountID {
	t.Helper()
	storedType, ok := model.AccountTypeFromStored(accountType)
	if !ok {
		t.Fatalf("unknown account type %q", accountType)
	}
	created, err := accounts.Create(ctx, database, model.NewAccount{
		BankConnectionID: nil,
		Institution:      nil,
		Name:             nil,
		IBAN:             &ibanText,
		AccountNumber:    nil,
		Currency:         currency,
		AccountType:      storedType,
		Balance:          money.Money{Minor: balanceMinor, Currency: currency},
		BalanceUpdatedAt: nil,
		GroupID:          nil,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return created
}

func createCategory(
	t *testing.T,
	database *db.Database,
	name string,
	taxRelevant bool,
	isTransfer bool,
) model.CategoryID {
	t.Helper()
	created, err := categories.Create(ctx, database, model.NewCategory{
		Name:          name,
		Color:         "#FF5722",
		BudgetLimit:   nil,
		IsTaxRelevant: taxRelevant,
		IsTransfer:    isTransfer,
	})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	return created
}

func txn(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	day civil.Date,
	amount money.Money,
	partner *string,
	purpose *string,
	categoryID *model.CategoryID,
	isNew bool,
) model.TransactionID {
	t.Helper()
	unique := counter.Add(1)
	hash := fmt.Sprintf("test-%d-%d", accountID, unique)
	rows := []model.NewTransaction{{
		BookingDate:  day,
		Amount:       amount,
		BalanceAfter: nil,
		PartnerName:  partner,
		Purpose:      purpose,
		CategoryID:   categoryID,
		ImportHash:   hash,
		CreatedAt:    time.Now().UTC(),
		IsNew:        isNew,
		TaxOverride:  nil,
	}}
	if _, err := transactions.InsertPlanned(ctx, database, accountID, rows); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	listed, err := transactions.ListForAccount(ctx, database, accountID, nil, nil)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	for _, transaction := range listed {
		if transaction.ImportHash == hash {
			return transaction.ID
		}
	}
	t.Fatalf("inserted transaction %q not found", hash)
	return 0
}

func view() *TransactionsView {
	return Default()
}

func startAll(t *testing.T, database *db.Database, vm *TransactionsView, today civil.Date) {
	t.Helper()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
}

func startAccount(
	t *testing.T,
	database *db.Database,
	vm *TransactionsView,
	accountID model.AccountID,
	today civil.Date,
) {
	t.Helper()
	requireNoError(t, vm.Start(ctx, database, StartArgs{AccountID: ap(accountID)}, today))
}

func partners(t *testing.T, vm *TransactionsView) []string {
	t.Helper()
	result := make([]string, 0, len(vm.State().Transactions))
	for _, transaction := range vm.State().Transactions {
		if transaction.PartnerName == nil {
			result = append(result, "")
			continue
		}
		result = append(result, *transaction.PartnerName)
	}
	return result
}

func ids(t *testing.T, vm *TransactionsView) []model.TransactionID {
	t.Helper()
	result := make([]model.TransactionID, 0, len(vm.State().Transactions))
	for _, transaction := range vm.State().Transactions {
		result = append(result, transaction.ID)
	}
	return result
}

func actions(t *testing.T, database *db.Database) []string {
	t.Helper()
	entries, err := audit.Recent(ctx, database, 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Action)
	}
	return names
}

func countAction(t *testing.T, database *db.Database, action string) int {
	t.Helper()
	count := 0
	for _, name := range actions(t, database) {
		if name == action {
			count++
		}
	}
	return count
}

func idSet(values ...model.TransactionID) map[model.TransactionID]struct{} {
	set := make(map[model.TransactionID]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func reapplyKeySet(keys ...ReapplyKey) map[ReapplyKey]struct{} {
	set := make(map[ReapplyKey]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

func standard(t *testing.T) (database *db.Database, accountID model.AccountID, food model.CategoryID, tax model.CategoryID) {
	t.Helper()
	database = testDatabase(t)
	accountID = createAccount(t, database, "DE89370400440532013000", 150_000, money.EUR, "checking")
	food = createCategory(t, database, "Food", false, false)
	tax = createCategory(t, database, "Spenden", true, false)
	txn(t, database, accountID, date(t, "2026-03-02"), eur(-3_000), sp("REWE"), nil, cp(food), false)
	txn(t, database, accountID, date(t, "2026-03-05"), eur(-2_000), sp("Kiosk"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-03-01"), eur(250_000), sp("Employer"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-01-15"), eur(-10_000), sp("UNICEF"), nil, cp(tax), false)
	return database, accountID, food, tax
}

// ---------------------------------------------------------------------------
// ViewModelsTest.kt
// ---------------------------------------------------------------------------

func TestTransactionsFiltersApplyAndCategoryFilterIsHonoured(t *testing.T) {
	database, _, food, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	assertEqual(t, len(vm.State().Transactions), 3, "this month")

	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionExpenses, today))
	assertEqual(t, len(vm.State().Transactions), 2, "expenses")

	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, OnlyCategoryFilter(food), today))
	assertEqual(t, partners(t, vm), []string{"REWE"}, "only Food")

	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, NoneCategoryFilter(), today))
	assertEqual(t, partners(t, vm), []string{"Kiosk"}, "uncategorized")

	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, AnyCategoryFilter(), today))
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisYear, today))
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "unic", today))
	assertEqual(t, partners(t, vm), []string{"UNICEF"}, "search")
}

func TestManualTransactionGetsAutoCategorizedAndAnIdenticalOneWaits(t *testing.T) {
	database, accountID, food, _ := standard(t)
	today := date(t, "2026-03-20")
	if _, err := rules.Create(ctx, database, model.NewRule{
		Pattern:    "edeka",
		CategoryID: food,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	vm := view()
	requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
		AccountID:   accountID,
		Date:        today,
		Amount:      eur(-1_234),
		PartnerName: sp("EDEKA Center"),
		Purpose:     nil,
		CategoryID:  nil,
	}, today, time.Now().UTC()))

	var added *model.TransactionItem
	for index := range vm.State().Transactions {
		transaction := &vm.State().Transactions[index]
		if transaction.PartnerName != nil && *transaction.PartnerName == "EDEKA Center" {
			added = transaction
			break
		}
	}
	if added == nil {
		t.Fatal("manual transaction was not added")
	}
	if added.CategoryName == nil || *added.CategoryName != "Food" {
		t.Fatalf("category = %v, want Food", added.CategoryName)
	}

	requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
		AccountID:   accountID,
		Date:        today,
		Amount:      eur(-1_234),
		PartnerName: sp("EDEKA Center"),
		Purpose:     nil,
		CategoryID:  nil,
	}, today, time.Now().UTC()))
	count := 0
	for _, transaction := range vm.State().Transactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == "EDEKA Center" {
			count++
		}
	}
	assertEqual(t, count, 1, "not added before confirming")
	if vm.State().PendingDuplicate == nil {
		t.Fatal("no pending duplicate")
	}
	assertEqual(t, vm.State().PendingDuplicate.Amount, eur(-1_234), "pending amount")

	vm.DismissDuplicate()
	if vm.State().PendingDuplicate != nil {
		t.Fatal("pending duplicate was not dismissed")
	}
	count = 0
	for _, transaction := range vm.State().Transactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == "EDEKA Center" {
			count++
		}
	}
	assertEqual(t, count, 1, "still one after dismissing")
}

func TestAManuallyAddedBookingIsNeverMarkedNew(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
		AccountID:   accountID,
		Date:        today,
		Amount:      eur(-1_234),
		PartnerName: sp("EDEKA Center"),
	}, today, time.Now().UTC()))

	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	for _, transaction := range items {
		if transaction.IsNew {
			t.Fatalf("transaction %d is new", transaction.ID)
		}
	}
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}
}

func TestAConfirmedIdenticalManualEntryIsAddedAsASecondBooking(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	for range 2 {
		requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
			AccountID:   accountID,
			Date:        today,
			Amount:      eur(-350),
			PartnerName: sp("Café"),
			Purpose:     sp("Coffee"),
		}, today, time.Now().UTC()))
	}
	confirmed, err := vm.ConfirmDuplicate(ctx, database, today, time.Now().UTC())
	requireNoError(t, err)
	if !confirmed {
		t.Fatal("nothing was confirmed")
	}
	count := 0
	for _, transaction := range vm.State().Transactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Café" {
			count++
		}
	}
	assertEqual(t, count, 2, "confirmed second booking")
	if vm.State().PendingDuplicate != nil {
		t.Fatal("pending duplicate is still set")
	}
	if vm.State().Error != nil {
		t.Fatalf("error = %q, want none", *vm.State().Error)
	}
	hashes := make(map[string]struct{})
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	for _, transaction := range items {
		if transaction.PartnerName == nil || *transaction.PartnerName != "Café" {
			continue
		}
		listed, err := transactions.ListForAccount(ctx, database, transaction.AccountID, nil, nil)
		requireNoError(t, err)
		for _, row := range listed {
			if row.ID == transaction.ID {
				hashes[row.ImportHash] = struct{}{}
			}
		}
	}
	assertEqual(t, len(hashes), 2, "each entry has its own hash")
	confirmed, err = vm.ConfirmDuplicate(ctx, database, today, time.Now().UTC())
	requireNoError(t, err)
	if confirmed {
		t.Fatal("nothing left to confirm")
	}
}

func TestShowAllFromAPartnerClearsTheNarrowingFilters(t *testing.T) {
	database, accountID, _, _ := standard(t)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	txn(t, database, other, date(t, "2025-11-03"), eur(-1_200), sp("REWE"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionIncome, today))
	requireNoError(t, vm.UpdateAccountFilter(ctx, database, ap(accountID), today))
	requireNoError(t, vm.ShowPartner(ctx, database, "REWE", today))

	state := vm.State()
	wantPartners := []*string{sp("REWE"), sp("REWE")}
	gotPartners := make([]*string, 0, len(state.Transactions))
	for _, transaction := range state.Transactions {
		gotPartners = append(gotPartners, transaction.PartnerName)
	}
	assertEqual(t, gotPartners, wantPartners, "partners")
	assertEqual(t, state.Period, PeriodAll, "period")
	assertEqual(t, state.Direction, model.DirectionAll, "direction")
	if state.AccountID != nil {
		t.Fatalf("account id = %d, want none", *state.AccountID)
	}
}

func TestTheManualDuplicateCheckIsPerAccount(t *testing.T) {
	database, accountID, _, _ := standard(t)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	today := date(t, "2026-03-20")
	vm := view()
	for _, target := range []struct {
		account model.AccountID
		partner string
	}{{accountID, "Bäckerei"}, {other, "Bäckerei"}} {
		requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
			AccountID:   target.account,
			Date:        today,
			Amount:      eur(-450),
			PartnerName: sp(target.partner),
			Purpose:     sp("Brötchen"),
		}, today, time.Now().UTC()))
	}
	if vm.State().PendingDuplicate != nil {
		t.Fatal("a duplicate was detected across accounts")
	}
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	count := 0
	for _, transaction := range items {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Bäckerei" {
			count++
		}
	}
	assertEqual(t, count, 2, "two bookings")

	requireNoError(t, vm.AddManualTransaction(ctx, database, ManualTransaction{
		AccountID:   accountID,
		Date:        today,
		Amount:      eur(-450),
		PartnerName: sp(" Bäckerei "),
		Purpose:     sp("Brötchen "),
	}, today, time.Now().UTC()))
	if vm.State().PendingDuplicate == nil {
		t.Fatal("no pending duplicate")
	}
	assertEqual(t, vm.State().PendingDuplicate.AccountID, accountID, "pending account")
	items, err = transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	count = 0
	for _, transaction := range items {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Bäckerei" {
			count++
		}
	}
	assertEqual(t, count, 2, "still two bookings")
}

func TestStatementImportPreviewsPicksTheAccountFromTheFileAndImports(t *testing.T) {
	database, _, _, _ := standard(t)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	dir := t.TempDir()
	file := filepath.Join(dir, "ing.csv")
	content := "IBAN;DE44 5001 0517 5407 3249 31\n\n" +
		"Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n" +
		"02.10.2025;02.10.2025;Stadtwerke;Lastschrift;Strom;80,00;EUR;-20,00;EUR\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write statement: %v", err)
	}
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.OpenImport(ctx, database, []string{file}, nil, today))

	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	if preview.AccountID == nil || *preview.AccountID != other {
		t.Fatalf("preview account = %v, want %d", preview.AccountID, other)
	}
	assertEqual(t, len(preview.Entries[0].Statement.Bookings), 1, "bookings")
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	count := 0
	for _, transaction := range items {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Stadtwerke" {
			count++
		}
	}
	assertEqual(t, count, 0, "nothing stored before confirming")

	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	if vm.State().ImportPreview != nil {
		t.Fatal("preview is still open")
	}
	assertEqual(t, vm.State().Period, PeriodAll, "period")
	assertEqual(t, partners(t, vm), []string{"Stadtwerke"}, "partners")
	account, ok, err := accounts.Get(ctx, database, other)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(8_000), "balance")
}

func TestRunningBalanceColumnShowsForOneCheckingAccountAndHidesForAllOrADepot(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	if vm.State().Balances == nil {
		t.Fatal("a single checking account shows the column")
	}
	assertEqual(t, vm.State().CurrentBalance, ptrMoney(eur(150_000)), "current balance")

	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	if vm.State().Balances != nil {
		t.Fatal("All accounts never shows it")
	}
	if vm.State().CurrentBalance != nil {
		t.Fatal("All accounts has no current balance")
	}

	depot := createAccount(t, database, "DE02120300000000202051", 500_000, money.EUR, "depot")
	startAccount(t, database, vm, depot, today)
	if vm.State().Balances != nil {
		t.Fatal("a depot's balance is a market value")
	}
}

func ptrMoney(value money.Money) *money.Money {
	return &value
}

func TestSelectingAGroupFiltersToItsAccountsAndAnEmptyCustomGroupShowsNone(t *testing.T) {
	database, _, _, _ := standard(t)
	savings := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "savings")
	txn(t, database, savings, date(t, "2026-03-03"), eur(-1_100), sp("Savings booking"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodAll, today))

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), today))
	assertEqual(t, partners(t, vm), []string{"Savings booking"}, "savings")

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("checking")), today))
	got := make(map[string]struct{})
	for _, partner := range partners(t, vm) {
		got[partner] = struct{}{}
	}
	assertEqual(t, got, stringSet("REWE", "Kiosk", "Employer", "UNICEF"), "checking")

	empty, err := accountgroups.Create(ctx, database, model.NewAccountGroup{Name: "Empty", SortOrder: 10})
	requireNoError(t, err)
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.CustomKey(empty)), today))
	if len(vm.State().Transactions) != 0 {
		t.Fatal("an empty custom group shows no bookings")
	}
}

func TestTheAccountSelectionPersistsAndANewViewRestoresIt(t *testing.T) {
	database, _, _, _ := standard(t)
	savings := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "savings")
	txn(t, database, savings, date(t, "2026-03-03"), eur(-1_100), sp("Savings booking"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), today))

	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, restored.State().Selection, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), "selection")
	assertEqual(t, partners(t, restored), []string{"Savings booking"}, "partners")
}

func TestThePanelSelectionIsSharedBetweenAccountsAndTransactions(t *testing.T) {
	database, accountID, _, _ := standard(t)
	savings := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "savings")
	txn(t, database, savings, date(t, "2026-03-03"), eur(-1_100), sp("Savings booking"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), today))

	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, restored.State().Selection, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), "selection")
	requireNoError(t, restored.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))

	loaded, err := accountpanel.LoadSelection(
		ctx,
		database,
		restored.State().Groups,
		groupingAccounts(restored.State().Accounts),
	)
	requireNoError(t, err)
	assertEqual(t, loaded, accountpanel.AccountSelectionFor(accountID), "stored selection")
}

func TestCollapsedGroupsPersistAndARefreshNeverUndoesAToggle(t *testing.T) {
	database, _, _, _ := standard(t)
	createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "savings")
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.ToggleCollapsed(ctx, database, accountgrouping.TypeKey("savings")))
	requireNoError(t, vm.Load(ctx, database, today))
	assertEqual(t, vm.State().Collapsed, stringSet("type:savings"), "a refresh keeps the toggled state")

	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, restored.State().Collapsed, stringSet("type:savings"), "restored collapsed")
	requireNoError(t, restored.ToggleCollapsed(ctx, database, accountgrouping.TypeKey("savings")))
	if len(restored.State().Collapsed) != 0 {
		t.Fatal("collapsed groups are not empty")
	}
}

func TestAStoredAccountSelectionWhoseAccountWasDeletedFallsBackToAll(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	requireNoError(t, accounts.Delete(ctx, database, accountID))

	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, restored.State().Selection, accountpanel.AllSelection(), "selection")
}

func TestStartWithAnAccountOverridesTheStoredSelection(t *testing.T) {
	database, accountID, _, _ := standard(t)
	savings := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "savings")
	txn(t, database, savings, date(t, "2026-03-03"), eur(-1_100), sp("Savings booking"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("savings")), today))

	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{AccountID: ap(accountID)}, today))
	assertEqual(t, restored.State().Selection, accountpanel.AccountSelectionFor(accountID), "selection")
	assertEqual(t, restored.State().AccountID, ap(accountID), "account id")
}

func TestTheBalanceColumnShowsForAnAccountSelectionAndHidesForAGroup(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	if vm.State().Balances == nil {
		t.Fatal("one account shows the balance column")
	}

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("checking")), today))
	if vm.State().Balances != nil {
		t.Fatal("a group hides the balance column")
	}
	if vm.State().AccountID != nil {
		t.Fatal("a group has no account id")
	}
}

func TestTheBalanceChartSpansTheShownPeriodClampedToToday(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisMonth, today))

	chart := vm.State().BalanceChart
	if chart == nil {
		t.Fatal("the chart shows")
	}
	assertEqual(t, chart[0].Date, date(t, "2026-03-01"), "first day")
	assertEqual(t, chart[len(chart)-1].Date, today, "last day")
	assertEqual(t, len(chart), 20, "length")
	assertEqual(t, chart[len(chart)-1].Balance, eur(150_000), "last balance")
}

func TestATextSearchNeverChangesTheBalanceChart(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisMonth, today))
	before := vm.State().BalanceChart
	if before == nil {
		t.Fatal("the chart shows")
	}

	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "rewe", today))
	assertEqual(t, partners(t, vm), []string{"REWE"}, "partners")
	assertEqual(t, vm.State().BalanceChart, before, "chart")
}

func TestAllAccountsAGroupAndADepotShowNoBalanceChart(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	if vm.State().BalanceChart != nil {
		t.Fatal("All accounts has no chart")
	}

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	if vm.State().BalanceChart == nil {
		t.Fatal("one non-depot account shows the chart")
	}

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("checking")), today))
	if vm.State().BalanceChart != nil {
		t.Fatal("a group has no chart")
	}

	depot := createAccount(t, database, "DE02120300000000202051", 500_000, money.EUR, "depot")
	startAccount(t, database, vm, depot, today)
	if vm.State().BalanceChart != nil {
		t.Fatal("a depot has no chart")
	}
}

func TestTheCollapsedBalanceChartPersistsAndARefreshNeverUndoesAToggle(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	if vm.State().ChartCollapsed {
		t.Fatal("chart starts expanded")
	}

	requireNoError(t, vm.ToggleChart(ctx, database))
	if !vm.State().ChartCollapsed {
		t.Fatal("chart was not collapsed")
	}
	requireNoError(t, vm.Load(ctx, database, today))
	if !vm.State().ChartCollapsed {
		t.Fatal("a refresh keeps the toggled state")
	}

	restored := view()
	startAccount(t, database, restored, accountID, today)
	if !restored.State().ChartCollapsed {
		t.Fatal("the collapsed chart is restored")
	}
	requireNoError(t, restored.ToggleChart(ctx, database))
	if restored.State().ChartCollapsed {
		t.Fatal("chart was not expanded")
	}
}

func TestOpeningAnAccountClearsItsUnreadCountButKeepsItsBookingsBold(t *testing.T) {
	database, accountID, _, _ := standard(t)
	fresh := txn(t, database, accountID, date(t, "2026-03-03"), eur(-1_200), sp("Fresh"), nil, nil, true)
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	assertEqual(t, counts, map[model.AccountID]int64{accountID: 1}, "new counts")

	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	if _, bold := vm.State().BoldIDs[fresh]; !bold {
		t.Fatal("the fresh booking is not bold")
	}
	counts, err = transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}
	found := false
	for _, account := range vm.State().Accounts {
		if account.ID == accountID {
			found = true
			assertEqual(t, account.NewCount, int64(0), "account new count")
		}
	}
	if !found {
		t.Fatal("account row not found")
	}
	if vm.State().Error != nil {
		t.Fatalf("error = %q, want none", *vm.State().Error)
	}
}

func TestANewBookingDatedLastMonthStaysBoldAfterOpeningTheAccount(t *testing.T) {
	database, accountID, _, _ := standard(t)
	late := txn(t, database, accountID, date(t, "2026-02-27"), eur(-800), sp("Late"), nil, nil, true)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	for _, id := range ids(t, vm) {
		if id == late {
			t.Fatal("this month's list doesn't show it")
		}
	}
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}

	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodAll, today))
	found := false
	for _, id := range ids(t, vm) {
		if id == late {
			found = true
		}
	}
	if !found {
		t.Fatal("the late booking is not shown")
	}
	if _, bold := vm.State().BoldIDs[late]; !bold {
		t.Fatal("the late booking is not bold")
	}
}

func TestSelectingAnotherAccountReplacesTheBoldBookings(t *testing.T) {
	database, accountID, _, _ := standard(t)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	first := txn(t, database, accountID, date(t, "2026-03-03"), eur(-1_200), sp("Fresh"), nil, nil, true)
	second := txn(t, database, other, date(t, "2026-03-03"), eur(-300), sp("OtherFresh"), nil, nil, true)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	assertEqual(t, vm.State().BoldIDs, idSet(first), "bold ids")

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(other), today))
	assertEqual(t, vm.State().BoldIDs, idSet(second), "a changed selection replaces the bold set")
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}
}

func TestAllAccountsAndGroupsNeverClearTheCounterAutomatically(t *testing.T) {
	database, accountID, _, _ := standard(t)
	first := txn(t, database, accountID, date(t, "2026-03-03"), eur(-1_200), sp("Fresh"), nil, nil, true)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	second := txn(t, database, other, date(t, "2026-03-03"), eur(-300), sp("OtherFresh"), nil, nil, true)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodAll, today))

	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	assertEqual(t, counts, map[model.AccountID]int64{accountID: 1, other: 1}, "the default All view clears nothing")
	assertEqual(t, vm.State().BoldIDs, idSet(first, second), "bold ids")
	total := int64(0)
	for _, account := range vm.State().Accounts {
		total += account.NewCount
	}
	assertEqual(t, total, int64(2), "new count sum")

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("checking")), today))
	counts, err = transactions.CountNew(ctx, database)
	requireNoError(t, err)
	assertEqual(t, counts, map[model.AccountID]int64{accountID: 1, other: 1}, "selecting a group clears nothing either")
}

func TestMarkReadClearsAGroupsAccountsAndAllClearsEverything(t *testing.T) {
	database, accountID, _, _ := standard(t)
	first := txn(t, database, accountID, date(t, "2026-03-03"), eur(-1_200), sp("Fresh"), nil, nil, true)
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	second := txn(t, database, other, date(t, "2026-03-03"), eur(-300), sp("OtherFresh"), nil, nil, true)
	savings := createAccount(t, database, "DE02120300000000202051", 0, money.EUR, "savings")
	txn(t, database, savings, date(t, "2026-03-04"), eur(-100), sp("SavingsFresh"), nil, nil, true)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodAll, today))

	requireNoError(t, vm.MarkRead(ctx, database, accountpanel.GroupSelection(accountgrouping.TypeKey("checking")), today))
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	assertEqual(t, counts, map[model.AccountID]int64{savings: 1}, "checking cleared")
	if _, bold := vm.State().BoldIDs[first]; !bold {
		t.Fatal("first is not bold")
	}
	if _, bold := vm.State().BoldIDs[second]; !bold {
		t.Fatal("second is not bold")
	}

	requireNoError(t, vm.MarkRead(ctx, database, accountpanel.AllSelection(), today))
	counts, err = transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}
	if vm.State().Error != nil {
		t.Fatalf("error = %q, want none", *vm.State().Error)
	}
}

func TestARestoredAccountSelectionAndADeepLinkClearTheCountLikeAClick(t *testing.T) {
	database, accountID, _, _ := standard(t)
	txn(t, database, accountID, date(t, "2026-03-03"), eur(-1_200), sp("Fresh"), nil, nil, true)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	counts, err := transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}

	txn(t, database, accountID, date(t, "2026-03-05"), eur(-200), sp("Restored"), nil, nil, true)
	restored := view()
	requireNoError(t, restored.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, restored.State().Selection, accountpanel.AccountSelectionFor(accountID), "selection")
	counts, err = transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}

	txn(t, database, accountID, date(t, "2026-03-06"), eur(-200), sp("Linked"), nil, nil, true)
	linked := view()
	startAccount(t, database, linked, accountID, today)
	counts, err = transactions.CountNew(ctx, database)
	requireNoError(t, err)
	if len(counts) != 0 {
		t.Fatalf("new counts = %v, want none", counts)
	}
}

func TestDeletingTheSelectedGroupFallsBackToAll(t *testing.T) {
	database, accountID, _, _ := standard(t)
	group, err := accountgroups.Create(ctx, database, model.NewAccountGroup{Name: "Reisen", SortOrder: 1})
	requireNoError(t, err)
	requireNoError(t, accounts.SetGroup(ctx, database, accountID, &group))
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.GroupSelection(accountgrouping.CustomKey(group)), today))
	assertEqual(t, vm.State().Selection, accountpanel.GroupSelection(accountgrouping.CustomKey(group)), "selection")

	requireNoError(t, vm.DeleteGroup(ctx, database, group, today))
	assertEqual(t, vm.State().Selection, accountpanel.AllSelection(), "selection after delete")
}

func TestGroupActionsAreAuditedAndABlankNameChangesNothing(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))

	requireNoError(t, vm.CreateGroup(ctx, database, "Reisen", today))
	assertEqual(t, countAction(t, database, "account_group.create"), 1, "create audit")
	assertEqual(t, len(vm.State().CustomGroups), 1, "custom groups")
	group := vm.State().CustomGroups[0].ID

	requireNoError(t, vm.RenameGroup(ctx, database, group, "Trips", today))
	assertEqual(t, countAction(t, database, "account_group.rename"), 1, "rename audit")
	assertEqual(t, vm.State().CustomGroups[0].Name, "Trips", "renamed")

	requireNoError(t, vm.MoveGroup(ctx, database, group, true, today))
	if countAction(t, database, "REORDER_ACCOUNT_GROUP") <= 0 {
		t.Fatal("no reorder audit entry")
	}

	requireNoError(t, vm.MoveAccountToGroup(ctx, database, accountID, &group, today))
	assertEqual(t, countAction(t, database, "account.set_group"), 1, "set group audit")
	var moved *model.AccountGroupID
	for _, account := range vm.State().Accounts {
		if account.ID == accountID {
			moved = account.GroupID
		}
	}
	assertEqual(t, moved, &group, "account group")

	requireNoError(t, vm.CreateGroup(ctx, database, "   ", today))
	if vm.State().Error == nil {
		t.Fatal("no error for a blank name")
	}
	assertEqual(t, *vm.State().Error, i18n.Text(i18n.En, i18n.ErrorNameRequired), "blank name error")
	assertEqual(t, len(vm.State().CustomGroups), 1, "custom groups after blank")
	assertEqual(t, countAction(t, database, "account_group.create"), 1, "create audit after blank")

	requireNoError(t, vm.DeleteGroup(ctx, database, group, today))
	assertEqual(t, countAction(t, database, "account_group.delete"), 1, "delete audit")
}

func TestAFilterHidesRowsButTheRemainingBalancesAreUnchanged(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	all := vm.State().Balances
	if all == nil {
		t.Fatal("no balances")
	}
	var rewe model.TransactionID
	for _, transaction := range vm.State().Transactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == "REWE" {
			rewe = transaction.ID
		}
	}

	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "rewe", today))
	assertEqual(t, len(vm.State().Transactions), 1, "filtered rows")
	assertEqual(t, vm.State().Balances[rewe], all[rewe], "remaining balance")
}

func TestExportCSVWritesTheBalanceColumnOnlyWhenItIsShown(t *testing.T) {
	database, accountID, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	dir := t.TempDir()
	withBalance := filepath.Join(dir, "with-balance.csv")
	requireNoError(t, vm.ExportCSV(ctx, database, withBalance))
	data, err := os.ReadFile(withBalance) // #nosec G304 -- test path
	requireNoError(t, err)
	first := strings.TrimSuffix(strings.Split(string(data), "\n")[0], "\r")
	if !strings.HasSuffix(first, ",Balance,Shop") {
		t.Fatalf("header = %q, want it to end with ,Balance,Shop", first)
	}

	withoutBalance := filepath.Join(dir, "without-balance.csv")
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	requireNoError(t, vm.ExportCSV(ctx, database, withoutBalance))
	data, err = os.ReadFile(withoutBalance) // #nosec G304 -- test path
	requireNoError(t, err)
	first = strings.TrimSuffix(strings.Split(string(data), "\n")[0], "\r")
	if strings.Contains(first, "Balance") {
		t.Fatalf("header = %q, the hidden column stays out", first)
	}
}

func TestAmountRangeMatchesAbsoluteValuesInBothDirections(t *testing.T) {
	database, _, _, _ := standard(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisYear, today))
	minAmount := eur(2_500)
	maxAmount := eur(15_000)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &minAmount, &maxAmount, today))
	got := make(map[string]struct{})
	for _, partner := range partners(t, vm) {
		got[partner] = struct{}{}
	}
	assertEqual(t, got, stringSet("REWE", "UNICEF"), "partners")

	large := eur(100_000)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &large, nil, today))
	assertEqual(t, partners(t, vm), []string{"Employer"}, "partners above 1000")
}

func TestRuleFromATransactionEscapesRegexAndDropsReferenceNumbers(t *testing.T) {
	database, accountID, food, _ := standard(t)
	txn(t, database, accountID, date(t, "2026-03-06"), eur(-1_299), sp("NETFLIX.COM 4711-2026"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-02-06"), eur(-1_299), sp("NETFLIX.COM 4711-2025"), nil, nil, false)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	var netflix *model.TransactionItem
	for index := range vm.State().Transactions {
		transaction := &vm.State().Transactions[index]
		if transaction.PartnerName != nil && strings.HasPrefix(*transaction.PartnerName, "NETFLIX") {
			netflix = transaction
			break
		}
	}
	if netflix == nil {
		t.Fatal("netflix booking not found")
	}
	pattern := vm.RulePatternFor(netflix)
	assertEqual(t, pattern, "NETFLIX\\.COM", "pattern")

	requireNoError(t, vm.CreateRule(ctx, database, pattern, food, today))
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, "Rule created and applied to 2 uncategorized transactions", "message")
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	count := 0
	for _, transaction := range items {
		if transaction.PartnerName != nil &&
			strings.HasPrefix(*transaction.PartnerName, "NETFLIX") &&
			transaction.CategoryID != nil && *transaction.CategoryID == food {
			count++
		}
	}
	assertEqual(t, count, 2, "categorized netflix bookings")
}

func TestRuleFromAPayPalTransactionSuggestsTheShop(t *testing.T) {
	database, accountID, _, _ := standard(t)
	txn(
		t,
		database,
		accountID,
		date(t, "2026-03-06"),
		eur(-1_299),
		sp("PayPal Europe S.a.r.l. et Cie S.C.A"),
		sp("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		nil,
		false,
	)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	var paypal *model.TransactionItem
	for index := range vm.State().Transactions {
		transaction := &vm.State().Transactions[index]
		if transaction.PartnerName != nil && strings.HasPrefix(*transaction.PartnerName, "PayPal") {
			paypal = transaction
			break
		}
	}
	if paypal == nil {
		t.Fatal("paypal booking not found")
	}
	assertEqual(t, vm.RulePatternFor(paypal), "Beispiel Shop GmbH", "pattern")
}

func TestReceiptsAttachedFromTheDetailPanelAreLinkedToTheTransaction(t *testing.T) {
	database, _, _, _ := standard(t)
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	documents := documentsservice.NewService(database, ocr.WithoutOCR(), docsDir)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	var rewe *model.TransactionItem
	for index := range vm.State().Transactions {
		transaction := &vm.State().Transactions[index]
		if transaction.PartnerName != nil && *transaction.PartnerName == "REWE" {
			rewe = transaction
			break
		}
	}
	if rewe == nil {
		t.Fatal("REWE booking not found")
	}
	selected := *rewe
	png := filepath.Join(dir, "bon.png")
	if err := os.WriteFile(png, []byte("not really a png"), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}

	requireNoError(t, vm.SelectTransaction(ctx, database, documents, &selected))
	assertEqual(t, len(vm.State().SelectedDocuments), 0, "documents before upload")
	requireNoError(t, vm.AttachDocuments(ctx, database, documents, rewe.ID, []string{png}, today))

	fileNames := make([]string, 0, len(vm.State().SelectedDocuments))
	for _, document := range vm.State().SelectedDocuments {
		fileNames = append(fileNames, document.FileName)
	}
	assertEqual(t, fileNames, []string{"bon.png"}, "attached documents")
	var shown *model.TransactionItem
	for index := range vm.State().Transactions {
		if vm.State().Transactions[index].ID == rewe.ID {
			shown = &vm.State().Transactions[index]
		}
	}
	if shown == nil {
		t.Fatal("booking not found after upload")
	}
	assertEqual(t, shown.DocumentCount, 1, "document count")
	if vm.State().Selected == nil {
		t.Fatal("detail panel closed")
	}
	assertEqual(t, vm.State().Selected.DocumentCount, 1, "selected document count")
}

func TestAutoCategorizeAppliesRulesToUncategorizedTransactions(t *testing.T) {
	database, _, food, _ := standard(t)
	if _, err := rules.Create(ctx, database, model.NewRule{
		Pattern:    "kiosk",
		CategoryID: food,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.AutoCategorize(ctx, database, today))
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, "Categorized 1 of 2 uncategorized transactions", "message")
}

// ---------------------------------------------------------------------------
// TransactionsViewModelPendingTest.kt
// ---------------------------------------------------------------------------

func pendingItem(t *testing.T, accountID model.AccountID) model.PendingBookingItem {
	t.Helper()
	return model.PendingBookingItem{
		AccountID:   accountID,
		Date:        date(t, "2026-10-03"),
		ValueDate:   ptrDate(date(t, "2026-10-02")),
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
		FetchedAt:   utc(t, 2026, time.October, 4, 9, 30),
	}
}

func ptrDate(value civil.Date) *civil.Date { return &value }

func TestPendingRowsAreInTheStateForOneAccountAndAbsentForAllAccounts(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-10-03"), eur(-12_000), sp("Beispiel AG"), sp("Gehalt"), nil, false)
	requireNoError(t, pendingbookings.ReplaceForAccount(ctx, database, accountID, []model.NewPendingBooking{{
		Date:        date(t, "2026-10-03"),
		ValueDate:   ptrDate(date(t, "2026-10-02")),
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
	}}, utc(t, 2026, time.October, 4, 9, 30)))
	today := date(t, "2026-10-04")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	assertEqual(t, vm.State().PendingBookings, []model.PendingBookingItem{pendingItem(t, accountID)}, "pending rows")

	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AllSelection(), today))
	if len(vm.State().PendingBookings) != 0 {
		t.Fatal("all accounts shows none")
	}
}

func TestPendingRowsStayOutOfTheCountedBookings(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-10-03"), eur(-12_000), sp("Beispiel AG"), sp("Gehalt"), nil, false)
	requireNoError(t, pendingbookings.ReplaceForAccount(ctx, database, accountID, []model.NewPendingBooking{{
		Date:        date(t, "2026-10-03"),
		ValueDate:   nil,
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
	}}, utc(t, 2026, time.October, 4, 9, 30)))
	today := date(t, "2026-10-04")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	assertEqual(t, len(vm.State().Transactions), 1, "the list counts only the booking")
	assertEqual(t, len(vm.State().PendingBookings), 1, "pending rows")
	assertEqual(t, vm.State().TotalExpenses, []money.CurrencyTotal{{Currency: money.EUR, Total: 12_000}}, "total expenses")
}

func TestPendingRowsShowOnlyUnfilteredForAPeriodThatIncludesToday(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	categoryID := createCategory(t, database, "Beispiel Haushalt", false, false)
	txn(t, database, accountID, date(t, "2026-10-03"), eur(-12_000), sp("Beispiel AG"), sp("Gehalt"), nil, false)
	requireNoError(t, pendingbookings.ReplaceForAccount(ctx, database, accountID, []model.NewPendingBooking{{
		Date:        date(t, "2026-10-03"),
		ValueDate:   ptrDate(date(t, "2026-10-02")),
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
	}}, utc(t, 2026, time.October, 4, 9, 30)))
	today := date(t, "2026-10-04")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	assertEqual(t, vm.State().PendingBookings, []model.PendingBookingItem{pendingItem(t, accountID)}, "pending rows")

	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisMonth, today))
	assertEqual(t, vm.State().PendingBookings, []model.PendingBookingItem{pendingItem(t, accountID)}, "this month keeps them")

	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, OnlyCategoryFilter(categoryID), today))
	if len(vm.State().PendingBookings) != 0 {
		t.Fatal("a category filter hides pending rows")
	}
	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, AnyCategoryFilter(), today))
	assertEqual(t, vm.State().PendingBookings, []model.PendingBookingItem{pendingItem(t, accountID)}, "pending rows after clearing")

	requireNoError(t, vm.Start(ctx, database, StartArgs{AccountID: ap(accountID), UncategorizedOnly: true}, today))
	if len(vm.State().PendingBookings) != 0 {
		t.Fatal("uncategorized only hides pending rows")
	}

	requireNoError(t, vm.Start(ctx, database, StartArgs{AccountID: ap(accountID), Period: pp(PeriodLastMonth)}, today))
	if len(vm.State().PendingBookings) != 0 {
		t.Fatal("last month hides pending rows")
	}

	startAccount(t, database, vm, accountID, today)
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "Gehalt", today))
	if len(vm.State().PendingBookings) != 0 {
		t.Fatal("a search hides pending rows")
	}
}

func TestPendingRowsShowOnlyWhileTheirFetchStampIsWithinTheMaxAge(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-10-03"), eur(-12_000), sp("Beispiel AG"), sp("Gehalt"), nil, false)
	hold := model.NewPendingBooking{
		Date:        date(t, "2026-09-04"),
		ValueDate:   nil,
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
	}
	today := date(t, "2026-10-04")
	vm := view()

	for _, testCase := range []struct {
		stamp    time.Time
		expected bool
	}{
		{utc(t, 2026, time.October, 4, 12, 0), true},
		{utc(t, 2026, time.September, 28, 12, 0), true},
		{utc(t, 2026, time.September, 27, 12, 0), true},
		{utc(t, 2026, time.September, 26, 12, 0), false},
	} {
		requireNoError(t, pendingbookings.ReplaceForAccount(ctx, database, accountID, []model.NewPendingBooking{hold}, testCase.stamp))
		startAccount(t, database, vm, accountID, today)
		assertEqual(t, len(vm.State().PendingBookings) != 0, testCase.expected, "stamp "+testCase.stamp.String())
	}
}

func TestPendingRowsNeverChangeTheTotalsOrTheCSVExport(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-10-01"), eur(-12_000), sp("Beispiel AG"), sp("Gehalt"), nil, false)
	today := date(t, "2026-10-04")
	vm := view()
	startAccount(t, database, vm, accountID, today)
	totalsBefore := vm.State().SelectedTotals()

	requireNoError(t, pendingbookings.ReplaceForAccount(ctx, database, accountID, []model.NewPendingBooking{{
		Date:        date(t, "2026-10-03"),
		ValueDate:   nil,
		Amount:      eur(-4_980),
		PartnerName: sp("Beispiel Tank GmbH"),
		Purpose:     sp("Kraftstoff"),
	}}, utc(t, 2026, time.October, 4, 9, 30)))
	requireNoError(t, vm.Load(ctx, database, today))
	assertEqual(t, vm.State().SelectedTotals(), totalsBefore, "selected totals")

	csvPath := filepath.Join(t.TempDir(), "pending-export.csv")
	requireNoError(t, vm.ExportCSV(ctx, database, csvPath))
	data, err := os.ReadFile(csvPath) // #nosec G304 -- test path
	requireNoError(t, err)
	text := string(data)
	if !strings.Contains(text, "Beispiel AG") {
		t.Fatal("the booking is missing from the export")
	}
	if strings.Contains(text, "Beispiel Tank GmbH") {
		t.Fatal("a pending row reached the export")
	}
}

// ---------------------------------------------------------------------------
// TransactionsImportPeriodTest.kt
// ---------------------------------------------------------------------------

func csvFile(t *testing.T, dir, name, ibanText string, rows []string) string {
	t.Helper()
	return multiCSV(t, dir, name, &ibanText, rows)
}

func multiCSV(t *testing.T, dir, name string, ibanLine *string, rows []string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var text strings.Builder
	if ibanLine != nil {
		text.WriteString("IBAN;" + *ibanLine + "\n\n")
	}
	text.WriteString("Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n")
	for _, row := range rows {
		text.WriteString(row)
	}
	if err := os.WriteFile(path, []byte(text.String()), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

func importRow(t *testing.T, day, partner, purpose, saldo, amount string) string {
	t.Helper()
	return day + ";" + day + ";" + partner + ";Lastschrift;" + purpose + ";" + saldo + ";EUR;" + amount + ";EUR\n"
}

func TestAnImportClearsTheExactMonthAndShowsBookingsOutsideIt(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	createAccount(t, database, ibanText, 0, money.EUR, "checking")
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Month: mp(dashboard.NewYearMonth(2026, 1))}, today))
	assertEqual(t, vm.State().Month, mp(dashboard.NewYearMonth(2026, 1)), "month")

	file := csvFile(t, t.TempDir(), "october.csv", ibanText,
		[]string{importRow(t, "02.10.2025", "Stadtwerke", "Import", "80,00", "-20,00")})
	requireNoError(t, vm.OpenImport(ctx, database, []string{file}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, false, today))

	if vm.State().Month != nil {
		t.Fatal("month is still set")
	}
	if vm.State().CustomRange != nil {
		t.Fatal("custom range is still set")
	}
	assertEqual(t, vm.State().Period, PeriodAll, "period")
	assertEqual(t, partners(t, vm), []string{"Stadtwerke"}, "partners")
}

func TestAnImportClearsACustomRangeAndShowsBookingsOutsideIt(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	accountID := createAccount(t, database, ibanText, 0, money.EUR, "checking")
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))
	assertEqual(t, vm.State().CustomRange, &DateRange{From: date(t, "2025-01-01"), To: date(t, "2025-03-31")}, "custom range")

	file := csvFile(t, t.TempDir(), "october.csv", ibanText,
		[]string{importRow(t, "02.10.2025", "Stadtwerke", "Import", "80,00", "-20,00")})
	requireNoError(t, vm.OpenImport(ctx, database, []string{file}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, false, today))

	if vm.State().CustomRange != nil {
		t.Fatal("custom range is still set")
	}
	if vm.State().Month != nil {
		t.Fatal("month is still set")
	}
	assertEqual(t, vm.State().Period, PeriodAll, "period")
	assertEqual(t, partners(t, vm), []string{"Stadtwerke"}, "partners")
	assertEqual(t, vm.State().AccountID, ap(accountID), "account id")
}

// ---------------------------------------------------------------------------
// TransactionsCustomRangeTest.kt
// ---------------------------------------------------------------------------

func customRangeFixture(t *testing.T) (
	database *db.Database,
	yearEnd, newYear, marchEnd, aprilStart model.TransactionID,
) {
	t.Helper()
	database = testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	yearEnd = txn(t, database, accountID, date(t, "2024-12-31"), eur(-1_000), sp("Year End"), nil, nil, false)
	newYear = txn(t, database, accountID, date(t, "2025-01-01"), eur(-2_000), sp("New Year"), nil, nil, false)
	marchEnd = txn(t, database, accountID, date(t, "2025-03-31"), eur(-3_000), sp("March End"), nil, nil, false)
	aprilStart = txn(t, database, accountID, date(t, "2025-04-01"), eur(-4_000), sp("April Start"), nil, nil, false)
	return database, yearEnd, newYear, marchEnd, aprilStart
}

func TestACustomRangeShowsExactlyTheBookingsInsideItBoundsInclusive(t *testing.T) {
	database, _, newYear, marchEnd, _ := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))

	assertEqual(t, ids(t, vm), []model.TransactionID{marchEnd, newYear}, "ids")
	assertEqual(t, vm.State().CustomRange, &DateRange{From: date(t, "2025-01-01"), To: date(t, "2025-03-31")}, "custom range")
	if vm.State().Month != nil {
		t.Fatal("month is set")
	}
}

func TestAReversedRangeChangesNothing(t *testing.T) {
	database, yearEnd, newYear, marchEnd, aprilStart := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-03-31"), date(t, "2025-01-01"), today))

	if vm.State().CustomRange != nil {
		t.Fatal("custom range is set")
	}
	assertEqual(t, ids(t, vm), []model.TransactionID{aprilStart, marchEnd, newYear, yearEnd}, "ids")
}

func TestSettingACustomRangeClearsAnActiveSelection(t *testing.T) {
	database, _, newYear, _, _ := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	vm.ToggleSelection(newYear)
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))
	if vm.State().SelectionAnchorID != nil {
		t.Fatal("selection anchor is set")
	}
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("selected ids are set")
	}
}

func TestPickingAPeriodClearsTheCustomRange(t *testing.T) {
	database, yearEnd, newYear, marchEnd, aprilStart := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))
	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodAll, today))
	if vm.State().CustomRange != nil {
		t.Fatal("custom range is set")
	}
	assertEqual(t, ids(t, vm), []model.TransactionID{aprilStart, marchEnd, newYear, yearEnd}, "ids")
}

func TestAMonthDeepLinkClearsTheCustomRange(t *testing.T) {
	database, yearEnd, _, _, _ := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))
	requireNoError(t, vm.Start(ctx, database, StartArgs{Month: mp(dashboard.NewYearMonth(2024, 12))}, today))
	if vm.State().CustomRange != nil {
		t.Fatal("custom range is set")
	}
	assertEqual(t, vm.State().Month, mp(dashboard.NewYearMonth(2024, 12)), "month")
	assertEqual(t, ids(t, vm), []model.TransactionID{yearEnd}, "ids")
}

func TestAPlainStartKeepsAnExistingCustomRange(t *testing.T) {
	database, _, newYear, marchEnd, _ := customRangeFixture(t)
	today := date(t, "2025-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Period: pp(PeriodAll)}, today))
	requireNoError(t, vm.SetCustomRange(ctx, database, date(t, "2025-01-01"), date(t, "2025-03-31"), today))
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, vm.State().CustomRange, &DateRange{From: date(t, "2025-01-01"), To: date(t, "2025-03-31")}, "custom range")
	assertEqual(t, ids(t, vm), []model.TransactionID{marchEnd, newYear}, "ids")
}

// ---------------------------------------------------------------------------
// ReapplyRulesTest.kt
// ---------------------------------------------------------------------------

func rule(t *testing.T, database *db.Database, pattern string, categoryID model.CategoryID, priority int64, direction model.RuleDirection) {
	t.Helper()
	if _, err := rules.Create(ctx, database, model.NewRule{
		Pattern:    pattern,
		CategoryID: categoryID,
		Priority:   priority,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  direction,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
}

func TestAHigherPriorityRuleMovesABookingToTheRulesCategory(t *testing.T) {
	database := testDatabase(t)
	loans := createCategory(t, database, "Loans", false, false)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "SPARKASSE", loans, 1, model.RuleDirectionAny)
	rule(t, database, "SPARKASSE", groceries, 5, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	booking := txn(t, database, accountID, date(t, "2026-06-14"), eur(-990), sp("Sparkasse"), nil, cp(loans), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	preview := vm.State().ReapplyPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	if len(preview.Changes) == 0 {
		t.Fatal("no changes")
	}
	assertEqual(t, preview.Changes[0], ReapplyChange{
		TransactionID:  booking,
		FromCategoryID: cp(loans),
		ToCategoryID:   groceries,
	}, "change")
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	row := findRow(t, database, booking)
	assertEqual(t, row.CategoryID, cp(groceries), "category")
}

func findRow(t *testing.T, database *db.Database, id model.TransactionID) model.TransactionItem {
	t.Helper()
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	for _, transaction := range items {
		if transaction.ID == id {
			return transaction
		}
	}
	t.Fatalf("transaction %d not found", id)
	return model.TransactionItem{}
}

func TestABookingNoRuleMatchesKeepsItsCategoryAndNeverBecomesNull(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	booking := txn(t, database, accountID, date(t, "2026-06-14"), eur(-320), sp("Wonder.Category"), nil, cp(groceries), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	if vm.State().ReapplyPreview != nil {
		t.Fatal("a preview was opened")
	}
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, booking).CategoryID, cp(groceries), "category")
}

func TestABookingWhoseMatchedCategoryEqualsItsOwnIsNotAChange(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(groceries), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	if vm.State().ReapplyPreview != nil {
		t.Fatal("a preview was opened")
	}
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 0, "update audits")
}

func TestAnExpenseOnlyRuleDoesNotChangeAnIncomeBooking(t *testing.T) {
	database := testDatabase(t)
	salary := createCategory(t, database, "Salary", false, false)
	rule(t, database, "PARKING", salary, 1, model.RuleDirectionExpense)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	booking := txn(t, database, accountID, date(t, "2026-06-14"), eur(1_000), sp("Parking fee"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	if vm.State().ReapplyPreview != nil {
		t.Fatal("a preview was opened")
	}
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, booking).CategoryID, (*model.CategoryID)(nil), "category")
}

func TestBookingsOutsideTheCurrentFilterAreUntouched(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	inside := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix Shops"), nil, nil, false)
	outside := txn(t, database, accountID, date(t, "2026-06-13"), eur(-700), sp("Netflix Vacations"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "Vacations", today))
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	preview := vm.State().ReapplyPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, len(preview.Changes), 1, "changes")
	assertEqual(t, preview.Changes[0].TransactionID, outside, "change booking")

	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, inside).CategoryID, (*model.CategoryID)(nil), "inside category")
	assertEqual(t, findRow(t, database, outside).CategoryID, cp(groceries), "outside category")
}

func TestConfirmAppliesExactlyThePreviewedSetEvenWhenANewRuleMatchesMore(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	previewed := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, nil, false)
	extra := txn(t, database, accountID, date(t, "2026-06-13"), eur(-1_500), sp("Disneyplus"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	preview := vm.State().ReapplyPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, len(preview.Changes), 1, "changes")
	assertEqual(t, preview.Changes[0].TransactionID, previewed, "change booking")
	assertEqual(t, preview.ShownCount, 2, "shown count")

	rule(t, database, "DISNEYPLUS", groceries, 9, model.RuleDirectionAny)
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, previewed).CategoryID, cp(groceries), "previewed category")
	assertEqual(t, findRow(t, database, extra).CategoryID, (*model.CategoryID)(nil), "extra category")
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 1, "update audits")
}

func TestANewRuleAddedAfterThePreviewDoesNotSneakIn(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	previewed := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	rule(t, database, "NETFLIX", loans, 5, model.RuleDirectionAny)
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, previewed).CategoryID, cp(groceries), "category")
}

func TestExcludingAGroupLeavesItsBookingsUnchanged(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	rule(t, database, "SPARKASSE", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	fromCategory := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(loans), false)
	fromUncategorized := txn(t, database, accountID, date(t, "2026-06-13"), eur(-785), sp("Sparkasse"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	assertEqual(t, len(vm.State().ReapplyPreview.Groups), 2, "groups")
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, reapplyKeySet(ReapplyKey{
		FromCategoryID: cp(loans),
		ToCategoryID:   groceries,
	}), today))
	assertEqual(t, findRow(t, database, fromCategory).CategoryID, cp(loans), "from category")
	assertEqual(t, findRow(t, database, fromUncategorized).CategoryID, cp(groceries), "from uncategorized")
}

func TestABookingChangedByHandBetweenPreviewAndConfirmIsSkipped(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	salary := createCategory(t, database, "Salary", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	booking := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(loans), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	requireNoError(t, vm.UpdateTransactionCategory(ctx, database, booking, cp(salary), today))
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, findRow(t, database, booking).CategoryID, cp(salary), "category")
}

func TestUndoRestoresPreviousValuesIncludingNullAndThenCannotUndoAgain(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	rule(t, database, "SPARKASSE", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	fromCategory := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(loans), false)
	fromUncategorized := txn(t, database, accountID, date(t, "2026-06-13"), eur(-785), sp("Sparkasse"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	if !vm.State().CanUndoReapply {
		t.Fatal("cannot undo the re-apply")
	}

	requireNoError(t, vm.UndoReapplyRules(ctx, database, today))
	assertEqual(t, findRow(t, database, fromCategory).CategoryID, cp(loans), "from category")
	assertEqual(t, findRow(t, database, fromUncategorized).CategoryID, (*model.CategoryID)(nil), "from uncategorized")
	if vm.State().CanUndoReapply {
		t.Fatal("can still undo")
	}
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 2, "update audits")
}

func TestUndoKeepsABookingThatWasMovedByHandAfterTheReapply(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	salary := createCategory(t, database, "Salary", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	movedAgain := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(loans), false)
	untouched := txn(t, database, accountID, date(t, "2026-06-13"), eur(-800), sp("Netflix"), nil, cp(loans), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	requireNoError(t, vm.UpdateTransactionCategory(ctx, database, movedAgain, cp(salary), today))
	requireNoError(t, vm.UndoReapplyRules(ctx, database, today))
	assertEqual(t, findRow(t, database, movedAgain).CategoryID, cp(salary), "moved again")
	assertEqual(t, findRow(t, database, untouched).CategoryID, cp(loans), "untouched")
	if vm.State().CanUndoReapply {
		t.Fatal("can still undo")
	}
}

func TestAConfirmThatChangesNothingKeepsThePreviousUndo(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	first := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(loans), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	if !vm.State().CanUndoReapply {
		t.Fatal("cannot undo the re-apply")
	}

	second := txn(t, database, accountID, date(t, "2026-06-12"), eur(-500), sp("Netflix"), nil, cp(loans), false)
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	requireNoError(t, vm.UpdateTransactionCategory(ctx, database, second, cp(groceries), today))
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	if !vm.State().CanUndoReapply {
		t.Fatal("the first re-apply can still be undone")
	}

	requireNoError(t, vm.UndoReapplyRules(ctx, database, today))
	assertEqual(t, findRow(t, database, first).CategoryID, cp(loans), "first category")
}

func TestNoAuditEntryAndNoDialogWhenNothingWouldChange(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, cp(groceries), false)
	txn(t, database, accountID, date(t, "2026-06-13"), eur(-320), sp("Wonder.Category"), nil, cp(groceries), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	if vm.State().CanUndoReapply {
		t.Fatal("can undo")
	}
	requireNoError(t, vm.ConfirmReapplyRules(ctx, database, map[ReapplyKey]struct{}{}, today))
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 0, "update audits")
}

func changeValues(changes []ReapplyChange) map[[3]int64]struct{} {
	values := make(map[[3]int64]struct{}, len(changes))
	for _, change := range changes {
		from := int64(-1)
		if change.FromCategoryID != nil {
			from = int64(*change.FromCategoryID)
		}
		values[[3]int64{int64(change.TransactionID), from, int64(change.ToCategoryID)}] = struct{}{}
	}
	return values
}

func TestThePreviewGroupsByFromToPairWithASumOnlyInThePrimaryCurrency(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	eurAccount := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	usdAccount := createAccount(t, database, "DE44500105175407324931", 0, testCurrency(t, "USD"), "checking")
	euro := txn(t, database, eurAccount, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, nil, false)
	dollar := txn(t, database, usdAccount, date(t, "2026-06-13"), testMoney(t, -500, "USD"), sp("Netflix USD"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	preview := vm.State().ReapplyPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, changeValues(preview.Changes), map[[3]int64]struct{}{
		{int64(euro), -1, int64(groceries)}:   {},
		{int64(dollar), -1, int64(groceries)}: {},
	}, "changes")
	assertEqual(t, len(preview.Groups), 1, "groups")
	assertEqual(t, preview.Groups[0].Count, 2, "count")
	if preview.Groups[0].Sum != nil {
		t.Fatalf("sum = %v, want none", *preview.Groups[0].Sum)
	}
	assertEqual(t, preview.Groups[0].Currency, money.EUR, "currency")
}

func testCurrency(t *testing.T, code string) money.Currency {
	t.Helper()
	currency, err := money.NewCurrency(code)
	if err != nil {
		t.Fatalf("currency %q: %v", code, err)
	}
	return currency
}

func TestDirectionAndCategoryFiltersLimitThePreviewToTheShownBookings(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	rule(t, database, "NETFLIX", groceries, 0, model.RuleDirectionAny)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	expense := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_200), sp("Netflix"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-06-13"), eur(800), sp("Netflix plus"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionExpenses, today))
	requireNoError(t, vm.PreviewReapplyRules(ctx, database, today))
	preview := vm.State().ReapplyPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, len(preview.Changes), 1, "changes")
	assertEqual(t, preview.Changes[0].TransactionID, expense, "change booking")
	assertEqual(t, preview.ShownCount, 1, "shown count")
}

// ---------------------------------------------------------------------------
// TaxOverrideTest.kt (transaction view model parts)
// ---------------------------------------------------------------------------

func TestSetTaxOverrideForSelectionChangesOnlySelectedAndShownBookings(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-12"), eur(-1_000), sp("Secret Partner"), sp("Secret purpose"), cp(normal), false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Other Partner"), nil, cp(normal), false)
	c := txn(t, database, accountID, date(t, "2026-06-14"), eur(-3_000), sp("Third Partner"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(a)
	vm.ToggleSelection(b)
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids")

	requireNoError(t, vm.SetTaxOverrideForSelection(ctx, database, bp(true), today))
	for _, id := range []model.TransactionID{a, b} {
		row := findRow(t, database, id)
		if !row.IsTaxRelevant {
			t.Fatalf("transaction %d is not tax relevant", id)
		}
		assertEqual(t, row.TaxSource, model.TaxSourceManual, "tax source")
		assertEqual(t, row.TaxOverride, bp(true), "tax override")
	}
	row := findRow(t, database, c)
	if row.IsTaxRelevant {
		t.Fatal("third booking is tax relevant")
	}
	assertEqual(t, row.TaxSource, model.TaxSourceCategory, "third tax source")
	assertEqual(t, row.TaxOverride, (*bool)(nil), "third tax override")
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids after change")

	entries, err := audit.Recent(ctx, database, 1000)
	requireNoError(t, err)
	taxEntries := make([]audit.Entry, 0)
	for _, entry := range entries {
		if entry.Action == "transaction.set_tax_override" {
			taxEntries = append(taxEntries, entry)
		}
	}
	assertEqual(t, len(taxEntries), 1, "tax audit entries")
	details := taxEntries[0].Details
	if !strings.Contains(details, "count=2") {
		t.Fatalf("details = %q, want count=2", details)
	}
	if !strings.Contains(details, "true") {
		t.Fatalf("details = %q, want true", details)
	}
	if strings.Contains(details, "Secret") {
		t.Fatalf("details = %q, want no partner", details)
	}
	if strings.Contains(details, "purpose") {
		t.Fatalf("details = %q, want no purpose", details)
	}
	if strings.Contains(details, "-10.00") {
		t.Fatalf("details = %q, want no amount", details)
	}
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, i18n.Format(i18n.En, i18n.MessageTaxOverrideSet, i18n.Int(2)), "message")

	requireNoError(t, vm.SetTaxOverrideForSelection(ctx, database, nil, today))
	for _, id := range []model.TransactionID{a, b} {
		row := findRow(t, database, id)
		if row.IsTaxRelevant {
			t.Fatalf("transaction %d is tax relevant", id)
		}
		assertEqual(t, row.TaxSource, model.TaxSourceCategory, "tax source after reset")
		assertEqual(t, row.TaxOverride, (*bool)(nil), "tax override after reset")
	}
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids after reset")
}

func TestSetTaxOverrideForSelectionNeverTouchesABookingTheListDoesNotShow(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-13"), eur(-1_000), sp("One"), nil, cp(normal), false)
	hidden := txn(t, database, accountID, date(t, "2027-01-15"), eur(-9_900), sp("Far Future"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, ids(t, vm), []model.TransactionID{a}, "ids")
	vm.ToggleSelection(a)

	requireNoError(t, vm.SetTaxOverrideForSelection(ctx, database, bp(true), today))
	assertEqual(t, findRow(t, database, a).TaxSource, model.TaxSourceManual, "manual")
	assertEqual(t, findRow(t, database, hidden).TaxOverride, (*bool)(nil), "hidden override")
	entries, err := audit.Recent(ctx, database, 1000)
	requireNoError(t, err)
	taxEntries := 0
	for _, entry := range entries {
		if entry.Action == "transaction.set_tax_override" {
			taxEntries++
		}
	}
	assertEqual(t, taxEntries, 1, "tax audit entries")
	recent, err := audit.Recent(ctx, database, 1)
	requireNoError(t, err)
	if !strings.Contains(recent[0].Details, "count=1") {
		t.Fatalf("details = %q, want count=1", recent[0].Details)
	}
}

func TestSetTaxOverrideFromTheDetailsPathUpdatesTheItemTheStateShows(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_000), sp("One"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	var selected *model.TransactionItem
	for index := range vm.State().Transactions {
		if vm.State().Transactions[index].ID == a {
			selected = &vm.State().Transactions[index]
		}
	}
	if selected == nil {
		t.Fatal("booking not found")
	}
	chosen := *selected
	documents := documentsservice.NewService(database, ocr.WithoutOCR(), "unused-docs")
	requireNoError(t, vm.SelectTransaction(ctx, database, documents, &chosen))
	if vm.State().Selected == nil {
		t.Fatal("no selected transaction")
	}
	assertEqual(t, vm.State().Selected.ID, a, "selected id")

	requireNoError(t, vm.SetTaxOverride(ctx, database, a, bp(false), today))
	if vm.State().Selected == nil {
		t.Fatal("no selected transaction after change")
	}
	assertEqual(t, vm.State().Selected.ID, a, "selected id after change")
	if vm.State().Selected.IsTaxRelevant {
		t.Fatal("selected booking is tax relevant")
	}
	assertEqual(t, vm.State().Selected.TaxSource, model.TaxSourceManual, "tax source")
	assertEqual(t, vm.State().Selected.TaxOverride, bp(false), "tax override")
	recent, err := audit.Recent(ctx, database, 1)
	requireNoError(t, err)
	details := recent[0].Details
	if !strings.Contains(details, "count=1") {
		t.Fatalf("details = %q, want count=1", details)
	}
	if !strings.Contains(details, "false") {
		t.Fatalf("details = %q, want false", details)
	}
	if strings.Contains(details, "One") {
		t.Fatalf("details = %q, want no partner", details)
	}
}

func TestARuleFlagAloneChangesTheShownBookingsWithoutTouchingTheOverride(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_000), sp("Beispiel Shop"), nil, cp(normal), false)
	if _, err := rules.Create(ctx, database, model.NewRule{
		Pattern:     "Beispiel",
		CategoryID:  normal,
		Priority:    0,
		AutoApply:   true,
		MatchField:  model.RuleFieldAny,
		Direction:   model.RuleDirectionAny,
		TaxRelevant: bp(true),
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	var shown *model.TransactionItem
	for index := range vm.State().Transactions {
		if vm.State().Transactions[index].ID == a {
			shown = &vm.State().Transactions[index]
		}
	}
	if shown == nil {
		t.Fatal("booking not found")
	}
	if !shown.IsTaxRelevant {
		t.Fatal("booking is not tax relevant")
	}
	assertEqual(t, shown.TaxSource, model.TaxSourceRule, "tax source")
	assertEqual(t, shown.TaxRulePattern, sp("Beispiel"), "tax rule pattern")
	assertEqual(t, shown.TaxOverride, (*bool)(nil), "tax override")
}

// ---------------------------------------------------------------------------
// DashboardMonthTest.kt (transactions parts)
// ---------------------------------------------------------------------------

func dashboardMonthFixture(t *testing.T) (database *db.Database, accountID model.AccountID, food model.CategoryID) {
	t.Helper()
	database = testDatabase(t)
	accountID = createAccount(t, database, "DE89370400440532013000", 100_000, money.EUR, "checking")
	food = createCategory(t, database, "Food", false, false)
	tax := createCategory(t, database, "Donations", true, false)
	transfers := createCategory(t, database, "Own transfers", false, true)
	txn(t, database, accountID, date(t, "2026-03-05"), eur(-10_000), sp("MaerzJetzt"), nil, cp(tax), false)
	txn(t, database, accountID, date(t, "2026-08-02"), eur(-12_000), sp("August Nazar"), nil, cp(food), false)
	txn(t, database, accountID, date(t, "2026-08-03"), eur(200_000), sp("Employer"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-08-04"), eur(-20_000), sp("Erika Musterfrau"), nil, cp(transfers), false)
	txn(t, database, accountID, date(t, "2026-08-05"), eur(-1_500), nil, nil, nil, false)
	txn(t, database, accountID, date(t, "2026-09-04"), eur(-6_000), sp("September Nazar"), nil, cp(food), false)
	txn(t, database, accountID, date(t, "2026-09-06"), eur(-5_000), sp("September Spenden"), nil, cp(tax), false)
	txn(t, database, accountID, date(t, "2026-10-02"), eur(-4_000), sp("Oktober Nazar"), nil, cp(food), false)
	txn(t, database, accountID, date(t, "2026-10-03"), eur(150_000), sp("Employer"), nil, nil, false)
	return database, accountID, food
}

func TestAMonthNavigationListsExactlyThatMonthsBookingsAndAPickedPeriodEndsIt(t *testing.T) {
	database, _, food := dashboardMonthFixture(t)
	today := date(t, "2026-10-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{
		CategoryID: cp(food),
		Month:      mp(dashboard.NewYearMonth(2026, 8)),
	}, today))
	assertEqual(t, vm.State().Month, mp(dashboard.NewYearMonth(2026, 8)), "month")
	assertEqual(t, partners(t, vm), []string{"August Nazar"}, "partners")
	if vm.State().Error != nil {
		t.Fatalf("error = %q, want none", *vm.State().Error)
	}

	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodThisMonth, today))
	if vm.State().Month != nil {
		t.Fatal("month is set")
	}
	assertEqual(t, partners(t, vm), []string{"Oktober Nazar"}, "partners after period")
}

// navigationCategory reproduces crate::app::dashboard::NavigationArgs::category,
// whose module has no Go port yet. The current month is a period, a past month
// is the exact one.
func navigationCategory(selected dashboard.YearMonth, current dashboard.YearMonth, categoryID model.CategoryID) StartArgs {
	args := StartArgs{CategoryID: cp(categoryID)}
	if selected != current {
		args.Month = mp(selected)
	} else {
		args.Period = pp(PeriodThisMonth)
	}
	return args
}

// navigationUncategorized reproduces
// crate::app::dashboard::NavigationArgs::uncategorized.
func navigationUncategorized(selected dashboard.YearMonth, current dashboard.YearMonth) StartArgs {
	args := StartArgs{UncategorizedOnly: true}
	if selected != current {
		args.Month = mp(selected)
	} else {
		args.Period = pp(PeriodThisMonth)
	}
	return args
}

func TestTheCurrentMonthsCategoryLinkListsOnlyThatMonthsBookings(t *testing.T) {
	database, _, food := dashboardMonthFixture(t)
	today := date(t, "2026-10-15")
	current := dashboard.YearMonthFromDate(today)
	vm := view()
	requireNoError(t, vm.Start(ctx, database, navigationCategory(current, current, food), today))

	assertEqual(t, vm.State().Period, PeriodThisMonth, "period")
	if vm.State().Month != nil {
		t.Fatal("the current month is a period, not an exact month")
	}
	assertEqual(t, partners(t, vm), []string{"Oktober Nazar"}, "August's booking stays out of the current month's list")
}

func TestAPastMonthsCategoryAndUncategorizedLinksListExactlyThatMonthsBookings(t *testing.T) {
	database, _, food := dashboardMonthFixture(t)
	today := date(t, "2026-10-15")
	current := dashboard.YearMonthFromDate(today)
	august := dashboard.NewYearMonth(2026, 8)

	vm := view()
	requireNoError(t, vm.Start(ctx, database, navigationCategory(august, current, food), today))
	assertEqual(t, vm.State().Month, mp(august), "month")
	assertEqual(t, partners(t, vm), []string{"August Nazar"}, "partners")

	uncategorizedView := view()
	requireNoError(t, uncategorizedView.Start(ctx, database, navigationUncategorized(august, current), today))
	assertEqual(t, uncategorizedView.State().Month, mp(august), "uncategorized month")
	assertEqual(t, uncategorizedView.State().CategoryFilter, NoneCategoryFilter(), "category filter")
	assertEqual(t, len(uncategorizedView.State().Transactions), 2, "August's two uncategorized bookings")
	for _, transaction := range uncategorizedView.State().Transactions {
		if transaction.Date.Year() != 2026 || transaction.Date.Month() != time.August {
			t.Fatalf("transaction %d is dated %s", transaction.ID, transaction.Date.String())
		}
	}
}

func TestWithAMonthSetAndOneAccountTheBalanceChartCoversOnlyThatMonth(t *testing.T) {
	database, accountID, _ := dashboardMonthFixture(t)
	today := date(t, "2026-10-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{
		AccountID: ap(accountID),
		Month:     mp(dashboard.NewYearMonth(2026, 8)),
	}, today))
	chart := vm.State().BalanceChart
	if chart == nil {
		t.Fatal("no chart")
	}
	assertEqual(t, chart[0].Date, date(t, "2026-08-01"), "first day")
	assertEqual(t, chart[len(chart)-1].Date, date(t, "2026-08-31"), "last day")
	assertEqual(t, len(chart), 31, "length")
}

// ---------------------------------------------------------------------------
// MultiDeleteTest.kt
// ---------------------------------------------------------------------------

func TestToggleSelectionTwiceRemovesAgainAndSetsTheAnchor(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	one := txn(t, database, accountID, date(t, "2026-06-13"), eur(-1_000), sp("One"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(one)
	assertEqual(t, vm.State().SelectedIDs, idSet(one), "selected ids")
	assertEqual(t, vm.State().SelectionAnchorID, tp(one), "anchor")
	vm.ToggleSelection(one)
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("selected ids are not empty")
	}
	assertEqual(t, vm.State().SelectionAnchorID, tp(one), "anchor after untoggle")
}

func TestSelectRangeSelectsTheShownRowsBetweenAnchorAndTarget(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	one := txn(t, database, accountID, date(t, "2026-06-11"), eur(-100), sp("One"), nil, nil, false)
	two := txn(t, database, accountID, date(t, "2026-06-12"), eur(-200), sp("Two"), nil, nil, false)
	three := txn(t, database, accountID, date(t, "2026-06-13"), eur(-300), sp("Three"), nil, nil, false)
	four := txn(t, database, accountID, date(t, "2026-06-14"), eur(-400), sp("Four"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	assertEqual(t, ids(t, vm), []model.TransactionID{four, three, two, one}, "ids")

	vm.ToggleSelection(two)
	vm.SelectRange(four)
	assertEqual(t, vm.State().SelectedIDs, idSet(two, three, four), "selected ids")
	assertEqual(t, vm.State().SelectionAnchorID, tp(four), "anchor")

	other := view()
	startAll(t, database, other, today)
	other.ToggleSelection(three)
	other.SelectRange(one)
	assertEqual(t, other.State().SelectedIDs, idSet(one, two, three), "selected ids after second range")
}

func TestSelectRangeWithoutAnAnchorBehavesLikeAToggle(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	one := txn(t, database, accountID, date(t, "2026-06-14"), eur(-100), sp("One"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.SelectAllShown()
	if vm.State().SelectionAnchorID != nil {
		t.Fatal("anchor is set")
	}
	vm.SelectRange(one)
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("selected ids are not empty")
	}
	assertEqual(t, vm.State().SelectionAnchorID, tp(one), "anchor")
}

func TestSelectAllShownSelectsOnlyWhatTheCurrentFilterShows(t *testing.T) {
	database := testDatabase(t)
	groceries := createCategory(t, database, "Groceries", false, false)
	loans := createCategory(t, database, "Loans", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	inCategory := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_000), sp("InCategory"), nil, cp(groceries), false)
	otherCategory := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("OtherCategory"), nil, cp(loans), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, OnlyCategoryFilter(groceries), today))
	assertEqual(t, ids(t, vm), []model.TransactionID{inCategory}, "ids")
	vm.SelectAllShown()
	assertEqual(t, vm.State().SelectedIDs, idSet(inCategory), "selected ids")
	if _, selected := vm.State().SelectedIDs[otherCategory]; selected {
		t.Fatal("other category is selected")
	}
}

func TestEveryFilterChangeClearsTheSelection(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	other := createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	txn(t, database, accountID, date(t, "2026-06-14"), eur(-100), sp("One"), nil, nil, false)
	txn(t, database, other, date(t, "2026-06-14"), eur(-200), sp("Two"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.SelectAllShown()
	if len(vm.State().SelectedIDs) == 0 {
		t.Fatal("nothing selected")
	}

	requireNoError(t, vm.UpdatePeriod(ctx, database, PeriodLastMonth, today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("period kept the selection")
	}

	vm.SelectAllShown()
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "One", today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("search kept the selection")
	}

	vm.SelectAllShown()
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionExpenses, today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("direction kept the selection")
	}

	vm.SelectAllShown()
	minimum := eur(500)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &minimum, nil, today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("amount range kept the selection")
	}

	vm.SelectAllShown()
	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, NoneCategoryFilter(), today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("category filter kept the selection")
	}

	vm.SelectAllShown()
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(accountID), today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("account selection kept the selection")
	}

	vm.SelectAllShown()
	requireNoError(t, vm.Start(ctx, database, StartArgs{Month: mp(dashboard.YearMonthFromDate(today))}, today))
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("start kept the selection")
	}
}

func TestDeleteSelectedDeletesExactlyTheConfirmedBookingsWithOneCountOnlyEntry(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-12"), eur(-1_000), sp("Secret Partner"), sp("Secret purpose"), nil, false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Other Partner"), nil, nil, false)
	c := txn(t, database, accountID, date(t, "2026-06-14"), eur(3_000), sp("Third"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(a)
	vm.ToggleSelection(b)
	requireNoError(t, vm.DeleteSelected(ctx, database, idSet(a, b), today))

	rows := idsFromDB(t, database)
	if _, found := rows[a]; found {
		t.Fatal("a was not deleted")
	}
	if _, found := rows[b]; found {
		t.Fatal("b was not deleted")
	}
	if _, found := rows[c]; !found {
		t.Fatal("c was deleted")
	}
	if len(vm.State().SelectedIDs) != 0 {
		t.Fatal("selected ids are not empty")
	}
	entries, err := audit.Recent(ctx, database, 1000)
	requireNoError(t, err)
	deleteEntries := make([]audit.Entry, 0)
	for _, entry := range entries {
		if entry.Action == "transaction.delete_all" {
			deleteEntries = append(deleteEntries, entry)
		}
	}
	assertEqual(t, len(deleteEntries), 1, "delete audit entries")
	details := deleteEntries[0].Details
	if !strings.Contains(details, "count=2") {
		t.Fatalf("details = %q, want count=2", details)
	}
	if strings.Contains(details, "Secret") {
		t.Fatalf("details = %q, want no partner", details)
	}
	if strings.Contains(details, "purpose") {
		t.Fatalf("details = %q, want no purpose", details)
	}
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, i18n.Format(i18n.En, i18n.MessageTransactionsDeleted, i18n.Int(2)), "message")
}

func idsFromDB(t *testing.T, database *db.Database) map[model.TransactionID]struct{} {
	t.Helper()
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	set := make(map[model.TransactionID]struct{}, len(items))
	for _, transaction := range items {
		set[transaction.ID] = struct{}{}
	}
	return set
}

func TestASelectedIDThatDisappearsOnRefreshIsDropped(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-12"), eur(-1_000), sp("One"), nil, nil, false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Two"), nil, nil, false)
	c := txn(t, database, accountID, date(t, "2026-06-14"), eur(-3_000), sp("Three"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	for _, id := range []model.TransactionID{a, b, c} {
		vm.ToggleSelection(id)
	}
	requireNoError(t, transactions.Delete(ctx, database, c))
	requireNoError(t, vm.Load(ctx, database, today))
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids")
}

func TestDeleteSelectedNeverDeletesAHiddenOrUnpassedBooking(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-12"), eur(-1_000), sp("Alpha"), nil, nil, false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Beta"), nil, nil, false)
	hidden := txn(t, database, accountID, date(t, "2026-06-14"), eur(-3_000), sp("Gamma"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "Alph", today))
	assertEqual(t, ids(t, vm), []model.TransactionID{a}, "ids")
	vm.ToggleSelection(a)
	requireNoError(t, vm.DeleteSelected(ctx, database, idSet(a, hidden), today))
	rows := idsFromDB(t, database)
	if _, found := rows[a]; found {
		t.Fatal("a was not deleted")
	}
	if _, found := rows[hidden]; !found {
		t.Fatal("hidden was deleted")
	}
	if _, found := rows[b]; !found {
		t.Fatal("b was deleted")
	}
}

func TestSelectedTotalsKeepCurrenciesSeparate(t *testing.T) {
	database := testDatabase(t)
	euroAccount := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	dollarAccount := createAccount(t, database, "DE44500105175407324931", 0, testCurrency(t, "USD"), "checking")
	txn(t, database, euroAccount, date(t, "2026-06-12"), eur(-1_000), sp("One"), nil, nil, false)
	txn(t, database, euroAccount, date(t, "2026-06-13"), eur(-250), sp("Two"), nil, nil, false)
	txn(t, database, dollarAccount, date(t, "2026-06-14"), testMoney(t, 500, "USD"), sp("USD Partner"), nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.SelectAllShown()
	assertEqual(t, len(vm.State().SelectedIDs), 3, "selected ids")
	totals := vm.State().SelectedTotals()
	assertEqual(t, len(totals), 2, "currencies")
	euroTotal := int64(0)
	dollarTotal := int64(0)
	for _, total := range totals {
		if total.Currency == money.EUR {
			euroTotal = total.Total
		}
		if total.Currency == testCurrency(t, "USD") {
			dollarTotal = total.Total
		}
	}
	assertEqual(t, euroTotal, int64(-1_250), "EUR total")
	assertEqual(t, dollarTotal, int64(500), "USD total")
}

// ---------------------------------------------------------------------------
// SetCategoryForSelectionTest.kt
// ---------------------------------------------------------------------------

func TestSetCategoryForSelectionChangesOnlySelectedAndShownBookings(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	office := createCategory(t, database, "Office Supplies", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-12"), eur(-1_000), sp("Secret Partner"), sp("Secret purpose"), cp(normal), false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Other Partner"), nil, cp(normal), false)
	c := txn(t, database, accountID, date(t, "2026-06-14"), eur(-3_000), sp("Third Partner"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(a)
	vm.ToggleSelection(b)
	requireNoError(t, vm.SetCategoryForSelection(ctx, database, cp(office), today))

	assertEqual(t, findRow(t, database, a).CategoryID, cp(office), "a category")
	assertEqual(t, findRow(t, database, b).CategoryID, cp(office), "b category")
	assertEqual(t, findRow(t, database, c).CategoryID, cp(normal), "c category")
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids")

	entries, err := audit.Recent(ctx, database, 1000)
	requireNoError(t, err)
	updateEntries := make([]audit.Entry, 0)
	for _, entry := range entries {
		if entry.Action == "transaction.update_categories" {
			updateEntries = append(updateEntries, entry)
		}
	}
	assertEqual(t, len(updateEntries), 1, "update audit entries")
	details := updateEntries[0].Details
	if !strings.Contains(details, "count=2") {
		t.Fatalf("details = %q, want count=2", details)
	}
	if strings.Contains(details, "Secret") {
		t.Fatalf("details = %q, want no partner", details)
	}
	if strings.Contains(details, "purpose") {
		t.Fatalf("details = %q, want no purpose", details)
	}
	if strings.Contains(details, "-10.00") {
		t.Fatalf("details = %q, want no amount", details)
	}
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, i18n.Format(i18n.En, i18n.MessageCategorySet, i18n.Int(2)), "message")
}

func TestSetCategoryForSelectionNeverTouchesABookingTheListDoesNotShow(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	office := createCategory(t, database, "Office Supplies", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-13"), eur(-1_000), sp("One"), nil, cp(normal), false)
	hidden := txn(t, database, accountID, date(t, "2027-01-15"), eur(-9_900), sp("Far Future"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, ids(t, vm), []model.TransactionID{a}, "ids")
	vm.ToggleSelection(a)
	requireNoError(t, vm.SetCategoryForSelection(ctx, database, cp(office), today))

	assertEqual(t, findRow(t, database, a).CategoryID, cp(office), "a category")
	assertEqual(t, findRow(t, database, hidden).CategoryID, cp(normal), "hidden category")
	recent, err := audit.Recent(ctx, database, 1)
	requireNoError(t, err)
	if !strings.Contains(recent[0].Details, "count=1") {
		t.Fatalf("details = %q, want count=1", recent[0].Details)
	}
}

func TestNullMakesTheSelectedBookingsUncategorized(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	a := txn(t, database, accountID, date(t, "2026-06-14"), eur(-1_000), sp("One"), nil, cp(normal), false)
	b := txn(t, database, accountID, date(t, "2026-06-13"), eur(-2_000), sp("Two"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(a)
	vm.ToggleSelection(b)
	requireNoError(t, vm.SetCategoryForSelection(ctx, database, nil, today))
	assertEqual(t, findRow(t, database, a).CategoryID, (*model.CategoryID)(nil), "a category")
	assertEqual(t, findRow(t, database, b).CategoryID, (*model.CategoryID)(nil), "b category")
	assertEqual(t, vm.State().SelectedIDs, idSet(a, b), "selected ids")
	// Repository audit details name the changed count only; no free text can
	// leak.
	recent, err := audit.Recent(ctx, database, 1)
	requireNoError(t, err)
	details := recent[0].Details
	if !strings.Contains(details, "count=2") {
		t.Fatalf("details = %q, want count=2", details)
	}
}

func TestBookingsAlreadyOnTheTargetCategoryAreNotCounted(t *testing.T) {
	database := testDatabase(t)
	normal := createCategory(t, database, "Groceries", false, false)
	office := createCategory(t, database, "Office Supplies", false, false)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	already := txn(t, database, accountID, date(t, "2026-06-13"), eur(-1_000), sp("One"), nil, cp(office), false)
	other := txn(t, database, accountID, date(t, "2026-06-14"), eur(-2_000), sp("Two"), nil, cp(normal), false)
	today := date(t, "2026-06-15")
	vm := view()
	startAll(t, database, vm, today)
	vm.ToggleSelection(already)
	vm.ToggleSelection(other)
	requireNoError(t, vm.SetCategoryForSelection(ctx, database, cp(office), today))
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	assertEqual(t, *vm.State().Message, i18n.Format(i18n.En, i18n.MessageCategorySet, i18n.Int(1)), "message")
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 1, "update audits")

	vm.ClearSelection()
	vm.ToggleSelection(already)
	requireNoError(t, vm.SetCategoryForSelection(ctx, database, cp(office), today))
	assertEqual(t, countAction(t, database, "transaction.update_categories"), 1, "update audits after second try")
	assertEqual(t, *vm.State().Message, i18n.Format(i18n.En, i18n.MessageCategorySet, i18n.Int(1)), "message after second try")
	assertEqual(t, findRow(t, database, already).CategoryID, cp(office), "already category")
}

// ---------------------------------------------------------------------------
// TransactionsUncategorizedLinkTest.kt
// ---------------------------------------------------------------------------

func TestTheUncategorizedLinkShowsBothAccountsAndClearsTheFilters(t *testing.T) {
	database := testDatabase(t)
	acc1 := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	acc2 := createAccount(t, database, "DE625 Casablanca 02011002 071903", 0, money.EUR, "checking")
	unc1 := txn(t, database, acc1, date(t, "2026-06-05"), eur(500), nil, nil, nil, false)
	unc2 := txn(t, database, acc2, date(t, "2026-06-07"), eur(700), nil, nil, nil, false)
	groceries := createCategory(t, database, "Groceries", false, false)
	txn(t, database, acc1, date(t, "2026-06-08"), eur(900), nil, nil, cp(groceries), false)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(acc1), today))
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "no such partner", today))
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionIncome, today))
	minimum := eur(1_000)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &minimum, nil, today))

	requireNoError(t, vm.Start(ctx, database, StartArgs{
		UncategorizedOnly: true,
		Period:            pp(PeriodThisMonth),
	}, today))
	state := vm.State()
	got := make(map[model.TransactionID]struct{}, len(state.Transactions))
	for _, transaction := range state.Transactions {
		got[transaction.ID] = struct{}{}
	}
	assertEqual(t, got, idSet(unc1, unc2), "uncategorized bookings")
	assertEqual(t, state.SearchQuery, "", "search query")
	assertEqual(t, state.Direction, model.DirectionAll, "direction")
	if state.MinAmount != nil {
		t.Fatal("min amount is set")
	}
	if state.MaxAmount != nil {
		t.Fatal("max amount is set")
	}
	assertEqual(t, state.Selection, accountpanel.AllSelection(), "selection")
	if state.AccountID != nil {
		t.Fatal("account id is set")
	}
	assertEqual(t, state.Period, PeriodThisMonth, "period")
}

func TestTheUncategorizedLinkWithAnExactMonthShowsThatMonthOnAllAccounts(t *testing.T) {
	database := testDatabase(t)
	acc1 := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	acc2 := createAccount(t, database, "DE625 Casablanca 02011002 071903", 0, money.EUR, "checking")
	may1 := txn(t, database, acc1, date(t, "2026-05-03"), eur(100), nil, nil, nil, false)
	may2 := txn(t, database, acc2, date(t, "2026-05-20"), eur(200), nil, nil, nil, false)
	txn(t, database, acc1, date(t, "2026-06-05"), eur(300), nil, nil, nil, false)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "no such partner", today))
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionIncome, today))
	minimum := eur(1_000)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &minimum, nil, today))

	requireNoError(t, vm.Start(ctx, database, StartArgs{
		UncategorizedOnly: true,
		Month:             mp(dashboard.NewYearMonth(2026, 5)),
	}, today))
	state := vm.State()
	got := make(map[model.TransactionID]struct{}, len(state.Transactions))
	for _, transaction := range state.Transactions {
		got[transaction.ID] = struct{}{}
	}
	assertEqual(t, got, idSet(may1, may2), "uncategorized bookings")
	assertEqual(t, state.SearchQuery, "", "search query")
	assertEqual(t, state.Direction, model.DirectionAll, "direction")
	if state.MinAmount != nil {
		t.Fatal("min amount is set")
	}
	if state.MaxAmount != nil {
		t.Fatal("max amount is set")
	}
	assertEqual(t, state.Selection, accountpanel.AllSelection(), "selection")
	if state.AccountID != nil {
		t.Fatal("account id is set")
	}
	assertEqual(t, state.Period, PeriodAll, "period")
}

func TestTheUncategorizedLinkIgnoresASavedPanelSelection(t *testing.T) {
	database := testDatabase(t)
	acc1 := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	acc2 := createAccount(t, database, "DE625 Casablanca 02011002 071903", 0, money.EUR, "checking")
	unc1 := txn(t, database, acc1, date(t, "2026-06-05"), eur(500), nil, nil, nil, false)
	unc2 := txn(t, database, acc2, date(t, "2026-06-07"), eur(700), nil, nil, nil, false)
	today := date(t, "2026-06-15")
	previous := view()
	requireNoError(t, previous.Load(ctx, database, today))
	requireNoError(t, previous.SelectAccounts(ctx, database, accountpanel.AccountSelectionFor(acc1), today))

	vm := view()
	requireNoError(t, vm.Start(ctx, database, StartArgs{}, today))
	assertEqual(t, vm.State().Selection, accountpanel.AccountSelectionFor(acc1), "restored selection")
	requireNoError(t, vm.Start(ctx, database, StartArgs{
		UncategorizedOnly: true,
		Period:            pp(PeriodThisMonth),
	}, today))
	state := vm.State()
	got := make(map[model.TransactionID]struct{}, len(state.Transactions))
	for _, transaction := range state.Transactions {
		got[transaction.ID] = struct{}{}
	}
	assertEqual(t, got, idSet(unc1, unc2), "uncategorized bookings")
	assertEqual(t, state.Selection, accountpanel.AllSelection(), "selection")
	if state.AccountID != nil {
		t.Fatal("account id is set")
	}
}

// ---------------------------------------------------------------------------
// TransactionsStartCategoryTest.kt
// ---------------------------------------------------------------------------

func TestStartWithACategorySetsTheFiltersAndClearsAPreviousSearch(t *testing.T) {
	database := testDatabase(t)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.UpdateSearchQuery(ctx, database, "Erika Mustermann", today))
	requireNoError(t, vm.Start(ctx, database, StartArgs{
		Period:     pp(PeriodThisMonth),
		CategoryID: cp(model.CategoryID(7)),
	}, today))
	state := vm.State()
	assertEqual(t, state.SearchQuery, "", "search query")
	assertEqual(t, state.CategoryFilter, OnlyCategoryFilter(model.CategoryID(7)), "category filter")
	assertEqual(t, state.Period, PeriodThisMonth, "period")
	assertEqual(t, state.Direction, model.DirectionAll, "direction")
	assertEqual(t, state.Selection, accountpanel.AllSelection(), "selection")
	if state.AccountID != nil {
		t.Fatal("account id is set")
	}
	if state.MinAmount != nil {
		t.Fatal("min amount is set")
	}
	if state.MaxAmount != nil {
		t.Fatal("max amount is set")
	}
}

func TestACategoryLinkClearsADirectionFilterAndAnAmountRange(t *testing.T) {
	database := testDatabase(t)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.UpdateDirection(ctx, database, model.DirectionIncome, today))
	minimum := eur(1_200)
	maximum := eur(10_000)
	requireNoError(t, vm.UpdateAmountRange(ctx, database, &minimum, &maximum, today))
	requireNoError(t, vm.UpdateCategoryFilter(ctx, database, NoneCategoryFilter(), today))
	requireNoError(t, vm.Start(ctx, database, StartArgs{CategoryID: cp(model.CategoryID(3))}, today))
	state := vm.State()
	assertEqual(t, state.CategoryFilter, OnlyCategoryFilter(model.CategoryID(3)), "category filter")
	assertEqual(t, state.Direction, model.DirectionAll, "direction")
	if state.MinAmount != nil {
		t.Fatal("min amount is set")
	}
	if state.MaxAmount != nil {
		t.Fatal("max amount is set")
	}
	assertEqual(t, state.Period, PeriodAll, "period")
}

func TestUncategorizedOnlyTogetherWithAPeriodUsesThatPeriodInsteadOfAll(t *testing.T) {
	database := testDatabase(t)
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.Start(ctx, database, StartArgs{
		UncategorizedOnly: true,
		Period:            pp(PeriodThisMonth),
	}, today))
	assertEqual(t, vm.State().CategoryFilter, NoneCategoryFilter(), "category filter")
	assertEqual(t, vm.State().Period, PeriodThisMonth, "period")
}
