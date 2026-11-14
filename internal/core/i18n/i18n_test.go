package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// placeholderAllowList are keys whose FinDesk texts already differ in their placeholders
// between languages. They are kept verbatim, so the consistency check cannot demand the same
// placeholder set for them.
//
//   - PendingLeftOut, UnreadableCount, SyncSummaryCategorized, SyncSummaryDuplicates,
//     ImportSummaryAuto, ImportSummaryKnown, ImportSummaryWrongCurrency, ImportSummaryDeleted,
//     SelectedCount, DeleteSelectedCount: French adds a plural marker for the same %d.
//   - ImportFilesSkipped: German repeats the plural marker for the verb.
//   - WrongCurrencySkipped, MessageAutoCategorized, ImportSummaryProbable,
//     ImportSummaryDeleted: German/French add a second plural marker for the verb.
//   - RuleImportDuplicates: French uses {1:s||} for an optional letter, which is not a plural
//     marker under FinDesk's regex; English and German use a real plural marker.
var placeholderAllowList = []Key{
	PendingLeftOut,
	UnreadableCount,
	ImportFilesSkipped,
	WrongCurrencySkipped,
	RuleImportDuplicates,
	MessageAutoCategorized,
	SyncSummaryCategorized,
	SyncSummaryDuplicates,
	ImportSummaryAuto,
	ImportSummaryKnown,
	ImportSummaryProbable,
	ImportSummaryWrongCurrency,
	ImportSummaryDeleted,
	SelectedCount,
	DeleteSelectedCount,
}

// paymentNameExceptions are variant names that legitimately carry a payment word: these four
// category strings describe transfers between the user's own accounts and stay part of category
// management.
var paymentNameExceptions = []string{
	"CategoryIsTransfer",
	"CategoryIsTransferHint",
	"CategoryTransferLabel",
	"CategorySplitTransfers",
	// Partner field label in the transactions table, not a payment feature.
	"PartnerPayee",
	// Reports statistic for recurring costs, not bank payments.
	"RecurringPayments",
	// Default category for transfers between the user's own accounts, not a payment feature.
	"DefaultCategoryOwnTransfers",
}

func TestEveryKeyNameRoundTrips(t *testing.T) {
	for _, key := range All() {
		name := key.String()
		got, ok := KeyFromName(name)
		if !ok || got != key {
			t.Errorf("KeyFromName(%q) = %v, %v; want %v, true", name, got, ok, key)
		}
	}
	if _, ok := KeyFromName("NoSuchKey"); ok {
		t.Error(`KeyFromName("NoSuchKey") found a key`)
	}
	if _, ok := KeyFromName(""); ok {
		t.Error(`KeyFromName("") found a key`)
	}
}

func TestEveryKeyHasTextInEveryLanguage(t *testing.T) {
	for _, key := range All() {
		for _, lang := range Languages {
			if Text(lang, key) == "" {
				t.Errorf("%s has no %s text", key, lang.Code())
			}
		}
	}
}

func TestNoPaymentKeyRemains(t *testing.T) {
	for _, key := range All() {
		name := key.String()
		for _, word := range []string{"Payment", "Transfer", "StandingOrder", "Scheduled", "Vop", "Payee"} {
			if strings.Contains(name, word) && !slices.Contains(paymentNameExceptions, name) {
				t.Errorf("%s looks like a payment key", name)
			}
		}
	}
}

func TestRestoredNonPaymentKeysHaveFinDeskEnglishText(t *testing.T) {
	expected := []struct {
		key  Key
		text string
	}{
		{TypeChecking, "Checking"},
		{TypeSavings, "Savings"},
		{TypeCreditCard, "Credit card"},
		{TypeLoan, "Loan"},
		{TypeDepot, "Depot"},
		{PeriodThisMonth, "This month"},
		{PeriodLastMonth, "Last month"},
		{PeriodLast3Months, "Last 3 months"},
		{PeriodThisYear, "This year"},
		{PeriodLastYear, "Last year"},
		{PeriodAllTime, "All time"},
		{DirectionAll, "All"},
		{DirectionIncome, "Income"},
		{DirectionExpenses, "Expenses"},
		{CadenceWeekly, "Weekly"},
		{CadenceMonthly, "Monthly"},
		{CadenceQuarterly, "Quarterly"},
		{CadenceYearly, "Yearly"},
		{TabAllDocuments, "All documents"},
		{TabUnlinked, "Unlinked"},
		{TabLinked, "Linked to transaction"},
		{PartnerPayee, "Partner / payee"},
		{RecurringPayments, "Recurring payments"},
		{DefaultCategoryOwnTransfers, "Own transfers"},
	}
	for _, want := range expected {
		if got := Text(En, want.key); got != want.text {
			t.Errorf("%s = %q, want %q", want.key, got, want.text)
		}
	}
}

