// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Remembering where the window was.
//!
//! Restoring a window to its last position is only safe if that position still
//! exists: a laptop undocked from a second monitor, or a display resolution
//! change, can leave saved coordinates entirely off-screen, a window the user
//! cannot see, drag or close. Everything here is about avoiding that.
//!
//! Stored separately from the app settings because it changes on every move and
//! resize; rewriting the settings file that often would be needless churn.
//!
//! Rust notes: the stores are synchronous, and the caller wraps them in
//! whatever blocking facility it has. Coordinates stay `f64` because the JS
//! numbers they came from are floats.

use serde::{Serialize, Serializer};
use serde_json::Value;
use std::collections::HashSet;
use std::io;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

pub const WINDOW_STATE_FILENAME: &str = "agentrq-window.json";

/// Never restore a window smaller than this; it matches the window's minimums.
pub const MIN_WIDTH: f64 = 940.0;
pub const MIN_HEIGHT: f64 = 600.0;

/// "Visible" means this much real overlap with some display, not merely a
/// corner touching it: a window one pixel on screen is a window the user lost.
const MIN_VISIBLE: f64 = 80.0;

/// A rectangle: a display's work area, or a window's bounds.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Rect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// Saved bounds as read from disk, where any field may be missing or unusable.
/// `None` stands for everything JS's `isFiniteNumber` refuses; `NaN` and the
/// infinities are refused here too.
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub struct Bounds {
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub width: Option<f64>,
    pub height: Option<f64>,
}

/// Where to open the window. `x` and `y` are `None` when there is no sensible
/// position, which lets the platform centre the window.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Placement {
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub width: f64,
    pub height: f64,
}

/// What is kept on disk. Numbers serialise as JS writes them (`1200`, not
/// `1200.0`), so an Electron-written file and a Tauri-written one read alike.
#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
pub struct WindowState {
    #[serde(skip_serializing_if = "Option::is_none", serialize_with = "number_opt")]
    pub x: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", serialize_with = "number_opt")]
    pub y: Option<f64>,
    #[serde(serialize_with = "number")]
    pub width: f64,
    #[serde(serialize_with = "number")]
    pub height: f64,
    pub maximized: bool,
}

pub const DEFAULT_WINDOW_STATE: WindowState = WindowState {
    x: None,
    y: None,
    width: 1280.0,
    height: 860.0,
    maximized: false,
};

fn number<S: Serializer>(value: &f64, serializer: S) -> Result<S::Ok, S::Error> {
    if value.fract() == 0.0 && value.abs() < 1e15 {
        serializer.serialize_i64(*value as i64)
    } else {
        serializer.serialize_f64(*value)
    }
}

fn number_opt<S: Serializer>(value: &Option<f64>, serializer: S) -> Result<S::Ok, S::Error> {
    // `skip_serializing_if` keeps `None` from ever getting here.
    number(&value.unwrap_or_default(), serializer)
}

fn finite(value: Option<f64>) -> Option<f64> {
    value.filter(|v| v.is_finite())
}

/// JS's `Math.round`, which rounds halves toward +infinity.
fn round(value: f64) -> f64 {
    (value + 0.5).floor()
}

/// Constrain saved bounds to a display that actually exists.
///
/// `displays` are the work areas of the attached displays. The result is bounds
/// guaranteed to be visible, or size-only when there is no sensible position.
pub fn clamp_to_displays(bounds: Option<Bounds>, displays: &[Rect]) -> Placement {
    let bounds = bounds.unwrap_or_default();
    let width = MIN_WIDTH.max(finite(bounds.width).unwrap_or(DEFAULT_WINDOW_STATE.width));
    let height = MIN_HEIGHT.max(finite(bounds.height).unwrap_or(DEFAULT_WINDOW_STATE.height));
    let size_only = Placement {
        x: None,
        y: None,
        width,
        height,
    };

    if displays.is_empty() {
        return size_only;
    }
    let (Some(x), Some(y)) = (finite(bounds.x), finite(bounds.y)) else {
        return size_only;
    };

    let target = displays.iter().find(|display| {
        let overlap_x = (x + width).min(display.x + display.width) - x.max(display.x);
        let overlap_y = (y + height).min(display.y + display.height) - y.max(display.y);
        overlap_x >= MIN_VISIBLE && overlap_y >= MIN_VISIBLE
    });

    // The saved position is gone: a monitor was unplugged, most likely. Return
    // size only so the window opens centred on the primary display rather than
    // somewhere the user cannot reach it.
    let Some(target) = target else {
        return size_only;
    };

    // Shrink to fit a smaller display before positioning, so a window saved on
    // a large monitor does not overflow a laptop screen.
    let fitted_width = width.min(target.width);
    let fitted_height = height.min(target.height);

    Placement {
        x: Some(round(
            x.max(target.x).min(target.x + target.width - fitted_width),
        )),
        y: Some(round(
            y.max(target.y)
                .min(target.y + target.height - fitted_height),
        )),
        width: round(fitted_width),
        height: round(fitted_height),
    }
}

