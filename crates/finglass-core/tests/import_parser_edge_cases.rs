//! Ported parser-level tests from FinDesk's `ParserEdgeCasesTest.kt`.
//!
//! No tests from this file are skipped.

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{mt940, ofx};
use support::dec;
use time::macros::date;

#[test]
fn a_truncated_mt940_detail_marker_does_not_abort_the_file() {
    let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:2401020102C10,00N066REF1
:86:?20First
:61:2401030103C20,00N
:61:2401040104D5,50N066REF3
:86:?20Third
:62F:C240104EUR24,50";

    let s = mt940::parse(mt940.as_bytes()).unwrap();

    assert!(s.skipped.is_empty(), "{:?}", s.skipped);
    assert_eq!(3, s.bookings.len());
    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(vec![dec("10.00"), dec("20.00"), dec("-5.50")], amounts);
    let dates: Vec<_> = s.bookings.iter().map(|b| b.date).collect();
    assert_eq!(
        vec![date!(2024 - 01 - 02), date!(2024 - 01 - 03), date!(2024 - 01 - 04)],
        dates
    );
    assert_eq!(Some("REF1"), s.bookings[0].bank_reference.as_deref());
    assert_eq!(None, s.bookings[1].bank_reference);
    assert_eq!(Some("REF3"), s.bookings[2].bank_reference.as_deref());
}

#[test]
fn ofx_amounts_with_both_separators_use_the_last_one_as_decimal_separator() {
    fn transaction(date: &str, amount: &str, fit_id: &str) -> String {
        format!(
            "<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>{date}</DTPOSTED>
<TRNAMT>{amount}</TRNAMT>
<FITID>{fit_id}</FITID>
<NAME>Test</NAME>
</STMTTRN>"
        )
    }

    let ofx = format!(
        "OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
<BANKMSGSRSV1>
<STMTTRNRS>
<STMTRS>
<CURDEF>EUR</CURDEF>
<BANKACCTFROM>
<ACCTID>DE89370400440532013000</ACCTID>
</BANKACCTFROM>
<BANKTRANLIST>
{}
{}
{}
{}
{}
{}
{}
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>",
        transaction("20261001000000", "-1.234,56", "REF-1"),
        transaction("20261002000000", "-1,234.56", "REF-2"),
        transaction("20261003000000", "-45,67", "REF-3"),
        transaction("20261004000000", "-45.67", "REF-4"),
        transaction("20261005000000", "1234", "REF-5"),
        transaction("20261006000000", "1.234.567", "REF-6"),
        transaction("20261007000000", "1,234,567", "REF-7"),
    );

    let s = ofx::parse(ofx.as_bytes()).unwrap();

    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(
        vec![
            dec("-1234.56"),
            dec("-1234.56"),
            dec("-45.67"),
            dec("-45.67"),
            dec("1234"),
        ],
        amounts
    );
    assert_eq!(
        vec![
            "Record 6: invalid amount '1.234.567'",
            "Record 7: invalid amount '1,234,567'",
        ],
        s.skipped
    );
}
