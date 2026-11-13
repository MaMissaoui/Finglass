package bind

import (
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/dashboardview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
)

// DashboardEvent is the name of the event emitted after every Dashboard change.
const DashboardEvent = "dashboard:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[DashboardDTO](DashboardEvent)
}

var errDashboardNotStarted = errors.New("dashboard is not started")

// DashboardStatementLineDTO is one line of the month statement.
type DashboardStatementLineDTO struct {
	Label      string `json:"label"`
	AmountText string `json:"amountText"`
	Kind       string `json:"kind"`
}

// DashboardCategoryRowDTO is one legend row of the spending split or the tax
// chart. CategoryID is -1 when the row opens the uncategorized list.
type DashboardCategoryRowDTO struct {
	Label       string  `json:"label"`
	PercentText string  `json:"percentText"`
	AmountText  string  `json:"amountText"`
	Colour      string  `json:"colour"`
	CategoryID  int     `json:"categoryId"`
	IsMore      bool    `json:"isMore"`
	Fraction    float32 `json:"fraction"`
}

// DashboardMonthBarDTO is one month of the income-vs-expenses chart.
type DashboardMonthBarDTO struct {
	Label            string  `json:"label"`
	Title            string  `json:"title"`
	IncomeText       string  `json:"incomeText"`
	ExpensesText     string  `json:"expensesText"`
	IncomeFraction   float32 `json:"incomeFraction"`
	ExpensesFraction float32 `json:"expensesFraction"`
	Partial          bool    `json:"partial"`
}

// DashboardBudgetRowDTO is one budget row.
type DashboardBudgetRowDTO struct {
	Name       string  `json:"name"`
	AmountText string  `json:"amountText"`
	RestText   string  `json:"restText"`
	Ratio      float32 `json:"ratio"`
	Over       bool    `json:"over"`
	Warning    bool    `json:"warning"`
	Colour     string  `json:"colour"`
}

// DashboardUpcomingRowDTO is one recurring payment due soon.
type DashboardUpcomingRowDTO struct {
	DateText   string `json:"dateText"`
	Title      string `json:"title"`
	Meta       string `json:"meta"`
	AmountText string `json:"amountText"`
	Expense    bool   `json:"expense"`
}

// DashboardTransactionRowDTO is one recent booking. Partner is the value
// OpenPartner takes; "" opens the full list instead.
type DashboardTransactionRowDTO struct {
	DateText   string `json:"dateText"`
	Title      string `json:"title"`
	Meta       string `json:"meta"`
	AmountText string `json:"amountText"`
	Expense    bool   `json:"expense"`
	Partner    string `json:"partner"`
}

