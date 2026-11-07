//! Heuristic extraction of the total amount and date from receipt/invoice text (German and English).
//!
//! The rules mirror the Kotlin `ReceiptParser`: every line's amounts are collected, the last line carrying a
//! total keyword decides the total (otherwise the largest amount wins), and dates are tried in a fixed order.
//! Amounts are integer minor units in the currency the caller passes in; nothing is rounded.

use std::sync::LazyLock;

use fancy_regex::Regex as FancyRegex;
use regex::Regex;
use time::{Date, Month};

use crate::import::two_digit_year::resolve;
use crate::money::{Currency, Money, parse_decimal};

/// What the parser found in a piece of receipt text.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Fields {
    /// The total: the last keyword line's last amount, or the largest amount in the text.
    pub total: Option<Money>,
    /// The first valid date in the supported range, or nothing.
    pub date: Option<Date>,
    /// Every distinct amount, in the order it first appeared.
    pub amounts: Vec<Money>,
}

// 1.234,56 | 1234,56 | 1,234.56 | 1234.56  (exactly two decimals)
static AMOUNT_REGEX: LazyLock<FancyRegex> = LazyLock::new(|| {
    FancyRegex::new(r"(?<![\d.,])(\d{1,3}(?:[.,\s]\d{3})+|\d+)([.,])(\d{2})(?!\d)(?![.,]\d)")
        .expect("the amount pattern is valid")
});

static TOTAL_KEYWORDS: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)\b(gesamtbetrag|gesamtsumme|gesamt|summe|zu zahlen|endbetrag|rechnungsbetrag|total|amount due|betrag)\b",
    )
    .expect("the keyword pattern is valid")
});

static DATE_D_M_YYYY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b(\d{1,2})\.(\d{1,2})\.(\d{4})\b").expect("the date pattern is valid"));
static DATE_YYYY_MM_DD: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b(\d{4})-(\d{2})-(\d{2})\b").expect("the date pattern is valid"));
static DATE_D_M_YY: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b(\d{1,2})\.(\d{1,2})\.(\d{2})\b").expect("the date pattern is valid"));
static DATE_D_M_YYYY_SLASH: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b(\d{1,2})/(\d{1,2})/(\d{4})\b").expect("the date pattern is valid"));

/// Parses receipt text into amounts, total and date.
///
/// `currency` decides how many decimals an amount may have (and therefore which candidates count as
/// amounts), and `today` is the anchor for two-digit years. Nothing reads the clock.
pub fn parse(text: &str, currency: Currency, today: Date) -> Fields {
    let mut amounts: Vec<Money> = Vec::new();
    let mut keyword_total: Option<Money> = None;

    for line in text.split(['\n', '\r']) {
        let line_amounts: Vec<Money> = amount_matches(line, currency);
        for amount in &line_amounts {
            if !amounts.contains(amount) {
                amounts.push(*amount);
            }
        }
        if TOTAL_KEYWORDS.is_match(line)
            && let Some(last) = line_amounts.last()
        {
            // The last keyword line wins (totals tend to come after subtotals).
            keyword_total = Some(*last);
        }
    }

    Fields {
        total: keyword_total.or_else(|| amounts.iter().copied().max()),
        date: find_date(text, today),
        amounts,
    }
}

fn amount_matches(line: &str, currency: Currency) -> Vec<Money> {
    let mut found = Vec::new();
    for captures in AMOUNT_REGEX.captures_iter(line).flatten() {
        if let Some(amount) = to_amount(&captures, currency) {
            found.push(amount);
        }
    }
    found
}

fn to_amount(captures: &fancy_regex::Captures<'_, str>, currency: Currency) -> Option<Money> {
    let int_part = captures.get(1)?.as_str();
    let separator = captures.get(2)?.as_str();
    let decimals = captures.get(3)?.as_str();

    // The grouping separator must differ from the decimal separator ("1.234,56" or "1,234.56").
    if int_part.contains(separator) {
        return None;
    }

    let digits: String = int_part
        .chars()
        .filter(|character| !matches!(character, '.' | ',' | ' '))
        .collect();
    parse_decimal(&format!("{digits}.{decimals}"), currency).ok()
}

