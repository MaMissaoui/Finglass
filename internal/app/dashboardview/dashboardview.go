// Package dashboardview ports crates/finglass-core/src/app/dashboard.rs: the
// Dashboard page state without any UI.
//
// This is the UI-free view of FinDesk's DashboardViewModel: the selected and
// loaded months, the clock and first-run flags, and a DashboardView whose rows
// and texts are ready to render. Every figure comes from
// dashboard.LoadDashboard; the page never recomputes money.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
package dashboardview

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/banktext"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// LocalToday is the local calendar date, falling back to UTC when the machine's
// offset cannot be determined.
//
// The dashboard's "today" uses the local date: near midnight UTC would
// otherwise show the wrong month.
func LocalToday() civil.Date {
	return civil.FromTime(time.Now())
}

// BalanceTone says how a balance should be coloured by the page.
type BalanceTone int

const (
	// BalanceTonePositive is money above zero.
	BalanceTonePositive BalanceTone = iota
	// BalanceToneZero is exactly zero.
	BalanceToneZero
	// BalanceToneNegative is money below zero.
	BalanceToneNegative
)

// NavigationArgs is the filter a dashboard click asks the Transactions page to
// show.
//
// It mirrors FinDesk's NavigationArgs: the current month is a period, a past
// month is the exact one.
type NavigationArgs struct {
	// Month is a specific past month, or nil for the Transactions page's
	// default period.
	Month *dashboard.YearMonth
	// CategoryID is one category, or nil for every category.
	CategoryID *model.CategoryID
	// UncategorizedOnly shows only bookings without a category.
	UncategorizedOnly bool
	// Text is a free-text partner search.
	Text *string
	// Period is the period to filter by; the current month's links pass
	// transactionsview.PeriodThisMonth.
	Period *transactionsview.Period
}

// AllNavigationArgs returns a link with no filter: "view all" and ordinary navigation.
func AllNavigationArgs() NavigationArgs {
	return NavigationArgs{}
}

// CategoryNavigation returns the NavigationArgs for one category row, as
// FinDesk's dashboard builds it.
//
// The current month keeps the plain period; a past month passes the exact
// month.
func CategoryNavigation(
	selected dashboard.YearMonth,
	current dashboard.YearMonth,
	categoryID model.CategoryID,
) NavigationArgs {
	month := pastMonth(selected, current)
	return NavigationArgs{
		Month:             month,
		CategoryID:        &categoryID,
		UncategorizedOnly: false,
		Text:              nil,
		Period:            periodIfCurrent(month),
	}
}

// UncategorizedNavigation returns the NavigationArgs for the pending-tasks card
// and the uncategorized slice.
func UncategorizedNavigation(selected dashboard.YearMonth, current dashboard.YearMonth) NavigationArgs {
	month := pastMonth(selected, current)
	return NavigationArgs{
		Month:             month,
		CategoryID:        nil,
		UncategorizedOnly: true,
		Text:              nil,
		Period:            periodIfCurrent(month),
	}
}

// PartnerNavigation returns the NavigationArgs for "show all from this
// partner".
func PartnerNavigation(text string) NavigationArgs {
	return NavigationArgs{Text: &text}
}

// StartArgs returns the arguments the Transactions page starts with.
func (n NavigationArgs) StartArgs() transactionsview.StartArgs {
	return transactionsview.StartArgs{
		AccountID:         nil,
		UncategorizedOnly: n.UncategorizedOnly,
		Partner:           n.Text,
		CategoryID:        n.CategoryID,
		Period:            n.Period,
		Month:             n.Month,
	}
}

func pastMonth(selected dashboard.YearMonth, current dashboard.YearMonth) *dashboard.YearMonth {
	if selected != current {
		month := selected
		return &month
	}
	return nil
}

func periodIfCurrent(month *dashboard.YearMonth) *transactionsview.Period {
	if month != nil {
		return nil
	}
	period := transactionsview.PeriodThisMonth
	return &period
}

// CategorySliceView is one slice of the spending split or the tax chart.
type CategorySliceView struct {
	// Label is the display label, already translated.
	Label string
	// Amount is the slice's amount.
	Amount money.Money
	// Color is the category colour, `#RRGGBB`.
	Color string
	// CategoryID is the category, or nil for uncategorized.
	CategoryID *model.CategoryID
	// Fraction is the slice's share of the total, for the split bar.
	Fraction float32
}

// CategoryRowView is one legend row of the spending split.
type CategoryRowView struct {
	// Label is the display label, already translated.
	Label string
	// PercentText is the percentage text, e.g. "49%".
	PercentText string
	// AmountText is the formatted amount.
	AmountText string
	// Color is the category colour, `#RRGGBB`.
	Color string
	// CategoryID is the category, or nil for uncategorized or the folded
	// "more" row.
	CategoryID *model.CategoryID
	// IsMore says the row folds the categories beyond the first eight.
	IsMore bool
	// Fraction is the row's share of the split bar, for the folded row too.
	Fraction float32
}

// StatementLineKind says what a month statement line's amount means, for
// colours and emphasis.
type StatementLineKind int

