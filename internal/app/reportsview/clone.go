package reportsview

import (
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// Clone returns a deep copy of the state.
//
// Rust's ReportsState is cloned into every Slint snapshot and moved into a
// worker. Go shares it unless it is copied, so the shell clones it before a
// background load or action exactly as it clones the Dashboard and Transactions
// states. The language, range and every pointer are copied too.
func (s *ReportsState) Clone() *ReportsState {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.CustomRange = cloneDateRange(s.CustomRange)
	cloned.LoadedRange = cloneDateRange(s.LoadedRange)
	if s.TaxReport != nil {
		taxReport := cloneTaxReport(*s.TaxReport)
		cloned.TaxReport = &taxReport
	}
	if s.TaxBookings != nil {
		bookings := cloneTaxBookings(*s.TaxBookings)
		cloned.TaxBookings = &bookings
	}
	cloned.Suggestions = cloneSuggestions(s.Suggestions)
	cloned.Recurring = cloneRecurring(s.Recurring)
	cloned.Error = cloneString(s.Error)
	cloned.Message = cloneString(s.Message)
	return &cloned
}

func cloneDateRange(value *DateRange) *DateRange {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTaxReport(reportValue report.TaxReport) report.TaxReport {
	cloned := reportValue
	cloned.ExpensesByCategory = cloneNamedAmounts(reportValue.ExpensesByCategory)
	cloned.TaxRelevantExpenses = cloneNamedAmounts(reportValue.TaxRelevantExpenses)
	cloned.TaxRelevantTransactions = cloneTransactions(reportValue.TaxRelevantTransactions)
	cloned.MonthlyBreakdown = cloneMonthly(reportValue.MonthlyBreakdown)
	cloned.TopVendors = cloneNamedAmounts(reportValue.TopVendors)
	cloned.CategoryShares = cloneCategoryShares(reportValue.CategoryShares)
	return cloned
}

func cloneTaxBookings(bookings report.TaxBookings) report.TaxBookings {
	cloned := bookings
	cloned.IncomeGroups = cloneTaxGroups(bookings.IncomeGroups)
	cloned.DeductionGroups = cloneTaxGroups(bookings.DeductionGroups)
	return cloned
}

func cloneTaxGroups(groups []report.TaxGroup) []report.TaxGroup {
	if groups == nil {
		return nil
	}
	cloned := make([]report.TaxGroup, len(groups))
	for index, group := range groups {
		cloned[index] = group
		cloned[index].CategoryID = cloneCategoryID(group.CategoryID)
		cloned[index].Bookings = cloneTransactions(group.Bookings)
	}
	return cloned
}

func cloneNamedAmounts(values []report.NamedAmount) []report.NamedAmount {
	if values == nil {
		return nil
	}
	cloned := make([]report.NamedAmount, len(values))
	copy(cloned, values)
	return cloned
}

func cloneMonthly(values []report.MonthlyData) []report.MonthlyData {
	if values == nil {
		return nil
	}
	cloned := make([]report.MonthlyData, len(values))
	copy(cloned, values)
	return cloned
}

func cloneCategoryShares(values []report.CategoryShare) []report.CategoryShare {
	if values == nil {
		return nil
	}
	cloned := make([]report.CategoryShare, len(values))
	for index, share := range values {
		cloned[index] = share
		cloned[index].CategoryID = cloneCategoryID(share.CategoryID)
	}
	return cloned
}

func cloneSuggestions(values []report.TaxSuggestion) []report.TaxSuggestion {
	if values == nil {
		return nil
	}
	cloned := make([]report.TaxSuggestion, len(values))
	for index, suggestion := range values {
		cloned[index] = suggestion
		cloned[index].Booking = *cloneTransaction(&suggestion.Booking)
	}
	return cloned
}

func cloneRecurring(values []recurring.RecurringPayment) []recurring.RecurringPayment {
	if values == nil {
		return nil
	}
	cloned := make([]recurring.RecurringPayment, len(values))
	for index, payment := range values {
		cloned[index] = payment
		cloned[index].CategoryName = cloneString(payment.CategoryName)
	}
	return cloned
}

func cloneTransactions(items []model.TransactionItem) []model.TransactionItem {
	if items == nil {
		return nil
	}
	cloned := make([]model.TransactionItem, len(items))
	for index := range items {
		cloned[index] = *cloneTransaction(&items[index])
	}
	return cloned
}

func cloneTransaction(item *model.TransactionItem) *model.TransactionItem {
	if item == nil {
		return nil
	}
	cloned := *item
	cloned.AccountIBAN = cloneString(item.AccountIBAN)
	cloned.BalanceAfter = cloneMoney(item.BalanceAfter)
	cloned.PartnerName = cloneString(item.PartnerName)
	cloned.Purpose = cloneString(item.Purpose)
	cloned.CategoryID = cloneCategoryID(item.CategoryID)
	cloned.CategoryName = cloneString(item.CategoryName)
	cloned.CategoryColor = cloneString(item.CategoryColor)
	cloned.TaxRulePattern = cloneString(item.TaxRulePattern)
	cloned.TaxOverride = cloneBool(item.TaxOverride)
	return &cloned
}

func cloneMoney(value *money.Money) *money.Money {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneCategoryID(value *model.CategoryID) *model.CategoryID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
