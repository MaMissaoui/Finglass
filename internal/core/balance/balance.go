// Package balance reconstructs an account's running balances and its end-of-day
// balance history.
//
// It ports crates/finglass-core/src/balance/ (running.rs and history.rs).
package balance

import (
	"math"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// Entry is the reconstructed balance of one booking.
type Entry struct {
	// Balance is the balance after the booking.
	Balance money.Money
	// BankDifference is the bank balance minus the computed balance, when they
	// differ.
	BankDifference *money.Money
}

// ComputeRunning returns the balance after each booking of one account, by
// transaction id.
//
// newestFirst is every booking of the account in list order (date descending,
// id descending); current is the account's balance after all bookings,
// future-dated ones included.
func ComputeRunning(current money.Money, newestFirst []model.TransactionItem) map[model.TransactionID]Entry {
	currency := current.Currency
	result := make(map[model.TransactionID]Entry)
	endOfDay := current.Minor

	// Encounter order of the date-descending list: days newest first, each
	// day's rows in list order.
	type dayRows struct {
		date civil.Date
		rows []model.TransactionItem
	}
	days := make([]dayRows, 0, len(newestFirst))
	for _, transaction := range newestFirst {
		if count := len(days); count > 0 && days[count-1].date == transaction.Date {
			days[count-1].rows = append(days[count-1].rows, transaction)
			continue
		}
		days = append(days, dayRows{
			date: transaction.Date,
			rows: []model.TransactionItem{transaction},
		})
	}

	for _, day := range days {
		startOfDay := endOfDay
		for _, transaction := range day.rows {
			startOfDay = subtractSaturating(startOfDay, transaction.Amount.Minor)
		}

		hasBank := 0
		for _, transaction := range day.rows {
			if transaction.BalanceAfter != nil {
				hasBank++
			}
		}
		var reconciles bool
		switch {
		case hasBank == 0:
			reconciles = true
		case hasBank == len(day.rows):
			reconciles = hasEndBalance(day.rows, endOfDay) && hasStartBalance(day.rows, startOfDay)
		default:
			reconciles = hasEndBalance(day.rows, endOfDay) || hasStartBalance(day.rows, startOfDay)
		}

		balance := endOfDay
		if reconciles {
			for _, transaction := range day.rows {
				result[transaction.ID] = Entry{
					Balance: money.Money{Minor: balance, Currency: currency},
				}
				balance = subtractSaturating(balance, transaction.Amount.Minor)
			}
		} else {
			for _, transaction := range day.rows {
				computed := balance
				if bank := transaction.BalanceAfter; bank != nil {
					var difference *money.Money
					if bank.Minor != computed {
						value := money.Money{
							Minor:    subtractSaturating(bank.Minor, computed),
							Currency: currency,
						}
						difference = &value
					}
					result[transaction.ID] = Entry{
						Balance:        money.Money{Minor: bank.Minor, Currency: currency},
						BankDifference: difference,
					}
					balance = bank.Minor
				} else {
					result[transaction.ID] = Entry{
						Balance: money.Money{Minor: computed, Currency: currency},
					}
				}
				balance = subtractSaturating(balance, transaction.Amount.Minor)
			}
		}

		endOfDay = balance
	}

	return result
}

func hasEndBalance(rows []model.TransactionItem, endOfDay int64) bool {
	for _, transaction := range rows {
		if bank := transaction.BalanceAfter; bank != nil && bank.Minor == endOfDay {
			return true
		}
	}
	return false
}

func hasStartBalance(rows []model.TransactionItem, startOfDay int64) bool {
	for _, transaction := range rows {
		if bank := transaction.BalanceAfter; bank != nil &&
			subtractSaturating(bank.Minor, transaction.Amount.Minor) == startOfDay {
			return true
		}
	}
	return false
}

// BalancePoint is one end-of-day balance for a chart.
type BalancePoint struct {
	// Date is the day the balance belongs to.
	Date civil.Date
	// Balance is the end-of-day balance.
	Balance money.Money
}

// ComputeHistory returns end-of-day balances from from to to, reconstructed
// backwards from current.
//
// Bookings dated after to are undone first.
func ComputeHistory(current money.Money, transactions []model.TransactionItem, from, to civil.Date) []BalancePoint {
	if from.After(to) {
		return nil
	}

	byDay := make(map[civil.Date]int64)
	for _, transaction := range transactions {
		byDay[transaction.Date] = addSaturating(byDay[transaction.Date], transaction.Amount.Minor)
	}
	afterTo := int64(0)
	for _, transaction := range transactions {
		if transaction.Date.After(to) {
			afterTo = addSaturating(afterTo, transaction.Amount.Minor)
		}
	}
	balance := subtractSaturating(current.Minor, afterTo)
	currency := current.Currency

	points := make([]BalancePoint, 0)
	day := to
	for {
		points = append(points, BalancePoint{
			Date:    day,
			Balance: money.Money{Minor: balance, Currency: currency},
		})
		balance = subtractSaturating(balance, byDay[day])
		if day == from {
			break
		}
		day = day.AddDays(-1)
	}
	reversePoints(points)

	return points
}

// HistoryFromRunning returns one balance point per day from from to to, oldest
// first, read from the running balances.
//
// newestFirst is every booking of one account in list order (date descending,
// id descending); running is ComputeRunning for that same list.
func HistoryFromRunning(
	newestFirst []model.TransactionItem,
	running map[model.TransactionID]Entry,
	from, to civil.Date,
) []BalancePoint {
	if from.After(to) || len(newestFirst) == 0 {
		return nil
	}

	ascending := make([]model.TransactionItem, 0, len(newestFirst))
	for index := len(newestFirst) - 1; index >= 0; index-- {
		ascending = append(ascending, newestFirst[index])
	}
	oldest := ascending[0]
	currency := oldest.Amount.Currency
	var current *int64
	if entry, ok := running[oldest.ID]; ok {
		balance := subtractSaturating(entry.Balance.Minor, oldest.Amount.Minor)
		current = &balance
	}
	points := make([]BalancePoint, 0)
	index := 0
	day := from

	for {
		for index < len(ascending) && !ascending[index].Date.After(day) {
			if entry, ok := running[ascending[index].ID]; ok {
				balance := entry.Balance.Minor
				current = &balance
			}
			index++
		}
		if current != nil {
			points = append(points, BalancePoint{
				Date:    day,
				Balance: money.Money{Minor: *current, Currency: currency},
			})
		}
		if day == to {
			break
		}
		day = day.AddDays(1)
	}

	return points
}

func reversePoints(points []BalancePoint) {
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
}

// addSaturating adds two minor-unit amounts without silently wrapping. Rust
// panics on overflow in a debug build and has no release behaviour to match, so
// the value is clamped instead of wrapping.
func addSaturating(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}

// subtractSaturating subtracts two minor-unit amounts without silently
// wrapping.
func subtractSaturating(left, right int64) int64 {
	if right > 0 && left < math.MinInt64+right {
		return math.MinInt64
	}
	if right < 0 && left > math.MaxInt64+right {
		return math.MaxInt64
	}
	return left - right
}
