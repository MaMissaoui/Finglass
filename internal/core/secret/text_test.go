package secret

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTextNeverFormatsTheSecret(t *testing.T) {
	const input = "987654"
	text := NewText([]byte(input))

	formats := []string{"%v", "%+v", "%#v", "%s", "%q", "%x"}
	for _, format := range formats {
		for _, arg := range []any{text, *text} {
			got := fmt.Sprintf(format, arg)
			if got != redactedText {
				t.Fatalf("%s (%T) = %q, want %q", format, arg, got, redactedText)
			}
			if strings.Contains(got, input) {
				t.Fatalf("%s (%T) leaked the secret: %q", format, arg, got)
			}
		}
	}

	if got := text.String(); got != redactedText {
		t.Fatalf("String() = %q, want %q", got, redactedText)
	}
	if got := text.GoString(); got != redactedText {
		t.Fatalf("GoString() = %q, want %q", got, redactedText)
	}
}

func TestTextWipeZeroesTheBytes(t *testing.T) {
	const input = "123456"
	text := NewText([]byte(input))

	// Keep the backing array so the test can prove Wipe zeroed it.
	backing := text.value

	text.Wipe()

	if text.value != nil {
		t.Fatalf("value = %v after Wipe, want nil", text.value)
	}
	for index, value := range backing {
		if value != 0 {
			t.Fatalf("byte %d = %d after Wipe, want 0", index, value)
		}
	}
	if !text.IsEmpty() {
		t.Fatal("wiped Text is not empty")
	}
	if text.Len() != 0 {
		t.Fatalf("Len() = %d after Wipe, want 0", text.Len())
	}
	if err := text.Use(func([]byte) error { return nil }); !errors.Is(err, ErrWiped) {
		t.Fatalf("Use after Wipe = %v, want ErrWiped", err)
	}
	if got := fmt.Sprintf("%v", text); got != redactedText {
		t.Fatalf("%%v after Wipe = %q, want %q", got, redactedText)
	}

	// Wiping twice is safe.
	text.Wipe()
}

func TestTextCopiesInput(t *testing.T) {
	input := []byte("654321")
	text := NewText(input)

	for index := range input {
		input[index] = 'x'
	}

	got := ""
	if err := text.Use(func(value []byte) error {
		got = string(value)
		return nil
	}); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if got != "654321" {
		t.Fatalf("Text changed with its input: %q", got)
	}
}

func TestTextEqualIsConstantTimeComparison(t *testing.T) {
	left := NewText([]byte("123456"))
	same := NewText([]byte("123456"))
	different := NewText([]byte("123457"))
	longer := NewText([]byte("1234567"))

	if !left.Equal(same) {
		t.Fatal("Equal returned false for equal secrets")
	}
	if left.Equal(different) {
		t.Fatal("Equal returned true for different secrets")
	}
	if left.Equal(longer) {
		t.Fatal("Equal returned true for different lengths")
	}
}
