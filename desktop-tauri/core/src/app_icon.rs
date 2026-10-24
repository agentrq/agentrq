// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The window icon: the one thing Linux will not work out for itself.
//!
//! Windows takes a window's taskbar icon from the icon compiled into the
//! executable, and macOS takes the Dock icon from the `.icns` inside the app
//! bundle. Both come from the packaging config, so neither platform has to be
//! told anything at runtime, which is exactly why the icon looks right on both
//! and wrong on Linux.
//!
//! Linux has no equivalent. Nothing about an AppImage or a `.deb` tells a
//! *running window* what to display, so a window created without an icon shows
//! the toolkit's default in the taskbar, the alt-tab switcher and the window
//! list. The icon has to be handed to each window explicitly.
//!
//! The second half of the same problem is the app id: a desktop environment
//! links a running window to its installed launcher entry by it, so the id the
//! window announces has to agree with the entry the packager writes. That rule
//! is [`launcher_entry_name`]; the binary name and bundle id are parameters
//! because the Electron and Tauri builds do not share them.
//!
//! Rust notes: the platform is a `process.platform` string, as in the JS, so an
//! unknown platform is simply "no icon needed". Checks the JS runs against
//! repository files (`package.json`, `electron-builder.yml`, the committed PNGs)
//! are ported as the pure rule they check, over values the caller reads.

use serde::Deserialize;
use std::path::{Component, Path, PathBuf};

/// The icon file, named as the *frontend* publishes it.
///
/// There is deliberately no second copy of this asset in the shell: the build
/// already places the frontend's file at the root of the renderer output. This
/// is the rounded, transparent mark rather than `large-icon.png`, which is the
/// same artwork on an opaque square tile. On Linux the window icon sits beside
/// the launcher icon, which is rendered from the (rounded) SVG, so two shapes
/// for one app would be exactly the wrongness this module exists to stop.
pub const ICON_FILENAME: &str = "agentrq.png";

/// The vector the whole icon set is rendered from, relative to the repository.
pub const ICON_SOURCE: &str = "frontend/public/favicon.svg";

/// The sizes a hicolor theme is normally asked for.
pub const ICON_SIZES: [u32; 8] = [16, 24, 32, 48, 64, 128, 256, 512];

/// Whether this platform needs to be handed a window icon at all.
///
/// Passing one on Windows or macOS would not be harmful, but it would override
/// the icon the packaged app already carries with a raw PNG that has not been
/// through the `.ico`/`.icns` conversion, losing the small-size variants those
/// formats exist to hold.
pub fn needs_window_icon(platform: &str) -> bool {
    platform == "linux"
}

/// What the shell asks of the filesystem.
pub trait FileExists {
    fn exists(&self, path: &Path) -> bool;
}

impl<F: Fn(&Path) -> bool> FileExists for F {
    fn exists(&self, path: &Path) -> bool {
        self(path)
    }
}

/// Join and resolve `.` and `..` lexically, as Node's `path.join` does.
fn join_normalized(base: &Path, segments: &[&str]) -> PathBuf {
    let mut out = PathBuf::new();
    for component in segments
        .iter()
        .fold(base.to_path_buf(), |acc, s| acc.join(s))
        .components()
    {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                if !out.pop() {
                    out.push("..");
                }
            }
            other => out.push(other.as_os_str()),
        }
    }
    out
}

/// Where the icon might be, most-preferred first.
///
/// `renderer_root` is the built renderer directory, `project_root` the shell's
/// package directory.
pub fn icon_search_paths(renderer_root: &Path, project_root: &Path) -> [PathBuf; 2] {
    [
        // What a packaged app has.
        join_normalized(renderer_root, &[ICON_FILENAME]),
        // A dev run serves the renderer from the dev server and never builds
        // it, so the path above does not exist there. Fall back to the source
        // asset the build would have copied: the same file, one step earlier.
        join_normalized(project_root, &["..", "frontend", "public", ICON_FILENAME]),
    ]
}

