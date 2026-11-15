//go:build darwin

package findeskproc

import (
	"context"
	"os/exec"
)

// newPlatformChecker reads the command names of every process.
func newPlatformChecker() Checker {
	return commandChecker{
		command: func(ctx context.Context) *exec.Cmd {
			// #nosec G204 -- the command name and arguments are fixed by this
			// package; no shell is involved.
			return exec.CommandContext(ctx, "ps", "-A", "-o", "comm=")
		},
		parse: parsePSOutput,
	}
}
