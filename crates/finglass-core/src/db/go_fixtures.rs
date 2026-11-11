//! Test-only fixtures shared with the Go SQLCipher gate.
//!
//! The generator runs by hand:
//!
//! ```text
//! cargo test -p finglass-core write_go_fixtures -- --ignored
//! ```
//!
//! It writes the Rust goldens (`testdata/golden/`) and `testdata/sqlcipher/rust-created.db`. The Go side writes
//! `testdata/sqlcipher/go-created.db`; [`opens_the_go_created_fixture`] checks that Rust can read it.

use std::fs;
use std::path::PathBuf;

use super::migrations::MIGRATIONS;
use super::{Database, DbError};
use crate::secret::{Passphrase, PassphraseError, PassphrasePurpose};

/// The test-only passphrase for the cross-language fixtures. It contains embedded single and double quotes, a
/// decomposed `é`, SQL-looking punctuation and non-ASCII characters on purpose. It is a dummy and protects nothing.
pub(crate) const FIXTURE_PASSPHRASE: &str = "T\u{eb}st 'q' \"dq\" e\u{301} \u{2014} ;?%+&=# \u{fc}n\u{ef}code";

/// The connection pragmas compared across languages, read with `PRAGMA <name>` on a live connection.
const CONNECTION_PRAGMAS: &[&str] = &[
    "synchronous",
    "journal_mode",
    "locking_mode",
    "foreign_keys",
    "busy_timeout",
    "cipher_page_size",
    "kdf_iter",
    "cipher_hmac_algorithm",
    "cipher_kdf_algorithm",
    "cipher_plaintext_header_size",
];

/// The repository root, found from this crate's manifest directory.
fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(2)
        .expect("crates/finglass-core has a repository root")
        .to_path_buf()
}

fn golden_dir() -> PathBuf {
    repo_root().join("testdata").join("golden")
}

fn sqlcipher_dir() -> PathBuf {
    repo_root().join("testdata").join("sqlcipher")
}

fn pdf_dir() -> PathBuf {
    repo_root().join("testdata").join("pdf")
}

/// The pattern strings the Go `regexp2` corpus checks.
///
/// They are collected by hand from `rules/payment_providers.rs` (the final strings its `Regex::new`
/// calls receive) and from the pattern literals in the rules, matcher and import tests, plus the
/// edge cases this milestone requires.
fn regex_patterns() -> Vec<String> {
    fn flexible(word: &str) -> String {
        word.chars()
            .map(|character| character.to_string())
            .collect::<Vec<_>>()
            .join("\\s?")
    }

    let ihr = format!("{}\\b", flexible("Ihr"));
    let mut patterns = vec![
        // payment_providers.rs
        "(?i)(EREF|KREF|MREF|CRED|DEBT|SVWZ|ABWA|ABWE|IBAN|BIC|PURP|COAM|OAMT|SQTP|BREF|RREF)\\+".to_owned(),
        format!(
            "(?i){}\\s+{}\\s+{}",
            flexible("Ihr"),
            flexible("Einkauf"),
            flexible("bei")
        ),
        format!("(?i)PP\\.\\d+\\.PP[A-Z]?\\s*\\.?\\s*(.+?)\\s*,\\s*{ihr}"),
        format!("(?i)^\\.\\s*(.+?)\\s*,\\s*{ihr}"),
        "(?i)PP\\.\\d+\\.PP[A-Z]?".to_owned(),
        "(?i)^PAYPAL\\s*\\*".to_owned(),
        "(?i)(?:\\s+|^)\\d+(?=\\s|$)|,\\s*\\d".to_owned(),
        ",\\s*\\S*\\d\\S*$".to_owned(),
        format!("\\b(?:{}\\s+)?{}\\b.*", flexible("ABBUCHUNG"), flexible("VOM")),
        "(?i)PP\\.\\d+\\.PP".to_owned(),
        // rules/matcher.rs tests
        "Erika Mustermann".to_owned(),
        "Gehalt".to_owned(),
        "REWE".to_owned(),
        "Beispiel".to_owned(),
        "beispielpay".to_owned(),
        "erika".to_owned(),
        "Bäckerei".to_owned(),
        "^(?!.*refund).*amazon".to_owned(),
        "(".to_owned(),
        "PayPal".to_owned(),
        "Beispiel Shop".to_owned(),
        "Ihr Einkauf".to_owned(),
        "BEISPIELSHOP".to_owned(),
        "Rückerstattung|Storno".to_owned(),
        "RETURN".to_owned(),
        "coffee".to_owned(),
        "ignored".to_owned(),
        // rules/file.rs and rules/import.rs tests
        "beispiel.?markt|musterladen".to_owned(),
        "erika mustermann".to_owned(),
        "beispiel gmbh".to_owned(),
        "raw cash".to_owned(),
        "beispiel markt".to_owned(),
        "raw".to_owned(),
        "edeka".to_owned(),
        "musterladen".to_owned(),
        "bad\tregex".to_owned(),
        "normal".to_owned(),
        "newline\npattern".to_owned(),
        "[unclosed".to_owned(),
        "[".to_owned(),
        "pattern".to_owned(),
        "muster gmbh".to_owned(),
        "gehalt".to_owned(),
        "rewe".to_owned(),
        "bad\tstock".to_owned(),
        "a.".repeat(501),
        // Edge cases required by the Go corpus.
        "(?=.*rewe)(?=.*markt)".to_owned(),
        "^(?!.*erstattung).*amazon".to_owned(),
        "(?<=PayPal \\*)\\w+".to_owned(),
        "(?<!Gut)schrift".to_owned(),
        "(\\w)\\1".to_owned(),
        "\\bmüller\\b".to_owned(),
        "straße|strasse".to_owned(),
        "\\p{Lu}{3,}".to_owned(),
        "\\d{2}\\.\\d{2}\\.\\d{4}".to_owned(),
        "(?P<x>a)".to_owned(),
        "a{2,1}".to_owned(),
        "*".to_owned(),
        "(?i)ÄÖÜ".to_owned(),
        "É".to_owned(),
        "ǅ".to_owned(),
        "ß".to_owned(),
        "ẞ".to_owned(),
        "\\s+gmbh$".to_owned(),
        "^sepa-lastschrift".to_owned(),
        ".*".to_owned(),
        String::new(),
        "(a+)+$".to_owned(),
    ];

    let mut seen = std::collections::BTreeSet::new();
    patterns.retain(|pattern| seen.insert(pattern.clone()));
    patterns
}

