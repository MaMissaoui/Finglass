package imports

import (
	"fmt"
	"testing"
)

const cardHVB = "Kartennummer;Zeitraum;Belegdatum;Eingangstag;Text/Verwendungszweck;Kurs;Betrag;Waehrung\n5412345678901234;01.05.2025 - 15.05.2025;03.05.2025;06.05.2025;REWE SAGT DANKE Ü;kurs;-58,94;EUR\n5412345678901234;01.05.2025 - 15.05.2025;04.05.2025;07.05.2025;Tankstelle am Ring;kurs;-12,50;EUR\n5412345678901234;01.05.2025 - 15.05.2025;05.05.2025;08.05.2025;Rückzahlung Versicherung;kurs;123,45;EUR"

func TestHVBCardCSVInUTF16LEWithoutBOM(t *testing.T) {
	statement, err := ParseCSV(utf16le(cardHVB), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.CharsetName != "UTF-16LE" {
		t.Errorf("charset = %q", statement.Csv.CharsetName)
	}
	if statement.Csv.Delimiter != ';' {
		t.Errorf("delimiter = %q", statement.Csv.Delimiter)
	}
	if len(statement.Bookings) != 3 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	wantDates := []civilDate{testDate(t, 2025, 5, 6), testDate(t, 2025, 5, 7), testDate(t, 2025, 5, 8)}
	for index, date := range wantDates {
		if statement.Bookings[index].Date != date {
			t.Errorf("date[%d] = %s, want %s", index, statement.Bookings[index].Date, date)
		}
	}
	wantValueDates := []civilDate{testDate(t, 2025, 5, 3), testDate(t, 2025, 5, 4), testDate(t, 2025, 5, 5)}
	for index, date := range wantValueDates {
		if statement.Bookings[index].ValueDate == nil || *statement.Bookings[index].ValueDate != date {
			t.Errorf("value date[%d] = %v, want %s", index, statement.Bookings[index].ValueDate, date)
		}
	}
	wantAmounts := []string{"-58.94", "-12.50", "123.45"}
	for index, amount := range wantAmounts {
		if got, want := statement.Bookings[index].Amount, dec(t, amount); got.Cmp(want) != 0 {
			t.Errorf("amount[%d] = %s, want %s", index, got, want)
		}
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "REWE SAGT DANKE Ü" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
	if statement.Bookings[0].Currency == nil || *statement.Bookings[0].Currency != "EUR" {
		t.Errorf("currency = %v", statement.Bookings[0].Currency)
	}
	if statement.Bookings[0].PartnerName != nil {
		t.Errorf("partner = %v", statement.Bookings[0].PartnerName)
	}
}

func TestHVBCardCSVInUTF16LEWithBOM(t *testing.T) {
	bytes := append([]byte{0xFF, 0xFE}, utf16le(cardHVB)...)
	statement, err := ParseCSV(bytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.CharsetName != "UTF-16LE" {
		t.Errorf("charset = %q", statement.Csv.CharsetName)
	}
	if len(statement.Bookings) != 3 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "REWE SAGT DANKE Ü" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
}

func TestHVBCardCSVInUTF16BEWithoutBOM(t *testing.T) {
	statement, err := ParseCSV(utf16be(cardHVB), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.CharsetName != "UTF-16BE" {
		t.Errorf("charset = %q", statement.Csv.CharsetName)
	}
	if len(statement.Bookings) != 3 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[2].Purpose == nil || *statement.Bookings[2].Purpose != "Rückzahlung Versicherung" {
		t.Errorf("purpose = %v", statement.Bookings[2].Purpose)
	}
}

func TestKartennummerMapsToNothing(t *testing.T) {
	headers := []string{"Kartennummer", "Zeitraum", "Belegdatum", "Eingangstag", "Text/Verwendungszweck", "Kurs", "Betrag", "Waehrung"}
	mapping := DetectMapping(headers)
	if mapping.Date == nil || *mapping.Date != 3 {
		t.Errorf("date = %v", mapping.Date)
	}
	if mapping.ValueDate == nil || *mapping.ValueDate != 2 {
		t.Errorf("value date = %v", mapping.ValueDate)
	}
	if mapping.Amount == nil || *mapping.Amount != 6 {
		t.Errorf("amount = %v", mapping.Amount)
	}
	if mapping.Purpose == nil || *mapping.Purpose != 4 {
		t.Errorf("purpose = %v", mapping.Purpose)
	}
	if mapping.Currency == nil || *mapping.Currency != 7 {
		t.Errorf("currency = %v", mapping.Currency)
	}
	if mapping.Partner != nil || mapping.Payer != nil || mapping.PartnerIBAN != nil || mapping.Balance != nil {
		t.Errorf("mapping = %+v", mapping)
	}
}

func TestExistingGermanCSVFormatStillParsesItsGroupedAmounts(t *testing.T) {
	utf8, err := ParseCSV([]byte("Buchungstag;Betrag;Verwendungszweck\n03.10.2026;1.234,56;Einkauf\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	windows, err := ParseCSV(cp1252(t, "Buchungstag;Betrag;Zahlungsempfänger;Verwendungszweck\n03.10.2026;1.234,56;Räderwerk;Rad kaufen\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.Csv.CharsetName != "UTF-8" {
		t.Errorf("utf8 charset = %q", utf8.Csv.CharsetName)
	}
	if got, want := utf8.Bookings[0].Amount, dec(t, "1234.56"); got.Cmp(want) != 0 {
		t.Errorf("utf8 amount = %s", got)
	}
	if windows.Csv.CharsetName != "windows-1252" {
		t.Errorf("windows charset = %q", windows.Csv.CharsetName)
	}
	if got, want := windows.Bookings[0].Amount, dec(t, "1234.56"); got.Cmp(want) != 0 {
		t.Errorf("windows amount = %s", got)
	}
	if windows.Bookings[0].PartnerName == nil || *windows.Bookings[0].PartnerName != "Räderwerk" {
		t.Errorf("partner = %v", windows.Bookings[0].PartnerName)
	}
}

const cardWorkbook = `<?xml version="1.0" encoding="UTF-8"?>
<x:workbook xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <x:workbookPr/>
  <x:sheets><x:sheet name="Transactions" sheetId="1" r:id="rId1"/></x:sheets>
</x:workbook>`

const cardRels = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`

const cardStyles = `<?xml version="1.0" encoding="UTF-8"?>
<x:styleSheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:numFmts count="1"><x:numFmt numFmtId="14" formatCode="M/d/yyyy"/></x:numFmts>
  <x:cellXfs count="2"><x:xf numFmtId="0"/><x:xf numFmtId="14"/></x:cellXfs>
</x:styleSheet>`

const cardSharedStrings = `<?xml version="1.0" encoding="UTF-8"?>
<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="16">
  <x:si><x:t>TransactionDate</x:t></x:si>
  <x:si><x:t>Text</x:t></x:si>
  <x:si><x:t>Type</x:t></x:si>
  <x:si><x:t>Currency Amount</x:t></x:si>
  <x:si><x:t>Currency Rate</x:t></x:si>
  <x:si><x:t>Currency</x:t></x:si>
  <x:si><x:t>Amount</x:t></x:si>
  <x:si><x:t>Merchant Area</x:t></x:si>
  <x:si><x:t>Merchant Category</x:t></x:si>
  <x:si><x:t>BookDate</x:t></x:si>
  <x:si><x:t>ValueDate</x:t></x:si>
  <x:si><x:t>Simpl Jewellery</x:t></x:si>
  <x:si><x:t>Purchase</x:t></x:si>
  <x:si><x:t>Shopping</x:t></x:si>
  <x:si><x:t>Jewellery Specialist</x:t></x:si>
  <x:si><x:t>Currency</x:t></x:si>
</x:sst>`

func cardXLSX(sheet string, workbook string, sharedStrings *string) []byte {
	entries := []zipEntry{
		{"xl/workbook.xml", []byte(workbook), 0},
		{"xl/_rels/workbook.xml.rels", []byte(cardRels), 0},
	}
	if sharedStrings != nil {
		entries = append(entries, zipEntry{"xl/sharedStrings.xml", []byte(*sharedStrings), 0})
	}
	entries = append(entries,
		zipEntry{"xl/styles.xml", []byte(cardStyles), 0},
		zipEntry{"xl/worksheets/sheet1.xml", []byte(sheet), 0},
	)
	return writeZip(entries)
}

func cardColumn(index int) string {
	return fmt.Sprintf("%c", 'A'+index)
}

func cardNorwegianHeader() string {
	header := ""
	for index := 0; index < 11; index++ {
		header += "<x:c r=\"" + cardColumn(index) + "1\" t=\"s\"><x:v>" + intToString(index) + "</x:v></x:c>"
	}
	return header
}

func cardNorwegianRow(amount string) string {
	return "<x:c r=\"A2\" s=\"1\"><x:v>46237</x:v></x:c>" +
		"<x:c r=\"B2\" t=\"s\"><x:v>11</x:v></x:c><x:c r=\"C2\" t=\"s\"><x:v>12</x:v></x:c>" +
		"<x:c r=\"D2\"><x:v>-10.00</x:v></x:c><x:c r=\"E2\"><x:v>1.08</x:v></x:c><x:c r=\"F2\" t=\"s\"><x:v>5</x:v></x:c>" +
		"<x:c r=\"G2\"><x:v>" + amount + "</x:v></x:c><x:c r=\"H2\" t=\"s\"><x:v>13</x:v></x:c><x:c r=\"I2\" t=\"s\"><x:v>14</x:v></x:c>" +
		"<x:c r=\"J2\" s=\"1\"><x:v>46238</x:v></x:c><x:c r=\"K2\" s=\"1\"><x:v>46239</x:v></x:c>"
}

func cardNorwegianSheet(row string) string {
	return "<?xml version=\"1.0\"?>\n<x:worksheet xmlns:x=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\">\n  <x:sheetData>\n    <x:row r=\"1\">" + cardNorwegianHeader() + "</x:row>\n    <x:row r=\"2\">" + row + "</x:row>\n  </x:sheetData>\n</x:worksheet>"
}

func cardParseRows(t *testing.T, bytes []byte) ParsedStatement {
	t.Helper()
	rows, err := FirstSheetRows(bytes)
	if err != nil {
		t.Fatal(err)
	}
	return ParseCSVRows(rows, nil, FormatXLSX, "XLSX", ';', testDate(t, 2026, 11, 1))
}

func TestBankNorwegianXLSXWithPrefixedNamespaceSharedStringsAndAReDeclaredDateStyle(t *testing.T) {
	sheet := cardNorwegianSheet(cardNorwegianRow("-82.36"))
	shared := cardSharedStrings
	statement := cardParseRows(t, cardXLSX(sheet, cardWorkbook, &shared))
	if statement.Format != FormatXLSX {
		t.Errorf("format = %s", statement.Format)
	}
	if statement.Csv.CharsetName != "XLSX" {
		t.Errorf("charset = %q", statement.Csv.CharsetName)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	booking := statement.Bookings[0]
	if booking.Date != testDate(t, 2026, 8, 4) {
		t.Errorf("date = %s", booking.Date)
	}
	if booking.ValueDate == nil || *booking.ValueDate != testDate(t, 2026, 8, 5) {
		t.Errorf("value date = %v", booking.ValueDate)
	}
	if got, want := booking.Amount, dec(t, "-82.36"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if booking.Purpose == nil || *booking.Purpose != "Simpl Jewellery" {
		t.Errorf("purpose = %v", booking.Purpose)
	}
	if booking.Currency != nil {
		t.Errorf("currency = %v", booking.Currency)
	}
}

func TestBankNorwegianXLSXWithDefaultNamespaceAndInlineStrings(t *testing.T) {
	namespace := "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	names := []string{
		"TransactionDate", "Text", "Type", "Currency Amount", "Currency Rate", "Currency",
		"Amount", "Merchant Area", "Merchant Category", "BookDate", "ValueDate",
	}
	header := ""
	for index, text := range names {
		header += "<c r=\"" + cardColumn(index) + "1\" t=\"inlineStr\"><is><t>" + text + "</t></is></c>"
	}
	sheet := "<?xml version=\"1.0\"?>\n<worksheet xmlns=\"" + namespace + "\">\n  <sheetData>\n    <row r=\"1\">" + header + "</row>\n" +
		"    <row r=\"2\"><c r=\"A2\" s=\"1\"><v>46237</v></c><c r=\"B2\" t=\"inlineStr\"><is><t>Simpl Jewellery</t></is></c><c r=\"C2\" t=\"inlineStr\"><is><t>Purchase</t></is></c>" +
		"<c r=\"D2\"><v>-10.00</v></c><c r=\"E2\"><v>1.08</v></c><c r=\"F2\" t=\"inlineStr\"><is><t>EUR</t></is></c><c r=\"G2\"><v>-82.36</v></c>" +
		"<c r=\"H2\" t=\"inlineStr\"><is><t>Shopping</t></is></c><c r=\"I2\" t=\"inlineStr\"><is><t>Jewellery Specialist</t></is></c>" +
		"<c r=\"J2\" s=\"1\"><v>46238</v></c><c r=\"K2\" s=\"1\"><v>46239</v></c></row>\n  </sheetData>\n</worksheet>"
	statement := cardParseRows(t, cardXLSX(sheet, cardWorkbook, nil))
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	booking := statement.Bookings[0]
	if booking.Date != testDate(t, 2026, 8, 4) {
		t.Errorf("date = %s", booking.Date)
	}
	if booking.ValueDate == nil || *booking.ValueDate != testDate(t, 2026, 8, 5) {
		t.Errorf("value date = %v", booking.ValueDate)
	}
	if got, want := booking.Amount, dec(t, "-82.36"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if booking.Purpose == nil || *booking.Purpose != "Simpl Jewellery" {
		t.Errorf("purpose = %v", booking.Purpose)
	}
}

func TestAUSDPurchaseWithItsOwnCurrencyAmountImportsTheBilledEURAmount(t *testing.T) {
	sheet := cardNorwegianSheet(cardNorwegianRow("-9.12"))
	shared := cardSharedStrings
	statement := cardParseRows(t, cardXLSX(sheet, cardWorkbook, &shared))
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-9.12"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if statement.Bookings[0].Currency != nil {
		t.Errorf("currency = %v", statement.Bookings[0].Currency)
	}
}

func TestNumericAmountsFromXLSXCells(t *testing.T) {
	numbers := []string{"1.5", "0.1", "1234.5", "-82.36", "1000"}
	rows := ""
	for index, number := range numbers {
		if index > 0 {
			rows += "\n    "
		}
		row := index + 2
		rows += "<x:row r=\"" + intToString(row) + "\"><x:c r=\"A" + intToString(row) + "\" t=\"inlineStr\"><x:is><x:t>2026-08-04</x:t></x:is></x:c>" +
			"<x:c r=\"B" + intToString(row) + "\"><x:v>" + number + "</x:v></x:c>" +
			"<x:c r=\"C" + intToString(row) + "\" t=\"inlineStr\"><x:is><x:t>Sinus Power</x:t></x:is></x:c></x:row>"
	}
	sheet := "<?xml version=\"1.0\"?>\n<x:worksheet xmlns:x=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\">\n  <x:sheetData>\n" +
		"    <x:row r=\"1\"><x:c r=\"A1\" t=\"inlineStr\"><x:is><x:t>Date</x:t></x:is></x:c><x:c r=\"B1\" t=\"inlineStr\"><x:is><x:t>Amount</x:t></x:is></x:c><x:c r=\"C1\" t=\"inlineStr\"><x:is><x:t>Purpose</x:t></x:is></x:c></x:row>\n" +
		"    " + rows + "\n  </x:sheetData>\n</x:worksheet>"
	shared := cardSharedStrings
	statement := cardParseRows(t, cardXLSX(sheet, cardWorkbook, &shared))
	want := []string{"1.50", "0.10", "1234.50", "-82.36", "1000.00"}
	if len(statement.Bookings) != len(want) {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	for index, value := range want {
		if got := statement.Bookings[index].Amount.String(); got != value {
			t.Errorf("amount[%d] = %q, want %q", index, got, value)
		}
	}
}
