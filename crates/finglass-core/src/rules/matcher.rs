//! The compiled auto-apply rule set, ported from FinDesk's `RuleMatcher`.

use fancy_regex::RegexBuilder;
use rust_decimal::Decimal;

use crate::model::{CategoryId, RuleDirection, RuleField};

use super::payment_providers::{PaymentProviders, ShopMatch};

/// One auto-apply rule compiled for [`RuleMatcher`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuleSpec {
    /// The text pattern.
    pub pattern: String,
    /// The category to assign when the rule matches.
    pub category_id: CategoryId,
    /// The booking field the pattern is matched against.
    pub field: RuleField,
    /// The booking direction the rule applies to.
    pub direction: RuleDirection,
}

impl RuleSpec {
    /// A rule unrestricted in field and direction.
    pub fn new(pattern: impl Into<String>, category_id: CategoryId) -> Self {
        Self {
            pattern: pattern.into(),
            category_id,
            field: RuleField::Any,
            direction: RuleDirection::Any,
        }
    }
}

/// A pattern compiled the way every rule match works: a case-insensitive regular expression when it
/// parses, a plain substring test otherwise.
#[derive(Debug, Clone)]
pub enum CompiledPattern {
    /// A compiled case-insensitive regular expression.
    Regex(Box<fancy_regex::Regex>),
    /// The lower-cased pattern used for a case-insensitive substring test.
    Substring(String),
}

impl CompiledPattern {
    /// Whether the pattern occurs in `text`.
    pub fn is_match(&self, text: &str) -> bool {
        match self {
            Self::Regex(regex) => regex.is_match(text).unwrap_or(false),
            Self::Substring(pattern) => text.to_lowercase().contains(pattern),
        }
    }
}

/// Compiles `pattern` the way every rule match works.
pub fn compile(pattern: &str) -> CompiledPattern {
    match RegexBuilder::new(pattern).case_insensitive(true).build() {
        Ok(regex) => CompiledPattern::Regex(Box::new(regex)),
        Err(_) => CompiledPattern::Substring(pattern.to_lowercase()),
    }
}

/// Auto-apply rules compiled once, in priority order. The first rule matching the original booking
/// (in its text scope and direction) wins, with one exception: for a PayPal payment whose purpose
/// names the shop, a provider rule — one that also matches the partner alone — gives way to the first
/// shop rule whose direction is the provider rule's direction (or any direction when the provider
/// rule is ANY). Any other rule, such as a high-priority purpose rule for a refund, keeps winning.
#[derive(Debug, Clone)]
pub struct RuleMatcher {
    /// The rules in priority order.
    pub specs: Vec<RuleSpec>,
    compiled: Vec<CompiledPattern>,
}

impl RuleMatcher {
    /// Compiles the patterns once.
    pub fn new(specs: Vec<RuleSpec>) -> Self {
        let compiled = specs.iter().map(|spec| compile(&spec.pattern)).collect();
        Self { specs, compiled }
    }

    /// Pairs of pattern to category id, all rules unrestricted (ANY/ANY).
    pub fn of(rules: &[(&str, CategoryId)]) -> Self {
        Self::new(
            rules
                .iter()
                .map(|(pattern, category_id)| RuleSpec::new(*pattern, *category_id))
                .collect(),
        )
    }

    /// Whether `spec`'s direction and text scope accept a booking, tested with `test`.
    pub fn matches(
        spec: &RuleSpec,
        test: &CompiledPattern,
        purpose: Option<&str>,
        partner_name: Option<&str>,
        amount: Option<Decimal>,
    ) -> bool {
        let direction_ok = match spec.direction {
            RuleDirection::Any => true,
            RuleDirection::Expense => amount.is_some_and(|amount| amount < Decimal::ZERO),
            RuleDirection::Income => amount.is_some_and(|amount| amount > Decimal::ZERO),
        };
        if !direction_ok {
            return false;
        }

        let text = match spec.field {
            RuleField::Any => match (purpose, partner_name) {
                (Some(purpose), Some(partner)) => format!("{purpose} {partner}"),
                (Some(purpose), None) => purpose.to_owned(),
                (None, Some(partner)) => partner.to_owned(),
                (None, None) => String::new(),
            },
            RuleField::Partner => partner_name.unwrap_or("").to_owned(),
            RuleField::Purpose => purpose.unwrap_or("").to_owned(),
        };

        !text.trim().is_empty() && test.is_match(&text)
    }

