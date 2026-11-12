package imports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type goldenDedupMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenDedupStep struct {
	Step               string  `json:"step"`
	Imported           int     `json:"imported"`
	Duplicates         int     `json:"duplicates"`
	Categorized        int     `json:"categorized"`
	ProbableDuplicates int     `json:"probable_duplicates"`
	PreviouslyDeleted  int     `json:"previously_deleted"`
	InvalidAmount      int     `json:"invalid_amount"`
	WrongCurrency      int     `json:"wrong_currency"`
	BalanceUpdated     *string `json:"balance_updated"`
	Summary            string  `json:"summary"`
}

type goldenDedupStored struct {
	Date        string           `json:"date"`
	Amount      goldenDedupMoney `json:"amount"`
	PartnerName *string          `json:"partner_name"`
	Purpose     *string          `json:"purpose"`
	ImportHash  string           `json:"import_hash"`
	IsNew       bool             `json:"is_new"`
}

type goldenDedupReports struct {
	Steps  []goldenDedupStep   `json:"steps"`
	Stored []goldenDedupStored `json:"stored"`
}

func TestDedupReportsMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "dedup", "reports.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var want goldenDedupReports
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}

	d := g3iDatabase(t)
	accountID := g3iAccount(t, d, g3iStringPointer("DE89370400440532013000"), 0, model.AccountTypeChecking)
	food := g3iCategory(t, d, "Food")
	g3iRule(t, d, "bäcker|kaffee", food, 0, model.RuleFieldAny, model.RuleDirectionAny)

	today := testDate(t, 2026, 10, 5)
	inputDir := filepath.Join(root, "testdata", "inputs", "dedup")
	readStatement := func(name string) ParsedStatement {
		t.Helper()
		bytes, err := os.ReadFile(filepath.Join(inputDir, name)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		statement, err := Read(name, bytes, nil, today)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return statement
	}
	a := readStatement("dedup_a.csv")
	b := readStatement("dedup_b.csv")
	c := readStatement("dedup_c.csv")

	records := make([]ImportReport, 0, 5)
	records = append(records, g3iImport(t, d, accountID, a, true, today))
	records = append(records, g3iImport(t, d, accountID, a, true, today))
	records = append(records, g3iImport(t, d, accountID, b, true, today))
	records = append(records, g3iImport(t, d, accountID, c, true, today))

	stored, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	var cafeID model.TransactionID
	foundCafe := false
	for _, booking := range stored {
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
	records = append(records, g3iImport(t, d, accountID, c, true, today))

	if len(want.Steps) != len(records) {
		t.Fatalf("steps = %d, want %d", len(records), len(want.Steps))
	}
	for index, expected := range want.Steps {
		got := records[index]
		if expected.Step == "" {
			t.Errorf("steps[%d] has no step name", index)
		}
		if got.Imported != expected.Imported {
			t.Errorf("%s: imported = %d, want %d", expected.Step, got.Imported, expected.Imported)
		}
		if got.Duplicates != expected.Duplicates {
			t.Errorf("%s: duplicates = %d, want %d", expected.Step, got.Duplicates, expected.Duplicates)
		}
		if got.Categorized != expected.Categorized {
			t.Errorf("%s: categorized = %d, want %d", expected.Step, got.Categorized, expected.Categorized)
		}
		if got.ProbableDuplicates != expected.ProbableDuplicates {
			t.Errorf("%s: probable_duplicates = %d, want %d", expected.Step, got.ProbableDuplicates, expected.ProbableDuplicates)
		}
		if got.PreviouslyDeleted != expected.PreviouslyDeleted {
			t.Errorf("%s: previously_deleted = %d, want %d", expected.Step, got.PreviouslyDeleted, expected.PreviouslyDeleted)
		}
		if got.InvalidAmount != expected.InvalidAmount {
			t.Errorf("%s: invalid_amount = %d, want %d", expected.Step, got.InvalidAmount, expected.InvalidAmount)
		}
		if got.WrongCurrency != expected.WrongCurrency {
			t.Errorf("%s: wrong_currency = %d, want %d", expected.Step, got.WrongCurrency, expected.WrongCurrency)
		}
		switch {
		case expected.BalanceUpdated == nil && got.BalanceUpdated != nil:
			t.Errorf("%s: balance_updated = %s, want null", expected.Step, got.BalanceUpdated)
		case expected.BalanceUpdated != nil && got.BalanceUpdated == nil:
			t.Errorf("%s: balance_updated = null, want %s", expected.Step, *expected.BalanceUpdated)
		case expected.BalanceUpdated != nil && got.BalanceUpdated.String() != *expected.BalanceUpdated:
			t.Errorf("%s: balance_updated = %s, want %s", expected.Step, got.BalanceUpdated, *expected.BalanceUpdated)
		}
		if got.Summary() != expected.Summary {
			t.Errorf("%s: summary = %q, want %q", expected.Step, got.Summary(), expected.Summary)
		}
	}

	stored, err = transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	if len(stored) != len(want.Stored) {
		t.Fatalf("stored = %d, want %d", len(stored), len(want.Stored))
	}
	for index, expected := range want.Stored {
		got := stored[index]
		if got.BookingDate.String() != expected.Date {
			t.Errorf("stored[%d].date = %s, want %s", index, got.BookingDate, expected.Date)
		}
		if got.Amount.Minor != expected.Amount.Minor {
			t.Errorf("stored[%d].amount.minor = %d, want %d", index, got.Amount.Minor, expected.Amount.Minor)
		}
		if got.Amount.Currency.String() != expected.Amount.Currency {
			t.Errorf("stored[%d].amount.currency = %s, want %s", index, got.Amount.Currency, expected.Amount.Currency)
		}
		if plain := money.FormatPlain(got.Amount); plain != expected.Amount.Plain {
			t.Errorf("stored[%d].amount.plain = %s, want %s", index, plain, expected.Amount.Plain)
		}
		if !reflect.DeepEqual(got.PartnerName, expected.PartnerName) {
			t.Errorf("stored[%d].partner_name = %v, want %v", index, got.PartnerName, expected.PartnerName)
		}
		if !reflect.DeepEqual(got.Purpose, expected.Purpose) {
			t.Errorf("stored[%d].purpose = %v, want %v", index, got.Purpose, expected.Purpose)
		}
		if got.ImportHash != expected.ImportHash {
			t.Errorf("stored[%d].import_hash = %s, want %s", index, got.ImportHash, expected.ImportHash)
		}
		if got.IsNew != expected.IsNew {
			t.Errorf("stored[%d].is_new = %v, want %v", index, got.IsNew, expected.IsNew)
		}
	}
}
