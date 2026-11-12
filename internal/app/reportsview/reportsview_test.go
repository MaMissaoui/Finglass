package reportsview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/documents/pdftext"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/documents"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/report"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

var nextHash atomic.Int64

func testDatabase(t *testing.T) *db.Database {
	t.Helper()

	passphrase, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), passphrase)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return date
}

func testAccount(t *testing.T, d *db.Database, currency money.Currency) model.AccountID {
	t.Helper()

	name := "Account"
	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        &name,
		Currency:    currency,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: currency},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func testCategory(t *testing.T, d *db.Database, name, color string, taxRelevant bool) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:          name,
		Color:         color,
		IsTaxRelevant: taxRelevant,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func testTransaction(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	minor int64,
	currency money.Currency,
	partner *string,
	purpose *string,
	categoryID *model.CategoryID,
) model.TransactionID {
	t.Helper()

	hash := fmt.Sprintf("test-%d", nextHash.Add(1))
	_, err := transactions.InsertImported(context.Background(), d, accountID, []model.NewTransaction{{
		BookingDate: day,
		Amount:      money.Money{Minor: minor, Currency: currency},
		PartnerName: partner,
		Purpose:     purpose,
		CategoryID:  categoryID,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("transactions.InsertImported: %v", err)
	}
	rows, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("transactions.ListForAccount: %v", err)
	}
	for _, row := range rows {
		if row.ImportHash == hash {
			return row.ID
		}
	}
	t.Fatalf("transaction with hash %q not found", hash)
	return 0
}

func withReceipt(t *testing.T, d *db.Database, transactionID model.TransactionID) {
	t.Helper()

	_, err := documents.Create(context.Background(), d, model.NewDocument{
		FileName:      "Beleg.pdf",
		StoredPath:    fmt.Sprintf("documents/beleg-%d.pdf", transactionID),
		MIMEType:      "application/pdf",
		TransactionID: &transactionID,
	})
	if err != nil {
		t.Fatalf("documents.Create: %v", err)
	}
}

func auditEntries(t *testing.T, d *db.Database, action string) []audit.Entry {
	t.Helper()

	entries, err := audit.Recent(context.Background(), d, 10_000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	filtered := make([]audit.Entry, 0)
	for _, entry := range entries {
		if entry.Action == action {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func auditCount(t *testing.T, d *db.Database) int {
	t.Helper()

	entries, err := audit.Recent(context.Background(), d, 10_000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return len(entries)
}

func saved(language i18n.Language, path string) string {
	return i18n.Format(language, i18n.MessageSavedTo, i18n.Str(fileName(path)))
}

func pdfText(t *testing.T, path string) *pdftext.Text {
	t.Helper()

	text, ok := pdftext.Extract(path, 20)
	if !ok {
		t.Fatalf("the exported PDF %q is readable", path)
	}
	return text
}

func stringPointer(value string) *string {
	return &value
}

func categoryPointer(id model.CategoryID) *model.CategoryID {
	return &id
}

func assertSettingKeysPinned(t *testing.T) {
	t.Helper()

	if money.PrimaryCurrencyKey != "money.primary_currency" {
		t.Fatalf("primary currency key = %q", money.PrimaryCurrencyKey)
	}
	if report.KeyKeywords != "tax.suggestionKeywords" {
		t.Fatalf("suggestion key = %q", report.KeyKeywords)
	}
}

// TaxTabTest.

func TestLoadingAYearFillsTheTabAndAcceptingAndDismissingSuggestionsWriteOneAuditEntryEach(t *testing.T) {
	assertSettingKeysPinned(t)
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	income := testCategory(t, database, "Gehalt", "#4CAF50", true)
	deduction := testCategory(t, database, "Spenden", "#9C27B0", true)
	salary := testTransaction(
		t, database, account, testDate(t, 2026, time.February, 1),
		200_000, money.EUR, stringPointer("Beispiel AG"), nil, categoryPointer(income),
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.March, 1),
		-12_000, money.EUR, stringPointer("Beispielhilfe e. V."),
		stringPointer("Spende"), categoryPointer(deduction),
	)
	suggested := testTransaction(
		t, database, account, testDate(t, 2026, time.April, 1),
		-3_000, money.EUR, stringPointer("Beispiel-Apotheke"),
		stringPointer("Medikamente"), nil,
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.May, 1),
		-900, money.EUR, stringPointer("Bäckerei Beispiel"), nil, nil,
	)
	state := New(i18n.En, testDate(t, 2026, time.June, 15))

	state.LoadTaxReport(ctx, database, 2026)
	loaded := *state
	if loaded.TaxBookings == nil {
		t.Fatal("loading fills tax bookings")
	}
	incomeGroups := make([]*model.CategoryID, 0)
	for _, group := range loaded.TaxBookings.IncomeGroups {
		incomeGroups = append(incomeGroups, group.CategoryID)
	}
	if len(incomeGroups) != 1 || incomeGroups[0] == nil || *incomeGroups[0] != income {
		t.Fatalf("income groups = %+v, want the salary category", incomeGroups)
	}
	deductionGroups := make([]*model.CategoryID, 0)
	for _, group := range loaded.TaxBookings.DeductionGroups {
		deductionGroups = append(deductionGroups, group.CategoryID)
	}
	if len(deductionGroups) != 1 || deductionGroups[0] == nil || *deductionGroups[0] != deduction {
		t.Fatalf("deduction groups = %+v, want the donation category", deductionGroups)
	}
	if loaded.TaxBookings.BookingCount != 2 {
		t.Fatalf("booking count = %d, want the two tax-relevant bookings", loaded.TaxBookings.BookingCount)
	}
	if len(loaded.Suggestions) != 1 || loaded.Suggestions[0].Booking.ID != suggested {
		t.Fatalf("suggestions = %+v, want the Apotheke booking", loaded.Suggestions)
	}
	if loaded.Suggestions[0].Keyword != "Apotheke" {
		t.Fatalf("suggestion keyword = %q", loaded.Suggestions[0].Keyword)
	}

	state.AcceptSuggestion(ctx, database, suggested)
	if len(state.Suggestions) != 0 {
		t.Fatal("the accepted suggestion is gone")
	}
	if state.TaxBookings.BookingCount != 3 {
		t.Fatalf("booking count = %d, want the accepted booking in", state.TaxBookings.BookingCount)
	}
	found := false
	for _, group := range state.TaxBookings.DeductionGroups {
		for _, booking := range group.Bookings {
			if booking.ID == suggested {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the accepted booking is a deduction")
	}

	dismissed := testTransaction(
		t, database, account, testDate(t, 2026, time.June, 1),
		-20_000, money.EUR, stringPointer("Handwerker Beispiel"), nil, nil,
	)
	state.LoadTaxReport(ctx, database, 2026)
	if len(state.Suggestions) != 1 || state.Suggestions[0].Booking.ID != dismissed {
		t.Fatalf("suggestions = %+v, want the dismissed booking", state.Suggestions)
	}
	state.DismissSuggestion(ctx, database, dismissed)
	if len(state.Suggestions) != 0 {
		t.Fatal("the dismissed suggestion is hidden")
	}
	state.UpdateYear(ctx, database, 2026)
	if len(state.Suggestions) != 0 {
		t.Fatal("the dismissed suggestion stays hidden after a reload")
	}

	entries := auditEntries(t, database, "transaction.set_tax_override")
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want one per action", len(entries))
	}
	hasTrue := false
	hasFalse := false
	for _, entry := range entries {
		if strings.Contains(entry.Details, "count=1") && strings.Contains(entry.Details, "Some(true)") {
			hasTrue = true
		}
		if strings.Contains(entry.Details, "count=1") && strings.Contains(entry.Details, "Some(false)") {
			hasFalse = true
		}
		if strings.Contains(entry.Details, "Apotheke") {
			t.Fatalf("no partner text in the audit entries: %+v", entries)
		}
		if strings.Contains(entry.Details, "-30.00") {
			t.Fatalf("no amount in the audit entries: %+v", entries)
		}
	}
	if !hasTrue {
		t.Fatalf("the accept entry names count and value: %+v", entries)
	}
	if !hasFalse {
		t.Fatalf("the dismiss entry names count and value: %+v", entries)
	}

	filter := model.TransactionFilter{}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	byID := make(map[model.TransactionID]model.TransactionItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	if byID[dismissed].TaxOverride == nil || *byID[dismissed].TaxOverride != false {
		t.Fatalf("dismissed override = %+v, want false", byID[dismissed].TaxOverride)
	}
	if byID[suggested].TaxOverride == nil || *byID[suggested].TaxOverride != true {
		t.Fatalf("suggested override = %+v, want true", byID[suggested].TaxOverride)
	}
	if !byID[salary].IsTaxRelevant {
		t.Fatal("the salary booking keeps its category relevance")
	}
}

func TestMarkNotTaxRelevantRemovesACategoryRelevantBookingAndResetTaxAutomaticBringsItBack(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	deduction := testCategory(t, database, "Spenden", "#9C27B0", true)
	donation := testTransaction(
		t, database, account, testDate(t, 2026, time.March, 1),
		-12_000, money.EUR, stringPointer("Beispielhilfe e. V."),
		nil, categoryPointer(deduction),
	)
	state := New(i18n.En, testDate(t, 2026, time.June, 15))
	state.LoadTaxReport(ctx, database, 2026)

	ids := func(group report.TaxGroup) []model.TransactionID {
		result := make([]model.TransactionID, 0)
		for _, booking := range group.Bookings {
			result = append(result, booking.ID)
		}
		return result
	}
	if got := ids(state.TaxBookings.DeductionGroups[0]); len(got) != 1 || got[0] != donation {
		t.Fatalf("deduction bookings = %+v, want the donation", got)
	}

	state.MarkNotTaxRelevant(ctx, database, donation)
	if len(state.TaxBookings.DeductionGroups) != 0 {
		t.Fatal("the booking is out of the groups")
	}
	if state.TaxBookings.BookingCount != 0 {
		t.Fatalf("booking count = %d, want no tax-relevant bookings", state.TaxBookings.BookingCount)
	}
	filter := model.TransactionFilter{}
	items, err := transactions.FindItems(ctx, database, &filter, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	var marked model.TransactionItem
	for _, item := range items {
		if item.ID == donation {
			marked = item
		}
	}
	if marked.IsTaxRelevant {
		t.Fatal("the marked booking is not tax relevant")
	}
	if marked.TaxSource != model.TaxSourceManual {
		t.Fatalf("tax source = %q, want manual", marked.TaxSource)
	}
	if marked.TaxOverride == nil || *marked.TaxOverride != false {
		t.Fatalf("tax override = %+v, want Some(false)", marked.TaxOverride)
	}

	state.ResetTaxAutomatic(ctx, database, donation)
	items, err = transactions.FindItems(ctx, database, &filter, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	var reset model.TransactionItem
	for _, item := range items {
		if item.ID == donation {
			reset = item
		}
	}
	if reset.TaxOverride != nil {
		t.Fatalf("tax override = %+v, want None", reset.TaxOverride)
	}
	if !reset.IsTaxRelevant {
		t.Fatal("the category flag decides again")
	}
	if got := ids(state.TaxBookings.DeductionGroups[0]); len(got) != 1 || got[0] != donation {
		t.Fatalf("deduction bookings = %+v, want the donation", got)
	}

	entries := auditEntries(t, database, "transaction.set_tax_override")
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want one per action", len(entries))
	}
	hasFalse := false
	hasNone := false
	for _, entry := range entries {
		if strings.Contains(entry.Details, "count=1") && strings.Contains(entry.Details, "Some(false)") {
			hasFalse = true
		}
		if strings.Contains(entry.Details, "count=1") && strings.Contains(entry.Details, "None") {
			hasNone = true
		}
		if strings.Contains(entry.Details, "Beispiel") {
			t.Fatalf("no booking text in the audit entries: %+v", entries)
		}
	}
	if !hasFalse || !hasNone {
		t.Fatalf("audit entries = %+v, want the false and None details", entries)
	}
}

func TestSetMissingReceiptsOnlyFiltersRowsButKeepsTheWholePeriodsTotals(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	deduction := testCategory(t, database, "Spenden", "#9C27B0", true)
	withReceiptID := testTransaction(
		t, database, account, testDate(t, 2026, time.March, 1),
		-12_000, money.EUR, stringPointer("Beispielhilfe e. V."),
		stringPointer("Spende"), categoryPointer(deduction),
	)
	withReceipt(t, database, withReceiptID)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.April, 1),
		-3_000, money.EUR, stringPointer("Beispiel-Apotheke"),
		stringPointer("Medikamente"), categoryPointer(deduction),
	)
	state := New(i18n.En, testDate(t, 2026, time.June, 15))
	state.LoadTaxReport(ctx, database, 2026)

	before := *state.TaxBookings
	beforeGroup := before.DeductionGroups[0]
	if len(beforeGroup.Bookings) != 2 {
		t.Fatalf("deduction bookings = %d, want 2", len(beforeGroup.Bookings))
	}

	state.SetMissingReceiptsOnly(true)
	after := *state
	if !after.MissingReceiptsOnly {
		t.Fatal("the filter flag is on for the shown rows")
	}
	if before.DeductionTotal() != after.TaxBookings.DeductionTotal() {
		t.Fatal("the totals stay those of the whole period")
	}
	if before.BookingCount != after.TaxBookings.BookingCount {
		t.Fatal("the booking count stays that of the whole period")
	}
	if len(beforeGroup.Bookings) != len(after.TaxBookings.DeductionGroups[0].Bookings) {
		t.Fatal("the data itself is unfiltered")
	}
	state.SetMissingReceiptsOnly(false)
	if state.MissingReceiptsOnly {
		t.Fatal("the filter flag is off again")
	}
}

// TaxExportTest.

func taxExportSetup(t *testing.T, d *db.Database) {
	t.Helper()

	account := testAccount(t, d, money.EUR)
	donations := testCategory(t, d, "Spenden", "#9C27B0", true)
	_ = testTransaction(
		t, d, account, testDate(t, 2026, time.March, 1),
		-12_000, money.EUR, stringPointer("Beispielhilfe e. V."),
		stringPointer("Spende"), categoryPointer(donations),
	)
}

func TestExportsWriteTheFilesAndSetTheSavedMessage(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	taxExportSetup(t, database)
	state := New(i18n.En, testDate(t, 2026, time.November, 7))
	state.LoadTaxReport(ctx, database, 2026)
	auditBefore := auditCount(t, database)

	directory := t.TempDir()
	xlsx := filepath.Join(directory, "tax.xlsx")
	state.ExportTaxXLSX(ctx, database, xlsx, testDate(t, 2026, time.November, 7))
	if info, err := os.Stat(xlsx); err != nil || !info.Mode().IsRegular() {
		t.Fatal("the workbook was written")
	}
	if state.Message == nil || *state.Message != saved(i18n.En, xlsx) {
		t.Fatalf("message = %v, want the saved message", state.Message)
	}
	if state.Error != nil {
		t.Fatalf("error = %v", *state.Error)
	}
	if state.IsExporting {
		t.Fatal("the busy flag is cleared")
	}

	pdf := filepath.Join(directory, "tax.pdf")
	state.ExportTaxPDF(ctx, database, pdf, testDate(t, 2026, time.November, 7))
	if info, err := os.Stat(pdf); err != nil || !info.Mode().IsRegular() {
		t.Fatal("the PDF was written")
	}
	if state.Message == nil || *state.Message != saved(i18n.En, pdf) {
		t.Fatalf("message = %v, want the saved message", state.Message)
	}
	if state.Error != nil {
		t.Fatalf("error = %v", *state.Error)
	}
	if pages := pdfText(t, pdf).PageCount; pages < 1 {
		t.Fatalf("page count = %d, want at least 1", pages)
	}
	if auditBefore != auditCount(t, database) {
		t.Fatal("exports are read-only: no audit entry")
	}
}

func TestPrintTaxWritesATemporaryPDFAndReportsSent(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	taxExportSetup(t, database)
	state := New(i18n.En, testDate(t, 2026, time.November, 7))
	state.LoadTaxReport(ctx, database, 2026)
	auditBefore := auditCount(t, database)

	path := state.PrintTax(ctx, database, testDate(t, 2026, time.November, 7))
	if path == nil {
		t.Fatal("the print file was written")
	}
	text := pdfText(t, *path)
	if text.PageCount != 1 {
		t.Fatalf("page count = %d, want the synthetic booking on one page", text.PageCount)
	}
	if !strings.Contains(text.Text, "Tax-relevant bookings 2026") {
		t.Fatalf("the document names the report: %s", text.Text)
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessagePrintSent) {
		t.Fatalf("message = %v, want sent", state.Message)
	}
	if state.Error != nil {
		t.Fatalf("error = %v", *state.Error)
	}
	if state.IsExporting {
		t.Fatal("the busy flag is cleared")
	}
	if auditBefore != auditCount(t, database) {
		t.Fatal("printing is read-only: no audit entry")
	}

	if err := os.Remove(*path); err != nil {
		t.Fatalf("remove print file: %v", err)
	}
}

// The printer cancellation/failure case is not ported: there is no printer
// adapter in core. PrintTax only writes a temporary file and the page opens it
// in the system viewer; a write failure sets ErrorPrintFailed.

// ReportsPeriodDataTest.

func TestExportsUseTheNewlySelectedYearAndNeverAnotherYearsCachedData(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	donations := testCategory(t, database, "Spenden", "#FF5722", true)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.March, 1),
		-10_000, money.EUR, stringPointer("YearOneSpende"), nil, categoryPointer(donations),
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2027, time.March, 1),
		-20_000, money.EUR, stringPointer("YearTwoSpende"), nil, categoryPointer(donations),
	)
	state := New(i18n.En, testDate(t, 2027, time.June, 15))

	state.LoadTaxReport(ctx, database, 2026)
	if state.LoadedRange.From != testDate(t, 2026, time.January, 1) {
		t.Fatalf("loaded from = %v", state.LoadedRange.From)
	}

	// The synchronous API finishes the year-two load before the export, so the
	// cached data is year two.
	state.LoadTaxReport(ctx, database, 2027)
	if state.LoadedRange.From != testDate(t, 2027, time.January, 1) {
		t.Fatal("the load published year two")
	}

	directory := t.TempDir()
	xlsx := filepath.Join(directory, "tax.xlsx")
	state.ExportTaxXLSX(ctx, database, xlsx, testDate(t, 2027, time.November, 7))
	// #nosec G304 -- the test chooses the exported workbook path.
	bytes, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatalf("read workbook: %v", err)
	}
	rows, err := imports.FirstSheetRows(bytes)
	if err != nil {
		t.Fatalf("FirstSheetRows: %v", err)
	}
	titled := false
	for _, cell := range rows[0] {
		if strings.Contains(cell, "2027") {
			titled = true
		}
	}
	if !titled {
		t.Fatalf("the workbook is titled for the selected year: %+v", rows[0])
	}
	hasDeduction := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "200" {
				hasDeduction = true
			}
		}
	}
	if !hasDeduction {
		t.Fatalf("the workbook holds year 2's deduction: %+v", rows)
	}

	pdf := filepath.Join(directory, "tax.pdf")
	state.ExportPDF(ctx, database, pdf, testDate(t, 2027, time.November, 7))
	text := pdfText(t, pdf).Text
	if !strings.Contains(text, "YearTwoSpende") {
		t.Fatalf("the PDF holds year 2's booking: %s", text)
	}
	if strings.Contains(text, "YearOneSpende") {
		t.Fatalf("the PDF does not hold year 1's booking: %s", text)
	}
}

