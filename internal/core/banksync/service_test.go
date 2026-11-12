package banksync

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/bankconnections"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	txrepo "github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

func TestFirstSyncCreatesAccountsSetsBalancesImportsAndCategorizes(t *testing.T) {
	h := newHarness(t)
	food := category(t, h.db, "Groceries")
	rule(t, h.db, "rewe", food)
	const giro = "DE89370400440532013000"
	const savings = "DE44500105175407324931"
	h.gateway.setSnapshots([]AccountSnapshot{
		snapshot(giro, "1234.56", []FinTsTransaction{
			booking(giro, h.today.AddDays(-3), "-4.50", stringPointer("REWE"), stringPointer("Kaffee"), stringPointer("1239.06")),
			booking(giro, h.today.AddDays(-3), "-4.50", stringPointer("REWE"), stringPointer("Kaffee"), stringPointer("1234.56")),
		}),
		snapshot(savings, "10000.00", nil),
	})
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	report, err := h.sync(t, interaction)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	assertInt(t, report.AccountsSynced, 2)
	assertInt(t, report.AccountsAdded, 2)
	assertInt(t, report.Imported, 2)
	assertInt(t, report.Categorized, 2)
	if got := interaction.pinAskedCount(); got != 1 {
		t.Fatalf("pin asked = %d, want 1 (one dialog, one PIN prompt)", got)
	}
	if got := interaction.offeredRememberValues(); !reflect.DeepEqual(got, []bool{true}) {
		t.Fatalf("offered remember = %v, want [true]", got)
	}
	assertDate(t, h.gateway.since(giro), h.today.AddDays(-90))

	loaded := h.accounts(t)
	giroAccount := findAccount(t, loaded, giro)
	if giroAccount.Balance.Minor != 123_456 {
		t.Fatalf("balance = %d, want %d", giroAccount.Balance.Minor, 123_456)
	}
	categorized := 0
	for _, transaction := range transactionsForAccount(t, h.db, giroAccount.ID) {
		if transaction.CategoryID != nil && *transaction.CategoryID == food {
			categorized++
		}
	}
	assertInt(t, categorized, 2)
	bank := loadBankItem(t, h.db, h.bank)
	if bank.LastSync == nil {
		t.Fatal("last sync is nil")
	}
	if bank.TanMethod == nil || *bank.TanMethod != "921" {
		t.Fatalf("tan method = %v, want 921", bank.TanMethod)
	}
	h.assertAuditHasNo(t, "12345", "999999", "demo-user", giro)
}

func TestTheFirstImportIntoAnEmptyAccountMarksNoBookingNew(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "980.00", []FinTsTransaction{
		booking(giro, h.today.AddDays(-3), "-20.00", stringPointer("Shop"), stringPointer("A"), stringPointer("980.00")),
	})})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	account := h.accounts(t)[0]
	for _, transaction := range transactionsForAccount(t, h.db, account.ID) {
		if transaction.IsNew {
			t.Fatal("the first import marked a booking new")
		}
	}
	counts, err := txrepo.CountNew(context.Background(), h.db)
	if err != nil {
		t.Fatalf("transactions.CountNew: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("count new = %v, want none", counts)
	}
}

func TestASecondImportMarksExactlyTheNewBookingsAndNeverReFlagsDuplicates(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	existing := booking(giro, h.today.AddDays(-3), "-20.00", stringPointer("Shop"), stringPointer("A"), stringPointer("980.00"))
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "980.00", []FinTsTransaction{existing})})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "945.00", []FinTsTransaction{
		existing,
		booking(giro, h.today.AddDays(-1), "-5.00", stringPointer("Shop"), stringPointer("B"), stringPointer("975.00")),
		booking(giro, h.today, "-30.00", stringPointer("Shop"), stringPointer("C"), stringPointer("945.00")),
	})})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertInt(t, report.Imported, 2)
	assertInt(t, report.Duplicates, 1)
	accountID := h.accounts(t)[0].ID
	counts, err := txrepo.CountNew(context.Background(), h.db)
	if err != nil {
		t.Fatalf("transactions.CountNew: %v", err)
	}
	if len(counts) != 1 || counts[accountID] != 2 {
		t.Fatalf("count new = %v, want map[%d:2]", counts, accountID)
	}
	purposes := make([]string, 0)
	for _, transaction := range transactionsForAccount(t, h.db, accountID) {
		if transaction.IsNew && transaction.Purpose != nil {
			purposes = append(purposes, *transaction.Purpose)
		}
	}
	if len(purposes) != 2 {
		t.Fatalf("new purposes = %v", purposes)
	}
	if !containsText(strings.Join(purposes, "\n"), "B") || !containsText(strings.Join(purposes, "\n"), "C") {
		t.Fatalf("new purposes = %v, want B and C", purposes)
	}
}

