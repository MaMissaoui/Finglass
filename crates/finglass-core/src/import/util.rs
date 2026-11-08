//! Small helpers shared by the statement parsers: decoding, IBAN checks, amounts and dates.

use rust_decimal::{Decimal, RoundingStrategy};
use std::str::FromStr;
use time::{Date, Month};

use super::model::ImportError;

// -- text decoding --------------------------------------------------------------------------------

/// UTF-8 if the bytes are valid UTF-8, otherwise Windows-1252 (the encoding most German bank
/// exports use). UTF-16 is detected first, before the strict UTF-8 attempt.
pub(crate) fn decode_statement(bytes: &[u8]) -> (String, &'static encoding_rs::Encoding) {
    if let Some(charset) = utf16_charset(bytes) {
        let (text, _, _) = charset.decode(bytes);
        return (strip_bom(text.into_owned()), charset);
    }
    match std::str::from_utf8(bytes) {
        Ok(text) => (strip_bom(text.to_owned()), encoding_rs::UTF_8),
        Err(_) => {
            let (text, _, _) = encoding_rs::WINDOWS_1252.decode(bytes);
            (strip_bom(text.into_owned()), encoding_rs::WINDOWS_1252)
        }
    }
}

/// Shared bank-text decoding: UTF-8 if valid, otherwise Windows-1252.
pub(crate) fn decode_text(bytes: &[u8]) -> String {
    match std::str::from_utf8(bytes) {
        Ok(text) => strip_bom(text.to_owned()),
        Err(_) => {
            let (text, _, _) = encoding_rs::WINDOWS_1252.decode(bytes);
            strip_bom(text.into_owned())
        }
    }
}

/// Decodes an XML document, honouring UTF-16 BOMs and the encoding declaration, and otherwise
/// falling back to Windows-1252.
pub(crate) fn decode_xml(bytes: &[u8]) -> String {
    if let Some(charset) = utf16_charset(bytes) {
        let (text, _, _) = charset.decode(bytes);
        return strip_bom(text.into_owned());
    }
    let head = String::from_utf8_lossy(&bytes[..bytes.len().min(512)]);
    if let Some(label) = declared_encoding(&head)
        && let Some(charset) = encoding_rs::Encoding::for_label(label.as_bytes())
    {
        let (text, _, _) = charset.decode(bytes);
        return strip_bom(text.into_owned());
    }
    decode_text(bytes)
}

fn declared_encoding(head: &str) -> Option<&str> {
    let declaration = head.trim_start();
    if !declaration.starts_with("<?xml") {
        return None;
    }
    let declaration = declaration.split("?>").next().unwrap_or(declaration);
    let lower = declaration.to_ascii_lowercase();
    let start = lower.find("encoding")?;
    let head = declaration;
    let rest = &head[start + "encoding".len()..];
    let rest = rest.trim_start();
    let rest = rest.strip_prefix('=')?.trim_start();
    let quote = rest.chars().next()?;
    if quote != '"' && quote != '\'' {
        return None;
    }
    let value = &rest[1..];
    let end = value.find(quote)?;
    Some(&value[..end])
}

pub(crate) fn decode_iso_8859_1(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| *byte as char).collect()
}

fn strip_bom(text: String) -> String {
    text.strip_prefix('\u{feff}').map(str::to_owned).unwrap_or(text)
}

fn utf16_charset(bytes: &[u8]) -> Option<&'static encoding_rs::Encoding> {
    if bytes.len() >= 2 {
        if bytes[0] == 0xFF && bytes[1] == 0xFE {
            return Some(encoding_rs::UTF_16LE);
        }
        if bytes[0] == 0xFE && bytes[1] == 0xFF {
            return Some(encoding_rs::UTF_16BE);
        }
    }
    let n = bytes.len().min(512);
    if n < 2 {
        return None;
    }
    let mut even_zeros = 0usize;
    let mut odd_zeros = 0usize;
    for (i, byte) in bytes[..n].iter().enumerate() {
        if *byte == 0 {
            if i % 2 == 0 {
                even_zeros += 1;
            } else {
                odd_zeros += 1;
            }
        }
    }
    let even_total = n - n / 2;
    let odd_total = n / 2;
    let low_half_zero = odd_zeros * 100 >= 30 * odd_total && even_zeros * 100 < 5 * even_total;
    let mirror_zero = even_zeros * 100 >= 30 * even_total && odd_zeros * 100 < 5 * odd_total;
    if low_half_zero {
        Some(encoding_rs::UTF_16LE)
    } else if mirror_zero {
        Some(encoding_rs::UTF_16BE)
    } else {
        None
    }
}

// -- IBAN -----------------------------------------------------------------------------------------

/// Strips spaces and upper-cases.
pub(crate) fn normalize_iban(iban: &str) -> String {
    iban.replace(' ', "").to_uppercase()
}

