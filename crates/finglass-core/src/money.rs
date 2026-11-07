//! Money as integer minor units with an explicit currency.
//!
//! Amounts are never stored or calculated as floating point, and totals never mix or convert currencies.

use std::collections::BTreeMap;
use std::fmt;
use std::str::FromStr;

use thiserror::Error;

/// Everything that can go wrong with a money value.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum MoneyError {
    /// The operands use different currencies.
    #[error("currency mismatch")]
    CurrencyMismatch,
    /// The result does not fit into `i64` minor units.
    #[error("money amount overflow")]
    Overflow,
    /// The text is not a valid amount or currency code.
    #[error("invalid money value: {0}")]
    Invalid(String),
}

/// An ISO 4217 currency code, stored as three ASCII letters.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Currency([u8; 3]);

impl Currency {
    /// The euro.
    pub const EUR: Self = Self(*b"EUR");

    /// Builds a currency code from exactly three ASCII letters, upper-casing them.
    pub fn new(text: &str) -> Result<Self, MoneyError> {
        let bytes = text.as_bytes();
        if bytes.len() != 3 || !bytes.iter().all(|byte| byte.is_ascii_alphabetic()) {
            return Err(MoneyError::Invalid(format!("invalid currency code {text:?}")));
        }

        Ok(Self([
            bytes[0].to_ascii_uppercase(),
            bytes[1].to_ascii_uppercase(),
            bytes[2].to_ascii_uppercase(),
        ]))
    }

    /// The code as three upper-case letters.
    pub fn as_str(&self) -> &str {
        std::str::from_utf8(&self.0).expect("currency codes are ASCII")
    }

    /// The number of decimal digits used by this currency.
    pub fn minor_exponent(self) -> u32 {
        match self.as_str() {
            "JPY" | "KRW" | "ISK" | "CLP" | "VND" | "XOF" | "XAF" => 0,
            "BHD" | "KWD" | "OMR" | "JOD" | "TND" | "IQD" | "LYD" => 3,
            _ => 2,
        }
    }
}

impl fmt::Display for Currency {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.as_str())
    }
}

impl FromStr for Currency {
    type Err = MoneyError;

    fn from_str(text: &str) -> Result<Self, Self::Err> {
        Self::new(text)
    }
}

/// An amount in integer minor units.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Money {
    /// The amount in the currency's minor units.
    pub minor: i64,
    /// The currency the amount is in.
    pub currency: Currency,
}

impl Money {
    /// Adds two amounts of the same currency.
    pub fn checked_add(self, other: Self) -> Result<Self, MoneyError> {
        if self.currency != other.currency {
            return Err(MoneyError::CurrencyMismatch);
        }

        let minor = self.minor.checked_add(other.minor).ok_or(MoneyError::Overflow)?;
        Ok(Self {
            minor,
            currency: self.currency,
        })
    }

    /// Whether the amount is below zero.
    pub fn is_negative(self) -> bool {
        self.minor < 0
    }
}

/// Sums per currency, never mixing or converting them.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Totals(BTreeMap<Currency, i64>);

impl Totals {
    /// Adds an amount to its currency's total.
    pub fn add(&mut self, money: Money) -> Result<(), MoneyError> {
        let total = self.0.get(&money.currency).copied().unwrap_or(0);
        let sum = total.checked_add(money.minor).ok_or(MoneyError::Overflow)?;
        self.0.insert(money.currency, sum);
        Ok(())
    }

    /// The total for one currency, or zero when it has no entries.
    pub fn get(&self, currency: Currency) -> i64 {
        self.0.get(&currency).copied().unwrap_or(0)
    }

    /// Iterates over currency totals in currency order.
    pub fn iter(&self) -> impl Iterator<Item = (&Currency, &i64)> {
        self.0.iter()
    }
}

