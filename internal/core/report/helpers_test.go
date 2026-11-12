package report

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/export/pdf"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func usd(minor int64) money.Money {
	currency, err := money.NewCurrency("USD")
	if err != nil {
		panic(err)
	}
	return money.Money{Minor: minor, Currency: currency}
}

func strPtr(value string) *string {
	return &value
}

func testDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %v, %d) failed", year, month, day)
	}
	return value
}

// testDatabase creates a throwaway encrypted database under t.TempDir().
func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	d, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

var accountNumber atomic.Uint64

func addAccount(t *testing.T, d *db.Database, currency money.Currency) model.AccountID {
	t.Helper()
	number := accountNumber.Add(1) - 1
	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        strPtr("Account " + strconv.FormatUint(number, 10)),
		Currency:    currency,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: currency},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func addCategory(
	t *testing.T,
	d *db.Database,
	name string,
	color string,
	taxRelevant bool,
	transfer bool,
) model.CategoryID {
	t.Helper()
	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:          name,
		Color:         color,
		IsTaxRelevant: taxRelevant,
		IsTransfer:    transfer,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

var transactionNumber atomic.Uint64

func addTransaction(
	t *testing.T,
	d *db.Database,
	account model.AccountID,
	day civil.Date,
	amount money.Money,
	partner *string,
	purpose *string,
	category *model.CategoryID,
) {
	t.Helper()
	number := transactionNumber.Add(1) - 1
	_, err := transactions.InsertImported(context.Background(), d, account, []model.NewTransaction{{
		BookingDate: day,
		Amount:      amount,
		PartnerName: partner,
		Purpose:     purpose,
		CategoryID:  category,
		ImportHash:  "test-hash-" + strconv.FormatUint(number, 10),
		CreatedAt:   time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("transactions.InsertImported: %v", err)
	}
}

func allItems(t *testing.T, d *db.Database) []model.TransactionItem {
	t.Helper()
	items, err := transactions.FindItems(context.Background(), d, &model.TransactionFilter{}, nil)
	if err != nil {
		t.Fatalf("transactions.FindItems: %v", err)
	}
	return items
}

// itemBuilder builds a TransactionItem for the pure calculations, like Rust's
// test-only Item.
type itemBuilder struct {
	id            int64
	minor         int64
	day           civil.Date
	categoryID    *model.CategoryID
	categoryName  *string
	categoryColor *string
	taxRelevant   bool
	taxSource     model.TaxSource
	currency      money.Currency
	documents     int
	partner       *string
	purpose       *string
	transfer      bool
	taxOverride   *bool
}

func item(id, minor int64) itemBuilder {
	date, ok := civil.New(2026, time.June, 1)
	if !ok {
		panic("valid date")
	}
	return itemBuilder{id: id, minor: minor, day: date, currency: money.EUR, taxSource: model.TaxSourceNone}
}

func (b itemBuilder) Date(day civil.Date) itemBuilder {
	b.day = day
	return b
}

func (b itemBuilder) Category(id int64, name string, color string) itemBuilder {
	categoryID := model.CategoryID(id)
	b.categoryID = &categoryID
	b.categoryName = strPtr(name)
	b.categoryColor = strPtr(color)
	return b
}

func (b itemBuilder) Tax(source model.TaxSource) itemBuilder {
	b.taxRelevant = true
	b.taxSource = source
	return b
}

func (b itemBuilder) TaxRelevant() itemBuilder {
	b.taxRelevant = true
	if b.taxSource == model.TaxSourceNone {
		b.taxSource = model.TaxSourceCategory
	}
	return b
}

func (b itemBuilder) Source(source model.TaxSource) itemBuilder {
	b.taxSource = source
	return b
}

func (b itemBuilder) Currency(currency money.Currency) itemBuilder {
	b.currency = currency
	return b
}

func (b itemBuilder) Documents(count int) itemBuilder {
	b.documents = count
	return b
}

func (b itemBuilder) Partner(partner string) itemBuilder {
	b.partner = strPtr(partner)
	return b
}

func (b itemBuilder) Purpose(purpose string) itemBuilder {
	b.purpose = strPtr(purpose)
	return b
}

func (b itemBuilder) Transfer() itemBuilder {
	b.transfer = true
	return b
}

func (b itemBuilder) OverrideTax(value bool) itemBuilder {
	b.taxOverride = &value
	return b
}

func (b itemBuilder) WithCurrency(currency money.Currency) itemBuilder {
	b.currency = currency
	return b
}

func (b itemBuilder) Build() model.TransactionItem {
	return model.TransactionItem{
		ID:            model.TransactionID(b.id),
		AccountID:     model.AccountID(1),
		AccountIBAN:   strPtr("DE89370400440532013000"),
		Date:          b.day,
		Amount:        money.Money{Minor: b.minor, Currency: b.currency},
		Currency:      b.currency,
		PartnerName:   b.partner,
		Purpose:       b.purpose,
		CategoryID:    b.categoryID,
		CategoryName:  b.categoryName,
		CategoryColor: b.categoryColor,
		IsTaxRelevant: b.taxRelevant,
		DocumentCount: b.documents,
		IsTransfer:    b.transfer,
		TaxSource:     b.taxSource,
		TaxOverride:   b.taxOverride,
	}
}

func itemsOf(values []itemBuilder) []model.TransactionItem {
	items := make([]model.TransactionItem, 0, len(values))
	for _, value := range values {
		items = append(items, value.Build())
	}
	return items
}

func categoryAmount(pairs []NamedAmount, name string) (money.Money, bool) {
	for _, entry := range pairs {
		if entry.Name == name {
			return entry.Amount, true
		}
	}
	return money.Money{}, false
}

func single[T any](t *testing.T, values []T) *T {
	t.Helper()
	if len(values) != 1 {
		t.Fatalf("expected exactly one item, got %d", len(values))
	}
	return &values[0]
}

// reportFixture builds the report tests' small database.
func reportFixture(t *testing.T) *db.Database {
	t.Helper()
	d := testDatabase(t)
	account := addAccount(t, d, money.EUR)
	food := addCategory(t, d, "Food", "#FF5722", false, false)
	office := addCategory(t, d, "Arbeitsmittel", "#2196F3", true, false)
	addTransaction(
		t, d, account, testDate(t, 2026, time.January, 10), eur(-4_000),
		strPtr("REWE"), nil, &food,
	)
	addTransaction(
		t, d, account, testDate(t, 2026, time.February, 10), eur(-25_000),
		strPtr("=HYPERLINK(\"http://evil\")"), strPtr("Monitor, 27\""), &office,
	)
	addTransaction(
		t, d, account, testDate(t, 2026, time.February, 28), eur(300_000),
		strPtr("Employer GmbH"), nil, nil,
	)
	addTransaction(
		t, d, account, testDate(t, 2025, time.December, 31), eur(-100),
		strPtr("Last year"), nil, nil,
	)
	return d
}

// --- PDF structural helpers -------------------------------------------------

func assertPDF(t *testing.T, content []byte) {
	t.Helper()
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		t.Fatal("a PDF header")
	}
	end := bytes.TrimSuffix(content, []byte("\n"))
	if !bytes.HasSuffix(end, []byte("%%EOF")) {
		t.Fatal("a PDF trailer")
	}
}

func pdfContains(content []byte, text string) bool {
	return strings.Contains(pdfStringText(content), text)
}

// pdfStringText decodes literal and hex strings in the (uncompressed) PDF
// content streams, like Rust's pdf_string_text.
func pdfStringText(content []byte) string {
	var out bytes.Buffer
	index := 0
	for index < len(content) {
		switch content[index] {
		case '(':
			index++
			literal := make([]byte, 0)
			for index < len(content) {
				switch content[index] {
				case ')':
					index++
					goto literalDone
				case '\\':
					index++
					if index >= len(content) {
						goto literalDone
					}
					switch content[index] {
					case 'n':
						literal = append(literal, '\n')
					case 'r':
						literal = append(literal, '\r')
					case 't':
						literal = append(literal, '\t')
					case 'b':
						literal = append(literal, 0x08)
					case 'f':
						literal = append(literal, 0x0c)
					case '0', '1', '2', '3', '4', '5', '6', '7':
						digitsStart := index
						digits := 0
						for digits < 3 && index < len(content) && content[index] >= '0' && content[index] <= '7' {
							index++
							digits++
						}
						value, err := strconv.ParseUint(string(content[digitsStart:index]), 8, 8)
						if err != nil {
							value = 0
						}
						literal = append(literal, byte(value))
						continue
					default:
						literal = append(literal, content[index])
					}
				default:
					literal = append(literal, content[index])
				}
				index++
			}
		literalDone:
			out.WriteString(pdf.TextFromWinAnsi(literal))
		case '<':
			if index+1 < len(content) && content[index+1] == '<' {
				index++
				continue
			}
			index++
			digits := make([]byte, 0)
			for index < len(content) && content[index] != '>' {
				if isASCIIHexDigit(content[index]) {
					digits = append(digits, content[index])
				}
				index++
			}
			if len(digits)%2 == 1 {
				digits = append(digits, '0')
			}
			decoded := make([]byte, 0, len(digits)/2)
			for position := 0; position+1 < len(digits); position += 2 {
				value, err := strconv.ParseUint(string(digits[position:position+2]), 16, 8)
				if err != nil {
					continue
				}
				decoded = append(decoded, byte(value))
			}
			out.WriteString(pdf.TextFromWinAnsi(decoded))
			index++
		default:
			index++
		}
	}
	return out.String()
}

func isASCIIHexDigit(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')
}

func pdfPageCount(content []byte) int {
	return bytesCount(content, []byte("/Type /Page")) - bytesCount(content, []byte("/Type /Pages"))
}

func bytesCount(haystack, needle []byte) int {
	count := 0
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if bytes.Equal(haystack[index:index+len(needle)], needle) {
			count++
		}
	}
	return count
}

// --- XLSX structural helpers ------------------------------------------------

func zipEntry(t *testing.T, content []byte, name string) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open the workbook: %v", err)
	}
	for _, entry := range archive.File {
		if entry.Name != name {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close %s: %v", name, err)
		}
		return content
	}
	t.Fatalf("the workbook has %s", name)
	return nil
}

