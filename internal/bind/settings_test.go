package bind

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/findesk"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/app/unlock"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/ruleimport"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

type settingsBindSeed struct {
	food   model.CategoryID
	salary model.CategoryID
	rule   model.RuleID
}

func seedBindSettings(t *testing.T, appPaths paths.AppPaths) settingsBindSeed {
	t.Helper()
	ctx := context.Background()
	database, err := unlock.Create(appPaths, "default", []byte(testPassphrase), []byte(testPassphrase), true)
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	budget := money.Money{Minor: 30_000, Currency: money.EUR}
	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:          "Food",
		Color:         "#FF5722",
		BudgetLimit:   &budget,
		IsTaxRelevant: true,
	})
	if err != nil {
		t.Fatalf("create food: %v", err)
	}
	salary, err := categories.Create(ctx, database, model.NewCategory{Name: "Salary", Color: "#2196F3"})
	if err != nil {
		t.Fatalf("create salary: %v", err)
	}
	rule, err := rulerepo.Create(ctx, database, model.NewRule{
		Pattern:    "rewe",
		CategoryID: food,
		Priority:   50,
		AutoApply:  true,
		MatchField: model.RuleFieldPartner,
		Direction:  model.RuleDirectionExpense,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return settingsBindSeed{food: food, salary: salary, rule: rule}
}

type settingsBindFixture struct {
	paths   paths.AppPaths
	session *shell.Session
	shell   *Shell
	service *Settings
	router  *Router
	seed    settingsBindSeed
	events  chan SettingsDTO
}

func newSettingsBindFixture(t *testing.T) *settingsBindFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seed := seedBindSettings(t, appPaths)

	session, offer, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	if offer != nil {
		t.Fatal("a nil FinDesk root offered a migration")
	}

	fixture := &settingsBindFixture{
		paths:   appPaths,
		session: session,
		seed:    seed,
		events:  make(chan SettingsDTO, 256),
	}
	fixture.shell = NewShell(session, offer, ShellOptions{
		Paths: appPaths,
		Clock: func() time.Time { return testNow },
	})
	fixture.service = NewSettings(session, SettingsOptions{
		Emit: func(dto SettingsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("settings", fixture.service)
	fixture.router.RegisterShell(fixture.shell)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.service.ServiceShutdown()
		_ = fixture.shell.ServiceShutdown()
	})
	return fixture
}

func (f *settingsBindFixture) wait(
	t *testing.T,
	description string,
	predicate func(SettingsDTO) bool,
) SettingsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.events:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func (f *settingsBindFixture) load(t *testing.T) SettingsDTO {
	t.Helper()
	if _, err := f.shell.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, err := f.service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f.wait(t, "the loaded settings", func(dto SettingsDTO) bool {
		return !dto.Loading && len(dto.CategoryRows) == 2
	})
}

func TestSettingsServiceLoadsEveryAppliedField(t *testing.T) {
	fixture := newSettingsBindFixture(t)
	dto := fixture.load(t)

	if dto.ErrorText != "" || dto.MessageText != "" {
		t.Fatalf("messages = %q / %q", dto.ErrorText, dto.MessageText)
	}
	if dto.CategoriesEmpty || dto.HasDuplicateColours {
		t.Fatalf("flags = empty %v duplicate %v", dto.CategoriesEmpty, dto.HasDuplicateColours)
	}
	if len(dto.CurrencyOptions) < 11 || dto.PrimaryCurrency != "EUR" || dto.PrimaryCurrencyIndex != 0 {
		t.Fatalf("currency = %+v / %q / %d", dto.CurrencyOptions, dto.PrimaryCurrency, dto.PrimaryCurrencyIndex)
	}
	if len(dto.CategoryRows) != 2 {
		t.Fatalf("category rows = %+v", dto.CategoryRows)
	}
	if dto.CategoryRows[0].Name != "Food" || dto.CategoryRows[0].BudgetText != "Budget €300.00" ||
		dto.CategoryRows[0].UsageText != "0 bookings" || !dto.CategoryRows[0].TaxRelevant {
		t.Fatalf("food row = %+v", dto.CategoryRows[0])
	}
	if len(dto.RuleRows) != 1 {
		t.Fatalf("rule rows = %+v", dto.RuleRows)
	}
	rule := dto.RuleRows[0]
	if rule.Pattern != "rewe" || rule.CategoryText != "Food" ||
		rule.Detail != "Partner name only · Expenses only" || rule.PrioText != "prio 50" {
		t.Fatalf("rule row = %+v", rule)
	}
	if len(dto.CategoryDialogSwatches) != len(categories.Colors) {
		t.Fatalf("swatches = %d", len(dto.CategoryDialogSwatches))
	}
	if len(dto.RuleDialogCategories) != 2 || len(dto.RuleDialogFieldOptions) != 3 ||
		len(dto.RuleDialogDirectionOptions) != 3 || len(dto.RuleDialogTaxOptions) != 3 {
		t.Fatalf("dialog options = %+v", dto)
	}
	if dto.TaxKeywordsCount == "" || !strings.HasSuffix(dto.TaxKeywordsCount, "keywords") {
		t.Fatalf("tax keyword count = %q", dto.TaxKeywordsCount)
	}
}

func TestSettingsChangedEventEqualsTheReturnedState(t *testing.T) {
	fixture := newSettingsBindFixture(t)
	fixture.load(t)

	returned, err := fixture.service.OpenAddCategory()
	if err != nil {
		t.Fatalf("OpenAddCategory: %v", err)
	}
	event := fixture.wait(t, "the opened category dialog", func(dto SettingsDTO) bool {
		return dto.CategoryDialogOpen
	})
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("settings:changed = %+v\nreturned = %+v", event, returned)
	}
	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(event, state) {
		t.Fatalf("settings:changed = %+v\nState = %+v", event, state)
	}
}

