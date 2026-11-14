// Package trace records a structural, allowlist-redacted trace of FinTS
// exchanges.
//
// The trace is for analysing bank answers offline. It never keeps a request or
// a response, never writes one anywhere, and records only segment types,
// versions, numbers, return codes, public bank parameters and counts. Every
// data element that the allowlist does not explicitly permit is replaced.
package trace

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/fints"
)

// The trace never recurses deeper than the codec accepts.
const maxRedactionDepth = 3

var (
	numberPattern  = regexp.MustCompile(`^[0-9]{1,6}$`)
	booleanPattern = regexp.MustCompile(`^[JN]$`)
	codePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}$`)
	versionPattern = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){1,3}$`)
	camtPattern    = regexp.MustCompile(`^urn:iso:std:iso:20022:tech:xsd:camt\.[0-9]{3}\.[0-9]{3}\.[0-9]{2}$`)
	segmentPattern = regexp.MustCompile(`^HK[A-Z]{3}$`)
	fourDigitCode  = regexp.MustCompile(`^[0-9]{4}$`)
	// smallNumber covers the counts and day limits inside HIUPD's allowed
	// transactions (signatures, limit days). A longer digit run may be a limit
	// amount and is redacted.
	smallNumber = regexp.MustCompile(`^[0-9]{1,3}$`)
	// accountTypeCode and currencyCode keep HIUPD's account type and currency
	// only in their exact shapes, so a name in an older layout is never kept.
	accountTypeCode = regexp.MustCompile(`^[0-9]{1,2}$`)
	currencyCode    = regexp.MustCompile(`^[A-Z]{3}$`)
)

// RedactMessage returns one redacted line per segment of message.
//
// Segments inside an HNVSD payload are indented by two spaces per nesting
// level. An unparseable message becomes a single placeholder line; no byte of
// the message is ever returned.
func RedactMessage(message []byte) []string {
	segments, err := fints.ParseMessage(message)
	if err != nil {
		return []string{unparseablePlaceholder(len(message))}
	}

	lines := make([]string, 0, len(segments))
	for _, segment := range segments {
		lines = append(lines, redactSegment(segment, 0, "")...)
	}
	return lines
}

func unparseablePlaceholder(length int) string {
	return "<unparseable message, " + strconv.Itoa(length) + " bytes>"
}

// redactSegment renders one parsed segment and, for HNVSD, its payload.
func redactSegment(segment fints.ParsedSegment, depth int, indent string) []string {
	if segment.Type() == "HNVSD" {
		return redactEncrypted(segment, depth, indent)
	}
	return []string{indent + redactSegmentLine(segment)}
}

// redactEncrypted renders an HNVSD segment and recurses into its binary
// payload. The payload is parsed only inside this call; no raw byte is kept.
func redactEncrypted(segment fints.ParsedSegment, depth int, indent string) []string {
	lines := []string{indent + headerText(segment) + " | <encrypted part follows>"}
	if segment.DEGCount() < 2 {
		return lines
	}

	group := segment.DEG(1)
	if group.Len() == 0 || !group.At(0).IsBinary() {
		return lines
	}
	payload := group.Binary(0)
	if len(payload) == 0 {
		lines = append(lines, indent+"  "+unparseablePlaceholder(0))
		return lines
	}
	if depth >= maxRedactionDepth {
		lines = append(lines, indent+"  "+unparseablePlaceholder(len(payload)))
		return lines
	}

	inner, err := fints.ParseMessage(payload)
	if err != nil {
		lines = append(lines, indent+"  "+unparseablePlaceholder(len(payload)))
		return lines
	}

	childIndent := indent + "  "
	for _, child := range inner {
		lines = append(lines, redactSegment(child, depth+1, childIndent)...)
	}
	return lines
}