/// Resolve the window icon, or `None` when this platform does not need one and
/// when, on the platform that does, the asset cannot be found.
///
/// A missing file is `None` rather than an error: an icon is cosmetic, and a
/// shell that refuses to open a window because it could not decorate it would
/// turn a cosmetic defect into an unusable app.
pub fn resolve_window_icon(
    platform: &str,
    renderer_root: &Path,
    project_root: &Path,
    files: &impl FileExists,
) -> Option<PathBuf> {
    if !needs_window_icon(platform) {
        return None;
    }
    icon_search_paths(renderer_root, project_root)
        .into_iter()
        .find(|path| files.exists(path))
}

/// The window-builder options for an icon.
///
/// `icon: None` is what keeps the other two platforms on their packaged icon.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct WindowIconOptions {
    pub icon: Option<PathBuf>,
}

pub fn window_icon_options(icon_path: Option<&Path>) -> WindowIconOptions {
    WindowIconOptions {
        icon: icon_path.map(Path::to_path_buf),
    }
}

/// The name of the launcher entry a Linux package installs for a binary.
///
/// The packager derives the executable, the package and the icon from the
/// binary's name, so the `.desktop` entry is installed as `<name>.desktop` and
/// that is the id the running window has to announce.
pub fn launcher_entry_name(binary_name: &str) -> String {
    format!("{binary_name}.desktop")
}

/// The file name of one size of the icon set; the size is parsed back out of it.
pub fn icon_file_name(size: u32) -> String {
    format!("{size}x{size}.png")
}

/// Whether `present` (the names in the icon directory) is exactly the icon set.
///
/// Nothing else: a leftover from an earlier size list would be collected into
/// the set alongside the current ones.
pub fn is_complete_icon_set(present: &[&str]) -> bool {
    let mut wanted: Vec<String> = ICON_SIZES.iter().map(|&s| icon_file_name(s)).collect();
    let mut pngs: Vec<String> = present
        .iter()
        .filter(|name| name.ends_with(".png"))
        .map(|name| name.to_string())
        .collect();
    wanted.sort();
    pngs.sort();
    wanted == pngs
}

/// Width and height from a PNG's header: the 8-byte signature and the IHDR
/// chunk's own 8 bytes come first, so they sit at offsets 16 and 20.
pub fn png_dimensions(bytes: &[u8]) -> Option<(u32, u32)> {
    let read = |at: usize| -> Option<u32> {
        Some(u32::from_be_bytes(bytes.get(at..at + 4)?.try_into().ok()?))
    };
    Some((read(16)?, read(20)?))
}

/// What the committed PNGs were rendered from, as recorded beside them.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct IconManifest {
    pub source: String,
    pub sha256: String,
    pub sizes: Vec<u32>,
}

/// Normalise the mark before fingerprinting it, so the answer is a property of
/// the artwork rather than of the machine it was checked out on.
///
/// A checkout that rewrites line endings has not changed the drawing, but it
/// changes every byte after the first newline. Every CR is dropped rather than
/// just the CRLF pairs, which makes this idempotent.
pub fn normalize_source(svg: &str) -> String {
    svg.replace('\r', "")
}

#[cfg(test)]
mod tests {
    use super::*;

    const RENDERER: &str = "/app/dist/renderer";
    const PROJECT: &str = "/app";

    fn built() -> PathBuf {
        Path::new(RENDERER).join(ICON_FILENAME)
    }

    fn source() -> PathBuf {
        PathBuf::from("/frontend/public").join(ICON_FILENAME)
    }

    /// Resolve against a fixed set of files that "exist".
    fn with_files(present: Vec<PathBuf>) -> impl Fn(&Path) -> bool {
        move |path| present.iter().any(|p| p == path)
    }

    /// Counts how often the filesystem was asked, and says everything exists.
    #[derive(Default)]
    struct Recording {
        looked: std::cell::Cell<u32>,
    }

    impl FileExists for Recording {
        fn exists(&self, _: &Path) -> bool {
            self.looked.set(self.looked.get() + 1);
            true
        }
    }

    fn resolve(platform: &str, files: &impl FileExists) -> Option<PathBuf> {
        resolve_window_icon(platform, Path::new(RENDERER), Path::new(PROJECT), files)
    }

