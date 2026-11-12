// Package rules holds the user rule patterns shared by the Go core.
package rules

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"github.com/dlclark/regexp2/syntax"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

const (
	// RuleFileMagic is the first line of every written file; on reading it is
	// just another comment line.
	RuleFileMagic = "# FinDesk rules v1"
	// RuleFileHeader is the column header every file must have.
	RuleFileHeader = "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply"
	// RuleFileMaxBytes is the largest accepted file, in bytes.
	RuleFileMaxBytes = 1024 * 1024
	// RuleFileMaxRows is the largest accepted number of data rows.
	RuleFileMaxRows = 5000
)

const (
	maxCategoryLength = 100
	maxPatternLength  = 1000
)

// RuleFileRow is one usable rule row of a rule file, with the 1-based file
// line it was read from.
//
// Pattern and category name are trimmed.
type RuleFileRow struct {
	// Line is the 1-based line in the file.
	Line int
	// Category is the category name.
	Category string
	// Pattern is the pattern.
	Pattern string
	// Field is the field the pattern is matched against.
	Field model.RuleField
	// Direction is the booking direction the rule applies to.
	Direction model.RuleDirection
	// Priority is the priority, highest first.
	Priority int64
	// AutoApply says whether matching bookings are categorized automatically.
	AutoApply bool
}

// RuleFileError is one row that could not be read; Reason is a localized
// string.
type RuleFileError struct {
	// Line is the 1-based line in the file.
	Line int
	// Reason is the localized reason.
	Reason string
}

// ParsedRuleFile holds all usable rows of a rule file plus everything that had
// to be skipped.
type ParsedRuleFile struct {
	// Rows are the usable rows.
	Rows []RuleFileRow
	// Errors are the rows that could not be read.
	Errors []RuleFileError
}

// RuleFileParseError is a whole-file problem, with a localized message.
type RuleFileParseError struct {
	// Message is the localized message.
	Message string
}

// Error returns the localized message.
func (e *RuleFileParseError) Error() string {
	return e.Message
}

func newRuleFileParseError(lang i18n.Language, key i18n.Key) *RuleFileParseError {
	return &RuleFileParseError{Message: i18n.Text(lang, key)}
}

// RuleFileItem is a rule as the file writer sees it: the pattern, the
// category's display name and the rule's fields.
type RuleFileItem struct {
	// Pattern is the text pattern.
	Pattern string
	// CategoryName is the category's display name.
	CategoryName string
	// Priority is the priority, highest first.
	Priority int64
	// AutoApply says whether matching bookings are categorized automatically.
	AutoApply bool
	// MatchField is the field the pattern is matched against.
	MatchField model.RuleField
	// Direction is the booking direction the rule applies to.
	Direction model.RuleDirection
}

// ValidateRulePattern returns why pattern is unusable as a rule pattern, as a
// localized user message, or nil when it is fine.
func ValidateRulePattern(pattern string, lang i18n.Language) *string {
	if strings.TrimSpace(pattern) == "" {
		message := i18n.Text(lang, i18n.ErrorPatternRequired)
		return &message
	}

	if _, err := regexp2.Compile(translateNamedGroups(pattern), regexp2.Unicode); err != nil {
		reason := fancyRegexError(pattern, err)
		message := i18n.Format(lang, i18n.ErrorInvalidRegex, i18n.Str(reason))
		return &message
	}
	return nil
}

// fancyRegexError approximates how Rust's fancy-regex displays a parse error,
// so rule-file error rows read like the Rust text. The position is always the
// end of the pattern: that is what fancy-regex reports for an unclosed class
// or group (the golden case), but other errors may point elsewhere in Rust.
// Errors without a mapping keep regexp2's own text.
func fancyRegexError(pattern string, err error) string {
	position := len(pattern)
	var syntaxError *syntax.Error
	if errors.As(err, &syntaxError) {
		switch syntaxError.Code {
		case syntax.ErrUnterminatedBracket,
			syntax.ErrInvalidCharRange,
			syntax.ErrReversedCharRange,
			syntax.ErrBadClassInCharRange:
			return fmt.Sprintf("Parsing error at position %d: Invalid character class", position)
		case syntax.ErrMissingParen:
			return fmt.Sprintf(
				"Parsing error at position %d: Opening parenthesis without closing parenthesis",
				position,
			)
		case syntax.ErrInvalidRepeatSize, syntax.ErrInvalidRepeatOp, syntax.ErrMissingRepeatArgument:
			return fmt.Sprintf("Parsing error at position %d: Invalid repeat syntax", position)
		case syntax.ErrIllegalEndEscape:
			return fmt.Sprintf("Parsing error at position %d: Backslash without following character", position)
		case syntax.ErrSubtractionMustBeLast:
			return fmt.Sprintf("Parsing error at position %d: Invalid character class", position)
		}
	}
	return err.Error()
}

