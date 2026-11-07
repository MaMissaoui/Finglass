//! The Tax bookings exports: an Excel workbook and a printable PDF.
//!
//! Ported from FinDesk's `document/TaxReportExporter.kt`. All strings come from `i18n`, every amount
//! is in the report's currency, and the PDF uses the same layout as the PDFBox version.

use std::fs;
use std::io;
use std::path::Path;

use time::Date;

use crate::export::pdf::{MARGIN, PAGE_WIDTH, PdfBuilder, PdfFont};
use crate::export::xlsx::write_atomically;
use crate::export::xlsx::{Cell, XlsxWriter};
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{TaxSource, TransactionItem};
use crate::rules::payment_providers::PaymentProviders;

use super::format::{date, pdf_amount};
use super::service::{TaxBookings, TaxGroup};

const GROUP_HEADER_HEIGHT: f32 = 15.0;
const TABLE_HEADER_HEIGHT: f32 = 12.0;
const ROW_HEIGHT: f32 = 14.0;

const X_DATE: f32 = 50.0;
const W_DATE: f32 = 56.0;
const X_DESC: f32 = 112.0;
const W_DESC: f32 = 232.0;
const X_ACCOUNT: f32 = 350.0;
const W_ACCOUNT: f32 = 74.0;
const AMOUNT_RIGHT_EDGE: f32 = 500.0;
const W_AMOUNT: f32 = 76.0;
const RECEIPT_RIGHT_EDGE: f32 = PAGE_WIDTH - MARGIN;
const W_RECEIPT: f32 = 40.0;

/// Exports the Tax bookings view as an Excel workbook or an A4 PDF.
pub struct TaxReportExporter;

impl TaxReportExporter {
    /// Writes the workbook: an overview sheet and a bookings sheet.
    pub fn export_xlsx(&self, bookings: &TaxBookings, file: &Path, language: Language) -> io::Result<()> {
        let mut writer = XlsxWriter::new();
        overview_sheet(&mut writer, bookings, language);
        bookings_sheet(&mut writer, bookings, language);
        writer.write(file)
    }

    /// Builds the printable report in memory.
    pub fn build_pdf(&self, bookings: &TaxBookings, today: Date, language: Language) -> Vec<u8> {
        let mut layout = PdfBuilder::new();
        layout.text(
            &i18n::format(language, Key::TaxExportTitle, &[Arg::from(bookings.title(language))]),
            PdfFont::Bold,
            18.0,
        );
        layout.text(
            &format!(
                "{}: {}",
                i18n::text(language, Key::ColCurrency),
                bookings.currency.as_str()
            ),
            PdfFont::Regular,
            10.0,
        );
        layout.text(
            &i18n::format(language, Key::PdfGenerated, &[Arg::from(date(today))]),
            PdfFont::Regular,
            9.0,
        );
        layout.gap(12.0);
        layout.row(
            i18n::text(language, Key::TaxIncomeTotal),
            &pdf_amount(bookings.income_total(), language, false),
            PdfFont::Regular,
            10.0,
        );
        layout.row(
            i18n::text(language, Key::TaxDeductionTotal),
            &pdf_amount(bookings.deduction_total(), language, false),
            PdfFont::Regular,
            10.0,
        );
        layout.row(
            i18n::text(language, Key::NetIncome),
            &pdf_amount(bookings.net_total(), language, false),
            PdfFont::Regular,
            10.0,
        );
        layout.row(
            i18n::text(language, Key::TaxReceipts),
            &i18n::format(
                language,
                Key::TaxReceiptsOf,
                &[Arg::from(bookings.with_receipt()), Arg::from(bookings.booking_count)],
            ),
            PdfFont::Regular,
            10.0,
        );
        if bookings.other_currency_bookings > 0 {
            layout.text(
                &i18n::format(
                    language,
                    Key::OtherCurrenciesExcluded,
                    &[
                        Arg::from(bookings.currency.as_str()),
                        Arg::from(bookings.other_currency_bookings),
                    ],
                ),
                PdfFont::Regular,
                9.0,
            );
        }
        layout.gap(10.0);
        section(
            &mut layout,
            i18n::text(language, Key::TaxSectionIncome),
            &bookings.income_groups,
            language,
        );
        section(
            &mut layout,
            i18n::text(language, Key::TaxSectionDeductions),
            &bookings.deduction_groups,
            language,
        );

        let page_count = layout.page_count();
        for index in 0..page_count {
            layout.add_footer(
                index,
                &i18n::format(language, Key::PdfPageOf, &[Arg::from(index + 1), Arg::from(page_count)]),
            );
        }

        layout.finish()
    }

    /// Saves the report as a PDF file, through a temp file next to the target.
    pub fn export_pdf(&self, bookings: &TaxBookings, file: &Path, today: Date, language: Language) -> io::Result<()> {
        let bytes = self.build_pdf(bookings, today, language);
        write_atomically(file, |temp| fs::write(temp, &bytes))
    }
}

