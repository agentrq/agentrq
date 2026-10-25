// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Zoom and full screen, for the title bar's window menu on macOS.
//!
//! The View menu already does both through the platform's own roles; this is
//! the same thing reachable from a button, answering with the state the menu
//! should show afterwards.

/// The steps a browser zooms through, so − and + land where people expect.
pub const ZOOM_PERCENTS: [u32; 17] = [
    25, 33, 50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200, 250, 300, 400, 500,
];

/// Which way a zoom button moves.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ZoomDirection {
    In,
    Out,
    Reset,
}

/// What the window menu shows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ViewState {
    pub zoom: u32,
    pub full_screen: bool,
}

/// The zoom one step in `direction` from `percent`.
///
/// A zoom between two steps, set by ⌘ and the scroll wheel, which do not use
/// this list, moves to the neighbouring step rather than skipping one.
pub fn next_zoom(percent: u32, direction: ZoomDirection) -> u32 {
    match direction {
        ZoomDirection::In => ZOOM_PERCENTS
            .iter()
            .copied()
            .find(|p| *p > percent)
            .unwrap_or(ZOOM_PERCENTS[ZOOM_PERCENTS.len() - 1]),
        ZoomDirection::Out => ZOOM_PERCENTS
            .iter()
            .rev()
            .copied()
            .find(|p| *p < percent)
            .unwrap_or(ZOOM_PERCENTS[0]),
        ZoomDirection::Reset => 100,
    }
}

/// A window, as far as zoom and full screen are concerned.
pub trait ViewWindow {
    fn zoom_factor(&self) -> f64;
    fn set_zoom_factor(&self, factor: f64);
    fn is_full_screen(&self) -> bool;
    fn set_full_screen(&self, full_screen: bool);
}

/// A window that can be maximized, for the Window menu's Zoom on Windows and
/// Linux, where it is a toggle.
pub trait MaximizeWindow {
    fn is_maximized(&self) -> bool;
    fn maximize(&self);
    fn unmaximize(&self);
}

/// Maximizes a window that is not, and restores one that is.
pub fn toggle_maximize(window: &dyn MaximizeWindow) {
    if window.is_maximized() {
        window.unmaximize();
    } else {
        window.maximize();
    }
}

/// JS's `Math.round`, which rounds halves toward +infinity.
fn round(value: f64) -> f64 {
    (value + 0.5).floor()
}

pub fn view_state(window: &dyn ViewWindow) -> ViewState {
    ViewState {
        zoom: round(window.zoom_factor() * 100.0) as u32,
        full_screen: window.is_full_screen(),
    }
}

pub fn zoom_window(window: &dyn ViewWindow, direction: ZoomDirection) -> ViewState {
    window.set_zoom_factor(f64::from(next_zoom(view_state(window).zoom, direction)) / 100.0);
    view_state(window)
}

/// Answers with the state asked for, not the window's: on macOS the switch is
/// animated, and `is_full_screen()` still reports the old value until it ends.
pub fn toggle_full_screen(window: &dyn ViewWindow) -> ViewState {
    let full_screen = !window.is_full_screen();
    window.set_full_screen(full_screen);
    ViewState {
        full_screen,
        ..view_state(window)
    }
}

impl ZoomDirection {
    /// What the title bar sends: `in`, `out`, or anything else for a reset.
    pub fn parse(direction: &str) -> Self {
        match direction {
            "in" => Self::In,
            "out" => Self::Out,
            _ => Self::Reset,
        }
    }
}

