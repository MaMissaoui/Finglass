//go:build !darwin && !windows && !linux

package credstore

import (
	"errors"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

func TestNewSystemIsUnavailable(t *testing.T) {
	store := NewSystem(credentials.KeychainService)
	if store.IsAvailable() {
		t.Fatal("the store reports itself available")
	}
	if _, found, err := store.Read("account"); !errors.Is(err, credentials.ErrUnavailable) || found {
		t.Errorf("Read = found %t, err %v; want ErrUnavailable", found, err)
	}
	if err := store.Write("account", []byte("secret")); !errors.Is(err, credentials.ErrUnavailable) {
		t.Errorf("Write = %v, want ErrUnavailable", err)
	}
	if err := store.Delete("account"); !errors.Is(err, credentials.ErrUnavailable) {
		t.Errorf("Delete = %v, want ErrUnavailable", err)
	}
}
