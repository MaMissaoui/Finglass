package settingsview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/backup"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/repo/accounts"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/repo/transactions"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
	"github.com/MaMissaoui/Finglass/internal/core/settings"
)

const testPassphrase = "correct horse battery staple"

func testContext() context.Context {
	return context.Background()
}

func mustPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()
	passphrase, err := secret.NewPassphrase([]byte(value), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}
	return passphrase
}

func fixture(t *testing.T) (paths.AppPaths, *db.Database) {
	t.Helper()

	resolved, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	databaseFile, err := resolved.DatabaseFile("default")
	if err != nil {
		t.Fatalf("DatabaseFile: %v", err)
	}
	passphrase := mustPassphrase(t, testPassphrase)
	defer passphrase.Wipe()
	database, err := db.Create(databaseFile, passphrase)
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return resolved, database
}

func loaded(t *testing.T, appPaths paths.AppPaths, database *db.Database) *SettingsState {
	t.Helper()
	state := New(i18n.En)
	if err := state.Load(testContext(), database, appPaths, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return state
}

func category(t *testing.T, database *db.Database, name, color string) model.CategoryID {
	t.Helper()
	id, err := categories.Create(testContext(), database, model.NewCategory{
		Name:  name,
		Color: color,
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func account(t *testing.T, database *db.Database) model.AccountID {
	t.Helper()
	name := "Account"
	id, err := accounts.Create(testContext(), database, model.NewAccount{
		Name:        &name,
		Currency:    money.EUR,
		AccountType: model.AccountTypeChecking,
		Balance:     money.Money{Minor: 0, Currency: money.EUR},
	})
	if err != nil {
		t.Fatalf("accounts.Create: %v", err)
	}
	return id
}

func transaction(
	t *testing.T,
	database *db.Database,
	accountID model.AccountID,
	categoryID *model.CategoryID,
) {
	t.Helper()
	bookingDate, ok := civil.New(2026, time.March, 2)
	if !ok {
		t.Fatal("civil.New")
	}
	partner := "REWE"
	if _, err := transactions.InsertImported(testContext(), database, accountID, []model.NewTransaction{
		{
			BookingDate: bookingDate,
			Amount:      money.Money{Minor: -3_000, Currency: money.EUR},
			PartnerName: &partner,
			CategoryID:  categoryID,
			ImportHash:  "rewe",
			CreatedAt:   time.Now().UTC(),
			IsNew:       false,
		},
	}); err != nil {
		t.Fatalf("transactions.InsertImported: %v", err)
	}
}

func auditDetails(t *testing.T, database *db.Database, action string) []string {
	t.Helper()
	entries, err := audit.Recent(testContext(), database, 1000)
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

func ruleFile(t *testing.T, dir, name string, rows []string) string {
	t.Helper()
	var text strings.Builder
	text.WriteString("# FinDesk rules v1\ncategory\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\n")
	for _, row := range rows {
		text.WriteString(row)
		text.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text.String()), 0o600); err != nil {
		t.Fatalf("write rule file: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- test path built from t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(data)
}

func pointer[T any](value T) *T {
	return &value
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mustInclude(t *testing.T, items []string, wanted string) {
	t.Helper()
	for _, item := range items {
		if item == wanted {
			return
		}
	}
	t.Fatalf("%v does not contain %q", items, wanted)
}

// AddDefaultCategoriesTest.kt

func TestOnAnEmptyDatabaseAll24DefaultsAreCreated(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("AddDefaultCategories: %v", err)
	}

	names := make([]string, 0, len(state.Categories))
	for _, entry := range state.Categories {
		names = append(names, entry.Name)
	}
	if len(state.Categories) != 24 {
		t.Fatalf("len(categories) = %d, want 24: %v", len(state.Categories), names)
	}
	mustInclude(t, names, "Groceries")
	mustInclude(t, names, "Drugstore & household")
	mustInclude(t, names, "Other income")
	for _, entry := range state.Categories {
		if !isHexColor(entry.Color) {
			t.Errorf("category %q has no palette colour: %q", entry.Name, entry.Color)
		}
	}
	if state.Message == nil || *state.Message != "24 default categories added" {
		t.Fatalf("message = %v, want %q", state.Message, "24 default categories added")
	}
}

func TestExistingCategoriesAreKeptAndMatchedCaseInsensitively(t *testing.T) {
	appPaths, database := fixture(t)
	existing := category(t, database, "groceries", "#123456")
	state := loaded(t, appPaths, database)

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("AddDefaultCategories: %v", err)
	}

	if len(state.Categories) != 24 {
		t.Fatalf("len(categories) = %d, want 24", len(state.Categories))
	}
	var groceries *model.Category
	for index := range state.Categories {
		if state.Categories[index].ID == existing {
			groceries = &state.Categories[index]
			break
		}
	}
	if groceries == nil {
		t.Fatalf("existing category %d is missing", existing)
	}
	if groceries.Name != "groceries" {
		t.Errorf("existing categories are never changed: name = %q", groceries.Name)
	}
	if state.Message == nil || *state.Message != "23 default categories added" {
		t.Fatalf("message = %v, want %q", state.Message, "23 default categories added")
	}

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("second AddDefaultCategories: %v", err)
	}
	if len(state.Categories) != 24 {
		t.Errorf("len(categories) = %d, want 24", len(state.Categories))
	}
	if state.Message == nil || *state.Message != "All default categories already exist" {
		t.Errorf(
			"message = %v, want %q",
			state.Message,
			i18n.Text(i18n.En, i18n.MessageDefaultCategoriesNone),
		)
	}
}

func TestAddingDefaultCategoriesIsAudited(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("AddDefaultCategories: %v", err)
	}

	got := auditDetails(t, database, "ADD_DEFAULT_CATEGORIES")
	want := []string{"Added 24 default categories"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audit details = %v, want %v", got, want)
	}
}

func TestTwoAddDefaultCallsCreateEveryCategoryOnce(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("first AddDefaultCategories: %v", err)
	}
	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("second AddDefaultCategories: %v", err)
	}

	names := make([]string, 0, len(state.Categories))
	for _, entry := range state.Categories {
		names = append(names, entry.Name)
	}
	if len(names) != 24 {
		t.Fatalf("len(names) = %d, want 24: %v", len(names), names)
	}
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		unique[name] = struct{}{}
	}
	if len(unique) != len(names) {
		t.Fatalf("a category is created twice: %v", names)
	}
}

