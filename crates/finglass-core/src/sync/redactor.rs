//! Masks sensitive values in bank text, ported from FinDesk's `BankingRedactor.kt`.
//!
//! [`redact`] is for protocol traces: it masks known secrets plus IBAN-looking tokens. [`mask_account_numbers`] is
//! for text that is stored or shown, keeping the last four characters so the user can still tell accounts apart.

use std::sync::LazyLock;

use fancy_regex::{Captures, Regex};

use crate::iban;

/// Replacement for every masked value; a single X, as Hibiscus' support instructions suggest.
pub const MASK: &str = "X";

/// Shortest value worth masking; below this a "secret" is more likely to damage readable text.
const MIN_SECRET_LENGTH: usize = 4;

static IBAN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b").expect("valid IBAN pattern"));
static FINTS_SYNTAX: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"[+:'?@]").expect("valid FinTS syntax pattern"));
static SPACED_IBAN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b").expect("valid spaced IBAN pattern")
});
static LONG_DIGITS: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?<![\dA-Za-z])\d{6,}(?![\dA-Za-z])").expect("valid long digit pattern"));
static LABELLED_ACCOUNT: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)((?:konto(?:nummer|nr)?|\bkto|\baccount(?: no\.?| number)?|\bacct)(?:\.?-?nr\.?)?:?\s+)(\d{1,12}(?: \d{1,12})*)(?![,.]?\d)",
    )
    .expect("valid labelled account pattern")
});
static RETURN_CODE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?<![\w…])[39]\d{3}(?!\w)").expect("valid return code pattern"));

/// FinTS return codes (3xxx warnings, 9xxx errors) in already masked bank text.
///
/// A code standing right after the mask is the kept tail of an account number ("…3000"), not a code.
pub fn return_codes(masked_text: Option<&str>) -> Vec<String> {
    let Some(text) = masked_text else {
        return Vec::new();
    };
    let mut codes = Vec::new();
    for capture in RETURN_CODE.find_iter(text).flatten() {
        let value = capture.as_str().to_owned();
        if !codes.contains(&value) {
            codes.push(value);
        }
    }
    codes
}

/// Masks known secrets and IBAN-looking tokens.
pub fn redact(text: &str, secrets: &[&str]) -> String {
    let mut result = text.to_owned();

    // Longest first, so a value that contains another is masked in one go instead of leaving fragments.
    let mut values = secrets
        .iter()
        .filter(|secret| secret.len() >= MIN_SECRET_LENGTH && !secret.trim().is_empty())
        .flat_map(|secret| [(*secret).to_owned(), fints_escaped(secret)])
        .collect::<Vec<_>>();
    values.sort_by_key(|value| std::cmp::Reverse(value.len()));
    values.dedup();
    for value in values {
        result = result.replace(&value, MASK);
    }

    result = IBAN.replace_all(&result, MASK).into_owned();
    // Spaced or lower-case IBANs only when the check digits match, so ids and payloads stay readable.
    SPACED_IBAN
        .replace_all(&result, |captures: &Captures<'_, str>| {
            let matched = captures.get(0).map_or("", |matched| matched.as_str());
            mask_spaced_iban(matched)
        })
        .into_owned()
}

