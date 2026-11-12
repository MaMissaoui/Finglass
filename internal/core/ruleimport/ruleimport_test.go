package ruleimport

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	rulefiles "github.com/MaMissaoui/Finglass/internal/core/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

const header = "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply"

var nextHash atomic.Int64

func database(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	opened, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func date(t *testing.T, year int, month time.Month, day int) civil.Date {
	t.Helper()

	value, ok := civil.New(year, month, day)
	if !ok {
		t.Fatalf("civil.New(%d, %d, %d) failed", year, month, day)
	}
	return value
}

func parse(t *testing.T, lines ...string) *rulefiles.ParsedRuleFile {
	t.Helper()

	joined := ""
	for index, line := range lines {
		if index > 0 {
			joined += "\n"
		}
		joined += line
	}
	parsed, err := rulefiles.ParseRuleFile([]byte(joined), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}
	return parsed
}

func row(category, pattern, searchIn, appliesTo, priority, autoApply string) string {
	return category + "\t" + pattern + "\t" + searchIn + "\t" + appliesTo + "\t" + priority + "\t" + autoApply
}

func category(t *testing.T, d *db.Database, name string) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:          name,
		Color:         "#3BC7B0",
		IsTaxRelevant: false,
		IsTransfer:    false,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func rule(
	t *testing.T,
	d *db.Database,
	pattern string,
	categoryID model.CategoryID,
	priority int64,
	field model.RuleField,
	direction model.RuleDirection,
) {
	t.Helper()
	ruleWith(t, d, pattern, categoryID, priority, true, field, direction)
}

func ruleWith(
	t *testing.T,
	d *db.Database,
	pattern string,
	categoryID model.CategoryID,
	priority int64,
	autoApply bool,
	field model.RuleField,
	direction model.RuleDirection,
) {
	t.Helper()

	if _, err := rulerepo.Create(context.Background(), d, model.NewRule{
		Pattern:    pattern,
		CategoryID: categoryID,
		Priority:   priority,
		AutoApply:  autoApply,
		MatchField: field,
		Direction:  direction,
	}); err != nil {
		t.Fatalf("rulerepo.Create: %v", err)
	}
}

func account(t *testing.T, d *db.Database) model.AccountID {
	t.Helper()

	id, err := accounts.Create(context.Background(), d, model.NewAccount{
		Name:        stringPointer("Account"),
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func txn(
	t *testing.T,
	d *db.Database,
	accountID model.AccountID,
	day civil.Date,
	amount string,
	partner *string,
	purpose *string,
	categoryID *model.CategoryID,
) model.TransactionID {
	t.Helper()

	parsedAmount, err := money.ParseDecimal(amount, money.EUR)
	if err != nil {
		t.Fatalf("ParseDecimal(%q): %v", amount, err)
	}
	hash := "hash-" + itoa(int(nextHash.Add(1)-1))
	if _, err := transactions.InsertImported(context.Background(), d, accountID, []model.NewTransaction{{
		BookingDate: day,
		Amount:      parsedAmount,
		PartnerName: partner,
		Purpose:     purpose,
		CategoryID:  categoryID,
		ImportHash:  hash,
		CreatedAt:   time.Now().UTC(),
		IsNew:       false,
	}}); err != nil {
		t.Fatalf("InsertImported: %v", err)
	}
	bookings, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	var newest model.TransactionID
	for _, booking := range bookings {
		if booking.ID > newest {
			newest = booking.ID
		}
	}
	return newest
}

func auditDetails(t *testing.T, d *db.Database, action string) []string {
	t.Helper()

	entries, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	details := make([]string, 0)
	for _, entry := range entries {
		if entry.Action == action {
			details = append(details, entry.Details)
		}
	}
	return details
}

func preview(t *testing.T, d *db.Database, parsed *rulefiles.ParsedRuleFile) *RuleImportPreview {
	t.Helper()

	result, err := Preview(context.Background(), d, parsed)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	return result
}

func apply(t *testing.T, d *db.Database, preview *RuleImportPreview, categorize bool) RuleImportResult {
	t.Helper()

	result, err := Apply(context.Background(), d, preview, categorize, i18n.En)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return result
}

func stringPointer(value string) *string {
	return &value
}

func TestExistingCategoriesAreMatchedCaseInsensitivelyAndNothingIsCreatedForThem(t *testing.T) {
	d := database(t)
	category(t, d, "groceries")

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "beispiel markt", "any", "any", "50", "true"),
		row("  Groceries  ", "musterladen", "any", "any", "50", "true"),
	))

	if len(result.CategoriesToCreate) != 0 {
		t.Errorf("categories to create = %v: Groceries exists, in another spelling", result.CategoriesToCreate)
	}
	if len(result.NewRules) != 2 {
		t.Errorf("new rules = %d", len(result.NewRules))
	}
	apply(t, d, result, false)

	listed, err := categories.List(context.Background(), d)
	if err != nil {
		t.Fatalf("categories.List: %v", err)
	}
	names := make([]string, 0, len(listed))
	for _, category := range listed {
		names = append(names, category.Name)
	}
	if !slices.Equal(names, []string{"groceries"}) {
		t.Errorf("category names = %v: the existing spelling wins", names)
	}
}

func TestADuplicateOfAnExistingRuleIsSkipped(t *testing.T) {
	d := database(t)
	groceries := category(t, d, "Groceries")
	rule(t, d, "beispiel markt", groceries, 30, model.RuleFieldPartner, model.RuleDirectionExpense)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "beispiel markt", "partner", "expense", "30", "false"),
	))
	applied := apply(t, d, result, false)

	if result.Duplicates != 1 {
		t.Errorf("duplicates = %d", result.Duplicates)
	}
	if len(result.NewRules) != 0 || len(result.Conflicts) != 0 {
		t.Errorf("new rules = %d, conflicts = %d", len(result.NewRules), len(result.Conflicts))
	}
	if applied.Rules != 0 {
		t.Errorf("created rules = %d: nothing new is created", applied.Rules)
	}
	listed, err := rulerepo.List(context.Background(), d)
	if err != nil || len(listed) != 1 {
		t.Errorf("rules = %d, %v", len(listed), err)
	}
}