func TestTheDefaultCategoryListHolds24EntriesInPaletteOrder(t *testing.T) {
	if len(categories.Entries) != 24 {
		t.Fatalf("len(Entries) = %d, want 24", len(categories.Entries))
	}
	if categories.Entries[0].Color != "#E76530" {
		t.Errorf("first colour = %q, want %q", categories.Entries[0].Color, "#E76530")
	}
	if len(categories.Colors) != 24 {
		t.Errorf("len(Colors) = %d, want 24: 24 categories, 24 distinct colours, no cycling", len(categories.Colors))
	}
}

// TestSettingKeysAndStoredTextMatchRust pins the two settings keys this view
// writes and the exact text each one stores.
func TestSettingKeysAndStoredTextMatchRust(t *testing.T) {
	if money.PrimaryCurrencyKey != "money.primary_currency" {
		t.Errorf("primary currency key = %q, want %q", money.PrimaryCurrencyKey, "money.primary_currency")
	}
	if TaxKeywordsKey != "tax.suggestionKeywords" {
		t.Errorf("tax keywords key = %q, want %q", TaxKeywordsKey, "tax.suggestionKeywords")
	}

	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)
	if err := state.SetPrimaryCurrency(testContext(), database, "usd"); err != nil {
		t.Fatalf("SetPrimaryCurrency: %v", err)
	}
	stored, ok, err := settings.Get(testContext(), database, money.PrimaryCurrencyKey)
	if err != nil || !ok {
		t.Fatalf("read primary currency: ok = %v, err = %v", ok, err)
	}
	if stored != "USD" {
		t.Errorf("stored primary currency = %q, want %q", stored, "USD")
	}
	if state.PrimaryCurrency.String() != "USD" {
		t.Errorf("state primary currency = %q, want %q", state.PrimaryCurrency.String(), "USD")
	}
	savedAudit := auditDetails(t, database, "SET_PRIMARY_CURRENCY")
	if !reflect.DeepEqual(savedAudit, []string{"USD"}) {
		t.Errorf("SET_PRIMARY_CURRENCY details = %v, want [USD]", savedAudit)
	}

	if err := state.SaveTaxKeywords(testContext(), database, "Alpha\n beta \nALPHA"); err != nil {
		t.Fatalf("SaveTaxKeywords: %v", err)
	}
	stored, ok, err = settings.Get(testContext(), database, TaxKeywordsKey)
	if err != nil || !ok {
		t.Fatalf("read tax keywords: ok = %v, err = %v", ok, err)
	}
	if stored != "Alpha\nbeta" {
		t.Errorf("stored tax keywords = %q, want %q", stored, "Alpha\nbeta")
	}
}

// DefaultCategoriesTransferFlagTest.kt

func TestOnlyOwnTransfersIsATransfer(t *testing.T) {
	transfers := make([]categories.DefaultCategory, 0)
	for _, entry := range categories.Entries {
		if entry.IsTransfer {
			transfers = append(transfers, entry)
		}
	}
	if len(transfers) != 1 {
		t.Fatalf("transfers = %d, want 1", len(transfers))
	}
	if transfers[0].Key != i18n.DefaultCategoryOwnTransfers {
		t.Errorf("transfer key = %v, want OwnTransfers", transfers[0].Key)
	}
	for _, entry := range categories.Entries {
		if entry.Key == i18n.DefaultCategoryCreditCard && entry.IsTransfer {
			t.Error("Credit card must not be a transfer")
		}
	}
}

func TestAddDefaultCategoriesStoresTheTransferFlag(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)

	if err := state.AddDefaultCategories(testContext(), database); err != nil {
		t.Fatalf("AddDefaultCategories: %v", err)
	}

	saved := make(map[string]bool, len(state.Categories))
	for _, entry := range state.Categories {
		saved[entry.Name] = entry.IsTransfer
	}
	if saved["Groceries"] {
		t.Error("Groceries is a transfer")
	}
	if saved["Credit card"] {
		t.Error("Credit card is a transfer")
	}
	if !saved["Own transfers"] {
		t.Error("Own transfers is not a transfer")
	}
}

// FixDuplicateColoursTest.kt

