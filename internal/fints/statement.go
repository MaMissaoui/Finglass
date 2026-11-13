// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "fmt"

// MT940Statement holds the raw MT940 data from HIKAZ.
//
// The bytes are exactly the bank's binary values. They are never decoded or
// re-encoded here; parsing MT940 into bookings is a later slice.
type MT940Statement struct {
	// Booked is the concatenated booked MT940 data, in wire order.
	Booked []byte
	// Pending is the concatenated pending MT940 data, in wire order.
	Pending []byte
}

// String returns only byte counts, never the statement content.
func (s MT940Statement) String() string {
	return fmt.Sprintf("fints.MT940Statement{booked:%d pending:%d}", len(s.Booked), len(s.Pending))
}

// GoString returns only byte counts, never the statement content.
func (s MT940Statement) GoString() string {
	return s.String()
}

// Format writes only byte counts, never the statement content, for every fmt
// verb.
func (s MT940Statement) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

// ExtractMT940 ports fints-rs's extract_mt940_data. It reads HIKAZ's booked
// binary value from DEG 1 and its pending value from DEG 2, unchanged, and
// concatenates them across all HIKAZ segments.
func ExtractMT940(segments []ParsedSegment) MT940Statement {
	var statement MT940Statement
	for _, segment := range segments {
		if segment.Type() != "HIKAZ" {
			continue
		}
		statement.Booked = append(statement.Booked, segment.DEG(1).Binary(0)...)
		statement.Pending = append(statement.Pending, segment.DEG(2).Binary(0)...)
	}
	return statement
}

// ExtractMT940Data is the fints-rs-named form of [ExtractMT940].
func ExtractMT940Data(segments []ParsedSegment) MT940Statement {
	return ExtractMT940(segments)
}

// MT940 returns the raw MT940 data from this response's HIKAZ segments.
func (r Response) MT940() MT940Statement {
	return ExtractMT940(r.Segments)
}

// CamtStatement holds the raw camt XML documents from HICAZ.
//
// Each document is one binary value, unchanged. Parsing camt into bookings is a
// later slice.
type CamtStatement struct {
	// Booked is the booked camt documents, in wire order.
	Booked [][]byte
	// Pending is the pending or not-booked camt documents, in wire order.
	Pending [][]byte
}

// String returns only document counts, never the XML content.
func (s CamtStatement) String() string {
	return fmt.Sprintf("fints.CamtStatement{booked:%d pending:%d}", len(s.Booked), len(s.Pending))
}

// GoString returns only document counts, never the XML content.
func (s CamtStatement) GoString() string {
	return s.String()
}

// Format writes only document counts, never the XML content, for every fmt
// verb.
func (s CamtStatement) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

// ExtractCamt reads the raw camt documents from every HICAZ segment, unchanged
// and in wire order.
//
// HICAZ (FinTS 3.0, Messages, "Kontoumsätze/Zeitraum camt", version 1) is:
// the account (KTI), the camt descriptor URN, then one group with the booked
// documents (1 to 99 binary elements separated by ':'), then the optional
// not-booked document. The groups are located after the descriptor, so the
// account group is never read as statement data. fints-rs has no HICAZ; this
// layout comes from the FinTS specification and is unverified against a real
// bank until the probe.
func ExtractCamt(segments []ParsedSegment) CamtStatement {
	var statement CamtStatement
	for _, segment := range segments {
		if segment.Type() != "HICAZ" {
			continue
		}
		descriptor := -1
		for index := 1; index < segment.DEGCount(); index++ {
			if isCamtURN(segment.DEG(index).Text(0)) {
				descriptor = index
				break
			}
		}
		if descriptor < 0 {
			continue
		}
		statement.Booked = appendBinaries(statement.Booked, segment.DEG(descriptor+1))
		statement.Pending = appendBinaries(statement.Pending, segment.DEG(descriptor+2))
	}
	return statement
}

// appendBinaries appends a copy of every non-empty binary element of group.
func appendBinaries(documents [][]byte, group DEG) [][]byte {
	for index := 0; index < group.Len(); index++ {
		if data := group.Binary(index); len(data) > 0 {
			documents = append(documents, append([]byte(nil), data...))
		}
	}
	return documents
}

// ExtractCamtData is the camt form of [ExtractCamt].
func ExtractCamtData(segments []ParsedSegment) CamtStatement {
	return ExtractCamt(segments)
}

// Camt returns the raw camt documents from this response's HICAZ segments.
func (r Response) Camt() CamtStatement {
	return ExtractCamt(r.Segments)
}
