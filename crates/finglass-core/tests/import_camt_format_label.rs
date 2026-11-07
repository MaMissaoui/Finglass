//! Ported parser-level tests from FinDesk's `CamtFormatLabelTest.kt`.
//!
//! Skipped tests (need the database/import service and audit log):
//! - `importing a camt-052 report writes the message type into the audit log`

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{StatementFormat, camt, csv};
use support::write_zip;

const IBAN: &str = "DE89370400440532013000";

fn camt_document(namespace: &str, message: &str) -> String {
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n{}\n{message}\n</Document>",
        if namespace.is_empty() {
            "<Document>".to_owned()
        } else {
            format!("<Document xmlns=\"{namespace}\">")
        }
    )
}

fn rpt(iban: &str, entries: &str) -> String {
    format!("<BkToCstmrAcctRpt><Rpt>\n<Acct><Id><IBAN>{iban}</IBAN></Id></Acct>\n{entries}\n</Rpt></BkToCstmrAcctRpt>")
}

fn stmt(iban: &str, entries: &str) -> String {
    format!("<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>{iban}</IBAN></Id></Acct>\n{entries}\n</Stmt></BkToCstmrStmt>")
}

fn entry(amount: &str, date: &str, reference: &str) -> String {
    format!(
        "<Ntry>\n<Amt Ccy=\"EUR\">{amount}</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\n\
<BookgDt><Dt>{date}</Dt></BookgDt><ValDt><Dt>{date}</Dt></ValDt>\n\
<AcctSvcrRef>{reference}</AcctSvcrRef>\n\
<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Partner</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>\n</Ntry>"
    )
}

#[test]
fn a_camt_052_account_report_is_labelled_camt_052_while_the_format_stays_camt053() {
    let xml = camt_document(
        "urn:iso:std:iso:20022:tech:xsd:camt.052.001.08",
        &rpt(IBAN, &entry("10.00", "2026-10-01", "A1")),
    );
    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(StatementFormat::Camt053, s.format);
    assert_eq!(Some("CAMT.052"), s.format_detail.as_deref());
    assert_eq!("CAMT.052 XML", s.format_label());
    assert_eq!(1, s.bookings.len());
}

#[test]
fn a_camt_053_statement_is_labelled_camt_053() {
    let xml = camt_document(
        "urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
        &stmt(IBAN, &entry("5.00", "2026-10-03", "A2")),
    );
    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(Some("CAMT.053"), s.format_detail.as_deref());
    assert_eq!("CAMT.053 XML", s.format_label());
}

#[test]
fn without_a_namespace_the_message_type_comes_from_the_roots_first_child() {
    let no_namespace = camt_document("", &rpt(IBAN, &entry("10.00", "2026-10-01", "A3")));
    assert_eq!(
        Some("CAMT.052"),
        camt::parse(no_namespace.as_bytes()).unwrap().format_detail.as_deref()
    );

    let stmt54 = camt_document(
        "",
        &stmt(IBAN, &entry("10.00", "2026-10-01", "A4")).replace("BkToCstmrStmt", "BkToCstmrDbtCdtNtfctn"),
    );
    assert_eq!(
        Some("CAMT.054"),
        camt::parse(stmt54.as_bytes()).unwrap().format_detail.as_deref()
    );
}

#[test]
fn a_zip_of_a_camt_052_and_a_camt_053_file_names_both_types_in_file_order() {
    let report = camt_document(
        "urn:iso:std:iso:20022:tech:xsd:camt.052.001.08",
        &rpt(IBAN, &entry("10.00", "2026-10-01", "B1")),
    );
    let statement = camt_document(
        "urn:iso:std:iso:20022:tech:xsd:camt.053.001.02",
        &stmt(IBAN, &entry("20.00", "2026-10-02", "B2")),
    );
    let zip = write_zip(&[
        ("report.xml", report.as_bytes()),
        ("statement.xml", statement.as_bytes()),
    ]);

    let s = camt::parse(&zip).unwrap();

    assert_eq!(vec![IBAN.to_owned()], s.accounts);
    assert_eq!(2, s.bookings.len());
    assert_eq!(Some("CAMT.052/CAMT.053"), s.format_detail.as_deref());
    assert_eq!("CAMT.052/CAMT.053 XML", s.format_label());
}

#[test]
fn csv_statements_keep_the_plain_csv_label() {
    let s = csv::parse(b"Buchungstag;Betrag;Verwendungszweck\n03.10.2026;-1,00;Kaffee\n", None).unwrap();

    assert_eq!(StatementFormat::Csv, s.format);
    assert_eq!(None, s.format_detail);
    assert_eq!("CSV", s.format_label());
}
