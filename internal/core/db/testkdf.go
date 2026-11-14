//go:build finglass_testkdf

package db

import (
	"fmt"

	dbkdf "github.com/MaMissaoui/Finglass/internal/core/db/kdf"
)

// TestKDFIterations is the SQLCipher PBKDF2 iteration count test builds use.
//
// It is one, so creating and opening a test database does not spend almost all
// of its time in SQLCipher's key derivation. Production builds never see this
// value; they use [DefaultKDFIterations].
const TestKDFIterations = dbkdf.TestKDFIterations

// SetDefaultKDFIterationsForTests sets SQLCipher's process-wide default PBKDF2
// iteration count to n and verifies it.
//
// SQLCipher keeps the default in a process-wide static, so this affects every
// connection opened afterwards, in every package and goroutine. Tests that use
// it must not run in parallel.
func SetDefaultKDFIterationsForTests(n int) error {
	return dbkdf.SetDefaultKDFIterationsForTests(n)
}

func init() {
	if err := SetDefaultKDFIterationsForTests(TestKDFIterations); err != nil {
		panic(fmt.Sprintf("finglass_testkdf: %v", err))
	}
}
