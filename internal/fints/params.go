// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strings"
)

// SecurityFunctionPINOnly is the PIN-only security function fints-rs uses
// before a TAN method is selected.
const SecurityFunctionPINOnly = "999"

// BankParams is the bank and user parameter state discovered during sync and
// dialog initialisation.
//
// It stores only read-only values: the BPD/UPD versions, the TAN methods and
// media parameters, the HIPINS operation rules, the highest segment versions,
// the accepted camt formats, the accounts from HIUPD/HISPA and the allowed
// security functions from 3920. Bank text (TAN method names, account owner and
// product names) stays unexported and is masked by [BankParams.String].
//
// The zero value is not usable; build one with [NewBankParams].
type BankParams struct {
	// BPDVersion is the bank parameter data version from HIBPA.
	BPDVersion uint16
	// UPDVersion is the user parameter data version from HIUPA.
	UPDVersion uint16
	// SystemID is the system id assigned by the bank in HISYN.
	SystemID string
	// TANMethods are the TAN methods advertised in HITANS, in wire order.
	TANMethods []TanMethod
	// OperationTANRequired maps a business segment type to whether HIPINS says
	// it needs a TAN.
	OperationTANRequired map[string]bool
	// SegmentVersions maps a business segment to the highest BPD parameter
	// version seen for it.
	SegmentVersions map[BusinessSegment]uint16
	// CamtFormats are the accepted camt URNs from HICAZS, in wire order.
	CamtFormats []string
	// Accounts are the accounts from HIUPD and HISPA, in wire order.
	Accounts []Account
	// AllowedSecurityFunctions are the security functions from the last 3920
	// code, in wire order.
	AllowedSecurityFunctions []string
	// SelectedSecurityFunction is the security function used for HNSHK.
	SelectedSecurityFunction string
	// PreferredSecurityFunction is the security function the caller remembered.
	// [BankParams.SelectSecurityFunction] uses it when the bank allows it.
	PreferredSecurityFunction string
	// SelectedTANMedium is the TAN medium name to send in HKTAN, when any.
	SelectedTANMedium string

	bpdSegments []ParsedSegment
	updSegments []ParsedSegment
}

// NewBankParams builds empty parameters with the PIN-only security function
// pre-selected, as fints-rs does.
func NewBankParams() *BankParams {
	return &BankParams{
		OperationTANRequired:     make(map[string]bool),
		SegmentVersions:          make(map[BusinessSegment]uint16),
		SelectedSecurityFunction: SecurityFunctionPINOnly,
	}
}

// IngestResponse merges one bank response into the parameters. It ingests
// every response, not only the sync response, as fints-rs does.
func (p *BankParams) IngestResponse(response *Response) {
	if p == nil || response == nil {
		return
	}
	if p.SelectedSecurityFunction == "" {
		p.SelectedSecurityFunction = SecurityFunctionPINOnly
	}
	if p.OperationTANRequired == nil {
		p.OperationTANRequired = make(map[string]bool)
	}
	if p.SegmentVersions == nil {
		p.SegmentVersions = make(map[BusinessSegment]uint16)
	}

	// A response that carries HITANS, HIUPD or a business parameter segment
	// replaces what earlier responses gave for that segment type instead of
	// appending to it, so a sync followed by an init lists nothing twice.
	// Accounts are merged instead (mergeAccount), because HIUPD and the HISPA
	// answer to HKSPA arrive in different responses. (fints-rs appends; this is
	// deliberate.)
	var sawTANMethods, sawUPD bool
	sawParameters := make(map[string]bool)
	for _, segment := range response.Segments {
		segmentType := segment.Type()
		if segmentType == "HITANS" && !sawTANMethods {
			p.TANMethods = nil
			sawTANMethods = true
		}
		if segmentType == "HIUPD" && !sawUPD {
			p.updSegments = nil
			sawUPD = true
		}
		if _, ok := businessForParameterType(segmentType); ok && !sawParameters[segmentType] {
			p.bpdSegments = dropSegmentType(p.bpdSegments, segmentType)
			sawParameters[segmentType] = true
		}
		switch segmentType {
		case "HIBPA":
			p.BPDVersion = BPDVersionFromHIBPA(segment)
		case "HIUPA":
			p.UPDVersion = UPDVersionFromHIUPA(segment)
		case "HITANS":
			p.TANMethods = append(p.TANMethods, ParseTanMethods(segment)...)
		case "HIPINS":
			for operation, required := range ParseHIPINS(segment) {
				p.OperationTANRequired[operation] = required
			}
		case "HICAZS":
			p.CamtFormats = appendUniqueStrings(p.CamtFormats, ParseCamtFormats(segment)...)
		case "HIUPD":
			p.updSegments = append(p.updSegments, segment)
			if account, ok := ParseHIUPD(segment); ok {
				p.Accounts = mergeAccount(p.Accounts, account)
			}
		case "HISPA":
			for _, account := range ParseHISPA(segment) {
				p.Accounts = mergeAccount(p.Accounts, account)
			}
		case "HISYN":
			if systemID := SystemIDFromHISYN(segment); systemID != "" {
				p.SystemID = systemID
			}
		}

		if business, ok := businessForParameterType(segmentType); ok {
			p.bpdSegments = append(p.bpdSegments, segment)
			if version := segment.Version(); version > p.SegmentVersions[business] {
				p.SegmentVersions[business] = version
			}
		}
	}

	if allowed := response.AllowedSecurityFunctions(); len(allowed) > 0 {
		p.AllowedSecurityFunctions = append([]string(nil), allowed...)
	}
}

