package credentials

// unavailableStore is what a system without a protected credential store
// gets: remembering a PIN is not offered, and every call reports that no
// protected store exists.
type unavailableStore struct{}

// NewUnavailable returns a store for a system without a protected credential
// store. Every call reports [ErrUnavailable]; IsAvailable is false.
func NewUnavailable() Store {
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