// The original coroutine-timing test blocked the year-two repository call and
// checked that exports still use the new year while the cached data was old.
// The synchronous API cannot be mid-load, so that blocking part is not ported;
// the assertions about never exporting another period's data are.

// InsightsTest (reports part).

func TestCustomRangeReportsSpanYears(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	_ = testTransaction(
		t, database, account, testDate(t, 2025, time.December, 20),
		-3_000, money.EUR, stringPointer("Dec"), nil, nil,
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.January, 10),
		-2_000, money.EUR, stringPointer("Jan"), nil, nil,
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.February, 10),
		-9_900, money.EUR, stringPointer("Outside"), nil, nil,
	)

	reportValue, err := report.GenerateRangeReport(
		ctx,
		database,
		testDate(t, 2025, time.December, 1),
		testDate(t, 2026, time.January, 31),
		i18n.En,
	)
	if err != nil {
		t.Fatalf("GenerateRangeReport: %v", err)
	}
	months := make([][2]int, 0)
	for _, month := range reportValue.MonthlyBreakdown {
		months = append(months, [2]int{month.Year, int(month.Month)})
	}
	wantMonths := [][2]int{{2025, 12}, {2026, 1}}
	if len(months) != len(wantMonths) {
		t.Fatalf("months = %+v, want %+v", months, wantMonths)
	}
	for index := range months {
		if months[index] != wantMonths[index] {
			t.Fatalf("months = %+v, want %+v", months, wantMonths)
		}
	}
	if reportValue.TotalExpenses != (money.Money{Minor: 5_000, Currency: money.EUR}) {
		t.Fatalf("total expenses = %+v", reportValue.TotalExpenses)
	}
	if reportValue.IsCalendarYear() {
		t.Fatal("the range is not a calendar year")
	}
	if title := reportValue.Title(i18n.En); title != "01.12.2025 – 31.01.2026" {
		t.Fatalf("title = %q", title)
	}

	state := New(i18n.En, testDate(t, 2026, time.March, 15))
	state.LoadTaxReport(ctx, database, 2026)
	state.SetCustomRange(
		ctx,
		database,
		testDate(t, 2025, time.December, 1),
		testDate(t, 2026, time.January, 31),
	)
	if state.TaxReport.TotalExpenses != (money.Money{Minor: 5_000, Currency: money.EUR}) {
		t.Fatal("the range report replaces the year report")
	}
}

