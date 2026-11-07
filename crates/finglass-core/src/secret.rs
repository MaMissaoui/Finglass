//! Passphrase handling.
//!
//! A passphrase is normalized to Unicode NFC once, validated for its purpose, and then kept in a [`SecretString`]
//! so it is wiped on drop and never appears in `Debug` output.

use secrecy::{ExposeSecret, SecretString};
use thiserror::Error;
use unicode_normalization::UnicodeNormalization;
use zeroize::Zeroizing;

/// The minimum length for a new passphrase, counted in characters after NFC normalization.
const MIN_NEW_LEN: usize = 10;

/// Why a passphrase is being supplied.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PassphrasePurpose {
    /// Opening an existing database. Only non-empty input is required.
    Unlock,
    /// Creating a database or changing its passphrase. FinDesk's rules apply.
    New,
}

/// Errors while building a [`Passphrase`]. Their messages never contain the input.
#[derive(Debug, Error, PartialEq, Eq)]
pub enum PassphraseError {
    /// The input was empty.
    #[error("passphrase must not be empty")]
    Empty,

    /// The input was shorter than the minimum.
    #[error("passphrase must be at least {min} characters")]
    TooShort {
        /// The minimum number of characters.
        min: usize,
    },

    /// The input started or ended with whitespace.
    #[error("passphrase must not start or end with whitespace")]
    SurroundingWhitespace,
}

/// A validated passphrase, stored as a secret.
pub struct Passphrase(SecretString);

impl Passphrase {
    /// Normalizes and validates `input`.
    pub fn new(input: &str, purpose: PassphrasePurpose) -> Result<Self, PassphraseError> {
        // Build the normalized text inside `Zeroizing` so the intermediate String is wiped when it drops.
        let normalized: Zeroizing<String> = Zeroizing::new(input.nfc().collect());

        if normalized.is_empty() {
            return Err(PassphraseError::Empty);
        }

        if purpose == PassphrasePurpose::New {
            if normalized.chars().count() < MIN_NEW_LEN {
                return Err(PassphraseError::TooShort { min: MIN_NEW_LEN });
            }

            let starts_or_ends_with_whitespace = normalized.chars().next().is_some_and(char::is_whitespace)
                || normalized.chars().next_back().is_some_and(char::is_whitespace);

            if starts_or_ends_with_whitespace || normalized.chars().all(char::is_whitespace) {
                return Err(PassphraseError::SurroundingWhitespace);
            }
        }

        // The `&str` is copied into the secret; the `Zeroizing<String>` is wiped when this function returns.
        Ok(Self(SecretString::from(normalized.as_str())))
    }

    /// Exposes the passphrase to the database module only.
    pub(crate) fn expose(&self) -> &str {
        self.0.expose_secret()
    }
}

impl std::fmt::Debug for Passphrase {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("Passphrase(***)")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nfc_normalizes_both_forms_to_the_same_value() {
        let decomposed = Passphrase::new("e\u{301}", PassphrasePurpose::Unlock).unwrap();
        let composed = Passphrase::new("\u{e9}", PassphrasePurpose::Unlock).unwrap();
        assert_eq!(decomposed.expose(), composed.expose());
        assert_eq!(decomposed.expose(), "\u{e9}");
    }

    #[test]
    fn empty_input_is_rejected_for_both_purposes() {
        assert_eq!(
            Passphrase::new("", PassphrasePurpose::Unlock).unwrap_err(),
            PassphraseError::Empty
        );
        assert_eq!(
            Passphrase::new("", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::Empty
        );
    }

    #[test]
    fn new_rejects_short_input_in_characters() {
        // Nine characters, even though the byte length is larger.
        assert_eq!(
            Passphrase::new("aaaaaaaaa", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::TooShort { min: 10 }
        );
        // Nine multi-byte characters are still nine characters.
        assert_eq!(
            Passphrase::new("ééééééééé", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::TooShort { min: 10 }
        );
        assert!(Passphrase::new("éééééééééé", PassphrasePurpose::New).is_ok());
    }

    #[test]
    fn new_rejects_surrounding_whitespace() {
        assert_eq!(
            Passphrase::new(" correct horse ", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::SurroundingWhitespace
        );
        assert_eq!(
            Passphrase::new("\tcorrect horse", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::SurroundingWhitespace
        );
        assert_eq!(
            Passphrase::new("           ", PassphrasePurpose::New).unwrap_err(),
            PassphraseError::SurroundingWhitespace
        );
    }

    #[test]
    fn unlock_accepts_a_short_passphrase() {
        let p = Passphrase::new("x", PassphrasePurpose::Unlock).unwrap();
        assert_eq!(p.expose(), "x");
    }

    #[test]
    fn debug_does_not_contain_the_passphrase() {
        let p = Passphrase::new("secret-horse-battery", PassphrasePurpose::New).unwrap();
        let debug = format!("{p:?}");
        assert_eq!(debug, "Passphrase(***)");
        assert!(!debug.contains("secret-horse-battery"));
    }

    #[test]
    fn errors_do_not_contain_the_input() {
        let input = " my secret passphrase ";
        for err in [
            Passphrase::new(input, PassphrasePurpose::New).unwrap_err(),
            Passphrase::new("short", PassphrasePurpose::New).unwrap_err(),
            Passphrase::new("", PassphrasePurpose::New).unwrap_err(),
        ] {
            assert!(!format!("{err}").contains("secret"));
            assert!(!format!("{err:?}").contains("secret"));
        }
    }
}