fn find_date(text: &str, today: Date) -> Option<Date> {
    for captures in DATE_D_M_YYYY.captures_iter(text) {
        let date = smart_date(
            captures.get(3)?.as_str(),
            captures.get(2)?.as_str(),
            captures.get(1)?.as_str(),
        );
        if let Some(date) = date.filter(in_supported_range) {
            return Some(date);
        }
    }

    for captures in DATE_YYYY_MM_DD.captures_iter(text) {
        let date = smart_date(
            captures.get(1)?.as_str(),
            captures.get(2)?.as_str(),
            captures.get(3)?.as_str(),
        );
        if let Some(date) = date.filter(in_supported_range) {
            return Some(date);
        }
    }

    for captures in DATE_D_M_YY.captures_iter(text) {
        let day = captures.get(1)?.as_str().parse::<u32>().ok()?;
        let month = captures.get(2)?.as_str().parse::<u32>().ok()?;
        let yy = captures.get(3)?.as_str().parse::<u8>().ok()?;
        let year = resolve(yy, today).ok()?;
        let date = strict_date(year, month, day);
        if let Some(date) = date.filter(in_supported_range) {
            return Some(date);
        }
    }

    for captures in DATE_D_M_YYYY_SLASH.captures_iter(text) {
        let first = captures.get(1)?.as_str().parse::<u32>().ok()?;
        let second = captures.get(2)?.as_str().parse::<u32>().ok()?;
        let year = captures.get(3)?.as_str().parse::<i32>().ok()?;
        let date = slash_date(year, first, second);
        if let Some(date) = date.filter(in_supported_range) {
            return Some(date);
        }
    }

    None
}

fn in_supported_range(date: &Date) -> bool {
    (1990..=2100).contains(&date.year())
}

/// A four-digit-year pattern as Java's SMART resolver reads it: a day above the month's length is
/// clamped to that length, while a day outside 1..=31 or a month outside 1..=12 is invalid.
fn smart_date(year: &str, month: &str, day: &str) -> Option<Date> {
    let year = year.parse::<i32>().ok()?;
    let month = month.parse::<u32>().ok()?;
    let day = day.parse::<u32>().ok()?;

    if !(1..=9999).contains(&year) || !(1..=12).contains(&month) || !(1..=31).contains(&day) {
        return None;
    }

    let month = Month::try_from(u8::try_from(month).ok()?).ok()?;
    let day = u8::try_from(day.min(u32::from(month.length(year)))).ok()?;
    Date::from_calendar_date(year, month, day).ok()
}

/// A strict calendar date, like `LocalDate.of`: no clamping of an invalid day.
fn strict_date(year: i32, month: u32, day: u32) -> Option<Date> {
    let month = Month::try_from(u8::try_from(month).ok()?).ok()?;
    let day = u8::try_from(day).ok()?;
    Date::from_calendar_date(year, month, day).ok()
}

/// `d/M/yyyy` is the day-first reading (`01/05/2026` is 1 May 2026). Only when that is invalid, fall
/// back to month/day, so `05/13/2026` is 13 May 2026.
fn slash_date(year: i32, first: u32, second: u32) -> Option<Date> {
    strict_date(year, second, first).or_else(|| strict_date(year, first, second))
}

#[cfg(test)]
mod tests {
    use super::*;
    use time::macros::date;

    fn today() -> Date {
        date!(2026 - 11 - 07)
    }

