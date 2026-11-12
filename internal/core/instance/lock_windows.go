//go:build windows

package instance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive, non-blocking lock on file.
func tryLock(file *os.File) error {
	var overlapped windows.Overlapped

	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrAlreadyRunning
		}
		return fmt.Errorf("lock %s: %w", file.Name(), err)
	}
	return nil
}
