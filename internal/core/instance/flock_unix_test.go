//go:build unix

package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestTheLockIsFlock proves the lock is a flock(2) lock, which is what Rust's
// File::try_lock uses, rather than an fcntl record lock.
func TestTheLockIsFlock(t *testing.T) {
	p := testPaths(t)

	lock, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()

	file, err := os.Open(filepath.Join(p.Root(), LockFile))
	if err != nil {
		t.Fatalf("Open lock file: %v", err)
	}
	defer func() { _ = file.Close() }()

	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("flock on a second descriptor = %v, want EWOULDBLOCK", err)
	}
}
