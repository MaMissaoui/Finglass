package fints

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Builder parity: src/segments/builder.rs tests, with invented identifiers.

const (
	camtFormat052 = "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"
	camtFormat053 = "urn:iso:std:iso:20022:tech:xsd:camt.053.001.02"
)

// testAccount is the invented account used by the account-form wire tests.
func testAccount() Account {
	return Account{
		IBAN:          testIBAN,
		BIC:           testBIC,
		AccountNumber: "1234567",
		SubAccount:    "",
		BLZ:           testBLZ,
	}
}

func TestSegmentFormattingShowsNoContent(t *testing.T) {
	segment := NewHKSAL(7, testAccount(), nil)
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		got := fmt.Sprintf(format, segment)
		if !strings.Contains(got, "HKSAL") {
			t.Fatalf("%s = %q, want the segment type", format, got)
		}
		for _, forbidden := range []string{testIBAN, testBIC, "1234567"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s of Segment leaked %q: %q", format, forbidden, got)
			}
		}
	}
}

func TestKTIProducesTwoDataElements(t *testing.T) {
	segment := NewHKSAL(7, testAccount(), nil)
	if len(segment.degs) < 2 {
		t.Fatalf("HKSAL has %d groups, want at least 2", len(segment.degs))
	}
	account := segment.degs[1]
	if account.Len() != 2 {
		t.Fatalf("KTI group has %d elements, want 2", account.Len())
	}
	if got := account.Text(0); got != testIBAN {
		t.Fatalf("KTI IBAN = %q", got)
	}
	if got := account.Text(1); got != testBIC {
		t.Fatalf("KTI BIC = %q", got)
	}
}

func TestKTISerializesAsIBANColonBIC(t *testing.T) {
	wire := segmentWire(t, NewHKSAL(7, testAccount(), nil), 3)
	if wire != "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N'" {
		t.Fatalf("HKSAL v7 wire = %q", wire)
	}
}

func TestHKSALAccountFormPerVersion(t *testing.T) {
	withSubAccount := testAccount()
	withSubAccount.SubAccount = "0001"
	escapedAccountNumber := testAccount()
	escapedAccountNumber.AccountNumber = "1234?567"

	cases := []struct {
		name    string
		version uint16
		account Account
		want    string
	}{
		{
			name:    "version 5 national",
			version: 5,
			account: testAccount(),
			want:    "HKSAL:3:5+1234567::280:99999999+N'",
		},
		{
			name:    "version 6 national",
			version: 6,
			account: testAccount(),
			want:    "HKSAL:3:6+1234567::280:99999999+N'",
		},
		{
			name:    "version 7 international",
			version: 7,
			account: testAccount(),
			want:    "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N'",
		},
		{
			name:    "version 6 with sub-account",
			version: 6,
			account: withSubAccount,
			want:    "HKSAL:3:6+1234567:0001:280:99999999+N'",
		},
		{
			name:    "version 6 escapes the account number",
			version: 6,
			account: escapedAccountNumber,
			want:    "HKSAL:3:6+1234??567::280:99999999+N'",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := segmentWire(t, NewHKSAL(testCase.version, testCase.account, nil), 3)
			if got != testCase.want {
				t.Fatalf("wire = %q\nwant %q", got, testCase.want)
			}
		})
	}
}

