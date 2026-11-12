package rules

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/repo/categories"
	rulesrepo "github.com/MaMissaoui/Finglass/internal/core/repo/rules"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func expense() *decimal.Decimal {
	value, err := decimal.Parse("-10.00")
	if err != nil {
		panic(err)
	}
	return &value
}

func income() *decimal.Decimal {
	value, err := decimal.Parse("10.00")
	if err != nil {
		panic(err)
	}
	return &value
}

func oneRule(pattern string, field model.RuleField, direction model.RuleDirection, categoryID int64) RuleSpec {
	return RuleSpec{
		Pattern:    pattern,
		CategoryID: model.CategoryID(categoryID),
		Field:      field,
		Direction:  direction,
	}
}

func anyRule(pattern string, categoryID int64) RuleSpec {
	return oneRule(pattern, model.RuleFieldAny, model.RuleDirectionAny, categoryID)
}

func assertCategory(t *testing.T, got *model.CategoryID, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("category = nil, want %d", want)
	}
	if *got != model.CategoryID(want) {
		t.Fatalf("category = %d, want %d", *got, want)
	}
}

func assertNoCategory(t *testing.T, got *model.CategoryID) {
	t.Helper()
	if got != nil {
		t.Fatalf("category = %d, want nil", *got)
	}
}

func TestPartnerRuleMatchesThePartnerNameButNotAPurposeThatOnlyMentionsIt(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Erika Mustermann", model.RuleFieldPartner, model.RuleDirectionAny, 1),
	})

	assertCategory(t, matcher.CategoryFor(nil, stringPointer("Erika Mustermann"), expense()), 1)
	assertNoCategory(t, matcher.CategoryFor(
		stringPointer("Gehalt Erika Mustermann"),
		stringPointer("BEISPIEL GMBH"),
		nil,
	))
	assertNoCategory(t, matcher.CategoryFor(nil, nil, nil))
}

func TestPurposeRuleIgnoresThePartnerName(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Gehalt", model.RuleFieldPurpose, model.RuleDirectionAny, 1),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer("Gehalt Oktober"),
		stringPointer("BEISPIEL GMBH"),
		income(),
	), 1)
	assertNoCategory(t, matcher.CategoryFor(
		stringPointer("Kartenzahlung"),
		stringPointer("Gehaltshaus"),
		expense(),
	))
	assertNoCategory(t, matcher.CategoryFor(nil, stringPointer("Gehalt"), income()))
}

func TestAnyRuleSearchesPurposeAndPartnerTogetherAsBefore(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{anyRule("REWE", 1)})

	assertCategory(t, matcher.CategoryFor(stringPointer("REWE SAGT DANKE"), nil, nil), 1)
	assertCategory(t, matcher.CategoryFor(nil, stringPointer("REWE Markt GmbH"), nil), 1)
	assertCategory(t, matcher.CategoryFor(stringPointer("rewe"), stringPointer("Edeka"), nil), 1)
}

func TestBlankTextDoesNotMatch(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("REWE", model.RuleFieldPurpose, model.RuleDirectionAny, 1),
	})

	assertNoCategory(t, matcher.CategoryFor(nil, stringPointer("REWE Markt GmbH"), nil))
}

func TestExpenseRuleMatchesNegativeAmountsOnly(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Beispiel", model.RuleFieldAny, model.RuleDirectionExpense, 1),
	})

	assertCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, expense()), 1)
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, income()))
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, nil))
}

func TestIncomeRuleMatchesPositiveAmountsOnly(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Beispiel", model.RuleFieldAny, model.RuleDirectionIncome, 1),
	})

	assertCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, income()), 1)
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, expense()))
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Beispiel"), nil, nil))
}

func TestFirstRuleInOrderWinsWhenAnEarlierRuleIsExcludedByDirection(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("beispielpay", model.RuleFieldAny, model.RuleDirectionIncome, 2),
		oneRule("beispielpay", model.RuleFieldAny, model.RuleDirectionAny, 3),
	})

	assertCategory(t, matcher.CategoryFor(nil, stringPointer("BeispielPay"), expense()), 3)
	assertCategory(t, matcher.CategoryFor(nil, stringPointer("BeispielPay"), income()), 2)
}

