package bind

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/dashboardview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

type dashboardFixture struct {
	paths     paths.AppPaths
	session   *shell.Session
	shell     *Shell
	dashboard *Dashboard
	router    *Router
	wantView  *dashboardview.DashboardView

	mu          sync.Mutex
	shellEvents []ShellDTO
	dashEvents  chan DashboardDTO
}

func newDashboardFixture(t *testing.T) *dashboardFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	want := seedBindDashboard(t, appPaths)

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

	fixture := &dashboardFixture{
		paths:      appPaths,
		session:    session,
		wantView:   &want,
		dashEvents: make(chan DashboardDTO, 256),
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
	fixture.dashboard = NewDashboard(session, DashboardOptions{
		Emit: func(dto DashboardDTO) {
			fixture.dashEvents <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("dashboard", fixture.dashboard)
	fixture.router.RegisterShell(fixture.shell)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.dashboard.ServiceShutdown()
		_ = fixture.shell.ServiceShutdown()
	})
	return fixture
}

func stringValue(value string) *string {
	return &value
}

func bindSeedDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func seedBindDashboard(t *testing.T, appPaths paths.AppPaths) dashboardview.DashboardView {
	t.Helper()
	ctx := context.Background()
	database, err := unlock.Create(
		appPaths,
		"default",
		[]byte(testPassphrase),
		[]byte(testPassphrase),
		true,
	)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}

	account, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringValue("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	if _, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringValue("DE44500105175407324931"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeSavings,
		Balance:     money.Money{Minor: 50_000, Currency: money.EUR},
	}); err != nil {
		t.Fatalf("accounts.Create savings: %v", err)
	}
	budget := int64(10_000)
	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:        "Food",
		Color:       "#FF5722",
		BudgetLimit: &money.Money{Minor: budget, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, category *model.CategoryID) {
		name := partner
		hash := fmt.Sprintf("%d-%s-%d-%s", account, day, minor, partner)
		if _, err := transactions.InsertPlanned(ctx, database, account, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &name,
			CategoryID:  category,
			ImportHash:  hash,
			CreatedAt:   testNow.UTC(),
		}}); err != nil {
			t.Fatalf("transactions.InsertPlanned(%s): %v", partner, err)
		}
	}

	insert(bindSeedDate(t, 2026, time.March, 5), -10_000, "March donation", nil)
	insert(bindSeedDate(t, 2026, time.September, 4), -6_000, "September food", &food)
	insert(bindSeedDate(t, 2026, time.September, 6), 150_000, "Employer", nil)
	insert(bindSeedDate(t, 2026, time.October, 2), -4_000, "October food", &food)
	insert(bindSeedDate(t, 2026, time.October, 3), 200_000, "Employer", nil)
	insert(bindSeedDate(t, 2026, time.October, 5), -1_500, "October uncategorized", nil)

	today := civil.FromTime(testNow)
	figures, err := dashboard.LoadDashboard(ctx, database, today, dashboard.NewYearMonth(2026, 10))
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	view := dashboardview.FromFigures(figures, 0, i18n.En)
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return view
}

func (f *dashboardFixture) unlock(t *testing.T) {
	t.Helper()
	if _, err := f.shell.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func (f *dashboardFixture) waitForDashboard(
	t *testing.T,
	description string,
	predicate func(DashboardDTO) bool,
) DashboardDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.dashEvents:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func (f *dashboardFixture) lastShellEvent() (ShellDTO, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.shellEvents) == 0 {
		return ShellDTO{}, false
	}
	return f.shellEvents[len(f.shellEvents)-1], true
}

func TestDashboardServiceLoadsTheFiguresAndMatchesFromFigures(t *testing.T) {
	fixture := newDashboardFixture(t)
	fixture.unlock(t)

	loading := fixture.waitForDashboard(t, "the loading state", func(dto DashboardDTO) bool {
		return dto.Loading
	})
	if loading.ShowContent {
		t.Fatal("the loading state shows content")
	}

	dto := fixture.waitForDashboard(t, "the loaded figures", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.ShowContent
	})
	if dto.FirstRun {
		t.Fatal("a seeded profile is the first run")
	}
	if dto.ErrorText != "" {
		t.Fatalf("error text = %q", dto.ErrorText)
	}
	assertDashboardDTOMatchesView(t, dto, *fixture.wantView)

	state, err := fixture.dashboard.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(state, dto) {
		t.Fatalf("State = %+v, want the published DTO %+v", state, dto)
	}
}

