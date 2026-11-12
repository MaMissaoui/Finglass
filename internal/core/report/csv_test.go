package report

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/imports"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

func TestCSVExportQuotesFieldsAndNeutralisesFormulas(t *testing.T) {
	d := reportFixture(t)
	file := filepath.Join(t.TempDir(), "out.csv")
	if err := ExportYearToCSV(context.Background(), d, 2026, file, i18n.En); err != nil {
		t.Fatalf("ExportYearToCSV: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the CSV: %v", err)
	}
	if !bytes.Equal(content[:3], utf8BOM) {
		t.Fatal("the file does not start with the UTF-8 BOM")
	}
	text := string(content[3:])
	if !strings.HasPrefix(text, "Date,Partner,Purpose") {
		t.Fatalf("the header is not the report header: %q", text)
	}
	if !strings.HasSuffix(strings.Split(text, "\r\n")[0], ",Shop") {
		t.Fatalf("the Shop column is last: %s", text)
	}
	if !strings.Contains(text, `"'=HYPERLINK(""http://evil"")"`) {
		t.Fatalf("the formula guard is missing: %s", text)
	}
	if !strings.Contains(text, `"Monitor, 27"""`) {
		t.Fatalf("the comma/quote escaping is missing: %s", text)
	}
	if !strings.Contains(text, ",-250.00,EUR,") {
		t.Fatalf("amounts keep their sign: %s", text)
	}
	if count := len(strings.Split(strings.TrimSpace(text), "\n")); count != 4 {
		t.Fatalf("line count = %d, want 4 (header + 3 rows)", count)
	}
}

func TestCSVExportNeutralisesAccountIBANFormulas(t *testing.T) {
	file := filepath.Join(t.TempDir(), "out.csv")
	transaction := item(1, -1_000).Partner("Shop").Purpose("Purpose").Build()
	transaction.AccountIBAN = strPtr("@TACKER-123")
	if err := ExportToCSV([]model.TransactionItem{transaction}, file, nil, i18n.En); err != nil {
		t.Fatalf("ExportToCSV: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the CSV: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, ",'@TACKER-123") {
		t.Fatalf("account IBAN formula is neutralised: %s", text)
	}
	if got := (CsvFormat{}).Text("=1+1"); got != "'=1+1" {
		t.Fatalf("CsvFormat.Text(\"=1+1\") = %q, want %q", got, "'=1+1")
	}
}

func paypalBooking(t *testing.T, id int64) model.TransactionItem {
	t.Helper()
	booking := item(id, -1_999).
		Date(testDate(t, 2026, time.March, 10)).
		Partner("PayPal Europe S.a.r.l. et Cie S.C.A").
		Purpose("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH").
		Build()
	return booking
}

func plainBooking(t *testing.T, id int64) model.TransactionItem {
	t.Helper()
	return item(id, -450).
		Date(testDate(t, 2026, time.March, 11)).
		Partner("Beispiel Bäckerei").
		Purpose("Brötchen").
		Build()
}

func TestCSVExportAddsTheShopLastAndKeepsTheRawPartner(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shop.csv")
	paypal := paypalBooking(t, 1)
	plain := plainBooking(t, 2)
	if err := ExportToCSV([]model.TransactionItem{paypal, plain}, file, nil, i18n.En); err != nil {
		t.Fatalf("ExportToCSV: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the CSV: %v", err)
	}
	records := imports.SplitRecords(strings.TrimPrefix(string(content), "\uFEFF"), ',', math.MaxInt)
	if !slices.Equal(records[0], []string{
		"Date", "Partner", "Purpose", "Category", "Amount", "Currency", "Account", "Shop",
	}) {
		t.Fatalf("header = %v", records[0])
	}
	if records[1][1] != "PayPal Europe S.a.r.l. et Cie S.C.A" {
		t.Fatalf("partner = %q; the raw partner stays in column 2", records[1][1])
	}
	if records[1][len(records[1])-1] != "Beispiel Shop GmbH" {
		t.Fatalf("shop = %q; the shop is the last field", records[1][len(records[1])-1])
	}
	if records[2][len(records[2])-1] != "" {
		t.Fatalf("shop = %q; a non-PayPal booking has no shop", records[2][len(records[2])-1])
	}
}

func TestCSVExportPutsTheShopAfterTheBalance(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shop-balances.csv")
	paypal := paypalBooking(t, 1)
	plain := plainBooking(t, 2)
	balances := map[model.TransactionID]money.Money{
		paypal.ID: eur(10_000),
		plain.ID:  eur(9_000),
	}
	if err := ExportToCSV([]model.TransactionItem{paypal, plain}, file, balances, i18n.En); err != nil {
		t.Fatalf("ExportToCSV: %v", err)
	}

	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the CSV: %v", err)
	}
	records := imports.SplitRecords(strings.TrimPrefix(string(content), "\uFEFF"), ',', math.MaxInt)
	if !slices.Equal(records[0], []string{
		"Date", "Partner", "Purpose", "Category", "Amount", "Currency", "Account", "Balance", "Shop",
	}) {
		t.Fatalf("header = %v", records[0])
	}
	if records[1][len(records[1])-2] != "100.00" {
		t.Fatalf("balance = %q; the balance stays before the shop", records[1][len(records[1])-2])
	}
	if records[1][len(records[1])-1] != "Beispiel Shop GmbH" {
		t.Fatalf("shop = %q, want Beispiel Shop GmbH", records[1][len(records[1])-1])
	}
	if records[2][len(records[2])-2] != "90.00" {
		t.Fatalf("balance = %q, want 90.00", records[2][len(records[2])-2])
	}
	if records[2][len(records[2])-1] != "" {
		t.Fatalf("shop = %q, want empty", records[2][len(records[2])-1])
	}
}