// The original also checked that a late year result cannot replace the range.
// The synchronous API has no in-flight year load, so that timing-only assertion
// is not ported.

// ViewModelsTest (reports part).

func TestReportsViewModelLoadsAndExports(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	account := testAccount(t, database, money.EUR)
	food := testCategory(t, database, "Food", "#4CAF50", false)
	donations := testCategory(t, database, "Spenden", "#FF5722", true)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.March, 2),
		-3_000, money.EUR, stringPointer("REWE"), nil, categoryPointer(food),
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.March, 5),
		-2_000, money.EUR, stringPointer("Kiosk"), nil, nil,
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.March, 1),
		250_000, money.EUR, stringPointer("Employer"), nil, nil,
	)
	_ = testTransaction(
		t, database, account, testDate(t, 2026, time.January, 15),
		-10_000, money.EUR, stringPointer("UNICEF"), nil, categoryPointer(donations),
	)
	state := New(i18n.En, testDate(t, 2026, time.March, 20))
	state.LoadTaxReport(ctx, database, 2026)
	if state.TaxReport.TotalIncome != (money.Money{Minor: 250_000, Currency: money.EUR}) {
		t.Fatalf("total income = %+v", state.TaxReport.TotalIncome)
	}

	pdf := filepath.Join(t.TempDir(), "r.pdf")
	state.ExportPDF(ctx, database, pdf, testDate(t, 2026, time.March, 20))
	// #nosec G304 -- the test chooses the exported PDF path.
	bytes, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatalf("read PDF: %v", err)
	}
	if len(bytes) == 0 {
		t.Fatal("the PDF is not empty")
	}
	if state.Message == nil || *state.Message != "Saved r.pdf" {
		t.Fatalf("message = %v", state.Message)
	}
}

