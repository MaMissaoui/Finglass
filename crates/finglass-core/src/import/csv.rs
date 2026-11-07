//! Reads bank CSV exports (Sparkasse, ING, DKB, Commerzbank, comdirect, N26, … and generic ones).

use rust_decimal::Decimal;
use time::{Date, OffsetDateTime};

use super::model::{CsvLayout, CsvMapping, ImportError, ImportedBooking, ParsedStatement, StatementFormat};
use super::util::{
    collapse_whitespace, decode_statement, find_ibans, is_valid_iban, normalize_iban, parse_amount, parse_csv_date,
};

const DATE: &[&str] = &[
    "buchungstag",
    "buchungsdatum",
    "buchung",
    // Card statements (HVB "Eingangstag", Bank Norwegian "BookDate"): the posting date wins.
    "eingangstag",
    "bookdate",
    "datum",
    "bookingdate",
    "date",
    "transactiondate",
];
const VALUE_DATE: &[&str] = &[
    "valutadatum",
    "valuta",
    "wertstellung",
    "wertstellungsdatum",
    "valuedate",
    "belegdatum",
];
const AMOUNT: &[&str] = &[
    "betrag",
    "betrageur",
    "betrageuro",
    "umsatz",
    "umsatzineur",
    "amount",
    "amounteur",
    "betragineur",
];
const DEBIT: &[&str] = &["soll", "sollbetrag", "ausgang", "debit", "lastschrift", "belastung"];
const CREDIT: &[&str] = &["haben", "habenbetrag", "eingang", "credit", "gutschrift"];
const PARTNER: &[&str] = &[
    "beguenstigterzahlungspflichtiger",
    "namezahlungsbeteiligter",
    "auftraggeberbeguenstigter",
    "zahlungsempfaengerin",
    "zahlungsempfaenger",
    "empfaenger",
    "auftraggeber",
    "partnername",
    "payee",
    "counterparty",
    "name",
];
const PAYER: &[&str] = &["zahlungspflichtiger", "zahlungspflichtigerin", "zahlungspflichtige"];
const PENDING_MARKERS: &[&str] = &[
    "umsatz vorgemerkt",
    "vorgemerkt",
    "pending",
    "ausstehend",
    "nicht gebucht",
];
const PARTNER_IBAN: &[&str] = &[
    "ibanzahlungsbeteiligter",
    "kontonummeriban",
    "iban",
    "kontonummer",
    "partneriban",
    "counterpartyiban",
];
const PURPOSE: &[&str] = &[
    "verwendungszweck",
    "purpose",
    "reference",
    "beschreibung",
    "description",
    "buchungstext",
    "umsatzart",
    "vorgang",
    "textverwendungszweck",
    "text",
];
const CURRENCY: &[&str] = &["waehrung", "currency", "whrg"];
const BALANCE: &[&str] = &["saldo", "kontostand", "balance"];

// FinDesk's own export headers, matched exactly and only when no bank alias matched.
const OWN_PARTNER: &[&str] = &["partner", "partenaire"];
const OWN_AMOUNT: &[&str] = &["amount", "betrag", "montant"];
const OWN_PURPOSE: &[&str] = &["purpose", "verwendungszweck", "libell", "libelle"];
const OWN_CURRENCY: &[&str] = &["currency", "waehrung", "devise"];
const OWN_BALANCE: &[&str] = &["balance", "saldo", "solde"];

/// Reads a CSV file, using today's date for two-digit years.
pub fn parse(bytes: &[u8], mapping: Option<&CsvMapping>) -> Result<ParsedStatement, ImportError> {
    parse_with_today(bytes, mapping, OffsetDateTime::now_utc().date())
}

/// Reads a CSV file with an explicit "today" for two-digit years.
pub fn parse_with_today(
    bytes: &[u8],
    mapping: Option<&CsvMapping>,
    today: Date,
) -> Result<ParsedStatement, ImportError> {
    let (text, charset) = decode_statement(bytes);
    let delimiter = detect_delimiter(&text);
    let rows = split_records(&text, delimiter, usize::MAX);
    Ok(parse_rows(
        &rows,
        mapping,
        StatementFormat::Csv,
        charset.name(),
        delimiter,
        today,
    ))
}

