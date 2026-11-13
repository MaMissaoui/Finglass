package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/reportsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/app/transactionsview"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/report"
)

type reportsSeedData struct {
	account        model.AccountID
	health         model.CategoryID
	salary         model.CategoryID
	withReceipt    model.TransactionID
	withoutReceipt model.TransactionID
	suggestion     model.TransactionID
}

func reportsSeedDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

func seedReportsDatabase(t *testing.T, appPaths paths.AppPaths) reportsSeedData {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	checking, err := accounts.Create(ctx, database, model.NewAccount{
		IBAN:        stringPointer("DE89370400440532013000"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 100_000, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("create checking: %v", err)
	}
	health, err := categories.Create(ctx, database, model.NewCategory{
		Name:          "Health",
		Color:         "#123456",
		IsTaxRelevant: true,
	})
	if err != nil {
		t.Fatalf("create health: %v", err)
	}
	salary, err := categories.Create(ctx, database, model.NewCategory{Name: "Salary", Color: "#2196F3"})
	if err != nil {
		t.Fatalf("create salary: %v", err)
	}

	insert := func(day civil.Date, minor int64, partner string, category *model.CategoryID, purpose *string) {
		name := partner
		if _, err := transactions.InsertPlanned(ctx, database, checking, []model.NewTransaction{{
			BookingDate: day,
			Amount:      money.Money{Minor: minor, Currency: money.EUR},
			PartnerName: &name,
			Purpose:     purpose,
			CategoryID:  category,
			ImportHash:  fmt.Sprintf("reports-%s-%d-%s", day.String(), minor, partner),
			CreatedAt:   testNow.UTC(),
		}}); err != nil {
			t.Fatalf("insert %s: %v", partner, err)
		}
	}

	insert(reportsSeedDate(t, 2026, time.January, 10), 300_000, "Employer", &salary, stringPointer("Salary January"))
	insert(reportsSeedDate(t, 2026, time.February, 15), -100_000, "Vermieter", nil, stringPointer("Rent February"))
	insert(reportsSeedDate(t, 2026, time.March, 1), -2_000, "Apotheke", &health, stringPointer("Receipt booking"))
	insert(reportsSeedDate(t, 2026, time.April, 2), -3_000, "Praxis", &health, stringPointer("No receipt booking"))
	insert(reportsSeedDate(t, 2026, time.May, 1), -500, "Apotheke Online", nil, nil)
	insert(reportsSeedDate(t, 2026, time.July, 1), -999, "Netflix", nil, nil)
	insert(reportsSeedDate(t, 2026, time.August, 1), -999, "Netflix", nil, nil)
	insert(reportsSeedDate(t, 2026, time.September, 1), -999, "Netflix", nil, nil)
	insert(reportsSeedDate(t, 2025, time.June, 1), -700, "Arzt", &health, nil)
	insert(reportsSeedDate(t, 2025, time.July, 1), -700, "Old Shop", nil, nil)

	withReceipt := reportsTransactionID(t, database, "Apotheke")
	withoutReceipt := reportsTransactionID(t, database, "Praxis")
	suggestion := reportsTransactionID(t, database, "Apotheke Online")
	documentTransaction := withReceipt
	if _, err := documents.Create(ctx, database, model.NewDocument{
		FileName:      "receipt.pdf",
		StoredPath:    "receipts/receipt.pdf",
		MIMEType:      "application/pdf",
		TransactionID: &documentTransaction,
	}); err != nil {
		t.Fatalf("create receipt document: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	return reportsSeedData{
		account:        checking,
		health:         health,
		salary:         salary,
		withReceipt:    withReceipt,
		withoutReceipt: withoutReceipt,
		suggestion:     suggestion,
	}
}

func reportsTransactionID(t *testing.T, database *db.Database, partner string) model.TransactionID {
	t.Helper()
	items, err := transactions.FindItems(context.Background(), database, &model.TransactionFilter{}, nil)
	if err != nil {
		t.Fatalf("find transactions: %v", err)
	}
	for _, item := range items {
		if item.PartnerName != nil && *item.PartnerName == partner {
			return item.ID
		}
	}
	t.Fatalf("transaction %q is not in the database", partner)
	return 0
}

func listenForReports(session *Session) (<-chan Change, func()) {
	events := make(chan Change, 512)
	remove := session.AddListener(func(change Change) {
		events <- change
	})
	return events, remove
}

func waitForReports(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(ReportsPageState) bool,
) ReportsPageState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "reports" {
				continue
			}
			page := session.ReportsState()
			if predicate(page) {
				return page
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedReportsSession(t *testing.T) (*Session, reportsSeedData, <-chan Change) {
	t.Helper()
	appPaths := testPaths(t)
	seed := seedReportsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)

	events, remove := listenForReports(session)
	t.Cleanup(remove)
	session.Navigate("reports")
	waitForReports(t, session, events, "the loaded reports", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.TaxReport != nil && page.Tab == "summary"
	})
	return session, seed, events
}

func countReportsAudit(t *testing.T, session *Session, action string) int {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Action == action {
			count++
		}
	}
	return count
}

func TestReportsLoadsTheSummaryAndTheTaxTabForALinkedYear(t *testing.T) {
	session, seed, events := newLoadedReportsSession(t)

	page := session.ReportsState()
	if page.Tab != "summary" || page.State.SelectedYear != 2026 {
		t.Fatalf("page = %+v", page)
	}
	if !page.State.TaxReport.IsCalendarYear() {
		t.Fatal("the loaded report is not a calendar year")
	}
	if page.State.IsLoading || page.State.IsExporting || page.State.CustomRange != nil {
		t.Fatalf("loaded flags = %+v", page.State)
	}
	if page.RangeFrom != "01.08.2026" || page.RangeTo != "15.10.2026" {
		t.Fatalf("default range = %q..%q", page.RangeFrom, page.RangeTo)
	}
	if page.State.TaxReport.TransactionCount != 8 ||
		page.State.TaxReport.CategoriesUsed != 2 {
		t.Fatalf("transaction stats = %+v", page.State.TaxReport)
	}
	if page.State.TaxBookings == nil || page.State.TaxBookings.BookingCount != 2 {
		t.Fatalf("tax bookings = %+v", page.State.TaxBookings)
	}
	if len(page.State.Recurring) != 1 || page.State.Recurring[0].Name != "Netflix" {
		t.Fatalf("recurring = %+v", page.State.Recurring)
	}
	if len(page.State.Suggestions) != 1 || page.State.Suggestions[0].Booking.ID != seed.suggestion {
		t.Fatalf("suggestions = %+v", page.State.Suggestions)
	}

	year := 2025
	session.ReportsOpen(&year)
	page = waitForReports(t, session, events, "the tax tab for 2025", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.Tab == "tax" && page.State.SelectedYear == 2025
	})
	if page.State.TaxBookings == nil || page.State.TaxBookings.BookingCount != 1 {
		t.Fatalf("2025 tax bookings = %+v", page.State.TaxBookings)
	}
}

