//! Finglass-owned dialog failures, ported from FinDesk's `BankingFailures.kt`.
//!
//! Bank message text can contain account numbers, so descriptions and audit-facing text always pass through the
//! redactor. Audit details carry return codes only, never a bank message.

use thiserror::Error;

use crate::i18n::{self, Arg, Key, Language};

use super::redactor;

/// A failed FinTS dialog, as the M5b adapter reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GatewayError {
    /// The server could not be reached.
    Network,
    /// The server answered with an HTTP status.
    HttpStatus(u16),
    /// The bank rejected the request with these return codes. Never a bank message.
    Bank {
        /// The return codes (for example `997` or `9050`).
        codes: Vec<String>,
    },
    /// The bank rejected the PIN.
    PinRejected,
    /// The user cancelled a prompt.
    Cancelled,
    /// Anything else. The text is treated as untrusted bank or transport text.
    Other(String),
}

impl GatewayError {
    /// The HTTP status the server answered with, when this is an HTTP failure.
    pub fn http_status(&self) -> Option<u16> {
        match self {
            Self::HttpStatus(status) => Some(*status),
            _ => None,
        }
    }

    /// Whether this is a connectivity problem.
    pub fn is_network_failure(&self) -> bool {
        match self {
            Self::Network => true,
            Self::Other(text) => is_network_text(text),
            _ => false,
        }
    }

    /// The bank's return codes, when it sent any.
    pub fn codes(&self) -> &[String] {
        match self {
            Self::Bank { codes } => codes,
            _ => &[],
        }
    }

    /// A short message for a failed dialog, as FinDesk builds it.
    ///
    /// Text from the error and the bank messages is cut at the first line break, capped at 300 characters and masked
    /// with [`redactor::mask_account_numbers`]. A base64 failure and an HTTP status are transport diagnoses; a bank
    /// message outranks the generic text.
    pub fn describe(&self, language: Language, bank_messages: &[String]) -> String {
        if let Self::Other(text) = self
            && text.to_lowercase().contains("base64")
            && let Some(line) = clean_line(text)
        {
            return i18n::format(
                language,
                Key::ErrorServerNotFints,
                &[Arg::Str(redactor::mask_account_numbers(&line))],
            );
        }

        if let Some(status) = self.http_status() {
            return i18n::format(language, Key::ErrorServerHttpStatus, &[Arg::from(i32::from(status))]);
        }

        if let Some(message) = bank_messages.iter().rev().find_map(|message| clean_line(message)) {
            return redactor::mask_account_numbers(&message);
        }

        match self {
            Self::Network => self.network_message(language),
            Self::PinRejected => i18n::text(language, Key::ErrorBankRejectedPin).to_owned(),
            Self::Cancelled => i18n::text(language, Key::ErrorCancelledByUser).to_owned(),
            Self::Bank { codes } if !codes.is_empty() => {
                let codes = codes.join(", ");
                format!("{} ({codes})", i18n::text(language, Key::ErrorBankRejected))
            }
            Self::Bank { .. } => i18n::text(language, Key::ErrorBankRejected).to_owned(),
            Self::HttpStatus(_) => unreachable!("handled above"),
            Self::Other(text) => match clean_line(text) {
                Some(line) => redactor::mask_account_numbers(&line),
                None => i18n::text(language, Key::ErrorDialogFailed).to_owned(),
            },
        }
    }

    /// [`Key::ErrorBankUnreachable`] plus the innermost first line, for example "(Connection reset)".
    pub fn network_message(&self, language: Language) -> String {
        let base = i18n::text(language, Key::ErrorBankUnreachable);
        match self {
            Self::Other(text) => match clean_line(text) {
                Some(line) => format!("{base} ({})", redactor::mask_account_numbers(&line)),
                None => base.to_owned(),
            },
            _ => base.to_owned(),
        }
    }
}

/// The user-visible failure of a service call.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
#[error("{message}")]
pub struct BankingError {
    /// A stable code the UI can branch on (`CANCELLED`, `FINTS_ERROR`, `OFFLINE`, …).
    pub code: String,
    /// The message shown to the user. Never contains a PIN, TAN or full account number.
    pub message: String,
}

impl BankingError {
    /// Builds an error from a code and a message.
    pub fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
        }
    }
}

/// A service or dialog result.
pub type BankResult<T> = Result<T, BankingError>;

