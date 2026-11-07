//! Booking hashes, ported from FinDesk's `FinTSClient.transactionHash`/`importHash` and
//! `StatementImportService.hashes`.

use std::collections::HashMap;

use rust_decimal::Decimal;
use sha2::{Digest, Sha256};
use time::Date;

use super::model::ImportedBooking;

/// Formats a date as the ISO text FinDesk's hash uses.
pub fn iso_date(date: Date) -> String {
    format!("{:04}-{:02}-{:02}", date.year(), date.month() as u8, date.day())
}

/// Formats an amount like Java's `stripTrailingZeros().toPlainString()`.
pub fn plain_amount(amount: Decimal) -> String {
    amount.normalize().to_string()
}

/// SHA-256 over the fields that identify a booking; used for duplicate detection.
pub fn transaction_hash(date: Date, amount: Decimal, partner: Option<&str>, purpose: Option<&str>) -> String {
    let input = format!(
        "{}|{}|{}|{}",
        iso_date(date),
        plain_amount(amount),
        partner.unwrap_or(""),
        purpose.unwrap_or("")
    );
    let digest = Sha256::digest(input.as_bytes());
    let mut hash = String::with_capacity(digest.len() * 2);
    for byte in digest {
        use std::fmt::Write as _;
        let _ = write!(hash, "{byte:02x}");
    }
    hash
}

