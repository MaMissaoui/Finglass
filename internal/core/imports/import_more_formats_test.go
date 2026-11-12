package imports

import (
	"testing"
)

func TestMT940WithTwoStatementsStructured86WrappedLineReversalAndYearRollover(t *testing.T) {
	text := cp1252(t, `:20:STARTUMS
:25:DE89370400440532013000
:28C:00001/001
:60F:C231030EUR1234,56
:61:2310301031D45,67N066NONREF
:86:166?00KARTENZAHLUNG?100005?20REWE SAGT DANKE?211234?32REWE Mar
kt GmbH
:61:2310311102C500,00N066NONREF
:86:166?00GUTSCHRIFT?100005?20Gehalt Oktober?32Müller & Söhne GmbH
:62F:C231031EUR1688,89
-
:20:STARTUMS
:25:DE89370400440532013000
:28C:00001/002
:60F:C231031EUR1688,89
:61:2312310102RD100,00N066RUECKLASTSCHRIFT
:86:166?00RÜCKBUCHUNG?20Lastschrift retour?32Kunde AG
:62F:C240102EUR1588,89`)
	statement, err := ParseMT940WithToday(text, limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if statement.Format != FormatMT940 {
		t.Errorf("format = %s", statement.Format)
	}
	if statement.AccountIBAN == nil || *statement.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("account iban = %v", statement.AccountIBAN)
	}
	if len(statement.Bookings) != 3 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if len(statement.Skipped) != 0 {
		t.Errorf("skipped = %v", statement.Skipped)
	}
	rewe := statement.Bookings[0]
	if rewe.Date != testDate(t, 2023, 10, 31) {
		t.Errorf("rewe date = %s", rewe.Date)
	}
	if rewe.ValueDate == nil || *rewe.ValueDate != testDate(t, 2023, 10, 30) {
		t.Errorf("rewe value date = %v", rewe.ValueDate)
	}
	if got, want := rewe.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("rewe amount = %s", got)
	}
	if rewe.PartnerName == nil || *rewe.PartnerName != "REWE Markt GmbH" {
		t.Errorf("rewe partner = %v", rewe.PartnerName)
	}
	if rewe.Purpose == nil || *rewe.Purpose != "REWE SAGT DANKE1234" {
		t.Errorf("rewe purpose = %v", rewe.Purpose)
	}
	if rewe.Currency == nil || *rewe.Currency != "EUR" {
		t.Errorf("rewe currency = %v", rewe.Currency)
	}
	if rewe.BalanceAfter != nil {
		t.Error("rewe balance after is set")
	}
	gehalt := statement.Bookings[1]
	if gehalt.Date != testDate(t, 2023, 11, 2) {
		t.Errorf("gehalt date = %s", gehalt.Date)
	}
	if gehalt.ValueDate == nil || *gehalt.ValueDate != testDate(t, 2023, 10, 31) {
		t.Errorf("gehalt value date = %v", gehalt.ValueDate)
	}
	if got, want := gehalt.Amount, dec(t, "500.00"); got.Cmp(want) != 0 {
		t.Errorf("gehalt amount = %s", got)
	}
	if gehalt.PartnerName == nil || *gehalt.PartnerName != "Müller & Söhne GmbH" {
		t.Errorf("gehalt partner = %v", gehalt.PartnerName)
	}
	if gehalt.Purpose == nil || *gehalt.Purpose != "Gehalt Oktober" {
		t.Errorf("gehalt purpose = %v", gehalt.Purpose)
	}
	reversal := statement.Bookings[2]
	if reversal.Date != testDate(t, 2024, 1, 2) {
		t.Errorf("reversal date = %s", reversal.Date)
	}
	if reversal.ValueDate == nil || *reversal.ValueDate != testDate(t, 2023, 12, 31) {
		t.Errorf("reversal value date = %v", reversal.ValueDate)
	}
	if got, want := reversal.Amount, dec(t, "-100.00"); got.Cmp(want) != 0 {
		t.Errorf("reversal amount = %s", got)
	}
	if reversal.BankReference == nil || *reversal.BankReference != "RUECKLASTSCHRIFT" {
		t.Errorf("reversal reference = %v", reversal.BankReference)
	}
}

