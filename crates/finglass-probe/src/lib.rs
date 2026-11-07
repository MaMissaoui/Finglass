//! Pure, network-free logic for the `finglass-probe` CLI.
//!
//! Everything in this module can be unit-tested without a bank connection.
//! The binary in `main.rs` is responsible for reading secrets and talking to
//! the FinTS endpoint.

use std::fmt;

use fints::banks::bank_by_blz;

/// Environment variable holding the FinTS product registration id.
pub const PRODUCT_ID_VAR: &str = "FINGLASS_FINTS_PRODUCT_ID";
/// Environment variable holding the bank code.
pub const BLZ_VAR: &str = "FINGLASS_PROBE_BLZ";
/// Environment variable holding the online-banking login.
pub const USER_VAR: &str = "FINGLASS_PROBE_USER";
/// Environment variable holding an optional FinTS server URL.
pub const URL_VAR: &str = "FINGLASS_PROBE_URL";
/// Environment variable holding the optional statement period in days.
pub const DAYS_VAR: &str = "FINGLASS_PROBE_DAYS";

const DEFAULT_DAYS: u32 = 30;
const MIN_DAYS: u32 = 1;
const MAX_DAYS: u32 = 90;

/// Validated probe inputs. Deliberately contains no PIN or TAN.
#[derive(Clone)]
pub struct Inputs {
    pub product_id: String,
    pub blz: String,
    pub user: String,
    pub url: String,
    pub days: u32,
}

impl fmt::Debug for Inputs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Inputs")
            .field("product_id", &self.product_id)
            .field("blz", &self.blz)
            // The login must never end up in logs or error output.
            .field("user", &"***")
            .field("url", &self.url)
            .field("days", &self.days)
            .finish()
    }
}

/// A single invalid input. The display text names the variable, never its
/// value, except for the BLZ where the spec allows the value to be shown.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InputError {
    pub variable: &'static str,
    pub message: String,
}

impl InputError {
    fn missing(variable: &'static str) -> Self {
        Self {
            variable,
            message: "is required".to_string(),
        }
    }

    fn invalid(variable: &'static str, message: impl Into<String>) -> Self {
        Self {
            variable,
            message: message.into(),
        }
    }
}

impl fmt::Display for InputError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}: {}", self.variable, self.message)
    }
}

fn non_empty(lookup: &impl Fn(&str) -> Option<String>, name: &str) -> Option<String> {
    lookup(name)
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

/// Parse and validate all inputs.
///
/// `lookup` is the only source of environment values: `main` passes a closure
/// over `std::env::var`, tests pass a closure over a `HashMap`.
pub fn parse_inputs(lookup: impl Fn(&str) -> Option<String>) -> Result<Inputs, Vec<InputError>> {
    let mut errors = Vec::new();

    let product_id = match non_empty(&lookup, PRODUCT_ID_VAR) {
        Some(value) => value,
        None => {
            errors.push(InputError::missing(PRODUCT_ID_VAR));
            String::new()
        }
    };

    let blz = match non_empty(&lookup, BLZ_VAR) {
        Some(value) => value,
        None => {
            errors.push(InputError::missing(BLZ_VAR));
            String::new()
        }
    };

    let blz_is_valid = blz.len() == 8 && blz.chars().all(|c| c.is_ascii_digit());
    if !blz.is_empty() && !blz_is_valid {
        errors.push(InputError::invalid(
            BLZ_VAR,
            format!("invalid BLZ '{blz}' (expected exactly 8 digits)"),
        ));
    }

    let user = match non_empty(&lookup, USER_VAR) {
        Some(value) => value,
        None => {
            errors.push(InputError::missing(USER_VAR));
            String::new()
        }
    };

    let explicit_url = non_empty(&lookup, URL_VAR);
    let url = match explicit_url {
        Some(url) => Some(url),
        // An unknown BLZ without an explicit URL is an error. Skip the lookup
        // when the BLZ itself is malformed so we do not report a second,
        // misleading problem.
        None if blz_is_valid => bank_by_blz(&blz).map(|bank| bank.url.to_string()),
        None => None,
    };
    let url = match url {
        Some(url) => url,
        None => {
            if blz_is_valid {
                errors.push(InputError::invalid(
                    URL_VAR,
                    format!("required: BLZ '{blz}' is not in the bank registry and no URL was given"),
                ));
            }
            String::new()
        }
    };

    let days = match non_empty(&lookup, DAYS_VAR) {
        Some(raw) => match raw.parse::<u32>() {
            Ok(value) if (MIN_DAYS..=MAX_DAYS).contains(&value) => value,
            _ => {
                errors.push(InputError::invalid(
                    DAYS_VAR,
                    format!("must be a whole number from {MIN_DAYS} to {MAX_DAYS}"),
                ));
                DEFAULT_DAYS
            }
        },
        None => DEFAULT_DAYS,
    };

    if errors.is_empty() {
        Ok(Inputs {
            product_id,
            blz,
            user,
            url,
            days,
        })
    } else {
        Err(errors)
    }
}

/// Mask an IBAN, keeping the country prefix and the last four non-space
/// characters, and preserving the original grouping.
pub fn mask_iban(iban: &str) -> String {
    let total = iban.chars().filter(|c| !c.is_whitespace()).count();
    let keep_from = if total < 4 { total } else { total - 4 };

    let leading_letters = iban
        .chars()
        .filter(|c| !c.is_whitespace())
        .take_while(|c| c.is_ascii_alphabetic())
        .count();
    // Keep the two-letter country prefix when it does not overlap the last
    // four characters. Short values are fully masked.
    let prefix_count = if total > 4 && leading_letters < keep_from {
        leading_letters
    } else {
        0
    };

    let mut seen = 0usize;
    let mut masked = String::with_capacity(iban.len());
    for ch in iban.chars() {
        if ch.is_whitespace() {
            masked.push(ch);
        } else {
            if seen < prefix_count || seen >= keep_from {
                masked.push(ch);
            } else {
                masked.push('*');
            }
            seen += 1;
        }
    }
    masked
}

/// Whether a TAN method needs an optical/photographic process the probe cannot
/// render. The name comes straight from the bank's HITANS data.
pub fn is_unsupported_tan_method(name: &str) -> bool {
    let name = name.to_ascii_lowercase();
    let optical = name.contains("flicker") || name.contains("photo");
    // chipTAN with manual code entry is a typed TAN; the optical variants are
    // not. Treat the generic chipTAN name as optical unless it says manual.
    let chip = name.contains("chiptan") && !name.contains("manual") && !name.contains("manuell");
    optical || chip
}

/// Result of a statement request for one account.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StatementState {
    Available {
        booking_count: usize,
        earliest: Option<String>,
        latest: Option<String>,
    },
    Unavailable,
}

