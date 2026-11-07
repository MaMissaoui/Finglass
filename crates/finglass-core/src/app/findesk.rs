//! UI-free helpers and state for the one-time FinDesk import.
//!
//! The Settings page only picks a folder and asks for FinDesk's passphrase; everything else lives here. The state
//! keeps what the dialogs need (the chosen folder, a wrong-passphrase flag, the report or a localized error) and runs
//! the import into the open profile. The passphrase itself is never stored.

use std::io;
use std::path::{Path, PathBuf};

use time::UtcOffset;

use crate::db::Database;
use crate::findesk_import::{self, FinDeskImportError, FinDeskImportReport};
use crate::i18n::{self, Arg, Key, Language};
use crate::paths::AppPaths;
use crate::profiles;
use crate::secret::Passphrase;

/// The folder the FinDesk picker starts in.
///
/// `findesk_home` is FinDesk's own `FINDESK_HOME`, read by the caller. Without it the default is `<home>/.findesk`;
/// starting inside the (normally hidden) dot-folder is what makes the picker show the database on macOS.
#[must_use]
pub fn findesk_start_dir(home: &Path, findesk_home: Option<&Path>) -> PathBuf {
    match findesk_home {
        Some(path) => path.to_path_buf(),
        None => home.join(".findesk"),
    }
}

/// [`findesk_start_dir`] for this machine: the user's home folder and FinDesk's `FINDESK_HOME`, if set and not empty.
/// `None` when the home folder is unknown. Only the app calls this; tests use [`findesk_start_dir`].
#[must_use]
pub fn default_findesk_start_dir() -> Option<PathBuf> {
    let home = dirs::home_dir()?;
    let findesk_home = std::env::var_os("FINDESK_HOME")
        .filter(|value| !value.is_empty())
        .map(PathBuf::from);
    Some(findesk_start_dir(&home, findesk_home.as_deref()))
}

/// Resolves what the user picked in the folder dialog to the folder that holds `findesk.db`.
///
/// Accepted are a folder that holds `findesk.db`, a FinDesk root whose `profiles/<slug>/` folders hold one, and the
/// `findesk.db` file itself. A FinDesk root's own database is its default profile and wins over the named profiles;
/// when it has none, the only named profile with a database is used; with several, the user must pick one.
#[must_use]
pub fn resolve_findesk_dir(chosen: &Path) -> Option<PathBuf> {
    if chosen.is_file() {
        if chosen.file_name().and_then(|name| name.to_str()) != Some("findesk.db") {
            return None;
        }

        let parent = chosen.parent()?;
        return Some(if parent.as_os_str().is_empty() {
            PathBuf::from(".")
        } else {
            parent.to_path_buf()
        });
    }

    if !chosen.is_dir() {
        return None;
    }

    if chosen.join("findesk.db").is_file() {
        return Some(chosen.to_path_buf());
    }

    let profiles_dir = chosen.join("profiles");
    if !profiles_dir.is_dir() {
        return None;
    }

    let mut candidates: Vec<PathBuf> = std::fs::read_dir(&profiles_dir)
        .ok()?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.is_dir() && path.join("findesk.db").is_file())
        .collect();

    // Never guess between several profiles: the user then picks the profile's own folder.
    if candidates.len() == 1 { candidates.pop() } else { None }
}

/// The state behind the FinDesk import dialogs and action.
#[derive(Debug, Clone, PartialEq)]
pub struct FindeskImportState {
    /// The language for user-visible texts.
    pub language: Language,
    /// The resolved FinDesk data folder, once the user picked a valid one.
    pub source_dir: Option<PathBuf>,
    /// Whether the last passphrase was rejected.
    pub passphrase_wrong: bool,
    /// Whether an import is running.
    pub is_importing: bool,
    /// The report of a successful import.
    pub report: Option<FinDeskImportReport>,
    /// The localized error of a failed import.
    pub error: Option<String>,
}