type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []xmlNode  `xml:",any"`
	Text     string     `xml:",chardata"`
}

func parseXML(t *testing.T, content []byte) xmlNode {
	t.Helper()
	var node xmlNode
	if err := xml.Unmarshal(content, &node); err != nil {
		t.Fatalf("parse the XML: %v", err)
	}
	return node
}

func xmlDescendants(node xmlNode, name string) []xmlNode {
	found := make([]xmlNode, 0)
	for _, child := range node.Children {
		if child.XMLName.Local == name {
			found = append(found, child)
		}
		found = append(found, xmlDescendants(child, name)...)
	}
	return found
}

func xmlAttribute(node xmlNode, name string) string {
	for _, attribute := range node.Attrs {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func cellNode(t *testing.T, sheet xmlNode, reference string) xmlNode {
	t.Helper()
	for _, cell := range xmlDescendants(sheet, "c") {
		if xmlAttribute(cell, "r") == reference {
			return cell
		}
	}
	t.Fatalf("the cell %s exists", reference)
	return xmlNode{}
}

func cellText(t *testing.T, sheet xmlNode, reference string) string {
	t.Helper()
	cell := cellNode(t, sheet, reference)
	texts := xmlDescendants(cell, "t")
	if len(texts) == 0 {
		return ""
	}
	return texts[0].Text
}

func cellValue(t *testing.T, sheet xmlNode, reference string) string {
	t.Helper()
	cell := cellNode(t, sheet, reference)
	values := xmlDescendants(cell, "v")
	if len(values) == 0 {
		return ""
	}
	return values[0].Text
}

func cellStyle(t *testing.T, sheet xmlNode, reference string) string {
	t.Helper()
	return xmlAttribute(cellNode(t, sheet, reference), "s")
}

func dateCell(t *testing.T, sheet xmlNode, reference string) string {
	t.Helper()
	serial, err := strconv.ParseInt(cellValue(t, sheet, reference), 10, 64)
	if err != nil {
		t.Fatalf("the date cell is a number: %v", err)
	}
	base, ok := civil.New(1899, time.December, 30)
	if !ok {
		t.Fatal("valid Excel epoch")
	}
	return base.AddDays(int(serial)).String()
}

// firstSheetRows reads the first worksheet through the production reader.
func firstSheetRows(t *testing.T, bytes []byte) [][]string {
	t.Helper()
	rows, err := imports.FirstSheetRows(bytes)
	if err != nil {
		t.Fatalf("first sheet rows: %v", err)
	}
	return rows
}

// xlsxSheetRows reads one worksheet by pointing the first relationship at that
// sheet and reusing the production reader, rewriting the relationship target in
// memory exactly like Rust's xlsx_sheet_rows.
func xlsxSheetRows(t *testing.T, content []byte, sheet int) [][]string {
	t.Helper()
	if sheet == 0 {
		return firstSheetRows(t, content)
	}
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open the workbook: %v", err)
	}
	type entry struct {
		name    string
		content []byte
	}
	entries := make([]entry, 0, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close %s: %v", file.Name, err)
		}
		entries = append(entries, entry{name: file.Name, content: data})
	}
	for index := range entries {
		if entries[index].name != "xl/_rels/workbook.xml.rels" {
			continue
		}
		replaced := bytes.ReplaceAll(
			entries[index].content,
			[]byte("Target=\"worksheets/sheet1.xml\""),
			[]byte("Target=\"worksheets/sheet"+strconv.Itoa(sheet+1)+".xml\""),
		)
		entries[index].content = replaced
	}
	var rewritten bytes.Buffer
	writer := zip.NewWriter(&rewritten)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		header.SetMode(0o644)
		output, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("rewrite %s: %v", item.name, err)
		}
		if _, err := output.Write(item.content); err != nil {
			t.Fatalf("rewrite %s: %v", item.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the rewritten workbook: %v", err)
	}
	return firstSheetRows(t, rewritten.Bytes())
}

// mappingEqual compares two detected mappings by value.
func mappingEqual(left, right imports.CsvMapping) bool {
	return sameOptionalInt(left.Date, right.Date) &&
		sameOptionalInt(left.ValueDate, right.ValueDate) &&
		sameOptionalInt(left.Amount, right.Amount) &&
		sameOptionalInt(left.Debit, right.Debit) &&
		sameOptionalInt(left.Credit, right.Credit) &&
		sameOptionalInt(left.Partner, right.Partner) &&
		sameOptionalInt(left.Payer, right.Payer) &&
		sameOptionalInt(left.PartnerIBAN, right.PartnerIBAN) &&
		sameOptionalInt(left.Purpose, right.Purpose) &&
		sameOptionalInt(left.Currency, right.Currency) &&
		sameOptionalInt(left.Balance, right.Balance)
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalIntEquals(value *int, expected int) bool {
	return value != nil && *value == expected
}

func xmlFind(t *testing.T, node xmlNode, name string) xmlNode {
	t.Helper()
	found := xmlDescendants(node, name)
	if len(found) == 0 {
		t.Fatalf("the element %s exists", name)
	}
	return found[0]
}