const (
	// StatementLineKindCarry is money carried over from the previous month.
	StatementLineKindCarry StatementLineKind = iota
	// StatementLineKindIn is income in the loaded month.
	StatementLineKindIn
	// StatementLineKindOut is spending in the loaded month.
	StatementLineKindOut
	// StatementLineKindMovement is transfers and loans outside income and
	// expenses.
	StatementLineKindMovement
	// StatementLineKindResult is the available balance, below the rule.
	StatementLineKindResult
)

// StatementLineView is one line of the dashboard's month statement.
type StatementLineView struct {
	// Label is the line's display label, already translated.
	Label string
	// AmountText is the formatted signed amount.
	AmountText string
	// Kind says what the amount means.
	Kind StatementLineKind
}

// MonthBarView is one month of the income-vs-expenses chart.
type MonthBarView struct {
	// Year is the year.
	Year int
	// Month is the month, 1 to 12.
	Month int
	// Label is the month's short label for the chart, e.g. "Sep" or "Sept.".
	Label string
	// Title is the month and year for the hover tooltip, e.g.
	// "September 2026".
	Title string
	// Income is money in, transfers excluded.
	Income money.Money
	// Expenses is money out, transfers excluded, as a positive amount.
	Expenses money.Money
	// IncomeText is the signed income amount for the hover tooltip, e.g.
	// "+€2,000.00".
	IncomeText string
	// ExpensesText is the signed expenses amount for the hover tooltip, e.g.
	// "-€135.00".
	ExpensesText string
	// IncomeFraction is the income bar's share of the chart's top, 0 to 1.
	IncomeFraction float32
	// ExpensesFraction is the expenses bar's share of the chart's top, 0 to 1.
	ExpensesFraction float32
	// Partial says whether this is still the running month.
	Partial bool
}

// BudgetRowView is one budget row.
type BudgetRowView struct {
	// CategoryID is the category.
	CategoryID model.CategoryID
	// Name is the category's name.
	Name string
	// Color is the category's colour, `#RRGGBB`.
	Color string
	// Limit is the monthly limit.
	Limit money.Money
	// Spent is what was spent this month, as a positive amount.
	Spent money.Money
	// Ratio is spent divided by the limit, clamped for the progress bar.
	Ratio float32
	// IsOver says whether spending exceeds the limit.
	IsOver bool
	// Warning says whether spending is ahead of the month's progress.
	Warning bool
	// AmountText is the "spent of limit" line.
	AmountText string
	// RestText is the "left" or "over budget" line.
	RestText string
}

// UpcomingRowView is one recurring payment due in the next 30 days.
type UpcomingRowView struct {
	// Name is the display name, cleaned for the UI.
	Name string
	// NextDate is the next expected date.
	NextDate civil.Date
	// DateText is the formatted next date.
	DateText string
	// Amount is the typical amount, signed.
	Amount money.Money
	// AmountText is the formatted typical amount.
	AmountText string
	// Cadence says how often it repeats.
	Cadence recurring.Cadence
	// Meta is the second line: cadence, category and variable-amount note.
	Meta string
	// IsIncome says whether the amount is money in.
	IsIncome bool
}

// TransactionRowView is one recent booking.
type TransactionRowView struct {
	// ID is the row id.
	ID model.TransactionID
	// Date is the booking date.
	Date civil.Date
	// DateText is the formatted booking date.
	DateText string
	// Title is the cleaned title.
	Title string
	// Meta is the category name, or the translated "Uncategorized".
	Meta string
	// Amount is the signed amount.
	Amount money.Money
	// AmountText is the formatted signed amount.
	AmountText string
	// IsExpense says whether the amount is money out.
	IsExpense bool
	// Partner is the raw partner name, for "show all from this partner".
	Partner *string
}

