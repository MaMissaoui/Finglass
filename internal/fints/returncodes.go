// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "strings"

// ReturnCodeKind is the closed classification of a [ReturnCode], mirroring
// fints-rs's ResponseCodeKind.
type ReturnCodeKind uint8

// The recognised return-code kinds. Every code has exactly one kind.
const (
	ReturnCodeKindUnknown ReturnCodeKind = iota
	ReturnCodeKindMessageReceived
	ReturnCodeKindOrderExecuted
	ReturnCodeKindTanRequired
	ReturnCodeKindDialogEnded
	ReturnCodeKindTanValid
	ReturnCodeKindTouchdown
	ReturnCodeKindPartialWarnings
	ReturnCodeKindScaExemption
	ReturnCodeKindAllowedSecurityFunctions
	ReturnCodeKindDecoupledInitiated
	ReturnCodeKindDecoupledPending
	ReturnCodeKindGeneralError
	ReturnCodeKindAuthenticationMissing
	ReturnCodeKindPartialErrors
	ReturnCodeKindUnexpectedInSync
	ReturnCodeKindDataElementMissing
	ReturnCodeKindPinWrong
	ReturnCodeKindDialogAborted
	ReturnCodeKindAccountLocked
	ReturnCodeKindOtherSuccess
	ReturnCodeKindOtherWarning
	ReturnCodeKindOtherError
)

// String returns the kind's name. It never contains bank data.
func (k ReturnCodeKind) String() string {
	switch k {
	case ReturnCodeKindMessageReceived:
		return "message-received"
	case ReturnCodeKindOrderExecuted:
		return "order-executed"
	case ReturnCodeKindTanRequired:
		return "tan-required"
	case ReturnCodeKindDialogEnded:
		return "dialog-ended"
	case ReturnCodeKindTanValid:
		return "tan-valid"
	case ReturnCodeKindTouchdown:
		return "touchdown"
	case ReturnCodeKindPartialWarnings:
		return "partial-warnings"
	case ReturnCodeKindScaExemption:
		return "sca-exemption"
	case ReturnCodeKindAllowedSecurityFunctions:
		return "allowed-security-functions"
	case ReturnCodeKindDecoupledInitiated:
		return "decoupled-initiated"
	case ReturnCodeKindDecoupledPending:
		return "decoupled-pending"
	case ReturnCodeKindGeneralError:
		return "general-error"
	case ReturnCodeKindAuthenticationMissing:
		return "authentication-missing"
	case ReturnCodeKindPartialErrors:
		return "partial-errors"
	case ReturnCodeKindUnexpectedInSync:
		return "unexpected-in-sync"
	case ReturnCodeKindDataElementMissing:
		return "data-element-missing"
	case ReturnCodeKindPinWrong:
		return "pin-wrong"
	case ReturnCodeKindDialogAborted:
		return "dialog-aborted"
	case ReturnCodeKindAccountLocked:
		return "account-locked"
	case ReturnCodeKindOtherSuccess:
		return "other-success"
	case ReturnCodeKindOtherWarning:
		return "other-warning"
	case ReturnCodeKindOtherError:
		return "other-error"
	default:
		return "unknown"
	}
}

// Kind classifies the code like fints-rs's ResponseCodeKind::from_code.
//
// The classification is independent of the text and parameters, so callers
// can inspect it without touching bank text.
func (r ReturnCode) Kind() ReturnCodeKind {
	switch r.Code {
	case "0010":
		return ReturnCodeKindMessageReceived
	case "0020":
		return ReturnCodeKindOrderExecuted
	case "0030":
		return ReturnCodeKindTanRequired
	case "0100":
		return ReturnCodeKindDialogEnded
	case "0900":
		return ReturnCodeKindTanValid
	case "3040":
		return ReturnCodeKindTouchdown
	case "3060":
		return ReturnCodeKindPartialWarnings
	case "3076":
		return ReturnCodeKindScaExemption
	case "3920":
		return ReturnCodeKindAllowedSecurityFunctions
	case "3955":
		return ReturnCodeKindDecoupledInitiated
	case "3956":
		return ReturnCodeKindDecoupledPending
	case "9010":
		return ReturnCodeKindGeneralError
	case "9040":
		return ReturnCodeKindAuthenticationMissing
	case "9050":
		return ReturnCodeKindPartialErrors
	case "9110":
		return ReturnCodeKindUnexpectedInSync
	case "9160":
		return ReturnCodeKindDataElementMissing
	case "9340":
		return ReturnCodeKindPinWrong
	case "9800":
		return ReturnCodeKindDialogAborted
	case "9942":
		return ReturnCodeKindAccountLocked
	}
	switch {
	case strings.HasPrefix(r.Code, "0"):
		return ReturnCodeKindOtherSuccess
	case strings.HasPrefix(r.Code, "3"):
		return ReturnCodeKindOtherWarning
	case strings.HasPrefix(r.Code, "9"):
		return ReturnCodeKindOtherError
	default:
		return ReturnCodeKindUnknown
	}
}

// Touchdown returns the first parameter of a 3040 code, the pagination point.
//
// It reports false when the code is not a 3040 or has no parameter.
func (r ReturnCode) Touchdown() (string, bool) {
	if r.Kind() != ReturnCodeKindTouchdown || len(r.Params) == 0 {
		return "", false
	}
	return r.Params[0], true
}

// AllowedSecurityFunctions returns the security functions of a 3920 code.
//
// It returns nil for every other code. The result is a copy.
func (r ReturnCode) AllowedSecurityFunctions() []string {
	if r.Kind() != ReturnCodeKindAllowedSecurityFunctions || len(r.Params) == 0 {
		return nil
	}
	functions := make([]string, len(r.Params))
	copy(functions, r.Params)
	return functions
}

// Codes returns every return code, HIRMG (global) first and HIRMS (segment)
// afterwards, each in wire order.
func (r Response) Codes() []ReturnCode {
	codes := make([]ReturnCode, 0, len(r.GlobalCodes)+len(r.SegmentCodes))
	codes = append(codes, r.GlobalCodes...)
	codes = append(codes, r.SegmentCodes...)
	return codes
}

// FirstError returns the first error code in [Response.Codes] order.
func (r Response) FirstError() (ReturnCode, bool) {
	for _, code := range r.Codes() {
		if code.IsError() {
			return code, true
		}
	}
	return ReturnCode{}, false
}

// HasKind reports whether any return code has the given kind.
func (r Response) HasKind(kind ReturnCodeKind) bool {
	for _, code := range r.Codes() {
		if code.Kind() == kind {
			return true
		}
	}
	return false
}

// Touchdown returns the first touchdown point, searching the segment codes
// before the global codes, as fints-rs does.
func (r Response) Touchdown() (string, bool) {
	for _, code := range r.SegmentCodes {
		if point, ok := code.Touchdown(); ok {
			return point, true
		}
	}
	for _, code := range r.GlobalCodes {
		if point, ok := code.Touchdown(); ok {
			return point, true
		}
	}
	return "", false
}

// AllowedSecurityFunctions returns the security functions from the first 3920
// code, searching the segment codes before the global codes, as fints-rs does.
func (r Response) AllowedSecurityFunctions() []string {
	for _, code := range r.SegmentCodes {
		if functions := code.AllowedSecurityFunctions(); functions != nil {
			return functions
		}
	}
	for _, code := range r.GlobalCodes {
		if functions := code.AllowedSecurityFunctions(); functions != nil {
			return functions
		}
	}
	return nil
}
