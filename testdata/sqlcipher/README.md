# Cross-language SQLCipher fixtures

These two databases are committed on purpose. They prove that the Rust build
and the Go build open the same encrypted file with the same passphrase.

| File | Created by | Row in `settings` |
|---|---|---|
| `rust-created.db` | `finglass-core` (rusqlite + bundled SQLCipher) | `go.fixture` = `from-rust` |
| `go-created.db` | `internal/core/db` (go-sqlcipher) | `go.fixture` = `from-go` |

## Passphrase

The fixture passphrase is a **dummy for fixtures only** and protects nothing.
As a Rust string literal it is:

```rust
"T\u{eb}st 'q' \"dq\" e\u{301} \u{2014} ;?%+&=# \u{fc}n\u{ef}code"
```

The value contains embedded single and double quotes, a decomposed `é` (`e`
followed by U+0301), SQL-looking punctuation (`;?%+&=#`) and non-ASCII
characters. Both language builds keep the same golden connection settings in
`testdata/golden/connection_pragmas.json`.

## Regenerating

1. Rust goldens and `rust-created.db`:

   ```sh
   cargo test -p finglass-core write_go_fixtures -- --ignored
   ```

2. `go-created.db`:

   ```sh
   go test ./internal/core/db -run TestWriteGoCreatedFixture -update-go-fixture
   ```

Never point either generator at a real data folder. It only writes into
`testdata/`.