/// Everything [`parse`] does after decoding and splitting; also used for XLSX sheets.
pub fn parse_rows(
    rows: &[Vec<String>],
    mapping: Option<&CsvMapping>,
    format: StatementFormat,
    charset_name: &str,
    delimiter: char,
    today: Date,
) -> ParsedStatement {
    let header_index = find_header(rows);
    let headers: Vec<String> = rows
        .get(header_index)
        .map(|row| row.iter().map(|cell| cell.trim().to_owned()).collect())
        .unwrap_or_default();
    let mapping = mapping.cloned().unwrap_or_else(|| detect_mapping(&headers));
    let layout = CsvLayout {
        delimiter,
        charset_name: charset_name.to_owned(),
        header_line: header_index + 1,
        headers,
        mapping: mapping.clone(),
    };
    let preamble_iban = preamble_iban(&rows[..header_index.min(rows.len())]);

    if !mapping.is_complete() {
        return ParsedStatement {
            format,
            bookings: Vec::new(),
            account_iban: preamble_iban,
            accounts: Vec::new(),
            skipped: vec!["Date and amount columns not recognised".to_owned()],
            pending_skipped: 0,
            csv: Some(layout),
            format_detail: None,
            closing_balance: None,
        };
    }

    let mut bookings = Vec::new();
    let mut skipped = Vec::new();
    let mut pending = 0u32;
    for (i, row) in rows.iter().skip(header_index + 1).enumerate() {
        if row.iter().all(|cell| cell.trim().is_empty()) {
            continue;
        }
        if row.iter().any(|cell| {
            PENDING_MARKERS
                .iter()
                .any(|marker| cell.trim().to_lowercase() == *marker)
        }) {
            pending += 1;
            continue;
        }
        let line_no = header_index + 2 + i;
        match to_booking(row, &mapping, today) {
            Ok(booking) => bookings.push(booking),
            Err(reason) => skipped.push(format!("Line {line_no}: {reason}")),
        }
    }

    ParsedStatement {
        format,
        bookings,
        account_iban: preamble_iban,
        accounts: Vec::new(),
        skipped,
        pending_skipped: pending,
        csv: Some(layout),
        format_detail: None,
        closing_balance: None,
    }
}

fn to_booking(row: &[String], mapping: &CsvMapping, today: Date) -> Result<ImportedBooking, String> {
    let cell = |index: Option<usize>| -> Option<String> {
        index
            .and_then(|i| row.get(i))
            .map(|value| value.trim())
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
    };

    let date = parse_csv_date(cell(mapping.date).as_deref(), today)
        .ok_or_else(|| format!("no valid date '{}'", cell(mapping.date).unwrap_or_default()))?;

    let amount = if mapping.amount.is_some() {
        cell(mapping.amount)
            .as_deref()
            .and_then(parse_money)
            .ok_or_else(|| format!("no valid amount '{}'", cell(mapping.amount).unwrap_or_default()))?
    } else {
        let debit = cell(mapping.debit)
            .as_deref()
            .and_then(parse_money)
            .map(|value| value.abs());
        let credit = cell(mapping.credit)
            .as_deref()
            .and_then(parse_money)
            .map(|value| value.abs());
        match (debit, credit) {
            (Some(debit), _) if debit != Decimal::ZERO => -debit,
            (Some(_), Some(credit)) => credit,
            (Some(debit), None) => debit,
            (None, Some(credit)) => credit,
            (None, None) => return Err("no debit or credit amount".to_owned()),
        }
    };

    let partner_iban = cell(mapping.partner_iban)
        .map(|value| normalize_iban(&value))
        .filter(|value| is_valid_iban(value));

    let partner_name = {
        let payer = if amount > Decimal::ZERO && mapping.payer.is_some() {
            cell(mapping.payer)
        } else {
            None
        };
        payer.or_else(|| cell(mapping.partner))
    }
    .map(|value| without_spreadsheet_guard(&value));

    let purpose = cell(mapping.purpose)
        .map(|value| without_spreadsheet_guard(&value))
        .map(|value| collapse_whitespace(&value));

    Ok(ImportedBooking {
        date,
        value_date: parse_csv_date(cell(mapping.value_date).as_deref(), today),
        amount,
        currency: cell(mapping.currency)
            .map(|value| value.to_uppercase())
            .filter(|value| value.chars().count() == 3),
        partner_name,
        partner_iban,
        purpose,
        balance_after: cell(mapping.balance).as_deref().and_then(parse_money),
        bank_reference: None,
    })
}