    /// The index of the rule that applies to a booking among `specs` in priority order, with `tests`
    /// compiled from their patterns in the same order and the shop `shop_match` already extracted
    /// once by the caller.
    pub fn select(
        specs: &[RuleSpec],
        tests: &[CompiledPattern],
        purpose: Option<&str>,
        partner_name: Option<&str>,
        amount: Option<Decimal>,
        shop_match: Option<&ShopMatch>,
    ) -> Option<usize> {
        let original = specs
            .iter()
            .enumerate()
            .find(|(index, spec)| Self::matches(spec, &tests[*index], purpose, partner_name, amount))
            .map(|(index, _)| index);

        let Some(shop_match) = shop_match else {
            return original;
        };
        let Some(original) = original else {
            return specs
                .iter()
                .enumerate()
                .find(|(index, spec)| Self::matches(spec, &tests[*index], None, Some(&shop_match.shop), amount))
                .map(|(index, _)| index);
        };

        let winner = &specs[original];
        if !Self::matches(
            winner,
            &tests[original],
            shop_match.provider_purpose.as_deref(),
            shop_match.provider_partner.as_deref(),
            amount,
        ) {
            return Some(original);
        }

        specs
            .iter()
            .enumerate()
            .find(|(index, candidate)| {
                (winner.direction == RuleDirection::Any || candidate.direction == winner.direction)
                    && Self::matches(candidate, &tests[*index], None, Some(&shop_match.shop), amount)
            })
            .map(|(index, _)| index)
            .or(Some(original))
    }

    /// The category id of the rule [`Self::select`] picks, or `None`.
    pub fn category_for(
        &self,
        purpose: Option<&str>,
        partner_name: Option<&str>,
        amount: Option<Decimal>,
    ) -> Option<CategoryId> {
        let shop_match = PaymentProviders::shop_match(purpose, partner_name);
        Self::select(
            &self.specs,
            &self.compiled,
            purpose,
            partner_name,
            amount,
            shop_match.as_ref(),
        )
        .map(|index| self.specs[index].category_id)
    }
}

#[cfg(test)]
mod tests {
    use rust_decimal::Decimal;

    use super::*;
    use crate::db::Database;
    use crate::model::{CategoryId, NewCategory, NewRule};
    use crate::repo::categories;
    use crate::secret::{Passphrase, PassphrasePurpose};

    fn expense() -> Decimal {
        "-10.00".parse().unwrap()
    }

    fn income() -> Decimal {
        "10.00".parse().unwrap()
    }

    fn one(pattern: &str, field: RuleField, direction: RuleDirection, category_id: i64) -> RuleSpec {
        RuleSpec {
            pattern: pattern.to_owned(),
            category_id: CategoryId(category_id),
            field,
            direction,
        }
    }

    fn any(pattern: &str, category_id: i64) -> RuleSpec {
        one(pattern, RuleField::Any, RuleDirection::Any, category_id)
    }

    #[test]
    fn partner_rule_matches_the_partner_name_but_not_a_purpose_that_only_mentions_it() {
        let matcher = RuleMatcher::new(vec![one("Erika Mustermann", RuleField::Partner, RuleDirection::Any, 1)]);

        assert_eq!(
            matcher.category_for(None, Some("Erika Mustermann"), Some(expense())),
            Some(CategoryId(1))
        );
        assert_eq!(
            matcher.category_for(Some("Gehalt Erika Mustermann"), Some("BEISPIEL GMBH"), None),
            None
        );
        assert_eq!(matcher.category_for(None, None, None), None);
    }

    #[test]
    fn purpose_rule_ignores_the_partner_name() {
        let matcher = RuleMatcher::new(vec![one("Gehalt", RuleField::Purpose, RuleDirection::Any, 1)]);

        assert_eq!(
            matcher.category_for(Some("Gehalt Oktober"), Some("BEISPIEL GMBH"), Some(income())),
            Some(CategoryId(1))
        );
        assert_eq!(
            matcher.category_for(Some("Kartenzahlung"), Some("Gehaltshaus"), Some(expense())),
            None
        );
        assert_eq!(matcher.category_for(None, Some("Gehalt"), Some(income())), None);
    }

