//! Reads the first worksheet of an untrusted XLSX file as rows of cell strings, in memory.

use std::io::{Cursor, Read};

use roxmltree::{Document, Node};
use rust_decimal::Decimal;
use rust_decimal::prelude::ToPrimitive;

use super::model::ImportError;
use super::util::{decode_xml, format_iso_date, is_decimal_syntax, parse_decimal};

const MAX_ENTRIES: usize = 2000;
const MAX_TOTAL_BYTES: u64 = 60 * 1024 * 1024;
const MAX_PART_BYTES: usize = 20 * 1024 * 1024;
const MAX_ROWS: usize = 100_000;
const MAX_COLUMNS: usize = 200;
const WORKBOOK: &str = "xl/workbook.xml";
const RELS: &str = "xl/_rels/workbook.xml.rels";
const SHARED_STRINGS: &str = "xl/sharedStrings.xml";
const STYLES: &str = "xl/styles.xml";

/// The first worksheet, row by row.
pub fn first_sheet_rows(bytes: &[u8]) -> Result<Vec<Vec<String>>, ImportError> {
    let parts = read_parts(bytes)?;

    let workbook_bytes = required_part(&parts, WORKBOOK)?;
    let rels_bytes = required_part(&parts, RELS)?;
    check_part_size(workbook_bytes)?;
    check_part_size(rels_bytes)?;

    let workbook_text = part_text(workbook_bytes)?;
    let rels_text = part_text(rels_bytes)?;

    let workbook_document = Document::parse(&workbook_text).map_err(|_| ImportError::XlsxUnreadable)?;
    ensure_depth(&workbook_document)?;
    let workbook = workbook_document.root_element();
    let sheet_ref = xml_descendants(workbook, "sheet")
        .into_iter()
        .next()
        .and_then(relationship_id)
        .ok_or(ImportError::XlsxUnreadable)?;

    let rels_document = Document::parse(&rels_text).map_err(|_| ImportError::XlsxUnreadable)?;
    ensure_depth(&rels_document)?;
    let rels_root = rels_document.root_element();
    let target = xml_children(rels_root, "Relationship")
        .into_iter()
        .filter(|relationship| relationship.attribute("Id") == Some(sheet_ref.as_str()))
        .map(|relationship| relationship.attribute("Target").unwrap_or(""))
        .find(|target| !target.starts_with('/') || !target.trim_start_matches('/').is_empty())
        .ok_or(ImportError::XlsxUnreadable)?;
    let sheet_name = resolve_target_path(target);

    let date1904 = xml_children(workbook, "workbookPr").into_iter().any(|properties| {
        properties
            .attribute("date1904")
            .is_some_and(|value| value.eq_ignore_ascii_case("true"))
    });

    let sheet_bytes = required_part(&parts, &sheet_name)?;
    check_part_size(sheet_bytes)?;
    let shared_strings = match selected_part(&parts, SHARED_STRINGS)? {
        Some(bytes) => {
            check_part_size(bytes)?;
            Some(shared_strings(bytes)?)
        }
        None => None,
    };
    let date_styles = match selected_part(&parts, STYLES)? {
        Some(bytes) => {
            check_part_size(bytes)?;
            Some(date_styles(bytes)?)
        }
        None => None,
    };

    let sheet_text = part_text(sheet_bytes)?;
    let sheet_document = Document::parse(&sheet_text).map_err(|_| ImportError::XlsxUnreadable)?;
    ensure_depth(&sheet_document)?;
    rows(
        sheet_document.root_element(),
        shared_strings.as_deref(),
        date_styles.as_ref(),
        date1904,
    )
}

fn read_parts(bytes: &[u8]) -> Result<Vec<(String, Vec<u8>)>, ImportError> {
    let mut archive = zip::ZipArchive::new(Cursor::new(bytes)).map_err(|_| ImportError::XlsxUnreadable)?;
    if archive.len() > MAX_ENTRIES {
        return Err(ImportError::XlsxTooLarge);
    }
    let mut parts = Vec::new();
    let mut total = 0u64;
    for index in 0..archive.len() {
        let mut entry = archive.by_index(index).map_err(|_| ImportError::XlsxUnreadable)?;
        let name = entry.name().map(|name| name.into_owned()).unwrap_or_default();
        let mut content = Vec::new();
        let mut buffer = [0u8; 64 * 1024];
        loop {
            let read = entry.read(&mut buffer).map_err(|_| ImportError::XlsxUnreadable)?;
            if read == 0 {
                break;
            }
            total += read as u64;
            if total > MAX_TOTAL_BYTES {
                return Err(ImportError::XlsxTooLarge);
            }
            if !entry.is_dir() {
                content.extend_from_slice(&buffer[..read]);
            }
        }
        if !entry.is_dir() {
            parts.push((name, content));
        }
    }
    Ok(parts)
}

