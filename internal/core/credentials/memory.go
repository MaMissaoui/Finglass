package credentials

import "sync"

// MemoryStore is an in-memory store for tests. It behaves like the Keychain
// store: writes replace, missing reads are absent, and deleting a missing entry
// succeeds. It never touches the system Keychain.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string][]byte
}

// NewMemory returns an available, empty in-memory store.
func NewMemory() Store {
	return &MemoryStore{entries: make(map[string][]byte)}
}

// IsAvailable reports that the store can keep secrets for the process lifetime.
func (m *MemoryStore) IsAvailable() bool {
	return true
}

// Read returns a copy of the stored secret, or found=false when absent.
func (m *MemoryStore) Read(account string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	secret, found := m.entries[account]
	if !found {
		return nil, false, nil
	}
	copyOfSecret := make([]byte, len(secret))
	copy(copyOfSecret, secret)
	return copyOfSecret, true, nil
}

// Write stores or replaces a secret.
func (m *MemoryStore) Write(account string, secret []byte) error {
	copyOfSecret := make([]byte, len(secret))
	copy(copyOfSecret, secret)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[account] = copyOfSecret
	return nil
}

// Delete removes a secret. Deleting a missing entry succeeds.
func (m *MemoryStore) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, account)
	return nil
}
