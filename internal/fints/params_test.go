package fints

import (
	"fmt"
	"strings"
	"testing"
)

func TestBankParamsIngestResponse(t *testing.T) {
	body := syncResponse(
		decoupledHitans(60, 5, 2),
		allowedSecurityFunction("942"),
		hipins("HKSAL:N+HKKAZ:J+HKCAZ:J+HKSPA:N"),
		hisals(7),
		hikazs(6),
		hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"),
		"HIUPD:9:6:4+1::280:99999999+"+testIBAN+"+test-customer++EUR+Erika Musterfrau++Girokonto+HKSAL:1+HKKAZ:1'",
		"HISPA:10:4:4+J:"+testIBAN2+":"+testBIC+":00000002::280:99999999'",
	)

	response, err := ParseResponse([]byte(body))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}

	params := NewBankParams()
	params.IngestResponse(response)

	if params.BPDVersion != testBPDVersion {
		t.Fatalf("BPDVersion = %d, want %d", params.BPDVersion, testBPDVersion)
	}
	if params.UPDVersion != testUPDVersion {
		t.Fatalf("UPDVersion = %d, want %d", params.UPDVersion, testUPDVersion)
	}
	if params.SystemID != testSystemID {
		t.Fatalf("SystemID = %q, want %q", params.SystemID, testSystemID)
	}
	if len(params.TANMethods) != 1 || params.TANMethods[0].SecurityFunction != "942" {
		t.Fatalf("TANMethods = %v", params.TANMethods)
	}
	if !params.TANMethods[0].IsDecoupled() {
		t.Fatal("the 942 method is not decoupled")
	}
	if params.TANMethods[0].DecoupledMaxPolls != 60 ||
		params.TANMethods[0].WaitBeforeFirstPoll != 5 ||
		params.TANMethods[0].WaitBeforeNextPoll != 2 {
		t.Fatalf("decoupled values = %d/%d/%d",
			params.TANMethods[0].DecoupledMaxPolls,
			params.TANMethods[0].WaitBeforeFirstPoll,
			params.TANMethods[0].WaitBeforeNextPoll)
	}
	if params.NeedsTAN(BusinessHKSAL) {
		t.Fatal("HIPINS says HKSAL needs no TAN")
	}
	if !params.NeedsTAN(BusinessHKKAZ) || !params.NeedsTAN(BusinessHKCAZ) {
		t.Fatal("HIPINS did not record the HKKAZ/HKCAZ TAN requirement")
	}
	if !params.NeedsTAN(BusinessHKTAB) {
		t.Fatal("an operation missing from HIPINS must default to needing a TAN")
	}
	if len(params.CamtFormats) != 1 ||
		params.CamtFormats[0] != "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02" {
		t.Fatalf("CamtFormats = %v", params.CamtFormats)
	}
	if len(params.Accounts) != 2 {
		t.Fatalf("Accounts = %d, want 2", len(params.Accounts))
	}
	if params.Accounts[0].IBAN != testIBAN || params.Accounts[1].IBAN != testIBAN2 {
		t.Fatalf("account IBANs = %q/%q", params.Accounts[0].IBAN, params.Accounts[1].IBAN)
	}
	if got := params.AllowedSecurityFunctions; len(got) != 1 || got[0] != "942" {
		t.Fatalf("AllowedSecurityFunctions = %v", got)
	}
	if got := params.HighestVersion(BusinessHKSAL); got != 7 {
		t.Fatalf("highest HKSAL version = %d, want 7", got)
	}
	if got := params.HighestVersion(BusinessHKKAZ); got != 6 {
		t.Fatalf("highest HKKAZ version = %d, want 6", got)
	}
	if got := params.HighestVersion(BusinessHKCAZ); got != 2 {
		t.Fatalf("highest HKCAZ version = %d, want 2", got)
	}
}

func TestBankParamsSupportedVersion(t *testing.T) {
	body := syncResponse(hisals(8), hisals(6), hikazs(6), hicazs(2, "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"))
	response, err := ParseResponse([]byte(body))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	params := NewBankParams()
	params.IngestResponse(response)

	cases := []struct {
		business BusinessSegment
		max      uint16
		want     uint16
	}{
		{BusinessHKSAL, 7, 6},
		{BusinessHKSAL, 8, 8},
		{BusinessHKSAL, 5, 5},
		{BusinessHKKAZ, 7, 6},
		{BusinessHKCAZ, 7, 2},
		{BusinessHKSPA, 7, 7},
		{BusinessSegment("HKXXX"), 7, 0},
	}
	for _, testCase := range cases {
		if got := params.SupportedVersion(testCase.business, testCase.max); got != testCase.want {
			t.Fatalf("SupportedVersion(%s, %d) = %d, want %d",
				testCase.business, testCase.max, got, testCase.want)
		}
	}
}

