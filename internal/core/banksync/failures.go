package banksync

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// Stable error codes the UI can branch on. They are Rust's BankingError codes.
const (
	// CodeOffline means the bank connection is in offline mode.
	CodeOffline = "OFFLINE"
	// CodeNoServer means the connection has no FinTS server URL.
	CodeNoServer = "NO_SERVER"
	// CodeNoUser means the connection has no online-banking user id.
	CodeNoUser = "NO_USER"
	// CodeCancelled means the user cancelled a prompt.
	CodeCancelled = "CANCELLED"
	// CodePinNotSaved means a PIN could not be stored.
	CodePinNotSaved = "PIN_NOT_SAVED"
	// CodePinSaved means a PIN was stored.
	CodePinSaved = "PIN_SAVED"
	// CodePinForgotten means the remembered PIN was forgotten.
	CodePinForgotten = "PIN_FORGOTTEN"
	// CodeSyncFailed means an internal operation failed during a sync.
	CodeSyncFailed = "SYNC_FAILED"
	// CodeFintsError means the bank's dialog failed.
	CodeFintsError = "FINTS_ERROR"
	// CodeNotFound means a connection or account was not found.
	CodeNotFound = "NOT_FOUND"
	// CodeAddBank is the audit action for adding a bank access.
	CodeAddBank = "ADD_BANK"
	// CodeChangeBankCode is the audit action for changing a bank code.
	CodeChangeBankCode = "CHANGE_BANK_CODE"
	// CodeDeleteBankAccess is the audit action for deleting a bank access.
	CodeDeleteBankAccess = "DELETE_BANK_ACCESS"
	// CodeResetFintsLogin is the audit action for resetting a login.
	CodeResetFintsLogin = "RESET_FINTS_LOGIN"
	// CodeSyncBank is the audit action for syncing a bank.
	CodeSyncBank = "SYNC_BANK"
)

// GatewayErrorKind names a Rust GatewayError variant.
type GatewayErrorKind int

// The gateway failure kinds.
const (
	// GatewayErrorNetwork means the server could not be reached.
	GatewayErrorNetwork GatewayErrorKind = iota
	// GatewayErrorHTTPStatus means the server answered with an HTTP status.
	GatewayErrorHTTPStatus
	// GatewayErrorBank means the bank rejected the request with return codes.
	GatewayErrorBank
	// GatewayErrorPinRejected means the bank rejected the PIN.
	GatewayErrorPinRejected
	// GatewayErrorCancelled means the user cancelled a prompt.
	GatewayErrorCancelled
	// GatewayErrorOther is any other failure; its text is untrusted bank or
	// transport text.
	GatewayErrorOther
)

// GatewayError is a failed FinTS dialog, as the M5b adapter reports it.
//
// The zero value is a network failure; build one with the New functions.
type GatewayError struct {
	kind       GatewayErrorKind
	httpStatus uint16
	codes      []string
	text       string
}

// NewGatewayNetworkError builds the "server could not be reached" failure.
func NewGatewayNetworkError() GatewayError {
	return GatewayError{kind: GatewayErrorNetwork}
}

// NewGatewayHTTPStatusError builds a failure carrying the HTTP status.
func NewGatewayHTTPStatusError(status uint16) GatewayError {
	return GatewayError{kind: GatewayErrorHTTPStatus, httpStatus: status}
}

// NewGatewayBankError builds a failure carrying the bank's return codes. It
// never carries a bank message.
func NewGatewayBankError(codes []string) GatewayError {
	return GatewayError{kind: GatewayErrorBank, codes: append([]string(nil), codes...)}
}

// NewGatewayPinRejectedError builds the "bank rejected the PIN" failure.
func NewGatewayPinRejectedError() GatewayError {
	return GatewayError{kind: GatewayErrorPinRejected}
}

// NewGatewayCancelledError builds the "user cancelled a prompt" failure.
func NewGatewayCancelledError() GatewayError {
	return GatewayError{kind: GatewayErrorCancelled}
}

// NewGatewayOtherError builds a failure from untrusted bank or transport text.
func NewGatewayOtherError(text string) GatewayError {
	return GatewayError{kind: GatewayErrorOther, text: text}
}

// Kind returns the Rust variant this error represents.
func (e GatewayError) Kind() GatewayErrorKind {
	return e.kind
}

// HTTPStatus returns the HTTP status the server answered with, when this is an
// HTTP failure.
func (e GatewayError) HTTPStatus() (uint16, bool) {
	if e.kind != GatewayErrorHTTPStatus {
		return 0, false
	}
	return e.httpStatus, true
}

// IsNetworkFailure reports whether this is a connectivity problem.
func (e GatewayError) IsNetworkFailure() bool {
	switch e.kind {
	case GatewayErrorNetwork:
		return true
	case GatewayErrorOther:
		return isNetworkText(e.text)
	default:
		return false
	}
}

// Codes returns the bank's return codes, when it sent any.
func (e GatewayError) Codes() []string {
	if e.kind != GatewayErrorBank {
		return []string{}
	}
	return append([]string{}, e.codes...)
}

