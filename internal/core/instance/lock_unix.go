//go:build unix

package instance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive, non-blocking flock(2) on file.
//
// flock is deliberate: it is used on macOS and Linux, and fcntl locks would not
// see a lock taken by an earlier Finglass version.
func tryLock(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrAlreadyRunning
		}
		return fmt.Errorf("lock %s: %w", file.Name(), err)
	}
	return nil
}
