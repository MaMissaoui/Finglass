package bind

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MaMissaoui/Finglass/internal/app/reportsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// ReportsEvent is the name of the event emitted after every Reports change.
const ReportsEvent = "reports:changed"

func init() {
	// Registering the event here gives the generator the name and the payload
	// type, so the frontend binds both at compile time.
	application.RegisterEvent[ReportsDTO](ReportsEvent)
}

var errReportsNotStarted = errors.New("reports is not started")

// ReportAmountRowDTO is one row of the tax-relevant amount list.
type ReportAmountRowDTO struct {
	Label      string  `json:"label"`
	AmountText string  `json:"amountText"`
	Fraction   float64 `json:"fraction"`
}

// ReportMonthlyRowDTO is one month of the monthly breakdown.
type ReportMonthlyRowDTO struct {
	Month        string `json:"month"`
	IncomeText   string `json:"incomeText"`
	ExpensesText string `json:"expensesText"`
	NetText      string `json:"netText"`
	IncomeZero   bool   `json:"incomeZero"`
	ExpensesZero bool   `json:"expensesZero"`
	NetZero      bool   `json:"netZero"`
	NetTone      string `json:"netTone"`
}

// ReportVendorRowDTO is one top-vendor row.
type ReportVendorRowDTO struct {
	Rank       string `json:"rank"`
	Name       string `json:"name"`
	AmountText string `json:"amountText"`
}

// ReportCategoryRowDTO is one category share row.
type ReportCategoryRowDTO struct {
	Label       string  `json:"label"`
	PercentText string  `json:"percentText"`
	AmountText  string  `json:"amountText"`
	Colour      string  `json:"colour"`
	CategoryID  int     `json:"categoryId"`
	IsMore      bool    `json:"isMore"`
	Fraction    float64 `json:"fraction"`
}

// ReportRecurringRowDTO is one recurring-payment row.
type ReportRecurringRowDTO struct {
	Name       string `json:"name"`
	Meta       string `json:"meta"`
	Cadence    string `json:"cadence"`
	NextText   string `json:"nextText"`
	AmountText string `json:"amountText"`
	YearlyText string `json:"yearlyText"`
	Income     bool   `json:"income"`
}

// ReportSuggestionRowDTO is one tax-suggestion row.
type ReportSuggestionRowDTO struct {
	ID          int    `json:"id"`
	DateText    string `json:"dateText"`
	Title       string `json:"title"`
	KeywordText string `json:"keywordText"`
	AmountText  string `json:"amountText"`
	Expense     bool   `json:"expense"`
}

// ReportTaxRowDTO is one flattened Tax bookings row: a section, a group or a
// booking.
type ReportTaxRowDTO struct {
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	Colour         string `json:"colour"`
	Summary        string `json:"summary"`
	ID             int    `json:"id"`
	DateText       string `json:"dateText"`
	AccountText    string `json:"accountText"`
	AmountText     string `json:"amountText"`
	Expense        bool   `json:"expense"`
	HasReceipt     bool   `json:"hasReceipt"`
	MissingReceipt bool   `json:"missingReceipt"`
	CanReset       bool   `json:"canReset"`
}

