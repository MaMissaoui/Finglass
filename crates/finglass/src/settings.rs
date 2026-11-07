//! Renders `finglass_core::app::settings` into the Settings page and runs its actions off the event loop.
//!
//! This module only maps core state to Slint models and wires the page's callbacks. Every decision about what a
//! category or rule may contain is made by `SettingsState`; the file dialogs and database work run on background
//! threads and publish their result through `slint::invoke_from_event_loop`, like the rest of the shell.

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::Arc;
use std::thread;

use finglass_core::app::accounts::Busy;
use finglass_core::app::findesk::{self, FindeskImportState};
use finglass_core::app::settings::SettingsState;
use finglass_core::db::Database;
use finglass_core::findesk_import::{FinDeskImportError, FinDeskImportReport};
use finglass_core::format;
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::model::{Category, CategoryId, RuleDirection, RuleField, RuleId};
use finglass_core::money::{Currency, Money};
use finglass_core::paths::AppPaths;
use finglass_core::repo;
use finglass_core::secret::{Passphrase, PassphrasePurpose};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel};
use time::OffsetDateTime;
use zeroize::Zeroizing;

use crate::shell::ShellState;
use crate::{CategoryRow, ColourSwatch, MainWindow, ProfileRow, RuleRow};

/// Applies `state` to the window without touching the database.
pub fn apply(main: &MainWindow, state: &SettingsState, restored_from: Option<&Path>) {
    main.set_settings_error(state.error.clone().unwrap_or_default().into());
    main.set_settings_message(state.message.clone().unwrap_or_default().into());
    main.set_settings_categories_empty(state.categories.is_empty());
    main.set_settings_has_duplicate_colours(has_duplicate_colours(&state.categories));

    main.set_settings_currency_options(string_model(
        state.currency_options.iter().map(|currency| currency.as_str()),
    ));
    main.set_settings_primary_currency(state.primary_currency.as_str().into());
    main.set_settings_primary_currency_index(primary_currency_index(state));
    main.set_settings_category_rows(category_model(state));
    main.set_settings_rule_rows(rule_model(state));
    main.set_settings_tax_keywords(state.tax_keywords.clone().into());
    main.set_settings_tax_keywords_count(tax_keywords_count(state).into());

    main.set_category_dialog_swatches(colour_model());
    main.set_rule_dialog_categories(string_model(
        state.categories.iter().map(|category| category.name.as_str()),
    ));
    main.set_rule_dialog_field_options(string_model(field_options(state.language).into_iter()));
    main.set_rule_dialog_direction_options(string_model(direction_options(state.language).into_iter()));
    main.set_rule_dialog_tax_options(string_model(tax_options(state.language).into_iter()));

    apply_profiles(main, state);
    apply_backup(main, state, restored_from);
    apply_rule_import(main, state);
}

/// Maps the profile list to both the Settings section and the top bar's dropdown.
fn apply_profiles(main: &MainWindow, state: &SettingsState) {
    main.set_settings_profile_rows(profile_model(state));
    main.set_profile_rows(profile_model(state));
    main.set_current_profile_name(
        state
            .profiles
            .iter()
            .find(|profile| profile.slug == state.open_profile)
            .or_else(|| state.profiles.first())
            .map(|profile| profile.name.clone())
            .unwrap_or_default()
            .into(),
    );
}

fn profile_model(state: &SettingsState) -> ModelRc<ProfileRow> {
    ModelRc::new(VecModel::from(
        state
            .profiles
            .iter()
            .enumerate()
            .map(|(index, profile)| {
                let open = profile.slug == state.open_profile;
                ProfileRow {
                    index: i32::try_from(index).unwrap_or(-1),
                    name: profile.name.clone().into(),
                    open,
                    next_start: !open && profile.slug == state.next_profile,
                    can_switch: !open,
                    can_delete: !profile.is_default() && !open,
                }
            })
            .collect::<Vec<_>>(),
    ))
}

/// Maps the backup and restore state, including the two restore dialogs.
fn apply_backup(main: &MainWindow, state: &SettingsState, restored_from: Option<&Path>) {
    main.set_settings_backup_busy(state.is_busy);
    main.set_settings_has_pending_restore(state.has_pending_restore);
    main.set_settings_backup_restored_text(
        restored_from
            .map(|folder| {
                i18n::format(
                    state.language,
                    Key::BackupRestoredAt,
                    &[Arg::from(folder.display().to_string())],
                )
            })
            .unwrap_or_default()
            .into(),
    );

    match &state.restore_candidate {
        Some(candidate) => {
            let name = file_name(&candidate.backup);
            main.set_settings_restore_title(
                i18n::format(state.language, Key::RestoreFileTitle, &[Arg::from(name)]).into(),
            );
            let created = i18n::format(
                state.language,
                Key::RestoreCreated,
                &[
                    Arg::from(candidate.info.created.clone()),
                    Arg::from(candidate.info.transactions),
                    Arg::from(candidate.info.documents),
                ],
            );
            let explanation = i18n::text(state.language, Key::RestoreExplanation);
            main.set_settings_restore_text(format!("{created}\n\n{explanation}").into());
            main.set_settings_restore_open(true);
        }
        None => main.set_settings_restore_open(false),
    }

    main.set_settings_restore_passphrase_wrong(state.restore_passphrase_wrong);
    match &state.restore_passphrase_for {
        Some(backup) => {
            main.set_settings_restore_passphrase_file(file_name(backup).into());
            main.set_settings_restore_passphrase_open(true);
        }
        None => {
            main.set_settings_restore_passphrase_open(false);
            main.set_settings_restore_passphrase("".into());
        }
    }
}

/// Maps the FinDesk import state to its folder, passphrase and result dialogs.
pub fn apply_findesk(main: &MainWindow, state: &FindeskImportState) {
    main.set_findesk_busy(state.is_importing);
    main.set_findesk_folder(
        state
            .source_dir
            .as_ref()
            .map(|folder| folder.display().to_string())
            .unwrap_or_default()
            .into(),
    );
    main.set_findesk_passphrase_wrong(state.passphrase_wrong);

    match (&state.report, &state.error) {
        (Some(report), _) => {
            main.set_findesk_report_title(i18n::text(state.language, Key::FinDeskImportReportTitle).into());
            main.set_findesk_report_text(findesk::report_text(report, state.language).into());
            main.set_findesk_report_error(false);
            main.set_findesk_report_open(true);
        }
        (None, Some(error)) => {
            main.set_findesk_report_title(i18n::text(state.language, Key::FinDeskImportFailedTitle).into());
            main.set_findesk_report_text(error.clone().into());
            main.set_findesk_report_error(true);
            main.set_findesk_report_open(true);
        }
        (None, None) => main.set_findesk_report_open(false),
    }
}

