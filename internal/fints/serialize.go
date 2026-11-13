// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import "strconv"

// escapeCharacter introduces the literal form of a separator.
const escapeCharacter = '?'

// appendEscapedText appends text in its wire form. The text must be
// representable in ISO-8859-1; a character outside that range is an error and
// is never replaced silently.
func appendEscapedText(dst []byte, text string) ([]byte, error) {
	for _, character := range text {
		if character > 0xFF {
			return dst, errTextNotLatin1
		}
		// #nosec G115 -- the range check above guarantees the byte conversion.
		dst = appendEscapedByte(dst, byte(character))
	}
	return dst, nil
}

// appendEscapedBytes appends Latin-1 text that is already held as bytes.
//
// It is the path for secrets: the PIN and TAN never become a Go string.
func appendEscapedBytes(dst []byte, text []byte) []byte {
	for _, character := range text {
		dst = appendEscapedByte(dst, character)
	}
	return dst
}

func appendEscapedByte(dst []byte, character byte) []byte {
	switch character {
	case escapeCharacter, '\'', '+', ':', '@':
		dst = append(dst, escapeCharacter)
	}
	return append(dst, character)
}

// appendBinary appends a binary data element: @<length>@<bytes>. The length
// counts encoded bytes.
func appendBinary(dst []byte, data []byte) []byte {
	dst = append(dst, '@')
	dst = strconv.AppendInt(dst, int64(len(data)), 10)
	dst = append(dst, '@')
	return append(dst, data...)
}

// appendDEG appends a data element group. Trailing absent data elements are
// stripped, as fints-rs does; a present but empty text element keeps its
// separator.
func appendDEG(dst []byte, group DEG) ([]byte, error) {
	last := len(group)
	for last > 0 && group[last-1].IsEmpty() {
		last--
	}
	for index := 0; index < last; index++ {
		if index > 0 {
			dst = append(dst, ':')
		}
		element := group[index]
		switch element.kind {
		case elementText:
			encoded, err := appendEscapedText(dst, element.text)
			if err != nil {
				return dst, err
			}
			dst = encoded
		case elementBinary:
			dst = appendBinary(dst, element.binary)
		case elementEmpty:
			// Nothing to write; the separator above keeps the position.
		}
	}
	return dst, nil
}

// appendSegmentBytes appends a complete segment. Trailing empty groups are
// stripped and the segment is terminated with an apostrophe.
func appendSegmentBytes(dst []byte, groups []DEG) ([]byte, error) {
	last := len(groups)
	for last > 0 && groupIsEmpty(groups[last-1]) {
		last--
	}
	for index := 0; index < last; index++ {
		if index > 0 {
			dst = append(dst, '+')
		}
		encoded, err := appendDEG(dst, groups[index])
		if err != nil {
			return dst, err
		}
		dst = encoded
	}
	return append(dst, '\''), nil
}

// segmentHeader builds TYPE:NUMBER:VERSION.
func segmentHeader(segmentType string, number, version uint16) DEG {
	return DEG{
		textElement(segmentType),
		textElement(strconv.FormatUint(uint64(number), 10)),
		textElement(strconv.FormatUint(uint64(version), 10)),
	}
}

func numberElement(number uint16) DataElement {
	return textElement(strconv.FormatUint(uint64(number), 10))
}

func boolElement(value bool) DataElement {
	if value {
		return textElement("J")
	}
	return textElement("N")
}