// ReportsDTO is everything the Reports page shows.
//
// Every value Rust's apply, apply_recurring and apply_tax_bookings set has a
// field here. Money is already formatted by the page; chart and bar geometry
// stay numbers.
type ReportsDTO struct {
	Tab       string `json:"tab"`
	Year      int    `json:"year"`
	YearLabel string `json:"yearLabel"`
	Loading   bool   `json:"loading"`
	Exporting bool   `json:"exporting"`

	RangeFrom  string `json:"rangeFrom"`
	RangeTo    string `json:"rangeTo"`
	RangeError string `json:"rangeError"`
	Custom     bool   `json:"custom"`

	ErrorText   string `json:"errorText"`
	MessageText string `json:"messageText"`

	HasReport            bool   `json:"hasReport"`
	Empty                bool   `json:"empty"`
	CanExportReport      bool   `json:"canExportReport"`
	ExportDisabledReason string `json:"exportDisabledReason"`
	CanExportTax         bool   `json:"canExportTax"`
	Linkable             bool   `json:"linkable"`

	EmptyTitle string `json:"emptyTitle"`
	EmptyHint  string `json:"emptyHint"`

	OtherCurrencyHint string `json:"otherCurrencyHint"`
	IncomeValue       string `json:"incomeValue"`
	ExpensesValue     string `json:"expensesValue"`
	NetValue          string `json:"netValue"`
	TaxValue          string `json:"taxValue"`
	NetTone           string `json:"netTone"`

	TaxRelevantRows  []ReportAmountRowDTO `json:"taxRelevantRows"`
	TaxRelevantEmpty string               `json:"taxRelevantEmpty"`

	StatTransactions string `json:"statTransactions"`
	StatCategories   string `json:"statCategories"`
	StatAverage      string `json:"statAverage"`
	StatHighest      string `json:"statHighest"`

	MonthlyTitle             string                `json:"monthlyTitle"`
	MonthlyRows              []ReportMonthlyRowDTO `json:"monthlyRows"`
	MonthlyTotalIncome       string                `json:"monthlyTotalIncome"`
	MonthlyTotalExpenses     string                `json:"monthlyTotalExpenses"`
	MonthlyTotalNet          string                `json:"monthlyTotalNet"`
	MonthlyTotalTone         string                `json:"monthlyTotalTone"`
	MonthlyTotalIncomeZero   bool                  `json:"monthlyTotalIncomeZero"`
	MonthlyTotalExpensesZero bool                  `json:"monthlyTotalExpensesZero"`
	MonthlyTotalNetZero      bool                  `json:"monthlyTotalNetZero"`

	CategoryRows  []ReportCategoryRowDTO `json:"categoryRows"`
	TransfersNote string                 `json:"transfersNote"`
	VendorRows    []ReportVendorRowDTO   `json:"vendorRows"`

	RecurringEmpty   bool                    `json:"recurringEmpty"`
	RecurringCount   string                  `json:"recurringCount"`
	RecurringYearly  string                  `json:"recurringYearly"`
	RecurringMonthly string                  `json:"recurringMonthly"`
	RecurringRows    []ReportRecurringRowDTO `json:"recurringRows"`

	HasTax              bool   `json:"hasTax"`
	MissingReceiptsOnly bool   `json:"missingReceiptsOnly"`
	TaxEmpty            bool   `json:"taxEmpty"`
	TaxEmptyTitle       string `json:"taxEmptyTitle"`
	TaxEmptyHint        string `json:"taxEmptyHint"`

	TaxIncomeValue      string `json:"taxIncomeValue"`
	TaxDeductionValue   string `json:"taxDeductionValue"`
	TaxNetValue         string `json:"taxNetValue"`
	TaxNetTone          string `json:"taxNetTone"`
	TaxReceiptsMissing  bool   `json:"taxReceiptsMissing"`
	TaxReceiptsValue    string `json:"taxReceiptsValue"`
	TaxReceiptsSubtitle string `json:"taxReceiptsSubtitle"`
	TaxOtherHint        string `json:"taxOtherHint"`

	TaxSuggestionsTitle string                   `json:"taxSuggestionsTitle"`
	TaxSuggestionsHint  string                   `json:"taxSuggestionsHint"`
	TaxSuggestions      []ReportSuggestionRowDTO `json:"taxSuggestions"`
	TaxRows             []ReportTaxRowDTO        `json:"taxRows"`
}

// ReportsOptions is the process wiring the caller supplies.
type ReportsOptions struct {
	// Emit publishes reports:changed; nil uses the running Wails application.
	Emit func(ReportsDTO)
}

// Reports is the Reports page's Wails service.
type Reports struct {
	session *shell.Session
	emit    func(ReportsDTO)
}

// NewReports builds the Reports service. session may be nil only for the
// self-check, which inspects the registry without calling the service.
func NewReports(session *shell.Session, options ReportsOptions) *Reports {
	return &Reports{session: session, emit: options.Emit}
}

// ServiceName implements application.ServiceName.
func (r *Reports) ServiceName() string {
	return "Reports"
}

// ServiceShutdown is a no-op: the router owns the one session listener.
func (r *Reports) ServiceShutdown() error {
	return nil
}

// publishPage emits the current Reports state.
func (r *Reports) publishPage() {
	r.emitDTO(r.snapshot())
}

// State returns the current Reports state without changing anything.
func (r *Reports) State() (ReportsDTO, error) {
	if r.session == nil {
		return ReportsDTO{}, nil
	}
	return r.snapshot(), nil
}

// ChangeYear moves the selected calendar year by delta.
func (r *Reports) ChangeYear(delta int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsChangeYear(delta) })
}

// SetCustom leaves the custom range, or clears its error when custom is true.
func (r *Reports) SetCustom(custom bool) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsSetCustom(custom) })
}

// ApplyRange validates and applies the custom range.
func (r *Reports) ApplyRange(from string, to string) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsApplyRange(from, to) })
}

// SetMissingOnly filters the Tax bookings rows to bookings without a receipt.
func (r *Reports) SetMissingOnly(only bool) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsSetMissingOnly(only) })
}

// Dismiss clears the error and success banner.
func (r *Reports) Dismiss() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsDismiss() })
}

// AcceptSuggestion marks the suggested booking tax relevant.
func (r *Reports) AcceptSuggestion(id int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsAcceptSuggestion(model.TransactionID(id)) })
}

