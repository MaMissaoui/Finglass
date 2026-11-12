package format

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func currency(t *testing.T, code string) money.Currency {
	t.Helper()
	value, err := money.NewCurrency(code)
	if err != nil {
		t.Fatalf("NewCurrency(%q): %v", code, err)
	}
	return value
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func usd(t *testing.T, minor int64) money.Money {
	return money.Money{Minor: minor, Currency: currency(t, "USD")}
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return value
}

func TestMoneyFormatting(t *testing.T) {
	if got := Money(eur(123_450), i18n.En, false); got != "€1,234.50" {
		t.Errorf("Money = %q", got)
	}
	if got := Money(eur(-4_567), i18n.En, false); got != "-€45.67" {
		t.Errorf("Money = %q", got)
	}
	if got := Money(eur(320_000), i18n.En, true); got != "+€3,200.00" {
		t.Errorf("Money = %q", got)
	}
	if got := Money(eur(0), i18n.En, true); got != "€0.00" {
		t.Errorf("Money zero = %q", got)
	}
	if got := CurrencySymbol(money.EUR); got != "€" {
		t.Errorf("CurrencySymbol(EUR) = %q", got)
	}
	if got := CurrencySymbol(currency(t, "USD")); got != "$" {
		t.Errorf("CurrencySymbol(USD) = %q", got)
	}
}

func TestMoneyUsesTheLanguageSeparators(t *testing.T) {
	if got := Money(eur(123_450), i18n.De, false); got != "1.234,50\u00a0€" {
		t.Errorf("German Money = %q", got)
	}
	if got := Money(eur(123_450), i18n.Fr, false); got != "€1\u202f234,50" {
		t.Errorf("French Money = %q", got)
	}
}

func TestMoneyUsesTheCurrencyExponent(t *testing.T) {
	if got := Money(money.Money{Minor: 1234, Currency: currency(t, "JPY")}, i18n.En, false); got != "¥1,234" {
		t.Errorf("JPY Money = %q", got)
	}
	if got := Money(money.Money{Minor: 12_345, Currency: currency(t, "KWD")}, i18n.En, false); got != "KWD12.345" {
		t.Errorf("KWD Money = %q", got)
	}
	if got := Money(money.Money{Minor: 100, Currency: currency(t, "QQQ")}, i18n.En, false); got != "QQQ 1.00" {
		t.Errorf("unknown Money = %q", got)
	}
	if got := Money(money.Money{Minor: 123_450, Currency: currency(t, "CHF")}, i18n.En, false); got != "CHF1,234.50" {
		t.Errorf("CHF Money = %q", got)
	}
}

func TestWholeMoneyRoundsHalfUp(t *testing.T) {
	if got := WholeMoney(eur(400_000), i18n.En); got != "€4,000" {
		t.Errorf("WholeMoney = %q", got)
	}
	if got := WholeMoney(eur(400_050), i18n.En); got != "€4,001" {
		t.Errorf("WholeMoney = %q", got)
	}
	if got := WholeMoney(eur(-400_049), i18n.En); got != "-€4,000" {
		t.Errorf("WholeMoney = %q", got)
	}
	if got := WholeMoney(eur(400_000), i18n.De); got != "4.000\u00a0€" {
		t.Errorf("German WholeMoney = %q", got)
	}
}

func TestGermanMoneyPutsTheNumberBeforeTheSymbol(t *testing.T) {
	chf := currency(t, "CHF")
	jpy := currency(t, "JPY")

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"en 245037", Money(eur(245_037), i18n.En, false), "€2,450.37"},
		{"en whole", WholeMoney(eur(400_000), i18n.En), "€4,000"},
		{"de 245037", Money(eur(245_037), i18n.De, false), "2.450,37\u00a0€"},
		{"de negative", Money(eur(-500_000), i18n.De, false), "-5.000,00\u00a0€"},
		{"de signed", Money(eur(43_100), i18n.De, true), "+431,00\u00a0€"},
		{"de whole", WholeMoney(eur(400_000), i18n.De), "4.000\u00a0€"},
		{"de usd", Money(usd(t, 50_000), i18n.De, false), "500,00\u00a0$"},
		{"de chf", Money(money.Money{Minor: 1_200, Currency: chf}, i18n.De, false), "12,00\u00a0CHF"},
		{"de jpy", Money(money.Money{Minor: 1_234, Currency: jpy}, i18n.De, false), "1.234\u00a0¥"},
		{"fr 245037", Money(eur(245_037), i18n.Fr, false), "€2\u202f450,37"},
		{"fr whole", WholeMoney(eur(400_000), i18n.Fr), "€4\u202f000"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
}

func TestGermanMoneyRoundTripsThroughParseAmount(t *testing.T) {
	for _, minor := range []int64{-456_789, 0, 43_100, 1_234_567_890_123} {
		for _, signed := range []bool{false, true} {
			text := Money(eur(minor), i18n.De, signed)
			got, ok := ParseAmount(text)
			if !ok {
				t.Errorf("ParseAmount(%q) failed", text)
				continue
			}
			want := decimal.New(minor, 2)
			if got.String() != want.String() {
				t.Errorf("ParseAmount(%q) = %q, want %q", text, got.String(), want.String())
			}
		}
	}
}

func TestPercentIsWholeAndLanguageSpecific(t *testing.T) {
	tests := []struct {
		share float64
		lang  i18n.Language
		want  string
	}{
		{0.49, i18n.En, "49%"},
		{1.0, i18n.En, "100%"},
		{0.0, i18n.En, "0%"},
		{0.004, i18n.En, "<1%"},
		{0.0004, i18n.En, "<1%"},
		{0.49, i18n.De, "49\u00a0%"},
		{0.0, i18n.De, "0\u00a0%"},
		{0.0001, i18n.De, "<1\u00a0%"},
		{0.1234, i18n.De, "12\u00a0%"},
		{0.03, i18n.Fr, "3\u00a0%"},
	}
	for _, test := range tests {
		if got := Percent(test.share, test.lang); got != test.want {
			t.Errorf("Percent(%v, %v) = %q, want %q", test.share, test.lang, got, test.want)
		}
	}
}

func TestSharesRoundByLargestRemainderAndSumToExactly100(t *testing.T) {
	if got := Shares([]money.Money{eur(1), eur(1), eur(1)}); !slices.Equal(got, []int64{34, 33, 33}) {
		t.Errorf("three equal shares = %v", got)
	}
	if got := Shares([]money.Money{eur(3_000), eur(7_000)}); !slices.Equal(got, []int64{30, 70}) {
		t.Errorf("3000/7000 shares = %v", got)
	}

	ten := make([]money.Money, 0, 10)
	for value := int64(1); value <= 10; value++ {
		ten = append(ten, eur(value))
	}
	total := int64(0)
	for _, share := range Shares(ten) {
		total += share
	}
	if total != 100 {
		t.Errorf("ten shares sum to %d, want 100", total)
	}
	reversed := make([]money.Money, 0, 10)
	for value := int64(10); value >= 1; value-- {
		reversed = append(reversed, eur(value))
	}
	if got := Shares(reversed); !slices.Equal(got, []int64{18, 16, 15, 13, 11, 9, 7, 5, 4, 2}) {
		t.Errorf("reversed shares = %v", got)
	}
}

func TestSharesHandleEmptyAndAllZeroLists(t *testing.T) {
	if got := Shares(nil); len(got) != 0 {
		t.Errorf("empty shares = %v", got)
	}
	if got := Shares([]money.Money{eur(0), eur(0), eur(0)}); !slices.Equal(got, []int64{0, 0, 0}) {
		t.Errorf("zero shares = %v", got)
	}
}

func TestTheAmountLabelCarriesTheCurrencySymbol(t *testing.T) {
	if got := i18n.Format(i18n.En, i18n.AmountIn, i18n.Str(CurrencySymbol(money.EUR))); got != "Amount (€)" {
		t.Errorf("EUR amount label = %q", got)
	}
	if got := i18n.Format(i18n.En, i18n.AmountIn, i18n.Str(CurrencySymbol(currency(t, "USD")))); got != "Amount ($)" {
		t.Errorf("USD amount label = %q", got)
	}
}

func TestDateInputAcceptsShortAndPaddedDaysAndRejectsImpossibleOnes(t *testing.T) {
	tests := []struct {
		input string
		want  civil.Date
		ok    bool
	}{
		{"5.11.2026", date(t, 2026, time.November, 5), true},
		{" 05.11.2026 ", date(t, 2026, time.November, 5), true},
		{"29.02.2028", date(t, 2028, time.February, 29), true},
		{"28.02.2026", date(t, 2026, time.February, 28), true},
		{"31.02.2026", civil.Date{}, false},
		{"29.02.2027", civil.Date{}, false},
		{"32.01.2026", civil.Date{}, false},
	}
	for _, test := range tests {
		got, ok := ParseDate(test.input)
		if ok != test.ok || (ok && got != test.want) {
			t.Errorf("ParseDate(%q) = %v, %v; want %v, %v", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestDateAndDateTimeFormatAsBefore(t *testing.T) {
	if got := Date(date(t, 2026, time.November, 5)); got != "05.11.2026" {
		t.Errorf("Date = %q", got)
	}
	moment := time.Date(2026, time.November, 5, 13, 7, 0, 0, time.UTC)
	if got := DateTime(moment); got != "05.11.2026 13:07" {
		t.Errorf("DateTime = %q", got)
	}
}

func TestIbanGrouping(t *testing.T) {
	if got := IBAN("DE89370400440532013000"); got != "DE89 3704 0044 0532 0130 00" {
		t.Errorf("IBAN = %q", got)
	}
}

func TestAmountParsingAcceptsGermanAndEnglishInput(t *testing.T) {
	tests := []struct {
		input string
		want  decimal.Decimal
	}{
		{"1.234,56", decimal.New(123_456, 2)},
		{"1,234.56", decimal.New(123_456, 2)},
		{"-12,5", decimal.New(-1_250, 2)},
		{"12 €", decimal.New(1_200, 2)},
		{"2.450,37 €", decimal.New(245_037, 2)},
		{"2.450,37\u00a0€", decimal.New(245_037, 2)},
		{"€2.450,37", decimal.New(245_037, 2)},
	}
	for _, test := range tests {
		got, ok := ParseAmount(test.input)
		if !ok || got.String() != test.want.String() {
			t.Errorf("ParseAmount(%q) = %q, %v; want %q", test.input, got.String(), ok, test.want.String())
		}
	}
	for _, input := range []string{"abc", ""} {
		if got, ok := ParseAmount(input); ok {
			t.Errorf("ParseAmount(%q) = %q, want failure", input, got.String())
		}
	}
}

func TestParseDecimalRejectsNumbersThatWouldExpandIntoBillionsOfDigits(t *testing.T) {
	for _, input := range []string{"1E999999999", "-1E999999999", "1E-999999999"} {
		if got, ok := ParseDecimal(input); ok {
			t.Errorf("ParseDecimal(%q) = %q, want failure", input, got.String())
		}
	}
}

func TestParseDecimalRejectsValuesOutOfTheDecimal15_2Range(t *testing.T) {
	if got, ok := ParseDecimal("1E15"); ok {
		t.Errorf("ParseDecimal(1E15) = %q, want failure (16 integer digits)", got.String())
	}
	if got, ok := ParseDecimal("1000000000000000"); ok {
		t.Errorf("ParseDecimal(1000000000000000) = %q, want failure (16 integer digits)", got.String())
	}
	got, ok := ParseDecimal("99999999999999.99")
	want := decimal.New(9_999_999_999_999_999, 2)
	if !ok || got.String() != want.String() {
		t.Errorf("ParseDecimal = %q, %v; want %q", got.String(), ok, want.String())
	}
}

func TestParseDecimalRejectsOverLongInputAndMoreThan10Decimals(t *testing.T) {
	digits65 := "12345678901234567890123456789012345678901234567890123456789012345"
	if got, ok := ParseDecimal(digits65); ok {
		t.Errorf("ParseDecimal(65 digits) = %q, want failure", got.String())
	}
	if got, ok := ParseDecimal("0.12345678901"); ok {
		t.Errorf("ParseDecimal(11 decimals) = %q, want failure", got.String())
	}
}

func TestParseAmountCapsTheSameValuesAndKeepsItsSeparatorHandling(t *testing.T) {
	failures := []string{
		"1E999999999", "-1E999999999", "1E-999999999", "1E15",
		"12345678901234567890123456789012345678901234567890123456789012345",
		"0.12345678901", "1000000000000000",
	}
	for _, input := range failures {
		if got, ok := ParseAmount(input); ok {
			t.Errorf("ParseAmount(%q) = %q, want failure", input, got.String())
		}
	}

	tests := []struct {
		input string
		want  decimal.Decimal
	}{
		{"99999999999999.99", decimal.New(9_999_999_999_999_999, 2)},
		{"1.234,56", decimal.New(123_456, 2)},
		{"1,234.56", decimal.New(123_456, 2)},
		{"-12,30", decimal.New(-1_230, 2)},
	}
	for _, test := range tests {
		got, ok := ParseAmount(test.input)
		if !ok || got.String() != test.want.String() {
			t.Errorf("ParseAmount(%q) = %q, %v; want %q", test.input, got.String(), ok, test.want.String())
		}
	}
}

func TestAnUnknownCurrencyCodeKeepsItsCodeAndSign(t *testing.T) {
	xyz := currency(t, "XYZ")
	if got := Money(money.Money{Minor: -5, Currency: xyz}, i18n.En, false); got != "-XYZ 0.05" {
		t.Errorf("Money(XYZ) = %q", got)
	}
	if got := CurrencySymbol(xyz); got != "XYZ" {
		t.Errorf("CurrencySymbol(XYZ) = %q", got)
	}
}

func TestDollarsUseTheDollarSymbol(t *testing.T) {
	if got := Money(usd(t, 50), i18n.En, false); got != "$0.50" {
		t.Errorf("Money(USD) = %q", got)
	}
}

func TestPercentClampsOutOfRangeShares(t *testing.T) {
	tests := []struct {
		share float64
		want  string
	}{
		{1e32, "100%"},
		{2.0, "100%"},
		{math.Inf(1), "100%"},
		{math.Inf(-1), "0%"},
		{math.NaN(), "0%"},
	}
	for _, test := range tests {
		if got := Percent(test.share, i18n.En); got != test.want {
			t.Errorf("Percent(%v) = %q, want %q", test.share, got, test.want)
		}
	}
}

func TestJavaCurrencyCodesAreSortedAndComplete(t *testing.T) {
	if !sort.StringsAreSorted(javaCurrencyCodes) {
		t.Error("javaCurrencyCodes is not sorted")
	}
	if len(javaCurrencyCodes) != 233 {
		t.Errorf("len(javaCurrencyCodes) = %d, want Rust's 233", len(javaCurrencyCodes))
	}
}

type formatGolden struct {
	Money             []moneyGoldenRow        `json:"money"`
	WholeMoney        []wholeMoneyGoldenRow   `json:"whole_money"`
	Percent           []percentGoldenRow      `json:"percent"`
	Shares            []sharesGoldenRow       `json:"shares"`
	Date              []dateGoldenRow         `json:"date"`
	DateTime          []dateTimeGoldenRow     `json:"date_time"`
	Iban              []ibanGoldenRow         `json:"iban"`
	ParseDate         []parseDateGoldenRow    `json:"parse_date"`
	ParseDecimal      []parseDecimalGoldenRow `json:"parse_decimal"`
	ParseAmount       []parseAmountGoldenRow  `json:"parse_amount"`
	MoneyParseDecimal []moneyParseDecimalRow  `json:"money_parse_decimal"`
	FormatPlain       []formatPlainGoldenRow  `json:"format_plain"`
}

type moneyGoldenRow struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	En       string `json:"en"`
	De       string `json:"de"`
	Fr       string `json:"fr"`
	EnSigned string `json:"en_signed"`
	DeSigned string `json:"de_signed"`
	FrSigned string `json:"fr_signed"`
}

type wholeMoneyGoldenRow struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	En       string `json:"en"`
	De       string `json:"de"`
	Fr       string `json:"fr"`
}

type percentGoldenRow struct {
	Share float64 `json:"share"`
	En    string  `json:"en"`
	De    string  `json:"de"`
	Fr    string  `json:"fr"`
}

type sharesGoldenRow struct {
	Amounts []int64 `json:"amounts"`
	Shares  []int64 `json:"shares"`
}

type dateGoldenRow struct {
	Date      string `json:"date"`
	Formatted string `json:"formatted"`
}

type dateTimeGoldenRow struct {
	Date      string `json:"date"`
	Hour      int64  `json:"hour"`
	Minute    int64  `json:"minute"`
	Formatted string `json:"formatted"`
}

type ibanGoldenRow struct {
	Input     string `json:"input"`
	Formatted string `json:"formatted"`
}

type parseDateGoldenRow struct {
	Input  string  `json:"input"`
	Parsed *string `json:"parsed"`
}

type parseDecimalGoldenRow struct {
	Input  string  `json:"input"`
	Parsed *string `json:"parsed"`
}

type parseAmountGoldenRow struct {
	Input  string  `json:"input"`
	Parsed *string `json:"parsed"`
}

type moneyParseDecimalRow struct {
	Currency string  `json:"currency"`
	Input    string  `json:"input"`
	Ok       bool    `json:"ok"`
	Minor    *int64  `json:"minor"`
	Plain    *string `json:"plain"`
	Error    *string `json:"error"`
}

type formatPlainGoldenRow struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Plain    string `json:"plain"`
}

func loadFormatGolden(t *testing.T) formatGolden {
	t.Helper()
	path := filepath.Join(repoRoot(t), "testdata", "golden", "format", "formats.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	var golden formatGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func TestFormatGoldenMoney(t *testing.T) {
	for _, row := range loadFormatGolden(t).Money {
		value := money.Money{Minor: row.Minor, Currency: currency(t, row.Currency)}
		tests := []struct {
			name string
			got  string
			want string
		}{
			{"en", Money(value, i18n.En, false), row.En},
			{"de", Money(value, i18n.De, false), row.De},
			{"fr", Money(value, i18n.Fr, false), row.Fr},
			{"en_signed", Money(value, i18n.En, true), row.EnSigned},
			{"de_signed", Money(value, i18n.De, true), row.DeSigned},
			{"fr_signed", Money(value, i18n.Fr, true), row.FrSigned},
		}
		for _, test := range tests {
			if test.got != test.want {
				t.Errorf("Money(%d %s, %s) = %q, want %q", row.Minor, row.Currency, test.name, test.got, test.want)
			}
		}
	}
}

func TestFormatGoldenWholeMoney(t *testing.T) {
	for _, row := range loadFormatGolden(t).WholeMoney {
		value := money.Money{Minor: row.Minor, Currency: currency(t, row.Currency)}
		tests := []struct {
			name string
			got  string
			want string
		}{
			{"en", WholeMoney(value, i18n.En), row.En},
			{"de", WholeMoney(value, i18n.De), row.De},
			{"fr", WholeMoney(value, i18n.Fr), row.Fr},
		}
		for _, test := range tests {
			if test.got != test.want {
				t.Errorf("WholeMoney(%d %s, %s) = %q, want %q", row.Minor, row.Currency, test.name, test.got, test.want)
			}
		}
	}
}

func TestFormatGoldenPercent(t *testing.T) {
	for _, row := range loadFormatGolden(t).Percent {
		tests := []struct {
			name string
			got  string
			want string
		}{
			{"en", Percent(row.Share, i18n.En), row.En},
			{"de", Percent(row.Share, i18n.De), row.De},
			{"fr", Percent(row.Share, i18n.Fr), row.Fr},
		}
		for _, test := range tests {
			if test.got != test.want {
				t.Errorf("Percent(%v, %s) = %q, want %q", row.Share, test.name, test.got, test.want)
			}
		}
	}
}

func TestFormatGoldenShares(t *testing.T) {
	for _, row := range loadFormatGolden(t).Shares {
		amounts := make([]money.Money, len(row.Amounts))
		for index, minor := range row.Amounts {
			amounts[index] = eur(minor)
		}
		if got := Shares(amounts); !slices.Equal(got, row.Shares) {
			t.Errorf("Shares(%v) = %v, want %v", row.Amounts, got, row.Shares)
		}
	}
}

func TestFormatGoldenDate(t *testing.T) {
	for _, row := range loadFormatGolden(t).Date {
		value, err := civil.ParseISO(row.Date)
		if err != nil {
			t.Errorf("ParseISO(%q): %v", row.Date, err)
			continue
		}
		if got := Date(value); got != row.Formatted {
			t.Errorf("Date(%q) = %q, want %q", row.Date, got, row.Formatted)
		}
	}
}

func TestFormatGoldenDateTime(t *testing.T) {
	for _, row := range loadFormatGolden(t).DateTime {
		value, err := civil.ParseISO(row.Date)
		if err != nil {
			t.Errorf("ParseISO(%q): %v", row.Date, err)
			continue
		}
		moment := time.Date(
			value.Year(), value.Month(), value.Day(),
			int(row.Hour), int(row.Minute), 0, 0, time.UTC,
		)
		if got := DateTime(moment); got != row.Formatted {
			t.Errorf("DateTime(%q %02d:%02d) = %q, want %q", row.Date, row.Hour, row.Minute, got, row.Formatted)
		}
	}
}

func TestFormatGoldenIban(t *testing.T) {
	for _, row := range loadFormatGolden(t).Iban {
		if got := IBAN(row.Input); got != row.Formatted {
			t.Errorf("IBAN(%q) = %q, want %q", row.Input, got, row.Formatted)
		}
	}
}

func TestFormatGoldenParseDate(t *testing.T) {
	for _, row := range loadFormatGolden(t).ParseDate {
		got, ok := ParseDate(row.Input)
		if row.Parsed == nil {
			if ok {
				t.Errorf("ParseDate(%q) = %v, want failure", row.Input, got)
			}
			continue
		}
		if !ok || got.String() != *row.Parsed {
			t.Errorf("ParseDate(%q) = %v, %v; want %q", row.Input, got, ok, *row.Parsed)
		}
	}
}

func TestFormatGoldenParseDecimal(t *testing.T) {
	for _, row := range loadFormatGolden(t).ParseDecimal {
		got, ok := ParseDecimal(row.Input)
		if row.Parsed == nil {
			if ok {
				t.Errorf("ParseDecimal(%q) = %q, want failure", row.Input, got.String())
			}
			continue
		}
		if !ok || got.String() != *row.Parsed {
			t.Errorf("ParseDecimal(%q) = %q, %v; want %q", row.Input, got.String(), ok, *row.Parsed)
		}
	}
}

func TestFormatGoldenParseAmount(t *testing.T) {
	for _, row := range loadFormatGolden(t).ParseAmount {
		got, ok := ParseAmount(row.Input)
		if row.Parsed == nil {
			if ok {
				t.Errorf("ParseAmount(%q) = %q, want failure", row.Input, got.String())
			}
			continue
		}
		if !ok || got.String() != *row.Parsed {
			t.Errorf("ParseAmount(%q) = %q, %v; want %q", row.Input, got.String(), ok, *row.Parsed)
		}
	}
}

func TestFormatGoldenMoneyParseDecimal(t *testing.T) {
	for _, row := range loadFormatGolden(t).MoneyParseDecimal {
		got, err := money.ParseDecimal(row.Input, currency(t, row.Currency))
		if (err == nil) != row.Ok {
			t.Errorf("ParseDecimal(%q, %s) = %+v, %v; want ok=%v", row.Input, row.Currency, got, err, row.Ok)
			continue
		}
		if !row.Ok {
			if row.Error == nil || err == nil || err.Error() != *row.Error {
				t.Errorf("ParseDecimal(%q, %s) error = %v, want %v", row.Input, row.Currency, err, row.Error)
			}
			continue
		}
		if row.Minor == nil || got.Minor != *row.Minor {
			t.Errorf("ParseDecimal(%q, %s).Minor = %d, want %v", row.Input, row.Currency, got.Minor, row.Minor)
		}
		if row.Plain == nil || money.FormatPlain(got) != *row.Plain {
			t.Errorf("FormatPlain(%q, %s) = %q, want %v", row.Input, row.Currency, money.FormatPlain(got), row.Plain)
		}
	}
}

func TestFormatGoldenFormatPlain(t *testing.T) {
	for _, row := range loadFormatGolden(t).FormatPlain {
		value := money.Money{Minor: row.Minor, Currency: currency(t, row.Currency)}
		if got := money.FormatPlain(value); got != row.Plain {
			t.Errorf("FormatPlain(%d %s) = %q, want %q", row.Minor, row.Currency, got, row.Plain)
		}
	}
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
