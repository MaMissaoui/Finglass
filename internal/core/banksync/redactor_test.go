package banksync

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/iban"
)

func TestMasksKnownSecretsAndIBANLikeTokens(t *testing.T) {
	line := "user=1234567890 iban=DE89370400440532013000 amount=12,34 rewe"
	redacted := Redact(line, []string{"1234567890"})
	if contains(redacted, "1234567890") {
		t.Fatalf("redacted contains the secret: %q", redacted)
	}
	if contains(redacted, "DE89370400440532013000") {
		t.Fatalf("redacted contains the IBAN: %q", redacted)
	}
	if !contains(redacted, "amount=12,34 rewe") {
		t.Fatalf("redacted lost readable text: %q", redacted)
	}
}

func TestDoesNotMaskShortValues(t *testing.T) {
	if got := Redact("PIN 123 bleibt", []string{"123"}); got != "PIN 123 bleibt" {
		t.Fatalf("Redact = %q, want %q", got, "PIN 123 bleibt")
	}
}

func TestMasksTheLongestOverlappingSecretWithoutLeavingFragments(t *testing.T) {
	if got := Redact("abcdef", []string{"abc", "abcdef"}); got != Mask {
		t.Fatalf("Redact = %q, want %q", got, Mask)
	}
}

func TestBlankSecretsAreIgnored(t *testing.T) {
	if got := Redact("text", []string{"", "  "}); got != "text" {
		t.Fatalf("Redact = %q, want %q", got, "text")
	}
}

