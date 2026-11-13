package shell

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/accountpanel"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/accountgrouping"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	"github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type transactionsSeedData struct {
	checking model.AccountID
	savings  model.AccountID
	food     model.CategoryID
	salary   model.CategoryID
	group    model.AccountGroupID
}

func seedTransactionsDatabase(t *testing.T, appPaths paths.AppPaths) transactionsSeedData {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	checkingName := "Girokonto"
	checking, err := accounts.Create(ctx, database, model.NewAccount{
		Name:        &checkingName,
		IBAN:        stringPointer("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create checking: %v", err)
	}
	savings, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE44500105175407324931"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeSavings,
		Balance:     money.Money{Minor: 25_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create savings: %v", err)
	}
	group, err := accountgroups.Create(ctx, database, model.NewAccountGroup{Name: "Reisen", SortOrder: 1})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:          "Food",
		Color:         "#FF5722",
		IsTaxRelevant: true,
	})
	if err != nil {
		t.Fatalf("create food: %v", err)
	}
	salary, err := categories.Create(ctx, database, model.NewCategory{Name: "Salary", Color: "#2196F3"})
	if err != nil {
		t.Fatalf("create salary: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, category *model.CategoryID, purpose *string) {
		name := partner
		if _, err := transactions.InsertPlanned(ctx, database, checking, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &name,
			Purpose:     purpose,
			CategoryID:  category,
			ImportHash:  fmt.Sprintf("%s-%d-%s", day.String(), minor, partner),
			CreatedAt:   testNow.UTC(),
		}}); err != nil {
			t.Fatalf("insert transaction %s: %v", partner, err)
		}
	}
	insert(testToday().AddDays(-2), 2_000, "Employer", &salary, stringPointer("Salary October"))
	insert(testToday().AddDays(-1), -500, "Bakery", nil, nil)
	insert(testToday().AddDays(-1), -250, "Grocery", &food, stringPointer("Groceries"))
	insert(testToday().AddDays(-40), -700, "Old Shop", nil, nil)

	if err := pendingbookings.ReplaceForAccount(ctx, database, checking, []model.NewPendingBooking{{
		Date:        testToday(),
		Amount:      money.Money{Minor: -50, Currency: money.EUR},
		PartnerName: stringPointer("Pending partner"),
		Purpose:     stringPointer("Pending purpose"),
	}}, testNow.UTC()); err != nil {
		t.Fatalf("insert pending booking: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return transactionsSeedData{
		checking: checking,
		savings:  savings,
		food:     food,
		salary:   salary,
		group:    group,
	}
}

func waitForTransactions(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(transactionsview.TransactionsState) bool,
) transactionsview.TransactionsState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "transactions" {
				continue
			}
			state := session.TransactionsState()
			if predicate(state) {
				return state
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedTransactionsSession(t *testing.T) (*Session, transactionsSeedData) {
	t.Helper()
	appPaths := testPaths(t)
	seed := seedTransactionsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)
	session.Navigate("transactions")
	events, remove := listenForChanges(session)
	defer remove()
	waitForTransactions(t, session, events, "the loaded transactions", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && len(state.Accounts) == 2 && len(state.Transactions) > 0
	})
	return session, seed
}

func transactionsAuditActions(t *testing.T, session *Session) []string {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Action)
	}
	return names
}

func countTransactionsAudit(t *testing.T, session *Session, action string) int {
	t.Helper()
	count := 0
	for _, name := range transactionsAuditActions(t, session) {
		if name == action {
			count++
		}
	}
	return count
}

func categoryOptionIndex(t *testing.T, state transactionsview.TransactionsState, id model.CategoryID) int {
	t.Helper()
	for index, category := range state.Categories {
		if category.ID == id {
			return index + 1
		}
	}
	t.Fatalf("category %d is not in the list", id)
	return 0
}

func findTransaction(t *testing.T, state transactionsview.TransactionsState, partner string) model.TransactionItem {
	t.Helper()
	for _, transaction := range state.Transactions {
		if transaction.PartnerName != nil && *transaction.PartnerName == partner {
			return transaction
		}
	}
	t.Fatalf("transaction %q is not shown", partner)
	return model.TransactionItem{}
}

