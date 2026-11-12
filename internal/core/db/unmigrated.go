// Opening a database without running migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// OpenUnmigrated opens an existing encrypted database without applying any
// migration and without writing its schema version.
//
// It exists for foreign databases, such as a copied FinDesk profile, whose
// schema is not Finglass's. The caller owns the file and must only use a
// throwaway copy: the connection is opened read-write so SQLite can recover a
// hot journal or replay a WAL, but this function itself only reads the schema.
func OpenUnmigrated(path string, p *secret.Passphrase) (*Database, error) {
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

	sqlDB, conn, err := openConnection(path, p)
	if err != nil {
		return nil, err
	}

	d := &Database{sqlDB: sqlDB, conn: conn}

	// Reading the schema forces SQLCipher to decrypt page 1, which is how a
	// wrong key surfaces. No pragma that writes is run and user_version is
	// only read by SQLite itself, never written.
	var count int64
	if err := conn.QueryRowContext(context.Background(), "SELECT count(*) FROM sqlite_master").Scan(&count); err != nil {
		_ = d.closeLocked()
		if isNotADB(err) {
			return nil, ErrWrongPassphraseOrDamaged
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	return d, nil
}
