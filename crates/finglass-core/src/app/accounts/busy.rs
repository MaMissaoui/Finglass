//! The busy flag, ported from FinDesk's `viewmodel/Busy.kt`.
//!
//! Sync, import, backup and document work set it while they run. The Accounts view refuses to delete an account or a
//! bank connection then, because the running work may still insert rows under them.
//!
//! FinDesk's `Busy` is a global object. Here it is a cheap shared handle: the app creates one per open profile and
//! gives clones to every view and worker, so tests (which run in parallel) each get their own.

use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

/// A shared count of running busy sections. Two handles are equal when they share the same count.
#[derive(Debug, Clone, Default)]
pub struct Busy(Arc<AtomicUsize>);

impl PartialEq for Busy {
    fn eq(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.0, &other.0)
    }
}

impl Eq for Busy {}

impl Busy {
    /// Marks the start of a busy section.
    pub fn begin(&self) {
        self.0.fetch_add(1, Ordering::SeqCst);
    }

    /// Marks the end of a busy section. An unmatched call never goes below zero.
    pub fn end(&self) {
        let mut running = self.0.load(Ordering::SeqCst);
        while running > 0 {
            match self
                .0
                .compare_exchange(running, running - 1, Ordering::SeqCst, Ordering::SeqCst)
            {
                Ok(_) => break,
                Err(actual) => running = actual,
            }
        }
    }

    /// Whether any busy section is active.
    #[must_use]
    pub fn active(&self) -> bool {
        self.0.load(Ordering::SeqCst) > 0
    }

    /// Runs `work` as one busy section; the section ends also when `work` panics.
    pub fn run<T>(&self, work: impl FnOnce() -> T) -> T {
        struct Section<'a>(&'a Busy);
        impl Drop for Section<'_> {
            fn drop(&mut self) {
                self.0.end();
            }
        }
        self.begin();
        let _section = Section(self);
        work()
    }
}

#[cfg(test)]
mod tests {
    use super::Busy;

    #[test]
    fn clones_share_the_count_and_end_never_goes_below_zero() {
        let busy = Busy::default();
        let other = busy.clone();
        assert!(!busy.active());
        other.begin();
        assert!(busy.active());
        busy.end();
        busy.end();
        assert!(!other.active());
        assert!(busy.run(|| other.active()));
        assert!(!busy.active());
    }
}
