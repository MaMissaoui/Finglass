//! Wiring for the first-run FinDesk migration in the unlock window.
//!
//! Every verification and migration runs on a worker thread. The callbacks only read the Slint fields, start the work
//! and update the window from `slint::invoke_from_event_loop`. Passphrase text is kept in `Zeroizing` strings and then
//! in [`Passphrase`] values; nothing here stores a passphrase after the step that used it.

use std::sync::{Arc, Mutex};
use std::thread;

use finglass_core::app;
use finglass_core::app::migration::{
    self as core_migration, FinDeskInstall, FinDeskProfile, MigratedProfile, MigrationError,
};
use finglass_core::app::startup::Startup;
use finglass_core::app::unlock;
use finglass_core::findesk_import::{FinDeskImportError, FinDeskImportReport};
use finglass_core::i18n::{self, Arg, Key, Language};
use finglass_core::paths::AppPaths;
use finglass_core::profiles;
use finglass_core::secret::{Passphrase, PassphrasePurpose};
use slint::{ComponentHandle, ModelRc, SharedString, VecModel};
use time::{OffsetDateTime, UtcOffset};
use zeroize::Zeroizing;

use crate::shell::{self, ShellState};
use crate::{MainWindow, UnlockWindow};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Step {
    Welcome,
    Profile,
    Summary,
}

#[derive(Debug, Clone)]
enum Outcome {
    Migrated(Box<FinDeskImportReport>),
    Skipped,
    Failed(String),
}

#[derive(Debug, Clone)]
struct ProfileResult {
    findesk_slug: Option<String>,
    finglass_slug: Option<String>,
    name: String,
    outcome: Outcome,
}

struct Session {
    install: FinDeskInstall,
    startup: Startup,
    step: Step,
    index: usize,
    same_passphrase: bool,
    needs_new_passphrase: bool,
    busy: bool,
    error: Option<String>,
    results: Vec<ProfileResult>,
}

/// Writes the marker for a welcome action. The two welcome buttons are the only callers.
pub(crate) fn welcome_action(paths: &AppPaths, migrate: bool, now: OffsetDateTime) -> std::io::Result<()> {
    core_migration::write_marker(paths, migrate, now)
}

