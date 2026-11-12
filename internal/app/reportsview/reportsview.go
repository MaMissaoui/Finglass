// Package reportsview ports crates/finglass-core/src/app/reports.rs: the
// Reports page state without any UI or coroutine machinery.
//
// It keeps FinDesk's ReportsViewModel state: the selected year or custom range,
// the tax report and tax bookings, keyword suggestions, recurring payments and
// the PDF, CSV, XLSX and print actions. Every write goes through the existing
// repositories, which write the audit log exactly once.
//
// The state types are single-caller, as Rust's `&mut self` implies. The Wails
// layer serializes calls with one sync.Mutex per profile.
package reportsview

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/recurring"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// DateRange is an inclusive date range, Rust's `(Date, Date)` pair.
type DateRange struct {
	// From is the first day, inclusive.
	From civil.Date
	// To is the last day, inclusive.
	To civil.Date
}

// ReportsState is the Reports page state.
//
// The state is single-caller, as Rust's `&mut self` implies. The Wails layer
// serializes calls with one sync.Mutex per profile.
type ReportsState struct {
	// SelectedYear is the selected calendar year.
	SelectedYear int
	// CustomRange, when non-nil, is reported over instead of SelectedYear.
	CustomRange *DateRange
	// TaxReport is the tax summary over the selected period.
	TaxReport *report.TaxReport
	// TaxBookings holds the Tax bookings tab's groups for the selected period.
	TaxBookings *report.TaxBookings
	// LoadedRange is the period TaxReport and TaxBookings were generated for;
	// nil until a load publishes them.
	LoadedRange *DateRange
	// Suggestions holds keyword suggestions for the selected period; accepting
	// or dismissing one stores a manual override.
	Suggestions []report.TaxSuggestion
	// MissingReceiptsOnly filters the tab's rows to bookings without a receipt;
	// totals stay whole-period.
	MissingReceiptsOnly bool
	// Recurring holds payments detected over the last 13 months, independent of
	// the selected period.
	Recurring []recurring.RecurringPayment
	// PrimaryCurrency is the profile's primary currency, shown when a total map
	// is empty.
	PrimaryCurrency money.Currency
	// IsLoading says whether a load is running.
	IsLoading bool
	// IsExporting says whether an export or print is running.
	IsExporting bool
	// Error is the last error, already localized.
	Error *string
	// Message is the last success message, already localized.
	Message *string
	// Language is the language for user-visible texts.
	Language i18n.Language
}

// New builds an empty state for language, with the year of today selected.
func New(language i18n.Language, today civil.Date) *ReportsState {
	return &ReportsState{
		SelectedYear:        today.Year(),
		CustomRange:         nil,
		TaxReport:           nil,
		TaxBookings:         nil,
		LoadedRange:         nil,
		Suggestions:         make([]report.TaxSuggestion, 0),
		PrimaryCurrency:     money.EUR,
		Recurring:           make([]recurring.RecurringPayment, 0),
		MissingReceiptsOnly: false,
		IsLoading:           false,
		IsExporting:         false,
		Error:               nil,
		Message:             nil,
		Language:            language,
	}
}

// LoadTaxReport loads the tax report, tax bookings and suggestions for a
// calendar year.
func (s *ReportsState) LoadTaxReport(ctx context.Context, d *db.Database, year int) {
	s.IsLoading = true
	s.Error = nil
	s.SelectedYear = year
	from, to := yearRange(year)

	primary, taxReport, taxBookings, suggestions, err := s.generateYear(ctx, d, year)
	if err != nil {
		s.fail(err)
		return
	}

	// Ignore stale results if the year or range changed meanwhile; synchronous
	// callers cannot race, but the guard keeps the published state exactly
	// FinDesk's.
	if s.SelectedYear == year && s.CustomRange == nil {
		s.TaxReport = &taxReport
		s.TaxBookings = &taxBookings
		s.LoadedRange = &DateRange{From: from, To: to}
		s.Suggestions = suggestions
		s.PrimaryCurrency = primary
		s.IsLoading = false
	}
}

// LoadRecurring loads the recurring payments detected over the last 13 months.
func (s *ReportsState) LoadRecurring(ctx context.Context, d *db.Database, today civil.Date) {
	start := today.AddDays(-400)
	filter := model.TransactionFilter{
		StartDate: &start,
		EndDate:   &today,
	}

	loaded, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		s.fail(err)
		return
	}
	primary, err := money.Primary(ctx, d)
	if err != nil {
		s.fail(err)
		return
	}

	s.Recurring = (recurring.RecurringDetector{}).Detect(loaded, today)
	s.PrimaryCurrency = primary
}

