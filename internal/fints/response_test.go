package fints

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseHIRMGReturnCode(t *testing.T) {
	response, err := ParseResponse([]byte("HIRMG:3:2+0010::Nachricht entgegengenommen.'"))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(response.GlobalCodes) != 1 {
		t.Fatalf("global codes = %d, want 1", len(response.GlobalCodes))
	}
	if len(response.SegmentCodes) != 0 {
		t.Fatalf("segment codes = %d, want 0", len(response.SegmentCodes))
	}
	code := response.GlobalCodes[0]
	if code.Code != "0010" {
		t.Fatalf("Code = %q", code.Code)
	}
	if code.RefElement != "" {
		t.Fatalf("RefElement = %q, want empty", code.RefElement)
	}
	if code.Text() != "Nachricht entgegengenommen." {
		t.Fatalf("Text = %q", code.Text())
	}
	if !code.IsSuccess() || code.IsWarning() || code.IsError() {
		t.Fatal("0010 is not classified as a success")
	}
	if len(code.Params) != 0 {
		t.Fatalf("Params = %v", code.Params)
	}
}

func TestParseHIRMSWithReferenceAndParameters(t *testing.T) {
	input := "HIRMS:4:2:3+3050:3:Hinweis:parameter-one:parameter-two+0010::OK'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(response.SegmentCodes) != 2 {
		t.Fatalf("segment codes = %d, want 2", len(response.SegmentCodes))
	}
	if len(response.GlobalCodes) != 0 {
		t.Fatalf("global codes = %d, want 0", len(response.GlobalCodes))
	}

	first := response.SegmentCodes[0]
	if first.Code != "3050" {
		t.Fatalf("Code = %q", first.Code)
	}
	if first.RefElement != "3" {
		t.Fatalf("RefElement = %q, want 3", first.RefElement)
	}
	if first.Text() != "Hinweis" {
		t.Fatalf("Text = %q", first.Text())
	}
	if len(first.Params) != 2 || first.Params[0] != "parameter-one" || first.Params[1] != "parameter-two" {
		t.Fatalf("Params = %v", first.Params)
	}
	if !first.IsWarning() || first.IsError() || first.IsSuccess() {
		t.Fatal("3050 is not classified as a warning")
	}

	second := response.SegmentCodes[1]
	if second.Code != "0010" || !second.IsSuccess() {
		t.Fatalf("second code = %q", second.Code)
	}
}

func TestReturnCodeClassification(t *testing.T) {
	cases := []struct {
		code    string
		success bool
		warning bool
		failure bool
	}{
		{code: "0010", success: true},
		{code: "0020", success: true},
		{code: "0030", success: true},
		{code: "0100", success: true},
		{code: "0900", success: true},
		{code: "0999", success: true},
		{code: "3040", warning: true},
		{code: "3060", warning: true},
		{code: "3076", warning: true},
		{code: "3920", warning: true},
		{code: "3955", warning: true},
		{code: "3956", warning: true},
		{code: "3999", warning: true},
		{code: "9010", failure: true},
		{code: "9040", failure: true},
		{code: "9050", failure: true},
		{code: "9110", failure: true},
		{code: "9160", failure: true},
		{code: "9340", failure: true},
		{code: "9800", failure: true},
		{code: "9942", failure: true},
		{code: "9999", failure: true},
		{code: "1234"},
		{code: "XXXX"},
		{code: ""},
	}

	for _, testCase := range cases {
		code := ReturnCode{Code: testCase.code}
		if code.IsSuccess() != testCase.success {
			t.Fatalf("%q IsSuccess = %v, want %v", testCase.code, code.IsSuccess(), testCase.success)
		}
		if code.IsWarning() != testCase.warning {
			t.Fatalf("%q IsWarning = %v, want %v", testCase.code, code.IsWarning(), testCase.warning)
		}
		if code.IsError() != testCase.failure {
			t.Fatalf("%q IsError = %v, want %v", testCase.code, code.IsError(), testCase.failure)
		}
	}
}

func TestReturnCodeFormattingShowsOnlyTheCode(t *testing.T) {
	const bankText = "Konto DE00999999990000000001 gesperrt"
	code := ReturnCode{Code: "9942", RefElement: "DE00999999990000000001", text: bankText}

	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		got := fmt.Sprintf(format, code)
		if !strings.Contains(got, "9942") {
			t.Fatalf("%s = %q, want the code", format, got)
		}
		for _, forbidden := range []string{bankText, "DE00999999990000000001"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s leaked bank data %q: %q", format, forbidden, got)
			}
		}
	}
	if !strings.Contains(code.Text(), bankText) {
		t.Fatal("Text() does not return the bank text")
	}
	if !code.IsError() {
		t.Fatal("9942 is not an error")
	}
}