func TestDashboardEventsCarryTheMethodResults(t *testing.T) {
	fixture := newDashboardFixture(t)
	fixture.unlock(t)
	fixture.waitForDashboard(t, "the loaded figures", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.ShowContent
	})

	for len(fixture.dashEvents) > 0 {
		<-fixture.dashEvents
	}

	dismissed, err := fixture.dashboard.DismissError()
	if err != nil {
		t.Fatalf("DismissError: %v", err)
	}
	event := fixture.waitForDashboard(t, "the dismissed error event", func(DashboardDTO) bool { return true })
	if !reflect.DeepEqual(event, dismissed) {
		t.Fatalf("dashboard event = %+v, want the method result %+v", event, dismissed)
	}

	shellBefore, err := fixture.shell.State()
	if err != nil {
		t.Fatalf("shell State: %v", err)
	}
	if _, err := fixture.dashboard.OpenAccounts(); err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	shellAfter, err := fixture.shell.State()
	if err != nil {
		t.Fatalf("shell State after link: %v", err)
	}
	if shellAfter.Screen != "accounts" {
		t.Fatalf("screen = %q, want accounts", shellAfter.Screen)
	}
	if reflect.DeepEqual(shellAfter, shellBefore) {
		t.Fatal("the link did not change the shell")
	}
	eventShell, ok := fixture.lastShellEvent()
	if !ok {
		t.Fatal("the link emitted no shell:changed event")
	}
	if !reflect.DeepEqual(eventShell, shellAfter) {
		t.Fatalf("shell event = %+v, want the method result %+v", eventShell, shellAfter)
	}
}

func TestDashboardMonthMethodsPublishAndSelectRefusesUnknownIndexes(t *testing.T) {
	fixture := newDashboardFixture(t)
	fixture.unlock(t)
	fixture.waitForDashboard(t, "the loaded figures", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.ShowContent
	})

	previous, err := fixture.dashboard.PreviousMonth()
	if err != nil {
		t.Fatalf("PreviousMonth: %v", err)
	}
	if !previous.Loading {
		t.Fatal("PreviousMonth did not publish the loading state")
	}
	event := fixture.waitForDashboard(t, "the previous month event", func(DashboardDTO) bool { return true })
	if !reflect.DeepEqual(event, previous) {
		t.Fatalf("dashboard event = %+v, want the method result %+v", event, previous)
	}
	fixture.waitForDashboard(t, "September", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.MonthSelected != 0
	})
	state, err := fixture.dashboard.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.MonthOptions[state.MonthSelected] != dashboardview.MonthText(dashboard.NewYearMonth(2026, 9), i18n.En) {
		t.Fatalf("selected month = %q", state.MonthOptions[state.MonthSelected])
	}

	if _, err := fixture.dashboard.NextMonth(); err != nil {
		t.Fatalf("NextMonth: %v", err)
	}
	fixture.waitForDashboard(t, "October", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.MonthOptions[dto.MonthSelected] == dashboardview.MonthText(dashboard.NewYearMonth(2026, 10), i18n.En)
	})

	if _, err := fixture.dashboard.CurrentMonth(); err != nil {
		t.Fatalf("CurrentMonth: %v", err)
	}
	fixture.waitForDashboard(t, "the current month", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.MonthSelected == 0
	})

	before, err := fixture.dashboard.State()
	if err != nil {
		t.Fatalf("State before refusal: %v", err)
	}
	for len(fixture.dashEvents) > 0 {
		<-fixture.dashEvents
	}
	refused, err := fixture.dashboard.SelectMonth(len(before.MonthOptions))
	if err == nil {
		t.Fatal("an out-of-range month index was accepted")
	}
	if !reflect.DeepEqual(refused, before) {
		t.Fatalf("a refused selection changed the DTO: %+v, want %+v", refused, before)
	}
	select {
	case event := <-fixture.dashEvents:
		t.Fatalf("a refused selection emitted %+v", event)
	default:
	}

	accepted, err := fixture.dashboard.SelectMonth(1)
	if err != nil {
		t.Fatalf("SelectMonth(1): %v", err)
	}
	if !accepted.Loading {
		t.Fatal("SelectMonth did not publish the loading state")
	}
	fixture.waitForDashboard(t, "the selected month", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.MonthSelected == 1
	})
}

func TestDashboardStateIsSafeWhileTheServiceLoads(t *testing.T) {
	fixture := newDashboardFixture(t)
	fixture.unlock(t)
	fixture.waitForDashboard(t, "the loaded figures", func(dto DashboardDTO) bool {
		return !dto.Loading && dto.ShowContent
	})

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
				_, _ = fixture.dashboard.State()
			}
		}
	}()

	for index := 0; index < 3; index++ {
		if _, err := fixture.dashboard.PreviousMonth(); err != nil {
			t.Fatalf("PreviousMonth: %v", err)
		}
		fixture.waitForDashboard(t, "a load while reading the DTO", func(dto DashboardDTO) bool {
			return !dto.Loading
		})
		if _, err := fixture.dashboard.NextMonth(); err != nil {
			t.Fatalf("NextMonth: %v", err)
		}
		fixture.waitForDashboard(t, "a load while reading the DTO", func(dto DashboardDTO) bool {
			return !dto.Loading
		})
	}

	close(stop)
	wait.Wait()
}

