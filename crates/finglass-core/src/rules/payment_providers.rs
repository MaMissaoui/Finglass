//! PayPal shop extraction, ported from FinDesk's `util/PaymentProviders.kt`.
//!
//! The provider is booked as the partner (e.g. "PayPal Europe S.a.r.l. et Cie S.C.A") and the real
//! shop appears in the purpose in one of three known formats: the `Ihr Einkauf bei <Shop>` marker
//! (usually inside SEPA tags), a pre-comma name without that marker, or a card descriptor such as
//! `PAYPAL *<SHOP>`.

use std::sync::LazyLock;

use fancy_regex::Regex;

/// A shop extracted from a booking plus the provider view of that booking: the booking's text with
/// the shop removed. [`crate::rules::RuleMatcher`] uses the view to recognise a provider rule.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ShopMatch {
    /// The shop's name.
    pub shop: String,
    /// The provider purpose view, when the format supplies one.
    pub provider_purpose: Option<String>,
    /// The provider partner view, when the format supplies one.
    pub provider_partner: Option<String>,
}

/// Extracts the actual shop from a payment-provider booking, currently for PayPal.
pub struct PaymentProviders;

const PROVIDER: &str = "PayPal";
const PROVIDER_PURPOSE: &str = "PAYPAL";
const MAX_SHOP_LENGTH: usize = 70;

/// SEPA structured-field tags, from FinDesk's `BankText.TAG`.
const TAG_PATTERN: &str = "(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\\+";

fn flexible(word: &str) -> String {
    word.chars()
        .map(|character| character.to_string())
        .collect::<Vec<_>>()
        .join("\\s?")
}

fn ihr() -> String {
    format!("{}\\b", flexible("Ihr"))
}

static MARKER: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        "(?i){}\\s+{}\\s+{}",
        flexible("Ihr"),
        flexible("Einkauf"),
        flexible("bei")
    ))
    .expect("marker pattern is valid")
});

static PRE_COMMA: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!("(?i)PP\\.\\d+\\.PP[A-Z]?\\s*\\.?\\s*(.+?)\\s*,\\s*{}", ihr()))
        .expect("pre-comma pattern is valid")
});

static PRE_COMMA_LEADING_DOT: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!("(?i)^\\.\\s*(.+?)\\s*,\\s*{}", ihr())).expect("leading-dot pattern is valid")
});

static PP_ID_IN_PURPOSE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new("(?i)PP\\.\\d+\\.PP[A-Z]?").expect("id pattern is valid"));

static CARD_START: LazyLock<Regex> =
    LazyLock::new(|| Regex::new("(?i)^PAYPAL\\s*\\*").expect("card-start pattern is valid"));

static CARD_STOP: LazyLock<Regex> =
    LazyLock::new(|| Regex::new("(?i)(?:\\s+|^)\\d+(?=\\s|$)|,\\s*\\d").expect("card-stop pattern is valid"));

static CODE_TAIL: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(",\\s*\\S*\\d\\S*$").expect("code-tail pattern is valid"));

static TOP_UP: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        "\\b(?:{}\\s+)?{}\\b.*",
        flexible("ABBUCHUNG"),
        flexible("VOM")
    ))
    .expect("top-up pattern is valid")
});

static PP_ID: LazyLock<Regex> = LazyLock::new(|| Regex::new("(?i)PP\\.\\d+\\.PP").expect("id pattern is valid"));

static TAG: LazyLock<Regex> = LazyLock::new(|| Regex::new(TAG_PATTERN).expect("tag pattern is valid"));

const NON_SHOPS: [&str; 6] = ["ihr", "einkauf", "bei", "abbuchung", "vom", "transfer"];

fn find<'t>(regex: &Regex, text: &'t str) -> Option<fancy_regex::Match<'t>> {
    regex.find(text).ok().flatten()
}

