package report

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/pdftext"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// --- golden shapes ----------------------------------------------------------

type goldenMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenNamedAmount struct {
	Name   string      `json:"name"`
	Amount goldenMoney `json:"amount"`
}

type goldenCategoryShare struct {
	CategoryID *int64      `json:"category_id"`
	Name       string      `json:"name"`
	Amount     goldenMoney `json:"amount"`
	Color      string      `json:"color"`
}

type goldenMonthlyData struct {
	Year     int         `json:"year"`
	Month    uint8       `json:"month"`
	Income   goldenMoney `json:"income"`
	Expenses goldenMoney `json:"expenses"`
	Net      goldenMoney `json:"net"`
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

type goldenExpenseItem struct {
	Description string      `json:"description"`
	Amount      goldenMoney `json:"amount"`
	Category    string      `json:"category"`
	Date        string      `json:"date"`
}

type goldenTaxReport struct {
	Year                    int                   `json:"year"`
	From                    string                `json:"from"`
	To                      string                `json:"to"`
	TotalIncome             goldenMoney           `json:"total_income"`
	TotalExpenses           goldenMoney           `json:"total_expenses"`
	NetIncome               goldenMoney           `json:"net_income"`
	ExpensesByCategory      []goldenNamedAmount   `json:"expenses_by_category"`
	TaxRelevantExpenses     []goldenNamedAmount   `json:"tax_relevant_expenses"`
	TaxRelevantTransactions []goldenTransaction   `json:"tax_relevant_transactions"`
	MonthlyBreakdown        []goldenMonthlyData   `json:"monthly_breakdown"`
	TopVendors              []goldenNamedAmount   `json:"top_vendors"`
	TransactionCount        int                   `json:"transaction_count"`
	CategoriesUsed          int                   `json:"categories_used"`
	Currency                string                `json:"currency"`
	OtherCurrencyBookings   int                   `json:"other_currency_bookings"`
	TransferTotal           goldenMoney           `json:"transfer_total"`
	CategoryShares          []goldenCategoryShare `json:"category_shares"`
	TaxRelevantTotal        goldenMoney           `json:"tax_relevant_total"`
	IsCalendarYear          bool                  `json:"is_calendar_year"`
	Title                   string                `json:"title"`
}

type goldenTaxGroup struct {
	CategoryID  *int64              `json:"category_id"`
	Name        string              `json:"name"`
	Color       string              `json:"color"`
	Bookings    []goldenTransaction `json:"bookings"`
	Income      goldenMoney         `json:"income"`
	Expenses    goldenMoney         `json:"expenses"`
	Net         goldenMoney         `json:"net"`
	WithReceipt int                 `json:"with_receipt"`
}

type goldenTaxBookings struct {
	From                  string           `json:"from"`
	To                    string           `json:"to"`
	Currency              string           `json:"currency"`
	IncomeGroups          []goldenTaxGroup `json:"income_groups"`
	DeductionGroups       []goldenTaxGroup `json:"deduction_groups"`
	BookingCount          int              `json:"booking_count"`
	OtherCurrencyBookings int              `json:"other_currency_bookings"`
	IncomeTotal           goldenMoney      `json:"income_total"`
	DeductionTotal        goldenMoney      `json:"deduction_total"`
	NetTotal              goldenMoney      `json:"net_total"`
	WithReceipt           int              `json:"with_receipt"`
	Title                 string           `json:"title"`
}

type goldenMonthlyReport struct {
	Year             int                 `json:"year"`
	Month            uint8               `json:"month"`
	TotalIncome      goldenMoney         `json:"total_income"`
	TotalExpenses    goldenMoney         `json:"total_expenses"`
	NetAmount        goldenMoney         `json:"net_amount"`
	ByCategory       []goldenNamedAmount `json:"by_category"`
	TopExpenses      []goldenExpenseItem `json:"top_expenses"`
	TransactionCount int                 `json:"transaction_count"`
}

type goldenReportEntry struct {
	Year   int             `json:"year"`
	Report goldenTaxReport `json:"report"`
}

type goldenTaxReportEntry struct {
	Year   int             `json:"year"`
	Report goldenTaxReport `json:"report"`
}

type goldenTaxBookingsEntry struct {
	Year     int               `json:"year"`
	Bookings goldenTaxBookings `json:"bookings"`
}

type goldenMonthlyEntry struct {
	Year   int                 `json:"year"`
	Month  uint8               `json:"month"`
	Report goldenMonthlyReport `json:"report"`
}

type goldenReports struct {
	Reports        []goldenReportEntry              `json:"reports"`
	TaxReports     []goldenTaxReportEntry           `json:"tax_reports"`
	TaxBookings    []goldenTaxBookingsEntry         `json:"tax_bookings"`
	MonthlyReports []goldenMonthlyEntry             `json:"monthly_reports"`
	SumByCategory  map[string][]goldenNamedAmount   `json:"sum_by_category"`
	CategoryShares map[string][]goldenCategoryShare `json:"category_shares"`
}

// --- export golden shapes ---------------------------------------------------

type goldenCSV struct {
	Language string `json:"language"`
	Text     string `json:"text"`
}

type goldenExtracted struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

type goldenPDFPage struct {
	WidthPt  float64 `json:"width_pt"`
	HeightPt float64 `json:"height_pt"`
}

type goldenPDFInfo struct {
	PageCount int             `json:"page_count"`
	Pages     []goldenPDFPage `json:"pages"`
}

type goldenReportPDFInfo struct {
	PageCount int `json:"page_count"`
}

type goldenTaxPDF struct {
	Text *goldenExtracted `json:"text"`
	Info *goldenPDFInfo   `json:"info"`
}

type goldenReportPDF struct {
	Text *goldenExtracted     `json:"text"`
	Info *goldenReportPDFInfo `json:"info"`
}

type goldenTaxXLSX struct {
	Sheets [][][]string `json:"sheets"`
}

type goldenExports struct {
	CSV       []goldenCSV     `json:"csv"`
	TaxXLSX   goldenTaxXLSX   `json:"tax_xlsx"`
	TaxPDF    goldenTaxPDF    `json:"tax_pdf"`
	ReportPDF goldenReportPDF `json:"report_pdf"`
}

// --- the golden checker -----------------------------------------------------

type goldenCheck struct {
	t      *testing.T
	prefix string
}

func (c *goldenCheck) fail(path, format string, args ...any) {
	c.t.Errorf("%s %s: %s", c.prefix, path, fmt.Sprintf(format, args...))
}

func (c *goldenCheck) money(path string, got money.Money, want goldenMoney) {
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

func (c *goldenCheck) optionalMoney(path string, got *money.Money, want *goldenMoney) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", got, want)
		return
	}
	if got != nil {
		c.money(path, *got, *want)
	}
}