func TestTheLowestIDKeepsItsColourAndTheOthersGetUnusedPaletteColours(t *testing.T) {
	appPaths, database := fixture(t)
	first := category(t, database, "Groceries", "#FF5722")
	second := category(t, database, "Travel", "#FF5722")
	third := category(t, database, "Cash", "#FF5722")
	untouched := category(t, database, "Health", "#123ABC")
	state := loaded(t, appPaths, database)

	if err := state.FixDuplicateColours(testContext(), database); err != nil {
		t.Fatalf("FixDuplicateColours: %v", err)
	}

	colors := make(map[model.CategoryID]string, len(state.Categories))
	for _, entry := range state.Categories {
		colors[entry.ID] = entry.Color
	}
	if colors[first] != "#FF5722" {
		t.Errorf("first colour = %q, want %q (the lowest id keeps the shared colour)", colors[first], "#FF5722")
	}
	if colors[second] != "#E76530" {
		t.Errorf("second colour = %q, want %q (the first unused palette colour)", colors[second], "#E76530")
	}
	if colors[third] != "#4693F3" {
		t.Errorf("third colour = %q, want %q (the next unused palette colour)", colors[third], "#4693F3")
	}
	if colors[untouched] != "#123ABC" {
		t.Errorf("untouched colour = %q, want %q", colors[untouched], "#123ABC")
	}
	distinct := make(map[string]struct{}, len(state.Categories))
	for _, entry := range state.Categories {
		distinct[strings.ToUpper(entry.Color)] = struct{}{}
	}
	if len(distinct) != len(state.Categories) {
		t.Errorf("after the fix something shares a colour: %v", colors)
	}
	wantMessage := i18n.Format(i18n.En, i18n.MessageColoursFixed, i18n.Int(2))
	if state.Message == nil || *state.Message != wantMessage {
		t.Errorf("message = %v, want %q", state.Message, wantMessage)
	}
	gotAudit := auditDetails(t, database, "FIX_CATEGORY_COLOURS")
	wantAudit := []string{"Recoloured 2 categories"}
	if !reflect.DeepEqual(gotAudit, wantAudit) {
		t.Errorf("audit details = %v, want %v", gotAudit, wantAudit)
	}
}

func TestFixingDuplicateColoursNeverTouchesNamesOrOtherFields(t *testing.T) {
	appPaths, database := fixture(t)
	groceries := category(t, database, "Groceries", "#FF5722")
	card := category(t, database, "Card", "#FF5722")
	if err := categories.Update(testContext(), database, card, model.NewCategory{
		Name:       "Card",
		Color:      "#FF5722",
		IsTransfer: true,
	}); err != nil {
		t.Fatalf("categories.Update card: %v", err)
	}
	if err := categories.Update(testContext(), database, groceries, model.NewCategory{
		Name:        "Groceries",
		Color:       "#FF5722",
		BudgetLimit: &money.Money{Minor: 12_000, Currency: money.EUR},
	}); err != nil {
		t.Fatalf("categories.Update groceries: %v", err)
	}
	_ = category(t, database, "Travel", "#FF5722")
	state := loaded(t, appPaths, database)

	if err := state.FixDuplicateColours(testContext(), database); err != nil {
		t.Fatalf("FixDuplicateColours: %v", err)
	}

	items := make(map[model.CategoryID]model.Category, len(state.Categories))
	for _, entry := range state.Categories {
		items[entry.ID] = entry
	}
	if items[groceries].Name != "Groceries" {
		t.Errorf("groceries name = %q", items[groceries].Name)
	}
	if !items[card].IsTransfer {
		t.Error("card lost its transfer flag")
	}
	wantBudget := &money.Money{Minor: 12_000, Currency: money.EUR}
	if items[groceries].BudgetLimit == nil || *items[groceries].BudgetLimit != *wantBudget {
		t.Errorf("groceries budget = %v, want %v", items[groceries].BudgetLimit, wantBudget)
	}
	if items[groceries].IsTaxRelevant {
		t.Error("groceries became tax relevant")
	}
}

func TestASecondFixChangesNothingAndWritesNoAuditEntry(t *testing.T) {
	appPaths, database := fixture(t)
	_ = category(t, database, "Groceries", "#FF5722")
	_ = category(t, database, "Travel", "#FF5722")
	state := loaded(t, appPaths, database)
	if err := state.FixDuplicateColours(testContext(), database); err != nil {
		t.Fatalf("first FixDuplicateColours: %v", err)
	}
	state.ClearMessages()
	colorsAfterFirst := make(map[model.CategoryID]string, len(state.Categories))
	for _, entry := range state.Categories {
		colorsAfterFirst[entry.ID] = entry.Color
	}
	fixesAfterFirst := auditDetails(t, database, "FIX_CATEGORY_COLOURS")

	if err := state.FixDuplicateColours(testContext(), database); err != nil {
		t.Fatalf("second FixDuplicateColours: %v", err)
	}

	colorsAfterSecond := make(map[model.CategoryID]string, len(state.Categories))
	for _, entry := range state.Categories {
		colorsAfterSecond[entry.ID] = entry.Color
	}
	if !reflect.DeepEqual(colorsAfterFirst, colorsAfterSecond) {
		t.Error("nothing is recoloured a second time")
	}
	if !reflect.DeepEqual(fixesAfterFirst, auditDetails(t, database, "FIX_CATEGORY_COLOURS")) {
		t.Error("no further audit entry")
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessageColoursAlreadyDistinct) {
		t.Errorf("message = %v", state.Message)
	}
}