func TestAConflictIsReportedAndTheExistingRuleStaysUnchanged(t *testing.T) {
	d := database(t)
	groceries := category(t, d, "Groceries")
	rule(t, d, "beispiel markt", groceries, 30, model.RuleFieldAny, model.RuleDirectionAny)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Household", "beispiel markt", "any", "any", "50", "true"),
	))
	applied := apply(t, d, result, false)

	want := RuleImportConflict{
		Line:             3,
		Pattern:          "beispiel markt",
		FileCategory:     "Household",
		ExistingCategory: "Groceries",
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0] != want {
		t.Errorf("conflicts = %+v, want %+v", result.Conflicts, want)
	}
	if len(result.NewRules) != 0 {
		t.Errorf("new rules = %d", len(result.NewRules))
	}
	if applied.Rules != 0 {
		t.Errorf("created rules = %d: existing rules are never changed", applied.Rules)
	}
	listed, err := rulerepo.List(context.Background(), d)
	if err != nil || len(listed) != 1 {
		t.Fatalf("rules = %d, %v", len(listed), err)
	}
	if listed[0].CategoryID != groceries {
		t.Errorf("existing category id = %d", listed[0].CategoryID)
	}
	if !slices.Equal(result.CategoriesToCreate, []string{"Household"}) {
		t.Errorf("categories to create = %v: the file's category is created but no rule points at it", result.CategoriesToCreate)
	}
}

func TestIdenticalRowsOfTheSameFileAreDuplicatesRowsWithAnotherCategoryConflictWithThem(t *testing.T) {
	d := database(t)
	file := parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "rewe", "any", "any", "50", "true"),
		row("Groceries", "rewe", "any", "any", "50", "true"),
		row("Cash", "rewe", "any", "any", "50", "true"),
		row("Groceries", "beispiel markt", "any", "any", "50", "true"),
	)

	result := preview(t, d, file)

	if result.Duplicates != 1 {
		t.Errorf("duplicates = %d", result.Duplicates)
	}
	want := RuleImportConflict{
		Line:             5,
		Pattern:          "rewe",
		FileCategory:     "Cash",
		ExistingCategory: "Groceries",
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0] != want {
		t.Errorf("conflicts = %+v, want %+v", result.Conflicts, want)
	}
	lines := make([]int, 0, len(result.NewRules))
	for _, row := range result.NewRules {
		lines = append(lines, row.Line)
	}
	if !slices.Equal(lines, []int{3, 6}) {
		t.Errorf("new rule lines = %v", lines)
	}
}

