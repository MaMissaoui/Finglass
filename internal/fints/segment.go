// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strconv"
	"time"
)

// Date is a calendar date in the FinTS wire format, YYYYMMDD.
type Date struct {
	Year  int
	Month int
	Day   int
}

// String returns the wire form, YYYYMMDD.
func (d Date) String() string {
	return fmt.Sprintf("%04d%02d%02d", d.Year, d.Month, d.Day)
}

func (d Date) valid() bool {
	return d.Year >= 1 && d.Year <= 9999 &&
		d.Month >= 1 && d.Month <= 12 &&
		d.Day >= 1 && d.Day <= 31
}

// TanSegmentRef is the closed set of business segments that HKTAN process 4
// may reference. There is deliberately no free segment-type string.
type TanSegmentRef string

// The only segments HKTAN process 4 may reference.
const (
	TanRefHKIDN TanSegmentRef = "HKIDN"
	TanRefHKSAL TanSegmentRef = "HKSAL"
	TanRefHKKAZ TanSegmentRef = "HKKAZ"
	TanRefHKCAZ TanSegmentRef = "HKCAZ"
	TanRefHKSPA TanSegmentRef = "HKSPA"
	TanRefHKTAB TanSegmentRef = "HKTAB"
)

func (r TanSegmentRef) valid() bool {
	switch r {
	case TanRefHKIDN, TanRefHKSAL, TanRefHKKAZ, TanRefHKCAZ, TanRefHKSPA, TanRefHKTAB:
		return true
	default:
		return false
	}
}

// Segment is one outgoing segment.
//
// Its representation is unexported and the only way to obtain a Segment is
// through the constructors in this package. The encoder checks the segment
// type against a fixed allowlist again, so a Segment cannot carry an order
// segment even if a constructor were changed by mistake.
type Segment struct {
	degs []DEG
	err  error
}

// String returns only the segment type and the number of data element groups,
// never their content.
func (s Segment) String() string {
	return fmt.Sprintf("fints.Segment{type:%s degs:%d}", s.segmentType(), len(s.degs))
}

// GoString returns only the segment type and the number of data element
// groups.
func (s Segment) GoString() string {
	return s.String()
}

// Format writes only the segment type and the number of data element groups,
// for every fmt verb.
func (s Segment) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

func (s Segment) segmentType() string {
	if len(s.degs) == 0 {
		return ""
	}
	return s.degs[0].Text(0)
}

// checkOutgoingType is the encoder's allowlist. It is also the internal test
// hook that proves every order segment is refused.
func checkOutgoingType(segmentType string) error {
	switch segmentType {
	case "HNHBK", "HNHBS", "HNVSK", "HNVSD", "HNSHK", "HNSHA",
		"HKIDN", "HKVVB", "HKSYN", "HKEND", "HKTAN", "HKTAB",
		"HKSPA", "HKSAL", "HKKAZ", "HKCAZ":
		return nil
	default:
		return fmt.Errorf("fints: outgoing segment type %q is not allowed", segmentType)
	}
}

// validate reports why a constructor refused its arguments, or whether the
// segment type is outside the allowlist.
func (s Segment) validate() error {
	if s.err != nil {
		return s.err
	}
	return checkOutgoingType(s.segmentType())
}

// appendOutgoingSegment validates and appends one outgoing segment with the
// given segment number.
func appendOutgoingSegment(dst []byte, segment Segment, number uint16) ([]byte, error) {
	if err := segment.validate(); err != nil {
		return dst, err
	}
	if len(segment.degs) == 0 {
		return dst, errUnknownSegmentShape
	}
	groups := make([]DEG, len(segment.degs))
	copy(groups, segment.degs)
	header := make(DEG, len(groups[0]))
	copy(header, groups[0])
	if len(header) >= 2 {
		header[1] = numberElement(number)
	}
	groups[0] = header
	return appendSegmentBytes(dst, groups)
}

func newSegment(segmentType string, version uint16, body ...DEG) Segment {
	groups := make([]DEG, 0, 1+len(body))
	groups = append(groups, segmentHeader(segmentType, 0, version))
	groups = append(groups, body...)
	return Segment{degs: groups}
}

// ktiGroup builds a KTI (Kontoverbindung International) group using the
// IBAN/BIC form: IBAN:BIC.
func ktiGroup(iban, bic string) (DEG, error) {
	if iban == "" {
		return nil, errEmptyIBAN
	}
	if bic == "" {
		return nil, errEmptyBIC
	}
	return DEG{textElement(iban), textElement(bic)}, nil
}

// nationalAccountGroup is the pre-version-6 account form used by HKSAL and
// HKKAZ: account number : sub-account : bank identifier : BIC.
func nationalAccountGroup(iban, bic string) DEG {
	return DEG{
		valueElement(iban),
		emptyElement(),
		textElement("280"),
		valueElement(bic),
	}
}

func dateElement(date Date) DataElement {
	return textElement(date.String())
}

