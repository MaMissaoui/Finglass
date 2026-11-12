// Package unlock ports crates/finglass-core/src/app/unlock.rs: unlocking,
// creating and rekeying the encrypted database.
//
// Finglass has no plaintext databases, so there is no "encrypt plaintext" mode:
// a profile is either new or already encrypted.
package unlock

import (
	"context"
	"crypto/subtle"
	"errors"
	"os"

	"golang.org/x/text/unicode/norm"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// UnlockMode says what the unlock window should offer for a profile.
type UnlockMode uint8

const (
	// ModeCreate means no database yet: choose a passphrase and create one.
	ModeCreate UnlockMode = iota
	// ModeUnlock means an encrypted database exists: enter its passphrase.
	ModeUnlock
	// ModeChange means an open database: replace its passphrase.
	ModeChange
)

// UnlockError values. No value carries the passphrase or any other secret.
var (
	// ErrWrongPassphrase means the passphrase is wrong (or the database is
	// damaged).
	ErrWrongPassphrase = errors.New("wrong passphrase")
	// ErrTooShort means a new passphrase had fewer than the minimum number of
	// characters.
	ErrTooShort = errors.New("passphrase is too short")
	// ErrSpaces means a new passphrase starts or ends with whitespace.
	ErrSpaces = errors.New("passphrase has surrounding spaces")
	// ErrMismatch means the two new passphrases differ.
	ErrMismatch = errors.New("passphrases do not match")
	// ErrNotAcknowledged means the user did not confirm that a forgotten
	// passphrase cannot be recovered.
	ErrNotAcknowledged = errors.New("recovery was not acknowledged")
	// ErrNotEncrypted means the file is a plaintext SQLite database, which
	// Finglass does not open.
	ErrNotEncrypted = errors.New("database is not encrypted")
	// ErrTooNew means the database was written by a newer version of Finglass.
	ErrTooNew = errors.New("database is too new")
	// ErrFailed means any other database, path or audit error.
	ErrFailed = errors.New("unlock failed")
)

// ModeFor returns the mode for a database file: unlock it when it exists,
// otherwise create it.
func ModeFor(dbFile string) UnlockMode {
	if _, err := os.Stat(dbFile); err == nil {
		return ModeUnlock
	}
	return ModeCreate
}

// ValidateNew validates a new passphrase in FinDesk's order: rules, then the
// repeat, then the acknowledgement.
//
// It returns the normalized passphrase on success. The repeat is normalized
// for comparison but is not itself required to satisfy the length rule; a
// short repeat is reported as a mismatch, exactly as in FinDesk.
//
// The caller owns and must wipe passphrase and repeat. ValidateNew never
// modifies either input. The caller owns the returned Passphrase and must wipe
// it when it is no longer needed.
func ValidateNew(passphrase, repeat []byte, acknowledged bool) (*secret.Passphrase, error) {
	created, err := secret.NewPassphrase(passphrase, secret.New)
	if err != nil {
		switch {
		case errors.Is(err, secret.ErrEmpty):
			return nil, ErrTooShort
		default:
			var tooShort *secret.TooShortError
			if errors.As(err, &tooShort) {
				return nil, ErrTooShort
			}
			if errors.Is(err, secret.ErrSurroundingWhitespace) {
				return nil, ErrSpaces
			}
			return nil, ErrFailed
		}
	}

	normalizedRepeat := norm.NFC.Append(nil, repeat...)
	defer wipe(normalizedRepeat)

	matches := false
	if useErr := created.Use(func(value []byte) error {
		if len(value) == len(normalizedRepeat) {
			matches = subtle.ConstantTimeCompare(value, normalizedRepeat) == 1
		}
		return nil
	}); useErr != nil {
		created.Wipe()
		return nil, ErrFailed
	}
	if !matches {
		created.Wipe()
		return nil, ErrMismatch
	}

	if !acknowledged {
		created.Wipe()
		return nil, ErrNotAcknowledged
	}

	return created, nil
}

// Unlock opens an existing database.
//
// The caller owns and must wipe input. Unlock never modifies it.
func Unlock(appPaths paths.AppPaths, slug string, input []byte) (*db.Database, error) {
	passphrase, err := secret.NewPassphrase(input, secret.Unlock)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	defer passphrase.Wipe()

	path, err := appPaths.DatabaseFile(slug)
	if err != nil {
		return nil, ErrFailed
	}

	opened, err := db.Open(path, passphrase)
	if err != nil {
		return nil, mapDBError(err)
	}
	return opened, nil
}

// Create creates a new encrypted database for slug.
//
// The caller owns and must wipe passphrase and repeat. Create never modifies
// either input.
func Create(appPaths paths.AppPaths, slug string, passphrase, repeat []byte, acknowledged bool) (*db.Database, error) {
	created, err := ValidateNew(passphrase, repeat, acknowledged)
	if err != nil {
		return nil, err
	}
	defer created.Wipe()

	profileDir, err := appPaths.ProfileDir(slug)
	if err != nil {
		return nil, ErrFailed
	}
	if err := paths.EnsureDir(profileDir); err != nil {
		return nil, ErrFailed
	}

	path, err := appPaths.DatabaseFile(slug)
	if err != nil {
		return nil, ErrFailed
	}
	opened, err := db.Create(path, created)
	if err != nil {
		return nil, mapDBError(err)
	}

	if err := audit.Log(context.Background(), opened, "DATABASE_CREATED", audit.ActorUser, "New encrypted database"); err != nil {
		_ = opened.Close()
		return nil, ErrFailed
	}

	return opened, nil
}

// Change replaces the passphrase of an already open database.
//
// The caller has opened the database with the current passphrase, so the
// current passphrase is not needed here. The caller owns and must wipe
// passphrase and repeat. Change never modifies either input.
func Change(d *db.Database, passphrase, repeat []byte, acknowledged bool) error {
	created, err := ValidateNew(passphrase, repeat, acknowledged)
	if err != nil {
		return err
	}
	defer created.Wipe()

	if err := d.ChangePassphrase(created); err != nil {
		return mapDBError(err)
	}

	if err := audit.Log(
		context.Background(),
		d,
		"DATABASE_PASSPHRASE_CHANGED",
		audit.ActorUser,
		"Database passphrase changed",
	); err != nil {
		return ErrFailed
	}

	return nil
}

func mapDBError(err error) error {
	switch {
	case errors.Is(err, db.ErrWrongPassphraseOrDamaged):
		return ErrWrongPassphrase
	case errors.Is(err, db.ErrNotEncrypted):
		return ErrNotEncrypted
	default:
		var tooNew *db.TooNewError
		if errors.As(err, &tooNew) {
			return ErrTooNew
		}
		return ErrFailed
	}
}

// wipe zeroes b. It is best effort: the compiler and runtime may keep copies.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
