package bind

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/balance"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accountgroups"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/pendingbookings"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type bindTransactionsSeed struct {
	checking model.AccountID
	savings  model.AccountID
	food     model.CategoryID
	salary   model.CategoryID
	group    model.AccountGroupID
}

type transactionsFixture struct {
	paths   paths.AppPaths
	session *shell.Session
	shell   *Shell
	service *Transactions
	router  *Router
	seed    bindTransactionsSeed

	mu          sync.Mutex
	shellEvents []ShellDTO
	events      chan TransactionsDTO
}

func seedBindTransactions(t *testing.T, appPaths paths.AppPaths) bindTransactionsSeed {
	t.Helper()
	ctx := context.Background()
	database, err := unlock.Create(appPaths, "default", []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}

	checkingName := "Girokonto"
	checking, err := accounts.Create(ctx, database, model.NewAccount{
		Name:        &checkingName,
		IBAN:        stringValue("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create checking: %v", err)
	}
	savings, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringValue("DE44500105175407324931"),
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
			ImportHash:  fmt.Sprintf("bind-%s-%d-%s", day.String(), minor, partner),
			CreatedAt:   testNow.UTC(),
		}}); err != nil {
			t.Fatalf("insert %s: %v", partner, err)
		}
	}
	insert(civil.FromTime(testNow).AddDays(-2), 2_000, "Employer", &salary, stringValue("Salary October"))
	insert(civil.FromTime(testNow).AddDays(-1), -500, "Bakery", nil, nil)
	insert(civil.FromTime(testNow).AddDays(-1), -250, "Grocery", &food, stringValue("Groceries"))
	insert(civil.FromTime(testNow).AddDays(-40), -700, "Old Shop", nil, nil)

	if err := pendingbookings.ReplaceForAccount(ctx, database, checking, []model.NewPendingBooking{{
		Date:        civil.FromTime(testNow),
		Amount:      money.Money{Minor: -50, Currency: money.EUR},
		PartnerName: stringValue("Pending partner"),
		Purpose:     stringValue("Pending purpose"),
	}}, testNow.UTC()); err != nil {
		t.Fatalf("insert pending: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return bindTransactionsSeed{
		checking: checking,
		savings:  savings,
		food:     food,
		salary:   salary,
		group:    group,
	}
}

func newTransactionsFixture(t *testing.T) *transactionsFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seed := seedBindTransactions(t, appPaths)

	session, offer, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	if offer != nil {
		t.Fatal("a nil FinDesk root offered a migration")
	}

	fixture := &transactionsFixture{
		paths:   appPaths,
		session: session,
		seed:    seed,
		events:  make(chan TransactionsDTO, 256),
	}
	fixture.shell = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
		Emit: func(dto ShellDTO) {
			fixture.mu.Lock()
			fixture.shellEvents = append(fixture.shellEvents, dto)
			fixture.mu.Unlock()
		},
	})
	fixture.service = NewTransactions(session, TransactionsOptions{
		Emit: func(dto TransactionsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("transactions", fixture.service)
	fixture.router.RegisterShell(fixture.shell)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.service.ServiceShutdown()
		_ = fixture.shell.ServiceShutdown()
	})
	return fixture
}

func (f *transactionsFixture) unlock(t *testing.T) {
	t.Helper()
	if _, err := f.shell.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func (f *transactionsFixture) wait(
	t *testing.T,
	description string,
	predicate func(TransactionsDTO) bool,
	fallback ...TransactionsDTO,
) TransactionsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.events:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			if len(fallback) > 0 {
				return fallback[0]
			}
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func (f *transactionsFixture) drain() {
	for len(f.events) > 0 {
		<-f.events
	}
}

func (f *transactionsFixture) load(t *testing.T) TransactionsDTO {
	t.Helper()
	f.unlock(t)
	dto, err := f.service.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f.wait(t, "the loaded transactions", func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.HasAccounts && len(dto.Rows) > 0
	}, dto)
}

func (f *transactionsFixture) action(
	t *testing.T,
	description string,
	run func() (TransactionsDTO, error),
	predicate func(TransactionsDTO) bool,
) TransactionsDTO {
	t.Helper()
	_, err := run()
	if err != nil {
		t.Fatalf("%s: %v", description, err)
	}
	return f.wait(t, description, predicate)
}

