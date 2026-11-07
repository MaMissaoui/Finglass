//! Ported parser-level tests from FinDesk's `CsvOwnExportReimportTest.kt`.
//!
//! The exporter (`ReportService`) is not part of this port, so the round-trip tests build the same
//! CSV text inline that FinDesk's export writes for EN, DE and FR.
//! No tests from this file are skipped.

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{CsvMapping, csv};
use rust_decimal::Decimal;
use support::dec;

struct Header {
    date: &'static str,
    partner: &'static str,
    purpose: &'static str,
    category: &'static str,
    amount: &'static str,
    currency: &'static str,
    account: &'static str,
    balance: &'static str,
    shop: &'static str,
}

const LANGUAGES: [Header; 3] = [
    Header {
        date: "Date",
        partner: "Partner",
        purpose: "Purpose",
        category: "Category",
        amount: "Amount",
        currency: "Currency",
        account: "Account",
        balance: "Balance",
        shop: "Shop",
    },
    Header {
        date: "Datum",
        partner: "Partner",
        purpose: "Verwendungszweck",
        category: "Kategorie",
        amount: "Betrag",
        currency: "Währung",
        account: "Konto",
        balance: "Saldo",
        shop: "Händler",
    },
    Header {
        date: "Date",
        partner: "Partenaire",
        purpose: "Libellé",
        category: "Catégorie",
        amount: "Montant",
        currency: "Devise",
        account: "Compte",
        balance: "Solde",
        shop: "Marchand",
    },
];

fn escape(field: &str) -> String {
    if field.chars().any(|c| c == ',' || c == '"' || c == '\n' || c == '\r') {
        format!("\"{}\"", field.replace('"', "\"\""))
    } else {
        field.to_owned()
    }
}

fn guard(field: &str) -> String {
    if field.chars().next().is_some_and(|first| "=+-@\t\r".contains(first)) {
        format!("'{field}")
    } else {
        field.to_owned()
    }
}

fn row(fields: &[String]) -> String {
    let mut line = fields.iter().map(|field| escape(field)).collect::<Vec<_>>().join(",");
    line.push_str("\r\n");
    line
}

fn export(language: &Header, transactions: &[(i32, &str, &str, &str, &str)], balances: Option<&[Decimal]>) -> String {
    let mut header = vec![
        language.date,
        language.partner,
        language.purpose,
        language.category,
        language.amount,
        language.currency,
        language.account,
    ];
    if balances.is_some() {
        header.push(language.balance);
    }
    header.push(language.shop);
    let mut text = row(&header.iter().map(|value| (*value).to_owned()).collect::<Vec<_>>());
    for (index, (_, date, amount, partner, purpose)) in transactions.iter().enumerate() {
        let mut fields = vec![
            (*date).to_owned(),
            guard(partner),
            guard(purpose),
            String::new(),
            (*amount).to_owned(),
            "EUR".to_owned(),
            "DE89370400440532013000".to_owned(),
        ];
        if let Some(balances) = balances {
            fields.push(balances[index].to_string());
        }
        fields.push(String::new());
        text.push_str(&row(&fields));
    }
    text
}

#[test]
fn own_export_round_trips_in_every_language() {
    let bookings = [
        (1, "2026-10-01", "2600.00", "Beispiel AG", "Gehalt Oktober"),
        (2, "2026-10-02", "-42.50", "Beispiel Markt", "Einkauf 4711"),
        (
            3,
            "2026-10-03",
            "-7.25",
            "Beispiel Laden",
            "Einkauf \"Spezial\", Teil 2",
        ),
    ];
    for language in &LANGUAGES {
        let text = export(language, &bookings, None);
        let parsed = csv::parse(text.as_bytes(), None).unwrap();

        assert_eq!(3, parsed.bookings.len(), "{:?}", parsed.skipped);
        assert!(parsed.skipped.is_empty(), "{:?}", parsed.skipped);
        for ((_, date, amount, partner, purpose), reimported) in bookings.iter().zip(parsed.bookings.iter()) {
            assert_eq!(date, &reimported.date.to_string());
            assert_eq!(dec(amount), reimported.amount);
            assert_eq!(Some("EUR"), reimported.currency.as_deref());
            assert_eq!(Some(*partner), reimported.partner_name.as_deref());
            assert_eq!(Some(*purpose), reimported.purpose.as_deref());
        }
    }
}

