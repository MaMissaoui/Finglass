package transactionsview

import (
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// Clone returns a deep copy of the view.
//
// Rust's TransactionsView is moved into a worker and given back afterwards. Go
// shares it unless it is copied, so the shell clones it before a background
// action exactly as it clones AccountsState. The busy handle stays the same
// value: it is the profile's one counter and is never replaced by a clone.
func (v *TransactionsView) Clone() *TransactionsView {
	if v == nil {
		return nil
	}
	cloned := *v
	cloned.state = v.state.Clone()
	cloned.lastRefreshedSelection = cloneSelection(v.lastRefreshedSelection)
	cloned.reapplyUndo = cloneReapplyChanges(v.reapplyUndo)
	return &cloned
}

// Clone returns a deep copy of the state.
func (s *TransactionsState) Clone() TransactionsState {
	if s == nil {
		return TransactionsState{}
	}
	cloned := *s
	cloned.Transactions = cloneTransactions(s.Transactions)
	cloned.Categories = cloneCategories(s.Categories)
	cloned.Accounts = cloneAccountItems(s.Accounts)
	cloned.Month = cloneYearMonth(s.Month)
	cloned.CustomRange = cloneDateRange(s.CustomRange)
	cloned.Selection = cloneSelectionValue(s.Selection)
	cloned.Groups = cloneGroups(s.Groups)
	cloned.GrandTotal = append([]money.CurrencyTotal(nil), s.GrandTotal...)
	cloned.Collapsed = cloneStrings(s.Collapsed)
	cloned.BoldIDs = cloneTransactionIDs(s.BoldIDs)
	cloned.CustomGroups = append([]model.AccountGroup(nil), s.CustomGroups...)
	cloned.Balances = cloneBalances(s.Balances)
	cloned.BalanceChart = append([]balance.BalancePoint(nil), s.BalanceChart...)
	cloned.CurrentBalance = cloneMoney(s.CurrentBalance)
	if s.BalanceCurrency != nil {
		currency := *s.BalanceCurrency
		cloned.BalanceCurrency = &currency
	}
	cloned.TotalIncome = append([]money.CurrencyTotal(nil), s.TotalIncome...)
	cloned.TotalExpenses = append([]money.CurrencyTotal(nil), s.TotalExpenses...)
	cloned.ImportPreview = cloneImportPreview(s.ImportPreview)
	cloned.MinAmount = cloneMoney(s.MinAmount)
	cloned.MaxAmount = cloneMoney(s.MaxAmount)
	cloned.Selected = cloneTransaction(s.Selected)
	cloned.SelectedDocuments = cloneDocuments(s.SelectedDocuments)
	cloned.PendingDuplicate = cloneManualEntry(s.PendingDuplicate)
	cloned.PendingBookings = clonePendingBookings(s.PendingBookings)
	cloned.ReapplyPreview = cloneReapplyPreview(s.ReapplyPreview)
	cloned.SelectedIDs = cloneTransactionIDs(s.SelectedIDs)
	if s.SelectionAnchorID != nil {
		anchor := *s.SelectionAnchorID
		cloned.SelectionAnchorID = &anchor
	}
	cloned.Error = cloneString(s.Error)
	cloned.Message = cloneString(s.Message)
	return cloned
}

func cloneTransactions(items []model.TransactionItem) []model.TransactionItem {
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

func cloneCategories(categories []model.Category) []model.Category {
	cloned := make([]model.Category, len(categories))
	for index := range categories {
		cloned[index] = categories[index]
		cloned[index].BudgetLimit = cloneMoney(categories[index].BudgetLimit)
	}
	return cloned
}

func cloneAccountItems(items []accountsview.AccountItem) []accountsview.AccountItem {
	cloned := make([]accountsview.AccountItem, len(items))
	for index := range items {
		cloned[index] = items[index]
		cloned[index].BankAccessID = cloneBankConnectionID(items[index].BankAccessID)
		cloned[index].AccountNumber = cloneString(items[index].AccountNumber)
		cloned[index].LastUpdate = cloneTime(items[index].LastUpdate)
		cloned[index].Name = cloneString(items[index].Name)
		cloned[index].GroupID = cloneAccountGroupID(items[index].GroupID)
	}
	return cloned
}

func cloneBankConnectionID(value *model.BankConnectionID) *model.BankConnectionID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneDocuments(items []model.DocumentItem) []model.DocumentItem {
	cloned := make([]model.DocumentItem, len(items))
	for index := range items {
		cloned[index] = items[index]
		cloned[index].TransactionID = cloneTransactionID(items[index].TransactionID)
		cloned[index].TransactionLabel = cloneString(items[index].TransactionLabel)
		if items[index].OCRConfidence != nil {
			confidence := *items[index].OCRConfidence
			cloned[index].OCRConfidence = &confidence
		}
	}
	return cloned
}

func clonePendingBookings(items []model.PendingBookingItem) []model.PendingBookingItem {
	cloned := make([]model.PendingBookingItem, len(items))
	for index := range items {
		cloned[index] = items[index]
		cloned[index].ValueDate = cloneCivilDate(items[index].ValueDate)
		cloned[index].PartnerName = cloneString(items[index].PartnerName)
		cloned[index].Purpose = cloneString(items[index].Purpose)
	}
	return cloned
}

func cloneBalances(values map[model.TransactionID]balance.Entry) map[model.TransactionID]balance.Entry {
	if values == nil {
		return nil
	}
	cloned := make(map[model.TransactionID]balance.Entry, len(values))
	for id, entry := range values {
		entry.BankDifference = cloneMoney(entry.BankDifference)
		cloned[id] = entry
	}
	return cloned
}

func cloneGroups(groups []accountgrouping.AccountGroupView) []accountgrouping.AccountGroupView {
	cloned := make([]accountgrouping.AccountGroupView, len(groups))
	for index := range groups {
		cloned[index] = groups[index]
		cloned[index].Name = cloneString(groups[index].Name)
		cloned[index].Accounts = cloneGroupingAccounts(groups[index].Accounts)
		cloned[index].Totals = append([]money.CurrencyTotal(nil), groups[index].Totals...)
	}
	return cloned
}

func cloneGroupingAccounts(items []accountgrouping.AccountItem) []accountgrouping.AccountItem {
	cloned := make([]accountgrouping.AccountItem, len(items))
	for index := range items {
		cloned[index] = items[index]
		cloned[index].Name = cloneString(items[index].Name)
		cloned[index].GroupID = cloneAccountGroupID(items[index].GroupID)
	}
	return cloned
}

func cloneSelection(value *accountpanel.AccountSelection) *accountpanel.AccountSelection {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSelectionValue(value accountpanel.AccountSelection) accountpanel.AccountSelection {
	return value
}

func cloneManualEntry(entry *ManualEntry) *ManualEntry {
	if entry == nil {
		return nil
	}
	cloned := *entry
	cloned.PartnerName = cloneString(entry.PartnerName)
	cloned.Purpose = cloneString(entry.Purpose)
	cloned.CategoryID = cloneCategoryID(entry.CategoryID)
	return &cloned
}

func cloneReapplyChanges(changes []ReapplyChange) []ReapplyChange {
	cloned := make([]ReapplyChange, len(changes))
	for index := range changes {
		cloned[index] = changes[index]
		cloned[index].FromCategoryID = cloneCategoryID(changes[index].FromCategoryID)
	}
	return cloned
}

func cloneReapplyPreview(preview *ReapplyPreview) *ReapplyPreview {
	if preview == nil {
		return nil
	}
	cloned := *preview
	cloned.Changes = cloneReapplyChanges(preview.Changes)
	cloned.Groups = make([]ReapplyGroup, len(preview.Groups))
	for index := range preview.Groups {
		cloned.Groups[index] = preview.Groups[index]
		cloned.Groups[index].FromCategoryID = cloneCategoryID(preview.Groups[index].FromCategoryID)
		cloned.Groups[index].Sum = cloneMoney(preview.Groups[index].Sum)
	}
	return &cloned
}

func cloneImportPreview(preview *ImportPreview) *ImportPreview {
	if preview == nil {
		return nil
	}
	cloned := *preview
	cloned.Files = append([]string(nil), preview.Files...)
	cloned.Entries = make([]ImportEntry, len(preview.Entries))
	for index := range preview.Entries {
		cloned.Entries[index] = ImportEntry{
			File:      preview.Entries[index].File,
			Statement: cloneParsedStatement(preview.Entries[index].Statement),
		}
	}
	cloned.Failures = append([]ImportFailure(nil), preview.Failures...)
	cloned.AccountID = cloneAccountID(preview.AccountID)
	return &cloned
}

func cloneParsedStatement(statement imports.ParsedStatement) imports.ParsedStatement {
	cloned := statement
	cloned.Bookings = make([]imports.ImportedBooking, len(statement.Bookings))
	for index := range statement.Bookings {
		cloned.Bookings[index] = statement.Bookings[index]
		cloned.Bookings[index].ValueDate = cloneCivilDate(statement.Bookings[index].ValueDate)
		cloned.Bookings[index].Currency = cloneString(statement.Bookings[index].Currency)
		cloned.Bookings[index].PartnerName = cloneString(statement.Bookings[index].PartnerName)
		cloned.Bookings[index].PartnerIBAN = cloneString(statement.Bookings[index].PartnerIBAN)
		cloned.Bookings[index].Purpose = cloneString(statement.Bookings[index].Purpose)
		cloned.Bookings[index].BankReference = cloneString(statement.Bookings[index].BankReference)
	}
	cloned.AccountIBAN = cloneString(statement.AccountIBAN)
	cloned.Accounts = append([]string(nil), statement.Accounts...)
	cloned.Skipped = append([]string(nil), statement.Skipped...)
	cloned.Csv = cloneCsvLayout(statement.Csv)
	cloned.FormatDetail = cloneString(statement.FormatDetail)
	cloned.ClosingBalance = cloneClosingBalance(statement.ClosingBalance)
	return cloned
}

func cloneCsvLayout(layout *imports.CsvLayout) *imports.CsvLayout {
	if layout == nil {
		return nil
	}
	cloned := *layout
	cloned.Headers = append([]string(nil), layout.Headers...)
	cloned.Mapping = layout.Mapping
	return &cloned
}

func cloneClosingBalance(value *imports.ClosingBalance) *imports.ClosingBalance {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Currency = cloneString(value.Currency)
	return &cloned
}

func cloneDateRange(value *DateRange) *DateRange {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneYearMonth(value *dashboard.YearMonth) *dashboard.YearMonth {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStrings(values map[string]struct{}) map[string]struct{} {
	if values == nil {
		return nil
	}
	cloned := make(map[string]struct{}, len(values))
	for key := range values {
		cloned[key] = struct{}{}
	}
	return cloned
}

func cloneTransactionIDs(values map[model.TransactionID]struct{}) map[model.TransactionID]struct{} {
	if values == nil {
		return nil
	}
	cloned := make(map[model.TransactionID]struct{}, len(values))
	for key := range values {
		cloned[key] = struct{}{}
	}
	return cloned
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

func cloneMoney(value *money.Money) *money.Money {
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

func cloneAccountID(value *model.AccountID) *model.AccountID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneAccountGroupID(value *model.AccountGroupID) *model.AccountGroupID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTransactionID(value *model.TransactionID) *model.TransactionID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneCivilDate(value *civil.Date) *civil.Date {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
