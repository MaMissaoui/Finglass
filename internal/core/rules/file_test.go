package rules

import (
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func ruleFileBytes(lines []string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

func ruleFileItem(
	pattern string,
	category string,
	priority int64,
	autoApply bool,
	field model.RuleField,
	direction model.RuleDirection,
) RuleFileItem {
	return RuleFileItem{
		Pattern:      pattern,
		CategoryName: category,
		Priority:     priority,
		AutoApply:    autoApply,
		MatchField:   field,
		Direction:    direction,
	}
}

func ruleFileRowText(
	category string,
	pattern string,
	searchIn string,
	appliesTo string,
	priority string,
	autoApply string,
) string {
	return strings.Join([]string{category, pattern, searchIn, appliesTo, priority, autoApply}, "\t")
}

func TestValidFileRoundTripThroughWrite(t *testing.T) {
	rules := []RuleFileItem{
		ruleFileItem(
			"beispiel.?markt|musterladen",
			"Groceries",
			60,
			true,
			model.RuleFieldAny,
			model.RuleDirectionAny,
		),
		ruleFileItem(
			"erika mustermann",
			"Own transfers",
			20,
			true,
			model.RuleFieldPartner,
			model.RuleDirectionAny,
		),
		ruleFileItem(
			"beispiel gmbh",
			"Salary",
			70,
			true,
			model.RuleFieldAny,
			model.RuleDirectionIncome,
		),
		ruleFileItem("raw cash", "Cash", 0, false, model.RuleFieldPurpose, model.RuleDirectionAny),
	}

	text, _ := WriteRuleFile(rules)
	parsed, err := ParseRuleFile([]byte(text), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %v, want none", parsed.Errors)
	}
	if len(parsed.Rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(parsed.Rows))
	}
	for index, rule := range rules {
		written := parsed.Rows[index]
		if written.Line != index+3 {
			t.Errorf("line = %d, want %d: data rows start at file line 3, below magic and header", written.Line, index+3)
		}
		if written.Pattern != rule.Pattern {
			t.Errorf("pattern = %q, want %q", written.Pattern, rule.Pattern)
		}
		if written.Category != rule.CategoryName {
			t.Errorf("category = %q, want %q", written.Category, rule.CategoryName)
		}
		if written.Field != rule.MatchField {
			t.Errorf("field = %q, want %q", written.Field, rule.MatchField)
		}
		if written.Direction != rule.Direction {
			t.Errorf("direction = %q, want %q", written.Direction, rule.Direction)
		}
		if written.Priority != rule.Priority {
			t.Errorf("priority = %d, want %d", written.Priority, rule.Priority)
		}
		if written.AutoApply != rule.AutoApply {
			t.Errorf("auto_apply = %t, want %t", written.AutoApply, rule.AutoApply)
		}
	}
}

func TestTheWrittenFileStartsWithTheMagicAndTheHeaderAndHoldsNoBOM(t *testing.T) {
	text, _ := WriteRuleFile([]RuleFileItem{
		ruleFileItem("beispiel markt", "Groceries", 50, true, model.RuleFieldAny, model.RuleDirectionAny),
	})
	lines := strings.Split(text, "\n")

	if lines[0] != RuleFileMagic {
		t.Errorf("line 0 = %q, want %q", lines[0], RuleFileMagic)
	}
	if lines[1] != RuleFileHeader {
		t.Errorf("line 1 = %q, want %q", lines[1], RuleFileHeader)
	}
	if want := ruleFileRowText("Groceries", "beispiel markt", "any", "any", "50", "true"); lines[2] != want {
		t.Errorf("line 2 = %q, want %q", lines[2], want)
	}
	if strings.HasPrefix(text, "\uFEFF") {
		t.Error("the written file starts with a byte order mark")
	}
}

func TestBOMAndCRLFLineEndingsAreAccepted(t *testing.T) {
	text := "\uFEFF" + RuleFileMagic + "\r\n" + RuleFileHeader + "\r\n" +
		ruleFileRowText("Erika género", "erika", "any", "any", "50", "true") + "\r\n" +
		ruleFileRowText("Groceries", "beispiel markt", "any", "any", "50", "true") + "\r\n"

	parsed, err := ParseRuleFile([]byte(text), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	categories := make([]string, len(parsed.Rows))
	lines := make([]int, len(parsed.Rows))
	for index, row := range parsed.Rows {
		categories[index] = row.Category
		lines[index] = row.Line
	}
	if strings.Join(categories, "\x00") != strings.Join([]string{"Erika género", "Groceries"}, "\x00") {
		t.Errorf("categories = %v, want [Erika género Groceries]", categories)
	}
	if len(lines) != 2 || lines[0] != 3 || lines[1] != 4 {
		t.Errorf("lines = %v, want [3 4]", lines)
	}
}

func TestCommentsAndBlankLinesAreSkippedButTheirLineNumbersStillCount(t *testing.T) {
	parsed, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		"",
		"# a note for the reader",
		"   ",
		RuleFileHeader,
		"",
		"# not a rule",
		ruleFileRowText("Cash", "raw cash", "any", "any", "50", "true"),
		ruleFileRowText("Groceries", "beispiel markt", "any", "any", "50", "true"),
	}), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	categories := make([]string, len(parsed.Rows))
	lines := make([]int, len(parsed.Rows))
	for index, row := range parsed.Rows {
		categories[index] = row.Category
		lines[index] = row.Line
	}
	if strings.Join(categories, "\x00") != strings.Join([]string{"Cash", "Groceries"}, "\x00") {
		t.Errorf("categories = %v, want [Cash Groceries]", categories)
	}
	if len(lines) != 2 || lines[0] != 8 || lines[1] != 9 {
		t.Errorf("lines = %v, want [8 9]", lines)
	}
}

