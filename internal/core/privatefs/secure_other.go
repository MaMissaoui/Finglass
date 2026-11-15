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

// Unsupported reports whether err means the volume cannot keep private
// permissions. It is never true here, where the calls are no-ops.
func Unsupported(error) bool { return false }
