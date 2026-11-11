// Package db is the encrypted Finglass database.
//
// Finglass deliberately keeps **one connection** per open database, guarded by
// a mutex. The passphrase is dropped right after opening, so it does not stay
// in memory for the whole session.
//
// The key goes through the SQLCipher driver's `_pragma_key` DSN parameter, so
// the driver sends `PRAGMA key` as the first statement on the connection,
// before its own `PRAGMA locking_mode` and `PRAGMA synchronous`. The DSN is
// built by a one-shot [driver.Connector]: it is kept only until the first
// connection is opened and then forgotten, so nothing can key a reconnect. The
// value is escaped for the driver (each `"` doubled, then URL-encoded), which
// makes SQLite read back exactly the passphrase bytes Rust's `'…'` literal
// sends.
//
// Known limits (accepted, see docs/PLAN.md, "Secrets: best effort"): the
// passphrase exists as a Go string in the DSN, in the driver's parsed
// parameters and in its formatted `PRAGMA key` query. The driver also frees its
// C copy without zeroing it. None of these can be wiped. The byte buffers this
// package builds are zeroed after the Go string conversion, which is best
// effort only. The statement text is never wrapped into an error, logged or
// traced, and no SQL logging hook may be installed on this connection.
//
// No connector, connect hook or closure keeps the passphrase after the first
// connection is opened. After [Open] or [Create] returns, nothing in
// [Database] references it.
package db

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sync"

	sqlite3 "github.com/WissCore/go-sqlcipher/v4"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// driverName is the driver registered by the SQLCipher module. It is still used
// by tests that open an unkeyed database directly.
const driverName = "sqlite3"

// synchronousMode is the SQLite synchronous level the connection needs to match
// the Rust build. SQLite's default is FULL (2), the driver's default is NORMAL
// (1), and the driver applies the pragma during Open, before the caller gets
// the connection. It therefore has to travel in the DSN.
const synchronousMode = "FULL"

// Database is an open encrypted database with one held connection.
type Database struct {
	mu     sync.Mutex
	sqlDB  *sql.DB
	conn   *sql.Conn
	closed bool
}

// Create makes a new encrypted database at path.
//
// The database is built in a temporary file next to path, migrated and then
// renamed into place, so a failure leaves nothing behind.
func Create(path string, p *secret.Passphrase) (*Database, error) {
	return createWithMigrations(path, p, Migrations)
}

// Open opens an existing encrypted database at path.
func Open(path string, p *secret.Passphrase) (*Database, error) {
	return openWithMigrations(path, p, Migrations)
}

// createWithMigrations is [Create] with an explicit migration set, for tests.
func createWithMigrations(path string, p *secret.Passphrase, migrations []Migration) (*Database, error) {
	if err := checkPassphrase(p); err != nil {
		return nil, err
	}
	if err := checkPath(path); err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("database I/O error: %w", err)
	}

	temp := tempDatabasePath(path)
	if err := buildTempDatabase(temp, path, p, migrations); err != nil {
		cleanupTemp(temp)
		return nil, err
	}

	return openWithMigrations(path, p, migrations)
}

// buildTempDatabase creates, keys, migrates and closes the temporary file,
// then restricts its mode and renames it into place.
func buildTempDatabase(temp, target string, p *secret.Passphrase, migrations []Migration) error {
	sqlDB, conn, err := openConnection(temp, p)
	if err != nil {
		return err
	}

	d := &Database{sqlDB: sqlDB, conn: conn}
	if err := setupConnection(context.Background(), conn, migrations); err != nil {
		_ = d.closeLocked()
		return err
	}
	if err := d.closeLocked(); err != nil {
		return err
	}

	// Owner-only before it gets its final name, not just after the first open.
	if err := restrictFilePermissions(temp); err != nil {
		return fmt.Errorf("database I/O error: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("database I/O error: %w", err)
	}
	return nil
}

// openWithMigrations is [Open] with an explicit migration set, for tests.
func openWithMigrations(path string, p *secret.Passphrase, migrations []Migration) (*Database, error) {
	if err := checkPassphrase(p); err != nil {
		return nil, err
	}
	if err := checkPath(path); err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("database I/O error: %w", err)
	}

	plain, err := hasSQLiteHeader(path)
	if err != nil {
		return nil, fmt.Errorf("database I/O error: %w", err)
	}
	if plain {
		return nil, ErrNotEncrypted
	}

	sqlDB, conn, err := openConnection(path, p)
	if err != nil {
		return nil, err
	}

	d := &Database{sqlDB: sqlDB, conn: conn}
	if err := setupConnection(context.Background(), conn, migrations); err != nil {
		_ = d.closeLocked()
		return nil, err
	}
	if err := restrictFilePermissions(path); err != nil {
		_ = d.closeLocked()
		return nil, fmt.Errorf("database I/O error: %w", err)
	}

	return d, nil
}