/// Dedup hashes for one file.
///
/// Identifies a booking by its fields plus the bank reference or running balance; without either, by
/// its position among identical bookings that day, so genuine repeats (two coffees) are kept while
/// re-importing the same or an overlapping export is recognised.
pub fn hashes(iban: &str, bookings: &[ImportedBooking]) -> Vec<String> {
    let mut seen: HashMap<String, usize> = HashMap::new();

    bookings
        .iter()
        .map(|booking| {
            let key = format!(
                "{}|{}|{}|{}",
                iso_date(booking.date),
                plain_amount(booking.amount),
                booking.partner_name.as_deref().unwrap_or(""),
                booking.purpose.as_deref().unwrap_or("")
            );
            let discriminator = if let Some(reference) = &booking.bank_reference {
                reference.clone()
            } else if let Some(balance) = booking.balance_after {
                plain_amount(balance)
            } else {
                let count = seen.entry(key).or_insert(0);
                *count += 1;
                format!("occ{count}")
            };
            let purpose = format!(
                "{}#{}#file:{}",
                booking.purpose.as_deref().unwrap_or(""),
                iban,
                discriminator
            );
            transaction_hash(
                booking.date,
                booking.amount,
                booking.partner_name.as_deref(),
                Some(&purpose),
            )
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use time::macros::date;

    fn booking(date: Date, amount: &str) -> ImportedBooking {
        ImportedBooking {
            date,
            value_date: None,
            amount: amount.parse().unwrap(),
            currency: Some("EUR".to_owned()),
            partner_name: None,
            partner_iban: None,
            purpose: None,
            balance_after: None,
            bank_reference: None,
        }
    }

    #[test]
    fn strip_trailing_zeros_to_plain_string_matches_java() {
        for (value, expected) in [
            ("10.50", "10.5"),
            ("100.00", "100"),
            ("-0.10", "-0.1"),
            ("0.00", "0"),
            ("1E+2", "100"),
            ("12.345", "12.345"),
            // Checked against java.math.BigDecimal by the reviewer.
            ("-0.00", "0"),
            ("1000", "1000"),
            ("0.000", "0"),
            ("123456789.10", "123456789.1"),
        ] {
            assert_eq!(plain_amount(value.parse().unwrap()), expected, "{value}");
        }
    }

    #[test]
    fn known_answer_hashes() {
        // Expected values computed once with:
        //   python3 -c 'import hashlib; print(hashlib.sha256("...".encode()).hexdigest())'
        let cases = [
            (
                date!(2026 - 10 - 03),
                "10.50",
                Some("Bäcker"),
                Some("Kaffee"),
                "def58a318d1e3580b1e1c6b56d9c4e12ceede08565a636e9195c8baf1e28318e",
            ),
            (
                date!(2026 - 10 - 01),
                "3200.00",
                Some("Müller & Söhne GmbH"),
                Some("Gehalt Oktober"),
                "bbe5a24b9c15d00906d8519c4a18b3aa5685d643701e23eb6e53f337e00f4edb",
            ),
            (
                date!(2026 - 10 - 03),
                "-0.10",
                None,
                Some(""),
                "229787067b4d87a6bdd9f7e3c7bb02d0967548b2c537b67423fed703f731c32f",
            ),
            (
                date!(2026 - 01 - 01),
                "0.00",
                Some("Zero"),
                Some("Nothing"),
                "eedd6a1d00bbc92171407ef48ac6ae47fd4d58c48931ffb186dda4e0df3acaed",
            ),
            (
                date!(2025 - 12 - 31),
                "-89.99",
                Some("AMAZON EU"),
                Some("Bestellung 303-123; Monitor"),
                "08c886b9765918612094084a60461a6b3bf2f20ebed58124f0a5ee4a3392761d",
            ),
        ];

        for (day, amount, partner, purpose, expected) in cases {
            assert_eq!(
                transaction_hash(day, amount.parse().unwrap(), partner, purpose),
                expected,
                "input: {day}|{amount}|{partner:?}|{purpose:?}"
            );
        }
    }

    #[test]
    fn the_transaction_hash_is_stable_and_changes_with_the_input() {
        let day = date!(2026 - 10 - 01);
        let amount = "-49.99".parse().unwrap();
        let first = transaction_hash(day, amount, Some("REWE"), Some("Shopping"));

        assert!(!first.is_empty());
        assert_eq!(
            first,
            transaction_hash(day, amount, Some("REWE"), Some("Shopping")),
            "same input, same hash"
        );
        assert_ne!(
            first,
            transaction_hash(date!(2026 - 10 - 02), amount, Some("REWE"), Some("Shopping")),
            "a different date is a different booking"
        );
    }

    #[test]
    fn file_hashes_use_reference_then_balance_then_occurrence() {
        let mut with_reference = booking(date!(2026 - 10 - 03), "-4.50");
        with_reference.partner_name = Some("Bäcker".to_owned());
        with_reference.purpose = Some("Kaffee".to_owned());
        with_reference.bank_reference = Some("REF-1".to_owned());

        let mut with_balance = with_reference.clone();
        with_balance.bank_reference = None;
        with_balance.balance_after = Some("100.00".parse().unwrap());

        let mut first = with_reference.clone();
        first.bank_reference = None;
        let second = first.clone();

        let hashes = hashes("DE89370400440532013000", &[with_reference, with_balance, first, second]);

        // The reference wins, then the running balance, then the occurrences in file order.
        assert_eq!(
            hashes[0],
            transaction_hash(
                date!(2026 - 10 - 03),
                "-4.50".parse().unwrap(),
                Some("Bäcker"),
                Some("Kaffee#DE89370400440532013000#file:REF-1")
            )
        );
        assert_ne!(hashes[0], hashes[1]);
        assert_ne!(hashes[2], hashes[3]);
        assert_eq!(
            hashes[2],
            transaction_hash(
                date!(2026 - 10 - 03),
                "-4.50".parse().unwrap(),
                Some("Bäcker"),
                Some("Kaffee#DE89370400440532013000#file:occ1")
            )
        );
        assert_eq!(
            hashes[3],
            transaction_hash(
                date!(2026 - 10 - 03),
                "-4.50".parse().unwrap(),
                Some("Bäcker"),
                Some("Kaffee#DE89370400440532013000#file:occ2")
            )
        );
        assert_eq!(
            hashes[1],
            transaction_hash(
                date!(2026 - 10 - 03),
                "-4.50".parse().unwrap(),
                Some("Bäcker"),
                Some("Kaffee#DE89370400440532013000#file:100")
            )
        );
    }
}
