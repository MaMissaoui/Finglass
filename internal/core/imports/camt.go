// Reading ISO 20022 CAMT.053 (and CAMT.052) account statements, as XML or a ZIP
// of XML files. Ports crates/finglass-core/src/import/camt.rs.
package imports

import (
	"archive/zip"
	stdbytes "bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/imports/xmltree"
)

const (
	maxEntryBytes = 50 * 1024 * 1024
	maxZipBytes   = 200 * 1024 * 1024
)

// LooksLikeCAMT reports whether the bytes look like a CAMT file or ZIP.
func LooksLikeCAMT(bytes []byte) bool {
	if isZip(bytes) {
		return true
	}
	head := decodeUTF8Lossy(bytes[:min(len(bytes), 2048)])
	return strings.Contains(head, "<Document") &&
		(strings.Contains(head, "camt.053") || strings.Contains(head, "camt.052") || strings.Contains(head, "BkToCstmrStmt"))
}

// ParseCAMT reads a CAMT statement.
func ParseCAMT(bytes []byte) (ParsedStatement, error) {
	var documents [][]byte
	if isZip(bytes) {
		unzipped, err := unzipXML(bytes)
		if err != nil {
			return ParsedStatement{}, err
		}
		documents = unzipped
	} else {
		documents = [][]byte{bytes}
	}
	bookings := []ImportedBooking{}
	skipped := []string{}
	accounts := []string{}
	messageTypes := []string{}
	reports := []camtReport{}
	var iban *string
	entryNumber := 0

	for _, documentBytes := range documents {
		text := decodeXML(documentBytes)
		if err := requireNoDoctype(text); err != nil {
			return ParsedStatement{}, err
		}
		document, err := xmltree.Parse(text)
		if err != nil {
			return ParsedStatement{}, Invalid("Malformed CAMT XML: " + err.Error())
		}
		root := document.RootElement()
		if messageType, ok := camtMessageType(root); ok && !containsString(messageTypes, messageType) {
			messageTypes = append(messageTypes, messageType)
		}
		statements := descendants(root, "Stmt")
		if len(statements) == 0 {
			statements = descendants(root, "Rpt")
		}
		for _, statement := range statements {
			accountID := child(child(statement, "Acct"), "Id")
			var accountIBAN *string
			if id := child(accountID, "IBAN"); id != nil {
				normalized := normalizeIBAN(textContent(id))
				accountIBAN = &normalized
			}
			var accountOther *string
			if other := child(accountID, "Othr"); other != nil {
				if id := child(other, "Id"); id != nil {
					trimmed := strings.TrimSpace(textContent(id))
					accountOther = &trimmed
				}
			}
			account := accountIBAN
			if account == nil && accountOther != nil && *accountOther != "" {
				account = accountOther
			}
			if account != nil && !containsString(accounts, *account) {
				accounts = append(accounts, *account)
			}
			if iban == nil {
				iban = accountIBAN
			}
			entryElements := children(statement, "Ntry")
			for _, entry := range entryElements {
				entryNumber++
				parsed, reason := entryToBookings(entry)
				if reason != "" {
					skipped = append(skipped, "Entry "+strconv.Itoa(entryNumber)+": "+reason)
				} else {
					bookings = append(bookings, parsed...)
				}
			}
			if report := reportBalance(statement, entryElements); report != nil {
				reports = append(reports, *report)
			}
		}
	}

	var closingBalance *ClosingBalance
	if len(skipped) == 0 {
		closingBalance = takeLatest(reports)
	}
	var formatDetail *string
	if len(messageTypes) > 0 {
		joined := strings.Join(messageTypes, "/")
		formatDetail = &joined
	}

	return ParsedStatement{
		Format:         FormatCAMT053,
		Bookings:       bookings,
		AccountIBAN:    iban,
		Accounts:       accounts,
		Skipped:        skipped,
		PendingSkipped: 0,
		Csv:            nil,
		FormatDetail:   formatDetail,
		ClosingBalance: closingBalance,
	}, nil
}

