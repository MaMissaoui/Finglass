//! Reads Quicken Interchange Format (.qif) files.

use std::collections::HashMap;

use time::{Date, OffsetDateTime};

use super::model::{ImportError, ImportedBooking, ParsedStatement, StatementFormat};
use super::util::{collapse_whitespace, date_from_ymd, decode_text, parse_amount, text_lines};

const BANK_TYPES: &[&str] = &["bank", "cash", "ccard", "oth a", "oth l"];
const SKIP_TYPES: &[&str] = &["cat", "class", "memorized", "invst"];

/// Whether the bytes look like a QIF file.
pub fn looks_like(bytes: &[u8]) -> bool {
    let text = decode_text(bytes);
    let head: String = text.chars().take(1024).collect();
    let normalised = head.to_lowercase();
    normalised.contains("!type:")
        && (normalised.contains("!type:bank")
            || normalised.contains("!type:cash")
            || normalised.contains("!type:ccard")
            || normalised.contains("!type:oth a")
            || normalised.contains("!type:oth l"))
}

/// Reads a QIF file, using today's date for two-digit years.
pub fn parse(bytes: &[u8]) -> Result<ParsedStatement, ImportError> {
    parse_with_today(bytes, OffsetDateTime::now_utc().date())
}

/// Reads a QIF file with an explicit "today" for two-digit years.
pub fn parse_with_today(bytes: &[u8], today: Date) -> Result<ParsedStatement, ImportError> {
    if !looks_like(bytes) {
        return Err(ImportError::invalid("Not a QIF file"));
    }
    let text = decode_text(bytes);
    let lines = text_lines(&text);
    let slash_order = detect_slash_order(&lines);

    let mut bookings = Vec::new();
    let mut skipped = Vec::new();
    let mut current: HashMap<char, String> = HashMap::new();
    let mut active_type: Option<String> = None;
    let mut record_no = 0usize;

    for line in &lines {
        let trimmed = line.trim();
        let type_prefix: String = trimmed.chars().take(6).collect();
        if type_prefix.eq_ignore_ascii_case("!Type:") {
            flush(
                &mut current,
                &mut record_no,
                active_type.as_deref(),
                slash_order,
                today,
                &mut bookings,
                &mut skipped,
            );
            active_type = Some(trimmed.chars().skip(6).collect::<String>().trim().to_lowercase());
            continue;
        }
        if starts_with_ignore_case(trimmed, "!Account")
            || starts_with_ignore_case(trimmed, "!Option:")
            || trimmed.starts_with('!')
        {
            flush(
                &mut current,
                &mut record_no,
                active_type.as_deref(),
                slash_order,
                today,
                &mut bookings,
                &mut skipped,
            );
            active_type = None;
            continue;
        }
        if active_type.as_deref().is_some_and(|kind| SKIP_TYPES.contains(&kind)) {
            continue;
        }
        if active_type.is_none() {
            continue;
        }
        if trimmed == "^" {
            flush(
                &mut current,
                &mut record_no,
                active_type.as_deref(),
                slash_order,
                today,
                &mut bookings,
                &mut skipped,
            );
            continue;
        }
        if trimmed.is_empty() {
            continue;
        }
        let mut chars = trimmed.chars();
        let Some(code) = chars.next() else {
            continue;
        };
        let value = chars.as_str().trim();
        let entry = current.entry(code).or_default();
        if !entry.is_empty() {
            entry.push('\n');
        }
        entry.push_str(value);
    }
    flush(
        &mut current,
        &mut record_no,
        active_type.as_deref(),
        slash_order,
        today,
        &mut bookings,
        &mut skipped,
    );

    Ok(ParsedStatement {
        format: StatementFormat::Qif,
        bookings,
        account_iban: None,
        accounts: Vec::new(),
        skipped,
        pending_skipped: 0,
        csv: None,
        format_detail: None,
        closing_balance: None,
    })
}

