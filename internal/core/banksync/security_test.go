package banksync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestTanIsNeverStored(t *testing.T) {
	h := newHarness(t)
	const tan = "773311"
	const giro = "DE89370400440532013000"
	h.gateway.setRequestTan(true)
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})
	interaction := newScriptedInteraction(stringPointer("12345"), false)
	interaction.setTan(tan)

	report, err := h.sync(t, interaction)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if interaction.tanAsked.Load() == 0 {
		t.Fatal("the scripted interaction never returned a TAN")
	}
	if strings.Contains(report.Summary(), tan) {
		t.Fatalf("summary leaked the TAN: %q", report.Summary())
	}

	for _, value := range allSettings(t, h.db) {
		if strings.Contains(value, tan) {
			t.Fatalf("a setting leaked the TAN: %q", value)
		}
	}
	for _, entry := range auditEntries(t, h.db, 100_000) {
		haystack := entry.At + " " + entry.Action + " " + string(entry.Actor) + " " + entry.Details
		if strings.Contains(haystack, tan) {
			t.Fatalf("an audit row leaked the TAN: %q", haystack)
		}
	}
	assertNoTextColumnContains(t, h.db, tan)

	profileFS := os.DirFS(h.dataDir)
	walkErr := fs.WalkDir(profileFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(profileFS, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), tan) {
			t.Fatalf("file %s leaked the TAN", path)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		t.Fatalf("walk profile dir: %v", walkErr)
	}

	h.store.mu.RLock()
	defer h.store.mu.RUnlock()
	for account, value := range h.store.entries {
		if strings.Contains(account, tan) || strings.Contains(string(value), tan) {
			t.Fatalf("credential store entry %q leaked the TAN", account)
		}
	}
}

func TestPinNeverInAuditOrErrors(t *testing.T) {
	h := newHarness(t)
	const pin = "12345"
	const giro = "DE89370400440532013000"
	h.gateway.setSnapshots([]AccountSnapshot{snapshot(giro, "100.00", nil)})

	// A success.
	if _, err := h.sync(t, newScriptedInteraction(stringPointer(pin), true)); err != nil {
		t.Fatalf("success sync: %v", err)
	}

	// A rejection.
	rejection := NewGatewayPinRejectedError()
	h.gateway.setFailure(gatewayFailure(rejection))
	_, rejectionErr := h.sync(t, newScriptedInteraction(stringPointer(pin), false))
	if rejectionErr == nil {
		t.Fatal("rejection sync succeeded")
	}
	if strings.Contains(rejectionErr.Error(), pin) {
		t.Fatalf("rejection error leaked the PIN: %q", rejectionErr.Error())
	}

	// A store failure.
	h.gateway.setFailure(nil)
	h.store.SetFailingWrites(true)
	if _, err := h.sync(t, newScriptedInteraction(stringPointer(pin), true)); err != nil {
		t.Fatalf("store-failure sync: %v", err)
	}
	if !h.service.PinNotSaved() {
		t.Fatal("pin not saved = false")
	}
	bank := loadBankItem(t, h.db, h.bank)
	saveErr := h.service.Client().savePin(
		context.Background(),
		h.db,
		bank.ID,
		NewPinEntry(secret.NewText([]byte(pin)), true),
	)
	if saveErr == nil {
		t.Fatal("savePin succeeded with a failing store")
	}
	if strings.Contains(saveErr.Error(), pin) {
		t.Fatalf("savePin error leaked the PIN: %q", saveErr.Error())
	}

	for _, value := range allSettings(t, h.db) {
		if strings.Contains(value, pin) {
			t.Fatalf("a setting leaked the PIN: %q", value)
		}
	}
	for _, entry := range auditEntries(t, h.db, 100_000) {
		haystack := entry.At + " " + entry.Action + " " + string(entry.Actor) + " " + entry.Details
		if strings.Contains(haystack, pin) {
			t.Fatalf("an audit row leaked the PIN: %q", haystack)
		}
	}
	assertNoTextColumnContains(t, h.db, pin)
	if strings.Contains(fmt.Sprintf("%v", h.service.PinNotSaved()), pin) {
		t.Fatal("pin-not-saved formatting leaked the PIN")
	}
}

func TestRememberingInteractionIsSafeForConcurrentUse(t *testing.T) {
	inner := newScriptedInteraction(stringPointer("77777"), true)
	var rejected atomic.Int64
	remembering := newRememberingInteraction(
		inner,
		secret.NewText([]byte("12345")),
		func() { rejected.Add(1) },
	)

	const goroutines = 24
	entries := make([]*PinEntry, goroutines)
	var group sync.WaitGroup
	for index := 0; index < goroutines; index++ {
		group.Add(1)
		go func(slot int) {
			defer group.Done()
			entries[slot] = remembering.Pin("Bank", "user", true)
		}(index)
	}
	group.Wait()

	storedUses := 0
	innerUses := 0
	for _, entry := range entries {
		if entry == nil {
			t.Fatal("a concurrent Pin call returned nil")
		}
		if entry.Remember() {
			innerUses++
		} else {
			storedUses++
		}
	}
	assertInt(t, storedUses, 1)
	assertInt(t, innerUses, goroutines-1)
	assertInt(t, inner.pinAskedCount(), goroutines-1)
	if got := rejected.Load(); got != 1 {
		t.Fatalf("onStoredRejected calls = %d, want 1", got)
	}
	if !remembering.storedUsed() {
		t.Fatal("stored PIN was not marked used")
	}
	if last := remembering.takeLastEntry(); last == nil {
		t.Fatal("the last entry was not kept")
	}
	if last := remembering.takeLastEntry(); last != nil {
		t.Fatal("the last entry was not cleared")
	}
}

func allSettings(t *testing.T, database *db.Database) []string {
	t.Helper()
	values := make([]string, 0)
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		rows, err := conn.QueryContext(ctx, "SELECT key, value FROM settings")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return err
			}
			values = append(values, key, value)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	return values
}

func assertNoTextColumnContains(t *testing.T, database *db.Database, wanted string) {
	t.Helper()
	err := database.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		tables, err := tableNames(ctx, conn)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if err := scanTextColumns(ctx, conn, table, wanted); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan text columns: %v", err)
	}
}

func tableNames(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(
		ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tables := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

func scanTextColumns(ctx context.Context, conn *sql.Conn, table, wanted string) error {
	columns, err := textColumns(ctx, conn, table)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = quoteSQLiteIdentifier(column)
	}
	query := strings.Join([]string{
		"SELECT",
		strings.Join(quoted, ", "),
		"FROM",
		quoteSQLiteIdentifier(table),
	}, " ")
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return err
		}
		for _, value := range values {
			if value.Valid && strings.Contains(value.String, wanted) {
				return fmt.Errorf("table %s leaked %q", table, wanted)
			}
		}
	}
	return rows.Err()
}

func textColumns(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	query := strings.Join([]string{
		"PRAGMA table_info(",
		quoteSQLiteIdentifier(table),
		")",
	}, "")
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns := make([]string, 0)
	for rows.Next() {
		var (
			position     int
			name         string
			columnType   string
			notNull      int
			defaultValue any
			primaryKey   int
		)
		if err := rows.Scan(&position, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		upper := strings.ToUpper(columnType)
		if strings.Contains(upper, "TEXT") || strings.Contains(upper, "CHAR") || strings.Contains(upper, "CLOB") {
			columns = append(columns, name)
		}
	}
	return columns, rows.Err()
}

func quoteSQLiteIdentifier(identifier string) string {
	return strings.Join([]string{`"`, strings.ReplaceAll(identifier, `"`, `""`), `"`}, "")
}
