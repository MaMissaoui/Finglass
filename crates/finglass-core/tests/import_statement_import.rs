//! Ported parser-level tests from FinDesk's `StatementImportTest.kt`.
//!
//! Skipped tests (need the database/import service, storing, dedup and balance updates):
//! - `re-importing and overlapping exports skip known bookings but keep genuine repeats`
//! - `statement iban selects the account and balance is taken from the newest booking`
//! - `newest-first files with running balances are inserted chronologically`
//! - `oldest-first files still import the same way`
//! - `a newest-first file covering one day is recognised by its running balances`
//! - `bookings from an imported file are never marked new`
//! - `camt files are detected by content`

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{CsvMapping, camt, csv};
use support::{cp1252, dec, write_zip};
use time::macros::date;

mod csv_statement_parser {
    use super::*;

    #[test]
    fn sparkasse_csv_camt_export_in_windows_1252_with_pending_rows() {
        let csv = cp1252(
            "\"Auftragskonto\";\"Buchungstag\";\"Valutadatum\";\"Buchungstext\";\"Verwendungszweck\";\"Glaeubiger ID\";\"Mandatsreferenz\";\"Kundenreferenz (End-to-End)\";\"Sammlerreferenz\";\"Lastschrift Ursprungsbetrag\";\"Auslagenersatz Ruecklastschrift\";\"Beguenstigter/Zahlungspflichtiger\";\"Kontonummer/IBAN\";\"BIC (SWIFT-Code)\";\"Betrag\";\"Waehrung\";\"Info\"
\"DE89370400440532013000\";\"03.10.26\";\"03.10.26\";\"KARTENZAHLUNG\";\"REWE SAGT DANKE 1234\";\"\";\"\";\"\";\"\";\"\";\"\";\"REWE Markt GmbH\";\"DE44500105175407324931\";\"COLSDE33XXX\";\"-45,67\";\"EUR\";\"Umsatz gebucht\"
\"DE89370400440532013000\";\"01.10.26\";\"01.10.26\";\"GUTSCHR. UEBERWEISUNG\";\"Gehalt Oktober\";\"\";\"\";\"\";\"\";\"\";\"\";\"Müller & Söhne GmbH\";\"\";\"\";\"3.200,00\";\"EUR\";\"Umsatz gebucht\"
\"DE89370400440532013000\";\"04.10.26\";\"04.10.26\";\"KARTENZAHLUNG\";\"Tankstelle\";\"\";\"\";\"\";\"\";\"\";\"\";\"Aral\";\"\";\"\";\"-60,00\";\"EUR\";\"Umsatz vorgemerkt\"",
        );

        let s = csv::parse(&csv, None).unwrap();

        assert_eq!("windows-1252", s.csv.as_ref().unwrap().charset_name);
        assert_eq!(';', s.csv.as_ref().unwrap().delimiter);
        assert_eq!(2, s.bookings.len());
        assert_eq!(1, s.pending_skipped);
        let rewe = &s.bookings[0];
        assert_eq!(date!(2026 - 10 - 03), rewe.date);
        assert_eq!(dec("-45.67"), rewe.amount);
        assert_eq!(Some("REWE Markt GmbH"), rewe.partner_name.as_deref());
        assert_eq!(Some("DE44500105175407324931"), rewe.partner_iban.as_deref());
        assert_eq!(Some("REWE SAGT DANKE 1234"), rewe.purpose.as_deref());
        assert_eq!(Some("Müller & Söhne GmbH"), s.bookings[1].partner_name.as_deref());
        assert_eq!(dec("3200.00"), s.bookings[1].amount);
    }

    #[test]
    fn ing_export_with_preamble_account_iban_and_running_balance() {
        let csv = "Umsatzanzeige;Datei erstellt am: 04.10.2026 10:00
;
IBAN;DE89 3704 0044 0532 0130 00
Kontoname;Girokonto
Bank;ING
Kunde;Max Mustermann
Zeitraum;01.09.2026 - 04.10.2026
Saldo;5.234,56;EUR

Sortierung;Datum absteigend

Buchung;Valuta;Auftraggeber/Empfänger;Buchungstext;Verwendungszweck;Saldo;Währung;Betrag;Währung
02.10.2026;02.10.2026;AMAZON EU;Lastschrift;\"Bestellung 303-123; Monitor\";5.234,56;EUR;-89,99;EUR
01.10.2026;01.10.2026;Employer GmbH;Gehalt/Rente;Gehalt;5.324,55;EUR;3.200,00;EUR";

        let s = csv::parse(csv.as_bytes(), None).unwrap();

        assert_eq!(Some("DE89370400440532013000"), s.account_iban.as_deref());
        assert_eq!(2, s.bookings.len());
        assert_eq!(Some("AMAZON EU"), s.bookings[0].partner_name.as_deref());
        assert_eq!(Some("Bestellung 303-123; Monitor"), s.bookings[0].purpose.as_deref());
        assert_eq!(Some(dec("5234.56")), s.bookings[0].balance_after);
        assert_eq!(dec("-89.99"), s.bookings[0].amount);
    }