/// Asks for a FinDesk folder on a background thread and opens the passphrase dialog for a valid one.
pub fn import_findesk(main: &MainWindow, state: &Arc<ShellState>) {
    let language = *state.language.lock().expect("language state poisoned");
    let start = findesk::default_findesk_start_dir();

    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let mut dialog =
            rfd::FileDialog::new().set_title(i18n::text(language, Key::FinDeskImportChooseFolder).to_owned());
        if let Some(start) = start {
            dialog = dialog.set_directory(start);
        }
        let chosen = dialog.pick_folder();

        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            let Some(ui) = main_weak.upgrade() else {
                return;
            };
            let Some(chosen) = chosen else {
                return;
            };

            let mut pending = state.findesk.lock().expect("findesk state poisoned").clone();
            pending.language = language;
            if pending.choose(&chosen).is_some() {
                *state.findesk.lock().expect("findesk state poisoned") = pending.clone();
                ui.set_findesk_passphrase("".into());
                ui.set_findesk_passphrase_open(true);
                apply_findesk(&ui, &pending);
            } else {
                let mut settings = state.settings.lock().expect("settings state poisoned").clone();
                settings.error = Some(i18n::text(language, Key::ErrorFinDeskNotAFolder).to_owned());
                settings.message = None;
                *state.settings.lock().expect("settings state poisoned") = settings.clone();
                apply(&ui, &settings, state.restored_from().as_deref());
            }
        });
    });
}

/// Runs the chosen import with FinDesk's passphrase while the profile's busy flag is held.
fn import_with_profile_busy(
    pending: &mut FindeskImportState,
    db: &Database,
    paths: &AppPaths,
    slug: Option<&str>,
    passphrase: &str,
    busy: &Busy,
) -> Result<FinDeskImportReport, FinDeskImportError> {
    run_while_profile_busy(busy, || match Passphrase::new(passphrase, PassphrasePurpose::Unlock) {
        Ok(passphrase) => pending.import_in_profile(db, paths, slug, &passphrase),
        Err(_) => {
            pending.is_importing = false;
            pending.report = None;
            pending.error = None;
            pending.passphrase_wrong = true;
            Err(FinDeskImportError::WrongPassphrase)
        }
    })
}

/// Runs `work` as one profile-busy section, so the section ends on every path.
fn run_while_profile_busy<T>(busy: &Busy, work: impl FnOnce() -> T) -> T {
    busy.run(work)
}

/// Runs the chosen import with FinDesk's passphrase; the field is cleared before any work starts.
pub fn submit_findesk_passphrase(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>, passphrase: &str) {
    main.set_findesk_passphrase("".into());
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };

    let mut pending = state.findesk.lock().expect("findesk state poisoned").clone();
    if pending.source_dir.is_none() || pending.is_importing {
        return;
    }
    pending.language = *state.language.lock().expect("language state poisoned");
    pending.is_importing = true;
    *state.findesk.lock().expect("findesk state poisoned") = pending.clone();
    apply_findesk(main, &pending);

    let slug = state.slug();
    let paths = paths.clone();
    let passphrase = Zeroizing::new(passphrase.to_owned());
    let busy = state.profile_busy.lock().expect("profile busy poisoned").clone();
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let result = import_with_profile_busy(&mut pending, &db, &paths, slug.as_deref(), passphrase.as_str(), &busy);
        let success = result.is_ok();

        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            let Some(ui) = main_weak.upgrade() else {
                return;
            };

            if success {
                ui.set_findesk_passphrase_open(false);
                if let Some(language) = crate::shell::reload_after_findesk_import(&ui, &state, &paths) {
                    pending.language = language;
                }
            } else if !pending.passphrase_wrong {
                ui.set_findesk_passphrase_open(false);
            }

            *state.findesk.lock().expect("findesk state poisoned") = pending.clone();
            apply_findesk(&ui, &pending);
        });
    });
}

/// Closes the passphrase dialog and forgets the chosen folder.
pub fn cancel_findesk_passphrase(main: &MainWindow, state: &Arc<ShellState>) {
    main.set_findesk_passphrase("".into());
    main.set_findesk_passphrase_open(false);

    let mut pending = state.findesk.lock().expect("findesk state poisoned").clone();
    pending.cancel();
    *state.findesk.lock().expect("findesk state poisoned") = pending.clone();
    apply_findesk(main, &pending);
}

/// Closes the success report or failure dialog without changing anything else.
pub fn close_findesk_report(main: &MainWindow, state: &Arc<ShellState>) {
    let mut pending = state.findesk.lock().expect("findesk state poisoned").clone();
    pending.clear_result();
    *state.findesk.lock().expect("findesk state poisoned") = pending.clone();
    apply_findesk(main, &pending);
}

/// Loads the page on a background thread and publishes the result on the event loop.
pub fn spawn_load(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let language = *state.language.lock().expect("language state poisoned");
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.language = language;
    pending.is_loading = true;
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());

    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let paths = paths.clone();
    let slug = state.slug();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let _ = pending.load(&db, &paths, slug.as_deref());
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                *state.settings.lock().expect("settings state poisoned") = pending.clone();
                apply(&ui, &pending, state.restored_from().as_deref());
            }
        });
    });
}

/// Clears the error and message banner.
pub fn dismiss_messages(main: &MainWindow, state: &Arc<ShellState>) {
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.clear_messages();
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());
}