// DashboardDTO is everything the Dashboard page shows.
//
// Money is already formatted by dashboardview; the chart fractions and the
// budget ratio stay numbers. Tones and statement kinds are the strings Rust's
// tone_code and StatementLineKind mapping return.
type DashboardDTO struct {
	Loading       bool     `json:"loading"`
	FirstRun      bool     `json:"firstRun"`
	ShowContent   bool     `json:"showContent"`
	ErrorText     string   `json:"errorText"`
	MonthOptions  []string `json:"monthOptions"`
	MonthSelected int      `json:"monthSelected"`

	MonthLabel      string `json:"monthLabel"`
	PreviousEnabled bool   `json:"previousEnabled"`
	NextEnabled     bool   `json:"nextEnabled"`
	IsCurrentMonth  bool   `json:"isCurrentMonth"`

	AvailableTitle    string                      `json:"availableTitle"`
	AvailableValue    string                      `json:"availableValue"`
	AvailableSubtitle string                      `json:"availableSubtitle"`
	AvailableTone     string                      `json:"availableTone"`
	StatementLines    []DashboardStatementLineDTO `json:"statementLines"`

	IncomeTitle   string `json:"incomeTitle"`
	IncomeValue   string `json:"incomeValue"`
	ExpensesTitle string `json:"expensesTitle"`
	ExpensesValue string `json:"expensesValue"`

	PendingTitle    string   `json:"pendingTitle"`
	PendingValue    string   `json:"pendingValue"`
	PendingSubtitle string   `json:"pendingSubtitle"`
	PendingLines    []string `json:"pendingLines"`
	PendingEnabled  bool     `json:"pendingEnabled"`

	ExpensesSectionTitle string                    `json:"expensesSectionTitle"`
	ExpensesEmptyText    string                    `json:"expensesEmptyText"`
	ExpenseRows          []DashboardCategoryRowDTO `json:"expenseRows"`
	TransfersNote        string                    `json:"transfersNote"`

	ChartTitle     string                 `json:"chartTitle"`
	ChartEmptyText string                 `json:"chartEmptyText"`
	ChartAxisTop   string                 `json:"chartAxisTop"`
	ChartAxisMid   string                 `json:"chartAxisMid"`
	ChartAxisZero  string                 `json:"chartAxisZero"`
	MonthBars      []DashboardMonthBarDTO `json:"monthBars"`
	IncomeLegend   string                 `json:"incomeLegend"`
	ExpensesLegend string                 `json:"expensesLegend"`
	PartialNote    string                 `json:"partialNote"`

	BudgetsTitle  string                  `json:"budgetsTitle"`
	BudgetRows    []DashboardBudgetRowDTO `json:"budgetRows"`
	HasBudgets    bool                    `json:"hasBudgets"`
	MonthProgress float32                 `json:"monthProgress"`

	TaxTitle string                    `json:"taxTitle"`
	TaxHint  string                    `json:"taxHint"`
	TaxValue string                    `json:"taxValue"`
	TaxRows  []DashboardCategoryRowDTO `json:"taxRows"`

	UpcomingTitle string                    `json:"upcomingTitle"`
	UpcomingRows  []DashboardUpcomingRowDTO `json:"upcomingRows"`
	HasUpcoming   bool                      `json:"hasUpcoming"`

	RecentTitle     string                       `json:"recentTitle"`
	RecentEmptyText string                       `json:"recentEmptyText"`
	RecentRows      []DashboardTransactionRowDTO `json:"recentRows"`
	ViewAllLabel    string                       `json:"viewAllLabel"`
}

// DashboardOptions is the process wiring the caller supplies.
type DashboardOptions struct {
	// Emit publishes dashboard:changed; nil uses the running Wails application.
	Emit func(DashboardDTO)
}

// Dashboard is the Dashboard page's Wails service.
//
// The router calls publishPage after the session reports a Dashboard change.
type Dashboard struct {
	session *shell.Session
	emit    func(DashboardDTO)
}

// NewDashboard builds the Dashboard service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewDashboard(session *shell.Session, options DashboardOptions) *Dashboard {
	return &Dashboard{
		session: session,
		emit:    options.Emit,
	}
}

// ServiceName implements application.ServiceName.
func (d *Dashboard) ServiceName() string {
	return "Dashboard"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (d *Dashboard) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Dashboard state.
func (d *Dashboard) publishPage() {
	d.emitDTO(d.snapshot())
}

// State returns the current Dashboard state without changing anything.
func (d *Dashboard) State() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, nil
	}
	return d.snapshot(), nil
}

// PreviousMonth opens the month before the selected one.
func (d *Dashboard) PreviousMonth() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardPreviousMonth()
	return d.publish(), nil
}

// NextMonth opens the month after the selected one.
func (d *Dashboard) NextMonth() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardNextMonth()
	return d.publish(), nil
}

// CurrentMonth returns to today's month.
func (d *Dashboard) CurrentMonth() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardCurrentMonth()
	return d.publish(), nil
}

// SelectMonth opens the month at index in the month list. An unknown index
// returns an error and changes nothing.
func (d *Dashboard) SelectMonth(index int) (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	if !d.session.DashboardSelectMonth(index) {
		return d.snapshot(), fmt.Errorf("unknown dashboard month index %d", index)
	}
	return d.publish(), nil
}

// DismissError clears the page's error text.
func (d *Dashboard) DismissError() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardDismissError()
	return d.snapshot(), nil
}

// OpenCategory opens Transactions filtered to categoryID, or to the
// uncategorized bookings when it is negative.
func (d *Dashboard) OpenCategory(categoryID int) (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenCategory(categoryID)
	return d.snapshot(), nil
}

// OpenUncategorized opens the uncategorized bookings.
func (d *Dashboard) OpenUncategorized() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenUncategorized()
	return d.snapshot(), nil
}