// UpdateYear switches to a calendar year and loads it.
func (s *ReportsState) UpdateYear(ctx context.Context, d *db.Database, year int) {
	s.CustomRange = nil
	s.LoadTaxReport(ctx, d, year)
}

// SetCustomRange switches to a custom range and loads it.
func (s *ReportsState) SetCustomRange(ctx context.Context, d *db.Database, from, to civil.Date) {
	if from.After(to) {
		message := i18n.Text(s.Language, i18n.InvalidDateRange)
		s.Error = &message
		return
	}

	s.CustomRange = &DateRange{From: from, To: to}
	s.IsLoading = true
	s.Error = nil

	primary, taxReport, taxBookings, suggestions, err := s.generateRange(ctx, d, from, to)
	if err != nil {
		s.fail(err)
		return
	}
	if equalRange(s.CustomRange, &DateRange{From: from, To: to}) {
		s.TaxReport = &taxReport
		s.TaxBookings = &taxBookings
		s.LoadedRange = &DateRange{From: from, To: to}
		s.Suggestions = suggestions
		s.PrimaryCurrency = primary
		s.IsLoading = false
	}
}

// AcceptSuggestion marks the booking tax relevant (the suggestion's accept).
func (s *ReportsState) AcceptSuggestion(ctx context.Context, d *db.Database, id model.TransactionID) {
	s.setTaxOverride(ctx, d, id, boolPointer(true))
}

// DismissSuggestion hides the suggestion for good by storing a "not relevant"
// override.
func (s *ReportsState) DismissSuggestion(ctx context.Context, d *db.Database, id model.TransactionID) {
	s.setTaxOverride(ctx, d, id, boolPointer(false))
}

// MarkNotTaxRelevant is the row action that marks a booking not tax relevant.
func (s *ReportsState) MarkNotTaxRelevant(ctx context.Context, d *db.Database, id model.TransactionID) {
	s.setTaxOverride(ctx, d, id, boolPointer(false))
}

// ResetTaxAutomatic is the row action that resets a booking's manual override,
// so rule and category decide again.
func (s *ReportsState) ResetTaxAutomatic(ctx context.Context, d *db.Database, id model.TransactionID) {
	s.setTaxOverride(ctx, d, id, nil)
}

// SetMissingReceiptsOnly filters the tab's rows only; the totals stay those of
// the whole period.
func (s *ReportsState) SetMissingReceiptsOnly(only bool) {
	s.MissingReceiptsOnly = only
}

// ExportPDF exports the tax summary PDF.
func (s *ReportsState) ExportPDF(ctx context.Context, d *db.Database, path string, today civil.Date) {
	s.IsExporting = true
	s.Error = nil
	dateRange := s.currentRange()

	var taxReport report.TaxReport
	var err error
	if s.TaxReport != nil && equalRange(s.LoadedRange, dateRange) {
		taxReport = *s.TaxReport
	} else {
		taxReport, err = s.generateReportForExport(ctx, d, dateRange)
	}
	if err != nil {
		s.fail(err)
		return
	}

	if err := (report.PdfReportExporter{}).Export(&taxReport, path, today, s.Language); err != nil {
		s.IsExporting = false
		s.Message = nil
		message := i18n.Text(s.Language, i18n.ErrorPdfExportFailed)
		s.Error = &message
		return
	}
	s.finishExport(path)
}

// ExportCSV exports a range or year as CSV.
func (s *ReportsState) ExportCSV(ctx context.Context, d *db.Database, path string) {
	s.IsExporting = true
	s.Error = nil

	var err error
	if s.CustomRange != nil {
		err = report.ExportRangeToCSV(
			ctx,
			d,
			s.CustomRange.From,
			s.CustomRange.To,
			path,
			s.Language,
		)
	} else {
		err = report.ExportYearToCSV(ctx, d, s.SelectedYear, path, s.Language)
	}

	if err == nil {
		s.finishExport(path)
		return
	}
	if isIOError(err) {
		s.IsExporting = false
		s.Message = nil
		message := i18n.Text(s.Language, i18n.ErrorCsvExportFailed)
		s.Error = &message
		return
	}
	s.fail(err)
}

// ExportTaxXLSX exports the Tax bookings tab as an Excel workbook.
func (s *ReportsState) ExportTaxXLSX(ctx context.Context, d *db.Database, path string, today civil.Date) {
	s.exportTax(ctx, d, path, today, func(
		bookings *report.TaxBookings,
		path string,
		_ civil.Date,
		language i18n.Language,
	) error {
		return (report.TaxReportExporter{}).ExportXLSX(bookings, path, language)
	})
}