func isZip(bytes []byte) bool {
	return len(bytes) > 4 && bytes[0] == 'P' && bytes[1] == 'K'
}

func unzipXML(bytes []byte) ([][]byte, error) {
	archive, err := zip.NewReader(stdbytes.NewReader(bytes), int64(len(bytes)))
	if err != nil {
		return nil, Invalid("The ZIP file could not be read")
	}
	result := [][]byte{}
	total := uint64(0)
	for _, entry := range archive.File {
		name := zipEntryName(entry)
		if zipEntryIsDir(entry) || !strings.HasSuffix(lowerText.String(name), ".xml") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, Invalid("The ZIP file could not be read")
		}
		data, err := io.ReadAll(io.LimitReader(reader, int64(maxEntryBytes)+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			return nil, Invalid("The ZIP file could not be read")
		}
		if len(data) > maxEntryBytes {
			return nil, Invalid(fmt.Sprintf("ZIP entry %s is too large", name))
		}
		total += uint64(len(data))
		if total > maxZipBytes {
			return nil, Invalid("ZIP content is too large")
		}
		result = append(result, data)
	}
	if len(result) == 0 {
		return nil, Invalid("The ZIP file contains no XML statements")
	}
	return result, nil
}

// zipEntryName mirrors the zip crate's name(): UTF-8 when the raw bytes are
// valid UTF-8, otherwise IBM code page 437.
func zipEntryName(entry *zip.File) string {
	if utf8.ValidString(entry.Name) {
		return entry.Name
	}
	decoded, err := charmap.CodePage437.NewDecoder().String(entry.Name)
	if err != nil {
		return ""
	}
	return decoded
}

// zipEntryIsDir mirrors the zip crate's is_dir(): the raw name ends in '/' or
// '\\'.
func zipEntryIsDir(entry *zip.File) bool {
	if entry.Name == "" {
		return false
	}
	last := entry.Name[len(entry.Name)-1]
	return last == '/' || last == '\\'
}

func children(node *xmltree.Node, name string) []*xmltree.Node {
	if node == nil {
		return nil
	}
	result := []*xmltree.Node{}
	for _, childNode := range node.Children() {
		if childNode.IsElement() && childNode.TagName() == name {
			result = append(result, childNode)
		}
	}
	return result
}

func child(node *xmltree.Node, name string) *xmltree.Node {
	if node == nil {
		return nil
	}
	for _, childNode := range node.Children() {
		if childNode.IsElement() && childNode.TagName() == name {
			return childNode
		}
	}
	return nil
}

func descendants(node *xmltree.Node, name string) []*xmltree.Node {
	if node == nil {
		return nil
	}
	result := []*xmltree.Node{}
	for _, descendant := range node.Descendants() {
		if descendant.IsElement() && descendant.TagName() == name {
			result = append(result, descendant)
		}
	}
	return result
}

func textContent(node *xmltree.Node) string {
	if node == nil {
		return ""
	}
	var text strings.Builder
	for _, descendant := range node.Descendants() {
		if descendant.IsText() {
			if value, ok := descendant.Text(); ok {
				text.WriteString(value)
			}
		}
	}
	return text.String()
}

func camtMessageType(root *xmltree.Node) (string, bool) {
	namespace := root.TagNamespace()
	if index := strings.Index(namespace, "camt."); index >= 0 {
		rest := namespace[index+5:]
		var digits strings.Builder
		for _, character := range rest {
			if character < '0' || character > '9' {
				break
			}
			digits.WriteRune(character)
		}
		if digits.Len() == 3 {
			return "CAMT." + digits.String(), true
		}
	}
	for _, childNode := range root.Children() {
		if !childNode.IsElement() {
			continue
		}
		switch childNode.TagName() {
		case "BkToCstmrAcctRpt":
			return "CAMT.052", true
		case "BkToCstmrStmt":
			return "CAMT.053", true
		case "BkToCstmrDbtCdtNtfctn":
			return "CAMT.054", true
		}
		return "", false
	}
	return "", false
}

