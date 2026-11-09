//! Ported from FinDesk's settings view-model tests.

use std::cell::{Cell, RefCell};
use std::path::{Path, PathBuf};

use time::macros::date;

use super::*;
use crate::model::{AccountId, AccountType, NewAccount, NewTransaction};

const PASS: &str = "correct horse battery staple";

fn fixture() -> (tempfile::TempDir, AppPaths, Database) {
    let temp = tempfile::tempdir().unwrap();
    let paths = AppPaths::resolve(Some(temp.path().as_os_str().to_owned()), None).unwrap();
    let passphrase = Passphrase::new(PASS, PassphrasePurpose::New).unwrap();
    let db = Database::create(&paths.database_file("default").unwrap(), &passphrase).unwrap();
    (temp, paths, db)
}

fn loaded(paths: &AppPaths, db: &Database) -> SettingsState {
    let mut state = SettingsState::new(Language::En);
    state.load(db, paths, None).unwrap();
    state
}

fn category(db: &Database, name: &str, color: &str) -> CategoryId {
    repo::categories::create(
        db,
        NewCategory {
            name: name.to_owned(),
            color: color.to_owned(),
            budget_limit: None,
            is_tax_relevant: false,
            is_transfer: false,
        },
    )
    .unwrap()
}

fn account(db: &Database) -> AccountId {
    repo::accounts::create(
        db,
        NewAccount {
            bank_connection_id: None,
            institution: None,
            name: Some("Account".to_owned()),
            iban: None,
            account_number: None,
            currency: Currency::EUR,
            account_type: AccountType::Checking,
            balance: Money {
                minor: 0,
                currency: Currency::EUR,
            },
            balance_updated_at: None,
            group_id: None,
        },
    )
    .unwrap()
}

fn transaction(db: &Database, account_id: AccountId, category_id: Option<CategoryId>) {
    repo::transactions::insert_imported(
        db,
        account_id,
        vec![NewTransaction {
            booking_date: date!(2026 - 03 - 02),
            amount: Money {
                minor: -3_000,
                currency: Currency::EUR,
            },
            balance_after: None,
            partner_name: Some("REWE".to_owned()),
            purpose: None,
            category_id,
            import_hash: "rewe".to_owned(),
            created_at: time::OffsetDateTime::now_utc(),
            is_new: false,
            tax_override: None,
        }],
    )
    .unwrap();
}

fn audit_details(db: &Database, action: &str) -> Vec<String> {
    crate::audit::recent(db, 1000)
        .unwrap()
        .into_iter()
        .filter(|entry| entry.action == action)
        .map(|entry| entry.details)
        .collect()
}

fn rule_file(dir: &Path, name: &str, rows: &[&str]) -> PathBuf {
    let mut text = String::from("# FinDesk rules v1\ncategory\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\n");
    for row in rows {
        text.push_str(row);
        text.push('\n');
    }
    let path = dir.join(name);
    std::fs::write(&path, text).unwrap();
    path
}

// AddDefaultCategoriesTest.kt

#[test]
fn on_an_empty_database_all_24_defaults_are_created() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);

    state.add_default_categories(&db).unwrap();

    let names: Vec<&str> = state.categories.iter().map(|category| category.name.as_str()).collect();
    assert_eq!(state.categories.len(), 24, "{names:?}");
    assert!(names.contains(&"Groceries"));
    assert!(names.contains(&"Drugstore & household"));
    assert!(names.contains(&"Other income"));
    assert!(
        state.categories.iter().all(|category| is_hex_color(&category.color)),
        "every default has a palette colour"
    );
    assert_eq!(state.message.as_deref(), Some("24 default categories added"));
    drop(temp);
}

#[test]
fn existing_categories_are_kept_and_matched_case_insensitively() {
    let (temp, paths, db) = fixture();
    let existing = category(&db, "groceries", "#123456");
    let mut state = loaded(&paths, &db);

    state.add_default_categories(&db).unwrap();

    assert_eq!(state.categories.len(), 24);
    let groceries = state
        .categories
        .iter()
        .find(|category| category.id == existing)
        .unwrap();
    assert_eq!(groceries.name, "groceries", "existing categories are never changed");
    assert_eq!(state.message.as_deref(), Some("23 default categories added"));

    state.add_default_categories(&db).unwrap();
    assert_eq!(state.categories.len(), 24);
    assert_eq!(state.message.as_deref(), Some("All default categories already exist"));
    drop(temp);
}

