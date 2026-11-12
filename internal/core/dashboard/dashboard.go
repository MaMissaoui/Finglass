// Package dashboard computes the dashboard's month figures, ported from
// FinDesk's `DashboardViewModel` and `crates/finglass-core/src/dashboard/mod.rs`.
//
// The computation is pure: it takes repository data and returns numbers. There
// is no UI state and no coroutine; callers choose the month and decide when to
// load.
package dashboard

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
)

// UncategorizedColor is the neutral grey for bookings without a category.
const UncategorizedColor = "#757575"

// UncategorizedLabel is the uncategorized slice's label.
const UncategorizedLabel = "Uncategorized"

// YearMonth is a calendar month without a day.
type YearMonth struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month int
}

// NewYearMonth builds a month.
func NewYearMonth(year, month int) YearMonth {
	if month < 1 || month > 12 {
		panic("month out of range")
	}
	return YearMonth{Year: year, Month: month}
}

// YearMonthFromDate returns the month containing date.
func YearMonthFromDate(date civil.Date) YearMonth {
	return YearMonth{Year: date.Year(), Month: int(date.Month())}
}

// Length returns the month's length in days.
func (ym YearMonth) Length() int {
	return daysInMonth(ym.Year, time.Month(ym.Month))
}

// Start returns the first day of the month.
func (ym YearMonth) Start() civil.Date {
	date, ok := civil.New(ym.Year, time.Month(ym.Month), 1)
	if !ok {
		panic("valid date")
	}
	return date
}

// End returns the last day of the month.
func (ym YearMonth) End() civil.Date {
	return ym.Start().AddDays(ym.Length() - 1)
}

// MinusMonths returns the month months months before this one.
func (ym YearMonth) MinusMonths(months int) YearMonth {
	total := ym.Year*12 + ym.Month - 1 - months
	return YearMonth{
		Year:  floorDiv(total, 12),
		Month: floorMod(total, 12) + 1,
	}
}

// PlusMonths returns the month months months after this one.
func (ym YearMonth) PlusMonths(months int) YearMonth {
	return ym.MinusMonths(-months)
}

// Compare returns -1, 0 or 1 when ym is before, equal to or after other.
func (ym YearMonth) Compare(other YearMonth) int {
	switch {
	case ym.Year < other.Year:
		return -1
	case ym.Year > other.Year:
		return 1
	case ym.Month < other.Month:
		return -1
	case ym.Month > other.Month:
		return 1
	default:
		return 0
	}
}

// Before reports whether ym is before other.
func (ym YearMonth) Before(other YearMonth) bool {
	return ym.Compare(other) < 0
}

// After reports whether ym is after other.
func (ym YearMonth) After(other YearMonth) bool {
	return ym.Compare(other) > 0
}

// MonthOptions returns every month from current back to earliest, newest first,
// clamped to a valid range.
//
// FinDesk's month header lists `generateSequence(current) { it.minusMonths(1) }`
// down to the oldest booking. When the two bounds cross, the current month is
// the only choice.
func MonthOptions(earliest, current YearMonth) []YearMonth {
	if current.Before(earliest) {
		earliest = current
	}
	months := make([]YearMonth, 0)
	month := current
	for {
		months = append(months, month)
		if month == earliest {
			break
		}
		month = month.MinusMonths(1)
	}
	return months
}

// ChartSlice is one slice of the spending split.
type ChartSlice struct {
	// Label is the category's name, or [UncategorizedLabel].
	Label string
	// Amount is the slice's amount.
	Amount money.Money
	// Color is the category's color.
	Color *string
	// CategoryID is the category's id, or nil for uncategorized.
	CategoryID *model.CategoryID
}

// MonthBar is one month in the six-month chart.
type MonthBar struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month int
	// Income is money in, transfers excluded.
	Income money.Money
	// Expenses is money out, transfers excluded, as a positive amount.
	Expenses money.Money
	// Partial says whether this is still the running month.
	Partial bool
}