func TestPlaceholdersAreTheSameInEveryLanguage(t *testing.T) {
	for _, key := range All() {
		en := placeholderTokens(Text(En, key))
		de := placeholderTokens(Text(De, key))
		fr := placeholderTokens(Text(Fr, key))
		if !slices.Equal(en, de) || !slices.Equal(en, fr) {
			if !slices.Contains(placeholderAllowList, key) {
				t.Errorf("%s uses different placeholders: en=%v, de=%v, fr=%v", key, en, de, fr)
			}
		}
	}
}

func TestFormatsAStringPlaceholder(t *testing.T) {
	if got, want := Format(En, ChangeCategory, Str("Groceries")), "Change category: Groceries"; got != want {
		t.Errorf("English: got %q, want %q", got, want)
	}
	if got, want := Format(De, ChangeCategory, Str("Lebensmittel")), "Kategorie ändern: Lebensmittel"; got != want {
		t.Errorf("German: got %q, want %q", got, want)
	}
	if got, want := Format(Fr, ChangeCategory, Str("Courses")), "Changer la catégorie : Courses"; got != want {
		t.Errorf("French: got %q, want %q", got, want)
	}
}

func TestFormatsPositionalArguments(t *testing.T) {
	// %1$d and %2$d name their argument, independent of their order in the text.
	if got, want := Format(En, PdfPageOf, Int(2), Int(7)), "Page 2 of 7"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, PdfPageOf, Int(7), Int(2)), "Page 7 of 2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, OtherCurrenciesExcluded, Str("EUR"), Int(3)), "All amounts in EUR. 3 bookings in other currencies are not included."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatsAnIntegerPlaceholder(t *testing.T) {
	if got, want := Format(En, SelectedCount, Int(3)), "3 selected"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatsALiteralPercent(t *testing.T) {
	if got, want := Format(En, ExtractedTextConfidence, Int(87)), "Extracted text (confidence 87%)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPluralInEnglishUsesTheCount(t *testing.T) {
	if got, want := Format(En, BookingCount, Int(1)), "1 booking"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, BookingCount, Int(2)), "2 bookings"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPluralInFrenchTreatsZeroAsSingular(t *testing.T) {
	if got, want := Format(Fr, BookingCount, Int(0)), "0 écriture"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(Fr, BookingCount, Int(1)), "1 écriture"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(Fr, BookingCount, Int(2)), "2 écritures"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMissingAndMismatchedArgumentsLeaveThePlaceholder(t *testing.T) {
	if got, want := Format(En, ChangeCategory), "Change category: %s"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, SelectedCount), "%d selected"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, SelectedCount, Str("three")), "%d selected"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// The plural marker still picks its plural form when the count is missing.
	if got, want := Format(En, CategorySplitMore), "%1$d more categories"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCountsPickSingularOrPluralPerLanguage(t *testing.T) {
	if got, want := Format(En, BookingCount, Int(1)), "1 booking"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, BookingCount, Int(0)), "0 bookings"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(De, BookingCount, Int(2)), "2 Buchungen"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(De, TransactionsSummary, Int(1), Str("€0.00"), Str("€3.50")), "1 Umsatz · Eingang €0.00 · Ausgang €3.50"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(Fr, BookingCount, Int(0)), "0 écriture"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(Fr, BookingCount, Int(2)), "2 écritures"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheCountCanBeAnyArgumentAndDriveSeveralWords(t *testing.T) {
	if got, want := Format(En, AccountsTotal, Str("€5.00"), Int(1)), "Total €5.00 across 1 account"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, RestoreCreated, Str("today"), Int(1), Int(2)), "Created today · 1 transaction · 2 documents"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, WrongCurrencySkipped, Int(1), Str("EUR")), "1 booking is in another currency than this EUR account and will be skipped."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := Format(En, OtherCurrenciesExcluded, Str("EUR"), Int(3)), "All amounts in EUR. 3 bookings in other currencies are not included."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEveryPluralMarkerPointsAtANumericArgument(t *testing.T) {
	for _, key := range All() {
		for _, lang := range Languages {
			value := Text(lang, key)
			conv := conversions(value)
			for _, index := range pluralIndices(value) {
				conversion, ok := conv[index]
				if !ok || conversion != 'd' {
					t.Errorf("%s %s: {%d:…} needs a %%d as argument %d", lang.Code(), key, index, index)
				}
			}
		}
	}
}

