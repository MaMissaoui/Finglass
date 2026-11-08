//! Amount, date and number formats per language, ported from FinDesk's `util/Formats.kt`.
//!
//! Money values are [`Money`] in integer minor units and use the currency's exponent. No money is ever formatted
//! through `f64`.

use std::str::FromStr;

use rust_decimal::{Decimal, RoundingStrategy};
use time::{Date, Month, OffsetDateTime};

use crate::i18n::Language;
use crate::iban;
use crate::money::{Currency, Money};

/// The no-break space that keeps a number and its unit together.
pub(crate) const NO_BREAK_SPACE: char = '\u{A0}';

/// The decimal and grouping separators of `lang`, as Java's `DecimalFormatSymbols` uses them.
fn separators(lang: Language) -> (char, char) {
    match lang {
        Language::En => ('.', ','),
        Language::De => (',', '.'),
        // Java 21's French locale groups with a narrow no-break space.
        Language::Fr => (',', '\u{202f}'),
    }
}

/// The currency symbol as [`money`] shows it, including the trailing space of an unknown code.
///
/// FinDesk uses `Currency.getInstance(code).getSymbol(Locale.US)`: a code Java knows shows its US symbol, which is
/// the code itself for most currencies ("CHF1,234.50"); only a code Java doesn't know gets "CODE " with a space.
/// The tables below are Java 21's (`Currency.getAvailableCurrencies()`), generated once from the JDK.
pub(crate) fn symbol_for(currency: Currency) -> String {
    match currency.as_str() {
        "AUD" => "A$".to_owned(),
        "BRL" => "R$".to_owned(),
        "CAD" => "CA$".to_owned(),
        "CNY" => "CN¥".to_owned(),
        "EUR" => "€".to_owned(),
        "GBP" => "£".to_owned(),
        "HKD" => "HK$".to_owned(),
        "ILS" => "₪".to_owned(),
        "INR" => "₹".to_owned(),
        "JPY" => "¥".to_owned(),
        "KRW" => "₩".to_owned(),
        "MXN" => "MX$".to_owned(),
        "NZD" => "NZ$".to_owned(),
        "PHP" => "₱".to_owned(),
        "TWD" => "NT$".to_owned(),
        "USD" => "$".to_owned(),
        "VND" => "₫".to_owned(),
        "XAF" => "FCFA".to_owned(),
        "XCD" => "EC$".to_owned(),
        "XOF" => "F CFA".to_owned(),
        "XPF" => "CFPF".to_owned(),
        "XXX" => "¤".to_owned(),
        code if JAVA_CURRENCY_CODES.binary_search(&code).is_ok() => code.to_owned(),
        other => format!("{other} "),
    }
}