func TestBankParamsSelectSecurityFunction(t *testing.T) {
	decoupled := TanMethod{SecurityFunction: "942", HKTANVersion: 7, DecoupledMaxPolls: 60}
	chip := TanMethod{SecurityFunction: "962", HKTANVersion: 7, DecoupledMaxPolls: -1}

	t.Run("prefers the remembered function", func(t *testing.T) {
		params := NewBankParams()
		params.AllowedSecurityFunctions = []string{"999", "962", "942"}
		params.PreferredSecurityFunction = "962"
		params.TANMethods = []TanMethod{decoupled, chip}
		params.SelectSecurityFunction()
		if params.SelectedSecurityFunction != "962" {
			t.Fatalf("selected = %q, want 962", params.SelectedSecurityFunction)
		}
	})

	t.Run("first allowed decoupled", func(t *testing.T) {
		params := NewBankParams()
		params.AllowedSecurityFunctions = []string{"962", "999", "942"}
		params.TANMethods = []TanMethod{decoupled, chip}
		params.SelectSecurityFunction()
		if params.SelectedSecurityFunction != "942" {
			t.Fatalf("selected = %q, want 942", params.SelectedSecurityFunction)
		}
	})

	t.Run("first allowed when none is decoupled", func(t *testing.T) {
		params := NewBankParams()
		params.AllowedSecurityFunctions = []string{"962", "999"}
		params.TANMethods = []TanMethod{chip}
		params.SelectSecurityFunction()
		if params.SelectedSecurityFunction != "962" {
			t.Fatalf("selected = %q, want 962", params.SelectedSecurityFunction)
		}
	})

	t.Run("empty list leaves the selection", func(t *testing.T) {
		params := NewBankParams()
		params.SelectedSecurityFunction = "962"
		params.SelectSecurityFunction()
		if params.SelectedSecurityFunction != "962" {
			t.Fatalf("selected = %q, want 962", params.SelectedSecurityFunction)
		}
	})
}

func TestBankParamsHKTANVersionAndSelectedMethod(t *testing.T) {
	params := NewBankParams()
	if got := params.HKTANVersion(); got != 7 {
		t.Fatalf("default HKTANVersion = %d, want 7", got)
	}
	params.SelectedSecurityFunction = "942"
	params.TANMethods = []TanMethod{{SecurityFunction: "942", HKTANVersion: 6}}
	if got := params.HKTANVersion(); got != 6 {
		t.Fatalf("HKTANVersion = %d, want 6", got)
	}
	method, ok := params.SelectedMethod()
	if !ok || method.SecurityFunction != "942" {
		t.Fatalf("SelectedMethod = %v/%t", method, ok)
	}
}

func TestBankParamsFormattingMasksBankText(t *testing.T) {
	const (
		owner     = "Erika Musterfrau-Geheim"
		challenge = "Bitte TAN fuer DE00999999990000000001"
	)
	params := NewBankParams()
	params.SystemID = "system-secret-identifier"
	params.Accounts = []Account{{
		IBAN:          testIBAN,
		AccountNumber: "999900004242",
		owner:         owner,
		productName:   challenge,
	}}
	params.TANMethods = []TanMethod{{SecurityFunction: "942", name: owner}}
	params.AllowedSecurityFunctions = []string{"942"}

	for _, value := range []any{params, *params} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(format, value)
			for _, forbidden := range []string{testIBAN, "999900004242", owner, challenge, "system-secret-identifier"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("%s leaked %q: %q", format, forbidden, got)
				}
			}
		}
	}
}

// TestTheNewestSupportedHITANSVersionWins is the HVB case: the bank lists the
// same method in HITANS v1 and v6, the v1 one first. HKTAN must go out as v6.
func TestTheNewestSupportedHITANSVersionWins(t *testing.T) {
	params := NewBankParams()
	params.IngestResponse(&Response{Segments: []ParsedSegment{
		parseOne(t, "HITANS:1:1:4+1+1+1+N:N:0:1:942:2:TECH-OLD:mobileTAN:6:1:SMS:3:1:J:1'"),
		parseOne(t, "HITANS:2:6:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1'"),
	}})
	params.SelectedSecurityFunction = "942"
	if got := params.HKTANVersion(); got != 6 {
		t.Fatalf("HKTANVersion = %d, want 6", got)
	}

	// A version above what this client builds is never chosen.
	params = NewBankParams()
	params.IngestResponse(&Response{Segments: []ParsedSegment{
		parseOne(t, "HITANS:2:6:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1'"),
		parseOne(t, "HITANS:3:8:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1'"),
	}})
	params.SelectedSecurityFunction = "942"
	if got := params.HKTANVersion(); got != 6 {
		t.Fatalf("HKTANVersion with a v8 offer = %d, want 6", got)
	}
}
