package transactionsview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

// ---------------------------------------------------------------------------
// MultiFileImportTest.kt
// ---------------------------------------------------------------------------

func TestTwoFilesArePreviewedInBookingOrderAndImportedWithTheNewestBalance(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	accountID := createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	older := multiCSV(t, dir, "october.csv", &ibanText, []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00"),
		importRow(t, "15.10.2025", "Obststand", "Obst", "50,00", "-30,00"),
	})
	newer := multiCSV(t, dir, "november.csv", &ibanText, []string{
		importRow(t, "05.11.2025", "Arbeitgeber", "Gehalt", "150,00", "100,00"),
		importRow(t, "20.11.2025", "Kino", "Tickets", "140,00", "-10,00"),
	})
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{newer, older}, nil, today))
	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, len(preview.Entries), 2, "entries")
	assertEqual(t, preview.Entries[0].File, older, "imported oldest first")
	assertEqual(t, preview.Entries[1].File, newer, "newest last")
	assertEqual(t, preview.Newest().File, newer, "newest")
	assertEqual(t, preview.BookingCount(), 4, "booking count")
	assertEqual(t, preview.AccountID, ap(accountID), "account")
	if !preview.BalanceUpdateAllowed {
		t.Fatal("balance update is not allowed")
	}
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	assertEqual(t, len(items), 0, "nothing is stored before confirming")

	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	if vm.State().ImportPreview != nil {
		t.Fatal("preview is still open")
	}
	items, err = transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	assertEqual(t, len(items), 4, "imported bookings")
	account, ok, err := accounts.Get(ctx, database, accountID)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(14_000), "balance")
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	want := i18n.Format(i18n.En, i18n.ImportSummaryFiles, i18n.Int(4), i18n.Int(2))
	if !strings.Contains(*vm.State().Message, want) {
		t.Fatalf("message = %q, want it to contain %q", *vm.State().Message, want)
	}
}

func TestImportingTheSameTwoFilesAgainStoresNoDuplicates(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	older := multiCSV(t, dir, "october.csv", &ibanText, []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00"),
	})
	newer := multiCSV(t, dir, "november.csv", &ibanText, []string{
		importRow(t, "20.11.2025", "Kino", "Tickets", "140,00", "-10,00"),
	})
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{older, newer}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	assertEqual(t, len(items), 2, "imported bookings")

	requireNoError(t, vm.OpenImport(ctx, database, []string{newer, older}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	items, err = transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	assertEqual(t, len(items), 2, "no duplicates after re-import")
	if vm.State().Message == nil {
		t.Fatal("no message")
	}
	want := i18n.Format(i18n.En, i18n.ImportSummaryKnown, i18n.Int(2))
	if !strings.Contains(*vm.State().Message, want) {
		t.Fatalf("message = %q, want it to contain %q", *vm.State().Message, want)
	}
}

func TestTheBalanceComesOnlyFromTheNewestFileAndOnlyWhenUpdateBalanceIsTrue(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	accountID := createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	older := multiCSV(t, dir, "october.csv", &ibanText, []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "50,00", "-20,00"),
	})
	newer := multiCSV(t, dir, "november.csv", &ibanText, []string{
		importRow(t, "20.11.2025", "Kino", "Tickets", "140,00", "-10,00"),
	})
	requireNoError(t, accounts.SetBalance(ctx, database, accountID, eur(100_000), time.Now().UTC()))
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{older, newer}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, false, today))
	account, ok, err := accounts.Get(ctx, database, accountID)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(100_000), "balance after import without update")

	olderAgain := multiCSV(t, dir, "october2.csv", &ibanText, []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "50,00", "-20,00"),
	})
	requireNoError(t, vm.OpenImport(ctx, database, []string{newer, olderAgain}, nil, today))
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	account, ok, err = accounts.Get(ctx, database, accountID)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(14_000), "balance after import with update")
}

