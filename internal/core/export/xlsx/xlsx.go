// Package xlsx writes minimal SpreadsheetML (`.xlsx`) workbooks on
// archive/zip and hand-written XML, with no third-party workbook library.
// Cell kinds: text and bold text as inline strings, plain numbers, money with
// `#,##0.00` and dates as Excel serial numbers with `dd.mm.yyyy`. There are
// deliberately no formulas (`<f>`).
//
// A text value starting with `=`, `+`, `-` or `@` stays a plain inline string;
// XML is escaped and stripped of characters invalid in XML 1.0, since bank text
// can contain them.
package xlsx

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

const xmlDeclaration = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n"

const stylesBody = `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <numFmts count="2"><numFmt numFmtId="164" formatCode="#,##0.00"/><numFmt numFmtId="165" formatCode="dd.mm.yyyy"/></numFmts>
 <fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
 <fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
 <borders count="1"><border/></borders>
 <cellStyleXfs count="1"><xf numFmtId="0" fontId="0"/></cellStyleXfs>
 <cellXfs count="4">
  <xf numFmtId="0" fontId="0" xfId="0"/>
  <xf numFmtId="0" fontId="1" applyFont="1" xfId="0"/>
  <xf numFmtId="164" fontId="0" applyNumberFormat="1" xfId="0"/>
  <xf numFmtId="165" fontId="0" applyNumberFormat="1" xfId="0"/>
 </cellXfs>
</styleSheet>`

const rootRels = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\"><Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"xl/workbook.xml\"/></Relationships>"

const (
	maxSheetNameLength = 31
	defaultSheetName   = "Sheet"
	uniqueSuffixRoom   = 5
)

var tempCounter atomic.Uint64

