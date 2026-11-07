//! Ported parser-level tests from FinDesk's `CardStatementImportTest.kt`.
//!
//! Skipped tests (need the database/import service and audit log):
//! - `reading and importing a card xlsx file`

#[path = "import_support.rs"]
mod support;

use finglass_core::import::xlsx::first_sheet_rows;
use finglass_core::import::{StatementFormat, csv};
use support::{cp1252, dec, write_zip};
use time::macros::date;

const HVB_CSV: &str = "Kartennummer;Zeitraum;Belegdatum;Eingangstag;Text/Verwendungszweck;Kurs;Betrag;Waehrung
5412345678901234;01.05.2025 - 15.05.2025;03.05.2025;06.05.2025;REWE SAGT DANKE Ü;kurs;-58,94;EUR
5412345678901234;01.05.2025 - 15.05.2025;04.05.2025;07.05.2025;Tankstelle am Ring;kurs;-12,50;EUR
5412345678901234;01.05.2025 - 15.05.2025;05.05.2025;08.05.2025;Rückzahlung Versicherung;kurs;123,45;EUR";

#[test]
fn hvb_card_csv_in_utf_16le_without_bom() {
    let s = csv::parse(&support::utf16le(HVB_CSV), None).unwrap();

    assert_eq!("UTF-16LE", s.csv.as_ref().unwrap().charset_name);
    assert_eq!(';', s.csv.as_ref().unwrap().delimiter);
    assert_eq!(3, s.bookings.len());
    let dates: Vec<_> = s.bookings.iter().map(|b| b.date).collect();
    assert_eq!(
        vec![date!(2025 - 05 - 06), date!(2025 - 05 - 07), date!(2025 - 05 - 08)],
        dates
    );
    let value_dates: Vec<_> = s.bookings.iter().filter_map(|b| b.value_date).collect();
    assert_eq!(
        vec![date!(2025 - 05 - 03), date!(2025 - 05 - 04), date!(2025 - 05 - 05)],
        value_dates
    );
    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(vec![dec("-58.94"), dec("-12.50"), dec("123.45")], amounts);
    assert_eq!(Some("REWE SAGT DANKE Ü"), s.bookings[0].purpose.as_deref());
    assert_eq!(Some("EUR"), s.bookings[0].currency.as_deref());
    assert_eq!(None, s.bookings[0].partner_name);
}

#[test]
fn hvb_card_csv_in_utf_16le_with_bom() {
    let mut bytes = vec![0xFF, 0xFE];
    bytes.extend_from_slice(&support::utf16le(HVB_CSV));
    let s = csv::parse(&bytes, None).unwrap();

    assert_eq!("UTF-16LE", s.csv.as_ref().unwrap().charset_name);
    assert_eq!(3, s.bookings.len());
    assert_eq!(Some("REWE SAGT DANKE Ü"), s.bookings[0].purpose.as_deref());
}

#[test]
fn hvb_card_csv_in_utf_16be_without_bom() {
    let s = csv::parse(&support::utf16be(HVB_CSV), None).unwrap();

    assert_eq!("UTF-16BE", s.csv.as_ref().unwrap().charset_name);
    assert_eq!(3, s.bookings.len());
    assert_eq!(Some("Rückzahlung Versicherung"), s.bookings[2].purpose.as_deref());
}

#[test]
fn kartennummer_maps_to_nothing() {
    let headers: Vec<String> = HVB_CSV.lines().next().unwrap().split(';').map(str::to_owned).collect();

    let mapping = csv::detect_mapping(&headers);

    assert_eq!(Some(3), mapping.date);
    assert_eq!(Some(2), mapping.value_date);
    assert_eq!(Some(6), mapping.amount);
    assert_eq!(Some(4), mapping.purpose);
    assert_eq!(Some(7), mapping.currency);
    assert_eq!(None, mapping.partner);
    assert_eq!(None, mapping.payer);
    assert_eq!(None, mapping.partner_iban);
    assert_eq!(None, mapping.balance);
}

#[test]
fn existing_german_csv_format_still_parses_its_grouped_amounts() {
    let utf8 = csv::parse(
        "Buchungstag;Betrag;Verwendungszweck\n03.10.2026;1.234,56;Einkauf\n".as_bytes(),
        None,
    )
    .unwrap();
    let windows = csv::parse(
        &cp1252("Buchungstag;Betrag;Zahlungsempfänger;Verwendungszweck\n03.10.2026;1.234,56;Räderwerk;Rad kaufen\n"),
        None,
    )
    .unwrap();

    assert_eq!("UTF-8", utf8.csv.as_ref().unwrap().charset_name);
    assert_eq!(dec("1234.56"), utf8.bookings[0].amount);
    assert_eq!("windows-1252", windows.csv.as_ref().unwrap().charset_name);
    assert_eq!(dec("1234.56"), windows.bookings[0].amount);
    assert_eq!(Some("Räderwerk"), windows.bookings[0].partner_name.as_deref());
}