// BudgetStatus is a category's budget for the selected month.
type BudgetStatus struct {
	// CategoryID is the category id.
	CategoryID model.CategoryID
	// CategoryName is the category name.
	CategoryName string
	// Color is the category color.
	Color string
	// Limit is the monthly limit.
	Limit money.Money
	// Spent is what was spent this month (positive).
	Spent money.Money
}

// Ratio returns spent divided by the limit, for a progress bar.
//
// It is a ratio, not money, so it is a float; it uses the same operands and
// order of operations as Rust.
func (b BudgetStatus) Ratio() float64 {
	if b.Limit.Minor > 0 {
		// The ratio Rust computes with `spent as f64 / limit as f64`.
		return float64(b.Spent.Minor) / float64(b.Limit.Minor)
	}
	return 0.0
}

// Remaining returns the limit minus what was spent.
func (b BudgetStatus) Remaining() money.Money {
	return money.Money{Minor: b.Limit.Minor - b.Spent.Minor, Currency: b.Limit.Currency}
}

// IsOver reports whether spending exceeds the limit.
func (b BudgetStatus) IsOver() bool {
	return b.Spent.Minor > b.Limit.Minor
}

// DashboardFigures is the figures one dashboard load publishes.
type DashboardFigures struct {
	// Currency is the currency of every total and chart below.
	Currency money.Currency
	// TotalBalance is the sum of every account's balance in the primary
	// currency.
	TotalBalance money.Money
	// AvailableBalance is money on checking, savings and credit-card accounts.
	AvailableBalance money.Money
	// LoanBalance is the sum of loan accounts' balances in the primary currency.
	LoanBalance money.Money
	// CarryForward is money available at the end of the previous month.
	CarryForward *money.Money
	// OtherMovements is what the available accounts moved outside income and
	// expenses.
	OtherMovements *money.Money
	// BalanceDate is the day the available balance is stated for; nil in the
	// current month.
	BalanceDate *civil.Date
	// HasLoans says whether the accounts include loans.
	HasLoans bool
	// OtherBalances are balances of accounts in other currencies, never added
	// to the totals. It holds Rust's `BTreeMap<Currency, Money>`: one entry per
	// currency, sorted by the currency code, and each amount's currency is its
	// own code.
	OtherBalances []money.Money
	// MonthlyIncome is income in the selected month, transfers excluded.
	MonthlyIncome money.Money
	// MonthlyExpenses is spending in the selected month, transfers excluded, as
	// a positive amount.
	MonthlyExpenses money.Money
	// UncategorizedCount is uncategorized bookings in the selected month, in
	// every currency.
	UncategorizedCount int
	// RecentTransactions are the newest bookings, independent of the selected
	// month.
	RecentTransactions []model.TransactionItem
	// ExpensesByCategory is spending by category in the selected month.
	ExpensesByCategory []ChartSlice
	// TaxDeductionsByCategory is tax-relevant spending from January 1 through
	// the selected month.
	TaxDeductionsByCategory []ChartSlice
	// LastMonths is the six months ending with the selected month.
	LastMonths []MonthBar
	// Budgets are the budgets for the selected month, most-used first.
	Budgets []BudgetStatus
	// Upcoming are recurring bookings expected in the next 30 days.
	Upcoming []recurring.RecurringPayment
	// MonthProgress is how far through the month we are, 0 to 1. It is a ratio,
	// not money.
	MonthProgress float64
	// TransfersThisMonth is money moved out between the user's own accounts this
	// month.
	TransfersThisMonth money.Money
	// SelectedMonth is the month the figures belong to.
	SelectedMonth YearMonth
	// LoadedMonth is the month whose figures these are; equal to
	// [DashboardFigures.SelectedMonth] after a load.
	LoadedMonth YearMonth
	// EarliestMonth is the month of the oldest booking in the primary currency.
	EarliestMonth YearMonth
	// CurrentMonth is today's calendar month at load time.
	CurrentMonth YearMonth
	// HasAccounts says whether there are any accounts.
	HasAccounts bool
}