// DismissSuggestion stores a "not relevant" override for the suggestion.
func (r *Reports) DismissSuggestion(id int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsDismissSuggestion(model.TransactionID(id)) })
}

// MarkNotTaxRelevant marks one tax row's booking not tax relevant.
func (r *Reports) MarkNotTaxRelevant(id int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsMarkNotTaxRelevant(model.TransactionID(id)) })
}

// ResetTaxAutomatic clears one booking's manual tax override.
func (r *Reports) ResetTaxAutomatic(id int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsResetTaxAutomatic(model.TransactionID(id)) })
}

// ExportReportPDF asks for a target and writes the tax summary PDF.
func (r *Reports) ExportReportPDF() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsExportReportPDF() })
}

// ExportCSV asks for a target and writes the transaction CSV.
func (r *Reports) ExportCSV() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsExportCSV() })
}

// ExportTaxXLSX asks for a target and writes the Tax bookings workbook.
func (r *Reports) ExportTaxXLSX() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsExportTaxXLSX() })
}

// ExportTaxPDF asks for a target and writes the Tax bookings PDF.
func (r *Reports) ExportTaxPDF() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsExportTaxPDF() })
}

// PrintTax writes the Tax bookings PDF and opens it with the system viewer.
func (r *Reports) PrintTax() (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsPrintTax() })
}

// OpenCategory opens Transactions filtered to one category, or to the
// uncategorized bookings when categoryID is negative.
func (r *Reports) OpenCategory(categoryID int) (ReportsDTO, error) {
	return r.action(func() { r.session.ReportsOpenCategory(categoryID) })
}

func (r *Reports) action(run func()) (ReportsDTO, error) {
	if r.session == nil {
		return ReportsDTO{}, errReportsNotStarted
	}
	run()
	return r.snapshot(), nil
}

func (r *Reports) emitDTO(dto ReportsDTO) {
	if r.emit != nil {
		r.emit(dto)
	} else if app := application.Get(); app != nil {
		app.Event.Emit(ReportsEvent, dto)
	}
}

func (r *Reports) snapshot() ReportsDTO {
	if r.session == nil {
		return ReportsDTO{}
	}
	return reportsDTO(r.session.ReportsState())
}

func reportsDTO(page shell.ReportsPageState) ReportsDTO {
	state := page.State
	language := state.Language
	taxReport := state.TaxReport
	bookings := state.TaxBookings

	dto := ReportsDTO{
		Tab:        page.Tab,
		Year:       state.SelectedYear,
		YearLabel:  strconv.Itoa(state.SelectedYear),
		Loading:    state.IsLoading,
		Exporting:  state.IsExporting,
		RangeFrom:  page.RangeFrom,
		RangeTo:    page.RangeTo,
		RangeError: page.RangeError,
		Custom:     state.CustomRange != nil,
		Linkable:   page.Linkable,
	}
	if state.Error != nil {
		dto.ErrorText = *state.Error
	}
	if state.Message != nil {
		dto.MessageText = *state.Message
	}

	dto.HasReport = taxReport != nil
	dto.Empty = taxReport != nil && taxReport.TransactionCount == 0
	dto.CanExportReport = taxReport != nil && taxReport.TransactionCount > 0
	if taxReport != nil && taxReport.TransactionCount == 0 {
		dto.ExportDisabledReason = i18n.Format(
			language,
			i18n.NothingToExport,
			i18n.Str(taxReport.Title(language)),
		)
	}
	dto.CanExportTax = bookings != nil && bookings.BookingCount > 0

	dto.EmptyTitle, dto.EmptyHint = reportsEmptyTexts(taxReport, language)
	if taxReport != nil {
		dto.OtherCurrencyHint = reportsOtherCurrencyHint(taxReport, language)
		dto.IncomeValue = format.Money(taxReport.TotalIncome, language, false)
		dto.ExpensesValue = format.Money(taxReport.TotalExpenses, language, false)
		dto.NetValue = format.Money(taxReport.NetIncome, language, false)
		dto.TaxValue = format.Money(taxReport.TaxRelevantTotal(), language, false)
		dto.NetTone = reportsTone(taxReport.NetIncome)
		dto.TaxRelevantRows = reportsAmountRows(taxReport.TaxRelevantExpenses, language)
		if len(taxReport.TaxRelevantExpenses) == 0 {
			dto.TaxRelevantEmpty = i18n.Text(language, i18n.NoTaxRelevantExpenses)
		}
		dto.StatTransactions = strconv.Itoa(taxReport.TransactionCount)
		dto.StatCategories = strconv.Itoa(taxReport.CategoriesUsed)
		dto.StatAverage = format.Money(reportsAverageMonthly(taxReport), language, false)
		dto.StatHighest = reportsHighestMonth(taxReport, language)
		dto.MonthlyTitle = i18n.Format(
			language,
			i18n.MonthlyIncomeExpenses,
			i18n.Str(taxReport.Title(language)),
		)
		dto.MonthlyRows = reportsMonthlyRows(taxReport, language)
		dto.MonthlyTotalIncome = format.Money(taxReport.TotalIncome, language, false)
		dto.MonthlyTotalExpenses = format.Money(taxReport.TotalExpenses, language, false)
		dto.MonthlyTotalNet = format.Money(taxReport.NetIncome, language, true)
		dto.MonthlyTotalTone = reportsTone(taxReport.NetIncome)
		dto.MonthlyTotalIncomeZero = taxReport.TotalIncome.Minor == 0
		dto.MonthlyTotalExpensesZero = taxReport.TotalExpenses.Minor == 0
		dto.MonthlyTotalNetZero = taxReport.NetIncome.Minor == 0
		dto.CategoryRows, dto.TransfersNote = reportsCategoryRows(taxReport, language)
		dto.VendorRows = reportsVendorRows(taxReport, language)
	}

	reportsApplyRecurring(&dto, state, language)
	reportsApplyTaxBookings(&dto, state, language)
	reportsNonNilLists(&dto)
	return dto
}