func TestMT940Structured86WithGVCPartnerIBANBICAndWrappedSubfieldMarker(t *testing.T) {
	text := `:20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:2401020103C1234,56NTRF
:86:166?00GUTSCHRIFT?100005?20EREF+X?21SVWZ+Rechnung 4711?2
0Weitere Zeile?30COBADEFFXXX?31DE89370400440532013000?32Max Muster
:62F:C240102EUR1234,56`
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	booking := statement.Bookings[0]
	if booking.Date != testDate(t, 2024, 1, 3) {
		t.Errorf("date = %s", booking.Date)
	}
	if booking.ValueDate == nil || *booking.ValueDate != testDate(t, 2024, 1, 2) {
		t.Errorf("value date = %v", booking.ValueDate)
	}
	if got, want := booking.Amount, dec(t, "1234.56"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if booking.PartnerName == nil || *booking.PartnerName != "Max Muster" {
		t.Errorf("partner = %v", booking.PartnerName)
	}
	if booking.PartnerIBAN == nil || *booking.PartnerIBAN != "DE89370400440532013000" {
		t.Errorf("partner iban = %v", booking.PartnerIBAN)
	}
	if booking.Purpose == nil || *booking.Purpose != "EREF+XSVWZ+Rechnung 4711Weitere Zeile" {
		t.Errorf("purpose = %v", booking.Purpose)
	}
}

func TestMT940PurposeContainingATimeIsNotCutAtAFalseTag(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:240102D10,00N066NONREF\n:86:2026-10-03T14:30:15 meeting at 14:30:15\n:62F:C240102EUR10,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Purpose == nil || *statement.Bookings[0].Purpose != "2026-10-03T14:30:15 meeting at 14:30:15" {
		t.Errorf("purpose = %v", statement.Bookings[0].Purpose)
	}
}

func TestMT940FundsCodeAfterDebitCreditMark(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:240102DR10,50N066NONREF\n:86:?20Coffee\n:62F:C240102EUR10,50"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-10.50"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	reversal := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:240102RCR10,50N066NONREF\n:86:?20Refund\n:62F:C240102EUR10,50"
	reversalStatement, err := ParseMT940WithToday([]byte(reversal), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := reversalStatement.Bookings[0].Amount, dec(t, "10.50"); got.Cmp(want) != 0 {
		t.Errorf("reversal amount = %s", got)
	}
}

func TestMT940WithA20DigitAmountSkipsTheRecordInsteadOfImportingIt(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:2401020103C99999999999999999999NTRF\n:86:?20Huge\n:62F:C240102EUR0,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 0 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if len(statement.Skipped) != 1 || !hasPrefix(statement.Skipped[0], "Record 1: invalid amount") {
		t.Errorf("skipped = %v", statement.Skipped)
	}
}

func TestMT940BankReferenceAfterSlashesAndSupplementaryDetails(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:240102D25,00NTRFEXAMPLE//1234567890\n extra line ignored\n:86:?20Taxi\n:62F:C240102EUR25,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].BankReference == nil || *statement.Bookings[0].BankReference != "1234567890" {
		t.Errorf("reference = %v", statement.Bookings[0].BankReference)
	}
}

func TestMT25CurrencyAppendedToIBAN(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000EUR\n:28C:1\n:60F:C240102EUR0,00\n:61:240102D5,00N066NONREF\n:86:?20Test\n:62F:C240102EUR5,00"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if statement.AccountIBAN == nil || *statement.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("account iban = %v", statement.AccountIBAN)
	}
}

func ofxSGMLFixture() string {
	return "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\nOLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n<BANKMSGSRSV1>\n<STMTTRNRS>\n<STMTRS>\n<CURDEF>EUR</CURDEF>\n<BANKACCTFROM>\n<BANKID>37040044</BANKID>\n<ACCTID>DE89370400440532013000</ACCTID>\n<ACCTTYPE>CHECKING</ACCTTYPE>\n</BANKACCTFROM>\n<BANKTRANLIST>\n<DTSTART>20261003000000</DTSTART>\n<DTEND>20261004000000</DTEND>\n<STMTTRN>\n<TRNTYPE>DEBIT</TRNTYPE>\n<DTPOSTED>20261003000000</DTPOSTED>\n<TRNAMT>-45.67</TRNAMT>\n<FITID>REF-1</FITID>\n<NAME>REWE Markt GmbH</NAME>\n<MEMO>REWE SAGT DANKE 1234</MEMO>\n</STMTTRN>\n<STMTTRN>\n<TRNTYPE>CREDIT</TRNTYPE>\n<DTPOSTED>20261001000000</DTPOSTED>\n<TRNAMT>3200.00</TRNAMT>\n<FITID>REF-2</FITID>\n<NAME>Müller &amp; Söhne GmbH</NAME>\n<MEMO>Gehalt Oktober</MEMO>\n</STMTTRN>\n</BANKTRANLIST>\n</STMTRS>\n</STMTTRNRS>\n</BANKMSGSRSV1>\n</OFX>"
}

