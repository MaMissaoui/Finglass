package dashboardview

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

var ctx = context.Background()

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	passphrase, err := secret.NewPassphrase(
		[]byte("correct horse battery staple"),
		secret.New,
	)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), passphrase)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func moneyOf(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func createAccount(
	t *testing.T,
	database *db.Database,
	iban string,
	balance int64,
	currency money.Currency,
	accountType model.AccountType,
) model.AccountID {
	t.Helper()
	id, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        &iban,
		Currency:    currency,
		AccountType: accountType,
		Balance:     money.Money{Minor: balance, Currency: currency},
	})
	if err != nil {
		t.Fatalf("accounts.Create(%s): %v", iban, err)
	}
	return id
}

func createCategory(
	t *testing.T,
	database *db.Database,
	name string,
	color string,
	taxRelevant bool,
	isTransfer bool,
	budget *int64,
) model.CategoryID {
	t.Helper()
	var limit *money.Money
	if budget != nil {
		value := moneyOf(*budget)
		limit = &value
	}
	id, err := categories.Create(ctx, database, model.NewCategory{
		Name:          name,
		Color:         color,
		BudgetLimit:   limit,
		IsTaxRelevant: taxRelevant,
		IsTransfer:    isTransfer,
	})
	if err != nil {
		t.Fatalf("categories.Create(%s): %v", name, err)
	}
	return id
}

func insertTransaction(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	currency money.Currency,
	day civil.Date,
	minor int64,
	partner string,
	categoryID *model.CategoryID,
) model.TransactionID {
	t.Helper()
	partnerName := partner
	hash := fmt.Sprintf("%d-%s-%d-%s", accountID, day, minor, partner)
	_, err := transactions.InsertPlanned(ctx, database, accountID, []model.NewTransaction{{
		BookingDate:  day,
		Amount:       money.Money{Minor: minor, Currency: currency},
		BalanceAfter: nil,
		PartnerName:  &partnerName,
		Purpose:      nil,
		CategoryID:   categoryID,
		ImportHash:   hash,
		CreatedAt:    time.Now().UTC(),
		IsNew:        false,
		TaxOverride:  nil,
	}})
	if err != nil {
		t.Fatalf("transactions.InsertPlanned: %v", err)
	}
	listed, err := transactions.ListForAccount(ctx, database, accountID, nil, nil)
	if err != nil {
		t.Fatalf("transactions.ListForAccount: %v", err)
	}
	for _, transaction := range listed {
		if transaction.ImportHash == hash {
			return transaction.ID
		}
	}
	t.Fatalf("inserted transaction %q not found", hash)
	return 0
}

func insertTransactionWithoutPartner(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	currency money.Currency,
	day civil.Date,
	minor int64,
	categoryID *model.CategoryID,
) model.TransactionID {
	t.Helper()
	hash := fmt.Sprintf("%d-%s-%d-none", accountID, day, minor)
	_, err := transactions.InsertPlanned(ctx, database, accountID, []model.NewTransaction{{
		BookingDate:  day,
		Amount:       money.Money{Minor: minor, Currency: currency},
		BalanceAfter: nil,
		PartnerName:  nil,
		Purpose:      nil,
		CategoryID:   categoryID,
		ImportHash:   hash,
		CreatedAt:    time.Now().UTC(),
		IsNew:        false,
		TaxOverride:  nil,
	}})
	if err != nil {
		t.Fatalf("transactions.InsertPlanned: %v", err)
	}
	listed, err := transactions.ListForAccount(ctx, database, accountID, nil, nil)
	if err != nil {
		t.Fatalf("transactions.ListForAccount: %v", err)
	}
	for _, transaction := range listed {
		if transaction.ImportHash == hash {
			return transaction.ID
		}
	}
	t.Fatalf("inserted transaction %q not found", hash)
	return 0
}

func seedMonths(t *testing.T, database *db.Database) (
	model.AccountID,
	model.CategoryID,
	model.CategoryID,
	model.CategoryID,
) {
	t.Helper()
	account := createAccount(
		t,
		database,
		"DE89370400440532013000",
		100_000,
		money.EUR,
		model.AccountTypeChecking,
	)
	foodBudget := int64(10_000)
	food := createCategory(t, database, "Food", "#FF5722", false, false, &foodBudget)
	donations := createCategory(t, database, "Donations", "#3BC7B0", true, false, nil)
	transfers := createCategory(t, database, "Own transfers", "#4CAF50", false, true, nil)

	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.March, 5), -10_000, "March donation", &donations)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.August, 2), -12_000, "August food", &food)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.August, 3), 200_000, "Employer", nil)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.August, 4), -20_000, "Own transfer", &transfers)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.August, 5), -1_500, "August uncategorized", nil)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.September, 4), -6_000, "September food", &food)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.September, 6), -5_000, "September donation", &donations)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.October, 2), -4_000, "October food", &food)
	insertTransaction(t, database, account, money.EUR, date(t, 2026, time.October, 3), 150_000, "Employer", nil)

	return account, food, donations, transfers
}