// -- Bank Norwegian XLSX -------------------------------------------------------------------------

const WORKBOOK: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:workbook xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <x:workbookPr/>
  <x:sheets><x:sheet name="Transactions" sheetId="1" r:id="rId1"/></x:sheets>
</x:workbook>"#;

const RELS: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>"#;

const STYLES: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:styleSheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:numFmts count="1"><x:numFmt numFmtId="14" formatCode="M/d/yyyy"/></x:numFmts>
  <x:cellXfs count="2"><x:xf numFmtId="0"/><x:xf numFmtId="14"/></x:cellXfs>
</x:styleSheet>"#;

const SHARED_STRINGS: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<x:sst xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="16">
  <x:si><x:t>TransactionDate</x:t></x:si>
  <x:si><x:t>Text</x:t></x:si>
  <x:si><x:t>Type</x:t></x:si>
  <x:si><x:t>Currency Amount</x:t></x:si>
  <x:si><x:t>Currency Rate</x:t></x:si>
  <x:si><x:t>Currency</x:t></x:si>
  <x:si><x:t>Amount</x:t></x:si>
  <x:si><x:t>Merchant Area</x:t></x:si>
  <x:si><x:t>Merchant Category</x:t></x:si>
  <x:si><x:t>BookDate</x:t></x:si>
  <x:si><x:t>ValueDate</x:t></x:si>
  <x:si><x:t>Simpl Jewellery</x:t></x:si>
  <x:si><x:t>Purchase</x:t></x:si>
  <x:si><x:t>Shopping</x:t></x:si>
  <x:si><x:t>Jewellery Specialist</x:t></x:si>
  <x:si><x:t>Currency</x:t></x:si>
</x:sst>"#;

fn xlsx(sheet: &str, workbook: &str, shared_strings: Option<&str>) -> Vec<u8> {
    let mut entries: Vec<(&str, &[u8])> = vec![
        ("xl/workbook.xml", workbook.as_bytes()),
        ("xl/_rels/workbook.xml.rels", RELS.as_bytes()),
    ];
    if let Some(shared) = shared_strings {
        entries.push(("xl/sharedStrings.xml", shared.as_bytes()));
    }
    entries.push(("xl/styles.xml", STYLES.as_bytes()));
    entries.push(("xl/worksheets/sheet1.xml", sheet.as_bytes()));
    write_zip(&entries)
}

fn col(index: usize) -> char {
    char::from(b'A' + index as u8)
}