func ofxXMLFixture() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<OFX>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <STMTRS>
        <CURDEF>USD</CURDEF>
        <BANKACCTFROM>
          <ACCTID>DE89370400440532013000</ACCTID>
        </BANKACCTFROM>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261003000000</DTPOSTED>
            <TRNAMT>-45.67</TRNAMT>
            <FITID>REF-1</FITID>
            <NAME>REWE Markt GmbH</NAME>
            <MEMO>REWE SAGT DANKE 1234</MEMO>
          </STMTTRN>
          <STMTTRN>
            <TRNTYPE>CREDIT</TRNTYPE>
            <DTPOSTED>20261001000000</DTPOSTED>
            <TRNAMT>3200.00</TRNAMT>
            <FITID>REF-2</FITID>
            <NAME>Müller &amp; Söhne GmbH</NAME>
            <MEMO>Gehalt Oktober</MEMO>
          </STMTTRN>
        </BANKTRANLIST>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>`
}

func TestOFXSGMLAndXMLProduceTheSameBookingsWithFITIDAsBankReference(t *testing.T) {
	sgml, err := ParseOFX([]byte(ofxSGMLFixture()))
	if err != nil {
		t.Fatal(err)
	}
	xml, err := ParseOFX([]byte(ofxXMLFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if sgml.Format != FormatOFX || xml.Format != FormatOFX {
		t.Errorf("formats = %s, %s", sgml.Format, xml.Format)
	}
	if sgml.AccountIBAN == nil || *sgml.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("sgml account = %v", sgml.AccountIBAN)
	}
	if xml.AccountIBAN == nil || *xml.AccountIBAN != "DE89370400440532013000" {
		t.Errorf("xml account = %v", xml.AccountIBAN)
	}
	if len(sgml.Bookings) != 2 || len(xml.Bookings) != 2 {
		t.Fatalf("bookings = %d, %d", len(sgml.Bookings), len(xml.Bookings))
	}
	if len(sgml.Skipped) != 0 || len(xml.Skipped) != 0 {
		t.Errorf("skipped = %v, %v", sgml.Skipped, xml.Skipped)
	}
	for index, want := range []string{"REF-1", "REF-2"} {
		if sgml.Bookings[index].BankReference == nil || *sgml.Bookings[index].BankReference != want {
			t.Errorf("sgml reference[%d] = %v", index, sgml.Bookings[index].BankReference)
		}
		if xml.Bookings[index].BankReference == nil || *xml.Bookings[index].BankReference != want {
			t.Errorf("xml reference[%d] = %v", index, xml.Bookings[index].BankReference)
		}
	}
	first := sgml.Bookings[0]
	if first.Date != testDate(t, 2026, 10, 3) {
		t.Errorf("date = %s", first.Date)
	}
	if got, want := first.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if first.PartnerName == nil || *first.PartnerName != "REWE Markt GmbH" {
		t.Errorf("partner = %v", first.PartnerName)
	}
	if first.Purpose == nil || *first.Purpose != "REWE SAGT DANKE 1234" {
		t.Errorf("purpose = %v", first.Purpose)
	}
	if first.Currency == nil || *first.Currency != "EUR" {
		t.Errorf("currency = %v", first.Currency)
	}
	if xml.Bookings[0].Currency == nil || *xml.Bookings[0].Currency != "USD" {
		t.Errorf("xml currency = %v", xml.Bookings[0].Currency)
	}
}

func TestOFXSGMLInWindows1252WithUmlautsAndDecodedAmpersand(t *testing.T) {
	text := cp1252(t, "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\nOLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n<BANKMSGSRSV1>\n<STMTTRNRS>\n<STMTRS>\n<BANKACCTFROM>\n<ACCTID>DE89370400440532013000</ACCTID>\n</BANKACCTFROM>\n<BANKTRANLIST>\n<STMTTRN>\n<TRNTYPE>DEBIT</TRNTYPE>\n<DTPOSTED>20261003000000</DTPOSTED>\n<TRNAMT>-45,67</TRNAMT>\n<FITID>REF-1</FITID>\n<NAME>Müller &amp; Söhne GmbH</NAME>\n<MEMO>äöü ß &amp;lt;b&amp;gt;</MEMO>\n</STMTTRN>\n</BANKTRANLIST>\n</STMTRS>\n</STMTTRNRS>\n</BANKMSGSRSV1>\n</OFX>")
	statement, err := ParseOFX(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	booking := statement.Bookings[0]
	if got, want := booking.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if booking.PartnerName == nil || *booking.PartnerName != "Müller & Söhne GmbH" {
		t.Errorf("partner = %v", booking.PartnerName)
	}
	if booking.Purpose == nil || *booking.Purpose != "äöü ß &lt;b&gt;" {
		t.Errorf("purpose = %v", booking.Purpose)
	}
}

func TestOFXWithAnOutOfRangeAmountSkipsTheRecordInsteadOfImportingIt(t *testing.T) {
	text := "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\nOLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n<BANKMSGSRSV1>\n<STMTTRNRS>\n<STMTRS>\n<CURDEF>EUR</CURDEF>\n<BANKTRANLIST>\n<STMTTRN>\n<TRNTYPE>DEBIT</TRNTYPE>\n<DTPOSTED>20261003000000</DTPOSTED>\n<TRNAMT>1E999999999</TRNAMT>\n<FITID>REF-1</FITID>\n<NAME>REWE Markt GmbH</NAME>\n</STMTTRN>\n</BANKTRANLIST>\n</STMTRS>\n</STMTTRNRS>\n</BANKMSGSRSV1>\n</OFX>"
	statement, err := ParseOFX([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 0 {
		t.Errorf("bookings = %d", len(statement.Bookings))
	}
	if len(statement.Skipped) != 1 || !contains(statement.Skipped[0], "invalid amount") {
		t.Errorf("skipped = %v", statement.Skipped)
	}
}

func TestOFXXMLWithDoctypeAndExternalEntityIsRejected(t *testing.T) {
	text := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE ofx [<!ENTITY x SYSTEM "file:///etc/passwd">]>
<OFX>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <STMTRS>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261003000000</DTPOSTED>
            <TRNAMT>-45.67</TRNAMT>
            <FITID>X</FITID>
            <NAME>&x;</NAME>
          </STMTTRN>
        </BANKTRANLIST>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>`
	if _, err := ParseOFX([]byte(text)); err == nil {
		t.Error("XXE OFX was accepted")
	}
}