// headerText returns the segment header, including its optional reference.
func headerText(segment fints.ParsedSegment) string {
	segmentType := segment.Type()
	if !isSegmentType(segmentType) {
		segmentType = redactedText(segmentType)
	}

	header := segmentType + ":" +
		strconv.FormatUint(uint64(segment.Number()), 10) + ":" +
		strconv.FormatUint(uint64(segment.Version()), 10)
	if reference, ok := segment.Reference(); ok {
		header += ":" + strconv.FormatUint(uint64(reference), 10)
	}
	return header
}

func isSegmentType(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// redactSegmentLine redacts the groups after the header according to the
// allowlist.
func redactSegmentLine(segment fints.ParsedSegment) string {
	header := headerText(segment)
	body := segment.DEGCount() - 1

	groups := make([]string, 0, body+1)
	groups = append(groups, header)

	add := func(index int, value string) {
		groups = append(groups, value)
	}
	group := func(index int) fints.DEG {
		return segment.DEG(index)
	}

	switch segment.Type() {
	case "HNHBK":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, "<len>")
			case 2:
				add(index, formatGroup(current, codeValue))
			case 3:
				add(index, namedGroup(current, "<dialog id>"))
			case 4:
				add(index, formatGroup(current, codeValue))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HNHBS":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 {
				add(index, formatGroup(current, codeValue))
			} else {
				add(index, redactedGroup(current))
			}
		}
	case "HNVSK", "HNSHA":
		add(0, "<redacted "+strconv.Itoa(body)+" groups>")
	case "HNSHK":
		groups = append(groups, redactHNSHK(segment, body)...)
	case "HKIDN":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, bankIdentifierGroup(current))
			case 2:
				add(index, namedGroup(current, "<customer id>"))
			case 3:
				add(index, namedGroup(current, "<system id>"))
			case 4:
				add(index, formatGroup(current, codeValue))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HKVVB":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1, 2, 3, 5:
				add(index, formatGroup(current, codeValue))
			case 4:
				add(index, namedGroup(current, "<product id>"))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HKSYN":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 {
				add(index, formatGroup(current, codeValue))
			} else {
				add(index, redactedGroup(current))
			}
		}
	case "HKTAN":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1, 2:
				add(index, formatGroup(current, codeValue))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HKSPA":
		for index := 1; index <= body; index++ {
			add(index, accountGroup(group(index)))
		}
	case "HKSAL":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2:
				add(index, formatGroup(current, codeValue))
			default:
				if groupIsEmpty(current) {
					add(index, "")
				} else {
					add(index, "<touchdown>")
				}
			}
		}
	case "HKKAZ":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2, 5:
				add(index, formatGroup(current, codeValue))
			case 3, 4:
				add(index, formatGroup(current, dateValue))
			case 6:
				if groupIsEmpty(current) {
					add(index, "")
				} else {
					add(index, "<touchdown>")
				}
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HKCAZ":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2, 3, 6:
				add(index, formatGroup(current, codeValue))
			case 4, 5:
				add(index, formatGroup(current, dateValue))
			case 7:
				if groupIsEmpty(current) {
					add(index, "")
				} else {
					add(index, "<touchdown>")
				}
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HKTAB":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 || index == 2 {
				add(index, formatGroup(current, codeValue))
			} else {
				add(index, redactedGroup(current))
			}
		}
	case "HKEND":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 {
				add(index, namedGroup(current, "<dialog id>"))
			} else {
				add(index, redactedGroup(current))
			}
		}
	case "HIRMG", "HIRMS":
		for index := 1; index <= body; index++ {
			add(index, returnCodeGroup(group(index)))
		}
	case "HIBPA":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, formatGroup(current, codeValue))
			case 2:
				add(index, bankIdentifierGroup(current))
			case 3:
				add(index, redactedGroup(current))
			default:
				add(index, formatGroup(current, codeValue))
			}
		}
	case "HIUPA":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, namedGroup(current, "<user id>"))
			case 2, 3:
				add(index, formatGroup(current, codeValue))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HIUPD":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2:
				add(index, formatGroup(current, maskValue))
			case 3:
				add(index, namedGroup(current, "<customer id>"))
			case 4:
				add(index, formatGroup(current, patternValue(accountTypeCode)))
			case 5:
				add(index, formatGroup(current, patternValue(currencyCode)))
			case 6, 7, 8, 9:
				add(index, redactedGroup(current))
			default:
				add(index, allowedTransactionGroup(current))
			}
		}
	case "HISYN":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 {
				add(index, namedGroup(current, "<system id>"))
			} else {
				add(index, redactedGroup(current))
			}
		}
	case "HITAN":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1, 2:
				add(index, formatGroup(current, codeValue))
			case 3:
				add(index, redactedGroup(current))
			case 4:
				add(index, challengeGroup(current))
			case 5:
				add(index, redactedGroup(current))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HITAB":
		for index := 1; index <= body; index++ {
			current := group(index)
			if index == 1 {
				add(index, formatGroup(current, codeValue))
				continue
			}
			add(index, mediaGroup(current))
		}
	case "HISAL":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2:
				add(index, redactedGroup(current))
			case 3:
				add(index, formatGroup(current, codeValue))
			case 4, 5:
				add(index, balanceGroup(current))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HIKAZ":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1, 2:
				add(index, mt940Group(current))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HICAZ":
		for index := 1; index <= body; index++ {
			current := group(index)
			switch index {
			case 1:
				add(index, accountGroup(current))
			case 2:
				add(index, formatGroup(current, codeValue))
			case 3:
				add(index, camtDocumentsGroup(current))
			default:
				add(index, redactedGroup(current))
			}
		}
	case "HISPA":
		for index := 1; index <= body; index++ {
			add(index, hispaGroup(group(index)))
		}
	default:
		if isParameterSegmentType(segment.Type()) {
			for index := 1; index <= body; index++ {
				add(index, formatGroup(group(index), codeValue))
			}
		} else {
			add(0, "<redacted "+strconv.Itoa(body)+" groups>")
		}
	}

	return strings.Join(groups, " | ")
}