func TestLocalizedShopHeadersNeverMapToThePartnerOrPurposeColumn(t *testing.T) {
	paypal := paypalBooking(t, 1)
	for _, lang := range i18n.Languages {
		file := filepath.Join(t.TempDir(), "shop-"+lang.Code()+".csv")
		if err := ExportToCSV([]model.TransactionItem{paypal}, file, nil, lang); err != nil {
			t.Fatalf("%s: ExportToCSV: %v", lang.Code(), err)
		}
		content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
		if err != nil {
			t.Fatalf("%s: read the CSV: %v", lang.Code(), err)
		}
		header := imports.SplitRecords(strings.TrimPrefix(string(content), "\uFEFF"), ',', math.MaxInt)[0]
		mapping := imports.DetectMapping(header)
		last := len(header) - 1

		if header[last] != i18n.Text(lang, i18n.ColShop) {
			t.Fatalf("%s: the Shop header = %q, want %q", lang.Code(), header[last], i18n.Text(lang, i18n.ColShop))
		}
		if withoutShop := imports.DetectMapping(header[:last]); !mappingEqual(withoutShop, mapping) {
			t.Fatalf("%s: mapping without vs. with Shop differ: %+v vs. %+v", lang.Code(), withoutShop, mapping)
		}
		if !optionalIntEquals(mapping.Date, 0) {
			t.Fatalf("%s: date = %v, want 0", lang.Code(), mapping.Date)
		}
		for name, value := range map[string]*int{
			"partner": mapping.Partner,
			"purpose": mapping.Purpose,
			"payer":   mapping.Payer,
		} {
			if value != nil && *value == last {
				t.Fatalf("%s: the Shop column is the %s", lang.Code(), name)
			}
		}
	}
}