func TestLowerAndUpperCaseHexCountAsTheSameColour(t *testing.T) {
	appPaths, database := fixture(t)
	first := category(t, database, "Groceries", "#ff5722")
	second := category(t, database, "Travel", "#FF5722")
	state := loaded(t, appPaths, database)

	if err := state.FixDuplicateColours(testContext(), database); err != nil {
		t.Fatalf("FixDuplicateColours: %v", err)
	}

	colors := make(map[model.CategoryID]string, len(state.Categories))
	for _, entry := range state.Categories {
		colors[entry.ID] = entry.Color
	}
	if colors[first] != "#ff5722" {
		t.Errorf("first colour = %q, want %q (untouched)", colors[first], "#ff5722")
	}
	if colors[second] != "#E76530" {
		t.Errorf("second colour = %q, want %q", colors[second], "#E76530")
	}
	wantMessage := i18n.Format(i18n.En, i18n.MessageColoursFixed, i18n.Int(1))
	if state.Message == nil || *state.Message != wantMessage {
		t.Errorf("message = %v, want %q", state.Message, wantMessage)
	}
	gotAudit := auditDetails(t, database, "FIX_CATEGORY_COLOURS")
	wantAudit := []string{"Recoloured 1 categories"}
	if !reflect.DeepEqual(gotAudit, wantAudit) {
		t.Errorf("audit details = %v, want %v", gotAudit, wantAudit)
	}
}

// RuleImportViewModelTest.kt

func TestARejectedRuleFileSetsAnErrorAndNoPreview(t *testing.T) {
	appPaths, database := fixture(t)
	_ = category(t, database, "Groceries", "#FF5722")
	bad := filepath.Join(t.TempDir(), "rules.tsv")
	if err := os.WriteFile(bad, []byte("Definitely not a rule file\njust some text\n"), 0o600); err != nil {
		t.Fatalf("write bad rule file: %v", err)
	}
	state := loaded(t, appPaths, database)

	if err := state.ChooseRuleImport(testContext(), database, bad); err != nil {
		t.Fatalf("ChooseRuleImport: %v", err)
	}

	if state.Error == nil || *state.Error != i18n.Text(i18n.En, i18n.ErrorRuleFileHeader) {
		t.Errorf("error = %v", state.Error)
	}
	if state.RuleImport != nil {
		t.Error("a rejected file leaves no preview")
	}
	if len(state.Rules) != 0 {
		t.Error("a rejected file changes no rules")
	}
}

func TestConfirmAppliesThePreviewAndReportsTheCounts(t *testing.T) {
	appPaths, database := fixture(t)
	_ = category(t, database, "Groceries", "#FF5722")
	file := ruleFile(t, t.TempDir(), "rules.tsv", []string{
		"Groceries\tmusterladen\tpartner\texpense\t60\ttrue",
	})
	state := loaded(t, appPaths, database)

	if err := state.ChooseRuleImport(testContext(), database, file); err != nil {
		t.Fatalf("ChooseRuleImport: %v", err)
	}
	if state.RuleImport == nil {
		t.Fatal("the preview is missing")
	}
	if state.RuleImport.MatchCount != 0 {
		t.Errorf("match count = %d, want 0: there are no bookings", state.RuleImport.MatchCount)
	}
	if len(state.RuleImport.CategoriesToCreate) != 0 {
		t.Errorf("categories to create = %v", state.RuleImport.CategoriesToCreate)
	}

	if err := state.ConfirmRuleImport(testContext(), database, true); err != nil {
		t.Fatalf("ConfirmRuleImport: %v", err)
	}

	if state.RuleImport != nil {
		t.Error("the preview is consumed")
	}
	if state.Error != nil {
		t.Errorf("error = %v", state.Error)
	}
	wantMessage := i18n.Format(
		i18n.En,
		i18n.MessageRulesImported,
		i18n.Int(1),
		i18n.Int(0),
		i18n.Int(0),
	)
	if state.Message == nil || *state.Message != wantMessage {
		t.Errorf("message = %v, want %q", state.Message, wantMessage)
	}
	if len(state.Rules) != 1 {
		t.Fatalf("len(rules) = %d, want 1", len(state.Rules))
	}
	if state.Rules[0].Priority != 60 {
		t.Errorf("priority = %d, want 60", state.Rules[0].Priority)
	}
	if state.Rules[0].Pattern != "musterladen" {
		t.Errorf("pattern = %q", state.Rules[0].Pattern)
	}
	if len(state.Categories) != 1 {
		t.Errorf("len(categories) = %d, want 1: Groceries existed before", len(state.Categories))
	}
}

func TestExportWritesTheRuleFileAndReportsTheCounts(t *testing.T) {
	appPaths, database := fixture(t)
	groceries := category(t, database, "Groceries", "#FF5722")
	if _, err := rulerepo.Create(testContext(), database, model.NewRule{
		Pattern:    "musterladen",
		CategoryID: groceries,
		Priority:   60,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
	output := filepath.Join(t.TempDir(), "out.tsv")
	state := loaded(t, appPaths, database)

	if err := state.ExportRules(testContext(), database, output); err != nil {
		t.Fatalf("ExportRules: %v", err)
	}

	if state.Error != nil {
		t.Errorf("error = %v", state.Error)
	}
	wantMessage := i18n.Format(i18n.En, i18n.MessageRulesExported, i18n.Int(1))
	if state.Message == nil || *state.Message != wantMessage {
		t.Errorf("message = %v, want %q", state.Message, wantMessage)
	}
	got := readFile(t, output)
	wantPrefix := "# FinDesk rules v1\ncategory\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\n" +
		"Groceries\tmusterladen\tany\tany\t60\ttrue\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("file = %q, want prefix %q", got, wantPrefix)
	}
}

func TestCancellingDropsThePreviewAndTheConfirmationAppliesNothing(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)
	file := ruleFile(t, t.TempDir(), "rules.tsv", []string{
		"Cash\traw cash\tany\tany\t1\ttrue",
	})

	if err := state.ChooseRuleImport(testContext(), database, file); err != nil {
		t.Fatalf("ChooseRuleImport: %v", err)
	}
	if state.RuleImport == nil || len(state.RuleImport.NewRules) != 1 {
		t.Fatalf("the preview is staged, the database untouched: %v", state.RuleImport)
	}
	if listed, err := rulerepo.List(testContext(), database); err != nil || len(listed) != 0 {
		t.Fatalf("choosing alone creates nothing: %v, %v", listed, err)
	}

	state.CancelRuleImport()
	if err := state.ConfirmRuleImport(testContext(), database, false); err != nil {
		t.Fatalf("ConfirmRuleImport: %v", err)
	}
	if len(state.Rules) != 0 {
		t.Error("a cancelled import applies nothing")
	}
}