func TestReportsChangeYearAndCustomRangeValidation(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)

	session.ReportsChangeYear(-1)
	page := waitForReports(t, session, events, "the previous year", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.SelectedYear == 2025 && page.State.CustomRange == nil
	})
	if page.RangeFrom != "01.08.2026" || page.RangeTo != "15.10.2026" {
		t.Fatalf("the year change did not reset the range fields: %q..%q", page.RangeFrom, page.RangeTo)
	}

	session.ReportsChangeYear(1)
	waitForReports(t, session, events, "the next year", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.SelectedYear == 2026
	})

	session.ReportsApplyRange("not a date", "31.12.2026")
	page = waitForReports(t, session, events, "the invalid date error", func(page ReportsPageState) bool {
		return page.RangeError == i18n.Text(i18n.En, i18n.EnterValidDate)
	})
	if page.State.IsLoading {
		t.Fatal("an invalid date started a load")
	}

	session.ReportsSetCustom(true)
	waitForReports(t, session, events, "the cleared range error", func(page ReportsPageState) bool {
		return page.RangeError == ""
	})

	session.ReportsApplyRange("31.12.2026", "01.01.2026")
	waitForReports(t, session, events, "the reversed range error", func(page ReportsPageState) bool {
		return page.RangeError == i18n.Text(i18n.En, i18n.InvalidDateRange)
	})

	session.ReportsApplyRange("01.12.2025", "31.01.2026")
	page = waitForReports(t, session, events, "the custom range", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.CustomRange != nil && page.RangeError == ""
	})
	if page.RangeFrom != "01.12.2025" || page.RangeTo != "31.01.2026" {
		t.Fatalf("custom range = %q..%q", page.RangeFrom, page.RangeTo)
	}
	if page.State.TaxReport == nil ||
		page.State.TaxReport.From != reportsSeedDate(t, 2025, time.December, 1) ||
		page.State.TaxReport.To != reportsSeedDate(t, 2026, time.January, 31) {
		t.Fatalf("custom report range = %+v", page.State.TaxReport)
	}
	if page.Linkable {
		t.Fatal("a custom range is linkable")
	}

	session.ReportsSetCustom(false)
	page = waitForReports(t, session, events, "the calendar year again", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.CustomRange == nil && page.State.SelectedYear == 2026
	})
	if page.State.TaxReport == nil || !page.State.TaxReport.IsCalendarYear() {
		t.Fatalf("the calendar year did not return: %+v", page.State.TaxReport)
	}
}