// NewHKIDN builds HKIDN:2, dialog identification.
//
// customerID is the Kunden-ID that the bank knows; the envelope's user id is
// only used in the signature and encryption key names.
func NewHKIDN(blz, customerID, systemID string) Segment {
	return newSegment("HKIDN", 2,
		DEG{textElement("280"), valueElement(blz)},
		DEG{valueElement(customerID)},
		DEG{valueElement(systemID)},
		DEG{textElement("1")},
	)
}

// NewHKVVB builds HKVVB:3, processing preparation.
//
// The product id and product version are always parameters; this package has
// no product id constant or default.
func NewHKVVB(bpdVersion, updVersion uint16, productID, productVersion string) Segment {
	return newSegment("HKVVB", 3,
		DEG{numberElement(bpdVersion)},
		DEG{numberElement(updVersion)},
		DEG{textElement("1")},
		DEG{valueElement(productID)},
		DEG{valueElement(productVersion)},
	)
}

// NewHKSYN builds HKSYN:3, a request for a new system id.
func NewHKSYN() Segment {
	return newSegment("HKSYN", 3, DEG{textElement("0")})
}

// NewHKEND builds HKEND:1, dialog end.
func NewHKEND(dialogID string) Segment {
	return newSegment("HKEND", 1, DEG{valueElement(dialogID)})
}

// NewHKTANProcess4 builds HKTAN process 4 (request a TAN challenge) for one of
// the allowed business segment references.
func NewHKTANProcess4(version uint16, reference TanSegmentRef, tanMediumName *string) Segment {
	if !reference.valid() {
		return Segment{err: errUnknownTanReference}
	}

	body := make([]DEG, 0, 12)
	body = append(body,
		DEG{textElement("4")},
		DEG{textElement(string(reference))},
	)
	if version >= 6 {
		for count := 0; count < 5; count++ {
			body = append(body, DEG{emptyElement()})
		}
	}
	if tanMediumName != nil {
		if version >= 6 {
			for count := 0; count < 3; count++ {
				body = append(body, DEG{emptyElement()})
			}
		}
		body = append(body, DEG{valueElement(*tanMediumName)})
	}
	return newSegment("HKTAN", version, body...)
}

// NewHKTANProcess2 builds HKTAN process 2 (submit a TAN) with a task reference
// from HITAN.
func NewHKTANProcess2(version uint16, taskReference string, tanMediumName *string) Segment {
	return newHKTANWithTaskReference("2", version, taskReference, tanMediumName)
}

// newHKTANWithTaskReference builds the shared layout of HKTAN processes 2 and S.
// process is a constant from those two constructors only.
func newHKTANWithTaskReference(process string, version uint16, taskReference string, tanMediumName *string) Segment {
	body := make([]DEG, 0, 12)
	body = append(body, DEG{textElement(process)})
	if version >= 6 {
		body = append(body,
			DEG{emptyElement()},
			DEG{emptyElement()},
			DEG{emptyElement()},
			DEG{valueElement(taskReference)},
			DEG{boolElement(false)},
			DEG{emptyElement()},
		)
	} else {
		body = append(body,
			DEG{emptyElement()},
			DEG{emptyElement()},
			DEG{valueElement(taskReference)},
			DEG{boolElement(false)},
		)
	}
	if tanMediumName != nil {
		if version >= 6 {
			for count := 0; count < 3; count++ {
				body = append(body, DEG{emptyElement()})
			}
		}
		body = append(body, DEG{valueElement(*tanMediumName)})
	}
	return newSegment("HKTAN", version, body...)
}

// NewHKTANProcessS builds HKTAN process S, the status poll for a decoupled
// TAN (app approval or push TAN), with the task reference from HITAN. Its
// layout is process 2's with "S" as the process code, as in fints-rs.
func NewHKTANProcessS(version uint16, taskReference string, tanMediumName *string) Segment {
	return newHKTANWithTaskReference("S", version, taskReference, tanMediumName)
}

// NewHKTAB builds HKTAB, a request for the registered TAN media. The medium
// class is "A" (all).
func NewHKTAB(version uint16) Segment {
	return newSegment("HKTAB", version, DEG{textElement("A")})
}

// NewHKSPA builds HKSPA, a request for the SEPA account connections.
func NewHKSPA(version uint16) Segment {
	return newSegment("HKSPA", version)
}

// NewHKSAL builds HKSAL, a balance request for one account.
//
// Version 6 and later use the international account form (KTI: IBAN:BIC);
// older versions use the national form. An empty IBAN or BIC is an error.
func NewHKSAL(version uint16, iban, bic string, touchdown *string) Segment {
	var account DEG
	if version >= 6 {
		international, err := ktiGroup(iban, bic)
		if err != nil {
			return Segment{err: err}
		}
		account = international
	} else {
		account = nationalAccountGroup(iban, bic)
	}

	body := []DEG{account, DEG{textElement("N")}, DEG{emptyElement()}}
	if touchdown != nil {
		body = append(body, DEG{valueElement(*touchdown)})
	}
	return newSegment("HKSAL", version, body...)
}