func TestMatchCountRespectsPriorityOrderSearchInAppliesToTheExistingRulesAndManualRules(t *testing.T) {
	d := database(t)
	salary := category(t, d, "Salary")
	rule(t, d, "beispielpay", salary, 5, model.RuleFieldPartner, model.RuleDirectionExpense)
	accountID := account(t, d)
	today := date(t, 2026, time.October, 24)

	txn(t, d, accountID, today, "-4.50", stringPointer("BeispielPay"), stringPointer("Kartenzahlung"), nil)
	txn(t, d, accountID, today, "40.00", stringPointer("Muster GmbH"), stringPointer("Rueckzahlung"), nil)
	txn(t, d, accountID, today, "-40.00", stringPointer("Muster GmbH"), nil, nil)
	txn(t, d, accountID, today, "2500.00", stringPointer("BeispielPay"), stringPointer("Gehalt Beispiel GmbH"), nil)
	txn(t, d, accountID, today, "-9.99", stringPointer("Musterladen"), nil, nil)
	txn(t, d, accountID, today, "1.23", stringPointer("BeispielPay"), nil, nil)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Salary", "muster gmbh", "partner", "income", "90", "true"),
		row("Other income", "gehalt", "purpose", "income", "40", "true"),
		row("Groceries", "musterladen", "partner", "any", "60", "false"),
	))

	if result.MatchCount != 3 {
		t.Errorf("match count = %d: the partner expense and the unmatched income are not counted", result.MatchCount)
	}
	if len(result.NewRules) != 3 {
		t.Errorf("new rules = %d: rules are previewed even when they do not participate in the match", len(result.NewRules))
	}
}

func TestApplyingCreatesTheCategoriesAndRulesWithAllTheirFieldsAndCategorizesOnlyUncategorizedBookings(t *testing.T) {
	d := database(t)
	groceries := category(t, d, "Groceries")
	stuff := category(t, d, "Stuff")
	accountID := account(t, d)
	today := date(t, 2026, time.October, 24)
	txn(t, d, accountID, today, "-42.00", stringPointer("Musterladen"), nil, nil)
	preCategorized := txn(t, d, accountID, today, "-7.77", stringPointer("Musterladen"), nil, &stuff)
	txn(t, d, accountID, today, "1500.00", stringPointer("Erika Mustermann"), nil, nil)
	txn(t, d, accountID, today, "-0.01", stringPointer("Nirwana"), nil, nil)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "musterladen", "partner", "expense", "60", "true"),
		row("Salary", "erika mustermann", "partner", "income", "70", "true"),
	))
	applied := apply(t, d, result, true)

	if applied.Categories != 1 || applied.Rules != 2 || applied.Categorized != 2 {
		t.Errorf("result = %+v", applied)
	}

	listed, err := categories.List(context.Background(), d)
	if err != nil {
		t.Fatalf("categories.List: %v", err)
	}
	var salary *model.Category
	for index := range listed {
		if listed[index].Name == "Salary" {
			salary = &listed[index]
			break
		}
	}
	if salary == nil {
		t.Fatal("Salary is missing")
	}
	wantColor := ""
	for _, entry := range categories.Entries {
		if entry.Key == i18n.DefaultCategorySalary {
			wantColor = entry.Color
			break
		}
	}
	if salary.Color != wantColor {
		t.Errorf("salary colour = %q, want %q", salary.Color, wantColor)
	}

	listedRules, err := rulerepo.List(context.Background(), d)
	if err != nil || len(listedRules) != 2 {
		t.Fatalf("rules = %d, %v", len(listedRules), err)
	}
	salaryRule := findRule(t, listedRules, "erika mustermann")
	if salaryRule.CategoryID != salary.ID || salaryRule.Priority != 70 || !salaryRule.AutoApply ||
		salaryRule.MatchField != model.RuleFieldPartner || salaryRule.Direction != model.RuleDirectionIncome {
		t.Errorf("salary rule = %+v", salaryRule)
	}
	if findRule(t, listedRules, "musterladen").Priority != 60 {
		t.Errorf("Musterladen priority = %d", findRule(t, listedRules, "musterladen").Priority)
	}

	bookings, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil || len(bookings) != 4 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}
	for _, booking := range bookings {
		switch {
		case booking.ID != preCategorized && booking.PartnerName != nil && *booking.PartnerName == "Musterladen":
			if booking.CategoryID == nil || *booking.CategoryID != groceries {
				t.Errorf("uncategorized expense category = %v: gets the rule's category", booking.CategoryID)
			}
		case booking.ID == preCategorized:
			if booking.CategoryID == nil || *booking.CategoryID != stuff {
				t.Errorf("pre-categorized category = %v: keeps it", booking.CategoryID)
			}
		case booking.PartnerName != nil && *booking.PartnerName == "Erika Mustermann":
			if booking.CategoryID == nil || *booking.CategoryID != salary.ID {
				t.Errorf("Salary category = %v", booking.CategoryID)
			}
		case booking.PartnerName != nil && *booking.PartnerName == "Nirwana":
			if booking.CategoryID != nil {
				t.Errorf("Nirwana category = %v: an unmatched booking stays uncategorized", booking.CategoryID)
			}
		}
	}
}