func accountChoiceIndexLabel(values []string, label string) int {
	for index, value := range values {
		if value == label {
			return index
		}
	}
	return 0
}

func transactionRowByTitle(dto TransactionsDTO, title string) *TransactionRowDTO {
	for index := range dto.Rows {
		if dto.Rows[index].Title == title {
			return &dto.Rows[index]
		}
	}
	return nil
}

func TestTransactionsServiceLoadsEveryAppliedField(t *testing.T) {
	fixture := newTransactionsFixture(t)
	dto := fixture.load(t)

	if dto.ErrorText != "" || dto.MessageText != "" {
		t.Fatalf("messages = %q / %q", dto.ErrorText, dto.MessageText)
	}
	if !dto.HasAccounts || !dto.HasTransactions || dto.HasBalance || dto.HasChart {
		t.Fatalf("flags = accounts %v transactions %v balance %v chart %v",
			dto.HasAccounts, dto.HasTransactions, dto.HasBalance, dto.HasChart)
	}
	if dto.CountText != "3" {
		t.Fatalf("count = %q, want 3", dto.CountText)
	}
	if len(dto.IncomeAmounts) != 1 || dto.IncomeAmounts[0].Text != "€20.00" ||
		dto.IncomeAmounts[0].Tone != "positive" {
		t.Fatalf("income = %+v", dto.IncomeAmounts)
	}
	if len(dto.ExpenseAmounts) != 1 || dto.ExpenseAmounts[0].Text != "€7.50" ||
		dto.ExpenseAmounts[0].Tone != "negative" {
		t.Fatalf("expenses = %+v", dto.ExpenseAmounts)
	}
	if len(dto.PeriodOptions) != 7 || dto.PeriodIndex != 0 {
		t.Fatalf("period = %+v / %d", dto.PeriodOptions, dto.PeriodIndex)
	}
	if dto.PeriodOptions[6] != i18n.Text(i18n.En, i18n.PeriodCustomRange) {
		t.Fatalf("custom period option = %q", dto.PeriodOptions[6])
	}
	if len(dto.DirectionOptions) != 3 || dto.DirectionIndex != 0 {
		t.Fatalf("direction = %+v / %d", dto.DirectionOptions, dto.DirectionIndex)
	}
	if len(dto.CategoryOptions) != 4 || dto.CategoryIndex != 0 {
		t.Fatalf("categories = %+v / %d", dto.CategoryOptions, dto.CategoryIndex)
	}
	if dto.AmountLabel != i18n.Text(i18n.En, i18n.AmountFilterAny) {
		t.Fatalf("amount label = %q", dto.AmountLabel)
	}
	if dto.AddAmountLabel != i18n.Format(i18n.En, i18n.AmountIn, i18n.Str("€")) {
		t.Fatalf("add amount label = %q", dto.AddAmountLabel)
	}
	if !reflect.DeepEqual(dto.AddCategoryNames, []string{i18n.Text(i18n.En, i18n.AutoRules), "Food", "Salary"}) {
		t.Fatalf("add categories = %+v", dto.AddCategoryNames)
	}
	if !reflect.DeepEqual(dto.CategoryNames, []string{
		i18n.Text(i18n.En, i18n.Uncategorized),
		"Food (" + i18n.Text(i18n.En, i18n.Tax) + ")",
		"Salary",
	}) {
		t.Fatalf("category names = %+v", dto.CategoryNames)
	}
	if !reflect.DeepEqual(dto.TaxOptions, []string{
		i18n.Text(i18n.En, i18n.TaxAutomatic),
		i18n.Text(i18n.En, i18n.TaxRelevantChoice),
		i18n.Text(i18n.En, i18n.TaxNotRelevantChoice),
	}) {
		t.Fatalf("tax options = %+v", dto.TaxOptions)
	}
	if dto.SelectionVisible || dto.AllSelected || dto.SelectionCountText != i18n.Format(i18n.En, i18n.SelectedCount, i18n.Int(0)) {
		t.Fatalf("selection = %+v", dto)
	}
	if dto.DetailOpen || dto.DuplicateOpen || dto.ReapplyOpen || dto.CanUndoReapply {
		t.Fatalf("dialogs = detail %v duplicate %v reapply %v undo %v",
			dto.DetailOpen, dto.DuplicateOpen, dto.ReapplyOpen, dto.CanUndoReapply)
	}
	if len(dto.PanelRows) == 0 || dto.PanelRows[0].Kind != "all" {
		t.Fatalf("panel rows = %+v", dto.PanelRows)
	}
	var checking *AccountPanelRowDTO
	for index := range dto.PanelRows {
		row := &dto.PanelRows[index]
		if row.Kind == "account" && row.ID == int(fixture.seed.checking) {
			checking = row
		}
	}
	if checking == nil || checking.Label != "Girokonto" || checking.Secondary != "…3000" {
		t.Fatalf("checking row = %+v", checking)
	}
	if len(dto.GroupChoices) != 1 || dto.GroupChoices[0].Name != "Reisen" {
		t.Fatalf("group choices = %+v", dto.GroupChoices)
	}

	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(state, dto) {
		t.Fatalf("State = %+v, want the loaded DTO %+v", state, dto)
	}
}