// mergeAccount adds account unless the same account is already listed (same
// IBAN, or same account number, sub-account and BLZ); then it only fills the
// listed entry's empty fields. HIUPD and HISPA describe the same accounts, so a
// plain append would list each account twice.
func mergeAccount(accounts []Account, account Account) []Account {
	for index := range accounts {
		existing := &accounts[index]
		sameIBAN := existing.IBAN != "" && existing.IBAN == account.IBAN
		sameNumber := existing.AccountNumber != "" &&
			existing.AccountNumber == account.AccountNumber &&
			existing.SubAccount == account.SubAccount &&
			existing.BLZ == account.BLZ
		if !sameIBAN && !sameNumber {
			continue
		}
		fillEmpty(&existing.IBAN, account.IBAN)
		fillEmpty(&existing.BIC, account.BIC)
		fillEmpty(&existing.AccountNumber, account.AccountNumber)
		fillEmpty(&existing.SubAccount, account.SubAccount)
		fillEmpty(&existing.BLZ, account.BLZ)
		fillEmpty(&existing.Currency, account.Currency)
		fillEmpty(&existing.AccountType, account.AccountType)
		fillEmpty(&existing.owner, account.owner)
		fillEmpty(&existing.productName, account.productName)
		if len(existing.AllowedBusinessTransactions) == 0 {
			existing.AllowedBusinessTransactions = account.AllowedBusinessTransactions
		}
		return accounts
	}
	return append(accounts, account)
}

func dropSegmentType(segments []ParsedSegment, segmentType string) []ParsedSegment {
	kept := segments[:0:0]
	for _, segment := range segments {
		if segment.Type() != segmentType {
			kept = append(kept, segment)
		}
	}
	return kept
}

func fillEmpty(target *string, value string) {
	if *target == "" {
		*target = value
	}
}

// businessForParameterType maps a BPD parameter segment type back to its
// business segment.
func businessForParameterType(parameterType string) (BusinessSegment, bool) {
	switch parameterType {
	case "HISALS":
		return BusinessHKSAL, true
	case "HIKAZS":
		return BusinessHKKAZ, true
	case "HICAZS":
		return BusinessHKCAZ, true
	case "HISPAS":
		return BusinessHKSPA, true
	case "HITABS":
		return BusinessHKTAB, true
	case "HITANS":
		return BusinessHKTAN, true
	default:
		return "", false
	}
}

// SelectSecurityFunction picks the security function for HNSHK.
//
// It prefers the allowed function the caller remembered, then the first
// allowed decoupled method, then the first allowed function, as this slice's
// spec requires. An empty 3920 list leaves the selection unchanged.
func (p *BankParams) SelectSecurityFunction() {
	if p == nil || len(p.AllowedSecurityFunctions) == 0 {
		return
	}
	allowed := p.AllowedSecurityFunctions
	if p.PreferredSecurityFunction != "" && containsString(allowed, p.PreferredSecurityFunction) {
		p.SelectedSecurityFunction = p.PreferredSecurityFunction
		return
	}
	for _, securityFunction := range allowed {
		if method, ok := p.methodForSecurityFunction(securityFunction); ok && method.IsDecoupled() {
			p.SelectedSecurityFunction = securityFunction
			return
		}
	}
	p.SelectedSecurityFunction = allowed[0]
}