// reportsNonNilLists turns absent lists into empty ones, so the page always
// receives JSON arrays (a nil slice would marshal as null), as the other
// page DTOs do.
func reportsNonNilLists(dto *ReportsDTO) {
	if dto.TaxRelevantRows == nil {
		dto.TaxRelevantRows = []ReportAmountRowDTO{}
	}
	if dto.MonthlyRows == nil {
		dto.MonthlyRows = []ReportMonthlyRowDTO{}
	}
	if dto.CategoryRows == nil {
		dto.CategoryRows = []ReportCategoryRowDTO{}
	}
	if dto.VendorRows == nil {
		dto.VendorRows = []ReportVendorRowDTO{}
	}
	if dto.RecurringRows == nil {
		dto.RecurringRows = []ReportRecurringRowDTO{}
	}
	if dto.TaxSuggestions == nil {
		dto.TaxSuggestions = []ReportSuggestionRowDTO{}
	}
	if dto.TaxRows == nil {
		dto.TaxRows = []ReportTaxRowDTO{}
	}
}

func reportsApplyRecurring(dto *ReportsDTO, state reportsview.ReportsState, language i18n.Language) {
	payments := state.Recurring
	count := 0
	for _, payment := range payments {
		if !payment.IsIncome() {
			count++
		}
	}
	totals := reportsRecurringCostTotals(payments, state.PrimaryCurrency)

	dto.RecurringEmpty = len(payments) == 0
	dto.RecurringCount = strconv.Itoa(count)
	dto.RecurringYearly = reportsYearlyCostText(totals, language)
	dto.RecurringMonthly = reportsMonthlyCostText(totals, language)
	dto.RecurringRows = reportsRecurringRows(payments, language)
}

func reportsApplyTaxBookings(dto *ReportsDTO, state reportsview.ReportsState, language i18n.Language) {
	bookings := state.TaxBookings
	dto.HasTax = bookings != nil
	dto.MissingReceiptsOnly = state.MissingReceiptsOnly
	if bookings == nil {
		dto.TaxEmpty = false
		dto.TaxRows = nil
		dto.TaxSuggestions = nil
		return
	}

	missing := bookings.BookingCount - bookings.WithReceipt()
	if missing < 0 {
		missing = 0
	}
	receiving := missing > 0
	dto.TaxEmpty = bookings.BookingCount == 0
	dto.TaxEmptyTitle = i18n.Format(
		language,
		i18n.NoTaxBookingsIn,
		i18n.Str(bookings.Title(language)),
	)
	dto.TaxEmptyHint = i18n.Text(language, i18n.TaxBookingsHowTo)
	dto.TaxIncomeValue = format.Money(bookings.IncomeTotal(), language, false)
	dto.TaxDeductionValue = format.Money(bookings.DeductionTotal(), language, false)
	dto.TaxNetValue = format.Money(bookings.NetTotal(), language, false)
	dto.TaxNetTone = reportsTone(bookings.NetTotal())
	dto.TaxReceiptsMissing = receiving
	if receiving {
		dto.TaxReceiptsValue = i18n.Format(language, i18n.ReceiptsMissing, i18n.Int(int64(missing)))
	} else {
		dto.TaxReceiptsValue = i18n.Text(language, i18n.ReceiptsComplete)
	}
	dto.TaxReceiptsSubtitle = i18n.Format(
		language,
		i18n.TaxReceiptsOf,
		i18n.Int(int64(bookings.WithReceipt())),
		i18n.Int(int64(bookings.BookingCount)),
	)
	dto.TaxOtherHint = reportsOtherCurrencyHintBookings(bookings, language)
	dto.TaxSuggestionsTitle = i18n.Format(
		language,
		i18n.TaxSuggestionsTitle,
		i18n.Int(int64(len(state.Suggestions))),
	)
	dto.TaxSuggestionsHint = i18n.Text(language, i18n.TaxSuggestionsHint)
	dto.TaxSuggestions = reportsSuggestionRows(state.Suggestions, language)
	dto.TaxRows = reportsTaxRows(bookings, state.MissingReceiptsOnly, language)
}

