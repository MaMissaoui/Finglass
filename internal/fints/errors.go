// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// ErrorKind is the closed classification of a [DialogError].
type ErrorKind uint8

// The dialog failure kinds.
//
// Mapping to banksync.GatewayErrorKind (banksync is deliberately not imported
// here, because it would make the adapter cyclic):
//
//	ErrorKindPinRejected    -> GatewayErrorPinRejected
//	ErrorKindAccountLocked  -> GatewayErrorBank
//	ErrorKindBankError      -> GatewayErrorBank
//	ErrorKindTanRejected    -> GatewayErrorBank
//	ErrorKindTransport      -> GatewayErrorHTTPStatus when HTTPStatus != 0,
//	                           else GatewayErrorNetwork
//	ErrorKindProtocol       -> GatewayErrorOther
//	ErrorKindCancelled      -> GatewayErrorCancelled
//	ErrorKindPollLimit      -> GatewayErrorOther
//	ErrorKindTouchdownLimit -> GatewayErrorOther
//	ErrorKindInvalidState   -> GatewayErrorOther
//
// Every kind maps to exactly one gateway kind, so the adapter can translate
// without inspecting text.
const (
	ErrorKindPinRejected ErrorKind = iota
	ErrorKindAccountLocked
	ErrorKindBankError
	ErrorKindTanRejected
	ErrorKindTransport
	ErrorKindProtocol
	ErrorKindCancelled
	ErrorKindPollLimit
	ErrorKindTouchdownLimit
	ErrorKindInvalidState
)

// String returns the kind's name. It never contains bank data.
func (k ErrorKind) String() string {
	switch k {
	case ErrorKindPinRejected:
		return "pin-rejected"
	case ErrorKindAccountLocked:
		return "account-locked"
	case ErrorKindBankError:
		return "bank-error"
	case ErrorKindTanRejected:
		return "tan-rejected"
	case ErrorKindTransport:
		return "transport"
	case ErrorKindProtocol:
		return "protocol"
	case ErrorKindCancelled:
		return "cancelled"
	case ErrorKindPollLimit:
		return "poll-limit"
	case ErrorKindTouchdownLimit:
		return "touchdown-limit"
	case ErrorKindInvalidState:
		return "invalid-state"
	default:
		return "unknown"
	}
}

// DialogError is a typed dialog failure.
//
// It carries the kind, the bank's return codes when there are any, and at most
// the Go type of a transport error. It never carries a PIN, a TAN, a challenge
// text, a bank message or any other response content. Error, String, GoString
// and Format print only the kind and the codes.
type DialogError struct {
	// Kind is the closed failure kind.
	Kind ErrorKind
	// Codes are the bank return codes that caused the failure, in response
	// order. They are empty for a transport, protocol, cancellation or limit
	// failure.
	Codes []string
	// HTTPStatus is the HTTP status of a transport failure whose error
	// implements [HTTPStatusError], else 0. It is a number only, never a body.
	HTTPStatus int

	cause     error
	causeType string
}

// Error implements error. It shows only the kind and the bank codes.
func (e *DialogError) Error() string {
	if e == nil {
		return "fints: <nil dialog error>"
	}
	var builder strings.Builder
	builder.WriteString("fints: ")
	switch e.Kind {
	case ErrorKindPinRejected:
		builder.WriteString("PIN rejected")
	case ErrorKindAccountLocked:
		builder.WriteString("account locked")
	case ErrorKindBankError:
		builder.WriteString("bank rejected the request")
	case ErrorKindTanRejected:
		builder.WriteString("TAN rejected")
	case ErrorKindTransport:
		builder.WriteString("transport failed")
		if e.causeType != "" {
			builder.WriteString(" (")
			builder.WriteString(e.causeType)
			builder.WriteString(")")
		}
		if e.HTTPStatus != 0 {
			builder.WriteString(" (HTTP ")
			builder.WriteString(strconv.Itoa(e.HTTPStatus))
			builder.WriteString(")")
		}
	case ErrorKindProtocol:
		builder.WriteString("protocol error")
	case ErrorKindCancelled:
		builder.WriteString("cancelled")
	case ErrorKindPollLimit:
		builder.WriteString("poll limit reached")
	case ErrorKindTouchdownLimit:
		builder.WriteString("touchdown limit reached")
	case ErrorKindInvalidState:
		builder.WriteString("invalid dialog state")
	default:
		builder.WriteString("dialog failed")
	}
	if len(e.Codes) > 0 {
		builder.WriteString(" (")
		builder.WriteString(strings.Join(e.Codes, ", "))
		builder.WriteString(")")
	}
	return builder.String()
}

