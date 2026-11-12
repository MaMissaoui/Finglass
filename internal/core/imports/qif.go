// Reading Quicken Interchange Format (.qif) files. Ports
// crates/finglass-core/src/import/qif.rs.
package imports

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
)

var qifBankTypes = []string{"bank", "cash", "ccard", "oth a", "oth l"}
var qifSkipTypes = []string{"cat", "class", "memorized", "invst"}

// LooksLikeQIF reports whether the bytes look like a QIF file.
func LooksLikeQIF(bytes []byte) bool {
	text := decodeText(bytes)
	characters := []rune(text)
	if len(characters) > 1024 {
		characters = characters[:1024]
	}
	normalised := lowerText.String(string(characters))
	return strings.Contains(normalised, "!type:") &&
		(strings.Contains(normalised, "!type:bank") ||
			strings.Contains(normalised, "!type:cash") ||
			strings.Contains(normalised, "!type:ccard") ||
			strings.Contains(normalised, "!type:oth a") ||
			strings.Contains(normalised, "!type:oth l"))
}

// ParseQIF reads a QIF file, using today's date for two-digit years.
func ParseQIF(bytes []byte) (ParsedStatement, error) {
	return ParseQIFWithToday(bytes, civil.FromTime(time.Now().UTC()))
}

// ParseQIFWithToday reads a QIF file with an explicit "today" for two-digit
// years.
func ParseQIFWithToday(bytes []byte, today civil.Date) (ParsedStatement, error) {
	if !LooksLikeQIF(bytes) {
		return ParsedStatement{}, Invalid("Not a QIF file")
	}
	text := decodeText(bytes)
	lines := textLines(text)
	slashOrder := detectSlashOrder(lines)

	bookings := []ImportedBooking{}
	skipped := []string{}
	current := map[rune]string{}
	var activeType *string
	recordNumber := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		recordNumber++
		if activeType != nil && containsString(qifBankTypes, *activeType) {
			booking, reason := qifToBooking(current, slashOrder, today)
			if reason != "" {
				skipped = append(skipped, "Record "+strconv.Itoa(recordNumber)+": "+reason)
			} else {
				bookings = append(bookings, booking)
			}
		}
		current = map[rune]string{}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		characters := []rune(trimmed)
		typePrefix := characters
		if len(typePrefix) > 6 {
			typePrefix = typePrefix[:6]
		}
		if asciiFoldEqual(string(typePrefix), "!Type:") {
			flush()
			lowered := lowerText.String(strings.TrimSpace(string(characters[6:])))
			activeType = &lowered
			continue
		}
		if startsWithIgnoreCase(trimmed, "!Account") ||
			startsWithIgnoreCase(trimmed, "!Option:") ||
			strings.HasPrefix(trimmed, "!") {
			flush()
			activeType = nil
			continue
		}
		if activeType != nil && containsString(qifSkipTypes, *activeType) {
			continue
		}
		if activeType == nil {
			continue
		}
		if trimmed == "^" {
			flush()
			continue
		}
		if trimmed == "" {
			continue
		}
		code := characters[0]
		value := strings.TrimSpace(string(characters[1:]))
		entry := current[code]
		if entry != "" {
			entry += "\n"
		}
		current[code] = entry + value
	}
	flush()

	return ParsedStatement{
		Format:         FormatQIF,
		Bookings:       bookings,
		AccountIBAN:    nil,
		Accounts:       []string{},
		Skipped:        skipped,
		PendingSkipped: 0,
	}, nil
}

func qifToBooking(fields map[rune]string, slashOrder qifSlashOrder, today civil.Date) (ImportedBooking, string) {
	dateText, ok := fields['D']
	if !ok {
		return ImportedBooking{}, "missing date"
	}
	date, ok := parseQIFDate(dateText, slashOrder, today)
	if !ok {
		return ImportedBooking{}, "invalid date '" + dateText + "'"
	}
	amountText, ok := fields['T']
	if !ok {
		amountText, ok = fields['U']
	}
	if !ok {
		return ImportedBooking{}, "missing amount"
	}
	amount, ok := parseAmount(amountText)
	if !ok {
		return ImportedBooking{}, "invalid amount '" + amountText + "'"
	}
	var partnerName *string
	if value, ok := fields['P']; ok {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			partnerName = &trimmed
		}
	}
	var purpose *string
	if value, ok := fields['M']; ok {
		collapsed := collapseWhitespace(value)
		if collapsed != "" {
			purpose = &collapsed
		}
	}
	return ImportedBooking{
		Date:          date,
		ValueDate:     nil,
		Amount:        amount,
		Currency:      nil,
		PartnerName:   partnerName,
		Purpose:       purpose,
		BankReference: nil,
	}, ""
}

