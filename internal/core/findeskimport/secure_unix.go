//go:build unix

package findeskimport

import "os"

func secureFile(path string) error {
	return os.Chmod(path, 0o600)
}
