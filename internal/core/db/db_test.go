package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// fixturePassphrase is the test-only passphrase of the cross-language fixtures.
// It contains embedded single and double quotes, a decomposed `é`, SQL-looking
// punctuation and non-ASCII characters. It is a dummy and protects nothing.
//
// #nosec G101 -- a committed fixture passphrase, deliberately public.
const fixturePassphrase = "T\u00ebst 'q' \"dq\" e\u0301 \u2014 ;?%+&=# \u00fcn\u00efcode"

// updateGoFixture regenerates the committed Go-created database.
var updateGoFixture = flag.Bool("update-go-fixture", false, "regenerate testdata/sqlcipher/go-created.db")

func newPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()

	p, err := secret.NewPassphrase([]byte(value), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase(%q): %v", value, err)
	}
	return p
}

func unlockPassphrase(t *testing.T, value string) *secret.Passphrase {
	t.Helper()

	p, err := secret.NewPassphrase([]byte(value), secret.Unlock)
	if err != nil {
		t.Fatalf("NewPassphrase(%q): %v", value, err)
	}
	return p
}

func fixtureNew(t *testing.T) *secret.Passphrase {
	t.Helper()
	return newPassphrase(t, fixturePassphrase)
}

func fixtureUnlock(t *testing.T) *secret.Passphrase {
	t.Helper()
	return unlockPassphrase(t, fixturePassphrase)
}

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func tableExists(t *testing.T, d *Database, name string) bool {
	t.Helper()

	var count int
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(
			ctx,
			"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			name,
		).Scan(&count)
	})
	if err != nil {
		t.Fatalf("table %q: %v", name, err)
	}
	return count > 0
}

func userVersion(t *testing.T, d *Database) int {
	t.Helper()

	var version int
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	})
	if err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

func settingValue(t *testing.T, d *Database, key string) string {
	t.Helper()

	var value string
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	})
	if err != nil {
		t.Fatalf("read setting %q: %v", key, err)
	}
	return value
}

func TestCreateThenOpenWithTheSamePassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	pass := newPassphrase(t, "correct horse battery staple")

	d, err := Create(path, pass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing")
	}
	if !tableExists(t, d, "audit_log") {
		t.Fatal("audit_log table is missing")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d, err = Open(path, pass)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing after reopening")
	}
}

func TestTheFileIsEncryptedOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	d, err := Create(path, newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	bytes, err := os.ReadFile(path) // #nosec G304 -- path is the test's temporary database
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if len(bytes) < len(sqliteHeader) {
		t.Fatalf("file has %d bytes, want at least %d", len(bytes), len(sqliteHeader))
	}
	if string(bytes[:len(sqliteHeader)]) == string(sqliteHeader) {
		t.Fatal("file starts with the plain SQLite header")
	}
}

func TestCreateRefusesAnExistingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	pass := newPassphrase(t, "correct horse battery staple")

	d, err := Create(path, pass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = d.Close()

	_, err = Create(path, pass)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Create = %v, want ErrAlreadyExists", err)
	}
}

func TestAWrongPassphraseIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	d, err := Create(path, newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = d.Close()

	_, err = Open(path, unlockPassphrase(t, "wrong passphrase"))
	if !errors.Is(err, ErrWrongPassphraseOrDamaged) {
		t.Fatalf("Open = %v, want ErrWrongPassphraseOrDamaged", err)
	}
}

func TestAPlainSQLiteFileIsNotEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")

	sqlDB, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := sqlDB.Exec("CREATE TABLE plain (id INTEGER);"); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("create plain table: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close plain database: %v", err)
	}

	_, err = Open(path, unlockPassphrase(t, "whatever"))
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("Open = %v, want ErrNotEncrypted", err)
	}
}

func TestAMissingFileIsNotFound(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing.db"), unlockPassphrase(t, "whatever"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open = %v, want ErrNotFound", err)
	}
}

func TestChangePassphraseRekeysTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	oldPass := newPassphrase(t, "correct horse battery staple")
	newPass := newPassphrase(t, "another very long passphrase")

	d, err := Create(path, oldPass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := d.ChangePassphrase(newPass); err != nil {
		t.Fatalf("ChangePassphrase: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = Open(path, oldPass)
	if !errors.Is(err, ErrWrongPassphraseOrDamaged) {
		t.Fatalf("Open with the old passphrase = %v, want ErrWrongPassphraseOrDamaged", err)
	}

	d, err = Open(path, newPass)
	if err != nil {
		t.Fatalf("Open with the new passphrase: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing after rekey")
	}
}

func TestErrorsNeverContainThePassphrase(t *testing.T) {
	const marker = "zz-MARKER-91f3"
	dir := t.TempDir()
	path := filepath.Join(dir, "finglass.db")
	pass := newPassphrase(t, marker+" passphrase")

	d, err := Create(path, pass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	errorsToCheck := []error{
		func() error {
			_, err := Open(path, unlockPassphrase(t, marker+` "wrong"; passphrase`))
			return err
		}(),
		func() error {
			_, err := Create(path, pass)
			return err
		}(),
		func() error {
			return d.ChangePassphrase(pass)
		}(),
	}

	for _, err := range errorsToCheck {
		if err == nil {
			t.Fatal("expected an error")
		}
		for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(text, marker) {
				t.Fatalf("error leaked the passphrase: %q", text)
			}
		}
	}
}

func TestTheDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	d, err := Create(path, newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = d.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %04o, want 0600", mode)
	}
}

func TestAFailedCreateLeavesNothingBehind(t *testing.T) {
	okV1 := Migration{Version: 1, Name: "ok", SQL: "CREATE TABLE first (id INTEGER);"}
	failV2 := Migration{Version: 2, Name: "failing", SQL: "CREATE TABLE second (id INTEGER); THIS IS NOT VALID SQL;"}

	dir := t.TempDir()
	path := filepath.Join(dir, "finglass.db")

	_, err := createWithMigrations(path, newPassphrase(t, "correct horse battery staple"), []Migration{okV1, failV2})
	var migrationErr *MigrationError
	if !errors.As(err, &migrationErr) || migrationErr.Version != 2 {
		t.Fatalf("createWithMigrations = %v, want MigrationError for version 2", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database file exists after a failed create: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir has %d entries after a failed create, want 0", len(entries))
	}
}

type schemaRow struct {
	Type string
	Name string
	SQL  string
}

func schemaRows(t *testing.T, d *Database) []schemaRow {
	t.Helper()

	var rows []schemaRow
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		result, err := conn.QueryContext(
			ctx,
			"SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name",
		)
		if err != nil {
			return err
		}
		defer func() {
			_ = result.Close()
		}()

		for result.Next() {
			var row schemaRow
			if err := result.Scan(&row.Type, &row.Name, &row.SQL); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return result.Err()
	})
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	return rows
}

func copyFile(t *testing.T, source, target string) {
	t.Helper()

	data, err := os.ReadFile(source) // #nosec G304 -- fixture path from the repository root
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil { // #nosec G703 -- target is a test temporary path
		t.Fatalf("write %s: %v", target, err)
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "sqlcipher", name)
}

func TestOpensTheRustCreatedFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rust-created.db")
	copyFile(t, fixturePath(t, "rust-created.db"), path)

	d, err := Open(path, fixtureUnlock(t))
	if err != nil {
		t.Fatalf("Open(rust-created.db): %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	if version := userVersion(t, d); version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
	if value := settingValue(t, d, "go.fixture"); value != "from-rust" {
		t.Fatalf("settings go.fixture = %q, want %q", value, "from-rust")
	}

	freshPath := filepath.Join(dir, "go-fresh.db")
	fresh, err := Create(freshPath, fixtureNew(t))
	if err != nil {
		t.Fatalf("Create(fresh): %v", err)
	}
	defer func() {
		_ = fresh.Close()
	}()

	if got, want := schemaRows(t, d), schemaRows(t, fresh); !reflect.DeepEqual(got, want) {
		t.Fatalf("sqlite_master differs between the Rust fixture and a fresh Go database:\nrust: %+v\ngo:   %+v", want, got)
	}
}

func TestMigrationsMatchTheRustGolden(t *testing.T) {
	path := filepath.Join(repoRoot(t), "testdata", "golden", "migrations.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var golden []Migration
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if !reflect.DeepEqual(golden, Migrations) {
		t.Fatalf("Migrations differ from %s", path)
	}
}

func TestWriteGoCreatedFixture(t *testing.T) {
	if !*updateGoFixture {
		t.Skip("run with -update-go-fixture to regenerate testdata/sqlcipher/go-created.db")
	}

	path := fixturePath(t, "go-created.db")
	cleanupTemp(path)

	d, err := Create(path, fixtureNew(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?)",
			"go.fixture",
			"from-go",
		)
		return err
	})
	if err != nil {
		_ = d.Close()
		t.Fatalf("insert go.fixture: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTheGoCreatedFixtureOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-created.db")
	copyFile(t, fixturePath(t, "go-created.db"), path)

	d, err := Open(path, fixtureUnlock(t))
	if err != nil {
		t.Fatalf("Open(go-created.db): %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	if version := userVersion(t, d); version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
	if value := settingValue(t, d, "go.fixture"); value != "from-go" {
		t.Fatalf("settings go.fixture = %q, want %q", value, "from-go")
	}
}

func TestTheCipherIsPinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	d, err := Create(path, newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	version, provider, err := d.CipherInfo()
	if err != nil {
		t.Fatalf("CipherInfo: %v", err)
	}
	if !strings.HasPrefix(version, "4.15.0") {
		t.Fatalf("cipher version = %q, want it to start with 4.15.0", version)
	}
	if provider != "libtomcrypt" {
		t.Fatalf("cipher provider = %q, want libtomcrypt", provider)
	}
}

func TestNoSilentReconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	pass := newPassphrase(t, "correct horse battery staple")

	d, err := Create(path, pass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	withConnErr := d.WithConn(context.Background(), func(context.Context, *sql.Conn) error {
		return nil
	})
	if !errors.Is(withConnErr, ErrUnavailable) {
		t.Fatalf("WithConn after Close = %v, want ErrUnavailable", withConnErr)
	}

	if err := d.VacuumInto(filepath.Join(t.TempDir(), "copy.db")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("VacuumInto after Close = %v, want ErrUnavailable", err)
	}

	if err := d.ChangePassphrase(pass); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ChangePassphrase after Close = %v, want ErrUnavailable", err)
	}
}

func TestForeignKeysAreOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finglass.db")
	d, err := Create(path, newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	var enabled int
	err = d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled)
	})
	if err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("foreign_keys = %d, want 1", enabled)
	}
}

