//! Reads bank statement files: CSV, CAMT.052/053, MT940, OFX, QIF and XLSX.

pub mod camt;
pub mod csv;
pub mod model;
pub mod mt940;
pub mod ofx;
pub mod qif;
pub mod two_digit_year;
pub mod xlsx;

mod util;

pub use model::{
    ClosingBalance, CsvLayout, CsvMapping, ImportError, ImportedBooking, ParsedStatement, StatementFormat,
};

use std::path::Path;

use time::Date;

/// The largest statement file Finglass reads.
pub const MAX_FILE_BYTES: u64 = 100 * 1024 * 1024;

/// Reads and parses a file. `csv_mapping` overrides the detected CSV column mapping.
pub fn read(
    file_name: &str,
    bytes: &[u8],
    csv_mapping: Option<&CsvMapping>,
    today: Date,
) -> Result<ParsedStatement, ImportError> {
    if bytes.len() as u64 > MAX_FILE_BYTES {
        return Err(ImportError::FileTooLarge);
    }
    let extension = Path::new(file_name)
        .extension()
        .and_then(|extension| extension.to_str())
        .unwrap_or("")
        .to_lowercase();
    let statement = match extension.as_str() {
        "xml" | "zip" => camt::parse(bytes)?,
        "ofx" | "qfx" => ofx::parse(bytes)?,
        "qif" => qif::parse_with_today(bytes, today)?,
        "sta" | "mt940" => mt940::parse_with_today(bytes, today)?,
        "xlsx" => csv::parse_rows(
            &xlsx::first_sheet_rows(bytes)?,
            csv_mapping,
            StatementFormat::Xlsx,
            "XLSX",
            ';',
            today,
        ),
        "csv" => csv::parse_with_today(bytes, csv_mapping, today)?,
        _ if mt940::looks_like(bytes) => mt940::parse_with_today(bytes, today)?,
        _ if camt::looks_like_camt(bytes) => camt::parse(bytes)?,
        _ if ofx::looks_like(bytes) => ofx::parse(bytes)?,
        _ if qif::looks_like(bytes) => qif::parse_with_today(bytes, today)?,
        _ => csv::parse_with_today(bytes, csv_mapping, today)?,
    };
    require_single_account(&statement)?;
    Ok(statement)
}

fn require_single_account(statement: &ParsedStatement) -> Result<(), ImportError> {
    if statement.accounts.len() > 1 {
        let accounts = statement
            .accounts
            .iter()
            .map(|account| {
                let tail: String = account
                    .chars()
                    .rev()
                    .take(4)
                    .collect::<String>()
                    .chars()
                    .rev()
                    .collect();
                format!("…{tail}")
            })
            .collect::<Vec<_>>()
            .join(", ");
        return Err(ImportError::invalid(format!(
            "This file holds bookings for {} accounts ({}). Export one file per account and import them one at a time.",
            statement.accounts.len(),
            accounts
        )));
    }
    Ok(())
}
