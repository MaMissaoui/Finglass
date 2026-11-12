package recurring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

type goldenMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenPayment struct {
	Name          string      `json:"name"`
	Cadence       string      `json:"cadence"`
	TypicalAmount goldenMoney `json:"typical_amount"`
	FixedAmount   bool        `json:"fixed_amount"`
	LastDate      string      `json:"last_date"`
	NextDate      string      `json:"next_date"`
	Occurrences   int         `json:"occurrences"`
	CategoryName  *string     `json:"category_name"`
	Currency      string      `json:"currency"`
	IsTransfer    bool        `json:"is_transfer"`
	IsIncome      bool        `json:"is_income"`
	YearlyAmount  goldenMoney `json:"yearly_amount"`
}

type goldenRecurringFile struct {
	File     string          `json:"file"`
	Today    string          `json:"today"`
	Detected []goldenPayment `json:"detected"`
	Upcoming []goldenPayment `json:"upcoming_30_days"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func goldenToday(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, 10, 15)
	if !ok {
		t.Fatal("invalid golden date")
	}
	return date
}

func currencyOf(value *string) money.Currency {
	if value != nil {
		if currency, err := money.NewCurrency(*value); err == nil {
			return currency
		}
	}
	return money.EUR
}

func bookingMoney(booking imports.ImportedBooking) money.Money {
	currency := currencyOf(booking.Currency)
	minor := int64(0)
	if amount, err := imports.AmountToMoney(booking.Amount, currency); err == nil {
		minor = amount.Minor
	}
	return money.Money{Minor: minor, Currency: currency}
}

func itemFromBooking(id int64, booking imports.ImportedBooking, accountIBAN *string) model.TransactionItem {
	currency := currencyOf(booking.Currency)
	var balanceAfter *money.Money
	if booking.BalanceAfter != nil {
		minor := int64(0)
		if amount, err := imports.AmountToMoney(*booking.BalanceAfter, currency); err == nil {
			minor = amount.Minor
		}
		value := money.Money{Minor: minor, Currency: currency}
		balanceAfter = &value
	}
	return model.TransactionItem{
		ID:           model.TransactionID(id),
		AccountID:    model.AccountID(1),
		AccountIBAN:  cloneOptional(accountIBAN),
		Date:         booking.Date,
		Amount:       bookingMoney(booking),
		BalanceAfter: balanceAfter,
		Currency:     currency,
		PartnerName:  cloneOptional(booking.PartnerName),
		Purpose:      cloneOptional(booking.Purpose),
		TaxSource:    model.TaxSourceNone,
	}
}

func recurringCategory(partner *string) *string {
	if partner == nil {
		return nil
	}
	var name string
	switch {
	case containsSubstring(*partner, "NETFLIX") || containsSubstring(*partner, "Spotify"):
		name = "Phone, internet & media"
	case containsSubstring(*partner, "Employer"):
		name = "Salary"
	case containsSubstring(*partner, "Stadtwerke"):
		name = "Energy"
	case containsSubstring(*partner, "HUK"):
		name = "Insurance"
	case containsSubstring(*partner, "ADAC"):
		name = "Leisure & clubs"
	case containsSubstring(*partner, "REWE"):
		name = "Groceries"
	default:
		return nil
	}
	return &name
}

func containsSubstring(text, part string) bool {
	if len(part) == 0 {
		return true
	}
	for index := 0; index+len(part) <= len(text); index++ {
		if text[index:index+len(part)] == part {
			return true
		}
	}
	return false
}

func checkGoldenMoney(t *testing.T, file, field string, got money.Money, want goldenMoney) {
	t.Helper()
	if got.Minor != want.Minor {
		t.Errorf("%s: %s.minor = %d, want %d", file, field, got.Minor, want.Minor)
	}
	if got.Currency.String() != want.Currency {
		t.Errorf("%s: %s.currency = %q, want %q", file, field, got.Currency.String(), want.Currency)
	}
	if plain := money.FormatPlain(got); plain != want.Plain {
		t.Errorf("%s: %s.plain = %q, want %q", file, field, plain, want.Plain)
	}
}

func checkGoldenPayment(t *testing.T, file string, index int, got RecurringPayment, want goldenPayment) {
	t.Helper()
	prefix := "detected[" + intToString(index) + "]."
	if got.Name != want.Name {
		t.Errorf("%s: %sname = %q, want %q", file, prefix, got.Name, want.Name)
	}
	if string(got.Cadence) != want.Cadence {
		t.Errorf("%s: %scadence = %q, want %q", file, prefix, got.Cadence, want.Cadence)
	}
	checkGoldenMoney(t, file, prefix+"typical_amount", got.TypicalAmount, want.TypicalAmount)
	if got.FixedAmount != want.FixedAmount {
		t.Errorf("%s: %sfixed_amount = %t, want %t", file, prefix, got.FixedAmount, want.FixedAmount)
	}
	if got.LastDate.String() != want.LastDate {
		t.Errorf("%s: %slast_date = %q, want %q", file, prefix, got.LastDate, want.LastDate)
	}
	if got.NextDate.String() != want.NextDate {
		t.Errorf("%s: %snext_date = %q, want %q", file, prefix, got.NextDate, want.NextDate)
	}
	if got.Occurrences != want.Occurrences {
		t.Errorf("%s: %soccurrences = %d, want %d", file, prefix, got.Occurrences, want.Occurrences)
	}
	checkOptionalString(t, file, prefix+"category_name", got.CategoryName, want.CategoryName)
	if got.Currency.String() != want.Currency {
		t.Errorf("%s: %scurrency = %q, want %q", file, prefix, got.Currency.String(), want.Currency)
	}
	if got.IsTransfer != want.IsTransfer {
		t.Errorf("%s: %sis_transfer = %t, want %t", file, prefix, got.IsTransfer, want.IsTransfer)
	}
	if got.IsIncome() != want.IsIncome {
		t.Errorf("%s: %sis_income = %t, want %t", file, prefix, got.IsIncome(), want.IsIncome)
	}
	checkGoldenMoney(t, file, prefix+"yearly_amount", got.YearlyAmount(), want.YearlyAmount)
}

func checkOptionalString(t *testing.T, file, field string, got, want *string) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s: %s = %v, want %v", file, field, showOptional(got), showOptional(want))
		}
		return
	}
	if *got != *want {
		t.Errorf("%s: %s = %q, want %q", file, field, *got, *want)
	}
}

func showOptional(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func intToString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func TestRecurringGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "recurring", "detections.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenRecurringFile
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) == 0 {
		t.Fatal("the recurring golden is empty")
	}

	files := []string{"monthly.csv", "quarterly.csv", "irregular.csv"}
	if len(golden) != len(files) {
		t.Fatalf("recurring golden files = %d, want %d", len(golden), len(files))
	}
	today := goldenToday(t)
	for fileIndex, name := range files {
		want := golden[fileIndex]
		if want.File != name {
			t.Fatalf("golden[%d].file = %q, want %q", fileIndex, want.File, name)
		}
		path := filepath.Join(root, "testdata", "inputs", "recurring", name)
		bytes, err := os.ReadFile(path) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		statement, err := imports.Read(name, bytes, nil, today)
		if err != nil {
			t.Fatalf("%s: Read: %v", name, err)
		}
		items := make([]model.TransactionItem, 0, len(statement.Bookings))
		for index, booking := range statement.Bookings {
			item := itemFromBooking(int64(index+1), booking, nil)
			if category := recurringCategory(booking.PartnerName); category != nil {
				categoryID := model.CategoryID(index + 1)
				item.CategoryID = &categoryID
				item.CategoryName = category
			}
			items = append(items, item)
		}

		detected := (RecurringDetector{}).Detect(items, today)
		upcoming := (RecurringDetector{}).Upcoming(detected, today, 30)

		if want.Today != today.String() {
			t.Errorf("%s: today = %q, want %q", name, today, want.Today)
		}
		if len(detected) != len(want.Detected) {
			t.Errorf("%s: detected = %d, want %d", name, len(detected), len(want.Detected))
		} else {
			for index := range want.Detected {
				checkGoldenPayment(t, name, index, detected[index], want.Detected[index])
			}
		}
		if len(upcoming) != len(want.Upcoming) {
			t.Errorf("%s: upcoming_30_days = %d, want %d", name, len(upcoming), len(want.Upcoming))
		} else {
			for index := range want.Upcoming {
				checkGoldenPayment(t, name+".upcoming", index, upcoming[index], want.Upcoming[index])
			}
		}
	}
}