    // needsWindowIcon

    #[test]
    fn is_true_on_linux() {
        assert!(needs_window_icon("linux"));
    }

    #[test]
    fn is_false_where_the_packaged_app_already_carries_its_icon() {
        assert!(!needs_window_icon("win32"));
        assert!(!needs_window_icon("darwin"));
    }

    #[test]
    fn is_false_for_a_platform_it_has_never_heard_of() {
        assert!(!needs_window_icon("freebsd"));
        assert!(!needs_window_icon(""));
    }

    // iconSearchPaths

    #[test]
    fn prefers_the_built_renderer_copy_over_the_frontend_source() {
        assert_eq!(
            icon_search_paths(Path::new(RENDERER), Path::new(PROJECT)),
            [built(), source()]
        );
    }

    #[test]
    fn names_the_same_file_the_web_build_publishes() {
        assert_eq!(ICON_FILENAME, "agentrq.png");
        for path in icon_search_paths(Path::new(RENDERER), Path::new(PROJECT)) {
            assert!(path.to_string_lossy().ends_with(ICON_FILENAME));
        }
    }

    #[test]
    fn resolves_dot_segments_like_a_path_join() {
        // Above the root there is nothing left to pop, so `..` is kept.
        assert_eq!(
            join_normalized(Path::new("a"), &["..", "..", "b", ".", "c"]),
            PathBuf::from("../b/c")
        );
    }

    #[test]
    fn drops_a_leading_current_directory_segment() {
        assert_eq!(
            join_normalized(Path::new("./a"), &["b"]),
            PathBuf::from("a/b")
        );
    }

    // resolveWindowIcon

    #[test]
    fn finds_the_icon_a_packaged_linux_app_ships() {
        assert_eq!(resolve("linux", &with_files(vec![built()])), Some(built()));
    }

    #[test]
    fn falls_back_to_the_frontend_source_when_the_renderer_is_unbuilt() {
        assert_eq!(
            resolve("linux", &with_files(vec![source()])),
            Some(source())
        );
    }

    #[test]
    fn returns_none_rather_than_failing_when_the_icon_is_missing() {
        assert_eq!(resolve("linux", &with_files(vec![])), None);
    }

    #[test]
    fn resolves_nothing_on_platforms_that_do_not_need_it() {
        // Even when the file is sitting right there.
        for platform in ["darwin", "win32"] {
            assert_eq!(
                resolve(platform, &with_files(vec![built(), source()])),
                None
            );
        }
    }

    #[test]
    fn does_not_touch_the_filesystem_on_platforms_that_do_not_need_it() {
        let files = Recording::default();
        assert_eq!(resolve("darwin", &files), None);
        assert_eq!(files.looked.get(), 0, "should not have looked");

        // The same stand-in does record when it is asked.
        assert_eq!(resolve("linux", &files), Some(built()));
        assert_eq!(files.looked.get(), 1);
    }

    // windowIconOptions

    #[test]
    fn spreads_an_icon_when_there_is_one() {
        assert_eq!(
            window_icon_options(Some(Path::new("/app/icon.png"))),
            WindowIconOptions {
                icon: Some(PathBuf::from("/app/icon.png"))
            }
        );
    }

    #[test]
    fn spreads_nothing_when_there_is_not() {
        // The absent icon is what leaves Windows and macOS on the icon their
        // packaged app already carries.
        assert_eq!(window_icon_options(None), WindowIconOptions::default());
        assert!(window_icon_options(None).icon.is_none());
    }

    // linux window association: the JS cases read package.json and
    // electron-builder.yml, so the rule is checked over the values they hold.
    // The Electron values stay as they are; the Tauri build passes its own.

