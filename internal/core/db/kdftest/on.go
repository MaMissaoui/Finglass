//go:build finglass_testkdf

// Package kdftest restores the real SQLCipher KDF for tests that read fixtures
// written by production builds. It is imported only from _test.go files.
package kdftest

import (
	"testing"

	dbkdf "github.com/MaMissaoui/Finglass/internal/core/db/kdf"
)

// Cheap reports whether the build lowered SQLCipher's KDF default for tests.
func Cheap() bool {
	return true
}

// RealKDF sets SQLCipher's process-wide default KDF iteration count back to the
// real one for a test that reads a fixture written by a production build, and
// registers a cleanup restoring the cheap test default.
//
// The setting is process-wide, so it must not be used from parallel tests.
func RealKDF(t testing.TB) {
	t.Helper()

	if err := dbkdf.SetDefaultKDFIterationsForTests(dbkdf.DefaultKDFIterations); err != nil {
		t.Fatalf("kdftest.RealKDF: %v", err)
	}
	t.Cleanup(func() {
		if err := dbkdf.SetDefaultKDFIterationsForTests(dbkdf.TestKDFIterations); err != nil {
			t.Errorf("kdftest.RealKDF cleanup: %v", err)
		}
	})
}
