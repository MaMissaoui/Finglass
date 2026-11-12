package accountsview

import "sync/atomic"

// Busy is the shared count of running busy sections, ported from FinDesk's
// `viewmodel/Busy.kt`.
//
// Sync, import, backup and document work set it while they run. The Accounts
// view refuses to delete an account or a bank connection then, because the
// running work may still insert rows under them.
//
// FinDesk's Busy is a global object. Here it is a cheap shared handle: the app
// creates one per open profile and gives copies to every view and worker, so
// tests (which run in parallel) each get their own. Busy values compare by
// identity, as Rust's Arc::ptr_eq: use == on the handles.
type Busy struct {
	state *busyState
}

type busyState struct {
	count atomic.Uint64
}

// NewBusy returns a fresh busy counter.
func NewBusy() Busy {
	return Busy{state: &busyState{}}
}

func (b *Busy) counter() *atomic.Uint64 {
	if b.state == nil {
		b.state = &busyState{}
	}
	return &b.state.count
}

// Begin marks the start of a busy section.
func (b *Busy) Begin() {
	b.counter().Add(1)
}

// End marks the end of a busy section. An unmatched call never goes below
// zero.
func (b *Busy) End() {
	counter := b.counter()
	for {
		running := counter.Load()
		if running == 0 {
			return
		}
		if counter.CompareAndSwap(running, running-1) {
			return
		}
	}
}

// Active reports whether any busy section is active.
func (b *Busy) Active() bool {
	return b.counter().Load() > 0
}

// Run runs work as one busy section; the section ends also when work panics.
func (b *Busy) Run(work func()) {
	b.Begin()
	defer b.End()
	work()
}
