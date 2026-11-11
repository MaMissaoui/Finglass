// Package rules holds the user rule patterns shared by the Go core.
package rules

import (
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// matchTimeout bounds one regular-expression match. A pattern that backtracks
// longer is treated as a non-match, exactly like a Rust runtime error.
const matchTimeout = 100 * time.Millisecond

// Pattern is a compiled rule pattern: a case-insensitive regular expression
// when the text parses, and a lower-cased substring test otherwise.
//
// The zero value is not usable; build one with [Compile].
type Pattern struct {
	regex     *regexp2.Regexp
	substring string
}

// Compile compiles pattern the way Rust's rule matcher does. It never fails: a
// pattern that does not parse becomes a case-insensitive substring test.
//
// The regular expression is case-insensitive and uses Unicode classes, so
// `\w`, `\d`, `\s` and `\b` keep the meaning they have in Rust's regex engine.
func Compile(pattern string) *Pattern {
	regex, err := regexp2.Compile(translateNamedGroups(pattern), regexp2.IgnoreCase|regexp2.Unicode)
	if err != nil {
		return &Pattern{substring: lower(pattern)}
	}
	regex.MatchTimeout = matchTimeout
	return &Pattern{regex: regex}
}

// translateNamedGroups rewrites Python-style named groups, `(?P<name>…)`, to
// the .NET spelling regexp2 understands, `(?<name>…)`. Rust's engine accepts
// both; without this the whole pattern would fall back to a substring test.
// The rewrite skips escapes and character classes, where the same bytes are
// literal text.
func translateNamedGroups(pattern string) string {
	if !strings.Contains(pattern, "(?P<") {
		return pattern
	}

	var out strings.Builder
	out.Grow(len(pattern))
	inClass := false
	escaped := false

	for index := 0; index < len(pattern); index++ {
		character := pattern[index]
		switch {
		case escaped:
			out.WriteByte(character)
			escaped = false
		case character == '\\':
			out.WriteByte(character)
			escaped = true
		case character == '[':
			inClass = true
			out.WriteByte(character)
		case character == ']' && inClass:
			inClass = false
			out.WriteByte(character)
		case !inClass && strings.HasPrefix(pattern[index:], "(?P<"):
			out.WriteString("(?<")
			index += len("(?P<") - 1
		default:
			out.WriteByte(character)
		}
	}
	return out.String()
}

// IsRegex reports whether the pattern compiled as a regular expression. When
// it is false, [Pattern.IsMatch] runs a lower-cased substring test.
func (p *Pattern) IsRegex() bool {
	return p != nil && p.regex != nil
}

// IsMatch reports whether text matches the pattern. A match error, such as the
// timeout a catastrophic pattern reaches, counts as no match.
func (p *Pattern) IsMatch(text string) bool {
	if p == nil {
		return false
	}
	if p.regex == nil {
		return strings.Contains(lower(text), p.substring)
	}
	matched, err := p.regex.MatchString(text)
	return err == nil && matched
}

// lower is full Unicode lower-casing, like Rust's str::to_lowercase: `İ`
// becomes `i` + U+0307 and a word-final `Σ` becomes `ς`. strings.ToLower only
// does the simple one-rune mapping. A cases.Caser is not safe for concurrent
// use, so each call makes its own.
func lower(s string) string {
	return cases.Lower(language.Und).String(s)
}