func TestHKSALWithTouchdown(t *testing.T) {
	touchdown := "TOUCH123"
	got := segmentWire(t, NewHKSAL(7, testAccount(), &touchdown), 3)
	if got != "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N++TOUCH123'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKKAZAccountFormPerVersion(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	touchdown := "TOUCH123"
	withSubAccount := testAccount()
	withSubAccount.SubAccount = "0001"
	escapedAccountNumber := testAccount()
	escapedAccountNumber.AccountNumber = "1234?567"

	cases := []struct {
		name      string
		version   uint16
		account   Account
		touchdown *string
		want      string
	}{
		{
			name:    "version 5 national",
			version: 5,
			account: testAccount(),
			want:    "HKKAZ:3:5+1234567::280:99999999+N+20250329+20260329'",
		},
		{
			name:    "version 6 national",
			version: 6,
			account: testAccount(),
			want:    "HKKAZ:3:6+1234567::280:99999999+N+20250329+20260329'",
		},
		{
			name:    "version 7 international",
			version: 7,
			account: testAccount(),
			want:    "HKKAZ:3:7+DE00999999990000000001:TESTDEFFXXX+N+20250329+20260329'",
		},
		{
			name:      "version 5 national with touchdown",
			version:   5,
			account:   testAccount(),
			touchdown: &touchdown,
			want:      "HKKAZ:3:5+1234567::280:99999999+N+20250329+20260329++TOUCH123'",
		},
		{
			name:      "version 6 national with touchdown",
			version:   6,
			account:   testAccount(),
			touchdown: &touchdown,
			want:      "HKKAZ:3:6+1234567::280:99999999+N+20250329+20260329++TOUCH123'",
		},
		{
			name:      "version 7 international with touchdown",
			version:   7,
			account:   testAccount(),
			touchdown: &touchdown,
			want:      "HKKAZ:3:7+DE00999999990000000001:TESTDEFFXXX+N+20250329+20260329++TOUCH123'",
		},
		{
			name:    "version 6 with sub-account",
			version: 6,
			account: withSubAccount,
			want:    "HKKAZ:3:6+1234567:0001:280:99999999+N+20250329+20260329'",
		},
		{
			name:    "version 6 escapes the account number",
			version: 6,
			account: escapedAccountNumber,
			want:    "HKKAZ:3:6+1234??567::280:99999999+N+20250329+20260329'",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := segmentWire(t, NewHKKAZ(testCase.version, testCase.account, start, end, testCase.touchdown), 3)
			if got != testCase.want {
				t.Fatalf("wire = %q\nwant %q", got, testCase.want)
			}
		})
	}
}

func TestHKCAZVersion1UsesKTI(t *testing.T) {
	start := Date{Year: 2025, Month: 1, Day: 2}
	end := Date{Year: 2025, Month: 1, Day: 31}
	got := segmentWire(t, NewHKCAZ(
		1,
		testAccount(),
		[]string{camtFormat052},
		false,
		start,
		end,
		nil,
		nil,
	), 3)
	want := "HKCAZ:3:1+DE00999999990000000001:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" +
		"+N+20250102+20250131'"
	if got != want {
		t.Fatalf("wire = %q\nwant %q", got, want)
	}
}

func TestHKCAZWireFormat(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	maxEntries := 100
	got := segmentWire(t, NewHKCAZ(
		7,
		testAccount(),
		[]string{camtFormat052},
		true,
		start,
		end,
		&maxEntries,
		nil,
	), 3)
	want := "HKCAZ:3:7+DE00999999990000000001:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" +
		"+J+20250329+20260329+100'"
	if got != want {
		t.Fatalf("wire = %q\nwant %q", got, want)
	}
}

func TestHKCAZWithoutOptionalFields(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	account := testAccount()
	account.IBAN = testIBAN2
	got := segmentWire(t, NewHKCAZ(
		6,
		account,
		[]string{camtFormat053},
		false,
		start,
		end,
		nil,
		nil,
	), 3)
	want := "HKCAZ:3:6+DE00999999990000000002:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.053.001.02" +
		"+N+20250329+20260329'"
	if got != want {
		t.Fatalf("wire = %q\nwant %q", got, want)
	}
}

func TestHKCAZAcceptsSeveralFormats(t *testing.T) {
	start := Date{Year: 2025, Month: 1, Day: 2}
	end := Date{Year: 2025, Month: 1, Day: 31}
	got := segmentWire(t, NewHKCAZ(
		7, testAccount(),
		[]string{camtFormat052, camtFormat053},
		false, start, end, nil, nil,
	), 3)
	want := "HKCAZ:3:7+DE00999999990000000001:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" + ":" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.053.001.02" +
		"+N+20250102+20250131'"
	if got != want {
		t.Fatalf("wire = %q\nwant %q", got, want)
	}
}

