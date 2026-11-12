package balance

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

const (
	balanceChartsGolden = "../../../testdata/golden/balance/charts.json"
	importStatements    = "../../../testdata/golden/import/statements.json"
)

type goldenMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenRunningEntry struct {
	ID             int64        `json:"id"`
	Date           string       `json:"date"`
	Amount         goldenMoney  `json:"amount"`
	Balance        goldenMoney  `json:"balance"`
	BankDifference *goldenMoney `json:"bank_difference"`
}

type goldenBalancePoint struct {
	Date    string      `json:"date"`
	Balance goldenMoney `json:"balance"`
}

type goldenChart struct {
	File            string               `json:"file"`
	CurrentBalance  goldenMoney          `json:"current_balance"`
	From            string               `json:"from"`
	To              string               `json:"to"`
	Running         []goldenRunningEntry `json:"running"`
	History         []goldenBalancePoint `json:"history"`
	HistoryComputed []goldenBalancePoint `json:"history_compute"`
}

type goldenBooking struct {
	Date         string  `json:"date"`
	Amount       string  `json:"amount"`
	Currency     *string `json:"currency"`
	PartnerName  *string `json:"partner_name"`
	Purpose      *string `json:"purpose"`
	BalanceAfter *string `json:"balance_after"`
}

type goldenStatement struct {
	AccountIBAN *string         `json:"account_iban"`
	Bookings    []goldenBooking `json:"bookings"`
}

type goldenImportFile struct {
	File      string           `json:"file"`
	OK        bool             `json:"ok"`
	Statement *goldenStatement `json:"statement"`
}

func decodeGoldenFile(t *testing.T, path string, target any) {
	t.Helper()
	//nolint:gosec // the path is one of this test's fixed testdata constants.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func goldenDate(t *testing.T, text string) civil.Date {
	t.Helper()
	date, err := civil.ParseISO(text)
	if err != nil {
		t.Fatalf("invalid golden date %q: %v", text, err)
	}
	return date
}

func goldenCurrency(optional *string) money.Currency {
	if optional == nil {
		return money.EUR
	}
	currency, err := money.NewCurrency(*optional)
	if err != nil {
		return money.EUR
	}
	return currency
}

// goldenBookingMoney mirrors the golden generator's amount_to_money +
// unwrap_or(0) handling.
func goldenBookingMoney(text string, currency money.Currency) money.Money {
	value, err := money.ParseDecimal(text, currency)
	if err != nil {
		return money.Money{Minor: 0, Currency: currency}
	}
	return value
}

func checkGoldenMoney(t *testing.T, label string, got money.Money, want goldenMoney) {
	t.Helper()
	if got.Minor != want.Minor {
		t.Fatalf("%s minor = %d, want %d", label, got.Minor, want.Minor)
	}
	if code := got.Currency.String(); code != want.Currency {
		t.Fatalf("%s currency = %s, want %s", label, code, want.Currency)
	}
	if plain := money.FormatPlain(got); plain != want.Plain {
		t.Fatalf("%s plain = %s, want %s", label, plain, want.Plain)
	}
}

func checkGoldenPoints(t *testing.T, label string, got []BalancePoint, want []goldenBalancePoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d", label, len(got), len(want))
	}
	for index := range want {
		date := goldenDate(t, want[index].Date)
		if got[index].Date != date {
			t.Fatalf("%s[%d] date = %s, want %s", label, index, got[index].Date, date)
		}
		checkGoldenMoney(t, label+"["+want[index].Date+"]", got[index].Balance, want[index].Balance)
	}
}

func itemsFromGoldenStatement(t *testing.T, statement *goldenStatement) []model.TransactionItem {
	t.Helper()
	items := make([]model.TransactionItem, 0, len(statement.Bookings))
	for index, booking := range statement.Bookings {
		currency := goldenCurrency(booking.Currency)
		var balanceAfter *money.Money
		if booking.BalanceAfter != nil {
			value := goldenBookingMoney(*booking.BalanceAfter, currency)
			balanceAfter = &value
		}
		items = append(items, model.TransactionItem{
			ID:           model.TransactionID(index + 1),
			AccountID:    model.AccountID(1),
			AccountIBAN:  statement.AccountIBAN,
			Date:         goldenDate(t, booking.Date),
			Amount:       goldenBookingMoney(booking.Amount, currency),
			BalanceAfter: balanceAfter,
			Currency:     currency,
			PartnerName:  booking.PartnerName,
			Purpose:      booking.Purpose,
			TaxSource:    model.TaxSourceNone,
		})
	}
	sort.SliceStable(items, func(left, right int) bool {
		if order := items[left].Date.Compare(items[right].Date); order != 0 {
			return order < 0
		}
		return items[left].ID < items[right].ID
	})
	return items
}

