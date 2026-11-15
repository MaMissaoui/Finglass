//go:build darwin || windows || linux

package credstore

import (
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/credentials/storetest"
)

// TestSystemStoreConformance runs against the real system store only when the
// person running the tests asks for it explicitly. Editing a credential store
// is never part of the normal suite.
func TestSystemStoreConformance(t *testing.T) {
	if os.Getenv("FINGLASS_KEYCHAIN_TEST") != "1" {
		t.Skip("set FINGLASS_KEYCHAIN_TEST=1 to run against the real system credential store")
	}

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("random service suffix: %v", err)
	}
	service := fmt.Sprintf("%s.test-%x", credentials.KeychainService, random)
	store := NewSystem(service)
	if !store.IsAvailable() {
		t.Fatal("the system credential store is not available")
	}

	t.Cleanup(func() {
		if err := store.Delete(storetest.Account); err != nil {
			t.Errorf("cleanup delete: %v", err)
		}
	})

	storetest.Run(t, store)
}