/// Masks account identifiers in bank text before it is stored, keeping the last four characters.
pub fn mask_account_numbers(text: &str) -> String {
    let mut result = SPACED_IBAN
        .replace_all(text, |captures: &Captures<'_, str>| {
            let matched = captures.get(0).map_or("", |matched| matched.as_str());
            match valid_iban_prefix_length(matched) {
                Some(length) => {
                    let prefix = &matched[..length];
                    let tail = last_four(&iban::normalize(prefix));
                    format!("…{tail}{}", &matched[length..])
                }
                // No valid IBAN: fail closed, whatever the match contains could still be an account number.
                None => format!("…{}", last_four(&matched.replace(' ', ""))),
            }
        })
        .into_owned();

    result = LABELLED_ACCOUNT
        .replace_all(&result, |captures: &Captures<'_, str>| {
            let label = captures.get(1).map_or("", |label| label.as_str());
            let digits = captures.get(2).map_or("", |digits| digits.as_str()).replace(' ', "");
            if digits.len() >= 6 {
                format!("{label}…{}", last_four(&digits))
            } else {
                captures.get(0).map_or("", |matched| matched.as_str()).to_owned()
            }
        })
        .into_owned();

    LONG_DIGITS
        .replace_all(&result, |captures: &Captures<'_, str>| {
            let matched = captures.get(0).map_or("", |matched| matched.as_str());
            format!("…{}", last_four(matched))
        })
        .into_owned()
}

/// The form a value takes inside a raw FinTS message, where the syntax characters `+ : ' ? @` are escaped with `?`.
pub fn fints_escaped(value: &str) -> String {
    FINTS_SYNTAX
        .replace_all(value, |captures: &Captures<'_, str>| {
            let matched = captures.get(0).map_or("", |matched| matched.as_str());
            format!("?{matched}")
        })
        .into_owned()
}

/// The length of the longest prefix of a [`SPACED_IBAN`] match that is a valid IBAN, or `None` when none is.
fn valid_iban_prefix_length(matched: &str) -> Option<usize> {
    if iban::is_valid(matched) {
        return Some(matched.len());
    }
    let mut cut = matched.rfind(' ');
    while let Some(index) = cut {
        if iban::is_valid(&matched[..index]) {
            return Some(index);
        }
        cut = matched[..index].rfind(' ');
    }
    None
}

fn mask_spaced_iban(matched: &str) -> String {
    match valid_iban_prefix_length(matched) {
        Some(length) => format!("{MASK}{}", &matched[length..]),
        None => matched.to_owned(),
    }
}