// DashboardView holds the rows and strings the Dashboard page renders.
type DashboardView struct {
	// SelectedMonth is the month the header shows.
	SelectedMonth dashboard.YearMonth
	// LoadedMonth is the month the figures below belong to.
	LoadedMonth dashboard.YearMonth
	// EarliestMonth is the oldest month with a booking in the primary
	// currency.
	EarliestMonth dashboard.YearMonth
	// CurrentMonth is today's calendar month.
	CurrentMonth dashboard.YearMonth
	// IsCurrentMonth says whether the selected month is the current one.
	IsCurrentMonth bool
	// PreviousEnabled says whether the previous-month control is enabled.
	PreviousEnabled bool
	// NextEnabled says whether the next-month control is enabled.
	NextEnabled bool
	// MonthLabel is the selected month as "November 2026".
	MonthLabel string
	// Currency is the currency of every total and chart.
	Currency money.Currency
	// TotalBalance is the sum of every account's balance in the primary
	// currency.
	TotalBalance money.Money
	// AvailableBalance is money on checking, savings and credit-card accounts.
	AvailableBalance money.Money
	// LoanBalance is the sum of loan accounts' balances.
	LoanBalance money.Money
	// HasLoans says whether the accounts include loans.
	HasLoans bool
	// CarryForward is money available at the end of the previous month.
	CarryForward *money.Money
	// OtherMovements is what the available accounts moved outside income and
	// expenses.
	OtherMovements *money.Money
	// BalanceDate is the day the available balance is stated for; nil in the
	// current month.
	BalanceDate *civil.Date
	// OtherBalances are balances of accounts in other currencies, never added
	// to the totals.
	OtherBalances []money.Money
	// MonthlyIncome is income in the loaded month, transfers excluded.
	MonthlyIncome money.Money
	// MonthlyExpenses is spending in the loaded month, transfers excluded, as a
	// positive amount.
	MonthlyExpenses money.Money
	// UncategorizedCount is uncategorized bookings in the loaded month, in every
	// currency.
	UncategorizedCount int
	// TaxTotal is tax-relevant spending from January 1 through the loaded
	// month.
	TaxTotal money.Money
	// ExpensesByCategory is spending by category in the loaded month.
	ExpensesByCategory []CategorySliceView
	// ExpenseRows are the legend rows, with categories beyond eight folded into
	// "more".
	ExpenseRows []CategoryRowView
	// TaxDeductionsByCategory is tax-relevant spending by category, raw and
	// complete.
	TaxDeductionsByCategory []CategorySliceView
	// TaxRows are the tax card's legend rows, with categories beyond eight
	// folded into "more".
	TaxRows []CategoryRowView
	// MonthBars are the six months ending with the loaded month.
	MonthBars []MonthBarView
	// MonthProgress is how far through the loaded month we are, 0 to 1.
	MonthProgress float32
	// TransfersThisMonth is money moved out between the user's own accounts
	// this month.
	TransfersThisMonth money.Money
	// Budgets are budgets for the loaded month, most-used first.
	Budgets []BudgetRowView
	// Upcoming are recurring payments expected in the next 30 days.
	Upcoming []UpcomingRowView
	// RecentTransactions are the newest bookings, independent of the loaded
	// month.
	RecentTransactions []TransactionRowView
	// StatementLines are the month statement's lines, in statement order.
	StatementLines []StatementLineView
	// AvailableTitle is the available card's title.
	AvailableTitle string
	// AvailableValue is the available card's formatted value.
	AvailableValue string
	// AvailableSubtitle is the available card's loan and currency notes below
	// the statement.
	AvailableSubtitle string
	// AvailableTone is the available value's colour tone.
	AvailableTone BalanceTone
	// IncomeTitle is the income card's title.
	IncomeTitle string
	// IncomeValue is the income card's formatted value.
	IncomeValue string
	// ExpensesTitle is the expenses card's title.
	ExpensesTitle string
	// ExpensesValue is the expenses card's formatted value.
	ExpensesValue string
	// TaxTitle is the tax card's title.
	TaxTitle string
	// TaxValue is the tax card's formatted total.
	TaxValue string
	// TaxHint is the tax card's hint when there is nothing to chart.
	TaxHint string
	// PendingTitle is the pending-tasks card's title.
	PendingTitle string
	// PendingValue is the pending-tasks card's formatted count.
	PendingValue string
	// PendingSubtitle is the pending-tasks card's subtitle.
	PendingSubtitle string
	// PendingLines are the pending-tasks detail lines, one per kind, for the
	// right-hand panel.
	PendingLines []string
	// PendingEnabled says whether the pending-tasks card opens the
	// uncategorized list.
	PendingEnabled bool
	// ExpensesSectionTitle is the expenses-by-category card's title.
	ExpensesSectionTitle string
	// ExpensesEmptyText is the text shown when there are no expenses.
	ExpensesEmptyText string
	// TransfersNote is the transfers line below the split, when transfers
	// happened.
	TransfersNote *string
	// ChartTitle is the income-vs-expenses card's title.
	ChartTitle string
	// ChartEmptyText is the text shown when the chart has no bookings.
	ChartEmptyText *string
	// ChartAxisTop is the chart's top axis label.
	ChartAxisTop string
	// ChartAxisMid is the chart's middle axis label.
	ChartAxisMid string
	// ChartAxisZero is the chart's zero axis label.
	ChartAxisZero string
	// IncomeLegend is the income legend label.
	IncomeLegend string
	// ExpensesLegend is the expenses legend label.
	ExpensesLegend string
	// PartialMonthNote is the note explaining pale bars in the running month.
	PartialMonthNote string
	// BudgetsTitle is the budgets card's title.
	BudgetsTitle string
	// UpcomingTitle is the upcoming card's title.
	UpcomingTitle string
	// RecentTitle is the recent transactions card's title.
	RecentTitle string
	// RecentEmptyText is the text shown when there are no recent bookings.
	RecentEmptyText string
	// ViewAllLabel is the "view all" action label.
	ViewAllLabel string
}

