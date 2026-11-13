package fints

import (
	"errors"
	"fmt"
	"strconv"
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

func TestHKTANProcess4WithoutMedium(t *testing.T) {
	for _, version := range []uint16{5, 6, 7} {
		got := segmentWire(t, NewHKTANProcess4(version, TanRefHKIDN, nil), 3)
		want := "HKTAN:3:" + strconv.FormatUint(uint64(version), 10) + "+4+HKIDN'"
		if got != want {
			t.Fatalf("version %d wire = %q, want %q", version, got, want)
		}
	}
}

func TestHKTANProcess4WithMedium(t *testing.T) {
	medium := "Handy"
	if got := segmentWire(t, NewHKTANProcess4(7, TanRefHKIDN, &medium), 3); got != "HKTAN:3:7+4+HKIDN+++++++++Handy'" {
		t.Fatalf("version 7 wire = %q", got)
	}
	if got := segmentWire(t, NewHKTANProcess4(5, TanRefHKSAL, &medium), 3); got != "HKTAN:3:5+4+HKSAL+Handy'" {
		t.Fatalf("version 5 wire = %q", got)
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

func TestHKTANProcess2WireFormat(t *testing.T) {
	const taskReference = "task-123"

	if got := segmentWire(t, NewHKTANProcess2(7, taskReference, nil), 3); got != "HKTAN:3:7+2++++task-123+N'" {
		t.Fatalf("version 7 wire = %q", got)
	}
	if got := segmentWire(t, NewHKTANProcess2(5, taskReference, nil), 3); got != "HKTAN:3:5+2+++task-123+N'" {
		t.Fatalf("version 5 wire = %q", got)
	}

	medium := "Handy"
	if got := segmentWire(t, NewHKTANProcess2(7, taskReference, &medium), 3); got != "HKTAN:3:7+2++++task-123+N+++++Handy'" {
		t.Fatalf("version 7 with medium wire = %q", got)
	}
}

// Process S (decoupled status poll) has process 2's layout with "S", as in
// fints-rs's hktan_process_s.
func TestHKTANProcessSWireFormat(t *testing.T) {
	const taskReference = "task-123"

	if got := segmentWire(t, NewHKTANProcessS(7, taskReference, nil), 3); got != "HKTAN:3:7+S++++task-123+N'" {
		t.Fatalf("version 7 wire = %q", got)
	}
	if got := segmentWire(t, NewHKTANProcessS(5, taskReference, nil), 3); got != "HKTAN:3:5+S+++task-123+N'" {
		t.Fatalf("version 5 wire = %q", got)
	}

	medium := "Handy"
	if got := segmentWire(t, NewHKTANProcessS(7, taskReference, &medium), 3); got != "HKTAN:3:7+S++++task-123+N+++++Handy'" {
		t.Fatalf("version 7 with medium wire = %q", got)
	}

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