func TestSameDayBookingsKeepTheirRunningBalancesInOrder(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "40.00", []FinTsTransaction{
		booking(giro, h.today, "-10.00", stringPointer("Shop"), stringPointer("A"), stringPointer("90.00")),
		booking(giro, h.today, "-20.00", stringPointer("Shop"), stringPointer("B"), stringPointer("70.00")),
		booking(giro, h.today, "-30.00", stringPointer("Shop"), stringPointer("C"), stringPointer("40.00")),
	})})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	accountID := h.accounts(t)[0].ID
	balances := make([]int64, 0)
	for _, transaction := range transactionsForAccount(t, h.db, accountID) {
		if transaction.BalanceAfter == nil {
			t.Fatal("balance after is nil")
		}
		balances = append(balances, transaction.BalanceAfter.Minor)
	}
	if want := []int64{4_000, 7_000, 9_000}; !reflect.DeepEqual(balances, want) {
		t.Fatalf("balances = %v, want %v", balances, want)
	}
}

func TestSyncStoresTheLoginsSupportedTransactions(t *testing.T) {
	h := newHarness(t)
	capabilities := BankCapabilities{Jobs: map[string]struct{}{
		JobBalance:         {},
		JobStatementsMT940: {},
	}}
	h.gateway.setCapabilities(&capabilities)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := ParseCapabilities(loadBankItem(t, h.db, h.bank).Capabilities)
	if stored == nil {
		t.Fatal("capabilities = nil")
	}
	if !stored.SupportsBalance() {
		t.Fatal("stored capabilities do not support balance")
	}
	if !stored.SupportsStatements() {
		t.Fatal("stored capabilities do not support statements")
	}
}

func TestSecondSyncOverlapsTheLastOneAndSkipsKnownBookings(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	first := booking(giro, h.today.AddDays(-3), "-20.00", stringPointer("Shop"), stringPointer("A"), stringPointer("980.00"))
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "980.00", []FinTsTransaction{first})})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	setLastSync(t, h.db, h.bank, time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC))
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "950.00", []FinTsTransaction{
		first,
		booking(giro, h.today, "-30.00", stringPointer("Shop"), stringPointer("B"), stringPointer("950.00")),
	})})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertDate(t, h.gateway.since(giro), mustDate(t, 2026, time.September, 19))
	assertInt(t, report.Imported, 1)
	assertInt(t, report.Duplicates, 1)
	assertInt(t, report.AccountsAdded, 0)
	if got := h.accounts(t)[0].Balance.Minor; got != 95_000 {
		t.Fatalf("balance = %d, want %d", got, 95_000)
	}
}

func TestFailedStatementsKeepTheOldSyncDateSoTheWindowIsFetchedAgain(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	previous := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	setLastSync(t, h.db, h.bank, previous)

	failed := snapshot(giro, "100.00", nil)
	statementError := "9010 Auftrag abgelehnt"
	failed.StatementError = &statementError
	h.gateway.setSnapshots([]AccountSnapshot{failed})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if want := []string{"3000: 9010 Auftrag abgelehnt"}; !reflect.DeepEqual(report.Warnings, want) {
		t.Fatalf("warnings = %v, want %v", report.Warnings, want)
	}
	bank := loadBankItem(t, h.db, h.bank)
	if bank.LastSync == nil || !bank.LastSync.Equal(previous) {
		t.Fatalf("last sync = %v, want %v", bank.LastSync, previous)
	}
}

func TestAChosenHistoryStartReachesKnownAndNewAccountsAndIsClearedAfterSuccess(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	const savings = "DE44500105175407324931"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	from := h.today.AddDays(-400)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}
	h.gateway.setSnapshots([]AccountSnapshot{
		snapshot(giro, "95.00", []FinTsTransaction{
			booking(giro, from.AddDays(1), "-5.00", stringPointer("Old Shop"), stringPointer("A"), stringPointer("95.00")),
		}),
		snapshot(savings, "200.00", []FinTsTransaction{
			booking(savings, from.AddDays(2), "-2.00", stringPointer("Old Shop"), stringPointer("B"), stringPointer("198.00")),
		}),
	})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertDate(t, h.gateway.since(giro), from)
	assertDate(t, h.gateway.since(savings), from)
	if report.HistoryFrom == nil || *report.HistoryFrom != from {
		t.Fatalf("history from = %v, want %s", report.HistoryFrom, from)
	}
	assertInt(t, report.Imported, 2)
	if bank := loadBankItem(t, h.db, h.bank); bank.SyncFrom != nil {
		t.Fatalf("sync from = %v, want nil", bank.SyncFrom)
	}
}

func TestAChosenHistoryStartAfterTheLastSyncNeverNarrowsTheWindow(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	setLastSync(t, h.db, h.bank, time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC))
	from := h.today.AddDays(-1)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertDate(t, h.gateway.since(giro), mustDate(t, 2026, time.August, 25))
}

func TestReFetchingBookingsThatWereImportedBeforeImportsNoneOfThem(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	existing := booking(giro, h.today.AddDays(-3), "-20.00", stringPointer("Shop"), stringPointer("A"), stringPointer("980.00"))
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "980.00", []FinTsTransaction{existing})})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	from := h.today.AddDays(-400)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "980.00", []FinTsTransaction{existing})})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertInt(t, report.Imported, 0)
	assertInt(t, report.Duplicates, 1)
}