// FromFigures turns one dashboard computation into the rows and texts the page
// shows.
//
// unlinkedDocuments is the number of receipts not linked to a booking; pending
// tasks count them together with the uncategorized bookings, as FinDesk does.
func FromFigures(figures dashboard.DashboardFigures, unlinkedDocuments int, language i18n.Language) DashboardView {
	loaded := figures.LoadedMonth
	currentLoaded := loaded == figures.CurrentMonth
	currency := figures.Currency

	expenseSlices := categorySlices(figures.ExpensesByCategory, language)
	taxSlices := categorySlices(figures.TaxDeductionsByCategory, language)
	taxRows := categoryRows(taxSlices, language)
	taxTotal := sumMoney(slicesAmounts(taxSlices), currency)
	monthBars := monthBars(figures.LastMonths, language)
	chartHasData := false
	for _, bar := range monthBars {
		if bar.Income.Minor != 0 || bar.Expenses.Minor != 0 {
			chartHasData = true
			break
		}
	}
	var chartEmptyText *string
	if !chartHasData {
		var text string
		if currentLoaded {
			text = i18n.Text(language, i18n.NoBookingsSixMonths)
		} else {
			text = i18n.Format(language, i18n.NoBookingsSixMonthsTo, i18n.Str(MonthText(loaded, language)))
		}
		chartEmptyText = &text
	}

	axisTopMinor := int64(0)
	for _, bar := range monthBars {
		if bar.Income.Minor > axisTopMinor {
			axisTopMinor = bar.Income.Minor
		}
		if bar.Expenses.Minor > axisTopMinor {
			axisTopMinor = bar.Expenses.Minor
		}
	}
	axisTopMinor = niceCeilingMinor(axisTopMinor)

	pending := figures.UncategorizedCount + unlinkedDocuments
	var pendingSubtitle string
	if pending == 0 {
		pendingSubtitle = i18n.Text(language, i18n.NothingPending)
	} else {
		pendingSubtitle = i18n.Format(
			language,
			i18n.PendingSubtitle,
			i18n.Int(int64(figures.UncategorizedCount)),
			i18n.Int(int64(unlinkedDocuments)),
		)
	}
	pendingLines := strings.Split(pendingSubtitle, " · ")

	var availableTitle string
	if figures.BalanceDate != nil {
		availableTitle = i18n.Format(language, i18n.AvailableAsOf, i18n.Str(format.Date(*figures.BalanceDate)))
	} else {
		availableTitle = i18n.Text(language, i18n.AvailableBalance)
	}
	availableValue := format.Money(figures.AvailableBalance, language, false)
	statementLines := statementLines(figures, availableTitle, language)

	budgets := make([]BudgetRowView, 0, len(figures.Budgets))
	for _, budget := range figures.Budgets {
		budgets = append(budgets, budgetRow(budget, language, figures.MonthProgress))
	}

	upcoming := make([]UpcomingRowView, 0, len(figures.Upcoming))
	for _, payment := range figures.Upcoming {
		name := payment.Name
		if display := banktext.DisplayPartner(&payment.Name); display != nil {
			name = *display
		}
		upcoming = append(upcoming, UpcomingRowView{
			Name:       name,
			NextDate:   payment.NextDate,
			DateText:   format.Date(payment.NextDate),
			Amount:     payment.TypicalAmount,
			AmountText: format.Money(payment.TypicalAmount, language, true),
			Cadence:    payment.Cadence,
			Meta:       upcomingMeta(payment, language),
			IsIncome:   payment.IsIncome(),
		})
	}

	recentTransactions := make([]TransactionRowView, 0, len(figures.RecentTransactions))
	for _, transaction := range figures.RecentTransactions {
		meta := ""
		if transaction.CategoryName != nil {
			meta = *transaction.CategoryName
		} else {
			meta = i18n.Text(language, i18n.Uncategorized)
		}
		recentTransactions = append(recentTransactions, TransactionRowView{
			ID:         transaction.ID,
			Date:       transaction.Date,
			DateText:   format.Date(transaction.Date),
			Title:      banktext.DisplayDescription(&transaction),
			Meta:       meta,
			Amount:     transaction.Amount,
			AmountText: format.Money(transaction.Amount, language, true),
			IsExpense:  transaction.IsExpense(),
			Partner:    transaction.PartnerName,
		})
	}

	otherBalances := make([]money.Money, len(figures.OtherBalances))
	copy(otherBalances, figures.OtherBalances)

	expenseSlicesCopy := make([]CategorySliceView, len(expenseSlices))
	copy(expenseSlicesCopy, expenseSlices)
	taxSlicesCopy := make([]CategorySliceView, len(taxSlices))
	copy(taxSlicesCopy, taxSlices)

	var transfersNote *string
	if figures.TransfersThisMonth.Minor > 0 {
		text := i18n.Format(
			language,
			i18n.CategorySplitTransfers,
			i18n.Str(format.Money(figures.TransfersThisMonth, language, false)),
		)
		transfersNote = &text
	}

	chartAxisTop := ""
	chartAxisMid := ""
	if axisTopMinor != 0 {
		chartAxisTop = axisLabel(axisTopMinor, currency, language)
		chartAxisMid = axisLabel(axisTopMinor/2, currency, language)
	}

	return DashboardView{
		SelectedMonth:           figures.SelectedMonth,
		LoadedMonth:             loaded,
		EarliestMonth:           figures.EarliestMonth,
		CurrentMonth:            figures.CurrentMonth,
		IsCurrentMonth:          figures.SelectedMonth == figures.CurrentMonth,
		PreviousEnabled:         figures.SelectedMonth.After(figures.EarliestMonth),
		NextEnabled:             figures.SelectedMonth.Before(figures.CurrentMonth),
		MonthLabel:              MonthText(figures.SelectedMonth, language),
		Currency:                currency,
		TotalBalance:            figures.TotalBalance,
		AvailableBalance:        figures.AvailableBalance,
		LoanBalance:             figures.LoanBalance,
		HasLoans:                figures.HasLoans,
		CarryForward:            figures.CarryForward,
		OtherMovements:          figures.OtherMovements,
		BalanceDate:             figures.BalanceDate,
		OtherBalances:           otherBalances,
		MonthlyIncome:           figures.MonthlyIncome,
		MonthlyExpenses:         figures.MonthlyExpenses,
		UncategorizedCount:      figures.UncategorizedCount,
		TaxTotal:                taxTotal,
		ExpensesByCategory:      expenseSlicesCopy,
		ExpenseRows:             categoryRows(expenseSlices, language),
		TaxDeductionsByCategory: taxSlicesCopy,
		TaxRows:                 taxRows,
		MonthBars:               monthBars,
		MonthProgress:           float32(figures.MonthProgress),
		TransfersThisMonth:      figures.TransfersThisMonth,
		Budgets:                 budgets,
		Upcoming:                upcoming,
		RecentTransactions:      recentTransactions,
		StatementLines:          statementLines,
		AvailableTitle:          availableTitle,
		AvailableValue:          availableValue,
		AvailableSubtitle:       availableSubtitle(figures, language),
		AvailableTone:           balanceTone(figures.AvailableBalance),
		IncomeTitle: monthTitle(
			currentLoaded,
			i18n.IncomeThisMonth,
			i18n.IncomeInMonth,
			loaded,
			language,
		),
		IncomeValue: format.Money(figures.MonthlyIncome, language, false),
		ExpensesTitle: monthTitle(
			currentLoaded,
			i18n.ExpensesThisMonth,
			i18n.ExpensesInMonth,
			loaded,
			language,
		),
		ExpensesValue:   format.Money(figures.MonthlyExpenses, language, false),
		TaxTitle:        i18n.Text(language, i18n.TaxDeductibleYtd),
		TaxValue:        format.Money(taxTotal, language, false),
		TaxHint:         i18n.Text(language, i18n.TaxDeductibleHint),
		PendingTitle:    i18n.Text(language, i18n.PendingTasks),
		PendingValue:    strconv.Itoa(pending),
		PendingSubtitle: pendingSubtitle,
		PendingLines:    pendingLines,
		PendingEnabled:  pending > 0,
		ExpensesSectionTitle: monthTitle(
			currentLoaded,
			i18n.ExpensesByCategory,
			i18n.ExpensesByCategoryInMonth,
			loaded,
			language,
		),
		ExpensesEmptyText: monthTitle(
			currentLoaded,
			i18n.NoExpensesThisMonth,
			i18n.NoExpensesInMonth,
			loaded,
			language,
		),
		TransfersNote:    transfersNote,
		ChartTitle:       i18n.Text(language, i18n.IncomeVsExpenses),
		ChartEmptyText:   chartEmptyText,
		ChartAxisTop:     chartAxisTop,
		ChartAxisMid:     chartAxisMid,
		ChartAxisZero:    axisZeroLabel(axisTopMinor, currency, language),
		IncomeLegend:     i18n.Text(language, i18n.ChartIncome),
		ExpensesLegend:   i18n.Text(language, i18n.ChartExpenses),
		PartialMonthNote: i18n.Text(language, i18n.ChartPartialMonthNote),
		BudgetsTitle: monthTitle(
			currentLoaded,
			i18n.BudgetsThisMonth,
			i18n.BudgetsInMonth,
			loaded,
			language,
		),
		UpcomingTitle:   i18n.Text(language, i18n.Upcoming30Days),
		RecentTitle:     i18n.Text(language, i18n.RecentTransactions),
		RecentEmptyText: i18n.Text(language, i18n.NoTransactionsYet),
		ViewAllLabel:    i18n.Text(language, i18n.ViewAll),
	}
}