func TestPathWithSpacesAndQuote(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	path := filepath.Join(dir, "my 'quoted' data.db")
	pass := newPassphrase(t, "correct horse battery staple")

	d, err := Create(path, pass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d, err = Open(path, pass)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing after reopening")
	}
}

func TestPathWithQuestionMarkIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "what?.db")
	pass := newPassphrase(t, "correct horse battery staple")

	if _, err := Create(path, pass); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Create = %v, want ErrInvalidPath", err)
	}
	if _, err := Open(path, pass); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Open = %v, want ErrInvalidPath", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused path left a file behind: %v", err)
	}
}

// pragmaQueries pins the pragma names the golden may contain, so the test never
// builds SQL from arbitrary input.
var pragmaQueries = map[string]string{
	"synchronous":                  "PRAGMA synchronous",
	"journal_mode":                 "PRAGMA journal_mode",
	"locking_mode":                 "PRAGMA locking_mode",
	"foreign_keys":                 "PRAGMA foreign_keys",
	"busy_timeout":                 "PRAGMA busy_timeout",
	"cipher_page_size":             "PRAGMA cipher_page_size",
	"kdf_iter":                     "PRAGMA kdf_iter",
	"cipher_hmac_algorithm":        "PRAGMA cipher_hmac_algorithm",
	"cipher_kdf_algorithm":         "PRAGMA cipher_kdf_algorithm",
	"cipher_plaintext_header_size": "PRAGMA cipher_plaintext_header_size",
}

func pragmaValue(t *testing.T, d *Database, name string) string {
	t.Helper()

	query, ok := pragmaQueries[name]
	if !ok {
		t.Fatalf("no query for pragma %q", name)
	}

	var value string
	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return conn.QueryRowContext(ctx, query).Scan(&value)
	})
	if err != nil {
		t.Fatalf("read PRAGMA %s: %v", name, err)
	}
	return value
}

