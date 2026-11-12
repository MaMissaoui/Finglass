package secret

import (
	"crypto/subtle"
	"fmt"
)

// redactedText is what every formatting method of Text returns.
const redactedText = "[redacted]"

// Text holds a secret such as a PIN or a TAN as bytes.
//
// It follows Passphrase's style: the bytes are copied in, never logged and
// never formatted. Wipe zeroes them and makes the Text unusable.
type Text struct {
	value []byte
	wiped bool
}

// NewText returns a Text holding a copy of input.
//
// The input is taken as bytes so the caller can wipe it afterwards. This
// function never modifies input.
func NewText(input []byte) *Text {
	value := make([]byte, len(input))
	copy(value, input)
	return &Text{value: value}
}

// Use calls f with the secret bytes.
//
// The slice b is owned by the Text: f must not retain it and must not use it
// after Use returns. Use after Wipe returns [ErrWiped].
func (t *Text) Use(f func(b []byte) error) error {
	if t == nil || t.wiped {
		return ErrWiped
	}
	return f(t.value)
}

// Len returns the number of bytes in the secret.
func (t *Text) Len() int {
	if t == nil || t.wiped {
		return 0
	}
	return len(t.value)
}

// IsEmpty reports whether the secret has no bytes.
func (t *Text) IsEmpty() bool {
	return t.Len() == 0
}

// Equal reports whether other holds the same bytes, in constant time.
//
// A nil or wiped Text compares as empty. Comparing lengths is not constant
// time, but no secret bytes are revealed beyond a length mismatch.
func (t *Text) Equal(other *Text) bool {
	left := t.bytes()
	right := other.bytes()
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare(left, right) == 1
}

// Wipe zeroes the bytes and makes the Text unusable. Later calls to [Text.Use]
// return [ErrWiped]. Wiping twice is safe.
func (t *Text) Wipe() {
	if t == nil {
		return
	}
	wipe(t.value)
	t.value = nil
	t.wiped = true
}

func (t *Text) bytes() []byte {
	if t == nil || t.wiped {
		return nil
	}
	return t.value
}

// The redaction methods use value receivers, so a dereferenced Text (`*t`) is
// redacted too instead of printing its fields through reflection.

// String implements fmt.Stringer and never reveals the secret.
func (Text) String() string {
	return redactedText
}

// GoString implements fmt.GoStringer and never reveals the secret.
func (Text) GoString() string {
	return redactedText
}

// Format implements fmt.Formatter. Every verb, including %x and %q, renders
// the redacted form.
func (Text) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(redactedText))
}
