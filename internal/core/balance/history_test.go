package balance

import (
	"reflect"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func chart(newestFirst []model.TransactionItem, currentBalance int64, from, to civil.Date) []BalancePoint {
	running := ComputeRunning(testMoney(currentBalance), newestFirst)
	return HistoryFromRunning(newestFirst, running, from, to)
}

func TestDaysBetweenBookingsCarryThePreviousValueAndSeveralBookingsOnOneDayGiveTheLastBalance(t *testing.T) {
	sameDayOlder := testTxn(1, mustDate(2026, time.March, 3), -2000, nil)
	newest := testTxn(2, mustDate(2026, time.March, 3), -1000, nil)
	oldest := testTxn(3, mustDate(2026, time.March, 1), 5000, nil)
	points := chart(
		[]model.TransactionItem{newest, sameDayOlder, oldest},
		10_000,
		mustDate(2026, time.March, 1),
		mustDate(2026, time.March, 4),
	)

	want := []BalancePoint{
		{Date: mustDate(2026, time.March, 1), Balance: testMoney(13_000)},
		{Date: mustDate(2026, time.March, 2), Balance: testMoney(13_000)},
		{Date: mustDate(2026, time.March, 3), Balance: testMoney(10_000)},
		{Date: mustDate(2026, time.March, 4), Balance: testMoney(10_000)},
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("points = %v, want %v", points, want)
	}
}

func TestADayBeforeTheOldestBookingGivesTheBalanceBeforeIt(t *testing.T) {
	newest := testTxn(2, mustDate(2026, time.March, 3), -1000, nil)
	oldest := testTxn(1, mustDate(2026, time.March, 1), 5000, nil)
	points := chart(
		[]model.TransactionItem{newest, oldest},
		10_000,
		mustDate(2026, time.February, 27),
		mustDate(2026, time.March, 2),
	)

	want := []BalancePoint{
		{Date: mustDate(2026, time.February, 27), Balance: testMoney(6000)},
		{Date: mustDate(2026, time.February, 28), Balance: testMoney(6000)},
		{Date: mustDate(2026, time.March, 1), Balance: testMoney(11_000)},
		{Date: mustDate(2026, time.March, 2), Balance: testMoney(11_000)},
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("points = %v, want %v", points, want)
	}
}

func TestABookingDatedAfterToDoesNotChangeTheValuesUpToTo(t *testing.T) {
	future := testTxn(2, mustDate(2026, time.April, 1), -5000, nil)
	today := testTxn(1, mustDate(2026, time.March, 1), 2000, nil)
	list := []model.TransactionItem{future, today}
	entries := ComputeRunning(testMoney(20_000), list)
	upTo := HistoryFromRunning(list, entries, mustDate(2026, time.March, 1), mustDate(2026, time.March, 5))

	if len(upTo) != 5 {
		t.Fatalf("history has %d points, want 5", len(upTo))
	}
	for _, point := range upTo {
		if point.Balance.Minor != 25_000 {
			t.Fatalf("balance before the future booking = %v, want 25000 (the future booking is ignored before its own date)", point.Balance)
		}
	}

	past := HistoryFromRunning(list, entries, mustDate(2026, time.April, 1), mustDate(2026, time.April, 1))
	if got, want := past, []BalancePoint{
		{Date: mustDate(2026, time.April, 1), Balance: testMoney(20_000)},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("past = %v, want %v", got, want)
	}
}

func TestABankBalanceWinsOverTheReconstructedChainSameAsTheColumn(t *testing.T) {
	newest := testTxn(2, mustDate(2026, time.March, 3), -3000, int64Pointer(7000))
	older := testTxn(1, mustDate(2026, time.March, 2), 5000, nil)
	points := chart(
		[]model.TransactionItem{newest, older},
		10_000,
		mustDate(2026, time.March, 2),
		mustDate(2026, time.March, 3),
	)

	want := []BalancePoint{
		{Date: mustDate(2026, time.March, 2), Balance: testMoney(10_000)},
		{Date: mustDate(2026, time.March, 3), Balance: testMoney(7000)},
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("points = %v, want %v", points, want)
	}
}

func TestNoBookingsAndAFromAfterToGiveAnEmptyList(t *testing.T) {
	if points := HistoryFromRunning(nil, map[model.TransactionID]Entry{},
		mustDate(2026, time.March, 1), mustDate(2026, time.March, 31)); len(points) != 0 {
		t.Fatalf("no bookings gave %v", points)
	}
	transaction := testTxn(1, mustDate(2026, time.March, 1), 1000, nil)
	entries := ComputeRunning(testMoney(1000), []model.TransactionItem{transaction})
	if points := HistoryFromRunning([]model.TransactionItem{transaction}, entries,
		mustDate(2026, time.March, 5), mustDate(2026, time.March, 1)); len(points) != 0 {
		t.Fatalf("from after to gave %v", points)
	}
}
