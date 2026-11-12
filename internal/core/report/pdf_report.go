// The tax report PDF, ported from FinDesk's document/PdfReportExporter.kt, via
// crates/finglass-core/src/report/pdf_report.rs.
package report

import (
	"fmt"
	"os"
	"sort"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/export/pdf"
	"github.com/MaMissaoui/Finglass/internal/core/export/xlsx"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// PdfReportExporter renders a TaxReport as a simple multi-page A4 PDF.
type PdfReportExporter struct{}

// BuildPDF builds the PDF in memory.
func (PdfReportExporter) BuildPDF(report *TaxReport, today civil.Date, lang i18n.Language) []byte {
	writer := pdf.NewBuilder()
	var title string
	if report.IsCalendarYear() {
		title = i18n.Format(lang, i18n.ReportTitleTax, i18n.Int(int64(report.Year)))
	} else {
		title = i18n.Format(lang, i18n.ReportTitleRange, i18n.Str(report.Title(lang)))
	}
	writer.Text(title, pdf.Bold, 18.0)
	writer.Text(
		i18n.Format(lang, i18n.PdfGenerated, i18n.Str(formatDate(today))),
		pdf.Regular,
		9.0,
	)
	writer.Gap(12.0)

	writer.Heading(i18n.Text(lang, i18n.PdfSummary))
	writer.Row(
		i18n.Text(lang, i18n.PdfTotalIncome),
		Money(report.TotalIncome, lang, false),
		pdf.Regular,
		10.0,
	)
	writer.Row(
		i18n.Text(lang, i18n.PdfTotalExpenses),
		Money(report.TotalExpenses, lang, false),
		pdf.Regular,
		10.0,
	)
	writer.Row(
		i18n.Text(lang, i18n.PdfNetIncome),
		Money(report.NetIncome, lang, false),
		pdf.Regular,
		10.0,
	)
	writer.Row(
		i18n.Text(lang, i18n.PdfTaxRelevantExpenses),
		Money(report.TaxRelevantTotal(), lang, false),
		pdf.Regular,
		10.0,
	)
	writer.Row(
		i18n.Text(lang, i18n.PdfTransactionCount),
		formatInt(report.TransactionCount),
		pdf.Regular,
		10.0,
	)
	if report.OtherCurrencyBookings > 0 {
		writer.Text(
			i18n.Format(
				lang,
				i18n.PdfAllAmountsIn,
				i18n.Str(report.Currency.String()),
				i18n.Int(int64(report.OtherCurrencyBookings)),
			),
			pdf.Regular,
			9.0,
		)
	}
	writer.Gap(10.0)

	writer.Heading(i18n.Text(lang, i18n.PdfTaxByCategory))
	if len(report.TaxRelevantExpenses) == 0 {
		writer.Text(i18n.Text(lang, i18n.PdfNoTaxRelevant), pdf.Regular, 10.0)
	}
	for _, entry := range report.TaxRelevantExpenses {
		writer.Row(entry.Name, Money(entry.Amount, lang, false), pdf.Regular, 10.0)
	}
	writer.Gap(10.0)

	writer.Heading(i18n.Text(lang, i18n.PdfMonthlyBreakdown))
	writer.Row(
		i18n.Text(lang, i18n.PdfMonth),
		i18n.Text(lang, i18n.PdfIncomeExpensesNet),
		pdf.Bold,
		10.0,
	)
	for _, month := range report.MonthlyBreakdown {
		var name string
		if report.IsCalendarYear() {
			name = MonthName(lang, month.Month)
		} else {
			name = fmt.Sprintf("%s %d", MonthName(lang, month.Month), month.Year)
		}
		writer.Row(
			name,
			fmt.Sprintf(
				"%s / %s / %s",
				Money(month.Income, lang, false),
				Money(month.Expenses, lang, false),
				Money(month.Net(), lang, false),
			),
			pdf.Regular,
			10.0,
		)
	}
	writer.Gap(10.0)

	writer.Heading(i18n.Text(lang, i18n.PdfExpensesByCategory))
	for _, entry := range report.ExpensesByCategory {
		writer.Row(entry.Name, Money(entry.Amount, lang, false), pdf.Regular, 10.0)
	}

	if len(report.TaxRelevantTransactions) != 0 {
		writer.Gap(10.0)
		writer.Heading(i18n.Text(lang, i18n.PdfTaxTransactions))
		sorted := make([]*model.TransactionItem, 0, len(report.TaxRelevantTransactions))
		for index := range report.TaxRelevantTransactions {
			sorted = append(sorted, &report.TaxRelevantTransactions[index])
		}
		sort.SliceStable(sorted, func(left, right int) bool {
			return sorted[left].Date.Before(sorted[right].Date)
		})
		for _, transaction := range sorted {
			description := truncateRunes(DisplayDescription(transaction), 55)
			writer.Row(
				fmt.Sprintf(
					"%s  %s  (%s)",
					formatDate(transaction.Date),
					description,
					optionalText(transaction.CategoryName),
				),
				Money(
					money.Money{
						Minor:    absClamped(transaction.Amount.Minor),
						Currency: transaction.Currency,
					},
					lang,
					false,
				),
				pdf.Regular,
				10.0,
			)
		}
	}

	return writer.Finish()
}

// Export writes the report to a file through a temp file next to the target.
func (PdfReportExporter) Export(
	report *TaxReport,
	file string,
	today civil.Date,
	lang i18n.Language,
) error {
	bytes := (PdfReportExporter{}).BuildPDF(report, today, lang)
	return xlsx.WriteAtomically(file, func(temp string) error {
		// #nosec G304 -- the temp path comes from WriteAtomically next to the
		// caller's export target.
		return os.WriteFile(temp, bytes, 0o600)
	})
}

// truncateRunes keeps at most limit characters, like Rust's chars().take(n).
func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}
