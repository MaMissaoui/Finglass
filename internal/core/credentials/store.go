// Package credentials keeps secrets out of the database and out of files.
//
// It defines the [Store] port only: the operating system's store is built by
// internal/platform/credstore, which keeps packages that must never reach the
// network (banksync) out of the store's dependency graph. This package holds
// the port, the unavailable store and an in-memory store for tests.
//
// Secrets are bytes. They are never logged and never appear in an error; a
// store's error names the operation and a fixed kind only.
package credentials

import "errors"

// KeychainService is the service every Finglass credential is filed under: the
// app's bundle identifier. The name predates the Windows and Linux stores; all
// of them file their entries under it.
const KeychainService = "io.github.mamissaoui.finglass"

// ErrUnavailable is returned by a store that this operating system cannot
// provide, or whose service is missing or locked.
var ErrUnavailable = errors.New("credential store is not available")

// Store keeps secrets by account key.
type Store interface {
	// IsAvailable reports whether this system has a protected store, so the UI
	// may offer to remember a PIN.
	IsAvailable() bool
	// Read returns the secret and whether it exists. A missing entry is not an
	// error.
	Read(account string) (secret []byte, found bool, err error)
	// Write stores a secret, replacing an existing entry for the same account.
	Write(account string, secret []byte) error
	// Delete removes an entry. Deleting a missing entry succeeds.
	Delete(account string) error
}
