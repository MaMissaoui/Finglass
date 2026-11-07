//! Ported parser-level tests from FinDesk's `ImportLimitationsTest.kt`.
//!
//! No tests from this file are skipped.

#[path = "import_support.rs"]
mod support;

use finglass_core::import::{csv, mt940, qif, two_digit_year};
use support::dec;
use time::macros::date;

fn today() -> time::Date {
    date!(2026 - 11 - 01)
}

#[test]
fn two_digit_years_resolve_to_19xx_when_20xx_would_be_more_than_a_year_ahead() {
    assert_eq!(1999, two_digit_year::resolve(99, today()).unwrap());
    assert_eq!(1928, two_digit_year::resolve(28, today()).unwrap());
    assert_eq!(2027, two_digit_year::resolve(27, today()).unwrap());
    assert_eq!(2026, two_digit_year::resolve(26, today()).unwrap());
    assert_eq!(2000, two_digit_year::resolve(0, today()).unwrap());

    assert_eq!(2099, two_digit_year::resolve(99, date!(2099 - 06 - 01)).unwrap());
    assert_eq!(2000, two_digit_year::resolve(0, date!(2099 - 06 - 01)).unwrap());
}

#[test]
fn two_digit_year_rejects_out_of_range_input() {
    assert!(two_digit_year::resolve(100, today()).is_err());
    assert!(two_digit_year::resolve(255, today()).is_err());
}

#[test]
fn mt940_value_date_with_a_year_before_1928_resolves_to_19xx() {
    let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C991231EUR0,00
:61:991231D45,67N066NONREF
:86:?20Old booking
:62F:C991231EUR0,00";

    let s = mt940::parse_with_today(mt940.as_bytes(), today()).unwrap();

    assert_eq!(1, s.bookings.len());
    assert_eq!(date!(1999 - 12 - 31), s.bookings[0].date);
    assert_eq!(Some(date!(1999 - 12 - 31)), s.bookings[0].value_date);
    assert_eq!(dec("-45.67"), s.bookings[0].amount);
}

#[test]
fn mt940_value_date_with_a_recent_two_digit_year_stays_20xx() {
    let mt940 = ":20:STARTUMS
:25:DE89370400440532013000
:28C:1
:60F:C260115EUR0,00
:61:260115D45,67N066NONREF
:86:?20Recent booking
:62F:C260115EUR0,00";

    let s = mt940::parse_with_today(mt940.as_bytes(), today()).unwrap();

    assert_eq!(date!(2026 - 01 - 15), s.bookings[0].date);
    assert_eq!(Some(date!(2026 - 01 - 15)), s.bookings[0].value_date);
}

#[test]
fn qif_two_digit_years_follow_the_same_rule() {
    let qif = "!Type:Bank
D12/31/99
T-10.00
POld
^
D01/15/26
T-20.00
PRecent
^";

    let s = qif::parse_with_today(qif.as_bytes(), today()).unwrap();

    let dates: Vec<_> = s.bookings.iter().map(|b| b.date).collect();
    assert_eq!(vec![date!(1999 - 12 - 31), date!(2026 - 01 - 15)], dates);
}

#[test]
fn csv_dates_are_parsed_strictly() {
    assert_eq!(None, csv::parse_date_with_today(Some("31.02.2026"), today()));
    assert_eq!(
        Some(date!(2028 - 02 - 29)),
        csv::parse_date_with_today(Some("29.02.2028"), today())
    );
    assert_eq!(None, csv::parse_date_with_today(Some("29.02.2027"), today()));
    assert_eq!(
        Some(date!(1999 - 12 - 31)),
        csv::parse_date_with_today(Some("31.12.99"), today())
    );
    assert_eq!(
        Some(date!(2026 - 01 - 15)),
        csv::parse_date_with_today(Some("15.01.26"), today())
    );
    assert_eq!(None, csv::parse_date_with_today(Some("2026-02-30"), today()));
    assert_eq!(
        Some(date!(2026 - 01 - 15)),
        csv::parse_date_with_today(Some("15.01.2026"), today())
    );
}

#[test]
fn csv_dates_with_a_time_and_a_one_digit_day_or_month_are_parsed() {
    assert_eq!(
        Some(date!(2026 - 10 - 01)),
        csv::parse_date_with_today(Some("1.10.2026 12:00"), today())
    );
    assert_eq!(
        Some(date!(2026 - 10 - 01)),
        csv::parse_date_with_today(Some("01.10.2026 12:00"), today())
    );
    assert_eq!(
        Some(date!(2026 - 10 - 01)),
        csv::parse_date_with_today(Some("2026-10-01T12:00:00"), today())
    );
    assert_eq!(
        Some(date!(2026 - 10 - 01)),
        csv::parse_date_with_today(Some("1.10.2026"), today())
    );
    assert_eq!(None, csv::parse_date_with_today(Some("31.02.2026 12:00"), today()));
}

#[test]
fn csv_two_digit_years_follow_two_digit_year_at_parse_time() {
    assert_eq!(
        Some(date!(1999 - 12 - 31)),
        csv::parse_date_with_today(Some("31.12.99"), today())
    );
    assert_eq!(
        Some(date!(2026 - 01 - 15)),
        csv::parse_date_with_today(Some("15.01.26"), today())
    );
    assert_eq!(
        Some(date!(1928 - 02 - 29)),
        csv::parse_date_with_today(Some("29.02.28"), today())
    );
    assert_eq!(None, csv::parse_date_with_today(Some("29.02.27"), today()));
    assert_eq!(None, csv::parse_date_with_today(Some("31.02.26"), today()));
}