    #[test]
    fn dkb_export_uses_payer_for_incoming_and_payee_for_outgoing() {
        let csv = "\"Konto\";\"Girokonto DE89370400440532013000\"
\"\"
\"Buchungsdatum\";\"Wertstellung\";\"Status\";\"Zahlungspflichtige*r\";\"Zahlungsempfänger*in\";\"Verwendungszweck\";\"Umsatztyp\";\"IBAN\";\"Betrag (€)\";\"Gläubiger-ID\";\"Mandatsreferenz\";\"Kundenreferenz\"
\"02.10.26\";\"02.10.26\";\"Gebucht\";\"Max Mustermann\";\"Stadtwerke\";\"Abschlag Strom\";\"Ausgang\";\"DE44500105175407324931\";\"-80,00\";\"\";\"\";\"\"
\"01.10.26\";\"01.10.26\";\"Gebucht\";\"Erika Musterfrau\";\"Max Mustermann\";\"Miete Anteil\";\"Eingang\";\"DE44500105175407324931\";\"400,00\";\"\";\"\";\"\"
\"05.10.26\";\"05.10.26\";\"Vorgemerkt\";\"Max Mustermann\";\"Kino\";\"Tickets\";\"Ausgang\";\"\";\"-20,00\";\"\";\"\";\"\"";

        let s = csv::parse(csv.as_bytes(), None).unwrap();

        let names: Vec<&str> = s.bookings.iter().filter_map(|b| b.partner_name.as_deref()).collect();
        assert_eq!(vec!["Stadtwerke", "Erika Musterfrau"], names);
        assert_eq!(Some("DE89370400440532013000"), s.account_iban.as_deref());
        assert_eq!(1, s.pending_skipped);
        assert_eq!(dec("-80.00"), s.bookings[0].amount);
    }

    #[test]
    fn english_export_with_debit_and_credit_columns_and_multiline_purpose() {
        let csv = "Date,Description,Payee,Debit,Credit,Balance\r\n\
2026-09-30,\"Card payment\nLondon\",Pret A Manger,4.50,,100.00\r\n\
2026-09-29,Salary,ACME Ltd,,\"1,500.00\",104.50\r\n";

        let s = csv::parse(csv.as_bytes(), None).unwrap();

        let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
        assert_eq!(vec![dec("-4.50"), dec("1500.00")], amounts);
        assert_eq!(Some("Card payment London"), s.bookings[0].purpose.as_deref());
        assert_eq!(Some("Pret A Manger"), s.bookings[0].partner_name.as_deref());
    }

    #[test]
    fn unknown_layouts_report_what_is_missing_and_accept_a_manual_mapping() {
        let csv = "When;What;How much\n03.10.2026;Coffee;-3,20\n";
        let detected = csv::parse(csv.as_bytes(), None).unwrap();
        assert!(detected.bookings.is_empty());
        assert!(!detected.csv.as_ref().unwrap().mapping.is_complete());

        let manual = CsvMapping {
            date: Some(0),
            purpose: Some(1),
            amount: Some(2),
            ..Default::default()
        };
        let manual = csv::parse(csv.as_bytes(), Some(&manual)).unwrap();
        assert_eq!(dec("-3.20"), manual.bookings[0].amount);
    }

    #[test]
    fn bad_rows_are_skipped_with_a_reason() {
        let csv =
            "Buchungstag;Betrag;Verwendungszweck\n03.10.2026;-1,00;ok\nkein Datum;-2,00;bad\n04.10.2026;abc;bad\n";
        let s = csv::parse(csv.as_bytes(), None).unwrap();
        assert_eq!(1, s.bookings.len());
        assert_eq!(2, s.skipped.len());
        assert!(s.skipped[0].starts_with("Line 3"), "{}", s.skipped[0]);
    }
}

