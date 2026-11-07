//! Display-only cleanup of raw bank text for lists, ported from FinDesk's `util/BankText.kt`.
//!
//! The stored `purpose` and `partner_name` never change: rules, search and duplicate detection work on the raw text.
//! These functions only render the readable part of it.

use std::sync::LazyLock;

use fancy_regex::Regex;

use crate::model::TransactionItem;
use crate::rules::PaymentProviders;

/// SEPA structured-field tags: an upper-case code of 4 letters (`BIC` is 3) directly followed by `+`.
///
/// Banks write them in upper case, but they are matched case-insensitively to be robust.
pub static TAG: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\+")
        .expect("tag pattern is valid")
});

/// Runs of 3 or more whitespace characters are the bank's padding.
static PADDING: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\s{3,}").expect("padding pattern is valid"));

const PLACEHOLDER: &str = "—";

/// Any whitespace run collapses to one space.
fn collapse_whitespace(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// The readable partner: splits on the bank's runs of 3 or more padding characters and keeps the first non-blank
/// part; any remaining whitespace run collapses to one space. Blank resolves to `None`.
pub fn display_partner(raw: Option<&str>) -> Option<String> {
    let raw = raw?;
    let mut start = 0_usize;
    let mut parts: Vec<&str> = Vec::new();
    for matched in PADDING.find_iter(raw).filter_map(std::result::Result::ok) {
        parts.push(&raw[start..matched.start()]);
        start = matched.end();
    }
    parts.push(&raw[start..]);

    parts
        .into_iter()
        .find(|part| !part.trim().is_empty())
        .map(collapse_whitespace)
        .map(|part| part.trim().to_owned())
        .filter(|part| !part.is_empty())
}

/// The readable purpose: if an `SVWZ+` field exists, its text up to the next tag; otherwise the text outside all
/// tagged fields (each tagged field runs from its tag to the next tag). A text with no tag at all is returned with
/// whitespace collapsed and no other change. Blank resolves to `None`.
pub fn display_purpose(raw: Option<&str>) -> Option<String> {
    let trimmed = raw?.trim();
    if trimmed.is_empty() {
        return None;
    }

    let matches: Vec<_> = TAG.find_iter(trimmed).filter_map(std::result::Result::ok).collect();
    let result = if matches.is_empty() {
        Some(trimmed.to_owned())
    } else if matches.iter().any(|matched| is_svwz(matched.as_str())) {
        // SVWZ holds the actual description; everything after it up to the next tag belongs to it.
        substrings(trimmed, &matches)
            .into_iter()
            .find(|(tag, _)| is_svwz(tag))
            .map(|(_, text)| text)
    } else {
        // Only the text before the first tag is outside the tagged fields.
        Some(trimmed[..matches[0].start()].to_owned())
    };

    result
        .map(|text| collapse_whitespace(&text).trim().to_owned())
        .filter(|text| !text.is_empty())
}

fn is_svwz(tag: &str) -> bool {
    tag.get(..4).is_some_and(|prefix| prefix.eq_ignore_ascii_case("SVWZ"))
}

/// Each tagged field as `(tag, text up to the next tag or the end)`; text before the first tag is dropped.
fn substrings(text: &str, matches: &[fancy_regex::Match<'_>]) -> Vec<(String, String)> {
    matches
        .iter()
        .enumerate()
        .map(|(index, matched)| {
            let end = matches.get(index + 1).map_or(text.len(), fancy_regex::Match::start);
            (
                text[matched.start()..matched.end()].to_owned(),
                text[matched.end()..end].to_owned(),
            )
        })
        .collect()
}

/// The readable title on every screen: for a PayPal payment "PayPal · <shop>", else the cleaned partner, else the
/// cleaned purpose, else a placeholder.
pub fn display_description(item: &TransactionItem) -> String {
    PaymentProviders::title_of(item.purpose.as_deref(), item.partner_name.as_deref())
        .or_else(|| display_partner(item.partner_name.as_deref()))
        .or_else(|| display_purpose(item.purpose.as_deref()))
        .unwrap_or_else(|| PLACEHOLDER.to_owned())
}

/// The row's second line: `None` when [`PaymentProviders::merchant_of`] names a shop (the title already names the
/// provider and the shop, and the full purpose stays in the details dialog), else the cleaned purpose when the
/// booking has a partner.
pub fn secondary_line(item: &TransactionItem) -> Option<String> {
    if PaymentProviders::merchant_of(item.purpose.as_deref(), item.partner_name.as_deref()).is_some() {
        None
    } else {
        display_purpose(item.purpose.as_deref()).filter(|_| item.partner_name.is_some())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{AccountId, TaxSource, TransactionId};
    use crate::money::{Currency, Money};
    use time::macros::date;

    fn txn(partner: Option<&str>, purpose: Option<&str>) -> TransactionItem {
        TransactionItem {
            id: TransactionId(1),
            account_id: AccountId(1),
            account_iban: Some("DE89370400440532013000".to_owned()),
            date: date!(2026 - 10 - 24),
            amount: Money {
                minor: -4250,
                currency: Currency::EUR,
            },
            balance_after: None,
            currency: Currency::EUR,
            partner_name: partner.map(str::to_owned),
            purpose: purpose.map(str::to_owned),
            category_id: None,
            category_name: None,
            category_color: None,
            is_tax_relevant: false,
            document_count: 0,
            is_new: false,
            is_transfer: false,
            tax_source: TaxSource::None,
            tax_rule_pattern: None,
            tax_override: None,
        }
    }

    #[test]
    fn a_purpose_with_eref_mref_cred_and_svwz_shows_only_the_svwz_text() {
        let raw = "EREF+REF0001 MREF+M-77 CRED+DE00ZZZ00000000001 SVWZ+Rechnung 4711";
        assert_eq!(display_purpose(Some(raw)).as_deref(), Some("Rechnung 4711"));
    }

    #[test]
    fn tags_glued_to_text_without_spaces_are_still_separated() {
        assert_eq!(
            display_purpose(Some("EREF+X1SVWZ+Miete Oktober")).as_deref(),
            Some("Miete Oktober")
        );
    }

    #[test]
    fn svwz_text_ends_at_the_next_tag() {
        assert_eq!(
            display_purpose(Some("SVWZ+Taschengeld PURP+RINP")).as_deref(),
            Some("Taschengeld")
        );
    }

    #[test]
    fn a_purpose_of_only_tags_without_svwz_shows_the_untagged_remainder_or_nothing() {
        // Nothing before the first tag and nothing after the last: no readable text at all
        assert_eq!(
            display_purpose(Some("EREF+B00520201172143239 MREF+P0202011731458239")),
            None
        );
        // Text left of the tags is outside the tagged fields
        assert_eq!(
            display_purpose(Some("Kartenzahlung EREF+REF0001")).as_deref(),
            Some("Kartenzahlung")
        );
    }

    #[test]
    fn lower_case_tags_are_recognised_too() {
        assert_eq!(display_purpose(Some("svwz+abc")).as_deref(), Some("abc"));
    }

    #[test]
    fn a_plain_purpose_without_tags_is_returned_unchanged_except_collapsed_whitespace() {
        assert_eq!(
            display_purpose(Some("Warenkauf  12.03.2026")).as_deref(),
            Some("Warenkauf 12.03.2026")
        );
    }

    #[test]
    fn a_plus_sign_that_opens_no_tag_is_left_alone() {
        assert_eq!(display_purpose(Some("A+B Bau")).as_deref(), Some("A+B Bau"));
    }

    #[test]
    fn the_first_non_blank_part_of_a_padded_partner_is_shown() {
        assert_eq!(
            display_partner(Some(
                "BEISPIEL KARTE GOLD      1234 56XX XXXX 7890      Abrechnung 10/26"
            ))
            .as_deref(),
            Some("BEISPIEL KARTE GOLD")
        );
    }

    #[test]
    fn a_double_space_in_a_partner_collapses_to_one() {
        assert_eq!(
            display_partner(Some("BEISPIEL  GMBH")).as_deref(),
            Some("BEISPIEL GMBH")
        );
    }

    #[test]
    fn null_and_blank_resolve_to_none() {
        assert_eq!(display_partner(None), None);
        assert_eq!(display_partner(Some("   ")), None);
        assert_eq!(display_purpose(None), None);
        assert_eq!(display_purpose(Some("\t  ")), None);
    }

    #[test]
    fn the_row_description_prefers_the_partner_then_the_purpose_then_a_placeholder() {
        let with_partner = display_description(&txn(Some("Erika Mustermann"), Some("EREF+R1 SVWZ+Rechnung 4711")));
        let without_partner = display_description(&txn(None, Some("EREF+R1 SVWZ+Rechnung 4711")));
        let with_nothing = display_description(&txn(None, None));
        assert_eq!(with_partner, "Erika Mustermann");
        assert_eq!(without_partner, "Rechnung 4711");
        assert_eq!(with_nothing, "—");
    }

    #[test]
    fn the_second_line_is_none_when_a_paypal_booking_names_a_shop() {
        let paypal = txn(
            Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
            Some("Ihr Einkauf bei Beispiel Shop GmbH"),
        );
        assert_eq!(
            secondary_line(&paypal),
            None,
            "the title already names the provider and the shop"
        );
    }

    #[test]
    fn a_shopless_paypal_booking_keeps_its_cleaned_purpose_on_the_second_line() {
        let paypal = txn(
            Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
            Some("PP.1234.PP ABBUCHUNG VOM GIROKONTO"),
        );
        assert_eq!(
            secondary_line(&paypal).as_deref(),
            Some("PP.1234.PP ABBUCHUNG VOM GIROKONTO")
        );
    }

    #[test]
    fn a_plain_bookings_second_line_is_the_cleaned_purpose() {
        let plain = txn(Some("Beispiel Inc."), Some("EREF+R1 SVWZ+Rechnung 4711"));
        assert_eq!(secondary_line(&plain).as_deref(), Some("Rechnung 4711"));
    }

    #[test]
    fn a_booking_without_a_partner_has_no_second_line() {
        let no_partner = txn(None, Some("SVWZ+Rechnung 4711"));
        assert_eq!(secondary_line(&no_partner), None);
    }

    #[test]
    fn a_paypal_booking_is_titled_with_the_provider_and_the_shop() {
        let paypal = txn(
            Some("PayPal Europe S.a.r.l. et Cie S.C.A"),
            Some("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
        );
        assert_eq!(display_description(&paypal), "PayPal · Beispiel Shop GmbH");
    }

    #[test]
    fn a_non_paypal_title_is_unchanged() {
        let plain = txn(
            Some("Beispiel Shop GmbH"),
            Some("EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
        );
        assert_eq!(
            display_description(&plain),
            "Beispiel Shop GmbH",
            "partner wins as before; no PayPal partner, no shop title"
        );

        let paypal_purpose_only = txn(None, Some("SVWZ+Ihr Einkauf bei Beispiel Shop GmbH"));
        assert_eq!(
            display_description(&paypal_purpose_only),
            "Ihr Einkauf bei Beispiel Shop GmbH",
            "no PayPal partner: the purpose is shown as before"
        );
    }
}