/// Every ISO 4217 code Java 21 knows, sorted (for the binary search in [`symbol_for`]).
const JAVA_CURRENCY_CODES: &[&str] = &[
    "ADP", "AED", "AFA", "AFN", "ALL", "AMD", "ANG", "AOA", "ARS", "ATS", "AUD", "AWG", "AYM", "AZM", "AZN", "BAM",
    "BBD", "BDT", "BEF", "BGL", "BGN", "BHD", "BIF", "BMD", "BND", "BOB", "BOV", "BRL", "BSD", "BTN", "BWP", "BYB",
    "BYN", "BYR", "BZD", "CAD", "CDF", "CHE", "CHF", "CHW", "CLF", "CLP", "CNY", "COP", "COU", "CRC", "CSD", "CUC",
    "CUP", "CVE", "CYP", "CZK", "DEM", "DJF", "DKK", "DOP", "DZD", "EEK", "EGP", "ERN", "ESP", "ETB", "EUR", "FIM",
    "FJD", "FKP", "FRF", "GBP", "GEL", "GHC", "GHS", "GIP", "GMD", "GNF", "GRD", "GTQ", "GWP", "GYD", "HKD", "HNL",
    "HRK", "HTG", "HUF", "IDR", "IEP", "ILS", "INR", "IQD", "IRR", "ISK", "ITL", "JMD", "JOD", "JPY", "KES", "KGS",
    "KHR", "KMF", "KPW", "KRW", "KWD", "KYD", "KZT", "LAK", "LBP", "LKR", "LRD", "LSL", "LTL", "LUF", "LVL", "LYD",
    "MAD", "MDL", "MGA", "MGF", "MKD", "MMK", "MNT", "MOP", "MRO", "MRU", "MTL", "MUR", "MVR", "MWK", "MXN", "MXV",
    "MYR", "MZM", "MZN", "NAD", "NGN", "NIO", "NLG", "NOK", "NPR", "NZD", "OMR", "PAB", "PEN", "PGK", "PHP", "PKR",
    "PLN", "PTE", "PYG", "QAR", "ROL", "RON", "RSD", "RUB", "RUR", "RWF", "SAR", "SBD", "SCR", "SDD", "SDG", "SEK",
    "SGD", "SHP", "SIT", "SKK", "SLE", "SLL", "SOS", "SRD", "SRG", "SSP", "STD", "STN", "SVC", "SYP", "SZL", "THB",
    "TJS", "TMM", "TMT", "TND", "TOP", "TPE", "TRL", "TRY", "TTD", "TWD", "TZS", "UAH", "UGX", "USD", "USN", "USS",
    "UYI", "UYU", "UZS", "VEB", "VED", "VEF", "VES", "VND", "VUV", "WST", "XAD", "XAF", "XAG", "XAU", "XBA", "XBB",
    "XBC", "XBD", "XCD", "XCG", "XDR", "XFO", "XFU", "XOF", "XPD", "XPF", "XPT", "XSU", "XTS", "XUA", "XXX", "YER",
    "YUM", "ZAR", "ZMK", "ZMW", "ZWD", "ZWG", "ZWL", "ZWN", "ZWR",
];

/// The currency's symbol as [`money`] shows it, without the trailing space of an unknown code.
pub fn currency_symbol(currency: Currency) -> String {
    symbol_for(currency).trim().to_owned()
}

/// Groups a non-empty digit string in blocks of three from the right.
fn group_digits(digits: &str, separator: char) -> String {
    let chars: Vec<char> = digits.chars().collect();
    let mut grouped = String::with_capacity(digits.len() + digits.len() / 3);
    for (index, character) in chars.iter().enumerate() {
        if index > 0 && (chars.len() - index).is_multiple_of(3) {
            grouped.push(separator);
        }
        grouped.push(*character);
    }
    grouped
}

/// "€1,234.56" / "-€1,234.56"; with `signed`, positive amounts get a leading "+".
///
/// German puts the number first, separated from the trimmed symbol by a no-break space: "2.450,37 €".
pub fn money(money: Money, lang: Language, signed: bool) -> String {
    let (decimal_separator, grouping_separator) = separators(lang);
    let exponent = money.currency.minor_exponent();
    let absolute = (money.minor as i128).unsigned_abs();
    let scale = 10_u128.pow(exponent);
    let whole = absolute / scale;
    let fraction = absolute % scale;
    let digits = group_digits(&whole.to_string(), grouping_separator);

    let formatted = if exponent == 0 {
        digits
    } else {
        format!(
            "{digits}{decimal_separator}{fraction:0width$}",
            width = exponent as usize
        )
    };
    let sign = if money.minor < 0 {
        "-"
    } else if signed && money.minor > 0 {
        "+"
    } else {
        ""
    };
    let symbol = symbol_for(money.currency);

    if lang == Language::De {
        format!("{sign}{formatted}{NO_BREAK_SPACE}{}", symbol.trim())
    } else {
        format!("{sign}{symbol}{formatted}")
    }
}

/// Whole currency units for chart axes: "€4,000" / "€4.000"; German puts the number first: "4.000 €".
pub fn whole_money(money: Money, lang: Language) -> String {
    let (_, grouping_separator) = separators(lang);
    let exponent = money.currency.minor_exponent();
    let absolute = (money.minor as i128).unsigned_abs();
    let scale = 10_u128.pow(exponent);
    // setScale(0, HALF_UP) on the absolute amount.
    let rounded = if exponent == 0 {
        absolute
    } else {
        (absolute + scale / 2) / scale
    };
    let digits = group_digits(&rounded.to_string(), grouping_separator);
    let sign = if money.minor < 0 { "-" } else { "" };
    let symbol = symbol_for(money.currency);

    if lang == Language::De {
        format!("{sign}{digits}{NO_BREAK_SPACE}{}", symbol.trim())
    } else {
        format!("{sign}{symbol}{digits}")
    }
}

