// The Tax bookings exports: an Excel workbook and a printable PDF.
//
// Ported from FinDesk's document/TaxReportExporter.kt, via
// crates/finglass-core/src/report/tax_export.rs. All strings come from i18n,
// every amount is in the report's currency, and the PDF uses the same layout as
// the PDFBox version.
package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/export/pdf"
	"github.com/MaMissaoui/Finglass/internal/core/export/xlsx"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

const (
	groupHeaderHeight float32 = 15.0
	tableHeaderHeight float32 = 12.0
	rowHeight         float32 = 14.0

	xDate    float32 = 50.0
	wDate    float32 = 56.0
	xDesc    float32 = 112.0
	wDesc    float32 = 232.0
	xAccount float32 = 350.0
	wAccount float32 = 74.0

	amountRightEdge  float32 = 500.0
	wAmount          float32 = 76.0
	receiptRightEdge float32 = pdf.PageWidth - pdf.Margin
	wReceipt         float32 = 40.0
)

// TaxReportExporter exports the Tax bookings view as an Excel workbook or an
// A4 PDF.
type TaxReportExporter struct{}

// ExportXLSX writes the workbook: an overview sheet and a bookings sheet.
func (TaxReportExporter) ExportXLSX(bookings *TaxBookings, file string, lang i18n.Language) error {
	writer := xlsx.NewWriter()
	overviewSheet(writer, bookings, lang)
	bookingsSheet(writer, bookings, lang)
	return writer.Write(file)
}

// BuildPDF builds the printable report in memory.
func (TaxReportExporter) BuildPDF(bookings *TaxBookings, today civil.Date, lang i18n.Language) []byte {
	layout := pdf.NewBuilder()
	layout.Text(
		i18n.Format(lang, i18n.TaxExportTitle, i18n.Str(bookings.Title(lang))),
		pdf.Bold,
		18.0,
	)
	layout.Text(
		fmt.Sprintf(
			"%s: %s",
			i18n.Text(lang, i18n.ColCurrency),
			bookings.Currency.String(),
		),
		pdf.Regular,
		10.0,
	)
	layout.Text(
		i18n.Format(lang, i18n.PdfGenerated, i18n.Str(formatDate(today))),
		pdf.Regular,
		9.0,
	)
	layout.Gap(12.0)
	layout.Row(
		i18n.Text(lang, i18n.TaxIncomeTotal),
		PDFAmount(bookings.IncomeTotal(), lang, false),
		pdf.Regular,
		10.0,
	)
	layout.Row(
		i18n.Text(lang, i18n.TaxDeductionTotal),
		PDFAmount(bookings.DeductionTotal(), lang, false),
		pdf.Regular,
		10.0,
	)
	layout.Row(
		i18n.Text(lang, i18n.NetIncome),
		PDFAmount(bookings.NetTotal(), lang, false),
		pdf.Regular,
		10.0,
	)
	layout.Row(
		i18n.Text(lang, i18n.TaxReceipts),
		i18n.Format(
			lang,
			i18n.TaxReceiptsOf,
			i18n.Int(int64(bookings.WithReceipt())),
			i18n.Int(int64(bookings.BookingCount)),
		),
		pdf.Regular,
		10.0,
	)
	if bookings.OtherCurrencyBookings > 0 {
		layout.Text(
			i18n.Format(
				lang,
				i18n.OtherCurrenciesExcluded,
				i18n.Str(bookings.Currency.String()),
				i18n.Int(int64(bookings.OtherCurrencyBookings)),
			),
			pdf.Regular,
			9.0,
		)
	}
	layout.Gap(10.0)
	section(
		layout,
		i18n.Text(lang, i18n.TaxSectionIncome),
		bookings.IncomeGroups,
		lang,
	)
	section(
		layout,
		i18n.Text(lang, i18n.TaxSectionDeductions),
		bookings.DeductionGroups,
		lang,
	)

	pageCount := layout.PageCount()
	for index := 0; index < pageCount; index++ {
		layout.AddFooter(
			index,
			i18n.Format(
				lang,
				i18n.PdfPageOf,
				i18n.Int(int64(index+1)),
				i18n.Int(int64(pageCount)),
			),
		)
	}

	return layout.Finish()
}

