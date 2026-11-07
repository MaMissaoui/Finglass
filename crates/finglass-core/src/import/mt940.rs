//! Reads SWIFT MT940 account statement files (.sta, .mt940, .txt).

use rust_decimal::Decimal;
use time::{Date, OffsetDateTime};

use super::model::{ImportError, ImportedBooking, ParsedStatement, StatementFormat};
use super::util::{
    collapse_whitespace, date_from_ymd, decode_iso_8859_1, decode_text, is_valid_iban, normalize_iban, parse_decimal,
    text_lines,
};

/// Whether the bytes look like an MT940 statement.
pub fn looks_like(bytes: &[u8]) -> bool {
    let head = decode_iso_8859_1(&bytes[..bytes.len().min(4096)]);
    head.contains(":20:")
        && head.contains(":25:")
        && (head.contains(":61:") || head.contains(":60F:") || head.contains(":62F:"))
}

/// Reads an MT940 file, using today's date for two-digit years.
pub fn parse(bytes: &[u8]) -> Result<ParsedStatement, ImportError> {
    parse_with_today(bytes, OffsetDateTime::now_utc().date())
}

/// Reads an MT940 file with an explicit "today" for two-digit years.
pub fn parse_with_today(bytes: &[u8], today: Date) -> Result<ParsedStatement, ImportError> {
    if !looks_like(bytes) {
        return Err(ImportError::invalid("Not an MT940 file"));
    }
    let text = decode_text(bytes);
    let statements = split_statements(&text);
    let mut bookings = Vec::new();
    let mut skipped = Vec::new();
    let mut accounts = Vec::new();
    let mut account_iban = None;
    let mut record_no = 0usize;

    for statement in &statements {
        if let Some(account_id) = statement_account_id(statement)
            && !accounts.contains(&account_id)
        {
            accounts.push(account_id);
        }
        if account_iban.is_none() {
            account_iban = extract_iban(statement);
        }
        let currency = extract_currency(statement);
        let tags = parse_tags(statement);

        let mut i = 0usize;
        while i < tags.len() {
            if tags[i].name != "61" {
                i += 1;
                continue;
            }
            let entry_text = tags[i].content.clone();
            let info_text = if i + 1 < tags.len() && tags[i + 1].name == "86" {
                i += 2;
                Some(tags[i - 1].content.clone())
            } else {
                i += 1;
                None
            };
            record_no += 1;
            match parse_record(&entry_text, info_text.as_deref(), currency.as_deref(), today) {
                Ok(booking) => bookings.push(booking),
                Err(reason) => skipped.push(format!("Record {record_no}: {reason}")),
            }
        }
    }

    Ok(ParsedStatement {
        format: StatementFormat::Mt940,
        bookings,
        account_iban,
        accounts,
        skipped,
        pending_skipped: 0,
        csv: None,
        format_detail: None,
        closing_balance: None,
    })
}

fn split_statements(text: &str) -> Vec<String> {
    let mut blocks = Vec::new();
    let mut current = String::new();
    for line in text_lines(text) {
        let trimmed = line.trim();
        if !trimmed.is_empty() && trimmed.chars().all(|c| c == '-') {
            if !current.trim().is_empty() {
                blocks.push(std::mem::take(&mut current));
            }
        } else {
            current.push_str(line);
            current.push('\n');
        }
    }
    if !current.trim().is_empty() {
        blocks.push(current);
    }
    if blocks.is_empty() {
        vec![text.to_owned()]
    } else {
        blocks
    }
}

#[derive(Debug, Clone)]
struct Tag {
    name: String,
    content: String,
}

fn parse_tags(text: &str) -> Vec<Tag> {
    let mut tags = Vec::new();
    let mut current: Option<Tag> = None;
    for line in text_lines(text) {
        if let Some((name, content)) = parse_tag_line(line) {
            if let Some(tag) = current.take() {
                tags.push(tag);
            }
            current = Some(Tag { name, content });
        } else if let Some(tag) = current.as_mut() {
            tag.content.push('\n');
            tag.content.push_str(line);
        }
    }
    if let Some(tag) = current {
        tags.push(tag);
    }
    tags
}

fn parse_tag_line(line: &str) -> Option<(String, String)> {
    let bytes = line.as_bytes();
    if bytes.len() < 5 || bytes[0] != b':' {
        return None;
    }
    if !bytes[1].is_ascii_digit() || !bytes[2].is_ascii_digit() {
        return None;
    }
    let mut colon = 3usize;
    if bytes.len() > 3 && bytes[3].is_ascii_alphabetic() {
        colon = 4;
    }
    if bytes.len() <= colon || bytes[colon] != b':' {
        return None;
    }
    Some((line[1..colon].to_owned(), line[colon + 1..].to_owned()))
}