func (c *goldenCheck) optionalString(path string, got, want *string) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", got, want)
		return
	}
	if got != nil && *got != *want {
		c.fail(path, "%q, want %q", *got, *want)
	}
}

func (c *goldenCheck) optionalBool(path string, got, want *bool) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", got, want)
		return
	}
	if got != nil && *got != *want {
		c.fail(path, "%t, want %t", *got, *want)
	}
}

func (c *goldenCheck) optionalCategoryID(path string, got *model.CategoryID, want *int64) {
	if (got == nil) != (want == nil) {
		c.fail(path, "%v, want %v", got, want)
		return
	}
	if got != nil && int64(*got) != *want {
		c.fail(path, "%d, want %d", int64(*got), *want)
	}
}

func (c *goldenCheck) namedAmounts(path string, got []NamedAmount, want []goldenNamedAmount) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		if got[index].Name != want[index].Name {
			c.fail(prefix+".name", "%q, want %q", got[index].Name, want[index].Name)
		}
		c.money(prefix+".amount", got[index].Amount, want[index].Amount)
	}
}

func (c *goldenCheck) shares(path string, got []CategoryShare, want []goldenCategoryShare) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		prefix := fmt.Sprintf("%s[%d]", path, index)
		c.optionalCategoryID(prefix+".category_id", got[index].CategoryID, want[index].CategoryID)
		if got[index].Name != want[index].Name {
			c.fail(prefix+".name", "%q, want %q", got[index].Name, want[index].Name)
		}
		c.money(prefix+".amount", got[index].Amount, want[index].Amount)
		if got[index].Color != want[index].Color {
			c.fail(prefix+".color", "%q, want %q", got[index].Color, want[index].Color)
		}
	}
}