func navigationMonthFixture(t *testing.T) (*db.Database, model.AccountID, model.CategoryID) {
	t.Helper()
	database := testDatabase(t)
	accountID := createAccount(
		t,
		database,
		"DE89370400440532013000",
		100_000,
		money.EUR,
		model.AccountTypeChecking,
	)
	food := createCategory(t, database, "Food", "#FF5722", false, false, nil)
	tax := createCategory(t, database, "Donations", "#3BC7B0", true, false, nil)
	transfers := createCategory(t, database, "Own transfers", "#4CAF50", false, true, nil)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.March, 5), -10_000, "MaerzJetzt", &tax)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.August, 2), -12_000, "August Nazar", &food)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.August, 3), 200_000, "Employer", nil)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.August, 4), -20_000, "Erika Musterfrau", &transfers)
	insertTransactionWithoutPartner(t, database, accountID, money.EUR, date(t, 2026, time.August, 5), -1_500, nil)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.September, 4), -6_000, "September Nazar", &food)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.September, 6), -5_000, "September Spenden", &tax)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.October, 2), -4_000, "Oktober Nazar", &food)
	insertTransaction(t, database, accountID, money.EUR, date(t, 2026, time.October, 3), 150_000, "Employer", nil)
	return database, accountID, food
}

func TestNothingLoadedShowsNoFigures(t *testing.T) {
	database := testDatabase(t)
	state := New(i18n.En, date(t, 2026, time.October, 15))

	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.LoadedMonth != nil {
		t.Fatalf("loaded month = %+v, want nil", state.LoadedMonth)
	}
	if state.IsLoading {
		t.Fatal("state is loading")
	}
	if state.View != nil {
		t.Fatal("view is set")
	}
	if state.HasFigures() {
		t.Fatal("has figures before the first load")
	}

	if err := state.Load(ctx, database, date(t, 2026, time.October, 15)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}
	if state.View == nil {
		t.Fatal("view is nil after a load")
	}
	if state.HasFigures() {
		t.Fatal("an account-less profile shows the getting-started card instead")
	}
}

func TestFirstRunIsDetectedAfterALoadWithoutAccounts(t *testing.T) {
	database := testDatabase(t)
	state := New(i18n.En, date(t, 2026, time.October, 15))
	if state.IsFirstRun() {
		t.Fatal("before the first load the page is busy")
	}

	if err := state.Load(ctx, database, date(t, 2026, time.October, 15)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !state.IsFirstRun() {
		t.Fatal("first run is not detected")
	}
	if state.HasFigures() {
		t.Fatal("has figures on the first-run card")
	}
	if state.HasAccounts {
		t.Fatal("has accounts without any")
	}
}

func TestMonthNavigationAndClampingFollowFindesk(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)

	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.EarliestMonth != dashboard.NewYearMonth(2026, 3) {
		t.Fatalf("earliest month = %+v", state.EarliestMonth)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}
	if !view.PreviousEnabled {
		t.Fatal("previous is disabled")
	}
	if view.NextEnabled {
		t.Fatal("next is enabled")
	}
	if view.MonthLabel != "October 2026" {
		t.Fatalf("month label = %q", view.MonthLabel)
	}

	if err := state.PreviousMonth(ctx, database, today); err != nil {
		t.Fatalf("PreviousMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}
	if state.View == nil || !state.View.NextEnabled {
		t.Fatal("next is disabled in a past month")
	}

	for index := 0; index < 6; index++ {
		if err := state.PreviousMonth(ctx, database, today); err != nil {
			t.Fatalf("PreviousMonth: %v", err)
		}
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 3) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if err := state.PreviousMonth(ctx, database, today); err != nil {
		t.Fatalf("PreviousMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 3) {
		t.Fatalf("the oldest booked month is the floor, got %+v", state.SelectedMonth)
	}

	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2026, 12)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("a future month clamps to the current month, got %+v", state.SelectedMonth)
	}

	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2025, 1)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 3) {
		t.Fatalf("a month before the first booking clamps to it, got %+v", state.SelectedMonth)
	}

	if err := state.SelectCurrentMonth(ctx, database, today); err != nil {
		t.Fatalf("SelectCurrentMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.View == nil || !state.View.IsCurrentMonth {
		t.Fatal("the view is not in the current month")
	}
}