func TestConnectionPragmasMatchRust(t *testing.T) {
	path := filepath.Join(repoRoot(t), "testdata", "golden", "connection_pragmas.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed path below the repository root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var golden map[string]string
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(golden) != len(pragmaQueries) {
		t.Fatalf("%s has %d pragmas, want %d", path, len(golden), len(pragmaQueries))
	}

	d, err := Create(filepath.Join(t.TempDir(), "pragmas.db"), newPassphrase(t, "correct horse battery staple"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	for name, want := range golden {
		if got := pragmaValue(t, d, name); got != want {
			t.Errorf("PRAGMA %s = %q, want %q (Rust)", name, got, want)
		}
	}
}

// hostilePassphrases exercise the driver's `_pragma_key` escaping: single and
// double quotes, doubled quotes, a backslash-free mix and SQL-looking
// punctuation.
var hostilePassphrases = []string{
	"it's 'single'",
	`say "double" now`,
	`two''single""double`,
	`mixed'"quote-pass`,
	"semi;q?p%+&=#-pass",
}

func TestRekeyWithQuotesThenReopen(t *testing.T) {
	const oldValue = "correct horse battery staple"

	for _, next := range hostilePassphrases {
		t.Run(next, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "finglass.db")

			d, err := Create(path, newPassphrase(t, oldValue))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := d.ChangePassphrase(newPassphrase(t, next)); err != nil {
				_ = d.Close()
				t.Fatalf("ChangePassphrase: %v", err)
			}
			if err := d.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			reopened, err := Open(path, unlockPassphrase(t, next))
			if err != nil {
				t.Fatalf("Open with the new passphrase: %v", err)
			}
			if !tableExists(t, reopened, "settings") {
				_ = reopened.Close()
				t.Fatal("settings table is missing after rekey")
			}
			if err := reopened.Close(); err != nil {
				t.Fatalf("Close after reopening: %v", err)
			}

			_, err = Open(path, unlockPassphrase(t, oldValue))
			if !errors.Is(err, ErrWrongPassphraseOrDamaged) {
				t.Fatalf("Open with the old passphrase = %v, want ErrWrongPassphraseOrDamaged", err)
			}
		})
	}
}

func TestOpenWithEveryEscapingCase(t *testing.T) {
	for _, pass := range hostilePassphrases {
		t.Run(pass, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "finglass.db")

			d, err := Create(path, newPassphrase(t, pass))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := d.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			d, err = Open(path, unlockPassphrase(t, pass))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() {
				_ = d.Close()
			}()
			if !tableExists(t, d, "settings") {
				t.Fatal("settings table is missing after reopening")
			}
		})
	}
}

func TestNulInPassphraseIsRefused(t *testing.T) {
	const marker = "zz-MARKER-91f3"

	p, err := secret.NewPassphrase([]byte(marker+"\x00passphrase"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}

	dir := t.TempDir()
	_, createErr := Create(filepath.Join(dir, "create.db"), p)
	_, openErr := Open(filepath.Join(dir, "open.db"), p)

	for name, err := range map[string]error{"Create": createErr, "Open": openErr} {
		if !errors.Is(err, ErrInvalidPassphrase) {
			t.Fatalf("%s = %v, want ErrInvalidPassphrase", name, err)
		}
		for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(text, marker) {
				t.Fatalf("%s error leaked the passphrase: %q", name, text)
			}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir has %d entries after a refused passphrase, want 0", len(entries))
	}
}

func TestConnectorIsOneShot(t *testing.T) {
	c := &oneShotConnector{dsn: filepath.Join(t.TempDir(), "one-shot.db")}

	first, err := c.Connect(context.Background())
	if err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if c.dsn != "" {
		t.Fatalf("connector still holds the DSN %q after the first Connect", c.dsn)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	second, err := c.Connect(context.Background())
	if err == nil {
		_ = second.Close()
		t.Fatal("second Connect succeeded, want an error")
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Fatal("second Connect returned driver.ErrBadConn, which would make database/sql retry")
	}
}

func TestWrongPassphraseRetryWorks(t *testing.T) {
	const right = "correct horse battery staple"
	path := filepath.Join(t.TempDir(), "finglass.db")

	d, err := Create(path, newPassphrase(t, right))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = Open(path, unlockPassphrase(t, "wrong passphrase"))
	if !errors.Is(err, ErrWrongPassphraseOrDamaged) {
		t.Fatalf("Open with a wrong passphrase = %v, want ErrWrongPassphraseOrDamaged", err)
	}

	d, err = Open(path, unlockPassphrase(t, right))
	if err != nil {
		t.Fatalf("Open after a wrong attempt: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()
	if !tableExists(t, d, "settings") {
		t.Fatal("settings table is missing after a retry")
	}
}