// OpenAll opens Transactions with no filter.
func (d *Dashboard) OpenAll() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenAll()
	return d.snapshot(), nil
}

// OpenPartner opens Transactions searching for partner.
func (d *Dashboard) OpenPartner(partner string) (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenPartner(partner)
	return d.snapshot(), nil
}

// OpenAccounts opens Accounts.
func (d *Dashboard) OpenAccounts() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenAccounts()
	return d.snapshot(), nil
}

// OpenSettings opens Settings on the categories section.
func (d *Dashboard) OpenSettings() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenSettings()
	return d.snapshot(), nil
}

// OpenTax opens Reports on the loaded month's tax year.
func (d *Dashboard) OpenTax() (DashboardDTO, error) {
	if d.session == nil {
		return DashboardDTO{}, errDashboardNotStarted
	}
	d.session.DashboardOpenTax()
	return d.snapshot(), nil
}

func (d *Dashboard) publish() DashboardDTO {
	dto := d.snapshot()
	d.emitDTO(dto)
	return dto
}

func (d *Dashboard) emitDTO(dto DashboardDTO) {
	if d.emit != nil {
		d.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(DashboardEvent, dto)
	}
}

func (d *Dashboard) snapshot() DashboardDTO {
	if d.session == nil {
		return DashboardDTO{}
	}
	return dashboardDTO(d.session.DashboardState())
}