func TestTheShopHeaderLeavesRecognizedImportColumnsWhereTheyWere(t *testing.T) {
	english := imports.DetectMapping([]string{
		"Date",
		"Payee",
		"Purpose",
		"Amount",
		"Shop",
	})
	if !optionalIntEquals(english.Partner, 1) {
		t.Fatalf("EN partner = %v, want 1; EN partner stays on its column", english.Partner)
	}
	if !optionalIntEquals(english.Purpose, 2) {
		t.Fatalf("EN purpose = %v, want 2; EN purpose stays on its column", english.Purpose)
	}
	if english.Partner != nil && *english.Partner == 4 {
		t.Fatal("the EN partner is the Shop column")
	}
	if english.Purpose != nil && *english.Purpose == 4 {
		t.Fatal("the EN purpose is the Shop column")
	}

	german := imports.DetectMapping([]string{
		"Buchungstag",
		"Zahlungsempfänger",
		"Verwendungszweck",
		"Betrag",
		"Händler",
	})
	if !optionalIntEquals(german.Partner, 1) {
		t.Fatalf("DE partner = %v, want 1; DE partner stays on its column", german.Partner)
	}
	if !optionalIntEquals(german.Purpose, 2) {
		t.Fatalf("DE purpose = %v, want 2; DE purpose stays on its column", german.Purpose)
	}
	if german.Partner != nil && *german.Partner == 4 {
		t.Fatal("the DE partner is the Shop column")
	}
	if german.Purpose != nil && *german.Purpose == 4 {
		t.Fatal("the DE purpose is the Shop column")
	}
}

func TestCSVFormatEscapesOnlyWhenNeeded(t *testing.T) {
	format := CsvFormat{}
	if got := format.Escape("plain"); got != "plain" {
		t.Fatalf("Escape(plain) = %q", got)
	}
	if got := format.Escape("a,b"); got != `"a,b"` {
		t.Fatalf("Escape(a,b) = %q", got)
	}
	if got := format.Text("+49 123"); got != "'+49 123" {
		t.Fatalf("Text(+49 123) = %q", got)
	}
	if got := format.Text(""); got != "" {
		t.Fatalf("Text(empty) = %q", got)
	}
}

func roundTripBookings(t *testing.T) []model.TransactionItem {
	t.Helper()
	return []model.TransactionItem{
		roundTripItem(t, 1, testDate(t, 2026, time.October, 1), 260_000, "Beispiel AG", "Gehalt Oktober"),
		roundTripItem(t, 2, testDate(t, 2026, time.October, 2), -4_250, "Beispiel Markt", "Einkauf 4711"),
		roundTripItem(
			t, 3, testDate(t, 2026, time.October, 3), -725,
			"Beispiel Laden", `Einkauf "Spezial", Teil 2`,
		),
	}
}

func roundTripItem(t *testing.T, id int64, day civil.Date, minor int64, partner, purpose string) model.TransactionItem {
	t.Helper()
	return item(id, minor).Date(day).Partner(partner).Purpose(purpose).Build()
}

func decimalOf(t *testing.T, minor int64) decimal.Decimal {
	t.Helper()
	value, err := decimal.Parse(money.FormatPlain(eur(minor)))
	if err != nil {
		t.Fatalf("parse %q: %v", money.FormatPlain(eur(minor)), err)
	}
	return value
}

func TestOwnExportRoundTripsInEveryLanguage(t *testing.T) {
	bookings := roundTripBookings(t)
	for _, lang := range i18n.Languages {
		file := filepath.Join(t.TempDir(), "own-"+lang.Code()+".csv")
		if err := ExportToCSV(bookings, file, nil, lang); err != nil {
			t.Fatalf("%s: ExportToCSV: %v", lang.Code(), err)
		}
		content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
		if err != nil {
			t.Fatalf("%s: read the CSV: %v", lang.Code(), err)
		}
		parsed, err := imports.ParseCSV(content, nil)
		if err != nil {
			t.Fatalf("%s: ParseCSV: %v", lang.Code(), err)
		}

		if len(parsed.Bookings) != 3 {
			t.Fatalf("%s: bookings = %d, want 3, skipped %v", lang.Code(), len(parsed.Bookings), parsed.Skipped)
		}
		if len(parsed.Skipped) != 0 {
			t.Fatalf("%s: skipped = %v, want none", lang.Code(), parsed.Skipped)
		}
		for index, exported := range bookings {
			reimported := parsed.Bookings[index]
			if exported.Date != reimported.Date {
				t.Fatalf("%s: date = %v, want %v", lang.Code(), reimported.Date, exported.Date)
			}
			if reimported.Amount.Cmp(decimalOf(t, exported.Amount.Minor)) != 0 {
				t.Fatalf(
					"%s: amount = %v, want %v",
					lang.Code(), reimported.Amount, decimalOf(t, exported.Amount.Minor),
				)
			}
			if reimported.Currency == nil || *reimported.Currency != exported.Currency.String() {
				t.Fatalf("%s: currency = %v, want %q", lang.Code(), reimported.Currency, exported.Currency.String())
			}
			if !sameOptionalString(exported.PartnerName, reimported.PartnerName) {
				t.Fatalf("%s: partner = %v, want %v", lang.Code(), reimported.PartnerName, exported.PartnerName)
			}
			if !sameOptionalString(exported.Purpose, reimported.Purpose) {
				t.Fatalf("%s: purpose = %v, want %v", lang.Code(), reimported.Purpose, exported.Purpose)
			}
		}
	}
}

