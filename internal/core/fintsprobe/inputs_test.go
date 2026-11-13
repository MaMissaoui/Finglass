package fintsprobe

import (
	"fmt"
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{
		ProductIDVar: "TESTPRODUCT0000000000000",
		BLZVar:       "99999999",
		UserVar:      "test-user",
		URLVar:       "https://fints.example.invalid/",
	}
}

func lookupMap(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func parseMap(values map[string]string) (Inputs, []InputError) {
	return ParseInputs(lookupMap(values))
}

func TestParsesAllInputsAndDefaultsDaysTo30(t *testing.T) {
	inputs, errors := parseMap(validEnv())
	if len(errors) != 0 {
		t.Fatalf("errors = %v", errors)
	}
	if inputs.ProductID != "TESTPRODUCT0000000000000" {
		t.Fatal("product id not parsed")
	}
	if inputs.BLZ != "99999999" {
		t.Fatalf("BLZ = %q", inputs.BLZ)
	}
	if inputs.User != "test-user" {
		t.Fatalf("User = %q", inputs.User)
	}
	if inputs.URL != "https://fints.example.invalid/" {
		t.Fatalf("URL = %q", inputs.URL)
	}
	if inputs.Days != 30 {
		t.Fatalf("Days = %d, want 30", inputs.Days)
	}
}

func TestParsesExplicitDays(t *testing.T) {
	values := validEnv()
	values[DaysVar] = "90"
	inputs, errors := parseMap(values)
	if len(errors) != 0 {
		t.Fatalf("errors = %v", errors)
	}
	if inputs.Days != 90 {
		t.Fatalf("Days = %d, want 90", inputs.Days)
	}
}

func TestEachRequiredInputCanBeMissing(t *testing.T) {
	for _, variable := range []string{BLZVar, UserVar, URLVar} {
		values := validEnv()
		delete(values, variable)
		_, errors := parseMap(values)
		if len(errors) != 1 {
			t.Fatalf("%s: errors = %v, want one", variable, errors)
		}
		if errors[0].Variable != variable {
			t.Fatalf("%s: error variable = %q", variable, errors[0].Variable)
		}
		if !strings.Contains(errors[0].Error(), "required") {
			t.Fatalf("%s: error = %q", variable, errors[0].Error())
		}
	}
}

func TestMissingProductIDIsNotAnError(t *testing.T) {
	values := validEnv()
	delete(values, ProductIDVar)
	inputs, errors := parseMap(values)
	if len(errors) != 0 {
		t.Fatalf("errors = %v", errors)
	}
	if inputs.ProductID != "" {
		t.Fatalf("ProductID = %q, want empty", inputs.ProductID)
	}
}

func TestRejectsInvalidProductIDs(t *testing.T) {
	cases := []string{
		strings.Repeat("A", 31),
		"PRODUCT?ID",
		"PRODUCT'ID",
		"PRODUCT+ID",
		"PRODUCT:ID",
		"PRODUCT@ID",
		"PRODUCT ID",
		"PRODUKTÄ",
	}
	for _, value := range cases {
		values := validEnv()
		values[ProductIDVar] = value
		_, errors := parseMap(values)
		if len(errors) != 1 || errors[0].Variable != ProductIDVar {
			t.Fatalf("value %q: errors = %v", value, errors)
		}
		if strings.Contains(errors[0].Error(), strings.TrimSpace(value)) {
			t.Fatalf("error leaked the value: %q", errors[0].Error())
		}
	}
}

// TestProductIDRule mirrors fintsproduct's tests with the same cases.
func TestProductIDRule(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "valid", value: "TESTPRODUCT0000000000000", want: true},
		{name: "shortest", value: "A", want: true},
		{name: "longest", value: strings.Repeat("A", 30), want: true},
		{name: "too long", value: strings.Repeat("A", 31), want: false},
		{name: "question mark", value: "PRODUCT?ID", want: false},
		{name: "apostrophe", value: "PRODUCT'ID", want: false},
		{name: "plus", value: "PRODUCT+ID", want: false},
		{name: "colon", value: "PRODUCT:ID", want: false},
		{name: "at sign", value: "PRODUCT@ID", want: false},
		{name: "space", value: "PRODUCT ID", want: false},
		{name: "umlaut", value: "PRODUKTÄ", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validProductID(test.value); got != test.want {
				t.Fatalf("validProductID(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestRejectsBLZThatIsNotEightDigits(t *testing.T) {
	for _, value := range []string{"12345", "123456789", "1234abcd", "zzzzzzzz"} {
		values := validEnv()
		values[BLZVar] = value
		_, errors := parseMap(values)
		if len(errors) != 1 || errors[0].Variable != BLZVar {
			t.Fatalf("value %q: errors = %v", value, errors)
		}
		if strings.Contains(errors[0].Error(), value) {
			t.Fatalf("error leaked the value: %q", errors[0].Error())
		}
	}
}

func TestRejectsUserWithFinTSSeparators(t *testing.T) {
	for _, value := range []string{"user?name", "user'name", "user+name", "user:name", "user@name"} {
		values := validEnv()
		values[UserVar] = value
		_, errors := parseMap(values)
		if len(errors) != 1 || errors[0].Variable != UserVar {
			t.Fatalf("value %q: errors = %v", value, errors)
		}
		if strings.Contains(errors[0].Error(), value) {
			t.Fatalf("error leaked the value: %q", errors[0].Error())
		}
	}
}

func TestRejectsInvalidURLs(t *testing.T) {
	for _, value := range []string{
		"http://fints.example.invalid/",
		"https://",
		"https:///path",
		"https://user:pass@fints.example.invalid/",
		"https://fints.example.invalid/#fragment",
		"https://fints.example.invalid/#",
		"not a URL",
		"ftp://fints.example.invalid/",
		"https://fints.example.invalid:bad/",
		"https://fints.example.invalid/%zz",
		"https://fints.example.invalid\\evil",
	} {
		values := validEnv()
		values[URLVar] = value
		_, errors := parseMap(values)
		if len(errors) != 1 || errors[0].Variable != URLVar {
			t.Fatalf("value %q: errors = %v", value, errors)
		}
		if strings.Contains(errors[0].Error(), value) {
			t.Fatalf("error leaked the value: %q", errors[0].Error())
		}
	}
}

func TestAcceptsAnUppercaseScheme(t *testing.T) {
	values := validEnv()
	values[URLVar] = "HTTPS://fints.example.invalid/"
	if _, errors := parseMap(values); len(errors) != 0 {
		t.Fatalf("errors = %v", errors)
	}
}

func TestRejectsOutOfRangeAndNonNumericDays(t *testing.T) {
	for _, value := range []string{"0", "91", "abc", "-1", "1.5"} {
		values := validEnv()
		values[DaysVar] = value
		_, errors := parseMap(values)
		if len(errors) != 1 || errors[0].Variable != DaysVar {
			t.Fatalf("value %q: errors = %v", value, errors)
		}
	}
	for _, value := range []string{"abc", "-42", "1.5", "ninety"} {
		values := validEnv()
		values[DaysVar] = value
		_, errors := parseMap(values)
		if strings.Contains(errors[0].Error(), value) {
			t.Fatalf("error leaked the value: %q", errors[0].Error())
		}
	}
}

func TestInputsFormattingMasksSecretsAndURLPath(t *testing.T) {
	inputs, errors := parseMap(validEnv())
	if len(errors) != 0 {
		t.Fatalf("errors = %v", errors)
	}
	inputs.URL = "https://fints.example.invalid/secret-path"

	formats := []string{"%v", "%+v", "%#v", "%s", "%q"}
	for _, format := range formats {
		for _, value := range []any{inputs, &inputs} {
			got := fmt.Sprintf(format, value)
			for _, forbidden := range []string{
				inputs.ProductID,
				inputs.User,
				"/secret-path",
				"secret-path",
			} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("%s leaked %q: %q", format, forbidden, got)
				}
			}
			if !strings.Contains(got, "https://fints.example.invalid") {
				t.Fatalf("%s did not keep the host: %q", format, got)
			}
		}
	}
}

func TestAllInputErrorsAreReportedTogether(t *testing.T) {
	values := map[string]string{
		BLZVar:  "bad",
		UserVar: "bad?user",
		URLVar:  "http://fints.example.invalid/",
		DaysVar: "0",
	}
	_, errors := parseMap(values)
	if len(errors) != 4 {
		t.Fatalf("errors = %v, want four", errors)
	}
	wanted := map[string]bool{BLZVar: true, UserVar: true, URLVar: true, DaysVar: true}
	for _, inputError := range errors {
		if !wanted[inputError.Variable] {
			t.Fatalf("unexpected variable %q", inputError.Variable)
		}
		delete(wanted, inputError.Variable)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing errors for %v", wanted)
	}
}
