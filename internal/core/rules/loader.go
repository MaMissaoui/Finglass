package rules

import (
	"context"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
)

// Matcher loads the auto-apply rules, compiled once, in priority order.
func Matcher(ctx context.Context, d *db.Database) (*RuleMatcher, error) {
	stored, err := rulesrepo.List(ctx, d)
	if err != nil {
		return nil, err
	}

	specs := make([]RuleSpec, 0, len(stored))
	for _, rule := range stored {
		if !rule.AutoApply {
			continue
		}
		specs = append(specs, RuleSpec{
			Pattern:    rule.Pattern,
			CategoryID: rule.CategoryID,
			Field:      rule.MatchField,
			Direction:  rule.Direction,
		})
	}
	return NewRuleMatcher(specs), nil
}
