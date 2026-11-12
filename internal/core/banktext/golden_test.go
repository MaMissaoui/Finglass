package banktext

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

type goldenBankTextRow struct {
	File               string  `json:"file"`
	Index              int     `json:"index"`
	PartnerName        *string `json:"partner_name"`
	Purpose            *string `json:"purpose"`
	DisplayPartner     *string `json:"display_partner"`
	DisplayPurpose     *string `json:"display_purpose"`
	DisplayDescription string  `json:"display_description"`
	SecondaryLine      *string `json:"secondary_line"`
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
		AccountIBAN:  cloneString(accountIBAN),
		Date:         booking.Date,
		Amount:       bookingMoney(booking),
		BalanceAfter: balanceAfter,
		Currency:     currency,
		PartnerName:  cloneString(booking.PartnerName),
		Purpose:      cloneString(booking.Purpose),
		TaxSource:    model.TaxSourceNone,
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalOptional(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func checkOptional(t *testing.T, file, field string, got, want *string) {
	t.Helper()
	if !equalOptional(got, want) {
		t.Errorf("%s: %s = %v, want %v", file, field, showOptional(got), showOptional(want))
	}
}

func showOptional(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func TestBankTextGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "bank_text", "bookings.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenBankTextRow
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) == 0 {
		t.Fatal("the bank-text golden is empty")
	}

	inputDir := filepath.Join(root, "testdata", "inputs", "import")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatal(err)
	}

	rows := make([]goldenBankTextRow, 0, len(golden))
	nextID := int64(1)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name[0] == '.' {
			continue
		}
		bytes, err := os.ReadFile(filepath.Join(inputDir, name)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		statement, err := imports.Read(name, bytes, nil, goldenToday(t))
		if err != nil {
			continue
		}
		for index, booking := range statement.Bookings {
			item := itemFromBooking(nextID, booking, statement.AccountIBAN)
			nextID++
			rows = append(rows, goldenBankTextRow{
				File:               name,
				Index:              index,
				PartnerName:        cloneString(booking.PartnerName),
				Purpose:            cloneString(booking.Purpose),
				DisplayPartner:     DisplayPartner(item.PartnerName),
				DisplayPurpose:     DisplayPurpose(item.Purpose),
				DisplayDescription: DisplayDescription(&item),
				SecondaryLine:      SecondaryLine(&item),
			})
		}
	}

	if len(rows) != len(golden) {
		t.Fatalf("bank-text rows = %d, golden = %d", len(rows), len(golden))
	}
	for index := range golden {
		want := golden[index]
		got := rows[index]
		file := want.File
		if got.File != want.File {
			t.Errorf("%s: file = %q, want %q", file, got.File, want.File)
		}
		if got.Index != want.Index {
			t.Errorf("%s: index = %d, want %d", file, got.Index, want.Index)
		}
		checkOptional(t, file, "partner_name", got.PartnerName, want.PartnerName)
		checkOptional(t, file, "purpose", got.Purpose, want.Purpose)
		checkOptional(t, file, "display_partner", got.DisplayPartner, want.DisplayPartner)
		checkOptional(t, file, "display_purpose", got.DisplayPurpose, want.DisplayPurpose)
		if got.DisplayDescription != want.DisplayDescription {
			t.Errorf("%s: display_description = %q, want %q", file, got.DisplayDescription, want.DisplayDescription)
		}
		checkOptional(t, file, "secondary_line", got.SecondaryLine, want.SecondaryLine)
	}
}