func TestCategoryRowsCarryBudgetUsageAndFlags(t *testing.T) {
	state := *settingsview.New(i18n.En)
	state.Categories = []model.Category{
		{
			ID:          1,
			Name:        "Food",
			Color:       "#FF5722",
			BudgetLimit: &money.Money{Minor: 30_000, Currency: money.EUR},
		},
		{ID: 2, Name: "Donations", Color: "#3BC7B0", IsTaxRelevant: true},
	}
	state.CategoryUsage = map[model.CategoryID]int{1: 5}

	rows := categoryRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].ID != 1 || rows[0].Name != "Food" || rows[0].BudgetText != "Budget €300.00" ||
		rows[0].UsageText != "5 bookings" {
		t.Fatalf("food row = %+v", rows[0])
	}
	if rows[1].BudgetText != "" || rows[1].UsageText != "0 bookings" || !rows[1].TaxRelevant {
		t.Fatalf("donations row = %+v", rows[1])
	}
}

func TestRuleRowsCarryCategoryTaxMarkerRestrictionsAndManualMarker(t *testing.T) {
	state := *settingsview.New(i18n.En)
	state.Categories = []model.Category{{ID: 1, Name: "Food", Color: "#FF5722"}}
	taxTrue := true
	taxFalse := false
	state.Rules = []model.Rule{
		{
			ID:          1,
			Pattern:     "rewe",
			CategoryID:  1,
			Priority:    50,
			AutoApply:   true,
			MatchField:  model.RuleFieldPartner,
			Direction:   model.RuleDirectionExpense,
			TaxRelevant: &taxTrue,
		},
		{
			ID:          2,
			Pattern:     "aldi",
			CategoryID:  1,
			Priority:    10,
			AutoApply:   false,
			MatchField:  model.RuleFieldPurpose,
			Direction:   model.RuleDirectionIncome,
			TaxRelevant: &taxFalse,
		},
	}

	rows := ruleRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].CategoryText != "Food · Tax" || rows[0].Detail != "Partner name only · Expenses only" ||
		rows[0].PrioText != "prio 50" {
		t.Fatalf("auto row = %+v", rows[0])
	}
	if rows[1].CategoryText != "Food · No tax" || rows[1].Detail != "Purpose only · Income only" ||
		rows[1].PrioText != "prio 10 · manual" {
		t.Fatalf("manual row = %+v", rows[1])
	}
}