impl FindeskImportState {
    /// Builds an empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            language,
            source_dir: None,
            passphrase_wrong: false,
            is_importing: false,
            report: None,
            error: None,
        }
    }

    /// Validates and remembers `chosen`, returning the resolved data folder.
    pub fn choose(&mut self, chosen: &Path) -> Option<PathBuf> {
        let resolved = resolve_findesk_dir(chosen)?;
        self.source_dir = Some(resolved.clone());
        self.passphrase_wrong = false;
        self.report = None;
        self.error = None;
        Some(resolved)
    }

    /// Forgets the chosen folder and closes any pending result.
    pub fn cancel(&mut self) {
        self.source_dir = None;
        self.passphrase_wrong = false;
        self.is_importing = false;
        self.report = None;
        self.error = None;
    }

    /// Drops the success report or the error without forgetting the folder.
    pub fn clear_result(&mut self) {
        self.report = None;
        self.error = None;
    }

    /// Runs the import from the chosen folder into `profile_dir` (`<profile>`; the documents go into its
    /// `documents/` subfolder), using `offset` for FinDesk's zone-less dates.
    pub fn import(
        &mut self,
        db: &Database,
        profile_dir: &Path,
        passphrase: &Passphrase,
        offset: UtcOffset,
    ) -> Result<FinDeskImportReport, FinDeskImportError> {
        let Some(source_dir) = self.source_dir.clone() else {
            return self.fail(FinDeskImportError::NotFound);
        };

        self.is_importing = true;
        let documents_dir = profile_dir.join("documents");
        let result = findesk_import::import_findesk(db, &documents_dir, &source_dir, passphrase, offset);
        self.is_importing = false;

        match result {
            Ok(report) => {
                self.report = Some(report.clone());
                self.error = None;
                self.passphrase_wrong = false;
                Ok(report)
            }
            Err(FinDeskImportError::WrongPassphrase) => {
                self.report = None;
                self.error = None;
                self.passphrase_wrong = true;
                Err(FinDeskImportError::WrongPassphrase)
            }
            Err(error) => self.fail(error),
        }
    }

    /// Runs the import into the open Finglass profile, with the machine's local UTC offset.
    pub fn import_in_profile(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        slug: Option<&str>,
        passphrase: &Passphrase,
    ) -> Result<FinDeskImportReport, FinDeskImportError> {
        let profile_dir = profiles::data_dir(paths, slug)
            .map_err(|_| FinDeskImportError::Io(io::Error::other("profile folder is unavailable")))?;
        let offset = UtcOffset::current_local_offset().unwrap_or(UtcOffset::UTC);
        self.import(db, &profile_dir, passphrase, offset)
    }

    /// The success report as display text, or `None`.
    #[must_use]
    pub fn report_text(&self) -> Option<String> {
        self.report.as_ref().map(|report| report_text(report, self.language))
    }

    fn fail(&mut self, error: FinDeskImportError) -> Result<FinDeskImportReport, FinDeskImportError> {
        self.report = None;
        self.passphrase_wrong = false;
        self.error = Some(error.message(self.language));
        Err(error)
    }
}

impl Default for FindeskImportState {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

/// The full success report: the one-line summary (which already names documents, settings and payment orders) plus
/// why rows were skipped.
#[must_use]
pub fn report_text(report: &FinDeskImportReport, language: Language) -> String {
    let orphan = report.account_groups.skipped_orphan
        + report.bank_connections.skipped_orphan
        + report.accounts.skipped_orphan
        + report.categories.skipped_orphan
        + report.rules.skipped_orphan
        + report.transactions.skipped_orphan
        + report.deleted_bookings.skipped_orphan
        + report.pending_bookings.skipped_orphan
        + report.ocr_texts.skipped_orphan;
    let invalid = report.account_groups.skipped_invalid
        + report.bank_connections.skipped_invalid
        + report.accounts.skipped_invalid
        + report.categories.skipped_invalid
        + report.rules.skipped_invalid
        + report.transactions.skipped_invalid
        + report.deleted_bookings.skipped_invalid
        + report.pending_bookings.skipped_invalid
        + report.documents.skipped_invalid
        + report.ocr_texts.skipped_invalid;
    let fractional = report.account_groups.skipped_fractional_amount
        + report.bank_connections.skipped_fractional_amount
        + report.accounts.skipped_fractional_amount
        + report.categories.skipped_fractional_amount
        + report.rules.skipped_fractional_amount
        + report.transactions.skipped_fractional_amount
        + report.deleted_bookings.skipped_fractional_amount
        + report.pending_bookings.skipped_fractional_amount
        + report.ocr_texts.skipped_fractional_amount;

    let mut text = report.summary(language);
    if report.total_skipped() > 0 {
        text.push('\n');
        text.push_str(&i18n::format(
            language,
            Key::FinDeskImportSkipped,
            &[
                Arg::from(orphan),
                Arg::from(invalid),
                Arg::from(fractional),
                Arg::from(report.settings.skipped),
            ],
        ));
    }
    text
}

#[cfg(test)]
mod tests {
    use std::fs;
    use std::path::Path;