// String implements fmt.Stringer and never prints response content.
func (e *DialogError) String() string {
	return e.Error()
}

// GoString implements fmt.GoStringer and never prints response content.
func (e *DialogError) GoString() string {
	return e.Error()
}

// Format implements fmt.Formatter and never prints response content.
func (e *DialogError) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(e.Error()))
}

// Unwrap returns the cancellation cause, when the error came from a context.
// Transport, protocol and limit errors deliberately keep no cause that could
// carry bank or transport text.
func (e *DialogError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// pinRejectedCodes are the return codes that always mean the PIN was rejected.
var pinRejectedCodes = map[string]bool{
	"9340": true,
	"9910": true,
	"9930": true,
	"9931": true,
	"9942": true,
}

func newCancelledError(cause error) *DialogError {
	if cause == nil {
		cause = errors.New("context cancelled")
	}
	return &DialogError{Kind: ErrorKindCancelled, cause: cause}
}

// HTTPStatusError is implemented by a transport error that carries an HTTP
// status code. The dialog keeps only the number, so the adapter can report
// GatewayErrorHTTPStatus; the error's text is never kept.
type HTTPStatusError interface {
	HTTPStatus() int
}

func newTransportError(cause error) *DialogError {
	errorType := ""
	status := 0
	if cause != nil {
		errorType = fmt.Sprintf("%T", cause)
		var statusError HTTPStatusError
		if errors.As(cause, &statusError) {
			status = statusError.HTTPStatus()
		}
	}
	return &DialogError{Kind: ErrorKindTransport, HTTPStatus: status, causeType: errorType}
}

func newProtocolError() *DialogError {
	return &DialogError{Kind: ErrorKindProtocol}
}

func newInvalidStateError() *DialogError {
	return &DialogError{Kind: ErrorKindInvalidState}
}

// codesMatching returns the response's return codes for which match reports
// true, in response order.
func codesMatching(response *Response, match func(ReturnCode) bool) []string {
	if response == nil {
		return nil
	}
	codes := make([]string, 0)
	for _, code := range response.Codes() {
		if match(code) {
			codes = appendUniqueStrings(codes, code.Code)
		}
	}
	return codes
}

// checkResponse maps the response's return codes to a DialogError.
//
// It is the non-init form: the PIN codes win anywhere, then 3938 locks the
// account, then any other 9xxx code is a BankError carrying only the codes.
func checkResponse(response *Response) *DialogError {
	if response == nil {
		return newProtocolError()
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return pinRejectedCodes[code.Code]
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindPinRejected, Codes: codes}
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return code.Code == "3938"
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindAccountLocked, Codes: codes}
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return code.IsError()
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindBankError, Codes: codes}
	}
	return nil
}

// checkInitResponse is the init form: any 9xxx before the dialog is open means
// the PIN was rejected, whatever its code. Account-locked 3938 still maps to
// AccountLocked.
func checkInitResponse(response *Response) *DialogError {
	if response == nil {
		return newProtocolError()
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return pinRejectedCodes[code.Code]
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindPinRejected, Codes: codes}
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return code.Code == "3938"
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindAccountLocked, Codes: codes}
	}
	if codes := codesMatching(response, func(code ReturnCode) bool {
		return code.IsError()
	}); len(codes) > 0 {
		return &DialogError{Kind: ErrorKindPinRejected, Codes: codes}
	}
	return nil
}
