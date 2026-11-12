package findeskimport

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/money"
)

type goldenCase struct {
	Bits     string          `json:"bits"`
	Display  string          `json:"display"`
	Exponent int32           `json:"exponent"`
	Kind     string          `json:"kind"`
	Value    json.RawMessage `json:"value"`
	Result   struct {
		OK  *int64 `json:"ok"`
		Err string `json:"err"`
	} `json:"result"`
}

func currencyForExponent(t *testing.T, exponent int32) money.Currency {
	t.Helper()
	code := "EUR"
	switch exponent {
	case 0:
		code = "JPY"
	case 2:
		code = "EUR"
	case 3:
		code = "BHD"
	default:
		t.Fatalf("no test currency with exponent %d", exponent)
	}
	currency, err := money.NewCurrency(code)
	if err != nil {
		t.Fatalf("NewCurrency(%q): %v", code, err)
	}
	return currency
}

func TestRequiredMinorMatchesRust(t *testing.T) {
	contents, err := os.ReadFile("testdata/required_minor.json") // #nosec G304 -- committed test data.
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	var golden struct {
		Cases []goldenCase `json:"cases"`
	}
	if err := json.Unmarshal(contents, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(golden.Cases) == 0 {
		t.Fatal("golden has no cases")
	}

	for index, testCase := range golden.Cases {
		testCase := testCase
		t.Run(fmt.Sprintf("%03d_%s_%d", index, testCase.Kind, testCase.Exponent), func(t *testing.T) {
			var value sourceNumber
			switch testCase.Kind {
			case "integer":
				if err := json.Unmarshal(testCase.Value, &value.integer); err != nil {
					t.Fatalf("decode integer: %v", err)
				}
				value.kind = sourceInteger
			case "text":
				if err := json.Unmarshal(testCase.Value, &value.text); err != nil {
					t.Fatalf("decode text: %v", err)
				}
				value.kind = sourceText
			case "real":
				bits, err := hex.DecodeString(testCase.Bits)
				if err != nil || len(bits) != 8 {
					t.Fatalf("decode bits %q: %v", testCase.Bits, err)
				}
				var packed uint64
				for _, byteValue := range bits {
					packed = packed<<8 | uint64(byteValue)
				}
				value.kind = sourceReal
				value.real = math.Float64frombits(packed)
				if !math.IsNaN(value.real) && !math.IsInf(value.real, 0) {
					formatted := strconv.FormatFloat(value.real, 'f', -1, 64)
					if formatted != testCase.Display {
						t.Errorf("FormatFloat = %q, want Rust display %q", formatted, testCase.Display)
					}
				}
			default:
				t.Fatalf("unknown kind %q", testCase.Kind)
			}

			currency := currencyForExponent(t, testCase.Exponent)
			minor, convertErr := requiredMinor(&value, currency)

			switch {
			case testCase.Result.OK != nil:
				if convertErr != nil {
					t.Fatalf("requiredMinor error = %v, want %d", convertErr, *testCase.Result.OK)
				}
				if minor != *testCase.Result.OK {
					t.Fatalf("requiredMinor = %d, want %d", minor, *testCase.Result.OK)
				}
			case testCase.Result.Err != "":
				if convertErr == nil {
					t.Fatalf("requiredMinor = %d, want error %s", minor, testCase.Result.Err)
				}
				if convertErr.Error() != testCase.Result.Err {
					t.Fatalf("requiredMinor error = %s, want %s", convertErr.Error(), testCase.Result.Err)
				}
			default:
				t.Fatalf("golden case has neither ok nor err: %+v", testCase.Result)
			}
		})
	}
}