#[test]
fn adding_default_categories_is_audited() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);

    state.add_default_categories(&db).unwrap();

    assert_eq!(
        audit_details(&db, "ADD_DEFAULT_CATEGORIES"),
        vec!["Added 24 default categories".to_owned()]
    );
    drop(temp);
}

#[test]
fn two_add_default_calls_create_every_category_once() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);

    state.add_default_categories(&db).unwrap();
    state.add_default_categories(&db).unwrap();

    let names: Vec<&str> = state.categories.iter().map(|category| category.name.as_str()).collect();
    assert_eq!(names.len(), 24, "{names:?}");
    let unique: BTreeSet<&str> = names.iter().copied().collect();
    assert_eq!(unique.len(), names.len(), "no category is created twice");
    drop(temp);
}

#[test]
fn the_default_category_list_holds_24_entries_in_palette_order() {
    assert_eq!(repo::categories::ENTRIES.len(), 24);
    assert_eq!(repo::categories::ENTRIES.first().unwrap().1, "#E76530");
    assert_eq!(
        repo::categories::COLORS.len(),
        24,
        "24 categories, 24 distinct colours, no cycling"
    );
}

// DefaultCategoriesTransferFlagTest.kt

#[test]
fn only_own_transfers_is_a_transfer() {
    let transfers: Vec<_> = repo::categories::ENTRIES
        .iter()
        .filter(|(_, _, is_transfer)| *is_transfer)
        .collect();
    assert_eq!(transfers.len(), 1);
    assert_eq!(transfers[0].0, Key::DefaultCategoryOwnTransfers);
    assert!(
        !repo::categories::ENTRIES
            .iter()
            .any(|(key, _, is_transfer)| { *key == Key::DefaultCategoryCreditCard && *is_transfer })
    );
}

#[test]
fn add_default_categories_stores_the_transfer_flag() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);

    state.add_default_categories(&db).unwrap();

    let saved: BTreeMap<&str, bool> = state
        .categories
        .iter()
        .map(|category| (category.name.as_str(), category.is_transfer))
        .collect();
    assert!(!saved["Groceries"]);
    assert!(!saved["Credit card"]);
    assert!(saved["Own transfers"]);
    drop(temp);
}

// FixDuplicateColoursTest.kt

#[test]
fn the_lowest_id_keeps_its_colour_and_the_others_get_unused_palette_colours() {
    let (temp, paths, db) = fixture();
    let first = category(&db, "Groceries", "#FF5722");
    let second = category(&db, "Travel", "#FF5722");
    let third = category(&db, "Cash", "#FF5722");
    let untouched = category(&db, "Health", "#123ABC");
    let mut state = loaded(&paths, &db);

    state.fix_duplicate_colours(&db).unwrap();

    let colors: BTreeMap<CategoryId, String> = state
        .categories
        .iter()
        .map(|category| (category.id, category.color.clone()))
        .collect();
    assert_eq!(colors[&first], "#FF5722", "the lowest id keeps the shared colour");
    assert_eq!(colors[&second], "#E76530", "the first unused palette colour");
    assert_eq!(colors[&third], "#4693F3", "the next unused palette colour");
    assert_eq!(colors[&untouched], "#123ABC", "a unique colour is never changed");
    let distinct: BTreeSet<String> = state
        .categories
        .iter()
        .map(|category| category.color.to_uppercase())
        .collect();
    assert_eq!(
        distinct.len(),
        state.categories.len(),
        "after the fix nothing shares a colour"
    );
    assert_eq!(
        state.message,
        Some(i18n::format(
            Language::En,
            Key::MessageColoursFixed,
            &[Arg::from(2_usize)]
        ))
    );
    assert_eq!(
        audit_details(&db, "FIX_CATEGORY_COLOURS"),
        vec!["Recoloured 2 categories".to_owned()]
    );
    drop(temp);
}

