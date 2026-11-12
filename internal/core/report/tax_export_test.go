package report

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func exporterItem(
	t *testing.T,
	id int64,
	minor int64,
	day civil.Date,
	categoryID int64,
	categoryName string,
	partner *string,
	purpose *string,
	documents int,
) model.TransactionItem {
	t.Helper()
	builder := item(id, minor).Date(day).Category(categoryID, categoryName, "#9C27B0").TaxRelevant()
	if partner != nil {
		builder = builder.Partner(*partner)
	}
	if purpose != nil {
		builder = builder.Purpose(*purpose)
	}
	return builder.Documents(documents).Build()
}

func taxGroup(id int64, name string, bookings []model.TransactionItem, income bool) TaxGroup {
	var incomeMinor int64
	if income {
		for _, booking := range bookings {
			incomeMinor += booking.Amount.Minor
		}
	}
	var expensesMinor int64
	if !income {
		for _, booking := range bookings {
			expensesMinor += absClamped(booking.Amount.Minor)
		}
	}
	categoryID := model.CategoryID(id)
	return TaxGroup{
		CategoryID: &categoryID,
		Name:       name,
		Color:      "#9C27B0",
		Bookings:   bookings,
		Income:     money.Money{Minor: incomeMinor, Currency: money.EUR},
		Expenses:   money.Money{Minor: expensesMinor, Currency: money.EUR},
	}
}

func exporterBookings(
	t *testing.T,
	incomeCount int64,
	deductionCount int64,
	longDescription bool,
	unencodablePartner bool,
	otherCurrency int,
) TaxBookings {
	t.Helper()
	day := testDate(t, 2026, time.June, 1)
	salary := make([]model.TransactionItem, 0, incomeCount)
	for id := int64(1); id <= incomeCount; id++ {
		purpose := fmt.Sprintf("Gehalt %d", id)
		salary = append(salary, exporterItem(
			t, id, 120_000, day.AddDays(-int(incomeCount-id)),
			10, "Gehalt", nil, &purpose, 0,
		))
	}
	royalties := make([]model.TransactionItem, 0, incomeCount)
	for id := int64(1); id <= incomeCount; id++ {
		purpose := fmt.Sprintf("Auszahlung %d", id)
		royalties = append(royalties, exporterItem(
			t, 100+id, 4_550, day.AddDays(-int(id)),
			11, "Tantiemen", nil, &purpose, 0,
		))
	}
	donations := make([]model.TransactionItem, 0, deductionCount)
	for id := int64(1); id <= deductionCount; id++ {
		partner := "Beispielhilfe e. V."
		if unencodablePartner && id == 1 {
			partner = "Beispielhilfe ✓ e. V."
		}
		purpose := fmt.Sprintf("Spende %d", id)
		documents := 0
		if id == 1 {
			documents = 1
		}
		donations = append(donations, exporterItem(
			t, 200+id, -3_000, day.AddDays(-int(id)),
			20, "Spenden", &partner, &purpose, documents,
		))
	}
	craft := make([]model.TransactionItem, 0, deductionCount)
	for id := int64(1); id <= deductionCount; id++ {
		partner := "Beispiel Werk GmbH"
		if longDescription {
			partner = "Beispielhaft lang benannte Handwerksfirma mit sehr ausführlichem Namen und Ort"
		}
		purpose := fmt.Sprintf("Renovierung %d", id)
		craft = append(craft, exporterItem(
			t, 300+id, -8_520, day.AddDays(-int(deductionCount-id)),
			21, "Handwerk", &partner, &purpose, 0,
		))
	}
	bookingCount := len(salary) + len(royalties) + len(donations) + len(craft)

	return TaxBookings{
		From:     testDate(t, 2026, time.January, 1),
		To:       testDate(t, 2026, time.December, 31),
		Currency: money.EUR,
		IncomeGroups: []TaxGroup{
			taxGroup(10, "Gehalt", salary, true),
			taxGroup(11, "Tantiemen", royalties, true),
		},
		DeductionGroups: []TaxGroup{
			taxGroup(20, "Spenden", donations, false),
			taxGroup(21, "Handwerk", craft, false),
		},
		BookingCount:          bookingCount,
		OtherCurrencyBookings: otherCurrency,
	}
}

