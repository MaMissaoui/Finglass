//! The plain-text rule file, ported from FinDesk's `importer/RuleFile.kt`.
//!
//! The file is UTF-8 with six tab-separated columns and LF line endings. Parsing is pure: no database, no file system.

use thiserror::Error;

use crate::i18n::{self, Arg, Key, Language};
use crate::model::{RuleDirection, RuleField};

/// One usable rule row of a rule file, with the 1-based file line it was read from.
///
/// Pattern and category name are trimmed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuleFileRow {
    /// The 1-based line in the file.
    pub line: usize,
    /// The category name.
    pub category: String,
    /// The pattern.
    pub pattern: String,
    /// The field the pattern is matched against.
    pub field: RuleField,
    /// The booking direction the rule applies to.
    pub direction: RuleDirection,
    /// The priority, highest first.
    pub priority: i64,
    /// Whether matching bookings are categorized automatically.
    pub auto_apply: bool,
}

/// One row that could not be read; `reason` is a localized string.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuleFileError {
    /// The 1-based line in the file.
    pub line: usize,
    /// The localized reason.
    pub reason: String,
}

/// All usable rows of a rule file plus everything that had to be skipped.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ParsedRuleFile {
    /// The usable rows.
    pub rows: Vec<RuleFileRow>,
    /// The rows that could not be read.
    pub errors: Vec<RuleFileError>,
}

/// A whole-file problem, with a localized message.
#[derive(Debug, Clone, PartialEq, Eq, Error)]
#[error("{message}")]
pub struct RuleFileParseError {
    /// The localized message.
    pub message: String,
}

impl RuleFileParseError {
    fn new(lang: Language, key: Key) -> Self {
        Self {
            message: i18n::text(lang, key).to_owned(),
        }
    }
}

/// A rule as the file writer sees it: the pattern, the category's display name and the rule's fields.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RuleFileItem {
    /// The text pattern.
    pub pattern: String,
    /// The category's display name.
    pub category_name: String,
    /// The priority, highest first.
    pub priority: i64,
    /// Whether matching bookings are categorized automatically.
    pub auto_apply: bool,
    /// The field the pattern is matched against.
    pub match_field: RuleField,
    /// The booking direction the rule applies to.
    pub direction: RuleDirection,
}

/// Why `pattern` is unusable as a rule pattern, as a localized user message, or `None` when it is fine.
pub fn validate_rule_pattern(pattern: &str, lang: Language) -> Option<String> {
    if pattern.trim().is_empty() {
        return Some(i18n::text(lang, Key::ErrorPatternRequired).to_owned());
    }

    match fancy_regex::Regex::new(pattern) {
        Ok(_) => None,
        Err(error) => Some(i18n::format(
            lang,
            Key::ErrorInvalidRegex,
            &[Arg::Str(error.to_string())],
        )),
    }
}

/// Reads and writes the plain-text rule file.
pub struct RuleFile;

impl RuleFile {
    /// First line of every written file; on reading it is just another comment line.
    pub const MAGIC: &'static str = "# FinDesk rules v1";

    /// The column header every file must have.
    pub const HEADER: &'static str = "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply";

    /// The largest accepted file, in bytes.
    pub const MAX_FILE_BYTES: usize = 1024 * 1024;

    /// The largest accepted number of data rows.
    pub const MAX_ROWS: usize = 5000;

    const MAX_CATEGORY_LENGTH: usize = 100;
    const MAX_PATTERN_LENGTH: usize = 1000;

