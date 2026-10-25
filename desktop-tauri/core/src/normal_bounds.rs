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

    /// Looks at the window and answers what to save. The bounds count as the
    /// window's normal ones only while it is none of maximized, full screen or
    /// minimized; the last normal bounds are kept otherwise.
    pub fn look(&self, bounds: Rect, state: Shown) -> Seen {
        let mut last = self.last.lock().unwrap();
        if !(state.maximized || state.full_screen || state.minimized) {
            *last = Some(bounds);
        }
        Seen {
            bounds,
            normal: *last,
            maximized: state.maximized,
        }
    }
}

/// What the window was doing when it was looked at.
#[derive(Debug, Clone, Copy, Default)]
pub struct Shown {
    pub maximized: bool,
    pub full_screen: bool,
    pub minimized: bool,
}

/// Where a window was built to sit, for [`NormalBounds::seeded`]: the size is
/// the one it was built with, the position its own when it was not placed
/// (the window manager centred it), and nothing when neither is known.
pub fn seed(
    placed: (Option<f64>, Option<f64>),
    size: (f64, f64),
    actual: Option<Rect>,
) -> Option<Rect> {
    let x = placed.0.or(actual.map(|r| r.x))?;
    let y = placed.1.or(actual.map(|r| r.y))?;
    Some(Rect {
        x,
        y,
        width: size.0,
        height: size.1,
    })
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

    const MAX: Shown = Shown {
        maximized: true,
        full_screen: false,
        minimized: false,
    };

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
        let seen = tracker.look(rect(10.0, 20.0, 1200.0, 800.0), Shown::default());
        let state = capture_window_state(&seen);
        assert_eq!(
            (state.x, state.y, state.width, state.height, state.maximized),
            (Some(10.0), Some(20.0), 1200.0, 800.0, false)
        );
    }

    #[test]
    fn a_maximized_window_is_saved_with_its_bounds_from_before() {
        let tracker = NormalBounds::default();
        tracker.look(rect(10.0, 20.0, 1200.0, 800.0), Shown::default());
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), MAX);
        let state = capture_window_state(&seen);
        assert_eq!(
            (state.width, state.height, state.maximized),
            (1200.0, 800.0, true)
        );
    }

    #[test]
    fn a_window_never_seen_normal_falls_back_to_its_own_bounds() {
        let tracker = NormalBounds::default();
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), MAX);
        assert_eq!(seen.normal_bounds(), None);
        assert_eq!(seen.bounds(), rect(0.0, 0.0, 2560.0, 1440.0));
        assert!(seen.is_maximized());
    }

    #[test]
    fn a_seed_is_what_an_opening_maximized_window_returns_to() {
        let tracker = NormalBounds::seeded(Some(rect(50.0, 60.0, 1000.0, 700.0)));
        let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), MAX);
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

    #[test]
    fn a_window_is_settled_only_when_it_is_none_of_maximized_full_screen_or_minimized() {
        let tracker = NormalBounds::default();
        let normal = rect(10.0, 20.0, 1200.0, 800.0);
        tracker.look(normal, Shown::default());
        for shown in [
            Shown {
                maximized: true,
                ..Shown::default()
            },
            Shown {
                full_screen: true,
                ..Shown::default()
            },
            Shown {
                minimized: true,
                ..Shown::default()
            },
        ] {
            let seen = tracker.look(rect(0.0, 0.0, 2560.0, 1440.0), shown);
            assert_eq!(seen.normal_bounds(), Some(normal), "{shown:?}");
        }
        // Full screen is not maximized: it is saved as the normal window it was.
        let seen = tracker.look(
            rect(0.0, 0.0, 2560.0, 1440.0),
            Shown {
                full_screen: true,
                ..Shown::default()
            },
        );
        assert!(!seen.is_maximized());
    }

    #[test]
    fn a_window_is_seeded_with_the_size_it_was_built_with() {
        let at = |x, y| rect(x, y, 2560.0, 1440.0);
        let built = (1200.0, 800.0);
        // Placed: the clamped placement, whatever the window reports.
        assert_eq!(
            seed((Some(5.0), Some(6.0)), built, Some(at(0.0, 0.0))),
            Some(rect(5.0, 6.0, 1200.0, 800.0))
        );
        // Centred by the window manager: the position it took.
        assert_eq!(
            seed((None, None), built, Some(at(70.0, 80.0))),
            Some(rect(70.0, 80.0, 1200.0, 800.0))
        );
        assert_eq!(seed((None, Some(6.0)), built, None), None);
        assert_eq!(seed((Some(5.0), None), built, None), None);
        assert_eq!(seed((None, None), built, None), None);
    }
}