fn selected_part<'a>(parts: &'a [(String, Vec<u8>)], name: &str) -> Result<Option<&'a [u8]>, ImportError> {
    let matching: Vec<&[u8]> = parts
        .iter()
        .filter(|(part_name, _)| part_name == name)
        .map(|(_, content)| content.as_slice())
        .collect();
    if matching.len() > 1 {
        return Err(ImportError::XlsxUnreadable);
    }
    Ok(matching.first().copied())
}

fn required_part<'a>(parts: &'a [(String, Vec<u8>)], name: &str) -> Result<&'a [u8], ImportError> {
    selected_part(parts, name)?.ok_or(ImportError::XlsxUnreadable)
}

fn check_part_size(bytes: &[u8]) -> Result<(), ImportError> {
    if bytes.len() > MAX_PART_BYTES {
        Err(ImportError::XlsxTooLarge)
    } else {
        Ok(())
    }
}

fn part_text(bytes: &[u8]) -> Result<String, ImportError> {
    let text = decode_xml(bytes);
    if text.contains("<!DOCTYPE") {
        return Err(ImportError::XlsxUnreadable);
    }
    Ok(text)
}

fn ensure_depth(document: &Document<'_>) -> Result<(), ImportError> {
    for node in document.descendants() {
        if node.is_element() && node.ancestors().filter(|ancestor| ancestor.is_element()).count() > 64 {
            return Err(ImportError::XlsxUnreadable);
        }
    }
    Ok(())
}

fn relationship_id(node: Node<'_, '_>) -> Option<String> {
    node.attributes()
        .find(|attribute| attribute.name() == "id")
        .map(|attribute| attribute.value().to_owned())
}

fn resolve_target_path(target: &str) -> String {
    let target = if let Some(stripped) = target.strip_prefix('/') {
        stripped
    } else {
        return resolve_segments(format!("xl/{target}"));
    };
    resolve_segments(target.to_owned())
}

fn resolve_segments(path: String) -> String {
    let mut parts: Vec<&str> = Vec::new();
    for segment in path.split('/') {
        match segment {
            "" | "." => {}
            ".." => {
                if parts.last().is_some_and(|last| *last != "..") {
                    parts.pop();
                } else {
                    parts.push("..");
                }
            }
            other => parts.push(other),
        }
    }
    parts.join("/")
}

fn shared_strings(bytes: &[u8]) -> Result<Vec<String>, ImportError> {
    let text = part_text(bytes)?;
    let document = Document::parse(&text).map_err(|_| ImportError::XlsxUnreadable)?;
    ensure_depth(&document)?;
    let root = document.root_element();
    Ok(xml_children(root, "si")
        .into_iter()
        .map(|si| {
            let mut values = Vec::new();
            for plain in xml_children(si, "t") {
                values.push(text_content(plain));
            }
            for run in xml_children(si, "r") {
                for text in xml_children(run, "t") {
                    values.push(text_content(text));
                }
            }
            values.join("")
        })
        .collect())
}

fn date_styles(bytes: &[u8]) -> Result<std::collections::HashMap<usize, bool>, ImportError> {
    let text = part_text(bytes)?;
    let document = Document::parse(&text).map_err(|_| ImportError::XlsxUnreadable)?;
    ensure_depth(&document)?;
    let root = document.root_element();
    let mut custom_dates = std::collections::HashMap::new();
    for format in xml_descendants(root, "numFmt") {
        if let Some(id) = format.attribute("numFmtId").and_then(|v| v.parse::<usize>().ok()) {
            custom_dates.insert(id, is_date_format_code(format.attribute("formatCode").unwrap_or("")));
        }
    }
    let mut result = std::collections::HashMap::new();
    let Some(cell_xfs) = xml_children(root, "cellXfs").into_iter().next() else {
        return Ok(result);
    };
    for (index, xf) in xml_children(cell_xfs, "xf").into_iter().enumerate() {
        if let Some(id) = xf.attribute("numFmtId").and_then(|v| v.parse::<usize>().ok()) {
            result.insert(
                index,
                (14..=22).contains(&id) || (45..=47).contains(&id) || custom_dates.get(&id).copied().unwrap_or(false),
            );
        }
    }
    Ok(result)
}

fn is_date_format_code(code: &str) -> bool {
    let mut cleaned = String::new();
    let mut chars = code.chars();
    while let Some(ch) = chars.next() {
        match ch {
            '"' => {
                for inner in chars.by_ref() {
                    if inner == '"' {
                        break;
                    }
                }
            }
            '[' => {
                for inner in chars.by_ref() {
                    if inner == ']' {
                        break;
                    }
                }
            }
            _ => cleaned.push(ch),
        }
    }
    let lowered = cleaned.to_lowercase();
    lowered.contains('d') || lowered.contains('y')
}