func TestTransactionsLoadsRowsPanelTotalsAndPending(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	state := session.TransactionsState()
	if len(state.Transactions) != 3 {
		t.Fatalf("this month's transactions = %d, want 3", len(state.Transactions))
	}
	if len(state.GrandTotal) != 1 || state.GrandTotal[0].Currency != money.EUR {
		t.Fatalf("grand total = %+v", state.GrandTotal)
	}
	if len(state.Groups) == 0 || len(state.Accounts) != 2 {
		t.Fatalf("panel = %d groups, %d accounts", len(state.Groups), len(state.Accounts))
	}
	if state.PrimaryCurrency != money.EUR {
		t.Fatalf("primary currency = %s", state.PrimaryCurrency)
	}
	if state.Selected != nil || state.PendingDuplicate != nil {
		t.Fatalf("a fresh load kept a dialog: %+v", state)
	}

	session.TransactionsPanelSelect("account", "", int(seed.checking), "")
	state = waitForTransactions(t, session, events, "the checking account", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Selection.Kind == accountpanel.SelectionAccount &&
			state.Selection.Account == seed.checking
	})
	if state.AccountID == nil || *state.AccountID != seed.checking {
		t.Fatalf("account id = %+v", state.AccountID)
	}
	if len(state.PendingBookings) != 1 || state.PendingBookings[0].PartnerName == nil {
		t.Fatalf("pending bookings = %+v", state.PendingBookings)
	}
	if state.Balances == nil {
		t.Fatal("a single account did not show a running balance")
	}
	if len(state.BalanceChart) == 0 {
		t.Fatal("a single account did not show a chart")
	}
}

func TestTransactionsFiltersRangeAmountAndChart(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.TransactionsSearch("Bakery")
	state := waitForTransactions(t, session, events, "the Bakery search", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.SearchQuery == "Bakery"
	})
	if len(state.Transactions) != 1 {
		t.Fatalf("Bakery rows = %d, want 1", len(state.Transactions))
	}
	if transactionsShowBackForTest(state) {
		t.Fatal("a plain search offered Back")
	}
	session.TransactionsBack()
	state = waitForTransactions(t, session, events, "the cleared search", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.SearchQuery == ""
	})
	if transactionsShowBackForTest(state) {
		t.Fatal("Back stayed visible after clearing the search")
	}

	session.TransactionsPeriodChanged(5)
	state = waitForTransactions(t, session, events, "all time", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Period == transactionsview.PeriodAll
	})
	if len(state.Transactions) != 4 {
		t.Fatalf("all-time rows = %d, want 4", len(state.Transactions))
	}

	session.TransactionsDirectionChanged(2)
	state = waitForTransactions(t, session, events, "expenses", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Direction == model.DirectionExpenses
	})
	if len(state.Transactions) != 3 {
		t.Fatalf("expense rows = %d, want 3", len(state.Transactions))
	}

	session.TransactionsOpenRange()
	dialog := session.TransactionsDialog()
	if !dialog.RangeOpen || dialog.RangeFrom == "" || dialog.RangeTo == "" {
		t.Fatalf("range dialog = %+v", dialog)
	}
	session.TransactionsApplyRange("not a date", "01.10.2026")
	dialog = session.TransactionsDialog()
	if dialog.RangeError != i18n.Text(i18n.En, i18n.EnterValidDate) {
		t.Fatalf("range error = %q", dialog.RangeError)
	}
	session.TransactionsApplyRange("15.10.2026", "01.10.2026")
	dialog = session.TransactionsDialog()
	if dialog.RangeError != i18n.Text(i18n.En, i18n.InvalidDateRange) {
		t.Fatalf("reversed range error = %q", dialog.RangeError)
	}
	session.TransactionsApplyRange("01.10.2026", "15.10.2026")
	state = waitForTransactions(t, session, events, "the custom range", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.CustomRange != nil && !session.TransactionsDialog().RangeOpen
	})
	if state.CustomRange.From != mustTransactionsDate(t, 2026, time.October, 1) {
		t.Fatalf("custom range = %+v", state.CustomRange)
	}

	session.TransactionsOpenAmount()
	dialog = session.TransactionsDialog()
	if !dialog.AmountOpen || dialog.AmountMinLabel == "" || dialog.AmountMaxLabel == "" {
		t.Fatalf("amount dialog = %+v", dialog)
	}
	session.TransactionsApplyAmount("not a number", "")
	dialog = session.TransactionsDialog()
	if dialog.AmountError != i18n.Text(i18n.En, i18n.EnterValidAmount) {
		t.Fatalf("amount error = %q", dialog.AmountError)
	}
	session.TransactionsApplyAmount("2.50", "5.00")
	state = waitForTransactions(t, session, events, "the amount bounds", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.MinAmount != nil && state.MaxAmount != nil
	})
	if state.MinAmount.Minor != 250 || state.MaxAmount.Minor != 500 {
		t.Fatalf("amount bounds = %+v, %+v", state.MinAmount, state.MaxAmount)
	}
	session.TransactionsResetAmount()
	state = waitForTransactions(t, session, events, "the cleared amounts", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.MinAmount == nil && state.MaxAmount == nil
	})

	session.TransactionsToggleChart()
	state = waitForTransactions(t, session, events, "the collapsed chart", func(state transactionsview.TransactionsState) bool {
		return state.ChartCollapsed
	})
	if !state.ChartCollapsed {
		t.Fatal("the chart did not collapse")
	}
}

