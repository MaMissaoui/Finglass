package rules

import (
	"strings"

	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

// RuleSpec is one auto-apply rule compiled for [RuleMatcher].
type RuleSpec struct {
	// Pattern is the text pattern.
	Pattern string
	// CategoryID is the category to assign when the rule matches.
	CategoryID model.CategoryID
	// Field is the booking field the pattern is matched against.
	Field model.RuleField
	// Direction is the booking direction the rule applies to.
	Direction model.RuleDirection
}

// NewRuleSpec returns a rule unrestricted in field and direction.
func NewRuleSpec(pattern string, categoryID model.CategoryID) RuleSpec {
	return RuleSpec{
		Pattern:    pattern,
		CategoryID: categoryID,
		Field:      model.RuleFieldAny,
		Direction:  model.RuleDirectionAny,
	}
}

// RulePair is a pattern and the category it assigns, used by [Of].
type RulePair struct {
	// Pattern is the text pattern.
	Pattern string
	// CategoryID is the category to assign when the rule matches.
	CategoryID model.CategoryID
}

// RuleMatcher holds auto-apply rules compiled once, in priority order. The
// first rule matching the original booking (in its text scope and direction)
// wins, with one exception: for a PayPal payment whose purpose names the shop,
// a provider rule — one that also matches the partner alone — gives way to the
// first shop rule whose direction is the provider rule's direction (or any
// direction when the provider rule is ANY). Any other rule, such as a
// high-priority purpose rule for a refund, keeps winning.
type RuleMatcher struct {
	// Specs are the rules in priority order.
	Specs    []RuleSpec
	compiled []*Pattern
}

// NewRuleMatcher compiles the patterns once.
func NewRuleMatcher(specs []RuleSpec) *RuleMatcher {
	compiled := make([]*Pattern, len(specs))
	for index, spec := range specs {
		compiled[index] = Compile(spec.Pattern)
	}
	return &RuleMatcher{Specs: specs, compiled: compiled}
}

// Of returns a matcher from pattern/category pairs, all rules unrestricted
// (ANY/ANY).
func Of(rules []RulePair) *RuleMatcher {
	specs := make([]RuleSpec, 0, len(rules))
	for _, rule := range rules {
		specs = append(specs, NewRuleSpec(rule.Pattern, rule.CategoryID))
	}
	return NewRuleMatcher(specs)
}

// Matches reports whether spec's direction and text scope accept a booking,
// tested with test.
func Matches(
	spec RuleSpec,
	test *Pattern,
	purpose *string,
	partnerName *string,
	amount *decimal.Decimal,
) bool {
	directionOK := false
	switch spec.Direction {
	case model.RuleDirectionAny:
		directionOK = true
	case model.RuleDirectionExpense:
		directionOK = amount != nil && amount.Sign() < 0
	case model.RuleDirectionIncome:
		directionOK = amount != nil && amount.Sign() > 0
	}
	if !directionOK {
		return false
	}

	var text string
	switch spec.Field {
	case model.RuleFieldAny:
		switch {
		case purpose != nil && partnerName != nil:
			text = *purpose + " " + *partnerName
		case purpose != nil:
			text = *purpose
		case partnerName != nil:
			text = *partnerName
		default:
			text = ""
		}
	case model.RuleFieldPartner:
		if partnerName != nil {
			text = *partnerName
		}
	case model.RuleFieldPurpose:
		if purpose != nil {
			text = *purpose
		}
	}

	return strings.TrimSpace(text) != "" && test.IsMatch(text)
}

// Select returns the index of the rule that applies to a booking among specs
// in priority order, with tests compiled from their patterns in the same order
// and the shop shop already extracted once by the caller.
func Select(
	specs []RuleSpec,
	tests []*Pattern,
	purpose *string,
	partnerName *string,
	amount *decimal.Decimal,
	shop *ShopMatch,
) *int {
	original := firstMatching(specs, tests, purpose, partnerName, amount)
	if shop == nil {
		return original
	}
	if original == nil {
		return firstMatching(specs, tests, nil, &shop.Shop, amount)
	}

	winner := specs[*original]
	if !Matches(winner, tests[*original], shop.ProviderPurpose, shop.ProviderPartner, amount) {
		return original
	}

	for index, candidate := range specs {
		if winner.Direction != model.RuleDirectionAny && candidate.Direction != winner.Direction {
			continue
		}
		if Matches(candidate, tests[index], nil, &shop.Shop, amount) {
			selected := index
			return &selected
		}
	}
	return original
}

// firstMatching returns the index of the first spec that matches, or nil.
func firstMatching(
	specs []RuleSpec,
	tests []*Pattern,
	purpose *string,
	partnerName *string,
	amount *decimal.Decimal,
) *int {
	for index, spec := range specs {
		if Matches(spec, tests[index], purpose, partnerName, amount) {
			selected := index
			return &selected
		}
	}
	return nil
}

// CategoryFor returns the category id of the rule [Select] picks, or nil.
func (m *RuleMatcher) CategoryFor(
	purpose *string,
	partnerName *string,
	amount *decimal.Decimal,
) *model.CategoryID {
	shopMatch := ShopMatchOf(purpose, partnerName)
	selected := Select(m.Specs, m.compiled, purpose, partnerName, amount, shopMatch)
	if selected == nil {
		return nil
	}
	category := m.Specs[*selected].CategoryID
	return &category
}
