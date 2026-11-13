package fints

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Serializer parity: src/serializer.rs tests.
func TestEscapeText(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no specials", input: "Hello", want: "Hello"},
		{name: "all specials", input: "A+B:C'D@E?F", want: "A?+B?:C?'D?@E??F"},
		{name: "extended latin1", input: "café résumé", want: "caf\xe9 r\xe9sum\xe9"},
		{name: "umlauts", input: "Ä Ö Ü ä ö ü ß", want: "\xc4 \xd6 \xdc \xe4 \xf6 \xfc \xdf"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := appendEscapedText(nil, testCase.input)
			if err != nil {
				t.Fatalf("appendEscapedText: %v", err)
			}
			if string(got) != testCase.want {
				t.Fatalf("appendEscapedText(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestEscapeTextRejectsCharactersOutsideISO88591(t *testing.T) {
	for _, input := range []string{"Hello 🌍", "Ω", "€"} {
		if _, err := appendEscapedText(nil, input); !errors.Is(err, errTextNotLatin1) {
			t.Fatalf("appendEscapedText(%q) error = %v, want errTextNotLatin1", input, err)
		}
	}
}

func TestSerializeBinary(t *testing.T) {
	if got := string(appendBinary(nil, []byte("HI"))); got != "@2@HI" {
		t.Fatalf("appendBinary = %q, want %q", got, "@2@HI")
	}
}

func TestSerializeDEGStripsTrailingEmptyElements(t *testing.T) {
	group := DEG{
		textElement("A"),
		textElement("B"),
		emptyElement(),
		emptyElement(),
	}
	got, err := appendDEG(nil, group)
	if err != nil {
		t.Fatalf("appendDEG: %v", err)
	}
	if string(got) != "A:B" {
		t.Fatalf("appendDEG = %q, want %q", got, "A:B")
	}
}

func TestSerializeDEGPreservesMiddleEmptyElements(t *testing.T) {
	group := DEG{textElement("A"), emptyElement(), textElement("C")}
	got, err := appendDEG(nil, group)
	if err != nil {
		t.Fatalf("appendDEG: %v", err)
	}
	if string(got) != "A::C" {
		t.Fatalf("appendDEG = %q, want %q", got, "A::C")
	}
}

func TestSerializeSegment(t *testing.T) {
	groups := []DEG{
		segmentHeader("HNHBS", 5, 1),
		{textElement("2")},
	}
	got, err := appendSegmentBytes(nil, groups)
	if err != nil {
		t.Fatalf("appendSegmentBytes: %v", err)
	}
	if string(got) != "HNHBS:5:1+2'" {
		t.Fatalf("appendSegmentBytes = %q, want %q", got, "HNHBS:5:1+2'")
	}
}

func TestEscapeTextNeverProducesASeparator(t *testing.T) {
	const input = "a?b'c+d:e@f"
	escaped, err := appendEscapedText(nil, input)
	if err != nil {
		t.Fatalf("appendEscapedText: %v", err)
	}
	for _, separator := range []byte{'\'', '+', ':'} {
		unescaped := 0
		for index, character := range escaped {
			if character == separator && (index == 0 || escaped[index-1] != escapeCharacter) {
				unescaped++
			}
		}
		if unescaped != 0 {
			t.Fatalf("escaped %q contains an unescaped %q", escaped, separator)
		}
	}
}

func TestTextValueRoundTripsThroughASegment(t *testing.T) {
	values := []string{
		"plain",
		"test-user",
		"a?b'c+d:e@f",
		"? ' + : @",
		"ä ö ü ß",
		"",
	}

	for _, value := range values {
		segment := NewHKIDN(testBLZ, value, testSystemID)
		wire := segmentWire(t, segment, 3)
		parsed := parseOne(t, wire)
		if got := parsed.DEG(2).Text(0); got != value {
			t.Fatalf("value %q round-tripped as %q (wire %q)", value, got, wire)
		}
	}
}

// Parser parity: src/parser.rs tests and tests/parser_additional_test.rs.
func TestParseSimpleSegment(t *testing.T) {
	segment := parseOne(t, "HNHBS:5:1+2'")
	if segment.Type() != "HNHBS" {
		t.Fatalf("Type = %q", segment.Type())
	}
	if segment.Number() != 5 {
		t.Fatalf("Number = %d", segment.Number())
	}
	if segment.Version() != 1 {
		t.Fatalf("Version = %d", segment.Version())
	}
	if got := segment.DEG(1).Text(0); got != "2" {
		t.Fatalf("DEG(1) = %q", got)
	}
}

func TestParseMultipleSegments(t *testing.T) {
	segments, err := ParseMessage([]byte("HIRMG:3:2+0010::Nachricht entgegengenommen.'HNHBS:4:1+1'"))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments", len(segments))
	}
	if segments[0].Type() != "HIRMG" || segments[1].Type() != "HNHBS" {
		t.Fatalf("types = %q, %q", segments[0].Type(), segments[1].Type())
	}
}

func TestParseEmptyDataElements(t *testing.T) {
	segment := parseOne(t, "HKTAN:5:7+4+HKIDN+++++++'")
	if segment.Type() != "HKTAN" {
		t.Fatalf("Type = %q", segment.Type())
	}
	if got := segment.DEG(1).Text(0); got != "4" {
		t.Fatalf("DEG(1) = %q", got)
	}
	if got := segment.DEG(2).Text(0); got != "HKIDN" {
		t.Fatalf("DEG(2) = %q", got)
	}
}

func TestParseDEGWithColons(t *testing.T) {
	segment := parseOne(t, "HNSHK:2:4+PIN:1+999'")
	if got := segment.DEG(1).Text(0); got != "PIN" {
		t.Fatalf("DEG(1) safety profile = %q", got)
	}
	if got := segment.DEG(1).Text(1); got != "1" {
		t.Fatalf("DEG(1) version = %q", got)
	}
	if got := segment.DEG(2).Text(0); got != "999" {
		t.Fatalf("DEG(2) = %q", got)
	}
}

func TestParseEscapedCharacters(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+Hello?+World?:Test?'End'")
	if got := segment.DEG(1).Text(0); got != "Hello+World:Test'End" {
		t.Fatalf("DEG(1) = %q", got)
	}
}

func TestParseDoubleEscape(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+A??B'")
	if got := segment.DEG(1).Text(0); got != "A?B" {
		t.Fatalf("DEG(1) = %q, want A?B", got)
	}
}

func TestParseBinaryData(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+@5@HELLO+next'")
	element := segment.DEG(1).At(0)
	if !element.IsBinary() {
		t.Fatal("first element is not binary")
	}
	if got := string(element.Binary()); got != "HELLO" {
		t.Fatalf("binary = %q", got)
	}
	if got := segment.DEG(2).Text(0); got != "next" {
		t.Fatalf("DEG(2) = %q", got)
	}
}

func TestParseZeroLengthBinary(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+@0@'")
	element := segment.DEG(1).At(0)
	if !element.IsBinary() {
		t.Fatal("zero-length value is not binary")
	}
	if len(element.Binary()) != 0 {
		t.Fatalf("zero-length value has %d bytes", len(element.Binary()))
	}
}

func TestParseSegmentHeaderWithReference(t *testing.T) {
	segment := parseOne(t, "HIRMS:4:2:3+0010::Nachricht entgegengenommen.'")
	if segment.Type() != "HIRMS" || segment.Number() != 4 || segment.Version() != 2 {
		t.Fatalf("header = %s:%d:%d", segment.Type(), segment.Number(), segment.Version())
	}
	reference, ok := segment.Reference()
	if !ok || reference != 3 {
		t.Fatalf("Reference = %d, %v; want 3, true", reference, ok)
	}
}

func TestParseSingleEmptyDEG(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+'")
	if segment.DEGCount() != 2 {
		t.Fatalf("DEGCount = %d, want 2", segment.DEGCount())
	}
	if segment.DEG(1).Len() != 1 || !segment.DEG(1).At(0).IsEmpty() {
		t.Fatalf("DEG(1) = %v, want one empty element", segment.DEG(1))
	}
}

func TestParseMultipleEmptyDataElements(t *testing.T) {
	segment := parseOne(t, "TEST:1:1+A:::B'")
	group := segment.DEG(1)
	if group.Len() != 4 {
		t.Fatalf("DEG(1).Len = %d, want 4", group.Len())
	}
	expected := []string{"A", "", "", "B"}
	for index, want := range expected {
		if got := group.Text(index); got != want {
			t.Fatalf("DEG(1)[%d] = %q, want %q", index, got, want)
		}
	}
}

func TestParseISO88591Characters(t *testing.T) {
	data := append([]byte("TEST:1:1+"), 0xC4, 0xD6, 0xDC)
	data = append(data, '\'')
	segments, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if got := segments[0].DEG(1).Text(0); got != "ÄÖÜ" {
		t.Fatalf("DEG(1) = %q, want ÄÖÜ", got)
	}
}

func TestParseMessageWithNewlines(t *testing.T) {
	segments, err := ParseMessage([]byte("HNHBS:5:1+2'\r\nHNHBK:1:3+000000000075+300+0'"))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments", len(segments))
	}
	if segments[0].Type() != "HNHBS" || segments[1].Type() != "HNHBK" {
		t.Fatalf("types = %q, %q", segments[0].Type(), segments[1].Type())
	}
}

func TestParseThenSerializeRoundTrips(t *testing.T) {
	inputs := []string{
		"HNHBS:5:1+2'",
		"TEST:1:1+Hello?+World'",
		"TEST:1:1+Hello?+World+last'",
		"HNSHK:2:4+PIN:1+999+one:two:three'",
		"TEST:1:1+A??B?:C?+D?'E?@F'",
	}
	for _, input := range inputs {
		segments, err := ParseMessage([]byte(input))
		if err != nil {
			t.Fatalf("ParseMessage(%q): %v", input, err)
		}
		if got := parsedWire(t, segments[0]); got != input {
			t.Fatalf("round trip %q = %q", input, got)
		}
	}
}

func TestParseRoundTripMultipleSegments(t *testing.T) {
	// The second proptest in tests/parser_additional_test.rs.
	data := []byte("SEG1:1:1+alpha'\r\nSEG2:2:1+beta'")
	segments, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(segments))
	}
	if got := segments[0].DEG(1).Text(0); got != "alpha" {
		t.Fatalf("first value = %q", got)
	}
	if got := segments[1].DEG(1).Text(0); got != "beta" {
		t.Fatalf("second value = %q", got)
	}
}

