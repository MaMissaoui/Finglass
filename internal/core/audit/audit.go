// Package audit is the audit log.
//
// Every user-visible change is recorded here. Details must never contain
// secrets or bank message text.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
)

// Actor says who caused an audit entry.
type Actor string

const (
	// ActorUser is the person using the app.
	ActorUser Actor = "user"
	// ActorImport is a file import.
	ActorImport Actor = "import"
	// ActorSync is a bank sync.
	ActorSync Actor = "sync"
	// ActorSystem is the app itself, for example a migration or cleanup.
	ActorSystem Actor = "system"
)

// ActorFromStored converts a stored actor value. Unknown values read as
// [ActorSystem].
func ActorFromStored(s string) Actor {
	switch s {
	case string(ActorUser):
		return ActorUser
	case string(ActorImport):
		return ActorImport
	case string(ActorSync):
		return ActorSync
	default:
		return ActorSystem
	}
}

func storedActor(actor Actor) string {
	switch actor {
	case ActorUser, ActorImport, ActorSync:
		return string(actor)
	default:
		return string(ActorSystem)
	}
}

// Entry is one audit log entry, as read back from the database.
type Entry struct {
	// ID is the row id.
	ID int64
	// At is when it happened, as RFC 3339 in UTC.
	At string
	// Action is what happened.
	Action string
	// Actor is who did it.
	Actor Actor
	// Details is extra context. It must never contain secrets or bank message
	// text.
	Details string
}

// Execer is satisfied by *sql.Conn and *sql.Tx.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// now is overridable in tests.
var now = time.Now

// Log appends an entry with the current UTC time.
//
// details must never contain secrets or bank message text.
func Log(ctx context.Context, d *db.Database, action string, actor Actor, details string) error {
	return d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		return LogConn(ctx, conn, action, actor, details)
	})
}

// LogConn appends an entry through an existing connection.
//
// The repository layer uses this inside a transaction, where [Log] cannot be
// used because the database mutex is not re-entrant. details must never contain
// secrets or bank message text.
func LogConn(ctx context.Context, ex Execer, action string, actor Actor, details string) error {
	at := now().UTC().Format(time.RFC3339Nano)

	if _, err := ex.ExecContext(
		ctx,
		"INSERT INTO audit_log (at, action, actor, details) VALUES (?1, ?2, ?3, ?4)",
		at,
		action,
		storedActor(actor),
		details,
	); err != nil {
		return fmt.Errorf("append audit entry: %w", err)
	}
	return nil
}

// Recent reads the newest entries first.
func Recent(ctx context.Context, d *db.Database, limit uint32) ([]Entry, error) {
	var entries []Entry

	err := d.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(
			ctx,
			"SELECT id, at, action, actor, details FROM audit_log ORDER BY id DESC LIMIT ?1",
			int64(limit),
		)
		if err != nil {
			return fmt.Errorf("read audit log: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				entry      Entry
				storedName string
			)
			if err := rows.Scan(
				&entry.ID,
				&entry.At,
				&entry.Action,
				&storedName,
				&entry.Details,
			); err != nil {
				return fmt.Errorf("scan audit entry: %w", err)
			}
			entry.Actor = ActorFromStored(storedName)
			entries = append(entries, entry)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read audit log: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
