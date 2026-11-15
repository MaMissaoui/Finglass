package shell

import (
	"context"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/findeskproc"
)

// findeskProcessTTL is how long a running-check result is reused. The views
// ask again every 5 s while they are open.
const findeskProcessTTL = 5 * time.Second

// SetFindeskProcessChecker replaces the running check. Production leaves the
// real checker from newSession; tests inject a fake so no test runs ps.
func (s *Session) SetFindeskProcessChecker(checker findeskproc.Checker) {
	if checker == nil {
		checker = findeskproc.NewChecker()
	}

	s.mu.Lock()
	s.findeskProcGeneration++
	s.findeskProc = checker
	s.findeskProcState = findeskproc.Unknown
	s.findeskProcAt = time.Time{}
	s.findeskProcChecking = false
	s.mu.Unlock()
}

// QuitFinDeskVisible reports whether the "Quit FinDesk first." note belongs on
// screen. Only a positive NotRunning hides it; Running and Unknown show it.
//
// A stale cache starts a background check and returns the previous state at
// once, so a DTO build never runs ps on the UI thread.
func (s *Session) QuitFinDeskVisible() bool {
	return s.findeskProcessState() != findeskproc.NotRunning
}

func (s *Session) findeskProcessState() findeskproc.State {
	s.mu.Lock()
	state := s.findeskProcState
	checker := s.findeskProc
	generation := s.findeskProcGeneration
	stale := s.findeskProcAt.IsZero() ||
		s.clock().Sub(s.findeskProcAt) >= findeskProcessTTL
	if !stale || s.findeskProcChecking || checker == nil {
		s.mu.Unlock()
		return state
	}
	s.findeskProcChecking = true
	s.mu.Unlock()

	go s.refreshFindeskProcess(generation, checker)
	return state
}

func (s *Session) refreshFindeskProcess(generation uint64, checker findeskproc.Checker) {
	ctx, cancel := context.WithTimeout(context.Background(), findeskproc.Timeout)
	defer cancel()

	state, err := checker.Running(ctx)
	if err != nil {
		state = findeskproc.Unknown
	}
	now := s.clock()

	s.mu.Lock()
	if generation != s.findeskProcGeneration || s.shutDown {
		s.mu.Unlock()
		return
	}
	changed := s.findeskProcState != state
	s.findeskProcState = state
	s.findeskProcAt = now
	s.findeskProcChecking = false
	s.mu.Unlock()

	// Only a changed state is news; an unchanged one would make every poll
	// re-pull the shell and Settings.
	if !changed {
		return
	}
	s.notify(Change{Page: "shell"})
	s.notify(Change{Page: "settings"})
}