func TestParseRoundTripArbitraryText(t *testing.T) {
	// The proptest in tests/parser_additional_test.rs, as a table.
	values := []string{
		"a",
		"Hello World",
		"20260329",
		"A+B",
		"A:B",
		"A'B",
		"A@B",
		"A?B",
	}
	for _, value := range values {
		segment := NewHKIDN(testBLZ, value, testSystemID)
		wire := segmentWire(t, segment, 3)
		parsed := parseOne(t, wire)
		if got := parsed.DEG(2).Text(0); got != value {
			t.Fatalf("value %q round-tripped as %q (wire %q)", value, got, wire)
		}
	}
}

func TestBinaryValueWithSeparatorsRoundTrips(t *testing.T) {
	payload := []byte("a'+\u0000?:@z")
	groups := []DEG{
		segmentHeader("HNVSD", 999, 1),
		{binaryElement(payload)},
	}
	wire, err := appendSegmentBytes(nil, groups)
	if err != nil {
		t.Fatalf("appendSegmentBytes: %v", err)
	}
	segments, err := ParseMessage(wire)
	if err != nil {
		t.Fatalf("ParseMessage(%q): %v", wire, err)
	}
	if got := segments[0].DEG(1).Binary(0); string(got) != string(payload) {
		t.Fatalf("binary = %q, want %q", got, payload)
	}
}