func TestBalanceChartsMatchTheRustGolden(t *testing.T) {
	var charts []goldenChart
	decodeGoldenFile(t, balanceChartsGolden, &charts)
	var imports []goldenImportFile
	decodeGoldenFile(t, importStatements, &imports)

	statements := make(map[string]*goldenStatement, len(imports))
	for _, file := range imports {
		if file.OK {
			statements[file.File] = file.Statement
		}
	}

	for _, chart := range charts {
		statement, ok := statements[chart.File]
		if !ok || statement == nil {
			t.Fatalf("%s has no import golden", chart.File)
		}

		items := itemsFromGoldenStatement(t, statement)
		if len(items) == 0 {
			t.Fatalf("%s has no bookings", chart.File)
		}

		currentMinor := int64(0)
		hasBalance := false
		for index := len(items) - 1; index >= 0; index-- {
			if items[index].BalanceAfter != nil {
				currentMinor = items[index].BalanceAfter.Minor
				hasBalance = true
				break
			}
		}
		if !hasBalance {
			currentMinor = 0
			for _, item := range items {
				currentMinor = addSaturating(currentMinor, item.Amount.Minor)
			}
		}
		current := money.Money{Minor: currentMinor, Currency: money.EUR}
		checkGoldenMoney(t, chart.File+" current_balance", current, chart.CurrentBalance)

		from := items[0].Date
		to := items[len(items)-1].Date
		if want := goldenDate(t, chart.From); from != want {
			t.Fatalf("%s from = %s, want %s", chart.File, from, want)
		}
		if want := goldenDate(t, chart.To); to != want {
			t.Fatalf("%s to = %s, want %s", chart.File, to, want)
		}

		newestFirst := make([]model.TransactionItem, 0, len(items))
		for index := len(items) - 1; index >= 0; index-- {
			newestFirst = append(newestFirst, items[index])
		}

		running := ComputeRunning(current, newestFirst)
		if len(running) != len(newestFirst) {
			t.Fatalf("%s running has %d entries, want %d", chart.File, len(running), len(newestFirst))
		}
		if len(chart.Running) != len(newestFirst) {
			t.Fatalf("%s golden running has %d entries, want %d", chart.File, len(chart.Running), len(newestFirst))
		}

		for index, item := range newestFirst {
			want := chart.Running[index]
			label := chart.File + " running[" + want.Date + "]"
			if want.ID != int64(item.ID) {
				t.Fatalf("%s id = %d, want %d", label, want.ID, item.ID)
			}
			if date := goldenDate(t, want.Date); item.Date != date {
				t.Fatalf("%s date = %s, want %s", label, item.Date, date)
			}
			checkGoldenMoney(t, label+" amount", item.Amount, want.Amount)
			entry, ok := running[item.ID]
			if !ok {
				t.Fatalf("%s has no running entry", label)
			}
			checkGoldenMoney(t, label+" balance", entry.Balance, want.Balance)
			if want.BankDifference == nil {
				if entry.BankDifference != nil {
					t.Fatalf("%s bank_difference = %v, want null", label, *entry.BankDifference)
				}
			} else {
				if entry.BankDifference == nil {
					t.Fatalf("%s bank_difference = null, want %v", label, *want.BankDifference)
				}
				checkGoldenMoney(t, label+" bank_difference", *entry.BankDifference, *want.BankDifference)
			}
		}

		history := HistoryFromRunning(newestFirst, running, from, to)
		checkGoldenPoints(t, chart.File+" history", history, chart.History)

		computed := ComputeHistory(current, newestFirst, from, to)
		checkGoldenPoints(t, chart.File+" history_compute", computed, chart.HistoryComputed)
	}
}