// DashboardInput is already-loaded repository data for one dashboard
// computation.
type DashboardInput struct {
	// Today is the current date.
	Today civil.Date
	// RequestedMonth is the requested month; computation clamps it to the data
	// and today.
	RequestedMonth YearMonth
	// PrimaryCurrency is the primary currency.
	PrimaryCurrency money.Currency
	// Accounts are every account.
	Accounts []model.Account
	// Categories are every category.
	Categories []model.Category
	// OldestDate is the oldest booking date in the primary currency.
	OldestDate *civil.Date
	// MonthBookings are every booking from the selected month's first day on, in
	// every currency.
	MonthBookings []model.TransactionItem
	// Window are the bookings from the chart window through the selected month,
	// in the primary currency.
	Window []model.TransactionItem
	// RecentTransactions are the newest bookings, independent of the selected
	// month.
	RecentTransactions []model.TransactionItem
	// UncategorizedCount is the count of uncategorized bookings in the selected
	// month, in every currency.
	UncategorizedCount int
	// Upcoming are recurring payments expected soon.
	Upcoming []recurring.RecurringPayment
}

// Dashboard is the dashboard's pure computation. It holds no state.
type Dashboard struct{}

// Compute computes the month figures from repository data.
func (Dashboard) Compute(input DashboardInput) DashboardFigures {
	nowMonth := YearMonthFromDate(input.Today)
	earliestMonth := nowMonth
	if input.OldestDate != nil {
		candidate := YearMonthFromDate(*input.OldestDate)
		if candidate.Before(nowMonth) {
			earliestMonth = candidate
		}
	}
	month := input.RequestedMonth
	if nowMonth.Before(month) {
		month = nowMonth
	}
	if earliestMonth.After(month) {
		month = earliestMonth
	}
	monthStart := month.Start()
	monthEnd := month.End()
	sixMonthsStart := month.MinusMonths(5).Start()
	yearStart, ok := civil.New(month.Year, time.January, 1)
	if !ok {
		panic("valid date")
	}
	currentMonth := month == nowMonth
	currency := input.PrimaryCurrency

	monthBookings := make(map[model.AccountID][]*model.TransactionItem)
	for index := range input.MonthBookings {
		transaction := &input.MonthBookings[index]
		monthBookings[transaction.AccountID] = append(monthBookings[transaction.AccountID], transaction)
	}

	balanceAt := func(account *model.Account) int64 {
		if account.AccountType == model.AccountTypeDepot || currentMonth {
			return account.Balance.Minor
		}
		var after int64
		for _, transaction := range monthBookings[account.ID] {
			if transaction.Date.After(monthEnd) {
				after += transaction.Amount.Minor
			}
		}
		return account.Balance.Minor - after
	}
	movement := func(account *model.Account) int64 {
		var total int64
		for _, transaction := range monthBookings[account.ID] {
			if currentMonth || !transaction.Date.After(monthEnd) {
				total += transaction.Amount.Minor
			}
		}
		return total
	}

	balances := money.SumByCurrency(
		input.Accounts,
		currency,
		func(account model.Account) money.Currency { return account.Currency },
		func(account model.Account) int64 { return balanceAt(&account) },
	)
	totalBalance := totalIn(balances, currency)
	otherBalances := make([]money.Money, 0, len(balances))
	for _, entry := range balances {
		if entry.Currency == currency {
			continue
		}
		otherBalances = append(otherBalances, money.Money{Minor: entry.Total, Currency: entry.Currency})
	}

	availableAccounts := make([]*model.Account, 0, len(input.Accounts))
	loanAccounts := make([]*model.Account, 0, len(input.Accounts))
	for index := range input.Accounts {
		account := &input.Accounts[index]
		if account.AccountType != model.AccountTypeLoan &&
			account.AccountType != model.AccountTypeDepot &&
			account.Currency == currency {
			availableAccounts = append(availableAccounts, account)
		}
		if account.AccountType == model.AccountTypeLoan {
			loanAccounts = append(loanAccounts, account)
		}
	}

	availableTotal := totalIn(money.SumByCurrency(
		availableAccounts,
		currency,
		func(account *model.Account) money.Currency { return account.Currency },
		func(account *model.Account) int64 { return balanceAt(account) },
	), currency)
	loanTotal := totalIn(money.SumByCurrency(
		loanAccounts,
		currency,
		func(account *model.Account) money.Currency { return account.Currency },
		func(account *model.Account) int64 { return balanceAt(account) },
	), currency)

	thisMonth := make([]*model.TransactionItem, 0, len(input.Window))
	for index := range input.Window {
		transaction := &input.Window[index]
		if !transaction.Date.Before(monthStart) && !transaction.Date.After(monthEnd) {
			thisMonth = append(thisMonth, transaction)
		}
	}
	thisMonthExpenses := make([]*model.TransactionItem, 0, len(thisMonth))
	for _, transaction := range thisMonth {
		if transaction.CountsAsSpending() {
			thisMonthExpenses = append(thisMonthExpenses, transaction)
		}
	}
	var monthlyIncome int64
	for _, transaction := range thisMonth {
		if transaction.CountsAsIncome() {
			monthlyIncome += transaction.Amount.Minor
		}
	}
	var monthlyExpenses int64
	for _, transaction := range thisMonthExpenses {
		monthlyExpenses += absInt64(transaction.Amount.Minor)
	}

	var carryForward int64
	var availableMovement int64
	for _, account := range availableAccounts {
		carryForward += balanceAt(account) - movement(account)
		availableMovement += movement(account)
	}
	otherMovements := availableMovement - (monthlyIncome - monthlyExpenses)

	lastMonths := make([]MonthBar, 0, 6)
	for index := 0; index < 6; index++ {
		barMonth := YearMonthFromDate(sixMonthsStart).PlusMonths(index)
		monthTransactions := make([]*model.TransactionItem, 0, len(input.Window))
		for windowIndex := range input.Window {
			transaction := &input.Window[windowIndex]
			if transaction.Date.Year() == barMonth.Year && int(transaction.Date.Month()) == barMonth.Month {
				monthTransactions = append(monthTransactions, transaction)
			}
		}
		var income int64
		for _, transaction := range monthTransactions {
			if transaction.CountsAsIncome() {
				income += transaction.Amount.Minor
			}
		}
		var expenses int64
		for _, transaction := range monthTransactions {
			if transaction.CountsAsSpending() {
				expenses += absInt64(transaction.Amount.Minor)
			}
		}
		lastMonths = append(lastMonths, MonthBar{
			Year:     barMonth.Year,
			Month:    barMonth.Month,
			Income:   moneyOf(income, currency),
			Expenses: moneyOf(expenses, currency),
			Partial:  barMonth == nowMonth,
		})
	}

	expensesByCategory := slices(thisMonthExpenses, currency)
	taxTransactions := make([]*model.TransactionItem, 0, len(input.Window))
	for index := range input.Window {
		transaction := &input.Window[index]
		if transaction.IsExpense() && transaction.IsTaxRelevant && !transaction.Date.Before(yearStart) {
			taxTransactions = append(taxTransactions, transaction)
		}
	}
	taxDeductionsByCategory := slices(taxTransactions, currency)

	budgets := make([]BudgetStatus, 0, len(input.Categories))
	for _, category := range input.Categories {
		if category.BudgetLimit == nil || category.BudgetLimit.Minor <= 0 {
			continue
		}
		var spent int64
		for _, transaction := range thisMonthExpenses {
			if transaction.CategoryID != nil && *transaction.CategoryID == category.ID {
				spent += absInt64(transaction.Amount.Minor)
			}
		}
		budgets = append(budgets, BudgetStatus{
			CategoryID:   category.ID,
			CategoryName: category.Name,
			Color:        category.Color,
			Limit:        *category.BudgetLimit,
			Spent:        moneyOf(spent, currency),
		})
	}
	sort.SliceStable(budgets, func(left, right int) bool {
		return totalCmp(budgets[right].Ratio(), budgets[left].Ratio()) < 0
	})

	var transfersThisMonth int64
	for _, transaction := range thisMonth {
		if transaction.IsTransfer && transaction.Amount.Minor < 0 {
			transfersThisMonth += absInt64(transaction.Amount.Minor)
		}
	}

	monthProgress := 1.0
	if currentMonth {
		// A ratio, not money: today's day over the month's length, as in Rust.
		monthProgress = float64(input.Today.Day()) / float64(month.Length())
	}

	recentTransactions := make([]model.TransactionItem, len(input.RecentTransactions))
	copy(recentTransactions, input.RecentTransactions)

	hasLoans := false
	for _, account := range input.Accounts {
		if account.AccountType == model.AccountTypeLoan {
			hasLoans = true
			break
		}
	}

	var balanceDate *civil.Date
	if !currentMonth {
		end := monthEnd
		balanceDate = &end
	}
	carry := moneyOf(carryForward, currency)
	other := moneyOf(otherMovements, currency)

	return DashboardFigures{
		Currency:                currency,
		TotalBalance:            moneyOf(totalBalance, currency),
		AvailableBalance:        moneyOf(availableTotal, currency),
		LoanBalance:             moneyOf(loanTotal, currency),
		CarryForward:            &carry,
		OtherMovements:          &other,
		BalanceDate:             balanceDate,
		HasLoans:                hasLoans,
		OtherBalances:           otherBalances,
		MonthlyIncome:           moneyOf(monthlyIncome, currency),
		MonthlyExpenses:         moneyOf(monthlyExpenses, currency),
		UncategorizedCount:      input.UncategorizedCount,
		RecentTransactions:      recentTransactions,
		ExpensesByCategory:      expensesByCategory,
		TaxDeductionsByCategory: taxDeductionsByCategory,
		LastMonths:              lastMonths,
		Budgets:                 budgets,
		Upcoming:                input.Upcoming,
		MonthProgress:           monthProgress,
		TransfersThisMonth:      moneyOf(transfersThisMonth, currency),
		SelectedMonth:           month,
		LoadedMonth:             month,
		EarliestMonth:           earliestMonth,
		CurrentMonth:            nowMonth,
		HasAccounts:             len(input.Accounts) > 0,
	}
}

