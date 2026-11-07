//! The Reports page state, without any UI or coroutine machinery.
//!
//! This ports FinDesk's `ReportsViewModel`: the selected year or custom range, the tax report and tax bookings,
//! keyword suggestions, recurring payments and the PDF, CSV, XLSX and print actions, but synchronous. Every write
//! goes through the existing repositories, which write the audit log exactly once.

use std::io::Write;
use std::path::{Path, PathBuf};

use time::{Date, Duration, Month};

use crate::db::Database;
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{TransactionFilter, TransactionId};
use crate::money::{Currencies, Currency};
use crate::recurring::{RecurringDetector, RecurringPayment};
use crate::repo::{self, RepoError, Result};
use crate::report::{
    self, CsvExportError, PdfReportExporter, TaxBookings, TaxReport, TaxReportExporter, TaxSuggestion,
};

/// The Reports page state.
///
/// It mirrors FinDesk's `ReportsState`; `language` is kept here because the actions are synchronous and only take
/// the database, a path and (where needed) today's date.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReportsState {
    /// The selected calendar year.
    pub selected_year: i32,
    /// Non-empty: report over this custom range instead of [`Self::selected_year`].
    pub custom_range: Option<(Date, Date)>,
    /// The tax summary over the selected period.
    pub tax_report: Option<TaxReport>,
    /// The Tax bookings tab's groups for the selected period.
    pub tax_bookings: Option<TaxBookings>,
    /// The period [`Self::tax_report`] and [`Self::tax_bookings`] were generated for; `None` until a load publishes
    /// them.
    pub loaded_range: Option<(Date, Date)>,
    /// Keyword suggestions for the selected period; accepting or dismissing one stores a manual override.
    pub suggestions: Vec<TaxSuggestion>,
    /// Whether the tab's rows are filtered to bookings without a receipt (totals stay whole-period).
    pub missing_receipts_only: bool,
    /// Detected over the last 13 months, independent of the selected period.
    pub recurring: Vec<RecurringPayment>,
    /// The profile's primary currency, shown when a total map is empty.
    pub primary_currency: Currency,
    /// Whether a load is running.
    pub is_loading: bool,
    /// Whether an export or print is running.
    pub is_exporting: bool,
    /// The last error, already localized.
    pub error: Option<String>,
    /// The last success message, already localized.
    pub message: Option<String>,
    /// The language for user-visible texts.
    pub language: Language,
}

impl ReportsState {
    /// Builds an empty state for `language`, with the year of `today` selected.
    #[must_use]
    pub fn new(language: Language, today: Date) -> Self {
        Self {
            selected_year: today.year(),
            custom_range: None,
            tax_report: None,
            tax_bookings: None,
            loaded_range: None,
            suggestions: Vec::new(),
            missing_receipts_only: false,
            recurring: Vec::new(),
            primary_currency: Currencies::DEFAULT,
            is_loading: false,
            is_exporting: false,
            error: None,
            message: None,
            language,
        }
    }

    /// Loads the tax report, tax bookings and suggestions for a calendar year.
    pub fn load_tax_report(&mut self, db: &Database, year: i32) {
        self.is_loading = true;
        self.error = None;
        self.selected_year = year;
        let (from, to) = year_range(year);

        match self.generate_year(db, year) {
            Ok((primary, report, tax_bookings, suggestions)) => {
                // Ignore stale results if the year or range changed meanwhile; synchronous callers cannot race, but
                // the guard keeps the published state exactly FinDesk's.
                if self.selected_year == year && self.custom_range.is_none() {
                    self.tax_report = Some(report);
                    self.tax_bookings = Some(tax_bookings);
                    self.loaded_range = Some((from, to));
                    self.suggestions = suggestions;
                    self.primary_currency = primary;
                    self.is_loading = false;
                }
            }
            Err(error) => self.fail(&error),
        }
    }

    /// Loads the recurring payments detected over the last 13 months.
    pub fn load_recurring(&mut self, db: &Database, today: Date) {
        let start = today
            .checked_sub(Duration::days(400))
            .expect("a date 400 days before today");

        let outcome = repo::transactions::find_items(
            db,
            &TransactionFilter {
                start_date: Some(start),
                end_date: Some(today),
                ..Default::default()
            },
            None,
        )
        .and_then(|transactions| {
            let primary = Currencies::primary(db)?;
            Ok((transactions, primary))
        });

        match outcome {
            Ok((transactions, primary)) => {
                self.recurring = RecurringDetector::detect(&transactions, today);
                self.primary_currency = primary;
            }
            Err(error) => self.fail(&error),
        }
    }

    /// Switches to a calendar year and loads it.
    pub fn update_year(&mut self, db: &Database, year: i32) {
        self.custom_range = None;
        self.load_tax_report(db, year);
    }

    /// Switches to a custom range and loads it.
    pub fn set_custom_range(&mut self, db: &Database, from: Date, to: Date) {
        if from > to {
            self.error = Some(i18n::text(self.language, Key::InvalidDateRange).to_owned());
            return;
        }

        self.custom_range = Some((from, to));
        self.is_loading = true;
        self.error = None;

        match self.generate_range(db, from, to) {
            Ok((primary, report, tax_bookings, suggestions)) => {
                if self.custom_range == Some((from, to)) {
                    self.tax_report = Some(report);
                    self.tax_bookings = Some(tax_bookings);
                    self.loaded_range = Some((from, to));
                    self.suggestions = suggestions;
                    self.primary_currency = primary;
                    self.is_loading = false;
                }
            }
            Err(error) => self.fail(&error),
        }
    }

