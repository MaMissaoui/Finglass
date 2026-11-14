//go:build !finglass_testkdf

// Package kdftest restores the real SQLCipher KDF for tests that read fixtures
// written by production builds. It is imported only from _test.go files.
package kdftest

import "testing"

// Cheap reports whether the build lowered SQLCipher's KDF default for tests.
func Cheap() bool {
	return false
}

// RealKDF is a no-op: without the finglass_testkdf tag the default is already
// the real KDF iteration count.
func RealKDF(testing.TB) {}
