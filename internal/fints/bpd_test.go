package fints

import (
	"reflect"
	"strconv"
	"testing"
)

func TestParseTanMethodsVersion6FlattenedTwoMethods(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:1:6:4+1+1+1+N:N:0"+
			":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1"+
			":962:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:Challenge:3:1:J:1:0:N:0:2:N:1:00:1'"))
	if len(methods) != 2 {
		t.Fatalf("methods = %d, want 2", len(methods))
	}

	push := methods[0]
	if push.SecurityFunction != "942" || push.TanProcess != TanProcessTwoStep {
		t.Fatalf("push = %#v", push)
	}
	if push.TechnicalID != "TECH-PUSH" {
		t.Fatalf("push.TechnicalID = %q", push.TechnicalID)
	}
	if push.DKID != "DKID-PUSH" || push.DKVersion != "1.0" {
		t.Fatalf("push DK = %q/%q", push.DKID, push.DKVersion)
	}
	if push.Name() != "pushTAN" {
		t.Fatalf("push.Name = %q", push.Name())
	}
	if push.MediumName != MediumRequired || !push.NeedsTanMedium() {
		t.Fatalf("push medium = %v/%t", push.MediumName, push.NeedsTanMedium())
	}
	if push.DecoupledMaxPolls != -1 || push.WaitBeforeFirstPoll != 0 || push.WaitBeforeNextPoll != 0 {
		t.Fatalf("push decoupled values = %d/%d/%d",
			push.DecoupledMaxPolls, push.WaitBeforeFirstPoll, push.WaitBeforeNextPoll)
	}
	if push.HKTANVersion != 6 {
		t.Fatalf("push.HKTANVersion = %d", push.HKTANVersion)
	}

	chip := methods[1]
	if chip.SecurityFunction != "962" {
		t.Fatalf("chip.SecurityFunction = %q", chip.SecurityFunction)
	}
	if chip.TechnicalID != "TECH-CHIP" || chip.DKID != "DKID-CHIP" || chip.DKVersion != "1.0" {
		t.Fatalf("chip = %#v", chip)
	}
	if chip.Name() != "chipTAN" {
		t.Fatalf("chip.Name = %q", chip.Name())
	}
	if chip.MediumName != MediumOptional || chip.NeedsTanMedium() {
		t.Fatalf("chip medium = %v/%t", chip.MediumName, chip.NeedsTanMedium())
	}
}