impl StatementState {
    pub fn is_available(&self) -> bool {
        matches!(self, Self::Available { .. })
    }
}

/// Plain, printable summary for one account. It stores no full IBAN.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountSummary {
    pub iban_masked: String,
    pub currency: String,
    pub balance_present: bool,
    pub statement: StatementState,
}

impl AccountSummary {
    pub fn new(iban: &str, currency: impl Into<String>, balance_present: bool, statement: StatementState) -> Self {
        Self {
            iban_masked: mask_iban(iban),
            currency: currency.into(),
            balance_present,
            statement,
        }
    }
}

/// A NO-GO reason category. Error kinds only ever carry the enum variant name,
/// never a bank message.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NoGoReason {
    LoginRejected,
    TanMethodUnsupported,
    TanTimeout,
    StatementsUnavailable,
    Other(&'static str),
}

impl NoGoReason {
    pub fn label(self) -> String {
        match self {
            Self::LoginRejected => "login rejected".to_string(),
            Self::TanMethodUnsupported => "TAN method unsupported".to_string(),
            Self::TanTimeout => "TAN timeout".to_string(),
            Self::StatementsUnavailable => "statements unavailable".to_string(),
            Self::Other(kind) => format!("other: {kind}"),
        }
    }
}

/// The final probe decision.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Decision {
    Go,
    NoGo(NoGoReason),
}

impl Decision {
    pub fn exit_code(self) -> i32 {
        match self {
            Self::Go => 0,
            Self::NoGo(_) => 1,
        }
    }
}

/// Format one account line. The IBAN is already masked in `AccountSummary`.
pub fn summary_line(summary: &AccountSummary) -> String {
    let balance = if summary.balance_present { "present" } else { "absent" };
    let statements = match &summary.statement {
        StatementState::Unavailable => "statements unavailable".to_string(),
        StatementState::Available {
            booking_count,
            earliest,
            latest,
        } => {
            let range = match (earliest, latest) {
                (Some(earliest), Some(latest)) => format!("{earliest}..{latest}"),
                _ => "no bookings".to_string(),
            };
            format!("statements {booking_count} bookings ({range}) MT940")
        }
    };

    format!(
        "IBAN {} | currency {} | balance {} | {}",
        summary.iban_masked, summary.currency, balance, statements
    )
}