func TestQIFWithUSDatesAndThousandsSeparator(t *testing.T) {
	text := "!Type:Bank\nD10/03/2026\nT-45.67\nPREWE Markt GmbH\nMREWE SAGT DANKE 1234\nN123\n^\nD10/01/26\nT3,200.00\nPMüller & Söhne GmbH\nMGehalt Oktober\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if statement.Format != FormatQIF {
		t.Errorf("format = %s", statement.Format)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	first := statement.Bookings[0]
	if first.Date != testDate(t, 2026, 10, 3) {
		t.Errorf("date = %s", first.Date)
	}
	if got, want := first.Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount = %s", got)
	}
	if first.Purpose == nil || *first.Purpose != "REWE SAGT DANKE 1234" {
		t.Errorf("purpose = %v", first.Purpose)
	}
	if first.BankReference != nil {
		t.Errorf("reference = %v", first.BankReference)
	}
	if statement.Bookings[1].Date != testDate(t, 2026, 10, 1) {
		t.Errorf("date 1 = %s", statement.Bookings[1].Date)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "3200.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
}

func TestQIFWithGermanDottedDates(t *testing.T) {
	text := "!Type:Bank\nD03.10.2026\nT-45,67\nPREWE Markt GmbH\n^\nD01.10.26\nT3.200,00\nPMüller & Söhne GmbH\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Date != testDate(t, 2026, 10, 3) {
		t.Errorf("date 0 = %s", statement.Bookings[0].Date)
	}
	if got, want := statement.Bookings[0].Amount, dec(t, "-45.67"); got.Cmp(want) != 0 {
		t.Errorf("amount 0 = %s", got)
	}
	if got, want := statement.Bookings[1].Amount, dec(t, "3200.00"); got.Cmp(want) != 0 {
		t.Errorf("amount 1 = %s", got)
	}
}

func TestQIFDayGreaterThan12DisambiguatesSlashDatesToDayMonth(t *testing.T) {
	text := "!Type:Bank\nD13/10/2026\nT-45.67\nPShop\n^\nD14/10/26\nT-20.00\nPCafe\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Date != testDate(t, 2026, 10, 13) {
		t.Errorf("date 0 = %s", statement.Bookings[0].Date)
	}
	if statement.Bookings[1].Date != testDate(t, 2026, 10, 14) {
		t.Errorf("date 1 = %s", statement.Bookings[1].Date)
	}
}

