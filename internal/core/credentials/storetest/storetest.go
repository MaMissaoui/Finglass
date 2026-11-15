// Package storetest is the one conformance test every credentials.Store must
// pass: the in-memory store, the macOS Keychain store, Windows Credential
// Manager and the Linux Secret Service.
package storetest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

// Account is the only account key [Run] writes. A real-store test deletes it
// in t.Cleanup.
const Account = "profile#bank-7"

// Run checks the store contract:
//
//   - a missing entry reads as found=false without an error;
//   - Write then Read returns the same bytes;
//   - a second Write replaces the entry;
//   - Delete removes it, and a further Read is absent;
//   - deleting a missing entry succeeds.
//
// Every error is checked for the secrets used here: a store must never put a
// secret in an error string.
func Run(t *testing.T, store credentials.Store) {
	t.Helper()

	if !store.IsAvailable() {
		t.Fatal("the store reports itself unavailable")
	}

	secret := []byte{0x00, 0x01, 0xff, 'g', 'r', 0xc3, 0xbc}
	replacement := []byte("replacement secret é")

	if got, found, err := store.Read(Account); err != nil {
		failWithSecret(t, "initial Read", err, secret, replacement)
	} else if found {
		t.Fatalf("initial Read = %x, want absent", got)
	}

	if err := store.Write(Account, secret); err != nil {
		failWithSecret(t, "Write", err, secret, replacement)
	}
	got, found, err := store.Read(Account)
	if err != nil {
		failWithSecret(t, "Read after Write", err, secret, replacement)
	}
	if !found {
		t.Fatal("Read after Write = absent")
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("Read = %x, want %x", got, secret)
	}
	clear(got)

	if err := store.Write(Account, replacement); err != nil {
		failWithSecret(t, "overwrite", err, secret, replacement)
	}
	got, found, err = store.Read(Account)
	if err != nil {
		failWithSecret(t, "Read after overwrite", err, secret, replacement)
	}
	if !found || !bytes.Equal(got, replacement) {
		t.Fatalf("Read after overwrite = %q, found %t; want %q", got, found, replacement)
	}
	clear(got)

	if err := store.Delete(Account); err != nil {
		failWithSecret(t, "Delete", err, secret, replacement)
	}
	if _, found, err := store.Read(Account); err != nil {
		failWithSecret(t, "Read after Delete", err, secret, replacement)
	} else if found {
		t.Fatal("Read after Delete = present")
	}
	if err := store.Delete(Account); err != nil {
		failWithSecret(t, "deleting an absent entry", err, secret, replacement)
	}
}

// failWithSecret fails with the operation and whether the error leaked one of
// the secrets, never with the error text itself when it did.
func failWithSecret(t *testing.T, operation string, err error, secrets ...[]byte) {
	t.Helper()

	for _, secret := range secrets {
		if len(secret) > 0 && strings.Contains(err.Error(), string(secret)) {
			t.Fatalf("%s failed with an error containing the secret", operation)
		}
	}
	t.Fatalf("%s: %v", operation, err)
}