func TestOpeningAgainAfterAMonthChangeShowsTodaysMonth(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	october := date(t, 2026, time.October, 15)
	state := New(i18n.En, october)
	if err := state.Load(ctx, database, october); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}

	november := date(t, 2026, time.November, 8)
	state.Open(november)
	if err := state.Load(ctx, database, november); err != nil {
		t.Fatalf("Load: %v", err)
	}

	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.CurrentMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("current month = %+v", state.CurrentMonth)
	}
	if view.SelectedMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("view selected month = %+v", view.SelectedMonth)
	}
	if view.CurrentMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("view current month = %+v", view.CurrentMonth)
	}
	if view.MonthLabel != "November 2026" {
		t.Fatalf("month label = %q", view.MonthLabel)
	}
	if !view.IsCurrentMonth {
		t.Fatal("view is not in the current month")
	}
	if view.NextEnabled {
		t.Fatal("next is enabled in today's month")
	}
}

func TestAReloadWhileOpenKeepsTheSelectedPastMonth(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	october := date(t, 2026, time.October, 15)
	state := New(i18n.En, october)
	if err := state.Load(ctx, database, october); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := state.SelectMonth(ctx, database, october, dashboard.NewYearMonth(2026, 9)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}

	november := date(t, 2026, time.November, 8)
	if err := state.Load(ctx, database, november); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if state.SelectedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
	if state.LoadedMonth == nil || *state.LoadedMonth != dashboard.NewYearMonth(2026, 9) {
		t.Fatalf("loaded month = %+v", state.LoadedMonth)
	}
	if state.CurrentMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("current month = %+v", state.CurrentMonth)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}
	if view.MonthLabel != "September 2026" {
		t.Fatalf("month label = %q", view.MonthLabel)
	}
	if !view.NextEnabled {
		t.Fatal("a past month can move towards today")
	}
}

func TestTheMonthListRunsFromTodayBackToTheEarliestBooking(t *testing.T) {
	earliest := dashboard.NewYearMonth(2026, 8)
	current := dashboard.NewYearMonth(2026, 11)
	got := dashboard.MonthOptions(earliest, current)
	want := []dashboard.YearMonth{
		dashboard.NewYearMonth(2026, 11),
		dashboard.NewYearMonth(2026, 10),
		dashboard.NewYearMonth(2026, 9),
		dashboard.NewYearMonth(2026, 8),
	}
	if !equalMonths(got, want) {
		t.Fatalf("month options = %+v, want %+v (newest first)", got, want)
	}
	got = dashboard.MonthOptions(current, earliest)
	want = []dashboard.YearMonth{dashboard.NewYearMonth(2026, 8)}
	if !equalMonths(got, want) {
		t.Fatalf("crossed bounds clamp to the current month: got %+v, want %+v", got, want)
	}
}

func equalMonths(left []dashboard.YearMonth, right []dashboard.YearMonth) bool {
	return reflect.DeepEqual(left, right)
}

func TestSelectingBeforeTheEarliestOrAfterTheCurrentMonthIsClamped(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)
	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2026, 1)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 3) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}

	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2027, 1)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("selected month = %+v", state.SelectedMonth)
	}
}

func TestBeginLoadRefreshesAStaleCurrentMonthBeforeClamping(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	october := date(t, 2026, time.October, 15)
	state := New(i18n.En, october)
	if err := state.Load(ctx, database, october); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.CurrentMonth != dashboard.NewYearMonth(2026, 10) {
		t.Fatalf("current month = %+v", state.CurrentMonth)
	}

	november := date(t, 2026, time.November, 8)
	state.BeginLoad(dashboard.NewYearMonth(2026, 11), november)

	if state.CurrentMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("current month = %+v", state.CurrentMonth)
	}
	if state.SelectedMonth != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("the old October current month must not clamp November, got %+v", state.SelectedMonth)
	}
	if len(state.Months) == 0 || state.Months[0] != dashboard.NewYearMonth(2026, 11) {
		t.Fatalf("months = %+v", state.Months)
	}
}