func TestTransactionsPeriodCategoryAndAccountChoices(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.TransactionsCategoryChanged(2)
	state := waitForTransactions(t, session, events, "the first category", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.CategoryFilter.Kind == transactionsview.CategoryFilterKindOnly
	})
	if state.CategoryFilter.CategoryID != state.Categories[0].ID {
		t.Fatalf("category filter = %+v", state.CategoryFilter)
	}
	if len(state.Transactions) != 1 {
		t.Fatalf("category rows = %d, want 1", len(state.Transactions))
	}

	session.TransactionsCategoryChanged(1)
	state = waitForTransactions(t, session, events, "uncategorized", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.CategoryFilter.Kind == transactionsview.CategoryFilterKindNone
	})
	if len(state.Transactions) != 1 {
		t.Fatalf("uncategorized rows = %d, want 1", len(state.Transactions))
	}

	session.TransactionsCategoryChanged(0)
	state = waitForTransactions(t, session, events, "every category", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.CategoryFilter.Kind == transactionsview.CategoryFilterKindAny
	})

	// The account choice list is all accounts, then the groups, then accounts.
	accountIndex := -1
	for index, account := range state.Accounts {
		if account.ID == seed.checking {
			accountIndex = 1 + len(state.Groups) + index
		}
	}
	if accountIndex < 0 {
		t.Fatal("the checking account is not in the choices")
	}
	session.TransactionsAccountChanged(accountIndex)
	state = waitForTransactions(t, session, events, "the checking account choice", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Selection.Kind == accountpanel.SelectionAccount
	})
	if state.Selection.Account != seed.checking {
		t.Fatalf("selection = %+v", state.Selection)
	}
}

func TestTransactionsPanelAndGroups(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	state := session.TransactionsState()
	groupIndex := 0
	for index, group := range state.Groups {
		if group.Key.Kind != accountgrouping.GroupKeyCustom {
			groupIndex = index
			break
		}
	}
	accountType := state.Groups[groupIndex].Key.AccountType
	groupKey := state.Groups[groupIndex].Key.ID()
	session.TransactionsPanelToggle("group", "type", -1, accountType)
	state = waitForTransactions(t, session, events, "the collapsed group", func(state transactionsview.TransactionsState) bool {
		_, ok := state.Collapsed[groupKey]
		return ok
	})
	if _, ok := state.Collapsed[groupKey]; !ok {
		t.Fatal("the group did not collapse")
	}
	session.TransactionsPanelToggle("group", "type", -1, accountType)
	waitForTransactions(t, session, events, "the expanded group", func(state transactionsview.TransactionsState) bool {
		_, ok := state.Collapsed[groupKey]
		return !ok
	})

	session.TransactionsPanelFilterChanged("giro")
	state = waitForTransactions(t, session, events, "the filtered panel", func(state transactionsview.TransactionsState) bool {
		return state.AccountFilterText == "giro"
	})
	session.TransactionsPanelFilterChanged("")
	waitForTransactions(t, session, events, "the cleared panel filter", func(state transactionsview.TransactionsState) bool {
		return state.AccountFilterText == ""
	})

	session.TransactionsPanelNewGroup("Urlaub")
	state = waitForTransactions(t, session, events, "the new group", func(state transactionsview.TransactionsState) bool {
		for _, group := range state.CustomGroups {
			if group.Name == "Urlaub" {
				return true
			}
		}
		return false
	})
	var urlaub model.AccountGroupID
	for _, group := range state.CustomGroups {
		if group.Name == "Urlaub" {
			urlaub = group.ID
		}
	}
	session.TransactionsPanelRenameGroup(int(urlaub), "Reisen")
	waitForTransactions(t, session, events, "the renamed group", func(state transactionsview.TransactionsState) bool {
		for _, group := range state.CustomGroups {
			if group.ID == urlaub && group.Name == "Reisen" {
				return true
			}
		}
		return false
	})
	session.TransactionsPanelMoveGroup(int(urlaub), true)
	waitForTransactions(t, session, events, "the moved group", func(state transactionsview.TransactionsState) bool {
		return len(state.CustomGroups) >= 2 && state.CustomGroups[0].ID == urlaub
	})
	session.TransactionsPanelMoveAccount(int(seed.checking), int(urlaub))
	state = waitForTransactions(t, session, events, "the moved account", func(state transactionsview.TransactionsState) bool {
		for _, account := range state.Accounts {
			if account.ID == seed.checking {
				return account.GroupID != nil && *account.GroupID == urlaub
			}
		}
		return false
	})
	session.TransactionsPanelDeleteGroup(int(urlaub))
	waitForTransactions(t, session, events, "the deleted group", func(state transactionsview.TransactionsState) bool {
		for _, group := range state.CustomGroups {
			if group.ID == urlaub {
				return false
			}
		}
		return true
	})

	if countTransactionsAudit(t, session, "account_group.create") < 2 ||
		countTransactionsAudit(t, session, "account_group.rename") != 1 ||
		countTransactionsAudit(t, session, "account_group.delete") != 1 ||
		countTransactionsAudit(t, session, "account.set_group") != 1 {
		t.Fatalf("group audit entries = %+v", transactionsAuditActions(t, session))
	}
}

