package shell

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/accountsview"
	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
)

// SettingsDialogState is the Settings page's dialog state without Slint.
//
// Rust keeps these fields on the window; the session keeps them here so a
// callback that takes no argument (save, confirm, cancel) can read the dialog
// it opened. The fields mirror settings_page.slint's dialog properties.
type SettingsDialogState struct {
	// CategoryOpen says whether the category dialog is open.
	CategoryOpen bool
	// CategoryID is the category being edited; -1 adds a new one.
	CategoryID int
	// CategoryName, CategoryColour and CategoryBudget are its fields.
	CategoryName   string
	CategoryColour string
	CategoryBudget string
	// CategoryTaxRelevant and CategoryTransfer are its checkboxes.
	CategoryTaxRelevant bool
	CategoryTransfer    bool
	// CategoryError is its validation message.
	CategoryError string

	// CategoryDeleteOpen says whether the category delete confirmation is open.
	CategoryDeleteOpen bool
	// CategoryDeleteID is the category it deletes.
	CategoryDeleteID int
	// CategoryDeleteTitle and CategoryDeleteText are its heading and body.
	CategoryDeleteTitle string
	CategoryDeleteText  string

	// RuleOpen says whether the rule dialog is open.
	RuleOpen bool
	// RuleID is the rule being edited; -1 adds a new one.
	RuleID int
	// RulePattern, RulePriority and RuleAutoApply are its fields.
	RulePattern   string
	RulePriority  string
	RuleAutoApply bool
	// RuleCategoryIndex is the selected category row.
	RuleCategoryIndex int
	// RuleFieldIndex, RuleDirectionIndex and RuleTaxIndex are its dropdowns.
	RuleFieldIndex     int
	RuleDirectionIndex int
	RuleTaxIndex       int
	// RuleError is its validation message.
	RuleError string

	// RuleDeleteOpen says whether the rule delete confirmation is open.
	RuleDeleteOpen bool
	// RuleDeleteID is the rule it deletes.
	RuleDeleteID int
	// RuleDeleteTitle and RuleDeleteText are its heading and body.
	RuleDeleteTitle string
	RuleDeleteText  string

	// RuleImportOpen says whether the rule-import preview was shown; it decides
	// whether the categorize box starts ticked for a new preview.
	RuleImportOpen bool
	// RuleImportCategorize is the preview's "categorize now" checkbox.
	RuleImportCategorize bool
}

// SettingsState returns a deep copy of the Settings page state.
func (s *Session) SettingsState() settingsview.SettingsState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return *settingsview.New(s.language)
	}
	return s.settings.Clone()
}

// SettingsDialog returns a copy of the Settings page's dialog state.
func (s *Session) SettingsDialog() SettingsDialogState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsDialog
}

// SettingsSection returns the section the page opens, as the shell records the
// section a navigation passed.
func (s *Session) SettingsSection() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsSection
}

// SettingsLoad loads the Settings page, as settings::spawn_load does.
func (s *Session) SettingsLoad() {
	s.mu.Lock()
	job := s.beginSettingsLoadLocked()
	s.mu.Unlock()
	s.startPageJob(job)
}

func (s *Session) beginSettingsLoadLocked() *pageJob {
	if s.db == nil || s.settings == nil {
		return nil
	}
	slug := copyString(s.slug)
	return s.beginSettingsJobLocked(
		false,
		func(pending *settingsview.SettingsState) {
			pending.Language = s.language
			pending.IsLoading = true
		},
		func(ctx context.Context, database *db.Database, pending *settingsview.SettingsState) {
			_ = pending.Load(ctx, database, s.paths, slug)
		},
		nil,
	)
}