func (c *goldenCheck) item(path string, got model.TransactionItem, want goldenTransaction) {
	if int64(got.ID) != want.ID {
		c.fail(path+".id", "%d, want %d", int64(got.ID), want.ID)
	}
	if int64(got.AccountID) != want.AccountID {
		c.fail(path+".account_id", "%d, want %d", int64(got.AccountID), want.AccountID)
	}
	c.optionalString(path+".account_iban", got.AccountIBAN, want.AccountIBAN)
	if got.Date.String() != want.Date {
		c.fail(path+".date", "%q, want %q", got.Date.String(), want.Date)
	}
	c.money(path+".amount", got.Amount, want.Amount)
	c.optionalMoney(path+".balance_after", got.BalanceAfter, want.BalanceAfter)
	if got.Currency.String() != want.Currency {
		c.fail(path+".currency", "%q, want %q", got.Currency.String(), want.Currency)
	}
	c.optionalString(path+".partner_name", got.PartnerName, want.PartnerName)
	c.optionalString(path+".purpose", got.Purpose, want.Purpose)
	c.optionalCategoryID(path+".category_id", got.CategoryID, want.CategoryID)
	c.optionalString(path+".category_name", got.CategoryName, want.CategoryName)
	c.optionalString(path+".category_color", got.CategoryColor, want.CategoryColor)
	if got.IsTaxRelevant != want.IsTaxRelevant {
		c.fail(path+".is_tax_relevant", "%t, want %t", got.IsTaxRelevant, want.IsTaxRelevant)
	}
	if got.DocumentCount != want.DocumentCount {
		c.fail(path+".document_count", "%d, want %d", got.DocumentCount, want.DocumentCount)
	}
	if got.IsNew != want.IsNew {
		c.fail(path+".is_new", "%t, want %t", got.IsNew, want.IsNew)
	}
	if got.IsTransfer != want.IsTransfer {
		c.fail(path+".is_transfer", "%t, want %t", got.IsTransfer, want.IsTransfer)
	}
	if name := taxSourceName(got.TaxSource); name != want.TaxSource {
		c.fail(path+".tax_source", "%q, want %q", name, want.TaxSource)
	}
	c.optionalString(path+".tax_rule_pattern", got.TaxRulePattern, want.TaxRulePattern)
	c.optionalBool(path+".tax_override", got.TaxOverride, want.TaxOverride)
}

func (c *goldenCheck) items(path string, got []model.TransactionItem, want []goldenTransaction) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		c.item(fmt.Sprintf("%s[%d]", path, index), got[index], want[index])
	}
}

func (c *goldenCheck) monthlyData(path string, got []MonthlyData, want []goldenMonthlyData) {
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
		c.money(prefix+".net", got[index].Net(), want[index].Net)
	}
}

