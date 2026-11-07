//! The shell-wide in-flight counter and profile generation.
//!
//! Every background job takes a generation at spawn and releases its slot when its result has been handled. A
//! profile switch bumps the generation, so a late result can never reach the next profile's pages.
//!
//! It lives outside `shell` so page controllers (and the screenshot harness, which compiles them on their own) can
//! share the same counter.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use finglass_core::app::accounts::Busy;

/// The shell-wide in-flight counter and profile generation.
#[derive(Clone)]
pub(crate) struct JobTracker {
    work: Busy,
    generation: Arc<AtomicU64>,
}

impl JobTracker {
    /// A tracker with no running jobs at generation zero.
    pub(crate) fn new() -> Self {
        Self {
            work: Busy::default(),
            generation: Arc::new(AtomicU64::new(0)),
        }
    }

    /// Marks a job as running and returns the generation it belongs to.
    pub(crate) fn begin(&self) -> u64 {
        self.work.begin();
        self.generation.load(Ordering::SeqCst)
    }

    /// Releases the job's slot.
    pub(crate) fn end(&self) {
        self.work.end();
    }

    /// Whether a job with `generation` may still publish.
    pub(crate) fn is_current(&self, generation: u64) -> bool {
        self.generation.load(Ordering::SeqCst) == generation
    }

    /// Invalidates every job that started before a profile switch.
    pub(crate) fn bump(&self) {
        self.generation.fetch_add(1, Ordering::SeqCst);
    }

    /// Whether any background job is running.
    pub(crate) fn active(&self) -> bool {
        self.work.active()
    }
}
