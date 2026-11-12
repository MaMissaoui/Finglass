package banksync

import (
	"os"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

// KeychainService is the service all Keychain entries are filed under: the
// app's bundle identifier.
const KeychainService = credentials.KeychainService

// CredentialStoreEnv can force the no-op store, e.g. for development runs.
const CredentialStoreEnv = "FINGLASS_" + "CREDENTIAL_STORE"

// AccountFor returns the account string of a bank access's remembered PIN, in
// the form the credential store uses.
func AccountFor(profileDir string, bankID int64) string {
	return credentials.AccountFor(profileDir, bankID)
}

// SystemCredentialStore returns the credential store the running system offers.
//
// The app calls this once at startup. FINGLASS_CREDENTIAL_STORE=none forces the
// no-op store; on every other operating system the no-op store is used
// regardless.
func SystemCredentialStore() credentials.Store {
	if value, ok := os.LookupEnv(CredentialStoreEnv); ok {
		return SystemCredentialStoreWith(&value)
	}
	return SystemCredentialStoreWith(nil)
}

// SystemCredentialStoreWith is [SystemCredentialStore] with the override value
// passed in, so tests never touch the environment.
func SystemCredentialStoreWith(override *string) credentials.Store {
	if override != nil && *override == "none" {
		return NoCredentialStore{}
	}
	return newSystemCredentialStore()
}