// beginSettingsJobLocked prepares one Settings job. The caller holds the
// session mutex and starts the returned job after releasing it.
//
// guard is true for an action and false for a load: Rust's spawn_pending never
// runs two Settings actions on the same UI, while spawn_load may be superseded
// by a newer load (the page request drops the older result).
func (s *Session) beginSettingsJobLocked(
	guard bool,
	prepare func(*settingsview.SettingsState),
	work func(context.Context, *db.Database, *settingsview.SettingsState),
	publishExtra func(*settingsview.SettingsState),
) *pageJob {
	if s.db == nil || s.settings == nil {
		return nil
	}

	var busy accountsview.Busy
	if guard {
		if s.settingsWork.Active() {
			return nil
		}
		busy = s.settingsWork
		busy.Begin()
	}
	generation, ok := s.beginJobLocked()
	if !ok {
		if guard {
			busy.End()
		}
		return nil
	}

	request := s.nextPageRequestLocked("settings")
	pending := s.settings.Clone()
	pending.Language = s.language
	if prepare != nil {
		prepare(&pending)
	}
	// The published state is a copy: the worker mutates pending without the
	// session mutex, exactly as the other page jobs do.
	published := pending.Clone()
	s.settings = &published
	database := s.db

	return &pageJob{
		page:       "settings",
		generation: generation,
		request:    request,
		database:   database,
		load: func(ctx context.Context, open *db.Database) {
			work(ctx, open, &pending)
		},
		publish: func() {
			s.settings = &pending
			if publishExtra != nil {
				publishExtra(&pending)
			}
		},
		finish: func(bool) {
			if guard {
				busy.End()
			}
		},
		afterPublish: func(published bool) {
			if !guard || !published {
				return
			}
			// Categories, rules and the primary currency feed the other pages'
			// formatting, so they hear about a Settings change as Rust
			// refreshes them.
			for _, page := range []string{"dashboard", "transactions", "reports"} {
				s.notify(Change{Page: page})
			}
		},
	}
}

