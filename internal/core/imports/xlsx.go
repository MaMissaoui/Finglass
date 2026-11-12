// Reading the first worksheet of an untrusted XLSX file as rows of cell
// strings, in memory. Ports crates/finglass-core/src/import/xlsx.rs.
package imports

import (
	"archive/zip"
	stdbytes "bytes"
	"io"
	"math"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/imports/xmltree"
)

const (
	maxEntries    = 2000
	maxTotalBytes = 60 * 1024 * 1024
	maxPartBytes  = 20 * 1024 * 1024
	maxRows       = 100_000
	maxColumns    = 200

	workbookPart      = "xl/workbook.xml"
	relationshipsPart = "xl/_rels/workbook.xml.rels"
	sharedStringsPart = "xl/sharedStrings.xml"
	stylesPart        = "xl/styles.xml"
)

// FirstSheetRows returns the first worksheet, row by row.
func FirstSheetRows(bytes []byte) ([][]string, error) {
	parts, err := readParts(bytes)
	if err != nil {
		return nil, err
	}

	workbookBytes, err := requiredPart(parts, workbookPart)
	if err != nil {
		return nil, err
	}
	relsBytes, err := requiredPart(parts, relationshipsPart)
	if err != nil {
		return nil, err
	}
	if err := checkPartSize(workbookBytes); err != nil {
		return nil, err
	}
	if err := checkPartSize(relsBytes); err != nil {
		return nil, err
	}

	workbookText, err := partText(workbookBytes)
	if err != nil {
		return nil, err
	}
	relsText, err := partText(relsBytes)
	if err != nil {
		return nil, err
	}

	workbookDocument, err := xmltree.Parse(workbookText)
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if err := ensureDepth(workbookDocument); err != nil {
		return nil, err
	}
	workbook := workbookDocument.RootElement()
	sheets := descendants(workbook, "sheet")
	if len(sheets) == 0 {
		return nil, ErrXlsxUnreadable
	}
	sheetRef, ok := relationshipID(sheets[0])
	if !ok {
		return nil, ErrXlsxUnreadable
	}

	relsDocument, err := xmltree.Parse(relsText)
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if err := ensureDepth(relsDocument); err != nil {
		return nil, err
	}
	relsRoot := relsDocument.RootElement()
	target := ""
	foundTarget := false
	for _, relationship := range children(relsRoot, "Relationship") {
		if id, ok := relationship.Attribute("Id"); !ok || id != sheetRef {
			continue
		}
		value, _ := relationship.Attribute("Target")
		if !strings.HasPrefix(value, "/") || strings.TrimLeft(value, "/") != "" {
			target = value
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		return nil, ErrXlsxUnreadable
	}
	sheetName := resolveTargetPath(target)

	date1904 := false
	for _, properties := range children(workbook, "workbookPr") {
		if value, ok := properties.Attribute("date1904"); ok && asciiFoldEqual(value, "true") {
			date1904 = true
		}
	}

	sheetBytes, err := requiredPart(parts, sheetName)
	if err != nil {
		return nil, err
	}
	if err := checkPartSize(sheetBytes); err != nil {
		return nil, err
	}
	var sharedStrings []string
	sharedBytes, found, err := selectedPart(parts, sharedStringsPart)
	if err != nil {
		return nil, err
	}
	if found {
		if err := checkPartSize(sharedBytes); err != nil {
			return nil, err
		}
		sharedStrings, err = parseSharedStrings(sharedBytes)
		if err != nil {
			return nil, err
		}
	}
	var dateStyles map[int]bool
	styleBytes, found, err := selectedPart(parts, stylesPart)
	if err != nil {
		return nil, err
	}
	if found {
		if err := checkPartSize(styleBytes); err != nil {
			return nil, err
		}
		dateStyles, err = parseDateStyles(styleBytes)
		if err != nil {
			return nil, err
		}
	}

	sheetText, err := partText(sheetBytes)
	if err != nil {
		return nil, err
	}
	sheetDocument, err := xmltree.Parse(sheetText)
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if err := ensureDepth(sheetDocument); err != nil {
		return nil, err
	}
	return sheetRows(sheetDocument.RootElement(), sharedStrings, dateStyles, date1904)
}

type xlsxPart struct {
	name    string
	content []byte
}

func readParts(bytes []byte) ([]xlsxPart, error) {
	archive, err := zip.NewReader(stdbytes.NewReader(bytes), int64(len(bytes)))
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if len(archive.File) > maxEntries {
		return nil, ErrXlsxTooLarge
	}
	parts := []xlsxPart{}
	total := uint64(0)
	for _, entry := range archive.File {
		name := zipEntryName(entry)
		reader, err := entry.Open()
		if err != nil {
			return nil, ErrXlsxUnreadable
		}
		isDir := zipEntryIsDir(entry)
		content := []byte{}
		buffer := make([]byte, 64*1024)
		for {
			read, readErr := reader.Read(buffer)
			if read > 0 {
				total += uint64(read)
				if total > maxTotalBytes {
					_ = reader.Close()
					return nil, ErrXlsxTooLarge
				}
				if !isDir {
					content = append(content, buffer[:read]...)
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = reader.Close()
				return nil, ErrXlsxUnreadable
			}
		}
		if err := reader.Close(); err != nil {
			return nil, ErrXlsxUnreadable
		}
		if !isDir {
			parts = append(parts, xlsxPart{name: name, content: content})
		}
	}
	return parts, nil
}

func selectedPart(parts []xlsxPart, name string) ([]byte, bool, error) {
	var matching [][]byte
	for _, part := range parts {
		if part.name == name {
			matching = append(matching, part.content)
		}
	}
	if len(matching) > 1 {
		return nil, false, ErrXlsxUnreadable
	}
	if len(matching) == 0 {
		return nil, false, nil
	}
	return matching[0], true, nil
}

func requiredPart(parts []xlsxPart, name string) ([]byte, error) {
	content, found, err := selectedPart(parts, name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrXlsxUnreadable
	}
	return content, nil
}

func checkPartSize(bytes []byte) error {
	if len(bytes) > maxPartBytes {
		return ErrXlsxTooLarge
	}
	return nil
}

func partText(bytes []byte) (string, error) {
	text := decodeXML(bytes)
	if strings.Contains(text, "<!DOCTYPE") {
		return "", ErrXlsxUnreadable
	}
	return text, nil
}

func ensureDepth(document *xmltree.Document) error {
	for _, node := range document.RootElement().Descendants() {
		if !node.IsElement() {
			continue
		}
		depth := 0
		for _, ancestor := range node.Ancestors() {
			if ancestor.IsElement() {
				depth++
			}
		}
		if depth > 64 {
			return ErrXlsxUnreadable
		}
	}
	return nil
}

func relationshipID(node *xmltree.Node) (string, bool) {
	for _, attribute := range node.Attributes() {
		if attribute.Local == "id" {
			return attribute.Value, true
		}
	}
	return "", false
}

func resolveTargetPath(target string) string {
	if strings.HasPrefix(target, "/") {
		return resolveSegments(target[1:])
	}
	return resolveSegments("xl/" + target)
}

func resolveSegments(path string) string {
	parts := []string{}
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(parts) > 0 && parts[len(parts)-1] != ".." {
				parts = parts[:len(parts)-1]
			} else {
				parts = append(parts, "..")
			}
		default:
			parts = append(parts, segment)
		}
	}
	return strings.Join(parts, "/")
}

