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

func TestSegmentFormattingShowsNoContent(t *testing.T) {
	segment := NewHKSAL(7, testIBAN, testBIC, nil)
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		got := fmt.Sprintf(format, segment)
		if !strings.Contains(got, "HKSAL") {
			t.Fatalf("%s = %q, want the segment type", format, got)
		}
		for _, forbidden := range []string{testIBAN, testBIC} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s of Segment leaked %q: %q", format, forbidden, got)
			}
		}
	}
}

func TestKTIProducesTwoDataElements(t *testing.T) {
	segment := NewHKSAL(7, testIBAN, testBIC, nil)
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
	wire := segmentWire(t, NewHKSAL(7, testIBAN, testBIC, nil), 3)
	if wire != "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N'" {
		t.Fatalf("HKSAL v7 wire = %q", wire)
	}
}

func TestHKSALVersion7WireFormat(t *testing.T) {
	got := segmentWire(t, NewHKSAL(7, testIBAN, testBIC, nil), 3)
	if got != "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKSALVersion5WireFormat(t *testing.T) {
	got := segmentWire(t, NewHKSAL(5, testIBAN, testBIC, nil), 3)
	if got != "HKSAL:3:5+DE00999999990000000001::280:TESTDEFFXXX+N'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKSALWithTouchdown(t *testing.T) {
	touchdown := "TOUCH123"
	got := segmentWire(t, NewHKSAL(7, testIBAN, testBIC, &touchdown), 3)
	if got != "HKSAL:3:7+DE00999999990000000001:TESTDEFFXXX+N++TOUCH123'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKKAZVersion7WireFormat(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	got := segmentWire(t, NewHKKAZ(7, testIBAN, testBIC, start, end, nil), 3)
	if got != "HKKAZ:3:7+DE00999999990000000001:TESTDEFFXXX+N+20250329+20260329'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKKAZVersion5WireFormat(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	got := segmentWire(t, NewHKKAZ(5, testIBAN, testBIC, start, end, nil), 3)
	if got != "HKKAZ:3:5+DE00999999990000000001::280:TESTDEFFXXX+N+20250329+20260329'" {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKKAZWithTouchdown(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	touchdown := "TOUCH123"
	got := segmentWire(t, NewHKKAZ(7, testIBAN, testBIC, start, end, &touchdown), 3)
	want := "HKKAZ:3:7+DE00999999990000000001:TESTDEFFXXX+N+20250329+20260329++TOUCH123'"
	if got != want {
		t.Fatalf("wire = %q", got)
	}
}

func TestHKCAZWireFormat(t *testing.T) {
	start := Date{Year: 2025, Month: 3, Day: 29}
	end := Date{Year: 2026, Month: 3, Day: 29}
	maxEntries := 100
	got := segmentWire(t, NewHKCAZ(
		7,
		testIBAN,
		testBIC,
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
	got := segmentWire(t, NewHKCAZ(
		6,
		testIBAN2,
		testBIC,
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
		7, testIBAN, testBIC,
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
		7, testIBAN, testBIC,
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

	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}

	if err := encode(NewHKCAZ(7, "", testBIC, formats, true, start, end, nil, nil)); !errors.Is(err, errEmptyIBAN) {
		t.Fatalf("empty IBAN error = %v", err)
	}
	if err := encode(NewHKCAZ(7, testIBAN, "", formats, true, start, end, nil, nil)); !errors.Is(err, errEmptyBIC) {
		t.Fatalf("empty BIC error = %v", err)
	}
	if err := encode(NewHKCAZ(7, testIBAN, testBIC, formats, true, Date{}, end, nil, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid date error = %v", err)
	}
	if err := encode(NewHKCAZ(7, testIBAN, testBIC, formats, true, start, end, &negative, nil)); !errors.Is(err, errInvalidMaxEntries) {
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
	if err := encode(NewHKSAL(7, "", testBIC, nil)); !errors.Is(err, errEmptyIBAN) {
		t.Fatalf("empty IBAN error = %v", err)
	}
	if err := encode(NewHKSAL(7, testIBAN, "", nil)); !errors.Is(err, errEmptyBIC) {
		t.Fatalf("empty BIC error = %v", err)
	}
}

func TestHKKAZRejectsBadDates(t *testing.T) {
	valid := Date{Year: 2025, Month: 3, Day: 29}
	encode := func(segment Segment) error {
		_, err := appendOutgoingSegment(nil, segment, 3)
		return err
	}
	if err := encode(NewHKKAZ(7, testIBAN, testBIC, Date{}, valid, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid start date error = %v", err)
	}
	if err := encode(NewHKKAZ(7, testIBAN, testBIC, valid, Date{Year: 2025, Month: 13, Day: 1}, nil)); !errors.Is(err, errInvalidDate) {
		t.Fatalf("invalid end date error = %v", err)
	}
}
