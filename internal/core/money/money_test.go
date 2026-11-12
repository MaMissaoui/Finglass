package money

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

func eur() Currency {
	return EUR
}

func amount(minor int64, currency Currency) Money {
	return Money{Minor: minor, Currency: currency}
}

func TestCurrencyCodesAreThreeUppercaseLetters(t *testing.T) {
	if got, err := NewCurrency("eur"); err != nil || got != eur() {
		t.Errorf("NewCurrency(\"eur\") = %v, %v", got, err)
	}
	if got, err := NewCurrency("EUR"); err != nil || got.String() != "EUR" {
		t.Errorf("NewCurrency(\"EUR\").String() = %q, %v", got, err)
	}
	if got, err := NewCurrency("usd"); err != nil || got.String() != "USD" {
		t.Errorf("NewCurrency(\"usd\").String() = %q, %v", got, err)
	}
	for _, text := range []string{"EU", "EURO", "E1R"} {
		if _, err := NewCurrency(text); err == nil {
			t.Errorf("NewCurrency(%q) succeeded, want an error", text)
		}
	}
}

func TestMinorExponentsMatchTheCurrency(t *testing.T) {
	tests := []struct {
		code string
		want uint32
	}{
		{"JPY", 0},
		{"KWD", 3},
		{"EUR", 2},
	}
	for _, test := range tests {
		currency, err := NewCurrency(test.code)
		if err != nil {
			t.Fatalf("NewCurrency(%q): %v", test.code, err)
		}
		if got := currency.MinorExponent(); got != test.want {
			t.Errorf("%s.MinorExponent() = %d, want %d", test.code, got, test.want)
		}
	}
}

func TestParsesAllSupportedDecimalForms(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"-12.34", -1234},
		{"12,34", 1234},
		{"1.234,56", 123456},
		{"1,234.56", 123456},
		{"+5", 500},
		{"0", 0},
	}
	for _, test := range tests {
		got, err := ParseDecimal(test.input, eur())
		if err != nil {
			t.Errorf("ParseDecimal(%q): %v", test.input, err)
			continue
		}
		if got != amount(test.want, eur()) {
			t.Errorf("ParseDecimal(%q) = %+v, want %+v", test.input, got, amount(test.want, eur()))
		}
	}
}

func TestThreeDigitFractionsDependOnTheCurrency(t *testing.T) {
	got, err := ParseDecimal("12.345", eur())
	if err != nil {
		t.Fatal(err)
	}
	if got != amount(1_234_500, eur()) {
		t.Errorf("EUR 12.345 = %+v", got)
	}

	kwd, err := NewCurrency("KWD")
	if err != nil {
		t.Fatal(err)
	}
	got, err = ParseDecimal("12.345", kwd)
	if err != nil {
		t.Fatal(err)
	}
	if got != amount(12345, kwd) {
		t.Errorf("KWD 12.345 = %+v", got)
	}
}

func TestMalformedGroupingIsAnError(t *testing.T) {
	for _, input := range []string{"1.2.3", "12,345,6"} {
		if _, err := ParseDecimal(input, eur()); err == nil {
			t.Errorf("ParseDecimal(%q) succeeded, want an error", input)
		}
	}
}

func TestTooManyFractionDigitsIsAnError(t *testing.T) {
	for _, input := range []string{"0.001", "1.2345"} {
		if _, err := ParseDecimal(input, eur()); err == nil {
			t.Errorf("ParseDecimal(%q) succeeded, want an error", input)
		}
	}

	_, err := ParseDecimal("1.2345", eur())
	var invalid InvalidError
	if !errors.As(err, &invalid) || invalid.Msg != "too many decimal places for EUR" {
		t.Errorf("ParseDecimal(\"1.2345\") error = %v, want the currency message", err)
	}
}

func TestJpyUsesWholeUnits(t *testing.T) {
	jpy, err := NewCurrency("JPY")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseDecimal("500", jpy)
	if err != nil {
		t.Fatal(err)
	}
	if got != amount(500, jpy) {
		t.Errorf("JPY 500 = %+v", got)
	}
}

func TestEmptyAndOtherTextIsAnError(t *testing.T) {
	for _, input := range []string{"", "   ", "abc", "+", "1e3"} {
		if _, err := ParseDecimal(input, eur()); err == nil {
			t.Errorf("ParseDecimal(%q) succeeded, want an error", input)
		}
	}
}

func TestFormatPlainRoundTrips(t *testing.T) {
	tests := []struct {
		minor    int64
		expected string
	}{
		{-123456, "-1234.56"},
		{0, "0.00"},
		{5, "0.05"},
		{100, "1.00"},
		{-1, "-0.01"},
		{math.MinInt64, "-92233720368547758.08"},
	}
	for _, test := range tests {
		value := amount(test.minor, eur())
		if got := FormatPlain(value); got != test.expected {
			t.Errorf("FormatPlain(%d) = %q, want %q", test.minor, got, test.expected)
		}
		parsed, err := ParseDecimal(test.expected, eur())
		if err != nil {
			t.Errorf("ParseDecimal(%q): %v", test.expected, err)
			continue
		}
		if parsed != value {
			t.Errorf("ParseDecimal(%q) = %+v, want %+v", test.expected, parsed, value)
		}
	}

	jpy, err := NewCurrency("JPY")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatPlain(amount(500, jpy)); got != "500" {
		t.Errorf("JPY FormatPlain(500) = %q", got)
	}
	kwd, err := NewCurrency("KWD")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatPlain(amount(12345, kwd)); got != "12.345" {
		t.Errorf("KWD FormatPlain(12345) = %q", got)
	}
}