    /// Marks the booking tax relevant (the suggestion's accept).
    pub fn accept_suggestion(&mut self, db: &Database, id: TransactionId) {
        self.set_tax_override(db, id, Some(true));
    }

    /// Hides the suggestion for good by storing a "not relevant" override.
    pub fn dismiss_suggestion(&mut self, db: &Database, id: TransactionId) {
        self.set_tax_override(db, id, Some(false));
    }

    /// Row action: marks a booking not tax relevant.
    pub fn mark_not_tax_relevant(&mut self, db: &Database, id: TransactionId) {
        self.set_tax_override(db, id, Some(false));
    }

    /// Row action: resets a booking's manual override, so rule and category decide again.
    pub fn reset_tax_automatic(&mut self, db: &Database, id: TransactionId) {
        self.set_tax_override(db, id, None);
    }

    /// Filters the tab's rows only; the totals stay those of the whole period.
    pub fn set_missing_receipts_only(&mut self, only: bool) {
        self.missing_receipts_only = only;
    }

    /// Exports the tax summary PDF.
    pub fn export_pdf(&mut self, db: &Database, path: &Path, today: Date) {
        self.is_exporting = true;
        self.error = None;
        let range = self.current_range();
        let cached = self.tax_report.clone().filter(|_| self.loaded_range == Some(range));

        let report = match cached {
            Some(report) => Ok(report),
            None => self.generate_report_for_export(db, range),
        };

        match report {
            Ok(report) => match PdfReportExporter.export(&report, path, today, self.language) {
                Ok(()) => self.finish_export(path),
                Err(_) => {
                    self.is_exporting = false;
                    self.message = None;
                    self.error = Some(i18n::text(self.language, Key::ErrorPdfExportFailed).to_owned());
                }
            },
            Err(error) => self.fail(&error),
        }
    }

    /// Exports a range or year as CSV.
    pub fn export_csv(&mut self, db: &Database, path: &Path) {
        self.is_exporting = true;
        self.error = None;

        let outcome = match self.custom_range {
            Some((from, to)) => report::export_range_to_csv(db, from, to, path, self.language),
            None => report::export_year_to_csv(db, self.selected_year, path, self.language),
        };

        match outcome {
            Ok(()) => self.finish_export(path),
            Err(CsvExportError::Repo(error)) => self.fail(&error),
            Err(CsvExportError::Io(_)) => {
                self.is_exporting = false;
                self.message = None;
                self.error = Some(i18n::text(self.language, Key::ErrorCsvExportFailed).to_owned());
            }
        }
    }

    /// Exports the Tax bookings tab as an Excel workbook.
    pub fn export_tax_xlsx(&mut self, db: &Database, path: &Path, today: Date) {
        self.export_tax(db, path, today, |bookings, path, _today, language| {
            TaxReportExporter.export_xlsx(bookings, path, language)
        });
    }

    /// Exports the Tax bookings tab as the printable PDF.
    pub fn export_tax_pdf(&mut self, db: &Database, path: &Path, today: Date) {
        self.export_tax(db, path, today, |bookings, path, today, language| {
            TaxReportExporter.export_pdf(bookings, path, today, language)
        });
    }

    /// Writes the Tax bookings PDF to a temporary file and returns its path.
    ///
    /// The Reports page opens the file in the system PDF viewer. This function never launches anything itself.
    pub fn print_tax(&mut self, db: &Database, today: Date) -> Option<PathBuf> {
        self.is_exporting = true;
        self.error = None;

        let bookings = match self.current_tax_bookings(db) {
            Ok(bookings) => bookings,
            Err(error) => {
                self.fail(&error);
                return None;
            }
        };
        let bytes = TaxReportExporter.build_pdf(&bookings, today, self.language);

        let outcome = (|| -> std::io::Result<PathBuf> {
            let mut file = tempfile::Builder::new()
                .prefix("finglass-tax-")
                .suffix(".pdf")
                .tempfile()?;
            file.write_all(&bytes)?;
            file.flush()?;
            let (_, path) = file.keep().map_err(|error| error.error)?;
            Ok(path)
        })();

        match outcome {
            Ok(path) => {
                self.is_exporting = false;
                self.message = Some(i18n::text(self.language, Key::MessagePrintSent).to_owned());
                Some(path)
            }
            Err(_) => {
                self.is_exporting = false;
                self.error = Some(i18n::text(self.language, Key::ErrorPrintFailed).to_owned());
                None
            }
        }
    }

    /// Clears the error and success message.
    pub fn clear_messages(&mut self) {
        self.error = None;
        self.message = None;
    }

    /// One manual override: the repository audits it, then the tab's data and the summary reload, whose
    /// tax-relevant total may change.
    fn set_tax_override(&mut self, db: &Database, id: TransactionId, value: Option<bool>) {
        self.is_loading = true;
        self.error = None;

        match repo::transactions::set_tax_override(db, &[id], value) {
            Ok(changed) => {
                if changed > 0 {
                    self.message = Some(i18n::format(
                        self.language,
                        Key::MessageTaxOverrideSet,
                        &[Arg::from(changed)],
                    ));
                }
                self.reload_tab(db);
            }
            Err(error) => self.fail(&error),
        }
    }