    #[test]
    fn announces_the_app_under_the_name_its_launcher_entry_is_installed_as() {
        // Electron: package.json `name` is `agentrq-desktop`, and `desktopName`
        // has to be `<name>.desktop`.
        assert_eq!(
            launcher_entry_name("agentrq-desktop"),
            "agentrq-desktop.desktop"
        );
        // Tauri: the binary and the bundle id are the parameters.
        assert_eq!(
            launcher_entry_name("agentrq-tauri"),
            "agentrq-tauri.desktop"
        );
        assert_eq!(
            launcher_entry_name("com.agentrq.desktop.tauri"),
            "com.agentrq.desktop.tauri.desktop"
        );
    }

    #[test]
    fn leaves_the_installed_entry_named_what_it_was_already_named() {
        // Choosing the product name instead would rename an installed file and
        // orphan existing installs from their entry.
        let product_name = "AgentRQ";
        let entry = launcher_entry_name("agentrq-desktop");

        assert_ne!(entry, format!("{product_name}.desktop"));
        assert_eq!(entry, "agentrq-desktop.desktop");
    }

    #[test]
    fn hands_the_window_the_same_shape_the_launcher_entry_gets() {
        // Not the opaque square tile: that is the same artwork, but the
        // launcher icon is rendered from the rounded SVG.
        assert_ne!(ICON_FILENAME, "large-icon.png");
        assert_eq!(ICON_SOURCE, "frontend/public/favicon.svg");
    }

    // linux icon set

    #[test]
    fn holds_every_size_named_the_way_the_size_is_parsed_out() {
        let names: Vec<String> = ICON_SIZES.iter().map(|&s| icon_file_name(s)).collect();
        let mut present: Vec<&str> = names.iter().map(String::as_str).collect();
        // A manifest beside the PNGs is not part of the set.
        present.push("source.json");
        assert!(is_complete_icon_set(&present));
        assert_eq!(icon_file_name(16), "16x16.png");

        // Nothing else, or a leftover from an earlier size list would be
        // collected alongside the current ones.
        present.push("20x20.png");
        assert!(!is_complete_icon_set(&present));
        assert!(!is_complete_icon_set(&present[..3]));
    }

    fn png_header(width: u32, height: u32) -> Vec<u8> {
        let mut bytes = vec![0x89, b'P', b'N', b'G', 0x0d, 0x0a, 0x1a, 0x0a];
        bytes.extend_from_slice(&13u32.to_be_bytes());
        bytes.extend_from_slice(b"IHDR");
        bytes.extend_from_slice(&width.to_be_bytes());
        bytes.extend_from_slice(&height.to_be_bytes());
        bytes
    }

    #[test]
    fn contains_images_that_are_really_the_size_their_name_claims() {
        for size in ICON_SIZES {
            assert_eq!(png_dimensions(&png_header(size, size)), Some((size, size)));
        }
        // Too short to hold a header at all.
        assert_eq!(png_dimensions(&[0x89, b'P']), None);
        assert_eq!(png_dimensions(&png_header(1, 1)[..22]), None);
    }

    #[test]
    fn was_rendered_from_the_mark_the_app_actually_uses() {
        let sizes = ICON_SIZES
            .iter()
            .map(u32::to_string)
            .collect::<Vec<_>>()
            .join(",");
        let manifest: IconManifest = serde_json::from_str(&format!(
            r#"{{"source":"{ICON_SOURCE}","sha256":"abc","sizes":[{sizes}]}}"#
        ))
        .unwrap();

        assert_eq!(manifest.source, ICON_SOURCE);
        assert_eq!(manifest.sizes, ICON_SIZES);
    }

    #[test]
    fn fingerprints_the_drawing_not_the_checkout() {
        // Windows checks the SVG out with CRLF, which changes every byte after
        // the first newline without changing the artwork at all.
        assert_eq!(
            normalize_source("<svg>\r\n  <rect />\r\n</svg>\r\n"),
            normalize_source("<svg>\n  <rect />\n</svg>\n")
        );
    }

    #[test]
    fn normalises_the_same_whether_or_not_it_has_already_been_normalised() {
        let crlf = "a\r\nb\r\n";
        assert_eq!(
            normalize_source(&normalize_source(crlf)),
            normalize_source(crlf)
        );
        assert_eq!(
            normalize_source(&crlf.replace('\r', "")),
            normalize_source(crlf)
        );
    }
}