func TestParseTanMethodsVersion7FlattenedTwoMethods(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:1:7:4+1+1+1+N:N:0"+
			":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J"+
			":962:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:Challenge:3:1:J:1:0:N:0:2:N:1:00:1'"))
	if len(methods) != 2 {
		t.Fatalf("methods = %d, want 2", len(methods))
	}

	push := methods[0]
	if push.SecurityFunction != "942" || push.TanProcess != TanProcessTwoStep {
		t.Fatalf("push = %#v", push)
	}
	if push.TechnicalID != "TECH-PUSH" || push.DKID != "DKID-PUSH" || push.DKVersion != "1.0" {
		t.Fatalf("push = %#v", push)
	}
	if push.Name() != "pushTAN" {
		t.Fatalf("push.Name = %q", push.Name())
	}
	if push.MediumName != MediumRequired || !push.NeedsTanMedium() {
		t.Fatalf("push medium = %v/%t", push.MediumName, push.NeedsTanMedium())
	}
	if push.DecoupledMaxPolls != 60 || push.WaitBeforeFirstPoll != 5 || push.WaitBeforeNextPoll != 2 {
		t.Fatalf("push decoupled values = %d/%d/%d",
			push.DecoupledMaxPolls, push.WaitBeforeFirstPoll, push.WaitBeforeNextPoll)
	}
	if !push.ManualConfirmationAllowed || !push.AutomatedPollingAllowed {
		t.Fatalf("push manual/automated = %t/%t",
			push.ManualConfirmationAllowed, push.AutomatedPollingAllowed)
	}
	if !push.IsDecoupled() {
		t.Fatal("push.IsDecoupled = false")
	}
	if push.HKTANVersion != 7 {
		t.Fatalf("push.HKTANVersion = %d", push.HKTANVersion)
	}

	chip := methods[1]
	if chip.SecurityFunction != "962" {
		t.Fatalf("chip.SecurityFunction = %q", chip.SecurityFunction)
	}
	if chip.TechnicalID != "TECH-CHIP" || chip.DKID != "DKID-CHIP" || chip.DKVersion != "1.0" {
		t.Fatalf("chip = %#v", chip)
	}
	if chip.Name() != "chipTAN" {
		t.Fatalf("chip.Name = %q", chip.Name())
	}
	if chip.MediumName != MediumOptional || chip.NeedsTanMedium() {
		t.Fatalf("chip medium = %v/%t", chip.MediumName, chip.NeedsTanMedium())
	}
	if chip.DecoupledMaxPolls != -1 || chip.WaitBeforeFirstPoll != 0 || chip.WaitBeforeNextPoll != 0 {
		t.Fatalf("chip decoupled values = %d/%d/%d",
			chip.DecoupledMaxPolls, chip.WaitBeforeFirstPoll, chip.WaitBeforeNextPoll)
	}
	if chip.ManualConfirmationAllowed || chip.AutomatedPollingAllowed {
		t.Fatalf("chip manual/automated = %t/%t",
			chip.ManualConfirmationAllowed, chip.AutomatedPollingAllowed)
	}
	if chip.IsDecoupled() {
		t.Fatal("chip.IsDecoupled = true")
	}
}

func TestParseTanMethodsDecoupledTechnicalID(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:1:7:4+1+1+1+N:N:0"+
			":942:2:DECOUPLED-APP:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1'"))
	if len(methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(methods))
	}
	if methods[0].DecoupledMaxPolls != -1 {
		t.Fatalf("DecoupledMaxPolls = %d", methods[0].DecoupledMaxPolls)
	}
	if !methods[0].IsDecoupled() {
		t.Fatal("IsDecoupled = false, want true for a DECOUPLED technical id")
	}
}

func TestParseTanMethodsVersion1UsesFourHeaderElements(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:1:1:4+1+1+1+N:N:0:1:942:2:TECH-OLD:mobileTAN:6:1:SMS:3:1:J:1'"))
	if len(methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(methods))
	}
	method := methods[0]
	if method.SecurityFunction != "942" || method.TanProcess != TanProcessTwoStep {
		t.Fatalf("method = %#v", method)
	}
	if method.TechnicalID != "TECH-OLD" || method.Name() != "mobileTAN" {
		t.Fatalf("method = %#v", method)
	}
	if method.DKID != "" || method.DKVersion != "" {
		t.Fatalf("v1 DK fields = %q/%q", method.DKID, method.DKVersion)
	}
	if method.MediumName != MediumNotAllowed || method.NeedsTanMedium() {
		t.Fatalf("v1 medium = %v/%t", method.MediumName, method.NeedsTanMedium())
	}
	if method.DecoupledMaxPolls != -1 || method.HKTANVersion != 1 {
		t.Fatalf("v1 decoupled/version = %d/%d", method.DecoupledMaxPolls, method.HKTANVersion)
	}
}

func TestParseTanMethodsVersion4And5(t *testing.T) {
	for _, version := range []string{"4", "5"} {
		t.Run("version "+version, func(t *testing.T) {
			input := "HITANS:1:" + version + ":4+1+1+1+N:N:0" +
				":912:2:TECH-V" + version + ":DKID-V" + version + ":1.0:mobileTAN" +
				":6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:2:1'"
			methods := ParseTanMethods(parseOne(t, input))
			if len(methods) != 1 {
				t.Fatalf("methods = %d, want 1", len(methods))
			}
			method := methods[0]
			if method.SecurityFunction != "912" || method.Name() != "mobileTAN" {
				t.Fatalf("method = %#v", method)
			}
			if method.TechnicalID != "TECH-V"+version ||
				method.DKID != "DKID-V"+version || method.DKVersion != "1.0" {
				t.Fatalf("method = %#v", method)
			}
			if method.MediumName != MediumRequired || !method.NeedsTanMedium() {
				t.Fatalf("medium = %v/%t", method.MediumName, method.NeedsTanMedium())
			}
			if method.DecoupledMaxPolls != -1 || method.HKTANVersion != parseVersion(t, version) {
				t.Fatalf("decoupled/version = %d/%d", method.DecoupledMaxPolls, method.HKTANVersion)
			}
		})
	}
}

