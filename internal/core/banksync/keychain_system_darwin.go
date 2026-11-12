//go:build darwin

package banksync

import "github.com/MaMissaoui/Finglass/internal/core/credentials"

// newSystemCredentialStore returns the macOS login Keychain store. It is only
// constructed by [SystemCredentialStoreWith] without the "none" override.
func newSystemCredentialStore() credentials.Store {
	return credentials.NewKeychain(KeychainService)
}