func TestTwoConfirmationsCreateEachRuleAndOneAuditEntry(t *testing.T) {
	appPaths, database := fixture(t)
	_ = category(t, database, "Groceries", "#FF5722")
	state := loaded(t, appPaths, database)
	file := ruleFile(t, t.TempDir(), "rules.tsv", []string{
		"Groceries\tmusterladen\tpartner\texpense\t60\ttrue",
	})
	if err := state.ChooseRuleImport(testContext(), database, file); err != nil {
		t.Fatalf("ChooseRuleImport: %v", err)
	}

	if err := state.ConfirmRuleImport(testContext(), database, true); err != nil {
		t.Fatalf("first ConfirmRuleImport: %v", err)
	}
	if err := state.ConfirmRuleImport(testContext(), database, true); err != nil {
		t.Fatalf("second ConfirmRuleImport: %v", err)
	}

	listed, err := rulerepo.List(testContext(), database)
	if err != nil {
		t.Fatalf("rules.List: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("len(rules) = %d, want 1: the rule is created once", len(listed))
	}
	if got := len(auditDetails(t, database, "IMPORT_RULES")); got != 1 {
		t.Errorf("IMPORT_RULES entries = %d, want 1", got)
	}
}

// TaxOverrideTest.kt (the settings part)

func TestSaveRuleStoresTheTaxFlagAndAnExplicitNullStoresNull(t *testing.T) {
	appPaths, database := fixture(t)
	groceries := category(t, database, "Groceries", "#FF5722")
	state := New(i18n.En)
	if err := state.Load(testContext(), database, appPaths, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := state.SaveRule(
		testContext(),
		database,
		nil,
		"Beispiel Shop",
		groceries,
		5,
		true,
		model.RuleFieldAny,
		model.RuleDirectionAny,
		pointer(true),
	); err != nil {
		t.Fatalf("first SaveRule: %v", err)
	}
	listed, err := rulerepo.List(testContext(), database)
	if err != nil || len(listed) == 0 {
		t.Fatalf("rules.List: %v", err)
	}
	item := listed[0]
	if item.TaxRelevant == nil || !*item.TaxRelevant {
		t.Errorf("tax flag = %v, want true", item.TaxRelevant)
	}
	ruleID := item.ID

	if err := state.SaveRule(
		testContext(),
		database,
		&ruleID,
		"Beispiel Shop",
		groceries,
		5,
		true,
		model.RuleFieldAny,
		model.RuleDirectionAny,
		nil,
	); err != nil {
		t.Fatalf("second SaveRule: %v", err)
	}
	listed, err = rulerepo.List(testContext(), database)
	if err != nil || len(listed) == 0 {
		t.Fatalf("rules.List after update: %v", err)
	}
	after := listed[0]
	if after.TaxRelevant != nil {
		t.Errorf("tax flag = %v, want nil: explicit null stores 'as the category'", *after.TaxRelevant)
	}
	if after.ID != ruleID {
		t.Errorf("rule id = %d, want %d: the edit updated the same rule", after.ID, ruleID)
	}
}

// TaxTabTest.kt (the settings part)

func TestSavingKeywordsStoresTheCleanedListAndResetReturnsToTheDefaults(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)
	if state.TaxKeywords != strings.Join(DefaultTaxKeywords, "\n") {
		t.Fatalf("tax keywords = %q, want defaults", state.TaxKeywords)
	}

	if err := state.SaveTaxKeywords(
		testContext(),
		database,
		"  Steuerberater \n\n handwerker\nKita\nHANDWERKER  \n",
	); err != nil {
		t.Fatalf("SaveTaxKeywords: %v", err)
	}

	keywords, err := TaxKeywords(testContext(), database)
	if err != nil {
		t.Fatalf("TaxKeywords: %v", err)
	}
	wantKeywords := []string{"Steuerberater", "handwerker", "Kita"}
	if !reflect.DeepEqual(keywords, wantKeywords) {
		t.Errorf(
			"tax keywords = %v, want %v: trimmed, non-blank, de-duplicated case-insensitively",
			keywords,
			wantKeywords,
		)
	}
	if state.TaxKeywords != "Steuerberater\nhandwerker\nKita" {
		t.Errorf("state tax keywords = %q", state.TaxKeywords)
	}

	saved := auditDetails(t, database, "SAVE_TAX_KEYWORDS")
	if len(saved) != 1 {
		t.Fatalf("SAVE_TAX_KEYWORDS entries = %v, want 1", saved)
	}
	if !strings.Contains(saved[0], "3") {
		t.Errorf("the entry names the count only: %s", saved[0])
	}

	if err := state.ResetTaxKeywords(testContext(), database); err != nil {
		t.Fatalf("ResetTaxKeywords: %v", err)
	}
	keywords, err = TaxKeywords(testContext(), database)
	if err != nil {
		t.Fatalf("TaxKeywords after reset: %v", err)
	}
	if !reflect.DeepEqual(keywords, DefaultTaxKeywords) {
		t.Errorf("tax keywords = %v, want defaults %v", keywords, DefaultTaxKeywords)
	}
	if state.TaxKeywords != strings.Join(DefaultTaxKeywords, "\n") {
		t.Errorf("state tax keywords = %q, want defaults", state.TaxKeywords)
	}
	resets := auditDetails(t, database, "RESET_TAX_KEYWORDS")
	if len(resets) != 1 {
		t.Fatalf("RESET_TAX_KEYWORDS entries = %v, want 1", resets)
	}
	if !strings.Contains(resets[0], "3") {
		t.Errorf("the reset entry names the count only: %s", resets[0])
	}
	if _, ok, err := settings.Get(testContext(), database, TaxKeywordsKey); err != nil || ok {
		t.Errorf("the custom list is gone: ok = %v, err = %v", ok, err)
	}
}

// InsightsTest.kt (the settings part)

func TestABudgetCanBeRemovedAgain(t *testing.T) {
	appPaths, database := fixture(t)
	food := category(t, database, "Food", "#FF5722")
	if err := categories.Update(testContext(), database, food, model.NewCategory{
		Name:        "Food",
		Color:       "#FF5722",
		BudgetLimit: &money.Money{Minor: 10_000, Currency: money.EUR},
	}); err != nil {
		t.Fatalf("categories.Update: %v", err)
	}
	state := loaded(t, appPaths, database)

	if err := state.SaveCategory(
		testContext(),
		database,
		&food,
		"Food",
		"#FF5722",
		nil,
		false,
		false,
	); err != nil {
		t.Fatalf("SaveCategory: %v", err)
	}

	saved, ok, err := categories.Get(testContext(), database, food)
	if err != nil || !ok {
		t.Fatalf("categories.Get: %v, %v", ok, err)
	}
	if saved.BudgetLimit != nil {
		t.Errorf("budget = %v, want nil", saved.BudgetLimit)
	}
}

// ViewModelsTest.kt (the settings part)

func TestSettingsValidatesRulesAndDeletingACategoryUnassignsTransactions(t *testing.T) {
	appPaths, database := fixture(t)
	food := category(t, database, "Food", "#4CAF50")
	accountID := account(t, database)
	transaction(t, database, accountID, &food)
	if _, err := rulerepo.Create(testContext(), database, model.NewRule{
		Pattern:    "rewe",
		CategoryID: food,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}); err != nil {
		t.Fatalf("rules.Create: %v", err)
	}
	state := loaded(t, appPaths, database)

	if state.ValidateRule("(unclosed") == nil {
		t.Error("an unclosed group must be refused")
	}
	if message := state.ValidateRule("rewe|edeka"); message != nil {
		t.Errorf("rewe|edeka = %q, want valid", *message)
	}
	wantDuplicate := i18n.Text(i18n.En, i18n.ErrorCategoryExists)
	if message := state.ValidateCategory("food", "#000000", nil); message == nil || *message != wantDuplicate {
		t.Errorf("duplicate category = %v, want %q", message, wantDuplicate)
	}

	if err := state.DeleteCategory(testContext(), database, food); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	bookings, err := transactions.ListForAccount(testContext(), database, accountID, nil, nil)
	if err != nil || len(bookings) == 0 {
		t.Fatalf("ListForAccount: %v, %v", bookings, err)
	}
	if bookings[0].CategoryID != nil {
		t.Errorf("booking category = %v, want nil", bookings[0].CategoryID)
	}
	listed, err := rulerepo.List(testContext(), database)
	if err != nil {
		t.Fatalf("rules.List: %v", err)
	}
	if len(listed) != 0 {
		t.Error("its rules are removed")
	}
	if got := len(auditDetails(t, database, "category.delete")); got != 1 {
		t.Errorf("category.delete entries = %d, want 1", got)
	}
}

// TestRenamingTheDefaultProfileUsesTheDefaultRegistryEntry covers the nil slug
// path, which Rust passes as None to profiles::rename.
func TestRenamingTheDefaultProfileUsesTheDefaultRegistryEntry(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)

	if err := state.RenameProfile(testContext(), database, appPaths, nil, "Household"); err != nil {
		t.Fatalf("RenameProfile: %v", err)
	}

	active, err := profiles.Active(appPaths)
	if err != nil {
		t.Fatalf("profiles.Active: %v", err)
	}
	if active.Slug != "" || active.Name != "Household" {
		t.Errorf("default profile = %+v, want slug \"\" and name Household", active)
	}
	if len(state.Profiles) == 0 || state.Profiles[0].Name != "Household" {
		t.Errorf("state profiles = %+v", state.Profiles)
	}
	got := auditDetails(t, database, "PROFILE_RENAMED")
	want := []string{"Profile default renamed to Household"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audit details = %v, want %v", got, want)
	}
}