/// Wires the migration callbacks and puts the window into the welcome step.
pub(crate) fn wire(
    unlock: &UnlockWindow,
    main: &MainWindow,
    paths: &Arc<AppPaths>,
    state: &Arc<ShellState>,
    startup: Startup,
    install: FinDeskInstall,
) {
    let session = Arc::new(Mutex::new(Session {
        install,
        startup,
        step: Step::Welcome,
        index: 0,
        same_passphrase: true,
        needs_new_passphrase: false,
        busy: false,
        error: None,
        results: Vec::new(),
    }));

    {
        let session = session.lock().expect("migration state poisoned");
        apply(unlock, paths, state, &session);
    }

    {
        let unlock_weak = unlock.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_migrate(move || {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy || !ui.get_acknowledged() {
                return;
            }

            let language = current_language(&state);
            match welcome_action(&paths, true, OffsetDateTime::now_utc()) {
                Ok(()) => {
                    session.step = Step::Profile;
                    session.index = 0;
                    session.same_passphrase = true;
                    session.needs_new_passphrase = false;
                    session.error = None;
                    clear_fields(&ui);
                    apply(&ui, &paths, &state, &session);
                }
                Err(_) => {
                    session.error = Some(i18n::text(language, Key::ErrorUnexpected).to_owned());
                    apply(&ui, &paths, &state, &session);
                }
            }
        });
    }

    {
        let unlock_weak = unlock.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_start_empty(move || {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy {
                return;
            }

            let language = current_language(&state);
            match welcome_action(&paths, false, OffsetDateTime::now_utc()) {
                Ok(()) => {
                    clear_fields(&ui);
                    let startup = session.startup.clone();
                    drop(session);
                    shell::configure_unlock_window(&ui, &paths, language, &startup);
                    ui.set_passphrase("".into());
                    ui.set_repeat("".into());
                }
                Err(_) => {
                    session.error = Some(i18n::text(language, Key::ErrorUnexpected).to_owned());
                    apply(&ui, &paths, &state, &session);
                }
            }
        });
    }

    {
        let unlock_weak = unlock.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_submit(move |findesk_input, new_input, repeat_input| {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let session_handle = session.clone();
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy {
                return;
            }

            let findesk_text = Zeroizing::new(findesk_input.to_string());
            let new_text = Zeroizing::new(new_input.to_string());
            let repeat_text = Zeroizing::new(repeat_input.to_string());
            session.same_passphrase = ui.get_migration_same_passphrase();
            clear_fields(&ui);

            let language = current_language(&state);
            let Some(profile) = session.install.profiles.get(session.index).cloned() else {
                return;
            };

            let findesk_passphrase = match Passphrase::new(findesk_text.as_str(), PassphrasePurpose::Unlock) {
                Ok(passphrase) => passphrase,
                Err(_) => {
                    session.error = Some(i18n::text(language, Key::WrongPassphrase).to_owned());
                    apply(&ui, &paths, &state, &session);
                    return;
                }
            };

            let use_same = session.same_passphrase && !session.needs_new_passphrase;
            let finglass_passphrase = if use_same {
                match Passphrase::new(findesk_text.as_str(), PassphrasePurpose::New) {
                    Ok(passphrase) => passphrase,
                    Err(_) => {
                        session.needs_new_passphrase = true;
                        session.error = Some(i18n::text(language, Key::MigrationNewPassphraseNeeded).to_owned());
                        apply(&ui, &paths, &state, &session);
                        return;
                    }
                }
            } else {
                match unlock::validate_new(new_text.as_str(), repeat_text.as_str(), true) {
                    Ok(passphrase) => passphrase,
                    Err(error) => {
                        session.error = Some(shell::unlock_error_text(language, &error));
                        apply(&ui, &paths, &state, &session);
                        return;
                    }
                }
            };

            session.busy = true;
            session.error = None;
            apply(&ui, &paths, &state, &session);

            let paths = paths.clone();
            let state = state.clone();
            let unlock_weak = unlock_weak.clone();
            let generation = state.jobs.begin();
            let offset = UtcOffset::current_local_offset().unwrap_or(UtcOffset::UTC);
            drop(session);

            thread::spawn(move || {
                let result = core_migration::migrate_profile(
                    &paths,
                    &profile,
                    &findesk_passphrase,
                    &finglass_passphrase,
                    offset,
                );
                drop(findesk_passphrase);
                drop(finglass_passphrase);

                let _ = slint::invoke_from_event_loop(move || {
                    state.jobs.end();
                    if !state.jobs.is_current(generation) {
                        return;
                    }
                    let Some(ui) = unlock_weak.upgrade() else {
                        return;
                    };
                    let mut session = session_handle.lock().expect("migration state poisoned");
                    session.busy = false;
                    let language = current_language(&state);

                    match result {
                        Ok(migrated) => {
                            session.results.push(migrated_result(&profile, migrated));
                            advance(&mut session);
                        }
                        Err(MigrationError::FinDesk(FinDeskImportError::WrongPassphrase)) => {
                            session.error = Some(i18n::text(language, Key::WrongPassphrase).to_owned());
                        }
                        // Nothing was created yet: FinDesk still writing, its files gone or unreadable for a moment.
                        // The user can quit FinDesk and retry this profile; the offer itself never comes back.
                        Err(
                            error @ MigrationError::FinDesk(
                                FinDeskImportError::SourceChanged
                                | FinDeskImportError::NotFound
                                | FinDeskImportError::Io(_),
                            ),
                        ) => {
                            session.error = Some(error.message(language));
                        }
                        Err(error) => {
                            session.results.push(ProfileResult {
                                findesk_slug: profile.slug.clone(),
                                finglass_slug: None,
                                name: profile.name.clone(),
                                outcome: Outcome::Failed(error.message(language)),
                            });
                            advance(&mut session);
                        }
                    }

                    clear_fields(&ui);
                    apply(&ui, &paths, &state, &session);
                });
            });
        });
    }

    {
        let unlock_weak = unlock.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_passphrase_changed(move |value| {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy || !session.same_passphrase {
                return;
            }

            let text = value.as_str();
            let needs_new = !text.is_empty() && Passphrase::new(text, PassphrasePurpose::New).is_err();
            if session.needs_new_passphrase != needs_new {
                session.needs_new_passphrase = needs_new;
                apply(&ui, &paths, &state, &session);
            }
        });
    }

    {
        let unlock_weak = unlock.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_skip(move || {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy {
                return;
            }
            let Some(profile) = session.install.profiles.get(session.index).cloned() else {
                return;
            };

            session.results.push(ProfileResult {
                findesk_slug: profile.slug,
                finglass_slug: None,
                name: profile.name,
                outcome: Outcome::Skipped,
            });
            advance(&mut session);
            clear_fields(&ui);
            apply(&ui, &paths, &state, &session);
        });
    }

    {
        let unlock_weak = unlock.as_weak();
        let main_weak = main.as_weak();
        let paths = paths.clone();
        let state = state.clone();
        let session = session.clone();
        unlock.on_migration_continue(move || {
            let Some(ui) = unlock_weak.upgrade() else {
                return;
            };
            let session_handle = session.clone();
            let mut session = session.lock().expect("migration state poisoned");
            if session.busy {
                return;
            }

            session.busy = true;
            session.error = None;
            apply(&ui, &paths, &state, &session);

            let pairs: Vec<(Option<String>, Option<String>)> = session
                .results
                .iter()
                .filter(|result| matches!(result.outcome, Outcome::Migrated(_)))
                .map(|result| (result.findesk_slug.clone(), result.finglass_slug.clone()))
                .collect();
            let active = core_migration::active_after(&session.install, &pairs);
            let paths = paths.clone();
            let state = state.clone();
            let main_weak = main_weak.clone();
            let unlock_weak = unlock_weak.clone();
            let generation = state.jobs.begin();
            drop(session);

            thread::spawn(move || {
                let result: Result<Startup, ()> = (|| {
                    profiles::set_active(&paths, active.as_deref()).map_err(|_| ())?;
                    app::startup::prepare_profile(&paths, active.as_deref(), OffsetDateTime::now_utc()).map_err(|_| ())
                })();

                let _ = slint::invoke_from_event_loop(move || {
                    state.jobs.end();
                    if !state.jobs.is_current(generation) {
                        return;
                    }
                    let Some(ui) = unlock_weak.upgrade() else {
                        return;
                    };
                    let mut session = session_handle.lock().expect("migration state poisoned");
                    session.busy = false;

                    match result {
                        Ok(startup) => {
                            let Some(main) = main_weak.upgrade() else {
                                return;
                            };
                            clear_fields(&ui);
                            shell::teardown_profile(&state, Some(&main), &startup);
                            shell::configure_unlock_window(&ui, &paths, current_language(&state), &startup);
                            let _ = ui.show();
                            let _ = main.hide();
                        }
                        Err(()) => {
                            let language = current_language(&state);
                            session.error = Some(i18n::text(language, Key::ErrorUnexpected).to_owned());
                            apply(&ui, &paths, &state, &session);
                        }
                    }
                });
            });
        });
    }
}