func dashboardDTO(state dashboardview.DashboardState) DashboardDTO {
	dto := DashboardDTO{
		Loading:       state.IsLoading,
		FirstRun:      state.IsFirstRun(),
		ShowContent:   state.HasFigures(),
		MonthOptions:  make([]string, 0, len(state.Months)),
		MonthSelected: 0,
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	for index, month := range state.Months {
		dto.MonthOptions = append(dto.MonthOptions, dashboardview.MonthText(month, state.Language))
		if month == state.SelectedMonth {
			dto.MonthSelected = index
		}
	}

	view := state.View
	if view == nil {
		return dto
	}

	dto.MonthLabel = view.MonthLabel
	dto.PreviousEnabled = view.PreviousEnabled
	dto.NextEnabled = view.NextEnabled
	dto.IsCurrentMonth = view.IsCurrentMonth

	dto.AvailableTitle = view.AvailableTitle
	dto.AvailableValue = view.AvailableValue
	dto.AvailableSubtitle = view.AvailableSubtitle
	dto.AvailableTone = toneCode(view.AvailableTone)
	dto.StatementLines = statementLineDTOs(view.StatementLines)

	dto.IncomeTitle = view.IncomeTitle
	dto.IncomeValue = view.IncomeValue
	dto.ExpensesTitle = view.ExpensesTitle
	dto.ExpensesValue = view.ExpensesValue

	dto.PendingTitle = view.PendingTitle
	dto.PendingValue = view.PendingValue
	dto.PendingSubtitle = view.PendingSubtitle
	dto.PendingLines = append([]string(nil), view.PendingLines...)
	dto.PendingEnabled = view.PendingEnabled

	dto.ExpensesSectionTitle = view.ExpensesSectionTitle
	dto.ExpensesEmptyText = view.ExpensesEmptyText
	dto.ExpenseRows = categoryRowDTOs(view.ExpenseRows)
	if view.TransfersNote != nil {
		dto.TransfersNote = *view.TransfersNote
	}

	dto.ChartTitle = view.ChartTitle
	if view.ChartEmptyText != nil {
		dto.ChartEmptyText = *view.ChartEmptyText
	}
	dto.ChartAxisTop = view.ChartAxisTop
	dto.ChartAxisMid = view.ChartAxisMid
	dto.ChartAxisZero = view.ChartAxisZero
	dto.MonthBars = monthBarDTOs(view.MonthBars)
	dto.IncomeLegend = view.IncomeLegend
	dto.ExpensesLegend = view.ExpensesLegend
	dto.PartialNote = view.PartialMonthNote

	dto.BudgetsTitle = view.BudgetsTitle
	dto.BudgetRows = budgetRowDTOs(view.Budgets)
	dto.HasBudgets = len(view.Budgets) > 0
	dto.MonthProgress = view.MonthProgress

	dto.TaxTitle = view.TaxTitle
	dto.TaxHint = view.TaxHint
	dto.TaxValue = view.TaxValue
	dto.TaxRows = categoryRowDTOs(view.TaxRows)

	dto.UpcomingTitle = view.UpcomingTitle
	dto.UpcomingRows = upcomingRowDTOs(view.Upcoming)
	dto.HasUpcoming = len(view.Upcoming) > 0

	dto.RecentTitle = view.RecentTitle
	dto.RecentEmptyText = view.RecentEmptyText
	dto.RecentRows = transactionRowDTOs(view.RecentTransactions)
	dto.ViewAllLabel = view.ViewAllLabel

	return dto
}

func toneCode(tone dashboardview.BalanceTone) string {
	switch tone {
	case dashboardview.BalanceToneZero:
		return "zero"
	case dashboardview.BalanceToneNegative:
		return "negative"
	default:
		return "positive"
	}
}

func statementKindCode(kind dashboardview.StatementLineKind) string {
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

func statementLineDTOs(lines []dashboardview.StatementLineView) []DashboardStatementLineDTO {
	rows := make([]DashboardStatementLineDTO, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, DashboardStatementLineDTO{
			Label:      line.Label,
			AmountText: line.AmountText,
			Kind:       statementKindCode(line.Kind),
		})
	}
	return rows
}

func categoryRowDTOs(rows []dashboardview.CategoryRowView) []DashboardCategoryRowDTO {
	result := make([]DashboardCategoryRowDTO, 0, len(rows))
	for _, row := range rows {
		categoryID := -1
		if row.CategoryID != nil {
			categoryID = int(*row.CategoryID)
		}
		result = append(result, DashboardCategoryRowDTO{
			Label:       row.Label,
			PercentText: row.PercentText,
			AmountText:  row.AmountText,
			Colour:      row.Color,
			CategoryID:  categoryID,
			IsMore:      row.IsMore,
			Fraction:    row.Fraction,
		})
	}
	return result
}

func monthBarDTOs(bars []dashboardview.MonthBarView) []DashboardMonthBarDTO {
	result := make([]DashboardMonthBarDTO, 0, len(bars))
	for _, bar := range bars {
		result = append(result, DashboardMonthBarDTO{
			Label:            bar.Label,
			Title:            bar.Title,
			IncomeText:       bar.IncomeText,
			ExpensesText:     bar.ExpensesText,
			IncomeFraction:   bar.IncomeFraction,
			ExpensesFraction: bar.ExpensesFraction,
			Partial:          bar.Partial,
		})
	}
	return result
}

func budgetRowDTOs(rows []dashboardview.BudgetRowView) []DashboardBudgetRowDTO {
	result := make([]DashboardBudgetRowDTO, 0, len(rows))
	for _, row := range rows {
		result = append(result, DashboardBudgetRowDTO{
			Name:       row.Name,
			AmountText: row.AmountText,
			RestText:   row.RestText,
			Ratio:      row.Ratio,
			Over:       row.IsOver,
			Warning:    row.Warning,
			Colour:     row.Color,
		})
	}
	return result
}

func upcomingRowDTOs(rows []dashboardview.UpcomingRowView) []DashboardUpcomingRowDTO {
	result := make([]DashboardUpcomingRowDTO, 0, len(rows))
	for _, row := range rows {
		result = append(result, DashboardUpcomingRowDTO{
			DateText:   row.DateText,
			Title:      row.Name,
			Meta:       row.Meta,
			AmountText: row.AmountText,
			Expense:    !row.IsIncome,
		})
	}
	return result
}

func transactionRowDTOs(rows []dashboardview.TransactionRowView) []DashboardTransactionRowDTO {
	result := make([]DashboardTransactionRowDTO, 0, len(rows))
	for _, row := range rows {
		partner := ""
		if row.Partner != nil {
			partner = *row.Partner
		}
		result = append(result, DashboardTransactionRowDTO{
			DateText:   row.DateText,
			Title:      row.Title,
			Meta:       row.Meta,
			AmountText: row.AmountText,
			Expense:    row.IsExpense,
			Partner:    partner,
		})
	}
	return result
}