func TestAWarningAnErrorOrACancelKeepsTheChosenHistoryStartForARetry(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	from := h.today.AddDays(-400)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}

	withWarning := snapshot(giro, "100.00", nil)
	statementError := "9010 Auftrag abgelehnt"
	withWarning.StatementError = &statementError
	h.gateway.setSnapshots([]AccountSnapshot{withWarning})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("warning sync: %v", err)
	}
	if bank := loadBankItem(t, h.db, h.bank); bank.SyncFrom == nil || *bank.SyncFrom != from {
		t.Fatalf("sync from after warning = %v, want %s", bank.SyncFrom, from)
	}

	h.gateway.setSnapshots(nil)
	failure := NewGatewayOtherError("9931 PIN gesperrt")
	h.gateway.setFailure(gatewayFailure(failure))
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err == nil {
		t.Fatal("sync with bank error succeeded")
	}
	if bank := loadBankItem(t, h.db, h.bank); bank.SyncFrom == nil || *bank.SyncFrom != from {
		t.Fatalf("sync from after error = %v, want %s", bank.SyncFrom, from)
	}

	h.gateway.setFailure(nil)
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(nil, false)); err == nil {
		t.Fatal("cancelled sync succeeded")
	}
	if bank := loadBankItem(t, h.db, h.bank); bank.SyncFrom == nil || *bank.SyncFrom != from {
		t.Fatalf("sync from after cancel = %v, want %s", bank.SyncFrom, from)
	}
}

func TestCancelledPinAndBankErrorsAreReportedAndNothingIsImported(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "1.00", []FinTsTransaction{
		booking(giro, h.today, "-1.00", stringPointer("X"), stringPointer("Y"), nil),
	})})

	_, cancelled := h.sync(t, newScriptedInteraction(nil, false))
	if cancelled == nil || bankingCode(cancelled) != CodeCancelled {
		t.Fatalf("cancelled error = %v, want code CANCELLED", cancelled)
	}

	failure := NewGatewayOtherError("9931 PIN gesperrt")
	h.gateway.setFailure(gatewayFailure(failure))
	_, failed := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if failed == nil || bankingCode(failed) != CodeFintsError {
		t.Fatalf("failed error = %v, want code FINTS_ERROR", failed)
	}
	if failed.Error() != "9931 PIN gesperrt" {
		t.Fatalf("message = %q, want %q", failed.Error(), "9931 PIN gesperrt")
	}
	if len(h.accounts(t)) != 0 {
		t.Fatal("accounts were created")
	}
	items, err := txrepo.FindItems(context.Background(), h.db, &model.TransactionFilter{}, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("transactions = %v, want none", items)
	}
}

func TestSyncNeedsAUserIDAndAServerURL(t *testing.T) {
	h := newHarness(t)
	noUser := createBank(t, h.db, "Sparkasse", "37050198", "", true)
	_, err := h.service.SyncBankConnection(
		context.Background(), h.db, noUser, newScriptedInteraction(stringPointer("1"), false), h.today,
	)
	if err == nil || bankingCode(err) != CodeNoUser {
		t.Fatalf("no user error = %v, want code NO_USER", err)
	}

	noServer := createBank(t, h.db, "Tiny Bank", "99999999", "u", false)
	_, err = h.service.SyncBankConnection(
		context.Background(), h.db, noServer, newScriptedInteraction(stringPointer("1"), false), h.today,
	)
	if err == nil || bankingCode(err) != CodeNoServer {
		t.Fatalf("no server error = %v, want code NO_SERVER", err)
	}

	serverURL := "https://fints.tiny.example/pintan"
	if _, err := bankconnections.UpdateConnection(
		context.Background(), h.db, noServer, model.BankConnectionUpdate{ServerURL: &serverURL},
	); err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	h.gateway.setSnapshots(nil)
	if _, err := h.service.SyncBankConnection(
		context.Background(), h.db, noServer, newScriptedInteraction(stringPointer("1"), false), h.today,
	); err != nil {
		t.Fatalf("sync with server: %v", err)
	}
}

func TestARenamedBankConnectionStillSyncsAndPromptsWithTheNewName(t *testing.T) {
	h := newHarness(t)
	if _, err := bankconnections.Update(
		context.Background(), h.db, h.bank, stringPointer("DKB Business"), nil,
	); err != nil {
		t.Fatalf("bankconnections.Update: %v", err)
	}
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "500.00", []FinTsTransaction{
		booking(giro, h.today, "-30.00", stringPointer("Shop"), stringPointer("B"), stringPointer("470.00")),
	})})
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	report, err := h.sync(t, interaction)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	assertInt(t, report.AccountsAdded, 1)
	assertInt(t, report.Imported, 1)
	if got := interaction.pinBankNameValues(); !reflect.DeepEqual(got, []string{"DKB Business"}) {
		t.Fatalf("pin bank names = %v, want [DKB Business]", got)
	}
}