func TestTransactionsServiceActionsChangeTheDTO(t *testing.T) {
	fixture := newTransactionsFixture(t)
	fixture.load(t)

	dto := fixture.action(t, "the search", func() (TransactionsDTO, error) {
		return fixture.service.Search("Bakery")
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && len(dto.Rows) == 1 && dto.Rows[0].Title == "Bakery"
	})
	if dto.ShowBack {
		t.Fatal("a plain search offered Back")
	}
	dto = fixture.action(t, "the cleared search", func() (TransactionsDTO, error) {
		return fixture.service.Back()
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && len(dto.Rows) == 3
	})

	dto = fixture.action(t, "all time", func() (TransactionsDTO, error) {
		return fixture.service.PeriodChanged(5)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.PeriodIndex == 5 && len(dto.Rows) == 4
	})
	if dto.PeriodExtra != "" {
		t.Fatalf("period extra = %q", dto.PeriodExtra)
	}

	dto = fixture.action(t, "expenses", func() (TransactionsDTO, error) {
		return fixture.service.DirectionChanged(2)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.DirectionIndex == 2 && len(dto.Rows) == 3
	})
	if dto.DirectionOptions[2] != i18n.Text(i18n.En, i18n.DirectionExpenses) {
		t.Fatalf("direction option = %q", dto.DirectionOptions[2])
	}

	dto = fixture.action(t, "the first category", func() (TransactionsDTO, error) {
		return fixture.service.CategoryChanged(2)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.CategoryIndex == 2 && len(dto.Rows) == 1
	})
	if dto.CategoryOptions[2] != "Food" {
		t.Fatalf("category option = %q", dto.CategoryOptions[2])
	}
	fixture.action(t, "all categories", func() (TransactionsDTO, error) {
		return fixture.service.CategoryChanged(0)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.CategoryIndex == 0
	})

	fixture.action(t, "every direction", func() (TransactionsDTO, error) {
		return fixture.service.DirectionChanged(0)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.DirectionIndex == 0
	})
	dto = fixture.action(t, "the checking account", func() (TransactionsDTO, error) {
		return fixture.service.PanelSelect("account", "", int(fixture.seed.checking), "")
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && dto.AccountIndex > 0 && len(dto.Rows) > 0
	})
	if !dto.Rows[0].Pending || dto.Rows[0].Title != "Pending partner" ||
		dto.Rows[0].AmountText != "-€0.50" {
		t.Fatalf("pending row = %+v", dto.Rows[0])
	}
	if !dto.HasBalance || !dto.HasChart || !dto.HasBalanceColumn {
		t.Fatalf("balance flags = %v / %v / %v", dto.HasBalance, dto.HasChart, dto.HasBalanceColumn)
	}
	if dto.BalanceText == "" || dto.ChartLine == "" || !dto.ChartNotFromZero {
		t.Fatalf("balance = %q chart %q not-from-zero %v", dto.BalanceText, dto.ChartLine, dto.ChartNotFromZero)
	}
	if !strings.HasPrefix(dto.ChartLine, "M 0.00") || !strings.Contains(dto.ChartLine, "L 700.00") {
		t.Fatalf("chart line = %q", dto.ChartLine)
	}

	fixture.action(t, "the collapsed chart", func() (TransactionsDTO, error) {
		return fixture.service.ToggleChart()
	}, func(dto TransactionsDTO) bool {
		return dto.ChartCollapsed
	})

	fixture.action(t, "the all selection", func() (TransactionsDTO, error) {
		return fixture.service.SelectAll()
	}, func(dto TransactionsDTO) bool {
		return dto.SelectionVisible && dto.AllSelected && len(dto.SelectionTotalsText) > 0
	})
	fixture.action(t, "the cleared selection", func() (TransactionsDTO, error) {
		return fixture.service.ClearSelection()
	}, func(dto TransactionsDTO) bool {
		return !dto.SelectionVisible && !dto.AllSelected
	})

	fixture.action(t, "the panel filter", func() (TransactionsDTO, error) {
		return fixture.service.PanelFilterChanged("giro")
	}, func(dto TransactionsDTO) bool {
		return dto.PanelFilterText == "giro" && dto.PanelShownText != ""
	})
}