func TestOwnExportRoundTripsTheBalanceColumnInEveryLanguage(t *testing.T) {
	bookings := roundTripBookings(t)
	balances := map[model.TransactionID]money.Money{
		bookings[0].ID: eur(260_000),
		bookings[1].ID: eur(255_750),
		bookings[2].ID: eur(255_025),
	}
	for _, lang := range i18n.Languages {
		file := filepath.Join(t.TempDir(), "own-balances-"+lang.Code()+".csv")
		if err := ExportToCSV(bookings, file, balances, lang); err != nil {
			t.Fatalf("%s: ExportToCSV: %v", lang.Code(), err)
		}
		content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
		if err != nil {
			t.Fatalf("%s: read the CSV: %v", lang.Code(), err)
		}
		parsed, err := imports.ParseCSV(content, nil)
		if err != nil {
			t.Fatalf("%s: ParseCSV: %v", lang.Code(), err)
		}

		if len(parsed.Bookings) != 3 {
			t.Fatalf("%s: bookings = %d, want 3, skipped %v", lang.Code(), len(parsed.Bookings), parsed.Skipped)
		}
		for index, exported := range bookings {
			reimported := parsed.Bookings[index]
			if reimported.BalanceAfter == nil {
				t.Fatalf("%s: the balance was parsed", lang.Code())
			}
			balance := balances[exported.ID]
			if reimported.BalanceAfter.Cmp(decimalOf(t, balance.Minor)) != 0 {
				t.Fatalf(
					"%s: balance = %v, want %v",
					lang.Code(), *reimported.BalanceAfter, decimalOf(t, balance.Minor),
				)
			}
		}
	}
}

func TestOwnExportRoundTripsTextThatTheSpreadsheetGuardPrefixes(t *testing.T) {
	guarded := []model.TransactionItem{
		roundTripItem(t, 4, testDate(t, 2026, time.October, 4), -100, "+Shop", "- Rate 3/12"),
		roundTripItem(t, 5, testDate(t, 2026, time.October, 5), -200, "@home", "=SUM(A1)"),
	}
	file := filepath.Join(t.TempDir(), "own-guarded.csv")
	if err := ExportToCSV(guarded, file, nil, i18n.En); err != nil {
		t.Fatalf("ExportToCSV: %v", err)
	}
	content, err := os.ReadFile(file) // #nosec G304 -- the test's own temp path.
	if err != nil {
		t.Fatalf("read the CSV: %v", err)
	}
	parsed, err := imports.ParseCSV(content, nil)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}

	if len(parsed.Bookings) != 2 {
		t.Fatalf("bookings = %d, want 2, skipped %v", len(parsed.Bookings), parsed.Skipped)
	}
	for index, exported := range guarded {
		reimported := parsed.Bookings[index]
		if !sameOptionalString(exported.PartnerName, reimported.PartnerName) {
			t.Fatalf("partner = %v, want %v", reimported.PartnerName, exported.PartnerName)
		}
		if !sameOptionalString(exported.Purpose, reimported.Purpose) {
			t.Fatalf("purpose = %v, want %v", reimported.Purpose, exported.Purpose)
		}
	}
}