func TestAGoodFileNextToAnUnreadableOneStillImports(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	good := multiCSV(t, dir, "good.csv", &ibanText, []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00"),
	})
	ghost := filepath.Join(dir, "ghost.csv")
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{good, ghost}, nil, today))
	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, preview.Files, []string{good, ghost}, "files")
	assertEqual(t, len(preview.Entries), 1, "entries")
	assertEqual(t, len(preview.Failures), 1, "failures")
	assertEqual(t, preview.Failures[0].FileName, "ghost.csv", "failed file name")
	assertEqual(t, preview.BookingCount(), 1, "booking count")
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	count := 0
	for _, transaction := range items {
		if transaction.PartnerName != nil && *transaction.PartnerName == "Stadtwerke" {
			count++
		}
	}
	assertEqual(t, count, 1, "imported booking")
	if vm.State().Error != nil {
		t.Fatalf("error = %q, want none", *vm.State().Error)
	}
}

func TestAllFilesUnreadableSetsAnErrorAndNoPreview(t *testing.T) {
	database := testDatabase(t)
	dir := t.TempDir()
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{
		filepath.Join(dir, "ghost1.csv"),
		filepath.Join(dir, "ghost2.csv"),
	}, nil, today))
	if vm.State().ImportPreview != nil {
		t.Fatal("a preview was opened")
	}
	want := i18n.Format(
		i18n.En,
		i18n.ErrorImportNoReadableFile,
		i18n.Str("ghost1.csv: File not found: ghost1.csv, ghost2.csv: File not found: ghost2.csv"),
	)
	if vm.State().Error == nil {
		t.Fatal("no error")
	}
	assertEqual(t, *vm.State().Error, want, "error")
}

func TestFilesForDifferentAccountsAreRefused(t *testing.T) {
	database := testDatabase(t)
	createAccount(t, database, "DE44500105175407324931", 0, money.EUR, "checking")
	createAccount(t, database, "DE02120300000000202051", 0, money.EUR, "checking")
	dir := t.TempDir()
	one := multiCSV(t, dir, "one.csv", sp("DE44500105175407324931"), []string{
		importRow(t, "02.10.2025", "Stadtwerke", "Strom", "80,00", "-20,00"),
	})
	two := multiCSV(t, dir, "two.csv", sp("DE02120300000000202051"), []string{
		importRow(t, "02.11.2025", "Kino", "Tickets", "90,00", "-10,00"),
	})
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{one, two}, nil, today))
	if vm.State().ImportPreview != nil {
		t.Fatal("a preview was opened")
	}
	if vm.State().Error == nil {
		t.Fatal("no error")
	}
	message := *vm.State().Error
	if !strings.Contains(message, "…4931") || !strings.Contains(message, "…2051") {
		t.Fatalf("error = %q, want both masked IBANs", message)
	}
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	if len(items) != 0 {
		t.Fatal("bookings were stored")
	}
}

func TestTooManyFilesSetsTheLimitErrorWithoutReadingAnyFile(t *testing.T) {
	database := testDatabase(t)
	dir := t.TempDir()
	files := make([]string, 0, 241)
	for index := 1; index <= 241; index++ {
		files = append(files, filepath.Join(dir, fmt.Sprintf("missing-%d.csv", index)))
	}
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, files, nil, today))
	if vm.State().ImportPreview != nil {
		t.Fatal("a preview was opened")
	}
	want := i18n.Format(i18n.En, i18n.ErrorImportTooManyFiles, i18n.Int(MaxImportFiles))
	if vm.State().Error == nil {
		t.Fatal("no error")
	}
	assertEqual(t, *vm.State().Error, want, "error")
}

func TestACardStatementWithoutAnIBANLinePicksTheSelectedCardAccount(t *testing.T) {
	database := testDatabase(t)
	card := createAccount(t, database, "CARD1234", 10_000, money.EUR, "credit_card")
	dir := t.TempDir()
	file := multiCSV(t, dir, "card.csv", nil, []string{
		importRow(t, "05.05.2026", "Restaurant", "Essen", "90,00", "-10,00"),
	})
	today := date(t, "2026-05-20")
	vm := view()
	startAccount(t, database, vm, card, today)
	requireNoError(t, vm.OpenImport(ctx, database, []string{file}, nil, today))
	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, preview.AccountID, ap(card), "account")
	if !preview.BalanceUpdateAllowed {
		t.Fatal("balance update is not allowed")
	}
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	account, ok, err := accounts.Get(ctx, database, card)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(9_000), "balance")
}