func TestTransactionsSelectionDetailAndBatchActions(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	state := session.TransactionsState()
	bakery := findTransaction(t, state, "Bakery")
	session.TransactionsToggleSelect(bakery.ID)
	state = waitForTransactions(t, session, events, "the selected booking", func(state transactionsview.TransactionsState) bool {
		_, ok := state.SelectedIDs[bakery.ID]
		return ok
	})
	if len(state.SelectedIDs) != 1 || state.SelectionAnchorID == nil || *state.SelectionAnchorID != bakery.ID {
		t.Fatalf("selection = %+v", state)
	}

	session.TransactionsSelectAll()
	state = waitForTransactions(t, session, events, "every booking selected", func(state transactionsview.TransactionsState) bool {
		return len(state.SelectedIDs) == len(state.Transactions)
	})
	if len(state.SelectedIDs) == 0 {
		t.Fatal("select all selected nothing")
	}
	session.TransactionsClearSelection()
	waitForTransactions(t, session, events, "the cleared selection", func(state transactionsview.TransactionsState) bool {
		return len(state.SelectedIDs) == 0
	})

	foodIndex := categoryOptionIndex(t, state, seed.food)
	session.TransactionsToggleSelect(bakery.ID)
	waitForTransactions(t, session, events, "a selected booking", func(state transactionsview.TransactionsState) bool {
		_, ok := state.SelectedIDs[bakery.ID]
		return ok
	})
	beforeCategories := countTransactionsAudit(t, session, "transaction.update_categories")
	session.TransactionsBatchCategory(foodIndex)
	state = waitForTransactions(t, session, events, "the batch category", func(state transactionsview.TransactionsState) bool {
		transaction := findTransaction(t, state, "Bakery")
		return transaction.CategoryID != nil && *transaction.CategoryID == seed.food
	})
	if countTransactionsAudit(t, session, "transaction.update_categories") != beforeCategories+1 {
		t.Fatalf("batch category audit = %+v", transactionsAuditActions(t, session))
	}

	beforeTax := countTransactionsAudit(t, session, "transaction.set_tax_override")
	session.TransactionsBatchTax(1)
	waitForJobs(t, session)
	if countTransactionsAudit(t, session, "transaction.set_tax_override") != beforeTax+1 {
		t.Fatalf("batch tax audit = %+v", transactionsAuditActions(t, session))
	}

	// The detail panel opens on the row, changes its category and closes.
	session.TransactionsClearSelection()
	waitForTransactions(t, session, events, "no selection", func(state transactionsview.TransactionsState) bool {
		return len(state.SelectedIDs) == 0
	})
	employer := findTransaction(t, state, "Employer")
	session.TransactionsOpenDetail(employer.ID)
	state = waitForTransactions(t, session, events, "the detail panel", func(state transactionsview.TransactionsState) bool {
		return state.Selected != nil && state.Selected.ID == employer.ID
	})
	if state.SelectedDocuments == nil {
		t.Fatal("the detail documents are nil")
	}
	beforeCategory := countTransactionsAudit(t, session, "transaction.set_category")
	session.TransactionsDetailCategory(foodIndex)
	state = waitForTransactions(t, session, events, "the detail category", func(state transactionsview.TransactionsState) bool {
		return state.Selected != nil && state.Selected.CategoryID != nil && *state.Selected.CategoryID == seed.food
	})
	if countTransactionsAudit(t, session, "transaction.set_category") != beforeCategory+1 {
		t.Fatalf("detail category audit = %+v", transactionsAuditActions(t, session))
	}
	session.TransactionsDetailTax(2)
	state = waitForTransactions(t, session, events, "the detail tax state", func(state transactionsview.TransactionsState) bool {
		return state.Selected != nil && state.Selected.TaxOverride != nil && !*state.Selected.TaxOverride
	})
	session.TransactionsDetailShowPartner("Employer")
	state = waitForTransactions(t, session, events, "the partner list", func(state transactionsview.TransactionsState) bool {
		return state.SearchQuery == "Employer"
	})
	if !transactionsShowBackForTest(state) {
		t.Fatal("the partner list has no Back")
	}
	session.TransactionsCloseDetail()
	waitForTransactions(t, session, events, "the closed detail", func(state transactionsview.TransactionsState) bool {
		return state.Selected == nil
	})
}