#[test]
fn csv_row_with_an_impossible_date_is_skipped_on_its_own_line() {
    let csv = "Buchungstag;Betrag;Verwendungszweck\n\
03.10.2026;-45,67;ok\n\
31.02.2026;-10,00;impossible\n\
04.10.2026;-5,00;also ok\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(2, s.bookings.len());
    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(vec![dec("-45.67"), dec("-5.00")], amounts);
    assert_eq!(1, s.skipped.len());
    assert!(s.skipped[0].starts_with("Line 3"), "{}", s.skipped[0]);
    assert!(s.skipped[0].contains("no valid date"), "{}", s.skipped[0]);
}

#[test]
fn tab_file_whose_fields_contain_commas_is_not_read_as_comma_separated() {
    let csv = "Buchungstag\tBetrag, EUR\tVerwendungszweck\n\
03.10.2026\t-45,67\tMiete, Oktober\n\
04.10.2026\t-10,00\tBrot, Butter\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!('\t', s.csv.as_ref().unwrap().delimiter);
    assert_eq!(2, s.bookings.len());
    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(vec![dec("-45.67"), dec("-10.00")], amounts);
    let purposes: Vec<_> = s.bookings.iter().filter_map(|b| b.purpose.as_deref()).collect();
    assert_eq!(vec!["Miete, Oktober", "Brot, Butter"], purposes);
}

#[test]
fn a_tab_and_comma_tie_goes_to_the_delimiter_with_a_recognisable_header() {
    let csv = "Buchungstag\tBetrag, EUR\n\
03.10.2026\t-45,67\n\
04.10.2026\t-10,00\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!('\t', s.csv.as_ref().unwrap().delimiter);
    let amounts: Vec<_> = s.bookings.iter().map(|b| b.amount).collect();
    assert_eq!(vec![dec("-45.67"), dec("-10.00")], amounts);
}

#[test]
fn quoted_semicolons_of_varying_number_do_not_hand_the_file_to_the_comma() {
    let csv = "Datum;Betrag;Text\n\
03.10.2026;-45,67;\"a;b\"\n\
04.10.2026;-10,00;\"a;b;c\"\n\
05.10.2026;-1,50;plain\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(';', s.csv.as_ref().unwrap().delimiter);
    let purposes: Vec<_> = s.bookings.iter().filter_map(|b| b.purpose.as_deref()).collect();
    assert_eq!(vec!["a;b", "a;b;c", "plain"], purposes);
}

#[test]
fn semicolon_file_with_quoted_commas_and_semicolons_keeps_its_delimiter() {
    let csv = "\"Datum\";\"Betrag\";\"Text\"\n\
\"03.10.2026\";\"-45,67\";\"a;b, c\"\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(';', s.csv.as_ref().unwrap().delimiter);
    assert_eq!(1, s.bookings.len());
    assert_eq!(Some("a;b, c"), s.bookings[0].purpose.as_deref());
    assert_eq!(dec("-45.67"), s.bookings[0].amount);
}

#[test]
fn comma_file_with_a_quoted_multi_line_purpose_imports_the_right_fields() {
    let csv = "Date,Description,Payee,Debit,Credit,Balance\r\n\
2026-09-30,\"Card payment\nLondon\",Pret A Manger,4.50,,100.00\r\n\
2026-09-29,Salary,ACME Ltd,,\"1,500.00\",104.50\r\n";

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(',', s.csv.as_ref().unwrap().delimiter);
    assert_eq!(2, s.bookings.len());
    assert_eq!(dec("-4.50"), s.bookings[0].amount);
    assert_eq!(Some("Pret A Manger"), s.bookings[0].partner_name.as_deref());
    assert_eq!(Some("Card payment London"), s.bookings[0].purpose.as_deref());
    assert_eq!(dec("1500.00"), s.bookings[1].amount);
    assert_eq!(Some("ACME Ltd"), s.bookings[1].partner_name.as_deref());
}

#[test]
fn csv_splitting_stops_after_the_requested_non_blank_records_and_keeps_blank_ones_before() {
    let text = "a;b\n\nc;d\ne;f\n";

    let records = csv::split_records(text, ';', 2);

    assert_eq!(3, records.len());
    assert_eq!(vec![String::new()], records[1]);
    let non_blank: Vec<&Vec<String>> = records
        .iter()
        .filter(|row| row.iter().any(|value| !value.trim().is_empty()))
        .collect();
    assert_eq!(
        vec![
            &vec!["a".to_owned(), "b".to_owned()],
            &vec!["c".to_owned(), "d".to_owned()]
        ],
        non_blank
    );
}

#[test]
fn a_file_of_5000_booking_rows_still_imports_all_of_them() {
    let mut csv = String::from("Buchungstag;Betrag;Verwendungszweck\n");
    for index in 0..5000 {
        csv.push_str(&format!("03.10.2026;-1,00;Row {index}\n"));
    }

    let s = csv::parse(csv.as_bytes(), None).unwrap();

    assert_eq!(5000, s.bookings.len());
}
