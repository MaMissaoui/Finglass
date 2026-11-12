//go:build !darwin

package banksync

import "github.com/MaMissaoui/Finglass/internal/core/credentials"

// newSystemCredentialStore returns the no-op store on every operating system
// without the macOS Keychain.
func newSystemCredentialStore() credentials.Store {
	return NoCredentialStore{}
}