func TestTransactionsRuleCreationAndAutoCategorize(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	state := session.TransactionsState()
	bakery := findTransaction(t, state, "Bakery")
	session.TransactionsOpenDetail(bakery.ID)
	state = waitForTransactions(t, session, events, "the uncategorized detail", func(state transactionsview.TransactionsState) bool {
		return state.Selected != nil && state.Selected.ID == bakery.ID
	})
	session.TransactionsOpenRule()
	dialog := session.TransactionsDialog()
	if !dialog.RuleOpen || dialog.RulePattern != "Bakery" {
		t.Fatalf("rule dialog = %+v", dialog)
	}
	session.TransactionsSaveRule("   ", 0)
	dialog = session.TransactionsDialog()
	if dialog.RuleError != i18n.Text(i18n.En, i18n.ErrorPatternRequired) {
		t.Fatalf("rule error = %q", dialog.RuleError)
	}
	foodIndex := 0
	for index, category := range state.Categories {
		if category.ID == seed.food {
			foodIndex = index
		}
	}
	beforeRules := countTransactionsAudit(t, session, "rule.create")
	session.TransactionsSaveRule("Bakery", foodIndex)
	state = waitForTransactions(t, session, events, "the categorised booking", func(state transactionsview.TransactionsState) bool {
		transaction := findTransaction(t, state, "Bakery")
		return transaction.CategoryID != nil && *transaction.CategoryID == seed.food
	})
	if session.TransactionsDialog().RuleOpen {
		t.Fatal("the rule dialog stayed open")
	}
	if countTransactionsAudit(t, session, "rule.create") != beforeRules+1 {
		t.Fatalf("rule audit = %+v", transactionsAuditActions(t, session))
	}

	session.TransactionsCloseDetail()
	waitForTransactions(t, session, events, "the closed detail", func(state transactionsview.TransactionsState) bool {
		return state.Selected == nil
	})
}

func TestTransactionsAddDuplicateDeleteAndMultiDelete(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.TransactionsOpenAdd()
	if dialog := session.TransactionsDialog(); !dialog.AddOpen {
		t.Fatalf("add dialog = %+v", dialog)
	}
	session.TransactionsAddSubmit(0, true, "not a date", "5.00", "Bakery", "", 0)
	if dialog := session.TransactionsDialog(); dialog.AddError != i18n.Text(i18n.En, i18n.EnterValidDate) {
		t.Fatalf("add date error = %q", dialog.AddError)
	}
	session.TransactionsAddSubmit(0, true, "14.10.2026", "not a number", "Bakery", "", 0)
	if dialog := session.TransactionsDialog(); dialog.AddError != i18n.Text(i18n.En, i18n.EnterValidAmount) {
		t.Fatalf("add amount error = %q", dialog.AddError)
	}

	beforeAdded := countTransactionsAudit(t, session, "transactions.import")
	state := session.TransactionsState()
	accountIndex := 0
	for index, account := range state.Accounts {
		if account.ID == seed.checking {
			accountIndex = index
		}
	}
	session.TransactionsAddSubmit(accountIndex, true, "14.10.2026", "5.00", "Bakery", "", 0)
	state = waitForTransactions(t, session, events, "the duplicate confirmation", func(state transactionsview.TransactionsState) bool {
		return state.PendingDuplicate != nil
	})
	if state.PendingDuplicate.Amount.Minor != -500 {
		t.Fatalf("pending duplicate = %+v", state.PendingDuplicate)
	}
	session.TransactionsConfirmDuplicate()
	state = waitForTransactions(t, session, events, "the confirmed duplicate", func(state transactionsview.TransactionsState) bool {
		return state.PendingDuplicate == nil && state.Message != nil
	})
	if countTransactionsAudit(t, session, "transactions.import") != beforeAdded+1 {
		t.Fatalf("add audit = %+v", transactionsAuditActions(t, session))
	}

	// A single delete keeps the row until it is confirmed.
	state = session.TransactionsState()
	grocery := findTransaction(t, state, "Grocery")
	rowsBefore := len(state.Transactions)
	session.TransactionsOpenDelete(grocery.ID)
	dialog := session.TransactionsDialog()
	if !dialog.DeleteOpen || dialog.DeleteTitle != i18n.Text(i18n.En, i18n.DeleteTransactionTitle) {
		t.Fatalf("delete dialog = %+v", dialog)
	}
	session.TransactionsCancelDelete()
	waitForTransactions(t, session, events, "the cancelled delete", func(state transactionsview.TransactionsState) bool {
		return !session.TransactionsDialog().DeleteOpen && len(state.Transactions) == rowsBefore
	})
	session.TransactionsOpenDelete(grocery.ID)
	beforeDeleted := countTransactionsAudit(t, session, "transaction.delete")
	session.TransactionsConfirmDelete(grocery.ID)
	state = waitForTransactions(t, session, events, "the deleted booking", func(state transactionsview.TransactionsState) bool {
		return !session.TransactionsDialog().DeleteOpen && len(state.Transactions) == rowsBefore-1
	})
	if countTransactionsAudit(t, session, "transaction.delete") != beforeDeleted+1 {
		t.Fatalf("delete audit = %+v", transactionsAuditActions(t, session))
	}

	// Multi-delete deletes exactly the selected rows.
	session.TransactionsSelectAll()
	state = waitForTransactions(t, session, events, "the full selection", func(state transactionsview.TransactionsState) bool {
		return len(state.SelectedIDs) == len(state.Transactions)
	})
	session.TransactionsOpenMultiDelete()
	dialog = session.TransactionsDialog()
	if !dialog.MultiDeleteOpen || dialog.MultiDeleteTitle == "" {
		t.Fatalf("multi delete dialog = %+v", dialog)
	}
	session.TransactionsCancelMultiDelete()
	waitForTransactions(t, session, events, "the cancelled multi delete", func(state transactionsview.TransactionsState) bool {
		return !session.TransactionsDialog().MultiDeleteOpen
	})
	session.TransactionsOpenMultiDelete()
	beforeMulti := countTransactionsAudit(t, session, "transaction.delete_all")
	session.TransactionsConfirmMultiDelete()
	state = waitForTransactions(t, session, events, "the deleted selection", func(state transactionsview.TransactionsState) bool {
		return !session.TransactionsDialog().MultiDeleteOpen && len(state.Transactions) == 0
	})
	if countTransactionsAudit(t, session, "transaction.delete_all") != beforeMulti+1 {
		t.Fatalf("multi delete audit = %+v", transactionsAuditActions(t, session))
	}
	if state.Message == nil {
		t.Fatal("multi delete left no message")
	}
}

