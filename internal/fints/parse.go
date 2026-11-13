// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "strings"

// ParseMessage parses an untrusted FinTS message into segments.
//
// The parser enforces a 16 MiB size limit, rejects a truncated segment,
// rejects a binary value that runs past the end and never panics. Empty input
// is an error. Text is decoded from ISO-8859-1 to UTF-8.
func ParseMessage(data []byte) ([]ParsedSegment, error) {
	return parseMessage(data)
}

func parseMessage(data []byte) ([]ParsedSegment, error) {
	if len(data) == 0 {
		return nil, &ParseError{Offset: 0, Reason: reasonEmptyMessage}
	}
	if len(data) > MaxMessageSize {
		return nil, &ParseError{Offset: 0, Reason: reasonTooLarge}
	}

	segments := make([]ParsedSegment, 0, 8)
	position := 0
	for {
		for position < len(data) && isSegmentSpace(data[position]) {
			position++
		}
		if position >= len(data) {
			break
		}
		segment, next, err := parseSegment(data, position)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
		position = next
	}
	if len(segments) == 0 {
		return nil, &ParseError{Offset: 0, Reason: reasonNoSegments}
	}
	return segments, nil
}

func isSegmentSpace(character byte) bool {
	return character == '\r' || character == '\n' || character == ' '
}

func parseSegment(data []byte, start int) (ParsedSegment, int, error) {
	groups := make([]DEG, 0, 8)
	current := DEG{}
	position := start
	needNewElement := true

	for {
		if position >= len(data) {
			return ParsedSegment{}, position, segmentParseError(groups, position, reasonUnterminated)
		}

		switch data[position] {
		case escapeCharacter:
			position++
			if position >= len(data) {
				return ParsedSegment{}, position, segmentParseError(groups, position, reasonUnterminatedEsc)
			}
			text := decodeLatin1(data[position : position+1])
			if needNewElement {
				current = append(current, textElement(text))
				needNewElement = false
			} else {
				current = appendTextToGroup(current, text)
			}
			position++

		case '@':
			position++
			lengthStart := position
			for position < len(data) && data[position] != '@' {
				position++
			}
			if position >= len(data) {
				return ParsedSegment{}, position, segmentParseError(groups, position, reasonUnterminatedBin)
			}
			length, ok := parseBinaryLength(data[lengthStart:position])
			if !ok {
				return ParsedSegment{}, position, segmentParseError(groups, position, reasonInvalidBinLength)
			}
			position++
			if length > len(data)-position {
				return ParsedSegment{}, position, segmentParseError(groups, position, reasonBinaryPastEnd)
			}
			value := append([]byte(nil), data[position:position+length]...)
			current = append(current, binaryElement(value))
			needNewElement = false
			position += length

		case ':':
			if len(current) == 0 {
				current = append(current, emptyElement())
			}
			position++
			needNewElement = true
			if position < len(data) &&
				(data[position] == ':' || data[position] == '+' || data[position] == '\'') {
				current = append(current, emptyElement())
			}

		case '+':
			if len(current) == 0 {
				current = append(current, emptyElement())
			}
			groups = append(groups, current)
			current = DEG{}
			needNewElement = true
			position++

		case '\'':
			if len(current) == 0 {
				current = append(current, emptyElement())
			}
			groups = append(groups, current)
			position++
			return newParsedSegment(groups, start), position, nil

		default:
			characterStart := position
			for position < len(data) && !isSeparator(data[position]) {
				position++
			}
			text := decodeLatin1(data[characterStart:position])
			if needNewElement {
				current = append(current, textElement(text))
				needNewElement = false
			} else {
				current = appendTextToGroup(current, text)
			}
		}
	}
}

func isSeparator(character byte) bool {
	switch character {
	case escapeCharacter, '@', ':', '+', '\'':
		return true
	default:
		return false
	}
}

func appendTextToGroup(group DEG, text string) DEG {
	if len(group) > 0 && group[len(group)-1].kind == elementText {
		group[len(group)-1].text += text
		return group
	}
	return append(group, textElement(text))
}

// decodeLatin1 converts ISO-8859-1 bytes to UTF-8. Every byte maps to the code
// point of the same value.
func decodeLatin1(data []byte) string {
	ascii := true
	for _, character := range data {
		if character >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(data)
	}

	var builder strings.Builder
	builder.Grow(len(data) * 2)
	for _, character := range data {
		builder.WriteRune(rune(character))
	}
	return builder.String()
}

// parseBinaryLength accepts a non-empty run of ASCII digits and caps it at the
// message size limit, so a huge declared length cannot overflow int.
func parseBinaryLength(digits []byte) (int, bool) {
	if len(digits) == 0 {
		return 0, false
	}
	length := 0
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		length = length*10 + int(digit-'0')
		if length > MaxMessageSize {
			return 0, false
		}
	}
	return length, true
}

func segmentParseError(groups []DEG, offset int, reason string) *ParseError {
	segment := newParsedSegment(groups, offset)
	return &ParseError{
		Type:   safeSegmentType(segment.Type()),
		Number: segment.Number(),
		Offset: offset,
		Reason: reason,
	}
}

// safeSegmentType keeps a parsed header value out of an error unless it has
// the shape of a FinTS segment type. A malformed message must not leak its
// first data element (which might be an account number) through the error's
// segment type.
func safeSegmentType(value string) string {
	if len(value) != 5 {
		return ""
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return ""
		}
	}
	return value
}
