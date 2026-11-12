package dashboard

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	temp := t.TempDir()
	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	d, err := db.Create(filepath.Join(temp, "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func amount(minor int64) money.Money {
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
	d *db.Database,
	iban string,
	balance int64,
	currency money.Currency,
	accountType model.AccountType,
) model.AccountID {
	t.Helper()
	id, err := accounts.Create(context.Background(), d, model.NewAccount{
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

func eurAccount(t *testing.T, d *db.Database, iban string, balance int64) model.AccountID {
	t.Helper()
	return createAccount(t, d, iban, balance, money.EUR, model.AccountTypeChecking)
}

func createCategory(
	t *testing.T,
	d *db.Database,
	name string,
	color string,
	taxRelevant bool,
	isTransfer bool,
	budget *int64,
) model.CategoryID {
	t.Helper()
	var limit *money.Money
	if budget != nil {
		value := amount(*budget)
		limit = &value
	}
	id, err := categories.Create(context.Background(), d, model.NewCategory{
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

func insertTxn(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	currency money.Currency,
	day civil.Date,
	minor int64,
	partner string,
	categoryID *model.CategoryID,
) model.TransactionID {
	t.Helper()
	hash := fmt.Sprintf("%d-%s-%d-%s", accountID, day, minor, partner)
	_, err := transactions.InsertPlanned(context.Background(), d, accountID, []model.NewTransaction{{
		BookingDate:  day,
		Amount:       money.Money{Minor: minor, Currency: currency},
		BalanceAfter: nil,
		PartnerName:  &partner,
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
	listed, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
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

func monthFixtures(t *testing.T, d *db.Database) (
	model.AccountID, model.CategoryID, model.CategoryID, model.CategoryID,
) {
	t.Helper()
	account := eurAccount(t, d, "DE89370400440532013000", 100_000)
	food := createCategory(t, d, "Food", "#FF5722", false, false, nil)
	tax := createCategory(t, d, "Donations", "#3BC7B0", true, false, nil)
	transfers := createCategory(t, d, "Own transfers", "#4CAF50", false, true, nil)
	budget := int64(10_000)
	if err := categories.Update(context.Background(), d, food, model.NewCategory{
		Name:          "Food",
		Color:         "#FF5722",
		BudgetLimit:   ptr(amount(budget)),
		IsTaxRelevant: false,
		IsTransfer:    false,
	}); err != nil {
		t.Fatalf("categories.Update: %v", err)
	}
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.March, 5), -10_000, "MaerzJetzt", &tax)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 2), -12_000, "August Nazar", &food)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 3), 200_000, "Employer", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 4), -20_000, "Erika Musterfrau", &transfers)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 5), -1500, "Uncategorized August", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.September, 4), -6000, "September Nazar", &food)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.September, 6), -5000, "September Spenden", &tax)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.October, 2), -4000, "Oktober Nazar", &food)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.October, 3), 150_000, "Employer", nil)
	return account, food, tax, transfers
}

func ptr[T any](value T) *T {
	return &value
}

func mustLoad(t *testing.T, d *db.Database, today civil.Date, month YearMonth) DashboardFigures {
	t.Helper()
	figures, err := LoadDashboard(context.Background(), d, today, month)
	if err != nil {
		t.Fatalf("LoadDashboard(%v): %v", month, err)
	}
	return figures
}

func TestTheDashboardOpensOnTheCurrentMonth(t *testing.T) {
	d := testDatabase(t)
	monthFixtures(t, d)
	today := date(t, 2026, time.October, 15)
	figures := mustLoad(t, d, today, NewYearMonth(2026, 10))

	if figures.SelectedMonth != NewYearMonth(2026, 10) {
		t.Errorf("selected_month = %v, want 2026-10", figures.SelectedMonth)
	}
	if figures.LoadedMonth != figures.SelectedMonth {
		t.Errorf("loaded_month = %v, want selected_month %v", figures.LoadedMonth, figures.SelectedMonth)
	}
	if figures.EarliestMonth != NewYearMonth(2026, 3) {
		t.Errorf("earliest_month = %v, want 2026-03 (the month of the oldest booking)", figures.EarliestMonth)
	}
	if figures.BalanceDate != nil {
		t.Errorf("balance_date = %v, want nil", *figures.BalanceDate)
	}
}