func reportsEmptyTexts(taxReport *report.TaxReport, language i18n.Language) (string, string) {
	if taxReport == nil {
		return "", ""
	}
	title := i18n.Format(language, i18n.NoTransactionsIn, i18n.Str(taxReport.Title(language)))
	if taxReport.OtherCurrencyBookings > 0 {
		return title, i18n.Format(
			language,
			i18n.OtherCurrenciesExcluded,
			i18n.Str(taxReport.Currency.String()),
			i18n.Int(int64(taxReport.OtherCurrencyBookings)),
		)
	}
	return title, i18n.Text(language, i18n.ReportsBuiltFrom)
}

func reportsOtherCurrencyHint(taxReport *report.TaxReport, language i18n.Language) string {
	if taxReport.OtherCurrencyBookings == 0 {
		return ""
	}
	return i18n.Format(
		language,
		i18n.OtherCurrenciesExcluded,
		i18n.Str(taxReport.Currency.String()),
		i18n.Int(int64(taxReport.OtherCurrencyBookings)),
	)
}

func reportsOtherCurrencyHintBookings(bookings *report.TaxBookings, language i18n.Language) string {
	if bookings.OtherCurrencyBookings == 0 {
		return ""
	}
	return i18n.Format(
		language,
		i18n.OtherCurrenciesExcluded,
		i18n.Str(bookings.Currency.String()),
		i18n.Int(int64(bookings.OtherCurrencyBookings)),
	)
}

func reportsAmountRows(rows []report.NamedAmount, language i18n.Language) []ReportAmountRowDTO {
	largest := uint64(0)
	for _, entry := range rows {
		if absolute := reportsUnsignedAbs(entry.Amount.Minor); absolute > largest {
			largest = absolute
		}
	}
	result := make([]ReportAmountRowDTO, 0, len(rows))
	for _, entry := range rows {
		fraction := 0.0
		if largest != 0 {
			fraction = float64(reportsUnsignedAbs(entry.Amount.Minor)) / float64(largest)
		}
		result = append(result, ReportAmountRowDTO{
			Label:      entry.Name,
			AmountText: format.Money(entry.Amount, language, false),
			Fraction:   fraction,
		})
	}
	return result
}

func reportsVendorRows(taxReport *report.TaxReport, language i18n.Language) []ReportVendorRowDTO {
	result := make([]ReportVendorRowDTO, 0, len(taxReport.TopVendors))
	for index, entry := range taxReport.TopVendors {
		result = append(result, ReportVendorRowDTO{
			Rank:       fmt.Sprintf("%d.", index+1),
			Name:       entry.Name,
			AmountText: format.Money(entry.Amount, language, false),
		})
	}
	return result
}

func reportsMonthlyRows(taxReport *report.TaxReport, language i18n.Language) []ReportMonthlyRowDTO {
	result := make([]ReportMonthlyRowDTO, 0, len(taxReport.MonthlyBreakdown))
	for _, month := range taxReport.MonthlyBreakdown {
		label := report.MonthName(language, month.Month)
		if !taxReport.IsCalendarYear() {
			label = fmt.Sprintf("%s %d", label, month.Year)
		}
		net := month.Net()
		result = append(result, ReportMonthlyRowDTO{
			Month:        label,
			IncomeText:   format.Money(month.Income, language, false),
			ExpensesText: format.Money(month.Expenses, language, false),
			NetText:      format.Money(net, language, net.Minor != 0),
			IncomeZero:   month.Income.Minor == 0,
			ExpensesZero: month.Expenses.Minor == 0,
			NetZero:      net.Minor == 0,
			NetTone:      reportsTone(net),
		})
	}
	return result
}

func reportsAverageMonthly(taxReport *report.TaxReport) money.Money {
	var total int64
	count := int64(0)
	for _, month := range taxReport.MonthlyBreakdown {
		if month.Expenses.Minor > 0 {
			total += month.Expenses.Minor
			count++
		}
	}
	if count == 0 {
		return money.Money{Minor: 0, Currency: taxReport.Currency}
	}
	return money.Money{Minor: (total + count/2) / count, Currency: taxReport.Currency}
}

