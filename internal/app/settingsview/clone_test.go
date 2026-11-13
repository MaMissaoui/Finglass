package settingsview

import (
	"testing"

	"github.com/MaMissaoui/Finglass/internal/app/auditview"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/profiles"
	"github.com/MaMissaoui/Finglass/internal/core/ruleimport"
	"github.com/MaMissaoui/Finglass/internal/core/rules"
)

func TestCloneCopiesEverySliceMapAndPointer(t *testing.T) {
	budget := money.Money{Minor: 30_000, Currency: money.EUR}
	tax := true
	open := "work"
	next := "default"
	message := "saved"
	errorText := "failed"

	state := New(i18n.En)
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	state.Categories = []model.Category{{
		ID:            1,
		Name:          "Food",
		Color:         "#FF5722",
		BudgetLimit:   &budget,
		IsTaxRelevant: true,
	}}
	state.CategoryUsage = map[model.CategoryID]int{1: 5}
	state.Rules = []model.Rule{{
		ID:          2,
		Pattern:     "rewe",
		CategoryID:  1,
		TaxRelevant: &tax,
	}}
	state.AuditLog = []auditview.AuditRow{{Action: "CREATE_CATEGORY"}}
	state.CurrencyOptions = []money.Currency{money.EUR, usd}
	state.Profiles = []profiles.Profile{{Slug: "work", Name: "Work"}}
	state.OpenProfile = &open
	state.NextProfile = &next
	state.RuleImport = &ruleimport.RuleImportPreview{
		CategoriesToCreate: []string{"Groceries"},
		NewRules:           []rules.RuleFileRow{{Line: 3, Category: "Groceries"}},
		Conflicts:          []ruleimport.RuleImportConflict{{Line: 4, Pattern: "rewe"}},
		Errors:             []rules.RuleFileError{{Line: 5, Reason: "bad"}},
	}
	state.RestoreCandidate = &RestoreCandidate{Backup: "/tmp/backup.zip"}
	state.RestorePassphraseFor = &open
	state.Message = &message
	state.Error = &errorText

	cloned := state.Clone()

	cloned.Categories[0].Name = "Changed"
	*cloned.Categories[0].BudgetLimit = money.Money{Minor: 1, Currency: money.EUR}
	cloned.CategoryUsage[1] = 9
	cloned.Rules[0].Pattern = "changed"
	*cloned.Rules[0].TaxRelevant = false
	cloned.AuditLog[0].Action = "changed"
	cloned.CurrencyOptions[0] = usd
	cloned.Profiles[0].Name = "Changed"
	*cloned.OpenProfile = "changed"
	*cloned.NextProfile = "changed"
	cloned.RuleImport.CategoriesToCreate[0] = "Changed"
	cloned.RuleImport.NewRules[0].Category = "Changed"
	cloned.RuleImport.Conflicts[0].Pattern = "changed"
	cloned.RuleImport.Errors[0].Reason = "changed"
	cloned.RestoreCandidate.Backup = "changed"
	*cloned.RestorePassphraseFor = "changed"
	*cloned.Message = "changed"
	*cloned.Error = "changed"

	if state.Categories[0].Name != "Food" || state.Categories[0].BudgetLimit.Minor != 30_000 {
		t.Fatal("the category was not copied")
	}
	if state.CategoryUsage[1] != 5 {
		t.Fatal("the usage map was not copied")
	}
	if state.Rules[0].Pattern != "rewe" || !*state.Rules[0].TaxRelevant {
		t.Fatal("the rule was not copied")
	}
	if state.AuditLog[0].Action != "CREATE_CATEGORY" {
		t.Fatal("the audit log was not copied")
	}
	if state.CurrencyOptions[0] != money.EUR || state.Profiles[0].Name != "Work" {
		t.Fatal("the slices were not copied")
	}
	if *state.OpenProfile != "work" || *state.NextProfile != "default" {
		t.Fatal("the profile slugs were not copied")
	}
	if state.RuleImport.CategoriesToCreate[0] != "Groceries" ||
		state.RuleImport.NewRules[0].Category != "Groceries" ||
		state.RuleImport.Conflicts[0].Pattern != "rewe" ||
		state.RuleImport.Errors[0].Reason != "bad" {
		t.Fatal("the rule-import preview was not copied")
	}
	if state.RestoreCandidate.Backup != "/tmp/backup.zip" || *state.RestorePassphraseFor != "work" {
		t.Fatal("the restore state was not copied")
	}
	if *state.Message != "saved" || *state.Error != "failed" {
		t.Fatal("the messages were not copied")
	}
}