#[test]
fn fixing_duplicate_colours_never_touches_names_or_other_fields() {
    let (temp, paths, db) = fixture();
    let groceries = category(&db, "Groceries", "#FF5722");
    let card = category(&db, "Card", "#FF5722");
    repo::categories::update(
        &db,
        card,
        NewCategory {
            name: "Card".to_owned(),
            color: "#FF5722".to_owned(),
            budget_limit: None,
            is_tax_relevant: false,
            is_transfer: true,
        },
    )
    .unwrap();
    repo::categories::update(
        &db,
        groceries,
        NewCategory {
            name: "Groceries".to_owned(),
            color: "#FF5722".to_owned(),
            budget_limit: Some(Money {
                minor: 12_000,
                currency: Currency::EUR,
            }),
            is_tax_relevant: false,
            is_transfer: false,
        },
    )
    .unwrap();
    let _travel = category(&db, "Travel", "#FF5722");
    let mut state = loaded(&paths, &db);

    state.fix_duplicate_colours(&db).unwrap();

    let items: BTreeMap<CategoryId, &Category> = state
        .categories
        .iter()
        .map(|category| (category.id, category))
        .collect();
    assert_eq!(items[&groceries].name, "Groceries");
    assert!(items[&card].is_transfer);
    assert_eq!(
        items[&groceries].budget_limit,
        Some(Money {
            minor: 12_000,
            currency: Currency::EUR
        })
    );
    assert!(!items[&groceries].is_tax_relevant);
    drop(temp);
}

#[test]
fn a_second_fix_changes_nothing_and_writes_no_audit_entry() {
    let (temp, paths, db) = fixture();
    category(&db, "Groceries", "#FF5722");
    category(&db, "Travel", "#FF5722");
    let mut state = loaded(&paths, &db);
    state.fix_duplicate_colours(&db).unwrap();
    state.clear_messages();
    let colors_after_first: BTreeMap<CategoryId, String> = state
        .categories
        .iter()
        .map(|category| (category.id, category.color.clone()))
        .collect();
    let fixes_after_first = audit_details(&db, "FIX_CATEGORY_COLOURS");

    state.fix_duplicate_colours(&db).unwrap();

    let colors_after_second: BTreeMap<CategoryId, String> = state
        .categories
        .iter()
        .map(|category| (category.id, category.color.clone()))
        .collect();
    assert_eq!(
        colors_after_first, colors_after_second,
        "nothing is recoloured a second time"
    );
    assert_eq!(
        fixes_after_first,
        audit_details(&db, "FIX_CATEGORY_COLOURS"),
        "no further audit entry"
    );
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageColoursAlreadyDistinct))
    );
    drop(temp);
}

#[test]
fn lower_and_upper_case_hex_count_as_the_same_colour() {
    let (temp, paths, db) = fixture();
    let first = category(&db, "Groceries", "#ff5722");
    let second = category(&db, "Travel", "#FF5722");
    let mut state = loaded(&paths, &db);

    state.fix_duplicate_colours(&db).unwrap();

    let colors: BTreeMap<CategoryId, String> = state
        .categories
        .iter()
        .map(|category| (category.id, category.color.clone()))
        .collect();
    assert_eq!(colors[&first], "#ff5722", "the lowest id keeps its colour untouched");
    assert_eq!(colors[&second], "#E76530");
    assert_eq!(
        state.message,
        Some(i18n::format(
            Language::En,
            Key::MessageColoursFixed,
            &[Arg::from(1_usize)]
        ))
    );
    assert_eq!(
        audit_details(&db, "FIX_CATEGORY_COLOURS"),
        vec!["Recoloured 1 categories".to_owned()]
    );
    drop(temp);
}

// RuleImportViewModelTest.kt

#[test]
fn a_rejected_rule_file_sets_an_error_and_no_preview() {
    let (temp, paths, db) = fixture();
    category(&db, "Groceries", "#FF5722");
    let bad = temp.path().join("rules.tsv");
    std::fs::write(&bad, "Definitely not a rule file\njust some text\n").unwrap();
    let mut state = loaded(&paths, &db);

    state.choose_rule_import(&db, &bad).unwrap();

    assert_eq!(
        state.error.as_deref(),
        Some(i18n::text(Language::En, Key::ErrorRuleFileHeader))
    );
    assert_eq!(state.rule_import, None);
    assert!(state.rules.is_empty());
    drop(temp);
}