func TestTransactionsServiceRangeAmountDialogs(t *testing.T) {
	fixture := newTransactionsFixture(t)
	fixture.load(t)

	dto := fixture.action(t, "the open range", func() (TransactionsDTO, error) {
		return fixture.service.OpenRange()
	}, func(dto TransactionsDTO) bool {
		return dto.RangeOpen && dto.RangeFrom != "" && dto.RangeTo != ""
	})
	dto = fixture.action(t, "the invalid range", func() (TransactionsDTO, error) {
		return fixture.service.ApplyRange("not a date", dto.RangeTo)
	}, func(dto TransactionsDTO) bool {
		return dto.RangeError == i18n.Text(i18n.En, i18n.EnterValidDate)
	})
	dto = fixture.action(t, "the reversed range", func() (TransactionsDTO, error) {
		return fixture.service.ApplyRange("15.10.2026", "01.10.2026")
	}, func(dto TransactionsDTO) bool {
		return dto.RangeError == i18n.Text(i18n.En, i18n.InvalidDateRange)
	})
	dto = fixture.action(t, "the custom range", func() (TransactionsDTO, error) {
		return fixture.service.ApplyRange("01.10.2026", "15.10.2026")
	}, func(dto TransactionsDTO) bool {
		return !dto.RangeOpen && strings.Contains(dto.PeriodExtra, "01.10.2026")
	})
	if dto.RangeError != "" {
		t.Fatalf("range error = %q", dto.RangeError)
	}

	dto = fixture.action(t, "the open amount", func() (TransactionsDTO, error) {
		return fixture.service.OpenAmount()
	}, func(dto TransactionsDTO) bool {
		return dto.AmountOpen && dto.AmountMinLabel != "" && dto.AmountMaxLabel != ""
	})
	dto = fixture.action(t, "the invalid amount", func() (TransactionsDTO, error) {
		return fixture.service.ApplyAmount("nope", "")
	}, func(dto TransactionsDTO) bool {
		return dto.AmountError == i18n.Text(i18n.En, i18n.EnterValidAmount)
	})
	dto = fixture.action(t, "the amount bounds", func() (TransactionsDTO, error) {
		return fixture.service.ApplyAmount("2.50", "5.00")
	}, func(dto TransactionsDTO) bool {
		return !dto.AmountOpen && strings.Contains(dto.AmountLabel, "2.50") && strings.Contains(dto.AmountLabel, "5.00")
	})
	if dto.AmountError != "" {
		t.Fatalf("amount error = %q", dto.AmountError)
	}
	fixture.action(t, "the reset amount", func() (TransactionsDTO, error) {
		return fixture.service.ResetAmount()
	}, func(dto TransactionsDTO) bool {
		return dto.AmountLabel == i18n.Text(i18n.En, i18n.AmountFilterAny)
	})
}

