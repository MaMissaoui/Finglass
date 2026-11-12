package imports

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const xlsxWorkbook = `<?xml version="1.0" encoding="UTF-8"?>
<x:workbook xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <x:workbookPr/>
  <x:sheets><x:sheet name="Sheet1" sheetId="1" r:id="rId1"/></x:sheets>
</x:workbook>`

const xlsxRels = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`

const xlsxDateStyles = `<?xml version="1.0" encoding="UTF-8"?>
<x:styleSheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:numFmts count="1"><x:numFmt numFmtId="14" formatCode="M/d/yyyy"/></x:numFmts>
  <x:cellXfs count="3">
    <x:xf numFmtId="0"/>
    <x:xf numFmtId="14"/>
    <x:xf numFmtId="2" applyNumberFormat="1"/>
  </x:cellXfs>
</x:styleSheet>`

func xlsxFixture(sheet string, workbook string, rels string, shared *string, styles *string) []byte {
	entries := []zipEntry{
		{"xl/workbook.xml", []byte(workbook), 0},
		{"xl/_rels/workbook.xml.rels", []byte(rels), 0},
	}
	if shared != nil {
		entries = append(entries, zipEntry{"xl/sharedStrings.xml", []byte(*shared), 0})
	}
	if styles != nil {
		entries = append(entries, zipEntry{"xl/styles.xml", []byte(*styles), 0})
	}
	entries = append(entries, zipEntry{"xl/worksheets/sheet1.xml", []byte(sheet), 0})
	return writeZip(entries)
}

func xlsxDefault(sheet string) []byte {
	return xlsxFixture(sheet, xlsxWorkbook, xlsxRels, nil, nil)
}

func xlsxFormatAmount(value string) string {
	integer, fraction, found := strings.Cut(value, ".")
	if !found {
		return value + ".00"
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	return integer + "." + fraction[:2]
}

func assertXlsxTooLarge(t *testing.T, rows [][]string, err error) {
	t.Helper()
	if !errors.Is(err, ErrXlsxTooLarge) {
		t.Fatalf("err = %v (rows %v), want ErrXlsxTooLarge", err, rows)
	}
}

func assertXlsxUnreadable(t *testing.T, rows [][]string, err error) {
	t.Helper()
	if !errors.Is(err, ErrXlsxUnreadable) {
		t.Fatalf("err = %v (rows %v), want ErrXlsxUnreadable", err, rows)
	}
}

func TestSparseCellsAtAAndABAndAFormulaCellWithCachedValue(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="inlineStr"><x:is><x:t>head</x:t></x:is></x:c>
      <x:c r="AB1" t="inlineStr"><x:is><x:t>wide</x:t></x:is></x:c>
    </x:row>
    <x:row r="2"><x:c r="A2"><x:f>1+1</x:f><x:v>2</x:v></x:c></x:row>
  </x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0][0] != "head" || rows[0][27] != "wide" || len(rows[0]) != 28 {
		t.Errorf("row 0 = %q", rows[0])
	}
	if len(rows[1]) != 1 || rows[1][0] != "2" {
		t.Errorf("row 1 = %q", rows[1])
	}
}

func TestNumbersComeOutAsPlainDotDecimalsAndParseAmountsReadsThem(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1"><x:v>1.5</x:v></x:c>
      <x:c r="B1"><x:v>0.1</x:v></x:c>
      <x:c r="C1"><x:v>1234.5</x:v></x:c>
      <x:c r="D1"><x:v>-82.36</x:v></x:c>
      <x:c r="E1"><x:v>1000</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.5", "0.1", "1234.5", "-82.36", "1000"}
	for index, value := range want {
		if rows[0][index] != value {
			t.Errorf("cell %d = %q, want %q", index, rows[0][index], value)
		}
	}
	wantFormatted := []string{"1.50", "0.10", "1234.50", "-82.36", "1000.00"}
	for index, value := range wantFormatted {
		if got := xlsxFormatAmount(rows[0][index]); got != value {
			t.Errorf("formatted %d = %q, want %q", index, got, value)
		}
	}
}

func TestDateStylesRenderISODatesAndThe1904EpochShiftsTheBase(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" s="1"><x:v>46237</x:v></x:c>
      <x:c r="B1" s="1"><x:v>46239.75</x:v></x:c>
      <x:c r="C1" s="2"><x:v>45.67</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>`
	styles := xlsxDateStyles
	rows, err := FirstSheetRows(xlsxFixture(sheet, xlsxWorkbook, xlsxRels, nil, &styles))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "2026-08-03" || rows[0][1] != "2026-08-05" || rows[0][2] != "45.67" {
		t.Errorf("row = %q", rows[0])
	}
	workbook1904 := strings.Replace(xlsxWorkbook, "<x:workbookPr/>", `<x:workbookPr date1904="true"/>`, 1)
	sheet1904 := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1" s="1"><x:v>0</x:v></x:c></x:row></x:sheetData>