func TestAMissingHeaderIsRejected(t *testing.T) {
	_, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		ruleFileRowText("Cash", "raw cash", "any", "any", "50", "true"),
	}), i18n.En)
	if err == nil {
		t.Fatal("ParseRuleFile returned no error")
	}
	if err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Errorf("message = %q, want %q", err.Error(), i18n.Text(i18n.En, i18n.ErrorRuleFileHeader))
	}
}

func TestAWrongHeaderIsRejected(t *testing.T) {
	_, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		"category\tpattern\tsearch_in\tapplies_to\tpriority\tautoApply",
		ruleFileRowText("Cash", "raw cash", "any", "any", "50", "true"),
	}), i18n.En)
	if err == nil {
		t.Fatal("ParseRuleFile returned no error")
	}
	if err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Errorf("message = %q, want %q", err.Error(), i18n.Text(i18n.En, i18n.ErrorRuleFileHeader))
	}

	// Even a header with the columns in another order is not this file format.
	swapped, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		"pattern\tcategory\tsearch_in\tapplies_to\tpriority\tauto_apply",
		ruleFileRowText("Cash", "raw cash", "any", "any", "50", "true"),
	}), i18n.En)
	if err == nil {
		t.Fatalf("ParseRuleFile returned %v, want an error", swapped)
	}
	if err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Errorf("swapped message = %q, want %q", err.Error(), i18n.Text(i18n.En, i18n.ErrorRuleFileHeader))
	}
}

func TestAFileWithoutAnyContentIsRejectedAsARuleFile(t *testing.T) {
	if _, err := ParseRuleFile(nil, i18n.En); err == nil ||
		err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Fatalf("empty file error = %v, want %q", err, i18n.Text(i18n.En, i18n.ErrorRuleFileHeader))
	}

	if _, err := ParseRuleFile([]byte("just some line"), i18n.En); err == nil ||
		err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Fatalf("headerless file error = %v, want %q", err, i18n.Text(i18n.En, i18n.ErrorRuleFileHeader))
	}
}

func TestAFileWithMoreThan5000DataRowsIsRejected(t *testing.T) {
	tooMany := make([]string, 0, 5001)
	for index := 1; index <= 5001; index++ {
		tooMany = append(tooMany, ruleFileRowText(
			"Cat "+itoa(int64(index)),
			"muster "+itoa(int64(index)),
			"any",
			"any",
			"50",
			"true",
		))
	}
	lines := append([]string{RuleFileMagic, RuleFileHeader}, tooMany...)
	if _, err := ParseRuleFile(ruleFileBytes(lines), i18n.En); err == nil ||
		err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileTooLarge) {
		t.Fatalf("error = %v, want %q", err, i18n.Text(i18n.En, i18n.ErrorRuleFileTooLarge))
	}

	exactlyEnough := append([]string{RuleFileMagic, RuleFileHeader}, tooMany[:5000]...)
	parsed, err := ParseRuleFile(ruleFileBytes(exactlyEnough), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}
	if len(parsed.Rows) != 5000 {
		t.Fatalf("len(rows) = %d, want 5000", len(parsed.Rows))
	}
}

