// Reports ported from FinDesk's document/ReportService.kt, via
// crates/finglass-core/src/report/service.rs.
//
// The calculations are pure: they take already-loaded bookings and return
// report data with integer minor-unit amounts. Totals never mix currencies;
// everything is in the report currency and bookings in other currencies are
// only counted.
package report

import (
	"context"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// UncategorizedLabel is the neutral label for bookings without a category.
const UncategorizedLabel = "Uncategorized"

// NamedAmount is a display name and the amount accounted for it.
type NamedAmount struct {
	// Name is the display name.
	Name string
	// Amount is the amount accounted for.
	Amount money.Money
}

// CategoryShare is one share of the spending split.
type CategoryShare struct {
	// CategoryID is the category's id, or nil for uncategorized.
	CategoryID *model.CategoryID
	// Name is the display name.
	Name string
	// Amount is the amount accounted for.
	Amount money.Money
	// Color is the category's color, or an empty string.
	Color string
}

// MonthlyData is one month of the tax report.
type MonthlyData struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month uint8
	// Income is money in.
	Income money.Money
	// Expenses is money out, as a positive amount.
	Expenses money.Money
}

// Net is income minus expenses.
func (m MonthlyData) Net() money.Money {
	return amountOf(m.Income.Minor-m.Expenses.Minor, m.Income.Currency)
}

// TaxReport is the annual or range tax report.
type TaxReport struct {
	// Year is the start year.
	Year int
	// From is the first day, inclusive.
	From civil.Date
	// To is the last day, inclusive.
	To civil.Date
	// TotalIncome is money in.
	TotalIncome money.Money
	// TotalExpenses is money out, positive.
	TotalExpenses money.Money
	// NetIncome is income minus expenses.
	NetIncome money.Money
	// ExpensesByCategory is expenses grouped by category name, largest first.
	ExpensesByCategory []NamedAmount
	// TaxRelevantExpenses is the Tax bookings tab's deduction groups: each
	// category name at its absolute net.
	TaxRelevantExpenses []NamedAmount
	// TaxRelevantTransactions is the tax-relevant bookings.
	TaxRelevantTransactions []model.TransactionItem
	// MonthlyBreakdown is every month in the range.
	MonthlyBreakdown []MonthlyData
	// TopVendors is the biggest vendors, largest first.
	TopVendors []NamedAmount
	// TransactionCount is how many bookings are in the report currency.
	TransactionCount int
	// CategoriesUsed is how many distinct categories the bookings use.
	CategoriesUsed int
	// Currency is the currency of every amount.
	Currency money.Currency
	// OtherCurrencyBookings is bookings in the period in another currency, not
	// included in any total.
	OtherCurrencyBookings int
	// TransferTotal is money moved between the user's own accounts.
	TransferTotal money.Money
	// CategoryShares is the spending split behind ExpensesByCategory.
	CategoryShares []CategoryShare
}

// TaxRelevantTotal returns the sum of the deduction groups.
func (r TaxReport) TaxRelevantTotal() money.Money {
	var total int64
	for _, entry := range r.TaxRelevantExpenses {
		total += entry.Amount.Minor
	}
	return amountOf(total, r.Currency)
}

// IsCalendarYear reports whether the report covers a full calendar year.
func (r TaxReport) IsCalendarYear() bool {
	return isCalendarYear(r.From, r.To, r.Year)
}

// Title returns the report title: the year, or the date range for a custom
// period.
func (r TaxReport) Title(_ i18n.Language) string {
	if r.IsCalendarYear() {
		return formatInt(r.Year)
	}
	return formatDate(r.From) + " – " + formatDate(r.To)
}

// EmptyTaxReport returns an empty report for a year, in euros like Kotlin's
// TaxReport.empty.
func EmptyTaxReport(year int) TaxReport {
	from := mustDate(year, 1, 1)
	to := mustDate(year, 12, 31)
	return TaxReport{
		Year:                    year,
		From:                    from,
		To:                      to,
		TotalIncome:             amountOf(0, money.EUR),
		TotalExpenses:           amountOf(0, money.EUR),
		NetIncome:               amountOf(0, money.EUR),
		ExpensesByCategory:      []NamedAmount{},
		TaxRelevantExpenses:     []NamedAmount{},
		TaxRelevantTransactions: []model.TransactionItem{},
		MonthlyBreakdown:        []MonthlyData{},
		TopVendors:              []NamedAmount{},
		Currency:                money.EUR,
		TransferTotal:           amountOf(0, money.EUR),
		CategoryShares:          []CategoryShare{},
	}
}

