package banksync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/MaMissaoui/Finglass/internal/core/credentials"
)

// NoCredentialStore is used where no protected store is offered: the option to
// remember a PIN is not shown.
type NoCredentialStore struct{}

// IsAvailable reports that the system offers no protected store.
func (NoCredentialStore) IsAvailable() bool {
	return false
}

// Read always reports that the key is absent.
func (NoCredentialStore) Read(string) ([]byte, bool, error) {
	return nil, false, nil
}

// Write always fails because there is no store.
func (NoCredentialStore) Write(string, []byte) error {
	return errors.New("no credential store available")
}

// Delete always succeeds; there is no entry to remove.
func (NoCredentialStore) Delete(string) error {
	return nil
}

// MemoryCredentialStore is an in-memory store for tests. It can simulate a
// locked or denied store without ever touching the system keychain.
type MemoryCredentialStore struct {
	mu          sync.RWMutex
	entries     map[string][]byte
	available   bool
	failReads   bool
	failWrites  bool
	failDeletes bool
}

// NewMemoryCredentialStore returns an available, empty store.
func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{
		entries:   make(map[string][]byte),
		available: true,
	}
}

// NewUnavailableMemoryCredentialStore returns a store that reports itself
// unavailable.
func NewUnavailableMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{entries: make(map[string][]byte)}
}

// FailingReads makes every later read fail, like a locked keychain.
func (m *MemoryCredentialStore) FailingReads() *MemoryCredentialStore {
	m.SetFailingReads(true)
	return m
}

// SetFailingReads makes every later read fail or work again.
func (m *MemoryCredentialStore) SetFailingReads(value bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failReads = value
}

// FailingWrites makes every later write fail, like a denied keychain.
func (m *MemoryCredentialStore) FailingWrites() *MemoryCredentialStore {
	m.SetFailingWrites(true)
	return m
}

// SetFailingWrites makes every later write fail or work again.
func (m *MemoryCredentialStore) SetFailingWrites(value bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failWrites = value
}

// FailingDeletes makes every later delete fail, like a denied keychain.
func (m *MemoryCredentialStore) FailingDeletes() *MemoryCredentialStore {
	m.SetFailingDeletes(true)
	return m
}

// SetFailingDeletes makes every later delete fail or work again.
func (m *MemoryCredentialStore) SetFailingDeletes(value bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failDeletes = value
}

// Contains reports whether the store holds an entry for account.
func (m *MemoryCredentialStore) Contains(account string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, found := m.entries[account]
	return found
}

// Len returns the number of stored entries.
func (m *MemoryCredentialStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// IsEmpty reports whether the store holds no entries.
func (m *MemoryCredentialStore) IsEmpty() bool {
	return m.Len() == 0
}

// String never prints stored secrets.
func (m *MemoryCredentialStore) String() string {
	return fmt.Sprintf("MemoryCredentialStore{entries:%d available:%t}", m.Len(), m.IsAvailable())
}

// GoString never prints stored secrets.
func (m *MemoryCredentialStore) GoString() string {
	return m.String()
}

// Format never prints stored secrets, for every fmt verb.
func (m *MemoryCredentialStore) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(m.String()))
}

// IsAvailable reports whether this store can keep secrets.
func (m *MemoryCredentialStore) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available
}

// Read returns a copy of the stored secret, or found=false when absent.
func (m *MemoryCredentialStore) Read(account string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.failReads {
		return nil, false, errors.New("keychain locked")
	}
	secret, found := m.entries[account]
	if !found {
		return nil, false, nil
	}
	return append([]byte(nil), secret...), true, nil
}

// Write stores or replaces a secret.
func (m *MemoryCredentialStore) Write(account string, secret []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWrites {
		return errors.New("keychain denied")
	}
	if m.entries == nil {
		m.entries = make(map[string][]byte)
	}
	m.entries[account] = append([]byte(nil), secret...)
	return nil
}

// Delete removes a secret. Deleting a missing entry succeeds.
func (m *MemoryCredentialStore) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDeletes {
		return errors.New("keychain denied")
	}
	delete(m.entries, account)
	return nil
}

// PinKey is the credential-store key of a bank access's remembered PIN.
//
// The profile folder makes it profile-specific: two profiles with the same bank
// id never share a PIN. Callers must pass the canonical folder from
// paths.AppPaths.ProfileDir; the default profile is the root and named profiles
// live below it, so their keys never collide. The absolute form matches
// FinDesk's pinKey(dataDir, id) so profile deletion can find the entries.
func PinKey(profileDir string, bankID int64) string {
	return credentials.AccountFor(profileDir, bankID)
}

// PinMarkerPath is the empty marker file that records a remembered PIN for a
// profile and bank access.
func PinMarkerPath(profileDir string, bankID int64) string {
	return filepath.Join(profileDir, "credentials", "bank-"+strconv.FormatInt(bankID, 10))
}

// ForgetPinsForProfile forgets a profile's remembered PINs before its folder is
// moved.
//
// Credentials live in the system store, keyed by the profile's data directory,
// so moving the folder alone would leave them behind. The markers under
// credentials/ name the banks even when the profile's database is locked. A
// stray marker is harmless: deleting a missing entry succeeds and the marker is
// removed.
func ForgetPinsForProfile(store credentials.Store, profileDir string) {
	if !store.IsAvailable() {
		return
	}
	credentialsDir := filepath.Join(profileDir, "credentials")
	entries, err := os.ReadDir(credentialsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		rest, found := strings.CutPrefix(name, "bank-")
		if !found {
			continue
		}
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			continue
		}
		if store.Delete(PinKey(profileDir, id)) == nil {
			_ = os.Remove(filepath.Join(credentialsDir, name))
		}
	}
}

// The credential-store port and its fakes used in tests.
var (
	_ credentials.Store = NoCredentialStore{}
	_ credentials.Store = (*MemoryCredentialStore)(nil)
)