func TestTransactionsReapplyPreviewConfirmAndUndo(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	if _, err := rules.Create(context.Background(), sessionDatabase(t, session), model.NewRule{
		Pattern:    "Bakery",
		CategoryID: seed.food,
		Priority:   5,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	session.TransactionsReapplyRules()
	state := waitForTransactions(t, session, events, "the re-apply preview", func(state transactionsview.TransactionsState) bool {
		return state.ReapplyPreview != nil
	})
	if len(state.ReapplyPreview.Groups) == 0 || len(state.ReapplyPreview.Changes) == 0 {
		t.Fatalf("preview = %+v", state.ReapplyPreview)
	}
	session.TransactionsCancelReapplyRules()
	waitForTransactions(t, session, events, "the cancelled preview", func(state transactionsview.TransactionsState) bool {
		return state.ReapplyPreview == nil
	})

	session.TransactionsReapplyRules()
	state = waitForTransactions(t, session, events, "the preview again", func(state transactionsview.TransactionsState) bool {
		return state.ReapplyPreview != nil
	})
	group := state.ReapplyPreview.Groups[0]
	from := -1
	if group.FromCategoryID != nil {
		from = int(*group.FromCategoryID)
	}
	session.TransactionsToggleReapplyGroup(from, int(group.ToCategoryID))
	if !transactionsReapplyExcludedContains(session, group.FromCategoryID, group.ToCategoryID) {
		t.Fatal("the group was not unticked")
	}
	session.TransactionsToggleReapplyGroup(from, int(group.ToCategoryID))
	if transactionsReapplyExcludedContains(session, group.FromCategoryID, group.ToCategoryID) {
		t.Fatal("the group was not ticked again")
	}

	beforeReapply := countTransactionsAudit(t, session, "transaction.update_categories")
	session.TransactionsConfirmReapplyRules()
	state = waitForTransactions(t, session, events, "the reapplied bookings", func(state transactionsview.TransactionsState) bool {
		return state.ReapplyPreview == nil && state.CanUndoReapply && state.Message != nil
	})
	if countTransactionsAudit(t, session, "transaction.update_categories") != beforeReapply+1 {
		t.Fatalf("reapply audit = %+v", transactionsAuditActions(t, session))
	}
	transaction := findTransaction(t, state, "Bakery")
	if transaction.CategoryID == nil || *transaction.CategoryID != seed.food {
		t.Fatalf("reapplied category = %+v", transaction.CategoryID)
	}

	beforeUndo := countTransactionsAudit(t, session, "transaction.update_categories")
	session.TransactionsUndoReapply()
	state = waitForTransactions(t, session, events, "the undone re-apply", func(state transactionsview.TransactionsState) bool {
		return !state.CanUndoReapply && state.Message != nil
	})
	if countTransactionsAudit(t, session, "transaction.update_categories") != beforeUndo+1 {
		t.Fatalf("undo audit = %+v", transactionsAuditActions(t, session))
	}
	transaction = findTransaction(t, state, "Bakery")
	if transaction.CategoryID != nil {
		t.Fatalf("undo left category = %+v", transaction.CategoryID)
	}
}

func TestTransactionsAutoCategorizeAppliesRules(t *testing.T) {
	session, seed := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	if _, err := rules.Create(context.Background(), sessionDatabase(t, session), model.NewRule{
		Pattern:    "Bakery",
		CategoryID: seed.food,
		Priority:   5,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	session.TransactionsAutoCategorize()
	state := waitForTransactions(t, session, events, "the auto categorization", func(state transactionsview.TransactionsState) bool {
		return state.Message != nil
	})
	transaction := findTransaction(t, state, "Bakery")
	if transaction.CategoryID == nil || *transaction.CategoryID != seed.food {
		t.Fatalf("auto category = %+v", transaction.CategoryID)
	}
}

func TestTransactionsOpenWithAddOpensTheDialogAfterTheLoad(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	session.TransactionsOpenWithAdd()
	waitForTransactions(t, session, events, "the add dialog after the load", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && len(state.Accounts) > 0
	})
	dialog := session.TransactionsDialog()
	if !dialog.AddOpen {
		t.Fatalf("add dialog = %+v", dialog)
	}
	if screen := session.CurrentScreen(); screen != "transactions" {
		t.Fatalf("screen = %q", screen)
	}
	if args := session.NavCurrent().Transactions; !reflect.DeepEqual(args, transactionsview.StartArgs{}) {
		t.Fatalf("navigation args = %+v", args)
	}
	if session.NavLen() != 0 {
		t.Fatalf("the menu action left %d back entries", session.NavLen())
	}
}

func TestTransactionsPendingSearchKeepsTheLatestQuery(t *testing.T) {
	var pending pendingTransactionsSearch
	pending.deferQuery("first")
	pending.deferQuery("second")
	if got, ok := pending.take(); !ok || got != "second" {
		t.Fatalf("take = %q, %v; want second", got, ok)
	}
	if _, ok := pending.take(); ok {
		t.Fatal("take returned a query twice")
	}
	pending.deferQuery("old")
	old, _ := pending.take()
	pending.deferQuery("new")
	pending.restore(old)
	if got, ok := pending.take(); !ok || got != "new" {
		t.Fatalf("take after restore = %q, %v; want new", got, ok)
	}
}

func TestTransactionsSearchTypedDuringALoadWins(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	}

	session.TransactionsPeriodChanged(5)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the period load did not start")
	}
	session.TransactionsSearch("Bakery")
	session.TransactionsSearch("Employer")
	close(release)

	state := waitForTransactions(t, session, events, "the latest search", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.SearchQuery == "Employer" && state.Period == transactionsview.PeriodAll
	})
	if len(state.Transactions) != 1 {
		t.Fatalf("Employer rows = %d, want 1", len(state.Transactions))
	}
	if state.Transactions[0].PartnerName == nil || *state.Transactions[0].PartnerName != "Employer" {
		t.Fatalf("row = %+v", state.Transactions[0])
	}
}