    /// Parses the bytes of a rule file.
    ///
    /// Whole-file problems are returned as a [`RuleFileParseError`] with a localized message. A bad data row is
    /// reported in [`ParsedRuleFile::errors`] while the valid rows remain.
    pub fn parse(bytes: &[u8], lang: Language) -> Result<ParsedRuleFile, RuleFileParseError> {
        if bytes.len() > Self::MAX_FILE_BYTES {
            return Err(RuleFileParseError::new(lang, Key::ErrorRuleFileTooLarge));
        }

        let text = std::str::from_utf8(bytes).map_err(|_| RuleFileParseError::new(lang, Key::ErrorRuleFileEncoding))?;
        // A leading byte-order mark decodes to U+FEFF; drop it before the first line.
        let body = text.strip_prefix('\u{feff}').unwrap_or(text);

        let lines: Vec<(usize, &str)> = body
            .split('\n')
            .enumerate()
            .map(|(index, line)| (index + 1, line.strip_suffix('\r').unwrap_or(line)))
            .collect();
        let content: Vec<(usize, &str)> = lines
            .into_iter()
            .filter(|(_, line)| !line.trim().is_empty() && !line.trim_start().starts_with('#'))
            .collect();

        if content.is_empty() || content[0].1.trim() != Self::HEADER {
            return Err(RuleFileParseError::new(lang, Key::ErrorRuleFileHeader));
        }
        if content.len() - 1 > Self::MAX_ROWS {
            return Err(RuleFileParseError::new(lang, Key::ErrorRuleFileTooLarge));
        }

        let mut rows = Vec::new();
        let mut errors = Vec::new();
        for (line, row) in content.into_iter().skip(1) {
            match Self::read_row(line, row, lang) {
                Ok(parsed) => rows.push(parsed),
                Err(reason) => errors.push(RuleFileError { line, reason }),
            }
        }

        Ok(ParsedRuleFile { rows, errors })
    }

    /// The usable [`RuleFileRow`] for one data line, or the first problem found.
    ///
    /// Two invalid keys never fall back to ANY: the row is rejected with its own reason.
    fn read_row(line: usize, text: &str, lang: Language) -> Result<RuleFileRow, String> {
        let columns: Vec<&str> = text.split('\t').collect();
        if columns.len() != 6 {
            return Err(i18n::format(
                lang,
                Key::RuleFileErrorColumns,
                &[Arg::Int(columns.len() as i64)],
            ));
        }

        let category = columns[0].trim();
        if category.is_empty() || category.encode_utf16().count() > Self::MAX_CATEGORY_LENGTH {
            return Err(i18n::text(lang, Key::RuleFileErrorCategory).to_owned());
        }

        let pattern = columns[1].trim();
        if pattern.encode_utf16().count() > Self::MAX_PATTERN_LENGTH {
            return Err(i18n::text(lang, Key::RuleFileErrorPatternLength).to_owned());
        }
        if let Some(reason) = validate_rule_pattern(pattern, lang) {
            return Err(i18n::format(lang, Key::RuleFileErrorPattern, &[Arg::Str(reason)]));
        }

        let search_in = columns[2].trim();
        let field = RuleField::from_stored(&search_in.to_lowercase())
            .ok_or_else(|| i18n::format(lang, Key::RuleFileErrorSearchIn, &[Arg::Str(search_in.to_owned())]))?;

        let applies_to = columns[3].trim();
        let direction = RuleDirection::from_stored(&applies_to.to_lowercase())
            .ok_or_else(|| i18n::format(lang, Key::RuleFileErrorAppliesTo, &[Arg::Str(applies_to.to_owned())]))?;

        let priority_text = columns[4].trim();
        let parsed_priority: i32 = priority_text
            .parse()
            .map_err(|_| i18n::format(lang, Key::RuleFileErrorPriority, &[Arg::Str(priority_text.to_owned())]))?;

        let auto_apply_text = columns[5].trim();
        if auto_apply_text != "true" && auto_apply_text != "false" {
            return Err(i18n::format(
                lang,
                Key::RuleFileErrorAutoApply,
                &[Arg::Str(auto_apply_text.to_owned())],
            ));
        }

        Ok(RuleFileRow {
            line,
            category: category.to_owned(),
            pattern: pattern.to_owned(),
            field,
            direction,
            priority: i64::from(parsed_priority),
            auto_apply: auto_apply_text == "true",
        })
    }