fn overview_sheet(writer: &mut XlsxWriter, bookings: &TaxBookings, language: Language) {
    writer.sheet(
        i18n::text(language, Key::XlsxSheetOverview),
        vec![14.0, 34.0, 10.0, 14.0, 14.0, 14.0, 12.0],
        false,
        false,
        |sheet| {
            sheet.row(vec![Cell::bold(i18n::format(
                language,
                Key::TaxExportTitle,
                &[Arg::from(bookings.title(language))],
            ))]);
            sheet.row(vec![Cell::text(format!(
                "{}: {}",
                i18n::text(language, Key::ColCurrency),
                bookings.currency.as_str()
            ))]);
            sheet.row(vec![
                Cell::bold(i18n::text(language, Key::TaxColSection)),
                Cell::bold(i18n::text(language, Key::ColCategory)),
                Cell::bold(i18n::text(language, Key::TaxColBookings)),
                Cell::bold(i18n::text(language, Key::ColIncome)),
                Cell::bold(i18n::text(language, Key::ColExpenses)),
                Cell::bold(i18n::text(language, Key::ColNet)),
                Cell::bold(i18n::text(language, Key::TaxColWithReceipt)),
            ]);
            for group in &bookings.income_groups {
                group_row(sheet, language, Key::TaxSectionIncome, group);
            }
            for group in &bookings.deduction_groups {
                group_row(sheet, language, Key::TaxSectionDeductions, group);
            }
            sheet.row(vec![
                Cell::empty(),
                Cell::bold(i18n::text(language, Key::TaxIncomeTotal)),
                Cell::empty(),
                Cell::money(bookings.income_total()),
            ]);
            sheet.row(vec![
                Cell::empty(),
                Cell::bold(i18n::text(language, Key::TaxDeductionTotal)),
                Cell::empty(),
                Cell::empty(),
                Cell::money(bookings.deduction_total()),
            ]);
            sheet.row(vec![
                Cell::empty(),
                Cell::bold(i18n::text(language, Key::NetIncome)),
                Cell::empty(),
                Cell::empty(),
                Cell::empty(),
                Cell::money(bookings.net_total()),
            ]);
            sheet.row(vec![
                Cell::empty(),
                Cell::bold(i18n::text(language, Key::TaxReceipts)),
                Cell::text(i18n::format(
                    language,
                    Key::TaxReceiptsOf,
                    &[Arg::from(bookings.with_receipt()), Arg::from(bookings.booking_count)],
                )),
            ]);
            if bookings.other_currency_bookings > 0 {
                sheet.row(vec![Cell::text(i18n::format(
                    language,
                    Key::OtherCurrenciesExcluded,
                    &[
                        Arg::from(bookings.currency.as_str()),
                        Arg::from(bookings.other_currency_bookings),
                    ],
                ))]);
            }
        },
    );
}

fn group_row(sheet: &mut crate::export::xlsx::Sheet, language: Language, section: Key, group: &TaxGroup) {
    sheet.row(vec![
        Cell::text(i18n::text(language, section)),
        Cell::text(group.name.clone()),
        Cell::number(group.bookings.len()),
        Cell::money(group.income),
        Cell::money(group.expenses),
        Cell::money(group.net()),
        Cell::number(group.with_receipt()),
    ]);
}

fn bookings_sheet(writer: &mut XlsxWriter, bookings: &TaxBookings, language: Language) {
    writer.sheet(
        i18n::text(language, Key::XlsxSheetBookings),
        vec![13.0, 24.0, 12.0, 30.0, 18.0, 34.0, 26.0, 12.0, 10.0, 10.0, 44.0],
        true,
        true,
        |sheet| {
            sheet.row(vec![
                Cell::bold(i18n::text(language, Key::TaxColSection)),
                Cell::bold(i18n::text(language, Key::ColCategory)),
                Cell::bold(i18n::text(language, Key::ColDate)),
                Cell::bold(i18n::text(language, Key::ColPartner)),
                Cell::bold(i18n::text(language, Key::ColShop)),
                Cell::bold(i18n::text(language, Key::ColPurpose)),
                Cell::bold(i18n::text(language, Key::ColAccount)),
                Cell::bold(i18n::text(language, Key::ColAmount)),
                Cell::bold(i18n::text(language, Key::ColCurrency)),
                Cell::bold(i18n::text(language, Key::TaxColReceipt)),
                Cell::bold(i18n::text(language, Key::TaxColSource)),
            ]);
            for group in &bookings.income_groups {
                for booking in &group.bookings {
                    booking_row(sheet, language, Key::TaxSectionIncome, group, booking);
                }
            }
            for group in &bookings.deduction_groups {
                for booking in &group.bookings {
                    booking_row(sheet, language, Key::TaxSectionDeductions, group, booking);
                }
            }
        },
    );
}

