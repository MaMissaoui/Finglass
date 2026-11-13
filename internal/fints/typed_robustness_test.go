package fints

import "testing"

func runTypedParsers(segment ParsedSegment) {
	_ = ParseTanMethods(segment)
	_ = ParseHIPINS(segment)
	_ = ParseCamtFormats(segment)
	_ = ParseHISPA(segment)
	_, _ = ParseHIUPD(segment)
	_, _ = ParseHITAN(segment)
	_ = ParseTanMedia(segment)
	_, _ = ParseBalance(segment)
	_ = ExtractMT940([]ParsedSegment{segment})
	_ = ExtractCamt([]ParsedSegment{segment})
}

func TestTypedParsersOnHostileInputNeverPanic(t *testing.T) {
	inputs := []string{
		"",
		"'",
		"+",
		"HITANS:1:7",
		"HITANS:1:7:4+",
		"HISAL:1:7:4+C:",
		"HISAL:1:7:4+C:not-a-number",
		"HIUPD:1:6:4+",
		"HISPA:1:4:4+",
		"HITAN:1:6:2+",
		"HITAB:1:6:4+",
		"HIKAZ:1:7:4+@99999@abc'",
		"HICAZ:1:7:4+@99999@abc'",
		"HICAZS:1:1:4+",
		"HIPINS:1:1:4+",
		"HIRMS:1:2+3040'",
		"HIRMS:1:2+3920'",
		"HIRMG:1:2+0010::OK'HIRMS:2:2:1+9999::Error'",
		"\x00\xff\xfe'",
	}
	for _, input := range inputs {
		runTypedParsers(ParsedSegment{})
		segments, err := ParseMessage([]byte(input))
		if err != nil {
			continue
		}
		for _, segment := range segments {
			runTypedParsers(segment)
		}
		_, _ = ParseResponse([]byte(input))
	}
}

func FuzzTypedResponses(f *testing.F) {
	seeds := []string{
		"HITANS:1:7:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J:962:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:Challenge:3:1:J:1:0:N:0:2:N:1:00:1'",
		"HITANS:1:6:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:962:2:TECH-CHIP:DKID-CHIP:1.0:chipTAN:6:1:Challenge:3:1:J:1:0:N:0:2:N:1:00:1'",
		"HITANS:1:1:4+1+1+1+N:N:0:1:942:2:TECH-OLD:mobileTAN:6:1:SMS:3:1:J:1'",
		"HITANS:1:5:4+1+1+1+N:N:0:912:2:TECH-V5:DKID-V5:1.0:mobileTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:2:1'",
		"HITANS:53:7:4+1+1+1+N:N:0:942:2:MTAN2:mobileTAN::mobile TAN:6:1:SMS:3:1:J:1:0:N:0:2:N:J:00:1:1:962:2:HHD1.4:HHD:1.4:Smart-TAN plus manuell:6:1:Challenge:3:1:J:1:0:N:0:2:N:J:00:1:1'",
		"HITANS:1:7:4+1+1+1+N:N:0:942:2:TECH-BAD:DKID-BAD:1.0:badTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:oops:x::J:J'",
		"HITANS:1:7:4+1+1+1+N:N:0:942:2:TECH-PUSH:DKID-PUSH:1.0:pushTAN:6:1:SMS:3:1:J:1:0:N:0:2:N:2:00:1:60:5:2:J:J:x:y'",
		"HITANS:1:7:4+1+1+1+N:N:0::2:TECH'",
		"HIPINS:6:1:4+1+1+0+HKSAL:N+HKKAZ:N+HKSPA:J+HKTAN:N'",
		"HICAZS:2:1:4+1+1+1+urn?:iso?:std?:iso?:20022?:tech?:xsd?:camt.052.001.02'",
		"HIUPD:7:6:4+1::280:99999999+DE00999999990000000001+test-customer+G+EUR+Erika Musterfrau++Girokonto+HKSAL:1+HKKAZ:1'",
		"HISPA:2:4:4+J:DE00999999990000000001:TESTDEFFXXX:00000001::280:99999999'",
		"HITAN:3:6:2+4+task-hash-1+task-ref-1+challenge-text+@3@ABC+20260315'",
		"HITAN:5:7:2+4+task-hash-7+task-ref-7+challenge-seven+@2@\x00\x01+20260315'",
		"HITAB:6:6:4+A:1:My Phone+1:1:Second Phone'",
		"HISAL:5:7:4+1::280:99999999+Girokonto+EUR+C:1234,56:EUR:20260315+D:25,50:EUR:20260314+5000,00+900,00'",
		"HIKAZ:1:7:4+@3@AAA+@3@BBB'",
		"HICAZ:1:7:4+@3@AAA+@3@BBB'",
		"HIRMG:1:2+0010::OK'",
		"HIRMS:1:2+3040::More:TOUCH-1234+3920::Allowed:912:940'",
		"",
		"'",
		"HITANS:1:7:4+",
		"HISAL:1:7:4+C:not-a-number'",
		"HIKAZ:1:7:4+@99999@abc'",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		segments, err := ParseMessage(data)
		if err == nil {
			for _, segment := range segments {
				runTypedParsers(segment)
			}
		}
		response, err := ParseResponse(data)
		if err == nil {
			_ = response.Codes()
			_, _ = response.FirstError()
			_, _ = response.Touchdown()
			_ = response.AllowedSecurityFunctions()
			_ = response.HasKind(ReturnCodeKindUnknown)
			_ = response.TanMethods()
			_ = response.TANRequiredOperations()
			_ = response.CamtFormats()
			_ = response.Accounts()
			_, _ = response.TANChallenge()
			_ = response.TanMedia()
			_, _ = response.Balance()
			_ = response.MT940()
			_ = response.Camt()
		}
	})
}