func TestNoStringFallsBackToBracketedPlurals(t *testing.T) {
	for _, key := range All() {
		for _, lang := range Languages {
			value := Text(lang, key)
			for _, suffix := range []string{"(s)", "(en)", "(e)", "(n)"} {
				if position := strings.Index(value, suffix); position >= 0 {
					var before rune
					hasBefore := position > 0
					if hasBefore {
						before, _ = utf8.DecodeLastRuneInString(value[:position])
					}
					if hasBefore && (unicode.IsLetter(before) || unicode.IsNumber(before)) {
						t.Errorf("%s %s: %q — use braces instead of %s", lang.Code(), key, value, suffix)
					}
				}
			}
			if strings.Contains(value, "Konto/Konten") || strings.Contains(value, "é(s)") || strings.Contains(value, "e(s)") {
				t.Errorf("%s %s: %q — use braces instead of a bracketed plural", lang.Code(), key, value)
			}
		}
	}
}

func TestLanguageCodesAndNamesRoundTrip(t *testing.T) {
	for _, lang := range Languages {
		if got, ok := LanguageFromCode(lang.Code()); !ok || got != lang {
			t.Errorf("LanguageFromCode(%q) = %v, %v; want %v, true", lang.Code(), got, ok, lang)
		}
		upper := strings.ToUpper(lang.Code())
		if got, ok := LanguageFromCode(upper); !ok || got != lang {
			t.Errorf("LanguageFromCode(%q) = %v, %v; want %v, true", upper, got, ok, lang)
		}
		if lang.NativeName() == "" {
			t.Errorf("%v has no native name", lang)
		}
	}
	if _, ok := LanguageFromCode("es"); ok {
		t.Error(`LanguageFromCode("es") found a language`)
	}
	if _, ok := LanguageFromCode(""); ok {
		t.Error(`LanguageFromCode("") found a language`)
	}
}

func TestSingularRulesMatchFinDesk(t *testing.T) {
	if !Fr.IsSingular(0) {
		t.Error("French zero should be singular")
	}
	if !Fr.IsSingular(1) {
		t.Error("French one should be singular")
	}
	if Fr.IsSingular(2) {
		t.Error("French two should be plural")
	}
	for _, lang := range []Language{En, De} {
		if lang.IsSingular(0) {
			t.Errorf("%s zero should be plural", lang.Code())
		}
		if !lang.IsSingular(1) {
			t.Errorf("%s one should be singular", lang.Code())
		}
		if lang.IsSingular(2) {
			t.Errorf("%s two should be plural", lang.Code())
		}
	}
}

func TestNumKeys(t *testing.T) {
	if NumKeys != 895 {
		t.Errorf("NumKeys = %d, want 895", NumKeys)
	}
	if len(All()) != NumKeys {
		t.Errorf("len(All()) = %d, want %d", len(All()), NumKeys)
	}
}