type qifSlashOrder int

const (
	qifSlashUnknown qifSlashOrder = iota
	qifSlashDayMonth
)

func detectSlashOrder(lines []string) qifSlashOrder {
	for _, line := range lines {
		text := strings.TrimLeftFunc(line, unicode.IsSpace)
		characters := []rune(text)
		if len(characters) == 0 || (characters[0] != 'D' && characters[0] != 'd') {
			continue
		}
		value := strings.ReplaceAll(strings.TrimSpace(string(characters[1:])), " ", "")
		if !strings.Contains(value, "/") && !strings.Contains(value, "'") {
			continue
		}
		index := strings.IndexAny(value, "/'")
		if index <= 0 {
			continue
		}
		first, ok := parseU32(value[:index])
		if !ok {
			continue
		}
		if first > 12 {
			return qifSlashDayMonth
		}
	}
	return qifSlashUnknown
}

func parseQIFDate(text string, slashOrder qifSlashOrder, today civil.Date) (civil.Date, bool) {
	t := strings.ReplaceAll(text, " ", "")
	if strings.Contains(t, ".") {
		parts := strings.Split(t, ".")
		if len(parts) != 3 {
			return civil.Date{}, false
		}
		day, dayOK := parseU32(parts[0])
		month, monthOK := parseU32(parts[1])
		year, yearOK := parseQIFYear(parts[2], today)
		if !dayOK || !monthOK || !yearOK {
			return civil.Date{}, false
		}
		return dateFromYMD(year, int(month), int(day))
	}
	normalised := strings.NewReplacer("'", "/", "-", "/").Replace(t)
	if !strings.Contains(normalised, "/") {
		return civil.Date{}, false
	}
	parts := strings.Split(normalised, "/")
	if len(parts) != 3 {
		return civil.Date{}, false
	}
	first, firstOK := parseU32(parts[0])
	second, secondOK := parseU32(parts[1])
	year, yearOK := parseQIFYear(parts[2], today)
	if !firstOK || !secondOK || !yearOK {
		return civil.Date{}, false
	}
	month, day := first, second
	if slashOrder == qifSlashDayMonth {
		month, day = second, first
	}
	return dateFromYMD(year, int(month), int(day))
}

func parseQIFYear(text string, today civil.Date) (int, bool) {
	var digits strings.Builder
	for _, character := range text {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}
	switch digits.Len() {
	case 2:
		year, ok := parseU8(digits.String())
		if !ok {
			return 0, false
		}
		resolved, err := ResolveTwoDigitYear(year, today)
		if err != nil {
			return 0, false
		}
		return resolved, true
	case 4:
		year, ok := parseI64(digits.String())
		return int(year), ok
	default:
		return 0, false
	}
}

func startsWithIgnoreCase(text string, prefix string) bool {
	characters := []rune(text)
	expected := []rune(prefix)
	if len(expected) > len(characters) {
		return false
	}
	for index, character := range expected {
		if !asciiFoldEqualRune(characters[index], character) {
			return false
		}
	}
	return true
}

func asciiFoldEqual(left, right string) bool {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) != len(rightRunes) {
		return false
	}
	for index := range leftRunes {
		if !asciiFoldEqualRune(leftRunes[index], rightRunes[index]) {
			return false
		}
	}
	return true
}

func asciiFoldEqualRune(left, right rune) bool {
	if left >= 'A' && left <= 'Z' {
		left += 'a' - 'A'
	}
	if right >= 'A' && right <= 'Z' {
		right += 'a' - 'A'
	}
	return left == right
}
