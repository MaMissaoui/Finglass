package trace

import (
	"fmt"
	"strings"
	"testing"
)

func redacted(message string) string {
	return strings.Join(RedactMessage([]byte(message)), "\n")
}

func assertContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("trace does not contain %q:\n%s", want, got)
		}
	}
}

func assertAbsent(t *testing.T, got string, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if marker == "" {
			continue
		}
		if strings.Contains(got, marker) {
			t.Fatalf("trace contains marker %q:\n%s", marker, got)
		}
	}
}

func TestRedactMessageMasksEveryRule(t *testing.T) {
	const (
		pin       = "HNSHAPINMARK0001"
		cust      = "HKIDNCUSTMARK0002"
		system    = "HKIDNSYSTEMMARK0003"
		product   = "HKVVBPRODUCTMARK0004"
		task      = "HKTANTASKMARK0005"
		bankTxt   = "HIRMSTEXTMARK0006"
		owner     = "HIUPDOWNERMARK0007"
		prodName  = "HIUPDPRODUCTNAME0008"
		amount    = "9876543210,99"
		iban      = "DE00999999990000000001"
		acct      = "HISPAACCOUNT0009"
		sub       = "HISPASUB00010"
		method    = "HITANSMETHODNAME011"
		mt940     = "MT940PURPOSEMARK0012"
		camt      = "CAMTENTRYMARK0013"
		challenge = "HITANCHALLENGE0014"
	)

	cases := []struct {
		name    string
		message string
		absent  []string
		present []string
	}{
		{
			name:    "HNSHA",
			message: "HNSHA:4:2+123456+" + pin + "'",
			absent:  []string{pin},
			present: []string{"<redacted 2 groups>"},
		},
		{
			name:    "HKIDN",
			message: "HKIDN:3:2+280:99999999+" + cust + "+" + system + "+1'",
			absent:  []string{cust, system},
			present: []string{"280:99999999", "<customer id>", "<system id>"},
		},
		{
			name:    "HKVVB",
			message: "HKVVB:4:3+1+1+1+" + product + "+1'",
			absent:  []string{product},
			present: []string{"<product id>"},
		},
		{
			name:    "HKTAN",
			message: "HKTAN:5:6+2+HKIDN+++" + task + "+J'",
			absent:  []string{task},
			present: []string{"HKTAN:5:6 | 2 | HKIDN"},
		},
		{
			name:    "HIRMS text",
			message: "HIRMS:3:2:4+0010::" + bankTxt + "'",
			absent:  []string{bankTxt},
			present: []string{"0010::<text …0006>"},
		},
		{
			name: "HIUPD owner and product name",
			message: "HIUPD:5:6+" + acct + ":" + sub + ":280:99999999+" + iban +
				"+CUSTMARK0002+1+EUR+" + owner + "++" + prodName + "+1234,56+HKKAZ'",
			absent:  []string{owner, prodName, acct, sub, iban},
			present: []string{"…0009:…0010:280:99999999", "…0001", "HKKAZ"},
		},
		{
			name: "HIUPD limit amount in an allowed transaction",
			message: "HIUPD:5:6+" + acct + ":" + sub + ":280:99999999+" + iban +
				"+CUSTMARK0002+1+EUR+" + owner + "++" + prodName + "+E:777555,:EUR:7+HKKAZ:1:Z:98765:EUR:7'",
			absent:  []string{"777555", "98765", owner},
			present: []string{"HKKAZ:1:<redacted len=1>:<redacted len=5>:"},
		},
		{
			name: "HIUPD older layout: an uppercase owner in the type and currency fields",
			message: "HIUPD:5:6+" + acct + ":" + sub + ":280:99999999+" + iban +
				"+CUSTMARK0002+MUELLER+SCHMIDT+" + owner + "'",
			absent:  []string{"MUELLER", "SCHMIDT"},
			present: []string{"<redacted len=7>"},
		},
		{
			name: "HISAL v7 with a full KTI keeps only BIC, 280 and BLZ",
			message: "HISAL:5:7:4+DE00999999990000000001:TESTDEFFXXX:ACCTMARK7777:SUBMARK8888:280:99999999" +
				"+PRODUCTNAME000+EUR+C:1,00:EUR:20260315'",
			absent:  []string{"ACCTMARK7777", "SUBMARK8888", "DE00999999990000000001"},
			present: []string{"…0001:TESTDEFFXXX:…7777:…8888:280:99999999"},
		},
		{
			name: "HICAZ with a full KTI keeps only BIC, 280 and BLZ",
			message: "HICAZ:5:1:4+DE00999999990000000001:TESTDEFFXXX:ACCTMARK7777:SUBMARK8888:280:99999999" +
				"+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02'",
			absent:  []string{"ACCTMARK7777", "SUBMARK8888", "DE00999999990000000001"},
			present: []string{"…0001:TESTDEFFXXX:…7777:…8888:280:99999999"},
		},
		{
			name:    "an 8-digit account number before 280 is masked",
			message: "HISAL:5:6:4+12345678::280:99999999+PRODUCTNAME000+EUR+C:1,00:EUR:20260315'",
			absent:  []string{"12345678"},
			present: []string{"…5678::280:99999999"},
		},
		{
			name:    "HITAB keeps only medium class and status",
			message: "HITAB:5:4:4+0+M:1:::::::ERIKA:123456'",
			absent:  []string{"ERIKA", "123456"},
			present: []string{"M:1:"},
		},
		{
			name: "HISAL amount",
			message: "HISAL:5:7:4+1::280:99999999+PRODUCTNAME000+EUR+" +
				"C:" + amount + ":EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'",
			absent:  []string{amount, "25,50", "5000,00", "900,00", "PRODUCTNAME000"},
			present: []string{"<amount>", "20260315", "20260314"},
		},
		{
			name:    "HIKAZ payload",
			message: hikazMessage(mt940),
			absent:  []string{mt940},
			present: []string{"<mt940 "},
		},
		{
			name:    "HICAZ payload",
			message: hicazMessage(camt),
			absent:  []string{camt},
			present: []string{"<camt "},
		},
		{
			name:    "HITAN challenge",
			message: "HITAN:4:7:3+4+task-hash+TASKREF00014+" + challenge + "+@0@'",
			absent:  []string{challenge, "TASKREF00014"},
			present: []string{"<challenge len="},
		},
		{
			name: "HISPA IBAN",
			message: "HISPA:2:6+J:" + iban + ":TESTDEFFXXX:" + acct + ":" + sub +
				":280:99999999'",
			absent:  []string{iban, acct, sub},
			present: []string{"J:…0001:TESTDEFFXXX:…0009:…0010:280:99999999"},
		},
		{
			name: "parameter segment text",
			message: "HITANS:5:7:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:" +
				method + ":6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:1:1'",
			absent:  []string{method},
			present: []string{"942", "1.0"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := redacted(testCase.message)
			assertAbsent(t, got, testCase.absent...)
			for _, want := range testCase.present {
				assertContains(t, got, want)
			}
		})
	}
}

