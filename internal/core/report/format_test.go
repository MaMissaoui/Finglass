package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

type formatCase struct {
	Function string `json:"fn"`
	Lang     string `json:"lang"`
	Month    uint8  `json:"month"`
	Currency string `json:"currency"`
	Minor    string `json:"minor"`
	Signed   bool   `json:"signed"`
	Out      string `json:"out"`
}

func TestReportFormatMatchesRust(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "report_format.json"))
	if err != nil {
		t.Fatalf("read the reference cases: %v", err)
	}
	var cases []formatCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse the reference cases: %v", err)
	}
	if len(cases) != 1_260 {
		t.Fatalf("reference case count = %d, want 1260", len(cases))
	}

	for index, testCase := range cases {
		lang := languageOf(t, testCase.Lang)
		var got string
		switch testCase.Function {
		case "month_name":
			got = MonthName(lang, testCase.Month)
		case "month_short_name":
			got = MonthShortName(lang, testCase.Month)
		case "money", "pdf_amount":
			currency, err := money.NewCurrency(testCase.Currency)
			if err != nil {
				t.Fatalf("case %d: currency %q: %v", index, testCase.Currency, err)
			}
			minor, err := strconv.ParseInt(testCase.Minor, 10, 64)
			if err != nil {
				t.Fatalf("case %d: minor %q: %v", index, testCase.Minor, err)
			}
			amount := money.Money{Minor: minor, Currency: currency}
			if testCase.Function == "money" {
				got = Money(amount, lang, testCase.Signed)
			} else {
				got = PDFAmount(amount, lang, testCase.Signed)
			}
		default:
			t.Fatalf("case %d: unknown function %q", index, testCase.Function)
		}
		if got != testCase.Out {
			t.Fatalf(
				"case %d %s(%s, month=%d, currency=%s, minor=%s, signed=%t) = %q, want %q",
				index,
				testCase.Function,
				testCase.Lang,
				testCase.Month,
				testCase.Currency,
				testCase.Minor,
				testCase.Signed,
				got,
				testCase.Out,
			)
		}
	}
}

func languageOf(t *testing.T, code string) i18n.Language {
	t.Helper()
	switch code {
	case "en":
		return i18n.En
	case "de":
		return i18n.De
	case "fr":
		return i18n.Fr
	default:
		t.Fatalf("unknown language %q", code)
		return i18n.En
	}
}