func TestOnlyTheSpreadsheetGuardApostropheIsRemovedFromText(t *testing.T) {
	csv := "Datum;Betrag;Partner;Verwendungszweck\n03.10.2026;-1,00;'abc;'abc\n"
	parsed, err := imports.ParseCSV([]byte(csv), nil)
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}

	if len(parsed.Bookings) != 1 {
		t.Fatalf("bookings = %d, want 1", len(parsed.Bookings))
	}
	if !sameOptionalString(parsed.Bookings[0].PartnerName, strPtr("'abc")) {
		t.Fatalf("partner = %v, want 'abc", parsed.Bookings[0].PartnerName)
	}
	if !sameOptionalString(parsed.Bookings[0].Purpose, strPtr("'abc")) {
		t.Fatalf("purpose = %v, want 'abc", parsed.Bookings[0].Purpose)
	}
}

func TestBankHeadersKeepTheirMapping(t *testing.T) {
	mapping := imports.DetectMapping([]string{
		"Buchungsdatum",
		"Wertstellung",
		"Status",
		"Zahlungspflichtige*r",
		"Zahlungsempfänger*in",
		"Verwendungszweck",
		"Umsatztyp",
		"IBAN",
		"Betrag (€)",
		"Gläubiger-ID",
		"Mandatsreferenz",
		"Kundenreferenz",
	})
	requireOptionalInt(t, "date", mapping.Date, 0)
	requireOptionalInt(t, "value date", mapping.ValueDate, 1)
	requireOptionalInt(t, "amount", mapping.Amount, 8)
	requireOptionalInt(t, "partner", mapping.Partner, 4)
	requireOptionalInt(t, "payer", mapping.Payer, 3)
	requireOptionalInt(t, "partner iban", mapping.PartnerIBAN, 7)
	requireOptionalInt(t, "purpose", mapping.Purpose, 5)

	mapping = imports.DetectMapping([]string{
		"Datum",
		"Name",
		"Partner",
		"Verwendungszweck",
		"Betrag",
	})
	requireOptionalInt(t, "date", mapping.Date, 0)
	requireOptionalInt(t, "amount", mapping.Amount, 4)
	requireOptionalInt(t, "partner", mapping.Partner, 1)
	requireOptionalInt(t, "purpose", mapping.Purpose, 3)

	mapping = imports.DetectMapping([]string{
		"Date",
		"Payee",
		"Purpose",
		"Amount",
		"Currency",
		"Partner",
	})
	requireOptionalInt(t, "date", mapping.Date, 0)
	requireOptionalInt(t, "amount", mapping.Amount, 3)
	requireOptionalInt(t, "partner", mapping.Partner, 1)
	requireOptionalInt(t, "purpose", mapping.Purpose, 2)
	requireOptionalInt(t, "currency", mapping.Currency, 4)
}

func TestFallbacksAreExactMatchOnly(t *testing.T) {
	if mapping := imports.DetectMapping([]string{"Datum", "Partnerbank", "Betrag"}); mapping.Partner != nil {
		t.Fatalf("partner = %v, want nil", mapping.Partner)
	}
	mapping := imports.DetectMapping([]string{"Date", "Montants", "Libellé", "Amount"})
	requireOptionalInt(t, "amount", mapping.Amount, 3)
}

func TestBankNorwegianRuleStillClearsTheCurrency(t *testing.T) {
	mapping := imports.DetectMapping([]string{
		"BookDate",
		"Amount",
		"Currency Amount",
		"Currency",
		"Devise",
	})
	if mapping.Currency != nil {
		t.Fatalf("currency = %v, want nil", mapping.Currency)
	}
}

func requireOptionalInt(t *testing.T, label string, value *int, want int) {
	t.Helper()
	if !optionalIntEquals(value, want) {
		t.Fatalf("%s = %v, want %d", label, value, want)
	}
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
