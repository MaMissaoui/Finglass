// Package banklist reads the DK FinTS bank list the user loads.
//
// Finglass ships no copy of the list and passes none on. The user chooses the
// CSV they received from the DK; this package keeps only the bank code, the
// server URL and the institute name. Nothing here decides whether a bank
// supports FinTS or which TAN methods it offers: the bank's parameter data
// (BPD) does that.
package banklist

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/privatefs"
)

// FileName is the bank list file in the data root.
const FileName = "bank-servers.json"

// MaxBytes is the largest bank list file Parse reads: 10 MB.
const MaxBytes = 10 << 20

// ErrTooLarge means the file is larger than MaxBytes.
var ErrTooLarge = errors.New("bank list file is larger than 10 MB")

// ErrUseCSV means the input is a binary file, usually the .xls version of the
// DK list, not its CSV.
var ErrUseCSV = errors.New("choose the .csv version of the DK list")

// MissingColumnError means the header has no column this package needs.
type MissingColumnError struct {
	// Column is the missing column: "BLZ" or "URL".
	Column string
}

// Error implements error.
func (e *MissingColumnError) Error() string {
	return fmt.Sprintf("bank list has no %s column", e.Column)
}

// Entry is one bank's server address and name.
type Entry struct {
	// URL is the bank's FinTS server address. It is always an https URL.
	URL string `json:"url"`
	// Name is the institute name. It may be empty.
	Name string `json:"name"`
}

// List maps a bank code to its server address and name.
type List map[string]Entry

// Lookup returns the entry for blz, ignoring surrounding whitespace.
func (l List) Lookup(blz string) (Entry, bool) {
	entry, ok := l[strings.TrimSpace(blz)]
	return entry, ok
}

// Report says how many rows loaded and why the others were skipped.
type Report struct {
	// Loaded is the number of entries in the list.
	Loaded int
	// Skipped counts skipped rows by reason.
	Skipped map[string]int
}

// SkippedTotal returns the number of skipped rows.
func (r Report) SkippedTotal() int {
	total := 0
	for _, count := range r.Skipped {
		total += count
	}
	return total
}

// Parse reads the DK list's CSV.
//
// The header may be preceded by a few metadata lines. Only the BLZ, URL and
// institute-name columns are read; every other column is dropped.
func Parse(data []byte) (List, Report, error) {
	list := make(List)
	report := Report{Skipped: make(map[string]int)}
	if len(data) > MaxBytes {
		return nil, report, ErrTooLarge
	}
	if hasBinaryMagic(data) {
		return nil, report, ErrUseCSV
	}

	decoded, err := decode(data)
	if err != nil {
		return nil, report, err
	}
	records, err := readCSV(decoded)
	if err != nil {
		return nil, report, err
	}

	headerIndex, columns := findHeader(records)
	if columns == nil || columns.blz < 0 {
		return nil, report, &MissingColumnError{Column: "BLZ"}
	}
	if columns.url < 0 {
		return nil, report, &MissingColumnError{Column: "URL"}
	}

	for _, row := range records[headerIndex+1:] {
		blz := strings.TrimSpace(field(row, columns.blz))
		if !isEightDigits(blz) {
			report.Skipped["blz"]++
			continue
		}
		if _, exists := list[blz]; exists {
			report.Skipped["duplicate"]++
			continue
		}

		rawURL := strings.TrimSpace(field(row, columns.url))
		reason := urlSkipReason(rawURL)
		if reason != "" {
			report.Skipped[reason]++
			continue
		}

		list[blz] = Entry{
			URL:  rawURL,
			Name: strings.TrimSpace(field(row, columns.name)),
		}
	}
	report.Loaded = len(list)
	return list, report, nil
}

// urlSkipReason returns why rawURL is not a usable https URL, or "" when it is.
func urlSkipReason(rawURL string) string {
	if rawURL == "" {
		return "empty"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "invalid"
	}
	if parsed.Scheme == "" {
		return "no scheme"
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "not https"
	}
	if parsed.Host == "" {
		return "invalid"
	}
	return ""
}

// hasBinaryMagic reports whether data starts with an OLE (Excel .xls) or ZIP
// signature.
func hasBinaryMagic(data []byte) bool {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0xD0, 0xCF, 0x11, 0xE0}) {
		return true
	}
	return len(data) >= 2 && bytes.Equal(data[:2], []byte{'P', 'K'})
}

// decode strips a UTF-8 BOM and decodes Windows-1252 only when the bytes are not
// valid UTF-8.
func decode(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data), nil
	}
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("decode bank list: %w", err)
	}
	return string(decoded), nil
}

// readCSV reads semicolon-separated records with the DK list's loose quoting.
func readCSV(text string) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = ';'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read bank list CSV: %w", err)
	}
	return records, nil
}

type headerColumns struct {
	blz  int
	url  int
	name int
}

