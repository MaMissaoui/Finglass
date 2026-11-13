package fints

import (
	"errors"
	"strings"
	"testing"
)

func TestParserRejectsEmptyInput(t *testing.T) {
	_, err := ParseMessage(nil)
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonEmptyMessage {
		t.Fatalf("error = %v, want reasonEmptyMessage", err)
	}
}

func TestParserRejectsWhitespaceOnlyInput(t *testing.T) {
	_, err := ParseMessage([]byte(" \r\n "))
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonNoSegments {
		t.Fatalf("error = %v, want reasonNoSegments", err)
	}
}

func TestParserRejectsTruncatedInput(t *testing.T) {
	for _, input := range []string{
		"HNHBS:5:1+2",
		"HNHBS:5:1+2'TEST:1:1+abc",
		"HNHBS",
	} {
		if _, err := ParseMessage([]byte(input)); err == nil {
			t.Fatalf("ParseMessage(%q) accepted a truncated message", input)
		}
	}
}

func TestParserRejectsBinaryPastTheEnd(t *testing.T) {
	_, err := ParseMessage([]byte("TEST:1:1+@99999@abc'"))
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonBinaryPastEnd {
		t.Fatalf("error = %v, want reasonBinaryPastEnd", err)
	}
}

func TestParserRejectsHugeDeclaredLength(t *testing.T) {
	for _, input := range []string{
		"TEST:1:1+@999999999999999999999999@'",
		"TEST:1:1+@-1@'",
		"TEST:1:1+@1x@'",
		"TEST:1:1+@@'",
	} {
		_, err := ParseMessage([]byte(input))
		if err == nil {
			t.Fatalf("ParseMessage(%q) accepted a bad length prefix", input)
		}
		var parseError *ParseError
		if !errors.As(err, &parseError) || parseError.Reason != reasonInvalidBinLength {
			t.Fatalf("ParseMessage(%q) error = %v, want reasonInvalidBinLength", input, err)
		}
	}
}

func TestParserRejectsUnterminatedEscape(t *testing.T) {
	_, err := ParseMessage([]byte("TEST:1:1+abc?"))
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonUnterminatedEsc {
		t.Fatalf("error = %v, want reasonUnterminatedEsc", err)
	}
}

func TestParserRejectsUnterminatedBinaryPrefix(t *testing.T) {
	_, err := ParseMessage([]byte("TEST:1:1+@12'"))
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonUnterminatedBin {
		t.Fatalf("error = %v, want reasonUnterminatedBin", err)
	}
}

func TestParserRejectsMessagesOverTheSizeCap(t *testing.T) {
	oversized := make([]byte, MaxMessageSize+1)
	for index := range oversized {
		oversized[index] = 'a'
	}
	_, err := ParseMessage(oversized)
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonTooLarge {
		t.Fatalf("error = %v, want reasonTooLarge", err)
	}
}

func TestParseErrorCarriesSegmentAndOffsetWithoutContent(t *testing.T) {
	input := "HKIDN:3:2+280:99999999+" + testIBAN + "+?"
	_, err := ParseMessage([]byte(input))
	if err == nil {
		t.Fatal("ParseMessage accepted an unterminated escape")
	}
	var parseError *ParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("error is %T, want *ParseError", err)
	}
	if parseError.Type != "HKIDN" {
		t.Fatalf("segment type = %q", parseError.Type)
	}
	if parseError.Number != 3 {
		t.Fatalf("segment number = %d", parseError.Number)
	}
	if parseError.Offset != len(input) {
		t.Fatalf("offset = %d, want %d", parseError.Offset, len(input))
	}
	if strings.Contains(err.Error(), testIBAN) {
		t.Fatalf("error contains element content: %v", err)
	}
}

func TestParseErrorDoesNotLeakTheFirstElement(t *testing.T) {
	// Without a header the whole input is the first data element. It must not
	// become the error's "segment type".
	_, err := ParseMessage([]byte(testIBAN))
	if err == nil {
		t.Fatal("ParseMessage accepted a message without a terminator")
	}
	if strings.Contains(err.Error(), testIBAN) {
		t.Fatalf("error leaks the first element: %v", err)
	}
	var parseError *ParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("error is %T", err)
	}
	if parseError.Type != "" {
		t.Fatalf("error segment type = %q, want empty", parseError.Type)
	}
}

func TestParserNeverPanicsOnHostileInput(t *testing.T) {
	inputs := [][]byte{
		[]byte("'"),
		[]byte("+"),
		[]byte("@@@'"),
		[]byte(":'"),
		[]byte("????'"),
		[]byte("@1@'"),
		[]byte("@0@'"),
		[]byte(":::+++'''"),
		[]byte("\x00\xff\xfe'"),
		{0x00},
		{0xff, 0xfe, 0xfd},
		[]byte(strings.Repeat("+", 1000) + "'"),
		[]byte(strings.Repeat("?", 1000)),
	}
	for _, input := range inputs {
		_, _ = ParseMessage(input)
		_, _ = ParseResponse(input)
	}
}

func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("HNHBS:5:1+2'"),
		[]byte("HIRMG:3:2+0010::Nachricht entgegengenommen.'HNHBS:4:1+1'"),
		[]byte("TEST:1:1+Hello?+World?:Test?'End'"),
		[]byte("TEST:1:1+@5@HELLO+next'"),
		[]byte("HIRMS:4:2:3+0010::Nachricht entgegengenommen.'"),
		[]byte("TEST:1:1+A:::B'"),
		[]byte(""),
		[]byte("HNHBS:5:1+2"),
		[]byte("TEST:1:1+@99999@abc'"),
		[]byte("TEST:1:1+abc?"),
		[]byte("TEST:1:1+@999999999999999999@'"),
		[]byte("TEST:1:1+@12'"),
		{0x00, 0xff, '?'},
		[]byte("HITANS:1:7:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J:962:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:Challenge:3:1:J:1:0:N:0:2:N:1:00:1'"),
		[]byte("HIPINS:6:1:4+1+1+0+HKSAL:N+HKKAZ:N+HKSPA:J+HKTAN:N'"),
		[]byte("HICAZS:2:1:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02'"),
		[]byte("HIUPD:7:6:4+1::280:99999999+DE00999999990000000001+test-customer+G+EUR+Erika Musterfrau++Girokonto+HKSAL:1+HKKAZ:1'"),
		[]byte("HISPA:2:4:4+J:DE00999999990000000001:TESTDEFFXXX:00000001::280:99999999'"),
		[]byte("HITAN:3:6:2+4+task-ref-1+order-hash+challenge-text+@3@ABC+20260315'"),
		[]byte("HITAB:6:6:4+A:1:My Phone+1:1:Second Phone'"),
		[]byte("HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'"),
		[]byte("HIKAZ:1:7:4+@3@AAA+@3@BBB'"),
		[]byte("HICAZ:1:7:4+@3@AAA+@3@BBB'"),
		[]byte("HICAZ:1:1:3+DE00999999990000000001:TESTDEFFXXX+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02+@3@AAA:@3@BBB+@3@CCC'"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		segments, err := ParseMessage(data)
		if err == nil && len(segments) == 0 {
			t.Fatal("a successful parse returned no segment")
		}
		if err == nil {
			for _, segment := range segments {
				if _, encodeErr := appendSegmentBytes(nil, segment.degs); encodeErr != nil {
					t.Fatalf("a parsed segment does not re-encode: %v", encodeErr)
				}
			}
		}
		_, _ = ParseResponse(data)
	})
}
