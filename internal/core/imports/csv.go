// Reading bank CSV exports (Sparkasse, ING, DKB, Commerzbank, comdirect, N26,
// … and generic ones). Ports crates/finglass-core/src/import/csv.rs.
package imports

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

var csvDateAliases = []string{
	"buchungstag",
	"buchungsdatum",
	"buchung",
	"eingangstag",
	"bookdate",
	"datum",
	"bookingdate",
	"date",
	"transactiondate",
}

var csvValueDateAliases = []string{
	"valutadatum",
	"valuta",
	"wertstellung",
	"wertstellungsdatum",
	"valuedate",
	"belegdatum",
}

var csvAmountAliases = []string{
	"betrag",
	"betrageur",
	"betrageuro",
	"umsatz",
	"umsatzineur",
	"amount",
	"amounteur",
	"betragineur",
}

var csvDebitAliases = []string{"soll", "sollbetrag", "ausgang", "debit", "lastschrift", "belastung"}

var csvCreditAliases = []string{"haben", "habenbetrag", "eingang", "credit", "gutschrift"}

var csvPartnerAliases = []string{
	"beguenstigterzahlungspflichtiger",
	"namezahlungsbeteiligter",
	"auftraggeberbeguenstigter",
	"zahlungsempfaengerin",
	"zahlungsempfaenger",
	"empfaenger",
	"auftraggeber",
	"partnername",
	"payee",
	"counterparty",
	"name",
}

var csvPayerAliases = []string{"zahlungspflichtiger", "zahlungspflichtigerin", "zahlungspflichtige"}

var csvPendingMarkers = []string{"umsatz vorgemerkt", "vorgemerkt", "pending", "ausstehend", "nicht gebucht"}

var csvPartnerIBANAliases = []string{
	"ibanzahlungsbeteiligter",
	"kontonummeriban",
	"iban",
	"kontonummer",
	"partneriban",
	"counterpartyiban",
}

var csvPurposeAliases = []string{
	"verwendungszweck",
	"purpose",
	"reference",
	"beschreibung",
	"description",
	"buchungstext",
	"umsatzart",
	"vorgang",
	"textverwendungszweck",
	"text",
}

var csvCurrencyAliases = []string{"waehrung", "currency", "whrg"}
var csvBalanceAliases = []string{"saldo", "kontostand", "balance"}

var csvOwnPartnerAliases = []string{"partner", "partenaire"}
var csvOwnAmountAliases = []string{"amount", "betrag", "montant"}
var csvOwnPurposeAliases = []string{"purpose", "verwendungszweck", "libell", "libelle"}
var csvOwnCurrencyAliases = []string{"currency", "waehrung", "devise"}
var csvOwnBalanceAliases = []string{"balance", "saldo", "solde"}

// ParseCSV reads a CSV file, using today's date for two-digit years.
func ParseCSV(bytes []byte, mapping *CsvMapping) (ParsedStatement, error) {
	return ParseCSVWithToday(bytes, mapping, civil.FromTime(time.Now().UTC()))
}

// ParseCSVWithToday reads a CSV file with an explicit "today" for two-digit
// years.
func ParseCSVWithToday(bytes []byte, mapping *CsvMapping, today civil.Date) (ParsedStatement, error) {
	text, set := decodeStatement(bytes)
	delimiter := detectDelimiter(text)
	rows := SplitRecords(text, delimiter, -1)
	return ParseCSVRows(rows, mapping, FormatCSV, set.name, delimiter, today), nil
}

// ParseCSVRows is everything ParseCSV does after decoding and splitting; it is
// also used for XLSX sheets.
func ParseCSVRows(rows [][]string, mapping *CsvMapping, format StatementFormat, charsetName string, delimiter rune, today civil.Date) ParsedStatement {
	headerIndex := findHeader(rows)
	var headers []string
	if headerIndex < len(rows) {
		headers = make([]string, 0, len(rows[headerIndex]))
		for _, cell := range rows[headerIndex] {
			headers = append(headers, strings.TrimSpace(cell))
		}
	} else {
		headers = []string{}
	}
	mappingValue := CsvMapping{}
	if mapping != nil {
		mappingValue = *mapping
	} else {
		mappingValue = DetectMapping(headers)
	}
	layout := CsvLayout{
		Delimiter:   delimiter,
		CharsetName: charsetName,
		HeaderLine:  headerIndex + 1,
		Headers:     headers,
		Mapping:     mappingValue,
	}
	preambleEnd := headerIndex
	if preambleEnd > len(rows) {
		preambleEnd = len(rows)
	}
	preamble := preambleIBAN(rows[:preambleEnd])

	if !mappingValue.IsComplete() {
		return ParsedStatement{
			Format:         format,
			Bookings:       []ImportedBooking{},
			AccountIBAN:    preamble,
			Accounts:       []string{},
			Skipped:        []string{"Date and amount columns not recognised"},
			PendingSkipped: 0,
			Csv:            &layout,
		}
	}

	bookings := []ImportedBooking{}
	skipped := []string{}
	var pending uint32
	for index, row := range rows[headerIndex+1:] {
		if rowAllBlank(row) {
			continue
		}
		if rowHasPendingMarker(row) {
			pending++
			continue
		}
		lineNumber := headerIndex + 2 + index
		booking, reason := toBooking(row, mappingValue, today)
		if reason != "" {
			skipped = append(skipped, fmt.Sprintf("Line %d: %s", lineNumber, reason))
		} else {
			bookings = append(bookings, booking)
		}
	}
	return ParsedStatement{
		Format:         format,
		Bookings:       bookings,
		AccountIBAN:    preamble,
		Accounts:       []string{},
		Skipped:        skipped,
		PendingSkipped: pending,
		Csv:            &layout,
	}
}