impl ViewState {
    /// The shape the renderer reads: `{ zoom, fullScreen }`.
    pub fn to_json(self) -> serde_json::Value {
        serde_json::json!({ "zoom": self.zoom, "fullScreen": self.full_screen })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;

    /// A window whose full-screen flag, like macOS's, only changes after the animation.
    struct FakeWindow {
        factor: Cell<f64>,
        full_screen: Cell<bool>,
        requested: Cell<Option<bool>>,
    }

    fn fake_window(factor: f64, full_screen: bool) -> FakeWindow {
        FakeWindow {
            factor: Cell::new(factor),
            full_screen: Cell::new(full_screen),
            requested: Cell::new(None),
        }
    }

    impl ViewWindow for FakeWindow {
        fn zoom_factor(&self) -> f64 {
            self.factor.get()
        }
        fn set_zoom_factor(&self, factor: f64) {
            self.factor.set(factor);
        }
        fn is_full_screen(&self) -> bool {
            self.full_screen.get()
        }
        fn set_full_screen(&self, full_screen: bool) {
            self.requested.set(Some(full_screen));
        }
    }

    use ZoomDirection::{In, Out, Reset};

    // nextZoom

    #[test]
    fn steps_through_the_list() {
        assert_eq!(next_zoom(100, In), 110);
        assert_eq!(next_zoom(100, Out), 90);
        assert_eq!(next_zoom(67, In), 75);
    }

    #[test]
    fn lands_on_the_neighbouring_step_from_between_two() {
        assert_eq!(next_zoom(105, In), 110);
        assert_eq!(next_zoom(105, Out), 100);
    }

    #[test]
    fn stops_at_either_end() {
        assert_eq!(next_zoom(ZOOM_PERCENTS[ZOOM_PERCENTS.len() - 1], In), 500);
        assert_eq!(next_zoom(ZOOM_PERCENTS[0], Out), 25);
    }

    #[test]
    fn resets_to_100_percent() {
        assert_eq!(next_zoom(250, Reset), 100);
    }

    // viewState

    #[test]
    fn reads_the_zoom_as_a_whole_percentage() {
        assert_eq!(
            view_state(&fake_window(1.1000000001, true)),
            ViewState {
                zoom: 110,
                full_screen: true
            }
        );
    }

    // zoomWindow

    #[test]
    fn sets_the_next_step_and_answers_with_it() {
        let window = fake_window(1.25, false);
        assert_eq!(
            zoom_window(&window, In),
            ViewState {
                zoom: 150,
                full_screen: false
            }
        );
        assert_eq!(window.factor.get(), 1.5);
    }

    // toggleFullScreen

    #[test]
    fn answers_with_the_state_asked_for_before_the_window_catches_up() {
        let window = fake_window(1.0, false);
        assert_eq!(
            toggle_full_screen(&window),
            ViewState {
                zoom: 100,
                full_screen: true
            }
        );
        assert_eq!(window.requested.get(), Some(true));

        window.full_screen.set(true);
        assert!(!toggle_full_screen(&window).full_screen);
        assert_eq!(window.requested.get(), Some(false));
    }

    #[test]
    fn a_direction_is_in_out_or_a_reset() {
        assert_eq!(ZoomDirection::parse("in"), In);
        assert_eq!(ZoomDirection::parse("out"), Out);
        assert_eq!(ZoomDirection::parse("reset"), Reset);
        assert_eq!(ZoomDirection::parse("sideways"), Reset);
    }

    #[test]
    fn the_state_goes_to_the_renderer_in_camel_case() {
        let state = ViewState {
            zoom: 125,
            full_screen: true,
        };
        assert_eq!(
            state.to_json(),
            serde_json::json!({ "zoom": 125, "fullScreen": true })
        );
    }

    struct Maximizable(Cell<bool>);

    impl MaximizeWindow for Maximizable {
        fn is_maximized(&self) -> bool {
            self.0.get()
        }
        fn maximize(&self) {
            self.0.set(true);
        }
        fn unmaximize(&self) {
            self.0.set(false);
        }
    }

    #[test]
    fn zoom_maximizes_a_window_and_restores_a_maximized_one() {
        let window = Maximizable(Cell::new(false));
        toggle_maximize(&window);
        assert!(window.is_maximized());
        toggle_maximize(&window);
        assert!(!window.is_maximized());
    }
}
