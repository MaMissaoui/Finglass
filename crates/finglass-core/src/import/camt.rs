//! Reads ISO 20022 CAMT.053 (and CAMT.052) account statements, as XML or a ZIP of XML files.

use std::io::{Cursor, Read};

use roxmltree::{Document, Node};
use rust_decimal::Decimal;

use super::model::{ClosingBalance, ImportError, ImportedBooking, ParsedStatement, StatementFormat};
use super::util::{
    collapse_whitespace, decode_xml, is_valid_iban, normalize_iban, parse_decimal, parse_iso_date, require_no_doctype,
};
use time::Date;

const MAX_ENTRY_BYTES: usize = 50 * 1024 * 1024;
const MAX_ZIP_BYTES: u64 = 200 * 1024 * 1024;

/// Whether the bytes look like a CAMT file or ZIP.
pub fn looks_like_camt(bytes: &[u8]) -> bool {
    if is_zip(bytes) {
        return true;
    }
    let head = String::from_utf8_lossy(&bytes[..bytes.len().min(2048)]);
    head.contains("<Document")
        && (head.contains("camt.053") || head.contains("camt.052") || head.contains("BkToCstmrStmt"))
}

/// Reads a CAMT statement.
pub fn parse(bytes: &[u8]) -> Result<ParsedStatement, ImportError> {
    let documents = if is_zip(bytes) {
        unzip_xml(bytes)?
    } else {
        vec![bytes.to_vec()]
    };
    let mut bookings = Vec::new();
    let mut skipped = Vec::new();
    let mut accounts = Vec::new();
    let mut message_types = Vec::new();
    let mut reports: Vec<(Date, Option<ClosingBalance>)> = Vec::new();
    let mut iban: Option<String> = None;
    let mut entry_no = 0usize;

    for xml in documents {
        let text = decode_xml(&xml);
        require_no_doctype(&text)?;
        let document =
            Document::parse(&text).map_err(|error| ImportError::invalid(format!("Malformed CAMT XML: {error}")))?;
        let root = document.root_element();
        if let Some(message_type) = message_type(root)
            && !message_types.contains(&message_type)
        {
            message_types.push(message_type);
        }
        let mut statements = descendants(root, "Stmt");
        if statements.is_empty() {
            statements = descendants(root, "Rpt");
        }
        for statement in statements {
            let account_id = child(statement, "Acct").and_then(|acct| child(acct, "Id"));
            let account_iban = account_id
                .and_then(|id| child(id, "IBAN"))
                .map(|iban| normalize_iban(&text_content(iban)));
            let account_other = account_id
                .and_then(|id| child(id, "Othr"))
                .and_then(|other| child(other, "Id"))
                .map(|id| text_content(id).trim().to_owned());
            let account = account_iban
                .clone()
                .or_else(|| account_other.filter(|value| !value.is_empty()));
            if let Some(account) = account
                && !accounts.contains(&account)
            {
                accounts.push(account);
            }
            if iban.is_none() {
                iban = account_iban;
            }
            let entry_elements = children(statement, "Ntry");
            for entry in &entry_elements {
                entry_no += 1;
                match entry_to_bookings(*entry) {
                    Ok(mut parsed) => bookings.append(&mut parsed),
                    Err(reason) => skipped.push(format!("Entry {entry_no}: {reason}")),
                }
            }
            if let Some(report) = report_balance(statement, &entry_elements) {
                reports.push(report);
            }
        }
    }

    let closing_balance = if skipped.is_empty() {
        take_latest(&reports)
    } else {
        None
    };
    let format_detail = if message_types.is_empty() {
        None
    } else {
        Some(message_types.join("/"))
    };

    Ok(ParsedStatement {
        format: StatementFormat::Camt053,
        bookings,
        account_iban: iban,
        accounts,
        skipped,
        pending_skipped: 0,
        csv: None,
        format_detail,
        closing_balance,
    })
}

fn is_zip(bytes: &[u8]) -> bool {
    bytes.len() > 4 && bytes[0] == b'P' && bytes[1] == b'K'
}

