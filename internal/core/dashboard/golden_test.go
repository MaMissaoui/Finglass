package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// fixturePassphrase is the throwaway dashboard database's test-only
// passphrase.
const fixturePassphrase = "correct horse battery staple"

type goldenYearMonth struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

type goldenMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenOtherBalance struct {
	Currency string      `json:"currency"`
	Amount   goldenMoney `json:"amount"`
}

type goldenChartSlice struct {
	Label      string      `json:"label"`
	Amount     goldenMoney `json:"amount"`
	Color      *string     `json:"color"`
	CategoryID *int64      `json:"category_id"`
}

type goldenMonthBar struct {
	Year     int         `json:"year"`
	Month    int         `json:"month"`
	Income   goldenMoney `json:"income"`
	Expenses goldenMoney `json:"expenses"`
	Partial  bool        `json:"partial"`
}

type goldenBudget struct {
	CategoryID   int64       `json:"category_id"`
	CategoryName string      `json:"category_name"`
	Color        string      `json:"color"`
	Limit        goldenMoney `json:"limit"`
	Spent        goldenMoney `json:"spent"`
	Ratio        float64     `json:"ratio"`
	Remaining    goldenMoney `json:"remaining"`
	IsOver       bool        `json:"is_over"`
}

type goldenPayment struct {
	Name          string      `json:"name"`
	Cadence       string      `json:"cadence"`
	TypicalAmount goldenMoney `json:"typical_amount"`
	FixedAmount   bool        `json:"fixed_amount"`
	LastDate      string      `json:"last_date"`
	NextDate      string      `json:"next_date"`
	Occurrences   int         `json:"occurrences"`
	CategoryName  *string     `json:"category_name"`
	Currency      string      `json:"currency"`
	IsTransfer    bool        `json:"is_transfer"`
	IsIncome      bool        `json:"is_income"`
	YearlyAmount  goldenMoney `json:"yearly_amount"`
}

type goldenTransaction struct {
	ID             int64        `json:"id"`
	AccountID      int64        `json:"account_id"`
	AccountIBAN    *string      `json:"account_iban"`
	Date           string       `json:"date"`
	Amount         goldenMoney  `json:"amount"`
	BalanceAfter   *goldenMoney `json:"balance_after"`
	Currency       string       `json:"currency"`
	PartnerName    *string      `json:"partner_name"`
	Purpose        *string      `json:"purpose"`
	CategoryID     *int64       `json:"category_id"`
	CategoryName   *string      `json:"category_name"`
	CategoryColor  *string      `json:"category_color"`
	IsTaxRelevant  bool         `json:"is_tax_relevant"`
	DocumentCount  int          `json:"document_count"`
	IsNew          bool         `json:"is_new"`
	IsTransfer     bool         `json:"is_transfer"`
	TaxSource      string       `json:"tax_source"`
	TaxRulePattern *string      `json:"tax_rule_pattern"`
	TaxOverride    *bool        `json:"tax_override"`
}

type goldenFigures struct {
	Currency                string               `json:"currency"`
	TotalBalance            goldenMoney          `json:"total_balance"`
	AvailableBalance        goldenMoney          `json:"available_balance"`
	LoanBalance             goldenMoney          `json:"loan_balance"`
	CarryForward            *goldenMoney         `json:"carry_forward"`
	OtherMovements          *goldenMoney         `json:"other_movements"`
	BalanceDate             *string              `json:"balance_date"`
	HasLoans                bool                 `json:"has_loans"`
	OtherBalances           []goldenOtherBalance `json:"other_balances"`
	MonthlyIncome           goldenMoney          `json:"monthly_income"`
	MonthlyExpenses         goldenMoney          `json:"monthly_expenses"`
	UncategorizedCount      int                  `json:"uncategorized_count"`
	RecentTransactions      []goldenTransaction  `json:"recent_transactions"`
	ExpensesByCategory      []goldenChartSlice   `json:"expenses_by_category"`
	TaxDeductionsByCategory []goldenChartSlice   `json:"tax_deductions_by_category"`
	LastMonths              []goldenMonthBar     `json:"last_months"`
	Budgets                 []goldenBudget       `json:"budgets"`
	Upcoming                []goldenPayment      `json:"upcoming"`
	MonthProgress           float64              `json:"month_progress"`
	TransfersThisMonth      goldenMoney          `json:"transfers_this_month"`
	SelectedMonth           goldenYearMonth      `json:"selected_month"`
	LoadedMonth             goldenYearMonth      `json:"loaded_month"`
	EarliestMonth           goldenYearMonth      `json:"earliest_month"`
	CurrentMonth            goldenYearMonth      `json:"current_month"`
	HasAccounts             bool                 `json:"has_accounts"`
}