func TestHKCAZWithTouchdown(t *testing.T) {
	start := Date{Year: 2025, Month: 1, Day: 2}
	end := Date{Year: 2025, Month: 1, Day: 31}
	touchdown := "TOUCH123"
	got := segmentWire(t, NewHKCAZ(
		7, testAccount(),
		[]string{camtFormat052},
		true, start, end, nil, &touchdown,
	), 3)
	want := "HKCAZ:3:7+DE00999999990000000001:TESTDEFFXXX+" +
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02" +
		"+J+20250102+20250131++TOUCH123'"
	if got != want {
		t.Fatalf("wire = %q\nwant %q", got, want)
	}
}

func TestHKCAZRejectsBadArguments(t *testing.T) {
	start := Date{Year: 2025, Month: 1, Day: 2}
	end := Date{Year: 2025, Month: 1, Day: 31}
	formats := []string{camtFormat052}
	negative := -1
	noIBAN := testAccount()
	noIBAN.IBAN = ""
	noBIC := testAccount()
	noBIC.BIC = ""

	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}

	if err := encode(NewHKCAZ(7, noIBAN, formats, true, start, end, nil, nil)); !errors.Is(err, errEmptyIBAN) {
		t.Fatalf("empty IBAN error = %v", err)
	}
	if err := encode(NewHKCAZ(7, noBIC, formats, true, start, end, nil, nil)); !errors.Is(err, errEmptyBIC) {
		t.Fatalf("empty BIC error = %v", err)
	}
	if err := encode(NewHKCAZ(7, testAccount(), formats, true, Date{}, end, nil, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid date error = %v", err)
	}
	if err := encode(NewHKCAZ(7, testAccount(), formats, true, start, end, &negative, nil)); !errors.Is(err, errInvalidMaxEntries) {
		t.Fatalf("negative max entries error = %v", err)
	}
}