/// The booking texts every corpus pattern runs against.
fn regex_inputs() -> Vec<&'static str> {
    vec![
        "Erstattung Amazon",
        "AMAZON EU S.A R.L.",
        "PayPal *Spotify",
        "Gutschrift",
        "REWE Markt GmbH",
        "Müller Drogerie",
        "MUELLER",
        "Straße",
        "STRASSE",
        "25.10.2026",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaa!",
        "BÄCKEREI SCHMIDT",
        "Bäckerei Müller & Söhne",
        "SEPA-Lastschrift REWE Markt GmbH",
        "sepa-lastschrift edeka",
        "Ihr Einkauf bei Beispiel Shop GmbH",
        "EREF+R1 SVWZ+PP.471.PPX . Beispiel Shop GmbH, Ihr Einkauf bei Beispiel Shop GmbH",
        "PAYPAL *BEISPIELSHOP 12345678 RETURN",
        "Rückerstattung Amazon",
        "AMAZON EU order",
        "Amazon refund",
        "Gutschrift von Müller",
        "PayPal *Spotify AB",
        "PayPal Europe S.a.r.l. et Cie S.C.A",
        "École française 12,50 €",
        "Café de Flore, Paris",
        "Crème brûlée 8,90 EUR",
        "Débit carte 1.234,56 €",
        "Paiement CB «Boulangerie»",
        "Groceries: Milch 1,29",
        "Rechnung Nr. 4711 vom 12.03.2026",
        "Gesamtbetrag 89,99 EUR",
        "1.234,56 €",
        "EUR 1,000.00",
        "Straße ẞ ẞ",
        "ǅungla",
        "ÄÖÜ äöü",
        "Éé Èè Êê Ëë",
        "Gutschrift 2026-01-05",
        "Zahlung an Erika Mustermann",
        " Gehalt Oktober ",
        "Amount (EUR)",
        "Amount EUR",
        "Muster GmbH",
        "BeispielPay",
        "Kartenzahlung",
        "Überweisung an Erika",
        "Musterladen",
        "bank transfer",
        "Rechnung\nNr. 4711",
        "Mehrwertsteuer\t19 %",
        "Gutschrift\u{a0}2026",
        "Narrow\u{202f}space",
        "a",
        "aa",
        "aaa",
        "aaaa",
    ]
}

