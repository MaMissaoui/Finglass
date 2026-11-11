// Filesystem helpers for the encrypted database.
package db

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// sqliteHeader is the plain SQLite file header. A SQLCipher file never starts
// with it.
var sqliteHeader = []byte("SQLite format 3\x00")

// tempCounter makes temporary names unique within the process, in addition to
// the pid and the nanosecond timestamp.
var tempCounter atomic.Uint64

// checkPath refuses paths that the driver would parse as DSN parameters.
func checkPath(path string) error {
	if strings.ContainsRune(path, '?') {
		return ErrInvalidPath
	}
	return nil
}

// hasSQLiteHeader reports whether the first 16 bytes are the plain SQLite
// header.
func hasSQLiteHeader(path string) (bool, error) {
	// #nosec G304 -- path is the database path chosen by the caller.
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = file.Close()
	}()

	var header [16]byte
	filled, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}

	return filled == len(header) && bytes.Equal(header[:], sqliteHeader), nil
}

// tempDatabasePath builds a unique temporary path in the same folder as the
// final file.
func tempDatabasePath(path string) string {
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	nanos := time.Now().UnixNano()
	counter := tempCounter.Add(1)

	return filepath.Join(parent, fmt.Sprintf("%s.creating-%d-%d-%d", name, os.Getpid(), nanos, counter))
}

// cleanupTemp removes a temporary database and the side files SQLite may have
// made next to it.
func cleanupTemp(path string) {
	_ = os.Remove(path)

	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

// restrictFilePermissions tightens the file mode to 0600 when group or others
// have any bit.
func restrictFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Mode().Perm()&0o077 != 0 {
		return os.Chmod(path, 0o600)
	}
	return nil
}
