// Package instance holds the single-instance lock for a data folder.
//
// Two Finglass processes on the same data root are unsafe: startup can apply a
// staged restore while the other process still has the database open. [Acquire]
// takes an exclusive lock on <root>/finglass.lock before startup touches
// anything, and the lock lives until [Lock.Release] or process exit.
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

// LockFile is the name of the lock file inside the data root.
const LockFile = "finglass.lock"

// ErrAlreadyRunning is returned when another process, or another handle in
// this process, already holds the lock.
var ErrAlreadyRunning = errors.New("another Finglass instance is already using this data folder")

// Lock is an exclusive lock on a data root.
//
// The lock file stays on disk after release; it holds no data.
//
// Keep the *Lock referenced until the process quits and call [Lock.Release]
// then. Unlike Rust's value-scoped lock, an unreachable *Lock lets the garbage
// collector close the file at some later point, which silently drops the lock
// and lets a second instance start.
type Lock struct {
	file *os.File
}

// Acquire creates the data root if needed, opens <root>/finglass.lock and tries
// to take an exclusive lock.
//
// The lock covers every profile in the data root, so a second process on the
// same FINGLASS_HOME fails with [ErrAlreadyRunning] even while the first one is
// only showing the unlock window.
func Acquire(p paths.AppPaths) (*Lock, error) {
	if err := paths.EnsureDir(p.Root()); err != nil {
		return nil, fmt.Errorf("create data folder: %w", err)
	}

	// #nosec G703 -- the lock file path is built from the resolved data
	// root, not from user input.
	file, err := os.OpenFile(
		filepath.Join(p.Root(), LockFile),
		os.O_RDWR|os.O_CREATE,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := tryLock(file); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &Lock{file: file}, nil
}

// Release closes the lock file and releases the lock. It is safe to call more
// than once.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	file := l.file
	l.file = nil

	if err := file.Close(); err != nil {
		return fmt.Errorf("close lock file: %w", err)
	}
	return nil
}