const NETWORK_MARKERS: &[&str] = &[
    "sending message to hbci server",
    "senden der hbci-nachricht",
    "connection refused",
    "connect timed out",
    "connection timed out",
    "connection reset",
    "network is unreachable",
    "no route to host",
    "unknownhostexception",
    "connectexception",
    "sockettimeoutexception",
    "sslexception",
    "handshakeexception",
];

fn is_network_text(text: &str) -> bool {
    let lower = text.to_lowercase();
    NETWORK_MARKERS.iter().any(|marker| lower.contains(marker))
}

/// The first meaningful line, capped at 300 characters.
fn clean_line(text: &str) -> Option<String> {
    let line = text.lines().next()?.trim();
    if line.is_empty() || line.starts_with("at ") || line.starts_with("\tat ") || line.starts_with("Caused by") {
        return None;
    }
    Some(line.chars().take(300).collect())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_socket_text_is_a_network_failure() {
        assert!(GatewayError::Network.is_network_failure());
        assert!(GatewayError::Other("java.net.ConnectException: Connection refused".to_owned()).is_network_failure());
        assert!(!GatewayError::Other("997 Falscher PIN".to_owned()).is_network_failure());
    }

    #[test]
    fn a_base64_failure_names_the_answer_that_is_not_fints() {
        let error = GatewayError::Other("Illegal base64 character 3c".to_owned());
        let message = error.describe(Language::En, &[]);
        assert!(message.contains("Illegal base64 character 3c"), "{message}");
    }

    #[test]
    fn an_illegal_argument_without_base64_is_not_called_a_non_fints_answer() {
        let error = GatewayError::Other("invalid value for BTG.value".to_owned());
        let message = error.describe(Language::En, &[]);
        assert!(!message.contains("isn't FinTS"), "{message}");
        assert_eq!(message, "invalid value for BTG.value");
    }

    #[test]
    fn an_http_status_wins_over_the_network_branch() {
        let error = GatewayError::HttpStatus(403);
        assert_eq!(error.http_status(), Some(403));
        let message = error.describe(Language::En, &[]);
        assert!(message.contains("HTTP 403"), "{message}");
        assert!(!message.contains("Could not reach"), "{message}");
    }

    #[test]
    fn a_bank_message_wins_over_the_generic_text() {
        let error = GatewayError::Bank {
            codes: vec!["997".to_owned()],
        };
        assert_eq!(
            error.describe(Language::En, &["997 Falscher PIN".to_owned()]),
            "997 Falscher PIN"
        );
    }

    #[test]
    fn a_bank_message_is_redacted() {
        let error = GatewayError::Bank {
            codes: vec!["3016".to_owned()],
        };
        let message = error.describe(Language::En, &["Konto DE89370400440532013000 unbekannt".to_owned()]);
        assert!(!message.contains("DE89370400440532013000"), "{message}");
        assert!(message.contains("…3000"), "{message}");
    }

    #[test]
    fn a_generic_fallback_keeps_the_first_line_only() {
        let error = GatewayError::Other("Dialog broke\nat org.example.Frame".to_owned());
        let message = error.describe(Language::En, &[]);
        assert_eq!(message, "Dialog broke");
    }

    #[test]
    fn empty_text_falls_back_to_the_dialog_failed_message() {
        let error = GatewayError::Other("\n".to_owned());
        assert_eq!(
            error.describe(Language::En, &[]),
            i18n::text(Language::En, Key::ErrorDialogFailed)
        );
    }

    #[test]
    fn a_network_failure_keeps_the_unreachable_message() {
        let message = GatewayError::Network.network_message(Language::En);
        assert_eq!(message, i18n::text(Language::En, Key::ErrorBankUnreachable));
        let detailed = GatewayError::Other("Connection reset".to_owned()).network_message(Language::En);
        assert!(detailed.ends_with("(Connection reset)"), "{detailed}");
    }

    #[test]
    fn a_cancelled_dialog_uses_the_cancelled_text() {
        assert_eq!(
            GatewayError::Cancelled.describe(Language::En, &[]),
            i18n::text(Language::En, Key::ErrorCancelledByUser)
        );
    }

    #[test]
    fn a_pin_rejection_uses_the_pin_text() {
        assert_eq!(
            GatewayError::PinRejected.describe(Language::En, &[]),
            i18n::text(Language::En, Key::ErrorBankRejectedPin)
        );
    }
}