/// Decide GO/NO-GO from plain summaries and an optional failure reason.
///
/// A failure reason always wins. Otherwise every account must have a balance
/// and a statement response. An absent balance without a more specific error
/// is reported as `other: BalanceAbsent`.
pub fn decide(summaries: &[AccountSummary], failure: Option<NoGoReason>) -> Decision {
    if let Some(reason) = failure {
        return Decision::NoGo(reason);
    }

    for summary in summaries {
        if !summary.balance_present {
            return Decision::NoGo(NoGoReason::Other("BalanceAbsent"));
        }
        if !summary.statement.is_available() {
            return Decision::NoGo(NoGoReason::StatementsUnavailable);
        }
    }

    Decision::Go
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    const GERMAN_IBAN: &str = "DE89 3704 0044 0532 0130 00";
    const AUSTRIAN_IBAN: &str = "AT61 1904 3002 3457 3201";

    fn env_map() -> HashMap<&'static str, String> {
        HashMap::from([
            (PRODUCT_ID_VAR, "PRODUCT123".to_string()),
            (BLZ_VAR, "12030000".to_string()),
            (USER_VAR, "my-user".to_string()),
            (URL_VAR, "https://example.invalid/fints".to_string()),
        ])
    }

    fn parse(map: &HashMap<&'static str, String>) -> Result<Inputs, Vec<InputError>> {
        parse_inputs(|name| map.get(name).cloned())
    }

    #[test]
    fn masks_german_iban_with_spaces() {
        assert_eq!(mask_iban(GERMAN_IBAN), "DE** **** **** **** **30 00");
    }

    #[test]
    fn masks_german_iban_without_spaces() {
        assert_eq!(mask_iban("DE89370400440532013000"), "DE****************3000");
    }

    #[test]
    fn masks_austrian_iban_with_spaces() {
        assert_eq!(mask_iban(AUSTRIAN_IBAN), "AT** **** **** **** 3201");
    }

    #[test]
    fn masks_austrian_iban_without_spaces() {
        assert_eq!(mask_iban("AT611904300234573201"), "AT**************3201");
    }

    #[test]
    fn masks_short_values_completely() {
        assert_eq!(mask_iban("ABC"), "***");
        assert_eq!(mask_iban("AB"), "**");
        assert_eq!(mask_iban(""), "");
        assert_eq!(mask_iban("A B"), "* *");
    }

    #[test]
    fn parses_all_inputs_and_defaults_days_to_30() {
        let inputs = parse(&env_map()).expect("valid inputs");
        assert_eq!(inputs.product_id, "PRODUCT123");
        assert_eq!(inputs.blz, "12030000");
        assert_eq!(inputs.user, "my-user");
        assert_eq!(inputs.url, "https://example.invalid/fints");
        assert_eq!(inputs.days, 30);
    }

    #[test]
    fn parses_explicit_days() {
        let mut map = env_map();
        map.insert(DAYS_VAR, "90".to_string());
        assert_eq!(parse(&map).expect("valid inputs").days, 90);
    }

    #[test]
    fn each_required_input_can_be_missing() {
        for variable in [PRODUCT_ID_VAR, BLZ_VAR, USER_VAR] {
            let mut map = env_map();
            map.remove(variable);
            let errors = parse(&map).expect_err("missing required input");
            assert_eq!(errors.len(), 1, "one error for {variable}");
            assert_eq!(errors[0].variable, variable);
        }
    }

    #[test]
    fn rejects_blz_that_is_not_eight_digits() {
        for blz in ["12345", "123456789", "1234abcd"] {
            let mut map = env_map();
            map.insert(BLZ_VAR, blz.to_string());
            let errors = parse(&map).expect_err("invalid BLZ");
            assert_eq!(errors.len(), 1);
            assert_eq!(errors[0].variable, BLZ_VAR);
        }
    }

    #[test]
    fn rejects_out_of_range_and_non_numeric_days() {
        for days in ["0", "91", "abc"] {
            let mut map = env_map();
            map.insert(DAYS_VAR, days.to_string());
            let errors = parse(&map).expect_err("invalid days");
            assert_eq!(errors.len(), 1, "one error for days={days}");
            assert_eq!(errors[0].variable, DAYS_VAR);
        }
    }

    #[test]
    fn unknown_blz_without_url_is_an_error() {
        let mut map = env_map();
        map.insert(BLZ_VAR, "99999999".to_string());
        map.remove(URL_VAR);
        let errors = parse(&map).expect_err("unknown BLZ without URL");
        assert_eq!(errors.len(), 1);
        assert_eq!(errors[0].variable, URL_VAR);
    }

    #[test]
    fn known_blz_without_url_uses_the_registry() {
        let mut map = env_map();
        map.remove(URL_VAR);
        let inputs = parse(&map).expect("registry URL");
        assert!(!inputs.url.is_empty());
    }

    #[test]
    fn debug_does_not_print_the_user_id() {
        let inputs = parse(&env_map()).expect("valid inputs");
        let debug = format!("{inputs:?}");
        assert!(!debug.contains("my-user"));
    }

    #[test]
    fn go_when_every_account_has_balance_and_statements() {
        let summaries = vec![
            AccountSummary::new(
                GERMAN_IBAN,
                "EUR",
                true,
                StatementState::Available {
                    booking_count: 3,
                    earliest: Some("2024-01-01".to_string()),
                    latest: Some("2024-01-31".to_string()),
                },
            ),
            AccountSummary::new(
                AUSTRIAN_IBAN,
                "EUR",
                true,
                StatementState::Available {
                    booking_count: 0,
                    earliest: None,
                    latest: None,
                },
            ),
        ];
        assert_eq!(decide(&summaries, None), Decision::Go);
    }

    #[test]
    fn no_go_for_each_reason_category() {
        let ok = AccountSummary::new(
            GERMAN_IBAN,
            "EUR",
            true,
            StatementState::Available {
                booking_count: 0,
                earliest: None,
                latest: None,
            },
        );

        assert_eq!(
            decide(std::slice::from_ref(&ok), Some(NoGoReason::LoginRejected)),
            Decision::NoGo(NoGoReason::LoginRejected)
        );
        assert_eq!(
            decide(std::slice::from_ref(&ok), Some(NoGoReason::TanMethodUnsupported)),
            Decision::NoGo(NoGoReason::TanMethodUnsupported)
        );
        assert_eq!(
            decide(std::slice::from_ref(&ok), Some(NoGoReason::TanTimeout)),
            Decision::NoGo(NoGoReason::TanTimeout)
        );
        assert_eq!(
            decide(std::slice::from_ref(&ok), Some(NoGoReason::Other("Transport"))),
            Decision::NoGo(NoGoReason::Other("Transport"))
        );

        let no_statements = AccountSummary::new(GERMAN_IBAN, "EUR", true, StatementState::Unavailable);
        assert_eq!(
            decide(&[no_statements], None),
            Decision::NoGo(NoGoReason::StatementsUnavailable)
        );
    }

    #[test]
    fn no_go_when_balance_is_absent() {
        let summary = AccountSummary::new(
            GERMAN_IBAN,
            "EUR",
            false,
            StatementState::Available {
                booking_count: 1,
                earliest: Some("2024-02-01".to_string()),
                latest: Some("2024-02-01".to_string()),
            },
        );
        assert_eq!(
            decide(&[summary], None),
            Decision::NoGo(NoGoReason::Other("BalanceAbsent"))
        );
    }

    #[test]
    fn summary_never_contains_a_full_iban() {
        let summary = AccountSummary::new(
            GERMAN_IBAN,
            "EUR",
            true,
            StatementState::Available {
                booking_count: 3,
                earliest: Some("2024-01-01".to_string()),
                latest: Some("2024-01-31".to_string()),
            },
        );
        let line = summary_line(&summary);
        assert!(!line.contains(GERMAN_IBAN));
        assert!(!line.contains("DE89370400440532013000"));
        assert!(line.contains("DE** **** **** **** **30 00"));
        assert!(line.contains("balance present"));
        assert!(line.contains("3 bookings"));
        assert!(line.contains("MT940"));
    }

    #[test]
    fn summary_reports_zero_bookings_and_unavailable_statements() {
        let zero = AccountSummary::new(
            AUSTRIAN_IBAN,
            "EUR",
            true,
            StatementState::Available {
                booking_count: 0,
                earliest: None,
                latest: None,
            },
        );
        let zero_line = summary_line(&zero);
        assert!(zero_line.contains("0 bookings (no bookings) MT940"));
        assert!(!zero_line.contains("no bookings (no bookings)"));

        let unavailable = AccountSummary::new(AUSTRIAN_IBAN, "EUR", false, StatementState::Unavailable);
        let unavailable_line = summary_line(&unavailable);
        assert!(unavailable_line.contains("balance absent"));
        assert!(unavailable_line.contains("statements unavailable"));
    }

    #[test]
    fn detects_optical_tan_methods() {
        assert!(is_unsupported_tan_method("chipTAN flicker"));
        assert!(is_unsupported_tan_method("photoTAN"));
        assert!(is_unsupported_tan_method("chipTAN-QR"));
        assert!(!is_unsupported_tan_method("chipTAN manuell"));
        assert!(!is_unsupported_tan_method("pushTAN 2.0"));
        assert!(!is_unsupported_tan_method("SMS-TAN"));
    }
}