/// Whole percentages of one share: "49%" in English, "49 %" with a no-break space in German and French.
///
/// Tiny shares between 0 and 0.5 % read "<1%" / "<1 %"; a zero share shows "0".
pub fn percent(share: f64, lang: Language) -> String {
    let separator = if lang == Language::En {
        String::new()
    } else {
        NO_BREAK_SPACE.to_string()
    };
    let text = if share <= 0.0 {
        "0".to_owned()
    } else if share < 0.005 {
        "<1".to_owned()
    } else {
        ((share * 100.0).round() as i64).clamp(0, 100).to_string()
    };

    format!("{text}{separator}%")
}

/// Whole percentages of `amounts` against their sum, rounded by the largest remainder, so the list always sums to
/// exactly 100 when it shows every share (`[1, 1, 1]` → 34, 33, 33). Empty or all-zero lists give all zeros.
pub fn shares(amounts: &[Money]) -> Vec<i64> {
    if amounts.is_empty() {
        return Vec::new();
    }

    let total: i128 = amounts.iter().map(|amount| i128::from(amount.minor)).sum();
    if total == 0 {
        return vec![0; amounts.len()];
    }

    const SCALE: i128 = 10_000_000_000;
    let raw: Vec<i128> = amounts
        .iter()
        .map(|amount| (i128::from(amount.minor) * 100 * SCALE) / total)
        .collect();
    let floors: Vec<i128> = raw.iter().map(|value| value.div_euclid(SCALE)).collect();
    let missing = 100 - floors.iter().sum::<i128>();

    let mut order: Vec<usize> = (0..amounts.len()).collect();
    order.sort_by(|&left, &right| {
        let left_remainder = raw[left] - floors[left] * SCALE;
        let right_remainder = raw[right] - floors[right] * SCALE;
        right_remainder.cmp(&left_remainder).then(left.cmp(&right))
    });

    let mut result: Vec<i64> = floors.iter().map(|floor| *floor as i64).collect();
    if missing > 0 {
        for index in order.into_iter().take(missing as usize) {
            result[index] += 1;
        }
    }

    result
}

/// "dd.MM.yyyy".
pub fn date(date: Date) -> String {
    format!("{:02}.{:02}.{:04}", date.day(), date.month() as u8, date.year())
}

/// Parses what users type in date fields: "05.11.2026" or "5.11.2026". Strict, so an impossible day such as
/// 31.02. is rejected instead of silently becoming 28.02.
pub fn parse_date(input: &str) -> Option<Date> {
    let parts: Vec<&str> = input.trim().split('.').collect();
    if parts.len() != 3 {
        return None;
    }

    let day = one_or_two_digits(parts[0])?;
    let month = one_or_two_digits(parts[1])?;
    let year = four_digits(parts[2])?;
    Date::from_calendar_date(
        year,
        Month::try_from(u8::try_from(month).ok()?).ok()?,
        u8::try_from(day).ok()?,
    )
    .ok()
}

fn one_or_two_digits(part: &str) -> Option<u32> {
    if (1..=2).contains(&part.len()) && part.bytes().all(|byte| byte.is_ascii_digit()) {
        part.parse().ok()
    } else {
        None
    }
}

fn four_digits(part: &str) -> Option<i32> {
    if part.len() == 4 && part.bytes().all(|byte| byte.is_ascii_digit()) {
        part.parse().ok()
    } else {
        None
    }
}

/// "dd.MM.yyyy HH:mm".
pub fn date_time(date_time: OffsetDateTime) -> String {
    format!(
        "{} {:02}:{:02}",
        date(date_time.date()),
        date_time.hour(),
        date_time.minute()
    )
}

/// Groups an IBAN in blocks of four: "DE89 3704 0044 ...".
pub fn iban(iban: &str) -> String {
    iban::format(iban)
}

