package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

type zipEntry struct {
	name   string
	data   []byte
	method uint16
	mode   os.FileMode
}

func readZip(t *testing.T, path string) []zipEntry {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
	}()

	entries := make([]zipEntry, 0, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", file.Name, err)
		}
		var content bytes.Buffer
		if _, err := io.Copy(&content, reader); err != nil { // #nosec G110 -- reference workbooks are trusted test fixtures.
			_ = reader.Close()
			t.Fatalf("read entry %s: %v", file.Name, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close entry %s: %v", file.Name, err)
		}
		entries = append(entries, zipEntry{name: file.Name, data: content.Bytes(), method: file.Method, mode: file.Mode()})
	}
	return entries
}

func zipEntryByName(t *testing.T, entries []zipEntry, name string) []byte {
	t.Helper()
	for _, entry := range entries {
		if entry.name == name {
			return entry.data
		}
	}
	t.Fatalf("the part %s exists", name)
	return nil
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func mustDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	date, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid test date %04d-%02d-%02d", year, month, day)
	}
	return date
}

type cellNode struct {
	Reference string `xml:"r,attr"`
	Inline    struct {
		Text string `xml:"t"`
	} `xml:"is"`
}

type rowNode struct {
	Cells []cellNode `xml:"c"`
}

type sheetNode struct {
	Pane *struct {
		YSplit      string `xml:"ySplit,attr"`
		TopLeftCell string `xml:"topLeftCell,attr"`
		State       string `xml:"state,attr"`
	} `xml:"sheetViews>sheetView>pane"`
	AutoFilter *struct {
		Reference string `xml:"ref,attr"`
	} `xml:"autoFilter"`
	Rows []rowNode `xml:"sheetData>row"`
}

func parsedCellText(sheet sheetNode, reference string) string {
	for _, row := range sheet.Rows {
		for _, cell := range row.Cells {
			if cell.Reference == reference {
				return cell.Inline.Text
			}
		}
	}
	return ""
}

func TestTwoSheetsRoundTripThroughTheReader(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "round-trip.xlsx")

	writer := NewWriter()
	writer.Sheet("Data", []float64{20.0, 12.0, 12.0, 14.0}, false, false, func(sheet *Sheet) {
		sheet.Row(
			Bold("Text"),
			Bold("Count"),
			Bold("Money"),
			Bold("Date"),
		)
		sheet.Row(
			Text("=1+1"),
			NumberInt(42),
			Money(eur(-8_236)),
			Date(mustDate(t, 2026, time.August, 3)),
		)
		sheet.Row(
			Text("A & B <tag> \"q\" 'a'"),
			Number("0.10"),
			Money(eur(123_450)),
			Date(mustDate(t, 1999, time.December, 31)),
		)
		sheet.Row(
			Text("first"),
			Empty(),
			Empty(),
			Text("sparse"),
		)
	})
	writer.Sheet("Extra", []float64{10.0}, false, false, func(sheet *Sheet) {
		sheet.Row(Text("second sheet"))
	})
	if err := writer.Write(file); err != nil {
		t.Fatalf("the workbook is written: %v", err)
	}

	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the workbook is at the target: %v", err)
	}
	bytes, err := os.ReadFile(file) // #nosec G304 -- file is this test's t.TempDir() workbook.
	if err != nil {
		t.Fatalf("the workbook is readable: %v", err)
	}
	rows, err := imports.FirstSheetRows(bytes)
	if err != nil {
		t.Fatalf("the reader accepts the workbook: %v", err)
	}

	if got, want := rows[0], []string{"Text", "Count", "Money", "Date"}; !equalStrings(got, want) {
		t.Fatalf("the bold header row = %#v, want %#v", got, want)
	}
	if got := rows[1][0]; got != "=1+1" {
		t.Fatalf("a text value that looks like a formula stays text: %q", got)
	}
	if got := rows[1][1]; got != "42" {
		t.Fatalf("number text = %q, want 42", got)
	}
	if got := rows[1][2]; got != "-82.36" {
		t.Fatalf("money text = %q, want -82.36", got)
	}
	if got := rows[1][3]; got != "2026-08-03" {
		t.Fatalf("a date cell reads back as an ISO date: %q", got)
	}
	if got := rows[2][0]; got != "A & B <tag> \"q\" 'a'" {
		t.Fatalf("XML escapes round trip: %q", got)
	}
	if got := rows[2][1]; got != "0.1" {
		t.Fatalf("the reader's number text drops trailing zeros: %q", got)
	}
	if got := rows[2][2]; got != "1234.5" {
		t.Fatalf("money text = %q, want 1234.5", got)
	}
	if got := rows[2][3]; got != "1999-12-31" {
		t.Fatalf("date text = %q, want 1999-12-31", got)
	}
	if got := rows[3][0]; got != "first" {
		t.Fatalf("gaps become empty strings: %q", got)
	}
	if got := rows[3][1]; got != "" {
		t.Fatalf("gap text = %q, want empty", got)
	}
	if got := rows[3][3]; got != "sparse" {
		t.Fatalf("cell after gaps = %q, want sparse", got)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hasFormula(data []byte) (bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "f" {
			return true, nil
		}
	}
}