func TestResponseReadOnlyHelpers(t *testing.T) {
	input := strings.Join([]string{
		"HNHBK:1:3+000000000400+300+test-dialog+1'",
		"HISYN:2:4:1+test-system'",
		"HIBPA:3:3+42+280+0+1+1+0'",
		"HIUPA:4:4+test-customer+7+55'",
		"HITANS:5:6:1+1+1+0'",
		"HIPINS:6:1:4+1+1+0+HKSAL:N+HKKAZ:N'",
		"HIUPD:7:6:4+1::280:99999999+DE00999999990000000001+test-customer++EUR+Test Name++Girokonto'",
		"HIRMG:8:2+0010::OK'",
	}, "")
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	if got := response.DialogID(); got != testDialogID {
		t.Fatalf("DialogID = %q", got)
	}
	if got := response.SystemID(); got != testSystemID {
		t.Fatalf("SystemID = %q", got)
	}
	if got := response.BPDVersion(); got != testBPDVersion {
		t.Fatalf("BPDVersion = %d", got)
	}
	if got := response.UPDVersion(); got != testUPDVersion {
		t.Fatalf("UPDVersion = %d", got)
	}

	if got := len(response.HITANS()); got != 1 {
		t.Fatalf("HITANS segments = %d", got)
	}
	if got := len(response.HIPINS()); got != 1 {
		t.Fatalf("HIPINS segments = %d", got)
	}
	if got := len(response.HIUPD()); got != 1 {
		t.Fatalf("HIUPD segments = %d", got)
	}
	if got := len(response.HIBPA()); got != 1 {
		t.Fatalf("HIBPA segments = %d", got)
	}

	if got := response.HIUPD()[0].DEG(2).Text(0); got != testIBAN {
		t.Fatalf("HIUPD IBAN = %q", got)
	}
	if got := response.HIPINS()[0].DEG(4).Text(0); got != "HKSAL" {
		t.Fatalf("HIPINS operation = %q", got)
	}
	if got := response.HITANS()[0].Version(); got != 6 {
		t.Fatalf("HITANS version = %d", got)
	}

	// The standalone helpers ported from response.rs read the same values.
	if got := DialogIDFromHNHBK(mustFind(t, response, "HNHBK")); got != testDialogID {
		t.Fatalf("DialogIDFromHNHBK = %q", got)
	}
	if got := SystemIDFromHISYN(mustFind(t, response, "HISYN")); got != testSystemID {
		t.Fatalf("SystemIDFromHISYN = %q", got)
	}
	if got := BPDVersionFromHIBPA(mustFind(t, response, "HIBPA")); got != testBPDVersion {
		t.Fatalf("BPDVersionFromHIBPA = %d", got)
	}
	if got := UPDVersionFromHIUPA(mustFind(t, response, "HIUPA")); got != testUPDVersion {
		t.Fatalf("UPDVersionFromHIUPA = %d", got)
	}
}

func mustFind(t *testing.T, response *Response, typ string) ParsedSegment {
	t.Helper()
	segment, ok := response.findSegment(typ)
	if !ok {
		t.Fatalf("no %s segment", typ)
	}
	return segment
}

func TestParseResponseFlattensHNVSD(t *testing.T) {
	inner := "HIRMG:2:2+0010::OK'HNHBS:3:1+1'"
	outer := fmt.Sprintf("HNHBK:1:3+000000000075+300+test-dialog+1'HNVSD:999:1+@%d@%s'", len(inner), inner)

	response, err := ParseResponse([]byte(outer))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	types := make([]string, 0, len(response.Segments))
	for _, segment := range response.Segments {
		types = append(types, segment.Type())
	}
	want := "HNHBK,HIRMG,HNHBS"
	if strings.Join(types, ",") != want {
		t.Fatalf("segment types = %v, want %s", types, want)
	}
	if len(response.GlobalCodes) != 1 || response.GlobalCodes[0].Code != "0010" {
		t.Fatalf("global codes = %v", response.GlobalCodes)
	}
}

func TestParseResponseRejectsDeeplyNestedHNVSD(t *testing.T) {
	payload := "HIRMG:1:2+0010::OK'"
	for depth := 0; depth < maxNestingDepth+1; depth++ {
		payload = fmt.Sprintf("HNVSD:999:1+@%d@%s'", len(payload), payload)
	}
	_, err := ParseResponse([]byte(payload))
	if err == nil {
		t.Fatal("ParseResponse accepted excessive HNVSD nesting")
	}
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonNestingTooDeep {
		t.Fatalf("error = %v, want reasonNestingTooDeep", err)
	}
}

func TestParseResponseRejectsHNVSDWithoutBinaryPayload(t *testing.T) {
	_, err := ParseResponse([]byte("HNVSD:999:1'"))
	if err == nil {
		t.Fatal("ParseResponse accepted HNVSD without a payload")
	}
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != reasonMissingHNVSDData {
		t.Fatalf("error = %v, want reasonMissingHNVSDData", err)
	}
	if parseError.Type != "HNVSD" {
		t.Fatalf("error segment type = %q", parseError.Type)
	}
}

func TestParseResponseReportsMalformedInnerPayload(t *testing.T) {
	inner := "HNHBK:1:3+"
	outer := fmt.Sprintf("HNVSD:999:1+@%d@%s'", len(inner), inner)
	if _, err := ParseResponse([]byte(outer)); err == nil {
		t.Fatal("ParseResponse accepted a truncated inner message")
	}
}

func TestResponseFormattingShowsNoBankData(t *testing.T) {
	input := "HNHBK:1:3+000000000200+300+test-dialog+1'" +
		"HIUPD:2:6:4+1::280:99999999+DE00999999990000000001+test-customer++EUR+Test Name'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, response)
		for _, forbidden := range []string{testIBAN, "Test Name", testDialogID} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s leaked %q: %q", format, forbidden, got)
			}
		}
	}
	segment := mustFind(t, response, "HIUPD")
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, segment)
		if strings.Contains(got, testIBAN) {
			t.Fatalf("%s of ParsedSegment leaked %q", format, testIBAN)
		}
	}
}