func TestSelectingAPastMonthMovesEveryMonthFigureAndLeavesTheNowOnes(t *testing.T) {
	d := testDatabase(t)
	_, food, _, _ := monthFixtures(t, d)
	today := date(t, 2026, time.October, 15)
	before := mustLoad(t, d, today, NewYearMonth(2026, 10))

	figures := mustLoad(t, d, today, NewYearMonth(2026, 8))

	if figures.SelectedMonth != NewYearMonth(2026, 8) {
		t.Errorf("selected_month = %v, want 2026-08", figures.SelectedMonth)
	}
	if figures.MonthlyIncome != amount(200_000) {
		t.Errorf("monthly_income = %v, want 2000.00 (August only)", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses != amount(13_500) {
		t.Errorf("monthly_expenses = %v, want 135.00 (August only)", figures.MonthlyExpenses)
	}
	labels := make([]string, 0, len(figures.ExpensesByCategory))
	for _, slice := range figures.ExpensesByCategory {
		labels = append(labels, slice.Label)
	}
	if fmt.Sprint(labels) != fmt.Sprint([]string{"Food", "Uncategorized"}) {
		t.Errorf("expenses_by_category labels = %v, want [Food Uncategorized] (the August split)", labels)
	}
	if figures.TransfersThisMonth != amount(20_000) {
		t.Errorf("transfers_this_month = %v, want 200.00 (the August transfer)", figures.TransfersThisMonth)
	}
	if figures.UncategorizedCount != 2 {
		t.Errorf("uncategorized_count = %d, want 2 (the uncategorized expense and salary in August)", figures.UncategorizedCount)
	}
	var foodBudget *BudgetStatus
	for index := range figures.Budgets {
		if figures.Budgets[index].CategoryID == food {
			foodBudget = &figures.Budgets[index]
		}
	}
	if foodBudget == nil {
		t.Fatal("the Food budget is missing")
	}
	if foodBudget.Spent != amount(12_000) {
		t.Errorf("food budget spent = %v, want 120.00 (budget spending is judged for August)", foodBudget.Spent)
	}
	if figures.MonthProgress != 1.0 {
		t.Errorf("month_progress = %v, want 1.0 (a past month is over)", figures.MonthProgress)
	}
	months := make([]int, 0, len(figures.LastMonths))
	for _, bar := range figures.LastMonths {
		months = append(months, bar.Month)
	}
	if fmt.Sprint(months) != fmt.Sprint([]int{3, 4, 5, 6, 7, 8}) {
		t.Errorf("last_months months = %v, want [3 4 5 6 7 8] (the six months ending with August)", months)
	}
	var augustBar *MonthBar
	for index := range figures.LastMonths {
		if figures.LastMonths[index].Month == 8 {
			augustBar = &figures.LastMonths[index]
		}
	}
	if augustBar == nil {
		t.Fatal("the August bar is missing")
	}
	if augustBar.Income.Minor <= 0 {
		t.Errorf("the August bar income = %v, want positive", augustBar.Income)
	}
	taxLabels := make([]string, 0, len(figures.TaxDeductionsByCategory))
	for _, slice := range figures.TaxDeductionsByCategory {
		taxLabels = append(taxLabels, slice.Label)
	}
	if fmt.Sprint(taxLabels) != fmt.Sprint([]string{"Donations"}) {
		t.Errorf("tax_deductions_by_category labels = %v, want [Donations] (January 1 to August 31)", taxLabels)
	}
	if len(figures.TaxDeductionsByCategory) != 1 {
		t.Fatalf("tax_deductions_by_category = %d, want 1", len(figures.TaxDeductionsByCategory))
	}
	if figures.TaxDeductionsByCategory[0].Amount != amount(10_000) {
		t.Errorf("tax_deductions_by_category[0].amount = %v, want 100.00", figures.TaxDeductionsByCategory[0].Amount)
	}
	if figures.TotalBalance != amount(-35_000) {
		t.Errorf("total_balance = %v, want -350.00 (August's balance reconstructed from today's)", figures.TotalBalance)
	}
	if figures.AvailableBalance != amount(-35_000) {
		t.Errorf("available_balance = %v, want -350.00", figures.AvailableBalance)
	}
	if before.LoanBalance != figures.LoanBalance {
		t.Errorf("loan_balance = %v, want %v", figures.LoanBalance, before.LoanBalance)
	}
	if len(before.RecentTransactions) != len(figures.RecentTransactions) {
		t.Errorf("recent_transactions = %d, want %d", len(figures.RecentTransactions), len(before.RecentTransactions))
	} else {
		for index := range before.RecentTransactions {
			if before.RecentTransactions[index].ID != figures.RecentTransactions[index].ID {
				t.Errorf(
					"recent_transactions[%d].id = %d, want %d",
					index,
					figures.RecentTransactions[index].ID,
					before.RecentTransactions[index].ID,
				)
			}
		}
	}
}

func TestTheSelectionClampsToTheLoadedRange(t *testing.T) {
	d := testDatabase(t)
	monthFixtures(t, d)
	today := date(t, 2026, time.October, 15)

	figures := mustLoad(t, d, today, NewYearMonth(2026, 12))
	if figures.SelectedMonth != NewYearMonth(2026, 10) {
		t.Errorf("a future month selected_month = %v, want 2026-10", figures.SelectedMonth)
	}
	figures = mustLoad(t, d, today, NewYearMonth(2026, 1))
	if figures.SelectedMonth != NewYearMonth(2026, 3) {
		t.Errorf("a month before the oldest booking selected_month = %v, want 2026-03", figures.SelectedMonth)
	}
	figures = mustLoad(t, d, today, NewYearMonth(2026, 10))
	if figures.SelectedMonth != NewYearMonth(2026, 10) {
		t.Errorf("the current month selected_month = %v, want 2026-10", figures.SelectedMonth)
	}
}

func TestAReloadKeepsTheSelection(t *testing.T) {
	d := testDatabase(t)
	monthFixtures(t, d)
	today := date(t, 2026, time.October, 15)
	mustLoad(t, d, today, NewYearMonth(2026, 8))
	again := mustLoad(t, d, today, NewYearMonth(2026, 8))

	if again.SelectedMonth != NewYearMonth(2026, 8) {
		t.Errorf("selected_month = %v, want 2026-08", again.SelectedMonth)
	}
	if again.MonthlyExpenses != amount(13_500) {
		t.Errorf("monthly_expenses = %v, want 135.00 (August again)", again.MonthlyExpenses)
	}
}

func TestWithoutBookingsEveryMonthIsTheCurrentMonth(t *testing.T) {
	d := testDatabase(t)
	eurAccount(t, d, "DE89370400440532013000", 0)
	today := date(t, 2026, time.October, 15)
	figures := mustLoad(t, d, today, NewYearMonth(2026, 10))

	if figures.EarliestMonth != NewYearMonth(2026, 10) {
		t.Errorf("earliest_month = %v, want 2026-10", figures.EarliestMonth)
	}
	if figures.SelectedMonth != NewYearMonth(2026, 10) {
		t.Errorf("selected_month = %v, want 2026-10", figures.SelectedMonth)
	}
}

func TestAMonthSelectedBeforeTheFirstLoadIsKept(t *testing.T) {
	d := testDatabase(t)
	account := eurAccount(t, d, "DE89370400440532013000", 0)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.May, 4), -6000, "May", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 2), -12_000, "August", nil)

	august := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 8))
	if august.SelectedMonth != NewYearMonth(2026, 8) {
		t.Errorf("selected_month = %v, want 2026-08", august.SelectedMonth)
	}
	if august.MonthlyExpenses != amount(12_000) {
		t.Errorf("monthly_expenses = %v, want 120.00", august.MonthlyExpenses)
	}

	tooEarly := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2025, 1))
	if tooEarly.SelectedMonth != NewYearMonth(2026, 5) {
		t.Errorf("selected_month = %v, want 2026-05", tooEarly.SelectedMonth)
	}
}

