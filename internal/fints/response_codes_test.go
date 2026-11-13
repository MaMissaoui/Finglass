package fints

import (
	"reflect"
	"testing"
)

func TestReturnCodeKindAllKnownCodes(t *testing.T) {
	cases := []struct {
		code string
		kind ReturnCodeKind
	}{
		{"0010", ReturnCodeKindMessageReceived},
		{"0020", ReturnCodeKindOrderExecuted},
		{"0030", ReturnCodeKindTanRequired},
		{"0100", ReturnCodeKindDialogEnded},
		{"0900", ReturnCodeKindTanValid},
		{"3040", ReturnCodeKindTouchdown},
		{"3060", ReturnCodeKindPartialWarnings},
		{"3076", ReturnCodeKindScaExemption},
		{"3920", ReturnCodeKindAllowedSecurityFunctions},
		{"3955", ReturnCodeKindDecoupledInitiated},
		{"3956", ReturnCodeKindDecoupledPending},
		{"9010", ReturnCodeKindGeneralError},
		{"9040", ReturnCodeKindAuthenticationMissing},
		{"9050", ReturnCodeKindPartialErrors},
		{"9110", ReturnCodeKindUnexpectedInSync},
		{"9160", ReturnCodeKindDataElementMissing},
		{"9340", ReturnCodeKindPinWrong},
		{"9800", ReturnCodeKindDialogAborted},
		{"9942", ReturnCodeKindAccountLocked},
	}
	for _, testCase := range cases {
		if got := (ReturnCode{Code: testCase.code}).Kind(); got != testCase.kind {
			t.Errorf("%s Kind = %v, want %v", testCase.code, got, testCase.kind)
		}
	}
}

func TestReturnCodeKindFallbacks(t *testing.T) {
	cases := []struct {
		code string
		kind ReturnCodeKind
	}{
		{"0999", ReturnCodeKindOtherSuccess},
		{"3999", ReturnCodeKindOtherWarning},
		{"9999", ReturnCodeKindOtherError},
		{"1234", ReturnCodeKindUnknown},
		{"XXXX", ReturnCodeKindUnknown},
		{"", ReturnCodeKindUnknown},
	}
	for _, testCase := range cases {
		if got := (ReturnCode{Code: testCase.code}).Kind(); got != testCase.kind {
			t.Errorf("%q Kind = %v, want %v", testCase.code, got, testCase.kind)
		}
	}
}

func TestReturnCodeHelpers(t *testing.T) {
	touchdown := ReturnCode{Code: "3040", Params: []string{"TOUCH-1234", "ignored"}}
	if point, ok := touchdown.Touchdown(); !ok || point != "TOUCH-1234" {
		t.Fatalf("Touchdown = %q, %v", point, ok)
	}
	if _, ok := (ReturnCode{Code: "3040"}).Touchdown(); ok {
		t.Fatal("3040 without parameters returned a touchdown")
	}
	if _, ok := (ReturnCode{Code: "0010", Params: []string{"TOUCH-1234"}}).Touchdown(); ok {
		t.Fatal("non-3040 returned a touchdown")
	}

	allowed := ReturnCode{Code: "3920", Params: []string{"912", "940"}}
	if got := allowed.AllowedSecurityFunctions(); !reflect.DeepEqual(got, []string{"912", "940"}) {
		t.Fatalf("AllowedSecurityFunctions = %v", got)
	}
	got := allowed.AllowedSecurityFunctions()
	got[0] = "changed"
	if allowed.Params[0] != "912" {
		t.Fatal("AllowedSecurityFunctions did not return a copy")
	}
	if got := (ReturnCode{Code: "0010", Params: []string{"912"}}).AllowedSecurityFunctions(); got != nil {
		t.Fatalf("non-3920 AllowedSecurityFunctions = %v", got)
	}
}