func assertDashboardDTOMatchesView(t *testing.T, dto DashboardDTO, view dashboardview.DashboardView) {
	t.Helper()
	if dto.MonthLabel != view.MonthLabel {
		t.Fatalf("month label = %q, want %q", dto.MonthLabel, view.MonthLabel)
	}
	if dto.PreviousEnabled != view.PreviousEnabled || dto.NextEnabled != view.NextEnabled || dto.IsCurrentMonth != view.IsCurrentMonth {
		t.Fatalf("month controls = %+v", dto)
	}
	if dto.AvailableTitle != view.AvailableTitle || dto.AvailableValue != view.AvailableValue || dto.AvailableSubtitle != view.AvailableSubtitle {
		t.Fatalf("available card = %q / %q / %q", dto.AvailableTitle, dto.AvailableValue, dto.AvailableSubtitle)
	}
	if dto.AvailableTone != toneName(view.AvailableTone) {
		t.Fatalf("available tone = %q, want %q", dto.AvailableTone, toneName(view.AvailableTone))
	}
	if len(dto.StatementLines) != len(view.StatementLines) {
		t.Fatalf("statement lines = %d, want %d", len(dto.StatementLines), len(view.StatementLines))
	}
	for index, line := range view.StatementLines {
		want := DashboardStatementLineDTO{Label: line.Label, AmountText: line.AmountText, Kind: kindName(line.Kind)}
		if dto.StatementLines[index] != want {
			t.Fatalf("statement line %d = %+v, want %+v", index, dto.StatementLines[index], want)
		}
	}
	if dto.IncomeTitle != view.IncomeTitle || dto.IncomeValue != view.IncomeValue ||
		dto.ExpensesTitle != view.ExpensesTitle || dto.ExpensesValue != view.ExpensesValue {
		t.Fatalf("income/expenses cards = %+v", dto)
	}
	if dto.PendingTitle != view.PendingTitle || dto.PendingValue != view.PendingValue ||
		dto.PendingSubtitle != view.PendingSubtitle || !reflect.DeepEqual(dto.PendingLines, view.PendingLines) ||
		dto.PendingEnabled != view.PendingEnabled {
		t.Fatalf("pending card = %+v", dto)
	}
	if dto.ExpensesSectionTitle != view.ExpensesSectionTitle || dto.ExpensesEmptyText != view.ExpensesEmptyText {
		t.Fatalf("expenses card = %q / %q", dto.ExpensesSectionTitle, dto.ExpensesEmptyText)
	}
	assertCategoryRows(t, "expense", dto.ExpenseRows, view.ExpenseRows)
	transfers := ""
	if view.TransfersNote != nil {
		transfers = *view.TransfersNote
	}
	if dto.TransfersNote != transfers {
		t.Fatalf("transfers note = %q, want %q", dto.TransfersNote, transfers)
	}
	chartEmpty := ""
	if view.ChartEmptyText != nil {
		chartEmpty = *view.ChartEmptyText
	}
	if dto.ChartTitle != view.ChartTitle || dto.ChartEmptyText != chartEmpty ||
		dto.ChartAxisTop != view.ChartAxisTop || dto.ChartAxisMid != view.ChartAxisMid ||
		dto.ChartAxisZero != view.ChartAxisZero || dto.IncomeLegend != view.IncomeLegend ||
		dto.ExpensesLegend != view.ExpensesLegend || dto.PartialNote != view.PartialMonthNote {
		t.Fatalf("chart = %+v", dto)
	}
	if len(dto.MonthBars) != len(view.MonthBars) {
		t.Fatalf("month bars = %d, want %d", len(dto.MonthBars), len(view.MonthBars))
	}
	for index, bar := range view.MonthBars {
		want := DashboardMonthBarDTO{
			Label: bar.Label, Title: bar.Title, IncomeText: bar.IncomeText, ExpensesText: bar.ExpensesText,
			IncomeFraction: bar.IncomeFraction, ExpensesFraction: bar.ExpensesFraction, Partial: bar.Partial,
		}
		if dto.MonthBars[index] != want {
			t.Fatalf("month bar %d = %+v, want %+v", index, dto.MonthBars[index], want)
		}
	}
	if dto.BudgetsTitle != view.BudgetsTitle || dto.HasBudgets != (len(view.Budgets) > 0) ||
		dto.MonthProgress != view.MonthProgress {
		t.Fatalf("budgets header = %+v", dto)
	}
	if len(dto.BudgetRows) != len(view.Budgets) {
		t.Fatalf("budget rows = %d, want %d", len(dto.BudgetRows), len(view.Budgets))
	}
	for index, row := range view.Budgets {
		want := DashboardBudgetRowDTO{
			Name: row.Name, AmountText: row.AmountText, RestText: row.RestText,
			Ratio: row.Ratio, Over: row.IsOver, Warning: row.Warning, Colour: row.Color,
		}
		if dto.BudgetRows[index] != want {
			t.Fatalf("budget row %d = %+v, want %+v", index, dto.BudgetRows[index], want)
		}
	}
	if dto.TaxTitle != view.TaxTitle || dto.TaxHint != view.TaxHint || dto.TaxValue != view.TaxValue {
		t.Fatalf("tax card = %q / %q / %q", dto.TaxTitle, dto.TaxHint, dto.TaxValue)
	}
	assertCategoryRows(t, "tax", dto.TaxRows, view.TaxRows)
	if dto.UpcomingTitle != view.UpcomingTitle || dto.HasUpcoming != (len(view.Upcoming) > 0) {
		t.Fatalf("upcoming header = %+v", dto)
	}
	if len(dto.UpcomingRows) != len(view.Upcoming) {
		t.Fatalf("upcoming rows = %d, want %d", len(dto.UpcomingRows), len(view.Upcoming))
	}
	for index, row := range view.Upcoming {
		want := DashboardUpcomingRowDTO{
			DateText: row.DateText, Title: row.Name, Meta: row.Meta,
			AmountText: row.AmountText, Expense: !row.IsIncome,
		}
		if dto.UpcomingRows[index] != want {
			t.Fatalf("upcoming row %d = %+v, want %+v", index, dto.UpcomingRows[index], want)
		}
	}
	if dto.RecentTitle != view.RecentTitle || dto.RecentEmptyText != view.RecentEmptyText ||
		dto.ViewAllLabel != view.ViewAllLabel {
		t.Fatalf("recent header = %+v", dto)
	}
	if len(dto.RecentRows) != len(view.RecentTransactions) {
		t.Fatalf("recent rows = %d, want %d", len(dto.RecentRows), len(view.RecentTransactions))
	}
	for index, row := range view.RecentTransactions {
		partner := ""
		if row.Partner != nil {
			partner = *row.Partner
		}
		want := DashboardTransactionRowDTO{
			DateText: row.DateText, Title: row.Title, Meta: row.Meta,
			AmountText: row.AmountText, Expense: row.IsExpense, Partner: partner,
		}
		if dto.RecentRows[index] != want {
			t.Fatalf("recent row %d = %+v, want %+v", index, dto.RecentRows[index], want)
		}
	}
	if len(dto.MonthOptions) == 0 || dto.MonthSelected < 0 || dto.MonthSelected >= len(dto.MonthOptions) {
		t.Fatalf("month options = %+v, selected %d", dto.MonthOptions, dto.MonthSelected)
	}
	if got := dto.MonthOptions[dto.MonthSelected]; got != dashboardview.MonthText(view.SelectedMonth, i18n.En) {
		t.Fatalf("selected month text = %q", got)
	}
}