// DashboardState is the Dashboard page's state.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
type DashboardState struct {
	// SelectedMonth is the month the header shows; a load may clamp it.
	SelectedMonth dashboard.YearMonth
	// LoadedMonth is the month whose figures are loaded; nil until the first
	// successful load.
	LoadedMonth *dashboard.YearMonth
	// EarliestMonth is the oldest month with a booking in the primary currency.
	EarliestMonth dashboard.YearMonth
	// CurrentMonth is today's calendar month at the last load.
	CurrentMonth dashboard.YearMonth
	// Months is every selectable month, newest first, from today's month back
	// to the earliest booking.
	Months []dashboard.YearMonth
	// IsLoading says whether a load is running.
	IsLoading bool
	// Error is the last error, already localized.
	Error *string
	// HasAccounts says whether the profile has any account.
	HasAccounts bool
	// View holds the rendered rows and texts once a load has published them.
	View *DashboardView
	// Language is the language for user-visible texts.
	Language i18n.Language
}

// New builds an empty state for language, opening on today's month.
func New(language i18n.Language, today civil.Date) *DashboardState {
	current := dashboard.YearMonthFromDate(today)
	return &DashboardState{
		SelectedMonth: current,
		LoadedMonth:   nil,
		EarliestMonth: current,
		CurrentMonth:  current,
		Months:        []dashboard.YearMonth{current},
		IsLoading:     false,
		Error:         nil,
		HasAccounts:   false,
		View:          nil,
		Language:      language,
	}
}

// IsFirstRun reports whether the page shows the getting-started card: the first
// load finished without accounts.
func (s *DashboardState) IsFirstRun() bool {
	return s.LoadedMonth != nil && !s.HasAccounts
}

// HasFigures reports whether the page has figures other than the first-run card
// or the busy indicator.
func (s *DashboardState) HasFigures() bool {
	return s.View != nil && !s.IsFirstRun()
}