</x:worksheet>`
	rows1904, err := FirstSheetRows(xlsxFixture(sheet1904, workbook1904, xlsxRels, nil, &styles))
	if err != nil {
		t.Fatal(err)
	}
	if rows1904[0][0] != "1904-01-01" {
		t.Errorf("1904 date = %q", rows1904[0][0])
	}
}

func TestACappedNumericCellStaysItsRawVTextAndAnOutOfRangeDateSerialIsEmpty(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1"><x:v>1E999999999</x:v></x:c>
      <x:c r="B1" s="1"><x:v>99999999</x:v></x:c>
      <x:c r="C1" s="1"><x:v>46237</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>`
	styles := xlsxDateStyles
	rows, err := FirstSheetRows(xlsxFixture(sheet, xlsxWorkbook, xlsxRels, nil, &styles))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "1E999999999" || rows[0][1] != "" || rows[0][2] != "2026-08-03" {
		t.Errorf("row = %q", rows[0])
	}
}

func TestAnOutOfRangeSharedStringIndexOnlyEmptiesThatCell(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="s"><x:v>0</x:v></x:c>
      <x:c r="B1" t="s"><x:v>99</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>`
	stringsXML := `<?xml version="1.0" encoding="UTF-8"?>
<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="1">
  <x:si><x:t>ok</x:t></x:si>
</x:sst>`
	rows, err := FirstSheetRows(xlsxFixture(sheet, xlsxWorkbook, xlsxRels, &stringsXML, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0]) != 2 || rows[0][0] != "ok" || rows[0][1] != "" {
		t.Errorf("row = %q", rows[0])
	}
}

func TestMoreThan2000EntriesIsRefused(t *testing.T) {
	entries := make([]zipEntry, 0, 2001)
	for index := 0; index < 2001; index++ {
		entries = append(entries, zipEntry{"part/" + intToString(index) + ".xml", []byte("x"), 0})
	}
	rows, err := FirstSheetRows(writeZip(entries))
	assertXlsxTooLarge(t, rows, err)
}

func TestA61MBEntryThatIsNotNeededIsRefused(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>`
	padding := make([]byte, 1024*1024)
	entries := []zipEntry{
		{"xl/padding.bin", padding, 61},
		{"xl/workbook.xml", []byte(xlsxWorkbook), 0},
		{"xl/_rels/workbook.xml.rels", []byte(xlsxRels), 0},
		{"xl/worksheets/sheet1.xml", []byte(sheet), 0},
	}
	rows, err := FirstSheetRows(writeZip(entries))
	assertXlsxTooLarge(t, rows, err)
}

func TestAMissingWorkbookIsUnreadable(t *testing.T) {
	rows, err := FirstSheetRows(writeZip([]zipEntry{{"xl/worksheets/sheet1.xml", []byte("<worksheet/>"), 0}}))
	assertXlsxUnreadable(t, rows, err)
}

func TestADoctypeInTheSheetIsUnreadable(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<!DOCTYPE worksheet>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	assertXlsxUnreadable(t, rows, err)
}