func TestALoanWithoutABalanceIsCreatedAsALoanAndNotedWithoutLeakingTheIBAN(t *testing.T) {
	h := newHarness(t)
	const loanIBAN = "DE89370400440532013040"
	const giro = "DE89370400440532013000"
	loanAccount := account(loanIBAN)
	loanAccount.AccountType = "loan"
	h.gateway.setSnapshots([]AccountSnapshot{
		{
			Account:        loanAccount,
			Balance:        nil,
			Transactions:   nil,
			StatementError: nil,
			BalanceCode:    stringPointer("3016"),
			Pending:        nil,
		},
		snapshot(giro, "1234.56", nil),
	})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	assertInt(t, report.AccountsAdded, 2)
	loaded := h.accounts(t)
	loan := findAccount(t, loaded, loanIBAN)
	if loan.AccountType != model.AccountTypeLoan {
		t.Fatalf("loan type = %q, want loan", loan.AccountType)
	}
	if len(report.Notes) != 1 {
		t.Fatalf("notes = %v, want one", report.Notes)
	}
	note := report.Notes[0]
	if !strings.Contains(note, "3040") || !strings.Contains(note, "3016") {
		t.Fatalf("loan note = %q, want 3040 and 3016", note)
	}
	if strings.Contains(note, loanIBAN) {
		t.Fatalf("loan note leaked the IBAN: %q", note)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
	if bank := loadBankItem(t, h.db, h.bank); bank.LastSync == nil {
		t.Fatal("last sync is nil")
	}
	for _, entry := range auditEntries(t, h.db, 100) {
		if strings.Contains(entry.Details, loanIBAN) {
			t.Fatalf("audit leaked the IBAN: %q", entry.Details)
		}
	}

	if err := accounts.SetBalance(
		context.Background(), h.db, loan.ID, money.Money{Minor: -1_200_000, Currency: money.EUR}, time.Now().UTC(),
	); err != nil {
		t.Fatalf("accounts.SetBalance: %v", err)
	}
	second, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(second.Notes) != 0 {
		t.Fatalf("notes = %v, want none", second.Notes)
	}
}

func TestAnOldLastSyncNotesTheSkippedWindowOnceAndStillAdvances(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	oldSync := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	setLastSync(t, h.db, h.bank, oldSync)

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncGapNotFetched,
		i18n.Str("22.12.2025"),
		i18n.Str("14.01.2026"),
		i18n.Int(InitialSyncDays),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
	assertDate(t, h.gateway.since(giro), mustDate(t, 2026, time.January, 15))
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", report.Warnings)
	}
	bank := loadBankItem(t, h.db, h.bank)
	if bank.LastSync == nil || !bank.LastSync.After(oldSync) {
		t.Fatalf("last sync = %v, want after %v", bank.LastSync, oldSync)
	}
}

