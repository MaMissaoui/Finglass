package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/app/startup"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulerepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
)

type settingsSeed struct {
	food   model.CategoryID
	salary model.CategoryID
	rule   model.RuleID
}

func seedSettingsDatabase(t *testing.T, appPaths paths.AppPaths) settingsSeed {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")

	food, err := categories.Create(ctx, database, model.NewCategory{
		Name:          "Food",
		Color:         "#FF5722",
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
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
	return settingsSeed{food: food, salary: salary, rule: rule}
}

func seedDuplicateColours(t *testing.T, appPaths paths.AppPaths) {
	t.Helper()
	ctx := context.Background()
	database := createTestDatabase(t, appPaths, "default")
	for _, name := range []string{"One", "Two"} {
		if _, err := categories.Create(ctx, database, model.NewCategory{
			Name:  name,
			Color: "#AABBCC",
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}
}

func waitForSettings(
	t *testing.T,
	session *Session,
	events <-chan Change,
	description string,
	predicate func(settingsview.SettingsState) bool,
) settingsview.SettingsState {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case change := <-events:
			if change.Page != "settings" {
				continue
			}
			state := session.SettingsState()
			if predicate(state) {
				return state
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func newLoadedSettingsSession(t *testing.T, appPaths paths.AppPaths) *Session {
	t.Helper()
	session := newTestSession(t, appPaths)
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	waitForJobs(t, session)

	events, remove := listenForChanges(session)
	defer remove()
	session.Navigate("settings")
	waitForSettings(t, session, events, "the loaded settings", func(state settingsview.SettingsState) bool {
		return !state.IsLoading
	})
	return session
}

func settingsAuditActions(t *testing.T, session *Session) []string {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Action)
	}
	return names
}

func settingsAuditDetails(t *testing.T, session *Session, action string) []string {
	t.Helper()
	entries, err := audit.Recent(context.Background(), sessionDatabase(t, session), 1000)
	if err != nil {
		t.Fatalf("recent audit entries: %v", err)
	}
	details := make([]string, 0)
	for _, entry := range entries {
		if entry.Action == action {
			details = append(details, entry.Details)
		}
	}
	return details
}

func containsAction(actions []string, action string) bool {
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}

func TestSettingsLoadCarriesTheSeededCategoriesRulesAndCurrency(t *testing.T) {
	appPaths := testPaths(t)
	seed := seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	state := session.SettingsState()
	if len(state.Categories) != 2 || len(state.Rules) != 1 {
		t.Fatalf("loaded %d categories and %d rules", len(state.Categories), len(state.Rules))
	}
	if state.Rules[0].ID != seed.rule || state.Rules[0].CategoryID != seed.food {
		t.Fatalf("rule = %+v", state.Rules[0])
	}
	if state.PrimaryCurrency != money.EUR || len(state.CurrencyOptions) < 11 {
		t.Fatalf("currency = %v / %+v", state.PrimaryCurrency, state.CurrencyOptions)
	}
	if session.SettingsSection() != SettingsAppearance {
		t.Fatalf("section = %d, want appearance", session.SettingsSection())
	}

	session.NavigateToSection("settings", SettingsCategories)
	waitForJobs(t, session)
	if session.SettingsSection() != SettingsCategories {
		t.Fatalf("section after navigation = %d", session.SettingsSection())
	}
	if session.CurrentScreen() != "settings" {
		t.Fatalf("screen = %q", session.CurrentScreen())
	}
}

func TestSettingsCategoryValidationUsesTheLocalizedTexts(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	session.SettingsOpenAddCategory()
	dialog := session.SettingsDialog()
	if !dialog.CategoryOpen || dialog.CategoryID != -1 || dialog.CategoryColour != categories.Colors[0] {
		t.Fatalf("add dialog = %+v", dialog)
	}

	session.SettingsCategoryChanged("", "#123456")
	if dialog := session.SettingsDialog(); dialog.CategoryError != i18n.Text(i18n.En, i18n.ErrorNameRequired) {
		t.Fatalf("blank name error = %q", dialog.CategoryError)
	}
	session.SettingsCategoryChanged("Food", "#123456")
	if dialog := session.SettingsDialog(); dialog.CategoryError != i18n.Text(i18n.En, i18n.ErrorCategoryExists) {
		t.Fatalf("duplicate error = %q", dialog.CategoryError)
	}
	session.SettingsCategoryChanged("Snacks", "#zzzzzz")
	if dialog := session.SettingsDialog(); dialog.CategoryError != i18n.Text(i18n.En, i18n.ErrorColorFormat) {
		t.Fatalf("colour error = %q", dialog.CategoryError)
	}

	session.SettingsSaveCategory("Snacks", "#123456", "not a number", false, false)
	if dialog := session.SettingsDialog(); dialog.CategoryError != i18n.Text(i18n.En, i18n.BudgetNotANumber) {
		t.Fatalf("budget error = %q", dialog.CategoryError)
	}
	session.SettingsCloseCategoryDialog()
	if dialog := session.SettingsDialog(); dialog.CategoryOpen || dialog.CategoryError != "" {
		t.Fatalf("close left the dialog open: %+v", dialog)
	}
}

func TestSettingsCategoryActionsSaveEditAndDelete(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	session.SettingsOpenAddCategory()
	session.SettingsSaveCategory("Snacks", "#123456", "12,50", true, false)
	state := waitForSettings(t, session, events, "the saved category", func(state settingsview.SettingsState) bool {
		return len(state.Categories) == 3
	})
	var snacks *model.Category
	for index := range state.Categories {
		if state.Categories[index].Name == "Snacks" {
			snacks = &state.Categories[index]
			break
		}
	}
	if snacks == nil {
		t.Fatal("the category was not saved")
	}
	if snacks.BudgetLimit == nil || *snacks.BudgetLimit != (money.Money{Minor: 1_250, Currency: money.EUR}) {
		t.Fatalf("budget = %+v", snacks.BudgetLimit)
	}
	if !snacks.IsTaxRelevant || snacks.IsTransfer || snacks.Color != "#123456" {
		t.Fatalf("category = %+v", snacks)
	}
	if dialog := session.SettingsDialog(); dialog.CategoryOpen {
		t.Fatalf("the dialog stayed open: %+v", dialog)
	}

	session.SettingsOpenEditCategory(snacks.ID)
	if dialog := session.SettingsDialog(); dialog.CategoryName != "Snacks" || dialog.CategoryBudget != "12.50" {
		t.Fatalf("edit dialog = %+v", dialog)
	}
	session.SettingsSaveCategory("Snacks", "#123456", "", false, true)
	state = waitForSettings(t, session, events, "the edited category", func(state settingsview.SettingsState) bool {
		for _, category := range state.Categories {
			if category.ID == snacks.ID {
				return category.BudgetLimit == nil && category.IsTransfer
			}
		}
		return false
	})
	if len(state.Categories) != 3 {
		t.Fatalf("edit changed the category count to %d", len(state.Categories))
	}

	session.SettingsRequestDeleteCategory(snacks.ID)
	if dialog := session.SettingsDialog(); !dialog.CategoryDeleteOpen || !strings.Contains(dialog.CategoryDeleteTitle, "Snacks") {
		t.Fatalf("delete dialog = %+v", dialog)
	}
	session.SettingsConfirmDeleteCategory()
	state = waitForSettings(t, session, events, "the deleted category", func(state settingsview.SettingsState) bool {
		return len(state.Categories) == 2
	})
	for _, category := range state.Categories {
		if category.ID == snacks.ID {
			t.Fatal("the deleted category is still there")
		}
	}
	if dialog := session.SettingsDialog(); dialog.CategoryDeleteOpen {
		t.Fatalf("the delete dialog stayed open: %+v", dialog)
	}

	actions := settingsAuditActions(t, session)
	for _, action := range []string{"category.create", "category.update", "category.delete"} {
		if !containsAction(actions, action) {
			t.Fatalf("audit actions = %v, missing %s", actions, action)
		}
	}
}

func TestSettingsDefaultCategoriesAndDuplicateColours(t *testing.T) {
	appPaths := testPaths(t)
	seedDuplicateColours(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	session.SettingsFixDuplicateColours()
	state := waitForSettings(t, session, events, "the fixed colours", func(state settingsview.SettingsState) bool {
		return state.Message != nil && strings.Contains(*state.Message, "recoloured")
	})
	if !settingsHasNoDuplicateColoursForTest(state.Categories) {
		t.Fatalf("duplicate colours remain: %+v", state.Categories)
	}
	if !containsAction(settingsAuditActions(t, session), "FIX_CATEGORY_COLOURS") {
		t.Fatal("the recolouring was not audited")
	}

	session.SettingsAddDefaultCategories()
	state = waitForSettings(t, session, events, "the default categories", func(state settingsview.SettingsState) bool {
		return len(state.Categories) >= 24
	})
	if state.Message == nil || !strings.Contains(*state.Message, "default categories added") {
		t.Fatalf("message = %v", state.Message)
	}
	if !containsAction(settingsAuditActions(t, session), "ADD_DEFAULT_CATEGORIES") {
		t.Fatal("the default categories were not audited")
	}
}

func settingsHasNoDuplicateColoursForTest(categories []model.Category) bool {
	seen := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		key := strings.ToUpper(category.Color)
		if _, present := seen[key]; present {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func TestSettingsTaxKeywordsSaveAndReset(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	session.SettingsSaveTaxKeywords("arzt\n\n  apotheke  \nhandwerk\n")
	state := waitForSettings(t, session, events, "the saved keywords", func(state settingsview.SettingsState) bool {
		return state.TaxKeywords == "arzt\napotheke\nhandwerk"
	})
	if state.Message != nil && *state.Message != "" {
		t.Fatalf("unexpected message %q", *state.Message)
	}
	if details := settingsAuditDetails(t, session, "SAVE_TAX_KEYWORDS"); len(details) != 1 ||
		details[0] != "Saved 3 tax suggestion keywords" {
		t.Fatalf("save audit = %v", details)
	}

	session.SettingsResetTaxKeywords()
	state = waitForSettings(t, session, events, "the reset keywords", func(state settingsview.SettingsState) bool {
		return state.TaxKeywords != "arzt\napotheke\nhandwerk" && state.TaxKeywords != ""
	})
	if details := settingsAuditDetails(t, session, "RESET_TAX_KEYWORDS"); len(details) != 1 ||
		details[0] != "Reset 3 tax suggestion keywords" {
		t.Fatalf("reset audit = %v", details)
	}
}

func TestSettingsRuleActionsValidateSaveEditAndDelete(t *testing.T) {
	appPaths := testPaths(t)
	seed := seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	session.SettingsOpenAddRule()
	if dialog := session.SettingsDialog(); !dialog.RuleOpen || dialog.RuleID != -1 ||
		dialog.RulePriority != "0" || !dialog.RuleAutoApply {
		t.Fatalf("add rule dialog = %+v", dialog)
	}

	session.SettingsRuleChanged("", "0")
	if dialog := session.SettingsDialog(); dialog.RuleError != i18n.Text(i18n.En, i18n.ErrorPatternRequired) {
		t.Fatalf("blank pattern error = %q", dialog.RuleError)
	}
	session.SettingsRuleChanged("rewe", "not a number")
	if dialog := session.SettingsDialog(); dialog.RuleError != i18n.Text(i18n.En, i18n.PriorityMustBeNumber) {
		t.Fatalf("priority error = %q", dialog.RuleError)
	}
	session.SettingsRuleChanged("(", "50")
	if dialog := session.SettingsDialog(); dialog.RuleError == "" {
		t.Fatal("an invalid pattern was accepted")
	}

	categoryIndex := -1
	for index, category := range session.SettingsState().Categories {
		if category.ID == seed.salary {
			categoryIndex = index
			break
		}
	}
	if categoryIndex < 0 {
		t.Fatal("the salary category is missing")
	}
	session.SettingsSaveRule("employer", categoryIndex, "80", true, 1, 2, 1)
	state := waitForSettings(t, session, events, "the saved rule", func(state settingsview.SettingsState) bool {
		return len(state.Rules) == 2
	})
	var saved *model.Rule
	for index := range state.Rules {
		if state.Rules[index].Pattern == "employer" {
			saved = &state.Rules[index]
			break
		}
	}
	if saved == nil {
		t.Fatal("the rule was not saved")
	}
	if saved.CategoryID != seed.salary || saved.Priority != 80 || !saved.AutoApply ||
		saved.MatchField != model.RuleFieldPartner || saved.Direction != model.RuleDirectionIncome ||
		saved.TaxRelevant == nil || !*saved.TaxRelevant {
		t.Fatalf("saved rule = %+v", saved)
	}
	if dialog := session.SettingsDialog(); dialog.RuleOpen {
		t.Fatalf("the rule dialog stayed open: %+v", dialog)
	}

	session.SettingsOpenEditRule(saved.ID)
	if dialog := session.SettingsDialog(); dialog.RulePattern != "employer" || dialog.RulePriority != "80" ||
		dialog.RuleFieldIndex != 1 || dialog.RuleDirectionIndex != 2 || dialog.RuleTaxIndex != 1 {
		t.Fatalf("edit rule dialog = %+v", dialog)
	}
	session.SettingsSaveRule("aldi", 0, "10", false, 2, 1, 2)
	state = waitForSettings(t, session, events, "the edited rule", func(state settingsview.SettingsState) bool {
		for _, rule := range state.Rules {
			if rule.ID == saved.ID {
				return rule.Pattern == "aldi"
			}
		}
		return false
	})
	for _, rule := range state.Rules {
		if rule.ID == saved.ID {
			if rule.Priority != 10 || rule.AutoApply || rule.MatchField != model.RuleFieldPurpose ||
				rule.Direction != model.RuleDirectionExpense || rule.TaxRelevant == nil || *rule.TaxRelevant {
				t.Fatalf("edited rule = %+v", rule)
			}
		}
	}

	session.SettingsRequestDeleteRule(saved.ID)
	if dialog := session.SettingsDialog(); !dialog.RuleDeleteOpen || !strings.Contains(dialog.RuleDeleteTitle, "aldi") {
		t.Fatalf("delete rule dialog = %+v", dialog)
	}
	session.SettingsConfirmDeleteRule()
	waitForSettings(t, session, events, "the deleted rule", func(state settingsview.SettingsState) bool {
		return len(state.Rules) == 1
	})
	actions := settingsAuditActions(t, session)
	for _, action := range []string{"rule.create", "rule.update", "rule.delete"} {
		if !containsAction(actions, action) {
			t.Fatalf("audit actions = %v, missing %s", actions, action)
		}
	}
}

func TestSettingsExportWritesTheRuleFileAndACancelledDialogChangesNothing(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	dir := t.TempDir()
	target := filepath.Join(dir, "rules.tsv")
	dialogs := &fakeFileDialogs{
		rulesPath:   target,
		rulesResult: true,
	}
	session.SetFileDialogs(dialogs)
	session.SettingsExportRules()
	waitForSettings(t, session, events, "the exported rules", func(state settingsview.SettingsState) bool {
		return state.Message != nil && strings.Contains(*state.Message, "exported")
	})
	// #nosec G304 -- test path built from t.TempDir.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	want := "# FinDesk rules v1\n" +
		"category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\n" +
		"Food\trewe\tany\tany\t50\ttrue\n"
	if string(data) != want {
		t.Fatalf("export =\n%s\nwant\n%s", data, want)
	}

	if dialogs.rulesTitle != i18n.Text(i18n.En, i18n.ExportRules) {
		t.Fatalf("dialog title = %q", dialogs.rulesTitle)
	}
	if !strings.HasPrefix(dialogs.rulesName, "finglass-rules-") || !strings.HasSuffix(dialogs.rulesName, ".tsv") {
		t.Fatalf("default name = %q", dialogs.rulesName)
	}

	before, err := os.ReadFile(target) // #nosec G304 -- the test chose this temporary path.
	if err != nil {
		t.Fatalf("read export again: %v", err)
	}
	dialogs.rulesResult = false
	session.SettingsExportRules()
	waitForJobs(t, session)
	after, err := os.ReadFile(target) // #nosec G304 -- the test chose this temporary path.
	if err != nil {
		t.Fatalf("read export after cancel: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a cancelled export changed the file")
	}
}

func TestSettingsImportPreviewsConfirmsAndCancels(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	fixture := filepath.Join("..", "..", "..", "testdata", "inputs", "rules", "import_workflow.tsv")
	dialogs := &fakeFileDialogs{ruleFile: fixture, ruleFileResult: true}
	session.SetFileDialogs(dialogs)

	session.SettingsImportRules()
	state := waitForSettings(t, session, events, "the rule preview", func(state settingsview.SettingsState) bool {
		return state.RuleImport != nil
	})
	if state.RuleImport == nil {
		t.Fatal("no preview")
	}
	if len(state.RuleImport.NewRules) != 3 || len(state.RuleImport.CategoriesToCreate) != 2 {
		t.Fatalf("preview = %+v", state.RuleImport)
	}
	if dialog := session.SettingsDialog(); !dialog.RuleImportOpen || !dialog.RuleImportCategorize {
		t.Fatalf("preview dialog = %+v", dialog)
	}

	session.SettingsConfirmRuleImport(false)
	state = waitForSettings(t, session, events, "the imported rules", func(state settingsview.SettingsState) bool {
		return state.RuleImport == nil && len(state.Rules) == 4
	})
	if state.Message == nil || !strings.Contains(*state.Message, "added") {
		t.Fatalf("import message = %v", state.Message)
	}
	if !containsAction(settingsAuditActions(t, session), "IMPORT_RULES") {
		t.Fatal("the rule import was not audited")
	}

	before := len(state.Rules)
	session.SettingsImportRules()
	state = waitForSettings(t, session, events, "the second preview", func(state settingsview.SettingsState) bool {
		return state.RuleImport != nil
	})
	session.SettingsCancelRuleImport()
	state = waitForSettings(t, session, events, "the cancelled preview", func(state settingsview.SettingsState) bool {
		return state.RuleImport == nil
	})
	if len(state.Rules) != before {
		t.Fatal("cancelling the preview changed the rules")
	}

	dialogs.ruleFileResult = false
	session.SettingsImportRules()
	waitForJobs(t, session)
	if session.SettingsState().RuleImport != nil {
		t.Fatal("a cancelled dialog opened a preview")
	}
}

func TestASecondSettingsActionIsRefusedWhileOneRuns(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	session.pageLoadGate = func(page string) {
		if page != "settings" {
			return
		}
		entered <- struct{}{}
		<-release
	}

	session.SettingsSaveTaxKeywords("first")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first action did not start")
	}
	if !session.settingsWork.Active() {
		t.Fatal("the settings busy section is not active")
	}
	if session.profileBusy.Active() {
		t.Fatal("an ordinary settings action touched the profile busy flag")
	}
	session.SettingsAddDefaultCategories()
	close(release)
	state := waitForSettings(t, session, events, "the first action", func(state settingsview.SettingsState) bool {
		return !state.IsLoading && state.TaxKeywords == "first"
	})
	if len(state.Categories) != 0 {
		t.Fatal("the refused action added default categories")
	}
	if session.settingsWork.Active() {
		t.Fatal("the busy section did not end")
	}
}

func TestASupersededSettingsLoadIsDropped(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	entered := make(chan chan struct{}, 2)
	session.pageLoadGate = func(page string) {
		if page != "settings" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.SettingsLoad()
	first := <-entered
	session.mu.Lock()
	session.language = i18n.De
	session.mu.Unlock()
	session.SettingsLoad()
	second := <-entered

	close(second)
	waitForSettings(t, session, events, "the second load", func(state settingsview.SettingsState) bool {
		return !state.IsLoading && state.Language == i18n.De
	})
	close(first)
	waitForJobs(t, session)

	state := session.SettingsState()
	if state.Language != i18n.De || state.IsLoading {
		t.Fatalf("the superseded load published %+v", state)
	}
}

func TestTeardownDuringASettingsLoadDropsItAndClosesWhenTheJobEnds(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newLoadedSettingsSession(t, appPaths)

	entered := make(chan chan struct{}, 1)
	session.pageLoadGate = func(page string) {
		if page != "settings" {
			return
		}
		gate := make(chan struct{})
		entered <- gate
		<-gate
	}

	session.SettingsLoad()
	gate := <-entered
	generation := session.jobs.Generation()
	old := sessionDatabase(t, session)

	session.Teardown(startup.Startup{})
	requireOpen(t, old, "the old database during the settings load")

	done := make(chan struct{})
	session.jobs.WhenIdle(generation, func() { close(done) })
	close(gate)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the settings job did not end")
	}

	requireClosed(t, old, "the old database after the settings job ends")
	state := session.SettingsState()
	if len(state.Categories) != 0 || len(state.Rules) != 0 || state.IsLoading {
		t.Fatalf("the teardown did not drop the load: %+v", state)
	}
	if dialog := session.SettingsDialog(); dialog.CategoryOpen || dialog.RuleOpen {
		t.Fatalf("the teardown kept a dialog: %+v", dialog)
	}
}

func TestSettingsStateIsSafeWhileActionsRun(t *testing.T) {
	appPaths := testPaths(t)
	createClosedTestDatabase(t, appPaths, "default")
	session := newLoadedSettingsSession(t, appPaths)
	events, remove := listenForChanges(session)
	defer remove()

	var wait sync.WaitGroup
	stop := make(chan struct{})
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = session.SettingsState()
				_ = session.SettingsDialog()
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	for _, text := range []string{"one", "two", "three"} {
		session.SettingsSaveTaxKeywords(text)
		waitForSettings(t, session, events, "a keyword save while reading", func(state settingsview.SettingsState) bool {
			return state.TaxKeywords == text
		})
	}

	close(stop)
	wait.Wait()
}

func TestAListenerMayReadSettingsWhileItRuns(t *testing.T) {
	appPaths := testPaths(t)
	seedSettingsDatabase(t, appPaths)
	session := newTestSession(t, appPaths)

	called := make(chan struct{}, 8)
	session.AddListener(func(Change) {
		_ = session.SettingsState()
		_ = session.SettingsDialog()
		called <- struct{}{}
	})

	done := make(chan error, 1)
	go func() {
		done <- session.Unlock([]byte(testPassphrase))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unlock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Unlock deadlocked in a listener")
	}
	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was never called")
	}
	waitForJobs(t, session)
}

func TestBudgetInputRoundTripsThroughTheCoreParser(t *testing.T) {
	values := []*money.Money{
		nil,
		{Minor: 0, Currency: money.EUR},
		{Minor: 5, Currency: money.EUR},
		{Minor: 30_000, Currency: money.EUR},
		{Minor: -1_250, Currency: money.EUR},
	}
	for _, value := range values {
		text := budgetInput(value)
		parsed, _, ok := parseBudget(text)
		if !ok {
			t.Fatalf("parseBudget(%q) was refused", text)
		}
		if (value == nil) != (parsed == nil) {
			t.Fatalf("budgetInput(%v) became %v", value, parsed)
		}
		if value != nil && *parsed != *value {
			t.Fatalf("budgetInput(%v) parsed back as %v", value, parsed)
		}
	}

	if budget, _, ok := parseBudget("  "); !ok || budget != nil {
		t.Fatalf("blank budget = %v, %v", budget, ok)
	}
	for _, test := range []struct {
		text string
		want int64
	}{
		{"250", 25_000},
		{"250,50", 25_050},
		{"1.234,56", 123_456},
		{"€ 99.9", 9_990},
	} {
		budget, _, ok := parseBudget(test.text)
		if !ok || budget == nil || budget.Minor != test.want {
			t.Fatalf("parseBudget(%q) = %v, %v; want %d", test.text, budget, ok, test.want)
		}
	}
	if _, key, ok := parseBudget("not a number"); ok || key != i18n.BudgetNotANumber {
		t.Fatalf("parseBudget(not a number) = %v, %v", key, ok)
	}
}

func TestDropdownIndicesMapToTheRuleEnums(t *testing.T) {
	if fieldAt(0) != model.RuleFieldAny || fieldAt(1) != model.RuleFieldPartner ||
		fieldAt(2) != model.RuleFieldPurpose {
		t.Fatal("field_at does not map to the rule fields")
	}
	if directionAt(0) != model.RuleDirectionAny || directionAt(1) != model.RuleDirectionExpense ||
		directionAt(2) != model.RuleDirectionIncome {
		t.Fatal("direction_at does not map to the rule directions")
	}
	if taxAt(0) != nil || taxAt(1) == nil || !*taxAt(1) ||
		taxAt(2) == nil || *taxAt(2) {
		t.Fatal("tax_at does not map to the tax values")
	}
	if fieldIndex(model.RuleFieldPurpose) != 2 ||
		directionIndex(model.RuleDirectionIncome) != 2 ||
		taxIndex(boolPointer(false)) != 2 {
		t.Fatal("the index helpers do not map the enums")
	}
}

func boolPointer(value bool) *bool {
	return &value
}
