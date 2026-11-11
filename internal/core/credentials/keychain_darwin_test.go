//go:build darwin

package credentials

import (
	"crypto/rand"
	"fmt"
	"os"
	"testing"
)

// TestKeychainRoundTrip talks to the real login Keychain and therefore runs
// only when the person running the tests asks for it explicitly.
func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("FINGLASS_KEYCHAIN_TEST") != "1" {
		t.Skip("set FINGLASS_KEYCHAIN_TEST=1 to run against the real Keychain")
	}

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("random service suffix: %v", err)
	}
	service := fmt.Sprintf("%s.test-%x", KeychainService, random)
	store := NewKeychain(service)

	const account = "profile#bank-7"
	t.Cleanup(func() {
		if err := store.Delete(account); err != nil {
			t.Errorf("cleanup delete: %v", err)
		}
	})

	runStoreContract(t, store)
}