// findHeader returns the first record within the first 10 that has both the BLZ
// and URL columns, plus the column indexes. headerIndex is -1 when no record
// has the BLZ column at all.
func findHeader(records [][]string) (int, *headerColumns) {
	limit := len(records)
	if limit > 10 {
		limit = 10
	}
	foundBLZ := -1
	for index := 0; index < limit; index++ {
		columns := headerFrom(records[index])
		if columns.blz >= 0 && foundBLZ < 0 {
			foundBLZ = index
		}
		if columns.blz >= 0 && columns.url >= 0 {
			return index, &columns
		}
	}
	if foundBLZ >= 0 {
		columns := headerFrom(records[foundBLZ])
		return foundBLZ, &columns
	}
	return -1, nil
}

func headerFrom(row []string) headerColumns {
	columns := headerColumns{blz: -1, url: -1, name: -1}
	for index, value := range row {
		name := normalizeHeader(value)
		switch {
		case name == "blz" || name == "bankleitzahl":
			if columns.blz < 0 {
				columns.blz = index
			}
		case isURLHeader(name):
			if columns.url < 0 {
				columns.url = index
			}
		case name == "institut" || name == "name" || name == "bankname" || name == "kreditinstitut":
			if columns.name < 0 {
				columns.name = index
			}
		}
	}
	return columns
}

// isURLHeader recognises the DK list's FinTS-server column. It names both PIN
// and TAN, plus URL, Zugang (access) or Adresse, or is simply "url".
func isURLHeader(name string) bool {
	if name == "url" {
		return true
	}
	return strings.Contains(name, "pin") &&
		strings.Contains(name, "tan") &&
		(strings.Contains(name, "url") ||
			strings.Contains(name, "zugang") ||
			strings.Contains(name, "adresse"))
}

// normalizeHeader trims, lower-cases and turns whitespace, '-', '_' and '/' into
// one space.
func normalizeHeader(name string) string {
	var builder strings.Builder
	pendingSpace := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsSpace(character) || character == '-' || character == '_' || character == '/' {
			pendingSpace = true
			continue
		}
		if pendingSpace && builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		pendingSpace = false
		builder.WriteRune(character)
	}
	return builder.String()
}

func field(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func isEightDigits(text string) bool {
	if len(text) != 8 {
		return false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

type storedFile struct {
	Version  int                  `json:"version"`
	LoadedOn string               `json:"loadedOn"`
	Entries  map[string]storedRow `json:"entries"`
}

type storedRow struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// Save writes list to <root>/bank-servers.json atomically.
func Save(root string, list List, loadedOn civil.Date) error {
	if err := paths.EnsureDir(root); err != nil {
		return fmt.Errorf("create data folder: %w", err)
	}

	file := storedFile{
		Version:  1,
		LoadedOn: loadedOn.String(),
		Entries:  make(map[string]storedRow, len(list)),
	}
	for blz, entry := range list {
		file.Entries[blz] = storedRow(entry)
	}
	data, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode bank list: %w", err)
	}

	temp, err := os.CreateTemp(root, "bank-servers-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary bank list: %w", err)
	}
	tempPath := temp.Name()
	armed := true
	defer func() {
		if armed {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write bank list: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync bank list: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close bank list: %w", err)
	}
	if err := privatefs.SecureFile(tempPath); err != nil && !privatefs.Unsupported(err) {
		return fmt.Errorf("secure bank list: %w", err)
	}
	if err := os.Rename(tempPath, filepath.Join(root, FileName)); err != nil {
		return fmt.Errorf("replace bank list: %w", err)
	}
	armed = false
	return nil
}

// Load reads <root>/bank-servers.json. present is false when the file is absent
// or corrupt; a corrupt file is logged without its contents.
func Load(root string) (List, civil.Date, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName)) // #nosec G304 -- built from the data root.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, civil.Date{}, false, nil
		}
		return nil, civil.Date{}, false, fmt.Errorf("read bank list: %w", err)
	}

	var file storedFile
	if err := json.Unmarshal(data, &file); err != nil {
		slog.Default().Warn("bank list file is corrupt; ignoring it", "error", err)
		return nil, civil.Date{}, false, nil
	}
	if file.Version != 1 {
		slog.Default().Warn("bank list file has an unknown version; ignoring it")
		return nil, civil.Date{}, false, nil
	}
	loadedOn, err := civil.ParseISO(file.LoadedOn)
	if err != nil {
		slog.Default().Warn("bank list file has an invalid date; ignoring it")
		return nil, civil.Date{}, false, nil
	}

	list := make(List, len(file.Entries))
	for blz, entry := range file.Entries {
		list[blz] = Entry(entry)
	}
	return list, loadedOn, true, nil
}

// Remove deletes <root>/bank-servers.json. A missing file is not an error.
func Remove(root string) error {
	if err := os.Remove(filepath.Join(root, FileName)); err != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove bank list: %w", err)
	}
	return nil
}