func hikazMessage(marker string) string {
	body := ":20:STARTUMS\n:25:99999999/1234567890\n" +
		":61:2603150315C1234,56NTRF" + marker + "\n" +
		":86:PURPOSE " + marker + " COUNTERPARTY\n"
	return "HIKAZ:5:7:4+@" + fmt.Sprint(len(body)) + "@" + body + "+@0@'"
}

func hicazMessage(marker string) string {
	body := "<Document><BkToCstmrStmt><Stmt><Ntry><Amt>" + marker +
		"</Amt><NtryDtls><TxDtls><RltdPties><Cdtr><Nm>" + marker +
		"</Nm></Cdtr></RltdPties></TxDtls></NtryDtls></Ntry></Stmt></BkToCstmrStmt></Document>"
	return "HICAZ:5:1:3+DE00999999990000000001:TESTDEFFXXX+" +
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.02+@" +
		fmt.Sprint(len(body)) + "@" + body + "+@0@'"
}

func TestRedactMessageKeepsCodesAndVersions(t *testing.T) {
	message := "HIRMG:2:2+0010::OK+3920::Allowed:942:962'"
	got := redacted(message)
	assertContains(t, got, "HIRMG:2:2 | 0010::<text …OK> | 3920::<text …owed>:942:962")
}

func TestRedactMessageUnparseable(t *testing.T) {
	raw := "not a segment with SECRETMARK"
	got := redacted(raw)
	if got != "<unparseable message, 29 bytes>" {
		t.Fatalf("unparseable trace = %q", got)
	}
	assertAbsent(t, got, "SECRETMARK", "not a segment")
}

func TestRedactMessageUnknownSegments(t *testing.T) {
	for _, message := range []string{
		"HIXYZ:1:1+SECRETMARK+2'",
		"HKXYZ:1:1+SECRETMARK+2'",
	} {
		got := redacted(message)
		assertContains(t, got, ":1:1 | <redacted 2 groups>")
		assertAbsent(t, got, "SECRETMARK")
	}
}