func reportsHighestMonth(taxReport *report.TaxReport, language i18n.Language) string {
	var highest *report.MonthlyData
	for index := range taxReport.MonthlyBreakdown {
		month := &taxReport.MonthlyBreakdown[index]
		if month.Expenses.Minor <= 0 {
			continue
		}
		if highest == nil || month.Expenses.Minor >= highest.Expenses.Minor {
			highest = month
		}
	}
	if highest == nil {
		return "—"
	}
	return fmt.Sprintf(
		"%s %s",
		report.MonthName(language, highest.Month),
		format.Money(highest.Expenses, language, false),
	)
}

func reportsCategoryRows(taxReport *report.TaxReport, language i18n.Language) ([]ReportCategoryRowDTO, string) {
	shares := taxReport.CategoryShares
	if len(shares) == 0 {
		return nil, ""
	}

	var total int64
	amounts := make([]money.Money, 0, len(shares))
	for _, share := range shares {
		total += share.Amount.Minor
		amounts = append(amounts, share.Amount)
	}
	percentages := format.Shares(amounts)
	visible := len(shares)
	if visible > 12 {
		visible = 12
	}

	rows := make([]ReportCategoryRowDTO, 0, visible+1)
	for index := 0; index < visible; index++ {
		share := shares[index]
		rows = append(rows, ReportCategoryRowDTO{
			Label:       share.Name,
			PercentText: format.Percent(float64(reportsClampPercent(percentages[index]))/100.0, language),
			AmountText:  format.Money(share.Amount, language, false),
			Colour:      reportsColour(share.Color),
			CategoryID:  reportsCategoryID(share.CategoryID),
			IsMore:      false,
			Fraction:    reportsFraction(share.Amount.Minor, total),
		})
	}

	rest := shares[visible:]
	if len(rest) > 0 {
		var restTotal int64
		for _, share := range rest {
			restTotal += share.Amount.Minor
		}
		var restPercent int64
		for _, percent := range percentages[visible:] {
			restPercent += percent
		}
		rows = append(rows, ReportCategoryRowDTO{
			Label: i18n.Format(
				language,
				i18n.CategorySplitMore,
				i18n.Int(int64(len(rest))),
			),
			PercentText: format.Percent(float64(reportsClampPercent(restPercent))/100.0, language),
			AmountText:  format.Money(money.Money{Minor: restTotal, Currency: taxReport.Currency}, language, false),
			Colour:      reportsColour("#9E9E9E"),
			CategoryID:  -1,
			IsMore:      true,
			Fraction:    reportsFraction(restTotal, total),
		})
	}

	transfersNote := ""
	if taxReport.TransferTotal.Minor > 0 {
		transfersNote = i18n.Format(
			language,
			i18n.CategorySplitTransfers,
			i18n.Str(format.Money(taxReport.TransferTotal, language, false)),
		)
	}
	return rows, transfersNote
}

func reportsRecurringRows(payments []recurring.RecurringPayment, language i18n.Language) []ReportRecurringRowDTO {
	sorted := make([]recurring.RecurringPayment, len(payments))
	copy(sorted, payments)
	sort.SliceStable(sorted, func(left, right int) bool {
		return reportsUnsignedAbs(sorted[left].YearlyAmount().Minor) >
			reportsUnsignedAbs(sorted[right].YearlyAmount().Minor)
	})

	result := make([]ReportRecurringRowDTO, 0, len(sorted))
	for _, payment := range sorted {
		amount := format.Money(payment.TypicalAmount, language, true)
		if !payment.FixedAmount {
			amount = "≈ " + amount
		}
		result = append(result, ReportRecurringRowDTO{
			Name:       payment.Name,
			Meta:       reportsRecurringMeta(payment, language),
			Cadence:    reportsCadenceText(payment.Cadence, language),
			NextText:   format.Date(payment.NextDate),
			AmountText: amount,
			YearlyText: format.Money(
				money.Money{
					Minor:    reportsAbsClamped(payment.YearlyAmount().Minor),
					Currency: payment.Currency,
				},
				language,
				false,
			),
			Income: payment.IsIncome(),
		})
	}
	return result
}

func reportsRecurringMeta(payment recurring.RecurringPayment, language i18n.Language) string {
	meta := i18n.Format(language, i18n.TimesSeen, i18n.Int(int64(payment.Occurrences)))
	if payment.CategoryName != nil {
		meta += " · " + *payment.CategoryName
	}
	if !payment.FixedAmount {
		meta += " · " + i18n.Text(language, i18n.AmountVaries)
	}
	return meta
}

func reportsCadenceText(cadence recurring.Cadence, language i18n.Language) string {
	switch cadence {
	case recurring.Weekly:
		return i18n.Text(language, i18n.CadenceWeekly)
	case recurring.Quarterly:
		return i18n.Text(language, i18n.CadenceQuarterly)
	case recurring.Yearly:
		return i18n.Text(language, i18n.CadenceYearly)
	default:
		return i18n.Text(language, i18n.CadenceMonthly)
	}
}