    use super::*;
    use crate::app::unlock;
    use crate::i18n::Language;
    use crate::secret::{Passphrase, PassphrasePurpose};

    const FIXTURE_PASSPHRASE: &str = "fixture passphrase 1";
    const TARGET_PASSPHRASE: &str = "target passphrase 1";

    fn fixture_copy() -> (tempfile::TempDir, PathBuf) {
        let fixture = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/findesk");
        let temp = tempfile::tempdir().unwrap();
        let destination = temp.path().join("findesk");
        copy_tree(&fixture, &destination);
        (temp, destination)
    }

    fn copy_tree(from: &Path, to: &Path) {
        fs::create_dir_all(to).unwrap();
        for entry in fs::read_dir(from).unwrap() {
            let entry = entry.unwrap();
            let target = to.join(entry.file_name());
            if entry.file_type().unwrap().is_dir() {
                copy_tree(&entry.path(), &target);
            } else {
                fs::copy(entry.path(), target).unwrap();
            }
        }
    }

    fn fixture_passphrase() -> Passphrase {
        Passphrase::new(FIXTURE_PASSPHRASE, PassphrasePurpose::Unlock).unwrap()
    }

    fn fresh_profile(temp: &tempfile::TempDir) -> (AppPaths, Database) {
        let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
        let db = unlock::create(&paths, "default", TARGET_PASSPHRASE, TARGET_PASSPHRASE, true).unwrap();
        (paths, db)
    }

    #[test]
    fn the_start_folder_is_findesk_home_or_the_hidden_home_folder() {
        let home = Path::new("/home/alice");
        assert_eq!(findesk_start_dir(home, None), PathBuf::from("/home/alice/.findesk"));

        let custom = Path::new("/data/findesk-data");
        assert_eq!(findesk_start_dir(home, Some(custom)), custom.to_path_buf());
    }

    #[test]
    fn a_data_folder_a_database_file_and_a_profile_root_are_accepted() {
        let temp = tempfile::tempdir().unwrap();
        let data = temp.path().join("findesk");
        fs::create_dir_all(&data).unwrap();
        fs::write(data.join("findesk.db"), b"db").unwrap();

        assert_eq!(resolve_findesk_dir(&data), Some(data.clone()));
        assert_eq!(resolve_findesk_dir(&data.join("findesk.db")), Some(data.clone()));

        let random = temp.path().join("random");
        fs::create_dir_all(&random).unwrap();
        assert_eq!(resolve_findesk_dir(&random), None);
    }

    #[test]
    fn a_root_with_profiles_uses_its_default_database_or_its_only_profile() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("findesk");
        let work = root.join("profiles/work");
        fs::create_dir_all(&work).unwrap();
        fs::write(work.join("findesk.db"), b"work").unwrap();

        assert_eq!(resolve_findesk_dir(&root), Some(work.clone()));

        let home = root.join("profiles/home");
        fs::create_dir_all(&home).unwrap();
        fs::write(home.join("findesk.db"), b"home").unwrap();
        assert_eq!(resolve_findesk_dir(&root), None, "never guess between two profiles");
        assert_eq!(resolve_findesk_dir(&home), Some(home.clone()));

        fs::write(root.join("findesk.db"), b"default").unwrap();
        assert_eq!(resolve_findesk_dir(&root), Some(root));
    }

