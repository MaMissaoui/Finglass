package imports

import (
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

type csvOwnHeader struct {
	date     string
	partner  string
	purpose  string
	category string
	amount   string
	currency string
	account  string
	balance  string
	shop     string
}

var csvOwnLanguages = []csvOwnHeader{
	{"Date", "Partner", "Purpose", "Category", "Amount", "Currency", "Account", "Balance", "Shop"},
	{"Datum", "Partner", "Verwendungszweck", "Kategorie", "Betrag", "Währung", "Konto", "Saldo", "Händler"},
	{"Date", "Partenaire", "Libellé", "Catégorie", "Montant", "Devise", "Compte", "Solde", "Marchand"},
}

func csvOwnEscape(field string) string {
	if strings.ContainsAny(field, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(field, "\"", "\"\"") + "\""
	}
	return field
}

func csvOwnGuard(field string) string {
	characters := []rune(field)
	if len(characters) > 0 && strings.ContainsRune("=+-@\t\r", characters[0]) {
		return "'" + field
	}
	return field
}

func csvOwnRow(fields []string) string {
	escaped := make([]string, 0, len(fields))
	for _, field := range fields {
		escaped = append(escaped, csvOwnEscape(field))
	}
	return strings.Join(escaped, ",") + "\r\n"
}

type csvOwnBooking struct {
	index   int
	date    string
	amount  string
	partner string
	purpose string
}

func csvOwnExport(language csvOwnHeader, transactions []csvOwnBooking, balances []decimal.Decimal, hasBalances bool) string {
	header := []string{
		language.date,
		language.partner,
		language.purpose,
		language.category,
		language.amount,
		language.currency,
		language.account,
	}
	if hasBalances {
		header = append(header, language.balance)
	}
	header = append(header, language.shop)
	var text strings.Builder
	text.WriteString(csvOwnRow(header))
	for index, transaction := range transactions {
		fields := []string{
			transaction.date,
			csvOwnGuard(transaction.partner),
			csvOwnGuard(transaction.purpose),
			"",
			transaction.amount,
			"EUR",
			"DE89370400440532013000",
		}
		if hasBalances {
			fields = append(fields, balances[index].String())
		}
		fields = append(fields, "")
		text.WriteString(csvOwnRow(fields))
	}
	return text.String()
}

func csvOwnBookings() []csvOwnBooking {
	return []csvOwnBooking{
		{1, "2026-10-01", "2600.00", "Beispiel AG", "Gehalt Oktober"},
		{2, "2026-10-02", "-42.50", "Beispiel Markt", "Einkauf 4711"},
		{3, "2026-10-03", "-7.25", "Beispiel Laden", "Einkauf \"Spezial\", Teil 2"},
	}
}

func TestOwnExportRoundTripsInEveryLanguage(t *testing.T) {
	bookings := csvOwnBookings()
	for _, language := range csvOwnLanguages {
		text := csvOwnExport(language, bookings, nil, false)
		parsed, err := ParseCSV([]byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Bookings) != 3 {
			t.Fatalf("%s: bookings = %d, skipped = %v", language.date, len(parsed.Bookings), parsed.Skipped)
		}
		if len(parsed.Skipped) != 0 {
			t.Errorf("%s: skipped = %v", language.date, parsed.Skipped)
		}
		for index, booking := range bookings {
			reimported := parsed.Bookings[index]
			if reimported.Date.String() != booking.date {
				t.Errorf("%s: date = %s, want %s", language.date, reimported.Date, booking.date)
			}
			if got, want := reimported.Amount, dec(t, booking.amount); got.Cmp(want) != 0 {
				t.Errorf("%s: amount = %s, want %s", language.date, got, want)
			}
			if reimported.Currency == nil || *reimported.Currency != "EUR" {
				t.Errorf("%s: currency = %v", language.date, reimported.Currency)
			}
			if reimported.PartnerName == nil || *reimported.PartnerName != booking.partner {
				t.Errorf("%s: partner = %v", language.date, reimported.PartnerName)
			}
			if reimported.Purpose == nil || *reimported.Purpose != booking.purpose {
				t.Errorf("%s: purpose = %v", language.date, reimported.Purpose)
			}
		}
	}
}

func TestOwnExportRoundTripsTheBalanceColumnInEveryLanguage(t *testing.T) {
	bookings := csvOwnBookings()
	balances := []decimal.Decimal{dec(t, "2600.00"), dec(t, "2557.50"), dec(t, "2550.25")}
	for _, language := range csvOwnLanguages {
		text := csvOwnExport(language, bookings, balances, true)
		parsed, err := ParseCSV([]byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Bookings) != 3 {
			t.Fatalf("%s: bookings = %d, skipped = %v", language.date, len(parsed.Bookings), parsed.Skipped)
		}
		for index, reimported := range parsed.Bookings {
			if reimported.BalanceAfter == nil || reimported.BalanceAfter.Cmp(balances[index]) != 0 {
				t.Errorf("%s: balance[%d] = %v, want %s", language.date, index, reimported.BalanceAfter, balances[index])
			}
		}
	}
}

func TestOwnExportRoundTripsTextThatTheSpreadsheetGuardPrefixes(t *testing.T) {
	guarded := []csvOwnBooking{
		{4, "2026-10-04", "-1.00", "+Shop", "- Rate 3/12"},
		{5, "2026-10-05", "-2.00", "@home", "=SUM(A1)"},
	}
	text := csvOwnExport(csvOwnLanguages[0], guarded, nil, false)
	parsed, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Bookings) != 2 {
		t.Fatalf("bookings = %d, skipped = %v", len(parsed.Bookings), parsed.Skipped)
	}
	for index, booking := range guarded {
		reimported := parsed.Bookings[index]
		if reimported.PartnerName == nil || *reimported.PartnerName != booking.partner {
			t.Errorf("partner[%d] = %v, want %s", index, reimported.PartnerName, booking.partner)
		}
		if reimported.Purpose == nil || *reimported.Purpose != booking.purpose {
			t.Errorf("purpose[%d] = %v, want %s", index, reimported.Purpose, booking.purpose)
		}
	}
}

