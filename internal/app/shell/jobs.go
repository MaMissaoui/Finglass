package shell

import "sync"

// Jobs is the shell-wide in-flight counter and profile generation.
//
// Every background job takes a generation at spawn and releases its slot when
// its result has been handled. A profile switch bumps the generation, so a late
// result can never reach the next profile's pages.
//
// It is safe for concurrent use. Unlike Rust's JobTracker, Go has no garbage
// collector to close an old database when its last reference disappears, so the
// counter also keeps per-generation in-flight counts: [Jobs.WhenIdle] runs a
// function when the last job of one generation finishes. That is how the
// session closes a torn-down profile's database exactly once.
type Jobs struct {
	mu         sync.Mutex
	generation uint64
	active     map[uint64]int
	total      int
	idle       map[uint64][]func()
}

// Begin marks a job as running and returns the generation it belongs to.
func (j *Jobs) Begin() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.active == nil {
		j.active = make(map[uint64]int)
	}
	generation := j.generation
	j.active[generation]++
	j.total++
	return generation
}

// End releases a slot of the current generation, as Rust's JobTracker::end
// does.
//
// Callers that may outlive a profile switch must use [Jobs.EndGeneration] with
// the generation [Begin] returned: Go needs per-generation counts to close a
// torn-down database when its last job ends, which Rust's one global counter
// cannot express.
func (j *Jobs) End() {
	j.mu.Lock()
	generation := j.generation
	j.mu.Unlock()
	j.EndGeneration(generation)
}

// EndGeneration releases a job's slot in generation.
func (j *Jobs) EndGeneration(generation uint64) {
	j.mu.Lock()
	count := j.active[generation]
	if count == 0 {
		j.mu.Unlock()
		return
	}
	if count > 1 {
		j.active[generation] = count - 1
		j.total--
		j.mu.Unlock()
		return
	}
	delete(j.active, generation)
	j.total--

	callbacks := j.idle[generation]
	delete(j.idle, generation)
	j.mu.Unlock()

	for _, callback := range callbacks {
		callback()
	}
}

// IsCurrent reports whether a job with generation may still publish.
func (j *Jobs) IsCurrent(generation uint64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.generation == generation
}

// Bump invalidates every job that started before a profile switch.
func (j *Jobs) Bump() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.generation++
}

// Generation returns the generation of jobs starting now.
func (j *Jobs) Generation() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.generation
}

// Active reports whether any background job is running.
func (j *Jobs) Active() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.total > 0
}

// InFlight returns how many jobs of generation are running.
func (j *Jobs) InFlight(generation uint64) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.active[generation]
}

// WhenIdle runs callback now when no job of generation is running, otherwise
// when the last one ends. The callback runs without the tracker's lock.
func (j *Jobs) WhenIdle(generation uint64, callback func()) {
	j.mu.Lock()
	if j.active[generation] == 0 {
		j.mu.Unlock()
		callback()
		return
	}
	if j.idle == nil {
		j.idle = make(map[uint64][]func())
	}
	j.idle[generation] = append(j.idle[generation], callback)
	j.mu.Unlock()
}