fn json_finite(value: &Value, key: &str) -> Option<f64> {
    finite(value.get(key).and_then(Value::as_f64))
}

/// Coerce whatever was on disk into a state object.
pub fn normalize_window_state(raw: &Value) -> WindowState {
    if !raw.is_object() {
        return DEFAULT_WINDOW_STATE;
    }
    WindowState {
        x: json_finite(raw, "x"),
        y: json_finite(raw, "y"),
        width: json_finite(raw, "width").map_or(DEFAULT_WINDOW_STATE.width, |v| v.max(MIN_WIDTH)),
        height: json_finite(raw, "height")
            .map_or(DEFAULT_WINDOW_STATE.height, |v| v.max(MIN_HEIGHT)),
        maximized: raw.get("maximized") == Some(&Value::Bool(true)),
    }
}

/// The file the state lives in.
pub trait WindowStateFile {
    /// Fails when the file is absent.
    fn read_file(&self) -> io::Result<String>;
    fn write_file(&self, contents: &str) -> io::Result<()>;
}

pub struct WindowStateStore<F: WindowStateFile> {
    file: F,
}

impl<F: WindowStateFile> WindowStateStore<F> {
    pub fn new(file: F) -> Self {
        Self { file }
    }

    /// Missing or corrupt reads as the defaults, which are a perfectly good
    /// window.
    pub fn load(&self) -> WindowState {
        self.file
            .read_file()
            .ok()
            .and_then(|contents| serde_json::from_str::<Value>(&contents).ok())
            .map_or(DEFAULT_WINDOW_STATE, |raw| normalize_window_state(&raw))
    }

    /// Normalises on the way in as well as out.
    pub fn save(&self, state: &WindowState) -> io::Result<()> {
        let normalized = normalize_window_state(&serde_json::to_value(state)?);
        self.file
            .write_file(&serde_json::to_string_pretty(&normalized)?)
    }
}

/// A window, as far as remembering it is concerned.
pub trait WindowGeometry {
    /// The size the window had before it was maximized or minimized, where the
    /// platform can say.
    fn normal_bounds(&self) -> Option<Rect>;
    fn bounds(&self) -> Rect;
    fn is_maximized(&self) -> bool;
}

/// Capture a window's current state.
///
/// A maximized or minimized window reports its *current* bounds, which are the
/// screen, not the size to restore to. The normal bounds are what to remember.
pub fn capture_window_state(window: &dyn WindowGeometry) -> WindowState {
    let bounds = window.normal_bounds().unwrap_or_else(|| window.bounds());
    WindowState {
        x: Some(bounds.x),
        y: Some(bounds.y),
        width: bounds.width,
        height: bounds.height,
        maximized: window.is_maximized(),
    }
}

/// Timers, injected so a debounce can be tested without waiting.
pub trait Timers: Send + Sync {
    /// Run `callback` once after `ms`. The handle can be passed to `clear_timer`.
    fn set_timer(&self, ms: u64, callback: Box<dyn FnOnce() + Send>) -> u64;
    fn clear_timer(&self, handle: u64);
    /// Whether this implementation uses real OS timers / threads.
    fn is_real(&self) -> bool {
        false
    }
}