func moneyOf(minor int64, currency money.Currency) money.Money {
	return money.Money{Minor: minor, Currency: currency}
}

func slices(expenses []*model.TransactionItem, currency money.Currency) []ChartSlice {
	chartSlices := make([]ChartSlice, 0)
	for _, transaction := range expenses {
		label := UncategorizedLabel
		if transaction.CategoryName != nil {
			label = *transaction.CategoryName
		}
		var color *string
		switch {
		case transaction.CategoryColor != nil:
			value := *transaction.CategoryColor
			color = &value
		case transaction.CategoryID == nil:
			value := UncategorizedColor
			color = &value
		}

		found := false
		for index := range chartSlices {
			if chartSlices[index].Label == label {
				chartSlices[index].Amount.Minor += absInt64(transaction.Amount.Minor)
				found = true
				break
			}
		}
		if found {
			continue
		}
		amount := absInt64(transaction.Amount.Minor)
		chartSlices = append(chartSlices, ChartSlice{
			Label:      label,
			Amount:     moneyOf(amount, currency),
			Color:      color,
			CategoryID: transaction.CategoryID,
		})
	}
	sort.SliceStable(chartSlices, func(left, right int) bool {
		return chartSlices[left].Amount.Minor > chartSlices[right].Amount.Minor
	})
	return chartSlices
}