fn parse_money(s: &str) -> Option<Decimal> {
    // "-1.234,56 €", "1,234.56", "−12,00" (unicode minus), "12,00 S" (Soll suffix)
    let mut text = s.replace('−', "-").replace("EUR", "").trim().to_owned();
    let mut negative = false;
    if text.ends_with(" S") || text.ends_with('-') {
        negative = true;
        if let Some(stripped) = text.strip_suffix(" S") {
            text = stripped.to_owned();
        }
        if let Some(stripped) = text.strip_suffix('-') {
            text = stripped.to_owned();
        }
    } else if let Some(stripped) = text.strip_suffix(" H") {
        text = stripped.to_owned();
    }
    let value = parse_amount(&text)?;
    if negative { Some(-value.abs()) } else { Some(value) }
}

/// Undoes only FinDesk's own spreadsheet guard apostrophe.
fn without_spreadsheet_guard(value: &str) -> String {
    let mut chars = value.chars();
    let first = chars.next();
    let second = chars.next();
    if first == Some('\'') && second.is_some_and(|c| "=+-@\t\r".contains(c)) {
        value.chars().skip(1).collect()
    } else {
        value.to_owned()
    }
}

/// Parses a CSV date using today's date for two-digit years.
pub fn parse_date(value: Option<&str>) -> Option<Date> {
    parse_csv_date(value, OffsetDateTime::now_utc().date())
}

/// Parses a CSV date with an explicit "today" for two-digit years.
pub fn parse_date_with_today(value: Option<&str>, today: Date) -> Option<Date> {
    parse_csv_date(value, today)
}

fn detect_delimiter(text: &str) -> char {
    let candidates = [';', ',', '\t', '|'];
    let records: Vec<(char, Vec<Vec<String>>)> = candidates
        .iter()
        .map(|delimiter| (*delimiter, split_records(text, *delimiter, 30)))
        .collect();
    let scores: Vec<(char, usize)> = records
        .iter()
        .map(|(delimiter, rows)| (*delimiter, delimiter_score(rows)))
        .collect();
    let best = scores.iter().map(|(_, score)| *score).max().unwrap_or(0);
    if best == 0 {
        return ';';
    }
    let tied: Vec<char> = scores
        .iter()
        .filter(|(_, score)| *score == best)
        .map(|(delimiter, _)| *delimiter)
        .collect();
    tied.iter()
        .copied()
        .find(|delimiter| {
            let rows = records
                .iter()
                .find(|(candidate, _)| candidate == delimiter)
                .map(|(_, rows)| rows.as_slice())
                .unwrap_or(&[]);
            has_header(rows)
        })
        .unwrap_or(tied[0])
}

fn delimiter_score(records: &[Vec<String>]) -> usize {
    let sample: Vec<&Vec<String>> = records
        .iter()
        .filter(|row| !row.iter().all(|cell| cell.trim().is_empty()))
        .take(30)
        .collect();
    let mut counts: Vec<(usize, usize)> = Vec::new();
    for row in sample {
        if row.len() > 1 {
            if let Some(entry) = counts.iter_mut().find(|(size, _)| *size == row.len()) {
                entry.1 += 1;
            } else {
                counts.push((row.len(), 1));
            }
        }
    }
    counts.iter().map(|(_, count)| *count).max().unwrap_or(0)
}

fn has_header(records: &[Vec<String>]) -> bool {
    records.iter().take(30).any(|row| {
        let headers: Vec<String> = row.iter().map(|cell| cell.trim().to_owned()).collect();
        detect_mapping(&headers).is_complete()
    })
}

fn find_header(rows: &[Vec<String>]) -> usize {
    rows.iter()
        .take(30)
        .position(|row| {
            let headers: Vec<String> = row.iter().map(|cell| cell.trim().to_owned()).collect();
            detect_mapping(&headers).is_complete()
        })
        .unwrap_or(0)
}

/// Normalises a header: lowercase, umlauts folded, non-alphanumerics removed.
pub fn normalize_header(header: &str) -> String {
    header
        .to_lowercase()
        .replace('ä', "ae")
        .replace('ö', "oe")
        .replace('ü', "ue")
        .replace('ß', "ss")
        .chars()
        .filter(|c| c.is_ascii_lowercase() || c.is_ascii_digit())
        .collect()
}

fn find_column(norm: &[String], used: &mut [bool], aliases: &[&str], exact: bool) -> Option<usize> {
    for alias in aliases {
        let found = norm
            .iter()
            .enumerate()
            .find(|(i, value)| !used[*i] && *value == alias)
            .map(|(i, _)| i)
            .or_else(|| {
                if exact {
                    None
                } else {
                    norm.iter()
                        .enumerate()
                        .find(|(i, value)| !used[*i] && alias.chars().count() >= 5 && value.starts_with(alias))
                        .map(|(i, _)| i)
                }
            });
        if let Some(index) = found {
            used[index] = true;
            return Some(index);
        }
    }
    None
}