    fn eur(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn parse_eur(text: &str) -> Fields {
        parse(text, Currency::EUR, today())
    }

    fn date_of(text: &str) -> Option<Date> {
        parse_eur(text).date
    }

    #[test]
    fn two_digit_years_use_the_shared_two_digit_year_rule() {
        assert_eq!(date_of("Datum 05.03.99"), Some(date!(1999 - 03 - 05)));
        assert_eq!(date_of("Datum 05.03.26"), Some(date!(2026 - 03 - 05)));
    }

    #[test]
    fn slash_dates_keep_the_day_first_reading_unless_it_is_invalid() {
        assert_eq!(date_of("Datum 01/05/2026"), Some(date!(2026 - 05 - 01)));
        assert_eq!(date_of("Datum 05/13/2026"), Some(date!(2026 - 05 - 13)));
    }

    #[test]
    fn an_invalid_date_alone_does_not_crash_and_yields_no_date() {
        assert_eq!(date_of("Datum 31.02.26"), None);
    }

    #[test]
    fn receipt_parser_handles_german_and_english_formats() {
        let german = parse_eur("EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95");
        assert_eq!(german.total, Some(eur(129_995)));
        assert_eq!(german.date, Some(date!(2026 - 10 - 03)));
        assert!(!german.amounts.contains(&eur(310)), "date fragments are not amounts");

        let english = parse_eur("Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00");
        assert_eq!(english.total, Some(eur(119_000)));
        assert_eq!(english.date, Some(date!(2026 - 01 - 05)));

        assert_eq!(parse_eur("no numbers here").total, None);
    }

    #[test]
    fn the_last_keyword_line_wins_and_otherwise_the_maximum_is_the_total() {
        let keyword = parse_eur("Zwischensumme 1,00\nSUMME 2,00\nTotal 3,00");
        assert_eq!(keyword.total, Some(eur(300)));
        assert_eq!(keyword.amounts, vec![eur(100), eur(200), eur(300)]);

        let without_keyword = parse_eur("Artikel 1,00\nArtikel 2,50\nArtikel 2,00");
        assert_eq!(without_keyword.total, Some(eur(250)));
    }

    #[test]
    fn grouping_and_decimal_separators_are_distinguished() {
        assert_eq!(parse_eur("Betrag 1.234,56").total, Some(eur(123_456)));
        assert_eq!(parse_eur("Betrag 1,234.56").total, Some(eur(123_456)));
        assert_eq!(parse_eur("Betrag 1234.56").total, Some(eur(123_456)));
        assert_eq!(parse_eur("Betrag 1.234.56").total, None);
    }

    #[test]
    fn amounts_respect_the_currency_exponent_without_rounding() {
        let jpy = Currency::new("JPY").unwrap();
        let japanese = parse("SUMME 12,34", jpy, today());
        assert_eq!(japanese.total, None);
        assert!(japanese.amounts.is_empty());

        let kwd = Currency::new("KWD").unwrap();
        let dinar = parse("SUMME 12,34", kwd, today());
        assert_eq!(
            dinar.total,
            Some(Money {
                minor: 12_340,
                currency: kwd
            })
        );
    }

    #[test]
    fn dates_keep_the_kotlin_precedence_and_smart_clamping() {
        assert_eq!(date_of("Datum 31.02.2026"), Some(date!(2026 - 02 - 28)));
        assert_eq!(date_of("Datum 2026-04-31"), Some(date!(2026 - 04 - 30)));
        assert_eq!(date_of("Datum 13.01.2026"), Some(date!(2026 - 01 - 13)));
        assert_eq!(date_of("Datum 1999-12-31"), Some(date!(1999 - 12 - 31)));
        assert_eq!(date_of("Datum 1899-12-31"), None);
    }

    #[test]
    fn never_panics_on_random_or_truncated_input() {
        let fixtures = [
            "EDEKA\n03.10.2026 14:22\nArtikel 1.234,50\nSUMME EUR 1.299,95",
            "Invoice 2026-01-05\nSubtotal 1,000.00\nTotal 1,190.00",
            "REWE Markt GmbH\nDatum: 03.10.2026\nMilch 1,29\nBrot 2,49\nSUMME EUR 3,78",
            "Datum 05.03.99 / 05/13/2026 / 31.02.2026",
            "",
        ];

        let pool: &[char] = &[
            '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.', ',', '/', ' ', '\t', '\n', '\r', 'a', 'E', 'U', 'R',
            ':', '&', '\u{0}', '\u{7f}', '\u{FFFF}', '€',
        ];

        let mut state: u64 = 0x5eed_1234_abcd_9876;
        let mut next = || {
            state = state
                .wrapping_mul(6_364_136_223_846_793_005)
                .wrapping_add(1_442_695_040_888_963_407);
            state
        };

        for _ in 0..2_000 {
            let length = (next() % 48) as usize;
            let mut text = String::with_capacity(length);
            for _ in 0..length {
                text.push(pool[(next() as usize) % pool.len()]);
            }
            let _ = parse(&text, Currency::EUR, today());
            let _ = parse(&text, Currency::new("JPY").unwrap(), today());
        }

        for fixture in fixtures {
            for (index, _) in fixture.char_indices() {
                let _ = parse(&fixture[..index], Currency::EUR, today());
            }
            let _ = parse(fixture, Currency::EUR, today());
        }
    }
}
