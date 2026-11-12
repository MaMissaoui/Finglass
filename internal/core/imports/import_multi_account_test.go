package imports

import (
	"strings"
	"testing"
)

const multiAccountIBAN = "DE89370400440532013000"
const multiAccountOtherIBAN = "DE44500105175407324931"

func multiAccountCAMT(statements string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n" + statements + "\n</Document>"
}

func multiAccountSTMT(iban string, entries string) string {
	return "<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>" + iban + "</IBAN></Id></Acct>\n" + entries + "\n</Stmt></BkToCstmrStmt>"
}

func multiAccountEntry(amount string, date string, reference string) string {
	return "<Ntry>\n<Amt Ccy=\"EUR\">" + amount + "</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\n" +
		"<BookgDt><Dt>" + date + "</Dt></BookgDt><ValDt><Dt>" + date + "</Dt></ValDt>\n" +
		"<AcctSvcrRef>" + reference + "</AcctSvcrRef>\n" +
		"<NtryDtls><TxDtls>\n<RltdPties><Cdtr><Nm>Partner</Nm></Cdtr></RltdPties>\n" +
		"<RmtInf><Ustrd>Test</Ustrd></RmtInf>\n</TxDtls></NtryDtls>\n</Ntry>"
}

func multiAccountOFXSGML(acctID string) string {
	return "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\nOLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n<BANKMSGSRSV1>\n<STMTTRNRS>\n<STMTRS>\n<CURDEF>EUR</CURDEF>\n<BANKACCTFROM>\n<BANKID>37040044</BANKID>\n<ACCTID>" + acctID + "</ACCTID>\n<ACCTTYPE>CHECKING</ACCTTYPE>\n</BANKACCTFROM>\n<BANKTRANLIST>\n<DTSTART>20261003000000</DTSTART>\n<DTEND>20261003000000</DTEND>\n<STMTTRN>\n<TRNTYPE>DEBIT</TRNTYPE>\n<DTPOSTED>20261003000000</DTPOSTED>\n<TRNAMT>-10.00</TRNAMT>\n<FITID>REF-" + acctID + "</FITID>\n<NAME>Partner</NAME>\n<MEMO>Test</MEMO>\n</STMTTRN>\n</BANKTRANLIST>\n</STMTRS>\n</STMTTRNRS>\n</BANKMSGSRSV1>\n</OFX>"
}

func multiAccountOFXXMLStatement(acctID string) string {
	return "  <BANKMSGSRSV1>\n    <STMTTRNRS>\n      <STMTRS>\n        <CURDEF>EUR</CURDEF>\n" +
		"        <BANKACCTFROM>\n          <ACCTID>" + acctID + "</ACCTID>\n        </BANKACCTFROM>\n" +
		"        <BANKTRANLIST>\n          <STMTTRN>\n            <TRNTYPE>DEBIT</TRNTYPE>\n" +
		"            <DTPOSTED>20261003000000</DTPOSTED>\n            <TRNAMT>-10.00</TRNAMT>\n" +
		"            <FITID>REF-" + acctID + "</FITID>\n            <NAME>Partner</NAME>\n" +
		"            <MEMO>Test</MEMO>\n          </STMTTRN>\n        </BANKTRANLIST>\n" +
		"      </STMTRS>\n    </STMTTRNRS>\n  </BANKMSGSRSV1>\n"
}

func TestTwoStmtBlocksForDifferentIBANsListsBothAccountsAndIsRefused(t *testing.T) {
	xml := multiAccountCAMT(
		multiAccountSTMT(multiAccountIBAN, multiAccountEntry("10.00", "2026-10-01", "A1")) +
			multiAccountSTMT(multiAccountOtherIBAN, multiAccountEntry("20.00", "2026-10-02", "A2")),
	)
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{multiAccountIBAN, multiAccountOtherIBAN}
	if len(statement.Accounts) != len(want) {
		t.Fatalf("accounts = %v", statement.Accounts)
	}
	for index := range want {
		if statement.Accounts[index] != want[index] {
			t.Errorf("account[%d] = %s, want %s", index, statement.Accounts[index], want[index])
		}
	}
	if len(statement.Bookings) != 2 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
}

func TestTwoStmtBlocksForTheSameIBANListsOneAccountAndKeepsAllBookings(t *testing.T) {
	xml := multiAccountCAMT(
		multiAccountSTMT(multiAccountIBAN, multiAccountEntry("10.00", "2026-10-01", "A1")) +
			multiAccountSTMT(multiAccountIBAN, multiAccountEntry("20.00", "2026-10-02", "A2")),
	)
	statement, err := ParseCAMT([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Accounts) != 1 || statement.Accounts[0] != multiAccountIBAN {
		t.Errorf("accounts = %v", statement.Accounts)
	}
	if len(statement.Bookings) != 2 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
}

func TestZipWithTwoCAMTFilesForDifferentAccountsIsRefused(t *testing.T) {
	first := multiAccountCAMT(multiAccountSTMT(multiAccountIBAN, multiAccountEntry("10.00", "2026-10-01", "A1")))
	second := multiAccountCAMT(multiAccountSTMT(multiAccountOtherIBAN, multiAccountEntry("20.00", "2026-10-02", "A2")))
	zipBytes := writeZip([]zipEntry{{"a.xml", []byte(first), 0}, {"b.xml", []byte(second), 0}})
	_, err := Read("two-accounts.zip", zipBytes, nil, limitationToday(t))
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("err = %v", err)
	}
}

func TestMT940WithTwoStatementsForDifferentAccountsIsRefused(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C241001EUR0,00\n:61:241001D10,00N066NONREF\n:86:?20Test\n:62F:C241001EUR10,00\n-\n:20:STARTUMS\n:25:DE44500105175407324931\n:28C:1\n:60F:C241001EUR0,00\n:61:241001D20,00N066NONREF\n:86:?20Test\n:62F:C241001EUR20,00"
	_, err := Read("two-accounts.sta", []byte(text), nil, limitationToday(t))
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("err = %v", err)
	}
}

func TestOFXSGMLWithTwoStatementBlocksForDifferentAccountsIsRefused(t *testing.T) {
	text := multiAccountOFXSGML(multiAccountIBAN) + multiAccountOFXSGML(multiAccountOtherIBAN)
	_, err := Read("two-accounts.ofx", []byte(text), nil, limitationToday(t))
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("err = %v", err)
	}
}

func TestOFXXMLWithTwoStatementBlocksForDifferentAccountsIsRefused(t *testing.T) {
	text := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<OFX>\n" +
		multiAccountOFXXMLStatement(multiAccountIBAN) +
		multiAccountOFXXMLStatement(multiAccountOtherIBAN) + "</OFX>"
	_, err := Read("two-accounts-xml.ofx", []byte(text), nil, limitationToday(t))
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("err = %v", err)
	}
}