func TestCategorizeFalseCreatesCategoriesAndRulesButTouchesNoBooking(t *testing.T) {
	d := database(t)
	category(t, d, "Groceries")
	accountID := account(t, d)
	txn(t, d, accountID, date(t, 2026, time.October, 24), "-42.00",
		stringPointer("Musterladen"), nil, nil)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "musterladen", "partner", "expense", "60", "true"),
	))
	applied := apply(t, d, result, false)

	if applied.Categorized != 0 {
		t.Errorf("categorized = %d", applied.Categorized)
	}
	bookings, err := transactions.ListForAccount(context.Background(), d, accountID, nil, nil)
	if err != nil || len(bookings) != 1 {
		t.Fatalf("bookings = %d, %v", len(bookings), err)
	}
	if bookings[0].CategoryID != nil {
		t.Errorf("category = %v", bookings[0].CategoryID)
	}
	listed, err := rulerepo.List(context.Background(), d)
	if err != nil || len(listed) != 1 {
		t.Errorf("rules = %d, %v", len(listed), err)
	}
}

func TestImportingTheSameFileTwiceAddsNothingTheSecondTime(t *testing.T) {
	d := database(t)
	category(t, d, "Groceries")
	file := "# FinDesk rules v1\n" + header + "\n" +
		row("Groceries", "musterladen", "partner", "expense", "60", "true") + "\n"
	parsed, err := rulefiles.ParseRuleFile([]byte(file), i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}

	first := apply(t, d, preview(t, d, parsed), true)
	if first.Rules != 1 {
		t.Errorf("first rules = %d", first.Rules)
	}

	secondPreview := preview(t, d, parsed)
	if len(secondPreview.NewRules) != 0 || len(secondPreview.CategoriesToCreate) != 0 {
		t.Errorf("second preview = %+v", secondPreview)
	}
	if secondPreview.Duplicates != 1 {
		t.Errorf("duplicates = %d", secondPreview.Duplicates)
	}
	second := apply(t, d, secondPreview, true)
	if second.Categories != 0 || second.Rules != 0 || second.Categorized != 0 {
		t.Errorf("second result = %+v", second)
	}
	listed, err := rulerepo.List(context.Background(), d)
	if err != nil || len(listed) != 1 {
		t.Errorf("rules = %d, %v", len(listed), err)
	}
	listedCategories, err := categories.List(context.Background(), d)
	if err != nil || len(listedCategories) != 1 {
		t.Errorf("categories = %d, %v", len(listedCategories), err)
	}
}

func TestImportAndExportLeaveAuditEntriesWithCountsOnly(t *testing.T) {
	d := database(t)
	groceries := category(t, d, "Groceries")
	txn(t, d, account(t, d), date(t, 2026, time.October, 24), "-4.50",
		stringPointer("Musterladen"), nil, nil)

	result := preview(t, d, parse(t,
		"# FinDesk rules v1",
		header,
		row("Groceries", "musterladen", "partner", "expense", "60", "true"),
	))
	apply(t, d, result, true)

	if got := auditDetails(t, d, "IMPORT_RULES"); !slices.Equal(got, []string{
		"Imported 1 rules, created 0 categories, categorized 1 transactions",
	}) {
		t.Errorf("import audit = %v", got)
	}

	rule(t, d, "bad\tstock", groceries, 0, model.RuleFieldAny, model.RuleDirectionAny)
	written, err := Export(context.Background(), d, filepath.Join(t.TempDir(), "rules.tsv"))
	if err != nil || written != 1 {
		t.Fatalf("Export = %d, %v", written, err)
	}
	if got := auditDetails(t, d, "EXPORT_RULES"); !slices.Equal(got, []string{"Exported 1 rules, skipped 1"}) {
		t.Errorf("export audit = %v", got)
	}
}

