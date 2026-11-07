//! Questions the bank asks during a dialog, ported from FinDesk's `BankingInteraction.kt`.

use secrecy::SecretString;

/// One TAN procedure offered by the bank.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TanMethod {
    /// The procedure id.
    pub id: String,
    /// The name shown to the user.
    pub name: String,
    /// The TAN medium, when the bank names one.
    pub medium_name: Option<String>,
    /// The TAN format, when the bank names one.
    pub tan_format: Option<String>,
}

/// The PIN the user entered. `remember` is honoured only for syncs.
#[derive(Clone)]
pub struct PinEntry {
    pin: SecretString,
    remember: bool,
}

impl PinEntry {
    /// Builds an entry from the user's input.
    pub fn new(pin: SecretString, remember: bool) -> Self {
        Self { pin, remember }
    }

    /// The secret PIN.
    pub fn pin(&self) -> &SecretString {
        &self.pin
    }

    /// Whether the user ticked "Remember PIN".
    pub fn remember(&self) -> bool {
        self.remember
    }

    /// Whether the user submitted an empty PIN.
    pub fn is_empty(&self) -> bool {
        secrecy::ExposeSecret::expose_secret(&self.pin).is_empty()
    }
}

impl std::fmt::Debug for PinEntry {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "PinEntry(remember={})", self.remember)
    }
}

/// A TAN challenge from the bank.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TanChallenge {
    /// The bank's message (the challenge text).
    pub message: String,
    /// A photoTAN or QR-TAN image, when the bank sent one.
    pub image: Option<Vec<u8>>,
    /// The image's MIME type.
    pub image_mime_type: Option<String>,
    /// A chipTAN flicker code, when the bank sent one.
    pub flicker_code: Option<String>,
}

/// Questions the bank asks during a FinTS dialog.
///
/// The banking code calls these from a background thread and blocks until the user answers; returning `None` (or
/// `false`) cancels the operation.
pub trait BankingInteraction {
    /// The online-banking PIN. The PIN is only kept when the user opts in, and only for syncs.
    fn pin(&self, bank_name: &str, user_id: &str, offer_remember: bool) -> Option<PinEntry>;

    /// Picks one of the TAN procedures the bank offers.
    fn select_tan_method(&self, options: &[TanMethod]) -> Option<String>;

    /// Picks a TAN medium (for example a registered phone or TAN generator).
    fn select_tan_medium(&self, options: &[String]) -> Option<String>;

    /// Enters a TAN for a challenge. The answer stays a secret for its whole life.
    fn tan(&self, challenge: &TanChallenge) -> Option<SecretString>;

    /// Decoupled procedures (for example pushTAN): the user confirms in the app and the dialog continues.
    fn confirm_decoupled(&self, message: &str) -> bool;

    /// An informational message from the bank.
    fn info(&self, _message: &str) {}
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pin_entry_debug_never_contains_the_pin() {
        let entry = PinEntry::new(SecretString::from("987654"), true);
        let debug = format!("{entry:?}");
        assert_eq!(debug, "PinEntry(remember=true)");
        assert!(!debug.contains("987654"));
    }

    #[test]
    fn a_tan_method_keeps_its_data() {
        let method = TanMethod {
            id: "942".to_owned(),
            name: "chipTAN optisch".to_owned(),
            medium_name: Some("TAN-Generator".to_owned()),
            tan_format: None,
        };
        assert_eq!(method.id, "942");
        assert_eq!(method.name, "chipTAN optisch");
        assert_eq!(method.medium_name.as_deref(), Some("TAN-Generator"));
    }
}