// MonthlyReport is the monthly spending report.
type MonthlyReport struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month uint8
	// TotalIncome is money in.
	TotalIncome money.Money
	// TotalExpenses is money out, positive.
	TotalExpenses money.Money
	// NetAmount is income minus expenses.
	NetAmount money.Money
	// ByCategory is expenses by category name, sorted by name.
	ByCategory []NamedAmount
	// TopExpenses is the ten biggest expenses.
	TopExpenses []ExpenseItem
	// TransactionCount is how many bookings are in the month.
	TransactionCount int
}

// ExpenseItem is one expense of the monthly report.
type ExpenseItem struct {
	// Description is the display title.
	Description string
	// Amount is the absolute amount.
	Amount money.Money
	// Category is the category name, or the uncategorized label.
	Category string
	// Date is the booking date.
	Date civil.Date
}

// MonthlyAmount is one month of a category trend.
type MonthlyAmount struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month uint8
	// Amount is the absolute amount in that month.
	Amount money.Money
}

// CategoryTrend is a category's spending trend.
type CategoryTrend struct {
	// CategoryID is the category id.
	CategoryID model.CategoryID
	// CategoryName is the category name, or the localized unknown label.
	CategoryName string
	// MonthlyData is every requested month.
	MonthlyData []MonthlyAmount
	// AverageMonthly is the average monthly amount.
	AverageMonthly money.Money
	// Trend is the latest month against the average, in percent, one decimal.
	Trend decimal.Decimal
}

// TaxGroup is one category's tax bookings of a period.
type TaxGroup struct {
	// CategoryID is the category id, or nil for uncategorized.
	CategoryID *model.CategoryID
	// Name is the display name.
	Name string
	// Color is the category's color.
	Color string
	// Bookings is the group's bookings, date ascending, then id.
	Bookings []model.TransactionItem
	// Income is money in, signed.
	Income money.Money
	// Expenses is money out, positive.
	Expenses money.Money
}

// Net is income minus expenses.
func (g TaxGroup) Net() money.Money {
	return amountOf(g.Income.Minor-g.Expenses.Minor, g.Income.Currency)
}

// WithReceipt counts the bookings carrying at least one linked receipt.
func (g TaxGroup) WithReceipt() int {
	count := 0
	for _, booking := range g.Bookings {
		if booking.DocumentCount > 0 {
			count++
		}
	}
	return count
}

// TaxBookings is the Tax bookings view over a period.
type TaxBookings struct {
	// From is the first day, inclusive.
	From civil.Date
	// To is the last day, inclusive.
	To civil.Date
	// Currency is the currency of every amount.
	Currency money.Currency
	// IncomeGroups holds categories whose net is non-negative.
	IncomeGroups []TaxGroup
	// DeductionGroups holds categories whose net is negative.
	DeductionGroups []TaxGroup
	// BookingCount is how many tax-relevant bookings are in the report
	// currency.
	BookingCount int
	// OtherCurrencyBookings is tax-relevant bookings in another currency; not
	// included in any total.
	OtherCurrencyBookings int
}

// IncomeTotal returns the sum of the income groups' nets.
func (b TaxBookings) IncomeTotal() money.Money {
	var total int64
	for _, group := range b.IncomeGroups {
		total += group.Net().Minor
	}
	return amountOf(total, b.Currency)
}

// DeductionTotal returns the sum of the deduction groups' absolute nets.
func (b TaxBookings) DeductionTotal() money.Money {
	var total int64
	for _, group := range b.DeductionGroups {
		total += absClamped(group.Net().Minor)
	}
	return amountOf(total, b.Currency)
}

// WithReceipt counts bookings with a receipt, across both sections.
func (b TaxBookings) WithReceipt() int {
	total := 0
	for _, group := range b.IncomeGroups {
		total += group.WithReceipt()
	}
	for _, group := range b.DeductionGroups {
		total += group.WithReceipt()
	}
	return total
}

// NetTotal is income total minus deduction total.
func (b TaxBookings) NetTotal() money.Money {
	return amountOf(b.IncomeTotal().Minor-b.DeductionTotal().Minor, b.Currency)
}

// Title returns "2026" for a full calendar year, otherwise the date range.
func (b TaxBookings) Title(_ i18n.Language) string {
	calendarYear := b.From.Year() == b.To.Year() &&
		b.From == mustDate(b.From.Year(), 1, 1) &&
		b.To == mustDate(b.From.Year(), 12, 31)
	if calendarYear {
		return formatInt(b.From.Year())
	}
	return formatDate(b.From) + " – " + formatDate(b.To)
}

func amountOf(minor int64, currency money.Currency) money.Money {
	return money.Money{Minor: minor, Currency: currency}
}

