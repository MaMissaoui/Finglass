//! Which bundled bank logo an account shows, and the monogram for banks without one.
//!
//! The matching rules are shared with FinDesk. They only inspect the bank's name, BLZ and BIC,
//! so the function is pure and has no UI types. The SVG assets live in the UI crate; the id of a
//! [`BankLogo`] is the SVG's file stem there.

/// One of the nine bundled bank logos.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum BankLogo {
    /// HypoVereinsbank / UniCredit.
    HypoVereinsbank,
    /// DKB (Deutsche Kreditbank).
    Dkb,
    /// Postbank.
    Postbank,
    /// Deutsche Bank.
    DeutscheBank,
    /// Commerzbank.
    Commerzbank,
    /// Santander.
    Santander,
    /// N26.
    N26,
    /// The Sparkassen.
    Sparkasse,
    /// The Volksbanken Raiffeisenbanken.
    Volksbank,
}

impl BankLogo {
    /// The file stem of the bundled SVG in `crates/finglass-ui/ui/banks/`.
    #[must_use]
    pub fn id(&self) -> &'static str {
        match self {
            Self::HypoVereinsbank => "hypovereinsbank",
            Self::Dkb => "dkb",
            Self::Postbank => "postbank",
            Self::DeutscheBank => "deutsche-bank",
            Self::Commerzbank => "commerzbank",
            Self::Santander => "santander",
            Self::N26 => "n26",
            Self::Sparkasse => "sparkasse",
            Self::Volksbank => "volksbank",
        }
    }
}

/// The logo for a bank, or `None` when it gets a monogram.
///
/// Matching is case-insensitive and trims the inputs. The first matching rule wins; the order is
/// part of the shared rules because several brands use another bank's BLZ range or BIC.
#[must_use]
pub fn bank_logo(name: &str, blz: Option<&str>, bic: Option<&str>) -> Option<BankLogo> {
    let name = name.trim().to_lowercase();
    let blz = blz.map(str::trim).unwrap_or_default();
    let bic = bic.map(str::trim).unwrap_or_default().to_ascii_uppercase();

    // 1. These brands use another bank's BIC or BLZ range but have their own look.
    if ["comdirect", "consors", "targo", "sparda", "psd", "apobank", "apotheker"]
        .iter()
        .any(|brand| name.contains(brand))
    {
        return None;
    }
    // 2. HypoVereinsbank / UniCredit.
    if name.contains("hypovereinsbank") || name.contains("hvb") || name.contains("unicredit") || bic.starts_with("HYVE")
    {
        return Some(BankLogo::HypoVereinsbank);
    }
    // 3. DKB.
    if name.contains("dkb")
        || name.contains("deutsche kreditbank")
        || bic.starts_with("BYLADEM1001")
        || blz == "12030000"
    {
        return Some(BankLogo::Dkb);
    }
    // 4. Postbank.
    if name.contains("postbank") || bic.starts_with("PBNK") {
        return Some(BankLogo::Postbank);
    }
    // 5. Deutsche Bank.
    if name.contains("deutsche bank") || bic.starts_with("DEUT") {
        return Some(BankLogo::DeutscheBank);
    }
    // 6. Commerzbank.
    if name.contains("commerzbank") || bic.starts_with("COBA") {
        return Some(BankLogo::Commerzbank);
    }
    // 7. Santander.
    if name.contains("santander") || bic.starts_with("SCFB") {
        return Some(BankLogo::Santander);
    }
    // 8. N26.
    if name.contains("n26") || bic.starts_with("NTSB") {
        return Some(BankLogo::N26);
    }
    // 9. Sparkasse: by name, or by the BLZ's fourth digit.
    if name.contains("sparkasse") || blz.as_bytes().get(3) == Some(&b'5') {
        return Some(BankLogo::Sparkasse);
    }
    // 10. Volksbank / Raiffeisenbank.
    if name.contains("volksbank")
        || name.contains("raiffeisenbank")
        || name.contains("vr bank")
        || name.contains("vr-bank")
        || name.contains("vrbank")
        || bic.starts_with("GENO")
    {
        return Some(BankLogo::Volksbank);
    }
    // 11. Everything else gets a monogram.
    None
}