func TestMalformedSheetXMLIsUnreadable(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"<x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	assertXlsxUnreadable(t, rows, err)
}

const xlsxOneCellSheet = `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>`

func TestSparseCellsWithRealCellReferencesLandInTheirOwnColumns(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="3">
      <x:c r="A3" t="inlineStr"><x:is><x:t>x</x:t></x:is></x:c>
      <x:c r="C3" t="inlineStr"><x:is><x:t>z</x:t></x:is></x:c>
    </x:row>
    <x:row r="5"><x:c r="AB5"><x:v>7</x:v></x:c></x:row>
  </x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[2]) != 3 || rows[2][0] != "x" || rows[2][1] != "" || rows[2][2] != "z" {
		t.Errorf("row 2 = %q", rows[2])
	}
	if len(rows[4]) != 28 || rows[4][27] != "7" {
		t.Errorf("row 4 = %q", rows[4])
	}
}

func TestAnOmittedMiddleCellKeepsLaterValuesInTheirColumns(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="inlineStr"><x:is><x:t>Date</x:t></x:is></x:c>
      <x:c r="B1" t="inlineStr"><x:is><x:t>Payee</x:t></x:is></x:c>
      <x:c r="C1" t="inlineStr"><x:is><x:t>Amount</x:t></x:is></x:c>
    </x:row>
    <x:row r="2">
      <x:c r="A2" t="inlineStr"><x:is><x:t>2026-08-03</x:t></x:is></x:c>
      <x:c r="C2"><x:v>-12.5</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>`
	rows, err := FirstSheetRows(xlsxDefault(sheet))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0]) != 3 || rows[0][0] != "Date" || rows[0][1] != "Payee" || rows[0][2] != "Amount" {
		t.Errorf("row 0 = %q", rows[0])
	}
	if len(rows[1]) != 3 || rows[1][0] != "2026-08-03" || rows[1][1] != "" || rows[1][2] != "-12.5" {
		t.Errorf("row 1 = %q", rows[1])
	}
}

func TestTwoEntriesWithTheSameWantedSheetNameAreUnreadable(t *testing.T) {
	bytesValue := xlsxDefault(xlsxOneCellSheet)
	centralDir := []byte{0x50, 0x4B, 0x01, 0x02}
	insertAt := bytes.Index(bytesValue, centralDir)
	if insertAt < 0 {
		t.Fatal("no central directory")
	}
	spliced := append([]byte{}, bytesValue[:insertAt]...)
	spliced = append(spliced, storedEntry("xl/worksheets/sheet1.xml", xlsxOneCellSheet)...)
	spliced = append(spliced, bytesValue[insertAt:]...)
	rows, err := FirstSheetRows(spliced)
	assertXlsxUnreadable(t, rows, err)
}

func TestRandomBytesAreUnreadable(t *testing.T) {
	bytesValue := make([]byte, 512)
	for index := range bytesValue {
		bytesValue[index] = byte(index*31 + 7)
	}
	rows, err := FirstSheetRows(bytesValue)
	assertXlsxUnreadable(t, rows, err)
}

func TestATruncatedZipIsUnreadable(t *testing.T) {
	bytesValue := xlsxDefault(xlsxOneCellSheet)
	rows, err := FirstSheetRows(bytesValue[:len(bytesValue)/2])
	assertXlsxUnreadable(t, rows, err)
}

func TestASheetNested100LevelsDeepIsUnreadable(t *testing.T) {
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0"?>`)
	sheet.WriteString(`<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for index := 0; index < 100; index++ {
		sheet.WriteString("<x:n>")
	}
	sheet.WriteString(`<x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>`)
	for index := 0; index < 100; index++ {
		sheet.WriteString("</x:n>")
	}
	sheet.WriteString("</x:worksheet>")
	rows, err := FirstSheetRows(xlsxDefault(sheet.String()))
	assertXlsxUnreadable(t, rows, err)
}