func TestResponseReturnCodeHelpers(t *testing.T) {
	input := "HIRMG:1:2+0010::OK+3920::Zugelassene Verfahren:912:940'" +
		"HIRMS:2:2:1+9010::Allgemeiner Fehler+3040::Aufsetzpunkt:TOUCH-1234'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	codes := response.Codes()
	if len(codes) != 4 {
		t.Fatalf("Codes length = %d, want 4", len(codes))
	}
	wantCodes := []string{"0010", "3920", "9010", "3040"}
	for index, want := range wantCodes {
		if codes[index].Code != want {
			t.Fatalf("Codes[%d] = %q, want %q", index, codes[index].Code, want)
		}
	}

	firstError, ok := response.FirstError()
	if !ok || firstError.Code != "9010" {
		t.Fatalf("FirstError = %q, %v", firstError.Code, ok)
	}
	if point, ok := response.Touchdown(); !ok || point != "TOUCH-1234" {
		t.Fatalf("Touchdown = %q, %v", point, ok)
	}
	if got := response.AllowedSecurityFunctions(); !reflect.DeepEqual(got, []string{"912", "940"}) {
		t.Fatalf("AllowedSecurityFunctions = %v", got)
	}
	if !response.HasKind(ReturnCodeKindAccountLocked) && !response.HasKind(ReturnCodeKindGeneralError) {
		t.Fatal("HasKind did not find the general error")
	}
	if response.HasKind(ReturnCodeKindAccountLocked) {
		t.Fatal("HasKind found an absent kind")
	}
}

func TestResponseTouchdownPrefersSegmentCodes(t *testing.T) {
	input := "HIRMG:1:2+3040::More:global-touch'" +
		"HIRMS:2:2:1+3040::More:segment-touch'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if point, ok := response.Touchdown(); !ok || point != "segment-touch" {
		t.Fatalf("Touchdown = %q, %v", point, ok)
	}
}

func TestResponseWithoutErrorsOrTouchdown(t *testing.T) {
	response, err := ParseResponse([]byte("HIRMG:1:2+0010::OK'"))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if firstError, ok := response.FirstError(); ok {
		t.Fatalf("FirstError = %q, %v", firstError.Code, ok)
	}
	if point, ok := response.Touchdown(); ok {
		t.Fatalf("Touchdown = %q, %v", point, ok)
	}
	if functions := response.AllowedSecurityFunctions(); functions != nil {
		t.Fatalf("AllowedSecurityFunctions = %v", functions)
	}
}

func TestResponseReturnCodeStates(t *testing.T) {
	cases := []struct {
		name  string
		input string
		kind  ReturnCodeKind
		error bool
	}{
		{"tan required", "HIRMS:1:2:1+0030::Auftrag entgegengenommen'", ReturnCodeKindTanRequired, false},
		{"decoupled initiated", "HIRMS:1:2:1+3955::Freigabe ausstehend'", ReturnCodeKindDecoupledInitiated, false},
		{"decoupled pending", "HIRMS:1:2:1+3956::Noch nicht bestaetigt'", ReturnCodeKindDecoupledPending, false},
		{"sca exemption", "HIRMS:1:2:1+3076::Keine starke Authentifizierung'", ReturnCodeKindScaExemption, false},
		{"pin wrong", "HIRMG:1:2+9340::PIN falsch'", ReturnCodeKindPinWrong, true},
		{"account locked", "HIRMG:1:2+9942::Zugang gesperrt'", ReturnCodeKindAccountLocked, true},
		{"dialog aborted", "HIRMG:1:2+9800::Dialog abgebrochen'", ReturnCodeKindDialogAborted, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response, err := ParseResponse([]byte(testCase.input))
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			if !response.HasKind(testCase.kind) {
				t.Fatalf("HasKind(%v) = false", testCase.kind)
			}
			_, isError := response.FirstError()
			if isError != testCase.error {
				t.Fatalf("FirstError found = %v, want %v", isError, testCase.error)
			}
		})
	}
}
