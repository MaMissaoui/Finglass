// Reading OFX bank statement files, both OFX 1.x SGML and OFX 2.x XML. Ports
// crates/finglass-core/src/import/ofx.rs.
package imports

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/imports/xmltree"
)

// LooksLikeOFX reports whether the bytes look like an OFX file.
func LooksLikeOFX(bytes []byte) bool {
	head := decodeISO88591(bytes[:min(len(bytes), 4096)])
	return (strings.Contains(head, "OFXHEADER:") && strings.Contains(head, "<OFX>")) ||
		(strings.Contains(head, "<OFX>") && strings.Contains(head, "<STMTTRN>"))
}

// ParseOFX reads an OFX file.
func ParseOFX(bytes []byte) (ParsedStatement, error) {
	if !LooksLikeOFX(bytes) {
		return ParsedStatement{}, Invalid("Not an OFX file")
	}
	text := decodeText(bytes)
	isXML := strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), "<?xml") || strings.Contains(text, "</")
	if isXML && !isSGML(text) {
		return parseXMLOFX(bytes)
	}
	return parseSGMLOFX(text)
}

func isSGML(text string) bool {
	return strings.Contains(text, "OFXHEADER:") || !strings.Contains(text, "</STMTTRN>")
}

func parseSGMLOFX(text string) (ParsedStatement, error) {
	ofxStart := strings.Index(text, "<OFX>")
	if ofxStart < 0 {
		return ParsedStatement{}, Invalid("OFX root not found")
	}
	body := text[ofxStart:]
	accountIBAN := extractSGMLAccountID(body)
	accounts := parseSGMLStatementAccounts(body)
	currency := extractSGMLCurrency(body)
	transactions := extractSGMLTransactions(body)
	bookings := []ImportedBooking{}
	skipped := []string{}

	for index, block := range transactions {
		booking, reason := parseSGMLTransaction(block, currency)
		if reason != "" {
			skipped = append(skipped, "Record "+strconv.Itoa(index+1)+": "+reason)
		} else {
			bookings = append(bookings, booking)
		}
	}

	return ParsedStatement{
		Format:         FormatOFX,
		Bookings:       bookings,
		AccountIBAN:    accountIBAN,
		Accounts:       accounts,
		Skipped:        skipped,
		PendingSkipped: 0,
	}, nil
}

func extractSGMLAccountID(text string) *string {
	for _, tag := range []string{"BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"} {
		block, ok := extractSGMLBlock(text, tag)
		if !ok {
			continue
		}
		id, ok := extractSGMLValue(block, "ACCTID")
		if !ok {
			continue
		}
		normalized := normalizeIBAN(id)
		if isValidIBAN(normalized) {
			return &normalized
		}
	}
	return nil
}

func extractSGMLCurrency(text string) *string {
	value, ok := extractSGMLValue(text, "CURDEF")
	if !ok {
		return nil
	}
	upper := upperText.String(strings.TrimSpace(value))
	if len([]rune(upper)) != 3 {
		return nil
	}
	return &upper
}

func extractSGMLTransactions(text string) []string {
	result := []string{}
	start := strings.Index(text, "<STMTTRN>")
	for start >= 0 {
		end := strings.Index(text[start:], "</STMTTRN>")
		if end < 0 {
			break
		}
		end = start + end + len("</STMTTRN>")
		result = append(result, text[start:end])
		next := strings.Index(text[end:], "<STMTTRN>")
		if next < 0 {
			break
		}
		start = end + next
	}
	return result
}

func parseSGMLTransaction(block string, currency *string) (ImportedBooking, string) {
	amountText, ok := extractSGMLValue(block, "TRNAMT")
	if !ok {
		return ImportedBooking{}, "missing amount"
	}
	amount, ok := parseOFXAmount(amountText)
	if !ok {
		return ImportedBooking{}, "invalid amount '" + amountText + "'"
	}
	dateText, ok := extractSGMLValue(block, "DTPOSTED")
	if !ok {
		return ImportedBooking{}, "missing date"
	}
	date, ok := parseOFXDate(dateText)
	if !ok {
		return ImportedBooking{}, "invalid date '" + dateText + "'"
	}
	var name *string
	if value, found := extractSGMLValue(block, "NAME"); found {
		name = decodeXMLEntities(&value)
	} else if payee, found := extractSGMLValue(block, "PAYEE"); found {
		if value, found := extractSGMLValue(payee, "NAME"); found {
			name = decodeXMLEntities(&value)
		}
	}
	var memo *string
	if value, found := extractSGMLValue(block, "MEMO"); found {
		memo = decodeXMLEntities(&value)
	}
	var fitID *string
	if value, found := extractSGMLValue(block, "FITID"); found {
		fitID = &value
	}

	var partnerName *string
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed != "" {
			partnerName = &trimmed
		}
	}
	var purpose *string
	if memo != nil {
		trimmed := strings.TrimSpace(*memo)
		if trimmed != "" {
			purpose = &trimmed
		}
	}
	var bankReference *string
	if fitID != nil {
		trimmed := strings.TrimSpace(*fitID)
		if trimmed != "" {
			bankReference = &trimmed
		}
	}

	return ImportedBooking{
		Date:          date,
		ValueDate:     nil,
		Amount:        amount,
		Currency:      currency,
		PartnerName:   partnerName,
		Purpose:       purpose,
		BankReference: bankReference,
	}, ""
}