func TestReportsMissingReceiptsFilterKeepsWholePeriodTotals(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)
	before := session.ReportsState()
	beforeRows := len(before.State.TaxBookings.DeductionGroups[0].Bookings)

	session.ReportsSetMissingOnly(true)
	page := waitForReports(t, session, events, "the missing-receipts filter", func(page ReportsPageState) bool {
		return page.State.MissingReceiptsOnly
	})
	if page.State.TaxBookings == nil {
		t.Fatal("the filter dropped the tax bookings")
	}
	if len(page.State.TaxBookings.DeductionGroups[0].Bookings) != beforeRows {
		t.Fatal("the filter changed the whole-period groups")
	}
	if page.State.TaxBookings.BookingCount != 2 {
		t.Fatalf("the whole-period booking count changed: %d", page.State.TaxBookings.BookingCount)
	}

	session.ReportsSetMissingOnly(false)
	waitForReports(t, session, events, "the unfiltered rows", func(page ReportsPageState) bool {
		return !page.State.MissingReceiptsOnly
	})
}

func TestReportsTaxActionsWriteOneAuditEntryEach(t *testing.T) {
	session, seed, events := newLoadedReportsSession(t)
	before := countReportsAudit(t, session, "transaction.set_tax_override")

	session.ReportsAcceptSuggestion(seed.suggestion)
	page := waitForReports(t, session, events, "the accepted suggestion", func(page ReportsPageState) bool {
		return !page.State.IsLoading && len(page.State.Suggestions) == 0 && page.State.Message != nil
	})
	if len(page.State.TaxBookings.DeductionGroups) != 2 {
		t.Fatalf("the accepted booking did not join the deductions: %+v", page.State.TaxBookings.DeductionGroups)
	}
	if got := countReportsAudit(t, session, "transaction.set_tax_override"); got != before+1 {
		t.Fatalf("accept audit entries = %d, want %d", got, before+1)
	}

	session.ReportsResetTaxAutomatic(seed.suggestion)
	page = waitForReports(t, session, events, "the reset override", func(page ReportsPageState) bool {
		return !page.State.IsLoading && len(page.State.Suggestions) == 1
	})
	if page.State.TaxReport == nil {
		t.Fatal("the reset dropped the report")
	}
	if got := countReportsAudit(t, session, "transaction.set_tax_override"); got != before+2 {
		t.Fatalf("reset audit entries = %d, want %d", got, before+2)
	}

	session.ReportsDismissSuggestion(seed.suggestion)
	page = waitForReports(t, session, events, "the dismissed suggestion", func(page ReportsPageState) bool {
		return !page.State.IsLoading && len(page.State.Suggestions) == 0
	})
	if got := countReportsAudit(t, session, "transaction.set_tax_override"); got != before+3 {
		t.Fatalf("dismiss audit entries = %d, want %d", got, before+3)
	}

	session.ReportsMarkNotTaxRelevant(seed.withoutReceipt)
	page = waitForReports(t, session, events, "the not-relevant booking", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.TaxBookings != nil &&
			page.State.TaxBookings.BookingCount == 1
	})
	if got := countReportsAudit(t, session, "transaction.set_tax_override"); got != before+4 {
		t.Fatalf("mark audit entries = %d, want %d", got, before+4)
	}

	session.ReportsResetTaxAutomatic(seed.withoutReceipt)
	waitForReports(t, session, events, "the category-relevant booking", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.TaxBookings != nil &&
			page.State.TaxBookings.BookingCount == 2
	})
	if got := countReportsAudit(t, session, "transaction.set_tax_override"); got != before+5 {
		t.Fatalf("final audit entries = %d, want %d", got, before+5)
	}

	session.ReportsDismiss()
	waitForReports(t, session, events, "the dismissed message", func(page ReportsPageState) bool {
		return page.State.Message == nil && page.State.Error == nil
	})
}