// ExportTaxPDF exports the Tax bookings tab as the printable PDF.
func (s *ReportsState) ExportTaxPDF(ctx context.Context, d *db.Database, path string, today civil.Date) {
	s.exportTax(ctx, d, path, today, func(
		bookings *report.TaxBookings,
		path string,
		today civil.Date,
		language i18n.Language,
	) error {
		return (report.TaxReportExporter{}).ExportPDF(bookings, path, today, language)
	})
}

// PrintTax writes the Tax bookings PDF to a temporary file and returns its
// path.
//
// The Reports page opens the file in the system PDF viewer. This function never
// launches anything itself.
func (s *ReportsState) PrintTax(ctx context.Context, d *db.Database, today civil.Date) *string {
	s.IsExporting = true
	s.Error = nil

	bookings, err := s.currentTaxBookings(ctx, d)
	if err != nil {
		s.fail(err)
		return nil
	}
	bytes := (report.TaxReportExporter{}).BuildPDF(&bookings, today, s.Language)

	path, err := writeTemporaryPDF(bytes)
	if err != nil {
		s.IsExporting = false
		message := i18n.Text(s.Language, i18n.ErrorPrintFailed)
		s.Error = &message
		return nil
	}

	s.IsExporting = false
	message := i18n.Text(s.Language, i18n.MessagePrintSent)
	s.Message = &message
	return &path
}

// ClearMessages clears the error and success message.
func (s *ReportsState) ClearMessages() {
	s.Error = nil
	s.Message = nil
}

// setTaxOverride records one manual override: the repository audits it, then the
// tab's data and the summary reload, whose tax-relevant total may change.
func (s *ReportsState) setTaxOverride(
	ctx context.Context,
	d *db.Database,
	id model.TransactionID,
	value *bool,
) {
	s.IsLoading = true
	s.Error = nil

	changed, err := transactions.SetTaxOverride(ctx, d, []model.TransactionID{id}, value)
	if err != nil {
		s.fail(err)
		return
	}
	if changed > 0 {
		message := i18n.Format(
			s.Language,
			i18n.MessageTaxOverrideSet,
			i18n.Int(int64(changed)),
		)
		s.Message = &message
	}
	s.reloadTab(ctx, d)
}

// reloadTab reloads the tab's data and the summary for the period the screen
// shows; stale periods keep their result.
func (s *ReportsState) reloadTab(ctx context.Context, d *db.Database) {
	dateRange := s.currentRange()
	customBefore := s.CustomRange

	primary, taxReport, taxBookings, suggestions, err := s.generateRange(
		ctx,
		d,
		dateRange.From,
		dateRange.To,
	)
	if err != nil {
		s.fail(err)
		return
	}
	if equalRange(s.CustomRange, customBefore) {
		s.TaxReport = &taxReport
		s.TaxBookings = &taxBookings
		s.LoadedRange = &DateRange{From: dateRange.From, To: dateRange.To}
		s.Suggestions = suggestions
		s.PrimaryCurrency = primary
		s.IsLoading = false
	}
}

// currentRange is the period the tab shows: the custom range, or the selected
// calendar year.
func (s *ReportsState) currentRange() *DateRange {
	if s.CustomRange != nil {
		copied := *s.CustomRange
		return &copied
	}
	from, to := yearRange(s.SelectedYear)
	return &DateRange{From: from, To: to}
}

// currentTaxBookings is the tab's data, loading it first when the screen has
// not finished loading yet or the period has changed.
func (s *ReportsState) currentTaxBookings(
	ctx context.Context,
	d *db.Database,
) (report.TaxBookings, error) {
	dateRange := s.currentRange()
	if s.TaxBookings != nil && equalRange(s.LoadedRange, dateRange) {
		return *s.TaxBookings, nil
	}
	return report.GenerateTaxBookings(
		ctx,
		d,
		dateRange.From,
		dateRange.To,
		s.Language,
	)
}

func (s *ReportsState) generateYear(
	ctx context.Context,
	d *db.Database,
	year int,
) (money.Currency, report.TaxReport, report.TaxBookings, []report.TaxSuggestion, error) {
	from, to := yearRange(year)
	primary, err := money.Primary(ctx, d)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	taxReport, err := report.GenerateTaxReport(ctx, d, year, s.Language)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	taxBookings, err := report.GenerateTaxBookings(ctx, d, from, to, s.Language)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	suggestions, err := s.findSuggestions(ctx, d, from, to, primary)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	return primary, taxReport, taxBookings, suggestions, nil
}

func (s *ReportsState) generateRange(
	ctx context.Context,
	d *db.Database,
	from, to civil.Date,
) (money.Currency, report.TaxReport, report.TaxBookings, []report.TaxSuggestion, error) {
	primary, err := money.Primary(ctx, d)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	taxReport, err := report.GenerateRangeReport(ctx, d, from, to, s.Language)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	taxBookings, err := report.GenerateTaxBookings(ctx, d, from, to, s.Language)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	suggestions, err := s.findSuggestions(ctx, d, from, to, primary)
	if err != nil {
		return money.Currency{}, report.TaxReport{}, report.TaxBookings{}, nil, err
	}
	return primary, taxReport, taxBookings, suggestions, nil
}

