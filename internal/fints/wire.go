// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strconv"
)

// MaxMessageSize is the largest message [ParseMessage] and [ParseResponse]
// accept: 16 MiB. Anything larger is rejected before it is parsed.
const MaxMessageSize = 16 << 20

// maxNestingDepth is how often a HNVSD payload may contain another HNVSD
// payload before nesting is rejected.
const maxNestingDepth = 4

type elementKind uint8

const (
	elementEmpty elementKind = iota
	elementText
	elementBinary
)

// DataElement is one FinTS data element (a DE).
//
// A DE is either absent, text or binary. Text is decoded from ISO-8859-1 and
// binary keeps its raw bytes. The zero DataElement is absent.
type DataElement struct {
	kind   elementKind
	text   string
	binary []byte
}

func emptyElement() DataElement {
	return DataElement{}
}

func textElement(s string) DataElement {
	return DataElement{kind: elementText, text: s}
}

// valueElement mirrors fints-rs's de_text: an empty string is an absent DE.
func valueElement(s string) DataElement {
	if s == "" {
		return emptyElement()
	}
	return textElement(s)
}

func binaryElement(b []byte) DataElement {
	return DataElement{kind: elementBinary, binary: b}
}

// IsEmpty reports whether the element is absent.
func (d DataElement) IsEmpty() bool {
	return d.kind == elementEmpty
}

// IsBinary reports whether the element is binary data.
func (d DataElement) IsBinary() bool {
	return d.kind == elementBinary
}

// Text returns the element's text. It is empty for an absent or binary
// element; binary values are read with [DataElement.Binary].
func (d DataElement) Text() string {
	if d.kind != elementText {
		return ""
	}
	return d.text
}

// Binary returns the element's raw bytes, or nil when it is not binary.
func (d DataElement) Binary() []byte {
	if d.kind != elementBinary {
		return nil
	}
	return d.binary
}

// String returns a representation without the element's content.
func (d DataElement) String() string {
	return d.format()
}

// GoString returns a representation without the element's content.
func (d DataElement) GoString() string {
	return d.format()
}

// Format writes a representation without the element's content, for every fmt
// verb.
func (d DataElement) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(d.format()))
}

func (d DataElement) format() string {
	switch d.kind {
	case elementText:
		return "fints.DataElement{text:" + strconv.Itoa(len(d.text)) + "}"
	case elementBinary:
		return "fints.DataElement{binary:" + strconv.Itoa(len(d.binary)) + "}"
	default:
		return "fints.DataElement{empty}"
	}
}

// DEG is a FinTS data element group (a DEG): data elements separated by
// colons on the wire.
type DEG []DataElement

// Len returns the number of data elements in the group.
func (g DEG) Len() int {
	return len(g)
}

// At returns the data element at index. Out-of-range indexes return an absent
// element, as fints-rs does.
func (g DEG) At(index int) DataElement {
	if index < 0 || index >= len(g) {
		return emptyElement()
	}
	return g[index]
}

// Text returns the text of the data element at index.
func (g DEG) Text(index int) string {
	return g.At(index).Text()
}

// Binary returns the raw bytes of the data element at index, or nil.
func (g DEG) Binary(index int) []byte {
	return g.At(index).Binary()
}

// String returns a representation without the group's content.
func (g DEG) String() string {
	return "fints.DEG{" + strconv.Itoa(len(g)) + "}"
}

// GoString returns a representation without the group's content.
func (g DEG) GoString() string {
	return g.String()
}

// Format writes a representation without the group's content, for every fmt
// verb.
func (g DEG) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(g.String()))
}

func groupIsEmpty(g DEG) bool {
	if len(g) == 0 {
		return true
	}
	for _, element := range g {
		if !element.IsEmpty() {
			return false
		}
	}
	return true
}

// ParsedSegment is a segment read from an untrusted bank response.
//
// It is a different type from [Segment], so the encoder cannot be handed a
// parsed segment by accident. Its header fields are available through the
// accessors; the data elements are read through [ParsedSegment.DEG].
type ParsedSegment struct {
	degs         []DEG
	segmentType  string
	number       uint16
	version      uint16
	reference    uint16
	hasReference bool
	offset       int
}

func newParsedSegment(degs []DEG, offset int) ParsedSegment {
	segment := ParsedSegment{degs: degs, offset: offset}
	if len(degs) == 0 {
		return segment
	}
	header := degs[0]
	segment.segmentType = header.Text(0)
	segment.number = parseUint16(header.Text(1))
	segment.version = parseUint16(header.Text(2))
	if reference := header.Text(3); reference != "" {
		if value, ok := parseOptionalUint16(reference); ok {
			segment.reference = value
			segment.hasReference = true
		}
	}
	return segment
}

// Type returns the segment type, for example "HIRMS". It is empty for a
// malformed segment without a header.
func (s ParsedSegment) Type() string {
	return s.segmentType
}

// Number returns the segment number from the header, or 0 when it is missing
// or not a number.
func (s ParsedSegment) Number() uint16 {
	return s.number
}

// Version returns the segment version from the header, or 0 when it is missing
// or not a number.
func (s ParsedSegment) Version() uint16 {
	return s.version
}

// Reference returns the optional segment reference from the header. A
// response segment uses it to reference the request segment it answers.
func (s ParsedSegment) Reference() (uint16, bool) {
	return s.reference, s.hasReference
}

// DEGCount returns the number of data element groups, including the header
// group at index 0.
func (s ParsedSegment) DEGCount() int {
	return len(s.degs)
}

// DEG returns the data element group at index. Out-of-range indexes return an
// empty group.
func (s ParsedSegment) DEG(index int) DEG {
	if index < 0 || index >= len(s.degs) {
		return nil
	}
	return s.degs[index]
}

// String returns a representation with only the header, never the data
// elements.
func (s ParsedSegment) String() string {
	return s.format()
}

// GoString returns a representation with only the header.
func (s ParsedSegment) GoString() string {
	return s.format()
}

// Format writes a representation with only the header, for every fmt verb.
func (s ParsedSegment) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.format()))
}

func (s ParsedSegment) format() string {
	return fmt.Sprintf("fints.ParsedSegment{type:%s number:%d version:%d}", s.segmentType, s.number, s.version)
}

func parseUint16(s string) uint16 {
	if value, ok := parseOptionalUint16(s); ok {
		return value
	}
	return 0
}

func parseOptionalUint16(s string) (uint16, bool) {
	if s == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(value), true
}
