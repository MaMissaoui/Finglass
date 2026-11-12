package imports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

type goldenMapping struct {
	Date        *int `json:"date"`
	ValueDate   *int `json:"value_date"`
	Amount      *int `json:"amount"`
	Debit       *int `json:"debit"`
	Credit      *int `json:"credit"`
	Partner     *int `json:"partner"`
	Payer       *int `json:"payer"`
	PartnerIBAN *int `json:"partner_iban"`
	Purpose     *int `json:"purpose"`
	Currency    *int `json:"currency"`
	Balance     *int `json:"balance"`
}

type goldenCSV struct {
	Delimiter   string        `json:"delimiter"`
	CharsetName string        `json:"charset_name"`
	HeaderLine  int           `json:"header_line"`
	Headers     []string      `json:"headers"`
	Mapping     goldenMapping `json:"mapping"`
}

type goldenClosing struct {
	Amount          string  `json:"amount"`
	Date            string  `json:"date"`
	Currency        *string `json:"currency"`
	PendingExcluded uint32  `json:"pending_excluded"`
}

type goldenBooking struct {
	Date          string  `json:"date"`
	ValueDate     *string `json:"value_date"`
	Amount        string  `json:"amount"`
	Currency      *string `json:"currency"`
	PartnerName   *string `json:"partner_name"`
	PartnerIBAN   *string `json:"partner_iban"`
	Purpose       *string `json:"purpose"`
	BalanceAfter  *string `json:"balance_after"`
	BankReference *string `json:"bank_reference"`
}

type goldenStatement struct {
	Format         string          `json:"format"`
	FormatLabel    string          `json:"format_label"`
	FormatDetail   *string         `json:"format_detail"`
	AccountIBAN    *string         `json:"account_iban"`
	Accounts       []string        `json:"accounts"`
	PendingSkipped uint32          `json:"pending_skipped"`
	Skipped        []string        `json:"skipped"`
	Csv            *goldenCSV      `json:"csv"`
	ClosingBalance *goldenClosing  `json:"closing_balance"`
	Bookings       []goldenBooking `json:"bookings"`
}

type goldenImportFile struct {
	File      string           `json:"file"`
	OK        bool             `json:"ok"`
	Error     string           `json:"error"`
	Statement *goldenStatement `json:"statement"`
}

type goldenHashBooking struct {
	Index           int    `json:"index"`
	TransactionHash string `json:"transaction_hash"`
	ImportHash      string `json:"import_hash"`
}

type goldenHashFile struct {
	File       string              `json:"file"`
	IBAN       string              `json:"iban"`
	FileHashes []string            `json:"file_hashes"`
	Bookings   []goldenHashBooking `json:"bookings"`
}

type goldenEdge struct {
	Name            string  `json:"name"`
	Date            string  `json:"date"`
	Amount          string  `json:"amount"`
	Partner         *string `json:"partner"`
	Purpose         *string `json:"purpose"`
	TransactionHash string  `json:"transaction_hash"`
}