func TestEveryPartIsWellFormedXMLWithNoFormulas(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "parts.xlsx")

	writer := NewWriter()
	writer.Sheet("Plan", []float64{20.0, 20.0}, true, true, func(sheet *Sheet) {
		sheet.Row(Bold("Head & <More>"), Bold("Second"))
		sheet.Row(Text("=1+1"), Text("a\u0001b\u000bc"))
		sheet.Row(
			Text("lone\uFFFDsurrogate"),
			Text("quote \" and ' plus >"),
		)
		sheet.Row(Text("row"))
	})
	writer.Sheet("Zweit", []float64{10.0}, false, false, func(sheet *Sheet) {
		sheet.Row(Text("nur Text"))
	})
	if err := writer.Write(file); err != nil {
		t.Fatalf("the workbook is written: %v", err)
	}

	entries := readZip(t, file)
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.name
	}
	wantNames := []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"xl/workbook.xml",
		"xl/_rels/workbook.xml.rels",
		"xl/styles.xml",
		"xl/worksheets/sheet1.xml",
		"xl/worksheets/sheet2.xml",
	}
	if !equalStrings(names, wantNames) {
		t.Fatalf("entry names = %#v, want %#v", names, wantNames)
	}

	for _, entry := range entries {
		formula, err := hasFormula(entry.data)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.name, err)
		}
		if formula {
			t.Fatalf("no formula element in any part, found one in %s", entry.name)
		}
	}

	contentTypesBytes := zipEntryByName(t, entries, "[Content_Types].xml")
	var contentTypes struct {
		Overrides []struct {
			PartName string `xml:"PartName,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(contentTypesBytes, &contentTypes); err != nil {
		t.Fatalf("parse content types: %v", err)
	}
	partNames := make([]string, 0, len(contentTypes.Overrides))
	for _, override := range contentTypes.Overrides {
		partNames = append(partNames, override.PartName)
	}
	if !containsString(partNames, "/xl/worksheets/sheet1.xml") {
		t.Fatalf("part names = %#v, want sheet1", partNames)
	}
	if !containsString(partNames, "/xl/worksheets/sheet2.xml") {
		t.Fatalf("part names = %#v, want sheet2", partNames)
	}

	sheet1Bytes := zipEntryByName(t, entries, "xl/worksheets/sheet1.xml")
	sheet1XML := string(sheet1Bytes)
	if !strings.Contains(sheet1XML, "<t xml:space=\"preserve\">=1+1</t>") {
		t.Fatalf("a formula-looking value is an inline string: %s", sheet1XML)
	}
	if strings.Contains(sheet1XML, "'=") {
		t.Fatalf("no apostrophe prefix: %s", sheet1XML)
	}
	for _, character := range sheet1XML {
		if character < 0x20 && character != '\t' && character != '\n' && character != '\r' {
			t.Fatalf("control characters are dropped: %s", sheet1XML)
		}
	}
	if !strings.Contains(sheet1XML, "Head &amp; &lt;More&gt;") {
		t.Fatalf("specials are escaped: %s", sheet1XML)
	}

	var sheet1 sheetNode
	if err := xml.Unmarshal(sheet1Bytes, &sheet1); err != nil {
		t.Fatalf("parse sheet1: %v", err)
	}
	if got := parsedCellText(sheet1, "B2"); got != "abc" {
		t.Fatalf("control chars dropped: %q", got)
	}
	if got := parsedCellText(sheet1, "B3"); got != "quote \" and ' plus >" {
		t.Fatalf("escaped text = %q", got)
	}
	if got := parsedCellText(sheet1, "A3"); got != "lone\uFFFDsurrogate" {
		t.Fatalf("replacement characters stay: %q", got)
	}

	if sheet1.Pane == nil {
		t.Fatal("the frozen pane exists")
	}
	if got := sheet1.Pane.YSplit; got != "1" {
		t.Fatalf("pane ySplit = %q, want 1", got)
	}
	if got := sheet1.Pane.TopLeftCell; got != "A2" {
		t.Fatalf("pane topLeftCell = %q, want A2", got)
	}
	if got := sheet1.Pane.State; got != "frozen" {
		t.Fatalf("pane state = %q, want frozen", got)
	}
	if sheet1.AutoFilter == nil {
		t.Fatal("the auto filter exists")
	}
	if got := sheet1.AutoFilter.Reference; got != "A1:B4" {
		t.Fatalf("auto filter ref = %q, want the header plus data range", got)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestSheetNamesAreCleanedToValidUniqueNames(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "names.xlsx")

	writer := NewWriter()
	for _, name := range []string{
		"Report: Q1/2026 [final] *draft?",
		"Same Name",
		"same name",
		"This name is far too long for an Excel sheet name, it keeps going on and on",
		"   ",
	} {
		writer.Sheet(name, []float64{10.0}, false, false, func(sheet *Sheet) {
			sheet.Row(Text("a"))
		})
	}
	if err := writer.Write(file); err != nil {
		t.Fatalf("the workbook is written: %v", err)
	}

	entries := readZip(t, file)
	workbookBytes := zipEntryByName(t, entries, "xl/workbook.xml")
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbookBytes, &workbook); err != nil {
		t.Fatalf("parse workbook: %v", err)
	}
	names := make([]string, len(workbook.Sheets))
	for index, sheet := range workbook.Sheets {
		names[index] = sheet.Name
	}

	if len(names) != 5 {
		t.Fatalf("sheet name count = %d, want 5", len(names))
	}
	for _, name := range names {
		if len([]rune(name)) > 31 {
			t.Fatalf("'%s' is at most 31 characters", name)
		}
		if name == "" {
			t.Fatalf("'%s' is not empty", name)
		}
		if strings.ContainsAny(name, "[]:*?/\\") {
			t.Fatalf("'%s' has no forbidden character", name)
		}
	}
	lowered := make([]string, len(names))
	for index, name := range names {
		lowered[index] = strings.ToLower(name)
	}
	for left := 0; left < len(lowered); left++ {
		for right := left + 1; right < len(lowered); right++ {
			if lowered[left] == lowered[right] {
				t.Fatalf("unique case-insensitively: %#v", names)
			}
		}
	}
	if got := names[len(names)-1]; got != "Sheet" {
		t.Fatalf("a blank name gets the default: %q", got)
	}
}

func TestAFailedWriteLeavesNoFileAtTheTargetAndNoTempFilesBehind(t *testing.T) {
	directory := t.TempDir()

	writer := NewWriter()
	writer.Sheet("Data", []float64{10.0}, false, false, func(sheet *Sheet) {
		sheet.Row(Text("a"))
	})

	missing := filepath.Join(directory, "missing-dir")
	missingTarget := filepath.Join(missing, "out.xlsx")
	if err := writer.Write(missingTarget); err == nil {
		t.Fatal("writing into a missing directory reports an error")
	}
	if _, err := os.Stat(missingTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing at the missing target: %v", err)
	}

	failingDirectory := filepath.Join(directory, "failing")
	if err := os.Mkdir(failingDirectory, 0o700); err != nil {
		t.Fatalf("the test directory exists: %v", err)
	}
	failingTarget := filepath.Join(failingDirectory, "out.xlsx")
	err := WriteAtomically(failingTarget, func(tempPath string) error {
		if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
			return err
		}
		return errors.New("deliberate failure")
	})
	if err == nil {
		t.Fatal("the deliberate failure is reported")
	}
	if _, statErr := os.Stat(failingTarget); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("nothing at the target: %v", statErr)
	}
	entries, readErr := os.ReadDir(failingDirectory)
	if readErr != nil {
		t.Fatalf("the directory is readable: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("no temp file is left behind, found %d entries", len(entries))
	}
}

func TestASheetWhoseRowsAreAllEmptyStillGetsAValidRange(t *testing.T) {
	writer := NewWriter()
	writer.Sheet("Empty", []float64{10.0}, true, true, func(sheet *Sheet) {
		sheet.Row(Empty())
	})
	var sheetXML string
	for _, part := range writer.parts() {
		if part.name == "xl/worksheets/sheet1.xml" {
			sheetXML = part.xml
			break
		}
	}
	if sheetXML == "" {
		t.Fatal("the worksheet part exists")
	}
	if !strings.Contains(sheetXML, "<dimension ref=\"A1:A1\"/>") {
		t.Fatalf("dimension = %s", sheetXML)
	}
}

func buildReferenceWorkbook(t *testing.T) *Writer {
	t.Helper()
	zeroPointOne := 0.1
	zeroPointTwo := 0.2
	writer := NewWriter()
	writer.Sheet("Buchungen 2026", []float64{12.0, 30.5, 14.25, 8.0, zeroPointOne + zeroPointTwo, 1e7, 7.0}, true, true, func(sheet *Sheet) {
		sheet.Row(
			Bold("Datum"),
			Bold("Partner"),
			Bold("Betrag"),
			Bold("Anzahl"),
			Bold("Leer"),
			Bold("X"),
			Bold("Y"),
		)
		sheet.Row(
			Date(mustDate(t, 2026, time.October, 15)),
			Text("Bäckerei <Schmidt> & \"Söhne\" 'x'"),
			Money(eur(-123_456)),
			NumberInt(3),
			Empty(),
			Text("=SUM(A1)"),
			Text("+1"),
		)
		sheet.Row(
			Date(mustDate(t, 1900, time.March, 1)),
			Text("ctrl\u0001\u0008\u000b\u000ctab\tnl\ncr\r\uFFFE\uFFFF?end"),
			Money(eur(5)),
			Number("12.5"),
			Text("-neg"),
			Text("@at"),
			Text("  leading and trailing  "),
		)
		sheet.Row(
			Empty(),
			Empty(),
			Money(eur(0)),
		)
		sheet.Row(Text("😀 emoji 𝄞"))
	})
	writer.Sheet("Zusammenfassung", []float64{}, false, false, func(sheet *Sheet) {
		sheet.Row(Text("only"))
	})
	for _, name := range []string{
		"",
		"History",
		"history",
		"a:b\\c/d?e*f[g]h",
		"'quoted'",
		"Ein sehr langer Blattname der über einunddreißig Zeichen hinausgeht",
		"😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀",
		"Zusammenfassung",
	} {
		writer.Sheet(name, []float64{10.0}, false, false, func(sheet *Sheet) {
			sheet.Row(Text(name))
		})
	}
	return writer
}

func TestReferenceWorkbookMatchesRust(t *testing.T) {
	file := filepath.Join(t.TempDir(), "reference.xlsx")
	if err := buildReferenceWorkbook(t).Write(file); err != nil {
		t.Fatalf("write the reference workbook: %v", err)
	}
	compareWorkbooks(t, file, filepath.Join("testdata", "reference.xlsx"))
}

func TestEmptyWorkbookMatchesRust(t *testing.T) {
	file := filepath.Join(t.TempDir(), "empty.xlsx")
	writer := NewWriter()
	writer.Sheet("Empty", nil, true, true, func(*Sheet) {})
	if err := writer.Write(file); err != nil {
		t.Fatalf("write the empty workbook: %v", err)
	}
	compareWorkbooks(t, file, filepath.Join("testdata", "empty.xlsx"))
}

func compareWorkbooks(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	gotEntries := readZip(t, gotPath)
	wantEntries := readZip(t, wantPath)

	gotNames := make([]string, len(gotEntries))
	for index, entry := range gotEntries {
		gotNames[index] = entry.name
	}
	wantNames := make([]string, len(wantEntries))
	for index, entry := range wantEntries {
		wantNames[index] = entry.name
	}
	if !equalStrings(gotNames, wantNames) {
		t.Fatalf("entry names and order = %#v, want %#v", gotNames, wantNames)
	}

	for index := range wantEntries {
		part := wantEntries[index].name
		if gotEntries[index].method != zip.Deflate {
			t.Fatalf("part %s compression method = %d, want Deflate", part, gotEntries[index].method)
		}
		if got := gotEntries[index].mode.Perm(); got != 0o644 {
			t.Fatalf("part %s permissions = %04o, want 0644", part, got)
		}
		got := gotEntries[index].data
		want := wantEntries[index].data
		if bytes.Equal(got, want) {
			continue
		}
		offset := 0
		for offset < len(got) && offset < len(want) && got[offset] == want[offset] {
			offset++
		}
		gotStart := max(0, offset-20)
		gotEnd := min(len(got), offset+20)
		wantStart := max(0, offset-20)
		wantEnd := min(len(want), offset+20)
		t.Fatalf(
			"part %s differs at offset %d (got %d bytes, want %d bytes)\ngot  %q\nwant %q",
			part,
			offset,
			len(got),
			len(want),
			got[gotStart:gotEnd],
			want[wantStart:wantEnd],
		)
	}
}
