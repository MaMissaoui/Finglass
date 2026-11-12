//go:build !unix

// Owner-only permissions outside Unix.
package backup

// chmod is a no-op where Rust's #[cfg(unix)] calls do nothing either.
func chmod(string, uint32) error {
	return nil
}