// ParseRuleFile parses the bytes of a rule file.
//
// Whole-file problems are returned as a [RuleFileParseError] with a localized
// message. A bad data row is reported in [ParsedRuleFile.Errors] while the
// valid rows remain.
func ParseRuleFile(data []byte, lang i18n.Language) (*ParsedRuleFile, error) {
	if len(data) > RuleFileMaxBytes {
		return nil, newRuleFileParseError(lang, i18n.ErrorRuleFileTooLarge)
	}
	if !utf8.Valid(data) {
		return nil, newRuleFileParseError(lang, i18n.ErrorRuleFileEncoding)
	}

	text := string(data)
	// A leading byte-order mark decodes to U+FEFF; drop it before the first line.
	body := strings.TrimPrefix(text, "\uFEFF")

	type sourceLine struct {
		line int
		text string
	}
	content := make([]sourceLine, 0, 64)
	for index, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), "#") {
			continue
		}
		content = append(content, sourceLine{line: index + 1, text: line})
	}

	if len(content) == 0 || strings.TrimSpace(content[0].text) != RuleFileHeader {
		return nil, newRuleFileParseError(lang, i18n.ErrorRuleFileHeader)
	}
	if len(content)-1 > RuleFileMaxRows {
		return nil, newRuleFileParseError(lang, i18n.ErrorRuleFileTooLarge)
	}

	parsed := &ParsedRuleFile{}
	for _, source := range content[1:] {
		row, reason := readRuleFileRow(source.line, source.text, lang)
		if reason != nil {
			parsed.Errors = append(parsed.Errors, RuleFileError{Line: source.line, Reason: *reason})
			continue
		}
		parsed.Rows = append(parsed.Rows, *row)
	}
	return parsed, nil
}

// readRuleFileRow returns the usable [RuleFileRow] for one data line, or the
// first problem found.
//
// Two invalid keys never fall back to ANY: the row is rejected with its own
// reason.
func readRuleFileRow(line int, text string, lang i18n.Language) (*RuleFileRow, *string) {
	columns := strings.Split(text, "\t")
	if len(columns) != 6 {
		reason := i18n.Format(lang, i18n.RuleFileErrorColumns, i18n.Int(int64(len(columns))))
		return nil, &reason
	}

	category := strings.TrimSpace(columns[0])
	if category == "" || utf16Length(category) > maxCategoryLength {
		reason := i18n.Text(lang, i18n.RuleFileErrorCategory)
		return nil, &reason
	}

	pattern := strings.TrimSpace(columns[1])
	if utf16Length(pattern) > maxPatternLength {
		reason := i18n.Text(lang, i18n.RuleFileErrorPatternLength)
		return nil, &reason
	}
	if reason := ValidateRulePattern(pattern, lang); reason != nil {
		formatted := i18n.Format(lang, i18n.RuleFileErrorPattern, i18n.Str(*reason))
		return nil, &formatted
	}

	searchIn := strings.TrimSpace(columns[2])
	field, ok := model.RuleFieldFromStored(lower(searchIn))
	if !ok {
		reason := i18n.Format(lang, i18n.RuleFileErrorSearchIn, i18n.Str(searchIn))
		return nil, &reason
	}

	appliesTo := strings.TrimSpace(columns[3])
	direction, ok := model.RuleDirectionFromStored(lower(appliesTo))
	if !ok {
		reason := i18n.Format(lang, i18n.RuleFileErrorAppliesTo, i18n.Str(appliesTo))
		return nil, &reason
	}

	priorityText := strings.TrimSpace(columns[4])
	priority, err := strconv.ParseInt(priorityText, 10, 32)
	if err != nil {
		reason := i18n.Format(lang, i18n.RuleFileErrorPriority, i18n.Str(priorityText))
		return nil, &reason
	}

	autoApplyText := strings.TrimSpace(columns[5])
	if autoApplyText != "true" && autoApplyText != "false" {
		reason := i18n.Format(lang, i18n.RuleFileErrorAutoApply, i18n.Str(autoApplyText))
		return nil, &reason
	}

	return &RuleFileRow{
		Line:      line,
		Category:  category,
		Pattern:   pattern,
		Field:     field,
		Direction: direction,
		Priority:  priority,
		AutoApply: autoApplyText == "true",
	}, nil
}

func utf16Length(text string) int {
	return len(utf16.Encode([]rune(text)))
}

// SkippedRules returns how many rules cannot be written at all.
func SkippedRules(rules []RuleFileItem) int {
	skipped := 0
	for _, rule := range rules {
		if !writableRule(rule) {
			skipped++
		}
	}
	return skipped
}

func writableRule(rule RuleFileItem) bool {
	return !strings.ContainsAny(rule.Pattern, "\t\r\n") && !strings.ContainsAny(rule.CategoryName, "\t\r\n")
}

// WriteRuleFile returns the rule file text for rules — magic line, header,
// then one line per rule, LF line endings, no BOM — and the number of skipped
// rules.
//
// A rule whose pattern or category name contains a TAB, CR or LF cannot be
// represented; it is skipped and counted.
func WriteRuleFile(rules []RuleFileItem) (string, int) {
	var text strings.Builder
	text.WriteString(RuleFileMagic)
	text.WriteByte('\n')
	text.WriteString(RuleFileHeader)

	skipped := 0
	for _, rule := range rules {
		if !writableRule(rule) {
			skipped++
			continue
		}

		text.WriteByte('\n')
		text.WriteString(strings.Join([]string{
			rule.CategoryName,
			rule.Pattern,
			rule.MatchField.AsStr(),
			rule.Direction.AsStr(),
			strconv.FormatInt(rule.Priority, 10),
			strconv.FormatBool(rule.AutoApply),
		}, "\t"))
	}
	text.WriteByte('\n')

	return text.String(), skipped
}