#[allow(clippy::too_many_arguments)]
fn flush(
    current: &mut HashMap<char, String>,
    record_no: &mut usize,
    active_type: Option<&str>,
    slash_order: SlashOrder,
    today: Date,
    bookings: &mut Vec<ImportedBooking>,
    skipped: &mut Vec<String>,
) {
    if current.is_empty() {
        return;
    }
    *record_no += 1;
    if active_type.is_some_and(|kind| BANK_TYPES.contains(&kind)) {
        match to_booking(current, slash_order, today) {
            Ok(booking) => bookings.push(booking),
            Err(reason) => skipped.push(format!("Record {}: {reason}", *record_no)),
        }
    }
    current.clear();
}

fn to_booking(fields: &HashMap<char, String>, slash_order: SlashOrder, today: Date) -> Result<ImportedBooking, String> {
    let date_text = fields.get(&'D').ok_or_else(|| "missing date".to_owned())?;
    let date = parse_qif_date(date_text, slash_order, today).ok_or_else(|| format!("invalid date '{date_text}'"))?;
    let amount_text = fields
        .get(&'T')
        .or_else(|| fields.get(&'U'))
        .ok_or_else(|| "missing amount".to_owned())?;
    let amount = parse_amount(amount_text).ok_or_else(|| format!("invalid amount '{amount_text}'"))?;
    Ok(ImportedBooking {
        date,
        value_date: None,
        amount,
        currency: None,
        partner_name: fields
            .get(&'P')
            .map(|value| value.trim().to_owned())
            .filter(|value| !value.is_empty()),
        purpose: fields
            .get(&'M')
            .map(|value| collapse_whitespace(value))
            .filter(|value| !value.is_empty()),
        bank_reference: None,
        partner_iban: None,
        balance_after: None,
    })
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SlashOrder {
    DayMonth,
    Unknown,
}

fn detect_slash_order(lines: &[&str]) -> SlashOrder {
    for line in lines {
        let text = line.trim_start();
        let mut chars = text.chars();
        if !chars.next().is_some_and(|c| c == 'D' || c == 'd') {
            continue;
        }
        let value = chars.as_str().trim().replace(' ', "");
        if !value.contains('/') && !value.contains('\'') {
            continue;
        }
        let separator = value.find(['/', '\'']);
        let Some(index) = separator else { continue };
        if index == 0 {
            continue;
        }
        let Ok(first) = value[..index].parse::<u32>() else {
            continue;
        };
        if first > 12 {
            return SlashOrder::DayMonth;
        }
    }
    SlashOrder::Unknown
}

fn parse_qif_date(text: &str, slash_order: SlashOrder, today: Date) -> Option<Date> {
    let t = text.replace(' ', "");
    if t.contains('.') {
        let parts: Vec<&str> = t.split('.').collect();
        if parts.len() != 3 {
            return None;
        }
        let day = parts[0].parse().ok()?;
        let month = parts[1].parse().ok()?;
        let year = parse_year(parts[2], today)?;
        return date_from_ymd(year, month, day);
    }
    let normalised = t.replace(['\'', '-'], "/");
    if !normalised.contains('/') {
        return None;
    }
    let parts: Vec<&str> = normalised.split('/').collect();
    if parts.len() != 3 {
        return None;
    }
    let first: u32 = parts[0].parse().ok()?;
    let second: u32 = parts[1].parse().ok()?;
    let year = parse_year(parts[2], today)?;
    let (month, day) = if slash_order == SlashOrder::DayMonth {
        (second, first)
    } else {
        (first, second)
    };
    date_from_ymd(year, month, day)
}

fn parse_year(text: &str, today: Date) -> Option<i32> {
    let digits: String = text.chars().filter(char::is_ascii_digit).collect();
    match digits.len() {
        2 => super::two_digit_year::resolve(digits.parse().ok()?, today).ok(),
        4 => digits.parse().ok(),
        _ => None,
    }
}

fn starts_with_ignore_case(text: &str, prefix: &str) -> bool {
    let mut chars = text.chars();
    prefix.chars().all(|expected| {
        chars
            .next()
            .is_some_and(|actual| actual.eq_ignore_ascii_case(&expected))
    })
}