#[test]
fn own_export_round_trips_the_balance_column_in_every_language() {
    let bookings = [
        (1, "2026-10-01", "2600.00", "Beispiel AG", "Gehalt Oktober"),
        (2, "2026-10-02", "-42.50", "Beispiel Markt", "Einkauf 4711"),
        (
            3,
            "2026-10-03",
            "-7.25",
            "Beispiel Laden",
            "Einkauf \"Spezial\", Teil 2",
        ),
    ];
    let balances = [dec("2600.00"), dec("2557.50"), dec("2550.25")];
    for language in &LANGUAGES {
        let text = export(language, &bookings, Some(&balances));
        let parsed = csv::parse(text.as_bytes(), None).unwrap();

        assert_eq!(3, parsed.bookings.len(), "{:?}", parsed.skipped);
        for (index, reimported) in parsed.bookings.iter().enumerate() {
            assert_eq!(Some(balances[index]), reimported.balance_after);
        }
    }
}

#[test]
fn own_export_round_trips_text_that_the_spreadsheet_guard_prefixes() {
    let guarded = [
        (4, "2026-10-04", "-1.00", "+Shop", "- Rate 3/12"),
        (5, "2026-10-05", "-2.00", "@home", "=SUM(A1)"),
    ];
    let text = export(&LANGUAGES[0], &guarded, None);
    let parsed = csv::parse(text.as_bytes(), None).unwrap();

    assert_eq!(2, parsed.bookings.len(), "{:?}", parsed.skipped);
    for ((_, _, _, partner, purpose), reimported) in guarded.iter().zip(parsed.bookings.iter()) {
        assert_eq!(Some(*partner), reimported.partner_name.as_deref());
        assert_eq!(Some(*purpose), reimported.purpose.as_deref());
    }
}

#[test]
fn only_the_spreadsheet_guard_apostrophe_is_removed_from_text() {
    let csv = "Datum;Betrag;Partner;Verwendungszweck\n03.10.2026;-1,00;'abc;'abc\n";

    let parsed = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(1, parsed.bookings.len());
    assert_eq!(Some("'abc"), parsed.bookings[0].partner_name.as_deref());
    assert_eq!(Some("'abc"), parsed.bookings[0].purpose.as_deref());
}

#[test]
fn bank_headers_keep_their_mapping() {
    assert_eq!(
        CsvMapping {
            date: Some(0),
            value_date: Some(1),
            amount: Some(8),
            partner: Some(4),
            payer: Some(3),
            partner_iban: Some(7),
            purpose: Some(5),
            ..Default::default()
        },
        csv::detect_mapping(&[
            "Buchungsdatum".to_owned(),
            "Wertstellung".to_owned(),
            "Status".to_owned(),
            "Zahlungspflichtige*r".to_owned(),
            "Zahlungsempfänger*in".to_owned(),
            "Verwendungszweck".to_owned(),
            "Umsatztyp".to_owned(),
            "IBAN".to_owned(),
            "Betrag (€)".to_owned(),
            "Gläubiger-ID".to_owned(),
            "Mandatsreferenz".to_owned(),
            "Kundenreferenz".to_owned(),
        ])
    );
    assert_eq!(
        CsvMapping {
            date: Some(0),
            amount: Some(4),
            partner: Some(1),
            purpose: Some(3),
            ..Default::default()
        },
        csv::detect_mapping(&[
            "Datum".to_owned(),
            "Name".to_owned(),
            "Partner".to_owned(),
            "Verwendungszweck".to_owned(),
            "Betrag".to_owned(),
        ])
    );
    assert_eq!(
        CsvMapping {
            date: Some(0),
            amount: Some(3),
            partner: Some(1),
            purpose: Some(2),
            currency: Some(4),
            ..Default::default()
        },
        csv::detect_mapping(&[
            "Date".to_owned(),
            "Payee".to_owned(),
            "Purpose".to_owned(),
            "Amount".to_owned(),
            "Currency".to_owned(),
            "Partner".to_owned(),
        ])
    );
}

#[test]
fn fallbacks_are_exact_match_only() {
    assert_eq!(
        None,
        csv::detect_mapping(&["Datum".to_owned(), "Partnerbank".to_owned(), "Betrag".to_owned(),]).partner
    );
    assert_eq!(
        Some(3),
        csv::detect_mapping(&[
            "Date".to_owned(),
            "Montants".to_owned(),
            "Libellé".to_owned(),
            "Amount".to_owned(),
        ])
        .amount
    );
}

#[test]
fn bank_norwegian_rule_still_clears_the_currency() {
    let mapping = csv::detect_mapping(&[
        "BookDate".to_owned(),
        "Amount".to_owned(),
        "Currency Amount".to_owned(),
        "Currency".to_owned(),
        "Devise".to_owned(),
    ]);
    assert_eq!(None, mapping.currency);
}