fn regex_corpus_json() -> String {
    let mut entries = Vec::new();

    for pattern in regex_patterns() {
        let compiled = crate::rules::matcher::compile(&pattern);
        let is_regex = matches!(compiled, crate::rules::matcher::CompiledPattern::Regex(_));
        let results: Vec<String> = regex_inputs()
            .iter()
            .map(|input| {
                format!(
                    "{{\"input\":{},\"matches\":{}}}",
                    json_string(input),
                    compiled.is_match(input)
                )
            })
            .collect();

        entries.push(format!(
            "{{\"pattern\":{},\"is_regex\":{},\"results\":[{}]}}",
            json_string(&pattern),
            is_regex,
            results.join(",")
        ));
    }

    format!("[{}]", entries.join(","))
}

/// The profile directories and bank ids the Go `AccountFor` golden covers.
fn keychain_accounts_json() -> String {
    let profiles = [
        "/tmp/finglass-profiles/default",
        "/tmp/finglass-profiles/anna",
        "/tmp/finglass-profiles/ben",
    ];
    let banks = [7_i64, 42_i64];
    let mut entries = Vec::new();

    for profile in profiles {
        for bank in banks {
            entries.push(format!(
                "{{\"profile_dir\":{},\"bank_id\":{},\"account\":{}}}",
                json_string(profile),
                bank,
                json_string(&crate::sync::keychain::account_for(std::path::Path::new(profile), bank))
            ));
        }
    }

    format!("[{}]", entries.join(","))
}

/// The PDF fixtures the Rust document tests use, by file name.
const PDF_FIXTURES: &[&str] = &[
    "findesk-receipt.pdf",
    "text-invoice.pdf",
    "text-invoice-dated.pdf",
    "text-receipt.pdf",
    "text-receipt-long.pdf",
    "text-receipt-rewe.pdf",
    "confidential.pdf",
    "empty.pdf",
    "multipage.pdf",
    "truncated.pdf",
    "malformed.pdf",
];

/// Writes the PDF fixtures into `directory`.
fn write_pdf_fixtures(directory: &std::path::Path) {
    use crate::documents::test_support::{empty_pdf, text_pdf};

    fs::create_dir_all(directory).unwrap();
    text_pdf(
        &directory.join("text-invoice.pdf"),
        &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"],
    );
    text_pdf(
        &directory.join("text-invoice-dated.pdf"),
        &["Rechnung Nr. 4711", "Datum: 12.03.2026", "Gesamtbetrag 89,99 EUR"],
    );
    text_pdf(
        &directory.join("text-receipt.pdf"),
        &["Datum: 12.03.2026", "Zwischensumme 75,62", "Gesamtbetrag 89,99 EUR"],
    );
    text_pdf(
        &directory.join("text-receipt-long.pdf"),
        &["Some receipt text here that is long enough"],
    );
    text_pdf(
        &directory.join("text-receipt-rewe.pdf"),
        &[
            "REWE Markt GmbH",
            "Datum: 03.10.2026",
            "Milch 1,29",
            "Brot 2,49",
            "SUMME EUR 3,78",
        ],
    );
    text_pdf(
        &directory.join("confidential.pdf"),
        &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR", "Bankleitzahl 12345678"],
    );
    empty_pdf(&directory.join("empty.pdf"));
    multipage_pdf(&directory.join("multipage.pdf"));

    let truncated = directory.join("truncated.pdf");
    text_pdf(&truncated, &["Rechnung Nr. 4711", "Gesamtbetrag 89,99 EUR"]);
    let bytes = fs::read(&truncated).unwrap();
    fs::write(&truncated, &bytes[..bytes.len() / 2]).unwrap();

    fs::write(directory.join("malformed.pdf"), b"%PDF-1.4\nthis is not a PDF\n").unwrap();

    fs::copy(
        repo_root()
            .join("crates/finglass-core/tests/fixtures/findesk/documents")
            .join("11111111-2222-3333-4444-555555555555_receipt.pdf"),
        directory.join("findesk-receipt.pdf"),
    )
    .unwrap();
}