// absClamped returns the unsigned absolute value clamped to i64::MAX, like
// Rust's i64::try_from(minor.unsigned_abs()).unwrap_or(i64::MAX).
func absClamped(minor int64) int64 {
	if minor >= 0 {
		return minor
	}
	if minor == math.MinInt64 {
		return math.MaxInt64
	}
	return -minor
}

// saturatingAddInt64 adds two integers, clamping at the i64 bounds like Rust's
// saturating_add.
func saturatingAddInt64(left, right int64) int64 {
	sum := left + right
	if right > 0 && sum < left {
		return math.MaxInt64
	}
	if right < 0 && sum > left {
		return math.MinInt64
	}
	return sum
}

func sumSigned(values []int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}

func sumAbs(values []int64) int64 {
	var total int64
	for _, value := range values {
		total += absClamped(value)
	}
	return total
}

// sepaTagPattern lists SEPA structured-field tags, from FinDesk's
// BankText.TAG.
var sepaTagPattern = regexp.MustCompile(
	`(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\+`,
)

func mustDate(year, month, day int) civil.Date {
	date, ok := civil.New(year, time.Month(month), day)
	if !ok {
		panic("valid date")
	}
	return date
}

// monthNumber narrows a dashboard month (1..12) to the report's u8 field
// without an unchecked integer conversion.
func monthNumber(month int) uint8 {
	months := [...]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	if month < 1 || month > 12 {
		panic("month out of range")
	}
	return months[month]
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

func formatDate(date civil.Date) string {
	return format.Date(date)
}

func isCalendarYear(from, to civil.Date, year int) bool {
	return from == mustDate(year, 1, 1) && to == mustDate(year, 12, 31)
}

// BuildReport builds the annual or range tax report.
func BuildReport(
	from civil.Date,
	to civil.Date,
	all []model.TransactionItem,
	currency money.Currency,
	lang i18n.Language,
) TaxReport {
	transactionsInCurrency := make([]*model.TransactionItem, 0, len(all))
	for index := range all {
		if all[index].Currency == currency {
			transactionsInCurrency = append(transactionsInCurrency, &all[index])
		}
	}
	otherCurrency := len(all) - len(transactionsInCurrency)

	income := make([]*model.TransactionItem, 0)
	expenses := make([]*model.TransactionItem, 0)
	for _, item := range transactionsInCurrency {
		if item.CountsAsIncome() {
			income = append(income, item)
		}
		if item.CountsAsSpending() {
			expenses = append(expenses, item)
		}
	}

	incomeAmounts := make([]int64, 0, len(income))
	for _, item := range income {
		incomeAmounts = append(incomeAmounts, item.Amount.Minor)
	}
	expenseAmounts := make([]int64, 0, len(expenses))
	for _, item := range expenses {
		expenseAmounts = append(expenseAmounts, item.Amount.Minor)
	}

	totalIncome := sumSigned(incomeAmounts)
	totalExpenses := sumAbs(expenseAmounts)

	taxRelevant := make([]model.TransactionItem, 0)
	for _, item := range transactionsInCurrency {
		if item.IsExpense() && item.IsTaxRelevant {
			taxRelevant = append(taxRelevant, *item)
		}
	}

	bookings := BuildTaxBookings(from, to, all, currency, lang)
	taxRelevantExpenses := make([]NamedAmount, 0, len(bookings.DeductionGroups))
	for _, group := range bookings.DeductionGroups {
		taxRelevantExpenses = append(
			taxRelevantExpenses,
			NamedAmount{Name: group.Name, Amount: amountOf(absClamped(group.Net().Minor), currency)},
		)
	}

	startMonth := dashboard.YearMonthFromDate(from)
	endMonth := dashboard.YearMonthFromDate(to)
	monthlyBreakdown := make([]MonthlyData, 0)
	for month := startMonth; month.Compare(endMonth) <= 0; month = month.PlusMonths(1) {
		monthIncome := make([]int64, 0)
		monthExpenses := make([]int64, 0)
		for _, item := range transactionsInCurrency {
			if item.Date.Year() != month.Year || int(item.Date.Month()) != month.Month {
				continue
			}
			if item.CountsAsIncome() {
				monthIncome = append(monthIncome, item.Amount.Minor)
			}
			if item.CountsAsSpending() {
				monthExpenses = append(monthExpenses, item.Amount.Minor)
			}
		}
		monthlyBreakdown = append(monthlyBreakdown, MonthlyData{
			Year:     month.Year,
			Month:    monthNumber(month.Month),
			Income:   amountOf(sumSigned(monthIncome), currency),
			Expenses: amountOf(sumAbs(monthExpenses), currency),
		})
	}

	categoriesUsed := make(map[model.CategoryID]struct{})
	for _, item := range transactionsInCurrency {
		if item.CategoryID != nil {
			categoriesUsed[*item.CategoryID] = struct{}{}
		}
	}

	transferAmounts := make([]int64, 0)
	for _, item := range transactionsInCurrency {
		if item.IsTransfer && item.Amount.Minor < 0 {
			transferAmounts = append(transferAmounts, item.Amount.Minor)
		}
	}
	transferTotal := sumAbs(transferAmounts)

	return TaxReport{
		Year:                    from.Year(),
		From:                    from,
		To:                      to,
		TotalIncome:             amountOf(totalIncome, currency),
		TotalExpenses:           amountOf(totalExpenses, currency),
		NetIncome:               amountOf(totalIncome-totalExpenses, currency),
		ExpensesByCategory:      sumByCategoryRefs(expenses, lang),
		TaxRelevantExpenses:     taxRelevantExpenses,
		TaxRelevantTransactions: taxRelevant,
		MonthlyBreakdown:        monthlyBreakdown,
		TopVendors:              topVendorsRefs(expenses, 5),
		TransactionCount:        len(transactionsInCurrency),
		CategoriesUsed:          len(categoriesUsed),
		Currency:                currency,
		OtherCurrencyBookings:   otherCurrency,
		TransferTotal:           amountOf(transferTotal, currency),
		CategoryShares:          categorySharesRefs(expenses, lang),
	}
}

// BuildTaxReport builds the annual tax report.
func BuildTaxReport(
	year int,
	transactions []model.TransactionItem,
	currency money.Currency,
	lang i18n.Language,
) TaxReport {
	return BuildReport(mustDate(year, 1, 1), mustDate(year, 12, 31), transactions, currency, lang)
}

// BuildTaxBookings builds the pure Tax bookings view over a period.
func BuildTaxBookings(
	from civil.Date,
	to civil.Date,
	all []model.TransactionItem,
	currency money.Currency,
	lang i18n.Language,
) TaxBookings {
	relevant := make([]*model.TransactionItem, 0)
	for index := range all {
		if all[index].IsTaxRelevant {
			relevant = append(relevant, &all[index])
		}
	}
	inCurrency := make([]*model.TransactionItem, 0, len(relevant))
	for _, item := range relevant {
		if item.Currency == currency {
			inCurrency = append(inCurrency, item)
		}
	}
	otherCurrency := len(relevant) - len(inCurrency)

	type categoryGroup struct {
		categoryID *model.CategoryID
		bookings   []*model.TransactionItem
	}
	grouped := make([]categoryGroup, 0)
	for _, booking := range inCurrency {
		found := false
		for index := range grouped {
			if sameCategoryID(grouped[index].categoryID, booking.CategoryID) {
				grouped[index].bookings = append(grouped[index].bookings, booking)
				found = true
				break
			}
		}
		if !found {
			grouped = append(grouped, categoryGroup{
				categoryID: booking.CategoryID,
				bookings:   []*model.TransactionItem{booking},
			})
		}
	}

	uncategorized := i18n.Text(lang, i18n.Uncategorized)
	groups := make([]TaxGroup, 0, len(grouped))
	for _, entry := range grouped {
		first := entry.bookings[0]
		name := uncategorized
		if first.CategoryName != nil {
			name = *first.CategoryName
		}
		color := ""
		if first.CategoryColor != nil {
			color = *first.CategoryColor
		}
		incomeAmounts := make([]int64, 0)
		expenseAmounts := make([]int64, 0)
		for _, item := range entry.bookings {
			if item.Amount.Minor > 0 {
				incomeAmounts = append(incomeAmounts, item.Amount.Minor)
			}
			if item.Amount.Minor < 0 {
				expenseAmounts = append(expenseAmounts, item.Amount.Minor)
			}
		}
		bookings := make([]model.TransactionItem, 0, len(entry.bookings))
		for _, item := range entry.bookings {
			bookings = append(bookings, *item)
		}
		sort.SliceStable(bookings, func(left, right int) bool {
			return compareTransactionTuple(bookings[left], bookings[right]) < 0
		})
		groups = append(groups, TaxGroup{
			CategoryID: entry.categoryID,
			Name:       name,
			Color:      color,
			Bookings:   bookings,
			Income:     amountOf(sumSigned(incomeAmounts), currency),
			Expenses:   amountOf(sumAbs(expenseAmounts), currency),
		})
	}

	sort.SliceStable(groups, func(left, right int) bool {
		leftNet := absClamped(groups[left].Net().Minor)
		rightNet := absClamped(groups[right].Net().Minor)
		if leftNet != rightNet {
			return leftNet > rightNet
		}
		return groups[left].Name < groups[right].Name
	})

	incomeGroups := make([]TaxGroup, 0)
	deductionGroups := make([]TaxGroup, 0)
	for _, group := range groups {
		if group.Net().Minor >= 0 {
			incomeGroups = append(incomeGroups, group)
		} else {
			deductionGroups = append(deductionGroups, group)
		}
	}

	return TaxBookings{
		From:                  from,
		To:                    to,
		Currency:              currency,
		IncomeGroups:          incomeGroups,
		DeductionGroups:       deductionGroups,
		BookingCount:          len(inCurrency),
		OtherCurrencyBookings: otherCurrency,
	}
}

// MonthlyReportOf builds the monthly spending report over already-filtered
// bookings.
func MonthlyReportOf(
	year int,
	month uint8,
	transactions []model.TransactionItem,
	currency money.Currency,
	lang i18n.Language,
) MonthlyReport {
	income := make([]int64, 0)
	expenseTransactions := make([]*model.TransactionItem, 0)
	for index := range transactions {
		item := &transactions[index]
		if item.CountsAsIncome() {
			income = append(income, item.Amount.Minor)
		}
		if item.CountsAsSpending() {
			expenseTransactions = append(expenseTransactions, item)
		}
	}

	totalIncome := sumSigned(income)
	expenseAmounts := make([]int64, 0, len(expenseTransactions))
	for _, item := range expenseTransactions {
		expenseAmounts = append(expenseAmounts, item.Amount.Minor)
	}
	totalExpenses := sumAbs(expenseAmounts)

	sorted := make([]*model.TransactionItem, len(expenseTransactions))
	copy(sorted, expenseTransactions)
	sort.SliceStable(sorted, func(left, right int) bool {
		return sorted[left].Amount.Minor < sorted[right].Amount.Minor
	})
	uncategorized := i18n.Text(lang, i18n.Uncategorized)
	topExpenses := make([]ExpenseItem, 0)
	for index, item := range sorted {
		if index >= 10 {
			break
		}
		category := uncategorized
		if item.CategoryName != nil {
			category = *item.CategoryName
		}
		topExpenses = append(topExpenses, ExpenseItem{
			Description: DisplayDescription(item),
			Amount:      amountOf(absClamped(item.Amount.Minor), item.Currency),
			Category:    category,
			Date:        item.Date,
		})
	}

	byCategory := sumByCategoryRefs(expenseTransactions, lang)
	sort.SliceStable(byCategory, func(left, right int) bool {
		return byCategory[left].Name < byCategory[right].Name
	})

	return MonthlyReport{
		Year:             year,
		Month:            month,
		TotalIncome:      amountOf(totalIncome, currency),
		TotalExpenses:    amountOf(totalExpenses, currency),
		NetAmount:        amountOf(totalIncome-totalExpenses, currency),
		ByCategory:       byCategory,
		TopExpenses:      topExpenses,
		TransactionCount: len(transactions),
	}
}

// SumByCategory groups expenses by category name, largest first.
func SumByCategory(transactions []model.TransactionItem, lang i18n.Language) []NamedAmount {
	refs := make([]*model.TransactionItem, 0, len(transactions))
	for index := range transactions {
		refs = append(refs, &transactions[index])
	}
	return sumByCategoryRefs(refs, lang)
}

func sumByCategoryRefs(transactions []*model.TransactionItem, lang i18n.Language) []NamedAmount {
	uncategorized := i18n.Text(lang, i18n.Uncategorized)
	type group struct {
		name     string
		amount   int64
		currency money.Currency
	}
	groups := make([]group, 0)
	for _, item := range transactions {
		name := uncategorized
		if item.CategoryName != nil {
			name = *item.CategoryName
		}
		amount := absClamped(item.Amount.Minor)
		found := false
		for index := range groups {
			if groups[index].name == name {
				groups[index].amount = saturatingAddInt64(groups[index].amount, amount)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, group{name: name, amount: amount, currency: item.Currency})
		}
	}
	sort.SliceStable(groups, func(left, right int) bool {
		return groups[left].amount > groups[right].amount
	})
	result := make([]NamedAmount, 0, len(groups))
	for _, entry := range groups {
		result = append(result, NamedAmount{Name: entry.name, Amount: amountOf(entry.amount, entry.currency)})
	}
	return result
}

// CategoryShares returns the spending split: one share per category, largest
// first.
func CategoryShares(spending []model.TransactionItem, lang i18n.Language) []CategoryShare {
	refs := make([]*model.TransactionItem, 0, len(spending))
	for index := range spending {
		refs = append(refs, &spending[index])
	}
	return categorySharesRefs(refs, lang)
}

func categorySharesRefs(spending []*model.TransactionItem, lang i18n.Language) []CategoryShare {
	uncategorized := i18n.Text(lang, i18n.Uncategorized)
	groups := make([]CategoryShare, 0)
	for _, item := range spending {
		amount := absClamped(item.Amount.Minor)
		found := false
		for index := range groups {
			if sameCategoryID(groups[index].CategoryID, item.CategoryID) {
				groups[index].Amount.Minor = saturatingAddInt64(groups[index].Amount.Minor, amount)
				found = true
				break
			}
		}
		if found {
			continue
		}
		name := uncategorized
		if item.CategoryName != nil {
			name = *item.CategoryName
		}
		color := ""
		if item.CategoryColor != nil {
			color = *item.CategoryColor
		}
		groups = append(groups, CategoryShare{
			CategoryID: item.CategoryID,
			Name:       name,
			Amount:     amountOf(amount, item.Currency),
			Color:      color,
		})
	}
	sort.SliceStable(groups, func(left, right int) bool {
		return groups[left].Amount.Minor > groups[right].Amount.Minor
	})
	return groups
}

// TopVendors returns the period's expenses grouped into vendors, largest
// first.
func TopVendors(expenses []model.TransactionItem, limit int) []NamedAmount {
	refs := make([]*model.TransactionItem, 0, len(expenses))
	for index := range expenses {
		refs = append(refs, &expenses[index])
	}
	return topVendorsRefs(refs, limit)
}

func topVendorsRefs(expenses []*model.TransactionItem, limit int) []NamedAmount {
	type vendorGroup struct {
		key   string
		named []namedBooking
	}

	groups := make([]vendorGroup, 0)
	for _, item := range expenses {
		name := DisplayDescription(item)
		key := vendorKey(name)
		found := false
		for index := range groups {
			if groups[index].key == key {
				groups[index].named = append(groups[index].named, namedBooking{item: item, name: name})
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, vendorGroup{key: key, named: []namedBooking{{item: item, name: name}}})
		}
	}

	vendors := make([]NamedAmount, 0, len(groups))
	for _, entry := range groups {
		name := vendorLabel(entry.named)
		var total int64
		for _, booking := range entry.named {
			total += absClamped(booking.item.Amount.Minor)
		}
		currency := money.EUR
		if len(entry.named) > 0 {
			currency = entry.named[0].item.Currency
		}
		vendors = append(vendors, NamedAmount{Name: name, Amount: amountOf(total, currency)})
	}
	sort.SliceStable(vendors, func(left, right int) bool {
		return vendors[left].Amount.Minor > vendors[right].Amount.Minor
	})
	if limit < len(vendors) {
		vendors = vendors[:limit]
	}
	return vendors
}

// vendorKey is Rust's key: collapse whitespace runs, then lower-case.
func vendorKey(name string) string {
	return rustLower(strings.Join(strings.Fields(name), " "))
}

// rustLower is full Unicode lower-casing, like Rust's str::to_lowercase.
func rustLower(text string) string {
	return cases.Lower(language.Und).String(text)
}

// namedBooking is one booking's display spelling, used by the vendor grouping.
type namedBooking struct {
	item *model.TransactionItem
	name string
}

// vendorLabel picks the most-used spelling, breaking a tie with the most
// recent booking's spelling.
func vendorLabel(named []namedBooking) string {
	counts := make(map[string]int)
	for _, entry := range named {
		counts[entry.name]++
	}
	mostUsed := 0
	for _, count := range counts {
		if count > mostUsed {
			mostUsed = count
		}
	}
	keys := make([]string, 0, len(counts))
	for name := range counts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	candidates := make([]string, 0)
	for _, name := range keys {
		if counts[name] == mostUsed {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	best := -1
	for index, entry := range named {
		inCandidates := false
		for _, candidate := range candidates {
			if candidate == entry.name {
				inCandidates = true
				break
			}
		}
		if !inCandidates {
			continue
		}
		if best < 0 {
			best = index
			continue
		}
		if compareTransactionTuple(*entry.item, *named[best].item) > 0 {
			best = index
		}
	}
	if best < 0 {
		return ""
	}
	return named[best].name
}

// GetCategoryTrends returns the category spending trends over the last months
// months, including the current one.
func GetCategoryTrends(
	ctx context.Context,
	d *db.Database,
	categoryID model.CategoryID,
	months int,
	today civil.Date,
	lang i18n.Language,
) (CategoryTrend, error) {
	category, found, err := categories.Get(ctx, d, categoryID)
	if err != nil {
		return CategoryTrend{}, err
	}
	currency, err := money.Primary(ctx, d)
	if err != nil {
		return CategoryTrend{}, err
	}
	firstMonth := dashboard.YearMonthFromDate(today).MinusMonths(monthsBack(months))
	start := firstMonth.Start()
	filter := model.TransactionFilter{
		StartDate:  &start,
		EndDate:    &today,
		CategoryID: &categoryID,
		Currency:   &currency,
	}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return CategoryTrend{}, err
	}

	monthlyData := make([]MonthlyAmount, 0, max(months, 0))
	for index := 0; index < months; index++ {
		month := firstMonth.PlusMonths(index)
		var amount int64
		for _, item := range items {
			if item.Date.Year() == month.Year && int(item.Date.Month()) == month.Month {
				amount += absClamped(item.Amount.Minor)
			}
		}
		monthlyData = append(monthlyData, MonthlyAmount{
			Year:   month.Year,
			Month:  monthNumber(month.Month),
			Amount: amountOf(amount, currency),
		})
	}

	var total int64
	for _, month := range monthlyData {
		total += month.Amount.Minor
	}
	var averageMinor int64
	if len(monthlyData) != 0 && total > 0 {
		count := int64(len(monthlyData))
		// Kotlin divides with scale 2 and HALF_UP.
		averageMinor = saturatingAddInt64(saturatingMulInt64(total, 2), count) / saturatingMulInt64(count, 2)
	}
	average := amountOf(averageMinor, currency)
	var latest int64
	if len(monthlyData) != 0 {
		latest = monthlyData[len(monthlyData)-1].Amount.Minor
	}
	trend := trendPercent(latest, averageMinor)

	name := i18n.Text(lang, i18n.ReportUnknownCategory)
	if found {
		name = category.Name
	}
	return CategoryTrend{
		CategoryID:     categoryID,
		CategoryName:   name,
		MonthlyData:    monthlyData,
		AverageMonthly: average,
		Trend:          trend,
	}, nil
}

// trendPercent computes the latest month against the average, in percent,
// one decimal: difference * 100 / average rounded to 1 dp, halfway away from
// zero, like rust_decimal's div plus round_dp_with_strategy.
func trendPercent(latest, average int64) decimal.Decimal {
	if average <= 0 {
		return decimal.Zero
	}
	difference := latest - average
	numerator := big.NewInt(difference)
	numerator.Mul(numerator, big.NewInt(1000))
	denominator := big.NewInt(average)
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	twiceRemainder := new(big.Int).Abs(remainder)
	twiceRemainder.Lsh(twiceRemainder, 1)
	if twiceRemainder.Cmp(denominator) >= 0 {
		if numerator.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return decimalFromTenths(quotient)
}

// decimalFromTenths builds a decimal from a value in tenths, dropping a
// trailing zero like rust_decimal's unscale.
func decimalFromTenths(tenths *big.Int) decimal.Decimal {
	if tenths.Sign() == 0 {
		return decimal.Zero
	}
	negative := tenths.Sign() < 0
	absolute := new(big.Int).Abs(tenths)
	digits := absolute.String()
	text := digits[:len(digits)-1]
	if last := digits[len(digits)-1]; last != '0' {
		text += "." + string(last)
	}
	if negative {
		text = "-" + text
	}
	value, err := decimal.Parse(text)
	if err != nil {
		panic("the trend is a valid decimal")
	}
	return value
}

func saturatingMulInt64(left, right int64) int64 {
	if left == 0 || right == 0 {
		return 0
	}
	product := left * right
	if product/right != left {
		if (left < 0) != (right < 0) {
			return math.MinInt64
		}
		return math.MaxInt64
	}
	return product
}

// monthsBack is Rust's i32::try_from(months.saturating_sub(1)).unwrap_or(i32::MAX).
func monthsBack(months int) int {
	if months <= 1 {
		return 0
	}
	back := months - 1
	if back > math.MaxInt32 {
		return math.MaxInt32
	}
	return back
}

// GenerateTaxReport generates the annual tax report from the database.
func GenerateTaxReport(ctx context.Context, d *db.Database, year int, lang i18n.Language) (TaxReport, error) {
	start := mustDate(year, 1, 1)
	end := mustDate(year, 12, 31)
	filter := model.TransactionFilter{StartDate: &start, EndDate: &end}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return TaxReport{}, err
	}
	currency, err := money.Primary(ctx, d)
	if err != nil {
		return TaxReport{}, err
	}
	return BuildTaxReport(year, items, currency, lang), nil
}

// GenerateRangeReport generates the tax report for an inclusive date range.
func GenerateRangeReport(
	ctx context.Context,
	d *db.Database,
	from civil.Date,
	to civil.Date,
	lang i18n.Language,
) (TaxReport, error) {
	if from.After(to) {
		return TaxReport{}, repo.InvalidError{Msg: i18n.Text(lang, i18n.InvalidDateRange)}
	}
	filter := model.TransactionFilter{StartDate: &from, EndDate: &to}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return TaxReport{}, err
	}
	currency, err := money.Primary(ctx, d)
	if err != nil {
		return TaxReport{}, err
	}
	return BuildReport(from, to, items, currency, lang), nil
}

// GenerateTaxBookings generates the Tax bookings view from the database.
func GenerateTaxBookings(
	ctx context.Context,
	d *db.Database,
	from civil.Date,
	to civil.Date,
	lang i18n.Language,
) (TaxBookings, error) {
	if from.After(to) {
		return TaxBookings{}, repo.InvalidError{Msg: i18n.Text(lang, i18n.InvalidDateRange)}
	}
	filter := model.TransactionFilter{StartDate: &from, EndDate: &to}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return TaxBookings{}, err
	}
	currency, err := money.Primary(ctx, d)
	if err != nil {
		return TaxBookings{}, err
	}
	return BuildTaxBookings(from, to, items, currency, lang), nil
}