    /// Whether a rule can be written at all: its pattern and category name must fit the one-line TSV format.
    pub fn skipped_rules(rules: &[RuleFileItem]) -> usize {
        rules.iter().filter(|rule| !Self::writable(rule)).count()
    }

    fn writable(rule: &RuleFileItem) -> bool {
        !rule.pattern.chars().any(is_line_break) && !rule.category_name.chars().any(is_line_break)
    }

    /// The rule file text for `rules`: magic line, header, then one line per rule, LF line endings, no BOM.
    ///
    /// A rule whose pattern or category name contains a TAB, CR or LF cannot be represented; it is skipped and
    /// counted.
    pub fn write(rules: &[RuleFileItem]) -> (String, usize) {
        let mut text = String::new();
        text.push_str(Self::MAGIC);
        text.push('\n');
        text.push_str(Self::HEADER);

        let mut skipped = 0_usize;
        for rule in rules {
            if !Self::writable(rule) {
                skipped += 1;
                continue;
            }

            text.push('\n');
            text.push_str(
                &[
                    rule.category_name.clone(),
                    rule.pattern.clone(),
                    rule.match_field.as_str().to_owned(),
                    rule.direction.as_str().to_owned(),
                    rule.priority.to_string(),
                    rule.auto_apply.to_string(),
                ]
                .join("\t"),
            );
        }
        text.push('\n');

        (text, skipped)
    }
}

fn is_line_break(character: char) -> bool {
    matches!(character, '\t' | '\r' | '\n')
}

/// Joins lines the way the tests write rule files.
#[cfg(test)]
fn file(lines: &[&str]) -> Vec<u8> {
    lines.join("\n").into_bytes()
}

#[cfg(test)]
mod tests {
    use super::*;

    const HEADER: &str = "category\tpattern\tsearch_in\tapplies_to\tpriority\tauto_apply";
    const MAGIC: &str = "# FinDesk rules v1";

    fn item(
        pattern: &str,
        category: &str,
        priority: i64,
        auto_apply: bool,
        field: RuleField,
        direction: RuleDirection,
    ) -> RuleFileItem {
        RuleFileItem {
            pattern: pattern.to_owned(),
            category_name: category.to_owned(),
            priority,
            auto_apply,
            match_field: field,
            direction,
        }
    }

    fn row(
        category: &str,
        pattern: &str,
        search_in: &str,
        applies_to: &str,
        priority: &str,
        auto_apply: &str,
    ) -> String {
        [category, pattern, search_in, applies_to, priority, auto_apply].join("\t")
    }

    #[test]
    fn valid_file_round_trip_through_write() {
        let rules = vec![
            item(
                "beispiel.?markt|musterladen",
                "Groceries",
                60,
                true,
                RuleField::Any,
                RuleDirection::Any,
            ),
            item(
                "erika mustermann",
                "Own transfers",
                20,
                true,
                RuleField::Partner,
                RuleDirection::Any,
            ),
            item(
                "beispiel gmbh",
                "Salary",
                70,
                true,
                RuleField::Any,
                RuleDirection::Income,
            ),
            item("raw cash", "Cash", 0, false, RuleField::Purpose, RuleDirection::Any),
        ];

        let text = RuleFile::write(&rules).0;
        let parsed = RuleFile::parse(text.as_bytes(), Language::En).unwrap();

        assert_eq!(parsed.errors, Vec::<RuleFileError>::new());
        assert_eq!(parsed.rows.len(), 4);
        for (index, rule) in rules.iter().enumerate() {
            let written = &parsed.rows[index];
            assert_eq!(
                written.line,
                index + 3,
                "data rows start at file line 3, below magic and header"
            );
            assert_eq!(written.pattern, rule.pattern);
            assert_eq!(written.category, rule.category_name);
            assert_eq!(written.field, rule.match_field);
            assert_eq!(written.direction, rule.direction);
            assert_eq!(written.priority, rule.priority);
            assert_eq!(written.auto_apply, rule.auto_apply);
        }
    }

