//! The tax report PDF, ported from FinDesk's `document/PdfReportExporter.kt`.

use std::fs;
use std::io;
use std::path::Path;

use time::Date;

use crate::export::pdf::{PdfBuilder, PdfFont};
use crate::export::xlsx::write_atomically;
use crate::i18n::{self, Arg, Key, Language};

use super::format::{date, money, month_name};
use super::service::{TaxReport, display_description};

/// Renders a [`TaxReport`] as a simple multi-page A4 PDF.
pub struct PdfReportExporter;

impl PdfReportExporter {
    /// Builds the PDF in memory.
    pub fn build_pdf(&self, report: &TaxReport, today: Date, language: Language) -> Vec<u8> {
        let mut writer = PdfBuilder::new();
        let title = if report.is_calendar_year() {
            i18n::format(language, Key::ReportTitleTax, &[Arg::from(report.year)])
        } else {
            i18n::format(language, Key::ReportTitleRange, &[Arg::from(report.title(language))])
        };
        writer.text(&title, PdfFont::Bold, 18.0);
        writer.text(
            &i18n::format(language, Key::PdfGenerated, &[Arg::from(date(today))]),
            PdfFont::Regular,
            9.0,
        );
        writer.gap(12.0);

        writer.heading(i18n::text(language, Key::PdfSummary));
        writer.row(
            i18n::text(language, Key::PdfTotalIncome),
            &money(report.total_income, language, false),
            PdfFont::Regular,
            10.0,
        );
        writer.row(
            i18n::text(language, Key::PdfTotalExpenses),
            &money(report.total_expenses, language, false),
            PdfFont::Regular,
            10.0,
        );
        writer.row(
            i18n::text(language, Key::PdfNetIncome),
            &money(report.net_income, language, false),
            PdfFont::Regular,
            10.0,
        );
        writer.row(
            i18n::text(language, Key::PdfTaxRelevantExpenses),
            &money(report.tax_relevant_total(), language, false),
            PdfFont::Regular,
            10.0,
        );
        writer.row(
            i18n::text(language, Key::PdfTransactionCount),
            &report.transaction_count.to_string(),
            PdfFont::Regular,
            10.0,
        );
        if report.other_currency_bookings > 0 {
            writer.text(
                &i18n::format(
                    language,
                    Key::PdfAllAmountsIn,
                    &[
                        Arg::from(report.currency.as_str()),
                        Arg::from(report.other_currency_bookings),
                    ],
                ),
                PdfFont::Regular,
                9.0,
            );
        }
        writer.gap(10.0);

        writer.heading(i18n::text(language, Key::PdfTaxByCategory));
        if report.tax_relevant_expenses.is_empty() {
            writer.text(i18n::text(language, Key::PdfNoTaxRelevant), PdfFont::Regular, 10.0);
        }
        for (category, amount) in &report.tax_relevant_expenses {
            writer.row(category, &money(*amount, language, false), PdfFont::Regular, 10.0);
        }
        writer.gap(10.0);

        writer.heading(i18n::text(language, Key::PdfMonthlyBreakdown));
        writer.row(
            i18n::text(language, Key::PdfMonth),
            i18n::text(language, Key::PdfIncomeExpensesNet),
            PdfFont::Bold,
            10.0,
        );
        for month in &report.monthly_breakdown {
            let name = if report.is_calendar_year() {
                month_name(language, month.month).to_owned()
            } else {
                format!("{} {}", month_name(language, month.month), month.year)
            };
            writer.row(
                &name,
                &format!(
                    "{} / {} / {}",
                    money(month.income, language, false),
                    money(month.expenses, language, false),
                    money(month.net(), language, false)
                ),
                PdfFont::Regular,
                10.0,
            );
        }
        writer.gap(10.0);

        writer.heading(i18n::text(language, Key::PdfExpensesByCategory));
        for (category, amount) in &report.expenses_by_category {
            writer.row(category, &money(*amount, language, false), PdfFont::Regular, 10.0);
        }

        if !report.tax_relevant_transactions.is_empty() {
            writer.gap(10.0);
            writer.heading(i18n::text(language, Key::PdfTaxTransactions));
            let mut transactions = report.tax_relevant_transactions.clone();
            transactions.sort_by_key(|left| left.date);
            for transaction in &transactions {
                let description: String = display_description(transaction).chars().take(55).collect();
                writer.row(
                    &format!(
                        "{}  {}  ({})",
                        date(transaction.date),
                        description,
                        transaction.category_name.as_deref().unwrap_or_default()
                    ),
                    &money(
                        crate::money::Money {
                            minor: i64::try_from(transaction.amount.minor.unsigned_abs()).unwrap_or(i64::MAX),
                            currency: transaction.currency,
                        },
                        language,
                        false,
                    ),
                    PdfFont::Regular,
                    10.0,
                );
            }
        }

        writer.finish()
    }

    /// Writes the report to a file through a temp file next to the target.
    pub fn export(&self, report: &TaxReport, file: &Path, today: Date, language: Language) -> io::Result<()> {
        let bytes = self.build_pdf(report, today, language);
        write_atomically(file, |temp| fs::write(temp, &bytes))
    }
}