// SavedPinTest.kt (profile deletion remembers to forget the PINs)

func TestDeletingAProfileForgetsThePinsBeforeMovingTheFolder(t *testing.T) {
	appPaths, database := fixture(t)
	business, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("profiles.Create: %v", err)
	}
	slug := business.Slug
	passports := filepath.Join(business.Dir, "passports")
	if err := os.MkdirAll(passports, 0o700); err != nil {
		t.Fatalf("MkdirAll passports: %v", err)
	}
	passport := filepath.Join(passports, "bank-1.dat")
	if err := os.WriteFile(passport, []byte("passport"), 0o600); err != nil {
		t.Fatalf("write passport: %v", err)
	}

	state := loaded(t, appPaths, database)
	seen := make([]string, 0)
	existed := false
	hook := func(got string) {
		seen = append(seen, got)
		existed = fileExists(passport)
	}

	if err := state.DeleteProfile(testContext(), database, appPaths, nil, slug, hook); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	if !reflect.DeepEqual(seen, []string{slug}) {
		t.Errorf("forgetPins calls = %v, want [%s]", seen, slug)
	}
	if !existed {
		t.Error("the hook runs while the profile folder still exists")
	}
	if dirExists(business.Dir) {
		t.Error("the profile folder moved")
	}
	listed, err := profiles.List(appPaths)
	if err != nil {
		t.Fatalf("profiles.List: %v", err)
	}
	for _, profile := range listed {
		if profile.Slug == slug {
			t.Errorf("profile %s is still registered", slug)
		}
	}
	if state.Message == nil || !strings.Contains(*state.Message, "Profile moved to") {
		t.Errorf("message = %v", state.Message)
	}
	if got := len(auditDetails(t, database, "PROFILE_DELETED")); got != 1 {
		t.Errorf("PROFILE_DELETED entries = %d, want 1", got)
	}
}

