// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strconv"
)

// Response is a parsed bank response.
//
// Segments holds the outer segments plus the segments unpacked from every
// HNVSD payload, in wire order. GlobalCodes come from HIRMG, SegmentCodes from
// HIRMS.
type Response struct {
	// Segments are all parsed segments, with HNVSD payloads unpacked.
	Segments []ParsedSegment
	// GlobalCodes are the message-level return codes from HIRMG.
	GlobalCodes []ReturnCode
	// SegmentCodes are the segment-level return codes from HIRMS.
	SegmentCodes []ReturnCode
}

// ParseResponse parses an untrusted bank response.
//
// HNVSD payloads are unpacked recursively, up to a nesting depth of
// [maxNestingDepth]. HIRMG and HIRMS return codes are collected. The input is
// treated as hostile: a truncated message, a binary value past the end, a
// huge declared length or an over-long message is an error, never a panic.
func ParseResponse(data []byte) (*Response, error) {
	outer, err := parseMessage(data)
	if err != nil {
		return nil, err
	}
	segments, err := flattenResponses(outer, 0)
	if err != nil {
		return nil, err
	}

	response := &Response{Segments: segments}
	for _, segment := range segments {
		switch segment.Type() {
		case "HIRMG":
			response.GlobalCodes = append(response.GlobalCodes, parseReturnCodes(segment)...)
		case "HIRMS":
			response.SegmentCodes = append(response.SegmentCodes, parseReturnCodes(segment)...)
		}
	}
	return response, nil
}

func flattenResponses(segments []ParsedSegment, depth int) ([]ParsedSegment, error) {
	flattened := make([]ParsedSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.Type() != "HNVSD" {
			flattened = append(flattened, segment)
			continue
		}
		if depth >= maxNestingDepth {
			return nil, &ParseError{
				Type:   segment.Type(),
				Number: segment.Number(),
				Offset: segment.offset,
				Reason: reasonNestingTooDeep,
			}
		}
		payload := segment.DEG(1)
		if payload.Len() == 0 || !payload.At(0).IsBinary() {
			return nil, &ParseError{
				Type:   segment.Type(),
				Number: segment.Number(),
				Offset: segment.offset,
				Reason: reasonMissingHNVSDData,
			}
		}
		inner, err := parseMessage(payload.Binary(0))
		if err != nil {
			return nil, err
		}
		nested, err := flattenResponses(inner, depth+1)
		if err != nil {
			return nil, err
		}
		flattened = append(flattened, nested...)
	}
	return flattened, nil
}

// ReturnCode is one return code from HIRMG or HIRMS.
type ReturnCode struct {
	// Code is the four-digit return code.
	Code string
	// RefElement is the referenced data element, for example "3" or "HKIDN".
	// It is empty for a message-level code without a reference. The HIRMS
	// segment header's own segment reference is available through
	// [ParsedSegment.Reference].
	RefElement string
	// Params are the non-empty parameter values after the text.
	Params []string
	text   string
}

// Text returns the bank's message text for this code.
//
// The text can carry account numbers and other bank data. It must never be
// logged, audited or shown in an error; only show it to the user in a context
// where bank data is expected.
func (r ReturnCode) Text() string {
	return r.text
}

// IsSuccess reports whether the code is a success code (0xxx).
func (r ReturnCode) IsSuccess() bool {
	return hasCodePrefix(r.Code, '0')
}

// IsWarning reports whether the code is a warning code (3xxx).
func (r ReturnCode) IsWarning() bool {
	return hasCodePrefix(r.Code, '3')
}

// IsError reports whether the code is an error code (9xxx).
func (r ReturnCode) IsError() bool {
	return hasCodePrefix(r.Code, '9')
}

func hasCodePrefix(code string, prefix byte) bool {
	return len(code) > 0 && code[0] == prefix
}

// String returns only the code, never the bank's text or parameters.
func (r ReturnCode) String() string {
	return r.Code
}

// GoString returns only the code, never the bank's text or parameters.
func (r ReturnCode) GoString() string {
	return "fints.ReturnCode{Code:" + strconv.Quote(r.Code) + "}"
}

// Format writes only the code, never the bank's text or parameters, for every
// fmt verb.
func (r ReturnCode) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.Code))
}