/// Real timers: one sleeping thread per pending call. Fine for a debounced
/// write; a runtime that has its own timers should implement `Timers` instead.
#[derive(Default)]
pub struct StdTimers {
    next: AtomicU64,
    pending: Arc<Mutex<HashSet<u64>>>,
}

impl Timers for StdTimers {
    fn is_real(&self) -> bool {
        true
    }

    fn set_timer(&self, ms: u64, callback: Box<dyn FnOnce() + Send>) -> u64 {
        let handle = self.next.fetch_add(1, Ordering::Relaxed);
        let pending = Arc::clone(&self.pending);
        pending.lock().unwrap().insert(handle);
        std::thread::spawn(move || {
            std::thread::sleep(Duration::from_millis(ms));
            if pending.lock().unwrap().remove(&handle) {
                callback();
            }
        });
        handle
    }

    fn clear_timer(&self, handle: u64) {
        self.pending.lock().unwrap().remove(&handle);
    }
}

/// Collapse a burst of move/resize events into one write.
///
/// Dragging a window emits these continuously; writing the file on each would
/// be hundreds of writes for one gesture. The last call's argument is the one
/// that runs.
pub struct Debounced<A: Send + 'static> {
    run: Arc<dyn Fn(A) + Send + Sync>,
    ms: u64,
    timers: Arc<dyn Timers>,
    handle: Arc<Mutex<Option<u64>>>,
}

pub fn debounce<A: Send + 'static>(
    run: impl Fn(A) + Send + Sync + 'static,
    ms: u64,
    timers: Arc<dyn Timers>,
) -> Debounced<A> {
    Debounced {
        run: Arc::new(run),
        ms,
        timers,
        handle: Arc::new(Mutex::new(None)),
    }
}