#[test]
fn confirm_applies_the_preview_and_reports_the_counts() {
    let (temp, paths, db) = fixture();
    category(&db, "Groceries", "#FF5722");
    let file = rule_file(
        temp.path(),
        "rules.tsv",
        &["Groceries\tmusterladen\tpartner\texpense\t60\ttrue"],
    );
    let mut state = loaded(&paths, &db);

    state.choose_rule_import(&db, &file).unwrap();
    let preview = state.rule_import.as_ref().unwrap();
    assert_eq!(
        preview.match_count, 0,
        "there are no bookings, so nothing would be categorized"
    );
    assert!(preview.categories_to_create.is_empty());

    state.confirm_rule_import(&db, true).unwrap();

    assert_eq!(state.rule_import, None, "the preview is consumed");
    assert_eq!(state.error, None);
    assert_eq!(
        state.message,
        Some(i18n::format(
            Language::En,
            Key::MessageRulesImported,
            &[Arg::from(1_i64), Arg::from(0_i64), Arg::from(0_i64)]
        ))
    );
    assert_eq!(state.rules.len(), 1);
    assert_eq!(state.rules[0].priority, 60);
    assert_eq!(state.rules[0].pattern, "musterladen");
    assert_eq!(state.categories.len(), 1, "Groceries existed before");
    drop(temp);
}

#[test]
fn export_writes_the_rule_file_and_reports_the_counts() {
    let (temp, paths, db) = fixture();
    let groceries = category(&db, "Groceries", "#FF5722");
    repo::rules::create(
        &db,
        NewRule {
            pattern: "musterladen".to_owned(),
            category_id: groceries,
            priority: 60,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
    let output = temp.path().join("out.tsv");
    let mut state = loaded(&paths, &db);

    state.export_rules(&db, &output).unwrap();

    assert_eq!(state.error, None);
    assert_eq!(
        state.message,
        Some(i18n::format(
            Language::En,
            Key::MessageRulesExported,
            &[Arg::from(1_usize)]
        ))
    );
    assert!(
        std::fs::read_to_string(&output).unwrap().starts_with(
            "# FinDesk rules v1\ncategory\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply\nGroceries\tmusterladen\tany\tany\t60\ttrue\n"
        )
    );
    drop(temp);
}

#[test]
fn cancelling_drops_the_preview_and_the_confirmation_applies_nothing() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);
    let file = rule_file(temp.path(), "rules.tsv", &["Cash\traw cash\tany\tany\t1\ttrue"]);

    state.choose_rule_import(&db, &file).unwrap();
    assert_eq!(
        state.rule_import.as_ref().map(|preview| preview.new_rules.len()),
        Some(1),
        "the preview is staged, the database untouched"
    );
    assert!(
        repo::rules::list(&db).unwrap().is_empty(),
        "choosing alone creates nothing"
    );

    state.cancel_rule_import();
    state.confirm_rule_import(&db, false).unwrap();
    assert!(state.rules.is_empty(), "a cancelled import applies nothing");
    drop(temp);
}

#[test]
fn two_confirmations_create_each_rule_and_one_audit_entry() {
    let (temp, paths, db) = fixture();
    category(&db, "Groceries", "#FF5722");
    let mut state = loaded(&paths, &db);
    let file = rule_file(
        temp.path(),
        "rules.tsv",
        &["Groceries\tmusterladen\tpartner\texpense\t60\ttrue"],
    );
    state.choose_rule_import(&db, &file).unwrap();

    state.confirm_rule_import(&db, true).unwrap();
    state.confirm_rule_import(&db, true).unwrap();

    assert_eq!(repo::rules::list(&db).unwrap().len(), 1, "the rule is created once");
    assert_eq!(
        audit_details(&db, "IMPORT_RULES").len(),
        1,
        "the import is audited once"
    );
    drop(temp);
}

// TaxOverrideTest.kt (the settings part)

#[test]
fn save_rule_stores_the_tax_flag_and_an_explicit_null_stores_null() {
    let (temp, paths, db) = fixture();
    let groceries = category(&db, "Groceries", "#FF5722");
    let mut state = SettingsState::new(Language::En);
    state.load(&db, &paths, None).unwrap();

    state
        .save_rule(
            &db,
            None,
            "Beispiel Shop",
            groceries,
            5,
            true,
            RuleField::Any,
            RuleDirection::Any,
            Some(true),
        )
        .unwrap();
    let item = repo::rules::list(&db).unwrap().into_iter().next().unwrap();
    assert_eq!(item.tax_relevant, Some(true), "the flag is stored with the new rule");
    let rule_id = item.id;

    state
        .save_rule(
            &db,
            Some(rule_id),
            "Beispiel Shop",
            groceries,
            5,
            true,
            RuleField::Any,
            RuleDirection::Any,
            None,
        )
        .unwrap();
    let after = repo::rules::list(&db).unwrap().into_iter().next().unwrap();
    assert_eq!(after.tax_relevant, None, "explicit null stores 'as the category'");
    assert_eq!(after.id, rule_id, "the edit updated the same rule");
    drop(temp);
}