func TestALastSync85DaysBackNotesTheWindowWithTheDefaultEnd(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	setLastSync(t, h.db, h.bank, time.Date(2026, time.January, 20, 12, 0, 0, 0, time.UTC))

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncGapNotFetched,
		i18n.Str("10.01.2026"),
		i18n.Str("14.01.2026"),
		i18n.Int(InitialSyncDays),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestAHistoryStartInsideTheGapEndsTheNoteTheDayBeforeIt(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	setLastSync(t, h.db, h.bank, time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
	from := mustDate(t, 2026, time.January, 5)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncGapNotFetched,
		i18n.Str("22.12.2025"),
		i18n.Str("04.01.2026"),
		i18n.Int(InitialSyncDays),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestALastSyncInsideTheInitialWindowDoesNotNoteAGap(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	setLastSync(t, h.db, h.bank, time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC))

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
}

func TestAChosenHistoryStartBeforeTheGapSuppressesTheGapNote(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	setLastSync(t, h.db, h.bank, time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
	from := mustDate(t, 2025, time.December, 1)
	if _, err := bankconnections.SetSyncFrom(context.Background(), h.db, h.bank, &from); err != nil {
		t.Fatalf("SetSyncFrom: %v", err)
	}

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
	assertDate(t, h.gateway.since(giro), from)
}

func TestAnOldLastSyncWithOnlyANewAccountDoesNotNoteAGap(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const savings = "DE44500105175407324931"
	setLastSync(t, h.db, h.bank, time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

	h.gateway.setSnapshots([]AccountSnapshot{snapshot(savings, "100.00", nil)})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
	assertInt(t, report.AccountsAdded, 1)
}

func TestAFirstSyncDoesNotNoteAGap(t *testing.T) {
	h := newHarnessWithToday(t, mustDate(t, 2026, time.April, 15))
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
}

func TestAnAccountWithoutAnIBANIsNotedWithoutExposingItsNumber(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	const number = "4111111111111111"
	withoutIBAN := account("")
	withoutIBAN.AccountNumber = number
	h.gateway.setSnapshots([]AccountSnapshot{
		snapshot(giro, "100.00", nil),
		{
			Account: withoutIBAN,
			Balance: &FinTsBalance{
				Account:          account(""),
				Balance:          mustDecimal("5.00"),
				AvailableBalance: nil,
				Currency:         "EUR",
				Date:             time.Now().UTC(),
			},
			Transactions:   nil,
			StatementError: nil,
			BalanceCode:    nil,
			Pending:        nil,
		},
	})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	assertInt(t, report.AccountsSynced, 1)
	assertInt(t, report.AccountsAdded, 1)
	assertInt(t, len(h.accounts(t)), 1)
	want := []string{i18n.Format(i18n.En, i18n.SyncAccountWithoutIban, i18n.Str("…1111"))}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
	if strings.Contains(strings.Join(report.Notes, "\n"), number) {
		t.Fatal("notes leaked the account number")
	}
	if strings.Contains(report.Summary(), number) {
		t.Fatal("summary leaked the account number")
	}
	for _, entry := range auditEntries(t, h.db, 100) {
		if strings.Contains(entry.Details, number) {
			t.Fatalf("audit leaked the account number: %q", entry.Details)
		}
	}
}

func TestAnAccountWithoutAnIBANOrNumberIsNotedByItsProductName(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	withoutIBAN := account("")
	withoutIBAN.AccountNumber = ""
	withoutIBAN.ProductName = stringPointer("Girokonto")
	h.gateway.setSnapshots([]AccountSnapshot{
		snapshot(giro, "100.00", nil),
		{
			Account:        withoutIBAN,
			Balance:        nil,
			Transactions:   nil,
			StatementError: nil,
			BalanceCode:    nil,
			Pending:        nil,
		},
	})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(report.Notes) == 0 || !strings.HasPrefix(report.Notes[0], "Girokonto:") {
		t.Fatalf("notes = %v, want one starting with Girokonto:", report.Notes)
	}
}

func TestABalanceInAnotherCurrencyIsNotStoredAndIsNoted(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "50.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	foreign := foreignSnapshot(giro, "USD")
	foreign.Account.Currency = "EUR"
	foreign.Account.CurrencyStated = true
	if foreign.Balance != nil {
		foreign.Balance.Account.Currency = "EUR"
	}
	h.gateway.setSnapshots([]AccountSnapshot{foreign})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := h.accounts(t)[0].Balance.Minor; got != 5_000 {
		t.Fatalf("balance = %d, want %d", got, 5_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("USD"),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestABalanceInTheAccountCurrencyIsStoredWithoutANote(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "50.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if got := h.accounts(t)[0].Balance.Minor; got != 10_000 {
		t.Fatalf("balance = %d, want %d", got, 10_000)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
}

func TestANewAccountWithABalanceInAnotherCurrencyStartsAtZeroAndIsNoted(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	foreign := foreignSnapshot(giro, "USD")
	foreign.Account.Currency = "EUR"
	foreign.Account.CurrencyStated = true
	if foreign.Balance != nil {
		foreign.Balance.Account.Currency = "EUR"
	}
	h.gateway.setSnapshots([]AccountSnapshot{foreign})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != money.EUR {
		t.Fatalf("currency = %s, want EUR", stored.Currency)
	}
	if stored.Balance.Minor != 0 {
		t.Fatalf("balance = %d, want 0", stored.Balance.Minor)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("USD"),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestASyncUpdatesTheBalanceAndImportsBookingsButKeepsTheAccountName(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	accountID := h.accounts(t)[0].ID
	name := "Haushalt"
	if err := accounts.Rename(context.Background(), h.db, accountID, &name); err != nil {
		t.Fatalf("accounts.Rename: %v", err)
	}

	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "120.00", []FinTsTransaction{
		booking(giro, h.today, "-5.00", stringPointer("Shop"), stringPointer("A"), stringPointer("120.00")),
	})})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertInt(t, report.Imported, 1)
	stored := h.accounts(t)[0]
	if stored.Name == nil || *stored.Name != "Haushalt" {
		t.Fatalf("name = %v, want Haushalt", stored.Name)
	}
	if stored.Balance.Minor != 12_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 12_000)
	}
}

func TestASyncStoresPendingLinesApartFromTheBookingsAndClearsThemOnceBooked(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	const employer = "Beispiel AG"
	const tank = "Beispiel Tank GmbH"
	booked := booking(giro, h.today.AddDays(-3), "-120.00", stringPointer(employer), stringPointer("Gehalt"), nil)
	pendingLine := booking(giro, h.today, "-49.80", stringPointer(tank), stringPointer("Kraftstoff"), nil)
	first := snapshot(giro, "1000.00", []FinTsTransaction{booked})
	first.Pending = []FinTsTransaction{pendingLine}
	h.gateway.setSnapshots([]AccountSnapshot{first})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	accountID := h.accounts(t)[0].ID
	items := transactionsForAccount(t, h.db, accountID)
	assertInt(t, len(items), 1)
	if items[0].Amount.Minor != -12_000 {
		t.Fatalf("booking amount = %d, want %d", items[0].Amount.Minor, -12_000)
	}
	pending, err := pendingbookings.FindItems(context.Background(), h.db, accountID)
	if err != nil {
		t.Fatalf("pendingbookings.FindItems: %v", err)
	}
	assertInt(t, len(pending), 1)
	if pending[0].Amount.Minor != -4_980 {
		t.Fatalf("pending amount = %d, want %d", pending[0].Amount.Minor, -4_980)
	}
	if pending[0].PartnerName == nil || *pending[0].PartnerName != tank {
		t.Fatalf("pending partner = %v, want %s", pending[0].PartnerName, tank)
	}

	second := snapshot(giro, "950.20", []FinTsTransaction{booked, pendingLine})
	second.Pending = []FinTsTransaction{}
	h.gateway.setSnapshots([]AccountSnapshot{second})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	items = transactionsForAccount(t, h.db, accountID)
	assertInt(t, len(items), 2)
	matches := 0
	for _, item := range items {
		if item.Amount.Minor == -4_980 && item.PartnerName != nil && *item.PartnerName == tank {
			matches++
		}
	}
	assertInt(t, matches, 1)
	pending, err = pendingbookings.FindItems(context.Background(), h.db, accountID)
	if err != nil {
		t.Fatalf("pendingbookings.FindItems: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %v, want none", pending)
	}

	keptDate := h.today.AddDays(2)
	kept := model.NewPendingBooking{
		Date:        keptDate,
		ValueDate:   &keptDate,
		Amount:      money.Money{Minor: -1_000, Currency: money.EUR},
		PartnerName: stringPointer(tank),
		Purpose:     stringPointer("Kraftstoff"),
	}
	if err := pendingbookings.ReplaceForAccount(
		context.Background(), h.db, accountID, []model.NewPendingBooking{kept}, time.Now().UTC(),
	); err != nil {
		t.Fatalf("pendingbookings.ReplaceForAccount: %v", err)
	}
	third := snapshot(giro, "950.20", nil)
	third.Pending = nil
	h.gateway.setSnapshots([]AccountSnapshot{third})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	stored, err := pendingbookings.FindItems(context.Background(), h.db, accountID)
	if err != nil {
		t.Fatalf("pendingbookings.FindItems: %v", err)
	}
	assertInt(t, len(stored), 1)
	if stored[0].Amount.Minor != -1_000 {
		t.Fatalf("pending amount = %d, want %d", stored[0].Amount.Minor, -1_000)
	}
}

func TestCSVBackFillThenSyncOfTheSameBookingsAddsNothing(t *testing.T) {
	h := newHarness(t)
	const iban = "DE89370400440532013000"
	accountID := manualAccount(t, h.db, iban)
	day := mustDate(t, 2026, time.October, 3)
	csv := csvStatement(t,
		"Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n"+
			"03.10.2026;-45,67;REWE;Kartenzahlung\n"+
			"03.10.2026;-4,50;Bäcker;Kaffee\n",
		day,
	)
	importStatement(t, h.db, accountID, csv, day)

	result, err := h.service.ImportTransactions(context.Background(), h.db, accountID, []FinTsTransaction{
		fintsLine(iban, day, "-45.67", "REWE Markt GmbH", "REWE SAGT DANKE 1234", "954.33"),
		fintsLine(iban, day, "-4.50", "BAECKEREI SCHMIDT", "KARTENZAHLUNG 03.10", "949.83"),
	})
	if err != nil {
		t.Fatalf("ImportTransactions: %v", err)
	}

	assertInt(t, result.Imported, 0)
	assertInt(t, len(transactionsForAccount(t, h.db, accountID)), 2)
}

func TestGenuineExtraRepeatsAndDifferentAmountsAreStillImported(t *testing.T) {
	h := newHarness(t)
	const iban = "DE89370400440532013000"
	accountID := manualAccount(t, h.db, iban)
	day := mustDate(t, 2026, time.October, 3)
	csv := csvStatement(t,
		"Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n"+
			"03.10.2026;-4,50;Bäcker;Kaffee\n"+
			"03.10.2026;-4,50;Bäcker;Kaffee\n",
		day,
	)
	importStatement(t, h.db, accountID, csv, day)

	result, err := h.service.ImportTransactions(context.Background(), h.db, accountID, []FinTsTransaction{
		fintsLine(iban, day, "-4.50", "BAECKEREI", "Kaffee", "100.00"),
		fintsLine(iban, day, "-4.50", "BAECKEREI", "Kaffee", "95.50"),
		fintsLine(iban, day, "-4.50", "BAECKEREI", "Kaffee", "91.00"),
		fintsLine(iban, day, "-4.70", "BAECKEREI", "Kaffee gross", "86.30"),
	})
	if err != nil {
		t.Fatalf("ImportTransactions: %v", err)
	}

	assertInt(t, result.Imported, 2)
	assertInt(t, len(transactionsForAccount(t, h.db, accountID)), 4)
}

func TestAnOfflineBankNeverReachesTheGateway(t *testing.T) {
	h := newHarness(t)
	offline, err := bankconnections.CreateOffline(context.Background(), h.db, "Norwegian Card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}
	interaction := newScriptedInteraction(stringPointer("12345"), false)

	_, syncErr := h.service.SyncBankConnection(
		context.Background(), h.db, offline.ID, interaction, h.today,
	)
	if syncErr == nil || bankingCode(syncErr) != CodeOffline {
		t.Fatalf("error = %v, want code OFFLINE", syncErr)
	}
	if got := interaction.pinAskedCount(); got != 0 {
		t.Fatalf("pin asked = %d, want 0", got)
	}
	if got := h.gateway.sinceRequestCount(); got != 0 {
		t.Fatalf("since requests = %d, want 0", got)
	}
}

func TestOnlyOfflineBanksAreRefusedLocallyAnOnlineBankStillSyncs(t *testing.T) {
	h := newHarness(t)
	h.gateway.setSnapshots(nil)

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestAnOfflineBankUpdateOnlyChangesTheName(t *testing.T) {
	h := newHarness(t)
	offline, err := bankconnections.CreateOffline(context.Background(), h.db, "Norwegian Card")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}

	updated, err := h.service.UpdateConnection(
		context.Background(),
		h.db,
		offline.ID,
		"Norwegian Visa",
		"12345678",
		"user",
		"https://attacker.example/fints",
	)
	if err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}

	assertString(t, updated.BankName, "Norwegian Visa")
	assertString(t, updated.BLZ, "")
	assertString(t, updated.UserID, "")
	if updated.ServerURL != nil {
		t.Fatalf("server URL = %v, want nil", updated.ServerURL)
	}
	if !updated.Offline {
		t.Fatal("updated bank is not offline")
	}
}

func TestAddingABankConnectionStoresTheServerURLAndAuditsIt(t *testing.T) {
	h := newHarness(t)
	config := FinTsConfig{
		BankName:    "Neue Bank",
		BLZ:         "12030000",
		BIC:         "BYLADEM1001",
		UserID:      "new-user",
		CustomerID:  nil,
		HBCIVersion: "300",
		ServerURL:   stringPointer("https://fints.example/new"),
		TANMethod:   nil,
		ProductID:   "TEST-PRODUCT",
	}

	created, err := h.service.AddBankConnection(context.Background(), h.db, config)
	if err != nil {
		t.Fatalf("AddBankConnection: %v", err)
	}

	assertString(t, created.BankName, "Neue Bank")
	if created.ServerURL == nil || *created.ServerURL != "https://fints.example/new" {
		t.Fatalf("server URL = %v, want https://fints.example/new", created.ServerURL)
	}
	if !auditHasAction(t, h.db, CodeAddBank) {
		t.Fatal("missing ADD_BANK audit entry")
	}
}

func TestASyncCorrectsAnAccountTheOldCurrencyFallbackCreatedAsEUR(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: money.EUR}, model.AccountTypeChecking)
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "CHF")})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	chf, err := money.NewCurrency("CHF")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	if stored.Currency != chf {
		t.Fatalf("currency = %s, want CHF", stored.Currency)
	}
	if stored.Balance.Minor != 8_850 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 8_850)
	}
	correction := auditEntriesWithAction(t, h.db, "CORRECT_ACCOUNT_CURRENCY")
	if len(correction) != 1 {
		t.Fatalf("corrections = %v, want one", correction)
	}
	if !strings.Contains(correction[0].Details, "EUR -> CHF") {
		t.Fatalf("correction details = %q", correction[0].Details)
	}
	if strings.Contains(correction[0].Details, giro) {
		t.Fatalf("correction leaked the IBAN: %q", correction[0].Details)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncCurrencyCorrected,
		i18n.Str("3000"),
		i18n.Str("EUR"),
		i18n.Str("CHF"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestASecondSyncOfACorrectedAccountWritesNoFurtherCorrection(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: money.EUR}, model.AccountTypeChecking)
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "CHF")})
	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	second := foreignSnapshot(giro, "CHF")
	if second.Balance != nil {
		second.Balance.Balance = mustDecimal("90.00")
	}
	h.gateway.setSnapshots([]AccountSnapshot{second})
	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	assertInt(t, len(auditEntriesWithAction(t, h.db, "CORRECT_ACCOUNT_CURRENCY")), 1)
	stored := h.accounts(t)[0]
	chf, err := money.NewCurrency("CHF")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	if stored.Currency != chf {
		t.Fatalf("currency = %s, want CHF", stored.Currency)
	}
	if stored.Balance.Minor != 9_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 9_000)
	}
	if len(report.Notes) != 0 {
		t.Fatalf("notes = %v, want none", report.Notes)
	}
}

