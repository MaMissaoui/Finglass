//go:build !windows

package credentials

// absolutePath normalises a POSIX path. The macOS account key must not change,
// so this is the exact algorithm 0.4.3/0.4.4 used.
func absolutePath(path string) string {
	return absolutePOSIXPath(path)
}
