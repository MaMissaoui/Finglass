// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// Source supplies the non-deterministic inputs of [BuildMessage]: the clock
// for the HNSHK/HNVSK timestamps and the security reference number shared by
// HNSHK and HNSHA.
//
// A nil function field means the production default: time.Now for Now, and a
// cryptographically random seven-digit number for SecurityReference. Tests
// inject fixed values so an encoded message is reproducible byte for byte.
type Source struct {
	// Now returns the time used in the security timestamps.
	Now func() time.Time
	// SecurityReference returns the number that links HNSHK and HNSHA.
	SecurityReference func() (uint32, error)
}

func (s Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Source) securityReference() (uint32, error) {
	if s.SecurityReference != nil {
		return s.SecurityReference()
	}
	return randomSecurityReference()
}

// randomSecurityReference returns a number in 1_000_000..9_999_999, the range
// fints-rs uses.
func randomSecurityReference() (uint32, error) {
	var buffer [4]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0, err
	}
	value := binary.BigEndian.Uint32(buffer[:])
	return 1_000_000 + value%9_000_000, nil
}

// MessageInput describes the envelope of one outgoing message.
//
// The product id is deliberately not part of it: the product id travels only
// in the HKVVB business segment, built with [NewHKVVB].
type MessageInput struct {
	// BLZ is the German bank code.
	BLZ string
	// UserID is the online-banking login. It is the user id in the HNSHK and
	// HNVSK key names.
	UserID string
	// CustomerID is the Kunden-ID. HKIDN carries it; build HKIDN with it.
	CustomerID string
	// SystemID is the system id assigned by the bank, or "0" before the first
	// synchronisation.
	SystemID string
	// DialogID is the dialog id assigned by the bank, or "0" at dialog start.
	DialogID string
	// MessageNumber is the message number within the dialog.
	MessageNumber uint16
	// SecurityFunction is the TAN security function, for example "999" for
	// PIN-only.
	SecurityFunction string
	// PIN is required and is only read inside its Use method.
	PIN *secret.Text
	// TAN is optional. A non-nil, empty TAN writes an empty TAN field.
	TAN *secret.Text
	// Source supplies the clock and the security reference number.
	Source Source
}

// String returns only the type name, never the connection data or a secret.
func (MessageInput) String() string {
	return "fints.MessageInput{}"
}

// GoString returns only the type name.
func (MessageInput) GoString() string {
	return "fints.MessageInput{}"
}

// Format writes only the type name, for every fmt verb.
func (MessageInput) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte("fints.MessageInput{}"))
}