func parseSharedStrings(bytes []byte) ([]string, error) {
	text, err := partText(bytes)
	if err != nil {
		return nil, err
	}
	document, err := xmltree.Parse(text)
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if err := ensureDepth(document); err != nil {
		return nil, err
	}
	root := document.RootElement()
	result := []string{}
	for _, si := range children(root, "si") {
		values := []string{}
		for _, plain := range children(si, "t") {
			values = append(values, textContent(plain))
		}
		for _, run := range children(si, "r") {
			for _, textNode := range children(run, "t") {
				values = append(values, textContent(textNode))
			}
		}
		result = append(result, strings.Join(values, ""))
	}
	return result, nil
}

func parseDateStyles(bytes []byte) (map[int]bool, error) {
	text, err := partText(bytes)
	if err != nil {
		return nil, err
	}
	document, err := xmltree.Parse(text)
	if err != nil {
		return nil, ErrXlsxUnreadable
	}
	if err := ensureDepth(document); err != nil {
		return nil, err
	}
	root := document.RootElement()
	customDates := map[int]bool{}
	for _, format := range descendants(root, "numFmt") {
		idText, ok := format.Attribute("numFmtId")
		if !ok {
			continue
		}
		id, ok := parseUsize(idText)
		if !ok {
			continue
		}
		code, _ := format.Attribute("formatCode")
		customDates[id] = isDateFormatCode(code)
	}
	result := map[int]bool{}
	cellXfs := children(root, "cellXfs")
	if len(cellXfs) == 0 {
		return result, nil
	}
	for index, xf := range children(cellXfs[0], "xf") {
		idText, ok := xf.Attribute("numFmtId")
		if !ok {
			continue
		}
		id, ok := parseUsize(idText)
		if !ok {
			continue
		}
		result[index] = (id >= 14 && id <= 22) || (id >= 45 && id <= 47) || customDates[id]
	}
	return result, nil
}

