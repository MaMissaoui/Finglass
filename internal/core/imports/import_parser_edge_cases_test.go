package imports

import (
	"testing"
)

func TestATruncatedMT940DetailMarkerDoesNotAbortTheFile(t *testing.T) {
	text := ":20:STARTUMS\n:25:DE89370400440532013000\n:28C:1\n:60F:C240102EUR0,00\n:61:2401020102C10,00N066REF1\n:86:?20First\n:61:2401030103C20,00N\n:61:2401040104D5,50N066REF3\n:86:?20Third\n:62F:C240104EUR24,50"
	statement, err := ParseMT940WithToday([]byte(text), limitationToday(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Skipped) != 0 {
		t.Errorf("skipped = %v", statement.Skipped)
	}
	if len(statement.Bookings) != 3 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	wantAmounts := []string{"10.00", "20.00", "-5.50"}
	for index, amount := range wantAmounts {
		if got, want := statement.Bookings[index].Amount, dec(t, amount); got.Cmp(want) != 0 {
			t.Errorf("amount[%d] = %s, want %s", index, got, want)
		}
	}
	wantDates := []civilDate{
		testDate(t, 2024, 1, 2),
		testDate(t, 2024, 1, 3),
		testDate(t, 2024, 1, 4),
	}
	for index, date := range wantDates {
		if statement.Bookings[index].Date != date {
			t.Errorf("date[%d] = %s, want %s", index, statement.Bookings[index].Date, date)
		}
	}
	if statement.Bookings[0].BankReference == nil || *statement.Bookings[0].BankReference != "REF1" {
		t.Errorf("reference 0 = %v", statement.Bookings[0].BankReference)
	}
	if statement.Bookings[1].BankReference != nil {
		t.Errorf("reference 1 = %v", statement.Bookings[1].BankReference)
	}
	if statement.Bookings[2].BankReference == nil || *statement.Bookings[2].BankReference != "REF3" {
		t.Errorf("reference 2 = %v", statement.Bookings[2].BankReference)
	}
}

func TestOFXAmountsWithBothSeparatorsUseTheLastOneAsDecimalSeparator(t *testing.T) {
	transaction := func(date string, amount string, fitID string) string {
		return "<STMTTRN>\n<TRNTYPE>DEBIT</TRNTYPE>\n<DTPOSTED>" + date +
			"</DTPOSTED>\n<TRNAMT>" + amount + "</TRNAMT>\n<FITID>" + fitID +
			"</FITID>\n<NAME>Test</NAME>\n</STMTTRN>"
	}
	text := "OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\nOLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n<BANKMSGSRSV1>\n<STMTTRNRS>\n<STMTRS>\n<CURDEF>EUR</CURDEF>\n<BANKACCTFROM>\n<ACCTID>DE89370400440532013000</ACCTID>\n</BANKACCTFROM>\n<BANKTRANLIST>\n" +
		transaction("20261001000000", "-1.234,56", "REF-1") + "\n" +
		transaction("20261002000000", "-1,234.56", "REF-2") + "\n" +
		transaction("20261003000000", "-45,67", "REF-3") + "\n" +
		transaction("20261004000000", "-45.67", "REF-4") + "\n" +
		transaction("20261005000000", "1234", "REF-5") + "\n" +
		transaction("20261006000000", "1.234.567", "REF-6") + "\n" +
		transaction("20261007000000", "1,234,567", "REF-7") + "\n" +
		"</BANKTRANLIST>\n</STMTRS>\n</STMTTRNRS>\n</BANKMSGSRSV1>\n</OFX>"
	statement, err := ParseOFX([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Bookings) != 5 {
		t.Fatalf("bookings = %d", len(statement.Bookings))
	}
	wantAmounts := []string{"-1234.56", "-1234.56", "-45.67", "-45.67", "1234"}
	for index, amount := range wantAmounts {
		if got, want := statement.Bookings[index].Amount, dec(t, amount); got.Cmp(want) != 0 {
			t.Errorf("amount[%d] = %s, want %s", index, got, want)
		}
	}
	wantSkipped := []string{
		"Record 6: invalid amount '1.234.567'",
		"Record 7: invalid amount '1,234,567'",
	}
	if len(statement.Skipped) != len(wantSkipped) {
		t.Fatalf("skipped = %v", statement.Skipped)
	}
	for index, skipped := range wantSkipped {
		if statement.Skipped[index] != skipped {
			t.Errorf("skipped[%d] = %q, want %q", index, statement.Skipped[index], skipped)
		}
	}
}