func TestOnlyTheRunningMonthIsPartial(t *testing.T) {
	d := testDatabase(t)
	account := eurAccount(t, d, "DE89370400440532013000", 0)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.August, 2), -12_000, "August", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.October, 3), -4000, "October", nil)

	current := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 10))
	months := make([]int, 0, len(current.LastMonths))
	for _, bar := range current.LastMonths {
		months = append(months, bar.Month)
	}
	if fmt.Sprint(months) != fmt.Sprint([]int{5, 6, 7, 8, 9, 10}) {
		t.Errorf("last_months months = %v, want [5 6 7 8 9 10]", months)
	}
	partial := make([]int, 0)
	for _, bar := range current.LastMonths {
		if bar.Partial {
			partial = append(partial, bar.Month)
		}
	}
	if fmt.Sprint(partial) != fmt.Sprint([]int{10}) {
		t.Errorf("partial months = %v, want [10] (exactly October is partial)", partial)
	}

	past := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 8))
	months = make([]int, 0, len(past.LastMonths))
	for _, bar := range past.LastMonths {
		months = append(months, bar.Month)
	}
	if fmt.Sprint(months) != fmt.Sprint([]int{3, 4, 5, 6, 7, 8}) {
		t.Errorf("past last_months months = %v, want [3 4 5 6 7 8]", months)
	}
	for _, bar := range past.LastMonths {
		if bar.Partial {
			t.Errorf("past month %d is partial, want none", bar.Month)
		}
	}
}

