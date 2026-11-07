//! Ported parser-level tests from FinDesk's `XlsxReaderTest.kt`.
//!
//! No tests from this file are skipped.

#[path = "import_support.rs"]
mod support;

use finglass_core::import::ImportError;
use finglass_core::import::xlsx::first_sheet_rows;
use support::{stored_entry, write_zip};

const WORKBOOK: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:workbook xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <x:workbookPr/>
  <x:sheets><x:sheet name="Sheet1" sheetId="1" r:id="rId1"/></x:sheets>
</x:workbook>"#;

const RELS: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>"#;

const DATE_STYLES: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:styleSheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:numFmts count="1"><x:numFmt numFmtId="14" formatCode="M/d/yyyy"/></x:numFmts>
  <x:cellXfs count="3">
    <x:xf numFmtId="0"/>
    <x:xf numFmtId="14"/>
    <x:xf numFmtId="2" applyNumberFormat="1"/>
  </x:cellXfs>
</x:styleSheet>"#;

fn xlsx(sheet: &str, workbook: &str, rels: &str, shared: Option<&str>, styles: Option<&str>) -> Vec<u8> {
    let mut entries: Vec<(&str, &[u8])> = vec![
        ("xl/workbook.xml", workbook.as_bytes()),
        ("xl/_rels/workbook.xml.rels", rels.as_bytes()),
    ];
    if let Some(shared) = shared {
        entries.push(("xl/sharedStrings.xml", shared.as_bytes()));
    }
    if let Some(styles) = styles {
        entries.push(("xl/styles.xml", styles.as_bytes()));
    }
    entries.push(("xl/worksheets/sheet1.xml", sheet.as_bytes()));
    write_zip(&entries)
}

fn default_xlsx(sheet: &str) -> Vec<u8> {
    xlsx(sheet, WORKBOOK, RELS, None, None)
}

fn format_amount(value: &str) -> String {
    let mut text = value.to_owned();
    if let Some((integer, fraction)) = text.clone().split_once('.') {
        let mut fraction = fraction.to_owned();
        while fraction.len() < 2 {
            fraction.push('0');
        }
        text = format!("{integer}.{}", &fraction[..2]);
    } else {
        text.push_str(".00");
    }
    text
}

fn assert_too_large(result: Result<Vec<Vec<String>>, ImportError>) {
    assert_eq!(Err(ImportError::XlsxTooLarge), result);
}

fn assert_unreadable(result: Result<Vec<Vec<String>>, ImportError>) {
    assert_eq!(Err(ImportError::XlsxUnreadable), result);
}

#[test]
fn sparse_cells_at_a_and_ab_and_a_formula_cell_with_cached_value() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="inlineStr"><x:is><x:t>head</x:t></x:is></x:c>
      <x:c r="AB1" t="inlineStr"><x:is><x:t>wide</x:t></x:is></x:c>
    </x:row>
    <x:row r="2"><x:c r="A2"><x:f>1+1</x:f><x:v>2</x:v></x:c></x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&default_xlsx(sheet)).unwrap();

    assert_eq!(2, rows.len());
    assert_eq!("head", rows[0][0]);
    assert_eq!("wide", rows[0][27]);
    assert_eq!(28, rows[0].len());
    assert_eq!(vec!["2"], rows[1]);
}

#[test]
fn numbers_come_out_as_plain_dot_decimals_and_parse_amounts_reads_them() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1"><x:v>1.5</x:v></x:c>
      <x:c r="B1"><x:v>0.1</x:v></x:c>
      <x:c r="C1"><x:v>1234.5</x:v></x:c>
      <x:c r="D1"><x:v>-82.36</x:v></x:c>
      <x:c r="E1"><x:v>1000</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&default_xlsx(sheet)).unwrap();

    assert_eq!(vec!["1.5", "0.1", "1234.5", "-82.36", "1000"], rows[0]);
    assert_eq!(
        vec!["1.50", "0.10", "1234.50", "-82.36", "1000.00"],
        rows[0].iter().map(|value| format_amount(value)).collect::<Vec<_>>()
    );
}

