//go:build linux

package credstore

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
	"github.com/MaMissaoui/Finglass/internal/core/credentials/storetest"
)

// linuxTestStore returns a store with a random service name, gated like the
// conformance test: it only runs when the person asks for a real store. The
// caller's cleanup deletes the one item it writes.
func linuxTestStore(t *testing.T, now func() time.Time) *linuxStore {
	t.Helper()

	if os.Getenv("FINGLASS_KEYCHAIN_TEST") != "1" {
		t.Skip("set FINGLASS_KEYCHAIN_TEST=1 to run against the real system credential store")
	}

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("random service suffix: %v", err)
	}
	service := fmt.Sprintf("%s.linux-test-%x", credentials.KeychainService, random)
	store := newStore(service, now)
	t.Cleanup(func() {
		if err := store.Delete(storetest.Account); err != nil {
			t.Errorf("cleanup delete: %v", err)
		}
	})
	return store
}

// fdCount counts the process's open file descriptors.
func fdCount(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	return len(entries)
}

// TestLinuxStoreKeepsConnection forces many session resets on a live bus
// connection. The client pointer must survive and the descriptor count must
// not grow.
func TestLinuxStoreKeepsConnection(t *testing.T) {
	store := linuxTestStore(t, time.Now)
	if !store.IsAvailable() {
		t.Fatal("the system credential store is not available")
	}

	beforeClient := store.client
	beforeFDs := fdCount(t)

	for i := 0; i < 50; i++ {
		store.resetSessionLocked()
		store.dropClientIfClosedLocked(errors.New("not a closed connection"))
		if !store.IsAvailable() {
			t.Fatal("the store is unavailable after a session reset")
		}
	}

	afterFDs := fdCount(t)
	if store.client != beforeClient {
		t.Fatal("the bus client was replaced although its connection was live")
	}
	if grew := afterFDs - beforeFDs; grew > 2 {
		t.Fatalf("open file descriptors grew by %d (from %d to %d)", grew, beforeFDs, afterFDs)
	}
}

// TestLinuxStoreTimeoutKeepsClient makes one deadline fail: IsAvailable must
// return quickly, keep the client, and recover once the cache expires.
func TestLinuxStoreTimeoutKeepsClient(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	store := linuxTestStore(t, func() time.Time { return now })
	if !store.IsAvailable() {
		t.Fatal("the system credential store is not available")
	}
	client := store.client

	store.callTimeout = time.Nanosecond
	start := time.Now()
	if store.IsAvailable() {
		t.Fatal("IsAvailable succeeded with a 1ns call timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("IsAvailable took %s with a 1ns timeout", elapsed)
	}
	if store.client != client {
		t.Fatal("a timeout replaced the bus client")
	}

	store.callTimeout = 2 * time.Second
	now = now.Add(6 * time.Second)
	if !store.IsAvailable() {
		t.Fatal("the store did not recover after the cache expired")
	}
}

// TestLinuxStoreAvailabilityCache checks that an unavailable result is cached
// for five seconds and that the injected clock expires it.
func TestLinuxStoreAvailabilityCache(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	store := linuxTestStore(t, func() time.Time { return now })
	if !store.IsAvailable() {
		t.Fatal("the system credential store is not available")
	}

	store.callTimeout = time.Nanosecond
	if store.IsAvailable() {
		t.Fatal("IsAvailable succeeded with a 1ns call timeout")
	}
	store.callTimeout = 2 * time.Second

	now = now.Add(4 * time.Second)
	if store.IsAvailable() {
		t.Fatal("the cache expired before five seconds")
	}

	now = now.Add(2 * time.Second)
	if !store.IsAvailable() {
		t.Fatal("the cache did not expire after six seconds")
	}
}

// TestLinuxStoreWriteIgnoresAvailabilityCache checks that a write still tries
// the service after an unavailable result, and that success clears the cache.
func TestLinuxStoreWriteIgnoresAvailabilityCache(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	store := linuxTestStore(t, func() time.Time { return now })
	if !store.IsAvailable() {
		t.Fatal("the system credential store is not available")
	}

	store.callTimeout = time.Nanosecond
	if store.IsAvailable() {
		t.Fatal("IsAvailable succeeded with a 1ns call timeout")
	}
	store.callTimeout = 2 * time.Second
	if !now.Before(store.unavailableUntil) {
		t.Fatal("the unavailable result was not cached")
	}

	secret := []byte("secret written through the availability cache")
	if err := store.Write(storetest.Account, secret); err != nil {
		t.Fatalf("Write after a cached unavailable result: %v", err)
	}
	if !store.unavailableUntil.IsZero() {
		t.Fatal("a successful Write did not clear the availability cache")
	}

	got, found, err := store.Read(storetest.Account)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatal("Read after Write = absent")
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("Read = %q, want %q", got, secret)
	}
}
