use super::*;
use std::collections::BTreeMap;

/// Keys whose FinDesk texts already differ in their placeholders between languages. They are kept
/// verbatim, so the consistency check cannot demand the same placeholder set for them.
///
/// - `PendingLeftOut`, `UnreadableCount`, `SyncSummaryCategorized`, `SyncSummaryDuplicates`,
///   `ImportSummaryAuto`, `ImportSummaryKnown`, `ImportSummaryWrongCurrency`, `ImportSummaryDeleted`,
///   `SelectedCount`, `DeleteSelectedCount`: French adds a plural marker for the same `%d`.
/// - `ImportFilesSkipped`: German repeats the plural marker for the verb.
/// - `WrongCurrencySkipped`, `MessageAutoCategorized`, `ImportSummaryProbable`,
///   `ImportSummaryDeleted`: German/French add a second plural marker for the verb.
/// - `RuleImportDuplicates`: French uses `{1:s||}` for an optional letter, which is not a plural
///   marker under FinDesk's regex; English and German use a real plural marker.
const PLACEHOLDER_ALLOW_LIST: &[Key] = &[
    Key::PendingLeftOut,
    Key::UnreadableCount,
    Key::ImportFilesSkipped,
    Key::WrongCurrencySkipped,
    Key::RuleImportDuplicates,
    Key::MessageAutoCategorized,
    Key::SyncSummaryCategorized,
    Key::SyncSummaryDuplicates,
    Key::ImportSummaryAuto,
    Key::ImportSummaryKnown,
    Key::ImportSummaryProbable,
    Key::ImportSummaryWrongCurrency,
    Key::ImportSummaryDeleted,
    Key::SelectedCount,
    Key::DeleteSelectedCount,
];

/// Variant names that legitimately carry a payment word: these four category strings describe
/// transfers between the user's own accounts and stay part of category management.
const PAYMENT_NAME_EXCEPTIONS: &[&str] = &[
    "CategoryIsTransfer",
    "CategoryIsTransferHint",
    "CategoryTransferLabel",
    "CategorySplitTransfers",
    // Partner field label in the transactions table, not a payment feature.
    "PartnerPayee",
    // Reports statistic for recurring costs, not bank payments.
    "RecurringPayments",
    // Default category for transfers between the user's own accounts, not a payment feature.
    "DefaultCategoryOwnTransfers",
];

#[test]
fn every_key_name_round_trips() {
    for &key in Key::ALL {
        let name = format!("{key:?}");
        assert_eq!(Key::from_name(&name), Some(key), "{name}");
    }
    assert_eq!(Key::from_name("NoSuchKey"), None);
    assert_eq!(Key::from_name(""), None);
}

#[test]
fn every_key_has_text_in_every_language() {
    for &key in Key::ALL {
        for &lang in Language::ALL {
            assert!(!text(lang, key).is_empty(), "{key:?} has no {} text", lang.code());
        }
    }
}

#[test]
fn no_payment_key_remains() {
    for &key in Key::ALL {
        let name = format!("{key:?}");
        for word in ["Payment", "Transfer", "StandingOrder", "Scheduled", "Vop", "Payee"] {
            if name.contains(word) {
                assert!(
                    PAYMENT_NAME_EXCEPTIONS.contains(&name.as_str()),
                    "{name} looks like a payment key"
                );
            }
        }
    }
}

#[test]
fn restored_non_payment_keys_have_fin_desk_english_text() {
    let expected = [
        (Key::TypeChecking, "Checking"),
        (Key::TypeSavings, "Savings"),
        (Key::TypeCreditCard, "Credit card"),
        (Key::TypeLoan, "Loan"),
        (Key::TypeDepot, "Depot"),
        (Key::PeriodThisMonth, "This month"),
        (Key::PeriodLastMonth, "Last month"),
        (Key::PeriodLast3Months, "Last 3 months"),
        (Key::PeriodThisYear, "This year"),
        (Key::PeriodLastYear, "Last year"),
        (Key::PeriodAllTime, "All time"),
        (Key::DirectionAll, "All"),
        (Key::DirectionIncome, "Income"),
        (Key::DirectionExpenses, "Expenses"),
        (Key::CadenceWeekly, "Weekly"),
        (Key::CadenceMonthly, "Monthly"),
        (Key::CadenceQuarterly, "Quarterly"),
        (Key::CadenceYearly, "Yearly"),
        (Key::TabAllDocuments, "All documents"),
        (Key::TabUnlinked, "Unlinked"),
        (Key::TabLinked, "Linked to transaction"),
        (Key::PartnerPayee, "Partner / payee"),
        (Key::RecurringPayments, "Recurring payments"),
        (Key::DefaultCategoryOwnTransfers, "Own transfers"),
    ];
    for (key, expected) in expected {
        assert_eq!(text(Language::En, key), expected, "{key:?}");
    }
}

