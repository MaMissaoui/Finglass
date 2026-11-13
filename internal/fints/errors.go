// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"errors"
	"fmt"
)

// ParseError describes a malformed message or segment.
//
// It carries the segment type, the segment number and the byte offset, and a
// fixed reason. It never carries a data element's content, so it can be
// returned and logged without exposing bank data.
type ParseError struct {
	// Type is the segment type, when it could be read before the error.
	Type string
	// Number is the segment number, when it could be read before the error.
	Number uint16
	// Offset is the byte offset in the parsed buffer where the error occurred.
	Offset int
	// Reason is a fixed, content-free description.
	Reason string
}

// Error implements error. It never contains a data element's content.
func (e *ParseError) Error() string {
	return fmt.Sprintf(
		"fints: parse error at byte %d in segment %q number %d: %s",
		e.Offset, e.Type, e.Number, e.Reason,
	)
}

// Fixed parse reasons. None of them embeds input.
const (
	reasonEmptyMessage     = "empty message"
	reasonNoSegments       = "message contains no segment"
	reasonUnterminated     = "segment is not terminated"
	reasonUnterminatedEsc  = "unterminated escape sequence"
	reasonUnterminatedBin  = "unterminated binary length prefix"
	reasonInvalidBinLength = "invalid binary length prefix"
	reasonBinaryPastEnd    = "binary data extends past the end of the message"
	reasonTooLarge         = "message exceeds the size limit"
	reasonNestingTooDeep   = "HNVSD nesting is too deep"
	reasonMissingHNVSDData = "HNVSD segment has no binary payload"
)

// Fixed encoding errors. None of them embeds a value, so a secret can never
// reach an error string.
var (
	errMissingPIN              = errors.New("fints: PIN is required")
	errMissingSecurityFunction = errors.New("fints: security function is required")
	errTextNotLatin1           = errors.New("fints: text contains a character outside ISO-8859-1")
	errMessageTooLarge         = errors.New("fints: message exceeds the size limit")
	errBufferGrew              = errors.New("fints: a buffer holding the PIN would have been reallocated")
	errTooManySegments         = errors.New("fints: too many segments")
	errEmptyIBAN               = errors.New("fints: IBAN must not be empty")
	errEmptyBIC                = errors.New("fints: BIC must not be empty")
	errUnknownTanReference     = errors.New("fints: unknown HKTAN segment reference")
	errInvalidDate             = errors.New("fints: invalid date")
	errInvalidMaxEntries       = errors.New("fints: max entries must not be negative")
	errUnknownSegmentShape     = errors.New("fints: segment has no header")
)