fn last_four(text: &str) -> String {
    let chars: Vec<char> = text.chars().collect();
    chars[chars.len().saturating_sub(4)..].iter().collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn masks_known_secrets_and_iban_like_tokens() {
        let line = "user=1234567890 iban=DE89370400440532013000 amount=12,34 rewe";
        let redacted = redact(line, &["1234567890"]);
        assert!(!redacted.contains("1234567890"));
        assert!(!redacted.contains("DE89370400440532013000"));
        assert!(redacted.contains("amount=12,34 rewe"));
    }

    #[test]
    fn does_not_mask_short_values() {
        assert_eq!(redact("PIN 123 bleibt", &["123"]), "PIN 123 bleibt");
    }

    #[test]
    fn masks_the_longest_overlapping_secret_without_leaving_fragments() {
        assert_eq!(redact("abcdef", &["abc", "abcdef"]), MASK);
    }

    #[test]
    fn blank_secrets_are_ignored() {
        assert_eq!(redact("text", &["", "  "]), "text");
    }

    #[test]
    fn masks_secrets_in_their_fints_escaped_form() {
        let line = "HNSHA:5:2+998+1234567+++ab?+cd?:e?'f???@g'";
        assert_eq!(redact(line, &["ab+cd:e'f?@g"]), "HNSHA:5:2+998+1234567+++X'");
    }

    #[test]
    fn escapes_every_fints_syntax_character_once() {
        assert_eq!(fints_escaped("a+b:c'd?e@f"), "a?+b?:c?'d??e?@f");
        assert_eq!(fints_escaped("plain"), "plain");
    }

    #[test]
    fn masks_spaced_and_lower_case_ibans() {
        assert_eq!(
            redact("IBAN DE89 3704 0044 0532 0130 00 Miete", &[]),
            format!("IBAN {MASK} Miete")
        );
        assert_eq!(redact("de89370400440532013000", &[]), MASK);
        assert_eq!(redact("de89 3704 0044 0532 0130 00", &[]), MASK);
    }

    #[test]
    fn masks_a_spaced_iban_whose_regex_match_swallowed_the_next_word() {
        let iban = "ES91 2100 0418 4502 0005 1332";
        assert!(iban::is_valid(iban));
        assert_eq!(redact(&format!("{iban} EUR"), &[]), format!("{MASK} EUR"));
    }

    #[test]
    fn masks_a_spaced_iban_whose_regex_match_swallowed_the_next_bic() {
        let iban = "AT61 1904 3002 3457 3201";
        assert!(iban::is_valid(iban));
        assert_eq!(
            redact(&format!("{iban} BIC BKAUATWW"), &[]),
            format!("{MASK} BIC BKAUATWW")
        );
    }

    #[test]
    fn masks_a_spaced_iban_whose_regex_match_swallowed_the_next_number() {
        let iban = "BE68 5390 0754 7034";
        assert!(iban::is_valid(iban));
        assert_eq!(redact(&format!("{iban} 2026"), &[]), format!("{MASK} 2026"));
    }

    #[test]
    fn masks_a_spaced_iban_inside_a_sentence_and_keeps_the_surrounding_words() {
        let iban = "PL61 1090 1014 0000 0712 1981 2874";
        assert!(iban::is_valid(iban));
        assert_eq!(redact(&format!("Konto {iban} ok"), &[]), format!("Konto {MASK} ok"));
    }

    #[test]
    fn does_not_mask_a_spaced_token_without_a_valid_iban_prefix() {
        let token = "DE00 1234 5678 9012 3456 78 X";
        assert!(!iban::is_valid("DE00 1234 5678 9012 3456 78"));
        assert_eq!(redact(token, &[]), token);
    }

    #[test]
    fn keeps_a_typical_trace_line_unchanged() {
        let line = "HNHBK:1:3+000000000123+300+DIA4711ABCD12345678+2'HIRMG:2:2+3060::Bitte beachten Sie die enthaltenen Warnungen.'";
        assert_eq!(redact(line, &[]), line);
    }

    #[test]
    fn does_not_mask_an_iban_looking_token_that_fails_the_check_digits() {
        let token = "DE00 1234 5678 9012 3456 78";
        assert!(!iban::is_valid(token));
        assert_eq!(redact(token, &[]), token);
    }

    #[test]
    fn mask_account_numbers_masks_ibans_keeping_the_last_four() {
        assert_eq!(
            mask_account_numbers("IBAN DE89370400440532013000 ungültig"),
            "IBAN …3000 ungültig"
        );
        assert_eq!(
            mask_account_numbers("IBAN DE89 3704 0044 0532 0130 00 ungültig"),
            "IBAN …3000 ungültig"
        );
        assert_eq!(mask_account_numbers("de89370400440532013000"), "…3000");
    }

    #[test]
    fn mask_account_numbers_masks_a_spaced_iban_whose_match_swallowed_the_next_word() {
        assert_eq!(
            mask_account_numbers("Konto ES91 2100 0418 4502 0005 1332 EUR ok"),
            "Konto …1332 EUR ok"
        );
    }

    #[test]
    fn mask_account_numbers_does_not_turn_a_kept_iban_tail_into_a_return_code() {
        assert_eq!(
            return_codes(Some(&mask_account_numbers(
                "Konto ES91 2100 0418 4502 0005 1332 EUR ok"
            ))),
            Vec::<String>::new()
        );
    }

    #[test]
    fn mask_account_numbers_keeps_the_last_four_of_valid_spaced_and_lower_case_ibans() {
        assert_eq!(mask_account_numbers("DE89 3704 0044 0532 0130 00"), "…3000");
        assert_eq!(mask_account_numbers("de89 3704 0044 0532 0130 00"), "…3000");
    }

    #[test]
    fn mask_account_numbers_fails_closed_for_a_spaced_iban_with_a_wrong_check_digit() {
        assert!(!iban::is_valid("DE00 3704 0044 0532 0130 00"));
        assert_eq!(mask_account_numbers("DE00 3704 0044 0532 0130 00"), "…3000");
    }

    #[test]
    fn redact_keeps_its_trace_behaviour_for_a_spaced_iban_whose_match_swallowed_the_next_word() {
        assert_eq!(
            redact("Konto ES91 2100 0418 4502 0005 1332 EUR ok", &[]),
            format!("Konto {MASK} EUR ok")
        );
    }

    #[test]
    fn mask_account_numbers_masks_long_digit_runs_keeping_the_last_four() {
        assert_eq!(
            mask_account_numbers("Konto 1234567890 gesperrt"),
            "Konto …7890 gesperrt"
        );
    }

    #[test]
    fn mask_account_numbers_leaves_return_codes_dates_amounts_and_short_numbers_alone() {
        assert_eq!(
            mask_account_numbers("9050 Auftrag abgelehnt am 23.10.2026 über 12,34 EUR"),
            "9050 Auftrag abgelehnt am 23.10.2026 über 12,34 EUR"
        );
        assert_eq!(mask_account_numbers("Betrag 10000 EUR"), "Betrag 10000 EUR");
        assert_eq!(mask_account_numbers("Jahr 2026"), "Jahr 2026");
    }

    #[test]
    fn mask_account_numbers_masks_labelled_digit_groups_keeping_the_last_four() {
        assert_eq!(mask_account_numbers("Konto 9876 5432"), "Konto …5432");
        assert_eq!(mask_account_numbers("Kto.-Nr. 1234 5678 90"), "Kto.-Nr. …7890");
        assert_eq!(
            mask_account_numbers("Empfängerkonto 1234 567890"),
            "Empfängerkonto …7890"
        );
        assert_eq!(
            mask_account_numbers("Girokonto 9876 5432 gesperrt"),
            "Girokonto …5432 gesperrt"
        );
        assert_eq!(mask_account_numbers("Kontonummer 1234567890"), "Kontonummer …7890");
        assert_eq!(mask_account_numbers("Account no. 9876 5432"), "Account no. …5432");
        assert_eq!(mask_account_numbers("Kto 1234 5678."), "Kto …5678.");
    }

    #[test]
    fn mask_account_numbers_leaves_unlabelled_digit_groups_alone() {
        assert_eq!(
            mask_account_numbers("Kontostand 1 234,56 EUR"),
            "Kontostand 1 234,56 EUR"
        );
        assert_eq!(mask_account_numbers("Konto 123 456,78 EUR"), "Konto 123 456,78 EUR");
        assert_eq!(mask_account_numbers("Konto: 12 34"), "Konto: 12 34");
        assert_eq!(mask_account_numbers("im Oktober 1234 5678"), "im Oktober 1234 5678");
        assert_eq!(mask_account_numbers("24.10.2026"), "24.10.2026");
        assert_eq!(mask_account_numbers("80331 München"), "80331 München");
        assert_eq!(
            mask_account_numbers("9050 3076 Auftrag abgelehnt"),
            "9050 3076 Auftrag abgelehnt"
        );
    }

    #[test]
    fn return_codes_still_find_codes_next_to_each_other_after_masking() {
        assert_eq!(
            return_codes(Some(&mask_account_numbers("9050 3076 Auftrag abgelehnt"))),
            vec!["9050", "3076"]
        );
        assert_eq!(
            return_codes(Some(&mask_account_numbers("Konto 9876 5432: 9050 Auftrag abgelehnt"))),
            vec!["9050"]
        );
    }

    #[test]
    fn mask_account_numbers_is_idempotent_for_labelled_groups_and_ibans() {
        for text in ["Konto 9876 5432", "IBAN DE89 3704 0044 0532 0130 00"] {
            let once = mask_account_numbers(text);
            assert_eq!(once, mask_account_numbers(&once));
        }
    }
}
