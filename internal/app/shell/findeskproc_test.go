package shell

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/findeskproc"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

type countingFindeskChecker struct {
	mu    sync.Mutex
	state findeskproc.State
	err   error
	calls int
}

func (c *countingFindeskChecker) Running(context.Context) (findeskproc.State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.state, c.err
}

func (c *countingFindeskChecker) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitForFindeskCalls(t *testing.T, checker *countingFindeskChecker, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if checker.count() >= want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %d running checks, got %d", want, checker.count())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestQuitFinDeskNoteIsHiddenOnlyOnANegativeCheck(t *testing.T) {
	tests := []struct {
		name  string
		state findeskproc.State
		err   error
		want  bool
	}{
		{"running", findeskproc.Running, nil, true},
		{"unknown", findeskproc.Unknown, nil, true},
		{"not running", findeskproc.NotRunning, nil, false},
		{"failed check", findeskproc.Unknown, errors.New("no process list"), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newTestSession(t, testPaths(t))
			checker := &countingFindeskChecker{state: test.state, err: test.err}
			session.SetFindeskProcessChecker(checker)

			// The initial cache is Unknown, so the note is visible at once.
			if !session.QuitFinDeskVisible() {
				t.Fatal("the note was hidden before any check")
			}
			waitForFindeskCalls(t, checker, 1)

			deadline := time.Now().Add(10 * time.Second)
			for {
				if session.QuitFinDeskVisible() == test.want {
					return
				}
				if !time.Now().Before(deadline) {
					t.Fatalf("visible = %v, want %v", !test.want, test.want)
				}
				time.Sleep(2 * time.Millisecond)
			}
		})
	}
}

func TestQuitFinDeskCheckIsCachedForFiveSeconds(t *testing.T) {
	now := testNow
	appPaths := testPaths(t)
	session := newSession(
		appPaths,
		nil,
		func() time.Time { return now },
		testToday,
		testGetenv,
		i18n.En,
	)
	t.Cleanup(func() { _ = session.Shutdown() })

	checker := &countingFindeskChecker{state: findeskproc.NotRunning}
	session.SetFindeskProcessChecker(checker)

	if !session.QuitFinDeskVisible() {
		t.Fatal("the initial Unknown state must show the note")
	}
	waitForFindeskCalls(t, checker, 1)
	deadline := time.Now().Add(10 * time.Second)
	for session.QuitFinDeskVisible() {
		if !time.Now().Before(deadline) {
			t.Fatal("the note stayed visible after a NotRunning check")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := checker.count(); got != 1 {
		t.Fatalf("checks after the first result = %d, want 1", got)
	}

	now = now.Add(4 * time.Second)
	if session.QuitFinDeskVisible() {
		t.Fatal("the note reappeared inside the cache window")
	}
	time.Sleep(10 * time.Millisecond)
	if got := checker.count(); got != 1 {
		t.Fatalf("checks at 4 s = %d, want the cached result", got)
	}

	now = now.Add(2 * time.Second)
	if session.QuitFinDeskVisible() {
		t.Fatal("the stale result was not the last positive NotRunning")
	}
	waitForFindeskCalls(t, checker, 2)
}