/// Stores the primary currency.
pub fn set_primary_currency(main: &MainWindow, state: &Arc<ShellState>, code: &str) {
    main.set_settings_primary_currency(code.into());
    let Some(pending) = pending_state(state) else {
        return;
    };
    let code = code.to_owned();
    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.set_primary_currency(db, &code).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Adds the default categories that do not exist yet.
pub fn add_default_categories(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    spawn_pending(
        main,
        state,
        pending,
        |pending, db| {
            if pending.add_default_categories(db).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Recolours categories that share a colour.
pub fn fix_duplicate_colours(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    spawn_pending(
        main,
        state,
        pending,
        |pending, db| {
            if pending.fix_duplicate_colours(db).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Opens the category dialog for a new category.
pub fn open_add_category(main: &MainWindow, _state: &Arc<ShellState>) {
    main.set_category_dialog_id(-1);
    main.set_category_dialog_name("".into());
    main.set_category_dialog_colour(repo::categories::COLORS[0].into());
    main.set_category_dialog_budget("".into());
    main.set_category_dialog_tax(false);
    main.set_category_dialog_transfer(false);
    main.set_category_dialog_error("".into());
    main.set_category_dialog_open(true);
}

/// Opens the category dialog for an existing category.
pub fn open_edit_category(main: &MainWindow, state: &Arc<ShellState>, id: CategoryId) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    let Some(category) = pending.categories.iter().find(|category| category.id == id) else {
        return;
    };
    main.set_category_dialog_id(id32(id.0));
    main.set_category_dialog_name(category.name.clone().into());
    main.set_category_dialog_colour(category.color.clone().into());
    main.set_category_dialog_budget(budget_input(category.budget_limit).into());
    main.set_category_dialog_tax(category.is_tax_relevant);
    main.set_category_dialog_transfer(category.is_transfer);
    main.set_category_dialog_error("".into());
    main.set_category_dialog_open(true);
}

/// Opens the delete confirmation for a category.
pub fn request_delete_category(main: &MainWindow, state: &Arc<ShellState>, id: CategoryId) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    let Some(category) = pending.categories.iter().find(|category| category.id == id) else {
        return;
    };
    let usage = pending.category_usage.get(&id).copied().unwrap_or(0);
    let mut text = i18n::text(pending.language, Key::DeleteCategoryText).to_owned();
    if usage > 0 {
        text.push(' ');
        text.push_str(&i18n::format(
            pending.language,
            Key::DeleteCategoryUsage,
            &[Arg::from(usage)],
        ));
    }

    main.set_category_delete_id(id32(id.0));
    main.set_category_delete_title(
        i18n::format(
            pending.language,
            Key::DeleteCategoryTitle,
            &[Arg::from(category.name.clone())],
        )
        .into(),
    );
    main.set_category_delete_text(text.into());
    main.set_category_delete_open(true);
}

/// Re-runs `validate_category` while the category dialog is edited.
pub fn validate_category(main: &MainWindow, state: &Arc<ShellState>, name: &str, colour: &str, id: i32) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    let exclude = (id >= 0).then(|| CategoryId(i64::from(id)));
    main.set_category_dialog_error(
        pending
            .validate_category(name, colour, exclude)
            .unwrap_or_default()
            .into(),
    );
}

/// Saves the category dialog's fields.
pub fn save_category(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    pending.clear_messages();

    let id = main.get_category_dialog_id();
    let name = main.get_category_dialog_name().to_string();
    let colour = main.get_category_dialog_colour().to_string();
    let budget_text = main.get_category_dialog_budget().to_string();
    let tax_relevant = main.get_category_dialog_tax();
    let transfer = main.get_category_dialog_transfer();
    let exclude = (id >= 0).then(|| CategoryId(i64::from(id)));

    if let Some(error) = pending.validate_category(&name, &colour, exclude) {
        main.set_category_dialog_error(error.into());
        return;
    }
    let budget = match parse_budget(&budget_text) {
        Ok(budget) => budget,
        Err(key) => {
            main.set_category_dialog_error(i18n::text(pending.language, key).into());
            return;
        }
    };
    main.set_category_dialog_error("".into());

    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending
                .save_category(db, exclude, &name, &colour, budget, tax_relevant, transfer)
                .is_err()
            {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |ui, pending| {
            if pending.error.is_none() {
                ui.set_category_dialog_open(false);
            } else if let Some(error) = &pending.error {
                ui.set_category_dialog_error(error.clone().into());
            }
        },
    );
}

/// Deletes the category whose confirmation is open.
pub fn delete_category(main: &MainWindow, state: &Arc<ShellState>, id: CategoryId) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    main.set_category_delete_open(false);
    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.delete_category(db, id).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |ui, _| {
            ui.set_category_delete_open(false);
        },
    );
}

/// Opens the rule dialog for a new rule.
pub fn open_add_rule(main: &MainWindow, state: &Arc<ShellState>) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    if pending.categories.is_empty() {
        return;
    }
    main.set_rule_dialog_categories(string_model(
        pending.categories.iter().map(|category| category.name.as_str()),
    ));
    main.set_rule_dialog_id(-1);
    main.set_rule_dialog_pattern("".into());
    main.set_rule_dialog_category_index(0);
    main.set_rule_dialog_priority("0".into());
    main.set_rule_dialog_auto_apply(true);
    main.set_rule_dialog_field_index(0);
    main.set_rule_dialog_direction_index(0);
    main.set_rule_dialog_tax_index(0);
    main.set_rule_dialog_error("".into());
    main.set_rule_dialog_open(true);
}

/// Opens the rule dialog for an existing rule.
pub fn open_edit_rule(main: &MainWindow, state: &Arc<ShellState>, id: RuleId) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    let Some(rule) = pending.rules.iter().find(|rule| rule.id == id) else {
        return;
    };
    main.set_rule_dialog_categories(string_model(
        pending.categories.iter().map(|category| category.name.as_str()),
    ));
    main.set_rule_dialog_id(id32(id.0));
    main.set_rule_dialog_pattern(rule.pattern.clone().into());
    main.set_rule_dialog_category_index(
        pending
            .categories
            .iter()
            .position(|category| category.id == rule.category_id)
            .map_or(0, |index| i32::try_from(index).unwrap_or(0)),
    );
    main.set_rule_dialog_priority(rule.priority.to_string().into());
    main.set_rule_dialog_auto_apply(rule.auto_apply);
    main.set_rule_dialog_field_index(field_index(rule.match_field));
    main.set_rule_dialog_direction_index(direction_index(rule.direction));
    main.set_rule_dialog_tax_index(tax_index(rule.tax_relevant));
    main.set_rule_dialog_error("".into());
    main.set_rule_dialog_open(true);
}

/// Opens the delete confirmation for a rule.
pub fn request_delete_rule(main: &MainWindow, state: &Arc<ShellState>, id: RuleId) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    let Some(rule) = pending.rules.iter().find(|rule| rule.id == id) else {
        return;
    };
    main.set_rule_delete_id(id32(id.0));
    main.set_rule_delete_title(
        i18n::format(
            pending.language,
            Key::DeleteRuleTitle,
            &[Arg::from(rule.pattern.clone())],
        )
        .into(),
    );
    main.set_rule_delete_text(i18n::text(pending.language, Key::DeleteRuleText).into());
    main.set_rule_delete_open(true);
}

/// Re-runs `validate_rule` while the rule dialog is edited.
pub fn validate_rule(main: &MainWindow, state: &Arc<ShellState>, pattern: &str, priority: &str) {
    let pending = state.settings.lock().expect("settings state poisoned").clone();
    main.set_rule_dialog_error(rule_error(&pending, pattern, priority).into());
}