func TestAFileLargerThan1MBIsRejected(t *testing.T) {
	if _, err := ParseRuleFile(make([]byte, RuleFileMaxBytes+1), i18n.En); err == nil ||
		err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileTooLarge) {
		t.Fatalf("error = %v, want %q", err, i18n.Text(i18n.En, i18n.ErrorRuleFileTooLarge))
	}
}

func TestBytesThatAreNotValidUTF8AreRejected(t *testing.T) {
	tail := []byte(RuleFileMagic + "\n" + RuleFileHeader + "\n")
	tail = append(tail, 0xFF)
	if _, err := ParseRuleFile(tail, i18n.En); err == nil ||
		err.Error() != i18n.Text(i18n.En, i18n.ErrorRuleFileEncoding) {
		t.Fatalf("error = %v, want %q", err, i18n.Text(i18n.En, i18n.ErrorRuleFileEncoding))
	}
}

func TestEveryRowErrorKindIsReportedWithItsFileLineWhileTheValidRowsRemain(t *testing.T) {
	longPattern := strings.Repeat("a.", 501)
	parsed, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		RuleFileHeader,
		ruleFileRowText("Cat One", "rewe", "any", "any", "50", "true"),
		"Cat Two\tpattern\tpartner\tincome\t50",
		ruleFileRowText("", "pattern", "any", "any", "50", "true"),
		ruleFileRowText("Cat Three", "[unclosed", "any", "any", "50", "true"),
		ruleFileRowText("Cat Four", longPattern, "any", "any", "50", "true"),
		ruleFileRowText("Cat Five", "pattern", "companions", "any", "50", "true"),
		ruleFileRowText("Cat Six", "pattern", "any", "outgoing", "50", "true"),
		ruleFileRowText("Cat Seven", "pattern", "any", "any", "bold", "true"),
		ruleFileRowText("Cat Eight", "pattern", "any", "any", "50", "maybe"),
		ruleFileRowText("Cat Nine", "edeka", "any", "any", "50", "true"),
	}), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	categories := make([]string, len(parsed.Rows))
	lines := make([]int, len(parsed.Rows))
	for index, row := range parsed.Rows {
		categories[index] = row.Category
		lines[index] = row.Line
	}
	if strings.Join(categories, "\x00") != strings.Join([]string{"Cat One", "Cat Nine"}, "\x00") {
		t.Errorf("categories = %v, want [Cat One Cat Nine]", categories)
	}
	if len(lines) != 2 || lines[0] != 3 || lines[1] != 12 {
		t.Errorf("row lines = %v, want [3 12]", lines)
	}

	errorLines := make([]int, len(parsed.Errors))
	for index, failure := range parsed.Errors {
		errorLines[index] = failure.Line
	}
	wantErrorLines := []int{4, 5, 6, 7, 8, 9, 10, 11}
	if len(errorLines) != len(wantErrorLines) {
		t.Fatalf("error lines = %v, want %v", errorLines, wantErrorLines)
	}
	for index := range wantErrorLines {
		if errorLines[index] != wantErrorLines[index] {
			t.Errorf("error lines = %v, want %v", errorLines, wantErrorLines)
		}
	}

	reasons := make([]string, len(parsed.Errors))
	for index, failure := range parsed.Errors {
		reasons[index] = failure.Reason
	}
	if want := i18n.Format(i18n.En, i18n.RuleFileErrorColumns, i18n.Int(5)); reasons[0] != want {
		t.Errorf("reason 0 = %q, want %q", reasons[0], want)
	}
	if want := i18n.Text(i18n.En, i18n.RuleFileErrorCategory); reasons[1] != want {
		t.Errorf("reason 1 = %q, want %q", reasons[1], want)
	}
	if invalid := ValidateRulePattern("[unclosed", i18n.En); invalid == nil {
		t.Error("ValidateRulePattern([unclosed]) = nil, want an error")
	} else if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorPattern,
		i18n.Str(*invalid),
	); reasons[2] != want {
		t.Errorf("reason 2 = %q, want %q", reasons[2], want)
	}
	if want := i18n.Text(i18n.En, i18n.RuleFileErrorPatternLength); reasons[3] != want {
		t.Errorf("reason 3 = %q, want %q", reasons[3], want)
	}
	if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorSearchIn,
		i18n.Str("companions"),
	); reasons[4] != want {
		t.Errorf("reason 4 = %q, want %q", reasons[4], want)
	}
	if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorAppliesTo,
		i18n.Str("outgoing"),
	); reasons[5] != want {
		t.Errorf("reason 5 = %q, want %q", reasons[5], want)
	}
	if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorPriority,
		i18n.Str("bold"),
	); reasons[6] != want {
		t.Errorf("reason 6 = %q, want %q", reasons[6], want)
	}
	if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorAutoApply,
		i18n.Str("maybe"),
	); reasons[7] != want {
		t.Errorf("reason 7 = %q, want %q", reasons[7], want)
	}
}