// checkPassphrase refuses a passphrase that cannot travel through the driver's
// DSN parameter. A NUL byte would truncate the C string the driver builds.
func checkPassphrase(p *secret.Passphrase) error {
	return p.Use(func(b []byte) error {
		if bytes.IndexByte(b, 0) >= 0 {
			return ErrInvalidPassphrase
		}
		return nil
	})
}

// buildDSN builds `<path>?_pragma_key=<escaped>&_synchronous=FULL`.
//
// escaped is the passphrase with every `"` doubled and then URL-encoded. SQLite
// reads the double-quoted token back to exactly the original bytes, the same
// bytes Rust's single-quoted literal sends. The DSN buffer is zeroed after the
// Go string conversion (best effort).
func buildDSN(path string, p *secret.Passphrase) (string, error) {
	var dsn string

	err := p.Use(func(b []byte) error {
		if bytes.IndexByte(b, 0) >= 0 {
			return ErrInvalidPassphrase
		}

		doubled := make([]byte, 0, len(b)+8)
		for _, c := range b {
			doubled = append(doubled, c)
			if c == '"' {
				doubled = append(doubled, '"')
			}
		}
		escaped := url.QueryEscape(string(doubled))
		wipe(doubled)

		buf := make([]byte, 0, len(path)+len(escaped)+len(synchronousMode)+32)
		buf = append(buf, path...)
		buf = append(buf, "?_pragma_key="...)
		buf = append(buf, escaped...)
		buf = append(buf, "&_synchronous="...)
		buf = append(buf, synchronousMode...)

		dsn = string(buf)
		wipe(buf)
		return nil
	})
	if err != nil {
		return "", err
	}

	return dsn, nil
}

// oneShotConnector hands out exactly one connection and forgets its DSN.
//
// The key lives only in the DSN, which the driver needs before it applies its
// own pragmas. After the first Connect, the DSN is cleared. A second Connect
// returns [ErrUnavailable] rather than driver.ErrBadConn, so database/sql does
// not retry and no reconnect can happen without the key.
type oneShotConnector struct {
	mu   sync.Mutex
	dsn  string
	used bool
}

// Connect implements driver.Connector.
func (c *oneShotConnector) Connect(context.Context) (driver.Conn, error) {
	c.mu.Lock()
	dsn := c.dsn
	used := c.used
	if !used {
		c.dsn = ""
		c.used = true
	}
	c.mu.Unlock()

	if used {
		return nil, ErrUnavailable
	}

	conn, err := (&sqlite3.SQLiteDriver{}).Open(dsn)
	if err != nil {
		// Never return the driver's error: its message can echo the SQL that
		// carried the key.
		return nil, keyedStatementError(err)
	}
	return conn, nil
}

// Driver implements driver.Connector.
func (c *oneShotConnector) Driver() driver.Driver {
	return &sqlite3.SQLiteDriver{}
}

// openConnection opens the SQLite file through a fresh one-shot connector and
// pins the pool to one connection, which is taken and kept.
func openConnection(path string, p *secret.Passphrase) (*sql.DB, *sql.Conn, error) {
	dsn, err := buildDSN(path, p)
	if err != nil {
		return nil, nil, err
	}

	sqlDB := sql.OpenDB(&oneShotConnector{dsn: dsn})

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		_ = sqlDB.Close()
		// Connect already maps the driver's error.
		return nil, nil, err
	}

	return sqlDB, conn, nil
}

// setupConnection forces decryption, turns foreign keys on and applies pending
// migrations. The key is already set by the driver's DSN parameter.
func setupConnection(ctx context.Context, conn *sql.Conn, migrations []Migration) error {
	// Reading the schema forces SQLCipher to decrypt page 1, which is how a
	// wrong key surfaces. With the key in the DSN the driver can already fail
	// during Open, but the check stays as a second gate.
	var count int64
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&count); err != nil {
		if isNotADB(err) {
			return ErrWrongPassphraseOrDamaged
		}
		return fmt.Errorf("database error: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	return migrate(ctx, conn, migrations)
}

// ChangePassphrase rekeys the database. The caller has already verified the
// current passphrase by opening the database.
func (d *Database) ChangePassphrase(p *secret.Passphrase) error {
	if err := checkPassphrase(p); err != nil {
		return err
	}

	return d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		return rekey(ctx, conn, p)
	})
}

// rekey runs `PRAGMA rekey` with the passphrase as a SQL string literal,
// doubling every quote exactly as rusqlite does. This is the one key statement
// that cannot go through the DSN, because the connection is already open.
func rekey(ctx context.Context, conn *sql.Conn, p *secret.Passphrase) error {
	return p.Use(func(b []byte) error {
		statement := make([]byte, 0, len(b)+24)
		statement = append(statement, "PRAGMA rekey = '"...)
		for _, c := range b {
			if c == '\'' {
				statement = append(statement, '\'')
			}
			statement = append(statement, c)
		}
		statement = append(statement, '\'')
		defer wipe(statement)

		if _, err := conn.ExecContext(ctx, string(statement)); err != nil {
			// Never the driver's message: it could echo the statement.
			return fmt.Errorf("database rekey failed (sqlite code %d)", sqliteCode(err))
		}
		return nil
	})
}