func TestCheckedAdditionRejectsMismatchesAndOverflow(t *testing.T) {
	usd, err := NewCurrency("USD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := amount(1, eur()).CheckedAdd(amount(2, usd)); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("mismatch error = %v, want ErrCurrencyMismatch", err)
	}
	if got, err := amount(1, eur()).CheckedAdd(amount(2, eur())); err != nil || got != amount(3, eur()) {
		t.Errorf("1 + 2 = %+v, %v", got, err)
	}
	if _, err := amount(math.MaxInt64, eur()).CheckedAdd(amount(1, eur())); !errors.Is(err, ErrOverflow) {
		t.Errorf("overflow error = %v, want ErrOverflow", err)
	}
}

func TestTotalsKeepCurrenciesApart(t *testing.T) {
	usd, err := NewCurrency("USD")
	if err != nil {
		t.Fatal(err)
	}
	gbp, err := NewCurrency("GBP")
	if err != nil {
		t.Fatal(err)
	}
	var totals Totals
	for _, value := range []Money{amount(100, eur()), amount(250, eur()), amount(50, usd)} {
		if err := totals.Add(value); err != nil {
			t.Fatal(err)
		}
	}

	if got := totals.Get(eur()); got != 350 {
		t.Errorf("EUR total = %d, want 350", got)
	}
	if got := totals.Get(usd); got != 50 {
		t.Errorf("USD total = %d, want 50", got)
	}
	if got := totals.Get(gbp); got != 0 {
		t.Errorf("GBP total = %d, want 0", got)
	}

	var entries []CurrencyTotal
	totals.Each(func(currency Currency, total int64) {
		entries = append(entries, CurrencyTotal{Currency: currency, Total: total})
	})
	want := []CurrencyTotal{{Currency: eur(), Total: 350}, {Currency: usd, Total: 50}}
	if len(entries) != len(want) || entries[0] != want[0] || entries[1] != want[1] {
		t.Errorf("entries = %+v, want %+v", entries, want)
	}
}

func TestNegativeAmountsAreReported(t *testing.T) {
	if !amount(-1, eur()).IsNegative() {
		t.Error("-1 is not negative")
	}
	if amount(0, eur()).IsNegative() {
		t.Error("0 is negative")
	}
	if amount(1, eur()).IsNegative() {
		t.Error("1 is negative")
	}
}

func TestTotalsPutThePrimaryCurrencyFirst(t *testing.T) {
	usd, err := NewCurrency("USD")
	if err != nil {
		t.Fatal(err)
	}
	items := []struct {
		code   string
		amount int64
	}{{"USD", 500}, {"EUR", 1000}, {"USD", 250}}

	totals := SumByCurrency(items, eur(), func(item struct {
		code   string
		amount int64
	}) Currency {
		currency, _ := NewCurrency(item.code)
		return currency
	}, func(item struct {
		code   string
		amount int64
	}) int64 {
		return item.amount
	})

	want := []CurrencyTotal{{Currency: eur(), Total: 1000}, {Currency: usd, Total: 750}}
	if len(totals) != len(want) || totals[0] != want[0] || totals[1] != want[1] {
		t.Errorf("totals = %+v, want %+v", totals, want)
	}
}

func TestThePrimaryCurrencyIsReadFromTheSettingsWithTheFindeskKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Create(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	ctx := context.Background()

	if got, err := Primary(ctx, database); err != nil || got != eur() {
		t.Errorf("Primary on an unset database = %v, %v; want EUR", got, err)
	}
	if err := settings.Set(ctx, database, PrimaryCurrencyKey, "USD"); err != nil {
		t.Fatal(err)
	}
	if got, err := Primary(ctx, database); err != nil || got.String() != "USD" {
		t.Errorf("Primary after setting USD = %v, %v", got, err)
	}
	if err := settings.Set(ctx, database, PrimaryCurrencyKey, "EU"); err != nil {
		t.Fatal(err)
	}
	if got, err := Primary(ctx, database); err != nil || got != eur() {
		t.Errorf("Primary with an invalid code = %v, %v; want EUR", got, err)
	}
	if err := settings.Set(ctx, database, PrimaryCurrencyKey, "EURO"); err != nil {
		t.Fatal(err)
	}
	if got, err := Primary(ctx, database); err != nil || got != eur() {
		t.Errorf("Primary with a longer value = %v, %v; want EUR", got, err)
	}
}

func TestInvalidErrorsUseRustQuoting(t *testing.T) {
	_, err := ParseDecimal("  ", eur())
	var invalid InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidError", err)
	}
	if invalid.Error() != `invalid money value: invalid amount "  "` {
		t.Errorf("error = %q", invalid.Error())
	}

	_, err = NewCurrency("E1R")
	if !errors.As(err, &invalid) || invalid.Error() != `invalid money value: invalid currency code "E1R"` {
		t.Errorf("currency error = %v", err)
	}
}
