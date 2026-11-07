//! Ported parser-level tests from FinDesk's `MoreFormatsTest.kt`.
//!
//! Skipped tests (need the database/import service):
//! - `re-importing the same mt940 reports all bookings as already known` (database, storing and dedup)
//! - `qif identical records with N type import both` (database, storing and dedup)

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{StatementFormat, mt940, ofx, qif, read};
use support::{cp1252, dec};
use time::macros::date;

mod mt940_parser {
    use super::*;

    #[test]
    fn mt940_with_two_statements_structured_86_wrapped_line_reversal_and_year_rollover() {
        let mt940 = cp1252(
            ":20:STARTUMS
:25:DE89370400440532013000
:28C:00001/001
:60F:C231030EUR1234,56
:61:2310301031D45,67N066NONREF
:86:166?00KARTENZAHLUNG?100005?20REWE SAGT DANKE?211234?32REWE Mar
kt GmbH
:61:2310311102C500,00N066NONREF
:86:166?00GUTSCHRIFT?100005?20Gehalt Oktober?32Müller & Söhne GmbH
:62F:C231031EUR1688,89
-
:20:STARTUMS
:25:DE89370400440532013000
:28C:00001/002
:60F:C231031EUR1688,89
:61:2312310102RD100,00N066RUECKLASTSCHRIFT
:86:166?00RÜCKBUCHUNG?20Lastschrift retour?32Kunde AG
:62F:C240102EUR1588,89",
        );

        let s = mt940::parse(&mt940).unwrap();

        assert_eq!(StatementFormat::Mt940, s.format);
        assert_eq!(Some("DE89370400440532013000"), s.account_iban.as_deref());
        assert_eq!(3, s.bookings.len());
        assert!(s.skipped.is_empty(), "{:?}", s.skipped);

        let rewe = &s.bookings[0];
        assert_eq!(date!(2023 - 10 - 31), rewe.date);
        assert_eq!(Some(date!(2023 - 10 - 30)), rewe.value_date);
        assert_eq!(dec("-45.67"), rewe.amount);
        assert_eq!(Some("REWE Markt GmbH"), rewe.partner_name.as_deref());
        assert_eq!(Some("REWE SAGT DANKE1234"), rewe.purpose.as_deref());
        assert_eq!(Some("EUR"), rewe.currency.as_deref());
        assert_eq!(None, rewe.balance_after);

        let gehalt = &s.bookings[1];
        assert_eq!(date!(2023 - 11 - 02), gehalt.date);
        assert_eq!(Some(date!(2023 - 10 - 31)), gehalt.value_date);
        assert_eq!(dec("500.00"), gehalt.amount);
        assert_eq!(Some("Müller & Söhne GmbH"), gehalt.partner_name.as_deref());
        assert_eq!(Some("Gehalt Oktober"), gehalt.purpose.as_deref());

        let reversal = &s.bookings[2];
        assert_eq!(date!(2024 - 01 - 02), reversal.date);
        assert_eq!(Some(date!(2023 - 12 - 31)), reversal.value_date);
        assert_eq!(dec("-100.00"), reversal.amount);
        assert_eq!(Some("RUECKLASTSCHRIFT"), reversal.bank_reference.as_deref());
    }

    #[test]
    fn mt940_structured_86_with_gvc_partner_iban_bic_and_wrapped_subfield_marker() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:2401020103C1234,56NTRF
:86:166?00GUTSCHRIFT?100005?20EREF+X?21SVWZ+Rechnung 4711?2
0Weitere Zeile?30COBADEFFXXX?31DE89370400440532013000?32Max Muster
:62F:C240102EUR1234,56";