// BeginLoad marks a load as running for month and updates the header
// immediately.
//
// today refreshes the current month first, so a stale CurrentMonth from a
// previous load cannot clamp a newer month. Only the future is cut off here;
// the load clamps the oldest month.
//
// The loaded month's figures stay visible until the new load publishes, exactly
// like FinDesk.
func (s *DashboardState) BeginLoad(month dashboard.YearMonth, today civil.Date) {
	s.CurrentMonth = dashboard.YearMonthFromDate(today)
	s.SelectedMonth = month
	if s.CurrentMonth.Before(s.SelectedMonth) {
		s.SelectedMonth = s.CurrentMonth
	}
	s.Months = dashboard.MonthOptions(s.EarliestMonth, s.CurrentMonth)
	s.IsLoading = true
	s.Error = nil

	if s.View != nil {
		s.View.SelectedMonth = s.SelectedMonth
		s.View.CurrentMonth = s.CurrentMonth
		s.View.MonthLabel = MonthText(s.View.SelectedMonth, s.Language)
		s.View.IsCurrentMonth = s.View.SelectedMonth == s.View.CurrentMonth
		s.View.PreviousEnabled = s.View.SelectedMonth.After(s.View.EarliestMonth)
		s.View.NextEnabled = s.View.SelectedMonth.Before(s.View.CurrentMonth)
	}
}

// Open opens the Dashboard on today's month, as FinDesk's new view model does.
func (s *DashboardState) Open(today civil.Date) {
	s.BeginLoad(dashboard.YearMonthFromDate(today), today)
}

// Load loads the selected month and publishes its figures.
func (s *DashboardState) Load(ctx context.Context, d *db.Database, today civil.Date) error {
	s.BeginLoad(s.SelectedMonth, today)
	figures, err := dashboard.LoadDashboard(ctx, d, today, s.SelectedMonth)
	if err != nil {
		s.IsLoading = false
		message := i18n.Text(s.Language, i18n.ErrorUnexpected)
		s.Error = &message
		return err
	}
	items, err := documents.FindItems(ctx, d, nil, true, nil)
	if err != nil {
		s.IsLoading = false
		message := i18n.Text(s.Language, i18n.ErrorUnexpected)
		s.Error = &message
		return err
	}
	unlinkedDocuments := len(items)
	s.SelectedMonth = figures.SelectedMonth
	loaded := figures.LoadedMonth
	s.LoadedMonth = &loaded
	s.EarliestMonth = figures.EarliestMonth
	s.CurrentMonth = figures.CurrentMonth
	s.Months = dashboard.MonthOptions(s.EarliestMonth, s.CurrentMonth)
	s.HasAccounts = figures.HasAccounts
	view := FromFigures(figures, unlinkedDocuments, s.Language)
	s.View = &view
	s.IsLoading = false
	return nil
}

// SelectMonth selects month and loads it. Only the future is clamped here; the
// load clamps the oldest month.
func (s *DashboardState) SelectMonth(
	ctx context.Context,
	d *db.Database,
	today civil.Date,
	month dashboard.YearMonth,
) error {
	s.BeginLoad(month, today)
	return s.Load(ctx, d, today)
}

// PreviousMonth switches one month back and loads it.
func (s *DashboardState) PreviousMonth(ctx context.Context, d *db.Database, today civil.Date) error {
	return s.SelectMonth(ctx, d, today, s.SelectedMonth.MinusMonths(1))
}

// NextMonth switches one month forward and loads it.
func (s *DashboardState) NextMonth(ctx context.Context, d *db.Database, today civil.Date) error {
	return s.SelectMonth(ctx, d, today, s.SelectedMonth.PlusMonths(1))
}

// SelectCurrentMonth returns to today's month and loads it.
//
// Rust's method is `current_month`; Go cannot give it that name because the
// state also has a CurrentMonth field.
func (s *DashboardState) SelectCurrentMonth(ctx context.Context, d *db.Database, today civil.Date) error {
	return s.SelectMonth(ctx, d, today, dashboard.YearMonthFromDate(today))
}

func monthTitle(
	currentLoaded bool,
	currentKey i18n.Key,
	monthKey i18n.Key,
	month dashboard.YearMonth,
	language i18n.Language,
) string {
	if currentLoaded {
		return i18n.Text(language, currentKey)
	}
	return i18n.Format(language, monthKey, i18n.Str(MonthText(month, language)))
}

// MonthText returns the display label for month, e.g. "November 2026".
func MonthText(month dashboard.YearMonth, language i18n.Language) string {
	name := report.MonthName(language, monthNumber(month.Month))
	runes := []rune(name)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return fmt.Sprintf("%s %d", string(runes), month.Year)
}

// monthNumber maps a validated month to the uint8 the report package takes,
// without an unchecked narrowing conversion.
func monthNumber(month int) uint8 {
	switch month {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 5
	case 6:
		return 6
	case 7:
		return 7
	case 8:
		return 8
	case 9:
		return 9
	case 10:
		return 10
	case 11:
		return 11
	case 12:
		return 12
	default:
		return 0
	}
}

func balanceTone(amount money.Money) BalanceTone {
	if amount.Minor > 0 {
		return BalanceTonePositive
	}
	if amount.Minor < 0 {
		return BalanceToneNegative
	}
	return BalanceToneZero
}