func TestTransactionsServiceAddRuleDeleteAndReapply(t *testing.T) {
	fixture := newTransactionsFixture(t)
	fixture.load(t)

	fixture.action(t, "the open add", func() (TransactionsDTO, error) {
		return fixture.service.OpenAdd()
	}, func(dto TransactionsDTO) bool {
		return dto.AddOpen
	})
	dto := fixture.action(t, "the add date error", func() (TransactionsDTO, error) {
		return fixture.service.AddSubmit(0, true, "not a date", "5.00", "Bakery", "", 0)
	}, func(dto TransactionsDTO) bool {
		return dto.AddError == i18n.Text(i18n.En, i18n.EnterValidDate)
	})
	dto = fixture.action(t, "the duplicate booking", func() (TransactionsDTO, error) {
		return fixture.service.AddSubmit(accountChoiceIndexLabel(dto.AddAccountOptions, "Girokonto"), true, "14.10.2026", "5.00", "Bakery", "", 0)
	}, func(dto TransactionsDTO) bool {
		return dto.DuplicateOpen && dto.DuplicateTitle != "" && dto.DuplicateText != ""
	})
	fixture.action(t, "the dismissed duplicate", func() (TransactionsDTO, error) {
		return fixture.service.DismissDuplicate()
	}, func(dto TransactionsDTO) bool {
		return !dto.DuplicateOpen
	})

	dto = fixture.action(t, "the detail", func() (TransactionsDTO, error) {
		row := transactionRowByTitle(dto, "Bakery")
		return fixture.service.OpenDetail(row.ID)
	}, func(dto TransactionsDTO) bool {
		return dto.DetailOpen && dto.DetailTitle == "Bakery"
	})
	if dto.DetailCategoryIndex != 0 || dto.DetailTaxIndex != 0 || !dto.DetailCanCreateRule {
		t.Fatalf("detail indexes = %+v", dto)
	}
	dto = fixture.action(t, "the rule dialog", func() (TransactionsDTO, error) {
		return fixture.service.OpenRule()
	}, func(dto TransactionsDTO) bool {
		return dto.RuleOpen && dto.RulePattern == "Bakery" && len(dto.RuleCategories) == 2
	})
	dto = fixture.action(t, "the empty rule", func() (TransactionsDTO, error) {
		return fixture.service.SaveRule("  ", 0)
	}, func(dto TransactionsDTO) bool {
		return dto.RuleError == i18n.Text(i18n.En, i18n.ErrorPatternRequired)
	})
	dto = fixture.action(t, "the created rule", func() (TransactionsDTO, error) {
		return fixture.service.SaveRule("Bakery", 0)
	}, func(dto TransactionsDTO) bool {
		return !dto.RuleOpen && dto.MessageText != ""
	})
	if row := transactionRowByTitle(dto, "Bakery"); row == nil || row.CategoryText != "Food" {
		t.Fatalf("categorised row = %+v (message %q, rows %+v)", row, dto.MessageText, dto.Rows)
	}

	// Make the booking uncategorized again so the preview has a change to offer.
	bakery := transactionRowByTitle(dto, "Bakery")
	dto = fixture.action(t, "the bakery selection", func() (TransactionsDTO, error) {
		return fixture.service.ToggleSelect(bakery.ID)
	}, func(dto TransactionsDTO) bool {
		return dto.SelectionVisible
	})
	dto = fixture.action(t, "the uncategorized bakery", func() (TransactionsDTO, error) {
		return fixture.service.BatchCategory(0)
	}, func(dto TransactionsDTO) bool {
		row := transactionRowByTitle(dto, "Bakery")
		return row != nil && row.CategoryText == i18n.Text(i18n.En, i18n.Uncategorized)
	})

	dto = fixture.action(t, "the reapply preview", func() (TransactionsDTO, error) {
		return fixture.service.ReapplyRules()
	}, func(dto TransactionsDTO) bool {
		return dto.ReapplyOpen && len(dto.ReapplyGroups) > 0
	})
	if dto.ReapplySummary == "" || dto.ReapplyWarning == "" || !dto.ReapplyConfirmEnabled {
		t.Fatalf("reapply = %+v", dto)
	}
	group := dto.ReapplyGroups[0]
	dto = fixture.action(t, "the unticked group", func() (TransactionsDTO, error) {
		return fixture.service.ToggleReapplyGroup(group.FromID, group.ToID)
	}, func(dto TransactionsDTO) bool {
		return !dto.ReapplyGroups[0].Ticked && !dto.ReapplyConfirmEnabled
	})
	dto = fixture.action(t, "the ticked group", func() (TransactionsDTO, error) {
		return fixture.service.ToggleReapplyGroup(group.FromID, group.ToID)
	}, func(dto TransactionsDTO) bool {
		return dto.ReapplyGroups[0].Ticked
	})
	dto = fixture.action(t, "the confirmed preview", func() (TransactionsDTO, error) {
		return fixture.service.ConfirmReapplyRules()
	}, func(dto TransactionsDTO) bool {
		return !dto.ReapplyOpen && dto.CanUndoReapply && dto.MessageText != ""
	})
	if row := transactionRowByTitle(dto, "Bakery"); row == nil || row.CategoryText != "Food" {
		t.Fatalf("reapplied row = %+v", row)
	}
	fixture.action(t, "the undone reapply", func() (TransactionsDTO, error) {
		return fixture.service.UndoReapply()
	}, func(dto TransactionsDTO) bool {
		row := transactionRowByTitle(dto, "Bakery")
		return !dto.CanUndoReapply && row != nil && row.CategoryText == i18n.Text(i18n.En, i18n.Uncategorized)
	})

	dto = fixture.action(t, "the delete dialog", func() (TransactionsDTO, error) {
		row := transactionRowByTitle(dto, "Grocery")
		return fixture.service.OpenDelete(row.ID)
	}, func(dto TransactionsDTO) bool {
		return dto.DeleteOpen && dto.DeleteID > 0 && dto.DeleteText != ""
	})
	fixture.action(t, "the cancelled delete", func() (TransactionsDTO, error) {
		return fixture.service.CancelDelete()
	}, func(dto TransactionsDTO) bool {
		return !dto.DeleteOpen
	})
}

