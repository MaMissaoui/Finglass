// Package findeskproc reports whether FinDesk is running on this machine.
//
// FinDesk's packaged launcher is named FinDesk on every platform: Compose
// Desktop's packageName is "FinDesk", macOS's CFBundleExecutable is FinDesk and
// Windows adds .exe. A FinDesk started from Gradle runs as java and is not
// detected; the import's source-changed error remains the backstop.
package findeskproc

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

// Timeout bounds one process-list command.
const Timeout = 2 * time.Second

// State is what the running check found.
type State uint8

const (
	// Unknown means the check could not tell, for example because the command
	// failed, timed out, printed nothing or this OS is unsupported. Callers
	// treat it like Running so a note is never lost.
	Unknown State = iota
	// NotRunning means the process list was read and held no FinDesk launcher.
	NotRunning
	// Running means the process list held FinDesk's launcher.
	Running
)

// String returns the state's name.
func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case NotRunning:
		return "not running"
	default:
		return "unknown"
	}
}

// Checker reports whether FinDesk is running.
type Checker interface {
	Running(ctx context.Context) (State, error)
}

// NewChecker returns the checker for this OS. An unsupported OS always returns
// Unknown.
func NewChecker() Checker {
	return newPlatformChecker()
}

// commandChecker runs one OS process-list command and parses its output.
type commandChecker struct {
	command func(context.Context) *exec.Cmd
	parse   func([]byte) State
}

// Running implements Checker. Every failure is Unknown: an error, a timeout, an
// empty output or a parse failure must never hide the quit note.
func (c commandChecker) Running(ctx context.Context) (State, error) {
	if c.command == nil || c.parse == nil {
		return Unknown, nil
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	command := c.command(ctx)
	// A nil Stdin makes os/exec read from the null device.
	command.Stdin = nil
	// Bounds the wait for the output pipes after the kill, so a child that
	// keeps stdout open cannot hold the call past the timeout.
	command.WaitDelay = time.Second
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard

	if err := command.Run(); err != nil {
		return Unknown, nil
	}
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return Unknown, nil
	}
	return c.parse(stdout.Bytes()), nil
}
