package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()

	pass, err := secret.NewPassphrase([]byte("correct horse battery staple"), secret.New)
	if err != nil {
		t.Fatalf("NewPassphrase: %v", err)
	}

	database, err := db.Create(filepath.Join(t.TempDir(), "finglass.db"), pass)
	if err != nil {
		t.Fatalf("Create database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return database
}

func TestRecentReturnsTheNewestEntriesFirst(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	if err := Log(ctx, database, "first", ActorUser, "one"); err != nil {
		t.Fatalf("Log first: %v", err)
	}
	if err := Log(ctx, database, "second", ActorImport, "two"); err != nil {
		t.Fatalf("Log second: %v", err)
	}
	if err := Log(ctx, database, "third", ActorSync, "three"); err != nil {
		t.Fatalf("Log third: %v", err)
	}

	entries, err := Recent(ctx, database, 2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Action != "third" || entries[0].Actor != ActorSync || entries[0].Details != "three" {
		t.Errorf("entries[0] = %+v, want third/sync/three", entries[0])
	}
	if entries[1].Action != "second" || entries[1].Actor != ActorImport || entries[1].Details != "two" {
		t.Errorf("entries[1] = %+v, want second/import/two", entries[1])
	}
	if got := storedActor(entries[0].Actor); got != "sync" {
		t.Errorf("stored actor = %q, want sync", got)
	}
}

func TestAnEntryHasARFC3339UTCTimestamp(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	if err := Log(ctx, database, "opened", ActorSystem, ""); err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, err := Recent(ctx, database, 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	entry := entries[0]
	if len(entry.At) < 20 {
		t.Fatalf("unexpected timestamp %q", entry.At)
	}
	if entry.At[len(entry.At)-1] != 'Z' {
		t.Errorf("timestamp %q does not end in Z", entry.At)
	}
	if entry.At[4] != '-' || entry.At[7] != '-' {
		t.Errorf("timestamp %q has the wrong date separators", entry.At)
	}
	if entry.At[10] != 'T' {
		t.Errorf("timestamp %q has the wrong date/time separator", entry.At)
	}
	if entry.At[13] != ':' || entry.At[16] != ':' {
		t.Errorf("timestamp %q has the wrong time separators", entry.At)
	}
}

func TestActorsAreStoredAsLowercaseText(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	for _, actor := range []Actor{ActorUser, ActorImport, ActorSync, ActorSystem} {
		if err := Log(ctx, database, "check", actor, ""); err != nil {
			t.Fatalf("Log %s: %v", actor, err)
		}
	}

	err := database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT actor FROM audit_log ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		var stored []string
		for rows.Next() {
			var actor string
			if err := rows.Scan(&actor); err != nil {
				return err
			}
			stored = append(stored, actor)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		want := []string{"user", "import", "sync", "system"}
		if len(stored) != len(want) {
			t.Errorf("stored actors = %v, want %v", stored, want)
			return nil
		}
		for index := range want {
			if stored[index] != want[index] {
				t.Errorf("stored actors = %v, want %v", stored, want)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithConn: %v", err)
	}
}

func TestTimestampFormatMatchesRust(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	original := now
	t.Cleanup(func() { now = original })

	base := time.Date(2026, time.November, 12, 8, 9, 10, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero nanoseconds", base, "2026-11-12T08:09:10Z"},
		{"500 milliseconds", base.Add(500 * time.Millisecond), "2026-11-12T08:09:10.5Z"},
		{"123456789 nanoseconds", base.Add(123456789 * time.Nanosecond), "2026-11-12T08:09:10.123456789Z"},
		{
			"non UTC input is stored as UTC",
			base.In(time.FixedZone("CET", 3600)),
			"2026-11-12T08:09:10Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.at
			now = func() time.Time { return at }

			if err := Log(ctx, database, "timestamp", ActorSystem, ""); err != nil {
				t.Fatalf("Log: %v", err)
			}
			entries, err := Recent(ctx, database, 1)
			if err != nil {
				t.Fatalf("Recent: %v", err)
			}
			if entries[0].At != tc.want {
				t.Errorf("at = %q, want %q", entries[0].At, tc.want)
			}
		})
	}
}

func TestUnknownStoredActorReadsAsSystem(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	err := database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		_, err := conn.ExecContext(
			ctx,
			"INSERT INTO audit_log (at, action, actor, details) VALUES (?1, ?2, ?3, ?4)",
			"2026-11-12T08:09:10Z",
			"check",
			"robot",
			"",
		)
		return err
	})
	if err != nil {
		t.Fatalf("WithConn: %v", err)
	}

	entries, err := Recent(ctx, database, 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if entries[0].Actor != ActorSystem {
		t.Errorf("actor = %q, want system", entries[0].Actor)
	}
	if got := ActorFromStored("robot"); got != ActorSystem {
		t.Errorf("ActorFromStored(robot) = %q, want system", got)
	}
}

func TestLogConnWorksInsideATransaction(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	err := database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := LogConn(ctx, tx, "committed", ActorUser, "yes"); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatalf("committed transaction: %v", err)
	}

	err = database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := LogConn(ctx, tx, "rolled-back", ActorUser, "no"); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Rollback()
	})
	if err != nil {
		t.Fatalf("rolled-back transaction: %v", err)
	}

	entries, err := Recent(ctx, database, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: %+v", len(entries), entries)
	}
	if entries[0].Action != "committed" || entries[0].Details != "yes" {
		t.Errorf("entry = %+v, want committed/yes", entries[0])
	}
}