func TestASecondTransactionsActionIsRefusedWhileOneRuns(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	session.TransactionsPeriodChanged(5)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first action did not start")
	}
	if session.profileBusy.Active() {
		t.Fatal("an ordinary transaction job touched the profile busy flag")
	}
	session.TransactionsPanelNewGroup("Refused")
	close(release)
	state := waitForTransactions(t, session, events, "the first action", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Period == transactionsview.PeriodAll
	})
	for _, group := range state.CustomGroups {
		if group.Name == "Refused" {
			t.Fatal("the refused action changed the state")
		}
	}
	if session.transactionsWork.Active() {
		t.Fatal("the busy section did not end")
	}
	if session.profileBusy.Active() {
		t.Fatal("the profile busy flag stayed set")
	}
}

func TestASupersededTransactionsLoadIsDropped(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.TransactionsPeriodChanged(0)
	first := <-entered
	session.transactionsWork.End()
	session.TransactionsPeriodChanged(5)
	second := <-entered

	close(second)
	waitForTransactions(t, session, events, "the second load", func(state transactionsview.TransactionsState) bool {
		return !state.IsLoading && state.Period == transactionsview.PeriodAll
	})
	close(first)
	waitForJobs(t, session)

	state := session.TransactionsState()
	if state.Period != transactionsview.PeriodAll {
		t.Fatalf("the superseded load published %+v", state.Period)
	}
	if state.IsLoading {
		t.Fatalf("the superseded load published the loading state: %+v", state)
	}
}

func TestTeardownDuringATransactionsLoadDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		if page != "transactions" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.TransactionsPeriodChanged(5)
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the transactions load")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the transactions job did not end")
	}

	requireClosed(t, old, "the old database after the transactions job ends")
	state := session.TransactionsState()
	if len(state.Transactions) != 0 || state.Selected != nil {
		t.Fatalf("the teardown did not drop the load: %+v", state)
	}
	if dialog := session.TransactionsDialog(); dialog.AddOpen || dialog.RangeOpen {
		t.Fatalf("the teardown kept a dialog: %+v", dialog)
	}
}

