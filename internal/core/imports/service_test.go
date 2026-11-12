package imports

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
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

func g3iDecimalPointer(value string) *decimal.Decimal {
	parsed := decValue(value)
	return &parsed
}

func g3iCategory(t *testing.T, d *db.Database, name string) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:          name,
		Color:         "#3BC7B0",
		BudgetLimit:   nil,
		IsTaxRelevant: false,
		IsTransfer:    false,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func g3iRule(
	t *testing.T,
	d *db.Database,
	pattern string,
	categoryID model.CategoryID,
	priority int64,
	matchField model.RuleField,
	direction model.RuleDirection,
) {
	t.Helper()

	if _, err := rulesrepo.Create(context.Background(), d, model.NewRule{
		Pattern:    pattern,
		CategoryID: categoryID,
		Priority:   priority,
		AutoApply:  true,
		MatchField: matchField,
		Direction:  direction,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
}

func g3iBooking(
	day civil.Date,
	amount string,
	partner *string,
	purpose *string,
	balanceAfter *string,
	bankReference *string,
) ImportedBooking {
	var balance *decimal.Decimal
	if balanceAfter != nil {
		balance = g3iDecimalPointer(*balanceAfter)
	}
	return ImportedBooking{
		Date:          day,
		ValueDate:     nil,
		Amount:        decValue(amount),
		Currency:      g3iStringPointer("EUR"),
		PartnerName:   partner,
		PartnerIBAN:   nil,
		Purpose:       purpose,
		BalanceAfter:  balance,
		BankReference: bankReference,
	}
}

func g3iStatement(bookings []ImportedBooking) ParsedStatement {
	return ParsedStatement{
		Format:         FormatCSV,
		Bookings:       bookings,
		AccountIBAN:    nil,
		Accounts:       nil,
		Skipped:        nil,
		PendingSkipped: 0,
		Csv:            nil,
		FormatDetail:   nil,
		ClosingBalance: nil,
	}
}

func g3iStoreBooking(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	amountMinor int64,
	hash string,
) {
	t.Helper()

	if _, err := transactions.InsertImported(
		context.Background(),
		d,
		accountID,
		[]model.NewTransaction{{
			BookingDate:  day,
			Amount:       g3iEUR(amountMinor),
			BalanceAfter: nil,
			PartnerName:  g3iStringPointer("Stored"),
			Purpose:      g3iStringPointer("Stored booking"),
			CategoryID:   nil,
			ImportHash:   hash,
			CreatedAt:    testNow(),
			IsNew:        false,
			TaxOverride:  nil,
		}},
	); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
}

func g3iStored(t *testing.T, d *db.Database, accountID model.AccountID) []model.Transaction {
	t.Helper()

	stored, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	return stored
}

func g3iStoredAmountsInInsertOrder(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
) []int64 {
	t.Helper()

	var amounts []int64
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT amount_minor FROM transactions WHERE account_id = ?1 ORDER BY id",
			int64(accountID),
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var amount int64
			if err := rows.Scan(&amount); err != nil {
				return err
			}
			amounts = append(amounts, amount)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read insert order: %v", err)
	}
	return amounts
}

func g3iBalance(t *testing.T, d *db.Database, accountID model.AccountID) money.Money {
	t.Helper()

	account, found, err := accounts.Get(context.Background(), d, accountID)
	if err != nil {
		t.Fatalf("accounts.Get: %v", err)
	}
	if !found {
		t.Fatal("the account is missing")
	}
	return account.Balance
}

func g3iSetBalance(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	balance money.Money,
	at time.Time,
) {
	t.Helper()

	if err := accounts.SetBalance(context.Background(), d, accountID, balance, at); err != nil {
		t.Fatalf("accounts.SetBalance: %v", err)
	}
}

func g3iImport(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	statement ParsedStatement,
	updateBalance bool,
	today civil.Date,
) ImportReport {
	t.Helper()

	report, err := Import(context.Background(), d, accountID, statement, updateBalance, today)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return report
}

func TestReImportingAndOverlappingExportsSkipKnownBookingsButKeepGenuineRepeats(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	food := g3iCategory(t, d, "Food")
	g3iRule(t, d, "bäcker|kaffee", food, 0, model.RuleFieldAny, model.RuleDirectionAny)

	today := testDate(t, 2026, 10, 5)
	csv := "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n" +
		"03.10.2026;-4,50;Bäcker;Kaffee\n" +
		"03.10.2026;-4,50;Bäcker;Kaffee\n" +
		"02.10.2026;-10,00;Kiosk;Zeitung\n"
	parsed, err := Read("a.csv", []byte(csv), nil, today)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	report := g3iImport(t, d, accountID, parsed, true, today)
	if report.Imported != 3 {
		t.Errorf("Imported = %d, want 3: two identical coffees on one day are both real", report.Imported)
	}
	if report.Categorized != 2 {
		t.Errorf("Categorized = %d, want 2", report.Categorized)
	}

	again := g3iImport(t, d, accountID, parsed, true, today)
	if again.Imported != 0 {
		t.Errorf("Imported = %d, want 0", again.Imported)
	}
	if again.Duplicates != 3 {
		t.Errorf("Duplicates = %d, want 3", again.Duplicates)
	}

	overlap := "Buchungstag;Betrag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck\n" +
		"04.10.2026;-2,00;Kiosk;Wasser\n" +
		"03.10.2026;-4,50;Bäcker;Kaffee\n" +
		"03.10.2026;-4,50;Bäcker;Kaffee\n"
	parsed, err = Read("b.csv", []byte(overlap), nil, today)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	report = g3iImport(t, d, accountID, parsed, true, today)
	if report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	if report.Duplicates != 2 {
		t.Errorf("Duplicates = %d, want 2", report.Duplicates)
	}
	if got := len(g3iStored(t, d, accountID)); got != 4 {
		t.Errorf("stored = %d, want 4", got)
	}
}

func TestStatementIBANSelectsTheAccountAndBalanceIsTakenFromTheNewestBooking(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	csv := "IBAN;DE89 3704 0044 0532 0130 00\n\n" +
		"Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n" +
		"02.10.2026;02.10.2026;AMAZON;Lastschrift;Monitor;910,01;EUR;-89,99;EUR\n" +
		"01.10.2026;01.10.2026;Employer;Gehalt;Gehalt;1.000,00;EUR;1.000,00;EUR\n"
	today := testDate(t, 2026, 10, 5)
	parsed, err := Read("ing.csv", []byte(csv), nil, today)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	matched := MatchAccount(context.Background(), d, parsed)
	if matched == nil || *matched != accountID {
		t.Errorf("MatchAccount = %v, want %d", matched, accountID)
	}

	report := g3iImport(t, d, accountID, parsed, true, today)
	if report.BalanceUpdated == nil || report.BalanceUpdated.Cmp(dec(t, "910.01")) != 0 {
		t.Errorf("BalanceUpdated = %v, want 910.01", report.BalanceUpdated)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(91_001) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(91_001))
	}
	if summary := report.Summary(); !strings.Contains(summary, "Balance set to €910.01") {
		t.Errorf("summary = %q", summary)
	}
}

