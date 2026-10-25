// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The size a window had before it was maximized.
//!
//! Electron's `getNormalBounds()` has no Tauri counterpart, so the shell
//! remembers it: every time the window is seen in its normal state, its bounds
//! are kept, and a maximized window is saved with the last of those. Looking is
//! done when the debounced save fires, never per resize event, because a
//! platform may report the maximized size a moment before it reports the state.

use crate::window_state::{Rect, WindowGeometry};
use std::sync::Mutex;

/// Where the window last sat in its normal state.
#[derive(Default)]
pub struct NormalBounds {
    last: Mutex<Option<Rect>>,
}

impl NormalBounds {
    /// Starts from the restored placement, so a window that opens maximized
    /// still has a size to come back to.
    pub fn seeded(bounds: Option<Rect>) -> Self {
        Self {
            last: Mutex::new(bounds),
        }
    }

    /// Looks at the window and answers what to save. `normal` is false while it
    /// is maximized, minimized or full screen, and the last normal bounds are
    /// kept then.
    pub fn look(&self, bounds: Rect, normal: bool, maximized: bool) -> Seen {
        let mut last = self.last.lock().unwrap();
        if normal {
            *last = Some(bounds);
        }
        Seen {
            bounds,
            normal: *last,
            maximized,
        }
    }
}

/// A window as it was seen: enough for `capture_window_state`.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Seen {
    bounds: Rect,
    normal: Option<Rect>,
    maximized: bool,
}

impl WindowGeometry for Seen {
    fn normal_bounds(&self) -> Option<Rect> {
        self.normal
    }
    fn bounds(&self) -> Rect {
        self.bounds
    }
    fn is_maximized(&self) -> bool {
        self.maximized
    }
}

/// Physical pixels to the logical ones the state file holds, so a file means
/// the same on a 1x and a 2x display.
pub fn to_logical(physical: Rect, scale: f64) -> Rect {
    let scale = if scale.is_finite() && scale > 0.0 {
        scale
    } else {
        1.0
    };
    Rect {
        x: physical.x / scale,
        y: physical.y / scale,
        width: physical.width / scale,
        height: physical.height / scale,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::window_state::capture_window_state;

    fn rect(x: f64, y: f64, width: f64, height: f64) -> Rect {
        Rect {
            x,
            y,
            width,
            height,
        }
    }

    #[test]
    fn a_normal_window_is_saved_as_it_is() {
        let tracker = NormalBounds::default();
        let seen = tracker.look(rect(10.0, 20.0, 1200.0, 800.0), true, false);
        let state = capture_window_state(&seen);
        assert_eq!(
            (state.x, state.y, state.width, state.height, state.maximized),
            (Some(10.0), Some(20.0), 1200.0, 800.0, false)
        );
    }

    #[test]
    fn a_maximized_window_is_saved_with_its_bounds_from_before() {
        let tracker = NormalBounds::default();
        tracker.look(rect(10.0, 20.0, 1200.0, 800.0), true, false);
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), false, true);
        let state = capture_window_state(&seen);
        assert_eq!(
            (state.width, state.height, state.maximized),
            (1200.0, 800.0, true)
        );
    }

    #[test]
    fn a_window_never_seen_normal_falls_back_to_its_own_bounds() {
        let tracker = NormalBounds::default();
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), false, true);
        assert_eq!(seen.normal_bounds(), None);
        assert_eq!(seen.bounds(), rect(0.0, 0.0, 2560.0, 1440.0));
        assert!(seen.is_maximized());
    }

    #[test]
    fn a_seed_is_what_an_opening_maximized_window_returns_to() {
        let tracker = NormalBounds::seeded(Some(rect(50.0, 60.0, 1000.0, 700.0)));
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), false, true);
        assert_eq!(seen.normal_bounds(), Some(rect(50.0, 60.0, 1000.0, 700.0)));
    }

    #[test]
    fn logical_bounds_divide_by_the_scale() {
        assert_eq!(
            to_logical(rect(200.0, 100.0, 2400.0, 1600.0), 2.0),
            rect(100.0, 50.0, 1200.0, 800.0)
        );
    }

    #[test]
    fn an_unusable_scale_counts_as_one() {
        let bounds = rect(1.0, 2.0, 3.0, 4.0);
        for scale in [0.0, -1.0, f64::NAN, f64::INFINITY] {
            assert_eq!(to_logical(bounds, scale), bounds);
        }
    }
}
