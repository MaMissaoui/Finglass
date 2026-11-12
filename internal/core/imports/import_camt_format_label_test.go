package imports

import (
	"strings"
	"testing"
)

func camtLabelDocument(namespace string, message string) string {
	open := "<Document>"
	if namespace != "" {
		open = "<Document xmlns=\"" + namespace + "\">"
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + open + "\n" + message + "\n</Document>"
}

func camtLabelRPT(iban string, entries string) string {
	return "<BkToCstmrAcctRpt><Rpt>\n<Acct><Id><IBAN>" + iban + "</IBAN></Id></Acct>\n" + entries + "\n</Rpt></BkToCstmrAcctRpt>"
}

func camtLabelSTMT(iban string, entries string) string {
	return "<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>" + iban + "</IBAN></Id></Acct>\n" + entries + "\n</Stmt></BkToCstmrStmt>"
}

func camtLabelEntry(amount string, date string, reference string) string {
	return "<Ntry>\n<Amt Ccy=\"EUR\">" + amount + "</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\n" +
		"<BookgDt><Dt>" + date + "</Dt></BookgDt><ValDt><Dt>" + date + "</Dt></ValDt>\n" +
		"<AcctSvcrRef>" + reference + "</AcctSvcrRef>\n" +
		"<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Partner</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>\n</Ntry>"
}

func TestACAMT052AccountReportIsLabelledCAMT052WhileTheFormatStaysCAMT053(t *testing.T) {
	xml := camtLabelDocument(
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.08",
		camtLabelRPT(camtClosingIBAN, camtLabelEntry("10.00", "2026-10-01", "A1")),
	)
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.Format != FormatCAMT053 {
		t.Errorf("format = %s", statement.Format)
	}
	if statement.FormatDetail == nil || *statement.FormatDetail != "CAMT.052" {
		t.Errorf("detail = %v", statement.FormatDetail)
	}
	if statement.FormatLabel() != "CAMT.052 XML" {
		t.Errorf("label = %q", statement.FormatLabel())
	}
	if len(statement.Bookings) != 1 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
}

func TestACAMT053StatementIsLabelledCAMT053(t *testing.T) {
	xml := camtLabelDocument(
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
		camtLabelSTMT(camtClosingIBAN, camtLabelEntry("5.00", "2026-10-03", "A2")),
	)
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if statement.FormatDetail == nil || *statement.FormatDetail != "CAMT.053" {
		t.Errorf("detail = %v", statement.FormatDetail)
	}
	if statement.FormatLabel() != "CAMT.053 XML" {
		t.Errorf("label = %q", statement.FormatLabel())
	}
}

func TestWithoutANamespaceTheMessageTypeComesFromTheRootsFirstChild(t *testing.T) {
	noNamespace := camtLabelDocument("", camtLabelRPT(camtClosingIBAN, camtLabelEntry("10.00", "2026-10-01", "A3")))
	statement, err := ParseCAMT([]byte(noNamespace))
	if err != nil {
		t.Fatal(err)
	}
	if statement.FormatDetail == nil || *statement.FormatDetail != "CAMT.052" {
		t.Errorf("detail = %v", statement.FormatDetail)
	}
	stmt54 := camtLabelDocument("", camtLabelSTMT(camtClosingIBAN, camtLabelEntry("10.00", "2026-10-01", "A4")))
	stmt54 = strings.ReplaceAll(stmt54, "BkToCstmrStmt", "BkToCstmrDbtCdtNtfctn")
	statement, err = ParseCAMT([]byte(stmt54))
	if err != nil {
		t.Fatal(err)
	}
	if statement.FormatDetail == nil || *statement.FormatDetail != "CAMT.054" {
		t.Errorf("detail 54 = %v", statement.FormatDetail)
	}
}

func TestAZipOfACAMT052AndACAMT053FileNamesBothTypesInFileOrder(t *testing.T) {
	report := camtLabelDocument(
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.08",
		camtLabelRPT(camtClosingIBAN, camtLabelEntry("10.00", "2026-10-01", "B1")),
	)
	statementXML := camtLabelDocument(
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
		camtLabelSTMT(camtClosingIBAN, camtLabelEntry("20.00", "2026-10-02", "B2")),
	)
	zipBytes := writeZip([]zipEntry{
		{"report.xml", []byte(report), 0},
		{"statement.xml", []byte(statementXML), 0},
	})
	statement, err := ParseCAMT(zipBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Accounts) != 1 || statement.Accounts[0] != camtClosingIBAN {
		t.Errorf("accounts = %v", statement.Accounts)
	}
	if len(statement.Bookings) != 2 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if statement.FormatDetail == nil || *statement.FormatDetail != "CAMT.052/CAMT.053" {
		t.Errorf("detail = %v", statement.FormatDetail)
	}
	if statement.FormatLabel() != "CAMT.052/CAMT.053 XML" {
		t.Errorf("label = %q", statement.FormatLabel())
	}
}

func TestCSVStatementsKeepThePlainCSVLabel(t *testing.T) {
	statement, err := ParseCSV([]byte("Buchungstag;Betrag;Verwendungszweck\n03.10.2026;-1,00;Kaffee\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Format != FormatCSV {
		t.Errorf("format = %s", statement.Format)
	}
	if statement.FormatDetail != nil {
		t.Errorf("detail = %v", statement.FormatDetail)
	}
	if statement.FormatLabel() != "CSV" {
		t.Errorf("label = %q", statement.FormatLabel())
	}
}
