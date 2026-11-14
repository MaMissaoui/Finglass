package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/db/kdftest"
)

func TestCreateUsesSQLCipherDefaultKDF(t *testing.T) {
	kdftest.RealKDF(t)

	d, err := Create(
		filepath.Join(t.TempDir(), "finglass.db"),
		newPassphrase(t, "correct horse battery staple"),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	if got := pragmaValue(t, d, "kdf_iter"); got != "256000" {
		t.Fatalf("PRAGMA kdf_iter = %q, want 256000", got)
	}
}

func TestCheapKDFOnlyInTestBuilds(t *testing.T) {
	conn := openMemoryConnection(t)

	var got int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA cipher_default_kdf_iter").Scan(&got); err != nil {
		t.Fatalf("read cipher_default_kdf_iter: %v", err)
	}

	want := DefaultKDFIterations
	if kdftest.Cheap() {
		want = 1
	}
	if got != want {
		t.Fatalf("cipher_default_kdf_iter = %d, want %d (cheap = %v)", got, want, kdftest.Cheap())
	}
}