// MultiCurrencyTest (reports parts).

func TestThePrimaryCurrencySettingSwitchesWhatReportsTotal(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	eur := testAccount(t, database, money.EUR)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	usd := testAccount(t, database, usdCurrency)
	_ = testTransaction(
		t, database, eur, testDate(t, 2026, time.October, 2),
		200_000, money.EUR, stringPointer("Employer"), nil, nil,
	)
	_ = testTransaction(
		t, database, usd, testDate(t, 2026, time.October, 3),
		80_000, usdCurrency, stringPointer("US client"), nil, nil,
	)
	if err := settings.Set(ctx, database, money.PrimaryCurrencyKey, "USD"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	state := New(i18n.En, testDate(t, 2026, time.October, 15))
	state.LoadTaxReport(ctx, database, 2026)
	reportValue := state.TaxReport

	if reportValue.Currency != usdCurrency {
		t.Fatalf("currency = %s", reportValue.Currency)
	}
	if reportValue.TotalIncome != (money.Money{Minor: 80_000, Currency: usdCurrency}) {
		t.Fatalf("total income = %+v", reportValue.TotalIncome)
	}
	if reportValue.OtherCurrencyBookings != 1 {
		t.Fatalf("other currency bookings = %d", reportValue.OtherCurrencyBookings)
	}
}

func TestEmptyTotalsFallBackToThePrimaryCurrency(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	if err := settings.Set(ctx, database, money.PrimaryCurrencyKey, "USD"); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}

	state := New(i18n.En, testDate(t, 2026, time.October, 15))
	state.LoadTaxReport(ctx, database, 2026)

	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	if state.PrimaryCurrency != usd {
		t.Fatalf("primary currency = %s", state.PrimaryCurrency)
	}
}

