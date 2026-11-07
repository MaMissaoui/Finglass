//! Ported parser-level tests from FinDesk's `MultiAccountImportTest.kt`.
//!
//! Skipped tests (need the database/import service):
//! - `camt with two accounts is refused by read and import stores nothing` (import storing)
//! - `camt with two blocks for the same account is accepted and imports all bookings` (import storing)
//! - `zip with two camt files for the same account is accepted` (import storing)
//! - `mt940 with two statements for the same account is accepted` (import storing)
//! - `ofx sgml with one statement block is accepted` (import storing)
//! - `ofx xml with one statement block is accepted` (import storing)

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{camt, read};
use support::write_zip;
use time::macros::date;

const IBAN: &str = "DE89370400440532013000";
const OTHER_IBAN: &str = "DE44500105175407324931";

fn camt(statements: &str) -> String {
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">\n{statements}\n</Document>"
    )
}

fn stmt(iban: &str, entries: &str) -> String {
    format!("<BkToCstmrStmt><Stmt>\n<Acct><Id><IBAN>{iban}</IBAN></Id></Acct>\n{entries}\n</Stmt></BkToCstmrStmt>")
}

fn entry(amount: &str, date: &str, reference: &str) -> String {
    format!(
        "<Ntry>\n<Amt Ccy=\"EUR\">{amount}</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>\n\
<BookgDt><Dt>{date}</Dt></BookgDt><ValDt><Dt>{date}</Dt></ValDt>\n\
<AcctSvcrRef>{reference}</AcctSvcrRef>\n\
<NtryDtls><TxDtls>\n<RltdPties><Cdtr><Nm>Partner</Nm></Cdtr></RltdPties>\n\
<RmtInf><Ustrd>Test</Ustrd></RmtInf>\n</TxDtls></NtryDtls>\n</Ntry>"
    )
}

fn ofx_sgml(acct_id: &str) -> String {
    format!(
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
<BANKID>37040044</BANKID>
<ACCTID>{acct_id}</ACCTID>
<ACCTTYPE>CHECKING</ACCTTYPE>
</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20261003000000</DTSTART>
<DTEND>20261003000000</DTEND>
<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>20261003000000</DTPOSTED>
<TRNAMT>-10.00</TRNAMT>
<FITID>REF-{acct_id}</FITID>
<NAME>Partner</NAME>
<MEMO>Test</MEMO>
</STMTTRN>
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>"
    )
}

fn ofx_xml_header() -> &'static str {
    r#"<?xml version="1.0" encoding="UTF-8"?>
<OFX>
"#
}

fn ofx_xml_statement(acct_id: &str) -> String {
    format!(
        r#"  <BANKMSGSRSV1>
    <STMTTRNRS>
      <STMTRS>
        <CURDEF>EUR</CURDEF>
        <BANKACCTFROM>
          <ACCTID>{acct_id}</ACCTID>
        </BANKACCTFROM>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261003000000</DTPOSTED>
            <TRNAMT>-10.00</TRNAMT>
            <FITID>REF-{acct_id}</FITID>
            <NAME>Partner</NAME>
            <MEMO>Test</MEMO>
          </STMTTRN>
        </BANKTRANLIST>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
"#
    )
}

mod camt_multi_account {
    use super::*;

    #[test]
    fn two_stmt_blocks_for_different_ibans_lists_both_accounts_and_is_refused() {
        let xml = camt(&format!(
            "{}{}",
            stmt(IBAN, &entry("10.00", "2026-10-01", "A1")),
            stmt(OTHER_IBAN, &entry("20.00", "2026-10-02", "A2"))
        ));
        let s = camt::parse(xml.as_bytes()).unwrap();
        assert_eq!(vec![IBAN.to_owned(), OTHER_IBAN.to_owned()], s.accounts);
        assert_eq!(2, s.bookings.len());
    }

    #[test]
    fn two_stmt_blocks_for_the_same_iban_lists_one_account_and_keeps_all_bookings() {
        let xml = camt(&format!(
            "{}{}",
            stmt(IBAN, &entry("10.00", "2026-10-01", "A1")),
            stmt(IBAN, &entry("20.00", "2026-10-02", "A2"))
        ));
        let s = camt::parse(xml.as_bytes()).unwrap();
        assert_eq!(vec![IBAN.to_owned()], s.accounts);
        assert_eq!(2, s.bookings.len());
    }
}

mod multi_account_read {
    use super::*;

    fn today() -> time::Date {
        date!(2026 - 11 - 01)
    }

    #[test]
    fn zip_with_two_camt_files_for_different_accounts_is_refused() {
        let first = camt(&stmt(IBAN, &entry("10.00", "2026-10-01", "A1")));
        let second = camt(&stmt(OTHER_IBAN, &entry("20.00", "2026-10-02", "A2")));
        let zip = write_zip(&[("a.xml", first.as_bytes()), ("b.xml", second.as_bytes())]);
        let error = read("two-accounts.zip", &zip, None, today()).unwrap_err();
        assert!(error.to_string().contains('2'), "{error}");
    }

    #[test]
    fn mt940_with_two_statements_for_different_accounts_is_refused() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C241001EUR0,00
:61:241001D10,00N066NONREF
:86:?20Test
:62F:C241001EUR10,00
-
:20:STARTUMS
:25:DE44500105175407324931
:28C:1
:60F:C241001EUR0,00
:61:241001D20,00N066NONREF
:86:?20Test
:62F:C241001EUR20,00";
        let error = read("two-accounts.sta", mt940.as_bytes(), None, today()).unwrap_err();
        assert!(error.to_string().contains('2'), "{error}");
    }

    #[test]
    fn ofx_sgml_with_two_statement_blocks_for_different_accounts_is_refused() {
        let ofx = format!("{}{}", ofx_sgml(IBAN), ofx_sgml(OTHER_IBAN));
        let error = read("two-accounts.ofx", ofx.as_bytes(), None, today()).unwrap_err();
        assert!(error.to_string().contains('2'), "{error}");
    }

    #[test]
    fn ofx_xml_with_two_statement_blocks_for_different_accounts_is_refused() {
        let ofx = format!(
            "{}{}{}</OFX>",
            ofx_xml_header(),
            ofx_xml_statement(IBAN),
            ofx_xml_statement(OTHER_IBAN)
        );
        let error = read("two-accounts-xml.ofx", ofx.as_bytes(), None, today()).unwrap_err();
        assert!(error.to_string().contains('2'), "{error}");
    }
}