func (c *goldenCheck) taxReport(path string, got TaxReport, want goldenTaxReport) {
	if got.Year != want.Year {
		c.fail(path+".year", "%d, want %d", got.Year, want.Year)
	}
	if got.From.String() != want.From {
		c.fail(path+".from", "%q, want %q", got.From.String(), want.From)
	}
	if got.To.String() != want.To {
		c.fail(path+".to", "%q, want %q", got.To.String(), want.To)
	}
	c.money(path+".total_income", got.TotalIncome, want.TotalIncome)
	c.money(path+".total_expenses", got.TotalExpenses, want.TotalExpenses)
	c.money(path+".net_income", got.NetIncome, want.NetIncome)
	c.namedAmounts(path+".expenses_by_category", got.ExpensesByCategory, want.ExpensesByCategory)
	c.namedAmounts(path+".tax_relevant_expenses", got.TaxRelevantExpenses, want.TaxRelevantExpenses)
	c.items(path+".tax_relevant_transactions", got.TaxRelevantTransactions, want.TaxRelevantTransactions)
	c.monthlyData(path+".monthly_breakdown", got.MonthlyBreakdown, want.MonthlyBreakdown)
	c.namedAmounts(path+".top_vendors", got.TopVendors, want.TopVendors)
	if got.TransactionCount != want.TransactionCount {
		c.fail(path+".transaction_count", "%d, want %d", got.TransactionCount, want.TransactionCount)
	}
	if got.CategoriesUsed != want.CategoriesUsed {
		c.fail(path+".categories_used", "%d, want %d", got.CategoriesUsed, want.CategoriesUsed)
	}
	if got.Currency.String() != want.Currency {
		c.fail(path+".currency", "%q, want %q", got.Currency.String(), want.Currency)
	}
	if got.OtherCurrencyBookings != want.OtherCurrencyBookings {
		c.fail(path+".other_currency_bookings", "%d, want %d", got.OtherCurrencyBookings, want.OtherCurrencyBookings)
	}
	c.money(path+".transfer_total", got.TransferTotal, want.TransferTotal)
	c.shares(path+".category_shares", got.CategoryShares, want.CategoryShares)
	c.money(path+".tax_relevant_total", got.TaxRelevantTotal(), want.TaxRelevantTotal)
	if got.IsCalendarYear() != want.IsCalendarYear {
		c.fail(path+".is_calendar_year", "%t, want %t", got.IsCalendarYear(), want.IsCalendarYear)
	}
	if title := got.Title(i18n.En); title != want.Title {
		c.fail(path+".title", "%q, want %q", title, want.Title)
	}
}

func (c *goldenCheck) taxGroup(path string, got TaxGroup, want goldenTaxGroup) {
	c.optionalCategoryID(path+".category_id", got.CategoryID, want.CategoryID)
	if got.Name != want.Name {
		c.fail(path+".name", "%q, want %q", got.Name, want.Name)
	}
	if got.Color != want.Color {
		c.fail(path+".color", "%q, want %q", got.Color, want.Color)
	}
	c.items(path+".bookings", got.Bookings, want.Bookings)
	c.money(path+".income", got.Income, want.Income)
	c.money(path+".expenses", got.Expenses, want.Expenses)
	c.money(path+".net", got.Net(), want.Net)
	if got.WithReceipt() != want.WithReceipt {
		c.fail(path+".with_receipt", "%d, want %d", got.WithReceipt(), want.WithReceipt)
	}
}

func (c *goldenCheck) taxGroups(path string, got []TaxGroup, want []goldenTaxGroup) {
	if len(got) != len(want) {
		c.fail(path, "length = %d, want %d", len(got), len(want))
		return
	}
	for index := range want {
		c.taxGroup(fmt.Sprintf("%s[%d]", path, index), got[index], want[index])
	}
}