func TestDuplicateColoursAreDetectedCaseInsensitively(t *testing.T) {
	categories := []model.Category{
		{ID: 1, Name: "A", Color: "#aabbcc"},
		{ID: 2, Name: "B", Color: "#AABBCC"},
		{ID: 3, Name: "C", Color: "#112233"},
	}
	if !hasDuplicateColours(categories) {
		t.Fatal("duplicate colours were not detected")
	}
	if hasDuplicateColours(categories[:1]) {
		t.Fatal("one colour was reported as a duplicate")
	}
}

func TestPrimaryCurrencyIndexMatchesTheOptionList(t *testing.T) {
	state := *settingsview.New(i18n.En)
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	xxx, err := money.NewCurrency("XXX")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	state.PrimaryCurrency = usd
	if index := primaryCurrencyIndex(state); index != 1 {
		t.Fatalf("USD index = %d, want 1", index)
	}
	state.PrimaryCurrency = xxx
	if index := primaryCurrencyIndex(state); index != 0 {
		t.Fatalf("unknown currency index = %d, want 0", index)
	}
}

func TestTaxKeywordCountIgnoresBlankLines(t *testing.T) {
	state := *settingsview.New(i18n.En)
	state.TaxKeywords = "arzt\n\n  apotheke  \nhandwerker\n"
	if got := taxKeywordsCount(state); got != "3 keywords" {
		t.Fatalf("count = %q, want 3 keywords", got)
	}
}

func TestRuleImportDTOTruncatesLongListsAndSizesTheDialog(t *testing.T) {
	state := *settingsview.New(i18n.En)
	conflicts := make([]ruleimport.RuleImportConflict, 25)
	errors := make([]rules.RuleFileError, 25)
	for index := range conflicts {
		conflicts[index] = ruleimport.RuleImportConflict{
			Line:             index + 1,
			Pattern:          "rewe",
			FileCategory:     "Groceries",
			ExistingCategory: "Food",
		}
		errors[index] = rules.RuleFileError{Line: index + 1, Reason: "bad row"}
	}
	state.RuleImport = &ruleimport.RuleImportPreview{
		CategoriesToCreate: []string{"Groceries", "Salary"},
		NewRules:           []rules.RuleFileRow{{Line: 3, Category: "Groceries"}},
		Conflicts:          conflicts,
		Errors:             errors,
		MatchCount:         7,
	}
	dialog := shell.SettingsDialogState{RuleImportOpen: true, RuleImportCategorize: false}

	dto := settingsDTO(state, dialog, 0, nil, findesk.FindeskImportState{Language: i18n.En})
	if !dto.RuleImportOpen || !dto.RuleImportShowCategorize || !dto.RuleImportConfirmEnabled {
		t.Fatalf("rule import flags = %+v", dto)
	}
	if dto.RuleImportCategorize {
		t.Fatal("the open dialog's unticked categorize box was overridden")
	}
	if !strings.HasSuffix(dto.RuleImportConflictsList, i18n.Format(i18n.En, i18n.AndMore, i18n.Int(5))) {
		t.Fatalf("conflict list = %q", dto.RuleImportConflictsList)
	}
	if !strings.HasSuffix(dto.RuleImportErrorsList, i18n.Format(i18n.En, i18n.AndMore, i18n.Int(5))) {
		t.Fatalf("error list = %q", dto.RuleImportErrorsList)
	}
	if strings.Count(dto.RuleImportConflictsList, "\n") != 20 {
		t.Fatalf("conflict list has %d lines", strings.Count(dto.RuleImportConflictsList, "\n")+1)
	}
	if dto.RuleImportCategorizeText != i18n.Format(
		i18n.En,
		i18n.RuleImportCategorize,
		i18n.Int(7),
	) {
		t.Fatalf("categorize text = %q", dto.RuleImportCategorizeText)
	}
}

