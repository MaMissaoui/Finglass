//! Minimal SpreadsheetML (`.xlsx`) writing on `zip` and hand-written XML, with no third-party workbook
//! library. Cell kinds: text and bold text as inline strings, plain numbers, money with `#,##0.00` and
//! dates as Excel serial numbers with `dd.mm.yyyy`. There are deliberately no formulas (`<f>`).
//!
//! A text value starting with `=`, `+`, `-` or `@` stays a plain inline string; XML is escaped and stripped
//! of characters invalid in XML 1.0, since bank text can contain them.

use std::fs::{self, File, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use time::{Date, Month};
use zip::ZipWriter;
use zip::write::SimpleFileOptions;

use crate::money::{Money, format_plain};

const XML_DECLARATION: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n";
const STYLES_BODY: &str = r##"<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <numFmts count="2"><numFmt numFmtId="164" formatCode="#,##0.00"/><numFmt numFmtId="165" formatCode="dd.mm.yyyy"/></numFmts>
 <fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
 <fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
 <borders count="1"><border/></borders>
 <cellStyleXfs count="1"><xf numFmtId="0" fontId="0"/></cellStyleXfs>
 <cellXfs count="4">
  <xf numFmtId="0" fontId="0" xfId="0"/>
  <xf numFmtId="0" fontId="1" applyFont="1" xfId="0"/>
  <xf numFmtId="164" fontId="0" applyNumberFormat="1" xfId="0"/>
  <xf numFmtId="165" fontId="0" applyNumberFormat="1" xfId="0"/>
 </cellXfs>
</styleSheet>"##;

const ROOT_RELS: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\"><Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"xl/workbook.xml\"/></Relationships>";

const MAX_SHEET_NAME_LENGTH: usize = 31;
const DEFAULT_SHEET_NAME: &str = "Sheet";
const UNIQUE_SUFFIX_ROOM: usize = 5;

/// Writes a file through a temporary file next to the target and moves the finished file into place, so a
/// failed export never leaves half a file at the target. On Unix the finished file has mode 0600.
pub fn write_atomically(target: &Path, write: impl FnOnce(&Path) -> io::Result<()>) -> io::Result<()> {
    let parent = match target.parent() {
        Some(parent) if !parent.as_os_str().is_empty() => parent.to_path_buf(),
        Some(_) => PathBuf::from("."),
        None => {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("the export target needs a parent directory: {}", target.display()),
            ));
        }
    };

    let (temp_path, temp_file) = create_temp_file(&parent)?;
    drop(temp_file);
    let mut guard = TempGuard {
        path: temp_path.clone(),
        armed: true,
    };

    write(&temp_path)?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&temp_path, fs::Permissions::from_mode(0o600))?;
    }

    fs::rename(&temp_path, target)?;
    guard.armed = false;
    Ok(())
}

struct TempGuard {
    path: PathBuf,
    armed: bool,
}

impl Drop for TempGuard {
    fn drop(&mut self) {
        if self.armed {
            let _ = fs::remove_file(&self.path);
        }
    }
}

