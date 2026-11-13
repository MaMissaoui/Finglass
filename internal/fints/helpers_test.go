package fints

import (
	"testing"
	"time"
)

// Invented fixtures only. No real bank code, IBAN, BIC, user or product id.
const (
	testBLZ        = "99999999"
	testIBAN       = "DE00999999990000000001"
	testIBAN2      = "DE00999999990000000002"
	testBIC        = "TESTDEFFXXX"
	testUserID     = "test-user"
	testCustomerID = "test-customer"
	testSystemID   = "test-system"
	testProductID  = "TESTPRODUCT0000000000000"
	testDialogID   = "test-dialog"
	testPIN        = "pin-4242-?'+:@"
	testTAN        = "tan-777"
)

const (
	testBPDVersion = 42
	testUPDVersion = 55
)

func fixedSource() Source {
	return Source{
		Now: func() time.Time {
			return time.Date(2026, 3, 29, 12, 34, 56, 0, time.UTC)
		},
		SecurityReference: func() (uint32, error) {
			return 1_234_567, nil
		},
	}
}

func segmentWire(t *testing.T, segment Segment, number uint16) string {
	t.Helper()
	data, err := appendOutgoingSegment(nil, segment, number)
	if err != nil {
		t.Fatalf("encode segment: %v", err)
	}
	return string(data)
}

func parsedWire(t *testing.T, segment ParsedSegment) string {
	t.Helper()
	data, err := appendSegmentBytes(nil, segment.degs)
	if err != nil {
		t.Fatalf("reserialize segment: %v", err)
	}
	return string(data)
}

func parseOne(t *testing.T, input string) ParsedSegment {
	t.Helper()
	segments, err := ParseMessage([]byte(input))
	if err != nil {
		t.Fatalf("ParseMessage(%q): %v", input, err)
	}
	if len(segments) != 1 {
		t.Fatalf("ParseMessage(%q) returned %d segments, want 1", input, len(segments))
	}
	return segments[0]
}

func escapedForFixture(t *testing.T, input string) string {
	t.Helper()
	encoded, err := appendEscapedText(nil, input)
	if err != nil {
		t.Fatalf("appendEscapedText(%q): %v", input, err)
	}
	return string(encoded)
}