// NeedsTAN reports whether HIPINS says the business segment needs a TAN. A
// business segment the bank did not mention defaults to true, which is the safe
// answer, as in fints-rs.
func (p *BankParams) NeedsTAN(business BusinessSegment) bool {
	if p == nil {
		return true
	}
	if required, ok := p.OperationTANRequired[string(business)]; ok {
		return required
	}
	return true
}

// HKTANVersion returns the HITANS version of the selected TAN method, or 7 when
// no method matches.
func (p *BankParams) HKTANVersion() uint16 {
	if p == nil {
		return 7
	}
	if method, ok := p.selectedMethod(); ok {
		return method.HKTANVersion
	}
	return 7
}

// SupportedVersion returns the highest BPD version of a business segment not
// above maxVersion, or maxVersion when the bank offers none. Only the closed
// set of business segments is accepted; an unknown value returns 0.
func (p *BankParams) SupportedVersion(business BusinessSegment, maxVersion uint16) uint16 {
	if _, ok := business.parameterSegmentType(); !ok {
		return 0
	}
	if p != nil {
		if version := HighestSegmentVersion(p.bpdSegments, business, maxVersion); version != 0 {
			return version
		}
		if version := p.SegmentVersions[business]; version != 0 {
			if version > maxVersion {
				return maxVersion
			}
			return version
		}
	}
	return maxVersion
}

// HighestVersion returns the highest BPD version seen for a business segment,
// or 0 when the bank offered none.
func (p *BankParams) HighestVersion(business BusinessSegment) uint16 {
	if p == nil {
		return 0
	}
	return p.SegmentVersions[business]
}

// SelectedMethod returns the TAN method for the selected security function.
func (p *BankParams) SelectedMethod() (TanMethod, bool) {
	if p == nil {
		return TanMethod{}, false
	}
	return p.selectedMethod()
}

// IsDecoupled reports whether the selected TAN method is decoupled.
func (p *BankParams) IsDecoupled() bool {
	method, ok := p.SelectedMethod()
	return ok && method.IsDecoupled()
}

// NeedsTANMedium reports whether the selected method requires a medium name.
func (p *BankParams) NeedsTANMedium() bool {
	method, ok := p.SelectedMethod()
	return ok && method.NeedsTanMedium()
}

func (p *BankParams) selectedMethod() (TanMethod, bool) {
	return p.methodForSecurityFunction(p.SelectedSecurityFunction)
}

// methodForSecurityFunction returns the method from the highest HITANS
// version this client supports. A bank lists a method once per HITANS version
// (HVB sends v1, v4, v5 and v6); the first, oldest one made HKTAN go out as v1
// (HVB 9075/9050).
func (p *BankParams) methodForSecurityFunction(securityFunction string) (TanMethod, bool) {
	best := TanMethod{}
	found := false
	for _, method := range p.TANMethods {
		if method.SecurityFunction != securityFunction || method.HKTANVersion > maxHKTANVersion {
			continue
		}
		if !found || method.HKTANVersion > best.HKTANVersion {
			best = method
			found = true
		}
	}
	return best, found
}

// maxHKTANVersion is the highest HKTAN version this client builds.
const maxHKTANVersion = 7

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func appendUniqueStrings(dst []string, values ...string) []string {
	for _, value := range values {
		if !containsString(dst, value) {
			dst = append(dst, value)
		}
	}
	return dst
}

// String returns only versions, counts and security-function codes. It never
// prints a system id, an account, a TAN method name or any other bank text.
func (p BankParams) String() string {
	return fmt.Sprintf(
		"fints.BankParams{bpd:%d upd:%d systemID:%d tanMethods:%d accounts:%d camtFormats:%d allowed:%s selected:%s preferred:%s medium:%t operations:%d versions:%d}",
		p.BPDVersion, p.UPDVersion, len(p.SystemID), len(p.TANMethods),
		len(p.Accounts), len(p.CamtFormats),
		strings.Join(p.AllowedSecurityFunctions, ","),
		p.SelectedSecurityFunction, p.PreferredSecurityFunction,
		p.SelectedTANMedium != "", len(p.OperationTANRequired),
		len(p.SegmentVersions),
	)
}

// GoString returns only versions, counts and codes, never bank text.
func (p BankParams) GoString() string {
	return p.String()
}

// Format writes only versions, counts and codes, for every fmt verb.
func (p BankParams) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(p.String()))
}
