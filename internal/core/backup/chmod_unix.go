//go:build unix

// Owner-only permissions on Unix.
package backup

import "syscall"

// chmod changes a path's mode. Rust's #[cfg(unix)] permission calls use the
// same system call.
func chmod(path string, mode uint32) error {
	return syscall.Chmod(path, mode)
}