func TestReportsCountBookingsInOtherCurrenciesInsteadOfAddingThem(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	eur := testAccount(t, database, money.EUR)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	usd := testAccount(t, database, usdCurrency)
	_ = testTransaction(
		t, database, eur, testDate(t, 2026, time.March, 1),
		-3_000, money.EUR, stringPointer("REWE"), nil, nil,
	)
	_ = testTransaction(
		t, database, usd, testDate(t, 2026, time.March, 2),
		-7_000, usdCurrency, stringPointer("Walmart"), nil, nil,
	)

	state := New(i18n.En, testDate(t, 2026, time.March, 15))
	state.LoadTaxReport(ctx, database, 2026)
	reportValue := state.TaxReport

	if reportValue.Currency != money.EUR {
		t.Fatalf("currency = %s", reportValue.Currency)
	}
	if reportValue.TotalExpenses != (money.Money{Minor: 3_000, Currency: money.EUR}) {
		t.Fatalf("total expenses = %+v", reportValue.TotalExpenses)
	}
	if reportValue.OtherCurrencyBookings != 1 {
		t.Fatalf("other currency bookings = %d", reportValue.OtherCurrencyBookings)
	}
	if reportValue.TransactionCount != 1 {
		t.Fatalf("transaction count = %d", reportValue.TransactionCount)
	}
}