// WithConn runs f with the held connection under the mutex.
//
// If the operation finds the connection dead, or if the database has been
// closed, [ErrUnavailable] is returned and the database stays closed. There is
// no silent reconnect.
func (d *Database) WithConn(ctx context.Context, f func(ctx context.Context, c *sql.Conn) error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return ErrUnavailable
	}

	if err := f(ctx, d.conn); err != nil {
		if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
			d.markClosedLocked()
		}
		return err
	}
	return nil
}

// VacuumInto writes a consistent, compacted copy to target.
//
// `VACUUM INTO` runs through the live connection, so it uses the database's key
// and never sees the passphrase. The target path is bound as a parameter, never
// formatted into SQL, so quotes in the path are harmless.
func (d *Database) VacuumInto(target string) error {
	return d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		return nil
	})
}

// CanReadWithCurrentKey checks whether file can be read with the key of this
// live connection.
//
// The file is attached **without** a `KEY` clause, so SQLCipher tries the main
// database's key. A `NOTADB` error means the key does not fit (or the file is
// not a database) and yields false. A database whose schema is newer than this
// build is refused with [TooNewError].
func (d *Database) CanReadWithCurrentKey(file string) (bool, error) {
	readable := false

	err := d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS staged", file); err != nil {
			// SQLCipher can reject the key already while attaching. Nothing is
			// attached then, so there is nothing to detach.
			if isNotADB(err) {
				return nil
			}
			return fmt.Errorf("database error: %w", err)
		}

		readErr := func() error {
			var count int64
			if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM staged.sqlite_master").Scan(&count); err != nil {
				if isNotADB(err) {
					return nil
				}
				return fmt.Errorf("database error: %w", err)
			}

			var found int64
			if err := conn.QueryRowContext(ctx, "PRAGMA staged.user_version").Scan(&found); err != nil {
				return fmt.Errorf("database error: %w", err)
			}

			supported := supportedVersion(Migrations)
			if found > int64(supported) {
				return &TooNewError{Found: int(found), Supported: supported}
			}

			readable = true
			return nil
		}()

		_, detachErr := conn.ExecContext(ctx, "DETACH DATABASE staged")

		if readErr != nil {
			// The read error is the useful one; a detach failure must not mask
			// it.
			return readErr
		}
		if detachErr != nil {
			return fmt.Errorf("database error: %w", detachErr)
		}
		return nil
	})

	return readable, err
}

// CipherInfo returns the SQLCipher version and provider of the linked library.
func (d *Database) CipherInfo() (version, provider string, err error) {
	err = d.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if err := conn.QueryRowContext(ctx, "PRAGMA cipher_version").Scan(&version); err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		if err := conn.QueryRowContext(ctx, "PRAGMA cipher_provider").Scan(&provider); err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return version, provider, nil
}

// Close closes the connection. It is safe to call more than once. Every later
// operation returns [ErrUnavailable].
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	return d.closeLocked()
}

// closeLocked closes the held connection and the pool. The caller holds mu.
func (d *Database) closeLocked() error {
	d.closed = true

	var first error
	if d.conn != nil {
		if err := d.conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			first = fmt.Errorf("database error: %w", err)
		}
		d.conn = nil
	}
	if d.sqlDB != nil {
		if err := d.sqlDB.Close(); err != nil && first == nil {
			first = fmt.Errorf("database error: %w", err)
		}
		d.sqlDB = nil
	}
	return first
}

// markClosedLocked marks the database unusable after a dead connection. The
// caller holds mu.
func (d *Database) markClosedLocked() {
	d.closed = true

	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}
	if d.sqlDB != nil {
		_ = d.sqlDB.Close()
		d.sqlDB = nil
	}
}

// isNotADB reports whether err is SQLite's NOTADB (code 26).
func isNotADB(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrNotADB
	}
	return false
}

// sqliteCode returns the numeric SQLite code in err, or 0 when err is not a
// SQLite error.
func sqliteCode(err error) int {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return int(sqliteErr.Code)
	}
	return 0
}

// keyedStatementError maps an error from a statement that carried the key. It
// never returns the driver's message, which could echo the SQL.
func keyedStatementError(err error) error {
	if isNotADB(err) {
		return ErrWrongPassphraseOrDamaged
	}
	return fmt.Errorf("database open failed (sqlite code %d)", sqliteCode(err))
}

// wipe zeroes b. It is best effort: copies made by the compiler or runtime
// cannot be reached.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