fn extract_iban(text: &str) -> Option<String> {
    let raw = parse_tags(text)
        .into_iter()
        .find(|tag| tag.name == "25")
        .map(|tag| tag.content.trim().to_owned())?;
    let candidate = raw.rsplit('/').next().unwrap_or(&raw).trim().to_owned();
    let normalized = normalize_iban(&candidate);
    if is_valid_iban(&normalized) {
        return Some(normalized);
    }
    if candidate.chars().count() > 3 {
        let chars: Vec<char> = candidate.chars().collect();
        let tail = &chars[chars.len() - 3..];
        if tail.iter().all(|c| c.is_alphabetic()) {
            let shortened: String = chars[..chars.len() - 3].iter().collect();
            let normalized = normalize_iban(&shortened);
            if is_valid_iban(&normalized) {
                return Some(normalized);
            }
        }
    }
    None
}

fn statement_account_id(text: &str) -> Option<String> {
    if let Some(iban) = extract_iban(text) {
        return Some(iban);
    }
    let raw = parse_tags(text)
        .into_iter()
        .find(|tag| tag.name == "25")
        .map(|tag| tag.content.trim().to_owned())?;
    if raw.is_empty() { None } else { Some(raw) }
}

fn extract_currency(text: &str) -> Option<String> {
    extract_balance_tag(text, "60F")
        .map(|balance| balance.currency)
        .or_else(|| extract_balance_tag(text, "60M").map(|balance| balance.currency))
        .or_else(|| extract_balance_tag(text, "62F").map(|balance| balance.currency))
}

struct Balance {
    currency: String,
}

fn extract_balance_tag(text: &str, tag_name: &str) -> Option<Balance> {
    let content = parse_tags(text)
        .into_iter()
        .find(|tag| tag.name == tag_name)
        .map(|tag| tag.content.trim().to_owned())?;
    let chars: Vec<char> = content.chars().collect();
    if chars.len() < 10 {
        return None;
    }
    let currency: String = chars[7..10].iter().collect();
    let amount_text: String = chars[10..].iter().collect();
    parse_mt_amount(amount_text.trim())?;
    Some(Balance { currency })
}

fn parse_record(
    text: &str,
    info_text: Option<&str>,
    currency: Option<&str>,
    today: Date,
) -> Result<ImportedBooking, String> {
    let line = text.split('\n').next().unwrap_or("").trim();
    let chars: Vec<char> = line.chars().collect();
    if chars.len() < 8 {
        return Err("too short".to_owned());
    }
    let value_date_text: String = chars[..6].iter().collect();
    let value_date =
        parse_mt_date(&value_date_text, today).ok_or_else(|| format!("invalid value date '{value_date_text}'"))?;

    let mut pos = 6usize;
    let mut entry_date = None;
    if pos + 4 <= chars.len() && chars[pos..pos + 4].iter().all(|c| c.is_ascii_digit()) {
        let text: String = chars[pos..pos + 4].iter().collect();
        entry_date = infer_booking_date(value_date, &text);
        pos += 4;
    }
    let booking_date = entry_date.unwrap_or(value_date);

    let indicator = if starts_with_at(&chars, pos, "RC") {
        pos += 2;
        "RC"
    } else if starts_with_at(&chars, pos, "RD") {
        pos += 2;
        "RD"
    } else if starts_with_at(&chars, pos, "C") {
        pos += 1;
        "C"
    } else if starts_with_at(&chars, pos, "D") {
        pos += 1;
        "D"
    } else {
        return Err("missing debit/credit indicator".to_owned());
    };

    if pos < chars.len() && chars[pos].is_alphabetic() {
        pos += 1;
    }

    let amount_start = pos;
    while pos < chars.len() && (chars[pos].is_ascii_digit() || chars[pos] == ',' || chars[pos] == '.') {
        pos += 1;
    }
    if pos == amount_start {
        return Err("missing amount".to_owned());
    }
    let raw_amount_text: String = chars[amount_start..pos].iter().collect();
    let raw_amount = parse_mt_amount(&raw_amount_text).ok_or_else(|| format!("invalid amount '{raw_amount_text}'"))?;
    let amount = match indicator {
        "D" | "RD" => -raw_amount,
        _ => raw_amount,
    };

    let detail_start = if pos < chars.len() && "NSF".contains(chars[pos]) {
        (pos + 4).min(chars.len())
    } else {
        pos
    };
    let detail_tail: String = chars[detail_start..].iter().collect();
    let detail_tail = detail_tail.trim();
    let bank_ref = detail_tail
        .split_once("//")
        .map(|(_, after)| after.trim().to_owned())
        .filter(|value| !value.is_empty());
    let customer_ref = detail_tail
        .split_once("//")
        .map(|(before, _)| before.trim().to_owned())
        .unwrap_or_else(|| detail_tail.trim().to_owned());
    let customer_ref = if customer_ref.is_empty() || customer_ref == "NONREF" {
        None
    } else {
        Some(customer_ref)
    };
    let reference = bank_ref.or(customer_ref);

    let info = info_text.map(parse_86);
    let purpose = info.as_ref().and_then(|info| info.purpose.clone()).or_else(|| {
        info_text
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
    });
    let partner_iban = info
        .as_ref()
        .and_then(|info| info.partner_iban.clone())
        .map(|iban| normalize_iban(&iban))
        .filter(|iban| is_valid_iban(iban));

    Ok(ImportedBooking {
        date: booking_date,
        value_date: Some(value_date),
        amount,
        currency: currency.map(str::to_owned),
        partner_name: info.as_ref().and_then(|info| info.partner_name.clone()),
        partner_iban,
        purpose,
        balance_after: None,
        bank_reference: reference,
    })
}