/// ISO 13616 mod-97 check.
pub(crate) fn is_valid_iban(iban: &str) -> bool {
    let s = normalize_iban(iban);
    let chars: Vec<char> = s.chars().collect();
    if !(15..=34).contains(&chars.len()) || !chars.iter().all(|c| c.is_alphanumeric()) {
        return false;
    }
    if !chars[0].is_alphabetic()
        || !chars[1].is_alphabetic()
        || !chars[2].is_ascii_digit()
        || !chars[3].is_ascii_digit()
    {
        return false;
    }
    if s.starts_with("DE") && chars.len() != 22 {
        return false;
    }
    let rearranged: String = chars[4..].iter().chain(chars[..4].iter()).collect();
    let mut remainder: u32 = 0;
    for ch in rearranged.chars() {
        if let Some(digit) = ch.to_digit(10) {
            remainder = (remainder * 10 + digit) % 97;
        } else if ch.is_ascii_uppercase() {
            let value = ch as u32 - 'A' as u32 + 10;
            remainder = (remainder * 100 + value) % 97;
        } else {
            return false;
        }
    }
    remainder == 1
}

/// Finds IBAN-shaped runs in a preamble cell, like Kotlin's `IBAN_PATTERN`.
pub(crate) fn find_ibans(text: &str) -> Vec<String> {
    let upper = text.to_uppercase();
    let chars: Vec<char> = upper.chars().collect();
    let mut result = Vec::new();
    let mut start = 0usize;
    while start + 4 <= chars.len() {
        if !(chars[start].is_ascii_uppercase()
            && chars[start + 1].is_ascii_uppercase()
            && chars[start + 2].is_ascii_digit()
            && chars[start + 3].is_ascii_digit())
        {
            start += 1;
            continue;
        }
        let mut position = start + 4;
        let mut items = 0usize;
        while position < chars.len() && items < 30 {
            let mut next = position;
            if chars[next] == ' ' {
                next += 1;
            }
            if next < chars.len() && chars[next].is_ascii_alphanumeric() {
                position = next + 1;
                items += 1;
            } else {
                break;
            }
        }
        if items >= 11 {
            let candidate: String = chars[start..position].iter().filter(|c| **c != ' ').collect();
            result.push(candidate);
        }
        // Continue right after the match, as a non-overlapping find-all would.
        start = position.max(start + 1);
    }
    result
}

// -- amounts --------------------------------------------------------------------------------------

/// Parses a decimal out of untrusted file text, with FinDesk's import bounds: at most 15 integer
/// digits, at most 10 decimal places and at most 64 input characters.
pub(crate) fn parse_decimal(text: &str) -> Option<Decimal> {
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

/// Parses user/file input like "1.234,56", "1,234.56", "-12.5" or "12" and rounds to two places.
pub(crate) fn parse_amount(input: &str) -> Option<Decimal> {
    let mut s = input.trim().replace(['€', ' ', '\u{A0}'], "");
    if s.is_empty() {
        return None;
    }
    let last_comma = s.rfind(',');
    let last_dot = s.rfind('.');
    if matches!((last_comma, last_dot), (Some(comma), Some(dot)) if comma > dot)
        || (last_comma.is_some() && last_dot.is_none())
    {
        s = s.replace('.', "").replace(',', ".");
    } else {
        s = s.replace(',', "");
    }
    let value = parse_decimal(&s)?.round_dp_with_strategy(2, RoundingStrategy::MidpointAwayFromZero);
    let mut value = value;
    value.rescale(2);
    Some(value)
}

/// Java `BigDecimal` syntax check, without any of FinDesk's size limits.
pub(crate) fn is_decimal_syntax(text: &str) -> bool {
    let s = text.trim();
    if s.is_empty() {
        return false;
    }
    let bytes = s.as_bytes();
    let mut i = 0usize;
    if bytes[i] == b'+' || bytes[i] == b'-' {
        i += 1;
        if i == bytes.len() {
            return false;
        }
    }
    let mut digits = 0usize;
    while i < bytes.len() && bytes[i].is_ascii_digit() {
        i += 1;
        digits += 1;
    }
    if i < bytes.len() && bytes[i] == b'.' {
        i += 1;
        while i < bytes.len() && bytes[i].is_ascii_digit() {
            i += 1;
            digits += 1;
        }
    }
    if digits == 0 {
        return false;
    }
    if i < bytes.len() && (bytes[i] == b'e' || bytes[i] == b'E') {
        i += 1;
        if i < bytes.len() && (bytes[i] == b'+' || bytes[i] == b'-') {
            i += 1;
        }
        let mut exponent_digits = 0usize;
        while i < bytes.len() && bytes[i].is_ascii_digit() {
            i += 1;
            exponent_digits += 1;
        }
        if exponent_digits == 0 {
            return false;
        }
    }
    i == bytes.len()
}

// -- dates ----------------------------------------------------------------------------------------

pub(crate) fn date_from_ymd(year: i32, month: u32, day: u32) -> Option<Date> {
    let month = Month::try_from(u8::try_from(month).ok()?).ok()?;
    Date::from_calendar_date(year, month, u8::try_from(day).ok()?).ok()
}

/// Parses the date formats FinDesk's CSV importer accepts, strictly.
pub(crate) fn parse_csv_date(value: Option<&str>, today: Date) -> Option<Date> {
    let s = value?;
    if s.trim().is_empty() {
        return None;
    }
    let token = s.split_whitespace().next()?;
    if token.is_empty() {
        return None;
    }
    if let Some(date) = parse_with_formats(token) {
        return Some(date);
    }
    if let Some(date) = parse_two_digit_date(token, today) {
        return Some(date);
    }
    if token.chars().count() > 10 {
        let prefix: String = token.chars().take(10).collect();
        return parse_with_formats(&prefix);
    }
    None
}

fn parse_with_formats(candidate: &str) -> Option<Date> {
    parse_dd_mm_uuuu(candidate)
        .or_else(|| parse_d_m_uuuu(candidate))
        .or_else(|| parse_uuuu_mm_dd(candidate))
        .or_else(|| parse_slash(candidate, false))
        .or_else(|| parse_slash(candidate, true))
}

fn parse_two_digit_date(token: &str, today: Date) -> Option<Date> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 3 {
        return None;
    }
    let day = two_digits(parts[0])?;
    let month = two_digits(parts[1])?;
    let yy = two_digits(parts[2])?;
    let year = super::two_digit_year::resolve(u8::try_from(yy).ok()?, today).ok()?;
    date_from_ymd(year, month, day)
}

