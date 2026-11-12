package settings

import (
	"context"
	"path/filepath"
	"testing"

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

func TestASettingRoundTripsAndCanBeOverwritten(t *testing.T) {
	database := testDatabase(t)
	ctx := context.Background()

	if err := Set(ctx, database, "language", "en"); err != nil {
		t.Fatalf("Set en: %v", err)
	}
	value, ok, err := Get(ctx, database, "language")
	if err != nil {
		t.Fatalf("Get en: %v", err)
	}
	if !ok || value != "en" {
		t.Errorf("Get(language) = %q, %v, want en, true", value, ok)
	}

	if err := Set(ctx, database, "language", "de"); err != nil {
		t.Fatalf("Set de: %v", err)
	}
	value, ok, err = Get(ctx, database, "language")
	if err != nil {
		t.Fatalf("Get de: %v", err)
	}
	if !ok || value != "de" {
		t.Errorf("Get(language) = %q, %v, want de, true", value, ok)
	}
}

func TestAMissingKeyIsNone(t *testing.T) {
	database := testDatabase(t)

	value, ok, err := Get(context.Background(), database, "missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
	if value != "" {
		t.Errorf("value = %q, want the empty string", value)
	}
}