        let s = mt940::parse(mt940.as_bytes()).unwrap();
        assert_eq!(1, s.bookings.len());
        let b = &s.bookings[0];
        assert_eq!(date!(2024 - 01 - 03), b.date);
        assert_eq!(Some(date!(2024 - 01 - 02)), b.value_date);
        assert_eq!(dec("1234.56"), b.amount);
        assert_eq!(Some("Max Muster"), b.partner_name.as_deref());
        assert_eq!(Some("DE89370400440532013000"), b.partner_iban.as_deref());
        assert_eq!(Some("EREF+XSVWZ+Rechnung 4711Weitere Zeile"), b.purpose.as_deref());
    }

    #[test]
    fn mt940_purpose_containing_a_time_is_not_cut_at_a_false_tag() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:240102D10,00N066NONREF
:86:2026-10-03T14:30:15 meeting at 14:30:15
:62F:C240102EUR10,00";

        let s = mt940::parse(mt940.as_bytes()).unwrap();
        assert_eq!(1, s.bookings.len());
        assert_eq!(
            Some("2026-10-03T14:30:15 meeting at 14:30:15"),
            s.bookings[0].purpose.as_deref()
        );
    }

    #[test]
    fn mt940_funds_code_after_debit_credit_mark() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:240102DR10,50N066NONREF
:86:?20Coffee
:62F:C240102EUR10,50";

        let s = mt940::parse(mt940.as_bytes()).unwrap();
        assert_eq!(1, s.bookings.len());
        assert_eq!(dec("-10.50"), s.bookings[0].amount);

        let mt940_reversal = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:240102RCR10,50N066NONREF
:86:?20Refund
:62F:C240102EUR10,50";
        let sr = mt940::parse(mt940_reversal.as_bytes()).unwrap();
        assert_eq!(dec("10.50"), sr.bookings[0].amount);
    }

    #[test]
    fn mt940_with_a_20_digit_amount_skips_the_record_instead_of_importing_it() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:2401020103C99999999999999999999NTRF
:86:?20Huge
:62F:C240102EUR0,00";

        let s = mt940::parse(mt940.as_bytes()).unwrap();

        assert!(s.bookings.is_empty());
        assert_eq!(1, s.skipped.len());
        assert!(s.skipped[0].starts_with("Record 1: invalid amount"), "{:?}", s.skipped);
    }

    #[test]
    fn mt940_bank_reference_after_slashes_and_supplementary_details() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C240102EUR0,00
:61:240102D25,00NTRFEXAMPLE//1234567890
extra line ignored
:86:?20Taxi
:62F:C240102EUR25,00";

        let s = mt940::parse(mt940.as_bytes()).unwrap();
        assert_eq!(1, s.bookings.len());
        assert_eq!(Some("1234567890"), s.bookings[0].bank_reference.as_deref());
    }

    #[test]
    fn mt25_currency_appended_to_iban() {
        let mt940 = ":20:STARTUMS
:25:DE89370400440532013000EUR
:28C:1
:60F:C240102EUR0,00
:61:240102D5,00N066NONREF
:86:?20Test
:62F:C240102EUR5,00";

        let s = mt940::parse(mt940.as_bytes()).unwrap();
        assert_eq!(Some("DE89370400440532013000"), s.account_iban.as_deref());
    }
}

mod ofx_parser {
    use super::*;
    use rust_decimal::Decimal;

    fn ofx_sgml() -> String {
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
<ACCTID>DE89370400440532013000</ACCTID>
<ACCTTYPE>CHECKING</ACCTTYPE>
</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20261003000000</DTSTART>
<DTEND>20261004000000</DTEND>
<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>20261003000000</DTPOSTED>
<TRNAMT>-45.67</TRNAMT>
<FITID>REF-1</FITID>
<NAME>REWE Markt GmbH</NAME>
<MEMO>REWE SAGT DANKE 1234</MEMO>
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT</TRNTYPE>
<DTPOSTED>20261001000000</DTPOSTED>
<TRNAMT>3200.00</TRNAMT>
<FITID>REF-2</FITID>
<NAME>Müller &amp; Söhne GmbH</NAME>
<MEMO>Gehalt Oktober</MEMO>
</STMTTRN>
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>"
            .to_owned()
    }