#[test]
fn placeholders_are_the_same_in_every_language() {
    for &key in Key::ALL {
        let en = placeholder_tokens(text(Language::En, key));
        let de = placeholder_tokens(text(Language::De, key));
        let fr = placeholder_tokens(text(Language::Fr, key));
        if en != de || en != fr {
            assert!(
                PLACEHOLDER_ALLOW_LIST.contains(&key),
                "{key:?} uses different placeholders: en={en:?}, de={de:?}, fr={fr:?}"
            );
        }
    }
}

#[test]
fn formats_a_string_placeholder() {
    assert_eq!(
        format(Language::En, Key::ChangeCategory, &["Groceries".into()]),
        "Change category: Groceries"
    );
    assert_eq!(
        format(Language::De, Key::ChangeCategory, &["Lebensmittel".into()]),
        "Kategorie ändern: Lebensmittel"
    );
    assert_eq!(
        format(Language::Fr, Key::ChangeCategory, &["Courses".into()]),
        "Changer la catégorie : Courses"
    );
}

#[test]
fn formats_positional_arguments() {
    // `%1$d` and `%2$d` name their argument, independent of their order in the text.
    assert_eq!(
        format(Language::En, Key::PdfPageOf, &[2.into(), 7.into()]),
        "Page 2 of 7"
    );
    assert_eq!(
        format(Language::En, Key::PdfPageOf, &[7.into(), 2.into()]),
        "Page 7 of 2"
    );
    assert_eq!(
        format(Language::En, Key::OtherCurrenciesExcluded, &["EUR".into(), 3.into()]),
        "All amounts in EUR. 3 bookings in other currencies are not included."
    );
}

#[test]
fn formats_an_integer_placeholder() {
    assert_eq!(format(Language::En, Key::SelectedCount, &[3.into()]), "3 selected");
}

#[test]
fn formats_a_literal_percent() {
    assert_eq!(
        format(Language::En, Key::ExtractedTextConfidence, &[87.into()]),
        "Extracted text (confidence 87%)"
    );
}

#[test]
fn plural_in_english_uses_the_count() {
    assert_eq!(format(Language::En, Key::BookingCount, &[1.into()]), "1 booking");
    assert_eq!(format(Language::En, Key::BookingCount, &[2.into()]), "2 bookings");
}

#[test]
fn plural_in_french_treats_zero_as_singular() {
    assert_eq!(format(Language::Fr, Key::BookingCount, &[0.into()]), "0 écriture");
    assert_eq!(format(Language::Fr, Key::BookingCount, &[1.into()]), "1 écriture");
    assert_eq!(format(Language::Fr, Key::BookingCount, &[2.into()]), "2 écritures");
}

#[test]
fn missing_and_mismatched_arguments_leave_the_placeholder() {
    assert_eq!(format(Language::En, Key::ChangeCategory, &[]), "Change category: %s");
    assert_eq!(format(Language::En, Key::SelectedCount, &[]), "%d selected");
    assert_eq!(
        format(Language::En, Key::SelectedCount, &["three".into()]),
        "%d selected"
    );
    // The plural marker still picks its plural form when the count is missing.
    assert_eq!(
        format(Language::En, Key::CategorySplitMore, &[]),
        "%1$d more categories"
    );
}

#[test]
fn counts_pick_singular_or_plural_per_language() {
    assert_eq!(format(Language::En, Key::BookingCount, &[1.into()]), "1 booking");
    assert_eq!(format(Language::En, Key::BookingCount, &[0.into()]), "0 bookings");
    assert_eq!(format(Language::De, Key::BookingCount, &[2.into()]), "2 Buchungen");
    assert_eq!(
        format(
            Language::De,
            Key::TransactionsSummary,
            &[1.into(), "€0.00".into(), "€3.50".into()]
        ),
        "1 Umsatz · Eingang €0.00 · Ausgang €3.50"
    );
    assert_eq!(format(Language::Fr, Key::BookingCount, &[0.into()]), "0 écriture");
    assert_eq!(format(Language::Fr, Key::BookingCount, &[2.into()]), "2 écritures");
}

#[test]
fn the_count_can_be_any_argument_and_drive_several_words() {
    assert_eq!(
        format(Language::En, Key::AccountsTotal, &["€5.00".into(), 1.into()]),
        "Total €5.00 across 1 account"
    );
    assert_eq!(
        format(Language::En, Key::RestoreCreated, &["today".into(), 1.into(), 2.into()]),
        "Created today · 1 transaction · 2 documents"
    );
    assert_eq!(
        format(Language::En, Key::WrongCurrencySkipped, &[1.into(), "EUR".into()]),
        "1 booking is in another currency than this EUR account and will be skipped."
    );
    assert_eq!(
        format(Language::En, Key::OtherCurrenciesExcluded, &["EUR".into(), 3.into()]),
        "All amounts in EUR. 3 bookings in other currencies are not included."
    );
}