func rowAllBlank(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func rowHasPendingMarker(row []string) bool {
	for _, cell := range row {
		trimmed := strings.ToLower(strings.TrimSpace(cell))
		for _, marker := range csvPendingMarkers {
			if trimmed == marker {
				return true
			}
		}
	}
	return false
}

func toBooking(row []string, mapping CsvMapping, today civil.Date) (ImportedBooking, string) {
	cell := func(index *int) *string {
		if index == nil || *index >= len(row) {
			return nil
		}
		value := strings.TrimSpace(row[*index])
		if value == "" {
			return nil
		}
		return &value
	}

	dateValue := cell(mapping.Date)
	date := parseCSVDate(dateValue, today)
	if date == nil {
		value := ""
		if dateValue != nil {
			value = *dateValue
		}
		return ImportedBooking{}, fmt.Sprintf("no valid date '%s'", value)
	}

	var amount decimal.Decimal
	if mapping.Amount != nil {
		amountValue := cell(mapping.Amount)
		parsed, ok := decimalFromOptional(amountValue, parseMoney)
		if !ok {
			value := ""
			if amountValue != nil {
				value = *amountValue
			}
			return ImportedBooking{}, fmt.Sprintf("no valid amount '%s'", value)
		}
		amount = parsed
	} else {
		debit, debitOK := decimalFromOptional(cell(mapping.Debit), parseMoney)
		credit, creditOK := decimalFromOptional(cell(mapping.Credit), parseMoney)
		if debitOK {
			debit = absDecimal(debit)
		}
		if creditOK {
			credit = absDecimal(credit)
		}
		switch {
		case debitOK && !debit.IsZero():
			amount = debit.Neg()
		case debitOK && creditOK:
			amount = credit
		case debitOK:
			amount = debit
		case creditOK:
			amount = credit
		default:
			return ImportedBooking{}, "no debit or credit amount"
		}
	}

	var partnerIBAN *string
	if value := cell(mapping.PartnerIBAN); value != nil {
		normalized := normalizeIBAN(*value)
		if isValidIBAN(normalized) {
			partnerIBAN = &normalized
		}
	}

	var partnerName *string
	var payer *string
	if amount.Cmp(decimal.Zero) > 0 && mapping.Payer != nil {
		payer = cell(mapping.Payer)
	}
	partnerValue := payer
	if partnerValue == nil {
		partnerValue = cell(mapping.Partner)
	}
	if partnerValue != nil {
		guarded := withoutSpreadsheetGuard(*partnerValue)
		partnerName = &guarded
	}

	var purpose *string
	if value := cell(mapping.Purpose); value != nil {
		guarded := collapseWhitespace(withoutSpreadsheetGuard(*value))
		purpose = &guarded
	}

	var currency *string
	if value := cell(mapping.Currency); value != nil {
		upper := upperText.String(*value)
		if len([]rune(upper)) == 3 {
			currency = &upper
		}
	}

	var balanceAfter *decimal.Decimal
	if value, ok := decimalFromOptional(cell(mapping.Balance), parseMoney); ok {
		balanceAfter = &value
	}

	return ImportedBooking{
		Date:          *date,
		ValueDate:     parseCSVDate(cell(mapping.ValueDate), today),
		Amount:        amount,
		Currency:      currency,
		PartnerName:   partnerName,
		PartnerIBAN:   partnerIBAN,
		Purpose:       purpose,
		BalanceAfter:  balanceAfter,
		BankReference: nil,
	}, ""
}

func decimalFromOptional(value *string, parse func(string) (decimal.Decimal, bool)) (decimal.Decimal, bool) {
	if value == nil {
		return decimal.Decimal{}, false
	}
	return parse(*value)
}

func absDecimal(value decimal.Decimal) decimal.Decimal {
	if value.IsZero() {
		return decimal.Zero
	}
	if value.Sign() < 0 {
		return value.Neg()
	}
	return value
}

func parseMoney(s string) (decimal.Decimal, bool) {
	text := strings.ReplaceAll(s, "−", "-")
	text = strings.ReplaceAll(text, "EUR", "")
	text = strings.TrimSpace(text)
	negative := false
	if strings.HasSuffix(text, " S") || strings.HasSuffix(text, "-") {
		negative = true
		text = strings.TrimSuffix(text, " S")
		text = strings.TrimSuffix(text, "-")
	} else if strings.HasSuffix(text, " H") {
		text = strings.TrimSuffix(text, " H")
	}
	value, ok := parseAmount(text)
	if !ok {
		return decimal.Decimal{}, false
	}
	if negative {
		return absDecimal(value).Neg(), true
	}
	return value, true
}

// withoutSpreadsheetGuard undoes only FinDesk's own spreadsheet guard
// apostrophe.
func withoutSpreadsheetGuard(value string) string {
	characters := []rune(value)
	if len(characters) < 2 {
		return value
	}
	second := characters[1]
	if characters[0] == '\'' && strings.ContainsRune("=+-@\t\r", second) {
		return string(characters[1:])
	}
	return value
}

// ParseDate parses a CSV date using today's date for two-digit years.
func ParseDate(value *string) *civil.Date {
	return ParseDateWithToday(value, civil.FromTime(time.Now().UTC()))
}

// ParseDateWithToday parses a CSV date with an explicit "today" for two-digit
// years.
func ParseDateWithToday(value *string, today civil.Date) *civil.Date {
	return parseCSVDate(value, today)
}

func detectDelimiter(text string) rune {
	candidates := []rune{';', ',', '\t', '|'}
	type recordSet struct {
		delimiter rune
		rows      [][]string
	}
	records := make([]recordSet, 0, len(candidates))
	scores := make([]int, 0, len(candidates))
	for _, delimiter := range candidates {
		rows := SplitRecords(text, delimiter, 30)
		records = append(records, recordSet{delimiter: delimiter, rows: rows})
		scores = append(scores, delimiterScore(rows))
	}
	best := 0
	for _, score := range scores {
		if score > best {
			best = score
		}
	}
	if best == 0 {
		return ';'
	}
	tied := make([]rune, 0, len(candidates))
	for index, score := range scores {
		if score == best {
			tied = append(tied, candidates[index])
		}
	}
	for _, delimiter := range tied {
		for _, set := range records {
			if set.delimiter == delimiter && hasHeader(set.rows) {
				return delimiter
			}
		}
	}
	return tied[0]
}

func delimiterScore(records [][]string) int {
	sample := make([][]string, 0, 30)
	for _, row := range records {
		if rowAllBlank(row) {
			continue
		}
		sample = append(sample, row)
		if len(sample) == 30 {
			break
		}
	}
	type count struct {
		size  int
		times int
	}
	counts := []count{}
	best := 0
	for _, row := range sample {
		if len(row) <= 1 {
			continue
		}
		found := false
		for index := range counts {
			if counts[index].size == len(row) {
				counts[index].times++
				found = true
				break
			}
		}
		if !found {
			counts = append(counts, count{size: len(row), times: 1})
		}
	}
	for _, entry := range counts {
		if entry.times > best {
			best = entry.times
		}
	}
	return best
}

func hasHeader(records [][]string) bool {
	for index, row := range records {
		if index >= 30 {
			break
		}
		headers := make([]string, 0, len(row))
		for _, cell := range row {
			headers = append(headers, strings.TrimSpace(cell))
		}
		if DetectMapping(headers).IsComplete() {
			return true
		}
	}
	return false
}

func findHeader(rows [][]string) int {
	for index, row := range rows {
		if index >= 30 {
			break
		}
		headers := make([]string, 0, len(row))
		for _, cell := range row {
			headers = append(headers, strings.TrimSpace(cell))
		}
		if DetectMapping(headers).IsComplete() {
			return index
		}
	}
	return 0
}

// NormalizeHeader lowercases a header, folds umlauts and removes
// non-alphanumerics.
func NormalizeHeader(header string) string {
	text := lowerText.String(header)
	text = strings.ReplaceAll(text, "ä", "ae")
	text = strings.ReplaceAll(text, "ö", "oe")
	text = strings.ReplaceAll(text, "ü", "ue")
	text = strings.ReplaceAll(text, "ß", "ss")
	var out strings.Builder
	for _, character := range text {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			out.WriteRune(character)
		}
	}
	return out.String()
}