/// Parses a plain decimal amount using the currency's exponent.
///
/// Accepts a leading sign, `.` or `,` as decimal separator, and thousands separators. When both separator characters
/// appear, the last one is the decimal separator. With a single separator followed by exactly three digits, and a
/// currency whose exponent is not three, the separator is treated as a thousands separator.
pub fn parse_decimal(text: &str, currency: Currency) -> Result<Money, MoneyError> {
    let trimmed = text.trim();
    let invalid = || MoneyError::Invalid(format!("invalid amount {text:?}"));

    if trimmed.is_empty() {
        return Err(invalid());
    }

    let (negative, unsigned) = match trimmed.as_bytes()[0] {
        b'-' => (true, &trimmed[1..]),
        b'+' => (false, &trimmed[1..]),
        _ => (false, trimmed),
    };

    if unsigned.is_empty() {
        return Err(invalid());
    }

    if !unsigned
        .bytes()
        .all(|byte| byte.is_ascii_digit() || byte == b'.' || byte == b',')
    {
        return Err(invalid());
    }

    let exponent = currency.minor_exponent();
    let dots = unsigned.matches('.').count();
    let commas = unsigned.matches(',').count();

    let (integer_part, fraction_part, thousands) = if dots > 0 && commas > 0 {
        let decimal_separator =
            if unsigned.rfind('.').expect("dot present") > unsigned.rfind(',').expect("comma present") {
                b'.'
            } else {
                b','
            };
        let thousands_separator = if decimal_separator == b'.' { b',' } else { b'.' };
        let decimal_index = unsigned
            .rfind(decimal_separator as char)
            .expect("decimal separator present");
        (
            &unsigned[..decimal_index],
            &unsigned[decimal_index + 1..],
            Some(thousands_separator),
        )
    } else if dots > 0 || commas > 0 {
        let separator = if dots > 0 { b'.' } else { b',' };
        let occurrences = if dots > 0 { dots } else { commas };

        if occurrences > 1 {
            (unsigned, "", Some(separator))
        } else {
            let separator_index = unsigned.find(separator as char).expect("separator present");
            let fraction_length = unsigned.len() - separator_index - 1;

            if fraction_length == 3 && exponent != 3 {
                (unsigned, "", Some(separator))
            } else {
                (&unsigned[..separator_index], &unsigned[separator_index + 1..], None)
            }
        }
    } else {
        (unsigned, "", None)
    };

    if !fraction_part.is_empty() && !fraction_part.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(invalid());
    }

    if fraction_part.len() > exponent as usize {
        return Err(MoneyError::Invalid(format!(
            "too many decimal places for {}",
            currency.as_str()
        )));
    }

    let integer_digits = match thousands {
        Some(separator) => grouped_integer_digits(integer_part, separator, &invalid)?,
        None => {
            if integer_part.is_empty() {
                "0".to_owned()
            } else if integer_part.bytes().all(|byte| byte.is_ascii_digit()) {
                integer_part.to_owned()
            } else {
                return Err(invalid());
            }
        }
    };

    let mut fraction_digits = fraction_part.to_owned();
    while fraction_digits.len() < exponent as usize {
        fraction_digits.push('0');
    }

    let integer: i128 = integer_digits.parse().map_err(|_| MoneyError::Overflow)?;
    let fraction: i128 = if fraction_digits.is_empty() {
        0
    } else {
        fraction_digits.parse().map_err(|_| MoneyError::Overflow)?
    };

    let scale = 10_i128.checked_pow(exponent).ok_or(MoneyError::Overflow)?;
    let mut value = integer
        .checked_mul(scale)
        .and_then(|value| value.checked_add(fraction))
        .ok_or(MoneyError::Overflow)?;

    if negative {
        value = -value;
    }

    let minor = i64::try_from(value).map_err(|_| MoneyError::Overflow)?;
    Ok(Money { minor, currency })
}

/// Removes thousands separators and rejects malformed digit groups.
fn grouped_integer_digits(text: &str, separator: u8, invalid: &impl Fn() -> MoneyError) -> Result<String, MoneyError> {
    let groups: Vec<&str> = text.split(separator as char).collect();

    if groups.is_empty() {
        return Err(invalid());
    }

    for (index, group) in groups.iter().enumerate() {
        if group.is_empty() || !group.bytes().all(|byte| byte.is_ascii_digit()) {
            return Err(invalid());
        }

        if index == 0 {
            if group.len() > 3 || (groups.len() > 1 && group.starts_with('0')) {
                return Err(invalid());
            }
        } else if group.len() != 3 {
            return Err(invalid());
        }
    }

    Ok(groups.concat())
}