/// Saves the rule dialog's fields.
pub fn save_rule(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    pending.clear_messages();

    let id = main.get_rule_dialog_id();
    let pattern = main.get_rule_dialog_pattern().to_string();
    let category_index = main.get_rule_dialog_category_index();
    let priority_text = main.get_rule_dialog_priority().to_string();
    let auto_apply = main.get_rule_dialog_auto_apply();
    let field = field_at(main.get_rule_dialog_field_index());
    let direction = direction_at(main.get_rule_dialog_direction_index());
    let tax_relevant = tax_at(main.get_rule_dialog_tax_index());

    let error = rule_error(&pending, &pattern, &priority_text);
    if !error.is_empty() {
        main.set_rule_dialog_error(error.into());
        return;
    }
    let Ok(priority) = priority_text.trim().parse::<i64>() else {
        main.set_rule_dialog_error(i18n::text(pending.language, Key::PriorityMustBeNumber).into());
        return;
    };
    let Some(category) = pending.categories.get(usize::try_from(category_index).unwrap_or(0)) else {
        return;
    };
    let category_id = category.id;
    main.set_rule_dialog_error("".into());

    let rule_id = (id >= 0).then(|| RuleId(i64::from(id)));
    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending
                .save_rule(
                    db,
                    rule_id,
                    &pattern,
                    category_id,
                    priority,
                    auto_apply,
                    field,
                    direction,
                    tax_relevant,
                )
                .is_err()
            {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |ui, pending| {
            if pending.error.is_none() {
                ui.set_rule_dialog_open(false);
            } else if let Some(error) = &pending.error {
                ui.set_rule_dialog_error(error.clone().into());
            }
        },
    );
}

/// Deletes the rule whose confirmation is open.
pub fn delete_rule(main: &MainWindow, state: &Arc<ShellState>, id: RuleId) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    main.set_rule_delete_open(false);
    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.delete_rule(db, id).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |ui, _| {
            ui.set_rule_delete_open(false);
        },
    );
}

/// Stores the tax suggestion keywords.
pub fn save_tax_keywords(main: &MainWindow, state: &Arc<ShellState>, text: &str) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    let text = text.to_owned();
    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.save_tax_keywords(db, &text).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Removes the custom tax keywords so the defaults apply again.
pub fn reset_tax_keywords(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    spawn_pending(
        main,
        state,
        pending,
        |pending, db| {
            if pending.reset_tax_keywords(db).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Asks for a backup target and writes a backup of the open profile.
pub fn create_backup(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let paths = paths.clone();
    let profile = state.slug();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let today = OffsetDateTime::now_utc().date();
        let file_name = format!(
            "finglass-backup-{:04}-{:02}-{:02}.zip",
            today.year(),
            u8::from(today.month()),
            today.day()
        );
        let filter = i18n::text(pending.language, Key::SaveBackup).to_owned();
        let Some(path) = rfd::FileDialog::new()
            .add_filter(&filter, &["zip"])
            .set_file_name(&file_name)
            .save_file()
        else {
            state.jobs.end();
            return;
        };

        pending.clear_messages();
        pending.is_busy = true;
        publish(main_weak.clone(), state.clone(), pending.clone(), generation, false);
        if pending.create_backup(&db, &paths, profile.as_deref(), &path).is_err() {
            pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
        }
        publish(main_weak, state, pending, generation, true);
    });
}

/// Asks for a backup file and prepares its confirmation dialog.
pub fn restore_backup(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let paths = paths.clone();
    let profile = state.slug();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let filter = i18n::text(pending.language, Key::RestoreBackup).to_owned();
        let Some(path) = rfd::FileDialog::new().add_filter(&filter, &["zip"]).pick_file() else {
            state.jobs.end();
            return;
        };

        pending.clear_messages();
        pending.is_busy = true;
        publish(main_weak.clone(), state.clone(), pending.clone(), generation, false);
        let result = pending.choose_restore(&paths, profile.as_deref(), &path);
        pending.is_busy = false;
        if result.is_err() {
            pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
        }
        publish(main_weak, state, pending, generation, true);
    });
}

/// Confirms the restore candidate currently in the confirmation dialog.
pub fn confirm_restore(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    let paths = paths.clone();
    let profile = state.slug();
    let pending = begin_busy(main, state, pending);

    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.confirm_restore(db, &paths, profile.as_deref()).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Drops the restore confirmation without changing anything.
pub fn cancel_restore_choice(main: &MainWindow, state: &Arc<ShellState>) {
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.cancel_restore_choice();
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());
}

/// Confirms a backup made with another passphrase. The passphrase is not kept in the UI state.
pub fn confirm_restore_passphrase(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>, passphrase: &str) {
    main.set_settings_restore_passphrase("".into());
    let Some(pending) = pending_state(state) else {
        return;
    };
    let paths = paths.clone();
    let profile = state.slug();
    let passphrase = Zeroizing::new(passphrase.to_owned());
    let pending = begin_busy(main, state, pending);

    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending
                .confirm_restore_with_passphrase(db, &paths, profile.as_deref(), passphrase.as_str())
                .is_err()
            {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Drops the passphrase request for a backup made under another passphrase.
pub fn cancel_restore_passphrase(main: &MainWindow, state: &Arc<ShellState>) {
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.cancel_restore_passphrase();
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());
}

/// Cancels a staged restore that has not been applied yet.
pub fn cancel_pending_restore(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(pending) = pending_state(state) else {
        return;
    };
    let paths = paths.clone();
    let profile = state.slug();
    let pending = begin_busy(main, state, pending);

    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.cancel_pending_restore(db, &paths, profile.as_deref()).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Marks the Settings page busy before a background action starts.
fn begin_busy(main: &MainWindow, state: &Arc<ShellState>, mut pending: SettingsState) -> SettingsState {
    pending.is_busy = true;
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());
    pending
}

/// Asks for a file and exports the rules into it.
pub fn export_rules(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let today = OffsetDateTime::now_utc().date();
        let file_name = format!(
            "finglass-rules-{:04}-{:02}-{:02}.tsv",
            today.year(),
            u8::from(today.month()),
            today.day()
        );
        let filter = i18n::text(pending.language, Key::ExportRules).to_owned();
        let Some(path) = rfd::FileDialog::new()
            .add_filter(&filter, &["tsv"])
            .set_file_name(&file_name)
            .save_file()
        else {
            state.jobs.end();
            return;
        };
        if pending.export_rules(&db, &path).is_err() {
            pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
        }
        publish(main_weak, state, pending, generation, true);
    });
}

/// Asks for a rule file and prepares its preview.
pub fn import_rules(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        let filter = i18n::text(pending.language, Key::ImportRules).to_owned();
        let Some(path) = rfd::FileDialog::new().add_filter(&filter, &["tsv", "txt"]).pick_file() else {
            state.jobs.end();
            return;
        };
        pending.clear_messages();
        if pending.choose_rule_import(&db, &path).is_err() {
            pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
        }
        publish(main_weak, state, pending, generation, true);
    });
}