// NewHKKAZ builds HKKAZ, an MT940 statement request for one account.
func NewHKKAZ(version uint16, iban, bic string, start, end Date, touchdown *string) Segment {
	if !start.valid() || !end.valid() {
		return Segment{err: errInvalidDate}
	}

	var account DEG
	if version >= 6 {
		international, err := ktiGroup(iban, bic)
		if err != nil {
			return Segment{err: err}
		}
		account = international
	} else {
		account = nationalAccountGroup(iban, bic)
	}

	body := []DEG{
		account,
		DEG{textElement("N")},
		DEG{dateElement(start)},
		DEG{dateElement(end)},
		DEG{emptyElement()},
	}
	if touchdown != nil {
		body = append(body, DEG{valueElement(*touchdown)})
	}
	return newSegment("HKKAZ", version, body...)
}

// NewHKCAZ builds HKCAZ, a camt statement request for one account.
//
// The accepted camt formats travel as one data element group, for example
// "urn:iso:std:iso:20022:tech:xsd:camt.052.001.02". allAccounts is the
// all-accounts flag (J/N). maxEntries and touchdown are optional.
func NewHKCAZ(
	version uint16,
	iban, bic string,
	camtFormats []string,
	allAccounts bool,
	start, end Date,
	maxEntries *int,
	touchdown *string,
) Segment {
	if !start.valid() || !end.valid() {
		return Segment{err: errInvalidDate}
	}
	international, err := ktiGroup(iban, bic)
	if err != nil {
		return Segment{err: err}
	}

	formats := make(DEG, 0, len(camtFormats))
	for _, format := range camtFormats {
		formats = append(formats, textElement(format))
	}

	body := []DEG{
		international,
		formats,
		DEG{boolElement(allAccounts)},
		DEG{dateElement(start)},
		DEG{dateElement(end)},
	}
	if maxEntries != nil {
		if *maxEntries < 0 {
			return Segment{err: errInvalidMaxEntries}
		}
		body = append(body, DEG{textElement(strconv.Itoa(*maxEntries))})
	} else {
		body = append(body, DEG{emptyElement()})
	}
	if touchdown != nil {
		body = append(body, DEG{valueElement(*touchdown)})
	}
	return newSegment("HKCAZ", version, body...)
}

// ---- envelope segments, built by [BuildMessage] ----

func newHNHBK(messageSize uint64, dialogID string, messageNumber uint16) Segment {
	return newSegment("HNHBK", 3,
		DEG{textElement(fmt.Sprintf("%012d", messageSize))},
		DEG{textElement("300")},
		DEG{valueElement(dialogID)},
		DEG{numberElement(messageNumber)},
	)
}

func newHNHBS(messageNumber uint16) Segment {
	return newSegment("HNHBS", 1, DEG{numberElement(messageNumber)})
}

func newHNVSK(blz, userID, systemID string, now time.Time) Segment {
	return newSegment("HNVSK", 3,
		DEG{textElement("PIN"), textElement("1")},
		DEG{textElement("998")},
		DEG{textElement("1")},
		DEG{textElement("2"), emptyElement(), valueElement(systemID)},
		DEG{textElement("1"), textElement(now.Format("20060102")), textElement(now.Format("150405"))},
		DEG{
			textElement("2"),
			textElement("2"),
			textElement("13"),
			binaryElement(make([]byte, 8)),
			textElement("5"),
			textElement("1"),
		},
		DEG{
			textElement("280"),
			valueElement(blz),
			valueElement(userID),
			textElement("V"),
			textElement("0"),
			textElement("0"),
		},
		DEG{textElement("0")},
	)
}

func newHNVSD(inner []byte) Segment {
	return newSegment("HNVSD", 1, DEG{binaryElement(inner)})
}

func newHNSHK(
	securityFunction string,
	securityReference uint32,
	blz, userID, systemID string,
	now time.Time,
) Segment {
	return newSegment("HNSHK", 4,
		DEG{textElement("PIN"), textElement("1")},
		DEG{valueElement(securityFunction)},
		DEG{textElement(strconv.FormatUint(uint64(securityReference), 10))},
		DEG{textElement("1")},
		DEG{textElement("1")},
		DEG{textElement("2"), emptyElement(), valueElement(systemID)},
		DEG{textElement("1")},
		DEG{textElement("1"), textElement(now.Format("20060102")), textElement(now.Format("150405"))},
		DEG{textElement("1"), textElement("999"), textElement("1")},
		DEG{textElement("6"), textElement("10"), textElement("16")},
		DEG{
			textElement("280"),
			valueElement(blz),
			valueElement(userID),
			textElement("S"),
			textElement("0"),
			textElement("0"),
		},
	)
}
