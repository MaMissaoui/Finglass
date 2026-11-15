//go:build !unix && !windows

package privatefs

// SecureDir is a no-op on this platform.
func SecureDir(string) error {
	return nil
}

// SecureFile is a no-op on this platform.
func SecureFile(string) error {
	return nil
}

// Check is a no-op on this platform.
func Check(string) error {
	return nil
}
