//go:build unix

package service

import "os"

func secureFile(path string) error {
	return os.Chmod(path, 0o600)
}