func TestNewestFirstFilesWithRunningBalancesAreInsertedChronologically(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	newestFirst := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 2),
			"-89.99",
			g3iStringPointer("AMAZON"),
			g3iStringPointer("Monitor"),
			g3iStringPointer("910.01"),
			nil,
		),
		g3iBooking(
			testDate(t, 2026, 10, 1),
			"1000.00",
			g3iStringPointer("Employer"),
			g3iStringPointer("Gehalt"),
			g3iStringPointer("1000.00"),
			nil,
		),
	})
	report := g3iImport(t, d, accountID, newestFirst, true, testDate(t, 2026, 10, 5))

	if report.Imported != 2 {
		t.Errorf("Imported = %d, want 2", report.Imported)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(91_001) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(91_001))
	}
	got := g3iStoredAmountsInInsertOrder(t, d, accountID)
	want := []int64{100_000, -8_999}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("insert order = %v, want %v: oldest booking first", got, want)
	}
}

func TestOldestFirstFilesStillImportTheSameWay(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	oldestFirst := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 1),
			"1000.00",
			g3iStringPointer("Employer"),
			g3iStringPointer("Gehalt"),
			g3iStringPointer("1000.00"),
			nil,
		),
		g3iBooking(
			testDate(t, 2026, 10, 2),
			"-89.99",
			g3iStringPointer("AMAZON"),
			g3iStringPointer("Monitor"),
			g3iStringPointer("910.01"),
			nil,
		),
	})
	report := g3iImport(t, d, accountID, oldestFirst, true, testDate(t, 2026, 10, 5))

	if report.Imported != 2 {
		t.Errorf("Imported = %d, want 2", report.Imported)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(91_001) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(91_001))
	}
	got := g3iStoredAmountsInInsertOrder(t, d, accountID)
	want := []int64{100_000, -8_999}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("insert order = %v, want %v", got, want)
	}
}