func TestTransactionsStateIsSafeWhileActionsRun(t *testing.T) {
	session, _ := newLoadedTransactionsSession(t)
	events, remove := listenForChanges(session)
	defer remove()

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.TransactionsState()
				_ = session.TransactionsDialog()
				// A short pause keeps the reader overlapping every action
				// without starving the job of the session mutex on a slow,
				// race-instrumented CI runner.
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	for _, index := range []int{5, 4, 3, 2} {
		session.TransactionsPanelFilterChanged(fmt.Sprintf("filter-%d", index))
		waitForTransactions(t, session, events, "a panel filter while reading", func(state transactionsview.TransactionsState) bool {
			return state.AccountFilterText == fmt.Sprintf("filter-%d", index)
		})
		period, _ := transactionsPeriodAt(index)
		session.TransactionsPeriodChanged(index)
		waitForTransactions(t, session, events, "a period while reading", func(state transactionsview.TransactionsState) bool {
			return !state.IsLoading && state.Period == period
		})
	}
	session.TransactionsPanelFilterChanged("")

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadTransactionsWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedTransactionsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)

	called := make(chan struct{}, 8)
	session.AddListener(func(Change) {
		_ = session.TransactionsState()
		_ = session.TransactionsDialog()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func TestTransactionsNavigationLinksArriveWithTheirStartArgs(t *testing.T) {
	october := dashboard.NewYearMonth(2026, 10)
	november := dashboard.NewYearMonth(2026, 11)

	tests := []struct {
		name   string
		open   func(*Session)
		screen string
		check  func(*testing.T, transactionsview.StartArgs)
	}{
		{
			name:   "category",
			open:   func(session *Session) { session.DashboardOpenCategory(7) },
			screen: "transactions",
			check: func(t *testing.T, args transactionsview.StartArgs) {
				t.Helper()
				if args.CategoryID == nil || *args.CategoryID != model.CategoryID(7) {
					t.Fatalf("category = %+v", args.CategoryID)
				}
				if args.Month == nil || *args.Month != october {
					t.Fatalf("month = %+v", args.Month)
				}
			},
		},
		{
			name:   "uncategorized",
			open:   func(session *Session) { session.DashboardOpenUncategorized() },
			screen: "transactions",
			check: func(t *testing.T, args transactionsview.StartArgs) {
				t.Helper()
				if !args.UncategorizedOnly {
					t.Fatal("uncategorized only is false")
				}
				if args.Month == nil || *args.Month != october {
					t.Fatalf("month = %+v", args.Month)
				}
			},
		},
		{
			name:   "partner",
			open:   func(session *Session) { session.DashboardOpenPartner("Bakery") },
			screen: "transactions",
			check: func(t *testing.T, args transactionsview.StartArgs) {
				t.Helper()
				if args.Partner == nil || *args.Partner != "Bakery" {
					t.Fatalf("partner = %+v", args.Partner)
				}
			},
		},
		{
			name:   "account",
			open:   func(session *Session) { session.AccountsOpenTransactions(9) },
			screen: "transactions",
			check: func(t *testing.T, args transactionsview.StartArgs) {
				t.Helper()
				if args.AccountID == nil || *args.AccountID != model.AccountID(9) {
					t.Fatalf("account = %+v", args.AccountID)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newTestSession(t, testPaths(t))
			setDashboardMonths(session, october, november)
			if test.name == "account" {
				session.Navigate("accounts")
			}

			test.open(session)
			if screen := session.CurrentScreen(); screen != test.screen {
				t.Fatalf("screen = %q, want %q", screen, test.screen)
			}
			test.check(t, session.NavCurrent().Transactions)
			target, ok := session.NavTarget()
			if !ok {
				t.Fatal("the link left no back entry")
			}
			if test.name == "account" {
				if target.Screen != "accounts" {
					t.Fatalf("back target = %+v", target)
				}
			} else if target.Screen != "dashboard" {
				t.Fatalf("back target = %+v", target)
			}
		})
	}
}

func TestTransactionsBackRestoresTheArgumentsALinkLeftWith(t *testing.T) {
	period := transactionsview.PeriodThisYear
	category := model.CategoryID(4)
	leave := PageArgs{
		Transactions: transactionsview.StartArgs{
			Period:     &period,
			CategoryID: &category,
		},
	}

	var nav NavState
	nav.Follow("transactions", "reports", leave, PageArgs{})
	entry, ok := nav.Back()
	if !ok {
		t.Fatal("a link leaves a back entry")
	}
	if !reflect.DeepEqual(entry.Args, leave) {
		t.Fatalf("restored args = %+v, want %+v", entry.Args, leave)
	}
}

func mustTransactionsDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func transactionsReapplyExcludedContains(
	session *Session,
	from *model.CategoryID,
	to model.CategoryID,
) bool {
	for _, key := range session.TransactionsReapplyExcludedKeys() {
		fromMatches := (key.FromCategoryID == nil) == (from == nil)
		if key.FromCategoryID != nil && from != nil && *key.FromCategoryID != *from {
			fromMatches = false
		}
		if fromMatches && key.ToCategoryID == to {
			return true
		}
	}
	return false
}

func transactionsShowBackForTest(state transactionsview.TransactionsState) bool {
	return state.SearchQuery != "" &&
		state.AccountID == nil &&
		state.Period == transactionsview.PeriodAll &&
		state.Direction == model.DirectionAll &&
		state.CategoryFilter.Kind == transactionsview.CategoryFilterKindAny &&
		state.CustomRange == nil &&
		state.MinAmount == nil &&
		state.MaxAmount == nil
}
