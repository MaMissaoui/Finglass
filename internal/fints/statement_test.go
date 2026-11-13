package fints

import (
	"bytes"
	"fmt"
	"testing"
)

func binaryStatementMessage(segmentType string, number, version uint16, booked, pending []byte) string {
	message := fmt.Sprintf("%s:%d:%d:4+@%d@%s", segmentType, number, version, len(booked), booked)
	if pending != nil {
		message += fmt.Sprintf("+@%d@%s", len(pending), pending)
	}
	return message + "'"
}

// camtStatementMessage builds a spec-shaped HICAZ v1: account, descriptor URN,
// the booked documents in one group, then the optional pending document.
func camtStatementMessage(number uint16, booked [][]byte, pending []byte) string {
	message := fmt.Sprintf("HICAZ:%d:1:3+DE00999999990000000001:TESTDEFFXXX+"+
		"urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02+", number)
	for index, document := range booked {
		if index > 0 {
			message += ":"
		}
		message += fmt.Sprintf("@%d@%s", len(document), document)
	}
	if pending != nil {
		message += fmt.Sprintf("+@%d@%s", len(pending), pending)
	}
	return message + "'"
}

func TestExtractMT940KeepsBinaryPayloads(t *testing.T) {
	booked := []byte{0xE4, 0xC3, 0xA4, '\'', '+', ':', '@', '?', 0x00, 0xFF}
	pending := []byte(`pending-'+:@?`)
	message := binaryStatementMessage("HIKAZ", 1, 7, booked, pending)

	segments, err := ParseMessage([]byte(message))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	statement := ExtractMT940(segments)
	if !bytes.Equal(statement.Booked, booked) {
		t.Fatalf("Booked = %v, want %v", statement.Booked, booked)
	}
	if !bytes.Equal(statement.Pending, pending) {
		t.Fatalf("Pending = %v, want %v", statement.Pending, pending)
	}
}

func TestExtractMT940BookedWithoutPending(t *testing.T) {
	booked := []byte{0xE4, ':', '+', '\''}
	segments, err := ParseMessage([]byte(binaryStatementMessage("HIKAZ", 2, 7, booked, nil)))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	statement := ExtractMT940(segments)
	if !bytes.Equal(statement.Booked, booked) {
		t.Fatalf("Booked = %v, want %v", statement.Booked, booked)
	}
	if len(statement.Pending) != 0 {
		t.Fatalf("Pending = %v, want empty", statement.Pending)
	}
}

func TestExtractMT940ConcatenatesSegments(t *testing.T) {
	segments, err := ParseMessage([]byte("HIKAZ:1:7:4+@3@AAA'HIKAZ:2:7:4+@3@BBB'"))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	statement := ExtractMT940(segments)
	if got := string(statement.Booked); got != "AAABBB" {
		t.Fatalf("Booked = %q", got)
	}
}

func TestExtractCamtKeepsDocumentsSeparate(t *testing.T) {
	bookedOne := append([]byte(`<?xml version="1.0"?><Document>`), 0xE4, 0xC3, 0xA4)
	bookedOne = append(bookedOne, []byte(`'+:@?'`)...)
	bookedTwo := []byte(`<?xml version="1.0"?><Document><Stmt>two</Stmt></Document>`)
	pending := append([]byte(`<Document>pending`), 0xC3, 0xA4)
	pending = append(pending, []byte(`:'+?@</Document>`)...)

	// Two booked documents in one HICAZ group, plus a second HICAZ page.
	bookedThree := []byte(`<Document><Stmt>three</Stmt></Document>`)
	message := camtStatementMessage(1, [][]byte{bookedOne, bookedTwo}, pending) +
		camtStatementMessage(2, [][]byte{bookedThree}, nil)
	segments, err := ParseMessage([]byte(message))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}

	statement := ExtractCamt(segments)
	if len(statement.Booked) != 3 {
		t.Fatalf("Booked documents = %d, want 3", len(statement.Booked))
	}
	if !bytes.Equal(statement.Booked[2], bookedThree) {
		t.Fatalf("Booked[2] = %v, want %v", statement.Booked[2], bookedThree)
	}
	if !bytes.Equal(statement.Booked[0], bookedOne) {
		t.Fatalf("Booked[0] = %v, want %v", statement.Booked[0], bookedOne)
	}
	if !bytes.Equal(statement.Booked[1], bookedTwo) {
		t.Fatalf("Booked[1] = %v, want %v", statement.Booked[1], bookedTwo)
	}
	if len(statement.Pending) != 1 || !bytes.Equal(statement.Pending[0], pending) {
		t.Fatalf("Pending = %v, want %v", statement.Pending, pending)
	}
}

func TestExtractCamtWithoutPending(t *testing.T) {
	booked := []byte(`<Document>booked</Document>`)
	segments, err := ParseMessage([]byte(camtStatementMessage(3, [][]byte{booked}, nil)))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	statement := ExtractCamt(segments)
	if len(statement.Booked) != 1 || !bytes.Equal(statement.Booked[0], booked) {
		t.Fatalf("Booked = %v, want %v", statement.Booked, booked)
	}
	if len(statement.Pending) != 0 {
		t.Fatalf("Pending = %v, want empty", statement.Pending)
	}
}

func TestExtractStatementsIgnoreOtherSegments(t *testing.T) {
	segments, err := ParseMessage([]byte("HISAL:1:7:4+C:1,00:EUR:20260315'"))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if statement := ExtractMT940(segments); len(statement.Booked) != 0 || len(statement.Pending) != 0 {
		t.Fatalf("MT940 = %#v", statement)
	}
	if statement := ExtractCamt(segments); len(statement.Booked) != 0 || len(statement.Pending) != 0 {
		t.Fatalf("Camt = %#v", statement)
	}
}

func TestResponseStatements(t *testing.T) {
	message := binaryStatementMessage("HIKAZ", 1, 7, []byte("mt940-booked"), []byte("mt940-pending")) +
		camtStatementMessage(2, [][]byte{[]byte("<Document>camt</Document>")}, nil)
	response, err := ParseResponse([]byte(message))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if got := string(response.MT940().Booked); got != "mt940-booked" {
		t.Fatalf("MT940 booked = %q", got)
	}
	if got := string(response.MT940().Pending); got != "mt940-pending" {
		t.Fatalf("MT940 pending = %q", got)
	}
	if camt := response.Camt(); len(camt.Booked) != 1 || string(camt.Booked[0]) != "<Document>camt</Document>" {
		t.Fatalf("Camt = %#v", camt)
	}
}

// The account group and the descriptor are never returned as statement data,
// and a HICAZ without a camt descriptor yields nothing.
func TestExtractCamtNeverReturnsTheAccountOrDescriptor(t *testing.T) {
	segments, err := ParseMessage([]byte(camtStatementMessage(1, [][]byte{[]byte("<Document/>")}, nil)))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	statement := ExtractCamt(segments)
	if len(statement.Booked) != 1 || string(statement.Booked[0]) != "<Document/>" {
		t.Fatalf("Booked = %q, want exactly the document", statement.Booked)
	}

	segments, err = ParseMessage([]byte("HICAZ:1:1:3+DE00999999990000000001:TESTDEFFXXX+@3@AAA+@3@BBB'"))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if statement := ExtractCamt(segments); len(statement.Booked) != 0 || len(statement.Pending) != 0 {
		t.Fatalf("a HICAZ without a descriptor returned %d booked and %d pending documents",
			len(statement.Booked), len(statement.Pending))
	}
}