// ExportPDF saves the report as a PDF file, through a temp file next to the
// target.
func (TaxReportExporter) ExportPDF(
	bookings *TaxBookings,
	file string,
	today civil.Date,
	lang i18n.Language,
) error {
	bytes := (TaxReportExporter{}).BuildPDF(bookings, today, lang)
	return xlsx.WriteAtomically(file, func(temp string) error {
		// #nosec G304 -- the temp path comes from WriteAtomically next to the
		// caller's export target.
		return os.WriteFile(temp, bytes, 0o600)
	})
}

func overviewSheet(writer *xlsx.Writer, bookings *TaxBookings, lang i18n.Language) {
	writer.Sheet(
		i18n.Text(lang, i18n.XlsxSheetOverview),
		[]float64{14.0, 34.0, 10.0, 14.0, 14.0, 14.0, 12.0},
		false,
		false,
		func(sheet *xlsx.Sheet) {
			sheet.Row(xlsx.Bold(i18n.Format(
				lang,
				i18n.TaxExportTitle,
				i18n.Str(bookings.Title(lang)),
			)))
			sheet.Row(xlsx.Text(fmt.Sprintf(
				"%s: %s",
				i18n.Text(lang, i18n.ColCurrency),
				bookings.Currency.String(),
			)))
			sheet.Row(
				xlsx.Bold(i18n.Text(lang, i18n.TaxColSection)),
				xlsx.Bold(i18n.Text(lang, i18n.ColCategory)),
				xlsx.Bold(i18n.Text(lang, i18n.TaxColBookings)),
				xlsx.Bold(i18n.Text(lang, i18n.ColIncome)),
				xlsx.Bold(i18n.Text(lang, i18n.ColExpenses)),
				xlsx.Bold(i18n.Text(lang, i18n.ColNet)),
				xlsx.Bold(i18n.Text(lang, i18n.TaxColWithReceipt)),
			)
			for index := range bookings.IncomeGroups {
				groupRow(sheet, lang, i18n.TaxSectionIncome, &bookings.IncomeGroups[index])
			}
			for index := range bookings.DeductionGroups {
				groupRow(sheet, lang, i18n.TaxSectionDeductions, &bookings.DeductionGroups[index])
			}
			sheet.Row(
				xlsx.Empty(),
				xlsx.Bold(i18n.Text(lang, i18n.TaxIncomeTotal)),
				xlsx.Empty(),
				xlsx.Money(bookings.IncomeTotal()),
			)
			sheet.Row(
				xlsx.Empty(),
				xlsx.Bold(i18n.Text(lang, i18n.TaxDeductionTotal)),
				xlsx.Empty(),
				xlsx.Empty(),
				xlsx.Money(bookings.DeductionTotal()),
			)
			sheet.Row(
				xlsx.Empty(),
				xlsx.Bold(i18n.Text(lang, i18n.NetIncome)),
				xlsx.Empty(),
				xlsx.Empty(),
				xlsx.Empty(),
				xlsx.Money(bookings.NetTotal()),
			)
			sheet.Row(
				xlsx.Empty(),
				xlsx.Bold(i18n.Text(lang, i18n.TaxReceipts)),
				xlsx.Text(i18n.Format(
					lang,
					i18n.TaxReceiptsOf,
					i18n.Int(int64(bookings.WithReceipt())),
					i18n.Int(int64(bookings.BookingCount)),
				)),
			)
			if bookings.OtherCurrencyBookings > 0 {
				sheet.Row(xlsx.Text(i18n.Format(
					lang,
					i18n.OtherCurrenciesExcluded,
					i18n.Str(bookings.Currency.String()),
					i18n.Int(int64(bookings.OtherCurrencyBookings)),
				)))
			}
		},
	)
}