func intPointer(value int) *int { return &value }

func TestAColumnRemapReReadsOnlyTheFilesWithTheSameHeaderLayout(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	a1 := multiCSV(t, dir, "a1.csv", &ibanText, []string{
		importRow(t, "05.01.2026", "A1Partner", "A1Purpose", "100,00", "-10,00"),
	})
	a2 := multiCSV(t, dir, "a2.csv", &ibanText, []string{
		importRow(t, "05.02.2026", "A2Partner", "A2Purpose", "90,00", "-10,00"),
	})
	other := filepath.Join(dir, "other.csv")
	otherText := "IBAN;" + ibanText + "\n\n" +
		"Buchung;Betrag;Verwendungszweck;Empfänger;Saldo;Währung\n" +
		"05.03.2026;-10,00;OtherPurpose;OtherPartner;80,00;EUR\n"
	if err := os.WriteFile(other, []byte(otherText), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{a1, a2, other}, nil, today))
	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	files := make([]string, 0, len(preview.Entries))
	purposes := make([]string, 0, len(preview.Entries))
	for _, entry := range preview.Entries {
		files = append(files, entry.File)
		if len(entry.Statement.Bookings) == 0 || entry.Statement.Bookings[0].Purpose == nil {
			t.Fatal("statement booking without a purpose")
		}
		purposes = append(purposes, *entry.Statement.Bookings[0].Purpose)
	}
	assertEqual(t, files, []string{a1, a2, other}, "files")
	assertEqual(t, purposes, []string{"A1Purpose", "A2Purpose", "OtherPurpose"}, "purposes")

	requireNoError(t, vm.RemapImport(ctx, database, imports.CsvMapping{
		Date:    intPointer(0),
		Amount:  intPointer(7),
		Balance: intPointer(5),
		Partner: intPointer(2),
		Purpose: intPointer(3),
	}, today))
	remapped := vm.State().ImportPreview
	if remapped == nil {
		t.Fatal("no remapped preview")
	}
	remappedPurposes := make([]string, 0, len(remapped.Entries))
	for _, entry := range remapped.Entries {
		if len(entry.Statement.Bookings) == 0 || entry.Statement.Bookings[0].Purpose == nil {
			t.Fatal("statement booking without a purpose")
		}
		remappedPurposes = append(remappedPurposes, *entry.Statement.Bookings[0].Purpose)
	}
	assertEqual(
		t,
		remappedPurposes,
		[]string{"Lastschrift", "Lastschrift", "OtherPurpose"},
		"the other layout keeps its auto-detected mapping",
	)
}

func TestOrderingPutsAnEmptyFileFirstAndTheQuarterFileNewest(t *testing.T) {
	database := testDatabase(t)
	ibanText := "DE44500105175407324931"
	accountID := createAccount(t, database, ibanText, 0, money.EUR, "checking")
	dir := t.TempDir()
	quarter := multiCSV(t, dir, "quarter.csv", &ibanText, []string{
		importRow(t, "10.01.2026", "QShop", "Q", "290,00", "-10,00"),
		importRow(t, "10.02.2026", "QShop", "Q", "280,00", "-10,00"),
		importRow(t, "10.03.2026", "QLast", "Q", "270,00", "-10,00"),
	})
	january := multiCSV(t, dir, "january.csv", &ibanText, []string{
		importRow(t, "20.01.2026", "JanShop", "J", "95,00", "-5,00"),
	})
	empty := multiCSV(t, dir, "empty.csv", &ibanText, nil)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.OpenImport(ctx, database, []string{january, quarter, empty}, nil, today))
	preview := vm.State().ImportPreview
	if preview == nil {
		t.Fatal("no preview")
	}
	assertEqual(t, preview.Entries[0].File, empty, "an empty file imports nothing, comes first")
	assertEqual(t, preview.Entries[1].File, january, "january second")
	assertEqual(t, preview.Newest().File, quarter, "quarter newest")
	if !preview.BalanceUpdateAllowed {
		t.Fatal("balance update is not allowed")
	}
	requireNoError(t, vm.ConfirmImport(ctx, database, true, today))
	account, ok, err := accounts.Get(ctx, database, accountID)
	requireNoError(t, err)
	if !ok {
		t.Fatal("account not found")
	}
	assertEqual(t, account.Balance, eur(27_000), "balance")
}