/// Applies the confirmed rule-import preview.
pub fn confirm_rule_import(main: &MainWindow, state: &Arc<ShellState>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    let categorize = main.get_rule_import_categorize();
    pending.is_importing_rules = true;
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());

    spawn_pending(
        main,
        state,
        pending,
        move |pending, db| {
            if pending.confirm_rule_import(db, categorize).is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

/// Drops the rule-import preview.
pub fn cancel_rule_import(main: &MainWindow, state: &Arc<ShellState>) {
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.cancel_rule_import();
    *state.settings.lock().expect("settings state poisoned") = pending.clone();
    apply(main, &pending, state.restored_from().as_deref());
}

fn pending_state(state: &Arc<ShellState>) -> Option<SettingsState> {
    let db_present = state.db.lock().expect("database state poisoned").is_some();
    if !db_present {
        return None;
    }
    let language = *state.language.lock().expect("language state poisoned");
    let mut pending = state.settings.lock().expect("settings state poisoned").clone();
    pending.language = language;
    Some(pending)
}

/// Runs a Settings action on a background thread and publishes the changed state on the event loop.
fn spawn_pending<F, After>(
    main: &MainWindow,
    state: &Arc<ShellState>,
    mut pending: SettingsState,
    work: F,
    after: After,
) where
    F: FnOnce(&mut SettingsState, &Database) + Send + 'static,
    After: FnOnce(&MainWindow, &SettingsState) + Send + 'static,
{
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        work(&mut pending, &db);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                *state.settings.lock().expect("settings state poisoned") = pending.clone();
                apply(&ui, &pending, state.restored_from().as_deref());
                after(&ui, &pending);
            }
        });
    });
}

/// Runs a profile action that needs the data root and the database, off the event loop.
fn spawn_profile_action<F, After>(
    main: &MainWindow,
    state: &Arc<ShellState>,
    mut pending: SettingsState,
    paths: Arc<AppPaths>,
    work: F,
    after: After,
) where
    F: FnOnce(&mut SettingsState, &Database, &AppPaths) + Send + 'static,
    After: FnOnce(&MainWindow, &SettingsState) + Send + 'static,
{
    let Some(db) = state.db.lock().expect("database state poisoned").clone() else {
        return;
    };
    let main_weak = main.as_weak();
    let state = state.clone();
    let generation = state.jobs.begin();
    thread::spawn(move || {
        work(&mut pending, &db, &paths);
        let _ = slint::invoke_from_event_loop(move || {
            state.jobs.end();
            if !state.jobs.is_current(generation) {
                return;
            }
            if let Some(ui) = main_weak.upgrade() {
                *state.settings.lock().expect("settings state poisoned") = pending.clone();
                apply(&ui, &pending, state.restored_from().as_deref());
                after(&ui, &pending);
            }
        });
    });
}

/// Opens the name dialog for a new profile.
pub fn open_new_profile(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>) {
    let language = *state.language.lock().expect("language state poisoned");
    main.set_profile_dialog_title(i18n::text(language, Key::NewProfile).into());
    main.set_profile_dialog_name("".into());
    main.set_profile_dialog_index(-1);
    main.set_profile_dialog_error("".into());
    main.set_profile_dialog_open(true);
}

/// Opens the name dialog to rename the profile at `index`.
pub fn open_rename_profile(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, index: i32) {
    let language = *state.language.lock().expect("language state poisoned");
    let profile = state
        .settings
        .lock()
        .expect("settings state poisoned")
        .profiles
        .get(usize::try_from(index).unwrap_or(usize::MAX))
        .cloned();
    let Some(profile) = profile else {
        return;
    };
    main.set_profile_dialog_title(
        i18n::format(language, Key::RenameProfile, &[Arg::from(profile.name.clone())]).into(),
    );
    main.set_profile_dialog_name(profile.name.into());
    main.set_profile_dialog_index(index);
    main.set_profile_dialog_error("".into());
    main.set_profile_dialog_open(true);
}

/// Closes the name dialog without changing anything.
pub fn close_profile_dialog(main: &MainWindow, _state: &Arc<ShellState>) {
    main.set_profile_dialog_open(false);
    main.set_profile_dialog_error("".into());
}