    fn ofx_xml() -> &'static str {
        r#"<?xml version="1.0" encoding="UTF-8"?>
<OFX>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <STMTRS>
        <CURDEF>USD</CURDEF>
        <BANKACCTFROM>
          <ACCTID>DE89370400440532013000</ACCTID>
        </BANKACCTFROM>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261003000000</DTPOSTED>
            <TRNAMT>-45.67</TRNAMT>
            <FITID>REF-1</FITID>
            <NAME>REWE Markt GmbH</NAME>
            <MEMO>REWE SAGT DANKE 1234</MEMO>
          </STMTTRN>
          <STMTTRN>
            <TRNTYPE>CREDIT</TRNTYPE>
            <DTPOSTED>20261001000000</DTPOSTED>
            <TRNAMT>3200.00</TRNAMT>
            <FITID>REF-2</FITID>
            <NAME>Müller &amp; Söhne GmbH</NAME>
            <MEMO>Gehalt Oktober</MEMO>
          </STMTTRN>
        </BANKTRANLIST>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>"#
    }

    #[test]
    fn ofx_sgml_and_xml_produce_the_same_bookings_with_fitid_as_bank_reference() {
        let sgml = ofx::parse(ofx_sgml().as_bytes()).unwrap();
        let xml = ofx::parse(ofx_xml().as_bytes()).unwrap();

        assert_eq!(StatementFormat::Ofx, sgml.format);
        assert_eq!(StatementFormat::Ofx, xml.format);
        assert_eq!(Some("DE89370400440532013000"), sgml.account_iban.as_deref());
        assert_eq!(Some("DE89370400440532013000"), xml.account_iban.as_deref());
        assert_eq!(2, sgml.bookings.len());
        assert_eq!(2, xml.bookings.len());
        assert!(sgml.skipped.is_empty());
        assert!(xml.skipped.is_empty());

        let sgml_refs: Vec<&str> = sgml
            .bookings
            .iter()
            .filter_map(|b| b.bank_reference.as_deref())
            .collect();
        let xml_refs: Vec<&str> = xml
            .bookings
            .iter()
            .filter_map(|b| b.bank_reference.as_deref())
            .collect();
        assert_eq!(vec!["REF-1", "REF-2"], sgml_refs);
        assert_eq!(vec!["REF-1", "REF-2"], xml_refs);

        let s = &sgml.bookings[0];
        assert_eq!(date!(2026 - 10 - 03), s.date);
        assert_eq!(dec("-45.67"), s.amount);
        assert_eq!(Some("REWE Markt GmbH"), s.partner_name.as_deref());
        assert_eq!(Some("REWE SAGT DANKE 1234"), s.purpose.as_deref());
        assert_eq!(Some("EUR"), s.currency.as_deref());

        let sgml_amounts: Vec<Decimal> = sgml.bookings.iter().map(|b| b.amount).collect();
        let xml_amounts: Vec<Decimal> = xml.bookings.iter().map(|b| b.amount).collect();
        assert_eq!(sgml_amounts, xml_amounts);
        let sgml_names: Vec<&Option<String>> = sgml.bookings.iter().map(|b| &b.partner_name).collect();
        let xml_names: Vec<&Option<String>> = xml.bookings.iter().map(|b| &b.partner_name).collect();
        assert_eq!(sgml_names, xml_names);
        let sgml_purposes: Vec<&Option<String>> = sgml.bookings.iter().map(|b| &b.purpose).collect();
        let xml_purposes: Vec<&Option<String>> = xml.bookings.iter().map(|b| &b.purpose).collect();
        assert_eq!(sgml_purposes, xml_purposes);
        assert_eq!(Some("USD"), xml.bookings[0].currency.as_deref());
    }

    #[test]
    fn ofx_sgml_in_windows_1252_with_umlauts_and_decoded_ampersand() {
        let sgml = cp1252(
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
<BANKACCTFROM>
<ACCTID>DE89370400440532013000</ACCTID>
</BANKACCTFROM>
<BANKTRANLIST>
<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>20261003000000</DTPOSTED>
<TRNAMT>-45,67</TRNAMT>
<FITID>REF-1</FITID>
<NAME>Müller &amp; Söhne GmbH</NAME>
<MEMO>äöü ß &amp;lt;b&amp;gt;</MEMO>
</STMTTRN>
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>",
        );

        let s = ofx::parse(&sgml).unwrap();
        assert_eq!(1, s.bookings.len());
        let b = &s.bookings[0];
        assert_eq!(dec("-45.67"), b.amount);
        assert_eq!(Some("Müller & Söhne GmbH"), b.partner_name.as_deref());
        assert_eq!(Some("äöü ß &lt;b&gt;"), b.purpose.as_deref());
    }

    #[test]
    fn ofx_with_an_out_of_range_amount_skips_the_record_instead_of_importing_it() {
        let sgml = "OFXHEADER:100
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
<BANKTRANLIST>
<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>20261003000000</DTPOSTED>
<TRNAMT>1E999999999</TRNAMT>
<FITID>REF-1</FITID>
<NAME>REWE Markt GmbH</NAME>
</STMTTRN>
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>";

        let s = ofx::parse(sgml.as_bytes()).unwrap();

        assert!(s.bookings.is_empty());
        assert_eq!(1, s.skipped.len());
        assert!(s.skipped[0].contains("invalid amount"), "{:?}", s.skipped);
    }

    #[test]
    fn ofx_xml_with_doctype_and_external_entity_is_rejected() {
        let xxe = r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE ofx [<!ENTITY x SYSTEM "file:///etc/passwd">]>
<OFX>
  <BANKMSGSRSV1>
    <STMTTRNRS>
      <STMTRS>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261003000000</DTPOSTED>
            <TRNAMT>-45.67</TRNAMT>
            <FITID>X</FITID>
            <NAME>&x;</NAME>
          </STMTTRN>
        </BANKTRANLIST>
      </STMTRS>
    </STMTTRNRS>
  </BANKMSGSRSV1>
</OFX>"#;
        assert!(ofx::parse(xxe.as_bytes()).is_err());
    }
}

