//! Converts FinDesk's stored values into Finglass's formats.
//!
//! Money leaves FinDesk as `REAL` or `INTEGER`. It is parsed through `Decimal`, never through a float multiplication,
//! and a value with more decimals than the currency allows is rejected instead of rounded.

use std::str::FromStr;

use rust_decimal::Decimal;
use time::macros::format_description;
use time::{Date, OffsetDateTime, Time, UtcOffset};

use crate::import::amount_to_money;
use crate::model::{AccountType, RuleDirection, RuleField};
use crate::money::Currency;
use crate::repo;

use super::source::SourceNumber;

/// Why a source value could not be converted.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ConvertError {
    /// A value is missing or invalid.
    Invalid,
    /// A monetary value has more decimal places than its currency allows.
    FractionalAmount,
}

pub(crate) fn decimal(value: &SourceNumber) -> Option<Decimal> {
    match value {
        SourceNumber::Integer(value) => Some(Decimal::from(*value)),
        SourceNumber::Real(value) => Decimal::from_str(&format!("{value}")).ok(),
        SourceNumber::Text(text) => Decimal::from_str(text.trim()).ok(),
    }
}

pub(crate) fn required_minor(value: Option<&SourceNumber>, currency: Currency) -> Result<i64, ConvertError> {
    let value = value.ok_or(ConvertError::Invalid)?;
    let amount = decimal(value).ok_or(ConvertError::Invalid)?;
    let normalized = amount.normalize();

    if normalized.scale() > currency.minor_exponent() {
        return Err(ConvertError::FractionalAmount);
    }

    amount_to_money(amount, currency)
        .map(|money| money.minor)
        .map_err(|_| ConvertError::Invalid)
}

pub(crate) fn optional_minor(value: Option<&SourceNumber>, currency: Currency) -> Result<Option<i64>, ConvertError> {
    value.map(|value| required_minor(Some(value), currency)).transpose()
}

pub(crate) fn required_date(value: Option<&str>) -> Result<Date, ConvertError> {
    let value = value.ok_or(ConvertError::Invalid)?;
    Date::parse(value, format_description!("[year]-[month]-[day]")).map_err(|_| ConvertError::Invalid)
}

pub(crate) fn optional_date(value: Option<&str>) -> Result<Option<Date>, ConvertError> {
    value.map(|value| required_date(Some(value))).transpose()
}

pub(crate) fn date_text(date: Date) -> Result<String, ConvertError> {
    repo::format_date(date).map_err(|_| ConvertError::Invalid)
}

pub(crate) fn required_timestamp(value: Option<&str>, offset: UtcOffset) -> Result<String, ConvertError> {
    let value = value.ok_or(ConvertError::Invalid)?;
    let timestamp = parse_timestamp(value, offset).ok_or(ConvertError::Invalid)?;
    repo::format_timestamp(timestamp).map_err(|_| ConvertError::Invalid)
}

pub(crate) fn optional_timestamp(value: Option<&str>, offset: UtcOffset) -> Result<Option<String>, ConvertError> {
    value.map(|value| required_timestamp(Some(value), offset)).transpose()
}

/// Parses `yyyy-MM-dd HH:mm:ss` with an optional fraction of 1 to 9 digits, without a zone.
fn parse_timestamp(value: &str, offset: UtcOffset) -> Option<OffsetDateTime> {
    let (date_text, time_text) = value.split_once(' ')?;
    let date = Date::parse(date_text, format_description!("[year]-[month]-[day]")).ok()?;

    let (clock, fraction) = match time_text.split_once('.') {
        Some((clock, fraction)) => (clock, Some(fraction)),
        None => (time_text, None),
    };

    let mut parts = clock.split(':');
    let hour = parts.next()?.parse::<u8>().ok()?;
    let minute = parts.next()?.parse::<u8>().ok()?;
    let second = parts.next()?.parse::<u8>().ok()?;
    if parts.next().is_some() {
        return None;
    }

    let nanosecond = match fraction {
        Some(fraction) => {
            if fraction.is_empty() || fraction.len() > 9 || !fraction.bytes().all(|byte| byte.is_ascii_digit()) {
                return None;
            }
            let padded = format!("{fraction:0<9}");
            padded.parse::<u32>().ok()?
        }
        None => 0,
    };

    let time = Time::from_hms_nano(hour, minute, second, nanosecond).ok()?;
    Some(time::PrimitiveDateTime::new(date, time).assume_offset(offset))
}

pub(crate) fn required_bool(value: Option<i64>) -> Result<bool, ConvertError> {
    match value {
        Some(0) => Ok(false),
        Some(1) => Ok(true),
        _ => Err(ConvertError::Invalid),
    }
}

pub(crate) fn optional_bool(value: Option<i64>) -> Result<Option<bool>, ConvertError> {
    match value {
        None => Ok(None),
        Some(0) => Ok(Some(false)),
        Some(1) => Ok(Some(true)),
        _ => Err(ConvertError::Invalid),
    }
}

pub(crate) fn currency(value: Option<&str>) -> Result<Currency, ConvertError> {
    let value = value.ok_or(ConvertError::Invalid)?;
    Currency::new(value.trim()).map_err(|_| ConvertError::Invalid)
}

pub(crate) fn account_type(value: Option<&str>) -> Result<AccountType, ConvertError> {
    value.and_then(AccountType::from_stored).ok_or(ConvertError::Invalid)
}

pub(crate) fn rule_field(value: Option<&str>) -> Result<RuleField, ConvertError> {
    value.and_then(RuleField::from_stored).ok_or(ConvertError::Invalid)
}

pub(crate) fn rule_direction(value: Option<&str>) -> Result<RuleDirection, ConvertError> {
    value.and_then(RuleDirection::from_stored).ok_or(ConvertError::Invalid)
}

pub(crate) fn confidence(value: Option<&SourceNumber>) -> Result<Option<f64>, ConvertError> {
    value
        .map(|value| {
            decimal(value)
                .and_then(|decimal| decimal.to_string().parse::<f64>().ok())
                .ok_or(ConvertError::Invalid)
        })
        .transpose()
}

/// Maps FinDesk's `ui.dark_theme` text to Finglass's theme code.
pub(crate) fn theme(value: &str) -> Option<&'static str> {
    if value.eq_ignore_ascii_case("true") {
        Some(crate::app::prefs::ThemePref::Dark.code())
    } else if value.eq_ignore_ascii_case("false") {
        Some(crate::app::prefs::ThemePref::Light.code())
    } else {
        None
    }
}
