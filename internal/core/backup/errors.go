// Errors from backups and restores.
package backup

import "errors"

// Sentinel errors. Their messages match the Rust `BackupError` texts.
var (
	// ErrNotABackup means the file has no backup manifest.
	ErrNotABackup = errors.New("not a Finglass backup")

	// ErrNoDatabase means the backup has a manifest but no database entry.
	ErrNoDatabase = errors.New("backup contains no database")

	// ErrTooLarge means the backup is too large to restore.
	ErrTooLarge = errors.New("backup is too large to restore")

	// ErrTooNew means the backup database comes from a newer Finglass build.
	ErrTooNew = errors.New("backup database is newer than this version of Finglass")
)

// UnsafePathError means a ZIP entry points outside the staging folder or at an
// unexpected location.
type UnsafePathError struct {
	// Name is the entry name exactly as the ZIP declared it.
	Name string
}

// Error implements error.
func (e *UnsafePathError) Error() string {
	return "unsafe path in backup: " + e.Name
}

// PassphraseRequiredError means the backup was made with a passphrase other
// than the live database's. It never carries a passphrase.
type PassphraseRequiredError struct {
	// WrongPassphraseGiven says whether a passphrase was supplied and turned
	// out to be wrong.
	WrongPassphraseGiven bool
}

// Error implements error.
func (e *PassphraseRequiredError) Error() string {
	return "this backup was made with a different passphrase"
}
