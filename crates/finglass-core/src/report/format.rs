//! Amount, date and month formatting for the report exports.
//!
//! Ported from FinDesk's `util/Formats.kt` and the number handling of `TaxReportExporter.kt`.
//! Reports use the app language's separators; dates always use `dd.MM.yyyy`.

use crate::i18n::Language;
use crate::money::Money;

pub use crate::format::{currency_symbol, date};

/// The full month name in the app language, like Java's `Month.getDisplayName(TextStyle.FULL, locale)`.
pub fn month_name(language: Language, month: u8) -> &'static str {
    const EN: [&str; 12] = [
        "January",
        "February",
        "March",
        "April",
        "May",
        "June",
        "July",
        "August",
        "September",
        "October",
        "November",
        "December",
    ];
    const DE: [&str; 12] = [
        "Januar",
        "Februar",
        "März",
        "April",
        "Mai",
        "Juni",
        "Juli",
        "August",
        "September",
        "Oktober",
        "November",
        "Dezember",
    ];
    const FR: [&str; 12] = [
        "janvier",
        "février",
        "mars",
        "avril",
        "mai",
        "juin",
        "juillet",
        "août",
        "septembre",
        "octobre",
        "novembre",
        "décembre",
    ];

    let names = match language {
        Language::En => &EN,
        Language::De => &DE,
        Language::Fr => &FR,
    };
    names[usize::from(month.clamp(1, 12)) - 1]
}

/// `Formats.money`: a currency symbol followed by the grouped amount, two decimals.
pub fn money(amount: Money, language: Language, signed: bool) -> String {
    let mut text = String::new();
    if amount.minor < 0 {
        text.push('-');
    } else if signed && amount.minor > 0 {
        text.push('+');
    }
    text.push_str(&crate::format::symbol_for(amount.currency));
    text.push_str(&grouped(amount, language));
    text
}

/// The tax-booking PDF amount without a currency symbol; same separators as [`money`].
pub fn pdf_amount(amount: Money, language: Language, signed: bool) -> String {
    let mut text = String::new();
    if amount.minor < 0 {
        text.push('-');
    } else if signed && amount.minor > 0 {
        text.push('+');
    }
    text.push_str(&grouped(amount, language));
    text
}

/// Groups the absolute value with the language's separators, always with two decimal places.
fn grouped(amount: Money, language: Language) -> String {
    let exponent = amount.currency.minor_exponent();
    let absolute = amount.minor.unsigned_abs();

    // Kotlin's exports use "#,##0.00" after `setScale(2)`, so the result always has two decimals.
    let cents = match exponent {
        0 => absolute.saturating_mul(100),
        1 => absolute.saturating_mul(10),
        2 => absolute,
        3 => (absolute.saturating_add(5)) / 10,
        other => {
            let divisor = 10_u64.saturating_pow(other.saturating_sub(2));
            (absolute.saturating_add(divisor / 2)) / divisor
        }
    };

    let whole = cents / 100;
    let fraction = cents % 100;
    let (group, decimal) = match language {
        Language::En => (',', '.'),
        Language::De => ('.', ','),
        Language::Fr => ('\u{202f}', ','),
    };

    let digits = whole.to_string();
    let mut grouped = String::with_capacity(digits.len() + digits.len() / 3 + 4);
    for (index, character) in digits.chars().enumerate() {
        if index > 0 && (digits.len() - index) % 3 == 0 {
            grouped.push(group);
        }
        grouped.push(character);
    }

    format!("{grouped}{decimal}{fraction:02}")
}