func TestTablesMatchTheRustGolden(t *testing.T) {
	// #nosec G304 -- fixed path below the repository root.
	data, err := os.ReadFile("../../../testdata/golden/i18n/keys.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden map[string]map[string]string
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(golden) != NumKeys {
		t.Fatalf("golden has %d keys, want %d", len(golden), NumKeys)
	}
	for _, key := range All() {
		name := key.String()
		texts, ok := golden[name]
		if !ok {
			t.Errorf("golden is missing %q", name)
			continue
		}
		for _, lang := range Languages {
			want, ok := texts[lang.Code()]
			if !ok {
				t.Errorf("golden is missing %q/%q", name, lang.Code())
				continue
			}
			if got := Text(lang, key); got != want {
				t.Errorf("%s/%s = %q, want %q", lang.Code(), name, got, want)
			}
		}
	}
	for name := range golden {
		if _, ok := KeyFromName(name); !ok {
			t.Errorf("golden key %q is not in All()", name)
		}
	}
}

func TestFormatEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     []Arg
		want     string
	}{
		{"zero plural index uses the plural", "{0:a|b}", []Arg{Int(1)}, "b"},
		{"overflowing plural index is not a marker", "{99999999999999999999:a|b}", nil, "{99999999999999999999:a|b}"},
		{"unterminated plural marker is not a marker", "{1:a|b", nil, "{1:a|b"},
		{"zero explicit index is not a placeholder", "%0$s", []Arg{Str("x")}, "%0$s"},
		{"trailing percent", "100%", nil, "100%"},
		{"unsupported conversion is copied", "%5.2f", nil, "%5.2f"},
		{"explicit indices do not advance the implicit one", "%2$s %1$s %s", []Arg{Str("a"), Str("b")}, "b a a"},
		{"a string conversion accepts an integer", "%s", []Arg{Int(-3)}, "-3"},
		{"an integer conversion rejects a string", "%d", []Arg{Str("x")}, "%d"},
		{"flags and width are parsed but ignored", "%-5d", []Arg{Int(7)}, "7"},
		{"non-ASCII around a placeholder", "ä%sö", []Arg{Str("ü")}, "äüö"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatText(En, test.template, test.args); got != test.want {
				t.Errorf("formatText(%q) = %q, want %q", test.template, got, test.want)
			}
		})
	}
}

// placeholderTokens returns normalized placeholders: a conversion and its 1-based argument
// index, plus pluralN markers.
func placeholderTokens(template string) []string {
	conv := conversions(template)
	tokens := make([]string, 0, len(conv))
	for index, conversion := range conv {
		tokens = append(tokens, fmt.Sprintf("%c%d", conversion, index))
	}
	for _, index := range pluralIndices(template) {
		tokens = append(tokens, fmt.Sprintf("plural%d", index))
	}
	sort.Strings(tokens)
	return tokens
}

// conversions returns the conversions in argument order, mirroring Java's String.format.
func conversions(template string) map[int]byte {
	result := make(map[int]byte)
	next := 1
	i := 0
	for i < len(template) {
		if template[i] != '%' {
			i++
			continue
		}
		if i+1 < len(template) && template[i+1] == '%' {
			i += 2
			continue
		}
		start := i + 1
		j := start
		for j < len(template) && isASCIIDigit(template[j]) {
			j++
		}
		explicit := 0
		hasExplicit := false
		if j > start && j < len(template) && template[j] == '$' {
			if index, err := strconv.Atoi(template[start:j]); err == nil {
				explicit = index
				hasExplicit = true
			}
			j++
		} else {
			j = start
		}
		for j < len(template) && isFlag(template[j]) {
			j++
		}
		for j < len(template) && isASCIIDigit(template[j]) {
			j++
		}
		if j < len(template) && template[j] == '.' {
			j++
			for j < len(template) && isASCIIDigit(template[j]) {
				j++
			}
		}
		if j < len(template) && (template[j] == 's' || template[j] == 'd') {
			conversion := template[j]
			if hasExplicit {
				result[explicit] = conversion
			} else {
				result[next] = conversion
				next++
			}
			i = j + 1
		} else {
			i = start
		}
	}
	return result
}

func pluralIndices(template string) []int {
	var indices []int
	i := 0
	for i < len(template) {
		if template[i] == '{' {
			if marker, ok := pluralMarker(template, i); ok {
				indices = append(indices, marker.index)
				i = marker.end
				continue
			}
		}
		i++
	}
	return indices
}
