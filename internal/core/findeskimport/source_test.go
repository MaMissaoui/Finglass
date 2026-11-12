package findeskimport

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

func TestSourceNumberScanning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbers.db")
	opened, err := db.Create(path, passphrase("source number passphrase", secret.New))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = opened.Close()
	}()

	err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, "CREATE TABLE numbers (id INTEGER PRIMARY KEY, value)"); err != nil {
			return err
		}
		values := []any{
			int64(5),
			float64(1.25),
			"hello",
			string([]byte{0xFF, 0xFE}),
			string([]byte{0xE2, 0x82}),
			[]byte{0x00, 0xFF},
			nil,
		}
		for _, value := range values {
			if _, err := conn.ExecContext(ctx, "INSERT INTO numbers (value) VALUES (?1)", value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("prepare numbers: %v", err)
	}

	type scanned struct {
		id    int64
		value any
	}
	var rows []scanned
	err = opened.WithConn(context.Background(), func(ctx context.Context, conn *sql.Conn) error {
		result, err := conn.QueryContext(ctx, "SELECT id, value FROM numbers ORDER BY id")
		if err != nil {
			return err
		}
		defer func() {
			_ = result.Close()
		}()
		for result.Next() {
			var row scanned
			if err := result.Scan(&row.id, &row.value); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return result.Err()
	})
	if err != nil {
		t.Fatalf("read numbers: %v", err)
	}
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7", len(rows))
	}

	integer, err := sourceNumberFromValue(rows[0].value)
	if err != nil || integer.kind != sourceInteger || integer.integer != 5 {
		t.Errorf("INTEGER = %+v, %v", integer, err)
	}
	real, err := sourceNumberFromValue(rows[1].value)
	if err != nil || real.kind != sourceReal || math.Abs(real.real-1.25) > 0 {
		t.Errorf("REAL = %+v, %v", real, err)
	}
	text, err := sourceNumberFromValue(rows[2].value)
	if err != nil || text.kind != sourceText || text.text != "hello" {
		t.Errorf("TEXT = %+v, %v", text, err)
	}
	invalidText, err := sourceNumberFromValue(rows[3].value)
	if err != nil || invalidText.text != "\uFFFD\uFFFD" {
		t.Errorf("invalid TEXT = %+v, %v", invalidText, err)
	}
	truncatedText, err := sourceNumberFromValue(rows[4].value)
	if err != nil || truncatedText.text != "\uFFFD" {
		t.Errorf("truncated TEXT = %+v, %v", truncatedText, err)
	}
	if _, err := sourceNumberFromValue(rows[5].value); err == nil {
		t.Error("BLOB was accepted")
	}
	if _, err := sourceNumberFromValue(rows[6].value); err == nil {
		t.Error("NULL was accepted")
	}
}