fn rows(
    sheet: Node<'_, '_>,
    shared_strings: Option<&[String]>,
    date_styles: Option<&std::collections::HashMap<usize, bool>>,
    date1904: bool,
) -> Result<Vec<Vec<String>>, ImportError> {
    let mut result: Vec<Vec<String>> = Vec::new();
    for row in xml_descendants(sheet, "row") {
        let index = match row.attribute("r").and_then(|value| value.parse::<i64>().ok()) {
            Some(value) => {
                let Some(index) = value.checked_sub(1) else {
                    return Err(ImportError::XlsxTooLarge);
                };
                if index < 0 || index >= MAX_ROWS as i64 {
                    return Err(ImportError::XlsxTooLarge);
                }
                index as usize
            }
            None => result.len(),
        };
        let mut cells: Vec<String> = Vec::new();
        let mut width = 0usize;
        for cell in xml_children(row, "c") {
            let explicit = cell.attribute("r").map(column_index).unwrap_or(-1);
            let column = if explicit >= 0 { explicit as usize } else { width };
            if column >= MAX_COLUMNS {
                return Err(ImportError::XlsxTooLarge);
            }
            while cells.len() <= column {
                cells.push(String::new());
            }
            cells[column] = cell_value(cell, shared_strings, date_styles, date1904);
            width = column + 1;
        }
        while result.len() < index {
            result.push(Vec::new());
        }
        if result.len() == index {
            result.push(cells);
        } else {
            result[index] = cells;
        }
    }
    Ok(result)
}

fn cell_value(
    cell: Node<'_, '_>,
    shared_strings: Option<&[String]>,
    date_styles: Option<&std::collections::HashMap<usize, bool>>,
    date1904: bool,
) -> String {
    let value = xml_child(cell, "v").map(text_content).unwrap_or_default();
    match cell.attribute("t").unwrap_or("") {
        "s" => value
            .trim()
            .parse::<usize>()
            .ok()
            .and_then(|index| shared_strings.and_then(|strings| strings.get(index)))
            .cloned()
            .unwrap_or_default(),
        "inlineStr" => xml_child(cell, "is")
            .map(|inline| {
                let mut values = Vec::new();
                for text in xml_children(inline, "t") {
                    values.push(text_content(text));
                }
                for run in xml_children(inline, "r") {
                    for text in xml_children(run, "t") {
                        values.push(text_content(text));
                    }
                }
                values.join("")
            })
            .unwrap_or_default(),
        "str" => value,
        "b" => {
            let trimmed = value.trim();
            if trimmed == "1" || trimmed.eq_ignore_ascii_case("true") {
                "TRUE".to_owned()
            } else {
                "FALSE".to_owned()
            }
        }
        "e" => String::new(),
        _ => {
            let text = value.trim();
            if !is_decimal_syntax(text) {
                return String::new();
            }
            let Some(number) = parse_decimal(text) else {
                return text.to_owned();
            };
            let style = cell
                .attribute("s")
                .and_then(|value| value.parse::<usize>().ok())
                .unwrap_or(0);
            if date_styles
                .and_then(|styles| styles.get(&style))
                .copied()
                .unwrap_or(false)
            {
                date_of(number, date1904).unwrap_or_default()
            } else {
                number.normalize().to_string()
            }
        }
    }
}

fn date_of(serial: Decimal, date1904: bool) -> Option<String> {
    let maximum = Decimal::new(2_958_465, 0);
    if serial < Decimal::ZERO || serial > maximum {
        return None;
    }
    let days = serial.trunc().to_i64()?;
    let base = if date1904 {
        time::Date::from_calendar_date(1904, time::Month::January, 1).ok()?
    } else {
        time::Date::from_calendar_date(1899, time::Month::December, 30).ok()?
    };
    base.checked_add(time::Duration::days(days)).map(format_iso_date)
}

/// "AB" → 27, "B2" → 1; the letters decide and the row digits are ignored.
fn column_index(reference: &str) -> i64 {
    let mut index: i64 = 0;
    for ch in reference.chars() {
        let upper = ch.to_ascii_uppercase();
        if !upper.is_ascii_uppercase() {
            break;
        }
        index = index
            .saturating_mul(26)
            .saturating_add(i64::from(upper as u8 - b'A' + 1));
    }
    index - 1
}

fn xml_children<'a, 'input>(node: Node<'a, 'input>, name: &str) -> Vec<Node<'a, 'input>> {
    node.children()
        .filter(|child| child.is_element() && child.tag_name().name() == name)
        .collect()
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
