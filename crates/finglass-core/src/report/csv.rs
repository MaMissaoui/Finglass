//! CSV writing and the report CSV exports, ported from `ReportService.kt` and `CsvFormat`.

use std::collections::BTreeMap;
use std::fs::File;
use std::io::{self, BufWriter, Write};
use std::path::Path;

use thiserror::Error;
use time::{Date, Month};

use crate::db::Database;
use crate::i18n::{self, Key, Language};
use crate::model::{TransactionFilter, TransactionId, TransactionItem};
use crate::money::format_plain;
use crate::repo::{self, RepoError};
use crate::rules::payment_providers::PaymentProviders;

/// Everything that can go wrong while exporting a database range to CSV.
#[derive(Debug, Error)]
pub enum CsvExportError {
    /// A repository read failed.
    #[error(transparent)]
    Repo(#[from] RepoError),
    /// Writing the file failed.
    #[error(transparent)]
    Io(#[from] io::Error),
}

/// RFC 4180 quoting and the spreadsheet formula guard, exactly like FinDesk's `CsvFormat`.
pub struct CsvFormat;

impl CsvFormat {
    /// Neutralises spreadsheet formula injection in free-text fields from bank data.
    pub fn text(field: &str) -> String {
        if field
            .chars()
            .next()
            .is_some_and(|first| matches!(first, '=' | '+' | '-' | '@' | '\t' | '\r'))
        {
            format!("'{field}")
        } else {
            field.to_owned()
        }
    }

    /// One CSV record with a `\r\n` line ending.
    pub fn row(fields: &[String]) -> String {
        let mut line = fields
            .iter()
            .map(|field| Self::escape(field))
            .collect::<Vec<_>>()
            .join(",");
        line.push_str("\r\n");
        line
    }

    /// Quotes a field only when it contains a comma, quote or line break.
    pub fn escape(field: &str) -> String {
        if field
            .chars()
            .any(|character| matches!(character, ',' | '"' | '\n' | '\r'))
        {
            format!("\"{}\"", field.replace('"', "\"\""))
        } else {
            field.to_owned()
        }
    }
}

/// Exports transactions as a UTF-8 CSV with a BOM.
///
/// The Shop column is always last; `balances` adds a Balance column just before it when the
/// transaction list shows a running balance. The Reports export leaves the Balance column out.
pub fn export_to_csv(
    transactions: &[TransactionItem],
    output: &Path,
    balances: Option<&BTreeMap<TransactionId, crate::money::Money>>,
    language: Language,
) -> io::Result<()> {
    let file = File::create(output)?;
    let mut writer = BufWriter::new(file);
    writer.write_all(&[0xef, 0xbb, 0xbf])?;

    let mut header = vec![
        i18n::text(language, Key::ColDate).to_owned(),
        i18n::text(language, Key::ColPartner).to_owned(),
        i18n::text(language, Key::ColPurpose).to_owned(),
        i18n::text(language, Key::ColCategory).to_owned(),
        i18n::text(language, Key::ColAmount).to_owned(),
        i18n::text(language, Key::ColCurrency).to_owned(),
        i18n::text(language, Key::ColAccount).to_owned(),
    ];
    if balances.is_some() {
        header.push(i18n::text(language, Key::ColBalance).to_owned());
    }
    header.push(i18n::text(language, Key::ColShop).to_owned());
    writer.write_all(CsvFormat::row(&header).as_bytes())?;

    for transaction in transactions {
        let mut fields = vec![
            transaction.date.to_string(),
            CsvFormat::text(transaction.partner_name.as_deref().unwrap_or_default()),
            CsvFormat::text(transaction.purpose.as_deref().unwrap_or_default()),
            CsvFormat::text(transaction.category_name.as_deref().unwrap_or_default()),
            format_plain(transaction.amount),
            CsvFormat::text(transaction.currency.as_str()),
            CsvFormat::text(transaction.account_iban.as_deref().unwrap_or_default()),
        ];
        if let Some(balances) = balances {
            fields.push(
                balances
                    .get(&transaction.id)
                    .map(|amount| format_plain(*amount))
                    .unwrap_or_default(),
            );
        }
        fields.push(CsvFormat::text(
            PaymentProviders::merchant_of(transaction.purpose.as_deref(), transaction.partner_name.as_deref())
                .as_deref()
                .unwrap_or_default(),
        ));
        writer.write_all(CsvFormat::row(&fields).as_bytes())?;
    }

    writer.flush()
}

/// Exports a whole year from the reports screen.
pub fn export_year_to_csv(db: &Database, year: i32, output: &Path, language: Language) -> Result<(), CsvExportError> {
    export_range_to_csv(
        db,
        Date::from_calendar_date(year, Month::January, 1).expect("valid date"),
        Date::from_calendar_date(year, Month::December, 31).expect("valid date"),
        output,
        language,
    )
}

/// Exports every booking in an inclusive date range.
pub fn export_range_to_csv(
    db: &Database,
    from: Date,
    to: Date,
    output: &Path,
    language: Language,
) -> Result<(), CsvExportError> {
    let transactions = repo::transactions::find_items(
        db,
        &TransactionFilter {
            start_date: Some(from),
            end_date: Some(to),
            ..Default::default()
        },
        None,
    )?;
    Ok(export_to_csv(&transactions, output, None, language)?)
}
