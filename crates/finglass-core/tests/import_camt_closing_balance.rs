//! Ported parser-level tests from FinDesk's `CamtClosingBalanceTest.kt`.
//!
//! Skipped tests (need the database/import service):
//! - `the account balance is set from the closing balance` (database, balance update)
//! - `without updateBalance the account balance is untouched` (database, balance update)
//! - `a booking newer than the closing date blocks the balance update` (database, balance guard)
//! - `a closing currency differing from the account currency leaves the balance untouched` (database)
//! - `a csv with running balances still sets the balance from the newest booking` (database, balance update)
//! - `the closing balance is allowed when it is at least as recent as the account's newest booking` (database)

#[path = "import_support.rs"]
mod support;

use finglass_core::import::camt;
use support::{dec, write_zip};
use time::macros::date;

const IBAN: &str = "DE89370400440532013000";

fn balance(code: &str, amount: &str, date: &str, ccy: &str, sign: &str) -> String {
    format!(
        "<Bal><Tp><CdOrPrtry><Cd>{code}</Cd></CdOrPrtry></Tp>\
<Amt Ccy=\"{ccy}\">{amount}</Amt>\
<CdtDbtInd>{sign}</CdtDbtInd>\
{}</Bal>",
        if date.is_empty() {
            String::new()
        } else {
            format!("<Dt><Dt>{date}</Dt></Dt>")
        }
    )
}

fn entry(amount: &str, sign: &str, status: &str, date: &str, reference: &str) -> String {
    format!(
        "<Ntry>\
<Amt Ccy=\"EUR\">{amount}</Amt><CdtDbtInd>{sign}</CdtDbtInd>\
{}\
<BookgDt><Dt>{date}</Dt></BookgDt><ValDt><Dt>{date}</Dt></ValDt>\
<AcctSvcrRef>{reference}</AcctSvcrRef>\
<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Café</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>\
</Ntry>",
        if status.is_empty() {
            String::new()
        } else {
            format!("<Sts>{status}</Sts>")
        }
    )
}

fn debited_entry(amount: &str, date: &str, reference: &str) -> String {
    format!(
        "<Ntry>\
<Amt Ccy=\"EUR\">{amount}</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\
<BookgDt><Dt>{date}</Dt></BookgDt><ValDt><Dt>{date}</Dt></ValDt>\
<AcctSvcrRef>{reference}</AcctSvcrRef>\
<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Café</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>\
</Ntry>"
    )
}

fn camt(iban: &str, parts: &[String]) -> String {
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n\
<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>{iban}</IBAN></Id></Acct>\n{}\n\
</Stmt></BkToCstmrStmt>\n</Document>",
        parts.join("\n")
    )
}

fn zip(xmls: &[String]) -> Vec<u8> {
    let entries: Vec<(String, Vec<u8>)> = xmls
        .iter()
        .enumerate()
        .map(|(index, xml)| (format!("statement{index}.xml"), xml.as_bytes().to_vec()))
        .collect();
    let refs: Vec<(&str, &[u8])> = entries
        .iter()
        .map(|(name, content)| (name.as_str(), content.as_slice()))
        .collect();
    write_zip(&refs)
}

#[test]
fn a_camt_entry_with_an_out_of_range_amount_is_skipped_instead_of_imported() {
    let xml = camt(IBAN, &[debited_entry("1E999999999", "2026-02-05", "R1")]);

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert!(s.bookings.is_empty());
    assert_eq!(vec!["Entry 1: unreadable amount"], s.skipped);
    assert_eq!(
        None, s.closing_balance,
        "a skipped entry makes the balance check unreliable"
    );
}

#[test]
fn opening_plus_booked_entries_matches_the_closing_balance() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
            entry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(2, s.bookings.len());
    let closing = s.closing_balance.unwrap();
    assert_eq!(dec("80.00"), closing.amount);
    assert_eq!(0, closing.pending_excluded);
    assert_eq!(Some("EUR"), closing.currency.as_deref());
    assert_eq!(date!(2026 - 02 - 28), closing.date);
}

#[test]
fn a_pending_entry_folded_into_the_closing_balance_is_taken_back_out() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "60.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
            entry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
            entry("20.00", "DBIT", "PDNG", "", "R3"),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(2, s.bookings.len(), "the pending entry is not a booking");
    let closing = s.closing_balance.unwrap();
    assert_eq!(dec("80.00"), closing.amount);
    assert_eq!(1, closing.pending_excluded);
}

#[test]
fn a_closing_balance_matching_neither_sum_is_rejected() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "55.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
            entry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
            entry("20.00", "DBIT", "PDNG", "", "R3"),
        ],
    );

    assert_eq!(None, camt::parse(xml.as_bytes()).unwrap().closing_balance);
}

#[test]
fn debit_balances_are_negated() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "50.00", "2026-02-28", "EUR", "DBIT"),
            balance("CLBD", "75.00", "2026-02-28", "EUR", "DBIT"),
            debited_entry("25.00", "2026-02-05", "R1"),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    let closing = s.closing_balance.unwrap();
    assert_eq!(dec("-75.00"), closing.amount);
    assert_eq!(0, closing.pending_excluded);
}