func TestCharsetEncodesLatin1Bytes(t *testing.T) {
	encoded, err := appendEscapedText(nil, "äöüß")
	if err != nil {
		t.Fatalf("appendEscapedText: %v", err)
	}
	want := []byte{0xE4, 0xF6, 0xFC, 0xDF}
	if len(encoded) != len(want) {
		t.Fatalf("encoded %v, want %v", encoded, want)
	}
	for index := range want {
		if encoded[index] != want[index] {
			t.Fatalf("encoded %v, want %v", encoded, want)
		}
	}

	segment := NewHKIDN(testBLZ, "ä ö ü ß", testSystemID)
	if got := parseOne(t, segmentWire(t, segment, 3)).DEG(2).Text(0); got != "ä ö ü ß" {
		t.Fatalf("umlauts round-tripped as %q", got)
	}
}

func TestACharacterOutsideLatin1IsAnError(t *testing.T) {
	if _, err := appendEscapedText(nil, "€"); !errors.Is(err, errTextNotLatin1) {
		t.Fatalf("euro error = %v, want errTextNotLatin1", err)
	}
	// The error must not echo the offending character.
	if strings.Contains(errTextNotLatin1.Error(), "€") {
		t.Fatal("the charset error contains the offending character")
	}
}

// Statements arrive as binary data (camt is UTF-8 XML, MT940 is whatever the
// bank sends). The parser must hand those bytes back unchanged, never run them
// through the ISO-8859-1 text decoding.
func TestBinaryPayloadBytesComeBackUnchanged(t *testing.T) {
	payload := []byte{'a', 0xE4, 'b', 0xC3, 0xA4, 'c', '\'', '+', ':', '?', '@'}
	input := append([]byte("TEST:1:1+@11@"), payload...)
	input = append(input, []byte("+next'")...)

	segments, err := ParseMessage(input)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	got := segments[0].DEG(1).At(0).Binary()
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary payload = % x, want % x", got, payload)
	}
	if text := segments[0].DEG(2).Text(0); text != "next" {
		t.Fatalf("the element after the binary payload = %q", text)
	}
}
