//! Reports, tax bookings and their CSV, XLSX and PDF exports.
//!
//! Ported from FinDesk's `document/ReportService.kt`, `document/TaxSuggestions.kt`,
//! `document/TaxReportExporter.kt` and `document/PdfReportExporter.kt`.

pub mod csv;
pub mod format;
pub mod pdf_report;
pub mod service;
pub mod suggestions;
pub mod tax_export;

#[cfg(test)]
mod tests;

pub use csv::{CsvExportError, CsvFormat, export_range_to_csv, export_to_csv, export_year_to_csv};
pub use pdf_report::PdfReportExporter;
pub use service::{
    CategoryShare, CategoryTrend, ExpenseItem, MonthlyAmount, MonthlyData, MonthlyReport, TaxBookings, TaxGroup,
    TaxReport, build_report, build_tax_bookings, build_tax_report, category_shares, display_description,
    generate_monthly_report, generate_range_report, generate_tax_bookings, generate_tax_report, get_category_trends,
    monthly_report, sum_by_category, top_vendors,
};
pub use suggestions::{
    DEFAULT_KEYWORDS, KEY_KEYWORDS, TaxSuggestion, find as find_tax_suggestions, keywords as tax_suggestion_keywords,
};
pub use tax_export::TaxReportExporter;
