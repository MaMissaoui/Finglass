package report

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func TestPDFExportWritesAReadableReport(t *testing.T) {
	d := reportFixture(t)
	report, err := GenerateTaxReport(context.Background(), d, 2026, i18n.En)
	if err != nil {
		t.Fatalf("GenerateTaxReport: %v", err)
	}
	file := filepath.Join(t.TempDir(), "report.pdf")
	if err := (PdfReportExporter{}).Export(&report, file, testDate(t, 2026, time.November, 7), i18n.En); err != nil {
		t.Fatalf("Export: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the PDF: %v", err)
	}
	assertPDF(t, content)
	if !pdfContains(content, "Finglass Tax Report 2026") {
		t.Fatal("the PDF has no title")
	}
	if !pdfContains(content, "Arbeitsmittel") {
		t.Fatal("the PDF has no Arbeitsmittel category")
	}
	if !pdfContains(content, "€3,000.00") {
		t.Fatal("the PDF has no euro amount")
	}
}

func TestGermanPDFExportUsesTheGermanMoneyConvention(t *testing.T) {
	d := reportFixture(t)
	report, err := GenerateTaxReport(context.Background(), d, 2026, i18n.De)
	if err != nil {
		t.Fatalf("GenerateTaxReport: %v", err)
	}
	content := (PdfReportExporter{}).BuildPDF(&report, testDate(t, 2026, time.November, 7), i18n.De)

	assertPDF(t, content)
	if !pdfContains(content, "Arbeitsmittel") {
		t.Fatal("the PDF has no Arbeitsmittel category")
	}
	if !pdfContains(content, "3.000,00\u00a0€") {
		t.Fatal("the PDF font does not encode the no-break space and the euro glyph")
	}
}

func TestPDFTitlesATaxRelevantPaypalBookingWithTheShop(t *testing.T) {
	d := testDatabase(t)
	account := addAccount(t, d, money.EUR)
	online := addCategory(t, d, "Online shopping", "#FF5722", true, false)
	addTransaction(
		t, d, account, testDate(t, 2026, time.June, 15), eur(-25_000),
		strPtr("PayPal Europe S.a.r.l. et Cie S.C.A"),
		strPtr("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
		&online,
	)
	report, err := GenerateTaxReport(context.Background(), d, 2026, i18n.En)
	if err != nil {
		t.Fatalf("GenerateTaxReport: %v", err)
	}
	file := filepath.Join(t.TempDir(), "tax-title.pdf")
	if err := (PdfReportExporter{}).Export(&report, file, testDate(t, 2026, time.November, 7), i18n.En); err != nil {
		t.Fatalf("Export: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the PDF: %v", err)
	}
	assertPDF(t, content)
	if !pdfContains(content, "PayPal · Beispiel Shop GmbH") {
		t.Fatal("the tax booking row does not use the display title")
	}
}

func TestPDFReportContainsTheTitleAndSanitizedText(t *testing.T) {
	transactions := itemsOf([]itemBuilder{
		item(1, -3_000).
			Category(20, "Spenden", "").
			TaxRelevant().
			Partner("Beispielhilfe ✓ e. V.").
			Purpose("Spende"),
	})
	report := BuildReport(
		testDate(t, 2026, time.January, 1),
		testDate(t, 2026, time.December, 31),
		transactions,
		money.EUR,
		i18n.En,
	)
	content := (PdfReportExporter{}).BuildPDF(&report, testDate(t, 2026, time.November, 7), i18n.En)
	assertPDF(t, content)
	if !pdfContains(content, "Finglass Tax Report 2026") {
		t.Fatal("the PDF has no title")
	}
	if !pdfContains(content, "Beispielhilfe ? e. V.") {
		t.Fatal("the unencodable character is not sanitized")
	}
}