/// Formats an amount as plain text with exactly the currency's number of decimals.
pub fn format_plain(money: Money) -> String {
    let exponent = money.currency.minor_exponent();

    if exponent == 0 {
        return money.minor.to_string();
    }

    let scale = 10_u128.pow(exponent);
    let absolute = (money.minor as i128).unsigned_abs();
    let whole = absolute / scale;
    let fraction = absolute % scale;
    let sign = if money.minor < 0 { "-" } else { "" };

    format!("{sign}{whole}.{fraction:0width$}", width = exponent as usize)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn eur() -> Currency {
        Currency::EUR
    }

    fn amount(minor: i64, currency: Currency) -> Money {
        Money { minor, currency }
    }

    #[test]
    fn currency_codes_are_three_uppercase_letters() {
        assert_eq!(Currency::new("eur").unwrap(), eur());
        assert_eq!(Currency::new("EUR").unwrap().as_str(), "EUR");
        assert_eq!("usd".parse::<Currency>().unwrap().as_str(), "USD");
        assert!(Currency::new("EU").is_err());
        assert!(Currency::new("EURO").is_err());
        assert!(Currency::new("E1R").is_err());
    }

    #[test]
    fn minor_exponents_match_the_currency() {
        assert_eq!(Currency::new("JPY").unwrap().minor_exponent(), 0);
        assert_eq!(Currency::new("KWD").unwrap().minor_exponent(), 3);
        assert_eq!(eur().minor_exponent(), 2);
    }

    #[test]
    fn parses_all_supported_decimal_forms() {
        assert_eq!(parse_decimal("-12.34", eur()).unwrap(), amount(-1234, eur()));
        assert_eq!(parse_decimal("12,34", eur()).unwrap(), amount(1234, eur()));
        assert_eq!(parse_decimal("1.234,56", eur()).unwrap(), amount(123456, eur()));
        assert_eq!(parse_decimal("1,234.56", eur()).unwrap(), amount(123456, eur()));
        assert_eq!(parse_decimal("+5", eur()).unwrap(), amount(500, eur()));
        assert_eq!(parse_decimal("0", eur()).unwrap(), amount(0, eur()));
    }

    #[test]
    fn three_digit_fractions_depend_on_the_currency() {
        assert_eq!(parse_decimal("12.345", eur()).unwrap(), amount(1_234_500, eur()));
        let kwd = Currency::new("KWD").unwrap();
        assert_eq!(parse_decimal("12.345", kwd).unwrap(), amount(12345, kwd));
    }

    #[test]
    fn malformed_grouping_is_an_error() {
        assert!(parse_decimal("1.2.3", eur()).is_err());
        assert!(parse_decimal("12,345,6", eur()).is_err());
    }

    #[test]
    fn too_many_fraction_digits_is_an_error() {
        assert!(parse_decimal("0.001", eur()).is_err());
        assert!(parse_decimal("1.2345", eur()).is_err());
    }

    #[test]
    fn jpy_uses_whole_units() {
        let jpy = Currency::new("JPY").unwrap();
        assert_eq!(parse_decimal("500", jpy).unwrap(), amount(500, jpy));
    }

    #[test]
    fn empty_and_other_text_is_an_error() {
        assert!(parse_decimal("", eur()).is_err());
        assert!(parse_decimal("   ", eur()).is_err());
        assert!(parse_decimal("abc", eur()).is_err());
        assert!(parse_decimal("+", eur()).is_err());
        assert!(parse_decimal("1e3", eur()).is_err());
    }

    #[test]
    fn format_plain_round_trips() {
        for (minor, expected) in [
            (-123456, "-1234.56"),
            (0, "0.00"),
            (5, "0.05"),
            (100, "1.00"),
            (-1, "-0.01"),
        ] {
            let money = amount(minor, eur());
            assert_eq!(format_plain(money), expected);
            assert_eq!(parse_decimal(expected, eur()).unwrap(), money);
        }

        let jpy = Currency::new("JPY").unwrap();
        assert_eq!(format_plain(amount(500, jpy)), "500");
        assert_eq!(parse_decimal("500", jpy).unwrap(), amount(500, jpy));

        let kwd = Currency::new("KWD").unwrap();
        assert_eq!(format_plain(amount(12345, kwd)), "12.345");
        assert_eq!(parse_decimal("12.345", kwd).unwrap(), amount(12345, kwd));
    }

    #[test]
    fn checked_addition_rejects_mismatches_and_overflow() {
        let usd = Currency::new("USD").unwrap();
        assert_eq!(
            amount(1, eur()).checked_add(amount(2, usd)).unwrap_err(),
            MoneyError::CurrencyMismatch
        );
        assert_eq!(
            amount(1, eur()).checked_add(amount(2, eur())).unwrap(),
            amount(3, eur())
        );
        assert_eq!(
            amount(i64::MAX, eur()).checked_add(amount(1, eur())).unwrap_err(),
            MoneyError::Overflow
        );
    }

    #[test]
    fn totals_keep_currencies_apart() {
        let usd = Currency::new("USD").unwrap();
        let mut totals = Totals::default();

        totals.add(amount(100, eur())).unwrap();
        totals.add(amount(250, eur())).unwrap();
        totals.add(amount(50, usd)).unwrap();

        assert_eq!(totals.get(eur()), 350);
        assert_eq!(totals.get(usd), 50);
        assert_eq!(totals.get(Currency::new("GBP").unwrap()), 0);

        let entries: Vec<(Currency, i64)> = totals.iter().map(|(currency, total)| (*currency, *total)).collect();
        assert_eq!(entries, vec![(eur(), 350), (usd, 50)]);
    }

    #[test]
    fn negative_amounts_are_reported() {
        assert!(amount(-1, eur()).is_negative());
        assert!(!amount(0, eur()).is_negative());
        assert!(!amount(1, eur()).is_negative());
    }
}