fn norwegian_header() -> String {
    (0..11)
        .map(|index| format!(r#"<x:c r="{}1" t="s"><x:v>{index}</x:v></x:c>"#, col(index)))
        .collect()
}

fn norwegian_row(amount: &str) -> String {
    format!(
        r#"<x:c r="A2" s="1"><x:v>46237</x:v></x:c>\
<x:c r="B2" t="s"><x:v>11</x:v></x:c><x:c r="C2" t="s"><x:v>12</x:v></x:c>\
<x:c r="D2"><x:v>-10.00</x:v></x:c><x:c r="E2"><x:v>1.08</x:v></x:c><x:c r="F2" t="s"><x:v>5</x:v></x:c>\
<x:c r="G2"><x:v>{amount}</x:v></x:c><x:c r="H2" t="s"><x:v>13</x:v></x:c><x:c r="I2" t="s"><x:v>14</x:v></x:c>\
<x:c r="J2" s="1"><x:v>46238</x:v></x:c><x:c r="K2" s="1"><x:v>46239</x:v></x:c>"#
    )
}

fn norwegian_sheet(row: &str) -> String {
    format!(
        r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1">{}</x:row>
    <x:row r="2">{row}</x:row>
  </x:sheetData>
</x:worksheet>"#,
        norwegian_header()
    )
}

fn parse_rows(bytes: &[u8]) -> finglass_core::import::ParsedStatement {
    csv::parse_rows(
        &first_sheet_rows(bytes).unwrap(),
        None,
        StatementFormat::Xlsx,
        "XLSX",
        ';',
        date!(2026 - 11 - 01),
    )
}

#[test]
fn bank_norwegian_xlsx_with_prefixed_namespace_shared_strings_and_a_re_declared_date_style() {
    let s = parse_rows(&xlsx(
        &norwegian_sheet(&norwegian_row("-82.36")),
        WORKBOOK,
        Some(SHARED_STRINGS),
    ));

    assert_eq!(StatementFormat::Xlsx, s.format);
    assert_eq!("XLSX", s.csv.as_ref().unwrap().charset_name);
    assert_eq!(1, s.bookings.len());
    let booking = &s.bookings[0];
    assert_eq!(date!(2026 - 08 - 04), booking.date);
    assert_eq!(Some(date!(2026 - 08 - 05)), booking.value_date);
    assert_eq!(dec("-82.36"), booking.amount);
    assert_eq!(Some("Simpl Jewellery"), booking.purpose.as_deref());
    assert_eq!(None, booking.currency);
}

#[test]
fn bank_norwegian_xlsx_with_default_namespace_and_inline_strings() {
    let ns = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
    let names = [
        "TransactionDate",
        "Text",
        "Type",
        "Currency Amount",
        "Currency Rate",
        "Currency",
        "Amount",
        "Merchant Area",
        "Merchant Category",
        "BookDate",
        "ValueDate",
    ];
    let header: String = names
        .iter()
        .enumerate()
        .map(|(index, text)| format!(r#"<c r="{}1" t="inlineStr"><is><t>{text}</t></is></c>"#, col(index)))
        .collect();
    let sheet = format!(
        r#"<?xml version="1.0"?>
<worksheet xmlns="{ns}">
  <sheetData>
    <row r="1">{header}</row>
    <row r="2"><c r="A2" s="1"><v>46237</v></c><c r="B2" t="inlineStr"><is><t>Simpl Jewellery</t></is></c><c r="C2" t="inlineStr"><is><t>Purchase</t></is></c>\
<c r="D2"><v>-10.00</v></c><c r="E2"><v>1.08</v></c><c r="F2" t="inlineStr"><is><t>EUR</t></is></c><c r="G2"><v>-82.36</v></c>\
<c r="H2" t="inlineStr"><is><t>Shopping</t></is></c><c r="I2" t="inlineStr"><is><t>Jewellery Specialist</t></is></c>\
<c r="J2" s="1"><v>46238</v></c><c r="K2" s="1"><v>46239</v></c></row>
  </sheetData>
</worksheet>"#
    );
    let s = parse_rows(&xlsx(&sheet, WORKBOOK, None));

    assert_eq!(1, s.bookings.len());
    let booking = &s.bookings[0];
    assert_eq!(date!(2026 - 08 - 04), booking.date);
    assert_eq!(Some(date!(2026 - 08 - 05)), booking.value_date);
    assert_eq!(dec("-82.36"), booking.amount);
    assert_eq!(Some("Simpl Jewellery"), booking.purpose.as_deref());
}

#[test]
fn a_usd_purchase_with_its_own_currency_amount_imports_the_billed_eur_amount() {
    let s = parse_rows(&xlsx(
        &norwegian_sheet(&norwegian_row("-9.12")),
        WORKBOOK,
        Some(SHARED_STRINGS),
    ));

    assert_eq!(1, s.bookings.len(), "not skipped as another currency");
    assert_eq!(dec("-9.12"), s.bookings[0].amount);
    assert_eq!(None, s.bookings[0].currency);
}

#[test]
fn numeric_amounts_from_xlsx_cells() {
    let numbers = ["1.5", "0.1", "1234.5", "-82.36", "1000"];
    let rows: String = numbers
        .iter()
        .enumerate()
        .map(|(index, number)| {
            let row = index + 2;
            format!(
                r#"<x:row r="{row}"><x:c r="A{row}" t="inlineStr"><x:is><x:t>2026-08-04</x:t></x:is></x:c>\
<x:c r="B{row}"><x:v>{number}</x:v></x:c>\
<x:c r="C{row}" t="inlineStr"><x:is><x:t>Sinus Power</x:t></x:is></x:c></x:row>"#
            )
        })
        .collect::<Vec<_>>()
        .join("\n    ");
    let sheet = format!(
        r#"<?xml version="1.0"?>
<x:worksheet xmlns:x="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <x:sheetData>
    <x:row r="1"><x:c r="A1" t="inlineStr"><x:is><x:t>Date</x:t></x:is></x:c><x:c r="B1" t="inlineStr"><x:is><x:t>Amount</x:t></x:is></x:c><x:c r="C1" t="inlineStr"><x:is><x:t>Purpose</x:t></x:is></x:c></x:row>
    {rows}
  </x:sheetData>
</x:worksheet>"#
    );
    let s = parse_rows(&xlsx(&sheet, WORKBOOK, Some(SHARED_STRINGS)));

    let amounts: Vec<String> = s.bookings.iter().map(|b| b.amount.to_string()).collect();
    assert_eq!(vec!["1.50", "0.10", "1234.50", "-82.36", "1000.00"], amounts);
}