func findColumn(norm []string, used []bool, aliases []string, exact bool) *int {
	for _, alias := range aliases {
		found := -1
		for index, value := range norm {
			if !used[index] && value == alias {
				found = index
				break
			}
		}
		if found < 0 && !exact {
			for index, value := range norm {
				if !used[index] && len([]rune(alias)) >= 5 && strings.HasPrefix(value, alias) {
					found = index
					break
				}
			}
		}
		if found >= 0 {
			used[found] = true
			return intPointer(found)
		}
	}
	return nil
}

// DetectMapping detects which header column holds which field.
func DetectMapping(headers []string) CsvMapping {
	norm := make([]string, 0, len(headers))
	for _, header := range headers {
		norm = append(norm, NormalizeHeader(header))
	}
	used := make([]bool, len(norm))

	valueDate := findColumn(norm, used, csvValueDateAliases, false)
	date := findColumn(norm, used, csvDateAliases, false)
	debit := findColumn(norm, used, csvDebitAliases, true)
	credit := findColumn(norm, used, csvCreditAliases, true)
	balance := findColumn(norm, used, csvBalanceAliases, false)
	amount := findColumn(norm, used, csvAmountAliases, false)
	partnerIBAN := findColumn(norm, used, csvPartnerIBANAliases, false)
	payer := findColumn(norm, used, csvPayerAliases, false)
	partner := findColumn(norm, used, csvPartnerAliases, false)
	purpose := findColumn(norm, used, csvPurposeAliases, false)
	currency := findColumn(norm, used, csvCurrencyAliases, false)
	currencyAliasFound := currency != nil

	if currency != nil && containsString(norm, "currencyamount") && containsString(norm, "amount") {
		currency = nil
	}

	if partner == nil && payer == nil {
		partner = findColumn(norm, used, csvOwnPartnerAliases, true)
	}
	if amount == nil && debit == nil && credit == nil {
		amount = findColumn(norm, used, csvOwnAmountAliases, true)
	}
	if purpose == nil {
		purpose = findColumn(norm, used, csvOwnPurposeAliases, true)
	}
	if currency == nil && !currencyAliasFound {
		currency = findColumn(norm, used, csvOwnCurrencyAliases, true)
	}
	if balance == nil {
		balance = findColumn(norm, used, csvOwnBalanceAliases, true)
	}

	finalPartner := partner
	if finalPartner == nil {
		finalPartner = payer
	}
	mapping := CsvMapping{
		Date:        date,
		ValueDate:   valueDate,
		Amount:      amount,
		Partner:     finalPartner,
		PartnerIBAN: partnerIBAN,
		Purpose:     purpose,
		Currency:    currency,
		Balance:     balance,
	}
	if amount == nil {
		mapping.Debit = debit
		mapping.Credit = credit
	}
	if finalPartner != nil {
		mapping.Payer = payer
	}
	return mapping
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func preambleIBAN(preamble [][]string) *string {
	for _, row := range preamble {
		for _, cell := range row {
			for _, candidate := range findIBANs(cell) {
				normalized := normalizeIBAN(candidate)
				if isValidIBAN(normalized) {
					return &normalized
				}
			}
		}
	}
	return nil
}

// SplitRecords splits RFC 4180 records: quoted fields may contain delimiters,
// doubled quotes and newlines. A negative maxNonBlankRecords means unlimited.
func SplitRecords(text string, delimiter rune, maxNonBlankRecords int) [][]string {
	if maxNonBlankRecords == 0 {
		return [][]string{}
	}
	characters := []rune(text)
	records := [][]string{}
	field := strings.Builder{}
	record := []string{}
	inQuotes := false
	nonBlankRecords := 0
	index := 0
	for index < len(characters) {
		character := characters[index]
		switch {
		case inQuotes && character == '"' && index+1 < len(characters) && characters[index+1] == '"':
			field.WriteRune('"')
			index++
		case character == '"' && (inQuotes || strings.TrimSpace(field.String()) == ""):
			inQuotes = !inQuotes
			if inQuotes {
				field.Reset()
			}
		case !inQuotes && character == delimiter:
			record = append(record, field.String())
			field.Reset()
		case !inQuotes && (character == '\n' || character == '\r'):
			if character == '\r' && index+1 < len(characters) && characters[index+1] == '\n' {
				index++
			}
			record = append(record, field.String())
			field.Reset()
			if !rowAllBlank(record) {
				nonBlankRecords++
			}
			records = append(records, record)
			record = []string{}
			if maxNonBlankRecords >= 0 && nonBlankRecords >= maxNonBlankRecords {
				return records
			}
		default:
			field.WriteRune(character)
		}
		index++
	}
	if field.Len() > 0 || len(record) > 0 {
		record = append(record, field.String())
		records = append(records, record)
	}
	return records
}
