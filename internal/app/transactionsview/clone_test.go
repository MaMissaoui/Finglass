package transactionsview

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// TestCloneIsDeep mutates every slice, map and pointer reachable from the view
// and checks that the original did not change.
func TestCloneIsDeep(t *testing.T) {
	accountID := model.AccountID(1)
	categoryID := model.CategoryID(7)
	transactionID := model.TransactionID(11)
	groupID := model.AccountGroupID(3)
	month := dashboard.NewYearMonth(2026, 11)
	anchor := transactionID
	name := "Giro"
	accountNumber := "1234"
	partner := "Partner"
	purpose := "Purpose"
	categoryName := "Food"
	categoryColour := "#ff0000"
	taxRule := "market"
	taxOverride := true
	iban := "DE89370400440532013000"
	documentLabel := "Booking"
	confidence := 0.9
	balanceDifference := money.Money{Minor: -20, Currency: money.EUR}
	minimum := money.Money{Minor: 100, Currency: money.EUR}
	maximum := money.Money{Minor: 200, Currency: money.EUR}

	state := NewState(i18n.En)
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("USD currency: %v", err)
	}
	state.Transactions = []model.TransactionItem{{
		ID:             transactionID,
		AccountID:      accountID,
		AccountIBAN:    &iban,
		Date:           date(t, "2026-11-05"),
		Amount:         eur(-100),
		BalanceAfter:   moneyPointer(eur(900)),
		Currency:       money.EUR,
		PartnerName:    &partner,
		Purpose:        &purpose,
		CategoryID:     &categoryID,
		CategoryName:   &categoryName,
		CategoryColor:  &categoryColour,
		IsTaxRelevant:  true,
		DocumentCount:  1,
		IsNew:          true,
		IsTransfer:     false,
		TaxSource:      model.TaxSourceRule,
		TaxRulePattern: &taxRule,
		TaxOverride:    &taxOverride,
	}}
	state.Categories = []model.Category{{
		ID:          categoryID,
		Name:        categoryName,
		Color:       categoryColour,
		BudgetLimit: moneyPointer(eur(500)),
	}}
	state.Accounts = []accountsview.AccountItem{{
		ID:            accountID,
		BankAccessID:  bankConnectionPointer(2),
		BankName:      "Bank",
		IBAN:          iban,
		AccountNumber: &accountNumber,
		Balance:       eur(900),
		Currency:      money.EUR,
		AccountType:   "checking",
		Name:          &name,
		GroupID:       &groupID,
		NewCount:      1,
	}}
	state.Month = &month
	state.CustomRange = &DateRange{From: date(t, "2026-11-01"), To: date(t, "2026-11-30")}
	state.Selection = accountpanel.AccountSelectionFor(accountID)
	state.Groups = []accountgrouping.AccountGroupView{{
		Key:      accountgrouping.CustomKey(groupID),
		Name:     &name,
		Accounts: []accountgrouping.AccountItem{{ID: accountID, Name: &name, GroupID: &groupID}},
		Totals:   []money.CurrencyTotal{{Currency: money.EUR, Total: 900}},
		NewCount: 1,
	}}
	state.GrandTotal = []money.CurrencyTotal{{Currency: money.EUR, Total: 900}}
	state.Collapsed = map[string]struct{}{"custom:3": {}}
	state.BoldIDs = map[model.TransactionID]struct{}{transactionID: {}}
	state.CustomGroups = []model.AccountGroup{{ID: groupID, Name: name}}
	state.Balances = map[model.TransactionID]balance.Entry{
		transactionID: {Balance: eur(900), BankDifference: &balanceDifference},
	}
	state.BalanceChart = []balance.BalancePoint{{Date: date(t, "2026-11-05"), Balance: eur(900)}}
	state.CurrentBalance = moneyPointer(eur(900))
	state.BalanceCurrency = currencyPointer(money.EUR)
	state.TotalIncome = []money.CurrencyTotal{{Currency: money.EUR, Total: 900}}
	state.TotalExpenses = []money.CurrencyTotal{{Currency: money.EUR, Total: 100}}
	state.ImportPreview = &ImportPreview{
		Files: []string{"/tmp/a.csv"},
		Entries: []ImportEntry{{
			File: "/tmp/a.csv",
			Statement: imports.ParsedStatement{
				AccountIBAN:  &iban,
				Accounts:     []string{iban},
				Skipped:      []string{"line 1"},
				FormatDetail: stringPointer("CAMT.052"),
				Csv: &imports.CsvLayout{
					Headers: []string{"Date"},
					Mapping: imports.CsvMapping{Date: intPointer(0)},
				},
			},
		}},
		Failures:  []ImportFailure{{FileName: "bad.csv", Message: "unreadable"}},
		AccountID: &accountID,
	}
	state.MinAmount = &minimum
	state.MaxAmount = &maximum
	state.Selected = &state.Transactions[0]
	state.SelectedDocuments = []model.DocumentItem{{
		ID:               1,
		FileName:         "receipt.pdf",
		TransactionID:    &transactionID,
		TransactionLabel: &documentLabel,
		OCRConfidence:    &confidence,
	}}
	state.PendingDuplicate = &ManualEntry{
		AccountID:   accountID,
		Date:        date(t, "2026-11-05"),
		Amount:      eur(100),
		Currency:    money.EUR,
		PartnerName: &partner,
		Purpose:     &purpose,
		CategoryID:  &categoryID,
	}
	state.PendingBookings = []model.PendingBookingItem{{
		AccountID:   accountID,
		Date:        date(t, "2026-11-06"),
		ValueDate:   datePointer(date(t, "2026-11-07")),
		Amount:      eur(-50),
		PartnerName: &partner,
		Purpose:     &purpose,
		FetchedAt:   testNow(),
	}}
	state.ReapplyPreview = &ReapplyPreview{
		Changes: []ReapplyChange{{
			TransactionID:  transactionID,
			FromCategoryID: &categoryID,
			ToCategoryID:   model.CategoryID(8),
		}},
		Groups: []ReapplyGroup{{
			FromCategoryID: &categoryID,
			FromName:       "Food",
			ToCategoryID:   model.CategoryID(8),
			ToName:         "Home",
			Count:          1,
			Sum:            moneyPointer(eur(100)),
			Currency:       money.EUR,
		}},
		ShownCount: 1,
	}
	state.SelectedIDs = map[model.TransactionID]struct{}{transactionID: {}}
	state.SelectionAnchorID = &anchor
	state.Error = stringPointer("error")
	state.Message = stringPointer("message")

	view := &TransactionsView{state: *state, busy: accountsview.NewBusy()}
	view.lastRefreshedSelection = selectionPointer(accountpanel.AllSelection())
	view.reapplyUndo = []ReapplyChange{{
		TransactionID:  transactionID,
		FromCategoryID: &categoryID,
		ToCategoryID:   model.CategoryID(8),
	}}

	clone := view.Clone()
	cloned := clone.state

	cloned.Transactions[0].PartnerName = stringPointer("changed")
	cloned.Transactions[0].CategoryID = nil
	cloned.Categories[0].BudgetLimit.Minor = 1
	cloned.Accounts[0].Name = stringPointer("changed")
	cloned.Accounts[0].GroupID = nil
	cloned.Month.Year = 2000
	cloned.CustomRange.From = date(t, "2000-01-01")
	cloned.Groups[0].Name = stringPointer("changed")
	cloned.Groups[0].Accounts[0].Name = stringPointer("changed")
	cloned.Groups[0].Totals[0].Total = 1
	cloned.Collapsed["custom:3"] = struct{}{}
	delete(cloned.Collapsed, "custom:3")
	cloned.BoldIDs[transactionID] = struct{}{}
	delete(cloned.BoldIDs, transactionID)
	cloned.CustomGroups[0].Name = "changed"
	entry := cloned.Balances[transactionID]
	entry.BankDifference.Minor = 1
	cloned.Balances[transactionID] = entry
	cloned.BalanceChart[0].Balance.Minor = 1
	cloned.CurrentBalance.Minor = 1
	*cloned.BalanceCurrency = usd
	cloned.TotalIncome[0].Total = 1
	cloned.ImportPreview.Files[0] = "changed"
	cloned.ImportPreview.Entries[0].Statement.Accounts[0] = "changed"
	cloned.ImportPreview.Entries[0].Statement.Skipped[0] = "changed"
	cloned.ImportPreview.Entries[0].Statement.Csv.Headers[0] = "changed"
	cloned.ImportPreview.Failures[0].Message = "changed"
	cloned.MinAmount.Minor = 1
	cloned.Selected.PartnerName = stringPointer("changed")
	cloned.SelectedDocuments[0].TransactionLabel = stringPointer("changed")
	*cloned.SelectedDocuments[0].OCRConfidence = 0
	cloned.PendingDuplicate.PartnerName = stringPointer("changed")
	cloned.PendingBookings[0].Purpose = stringPointer("changed")
	cloned.ReapplyPreview.Changes[0].FromCategoryID = nil
	cloned.ReapplyPreview.Groups[0].Sum.Minor = 1
	cloned.SelectedIDs[model.TransactionID(99)] = struct{}{}
	*cloned.SelectionAnchorID = model.TransactionID(99)
	*cloned.Error = "changed"
	*clone.lastRefreshedSelection = accountpanel.AccountSelectionFor(99)
	clone.reapplyUndo[0].FromCategoryID = nil

	if *state.Transactions[0].PartnerName != "Partner" {
		t.Fatal("the clone shares a transaction pointer")
	}
	if state.Transactions[0].CategoryID == nil || *state.Transactions[0].CategoryID != categoryID {
		t.Fatal("the clone shares a category id pointer")
	}
	if state.Categories[0].BudgetLimit.Minor != 500 {
		t.Fatal("the clone shares a category budget")
	}
	if *state.Accounts[0].Name != "Giro" || state.Accounts[0].GroupID == nil {
		t.Fatal("the clone shares an account pointer")
	}
	if state.Month.Year != 2026 {
		t.Fatal("the clone shares the month pointer")
	}
	if state.CustomRange.From != date(t, "2026-11-01") {
		t.Fatal("the clone shares the custom range")
	}
	if *state.Groups[0].Name != "Giro" || *state.Groups[0].Accounts[0].Name != "Giro" {
		t.Fatal("the clone shares a group pointer")
	}
	if state.Groups[0].Totals[0].Total != 900 {
		t.Fatal("the clone shares a group total slice")
	}
	if _, ok := state.Collapsed["custom:3"]; !ok {
		t.Fatal("the clone shares the collapsed map")
	}
	if _, ok := state.BoldIDs[transactionID]; !ok {
		t.Fatal("the clone shares the bold map")
	}
	if state.CustomGroups[0].Name != "Giro" {
		t.Fatal("the clone shares the custom groups slice")
	}
	if state.Balances[transactionID].BankDifference.Minor != -20 {
		t.Fatal("the clone shares a balance difference")
	}
	if state.BalanceChart[0].Balance.Minor != 900 {
		t.Fatal("the clone shares the balance chart slice")
	}
	if state.CurrentBalance.Minor != 900 || *state.BalanceCurrency != money.EUR {
		t.Fatal("the clone shares the current balance")
	}
	if state.ImportPreview.Files[0] != "/tmp/a.csv" ||
		state.ImportPreview.Entries[0].Statement.Accounts[0] != iban ||
		state.ImportPreview.Entries[0].Statement.Skipped[0] != "line 1" ||
		state.ImportPreview.Entries[0].Statement.Csv.Headers[0] != "Date" ||
		state.ImportPreview.Failures[0].Message != "unreadable" {
		t.Fatal("the clone shares the import preview")
	}
	if state.MinAmount.Minor != 100 {
		t.Fatal("the clone shares the amount bounds")
	}
	if *state.Selected.PartnerName != "Partner" {
		t.Fatal("the clone shares the selected transaction")
	}
	if *state.SelectedDocuments[0].TransactionLabel != "Booking" ||
		*state.SelectedDocuments[0].OCRConfidence != 0.9 {
		t.Fatal("the clone shares the selected documents")
	}
	if *state.PendingDuplicate.PartnerName != "Partner" {
		t.Fatal("the clone shares the pending duplicate")
	}
	if *state.PendingBookings[0].Purpose != "Purpose" {
		t.Fatal("the clone shares the pending bookings")
	}
	if state.ReapplyPreview.Changes[0].FromCategoryID == nil ||
		state.ReapplyPreview.Groups[0].Sum.Minor != 100 {
		t.Fatal("the clone shares the reapply preview")
	}
	if _, ok := state.SelectedIDs[model.TransactionID(99)]; ok {
		t.Fatal("the clone shares the selected ids map")
	}
	if *state.SelectionAnchorID != transactionID {
		t.Fatal("the clone shares the selection anchor")
	}
	if *state.Error != "error" {
		t.Fatal("the clone shares the error text")
	}
	if *view.lastRefreshedSelection != accountpanel.AllSelection() {
		t.Fatal("the clone shares the refreshed selection")
	}
	if view.reapplyUndo[0].FromCategoryID == nil {
		t.Fatal("the clone shares the undo record")
	}

	if &clone.state == &view.state {
		t.Fatal("the clone reused the state value")
	}
	if clone.Busy().Active() {
		t.Fatal("the clone started with a busy handle")
	}
	clone.Busy().Begin()
	if !view.Busy().Active() {
		t.Fatal("the clone did not share the busy handle")
	}
	clone.Busy().End()
}

func moneyPointer(value money.Money) *money.Money { return &value }

func currencyPointer(value money.Currency) *money.Currency { return &value }

func stringPointer(value string) *string { return &value }

func datePointer(value civil.Date) *civil.Date { return &value }

func bankConnectionPointer(value model.BankConnectionID) *model.BankConnectionID { return &value }

func selectionPointer(value accountpanel.AccountSelection) *accountpanel.AccountSelection {
	return &value
}

func testNow() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
