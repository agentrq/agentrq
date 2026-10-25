// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What the window chrome keeps, apart from the window itself: the state file,
//! each window's zoom, and the Linux launcher id. Tauri has no getter for a
//! webview's zoom, so it is kept here, and the View menu and the title bar both
//! go through it, never through a native zoom shortcut.

use std::collections::HashMap;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use agentrq_desktop_core::window_state::WindowStateFile;

/// `agentrq-window.json` in the app's data dir.
pub struct StateFile(pub PathBuf);

impl WindowStateFile for StateFile {
    fn read_file(&self) -> io::Result<String> {
        std::fs::read_to_string(&self.0)
    }

    fn write_file(&self, contents: &str) -> io::Result<()> {
        self.0
            .parent()
            .map_or(Ok(()), std::fs::create_dir_all)
            .and_then(|()| std::fs::write(&self.0, contents))
    }
}

/// The zoom of each window, by label. A window nobody has zoomed is at 1.
#[derive(Default)]
pub struct ZoomTable(Mutex<HashMap<String, f64>>);

impl ZoomTable {
    pub fn get(&self, label: &str) -> f64 {
        self.0.lock().unwrap().get(label).copied().unwrap_or(1.0)
    }

    pub fn set(&self, label: &str, factor: f64) {
        self.0.lock().unwrap().insert(label.to_string(), factor);
    }

    /// A window made again under the same label starts at 1 again.
    pub fn forget(&self, label: &str) {
        self.0.lock().unwrap().remove(label);
    }
}

/// Where the folder chooser opens: where the field already points, when that
/// is an absolute path, so changing one is not a fresh hunt through the disk.
pub fn chooser_start(current: Option<&str>) -> Option<PathBuf> {
    current
        .map(Path::new)
        .filter(|path| path.is_absolute())
        .map(Path::to_path_buf)
}

#[cfg(test)]
mod tests {
    use super::*;
    use agentrq_desktop_core::app_icon::{launcher_entry_name, png_dimensions};
    use agentrq_desktop_core::window_state::{
        WindowStateStore, DEFAULT_WINDOW_STATE, WINDOW_STATE_FILENAME,
    };

    #[test]
    fn a_missing_file_reads_as_the_defaults_and_a_save_creates_its_folder() {
        let dir = tempfile::tempdir().unwrap();
        let store = WindowStateStore::new(StateFile(
            dir.path().join("data").join(WINDOW_STATE_FILENAME),
        ));
        assert_eq!(store.load(), DEFAULT_WINDOW_STATE);

        let mut state = DEFAULT_WINDOW_STATE;
        state.x = Some(40.0);
        state.maximized = true;
        store.save(&state).unwrap();
        assert_eq!(store.load(), state);
    }

    #[test]
    fn a_save_that_cannot_be_written_says_so() {
        let dir = tempfile::tempdir().unwrap();
        let blocker = dir.path().join("file");
        std::fs::write(&blocker, "").unwrap();
        // The parent is a file, so no folder can be made.
        let store = StateFile(blocker.join(WINDOW_STATE_FILENAME));
        assert!(store.write_file("{}").is_err());
    }

    #[test]
    fn a_window_is_at_one_until_zoomed_and_each_keeps_its_own() {
        let zoom = ZoomTable::default();
        assert_eq!(zoom.get("main"), 1.0);
        zoom.set("main", 1.25);
        zoom.set("other", 0.5);
        assert_eq!((zoom.get("main"), zoom.get("other")), (1.25, 0.5));
        zoom.forget("main");
        assert_eq!((zoom.get("main"), zoom.get("other")), (1.0, 0.5));
    }

    fn config() -> serde_json::Value {
        serde_json::from_str(include_str!("../tauri.conf.json")).unwrap()
    }

    /// A desktop environment joins a running window to its launcher by the id
    /// the window announces, and the packager names the launcher after the
    /// binary. If the binary is renamed the two part company and Linux shows
    /// the toolkit's default icon again.
    #[test]
    fn the_launcher_the_package_installs_is_named_after_the_binary() {
        let binary = config()["mainBinaryName"].as_str().unwrap().to_string();
        assert_eq!(binary, "agentrq-tauri");
        assert_eq!(launcher_entry_name(&binary), "agentrq-tauri.desktop");
        assert!(include_str!("../Cargo.toml").contains(&format!("name = \"{binary}\"")));
    }

    #[test]
    fn the_window_icon_is_a_square_png_at_the_size_it_is_named_for() {
        let icon = include_bytes!("../icons/128x128@2x.png");
        assert_eq!(png_dimensions(icon), Some((256, 256)));
        let one_x = include_bytes!("../icons/128x128.png");
        assert_eq!(png_dimensions(one_x), Some((128, 128)));
    }

    #[test]
    fn the_chooser_opens_where_the_field_points_if_that_is_absolute() {
        let absolute = std::env::temp_dir();
        assert_eq!(chooser_start(absolute.to_str()), Some(absolute.clone()));
        assert_eq!(chooser_start(Some("relative/dir")), None);
        assert_eq!(chooser_start(Some("")), None);
        assert_eq!(chooser_start(None), None);
    }
}