fn booking_row(
    sheet: &mut crate::export::xlsx::Sheet,
    language: Language,
    section: Key,
    group: &TaxGroup,
    booking: &TransactionItem,
) {
    let receipt = if booking.document_count > 0 {
        i18n::text(language, Key::Yes)
    } else {
        i18n::text(language, Key::No)
    };
    sheet.row(vec![
        Cell::text(i18n::text(language, section)),
        Cell::text(group.name.clone()),
        Cell::date(booking.date),
        Cell::text(booking.partner_name.clone().unwrap_or_default()),
        Cell::text(
            PaymentProviders::merchant_of(booking.purpose.as_deref(), booking.partner_name.as_deref())
                .unwrap_or_default(),
        ),
        Cell::text(booking.purpose.clone().unwrap_or_default()),
        Cell::text(booking.account_iban.clone().unwrap_or_default()),
        Cell::money(booking.amount),
        Cell::text(booking.currency.as_str()),
        Cell::text(receipt),
        Cell::text(source_line(language, booking)),
    ]);
}

fn source_line(language: Language, booking: &TransactionItem) -> String {
    let base = if booking.is_tax_relevant {
        i18n::text(language, Key::TaxRelevantChoice)
    } else {
        i18n::text(language, Key::TaxNotRelevantChoice)
    };
    match booking.tax_source {
        TaxSource::Manual => i18n::format(language, Key::TaxSetByYou, &[Arg::from(base)]),
        TaxSource::Rule => i18n::format(
            language,
            Key::TaxFromRule,
            &[
                Arg::from(base),
                Arg::from(booking.tax_rule_pattern.as_deref().unwrap_or_default()),
            ],
        ),
        TaxSource::Category => i18n::format(
            language,
            Key::TaxFromCategory,
            &[
                Arg::from(base),
                Arg::from(
                    booking
                        .category_name
                        .as_deref()
                        .unwrap_or_else(|| i18n::text(language, Key::Uncategorized)),
                ),
            ],
        ),
        TaxSource::None => base.to_owned(),
    }
}

fn section(layout: &mut PdfBuilder, title: &str, groups: &[TaxGroup], language: Language) {
    if groups.is_empty() {
        return;
    }
    layout.text(title, PdfFont::Bold, 12.0);
    layout.gap(2.0);
    for group in groups {
        layout.ensure_space(GROUP_HEADER_HEIGHT + TABLE_HEADER_HEIGHT + ROW_HEIGHT);
        layout.advance(GROUP_HEADER_HEIGHT);
        layout.draw(
            &format!(
                "{} · {} · {}",
                group.name,
                group.bookings.len(),
                pdf_amount(group.net(), language, true)
            ),
            PdfFont::Bold,
            11.0,
            MARGIN,
        );
        table_header(layout, language);
        for booking in &group.bookings {
            table_row(layout, booking, language);
        }
        layout.gap(6.0);
    }
}

fn table_header(layout: &mut PdfBuilder, language: Language) {
    layout.ensure_space(TABLE_HEADER_HEIGHT + ROW_HEIGHT);
    layout.advance(TABLE_HEADER_HEIGHT);
    layout.draw_fitted(i18n::text(language, Key::ColDate), PdfFont::Bold, 8.0, X_DATE, W_DATE);
    layout.draw_fitted(
        i18n::text(language, Key::ColDescription),
        PdfFont::Bold,
        8.0,
        X_DESC,
        W_DESC,
    );
    layout.draw_fitted(
        i18n::text(language, Key::ColAccount),
        PdfFont::Bold,
        8.0,
        X_ACCOUNT,
        W_ACCOUNT,
    );
    layout.draw_right_fitted(
        i18n::text(language, Key::ColAmount),
        PdfFont::Bold,
        8.0,
        AMOUNT_RIGHT_EDGE,
        W_AMOUNT,
    );
    layout.draw_right_fitted(
        i18n::text(language, Key::TaxColReceipt),
        PdfFont::Bold,
        8.0,
        RECEIPT_RIGHT_EDGE,
        W_RECEIPT,
    );
}

fn table_row(layout: &mut PdfBuilder, booking: &TransactionItem, language: Language) {
    layout.ensure_space(ROW_HEIGHT);
    layout.advance(ROW_HEIGHT);
    layout.draw_fitted(&date(booking.date), PdfFont::Regular, 9.0, X_DATE, W_DATE);
    layout.draw_fitted(
        &super::service::display_description(booking),
        PdfFont::Regular,
        9.0,
        X_DESC,
        W_DESC,
    );
    layout.draw_fitted(&short_account(booking), PdfFont::Regular, 9.0, X_ACCOUNT, W_ACCOUNT);
    layout.draw_right_fitted(
        &pdf_amount(booking.amount, language, true),
        PdfFont::Regular,
        9.0,
        AMOUNT_RIGHT_EDGE,
        W_AMOUNT,
    );
    layout.draw_right_fitted(
        if booking.document_count > 0 {
            i18n::text(language, Key::Yes)
        } else {
            i18n::text(language, Key::No)
        },
        PdfFont::Regular,
        9.0,
        RECEIPT_RIGHT_EDGE,
        W_RECEIPT,
    );
}

fn short_account(booking: &TransactionItem) -> String {
    booking
        .account_iban
        .as_deref()
        .filter(|iban| !iban.trim().is_empty())
        .map(|iban| {
            let characters: Vec<char> = iban.chars().collect();
            let start = characters.len().saturating_sub(4);
            format!("…{}", characters[start..].iter().collect::<String>())
        })
        .unwrap_or_default()
}