func TestTransactionsServiceDeleteMultiDeleteAndBatch(t *testing.T) {
	fixture := newTransactionsFixture(t)
	dto := fixture.load(t)

	dto = fixture.action(t, "the grocery delete", func() (TransactionsDTO, error) {
		row := transactionRowByTitle(dto, "Grocery")
		return fixture.service.ConfirmDelete(row.ID)
	}, func(dto TransactionsDTO) bool {
		return !dto.Loading && transactionRowByTitle(dto, "Grocery") == nil
	})

	dto = fixture.action(t, "the all selection", func() (TransactionsDTO, error) {
		return fixture.service.SelectAll()
	}, func(dto TransactionsDTO) bool {
		return dto.AllSelected
	})
	dto = fixture.action(t, "the multi delete dialog", func() (TransactionsDTO, error) {
		return fixture.service.OpenMultiDelete()
	}, func(dto TransactionsDTO) bool {
		return dto.MultiDeleteOpen && dto.MultiDeleteTitle != "" && dto.MultiDeleteRange != ""
	})
	fixture.action(t, "the cancelled multi delete", func() (TransactionsDTO, error) {
		return fixture.service.CancelMultiDelete()
	}, func(dto TransactionsDTO) bool {
		return !dto.MultiDeleteOpen
	})
	fixture.action(t, "the multi deleted rows", func() (TransactionsDTO, error) {
		return fixture.service.ConfirmMultiDelete()
	}, func(dto TransactionsDTO) bool {
		return !dto.MultiDeleteOpen && len(dto.Rows) == 0 && dto.MessageText != ""
	})
}

func TestTransactionsServiceBatchCategoryAndTax(t *testing.T) {
	fixture := newTransactionsFixture(t)
	dto := fixture.load(t)

	dto = fixture.action(t, "the bakery selection", func() (TransactionsDTO, error) {
		row := transactionRowByTitle(dto, "Bakery")
		return fixture.service.ToggleSelect(row.ID)
	}, func(dto TransactionsDTO) bool {
		return dto.SelectionVisible
	})
	dto = fixture.action(t, "the batch category", func() (TransactionsDTO, error) {
		return fixture.service.BatchCategory(1)
	}, func(dto TransactionsDTO) bool {
		row := transactionRowByTitle(dto, "Bakery")
		return row != nil && row.CategoryText == "Food"
	})
	if dto.MessageText == "" {
		t.Fatalf("batch category left no message (message %q, rows %+v)", dto.MessageText, dto.Rows)
	}
	dto = fixture.action(t, "the batch tax", func() (TransactionsDTO, error) {
		return fixture.service.BatchTax(1)
	}, func(dto TransactionsDTO) bool {
		return strings.Contains(dto.MessageText, i18n.Format(i18n.En, i18n.MessageTaxOverrideSet, i18n.Int(1)))
	})
	if dto.MessageText == "" {
		t.Fatal("batch tax left no message")
	}
}

