// CSV writing and the report CSV exports, ported from ReportService.kt and
// CsvFormat, via crates/finglass-core/src/report/csv.rs.
package report

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// utf8BOM is the UTF-8 byte order mark every report CSV starts with.
var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// CsvFormat is RFC 4180 quoting and the spreadsheet formula guard, exactly like
// FinDesk's CsvFormat.
type CsvFormat struct{}

// Text neutralises spreadsheet formula injection in free-text fields from bank
// data.
func (CsvFormat) Text(field string) string {
	for _, first := range field {
		switch first {
		case '=', '+', '-', '@', '\t', '\r':
			return "'" + field
		default:
			return field
		}
	}
	return field
}

// Row is one CSV record with a CRLF line ending.
func (CsvFormat) Row(fields []string) string {
	escaped := make([]string, 0, len(fields))
	for _, field := range fields {
		escaped = append(escaped, CsvFormat{}.Escape(field))
	}
	return strings.Join(escaped, ",") + "\r\n"
}

// Escape quotes a field only when it contains a comma, quote or line break.
func (CsvFormat) Escape(field string) string {
	if strings.ContainsAny(field, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(field, "\"", "\"\"") + "\""
	}
	return field
}

// ExportToCSV exports transactions as a UTF-8 CSV with a BOM.
//
// The Shop column is always last; balances adds a Balance column just before it
// when the transaction list shows a running balance. The Reports export leaves
// the Balance column out: nil means no balance column, an empty map still adds
// one.
func ExportToCSV(
	all []model.TransactionItem,
	output string,
	balances map[model.TransactionID]money.Money,
	lang i18n.Language,
) error {
	file, err := os.Create(output) // #nosec G304 -- the export path is chosen by the caller.
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	if _, err := writer.Write(utf8BOM); err != nil {
		_ = file.Close()
		return err
	}

	header := []string{
		i18n.Text(lang, i18n.ColDate),
		i18n.Text(lang, i18n.ColPartner),
		i18n.Text(lang, i18n.ColPurpose),
		i18n.Text(lang, i18n.ColCategory),
		i18n.Text(lang, i18n.ColAmount),
		i18n.Text(lang, i18n.ColCurrency),
		i18n.Text(lang, i18n.ColAccount),
	}
	if balances != nil {
		header = append(header, i18n.Text(lang, i18n.ColBalance))
	}
	header = append(header, i18n.Text(lang, i18n.ColShop))
	if _, err := writer.WriteString(CsvFormat{}.Row(header)); err != nil {
		_ = file.Close()
		return err
	}

	for _, transaction := range all {
		fields := []string{
			transaction.Date.String(),
			CsvFormat{}.Text(optionalText(transaction.PartnerName)),
			CsvFormat{}.Text(optionalText(transaction.Purpose)),
			CsvFormat{}.Text(optionalText(transaction.CategoryName)),
			money.FormatPlain(transaction.Amount),
			CsvFormat{}.Text(transaction.Currency.String()),
			CsvFormat{}.Text(optionalText(transaction.AccountIBAN)),
		}
		if balances != nil {
			balance := ""
			if amount, ok := balances[transaction.ID]; ok {
				balance = money.FormatPlain(amount)
			}
			fields = append(fields, balance)
		}
		fields = append(fields, CsvFormat{}.Text(optionalText(
			rules.MerchantOf(transaction.Purpose, transaction.PartnerName),
		)))
		if _, err := writer.WriteString(CsvFormat{}.Row(fields)); err != nil {
			_ = file.Close()
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	// Rust drops the BufWriter and the File after flushing; a close error is
	// not surfaced.
	_ = file.Close()
	return nil
}

// ExportYearToCSV exports a whole year from the reports screen.
func ExportYearToCSV(
	ctx context.Context,
	d *db.Database,
	year int,
	output string,
	lang i18n.Language,
) error {
	return ExportRangeToCSV(ctx, d, mustDate(year, 1, 1), mustDate(year, 12, 31), output, lang)
}

// ExportRangeToCSV exports every booking in an inclusive date range.
func ExportRangeToCSV(
	ctx context.Context,
	d *db.Database,
	from civil.Date,
	to civil.Date,
	output string,
	lang i18n.Language,
) error {
	filter := model.TransactionFilter{StartDate: &from, EndDate: &to}
	items, err := transactions.FindItems(ctx, d, &filter, nil)
	if err != nil {
		return err
	}
	return ExportToCSV(items, output, nil, lang)
}

// optionalText returns the pointed-to text, or the empty string for nil.
func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
