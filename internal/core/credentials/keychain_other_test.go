//go:build !darwin

package credentials

import (
	"errors"
	"testing"
)

func TestUnavailableKeychainStore(t *testing.T) {
	store := NewKeychain(KeychainService)
	if store.IsAvailable() {
		t.Fatal("the non-macOS Keychain store reports itself available")
	}

	secret, found, err := store.Read("account")
	if !errors.Is(err, ErrUnavailable) || found || secret != nil {
		t.Errorf("Read = %v, %t, %v; want nil, false, ErrUnavailable", secret, found, err)
	}
	if err := store.Write("account", []byte("secret")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Write = %v, want ErrUnavailable", err)
	}
	if err := store.Delete("account"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Delete = %v, want ErrUnavailable", err)
	}
}