    #[test]
    fn any_rule_searches_purpose_and_partner_together_as_before() {
        let matcher = RuleMatcher::new(vec![any("REWE", 1)]);

        assert_eq!(
            matcher.category_for(Some("REWE SAGT DANKE"), None, None),
            Some(CategoryId(1))
        );
        assert_eq!(
            matcher.category_for(None, Some("REWE Markt GmbH"), None),
            Some(CategoryId(1))
        );
        assert_eq!(
            matcher.category_for(Some("rewe"), Some("Edeka"), None),
            Some(CategoryId(1))
        );
    }

    #[test]
    fn blank_text_does_not_match() {
        let matcher = RuleMatcher::new(vec![one("REWE", RuleField::Purpose, RuleDirection::Any, 1)]);

        assert_eq!(matcher.category_for(None, Some("REWE Markt GmbH"), None), None);
    }

    #[test]
    fn expense_rule_matches_negative_amounts_only() {
        let matcher = RuleMatcher::new(vec![one("Beispiel", RuleField::Any, RuleDirection::Expense, 1)]);

        assert_eq!(
            matcher.category_for(Some("Beispiel"), None, Some(expense())),
            Some(CategoryId(1))
        );
        assert_eq!(matcher.category_for(Some("Beispiel"), None, Some(income())), None);
        assert_eq!(
            matcher.category_for(Some("Beispiel"), None, None),
            None,
            "with amount == null only ANY rules can match"
        );
    }

    #[test]
    fn income_rule_matches_positive_amounts_only() {
        let matcher = RuleMatcher::new(vec![one("Beispiel", RuleField::Any, RuleDirection::Income, 1)]);

        assert_eq!(
            matcher.category_for(Some("Beispiel"), None, Some(income())),
            Some(CategoryId(1))
        );
        assert_eq!(matcher.category_for(Some("Beispiel"), None, Some(expense())), None);
        assert_eq!(matcher.category_for(Some("Beispiel"), None, None), None);
    }

    #[test]
    fn first_rule_in_order_wins_when_an_earlier_rule_is_excluded_by_direction() {
        let matcher = RuleMatcher::new(vec![
            one("beispielpay", RuleField::Any, RuleDirection::Income, 2),
            one("beispielpay", RuleField::Any, RuleDirection::Any, 3),
        ]);

        assert_eq!(
            matcher.category_for(None, Some("BeispielPay"), Some(expense())),
            Some(CategoryId(3))
        );
        assert_eq!(
            matcher.category_for(None, Some("BeispielPay"), Some(income())),
            Some(CategoryId(2))
        );
    }

    #[test]
    fn of_creates_unrestricted_rules_as_before() {
        let matcher = RuleMatcher::of(&[("erika", CategoryId(7))]);

        assert_eq!(
            matcher.category_for(Some("Überweisung an Erika"), None, None),
            Some(CategoryId(7))
        );
    }

    #[test]
    fn a_pattern_with_an_umlaut_matches_case_insensitively() {
        let matcher = RuleMatcher::new(vec![one("Bäckerei", RuleField::Partner, RuleDirection::Any, 1)]);

        assert_eq!(
            matcher.category_for(None, Some("BÄCKEREI SCHMIDT"), None),
            Some(CategoryId(1))
        );
    }

    #[test]
    fn a_pattern_with_a_lookahead_matches_like_java() {
        // Java-style negative lookahead: an Amazon booking that is not a refund.
        let matcher = RuleMatcher::new(vec![any("^(?!.*refund).*amazon", 1)]);

        assert_eq!(
            matcher.category_for(Some("AMAZON EU order"), None, Some(expense())),
            Some(CategoryId(1))
        );
        assert_eq!(matcher.category_for(Some("Amazon refund"), None, Some(income())), None);
    }

    #[test]
    fn an_invalid_pattern_falls_back_to_a_case_insensitive_substring() {
        let matcher = RuleMatcher::new(vec![any("(", 1)]);

        assert_eq!(
            matcher.category_for(Some("Amount (EUR)"), None, None),
            Some(CategoryId(1))
        );
        assert_eq!(matcher.category_for(Some("Amount EUR"), None, None), None);
    }