fn unzip_xml(bytes: &[u8]) -> Result<Vec<Vec<u8>>, ImportError> {
    let mut archive =
        zip::ZipArchive::new(Cursor::new(bytes)).map_err(|_| ImportError::invalid("The ZIP file could not be read"))?;
    let mut result = Vec::new();
    let mut total = 0u64;
    for index in 0..archive.len() {
        let mut entry = archive
            .by_index(index)
            .map_err(|_| ImportError::invalid("The ZIP file could not be read"))?;
        let name = entry.name().map(|name| name.into_owned()).unwrap_or_default();
        if entry.is_dir() || !name.to_lowercase().ends_with(".xml") {
            continue;
        }
        let mut data = Vec::new();
        let mut limited = entry.by_ref().take((MAX_ENTRY_BYTES + 1) as u64);
        limited
            .read_to_end(&mut data)
            .map_err(|_| ImportError::invalid("The ZIP file could not be read"))?;
        if data.len() > MAX_ENTRY_BYTES {
            return Err(ImportError::invalid(format!("ZIP entry {name} is too large")));
        }
        total += data.len() as u64;
        if total > MAX_ZIP_BYTES {
            return Err(ImportError::invalid("ZIP content is too large"));
        }
        result.push(data);
    }
    if result.is_empty() {
        return Err(ImportError::invalid("The ZIP file contains no XML statements"));
    }
    Ok(result)
}

fn children<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Vec<Node<'a, 'input>> {
    node.children()
        .filter(|child| child.is_element() && child.tag_name().name() == name)
        .collect()
}

fn child<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Option<Node<'a, 'input>> {
    node.children()
        .find(|child| child.is_element() && child.tag_name().name() == name)
}

