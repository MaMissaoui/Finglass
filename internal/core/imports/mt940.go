// Reading SWIFT MT940 account statement files (.sta, .mt940, .txt). Ports
// crates/finglass-core/src/import/mt940.rs.
package imports

import (
	"strconv"
	"strings"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

// LooksLikeMT940 reports whether the bytes look like an MT940 statement.
func LooksLikeMT940(bytes []byte) bool {
	head := decodeISO88591(bytes[:min(len(bytes), 4096)])
	return strings.Contains(head, ":20:") &&
		strings.Contains(head, ":25:") &&
		(strings.Contains(head, ":61:") || strings.Contains(head, ":60F:") || strings.Contains(head, ":62F:"))
}

// ParseMT940 reads an MT940 file, using today's date for two-digit years.
func ParseMT940(bytes []byte) (ParsedStatement, error) {
	return ParseMT940WithToday(bytes, civil.FromTime(time.Now().UTC()))
}

// ParseMT940WithToday reads an MT940 file with an explicit "today" for
// two-digit years.
func ParseMT940WithToday(bytes []byte, today civil.Date) (ParsedStatement, error) {
	if !LooksLikeMT940(bytes) {
		return ParsedStatement{}, Invalid("Not an MT940 file")
	}
	text := decodeText(bytes)
	statements := splitMT940Statements(text)
	bookings := []ImportedBooking{}
	skipped := []string{}
	accounts := []string{}
	var accountIBAN *string
	recordNumber := 0

	for _, statement := range statements {
		if accountID, ok := statementAccountID(statement); ok {
			if !containsString(accounts, accountID) {
				accounts = append(accounts, accountID)
			}
		}
		if accountIBAN == nil {
			accountIBAN = extractIBAN(statement)
		}
		currency := extractCurrency(statement)
		tags := parseTags(statement)

		index := 0
		for index < len(tags) {
			if tags[index].name != "61" {
				index++
				continue
			}
			entryText := tags[index].content
			var infoText *string
			if index+1 < len(tags) && tags[index+1].name == "86" {
				infoText = &tags[index+1].content
				index += 2
			} else {
				index++
			}
			recordNumber++
			booking, reason := parseMT940Record(entryText, infoText, currency, today)
			if reason != "" {
				skipped = append(skipped, "Record "+strconv.Itoa(recordNumber)+": "+reason)
			} else {
				bookings = append(bookings, booking)
			}
		}
	}

	return ParsedStatement{
		Format:         FormatMT940,
		Bookings:       bookings,
		AccountIBAN:    accountIBAN,
		Accounts:       accounts,
		Skipped:        skipped,
		PendingSkipped: 0,
		Csv:            nil,
		FormatDetail:   nil,
		ClosingBalance: nil,
	}, nil
}

func splitMT940Statements(text string) []string {
	blocks := []string{}
	current := strings.Builder{}
	for _, line := range textLines(text) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && allRune(trimmed, '-') {
			if strings.TrimSpace(current.String()) != "" {
				blocks = append(blocks, current.String())
				current.Reset()
			}
		} else {
			current.WriteString(line)
			current.WriteByte('\n')
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		blocks = append(blocks, current.String())
	}
	if len(blocks) == 0 {
		return []string{text}
	}
	return blocks
}

func allRune(text string, wanted rune) bool {
	for _, character := range text {
		if character != wanted {
			return false
		}
	}
	return text != ""
}

type mt940Tag struct {
	name    string
	content string
}

func parseTags(text string) []mt940Tag {
	tags := []mt940Tag{}
	var current *mt940Tag
	for _, line := range textLines(text) {
		if name, content, ok := parseTagLine(line); ok {
			if current != nil {
				tags = append(tags, *current)
			}
			current = &mt940Tag{name: name, content: content}
		} else if current != nil {
			current.content += "\n" + line
		}
	}
	if current != nil {
		tags = append(tags, *current)
	}
	return tags
}

func parseTagLine(line string) (string, string, bool) {
	bytes := []byte(line)
	if len(bytes) < 5 || bytes[0] != ':' {
		return "", "", false
	}
	if bytes[1] < '0' || bytes[1] > '9' || bytes[2] < '0' || bytes[2] > '9' {
		return "", "", false
	}
	colon := 3
	if len(bytes) > 3 && isASCIIAlpha(bytes[3]) {
		colon = 4
	}
	if len(bytes) <= colon || bytes[colon] != ':' {
		return "", "", false
	}
	return line[1:colon], line[colon+1:], true
}

func isASCIIAlpha(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func extractIBAN(text string) *string {
	var raw string
	found := false
	for _, tag := range parseTags(text) {
		if tag.name == "25" {
			raw = strings.TrimSpace(tag.content)
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	candidate := raw
	if index := strings.LastIndex(raw, "/"); index >= 0 {
		candidate = raw[index+1:]
	}
	candidate = strings.TrimSpace(candidate)
	normalized := normalizeIBAN(candidate)
	if isValidIBAN(normalized) {
		return &normalized
	}
	characters := []rune(candidate)
	if len(characters) > 3 {
		tail := characters[len(characters)-3:]
		allAlpha := true
		for _, character := range tail {
			if !isAlphabetic(character) {
				allAlpha = false
				break
			}
		}
		if allAlpha {
			shortened := string(characters[:len(characters)-3])
			normalized := normalizeIBAN(shortened)
			if isValidIBAN(normalized) {
				return &normalized
			}
		}
	}
	return nil
}

func statementAccountID(text string) (string, bool) {
	if iban := extractIBAN(text); iban != nil {
		return *iban, true
	}
	for _, tag := range parseTags(text) {
		if tag.name == "25" {
			raw := strings.TrimSpace(tag.content)
			if raw == "" {
				return "", false
			}
			return raw, true
		}
	}
	return "", false
}

func extractCurrency(text string) *string {
	for _, name := range []string{"60F", "60M", "62F"} {
		if currency, ok := extractBalanceTag(text, name); ok {
			return &currency
		}
	}
	return nil
}

func extractBalanceTag(text string, tagName string) (string, bool) {
	var content string
	found := false
	for _, tag := range parseTags(text) {
		if tag.name == tagName {
			content = strings.TrimSpace(tag.content)
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	characters := []rune(content)
	if len(characters) < 10 {
		return "", false
	}
	currency := string(characters[7:10])
	amountText := string(characters[10:])
	if _, ok := parseMTAmount(strings.TrimSpace(amountText)); !ok {
		return "", false
	}
	return currency, true
}

func parseMT940Record(text string, infoText *string, currency *string, today civil.Date) (ImportedBooking, string) {
	line := strings.TrimSpace(strings.Split(text, "\n")[0])
	characters := []rune(line)
	if len(characters) < 8 {
		return ImportedBooking{}, "too short"
	}
	valueDateText := string(characters[:6])
	valueDate, ok := parseMTDate(valueDateText, today)
	if !ok {
		return ImportedBooking{}, "invalid value date '" + valueDateText + "'"
	}

	position := 6
	var entryDate *civil.Date
	if position+4 <= len(characters) && allASCIIDigits(string(characters[position:position+4])) {
		entryDate = inferBookingDate(valueDate, string(characters[position:position+4]))
		position += 4
	}
	bookingDate := valueDate
	if entryDate != nil {
		bookingDate = *entryDate
	}

	var indicator string
	switch {
	case startsWithAt(characters, position, "RC"):
		position += 2
		indicator = "RC"
	case startsWithAt(characters, position, "RD"):
		position += 2
		indicator = "RD"
	case startsWithAt(characters, position, "C"):
		position++
		indicator = "C"
	case startsWithAt(characters, position, "D"):
		position++
		indicator = "D"
	default:
		return ImportedBooking{}, "missing debit/credit indicator"
	}

	if position < len(characters) && isAlphabetic(characters[position]) {
		position++
	}

	amountStart := position
	for position < len(characters) &&
		(isASCIIDigit(characters[position]) || characters[position] == ',' || characters[position] == '.') {
		position++
	}
	if position == amountStart {
		return ImportedBooking{}, "missing amount"
	}
	rawAmountText := string(characters[amountStart:position])
	rawAmount, ok := parseMTAmount(rawAmountText)
	if !ok {
		return ImportedBooking{}, "invalid amount '" + rawAmountText + "'"
	}
	amount := rawAmount
	if indicator == "D" || indicator == "RD" {
		amount = amount.Neg()
	}

	detailStart := position
	if position < len(characters) && strings.ContainsRune("NSF", characters[position]) {
		detailStart = min(position+4, len(characters))
	}
	detailTail := strings.TrimSpace(string(characters[detailStart:]))
	var bankReference *string
	var customerReference *string
	if before, after, found := strings.Cut(detailTail, "//"); found {
		trimmedAfter := strings.TrimSpace(after)
		if trimmedAfter != "" {
			bankReference = &trimmedAfter
		}
		trimmedBefore := strings.TrimSpace(before)
		customerReference = &trimmedBefore
	} else {
		trimmed := strings.TrimSpace(detailTail)
		customerReference = &trimmed
	}
	if customerReference != nil && (*customerReference == "" || *customerReference == "NONREF") {
		customerReference = nil
	}
	reference := bankReference
	if reference == nil {
		reference = customerReference
	}

	var info *structuredInfo
	if infoText != nil {
		parsed := parse86(*infoText)
		info = &parsed
	}
	var purpose *string
	if info != nil && info.purpose != nil {
		purpose = info.purpose
	} else if infoText != nil {
		trimmed := strings.TrimSpace(*infoText)
		if trimmed != "" {
			purpose = &trimmed
		}
	}
	var partnerIBAN *string
	if info != nil && info.partnerIBAN != nil {
		normalized := normalizeIBAN(*info.partnerIBAN)
		if isValidIBAN(normalized) {
			partnerIBAN = &normalized
		}
	}
	var partnerName *string
	if info != nil {
		partnerName = info.partnerName
	}

	return ImportedBooking{
		Date:          bookingDate,
		ValueDate:     datePointer(valueDate),
		Amount:        amount,
		Currency:      currency,
		PartnerName:   partnerName,
		PartnerIBAN:   partnerIBAN,
		Purpose:       purpose,
		BalanceAfter:  nil,
		BankReference: reference,
	}, ""
}

func startsWithAt(characters []rune, position int, prefix string) bool {
	for offset, expected := range []rune(prefix) {
		index := position + offset
		if index >= len(characters) || characters[index] != expected {
			return false
		}
	}
	return true
}

func parseMTDate(text string, today civil.Date) (civil.Date, bool) {
	characters := []rune(text)
	if len(characters) != 6 || !allASCIIDigits(text) {
		return civil.Date{}, false
	}
	yy, ok := parseU8(string(characters[:2]))
	if !ok {
		return civil.Date{}, false
	}
	year, err := ResolveTwoDigitYear(yy, today)
	if err != nil {
		return civil.Date{}, false
	}
	month, monthOK := parseU32(string(characters[2:4]))
	day, dayOK := parseU32(string(characters[4:6]))
	if !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(year, int(month), int(day))
}

func inferBookingDate(valueDate civil.Date, entryDate string) *civil.Date {
	characters := []rune(entryDate)
	if len(characters) != 4 {
		return nil
	}
	month, monthOK := parseU32(string(characters[:2]))
	day, dayOK := parseU32(string(characters[2:]))
	if !monthOK || !dayOK {
		return nil
	}
	year := valueDate.Year()
	valueMonth := int(valueDate.Month())
	if valueMonth == 12 && month == 1 {
		year++
	}
	if valueMonth == 1 && month == 12 {
		year--
	}
	date, ok := dateFromYMD(year, int(month), int(day))
	if !ok {
		return nil
	}
	return &date
}

type structuredInfo struct {
	partnerName *string
	partnerIBAN *string
	purpose     *string
}

func parse86(text string) structuredInfo {
	trimmed := strings.TrimSpace(text)
	compactForCheck := strings.ReplaceAll(trimmed, "\n", "")
	runesForCheck := []rune(compactForCheck)
	isStructured := strings.HasPrefix(trimmed, "?") ||
		(len(runesForCheck) >= 4 && allASCIIDigits(string(runesForCheck[:3])) && runesForCheck[3] == '?')
	if !isStructured {
		purpose := collapseWhitespace(trimmed)
		var purposePointer *string
		if purpose != "" {
			purposePointer = &purpose
		}
		return structuredInfo{purpose: purposePointer}
	}

	compact := strings.ReplaceAll(trimmed, "\n", "")
	compactRunes := []rune(compact)
	body := compact
	if len(compactRunes) >= 3 && allASCIIDigits(string(compactRunes[:3])) {
		body = string(compactRunes[3:])
	}

	characters := []rune(body)
	type field struct {
		key        string
		start      int
		valueStart int
	}
	fields := []field{}
	index := 0
	for index+2 < len(characters) {
		if characters[index] == '?' && isASCIIDigit(characters[index+1]) && isASCIIDigit(characters[index+2]) {
			key := string(characters[index+1 : index+3])
			fields = append(fields, field{key: key, start: index, valueStart: index + 3})
			index += 3
		} else {
			index++
		}
	}

	type value struct {
		key   string
		value string
	}
	values := make([]value, 0, len(fields))
	for fieldIndex, current := range fields {
		end := len(characters)
		if fieldIndex+1 < len(fields) {
			end = fields[fieldIndex+1].start
		}
		values = append(values, value{key: current.key, value: string(characters[current.valueStart:end])})
	}

	var purposeByNumbers strings.Builder
	for _, item := range values {
		number, ok := parseU32(item.key)
		if ok && ((number >= 20 && number <= 29) || (number >= 60 && number <= 63)) {
			purposeByNumbers.WriteString(strings.TrimSpace(item.value))
		}
	}
	var purpose *string
	if purposeByNumbers.Len() > 0 {
		text := purposeByNumbers.String()
		purpose = &text
	} else {
		for _, wanted := range []string{"00", "10"} {
			found := false
			for _, item := range values {
				if item.key == wanted {
					text := strings.TrimSpace(item.value)
					purpose = &text
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	if purpose != nil && *purpose == "" {
		purpose = nil
	}

	var partnerNameBuilder strings.Builder
	for _, item := range values {
		if item.key == "32" || item.key == "33" {
			partnerNameBuilder.WriteString(strings.TrimSpace(item.value))
		}
	}
	var partnerName *string
	if partnerNameBuilder.Len() > 0 {
		text := partnerNameBuilder.String()
		partnerName = &text
	}
	var partnerIBAN *string
	for _, item := range values {
		if item.key == "31" {
			text := strings.TrimSpace(item.value)
			if text != "" {
				partnerIBAN = &text
			}
			break
		}
	}

	return structuredInfo{partnerName: partnerName, partnerIBAN: partnerIBAN, purpose: purpose}
}

func parseMTAmount(text string) (decimal.Decimal, bool) {
	normalized := strings.ReplaceAll(text, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", ".")
	return parseDecimal(normalized)
}

func decodeISO88591(bytes []byte) string {
	runes := make([]rune, 0, len(bytes))
	for _, b := range bytes {
		runes = append(runes, rune(b))
	}
	return string(runes)
}
