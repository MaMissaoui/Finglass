// Package kdf holds SQLCipher's KDF iteration defaults. It lives in its own
// package so internal/core/db and its test helper kdftest can share them
// without an import cycle.
package kdf

// DefaultKDFIterations is SQLCipher 4's default PBKDF2 iteration count (256000).
//
// Production relies on this SQLCipher default and never sets it. Only the
// finglass_testkdf build lowers it for tests.
const DefaultKDFIterations = 256000