func TestUnlinkedReceiptsCountAsPendingTasks(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	for _, name := range []string{"a.pdf", "b.pdf"} {
		if _, err := documents.Create(ctx, database, model.NewDocument{
			FileName:   name,
			StoredPath: "documents/" + name,
			MIMEType:   "application/pdf",
		}); err != nil {
			t.Fatalf("documents.Create(%s): %v", name, err)
		}
	}
	state := New(i18n.En, today)
	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}

	figures, err := dashboard.LoadDashboard(ctx, database, today, dashboard.NewYearMonth(2026, 10))
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}
	if want := fmt.Sprint(figures.UncategorizedCount + 2); view.PendingValue != want {
		t.Fatalf("pending value = %q, want %q", view.PendingValue, want)
	}
	if !strings.Contains(view.PendingSubtitle, "2 unlinked docs") {
		t.Fatalf("pending subtitle = %q", view.PendingSubtitle)
	}
}

func TestTheSeededViewMatchesTheDashboardFigures(t *testing.T) {
	database := testDatabase(t)
	_, food, _, _ := seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)
	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}

	figures, err := dashboard.LoadDashboard(ctx, database, today, dashboard.NewYearMonth(2026, 10))
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	if view.AvailableBalance != figures.AvailableBalance {
		t.Fatalf("available balance = %+v, want %+v", view.AvailableBalance, figures.AvailableBalance)
	}
	if view.TotalBalance != figures.TotalBalance {
		t.Fatalf("total balance = %+v, want %+v", view.TotalBalance, figures.TotalBalance)
	}
	if view.LoanBalance != figures.LoanBalance {
		t.Fatalf("loan balance = %+v, want %+v", view.LoanBalance, figures.LoanBalance)
	}
	if view.MonthlyIncome != figures.MonthlyIncome {
		t.Fatalf("monthly income = %+v, want %+v", view.MonthlyIncome, figures.MonthlyIncome)
	}
	if view.MonthlyExpenses != figures.MonthlyExpenses {
		t.Fatalf("monthly expenses = %+v, want %+v", view.MonthlyExpenses, figures.MonthlyExpenses)
	}
	if view.UncategorizedCount != figures.UncategorizedCount {
		t.Fatalf("uncategorized count = %d, want %d", view.UncategorizedCount, figures.UncategorizedCount)
	}
	if view.TransfersThisMonth != figures.TransfersThisMonth {
		t.Fatalf("transfers = %+v, want %+v", view.TransfersThisMonth, figures.TransfersThisMonth)
	}
	if len(view.ExpensesByCategory) != len(figures.ExpensesByCategory) {
		t.Fatalf("expense slices = %d, want %d", len(view.ExpensesByCategory), len(figures.ExpensesByCategory))
	}
	if len(view.MonthBars) != 6 {
		t.Fatalf("month bars = %d, want 6", len(view.MonthBars))
	}
	if len(view.RecentTransactions) != len(figures.RecentTransactions) {
		t.Fatalf("recent transactions = %d, want %d", len(view.RecentTransactions), len(figures.RecentTransactions))
	}

	var foodBudget *BudgetRowView
	for index := range view.Budgets {
		if view.Budgets[index].CategoryID == food {
			foodBudget = &view.Budgets[index]
			break
		}
	}
	if foodBudget == nil {
		t.Fatal("food budget is missing")
	}
	if foodBudget.Spent != moneyOf(4000) {
		t.Fatalf("food budget spent = %+v, want October's food booking", foodBudget.Spent)
	}
	if foodBudget.Limit != moneyOf(10_000) {
		t.Fatalf("food budget limit = %+v", foodBudget.Limit)
	}
	if foodBudget.IsOver {
		t.Fatal("food budget is over")
	}
	if foodBudget.RestText != "€60.00 left" {
		t.Fatalf("food budget rest text = %q", foodBudget.RestText)
	}

	if view.IncomeTitle != "Income this month" {
		t.Fatalf("income title = %q", view.IncomeTitle)
	}
	if view.IncomeValue != "€1,500.00" {
		t.Fatalf("income value = %q", view.IncomeValue)
	}
	if view.ExpensesValue != "€40.00" {
		t.Fatalf("expenses value = %q", view.ExpensesValue)
	}
	if view.PendingValue != "1" {
		t.Fatalf("pending value = %q, want October's income has no category", view.PendingValue)
	}
	if view.PendingSubtitle != "1 uncategorized · 0 unlinked docs" {
		t.Fatalf("pending subtitle = %q", view.PendingSubtitle)
	}
	if view.ExpenseRows[0].Label != "Food" {
		t.Fatalf("first expense label = %q", view.ExpenseRows[0].Label)
	}
	if view.ExpenseRows[0].PercentText != "100%" {
		t.Fatalf("first expense percent = %q", view.ExpenseRows[0].PercentText)
	}
	if view.TaxTotal != moneyOf(15_000) {
		t.Fatalf("tax total = %+v", view.TaxTotal)
	}
	if view.TaxDeductionsByCategory[0].Label != "Donations" {
		t.Fatalf("first tax slice = %q", view.TaxDeductionsByCategory[0].Label)
	}
	if view.TaxRows[0].Label != "Donations" {
		t.Fatalf("first tax row = %q", view.TaxRows[0].Label)
	}
	if view.TaxRows[0].PercentText != "100%" {
		t.Fatalf("first tax percent = %q", view.TaxRows[0].PercentText)
	}
	if view.AvailableSubtitle != "" {
		t.Fatalf("available subtitle = %q, want no loan or currency notes in this fixture", view.AvailableSubtitle)
	}
	if view.ChartAxisZero != "€0" {
		t.Fatalf("chart axis zero = %q", view.ChartAxisZero)
	}
	if view.ViewAllLabel == "" {
		t.Fatal("view all label is empty")
	}
}

