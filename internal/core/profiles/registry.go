package profiles

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// baseSlug turns a display name into FinDesk's slug: lower case, umlauts
// expanded, runs of other characters collapsed to one hyphen.
func baseSlug(name string) string {
	lowered := strings.ToLower(name)
	lowered = strings.NewReplacer(
		"ä", "a",
		"ö", "o",
		"ü", "u",
		"ß", "ss",
	).Replace(lowered)

	base := make([]rune, 0, len(lowered))
	for _, character := range lowered {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			base = append(base, character)
			continue
		}
		if len(base) == 0 || base[len(base)-1] != '-' {
			base = append(base, '-')
		}
	}

	trimmed := strings.Trim(string(base), "-")
	if trimmed == "" {
		trimmed = "profile"
	}

	runes := []rune(trimmed)
	if len(runes) > MaxNameLength {
		runes = runes[:MaxNameLength]
	}
	return strings.Trim(string(runes), "-")
}

// escape applies exactly the escapes the Rust registry writer applies.
func escape(text string) string {
	var escaped strings.Builder
	escaped.Grow(len(text))

	for _, character := range text {
		switch character {
		case '\\':
			escaped.WriteString(`\\`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		case '\u000C':
			escaped.WriteString(`\f`)
		case '=':
			escaped.WriteString(`\=`)
		case ':':
			escaped.WriteString(`\:`)
		default:
			escaped.WriteRune(character)
		}
	}

	return escaped.String()
}

// unescape reverses [escape] and also accepts Java-style \uXXXX escapes.
func unescape(text string) string {
	characters := []rune(text)
	var result strings.Builder
	result.Grow(len(text))

	for index := 0; index < len(characters); index++ {
		character := characters[index]
		if character != '\\' {
			result.WriteRune(character)
			continue
		}

		index++
		if index >= len(characters) {
			result.WriteByte('\\')
			break
		}

		switch characters[index] {
		case 't':
			result.WriteByte('\t')
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 'f':
			result.WriteByte('\u000C')
		case 'u':
			digits := make([]rune, 0, 4)
			for len(digits) < 4 && index+1 < len(characters) {
				index++
				digits = append(digits, characters[index])
			}
			if len(digits) != 4 {
				break
			}

			code, ok := parseHex4(string(digits))
			if !ok || code > utf8.MaxRune {
				break
			}
			decoded := rune(code)
			if utf8.ValidRune(decoded) {
				result.WriteRune(decoded)
			}
		default:
			result.WriteRune(characters[index])
		}
	}

	return result.String()
}

// parseHex4 parses exactly four hexadecimal digits.
func parseHex4(digits string) (uint32, bool) {
	if len(digits) != 4 {
		return 0, false
	}

	var value uint32
	for _, character := range digits {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint32(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint32(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint32(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

// parseProperties parses the Java-style registry text.
func parseProperties(text string) map[string]string {
	properties := make(map[string]string)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		line = strings.TrimLeftFunc(line, unicode.IsSpace)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		index, found := separatorIndex(line)
		if !found {
			properties[unescape(strings.TrimRightFunc(line, unicode.IsSpace))] = ""
			continue
		}

		key := strings.TrimRightFunc(line[:index], unicode.IsSpace)
		rest := strings.TrimLeftFunc(line[index:], unicode.IsSpace)
		if strings.HasPrefix(rest, "=") {
			rest = rest[1:]
		} else if strings.HasPrefix(rest, ":") {
			rest = rest[1:]
		}
		value := strings.TrimLeftFunc(rest, unicode.IsSpace)

		properties[unescape(key)] = unescape(value)
	}

	return properties
}

// separatorIndex finds the first unescaped '=', ':' or whitespace rune.
func separatorIndex(line string) (int, bool) {
	escaped := false
	for index, character := range line {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '=' || character == ':' || unicode.IsSpace(character) {
			return index, true
		}
	}
	return 0, false
}
