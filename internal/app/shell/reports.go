package shell

import (
	"context"
	"math"
	"strconv"

	"github.com/MaMissaoui/Finglass/internal/app/reportsview"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/dashboard"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/format"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

// reportsExportFile is one of the four files the Reports page writes, Rust's
// ExportFile.
type reportsExportFile uint8

const (
	reportsExportReportPDF reportsExportFile = iota
	reportsExportCSV
	reportsExportTaxXLSX
	reportsExportTaxPDF
)

// ReportsPageState is one Reports snapshot: the page state plus the values that
// live on the controller rather than in ReportsState.
type ReportsPageState struct {
	// State is a deep copy of the page's state.
	State reportsview.ReportsState
	// Tab is "summary" or "tax", as open_page sets it.
	Tab string
	// RangeFrom and RangeTo are the custom-range dialog's fields.
	RangeFrom string
	RangeTo   string
	// RangeError is the dialog's localized validation text.
	RangeError string
	// Linkable says whether the category rows may link into Transactions.
	Linkable bool
}

// ReportsState returns a deep copy of the Reports page state.
func (s *Session) ReportsState() ReportsPageState {
	s.mu.Lock()
	defer s.mu.Unlock()

	page := ReportsPageState{
		Tab:        s.reportsTab,
		RangeFrom:  s.reportsRangeFrom,
		RangeTo:    s.reportsRangeTo,
		RangeError: s.reportsRangeError,
	}
	if s.reports == nil {
		page.State = *reportsview.New(s.language, s.today())
		return page
	}
	page.State = *s.reports.Clone()
	if page.State.CustomRange != nil {
		page.RangeFrom = format.Date(page.State.CustomRange.From)
		page.RangeTo = format.Date(page.State.CustomRange.To)
	}
	if page.State.TaxReport != nil {
		page.Linkable = reportsLinkable(*page.State.TaxReport, s.today())
	}
	return page
}

// ReportsOpen opens the Reports page and optionally its Tax bookings tab for
// year, as a dashboard tax link does.
func (s *Session) ReportsOpen(year *int) {
	args := PageArgs{ReportsYear: year}
	s.mu.Lock()
	s.nav.Enter(args)
	job := s.openPageLocked("reports", args)
	s.mu.Unlock()

	s.notify(Change{Page: "shell"})
	s.startPageJob(job)
}

// beginReportsOpenLocked prepares the page's open for year. The caller holds
// the session mutex and starts the returned job after releasing it.
func (s *Session) beginReportsOpenLocked(year *int) *pageJob {
	if s.reports != nil {
		s.reports.Language = s.language
	}
	if s.db == nil {
		return nil
	}

	today := s.today()
	if year != nil || s.reports.CustomRange == nil {
		s.setReportsDefaultRangeLocked(today)
	}

	job, _ := s.beginReportsJobLocked(true, func(state *reportsview.ReportsState) {
		if year != nil {
			state.SelectedYear = *year
			state.CustomRange = nil
		}
		state.IsLoading = true
		state.Error = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		_ func() bool,
	) {
		if year != nil {
			state.SelectedYear = *year
			state.CustomRange = nil
		}
		if state.CustomRange != nil {
			state.SetCustomRange(ctx, database, state.CustomRange.From, state.CustomRange.To)
		} else {
			state.UpdateYear(ctx, database, state.SelectedYear)
		}
		state.LoadRecurring(ctx, database, today)
	})
	return job
}

// ReportsChangeYear moves the selected calendar year by delta and reloads it.
func (s *Session) ReportsChangeYear(delta int) {
	today := s.today()
	s.mu.Lock()
	job, _ := s.beginReportsJobLocked(false, func(state *reportsview.ReportsState) {
		s.setReportsDefaultRangeLocked(today)
		state.CustomRange = nil
		state.SelectedYear = reportsSaturatingAdd(state.SelectedYear, delta)
		state.IsLoading = true
		state.Error = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		_ func() bool,
	) {
		state.UpdateYear(ctx, database, state.SelectedYear)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// ReportsSetCustom is on_reports_set_custom: leave the custom mode, or only
// clear the range error when the dialog is opening.
func (s *Session) ReportsSetCustom(custom bool) {
	s.mu.Lock()
	s.reportsRangeError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "reports"})
	if !custom {
		s.ReportsChangeYear(0)
	}
}

// ReportsApplyRange validates and applies the custom range.
func (s *Session) ReportsApplyRange(fromText string, toText string) {
	s.mu.Lock()
	if s.db == nil || s.reports == nil || s.reportsWork.Active() {
		s.mu.Unlock()
		return
	}
	language := s.reports.Language
	s.reportsRangeFrom = fromText
	s.reportsRangeTo = toText
	s.mu.Unlock()

	from, ok := format.ParseDate(fromText)
	if !ok {
		s.setReportsRangeError(language, i18n.EnterValidDate)
		return
	}
	to, ok := format.ParseDate(toText)
	if !ok {
		s.setReportsRangeError(language, i18n.EnterValidDate)
		return
	}
	if from.After(to) {
		s.setReportsRangeError(language, i18n.InvalidDateRange)
		return
	}

	s.mu.Lock()
	s.reportsRangeError = ""
	job, _ := s.beginReportsJobLocked(false, func(state *reportsview.ReportsState) {
		state.CustomRange = &reportsview.DateRange{From: from, To: to}
		state.IsLoading = true
		state.Error = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		_ func() bool,
	) {
		state.SetCustomRange(ctx, database, from, to)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

func (s *Session) setReportsRangeError(language i18n.Language, key i18n.Key) {
	s.mu.Lock()
	s.reportsRangeError = i18n.Text(language, key)
	s.mu.Unlock()
	s.notify(Change{Page: "reports"})
}

// ReportsSetMissingOnly filters the Tax bookings rows to bookings without a
// receipt; the totals stay whole-period.
func (s *Session) ReportsSetMissingOnly(only bool) {
	s.mu.Lock()
	if s.reports == nil || s.reportsWork.Active() {
		s.mu.Unlock()
		return
	}
	s.reports.SetMissingReceiptsOnly(only)
	s.mu.Unlock()
	s.notify(Change{Page: "reports"})
}

// ReportsDismiss clears the error and success banner.
func (s *Session) ReportsDismiss() {
	s.mu.Lock()
	if s.reports == nil || s.reportsWork.Active() {
		s.mu.Unlock()
		return
	}
	s.reports.ClearMessages()
	s.mu.Unlock()
	s.notify(Change{Page: "reports"})
}

// ReportsAcceptSuggestion marks the suggested booking tax relevant.
func (s *Session) ReportsAcceptSuggestion(id model.TransactionID) {
	s.startReportsAction(func(ctx context.Context, state *reportsview.ReportsState, database *db.Database) {
		state.AcceptSuggestion(ctx, database, id)
	})
}

// ReportsDismissSuggestion stores a "not relevant" override for the suggestion.
func (s *Session) ReportsDismissSuggestion(id model.TransactionID) {
	s.startReportsAction(func(ctx context.Context, state *reportsview.ReportsState, database *db.Database) {
		state.DismissSuggestion(ctx, database, id)
	})
}

// ReportsMarkNotTaxRelevant marks one tax row's booking not tax relevant.
func (s *Session) ReportsMarkNotTaxRelevant(id model.TransactionID) {
	s.startReportsAction(func(ctx context.Context, state *reportsview.ReportsState, database *db.Database) {
		state.MarkNotTaxRelevant(ctx, database, id)
	})
}

// ReportsResetTaxAutomatic clears one booking's manual tax override.
func (s *Session) ReportsResetTaxAutomatic(id model.TransactionID) {
	s.startReportsAction(func(ctx context.Context, state *reportsview.ReportsState, database *db.Database) {
		state.ResetTaxAutomatic(ctx, database, id)
	})
}

// ReportsExportReportPDF asks for a target and writes the tax summary PDF.
func (s *Session) ReportsExportReportPDF() {
	today := s.today()
	s.startReportsExport(i18n.ExportReport, reportsExportReportPDF, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		path string,
	) {
		state.ExportPDF(ctx, database, path, today)
	})
}

// ReportsExportCSV asks for a target and writes the transaction CSV.
func (s *Session) ReportsExportCSV() {
	s.startReportsExport(i18n.ExportTransactions, reportsExportCSV, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		path string,
	) {
		state.ExportCSV(ctx, database, path)
	})
}

// ReportsExportTaxXLSX asks for a target and writes the Tax bookings workbook.
func (s *Session) ReportsExportTaxXLSX() {
	today := s.today()
	s.startReportsExport(i18n.ExportExcel, reportsExportTaxXLSX, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		path string,
	) {
		state.ExportTaxXLSX(ctx, database, path, today)
	})
}

// ReportsExportTaxPDF asks for a target and writes the Tax bookings PDF.
func (s *Session) ReportsExportTaxPDF() {
	today := s.today()
	s.startReportsExport(i18n.ExportPdf, reportsExportTaxPDF, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		path string,
	) {
		state.ExportTaxPDF(ctx, database, path, today)
	})
}

// ReportsPrintTax writes the Tax bookings PDF where reportsview writes it and
// opens it with the system viewer.
func (s *Session) ReportsPrintTax() {
	today := s.today()
	s.mu.Lock()
	opener := s.fileOpener
	job, _ := s.beginReportsJobLocked(false, func(state *reportsview.ReportsState) {
		state.IsExporting = true
		state.Error = nil
		state.Message = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		current func() bool,
	) {
		if !current() {
			return
		}
		path := state.PrintTax(ctx, database, today)
		if path != nil && opener != nil {
			_ = opener.OpenFile(*path)
		}
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// ReportsOpenCategory opens Transactions filtered to one category, or to the
// uncategorized bookings when categoryID is negative.
func (s *Session) ReportsOpenCategory(categoryID int) {
	today := s.today()
	s.mu.Lock()
	if s.reports == nil || s.reportsWork.Active() {
		s.mu.Unlock()
		return
	}
	snapshot := s.reports.Clone()
	if snapshot == nil || snapshot.TaxReport == nil || !reportsLinkable(*snapshot.TaxReport, today) {
		s.mu.Unlock()
		return
	}
	taxReport := snapshot.TaxReport
	s.mu.Unlock()

	period := transactionsview.PeriodLastYear
	if taxReport.From.Year() == today.Year() {
		period = transactionsview.PeriodThisYear
	}
	var args transactionsview.StartArgs
	if categoryID < 0 {
		args = transactionsview.StartArgs{Period: &period, UncategorizedOnly: true}
	} else {
		id := model.CategoryID(categoryID)
		args = transactionsview.StartArgs{Period: &period, CategoryID: &id}
	}
	s.FollowLink("transactions", PageArgs{Transactions: args})
}

// startReportsAction prepares one action on a clone of the page state and
// starts it. It is Rust's spawn_action.
func (s *Session) startReportsAction(
	work func(context.Context, *reportsview.ReportsState, *db.Database),
) {
	s.mu.Lock()
	job, _ := s.beginReportsJobLocked(false, func(state *reportsview.ReportsState) {
		state.IsLoading = true
		state.Error = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		_ func() bool,
	) {
		work(ctx, state, database)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// startReportsExport prepares one export and starts it. It is Rust's
// spawn_export: the dialog runs in the worker, without the session mutex.
func (s *Session) startReportsExport(
	title i18n.Key,
	file reportsExportFile,
	work func(context.Context, *reportsview.ReportsState, *db.Database, string),
) {
	s.mu.Lock()
	dialogs := s.fileDialogs
	var fileName string
	job, _ := s.beginReportsJobLocked(false, func(state *reportsview.ReportsState) {
		fileName = reportsDefaultFileName(state, file)
		state.IsExporting = true
		state.Error = nil
		state.Message = nil
	}, func(
		ctx context.Context,
		state *reportsview.ReportsState,
		database *db.Database,
		current func() bool,
	) {
		if dialogs == nil {
			state.IsExporting = false
			return
		}
		path, ok := dialogs.SaveFile(title, fileName)
		if !ok || path == "" {
			state.IsExporting = false
			return
		}
		if !current() {
			return
		}
		work(ctx, state, database, path)
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// beginReportsJobLocked prepares one Reports job. The caller holds the session
// mutex and starts the returned job after releasing it.
//
// allowBusy lets a page open supersede a running load; every action and export
// refuses while the page is busy, as Rust's try_lock does. The returned work
// receives a current function so a worker can drop its result after a dialog
// or before touching the database, as Rust checks jobs.is_current.
func (s *Session) beginReportsJobLocked(
	allowBusy bool,
	prepare func(*reportsview.ReportsState),
	work func(context.Context, *reportsview.ReportsState, *db.Database, func() bool),
) (*pageJob, bool) {
	if s.db == nil || s.reports == nil {
		return nil, false
	}
	if !allowBusy && s.reportsWork.Active() {
		return nil, false
	}

	busy := s.reportsWork
	busy.Begin()
	generation, ok := s.beginJobLocked()
	if !ok {
		busy.End()
		return nil, false
	}

	request := s.nextPageRequestLocked("reports")
	pending := s.reports.Clone()
	pending.Language = s.language
	prepare(pending)
	published := pending.Clone()
	s.reports = published
	database := s.db
	current := func() bool { return s.jobs.IsCurrent(generation) }

	return &pageJob{
		page:       "reports",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			if !current() {
				return
			}
			work(ctx, pending, open, current)
		},
		publish: func() {
			pending.Language = s.language
			s.reports = pending
		},
		finish: func(bool) {
			busy.End()
		},
	}, true
}

// resetReportsLocked replaces the Reports state and its controller fields.
func (s *Session) resetReportsLocked() {
	s.reports = reportsview.New(s.language, s.today())
	s.reportsTab = "summary"
	s.setReportsDefaultRangeLocked(s.today())
	s.reportsRangeError = ""
}

// setReportsDefaultRangeLocked sets the dialog's default range: the start of
// the month two months back through today.
func (s *Session) setReportsDefaultRangeLocked(today civil.Date) {
	s.reportsRangeFrom, s.reportsRangeTo = reportsDefaultRange(today)
}

// reportsDefaultRange is Rust's set_default_range: the start of the month two
// months back through today, formatted for the dialog.
func reportsDefaultRange(today civil.Date) (string, string) {
	from := dashboard.YearMonthFromDate(today).MinusMonths(2).Start()
	return format.Date(from), format.Date(today)
}

// reportsSaturatingAdd is Rust's i32::saturating_add for the selected year.
func reportsSaturatingAdd(year, delta int) int {
	if delta > 0 && year > math.MaxInt-delta {
		return math.MaxInt
	}
	if delta < 0 && year < math.MinInt-delta {
		return math.MinInt
	}
	return year + delta
}

// reportsLinkable reports whether the report covers exactly this or last
// calendar year, the only periods the category rows link into Transactions
// with.
func reportsLinkable(taxReport report.TaxReport, today civil.Date) bool {
	fromYear, toYear := taxReport.From.Year(), taxReport.To.Year()
	thisYear := fromYear == today.Year() && toYear == today.Year()
	lastYear := fromYear == today.Year()-1 && toYear == today.Year()-1
	return (thisYear || lastYear) && taxReport.IsCalendarYear()
}

// reportsFileSuffix is the suffix the default file names use: the year, or the
// ISO custom range.
func reportsFileSuffix(state *reportsview.ReportsState) string {
	if state.CustomRange != nil {
		return state.CustomRange.From.String() + "_" + state.CustomRange.To.String()
	}
	return strconv.Itoa(state.SelectedYear)
}

// reportsDefaultFileName is the save dialog's suggested name.
func reportsDefaultFileName(state *reportsview.ReportsState, file reportsExportFile) string {
	suffix := reportsFileSuffix(state)
	switch file {
	case reportsExportReportPDF:
		return "report-" + suffix + ".pdf"
	case reportsExportCSV:
		return "finglass-" + suffix + ".csv"
	case reportsExportTaxXLSX:
		return "tax-bookings-" + suffix + ".xlsx"
	case reportsExportTaxPDF:
		return "tax-bookings-" + suffix + ".pdf"
	default:
		return ""
	}
}
