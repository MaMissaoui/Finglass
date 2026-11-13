// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"fmt"
	"strconv"
	"strings"
)

// TanProcess is the closed set of TAN processes from HITANS.
type TanProcess uint8

// The TAN processes. Every value other than "1" is two-step, as fints-rs does.
const (
	TanProcessTwoStep TanProcess = iota
	TanProcessOneStep
)

// String returns the process name. It never contains bank data.
func (p TanProcess) String() string {
	if p == TanProcessOneStep {
		return "one-step"
	}
	return "two-step"
}

func tanProcessFromWire(value string) TanProcess {
	if value == "1" {
		return TanProcessOneStep
	}
	return TanProcessTwoStep
}

// MediumName says whether HITANS allows, requires or forbids a TAN medium name
// for a method.
type MediumName uint8

// The TAN medium name requirements.
const (
	MediumNotAllowed MediumName = iota
	MediumOptional
	MediumRequired
)

// TanMediumName is the descriptive form of [MediumName].
type TanMediumName = MediumName

// String returns the requirement name. It never contains bank data.
func (m MediumName) String() string {
	switch m {
	case MediumOptional:
		return "optional"
	case MediumRequired:
		return "required"
	default:
		return "not-allowed"
	}
}

// TanMethod is one TAN method advertised by the bank in HITANS.
type TanMethod struct {
	// SecurityFunction is the security function code, for example "940".
	SecurityFunction string
	// TanProcess is the one-step or two-step process.
	TanProcess TanProcess
	// TechnicalID is the method's technical id, for example "MTAN2".
	TechnicalID string
	// DKID is the method's DK/ZKA id for HITANS version 4 and later.
	DKID string
	// DKVersion is the method's DK/ZKA version for HITANS version 4 and later.
	DKVersion string
	// MediumName says whether a TAN medium name is allowed or required.
	MediumName MediumName
	// DecoupledMaxPolls is the maximum number of decoupled polls, -1 when
	// unlimited or absent.
	DecoupledMaxPolls int
	// WaitBeforeFirstPoll is the wait in seconds before the first poll.
	WaitBeforeFirstPoll int
	// WaitBeforeNextPoll is the wait in seconds between polls.
	WaitBeforeNextPoll int
	// ManualConfirmationAllowed reports whether a person may confirm manually.
	ManualConfirmationAllowed bool
	// AutomatedPollingAllowed reports whether automatic polling is allowed.
	AutomatedPollingAllowed bool
	// HKTANVersion is the HITANS version, which is also the HKTAN version.
	HKTANVersion uint16

	name                     string
	decoupledMaxPollsPresent bool
}

// Name returns the bank's display name for this method.
//
// The name may carry account numbers; never log or audit it.
func (m TanMethod) Name() string {
	return m.name
}

// NeedsTanMedium reports whether the bank requires a TAN medium name. An
// optional medium name does not count as required.
func (m TanMethod) NeedsTanMedium() bool {
	return m.MediumName == MediumRequired
}

// IsDecoupled reports whether this is a decoupled method. HITANS version 7
// marks a method decoupled by carrying a maximum-poll element, or by naming
// its technical id or DK id "DECOUPLED".
func (m TanMethod) IsDecoupled() bool {
	if m.HKTANVersion < 7 {
		return false
	}
	if m.decoupledMaxPollsPresent || m.DecoupledMaxPolls >= 0 {
		return true
	}
	return containsDecoupled(m.TechnicalID) || containsDecoupled(m.DKID)
}

func containsDecoupled(value string) bool {
	return strings.Contains(strings.ToUpper(value), "DECOUPLED")
}

// String returns only non-text metadata, never the bank's display name.
func (m TanMethod) String() string {
	return fmt.Sprintf(
		"fints.TanMethod{securityFunction:%s process:%s technicalID:%s dkID:%s dkVersion:%s medium:%s maxPolls:%d waitFirst:%d waitNext:%d manual:%t automated:%t isDecoupled:%t hktanVersion:%d}",
		m.SecurityFunction, m.TanProcess, m.TechnicalID, m.DKID, m.DKVersion,
		m.MediumName, m.DecoupledMaxPolls, m.WaitBeforeFirstPoll,
		m.WaitBeforeNextPoll, m.ManualConfirmationAllowed,
		m.AutomatedPollingAllowed, m.IsDecoupled(), m.HKTANVersion,
	)
}

// GoString returns only metadata, never the bank's display name.
func (m TanMethod) GoString() string {
	return m.String()
}

// Format writes only metadata, never the bank's display name, for every fmt
// verb.
func (m TanMethod) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(m.String()))
}