type parsedBalance struct {
	amount   decimal.Decimal
	currency *string
	element  *xmltree.Node
}

type camtReport struct {
	date    civil.Date
	balance *ClosingBalance
}

func reportBalance(statement *xmltree.Node, entries []*xmltree.Node) *camtReport {
	closing := balanceOf(statement, "CLBD")
	if closing == nil {
		closing = balanceOf(statement, "ITBD")
	}
	if closing == nil {
		return nil
	}
	date, ok := balanceDate(closing.element)
	if !ok {
		return nil
	}
	opening := balanceOf(statement, "OPBD")
	if opening == nil {
		opening = balanceOf(statement, "PRCD")
	}
	if opening == nil {
		return &camtReport{date: date}
	}
	booked := decimal.Zero
	pending := decimal.Zero
	var pendingCount uint32
	currencies := []*string{}
	for _, entry := range entries {
		amount, ok := entrySignedAmount(entry)
		if !ok {
			continue
		}
		var currency *string
		if amountNode := child(entry, "Amt"); amountNode != nil {
			if value, found := amountNode.Attribute("Ccy"); found && value != "" {
				currency = &value
			}
		}
		currencies = append(currencies, currency)
		if status, hasStatus := entryStatus(entry); !hasStatus || *status == "BOOK" {
			booked = booked.Add(amount)
		} else {
			pending = pending.Add(amount)
			pendingCount++
		}
	}
	if closing.currency != nil {
		for _, currency := range currencies {
			if !sameOptionalString(currency, closing.currency) {
				return &camtReport{date: date}
			}
		}
	}
	if opening.amount.Add(booked).Cmp(closing.amount) == 0 {
		return &camtReport{date: date, balance: &ClosingBalance{
			Amount:          closing.amount,
			Date:            date,
			Currency:        closing.currency,
			PendingExcluded: 0,
		}}
	}
	if pendingCount > 0 && opening.amount.Add(booked).Add(pending).Cmp(closing.amount) == 0 {
		return &camtReport{date: date, balance: &ClosingBalance{
			Amount:          closing.amount.Sub(pending),
			Date:            date,
			Currency:        closing.currency,
			PendingExcluded: pendingCount,
		}}
	}
	return &camtReport{date: date}
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func balanceOf(statement *xmltree.Node, code string) *parsedBalance {
	for _, balance := range children(statement, "Bal") {
		cd := child(child(child(balance, "Tp"), "CdOrPrtry"), "Cd")
		if cd != nil && strings.TrimSpace(textContent(cd)) == code {
			return parseBalance(balance)
		}
	}
	return nil
}

func parseBalance(balance *xmltree.Node) *parsedBalance {
	amountElement := child(balance, "Amt")
	if amountElement == nil {
		return nil
	}
	value, ok := parseDecimal(textContent(amountElement))
	if !ok {
		return nil
	}
	amount := value
	if indicator := child(balance, "CdtDbtInd"); indicator != nil && strings.TrimSpace(textContent(indicator)) == "DBIT" {
		amount = amount.Neg()
	}
	var currency *string
	if value, found := amountElement.Attribute("Ccy"); found && value != "" {
		currency = &value
	}
	return &parsedBalance{amount: amount, currency: currency, element: balance}
}

func balanceDate(balance *xmltree.Node) (civil.Date, bool) {
	container := child(balance, "Dt")
	if container == nil {
		return civil.Date{}, false
	}
	date := child(container, "Dt")
	if date == nil {
		date = child(container, "DtTm")
	}
	if date == nil {
		return civil.Date{}, false
	}
	return parseISODate(textContent(date))
}

func entryStatus(entry *xmltree.Node) (*string, bool) {
	status := child(entry, "Sts")
	if status == nil {
		return nil, false
	}
	text := textContent(status)
	if code := child(status, "Cd"); code != nil {
		text = textContent(code)
	}
	trimmed := strings.TrimSpace(text)
	return &trimmed, true
}

func entrySignedAmount(entry *xmltree.Node) (decimal.Decimal, bool) {
	amountElement := child(entry, "Amt")
	if amountElement == nil {
		return decimal.Decimal{}, false
	}
	value, ok := parseDecimal(textContent(amountElement))
	if !ok {
		return decimal.Decimal{}, false
	}
	if indicator := child(entry, "CdtDbtInd"); indicator != nil && strings.TrimSpace(textContent(indicator)) == "DBIT" {
		return value.Neg(), true
	}
	return value, true
}

func takeLatest(reports []camtReport) *ClosingBalance {
	if len(reports) == 0 {
		return nil
	}
	latest := reports[0].date
	for _, report := range reports[1:] {
		if report.date.After(latest) {
			latest = report.date
		}
	}
	atLatest := []*ClosingBalance{}
	for _, report := range reports {
		if report.date == latest {
			atLatest = append(atLatest, report.balance)
		}
	}
	for _, balance := range atLatest {
		if balance == nil {
			return nil
		}
	}
	if len(atLatest) == 0 {
		return nil
	}
	first := atLatest[0]
	for _, balance := range atLatest {
		if balance.Amount.Cmp(first.Amount) != 0 || !sameOptionalString(balance.Currency, first.Currency) {
			return nil
		}
	}
	result := *first
	return &result
}

func entryToBookings(entry *xmltree.Node) ([]ImportedBooking, string) {
	status, hasStatus := entryStatus(entry)
	if hasStatus && *status != "BOOK" {
		return []ImportedBooking{}, ""
	}

	date, ok := dateOf(child(entry, "BookgDt"))
	if !ok {
		date, ok = dateOf(child(entry, "ValDt"))
	}
	if !ok {
		return nil, "no booking date"
	}
	var valueDate *civil.Date
	if parsed, found := dateOf(child(entry, "ValDt")); found {
		valueDate = &parsed
	}
	amountElement := child(entry, "Amt")
	if amountElement == nil {
		return nil, "no amount"
	}
	var currency *string
	if value, found := amountElement.Attribute("Ccy"); found && value != "" {
		currency = &value
	}
	debit := false
	if indicator := child(entry, "CdtDbtInd"); indicator != nil && strings.TrimSpace(textContent(indicator)) == "DBIT" {
		debit = true
	}
	var entryReference *string
	if reference := child(entry, "AcctSvcrRef"); reference != nil {
		trimmed := strings.TrimSpace(textContent(reference))
		entryReference = &trimmed
	}

	details := descendants(entry, "TxDtls")
	if len(details) > 1 {
		allWithAmount := true
		for _, transaction := range details {
			if child(transaction, "Amt") == nil && child(transaction, "AmtDtls") == nil {
				allWithAmount = false
				break
			}
		}
		if allWithAmount {
			bookings := make([]ImportedBooking, 0, len(details))
			for index, transaction := range details {
				var amountNode *xmltree.Node
				if node := child(transaction, "Amt"); node != nil {
					amountNode = node
				} else if detailsNode := child(transaction, "AmtDtls"); detailsNode != nil {
					if matches := descendants(detailsNode, "Amt"); len(matches) > 0 {
						amountNode = matches[0]
					}
				}
				txAmount, ok := nodeAmount(amountNode)
				if !ok {
					return nil, "batch item without amount"
				}
				amount := txAmount
				if debit {
					amount = amount.Neg()
				}
				var reference *string
				if entryReference != nil {
					value := fmt.Sprintf("%s/%d", *entryReference, index)
					reference = &value
				}
				bookings = append(bookings, makeBooking(
					date, valueDate, amount, currency, transaction, debit, reference, nil,
				))
			}
			return bookings, ""
		}
	}

	value, ok := nodeAmount(amountElement)
	if !ok {
		return nil, "unreadable amount"
	}
	amount := value
	if debit {
		amount = amount.Neg()
	}
	var transaction *xmltree.Node
	if len(details) > 0 {
		transaction = details[0]
	}
	var fallbackText *string
	if info := child(entry, "AddtlNtryInf"); info != nil {
		text := textContent(info)
		fallbackText = &text
	}
	return []ImportedBooking{makeBooking(
		date, valueDate, amount, currency, transaction, debit, entryReference, fallbackText,
	)}, ""
}

func makeBooking(
	date civil.Date,
	valueDate *civil.Date,
	amount decimal.Decimal,
	currency *string,
	transaction *xmltree.Node,
	debit bool,
	reference *string,
	fallbackText *string,
) ImportedBooking {
	var parties *xmltree.Node
	if transaction != nil {
		parties = child(transaction, "RltdPties")
	}
	var party *xmltree.Node
	if parties != nil {
		if debit {
			party = child(parties, "Cdtr")
		} else {
			party = child(parties, "Dbtr")
		}
	}
	var partyName *string
	if party != nil {
		partyNode := child(party, "Pty")
		if partyNode == nil {
			partyNode = party
		}
		if name := child(partyNode, "Nm"); name != nil {
			trimmed := strings.TrimSpace(textContent(name))
			if trimmed != "" {
				partyName = &trimmed
			}
		}
	}
	var partyIBAN *string
	if parties != nil {
		var account *xmltree.Node
		if debit {
			account = child(parties, "CdtrAcct")
		} else {
			account = child(parties, "DbtrAcct")
		}
		if id := child(child(account, "Id"), "IBAN"); id != nil {
			normalized := normalizeIBAN(textContent(id))
			if isValidIBAN(normalized) {
				partyIBAN = &normalized
			}
		}
	}

	var purpose *string
	if transaction != nil {
		if info := child(transaction, "RmtInf"); info != nil {
			lines := []string{}
			for _, line := range children(info, "Ustrd") {
				lines = append(lines, strings.TrimSpace(textContent(line)))
			}
			joined := strings.Join(lines, " ")
			if joined != "" {
				purpose = &joined
			}
		}
		if purpose == nil {
			if info := child(transaction, "AddtlTxInf"); info != nil {
				trimmed := strings.TrimSpace(textContent(info))
				purpose = &trimmed
			}
		}
	}
	if purpose == nil && fallbackText != nil {
		trimmed := strings.TrimSpace(*fallbackText)
		purpose = &trimmed
	}
	if purpose != nil {
		collapsed := collapseWhitespace(*purpose)
		if collapsed == "" {
			purpose = nil
		} else {
			purpose = &collapsed
		}
	}

	if transaction != nil {
		if referenceNode := child(child(transaction, "Refs"), "AcctSvcrRef"); referenceNode != nil {
			trimmed := strings.TrimSpace(textContent(referenceNode))
			reference = &trimmed
		}
	}
	var bankReference *string
	if reference != nil && *reference != "" && *reference != "NOTPROVIDED" {
		bankReference = reference
	}

	return ImportedBooking{
		Date:          date,
		ValueDate:     valueDate,
		Amount:        amount,
		Currency:      currency,
		PartnerName:   partyName,
		PartnerIBAN:   partyIBAN,
		Purpose:       purpose,
		BalanceAfter:  nil,
		BankReference: bankReference,
	}
}

func dateOf(element *xmltree.Node) (civil.Date, bool) {
	if element == nil {
		return civil.Date{}, false
	}
	date := child(element, "Dt")
	if date == nil {
		date = child(element, "DtTm")
	}
	if date == nil {
		return civil.Date{}, false
	}
	return parseISODate(textContent(date))
}

func nodeAmount(element *xmltree.Node) (decimal.Decimal, bool) {
	if element == nil {
		return decimal.Decimal{}, false
	}
	value, ok := parseDecimal(textContent(element))
	if !ok {
		return decimal.Decimal{}, false
	}
	return absDecimal(value), true
}