// GenerateMonthlyReport generates the monthly spending report from the
// database.
func GenerateMonthlyReport(
	ctx context.Context,
	d *db.Database,
	year int,
	month uint8,
	lang i18n.Language,
) (MonthlyReport, error) {
	selected := dashboard.NewYearMonth(year, int(month))
	start := selected.Start()
	end := selected.End()
	currency, err := money.Primary(ctx, d)
	if err != nil {
		return MonthlyReport{}, err
	}
	filter := model.TransactionFilter{StartDate: &start, EndDate: &end, Currency: &currency}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return MonthlyReport{}, err
	}
	return MonthlyReportOf(year, month, items, currency, lang), nil
}

func sameCategoryID(left, right *model.CategoryID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// compareTransactionTuple orders bookings by date, then id, like Rust's
// (date, id) tuple comparison.
func compareTransactionTuple(left, right model.TransactionItem) int {
	if comparison := left.Date.Compare(right.Date); comparison != 0 {
		return comparison
	}
	switch {
	case left.ID < right.ID:
		return -1
	case left.ID > right.ID:
		return 1
	default:
		return 0
	}
}

// DisplayDescription returns what every screen and export shows as a booking's
// title.
func DisplayDescription(item *model.TransactionItem) string {
	if title := rules.TitleOf(item.Purpose, item.PartnerName); title != nil {
		return *title
	}
	if partner := displayPartner(item.PartnerName); partner != nil {
		return *partner
	}
	if purpose := displayPurpose(item.Purpose); purpose != nil {
		return *purpose
	}
	return "—"
}

// displayPartner renders the readable partner, as FinDesk's
// BankText.displayPartner does.
func displayPartner(raw *string) *string {
	if raw == nil {
		return nil
	}
	for _, part := range splitWhitespaceRuns(*raw, 3) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		cleaned := strings.Join(strings.Fields(part), " ")
		if cleaned == "" {
			return nil
		}
		return &cleaned
	}
	return nil
}

