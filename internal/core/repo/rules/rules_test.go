package rules

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/repo"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

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

func newCategory(t *testing.T, d *db.Database, name string) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:  name,
		Color: "#3BC7B0",
	})
	if err != nil {
		t.Fatalf("categories.Create(%q): %v", name, err)
	}
	return id
}

func newRule(pattern string, categoryID model.CategoryID) model.NewRule {
	return model.NewRule{
		Pattern:    pattern,
		CategoryID: categoryID,
		Priority:   0,
		AutoApply:  true,
		MatchField: model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}
}

func entries(t *testing.T, d *db.Database) []audit.Entry {
	t.Helper()

	recent, err := audit.Recent(context.Background(), d, 1000)
	if err != nil {
		t.Fatalf("audit.Recent: %v", err)
	}
	return recent
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestCategoriesAndRulesCRUDAndOrdering(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	eur := money.Money{Minor: 50_000, Currency: money.EUR}

	zebra, err := categories.Create(ctx, d, model.NewCategory{Name: "zebra", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("Create zebra: %v", err)
	}
	alpha, err := categories.Create(ctx, d, model.NewCategory{Name: "Alpha", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("Create Alpha: %v", err)
	}
	beta, err := categories.Create(ctx, d, model.NewCategory{Name: "beta", Color: "#3BC7B0"})
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}

	listedCategories, err := categories.List(ctx, d)
	if err != nil {
		t.Fatalf("categories.List: %v", err)
	}
	names := make([]string, 0, len(listedCategories))
	for _, category := range listedCategories {
		names = append(names, category.Name)
	}
	if !slices.Equal(names, []string{"Alpha", "beta", "zebra"}) {
		t.Errorf("category names = %v", names)
	}

	updated := model.NewCategory{Name: "Renamed", Color: "#000000", BudgetLimit: &eur, IsTaxRelevant: true}
	if err := categories.Update(ctx, d, zebra, updated); err != nil {
		t.Fatalf("categories.Update: %v", err)
	}
	loaded, ok, err := categories.Get(ctx, d, zebra)
	if err != nil || !ok {
		t.Fatalf("categories.Get: %v, %v", ok, err)
	}
	if loaded.Name != "Renamed" || loaded.Color != "#000000" || loaded.BudgetLimit == nil ||
		*loaded.BudgetLimit != eur || !loaded.IsTaxRelevant {
		t.Errorf("updated category = %+v", loaded)
	}

	lowNew := newRule("low", alpha)
	lowNew.Priority = 1
	lowNew.AutoApply = false
	lowNew.MatchField = model.RuleFieldAny
	lowNew.Direction = model.RuleDirectionAny
	ruleLow, err := Create(ctx, d, lowNew)
	if err != nil {
		t.Fatalf("Create low: %v", err)
	}
	highNew := newRule("high", beta)
	highNew.Priority = 5
	highNew.AutoApply = true
	highNew.MatchField = model.RuleFieldPurpose
	highNew.Direction = model.RuleDirectionIncome
	no := false
	highNew.TaxRelevant = &no
	ruleHigh, err := Create(ctx, d, highNew)
	if err != nil {
		t.Fatalf("Create high: %v", err)
	}

	listed, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ordered := make([]model.RuleID, 0, len(listed))
	for _, rule := range listed {
		ordered = append(ordered, rule.ID)
	}
	if !slices.Equal(ordered, []model.RuleID{ruleHigh, ruleLow}) {
		t.Errorf("rule order = %v", ordered)
	}

	var updatedRule *model.Rule
	for index := range listed {
		if listed[index].ID == ruleLow {
			updatedRule = &listed[index]
			break
		}
	}
	if updatedRule == nil {
		t.Fatal("the low rule is missing")
	}
	updatedRule.Priority = 10
	updatedRule.Pattern = "changed"
	updatedRule.MatchField = model.RuleFieldPartner
	updatedRule.Direction = model.RuleDirectionExpense
	yes := true
	updatedRule.TaxRelevant = &yes
	updatedRule.AutoApply = false
	update := model.NewRule{
		Pattern:     updatedRule.Pattern,
		CategoryID:  updatedRule.CategoryID,
		Priority:    updatedRule.Priority,
		AutoApply:   updatedRule.AutoApply,
		MatchField:  updatedRule.MatchField,
		Direction:   updatedRule.Direction,
		TaxRelevant: updatedRule.TaxRelevant,
	}
	if err := Update(ctx, d, ruleLow, update); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List after Update: %v", err)
	}
	if after[0].ID != ruleLow || after[0].Pattern != "changed" ||
		after[0].MatchField != model.RuleFieldPartner ||
		after[0].Direction != model.RuleDirectionExpense ||
		after[0].TaxRelevant == nil || !*after[0].TaxRelevant {
		t.Errorf("updated rule = %+v", after[0])
	}

	if err := Delete(ctx, d, ruleHigh); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	remaining, err := List(ctx, d)
	if err != nil || len(remaining) != 1 {
		t.Errorf("rules after Delete = %d, %v", len(remaining), err)
	}
}

func TestCreateListUpdateDeleteRoundTripAndAudit(t *testing.T) {
	d := database(t)
	ctx := context.Background()
	category := newCategory(t, d, "Food")
	before := len(entries(t, d))

	ruleNew := newRule("market", category)
	ruleNew.Priority = 3
	taxRelevant := true
	ruleNew.TaxRelevant = &taxRelevant
	id, err := Create(ctx, d, ruleNew)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	listed, err := List(ctx, d)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List length = %d", len(listed))
	}
	rule := listed[0]
	if rule.ID != id || rule.Pattern != "market" || rule.CategoryID != category || rule.Priority != 3 ||
		!rule.AutoApply || rule.MatchField != model.RuleFieldAny ||
		rule.Direction != model.RuleDirectionAny || rule.TaxRelevant == nil || !*rule.TaxRelevant {
		t.Errorf("rule = %+v", rule)
	}

	after := entries(t, d)
	if len(after) != before+1 {
		t.Fatalf("audit entries = %d, want %d", len(after), before+1)
	}
	if after[0].Action != "rule.create" || after[0].Actor != audit.ActorUser ||
		after[0].Details != "id="+itoa(int64(id)) {
		t.Errorf("create audit = %+v", after[0])
	}

	update := ruleNew
	update.Pattern = "renamed"
	if err := Update(ctx, d, id, update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after = entries(t, d)
	if after[0].Action != "rule.update" || after[0].Actor != audit.ActorUser ||
		after[0].Details != "id="+itoa(int64(id)) {
		t.Errorf("update audit = %+v", after[0])
	}

	if err := Delete(ctx, d, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	after = entries(t, d)
	if after[0].Action != "rule.delete" || after[0].Actor != audit.ActorUser ||
		after[0].Details != "id="+itoa(int64(id)) {
		t.Errorf("delete audit = %+v", after[0])
	}

	if err := Delete(ctx, d, id); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if err := Update(ctx, d, model.RuleID(999999), update); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("Update of an unknown rule = %v, want ErrNotFound", err)
	}
}

func TestRulesWithUnknownStoredTextAreInvalid(t *testing.T) {
	d := database(t)
	ctx := context.Background()

	var invalid repo.InvalidError
	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		row := conn.QueryRowContext(
			ctx,
			"SELECT 1, 'pattern', 1, 0, 1, 'bogus', 'any', NULL",
		)
		_, err := fromRow(row)
		return err
	})
	if !errors.As(err, &invalid) {
		t.Errorf("fromRow with an unknown match field = %v", err)
	}

	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		row := conn.QueryRowContext(
			ctx,
			"SELECT 1, 'pattern', 1, 0, 1, 'any', 'bogus', NULL",
		)
		_, err := fromRow(row)
		return err
	})
	if !errors.As(err, &invalid) {
		t.Errorf("fromRow with an unknown direction = %v", err)
	}
}
