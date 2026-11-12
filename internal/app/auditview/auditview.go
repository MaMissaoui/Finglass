// Package auditview ports crates/finglass-core/src/app/audit.rs: audit log
// rows ready for the settings screen.
package auditview

import (
	"context"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/db"
)

// AuditRow is one audit log row, with its timestamp already rendered in local
// time.
type AuditRow struct {
	// Time is the local date and time, for example "2026-11-07 14:05".
	Time string
	// Action is the stored action name.
	Action string
	// Actor is the stored actor name.
	Actor string
	// Details is the stored details.
	Details string
}

// Recent reads the newest limit entries, newest first.
func Recent(ctx context.Context, d *db.Database, limit uint32) ([]AuditRow, error) {
	entries, err := audit.Recent(ctx, d, limit)
	if err != nil {
		return nil, err
	}

	rows := make([]AuditRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, AuditRow{
			Time:    localTime(entry.At),
			Action:  entry.Action,
			Actor:   string(entry.Actor),
			Details: entry.Details,
		})
	}
	return rows, nil
}

func localTime(timestamp string) string {
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return parsed.In(time.Local).Format("2006-01-02 15:04")
}
