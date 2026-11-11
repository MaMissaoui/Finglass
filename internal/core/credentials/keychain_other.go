//go:build !darwin

package credentials

// unavailableStore is what every non-macOS system gets: remembering a PIN is
// not offered, and every call reports that no protected store exists.
type unavailableStore struct{}

// NewKeychain returns an unavailable store on every operating system without
// the macOS Keychain.
func NewKeychain(_ string) Store {
	return unavailableStore{}
}

// IsAvailable reports that there is no protected store.
func (unavailableStore) IsAvailable() bool {
	return false
}

// Read always reports that the store is unavailable.
func (unavailableStore) Read(_ string) ([]byte, bool, error) {
	return nil, false, ErrUnavailable
}

// Write always reports that the store is unavailable.
func (unavailableStore) Write(_ string, _ []byte) error {
	return ErrUnavailable
}

// Delete always reports that the store is unavailable.
func (unavailableStore) Delete(_ string) error {
	return ErrUnavailable
}