type goldenEntry struct {
	RequestedMonth goldenYearMonth `json:"requested_month"`
	Figures        goldenFigures   `json:"figures"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func goldenToday(t *testing.T) civil.Date {
	t.Helper()
	return date(t, 2026, time.October, 15)
}

func categoryByName(t *testing.T, d *db.Database, name string) model.CategoryID {
	t.Helper()
	all, err := categories.List(context.Background(), d)
	if err != nil {
		t.Fatalf("categories.List: %v", err)
	}
	for _, category := range all {
		if category.Name == name {
			return category.ID
		}
	}
	t.Fatalf("the category %q exists", name)
	return 0
}

// dashboardFixture builds the throwaway database the dashboard, report and
// export goldens share, step for step as Rust does.
func dashboardFixture(t *testing.T) *db.Database {
	t.Helper()
	ctx := context.Background()
	pass, err := secret.NewPassphrase([]byte(fixturePassphrase), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	d, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	checking, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        ptr("Checking"),
		IBAN:        ptr("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     amount(0),
	})
	if err != nil {
		t.Fatalf("the checking account: %v", err)
	}
	savings, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        ptr("Savings"),
		IBAN:        ptr("DE44500105175407324931"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeSavings,
		Balance:     amount(0),
	})
	if err != nil {
		t.Fatalf("the savings account: %v", err)
	}
	card, err := accounts.Create(ctx, d, model.NewAccount{
		Name:          ptr("Credit card"),
		AccountNumber: ptr("5412345678901234"),
		Currency:      money.EUR,
		AccountType:   model.AccountTypeCreditCard,
		Balance:       amount(0),
	})
	if err != nil {
		t.Fatalf("the card account: %v", err)
	}

	if _, err := categories.SeedDefaults(ctx, d, i18n.En); err != nil {
		t.Fatalf("the default categories: %v", err)
	}

	for _, entry := range []struct {
		name  string
		minor int64
	}{
		{name: "Groceries", minor: 40_000},
		{name: "Restaurants & cafés", minor: 15_000},
	} {
		id := categoryByName(t, d, entry.name)
		category, ok, err := categories.Get(ctx, d, id)
		if err != nil {
			t.Fatalf("category get: %v", err)
		}
		if !ok {
			t.Fatalf("the category %q exists", entry.name)
		}
		category.BudgetLimit = ptr(amount(entry.minor))
		if err := categories.Update(ctx, d, id, model.NewCategory{
			Name:          category.Name,
			Color:         category.Color,
			BudgetLimit:   category.BudgetLimit,
			IsTaxRelevant: category.IsTaxRelevant,
			IsTransfer:    category.IsTransfer,
		}); err != nil {
			t.Fatalf("the budget: %v", err)
		}
	}
	for _, name := range []string{"Health", "Taxes & fees"} {
		id := categoryByName(t, d, name)
		category, ok, err := categories.Get(ctx, d, id)
		if err != nil {
			t.Fatalf("category get: %v", err)
		}
		if !ok {
			t.Fatalf("the category %q exists", name)
		}
		if err := categories.Update(ctx, d, id, model.NewCategory{
			Name:          category.Name,
			Color:         category.Color,
			BudgetLimit:   category.BudgetLimit,
			IsTaxRelevant: true,
			IsTransfer:    category.IsTransfer,
		}); err != nil {
			t.Fatalf("the tax flag: %v", err)
		}
	}

	ruleBytes, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "inputs", "rules", "default_rules.tsv")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatalf("read the default rules: %v", err)
	}
	parsedRules, err := rules.ParseRuleFile(ruleBytes, i18n.En)
	if err != nil {
		t.Fatalf("the default rules parse: %v", err)
	}
	for _, row := range parsedRules.Rows {
		if _, err := rulesrepo.Create(ctx, d, model.NewRule{
			Pattern:    row.Pattern,
			CategoryID: categoryByName(t, d, row.Category),
			Priority:   row.Priority,
			AutoApply:  row.AutoApply,
			MatchField: row.Field,
			Direction:  row.Direction,
		}); err != nil {
			t.Fatalf("the default rule: %v", err)
		}
	}

	today := goldenToday(t)
	for _, file := range []struct {
		name    string
		account model.AccountID
	}{
		{name: "dashboard_checking.csv", account: checking},
		{name: "dashboard_savings.csv", account: savings},
		{name: "dashboard_card.csv", account: card},
	} {
		bytes, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "inputs", "import", file.name)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("read %s: %v", file.name, err)
		}
		statement, err := imports.Read(file.name, bytes, nil, today)
		if err != nil {
			t.Fatalf("the dashboard statement parses: %v", err)
		}
		if _, err := imports.Import(ctx, d, file.account, statement, true, today); err != nil {
			t.Fatalf("the dashboard statement imports: %v", err)
		}
	}
	return d
}

type goldenChecker struct {
	t      *testing.T
	prefix string
}

func (c *goldenChecker) fail(path, format string, args ...any) {
	c.t.Errorf("%s %s: %s", c.prefix, path, fmt.Sprintf(format, args...))
}

func (c *goldenChecker) money(path string, got money.Money, want goldenMoney) {
	if got.Minor != want.Minor {
		c.fail(path+".minor", "%d, want %d", got.Minor, want.Minor)
	}
	if got.Currency.String() != want.Currency {
		c.fail(path+".currency", "%q, want %q", got.Currency.String(), want.Currency)
	}
	if plain := money.FormatPlain(got); plain != want.Plain {
		c.fail(path+".plain", "%q, want %q", plain, want.Plain)
	}
}

func (c *goldenChecker) optionalMoney(path string, got *money.Money, want *goldenMoney) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", showMoney(got), showGoldenMoney(want))
		return
	}
	if got != nil {
		c.money(path, *got, *want)
	}
}

func (c *goldenChecker) yearMonth(path string, got YearMonth, want goldenYearMonth) {
	if got.Year != want.Year || got.Month != want.Month {
		c.fail(path, "{year: %d, month: %d}, want {year: %d, month: %d}",
			got.Year, got.Month, want.Year, want.Month)
	}
}

func (c *goldenChecker) optionalDate(path string, got *civil.Date, want *string) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", showDate(got), showString(want))
		return
	}
	if got != nil && got.String() != *want {
		c.fail(path, "%q, want %q", got.String(), *want)
	}
}

func (c *goldenChecker) optionalString(path string, got, want *string) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", showString(got), showString(want))
		return
	}
	if got != nil && *got != *want {
		c.fail(path, "%q, want %q", *got, *want)
	}
}

func (c *goldenChecker) optionalBool(path string, got, want *bool) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", showBool(got), showBool(want))
		return
	}
	if got != nil && *got != *want {
		c.fail(path, "%t, want %t", *got, *want)
	}
}

func (c *goldenChecker) optionalCategoryID(path string, got *model.CategoryID, want *int64) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", showCategoryID(got), showInt64(want))
		return
	}
	if got != nil && int64(*got) != *want {
		c.fail(path, "%d, want %d", int64(*got), *want)
	}
}

func (c *goldenChecker) chartSlices(path string, got []ChartSlice, want []goldenChartSlice) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		c.money(prefix+".amount", got[index].Amount, want[index].Amount)
		if got[index].Label != want[index].Label {
			c.fail(prefix+".label", "%q, want %q", got[index].Label, want[index].Label)
		}
		c.optionalString(prefix+".color", got[index].Color, want[index].Color)
		c.optionalCategoryID(prefix+".category_id", got[index].CategoryID, want[index].CategoryID)
	}
}

func (c *goldenChecker) monthBars(path string, got []MonthBar, want []goldenMonthBar) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if got[index].Year != want[index].Year {
			c.fail(prefix+".year", "%d, want %d", got[index].Year, want[index].Year)
		}
		if got[index].Month != want[index].Month {
			c.fail(prefix+".month", "%d, want %d", got[index].Month, want[index].Month)
		}
		c.money(prefix+".income", got[index].Income, want[index].Income)
		c.money(prefix+".expenses", got[index].Expenses, want[index].Expenses)
		if got[index].Partial != want[index].Partial {
			c.fail(prefix+".partial", "%t, want %t", got[index].Partial, want[index].Partial)
		}
	}
}

func (c *goldenChecker) budgets(path string, got []BudgetStatus, want []goldenBudget) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if int64(got[index].CategoryID) != want[index].CategoryID {
			c.fail(prefix+".category_id", "%d, want %d", int64(got[index].CategoryID), want[index].CategoryID)
		}
		if got[index].CategoryName != want[index].CategoryName {
			c.fail(prefix+".category_name", "%q, want %q", got[index].CategoryName, want[index].CategoryName)
		}
		if got[index].Color != want[index].Color {
			c.fail(prefix+".color", "%q, want %q", got[index].Color, want[index].Color)
		}
		c.money(prefix+".limit", got[index].Limit, want[index].Limit)
		c.money(prefix+".spent", got[index].Spent, want[index].Spent)
		if got[index].Ratio() != want[index].Ratio {
			c.fail(prefix+".ratio", "%v, want %v", got[index].Ratio(), want[index].Ratio)
		}
		c.money(prefix+".remaining", got[index].Remaining(), want[index].Remaining)
		if got[index].IsOver() != want[index].IsOver {
			c.fail(prefix+".is_over", "%t, want %t", got[index].IsOver(), want[index].IsOver)
		}
	}
}

func (c *goldenChecker) payments(path string, got []recurring.RecurringPayment, want []goldenPayment) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if got[index].Name != want[index].Name {
			c.fail(prefix+".name", "%q, want %q", got[index].Name, want[index].Name)
		}
		if string(got[index].Cadence) != want[index].Cadence {
			c.fail(prefix+".cadence", "%q, want %q", got[index].Cadence, want[index].Cadence)
		}
		c.money(prefix+".typical_amount", got[index].TypicalAmount, want[index].TypicalAmount)
		if got[index].FixedAmount != want[index].FixedAmount {
			c.fail(prefix+".fixed_amount", "%t, want %t", got[index].FixedAmount, want[index].FixedAmount)
		}
		if got[index].LastDate.String() != want[index].LastDate {
			c.fail(prefix+".last_date", "%q, want %q", got[index].LastDate.String(), want[index].LastDate)
		}
		if got[index].NextDate.String() != want[index].NextDate {
			c.fail(prefix+".next_date", "%q, want %q", got[index].NextDate.String(), want[index].NextDate)
		}
		if got[index].Occurrences != want[index].Occurrences {
			c.fail(prefix+".occurrences", "%d, want %d", got[index].Occurrences, want[index].Occurrences)
		}
		c.optionalString(prefix+".category_name", got[index].CategoryName, want[index].CategoryName)
		if got[index].Currency.String() != want[index].Currency {
			c.fail(prefix+".currency", "%q, want %q", got[index].Currency.String(), want[index].Currency)
		}
		if got[index].IsTransfer != want[index].IsTransfer {
			c.fail(prefix+".is_transfer", "%t, want %t", got[index].IsTransfer, want[index].IsTransfer)
		}
		if got[index].IsIncome() != want[index].IsIncome {
			c.fail(prefix+".is_income", "%t, want %t", got[index].IsIncome(), want[index].IsIncome)
		}
		c.money(prefix+".yearly_amount", got[index].YearlyAmount(), want[index].YearlyAmount)
	}
}

func (c *goldenChecker) transactions(path string, got []model.TransactionItem, want []goldenTransaction) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		item := got[index]
		expected := want[index]
		if int64(item.ID) != expected.ID {
			c.fail(prefix+".id", "%d, want %d", int64(item.ID), expected.ID)
		}
		if int64(item.AccountID) != expected.AccountID {
			c.fail(prefix+".account_id", "%d, want %d", int64(item.AccountID), expected.AccountID)
		}
		c.optionalString(prefix+".account_iban", item.AccountIBAN, expected.AccountIBAN)
		if item.Date.String() != expected.Date {
			c.fail(prefix+".date", "%q, want %q", item.Date.String(), expected.Date)
		}
		c.money(prefix+".amount", item.Amount, expected.Amount)
		c.optionalMoney(prefix+".balance_after", item.BalanceAfter, expected.BalanceAfter)
		if item.Currency.String() != expected.Currency {
			c.fail(prefix+".currency", "%q, want %q", item.Currency.String(), expected.Currency)
		}
		c.optionalString(prefix+".partner_name", item.PartnerName, expected.PartnerName)
		c.optionalString(prefix+".purpose", item.Purpose, expected.Purpose)
		c.optionalCategoryID(prefix+".category_id", item.CategoryID, expected.CategoryID)
		c.optionalString(prefix+".category_name", item.CategoryName, expected.CategoryName)
		c.optionalString(prefix+".category_color", item.CategoryColor, expected.CategoryColor)
		if item.IsTaxRelevant != expected.IsTaxRelevant {
			c.fail(prefix+".is_tax_relevant", "%t, want %t", item.IsTaxRelevant, expected.IsTaxRelevant)
		}
		if item.DocumentCount != expected.DocumentCount {
			c.fail(prefix+".document_count", "%d, want %d", item.DocumentCount, expected.DocumentCount)
		}
		if item.IsNew != expected.IsNew {
			c.fail(prefix+".is_new", "%t, want %t", item.IsNew, expected.IsNew)
		}
		if item.IsTransfer != expected.IsTransfer {
			c.fail(prefix+".is_transfer", "%t, want %t", item.IsTransfer, expected.IsTransfer)
		}
		if name := taxSourceName(item.TaxSource); name != expected.TaxSource {
			c.fail(prefix+".tax_source", "%q, want %q", name, expected.TaxSource)
		}
		c.optionalString(prefix+".tax_rule_pattern", item.TaxRulePattern, expected.TaxRulePattern)
		c.optionalBool(prefix+".tax_override", item.TaxOverride, expected.TaxOverride)
	}
}

func (c *goldenChecker) figures(got DashboardFigures, want goldenFigures) {
	if got.Currency.String() != want.Currency {
		c.fail("figures.currency", "%q, want %q", got.Currency.String(), want.Currency)
	}
	c.money("figures.total_balance", got.TotalBalance, want.TotalBalance)
	c.money("figures.available_balance", got.AvailableBalance, want.AvailableBalance)
	c.money("figures.loan_balance", got.LoanBalance, want.LoanBalance)
	c.optionalMoney("figures.carry_forward", got.CarryForward, want.CarryForward)
	c.optionalMoney("figures.other_movements", got.OtherMovements, want.OtherMovements)
	c.optionalDate("figures.balance_date", got.BalanceDate, want.BalanceDate)
	if got.HasLoans != want.HasLoans {
		c.fail("figures.has_loans", "%t, want %t", got.HasLoans, want.HasLoans)
	}
	if len(got.OtherBalances) != len(want.OtherBalances) {
		c.fail("figures.other_balances", "length = %d, want %d", len(got.OtherBalances), len(want.OtherBalances))
	} else {
		for index := range want.OtherBalances {
			prefix := fmt.Sprintf("figures.other_balances[%d]", index)
			if got.OtherBalances[index].Currency.String() != want.OtherBalances[index].Currency {
				c.fail(prefix+".currency", "%q, want %q",
					got.OtherBalances[index].Currency.String(), want.OtherBalances[index].Currency)
			}
			c.money(prefix+".amount", got.OtherBalances[index], want.OtherBalances[index].Amount)
		}
	}
	c.money("figures.monthly_income", got.MonthlyIncome, want.MonthlyIncome)
	c.money("figures.monthly_expenses", got.MonthlyExpenses, want.MonthlyExpenses)
	if got.UncategorizedCount != want.UncategorizedCount {
		c.fail("figures.uncategorized_count", "%d, want %d", got.UncategorizedCount, want.UncategorizedCount)
	}
	c.transactions("figures.recent_transactions", got.RecentTransactions, want.RecentTransactions)
	c.chartSlices("figures.expenses_by_category", got.ExpensesByCategory, want.ExpensesByCategory)
	c.chartSlices("figures.tax_deductions_by_category", got.TaxDeductionsByCategory, want.TaxDeductionsByCategory)
	c.monthBars("figures.last_months", got.LastMonths, want.LastMonths)
	c.budgets("figures.budgets", got.Budgets, want.Budgets)
	c.payments("figures.upcoming", got.Upcoming, want.Upcoming)
	if got.MonthProgress != want.MonthProgress {
		c.fail("figures.month_progress", "%v, want %v", got.MonthProgress, want.MonthProgress)
	}
	c.money("figures.transfers_this_month", got.TransfersThisMonth, want.TransfersThisMonth)
	c.yearMonth("figures.selected_month", got.SelectedMonth, want.SelectedMonth)
	c.yearMonth("figures.loaded_month", got.LoadedMonth, want.LoadedMonth)
	c.yearMonth("figures.earliest_month", got.EarliestMonth, want.EarliestMonth)
	c.yearMonth("figures.current_month", got.CurrentMonth, want.CurrentMonth)
	if got.HasAccounts != want.HasAccounts {
		c.fail("figures.has_accounts", "%t, want %t", got.HasAccounts, want.HasAccounts)
	}
}

func taxSourceName(source model.TaxSource) string {
	switch source {
	case model.TaxSourceManual:
		return "Manual"
	case model.TaxSourceRule:
		return "Rule"
	case model.TaxSourceCategory:
		return "Category"
	default:
		return "None"
	}
}

func showMoney(value *money.Money) string {
	if value == nil {
		return "nil"
	}
	return value.Currency.String() + " " + money.FormatPlain(*value)
}

func showGoldenMoney(value *goldenMoney) string {
	if value == nil {
		return "nil"
	}
	return value.Currency + " " + value.Plain
}

func showDate(value *civil.Date) string {
	if value == nil {
		return "nil"
	}
	return value.String()
}

func showString(value *string) string {
	if value == nil {
		return "nil"
	}
	return *value
}

func showBool(value *bool) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%t", *value)
}

func showCategoryID(value *model.CategoryID) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", int64(*value))
}

func showInt64(value *int64) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *value)
}

func TestDashboardMatchesTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "dashboard", "months.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenEntry
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}

	requestedMonths := []YearMonth{
		NewYearMonth(2026, 10),
		NewYearMonth(2026, 9),
		NewYearMonth(2026, 7),
		NewYearMonth(2026, 1),
	}
	if len(golden) != len(requestedMonths) {
		t.Fatalf("golden months = %d, want %d", len(golden), len(requestedMonths))
	}

	d := dashboardFixture(t)
	today := goldenToday(t)
	for index, requested := range requestedMonths {
		entry := golden[index]
		monthLabel := fmt.Sprintf("%04d-%02d", requested.Year, requested.Month)
		checker := &goldenChecker{t: t, prefix: fmt.Sprintf("months[%d] (%s)", index, monthLabel)}
		checker.yearMonth("requested_month", requested, entry.RequestedMonth)

		figures, err := LoadDashboard(context.Background(), d, today, requested)
		if err != nil {
			t.Fatalf("%s: LoadDashboard: %v", checker.prefix, err)
		}
		checker.figures(figures, entry.Figures)
	}
}