/// Detects which header column holds which field.
pub fn detect_mapping(headers: &[String]) -> CsvMapping {
    let norm: Vec<String> = headers.iter().map(|header| normalize_header(header)).collect();
    let mut used = vec![false; norm.len()];

    // Order matters so that e.g. "Valutadatum" isn't taken as the booking date.
    let value_date = find_column(&norm, &mut used, VALUE_DATE, false);
    let date = find_column(&norm, &mut used, DATE, false);
    let debit = find_column(&norm, &mut used, DEBIT, true);
    let credit = find_column(&norm, &mut used, CREDIT, true);
    let mut balance = find_column(&norm, &mut used, BALANCE, false);
    let mut amount = find_column(&norm, &mut used, AMOUNT, false);
    let partner_iban = find_column(&norm, &mut used, PARTNER_IBAN, false);
    let payer = find_column(&norm, &mut used, PAYER, false);
    let mut partner = find_column(&norm, &mut used, PARTNER, false);
    let mut purpose = find_column(&norm, &mut used, PURPOSE, false);
    let mut currency = find_column(&norm, &mut used, CURRENCY, false);
    let currency_alias_found = currency.is_some();

    if currency.is_some()
        && norm.iter().any(|value| value == "currencyamount")
        && norm.iter().any(|value| value == "amount")
    {
        currency = None;
    }

    if partner.is_none() && payer.is_none() {
        partner = find_column(&norm, &mut used, OWN_PARTNER, true);
    }
    if amount.is_none() && debit.is_none() && credit.is_none() {
        amount = find_column(&norm, &mut used, OWN_AMOUNT, true);
    }
    if purpose.is_none() {
        purpose = find_column(&norm, &mut used, OWN_PURPOSE, true);
    }
    if currency.is_none() && !currency_alias_found {
        currency = find_column(&norm, &mut used, OWN_CURRENCY, true);
    }
    if balance.is_none() {
        balance = find_column(&norm, &mut used, OWN_BALANCE, true);
    }

    let final_partner = partner.or(payer);
    CsvMapping {
        date,
        value_date,
        amount,
        debit: if amount.is_none() { debit } else { None },
        credit: if amount.is_none() { credit } else { None },
        partner: final_partner,
        payer: if final_partner.is_some() { payer } else { None },
        partner_iban,
        purpose,
        currency,
        balance,
    }
}

fn preamble_iban(preamble: &[Vec<String>]) -> Option<String> {
    preamble
        .iter()
        .flat_map(|row| row.iter())
        .flat_map(|cell| find_ibans(cell))
        .map(|candidate| normalize_iban(&candidate))
        .find(|candidate| is_valid_iban(candidate))
}

/// RFC 4180 record splitting: quoted fields may contain delimiters, doubled quotes and newlines.
pub fn split_records(text: &str, delimiter: char, max_non_blank_records: usize) -> Vec<Vec<String>> {
    if max_non_blank_records == 0 {
        return Vec::new();
    }
    let chars: Vec<char> = text.chars().collect();
    let mut records = Vec::new();
    let mut field = String::new();
    let mut record: Vec<String> = Vec::new();
    let mut in_quotes = false;
    let mut non_blank_records = 0usize;
    let mut i = 0usize;
    while i < chars.len() {
        let c = chars[i];
        if in_quotes && c == '"' && i + 1 < chars.len() && chars[i + 1] == '"' {
            field.push('"');
            i += 1;
        } else if c == '"' && (in_quotes || field.trim().is_empty()) {
            in_quotes = !in_quotes;
            if in_quotes {
                field.clear();
            }
        } else if !in_quotes && c == delimiter {
            record.push(std::mem::take(&mut field));
        } else if !in_quotes && (c == '\n' || c == '\r') {
            if c == '\r' && i + 1 < chars.len() && chars[i + 1] == '\n' {
                i += 1;
            }
            record.push(std::mem::take(&mut field));
            if record.iter().any(|value| !value.trim().is_empty()) {
                non_blank_records += 1;
            }
            records.push(std::mem::take(&mut record));
            if non_blank_records >= max_non_blank_records && max_non_blank_records != usize::MAX {
                return records;
            }
        } else {
            field.push(c);
        }
        i += 1;
    }
    if !field.is_empty() || !record.is_empty() {
        record.push(field);
        records.push(record);
    }
    records
}