func TestARefusedProfileDeleteForgetsNoPins(t *testing.T) {
	appPaths, database := fixture(t)
	business, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("profiles.Create: %v", err)
	}
	slug := business.Slug
	state := loaded(t, appPaths, database)
	calls := 0
	hook := func(string) {
		calls++
	}

	// The open profile cannot be deleted.
	if err := state.DeleteProfile(testContext(), database, appPaths, &slug, slug, hook); err == nil {
		t.Error("an open profile must be refused")
	}
	if calls != 0 {
		t.Errorf("forgetPins calls = %d, want 0: an open profile is refused before forgetting PINs", calls)
	}
	if !dirExists(business.Dir) {
		t.Error("the folder stays")
	}

	// An unknown profile cannot be deleted either.
	if err := state.DeleteProfile(testContext(), database, appPaths, nil, "missing", hook); err == nil {
		t.Error("an unknown profile must be refused")
	}
	if calls != 0 {
		t.Errorf("forgetPins calls = %d, want 0: an unknown profile is refused before forgetting PINs", calls)
	}

	// The default profile cannot be deleted.
	if err := state.DeleteProfile(testContext(), database, appPaths, nil, "default", hook); err == nil {
		t.Error("the default profile must be refused")
	}
	if calls != 0 {
		t.Errorf("forgetPins calls = %d, want 0: the default profile is refused before forgetting PINs", calls)
	}
	if got := len(auditDetails(t, database, "PROFILE_DELETED")); got != 0 {
		t.Errorf("PROFILE_DELETED entries = %d, want 0", got)
	}
}

// The backup and restore flow the Settings page drives.

func TestABackupCanBeCreatedChosenStagedAndCancelled(t *testing.T) {
	appPaths, database := fixture(t)
	state := loaded(t, appPaths, database)
	backupPath := filepath.Join(t.TempDir(), "backup.zip")

	if err := state.CreateBackup(testContext(), database, appPaths, nil, backupPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if !fileExists(backupPath) {
		t.Fatal("the backup file is missing")
	}
	if state.IsBusy {
		t.Error("is_busy is still set")
	}
	if state.Message == nil || !strings.HasPrefix(*state.Message, "Backup saved to backup.zip") {
		t.Errorf("message = %v", state.Message)
	}
	if got := len(auditDetails(t, database, "BACKUP_CREATED")); got != 1 {
		t.Errorf("BACKUP_CREATED entries = %d, want 1", got)
	}

	if err := state.ChooseRestore(appPaths, nil, backupPath); err != nil {
		t.Fatalf("ChooseRestore: %v", err)
	}
	if state.RestoreCandidate == nil {
		t.Fatal("the restore candidate is missing")
	}
	if state.RestoreCandidate.Info.Transactions != 0 {
		t.Errorf("transactions = %d, want 0", state.RestoreCandidate.Info.Transactions)
	}

	if err := state.ConfirmRestore(testContext(), database, appPaths, nil); err != nil {
		t.Fatalf("ConfirmRestore: %v", err)
	}
	if !state.HasPendingRestore {
		t.Error("the restore is not pending")
	}
	if state.RestoreCandidate != nil {
		t.Error("the candidate is consumed")
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessageRestorePrepared) {
		t.Errorf("message = %v", state.Message)
	}
	if got := len(auditDetails(t, database, "RESTORE_STAGED")); got != 1 {
		t.Errorf("RESTORE_STAGED entries = %d, want 1", got)
	}

	if err := state.CancelPendingRestore(testContext(), database, appPaths, nil); err != nil {
		t.Fatalf("CancelPendingRestore: %v", err)
	}
	if state.HasPendingRestore {
		t.Error("the pending restore is still there")
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessageRestoreCancelled) {
		t.Errorf("message = %v", state.Message)
	}
}

// TestDeleteProfileForgetsPinsBeforeTheFolderMoves covers the callback contract
// in one test: it runs once with the slug while the folder still exists, and a
// refused delete never calls it.
func TestDeleteProfileForgetsPinsBeforeTheFolderMoves(t *testing.T) {
	appPaths, database := fixture(t)
	business, err := profiles.Create(appPaths, "Business")
	if err != nil {
		t.Fatalf("profiles.Create: %v", err)
	}
	slug := business.Slug
	passports := filepath.Join(business.Dir, "passports")
	if err := os.MkdirAll(passports, 0o700); err != nil {
		t.Fatalf("MkdirAll passports: %v", err)
	}
	passport := filepath.Join(passports, "bank-1.dat")
	if err := os.WriteFile(passport, []byte("passport"), 0o600); err != nil {
		t.Fatalf("write passport: %v", err)
	}
	state := loaded(t, appPaths, database)

	calls := make([]string, 0)
	hook := func(got string) {
		calls = append(calls, got)
		if !fileExists(passport) {
			t.Error("forgetPins ran after the profile folder moved")
		}
	}

	if err := state.DeleteProfile(testContext(), database, appPaths, &slug, slug, hook); err == nil {
		t.Error("an open profile must be refused")
	}
	if err := state.DeleteProfile(testContext(), database, appPaths, nil, "missing", hook); err == nil {
		t.Error("an unknown profile must be refused")
	}
	if len(calls) != 0 {
		t.Fatalf("a refused delete called forgetPins: %v", calls)
	}
	if !dirExists(business.Dir) || !fileExists(passport) {
		t.Fatal("a refused delete moved the folder")
	}

	if err := state.DeleteProfile(testContext(), database, appPaths, nil, slug, hook); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{slug}) {
		t.Errorf("forgetPins calls = %v, want [%s]", calls, slug)
	}
	if dirExists(business.Dir) || fileExists(passport) {
		t.Error("the profile folder moved")
	}
}