func TestOfCreatesUnrestrictedRulesAsBefore(t *testing.T) {
	matcher := Of([]RulePair{{Pattern: "erika", CategoryID: model.CategoryID(7)}})

	assertCategory(t, matcher.CategoryFor(stringPointer("Überweisung an Erika"), nil, nil), 7)
}

func TestAPatternWithAnUmlautMatchesCaseInsensitively(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Bäckerei", model.RuleFieldPartner, model.RuleDirectionAny, 1),
	})

	assertCategory(t, matcher.CategoryFor(nil, stringPointer("BÄCKEREI SCHMIDT"), nil), 1)
}

func TestAPatternWithALookaheadMatchesLikeJava(t *testing.T) {
	// Java-style negative lookahead: an Amazon booking that is not a refund.
	matcher := NewRuleMatcher([]RuleSpec{anyRule("^(?!.*refund).*amazon", 1)})

	assertCategory(t, matcher.CategoryFor(stringPointer("AMAZON EU order"), nil, expense()), 1)
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Amazon refund"), nil, income()))
}

func TestAnInvalidPatternFallsBackToACaseInsensitiveSubstring(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{anyRule("(", 1)})

	assertCategory(t, matcher.CategoryFor(stringPointer("Amount (EUR)"), nil, nil), 1)
	assertNoCategory(t, matcher.CategoryFor(stringPointer("Amount EUR"), nil, nil))
}

// PayPal shop matching: a shop rule displaces only the provider rule, never a
// rule that matches the original.

func matcherPaypalPurpose(shop string) string {
	return fmt.Sprintf("EREF+R1 SVWZ+PP.471.PPX . %s, Ihr Einkauf bei %s", shop, shop)
}

func TestAPartnerRuleForTheShopBeatsThePaypalRuleWhateverTheRuleOrder(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionAny, 2),
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionAny, 1),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	), 1)
	assertCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		income(),
	), 1)
}

func TestWithNoShopRuleThePaypalRuleAppliesThroughTheOriginalView(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionAny, 2),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	), 2)
}

func TestAPurposeOnlyRuleKeepsWinningForAPaypalBooking(t *testing.T) {
	purposeOnly := NewRuleMatcher([]RuleSpec{
		oneRule("Ihr Einkauf", model.RuleFieldPurpose, model.RuleDirectionAny, 3),
	})
	assertCategory(t, purposeOnly.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	), 3)

	withShop := NewRuleMatcher([]RuleSpec{
		oneRule("Ihr Einkauf", model.RuleFieldPurpose, model.RuleDirectionAny, 3),
		oneRule("Beispiel", model.RuleFieldPartner, model.RuleDirectionAny, 1),
	})
	assertCategory(t, withShop.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	), 3)
}

func TestAnIncomeOnlyRuleDoesNotMatchAnExpenseViaTheShopView(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Beispiel", model.RuleFieldPartner, model.RuleDirectionIncome, 4),
	})

	assertNoCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	))
	assertCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		income(),
	), 4)
}

func TestAShopRuleAppliesWhenNoOtherRuleMatches(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		expense(),
	), 20)
}

func TestAShopRuleBeatsThePaypalRuleForACardBookingWithAPaypalPartner(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionAny, 10),
		oneRule("BEISPIELSHOP", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer("PAYPAL *BEISPIELSHOP"),
		stringPointer("PAYPAL *BEISPIELSHOP"),
		expense(),
	), 20)
}

func TestANonPaypalBookingIsMatchedExactlyAsBefore(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Beispiel", model.RuleFieldPartner, model.RuleDirectionAny, 5),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer("Kauf bei Beispiel"),
		stringPointer("Beispiel Shop GmbH"),
		expense(),
	), 5)

	assertNoCategory(t, matcher.CategoryFor(
		stringPointer("Ihr Einkauf bei Beispiel"),
		nil,
		expense(),
	))
}