// TaxTabTest.kt (the settings part)

#[test]
fn saving_keywords_stores_the_cleaned_list_and_reset_returns_to_the_defaults() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);
    assert_eq!(state.tax_keywords, DEFAULT_TAX_KEYWORDS.join("\n"));

    state
        .save_tax_keywords(&db, "  Steuerberater \n\n handwerker\nKita\nHANDWERKER  \n")
        .unwrap();

    assert_eq!(
        crate::app::settings::tax_keywords(&db).unwrap(),
        vec!["Steuerberater".to_owned(), "handwerker".to_owned(), "Kita".to_owned()],
        "trimmed, non-blank, de-duplicated case-insensitively"
    );
    assert_eq!(state.tax_keywords, "Steuerberater\nhandwerker\nKita");

    let saved = audit_details(&db, "SAVE_TAX_KEYWORDS");
    assert_eq!(saved.len(), 1);
    assert!(saved[0].contains('3'), "the entry names the count only: {}", saved[0]);

    state.reset_tax_keywords(&db).unwrap();
    assert_eq!(
        crate::app::settings::tax_keywords(&db).unwrap(),
        DEFAULT_TAX_KEYWORDS.to_vec()
    );
    assert_eq!(state.tax_keywords, DEFAULT_TAX_KEYWORDS.join("\n"));
    let resets = audit_details(&db, "RESET_TAX_KEYWORDS");
    assert_eq!(resets.len(), 1);
    assert!(resets[0].contains('3'));
    assert_eq!(
        get_setting(&db, TAX_KEYWORDS_KEY).unwrap(),
        None,
        "the custom list is gone"
    );
    drop(temp);
}

// InsightsTest.kt (the settings part)

#[test]
fn a_budget_can_be_removed_again() {
    let (temp, paths, db) = fixture();
    let food = category(&db, "Food", "#FF5722");
    repo::categories::update(
        &db,
        food,
        NewCategory {
            name: "Food".to_owned(),
            color: "#FF5722".to_owned(),
            budget_limit: Some(Money {
                minor: 10_000,
                currency: Currency::EUR,
            }),
            is_tax_relevant: false,
            is_transfer: false,
        },
    )
    .unwrap();
    let mut state = loaded(&paths, &db);

    state
        .save_category(&db, Some(food), "Food", "#FF5722", None, false, false)
        .unwrap();

    let saved = repo::categories::get(&db, food).unwrap().unwrap();
    assert_eq!(saved.budget_limit, None);
    drop(temp);
}

// ViewModelsTest.kt (the settings part)

#[test]
fn settings_validates_rules_and_deleting_a_category_unassigns_transactions() {
    let (temp, paths, db) = fixture();
    let food = category(&db, "Food", "#4CAF50");
    let account = account(&db);
    transaction(&db, account, Some(food));
    repo::rules::create(
        &db,
        NewRule {
            pattern: "rewe".to_owned(),
            category_id: food,
            priority: 0,
            auto_apply: true,
            match_field: RuleField::Any,
            direction: RuleDirection::Any,
            tax_relevant: None,
        },
    )
    .unwrap();
    let mut state = loaded(&paths, &db);

    assert!(state.validate_rule("(unclosed").is_some());
    assert_eq!(state.validate_rule("rewe|edeka"), None);
    assert_eq!(
        state.validate_category("food", "#000000", None),
        Some(i18n::text(Language::En, Key::ErrorCategoryExists).to_owned())
    );

    state.delete_category(&db, food).unwrap();

    let booking = repo::transactions::list_for_account(&db, account, None, None)
        .unwrap()
        .into_iter()
        .next()
        .unwrap();
    assert_eq!(booking.category_id, None);
    assert!(repo::rules::list(&db).unwrap().is_empty(), "its rules are removed");
    assert_eq!(audit_details(&db, "category.delete").len(), 1);
    drop(temp);
}