/// Parses a decimal out of untrusted file text, with FinDesk's import bounds: values with more than 15 integer
/// digits or more than 10 decimal places, or an input longer than 64 characters, are rejected.
pub fn parse_decimal(text: &str) -> Option<Decimal> {
    if text.chars().count() > 64 {
        return None;
    }
    let trimmed = text.trim();
    let value = Decimal::from_str(trimmed).ok()?;
    let scale = value.scale() as i32;
    if scale > 10 {
        return None;
    }
    let mantissa_digits = value.mantissa().unsigned_abs().to_string().len() as i32;
    let integer_digits = mantissa_digits - scale;
    if integer_digits > 15 {
        return None;
    }
    Some(value)
}

/// Parses user input like "1.234,56", "1,234.56", "-12.5" or "12". Returns `None` if it isn't a number.
pub fn parse_amount(input: &str) -> Option<Decimal> {
    let mut text = input.trim().replace(['€', ' ', NO_BREAK_SPACE], "");
    if text.is_empty() {
        return None;
    }

    let last_comma = text.rfind(',');
    let last_dot = text.rfind('.');
    text = if matches!((last_comma, last_dot), (Some(comma), Some(dot)) if comma > dot)
        || (last_comma.is_some() && last_dot.is_none())
    {
        text.replace('.', "").replace(',', ".")
    } else {
        text.replace(',', "")
    };

    let mut value = parse_decimal(&text)?.round_dp_with_strategy(2, RoundingStrategy::MidpointAwayFromZero);
    value.rescale(2);
    Some(value)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::i18n::{Arg, Key};
    use time::macros::{date, datetime};

    fn eur(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn usd(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::new("USD").unwrap(),
        }
    }

    #[test]
    fn money_formatting() {
        assert_eq!(money(eur(123_450), Language::En, false), "€1,234.50");
        assert_eq!(money(eur(-4_567), Language::En, false), "-€45.67");
        assert_eq!(money(eur(320_000), Language::En, true), "+€3,200.00");
        assert_eq!(money(eur(0), Language::En, true), "€0.00");
        assert_eq!(currency_symbol(Currency::EUR), "€");
        assert_eq!(currency_symbol(Currency::new("USD").unwrap()), "$");
    }

    #[test]
    fn money_uses_the_language_separators() {
        assert_eq!(money(eur(123_450), Language::De, false), "1.234,50\u{a0}€");
        assert_eq!(money(eur(123_450), Language::Fr, false), "€1\u{202f}234,50");
    }

    #[test]
    fn money_uses_the_currency_exponent() {
        let jpy = Currency::new("JPY").unwrap();
        assert_eq!(
            money(
                Money {
                    minor: 1234,
                    currency: jpy
                },
                Language::En,
                false
            ),
            "¥1,234"
        );

        let kwd = Currency::new("KWD").unwrap();
        assert_eq!(
            money(
                Money {
                    minor: 12_345,
                    currency: kwd
                },
                Language::En,
                false
            ),
            "KWD12.345",
            "Java knows KWD, so it shows the bare code without a space"
        );
        let unknown = Currency::new("QQQ").unwrap();
        assert_eq!(
            money(
                Money {
                    minor: 100,
                    currency: unknown
                },
                Language::En,
                false
            ),
            "QQQ 1.00"
        );
        let chf = Currency::new("CHF").unwrap();
        assert_eq!(
            money(
                Money {
                    minor: 123_450,
                    currency: chf
                },
                Language::En,
                false
            ),
            "CHF1,234.50"
        );
    }

    #[test]
    fn whole_money_rounds_half_up() {
        assert_eq!(whole_money(eur(400_000), Language::En), "€4,000");
        assert_eq!(whole_money(eur(400_050), Language::En), "€4,001");
        assert_eq!(whole_money(eur(-400_049), Language::En), "-€4,000");
        assert_eq!(whole_money(eur(400_000), Language::De), "4.000\u{a0}€");
    }

    #[test]
    fn german_money_puts_the_number_before_the_symbol() {
        let chf = Currency::new("CHF").unwrap();
        let jpy = Currency::new("JPY").unwrap();

        assert_eq!(money(eur(245_037), Language::De, false), "2.450,37\u{a0}€");
        assert_eq!(money(eur(-500_000), Language::De, false), "-5.000,00\u{a0}€");
        assert_eq!(money(eur(43_100), Language::De, true), "+431,00\u{a0}€");
        assert_eq!(whole_money(eur(400_000), Language::De), "4.000\u{a0}€");
        assert_eq!(money(usd(50_000), Language::De, false), "500,00\u{a0}$");
        assert_eq!(
            money(
                Money {
                    minor: 1_200,
                    currency: chf
                },
                Language::De,
                false
            ),
            "12,00\u{a0}CHF"
        );
        assert_eq!(
            money(
                Money {
                    minor: 1_234,
                    currency: jpy
                },
                Language::De,
                false
            ),
            "1.234\u{a0}¥"
        );

        assert_eq!(money(eur(245_037), Language::En, false), "€2,450.37");
        assert_eq!(whole_money(eur(400_000), Language::En), "€4,000");
        assert_eq!(money(eur(245_037), Language::Fr, false), "€2\u{202f}450,37");
        assert_eq!(whole_money(eur(400_000), Language::Fr), "€4\u{202f}000");
    }

    #[test]
    fn german_money_round_trips_through_parse_amount() {
        for minor in [-456_789, 0, 43_100, 1_234_567_890_123] {
            for signed in [false, true] {
                let text = money(eur(minor), Language::De, signed);
                assert_eq!(
                    parse_amount(&text),
                    Some(Decimal::new(minor, 2)),
                    "round trip for {text:?}"
                );
            }
        }
    }

    #[test]
    fn percent_is_whole_and_language_specific() {
        assert_eq!(percent(0.49, Language::En), "49%");
        assert_eq!(percent(1.0, Language::En), "100%");
        assert_eq!(percent(0.0, Language::En), "0%");
        assert_eq!(percent(0.004, Language::En), "<1%");
        assert_eq!(percent(0.0004, Language::En), "<1%");

        assert_eq!(percent(0.49, Language::De), "49\u{a0}%");
        assert_eq!(percent(0.0, Language::De), "0\u{a0}%");
        assert_eq!(percent(0.0001, Language::De), "<1\u{a0}%");
        assert_eq!(percent(0.1234, Language::De), "12\u{a0}%");

        assert_eq!(percent(0.03, Language::Fr), "3\u{a0}%");
    }

    #[test]
    fn shares_round_by_largest_remainder_and_sum_to_exactly_100() {
        assert_eq!(shares(&[eur(1), eur(1), eur(1)]), vec![34, 33, 33]);
        assert_eq!(shares(&[eur(3_000), eur(7_000)]), vec![30, 70]);

        let ten: Vec<Money> = (1..=10).map(eur).collect();
        assert_eq!(shares(&ten).iter().sum::<i64>(), 100);
        // Sorted ten ways, as the split bar receives them: 10 first
        let reversed: Vec<Money> = (1..=10).rev().map(eur).collect();
        assert_eq!(shares(&reversed), vec![18, 16, 15, 13, 11, 9, 7, 5, 4, 2]);
    }

    #[test]
    fn shares_handle_empty_and_all_zero_lists() {
        assert!(shares(&[]).is_empty());
        assert_eq!(shares(&[eur(0), eur(0), eur(0)]), vec![0, 0, 0]);
    }

    #[test]
    fn the_amount_label_carries_the_currency_symbol() {
        assert_eq!(
            crate::i18n::format(Language::En, Key::AmountIn, &[Arg::Str(currency_symbol(Currency::EUR))]),
            "Amount (€)"
        );
        assert_eq!(
            crate::i18n::format(
                Language::En,
                Key::AmountIn,
                &[Arg::Str(currency_symbol(Currency::new("USD").unwrap()))]
            ),
            "Amount ($)"
        );
    }

    #[test]
    fn date_input_accepts_short_and_padded_days_and_rejects_impossible_ones() {
        assert_eq!(parse_date("5.11.2026"), Some(date!(2026 - 11 - 05)));
        assert_eq!(parse_date(" 05.11.2026 "), Some(date!(2026 - 11 - 05)));
        assert_eq!(parse_date("29.02.2028"), Some(date!(2028 - 02 - 29)));
        assert_eq!(parse_date("28.02.2026"), Some(date!(2026 - 02 - 28)));
        // Lenient parsing turned these into 28.02.; a scheduled transfer would then run on another day
        assert_eq!(parse_date("31.02.2026"), None);
        assert_eq!(parse_date("29.02.2027"), None);
        assert_eq!(parse_date("32.01.2026"), None);
    }

    #[test]
    fn date_and_date_time_format_as_before() {
        assert_eq!(date(date!(2026 - 11 - 05)), "05.11.2026");
        assert_eq!(date_time(datetime!(2026 - 11 - 05 13:07 UTC)), "05.11.2026 13:07");
    }

    #[test]
    fn iban_grouping() {
        assert_eq!(iban("DE89370400440532013000"), "DE89 3704 0044 0532 0130 00");
    }

    #[test]
    fn amount_parsing_accepts_german_and_english_input() {
        assert_eq!(parse_amount("1.234,56"), Some(Decimal::new(123_456, 2)));
        assert_eq!(parse_amount("1,234.56"), Some(Decimal::new(123_456, 2)));
        assert_eq!(parse_amount("-12,5"), Some(Decimal::new(-1_250, 2)));
        assert_eq!(parse_amount("12 €"), Some(Decimal::new(1_200, 2)));
        assert_eq!(parse_amount("2.450,37 €"), Some(Decimal::new(245_037, 2)));
        assert_eq!(
            parse_amount("2.450,37\u{a0}€"),
            Some(Decimal::new(245_037, 2)),
            "the new German form uses a no-break space"
        );
        assert_eq!(
            parse_amount("€2.450,37"),
            Some(Decimal::new(245_037, 2)),
            "the old German form still parses"
        );
        assert_eq!(parse_amount("abc"), None);
        assert_eq!(parse_amount(""), None);
    }

    #[test]
    fn parse_decimal_rejects_numbers_that_would_expand_into_billions_of_digits() {
        assert_eq!(parse_decimal("1E999999999"), None);
        assert_eq!(parse_decimal("-1E999999999"), None);
        assert_eq!(parse_decimal("1E-999999999"), None);
    }

    #[test]
    fn parse_decimal_rejects_values_out_of_the_decimal_15_2_range() {
        assert_eq!(parse_decimal("1E15"), None, "16 integer digits");
        assert_eq!(parse_decimal("1000000000000000"), None, "16 integer digits");
        assert_eq!(
            parse_decimal("99999999999999.99"),
            Some(Decimal::new(9_999_999_999_999_999, 2)),
            "15 integer digits still fits"
        );
    }

    #[test]
    fn parse_decimal_rejects_over_long_input_and_more_than_10_decimals() {
        let digits65 = "12345678901234567890123456789012345678901234567890123456789012345";
        assert_eq!(parse_decimal(digits65), None);
        assert_eq!(parse_decimal("0.12345678901"), None, "11 decimal places");
    }

    #[test]
    fn parse_amount_caps_the_same_values_and_keeps_its_separator_handling() {
        assert_eq!(parse_amount("1E999999999"), None);
        assert_eq!(parse_amount("-1E999999999"), None);
        assert_eq!(parse_amount("1E-999999999"), None);
        assert_eq!(parse_amount("1E15"), None);
        assert_eq!(
            parse_amount("12345678901234567890123456789012345678901234567890123456789012345"),
            None
        );
        assert_eq!(parse_amount("0.12345678901"), None);
        assert_eq!(parse_amount("1000000000000000"), None);
        assert_eq!(
            parse_amount("99999999999999.99"),
            Some(Decimal::new(9_999_999_999_999_999, 2))
        );

        assert_eq!(
            parse_amount("1.234,56"),
            Some(Decimal::new(123_456, 2)),
            "German format"
        );
        assert_eq!(
            parse_amount("1,234.56"),
            Some(Decimal::new(123_456, 2)),
            "English format"
        );
        assert_eq!(parse_amount("-12,30"), Some(Decimal::new(-1_230, 2)), "German format");
    }

    #[test]
    fn an_unknown_currency_code_keeps_its_code_and_sign() {
        let xyz = Currency::new("XYZ").unwrap();
        assert_eq!(
            money(
                Money {
                    minor: -5,
                    currency: xyz
                },
                Language::En,
                false
            ),
            "-XYZ 0.05"
        );
        assert_eq!(currency_symbol(xyz), "XYZ");
    }

    #[test]
    fn dollars_use_the_dollar_symbol() {
        assert_eq!(money(usd(50), Language::En, false), "$0.50");
    }
}