// redactHNSHK keeps the security function and collapses the rest.
func redactHNSHK(segment fints.ParsedSegment, body int) []string {
	if body <= 0 {
		return []string{"<redacted 0 groups>"}
	}

	securityIndex := 2
	if first := segment.DEG(1); first.Len() == 1 && numberPattern.MatchString(first.Text(0)) {
		// A synthetic segment may put the security function in the first body
		// group; the codec's own layout uses the second.
		securityIndex = 1
	}
	if securityIndex > body {
		securityIndex = body
	}

	groups := make([]string, 0, 2)
	groups = append(groups, formatGroup(segment.DEG(securityIndex), codeValue))
	groups = append(groups, "<redacted "+strconv.Itoa(body-1)+" groups>")
	return groups
}

func isParameterSegmentType(segmentType string) bool {
	return strings.HasPrefix(segmentType, "HI") && strings.HasSuffix(segmentType, "S")
}

// ---- element and group helpers ----

func redactedText(value string) string {
	return "<redacted len=" + strconv.Itoa(len(value)) + ">"
}

func redactedBinary(value []byte) string {
	return "<redacted len=" + strconv.Itoa(len(value)) + ">"
}

func redactedValue(value string) string {
	if value == "" {
		return ""
	}
	return redactedText(value)
}

func codeValue(value string) string {
	if value == "" {
		return ""
	}
	switch {
	case numberPattern.MatchString(value):
		return value
	case booleanPattern.MatchString(value):
		return value
	case codePattern.MatchString(value):
		return value
	case versionPattern.MatchString(value):
		return value
	case camtPattern.MatchString(value):
		return value
	default:
		return redactedText(value)
	}
}