func TestStatementLinesAddUpWithACarryForwardAndAMovement(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)
	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2026, 8)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	kinds := make([]StatementLineKind, 0, len(view.StatementLines))
	for _, line := range view.StatementLines {
		kinds = append(kinds, line.Kind)
	}
	wantKinds := []StatementLineKind{
		StatementLineKindCarry,
		StatementLineKindIn,
		StatementLineKindOut,
		StatementLineKindMovement,
		StatementLineKindResult,
	}
	if !equalKinds(kinds, wantKinds) {
		t.Fatalf("statement kinds = %+v, want %+v", kinds, wantKinds)
	}
	if view.StatementLines[0].Label != "Carried over from July 2026" {
		t.Fatalf("carry label = %q", view.StatementLines[0].Label)
	}
	if view.StatementLines[1].Label != "Income" {
		t.Fatalf("income label = %q", view.StatementLines[1].Label)
	}
	if view.StatementLines[2].Label != "Expenses" {
		t.Fatalf("expenses label = %q", view.StatementLines[2].Label)
	}
	if view.StatementLines[3].Label != "Transfers and loans" {
		t.Fatalf("movement label = %q", view.StatementLines[3].Label)
	}
	if view.StatementLines[4].Label != view.AvailableTitle {
		t.Fatalf("result label = %q, want %q", view.StatementLines[4].Label, view.AvailableTitle)
	}

	minor := func(line StatementLineView) int64 {
		amount, ok := format.ParseAmount(line.AmountText)
		if !ok {
			t.Fatalf("a statement amount parses back: %q", line.AmountText)
		}
		mantissa := amount.Mantissa()
		if !mantissa.IsInt64() {
			t.Fatalf("statement amount overflows int64: %q", line.AmountText)
		}
		return mantissa.Int64()
	}
	result := view.StatementLines[len(view.StatementLines)-1]
	var sum int64
	for _, line := range view.StatementLines[:len(view.StatementLines)-1] {
		sum += minor(line)
	}
	if sum != minor(result) {
		t.Fatalf("statement sum = %d, want result %d", sum, minor(result))
	}
	if minor(result) != view.AvailableBalance.Minor {
		t.Fatalf("statement result = %d, want available balance %d", minor(result), view.AvailableBalance.Minor)
	}
}

func equalKinds(left []StatementLineKind, right []StatementLineKind) bool {
	return reflect.DeepEqual(left, right)
}

func TestAStatementWithoutACarryForwardHasNoCarryOrMovementLine(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	figures, err := dashboard.LoadDashboard(ctx, database, today, dashboard.NewYearMonth(2026, 3))
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	figures.CarryForward = nil
	figures.OtherMovements = nil
	view := FromFigures(figures, 0, i18n.En)

	kinds := make([]StatementLineKind, 0, len(view.StatementLines))
	for _, line := range view.StatementLines {
		kinds = append(kinds, line.Kind)
	}
	want := []StatementLineKind{
		StatementLineKindIn,
		StatementLineKindOut,
		StatementLineKindResult,
	}
	if !equalKinds(kinds, want) {
		t.Fatalf("statement kinds = %+v, want %+v", kinds, want)
	}
	if view.CarryForward != nil {
		t.Fatalf("carry forward = %+v, want nil", view.CarryForward)
	}
	if view.OtherMovements != nil {
		t.Fatalf("other movements = %+v, want nil", view.OtherMovements)
	}
	if view.StatementLines[0].AmountText != "+€0.00" {
		t.Fatalf("income amount = %q", view.StatementLines[0].AmountText)
	}
}