fn migrated_result(profile: &FinDeskProfile, migrated: MigratedProfile) -> ProfileResult {
    ProfileResult {
        findesk_slug: profile.slug.clone(),
        finglass_slug: migrated.finglass_slug,
        name: profile.name.clone(),
        outcome: Outcome::Migrated(Box::new(migrated.report)),
    }
}

fn advance(session: &mut Session) {
    session.index += 1;
    session.same_passphrase = true;
    session.needs_new_passphrase = false;
    session.error = None;
    session.step = if session.index >= session.install.profiles.len() {
        Step::Summary
    } else {
        Step::Profile
    };
}

fn apply(ui: &UnlockWindow, _paths: &AppPaths, state: &ShellState, session: &Session) {
    let language = current_language(state);
    ui.set_mode("migrate".into());
    ui.set_busy(session.busy);
    ui.set_error_text(session.error.clone().unwrap_or_default().into());
    ui.set_migration_step(step_name(session.step).into());

    let names: Vec<SharedString> = session
        .install
        .profiles
        .iter()
        .map(|profile| profile.name.clone().into())
        .collect();
    ui.set_migration_profile_names(ModelRc::new(VecModel::from(names)));
    ui.set_migration_profile_count(i32::try_from(session.install.profiles.len()).unwrap_or(i32::MAX));

    if let Some(profile) = session.install.profiles.get(session.index) {
        ui.set_migration_profile_name(profile.name.clone().into());
        ui.set_migration_profile_heading(
            i18n::format(
                language,
                Key::MigrationProfileHeading,
                &[
                    Arg::from(session.index + 1),
                    Arg::from(session.install.profiles.len()),
                    profile.name.clone().into(),
                ],
            )
            .into(),
        );
    } else {
        ui.set_migration_profile_name("".into());
        ui.set_migration_profile_heading("".into());
    }

    ui.set_migration_same_passphrase(session.same_passphrase);
    ui.set_migration_new_required(!session.same_passphrase || session.needs_new_passphrase);
    ui.set_passphrase_hint(i18n::format(language, Key::PassphraseHint, &[shell::MIN_PASSPHRASE_LENGTH.into()]).into());

    let lines: Vec<SharedString> = session
        .results
        .iter()
        .map(|result| summary_line(language, result).into())
        .collect();
    ui.set_migration_summary_lines(ModelRc::new(VecModel::from(lines)));
}