// patternValue keeps a value only when it matches pattern exactly.
func patternValue(pattern *regexp.Regexp) func(string) string {
	return func(value string) string {
		if value == "" {
			return ""
		}
		if pattern.MatchString(value) {
			return value
		}
		return redactedText(value)
	}
}

func maskValue(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) < 4 {
		return "…"
	}
	return "…" + string(runes[len(runes)-4:])
}

func textValue(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	tail := runes
	if len(runes) > 4 {
		tail = runes[len(runes)-4:]
	}
	return "<text …" + string(tail) + ">"
}

func dateValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) == 8 && isDigits(value) {
		return value
	}
	return redactedText(value)
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func formatGroup(group fints.DEG, rule func(string) string) string {
	if group.Len() == 0 {
		return ""
	}
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		switch {
		case element.IsBinary():
			elements[index] = redactedBinary(element.Binary())
		case element.IsEmpty():
			elements[index] = ""
		default:
			elements[index] = rule(element.Text())
		}
	}
	return strings.Join(elements, ":")
}

func redactedGroup(group fints.DEG) string {
	return formatGroup(group, redactedValue)
}

func groupIsEmpty(group fints.DEG) bool {
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		if element.IsBinary() {
			if len(element.Binary()) > 0 {
				return false
			}
			continue
		}
		if element.Text() != "" {
			return false
		}
	}
	return true
}

func namedGroup(group fints.DEG, name string) string {
	if groupIsEmpty(group) {
		return ""
	}
	return name
}

// bankIdentifierGroup renders the public KTV bank identifier 280:BLZ.
func bankIdentifierGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	if group.Len() == 1 {
		value := group.Text(0)
		switch value {
		case "":
			return ""
		case "280":
			return "280"
		default:
			return "280:" + value
		}
	}
	blz := group.Text(1)
	if blz == "" {
		blz = group.Text(group.Len() - 1)
	}
	return "280:" + blz
}

// accountGroup renders a KTV national account or a KTI IBAN:BIC group.
func accountGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	if group.Len() == 2 {
		return maskValue(group.Text(0)) + ":" + group.Text(1)
	}
	if group.Len() == 1 {
		return maskValue(group.Text(0))
	}
	return maskValue(group.Text(0)) + ":" +
		maskValue(group.Text(1)) + ":280:" + group.Text(3)
}

func returnCodeGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	code := group.Text(0)
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		if element.IsBinary() {
			elements[index] = redactedBinary(element.Binary())
			continue
		}
		value := element.Text()
		if value == "" {
			elements[index] = ""
			continue
		}
		switch index {
		case 0:
			if fourDigitCode.MatchString(value) {
				elements[index] = value
			} else {
				elements[index] = redactedText(value)
			}
		case 1:
			elements[index] = codeValue(value)
		case 2:
			elements[index] = textValue(value)
		default:
			if code == "3920" {
				elements[index] = codeValue(value)
			} else {
				elements[index] = redactedText(value)
			}
		}
	}
	return strings.Join(elements, ":")
}

func allowedTransactionGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		switch {
		case element.IsBinary():
			elements[index] = redactedBinary(element.Binary())
		case element.IsEmpty():
			elements[index] = ""
		default:
			value := element.Text()
			if segmentPattern.MatchString(value) || smallNumber.MatchString(value) {
				elements[index] = value
			} else {
				elements[index] = redactedText(value)
			}
		}
	}
	return strings.Join(elements, ":")
}

func balanceGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		if element.IsBinary() {
			elements[index] = redactedBinary(element.Binary())
			continue
		}
		value := element.Text()
		if value == "" {
			elements[index] = ""
			continue
		}
		switch index {
		case 0:
			elements[index] = codeValue(value)
		case 1:
			elements[index] = "<amount>"
		case 2:
			elements[index] = codeValue(value)
		case 3:
			elements[index] = dateValue(value)
		default:
			elements[index] = redactedText(value)
		}
	}
	return strings.Join(elements, ":")
}

