// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"context"
	"time"
)

// Transport carries one FinTS message to the bank and returns the raw answer.
//
// The request holds the PIN and, for HKTAN process 2, the TAN. An
// implementation must not keep, copy, log or retain the request after Exchange
// returns; the dialog wipes the request immediately after the call. Base64
// encoding and HTTP belong to a later slice.
type Transport interface {
	Exchange(ctx context.Context, request []byte) ([]byte, error)
}

// Sleeper waits between decoupled polls.
//
// The dialog injects it so tests can record durations without sleeping. A
// production implementation must return promptly with ctx's error when ctx is
// done.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// realSleeper is the production Sleeper. It honours ctx.
type realSleeper struct{}

// Sleep waits for d or until ctx is done.
func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