    #[test]
    fn the_written_file_starts_with_the_magic_and_the_header_and_holds_no_bom() {
        let text = RuleFile::write(&[item(
            "beispiel markt",
            "Groceries",
            50,
            true,
            RuleField::Any,
            RuleDirection::Any,
        )])
        .0;
        let lines: Vec<&str> = text.split('\n').collect();

        assert_eq!(lines[0], MAGIC);
        assert_eq!(lines[1], HEADER);
        assert_eq!(lines[2], row("Groceries", "beispiel markt", "any", "any", "50", "true"));
        assert!(!text.starts_with('\u{feff}'), "no byte order mark is written");
    }

    #[test]
    fn bom_and_crlf_line_endings_are_accepted() {
        let text = format!(
            "\u{feff}{MAGIC}\r\n{}\r\n{}\r\n{}\r\n",
            HEADER,
            row("Erika género", "erika", "any", "any", "50", "true"),
            row("Groceries", "beispiel markt", "any", "any", "50", "true")
        );

        let parsed = RuleFile::parse(text.as_bytes(), Language::En).unwrap();

        assert_eq!(
            parsed.rows.iter().map(|row| row.category.as_str()).collect::<Vec<_>>(),
            vec!["Erika género", "Groceries"]
        );
        assert_eq!(parsed.rows.iter().map(|row| row.line).collect::<Vec<_>>(), vec![3, 4]);
    }

    #[test]
    fn comments_and_blank_lines_are_skipped_but_their_line_numbers_still_count() {
        let parsed = RuleFile::parse(
            &file(&[
                MAGIC,
                "",
                "# a note for the reader",
                "   ",
                HEADER,
                "",
                "# not a rule",
                &row("Cash", "raw cash", "any", "any", "50", "true"),
                &row("Groceries", "beispiel markt", "any", "any", "50", "true"),
            ]),
            Language::En,
        )
        .unwrap();

        assert_eq!(
            parsed.rows.iter().map(|row| row.category.as_str()).collect::<Vec<_>>(),
            vec!["Cash", "Groceries"]
        );
        assert_eq!(parsed.rows.iter().map(|row| row.line).collect::<Vec<_>>(), vec![8, 9]);
    }

    #[test]
    fn a_missing_header_is_rejected() {
        let error = RuleFile::parse(
            &file(&[MAGIC, &row("Cash", "raw cash", "any", "any", "50", "true")]),
            Language::En,
        )
        .unwrap_err();
        assert_eq!(error.message, i18n::text(Language::En, Key::ErrorRuleFileHeader));
    }

    #[test]
    fn a_wrong_header_is_rejected() {
        let error = RuleFile::parse(
            &file(&[
                MAGIC,
                "category\tpattern\tsearch_in\tapplies_to\tpriority\tautoApply",
                &row("Cash", "raw cash", "any", "any", "50", "true"),
            ]),
            Language::En,
        )
        .unwrap_err();
        assert_eq!(error.message, i18n::text(Language::En, Key::ErrorRuleFileHeader));

        // Even a header with the columns in another order is not this file format.
        let swapped = RuleFile::parse(
            &file(&[
                MAGIC,
                "pattern\tcategory\tsearch_in\tapplies_to\tpriority\tauto_apply",
                &row("Cash", "raw cash", "any", "any", "50", "true"),
            ]),
            Language::En,
        )
        .unwrap_err();
        assert_eq!(swapped.message, i18n::text(Language::En, Key::ErrorRuleFileHeader));
    }