/// The monogram fallback: up to three letters and a stable colour index in `0..8`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Monogram {
    /// The letters drawn on the tile.
    pub letters: String,
    /// The index into the theme's eight monogram colours.
    pub colour: u8,
}

/// Builds the monogram for a bank name.
///
/// A short all-uppercase word such as "HVB", "DKB" or "ING" wins; otherwise the initials of the
/// first two words, or the first two letters of a single word. The colour is a sum of the
/// lower-cased name's UTF-8 bytes modulo eight, so the same name always gets the same tile.
#[must_use]
pub fn monogram(name: &str) -> Monogram {
    let letters = monogram_letters(name);
    let sum = name.to_lowercase().bytes().map(usize::from).sum::<usize>();
    let colour = u8::try_from(sum % 8).unwrap_or_default();
    Monogram { letters, colour }
}

fn monogram_letters(name: &str) -> String {
    let words: Vec<&str> = name.split_whitespace().collect();

    if let Some(word) = words.iter().find(|word| {
        let mut chars = word.chars();
        let Some(first) = chars.next() else {
            return false;
        };
        let length = word.chars().count();
        (2..=4).contains(&length) && first.is_alphabetic() && word.chars().all(|c| c.is_uppercase())
    }) {
        return word.chars().take(3).collect();
    }

    let starting_with_letter: Vec<&str> = words
        .iter()
        .copied()
        .filter(|word| word.chars().next().is_some_and(char::is_alphabetic))
        .collect();

    match starting_with_letter.as_slice() {
        [] => String::new(),
        [only] => only
            .chars()
            .filter(|character| character.is_alphabetic())
            .take(2)
            .map(uppercase)
            .collect(),
        [first, second, ..] => [*first, *second]
            .into_iter()
            .filter_map(|word| word.chars().find(|character| character.is_alphabetic()))
            .map(uppercase)
            .collect(),
    }
}

