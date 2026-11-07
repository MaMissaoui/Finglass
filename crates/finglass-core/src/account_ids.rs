//! Identifiers stored in `accounts.iban`, ported from FinDesk's `util/AccountIds.kt`.
//!
//! An offline bank's account may name a card instead of an IBAN: the synthetic identifier "CARD" plus exactly the
//! card's last 4 digits (e.g. `CARD4321`). A full card number is never stored, so the identifier carries nothing
//! sensitive.

use crate::iban;

/// Prefix of the synthetic identifier of a card account.
pub const CARD_PREFIX: &str = "CARD";

/// Synthetic account identifier of a card account: the prefix plus the 4 given digits.
pub fn card(last4: &str) -> String {
    format!("{CARD_PREFIX}{}", last4.trim())
}

/// Whether `id` names a card account ("CARD" + 4 digits) instead of an IBAN.
pub fn is_card(id: &str) -> bool {
    let Some(tail) = id.strip_prefix(CARD_PREFIX) else {
        return false;
    };
    tail.chars().count() == 4 && tail.chars().all(|character| character.is_ascii_digit())
}

/// Display text of an account identifier: a card's last 4 digits, otherwise the formatted IBAN.
pub fn display(id: &str) -> String {
    if is_card(id) {
        let last4: String = id.chars().rev().take(4).collect::<String>().chars().rev().collect();
        format!("•••• {last4}")
    } else {
        iban::format(id)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const IBAN: &str = "DE89370400440532013000";

    #[test]
    fn a_card_identifier_is_not_an_iban() {
        assert_eq!(card("4321"), "CARD4321");
        assert!(is_card("CARD4321"));
        assert!(!iban::is_valid("CARD4321"), "a card id has no IBAN length");
        assert_eq!(iban::normalize("CARD4321"), "CARD4321", "a card id is left unchanged");
        assert!(!is_card(IBAN));
        assert!(!is_card("CARD432"));
        assert!(!is_card("CARD43219"));
        assert!(!is_card("HEAD12"));
    }

    #[test]
    fn display_shows_a_cards_last_4_digits_and_formats_an_iban_as_before() {
        assert_eq!(display("CARD4321"), "•••• 4321");
        assert_eq!(display(IBAN), iban::format(IBAN));
    }
}