fn collapse_whitespace(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

impl PaymentProviders {
    /// The row title for a shop found by [`Self::merchant_of`]: "PayPal · <shop>", or `None` without
    /// a shop.
    pub fn title_of(purpose: Option<&str>, partner_name: Option<&str>) -> Option<String> {
        Self::merchant_of(purpose, partner_name).map(|shop| format!("{PROVIDER} · {shop}"))
    }

    /// The shop the raw purpose buys from, or `None` when the booking names no shop.
    pub fn merchant_of(purpose: Option<&str>, partner_name: Option<&str>) -> Option<String> {
        Self::shop_match(purpose, partner_name).map(|matched| matched.shop)
    }

    /// The shop named by `purpose` for the PayPal `partner_name`, with the provider view that
    /// [`crate::rules::RuleMatcher`] uses to recognise a provider rule.
    ///
    /// Formats are tried in this order: the `Ihr Einkauf bei` marker, a pre-comma name without that
    /// marker, and a card descriptor (`PAYPAL *<SHOP>`). The marker format prefers the pre-comma name
    /// when it is the fuller one.
    pub fn shop_match(purpose: Option<&str>, partner_name: Option<&str>) -> Option<ShopMatch> {
        let partner_has_paypal = partner_name.is_some_and(|partner| partner.to_lowercase().contains("paypal"));
        let text = purpose.map(collapse_whitespace).unwrap_or_default();

        // 1. Marker format, e.g. "PP.0471.PPX . <Shop>, Ihr Einkauf bei <Shop>"
        let marker = if partner_has_paypal { find(&MARKER, &text) } else { None };
        if let Some(marker) = marker {
            let tail = &text[marker.end()..];
            let next_tag = find(&TAG, tail);
            let post_raw = match next_tag {
                Some(tag) => &tail[..tag.start()],
                None => tail,
            };
            let pre_raw = pre_comma_name(&text, marker.start());
            let post = sanitize(Some(&cut_top_up(post_raw)));
            let pre = sanitize(pre_raw.as_deref());
            let shop = match (pre, post) {
                (None, post) => post,
                (pre, None) => pre,
                (Some(pre), Some(post)) => Some(preferred(&pre, &post)),
            };
            if let Some(shop) = shop {
                return Some(ShopMatch {
                    shop,
                    provider_purpose: None,
                    provider_partner: partner_name.map(str::to_owned),
                });
            }
        }

        // 2. A pre-comma name without the marker, e.g. "PP.1234.PP . <Shop>, Ihr Abo"
        if partner_has_paypal && marker.is_none() {
            let captures = PRE_COMMA
                .captures(&text)
                .ok()
                .flatten()
                .or_else(|| PRE_COMMA_LEADING_DOT.captures(&text).ok().flatten());
            if let Some(captures) = captures {
                let capture = captures.get(1).map(|capture| capture.as_str());
                if let Some(shop) = sanitize(capture) {
                    return Some(ShopMatch {
                        shop,
                        provider_purpose: None,
                        provider_partner: partner_name.map(str::to_owned),
                    });
                }
            }
        }

        // 3. Card format, e.g. "PAYPAL *<SHOP>" with a blank or PayPal partner
        let card_start = find(&CARD_START, &text);
        if let Some(card_start) = card_start
            && (partner_name.is_none_or(|partner| partner.trim().is_empty()) || partner_has_paypal)
        {
            let after_star = &text[card_start.end()..];
            let stop = find(&CARD_STOP, after_star);
            let shop_raw = match stop {
                Some(stop) => &after_star[..stop.start()],
                None => after_star,
            };
            if let Some(shop) = sanitize(Some(shop_raw)) {
                return Some(ShopMatch {
                    shop,
                    provider_purpose: Some(PROVIDER_PURPOSE.to_owned()),
                    provider_partner: partner_has_paypal.then(|| PROVIDER_PURPOSE.to_owned()),
                });
            }
        }

        None
    }
}

/// The `<name>` between a `PP.<n>.PP` id and the comma immediately before the marker, or `None`.
fn pre_comma_name(text: &str, marker_start: usize) -> Option<String> {
    let prefix = &text[..marker_start];
    let id = PP_ID_IN_PURPOSE
        .find_iter(prefix)
        .filter_map(std::result::Result::ok)
        .last();
    let rest = if let Some(id) = id {
        let after = prefix[id.end()..].trim();
        after.strip_prefix('.').unwrap_or(after).trim()
    } else {
        if !prefix.starts_with('.') {
            return None;
        }
        let trimmed = prefix.trim();
        trimmed.strip_prefix('.').unwrap_or(trimmed).trim()
    };
    if !rest.ends_with(',') {
        return None;
    }
    let name = rest[..rest.len() - 1].trim();
    (!name.is_empty()).then(|| name.to_owned())
}

/// Cuts an upper-case top-up note from the post-marker text, unless nothing would remain before it.
fn cut_top_up(text: &str) -> String {
    let Some(note) = find(&TOP_UP, text) else {
        return text.to_owned();
    };
    let before = &text[..note.start()];
    if before.trim().is_empty() {
        text.to_owned()
    } else {
        before.to_owned()
    }
}

/// The fuller of the pre-comma name and the post-marker shop when both pass the gate.
fn preferred(pre: &str, post: &str) -> String {
    let normalized_pre = normalize(pre);
    let normalized_post = normalize(post);
    if normalized_pre == normalized_post {
        if pre.chars().filter(|character| character.is_whitespace()).count()
            < post.chars().filter(|character| character.is_whitespace()).count()
        {
            pre.to_owned()
        } else {
            post.to_owned()
        }
    } else if normalized_pre.starts_with(&normalized_post) {
        if normalized_pre.chars().count() > normalized_post.chars().count() {
            pre.to_owned()
        } else {
            post.to_owned()
        }
    } else if normalized_post.starts_with(&normalized_pre) {
        if normalized_post.chars().count() > normalized_pre.chars().count() {
            post.to_owned()
        } else {
            pre.to_owned()
        }
    } else {
        post.to_owned()
    }
}

fn normalize(shop: &str) -> String {
    shop.chars()
        .filter(|character| !character.is_whitespace())
        .collect::<String>()
        .to_lowercase()
}

/// Cleans a candidate shop and rejects text that only looks like one: ids, codes, provider names and
/// the marker words themselves. The code tail is stripped before leading punctuation, so
/// `, AB12CD34` leaves nothing rather than a bare code.
fn sanitize(raw: Option<&str>) -> Option<String> {
    let raw = raw?;
    let mut candidate = collapse_whitespace(raw);

    if let Some(tail) = find(&CODE_TAIL, &candidate) {
        candidate.replace_range(tail.start().., "");
    }
    candidate = candidate
        .trim_start_matches(|character: char| !character.is_alphanumeric())
        .to_owned();
    candidate = candidate
        .trim_end_matches(|character: char| {
            character == ',' || character == ';' || character == ':' || character.is_whitespace()
        })
        .to_owned();
    candidate = candidate.chars().take(MAX_SHOP_LENGTH).collect::<String>();
    candidate = candidate.trim_end().to_owned();

    if candidate.chars().count() < 2 {
        return None;
    }
    if !candidate.chars().any(char::is_alphabetic) {
        return None;
    }
    if candidate.chars().all(|character| character.is_ascii_digit()) {
        return None;
    }
    if candidate.to_lowercase().contains("paypal") {
        return None;
    }
    if PP_ID.is_match(&candidate).unwrap_or(false) {
        return None;
    }
    if NON_SHOPS.contains(&candidate.to_lowercase().as_str()) {
        return None;
    }
    Some(candidate)
}

#[cfg(test)]
mod tests {
    use super::*;

    const PAYPAL_PARTNER: &str = "PayPal Europe S.a.r.l. et Cie S.C.A";

    fn paypal_purpose(shop: &str) -> String {
        format!("EREF+REF001 SVWZ+PP.0471.PPX . {shop}, Ihr Einkauf bei {shop}")
    }

    fn merchant(purpose: &str, partner: &str) -> Option<String> {
        PaymentProviders::merchant_of(Some(purpose), Some(partner))
    }

    #[test]
    fn the_shop_after_the_marker_is_extracted_from_a_purpose_with_sepa_tags_and_padding() {
        let raw = "EREF+REF001          SVWZ+PP.0471.PPX .   Beispiel Shop GmbH,   Ihr Einkauf bei Beispiel Shop GmbH";
        assert_eq!(merchant(raw, PAYPAL_PARTNER).as_deref(), Some("Beispiel Shop GmbH"));
    }

    #[test]
    fn a_padded_partner_counts_as_paypal_when_its_readable_part_does() {
        let partner = "PayPal Europe S.a.r.l.      Buchungscode 4711      Abrechnung 10/26";
        assert_eq!(
            merchant(&paypal_purpose("Beispiel Shop GmbH"), partner).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn the_marker_is_matched_ignoring_case() {
        let raw = "SVWZ+PP.0471.PPX . Beispiel Shop GmbH, IHR EINKAUF BEI Beispiel Shop GmbH";
        assert_eq!(merchant(raw, PAYPAL_PARTNER).as_deref(), Some("Beispiel Shop GmbH"));
    }

    #[test]
    fn trailing_separators_are_stripped_but_a_trailing_dot_is_kept() {
        assert_eq!(
            merchant(&paypal_purpose("Beispiel Shop S.a.r.l."), PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop S.a.r.l.")
        );
        assert_eq!(
            merchant(&paypal_purpose("Beispiel Shop GmbH,;:"), PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn the_shop_is_extracted_without_svwz_and_ends_at_the_next_tag() {
        let without_svwz = "EREF+REF001 PP.0471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH";
        assert_eq!(
            merchant(without_svwz, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        let followed_by_tag = "EREF+REF001 Ihr Einkauf bei Beispiel Shop GmbH SVWZ+Rechnung 4711";
        assert_eq!(
            merchant(followed_by_tag, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn title_of_combines_the_provider_and_the_shop() {
        assert_eq!(
            PaymentProviders::title_of(Some(&paypal_purpose("Beispiel Shop GmbH")), Some(PAYPAL_PARTNER)).as_deref(),
            Some("PayPal · Beispiel Shop GmbH")
        );
        assert_eq!(
            PaymentProviders::title_of(Some(&paypal_purpose("Beispiel Shop GmbH")), Some("Beispiel Shop GmbH")),
            None
        );
    }

    #[test]
    fn an_over_long_shop_name_is_capped_at_70_characters() {
        let long = "BeispielShop".repeat(12);
        assert_eq!(
            merchant(&paypal_purpose(&long), PAYPAL_PARTNER).map(|shop| shop.chars().count()),
            Some(70)
        );

        let spaced = "Beispiel Shop ".repeat(10).trim_end().to_owned();
        assert_eq!(
            merchant(&paypal_purpose(&spaced), PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop ".repeat(5).trim_end())
        );
    }

    #[test]
    fn a_non_paypal_partner_or_none_gives_none_even_with_a_marker() {
        assert_eq!(
            merchant(&paypal_purpose("Beispiel Shop GmbH"), "Beispiel Shop GmbH"),
            None
        );
        assert_eq!(merchant(&paypal_purpose("Beispiel Shop GmbH"), "PP Muster GmbH"), None);
        assert_eq!(
            PaymentProviders::merchant_of(Some(&paypal_purpose("Beispiel Shop GmbH")), None),
            None
        );
        assert_eq!(merchant(&paypal_purpose("Beispiel Shop GmbH"), "   "), None);
    }

    #[test]
    fn a_purpose_without_the_marker_or_without_text_gives_none() {
        assert_eq!(
            merchant("EREF+REF001 SVWZ+PP.0471.PPX . Beispiel Shop GmbH", PAYPAL_PARTNER),
            None
        );
        assert_eq!(PaymentProviders::merchant_of(None, Some(PAYPAL_PARTNER)), None);
        assert_eq!(merchant("   ", PAYPAL_PARTNER), None);
    }

    #[test]
    fn an_empty_tail_after_the_marker_gives_none() {
        assert_eq!(merchant(&paypal_purpose("   ., "), PAYPAL_PARTNER), None);
    }

    #[test]
    fn the_marker_is_found_with_an_id_before_the_paypal_id() {
        let raw = "1234567890123 PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH";
        assert_eq!(merchant(raw, PAYPAL_PARTNER).as_deref(), Some("Beispiel Shop GmbH"));
    }

    #[test]
    fn the_marker_tolerates_a_space_inserted_inside_a_word() {
        let in_first_word = "PP.1234.PP . Beispiel Shop GmbH, Ihr Ein kauf bei Beispiel Shop GmbH";
        assert_eq!(
            merchant(in_first_word, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        let in_last_word = "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf b ei Beispiel Shop GmbH";
        assert_eq!(
            merchant(in_last_word, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn the_fuller_of_the_pre_comma_name_and_the_marker_shop_wins() {
        assert_eq!(
            merchant(
                "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Sh",
                PAYPAL_PARTNER
            )
            .as_deref(),
            Some("Beispiel Shop GmbH")
        );
        assert_eq!(
            merchant(
                "PP.1234.PP . Beispiel Sh, Ihr Einkauf bei Beispiel Shop GmbH",
                PAYPAL_PARTNER
            )
            .as_deref(),
            Some("Beispiel Shop GmbH")
        );
        assert_eq!(
            merchant(
                "PP.1234.PP . Beispiel Sh op GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
                PAYPAL_PARTNER
            )
            .as_deref(),
            Some("Beispiel Shop GmbH")
        );
        assert_eq!(
            merchant(
                "PP.1234.PP . Anderer Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
                PAYPAL_PARTNER
            )
            .as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn a_code_tail_after_the_marker_shop_is_dropped() {
        let with_shop = "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH, AB12CD34";
        assert_eq!(
            merchant(with_shop, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        let without_shop = "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei , AB12CD34";
        assert_eq!(
            merchant(without_shop, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        assert_eq!(merchant("Ihr Einkauf bei , AB12CD34", PAYPAL_PARTNER), None);
    }

    #[test]
    fn an_upper_case_top_up_note_after_the_marker_shop_is_cut() {
        let with_abbuchung =
            "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH ABBUCHUNG VOM PAYPAL-KONTO";
        assert_eq!(
            merchant(with_abbuchung, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        let inserted_space =
            "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH ABBU CHUNG VOM PAYPAL-KONTO";
        assert_eq!(
            merchant(inserted_space, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );

        let only_vom = "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH VOM GIROKONTO";
        assert_eq!(
            merchant(only_vom, PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn a_lower_case_vom_inside_the_shop_name_is_kept() {
        let raw = "PP.1234.PP . Blumen vom Beispielhof, Ihr Einkauf bei Blumen vom Beispielhof";
        assert_eq!(merchant(raw, PAYPAL_PARTNER).as_deref(), Some("Blumen vom Beispielhof"));
    }

    #[test]
    fn a_pre_comma_name_without_the_marker_is_the_shop() {
        assert_eq!(
            merchant("PP.1234.PP . Beispiel Shop GmbH, Ihr Abo", PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn a_leading_dot_gives_the_pre_comma_name_without_a_pp_id() {
        assert_eq!(
            merchant(". Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Sh", PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
        assert_eq!(
            merchant(". Beispiel Shop GmbH, Ihr Abo", PAYPAL_PARTNER).as_deref(),
            Some("Beispiel Shop GmbH")
        );
    }

    #[test]
    fn card_descriptors_give_the_shop_before_the_digits() {
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *BEISPIELSHOP"), Some("PAYPAL *BEISPIELSHOP")).as_deref(),
            Some("BEISPIELSHOP")
        );
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *BEISPIELSHOP"), None).as_deref(),
            Some("BEISPIELSHOP")
        );
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL*BEISPIELSHOP"), None).as_deref(),
            Some("BEISPIELSHOP")
        );
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *BEISPIEL SHOP 12345678"), None).as_deref(),
            Some("BEISPIEL SHOP")
        );
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *BEISPIEL SHOP, 4930123456"), None).as_deref(),
            Some("BEISPIEL SHOP")
        );
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *SHOP24"), None).as_deref(),
            Some("SHOP24")
        );
    }

    #[test]
    fn a_card_purpose_with_a_non_paypal_partner_gives_no_shop() {
        assert_eq!(
            PaymentProviders::merchant_of(Some("PAYPAL *BEISPIELSHOP"), Some("Beispiel Shop GmbH")),
            None
        );
    }

    #[test]
    fn ids_withdrawals_transfers_and_empty_tails_give_no_shop() {
        assert_eq!(merchant("PP.1234.PP . , Ihr Einkauf bei", PAYPAL_PARTNER), None);
        assert_eq!(merchant("1234567890123 PP.1234.PP", PAYPAL_PARTNER), None);
        assert_eq!(merchant("1234567890", PAYPAL_PARTNER), None);
        assert_eq!(merchant("PP.1234.PP ABBUCHUNG VOM GIROKONTO", PAYPAL_PARTNER), None);
        assert_eq!(merchant("EREF+123 SVWZ+ABC TRANSFER", PAYPAL_PARTNER), None);
        assert_eq!(
            merchant(
                "PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
                "Beispiel Shop GmbH"
            ),
            None
        );
    }

    #[test]
    fn match_exposes_the_provider_view_for_marker_and_card_bookings() {
        let marker = PaymentProviders::shop_match(
            Some("PP.1234.PP . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH"),
            Some(PAYPAL_PARTNER),
        )
        .unwrap();
        assert_eq!(marker.shop, "Beispiel Shop GmbH");
        assert_eq!(marker.provider_purpose, None);
        assert_eq!(marker.provider_partner.as_deref(), Some(PAYPAL_PARTNER));

        let card = PaymentProviders::shop_match(Some("PAYPAL *BEISPIELSHOP"), None).unwrap();
        assert_eq!(card.shop, "BEISPIELSHOP");
        assert_eq!(card.provider_purpose.as_deref(), Some("PAYPAL"));
        assert_eq!(card.provider_partner, None);

        let card_with_partner =
            PaymentProviders::shop_match(Some("PAYPAL *BEISPIELSHOP"), Some("PAYPAL *BEISPIELSHOP")).unwrap();
        assert_eq!(card_with_partner.provider_purpose.as_deref(), Some("PAYPAL"));
        assert_eq!(card_with_partner.provider_partner.as_deref(), Some("PAYPAL"));
    }
}
