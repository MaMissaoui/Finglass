//go:build !unix

// Owner-only permissions outside Unix.
package backup

// chmod is a no-op on this platform.
func chmod(string, uint32) error {
	return nil
}
