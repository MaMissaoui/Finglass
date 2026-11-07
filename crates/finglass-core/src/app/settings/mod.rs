//! The Settings view's state, without any UI or coroutine machinery.
//!
//! This ports FinDesk's `SettingsViewModel` minus the protocol trace (M5b) and the dark-theme flag, which
//! [`crate::app::prefs`] already owns. Every write goes through the existing repositories and services, which audit;
//! this module adds audit entries only for the settings and profile changes that have no repository of their own.
//!
//! The state is synchronous: each action runs to completion and reports its result in [`SettingsState::error`] and
//! [`SettingsState::message`], exactly as the Kotlin view model publishes it after a job completes.

#[cfg(test)]
mod tests;

use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};

use thiserror::Error;

use crate::app::accounts::Busy;
use crate::app::audit::{self as app_audit, AuditRow};
use crate::audit::{self, Actor};
use crate::backup::{BackupError, BackupInfo, BackupService};
use crate::db::{Database, DbError};
use crate::i18n::{self, Arg, Key, Language};
use crate::model::{Category, CategoryId, NewCategory, NewRule, Rule, RuleDirection, RuleField, RuleId};
use crate::money::{Currencies, Currency, Money};
use crate::paths::AppPaths;
use crate::profiles::{self, Profile, ProfileError};
use crate::repo::{self, RepoError};
use crate::rules::file::RuleFile;
use crate::rules::import::{RuleImportError, RuleImportPreview, RuleImportService};
use crate::rules::validate_rule_pattern;
use crate::secret::{Passphrase, PassphrasePurpose};
use crate::settings::{get_setting, set_setting};

pub use crate::report::suggestions::{DEFAULT_KEYWORDS as DEFAULT_TAX_KEYWORDS, KEY_KEYWORDS as TAX_KEYWORDS_KEY};

/// The currencies FinDesk offers for the primary currency, before the accounts' own currencies are added.
///
/// `money::Currencies` in this crate had no common list when this module was ported; the list is kept here so the
/// settings state can offer the same choices without changing another module.
pub const COMMON_CURRENCIES: [&str; 11] = [
    "EUR", "USD", "GBP", "CHF", "PLN", "CZK", "SEK", "DKK", "NOK", "HUF", "JPY",
];

/// Everything that can go wrong in a Settings action.
#[derive(Debug, Error)]
pub enum SettingsError {
    /// A user-visible message that the caller renders from the i18n table.
    #[error("user-visible message")]
    Message(Key),

    /// No profile is registered under that slug.
    #[error("unknown profile")]
    UnknownProfile,