mod qif_parser {
    use super::*;

    #[test]
    fn qif_with_us_dates_and_thousands_separator() {
        let qif = "!Type:Bank
D10/03/2026
T-45.67
PREWE Markt GmbH
MREWE SAGT DANKE 1234
N123
^
D10/01/26
T3,200.00
PMüller & Söhne GmbH
MGehalt Oktober
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(StatementFormat::Qif, s.format);
        assert_eq!(2, s.bookings.len());
        assert_eq!(date!(2026 - 10 - 03), s.bookings[0].date);
        assert_eq!(dec("-45.67"), s.bookings[0].amount);
        assert_eq!(Some("REWE SAGT DANKE 1234"), s.bookings[0].purpose.as_deref());
        assert_eq!(None, s.bookings[0].bank_reference);
        assert_eq!(date!(2026 - 10 - 01), s.bookings[1].date);
        assert_eq!(dec("3200.00"), s.bookings[1].amount);
    }

    #[test]
    fn qif_with_german_dotted_dates() {
        let qif = "!Type:Bank
D03.10.2026
T-45,67
PREWE Markt GmbH
^
D01.10.26
T3.200,00
PMüller & Söhne GmbH
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(2, s.bookings.len());
        assert_eq!(date!(2026 - 10 - 03), s.bookings[0].date);
        assert_eq!(dec("-45.67"), s.bookings[0].amount);
        assert_eq!(dec("3200.00"), s.bookings[1].amount);
    }

    #[test]
    fn qif_day_greater_than_12_disambiguates_slash_dates_to_day_month() {
        let qif = "!Type:Bank
D13/10/2026
T-45.67
PShop
^
D14/10/26
T-20.00
PCafe
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(2, s.bookings.len());
        assert_eq!(date!(2026 - 10 - 13), s.bookings[0].date);
        assert_eq!(date!(2026 - 10 - 14), s.bookings[1].date);
    }