func TestTransactionsRowsPutPendingLinesFirst(t *testing.T) {
	state := transactionsview.NewState(i18n.En)
	state.Transactions = []model.TransactionItem{
		transactionItem(2, mustBindDate(t, 2026, time.November, 6), -100, false),
		transactionItem(1, mustBindDate(t, 2026, time.November, 5), 200, false),
	}
	state.PendingBookings = []model.PendingBookingItem{
		pendingBookingItem(mustBindDate(t, 2026, time.November, 7), -50),
	}

	rows := transactionsTransactionRows(*state, i18n.En)

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if !rows[0].Pending {
		t.Fatal("the first row is not pending")
	}
	if rows[0].Title != "Pending partner" {
		t.Fatalf("pending title = %q", rows[0].Title)
	}
	if rows[0].AmountText != "-€0.50" {
		t.Fatalf("pending amount = %q", rows[0].AmountText)
	}
	if rows[1].Title != "Partner 2" || rows[2].Title != "Partner 1" {
		t.Fatalf("row titles = %q, %q", rows[1].Title, rows[2].Title)
	}
}

func TestTransactionsBoldRowsComeFromBoldIDs(t *testing.T) {
	state := transactionsview.NewState(i18n.En)
	state.Transactions = []model.TransactionItem{
		transactionItem(1, mustBindDate(t, 2026, time.November, 5), -100, false),
		transactionItem(2, mustBindDate(t, 2026, time.November, 4), -100, true),
	}
	state.BoldIDs[model.TransactionID(1)] = struct{}{}

	rows := transactionsTransactionRows(*state, i18n.En)

	if !rows[0].IsNew {
		t.Fatal("the state's bold set did not mark the row")
	}
	if rows[1].IsNew {
		t.Fatal("the row's own flag was used for display")
	}
}

func TestTransactionsPerCurrencyTotalsKeepThePrimaryFirstOrder(t *testing.T) {
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("USD: %v", err)
	}
	rows := transactionsAmountRows([]money.CurrencyTotal{
		{Currency: money.EUR, Total: 1_000},
		{Currency: usd, Total: 500},
	}, "positive", i18n.En)

	if len(rows) != 2 || rows[0].Text != "€10.00" || rows[1].Text != "$5.00" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestTransactionsCategoryAndTaxIndexesMapToTheCoreValues(t *testing.T) {
	state := transactionsview.NewState(i18n.En)
	state.Categories = []model.Category{{
		ID:            model.CategoryID(7),
		Name:          "Food",
		Color:         "#FF0000",
		IsTaxRelevant: true,
	}}

	if got := transactionsCategoryFilterIndex(*state); got != 0 {
		t.Fatalf("category index = %d, want 0", got)
	}
	if got := transactionsCategoryNames(*state, i18n.En)[1]; got != "Food (tax)" {
		t.Fatalf("category name = %q", got)
	}
	if got := transactionsAddCategoryNames(*state, i18n.En)[0]; got != "Auto (rules)" {
		t.Fatalf("add category = %q", got)
	}
	state.CategoryFilter = transactionsview.OnlyCategoryFilter(7)
	if got := transactionsCategoryFilterIndex(*state); got != 2 {
		t.Fatalf("only-category index = %d, want 2", got)
	}
}

func TestTransactionsPendingRowsHaveNoBalanceOrSelection(t *testing.T) {
	state := transactionsview.NewState(i18n.En)
	state.PendingBookings = []model.PendingBookingItem{
		pendingBookingItem(mustBindDate(t, 2026, time.November, 7), -50),
	}
	state.Balances = map[model.TransactionID]balance.Entry{}
	state.SelectedIDs[model.TransactionID(1)] = struct{}{}

	rows := transactionsTransactionRows(*state, i18n.En)

	if !rows[0].Pending || rows[0].BalanceText != "" || rows[0].Selected || rows[0].BalanceWarning {
		t.Fatalf("pending row = %+v", rows[0])
	}
}