func TestQIFApostropheYearSeparator(t *testing.T) {
	text := "!Type:Bank\nD10/03'26\nT-12.00\nPKiosk\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 || statement.Bookings[0].Date != testDate(t, 2026, 10, 3) {
		t.Errorf("bookings = %v", statement.Bookings)
	}
}

func TestQIFUnpaddedAndSpacedDates(t *testing.T) {
	text := "!Type:Bank\nD1/5/26\nT-10.00\nPA\n^\nD 1/ 5'26\nT-11.00\nPB\n^\nD5.1.2026\nT-12.00\nPC\n^\nD12/25/2026\nT-13.00\nPD\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 4 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	want := []civilDate{
		testDate(t, 2026, 1, 5),
		testDate(t, 2026, 1, 5),
		testDate(t, 2026, 1, 5),
		testDate(t, 2026, 12, 25),
	}
	for index, date := range want {
		if statement.Bookings[index].Date != date {
			t.Errorf("date[%d] = %s, want %s", index, statement.Bookings[index].Date, date)
		}
	}
}

func TestQIFWithBOMAndAccountHeader(t *testing.T) {
	text := "\uFEFF!Account\nNMy Bank\nTBank\n^\n!Type:Bank\nD10/03/2026\nT-45.67\nPREWE Markt GmbH\nMREWE SAGT DANKE\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 1 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	if statement.Bookings[0].Date != testDate(t, 2026, 10, 3) {
		t.Errorf("date = %s", statement.Bookings[0].Date)
	}
}

func TestQIFSkipsNonBankTypes(t *testing.T) {
	text := "!Type:Bank\nD10/03/2026\nT-45.67\nPREWE\n^\n!Type:Invst\nD10/03/2026\nT-100.00\nPBroker\n^\n!Type:Bank\nD10/04/2026\nT-12.00\nPCafe\n^"
	statement, err := ParseQIF([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 2 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	names := []string{}
	for _, booking := range statement.Bookings {
		if booking.PartnerName != nil {
			names = append(names, *booking.PartnerName)
		}
	}
	if len(names) != 2 || names[0] != "REWE" || names[1] != "Cafe" {
		t.Errorf("names = %q", names)
	}
}

func TestServiceRoutesSTAOFXQIFAndTXTMT940ToCorrectParsers(t *testing.T) {
	today := limitationToday(t)
	mt940 := []byte(moreFormatsMT940Text())
	if statement, err := Read("stmt.sta", mt940, nil, today); err != nil || statement.Format != FormatMT940 {
		t.Errorf("sta = %v, %v", statement.Format, err)
	}
	if statement, err := Read("stmt.txt", mt940, nil, today); err != nil || statement.Format != FormatMT940 {
		t.Errorf("txt = %v, %v", statement.Format, err)
	}
	ofx := []byte(ofxSGMLFixture())
	if statement, err := Read("stmt.ofx", ofx, nil, today); err != nil || statement.Format != FormatOFX {
		t.Errorf("ofx = %v, %v", statement.Format, err)
	}
	qif := []byte("!Type:Bank\nD10/03/2026\nT-45.67\nPREWE Markt GmbH\nMREWE SAGT DANKE 1234\n^")
	if statement, err := Read("stmt.qif", qif, nil, today); err != nil || statement.Format != FormatQIF {
		t.Errorf("qif = %v, %v", statement.Format, err)
	}
}

func moreFormatsMT940Text() string {
	return ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:00001/001\n:60F:C231030EUR1234,56\n:61:2310301031D45,67N066NONREF\n:86:166?00KARTENZAHLUNG?100005?20REWE SAGT DANKE?211234?32REWE Markt GmbH\n:61:2310311102C500,00N066NONREF\n:86:166?00GUTSCHRIFT?100005?20Gehalt Oktober?32Müller & Söhne GmbH\n:62F:C231031EUR1688,89"
}

func hasPrefix(text, prefix string) bool {
	return len(text) >= len(prefix) && text[:len(prefix)] == prefix
}

func contains(text, part string) bool {
	return len(part) == 0 || (len(text) >= len(part) && indexOf(text, part) >= 0)
}

func indexOf(text, part string) int {
	for index := 0; index+len(part) <= len(text); index++ {
		if text[index:index+len(part)] == part {
			return index
		}
	}
	return -1
}
