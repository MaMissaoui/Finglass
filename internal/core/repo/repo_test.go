package repo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/money"
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
		t.Fatalf("db.Create: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestDateHelpersUseTheStoredText(t *testing.T) {
	date, ok := civil.New(2026, time.October, 3)
	if !ok {
		t.Fatal("civil.New failed")
	}

	stored, err := FormatDate(date)
	if err != nil || stored != "2026-10-03" {
		t.Fatalf("FormatDate = %q, %v", stored, err)
	}
	parsed, err := ParseDate(stored)
	if err != nil || parsed != date {
		t.Fatalf("ParseDate(%q) = %v, %v", stored, parsed, err)
	}

	_, err = ParseDate("2026-13-03")
	var invalid InvalidError
	if !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Msg, "invalid date: ") {
		t.Errorf("ParseDate of an impossible date = %v", err)
	}
}

func TestTimestampHelpersKeepTheOffset(t *testing.T) {
	location := time.FixedZone("CEST", 2*60*60)
	timestamp := time.Date(2026, time.October, 3, 9, 30, 15, 123456789, location)

	stored, err := FormatTimestamp(timestamp)
	if err != nil {
		t.Fatalf("FormatTimestamp: %v", err)
	}
	if stored != "2026-10-03T09:30:15.123456789+02:00" {
		t.Errorf("FormatTimestamp = %q", stored)
	}
	parsed, err := ParseTimestamp(stored)
	if err != nil || !parsed.Equal(timestamp) || parsed.Format(time.RFC3339Nano) != stored {
		t.Errorf("ParseTimestamp(%q) = %v, %v", stored, parsed, err)
	}

	utc := time.Date(2026, time.October, 3, 9, 30, 0, 0, time.UTC)
	if stored, err := FormatTimestamp(utc); err != nil || stored != "2026-10-03T09:30:00Z" {
		t.Errorf("FormatTimestamp(UTC) = %q, %v", stored, err)
	}

	_, err = ParseTimestamp("not a timestamp")
	var invalid InvalidError
	if !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Msg, "invalid timestamp: ") {
		t.Errorf("ParseTimestamp of garbage = %v", err)
	}
}

func TestCurrencyAndBoolHelpers(t *testing.T) {
	currency, err := ParseCurrency("eur")
	if err != nil || currency != money.EUR {
		t.Errorf("ParseCurrency(eur) = %v, %v", currency, err)
	}
	if _, err := ParseCurrency("EURO"); err == nil {
		t.Error("ParseCurrency accepted EURO")
	}

	if !FromIntBool(1) || FromIntBool(0) || !FromIntBool(-1) {
		t.Error("FromIntBool does not use value != 0")
	}
	if ToIntBool(true) != 1 || ToIntBool(false) != 0 {
		t.Error("ToIntBool does not use 1/0")
	}
	if got := ToOptionalIntBool(nil); got != nil {
		t.Errorf("ToOptionalIntBool(nil) = %v", got)
	}
	yes := true
	if got := ToOptionalIntBool(&yes); got == nil || *got != 1 {
		t.Errorf("ToOptionalIntBool(true) = %v", got)
	}
	one := int64(1)
	if got := FromOptionalIntBool(&one); got == nil || !*got {
		t.Errorf("FromOptionalIntBool(1) = %v", got)
	}
}

func TestDetailHelpers(t *testing.T) {
	if got := IDAndName(7, "Name"); got != "id=7 name=Name" {
		t.Errorf("IDAndName = %q", got)
	}
	if got := IDOnly(7); got != "id=7" {
		t.Errorf("IDOnly = %q", got)
	}
}

func TestInTxCommitsOnSuccessAndRollsBackOnFailure(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	countAudit := func() int64 {
		t.Helper()
		var count int64
		err := database.WithConn(ctx, func(ctx context.Context, conn *sql.Conn) error {
			return conn.QueryRowContext(ctx, "SELECT count(*) FROM audit_log").Scan(&count)
		})
		if err != nil {
			t.Fatalf("count audit: %v", err)
		}
		return count
	}

	err := InTx(ctx, database, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			"INSERT INTO audit_log (at, action, actor, details) VALUES (?1, ?2, ?3, ?4)",
			"2026-01-01T00:00:00Z", "test", "user", "detail",
		)
		return err
	})
	if err != nil {
		t.Fatalf("InTx success: %v", err)
	}
	if count := countAudit(); count != 1 {
		t.Fatalf("audit count after commit = %d, want 1", count)
	}

	stop := errors.New("stop")
	err = InTx(ctx, database, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO audit_log (at, action, actor, details) VALUES (?1, ?2, ?3, ?4)",
			"2026-01-02T00:00:00Z", "test", "user", "detail",
		); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("InTx failure = %v, want %v", err, stop)
	}
	if count := countAudit(); count != 1 {
		t.Errorf("audit count after rollback = %d, want 1", count)
	}
}