func TestAvailableBalanceExcludesLoansAndDepotsAndLoanBalanceSumsLoans(t *testing.T) {
	d := testDatabase(t)
	eurAccount(t, d, "DE89370400440532013001", 10_000)
	createAccount(t, d, "DE89370400440532013002", 5000, money.EUR, model.AccountTypeSavings)
	createAccount(t, d, "DE89370400440532013003", -3000, money.EUR, model.AccountTypeCreditCard)
	createAccount(t, d, "DE89370400440532013004", -100_000, money.EUR, model.AccountTypeLoan)
	createAccount(t, d, "DE89370400440532013005", 50_000, money.EUR, model.AccountTypeDepot)

	figures := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 10))

	if figures.AvailableBalance != amount(12_000) {
		t.Errorf("available_balance = %v, want 120.00", figures.AvailableBalance)
	}
	if figures.LoanBalance != amount(-100_000) {
		t.Errorf("loan_balance = %v, want -1000.00", figures.LoanBalance)
	}
	if figures.TotalBalance != amount(-38_000) {
		t.Errorf("total_balance = %v, want -380.00", figures.TotalBalance)
	}
	if !figures.HasLoans {
		t.Error("has_loans = false, want true")
	}
}

func TestWithoutLoanAccountsLoanBalanceIsZeroAndHasLoansIsFalse(t *testing.T) {
	d := testDatabase(t)
	eurAccount(t, d, "DE89370400440532013001", 25_000)
	createAccount(t, d, "DE89370400440532013002", 100_000, money.EUR, model.AccountTypeDepot)

	figures := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 10))

	if figures.AvailableBalance != amount(25_000) {
		t.Errorf("available_balance = %v, want 250.00", figures.AvailableBalance)
	}
	if figures.LoanBalance.Minor != 0 {
		t.Errorf("loan_balance.minor = %d, want 0", figures.LoanBalance.Minor)
	}
	if figures.HasLoans {
		t.Error("has_loans = true, want false")
	}
}

func carryFixtures(t *testing.T, d *db.Database) (
	model.AccountID, model.AccountID, model.AccountID, model.CategoryID,
) {
	t.Helper()
	checking := eurAccount(t, d, "DE89370400440532013000", 100_000)
	loan := createAccount(t, d, "DE44500105175407324001", -500_000, money.EUR, model.AccountTypeLoan)
	depot := createAccount(t, d, "DE02120300000000202051", 300_000, money.EUR, model.AccountTypeDepot)
	transfers := createCategory(t, d, "Own transfers", "#4CAF50", false, true, nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.August, 1), -1000, "August", nil)
	insertTxn(t, d, loan, money.EUR, date(t, 2026, time.August, 2), -10_000, "Loan opening", nil)
	insertTxn(t, d, depot, money.EUR, date(t, 2026, time.August, 3), 10_000, "Depot opening", nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.September, 4), 200_000, "Employer", nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.September, 10), -230_000, "Rent", nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.October, 3), 25_000, "Refund", nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.October, 5), -5000, "Bakery", nil)
	return checking, loan, depot, transfers
}

func load(t *testing.T, d *db.Database, month YearMonth) DashboardFigures {
	t.Helper()
	return mustLoad(t, d, date(t, 2026, time.October, 20), month)
}

func TestAPastMonthShowsItsOwnBalanceCarriedOverFromThePreviousOne(t *testing.T) {
	d := testDatabase(t)
	carryFixtures(t, d)

	figures := load(t, d, NewYearMonth(2026, 9))

	if figures.AvailableBalance != amount(80_000) {
		t.Errorf("available_balance = %v, want 800.00", figures.AvailableBalance)
	}
	if figures.BalanceDate == nil || figures.BalanceDate.String() != "2026-09-30" {
		t.Errorf("balance_date = %v, want 2026-09-30", figures.BalanceDate)
	}
	if figures.CarryForward == nil || *figures.CarryForward != amount(110_000) {
		t.Errorf("carry_forward = %v, want 1100.00", figures.CarryForward)
	}
	if figures.OtherMovements == nil || *figures.OtherMovements != amount(0) {
		t.Errorf("other_movements = %v, want 0.00", figures.OtherMovements)
	}
}