fn two_digits(part: &str) -> Option<u32> {
    if part.len() == 2 && part.bytes().all(|b| b.is_ascii_digit()) {
        part.parse().ok()
    } else {
        None
    }
}

fn two_or_one_digits(part: &str) -> Option<u32> {
    if (1..=2).contains(&part.len()) && part.bytes().all(|b| b.is_ascii_digit()) {
        part.parse().ok()
    } else {
        None
    }
}

fn four_digits(part: &str) -> Option<i32> {
    if part.len() == 4 && part.bytes().all(|b| b.is_ascii_digit()) {
        part.parse().ok()
    } else {
        None
    }
}

fn parse_dd_mm_uuuu(candidate: &str) -> Option<Date> {
    let parts: Vec<&str> = candidate.split('.').collect();
    if parts.len() != 3 {
        return None;
    }
    date_from_ymd(four_digits(parts[2])?, two_digits(parts[1])?, two_digits(parts[0])?)
}

fn parse_d_m_uuuu(candidate: &str) -> Option<Date> {
    let parts: Vec<&str> = candidate.split('.').collect();
    if parts.len() != 3 {
        return None;
    }
    date_from_ymd(
        four_digits(parts[2])?,
        two_or_one_digits(parts[1])?,
        two_or_one_digits(parts[0])?,
    )
}

fn parse_uuuu_mm_dd(candidate: &str) -> Option<Date> {
    let parts: Vec<&str> = candidate.split('-').collect();
    if parts.len() != 3 {
        return None;
    }
    date_from_ymd(four_digits(parts[0])?, two_digits(parts[1])?, two_digits(parts[2])?)
}

fn parse_slash(candidate: &str, month_first: bool) -> Option<Date> {
    let parts: Vec<&str> = candidate.split('/').collect();
    if parts.len() != 3 {
        return None;
    }
    let first = two_digits(parts[0])?;
    let second = two_digits(parts[1])?;
    let year = four_digits(parts[2])?;
    let (month, day) = if month_first { (first, second) } else { (second, first) };
    date_from_ymd(year, month, day)
}

/// Parses an ISO `yyyy-mm-dd` date, as used by CAMT and XML OFX.
pub(crate) fn parse_iso_date(text: &str) -> Option<Date> {
    let prefix: String = text.trim().chars().take(10).collect();
    let parts: Vec<&str> = prefix.split('-').collect();
    if parts.len() != 3 {
        return None;
    }
    date_from_ymd(four_digits(parts[0])?, two_digits(parts[1])?, two_digits(parts[2])?)
}

pub(crate) fn format_iso_date(date: Date) -> String {
    format!("{:04}-{:02}-{:02}", date.year(), date.month() as u8, date.day())
}

pub(crate) fn require_no_doctype(text: &str) -> Result<(), ImportError> {
    if text.contains("<!DOCTYPE") {
        return Err(ImportError::invalid("DOCTYPE is not allowed"));
    }
    Ok(())
}

pub(crate) fn collapse_whitespace(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Kotlin `String.lines()`: split on CRLF, LF or CR, without a trailing empty line.
pub(crate) fn text_lines(text: &str) -> Vec<&str> {
    let mut lines = Vec::new();
    let bytes = text.as_bytes();
    let mut start = 0usize;
    let mut i = 0usize;
    while i < bytes.len() {
        if bytes[i] == b'\r' || bytes[i] == b'\n' {
            lines.push(&text[start..i]);
            if bytes[i] == b'\r' && i + 1 < bytes.len() && bytes[i + 1] == b'\n' {
                i += 1;
            }
            start = i + 1;
        }
        i += 1;
    }
    if start < text.len() {
        lines.push(&text[start..]);
    }
    lines
}