// BuildMessage builds a complete PIN/TAN message:
//
//	HNHBK:1:3   header with the total length
//	HNVSK:998:3 encryption head
//	HNVSD:999:1 encrypted data wrapper, containing
//	  HNSHK:2:4 signature head
//	  the business segments, numbered from 3
//	  HNSHA:n:2 signature footer with the PIN and optional TAN
//	HNHBS:m:1   trailer
//
// The returned bytes contain the PIN and, when given, the TAN. The caller must
// pass them to [Wipe] as soon as they are no longer needed, and must not turn
// them into a string or log them. The PIN and TAN are only read inside
// [secret.Text.Use]; they never become a Go string, a format argument or an
// error.
//
// BuildMessage is deterministic for a fixed [Source].
func BuildMessage(input MessageInput, segments ...Segment) ([]byte, error) {
	if input.PIN == nil {
		return nil, errMissingPIN
	}
	if input.SecurityFunction == "" {
		return nil, errMissingSecurityFunction
	}
	if len(segments) > 65_000 {
		return nil, errTooManySegments
	}
	for _, segment := range segments {
		if err := segment.validate(); err != nil {
			return nil, err
		}
	}

	reference, err := input.Source.securityReference()
	if err != nil {
		return nil, fmt.Errorf("fints: security reference: %w", err)
	}
	now := input.Source.now()

	inner := make([]byte, 0, 1024)
	inner, err = appendOutgoingSegment(
		inner,
		newHNSHK(input.SecurityFunction, reference, input.BLZ, input.UserID, input.SystemID, now),
		2,
	)
	if err != nil {
		Wipe(inner)
		return nil, err
	}

	number := uint16(3)
	for _, segment := range segments {
		inner, err = appendOutgoingSegment(inner, segment, number)
		if err != nil {
			Wipe(inner)
			return nil, err
		}
		number++
	}

	// Reserve room for the escaped PIN and TAN before they are written, so no
	// append reallocates a buffer that already holds them and leaves an
	// unwiped copy behind. Every byte may need an escape character.
	inner = reserve(inner, hnshaCapacity(input.PIN, input.TAN))
	innerCap := cap(inner)
	inner, err = appendHNSHA(inner, number, reference, input.PIN, input.TAN)
	if err != nil {
		Wipe(inner)
		return nil, err
	}
	if cap(inner) != innerCap {
		Wipe(inner)
		return nil, errBufferGrew
	}
	number++
	trailerNumber := number

	// HNVSK and HNHBS hold no secret; encode them first so body can be sized
	// exactly and never reallocates after the PIN is copied into it.
	hnvsk, err := appendOutgoingSegment(nil, newHNVSK(input.BLZ, input.UserID, input.SystemID, now), 998)
	if err != nil {
		Wipe(inner)
		return nil, err
	}
	hnhbs, err := appendOutgoingSegment(nil, newHNHBS(input.MessageNumber), trailerNumber)
	if err != nil {
		Wipe(inner)
		return nil, err
	}

	body := make([]byte, 0, len(hnvsk)+len(inner)+hnvsdOverhead+len(hnhbs))
	bodyCap := cap(body)
	body = append(body, hnvsk...)
	body, err = appendOutgoingSegment(body, newHNVSD(inner), 999)
	if err != nil {
		Wipe(inner)
		Wipe(body)
		return nil, err
	}
	body = append(body, hnhbs...)
	if cap(body) != bodyCap {
		Wipe(inner)
		Wipe(body)
		return nil, errBufferGrew
	}

	placeholder, err := appendOutgoingSegment(nil, newHNHBK(0, input.DialogID, input.MessageNumber), 1)
	if err != nil {
		Wipe(inner)
		Wipe(body)
		return nil, err
	}
	total := len(placeholder) + len(body)
	if total > MaxMessageSize {
		Wipe(inner)
		Wipe(body)
		return nil, errMessageTooLarge
	}

	header, err := appendOutgoingSegment(nil, newHNHBK(uint64(total), input.DialogID, input.MessageNumber), 1)
	if err != nil {
		Wipe(inner)
		Wipe(body)
		return nil, err
	}
	if len(header) != len(placeholder) {
		Wipe(inner)
		Wipe(body)
		return nil, errMessageTooLarge
	}

	message := make([]byte, 0, total)
	message = append(message, header...)
	message = append(message, body...)
	if len(message) != total {
		Wipe(inner)
		Wipe(body)
		Wipe(message)
		return nil, errMessageTooLarge
	}

	Wipe(inner)
	Wipe(body)
	return message, nil
}

// appendHNSHA appends the signature footer.
//
// HNSHA is built here rather than as a [Segment] so the PIN and TAN never enter
// a data element: their bytes are escaped straight into dst inside Use.
func appendHNSHA(
	dst []byte,
	number uint16,
	reference uint32,
	pin, tan *secret.Text,
) ([]byte, error) {
	if err := checkOutgoingType("HNSHA"); err != nil {
		return dst, err
	}

	dst = append(dst, "HNSHA:"...)
	dst = strconv.AppendUint(dst, uint64(number), 10)
	dst = append(dst, ":2+"...)
	dst = strconv.AppendUint(dst, uint64(reference), 10)
	dst = append(dst, '+') // empty validation result
	dst = append(dst, '+') // start of the user-defined signature group

	useErr := pin.Use(func(pinBytes []byte) error {
		dst = appendEscapedBytes(dst, pinBytes)
		if tan == nil {
			return nil
		}
		return tan.Use(func(tanBytes []byte) error {
			dst = append(dst, ':')
			dst = appendEscapedBytes(dst, tanBytes)
			return nil
		})
	})
	if useErr != nil {
		return dst, fmt.Errorf("fints: signature unavailable: %w", useErr)
	}
	return append(dst, '\''), nil
}

// hnvsdOverhead bounds HNVSD's bytes around its binary payload:
// "HNVSD:999:1+@" + up to 20 length digits + "@" + "'".
const hnvsdOverhead = 64

// hnshaCapacity bounds HNSHA's encoded size: its fixed parts and numbers, plus
// the PIN and TAN with every byte escaped.
func hnshaCapacity(pin, tan *secret.Text) int {
	size := 64 + 2*pin.Len()
	if tan != nil {
		size += 1 + 2*tan.Len()
	}
	return size
}

// reserve returns b with room for at least n more bytes. It copies b into a
// new array only before a secret is written, and wipes the old array.
func reserve(b []byte, n int) []byte {
	if cap(b)-len(b) >= n {
		return b
	}
	grown := make([]byte, len(b), len(b)+n)
	copy(grown, b)
	Wipe(b)
	return grown
}

// Wipe zeroes b. It is meant for the message returned by [BuildMessage] and
// for any buffer that held the PIN or TAN.
func Wipe(b []byte) {
	clear(b)
}
