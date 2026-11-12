package imports

import (
	"testing"
)

const camtClosingIBAN = "DE89370400440532013000"

func camtClosingBalance(code string, amount string, date string, ccy string, sign string) string {
	datePart := ""
	if date != "" {
		datePart = "<Dt><Dt>" + date + "</Dt></Dt>"
	}
	return "<Bal><Tp><CdOrPrtry><Cd>" + code + "</Cd></CdOrPrtry></Tp>" +
		"<Amt Ccy=\"" + ccy + "\">" + amount + "</Amt>" +
		"<CdtDbtInd>" + sign + "</CdtDbtInd>" +
		datePart + "</Bal>"
}

func camtClosingEntry(amount string, sign string, status string, date string, reference string) string {
	statusPart := ""
	if status != "" {
		statusPart = "<Sts>" + status + "</Sts>"
	}
	return "<Ntry>" +
		"<Amt Ccy=\"EUR\">" + amount + "</Amt><CdtDbtInd>" + sign + "</CdtDbtInd>" +
		statusPart +
		"<BookgDt><Dt>" + date + "</Dt></BookgDt><ValDt><Dt>" + date + "</Dt></ValDt>" +
		"<AcctSvcrRef>" + reference + "</AcctSvcrRef>" +
		"<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Café</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>" +
		"</Ntry>"
}

func camtClosingDebitedEntry(amount string, date string, reference string) string {
	return "<Ntry>" +
		"<Amt Ccy=\"EUR\">" + amount + "</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>" +
		"<BookgDt><Dt>" + date + "</Dt></BookgDt><ValDt><Dt>" + date + "</Dt></ValDt>" +
		"<AcctSvcrRef>" + reference + "</AcctSvcrRef>" +
		"<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Café</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>" +
		"</Ntry>"
}

func camtClosingXML(iban string, parts []string) string {
	joined := ""
	for index, part := range parts {
		if index > 0 {
			joined += "\n"
		}
		joined += part
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n" +
		"<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>" + iban + "</IBAN></Id></Acct>\n" + joined + "\n" +
		"</Stmt></BkToCstmrStmt>\n</Document>"
}

func camtClosingZip(xmls []string) []byte {
	entries := make([]zipEntry, 0, len(xmls))
	for index, xml := range xmls {
		entries = append(entries, zipEntry{"statement" + intToString(index) + ".xml", []byte(xml), 0})
	}
	return writeZip(entries)
}

func TestACAMTEntryWithAnOutOfRangeAmountIsSkippedInsteadOfImported(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{camtClosingDebitedEntry("1E999999999", "2026-02-05", "R1")})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 0 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if len(statement.Skipped) != 1 || statement.Skipped[0] != "Entry 1: unreadable amount" {
		t.Errorf("skipped = %v", statement.Skipped)
	}
	if statement.ClosingBalance != nil {
		t.Error("closing balance is set")
	}
}

func TestOpeningPlusBookedEntriesMatchesTheClosingBalance(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	closing := statement.ClosingBalance
	if closing == nil {
		t.Fatal("no closing balance")
	}
	if closing.Amount.Cmp(dec(t, "80.00")) != 0 {
		t.Errorf("amount = %s", closing.Amount)
	}
	if closing.PendingExcluded != 0 {
		t.Errorf("pending = %d", closing.PendingExcluded)
	}
	if closing.Currency == nil || *closing.Currency != "EUR" {
		t.Errorf("currency = %v", closing.Currency)
	}
	if closing.Date != testDate(t, 2026, 2, 28) {
		t.Errorf("date = %s", closing.Date)
	}
}