/// Creates or renames the profile the name dialog shows.
pub fn save_profile(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    pending.clear_messages();
    let index = main.get_profile_dialog_index();
    let name = main.get_profile_dialog_name().to_string();
    let paths = paths.clone();

    spawn_profile_action(
        main,
        state,
        pending,
        paths,
        move |pending, db, paths| {
            let result = if index < 0 {
                pending.create_profile(db, paths, &name)
            } else {
                let profile = pending
                    .profiles
                    .get(usize::try_from(index).unwrap_or(usize::MAX))
                    .cloned();
                match profile {
                    Some(profile) => pending.rename_profile(db, paths, profile.slug.as_deref(), &name),
                    None => Ok(()),
                }
            };
            if result.is_err() {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |ui, pending| {
            if pending.error.is_none() {
                ui.set_profile_dialog_open(false);
            } else if let Some(error) = &pending.error {
                ui.set_profile_dialog_error(error.clone().into());
            }
        },
    );
}

/// Opens the confirmation for deleting the profile at `index`.
pub fn request_delete_profile(main: &MainWindow, state: &Arc<ShellState>, _paths: &Arc<AppPaths>, index: i32) {
    let language = *state.language.lock().expect("language state poisoned");
    let profile = state
        .settings
        .lock()
        .expect("settings state poisoned")
        .profiles
        .get(usize::try_from(index).unwrap_or(usize::MAX))
        .cloned();
    let Some(profile) = profile else {
        return;
    };
    main.set_profile_delete_index(index);
    main.set_profile_delete_title(
        i18n::format(language, Key::DeleteProfile, &[Arg::from(profile.name.clone())]).into(),
    );
    main.set_profile_delete_text(i18n::format(language, Key::DeleteProfileText, &[Arg::from(profile.name)]).into());
    main.set_profile_delete_open(true);
}

/// Closes the delete confirmation without moving anything.
pub fn cancel_delete_profile(main: &MainWindow, _state: &Arc<ShellState>) {
    main.set_profile_delete_open(false);
}

/// Moves the confirmed profile's folder aside after forgetting its remembered PINs.
pub fn confirm_delete_profile(main: &MainWindow, state: &Arc<ShellState>, paths: &Arc<AppPaths>) {
    let Some(mut pending) = pending_state(state) else {
        return;
    };
    pending.clear_messages();
    let index = main.get_profile_delete_index();
    main.set_profile_delete_open(false);
    let profile = pending
        .profiles
        .get(usize::try_from(index).unwrap_or(usize::MAX))
        .cloned();
    let Some(profile) = profile else {
        return;
    };
    let Some(slug) = profile.slug.clone() else {
        return;
    };
    let open_profile = pending.open_profile.clone();
    let paths = paths.clone();
    let credentials = state.credentials.clone();

    spawn_profile_action(
        main,
        state,
        pending,
        paths,
        move |pending, db, paths| {
            let forget = |slug: &str| {
                if let Ok(dir) = paths.profile_dir(slug) {
                    finglass_core::sync::credentials::forget_pins_for_profile(credentials.as_ref(), &dir);
                }
            };
            if pending
                .delete_profile(db, paths, open_profile.as_deref(), &slug, &forget)
                .is_err()
            {
                pending.error = Some(i18n::text(pending.language, Key::ErrorUnexpected).to_owned());
            }
        },
        |_, _| {},
    );
}

fn publish(
    main: slint::Weak<MainWindow>,
    state: Arc<ShellState>,
    pending: SettingsState,
    generation: u64,
    finish: bool,
) {
    let _ = slint::invoke_from_event_loop(move || {
        if finish {
            state.jobs.end();
        }
        if !state.jobs.is_current(generation) {
            return;
        }
        if let Some(ui) = main.upgrade() {
            *state.settings.lock().expect("settings state poisoned") = pending.clone();
            apply(&ui, &pending, state.restored_from().as_deref());
        }
    });
}

fn apply_rule_import(main: &MainWindow, state: &SettingsState) {
    let Some(preview) = &state.rule_import else {
        main.set_rule_import_open(false);
        return;
    };

    let language = state.language;
    if !main.get_rule_import_open() {
        main.set_rule_import_categorize(true);
    }
    main.set_rule_import_open(true);
    main.set_rule_import_busy(state.is_importing_rules);
    main.set_rule_import_new_rules_text(
        i18n::format(language, Key::RuleImportNewRules, &[Arg::from(preview.new_rules.len())]).into(),
    );

    main.set_rule_import_categories_text(
        if preview.categories_to_create.is_empty() {
            String::new()
        } else {
            i18n::format(
                language,
                Key::RuleImportCategories,
                &[Arg::from(preview.categories_to_create.len())],
            )
        }
        .into(),
    );
    main.set_rule_import_categories_list(
        listed(language, preview.categories_to_create.iter().map(String::as_str)).into(),
    );

    main.set_rule_import_duplicates_text(
        if preview.duplicates == 0 {
            String::new()
        } else {
            i18n::format(language, Key::RuleImportDuplicates, &[Arg::from(preview.duplicates)])
        }
        .into(),
    );

    main.set_rule_import_conflicts_text(
        if preview.conflicts.is_empty() {
            String::new()
        } else {
            i18n::format(
                language,
                Key::RuleImportConflicts,
                &[Arg::from(preview.conflicts.len())],
            )
        }
        .into(),
    );
    let mut conflicts: Vec<String> = preview
        .conflicts
        .iter()
        .map(|conflict| {
            i18n::format(
                language,
                Key::RuleImportConflictLine,
                &[
                    Arg::from(conflict.line),
                    Arg::from(conflict.pattern.clone()),
                    Arg::from(conflict.file_category.clone()),
                    Arg::from(conflict.existing_category.clone()),
                ],
            )
        })
        .collect();
    conflicts.truncate(20);
    let mut conflicts_text = conflicts.join("\n");
    let more = preview.conflicts.len().saturating_sub(20);
    if more > 0 {
        if !conflicts_text.is_empty() {
            conflicts_text.push('\n');
        }
        conflicts_text.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    main.set_rule_import_conflicts_list(conflicts_text.into());

    main.set_rule_import_errors_text(
        if preview.errors.is_empty() {
            String::new()
        } else {
            i18n::format(language, Key::RuleImportErrors, &[Arg::from(preview.errors.len())])
        }
        .into(),
    );
    let mut errors: Vec<String> = preview
        .errors
        .iter()
        .map(|error| {
            i18n::format(
                language,
                Key::RuleImportErrorLine,
                &[Arg::from(error.line), Arg::from(error.reason.clone())],
            )
        })
        .collect();
    errors.truncate(20);
    let mut errors_text = errors.join("\n");
    let more = preview.errors.len().saturating_sub(20);
    if more > 0 {
        if !errors_text.is_empty() {
            errors_text.push('\n');
        }
        errors_text.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    main.set_rule_import_errors_list(errors_text.into());

    main.set_rule_import_show_categorize(preview.match_count > 0);
    main.set_rule_import_categorize_text(
        i18n::format(language, Key::RuleImportCategorize, &[Arg::from(preview.match_count)]).into(),
    );
    main.set_rule_import_confirm_enabled(
        !state.is_importing_rules && (!preview.new_rules.is_empty() || !preview.categories_to_create.is_empty()),
    );
}

fn listed<'a>(language: Language, names: impl Iterator<Item = &'a str>) -> String {
    let names: Vec<&str> = names.collect();
    let mut text = names.iter().take(20).copied().collect::<Vec<_>>().join(", ");
    let more = names.len().saturating_sub(20);
    if more > 0 {
        if !text.is_empty() {
            text.push_str(", ");
        }
        text.push_str(&i18n::format(language, Key::AndMore, &[Arg::from(more)]));
    }
    text
}

fn category_model(state: &SettingsState) -> ModelRc<CategoryRow> {
    ModelRc::new(VecModel::from(category_rows(state)))
}

fn category_rows(state: &SettingsState) -> Vec<CategoryRow> {
    state
        .categories
        .iter()
        .map(|category| {
            let usage = state.category_usage.get(&category.id).copied().unwrap_or(0);
            CategoryRow {
                id: id32(category.id.0),
                name: category.name.clone().into(),
                colour: color(&category.color),
                budget_text: category
                    .budget_limit
                    .map(|budget| {
                        i18n::format(
                            state.language,
                            Key::BudgetAmount,
                            &[Arg::from(format::money(budget, state.language, false))],
                        )
                    })
                    .unwrap_or_default()
                    .into(),
                usage_text: i18n::format(state.language, Key::CategoryUsage, &[Arg::from(usage)]).into(),
                tax_relevant: category.is_tax_relevant,
                transfer: category.is_transfer,
            }
        })
        .collect()
}

fn rule_model(state: &SettingsState) -> ModelRc<RuleRow> {
    ModelRc::new(VecModel::from(rule_rows(state)))
}

fn rule_rows(state: &SettingsState) -> Vec<RuleRow> {
    let names: BTreeMap<CategoryId, &str> = state
        .categories
        .iter()
        .map(|category| (category.id, category.name.as_str()))
        .collect();

    state
        .rules
        .iter()
        .map(|rule| {
            let mut category_text = names.get(&rule.category_id).copied().unwrap_or_default().to_owned();
            match rule.tax_relevant {
                Some(true) => {
                    category_text.push_str(" · ");
                    category_text.push_str(i18n::text(state.language, Key::RuleTaxMarkerYes));
                }
                Some(false) => {
                    category_text.push_str(" · ");
                    category_text.push_str(i18n::text(state.language, Key::RuleTaxMarkerNo));
                }
                None => {}
            }

            let mut restrictions = Vec::new();
            match rule.match_field {
                RuleField::Any => {}
                RuleField::Partner => restrictions.push(i18n::text(state.language, Key::RuleFieldPartner)),
                RuleField::Purpose => restrictions.push(i18n::text(state.language, Key::RuleFieldPurpose)),
            }
            match rule.direction {
                RuleDirection::Any => {}
                RuleDirection::Expense => restrictions.push(i18n::text(state.language, Key::RuleDirectionExpense)),
                RuleDirection::Income => restrictions.push(i18n::text(state.language, Key::RuleDirectionIncome)),
            }

            let mut prio_text = i18n::format(state.language, Key::Prio, &[Arg::from(rule.priority)]);
            if !rule.auto_apply {
                prio_text.push_str(" · ");
                prio_text.push_str(i18n::text(state.language, Key::Manual));
            }

            RuleRow {
                id: id32(rule.id.0),
                pattern: rule.pattern.clone().into(),
                category_text: category_text.into(),
                detail: restrictions.join(" · ").into(),
                prio_text: prio_text.into(),
            }
        })
        .collect()
}

fn primary_currency_index(state: &SettingsState) -> i32 {
    state
        .currency_options
        .iter()
        .position(|currency| *currency == state.primary_currency)
        .map_or(0, |index| i32::try_from(index).unwrap_or(0))
}

fn has_duplicate_colours(categories: &[Category]) -> bool {
    let mut seen = BTreeMap::<String, usize>::new();
    for category in categories {
        *seen.entry(category.color.to_uppercase()).or_default() += 1;
    }
    seen.values().any(|count| *count > 1)
}

fn tax_keywords_count(state: &SettingsState) -> String {
    let count = state
        .tax_keywords
        .lines()
        .filter(|line| !line.trim().is_empty())
        .count();
    i18n::format(state.language, Key::TaxKeywordsCount, &[Arg::from(count)])
}

fn budget_input(budget: Option<Money>) -> String {
    let Some(budget) = budget else {
        return String::new();
    };
    let exponent = budget.currency.minor_exponent();
    if exponent == 0 {
        return budget.minor.to_string();
    }
    let scale = 10_i64.pow(exponent);
    let whole = budget.minor / scale;
    let fraction = (budget.minor % scale).abs();
    format!("{whole}.{fraction:0width$}", width = exponent as usize)
}

fn parse_budget(text: &str) -> Result<Option<Money>, Key> {
    if text.trim().is_empty() {
        return Ok(None);
    }
    // Budgets are stored in EUR (`repo::categories`); the core parser applies the currency's exponent, so "250" is
    // 250.00, not 2.50.
    let text = text.trim().trim_start_matches('€').trim_end_matches('€').trim();
    let money = finglass_core::money::parse_decimal(text, Currency::EUR).map_err(|_| Key::BudgetNotANumber)?;
    Ok(Some(money))
}

fn rule_error(state: &SettingsState, pattern: &str, priority: &str) -> String {
    if let Some(error) = state.validate_rule(pattern) {
        return error;
    }
    if priority.trim().parse::<i64>().is_err() {
        return i18n::text(state.language, Key::PriorityMustBeNumber).to_owned();
    }
    String::new()
}

fn field_options(language: Language) -> Vec<&'static str> {
    vec![
        i18n::text(language, Key::RuleFieldAny),
        i18n::text(language, Key::RuleFieldPartner),
        i18n::text(language, Key::RuleFieldPurpose),
    ]
}

fn direction_options(language: Language) -> Vec<&'static str> {
    vec![
        i18n::text(language, Key::RuleDirectionAny),
        i18n::text(language, Key::RuleDirectionExpense),
        i18n::text(language, Key::RuleDirectionIncome),
    ]
}

fn tax_options(language: Language) -> Vec<&'static str> {
    vec![
        i18n::text(language, Key::RuleTaxAsCategory),
        i18n::text(language, Key::TaxRelevantChoice),
        i18n::text(language, Key::TaxNotRelevantChoice),
    ]
}