// TestRestorePassphraseIsWipedAndNeverKept checks that no attempt leaves the
// passphrase in the state and that the state has no field of a secret type.
func TestRestorePassphraseIsWipedAndNeverKept(t *testing.T) {
	appPaths, database := fixture(t)
	otherPassphrase := "a different backup passphrase"
	otherDir := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("MkdirAll other: %v", err)
	}
	otherSecret := mustPassphrase(t, otherPassphrase)
	defer otherSecret.Wipe()
	otherDatabase, err := db.Create(filepath.Join(otherDir, backup.DBName), otherSecret)
	if err != nil {
		t.Fatalf("db.Create other: %v", err)
	}
	t.Cleanup(func() {
		if err := otherDatabase.Close(); err != nil {
			t.Errorf("close other database: %v", err)
		}
	})
	backupPath := filepath.Join(t.TempDir(), "other.zip")
	if _, err := backup.NewService(otherDir, "test").CreateBackup(otherDatabase, backupPath); err != nil {
		t.Fatalf("CreateBackup other: %v", err)
	}

	state := loaded(t, appPaths, database)
	if err := state.ChooseRestore(appPaths, nil, backupPath); err != nil {
		t.Fatalf("ChooseRestore: %v", err)
	}
	if err := state.ConfirmRestore(testContext(), database, appPaths, nil); err != nil {
		t.Fatalf("ConfirmRestore: %v", err)
	}
	if state.RestorePassphraseFor == nil {
		t.Fatal("the state must ask for the backup's passphrase")
	}
	assertNoPassphraseInState(t, state, testPassphrase, otherPassphrase)

	wrong := []byte("wrong passphrase")
	defer wipeBytes(wrong)
	if err := state.ConfirmRestoreWithPassphrase(
		testContext(),
		database,
		appPaths,
		nil,
		wrong,
	); err != nil {
		t.Fatalf("ConfirmRestoreWithPassphrase wrong: %v", err)
	}
	if !state.RestorePassphraseWrong {
		t.Error("the wrong passphrase is not recorded")
	}
	if state.RestorePassphraseFor == nil {
		t.Error("the state must keep asking after a wrong passphrase")
	}
	assertNoPassphraseInState(t, state, testPassphrase, otherPassphrase, string(wrong))

	correct := []byte(otherPassphrase)
	defer wipeBytes(correct)
	if err := state.ConfirmRestoreWithPassphrase(
		testContext(),
		database,
		appPaths,
		nil,
		correct,
	); err != nil {
		t.Fatalf("ConfirmRestoreWithPassphrase: %v", err)
	}
	if !state.HasPendingRestore {
		t.Error("the restore is not pending")
	}
	if state.RestorePassphraseFor != nil {
		t.Error("the passphrase request is not cleared")
	}
	if state.RestorePassphraseWrong {
		t.Error("the wrong flag is not cleared")
	}
	if state.RestoreCandidate != nil {
		t.Error("the candidate is not cleared")
	}
	if state.Message == nil || *state.Message != i18n.Text(i18n.En, i18n.MessageRestorePrepared) {
		t.Errorf("message = %v", state.Message)
	}
	assertNoPassphraseInState(t, state, testPassphrase, otherPassphrase, string(wrong), string(correct))

	assertNoSecretFields(t, reflect.TypeOf(SettingsState{}))
	assertNoSecretFields(t, reflect.TypeOf(RestoreCandidate{}))
}

func assertNoPassphraseInState(t *testing.T, state *SettingsState, secrets ...string) {
	t.Helper()
	rendered := []string{
		fmt.Sprintf("%v", state),
		fmt.Sprintf("%+v", state),
		fmt.Sprintf("%#v", state),
	}
	for _, secretValue := range secrets {
		if secretValue == "" {
			continue
		}
		for _, text := range rendered {
			if strings.Contains(text, secretValue) {
				t.Fatalf("the state renders the passphrase %q: %s", secretValue, text)
			}
		}
	}
}

func assertNoSecretFields(t *testing.T, stateType reflect.Type) {
	t.Helper()
	passphraseType := reflect.TypeOf(secret.Passphrase{})
	passphrasePointerType := reflect.TypeOf(&secret.Passphrase{})
	for index := 0; index < stateType.NumField(); index++ {
		field := stateType.Field(index)
		if field.Type == passphraseType || field.Type == passphrasePointerType {
			t.Errorf("%s.%s holds a secret type", stateType.Name(), field.Name)
		}
	}
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