// reportsRecurringCostTotals is the yearly cost per currency, primary currency
// first and then by code. Income and transfers never count, and currencies are
// never mixed.
func reportsRecurringCostTotals(
	payments []recurring.RecurringPayment,
	primary money.Currency,
) []money.CurrencyTotal {
	totals := make(map[money.Currency]int64)
	for _, payment := range payments {
		if payment.IsIncome() || payment.IsTransfer {
			continue
		}
		amount := reportsAbsClamped(payment.YearlyAmount().Minor)
		totals[payment.Currency] = reportsSaturatingAdd(totals[payment.Currency], amount)
	}

	rows := make([]money.CurrencyTotal, 0, len(totals))
	for currency, total := range totals {
		rows = append(rows, money.CurrencyTotal{Currency: currency, Total: total})
	}
	sort.Slice(rows, func(left, right int) bool {
		leftPrimary := rows[left].Currency != primary
		rightPrimary := rows[right].Currency != primary
		if leftPrimary != rightPrimary {
			return !leftPrimary
		}
		return rows[left].Currency.String() < rows[right].Currency.String()
	})
	return rows
}

func reportsYearlyCostText(totals []money.CurrencyTotal, language i18n.Language) string {
	lines := make([]string, 0, len(totals))
	for _, total := range totals {
		lines = append(lines, format.Money(
			money.Money{Minor: total.Total, Currency: total.Currency},
			language,
			false,
		))
	}
	return strings.Join(lines, "\n")
}

func reportsMonthlyCostText(totals []money.CurrencyTotal, language i18n.Language) string {
	lines := make([]string, 0, len(totals))
	for _, total := range totals {
		lines = append(lines, format.Money(
			money.Money{Minor: reportsDivideRound(total.Total, 12), Currency: total.Currency},
			language,
			false,
		))
	}
	return strings.Join(lines, "\n")
}

func reportsDivideRound(minor int64, divisor int64) int64 {
	if divisor <= 0 {
		return 0
	}
	half := divisor / 2
	if minor >= 0 {
		quotient := minor / divisor
		if minor%divisor >= half {
			quotient++
		}
		return quotient
	}
	quotient := minor / divisor
	if minor%divisor <= -half {
		quotient--
	}
	return quotient
}

// reportsTaxRows is the flattened Tax bookings list: section labels, group
// headers and the visible bookings.
func reportsTaxRows(
	bookings *report.TaxBookings,
	missingOnly bool,
	language i18n.Language,
) []ReportTaxRowDTO {
	rows := make([]ReportTaxRowDTO, 0)
	visible := func(booking model.TransactionItem) bool {
		return !missingOnly || booking.DocumentCount == 0
	}

	if reportsGroupsHaveVisible(bookings.IncomeGroups, visible) {
		rows = append(rows, reportsSectionRow(i18n.TaxSectionIncome, language))
		for index := range bookings.IncomeGroups {
			rows = reportsPushGroup(rows, bookings.IncomeGroups[index], bookings.Currency, visible, language)
		}
	}
	if reportsGroupsHaveVisible(bookings.DeductionGroups, visible) {
		rows = append(rows, reportsSectionRow(i18n.TaxSectionDeductions, language))
		for index := range bookings.DeductionGroups {
			rows = reportsPushGroup(rows, bookings.DeductionGroups[index], bookings.Currency, visible, language)
		}
	}
	return rows
}

func reportsGroupsHaveVisible(
	groups []report.TaxGroup,
	visible func(model.TransactionItem) bool,
) bool {
	for _, group := range groups {
		for _, booking := range group.Bookings {
			if visible(booking) {
				return true
			}
		}
	}
	return false
}

func reportsSectionRow(key i18n.Key, language i18n.Language) ReportTaxRowDTO {
	return ReportTaxRowDTO{
		Kind:   "section",
		Title:  i18n.Text(language, key),
		Colour: reportsColour("#000000"),
	}
}

func reportsPushGroup(
	rows []ReportTaxRowDTO,
	group report.TaxGroup,
	currency money.Currency,
	visible func(model.TransactionItem) bool,
	language i18n.Language,
) []ReportTaxRowDTO {
	if !reportsGroupHasVisible(group, visible) {
		return rows
	}

	net := group.Net()
	rows = append(rows, ReportTaxRowDTO{
		Kind:   "group",
		Title:  group.Name,
		Colour: reportsColour(group.Color),
		Summary: i18n.Format(
			language,
			i18n.BookingsCountTotal,
			i18n.Int(int64(len(group.Bookings))),
			i18n.Str(format.Money(net, language, true)),
		),
		AmountText: format.Money(net, language, true),
		Expense:    net.Minor < 0,
	})
	for _, booking := range group.Bookings {
		if visible(booking) {
			rows = append(rows, reportsBookingRow(booking, currency, language))
		}
	}
	return rows
}