/// Writes a three-page PDF with different page sizes and a text line on each page.
fn multipage_pdf(path: &std::path::Path) {
    use pdf_extract::{Dictionary, Document, Object, Stream};

    fn escape_text(text: &str) -> String {
        text.replace('\\', "\\\\").replace('(', "\\(").replace(')', "\\)")
    }

    let mut document = Document::with_version("1.5");
    let pages_id = document.new_object_id();

    let mut font = Dictionary::new();
    font.set("Type", Object::Name(b"Font".to_vec()));
    font.set("Subtype", Object::Name(b"Type1".to_vec()));
    font.set("BaseFont", Object::Name(b"Helvetica".to_vec()));
    font.set("Encoding", Object::Name(b"WinAnsiEncoding".to_vec()));
    let font_id = document.add_object(font);

    let mut fonts = Dictionary::new();
    fonts.set("F1", Object::Reference(font_id));
    let mut resources = Dictionary::new();
    resources.set("Font", fonts);
    let resources_id = document.add_object(resources);

    let pages = [
        (595.0, 842.0, "Seite Eins"),
        (612.0, 792.0, "Page Two"),
        (420.0, 595.0, "Page Three"),
    ];
    let mut page_ids = Vec::new();

    for (width, height, label) in pages {
        let content = format!("BT\n/F1 12 Tf\n50 700 Td\n({}) Tj\nET\n", escape_text(label));
        let content_id = document.add_object(Stream::new(Dictionary::new(), content.into_bytes()));

        let mut page = Dictionary::new();
        page.set("Type", Object::Name(b"Page".to_vec()));
        page.set("Parent", Object::Reference(pages_id));
        page.set(
            "MediaBox",
            Object::Array(vec![0.into(), 0.into(), width.into(), height.into()]),
        );
        page.set("Contents", Object::Reference(content_id));
        page.set("Resources", Object::Reference(resources_id));
        page_ids.push(document.add_object(page));
    }

    let page_count = i64::try_from(page_ids.len()).unwrap();
    let mut pages_dictionary = Dictionary::new();
    pages_dictionary.set("Type", Object::Name(b"Pages".to_vec()));
    pages_dictionary.set(
        "Kids",
        Object::Array(page_ids.into_iter().map(Object::Reference).collect()),
    );
    pages_dictionary.set("Count", Object::Integer(page_count));
    document.objects.insert(pages_id, Object::Dictionary(pages_dictionary));

    let mut catalog = Dictionary::new();
    catalog.set("Type", Object::Name(b"Catalog".to_vec()));
    catalog.set("Pages", Object::Reference(pages_id));
    let catalog_id = document.add_object(catalog);
    document.trailer.set("Root", Object::Reference(catalog_id));

    document.save(path).expect("the multi-page test PDF can be written");
}

fn pdf_text_json(directory: &std::path::Path) -> String {
    let mut entries = Vec::new();

    for file in PDF_FIXTURES {
        let path = directory.join(file);
        let text = crate::documents::pdf_text::extract_text(&path, 5);
        let info = crate::documents::pdf_text::inspect(&path);

        let text_json = match text {
            Some(text) => format!(
                "{{\"text\":{},\"page_count\":{}}}",
                json_string(&text.text),
                text.page_count
            ),
            None => "null".to_owned(),
        };
        let info_json = match info {
            Some(info) => {
                let pages: Vec<String> = info
                    .pages
                    .iter()
                    .map(|page| format!("{{\"width_pt\":{},\"height_pt\":{}}}", page.width_pt, page.height_pt))
                    .collect();
                format!("{{\"page_count\":{},\"pages\":[{}]}}", info.page_count, pages.join(","))
            }
            None => "null".to_owned(),
        };

        entries.push(format!(
            "{{\"file\":{},\"text\":{},\"info\":{}}}",
            json_string(file),
            text_json,
            info_json
        ));
    }

    format!("[{}]", entries.join(","))
}

/// Quotes a string for JSON. Everything outside the control characters stays as UTF-8.
fn json_string(value: &str) -> String {
    let mut out = String::with_capacity(value.len() + 2);
    out.push('"');

    for ch in value.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }

    out.push('"');
    out
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

fn migrations_json() -> String {
    let entries: Vec<String> = MIGRATIONS
        .iter()
        .map(|migration| {
            format!(
                "{{\"version\":{},\"name\":{},\"sql\":{}}}",
                migration.version,
                json_string(migration.name),
                json_string(migration.sql),
            )
        })
        .collect();

    format!("[{}]", entries.join(","))
}