func TestAnAccountWithAnEURBookingKeepsItsCurrencyAndTheOtherCurrencyNote(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	accountID := bankAccount(
		t, h.db, h.bank, giro,
		money.Money{Minor: 10_000, Currency: money.EUR},
		model.AccountTypeChecking,
	)
	if _, err := txrepo.InsertImported(context.Background(), h.db, accountID, []model.NewTransaction{{
		BookingDate:  h.today.AddDays(-5),
		Amount:       money.Money{Minor: -500, Currency: money.EUR},
		BalanceAfter: nil,
		PartnerName:  stringPointer("Shop"),
		Purpose:      stringPointer("Old EUR booking"),
		CategoryID:   nil,
		ImportHash:   "old-eur",
		CreatedAt:    time.Now().UTC(),
		IsNew:        false,
		TaxOverride:  nil,
	}}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "CHF")})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != money.EUR {
		t.Fatalf("currency = %s, want EUR", stored.Currency)
	}
	if stored.Balance.Minor != 10_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 10_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("CHF"),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
	if len(auditEntriesWithAction(t, h.db, "CORRECT_ACCOUNT_CURRENCY")) != 0 {
		t.Fatal("the account was corrected")
	}
}

func TestAnAccountWhoseBankStatesItsCurrencyIsNotCorrected(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: money.EUR}, model.AccountTypeChecking)
	foreign := foreignSnapshot(giro, "CHF")
	foreign.Account.CurrencyStated = true
	h.gateway.setSnapshots([]AccountSnapshot{foreign})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != money.EUR {
		t.Fatalf("currency = %s, want EUR", stored.Currency)
	}
	if stored.Balance.Minor != 10_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 10_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("CHF"),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
	if len(auditEntriesWithAction(t, h.db, "CORRECT_ACCOUNT_CURRENCY")) != 0 {
		t.Fatal("the account was corrected")
	}
}