    #[test]
    fn a_file_without_any_content_is_rejected_as_a_rule_file() {
        assert_eq!(
            RuleFile::parse(&[], Language::En).unwrap_err().message,
            i18n::text(Language::En, Key::ErrorRuleFileHeader)
        );
        let no_header = RuleFile::parse(b"just some line", Language::En).unwrap_err();
        assert_eq!(no_header.message, i18n::text(Language::En, Key::ErrorRuleFileHeader));
    }

    #[test]
    fn a_file_with_more_than_5000_data_rows_is_rejected() {
        let mut too_many: Vec<String> = (1..=5001)
            .map(|index| {
                row(
                    &format!("Cat {index}"),
                    &format!("muster {index}"),
                    "any",
                    "any",
                    "50",
                    "true",
                )
            })
            .collect();
        let mut lines = vec![MAGIC.to_owned(), HEADER.to_owned()];
        lines.append(&mut too_many);
        let error = RuleFile::parse(lines.join("\n").as_bytes(), Language::En).unwrap_err();
        assert_eq!(error.message, i18n::text(Language::En, Key::ErrorRuleFileTooLarge));

        let mut exactly_enough = vec![MAGIC.to_owned(), HEADER.to_owned()];
        exactly_enough.extend((1..=5000).map(|index| {
            row(
                &format!("Cat {index}"),
                &format!("muster {index}"),
                "any",
                "any",
                "50",
                "true",
            )
        }));
        assert_eq!(
            RuleFile::parse(exactly_enough.join("\n").as_bytes(), Language::En)
                .unwrap()
                .rows
                .len(),
            5000
        );
    }

    #[test]
    fn a_file_larger_than_1_mb_is_rejected() {
        let error = RuleFile::parse(&vec![0_u8; 1024 * 1024 + 1], Language::En).unwrap_err();
        assert_eq!(error.message, i18n::text(Language::En, Key::ErrorRuleFileTooLarge));
    }

    #[test]
    fn bytes_that_are_not_valid_utf8_are_rejected() {
        let mut tail = format!("{MAGIC}\n{HEADER}\n").into_bytes();
        tail.push(0xFF);
        let error = RuleFile::parse(&tail, Language::En).unwrap_err();
        assert_eq!(error.message, i18n::text(Language::En, Key::ErrorRuleFileEncoding));
    }

    #[test]
    fn every_row_error_kind_is_reported_with_its_file_line_while_the_valid_rows_remain() {
        let long_pattern = "a.".repeat(501);
        let parsed = RuleFile::parse(
            &file(&[
                MAGIC,
                HEADER,
                &row("Cat One", "rewe", "any", "any", "50", "true"),
                "Cat Two\tpattern\tpartner\tincome\t50",
                &row("", "pattern", "any", "any", "50", "true"),
                &row("Cat Three", "[unclosed", "any", "any", "50", "true"),
                &row("Cat Four", &long_pattern, "any", "any", "50", "true"),
                &row("Cat Five", "pattern", "companions", "any", "50", "true"),
                &row("Cat Six", "pattern", "any", "outgoing", "50", "true"),
                &row("Cat Seven", "pattern", "any", "any", "bold", "true"),
                &row("Cat Eight", "pattern", "any", "any", "50", "maybe"),
                &row("Cat Nine", "edeka", "any", "any", "50", "true"),
            ]),
            Language::En,
        )
        .unwrap();

        assert_eq!(
            parsed.rows.iter().map(|row| row.category.as_str()).collect::<Vec<_>>(),
            vec!["Cat One", "Cat Nine"]
        );
        assert_eq!(parsed.rows.iter().map(|row| row.line).collect::<Vec<_>>(), vec![3, 12]);
        assert_eq!(
            parsed.errors.iter().map(|error| error.line).collect::<Vec<_>>(),
            vec![4, 5, 6, 7, 8, 9, 10, 11]
        );
        let reasons: Vec<&str> = parsed.errors.iter().map(|error| error.reason.as_str()).collect();
        assert_eq!(
            reasons[0],
            i18n::format(Language::En, Key::RuleFileErrorColumns, &[Arg::Int(5)])
        );
        assert_eq!(reasons[1], i18n::text(Language::En, Key::RuleFileErrorCategory));
        assert_eq!(
            reasons[2],
            i18n::format(
                Language::En,
                Key::RuleFileErrorPattern,
                &[Arg::Str(validate_rule_pattern("[unclosed", Language::En).unwrap())]
            )
        );
        assert_eq!(reasons[3], i18n::text(Language::En, Key::RuleFileErrorPatternLength));
        assert_eq!(
            reasons[4],
            i18n::format(
                Language::En,
                Key::RuleFileErrorSearchIn,
                &[Arg::Str("companions".to_owned())]
            )
        );
        assert_eq!(
            reasons[5],
            i18n::format(
                Language::En,
                Key::RuleFileErrorAppliesTo,
                &[Arg::Str("outgoing".to_owned())]
            )
        );
        assert_eq!(
            reasons[6],
            i18n::format(Language::En, Key::RuleFileErrorPriority, &[Arg::Str("bold".to_owned())])
        );
        assert_eq!(
            reasons[7],
            i18n::format(
                Language::En,
                Key::RuleFileErrorAutoApply,
                &[Arg::Str("maybe".to_owned())]
            )
        );
    }