func isDateFormatCode(code string) bool {
	var cleaned strings.Builder
	characters := []rune(code)
	index := 0
	for index < len(characters) {
		character := characters[index]
		index++
		switch character {
		case '"':
			for index < len(characters) {
				inner := characters[index]
				index++
				if inner == '"' {
					break
				}
			}
		case '[':
			for index < len(characters) {
				inner := characters[index]
				index++
				if inner == ']' {
					break
				}
			}
		default:
			cleaned.WriteRune(character)
		}
	}
	lowered := lowerText.String(cleaned.String())
	return strings.Contains(lowered, "d") || strings.Contains(lowered, "y")
}

func sheetRows(sheet *xmltree.Node, sharedStrings []string, dateStyles map[int]bool, date1904 bool) ([][]string, error) {
	result := [][]string{}
	for _, row := range descendants(sheet, "row") {
		index := len(result)
		if valueText, ok := row.Attribute("r"); ok {
			if value, parsed := parseI64(valueText); parsed {
				indexValue := value - 1
				if value == math.MinInt64 {
					return nil, ErrXlsxTooLarge
				}
				if indexValue < 0 || indexValue >= maxRows {
					return nil, ErrXlsxTooLarge
				}
				index = int(indexValue)
			}
		}
		cells := []string{}
		width := 0
		for _, cell := range children(row, "c") {
			explicit := int64(-1)
			if reference, ok := cell.Attribute("r"); ok {
				explicit = columnIndex(reference)
			}
			column := width
			if explicit >= 0 {
				column = int(explicit)
			}
			if column >= maxColumns {
				return nil, ErrXlsxTooLarge
			}
			for len(cells) <= column {
				cells = append(cells, "")
			}
			cells[column] = cellValue(cell, sharedStrings, dateStyles, date1904)
			width = column + 1
		}
		for len(result) < index {
			result = append(result, []string{})
		}
		if len(result) == index {
			result = append(result, cells)
		} else {
			result[index] = cells
		}
	}
	return result, nil
}

func cellValue(cell *xmltree.Node, sharedStrings []string, dateStyles map[int]bool, date1904 bool) string {
	value := ""
	if node := child(cell, "v"); node != nil {
		value = textContent(node)
	}
	cellType, _ := cell.Attribute("t")
	switch cellType {
	case "s":
		index, ok := parseUsize(strings.TrimSpace(value))
		if !ok || index >= len(sharedStrings) {
			return ""
		}
		return sharedStrings[index]
	case "inlineStr":
		inline := child(cell, "is")
		if inline == nil {
			return ""
		}
		values := []string{}
		for _, textNode := range children(inline, "t") {
			values = append(values, textContent(textNode))
		}
		for _, run := range children(inline, "r") {
			for _, textNode := range children(run, "t") {
				values = append(values, textContent(textNode))
			}
		}
		return strings.Join(values, "")
	case "str":
		return value
	case "b":
		trimmed := strings.TrimSpace(value)
		if trimmed == "1" || asciiFoldEqual(trimmed, "true") {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		return ""
	default:
		text := strings.TrimSpace(value)
		if !isDecimalSyntax(text) {
			return ""
		}
		number, ok := parseDecimal(text)
		if !ok {
			return text
		}
		style := 0
		if styleText, found := cell.Attribute("s"); found {
			if parsed, valid := parseUsize(styleText); valid {
				style = parsed
			}
		}
		if dateStyles[style] {
			if date, ok := xlsxDateOf(number, date1904); ok {
				return date
			}
			return ""
		}
		return number.Normalize().String()
	}
}

func xlsxDateOf(serial decimal.Decimal, date1904 bool) (string, bool) {
	maximum := decimal.New(2_958_465, 0)
	if serial.Cmp(decimal.Zero) < 0 || serial.Cmp(maximum) > 0 {
		return "", false
	}
	days, ok := serial.Trunc().Int64()
	if !ok {
		return "", false
	}
	var base civil.Date
	if date1904 {
		base, ok = civil.New(1904, 1, 1)
	} else {
		base, ok = civil.New(1899, 12, 30)
	}
	if !ok {
		return "", false
	}
	result := base.AddDays(int(days))
	if result.Year() < 0 || result.Year() > 9999 {
		return "", false
	}
	return formatISODate(result), true
}

func columnIndex(reference string) int64 {
	index := int64(0)
	for _, character := range reference {
		upper := character
		if upper >= 'a' && upper <= 'z' {
			upper -= 'a' - 'A'
		}
		if upper < 'A' || upper > 'Z' {
			break
		}
		if index > (math.MaxInt64-26)/26 {
			index = math.MaxInt64
		} else {
			index *= 26
		}
		value := int64(upper-'A') + 1
		if index > math.MaxInt64-value {
			index = math.MaxInt64
		} else {
			index += value
		}
	}
	return index - 1
}
