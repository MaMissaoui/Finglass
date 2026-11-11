package secret

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func mustPassphrase(t *testing.T, input string, purpose Purpose) *Passphrase {
	t.Helper()

	p, err := NewPassphrase([]byte(input), purpose)
	if err != nil {
		t.Fatalf("NewPassphrase(%q, %v): %v", input, purpose, err)
	}
	return p
}

func exposed(t *testing.T, p *Passphrase) string {
	t.Helper()

	var value string
	err := p.Use(func(b []byte) error {
		value = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	return value
}

type passphraseVector struct {
	InputHex      string `json:"input_hex"`
	Purpose       string `json:"purpose"`
	OK            bool   `json:"ok"`
	NormalizedHex string `json:"normalized_hex"`
	Error         string `json:"error"`
}

func loadVectors(t *testing.T) []passphraseVector {
	t.Helper()

	path := filepath.Join(repoRoot(t), "testdata", "golden", "passphrase_vectors.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var vectors []passphraseVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(vectors) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return vectors
}

func vectorPurpose(t *testing.T, purpose string) Purpose {
	t.Helper()

	switch purpose {
	case "unlock":
		return Unlock
	case "new":
		return New
	default:
		t.Fatalf("unknown purpose %q in the vectors", purpose)
		return Unlock
	}
}

func TestVectorsMatchTheRustBuild(t *testing.T) {
	for _, vector := range loadVectors(t) {
		name := fmt.Sprintf("%s/%s", vector.Purpose, vector.InputHex)
		if vector.InputHex == "" {
			name = vector.Purpose + "/empty"
		}

		t.Run(name, func(t *testing.T) {
			input, err := hex.DecodeString(vector.InputHex)
			if err != nil {
				t.Fatalf("decode input_hex: %v", err)
			}

			p, err := NewPassphrase(input, vectorPurpose(t, vector.Purpose))
			if vector.OK {
				if err != nil {
					t.Fatalf("NewPassphrase returned %v, want success", err)
				}
				got := hex.EncodeToString([]byte(exposed(t, p)))
				if got != vector.NormalizedHex {
					t.Fatalf("normalised bytes = %s, want %s", got, vector.NormalizedHex)
				}
				return
			}

			if err == nil {
				t.Fatal("NewPassphrase succeeded, want an error")
			}

			switch vector.Error {
			case "empty":
				if !errors.Is(err, ErrEmpty) {
					t.Fatalf("error = %v, want ErrEmpty", err)
				}
			case "too_short":
				var tooShort *TooShortError
				if !errors.As(err, &tooShort) {
					t.Fatalf("error = %v, want *TooShortError", err)
				}
				if tooShort.Min != minNewLength {
					t.Fatalf("Min = %d, want %d", tooShort.Min, minNewLength)
				}
			case "surrounding_whitespace":
				if !errors.Is(err, ErrSurroundingWhitespace) {
					t.Fatalf("error = %v, want ErrSurroundingWhitespace", err)
				}
			default:
				t.Fatalf("unknown error kind %q in the vectors", vector.Error)
			}
		})
	}
}

func TestNFCNormalizesBothFormsToTheSameValue(t *testing.T) {
	decomposed := mustPassphrase(t, "e\u0301", Unlock)
	composed := mustPassphrase(t, "\u00e9", Unlock)

	if exposed(t, decomposed) != exposed(t, composed) {
		t.Fatal("decomposed and composed forms differ after NFC")
	}
	if got := exposed(t, decomposed); got != "\u00e9" {
		t.Fatalf("normalised value = %q, want %q", got, "\u00e9")
	}
}

func TestEmptyInputIsRejectedForBothPurposes(t *testing.T) {
	for _, purpose := range []Purpose{Unlock, New} {
		if _, err := NewPassphrase(nil, purpose); !errors.Is(err, ErrEmpty) {
			t.Fatalf("purpose %v: error = %v, want ErrEmpty", purpose, err)
		}
	}
}

func TestNewRejectsShortInputInCharacters(t *testing.T) {
	_, err := NewPassphrase([]byte("aaaaaaaaa"), New)
	var tooShort *TooShortError
	if !errors.As(err, &tooShort) || tooShort.Min != 10 {
		t.Fatalf("nine ASCII characters: error = %v, want *TooShortError{Min: 10}", err)
	}

	_, err = NewPassphrase([]byte("\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9"), New)
	if !errors.As(err, &tooShort) || tooShort.Min != 10 {
		t.Fatalf("nine multi-byte characters: error = %v, want *TooShortError{Min: 10}", err)
	}

	if _, err := NewPassphrase([]byte("\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9"), New); err != nil {
		t.Fatalf("ten multi-byte characters: %v", err)
	}
}

func TestNewRejectsSurroundingWhitespace(t *testing.T) {
	cases := []string{
		" correct horse ",
		"\tcorrect horse",
		"           ",
		"\u00a0correct horse",
		"correct horse\u00a0",
	}
	for _, input := range cases {
		if _, err := NewPassphrase([]byte(input), New); !errors.Is(err, ErrSurroundingWhitespace) {
			t.Fatalf("NewPassphrase(%q): error = %v, want ErrSurroundingWhitespace", input, err)
		}
	}
}

func TestUnlockAcceptsAShortPassphrase(t *testing.T) {
	p := mustPassphrase(t, "x", Unlock)
	if got := exposed(t, p); got != "x" {
		t.Fatalf("value = %q, want %q", got, "x")
	}
}

func TestRedaction(t *testing.T) {
	const input = "secret-horse-battery"
	p := mustPassphrase(t, input, New)

	formats := []struct {
		verb   string
		format string
	}{
		{"%v", "%v"},
		{"%+v", "%+v"},
		{"%#v", "%#v"},
		{"%s", "%s"},
		{"%q", "%q"},
		{"%x", "%x"},
	}
	for _, tc := range formats {
		// Both the pointer and a dereferenced value must be redacted.
		for _, arg := range []any{p, *p} {
			got := fmt.Sprintf(tc.format, arg)
			if got != redacted {
				t.Fatalf("%s (%T) = %q, want %q", tc.verb, arg, got, redacted)
			}
			if strings.Contains(got, input) {
				t.Fatalf("%s (%T) leaked the passphrase: %q", tc.verb, arg, got)
			}
		}
	}
	if got, err := json.Marshal(struct{ P Passphrase }{*p}); err != nil || strings.Contains(string(got), input) {
		t.Fatalf("json.Marshal of a value leaked or failed: %s, %v", got, err)
	}

	if got := p.String(); got != redacted {
		t.Fatalf("String() = %q, want %q", got, redacted)
	}
	if got := p.GoString(); got != redacted {
		t.Fatalf("GoString() = %q, want %q", got, redacted)
	}

	jsonBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(jsonBytes) != `"***"` {
		t.Fatalf("json.Marshal = %s, want %q", jsonBytes, `"***"`)
	}

	text, err := p.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(text) != redacted {
		t.Fatalf("MarshalText = %q, want %q", text, redacted)
	}

	// The struct's unexported field must not leak through reflection either.
	jsonBytes, err = json.Marshal(struct {
		Pass *Passphrase `json:"pass"`
	}{Pass: p})
	if err != nil {
		t.Fatalf("json.Marshal struct: %v", err)
	}
	if string(jsonBytes) != `{"pass":"***"}` {
		t.Fatalf("json.Marshal struct = %s", jsonBytes)
	}
}

func TestWipe(t *testing.T) {
	p := mustPassphrase(t, "correct horse battery staple", New)

	p.Wipe()

	if p.value != nil {
		t.Fatalf("value = %v after Wipe, want nil", p.value)
	}

	err := p.Use(func([]byte) error {
		return nil
	})
	if !errors.Is(err, ErrWiped) {
		t.Fatalf("Use after Wipe = %v, want ErrWiped", err)
	}

	// Redaction still works on a wiped value.
	if got := fmt.Sprintf("%v", p); got != redacted {
		t.Fatalf("%%v after Wipe = %q, want %q", got, redacted)
	}

	// Wiping twice is safe.
	p.Wipe()
}

func TestErrorsDoNotContainTheInput(t *testing.T) {
	inputs := []struct {
		input   string
		purpose Purpose
	}{
		{" zzleakmarker ", New},
		{"zzshort", New},
		{"", New},
	}
	for _, tc := range inputs {
		_, err := NewPassphrase([]byte(tc.input), tc.purpose)
		if err == nil {
			t.Fatalf("NewPassphrase(%q) succeeded, want an error", tc.input)
		}
		for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(text, "zz") {
				t.Fatalf("error leaked the input: %q", text)
			}
		}
	}
}