// WriteAtomically writes a file through a temporary file next to the target
// and moves the finished file into place, so a failed export never leaves half
// a file at the target. On Unix the finished file has mode 0600.
func WriteAtomically(target string, write func(tempPath string) error) error {
	parent := tempParent(target)
	if parent == "" {
		return fmt.Errorf("the export target needs a parent directory: %s", target)
	}

	tempPath, tempFile, err := createTempFile(parent)
	if err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	armed := true
	defer func() {
		if armed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := write(tempPath); err != nil {
		return err
	}

	if err := os.Chmod(tempPath, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	armed = false
	return nil
}

// tempParent mirrors Rust's target.parent(): a bare file name has the empty
// parent, which becomes ".", while an empty target has no parent at all.
func tempParent(target string) string {
	if target == "" {
		return ""
	}
	parent := filepath.Dir(target)
	if parent == "" {
		return "."
	}
	return parent
}

func createTempFile(parent string) (string, *os.File, error) {
	for attempt := 0; attempt < 1_000; attempt++ {
		number := tempCounter.Add(1) - 1
		path := filepath.Join(parent, fmt.Sprintf("findesk-export-%d-%d.tmp", os.Getpid(), number))
		// #nosec G302 G304 -- Rust creates the temp file with OpenOptions' default
		// mode and narrows it to 0600 after writing; path is built here from parent.
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return path, file, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return "", nil, err
	}

	return "", nil, fmt.Errorf("could not create a temporary export file: %w", fs.ErrExist)
}

// Writer is a workbook under construction.
type Writer struct {
	sheets []*Sheet
}

// NewWriter opens an empty workbook.
func NewWriter() *Writer {
	return &Writer{}
}

// Sheet adds one worksheet. columns holds the columns' display widths;
// freezeHeader freezes row 1; autoFilter puts a filter over the header and
// data range.
func (w *Writer) Sheet(name string, columns []float64, freezeHeader, autoFilter bool, content func(*Sheet)) {
	sheet := &Sheet{
		rawName:      name,
		columns:      columns,
		freezeHeader: freezeHeader,
		autoFilter:   autoFilter,
		rows:         nil,
	}
	content(sheet)
	w.sheets = append(w.sheets, sheet)
}

// Write writes the workbook to target through a temp file next to it, then
// moves it into place.
func (w *Writer) Write(target string) error {
	parts := w.parts()
	return WriteAtomically(target, func(tempPath string) error {
		file, err := os.Create(tempPath) // #nosec G304 -- tempPath was created by this package next to target.
		if err != nil {
			return err
		}
		archive := zip.NewWriter(file)
		for _, part := range parts {
			header := &zip.FileHeader{Name: part.name, Method: zip.Deflate}
			header.SetMode(0o644)
			entry, err := archive.CreateHeader(header)
			if err != nil {
				_ = file.Close()
				return err
			}
			if _, err := entry.Write([]byte(part.xml)); err != nil {
				_ = file.Close()
				return err
			}
		}
		if err := archive.Close(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
}

type packagePart struct {
	name string
	xml  string
}

// parts returns every package part in write order: the content types, the two
// relationship parts, the workbook, the styles and one worksheet per sheet.
func (w *Writer) parts() []packagePart {
	names := w.cleanedSheetNames()
	parts := make([]packagePart, 0, 5+len(w.sheets))
	parts = append(parts, packagePart{"[Content_Types].xml", contentTypesXML(names)})
	parts = append(parts, packagePart{"_rels/.rels", rootRels})
	parts = append(parts, packagePart{"xl/workbook.xml", workbookXML(names)})
	parts = append(parts, packagePart{"xl/_rels/workbook.xml.rels", workbookRelsXML(len(names))})
	parts = append(parts, packagePart{"xl/styles.xml", stylesXML()})
	for index, sheet := range w.sheets {
		parts = append(parts, packagePart{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", index+1),
			sheetXML(sheet),
		})
	}
	return parts
}

// cleanedSheetNames returns the sheets' names cleaned to valid, unique
// (case-insensitive) Excel sheet names, in order.
func (w *Writer) cleanedSheetNames() []string {
	taken := make([]string, 0, len(w.sheets))
	names := make([]string, 0, len(w.sheets))
	for _, sheet := range w.sheets {
		names = append(names, cleanSheetName(sheet.rawName, &taken))
	}
	return names
}

// Sheet is one worksheet: its name as given (cleaned when written), its
// columns' widths and its rows.
type Sheet struct {
	rawName      string
	columns      []float64
	freezeHeader bool
	autoFilter   bool
	rows         [][]cellValue
}

// Row appends one row; place Cell.Empty where a cell should be left blank. It
// accepts either individual cells or one []Cell, mirroring Rust's Vec<Cell>.
func (s *Sheet) Row(cells ...Cell) {
	row := make([]cellValue, 0, len(cells))
	for _, cell := range cells {
		switch value := cell.(type) {
		case nil:
		case cellValue:
			row = append(row, value)
		case []Cell:
			for _, nested := range value {
				switch nestedValue := nested.(type) {
				case nil:
				case cellValue:
					row = append(row, nestedValue)
				default:
					panic("xlsx: Row takes cells or one slice of cells")
				}
			}
		default:
			panic("xlsx: Row takes cells or one slice of cells")
		}
	}
	s.rows = append(s.rows, row)
}

// Cell is an opaque cell value, created only through the factory functions.
// It also lets Row accept a whole []Cell without expansion.
type Cell any

type cellValue struct {
	kind  cellKind
	value string
}

type cellKind int

const (
	cellEmpty cellKind = iota
	cellText
	cellBold
	cellNumber
	cellMoney
	cellDate
)

// Text is a plain inline string. Text that looks like a formula stays text.
func Text(value string) Cell {
	return cellValue{kind: cellText, value: value}
}

// Bold is a bold inline string.
func Bold(value string) Cell {
	return cellValue{kind: cellBold, value: value}
}

// Number is a plain number, written exactly as its text form.
func Number(value string) Cell {
	return cellValue{kind: cellNumber, value: value}
}

// NumberInt is a plain integer number, written exactly as its text form.
func NumberInt(value int64) Cell {
	return cellValue{kind: cellNumber, value: strconv.FormatInt(value, 10)}
}

// Money is a money amount, written from integer minor units with the
// currency's exponent.
func Money(value money.Money) Cell {
	return cellValue{kind: cellMoney, value: money.FormatPlain(value)}
}

// Date is a date, written as an Excel serial number (days since 1899-12-30).
func Date(value civil.Date) Cell {
	return cellValue{kind: cellDate, value: strconv.FormatInt(excelSerial(value), 10)}
}

// Empty is an empty cell; used to leave a gap in a row.
func Empty() Cell {
	return cellValue{kind: cellEmpty, value: ""}
}

func excelSerial(value civil.Date) int64 {
	base := time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)
	date := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return date.Unix()/86_400 - base.Unix()/86_400
}

func contentTypesXML(sheetNames []string) string {
	var xml strings.Builder
	xml.WriteString(xmlDeclaration)
	xml.WriteString("<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">")
	xml.WriteString("<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>")
	xml.WriteString("<Default Extension=\"xml\" ContentType=\"application/xml\"/>")
	xml.WriteString("<Override PartName=\"/xl/workbook.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml\"/>")
	for index := range sheetNames {
		fmt.Fprintf(&xml, "<Override PartName=\"/xl/worksheets/sheet%d.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml\"/>", index+1)
	}
	xml.WriteString("<Override PartName=\"/xl/styles.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml\"/>")
	xml.WriteString("</Types>")
	return xml.String()
}

func workbookXML(sheetNames []string) string {
	var xml strings.Builder
	xml.WriteString(xmlDeclaration)
	xml.WriteString("<workbook xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"")
	xml.WriteString(" xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\">")
	xml.WriteString("<sheets>")
	for index, name := range sheetNames {
		fmt.Fprintf(&xml, "<sheet name=\"%s\" sheetId=\"%d\" r:id=\"rId%d\"/>", escapeXML(name), index+1, index+1)
	}
	xml.WriteString("</sheets></workbook>")
	return xml.String()
}

func workbookRelsXML(sheetCount int) string {
	var xml strings.Builder
	xml.WriteString(xmlDeclaration)
	xml.WriteString("<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">")
	for index := 0; index < sheetCount; index++ {
		fmt.Fprintf(&xml, "<Relationship Id=\"rId%d\"", index+1)
		xml.WriteString(" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\"")
		fmt.Fprintf(&xml, " Target=\"worksheets/sheet%d.xml\"/>", index+1)
	}
	fmt.Fprintf(&xml, "<Relationship Id=\"rId%d\"", sheetCount+1)
	xml.WriteString(" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles\"")
	xml.WriteString(" Target=\"styles.xml\"/>")
	xml.WriteString("</Relationships>")
	return xml.String()
}

func stylesXML() string {
	return xmlDeclaration + stylesBody
}

func sheetXML(sheet *Sheet) string {
	// At least column A, so a sheet whose rows are all empty still gets a valid
	// range.
	lastColumn := 1
	for _, row := range sheet.rows {
		last := 0
		for index := len(row) - 1; index >= 0; index-- {
			if row[index].kind != cellEmpty {
				last = index + 1
				break
			}
		}
		if last > lastColumn {
			lastColumn = last
		}
	}
	lastRow := len(sheet.rows)
	sheetRange := "A1"
	if len(sheet.rows) != 0 {
		sheetRange = fmt.Sprintf("A1:%s%d", columnLetter(lastColumn-1), lastRow)
	}

	var xml strings.Builder
	xml.WriteString(xmlDeclaration)
	xml.WriteString("<worksheet xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"")
	xml.WriteString(" xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\">")
	fmt.Fprintf(&xml, "<dimension ref=\"%s\"/>", sheetRange)
	xml.WriteString("<sheetViews><sheetView workbookViewId=\"0\">")
	if sheet.freezeHeader && len(sheet.rows) != 0 {
		xml.WriteString("<pane ySplit=\"1\" topLeftCell=\"A2\" activePane=\"bottomLeft\" state=\"frozen\"/>")
	}
	xml.WriteString("</sheetView></sheetViews>")
	if len(sheet.columns) != 0 {
		xml.WriteString("<cols>")
		for index, width := range sheet.columns {
			fmt.Fprintf(&xml, "<col min=\"%d\" max=\"%d\" width=\"%s\" customWidth=\"1\"/>", index+1, index+1, formatWidth(width))
		}
		xml.WriteString("</cols>")
	}
	xml.WriteString("<sheetData>")
	for rowIndex, cells := range sheet.rows {
		fmt.Fprintf(&xml, "<row r=\"%d\">", rowIndex+1)
		for columnIndex, cell := range cells {
			xml.WriteString(cellXML(cell, rowIndex+1, columnIndex))
		}
		xml.WriteString("</row>")
	}
	xml.WriteString("</sheetData>")
	if sheet.autoFilter && len(sheet.rows) != 0 {
		fmt.Fprintf(&xml, "<autoFilter ref=\"%s\"/>", sheetRange)
	}
	xml.WriteString("</worksheet>")
	return xml.String()
}

func cellXML(cell cellValue, row, column int) string {
	if cell.kind == cellEmpty {
		return ""
	}

	reference := fmt.Sprintf("%s%d", columnLetter(column), row)
	switch cell.kind {
	case cellText:
		return inlineString(reference, 0, cell.value)
	case cellBold:
		return inlineString(reference, 1, cell.value)
	case cellNumber:
		return fmt.Sprintf("<c r=\"%s\"><v>%s</v></c>", reference, cell.value)
	case cellMoney:
		return fmt.Sprintf("<c r=\"%s\" s=\"2\"><v>%s</v></c>", reference, cell.value)
	case cellDate:
		return fmt.Sprintf("<c r=\"%s\" s=\"3\"><v>%s</v></c>", reference, cell.value)
	default:
		return ""
	}
}

func inlineString(reference string, style int, value string) string {
	styleAttribute := ""
	if style != 0 {
		styleAttribute = fmt.Sprintf(" s=\"%d\"", style)
	}
	return fmt.Sprintf(
		"<c r=\"%s\"%s t=\"inlineStr\"><is><t xml:space=\"preserve\">%s</t></is></c>",
		reference,
		styleAttribute,
		escapeXML(value),
	)
}

// formatWidth formats a column width the way Kotlin's Double.toString normally
// does for such values: whole numbers keep a trailing `.0`, other ordinary
// values use their shortest decimal form.
func formatWidth(width float64) string {
	if !math.IsNaN(width) && !math.IsInf(width, 0) && math.Trunc(width) == width && math.Abs(width) < 10_000_000 {
		return strconv.FormatFloat(width, 'f', 1, 64)
	}
	return strconv.FormatFloat(width, 'f', -1, 64)
}

// cleanSheetName cleans raw into a valid sheet name and, taken one, a unique
// (case-insensitive) variant.
func cleanSheetName(raw string, taken *[]string) string {
	var replaced strings.Builder
	for _, character := range raw {
		if strings.ContainsRune("[]:*?/\\", character) {
			replaced.WriteRune('_')
		} else {
			replaced.WriteRune(character)
		}
	}
	trimmed := strings.TrimSpace(replaced.String())
	base := takeUTF16Units(trimmed, maxSheetNameLength)
	if base == "" {
		base = defaultSheetName
	}

	found := false
	for _, name := range *taken {
		if sameName(name, base) {
			found = true
			break
		}
	}
	if !found {
		*taken = append(*taken, base)
		return base
	}

	prefix := takeUTF16Units(base, maxSheetNameLength-uniqueSuffixRoom)
	for number := 2; ; number++ {
		candidate := fmt.Sprintf("%s-%d", prefix, number)
		found = false
		for _, name := range *taken {
			if sameName(name, candidate) {
				found = true
				break
			}
		}
		if !found {
			*taken = append(*taken, candidate)
			return candidate
		}
	}
}

// takeUTF16Units mirrors Kotlin's String.take(n): it counts UTF-16 code units
// and keeps whole characters up to that limit.
func takeUTF16Units(value string, limit int) string {
	var result strings.Builder
	used := 0
	for _, character := range value {
		width := utf16.RuneLen(character)
		if width < 0 {
			width = 1
		}
		if used+width > limit {
			break
		}
		result.WriteRune(character)
		used += width
	}
	return result.String()
}

func sameName(left, right string) bool {
	//nolint:staticcheck // same_name compares Rust's to_lowercase results, not EqualFold's folding.
	return strings.ToLower(left) == strings.ToLower(right)
}

// escapeXML escapes the five XML special characters and drops what XML 1.0
// cannot carry: control characters other than tab, LF and CR, lone surrogates
// and U+FFFE/U+FFFF.
func escapeXML(value string) string {
	var escaped strings.Builder
	escaped.Grow(len(value))
	for _, character := range value {
		switch character {
		case '&':
			escaped.WriteString("&amp;")
		case '<':
			escaped.WriteString("&lt;")
		case '>':
			escaped.WriteString("&gt;")
		case '"':
			escaped.WriteString("&quot;")
		case '\'':
			escaped.WriteString("&apos;")
		case '\t', '\n', '\r':
			escaped.WriteRune(character)
		case '\uFFFE', '\uFFFF':
		default:
			if character >= 0x20 {
				escaped.WriteRune(character)
			}
		}
	}
	return escaped.String()
}

// columnLetter returns "A", …, "Z", "AA", … for a 0-based column index.
func columnLetter(index int) string {
	remaining := index
	var letters []byte
	for {
		letters = append(letters, byte('A'+remaining%26))
		if remaining < 26 {
			break
		}
		remaining = remaining/26 - 1
	}
	for left, right := 0, len(letters)-1; left < right; left, right = left+1, right-1 {
		letters[left], letters[right] = letters[right], letters[left]
	}
	return string(letters)
}