func TestCarryForwardIncomeExpensesAndOtherMovementsAddUpExactly(t *testing.T) {
	d := testDatabase(t)
	carryFixtures(t, d)

	for _, month := range []YearMonth{NewYearMonth(2026, 9), NewYearMonth(2026, 10)} {
		figures := load(t, d, month)
		total := figures.CarryForward.Minor + figures.MonthlyIncome.Minor -
			figures.MonthlyExpenses.Minor + figures.OtherMovements.Minor
		if total != figures.AvailableBalance.Minor {
			t.Errorf("the identity holds for %v: total = %d, want available_balance %d",
				month, total, figures.AvailableBalance.Minor)
		}
	}
}

func TestALoanRepaymentLowersTheAvailableBalanceButIsNotAnExpense(t *testing.T) {
	d := testDatabase(t)
	checking, loan, _, transfers := carryFixtures(t, d)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.September, 15), -50_000, "Loan repayment", &transfers)
	insertTxn(t, d, loan, money.EUR, date(t, 2026, time.September, 15), 50_000, "Loan repayment", &transfers)

	figures := load(t, d, NewYearMonth(2026, 9))

	if figures.OtherMovements == nil || *figures.OtherMovements != amount(-50_000) {
		t.Errorf("other_movements = %v, want -500.00", figures.OtherMovements)
	}
	if figures.MonthlyIncome != amount(200_000) {
		t.Errorf("monthly_income = %v, want 2000.00", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses != amount(230_000) {
		t.Errorf("monthly_expenses = %v, want 2300.00", figures.MonthlyExpenses)
	}
}

func TestLoanInterestIsAnExpenseAndComesBackAsAnOtherMovement(t *testing.T) {
	d := testDatabase(t)
	_, loan, _, _ := carryFixtures(t, d)
	insertTxn(t, d, loan, money.EUR, date(t, 2026, time.September, 20), -4000, "Loan interest", nil)

	figures := load(t, d, NewYearMonth(2026, 9))

	if figures.MonthlyExpenses != amount(234_000) {
		t.Errorf("monthly_expenses = %v, want 2340.00", figures.MonthlyExpenses)
	}
	if figures.OtherMovements == nil || *figures.OtherMovements != amount(4000) {
		t.Errorf("other_movements = %v, want 40.00", figures.OtherMovements)
	}
	total := figures.CarryForward.Minor + figures.MonthlyIncome.Minor -
		figures.MonthlyExpenses.Minor + figures.OtherMovements.Minor
	if total != figures.AvailableBalance.Minor {
		t.Errorf("total = %d, want available_balance %d", total, figures.AvailableBalance.Minor)
	}
}

func TestATransferBetweenAvailableAccountsIsNoOtherMovement(t *testing.T) {
	d := testDatabase(t)
	checking, _, _, transfers := carryFixtures(t, d)
	savings := createAccount(t, d, "DE44500105175407324931", 50_000, money.EUR, model.AccountTypeChecking)
	insertTxn(t, d, savings, money.EUR, date(t, 2026, time.August, 1), 100, "August", nil)
	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.September, 18), -10_000, "To savings", &transfers)
	insertTxn(t, d, savings, money.EUR, date(t, 2026, time.September, 18), 10_000, "From checking", &transfers)

	figures := load(t, d, NewYearMonth(2026, 9))

	if figures.OtherMovements == nil || *figures.OtherMovements != amount(0) {
		t.Errorf("other_movements = %v, want 0.00", figures.OtherMovements)
	}
	if figures.MonthlyIncome != amount(200_000) {
		t.Errorf("monthly_income = %v, want 2000.00", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses != amount(230_000) {
		t.Errorf("monthly_expenses = %v, want 2300.00", figures.MonthlyExpenses)
	}
}

func TestAnIncompleteHistoryStillShowsTheCarryForward(t *testing.T) {
	d := testDatabase(t)
	carryFixtures(t, d)
	savings := createAccount(t, d, "DE44500105175407324931", 50_000, money.EUR, model.AccountTypeChecking)
	insertTxn(t, d, savings, money.EUR, date(t, 2026, time.September, 5), 10_000, "First booking", nil)

	september := load(t, d, NewYearMonth(2026, 9))
	if september.CarryForward == nil || *september.CarryForward != amount(150_000) {
		t.Errorf("carry_forward = %v, want 1500.00", september.CarryForward)
	}
	if september.OtherMovements == nil || *september.OtherMovements != amount(0) {
		t.Errorf("other_movements = %v, want 0.00", september.OtherMovements)
	}

	for _, month := range []YearMonth{NewYearMonth(2026, 9), NewYearMonth(2026, 10)} {
		figures := load(t, d, month)
		total := figures.CarryForward.Minor + figures.MonthlyIncome.Minor -
			figures.MonthlyExpenses.Minor + figures.OtherMovements.Minor
		if total != figures.AvailableBalance.Minor {
			t.Errorf("the identity holds for %v despite the incomplete history: total = %d, want %d",
				month, total, figures.AvailableBalance.Minor)
		}
	}
}

func TestTheCurrentMonthKeepsTodaysBalanceEvenWithAFutureDatedBooking(t *testing.T) {
	d := testDatabase(t)
	checking, _, _, _ := carryFixtures(t, d)

	figures := load(t, d, NewYearMonth(2026, 10))
	if figures.BalanceDate != nil {
		t.Errorf("balance_date = %v, want nil", *figures.BalanceDate)
	}
	if figures.AvailableBalance != amount(100_000) {
		t.Errorf("available_balance = %v, want 1000.00", figures.AvailableBalance)
	}

	insertTxn(t, d, checking, money.EUR, date(t, 2026, time.November, 5), -10_000, "Future", nil)
	figures = load(t, d, NewYearMonth(2026, 10))
	if figures.AvailableBalance != amount(100_000) {
		t.Errorf("available_balance after the future booking = %v, want 1000.00", figures.AvailableBalance)
	}
	total := figures.CarryForward.Minor + figures.MonthlyIncome.Minor -
		figures.MonthlyExpenses.Minor + figures.OtherMovements.Minor
	if total != figures.AvailableBalance.Minor {
		t.Errorf("total = %d, want available_balance %d", total, figures.AvailableBalance.Minor)
	}
}

func TestADepotKeepsItsCurrentMarketBalanceForAPastMonth(t *testing.T) {
	d := testDatabase(t)
	_, _, depot, _ := carryFixtures(t, d)
	insertTxn(t, d, depot, money.EUR, date(t, 2026, time.September, 12), 70_000, "Depot buy", nil)

	figures := load(t, d, NewYearMonth(2026, 9))

	if figures.AvailableBalance != amount(80_000) {
		t.Errorf("available_balance = %v, want 800.00", figures.AvailableBalance)
	}
	if figures.LoanBalance != amount(-500_000) {
		t.Errorf("loan_balance = %v, want -5000.00", figures.LoanBalance)
	}
	if figures.TotalBalance != amount(-120_000) {
		t.Errorf("total_balance = %v, want -1200.00 (the depot keeps its current balance)", figures.TotalBalance)
	}
}

func TestDashboardTotalsStayInThePrimaryCurrency(t *testing.T) {
	d := testDatabase(t)
	eur := eurAccount(t, d, "DE89370400440532013000", 100_000)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("money.NewCurrency(USD): %v", err)
	}
	usd := createAccount(t, d, "DE44500105175407324931", 50_000, usdCurrency, model.AccountTypeChecking)
	insertTxn(t, d, eur, money.EUR, date(t, 2026, time.October, 2), 200_000, "Employer", nil)
	insertTxn(t, d, usd, usdCurrency, date(t, 2026, time.October, 3), 80_000, "US client", nil)
	insertTxn(t, d, usd, usdCurrency, date(t, 2026, time.October, 4), -4000, "US shop", nil)

	figures := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 10))

	if figures.Currency != money.EUR {
		t.Errorf("currency = %v, want EUR", figures.Currency)
	}
	if figures.TotalBalance != amount(100_000) {
		t.Errorf("total_balance = %v, want 1000.00 (not 1500: USD isn't added to EUR)", figures.TotalBalance)
	}
	balance := otherBalance(figures, usdCurrency)
	wantBalance := money.Money{Minor: 50_000, Currency: usdCurrency}
	if balance == nil || *balance != wantBalance {
		t.Errorf("other_balances[USD] = %v, want %v", balance, wantBalance)
	}
	if figures.MonthlyIncome != amount(200_000) {
		t.Errorf("monthly_income = %v, want 2000.00", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses.Minor != 0 {
		t.Errorf("monthly_expenses.minor = %d, want 0", figures.MonthlyExpenses.Minor)
	}
	if len(figures.ExpensesByCategory) != 0 {
		t.Errorf("expenses_by_category = %v, want empty", figures.ExpensesByCategory)
	}
	if figures.UncategorizedCount != 3 {
		t.Errorf("uncategorized_count = %d, want 3 (all uncategorized bookings, any currency)", figures.UncategorizedCount)
	}
}

func TestThePrimaryCurrencySettingSwitchesWhatTheDashboardTotals(t *testing.T) {
	d := testDatabase(t)
	eur := eurAccount(t, d, "DE89370400440532013000", 100_000)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("money.NewCurrency(USD): %v", err)
	}
	usd := createAccount(t, d, "DE44500105175407324931", 50_000, usdCurrency, model.AccountTypeChecking)
	insertTxn(t, d, eur, money.EUR, date(t, 2026, time.October, 2), 200_000, "Employer", nil)
	insertTxn(t, d, usd, usdCurrency, date(t, 2026, time.October, 3), 80_000, "US client", nil)
	if err := settings.Set(context.Background(), d, money.PrimaryCurrencyKey, "USD"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	figures := mustLoad(t, d, date(t, 2026, time.October, 15), NewYearMonth(2026, 10))

	if figures.Currency != usdCurrency {
		t.Errorf("currency = %v, want USD", figures.Currency)
	}
	if figures.TotalBalance.Minor != 50_000 {
		t.Errorf("total_balance.minor = %d, want 50000", figures.TotalBalance.Minor)
	}
	if figures.MonthlyIncome.Minor != 80_000 {
		t.Errorf("monthly_income.minor = %d, want 80000", figures.MonthlyIncome.Minor)
	}
}

func TestDashboardAndBarsLeaveTransfersOutOfIncomeAndSpending(t *testing.T) {
	d := testDatabase(t)
	today := date(t, 2026, time.June, 15)
	account := eurAccount(t, d, "DE89370400440532013000", 100_000)
	second := eurAccount(t, d, "DE12500105170137075030", 50_000)
	transfers := createCategory(t, d, "Own transfers", "#4CAF50", false, true, nil)
	groceries := createCategory(t, d, "Groceries", "#FF5722", false, false, nil)
	insertTxn(t, d, account, money.EUR, today, -50_000, "Erika Musterfrau", &transfers)
	insertTxn(t, d, second, money.EUR, today, 50_000, "Erika Musterfrau", &transfers)
	insertTxn(t, d, account, money.EUR, today.AddDays(1), -4000, "BEISPIEL MARKT", &groceries)
	insertTxn(t, d, account, money.EUR, today.AddDays(2), 100_000, "Erika Mustermann", nil)

	figures := mustLoad(t, d, today, NewYearMonth(2026, 6))
	if figures.MonthlyIncome != amount(100_000) {
		t.Errorf("monthly_income = %v, want 1000.00", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses != amount(4000) {
		t.Errorf("monthly_expenses = %v, want 40.00", figures.MonthlyExpenses)
	}
	labels := make([]string, 0, len(figures.ExpensesByCategory))
	for _, slice := range figures.ExpensesByCategory {
		labels = append(labels, slice.Label)
	}
	if fmt.Sprint(labels) != fmt.Sprint([]string{"Groceries"}) {
		t.Errorf("expenses_by_category labels = %v, want [Groceries]", labels)
	}
	if figures.TransfersThisMonth != amount(50_000) {
		t.Errorf("transfers_this_month = %v, want 500.00", figures.TransfersThisMonth)
	}
	var june *MonthBar
	for index := range figures.LastMonths {
		if figures.LastMonths[index].Month == 6 {
			june = &figures.LastMonths[index]
		}
	}
	if june == nil {
		t.Fatal("the June bar is missing")
	}
	if june.Income != amount(100_000) {
		t.Errorf("june.income = %v, want 1000.00", june.Income)
	}
	if june.Expenses != amount(4000) {
		t.Errorf("june.expenses = %v, want 40.00", june.Expenses)
	}
}

func TestBudgetsLeaveTransfersOutAndATransferBudgetStaysAtZeroSpent(t *testing.T) {
	d := testDatabase(t)
	today := date(t, 2026, time.June, 15)
	account := eurAccount(t, d, "DE89370400440532013000", 0)
	second := eurAccount(t, d, "DE12500105170137075030", 0)
	transfersBudget := int64(50_000)
	transportBudget := int64(10_000)
	transfers := createCategory(t, d, "Own transfers", "#4CAF50", false, true, &transfersBudget)
	transport := createCategory(t, d, "Transport", "#FF5722", true, false, &transportBudget)
	insertTxn(t, d, account, money.EUR, today, -50_000, "Transfer", &transfers)
	insertTxn(t, d, second, money.EUR, today, 50_000, "Transfer", &transfers)
	insertTxn(t, d, account, money.EUR, today, -3000, "Train", &transport)

	figures := mustLoad(t, d, today, NewYearMonth(2026, 6))
	var transportStatus *BudgetStatus
	for index := range figures.Budgets {
		if figures.Budgets[index].CategoryID == transport {
			transportStatus = &figures.Budgets[index]
		}
	}
	if transportStatus == nil {
		t.Fatal("the Transport budget is missing")
	}
	if transportStatus.Spent != amount(3000) {
		t.Errorf("transport budget spent = %v, want 30.00", transportStatus.Spent)
	}
	var transferStatus *BudgetStatus
	for index := range figures.Budgets {
		if figures.Budgets[index].CategoryID == transfers {
			transferStatus = &figures.Budgets[index]
		}
	}
	if transferStatus == nil {
		t.Fatal("the transfer budget is missing")
	}
	if transferStatus.Spent != amount(0) {
		t.Errorf("transfer budget spent = %v, want 0.00", transferStatus.Spent)
	}
	if transferStatus.Remaining() != amount(50_000) {
		t.Errorf("transfer budget remaining = %v, want 500.00", transferStatus.Remaining())
	}
}

func TestDashboardSeparatesIncomeFromExpenses(t *testing.T) {
	d := testDatabase(t)
	today := date(t, 2026, time.March, 20)
	account := eurAccount(t, d, "DE89370400440532013000", 150_000)
	food := createCategory(t, d, "Food", "#4CAF50", false, false, nil)
	tax := createCategory(t, d, "Spenden", "#3BC7B0", true, false, nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.March, 2), -3000, "REWE", &food)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.March, 5), -2000, "Kiosk", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.March, 1), 250_000, "Employer", nil)
	insertTxn(t, d, account, money.EUR, date(t, 2026, time.January, 15), -10_000, "UNICEF", &tax)

	figures := mustLoad(t, d, today, NewYearMonth(2026, 3))

	if figures.TotalBalance != amount(150_000) {
		t.Errorf("total_balance = %v, want 1500.00", figures.TotalBalance)
	}
	if figures.MonthlyIncome != amount(250_000) {
		t.Errorf("monthly_income = %v, want 2500.00", figures.MonthlyIncome)
	}
	if figures.MonthlyExpenses != amount(5000) {
		t.Errorf("monthly_expenses = %v, want 50.00", figures.MonthlyExpenses)
	}
	if figures.UncategorizedCount != 2 {
		t.Errorf("uncategorized_count = %d, want 2 (Kiosk and the uncategorized salary)", figures.UncategorizedCount)
	}
	labels := make([]string, 0, len(figures.ExpensesByCategory))
	for _, slice := range figures.ExpensesByCategory {
		labels = append(labels, slice.Label)
	}
	if fmt.Sprint(labels) != fmt.Sprint([]string{"Food", "Uncategorized"}) {
		t.Errorf("expenses_by_category labels = %v, want [Food Uncategorized]", labels)
	}
	if len(figures.ExpensesByCategory) != 2 {
		t.Fatalf("expenses_by_category = %d, want 2", len(figures.ExpensesByCategory))
	}
	if figures.ExpensesByCategory[0].Color == nil || *figures.ExpensesByCategory[0].Color != "#4CAF50" {
		t.Errorf("expenses_by_category[0].color = %v, want #4CAF50", figures.ExpensesByCategory[0].Color)
	}
	if figures.ExpensesByCategory[1].Color == nil || *figures.ExpensesByCategory[1].Color != UncategorizedColor {
		t.Errorf("expenses_by_category[1].color = %v, want %s", figures.ExpensesByCategory[1].Color, UncategorizedColor)
	}
	taxLabels := make([]string, 0, len(figures.TaxDeductionsByCategory))
	for _, slice := range figures.TaxDeductionsByCategory {
		taxLabels = append(taxLabels, slice.Label)
	}
	if fmt.Sprint(taxLabels) != fmt.Sprint([]string{"Spenden"}) {
		t.Errorf("tax_deductions_by_category labels = %v, want [Spenden]", taxLabels)
	}
	if len(figures.LastMonths) != 6 {
		t.Errorf("last_months = %d, want 6", len(figures.LastMonths))
	}
	var januaryBar *MonthBar
	for index := range figures.LastMonths {
		if figures.LastMonths[index].Month == 1 {
			januaryBar = &figures.LastMonths[index]
		}
	}
	if januaryBar == nil {
		t.Fatal("the January bar is missing")
	}
	if januaryBar.Expenses != amount(10_000) {
		t.Errorf("the January bar expenses = %v, want 100.00", januaryBar.Expenses)
	}
}

func otherBalance(figures DashboardFigures, currency money.Currency) *money.Money {
	for index := range figures.OtherBalances {
		if figures.OtherBalances[index].Currency == currency {
			return &figures.OtherBalances[index]
		}
	}
	return nil
}
