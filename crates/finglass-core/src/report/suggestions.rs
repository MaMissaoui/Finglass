//! Tax-relevance suggestions from keywords, ported from FinDesk's `document/TaxSuggestions.kt`.

use fancy_regex::Regex;

use crate::db::Database;
use crate::model::{TaxSource, TransactionItem};
use crate::settings;

/// The settings key with the user's own keywords, one per line.
pub const KEY_KEYWORDS: &str = "tax.suggestionKeywords";

/// The built-in suggestion keywords.
pub const DEFAULT_KEYWORDS: &[&str] = &[
    "Spende",
    "Handwerker",
    "Schornsteinfeger",
    "Steuerberater",
    "Lohnsteuerhilfe",
    "Kita",
    "Kindergarten",
    "Tagesmutter",
    "Schulgeld",
    "Krankenkasse",
    "Zahnarzt",
    "Apotheke",
    "Fortbildung",
    "Fachliteratur",
    "Gewerkschaft",
    "Kirchensteuer",
    "Riester",
    "Haftpflicht",
    "donation",
    "charity",
    "tax advisor",
    "tuition",
    "assurance",
];

/// One suggestion: a booking and the keyword that matched it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaxSuggestion {
    /// The suggested booking.
    pub booking: TransactionItem,
    /// The keyword that matched.
    pub keyword: String,
}

/// The keywords to suggest with: the setting's trimmed lines, or the built-in defaults.
pub fn keywords(db: &Database) -> crate::repo::Result<Vec<String>> {
    let stored = settings::get_setting(db, KEY_KEYWORDS)?;
    let Some(stored) = stored else {
        return Ok(DEFAULT_KEYWORDS.iter().map(|keyword| (*keyword).to_owned()).collect());
    };
    let lines: Vec<String> = stored
        .split('\n')
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .map(str::to_owned)
        .collect();
    if lines.is_empty() {
        Ok(DEFAULT_KEYWORDS.iter().map(|keyword| (*keyword).to_owned()).collect())
    } else {
        Ok(lines)
    }
}

/// Finds every booking a keyword hints at, newest first.
pub fn find(bookings: &[TransactionItem], keywords: &[String]) -> Vec<TaxSuggestion> {
    let prepared: Vec<(String, Regex)> = keywords
        .iter()
        .map(|keyword| keyword.trim())
        .filter(|keyword| keyword.chars().count() >= 3)
        .map(|keyword| {
            let pattern = format!("(?i)(?<![\\p{{L}}\\p{{N}}]){}", regex::escape(keyword));
            (
                keyword.to_owned(),
                Regex::new(&pattern).expect("the suggestion pattern is valid"),
            )
        })
        .collect();
    if prepared.is_empty() {
        return Vec::new();
    }

    let mut suggestions: Vec<TaxSuggestion> = bookings
        .iter()
        .filter_map(|booking| match_booking(&prepared, booking))
        .collect();
    suggestions.sort_by_key(|left| std::cmp::Reverse(left.booking.date));
    suggestions
}

fn match_booking(prepared: &[(String, Regex)], booking: &TransactionItem) -> Option<TaxSuggestion> {
    if booking.is_tax_relevant {
        return None;
    }
    if booking.tax_source == TaxSource::Manual {
        return None;
    }
    if booking.is_transfer {
        return None;
    }

    prepared
        .iter()
        .find(|(_, regex)| {
            booking
                .partner_name
                .as_deref()
                .is_some_and(|partner| regex.is_match(partner).unwrap_or(false))
                || booking
                    .purpose
                    .as_deref()
                    .is_some_and(|purpose| regex.is_match(purpose).unwrap_or(false))
        })
        .map(|(keyword, _)| TaxSuggestion {
            booking: booking.clone(),
            keyword: keyword.clone(),
        })
}