mod camt_parser {
    use super::*;

    fn camt(entries: &str) -> String {
        format!(
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\">
  <BkToCstmrStmt><Stmt>
    <Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct>
    {entries}
  </Stmt></BkToCstmrStmt>
</Document>"
        )
    }

    fn debit_entry() -> &'static str {
        "<Ntry>
  <Amt Ccy=\"EUR\">45.67</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts>
  <BookgDt><Dt>2026-10-03</Dt></BookgDt><ValDt><Dt>2026-10-03</Dt></ValDt>
  <AcctSvcrRef>REF-1</AcctSvcrRef>
  <NtryDtls><TxDtls>
    <RltdPties><Cdtr><Nm>REWE Markt GmbH</Nm></Cdtr><CdtrAcct><Id><IBAN>DE44500105175407324931</IBAN></Id></CdtrAcct></RltdPties>
    <RmtInf><Ustrd>REWE SAGT DANKE</Ustrd><Ustrd>1234</Ustrd></RmtInf>
  </TxDtls></NtryDtls>
</Ntry>"
    }

    #[test]
    fn reads_booked_entries_with_parties_purpose_and_reference() {
        let credit = "<Ntry><Amt Ccy=\"EUR\">3200.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-10-01</Dt></BookgDt>
  <NtryDtls><TxDtls><RltdPties><Dbtr><Nm>Employer GmbH</Nm></Dbtr><Cdtr><Nm>Me</Nm></Cdtr></RltdPties><RmtInf><Ustrd>Gehalt</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry>
<Ntry><Amt Ccy=\"EUR\">60.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>PDNG</Sts><BookgDt><Dt>2026-10-04</Dt></BookgDt></Ntry>";
        let xml = camt(&format!("{}{}", debit_entry(), credit));
        let s = camt::parse(xml.as_bytes()).unwrap();

        assert_eq!(Some("DE89370400440532013000"), s.account_iban.as_deref());
        assert_eq!(2, s.bookings.len(), "pending entry skipped");
        let rewe = &s.bookings[0];
        assert_eq!(dec("-45.67"), rewe.amount);
        assert_eq!(Some("REWE Markt GmbH"), rewe.partner_name.as_deref());
        assert_eq!(Some("DE44500105175407324931"), rewe.partner_iban.as_deref());
        assert_eq!(Some("REWE SAGT DANKE 1234"), rewe.purpose.as_deref());
        assert_eq!(Some("REF-1"), rewe.bank_reference.as_deref());
        assert_eq!(Some("Employer GmbH"), s.bookings[1].partner_name.as_deref());
    }

    #[test]
    fn batch_bookings_are_split_into_their_transactions() {
        let batch = "<Ntry><Amt Ccy=\"EUR\">30.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-10-02</Dt></BookgDt><AcctSvcrRef>B1</AcctSvcrRef>
  <NtryDtls>
    <TxDtls><Amt Ccy=\"EUR\">10.00</Amt><RltdPties><Cdtr><Nm>A</Nm></Cdtr></RltdPties></TxDtls>
    <TxDtls><Amt Ccy=\"EUR\">20.00</Amt><RltdPties><Cdtr><Nm>B</Nm></Cdtr></RltdPties></TxDtls>
  </NtryDtls></Ntry>";
        let xml = camt(batch);
        let s = camt::parse(xml.as_bytes()).unwrap();
        let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
        assert_eq!(vec![dec("-10.00"), dec("-20.00")], amounts);
        let refs: Vec<&str> = s.bookings.iter().filter_map(|b| b.bank_reference.as_deref()).collect();
        assert_eq!(vec!["B1/0", "B1/1"], refs);
    }

    #[test]
    fn zip_of_statements_and_xxe_protection() {
        let xml = camt(debit_entry());
        let zip = write_zip(&[("2026-10.xml", xml.as_bytes()), ("readme.txt", b"ignore".as_slice())]);
        assert!(camt::looks_like_camt(&zip));
        assert_eq!(1, camt::parse(&zip).unwrap().bookings.len());

        let xxe = "<?xml version=\"1.0\"?><!DOCTYPE d [<!ENTITY x SYSTEM \"file:///etc/passwd\">]><Document><BkToCstmrStmt>&x;</BkToCstmrStmt></Document>";
        assert!(camt::parse(xxe.as_bytes()).is_err());
    }
}
