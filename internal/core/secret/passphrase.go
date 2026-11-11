// Package secret holds the passphrase rules and the in-memory secret itself.
//
// A passphrase is normalised to Unicode NFC once, validated for its purpose and
// then kept as bytes. The rules mirror `crates/finglass-core/src/secret.rs`.
// Secrets are best effort: Go's garbage collector and any copies made by a
// caller or the webview cannot be wiped.
package secret

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The minimum number of runes for a new passphrase, counted after NFC.
const minNewLength = 10

// redacted is what every formatting and marshalling method returns.
const redacted = "Passphrase(***)"

// ErrEmpty is returned when the normalised input is empty.
var ErrEmpty = errors.New("passphrase must not be empty")

// ErrSurroundingWhitespace is returned when a new passphrase starts or ends
// with whitespace, or consists only of whitespace.
var ErrSurroundingWhitespace = errors.New("passphrase must not start or end with whitespace")

// ErrWiped is returned when a wiped passphrase is used.
var ErrWiped = errors.New("passphrase has been wiped")

// TooShortError is returned when a new passphrase has fewer than Min runes.
type TooShortError struct {
	// Min is the minimum number of runes.
	Min int
}

// Error implements error. It never contains the input.
func (e *TooShortError) Error() string {
	return fmt.Sprintf("passphrase must be at least %d characters", e.Min)
}

// Purpose says why a passphrase is being supplied.
type Purpose int

const (
	// Unlock opens an existing database. Only non-empty input is required.
	Unlock Purpose = iota
	// New creates a database or changes its passphrase. FinDesk's rules apply.
	New
)

// Passphrase is a validated passphrase, normalised to NFC.
//
// The zero value is not usable; build one with [NewPassphrase].
type Passphrase struct {
	value []byte
	wiped bool
}

// NewPassphrase normalises and validates input.
//
// The input is taken as bytes so the caller can wipe it afterwards. This
// function never modifies input. Intermediate buffers that held the secret are
// zeroed before it returns (best effort).
func NewPassphrase(input []byte, purpose Purpose) (*Passphrase, error) {
	// Append into a fresh buffer so the caller's slice is never touched and the
	// normalised copy can be wiped on every path.
	normalized := norm.NFC.Append(nil, input...)
	defer wipe(normalized)

	if len(normalized) == 0 {
		return nil, ErrEmpty
	}

	if purpose == New {
		if utf8.RuneCount(normalized) < minNewLength {
			return nil, &TooShortError{Min: minNewLength}
		}

		first, _ := utf8.DecodeRune(normalized)
		last, _ := utf8.DecodeLastRune(normalized)
		if unicode.IsSpace(first) || unicode.IsSpace(last) {
			return nil, ErrSurroundingWhitespace
		}

		allWhitespace := true
		for rest := normalized; len(rest) > 0; {
			r, size := utf8.DecodeRune(rest)
			if !unicode.IsSpace(r) {
				allWhitespace = false
				break
			}
			rest = rest[size:]
		}
		if allWhitespace {
			return nil, ErrSurroundingWhitespace
		}
	}

	value := make([]byte, len(normalized))
	copy(value, normalized)
	return &Passphrase{value: value}, nil
}

// Use calls f with the normalised bytes.
//
// Only `internal/core/db` (and, later, the credential store) may call Use. The
// slice b is owned by the Passphrase: f must not retain it and must not use it
// after Use returns.
func (p *Passphrase) Use(f func(b []byte) error) error {
	if p == nil || p.wiped {
		return ErrWiped
	}
	return f(p.value)
}

// Wipe zeroes the bytes and makes the passphrase unusable. Later calls to
// [Passphrase.Use] return [ErrWiped].
func (p *Passphrase) Wipe() {
	if p == nil {
		return
	}
	wipe(p.value)
	p.value = nil
	p.wiped = true
}

// The redaction methods use value receivers, so a dereferenced Passphrase
// (`*p`) is redacted too instead of printing its fields through reflection.

// String implements fmt.Stringer and never reveals the passphrase.
func (Passphrase) String() string {
	return redacted
}

// GoString implements fmt.GoStringer and never reveals the passphrase.
func (Passphrase) GoString() string {
	return redacted
}

// Format implements fmt.Formatter. Every verb, including %x and %q, renders
// the redacted form.
func (Passphrase) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(redacted))
}

// MarshalJSON returns the redacted JSON string.
func (Passphrase) MarshalJSON() ([]byte, error) {
	return []byte(`"***"`), nil
}

// MarshalText returns the redacted text form.
func (Passphrase) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// wipe zeroes b. It is best effort: the compiler and runtime may keep copies.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