fn field_index(field: RuleField) -> i32 {
    match field {
        RuleField::Any => 0,
        RuleField::Partner => 1,
        RuleField::Purpose => 2,
    }
}

fn direction_index(direction: RuleDirection) -> i32 {
    match direction {
        RuleDirection::Any => 0,
        RuleDirection::Expense => 1,
        RuleDirection::Income => 2,
    }
}

fn tax_index(tax: Option<bool>) -> i32 {
    match tax {
        None => 0,
        Some(true) => 1,
        Some(false) => 2,
    }
}

fn field_at(index: i32) -> RuleField {
    match index {
        1 => RuleField::Partner,
        2 => RuleField::Purpose,
        _ => RuleField::Any,
    }
}

fn direction_at(index: i32) -> RuleDirection {
    match index {
        1 => RuleDirection::Expense,
        2 => RuleDirection::Income,
        _ => RuleDirection::Any,
    }
}

fn tax_at(index: i32) -> Option<bool> {
    match index {
        1 => Some(true),
        2 => Some(false),
        _ => None,
    }
}

fn string_model<'a>(values: impl Iterator<Item = &'a str>) -> ModelRc<SharedString> {
    ModelRc::new(VecModel::from(values.map(SharedString::from).collect::<Vec<_>>()))
}

fn colour_model() -> ModelRc<ColourSwatch> {
    ModelRc::new(VecModel::from(
        repo::categories::COLORS
            .iter()
            .map(|hex| ColourSwatch {
                hex: (*hex).into(),
                colour: color(hex),
            })
            .collect::<Vec<_>>(),
    ))
}

fn id32(id: i64) -> i32 {
    i32::try_from(id).unwrap_or(-1)
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_else(|| path.display().to_string())
}

fn color(hex: &str) -> slint::Color {
    let text = hex.trim().trim_start_matches('#');
    if text.len() == 6
        && let Ok(value) = u32::from_str_radix(text, 16)
    {
        return slint::Color::from_rgb_u8(
            ((value >> 16) & 0xFF) as u8,
            ((value >> 8) & 0xFF) as u8,
            (value & 0xFF) as u8,
        );
    }

    slint::Color::from_rgb_u8(117, 117, 117)
}

#[cfg(test)]
mod tests {
    use std::fs;

    use super::*;
    use finglass_core::app::settings::SettingsState;
    use finglass_core::model::{Category, Rule};

    fn category(id: i64, name: &str, colour: &str, budget: Option<i64>, tax: bool) -> Category {
        Category {
            id: CategoryId(id),
            name: name.to_owned(),
            color: colour.to_owned(),
            budget_limit: budget.map(|minor| Money {
                minor,
                currency: Currency::EUR,
            }),
            is_tax_relevant: tax,
            is_transfer: false,
        }
    }

    fn rule(id: i64, pattern: &str, category: i64, priority: i64, auto: bool) -> Rule {
        Rule {
            id: RuleId(id),
            pattern: pattern.to_owned(),
            category_id: CategoryId(category),
            priority,
            auto_apply: auto,
            match_field: RuleField::Partner,
            direction: RuleDirection::Expense,
            tax_relevant: None,
        }
    }

