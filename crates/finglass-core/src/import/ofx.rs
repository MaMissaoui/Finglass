//! Reads OFX bank statement files, both OFX 1.x SGML and OFX 2.x XML.

use roxmltree::{Document, Node};

use super::model::{ImportError, ImportedBooking, ParsedStatement, StatementFormat};
use super::util::{
    date_from_ymd, decode_iso_8859_1, decode_text, decode_xml, is_valid_iban, normalize_iban, parse_decimal,
    require_no_doctype,
};
use time::Date;

/// Whether the bytes look like an OFX file.
pub fn looks_like(bytes: &[u8]) -> bool {
    let head = decode_iso_8859_1(&bytes[..bytes.len().min(4096)]);
    (head.contains("OFXHEADER:") && head.contains("<OFX>")) || (head.contains("<OFX>") && head.contains("<STMTTRN>"))
}

/// Reads an OFX file.
pub fn parse(bytes: &[u8]) -> Result<ParsedStatement, ImportError> {
    if !looks_like(bytes) {
        return Err(ImportError::invalid("Not an OFX file"));
    }
    let text = decode_text(bytes);
    let is_xml = text.trim_start().starts_with("<?xml") || text.contains("</");
    if is_xml && !is_sgml(&text) {
        parse_xml_ofx(bytes)
    } else {
        parse_sgml_ofx(&text)
    }
}

fn is_sgml(text: &str) -> bool {
    text.contains("OFXHEADER:") || !text.contains("</STMTTRN>")
}