/// The inputs every passphrase vector uses, for both purposes. Invisible characters are escapes, never literals.
fn vector_inputs() -> Vec<&'static str> {
    vec![
        "",
        " ",
        "\t",
        "aaaaaaaaa",
        "aaaaaaaaaa",
        "\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}",
        "\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}\u{e9}",
        "e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}e\u{301}",
        " correct horse ",
        "\tcorrect horse",
        "correct horse\u{a0}",
        "\u{3000}correct horse",
        "correct horse\u{85}",
        "\u{200b}correct horse",
        "correct horse\u{180e}",
        "correct\u{2003}horse battery",
        "\u{212b}ngstr\u{f6}m-passphrase",
        "\u{fb01}nancial-passphrase",
        "\u{1100}\u{1161}\u{11a8}-hangul-pass",
        "emoji-\u{1f512}-passphrase",
        "it's a 'quoted' passphrase",
        "say \"hi\" please ok",
        "a''b\"\"c'\"d-passphrase",
        "semi;colon?query%25+plus&amp=eq#hash",
        FIXTURE_PASSPHRASE,
    ]
}

fn passphrase_vectors_json() -> String {
    let mut entries = Vec::new();

    for input in vector_inputs() {
        for purpose in [PassphrasePurpose::Unlock, PassphrasePurpose::New] {
            let purpose_name = match purpose {
                PassphrasePurpose::Unlock => "unlock",
                PassphrasePurpose::New => "new",
            };

            match Passphrase::new(input, purpose) {
                Ok(passphrase) => entries.push(format!(
                    "{{\"input_hex\":{},\"purpose\":{},\"ok\":true,\"normalized_hex\":{}}}",
                    json_string(&hex(input.as_bytes())),
                    json_string(purpose_name),
                    json_string(&hex(passphrase.expose().as_bytes())),
                )),
                Err(err) => {
                    let kind = match err {
                        PassphraseError::Empty => "empty",
                        PassphraseError::TooShort { .. } => "too_short",
                        PassphraseError::SurroundingWhitespace => "surrounding_whitespace",
                    };
                    entries.push(format!(
                        "{{\"input_hex\":{},\"purpose\":{},\"ok\":false,\"error\":{}}}",
                        json_string(&hex(input.as_bytes())),
                        json_string(purpose_name),
                        json_string(kind),
                    ));
                }
            }
        }
    }

    format!("[{}]", entries.join(","))
}

/// Reads one connection pragma as a string, the same way the Go test does.
fn pragma_value(conn: &rusqlite::Connection, name: &str) -> rusqlite::Result<String> {
    conn.query_row(&format!("PRAGMA {name}"), [], |row| {
        Ok(match row.get_ref(0)? {
            rusqlite::types::ValueRef::Null => String::new(),
            rusqlite::types::ValueRef::Integer(value) => value.to_string(),
            rusqlite::types::ValueRef::Real(value) => value.to_string(),
            rusqlite::types::ValueRef::Text(value) => String::from_utf8_lossy(value).into_owned(),
            rusqlite::types::ValueRef::Blob(value) => value.iter().map(|byte| format!("{byte:02x}")).collect(),
        })
    })
}

/// The connection settings of a live database, as a JSON object of strings.
fn connection_pragmas_json(db: &Database) -> String {
    let values = db
        .with_conn(|conn| {
            CONNECTION_PRAGMAS
                .iter()
                .map(|name| Ok((*name, pragma_value(conn, name)?)))
                .collect::<Result<Vec<_>, DbError>>()
        })
        .unwrap();

    let entries: Vec<String> = values
        .iter()
        .map(|(name, value)| format!("{}:{}", json_string(name), json_string(value)))
        .collect();

    format!("{{{}}}", entries.join(","))
}

fn fixture_passphrase(purpose: PassphrasePurpose) -> Passphrase {
    Passphrase::new(FIXTURE_PASSPHRASE, purpose).expect("the fixture passphrase is valid")
}