func TestANewestFirstFileCoveringOneDayIsRecognisedByItsRunningBalances(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	day := testDate(t, 2026, 10, 2)
	oneDay := g3iStatement([]ImportedBooking{
		g3iBooking(day, "-5.00", g3iStringPointer("Kiosk"), g3iStringPointer("Zeitung"), g3iStringPointer("985.00"), nil),
		g3iBooking(day, "-10.00", g3iStringPointer("Bäcker"), g3iStringPointer("Brot"), g3iStringPointer("990.00"), nil),
		g3iBooking(day, "1000.00", g3iStringPointer("Employer"), g3iStringPointer("Bonus"), g3iStringPointer("1000.00"), nil),
	})
	report := g3iImport(t, d, accountID, oneDay, true, testDate(t, 2026, 10, 5))

	if report.Imported != 3 {
		t.Errorf("Imported = %d, want 3", report.Imported)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(98_500) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(98_500))
	}
	got := g3iStoredAmountsInInsertOrder(t, d, accountID)
	want := []int64{100_000, -1_000, -500}
	if len(got) != len(want) {
		t.Fatalf("insert order = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("insert order = %v, want %v", got, want)
			break
		}
	}
}

func TestBookingsFromAnImportedFileAreNeverMarkedNew(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)

	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 3),
			"-4.50",
			g3iStringPointer("Bäcker"),
			g3iStringPointer("Kaffee"),
			nil,
			nil,
		),
		g3iBooking(
			testDate(t, 2026, 10, 2),
			"-10.00",
			g3iStringPointer("Kiosk"),
			g3iStringPointer("Zeitung"),
			nil,
			nil,
		),
	})
	report := g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))

	if report.Imported != 2 {
		t.Errorf("Imported = %d, want 2", report.Imported)
	}
	for _, booking := range g3iStored(t, d, accountID) {
		if booking.IsNew {
			t.Errorf("booking %d is new", booking.ID)
		}
	}
}

func TestCAMTFilesAreDetectedByContent(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	xml := `<?xml version="1.0"?><Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.053.001.08"><BkToCstmrStmt><Stmt><Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct><Ntry><Amt Ccy="EUR">5.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts><Cd>BOOK</Cd></Sts><BookgDt><Dt>2026-10-01</Dt></BookgDt><AcctSvcrRef>X9</AcctSvcrRef></Ntry></Stmt></BkToCstmrStmt></Document>`
	today := testDate(t, 2026, 10, 5)
	parsed, err := Read("export.dat", []byte(xml), nil, today)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if parsed.Format != FormatCAMT053 {
		t.Errorf("format = %q, want %q", parsed.Format, FormatCAMT053)
	}
	if report := g3iImport(t, d, accountID, parsed, true, today); report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	reparsed, err := Read("export.dat", []byte(xml), nil, today)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if report := g3iImport(t, d, accountID, reparsed, true, today); report.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", report.Duplicates)
	}
}

