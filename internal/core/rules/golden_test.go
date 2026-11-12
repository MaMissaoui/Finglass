package rules

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

func readGolden(t *testing.T, area string, name string) []byte {
	t.Helper()

	path := filepath.Join(repoRoot(t), "testdata", "golden", area, name)
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func readInput(t *testing.T, area string, name string) []byte {
	t.Helper()

	path := filepath.Join(repoRoot(t), "testdata", "inputs", area, name)
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func decodeGolden(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
}

func equalOptionalString(got *string, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func equalOptionalInt(got *int, want *int) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func equalOptionalInt64(got *int64, want *int64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func showOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func showOptionalInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func showOptionalInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

type goldenDefaultRule struct {
	Index      int    `json:"index"`
	CategoryID int64  `json:"category_id"`
	Category   string `json:"category"`
	Pattern    string `json:"pattern"`
	Field      string `json:"field"`
	Direction  string `json:"direction"`
	Priority   int64  `json:"priority"`
}

type goldenMatcherBooking struct {
	Index           int     `json:"index"`
	Date            string  `json:"date"`
	Amount          string  `json:"amount"`
	PartnerName     *string `json:"partner_name"`
	Purpose         *string `json:"purpose"`
	Shop            *string `json:"shop"`
	SelectedIndex   *int    `json:"selected_index"`
	SelectedPattern *string `json:"selected_pattern"`
	CategoryID      *int64  `json:"category_id"`
	Category        *string `json:"category"`
}

type goldenShopRule struct {
	Pattern    string `json:"pattern"`
	Field      string `json:"field"`
	Direction  string `json:"direction"`
	CategoryID int64  `json:"category_id"`
}

type goldenShopCase struct {
	Name               string           `json:"name"`
	Purpose            string           `json:"purpose"`
	Partner            *string          `json:"partner"`
	Rules              []goldenShopRule `json:"rules"`
	SelectedIndex      *int             `json:"selected_index"`
	SelectedCategoryID *int64           `json:"selected_category_id"`
}

type goldenMatcher struct {
	DefaultRules []goldenDefaultRule    `json:"default_rules"`
	Bookings     []goldenMatcherBooking `json:"bookings"`
	ShopCases    []goldenShopCase       `json:"shop_cases"`
}

func TestRulesMatcherGolden(t *testing.T) {
	var golden goldenMatcher
	decodeGolden(t, readGolden(t, "rules", "matcher.json"), &golden)

	parsed, err := ParseRuleFile(readInput(t, "rules", "default_rules.tsv"), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile(default_rules.tsv): %v", err)
	}

	specs := make([]RuleSpec, 0, len(parsed.Rows))
	names := make(map[model.CategoryID]string, len(parsed.Rows))
	patterns := make([]*Pattern, 0, len(parsed.Rows))
	for index, row := range parsed.Rows {
		categoryID := model.CategoryID(index + 1)
		names[categoryID] = row.Category
		specs = append(specs, RuleSpec{
			Pattern:    row.Pattern,
			CategoryID: categoryID,
			Field:      row.Field,
			Direction:  row.Direction,
		})
		patterns = append(patterns, Compile(row.Pattern))
	}
	matcher := NewRuleMatcher(specs)

	if len(golden.DefaultRules) != len(parsed.Rows) {
		t.Fatalf("default_rules: golden has %d rows, parsed %d", len(golden.DefaultRules), len(parsed.Rows))
	}
	for index, want := range golden.DefaultRules {
		row := parsed.Rows[index]
		if want.Index != index {
			t.Errorf("default_rules[%d].index = %d, want %d", index, want.Index, index)
		}
		if want.CategoryID != int64(index+1) {
			t.Errorf("default_rules[%d].category_id = %d, want %d", index, want.CategoryID, index+1)
		}
		if want.Category != row.Category ||
			want.Pattern != row.Pattern ||
			want.Field != row.Field.AsStr() ||
			want.Direction != row.Direction.AsStr() ||
			want.Priority != row.Priority {
			t.Errorf("default_rules[%d] = %+v, want category %q pattern %q field %q direction %q priority %d",
				index,
				want,
				row.Category,
				row.Pattern,
				row.Field,
				row.Direction,
				row.Priority,
			)
		}
	}

	bookings := parseBookings(t, readInput(t, "rules", "bookings.csv"))
	if len(golden.Bookings) != len(bookings) {
		t.Fatalf("bookings: golden has %d, parsed %d", len(golden.Bookings), len(bookings))
	}
	for index, want := range golden.Bookings {
		booking := bookings[index]
		shopMatch := ShopMatchOf(&booking.purpose, &booking.partner)
		selected := Select(specs, patterns, &booking.purpose, &booking.partner, &booking.amount, shopMatch)
		category := matcher.CategoryFor(&booking.purpose, &booking.partner, &booking.amount)

		var shop *string
		if shopMatch != nil {
			shop = &shopMatch.Shop
		}
		var selectedPattern *string
		if selected != nil {
			pattern := specs[*selected].Pattern
			selectedPattern = &pattern
		}
		var categoryID *int64
		var categoryName *string
		if category != nil {
			id := int64(*category)
			categoryID = &id
			name := names[*category]
			categoryName = &name
		}

		if want.Index != index {
			t.Errorf("bookings[%d].index = %d, want %d", index, want.Index, index)
		}
		if want.Date != booking.date {
			t.Errorf("bookings[%d].date = %q, want %q", index, want.Date, booking.date)
		}
		if want.Amount != booking.amount.String() {
			t.Errorf("bookings[%d].amount = %q, want %q", index, want.Amount, booking.amount.String())
		}
		if !equalOptionalString(want.PartnerName, &booking.partner) {
			t.Errorf("bookings[%d].partner_name = %v, want %q", index, showOptionalString(want.PartnerName), booking.partner)
		}
		if !equalOptionalString(want.Purpose, &booking.purpose) {
			t.Errorf("bookings[%d].purpose = %v, want %q", index, showOptionalString(want.Purpose), booking.purpose)
		}
		if !equalOptionalString(want.Shop, shop) {
			t.Errorf("bookings[%d].shop = %v, want %v", index, showOptionalString(want.Shop), showOptionalString(shop))
		}
		if !equalOptionalInt(want.SelectedIndex, selected) {
			t.Errorf(
				"bookings[%d].selected_index = %v, want %v",
				index,
				showOptionalInt(want.SelectedIndex),
				showOptionalInt(selected),
			)
		}
		if !equalOptionalString(want.SelectedPattern, selectedPattern) {
			t.Errorf(
				"bookings[%d].selected_pattern = %v, want %v",
				index,
				showOptionalString(want.SelectedPattern),
				showOptionalString(selectedPattern),
			)
		}
		if !equalOptionalInt64(want.CategoryID, categoryID) {
			t.Errorf(
				"bookings[%d].category_id = %v, want %v",
				index,
				showOptionalInt64(want.CategoryID),
				showOptionalInt64(categoryID),
			)
		}
		if !equalOptionalString(want.Category, categoryName) {
			t.Errorf(
				"bookings[%d].category = %v, want %v",
				index,
				showOptionalString(want.Category),
				showOptionalString(categoryName),
			)
		}
	}

	amount := decimal.New(-1000, 2)
	for index, want := range golden.ShopCases {
		caseSpecs := make([]RuleSpec, 0, len(want.Rules))
		tests := make([]*Pattern, 0, len(want.Rules))
		for _, rule := range want.Rules {
			field, ok := model.RuleFieldFromStored(rule.Field)
			if !ok {
				t.Fatalf("shop_cases[%d]: unknown field %q", index, rule.Field)
			}
			direction, ok := model.RuleDirectionFromStored(rule.Direction)
			if !ok {
				t.Fatalf("shop_cases[%d]: unknown direction %q", index, rule.Direction)
			}
			caseSpecs = append(caseSpecs, RuleSpec{
				Pattern:    rule.Pattern,
				CategoryID: model.CategoryID(rule.CategoryID),
				Field:      field,
				Direction:  direction,
			})
			tests = append(tests, Compile(rule.Pattern))
		}

		shopMatch := ShopMatchOf(&want.Purpose, want.Partner)
		selected := Select(caseSpecs, tests, &want.Purpose, want.Partner, &amount, shopMatch)
		var selectedCategory *int64
		if selected != nil {
			id := int64(caseSpecs[*selected].CategoryID)
			selectedCategory = &id
		}

		if !equalOptionalInt(want.SelectedIndex, selected) {
			t.Errorf(
				"shop_cases[%d] %q: selected_index = %v, want %v",
				index,
				want.Name,
				showOptionalInt(want.SelectedIndex),
				showOptionalInt(selected),
			)
		}
		if !equalOptionalInt64(want.SelectedCategoryID, selectedCategory) {
			t.Errorf(
				"shop_cases[%d] %q: selected_category_id = %v, want %v",
				index,
				want.Name,
				showOptionalInt64(want.SelectedCategoryID),
				showOptionalInt64(selectedCategory),
			)
		}
	}
}

type parsedBooking struct {
	date    string
	amount  decimal.Decimal
	partner string
	purpose string
}

// parseBookings reads the golden CSV the way the Rust import produced it for
// the four fields the matcher golden records.
func parseBookings(t *testing.T, data []byte) []parsedBooking {
	t.Helper()

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse bookings.csv: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("bookings.csv has no header")
	}

	bookings := make([]parsedBooking, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != 4 {
			t.Fatalf("bookings.csv row has %d columns, want 4", len(record))
		}
		parts := strings.Split(record[0], ".")
		if len(parts) != 3 {
			t.Fatalf("bookings.csv date %q is not dd.mm.yyyy", record[0])
		}
		day, err := strconv.Atoi(parts[0])
		if err != nil {
			t.Fatalf("bookings.csv day %q: %v", parts[0], err)
		}
		month, err := strconv.Atoi(parts[1])
		if err != nil {
			t.Fatalf("bookings.csv month %q: %v", parts[1], err)
		}
		year, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("bookings.csv year %q: %v", parts[2], err)
		}
		amount, err := decimal.Parse(strings.ReplaceAll(record[1], ",", "."))
		if err != nil {
			t.Fatalf("bookings.csv amount %q: %v", record[1], err)
		}
		bookings = append(bookings, parsedBooking{
			date:    fmt.Sprintf("%04d-%02d-%02d", year, month, day),
			amount:  amount,
			partner: record[2],
			purpose: record[3],
		})
	}
	return bookings
}

type goldenShopMatch struct {
	Shop            string  `json:"shop"`
	ProviderPurpose *string `json:"provider_purpose"`
	ProviderPartner *string `json:"provider_partner"`
}

type goldenPaymentCase struct {
	Name      string           `json:"name"`
	Purpose   string           `json:"purpose"`
	Partner   *string          `json:"partner"`
	ShopMatch *goldenShopMatch `json:"shop_match"`
	Title     *string          `json:"title"`
	Merchant  *string          `json:"merchant"`
}

func TestPaymentProvidersGolden(t *testing.T) {
	var golden []goldenPaymentCase
	decodeGolden(t, readGolden(t, "rules", "payment_providers.json"), &golden)
	if len(golden) == 0 {
		t.Fatal("payment_providers.json is empty")
	}

	for index, want := range golden {
		matched := ShopMatchOf(&want.Purpose, want.Partner)
		if want.ShopMatch == nil {
			if matched != nil {
				t.Errorf("[%d] %q: shop_match = %+v, want nil", index, want.Name, matched)
			}
		} else if matched == nil {
			t.Errorf("[%d] %q: shop_match = nil, want %+v", index, want.Name, *want.ShopMatch)
		} else {
			if matched.Shop != want.ShopMatch.Shop {
				t.Errorf("[%d] %q: shop = %q, want %q", index, want.Name, matched.Shop, want.ShopMatch.Shop)
			}
			if !equalOptionalString(matched.ProviderPurpose, want.ShopMatch.ProviderPurpose) {
				t.Errorf(
					"[%d] %q: provider_purpose = %v, want %v",
					index,
					want.Name,
					showOptionalString(matched.ProviderPurpose),
					showOptionalString(want.ShopMatch.ProviderPurpose),
				)
			}
			if !equalOptionalString(matched.ProviderPartner, want.ShopMatch.ProviderPartner) {
				t.Errorf(
					"[%d] %q: provider_partner = %v, want %v",
					index,
					want.Name,
					showOptionalString(matched.ProviderPartner),
					showOptionalString(want.ShopMatch.ProviderPartner),
				)
			}
		}

		title := TitleOf(&want.Purpose, want.Partner)
		if !equalOptionalString(title, want.Title) {
			t.Errorf(
				"[%d] %q: title = %v, want %v",
				index,
				want.Name,
				showOptionalString(title),
				showOptionalString(want.Title),
			)
		}
		merchant := MerchantOf(&want.Purpose, want.Partner)
		if !equalOptionalString(merchant, want.Merchant) {
			t.Errorf(
				"[%d] %q: merchant = %v, want %v",
				index,
				want.Name,
				showOptionalString(merchant),
				showOptionalString(want.Merchant),
			)
		}
	}
}

type goldenRuleFileRow struct {
	Line      int    `json:"line"`
	Category  string `json:"category"`
	Pattern   string `json:"pattern"`
	Field     string `json:"field"`
	Direction string `json:"direction"`
	Priority  int64  `json:"priority"`
	AutoApply bool   `json:"auto_apply"`
}

type goldenRuleFileError struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

type goldenRewritten struct {
	Text    string `json:"text"`
	Skipped int    `json:"skipped"`
}

type goldenRuleFile struct {
	File      string                `json:"file"`
	OK        bool                  `json:"ok"`
	Rows      []goldenRuleFileRow   `json:"rows"`
	Errors    []goldenRuleFileError `json:"errors"`
	Rewritten *goldenRewritten      `json:"rewritten"`
	Error     *string               `json:"error"`
}

type goldenRuleFiles struct {
	Files []goldenRuleFile `json:"files"`
}

func TestRuleFilesGolden(t *testing.T) {
	var golden goldenRuleFiles
	decodeGolden(t, readGolden(t, "rules", "files.json"), &golden)

	directory := filepath.Join(repoRoot(t), "testdata", "inputs", "rules")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", directory, err)
	}
	var inputs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tsv") {
			inputs = append(inputs, entry.Name())
		}
	}

	if len(golden.Files) != len(inputs) {
		t.Fatalf("golden has %d files, inputs has %d", len(golden.Files), len(inputs))
	}

	for index, want := range golden.Files {
		if want.File != inputs[index] {
			t.Errorf("files[%d] = %q, want %q", index, want.File, inputs[index])
			continue
		}

		parsed, parseErr := ParseRuleFile(readInput(t, "rules", want.File), i18n.En)
		if !want.OK {
			if parseErr == nil {
				t.Errorf("%s: parse succeeded, want error %v", want.File, showOptionalString(want.Error))
				continue
			}
			if want.Error == nil || parseErr.Error() != *want.Error {
				t.Errorf("%s: error = %q, want %v", want.File, parseErr.Error(), showOptionalString(want.Error))
			}
			continue
		}
		if parseErr != nil {
			t.Errorf("%s: parse error: %v", want.File, parseErr)
			continue
		}

		if len(want.Rows) != len(parsed.Rows) {
			t.Errorf("%s: %d rows, want %d", want.File, len(parsed.Rows), len(want.Rows))
			continue
		}
		items := make([]RuleFileItem, 0, len(parsed.Rows))
		for rowIndex, row := range parsed.Rows {
			expected := want.Rows[rowIndex]
			if expected.Line != row.Line ||
				expected.Category != row.Category ||
				expected.Pattern != row.Pattern ||
				expected.Field != row.Field.AsStr() ||
				expected.Direction != row.Direction.AsStr() ||
				expected.Priority != row.Priority ||
				expected.AutoApply != row.AutoApply {
				t.Errorf("%s rows[%d] = %+v, want %+v", want.File, rowIndex, row, expected)
			}
			items = append(items, RuleFileItem{
				Pattern:      row.Pattern,
				CategoryName: row.Category,
				Priority:     row.Priority,
				AutoApply:    row.AutoApply,
				MatchField:   row.Field,
				Direction:    row.Direction,
			})
		}

		if len(want.Errors) != len(parsed.Errors) {
			t.Errorf("%s: %d errors, want %d", want.File, len(parsed.Errors), len(want.Errors))
		} else {
			for errorIndex, failure := range parsed.Errors {
				expected := want.Errors[errorIndex]
				if expected.Line != failure.Line || expected.Reason != failure.Reason {
					t.Errorf("%s errors[%d] = %+v, want %+v", want.File, errorIndex, failure, expected)
				}
			}
		}

		text, skipped := WriteRuleFile(items)
		if want.Rewritten == nil {
			t.Errorf("%s: golden has no rewritten text", want.File)
			continue
		}
		if text != want.Rewritten.Text {
			t.Errorf("%s: rewritten text = %q, want %q", want.File, text, want.Rewritten.Text)
		}
		if skipped != want.Rewritten.Skipped {
			t.Errorf("%s: skipped = %d, want %d", want.File, skipped, want.Rewritten.Skipped)
		}
	}
}
