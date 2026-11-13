package settingsview

import (
	"github.com/MaMissaoui/Finglass/internal/app/auditview"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/ruleimport"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

// Clone returns a deep copy of the state.
//
// The shell runs every job on a clone so the worker never mutates published
// state without the session mutex. Slices, maps and every pointer are copied,
// so changing the clone can never change the original.
func (s *SettingsState) Clone() SettingsState {
	if s == nil {
		return *New(i18n.En)
	}

	cloned := *s
	cloned.Categories = make([]model.Category, len(s.Categories))
	for index, category := range s.Categories {
		cloned.Categories[index] = cloneCategory(category)
	}

	cloned.CategoryUsage = make(map[model.CategoryID]int, len(s.CategoryUsage))
	for id, usage := range s.CategoryUsage {
		cloned.CategoryUsage[id] = usage
	}

	cloned.Rules = make([]model.Rule, len(s.Rules))
	for index, rule := range s.Rules {
		cloned.Rules[index] = cloneRule(rule)
	}

	cloned.AuditLog = append([]auditview.AuditRow(nil), s.AuditLog...)
	cloned.CurrencyOptions = append([]money.Currency(nil), s.CurrencyOptions...)
	cloned.Profiles = append([]profiles.Profile(nil), s.Profiles...)
	cloned.OpenProfile = copyString(s.OpenProfile)
	cloned.NextProfile = copyString(s.NextProfile)
	cloned.RuleImport = cloneRuleImport(s.RuleImport)
	cloned.RestoreCandidate = cloneRestoreCandidate(s.RestoreCandidate)
	cloned.RestorePassphraseFor = copyString(s.RestorePassphraseFor)
	cloned.Error = copyString(s.Error)
	cloned.Message = copyString(s.Message)
	return cloned
}

func cloneCategory(category model.Category) model.Category {
	cloned := category
	if category.BudgetLimit != nil {
		budget := *category.BudgetLimit
		cloned.BudgetLimit = &budget
	}
	return cloned
}

func cloneRule(rule model.Rule) model.Rule {
	cloned := rule
	if rule.TaxRelevant != nil {
		taxRelevant := *rule.TaxRelevant
		cloned.TaxRelevant = &taxRelevant
	}
	return cloned
}

func cloneRuleImport(preview *ruleimport.RuleImportPreview) *ruleimport.RuleImportPreview {
	if preview == nil {
		return nil
	}
	cloned := *preview
	cloned.CategoriesToCreate = append([]string(nil), preview.CategoriesToCreate...)
	cloned.NewRules = append([]rules.RuleFileRow(nil), preview.NewRules...)
	cloned.Conflicts = append([]ruleimport.RuleImportConflict(nil), preview.Conflicts...)
	cloned.Errors = append([]rules.RuleFileError(nil), preview.Errors...)
	return &cloned
}

func cloneRestoreCandidate(candidate *RestoreCandidate) *RestoreCandidate {
	if candidate == nil {
		return nil
	}
	cloned := *candidate
	return &cloned
}