    #[test]
    fn the_state_imports_the_fixture_and_reports_the_counts() {
        let (source_temp, source_dir) = fixture_copy();
        let target_temp = tempfile::tempdir().unwrap();
        let (paths, db) = fresh_profile(&target_temp);

        let mut state = FindeskImportState::new(Language::En);
        assert_eq!(state.choose(&source_dir), Some(source_dir.clone()));
        let report = state
            .import_in_profile(&db, &paths, None, &fixture_passphrase())
            .unwrap();

        assert_eq!(report.account_groups.imported, 1);
        assert_eq!(report.bank_connections.imported, 2);
        assert_eq!(report.accounts.imported, 3);
        assert_eq!(report.categories.imported, 3);
        assert_eq!(report.rules.imported, 2);
        assert_eq!(report.transactions.imported, 8);
        assert_eq!(report.documents.copied, 1);
        assert_eq!(report.documents.missing, 2);
        assert_eq!(report.settings.imported, 8);
        assert_eq!(report.payment_orders_left_behind, 1);
        assert_eq!(state.report.as_ref(), Some(&report));
        assert!(state.error.is_none());
        assert!(!state.passphrase_wrong);

        let text = state.report_text().unwrap();
        assert!(text.contains("payment orders"), "{text}");
        drop(source_temp);
    }

    #[test]
    fn a_non_empty_profile_reports_the_named_profile_hint() {
        let (_source_temp, source_dir) = fixture_copy();
        let target_temp = tempfile::tempdir().unwrap();
        let (paths, db) = fresh_profile(&target_temp);

        let mut first = FindeskImportState::new(Language::En);
        first.choose(&source_dir).unwrap();
        first
            .import_in_profile(&db, &paths, None, &fixture_passphrase())
            .unwrap();

        let mut second = FindeskImportState::new(Language::En);
        second.choose(&source_dir).unwrap();
        let error = second
            .import_in_profile(&db, &paths, None, &fixture_passphrase())
            .unwrap_err();

        assert!(matches!(error, FinDeskImportError::TargetNotEmpty));
        let text = second.error.clone().unwrap();
        assert!(text.contains("new profile"), "{text}");
        assert!(text.contains("Profiles"), "{text}");
        assert!(text.contains("import"), "{text}");
        assert!(second.report.is_none());
    }

    #[test]
    fn a_wrong_passphrase_keeps_the_folder_for_a_retry() {
        let (_source_temp, source_dir) = fixture_copy();
        let target_temp = tempfile::tempdir().unwrap();
        let (paths, db) = fresh_profile(&target_temp);

        let mut state = FindeskImportState::new(Language::En);
        state.choose(&source_dir).unwrap();
        let wrong = Passphrase::new("not the fixture passphrase", PassphrasePurpose::Unlock).unwrap();
        let error = state.import_in_profile(&db, &paths, None, &wrong).unwrap_err();

        assert!(matches!(error, FinDeskImportError::WrongPassphrase));
        assert!(state.passphrase_wrong);
        assert!(state.error.is_none());
        assert_eq!(state.source_dir, Some(source_dir));
        assert!(!state.is_importing);
    }

    #[test]
    fn report_text_breaks_down_every_skipped_row_once() {
        let report = FinDeskImportReport {
            transactions: findesk_import::ImportCounts {
                imported: 4,
                skipped: 3,
                skipped_orphan: 1,
                skipped_invalid: 1,
                skipped_fractional_amount: 1,
            },
            documents: findesk_import::DocumentImportCounts {
                copied: 2,
                skipped: 3,
                missing: 2,
                refused: 1,
                skipped_invalid: 0,
            },
            settings: findesk_import::SettingsImportCounts {
                imported: 5,
                skipped: 1,
            },
            payment_orders_left_behind: 7,
            ..FinDeskImportReport::default()
        };

        let text = report_text(&report, Language::En);
        assert!(text.contains("Imported 0 account groups"), "{text}");
        assert!(text.contains("2 documents, 0 OCR texts and 5 settings"), "{text}");
        assert!(
            text.contains("Skipped 7 rows, 2 documents with a missing file and 1 refused documents"),
            "{text}"
        );
        assert!(text.contains("Left 7 payment orders behind"), "{text}");
        // The breakdown plus the missing and refused documents add up to the total of 7.
        assert!(
            text.contains(
                "Skipped: 1 rows without their account, category or document, 1 invalid rows, \
                 1 with a fractional cent amount, and 1 settings Finglass does not use."
            ),
            "{text}"
        );
        assert_eq!(text.lines().count(), 2, "{text}");
    }
}
