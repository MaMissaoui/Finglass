package balance

import (
	"reflect"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func mustDate(year int, month time.Month, day int) civil.Date {
	date, ok := civil.New(year, month, day)
	if !ok {
		panic("invalid test date")
	}
	return date
}

func testMoney(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func testTxn(id int64, day civil.Date, minor int64, balanceAfter *int64) model.TransactionItem {
	var bank *money.Money
	if balanceAfter != nil {
		value := testMoney(*balanceAfter)
		bank = &value
	}
	iban := "DE1"
	return model.TransactionItem{
		ID:           model.TransactionID(id),
		AccountID:    model.AccountID(1),
		AccountIBAN:  &iban,
		Date:         day,
		Amount:       testMoney(minor),
		BalanceAfter: bank,
		Currency:     money.EUR,
		TaxSource:    model.TaxSourceNone,
	}
}

func requireEntry(t *testing.T, entries map[model.TransactionID]Entry, id model.TransactionID) Entry {
	t.Helper()
	entry, ok := entries[id]
	if !ok {
		t.Fatalf("no entry for transaction %d", id)
	}
	return entry
}

func TestNewestRowEqualsTheCurrentBalanceAndEveryRowChainsToTheOlderOne(t *testing.T) {
	newest := testTxn(3, mustDate(2026, time.March, 3), -3000, nil)
	middle := testTxn(2, mustDate(2026, time.March, 2), 10_000, nil)
	oldest := testTxn(1, mustDate(2026, time.March, 1), -5000, nil)

	entries := ComputeRunning(testMoney(12_000), []model.TransactionItem{newest, middle, oldest})

	if got, want := requireEntry(t, entries, newest.ID).Balance, testMoney(12_000); got != want {
		t.Fatalf("newest balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, middle.ID).Balance, testMoney(15_000); got != want {
		t.Fatalf("middle balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, oldest.ID).Balance, testMoney(5000); got != want {
		t.Fatalf("oldest balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, middle.ID).Balance.Minor,
		requireEntry(t, entries, newest.ID).Balance.Minor-newest.Amount.Minor; got != want {
		t.Fatalf("middle chain = %d, want %d", got, want)
	}
	if got, want := requireEntry(t, entries, oldest.ID).Balance.Minor,
		requireEntry(t, entries, middle.ID).Balance.Minor-middle.Amount.Minor; got != want {
		t.Fatalf("oldest chain = %d, want %d", got, want)
	}
	for id, entry := range entries {
		if entry.BankDifference != nil {
			t.Fatalf("transaction %d has a bank difference", id)
		}
	}
}

func TestSameDayBookingsChainByIDOrder(t *testing.T) {
	oldest := testTxn(1, mustDate(2026, time.March, 1), -1000, nil)
	middle := testTxn(2, mustDate(2026, time.March, 1), -2000, nil)
	newest := testTxn(3, mustDate(2026, time.March, 1), -3000, nil)

	entries := ComputeRunning(testMoney(4000), []model.TransactionItem{newest, middle, oldest})

	if got, want := requireEntry(t, entries, newest.ID).Balance, testMoney(4000); got != want {
		t.Fatalf("newest balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, middle.ID).Balance, testMoney(7000); got != want {
		t.Fatalf("middle balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, oldest.ID).Balance, testMoney(9000); got != want {
		t.Fatalf("oldest balance = %v, want %v", got, want)
	}
}

func TestAFutureDatedBookingIsTheNewestRowAndStillStartsAtTheCurrentBalance(t *testing.T) {
	future := testTxn(2, mustDate(2026, time.December, 31), -5000, nil)
	today := testTxn(1, mustDate(2026, time.March, 1), 2000, nil)

	entries := ComputeRunning(testMoney(20_000), []model.TransactionItem{future, today})

	if got, want := requireEntry(t, entries, future.ID).Balance, testMoney(20_000); got != want {
		t.Fatalf("future balance = %v, want %v (the account balance includes future bookings)", got, want)
	}
	if got, want := requireEntry(t, entries, today.ID).Balance, testMoney(25_000); got != want {
		t.Fatalf("today balance = %v, want %v", got, want)
	}
}

func TestABankBalanceIsPreferredAndOnlyItsOwnRowIsMarked(t *testing.T) {
	newest := testTxn(2, mustDate(2026, time.March, 3), -3000, int64Pointer(10_000))
	older := testTxn(1, mustDate(2026, time.March, 2), 5000, nil)

	entries := ComputeRunning(testMoney(15_000), []model.TransactionItem{newest, older})

	if got, want := requireEntry(t, entries, newest.ID).Balance, testMoney(10_000); got != want {
		t.Fatalf("newest balance = %v, want %v", got, want)
	}
	difference := requireEntry(t, entries, newest.ID).BankDifference
	if difference == nil || *difference != testMoney(-5000) {
		t.Fatalf("newest difference = %v, want -5000", difference)
	}
	if got, want := requireEntry(t, entries, older.ID).Balance, testMoney(13_000); got != want {
		t.Fatalf("older balance = %v, want %v", got, want)
	}
	if requireEntry(t, entries, older.ID).BankDifference != nil {
		t.Fatal("older row has a bank difference")
	}
}

func TestAnEqualBankBalanceIsNotAMismatch(t *testing.T) {
	newest := testTxn(1, mustDate(2026, time.March, 3), -3000, int64Pointer(15_050))

	entries := ComputeRunning(testMoney(15_050), []model.TransactionItem{newest})

	if got, want := requireEntry(t, entries, newest.ID).Balance, testMoney(15_050); got != want {
		t.Fatalf("balance = %v, want %v", got, want)
	}
	if requireEntry(t, entries, newest.ID).BankDifference != nil {
		t.Fatal("an equal bank balance is a mismatch")
	}
}

func TestWithoutBankValuesThePerDayBalancesMatchBalanceHistory(t *testing.T) {
	d1 := mustDate(2026, time.March, 1)
	d2 := mustDate(2026, time.March, 2)
	d3 := mustDate(2026, time.March, 3)
	newestFirst := []model.TransactionItem{
		testTxn(4, d3, -3000, nil),
		testTxn(3, d3, -2000, nil),
		testTxn(2, d2, 10_000, nil),
		testTxn(1, d1, -5000, nil),
	}
	current := testMoney(12_000)
	entries := ComputeRunning(current, newestFirst)

	for _, day := range []civil.Date{d1, d2, d3} {
		var newestThatDay model.TransactionItem
		for _, transaction := range newestFirst {
			if transaction.Date == day {
				newestThatDay = transaction
				break
			}
		}
		points := ComputeHistory(current, newestFirst, day, day)
		if len(points) != 1 {
			t.Fatalf("history for %s has %d points", day, len(points))
		}
		if got, want := requireEntry(t, entries, newestThatDay.ID).Balance, points[0].Balance; got != want {
			t.Fatalf("balance for %s = %v, want %v", day, got, want)
		}
	}
}

func TestADayWhoseListOrderDiffersFromTheBanksBookingOrderIsPlacedArithmetically(t *testing.T) {
	sep30 := mustDate(2026, time.September, 30)
	oct1 := mustDate(2026, time.October, 1)
	minus1000 := testTxn(1, sep30, -100_000, int64Pointer(300_000))
	minus500 := testTxn(2, oct1, -50_000, int64Pointer(248_000))
	minus700 := testTxn(3, oct1, -70_000, int64Pointer(178_000))
	minus20 := testTxn(4, oct1, -2000, int64Pointer(298_000))
	newestFirst := []model.TransactionItem{minus20, minus700, minus500, minus1000}

	entries := ComputeRunning(testMoney(178_000), newestFirst)

	for id, entry := range entries {
		if entry.BankDifference != nil {
			t.Fatalf("transaction %d has a bank difference", id)
		}
	}
	if got, want := requireEntry(t, entries, minus20.ID).Balance, testMoney(178_000); got != want {
		t.Fatalf("minus20 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus700.ID).Balance, testMoney(180_000); got != want {
		t.Fatalf("minus700 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus500.ID).Balance, testMoney(250_000); got != want {
		t.Fatalf("minus500 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus1000.ID).Balance, testMoney(300_000); got != want {
		t.Fatalf("minus1000 balance = %v, want %v", got, want)
	}
	if got, want := HistoryFromRunning(newestFirst, entries, sep30, oct1), []BalancePoint{
		{Date: sep30, Balance: testMoney(300_000)},
		{Date: oct1, Balance: testMoney(178_000)},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

func TestADayReconcilesFromItsStartBalanceEvenWhenItsEndBalanceMatchesNoBankValue(t *testing.T) {
	sep30 := mustDate(2026, time.September, 30)
	oct1 := mustDate(2026, time.October, 1)
	minus1000 := testTxn(1, sep30, -100_000, int64Pointer(300_000))
	minus500 := testTxn(2, oct1, -50_000, int64Pointer(248_000))
	minus700 := testTxn(3, oct1, -70_000, int64Pointer(178_000))
	minus20 := testTxn(4, oct1, -2000, int64Pointer(298_000))
	plus100 := testTxn(5, oct1, 10_000, nil)

	entries := ComputeRunning(testMoney(188_000), []model.TransactionItem{
		plus100,
		minus20,
		minus700,
		minus500,
		minus1000,
	})

	for id, entry := range entries {
		if entry.BankDifference != nil {
			t.Fatalf("transaction %d has a bank difference", id)
		}
	}
	if got, want := requireEntry(t, entries, plus100.ID).Balance, testMoney(188_000); got != want {
		t.Fatalf("plus100 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus20.ID).Balance, testMoney(178_000); got != want {
		t.Fatalf("minus20 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus700.ID).Balance, testMoney(180_000); got != want {
		t.Fatalf("minus700 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus500.ID).Balance, testMoney(250_000); got != want {
		t.Fatalf("minus500 balance = %v, want %v", got, want)
	}
	if got, want := requireEntry(t, entries, minus1000.ID).Balance, testMoney(300_000); got != want {
		t.Fatalf("minus1000 balance = %v, want %v", got, want)
	}
}

func TestADayReconcilesWhenABankValueEqualsItsEndBalance(t *testing.T) {
	sep30 := mustDate(2026, time.September, 30)
	oct1 := mustDate(2026, time.October, 1)
	minus1000 := testTxn(1, sep30, -100_000, int64Pointer(300_000))
	plus100 := testTxn(2, oct1, 10_000, nil)
	minus500 := testTxn(3, oct1, -50_000, int64Pointer(258_000))
	minus700 := testTxn(4, oct1, -70_000, int64Pointer(188_000))
	minus20 := testTxn(5, oct1, -2000, int64Pointer(308_000))

	entries := ComputeRunning(testMoney(188_000), []model.TransactionItem{
		minus20,
		minus700,
		minus500,
		plus100,
		minus1000,
	})

	for id, entry := range entries {
		if entry.BankDifference != nil {
			t.Fatalf("transaction %d has a bank difference", id)
		}
	}
	if got, want := requireEntry(t, entries, minus1000.ID).Balance, testMoney(300_000); got != want {
		t.Fatalf("minus1000 balance = %v, want %v", got, want)
	}
}

func TestAFullyBankCoveredDayWithARealGapNoLongerReconcilesFromOneMatchingEnd(t *testing.T) {
	sep30 := mustDate(2026, time.September, 30)
	oct1 := mustDate(2026, time.October, 1)
	minus1000 := testTxn(1, sep30, -100_000, int64Pointer(300_000))
	minus700 := testTxn(2, oct1, -70_000, int64Pointer(178_000))
	minus20 := testTxn(3, oct1, -2000, int64Pointer(298_000))

	entries := ComputeRunning(testMoney(178_000), []model.TransactionItem{minus20, minus700, minus1000})

	flagged := false
	for _, transaction := range []model.TransactionItem{minus20, minus700} {
		if requireEntry(t, entries, transaction.ID).BankDifference != nil {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("at least one 10-01 row is flagged with a bank difference")
	}
	sum := int64(0)
	for _, entry := range entries {
		if entry.BankDifference != nil {
			sum += entry.BankDifference.Minor
		}
	}
	if got, want := sum, int64(50_000); got != want {
		t.Fatalf("difference sum = %d, want %d", got, want)
	}
}

func TestAFullyBankCoveredDayWhereOnlyTheEndMatchesKeepsABankDifference(t *testing.T) {
	minus20 := testTxn(2, mustDate(2026, time.October, 1), -2000, int64Pointer(15_000))
	minus10 := testTxn(1, mustDate(2026, time.October, 1), -1000, int64Pointer(10_000))

	entries := ComputeRunning(testMoney(10_000), []model.TransactionItem{minus10, minus20})

	for _, entry := range entries {
		if entry.BankDifference != nil {
			return
		}
	}
	t.Fatal("no entry has a bank difference")
}