func TestBookingsInAnotherCurrencyAreSkippedAndCounted(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	usd := g3iBooking(testDate(t, 2026, 10, 3), "-4.50", g3iStringPointer("Shop"), nil, nil, nil)
	usd.Currency = g3iStringPointer("USD")
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(testDate(t, 2026, 10, 3), "-4.50", g3iStringPointer("Shop"), nil, nil, nil),
		usd,
	})

	report := g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))
	if report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	if report.WrongCurrency != 1 {
		t.Errorf("WrongCurrency = %d, want 1", report.WrongCurrency)
	}
	if report.Currency != money.EUR {
		t.Errorf("Currency = %v, want EUR", report.Currency)
	}
	if summary := report.Summary(); !strings.Contains(summary, "1 in another currency than the account skipped") {
		t.Errorf("summary = %q", summary)
	}
}

func TestAnAmountWithTooManyDecimalPlacesIsSkippedAndCounted(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(testDate(t, 2026, 10, 3), "-4.501", g3iStringPointer("Shop"), nil, nil, nil),
		g3iBooking(testDate(t, 2026, 10, 3), "-4.50", g3iStringPointer("Shop"), nil, nil, nil),
	})

	report := g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))
	if report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	if report.InvalidAmount != 1 {
		t.Errorf("InvalidAmount = %d, want 1", report.InvalidAmount)
	}
	if summary := report.Summary(); !strings.Contains(summary, "1 with too many decimal places skipped") {
		t.Errorf("summary = %q", summary)
	}
}

func TestAnImportWritesOneAuditEntryWithMaskedIBANAndCounts(t *testing.T) {
	d := g3iDatabase(t)
	ibanText := "DE89370400440532013000"
	accountID := g3iAccount(t, d, g3iStringPointer(ibanText), 0, model.AccountTypeChecking)
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 3),
			"-4.50",
			g3iStringPointer("Shop"),
			nil,
			g3iStringPointer("100.00"),
			nil,
		),
	})

	g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))

	entries, err := audit.Recent(context.Background(), d, 10)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	var importEntry *audit.Entry
	for index := range entries {
		if entries[index].Action == "IMPORT_STATEMENT" {
			importEntry = &entries[index]
			break
		}
	}
	if importEntry == nil {
		t.Fatal("no IMPORT_STATEMENT entry")
	}
	if importEntry.Actor != audit.ActorUser {
		t.Errorf("actor = %q, want %q", importEntry.Actor, audit.ActorUser)
	}
	if !strings.Contains(importEntry.Details, "…3000") {
		t.Errorf("details = %q, want a masked IBAN", importEntry.Details)
	}
	if strings.Contains(importEntry.Details, ibanText) {
		t.Errorf("details = %q, the full IBAN must not be logged", importEntry.Details)
	}
	if !strings.Contains(importEntry.Details, "1 new, 0 known, 0 probable duplicates, 0 deleted earlier") {
		t.Errorf("details = %q", importEntry.Details)
	}
	count := 0
	for _, entry := range entries {
		if entry.Action == "IMPORT_STATEMENT" {
			count++
		}
		if entry.Action == "account.set_balance" {
			t.Error("the import's balance update is part of the single import audit entry")
		}
	}
	if count != 1 {
		t.Errorf("IMPORT_STATEMENT entries = %d, want 1", count)
	}
}

func TestAnImportWithTwoAccountsIsRefusedAndStoresNothing(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 1),
			"-10.00",
			g3iStringPointer("Partner"),
			g3iStringPointer("Test"),
			nil,
			nil,
		),
	})
	parsed.Accounts = []string{"DE89370400440532013000", "DE44500105175407324931"}

	_, err := Import(context.Background(), d, accountID, parsed, true, testDate(t, 2026, 10, 5))
	if err == nil {
		t.Fatal("Import succeeded, want an error")
	}
	message := err.Error()
	for _, text := range []string{"2", "…3000", "…4931"} {
		if !strings.Contains(message, text) {
			t.Errorf("message = %q, want %q", message, text)
		}
	}
	var several *SeveralAccountsError
	if !errors.As(err, &several) {
		t.Errorf("error type = %T, want *SeveralAccountsError", err)
	}
	if stored := g3iStored(t, d, accountID); len(stored) != 0 {
		t.Errorf("stored = %d, want 0", len(stored))
	}
}