func categorySlices(slices []dashboard.ChartSlice, language i18n.Language) []CategorySliceView {
	var total int64
	for _, slice := range slices {
		total += slice.Amount.Minor
	}
	views := make([]CategorySliceView, 0, len(slices))
	for _, slice := range slices {
		label := slice.Label
		if slice.CategoryID == nil && slice.Label == dashboard.UncategorizedLabel {
			label = i18n.Text(language, i18n.Uncategorized)
		}
		color := dashboard.UncategorizedColor
		if slice.Color != nil {
			color = *slice.Color
		}
		views = append(views, CategorySliceView{
			Label:      label,
			Amount:     slice.Amount,
			Color:      color,
			CategoryID: slice.CategoryID,
			Fraction:   fraction(slice.Amount.Minor, total),
		})
	}
	return views
}

func categoryRows(slices []CategorySliceView, language i18n.Language) []CategoryRowView {
	if len(slices) == 0 {
		return make([]CategoryRowView, 0)
	}

	var total int64
	amounts := make([]money.Money, 0, len(slices))
	for _, slice := range slices {
		total += slice.Amount.Minor
		amounts = append(amounts, slice.Amount)
	}
	shares := format.Shares(amounts)
	visible := len(slices)
	if visible > 8 {
		visible = 8
	}
	rows := make([]CategoryRowView, 0, visible+1)
	for index := 0; index < visible; index++ {
		slice := slices[index]
		rows = append(rows, CategoryRowView{
			Label:       slice.Label,
			PercentText: format.Percent(float64(shares[index])/100.0, language),
			AmountText:  format.Money(slice.Amount, language, false),
			Color:       slice.Color,
			CategoryID:  slice.CategoryID,
			IsMore:      false,
			Fraction:    fraction(slice.Amount.Minor, total),
		})
	}

	if len(slices) > visible {
		rest := slices[visible:]
		var restTotal int64
		for _, slice := range rest {
			restTotal += slice.Amount.Minor
		}
		var restPercent int64
		for _, share := range shares[visible:] {
			restPercent += share
		}
		currency := rest[0].Amount.Currency
		rows = append(rows, CategoryRowView{
			Label: i18n.Format(
				language,
				i18n.CategorySplitMore,
				i18n.Int(int64(len(rest))),
			),
			PercentText: format.Percent(float64(restPercent)/100.0, language),
			AmountText: format.Money(
				money.Money{Minor: restTotal, Currency: currency},
				language,
				false,
			),
			// The split renders the folded row in the theme's neutral divider
			// colour.
			Color:      "",
			CategoryID: nil,
			IsMore:     true,
			Fraction:   fraction(restTotal, total),
		})
	}

	return rows
}

func monthBars(months []dashboard.MonthBar, language i18n.Language) []MonthBarView {
	var top int64
	for _, bar := range months {
		if bar.Income.Minor > top {
			top = bar.Income.Minor
		}
		if bar.Expenses.Minor > top {
			top = bar.Expenses.Minor
		}
	}
	top = niceCeilingMinor(top)

	views := make([]MonthBarView, 0, len(months))
	for _, bar := range months {
		expensesText := format.Money(
			money.Money{
				Minor:    -absInt64(bar.Expenses.Minor),
				Currency: bar.Expenses.Currency,
			},
			language,
			false,
		)
		views = append(views, MonthBarView{
			Year:             bar.Year,
			Month:            bar.Month,
			Label:            report.MonthShortName(language, monthNumber(bar.Month)),
			Title:            MonthText(dashboard.NewYearMonth(bar.Year, bar.Month), language),
			Income:           bar.Income,
			Expenses:         bar.Expenses,
			IncomeText:       format.Money(bar.Income, language, true),
			ExpensesText:     expensesText,
			IncomeFraction:   fraction(bar.Income.Minor, top),
			ExpensesFraction: fraction(bar.Expenses.Minor, top),
			Partial:          bar.Partial,
		})
	}
	return views
}

func fraction(amount int64, total int64) float32 {
	if total <= 0 {
		return 0.0
	}
	return float32(amount) / float32(total)
}

func niceCeilingMinor(value int64) int64 {
	if value <= 0 {
		return 0
	}

	magnitude := int64(1)
	for magnitude <= value/10 {
		magnitude *= 10
	}

	for _, factor := range []int64{1, 2, 5, 10} {
		if magnitude > math.MaxInt64/factor {
			continue
		}
		candidate := magnitude * factor
		if candidate >= value {
			return candidate
		}
	}
	return value
}

func axisLabel(minor int64, currency money.Currency, language i18n.Language) string {
	amount := money.Money{Minor: minor, Currency: currency}
	unit := pow10(currency.MinorExponent())
	if minor < 10*unit {
		return format.Money(amount, language, false)
	}
	return format.WholeMoney(amount, language)
}

// axisZeroLabel follows the top label's style: whole euros when the axis uses
// whole euros.
func axisZeroLabel(topMinor int64, currency money.Currency, language i18n.Language) string {
	amount := money.Money{Minor: 0, Currency: currency}
	unit := pow10(currency.MinorExponent())
	if topMinor >= 10*unit {
		return format.WholeMoney(amount, language)
	}
	return format.Money(amount, language, false)
}

func pow10(exponent uint32) int64 {
	result := int64(1)
	for index := uint32(0); index < exponent; index++ {
		result *= 10
	}
	return result
}