func TestReportsExportsWriteTheDialogPath(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)
	fake := &fakeFileDialogs{}
	session.SetFileDialogs(fake)

	type exportCase struct {
		name        string
		title       i18n.Key
		defaultName string
		call        func()
		prefix      string
		contains    string
	}
	cases := []exportCase{
		{
			name:        "report.pdf",
			title:       i18n.ExportReport,
			defaultName: "report-2026.pdf",
			call:        session.ReportsExportReportPDF,
			prefix:      "%PDF-",
		},
		{
			name:        "transactions.csv",
			title:       i18n.ExportTransactions,
			defaultName: "finglass-2026.csv",
			call:        session.ReportsExportCSV,
			prefix:      "\ufeffDate,Partner,Purpose",
			contains:    "Employer",
		},
		{
			name:        "tax.xlsx",
			title:       i18n.ExportExcel,
			defaultName: "tax-bookings-2026.xlsx",
			call:        session.ReportsExportTaxXLSX,
			prefix:      "PK\x03\x04",
		},
		{
			name:        "tax.pdf",
			title:       i18n.ExportPdf,
			defaultName: "tax-bookings-2026.pdf",
			call:        session.ReportsExportTaxPDF,
			prefix:      "%PDF-",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), testCase.name)
			fake.mu.Lock()
			fake.filePath = path
			fake.fileResult = true
			fake.fileCalls = 0
			fake.mu.Unlock()

			testCase.call()
			waitForReports(t, session, events, "the started export", func(page ReportsPageState) bool {
				return page.State.IsExporting
			})
			page := waitForReports(t, session, events, "the finished export", func(page ReportsPageState) bool {
				return !page.State.IsExporting && page.State.Message != nil
			})
			if page.State.Error != nil {
				t.Fatalf("export error = %q", *page.State.Error)
			}
			fake.mu.Lock()
			title := fake.fileTitle
			defaultName := fake.fileDefaultName
			calls := fake.fileCalls
			fake.mu.Unlock()
			if calls != 1 || title != testCase.title || defaultName != testCase.defaultName {
				t.Fatalf("dialog = %d calls, title %q, name %q", calls, title, defaultName)
			}
			content, err := os.ReadFile(path) // #nosec G304 -- test reads the file its fake dialog chose.
			if err != nil {
				t.Fatalf("read export: %v", err)
			}
			if !strings.HasPrefix(string(content), testCase.prefix) {
				t.Fatalf("export prefix = %q", content[:min(len(content), len(testCase.prefix))])
			}
			if testCase.contains != "" && !strings.Contains(string(content), testCase.contains) {
				t.Fatalf("export does not contain %q", testCase.contains)
			}
		})
	}
}

func TestReportsCancelledExportWritesNothingAndClearsExporting(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)
	path := filepath.Join(t.TempDir(), "cancelled.csv")
	fake := &fakeFileDialogs{filePath: path, fileResult: false}
	session.SetFileDialogs(fake)

	session.ReportsExportCSV()
	waitForReports(t, session, events, "the started cancelled export", func(page ReportsPageState) bool {
		return page.State.IsExporting
	})
	page := waitForReports(t, session, events, "the cancelled export", func(page ReportsPageState) bool {
		return !page.State.IsExporting
	})
	if page.State.Message != nil || page.State.Error != nil {
		t.Fatalf("a cancelled export left a message: %+v", page.State)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a cancelled export wrote %q: %v", path, err)
	}
}

type fakeFileOpener struct {
	mu    sync.Mutex
	paths []string
}

func (o *fakeFileOpener) OpenFile(path string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.paths = append(o.paths, path)
	return nil
}

func (o *fakeFileOpener) opened() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.paths...)
}