fn starts_with_at(chars: &[char], pos: usize, prefix: &str) -> bool {
    for (i, expected) in (pos..).zip(prefix.chars()) {
        if i >= chars.len() || chars[i] != expected {
            return false;
        }
    }
    true
}

fn parse_mt_date(text: &str, today: Date) -> Option<Date> {
    let chars: Vec<char> = text.chars().collect();
    if chars.len() != 6 || !chars.iter().all(|c| c.is_ascii_digit()) {
        return None;
    }
    let yy: String = chars[..2].iter().collect();
    let year = super::two_digit_year::resolve(yy.parse().ok()?, today).ok()?;
    let month: String = chars[2..4].iter().collect();
    let day: String = chars[4..6].iter().collect();
    date_from_ymd(year, month.parse().ok()?, day.parse().ok()?)
}

fn infer_booking_date(value_date: Date, entry_date: &str) -> Option<Date> {
    let chars: Vec<char> = entry_date.chars().collect();
    if chars.len() != 4 {
        return None;
    }
    let month: u32 = chars[..2].iter().collect::<String>().parse().ok()?;
    let day: u32 = chars[2..].iter().collect::<String>().parse().ok()?;
    let mut year = value_date.year();
    let value_month = value_date.month() as u8;
    if value_month == 12 && month == 1 {
        year += 1;
    }
    if value_month == 1 && month == 12 {
        year -= 1;
    }
    date_from_ymd(year, month, day)
}

struct StructuredInfo {
    partner_name: Option<String>,
    partner_iban: Option<String>,
    purpose: Option<String>,
}

fn parse_86(text: &str) -> StructuredInfo {
    let trimmed = text.trim();
    let compact_for_check = trimmed.replace('\n', "");
    let is_structured = trimmed.starts_with('?')
        || (compact_for_check.len() >= 4
            && compact_for_check.chars().take(3).all(|c| c.is_ascii_digit())
            && compact_for_check.chars().nth(3) == Some('?'));
    if !is_structured {
        let purpose = collapse_whitespace(trimmed);
        return StructuredInfo {
            partner_name: None,
            partner_iban: None,
            purpose: if purpose.is_empty() { None } else { Some(purpose) },
        };
    }

    let compact: String = trimmed.replace('\n', "");
    let body = if compact.chars().take(3).all(|c| c.is_ascii_digit()) && compact.chars().count() >= 3 {
        compact.chars().skip(3).collect::<String>()
    } else {
        compact
    };

    let chars: Vec<char> = body.chars().collect();
    let mut fields: Vec<(String, usize, usize)> = Vec::new();
    let mut i = 0usize;
    while i + 2 < chars.len() {
        if chars[i] == '?' && chars[i + 1].is_ascii_digit() && chars[i + 2].is_ascii_digit() {
            let key: String = chars[i + 1..i + 3].iter().collect();
            fields.push((key, i, i + 3));
            i += 3;
        } else {
            i += 1;
        }
    }

    let mut values: Vec<(String, String)> = Vec::new();
    for (index, (key, _, value_start)) in fields.iter().enumerate() {
        let end = if index + 1 < fields.len() {
            fields[index + 1].1
        } else {
            chars.len()
        };
        let value: String = chars[*value_start..end].iter().collect();
        values.push((key.clone(), value));
    }

    let purpose_by_numbers: String = values
        .iter()
        .filter(|(key, _)| {
            key.parse::<u32>()
                .is_ok_and(|n| (20..=29).contains(&n) || (60..=63).contains(&n))
        })
        .map(|(_, value)| value.trim())
        .collect();
    let purpose = if !purpose_by_numbers.is_empty() {
        Some(purpose_by_numbers)
    } else {
        values
            .iter()
            .find(|(key, _)| key == "00")
            .map(|(_, value)| value.trim().to_owned())
            .or_else(|| {
                values
                    .iter()
                    .find(|(key, _)| key == "10")
                    .map(|(_, value)| value.trim().to_owned())
            })
    };
    let purpose = purpose.filter(|value| !value.is_empty());

    let partner_name: String = values
        .iter()
        .filter(|(key, _)| key == "32" || key == "33")
        .map(|(_, value)| value.trim())
        .collect();
    let partner_name = if partner_name.is_empty() {
        None
    } else {
        Some(partner_name)
    };
    let partner_iban = values
        .iter()
        .find(|(key, _)| key == "31")
        .map(|(_, value)| value.trim().to_owned())
        .filter(|value| !value.is_empty());

    StructuredInfo {
        partner_name,
        partner_iban,
        purpose,
    }
}

fn parse_mt_amount(text: &str) -> Option<Decimal> {
    let normalized = text.replace('.', "").replace(',', ".");
    parse_decimal(&normalized)
}
