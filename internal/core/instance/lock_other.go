//go:build !unix && !windows

package instance

import (
	"fmt"
	"os"
)

// tryLock reports that single-instance locking is not supported here.
func tryLock(file *os.File) error {
	return fmt.Errorf("single-instance locking is unsupported on this platform: %s", file.Name())
}
