package imports

import (
	"testing"
)

func TestSparkasseCSVCAMTExportInWindows1252WithPendingRows(t *testing.T) {
	csvText := cp1252(t, "\"Auftragskonto\";\"Buchungstag\";\"Valutadatum\";\"Buchungstext\";\"Verwendungszweck\";\"Glaeubiger ID\";\"Mandatsreferenz\";\"Kundenreferenz (End-to-End)\";\"Sammlerreferenz\";\"Lastschrift Ursprungsbetrag\";\"Auslagenersatz Ruecklastschrift\";\"Beguenstigter/Zahlungspflichtiger\";\"Kontonummer/IBAN\";\"BIC (SWIFT-Code)\";\"Betrag\";\"Waehrung\";\"Info\"\n\"DE89370400440532013000\";\"03.10.26\";\"03.10.26\";\"KARTENZAHLUNG\";\"REWE SAGT DANKE 1234\";\"\";\"\";\"\";\"\";\"\";\"\";\"REWE Markt GmbH\";\"DE44500105175407324931\";\"COLSDE33XXX\";\"-45,67\";\"EUR\";\"Umsatz gebucht\"\n\"DE89370400440532013000\";\"01.10.26\";\"01.10.26\";\"GUTSCHR. UEBERWEISUNG\";\"Gehalt Oktober\";\"\";\"\";\"\";\"\";\"\";\"\";\"Müller & Söhne GmbH\";\"\";\"\";\"3.200,00\";\"EUR\";\"Umsatz gebucht\"\n\"DE89370400440532013000\";\"04.10.26\";\"04.10.26\";\"KARTENZAHLUNG\";\"Tankstelle\";\"\";\"\";\"\";\"\";\"\";\"\";\"Aral\";\"\";\"\";\"-60,00\";\"EUR\";\"Umsatz vorgemerkt\"")
	statement, err := ParseCSV(csvText, nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Csv.CharsetName != "windows-1252" {
		t.Errorf("charset = %q", statement.Csv.CharsetName)
	}
	if statement.Csv.Delimiter != ';' {
		t.Errorf("delimiter = %q", statement.Csv.Delimiter)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.PendingSkipped != 1 {
		t.Errorf("pending = %d", statement.PendingSkipped)
	}
	rewe := statement.Bookings[0]
	if rewe.Date != testDate(t, 2026, 10, 3) {
		t.Errorf("date = %s", rewe.Date)
	}
	if got, want := rewe.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if rewe.PartnerName == nil || *rewe.PartnerName != "REWE Markt GmbH" {
		t.Errorf("partner = %v", rewe.PartnerName)
	}
	if rewe.PartnerIBAN == nil || *rewe.PartnerIBAN != "DE44500105175407324931" {
		t.Errorf("partner iban = %v", rewe.PartnerIBAN)
	}
	if rewe.Purpose == nil || *rewe.Purpose != "REWE SAGT DANKE 1234" {
		t.Errorf("purpose = %v", rewe.Purpose)
	}
	if statement.Bookings[1].PartnerName == nil || *statement.Bookings[1].PartnerName != "Müller & Söhne GmbH" {
		t.Errorf("partner 1 = %v", statement.Bookings[1].PartnerName)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "3200.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
}

func TestINGExportWithPreambleAccountIBANAndRunningBalance(t *testing.T) {
	text := "Umsatzanzeige;Datei erstellt am: 04.10.2026 10:00\n;\nIBAN;DE89 3704 0044 0532 0130 00\nKontoname;Girokonto\nBank;ING\nKunde;Max Mustermann\nZeitraum;01.09.2026 - 04.10.2026\nSaldo;5.234,56;EUR\n\nSortierung;Datum absteigend\n\nBuchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung\n02.10.2026;02.10.2026;AMAZON EU;Lastschrift;\"Bestellung 303-123; Monitor\";5.234,56;EUR;-89,99;EUR\n01.10.2026;01.10.2026;Employer GmbH;Gehalt/Rente;Gehalt;5.324,55;EUR;3.200,00;EUR"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.AccountIBAN == nil || *statement.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("account = %v", statement.AccountIBAN)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].PartnerName == nil || *statement.Bookings[0].PartnerName != "AMAZON EU" {
		t.Errorf("partner = %v", statement.Bookings[0].PartnerName)
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "Bestellung 303-123; Monitor" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
	if statement.Bookings[0].BalanceAfter == nil || statement.Bookings[0].BalanceAfter.Cmp(dec(t, "5234.56")) != 0 {
		t.Errorf("balance = %v", statement.Bookings[0].BalanceAfter)
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-89.99"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
}

func TestDKBExportUsesPayerForIncomingAndPayeeForOutgoing(t *testing.T) {
	text := "\"Konto\";\"Girokonto DE89370400440532013000\"\n\"\"\n\"Buchungsdatum\";\"Wertstellung\";\"Status\";\"Zahlungspflichtige*r\";\"Zahlungsempfänger*in\";\"Verwendungszweck\";\"Umsatztyp\";\"IBAN\";\"Betrag (€)\";\"Gläubiger-ID\";\"Mandatsreferenz\";\"Kundenreferenz\"\n\"02.10.26\";\"02.10.26\";\"Gebucht\";\"Max Mustermann\";\"Stadtwerke\";\"Abschlag Strom\";\"Ausgang\";\"DE44500105175407324931\";\"-80,00\";\"\";\"\";\"\"\n\"01.10.26\";\"01.10.26\";\"Gebucht\";\"Erika Musterfrau\";\"Max Mustermann\";\"Miete Anteil\";\"Eingang\";\"DE44500105175407324931\";\"400,00\";\"\";\"\";\"\"\n\"05.10.26\";\"05.10.26\";\"Vorgemerkt\";\"Max Mustermann\";\"Kino\";\"Tickets\";\"Ausgang\";\"\";\"-20,00\";\"\";\"\";\"\""
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, booking := range statement.Bookings {
		if booking.PartnerName != nil {
			names = append(names, *booking.PartnerName)
		}
	}
	if len(names) != 2 || names[0] != "Stadtwerke" || names[1] != "Erika Musterfrau" {
		t.Errorf("names = %q", names)
	}
	if statement.AccountIBAN == nil || *statement.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("account = %v", statement.AccountIBAN)
	}
	if statement.PendingSkipped != 1 {
		t.Errorf("pending = %d", statement.PendingSkipped)
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-80.00"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
}

func TestEnglishExportWithDebitAndCreditColumnsAndMultilinePurpose(t *testing.T) {
	text := "Date,Description,Payee,Debit,Credit,Balance\r\n2026-09-30,\"Card payment\nLondon\",Pret A Manger,4.50,,100.00\r\n2026-09-29,Salary,ACME Ltd,,\"1,500.00\",104.50\r\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-4.50"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "1500.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "Card payment London" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
	if statement.Bookings[0].PartnerName == nil || *statement.Bookings[0].PartnerName != "Pret A Manger" {
		t.Errorf("partner = %v", statement.Bookings[0].PartnerName)
	}
}

func TestUnknownLayoutsReportWhatIsMissingAndAcceptAManualMapping(t *testing.T) {
	text := "When;What;How much\n03.10.2026;Coffee;-3,20\n"
	detected, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(detected.Bookings) != 0 {
		t.Errorf("bookings = %d", len(detected.Bookings))
	}
	if detected.Csv.Mapping.IsComplete() {
		t.Error("the detected mapping is complete")
	}
	manual := CsvMapping{Date: intPointer(0), Purpose: intPointer(1), Amount: intPointer(2)}
	parsed, err := ParseCSV([]byte(text), &manual)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(parsed.Bookings))
	}
	if got, want := parsed.Bookings[0].Amount, dec(t, "-3.20"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
}

func TestBadRowsAreSkippedWithAReason(t *testing.T) {
	text := "Buchungstag;Betrag;Verwendungszweck\n03.10.2026;-1,00;ok\nkein Datum;-2,00;bad\n04.10.2026;abc;bad\n"
	statement, err := ParseCSV([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if len(statement.Skipped) != 2 {
		t.Errorf("skipped = %v", statement.Skipped)
	}
	if !hasPrefix(statement.Skipped[0], "Line 3") {
		t.Errorf("skipped[0] = %q", statement.Skipped[0])
	}
}

func statementImportCAMT(entries string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n  <BkToCstmrStmt><Stmt>\n    <Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct>\n    " + entries + "\n  </Stmt></BkToCstmrStmt>\n</Document>"
}

func statementImportDebitEntry() string {
	return "<Ntry>\n  <Amt Ccy=\"EUR\">45.67</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\n  <BookgDt><Dt>2026-10-03</Dt></BookgDt><ValDt><Dt>2026-10-03</Dt></ValDt>\n  <AcctSvcrRef>REF-1</AcctSvcrRef>\n  <NtryDtls><TxDtls>\n    <RltdPties><Cdtr><Nm>REWE Markt GmbH</Nm></Cdtr><CdtrAcct><Id><IBAN>DE44500105175407324931</IBAN></Id></CdtrAcct></RltdPties>\n    <RmtInf><Ustrd>REWE SAGT DANKE</Ustrd><Ustrd>1234</Ustrd></RmtInf>\n  </TxDtls></NtryDtls>\n</Ntry>"
}

func TestReadsBookedEntriesWithPartiesPurposeAndReference(t *testing.T) {
	incomingEntry := "<Ntry><Amt Ccy=\"EUR\">3200.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-10-01</Dt></BookgDt>\n  <NtryDtls><TxDtls><RltdPties><Dbtr><Nm>Employer GmbH</Nm></Dbtr><Cdtr><Nm>Me</Nm></Cdtr></RltdPties><RmtInf><Ustrd>Gehalt</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry>\n<Ntry><Amt Ccy=\"EUR\">60.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>PDNG</Sts><BookgDt><Dt>2026-10-04</Dt></BookgDt></Ntry>"
	xml := statementImportCAMT(statementImportDebitEntry() + incomingEntry)
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.AccountIBAN == nil || *statement.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("account = %v", statement.AccountIBAN)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	rewe := statement.Bookings[0]
	if got, want := rewe.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if rewe.PartnerName == nil || *rewe.PartnerName != "REWE Markt GmbH" {
		t.Errorf("partner = %v", rewe.PartnerName)
	}
	if rewe.PartnerIBAN == nil || *rewe.PartnerIBAN != "DE44500105175407324931" {
		t.Errorf("partner iban = %v", rewe.PartnerIBAN)
	}
	if rewe.Purpose == nil || *rewe.Purpose != "REWE SAGT DANKE 1234" {
		t.Errorf("purpose = %v", rewe.Purpose)
	}
	if rewe.BankReference == nil || *rewe.BankReference != "REF-1" {
		t.Errorf("reference = %v", rewe.BankReference)
	}
	if statement.Bookings[1].PartnerName == nil || *statement.Bookings[1].PartnerName != "Employer GmbH" {
		t.Errorf("partner 1 = %v", statement.Bookings[1].PartnerName)
	}
}

func TestBatchBookingsAreSplitIntoTheirTransactions(t *testing.T) {
	batch := "<Ntry><Amt Ccy=\"EUR\">30.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-10-02</Dt></BookgDt><AcctSvcrRef>B1</AcctSvcrRef>\n  <NtryDtls>\n    <TxDtls><Amt Ccy=\"EUR\">10.00</Amt><RltdPties><Cdtr><Nm>A</Nm></Cdtr></RltdPties></TxDtls>\n    <TxDtls><Amt Ccy=\"EUR\">20.00</Amt><RltdPties><Cdtr><Nm>B</Nm></Cdtr></RltdPties></TxDtls>\n  </NtryDtls></Ntry>"
	statement, err := ParseCAMT([]byte(statementImportCAMT(batch)))
	if err != nil {
		t.Fatal(err)
	}
	wantAmounts := []string{"-10.00", "-20.00"}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	for index, amount := range wantAmounts {
		if got, want := statement.Bookings[index].Amount, dec(t, amount); got.Cmp(want) != 0 {
			t.Errorf("amount[%d] = %s, want %s", index, got, want)
		}
	}
	for index, want := range []string{"B1/0", "B1/1"} {
		if statement.Bookings[index].BankReference == nil || *statement.Bookings[index].BankReference != want {
			t.Errorf("reference[%d] = %v", index, statement.Bookings[index].BankReference)
		}
	}
}

func TestZipOfStatementsAndXXEProtection(t *testing.T) {
	xml := statementImportCAMT(statementImportDebitEntry())
	zipBytes := writeZip([]zipEntry{
		{"2026-10.xml", []byte(xml), 0},
		{"readme.txt", []byte("ignore"), 0},
	})
	if !LooksLikeCAMT(zipBytes) {
		t.Error("the ZIP is not detected as CAMT")
	}
	statement, err := ParseCAMT(zipBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	xxe := "<?xml version=\"1.0\"?><!DOCTYPE d [<!ENTITY x SYSTEM \"file:///etc/passwd\">]><Document><BkToCstmrStmt>&x;</BkToCstmrStmt></Document>"
	if _, err := ParseCAMT([]byte(xxe)); err == nil {
		t.Error("the XXE CAMT was accepted")
	}
}