#[test]
fn every_plural_marker_points_at_a_numeric_argument() {
    for &key in Key::ALL {
        for &lang in Language::ALL {
            let value = text(lang, key);
            let conversions = conversions(value);
            for index in plural_indices(value) {
                assert_eq!(
                    conversions.get(&index),
                    Some(&'d'),
                    "{} {key:?}: {{{index}:…}} needs a %d as argument {index}",
                    lang.code()
                );
            }
        }
    }
}

#[test]
fn no_string_falls_back_to_bracketed_plurals() {
    for &key in Key::ALL {
        for &lang in Language::ALL {
            let value = text(lang, key);
            for suffix in ["(s)", "(en)", "(e)", "(n)"] {
                if let Some(position) = value.find(suffix) {
                    let before = value[..position].chars().next_back();
                    assert!(
                        !before.is_some_and(|ch| ch.is_alphanumeric()),
                        "{} {key:?}: \"{value}\" — use braces instead of {suffix}",
                        lang.code()
                    );
                }
            }
            assert!(
                !value.contains("Konto/Konten") && !value.contains("é(s)") && !value.contains("e(s)"),
                "{} {key:?}: \"{value}\" — use braces instead of a bracketed plural",
                lang.code()
            );
        }
    }
}

#[test]
fn language_codes_and_names_round_trip() {
    for &lang in Language::ALL {
        assert_eq!(Language::from_code(lang.code()), Some(lang));
        assert_eq!(Language::from_code(&lang.code().to_ascii_uppercase()), Some(lang));
        assert!(!lang.native_name().is_empty());
    }
    assert_eq!(Language::from_code("es"), None);
    assert_eq!(Language::from_code(""), None);
}

#[test]
fn singular_rules_match_fin_desk() {
    assert!(Language::Fr.is_singular(0));
    assert!(Language::Fr.is_singular(1));
    assert!(!Language::Fr.is_singular(2));
    for lang in [Language::En, Language::De] {
        assert!(!lang.is_singular(0));
        assert!(lang.is_singular(1));
        assert!(!lang.is_singular(2));
    }
}

/// Normalized placeholders: a conversion and its 1-based argument index, plus `pluralN` markers.
fn placeholder_tokens(template: &str) -> Vec<String> {
    let mut tokens: Vec<String> = conversions(template)
        .into_iter()
        .map(|(index, conversion)| format!("{conversion}{index}"))
        .collect();
    tokens.extend(
        plural_indices(template)
            .into_iter()
            .map(|index| format!("plural{index}")),
    );
    tokens.sort();
    tokens
}

/// The conversions in argument order, mirroring Java's `String.format`.
fn conversions(template: &str) -> BTreeMap<usize, char> {
    let bytes = template.as_bytes();
    let mut result = BTreeMap::new();
    let mut next = 1_usize;
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] != b'%' {
            i += 1;
            continue;
        }
        if i + 1 < bytes.len() && bytes[i + 1] == b'%' {
            i += 2;
            continue;
        }
        let start = i + 1;
        let mut j = start;
        while j < bytes.len() && bytes[j].is_ascii_digit() {
            j += 1;
        }
        let explicit = if j > start && j < bytes.len() && bytes[j] == b'$' {
            let index = template[start..j].parse::<usize>().ok();
            j += 1;
            index
        } else {
            j = start;
            None
        };
        while j < bytes.len() && matches!(bytes[j], b'-' | b'#' | b'+' | b' ' | b'0' | b',' | b'(') {
            j += 1;
        }
        while j < bytes.len() && bytes[j].is_ascii_digit() {
            j += 1;
        }
        if j < bytes.len() && bytes[j] == b'.' {
            j += 1;
            while j < bytes.len() && bytes[j].is_ascii_digit() {
                j += 1;
            }
        }
        if j < bytes.len() && matches!(bytes[j], b's' | b'd') {
            let conversion = char::from(bytes[j]);
            match explicit {
                Some(index) => {
                    result.insert(index, conversion);
                }
                None => {
                    result.insert(next, conversion);
                    next += 1;
                }
            }
            i = j + 1;
        } else {
            i = start;
        }
    }
    result
}

fn plural_indices(template: &str) -> Vec<usize> {
    let mut indices = Vec::new();
    let mut i = 0;
    while i < template.len() {
        if template.as_bytes()[i] == b'{'
            && let Some((end, index, _, _)) = plural_marker(template, i)
        {
            indices.push(index);
            i = end;
            continue;
        }
        i += 1;
    }
    indices
}