func TestParseTanMethodsFintsRSMockLine(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:53:7:4+1+1+1+N:N:0:942:2:MTAN2:mobileTAN::mobile TAN:6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:1:1:962:2:HHD1.4:HHD:1.4:Smart-TAN plus manuell:6:1:Challenge:3:1:J:1:0:N:0:2:N:J:00:1:1'"))
	if len(methods) != 2 {
		t.Fatalf("methods = %d, want 2", len(methods))
	}
	if methods[0].SecurityFunction != "942" || methods[1].SecurityFunction != "962" {
		t.Fatalf("security functions = %q/%q",
			methods[0].SecurityFunction, methods[1].SecurityFunction)
	}
	if methods[0].Name() != "mobile TAN" || methods[1].Name() != "Smart-TAN plus manuell" {
		t.Fatalf("names = %q/%q", methods[0].Name(), methods[1].Name())
	}
	if methods[0].TechnicalID != "MTAN2" || methods[1].TechnicalID != "HHD1.4" {
		t.Fatalf("technical ids = %q/%q", methods[0].TechnicalID, methods[1].TechnicalID)
	}
}

func parseVersion(t *testing.T, value string) uint16 {
	t.Helper()
	version, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		t.Fatalf("ParseUint(%q): %v", value, err)
	}
	return uint16(version)
}

func TestParseTanMethodsBadNumbersAndRemainders(t *testing.T) {
	t.Run("bad numbers fall back to absent", func(t *testing.T) {
		methods := ParseTanMethods(parseOne(t,
			"HITANS:1:7:4+1+1+1+N:N:0"+
				":942:2:TECH-BAD:DKID-BAD:1.0:badTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:oops:x::J:J'"))
		if len(methods) != 1 {
			t.Fatalf("methods = %d, want 1", len(methods))
		}
		method := methods[0]
		if method.DecoupledMaxPolls != -1 {
			t.Fatalf("DecoupledMaxPolls = %d", method.DecoupledMaxPolls)
		}
		if method.WaitBeforeFirstPoll != 0 || method.WaitBeforeNextPoll != 0 {
			t.Fatalf("waits = %d/%d", method.WaitBeforeFirstPoll, method.WaitBeforeNextPoll)
		}
		if !method.ManualConfirmationAllowed || !method.AutomatedPollingAllowed {
			t.Fatalf("manual/automated = %t/%t",
				method.ManualConfirmationAllowed, method.AutomatedPollingAllowed)
		}
	})

	t.Run("trailing remainder shorter than three is skipped", func(t *testing.T) {
		methods := ParseTanMethods(parseOne(t,
			"HITANS:1:7:4+1+1+1+N:N:0"+
				":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J"+
				":x:y'"))
		if len(methods) != 1 {
			t.Fatalf("methods = %d, want 1", len(methods))
		}
	})

	t.Run("empty security function is skipped", func(t *testing.T) {
		methods := ParseTanMethods(parseOne(t,
			"HITANS:1:7:4+1+1+1+N:N:0"+
				":942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J"+
				"::2:TECH'"))
		if len(methods) != 1 {
			t.Fatalf("methods = %d, want 1", len(methods))
		}
	})
}