fn create_temp_file(parent: &Path) -> io::Result<(PathBuf, File)> {
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    for _ in 0..1_000 {
        let number = COUNTER.fetch_add(1, Ordering::Relaxed);
        let path = parent.join(format!("findesk-export-{}-{number}.tmp", std::process::id()));
        match OpenOptions::new().write(true).create_new(true).open(&path) {
            Ok(file) => return Ok((path, file)),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }

    Err(io::Error::new(
        io::ErrorKind::AlreadyExists,
        "could not create a temporary export file",
    ))
}

/// A workbook under construction.
#[derive(Debug, Default)]
pub struct XlsxWriter {
    sheets: Vec<Sheet>,
}

impl XlsxWriter {
    /// Opens a sheet and fills it from `content`. `columns` holds the columns' display widths;
    /// `freeze_header` freezes row 1; `auto_filter` puts a filter over the header and data range.
    pub fn new() -> Self {
        Self::default()
    }

    /// Adds one worksheet.
    pub fn sheet(
        &mut self,
        name: impl Into<String>,
        columns: Vec<f64>,
        freeze_header: bool,
        auto_filter: bool,
        content: impl FnOnce(&mut Sheet),
    ) {
        let mut sheet = Sheet {
            raw_name: name.into(),
            columns,
            freeze_header,
            auto_filter,
            rows: Vec::new(),
        };
        content(&mut sheet);
        self.sheets.push(sheet);
    }

    /// Writes the workbook to `target` through a temp file next to it, then moves it into place.
    pub fn write(&self, target: &Path) -> io::Result<()> {
        let parts = self.parts();
        write_atomically(target, |temp| {
            let file = File::create(temp)?;
            let mut archive = ZipWriter::new(file);
            for (name, xml) in &parts {
                archive
                    .start_file(name.as_str(), SimpleFileOptions::default())
                    .map_err(io::Error::other)?;
                archive.write_all(xml.as_bytes())?;
            }
            archive.finish().map_err(io::Error::other)?;
            Ok(())
        })
    }

    /// Every package part in write order: the content types, the two relationship parts, the workbook, the
    /// styles and one worksheet per sheet.
    fn parts(&self) -> Vec<(String, String)> {
        let names = self.cleaned_sheet_names();
        let mut parts = Vec::with_capacity(5 + self.sheets.len());
        parts.push(("[Content_Types].xml".to_owned(), content_types_xml(&names)));
        parts.push(("_rels/.rels".to_owned(), ROOT_RELS.to_owned()));
        parts.push(("xl/workbook.xml".to_owned(), workbook_xml(&names)));
        parts.push(("xl/_rels/workbook.xml.rels".to_owned(), workbook_rels_xml(names.len())));
        parts.push(("xl/styles.xml".to_owned(), styles_xml()));
        for (index, sheet) in self.sheets.iter().enumerate() {
            parts.push((format!("xl/worksheets/sheet{}.xml", index + 1), sheet_xml(sheet)));
        }
        parts
    }

    /// The sheets' names cleaned to valid, unique (case-insensitive) Excel sheet names, in order.
    fn cleaned_sheet_names(&self) -> Vec<String> {
        let mut taken = Vec::new();
        self.sheets
            .iter()
            .map(|sheet| clean_sheet_name(&sheet.raw_name, &mut taken))
            .collect()
    }
}

/// One worksheet: its name as given (cleaned when written), its columns' widths and its rows.
#[derive(Debug)]
pub struct Sheet {
    raw_name: String,
    columns: Vec<f64>,
    freeze_header: bool,
    auto_filter: bool,
    rows: Vec<Vec<Cell>>,
}

impl Sheet {
    /// Appends one row; place [`Cell::empty`] where a cell should be left blank.
    pub fn row(&mut self, cells: Vec<Cell>) {
        self.rows.push(cells);
    }
}

/// One cell of a [`Sheet`]; created only through its factory functions.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cell {
    kind: CellKind,
    value: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum CellKind {
    Empty,
    Text,
    Bold,
    Number,
    Money,
    Date,
}

impl Cell {
    /// A plain inline string. Text that looks like a formula stays text.
    pub fn text(value: impl Into<String>) -> Self {
        Self {
            kind: CellKind::Text,
            value: value.into(),
        }
    }

    /// A bold inline string.
    pub fn bold(value: impl Into<String>) -> Self {
        Self {
            kind: CellKind::Bold,
            value: value.into(),
        }
    }

    /// A plain number, written exactly as its text form.
    pub fn number(value: impl ToString) -> Self {
        Self {
            kind: CellKind::Number,
            value: value.to_string(),
        }
    }

    /// A money amount, written from integer minor units with the currency's exponent.
    pub fn money(value: Money) -> Self {
        Self {
            kind: CellKind::Money,
            value: format_plain(value),
        }
    }

    /// A date, written as an Excel serial number (days since 1899-12-30).
    pub fn date(value: Date) -> Self {
        Self {
            kind: CellKind::Date,
            value: excel_serial(value).to_string(),
        }
    }

    /// An empty cell; used to leave a gap in a row.
    pub fn empty() -> Self {
        Self {
            kind: CellKind::Empty,
            value: String::new(),
        }
    }
}

fn excel_serial(value: Date) -> i64 {
    let base = Date::from_calendar_date(1899, Month::December, 30).expect("the Excel base date is valid");
    i64::from(value.to_julian_day()) - i64::from(base.to_julian_day())
}

fn content_types_xml(sheet_names: &[String]) -> String {
    let mut xml = String::from(XML_DECLARATION);
    xml.push_str("<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">");
    xml.push_str(
        "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>",
    );
    xml.push_str("<Default Extension=\"xml\" ContentType=\"application/xml\"/>");
    xml.push_str("<Override PartName=\"/xl/workbook.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml\"/>");
    for index in 0..sheet_names.len() {
        xml.push_str(&format!(
            "<Override PartName=\"/xl/worksheets/sheet{}.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml\"/>",
            index + 1
        ));
    }
    xml.push_str("<Override PartName=\"/xl/styles.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml\"/>");
    xml.push_str("</Types>");
    xml
}

fn workbook_xml(sheet_names: &[String]) -> String {
    let mut xml = String::from(XML_DECLARATION);
    xml.push_str("<workbook xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"");
    xml.push_str(" xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\">");
    xml.push_str("<sheets>");
    for (index, name) in sheet_names.iter().enumerate() {
        xml.push_str(&format!(
            "<sheet name=\"{}\" sheetId=\"{}\" r:id=\"rId{}\"/>",
            escape_xml(name),
            index + 1,
            index + 1
        ));
    }
    xml.push_str("</sheets></workbook>");
    xml
}

fn workbook_rels_xml(sheet_count: usize) -> String {
    let mut xml = String::from(XML_DECLARATION);
    xml.push_str("<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">");
    for index in 0..sheet_count {
        xml.push_str(&format!("<Relationship Id=\"rId{}\"", index + 1));
        xml.push_str(" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\"");
        xml.push_str(&format!(" Target=\"worksheets/sheet{}.xml\"/>", index + 1));
    }
    xml.push_str(&format!("<Relationship Id=\"rId{}\"", sheet_count + 1));
    xml.push_str(" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles\"");
    xml.push_str(" Target=\"styles.xml\"/>");
    xml.push_str("</Relationships>");
    xml
}

fn styles_xml() -> String {
    let mut xml = String::from(XML_DECLARATION);
    xml.push_str(STYLES_BODY);
    xml
}

fn sheet_xml(sheet: &Sheet) -> String {
    // At least column A, so a sheet whose rows are all empty still gets a valid range.
    let last_column = sheet
        .rows
        .iter()
        .map(|row| {
            row.iter()
                .rposition(|cell| cell.kind != CellKind::Empty)
                .map(|index| index + 1)
                .unwrap_or(0)
        })
        .max()
        .unwrap_or(0)
        .max(1);
    let last_row = sheet.rows.len();
    let range = if sheet.rows.is_empty() {
        "A1".to_owned()
    } else {
        format!("A1:{}{last_row}", column_letter(last_column - 1))
    };

    let mut xml = String::from(XML_DECLARATION);
    xml.push_str("<worksheet xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"");
    xml.push_str(" xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\">");
    xml.push_str(&format!("<dimension ref=\"{range}\"/>"));
    xml.push_str("<sheetViews><sheetView workbookViewId=\"0\">");
    if sheet.freeze_header && !sheet.rows.is_empty() {
        xml.push_str("<pane ySplit=\"1\" topLeftCell=\"A2\" activePane=\"bottomLeft\" state=\"frozen\"/>");
    }
    xml.push_str("</sheetView></sheetViews>");
    if !sheet.columns.is_empty() {
        xml.push_str("<cols>");
        for (index, width) in sheet.columns.iter().enumerate() {
            xml.push_str(&format!(
                "<col min=\"{}\" max=\"{}\" width=\"{}\" customWidth=\"1\"/>",
                index + 1,
                index + 1,
                format_width(*width)
            ));
        }
        xml.push_str("</cols>");
    }
    xml.push_str("<sheetData>");
    for (row_index, cells) in sheet.rows.iter().enumerate() {
        xml.push_str(&format!("<row r=\"{}\">", row_index + 1));
        for (column_index, cell) in cells.iter().enumerate() {
            xml.push_str(&cell_xml(cell, row_index + 1, column_index));
        }
        xml.push_str("</row>");
    }
    xml.push_str("</sheetData>");
    if sheet.auto_filter && !sheet.rows.is_empty() {
        xml.push_str(&format!("<autoFilter ref=\"{range}\"/>"));
    }
    xml.push_str("</worksheet>");
    xml
}

fn cell_xml(cell: &Cell, row: usize, column: usize) -> String {
    if cell.kind == CellKind::Empty {
        return String::new();
    }

    let reference = format!("{}{row}", column_letter(column));
    match cell.kind {
        CellKind::Text => inline_str(&reference, 0, &cell.value),
        CellKind::Bold => inline_str(&reference, 1, &cell.value),
        CellKind::Number => format!("<c r=\"{reference}\"><v>{}</v></c>", cell.value),
        CellKind::Money => format!("<c r=\"{reference}\" s=\"2\"><v>{}</v></c>", cell.value),
        CellKind::Date => format!("<c r=\"{reference}\" s=\"3\"><v>{}</v></c>", cell.value),
        CellKind::Empty => String::new(),
    }
}

fn inline_str(reference: &str, style: u8, value: &str) -> String {
    let style_attribute = if style == 0 {
        String::new()
    } else {
        format!(" s=\"{style}\"")
    };
    format!(
        "<c r=\"{reference}\"{style_attribute} t=\"inlineStr\"><is><t xml:space=\"preserve\">{}</t></is></c>",
        escape_xml(value)
    )
}

/// Formats a column width the way Kotlin's `Double.toString` normally does for such values: whole numbers
/// keep a trailing `.0`, other ordinary values use their shortest decimal form.
fn format_width(width: f64) -> String {
    if width.is_finite() && width.fract() == 0.0 && width.abs() < 10_000_000.0 {
        format!("{width:.1}")
    } else {
        format!("{width}")
    }
}

/// Cleans `raw` into a valid sheet name and, taken one, a unique (case-insensitive) variant.
fn clean_sheet_name(raw: &str, taken: &mut Vec<String>) -> String {
    let replaced: String = raw
        .chars()
        .map(|character| {
            if matches!(character, '[' | ']' | ':' | '*' | '?' | '/' | '\\') {
                '_'
            } else {
                character
            }
        })
        .collect();
    let trimmed = replaced.trim();
    let mut base = take_utf16_units(trimmed, MAX_SHEET_NAME_LENGTH);
    if base.is_empty() {
        base = DEFAULT_SHEET_NAME.to_owned();
    }

    if !taken.iter().any(|name| same_name(name, &base)) {
        taken.push(base.clone());
        return base;
    }

    let prefix = take_utf16_units(&base, MAX_SHEET_NAME_LENGTH - UNIQUE_SUFFIX_ROOM);
    let mut number = 2;
    loop {
        let candidate = format!("{prefix}-{number}");
        if !taken.iter().any(|name| same_name(name, &candidate)) {
            taken.push(candidate.clone());
            return candidate;
        }
        number += 1;
    }
}

/// Kotlin's `String.take(n)` counts UTF-16 code units; this keeps whole characters up to that limit.
fn take_utf16_units(value: &str, limit: usize) -> String {
    let mut result = String::new();
    let mut used = 0;
    for character in value.chars() {
        let width = character.len_utf16();
        if used + width > limit {
            break;
        }
        result.push(character);
        used += width;
    }
    result
}

fn same_name(left: &str, right: &str) -> bool {
    left.to_lowercase() == right.to_lowercase()
}

/// Escapes the five XML special characters and drops what XML 1.0 cannot carry: control characters other
/// than tab, LF and CR, lone surrogates and U+FFFE/U+FFFF.
fn escape_xml(value: &str) -> String {
    let mut escaped = String::with_capacity(value.len());
    for character in value.chars() {
        match character {
            '&' => escaped.push_str("&amp;"),
            '<' => escaped.push_str("&lt;"),
            '>' => escaped.push_str("&gt;"),
            '"' => escaped.push_str("&quot;"),
            '\'' => escaped.push_str("&apos;"),
            '\t' | '\n' | '\r' => escaped.push(character),
            character if (character as u32) < 0x20 => {}
            '\u{FFFE}' | '\u{FFFF}' => {}
            character => escaped.push(character),
        }
    }
    escaped
}

/// "A", …, "Z", "AA", … for a 0-based column index.
fn column_letter(index: usize) -> String {
    let mut remaining = index;
    let mut letters = String::new();
    loop {
        let letter = char::from(b'A' + u8::try_from(remaining % 26).expect("a letter offset"));
        letters.insert(0, letter);
        if remaining < 26 {
            break;
        }
        remaining = remaining / 26 - 1;
    }
    letters
}

#[cfg(test)]
mod tests {
    use std::io::{Cursor, Read};

    use roxmltree::{Document, Node};
    use time::macros::date;
    use zip::ZipArchive;

    use super::*;
    use crate::import::xlsx::first_sheet_rows;
    use crate::money::Currency;

    fn eur(minor: i64) -> Money {
        Money {
            minor,
            currency: Currency::EUR,
        }
    }

    fn archive(bytes: &[u8]) -> ZipArchive<Cursor<&[u8]>> {
        ZipArchive::new(Cursor::new(bytes)).expect("the workbook is a zip archive")
    }

    fn entry_names(bytes: &[u8]) -> Vec<String> {
        let archive = archive(bytes);
        (0..archive.len())
            .map(|index| {
                archive
                    .name_for_index(index)
                    .expect("a named zip entry")
                    .expect("the name is UTF-8")
                    .into_owned()
            })
            .collect()
    }

    fn entry(bytes: &[u8], name: &str) -> Vec<u8> {
        let mut archive = archive(bytes);
        let mut entry = archive.by_name(name).expect("the part exists");
        let mut content = Vec::new();
        entry.read_to_end(&mut content).expect("the part is readable");
        content
    }

    fn parse(bytes: &[u8]) -> Document<'_> {
        Document::parse(std::str::from_utf8(bytes).expect("XML is UTF-8")).expect("the part is well-formed XML")
    }

    fn elements<'a, 'input>(node: Node<'a, 'input>) -> Vec<Node<'a, 'input>> {
        node.descendants().filter(|child| child.is_element()).collect()
    }

    fn cell_text(document: &Document<'_>, reference: &str) -> String {
        let cell = elements(document.root_element())
            .into_iter()
            .find(|element| element.tag_name().name() == "c" && element.attribute("r") == Some(reference))
            .expect("the cell exists");
        cell.descendants()
            .find(|child| child.tag_name().name() == "t")
            .map(|text| text.text().unwrap_or("").to_owned())
            .unwrap_or_default()
    }

    #[test]
    fn two_sheets_round_trip_through_the_reader() {
        let directory = tempfile::tempdir().expect("a temp directory");
        let file = directory.path().join("round-trip.xlsx");

        let mut writer = XlsxWriter::new();
        writer.sheet("Data", vec![20.0, 12.0, 12.0, 14.0], false, false, |sheet| {
            sheet.row(vec![
                Cell::bold("Text"),
                Cell::bold("Count"),
                Cell::bold("Money"),
                Cell::bold("Date"),
            ]);
            sheet.row(vec![
                Cell::text("=1+1"),
                Cell::number(42),
                Cell::money(eur(-8_236)),
                Cell::date(date!(2026 - 08 - 03)),
            ]);
            sheet.row(vec![
                Cell::text("A & B <tag> \"q\" 'a'"),
                Cell::number("0.10"),
                Cell::money(eur(123_450)),
                Cell::date(date!(1999 - 12 - 31)),
            ]);
            sheet.row(vec![
                Cell::text("first"),
                Cell::empty(),
                Cell::empty(),
                Cell::text("sparse"),
            ]);
        });
        writer.sheet("Extra", vec![10.0], false, false, |sheet| {
            sheet.row(vec![Cell::text("second sheet")]);
        });
        writer.write(&file).expect("the workbook is written");

        assert!(file.is_file(), "the workbook is at the target");
        let bytes = fs::read(&file).expect("the workbook is readable");
        let rows = first_sheet_rows(&bytes).expect("the reader accepts the workbook");

        assert_eq!(rows[0], vec!["Text", "Count", "Money", "Date"], "the bold header row");
        assert_eq!(rows[1][0], "=1+1", "a text value that looks like a formula stays text");
        assert_eq!(rows[1][1], "42");
        assert_eq!(rows[1][2], "-82.36");
        assert_eq!(rows[1][3], "2026-08-03", "a date cell reads back as an ISO date");
        assert_eq!(rows[2][0], "A & B <tag> \"q\" 'a'", "XML escapes round trip");
        assert_eq!(rows[2][1], "0.1", "the reader's number text drops trailing zeros");
        assert_eq!(rows[2][2], "1234.5");
        assert_eq!(rows[2][3], "1999-12-31");
        assert_eq!(rows[3][0], "first", "gaps become empty strings");
        assert_eq!(rows[3][1], "");
        assert_eq!(rows[3][3], "sparse");
    }

    #[test]
    fn every_part_is_well_formed_xml_with_no_formulas() {
        let directory = tempfile::tempdir().expect("a temp directory");
        let file = directory.path().join("parts.xlsx");

        let mut writer = XlsxWriter::new();
        writer.sheet("Plan", vec![20.0, 20.0], true, true, |sheet| {
            sheet.row(vec![Cell::bold("Head & <More>"), Cell::bold("Second")]);
            sheet.row(vec![Cell::text("=1+1"), Cell::text("a\u{1}b\u{b}c")]);
            sheet.row(vec![
                Cell::text("lone\u{FFFD}surrogate"),
                Cell::text("quote \" and ' plus >"),
            ]);
            sheet.row(vec![Cell::text("row")]);
        });
        writer.sheet("Zweit", vec![10.0], false, false, |sheet| {
            sheet.row(vec![Cell::text("nur Text")]);
        });
        writer.write(&file).expect("the workbook is written");

        let bytes = fs::read(&file).expect("the workbook is readable");
        assert_eq!(
            entry_names(&bytes),
            vec![
                "[Content_Types].xml",
                "_rels/.rels",
                "xl/workbook.xml",
                "xl/_rels/workbook.xml.rels",
                "xl/styles.xml",
                "xl/worksheets/sheet1.xml",
                "xl/worksheets/sheet2.xml",
            ]
        );

        let names = entry_names(&bytes);
        let texts: Vec<(String, String)> = names
            .iter()
            .map(|name| {
                let text = String::from_utf8(entry(&bytes, name)).expect("XML is UTF-8");
                (name.clone(), text)
            })
            .collect();
        let documents: Vec<Document<'_>> = texts
            .iter()
            .map(|(_, text)| Document::parse(text).expect("the part is well-formed XML"))
            .collect();
        for document in &documents {
            assert!(
                elements(document.root_element())
                    .into_iter()
                    .all(|element| element.tag_name().name() != "f"),
                "no formula element in any part"
            );
        }

        let content_types_bytes = entry(&bytes, "[Content_Types].xml");
        let content_types = parse(&content_types_bytes);
        let part_names: Vec<String> = elements(content_types.root_element())
            .into_iter()
            .filter(|element| element.tag_name().name() == "Override")
            .filter_map(|element| element.attribute("PartName").map(str::to_owned))
            .collect();
        assert!(part_names.contains(&"/xl/worksheets/sheet1.xml".to_owned()));
        assert!(part_names.contains(&"/xl/worksheets/sheet2.xml".to_owned()));

        let sheet1_bytes = entry(&bytes, "xl/worksheets/sheet1.xml");
        let sheet1_xml = std::str::from_utf8(&sheet1_bytes).expect("XML is UTF-8");
        assert!(
            sheet1_xml.contains("<t xml:space=\"preserve\">=1+1</t>"),
            "a formula-looking value is an inline string: {sheet1_xml}"
        );
        assert!(!sheet1_xml.contains("'="), "no apostrophe prefix");
        assert!(
            !sheet1_xml
                .chars()
                .any(|character| (character as u32) < 0x20 && !matches!(character, '\t' | '\n' | '\r')),
            "control characters are dropped: {sheet1_xml}"
        );
        assert!(
            sheet1_xml.contains("Head &amp; &lt;More&gt;"),
            "specials are escaped: {sheet1_xml}"
        );

        let sheet1 = parse(&sheet1_bytes);
        assert_eq!(cell_text(&sheet1, "B2"), "abc", "control chars dropped");
        assert_eq!(cell_text(&sheet1, "B3"), "quote \" and ' plus >");
        assert_eq!(
            cell_text(&sheet1, "A3"),
            "lone\u{FFFD}surrogate",
            "replacement characters stay; a lone surrogate cannot exist in Rust"
        );

        let pane = elements(sheet1.root_element())
            .into_iter()
            .find(|element| element.tag_name().name() == "pane")
            .expect("the frozen pane exists");
        assert_eq!(pane.attribute("ySplit"), Some("1"));
        assert_eq!(pane.attribute("topLeftCell"), Some("A2"));
        assert_eq!(pane.attribute("state"), Some("frozen"));
        let filter = elements(sheet1.root_element())
            .into_iter()
            .find(|element| element.tag_name().name() == "autoFilter")
            .expect("the auto filter exists");
        assert_eq!(filter.attribute("ref"), Some("A1:B4"), "header plus data range");
    }

    #[test]
    fn sheet_names_are_cleaned_to_valid_unique_names() {
        let directory = tempfile::tempdir().expect("a temp directory");
        let file = directory.path().join("names.xlsx");

        let mut writer = XlsxWriter::new();
        for name in [
            "Report: Q1/2026 [final] *draft?",
            "Same Name",
            "same name",
            "This name is far too long for an Excel sheet name, it keeps going on and on",
            "   ",
        ] {
            writer.sheet(name, vec![10.0], false, false, |sheet| {
                sheet.row(vec![Cell::text("a")]);
            });
        }
        writer.write(&file).expect("the workbook is written");

        let bytes = fs::read(&file).expect("the workbook is readable");
        let workbook_bytes = entry(&bytes, "xl/workbook.xml");
        let workbook = parse(&workbook_bytes);
        let names: Vec<String> = elements(workbook.root_element())
            .into_iter()
            .filter(|element| element.tag_name().name() == "sheet")
            .filter_map(|element| element.attribute("name").map(str::to_owned))
            .collect();

        assert_eq!(names.len(), 5);
        for name in &names {
            assert!(name.chars().count() <= 31, "'{name}' is at most 31 characters");
            assert!(!name.is_empty(), "'{name}' is not empty");
            assert!(
                !name.chars().any(|character| "[]:*?/\\".contains(character)),
                "'{name}' has no forbidden character"
            );
        }
        let mut lowered = names.iter().map(|name| name.to_lowercase()).collect::<Vec<_>>();
        lowered.sort();
        lowered.dedup();
        assert_eq!(lowered.len(), names.len(), "unique case-insensitively: {names:?}");
        assert_eq!(
            names.last().map(String::as_str),
            Some("Sheet"),
            "a blank name gets the default"
        );
    }

    #[test]
    fn a_failed_write_leaves_no_file_at_the_target_and_no_temp_files_behind() {
        let directory = tempfile::tempdir().expect("a temp directory");

        let mut writer = XlsxWriter::new();
        writer.sheet("Data", vec![10.0], false, false, |sheet| {
            sheet.row(vec![Cell::text("a")]);
        });

        let missing = directory.path().join("missing-dir");
        let missing_target = missing.join("out.xlsx");
        assert!(writer.write(&missing_target).is_err());
        assert!(!missing_target.exists());

        let failing_directory = directory.path().join("failing");
        fs::create_dir(&failing_directory).expect("the test directory exists");
        let failing_target = failing_directory.join("out.xlsx");
        let result = write_atomically(&failing_target, |temp| {
            fs::write(temp, b"partial")?;
            Err(io::Error::other("deliberate failure"))
        });
        assert!(result.is_err());
        assert!(!failing_target.exists(), "nothing at the target");
        assert!(
            fs::read_dir(&failing_directory)
                .expect("the directory is readable")
                .next()
                .is_none(),
            "no temp file is left behind"
        );
    }

    #[cfg(unix)]
    #[test]
    fn the_finished_file_has_mode_0600() {
        use std::os::unix::fs::PermissionsExt;

        let directory = tempfile::tempdir().expect("a temp directory");
        let target = directory.path().join("mode.xlsx");
        write_atomically(&target, |temp| fs::write(temp, b"content")).expect("the file is written");
        let mode = fs::metadata(&target).expect("the file exists").permissions().mode();
        assert_eq!(mode & 0o777, 0o600);
    }

    #[test]
    fn a_sheet_whose_rows_are_all_empty_still_gets_a_valid_range() {
        let mut writer = XlsxWriter::new();
        writer.sheet("Empty", vec![10.0], true, true, |sheet| {
            sheet.row(vec![Cell::empty()]);
        });
        let sheet = writer
            .parts()
            .into_iter()
            .find(|(name, _)| name == "xl/worksheets/sheet1.xml")
            .map(|(_, xml)| xml)
            .expect("the worksheet part exists");
        assert!(sheet.contains("<dimension ref=\"A1:A1\"/>"), "{sheet}");
    }
}
