// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! A panic is written to the app's log dir before the process goes down.
//!
//! A release build on Windows has no console, and a macOS or Linux app started
//! from its icon has nowhere for stderr to go, so without this a crash leaves
//! no trace. The previous hook still runs after ours.

use std::fs::{self, OpenOptions};
use std::io::Write;
use std::panic::PanicHookInfo;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

/// The file panics are appended to.
pub fn log_file(dir: &Path) -> PathBuf {
    dir.join("panic.log")
}

/// One line per panic: when (Unix seconds), where, and what it said.
pub fn line(info: &PanicHookInfo<'_>, now: SystemTime) -> String {
    let secs = now.duration_since(UNIX_EPOCH).map_or(0, |d| d.as_secs());
    // Always there in practice; std only reserves the right to omit it.
    let at = info
        .location()
        .map(|l| format!("{}:{}", l.file(), l.line()))
        .unwrap_or_default();
    let payload = info.payload();
    let message = payload
        .downcast_ref::<&str>()
        .map(|s| s.to_string())
        .or_else(|| payload.downcast_ref::<String>().cloned())
        .unwrap_or_else(|| "non-string panic payload".to_string());
    format!("{secs} panic at {at}: {message}\n")
}

/// Appends `text` to the panic log in `dir`, creating both if needed.
pub fn append(dir: &Path, text: &str) -> std::io::Result<()> {
    fs::create_dir_all(dir)?;
    OpenOptions::new()
        .create(true)
        .append(true)
        .open(log_file(dir))?
        .write_all(text.as_bytes())
}

/// Installs the hook. A failure to write is ignored: the panic is already the
/// thing being reported, and the previous hook still prints it.
pub fn install(dir: PathBuf) {
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let _ = append(&dir, &line(info, SystemTime::now()));
        previous(info);
    }));
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Arc, Mutex};
    use std::time::Duration;

    /// The panic hook is global: tests that swap it take turns.
    static HOOK: Mutex<()> = Mutex::new(());

    /// Runs `f` under a hook that captures the line `line` makes for its panic.
    fn line_for(f: impl FnOnce() + std::panic::UnwindSafe) -> String {
        let _turn = HOOK.lock().unwrap();
        let out = Arc::new(Mutex::new(String::new()));
        let seen = out.clone();
        let previous = std::panic::take_hook();
        std::panic::set_hook(Box::new(move |info| {
            *seen.lock().unwrap() = line(info, UNIX_EPOCH + Duration::from_secs(42));
        }));
        let _ = std::panic::catch_unwind(f);
        std::panic::set_hook(previous);
        let text = out.lock().unwrap().clone();
        text
    }

    #[test]
    fn names_the_time_the_place_and_the_message() {
        let text = line_for(|| panic!("static message"));
        assert!(text.starts_with("42 panic at "), "{text}");
        assert!(text.contains("panic_log.rs:"), "{text}");
        assert!(text.ends_with(": static message\n"), "{text}");

        let n = 7;
        assert!(line_for(move || panic!("formatted {n}")).ends_with(": formatted 7\n"));
        assert!(line_for(|| std::panic::panic_any(5_u8)).ends_with(": non-string panic payload\n"));
    }

    #[test]
    fn appends_and_creates_the_dir() {
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("logs");
        append(&dir, "one\n").unwrap();
        append(&dir, "two\n").unwrap();
        assert_eq!(fs::read_to_string(log_file(&dir)).unwrap(), "one\ntwo\n");
    }

    #[test]
    fn reports_a_dir_it_cannot_create() {
        let tmp = tempfile::tempdir().unwrap();
        let file = tmp.path().join("file");
        fs::write(&file, "").unwrap();
        assert!(append(&file.join("logs"), "x").is_err());
    }

    #[test]
    fn the_installed_hook_writes_the_log_and_keeps_the_previous_hook() {
        let _turn = HOOK.lock().unwrap();
        let tmp = tempfile::tempdir().unwrap();
        let ran = Arc::new(Mutex::new(false));
        let flag = ran.clone();
        let original = std::panic::take_hook();
        std::panic::set_hook(Box::new(move |_| *flag.lock().unwrap() = true));
        install(tmp.path().to_path_buf());
        let _ = std::panic::catch_unwind(|| panic!("from the app"));
        std::panic::set_hook(original);

        let log = fs::read_to_string(log_file(tmp.path())).unwrap();
        assert!(log.contains("panic at "), "{log}");
        assert!(log.ends_with(": from the app\n"), "{log}");
        assert!(*ran.lock().unwrap());
    }
}