func paypalExportBookings(t *testing.T) TaxBookings {
	t.Helper()
	paypal := exporterItem(
		t, 1, -1_999, testDate(t, 2026, time.June, 1),
		20, "Spenden",
		strPtr("PayPal Europe S.a.r.l. et Cie S.C.A"),
		strPtr("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		0,
	)
	plain := exporterItem(
		t, 2, -450, testDate(t, 2026, time.June, 1),
		20, "Spenden",
		strPtr("Beispiel Bäckerei"),
		strPtr("Brötchen"),
		0,
	)
	return TaxBookings{
		From:     testDate(t, 2026, time.January, 1),
		To:       testDate(t, 2026, time.December, 31),
		Currency: money.EUR,
		DeductionGroups: []TaxGroup{
			taxGroup(20, "Spenden", []model.TransactionItem{paypal, plain}, false),
		},
		BookingCount: 2,
	}
}

func TestExcelHasOneOverviewRowPerGroupPlusTotalsAndOneBookingsRowPerBooking(t *testing.T) {
	info := exporterBookings(t, 2, 3, false, false, 2)
	file := filepath.Join(t.TempDir(), "tax.xlsx")
	if err := (TaxReportExporter{}).ExportXLSX(&info, file, i18n.En); err != nil {
		t.Fatalf("ExportXLSX: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the workbook: %v", err)
	}
	rows := firstSheetRows(t, content)
	if rows[0][0] != "Tax-relevant bookings 2026" {
		t.Fatalf("rows[0][0] = %q", rows[0][0])
	}
	if rows[1][0] != "Currency: EUR" {
		t.Fatalf("rows[1][0] = %q", rows[1][0])
	}
	if !slices.Equal(rows[2], []string{
		"Section", "Category", "Bookings", "Income", "Expenses", "Net", "With receipt",
	}) {
		t.Fatalf("rows[2] = %v", rows[2])
	}
	if !slices.Equal(rows[3], []string{"Income", "Gehalt", "2", "2400", "0", "2400", "0"}) {
		t.Fatalf("rows[3] = %v", rows[3])
	}
	if !slices.Equal(rows[4], []string{"Income", "Tantiemen", "2", "91", "0", "91", "0"}) {
		t.Fatalf("rows[4] = %v", rows[4])
	}
	if !slices.Equal(rows[5], []string{"Deductions", "Spenden", "3", "0", "90", "-90", "1"}) {
		t.Fatalf("rows[5] = %v", rows[5])
	}
	if !slices.Equal(rows[6], []string{"Deductions", "Handwerk", "3", "0", "255.6", "-255.6", "0"}) {
		t.Fatalf("rows[6] = %v", rows[6])
	}
	if rows[7][1] != "Tax-relevant income" {
		t.Fatalf("rows[7][1] = %q", rows[7][1])
	}
	if rows[7][3] != "2491" {
		t.Fatalf("rows[7][3] = %q, want 2491", rows[7][3])
	}
	if rows[8][1] != "Deductions" {
		t.Fatalf("rows[8][1] = %q", rows[8][1])
	}
	if rows[8][4] != "345.6" {
		t.Fatalf("rows[8][4] = %q, want 345.6", rows[8][4])
	}
	if rows[9][1] != "Net income" {
		t.Fatalf("rows[9][1] = %q", rows[9][1])
	}
	if rows[9][5] != "2145.4" {
		t.Fatalf("rows[9][5] = %q, want 2145.4", rows[9][5])
	}
	if rows[10][1] != "Receipts" {
		t.Fatalf("rows[10][1] = %q", rows[10][1])
	}
	if rows[10][2] != "1 of 10" {
		t.Fatalf("rows[10][2] = %q, want 1 of 10", rows[10][2])
	}
	if !strings.HasPrefix(rows[11][0], "All amounts in EUR") {
		t.Fatalf("the other-currency hint: %v", rows[11])
	}

	sheet := parseXML(t, zipEntry(t, content, "xl/worksheets/sheet2.xml"))
	sheetRows := xmlDescendants(sheet, "row")
	if len(sheetRows) != 1+info.BookingCount {
		t.Fatalf("booking rows = %d, want %d", len(sheetRows), 1+info.BookingCount)
	}
	header := make([]string, 0, 11)
	for column := 'A'; column <= 'K'; column++ {
		header = append(header, cellText(t, sheet, string(column)+"1"))
	}
	if !slices.Equal(header, []string{
		"Section", "Category", "Date", "Partner", "Shop", "Purpose", "Account", "Amount", "Currency", "Receipt",
		"Source",
	}) {
		t.Fatalf("header = %v; the Shop column sits directly after the Partner", header)
	}
	firstDonation := 2 + 4
	if got := cellText(t, sheet, fmt.Sprintf("A%d", firstDonation)); got != "Deductions" {
		t.Fatalf("A%d = %q", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("B%d", firstDonation)); got != "Spenden" {
		t.Fatalf("B%d = %q", firstDonation, got)
	}
	if got := dateCell(t, sheet, fmt.Sprintf("C%d", firstDonation)); got != "2026-05-31" {
		t.Fatalf("C%d = %q, want 2026-05-31; the date is a real date cell", firstDonation, got)
	}
	if got := cellStyle(t, sheet, fmt.Sprintf("C%d", firstDonation)); got != "3" {
		t.Fatalf("C%d style = %q, want 3; the date format", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("D%d", firstDonation)); got != "Beispielhilfe e. V." {
		t.Fatalf("D%d = %q", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("F%d", firstDonation)); got != "Spende 1" {
		t.Fatalf("F%d = %q; the raw purpose keeps its column", firstDonation, got)
	}
	if got := cellValue(t, sheet, fmt.Sprintf("H%d", firstDonation)); got != "-30.00" {
		t.Fatalf("H%d = %q, want -30.00; signed amount as a number", firstDonation, got)
	}
	if got := cellStyle(t, sheet, fmt.Sprintf("H%d", firstDonation)); got != "2" {
		t.Fatalf("H%d style = %q, want 2; the money format", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("I%d", firstDonation)); got != "EUR" {
		t.Fatalf("I%d = %q", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("J%d", firstDonation)); got != "Yes" {
		t.Fatalf("J%d = %q; the first donation carries a receipt", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("G%d", firstDonation)); got != "DE89370400440532013000" {
		t.Fatalf("G%d = %q; the full IBAN as text", firstDonation, got)
	}
	if got := cellText(t, sheet, fmt.Sprintf("K%d", firstDonation)); !strings.Contains(got, "from category") {
		t.Fatalf("K%d = %q; the source names the deciding layer", firstDonation, got)
	}
	last := len(sheetRows)
	if got := cellText(t, sheet, fmt.Sprintf("A%d", last)); got != "Deductions" {
		t.Fatalf("A%d = %q; the deductions come after the income", last, got)
	}
	filter := xmlFind(t, sheet, "autoFilter")
	expected := fmt.Sprintf("A1:K%d", last)
	if got := xmlAttribute(filter, "ref"); got != expected {
		t.Fatalf("autoFilter ref = %q, want %q; the filter spans header and data", got, expected)
	}
	pane := xmlFind(t, sheet, "pane")
	if got := xmlAttribute(pane, "state"); got != "frozen" {
		t.Fatalf("pane state = %q, want frozen", got)
	}
}

func TestExcelShowsTheShopAfterThePartnerAndKeepsTheRawPartner(t *testing.T) {
	info := paypalExportBookings(t)
	file := filepath.Join(t.TempDir(), "tax-shop.xlsx")
	if err := (TaxReportExporter{}).ExportXLSX(&info, file, i18n.En); err != nil {
		t.Fatalf("ExportXLSX: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the workbook: %v", err)
	}
	sheet := parseXML(t, zipEntry(t, content, "xl/worksheets/sheet2.xml"))
	if got := cellText(t, sheet, "D2"); got != "PayPal Europe S.a.r.l. et Cie S.C.A" {
		t.Fatalf("D2 = %q; the raw partner", got)
	}
	if got := cellText(t, sheet, "E2"); got != "Beispiel Shop GmbH" {
		t.Fatalf("E2 = %q; the shop follows the partner", got)
	}
	if got := cellText(t, sheet, "E3"); got != "" {
		t.Fatalf("E3 = %q; a non-PayPal booking has an empty shop", got)
	}
	filter := xmlFind(t, sheet, "autoFilter")
	if got := xmlAttribute(filter, "ref"); got != "A1:K3" {
		t.Fatalf("autoFilter ref = %q, want A1:K3", got)
	}
}

func TestPDFTitlesAPaypalBookingWithTheProviderAndTheShop(t *testing.T) {
	info := paypalExportBookings(t)
	content := (TaxReportExporter{}).BuildPDF(&info, testDate(t, 2026, time.November, 7), i18n.En)
	assertPDF(t, content)
	if !pdfContains(content, "PayPal · Beispiel Shop GmbH") {
		t.Fatal("the WinAnsi periodcentered does not survive")
	}
}

func TestExcelSheetNamesAreLocalizedInGerman(t *testing.T) {
	info := exporterBookings(t, 1, 1, false, false, 0)
	file := filepath.Join(t.TempDir(), "tax-de.xlsx")
	if err := (TaxReportExporter{}).ExportXLSX(&info, file, i18n.De); err != nil {
		t.Fatalf("ExportXLSX: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the workbook: %v", err)
	}
	workbook := parseXML(t, zipEntry(t, content, "xl/workbook.xml"))
	names := make([]string, 0)
	for _, sheet := range xmlDescendants(workbook, "sheet") {
		names = append(names, xmlAttribute(sheet, "name"))
	}
	if !slices.Equal(names, []string{"Übersicht", "Buchungen"}) {
		t.Fatalf("sheet names = %v, want [Übersicht Buchungen]", names)
	}
	if got := firstSheetRows(t, content)[0][0]; got != "Steuerrelevante Buchungen 2026" {
		t.Fatalf("overview title = %q", got)
	}
}

func TestPDFPaginatesWithFootersSectionsAndFittedDescriptions(t *testing.T) {
	info := exporterBookings(t, 40, 55, true, true, 0)
	content := (TaxReportExporter{}).BuildPDF(&info, testDate(t, 2026, time.November, 7), i18n.En)
	pages := pdfPageCount(content)
	if pages < 3 {
		t.Fatalf("pages = %d, want at least 3; 190 bookings fill several pages", pages)
	}

	if !pdfContains(content, "Tax-relevant bookings 2026") {
		t.Fatal("the title is missing")
	}
	if !pdfContains(content, "Income") {
		t.Fatal("the income section is missing")
	}
	if !pdfContains(content, "Deductions") {
		t.Fatal("the deductions section is missing")
	}
	for _, group := range info.IncomeGroups {
		if !pdfContains(content, group.Name) {
			t.Fatalf("every group header: %s", group.Name)
		}
	}
	for _, group := range info.DeductionGroups {
		if !pdfContains(content, group.Name) {
			t.Fatalf("every group header: %s", group.Name)
		}
	}
	for page := 1; page <= pages; page++ {
		footer := fmt.Sprintf("Page %d of %d", page, pages)
		if !pdfContains(content, footer) {
			t.Fatalf("the footer of page %d is missing", page)
		}
	}
	if !pdfContains(content, "…") {
		t.Fatal("the long description is not cut with an ellipsis")
	}
	if !pdfContains(content, "Beispielhilfe ? e. V.") {
		t.Fatal("characters outside WinAnsi become ? without throwing")
	}
}

func TestExportPDFWritesALoadableFileWithTheSamePageCount(t *testing.T) {
	info := exporterBookings(t, 30, 30, false, false, 0)
	pages := pdfPageCount((TaxReportExporter{}).BuildPDF(&info, testDate(t, 2026, time.November, 7), i18n.En))
	file := filepath.Join(t.TempDir(), "tax.pdf")
	if err := (TaxReportExporter{}).ExportPDF(&info, file, testDate(t, 2026, time.November, 7), i18n.En); err != nil {
		t.Fatalf("ExportPDF: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the PDF: %v", err)
	}
	assertPDF(t, content)
	if got := pdfPageCount(content); got != pages {
		t.Fatalf("page count = %d, want %d", got, pages)
	}
}