func TestABalanceCurrencyThatIsNotThreeLettersNeverCorrectsTheAccount(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: money.EUR}, model.AccountTypeChecking)
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "CH")})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != money.EUR {
		t.Fatalf("currency = %s, want EUR", stored.Currency)
	}
	if stored.Balance.Minor != 10_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 10_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("CH"),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestABlankBalanceCurrencyNeverCorrectsTheAccount(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: money.EUR}, model.AccountTypeChecking)
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "")})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != money.EUR {
		t.Fatalf("currency = %s, want EUR", stored.Currency)
	}
	if stored.Balance.Minor != 10_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 10_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str(""),
		i18n.Str("EUR"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestAnAccountKeptInAnotherCurrencyThanEURIsNotCorrected(t *testing.T) {
	h := newHarness(t)
	const giro = "DE89370400440532013000"
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	bankAccount(t, h.db, h.bank, giro, money.Money{Minor: 10_000, Currency: usd}, model.AccountTypeChecking)
	h.gateway.setSnapshots([]AccountSnapshot{foreignSnapshot(giro, "CHF")})

	report, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := h.accounts(t)[0]
	if stored.Currency != usd {
		t.Fatalf("currency = %s, want USD", stored.Currency)
	}
	if stored.Balance.Minor != 10_000 {
		t.Fatalf("balance = %d, want %d", stored.Balance.Minor, 10_000)
	}
	want := []string{i18n.Format(
		i18n.En,
		i18n.SyncBalanceOtherCurrency,
		i18n.Str("3000"),
		i18n.Str("CHF"),
		i18n.Str("USD"),
	)}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Fatalf("notes = %v, want %v", report.Notes, want)
	}
}

