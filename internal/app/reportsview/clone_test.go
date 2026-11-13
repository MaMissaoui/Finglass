package reportsview

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// TestCloneIsDeep mutates every slice and pointer reachable from the state and
// checks that the original did not change.
func TestCloneIsDeep(t *testing.T) {
	today := testDate(t, 2026, time.October, 15)
	categoryID := model.CategoryID(7)
	accountID := model.AccountID(1)
	iban := "DE89370400440532013000"
	partner := "Apotheke"
	purpose := "Rechnung"
	categoryName := "Health"
	categoryColor := "#123456"
	taxRule := "apotheke"
	taxOverride := true
	errorText := "error"
	messageText := "message"

	transaction := model.TransactionItem{
		ID:             11,
		AccountID:      accountID,
		AccountIBAN:    &iban,
		Date:           testDate(t, 2026, time.March, 1),
		Amount:         money.Money{Minor: -2_000, Currency: money.EUR},
		BalanceAfter:   &money.Money{Minor: 9_000, Currency: money.EUR},
		Currency:       money.EUR,
		PartnerName:    &partner,
		Purpose:        &purpose,
		CategoryID:     &categoryID,
		CategoryName:   &categoryName,
		CategoryColor:  &categoryColor,
		IsTaxRelevant:  true,
		DocumentCount:  1,
		TaxSource:      model.TaxSourceRule,
		TaxRulePattern: &taxRule,
		TaxOverride:    &taxOverride,
	}

	state := New(i18n.En, today)
	state.SelectedYear = 2025
	state.CustomRange = &DateRange{
		From: testDate(t, 2025, time.December, 1),
		To:   testDate(t, 2026, time.January, 31),
	}
	state.TaxReport = &report.TaxReport{
		Year:                    2026,
		From:                    testDate(t, 2026, time.January, 1),
		To:                      testDate(t, 2026, time.December, 31),
		TotalIncome:             money.Money{Minor: 10_000, Currency: money.EUR},
		TotalExpenses:           money.Money{Minor: 5_000, Currency: money.EUR},
		NetIncome:               money.Money{Minor: 5_000, Currency: money.EUR},
		ExpensesByCategory:      []report.NamedAmount{{Name: "Health", Amount: money.Money{Minor: 5_000, Currency: money.EUR}}},
		TaxRelevantExpenses:     []report.NamedAmount{{Name: "Health", Amount: money.Money{Minor: 5_000, Currency: money.EUR}}},
		TaxRelevantTransactions: []model.TransactionItem{transaction},
		MonthlyBreakdown: []report.MonthlyData{{
			Year:     2026,
			Month:    1,
			Income:   money.Money{Minor: 10_000, Currency: money.EUR},
			Expenses: money.Money{Minor: 5_000, Currency: money.EUR},
		}},
		TopVendors: []report.NamedAmount{{Name: "Apotheke", Amount: money.Money{Minor: 5_000, Currency: money.EUR}}},
		Currency:   money.EUR,
		CategoryShares: []report.CategoryShare{{
			CategoryID: &categoryID,
			Name:       "Health",
			Amount:     money.Money{Minor: 5_000, Currency: money.EUR},
			Color:      categoryColor,
		}},
		TransferTotal: money.Money{Minor: 0, Currency: money.EUR},
	}
	state.TaxBookings = &report.TaxBookings{
		From:     testDate(t, 2026, time.January, 1),
		To:       testDate(t, 2026, time.December, 31),
		Currency: money.EUR,
		DeductionGroups: []report.TaxGroup{{
			CategoryID: &categoryID,
			Name:       "Health",
			Color:      categoryColor,
			Bookings:   []model.TransactionItem{transaction},
			Income:     money.Money{Minor: 0, Currency: money.EUR},
			Expenses:   money.Money{Minor: 5_000, Currency: money.EUR},
		}},
		BookingCount: 1,
	}
	state.LoadedRange = &DateRange{
		From: testDate(t, 2026, time.January, 1),
		To:   testDate(t, 2026, time.December, 31),
	}
	state.Suggestions = []report.TaxSuggestion{{Booking: transaction, Keyword: "Apotheke"}}
	state.Recurring = []recurring.RecurringPayment{{
		Name:          "Netflix",
		Cadence:       recurring.Monthly,
		TypicalAmount: money.Money{Minor: -999, Currency: money.EUR},
		FixedAmount:   true,
		LastDate:      testDate(t, 2026, time.September, 1),
		NextDate:      testDate(t, 2026, time.October, 1),
		Occurrences:   3,
		CategoryName:  &categoryName,
		Currency:      money.EUR,
		IsTransfer:    false,
	}}
	state.Error = &errorText
	state.Message = &messageText

	clone := state.Clone()
	if clone == state {
		t.Fatal("the clone reused the state pointer")
	}

	clone.CustomRange.From = testDate(t, 2000, time.January, 1)
	clone.LoadedRange.To = testDate(t, 2000, time.December, 31)
	clone.TaxReport.TotalIncome.Minor = 1
	clone.TaxReport.ExpensesByCategory[0].Name = "changed"
	clone.TaxReport.TaxRelevantExpenses[0].Amount.Minor = 1
	clone.TaxReport.TaxRelevantTransactions[0].PartnerName = stringPointer("changed")
	clone.TaxReport.TaxRelevantTransactions[0].CategoryID = nil
	clone.TaxReport.MonthlyBreakdown[0].Income.Minor = 1
	clone.TaxReport.TopVendors[0].Name = "changed"
	clone.TaxReport.CategoryShares[0].CategoryID = nil
	clone.TaxBookings.DeductionGroups[0].CategoryID = nil
	clone.TaxBookings.DeductionGroups[0].Bookings[0].Purpose = stringPointer("changed")
	clone.Suggestions[0].Booking.PartnerName = stringPointer("changed")
	clone.Recurring[0].CategoryName = stringPointer("changed")
	*clone.Error = "changed"
	*clone.Message = "changed"

	if state.CustomRange.From != testDate(t, 2025, time.December, 1) ||
		state.LoadedRange.To != testDate(t, 2026, time.December, 31) {
		t.Fatal("the clone shares a range")
	}
	if state.TaxReport.TotalIncome.Minor != 10_000 ||
		state.TaxReport.ExpensesByCategory[0].Name != "Health" ||
		state.TaxReport.TaxRelevantExpenses[0].Amount.Minor != 5_000 ||
		*state.TaxReport.TaxRelevantTransactions[0].PartnerName != "Apotheke" ||
		state.TaxReport.TaxRelevantTransactions[0].CategoryID == nil ||
		state.TaxReport.MonthlyBreakdown[0].Income.Minor != 10_000 ||
		state.TaxReport.TopVendors[0].Name != "Apotheke" ||
		state.TaxReport.CategoryShares[0].CategoryID == nil {
		t.Fatal("the clone shares the tax report")
	}
	if state.TaxBookings.DeductionGroups[0].CategoryID == nil ||
		*state.TaxBookings.DeductionGroups[0].Bookings[0].Purpose != "Rechnung" {
		t.Fatal("the clone shares the tax bookings")
	}
	if *state.Suggestions[0].Booking.PartnerName != "Apotheke" {
		t.Fatal("the clone shares a suggestion")
	}
	if *state.Recurring[0].CategoryName != "Health" {
		t.Fatal("the clone shares a recurring payment")
	}
	if *state.Error != "error" || *state.Message != "message" {
		t.Fatal("the clone shares a message")
	}
	if clone.SelectedYear != state.SelectedYear || clone.Language != state.Language {
		t.Fatal("the clone did not keep the plain values")
	}
}