    /// Reloads the tab's data and the summary for the period the screen shows; stale periods keep their result.
    fn reload_tab(&mut self, db: &Database) {
        let range = self.current_range();
        let custom_before = self.custom_range;

        match self.generate_range(db, range.0, range.1) {
            Ok((primary, report, tax_bookings, suggestions)) => {
                let still_current = self.custom_range == custom_before;
                if still_current {
                    self.tax_report = Some(report);
                    self.tax_bookings = Some(tax_bookings);
                    self.loaded_range = Some(range);
                    self.suggestions = suggestions;
                    self.primary_currency = primary;
                    self.is_loading = false;
                }
            }
            Err(error) => self.fail(&error),
        }
    }

    /// The period the tab shows: the custom range, or the selected calendar year.
    fn current_range(&self) -> (Date, Date) {
        self.custom_range.unwrap_or_else(|| year_range(self.selected_year))
    }

    /// The tab's data, loading it first when the screen has not finished loading yet or the period has changed.
    fn current_tax_bookings(&self, db: &Database) -> Result<TaxBookings> {
        let range = self.current_range();
        if let Some(bookings) = self.tax_bookings.as_ref().filter(|_| self.loaded_range == Some(range)) {
            return Ok(bookings.clone());
        }
        report::generate_tax_bookings(db, range.0, range.1, self.language)
    }

    fn generate_year(
        &self,
        db: &Database,
        year: i32,
    ) -> Result<(Currency, TaxReport, TaxBookings, Vec<TaxSuggestion>)> {
        let (from, to) = year_range(year);
        let primary = Currencies::primary(db)?;
        let report = report::generate_tax_report(db, year, self.language)?;
        let tax_bookings = report::generate_tax_bookings(db, from, to, self.language)?;
        let suggestions = self.find_suggestions(db, from, to, primary)?;
        Ok((primary, report, tax_bookings, suggestions))
    }

    fn generate_range(
        &self,
        db: &Database,
        from: Date,
        to: Date,
    ) -> Result<(Currency, TaxReport, TaxBookings, Vec<TaxSuggestion>)> {
        let primary = Currencies::primary(db)?;
        let report = report::generate_range_report(db, from, to, self.language)?;
        let tax_bookings = report::generate_tax_bookings(db, from, to, self.language)?;
        let suggestions = self.find_suggestions(db, from, to, primary)?;
        Ok((primary, report, tax_bookings, suggestions))
    }

    /// Keyword suggestions over the period's bookings of the primary currency.
    fn find_suggestions(&self, db: &Database, from: Date, to: Date, primary: Currency) -> Result<Vec<TaxSuggestion>> {
        let bookings = repo::transactions::find_items(
            db,
            &TransactionFilter {
                start_date: Some(from),
                end_date: Some(to),
                currency: Some(primary),
                ..Default::default()
            },
            None,
        )?;
        let keywords = report::tax_suggestion_keywords(db)?;
        Ok(report::find_tax_suggestions(&bookings, &keywords))
    }

    fn generate_report_for_export(&self, db: &Database, range: (Date, Date)) -> Result<TaxReport> {
        if self.custom_range.is_some() {
            report::generate_range_report(db, range.0, range.1, self.language)
        } else {
            report::generate_tax_report(db, self.selected_year, self.language)
        }
    }

    fn export_tax(
        &mut self,
        db: &Database,
        path: &Path,
        today: Date,
        export: impl FnOnce(&TaxBookings, &Path, Date, Language) -> std::io::Result<()>,
    ) {
        self.is_exporting = true;
        self.error = None;

        match self.current_tax_bookings(db) {
            Ok(bookings) => match export(&bookings, path, today, self.language) {
                Ok(()) => self.finish_export(path),
                Err(_) => {
                    self.is_exporting = false;
                    self.error = Some(i18n::text(self.language, Key::ErrorTaxExportFailed).to_owned());
                }
            },
            Err(error) => self.fail(&error),
        }
    }

    fn finish_export(&mut self, path: &Path) {
        self.is_exporting = false;
        self.message = Some(i18n::format(
            self.language,
            Key::MessageSavedTo,
            &[Arg::from(file_name(path))],
        ));
    }

    fn fail(&mut self, error: &RepoError) {
        let message = self.error_text(error);
        self.is_loading = false;
        self.is_exporting = false;
        self.error = Some(message);
    }

    fn error_text(&self, error: &RepoError) -> String {
        match error {
            RepoError::Invalid(message) => message.clone(),
            _ => i18n::text(self.language, Key::ErrorUnexpected).to_owned(),
        }
    }
}