func TestZeroMovementsHideTheMovementLine(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)
	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2026, 9)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	if view.OtherMovements == nil || *view.OtherMovements != moneyOf(0) {
		t.Fatalf("other movements = %+v, want 0", view.OtherMovements)
	}
	for _, line := range view.StatementLines {
		if line.Kind == StatementLineKindMovement {
			t.Fatal("zero movements show a movement line")
		}
	}
	if view.StatementLines[0].Kind != StatementLineKindCarry {
		t.Fatalf("first line kind = %v, want carry", view.StatementLines[0].Kind)
	}
}

func TestTheStatementUsesGermanAmounts(t *testing.T) {
	database := testDatabase(t)
	seedMonths(t, database)
	today := date(t, 2026, time.October, 15)
	state := New(i18n.De, today)
	if err := state.SelectMonth(ctx, database, today, dashboard.NewYearMonth(2026, 8)); err != nil {
		t.Fatalf("SelectMonth: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	amounts := make([]string, 0, len(view.StatementLines))
	for _, line := range view.StatementLines {
		amounts = append(amounts, line.AmountText)
	}
	want := []string{
		"-2.015,00\u00a0€",
		"+2.000,00\u00a0€",
		"-135,00\u00a0€",
		"-200,00\u00a0€",
		"-350,00\u00a0€",
	}
	if !equalStrings(amounts, want) {
		t.Fatalf("statement amounts = %q, want %q", amounts, want)
	}
	if view.StatementLines[0].Label != "Übertrag aus Juli 2026" {
		t.Fatalf("carry label = %q", view.StatementLines[0].Label)
	}
	if view.StatementLines[1].Label != "Einnahmen" {
		t.Fatalf("income label = %q", view.StatementLines[1].Label)
	}
	if view.StatementLines[2].Label != "Ausgaben" {
		t.Fatalf("expenses label = %q", view.StatementLines[2].Label)
	}
	if view.StatementLines[3].Label != "Umbuchungen und Kredite" {
		t.Fatalf("movement label = %q", view.StatementLines[3].Label)
	}
	if view.StatementLines[4].Label != "Verfügbar am 31.08.2026" {
		t.Fatalf("result label = %q", view.StatementLines[4].Label)
	}
}

func equalStrings(left []string, right []string) bool {
	return reflect.DeepEqual(left, right)
}

func TestTwoCurrenciesATransferABudgetAndALoanKeepTheirFigures(t *testing.T) {
	database := testDatabase(t)
	eur := createAccount(
		t,
		database,
		"DE89370400440532013000",
		100_000,
		money.EUR,
		model.AccountTypeChecking,
	)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	usd := createAccount(
		t,
		database,
		"DE44500105175407324931",
		50_000,
		usdCurrency,
		model.AccountTypeChecking,
	)
	loan := createAccount(
		t,
		database,
		"DE44500105175407324930",
		-500_000,
		money.EUR,
		model.AccountTypeLoan,
	)
	foodBudget := int64(10_000)
	food := createCategory(t, database, "Food", "#FF5722", false, false, &foodBudget)
	transfers := createCategory(t, database, "Own transfers", "#4CAF50", false, true, nil)
	insertTransaction(t, database, eur, money.EUR, date(t, 2026, time.October, 2), -4_000, "Bakery", &food)
	insertTransaction(t, database, eur, money.EUR, date(t, 2026, time.October, 3), 150_000, "Employer", nil)
	insertTransaction(t, database, eur, money.EUR, date(t, 2026, time.October, 4), -20_000, "Own transfer", &transfers)
	insertTransaction(t, database, usd, usdCurrency, date(t, 2026, time.October, 5), -3_000, "US shop", nil)
	insertTransaction(t, database, loan, money.EUR, date(t, 2026, time.October, 6), -1_000, "Interest", nil)

	today := date(t, 2026, time.October, 15)
	state := New(i18n.En, today)
	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}
	figures, err := dashboard.LoadDashboard(ctx, database, today, dashboard.NewYearMonth(2026, 10))
	if err != nil {
		t.Fatalf("LoadDashboard: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	if view.Currency != money.EUR {
		t.Fatalf("currency = %s", view.Currency)
	}
	if view.AvailableBalance != figures.AvailableBalance {
		t.Fatalf("available balance = %+v, want %+v", view.AvailableBalance, figures.AvailableBalance)
	}
	if view.TotalBalance != figures.TotalBalance {
		t.Fatalf("total balance = %+v, want %+v", view.TotalBalance, figures.TotalBalance)
	}
	if view.LoanBalance != moneyOf(-500_000) {
		t.Fatalf("loan balance = %+v", view.LoanBalance)
	}
	if !view.HasLoans {
		t.Fatal("has loans is false")
	}
	wantOther := []money.Money{{Minor: 50_000, Currency: usdCurrency}}
	if len(view.OtherBalances) != 1 || view.OtherBalances[0] != wantOther[0] {
		t.Fatalf("other balances = %+v, want %+v", view.OtherBalances, wantOther)
	}
	if !strings.Contains(view.AvailableSubtitle, "Loans") {
		t.Fatalf("available subtitle = %q", view.AvailableSubtitle)
	}
	if !strings.Contains(view.AvailableSubtitle, "other currencies") {
		t.Fatalf("available subtitle = %q", view.AvailableSubtitle)
	}
	if view.TransfersThisMonth != moneyOf(20_000) {
		t.Fatalf("transfers = %+v", view.TransfersThisMonth)
	}
	if view.Budgets[0].Spent != moneyOf(4_000) {
		t.Fatalf("budget spent = %+v", view.Budgets[0].Spent)
	}
	if view.MonthlyIncome != moneyOf(150_000) {
		t.Fatalf("monthly income = %+v", view.MonthlyIncome)
	}
	if view.MonthlyExpenses != moneyOf(5_000) {
		t.Fatalf("monthly expenses = %+v", view.MonthlyExpenses)
	}
	if view.UncategorizedCount != 3 {
		t.Fatalf("uncategorized count = %d, want 3", view.UncategorizedCount)
	}
}

func periodPointer(period transactionsview.Period) *transactionsview.Period {
	return &period
}

func monthPointer(month dashboard.YearMonth) *dashboard.YearMonth {
	return &month
}

func categoryPointer(category model.CategoryID) *model.CategoryID {
	return &category
}

func TestNavigationArgsCarryTheFilterFindeskUses(t *testing.T) {
	current := dashboard.NewYearMonth(2026, 10)
	past := dashboard.NewYearMonth(2026, 8)
	category := model.CategoryID(7)

	if AllNavigationArgs() != (NavigationArgs{}) {
		t.Fatalf("all = %+v", AllNavigationArgs())
	}
	got := CategoryNavigation(current, current, category)
	want := NavigationArgs{
		Month:             nil,
		CategoryID:        categoryPointer(category),
		UncategorizedOnly: false,
		Text:              nil,
		Period:            periodPointer(transactionsview.PeriodThisMonth),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current month category = %+v, want %+v", got, want)
	}
	got = CategoryNavigation(past, current, category)
	want = NavigationArgs{
		Month:             monthPointer(past),
		CategoryID:        categoryPointer(category),
		UncategorizedOnly: false,
		Text:              nil,
		Period:            nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("past month category = %+v, want %+v", got, want)
	}
	got = UncategorizedNavigation(current, current)
	want = NavigationArgs{
		Month:             nil,
		CategoryID:        nil,
		UncategorizedOnly: true,
		Text:              nil,
		Period:            periodPointer(transactionsview.PeriodThisMonth),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current month uncategorized = %+v, want %+v", got, want)
	}
	got = UncategorizedNavigation(past, current)
	want = NavigationArgs{
		Month:             monthPointer(past),
		CategoryID:        nil,
		UncategorizedOnly: true,
		Text:              nil,
		Period:            nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("past month uncategorized = %+v, want %+v", got, want)
	}
	text := "Bakery"
	got = PartnerNavigation(text)
	want = NavigationArgs{
		Month:             nil,
		CategoryID:        nil,
		UncategorizedOnly: false,
		Text:              &text,
		Period:            nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partner = %+v, want %+v", got, want)
	}
}

func TestNavigationArgsBecomeTheTransactionsStartArguments(t *testing.T) {
	current := dashboard.NewYearMonth(2026, 10)
	past := dashboard.NewYearMonth(2026, 8)
	category := model.CategoryID(7)

	got := CategoryNavigation(current, current, category).StartArgs()
	want := transactionsview.StartArgs{
		CategoryID: categoryPointer(category),
		Period:     periodPointer(transactionsview.PeriodThisMonth),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current month start args = %+v, want %+v", got, want)
	}
	got = CategoryNavigation(past, current, category).StartArgs()
	want = transactionsview.StartArgs{
		CategoryID: categoryPointer(category),
		Month:      monthPointer(past),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("past month start args = %+v, want %+v", got, want)
	}
	got = UncategorizedNavigation(past, current).StartArgs()
	want = transactionsview.StartArgs{
		UncategorizedOnly: true,
		Month:             monthPointer(past),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uncategorized start args = %+v, want %+v", got, want)
	}
	text := "Bakery"
	got = PartnerNavigation(text).StartArgs()
	want = transactionsview.StartArgs{Partner: &text}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partner start args = %+v, want %+v", got, want)
	}
	if AllNavigationArgs().StartArgs() != (transactionsview.StartArgs{}) {
		t.Fatalf("all start args = %+v", AllNavigationArgs().StartArgs())
	}
}

func TestUpcomingRecurringPaymentsBecomeRows(t *testing.T) {
	database := testDatabase(t)
	account := createAccount(
		t,
		database,
		"DE89370400440532013000",
		100_000,
		money.EUR,
		model.AccountTypeChecking,
	)
	for month := 5; month <= 7; month++ {
		day := date(t, 2026, time.Month(month), 10)
		insertTransaction(t, database, account, money.EUR, day, -999, "Spotify", nil)
	}

	today := date(t, 2026, time.July, 15)
	state := New(i18n.En, today)
	if err := state.Load(ctx, database, today); err != nil {
		t.Fatalf("Load: %v", err)
	}
	view := state.View
	if view == nil {
		t.Fatal("view is nil")
	}

	if len(view.Upcoming) != 1 {
		t.Fatalf("upcoming rows = %d, want 1", len(view.Upcoming))
	}
	payment := view.Upcoming[0]
	if payment.Name != "Spotify" {
		t.Fatalf("name = %q", payment.Name)
	}
	if payment.Cadence != recurring.Monthly {
		t.Fatalf("cadence = %v", payment.Cadence)
	}
	if !strings.Contains(payment.Meta, "Monthly") {
		t.Fatalf("meta = %q", payment.Meta)
	}
	if payment.AmountText != "-€9.99" {
		t.Fatalf("amount text = %q", payment.AmountText)
	}
	if !strings.HasSuffix(payment.DateText, ".2026") {
		t.Fatalf("date text = %q", payment.DateText)
	}
}

func transactionPartners(view *transactionsview.TransactionsState) []string {
	partners := make([]string, 0, len(view.Transactions))
	for _, transaction := range view.Transactions {
		if transaction.PartnerName != nil {
			partners = append(partners, *transaction.PartnerName)
		}
	}
	return partners
}

func TestTheCurrentMonthsCategoryLinkListsOnlyThatMonthsBookings(t *testing.T) {
	database, _, food := navigationMonthFixture(t)
	today := date(t, 2026, time.October, 15)
	current := dashboard.YearMonthFromDate(today)
	vm := transactionsview.New(i18n.En)
	if err := vm.Start(ctx, database, CategoryNavigation(current, current, food).StartArgs(), today); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if vm.State().Period != transactionsview.PeriodThisMonth {
		t.Fatalf("period = %v", vm.State().Period)
	}
	if vm.State().Month != nil {
		t.Fatal("the current month is a period, not an exact month")
	}
	partners := transactionPartners(vm.State())
	if !equalStrings(partners, []string{"Oktober Nazar"}) {
		t.Fatalf("partners = %q, want August's booking to stay out of the current month's list", partners)
	}
}

func TestAPastMonthsCategoryAndUncategorizedLinksListExactlyThatMonthsBookings(t *testing.T) {
	database, _, food := navigationMonthFixture(t)
	today := date(t, 2026, time.October, 15)
	current := dashboard.YearMonthFromDate(today)
	august := dashboard.NewYearMonth(2026, 8)

	vm := transactionsview.New(i18n.En)
	if err := vm.Start(ctx, database, CategoryNavigation(august, current, food).StartArgs(), today); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if vm.State().Month == nil || *vm.State().Month != august {
		t.Fatalf("month = %+v, want August", vm.State().Month)
	}
	partners := transactionPartners(vm.State())
	if !equalStrings(partners, []string{"August Nazar"}) {
		t.Fatalf("partners = %q", partners)
	}

	uncategorized := transactionsview.New(i18n.En)
	if err := uncategorized.Start(ctx, database, UncategorizedNavigation(august, current).StartArgs(), today); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if uncategorized.State().Month == nil || *uncategorized.State().Month != august {
		t.Fatalf("month = %+v, want August", uncategorized.State().Month)
	}
	if uncategorized.State().CategoryFilter != transactionsview.NoneCategoryFilter() {
		t.Fatalf("category filter = %+v", uncategorized.State().CategoryFilter)
	}
	if len(uncategorized.State().Transactions) != 2 {
		t.Fatalf("transactions = %d, want August's two uncategorized bookings", len(uncategorized.State().Transactions))
	}
	for _, transaction := range uncategorized.State().Transactions {
		if transaction.Date.Year() != 2026 || transaction.Date.Month() != time.August {
			t.Fatalf("transaction %d is dated %s", transaction.ID, transaction.Date)
		}
	}
}