// Describe returns a short message for a failed dialog, as FinDesk builds it.
//
// Text from the error and the bank messages is cut at the first line break,
// capped at 300 characters and masked with [MaskAccountNumbers]. A base64
// failure and an HTTP status are transport diagnoses; a bank message outranks
// the generic text.
func (e GatewayError) Describe(language i18n.Language, bankMessages []string) string {
	if e.kind == GatewayErrorOther && strings.Contains(strings.ToLower(e.text), "base64") {
		if line, ok := cleanLine(e.text); ok {
			return i18n.Format(language, i18n.ErrorServerNotFints, i18n.Str(MaskAccountNumbers(line)))
		}
	}

	if status, ok := e.HTTPStatus(); ok {
		return i18n.Format(language, i18n.ErrorServerHttpStatus, i18n.Int(int64(status)))
	}

	for index := len(bankMessages) - 1; index >= 0; index-- {
		if line, ok := cleanLine(bankMessages[index]); ok {
			return MaskAccountNumbers(line)
		}
	}

	switch e.kind {
	case GatewayErrorNetwork:
		return e.NetworkMessage(language)
	case GatewayErrorPinRejected:
		return i18n.Text(language, i18n.ErrorBankRejectedPin)
	case GatewayErrorCancelled:
		return i18n.Text(language, i18n.ErrorCancelledByUser)
	case GatewayErrorBank:
		if len(e.codes) > 0 {
			return fmt.Sprintf("%s (%s)", i18n.Text(language, i18n.ErrorBankRejected), strings.Join(e.codes, ", "))
		}
		return i18n.Text(language, i18n.ErrorBankRejected)
	case GatewayErrorOther:
		if line, ok := cleanLine(e.text); ok {
			return MaskAccountNumbers(line)
		}
		return i18n.Text(language, i18n.ErrorDialogFailed)
	default:
		return i18n.Text(language, i18n.ErrorDialogFailed)
	}
}

// NetworkMessage returns ErrorBankUnreachable plus the innermost first line, for
// example "(Connection reset)".
func (e GatewayError) NetworkMessage(language i18n.Language) string {
	base := i18n.Text(language, i18n.ErrorBankUnreachable)
	if e.kind == GatewayErrorOther {
		if line, ok := cleanLine(e.text); ok {
			return fmt.Sprintf("%s (%s)", base, MaskAccountNumbers(line))
		}
	}
	return base
}

// Error implements error without ever printing the untrusted text.
func (e GatewayError) Error() string {
	switch e.kind {
	case GatewayErrorNetwork:
		return "bank network failure"
	case GatewayErrorHTTPStatus:
		return fmt.Sprintf("bank answered with HTTP status %d", e.httpStatus)
	case GatewayErrorBank:
		if len(e.codes) > 0 {
			return fmt.Sprintf("bank rejected the request (%s)", strings.Join(e.codes, ", "))
		}
		return "bank rejected the request"
	case GatewayErrorPinRejected:
		return "bank rejected the PIN"
	case GatewayErrorCancelled:
		return "cancelled"
	default:
		return "bank dialog failed"
	}
}

// String implements fmt.Stringer and never prints the untrusted text.
func (e GatewayError) String() string {
	return e.Error()
}

// GoString implements fmt.GoStringer and never prints the untrusted text.
func (e GatewayError) GoString() string {
	return e.Error()
}

// Format implements fmt.Formatter and never prints the untrusted text.
func (e GatewayError) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(e.Error()))
}

// BankingError is the user-visible failure of a service call.
type BankingError struct {
	// Code is a stable code the UI can branch on (OFFLINE, FINTS_ERROR, ...).
	Code string
	// Message is shown to the user. Never contains a PIN, TAN or full account
	// number.
	Message string
}

// NewBankingError builds an error from a code and a message.
//
// Text that comes from a bank is masked with [MaskAccountNumbers] before it is
// stored, so only the last four characters of account numbers remain.
func NewBankingError(code, message string) BankingError {
	return BankingError{Code: code, Message: MaskAccountNumbers(message)}
}

// Error implements error. It never contains the code.
func (e BankingError) Error() string {
	return e.Message
}

// NETWORK_MARKERS are the transport texts that identify a connectivity problem.
var networkMarkers = []string{
	"sending message to hbci server",
	"senden der hbci-nachricht",
	"connection refused",
	"connect timed out",
	"connection timed out",
	"connection reset",
	"network is unreachable",
	"no route to host",
	"unknownhostexception",
	"connectexception",
	"sockettimeoutexception",
	"sslexception",
	"handshakeexception",
}

func isNetworkText(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range networkMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// cleanLine returns the first meaningful line, capped at 300 characters.
func cleanLine(text string) (string, bool) {
	first := text
	if index := strings.IndexByte(first, '\n'); index >= 0 {
		first = first[:index]
	}
	first = strings.TrimSuffix(first, "\r")
	line := strings.TrimSpace(first)
	if line == "" || strings.HasPrefix(line, "at ") || strings.HasPrefix(line, "\tat ") ||
		strings.HasPrefix(line, "Caused by") {
		return "", false
	}
	return truncateRunes(line, 300), true
}

// truncateRunes keeps at most limit runes, like Rust's chars().take(limit).
func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}