// findSuggestions returns keyword suggestions over the period's bookings of the
// primary currency.
func (s *ReportsState) findSuggestions(
	ctx context.Context,
	d *db.Database,
	from, to civil.Date,
	primary money.Currency,
) ([]report.TaxSuggestion, error) {
	filter := model.TransactionFilter{
		StartDate: &from,
		EndDate:   &to,
		Currency:  &primary,
	}
	bookings, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return nil, err
	}
	keywords, err := report.Keywords(ctx, d)
	if err != nil {
		return nil, err
	}
	return report.FindTaxSuggestions(bookings, keywords), nil
}

func (s *ReportsState) generateReportForExport(
	ctx context.Context,
	d *db.Database,
	dateRange *DateRange,
) (report.TaxReport, error) {
	if s.CustomRange != nil {
		return report.GenerateRangeReport(ctx, d, dateRange.From, dateRange.To, s.Language)
	}
	return report.GenerateTaxReport(ctx, d, s.SelectedYear, s.Language)
}

func (s *ReportsState) exportTax(
	ctx context.Context,
	d *db.Database,
	path string,
	today civil.Date,
	export func(*report.TaxBookings, string, civil.Date, i18n.Language) error,
) {
	s.IsExporting = true
	s.Error = nil

	bookings, err := s.currentTaxBookings(ctx, d)
	if err != nil {
		s.fail(err)
		return
	}
	if err := export(&bookings, path, today, s.Language); err != nil {
		s.IsExporting = false
		message := i18n.Text(s.Language, i18n.ErrorTaxExportFailed)
		s.Error = &message
		return
	}
	s.finishExport(path)
}

func (s *ReportsState) finishExport(path string) {
	s.IsExporting = false
	message := i18n.Format(
		s.Language,
		i18n.MessageSavedTo,
		i18n.Str(fileName(path)),
	)
	s.Message = &message
}

func (s *ReportsState) fail(err error) {
	message := s.errorText(err)
	s.IsLoading = false
	s.IsExporting = false
	s.Error = &message
}

func (s *ReportsState) errorText(err error) string {
	var invalid repo.InvalidError
	if errors.As(err, &invalid) {
		return invalid.Msg
	}
	var invalidPointer *repo.InvalidError
	if errors.As(err, &invalidPointer) {
		return invalidPointer.Msg
	}
	return i18n.Text(s.Language, i18n.ErrorUnexpected)
}

func yearRange(year int) (civil.Date, civil.Date) {
	return mustDate(year, time.January, 1), mustDate(year, time.December, 31)
}

func mustDate(year int, month time.Month, day int) civil.Date {
	date, ok := civil.New(year, month, day)
	if !ok {
		panic("a valid year")
	}
	return date
}

func equalRange(left, right *DateRange) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.From == right.From && left.To == right.To
}

func boolPointer(value bool) *bool {
	return &value
}

// fileName is Rust's Path::file_name: the last normal component, or the whole
// path when there is none.
func fileName(path string) string {
	if name, ok := rustFileName(path); ok {
		return name
	}
	return path
}

// rustFileName returns the last normal path component, like Rust's
// Path::file_name.
func rustFileName(path string) (string, bool) {
	components := splitPathComponents(path)
	if len(components) == 0 {
		return "", false
	}
	last := components[len(components)-1]
	if last == "." || last == ".." {
		return "", false
	}
	return last, true
}

// splitPathComponents keeps only the normal components, dropping roots,
// separators and "." just like Rust's Path::components.
func splitPathComponents(path string) []string {
	components := make([]string, 0)
	for _, component := range splitRuns(path, byte(filepath.Separator)) {
		switch component {
		case "", ".":
			continue
		default:
			components = append(components, component)
		}
	}
	return components
}

func splitRuns(path string, separator byte) []string {
	parts := make([]string, 0)
	start := 0
	for index := 0; index <= len(path); index++ {
		if index == len(path) || path[index] == separator {
			parts = append(parts, path[start:index])
			start = index + 1
		}
	}
	return parts
}

// writeTemporaryPDF writes bytes to a fresh temporary file and returns its
// path, like Rust's NamedTempFile::keep.
func writeTemporaryPDF(bytes []byte) (string, error) {
	file, err := os.CreateTemp("", "finglass-tax-*.pdf")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(bytes); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func isIOError(err error) bool {
	var pathError *fs.PathError
	return errors.As(err, &pathError)
}