func extractSGMLBlock(text string, tag string) (string, bool) {
	open := "<" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	closeTag := "</" + tag + ">"
	end := strings.Index(text[start:], closeTag)
	if end < 0 {
		return "", false
	}
	end = start + end + len(closeTag)
	return text[start:end], true
}

func extractSGMLValue(text string, tag string) (string, bool) {
	open := "<" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	valueStart := start + len(tag) + 2
	end := strings.IndexByte(text[valueStart:], '<')
	if end < 0 {
		end = len(text)
	} else {
		end = valueStart + end
	}
	return text[valueStart:end], true
}

func parseXMLOFX(bytes []byte) (ParsedStatement, error) {
	text := decodeXML(bytes)
	if err := requireNoDoctype(text); err != nil {
		return ParsedStatement{}, err
	}
	document, err := xmltree.Parse(text)
	if err != nil {
		return ParsedStatement{}, Invalid("Malformed OFX XML: " + err.Error())
	}
	root := document.RootElement()
	accountIBAN := findXMLAccountID(root)
	accounts := parseXMLStatementAccounts(root)
	currency := findXMLCurrency(root)
	transactions := descendants(root, "STMTTRN")
	bookings := []ImportedBooking{}
	skipped := []string{}

	for index, transaction := range transactions {
		booking, reason := parseXMLTransaction(transaction, currency)
		if reason != "" {
			skipped = append(skipped, "Record "+strconv.Itoa(index+1)+": "+reason)
		} else {
			bookings = append(bookings, booking)
		}
	}

	return ParsedStatement{
		Format:         FormatOFX,
		Bookings:       bookings,
		AccountIBAN:    accountIBAN,
		Accounts:       accounts,
		Skipped:        skipped,
		PendingSkipped: 0,
	}, nil
}

func parseXMLTransaction(element *xmltree.Node, currency *string) (ImportedBooking, string) {
	amountNode := child(element, "TRNAMT")
	if amountNode == nil {
		return ImportedBooking{}, "missing amount"
	}
	amountText := strings.TrimSpace(textContent(amountNode))
	amount, ok := parseOFXAmount(amountText)
	if !ok {
		return ImportedBooking{}, "invalid amount '" + amountText + "'"
	}
	dateNode := child(element, "DTPOSTED")
	if dateNode == nil {
		return ImportedBooking{}, "missing date"
	}
	dateText := strings.TrimSpace(textContent(dateNode))
	date, ok := parseOFXDate(dateText)
	if !ok {
		return ImportedBooking{}, "invalid date '" + dateText + "'"
	}
	var name *string
	if node := child(element, "NAME"); node != nil {
		trimmed := strings.TrimSpace(textContent(node))
		name = &trimmed
	} else if payee := child(element, "PAYEE"); payee != nil {
		if node := child(payee, "NAME"); node != nil {
			trimmed := strings.TrimSpace(textContent(node))
			name = &trimmed
		}
	}
	var memo *string
	if node := child(element, "MEMO"); node != nil {
		trimmed := strings.TrimSpace(textContent(node))
		memo = &trimmed
	}
	var fitID *string
	if node := child(element, "FITID"); node != nil {
		trimmed := strings.TrimSpace(textContent(node))
		fitID = &trimmed
	}
	if name != nil && *name == "" {
		name = nil
	}
	if memo != nil && *memo == "" {
		memo = nil
	}
	if fitID != nil && *fitID == "" {
		fitID = nil
	}
	return ImportedBooking{
		Date:          date,
		ValueDate:     nil,
		Amount:        amount,
		Currency:      currency,
		PartnerName:   name,
		Purpose:       memo,
		BankReference: fitID,
	}, ""
}

