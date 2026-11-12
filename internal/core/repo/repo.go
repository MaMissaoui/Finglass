// Package repo holds the shared parts of the repositories: errors, transaction
// handling and column helpers.
//
// Every mutating repository function runs in one transaction and writes exactly
// one audit entry in that transaction. Audit details contain ids and names
// only, never IBANs, account numbers, user ids or purpose text. It ports
// `crates/finglass-core/src/repo/mod.rs`.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/money"
)

// ErrNotFound reports that the requested row does not exist.
var ErrNotFound = errors.New("record not found")

// ErrCurrencyMismatch reports a money value that uses a different currency
// than the row it belongs to.
var ErrCurrencyMismatch = errors.New("currency mismatch")

// InvalidError reports stored or supplied text that is not valid for the
// column.
type InvalidError struct {
	// Msg is the detail after the "invalid value: " prefix.
	Msg string
}

// Error implements error with Rust's RepoError::Invalid display.
func (e InvalidError) Error() string {
	return "invalid value: " + e.Msg
}

// InTx runs work in a transaction on the database's single connection.
//
// A failure rolls the transaction back; the audit entry a repository writes
// inside the transaction rolls back with it.
func InTx(ctx context.Context, d *db.Database, work func(ctx context.Context, tx *sql.Tx) error) error {
	return d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		if err := work(ctx, tx); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	})
}

// FormatDate formats a date as `YYYY-MM-DD`.
func FormatDate(date civil.Date) (string, error) {
	return date.String(), nil
}

// ParseDate parses a `YYYY-MM-DD` date.
func ParseDate(text string) (civil.Date, error) {
	date, err := civil.ParseISO(text)
	if err != nil {
		return civil.Date{}, InvalidError{Msg: "invalid date: " + err.Error()}
	}
	return date, nil
}

// FormatTimestamp formats a timestamp as RFC 3339 text, keeping the value's own
// offset.
func FormatTimestamp(timestamp time.Time) (string, error) {
	return timestamp.Format(time.RFC3339Nano), nil
}

// ParseTimestamp parses an RFC 3339 timestamp.
func ParseTimestamp(text string) (time.Time, error) {
	timestamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, InvalidError{Msg: "invalid timestamp: " + err.Error()}
	}
	return timestamp, nil
}

// ParseCurrency reads a currency from a stored text column.
func ParseCurrency(text string) (money.Currency, error) {
	return money.NewCurrency(text)
}

// FromIntBool reads a boolean from its `INTEGER` 0/1 representation.
func FromIntBool(value int64) bool {
	return value != 0
}

// ToIntBool converts a boolean to its `INTEGER` 0/1 representation.
func ToIntBool(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// ToOptionalIntBool converts an optional boolean to its `INTEGER`
// representation.
func ToOptionalIntBool(value *bool) *int64 {
	if value == nil {
		return nil
	}
	converted := ToIntBool(*value)
	return &converted
}

// FromOptionalIntBool reads an optional boolean from its `INTEGER`
// representation.
func FromOptionalIntBool(value *int64) *bool {
	if value == nil {
		return nil
	}
	converted := FromIntBool(*value)
	return &converted
}

// IDAndName is the detail text a repository writes for an id and a name.
func IDAndName(id int64, name string) string {
	return "id=" + strconv.FormatInt(id, 10) + " name=" + name
}

// IDOnly is the detail text a repository writes for an id only.
func IDOnly(id int64) string {
	return "id=" + strconv.FormatInt(id, 10)
}