// displayPurpose renders the readable purpose, as FinDesk's
// BankText.displayPurpose does.
func displayPurpose(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil
	}
	matches := sepaTagPattern.FindAllStringIndex(trimmed, -1)
	result := trimmed
	if len(matches) != 0 {
		svwz := -1
		for index, match := range matches {
			tag := trimmed[match[0]:match[1]]
			if strings.HasPrefix(strings.ToLower(tag), "svwz") {
				svwz = index
				break
			}
		}
		if svwz >= 0 {
			next := len(trimmed)
			if svwz+1 < len(matches) {
				next = matches[svwz+1][0]
			}
			result = trimmed[matches[svwz][1]:next]
		} else {
			result = trimmed[:matches[0][0]]
		}
	}
	cleaned := strings.Join(strings.Fields(result), " ")
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

// splitWhitespaceRuns splits text at every run of at least minimum whitespace
// characters, like Rust's Regex::split with `\s{3,}`.
func splitWhitespaceRuns(text string, minimum int) []string {
	parts := make([]string, 0)
	start := 0
	index := 0
	for index < len(text) {
		character, size := utf8.DecodeRuneInString(text[index:])
		if !unicode.IsSpace(character) {
			index += size
			continue
		}
		runStart := index
		count := 0
		for index < len(text) {
			next, nextSize := utf8.DecodeRuneInString(text[index:])
			if !unicode.IsSpace(next) {
				break
			}
			count++
			index += nextSize
		}
		if count >= minimum {
			parts = append(parts, text[start:runStart])
			start = index
		}
	}
	parts = append(parts, text[start:])
	return parts
}