#[test]
fn prcd_and_itbd_balance_types_work_like_opbd_and_clbd() {
    let xml = camt(
        IBAN,
        &[
            balance("PRCD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("ITBD", "80.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
            entry("10.00", "CRDT", "BOOK", "2026-02-05", "R2"),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(dec("80.00"), s.closing_balance.unwrap().amount);
}

#[test]
fn only_available_balances_a_missing_opening_balance_or_a_dateless_closing_balance_yield_none() {
    let only_available = camt(
        IBAN,
        &[
            balance("OPAV", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLAV", "80.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );
    assert_eq!(None, camt::parse(only_available.as_bytes()).unwrap().closing_balance);

    let no_opening = camt(
        IBAN,
        &[
            balance("CLBD", "50.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );
    assert_eq!(None, camt::parse(no_opening.as_bytes()).unwrap().closing_balance);

    let no_closing_date = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "50.00", "", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );
    assert_eq!(None, camt::parse(no_closing_date.as_bytes()).unwrap().closing_balance);
}

#[test]
fn a_skipped_entry_disables_the_closing_balance() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "50.00", "2026-02-28", "EUR", "CRDT"),
            "<Ntry><Amt Ccy=\"EUR\">abc</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-02-05</Dt></BookgDt></Ntry>"
                .to_owned(),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(1, s.skipped.len());
    assert_eq!(None, s.closing_balance);
}

#[test]
fn a_closing_currency_differing_from_the_entry_currencies_is_rejected() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "USD", "CRDT"),
            balance("CLBD", "50.00", "2026-02-28", "USD", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );

    assert_eq!(None, camt::parse(xml.as_bytes()).unwrap().closing_balance);
}

#[test]
fn the_zip_picks_the_report_with_the_latest_closing_date() {
    let january = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-01-31", "EUR", "CRDT"),
            balance("CLBD", "70.00", "2026-01-31", "EUR", "CRDT"),
            debited_entry("30.00", "2026-01-15", "R1"),
        ],
    );
    let february = camt(
        IBAN,
        &[
            balance("OPBD", "70.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
            entry("10.00", "CRDT", "BOOK", "2026-02-10", "R2"),
        ],
    );

    let s = camt::parse(&zip(&[january, february])).unwrap();
    assert_eq!(dec("80.00"), s.closing_balance.unwrap().amount);
}

#[test]
fn a_latest_report_that_does_not_reconcile_leaves_no_fallback_to_an_older_one() {
    let january = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-01-31", "EUR", "CRDT"),
            balance("CLBD", "70.00", "2026-01-31", "EUR", "CRDT"),
            debited_entry("30.00", "2026-01-15", "R1"),
        ],
    );
    let february = camt(
        IBAN,
        &[
            balance("OPBD", "70.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "99.00", "2026-02-28", "EUR", "CRDT"),
            entry("10.00", "CRDT", "BOOK", "2026-02-10", "R2"),
        ],
    );

    assert_eq!(None, camt::parse(&zip(&[january, february])).unwrap().closing_balance);
}

#[test]
fn two_reports_sharing_the_latest_date_with_different_closing_balances_yield_none() {
    let first = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "80.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );
    let second = camt(
        IBAN,
        &[
            balance("OPBD", "110.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "95.00", "2026-02-28", "EUR", "CRDT"),
            debited_entry("15.00", "2026-02-05", "R2"),
        ],
    );

    assert_eq!(None, camt::parse(&zip(&[first, second])).unwrap().closing_balance);
}

#[test]
fn a_batch_entry_reconciles_on_the_entry_amount_not_the_split_details() {
    let batch = "<Ntry>\
<Amt Ccy=\"EUR\">10.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>BOOK</Sts>\
<BookgDt><Dt>2026-02-05</Dt></BookgDt><ValDt><Dt>2026-02-05</Dt></ValDt>\
<AcctSvcrRef>R1</AcctSvcrRef>\
<NtryDtls><TxDtls><Amt Ccy=\"EUR\">6.00</Amt><RltdPties><Cdtr><Nm>A</Nm></Cdtr></RltdPties></TxDtls>\
<TxDtls><Amt Ccy=\"EUR\">4.00</Amt><RltdPties><Cdtr><Nm>B</Nm></Cdtr></RltdPties></TxDtls></NtryDtls>\
</Ntry>";

    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "110.00", "2026-02-28", "EUR", "CRDT"),
            batch.to_owned(),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(2, s.bookings.len());
    let closing = s.closing_balance.unwrap();
    assert_eq!(dec("110.00"), closing.amount);
    assert_eq!(0, closing.pending_excluded);
}

#[test]
fn a_malformed_balance_is_no_parse_failure_and_keeps_the_bookings() {
    let xml = camt(
        IBAN,
        &[
            balance("OPBD", "100.00", "2026-02-28", "EUR", "CRDT"),
            balance("CLBD", "abc", "2026-02-28", "EUR", "CRDT"),
            debited_entry("30.00", "2026-02-05", "R1"),
        ],
    );

    let s = camt::parse(xml.as_bytes()).unwrap();

    assert_eq!(1, s.bookings.len());
    assert_eq!(None, s.closing_balance);
}