func mt940Group(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	element := group.At(0)
	if element.IsBinary() {
		data := element.Binary()
		return "<mt940 " + strconv.Itoa(len(data)) + " bytes, " +
			strconv.Itoa(strings.Count(string(data), ":61:")) + " bookings>"
	}
	if element.IsEmpty() {
		return ""
	}
	return redactedText(element.Text())
}

func camtDocumentsGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		switch {
		case element.IsBinary():
			data := element.Binary()
			elements[index] = "<camt " + strconv.Itoa(len(data)) + " bytes, " +
				strconv.Itoa(strings.Count(string(data), "<Ntry>")) + " entries>"
		case element.IsEmpty():
			elements[index] = ""
		default:
			elements[index] = redactedText(element.Text())
		}
	}
	return strings.Join(elements, ":")
}

func challengeGroup(group fints.DEG) string {
	if groupIsEmpty(group) {
		return ""
	}
	length := 0
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		if element.IsBinary() {
			length += len(element.Binary())
		} else {
			length += len(element.Text())
		}
	}
	return "<challenge len=" + strconv.Itoa(length) + ">"
}

func mediaGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}
	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		switch {
		case element.IsBinary():
			elements[index] = redactedBinary(element.Binary())
		case element.IsEmpty():
			elements[index] = ""
		case index == 2:
			elements[index] = redactedText(element.Text())
		default:
			elements[index] = codeValue(element.Text())
		}
	}
	return strings.Join(elements, ":")
}

func hispaGroup(group fints.DEG) string {
	if group.Len() == 0 {
		return ""
	}

	hasFlag := group.Text(0) == "J" || group.Text(0) == "N"

	elements := make([]string, group.Len())
	for index := 0; index < group.Len(); index++ {
		element := group.At(index)
		if element.IsBinary() {
			elements[index] = redactedBinary(element.Binary())
			continue
		}
		value := element.Text()
		if value == "" {
			elements[index] = ""
			continue
		}

		position := index
		if hasFlag {
			position = index - 1
		}
		switch {
		case hasFlag && index == 0:
			elements[index] = codeValue(value)
		case position == 0:
			elements[index] = maskValue(value)
		case position == 1:
			elements[index] = value
		case position == 2:
			elements[index] = maskValue(value)
		case position == 3:
			elements[index] = maskValue(value)
		case position == 4:
			elements[index] = value
		case position == 5:
			elements[index] = value
		default:
			elements[index] = redactedText(value)
		}
	}
	return strings.Join(elements, ":")
}

// ---- note redaction ----

// RedactNote keeps the technical characters, masks a word that carries a digit
// run of five or more, and cuts the result at 80 characters.
func RedactNote(text string) string {
	filtered := make([]rune, 0, len(text))
	for _, character := range text {
		if isNoteCharacter(character) {
			filtered = append(filtered, character)
		}
	}

	var builder strings.Builder
	for index := 0; index < len(filtered); {
		if !isNoteAlphanumeric(filtered[index]) {
			builder.WriteRune(filtered[index])
			index++
			continue
		}
		end := index
		for end < len(filtered) && isNoteAlphanumeric(filtered[end]) {
			end++
		}
		word := string(filtered[index:end])
		if hasLongDigitRun(word) {
			builder.WriteString(maskValue(word))
		} else {
			builder.WriteString(word)
		}
		index = end
	}

	result := []rune(builder.String())
	if len(result) > 80 {
		result = result[:80]
	}
	return string(result)
}

func isNoteCharacter(character rune) bool {
	if isNoteAlphanumeric(character) {
		return true
	}
	switch character {
	case ' ', '.', ',', ':', '(', ')', '/', '_', '-':
		return true
	default:
		return false
	}
}

func isNoteAlphanumeric(character rune) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9')
}

func hasLongDigitRun(value string) bool {
	run := 0
	for _, character := range value {
		if character >= '0' && character <= '9' {
			run++
			if run >= 5 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