impl<A: Send + 'static> Debounced<A> {
    pub fn call(&self, arg: A) {
        // Held until the new timer's handle is stored, so a timer that fires at
        // once cannot clear a handle that is not there yet.
        let mut handle = self.handle.lock().unwrap();
        if let Some(previous) = handle.take() {
            self.timers.clear_timer(previous);
        }
        let run = Arc::clone(&self.run);
        let slot = Arc::clone(&self.handle);
        *handle = Some(self.timers.set_timer(
            self.ms,
            Box::new(move || {
                *slot.lock().unwrap() = None;
                run(arg);
            }),
        ));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::RefCell;

    const LAPTOP: Rect = Rect {
        x: 0.0,
        y: 0.0,
        width: 1440.0,
        height: 900.0,
    };
    const EXTERNAL: Rect = Rect {
        x: 1440.0,
        y: 0.0,
        width: 2560.0,
        height: 1440.0,
    };

    fn bounds(x: f64, y: f64, width: f64, height: f64) -> Option<Bounds> {
        Some(Bounds {
            x: Some(x),
            y: Some(y),
            width: Some(width),
            height: Some(height),
        })
    }

    fn size(width: f64, height: f64) -> Placement {
        Placement {
            x: None,
            y: None,
            width,
            height,
        }
    }

    fn at(x: f64, y: f64, width: f64, height: f64) -> Placement {
        Placement {
            x: Some(x),
            y: Some(y),
            width,
            height,
        }
    }

    // clampToDisplays

    #[test]
    fn keeps_a_position_that_is_still_on_a_display() {
        assert_eq!(
            clamp_to_displays(bounds(100.0, 100.0, 1200.0, 800.0), &[LAPTOP]),
            at(100.0, 100.0, 1200.0, 800.0)
        );
    }

    #[test]
    fn keeps_a_position_on_a_secondary_display() {
        let result = clamp_to_displays(bounds(1600.0, 200.0, 1200.0, 800.0), &[LAPTOP, EXTERNAL]);
        assert_eq!((result.x, result.y), (Some(1600.0), Some(200.0)));
    }

    #[test]
    fn drops_a_position_whose_display_is_gone() {
        assert_eq!(
            clamp_to_displays(bounds(2000.0, 300.0, 1200.0, 800.0), &[LAPTOP]),
            size(1200.0, 800.0)
        );
    }

    #[test]
    fn drops_a_position_that_is_barely_on_screen() {
        assert_eq!(
            clamp_to_displays(bounds(1400.0, 100.0, 1200.0, 800.0), &[LAPTOP]),
            size(1200.0, 800.0)
        );
    }

    #[test]
    fn pulls_a_partly_off_screen_window_back_into_view() {
        let result = clamp_to_displays(bounds(1000.0, 700.0, 1200.0, 800.0), &[LAPTOP]);
        assert_eq!(result.x, Some(240.0));
        assert_eq!(result.y, Some(100.0));
    }

    #[test]
    fn shrinks_a_window_saved_on_a_larger_display() {
        let result = clamp_to_displays(bounds(1440.0, 0.0, 2400.0, 1300.0), &[EXTERNAL]);
        assert_eq!(result.width, 2400.0);
        assert_eq!(
            clamp_to_displays(bounds(0.0, 0.0, 2400.0, 1300.0), &[LAPTOP]),
            at(0.0, 0.0, 1440.0, 900.0)
        );
    }

    #[test]
    fn never_restores_a_window_below_its_minimum_size() {
        let result = clamp_to_displays(bounds(0.0, 0.0, 100.0, 100.0), &[LAPTOP]);
        assert_eq!((result.width, result.height), (MIN_WIDTH, MIN_HEIGHT));
    }

    #[test]
    fn falls_back_to_defaults_when_there_is_nothing_usable() {
        assert_eq!(
            clamp_to_displays(None, &[LAPTOP]),
            size(DEFAULT_WINDOW_STATE.width, DEFAULT_WINDOW_STATE.height)
        );
        let sized = Some(Bounds {
            width: Some(1200.0),
            height: Some(800.0),
            ..Default::default()
        });
        assert_eq!(clamp_to_displays(sized, &[]), size(1200.0, 800.0));
        // JS also passes `null` for the displays; a slice cannot be null, so the
        // empty slice above stands for both.
    }

    #[test]
    fn ignores_a_position_that_is_not_a_real_number() {
        // NaN and Infinity as in JS; '100' and null have no typed form and
        // arrive as `None`.
        for bad in [Some(f64::NAN), Some(f64::INFINITY), None] {
            let saved = Some(Bounds {
                x: bad,
                y: Some(0.0),
                width: Some(1200.0),
                height: Some(800.0),
            });
            assert_eq!(clamp_to_displays(saved, &[LAPTOP]), size(1200.0, 800.0));
        }
    }

    #[test]
    fn rounds_halves_up_like_javascript() {
        let result = clamp_to_displays(bounds(10.5, -0.5, 1000.5, 700.5), &[LAPTOP]);
        // -0.5 is pulled onto the display, so only the positive halves round.
        assert_eq!(result, at(11.0, 0.0, 1001.0, 701.0));
        assert_eq!(round(-2.5), -2.0);
    }

    // normalizeWindowState

    #[test]
    fn keeps_a_well_formed_state() {
        assert_eq!(
            normalize_window_state(
                &json!({"x": 10, "y": 20, "width": 1200, "height": 800, "maximized": true})
            ),
            WindowState {
                x: Some(10.0),
                y: Some(20.0),
                width: 1200.0,
                height: 800.0,
                maximized: true
            }
        );
    }

    #[test]
    fn drops_a_position_that_is_not_usable_but_keeps_the_size() {
        assert_eq!(
            normalize_window_state(&json!({"width": 1200, "height": 800})),
            WindowState {
                x: None,
                y: None,
                width: 1200.0,
                height: 800.0,
                maximized: false
            }
        );
    }

    #[test]
    fn raises_a_too_small_size_to_the_minimum() {
        let state = normalize_window_state(&json!({"width": 10, "height": 10}));
        assert_eq!((state.width, state.height), (MIN_WIDTH, MIN_HEIGHT));
    }

    #[test]
    fn falls_back_to_the_default_size_when_the_stored_one_is_unusable() {
        assert_eq!(
            normalize_window_state(&json!({"x": 10, "y": 20})),
            WindowState {
                x: Some(10.0),
                y: Some(20.0),
                ..DEFAULT_WINDOW_STATE
            }
        );
        let state = normalize_window_state(&json!({"width": null, "height": "800"}));
        assert_eq!(
            (state.width, state.height),
            (DEFAULT_WINDOW_STATE.width, DEFAULT_WINDOW_STATE.height)
        );
    }

    #[test]
    fn falls_back_to_defaults_for_anything_unusable() {
        for raw in [json!(null), json!("string"), json!(42), json!([])] {
            assert_eq!(normalize_window_state(&raw), DEFAULT_WINDOW_STATE);
        }
    }

    #[test]
    fn treats_a_non_boolean_maximized_as_false() {
        let raw = json!({"width": 1200, "height": 800, "maximized": "yes"});
        assert!(!normalize_window_state(&raw).maximized);
    }

    // createWindowStateStore

    #[derive(Default)]
    struct MemoryFile {
        contents: RefCell<Option<String>>,
    }

    impl MemoryFile {
        fn with(contents: &str) -> Self {
            Self {
                contents: RefCell::new(Some(contents.to_string())),
            }
        }
    }

    impl WindowStateFile for &MemoryFile {
        fn read_file(&self) -> io::Result<String> {
            self.contents
                .borrow()
                .clone()
                .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "ENOENT"))
        }
        fn write_file(&self, contents: &str) -> io::Result<()> {
            *self.contents.borrow_mut() = Some(contents.to_string());
            Ok(())
        }
    }

    #[test]
    fn returns_defaults_on_first_run() {
        let file = MemoryFile::default();
        assert_eq!(WindowStateStore::new(&file).load(), DEFAULT_WINDOW_STATE);
    }

    #[test]
    fn returns_defaults_for_a_corrupt_file_rather_than_failing_to_open() {
        let file = MemoryFile::with("{ not json");
        assert_eq!(WindowStateStore::new(&file).load(), DEFAULT_WINDOW_STATE);
    }

    #[test]
    fn round_trips_a_state() {
        let file = MemoryFile::default();
        let store = WindowStateStore::new(&file);
        let state = WindowState {
            x: Some(10.0),
            y: Some(20.0),
            width: 1200.0,
            height: 800.0,
            maximized: false,
        };
        store.save(&state).unwrap();
        assert_eq!(store.load(), state);
    }

    #[test]
    fn normalises_on_the_way_in_as_well_as_out() {
        let file = MemoryFile::default();
        let store = WindowStateStore::new(&file);
        store
            .save(&WindowState {
                width: 10.0,
                height: 10.0,
                ..DEFAULT_WINDOW_STATE
            })
            .unwrap();
        let written: Value =
            serde_json::from_str(file.contents.borrow().as_ref().unwrap()).unwrap();
        assert_eq!(written["width"], json!(MIN_WIDTH as i64));
        assert_eq!(written["height"], json!(MIN_HEIGHT as i64));
    }

    #[test]
    fn writes_numbers_the_way_javascript_does() {
        // Not in the vitest file: a file Electron wrote and one Tauri wrote must
        // look alike, so whole numbers carry no `.0` and a position is omitted
        // rather than written as null.
        let file = MemoryFile::default();
        let store = WindowStateStore::new(&file);
        store
            .save(&WindowState {
                x: Some(10.0),
                y: Some(20.5),
                ..DEFAULT_WINDOW_STATE
            })
            .unwrap();
        let text = file.contents.borrow().clone().unwrap();
        assert!(text.contains("\"x\": 10,"), "{text}");
        assert!(text.contains("\"y\": 20.5,"), "{text}");
        store.save(&DEFAULT_WINDOW_STATE).unwrap();
        let text = file.contents.borrow().clone().unwrap();
        assert!(!text.contains("\"x\""), "{text}");
        assert!(text.contains("\"width\": 1280,"), "{text}");
    }

    // captureWindowState

    struct FakeWindow {
        normal: Option<Rect>,
        current: Rect,
        maximized: bool,
    }

    impl WindowGeometry for FakeWindow {
        fn normal_bounds(&self) -> Option<Rect> {
            self.normal
        }
        fn bounds(&self) -> Rect {
            self.current
        }
        fn is_maximized(&self) -> bool {
            self.maximized
        }
    }

    #[test]
    fn records_the_restore_size_not_the_maximized_size() {
        let window = FakeWindow {
            normal: Some(Rect {
                x: 10.0,
                y: 20.0,
                width: 1200.0,
                height: 800.0,
            }),
            current: Rect {
                x: 0.0,
                y: 0.0,
                width: 2560.0,
                height: 1440.0,
            },
            maximized: true,
        };
        assert_eq!(
            capture_window_state(&window),
            WindowState {
                x: Some(10.0),
                y: Some(20.0),
                width: 1200.0,
                height: 800.0,
                maximized: true
            }
        );
    }

    #[test]
    fn falls_back_to_get_bounds_when_normal_bounds_are_unavailable() {
        let window = FakeWindow {
            normal: None,
            current: Rect {
                x: 5.0,
                y: 6.0,
                width: 1000.0,
                height: 700.0,
            },
            maximized: false,
        };
        assert_eq!(
            capture_window_state(&window),
            WindowState {
                x: Some(5.0),
                y: Some(6.0),
                width: 1000.0,
                height: 700.0,
                maximized: false
            }
        );
    }

    // debounce

    #[derive(Default)]
    struct FakeTimers {
        callbacks: Mutex<Vec<Box<dyn FnOnce() + Send>>>,
        cleared: Mutex<Vec<u64>>,
    }

    impl Timers for FakeTimers {
        fn set_timer(&self, _ms: u64, callback: Box<dyn FnOnce() + Send>) -> u64 {
            let mut callbacks = self.callbacks.lock().unwrap();
            callbacks.push(callback);
            callbacks.len() as u64
        }
        fn clear_timer(&self, handle: u64) {
            self.cleared.lock().unwrap().push(handle);
        }
    }

    impl FakeTimers {
        /// Fire the most recently set timer, as the real one would.
        fn fire_last(&self) {
            let callback = self.callbacks.lock().unwrap().pop().unwrap();
            callback();
        }
    }

    struct Recorder(Arc<Mutex<Vec<&'static str>>>);

    impl Recorder {
        fn new() -> (Self, impl Fn(&'static str) + Send + Sync + 'static) {
            let calls = Arc::new(Mutex::new(Vec::new()));
            let sink = Arc::clone(&calls);
            (Self(calls), move |arg| sink.lock().unwrap().push(arg))
        }
        fn calls(&self) -> Vec<&'static str> {
            self.0.lock().unwrap().clone()
        }
    }

    #[test]
    fn runs_once_for_a_burst_of_calls() {
        let timers = Arc::new(FakeTimers::default());
        let (recorder, run) = Recorder::new();
        let debounced = debounce(run, 500, timers.clone());

        debounced.call("a");
        debounced.call("b");
        debounced.call("c");
        timers.fire_last();

        assert_eq!(timers.cleared.lock().unwrap().len(), 2);
        assert_eq!(recorder.calls(), ["c"]);
    }

    #[test]
    fn runs_again_after_the_previous_call_settled() {
        let timers = Arc::new(FakeTimers::default());
        let (recorder, run) = Recorder::new();
        let debounced = debounce(run, 500, timers.clone());

        debounced.call("first");
        timers.fire_last();
        debounced.call("second");
        timers.fire_last();

        assert_eq!(recorder.calls(), ["first", "second"]);
        assert!(timers.cleared.lock().unwrap().is_empty());
    }

    #[test]
    fn really_does_defer_using_the_default_timer() {
        let (recorder, run) = Recorder::new();
        let debounced = debounce(run, 5, Arc::new(StdTimers::default()));

        debounced.call("x");
        assert!(recorder.calls().is_empty());
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(recorder.calls(), ["x"]);
    }

    #[test]
    fn the_default_timer_can_be_cancelled() {
        let timers = StdTimers::default();
        let (recorder, run) = Recorder::new();
        let run = Arc::new(run);
        let callback = |tag: &'static str| -> Box<dyn FnOnce() + Send> {
            let run = Arc::clone(&run);
            Box::new(move || run(tag))
        };
        let cancelled = timers.set_timer(5, callback("cancelled"));
        timers.set_timer(5, callback("kept"));
        timers.clear_timer(cancelled);
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(recorder.calls(), ["kept"]);
    }
}