func (c *goldenCheck) taxBookings(path string, got TaxBookings, want goldenTaxBookings) {
	if got.From.String() != want.From {
		c.fail(path+".from", "%q, want %q", got.From.String(), want.From)
	}
	if got.To.String() != want.To {
		c.fail(path+".to", "%q, want %q", got.To.String(), want.To)
	}
	if got.Currency.String() != want.Currency {
		c.fail(path+".currency", "%q, want %q", got.Currency.String(), want.Currency)
	}
	c.taxGroups(path+".income_groups", got.IncomeGroups, want.IncomeGroups)
	c.taxGroups(path+".deduction_groups", got.DeductionGroups, want.DeductionGroups)
	if got.BookingCount != want.BookingCount {
		c.fail(path+".booking_count", "%d, want %d", got.BookingCount, want.BookingCount)
	}
	if got.OtherCurrencyBookings != want.OtherCurrencyBookings {
		c.fail(path+".other_currency_bookings", "%d, want %d", got.OtherCurrencyBookings, want.OtherCurrencyBookings)
	}
	c.money(path+".income_total", got.IncomeTotal(), want.IncomeTotal)
	c.money(path+".deduction_total", got.DeductionTotal(), want.DeductionTotal)
	c.money(path+".net_total", got.NetTotal(), want.NetTotal)
	if got.WithReceipt() != want.WithReceipt {
		c.fail(path+".with_receipt", "%d, want %d", got.WithReceipt(), want.WithReceipt)
	}
	if title := got.Title(i18n.En); title != want.Title {
		c.fail(path+".title", "%q, want %q", title, want.Title)
	}
}

func (c *goldenCheck) monthlyReport(path string, got MonthlyReport, want goldenMonthlyReport) {
	if got.Year != want.Year {
		c.fail(path+".year", "%d, want %d", got.Year, want.Year)
	}
	if got.Month != want.Month {
		c.fail(path+".month", "%d, want %d", got.Month, want.Month)
	}
	c.money(path+".total_income", got.TotalIncome, want.TotalIncome)
	c.money(path+".total_expenses", got.TotalExpenses, want.TotalExpenses)
	c.money(path+".net_amount", got.NetAmount, want.NetAmount)
	c.namedAmounts(path+".by_category", got.ByCategory, want.ByCategory)
	if len(got.TopExpenses) != len(want.TopExpenses) {
		c.fail(path+".top_expenses", "length = %d, want %d", len(got.TopExpenses), len(want.TopExpenses))
	} else {
		for index := range want.TopExpenses {
			prefix := fmt.Sprintf("%s.top_expenses[%d]", path, index)
			if got.TopExpenses[index].Description != want.TopExpenses[index].Description {
				c.fail(prefix+".description", "%q, want %q",
					got.TopExpenses[index].Description, want.TopExpenses[index].Description)
			}
			c.money(prefix+".amount", got.TopExpenses[index].Amount, want.TopExpenses[index].Amount)
			if got.TopExpenses[index].Category != want.TopExpenses[index].Category {
				c.fail(prefix+".category", "%q, want %q",
					got.TopExpenses[index].Category, want.TopExpenses[index].Category)
			}
			if got.TopExpenses[index].Date.String() != want.TopExpenses[index].Date {
				c.fail(prefix+".date", "%q, want %q",
					got.TopExpenses[index].Date.String(), want.TopExpenses[index].Date)
			}
		}
	}
	if got.TransactionCount != want.TransactionCount {
		c.fail(path+".transaction_count", "%d, want %d", got.TransactionCount, want.TransactionCount)
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

// --- the shared database fixture -------------------------------------------

const goldenFixturePassphrase = "correct horse battery staple"

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
	return testDate(t, 2026, time.October, 15)
}

func goldenCategoryByName(t *testing.T, d *db.Database, name string) model.CategoryID {
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

func ptr[T any](value T) *T {
	return &value
}

// reportGoldenFixture builds the throwaway database the report and export
// goldens share, step for step as Rust does.
func reportGoldenFixture(t *testing.T) *db.Database {
	t.Helper()
	ctx := context.Background()
	pass, err := secret.NewPassphrase([]byte(goldenFixturePassphrase), secret.New)
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
		Balance:     eur(0),
	})
	if err != nil {
		t.Fatalf("the checking account: %v", err)
	}
	savings, err := accounts.Create(ctx, d, model.NewAccount{
		Name:        ptr("Savings"),
		IBAN:        ptr("DE44500105175407324931"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeSavings,
		Balance:     eur(0),
	})
	if err != nil {
		t.Fatalf("the savings account: %v", err)
	}
	card, err := accounts.Create(ctx, d, model.NewAccount{
		Name:          ptr("Credit card"),
		AccountNumber: ptr("5412345678901234"),
		Currency:      money.EUR,
		AccountType:   model.AccountTypeCreditCard,
		Balance:       eur(0),
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
		id := goldenCategoryByName(t, d, entry.name)
		category, ok, err := categories.Get(ctx, d, id)
		if err != nil {
			t.Fatalf("category get: %v", err)
		}
		if !ok {
			t.Fatalf("the category %q exists", entry.name)
		}
		category.BudgetLimit = ptr(eur(entry.minor))
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
		id := goldenCategoryByName(t, d, name)
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
			CategoryID: goldenCategoryByName(t, d, row.Category),
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
		content, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "inputs", "import", file.name)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("read %s: %v", file.name, err)
		}
		statement, err := imports.Read(file.name, content, nil, today)
		if err != nil {
			t.Fatalf("the dashboard statement parses: %v", err)
		}
		if _, err := imports.Import(ctx, d, file.account, statement, true, today); err != nil {
			t.Fatalf("the dashboard statement imports: %v", err)
		}
	}
	return d
}

