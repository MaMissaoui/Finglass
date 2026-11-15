//go:build windows

package credentials

import (
	"path/filepath"
	"strings"
)

// absolutePath normalises a Windows path.
//
// A Windows path (a drive letter, a UNC prefix or a backslash) becomes an
// absolute, cleaned path; without this, `C:\Users\x\...` was treated as a
// relative POSIX path and joined to the working directory. A path in the
// POSIX form keeps the POSIX algorithm, so account keys written by other
// systems, which FinDesk also reads, do not change.
func absolutePath(path string) string {
	if path == "" {
		return path
	}
	if !isWindowsPath(path) {
		return absolutePOSIXPath(path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(absolute)
}

// isWindowsPath reports whether path uses Windows syntax.
func isWindowsPath(path string) bool {
	if strings.Contains(path, `\`) {
		return true
	}
	return len(path) >= 2 && path[1] == ':'
}