func TestOnlyTheSpreadsheetGuardApostropheIsRemovedFromText(t *testing.T) {
	text := "Datum;Betrag;Partner;Verwendungszweck\n03.10.2026;-1,00;'abc;'abc\n"
	parsed, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(parsed.Bookings))
	}
	if parsed.Bookings[0].PartnerName == nil || *parsed.Bookings[0].PartnerName != "'abc" {
		t.Errorf("partner = %v", parsed.Bookings[0].PartnerName)
	}
	if parsed.Bookings[0].Purpose == nil || *parsed.Bookings[0].Purpose != "'abc" {
		t.Errorf("purpose = %v", parsed.Bookings[0].Purpose)
	}
}

func TestBankHeadersKeepTheirMapping(t *testing.T) {
	mapping := DetectMapping([]string{
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
	if !mappingEquals(mapping, CsvMapping{
		Date: intPointer(0), ValueDate: intPointer(1), Amount: intPointer(8),
		Partner: intPointer(4), Payer: intPointer(3), PartnerIBAN: intPointer(7), Purpose: intPointer(5),
	}) {
		t.Errorf("mapping 1 = %+v", mapping)
	}
	mapping = DetectMapping([]string{"Datum", "Name", "Partner", "Verwendungszweck", "Betrag"})
	if !mappingEquals(mapping, CsvMapping{Date: intPointer(0), Amount: intPointer(4), Partner: intPointer(1), Purpose: intPointer(3)}) {
		t.Errorf("mapping 2 = %+v", mapping)
	}
	mapping = DetectMapping([]string{"Date", "Payee", "Purpose", "Amount", "Currency", "Partner"})
	if !mappingEquals(mapping, CsvMapping{
		Date: intPointer(0), Amount: intPointer(3), Partner: intPointer(1), Purpose: intPointer(2), Currency: intPointer(4),
	}) {
		t.Errorf("mapping 3 = %+v", mapping)
	}
}

func TestFallbacksAreExactMatchOnly(t *testing.T) {
	if mapping := DetectMapping([]string{"Datum", "Partnerbank", "Betrag"}); mapping.Partner != nil {
		t.Errorf("partner = %v", mapping.Partner)
	}
	mapping := DetectMapping([]string{"Date", "Montants", "Libellé", "Amount"})
	if mapping.Amount == nil || *mapping.Amount != 3 {
		t.Errorf("amount = %v", mapping.Amount)
	}
}

func TestBankNorwegianRuleStillClearsTheCurrency(t *testing.T) {
	mapping := DetectMapping([]string{"BookDate", "Amount", "Currency Amount", "Currency", "Devise"})
	if mapping.Currency != nil {
		t.Errorf("currency = %v", mapping.Currency)
	}
}

func mappingEquals(left, right CsvMapping) bool {
	return sameIntPointer(left.Date, right.Date) &&
		sameIntPointer(left.ValueDate, right.ValueDate) &&
		sameIntPointer(left.Amount, right.Amount) &&
		sameIntPointer(left.Debit, right.Debit) &&
		sameIntPointer(left.Credit, right.Credit) &&
		sameIntPointer(left.Partner, right.Partner) &&
		sameIntPointer(left.Payer, right.Payer) &&
		sameIntPointer(left.PartnerIBAN, right.PartnerIBAN) &&
		sameIntPointer(left.Purpose, right.Purpose) &&
		sameIntPointer(left.Currency, right.Currency) &&
		sameIntPointer(left.Balance, right.Balance)
}

func sameIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