#[test]
fn date_styles_render_iso_dates_and_the_1904_epoch_shifts_the_base() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" s="1"><x:v>46237</x:v></x:c>
      <x:c r="B1" s="1"><x:v>46239.75</x:v></x:c>
      <x:c r="C1" s="2"><x:v>45.67</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&xlsx(sheet, WORKBOOK, RELS, None, Some(DATE_STYLES))).unwrap();
    assert_eq!("2026-08-03", rows[0][0]);
    assert_eq!("2026-08-05", rows[0][1]);
    assert_eq!("45.67", rows[0][2]);

    let workbook_1904 = WORKBOOK.replace("<x:workbookPr/>", r#"<x:workbookPr date1904="true"/>"#);
    let sheet_1904 = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1" s="1"><x:v>0</x:v></x:c></x:row></x:sheetData>
</x:worksheet>"#;
    let rows_1904 = first_sheet_rows(&xlsx(sheet_1904, &workbook_1904, RELS, None, Some(DATE_STYLES))).unwrap();
    assert_eq!("1904-01-01", rows_1904[0][0]);
}

#[test]
fn a_capped_numeric_cell_stays_its_raw_v_text_and_an_out_of_range_date_serial_is_empty() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1"><x:v>1E999999999</x:v></x:c>
      <x:c r="B1" s="1"><x:v>99999999</x:v></x:c>
      <x:c r="C1" s="1"><x:v>46237</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&xlsx(sheet, WORKBOOK, RELS, None, Some(DATE_STYLES))).unwrap();

    assert_eq!("1E999999999", rows[0][0]);
    assert_eq!("", rows[0][1]);
    assert_eq!("2026-08-03", rows[0][2]);
}

#[test]
fn an_out_of_range_shared_string_index_only_empties_that_cell() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="s"><x:v>0</x:v></x:c>
      <x:c r="B1" t="s"><x:v>99</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>"#;
    let strings = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="1">
  <x:si><x:t>ok</x:t></x:si>
</x:sst>"#;
    let rows = first_sheet_rows(&xlsx(sheet, WORKBOOK, RELS, Some(strings), None)).unwrap();
    assert_eq!(vec!["ok", ""], rows[0]);
}

#[test]
fn more_than_2000_entries_is_refused() {
    let entries: Vec<(String, Vec<u8>)> = (0..2001)
        .map(|index| (format!("part/{index}.xml"), b"x".to_vec()))
        .collect();
    let refs: Vec<(&str, &[u8])> = entries
        .iter()
        .map(|(name, content)| (name.as_str(), content.as_slice()))
        .collect();
    assert_too_large(first_sheet_rows(&write_zip(&refs)));
}

#[test]
fn a_61_mb_entry_that_is_not_needed_is_refused() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>"#;
    let padding = vec![0u8; 1024 * 1024];
    // The padding entry alone is 1 MB; repeat it 61 times by writing the same content under one entry.
    let mut writer = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
    use std::io::Write;
    writer
        .start_file("xl/padding.bin", zip::write::SimpleFileOptions::default())
        .unwrap();
    for _ in 0..61 {
        writer.write_all(&padding).unwrap();
    }
    writer
        .start_file("xl/workbook.xml", zip::write::SimpleFileOptions::default())
        .unwrap();
    writer.write_all(WORKBOOK.as_bytes()).unwrap();
    writer
        .start_file("xl/_rels/workbook.xml.rels", zip::write::SimpleFileOptions::default())
        .unwrap();
    writer.write_all(RELS.as_bytes()).unwrap();
    writer
        .start_file("xl/worksheets/sheet1.xml", zip::write::SimpleFileOptions::default())
        .unwrap();
    writer.write_all(sheet.as_bytes()).unwrap();
    let bytes = writer.finish().unwrap().into_inner();
    assert_too_large(first_sheet_rows(&bytes));
}

#[test]
fn a_missing_workbook_is_unreadable() {
    let bytes = write_zip(&[("xl/worksheets/sheet1.xml", b"<worksheet/>".as_slice())]);
    assert_unreadable(first_sheet_rows(&bytes));
}