// LoadDashboard loads the repository data and computes one dashboard state.
//
// requestedMonth is clamped to the data's oldest month and to today, exactly as
// the view model does.
func LoadDashboard(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
	requestedMonth YearMonth,
) (DashboardFigures, error) {
	primary, err := money.Primary(ctx, d)
	if err != nil {
		return DashboardFigures{}, err
	}
	oldestDate, err := transactions.OldestDate(ctx, d, &primary)
	if err != nil {
		return DashboardFigures{}, err
	}
	nowMonth := YearMonthFromDate(today)
	earliestMonth := nowMonth
	if oldestDate != nil {
		candidate := YearMonthFromDate(*oldestDate)
		if candidate.Before(nowMonth) {
			earliestMonth = candidate
		}
	}
	month := requestedMonth
	if nowMonth.Before(month) {
		month = nowMonth
	}
	if earliestMonth.After(month) {
		month = earliestMonth
	}

	allAccounts, err := accounts.List(ctx, d)
	if err != nil {
		return DashboardFigures{}, err
	}
	allCategories, err := categories.List(ctx, d)
	if err != nil {
		return DashboardFigures{}, err
	}
	monthStart := month.Start()
	monthBookings, err := transactions.FindItems(ctx, d, &model.TransactionFilter{
		StartDate: &monthStart,
	}, nil)
	if err != nil {
		return DashboardFigures{}, err
	}
	yearStart, ok := civil.New(month.Year, time.January, 1)
	if !ok {
		panic("valid date")
	}
	windowStart := month.MinusMonths(5).Start()
	if yearStart.Before(windowStart) {
		windowStart = yearStart
	}
	monthEnd := month.End()
	window, err := transactions.FindItems(ctx, d, &model.TransactionFilter{
		StartDate: &windowStart,
		EndDate:   &monthEnd,
		Currency:  &primary,
	}, nil)
	if err != nil {
		return DashboardFigures{}, err
	}
	recentLimit := 8
	recentTransactions, err := transactions.FindItems(ctx, d, &model.TransactionFilter{}, &recentLimit)
	if err != nil {
		return DashboardFigures{}, err
	}
	uncategorized, err := transactions.FindItems(ctx, d, &model.TransactionFilter{
		StartDate:         &monthStart,
		EndDate:           &monthEnd,
		UncategorizedOnly: true,
	}, nil)
	if err != nil {
		return DashboardFigures{}, err
	}

	recurringStart := today.AddDays(-400)
	recurringWindow, err := transactions.FindItems(ctx, d, &model.TransactionFilter{
		StartDate: &recurringStart,
		EndDate:   &today,
	}, nil)
	if err != nil {
		return DashboardFigures{}, err
	}
	detector := recurring.RecurringDetector{}
	detected := detector.Detect(recurringWindow, today)
	upcoming := detector.Upcoming(detected, today, 30)

	return (Dashboard{}).Compute(DashboardInput{
		Today:              today,
		RequestedMonth:     month,
		PrimaryCurrency:    primary,
		Accounts:           allAccounts,
		Categories:         allCategories,
		OldestDate:         oldestDate,
		MonthBookings:      monthBookings,
		Window:             window,
		RecentTransactions: recentTransactions,
		UncategorizedCount: len(uncategorized),
		Upcoming:           upcoming,
	}), nil
}

