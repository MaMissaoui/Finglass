package banksync

import (
	"os"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

// KeychainService is the service all Keychain entries are filed under: the
// app's bundle identifier.
const KeychainService = credentials.KeychainService

// CredentialStoreEnv can force the no-op store, e.g. for development runs.
const CredentialStoreEnv = "FINGLASS_CREDENTIAL_STORE" // #nosec G101 -- an environment variable name, not a credential.

// AccountFor returns the account string of a bank access's remembered PIN, in
// the form the credential store uses.
func AccountFor(profileDir string, bankID int64) string {
	return credentials.AccountFor(profileDir, bankID)
}

// SystemCredentialStore returns the credential store the app should use: the
// store the platform built, unless FINGLASS_CREDENTIAL_STORE=none forces the
// no-op store.
//
// The desktop app builds the platform store once at startup with
// credstore.NewSystem and passes it here. banksync never builds it itself: the
// Windows and D-Bus dependencies reach net, and banksync must not.
func SystemCredentialStore(store credentials.Store) credentials.Store {
	if value, ok := os.LookupEnv(CredentialStoreEnv); ok {
		return SystemCredentialStoreWith(store, &value)
	}
	return SystemCredentialStoreWith(store, nil)
}

// SystemCredentialStoreWith is [SystemCredentialStore] with the override value
// passed in, so tests never touch the environment. A nil store (no platform
// store built) is the no-op store.
func SystemCredentialStoreWith(store credentials.Store, override *string) credentials.Store {
	if override != nil && *override == "none" {
		return NoCredentialStore{}
	}
	if store == nil {
		return NoCredentialStore{}
	}
	return store
}