#[test]
fn a_doctype_in_the_sheet_is_unreadable() {
    let sheet = r#"<?xml version="1.0"?>
<!DOCTYPE worksheet>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>"#;
    assert_unreadable(first_sheet_rows(&default_xlsx(sheet)));
}

#[test]
fn malformed_sheet_xml_is_unreadable() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"<x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>"#;
    assert_unreadable(first_sheet_rows(&default_xlsx(sheet)));
}

const ONE_CELL_SHEET: &str = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>
</x:worksheet>"#;

#[test]
fn sparse_cells_with_real_cell_references_land_in_their_own_columns() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="3">
      <x:c r="A3" t="inlineStr"><x:is><x:t>x</x:t></x:is></x:c>
      <x:c r="C3" t="inlineStr"><x:is><x:t>z</x:t></x:is></x:c>
    </x:row>
    <x:row r="5"><x:c r="AB5"><x:v>7</x:v></x:c></x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&default_xlsx(sheet)).unwrap();

    assert_eq!(vec!["x", "", "z"], rows[2]);
    assert_eq!(28, rows[4].len());
    assert_eq!("7", rows[4][27]);
}

#[test]
fn an_omitted_middle_cell_keeps_later_values_in_their_columns() {
    let sheet = r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">
      <x:c r="A1" t="inlineStr"><x:is><x:t>Date</x:t></x:is></x:c>
      <x:c r="B1" t="inlineStr"><x:is><x:t>Payee</x:t></x:is></x:c>
      <x:c r="C1" t="inlineStr"><x:is><x:t>Amount</x:t></x:is></x:c>
    </x:row>
    <x:row r="2">
      <x:c r="A2" t="inlineStr"><x:is><x:t>2026-08-03</x:t></x:is></x:c>
      <x:c r="C2"><x:v>-12.5</x:v></x:c>
    </x:row>
  </x:sheetData>
</x:worksheet>"#;
    let rows = first_sheet_rows(&default_xlsx(sheet)).unwrap();

    assert_eq!(vec!["Date", "Payee", "Amount"], rows[0]);
    assert_eq!(vec!["2026-08-03", "", "-12.5"], rows[1]);
}

#[test]
fn two_entries_with_the_same_wanted_sheet_name_are_unreadable() {
    let bytes = default_xlsx(ONE_CELL_SHEET);
    let central_dir = [0x50u8, 0x4B, 0x01, 0x02];
    let insert_at = bytes
        .windows(central_dir.len())
        .position(|window| window == central_dir)
        .unwrap();
    let mut spliced = bytes[..insert_at].to_vec();
    spliced.extend_from_slice(&stored_entry("xl/worksheets/sheet1.xml", ONE_CELL_SHEET));
    spliced.extend_from_slice(&bytes[insert_at..]);
    assert_unreadable(first_sheet_rows(&spliced));
}

#[test]
fn random_bytes_are_unreadable() {
    let bytes: Vec<u8> = (0..512).map(|index| (index * 31 + 7) as u8).collect();
    assert_unreadable(first_sheet_rows(&bytes));
}

#[test]
fn a_truncated_zip_is_unreadable() {
    let bytes = default_xlsx(ONE_CELL_SHEET);
    assert_unreadable(first_sheet_rows(&bytes[..bytes.len() / 2]));
}

#[test]
fn a_sheet_nested_100_levels_deep_is_unreadable() {
    let mut sheet = String::from(r#"<?xml version="1.0"?>"#);
    sheet.push_str(r#"<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">"#);
    for _ in 0..100 {
        sheet.push_str("<x:n>");
    }
    sheet.push_str(r#"<x:sheetData><x:row r="1"><x:c r="A1"><x:v>1</x:v></x:c></x:row></x:sheetData>"#);
    for _ in 0..100 {
        sheet.push_str("</x:n>");
    }
    sheet.push_str("</x:worksheet>");
    assert_unreadable(first_sheet_rows(&default_xlsx(&sheet)));
}