// totalIn returns one currency's total, or zero when it has no entries.
func totalIn(entries []money.CurrencyTotal, currency money.Currency) int64 {
	for _, entry := range entries {
		if entry.Currency == currency {
			return entry.Total
		}
	}
	return 0
}

// totalCmp orders floats like Rust's f64::total_cmp.
func totalCmp(left, right float64) int {
	leftBits := sortableBits(left)
	rightBits := sortableBits(right)
	switch {
	case leftBits < rightBits:
		return -1
	case leftBits > rightBits:
		return 1
	default:
		return 0
	}
}

// sortableBits maps a float's bits to an order-preserving unsigned value, the
// same total order Rust's f64::total_cmp gives.
func sortableBits(value float64) uint64 {
	bits := math.Float64bits(value)
	if bits>>63 != 0 {
		return ^bits
	}
	return bits | (1 << 63)
}

func daysInMonth(year int, month time.Month) int {
	switch month {
	case time.January, time.March, time.May, time.July, time.August, time.October, time.December:
		return 31
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 0
	}
}

func floorDiv(left, right int) int {
	quotient := left / right
	remainder := left % right
	if remainder != 0 && (remainder < 0) != (right < 0) {
		quotient--
	}
	return quotient
}

func floorMod(left, right int) int {
	remainder := left % right
	if remainder != 0 && (remainder < 0) != (right < 0) {
		remainder += right
	}
	return remainder
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