func groupRow(sheet *xlsx.Sheet, lang i18n.Language, section i18n.Key, group *TaxGroup) {
	sheet.Row(
		xlsx.Text(i18n.Text(lang, section)),
		xlsx.Text(group.Name),
		xlsx.NumberInt(int64(len(group.Bookings))),
		xlsx.Money(group.Income),
		xlsx.Money(group.Expenses),
		xlsx.Money(group.Net()),
		xlsx.NumberInt(int64(group.WithReceipt())),
	)
}

func bookingsSheet(writer *xlsx.Writer, bookings *TaxBookings, lang i18n.Language) {
	writer.Sheet(
		i18n.Text(lang, i18n.XlsxSheetBookings),
		[]float64{13.0, 24.0, 12.0, 30.0, 18.0, 34.0, 26.0, 12.0, 10.0, 10.0, 44.0},
		true,
		true,
		func(sheet *xlsx.Sheet) {
			sheet.Row(
				xlsx.Bold(i18n.Text(lang, i18n.TaxColSection)),
				xlsx.Bold(i18n.Text(lang, i18n.ColCategory)),
				xlsx.Bold(i18n.Text(lang, i18n.ColDate)),
				xlsx.Bold(i18n.Text(lang, i18n.ColPartner)),
				xlsx.Bold(i18n.Text(lang, i18n.ColShop)),
				xlsx.Bold(i18n.Text(lang, i18n.ColPurpose)),
				xlsx.Bold(i18n.Text(lang, i18n.ColAccount)),
				xlsx.Bold(i18n.Text(lang, i18n.ColAmount)),
				xlsx.Bold(i18n.Text(lang, i18n.ColCurrency)),
				xlsx.Bold(i18n.Text(lang, i18n.TaxColReceipt)),
				xlsx.Bold(i18n.Text(lang, i18n.TaxColSource)),
			)
			for groupIndex := range bookings.IncomeGroups {
				group := &bookings.IncomeGroups[groupIndex]
				for bookingIndex := range group.Bookings {
					bookingRow(sheet, lang, i18n.TaxSectionIncome, group, &group.Bookings[bookingIndex])
				}
			}
			for groupIndex := range bookings.DeductionGroups {
				group := &bookings.DeductionGroups[groupIndex]
				for bookingIndex := range group.Bookings {
					bookingRow(sheet, lang, i18n.TaxSectionDeductions, group, &group.Bookings[bookingIndex])
				}
			}
		},
	)
}

func bookingRow(
	sheet *xlsx.Sheet,
	lang i18n.Language,
	section i18n.Key,
	group *TaxGroup,
	booking *model.TransactionItem,
) {
	receipt := i18n.Text(lang, i18n.No)
	if booking.DocumentCount > 0 {
		receipt = i18n.Text(lang, i18n.Yes)
	}
	sheet.Row(
		xlsx.Text(i18n.Text(lang, section)),
		xlsx.Text(group.Name),
		xlsx.Date(booking.Date),
		xlsx.Text(optionalText(booking.PartnerName)),
		xlsx.Text(optionalText(rules.MerchantOf(booking.Purpose, booking.PartnerName))),
		xlsx.Text(optionalText(booking.Purpose)),
		xlsx.Text(optionalText(booking.AccountIBAN)),
		xlsx.Money(booking.Amount),
		xlsx.Text(booking.Currency.String()),
		xlsx.Text(receipt),
		xlsx.Text(sourceLine(lang, booking)),
	)
}