    #[test]
    fn qif_apostrophe_year_separator() {
        let qif = "!Type:Bank
D10/03'26
T-12.00
PKiosk
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(date!(2026 - 10 - 03), s.bookings[0].date);
    }

    #[test]
    fn qif_unpadded_and_spaced_dates() {
        let qif = "!Type:Bank
D1/5/26
T-10.00
PA
^
D 1/ 5'26
T-11.00
PB
^
D5.1.2026
T-12.00
PC
^
D12/25/2026
T-13.00
PD
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(4, s.bookings.len());
        assert_eq!(date!(2026 - 01 - 05), s.bookings[0].date);
        assert_eq!(date!(2026 - 01 - 05), s.bookings[1].date);
        assert_eq!(date!(2026 - 01 - 05), s.bookings[2].date);
        assert_eq!(date!(2026 - 12 - 25), s.bookings[3].date);
    }

    #[test]
    fn qif_with_bom_and_account_header() {
        let qif = "\u{feff}!Account
NMy Bank
TBank
^
!Type:Bank
D10/03/2026
T-45.67
PREWE Markt GmbH
MREWE SAGT DANKE
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(1, s.bookings.len());
        assert_eq!(date!(2026 - 10 - 03), s.bookings[0].date);
    }

    #[test]
    fn qif_skips_non_bank_types() {
        let qif = "!Type:Bank
D10/03/2026
T-45.67
PREWE
^
!Type:Invst
D10/03/2026
T-100.00
PBroker
^
!Type:Bank
D10/04/2026
T-12.00
PCafe
^";

        let s = qif::parse(qif.as_bytes()).unwrap();
        assert_eq!(2, s.bookings.len());
        let names: Vec<&str> = s.bookings.iter().filter_map(|b| b.partner_name.as_deref()).collect();
        assert_eq!(vec!["REWE", "Cafe"], names);
    }
}

mod more_formats_integration {
    use super::*;

    fn mt940_text() -> &'static str {
        ":20:STARTUMS
:25:DE89370400440532013000
:28C:00001/001
:60F:C231030EUR1234,56
:61:2310301031D45,67N066NONREF
:86:166?00KARTENZAHLUNG?100005?20REWE SAGT DANKE?211234?32REWE Markt GmbH
:61:2310311102C500,00N066NONREF
:86:166?00GUTSCHRIFT?100005?20Gehalt Oktober?32Müller & Söhne GmbH
:62F:C231031EUR1688,89"
    }

    #[test]
    fn service_routes_sta_ofx_qif_and_txt_mt940_to_correct_parsers() {
        let today = date!(2026 - 11 - 01);
        let mt940 = mt940_text().as_bytes();
        assert_eq!(
            StatementFormat::Mt940,
            read("stmt.sta", mt940, None, today).unwrap().format
        );
        assert_eq!(
            StatementFormat::Mt940,
            read("stmt.txt", mt940, None, today).unwrap().format
        );

        let ofx = "OFXHEADER:100
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
<BANKACCTFROM>
<ACCTID>DE89370400440532013000</ACCTID>
</BANKACCTFROM>
<BANKTRANLIST>
<STMTTRN>
<TRNTYPE>DEBIT</TRNTYPE>
<DTPOSTED>20261003000000</DTPOSTED>
<TRNAMT>-45.67</TRNAMT>
<FITID>REF-1</FITID>
<NAME>REWE Markt GmbH</NAME>
<MEMO>REWE SAGT DANKE 1234</MEMO>
</STMTTRN>
</BANKTRANLIST>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>";
        assert_eq!(
            StatementFormat::Ofx,
            read("stmt.ofx", ofx.as_bytes(), None, today).unwrap().format
        );

        let qif = "!Type:Bank
D10/03/2026
T-45.67
PREWE Markt GmbH
MREWE SAGT DANKE 1234
^";
        assert_eq!(
            StatementFormat::Qif,
            read("stmt.qif", qif.as_bytes(), None, today).unwrap().format
        );
    }
}