fn year_range(year: i32) -> (Date, Date) {
    (
        Date::from_calendar_date(year, Month::January, 1).expect("a valid year"),
        Date::from_calendar_date(year, Month::December, 31).expect("a valid year"),
    )
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| path.display().to_string())
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicU64, Ordering};

    use time::macros::date;

    use super::*;
    use crate::audit;
    use crate::model::{AccountId, AccountType, CategoryId, NewAccount, NewCategory, NewDocument, NewTransaction};
    use crate::money::Money;
    use crate::repo::{accounts, categories, documents, transactions};
    use crate::secret::{Passphrase, PassphrasePurpose};

    static NEXT_HASH: AtomicU64 = AtomicU64::new(0);

    fn database() -> (tempfile::TempDir, Database) {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("finglass.db");
        let pass = Passphrase::new("correct horse battery staple", PassphrasePurpose::New).unwrap();
        let db = Database::create(&path, &pass).unwrap();
        (temp, db)
    }

    fn account(db: &Database, currency: Currency) -> AccountId {
        accounts::create(
            db,
            NewAccount {
                bank_connection_id: None,
                institution: None,
                name: Some("Account".to_owned()),
                iban: None,
                account_number: None,
                currency,
                account_type: AccountType::Checking,
                balance: Money { minor: 0, currency },
                balance_updated_at: None,
                group_id: None,
            },
        )
        .unwrap()
    }

    fn category(db: &Database, name: &str, color: &str, tax_relevant: bool) -> CategoryId {
        categories::create(
            db,
            NewCategory {
                name: name.to_owned(),
                color: color.to_owned(),
                budget_limit: None,
                is_tax_relevant: tax_relevant,
                is_transfer: false,
            },
        )
        .unwrap()
    }

    #[allow(clippy::too_many_arguments)]
    fn transaction(
        db: &Database,
        account_id: AccountId,
        day: Date,
        minor: i64,
        currency: Currency,
        partner: Option<&str>,
        purpose: Option<&str>,
        category_id: Option<CategoryId>,
    ) -> TransactionId {
        let hash = format!(
            "test-{}-{}",
            std::process::id(),
            NEXT_HASH.fetch_add(1, Ordering::Relaxed)
        );
        transactions::insert_imported(
            db,
            account_id,
            vec![NewTransaction {
                booking_date: day,
                amount: Money { minor, currency },
                balance_after: None,
                partner_name: partner.map(str::to_owned),
                purpose: purpose.map(str::to_owned),
                category_id,
                import_hash: hash.clone(),
                created_at: time::OffsetDateTime::now_utc(),
                is_new: false,
                tax_override: None,
            }],
        )
        .unwrap();
        transactions::list_for_account(db, account_id, None, None)
            .unwrap()
            .into_iter()
            .find(|row| row.import_hash == hash)
            .unwrap()
            .id
    }

    fn with_receipt(db: &Database, transaction_id: TransactionId) {
        documents::create(
            db,
            NewDocument {
                file_name: "Beleg.pdf".to_owned(),
                stored_path: format!("documents/beleg-{}.pdf", transaction_id.0),
                mime_type: "application/pdf".to_owned(),
                transaction_id: Some(transaction_id),
            },
        )
        .unwrap();
    }

    fn audit_entries(db: &Database, action: &str) -> Vec<crate::audit::AuditEntry> {
        audit::recent(db, 10_000)
            .unwrap()
            .into_iter()
            .filter(|entry| entry.action == action)
            .collect()
    }

    fn audit_count(db: &Database) -> usize {
        audit::recent(db, 10_000).unwrap().len()
    }

    fn saved(language: Language, path: &Path) -> String {
        i18n::format(language, Key::MessageSavedTo, &[Arg::from(file_name(path))])
    }

    fn pdf_text(path: &Path) -> String {
        crate::documents::pdf_text::extract_text(path, 20)
            .expect("the exported PDF is readable")
            .text
    }

    // ---------------------------------------------------------------------------------------------
    // TaxTabTest
    // ---------------------------------------------------------------------------------------------

    #[test]
    fn loading_a_year_fills_the_tab_and_accepting_and_dismissing_suggestions_write_one_audit_entry_each() {
        let (_temp, db) = database();
        let account = account(&db, Currency::EUR);
        let income = category(&db, "Gehalt", "#4CAF50", true);
        let deduction = category(&db, "Spenden", "#9C27B0", true);
        let salary = transaction(
            &db,
            account,
            date!(2026 - 02 - 01),
            200_000,
            Currency::EUR,
            Some("Beispiel AG"),
            None,
            Some(income),
        );
        let _donation = transaction(
            &db,
            account,
            date!(2026 - 03 - 01),
            -12_000,
            Currency::EUR,
            Some("Beispielhilfe e. V."),
            Some("Spende"),
            Some(deduction),
        );
        let suggested = transaction(
            &db,
            account,
            date!(2026 - 04 - 01),
            -3_000,
            Currency::EUR,
            Some("Beispiel-Apotheke"),
            Some("Medikamente"),
            None,
        );
        let _not_matched = transaction(
            &db,
            account,
            date!(2026 - 05 - 01),
            -900,
            Currency::EUR,
            Some("Bäckerei Beispiel"),
            None,
            None,
        );
        let mut state = ReportsState::new(Language::En, date!(2026 - 06 - 15));

        state.load_tax_report(&db, 2026);
        let loaded = state.clone();
        let tax_bookings = loaded.tax_bookings.expect("loading fills tax bookings");
        assert_eq!(
            tax_bookings
                .income_groups
                .iter()
                .map(|group| group.category_id)
                .collect::<Vec<_>>(),
            vec![Some(income)],
            "the salary category is an income group"
        );
        assert_eq!(
            tax_bookings
                .deduction_groups
                .iter()
                .map(|group| group.category_id)
                .collect::<Vec<_>>(),
            vec![Some(deduction)]
        );
        assert_eq!(tax_bookings.booking_count, 2, "the two tax-relevant bookings are in");
        assert_eq!(
            loaded
                .suggestions
                .iter()
                .map(|suggestion| suggestion.booking.id)
                .collect::<Vec<_>>(),
            vec![suggested],
            "the Apotheke booking is suggested, the 'Spende' one is already relevant"
        );
        assert_eq!(loaded.suggestions[0].keyword, "Apotheke");

        state.accept_suggestion(&db, suggested);
        assert!(state.suggestions.is_empty(), "the accepted suggestion is gone");
        let groups = state.tax_bookings.as_ref().unwrap();
        assert_eq!(groups.booking_count, 3, "the accepted booking joined the tax bookings");
        assert!(
            groups
                .deduction_groups
                .iter()
                .flat_map(|group| &group.bookings)
                .any(|booking| booking.id == suggested),
            "the accepted booking is a deduction"
        );

        let dismissed = transaction(
            &db,
            account,
            date!(2026 - 06 - 01),
            -20_000,
            Currency::EUR,
            Some("Handwerker Beispiel"),
            None,
            None,
        );
        state.load_tax_report(&db, 2026);
        assert_eq!(
            state
                .suggestions
                .iter()
                .map(|suggestion| suggestion.booking.id)
                .collect::<Vec<_>>(),
            vec![dismissed]
        );
        state.dismiss_suggestion(&db, dismissed);
        assert!(state.suggestions.is_empty(), "the dismissed suggestion is hidden");
        state.update_year(&db, 2026);
        assert!(
            state.suggestions.is_empty(),
            "the dismissed suggestion stays hidden after a reload"
        );

        let entries = audit_entries(&db, "transaction.set_tax_override");
        assert_eq!(entries.len(), 2, "one entry per action: accept and dismiss");
        assert!(
            entries
                .iter()
                .any(|entry| entry.details.contains("count=1") && entry.details.contains("Some(true)")),
            "the accept entry names count and value: {:?}",
            entries
        );
        assert!(
            entries
                .iter()
                .any(|entry| entry.details.contains("count=1") && entry.details.contains("Some(false)")),
            "the dismiss entry names count and value: {:?}",
            entries
        );
        assert!(
            entries.iter().all(|entry| !entry.details.contains("Apotheke")),
            "no partner text in the audit entries: {entries:?}"
        );
        assert!(
            entries.iter().all(|entry| !entry.details.contains("-30.00")),
            "no amount in the audit entries: {entries:?}"
        );

        let by_id: std::collections::BTreeMap<_, _> =
            repo::transactions::find_items(&db, &TransactionFilter::default(), None)
                .unwrap()
                .into_iter()
                .map(|item| (item.id, item))
                .collect();
        assert_eq!(by_id[&dismissed].tax_override, Some(false));
        assert_eq!(by_id[&suggested].tax_override, Some(true));
        assert!(
            by_id[&salary].is_tax_relevant,
            "the salary booking keeps its category relevance"
        );
    }

    #[test]
    fn mark_not_tax_relevant_removes_a_category_relevant_booking_and_reset_tax_automatic_brings_it_back() {
        let (_temp, db) = database();
        let account = account(&db, Currency::EUR);
        let deduction = category(&db, "Spenden", "#9C27B0", true);
        let donation = transaction(
            &db,
            account,
            date!(2026 - 03 - 01),
            -12_000,
            Currency::EUR,
            Some("Beispielhilfe e. V."),
            None,
            Some(deduction),
        );
        let mut state = ReportsState::new(Language::En, date!(2026 - 06 - 15));
        state.load_tax_report(&db, 2026);
        assert_eq!(
            state.tax_bookings.as_ref().unwrap().deduction_groups[0]
                .bookings
                .iter()
                .map(|booking| booking.id)
                .collect::<Vec<_>>(),
            vec![donation]
        );

        state.mark_not_tax_relevant(&db, donation);
        assert!(
            state.tax_bookings.as_ref().unwrap().deduction_groups.is_empty(),
            "the booking is out of the groups"
        );
        assert_eq!(
            state.tax_bookings.as_ref().unwrap().booking_count,
            0,
            "no tax-relevant bookings left in the period"
        );
        let marked = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == donation)
            .unwrap();
        assert!(!marked.is_tax_relevant);
        assert_eq!(marked.tax_source, crate::model::TaxSource::Manual);
        assert_eq!(marked.tax_override, Some(false));

        state.reset_tax_automatic(&db, donation);
        let reset = repo::transactions::find_items(&db, &TransactionFilter::default(), None)
            .unwrap()
            .into_iter()
            .find(|item| item.id == donation)
            .unwrap();
        assert_eq!(reset.tax_override, None, "None resets to automatic");
        assert!(reset.is_tax_relevant, "the category flag decides again");
        assert_eq!(
            state.tax_bookings.as_ref().unwrap().deduction_groups[0]
                .bookings
                .iter()
                .map(|booking| booking.id)
                .collect::<Vec<_>>(),
            vec![donation]
        );

        let entries = audit_entries(&db, "transaction.set_tax_override");
        assert_eq!(entries.len(), 2, "one entry per action");
        assert!(
            entries
                .iter()
                .any(|entry| entry.details.contains("count=1") && entry.details.contains("Some(false)"))
        );
        assert!(
            entries
                .iter()
                .any(|entry| entry.details.contains("count=1") && entry.details.contains("None"))
        );
        assert!(
            entries.iter().all(|entry| !entry.details.contains("Beispiel")),
            "no booking text in the audit entries: {entries:?}"
        );
    }

    #[test]
    fn set_missing_receipts_only_filters_rows_but_keeps_the_whole_periods_totals() {
        let (_temp, db) = database();
        let account = account(&db, Currency::EUR);
        let deduction = category(&db, "Spenden", "#9C27B0", true);
        let with_receipt_id = transaction(
            &db,
            account,
            date!(2026 - 03 - 01),
            -12_000,
            Currency::EUR,
            Some("Beispielhilfe e. V."),
            Some("Spende"),
            Some(deduction),
        );
        with_receipt(&db, with_receipt_id);
        transaction(
            &db,
            account,
            date!(2026 - 04 - 01),
            -3_000,
            Currency::EUR,
            Some("Beispiel-Apotheke"),
            Some("Medikamente"),
            Some(deduction),
        );
        let mut state = ReportsState::new(Language::En, date!(2026 - 06 - 15));
        state.load_tax_report(&db, 2026);

        let before = state.tax_bookings.clone().unwrap();
        let before_group = &before.deduction_groups[0];
        assert_eq!(before_group.bookings.len(), 2);

        state.set_missing_receipts_only(true);
        let after = state.clone();
        assert!(after.missing_receipts_only, "the filter flag is on for the shown rows");
        assert_eq!(
            before.deduction_total(),
            after.tax_bookings.as_ref().unwrap().deduction_total(),
            "the totals stay those of the whole period"
        );
        assert_eq!(before.booking_count, after.tax_bookings.as_ref().unwrap().booking_count);
        assert_eq!(
            before_group.bookings.len(),
            after.tax_bookings.as_ref().unwrap().deduction_groups[0].bookings.len(),
            "the data itself is unfiltered"
        );
        state.set_missing_receipts_only(false);
        assert!(!state.missing_receipts_only);
    }

    // ---------------------------------------------------------------------------------------------
    // TaxExportTest
    // ---------------------------------------------------------------------------------------------

    fn tax_export_setup(db: &Database) {
        let account = account(db, Currency::EUR);
        let donations = category(db, "Spenden", "#9C27B0", true);
        transaction(
            db,
            account,
            date!(2026 - 03 - 01),
            -12_000,
            Currency::EUR,
            Some("Beispielhilfe e. V."),
            Some("Spende"),
            Some(donations),
        );
    }

    #[test]
    fn exports_write_the_files_and_set_the_saved_message() {
        let (temp, db) = database();
        tax_export_setup(&db);
        let mut state = ReportsState::new(Language::En, date!(2026 - 11 - 07));
        state.load_tax_report(&db, 2026);
        let audit_before = audit_count(&db);

        let xlsx = temp.path().join("tax.xlsx");
        state.export_tax_xlsx(&db, &xlsx, date!(2026 - 11 - 07));
        assert!(xlsx.is_file(), "the workbook was written");
        assert_eq!(state.message.as_deref(), Some(saved(Language::En, &xlsx).as_str()));
        assert_eq!(state.error, None);
        assert!(!state.is_exporting, "the busy flag is cleared");

        let pdf = temp.path().join("tax.pdf");
        state.export_tax_pdf(&db, &pdf, date!(2026 - 11 - 07));
        assert!(pdf.is_file(), "the PDF was written");
        assert_eq!(state.message.as_deref(), Some(saved(Language::En, &pdf).as_str()));
        assert_eq!(state.error, None);

        let pages = crate::documents::pdf_text::extract_text(&pdf, 20)
            .expect("the exported PDF is readable")
            .page_count;
        assert!(pages >= 1);
        assert_eq!(audit_before, audit_count(&db), "exports are read-only: no audit entry");
    }

    #[test]
    fn print_tax_writes_a_temporary_pdf_and_reports_sent() {
        let (_temp, db) = database();
        tax_export_setup(&db);
        let mut state = ReportsState::new(Language::En, date!(2026 - 11 - 07));
        state.load_tax_report(&db, 2026);
        let audit_before = audit_count(&db);

        let path = state
            .print_tax(&db, date!(2026 - 11 - 07))
            .expect("the print file was written");
        let text = crate::documents::pdf_text::extract_text(&path, 20).expect("the PDF is readable");
        assert_eq!(text.page_count, 1, "the synthetic booking fits on one page");
        assert!(
            text.text.contains("Tax-relevant bookings 2026"),
            "the document names the report: {}",
            text.text
        );
        assert_eq!(
            state.message.as_deref(),
            Some(i18n::text(Language::En, Key::MessagePrintSent))
        );
        assert_eq!(state.error, None);
        assert!(!state.is_exporting);
        assert_eq!(audit_before, audit_count(&db), "printing is read-only: no audit entry");

        std::fs::remove_file(path).unwrap();
    }

    // The printer cancellation/failure case is not ported: there is no printer adapter in core. `print_tax` only
    // writes a temporary file and the page opens it in the system viewer; a write failure sets `ErrorPrintFailed`.

    // ---------------------------------------------------------------------------------------------
    // ReportsPeriodDataTest
    // ---------------------------------------------------------------------------------------------

    #[test]
    fn exports_use_the_newly_selected_year_and_never_another_years_cached_data() {
        let (temp, db) = database();
        let account = account(&db, Currency::EUR);
        let donations = category(&db, "Spenden", "#FF5722", true);
        transaction(
            &db,
            account,
            date!(2026 - 03 - 01),
            -10_000,
            Currency::EUR,
            Some("YearOneSpende"),
            None,
            Some(donations),
        );
        transaction(
            &db,
            account,
            date!(2027 - 03 - 01),
            -20_000,
            Currency::EUR,
            Some("YearTwoSpende"),
            None,
            Some(donations),
        );
        let mut state = ReportsState::new(Language::En, date!(2027 - 06 - 15));

        state.load_tax_report(&db, 2026);
        assert_eq!(state.loaded_range.unwrap().0, date!(2026 - 01 - 01));

        // The synchronous API finishes the year-two load before the export, so the cached data is year two.
        state.load_tax_report(&db, 2027);
        assert_eq!(
            state.loaded_range.unwrap().0,
            date!(2027 - 01 - 01),
            "the load published year two"
        );

        let xlsx = temp.path().join("tax.xlsx");
        state.export_tax_xlsx(&db, &xlsx, date!(2027 - 11 - 07));
        let bytes = std::fs::read(&xlsx).unwrap();
        let rows = crate::import::xlsx::first_sheet_rows(&bytes).unwrap();
        assert!(
            rows[0].iter().any(|cell| cell.contains("2027")),
            "the workbook is titled for the selected year: {:?}",
            rows[0]
        );
        assert!(
            rows.iter().flatten().any(|cell| cell == "200"),
            "the workbook holds year 2's deduction: {rows:?}"
        );

        let pdf = temp.path().join("tax.pdf");
        state.export_pdf(&db, &pdf, date!(2027 - 11 - 07));
        let text = pdf_text(&pdf);
        assert!(text.contains("YearTwoSpende"), "the PDF holds year 2's booking: {text}");
        assert!(
            !text.contains("YearOneSpende"),
            "the PDF does not hold year 1's booking: {text}"
        );
    }

    // The original coroutine-timing test blocked the year-two repository call and checked that exports still use the
    // new year while the cached data was old. The synchronous API cannot be mid-load, so that blocking part is not
    // ported; the assertions about never exporting another period's data are.

    // ---------------------------------------------------------------------------------------------
    // InsightsTest (reports part)
    // ---------------------------------------------------------------------------------------------

    #[test]
    fn custom_range_reports_span_years() {
        let (_temp, db) = database();
        let account = account(&db, Currency::EUR);
        transaction(
            &db,
            account,
            date!(2025 - 12 - 20),
            -3_000,
            Currency::EUR,
            Some("Dec"),
            None,
            None,
        );
        transaction(
            &db,
            account,
            date!(2026 - 01 - 10),
            -2_000,
            Currency::EUR,
            Some("Jan"),
            None,
            None,
        );
        transaction(
            &db,
            account,
            date!(2026 - 02 - 10),
            -9_900,
            Currency::EUR,
            Some("Outside"),
            None,
            None,
        );

        let report =
            report::generate_range_report(&db, date!(2025 - 12 - 01), date!(2026 - 01 - 31), Language::En).unwrap();
        assert_eq!(
            report
                .monthly_breakdown
                .iter()
                .map(|month| (month.year, month.month))
                .collect::<Vec<_>>(),
            vec![(2025, 12), (2026, 1)]
        );
        assert_eq!(
            report.total_expenses,
            Money {
                minor: 5_000,
                currency: Currency::EUR
            }
        );
        assert!(!report.is_calendar_year());
        assert_eq!(report.title(Language::En), "01.12.2025 – 31.01.2026");

        let mut state = ReportsState::new(Language::En, date!(2026 - 03 - 15));
        state.load_tax_report(&db, 2026);
        state.set_custom_range(&db, date!(2025 - 12 - 01), date!(2026 - 01 - 31));
        assert_eq!(
            state.tax_report.as_ref().unwrap().total_expenses,
            Money {
                minor: 5_000,
                currency: Currency::EUR
            },
            "the range report replaces the year report"
        );
    }

    // The original also checked that a late year result cannot replace the range. The synchronous API has no
    // in-flight year load, so that timing-only assertion is not ported.

    // ---------------------------------------------------------------------------------------------
    // ViewModelsTest (reports part)
    // ---------------------------------------------------------------------------------------------

    #[test]
    fn reports_view_model_loads_and_exports() {
        let (temp, db) = database();
        let account = account(&db, Currency::EUR);
        let food = category(&db, "Food", "#4CAF50", false);
        let donations = category(&db, "Spenden", "#FF5722", true);
        transaction(
            &db,
            account,
            date!(2026 - 03 - 02),
            -3_000,
            Currency::EUR,
            Some("REWE"),
            None,
            Some(food),
        );
        transaction(
            &db,
            account,
            date!(2026 - 03 - 05),
            -2_000,
            Currency::EUR,
            Some("Kiosk"),
            None,
            None,
        );
        transaction(
            &db,
            account,
            date!(2026 - 03 - 01),
            250_000,
            Currency::EUR,
            Some("Employer"),
            None,
            None,
        );
        transaction(
            &db,
            account,
            date!(2026 - 01 - 15),
            -10_000,
            Currency::EUR,
            Some("UNICEF"),
            None,
            Some(donations),
        );
        let mut state = ReportsState::new(Language::En, date!(2026 - 03 - 20));
        state.load_tax_report(&db, 2026);
        assert_eq!(
            state.tax_report.as_ref().unwrap().total_income,
            Money {
                minor: 250_000,
                currency: Currency::EUR
            }
        );

        let pdf = temp.path().join("r.pdf");
        state.export_pdf(&db, &pdf, date!(2026 - 03 - 20));
        assert!(!std::fs::read(&pdf).unwrap().is_empty());
        assert_eq!(state.message.as_deref(), Some("Saved r.pdf"));
    }

    // ---------------------------------------------------------------------------------------------
    // MultiCurrencyTest (reports parts)
    // ---------------------------------------------------------------------------------------------

    #[test]
    fn the_primary_currency_setting_switches_what_reports_total() {
        let (_temp, db) = database();
        let eur = account(&db, Currency::EUR);
        let usd = account(&db, Currency::new("USD").unwrap());
        transaction(
            &db,
            eur,
            date!(2026 - 10 - 02),
            200_000,
            Currency::EUR,
            Some("Employer"),
            None,
            None,
        );
        transaction(
            &db,
            usd,
            date!(2026 - 10 - 03),
            80_000,
            Currency::new("USD").unwrap(),
            Some("US client"),
            None,
            None,
        );
        crate::settings::set_setting(&db, Currencies::KEY_PRIMARY, "USD").unwrap();

        let mut state = ReportsState::new(Language::En, date!(2026 - 10 - 15));
        state.load_tax_report(&db, 2026);
        let report = state.tax_report.as_ref().unwrap();

        assert_eq!(report.currency, Currency::new("USD").unwrap());
        assert_eq!(
            report.total_income,
            Money {
                minor: 80_000,
                currency: Currency::new("USD").unwrap()
            }
        );
        assert_eq!(report.other_currency_bookings, 1);
    }

    #[test]
    fn empty_totals_fall_back_to_the_primary_currency() {
        let (_temp, db) = database();
        crate::settings::set_setting(&db, Currencies::KEY_PRIMARY, "USD").unwrap();

        let mut state = ReportsState::new(Language::En, date!(2026 - 10 - 15));
        state.load_tax_report(&db, 2026);

        assert_eq!(state.primary_currency, Currency::new("USD").unwrap());
    }

    #[test]
    fn reports_count_bookings_in_other_currencies_instead_of_adding_them() {
        let (_temp, db) = database();
        let eur = account(&db, Currency::EUR);
        let usd = account(&db, Currency::new("USD").unwrap());
        transaction(
            &db,
            eur,
            date!(2026 - 03 - 01),
            -3_000,
            Currency::EUR,
            Some("REWE"),
            None,
            None,
        );
        transaction(
            &db,
            usd,
            date!(2026 - 03 - 02),
            -7_000,
            Currency::new("USD").unwrap(),
            Some("Walmart"),
            None,
            None,
        );

        let mut state = ReportsState::new(Language::En, date!(2026 - 03 - 15));
        state.load_tax_report(&db, 2026);
        let report = state.tax_report.as_ref().unwrap();

        assert_eq!(report.currency, Currency::EUR);
        assert_eq!(
            report.total_expenses,
            Money {
                minor: 3_000,
                currency: Currency::EUR
            }
        );
        assert_eq!(report.other_currency_bookings, 1);
        assert_eq!(report.transaction_count, 1);
    }

    #[test]
    fn recurring_payments_are_per_currency() {
        let (_temp, db) = database();
        let eur = account(&db, Currency::EUR);
        let usd = account(&db, Currency::new("USD").unwrap());
        for month in 6..=8 {
            let day = Date::from_calendar_date(2026, Month::try_from(month).unwrap(), 5).unwrap();
            transaction(&db, eur, day, -999, Currency::EUR, Some("Spotify"), None, None);
            transaction(
                &db,
                usd,
                day,
                -1_199,
                Currency::new("USD").unwrap(),
                Some("Spotify"),
                None,
                None,
            );
        }

        let mut state = ReportsState::new(Language::En, date!(2026 - 09 - 20));
        state.load_recurring(&db, date!(2026 - 09 - 20));

        let payments: std::collections::BTreeSet<(Currency, i64)> = state
            .recurring
            .iter()
            .map(|payment| (payment.currency, payment.typical_amount.minor))
            .collect();
        assert_eq!(
            payments,
            std::collections::BTreeSet::from([(Currency::EUR, -999), (Currency::new("USD").unwrap(), -1_199),])
        );
    }
}
