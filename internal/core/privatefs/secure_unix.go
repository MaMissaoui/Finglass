//go:build unix

package privatefs

import (
	"fmt"
	"os"
)

// SecureDir makes path a private folder: mode 0700.
func SecureDir(path string) error {
	// #nosec G302 -- a private folder needs 0700, not a file's 0600.
	return os.Chmod(path, 0o700)
}

// SecureFile makes path a private file: mode 0600.
func SecureFile(path string) error {
	return os.Chmod(path, 0o600)
}

// Unsupported reports whether err means the volume cannot keep private
// permissions. Unix permission bits always exist, so it is never true.
func Unsupported(error) bool { return false }

// Check reports whether path has the private mode for its type: 0700 for a
// folder and 0600 for a file.
func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	want := os.FileMode(0o600)
	if info.IsDir() {
		want = 0o700
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("%s: mode is not private", path)
	}
	return nil
}