func TestParseTanMethodsNameFallbackAndOneStep(t *testing.T) {
	methods := ParseTanMethods(parseOne(t,
		"HITANS:1:5:4+1+1+1+N:N:0:912:1:TECH'"))
	if len(methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(methods))
	}
	if methods[0].Name() != "TAN-912" {
		t.Fatalf("Name = %q", methods[0].Name())
	}
	if methods[0].TanProcess != TanProcessOneStep {
		t.Fatalf("TanProcess = %v", methods[0].TanProcess)
	}
}

func TestParseHIPINS(t *testing.T) {
	segment := parseOne(t, "HIPINS:6:1:4+1+1+0+HKSAL:N+HKKAZ:N+HKSPA:J+HKTAN:N'")
	operations := ParseHIPINS(segment)
	want := map[string]bool{
		"HKSAL": false,
		"HKKAZ": false,
		"HKSPA": true,
		"HKTAN": false,
	}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("ParseHIPINS = %v, want %v", operations, want)
	}
}

func TestParseHIPINSIgnoresMalformedPairs(t *testing.T) {
	segment := parseOne(t, "HIPINS:1:1:4+1+HKSAL:X+HKKAZ:J+TOOLONG:J+HKCAZ'")
	operations := ParseHIPINS(segment)
	if len(operations) != 1 || !operations["HKKAZ"] {
		t.Fatalf("ParseHIPINS = %v", operations)
	}
}

func TestHighestSegmentVersion(t *testing.T) {
	segments := []ParsedSegment{
		parseOne(t, "HISALS:10:5:4+1+1'"),
		parseOne(t, "HISALS:11:7:4+1+1'"),
		parseOne(t, "HIKAZS:12:6:4+1+1'"),
		parseOne(t, "HICAZS:13:2:4+1+1'"),
		parseOne(t, "HITABS:14:3:4+1+1'"),
	}

	cases := []struct {
		business BusinessSegment
		max      uint16
		want     uint16
	}{
		{BusinessHKSAL, 7, 7},
		{BusinessHKSAL, 6, 5},
		{BusinessHKSAL, 4, 0},
		{BusinessHKKAZ, 7, 6},
		{BusinessHKCAZ, 7, 2},
		{BusinessHKTAB, 7, 3},
		{BusinessHKTAN, 7, 0},
		{BusinessHKSPA, 7, 0},
		{BusinessSegment("HKUNKNOWN"), 7, 0},
	}
	for _, testCase := range cases {
		got := HighestSegmentVersion(segments, testCase.business, testCase.max)
		if got != testCase.want {
			t.Fatalf("HighestSegmentVersion(%s, %d) = %d, want %d",
				testCase.business, testCase.max, got, testCase.want)
		}
	}
}

func TestResponseHighestSegmentVersionAndCamtFormats(t *testing.T) {
	input := "HIBPA:1:3:4+42+280+0+1+1+0'" +
		"HICAZS:2:1:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02'" +
		"HICAZS:3:3:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.053.001.02'"
	response, err := ParseResponse([]byte(input))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if got := response.HighestSegmentVersion(BusinessHKCAZ, 7); got != 3 {
		t.Fatalf("HighestSegmentVersion = %d, want 3", got)
	}
	want := []string{
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.02",
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
	}
	if got := response.CamtFormats(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CamtFormats = %v, want %v", got, want)
	}
}

func TestParseCamtFormatsOneAndSeveralURNs(t *testing.T) {
	one := ParseCamtFormats(parseOne(t,
		"HICAZS:1:1:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02'"))
	wantOne := []string{"urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"}
	if !reflect.DeepEqual(one, wantOne) {
		t.Fatalf("one URN = %v", one)
	}

	several := ParseCamtFormats(parseOne(t,
		"HICAZS:2:1:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02:urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.053.001.02:not-a-urn'"))
	wantSeveral := []string{
		"urn:iso:std:iso:20022:tech:xsd:camt.052.001.02",
		"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
	}
	if !reflect.DeepEqual(several, wantSeveral) {
		t.Fatalf("several URNs = %v", several)
	}
}