func TestTwoStatementBlocksForTheSameAccountAreAcceptedAndImportAllBookings(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 1),
			"-10.00",
			g3iStringPointer("Partner"),
			g3iStringPointer("Test"),
			nil,
			nil,
		),
		g3iBooking(
			testDate(t, 2026, 10, 2),
			"-20.00",
			g3iStringPointer("Partner"),
			g3iStringPointer("Test"),
			nil,
			nil,
		),
	})
	parsed.Accounts = []string{"DE89370400440532013000"}

	report := g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))
	if report.Imported != 2 {
		t.Errorf("Imported = %d, want 2", report.Imported)
	}
	if stored := g3iStored(t, d, accountID); len(stored) != 2 {
		t.Errorf("stored = %d, want 2", len(stored))
	}
}

func TestAnExpenseOnlyRuleCategorizesOnlyTheExpense(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	groceries := g3iCategory(t, d, "Groceries")
	g3iRule(
		t,
		d,
		"Erika Mustermann",
		groceries,
		5,
		model.RuleFieldPartner,
		model.RuleDirectionExpense,
	)

	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2026, 10, 3),
			"-45.67",
			g3iStringPointer("Erika Mustermann"),
			g3iStringPointer("Kartenzahlung"),
			nil,
			nil,
		),
		g3iBooking(
			testDate(t, 2026, 10, 3),
			"1000.00",
			g3iStringPointer("Erika Mustermann"),
			g3iStringPointer("Rueckzahlung"),
			nil,
			nil,
		),
	})
	g3iImport(t, d, accountID, parsed, true, testDate(t, 2026, 10, 5))

	var expense *model.Transaction
	var income *model.Transaction
	stored := g3iStored(t, d, accountID)
	for index := range stored {
		if stored[index].Amount.Minor < 0 {
			expense = &stored[index]
		}
		if stored[index].Amount.Minor > 0 {
			income = &stored[index]
		}
	}
	if expense == nil || income == nil {
		t.Fatal("the expense and income bookings are missing")
	}
	if expense.CategoryID == nil || *expense.CategoryID != groceries {
		t.Errorf("expense category = %v, want %d", expense.CategoryID, groceries)
	}
	if income.CategoryID != nil {
		t.Errorf("income category = %v, want nil", *income.CategoryID)
	}
}

func TestARememberedDeletedBookingStaysOutOfAReImport(t *testing.T) {
	d := g3iDatabase(t)
	ibanText := "DE89370400440532013016"
	accountID := g3iAccount(t, d, g3iStringPointer(ibanText), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 5)
	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(day, "-5.00", g3iStringPointer("Cafe"), nil, nil, g3iStringPointer("REF-CAFE")),
		g3iBooking(day.AddDays(-1), "-7.00", g3iStringPointer("Bakery"), nil, nil, g3iStringPointer("REF-BAKERY")),
		g3iBooking(day.AddDays(-2), "-9.00", g3iStringPointer("Bookshop"), nil, nil, g3iStringPointer("REF-BOOKSHOP")),
	})

	if report := g3iImport(t, d, accountID, parsed, false, day); report.Imported != 3 {
		t.Errorf("Imported = %d, want 3", report.Imported)
	}

	var cafeID model.TransactionID
	foundCafe := false
	for _, booking := range g3iStored(t, d, accountID) {
		if booking.PartnerName != nil && *booking.PartnerName == "Cafe" {
			cafeID = booking.ID
			foundCafe = true
			break
		}
	}
	if !foundCafe {
		t.Fatal("the Cafe booking is missing")
	}
	if err := transactions.Delete(context.Background(), d, cafeID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rerun := g3iImport(t, d, accountID, parsed, false, day)
	if rerun.Imported != 0 {
		t.Errorf("Imported = %d, want 0: the deleted booking stays out", rerun.Imported)
	}
	if rerun.PreviouslyDeleted != 1 {
		t.Errorf("PreviouslyDeleted = %d, want 1", rerun.PreviouslyDeleted)
	}
	if rerun.Duplicates != 2 {
		t.Errorf("Duplicates = %d, want 2: the two stored bookings count as known", rerun.Duplicates)
	}
	if summary := rerun.Summary(); !strings.Contains(summary, ", 1 deleted earlier, skipped") {
		t.Errorf("summary = %q", summary)
	}
}