func sumMoney(amounts []money.Money, currency money.Currency) money.Money {
	var total int64
	for _, amount := range amounts {
		total += amount.Minor
	}
	return money.Money{Minor: total, Currency: currency}
}

func slicesAmounts(slices []CategorySliceView) []money.Money {
	amounts := make([]money.Money, 0, len(slices))
	for _, slice := range slices {
		amounts = append(amounts, slice.Amount)
	}
	return amounts
}

func statementLines(
	figures dashboard.DashboardFigures,
	availableTitle string,
	language i18n.Language,
) []StatementLineView {
	lines := make([]StatementLineView, 0, 5)

	if figures.CarryForward != nil {
		lines = append(lines, StatementLineView{
			Label: i18n.Format(
				language,
				i18n.CarryForwardLine,
				i18n.Str(MonthText(figures.LoadedMonth.MinusMonths(1), language)),
			),
			AmountText: format.Money(*figures.CarryForward, language, false),
			Kind:       StatementLineKindCarry,
		})
	}

	lines = append(lines, StatementLineView{
		Label:      i18n.Text(language, i18n.DirectionIncome),
		AmountText: incomeAmount(figures.MonthlyIncome, language),
		Kind:       StatementLineKindIn,
	})

	lines = append(lines, StatementLineView{
		Label: i18n.Text(language, i18n.DirectionExpenses),
		AmountText: format.Money(
			money.Money{
				Minor:    -absInt64(figures.MonthlyExpenses.Minor),
				Currency: figures.MonthlyExpenses.Currency,
			},
			language,
			false,
		),
		Kind: StatementLineKindOut,
	})

	if figures.OtherMovements != nil && figures.OtherMovements.Minor != 0 {
		lines = append(lines, StatementLineView{
			Label:      i18n.Text(language, i18n.OtherMovementsLabel),
			AmountText: format.Money(*figures.OtherMovements, language, true),
			Kind:       StatementLineKindMovement,
		})
	}

	lines = append(lines, StatementLineView{
		Label:      availableTitle,
		AmountText: format.Money(figures.AvailableBalance, language, false),
		Kind:       StatementLineKindResult,
	})

	return lines
}

// incomeAmount prints income with a leading `+`, including zero: "+0,00 €".
func incomeAmount(income money.Money, language i18n.Language) string {
	text := format.Money(income, language, true)
	if income.Minor == 0 {
		return "+" + text
	}
	return text
}

func availableSubtitle(figures dashboard.DashboardFigures, language i18n.Language) string {
	lines := make([]string, 0, 2)

	if figures.HasLoans {
		lines = append(lines, i18n.Format(
			language,
			i18n.LoansSubtitle,
			i18n.Str(format.Money(figures.LoanBalance, language, false)),
			i18n.Str(format.Money(figures.TotalBalance, language, false)),
		))
	}

	if len(figures.OtherBalances) > 0 {
		amounts := make([]string, 0, len(figures.OtherBalances))
		for _, amount := range figures.OtherBalances {
			amounts = append(amounts, format.Money(amount, language, false))
		}
		lines = append(lines, i18n.Format(
			language,
			i18n.PlusOtherCurrencies,
			i18n.Str(strings.Join(amounts, " · ")),
		))
	}

	return strings.Join(lines, "\n")
}

func upcomingMeta(payment recurring.RecurringPayment, language i18n.Language) string {
	var cadence i18n.Key
	switch payment.Cadence {
	case recurring.Weekly:
		cadence = i18n.CadenceWeekly
	case recurring.Monthly:
		cadence = i18n.CadenceMonthly
	case recurring.Quarterly:
		cadence = i18n.CadenceQuarterly
	case recurring.Yearly:
		cadence = i18n.CadenceYearly
	}
	meta := i18n.Text(language, cadence)
	if payment.CategoryName != nil {
		meta += " · "
		meta += *payment.CategoryName
	}
	if !payment.FixedAmount {
		meta += " · "
		meta += i18n.Text(language, i18n.AmountVaries)
	}
	return meta
}

func budgetRow(budget dashboard.BudgetStatus, language i18n.Language, monthProgress float64) BudgetRowView {
	over := budget.IsOver()
	ratio := budget.Ratio()
	var rest money.Money
	if over {
		rest = money.Money{
			Minor:    budget.Spent.Minor - budget.Limit.Minor,
			Currency: budget.Limit.Currency,
		}
	} else {
		rest = budget.Remaining()
	}
	var restText string
	if over {
		restText = i18n.Format(
			language,
			i18n.OverBudget,
			i18n.Str(format.Money(rest, language, false)),
		)
	} else {
		restText = i18n.Format(
			language,
			i18n.LeftBudget,
			i18n.Str(format.Money(rest, language, false)),
		)
	}

	return BudgetRowView{
		CategoryID: budget.CategoryID,
		Name:       budget.CategoryName,
		Color:      budget.Color,
		Limit:      budget.Limit,
		Spent:      budget.Spent,
		Ratio:      float32(math.Max(0.0, math.Min(1.0, ratio))),
		IsOver:     over,
		Warning:    !over && ratio > monthProgress+0.1,
		AmountText: i18n.Format(
			language,
			i18n.SpentOfLimit,
			i18n.Str(format.Money(budget.Spent, language, false)),
			i18n.Str(format.Money(budget.Limit, language, false)),
		),
		RestText: restText,
	}
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