func reportsGroupHasVisible(
	group report.TaxGroup,
	visible func(model.TransactionItem) bool,
) bool {
	for _, booking := range group.Bookings {
		if visible(booking) {
			return true
		}
	}
	return false
}

func reportsBookingRow(
	booking model.TransactionItem,
	currency money.Currency,
	language i18n.Language,
) ReportTaxRowDTO {
	account := ""
	if booking.AccountIBAN != nil && *booking.AccountIBAN != "" {
		account = "…" + reportsLastRunes(*booking.AccountIBAN, 4)
	}
	amount := money.Money{Minor: booking.Amount.Minor, Currency: currency}
	return ReportTaxRowDTO{
		Kind:           "booking",
		Title:          report.DisplayDescription(&booking),
		Subtitle:       reportsTaxSourceText(booking, language),
		Colour:         reportsColour("#000000"),
		ID:             reportsID(int64(booking.ID)),
		DateText:       format.Date(booking.Date),
		AccountText:    account,
		AmountText:     format.Money(amount, language, true),
		Expense:        booking.Amount.Minor < 0,
		HasReceipt:     booking.DocumentCount > 0,
		MissingReceipt: booking.DocumentCount == 0,
		CanReset:       booking.TaxOverride != nil,
	}
}

func reportsTaxSourceText(booking model.TransactionItem, language i18n.Language) string {
	if booking.TaxSource == model.TaxSourceCategory {
		return ""
	}
	base := i18n.Text(language, i18n.TaxNotRelevantChoice)
	if booking.IsTaxRelevant {
		base = i18n.Text(language, i18n.TaxRelevantChoice)
	}
	switch booking.TaxSource {
	case model.TaxSourceManual:
		return i18n.Format(language, i18n.TaxSetByYou, i18n.Str(base))
	case model.TaxSourceRule:
		pattern := ""
		if booking.TaxRulePattern != nil {
			pattern = *booking.TaxRulePattern
		}
		return i18n.Format(language, i18n.TaxFromRule, i18n.Str(base), i18n.Str(pattern))
	case model.TaxSourceNone:
		return base
	default:
		return ""
	}
}

func reportsSuggestionRows(
	suggestions []report.TaxSuggestion,
	language i18n.Language,
) []ReportSuggestionRowDTO {
	result := make([]ReportSuggestionRowDTO, 0, len(suggestions))
	for _, suggestion := range suggestions {
		booking := suggestion.Booking
		result = append(result, ReportSuggestionRowDTO{
			ID:          reportsID(int64(booking.ID)),
			DateText:    format.Date(booking.Date),
			Title:       report.DisplayDescription(&booking),
			KeywordText: i18n.Format(language, i18n.MatchesKeyword, i18n.Str(suggestion.Keyword)),
			AmountText:  format.Money(booking.Amount, language, true),
			Expense:     booking.Amount.Minor < 0,
		})
	}
	return result
}

func reportsTone(amount money.Money) string {
	switch {
	case amount.Minor < 0:
		return "negative"
	case amount.Minor > 0:
		return "positive"
	default:
		return "zero"
	}
}

func reportsFraction(amount int64, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(amount) / float64(total)
}

func reportsClampPercent(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func reportsCategoryID(value *model.CategoryID) int {
	if value == nil {
		return -1
	}
	id := int64(*value)
	if id < math.MinInt32 || id > math.MaxInt32 {
		return -1
	}
	return int(id)
}

func reportsID(value int64) int {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(value)
}

// reportsColour normalizes a category colour to "#RRGGBB"; an unknown value is
// Rust's fallback grey.
func reportsColour(hex string) string {
	text := strings.TrimLeft(strings.TrimSpace(hex), "#")
	if len(text) != 6 {
		return "#757575"
	}
	for index := 0; index < len(text); index++ {
		character := text[index]
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F') {
			return "#757575"
		}
	}
	return "#" + strings.ToUpper(text)
}

func reportsLastRunes(text string, count int) string {
	runes := []rune(text)
	if len(runes) <= count {
		return text
	}
	return string(runes[len(runes)-count:])
}

func reportsUnsignedAbs(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	// big.Int avoids the uint64 conversion gosec cannot prove is in range;
	// -(MinInt64) does not fit in an int64.
	return new(big.Int).Abs(big.NewInt(value)).Uint64()
}

func reportsAbsClamped(value int64) int64 {
	if value >= 0 {
		return value
	}
	if value == math.MinInt64 {
		return math.MaxInt64
	}
	return -value
}

func reportsSaturatingAdd(left, right int64) int64 {
	sum := left + right
	if right > 0 && sum < left {
		return math.MaxInt64
	}
	if right < 0 && sum > left {
		return math.MinInt64
	}
	return sum
}