func sourceLine(lang i18n.Language, booking *model.TransactionItem) string {
	base := i18n.Text(lang, i18n.TaxNotRelevantChoice)
	if booking.IsTaxRelevant {
		base = i18n.Text(lang, i18n.TaxRelevantChoice)
	}
	switch booking.TaxSource {
	case model.TaxSourceManual:
		return i18n.Format(lang, i18n.TaxSetByYou, i18n.Str(base))
	case model.TaxSourceRule:
		return i18n.Format(
			lang,
			i18n.TaxFromRule,
			i18n.Str(base),
			i18n.Str(optionalText(booking.TaxRulePattern)),
		)
	case model.TaxSourceCategory:
		category := optionalText(booking.CategoryName)
		if booking.CategoryName == nil {
			category = i18n.Text(lang, i18n.Uncategorized)
		}
		return i18n.Format(
			lang,
			i18n.TaxFromCategory,
			i18n.Str(base),
			i18n.Str(category),
		)
	default:
		return base
	}
}

func section(layout *pdf.Builder, title string, groups []TaxGroup, lang i18n.Language) {
	if len(groups) == 0 {
		return
	}
	layout.Text(title, pdf.Bold, 12.0)
	layout.Gap(2.0)
	for index := range groups {
		group := &groups[index]
		layout.EnsureSpace(groupHeaderHeight + tableHeaderHeight + rowHeight)
		layout.Advance(groupHeaderHeight)
		layout.Draw(
			fmt.Sprintf(
				"%s · %d · %s",
				group.Name,
				len(group.Bookings),
				PDFAmount(group.Net(), lang, true),
			),
			pdf.Bold,
			11.0,
			pdf.Margin,
		)
		tableHeader(layout, lang)
		for bookingIndex := range group.Bookings {
			tableRow(layout, &group.Bookings[bookingIndex], lang)
		}
		layout.Gap(6.0)
	}
}

func tableHeader(layout *pdf.Builder, lang i18n.Language) {
	layout.EnsureSpace(tableHeaderHeight + rowHeight)
	layout.Advance(tableHeaderHeight)
	layout.DrawFitted(i18n.Text(lang, i18n.ColDate), pdf.Bold, 8.0, xDate, wDate)
	layout.DrawFitted(
		i18n.Text(lang, i18n.ColDescription),
		pdf.Bold,
		8.0,
		xDesc,
		wDesc,
	)
	layout.DrawFitted(
		i18n.Text(lang, i18n.ColAccount),
		pdf.Bold,
		8.0,
		xAccount,
		wAccount,
	)
	layout.DrawRightFitted(
		i18n.Text(lang, i18n.ColAmount),
		pdf.Bold,
		8.0,
		amountRightEdge,
		wAmount,
	)
	layout.DrawRightFitted(
		i18n.Text(lang, i18n.TaxColReceipt),
		pdf.Bold,
		8.0,
		receiptRightEdge,
		wReceipt,
	)
}

func tableRow(layout *pdf.Builder, booking *model.TransactionItem, lang i18n.Language) {
	layout.EnsureSpace(rowHeight)
	layout.Advance(rowHeight)
	layout.DrawFitted(formatDate(booking.Date), pdf.Regular, 9.0, xDate, wDate)
	layout.DrawFitted(
		DisplayDescription(booking),
		pdf.Regular,
		9.0,
		xDesc,
		wDesc,
	)
	layout.DrawFitted(shortAccount(booking), pdf.Regular, 9.0, xAccount, wAccount)
	layout.DrawRightFitted(
		PDFAmount(booking.Amount, lang, true),
		pdf.Regular,
		9.0,
		amountRightEdge,
		wAmount,
	)
	receipt := i18n.Text(lang, i18n.No)
	if booking.DocumentCount > 0 {
		receipt = i18n.Text(lang, i18n.Yes)
	}
	layout.DrawRightFitted(
		receipt,
		pdf.Regular,
		9.0,
		receiptRightEdge,
		wReceipt,
	)
}

func shortAccount(booking *model.TransactionItem) string {
	if booking.AccountIBAN == nil || strings.TrimSpace(*booking.AccountIBAN) == "" {
		return ""
	}
	characters := []rune(*booking.AccountIBAN)
	start := len(characters) - 4
	if start < 0 {
		start = 0
	}
	return "…" + string(characters[start:])
}
