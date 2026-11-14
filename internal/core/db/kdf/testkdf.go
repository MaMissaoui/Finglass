//go:build finglass_testkdf

package kdf

import (
	"database/sql"
	"fmt"

	_ "github.com/WissCore/go-sqlcipher/v4"
)

// TestKDFIterations is the SQLCipher PBKDF2 iteration count test builds use.
//
// It is one, so creating and opening a test database does not spend almost all
// of its time in SQLCipher's key derivation. Production builds never see this
// value; they use [DefaultKDFIterations].
const TestKDFIterations = 1

// SetDefaultKDFIterationsForTests sets SQLCipher's process-wide default PBKDF2
// iteration count to n and verifies it.
//
// SQLCipher keeps the default in a process-wide static, so this affects every
// connection opened afterwards, in every package and goroutine. Tests that use
// it must not run in parallel.
func SetDefaultKDFIterationsForTests(n int) error {
	if n <= 0 {
		return fmt.Errorf("invalid KDF iteration count %d", n)
	}

	sqlDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return fmt.Errorf("open throwaway connection: %w", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	// The value is an integer this package controls, never user input.
	if _, err := sqlDB.Exec(fmt.Sprintf("PRAGMA cipher_default_kdf_iter = %d", n)); err != nil {
		return fmt.Errorf("set cipher_default_kdf_iter: %w", err)
	}

	var got int
	if err := sqlDB.QueryRow("PRAGMA cipher_default_kdf_iter").Scan(&got); err != nil {
		return fmt.Errorf("read cipher_default_kdf_iter: %w", err)
	}
	if got != n {
		return fmt.Errorf("cipher_default_kdf_iter = %d, want %d", got, n)
	}
	return nil
}