func TestABankTypedLoanCorrectsOnlyAStoredDepotNeverATypeTheUserChose(t *testing.T) {
	h := newHarness(t)
	const depotIBAN = "DE89370400440532013041"
	const savingsIBAN = "DE89370400440532013042"
	depotID := bankAccount(t, h.db, h.bank, depotIBAN, money.Money{Minor: 0, Currency: money.EUR}, model.AccountTypeDepot)
	bankAccount(t, h.db, h.bank, savingsIBAN, money.Money{Minor: 0, Currency: money.EUR}, model.AccountTypeSavings)
	depot := account(depotIBAN)
	depot.AccountType = "loan"
	savings := account(savingsIBAN)
	savings.AccountType = "loan"
	h.gateway.setSnapshots([]AccountSnapshot{
		snapshot(depotIBAN, "1.00", nil).withAccount(depot),
		snapshot(savingsIBAN, "2.00", nil).withAccount(savings),
	})

	if _, err := h.sync(t, newScriptedInteraction(stringPointer("12345"), false)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	loaded := h.accounts(t)
	if got := findAccount(t, loaded, depotIBAN).AccountType; got != model.AccountTypeLoan {
		t.Fatalf("depot type = %q, want loan", got)
	}
	if got := findAccount(t, loaded, savingsIBAN).AccountType; got != model.AccountTypeSavings {
		t.Fatalf("savings type = %q, want savings (a type the user chose is left alone)", got)
	}
	corrections := auditEntriesWithAction(t, h.db, "CHANGE_ACCOUNT_TYPE")
	if len(corrections) != 1 {
		t.Fatalf("corrections = %v, want one", corrections)
	}
	if !strings.Contains(corrections[0].Details, "depot -> loan") {
		t.Fatalf("correction details = %q", corrections[0].Details)
	}
	assertString(t, corrections[0].Details, "Account "+decimalText(int64(depotID))+": depot -> loan")
}