func TestRecurringPaymentsArePerCurrency(t *testing.T) {
	ctx := context.Background()
	database := testDatabase(t)
	eur := testAccount(t, database, money.EUR)
	usdCurrency, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	usd := testAccount(t, database, usdCurrency)
	for month := time.June; month <= time.August; month++ {
		day := testDate(t, 2026, month, 5)
		_ = testTransaction(
			t, database, eur, day, -999, money.EUR,
			stringPointer("Spotify"), nil, nil,
		)
		_ = testTransaction(
			t, database, usd, day, -1_199, usdCurrency,
			stringPointer("Spotify"), nil, nil,
		)
	}

	state := New(i18n.En, testDate(t, 2026, time.September, 20))
	state.LoadRecurring(ctx, database, testDate(t, 2026, time.September, 20))

	type paymentKey struct {
		currency money.Currency
		minor    int64
	}
	payments := make(map[paymentKey]struct{})
	for _, payment := range state.Recurring {
		payments[paymentKey{
			currency: payment.TypicalAmount.Currency,
			minor:    payment.TypicalAmount.Minor,
		}] = struct{}{}
	}
	want := map[paymentKey]struct{}{
		{currency: money.EUR, minor: -999}:     {},
		{currency: usdCurrency, minor: -1_199}: {},
	}
	if len(payments) != len(want) {
		t.Fatalf("recurring payments = %+v, want %+v", payments, want)
	}
	for key := range want {
		if _, ok := payments[key]; !ok {
			t.Fatalf("recurring payments = %+v, want %+v", payments, want)
		}
	}
}