// ParseTanMethods reads every TAN method from HITANS. The parameter group is
// DEG 4: it starts with three header elements (four in version 1) and then
// carries all methods back to back as flattened elements. The field positions
// and each method's element count depend on the HITANS version.
func ParseTanMethods(segment ParsedSegment) []TanMethod {
	version := segment.Version()
	group := segment.DEG(4)
	if group.Len() == 0 {
		return []TanMethod{}
	}

	headerElements := 3
	if version == 1 {
		headerElements = 4
	}
	methodElements := tanMethodElementCount(version)

	methods := make([]TanMethod, 0, 4)
	for offset := headerElements; offset < group.Len(); {
		if group.Len()-offset < 3 {
			// A trailing remainder too short to be a method is skipped.
			break
		}
		if group.Text(offset) == "" {
			// An empty security function marks the end of the method list.
			break
		}

		length := methodElements
		if length == 0 || offset+length >= group.Len() {
			length = group.Len() - offset
		} else if !looksLikeTanMethodStart(group, offset+length) {
			length = findTanMethodLength(group, offset, methodElements)
		}
		if length < 3 {
			break
		}

		methods = append(methods, parseTanMethod(group, offset, version))
		offset += length
	}
	return methods
}

// tanMethodElementCount is the number of flattened elements per method for a
// HITANS version. Unknown versions fall back to "read the rest as one method".
func tanMethodElementCount(version uint16) int {
	switch version {
	case 1:
		return 11
	case 2:
		return 15
	case 3:
		return 18
	case 4, 5:
		return 22
	case 6:
		return 21
	case 7:
		return 26
	default:
		return 0
	}
}

// looksLikeTanMethodStart recognises the start of a flattened method. Security
// functions are three-digit codes and the TAN process is 1 or 2.
func looksLikeTanMethodStart(group DEG, index int) bool {
	if index < 0 || index+1 >= group.Len() {
		return false
	}
	securityFunction := group.Text(index)
	if len(securityFunction) != 3 {
		return false
	}
	for _, character := range securityFunction {
		if character < '0' || character > '9' {
			return false
		}
	}
	process := group.Text(index + 1)
	return process == "1" || process == "2"
}

// findTanMethodLength handles methods that omit their trailing empty elements
// even when another method follows, as fints-rs's mock server does. It prefers
// the version's full element count and otherwise finds the latest plausible
// method start before it.
func findTanMethodLength(group DEG, offset, methodElements int) int {
	if methodElements <= 0 {
		return group.Len() - offset
	}
	if offset+methodElements < group.Len() && looksLikeTanMethodStart(group, offset+methodElements) {
		return methodElements
	}
	for length := methodElements - 1; length >= 3; length-- {
		if looksLikeTanMethodStart(group, offset+length) {
			return length
		}
	}
	return methodElements
}

func parseTanMethod(group DEG, offset int, version uint16) TanMethod {
	at := func(index int) string {
		return group.Text(offset + index)
	}

	nameIndex := 3
	if version >= 4 {
		nameIndex = 5
	}
	name := at(nameIndex)

	dkID := ""
	dkVersion := ""
	if version >= 4 {
		dkID = at(3)
		dkVersion = at(4)
	}

	mediumIndex := -1
	switch {
	case version == 3:
		mediumIndex = 16
	case version == 4 || version == 5:
		mediumIndex = 20
	case version >= 6:
		mediumIndex = 18
	}
	medium := MediumNotAllowed
	if mediumIndex >= 0 {
		switch at(mediumIndex) {
		case "1":
			medium = MediumOptional
		case "2":
			medium = MediumRequired
		}
	}

	maxPolls := -1
	waitFirst := 0
	waitNext := 0
	manualConfirmation := false
	automatedPolling := false
	maxPollsPresent := false
	if version >= 7 {
		maxPollsValue := at(21)
		if maxPollsValue != "" {
			maxPollsPresent = true
			maxPolls = parseOptionalInt(maxPollsValue, -1)
		}
		waitFirst = parseOptionalInt(at(22), 0)
		waitNext = parseOptionalInt(at(23), 0)
		manualConfirmation = at(24) == "J"
		automatedPolling = at(25) == "J"
	}

	if name == "" {
		name = "TAN-" + at(0)
	}

	return TanMethod{
		SecurityFunction:          at(0),
		TanProcess:                tanProcessFromWire(at(1)),
		TechnicalID:               at(2),
		DKID:                      dkID,
		DKVersion:                 dkVersion,
		MediumName:                medium,
		DecoupledMaxPolls:         maxPolls,
		WaitBeforeFirstPoll:       waitFirst,
		WaitBeforeNextPoll:        waitNext,
		ManualConfirmationAllowed: manualConfirmation,
		AutomatedPollingAllowed:   automatedPolling,
		HKTANVersion:              version,
		name:                      name,
		decoupledMaxPollsPresent:  maxPollsPresent,
	}
}

// ParseHITANS is the segment-named form of [ParseTanMethods].
func ParseHITANS(segment ParsedSegment) []TanMethod {
	return ParseTanMethods(segment)
}

// TanMethods returns the TAN methods from every HITANS segment, in wire order.
func (r Response) TanMethods() []TanMethod {
	methods := make([]TanMethod, 0)
	for _, segment := range r.findSegments("HITANS") {
		methods = append(methods, ParseTanMethods(segment)...)
	}
	return methods
}

func parseOptionalInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// ParseHIPINS ports fints-rs's parse_hipins. It reads the bank's segment-type
// strings and returns whether each operation needs a TAN.
func ParseHIPINS(segment ParsedSegment) map[string]bool {
	operations := make(map[string]bool)
	for groupIndex := 1; groupIndex < segment.DEGCount(); groupIndex++ {
		group := segment.DEG(groupIndex)
		for index := 0; index+1 < group.Len(); {
			key := group.Text(index)
			value := group.Text(index + 1)
			if looksLikeSegmentType(key) && (value == "J" || value == "N") {
				operations[key] = value == "J"
				index += 2
			} else {
				index++
			}
		}
	}
	return operations
}

// TANRequiredOperations returns the merged HIPINS maps from every HIPINS
// segment, keyed by the bank's segment types.
func (r Response) TANRequiredOperations() map[string]bool {
	operations := make(map[string]bool)
	for _, segment := range r.findSegments("HIPINS") {
		for key, value := range ParseHIPINS(segment) {
			operations[key] = value
		}
	}
	return operations
}

func looksLikeSegmentType(value string) bool {
	if len(value) != 5 || !strings.HasPrefix(value, "HK") {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// BusinessSegment is a closed set of read-only business segment types whose
// BPD parameter segment carries a supported version. It is deliberately not a
// free string.
type BusinessSegment string

// The business segment types this package can request.
const (
	BusinessHKSAL BusinessSegment = "HKSAL"
	BusinessHKKAZ BusinessSegment = "HKKAZ"
	BusinessHKCAZ BusinessSegment = "HKCAZ"
	BusinessHKSPA BusinessSegment = "HKSPA"
	BusinessHKTAB BusinessSegment = "HKTAB"
	BusinessHKTAN BusinessSegment = "HKTAN"
)

// parameterSegmentType maps a business segment to its BPD parameter segment.
func (b BusinessSegment) parameterSegmentType() (string, bool) {
	switch b {
	case BusinessHKSAL:
		return "HISALS", true
	case BusinessHKKAZ:
		return "HIKAZS", true
	case BusinessHKCAZ:
		return "HICAZS", true
	case BusinessHKSPA:
		return "HISPAS", true
	case BusinessHKTAB:
		return "HITABS", true
	case BusinessHKTAN:
		return "HITANS", true
	default:
		return "", false
	}
}

// HighestSegmentVersion ports fints-rs's find_highest_segment_version for the
// closed set of business segments. It returns the highest parameter-segment
// version not above maxClientVersion, or 0 when the bank offers none.
func HighestSegmentVersion(
	segments []ParsedSegment,
	business BusinessSegment,
	maxClientVersion uint16,
) uint16 {
	parameterType, ok := business.parameterSegmentType()
	if !ok {
		return 0
	}
	highest := uint16(0)
	for _, segment := range segments {
		if segment.Type() != parameterType {
			continue
		}
		if version := segment.Version(); version <= maxClientVersion && version > highest {
			highest = version
		}
	}
	return highest
}

// FindHighestSegmentVersion is the fints-rs-named form of
// [HighestSegmentVersion].
func FindHighestSegmentVersion(
	segments []ParsedSegment,
	business BusinessSegment,
	maxClientVersion uint16,
) uint16 {
	return HighestSegmentVersion(segments, business, maxClientVersion)
}

// HighestSegmentVersion returns the highest supported version of a business
// segment from this response's BPD parameter segments.
func (r Response) HighestSegmentVersion(
	business BusinessSegment,
	maxClientVersion uint16,
) uint16 {
	return HighestSegmentVersion(r.Segments, business, maxClientVersion)
}

// ParseCamtFormats reads the accepted camt message URNs from one HICAZS
// parameter segment. Only URN values are returned, in wire order.
func ParseCamtFormats(segment ParsedSegment) []string {
	formats := make([]string, 0)
	for groupIndex := 1; groupIndex < segment.DEGCount(); groupIndex++ {
		group := segment.DEG(groupIndex)
		for index := 0; index < group.Len(); index++ {
			value := group.Text(index)
			if isCamtURN(value) {
				formats = append(formats, value)
			}
		}
	}
	return formats
}

// ParseHICAZS is the segment-named form of [ParseCamtFormats].
func ParseHICAZS(segment ParsedSegment) []string {
	return ParseCamtFormats(segment)
}

// CamtFormats returns the accepted camt message URNs from every HICAZS segment.
func (r Response) CamtFormats() []string {
	formats := make([]string, 0)
	for _, segment := range r.findSegments("HICAZS") {
		formats = append(formats, ParseCamtFormats(segment)...)
	}
	return formats
}

func isCamtURN(value string) bool {
	const prefix = "urn:iso:std:iso:20022:tech:xsd:camt."
	return len(value) > len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}