func assertCategoryRows(t *testing.T, name string, got []DashboardCategoryRowDTO, want []dashboardview.CategoryRowView) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s rows = %d, want %d", name, len(got), len(want))
	}
	for index, row := range want {
		categoryID := -1
		if row.CategoryID != nil {
			categoryID = int(*row.CategoryID)
		}
		expected := DashboardCategoryRowDTO{
			Label: row.Label, PercentText: row.PercentText, AmountText: row.AmountText,
			Colour: row.Color, CategoryID: categoryID, IsMore: row.IsMore, Fraction: row.Fraction,
		}
		if got[index] != expected {
			t.Fatalf("%s row %d = %+v, want %+v", name, index, got[index], expected)
		}
	}
}

func toneName(tone dashboardview.BalanceTone) string {
	switch tone {
	case dashboardview.BalanceToneZero:
		return "zero"
	case dashboardview.BalanceToneNegative:
		return "negative"
	default:
		return "positive"
	}
}

func kindName(kind dashboardview.StatementLineKind) string {
	switch kind {
	case dashboardview.StatementLineKindCarry:
		return "carry"
	case dashboardview.StatementLineKindIn:
		return "in"
	case dashboardview.StatementLineKindOut:
		return "out"
	case dashboardview.StatementLineKindMovement:
		return "movement"
	default:
		return "result"
	}
}