func TestAPendingEntryFoldedIntoTheClosingBalanceIsTakenBackOut(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "60.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
		camtClosingEntry("20.00", "DBIT", "PDNG", "", "R3"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	closing := statement.ClosingBalance
	if closing == nil {
		t.Fatal("no closing balance")
	}
	if closing.Amount.Cmp(dec(t, "80.00")) != 0 {
		t.Errorf("amount = %s", closing.Amount)
	}
	if closing.PendingExcluded != 1 {
		t.Errorf("pending = %d", closing.PendingExcluded)
	}
}

func TestAClosingBalanceMatchingNeitherSumIsRejected(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "55.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
		camtClosingEntry("20.00", "DBIT", "PDNG", "", "R3"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance != nil {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestDebitBalancesAreNegated(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "50.00", "2026-02-28", "EUR", "DBIT"),
		camtClosingBalance("CLBD", "75.00", "2026-02-28", "EUR", "DBIT"),
		camtClosingDebitedEntry("25.00", "2026-02-05", "R1"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	closing := statement.ClosingBalance
	if closing == nil {
		t.Fatal("no closing balance")
	}
	if closing.Amount.Cmp(dec(t, "-75.00")) != 0 {
		t.Errorf("amount = %s", closing.Amount)
	}
	if closing.PendingExcluded != 0 {
		t.Errorf("pending = %d", closing.PendingExcluded)
	}
}

func TestPRCDAndITBDBalanceTypesWorkLikeOPBDAndCLBD(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("PRCD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("ITBD", "80.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance == nil || statement.ClosingBalance.Amount.Cmp(dec(t, "80.00")) != 0 {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestOnlyAvailableBalancesAMissingOpeningBalanceOrADatelessClosingBalanceYieldNone(t *testing.T) {
	onlyAvailable := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPAV", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLAV", "80.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	if statement, err := ParseCAMT([]byte(onlyAvailable)); err != nil || statement.ClosingBalance != nil {
		t.Errorf("only available = %v, %v", statement.ClosingBalance, err)
	}
	noOpening := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("CLBD", "50.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	if statement, err := ParseCAMT([]byte(noOpening)); err != nil || statement.ClosingBalance != nil {
		t.Errorf("no opening = %v, %v", statement.ClosingBalance, err)
	}
	noClosingDate := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "50.00", "", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	if statement, err := ParseCAMT([]byte(noClosingDate)); err != nil || statement.ClosingBalance != nil {
		t.Errorf("no closing date = %v, %v", statement.ClosingBalance, err)
	}
}

func TestASkippedEntryDisablesTheClosingBalance(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "50.00", "2026-02-28", "EUR", "CRDT"),
		"<Ntry><Amt Ccy=\"EUR\">abc</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-02-05</Dt></BookgDt></Ntry>",
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Skipped) != 1 {
		t.Errorf("skipped = %v", statement.Skipped)
	}
	if statement.ClosingBalance != nil {
		t.Error("closing balance is set")
	}
}

func TestAClosingCurrencyDifferingFromTheEntryCurrenciesIsRejected(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "USD", "CRDT"),
		camtClosingBalance("CLBD", "50.00", "2026-02-28", "USD", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance != nil {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestTheZipPicksTheReportWithTheLatestClosingDate(t *testing.T) {
	january := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-01-31", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "70.00", "2026-01-31", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-01-15", "R1"),
	})
	february := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "70.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-10", "R2"),
	})
	statement, err := ParseCAMT(camtClosingZip([]string{january, february}))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance == nil || statement.ClosingBalance.Amount.Cmp(dec(t, "80.00")) != 0 {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestALatestReportThatDoesNotReconcileLeavesNoFallbackToAnOlderOne(t *testing.T) {
	january := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-01-31", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "70.00", "2026-01-31", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-01-15", "R1"),
	})
	february := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "70.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "99.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingEntry("10.00", "CRDT", "BOOK", "2026-02-10", "R2"),
	})
	statement, err := ParseCAMT(camtClosingZip([]string{january, february}))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance != nil {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestTwoReportsSharingTheLatestDateWithDifferentClosingBalancesYieldNone(t *testing.T) {
	first := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	second := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "110.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "95.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("15.00", "2026-02-05", "R2"),
	})
	statement, err := ParseCAMT(camtClosingZip([]string{first, second}))
	if err != nil {
		t.Fatal(err)
	}
	if statement.ClosingBalance != nil {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}

func TestABatchEntryReconcilesOnTheEntryAmountNotTheSplitDetails(t *testing.T) {
	batch := "<Ntry>" +
		"<Amt Ccy=\"EUR\">10.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>BOOK</Sts>" +
		"<BookgDt><Dt>2026-02-05</Dt></BookgDt><ValDt><Dt>2026-02-05</Dt></ValDt>" +
		"<AcctSvcrRef>R1</AcctSvcrRef>" +
		"<NtryDtls><TxDtls><Amt Ccy=\"EUR\">6.00</Amt><RltdPties><Cdtr><Nm>A</Nm></Cdtr></RltdPties></TxDtls>" +
		"<TxDtls><Amt Ccy=\"EUR\">4.00</Amt><RltdPties><Cdtr><Nm>B</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>" +
		"</Ntry>"
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "110.00", "2026-02-28", "EUR", "CRDT"),
		batch,
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	closing := statement.ClosingBalance
	if closing == nil {
		t.Fatal("no closing balance")
	}
	if closing.Amount.Cmp(dec(t, "110.00")) != 0 {
		t.Errorf("amount = %s", closing.Amount)
	}
	if closing.PendingExcluded != 0 {
		t.Errorf("pending = %d", closing.PendingExcluded)
	}
}

func TestAMalformedBalanceIsNoParseFailureAndKeepsTheBookings(t *testing.T) {
	xml := camtClosingXML(camtClosingIBAN, []string{
		camtClosingBalance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
		camtClosingBalance("CLBD", "abc", "2026-02-28", "EUR", "CRDT"),
		camtClosingDebitedEntry("30.00", "2026-02-05", "R1"),
	})
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if statement.ClosingBalance != nil {
		t.Errorf("closing = %v", statement.ClosingBalance)
	}
}