func TestSecurityHeadersUseTheProfileVersion(t *testing.T) {
	now := fixedSource().now()
	cases := []struct {
		securityFunction string
		want             string
	}{
		{SecurityFunctionPINOnly, "1"},
		{"904", "2"},
		{"921", "2"},
		{"942", "2"},
	}
	for _, testCase := range cases {
		t.Run(testCase.securityFunction, func(t *testing.T) {
			hnvsk := parseOne(t, segmentWire(t,
				newHNVSK(testCase.securityFunction, testBLZ, testUserID, testSystemID, now), 998))
			if got := hnvsk.DEG(1).Text(1); got != testCase.want {
				t.Fatalf("HNVSK profile = %q, want %q", got, testCase.want)
			}
			hnshk := parseOne(t, segmentWire(t,
				newHNSHK(testCase.securityFunction, 1_234_567, testBLZ, testUserID, testSystemID, now), 2))
			if got := hnshk.DEG(1).Text(1); got != testCase.want {
				t.Fatalf("HNSHK profile = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSecurityHeadersEncodeAnEmptySystemIDAsZero(t *testing.T) {
	now := fixedSource().now()
	cases := []struct {
		name     string
		systemID string
		want     string
	}{
		{"empty", "", "0"},
		{"real", testSystemID, testSystemID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hnvsk := parseOne(t, segmentWire(t,
				newHNVSK("942", testBLZ, testUserID, testCase.systemID, now), 998))
			assertSecurityIdentification(t, hnvsk.DEG(4), testCase.want)
			hnshk := parseOne(t, segmentWire(t,
				newHNSHK("942", 1_234_567, testBLZ, testUserID, testCase.systemID, now), 2))
			assertSecurityIdentification(t, hnshk.DEG(6), testCase.want)
		})
	}
}

func assertSecurityIdentification(t *testing.T, group DEG, wantSystemID string) {
	t.Helper()
	if group.Len() != 3 {
		t.Fatalf("security identification has %d elements, want 3", group.Len())
	}
	if got := group.Text(0); got != identifiedRoleMessageSender {
		t.Fatalf("identified role = %q, want %q", got, identifiedRoleMessageSender)
	}
	if got := group.Text(2); got != wantSystemID {
		t.Fatalf("system id = %q, want %q", got, wantSystemID)
	}
}

func TestHKIDNWireFormat(t *testing.T) {
	got := segmentWire(t, NewHKIDN(testBLZ, testCustomerID, testSystemID), 3)
	if got != "HKIDN:3:2+280:99999999+test-customer+test-system+1'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKVVBWireFormat(t *testing.T) {
	want := "HKVVB:3:3+0+0+1+TESTPRODUCT0000000000000+1.0'"
	if got := segmentWire(t, NewHKVVB(0, 0, testProductID, "1.0"), 3); got != want {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKVVBProductFieldsAreParameters(t *testing.T) {
	first := segmentWire(t, NewHKVVB(1, 2, "PRODUCT-A", "9.9"), 3)
	second := segmentWire(t, NewHKVVB(1, 2, "PRODUCT-B", "1.0"), 3)
	if first == second {
		t.Fatal("different product ids produced the same HKVVB wire")
	}
}

func TestHKSYNWireFormat(t *testing.T) {
	if got := segmentWire(t, NewHKSYN(), 3); got != "HKSYN:3:3+0'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKENDWireFormat(t *testing.T) {
	if got := segmentWire(t, NewHKEND(testDialogID), 3); got != "HKEND:3:1+test-dialog'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKTABWireFormat(t *testing.T) {
	if got := segmentWire(t, NewHKTAB(5), 3); got != "HKTAB:3:5+A'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKSPAWireFormat(t *testing.T) {
	if got := segmentWire(t, NewHKSPA(1), 3); got != "HKSPA:3:1'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKTANWireLayoutPerVersion(t *testing.T) {
	const taskReference = "task-123"
	medium := "Handy"

	cases := []struct {
		name    string
		version uint16
		segment Segment
		want    string
	}{
		{
			name:    "process 4 v5 without medium",
			version: 5,
			// v5: 1 TAN process=4, 2 segment type=HKIDN; fields 3-12 absent.
			segment: NewHKTANProcess4(5, TanRefHKIDN, nil),
			want:    "HKTAN:3:5+4+HKIDN'",
		},
		{
			name:    "process 4 v6 without medium",
			version: 6,
			// v6: 1 TAN process=4, 2 segment type=HKIDN; fields 3-12 absent.
			segment: NewHKTANProcess4(6, TanRefHKIDN, nil),
			want:    "HKTAN:3:6+4+HKIDN'",
		},
		{
			name:    "process 4 v7 without medium",
			version: 7,
			// v7: 1 TAN process=4, 2 segment type=HKIDN; fields 3-12 absent.
			segment: NewHKTANProcess4(7, TanRefHKIDN, nil),
			want:    "HKTAN:3:7+4+HKIDN'",
		},
		{
			name:    "process 4 v5 with medium",
			version: 5,
			// v5: 1 TAN process=4, 2 segment type=HKSAL, 3-11 empty,
			// 12 TAN medium name=Handy.
			segment: NewHKTANProcess4(5, TanRefHKSAL, &medium),
			want:    "HKTAN:3:5+4+HKSAL++++++++++Handy'",
		},
		{
			name:    "process 4 v6 with medium",
			version: 6,
			// v6: 1 TAN process=4, 2 segment type=HKIDN, 3-10 empty,
			// 11 TAN medium name=Handy; 12 response HHD_UC absent.
			segment: NewHKTANProcess4(6, TanRefHKIDN, &medium),
			want:    "HKTAN:3:6+4+HKIDN+++++++++Handy'",
		},
		{
			name:    "process 4 v7 with medium",
			version: 7,
			// v7: 1 TAN process=4, 2 segment type=HKIDN, 3-10 empty,
			// 11 TAN medium name=Handy; 12 response HHD_UC absent.
			segment: NewHKTANProcess4(7, TanRefHKIDN, &medium),
			want:    "HKTAN:3:7+4+HKIDN+++++++++Handy'",
		},
		{
			name:    "process 2 v5 without medium",
			version: 5,
			// v5: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 empty, 7 further TAN follows=N; fields 8-12 absent.
			segment: NewHKTANProcess2(5, taskReference, nil),
			want:    "HKTAN:3:5+2++++task-123++N'",
		},
		{
			name:    "process 2 v6 without medium",
			version: 6,
			// v6: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N; fields 7-12 absent.
			segment: NewHKTANProcess2(6, taskReference, nil),
			want:    "HKTAN:3:6+2++++task-123+N'",
		},
		{
			name:    "process 2 v7 without medium",
			version: 7,
			// v7: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N; fields 7-12 absent.
			segment: NewHKTANProcess2(7, taskReference, nil),
			want:    "HKTAN:3:7+2++++task-123+N'",
		},
		{
			name:    "process 2 v5 with medium",
			version: 5,
			// v5: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 empty, 7 further TAN follows=N, 8-11 empty,
			// 12 TAN medium name=Handy.
			segment: NewHKTANProcess2(5, taskReference, &medium),
			want:    "HKTAN:3:5+2++++task-123++N+++++Handy'",
		},
		{
			name:    "process 2 v6 with medium",
			version: 6,
			// v6: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N, 7-10 empty, 11 TAN medium name=Handy;
			// 12 response HHD_UC absent.
			segment: NewHKTANProcess2(6, taskReference, &medium),
			want:    "HKTAN:3:6+2++++task-123+N+++++Handy'",
		},
		{
			name:    "process 2 v7 with medium",
			version: 7,
			// v7: 1 TAN process=2, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N, 7-10 empty, 11 TAN medium name=Handy;
			// 12 response HHD_UC absent.
			segment: NewHKTANProcess2(7, taskReference, &medium),
			want:    "HKTAN:3:7+2++++task-123+N+++++Handy'",
		},
		{
			name:    "process S v5 without medium",
			version: 5,
			// v5: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 empty, 7 further TAN follows=N; fields 8-12 absent.
			segment: NewHKTANProcessS(5, taskReference, nil),
			want:    "HKTAN:3:5+S++++task-123++N'",
		},
		{
			name:    "process S v6 without medium",
			version: 6,
			// v6: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N; fields 7-12 absent.
			segment: NewHKTANProcessS(6, taskReference, nil),
			want:    "HKTAN:3:6+S++++task-123+N'",
		},
		{
			name:    "process S v7 without medium",
			version: 7,
			// v7: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N; fields 7-12 absent.
			segment: NewHKTANProcessS(7, taskReference, nil),
			want:    "HKTAN:3:7+S++++task-123+N'",
		},
		{
			name:    "process S v5 with medium",
			version: 5,
			// v5: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 empty, 7 further TAN follows=N, 8-11 empty,
			// 12 TAN medium name=Handy.
			segment: NewHKTANProcessS(5, taskReference, &medium),
			want:    "HKTAN:3:5+S++++task-123++N+++++Handy'",
		},
		{
			name:    "process S v6 with medium",
			version: 6,
			// v6: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N, 7-10 empty, 11 TAN medium name=Handy;
			// 12 response HHD_UC absent.
			segment: NewHKTANProcessS(6, taskReference, &medium),
			want:    "HKTAN:3:6+S++++task-123+N+++++Handy'",
		},
		{
			name:    "process S v7 with medium",
			version: 7,
			// v7: 1 TAN process=S, 2-4 empty, 5 task reference=task-123,
			// 6 further TAN follows=N, 7-10 empty, 11 TAN medium name=Handy;
			// 12 response HHD_UC absent.
			segment: NewHKTANProcessS(7, taskReference, &medium),
			want:    "HKTAN:3:7+S++++task-123+N+++++Handy'",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := segmentWire(t, testCase.segment, 3)
			if got != testCase.want {
				t.Fatalf("version %d wire = %q\nwant %q", testCase.version, got, testCase.want)
			}
		})
	}
}

func TestHKTANProcess4RejectsUnknownReference(t *testing.T) {
	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}
	if err := encode(NewHKTANProcess4(7, TanSegmentRef("HKCCS"), nil)); !errors.Is(err, errUnknownTanReference) {
		t.Fatalf("order reference error = %v, want errUnknownTanReference", err)
	}
	if err := encode(NewHKTANProcess4(7, TanSegmentRef(""), nil)); !errors.Is(err, errUnknownTanReference) {
		t.Fatalf("empty reference error = %v, want errUnknownTanReference", err)
	}
}

// Process S (decoupled status poll) has process 2's layout with "S", as in
// fints-rs's hktan_process_s.
func TestHKTANProcessSEscapesTaskReference(t *testing.T) {
	// A task reference is escaped like every other caller value.
	if got := segmentWire(t, NewHKTANProcessS(7, "a+b:c'd", nil), 3); got != "HKTAN:3:7+S++++a?+b?:c?'d+N'" {
		t.Fatalf("escaped task reference wire = %q", got)
	}
}

func TestHKSALRejectsEmptyAccountIdentifiers(t *testing.T) {
	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}

	noIBAN := testAccount()
	noIBAN.IBAN = ""
	noBIC := testAccount()
	noBIC.BIC = ""
	noAccountNumber := testAccount()
	noAccountNumber.AccountNumber = ""
	noBLZ := testAccount()
	noBLZ.BLZ = ""

	emptyIBAN := encode(NewHKSAL(7, noIBAN, nil))
	if !errors.Is(emptyIBAN, errEmptyIBAN) {
		t.Fatalf("empty IBAN error = %v", emptyIBAN)
	}
	emptyBIC := encode(NewHKSAL(7, noBIC, nil))
	if !errors.Is(emptyBIC, errEmptyBIC) {
		t.Fatalf("empty BIC error = %v", emptyBIC)
	}
	emptyAccountNumber := encode(NewHKSAL(6, noAccountNumber, nil))
	if !errors.Is(emptyAccountNumber, errEmptyAccountNumber) {
		t.Fatalf("empty account number error = %v", emptyAccountNumber)
	}
	emptyBLZ := encode(NewHKSAL(6, noBLZ, nil))
	if !errors.Is(emptyBLZ, errEmptyBLZ) {
		t.Fatalf("empty BLZ error = %v", emptyBLZ)
	}

	for _, err := range []error{emptyIBAN, emptyBIC, emptyAccountNumber, emptyBLZ} {
		for _, forbidden := range []string{testIBAN, "1234567"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("error %q leaked %q", err, forbidden)
			}
		}
	}
}

func TestHKKAZRejectsEmptyAccountIdentifiers(t *testing.T) {
	valid := Date{Year: 2025, Month: 3, Day: 29}
	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}

	noAccountNumber := testAccount()
	noAccountNumber.AccountNumber = ""
	noBLZ := testAccount()
	noBLZ.BLZ = ""
	noBIC := testAccount()
	noBIC.BIC = ""

	if err := encode(NewHKKAZ(6, noAccountNumber, valid, valid, nil)); !errors.Is(err, errEmptyAccountNumber) {
		t.Fatalf("empty account number error = %v", err)
	}
	if err := encode(NewHKKAZ(6, noBLZ, valid, valid, nil)); !errors.Is(err, errEmptyBLZ) {
		t.Fatalf("empty BLZ error = %v", err)
	}
	if err := encode(NewHKKAZ(7, noBIC, valid, valid, nil)); !errors.Is(err, errEmptyBIC) {
		t.Fatalf("empty BIC error = %v", err)
	}
}

func TestHKKAZRejectsBadDates(t *testing.T) {
	valid := Date{Year: 2025, Month: 3, Day: 29}
	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}
	if err := encode(NewHKKAZ(7, testAccount(), Date{}, valid, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid start date error = %v", err)
	}
	if err := encode(NewHKKAZ(7, testAccount(), valid, Date{Year: 2025, Month: 13, Day: 1}, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid end date error = %v", err)
	}
}