func TestAStoredBookingFromAnotherSourceIsAProbableDuplicateForTheImport(t *testing.T) {
	d := g3iDatabase(t)
	ibanText := "DE89370400440532013000"
	accountID := g3iAccount(t, d, g3iStringPointer(ibanText), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 3)
	g3iStoreBooking(t, d, accountID, day, -4_567, "bank-sync-hash")

	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(day, "-45.67", g3iStringPointer("REWE"), g3iStringPointer("Kartenzahlung"), nil, nil),
	})
	report := g3iImport(t, d, accountID, parsed, true, day)

	if report.Imported != 0 {
		t.Errorf("Imported = %d, want 0", report.Imported)
	}
	if report.ProbableDuplicates != 1 {
		t.Errorf("ProbableDuplicates = %d, want 1", report.ProbableDuplicates)
	}
	if summary := report.Summary(); !strings.Contains(summary, "1 probable duplicate") {
		t.Errorf("summary = %q", summary)
	}
}

func TestAManualEntryFollowedByAnImportOfTheSameBookingIsAProbableDuplicate(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	day := testDate(t, 2026, 10, 3)
	g3iStoreBooking(t, d, accountID, day, -8_999, "manual")

	parsed := g3iStatement([]ImportedBooking{
		g3iBooking(day, "-89.99", g3iStringPointer("AMAZON EU"), g3iStringPointer("Bestellung 303"), nil, nil),
	})
	report := g3iImport(t, d, accountID, parsed, true, day)

	if report.Imported != 0 {
		t.Errorf("Imported = %d, want 0", report.Imported)
	}
	if report.ProbableDuplicates != 1 {
		t.Errorf("ProbableDuplicates = %d, want 1", report.ProbableDuplicates)
	}
}

func TestAnOldStatementDoesNotResetANewerBalance(t *testing.T) {
	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	g3iStoreBooking(t, d, accountID, testDate(t, 2026, 10, 4), -100, "newest")
	g3iSetBalance(
		t,
		d,
		accountID,
		g3iEUR(500_000),
		time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
	)

	old := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2025, 1, 15),
			"-10.00",
			g3iStringPointer("Old shop"),
			g3iStringPointer("Old"),
			g3iStringPointer("1234.00"),
			nil,
		),
	})
	allowed, err := BalanceUpdateAllowed(context.Background(), d, accountID, old, testDate(t, 2026, 10, 5))
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}

	report := g3iImport(t, d, accountID, old, true, testDate(t, 2026, 10, 5))
	if report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	if report.BalanceUpdated != nil {
		t.Errorf("BalanceUpdated = %v, want nil", report.BalanceUpdated)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(500_000) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(500_000))
	}
}

func TestAnOldFileNeverReplacesABalanceTypedOnAnAccountWithoutBookings(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 50_000, model.AccountTypeChecking)
	old := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2019, 12, 31),
			"10.00",
			nil,
			nil,
			g3iStringPointer("100.00"),
			nil,
		),
	})

	allowed, err := BalanceUpdateAllowed(context.Background(), d, accountID, old, today)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}

	report := g3iImport(t, d, accountID, old, true, today)
	if report.Imported != 1 {
		t.Errorf("Imported = %d, want 1", report.Imported)
	}
	if report.BalanceUpdated != nil {
		t.Errorf("BalanceUpdated = %v, want nil", report.BalanceUpdated)
	}
	if got := g3iBalance(t, d, accountID); got != g3iEUR(50_000) {
		t.Errorf("balance = %v, want %v", got, g3iEUR(50_000))
	}
}

func TestAZeroBalanceOnAnAccountWithoutBookingsMayStillBeSet(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	old := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2019, 12, 31),
			"10.00",
			nil,
			nil,
			g3iStringPointer("100.00"),
			nil,
		),
	})

	allowed, err := BalanceUpdateAllowed(context.Background(), d, accountID, old, today)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if !allowed {
		t.Error("BalanceUpdateAllowed = false, want true")
	}
}