// TestSettingKeysAndStoredTextsArePinned pins the three settings keys and the
// chart's stored texts to FinDesk's literals.
func TestSettingKeysAndStoredTextsArePinned(t *testing.T) {
	assertEqual(t, SelectionKey, "transactions.selection", "selection key")
	assertEqual(t, CollapsedKey, "accountGroups.collapsed", "collapsed key")
	assertEqual(t, ChartCollapsedKey, "transactions.chartCollapsed", "chart key")

	database := testDatabase(t)
	today := date(t, "2026-03-20")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	requireNoError(t, vm.ToggleChart(ctx, database))
	stored, ok, err := settings.Get(ctx, database, "transactions.chartCollapsed")
	requireNoError(t, err)
	if !ok {
		t.Fatal("chart setting was not stored")
	}
	assertEqual(t, stored, "true", "collapsed chart text")

	requireNoError(t, vm.ToggleChart(ctx, database))
	stored, ok, err = settings.Get(ctx, database, "transactions.chartCollapsed")
	requireNoError(t, err)
	if !ok {
		t.Fatal("chart setting was not stored")
	}
	assertEqual(t, stored, "false", "expanded chart text")

	requireNoError(t, settings.Set(ctx, database, "transactions.chartCollapsed", "yes"))
	restored := view()
	requireNoError(t, restored.Load(ctx, database, today))
	if restored.State().ChartCollapsed {
		t.Fatal("an unknown stored text loads as expanded")
	}
}

// ---------------------------------------------------------------------------
// MultiCurrencyTest.kt (transactions part)
// ---------------------------------------------------------------------------

func TestEmptyTotalsFallBackToThePrimaryCurrency(t *testing.T) {
	database := testDatabase(t)
	requireNoError(t, settings.Set(ctx, database, money.PrimaryCurrencyKey, "USD"))
	today := date(t, "2026-06-15")
	vm := view()
	requireNoError(t, vm.Load(ctx, database, today))
	assertEqual(t, vm.State().PrimaryCurrency, testCurrency(t, "USD"), "primary currency")
}

// ---------------------------------------------------------------------------
// RuleDirectionImportTest.kt (view model part)
// ---------------------------------------------------------------------------

func TestAutoCategorizeViaTheViewModelMatchesOnlyTheExpense(t *testing.T) {
	database := testDatabase(t)
	accountID := createAccount(t, database, "DE89370400440532013000", 0, money.EUR, "checking")
	groceries := createCategory(t, database, "Groceries", false, false)
	if _, err := rules.Create(ctx, database, model.NewRule{
		Pattern:    "Erika Mustermann",
		CategoryID: groceries,
		Priority:   5,
		AutoApply:  true,
		MatchField: model.RuleFieldPartner,
		Direction:  model.RuleDirectionExpense,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	txn(t, database, accountID, date(t, "2026-10-03"), eur(-4_567), sp("Erika Mustermann"), nil, nil, false)
	txn(t, database, accountID, date(t, "2026-10-03"), eur(100_000), sp("Erika Mustermann"), nil, nil, false)
	today := date(t, "2026-10-15")
	vm := view()
	requireNoError(t, vm.AutoCategorize(ctx, database, today))
	filter := model.TransactionFilter{Direction: model.DirectionAll}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	requireNoError(t, err)
	rows := make(map[int]*model.CategoryID)
	for _, transaction := range items {
		sign := 1
		if transaction.Amount.Minor < 0 {
			sign = -1
		}
		rows[sign] = transaction.CategoryID
	}
	assertEqual(t, rows[-1], cp(groceries), "the expense gets the rule's category")
	assertEqual(t, rows[1], (*model.CategoryID)(nil), "the income does not match an EXPENSE-only rule")
}