func findXMLAccountID(root *xmltree.Node) *string {
	for _, tag := range []string{"BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"} {
		blocks := descendants(root, tag)
		if len(blocks) == 0 {
			continue
		}
		idNode := child(blocks[0], "ACCTID")
		if idNode == nil {
			continue
		}
		id := strings.TrimSpace(textContent(idNode))
		normalized := normalizeIBAN(id)
		if isValidIBAN(normalized) {
			return &normalized
		}
	}
	return nil
}

func findXMLCurrency(root *xmltree.Node) *string {
	accept := func(value string) *string {
		upper := upperText.String(strings.TrimSpace(value))
		if len([]rune(upper)) != 3 {
			return nil
		}
		return &upper
	}
	if node := child(root, "CURDEF"); node != nil {
		if value := accept(textContent(node)); value != nil {
			return value
		}
	}
	if nodes := descendants(root, "CURDEF"); len(nodes) > 0 {
		return accept(textContent(nodes[0]))
	}
	return nil
}

func parseSGMLStatementAccounts(text string) []string {
	result := []string{}
	for _, tag := range []string{"STMTRS", "CCSTMTRS"} {
		open := "<" + tag + ">"
		closeTag := "</" + tag + ">"
		start := strings.Index(text, open)
		for start >= 0 {
			relativeEnd := strings.Index(text[start:], closeTag)
			if relativeEnd < 0 {
				break
			}
			end := start + relativeEnd + len(closeTag)
			if id, ok := extractSGMLValue(text[start:end], "ACCTID"); ok {
				id = strings.TrimSpace(id)
				if id != "" {
					normalized := normalizeIBAN(id)
					account := id
					if isValidIBAN(normalized) {
						account = normalized
					}
					if !containsString(result, account) {
						result = append(result, account)
					}
				}
			}
			next := strings.Index(text[end:], open)
			if next < 0 {
				break
			}
			start = end + next
		}
	}
	return result
}

func parseXMLStatementAccounts(root *xmltree.Node) []string {
	result := []string{}
	for _, tag := range []string{"STMTRS", "CCSTMTRS"} {
		for _, block := range descendants(root, tag) {
			var id string
			found := false
			for _, accountTag := range []string{"BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"} {
				account := child(block, accountTag)
				if account == nil {
					continue
				}
				idNode := child(account, "ACCTID")
				if idNode == nil {
					continue
				}
				id = strings.TrimSpace(textContent(idNode))
				found = true
				break
			}
			if !found {
				continue
			}
			normalized := normalizeIBAN(id)
			account := id
			if isValidIBAN(normalized) {
				account = normalized
			}
			if !containsString(result, account) {
				result = append(result, account)
			}
		}
	}
	return result
}

func parseOFXDate(text string) (civil.Date, bool) {
	var digits strings.Builder
	for _, character := range text {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}
	value := digits.String()
	if len(value) < 8 {
		return civil.Date{}, false
	}
	prefix := value[:8]
	year, yearOK := parseI64(prefix[0:4])
	month, monthOK := parseU32(prefix[4:6])
	day, dayOK := parseU32(prefix[6:8])
	if !yearOK || !monthOK || !dayOK {
		return civil.Date{}, false
	}
	return dateFromYMD(int(year), int(month), int(day))
}

func parseOFXAmount(text string) (decimal.Decimal, bool) {
	trimmed := strings.TrimSpace(text)
	hasDot := strings.Contains(trimmed, ".")
	hasComma := strings.Contains(trimmed, ",")
	var normalized string
	switch {
	case hasDot && hasComma:
		decimalSeparator := byte('.')
		if strings.LastIndex(trimmed, ",") > strings.LastIndex(trimmed, ".") {
			decimalSeparator = ','
		}
		grouping := byte(',')
		if decimalSeparator == ',' {
			grouping = '.'
		}
		normalized = strings.ReplaceAll(trimmed, string(grouping), "")
		normalized = strings.ReplaceAll(normalized, string(decimalSeparator), ".")
	case !hasDot && hasComma:
		normalized = strings.ReplaceAll(trimmed, ",", ".")
	default:
		normalized = strings.ReplaceAll(trimmed, ",", "")
	}
	return parseDecimal(normalized)
}

func decodeXMLEntities(text *string) *string {
	if text == nil {
		return nil
	}
	value := *text
	value = strings.ReplaceAll(value, "&lt;", "<")
	value = strings.ReplaceAll(value, "&gt;", ">")
	value = strings.ReplaceAll(value, "&quot;", "\"")
	value = strings.ReplaceAll(value, "&apos;", "'")
	value = strings.ReplaceAll(value, "&amp;", "&")
	return &value
}