fn descendants<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Vec<Node<'a, 'input>> {
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

fn message_type(root: Node<'_, '_>) -> Option<String> {
    if let Some(namespace) = root.tag_name().namespace()
        && let Some(index) = namespace.find("camt.")
    {
        let rest = &namespace[index + 5..];
        let digits: String = rest.chars().take_while(|c| c.is_ascii_digit()).collect();
        if digits.len() == 3 {
            return Some(format!("CAMT.{digits}"));
        }
    }
    let first = root.children().find(|child| child.is_element())?;
    match first.tag_name().name() {
        "BkToCstmrAcctRpt" => Some("CAMT.052".to_owned()),
        "BkToCstmrStmt" => Some("CAMT.053".to_owned()),
        "BkToCstmrDbtCdtNtfctn" => Some("CAMT.054".to_owned()),
        _ => None,
    }
}

struct ParsedBalance<'a, 'input> {
    amount: Decimal,
    currency: Option<String>,
    element: Node<'a, 'input>,
}

fn report_balance<'a, 'input>(
    stmt: Node<'a, 'input>,
    entries: &[Node<'a, 'input>],
) -> Option<(Date, Option<ClosingBalance>)> {
    let closing = balance_of(stmt, "CLBD").or_else(|| balance_of(stmt, "ITBD"))?;
    let date = balance_date(closing.element)?;
    let opening = match balance_of(stmt, "OPBD").or_else(|| balance_of(stmt, "PRCD")) {
        Some(balance) => balance,
        None => return Some((date, None)),
    };
    let mut booked = Decimal::ZERO;
    let mut pending = Decimal::ZERO;
    let mut pending_count = 0u32;
    let mut currencies: Vec<Option<String>> = Vec::new();
    for entry in entries {
        let Some(amount) = entry_signed_amount(*entry) else {
            continue;
        };
        currencies.push(
            child(*entry, "Amt")
                .and_then(|amt| attribute(amt, "Ccy"))
                .filter(|value| !value.is_empty()),
        );
        if matches!(entry_status(*entry).as_deref(), Some("BOOK") | None) {
            booked += amount;
        } else {
            pending += amount;
            pending_count += 1;
        }
    }
    if let Some(currency) = &closing.currency
        && currencies
            .iter()
            .any(|entry_currency| entry_currency.as_deref() != Some(currency))
    {
        return Some((date, None));
    }
    let reconciled = if opening.amount + booked == closing.amount {
        Some(ClosingBalance {
            amount: closing.amount,
            date,
            currency: closing.currency.clone(),
            pending_excluded: 0,
        })
    } else if pending_count > 0 && opening.amount + booked + pending == closing.amount {
        Some(ClosingBalance {
            amount: closing.amount - pending,
            date,
            currency: closing.currency.clone(),
            pending_excluded: pending_count,
        })
    } else {
        None
    };
    Some((date, reconciled))
}

fn balance_of<'a, 'input>(stmt: Node<'a, 'input>, code: &str) -> Option<ParsedBalance<'a, 'input>> {
    children(stmt, "Bal")
        .into_iter()
        .find(|balance| {
            child(*balance, "Tp")
                .and_then(|tp| child(tp, "CdOrPrtry"))
                .and_then(|prtry| child(prtry, "Cd"))
                .map(|cd| text_content(cd).trim() == code)
                .unwrap_or(false)
        })
        .and_then(parse_balance)
}

fn parse_balance<'a, 'input>(balance: Node<'a, 'input>) -> Option<ParsedBalance<'a, 'input>> {
    let amount_element = child(balance, "Amt")?;
    let value = parse_decimal(&text_content(amount_element))?;
    let amount = if child(balance, "CdtDbtInd")
        .map(|indicator| text_content(indicator).trim() == "DBIT")
        .unwrap_or(false)
    {
        -value
    } else {
        value
    };
    Some(ParsedBalance {
        amount,
        currency: attribute(amount_element, "Ccy").filter(|value| !value.is_empty()),
        element: balance,
    })
}

fn balance_date(balance: Node<'_, '_>) -> Option<Date> {
    let date = child(balance, "Dt")?;
    let date = child(date, "Dt").or_else(|| child(date, "DtTm"))?;
    parse_iso_date(&text_content(date))
}

fn entry_status(entry: Node<'_, '_>) -> Option<String> {
    let status = child(entry, "Sts")?;
    let text = child(status, "Cd")
        .map(text_content)
        .unwrap_or_else(|| text_content(status));
    Some(text.trim().to_owned())
}

fn entry_signed_amount(entry: Node<'_, '_>) -> Option<Decimal> {
    let amount = child(entry, "Amt")?;
    let value = parse_decimal(&text_content(amount))?;
    if child(entry, "CdtDbtInd")
        .map(|indicator| text_content(indicator).trim() == "DBIT")
        .unwrap_or(false)
    {
        Some(-value)
    } else {
        Some(value)
    }
}

fn take_latest(reports: &[(Date, Option<ClosingBalance>)]) -> Option<ClosingBalance> {
    let latest = reports.iter().map(|(date, _)| *date).max()?;
    let at_latest: Vec<&Option<ClosingBalance>> = reports
        .iter()
        .filter(|(date, _)| *date == latest)
        .map(|(_, balance)| balance)
        .collect();
    if at_latest.iter().any(|balance| balance.is_none()) {
        return None;
    }
    let first = at_latest.first()?.as_ref()?;
    if at_latest.iter().all(|balance| {
        balance
            .as_ref()
            .is_some_and(|other| other.amount == first.amount && other.currency == first.currency)
    }) {
        Some(first.clone())
    } else {
        None
    }
}

fn entry_to_bookings(entry: Node<'_, '_>) -> Result<Vec<ImportedBooking>, String> {
    let status = entry_status(entry);
    if status.as_deref().is_some_and(|status| status != "BOOK") {
        return Ok(Vec::new());
    }

    let date = date_of(child(entry, "BookgDt"))
        .or_else(|| date_of(child(entry, "ValDt")))
        .ok_or_else(|| "no booking date".to_owned())?;
    let value_date = date_of(child(entry, "ValDt"));
    let amount_element = child(entry, "Amt").ok_or_else(|| "no amount".to_owned())?;
    let currency = attribute(amount_element, "Ccy").filter(|value| !value.is_empty());
    let debit = child(entry, "CdtDbtInd")
        .map(|indicator| text_content(indicator).trim() == "DBIT")
        .unwrap_or(false);
    let entry_reference = child(entry, "AcctSvcrRef").map(|reference| text_content(reference).trim().to_owned());

    let details = descendants(entry, "TxDtls");
    if details.len() > 1
        && details
            .iter()
            .all(|tx| child(*tx, "Amt").is_some() || child(*tx, "AmtDtls").is_some())
    {
        let mut bookings = Vec::new();
        for (index, tx) in details.iter().enumerate() {
            let tx_amount = child(*tx, "Amt")
                .or_else(|| child(*tx, "AmtDtls").and_then(|amt_dtls| descendants(amt_dtls, "Amt").first().copied()))
                .and_then(|element| amount(element))
                .ok_or_else(|| "batch item without amount".to_owned())?;
            let amount = if debit { -tx_amount } else { tx_amount };
            let reference = entry_reference.as_ref().map(|reference| format!("{reference}/{index}"));
            bookings.push(booking(
                date,
                value_date,
                amount,
                currency.clone(),
                Some(*tx),
                debit,
                reference,
                None,
            ));
        }
        return Ok(bookings);
    }

    let value = amount(amount_element).ok_or_else(|| "unreadable amount".to_owned())?;
    let amount = if debit { -value } else { value };
    Ok(vec![booking(
        date,
        value_date,
        amount,
        currency,
        details.first().copied(),
        debit,
        entry_reference,
        child(entry, "AddtlNtryInf").map(text_content),
    )])
}

#[allow(clippy::too_many_arguments)]
fn booking(
    date: Date,
    value_date: Option<Date>,
    amount: Decimal,
    currency: Option<String>,
    tx: Option<Node<'_, '_>>,
    debit: bool,
    reference: Option<String>,
    fallback_text: Option<String>,
) -> ImportedBooking {
    let parties = tx.and_then(|tx| child(tx, "RltdPties"));
    let party = parties.and_then(|parties| {
        if debit {
            child(parties, "Cdtr")
        } else {
            child(parties, "Dbtr")
        }
    });
    let party_name = party
        .and_then(|party| child(party, "Pty").or(Some(party)))
        .and_then(|party| child(party, "Nm"))
        .map(|name| text_content(name).trim().to_owned())
        .filter(|name| !name.is_empty());
    let party_iban = parties
        .and_then(|parties| {
            if debit {
                child(parties, "CdtrAcct")
            } else {
                child(parties, "DbtrAcct")
            }
        })
        .and_then(|account| child(account, "Id"))
        .and_then(|id| child(id, "IBAN"))
        .map(|iban| normalize_iban(&text_content(iban)))
        .filter(|iban| is_valid_iban(iban));

    let purpose = tx
        .and_then(|tx| child(tx, "RmtInf"))
        .map(|info| {
            children(info, "Ustrd")
                .iter()
                .map(|line| text_content(*line).trim().to_owned())
                .collect::<Vec<_>>()
                .join(" ")
        })
        .filter(|value| !value.is_empty())
        .or_else(|| {
            tx.and_then(|tx| child(tx, "AddtlTxInf"))
                .map(|info| text_content(info).trim().to_owned())
        })
        .or_else(|| fallback_text.map(|text| text.trim().to_owned()));

    let reference = tx
        .and_then(|tx| child(tx, "Refs"))
        .and_then(|refs| child(refs, "AcctSvcrRef"))
        .map(|reference| text_content(reference).trim().to_owned())
        .or(reference);

    ImportedBooking {
        date,
        value_date,
        amount,
        currency,
        partner_name: party_name,
        partner_iban: party_iban,
        purpose: purpose
            .map(|purpose| collapse_whitespace(&purpose))
            .filter(|purpose| !purpose.is_empty()),
        balance_after: None,
        bank_reference: reference.filter(|reference| !reference.is_empty() && reference != "NOTPROVIDED"),
    }
}

fn date_of(element: Option<Node<'_, '_>>) -> Option<Date> {
    let element = element?;
    let date = child(element, "Dt").or_else(|| child(element, "DtTm"))?;
    parse_iso_date(&text_content(date))
}

fn amount(element: Node<'_, '_>) -> Option<Decimal> {
    parse_decimal(&text_content(element)).map(|value| value.abs())
}

fn attribute<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Option<String> {
    node.attribute(name).map(str::to_owned)
}
