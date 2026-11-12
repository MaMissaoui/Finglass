// Package settings stores key/value settings in the encrypted database.
//
// The SQL matches `crates/finglass-core/src/settings.rs`, so both builds see
// the same rows.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MaMissaoui/Finglass/internal/core/db"
)

// Get reads a setting. ok is false when the key is absent.
func Get(ctx context.Context, d *db.Database, key string) (value string, ok bool, err error) {
	err = d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		scanErr := conn.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?1", key).Scan(&value)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			return nil
		case scanErr != nil:
			return fmt.Errorf("read setting: %w", scanErr)
		default:
			ok = true
			return nil
		}
	})
	if err != nil {
		return "", false, err
	}
	return value, ok, nil
}

// Set writes a setting, replacing an existing value.
func Set(ctx context.Context, d *db.Database, key, value string) error {
	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			`INSERT INTO settings (key, value) VALUES (?1, ?2)
             ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
			key,
			value,
		)
		if err != nil {
			return fmt.Errorf("write setting: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