fn uppercase(character: char) -> char {
    character.to_uppercase().next().unwrap_or(character)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn logo(name: &str, blz: Option<&str>, bic: Option<&str>) -> Option<&'static str> {
        bank_logo(name, blz, bic).map(|logo| logo.id())
    }

    #[test]
    fn brand_exclusions_win_before_every_logo_rule() {
        assert_eq!(logo("comdirect bank", Some("37040044"), Some("COBADEHDXXX")), None);
        assert_eq!(logo("Consorsbank", Some("76026000"), Some("CSDBDE71XXX")), None);
        assert_eq!(logo("Targobank", Some("30020900"), Some("CMCIDEDDXXX")), None);
        assert_eq!(logo("Sparda-Bank", Some("50090500"), Some("GENODEF1S12")), None);
        assert_eq!(logo("PSD Bank", None, None), None);
        assert_eq!(logo("APO Bank", None, None), None);
        assert_eq!(logo("Apothekerbank", None, None), None);
        // The exclusion comes first even when a later rule would match.
        assert_eq!(logo("Sparda-Bank", None, Some("HYVEDEMMXXX")), None);
    }

    #[test]
    fn hypovereinsbank_matches_by_name_and_bic() {
        assert_eq!(
            logo(
                "UniCredit Bank - HypoVereinsbank AG",
                Some("70020270"),
                Some("HYVEDEMMXXX")
            ),
            Some("hypovereinsbank")
        );
        assert_eq!(logo("HVB Mastercard", None, None), Some("hypovereinsbank"));
        assert_eq!(logo("Some Bank", None, Some("HYVEDEMMXXX")), Some("hypovereinsbank"));
    }

    #[test]
    fn dkb_matches_by_name_bic_and_blz() {
        assert_eq!(logo("DKB Privat", Some("12030000"), Some("BYLADEM1001")), Some("dkb"));
        assert_eq!(logo("Deutsche Kreditbank AG", None, None), Some("dkb"));
        assert_eq!(logo("Some Bank", None, Some("BYLADEM1001")), Some("dkb"));
        assert_eq!(logo("Some Bank", Some("12030000"), None), Some("dkb"));
    }

    #[test]
    fn the_remaining_logo_rules_match_by_name_or_bic() {
        assert_eq!(logo("Postbank", None, None), Some("postbank"));
        assert_eq!(logo("Some Bank", None, Some("PBNKDEFFXXX")), Some("postbank"));

        assert_eq!(logo("Deutsche Bank", None, None), Some("deutsche-bank"));
        assert_eq!(logo("Some Bank", None, Some("DEUTDEFFXXX")), Some("deutsche-bank"));

        assert_eq!(logo("Commerzbank", None, None), Some("commerzbank"));
        assert_eq!(logo("Some Bank", None, Some("COBADEFFXXX")), Some("commerzbank"));

        assert_eq!(logo("Santander", None, None), Some("santander"));
        assert_eq!(logo("Some Bank", None, Some("SCFBDEFFXXX")), Some("santander"));

        assert_eq!(logo("N26", None, None), Some("n26"));
        assert_eq!(logo("Some Bank", None, Some("NTSBDEB1XXX")), Some("n26"));
    }

    #[test]
    fn sparkasse_matches_by_name_and_the_fourth_blz_digit() {
        assert_eq!(logo("Sparkasse KölnBonn", None, None), Some("sparkasse"));
        assert_eq!(logo("Some Bank", Some("45050001"), None), Some("sparkasse"));
        assert_eq!(logo("Some Bank", Some("12355001"), None), Some("sparkasse"));
        assert_eq!(logo("Some Bank", Some("12340001"), None), None);
    }

    #[test]
    fn volksbank_matches_by_name_and_the_geno_bic() {
        assert_eq!(
            logo("Volksbank Mittelhessen", Some("51390000"), Some("VBMHDE5FXXX")),
            Some("volksbank")
        );
        assert_eq!(logo("Raiffeisenbank", None, None), Some("volksbank"));
        assert_eq!(logo("VR Bank Südpfalz", None, None), Some("volksbank"));
        assert_eq!(logo("VR-Bank", None, None), Some("volksbank"));
        assert_eq!(logo("VRBank", None, None), Some("volksbank"));
        assert_eq!(logo("Some Bank", None, Some("GENODEF1S12")), Some("volksbank"));
    }

    #[test]
    fn unknown_banks_get_no_logo() {
        assert_eq!(logo("Bank Norwegian", None, None), None);
        assert_eq!(logo("BayernLB", None, Some("BYLADEMMXXX")), None);
        assert_eq!(logo("", None, None), None);
    }

    #[test]
    fn matching_trims_and_ignores_case() {
        assert_eq!(logo("  dkb  ", Some(" 12030000 "), Some(" byladm1001 ")), Some("dkb"));
        assert_eq!(logo("  SPARKASSE  ", None, None), Some("sparkasse"));
        assert_eq!(logo("  volksbank  ", None, None), Some("volksbank"));
    }

    #[test]
    fn the_first_matching_rule_wins() {
        assert_eq!(logo("DKB", None, Some("HYVEDEMMXXX")), Some("hypovereinsbank"));
        assert_eq!(logo("Commerzbank", Some("12030000"), None), Some("dkb"));
    }

    #[test]
    fn monogram_uses_a_short_uppercase_word_first() {
        assert_eq!(monogram("HVB Mastercard").letters, "HVB");
        assert_eq!(monogram("DKB Privat").letters, "DKB");
        assert_eq!(monogram("ING").letters, "ING");
    }

    #[test]
    fn monogram_otherwise_uses_initials_or_the_first_two_letters() {
        assert_eq!(monogram("Bank Norwegian").letters, "BN");
        assert_eq!(monogram("Sparkasse").letters, "SP");
        assert_eq!(monogram("ING-DiBa").letters, "IN");
        assert_eq!(monogram("  ").letters, "");
    }

    #[test]
    fn monogram_colour_is_stable_and_in_range() {
        let first = monogram("Bank Norwegian");
        let second = monogram("Bank Norwegian");
        assert_eq!(first, second);
        assert_eq!(first.colour, monogram("bank norwegian").colour);
        assert!(first.colour < 8);
        // The sum of the lower-cased UTF-8 bytes modulo 8, not a randomized hash.
        let expected = "bank norwegian".bytes().map(usize::from).sum::<usize>() % 8;
        assert_eq!(usize::from(first.colour), expected);
    }
}