func parseReturnCodes(segment ParsedSegment) []ReturnCode {
	codes := make([]ReturnCode, 0, segment.DEGCount())
	for index := 1; index < segment.DEGCount(); index++ {
		group := segment.DEG(index)
		code := group.Text(0)
		if code == "" {
			continue
		}
		entry := ReturnCode{
			Code:       code,
			RefElement: group.Text(1),
			text:       group.Text(2),
		}
		for parameter := 3; parameter < group.Len(); parameter++ {
			if value := group.At(parameter).Text(); value != "" {
				entry.Params = append(entry.Params, value)
			}
		}
		codes = append(codes, entry)
	}
	return codes
}

// String returns counts only, never segment content.
func (r Response) String() string {
	return fmt.Sprintf(
		"fints.Response{segments:%d globalCodes:%d segmentCodes:%d}",
		len(r.Segments), len(r.GlobalCodes), len(r.SegmentCodes),
	)
}

// GoString returns counts only.
func (r Response) GoString() string {
	return r.String()
}

// Format writes counts only, for every fmt verb.
func (r Response) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

// findSegments returns every segment of the given type, in wire order.
func (r Response) findSegments(typ string) []ParsedSegment {
	matches := make([]ParsedSegment, 0, 1)
	for _, segment := range r.Segments {
		if segment.Type() == typ {
			matches = append(matches, segment)
		}
	}
	return matches
}

// findSegment returns the first segment of the given type.
func (r Response) findSegment(typ string) (ParsedSegment, bool) {
	for _, segment := range r.Segments {
		if segment.Type() == typ {
			return segment, true
		}
	}
	return ParsedSegment{}, false
}

// HITANS returns the raw HITANS segments (TAN method parameters).
func (r Response) HITANS() []ParsedSegment {
	return r.findSegments("HITANS")
}

// HIPINS returns the raw HIPINS segments (which operations require a PIN).
func (r Response) HIPINS() []ParsedSegment {
	return r.findSegments("HIPINS")
}

// HIUPD returns the raw HIUPD segments (user parameter data).
func (r Response) HIUPD() []ParsedSegment {
	return r.findSegments("HIUPD")
}

// HIBPA returns the raw HIBPA segments (bank parameter data header).
func (r Response) HIBPA() []ParsedSegment {
	return r.findSegments("HIBPA")
}

// DialogID returns the dialog id from the first HNHBK segment, or "".
func (r Response) DialogID() string {
	if segment, ok := r.findSegment("HNHBK"); ok {
		return DialogIDFromHNHBK(segment)
	}
	return ""
}

// SystemID returns the system id from the first HISYN segment, or "".
func (r Response) SystemID() string {
	if segment, ok := r.findSegment("HISYN"); ok {
		return SystemIDFromHISYN(segment)
	}
	return ""
}

// BPDVersion returns the BPD version from the first HIBPA segment, or 0.
func (r Response) BPDVersion() uint16 {
	if segment, ok := r.findSegment("HIBPA"); ok {
		return BPDVersionFromHIBPA(segment)
	}
	return 0
}

// UPDVersion returns the UPD version from the first HIUPA segment, or 0.
func (r Response) UPDVersion() uint16 {
	if segment, ok := r.findSegment("HIUPA"); ok {
		return UPDVersionFromHIUPA(segment)
	}
	return 0
}

// DialogIDFromHNHBK reads the dialog id from HNHBK's DEG 3.
func DialogIDFromHNHBK(segment ParsedSegment) string {
	return segment.DEG(3).Text(0)
}

// SystemIDFromHISYN reads the assigned system id from HISYN's DEG 1.
func SystemIDFromHISYN(segment ParsedSegment) string {
	return segment.DEG(1).Text(0)
}

// BPDVersionFromHIBPA reads the BPD version from HIBPA's DEG 1.
func BPDVersionFromHIBPA(segment ParsedSegment) uint16 {
	return parseUint16(segment.DEG(1).Text(0))
}

// UPDVersionFromHIUPA reads the UPD version from HIUPA's DEG 3.
func UPDVersionFromHIUPA(segment ParsedSegment) uint16 {
	return parseUint16(segment.DEG(3).Text(0))
}
