package receipt

import (
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func today(t *testing.T) civil.Date {
	t.Helper()
	date, ok := civil.New(2026, 11, 7)
	if !ok {
		t.Fatal("invalid test date")
	}
	return date
}

func eur(minor int64) money.Money {
	return money.Money{Minor: minor, Currency: money.EUR}
}

func parseEUR(t *testing.T, text string) Fields {
	t.Helper()
	return Parse(text, money.EUR, today(t))
}

func dateOf(t *testing.T, text string) *civil.Date {
	t.Helper()
	return parseEUR(t, text).Date
}

func testDate(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()
	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", year, month, day)
	}
	return value
}

func checkDate(t *testing.T, text string, want civil.Date) {
	t.Helper()
	got := dateOf(t, text)
	if got == nil || *got != want {
		t.Errorf("date_of(%q) = %v, want %v", text, got, want)
	}
}

func checkNoDate(t *testing.T, text string) {
	t.Helper()
	if got := dateOf(t, text); got != nil {
		t.Errorf("date_of(%q) = %v, want nil", text, got)
	}
}

func checkMoney(t *testing.T, label string, got *money.Money, want money.Money) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestTwoDigitYearsUseTheSharedTwoDigitYearRule(t *testing.T) {
	checkDate(t, "Datum 05.03.99", testDate(t, 1999, time.March, 5))
	checkDate(t, "Datum 05.03.26", testDate(t, 2026, time.March, 5))
}

func TestSlashDatesKeepTheDayFirstReadingUnlessItIsInvalid(t *testing.T) {
	checkDate(t, "Datum 01/05/2026", testDate(t, 2026, time.May, 1))
	checkDate(t, "Datum 05/13/2026", testDate(t, 2026, time.May, 13))
}

func TestAnInvalidDateAloneDoesNotCrashAndYieldsNoDate(t *testing.T) {
	checkNoDate(t, "Datum 31.02.26")
}

func TestReceiptParserHandlesGermanAndEnglishFormats(t *testing.T) {
	german := parseEUR(t, "EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95")
	wantTotal := eur(129_995)
	checkMoney(t, "german.total", german.Total, wantTotal)
	checkDate(t, "EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95", testDate(t, 2026, time.October, 3))
	for _, amount := range german.Amounts {
		if amount == eur(310) {
			t.Error("date fragments are not amounts")
		}
	}

	english := parseEUR(t, "Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00")
	checkMoney(t, "english.total", english.Total, eur(119_000))
	checkDate(t, "Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00", testDate(t, 2026, time.January, 5))

	if got := parseEUR(t, "no numbers here").Total; got != nil {
		t.Errorf("no numbers total = %v, want nil", got)
	}
}

func TestTheLastKeywordLineWinsAndOtherwiseTheMaximumIsTheTotal(t *testing.T) {
	keyword := parseEUR(t, "Zwischensumme 1,00\nSUMME 2,00\nTotal 3,00")
	checkMoney(t, "keyword.total", keyword.Total, eur(300))
	wantAmounts := []money.Money{eur(100), eur(200), eur(300)}
	if len(keyword.Amounts) != len(wantAmounts) {
		t.Fatalf("keyword.amounts = %v, want %v", keyword.Amounts, wantAmounts)
	}
	for index := range wantAmounts {
		if keyword.Amounts[index] != wantAmounts[index] {
			t.Errorf("keyword.amounts[%d] = %v, want %v", index, keyword.Amounts[index], wantAmounts[index])
		}
	}

	withoutKeyword := parseEUR(t, "Artikel 1,00\nArtikel 2,50\nArtikel 2,00")
	checkMoney(t, "without_keyword.total", withoutKeyword.Total, eur(250))
}

func TestGroupingAndDecimalSeparatorsAreDistinguished(t *testing.T) {
	checkMoney(t, "1.234,56", parseEUR(t, "Betrag 1.234,56").Total, eur(123_456))
	checkMoney(t, "1,234.56", parseEUR(t, "Betrag 1,234.56").Total, eur(123_456))
	checkMoney(t, "1234.56", parseEUR(t, "Betrag 1234.56").Total, eur(123_456))
	if got := parseEUR(t, "Betrag 1.234.56").Total; got != nil {
		t.Errorf("1.234.56 total = %v, want nil", got)
	}
}

func TestAmountsRespectTheCurrencyExponentWithoutRounding(t *testing.T) {
	jpy, err := money.NewCurrency("JPY")
	if err != nil {
		t.Fatal(err)
	}
	japanese := Parse("SUMME 12,34", jpy, today(t))
	if japanese.Total != nil {
		t.Errorf("japanese.total = %v, want nil", japanese.Total)
	}
	if len(japanese.Amounts) != 0 {
		t.Errorf("japanese.amounts = %v, want empty", japanese.Amounts)
	}

	kwd, err := money.NewCurrency("KWD")
	if err != nil {
		t.Fatal(err)
	}
	dinar := Parse("SUMME 12,34", kwd, today(t))
	checkMoney(t, "dinar.total", dinar.Total, money.Money{Minor: 12_340, Currency: kwd})
}

func TestDatesKeepTheKotlinPrecedenceAndSmartClamping(t *testing.T) {
	checkDate(t, "Datum 31.02.2026", testDate(t, 2026, time.February, 28))
	checkDate(t, "Datum 2026-04-31", testDate(t, 2026, time.April, 30))
	checkDate(t, "Datum 13.01.2026", testDate(t, 2026, time.January, 13))
	checkDate(t, "Datum 1999-12-31", testDate(t, 1999, time.December, 31))
	checkNoDate(t, "Datum 1899-12-31")
}

func TestNeverPanicsOnRandomOrTruncatedInput(t *testing.T) {
	fixtures := []string{
		"EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95",
		"Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00",
		"REWE Markt GmbH\nDatum: 03.10.2026\nMilch 1,29\nBrot 2,49\nSUMME EUR 3,78",
		"Datum 05.03.99 / 05/13/2026 / 31.02.2026",
		"",
	}

	pool := []rune{
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.', ',', '/', ' ', '\t', '\n', '\r', 'a', 'E', 'U', 'R',
		':', '&', '\u0000', '\u007f', '\uFFFF', '€',
	}

	state := uint64(0x5eed_1234_abcd_9876)
	next := func() uint64 {
		state = state*6_364_136_223_846_793_005 + 1_442_695_040_888_963_407
		return state
	}

	jpy, err := money.NewCurrency("JPY")
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2_000; iteration++ {
		length := int(next() % 48)
		text := make([]rune, 0, length)
		for index := 0; index < length; index++ {
			text = append(text, pool[next()%uint64(len(pool))])
		}
		_ = Parse(string(text), money.EUR, today(t))
		_ = Parse(string(text), jpy, today(t))
	}

	for _, fixture := range fixtures {
		for index := range fixture {
			_ = Parse(fixture[:index], money.EUR, today(t))
		}
		_ = Parse(fixture, money.EUR, today(t))
	}
}
