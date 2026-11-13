package accountsview

import (
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// Clone returns a deep copy of the state.
//
// Rust's AccountsState is Clone: the shell clones it before a background
// action and publishes the clone afterwards. The Busy handle is shared by
// identity, as Rust's Arc<Busy> is, so the copy keeps the profile's one counter.
func (s *AccountsState) Clone() AccountsState {
	if s == nil {
		return AccountsState{}
	}

	cloned := *s
	cloned.Accounts = cloneAccounts(s.Accounts)
	cloned.BankAccesses = cloneBankAccesses(s.BankAccesses)
	cloned.Detail = cloneDetail(s.Detail)
	cloned.CollapsedBanks = cloneBankIDs(s.CollapsedBanks)
	cloned.Selection = cloneSelection(s.Selection)
	cloned.Groups = cloneGroups(s.Groups)
	cloned.GrandTotal = append([]money.CurrencyTotal(nil), s.GrandTotal...)
	cloned.Collapsed = cloneStrings(s.Collapsed)
	cloned.CustomGroups = append([]model.AccountGroup(nil), s.CustomGroups...)
	return cloned
}

func cloneAccounts(accounts []AccountItem) []AccountItem {
	cloned := make([]AccountItem, len(accounts))
	for index, account := range accounts {
		cloned[index] = account
		cloned[index].BankAccessID = cloneBankConnectionID(account.BankAccessID)
		cloned[index].AccountNumber = cloneString(account.AccountNumber)
		cloned[index].LastUpdate = cloneTime(account.LastUpdate)
		cloned[index].Name = cloneString(account.Name)
		cloned[index].GroupID = cloneAccountGroupID(account.GroupID)
	}
	return cloned
}

func cloneBankAccesses(banks []model.BankAccessItem) []model.BankAccessItem {
	cloned := make([]model.BankAccessItem, len(banks))
	for index, bank := range banks {
		cloned[index] = bank
		cloned[index].BIC = cloneString(bank.BIC)
		cloned[index].TanMethod = cloneString(bank.TanMethod)
		cloned[index].ServerURL = cloneString(bank.ServerURL)
		cloned[index].LastSync = cloneTime(bank.LastSync)
		cloned[index].Capabilities = cloneString(bank.Capabilities)
		cloned[index].SyncFrom = cloneCivilDate(bank.SyncFrom)
	}
	return cloned
}

func cloneDetail(detail *AccountDetail) *AccountDetail {
	if detail == nil {
		return nil
	}
	cloned := *detail
	cloned.Account = cloneAccounts([]AccountItem{detail.Account})[0]
	cloned.History = append([]balance.BalancePoint(nil), detail.History...)
	cloned.Recent = cloneTransactions(detail.Recent)
	return &cloned
}

func cloneTransactions(transactions []model.TransactionItem) []model.TransactionItem {
	cloned := make([]model.TransactionItem, len(transactions))
	for index, transaction := range transactions {
		cloned[index] = transaction
		cloned[index].AccountIBAN = cloneString(transaction.AccountIBAN)
		cloned[index].BalanceAfter = cloneMoney(transaction.BalanceAfter)
		cloned[index].PartnerName = cloneString(transaction.PartnerName)
		cloned[index].Purpose = cloneString(transaction.Purpose)
		cloned[index].CategoryID = cloneCategoryID(transaction.CategoryID)
		cloned[index].CategoryName = cloneString(transaction.CategoryName)
		cloned[index].CategoryColor = cloneString(transaction.CategoryColor)
		cloned[index].TaxRulePattern = cloneString(transaction.TaxRulePattern)
		cloned[index].TaxOverride = cloneBool(transaction.TaxOverride)
	}
	return cloned
}

func cloneGroups(groups []accountgrouping.AccountGroupView) []accountgrouping.AccountGroupView {
	cloned := make([]accountgrouping.AccountGroupView, len(groups))
	for index, group := range groups {
		cloned[index] = group
		cloned[index].Name = cloneString(group.Name)
		cloned[index].Accounts = make([]accountgrouping.AccountItem, len(group.Accounts))
		for accountIndex, account := range group.Accounts {
			cloned[index].Accounts[accountIndex] = account
			cloned[index].Accounts[accountIndex].Name = cloneString(account.Name)
			cloned[index].Accounts[accountIndex].GroupID = cloneAccountGroupID(account.GroupID)
		}
		cloned[index].Totals = append([]money.CurrencyTotal(nil), group.Totals...)
	}
	return cloned
}

func cloneSelection(selection accountpanel.AccountSelection) accountpanel.AccountSelection {
	return selection
}

func cloneBankIDs(values map[model.BankConnectionID]struct{}) map[model.BankConnectionID]struct{} {
	if values == nil {
		return nil
	}
	cloned := make(map[model.BankConnectionID]struct{}, len(values))
	for key := range values {
		cloned[key] = struct{}{}
	}
	return cloned
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

func cloneString(value *string) *string {
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

func cloneBankConnectionID(value *model.BankConnectionID) *model.BankConnectionID {
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

func cloneCategoryID(value *model.CategoryID) *model.CategoryID {
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