    /// A repository failure.
    #[error(transparent)]
    Repo(#[from] RepoError),

    /// A settings or database failure.
    #[error(transparent)]
    Db(#[from] DbError),

    /// A profile folder failure.
    #[error(transparent)]
    Profile(#[from] ProfileError),

    /// A backup or restore failure.
    #[error(transparent)]
    Backup(#[from] BackupError),

    /// A rule import or export failure.
    #[error(transparent)]
    RuleImport(#[from] RuleImportError),
}

/// A backup chosen for restore, waiting for confirmation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RestoreCandidate {
    /// The backup file.
    pub backup: PathBuf,
    /// What its manifest says.
    pub info: BackupInfo,
}

/// The Settings page state.
///
/// It mirrors FinDesk's `SettingsState` minus `darkTheme`, `protocolTraceEnabled` and `hasProtocolTrace`. Fields that
/// only the shell can know (`dataDirectory`, the documents folder, OCR availability, encryption state) are left to the
/// shell as well; this state only holds what the actions below load and change.
#[derive(Debug, Clone, PartialEq)]
pub struct SettingsState {
    /// Every category, in repository order (by name).
    pub categories: Vec<Category>,
    /// Bookings per category id; a category with no bookings is simply absent.
    pub category_usage: BTreeMap<CategoryId, usize>,
    /// Every rule, highest priority first.
    pub rules: Vec<Rule>,
    /// The newest audit entries, ready for the settings screen.
    pub audit_log: Vec<AuditRow>,
    /// The primary currency.
    pub primary_currency: Currency,
    /// The primary-currency choices: the common ones plus any an account uses.
    pub currency_options: Vec<Currency>,
    /// The tax suggestion keywords as one editable multi-line text.
    pub tax_keywords: String,
    /// Every profile, the default one first.
    pub profiles: Vec<Profile>,
    /// The profile running now (`None` is the default profile).
    pub open_profile: Option<String>,
    /// The profile that opens at the next start.
    pub next_profile: Option<String>,
    /// A rule file preview waiting for confirmation.
    pub rule_import: Option<RuleImportPreview>,
    /// Whether a confirmed rule import is applying its preview.
    pub is_importing_rules: bool,
    /// Whether a restore is staged and waits for the next start.
    pub has_pending_restore: bool,
    /// A backup chosen for restore, waiting for confirmation.
    pub restore_candidate: Option<RestoreCandidate>,
    /// A backup made under another passphrase, waiting for that passphrase.
    pub restore_passphrase_for: Option<PathBuf>,
    /// Whether the last passphrase for [`Self::restore_passphrase_for`] was wrong.
    pub restore_passphrase_wrong: bool,
    /// Whether a backup or restore is running.
    pub is_busy: bool,
    /// Whether a load or refresh is running.
    pub is_loading: bool,
    /// The last error, already localized.
    pub error: Option<String>,
    /// The last success message, already localized.
    pub message: Option<String>,
    /// The language for user-visible texts.
    pub language: Language,
    /// The profile's busy flag, shared with the other views; the app replaces it with its own (see [`Busy`]).
    pub busy: Busy,
}

impl SettingsState {
    /// Builds an empty state for `language`.
    #[must_use]
    pub fn new(language: Language) -> Self {
        Self {
            categories: Vec::new(),
            category_usage: BTreeMap::new(),
            rules: Vec::new(),
            audit_log: Vec::new(),
            primary_currency: Currencies::DEFAULT,
            currency_options: common_currencies(),
            tax_keywords: String::new(),
            profiles: Vec::new(),
            open_profile: None,
            next_profile: None,
            rule_import: None,
            is_importing_rules: false,
            has_pending_restore: false,
            restore_candidate: None,
            restore_passphrase_for: None,
            restore_passphrase_wrong: false,
            is_busy: false,
            is_loading: false,
            error: None,
            message: None,
            language,
            busy: Busy::default(),
        }
    }

    /// Loads every part of the Settings page.
    pub fn load(&mut self, db: &Database, paths: &AppPaths, open_profile: Option<&str>) -> Result<(), SettingsError> {
        self.is_loading = true;
        self.open_profile = open_profile.map(str::to_owned);
        let profile = self.open_profile.clone();
        if let Err(error) = self.refresh(db, paths, profile.as_deref()) {
            self.is_loading = false;
            return self.action_error(error);
        }
        Ok(())
    }

    /// Reloads the state from the database and the profile registry.
    pub fn refresh(&mut self, db: &Database, paths: &AppPaths, profile: Option<&str>) -> Result<(), SettingsError> {
        let categories = repo::categories::list(db)?;
        let category_usage = category_usage(db)?;
        let rules = repo::rules::list(db)?;
        let audit_log = app_audit::recent(db, 100)?;
        let primary_currency = Currencies::primary(db)?;
        let currency_options = currency_options(db)?;
        let tax_keywords = tax_keywords_text(db)?;
        let profiles = profiles::list(paths)?;
        let next_profile = profiles::active(paths)?.slug;
        let has_pending_restore = backup_service(paths, profile)?.has_pending_restore();

        self.categories = categories;
        self.category_usage = category_usage;
        self.rules = rules;
        self.audit_log = audit_log;
        self.primary_currency = primary_currency;
        self.currency_options = currency_options;
        self.tax_keywords = tax_keywords;
        self.profiles = profiles;
        self.next_profile = next_profile;
        self.has_pending_restore = has_pending_restore;
        self.is_loading = false;
        Ok(())
    }

    /// Stores the primary currency and records the change.
    pub fn set_primary_currency(&mut self, db: &Database, code: &str) -> Result<(), SettingsError> {
        let Ok(currency) = Currency::new(code) else {
            self.fail(Key::ErrorUnexpected);
            return Ok(());
        };

        set_setting(db, Currencies::KEY_PRIMARY, currency.as_str())?;
        audit::log(db, "SET_PRIMARY_CURRENCY", Actor::User, currency.as_str())?;
        self.primary_currency = currency;
        Ok(())
    }

    /// Validates a category name and colour; `exclude_id` is the category being edited, if any.
    #[must_use]
    pub fn validate_category(&self, name: &str, color: &str, exclude_id: Option<CategoryId>) -> Option<String> {
        if name.trim().is_empty() {
            return Some(i18n::text(self.language, Key::ErrorNameRequired).to_owned());
        }
        if !is_hex_color(color) {
            return Some(i18n::text(self.language, Key::ErrorColorFormat).to_owned());
        }

        let candidate = name.trim().to_lowercase();
        let duplicate = self
            .categories
            .iter()
            .any(|category| Some(category.id) != exclude_id && category.name.to_lowercase() == candidate);
        duplicate.then(|| i18n::text(self.language, Key::ErrorCategoryExists).to_owned())
    }

    /// Creates or updates a category and reloads the state.
    #[allow(clippy::too_many_arguments)]
    pub fn save_category(
        &mut self,
        db: &Database,
        id: Option<CategoryId>,
        name: &str,
        color: &str,
        budget_limit: Option<Money>,
        is_tax_relevant: bool,
        is_transfer: bool,
    ) -> Result<(), SettingsError> {
        if let Some(message) = self.validate_category(name, color, id) {
            self.error = Some(message);
            return Ok(());
        }

        let new = NewCategory {
            name: name.trim().to_owned(),
            color: color.to_owned(),
            budget_limit,
            is_tax_relevant,
            is_transfer,
        };
        let outcome = match id {
            Some(id) => repo::categories::update(db, id, new),
            None => repo::categories::create(db, new).map(|_| ()),
        };
        outcome?;
        self.reload(db)
    }

    /// Deletes a category; its bookings stay uncategorized and its rules are removed.
    pub fn delete_category(&mut self, db: &Database, id: CategoryId) -> Result<(), SettingsError> {
        repo::categories::delete(db, id)?;
        self.reload(db)
    }

    /// Stores the tax suggestion keywords: one per line, trimmed, non-blank and de-duplicated case-insensitively.
    ///
    /// The audit entry carries the saved count only, never keyword text.
    pub fn save_tax_keywords(&mut self, db: &Database, text: &str) -> Result<(), SettingsError> {
        let mut seen = BTreeSet::new();
        let saved: Vec<&str> = text
            .lines()
            .map(str::trim)
            .filter(|line| !line.is_empty())
            .filter(|line| seen.insert(line.to_lowercase()))
            .collect();

        set_setting(db, TAX_KEYWORDS_KEY, &saved.join("\n"))?;
        audit::log(
            db,
            "SAVE_TAX_KEYWORDS",
            Actor::User,
            &format!("Saved {} tax suggestion keywords", saved.len()),
        )?;
        self.reload(db)
    }

    /// Removes the custom keyword list so the built-in defaults apply again; the audit entry carries the removed
    /// count only.
    pub fn reset_tax_keywords(&mut self, db: &Database) -> Result<(), SettingsError> {
        let removed = get_setting(db, TAX_KEYWORDS_KEY)?
            .map(|stored| stored.lines().filter(|line| !line.trim().is_empty()).count())
            .unwrap_or(0);

        delete_setting(db, TAX_KEYWORDS_KEY)?;
        audit::log(
            db,
            "RESET_TAX_KEYWORDS",
            Actor::User,
            &format!("Reset {removed} tax suggestion keywords"),
        )?;
        self.reload(db)
    }

    /// Recolours categories that share a colour: in each group the lowest id keeps its colour and the others get
    /// the first palette colour that no category uses at that moment.
    pub fn fix_duplicate_colours(&mut self, db: &Database) -> Result<(), SettingsError> {
        let categories = repo::categories::list(db)?;
        let mut used: BTreeSet<String> = categories
            .iter()
            .map(|category| category.color.to_uppercase())
            .collect();
        let mut groups: Vec<(String, Vec<Category>)> = Vec::new();
        for category in categories {
            let key = category.color.to_uppercase();
            match groups.iter_mut().find(|(existing, _)| *existing == key) {
                Some((_, group)) => group.push(category),
                None => groups.push((key, vec![category])),
            }
        }

        let mut recoloured = 0_usize;
        for (_, mut group) in groups {
            if group.len() < 2 {
                continue;
            }
            group.sort_by_key(|category| category.id);
            for category in group.into_iter().skip(1) {
                let Some(free) = repo::categories::COLORS
                    .iter()
                    .find(|color| !used.contains(&color.to_uppercase()))
                    .copied()
                else {
                    break;
                };

                repo::categories::update(
                    db,
                    category.id,
                    NewCategory {
                        name: category.name,
                        color: free.to_owned(),
                        budget_limit: category.budget_limit,
                        is_tax_relevant: category.is_tax_relevant,
                        is_transfer: category.is_transfer,
                    },
                )?;
                used.insert(free.to_uppercase());
                recoloured += 1;
            }
        }

        self.reload(db)?;
        if recoloured > 0 {
            audit::log(
                db,
                "FIX_CATEGORY_COLOURS",
                Actor::User,
                &format!("Recoloured {recoloured} categories"),
            )?;
            self.message = Some(i18n::format(
                self.language,
                Key::MessageColoursFixed,
                &[Arg::from(recoloured)],
            ));
        } else {
            self.message = Some(i18n::text(self.language, Key::MessageColoursAlreadyDistinct).to_owned());
        }
        Ok(())
    }

    /// Adds the default categories that do not exist yet, in the current UI language.
    pub fn add_default_categories(&mut self, db: &Database) -> Result<(), SettingsError> {
        let added = repo::categories::seed_defaults(db, self.language)?;
        self.reload(db)?;
        self.message = Some(if added == 0 {
            i18n::text(self.language, Key::MessageDefaultCategoriesNone).to_owned()
        } else {
            i18n::format(self.language, Key::MessageDefaultCategoriesAdded, &[Arg::from(added)])
        });
        Ok(())
    }

    /// Validates a rule pattern, as a localized message or `None`.
    #[must_use]
    pub fn validate_rule(&self, pattern: &str) -> Option<String> {
        validate_rule_pattern(pattern, self.language)
    }

    /// Creates or updates a rule, including its tax flag (`None` means "as the category").
    #[allow(clippy::too_many_arguments)]
    pub fn save_rule(
        &mut self,
        db: &Database,
        id: Option<RuleId>,
        pattern: &str,
        category_id: CategoryId,
        priority: i64,
        auto_apply: bool,
        match_field: RuleField,
        direction: RuleDirection,
        tax_relevant: Option<bool>,
    ) -> Result<(), SettingsError> {
        if let Some(message) = self.validate_rule(pattern) {
            self.error = Some(message);
            return Ok(());
        }

        let new = NewRule {
            pattern: pattern.trim().to_owned(),
            category_id,
            priority,
            auto_apply,
            match_field,
            direction,
            tax_relevant,
        };
        match id {
            Some(id) => repo::rules::update(db, id, new)?,
            None => {
                repo::rules::create(db, new)?;
            }
        }
        self.reload(db)
    }

    /// Deletes a rule and reloads the state.
    pub fn delete_rule(&mut self, db: &Database, id: RuleId) -> Result<(), SettingsError> {
        repo::rules::delete(db, id)?;
        self.reload(db)
    }

    /// Writes the current rules to a shareable rule file and reports how many were written and skipped.
    pub fn export_rules(&mut self, db: &Database, path: &Path) -> Result<(), SettingsError> {
        let total = repo::rules::list(db)?.len();
        let written = RuleImportService::export(db, path)?;
        let skipped = total.saturating_sub(written);

        self.message = Some(if skipped == 0 {
            i18n::format(self.language, Key::MessageRulesExported, &[Arg::from(written)])
        } else {
            i18n::format(
                self.language,
                Key::MessageRulesExportSkipped,
                &[Arg::from(written), Arg::from(skipped)],
            )
        });
        self.error = None;
        Ok(())
    }

    /// Parses and classifies a rule file; nothing is written yet.
    pub fn choose_rule_import(&mut self, db: &Database, path: &Path) -> Result<(), SettingsError> {
        let size = match std::fs::metadata(path) {
            Ok(metadata) => metadata.len(),
            Err(_) => {
                self.fail(Key::ErrorUnexpected);
                return Ok(());
            }
        };
        if size > RuleFile::MAX_FILE_BYTES as u64 {
            self.fail(Key::ErrorRuleFileTooLarge);
            return Ok(());
        }

        let bytes = match std::fs::read(path) {
            Ok(bytes) => bytes,
            Err(_) => {
                self.fail(Key::ErrorUnexpected);
                return Ok(());
            }
        };
        let parsed = match RuleFile::parse(&bytes, self.language) {
            Ok(parsed) => parsed,
            Err(error) => {
                self.error = Some(error.message);
                return Ok(());
            }
        };
        let preview = match RuleImportService::preview(db, &parsed) {
            Ok(preview) => preview,
            Err(error) => return self.action_error(error.into()),
        };

        self.rule_import = Some(preview);
        self.error = None;
        self.message = None;
        Ok(())
    }

    /// Applies the confirmed preview to the database and clears the preview.
    pub fn confirm_rule_import(&mut self, db: &Database, categorize: bool) -> Result<(), SettingsError> {
        let Some(preview) = self.rule_import.clone() else {
            return Ok(());
        };
        self.is_importing_rules = true;

        let result = RuleImportService::apply(db, &preview, categorize, self.language);
        let result = match result {
            Ok(result) => result,
            Err(error) => {
                self.is_importing_rules = false;
                return self.action_error(error.into());
            }
        };

        if let Err(error) = self.reload(db) {
            self.is_importing_rules = false;
            return self.action_error(error);
        }
        self.rule_import = None;
        self.is_importing_rules = false;
        self.message = Some(i18n::format(
            self.language,
            Key::MessageRulesImported,
            &[
                Arg::from(result.rules),
                Arg::from(result.categories),
                Arg::from(result.categorized),
            ],
        ));
        self.error = None;
        Ok(())
    }

    /// Drops the preview without applying anything.
    pub fn cancel_rule_import(&mut self) {
        self.rule_import = None;
    }

    /// Writes a backup of the profile's data and reports its counts.
    pub fn create_backup(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        profile: Option<&str>,
        target: &Path,
    ) -> Result<(), SettingsError> {
        self.is_busy = true;
        self.error = None;

        let service = match backup_service(paths, profile) {
            Ok(service) => service,
            Err(error) => {
                self.is_busy = false;
                return self.action_error(error);
            }
        };
        let info = match self.busy.clone().run(|| service.create_backup(db, target)) {
            Ok(info) => info,
            Err(error) => {
                self.is_busy = false;
                return self.action_error(error.into());
            }
        };

        let name = file_name(target);
        if let Err(error) = audit::log(
            db,
            "BACKUP_CREATED",
            Actor::User,
            &format!(
                "Backup {name}: {} transactions, {} documents",
                info.transactions, info.documents
            ),
        ) {
            self.is_busy = false;
            return self.action_error(error.into());
        }
        if let Err(error) = self.refresh(db, paths, profile) {
            self.is_busy = false;
            return self.action_error(error);
        }
        self.is_busy = false;
        self.message = Some(i18n::format(
            self.language,
            Key::MessageBackupSaved,
            &[Arg::from(name), Arg::from(info.transactions), Arg::from(info.documents)],
        ));
        Ok(())
    }

    /// Reads a backup's manifest and asks for confirmation; nothing changes yet.
    pub fn choose_restore(
        &mut self,
        paths: &AppPaths,
        profile: Option<&str>,
        backup: &Path,
    ) -> Result<(), SettingsError> {
        let service = match backup_service(paths, profile) {
            Ok(service) => service,
            Err(error) => return self.action_error(error),
        };
        match service.inspect(backup) {
            Ok(info) => {
                self.restore_candidate = Some(RestoreCandidate {
                    backup: backup.to_path_buf(),
                    info,
                });
                Ok(())
            }
            Err(error) => self.action_error(error.into()),
        }
    }

    /// Confirms the chosen backup with the live database's passphrase; it replaces the data on the next start.
    pub fn confirm_restore(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        profile: Option<&str>,
    ) -> Result<(), SettingsError> {
        let Some(backup) = self
            .restore_candidate
            .as_ref()
            .map(|candidate| candidate.backup.clone())
        else {
            return Ok(());
        };
        self.stage_restore(db, paths, profile, &backup, None)
    }

    /// Confirms a backup made under another passphrase. The passphrase is checked and never stored.
    pub fn confirm_restore_with_passphrase(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        profile: Option<&str>,
        passphrase: &str,
    ) -> Result<(), SettingsError> {
        let Some(backup) = self.restore_passphrase_for.clone() else {
            return Ok(());
        };
        let passphrase = match Passphrase::new(passphrase, PassphrasePurpose::Unlock) {
            Ok(passphrase) => passphrase,
            Err(_) => {
                self.restore_passphrase_wrong = true;
                return Ok(());
            }
        };
        self.stage_restore(db, paths, profile, &backup, Some(&passphrase))
    }

    /// Drops the backup chosen for restore.
    pub fn cancel_restore_choice(&mut self) {
        self.restore_candidate = None;
    }

    /// Drops the passphrase request for a backup made under another passphrase.
    pub fn cancel_restore_passphrase(&mut self) {
        self.restore_passphrase_for = None;
        self.restore_passphrase_wrong = false;
    }

    /// Removes a staged restore that waits for the next start.
    pub fn cancel_pending_restore(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        profile: Option<&str>,
    ) -> Result<(), SettingsError> {
        let service = match backup_service(paths, profile) {
            Ok(service) => service,
            Err(error) => return self.action_error(error),
        };
        service.cancel_pending_restore();
        if let Err(error) = self.refresh(db, paths, profile) {
            return self.action_error(error);
        }
        self.message = Some(i18n::text(self.language, Key::MessageRestoreCancelled).to_owned());
        Ok(())
    }

    /// Creates a profile and records it.
    pub fn create_profile(&mut self, db: &Database, paths: &AppPaths, name: &str) -> Result<(), SettingsError> {
        let profile = match profiles::create(paths, name) {
            Ok(profile) => profile,
            Err(error) => return self.action_error(profile_error(error)),
        };
        audit::log(
            db,
            "PROFILE_CREATED",
            Actor::User,
            &format!(
                "Profile {} ({})",
                profile.name,
                profile.slug.as_deref().unwrap_or("default")
            ),
        )?;
        let open_profile = self.open_profile.clone();
        self.refresh(db, paths, open_profile.as_deref())
    }

    /// Renames the default profile (`None`) or a stored one and records it.
    pub fn rename_profile(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        slug: Option<&str>,
        name: &str,
    ) -> Result<(), SettingsError> {
        if let Err(error) = profiles::rename(paths, slug, name) {
            return self.action_error(profile_error(error));
        }
        audit::log(
            db,
            "PROFILE_RENAMED",
            Actor::User,
            &format!("Profile {} renamed to {}", slug.unwrap_or("default"), name.trim()),
        )?;
        let open_profile = self.open_profile.clone();
        self.refresh(db, paths, open_profile.as_deref())
    }

    /// Validates a profile and returns the slug to switch to; the live switch (closing the database and showing the
    /// unlock window) is the app's job.
    pub fn switch_profile(&self, paths: &AppPaths, slug: Option<&str>) -> Result<Option<String>, SettingsError> {
        if let Some(slug) = slug {
            let known = profiles::list(paths)?
                .iter()
                .any(|profile| profile.slug.as_deref() == Some(slug));
            if !known {
                return Err(SettingsError::UnknownProfile);
            }
        }
        Ok(slug.map(str::to_owned))
    }

    /// Moves a profile's folder aside and forgets its remembered PINs first.
    ///
    /// `forget_pins` runs before the folder moves, because the credentials live in the operating system's store keyed
    /// by the old folder. The app passes the real credential store later (M5c); tests pass a recording fake.
    pub fn delete_profile(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        open_profile: Option<&str>,
        slug: &str,
        forget_pins: &dyn Fn(&str),
    ) -> Result<(), SettingsError> {
        // Validate before forgetting anything: a refused delete must not touch the credential store.
        if let Err(error) = validate_delete(paths, open_profile, slug) {
            return self.action_error(profile_error(error));
        }
        forget_pins(slug);
        let moved = match profiles::delete(paths, slug, open_profile) {
            Ok(moved) => moved,
            Err(error) => return self.action_error(profile_error(error)),
        };
        audit::log(
            db,
            "PROFILE_DELETED",
            Actor::User,
            &format!("Profile {slug} moved to {}", moved.display()),
        )?;
        let open_profile = self.open_profile.clone();
        if let Err(error) = self.refresh(db, paths, open_profile.as_deref()) {
            return self.action_error(error);
        }
        self.message = Some(i18n::format(
            self.language,
            Key::MessageProfileMoved,
            &[Arg::from(moved.display().to_string())],
        ));
        Ok(())
    }

    /// Clears the error and success message.
    pub fn clear_messages(&mut self) {
        self.error = None;
        self.message = None;
    }

    fn stage_restore(
        &mut self,
        db: &Database,
        paths: &AppPaths,
        profile: Option<&str>,
        backup: &Path,
        passphrase: Option<&Passphrase>,
    ) -> Result<(), SettingsError> {
        self.is_busy = true;
        self.restore_candidate = None;
        self.restore_passphrase_for = None;

        let service = match backup_service(paths, profile) {
            Ok(service) => service,
            Err(error) => {
                self.is_busy = false;
                return self.action_error(error);
            }
        };
        match self.busy.clone().run(|| service.stage_restore(db, backup, passphrase)) {
            Ok(_) => {}
            Err(BackupError::PassphraseRequired { wrong_passphrase_given }) => {
                self.is_busy = false;
                self.restore_passphrase_for = Some(backup.to_path_buf());
                self.restore_passphrase_wrong = wrong_passphrase_given;
                return Ok(());
            }
            Err(error) => {
                self.is_busy = false;
                return self.action_error(error.into());
            }
        }

        self.restore_passphrase_wrong = false;
        if let Err(error) = audit::log(
            db,
            "RESTORE_STAGED",
            Actor::User,
            &format!("Restore from {} staged", file_name(backup)),
        ) {
            self.is_busy = false;
            return self.action_error(error.into());
        }
        if let Err(error) = self.refresh(db, paths, profile) {
            self.is_busy = false;
            return self.action_error(error);
        }
        self.is_busy = false;
        self.message = Some(i18n::text(self.language, Key::MessageRestorePrepared).to_owned());
        Ok(())
    }

    /// Reloads the database-backed fields after an action that does not change the profile registry.
    fn reload(&mut self, db: &Database) -> Result<(), SettingsError> {
        let categories = repo::categories::list(db)?;
        let category_usage = category_usage(db)?;
        let rules = repo::rules::list(db)?;
        let audit_log = app_audit::recent(db, 100)?;
        let primary_currency = Currencies::primary(db)?;
        let currency_options = currency_options(db)?;
        let tax_keywords = tax_keywords_text(db)?;

        self.categories = categories;
        self.category_usage = category_usage;
        self.rules = rules;
        self.audit_log = audit_log;
        self.primary_currency = primary_currency;
        self.currency_options = currency_options;
        self.tax_keywords = tax_keywords;
        self.is_loading = false;
        Ok(())
    }

    fn action_error(&mut self, error: SettingsError) -> Result<(), SettingsError> {
        if let SettingsError::Message(key) = &error {
            self.fail(*key);
            return Ok(());
        }
        self.error = Some(i18n::text(self.language, Key::ErrorUnexpected).to_owned());
        Err(error)
    }

    fn fail(&mut self, key: Key) {
        self.error = Some(i18n::text(self.language, key).to_owned());
    }
}

impl Default for SettingsState {
    fn default() -> Self {
        Self::new(Language::En)
    }
}

/// The built-in currency choices.
fn common_currencies() -> Vec<Currency> {
    COMMON_CURRENCIES
        .iter()
        .map(|code| Currency::new(code).expect("the common currency codes are valid"))
        .collect()
}

/// The common choices plus every currency an account uses, keeping the first occurrence.
fn currency_options(db: &Database) -> Result<Vec<Currency>, SettingsError> {
    let mut options = common_currencies();
    for account in repo::accounts::list(db)? {
        if !options.contains(&account.currency) {
            options.push(account.currency);
        }
    }
    Ok(options)
}

/// The keywords the tax suggestions use: the stored lines, or the built-in defaults (see [`crate::report::suggestions`]).
pub fn tax_keywords(db: &Database) -> Result<Vec<String>, RepoError> {
    crate::report::suggestions::keywords(db)
}

fn tax_keywords_text(db: &Database) -> Result<String, RepoError> {
    Ok(tax_keywords(db)?.join("\n"))
}

/// Deletes a setting. FinDesk's `SettingsRepository.delete` has no counterpart in `crate::settings` yet.
fn delete_setting(db: &Database, key: &str) -> Result<(), DbError> {
    db.with_conn(|conn| {
        conn.execute("DELETE FROM settings WHERE key = ?1", [key])?;
        Ok(())
    })
}

fn category_usage(db: &Database) -> Result<BTreeMap<CategoryId, usize>, SettingsError> {
    let rows = db.with_conn(|conn| {
        let mut statement = conn.prepare(
            "SELECT category_id, count(*) FROM transactions
             WHERE category_id IS NOT NULL GROUP BY category_id",
        )?;
        let rows = statement
            .query_map([], |row| Ok((row.get::<_, i64>(0)?, row.get::<_, i64>(1)?)))?
            .collect::<Result<Vec<_>, _>>()?;
        Ok(rows)
    })?;

    Ok(rows
        .into_iter()
        .map(|(id, count)| (CategoryId(id), usize::try_from(count).unwrap_or(0)))
        .collect())
}

fn backup_service(paths: &AppPaths, profile: Option<&str>) -> Result<BackupService, SettingsError> {
    Ok(BackupService::new(profiles::data_dir(paths, profile)?))
}

/// Checks a delete target the same way [`profiles::delete`] will, before any side effect runs.
fn validate_delete(paths: &AppPaths, open_profile: Option<&str>, slug: &str) -> Result<(), ProfileError> {
    if open_profile == Some(slug) {
        return Err(ProfileError::OpenProfile);
    }
    if slug == "default" {
        return Err(ProfileError::DefaultProfile);
    }
    let known = profiles::list(paths)?
        .iter()
        .any(|profile| profile.slug.as_deref() == Some(slug));
    if !known {
        return Err(ProfileError::Unknown(slug.to_owned()));
    }
    Ok(())
}

fn profile_error(error: ProfileError) -> SettingsError {
    match error {
        ProfileError::EmptyName => SettingsError::Message(Key::ErrorNameRequired),
        other => SettingsError::Profile(other),
    }
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| path.display().to_string())
}

fn is_hex_color(color: &str) -> bool {
    let bytes = color.as_bytes();
    bytes.len() == 7 && bytes[0] == b'#' && bytes[1..].iter().all(u8::is_ascii_hexdigit)
}