// --- the report golden ------------------------------------------------------

func TestReportsMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "report", "reports.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenReports
	if err := json.Unmarshal(content, &golden); err != nil {
		t.Fatal(err)
	}

	d := reportGoldenFixture(t)
	items := allItems(t, d)
	years := []int{2025, 2026}
	if len(golden.Reports) != len(years) || len(golden.TaxReports) != len(years) || len(golden.TaxBookings) != len(years) {
		t.Fatalf(
			"golden year counts = %d/%d/%d, want %d",
			len(golden.Reports), len(golden.TaxReports), len(golden.TaxBookings), len(years),
		)
	}

	for index, year := range years {
		yearItems := make([]model.TransactionItem, 0)
		for _, item := range items {
			if item.Date.Year() == year {
				yearItems = append(yearItems, item)
			}
		}
		from := testDate(t, year, time.January, 1)
		to := testDate(t, year, time.December, 31)

		checker := &goldenCheck{t: t, prefix: fmt.Sprintf("reports[%d] (%d)", index, year)}
		checker.taxReport("report", BuildReport(from, to, yearItems, money.EUR, i18n.En), golden.Reports[index].Report)

		checker = &goldenCheck{t: t, prefix: fmt.Sprintf("tax_reports[%d] (%d)", index, year)}
		checker.taxReport("report", BuildTaxReport(year, yearItems, money.EUR, i18n.En), golden.TaxReports[index].Report)

		checker = &goldenCheck{t: t, prefix: fmt.Sprintf("tax_bookings[%d] (%d)", index, year)}
		checker.taxBookings("bookings", BuildTaxBookings(from, to, yearItems, money.EUR, i18n.En), golden.TaxBookings[index].Bookings)
	}

	months := []struct {
		year  int
		month uint8
	}{{2025, 12}, {2026, 10}}
	if len(golden.MonthlyReports) != len(months) {
		t.Fatalf("golden month count = %d, want %d", len(golden.MonthlyReports), len(months))
	}
	for index, entry := range months {
		monthItems := make([]model.TransactionItem, 0)
		for _, item := range items {
			if item.Date.Year() == entry.year && item.Date.Month() == time.Month(entry.month) {
				monthItems = append(monthItems, item)
			}
		}
		checker := &goldenCheck{t: t, prefix: fmt.Sprintf("monthly_reports[%d] (%d-%02d)", index, entry.year, entry.month)}
		checker.monthlyReport(
			"report",
			MonthlyReportOf(entry.year, entry.month, monthItems, money.EUR, i18n.En),
			golden.MonthlyReports[index].Report,
		)
	}

	spending := make([]model.TransactionItem, 0)
	for _, item := range items {
		if item.CountsAsSpending() {
			spending = append(spending, item)
		}
	}
	for _, lang := range []i18n.Language{i18n.En, i18n.De} {
		code := lang.Code()
		wantCategories, ok := golden.SumByCategory[code]
		if !ok {
			t.Fatalf("the golden has sum_by_category.%s", code)
		}
		checker := &goldenCheck{t: t, prefix: "sum_by_category." + code}
		checker.namedAmounts("entries", SumByCategory(spending, lang), wantCategories)

		wantShares, ok := golden.CategoryShares[code]
		if !ok {
			t.Fatalf("the golden has category_shares.%s", code)
		}
		checker = &goldenCheck{t: t, prefix: "category_shares." + code}
		checker.shares("entries", CategoryShares(spending, lang), wantShares)
	}
}

