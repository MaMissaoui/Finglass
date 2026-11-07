//! IBAN normalisation and validation, ported from FinDesk's `util/Iban.kt`.

/// Strips spaces and upper-cases.
pub fn normalize(iban: &str) -> String {
    iban.replace(' ', "").to_uppercase()
}

/// ISO 13616 mod-97 check.
pub fn is_valid(iban: &str) -> bool {
    let normalised = normalize(iban);
    let chars: Vec<char> = normalised.chars().collect();

    if !(15..=34).contains(&chars.len()) || !chars.iter().all(|c| c.is_alphanumeric()) {
        return false;
    }
    if !chars[0].is_alphabetic()
        || !chars[1].is_alphabetic()
        || !chars[2].is_ascii_digit()
        || !chars[3].is_ascii_digit()
    {
        return false;
    }
    if normalised.starts_with("DE") && chars.len() != 22 {
        return false;
    }

    // Moving the first four characters to the end and expanding letters (A = 10, ..., Z = 35)
    // gives a decimal string whose remainder modulo 97 is the checksum. Do it incrementally so
    // no big integer is needed.
    let mut remainder: u32 = 0;
    for ch in chars[4..].iter().chain(chars[..4].iter()) {
        if let Some(digit) = ch.to_digit(10) {
            remainder = (remainder * 10 + digit) % 97;
        } else {
            let value = (*ch as u32).saturating_sub('A' as u32) + 10;
            remainder = (remainder * 100 + value) % 97;
        }
    }

    remainder == 1
}

/// Groups an IBAN in blocks of four, as FinDesk displays it.
pub fn format(iban: &str) -> String {
    let normalised = normalize(iban);
    let chars: Vec<char> = normalised.chars().collect();
    chars
        .chunks(4)
        .map(|chunk| chunk.iter().collect::<String>())
        .collect::<Vec<_>>()
        .join(" ")
}

/// Masks an IBAN (or a synthetic account id) as `…` plus its last four characters.
pub fn mask(iban: &str) -> String {
    let tail: String = iban.chars().rev().take(4).collect::<String>().chars().rev().collect();
    format!("…{tail}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn iban_checksum() {
        assert!(is_valid("DE89 3704 0044 0532 0130 00"));
        assert!(is_valid("GB82WEST12345698765432"));
        assert!(!is_valid("DE89 3704 0044 0532 0130 01"));
        assert!(!is_valid("DE8937040044053201300"), "German IBANs have 22 characters");
        assert!(!is_valid("not an iban"));
    }

    #[test]
    fn a_norwegian_iban_is_valid_and_normalizes_unchanged() {
        // Synthetic identifier from the ISO 3116 test set, no real account
        assert!(is_valid("NO9386011117947"));
        assert_eq!(normalize("NO93 8601 1117 947"), "NO9386011117947");
    }

    #[test]
    fn a_card_identifier_is_not_an_iban() {
        assert!(!is_valid("CARD4321"), "a card id has no IBAN length");
        assert_eq!(normalize("CARD4321"), "CARD4321", "a card id is left unchanged");
    }

    #[test]
    fn an_iban_is_grouped_in_fours_for_display() {
        assert_eq!(format("DE89370400440532013000"), "DE89 3704 0044 0532 0130 00");
        assert_eq!(format("NO93 8601 1117 947"), "NO93 8601 1117 947");
    }

    #[test]
    fn an_iban_is_masked_to_its_last_four_characters() {
        assert_eq!(mask("DE89370400440532013000"), "…3000");
        assert_eq!(mask("CARD4321"), "…4321");
    }
}