/// Writes the goldens and the Rust-created database. Run by hand; not part of the normal test run.
#[test]
#[ignore]
fn write_go_fixtures() {
    let golden = golden_dir();
    let sqlcipher = sqlcipher_dir();
    let pdf = pdf_dir();
    fs::create_dir_all(&golden).unwrap();
    fs::create_dir_all(&sqlcipher).unwrap();
    fs::create_dir_all(&pdf).unwrap();

    fs::write(golden.join("migrations.json"), migrations_json()).unwrap();
    fs::write(golden.join("passphrase_vectors.json"), passphrase_vectors_json()).unwrap();
    fs::write(golden.join("regex_corpus.json"), regex_corpus_json()).unwrap();
    fs::write(golden.join("keychain_accounts.json"), keychain_accounts_json()).unwrap();
    write_pdf_fixtures(&pdf);
    fs::write(golden.join("pdf_text.json"), pdf_text_json(&pdf)).unwrap();

    let path = sqlcipher.join("rust-created.db");
    if path.exists() {
        fs::remove_file(&path).unwrap();
    }

    let passphrase = fixture_passphrase(PassphrasePurpose::New);
    let db = Database::create(&path, &passphrase).unwrap();
    db.with_conn(|conn| {
        conn.execute(
            "INSERT INTO settings (key, value) VALUES (?1, ?2)",
            ["go.fixture", "from-rust"],
        )?;
        Ok(())
    })
    .unwrap();

    fs::write(golden.join("connection_pragmas.json"), connection_pragmas_json(&db)).unwrap();
}

/// Rust must open the database Go created with the same hostile passphrase.
#[test]
fn opens_the_go_created_fixture() {
    let source = sqlcipher_dir().join("go-created.db");
    assert!(
        source.exists(),
        "{} is missing; create it with `go test ./internal/core/db -run TestWriteGoCreatedFixture -update-go-fixture`",
        source.display()
    );

    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("go-created.db");
    fs::copy(&source, &path).unwrap();

    let passphrase = fixture_passphrase(PassphrasePurpose::Unlock);
    let db = Database::open(&path, &passphrase).unwrap();

    let (version, value): (u32, String) = db
        .with_conn(|conn| {
            let version = conn.query_row("PRAGMA user_version", [], |row| row.get(0))?;
            let value = conn.query_row("SELECT value FROM settings WHERE key = 'go.fixture'", [], |row| {
                row.get(0)
            })?;
            Ok((version, value))
        })
        .unwrap();

    assert_eq!(version, 3);
    assert_eq!(value, "from-go");
    drop(db);

    let wrong = Passphrase::new("wrong passphrase", PassphrasePurpose::Unlock).unwrap();
    let err = match Database::open(&path, &wrong) {
        Ok(_) => panic!("a wrong passphrase opened the database"),
        Err(err) => err,
    };
    assert!(matches!(err, DbError::WrongPassphraseOrDamaged));
}

/// The committed goldens must not go stale.
#[test]
fn the_committed_goldens_match_this_build() {
    let path = golden_dir().join("migrations.json");
    let committed = fs::read_to_string(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display()));
    assert_eq!(
        committed,
        migrations_json(),
        "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
        path.display()
    );

    let temp = tempfile::tempdir().unwrap();
    let db = Database::create(
        &temp.path().join("pragmas.db"),
        &fixture_passphrase(PassphrasePurpose::New),
    )
    .unwrap();

    let path = golden_dir().join("connection_pragmas.json");
    let committed = fs::read_to_string(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display()));
    assert_eq!(
        committed,
        connection_pragmas_json(&db),
        "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
        path.display()
    );

    let path = golden_dir().join("regex_corpus.json");
    let committed = fs::read_to_string(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display()));
    assert_eq!(
        committed,
        regex_corpus_json(),
        "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
        path.display()
    );

    let path = golden_dir().join("keychain_accounts.json");
    let committed = fs::read_to_string(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display()));
    assert_eq!(
        committed,
        keychain_accounts_json(),
        "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
        path.display()
    );

    let temp = tempfile::tempdir().unwrap();
    let pdf = temp.path().join("pdf");
    write_pdf_fixtures(&pdf);

    let path = golden_dir().join("pdf_text.json");
    let committed = fs::read_to_string(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display()));
    assert_eq!(
        committed,
        pdf_text_json(&pdf),
        "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
        path.display()
    );

    for file in PDF_FIXTURES {
        let committed = pdf_dir().join(file);
        let generated = pdf.join(file);
        assert_eq!(
            fs::read(&committed).unwrap_or_else(|err| panic!("read {}: {err}", committed.display())),
            fs::read(&generated).unwrap_or_else(|err| panic!("read {}: {err}", generated.display())),
            "{} is stale; run `cargo test -p finglass-core write_go_fixtures -- --ignored`",
            committed.display()
        );
    }
}