    // PayPal shop matching: a shop rule displaces only the provider rule, never a rule that matches the original.

    const PAYPAL_PARTNER: &str = "PayPal Europe S.a.r.l. et Cie S.C.A";

    fn paypal_purpose(shop: &str) -> String {
        format!("EREF+R1 SVWZ+PP.471.PPX . {shop}, Ihr Einkauf bei {shop}")
    }

    #[test]
    fn a_partner_rule_for_the_shop_beats_the_paypal_rule_whatever_the_rule_order() {
        let matcher = RuleMatcher::new(vec![
            one("PayPal", RuleField::Partner, RuleDirection::Any, 2),
            one("Beispiel Shop", RuleField::Partner, RuleDirection::Any, 1),
        ]);

        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            Some(CategoryId(1))
        );
        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(income())
            ),
            Some(CategoryId(1))
        );
    }

    #[test]
    fn with_no_shop_rule_the_paypal_rule_applies_through_the_original_view() {
        let matcher = RuleMatcher::new(vec![one("PayPal", RuleField::Partner, RuleDirection::Any, 2)]);

        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            Some(CategoryId(2))
        );
    }

    #[test]
    fn a_purpose_only_rule_keeps_winning_for_a_paypal_booking() {
        let purpose_only = RuleMatcher::new(vec![one("Ihr Einkauf", RuleField::Purpose, RuleDirection::Any, 3)]);
        assert_eq!(
            purpose_only.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            Some(CategoryId(3))
        );

        let with_shop = RuleMatcher::new(vec![
            one("Ihr Einkauf", RuleField::Purpose, RuleDirection::Any, 3),
            one("Beispiel", RuleField::Partner, RuleDirection::Any, 1),
        ]);
        assert_eq!(
            with_shop.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            Some(CategoryId(3))
        );
    }

    #[test]
    fn an_income_only_rule_does_not_match_an_expense_via_the_shop_view() {
        let matcher = RuleMatcher::new(vec![one("Beispiel", RuleField::Partner, RuleDirection::Income, 4)]);

        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            None
        );
        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(income())
            ),
            Some(CategoryId(4))
        );
    }

    #[test]
    fn a_shop_rule_applies_when_no_other_rule_matches() {
        let matcher = RuleMatcher::new(vec![one("Beispiel Shop", RuleField::Partner, RuleDirection::Any, 20)]);

        assert_eq!(
            matcher.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(expense())
            ),
            Some(CategoryId(20))
        );
    }

    #[test]
    fn a_shop_rule_beats_the_paypal_rule_for_a_card_booking_with_a_paypal_partner() {
        let matcher = RuleMatcher::new(vec![
            one("PayPal", RuleField::Partner, RuleDirection::Any, 10),
            one("BEISPIELSHOP", RuleField::Partner, RuleDirection::Any, 20),
        ]);

        assert_eq!(
            matcher.category_for(
                Some("PAYPAL *BEISPIELSHOP"),
                Some("PAYPAL *BEISPIELSHOP"),
                Some(expense())
            ),
            Some(CategoryId(20))
        );
    }

    #[test]
    fn a_non_paypal_booking_is_matched_exactly_as_before() {
        let matcher = RuleMatcher::new(vec![one("Beispiel", RuleField::Partner, RuleDirection::Any, 5)]);

        assert_eq!(
            matcher.category_for(Some("Kauf bei Beispiel"), Some("Beispiel Shop GmbH"), Some(expense())),
            Some(CategoryId(5)),
            "no PayPal partner, no shop view"
        );
        assert_eq!(
            matcher.category_for(Some("Ihr Einkauf bei Beispiel"), None, Some(expense())),
            None,
            "a PayPal-like purpose without a PayPal partner stays rule-less"
        );
    }

    #[test]
    fn a_refund_purpose_rule_keeps_winning_over_a_shop_rule() {
        let refund_purpose =
            "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Rückerstattung, Ihr Einkauf bei Beispiel Shop GmbH";
        let matcher = RuleMatcher::new(vec![
            one("Rückerstattung|Storno", RuleField::Purpose, RuleDirection::Any, 10),
            one("Beispiel Shop", RuleField::Partner, RuleDirection::Any, 20),
        ]);

        assert_eq!(
            matcher.category_for(Some(refund_purpose), Some(PAYPAL_PARTNER), Some(income())),
            Some(CategoryId(10))
        );
    }

    #[test]
    fn a_shop_rule_only_displaces_a_provider_rule_of_the_same_or_any_direction() {
        let any_direction_shop = RuleMatcher::new(vec![
            one("PayPal", RuleField::Partner, RuleDirection::Income, 10),
            one("Beispiel Shop", RuleField::Partner, RuleDirection::Any, 20),
        ]);
        assert_eq!(
            any_direction_shop.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(income())
            ),
            Some(CategoryId(10))
        );

        let income_shop = RuleMatcher::new(vec![
            one("PayPal", RuleField::Partner, RuleDirection::Income, 10),
            one("Beispiel Shop", RuleField::Partner, RuleDirection::Income, 20),
        ]);
        assert_eq!(
            income_shop.category_for(
                Some(&paypal_purpose("Beispiel Shop GmbH")),
                Some(PAYPAL_PARTNER),
                Some(income())
            ),
            Some(CategoryId(20))
        );
    }

    #[test]
    fn a_non_paypal_booking_never_gets_a_shop_view() {
        let matcher = RuleMatcher::new(vec![
            one("PayPal", RuleField::Partner, RuleDirection::Any, 10),
            one("Beispiel Shop", RuleField::Partner, RuleDirection::Any, 20),
        ]);

        assert_eq!(
            matcher.category_for(
                Some("Kauf bei Beispiel Shop"),
                Some("Beispiel Shop GmbH"),
                Some(expense())
            ),
            Some(CategoryId(20))
        );
        assert_eq!(
            matcher.category_for(Some("Ihr Einkauf bei Beispiel Shop"), None, Some(expense())),
            None
        );
    }

    #[test]
    fn card_format_rules_behave_like_the_marker_format() {
        let matcher = RuleMatcher::new(vec![
            one("PayPal", RuleField::Any, RuleDirection::Any, 10),
            one("BEISPIELSHOP", RuleField::Any, RuleDirection::Any, 20),
        ]);

        assert_eq!(
            matcher.category_for(Some("PAYPAL *BEISPIELSHOP"), None, Some(expense())),
            Some(CategoryId(20))
        );

        let purpose_only = RuleMatcher::new(vec![
            one("RETURN", RuleField::Purpose, RuleDirection::Any, 10),
            one("BEISPIELSHOP", RuleField::Partner, RuleDirection::Any, 20),
        ]);
        assert_eq!(
            purpose_only.category_for(
                Some("PAYPAL *BEISPIELSHOP 12345678 RETURN"),
                Some("PAYPAL *BEISPIELSHOP"),
                Some(income())
            ),
            Some(CategoryId(10))
        );
    }

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    #[test]
    fn the_matcher_is_built_from_auto_apply_rules_in_priority_order() {
        let (_temp, db) = database();
        let category = categories::create(
            &db,
            NewCategory {
                name: "Food".to_owned(),
                color: "#3BC7B0".to_owned(),
                budget_limit: None,
                is_tax_relevant: false,
                is_transfer: false,
            },
        )
        .unwrap();

        // Low priority rule that would otherwise be first by insertion order.
        crate::repo::rules::create(
            &db,
            NewRule {
                pattern: "coffee".to_owned(),
                category_id: category,
                priority: 1,
                auto_apply: true,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )
        .unwrap();
        crate::repo::rules::create(
            &db,
            NewRule {
                pattern: "coffee".to_owned(),
                category_id: category,
                priority: 5,
                auto_apply: true,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )
        .unwrap();
        crate::repo::rules::create(
            &db,
            NewRule {
                pattern: "ignored".to_owned(),
                category_id: category,
                priority: 100,
                auto_apply: false,
                match_field: RuleField::Any,
                direction: RuleDirection::Any,
                tax_relevant: None,
            },
        )
        .unwrap();

        let matcher = super::super::matcher(&db).unwrap();
        assert_eq!(matcher.specs.len(), 2, "only auto-apply rules are compiled");
        assert_eq!(matcher.category_for(Some("coffee"), None, None), Some(category));
    }
}
