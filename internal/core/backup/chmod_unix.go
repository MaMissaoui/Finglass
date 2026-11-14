//go:build unix

// Owner-only permissions on Unix.
package backup

import "syscall"

// chmod changes a path's mode with the platform's chmod system call.
func chmod(path string, mode uint32) error {
	return syscall.Chmod(path, mode)
}