type goldenHashDocument struct {
	Files     []goldenHashFile `json:"files"`
	EdgeCases []goldenEdge     `json:"edge_cases"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func goldenToday(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, 10, 15)
	if !ok {
		t.Fatal("invalid golden date")
	}
	return date
}

func TestImportStatementsMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "import", "statements.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenImportFile
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) == 0 {
		t.Fatal("the import golden is empty")
	}
	inputDir := filepath.Join(root, "testdata", "inputs", "import")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatal(err)
	}
	inputFiles := []string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}
		inputFiles = append(inputFiles, entry.Name())
	}
	if len(inputFiles) != len(golden) {
		t.Fatalf("input files = %d, golden entries = %d", len(inputFiles), len(golden))
	}
	seen := map[string]bool{}
	for _, expected := range golden {
		seen[expected.File] = true
		bytes, err := os.ReadFile(filepath.Join(inputDir, expected.File)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", expected.File, err)
		}
		statement, err := Read(expected.File, bytes, nil, goldenToday(t))
		if expected.OK {
			if err != nil {
				t.Errorf("%s: Read failed: %v", expected.File, err)
				continue
			}
			if expected.Statement == nil {
				t.Errorf("%s: the golden has no statement", expected.File)
				continue
			}
			compareGoldenStatement(t, expected.File, statement, *expected.Statement)
			continue
		}
		if err == nil {
			t.Errorf("%s: Read succeeded, want error %q", expected.File, expected.Error)
			continue
		}
		if err.Error() != expected.Error {
			t.Errorf("%s: error = %q, want %q", expected.File, err.Error(), expected.Error)
		}
	}
	for _, name := range inputFiles {
		if !seen[name] {
			t.Errorf("%s: no golden entry", name)
		}
	}
}

func compareGoldenStatement(t *testing.T, file string, got ParsedStatement, want goldenStatement) {
	t.Helper()
	checkGoldenField(t, file, "format", string(got.Format), want.Format)
	checkGoldenField(t, file, "format_label", got.FormatLabel(), want.FormatLabel)
	checkGoldenField(t, file, "format_detail", got.FormatDetail, want.FormatDetail)
	checkGoldenField(t, file, "account_iban", got.AccountIBAN, want.AccountIBAN)
	if !reflect.DeepEqual(got.Accounts, want.Accounts) {
		t.Errorf("%s: accounts = %v, want %v", file, got.Accounts, want.Accounts)
	}
	checkGoldenField(t, file, "pending_skipped", got.PendingSkipped, want.PendingSkipped)
	if !reflect.DeepEqual(got.Skipped, want.Skipped) {
		t.Errorf("%s: skipped = %v, want %v", file, got.Skipped, want.Skipped)
	}
	compareGoldenCSV(t, file, got.Csv, want.Csv)
	compareGoldenClosing(t, file, got.ClosingBalance, want.ClosingBalance)
	if len(got.Bookings) != len(want.Bookings) {
		t.Fatalf("%s: bookings = %d, want %d", file, len(got.Bookings), len(want.Bookings))
	}
	for index := range want.Bookings {
		compareGoldenBooking(t, file, index, got.Bookings[index], want.Bookings[index])
	}
}

func checkGoldenField(t *testing.T, file, field string, got, want any) {
	t.Helper()
	gotValue := dereference(got)
	wantValue := dereference(want)
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("%s: %s = %v, want %v", file, field, gotValue, wantValue)
	}
}

func dereference(value any) any {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil
		}
		return dereference(reflected.Elem().Interface())
	}
	return value
}

func compareGoldenCSV(t *testing.T, file string, got *CsvLayout, want *goldenCSV) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s: csv = %v, want %v", file, got, want)
		}
		return
	}
	checkGoldenField(t, file, "csv.delimiter", string(got.Delimiter), want.Delimiter)
	checkGoldenField(t, file, "csv.charset_name", got.CharsetName, want.CharsetName)
	checkGoldenField(t, file, "csv.header_line", got.HeaderLine, want.HeaderLine)
	if !reflect.DeepEqual(got.Headers, want.Headers) {
		t.Errorf("%s: csv.headers = %v, want %v", file, got.Headers, want.Headers)
	}
	mapping := got.Mapping
	expected := want.Mapping
	checkGoldenField(t, file, "csv.mapping.date", mapping.Date, expected.Date)
	checkGoldenField(t, file, "csv.mapping.value_date", mapping.ValueDate, expected.ValueDate)
	checkGoldenField(t, file, "csv.mapping.amount", mapping.Amount, expected.Amount)
	checkGoldenField(t, file, "csv.mapping.debit", mapping.Debit, expected.Debit)
	checkGoldenField(t, file, "csv.mapping.credit", mapping.Credit, expected.Credit)
	checkGoldenField(t, file, "csv.mapping.partner", mapping.Partner, expected.Partner)
	checkGoldenField(t, file, "csv.mapping.payer", mapping.Payer, expected.Payer)
	checkGoldenField(t, file, "csv.mapping.partner_iban", mapping.PartnerIBAN, expected.PartnerIBAN)
	checkGoldenField(t, file, "csv.mapping.purpose", mapping.Purpose, expected.Purpose)
	checkGoldenField(t, file, "csv.mapping.currency", mapping.Currency, expected.Currency)
	checkGoldenField(t, file, "csv.mapping.balance", mapping.Balance, expected.Balance)
}

func compareGoldenClosing(t *testing.T, file string, got *ClosingBalance, want *goldenClosing) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s: closing_balance = %v, want %v", file, got, want)
		}
		return
	}
	checkGoldenField(t, file, "closing_balance.amount", got.Amount.String(), want.Amount)
	checkGoldenField(t, file, "closing_balance.date", got.Date.String(), want.Date)
	checkGoldenField(t, file, "closing_balance.currency", got.Currency, want.Currency)
	checkGoldenField(t, file, "closing_balance.pending_excluded", got.PendingExcluded, want.PendingExcluded)
}

func compareGoldenBooking(t *testing.T, file string, index int, got ImportedBooking, want goldenBooking) {
	t.Helper()
	prefix := "bookings[" + intToString(index) + "]."
	checkGoldenField(t, file, prefix+"date", got.Date.String(), want.Date)
	checkGoldenField(t, file, prefix+"value_date", optionalDateText(got.ValueDate), want.ValueDate)
	checkGoldenField(t, file, prefix+"amount", got.Amount.String(), want.Amount)
	checkGoldenField(t, file, prefix+"currency", got.Currency, want.Currency)
	checkGoldenField(t, file, prefix+"partner_name", got.PartnerName, want.PartnerName)
	checkGoldenField(t, file, prefix+"partner_iban", got.PartnerIBAN, want.PartnerIBAN)
	checkGoldenField(t, file, prefix+"purpose", got.Purpose, want.Purpose)
	checkGoldenField(t, file, prefix+"balance_after", optionalDecimalText(got.BalanceAfter), want.BalanceAfter)
	checkGoldenField(t, file, prefix+"bank_reference", got.BankReference, want.BankReference)
}

func optionalDateText(value *civil.Date) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}

func optionalDecimalText(value *decimal.Decimal) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}

func TestImportHashesMatchTheRustGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "hash", "hashes.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenHashDocument
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	inputDir := filepath.Join(root, "testdata", "inputs", "import")
	if len(golden.Files) == 0 {
		t.Fatal("the hash golden has no files")
	}
	for _, expected := range golden.Files {
		bytes, err := os.ReadFile(filepath.Join(inputDir, expected.File)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", expected.File, err)
		}
		statement, err := Read(expected.File, bytes, nil, goldenToday(t))
		if err != nil {
			t.Errorf("%s: Read failed: %v", expected.File, err)
			continue
		}
		iban := ""
		if statement.AccountIBAN != nil {
			iban = *statement.AccountIBAN
		}
		checkGoldenField(t, expected.File, "iban", iban, expected.IBAN)
		hashes := Hashes(iban, statement.Bookings)
		if !reflect.DeepEqual(hashes, expected.FileHashes) {
			t.Errorf("%s: file_hashes = %v, want %v", expected.File, hashes, expected.FileHashes)
		}
		if len(statement.Bookings) != len(expected.Bookings) {
			t.Errorf("%s: bookings = %d, want %d", expected.File, len(statement.Bookings), len(expected.Bookings))
			continue
		}
		for index, booking := range statement.Bookings {
			want := expected.Bookings[index]
			checkGoldenField(t, expected.File, "bookings["+intToString(index)+"].index", index, want.Index)
			transactionHash := TransactionHash(booking.Date, booking.Amount, booking.PartnerName, booking.Purpose)
			checkGoldenField(t, expected.File, "bookings["+intToString(index)+"].transaction_hash", transactionHash, want.TransactionHash)
			importHash := ImportHash(
				booking.Date,
				booking.Amount,
				booking.PartnerName,
				booking.Purpose,
				iban,
				booking.BankReference,
				booking.BalanceAfter,
				nil,
			)
			checkGoldenField(t, expected.File, "bookings["+intToString(index)+"].import_hash", importHash, want.ImportHash)
		}
	}
	for _, edge := range golden.EdgeCases {
		date, err := civil.ParseISO(edge.Date)
		if err != nil {
			t.Errorf("%s: %v", edge.Name, err)
			continue
		}
		amount, err := decimal.Parse(edge.Amount)
		if err != nil {
			t.Errorf("%s: %v", edge.Name, err)
			continue
		}
		hash := TransactionHash(date, amount, edge.Partner, edge.Purpose)
		checkGoldenField(t, edge.Name, "transaction_hash", hash, edge.TransactionHash)
	}
}