func TestAnUnknownSearchInKeyIsAnErrorNeverASilentFallbackToAny(t *testing.T) {
	parsed, err := ParseRuleFile(ruleFileBytes([]string{
		RuleFileMagic,
		RuleFileHeader,
		ruleFileRowText("Groceries", "beispiel markt", "partner", "any", "50", "true"),
		ruleFileRowText("Cash", "raw", "purpose only", "any", "50", "true"),
	}), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	if len(parsed.Rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: the partner row is fine, the unknown key is rejected", len(parsed.Rows))
	}
	for _, row := range parsed.Rows {
		if row.Field == model.RuleFieldPurpose {
			t.Error("an unknown key was mapped onto a field")
		}
	}
	if len(parsed.Errors) != 1 || parsed.Errors[0].Line != 4 {
		t.Fatalf("errors = %v, want one error on line 4", parsed.Errors)
	}
	if want := i18n.Format(
		i18n.En,
		i18n.RuleFileErrorSearchIn,
		i18n.Str("purpose only"),
	); parsed.Errors[0].Reason != want {
		t.Errorf("reason = %q, want %q", parsed.Errors[0].Reason, want)
	}
}

func TestARuleWithATabCROrLFInItsPatternOrCategoryIsSkippedOnWriteAndCounted(t *testing.T) {
	rules := []RuleFileItem{
		ruleFileItem("musterladen", "Groceries", 50, true, model.RuleFieldAny, model.RuleDirectionAny),
		ruleFileItem("bad\tregex", "Cash", 50, true, model.RuleFieldAny, model.RuleDirectionAny),
		ruleFileItem("normal", "Bad\rName", 50, true, model.RuleFieldAny, model.RuleDirectionAny),
		ruleFileItem(
			"newline\npattern",
			"Other",
			50,
			true,
			model.RuleFieldAny,
			model.RuleDirectionAny,
		),
	}

	text, skipped := WriteRuleFile(rules)
	if skipped != 3 {
		t.Errorf("skipped = %d, want 3: three rules cannot be written", skipped)
	}
	if got := SkippedRules(rules); got != 3 {
		t.Errorf("SkippedRules = %d, want 3", got)
	}

	parsed, err := ParseRuleFile([]byte(text), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}
	if len(parsed.Rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(parsed.Rows))
	}
	if parsed.Rows[0].Pattern != "musterladen" {
		t.Errorf("pattern = %q, want %q", parsed.Rows[0].Pattern, "musterladen")
	}
}

func TestValidationReportsBlankAndInvalidPatterns(t *testing.T) {
	if got := ValidateRulePattern("   ", i18n.En); got == nil {
		t.Errorf("ValidateRulePattern(blank) = nil, want %q", i18n.Text(i18n.En, i18n.ErrorPatternRequired))
	} else if want := i18n.Text(i18n.En, i18n.ErrorPatternRequired); *got != want {
		t.Errorf("ValidateRulePattern(blank) = %q, want %q", *got, want)
	}
	if got := ValidateRulePattern("(", i18n.En); got == nil {
		t.Error("ValidateRulePattern(() = nil, want an error")
	}
	if got := ValidateRulePattern("beispiel", i18n.En); got != nil {
		t.Errorf("ValidateRulePattern(beispiel) = %q, want nil", *got)
	}
}