func TestARefundPurposeRuleKeepsWinningOverAShopRule(t *testing.T) {
	refundPurpose := "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Rückerstattung, " +
		"Ihr Einkauf bei Beispiel Shop GmbH"
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("Rückerstattung|Storno", model.RuleFieldPurpose, model.RuleDirectionAny, 10),
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer(refundPurpose),
		stringPointer(testPaypalPartner),
		income(),
	), 10)
}

func TestAShopRuleOnlyDisplacesAProviderRuleOfTheSameOrAnyDirection(t *testing.T) {
	anyDirectionShop := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionIncome, 10),
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})
	assertCategory(t, anyDirectionShop.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		income(),
	), 10)

	incomeShop := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionIncome, 10),
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionIncome, 20),
	})
	assertCategory(t, incomeShop.CategoryFor(
		stringPointer(matcherPaypalPurpose("Beispiel Shop GmbH")),
		stringPointer(testPaypalPartner),
		income(),
	), 20)
}

func TestANonPaypalBookingNeverGetsAShopView(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldPartner, model.RuleDirectionAny, 10),
		oneRule("Beispiel Shop", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})

	assertCategory(t, matcher.CategoryFor(
		stringPointer("Kauf bei Beispiel Shop"),
		stringPointer("Beispiel Shop GmbH"),
		expense(),
	), 20)
	assertNoCategory(t, matcher.CategoryFor(
		stringPointer("Ihr Einkauf bei Beispiel Shop"),
		nil,
		expense(),
	))
}

func TestCardFormatRulesBehaveLikeTheMarkerFormat(t *testing.T) {
	matcher := NewRuleMatcher([]RuleSpec{
		oneRule("PayPal", model.RuleFieldAny, model.RuleDirectionAny, 10),
		oneRule("BEISPIELSHOP", model.RuleFieldAny, model.RuleDirectionAny, 20),
	})

	assertCategory(t, matcher.CategoryFor(stringPointer("PAYPAL *BEISPIELSHOP"), nil, expense()), 20)

	purposeOnly := NewRuleMatcher([]RuleSpec{
		oneRule("RETURN", model.RuleFieldPurpose, model.RuleDirectionAny, 10),
		oneRule("BEISPIELSHOP", model.RuleFieldPartner, model.RuleDirectionAny, 20),
	})
	assertCategory(t, purposeOnly.CategoryFor(
		stringPointer("PAYPAL *BEISPIELSHOP 12345678 RETURN"),
		stringPointer("PAYPAL *BEISPIELSHOP"),
		income(),
	), 10)
}

func testDatabase(t *testing.T) *db.Database {
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

func testCategory(t *testing.T, d *db.Database) model.CategoryID {
	t.Helper()

	id, err := categories.Create(context.Background(), d, model.NewCategory{
		Name:  "Food",
		Color: "#3BC7B0",
	})
	if err != nil {
		t.Fatalf("categories.Create: %v", err)
	}
	return id
}

func TestTheMatcherIsBuiltFromAutoApplyRulesInPriorityOrder(t *testing.T) {
	d := testDatabase(t)
	ctx := context.Background()
	category := testCategory(t, d)

	newRule := func(pattern string, priority int64, autoApply bool) model.NewRule {
		return model.NewRule{
			Pattern:    pattern,
			CategoryID: category,
			Priority:   priority,
			AutoApply:  autoApply,
			MatchField: model.RuleFieldAny,
			Direction:  model.RuleDirectionAny,
		}
	}

	for _, rule := range []model.NewRule{
		// Low priority rule that would otherwise be first by insertion order.
		newRule("coffee", 1, true),
		newRule("coffee", 5, true),
		newRule("ignored", 100, false),
	} {
		if _, err := rulesrepo.Create(ctx, d, rule); err != nil {
			t.Fatalf("rules.Create: %v", err)
		}
	}

	matcher, err := Matcher(ctx, d)
	if err != nil {
		t.Fatalf("Matcher: %v", err)
	}
	if len(matcher.Specs) != 2 {
		t.Fatalf("len(Specs) = %d, want 2: only auto-apply rules are compiled", len(matcher.Specs))
	}
	assertCategory(t, matcher.CategoryFor(stringPointer("coffee"), nil, nil), int64(category))
}
