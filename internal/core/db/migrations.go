// Versioned schema migrations.
//
// The version lives in `PRAGMA user_version`. Versions start at 1, rise by one
// and are **never edited after release**.
package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration is one schema step. Released steps must never be edited.
type Migration struct {
	// Version is the schema version this step produces.
	Version int
	// Name is a short human-readable label.
	Name string
	// SQL is the step's SQL. It may contain several statements.
	SQL string
}

// Migrations are the schema steps this build knows about. Released steps must
// never be edited: a released migration is history, so fixes go into a new one.
//
// This list is copied verbatim from
// `crates/finglass-core/src/db/migrations.rs`; TestMigrationsMatchTheRustGolden
// checks it against testdata/golden/migrations.json.
var Migrations = []Migration{
	{
		Version: 1,
		Name:    "initial",
		SQL: `CREATE TABLE settings (
        key   TEXT PRIMARY KEY NOT NULL,
        value TEXT NOT NULL
    ) STRICT;
    CREATE TABLE audit_log (
        id      INTEGER PRIMARY KEY,
        at      TEXT NOT NULL,
        action  TEXT NOT NULL,
        actor   TEXT NOT NULL,
        details TEXT NOT NULL
    ) STRICT;
    CREATE INDEX audit_log_at ON audit_log (at);`,
	},
	{
		Version: 2,
		Name:    "domain",
		SQL: `CREATE TABLE account_groups (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE bank_connections (
    id                  INTEGER PRIMARY KEY,
    bank_name           TEXT NOT NULL,
    blz                 TEXT NOT NULL,
    bic                 TEXT,
    user_id             TEXT NOT NULL,
    server_url          TEXT,
    tan_method          TEXT,
    sync_from           TEXT,
    last_sync           TEXT,
    capabilities        TEXT,
    include_in_sync_all INTEGER NOT NULL DEFAULT 1 CHECK (include_in_sync_all IN (0, 1))
) STRICT;

CREATE TABLE accounts (
    id                 INTEGER PRIMARY KEY,
    bank_connection_id INTEGER REFERENCES bank_connections (id) ON DELETE CASCADE,
    institution        TEXT,
    name               TEXT,
    iban               TEXT,
    account_number     TEXT,
    currency           TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
    account_type       TEXT NOT NULL DEFAULT 'checking'
                       CHECK (account_type IN ('checking', 'savings', 'credit_card', 'loan', 'depot')),
    balance_minor      INTEGER NOT NULL DEFAULT 0,
    balance_updated_at TEXT,
    group_id           INTEGER REFERENCES account_groups (id) ON DELETE SET NULL
) STRICT;
CREATE INDEX accounts_iban ON accounts (iban);

CREATE TABLE categories (
    id                 INTEGER PRIMARY KEY,
    name               TEXT NOT NULL,
    color              TEXT NOT NULL DEFAULT '#3BC7B0',
    budget_limit_minor INTEGER,
    is_tax_relevant    INTEGER NOT NULL DEFAULT 0 CHECK (is_tax_relevant IN (0, 1)),
    is_transfer        INTEGER NOT NULL DEFAULT 0 CHECK (is_transfer IN (0, 1))
) STRICT;

CREATE TABLE rules (
    id           INTEGER PRIMARY KEY,
    pattern      TEXT NOT NULL,
    category_id  INTEGER NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    priority     INTEGER NOT NULL DEFAULT 0,
    auto_apply   INTEGER NOT NULL DEFAULT 1 CHECK (auto_apply IN (0, 1)),
    match_field  TEXT NOT NULL DEFAULT 'any' CHECK (match_field IN ('any', 'partner', 'purpose')),
    direction    TEXT NOT NULL DEFAULT 'any' CHECK (direction IN ('any', 'expense', 'income')),
    tax_relevant INTEGER CHECK (tax_relevant IN (0, 1))
) STRICT;

CREATE TABLE transactions (
    id                  INTEGER PRIMARY KEY,
    account_id          INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    booking_date        TEXT NOT NULL,
    amount_minor        INTEGER NOT NULL,
    currency            TEXT NOT NULL CHECK (length(currency) = 3),
    balance_after_minor INTEGER,
    partner_name        TEXT,
    purpose             TEXT,
    category_id         INTEGER REFERENCES categories (id) ON DELETE SET NULL,
    import_hash         TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    is_new              INTEGER NOT NULL DEFAULT 0 CHECK (is_new IN (0, 1)),
    tax_override        INTEGER CHECK (tax_override IN (0, 1))
) STRICT;
CREATE INDEX transactions_account_date ON transactions (account_id, booking_date);
CREATE INDEX transactions_account_hash ON transactions (account_id, import_hash);
CREATE INDEX transactions_category ON transactions (category_id);

CREATE TABLE deleted_bookings (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    import_hash TEXT NOT NULL,
    deleted_at  TEXT NOT NULL
) STRICT;
CREATE INDEX deleted_bookings_account_hash ON deleted_bookings (account_id, import_hash);

CREATE TABLE pending_bookings (
    id           INTEGER PRIMARY KEY,
    account_id   INTEGER NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    booking_date TEXT NOT NULL,
    value_date   TEXT,
    amount_minor INTEGER NOT NULL,
    currency     TEXT NOT NULL CHECK (length(currency) = 3),
    partner_name TEXT,
    purpose      TEXT,
    fetched_at   TEXT NOT NULL
) STRICT;

CREATE TABLE documents (
    id             INTEGER PRIMARY KEY,
    file_name      TEXT NOT NULL,
    stored_path    TEXT NOT NULL,
    mime_type      TEXT NOT NULL,
    uploaded_at    TEXT NOT NULL,
    transaction_id INTEGER REFERENCES transactions (id) ON DELETE SET NULL
) STRICT;
CREATE INDEX documents_transaction ON documents (transaction_id);

CREATE TABLE ocr_texts (
    id          INTEGER PRIMARY KEY,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT 'de',
    confidence  REAL
) STRICT;`,
	},
	{
		Version: 3,
		Name:    "offline_bank_connections",
		SQL: `ALTER TABLE bank_connections
    ADD COLUMN offline INTEGER NOT NULL DEFAULT 0 CHECK (offline IN (0, 1));`,
	},
}

// supportedVersion is the newest version in migrations.
func supportedVersion(migrations []Migration) int {
	supported := 0
	for _, migration := range migrations {
		if migration.Version > supported {
			supported = migration.Version
		}
	}
	return supported
}

// migrate applies every migration newer than the database's user_version.
//
// Each step and its version bump commit together. A failing step is rolled back
// on its own; earlier steps stay committed.
func migrate(ctx context.Context, conn *sql.Conn, migrations []Migration) error {
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	supported := supportedVersion(migrations)
	if current > supported {
		return &TooNewError{Found: current, Supported: supported}
	}

	for _, migration := range migrations {
		if migration.Version <= current {
			continue
		}
		if err := applyMigration(ctx, conn, migration); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration runs one step in its own transaction and bumps user_version
// with it.
func applyMigration(ctx context.Context, conn *sql.Conn, migration Migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return &MigrationError{Version: migration.Version, Err: err}
	}

	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		_ = tx.Rollback()
		return &MigrationError{Version: migration.Version, Err: err}
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", migration.Version)); err != nil {
		_ = tx.Rollback()
		return &MigrationError{Version: migration.Version, Err: err}
	}

	if err := tx.Commit(); err != nil {
		return &MigrationError{Version: migration.Version, Err: err}
	}

	return nil
}
