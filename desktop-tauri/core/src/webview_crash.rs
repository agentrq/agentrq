// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What to do when the webview's own process dies.
//!
//! Electron's `network-service.js` handled Chromium's network process dying
//! and restarted the event stream; here the network is in-process, so that
//! failure cannot happen. What can is the page's process: the window goes blank
//! and nothing the user does brings it back. Where the platform reports it, the
//! window is reloaded.

use std::sync::Mutex;
use std::time::{Duration, Instant};

/// Why the page's process ended, as far as the platforms say.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Crash {
    /// It crashed, or stopped answering.
    Failed,
    /// The system ended it for using too much memory.
    OutOfMemory,
    /// The shell asked for it to end (closing the window): not a crash.
    Requested,
}

/// Whether a reload is the right answer.
pub fn should_reload(crash: Crash) -> bool {
    !matches!(crash, Crash::Requested)
}

/// A page that dies as it loads would otherwise be reloaded for ever.
pub const MAX_RELOADS: usize = 3;
pub const RELOAD_WINDOW: Duration = Duration::from_secs(60);

/// Allows `MAX_RELOADS` reloads in any `RELOAD_WINDOW`.
#[derive(Default)]
pub struct ReloadGuard {
    recent: Mutex<Vec<Instant>>,
}

impl ReloadGuard {
    /// Whether to reload now, counting it when so.
    pub fn allow(&self, crash: Crash, now: Instant) -> bool {
        if !should_reload(crash) {
            return false;
        }
        let mut recent = self.recent.lock().unwrap();
        recent.retain(|at| now.duration_since(*at) < RELOAD_WINDOW);
        if recent.len() >= MAX_RELOADS {
            return false;
        }
        recent.push(now);
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_crash_and_a_memory_kill_reload_but_a_requested_end_does_not() {
        assert!(should_reload(Crash::Failed));
        assert!(should_reload(Crash::OutOfMemory));
        assert!(!should_reload(Crash::Requested));
    }

    #[test]
    fn a_requested_end_is_never_allowed_and_is_not_counted() {
        let guard = ReloadGuard::default();
        let now = Instant::now();
        for _ in 0..10 {
            assert!(!guard.allow(Crash::Requested, now));
        }
        assert!(guard.allow(Crash::Failed, now));
    }

    #[test]
    fn a_page_that_keeps_dying_is_given_up_on() {
        let guard = ReloadGuard::default();
        let now = Instant::now();
        for _ in 0..MAX_RELOADS {
            assert!(guard.allow(Crash::Failed, now));
        }
        assert!(!guard.allow(Crash::Failed, now));
    }

    #[test]
    fn the_count_starts_again_after_a_quiet_minute() {
        let guard = ReloadGuard::default();
        let start = Instant::now();
        for _ in 0..MAX_RELOADS {
            assert!(guard.allow(Crash::OutOfMemory, start));
        }
        assert!(guard.allow(Crash::Failed, start + RELOAD_WINDOW));
    }
}