func TestMasksSecretsInTheirFintsEscapedForm(t *testing.T) {
	line := "HNSHA:5:2+998+1234567+++ab?+cd?:e?'f???@g'"
	if got, want := Redact(line, []string{"ab+cd:e'f?@g"}), "HNSHA:5:2+998+1234567+++X'"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestEscapesEveryFintsSyntaxCharacterOnce(t *testing.T) {
	if got, want := FintsEscaped("a+b:c'd?e@f"), "a?+b?:c?'d??e?@f"; got != want {
		t.Fatalf("FintsEscaped = %q, want %q", got, want)
	}
	if got := FintsEscaped("plain"); got != "plain" {
		t.Fatalf("FintsEscaped = %q, want %q", got, "plain")
	}
}

func TestMasksSpacedAndLowerCaseIBANs(t *testing.T) {
	if got, want := Redact("IBAN DE89 3704 0044 0532 0130 00 Miete", nil), "IBAN "+Mask+" Miete"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
	if got := Redact("de89370400440532013000", nil); got != Mask {
		t.Fatalf("Redact = %q, want %q", got, Mask)
	}
	if got := Redact("de89 3704 0044 0532 0130 00", nil); got != Mask {
		t.Fatalf("Redact = %q, want %q", got, Mask)
	}
}

func TestMasksASpacedIBANWhoseRegexMatchSwallowedTheNextWord(t *testing.T) {
	ibanText := "ES91 2100 0418 4502 0005 1332"
	if !iban.IsValid(ibanText) {
		t.Fatal("test IBAN is not valid")
	}
	if got, want := Redact(ibanText+" EUR", nil), Mask+" EUR"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestMasksASpacedIBANWhoseRegexMatchSwallowedTheNextBIC(t *testing.T) {
	ibanText := "AT61 1904 3002 3457 3201"
	if !iban.IsValid(ibanText) {
		t.Fatal("test IBAN is not valid")
	}
	if got, want := Redact(ibanText+" BIC BKAUATWW", nil), Mask+" BIC BKAUATWW"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestMasksASpacedIBANWhoseRegexMatchSwallowedTheNextNumber(t *testing.T) {
	ibanText := "BE68 5390 0754 7034"
	if !iban.IsValid(ibanText) {
		t.Fatal("test IBAN is not valid")
	}
	if got, want := Redact(ibanText+" 2026", nil), Mask+" 2026"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestMasksASpacedIBANInsideASentenceAndKeepsTheSurroundingWords(t *testing.T) {
	ibanText := "PL61 1090 1014 0000 0712 1981 2874"
	if !iban.IsValid(ibanText) {
		t.Fatal("test IBAN is not valid")
	}
	if got, want := Redact("Konto "+ibanText+" ok", nil), "Konto "+Mask+" ok"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestDoesNotMaskASpacedTokenWithoutAValidIBANPrefix(t *testing.T) {
	token := strings.Join([]string{"DE00", "1234", "5678", "9012", "3456", "78", "X"}, " ")
	if iban.IsValid("DE00 1234 5678 9012 3456 78") {
		t.Fatal("test IBAN is valid")
	}
	if got := Redact(token, nil); got != token {
		t.Fatalf("Redact = %q, want %q", got, token)
	}
}

func TestKeepsATypicalTraceLineUnchanged(t *testing.T) {
	line := "HNHBK:1:3+000000000123+300+DIA4711ABCD12345678+2'HIRMG:2:2+3060::Bitte beachten Sie die enthaltenen Warnungen.'"
	if got := Redact(line, nil); got != line {
		t.Fatalf("Redact = %q, want %q", got, line)
	}
}

func TestDoesNotMaskAnIBANLookingTokenThatFailsTheCheckDigits(t *testing.T) {
	token := strings.Join([]string{"DE00", "1234", "5678", "9012", "3456", "78"}, " ")
	if iban.IsValid(token) {
		t.Fatal("test IBAN is valid")
	}
	if got := Redact(token, nil); got != token {
		t.Fatalf("Redact = %q, want %q", got, token)
	}
}

func TestMaskAccountNumbersMasksIBANsKeepingTheLastFour(t *testing.T) {
	if got, want := MaskAccountNumbers("IBAN DE89370400440532013000 ungültig"), "IBAN …3000 ungültig"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
	if got, want := MaskAccountNumbers("IBAN DE89 3704 0044 0532 0130 00 ungültig"), "IBAN …3000 ungültig"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
	if got, want := MaskAccountNumbers("de89370400440532013000"), "…3000"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
}

func TestMaskAccountNumbersMasksASpacedIBANWhoseMatchSwallowedTheNextWord(t *testing.T) {
	if got, want := MaskAccountNumbers("Konto ES91 2100 0418 4502 0005 1332 EUR ok"), "Konto …1332 EUR ok"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
}

func TestMaskAccountNumbersDoesNotTurnAKeptIBANTailIntoAReturnCode(t *testing.T) {
	masked := MaskAccountNumbers("Konto ES91 2100 0418 4502 0005 1332 EUR ok")
	if codes := ReturnCodes(&masked); len(codes) != 0 {
		t.Fatalf("ReturnCodes = %v, want none", codes)
	}
}

func TestMaskAccountNumbersKeepsTheLastFourOfValidSpacedAndLowerCaseIBANs(t *testing.T) {
	if got, want := MaskAccountNumbers("DE89 3704 0044 0532 0130 00"), "…3000"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
	if got, want := MaskAccountNumbers("de89 3704 0044 0532 0130 00"), "…3000"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
}

func TestMaskAccountNumbersFailsClosedForASpacedIBANWithAWrongCheckDigit(t *testing.T) {
	if iban.IsValid("DE00 3704 0044 0532 0130 00") {
		t.Fatal("test IBAN is valid")
	}
	if got, want := MaskAccountNumbers("DE00 3704 0044 0532 0130 00"), "…3000"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
}

func TestRedactKeepsItsTraceBehaviourForASpacedIBANWhoseMatchSwallowedTheNextWord(t *testing.T) {
	if got, want := Redact("Konto ES91 2100 0418 4502 0005 1332 EUR ok", nil), "Konto "+Mask+" EUR ok"; got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}

func TestMaskAccountNumbersMasksLongDigitRunsKeepingTheLastFour(t *testing.T) {
	if got, want := MaskAccountNumbers("Konto 1234567890 gesperrt"), "Konto …7890 gesperrt"; got != want {
		t.Fatalf("MaskAccountNumbers = %q, want %q", got, want)
	}
}

func TestMaskAccountNumbersLeavesReturnCodesDatesAmountsAndShortNumbersAlone(t *testing.T) {
	cases := []string{
		"9050 Auftrag abgelehnt am 23.10.2026 über 12,34 EUR",
		"Betrag 10000 EUR",
		"Jahr 2026",
	}
	for _, text := range cases {
		if got := MaskAccountNumbers(text); got != text {
			t.Fatalf("MaskAccountNumbers(%q) = %q, want %q", text, got, text)
		}
	}
}

func TestMaskAccountNumbersMasksLabelledDigitGroupsKeepingTheLastFour(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"Konto 9876 5432", "Konto …5432"},
		{"Kto.-Nr. 1234 5678 90", "Kto.-Nr. …7890"},
		{"Empfängerkonto 1234 567890", "Empfängerkonto …7890"},
		{"Girokonto 9876 5432 gesperrt", "Girokonto …5432 gesperrt"},
		{"Kontonummer 1234567890", "Kontonummer …7890"},
		{"Account no. 9876 5432", "Account no. …5432"},
		{"Kto 1234 5678.", "Kto …5678."},
	}
	for _, testCase := range cases {
		if got := MaskAccountNumbers(testCase.text); got != testCase.want {
			t.Fatalf("MaskAccountNumbers(%q) = %q, want %q", testCase.text, got, testCase.want)
		}
	}
}

func TestMaskAccountNumbersLeavesUnlabelledDigitGroupsAlone(t *testing.T) {
	cases := []string{
		"Kontostand 1 234,56 EUR",
		"Konto 123 456,78 EUR",
		"Konto: 12 34",
		"im Oktober 1234 5678",
		"24.10.2026",
		"80331 München",
		"9050 3076 Auftrag abgelehnt",
	}
	for _, text := range cases {
		if got := MaskAccountNumbers(text); got != text {
			t.Fatalf("MaskAccountNumbers(%q) = %q, want %q", text, got, text)
		}
	}
}

func TestReturnCodesStillFindCodesNextToEachOtherAfterMasking(t *testing.T) {
	masked := MaskAccountNumbers("9050 3076 Auftrag abgelehnt")
	if got, want := ReturnCodes(&masked), []string{"9050", "3076"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReturnCodes = %v, want %v", got, want)
	}
	masked = MaskAccountNumbers("Konto 9876 5432: 9050 Auftrag abgelehnt")
	if got, want := ReturnCodes(&masked), []string{"9050"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReturnCodes = %v, want %v", got, want)
	}
}

func TestMaskAccountNumbersIsIdempotentForLabelledGroupsAndIBANs(t *testing.T) {
	for _, text := range []string{"Konto 9876 5432", "IBAN DE89 3704 0044 0532 0130 00"} {
		once := MaskAccountNumbers(text)
		if twice := MaskAccountNumbers(once); twice != once {
			t.Fatalf("MaskAccountNumbers not idempotent for %q: %q then %q", text, once, twice)
		}
	}
}

func contains(text, wanted string) bool {
	for index := 0; index+len(wanted) <= len(text); index++ {
		if text[index:index+len(wanted)] == wanted {
			return true
		}
	}
	return false
}

type redactorGoldenRedaction struct {
	Secrets []string `json:"secrets"`
	Out     string   `json:"out"`
}

type redactorGoldenCase struct {
	Text                string                    `json:"text"`
	MaskAccountNumbers  string                    `json:"mask_account_numbers"`
	ReturnCodesOfMasked []string                  `json:"return_codes_of_masked"`
	ReturnCodesOfText   []string                  `json:"return_codes_of_text"`
	Redactions          []redactorGoldenRedaction `json:"redact"`
}

type redactorGoldenEscaped struct {
	Value string `json:"value"`
	Out   string `json:"out"`
}

type redactorGolden struct {
	Cases           []redactorGoldenCase    `json:"cases"`
	FintsEscaped    []redactorGoldenEscaped `json:"fints_escaped"`
	ReturnCodesNone []string                `json:"return_codes_none"`
}

// TestRedactorMatchesRust checks every redactor result against the corpus the
// Rust implementation wrote.
func TestRedactorMatchesRust(t *testing.T) {
	const path = "testdata/redactor.json"
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the package directory
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var golden redactorGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(golden.Cases) == 0 {
		t.Fatalf("%s has no cases", path)
	}

	for index, testCase := range golden.Cases {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			if got := MaskAccountNumbers(testCase.Text); got != testCase.MaskAccountNumbers {
				t.Fatalf("text %q\nMaskAccountNumbers = %q, want %q", testCase.Text, got, testCase.MaskAccountNumbers)
			}

			masked := testCase.MaskAccountNumbers
			if got := ReturnCodes(&masked); !reflect.DeepEqual(got, testCase.ReturnCodesOfMasked) {
				t.Fatalf("text %q\nReturnCodes(masked) = %q, want %q", testCase.Text, got, testCase.ReturnCodesOfMasked)
			}
			original := testCase.Text
			if got := ReturnCodes(&original); !reflect.DeepEqual(got, testCase.ReturnCodesOfText) {
				t.Fatalf("text %q\nReturnCodes(text) = %q, want %q", testCase.Text, got, testCase.ReturnCodesOfText)
			}

			for _, redaction := range testCase.Redactions {
				if got := Redact(testCase.Text, redaction.Secrets); got != redaction.Out {
					t.Fatalf("text %q\nRedact(secrets %q) = %q, want %q", testCase.Text, redaction.Secrets, got, redaction.Out)
				}
			}
		})
	}

	for _, escaped := range golden.FintsEscaped {
		if got := FintsEscaped(escaped.Value); got != escaped.Out {
			t.Fatalf("FintsEscaped(%q) = %q, want %q", escaped.Value, got, escaped.Out)
		}
	}

	if got := ReturnCodes(nil); !reflect.DeepEqual(got, golden.ReturnCodesNone) {
		t.Fatalf("ReturnCodes(nil) = %q, want %q", got, golden.ReturnCodesNone)
	}
}