fn parse_sgml_ofx(text: &str) -> Result<ParsedStatement, ImportError> {
    let ofx_start = text
        .find("<OFX>")
        .ok_or_else(|| ImportError::invalid("OFX root not found"))?;
    let body = &text[ofx_start..];
    let account_iban = extract_sgml_account_id(body);
    let accounts = parse_sgml_statement_accounts(body);
    let currency = extract_sgml_currency(body);
    let transactions = extract_sgml_transactions(body);
    let mut bookings = Vec::new();
    let mut skipped = Vec::new();

    for (index, block) in transactions.iter().enumerate() {
        match parse_sgml_transaction(block, currency.as_deref()) {
            Ok(booking) => bookings.push(booking),
            Err(reason) => skipped.push(format!("Record {}: {reason}", index + 1)),
        }
    }

    Ok(ParsedStatement {
        format: StatementFormat::Ofx,
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

fn extract_sgml_account_id(text: &str) -> Option<String> {
    for tag in ["BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"] {
        let Some(block) = extract_sgml_block(text, tag) else {
            continue;
        };
        let Some(id) = extract_sgml_leaf(&block, "ACCTID") else {
            continue;
        };
        let normalized = normalize_iban(&id);
        if is_valid_iban(&normalized) {
            return Some(normalized);
        }
    }
    None
}

fn extract_sgml_currency(text: &str) -> Option<String> {
    extract_sgml_leaf(text, "CURDEF")
        .map(|value| value.trim().to_uppercase())
        .filter(|value| value.chars().count() == 3)
}

fn extract_sgml_transactions(text: &str) -> Vec<String> {
    let mut result = Vec::new();
    let mut start = text.find("<STMTTRN>");
    while let Some(begin) = start {
        let Some(end) = text[begin..].find("</STMTTRN>") else {
            break;
        };
        let end = begin + end + "</STMTTRN>".len();
        result.push(text[begin..end].to_owned());
        start = text[end..].find("<STMTTRN>").map(|offset| end + offset);
    }
    result
}

fn parse_sgml_transaction(block: &str, currency: Option<&str>) -> Result<ImportedBooking, String> {
    let amount_text = extract_sgml_leaf(block, "TRNAMT").ok_or_else(|| "missing amount".to_owned())?;
    let amount = parse_ofx_amount(&amount_text).ok_or_else(|| format!("invalid amount '{amount_text}'"))?;
    let date_text = extract_sgml_leaf(block, "DTPOSTED").ok_or_else(|| "missing date".to_owned())?;
    let date = parse_ofx_date(&date_text).ok_or_else(|| format!("invalid date '{date_text}'"))?;
    let name = decode_xml_entities(
        extract_sgml_leaf(block, "NAME")
            .or_else(|| extract_sgml_leaf(block, "PAYEE").and_then(|payee| extract_sgml_leaf(&payee, "NAME")))
            .as_deref(),
    );
    let memo = decode_xml_entities(extract_sgml_leaf(block, "MEMO").as_deref());
    let fitid = extract_sgml_leaf(block, "FITID");
    Ok(ImportedBooking {
        date,
        value_date: None,
        amount,
        currency: currency.map(str::to_owned),
        partner_name: name.map(|name| name.trim().to_owned()).filter(|name| !name.is_empty()),
        purpose: memo.map(|memo| memo.trim().to_owned()).filter(|memo| !memo.is_empty()),
        bank_reference: fitid
            .map(|fitid| fitid.trim().to_owned())
            .filter(|fitid| !fitid.is_empty()),
        partner_iban: None,
        balance_after: None,
    })
}

fn extract_sgml_block(text: &str, tag: &str) -> Option<String> {
    let start = text.find(&format!("<{tag}>"))?;
    let close = format!("</{tag}>");
    let end = text[start..].find(&close)?;
    let end = start + end + close.len();
    Some(text[start..end].to_owned())
}

fn extract_sgml_leaf(text: &str, tag: &str) -> Option<String> {
    let start = text.find(&format!("<{tag}>"))?;
    let value_start = start + tag.len() + 2;
    let end = text[value_start..]
        .find('<')
        .map(|offset| value_start + offset)
        .unwrap_or(text.len());
    Some(text[value_start..end].to_owned())
}

fn parse_xml_ofx(bytes: &[u8]) -> Result<ParsedStatement, ImportError> {
    let text = decode_xml(bytes);
    require_no_doctype(&text)?;
    let document =
        Document::parse(&text).map_err(|error| ImportError::invalid(format!("Malformed OFX XML: {error}")))?;
    let root = document.root_element();
    let account_iban = find_account_id(root);
    let accounts = parse_xml_statement_accounts(root);
    let currency = find_currency(root);
    let transactions = xml_descendants(root, "STMTTRN");
    let mut bookings = Vec::new();
    let mut skipped = Vec::new();

    for (index, transaction) in transactions.iter().enumerate() {
        match parse_xml_transaction(*transaction, currency.as_deref()) {
            Ok(booking) => bookings.push(booking),
            Err(reason) => skipped.push(format!("Record {}: {reason}", index + 1)),
        }
    }

    Ok(ParsedStatement {
        format: StatementFormat::Ofx,
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

fn parse_xml_transaction(element: Node<'_, '_>, currency: Option<&str>) -> Result<ImportedBooking, String> {
    let amount_text = xml_child(element, "TRNAMT")
        .map(|node| text_content(node).trim().to_owned())
        .ok_or_else(|| "missing amount".to_owned())?;
    let amount = parse_ofx_amount(&amount_text).ok_or_else(|| format!("invalid amount '{amount_text}'"))?;
    let date_text = xml_child(element, "DTPOSTED")
        .map(|node| text_content(node).trim().to_owned())
        .ok_or_else(|| "missing date".to_owned())?;
    let date = parse_ofx_date(&date_text).ok_or_else(|| format!("invalid date '{date_text}'"))?;
    let name = xml_child(element, "NAME")
        .map(|node| text_content(node).trim().to_owned())
        .or_else(|| {
            xml_child(element, "PAYEE")
                .and_then(|payee| xml_child(payee, "NAME"))
                .map(|node| text_content(node).trim().to_owned())
        });
    let memo = xml_child(element, "MEMO").map(|node| text_content(node).trim().to_owned());
    let fitid = xml_child(element, "FITID").map(|node| text_content(node).trim().to_owned());
    Ok(ImportedBooking {
        date,
        value_date: None,
        amount,
        currency: currency.map(str::to_owned),
        partner_name: name.filter(|name| !name.is_empty()),
        purpose: memo.filter(|memo| !memo.is_empty()),
        bank_reference: fitid.filter(|fitid| !fitid.is_empty()),
        partner_iban: None,
        balance_after: None,
    })
}

fn find_account_id(root: Node<'_, '_>) -> Option<String> {
    for tag in ["BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"] {
        let Some(block) = xml_descendants(root, tag).into_iter().next() else {
            continue;
        };
        let Some(id) = xml_child(block, "ACCTID").map(|node| text_content(node).trim().to_owned()) else {
            continue;
        };
        let normalized = normalize_iban(&id);
        if is_valid_iban(&normalized) {
            return Some(normalized);
        }
    }
    None
}

fn find_currency(root: Node<'_, '_>) -> Option<String> {
    xml_child(root, "CURDEF")
        .map(|node| text_content(node).trim().to_uppercase())
        .filter(|value| value.chars().count() == 3)
        .or_else(|| {
            xml_descendants(root, "CURDEF")
                .into_iter()
                .next()
                .map(|node| text_content(node).trim().to_uppercase())
                .filter(|value| value.chars().count() == 3)
        })
}

fn parse_sgml_statement_accounts(text: &str) -> Vec<String> {
    let mut result = Vec::new();
    for tag in ["STMTRS", "CCSTMTRS"] {
        let open = format!("<{tag}>");
        let close = format!("</{tag}>");
        let mut start = text.find(&open);
        while let Some(begin) = start {
            let Some(relative_end) = text[begin..].find(&close) else {
                break;
            };
            let end = begin + relative_end + close.len();
            if let Some(id) = extract_sgml_leaf(&text[begin..end], "ACCTID") {
                let id = id.trim();
                if !id.is_empty() {
                    let normalized = normalize_iban(id);
                    let account = if is_valid_iban(&normalized) {
                        normalized
                    } else {
                        id.to_owned()
                    };
                    if !result.contains(&account) {
                        result.push(account);
                    }
                }
            }
            start = text[end..].find(&open).map(|offset| end + offset);
        }
    }
    result
}

fn parse_xml_statement_accounts(root: Node<'_, '_>) -> Vec<String> {
    let mut result = Vec::new();
    for tag in ["STMTRS", "CCSTMTRS"] {
        for block in xml_descendants(root, tag) {
            let id = ["BANKACCTFROM", "CCACCTFROM", "INVACCTFROM"]
                .iter()
                .find_map(|account_tag| {
                    xml_child(block, account_tag)
                        .and_then(|account| xml_child(account, "ACCTID"))
                        .map(|node| text_content(node).trim().to_owned())
                });
            let Some(id) = id else {
                continue;
            };
            let normalized = normalize_iban(&id);
            let account = if is_valid_iban(&normalized) { normalized } else { id };
            if !result.contains(&account) {
                result.push(account);
            }
        }
    }
    result
}

fn xml_child<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Option<Node<'a, 'input>> {
    node.children()
        .find(|child| child.is_element() && child.tag_name().name() == name)
}

fn xml_descendants<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Vec<Node<'a, 'input>> {
    node.descendants()
        .filter(|child| child.is_element() && child.tag_name().name() == name)
        .collect()
}

fn text_content(node: Node<'_, '_>) -> String {
    let mut text = String::new();
    for descendant in node.descendants() {
        if descendant.is_text()
            && let Some(value) = descendant.text()
        {
            text.push_str(value);
        }
    }
    text
}

fn parse_ofx_date(text: &str) -> Option<Date> {
    let digits: String = text.chars().filter(char::is_ascii_digit).collect();
    if digits.len() < 8 {
        return None;
    }
    let prefix = &digits[..8];
    let year: i32 = prefix[0..4].parse().ok()?;
    let month: u32 = prefix[4..6].parse().ok()?;
    let day: u32 = prefix[6..8].parse().ok()?;
    date_from_ymd(year, month, day)
}

fn parse_ofx_amount(text: &str) -> Option<rust_decimal::Decimal> {
    let t = text.trim();
    let has_dot = t.contains('.');
    let has_comma = t.contains(',');
    let normalized = if has_dot && has_comma {
        let decimal = if t.rfind(',') > t.rfind('.') { ',' } else { '.' };
        let grouping = if decimal == ',' { '.' } else { ',' };
        t.replace(grouping, "").replace(decimal, ".")
    } else if !has_dot && has_comma {
        t.replace(',', ".")
    } else {
        t.replace(',', "")
    };
    parse_decimal(&normalized)
}

fn decode_xml_entities(text: Option<&str>) -> Option<String> {
    let text = text?;
    // &amp; last, so "&amp;lt;" stays the literal text "&lt;"
    Some(
        text.replace("&lt;", "<")
            .replace("&gt;", ">")
            .replace("&quot;", "\"")
            .replace("&apos;", "'")
            .replace("&amp;", "&"),
    )
}
