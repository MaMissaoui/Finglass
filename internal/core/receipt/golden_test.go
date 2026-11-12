package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

type goldenMoney struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

type goldenReceipt struct {
	File    string        `json:"file"`
	Total   *goldenMoney  `json:"total"`
	Date    *string       `json:"date"`
	Amounts []goldenMoney `json:"amounts"`
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

func receiptGoldenToday(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, 11, 7)
	if !ok {
		t.Fatal("invalid golden date")
	}
	return date
}

func checkGoldenMoney(t *testing.T, file, field string, got money.Money, want goldenMoney) {
	t.Helper()
	if got.Minor != want.Minor {
		t.Errorf("%s: %s.minor = %d, want %d", file, field, got.Minor, want.Minor)
	}
	if got.Currency.String() != want.Currency {
		t.Errorf("%s: %s.currency = %q, want %q", file, field, got.Currency.String(), want.Currency)
	}
	if plain := money.FormatPlain(got); plain != want.Plain {
		t.Errorf("%s: %s.plain = %q, want %q", file, field, plain, want.Plain)
	}
}

func checkGoldenTotal(t *testing.T, file string, got *money.Money, want *goldenMoney) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s: total = %v, want %v", file, got, want)
		}
		return
	}
	checkGoldenMoney(t, file, "total", *got, *want)
}

func checkGoldenDate(t *testing.T, file string, got *civil.Date, want *string) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Errorf("%s: date = %v, want %v", file, got, want)
		}
		return
	}
	if got.String() != *want {
		t.Errorf("%s: date = %q, want %q", file, got.String(), *want)
	}
}

func TestReceiptGolden(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "receipt", "receipts.json")) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenReceipt
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) == 0 {
		t.Fatal("the receipt golden is empty")
	}

	inputDir := filepath.Join(root, "testdata", "inputs", "receipt")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) != len(golden) {
		t.Fatalf("receipt inputs = %d, golden = %d", len(names), len(golden))
	}

	today := receiptGoldenToday(t)
	for index, name := range names {
		want := golden[index]
		if want.File != name {
			t.Fatalf("golden[%d].file = %q, want %q", index, want.File, name)
		}
		bytes, err := os.ReadFile(filepath.Join(inputDir, name)) // #nosec G304 -- fixed repository fixture path.
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fields := Parse(string(bytes), money.EUR, today)
		checkGoldenTotal(t, name, fields.Total, want.Total)
		checkGoldenDate(t, name, fields.Date, want.Date)
		if len(fields.Amounts) != len(want.Amounts) {
			t.Errorf("%s: amounts = %d, want %d", name, len(fields.Amounts), len(want.Amounts))
			continue
		}
		for amountIndex := range want.Amounts {
			checkGoldenMoney(t, name, "amounts["+intToString(amountIndex)+"]", fields.Amounts[amountIndex], want.Amounts[amountIndex])
		}
	}
}

func intToString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
