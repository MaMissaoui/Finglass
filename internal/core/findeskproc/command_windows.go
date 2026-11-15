//go:build windows

package findeskproc

import (
	"context"
	"os/exec"
)

// newPlatformChecker reads the image names of every process.
func newPlatformChecker() Checker {
	return commandChecker{
		command: func(ctx context.Context) *exec.Cmd {
			// #nosec G204 -- the command name and arguments are fixed by this
			// package; no shell is involved.
			return exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH")
		},
		parse: parseTasklistOutput,
	}
}