func TestExportIntoAFreshDatabaseReproducesTheSameRules(t *testing.T) {
	d := database(t)
	groceries := category(t, d, "Groceries")
	salary := category(t, d, "Salary")
	cash := category(t, d, "Cash")
	rule(t, d, "beispiel.?markt|musterladen", groceries, 60, model.RuleFieldAny, model.RuleDirectionAny)
	rule(t, d, "beispiel gmbh", cash, 20, model.RuleFieldPartner, model.RuleDirectionAny)
	rule(t, d, "beispiel gmbh", salary, 70, model.RuleFieldPartner, model.RuleDirectionIncome)
	ruleWith(t, d, "raw cash", cash, 0, false, model.RuleFieldPurpose, model.RuleDirectionAny)
	rule(t, d, "bad\tstock", cash, 100, model.RuleFieldAny, model.RuleDirectionAny)

	original, err := rulerepo.List(context.Background(), d)
	if err != nil {
		t.Fatalf("rulerepo.List: %v", err)
	}
	filtered := make([]model.Rule, 0, len(original))
	for _, rule := range original {
		if rule.Pattern != "bad\tstock" {
			filtered = append(filtered, rule)
		}
	}

	file := filepath.Join(t.TempDir(), "findesk-rules-2026-10-24.tsv")
	written, err := Export(context.Background(), d, file)
	if err != nil || written != 4 {
		t.Fatalf("Export = %d, %v", written, err)
	}

	fresh := database(t)
	data, err := os.ReadFile(file) // #nosec G304 -- file is a fixed test path under t.TempDir()
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	parsed, err := rulefiles.ParseRuleFile(data, i18n.En)
	if err != nil {
		t.Fatalf("ParseRuleFile: %v", err)
	}
	result := preview(t, fresh, parsed)
	if len(result.NewRules) != 4 {
		t.Errorf("new rules = %d", len(result.NewRules))
	}
	apply(t, fresh, result, false)

	restoredRules, err := rulerepo.List(context.Background(), fresh)
	if err != nil {
		t.Fatalf("rulerepo.List fresh: %v", err)
	}
	restored := make(map[ruleRestoreKey]model.Rule)
	for _, rule := range restoredRules {
		restored[ruleRestoreKey{pattern: rule.Pattern, priority: rule.Priority}] = rule
	}
	for _, rule := range filtered {
		copy, present := restored[ruleRestoreKey{pattern: rule.Pattern, priority: rule.Priority}]
		if !present {
			t.Errorf("rule %q at %d is missing", rule.Pattern, rule.Priority)
			continue
		}
		originalCategory, ok, err := categories.Get(context.Background(), d, rule.CategoryID)
		if err != nil || !ok {
			t.Fatalf("categories.Get original: %v, %v", ok, err)
		}
		copyCategory, ok, err := categories.Get(context.Background(), fresh, copy.CategoryID)
		if err != nil || !ok {
			t.Fatalf("categories.Get copy: %v, %v", ok, err)
		}
		if originalCategory.Name != copyCategory.Name {
			t.Errorf("category names = %q, %q", originalCategory.Name, copyCategory.Name)
		}
		if rule.Priority != copy.Priority || rule.AutoApply != copy.AutoApply ||
			rule.MatchField != copy.MatchField || rule.Direction != copy.Direction {
			t.Errorf("rule %q differs: %+v vs %+v", rule.Pattern, rule, copy)
		}
	}
}

type ruleRestoreKey struct {
	pattern  string
	priority int64
}

func findRule(t *testing.T, listed []model.Rule, pattern string) model.Rule {
	t.Helper()

	for _, rule := range listed {
		if rule.Pattern == pattern {
			return rule
		}
	}
	t.Fatalf("rule %q is missing", pattern)
	return model.Rule{}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	if negative {
		digits = append(digits, '-')
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
