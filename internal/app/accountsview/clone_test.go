package accountsview

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func TestCloneIsDeepAndSharesTheBusyHandle(t *testing.T) {
	name := "Household"
	iban := "DE89370400440532013000"
	number := "1234"
	bankID := model.BankConnectionID(7)
	groupID := model.AccountGroupID(4)
	categoryID := model.CategoryID(3)
	bic := "COBADEFFXXX"
	lastSync := time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	syncFrom, ok := civil.New(2026, time.January, 1)
	if !ok {
		t.Fatal("civil.New failed")
	}
	amount := money.Money{Minor: 100, Currency: money.EUR}

	busy := NewBusy()
	state := New(i18n.En)
	state.Busy = busy
	state.Accounts = []AccountItem{{
		ID:            1,
		BankAccessID:  &bankID,
		BankName:      "Test Bank",
		IBAN:          iban,
		AccountNumber: &number,
		Balance:       amount,
		Currency:      money.EUR,
		AccountType:   "checking",
		LastUpdate:    &updated,
		Name:          &name,
		GroupID:       &groupID,
		NewCount:      2,
	}}
	state.BankAccesses = []model.BankAccessItem{{
		ID:               bankID,
		BankName:         "Test Bank",
		BLZ:              "37040044",
		BIC:              &bic,
		LastSync:         &lastSync,
		SyncFrom:         &syncFrom,
		IncludeInSyncAll: true,
	}}
	state.Detail = &AccountDetail{
		Account: state.Accounts[0],
		Days:    30,
		History: []balance.BalancePoint{{Date: syncFrom, Balance: amount}},
		Recent: []model.TransactionItem{{
			ID:          9,
			AccountIBAN: &iban,
			Date:        syncFrom,
			Amount:      amount,
			Currency:    money.EUR,
			PartnerName: &name,
			CategoryID:  &categoryID,
		}},
		IncomeInPeriod:   amount,
		ExpensesInPeriod: amount,
	}
	state.CollapsedBanks = map[model.BankConnectionID]struct{}{bankID: {}}
	state.Collapsed = map[string]struct{}{"type:checking": {}}
	state.Selection = accountpanel.AccountSelectionFor(1)
	state.Groups = []accountgrouping.AccountGroupView{{
		Key:      accountgrouping.TypeKey("checking"),
		Name:     &name,
		Accounts: []accountgrouping.AccountItem{{ID: 1, Name: &name, GroupID: &groupID}},
		Totals:   []money.CurrencyTotal{{Currency: money.EUR, Total: 100}},
	}}
	state.GrandTotal = []money.CurrencyTotal{{Currency: money.EUR, Total: 100}}
	state.CustomGroups = []model.AccountGroup{{ID: groupID, Name: "Reisen", SortOrder: 1}}

	cloned := state.Clone()
	if cloned.Busy != state.Busy {
		t.Fatal("Clone did not share the Busy handle")
	}
	if cloned.Accounts[0].BankAccessID == state.Accounts[0].BankAccessID {
		t.Fatal("Clone shares the account's bank id pointer")
	}
	*cloned.Accounts[0].Name = "Changed"
	if *state.Accounts[0].Name != "Household" {
		t.Fatal("mutating the clone changed the original account name")
	}
	cloned.CollapsedBanks[bankID] = struct{}{}
	delete(cloned.CollapsedBanks, bankID)
	if _, ok := state.CollapsedBanks[bankID]; !ok {
		t.Fatal("mutating the clone changed the original collapsed banks")
	}
	cloned.Collapsed["type:checking"] = struct{}{}
	delete(cloned.Collapsed, "type:checking")
	if _, ok := state.Collapsed["type:checking"]; !ok {
		t.Fatal("mutating the clone changed the original collapsed groups")
	}
	cloned.Detail.History[0].Balance.Minor = 999
	if state.Detail.History[0].Balance.Minor != 100 {
		t.Fatal("mutating the clone changed the original history")
	}
	*cloned.Detail.Recent[0].PartnerName = "Other"
	if *state.Detail.Recent[0].PartnerName != "Household" {
		t.Fatal("mutating the clone changed the original recent booking")
	}
	*cloned.Groups[0].Name = "Other"
	if *state.Groups[0].Name != "Household" {
		t.Fatal("mutating the clone changed the original group name")
	}
	cloned.GrandTotal[0].Total = 999
	if state.GrandTotal[0].Total != 100 {
		t.Fatal("mutating the clone changed the original grand total")
	}
	cloned.CustomGroups[0].Name = "Other"
	if state.CustomGroups[0].Name != "Reisen" {
		t.Fatal("mutating the clone changed the original custom group")
	}
}

func TestCloningNilStateIsEmpty(t *testing.T) {
	var state *AccountsState
	cloned := state.Clone()
	if cloned.Accounts != nil || cloned.BankAccesses != nil || cloned.Detail != nil {
		t.Fatalf("nil clone = %+v", cloned)
	}
}