func TestTransactionsChartPathUsesTheStatePoints(t *testing.T) {
	points := []balance.BalancePoint{
		{Date: mustBindDate(t, 2026, time.November, 1), Balance: money.Money{Minor: 100, Currency: money.EUR}},
		{Date: mustBindDate(t, 2026, time.November, 2), Balance: money.Money{Minor: 300, Currency: money.EUR}},
	}
	current := money.Money{Minor: 300, Currency: money.EUR}
	chart, ok := transactionsChartData(points, &current, i18n.En)
	if !ok {
		t.Fatal("chart_data refused two points")
	}
	if !strings.HasPrefix(chart.Line, "M 0.00") || !strings.Contains(chart.Line, "L 700.00") {
		t.Fatalf("line = %q", chart.Line)
	}
	if !chart.NotFromZero || chart.Value != "€3.00" {
		t.Fatalf("chart = %+v", chart)
	}
	if chart.AxisStart != "01.11.2026" || chart.AxisEnd != "02.11.2026" {
		t.Fatalf("axis = %q / %q", chart.AxisStart, chart.AxisEnd)
	}
	if _, ok := transactionsChartData(points[:1], &current, i18n.En); ok {
		t.Fatal("one point produced a chart")
	}
}

func TestTransactionsReapplyGroupRowsKeepTheTickState(t *testing.T) {
	preview := &transactionsview.ReapplyPreview{
		Changes: []transactionsview.ReapplyChange{{
			TransactionID: model.TransactionID(1),
			ToCategoryID:  model.CategoryID(7),
		}},
		Groups: []transactionsview.ReapplyGroup{{
			FromName:     "Uncategorized",
			ToCategoryID: model.CategoryID(7),
			ToName:       "Food",
			Count:        2,
			Sum:          &money.Money{Minor: -4_200, Currency: money.EUR},
			Currency:     money.EUR,
		}},
		ShownCount: 3,
	}
	from := model.CategoryID(7)
	excluded := transactionsExcludedSet([]shell.TransactionsReapplyKey{{
		ToCategoryID: from,
	}})
	rows := transactionsReapplyGroupRows(preview, excluded, i18n.En)

	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Title != "Uncategorized → Food" || rows[0].Detail != "2 · -€42.00" {
		t.Fatalf("row = %+v", rows[0])
	}
	if rows[0].Ticked || rows[0].FromID != -1 || rows[0].ToID != 7 {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestTransactionsOneChangePublishesOneEventEqualToTheReturnedDTO(t *testing.T) {
	fixture := newTransactionsFixture(t)
	fixture.load(t)
	fixture.drain()

	dto, err := fixture.service.PanelFilterChanged("giro")
	if err != nil {
		t.Fatalf("PanelFilterChanged: %v", err)
	}
	event := fixture.wait(t, "the panel filter event", func(dto TransactionsDTO) bool {
		return dto.PanelFilterText == "giro"
	})
	if !reflect.DeepEqual(event, dto) {
		t.Fatalf("event = %+v, want the returned DTO %+v", event, dto)
	}
	select {
	case extra := <-fixture.events:
		t.Fatalf("a second event was published: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTransactionsDTOAndErrorsNeverShowAFullIBAN(t *testing.T) {
	fixture := newTransactionsFixture(t)
	dto := fixture.load(t)
	if _, err := fixture.service.OpenDetail(dto.Rows[0].ID); err != nil {
		t.Fatalf("OpenDetail: %v", err)
	}
	dto = fixture.wait(t, "the detail", func(dto TransactionsDTO) bool {
		return dto.DetailOpen
	})
	serialized, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	full := "DE89370400440532013000"
	grouped := format.IBAN(full)
	if strings.Contains(string(serialized), full) || strings.Contains(string(serialized), grouped) {
		t.Fatalf("a DTO field contains the full IBAN: %s", serialized)
	}
	if strings.Contains(dto.ErrorText, full) || strings.Contains(dto.ErrorText, grouped) {
		t.Fatalf("an error string contains the full IBAN: %q", dto.ErrorText)
	}
}

func transactionItem(id int64, date civil.Date, minor int64, isNew bool) model.TransactionItem {
	partner := fmt.Sprintf("Partner %d", id)
	return model.TransactionItem{
		ID:          model.TransactionID(id),
		AccountID:   model.AccountID(1),
		Date:        date,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		Currency:    money.EUR,
		PartnerName: &partner,
		IsNew:       isNew,
	}
}

func pendingBookingItem(date civil.Date, minor int64) model.PendingBookingItem {
	return model.PendingBookingItem{
		AccountID:   model.AccountID(1),
		Date:        date,
		Amount:      money.Money{Minor: minor, Currency: money.EUR},
		PartnerName: stringValue("Pending partner"),
		Purpose:     stringValue("Pending purpose"),
		FetchedAt:   time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
	}
}

func mustBindDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}