func TestAZeroBalanceSetTodayIsNotEmptyAndRejectsAnOldFile(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	g3iSetBalance(
		t,
		d,
		accountID,
		g3iEUR(0),
		time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
	)
	old := g3iStatement([]ImportedBooking{
		g3iBooking(
			testDate(t, 2019, 12, 31),
			"10.00",
			nil,
			nil,
			g3iStringPointer("100.00"),
			nil,
		),
	})

	allowed, err := BalanceUpdateAllowed(context.Background(), d, accountID, old, today)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}
	current := g3iStatement([]ImportedBooking{
		g3iBooking(today, "10.00", nil, nil, g3iStringPointer("100.00"), nil),
	})
	allowed, err = BalanceUpdateAllowed(context.Background(), d, accountID, current, today)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if !allowed {
		t.Error("BalanceUpdateAllowed = false, want true")
	}

	report := g3iImport(t, d, accountID, old, true, today)
	if report.BalanceUpdated != nil {
		t.Errorf("BalanceUpdated = %v, want nil", report.BalanceUpdated)
	}
	if got := g3iBalance(t, d, accountID); got.Minor != 0 {
		t.Errorf("balance = %v, want 0", got)
	}
}

func TestABalanceSetByUpdateBalanceOnlyAcceptsAFileAtLeastAsRecentAsTheUpdate(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 50_000, model.AccountTypeChecking)
	g3iSetBalance(
		t,
		d,
		accountID,
		g3iEUR(50_000),
		time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
	)

	allowed, err := BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(today, "10.00", nil, nil, g3iStringPointer("100.00"), nil),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if !allowed {
		t.Error("BalanceUpdateAllowed = false, want true")
	}
	allowed, err = BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(today.AddDays(-1), "10.00", nil, nil, g3iStringPointer("100.00"), nil),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}
}

func TestATypedBalanceAcceptsAFile45DaysOld(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 50_000, model.AccountTypeChecking)

	allowed, err := BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(
				testDate(t, 2026, 4, 5),
				"10.00",
				nil,
				nil,
				g3iStringPointer("100.00"),
				nil,
			),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if !allowed {
		t.Error("BalanceUpdateAllowed = false, want true")
	}
}

func TestATypedBalanceRejectsAFile46DaysOld(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 50_000, model.AccountTypeChecking)

	allowed, err := BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(
				testDate(t, 2026, 4, 4),
				"10.00",
				nil,
				nil,
				g3iStringPointer("100.00"),
				nil,
			),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}
}

func TestACardAccountWithATypedBalanceRejectsAnOldFile(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, nil, 10_000, model.AccountTypeCreditCard)

	allowed, err := BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(
				testDate(t, 2019, 12, 31),
				"10.00",
				nil,
				nil,
				g3iStringPointer("100.00"),
				nil,
			),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}
}

func TestAnAccountWithBookingsStillComparesTheFileWithItsNewestBooking(t *testing.T) {
	d := g3iDatabase(t)
	today := testDate(t, 2026, 5, 20)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 100_000, model.AccountTypeChecking)
	g3iStoreBooking(t, d, accountID, testDate(t, 2026, 10, 1), -1_000, "existing")

	allowed, err := BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(
				testDate(t, 2026, 10, 1),
				"10.00",
				nil,
				nil,
				g3iStringPointer("100.00"),
				nil,
			),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if !allowed {
		t.Error("BalanceUpdateAllowed = false, want true")
	}
	allowed, err = BalanceUpdateAllowed(
		context.Background(),
		d,
		accountID,
		g3iStatement([]ImportedBooking{
			g3iBooking(
				testDate(t, 2026, 9, 30),
				"10.00",
				nil,
				nil,
				g3iStringPointer("100.00"),
				nil,
			),
		}),
		today,
	)
	if err != nil {
		t.Fatalf("BalanceUpdateAllowed: %v", err)
	}
	if allowed {
		t.Error("BalanceUpdateAllowed = true, want false")
	}
}
