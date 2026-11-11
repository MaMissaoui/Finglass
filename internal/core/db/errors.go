// Errors from opening, creating and using the encrypted database.
package db

import (
	"errors"
	"fmt"
)

// Sentinel errors. Their messages match the Rust `DbError` texts where a Rust
// variant exists.
var (
	// ErrNotFound means the database file does not exist.
	ErrNotFound = errors.New("database file not found")

	// ErrAlreadyExists means the database file already exists.
	ErrAlreadyExists = errors.New("database file already exists")

	// ErrNotEncrypted means the file has a plain SQLite header instead of an
	// encrypted SQLCipher file.
	ErrNotEncrypted = errors.New("database file is not encrypted")

	// ErrWrongPassphraseOrDamaged means the passphrase is wrong or the file is
	// damaged. SQLCipher cannot tell the two apart.
	ErrWrongPassphraseOrDamaged = errors.New("wrong passphrase or damaged database")

	// ErrUnavailable means the connection is gone: it was closed, or an
	// operation found it dead. The database never reconnects silently.
	ErrUnavailable = errors.New("database connection is unavailable")

	// ErrInvalidPath means the path cannot be handed to the driver. A question
	// mark would be parsed as a DSN parameter.
	ErrInvalidPath = errors.New("database path must not contain '?'")

	// ErrInvalidPassphrase means the passphrase cannot travel through the
	// driver's DSN parameter. A NUL byte would truncate the C string the driver
	// builds for `PRAGMA key`.
	ErrInvalidPassphrase = errors.New("passphrase must not contain a NUL byte")
)

// TooNewError means the database was created by a newer Finglass build.
type TooNewError struct {
	// Found is the version found in the file.
	Found int
	// Supported is the newest version this build knows about.
	Supported int
}

// Error implements error.
func (e *TooNewError) Error() string {
	return fmt.Sprintf("database version %d is newer than supported version %d", e.Found, e.Supported)
}

// MigrationError means one migration step failed.
type MigrationError struct {
	// Version is the migration version that failed.
	Version int
	// Err is the underlying SQLite error.
	Err error
}

// Error implements error.
func (e *MigrationError) Error() string {
	return fmt.Sprintf("migration %d failed", e.Version)
}

// Unwrap returns the underlying error.
func (e *MigrationError) Unwrap() error {
	return e.Err
}
