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
    fs::create_dir_all(&golden).unwrap();
    fs::create_dir_all(&sqlcipher).unwrap();

    fs::write(golden.join("migrations.json"), migrations_json()).unwrap();
    fs::write(golden.join("passphrase_vectors.json"), passphrase_vectors_json()).unwrap();

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
}