    #[test]
    fn an_unknown_search_in_key_is_an_error_never_a_silent_fallback_to_any() {
        let parsed = RuleFile::parse(
            &file(&[
                MAGIC,
                HEADER,
                &row("Groceries", "beispiel markt", "partner", "any", "50", "true"),
                &row("Cash", "raw", "purpose only", "any", "50", "true"),
            ]),
            Language::En,
        )
        .unwrap();

        assert_eq!(
            parsed.rows.len(),
            1,
            "the partner row is fine, the unknown key is rejected"
        );
        assert!(
            parsed.rows.iter().all(|row| row.field != RuleField::Purpose),
            "unknown keys are not mapped onto a field"
        );
        assert_eq!(
            parsed.errors.iter().map(|error| error.line).collect::<Vec<_>>(),
            vec![4]
        );
        assert_eq!(
            parsed.errors[0].reason,
            i18n::format(
                Language::En,
                Key::RuleFileErrorSearchIn,
                &[Arg::Str("purpose only".to_owned())]
            )
        );
    }

    #[test]
    fn a_rule_with_a_tab_cr_or_lf_in_its_pattern_or_category_is_skipped_on_write_and_counted() {
        let rules = vec![
            item("musterladen", "Groceries", 50, true, RuleField::Any, RuleDirection::Any),
            item("bad\tregex", "Cash", 50, true, RuleField::Any, RuleDirection::Any),
            item("normal", "Bad\rName", 50, true, RuleField::Any, RuleDirection::Any),
            item(
                "newline\npattern",
                "Other",
                50,
                true,
                RuleField::Any,
                RuleDirection::Any,
            ),
        ];

        let (text, skipped) = RuleFile::write(&rules);
        assert_eq!(skipped, 3, "three rules cannot be written");
        assert_eq!(RuleFile::skipped_rules(&rules), 3);

        let parsed = RuleFile::parse(text.as_bytes(), Language::En).unwrap();
        assert_eq!(
            parsed.rows.iter().map(|row| row.pattern.as_str()).collect::<Vec<_>>(),
            vec!["musterladen"]
        );
        assert_eq!(parsed.rows.len(), 1);
    }

    #[test]
    fn validation_reports_blank_and_invalid_patterns() {
        assert_eq!(
            validate_rule_pattern("   ", Language::En),
            Some(i18n::text(Language::En, Key::ErrorPatternRequired).to_owned())
        );
        assert!(validate_rule_pattern("(", Language::En).is_some());
        assert_eq!(validate_rule_pattern("beispiel", Language::En), None);
    }
}