func TestReportsPrintTaxRecordsExactlyOneOpenedPath(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)
	opener := &fakeFileOpener{}
	session.SetFileOpener(opener)

	session.ReportsPrintTax()
	page := waitForReports(t, session, events, "the printed tax PDF", func(page ReportsPageState) bool {
		return !page.State.IsExporting && page.State.Message != nil
	})
	if *page.State.Message != i18n.Text(i18n.En, i18n.MessagePrintSent) {
		t.Fatalf("print message = %q", *page.State.Message)
	}
	paths := opener.opened()
	if len(paths) != 1 {
		t.Fatalf("opened paths = %v", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("printed PDF: %v", err)
	}
	if !strings.HasSuffix(paths[0], ".pdf") {
		t.Fatalf("printed path = %q", paths[0])
	}
	t.Cleanup(func() { _ = os.Remove(paths[0]) })
}

func TestReportsRefuseASecondActionWhileAnExportRuns(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	fake := &fakeFileDialogs{
		filePath:   filepath.Join(t.TempDir(), "busy.csv"),
		fileResult: true,
	}
	fake.onFile = func() {
		close(entered)
		<-release
	}
	session.SetFileDialogs(fake)

	session.ReportsExportCSV()
	<-entered

	before := session.ReportsState()
	session.ReportsChangeYear(-1)
	session.ReportsSetMissingOnly(true)
	session.ReportsApplyRange("01.01.2026", "31.01.2026")
	session.ReportsDismiss()
	after := session.ReportsState()
	if after.State.SelectedYear != before.State.SelectedYear ||
		after.State.MissingReceiptsOnly != before.State.MissingReceiptsOnly ||
		after.State.CustomRange != nil ||
		after.RangeError != "" {
		t.Fatalf("a busy action changed the state: before %+v after %+v", before, after)
	}
	if !after.State.IsExporting {
		t.Fatal("the export stopped being busy")
	}

	close(release)
	waitForReports(t, session, events, "the export after the refusal", func(page ReportsPageState) bool {
		return !page.State.IsExporting && page.State.Message != nil
	})
}

func TestASupersededReportsLoadIsDropped(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "reports" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.ReportsChangeYear(1)
	first := <-entered
	session.reportsWork.End()
	session.ReportsChangeYear(2)
	second := <-entered

	close(second)
	waitForReports(t, session, events, "the second reports load", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.SelectedYear == 2029
	})
	close(first)
	waitForJobs(t, session)

	page := session.ReportsState()
	if page.State.SelectedYear != 2029 {
		t.Fatalf("the superseded load published %d", page.State.SelectedYear)
	}
	if page.State.IsLoading {
		t.Fatal("the superseded load published the loading state")
	}
}

func TestTeardownDuringAReportsLoadDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session, _, _ := newLoadedReportsSession(t)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		if page != "reports" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.ReportsChangeYear(1)
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the reports load")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the reports job did not end")
	}

	requireClosed(t, old, "the old database after the reports job ends")
	page := session.ReportsState()
	if page.State.TaxReport != nil || page.State.IsLoading {
		t.Fatalf("the teardown did not drop the load: %+v", page.State)
	}
	if page.Tab != "summary" {
		t.Fatalf("the teardown kept the tab %q", page.Tab)
	}
}

func TestTeardownDuringAReportsExportDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	session, _, _ := newLoadedReportsSession(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	path := filepath.Join(t.TempDir(), "teardown.csv")
	fake := &fakeFileDialogs{filePath: path, fileResult: true}
	fake.onFile = func() {
		close(entered)
		<-release
	}
	session.SetFileDialogs(fake)

	session.ReportsExportCSV()
	<-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the reports export")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the reports export did not end")
	}

	requireClosed(t, old, "the old database after the reports export ends")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the torn-down export wrote %q: %v", path, err)
	}
	page := session.ReportsState()
	if page.State.IsExporting || page.State.TaxReport != nil {
		t.Fatalf("the teardown did not drop the export: %+v", page.State)
	}
}