    #[test]
    fn category_rows_carry_budget_usage_and_flags() {
        let mut state = SettingsState::new(Language::En);
        state.categories = vec![
            category(1, "Food", "#FF5722", Some(30_000), false),
            category(2, "Donations", "#3BC7B0", None, true),
        ];
        state.category_usage.insert(CategoryId(1), 5);

        let rows = category_rows(&state);
        assert_eq!(rows.len(), 2);
        assert_eq!(rows[0].id, 1);
        assert_eq!(rows[0].name, SharedString::from("Food"));
        assert_eq!(rows[0].budget_text, SharedString::from("Budget €300.00"));
        assert_eq!(rows[0].usage_text, SharedString::from("5 bookings"));
        assert_eq!(rows[1].budget_text, SharedString::from(""));
        assert_eq!(rows[1].usage_text, SharedString::from("0 bookings"));
        assert!(rows[1].tax_relevant);
    }

    #[test]
    fn rule_rows_carry_category_tax_marker_restrictions_and_manual_marker() {
        let mut state = SettingsState::new(Language::En);
        state.categories = vec![category(1, "Food", "#FF5722", None, false)];
        let mut auto = rule(1, "rewe", 1, 50, true);
        auto.tax_relevant = Some(true);
        let mut manual = rule(2, "aldi", 1, 10, false);
        manual.tax_relevant = Some(false);
        manual.match_field = RuleField::Purpose;
        manual.direction = RuleDirection::Income;
        state.rules = vec![auto, manual];

        let rows = rule_rows(&state);
        assert_eq!(rows[0].category_text, SharedString::from("Food · Tax"));
        assert_eq!(rows[0].detail, SharedString::from("Partner name only · Expenses only"));
        assert_eq!(rows[0].prio_text, SharedString::from("prio 50"));
        assert_eq!(rows[1].category_text, SharedString::from("Food · No tax"));
        assert_eq!(rows[1].detail, SharedString::from("Purpose only · Income only"));
        assert_eq!(rows[1].prio_text, SharedString::from("prio 10 · manual"));
    }

    #[test]
    fn duplicate_colours_are_detected_case_insensitively() {
        let categories = vec![
            category(1, "A", "#aabbcc", None, false),
            category(2, "B", "#AABBCC", None, false),
            category(3, "C", "#112233", None, false),
        ];
        assert!(has_duplicate_colours(&categories));
        assert!(!has_duplicate_colours(&categories[..1]));
    }

    #[test]
    fn primary_currency_index_matches_the_option_list() {
        let mut state = SettingsState::new(Language::En);
        state.primary_currency = Currency::new("USD").unwrap();
        assert_eq!(primary_currency_index(&state), 1);
        state.primary_currency = Currency::new("XXX").unwrap();
        assert_eq!(primary_currency_index(&state), 0);
    }

    #[test]
    fn budget_input_round_trips_through_the_core_parser() {
        for minor in [None, Some(0), Some(5), Some(30_000), Some(-1_250)] {
            let money = minor.map(|minor| Money {
                minor,
                currency: Currency::EUR,
            });
            let text = budget_input(money);
            let parsed = parse_budget(&text).expect("a formatted budget is parseable");
            assert_eq!(parsed, money, "{text:?}");
        }
        assert_eq!(parse_budget("  "), Ok(None));
        let eur = |minor| {
            Ok(Some(Money {
                minor,
                currency: Currency::EUR,
            }))
        };
        assert_eq!(parse_budget("250"), eur(25_000), "a whole number is whole euros");
        assert_eq!(parse_budget("250,50"), eur(25_050));
        assert_eq!(parse_budget("1.234,56"), eur(123_456));
        assert_eq!(parse_budget("€ 99.9"), eur(9_990));
        assert_eq!(parse_budget("not a number"), Err(Key::BudgetNotANumber));
    }

    #[test]
    fn tax_keyword_count_ignores_blank_lines() {
        let mut state = SettingsState::new(Language::En);
        state.tax_keywords = "arzt\n\n  apotheke  \nhandwerker\n".to_owned();
        assert_eq!(tax_keywords_count(&state), "3 keywords");
    }

    #[test]
    fn dropdown_indices_map_to_the_rule_enums() {
        assert_eq!(field_at(0), RuleField::Any);
        assert_eq!(field_at(1), RuleField::Partner);
        assert_eq!(field_at(2), RuleField::Purpose);
        assert_eq!(direction_at(1), RuleDirection::Expense);
        assert_eq!(direction_at(2), RuleDirection::Income);
        assert_eq!(tax_at(0), None);
        assert_eq!(tax_at(1), Some(true));
        assert_eq!(tax_at(2), Some(false));
        assert_eq!(field_index(RuleField::Purpose), 2);
        assert_eq!(direction_index(RuleDirection::Income), 2);
        assert_eq!(tax_index(Some(false)), 2);
    }

    fn fixture_copy() -> (tempfile::TempDir, std::path::PathBuf) {
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

        let fixture = Path::new(env!("CARGO_MANIFEST_DIR")).join("../finglass-core/tests/fixtures/findesk");
        let temp = tempfile::tempdir().unwrap();
        let destination = temp.path().join("findesk");
        copy_tree(&fixture, &destination);
        (temp, destination)
    }

    fn findesk_target() -> (tempfile::TempDir, AppPaths, Database) {
        let temp = tempfile::tempdir().unwrap();
        let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
        let passphrase = Passphrase::new("target passphrase 1", PassphrasePurpose::New).unwrap();
        let db = Database::create(&temp.path().join("finglass.db"), &passphrase).unwrap();
        (temp, paths, db)
    }

    #[test]
    fn the_profile_busy_section_is_active_during_work_and_released_afterwards() {
        let busy = Busy::default();

        let active_during = run_while_profile_busy(&busy, || busy.active());

        assert!(active_during, "the work runs inside the busy section");
        assert!(!busy.active(), "the section ends after the work");
    }

    #[test]
    fn the_findesk_import_releases_the_profile_busy_after_success() {
        let (_fixture_temp, source_dir) = fixture_copy();
        let (_target_temp, paths, db) = findesk_target();
        let busy = Busy::default();
        let mut pending = FindeskImportState::new(Language::En);
        pending.source_dir = Some(source_dir);

        let result = import_with_profile_busy(&mut pending, &db, &paths, None, "fixture passphrase 1", &busy);

        assert!(result.is_ok());
        assert!(pending.report.is_some());
        assert!(!busy.active());
    }

    #[test]
    fn the_findesk_import_releases_the_profile_busy_after_an_error() {
        let (_fixture_temp, source_dir) = fixture_copy();
        let (_target_temp, paths, db) = findesk_target();
        let busy = Busy::default();
        let mut pending = FindeskImportState::new(Language::En);
        pending.source_dir = Some(source_dir);

        let result = import_with_profile_busy(&mut pending, &db, &paths, None, "wrong passphrase", &busy);

        assert!(matches!(result, Err(FinDeskImportError::WrongPassphrase)));
        assert!(pending.passphrase_wrong);
        assert!(!busy.active());
    }
}
