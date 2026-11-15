//go:build !darwin && !linux && !windows

package findeskproc

import "context"

// unknownChecker is the fallback on an OS without a process-list command.
type unknownChecker struct{}

// Running implements Checker and always reports Unknown.
func (unknownChecker) Running(context.Context) (State, error) {
	return Unknown, nil
}

// newPlatformChecker returns the fallback checker.
func newPlatformChecker() Checker {
	return unknownChecker{}
}