// SavedPinTest.kt (profile deletion remembers to forget the PINs)

#[test]
fn deleting_a_profile_forgets_the_pins_before_moving_the_folder() {
    let (temp, paths, db) = fixture();
    let business = profiles::create(&paths, "Business").unwrap();
    let slug = business.slug.clone().unwrap();
    let passports = business.dir.join("passports");
    std::fs::create_dir_all(&passports).unwrap();
    std::fs::write(passports.join("bank-1.dat"), "passport").unwrap();

    let mut state = loaded(&paths, &db);
    let seen = RefCell::new(Vec::new());
    let existed = Cell::new(false);
    let hook = |slug: &str| {
        seen.borrow_mut().push(slug.to_owned());
        existed.set(passports.join("bank-1.dat").exists());
    };

    state.delete_profile(&db, &paths, None, &slug, &hook).unwrap();

    assert_eq!(*seen.borrow(), vec![slug.clone()]);
    assert!(existed.get(), "the hook runs while the profile folder still exists");
    assert!(!business.dir.exists(), "the profile folder moved");
    assert!(
        !profiles::list(&paths)
            .unwrap()
            .iter()
            .any(|profile| profile.slug.as_deref() == Some(slug.as_str()))
    );
    assert!(
        state
            .message
            .as_deref()
            .is_some_and(|message| message.contains("Profile moved to")),
        "{:?}",
        state.message
    );
    assert_eq!(audit_details(&db, "PROFILE_DELETED").len(), 1);
    drop(temp);
}

#[test]
fn a_refused_profile_delete_forgets_no_pins() {
    let (temp, paths, db) = fixture();
    let business = profiles::create(&paths, "Business").unwrap();
    let slug = business.slug.clone().unwrap();
    let mut state = loaded(&paths, &db);
    let calls = Cell::new(0_usize);
    let hook = |_slug: &str| calls.set(calls.get() + 1);

    // The open profile cannot be deleted.
    assert!(state.delete_profile(&db, &paths, Some(&slug), &slug, &hook).is_err());
    assert_eq!(calls.get(), 0, "an open profile is refused before forgetting PINs");
    assert!(business.dir.exists(), "the folder stays");

    // An unknown profile cannot be deleted either.
    assert!(state.delete_profile(&db, &paths, None, "missing", &hook).is_err());
    assert_eq!(calls.get(), 0, "an unknown profile is refused before forgetting PINs");

    // The default profile cannot be deleted.
    assert!(state.delete_profile(&db, &paths, None, "default", &hook).is_err());
    assert_eq!(calls.get(), 0, "the default profile is refused before forgetting PINs");
    assert_eq!(audit_details(&db, "PROFILE_DELETED").len(), 0);
    drop(temp);
}

// The backup and restore flow the Settings page drives.

#[test]
fn a_backup_can_be_created_chosen_staged_and_cancelled() {
    let (temp, paths, db) = fixture();
    let mut state = loaded(&paths, &db);
    let backup = temp.path().join("backup.zip");

    state.create_backup(&db, &paths, None, &backup).unwrap();
    assert!(backup.is_file());
    assert!(!state.is_busy);
    assert!(
        state
            .message
            .as_deref()
            .is_some_and(|message| message.starts_with("Backup saved to backup.zip")),
        "{:?}",
        state.message
    );
    assert_eq!(audit_details(&db, "BACKUP_CREATED").len(), 1);

    state.choose_restore(&paths, None, &backup).unwrap();
    assert!(state.restore_candidate.is_some());
    assert_eq!(state.restore_candidate.as_ref().unwrap().info.transactions, 0);

    state.confirm_restore(&db, &paths, None).unwrap();
    assert!(state.has_pending_restore);
    assert_eq!(state.restore_candidate, None);
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageRestorePrepared))
    );
    assert_eq!(audit_details(&db, "RESTORE_STAGED").len(), 1);

    state.cancel_pending_restore(&db, &paths, None).unwrap();
    assert!(!state.has_pending_restore);
    assert_eq!(
        state.message.as_deref(),
        Some(i18n::text(Language::En, Key::MessageRestoreCancelled))
    );
    drop(temp);
}