// --- the export golden ------------------------------------------------------

func TestExportsMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "export", "exports.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenExports
	if err := json.Unmarshal(content, &golden); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	d := reportGoldenFixture(t)
	from := testDate(t, 2026, time.January, 1)
	to := testDate(t, 2026, time.December, 31)
	today := goldenToday(t)
	temp := t.TempDir()

	languages := []i18n.Language{i18n.En, i18n.De}
	if len(golden.CSV) != len(languages) {
		t.Fatalf("golden CSV count = %d, want %d", len(golden.CSV), len(languages))
	}
	for index, lang := range languages {
		path := filepath.Join(temp, "transactions-"+lang.Code()+".csv")
		if err := ExportRangeToCSV(ctx, d, from, to, path, lang); err != nil {
			t.Fatalf("%s: ExportRangeToCSV: %v", lang.Code(), err)
		}
		exported, err := os.ReadFile(path) // #nosec G304 -- the test's own temp path.
		if err != nil {
			t.Fatalf("%s: read the CSV: %v", lang.Code(), err)
		}
		entry := golden.CSV[index]
		if entry.Language != lang.Code() {
			t.Fatalf("golden CSV[%d].language = %q, want %q", index, entry.Language, lang.Code())
		}
		if string(exported) != entry.Text {
			t.Fatalf(
				"%s: the CSV differs from the Rust golden\n got: %q\nwant: %q",
				lang.Code(), string(exported), entry.Text,
			)
		}
	}

	items := allItems(t, d)
	yearItems := make([]model.TransactionItem, 0)
	for _, item := range items {
		if item.Date.Year() == 2026 {
			yearItems = append(yearItems, item)
		}
	}
	bookings := BuildTaxBookings(from, to, yearItems, money.EUR, i18n.En)
	exporter := TaxReportExporter{}

	xlsxPath := filepath.Join(temp, "tax-bookings.xlsx")
	if err := exporter.ExportXLSX(&bookings, xlsxPath, i18n.En); err != nil {
		t.Fatalf("the tax XLSX export: %v", err)
	}
	xlsxBytes, err := os.ReadFile(xlsxPath) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the tax XLSX: %v", err)
	}
	if len(golden.TaxXLSX.Sheets) != 2 {
		t.Fatalf("golden sheets = %d, want 2", len(golden.TaxXLSX.Sheets))
	}
	for sheetIndex, want := range golden.TaxXLSX.Sheets {
		rows := xlsxSheetRows(t, xlsxBytes, sheetIndex)
		if len(rows) != len(want) {
			t.Fatalf("sheet %d row count = %d, want %d", sheetIndex, len(rows), len(want))
		}
		for rowIndex := range want {
			if !slices.Equal(rows[rowIndex], want[rowIndex]) {
				t.Errorf(
					"sheet %d row %d = %v, want %v",
					sheetIndex, rowIndex, rows[rowIndex], want[rowIndex],
				)
			}
		}
	}

	pdfBytes := exporter.BuildPDF(&bookings, today, i18n.En)
	pdfPath := filepath.Join(temp, "tax-bookings.pdf")
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		t.Fatalf("write the tax PDF: %v", err)
	}
	pdfText, ok := pdftext.Extract(pdfPath, 100)
	if !ok || pdfText == nil {
		t.Fatal("the tax PDF has no text layer")
	}
	if golden.TaxPDF.Text == nil {
		t.Fatal("the golden has no tax PDF text")
	}
	if !sameWords(pdfText.Text, golden.TaxPDF.Text.Text) {
		t.Errorf("tax PDF text:\n got: %q\nwant: %q", pdfText.Text, golden.TaxPDF.Text.Text)
	}
	if pdfText.PageCount != golden.TaxPDF.Text.PageCount {
		t.Errorf("tax PDF page count = %d, want %d", pdfText.PageCount, golden.TaxPDF.Text.PageCount)
	}
	pdfInfo, ok := pdftext.Inspect(pdfPath)
	if !ok || pdfInfo == nil {
		t.Fatal("the tax PDF has no page info")
	}
	if golden.TaxPDF.Info == nil {
		t.Fatal("the golden has no tax PDF info")
	}
	if pdfInfo.PageCount != golden.TaxPDF.Info.PageCount {
		t.Errorf("tax PDF info page count = %d, want %d", pdfInfo.PageCount, golden.TaxPDF.Info.PageCount)
	}
	if len(pdfInfo.Pages) != len(golden.TaxPDF.Info.Pages) {
		t.Fatalf("tax PDF page sizes = %d, want %d", len(pdfInfo.Pages), len(golden.TaxPDF.Info.Pages))
	}
	for index, page := range pdfInfo.Pages {
		if page.WidthPt != golden.TaxPDF.Info.Pages[index].WidthPt {
			t.Errorf("tax PDF page %d width = %v, want %v", index, page.WidthPt, golden.TaxPDF.Info.Pages[index].WidthPt)
		}
		if page.HeightPt != golden.TaxPDF.Info.Pages[index].HeightPt {
			t.Errorf("tax PDF page %d height = %v, want %v", index, page.HeightPt, golden.TaxPDF.Info.Pages[index].HeightPt)
		}
	}

	report := BuildReport(from, to, yearItems, money.EUR, i18n.En)
	reportBytes := (PdfReportExporter{}).BuildPDF(&report, today, i18n.En)
	reportPath := filepath.Join(temp, "report.pdf")
	if err := os.WriteFile(reportPath, reportBytes, 0o600); err != nil {
		t.Fatalf("write the report PDF: %v", err)
	}
	reportText, ok := pdftext.Extract(reportPath, 100)
	if !ok || reportText == nil {
		t.Fatal("the report PDF has no text layer")
	}
	if golden.ReportPDF.Text == nil {
		t.Fatal("the golden has no report PDF text")
	}
	if !sameWords(reportText.Text, golden.ReportPDF.Text.Text) {
		t.Errorf("report PDF text:\n got: %q\nwant: %q", reportText.Text, golden.ReportPDF.Text.Text)
	}
	if reportText.PageCount != golden.ReportPDF.Text.PageCount {
		t.Errorf("report PDF page count = %d, want %d", reportText.PageCount, golden.ReportPDF.Text.PageCount)
	}
	reportInfo, ok := pdftext.Inspect(reportPath)
	if !ok || reportInfo == nil {
		t.Fatal("the report PDF has no page info")
	}
	if golden.ReportPDF.Info == nil {
		t.Fatal("the golden has no report PDF info")
	}
	if reportInfo.PageCount != golden.ReportPDF.Info.PageCount {
		t.Errorf("report PDF info page count = %d, want %d", reportInfo.PageCount, golden.ReportPDF.Info.PageCount)
	}
}

// sameWords compares two text layers as ordered words. Go's pdftext reader
// ignores the Td operator this writer uses for positioning, so it returns the
// page's words as one row; Rust's pdf-extract keeps the line breaks. The words
// themselves and their order are what the export promises.
func sameWords(got, want string) bool {
	return slices.Equal(strings.Fields(got), strings.Fields(want))
}