func TestReportsStateIsSafeWhileActionsRun(t *testing.T) {
	session, _, events := newLoadedReportsSession(t)

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.ReportsState()
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	for _, delta := range []int{-1, 1, -2, 2} {
		session.ReportsChangeYear(delta)
		expected := session.ReportsState().State.SelectedYear
		waitForReports(t, session, events, "a year while reading", func(page ReportsPageState) bool {
			return !page.State.IsLoading && page.State.SelectedYear == expected
		})
	}

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadReportsWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedReportsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)

	called := make(chan struct{}, 16)
	session.AddListener(func(Change) {
		_ = session.ReportsState()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func TestReportsCategoryLinksFollowTheLinkableRules(t *testing.T) {
	session, seed, events := newLoadedReportsSession(t)

	session.ReportsOpenCategory(int(seed.health))
	if screen := session.CurrentScreen(); screen != "transactions" {
		t.Fatalf("screen = %q", screen)
	}
	args := session.NavCurrent().Transactions
	if args.Period == nil || *args.Period != transactionsview.PeriodThisYear ||
		args.CategoryID == nil || *args.CategoryID != seed.health ||
		args.UncategorizedOnly {
		t.Fatalf("category args = %+v", args)
	}

	waitForJobs(t, session)
	session.ReportsOpenCategory(-1)
	args = session.NavCurrent().Transactions
	if args.Period == nil || *args.Period != transactionsview.PeriodThisYear ||
		args.CategoryID != nil || !args.UncategorizedOnly {
		t.Fatalf("uncategorized args = %+v", args)
	}

	year := 2025
	session.ReportsOpen(&year)
	waitForReports(t, session, events, "the 2025 tax tab", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.Tab == "tax" && page.State.SelectedYear == 2025
	})
	session.ReportsOpenCategory(int(seed.health))
	args = session.NavCurrent().Transactions
	if args.Period == nil || *args.Period != transactionsview.PeriodLastYear {
		t.Fatalf("last-year args = %+v", args)
	}

	waitForJobs(t, session)
	session.ReportsOpen(&year)
	waitForReports(t, session, events, "the 2025 tax tab again", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.Tab == "tax" && page.State.SelectedYear == 2025
	})
	session.ReportsApplyRange("01.12.2025", "31.01.2026")
	waitForReports(t, session, events, "a custom range", func(page ReportsPageState) bool {
		return !page.State.IsLoading && page.State.CustomRange != nil
	})
	session.ReportsOpenCategory(int(seed.health))
	if screen := session.CurrentScreen(); screen != "reports" {
		t.Fatalf("a custom range linked to %q", screen)
	}
}

func TestReportsFileNamesUseTheYearOrTheISORange(t *testing.T) {
	state := reportsview.New(i18n.En, reportsSeedDate(t, 2026, time.October, 15))
	state.SelectedYear = 2026
	if got := reportsFileSuffix(state); got != "2026" {
		t.Fatalf("suffix = %q", got)
	}
	want := map[reportsExportFile]string{
		reportsExportReportPDF: "report-2026.pdf",
		reportsExportCSV:       "finglass-2026.csv",
		reportsExportTaxXLSX:   "tax-bookings-2026.xlsx",
		reportsExportTaxPDF:    "tax-bookings-2026.pdf",
	}
	for file, name := range want {
		if got := reportsDefaultFileName(state, file); got != name {
			t.Fatalf("name = %q, want %q", got, name)
		}
	}

	state.CustomRange = &reportsview.DateRange{
		From: reportsSeedDate(t, 2025, time.December, 1),
		To:   reportsSeedDate(t, 2026, time.January, 31),
	}
	if got := reportsFileSuffix(state); got != "2025-12-01_2026-01-31" {
		t.Fatalf("custom suffix = %q", got)
	}
	if got := reportsDefaultFileName(state, reportsExportReportPDF); got != "report-2025-12-01_2026-01-31.pdf" {
		t.Fatalf("custom name = %q", got)
	}
}

func TestReportsCategoryLinksOnlyCoverThisOrLastCalendarYear(t *testing.T) {
	today := reportsSeedDate(t, 2026, time.October, 15)
	if !reportsLinkable(report.EmptyTaxReport(2026), today) {
		t.Fatal("this year does not link")
	}
	if !reportsLinkable(report.EmptyTaxReport(2025), today) {
		t.Fatal("last year does not link")
	}
	if reportsLinkable(report.EmptyTaxReport(2024), today) {
		t.Fatal("a year before last links")
	}

	partial := report.EmptyTaxReport(2026)
	partial.From = reportsSeedDate(t, 2026, time.February, 1)
	if reportsLinkable(partial, today) {
		t.Fatal("a custom range links")
	}
}