// allowedSettingsParameters lists every exported Settings method that takes an
// argument, with the exact parameter names Rust's callbacks pass. Any method
// outside this list must take no arguments, so no path, file name, directory or
// file contents can reach a dialog from JavaScript.
var allowedSettingsParameters = map[string][]string{
	"SetPrimaryCurrency": {"code"},
	"OpenEditCategory":   {"id"},
	"OpenDeleteCategory": {"id"},
	"CategoryChanged":    {"name", "colour"},
	"SaveCategory":       {"name", "colour", "budget", "taxRelevant", "transfer"},
	"OpenEditRule":       {"id"},
	"OpenDeleteRule":     {"id"},
	"RuleChanged":        {"pattern", "priority"},
	"SaveRule": {
		"pattern",
		"categoryIndex",
		"priority",
		"autoApply",
		"fieldIndex",
		"flow",
		"taxIndex",
	},
	"SaveTaxKeywords":   {"text"},
	"ConfirmRuleImport": {"categorize"},

	"ConfirmRestorePassphrase": {"passphrase"},
	"SubmitFindeskPassphrase":  {"passphrase"},
	"RenameProfile":            {"index"},
	"SaveProfile":              {"name"},
	"DeleteProfile":            {"index"},
}

func TestSettingsBindingsNeverTakeAPath(t *testing.T) {
	actualNames := settingsParameterNames(t)
	typ := reflect.TypeOf(&Settings{})
	methods := make(map[string]bool, typ.NumMethod())
	for index := 0; index < typ.NumMethod(); index++ {
		method := typ.Method(index)
		methods[method.Name] = true
		allowed, listed := allowedSettingsParameters[method.Name]
		names := actualNames[method.Name]
		if !listed && method.Type.NumIn() != 1 {
			t.Fatalf("%s takes arguments but is not in the allowed list", method.Name)
		}
		if len(names) != method.Type.NumIn()-1 {
			t.Fatalf("%s parameter names = %v, but reflection sees %d", method.Name, names, method.Type.NumIn()-1)
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			for _, forbidden := range []string{"path", "file", "dir", "folder", "document", "content"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s parameter %q could carry a path", method.Name, name)
				}
			}
		}
		if !listed {
			continue
		}
		if len(allowed) != len(names) {
			t.Fatalf("%s parameters = %v, want %v", method.Name, names, allowed)
		}
		for position, name := range allowed {
			if names[position] != name {
				t.Fatalf("%s parameter %d = %q, want %q", method.Name, position, names[position], name)
			}
			parameter := method.Type.In(position + 1)
			switch parameter.Kind() {
			case reflect.Int, reflect.Bool, reflect.String:
			default:
				t.Fatalf("%s parameter %q has type %s", method.Name, name, parameter)
			}
		}
	}

	for _, name := range []string{"Load", "ImportRules", "ExportRules"} {
		if !methods[name] {
			t.Fatalf("%s is not exported", name)
		}
		if len(actualNames[name]) != 0 {
			t.Fatalf("%s takes %v", name, actualNames[name])
		}
	}
}

func settingsParameterNames(t *testing.T) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "settings.go", nil, 0)
	if err != nil {
		t.Fatalf("parse settings.go: %v", err)
	}
	names := make(map[string][]string)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		if receiverType(function.Recv.List[0].Type) != "Settings" {
			continue
		}
		names[function.Name.Name] = parameterNamesOf(function.Type.Params)
	}
	return names
}