// SettingsDismissMessages clears the error and success banner.
func (s *Session) SettingsDismissMessages() {
	s.mu.Lock()
	if s.settings != nil {
		s.settings.ClearMessages()
	}
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsSetPrimaryCurrency stores the primary currency.
func (s *Session) SettingsSetPrimaryCurrency(code string) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.SetPrimaryCurrency(ctx, database, code); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsAddDefaultCategories adds the default categories that do not exist
// yet.
func (s *Session) SettingsAddDefaultCategories() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.AddDefaultCategories(ctx, database); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsFixDuplicateColours recolours categories that share a colour.
func (s *Session) SettingsFixDuplicateColours() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.FixDuplicateColours(ctx, database); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsOpenAddCategory opens the category dialog for a new category.
func (s *Session) SettingsOpenAddCategory() {
	s.mu.Lock()
	s.settingsDialog.CategoryOpen = true
	s.settingsDialog.CategoryID = -1
	s.settingsDialog.CategoryName = ""
	s.settingsDialog.CategoryColour = categories.Colors[0]
	s.settingsDialog.CategoryBudget = ""
	s.settingsDialog.CategoryTaxRelevant = false
	s.settingsDialog.CategoryTransfer = false
	s.settingsDialog.CategoryError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsOpenEditCategory opens the category dialog for an existing category.
func (s *Session) SettingsOpenEditCategory(id model.CategoryID) {
	s.mu.Lock()
	if s.settings == nil {
		s.mu.Unlock()
		return
	}
	var found *model.Category
	for index := range s.settings.Categories {
		if s.settings.Categories[index].ID == id {
			found = &s.settings.Categories[index]
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.CategoryOpen = true
	s.settingsDialog.CategoryID = int(id)
	s.settingsDialog.CategoryName = found.Name
	s.settingsDialog.CategoryColour = found.Color
	s.settingsDialog.CategoryBudget = budgetInput(found.BudgetLimit)
	s.settingsDialog.CategoryTaxRelevant = found.IsTaxRelevant
	s.settingsDialog.CategoryTransfer = found.IsTransfer
	s.settingsDialog.CategoryError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsRequestDeleteCategory opens the delete confirmation for a category.
func (s *Session) SettingsRequestDeleteCategory(id model.CategoryID) {
	s.mu.Lock()
	if s.settings == nil {
		s.mu.Unlock()
		return
	}
	var found *model.Category
	for index := range s.settings.Categories {
		if s.settings.Categories[index].ID == id {
			found = &s.settings.Categories[index]
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		return
	}
	usage := s.settings.CategoryUsage[id]
	text := i18n.Text(s.settings.Language, i18n.DeleteCategoryText)
	if usage > 0 {
		text += " " + i18n.Format(
			s.settings.Language,
			i18n.DeleteCategoryUsage,
			i18n.Int(int64(usage)),
		)
	}
	s.settingsDialog.CategoryDeleteOpen = true
	s.settingsDialog.CategoryDeleteID = int(id)
	s.settingsDialog.CategoryDeleteTitle = i18n.Format(
		s.settings.Language,
		i18n.DeleteCategoryTitle,
		i18n.Str(found.Name),
	)
	s.settingsDialog.CategoryDeleteText = text
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsValidateCategory re-runs validate_category for an explicit dialog
// id, as Rust's validate_category does.
func (s *Session) SettingsValidateCategory(name string, colour string, id int) {
	s.mu.Lock()
	s.settingsValidateCategoryLocked(name, colour, id)
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

func (s *Session) settingsValidateCategoryLocked(name string, colour string, id int) {
	if s.settings == nil {
		return
	}
	var exclude *model.CategoryID
	if id >= 0 {
		categoryID := model.CategoryID(id)
		exclude = &categoryID
	}
	if message := s.settings.ValidateCategory(name, colour, exclude); message != nil {
		s.settingsDialog.CategoryError = *message
	} else {
		s.settingsDialog.CategoryError = ""
	}
}

// SettingsCategoryChanged re-runs validate_category while the category dialog
// is edited.
func (s *Session) SettingsCategoryChanged(name string, colour string) {
	s.mu.Lock()
	s.settingsValidateCategoryLocked(name, colour, s.settingsDialog.CategoryID)
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

func (s *Session) settingsActionReadyLocked() bool {
	return s.db != nil && s.settings != nil && !s.settingsWork.Active()
}

// SettingsSaveCategory saves the category dialog's fields.
func (s *Session) SettingsSaveCategory(
	name string,
	colour string,
	budgetText string,
	taxRelevant bool,
	transfer bool,
) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}

	var exclude *model.CategoryID
	if s.settingsDialog.CategoryID >= 0 {
		id := model.CategoryID(s.settingsDialog.CategoryID)
		exclude = &id
	}
	if message := s.settings.ValidateCategory(name, colour, exclude); message != nil {
		s.settingsDialog.CategoryError = *message
		s.mu.Unlock()
		s.notify(Change{Page: "settings"})
		return
	}
	budget, key, ok := parseBudget(budgetText)
	if !ok {
		s.settingsDialog.CategoryError = i18n.Text(s.settings.Language, key)
		s.mu.Unlock()
		s.notify(Change{Page: "settings"})
		return
	}
	s.settingsDialog.CategoryError = ""

	job := s.beginSettingsJobLocked(true, func(pending *settingsview.SettingsState) {
		pending.ClearMessages()
	}, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.SaveCategory(
			ctx,
			database,
			exclude,
			name,
			colour,
			budget,
			taxRelevant,
			transfer,
		); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, func(pending *settingsview.SettingsState) {
		if pending.Error == nil {
			s.settingsDialog.CategoryOpen = false
			s.settingsDialog.CategoryError = ""
		} else {
			s.settingsDialog.CategoryError = *pending.Error
		}
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsCloseCategoryDialog closes the category dialog without changing
// anything.
func (s *Session) SettingsCloseCategoryDialog() {
	s.mu.Lock()
	s.settingsDialog.CategoryOpen = false
	s.settingsDialog.CategoryError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsDeleteCategory deletes a category and reloads the state, as Rust's
// delete_category does.
func (s *Session) SettingsDeleteCategory(id model.CategoryID) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.CategoryDeleteOpen = false
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.DeleteCategory(ctx, database, id); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, func(*settingsview.SettingsState) {
		s.settingsDialog.CategoryDeleteOpen = false
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsConfirmDeleteCategory deletes the category whose confirmation is
// open.
func (s *Session) SettingsConfirmDeleteCategory() {
	s.mu.Lock()
	id := model.CategoryID(s.settingsDialog.CategoryDeleteID)
	s.mu.Unlock()
	s.SettingsDeleteCategory(id)
}

// SettingsCancelDeleteCategory closes the delete confirmation.
func (s *Session) SettingsCancelDeleteCategory() {
	s.mu.Lock()
	s.settingsDialog.CategoryDeleteOpen = false
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsOpenAddRule opens the rule dialog for a new rule.
func (s *Session) SettingsOpenAddRule() {
	s.mu.Lock()
	if s.settings == nil || len(s.settings.Categories) == 0 {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.RuleOpen = true
	s.settingsDialog.RuleID = -1
	s.settingsDialog.RulePattern = ""
	s.settingsDialog.RuleCategoryIndex = 0
	s.settingsDialog.RulePriority = "0"
	s.settingsDialog.RuleAutoApply = true
	s.settingsDialog.RuleFieldIndex = 0
	s.settingsDialog.RuleDirectionIndex = 0
	s.settingsDialog.RuleTaxIndex = 0
	s.settingsDialog.RuleError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsOpenEditRule opens the rule dialog for an existing rule.
func (s *Session) SettingsOpenEditRule(id model.RuleID) {
	s.mu.Lock()
	if s.settings == nil {
		s.mu.Unlock()
		return
	}
	var found *model.Rule
	for index := range s.settings.Rules {
		if s.settings.Rules[index].ID == id {
			found = &s.settings.Rules[index]
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		return
	}
	categoryIndex := 0
	for index := range s.settings.Categories {
		if s.settings.Categories[index].ID == found.CategoryID {
			categoryIndex = index
			break
		}
	}
	s.settingsDialog.RuleOpen = true
	s.settingsDialog.RuleID = int(id)
	s.settingsDialog.RulePattern = found.Pattern
	s.settingsDialog.RuleCategoryIndex = categoryIndex
	s.settingsDialog.RulePriority = strconv.FormatInt(found.Priority, 10)
	s.settingsDialog.RuleAutoApply = found.AutoApply
	s.settingsDialog.RuleFieldIndex = fieldIndex(found.MatchField)
	s.settingsDialog.RuleDirectionIndex = directionIndex(found.Direction)
	s.settingsDialog.RuleTaxIndex = taxIndex(found.TaxRelevant)
	s.settingsDialog.RuleError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsRequestDeleteRule opens the delete confirmation for a rule.
func (s *Session) SettingsRequestDeleteRule(id model.RuleID) {
	s.mu.Lock()
	if s.settings == nil {
		s.mu.Unlock()
		return
	}
	var found *model.Rule
	for index := range s.settings.Rules {
		if s.settings.Rules[index].ID == id {
			found = &s.settings.Rules[index]
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.RuleDeleteOpen = true
	s.settingsDialog.RuleDeleteID = int(id)
	s.settingsDialog.RuleDeleteTitle = i18n.Format(
		s.settings.Language,
		i18n.DeleteRuleTitle,
		i18n.Str(found.Pattern),
	)
	s.settingsDialog.RuleDeleteText = i18n.Text(s.settings.Language, i18n.DeleteRuleText)
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsValidateRule re-runs validate_rule for the open dialog.
func (s *Session) SettingsValidateRule(pattern string, priority string) {
	s.mu.Lock()
	s.settingsValidateRuleLocked(pattern, priority)
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

func (s *Session) settingsValidateRuleLocked(pattern string, priority string) {
	if s.settings == nil {
		return
	}
	s.settingsDialog.RuleError = ruleError(s.settings, pattern, priority)
}

// SettingsRuleChanged re-runs validate_rule while the rule dialog is edited.
func (s *Session) SettingsRuleChanged(pattern string, priority string) {
	s.mu.Lock()
	s.settingsValidateRuleLocked(pattern, priority)
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsSaveRule saves the rule dialog's fields.
func (s *Session) SettingsSaveRule(
	pattern string,
	categoryIndex int,
	priorityText string,
	autoApply bool,
	fieldIndex int,
	directionIndex int,
	taxIndex int,
) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}

	if message := ruleError(s.settings, pattern, priorityText); message != "" {
		s.settingsDialog.RuleError = message
		s.mu.Unlock()
		s.notify(Change{Page: "settings"})
		return
	}
	priority, err := strconv.ParseInt(strings.TrimSpace(priorityText), 10, 64)
	if err != nil {
		s.settingsDialog.RuleError = i18n.Text(s.settings.Language, i18n.PriorityMustBeNumber)
		s.mu.Unlock()
		s.notify(Change{Page: "settings"})
		return
	}
	if categoryIndex < 0 || categoryIndex >= len(s.settings.Categories) {
		s.mu.Unlock()
		return
	}
	categoryID := s.settings.Categories[categoryIndex].ID
	var ruleID *model.RuleID
	if s.settingsDialog.RuleID >= 0 {
		id := model.RuleID(s.settingsDialog.RuleID)
		ruleID = &id
	}
	field := fieldAt(fieldIndex)
	direction := directionAt(directionIndex)
	taxRelevant := taxAt(taxIndex)
	s.settingsDialog.RuleError = ""

	job := s.beginSettingsJobLocked(true, func(pending *settingsview.SettingsState) {
		pending.ClearMessages()
	}, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.SaveRule(
			ctx,
			database,
			ruleID,
			pattern,
			categoryID,
			priority,
			autoApply,
			field,
			direction,
			taxRelevant,
		); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, func(pending *settingsview.SettingsState) {
		if pending.Error == nil {
			s.settingsDialog.RuleOpen = false
			s.settingsDialog.RuleError = ""
		} else {
			s.settingsDialog.RuleError = *pending.Error
		}
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsCloseRuleDialog closes the rule dialog without changing anything.
func (s *Session) SettingsCloseRuleDialog() {
	s.mu.Lock()
	s.settingsDialog.RuleOpen = false
	s.settingsDialog.RuleError = ""
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsDeleteRule deletes a rule and reloads the state, as Rust's
// delete_rule does.
func (s *Session) SettingsDeleteRule(id model.RuleID) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.RuleDeleteOpen = false
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.DeleteRule(ctx, database, id); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, func(*settingsview.SettingsState) {
		s.settingsDialog.RuleDeleteOpen = false
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsConfirmDeleteRule deletes the rule whose confirmation is open.
func (s *Session) SettingsConfirmDeleteRule() {
	s.mu.Lock()
	id := model.RuleID(s.settingsDialog.RuleDeleteID)
	s.mu.Unlock()
	s.SettingsDeleteRule(id)
}

// SettingsCancelDeleteRule closes the delete confirmation.
func (s *Session) SettingsCancelDeleteRule() {
	s.mu.Lock()
	s.settingsDialog.RuleDeleteOpen = false
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

// SettingsSaveTaxKeywords stores the tax suggestion keywords.
func (s *Session) SettingsSaveTaxKeywords(text string) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.SaveTaxKeywords(ctx, database, text); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsResetTaxKeywords removes the custom keywords so the defaults apply
// again.
func (s *Session) SettingsResetTaxKeywords() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := pending.ResetTaxKeywords(ctx, database); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsExportRules asks for a file and exports the rules into it. The path
// comes only from the native dialog, inside the job and without the session
// mutex.
func (s *Session) SettingsExportRules() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	dialogs := s.fileDialogs
	language := s.language
	today := s.today()
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if dialogs == nil {
			return
		}
		defaultName := fmt.Sprintf(
			"finglass-rules-%04d-%02d-%02d.tsv",
			today.Year(),
			int(today.Month()),
			today.Day(),
		)
		path, ok := dialogs.SaveRules(i18n.Text(language, i18n.ExportRules), defaultName)
		if !ok || strings.TrimSpace(path) == "" {
			return
		}
		if err := pending.ExportRules(ctx, database, path); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, nil)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsImportRules asks for a rule file and prepares its preview.
func (s *Session) SettingsImportRules() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	dialogs := s.fileDialogs
	language := s.language
	job := s.beginSettingsJobLocked(true, nil, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if dialogs == nil {
			return
		}
		path, ok := dialogs.PickRuleFile(i18n.Text(language, i18n.ImportRules))
		if !ok || strings.TrimSpace(path) == "" {
			return
		}
		pending.ClearMessages()
		if err := pending.ChooseRuleImport(ctx, database, path); err != nil {
			pending.Error = settingsUnexpected(pending.Language)
		}
	}, func(pending *settingsview.SettingsState) {
		if pending.RuleImport == nil {
			s.settingsDialog.RuleImportOpen = false
			return
		}
		if !s.settingsDialog.RuleImportOpen {
			s.settingsDialog.RuleImportCategorize = true
		}
		s.settingsDialog.RuleImportOpen = true
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsConfirmRuleImport applies the confirmed rule-import preview.
func (s *Session) SettingsConfirmRuleImport(categorize bool) {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	s.settingsDialog.RuleImportCategorize = categorize
	job := s.beginSettingsJobLocked(
		true,
		func(pending *settingsview.SettingsState) {
			pending.IsImportingRules = true
		},
		func(ctx context.Context, database *db.Database, pending *settingsview.SettingsState) {
			if err := pending.ConfirmRuleImport(ctx, database, categorize); err != nil {
				pending.Error = settingsUnexpected(pending.Language)
			}
		},
		func(pending *settingsview.SettingsState) {
			if pending.RuleImport == nil {
				s.settingsDialog.RuleImportOpen = false
			}
		},
	)
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsCancelRuleImport drops the preview without applying anything.
func (s *Session) SettingsCancelRuleImport() {
	s.mu.Lock()
	if s.settings != nil {
		s.settings.CancelRuleImport()
	}
	s.settingsDialog.RuleImportOpen = false
	s.mu.Unlock()
	s.notify(Change{Page: "settings"})
}

func settingsUnexpected(language i18n.Language) *string {
	message := i18n.Text(language, i18n.ErrorUnexpected)
	return &message
}

// budgetInput formats a budget for the category dialog, using the currency's
// exponent exactly as Rust's budget_input does.
func budgetInput(budget *money.Money) string {
	if budget == nil {
		return ""
	}
	exponent := budget.Currency.MinorExponent()
	if exponent == 0 {
		return strconv.FormatInt(budget.Minor, 10)
	}
	scale := int64(1)
	for index := uint32(0); index < exponent; index++ {
		scale *= 10
	}
	whole := budget.Minor / scale
	fraction := budget.Minor % scale
	if fraction < 0 {
		fraction = -fraction
	}
	return fmt.Sprintf("%d.%0*d", whole, int(exponent), fraction)
}

// parseBudget parses the category dialog's budget field exactly as Rust's
// parse_budget does: empty is no budget, surrounding euro signs are ignored,
// and the core parser applies EUR's exponent.
func parseBudget(text string) (*money.Money, i18n.Key, bool) {
	if strings.TrimSpace(text) == "" {
		return nil, 0, true
	}
	trimmed := strings.Trim(strings.TrimSpace(text), "€")
	trimmed = strings.TrimSpace(trimmed)
	parsed, err := money.ParseDecimal(trimmed, money.EUR)
	if err != nil {
		return nil, i18n.BudgetNotANumber, false
	}
	return &parsed, 0, true
}

// ruleError returns the localized rule error, or "" when the fields
// are valid.
func ruleError(state *settingsview.SettingsState, pattern string, priority string) string {
	if message := state.ValidateRule(pattern); message != nil {
		return *message
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(priority), 10, 64); err != nil {
		return i18n.Text(state.Language, i18n.PriorityMustBeNumber)
	}
	return ""
}

// fieldIndex is Rust's field_index.
func fieldIndex(field model.RuleField) int {
	switch field {
	case model.RuleFieldPartner:
		return 1
	case model.RuleFieldPurpose:
		return 2
	default:
		return 0
	}
}

// directionIndex is Rust's direction_index.
func directionIndex(direction model.RuleDirection) int {
	switch direction {
	case model.RuleDirectionExpense:
		return 1
	case model.RuleDirectionIncome:
		return 2
	default:
		return 0
	}
}

// taxIndex is Rust's tax_index.
func taxIndex(tax *bool) int {
	if tax == nil {
		return 0
	}
	if *tax {
		return 1
	}
	return 2
}

// fieldAt is Rust's field_at.
func fieldAt(index int) model.RuleField {
	switch index {
	case 1:
		return model.RuleFieldPartner
	case 2:
		return model.RuleFieldPurpose
	default:
		return model.RuleFieldAny
	}
}

// directionAt is Rust's direction_at.
func directionAt(index int) model.RuleDirection {
	switch index {
	case 1:
		return model.RuleDirectionExpense
	case 2:
		return model.RuleDirectionIncome
	default:
		return model.RuleDirectionAny
	}
}

// taxAt is Rust's tax_at.
func taxAt(index int) *bool {
	switch index {
	case 1:
		value := true
		return &value
	case 2:
		value := false
		return &value
	default:
		return nil
	}
}