fn summary_line(language: Language, result: &ProfileResult) -> String {
    match &result.outcome {
        Outcome::Migrated(report) => i18n::format(
            language,
            Key::MigrationProfileMigrated,
            &[result.name.clone().into(), report.summary(language).into()],
        ),
        Outcome::Skipped => i18n::format(language, Key::MigrationProfileSkipped, &[result.name.clone().into()]),
        Outcome::Failed(error) => i18n::format(
            language,
            Key::MigrationProfileFailed,
            &[result.name.clone().into(), error.clone().into()],
        ),
    }
}

fn clear_fields(ui: &UnlockWindow) {
    ui.set_migration_passphrase("".into());
    ui.set_migration_new_passphrase("".into());
    ui.set_migration_repeat("".into());
    ui.set_migration_show_passphrase(false);
}

fn step_name(step: Step) -> &'static str {
    match step {
        Step::Welcome => "welcome",
        Step::Profile => "profile",
        Step::Summary => "summary",
    }
}

fn current_language(state: &ShellState) -> Language {
    *state.language.lock().expect("language state poisoned")
}

#[cfg(test)]
mod tests {
    use super::*;
    use finglass_core::app::unlock::UnlockError;
    use time::macros::datetime;

    fn paths(temp: &tempfile::TempDir) -> AppPaths {
        AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap()
    }

    #[test]
    fn both_welcome_actions_write_the_marker() {
        let temp = tempfile::tempdir().unwrap();
        let paths = paths(&temp);
        let now = datetime!(2026-10-15 12:34:56 UTC);

        welcome_action(&paths, true, now).unwrap();
        assert!(
            std::fs::read_to_string(paths.root().join(core_migration::MARKER_FILE))
                .unwrap()
                .starts_with("migrate ")
        );

        welcome_action(&paths, false, now).unwrap();
        assert!(
            std::fs::read_to_string(paths.root().join(core_migration::MARKER_FILE))
                .unwrap()
                .starts_with("skip ")
        );
    }

    #[test]
    fn every_migration_error_has_text() {
        for &language in Language::ALL {
            for error in [
                MigrationError::TargetNotEmpty,
                MigrationError::FinDesk(FinDeskImportError::SourceChanged),
                MigrationError::FinDesk(FinDeskImportError::WrongPassphrase),
            ] {
                assert!(!error.message(language).is_empty(), "{language:?} / {error:?}");
            }
        }
    }

    #[test]
    fn an_unlock_error_has_text_in_every_language() {
        for &language in Language::ALL {
            assert!(!shell::unlock_error_text(language, &UnlockError::Mismatch).is_empty());
        }
    }
}