func TestRedactMessageEncryptedPayload(t *testing.T) {
	inner := "HNSHK:2:4+999+SECRETMARK'HKIDN:3:2+280:99999999+CUSTMARK+SYSTEMMARK+1'"
	message := "HNVSD:999:1+@" + fmt.Sprint(len(inner)) + "@" + inner + "'"
	lines := RedactMessage([]byte(message))
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != "HNVSD:999:1 | <encrypted part follows>" {
		t.Fatalf("encrypted line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  HNSHK:2:4 | ") {
		t.Fatalf("inner HNSHK line = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "  HKIDN:3:2 | ") {
		t.Fatalf("inner HKIDN line = %q", lines[2])
	}
	assertAbsent(t, strings.Join(lines, "\n"), "SECRETMARK", "CUSTMARK", "SYSTEMMARK")
}

func TestRedactMessageEncryptedPayloadUnparseable(t *testing.T) {
	inner := "raw SECRETMARK bytes"
	message := "HNVSD:999:1+@" + fmt.Sprint(len(inner)) + "@" + inner + "'"
	got := redacted(message)
	assertContains(t, got, "  <unparseable message, 20 bytes>")
	assertAbsent(t, got, "SECRETMARK")
}

func TestRedactMessageRequestDatesAndTouchdown(t *testing.T) {
	message := "HKKAZ:2:7+DE00999999990000000001:TESTDEFFXXX+N+20260101+20260331+N+TOUCHDOWNMARK'"
	got := redacted(message)
	assertContains(t, got, "20260101", "20260331", "<touchdown>")
	assertAbsent(t, got, "TOUCHDOWNMARK")

	message = "HKCAZ:2:1+DE00999999990000000001:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02+J+20260101+20260331+10+TOUCHDOWNMARK'"
	got = redacted(message)
	assertContains(t, got, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02", "20260101", "20260331", "<touchdown>")
	assertAbsent(t, got, "TOUCHDOWNMARK")
}

func TestRedactMessageHeaderDialogAndSystem(t *testing.T) {
	got := redacted("HNHBK:1:3+000000000123+300+DIALOGMARK0001+1'" +
		"HNHBS:2:1+1'" +
		"HKEND:3:1+DIALOGMARK0001'")
	assertContains(t, got, "<len>", "300", "<dialog id>")
	assertAbsent(t, got, "DIALOGMARK0001", "000000000123")
}

func TestRedactMessageHNSHKAndHNVSK(t *testing.T) {
	got := redacted("HNVSK:998:3+SECRETMARK+2'" +
		"HNSHK:2:4+PIN:1+999+REFMARK+1+1+2::SYSTEMMARK+1+1:20260329:123456+1:999:1+6:10:16+280:99999999:USERMARK:S:0:0'")
	assertContains(t, got, "HNVSK:998:3 | <redacted 2 groups>", "HNSHK:2:4 | 999 | <redacted 10 groups>")
	assertAbsent(t, got, "SECRETMARK", "REFMARK", "SYSTEMMARK", "USERMARK")

	got = redacted("HNVSK:998:3+a+b+c+d+e+f'" +
		"HNSHK:2:4+999+a+b+c+d+e+f+g+h'")
	assertContains(t, got, "HNVSK:998:3 | <redacted 6 groups>", "HNSHK:2:4 | 999 | <redacted 8 groups>")
}

func TestRedactMessageCodeRules(t *testing.T) {
	got := redacted("HISALS:1:5+1+1+1+1'")
	assertContains(t, got, "HISALS:1:5 | 1 | 1 | 1 | 1")
	assertAbsent(t, got, "<redacted")

	got = redacted("HISALS:1:5+FREE TEXT+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02+1.2.3.4+J'")
	assertContains(t, got, "<redacted len=9>", "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02", "1.2.3.4", "J")
}

func TestRedactNote(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"bank 1 of 3", "bank 1 of 3"},
		{"DE00999999990000000001", "…0001"},
		{"1234567", "…4567"},
		{"account DE00999999990000000001 ok", "account …0001 ok"},
		{strings.Repeat("a", 100), strings.Repeat("a", 80)},
		{"drop\u0000this", "dropthis"},
	}
	for _, testCase := range cases {
		if got := RedactNote(testCase.input); got != testCase.want {
			t.Fatalf("RedactNote(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}
