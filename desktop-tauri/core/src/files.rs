// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Opening a local file that a message linked to.
//!
//! The renderer never navigates to a `file:` URL: it asks the shell to open
//! one, and this decides what "open" may mean. The link came from message
//! content, which an agent wrote and which can be wrong or hostile, so only
//! file types that are *read* by their default application open. Everything
//! else is revealed in the file manager, where the person can see what it is.
//! A revealed file has been shown, not run.
//!
//! The list is an allowlist: an unknown extension is not assumed harmless.

use crate::uri::decode_uri_component;
use url::Url;

/// What may be done with a path.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FileOpenAction {
    /// Hand to the default application.
    Open,
    /// Show in the file manager without opening it.
    Reveal,
}

/// Extensions whose default application displays the file rather than
/// executing it. Notably absent, and deliberately: `html` and `svg` (script and
/// markup the author chose), `sh`, `command`, `app`, `exe`, `bat`, `scpt` (they
/// run), archives (they invite a double-click on what is inside) and files with
/// no extension (the platform decides from metadata nobody has looked at).
const READABLE_EXTENSIONS: &[&str] = &[
    // Text and source
    "txt", "md", "markdown", "rst", "adoc", "org", "log", "csv", "tsv", "json", "jsonl", "yaml",
    "yml", "toml", "ini", "cfg", "conf", "env", "xml", "go", "mod", "sum", "js", "mjs", "cjs",
    "jsx", "ts", "tsx", "vue", "svelte", "py", "rb", "rs", "java", "kt", "swift", "c", "h", "cc",
    "cpp", "hpp", "cs", "php", "sql", "graphql", "proto", "diff", "patch", "css", "scss", "less",
    // Documents
    "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "rtf", "pages",
    "numbers", "key", "epub", // Media
    "png", "jpg", "jpeg", "gif", "webp", "bmp", "tif", "tiff", "heic", "avif", "mp3", "wav", "m4a",
    "flac", "aac", "ogg", "mp4", "mov", "m4v", "webm", "mkv", "avi",
];

/// The filesystem path a `file:` URL names, or `""` for anything that is not a
/// local file URL, including `file://server/share/x`: that names another
/// machine, and reaching for it would hang on a network mount at best.
pub fn local_path_from_file_url(raw_url: &str) -> String {
    let Ok(url) = Url::parse(raw_url) else {
        return String::new();
    };
    if url.scheme() != "file" {
        return String::new();
    }
    match url.host_str() {
        None | Some("" | "localhost") => {}
        Some(_) => return String::new(),
    }

    // A stray `%` in a filename is not a reason to refuse the whole path.
    let mut path = decode_uri_component(url.path()).unwrap_or_else(|| url.path().to_string());

    // Windows drive paths arrive as `/C:/Users/…`; the leading slash belongs to
    // the URL form, not the path.
    let bytes = path.as_bytes();
    if bytes.len() >= 3 && bytes[0] == b'/' && bytes[1].is_ascii_alphabetic() && bytes[2] == b':' {
        path.remove(0);
    }

    // `file:///%5C%5Chost` has no host, but decodes to a UNC path on Windows:
    // the same other machine, by another spelling.
    let unc = path
        .chars()
        .take(2)
        .filter(|c| matches!(c, '/' | '\\'))
        .count()
        == 2;
    // A NUL byte truncates the path inside the platform's own file APIs, so
    // what gets opened would not be what was inspected here.
    if unc || path.contains('\0') {
        String::new()
    } else {
        path
    }
}

/// What may be done with a path, from its name alone.
pub fn file_open_action(path: &str, is_directory: bool) -> FileOpenAction {
    // A folder has no default application to run; opening one shows its contents.
    if is_directory {
        return FileOpenAction::Open;
    }

    let name = path.rsplit(['/', '\\']).next().unwrap_or("");
    // A leading dot is `.zshrc`: a name, not an extension.
    let extension = match name.rfind('.') {
        None | Some(0) => return FileOpenAction::Reveal,
        Some(dot) => name[dot + 1..].to_lowercase(),
    };
    if READABLE_EXTENSIONS.contains(&extension.as_str()) {
        FileOpenAction::Open
    } else {
        FileOpenAction::Reveal
    }
}

/// The operating system, for the part of opening a file that is not a rule.
pub trait FileShell {
    /// `Some(is_directory)` when the path exists, `None` when it cannot be
    /// looked at. "Gone" and "cannot look" are deliberately not told apart.
    fn stat(&self, path: &str) -> Option<bool>;
    /// Show the item in the file manager.
    fn reveal(&self, path: &str);
    /// Open with the default application. `Err` carries the platform's message.
    fn open(&self, path: &str) -> Result<(), String>;
}

/// What the renderer is told after asking to open a file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OpenOutcome {
    Done { revealed: bool },
    Failed { error: String },
}

/// Open or reveal the file a `file:` link names, per the allowlist.
pub fn open_file_link<S: FileShell>(shell: &S, raw_url: &str) -> OpenOutcome {
    let path = local_path_from_file_url(raw_url);
    if path.is_empty() {
        return OpenOutcome::Failed {
            error: "That link does not point at a file on this computer.".to_string(),
        };
    }

    let Some(is_directory) = shell.stat(&path) else {
        return OpenOutcome::Failed {
            error: format!("No such file: {path}"),
        };
    };

    if file_open_action(&path, is_directory) == FileOpenAction::Reveal {
        shell.reveal(&path);
        return OpenOutcome::Done { revealed: true };
    }

    match shell.open(&path) {
        Ok(()) => OpenOutcome::Done { revealed: false },
        Err(error) => OpenOutcome::Failed { error },
    }
}

impl OpenOutcome {
    /// The shape the renderer reads: `{ ok, revealed }` or `{ ok: false, error }`.
    pub fn to_json(&self) -> serde_json::Value {
        match self {
            Self::Done { revealed } => serde_json::json!({ "ok": true, "revealed": revealed }),
            Self::Failed { error } => serde_json::json!({ "ok": false, "error": error }),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    #[test]
    fn reads_an_ordinary_local_file_url() {
        assert_eq!(
            local_path_from_file_url("file:///Users/mt/brain/skills_support_plan.md"),
            "/Users/mt/brain/skills_support_plan.md"
        );
    }

    #[test]
    fn decodes_percent_escapes_so_the_path_is_the_one_on_disk() {
        assert_eq!(
            local_path_from_file_url("file:///Users/mt/My%20Notes/a%2Bb.md"),
            "/Users/mt/My Notes/a+b.md"
        );
    }

    #[test]
    fn accepts_localhost_which_is_this_machine_spelled_out() {
        assert_eq!(
            local_path_from_file_url("file://localhost/etc/hosts"),
            "/etc/hosts"
        );
    }

    #[test]
    fn refuses_a_host_that_is_another_machine() {
        assert_eq!(
            local_path_from_file_url("file://fileserver/share/report.pdf"),
            ""
        );
        // No host, but a UNC path once Windows reads it.
        // The parser folds `file:////host` into a rooted path, not a UNC one.
        assert_eq!(
            local_path_from_file_url("file:////fileserver/share/report.pdf"),
            "/fileserver/share/report.pdf"
        );
        for raw in [
            "file:///%5C%5Cfileserver%5Cshare%5Creport.pdf",
            "file:///%5Cfileserver%5Cshare%5Creport.pdf",
            "file:///%2F%2Ffileserver/share/report.pdf",
        ] {
            assert_eq!(local_path_from_file_url(raw), "", "{raw}");
        }
        assert_eq!(
            local_path_from_file_url("file:///C:/Users/me/report.pdf"),
            "C:/Users/me/report.pdf"
        );
    }

    #[test]
    fn refuses_every_other_scheme() {
        assert_eq!(local_path_from_file_url("https://example.com/a.md"), "");
        assert_eq!(local_path_from_file_url("app://agentrq/tasks/1"), "");
        assert_eq!(local_path_from_file_url("javascript:alert(1)"), "");
    }

    #[test]
    fn refuses_junk() {
        // null collapses to the empty string, as in JS.
        assert_eq!(local_path_from_file_url("/Users/mt/plan.md"), "");
        assert_eq!(local_path_from_file_url(""), "");
    }

    #[test]
    fn refuses_a_path_carrying_a_nul_byte() {
        assert_eq!(
            local_path_from_file_url("file:///Users/mt/plan.md%00.command"),
            ""
        );
    }

    #[test]
    fn keeps_a_malformed_escape_rather_than_refusing_the_file() {
        assert_eq!(
            local_path_from_file_url("file:///tmp/100%.md"),
            "/tmp/100%.md"
        );
    }

    #[test]
    fn drops_the_slash_the_url_form_adds_to_a_windows_drive_path() {
        assert_eq!(
            local_path_from_file_url("file:///C:/Users/mt/plan.md"),
            "C:/Users/mt/plan.md"
        );
    }

    fn action(path: &str) -> FileOpenAction {
        file_open_action(path, false)
    }

    #[test]
    fn opens_the_documents_people_actually_link_to() {
        for path in [
            "/Users/mt/skills_support_plan.md",
            "/Users/mt/notes.txt",
            "/Users/mt/report.pdf",
            "/Users/mt/data.csv",
            "/Users/mt/main.go",
            "/Users/mt/screenshot.png",
            "/Users/mt/demo.mp4",
            "/Users/mt/deck.pptx",
        ] {
            assert_eq!(action(path), FileOpenAction::Open, "{path}");
        }
    }

    #[test]
    fn is_not_fooled_by_an_uppercase_extension() {
        assert_eq!(action("/Users/mt/PLAN.MD"), FileOpenAction::Open);
    }

    #[test]
    fn reveals_anything_that_would_run_rather_than_open_it() {
        for path in [
            "/Users/mt/install.sh",
            "/Users/mt/setup.command",
            "/Applications/Calculator.app",
            "/Users/mt/setup.exe",
            "/Users/mt/go.bat",
            "/Users/mt/thing.scpt",
        ] {
            assert_eq!(action(path), FileOpenAction::Reveal, "{path}");
        }
    }

    #[test]
    fn reveals_markup_that_opens_in_a_browser() {
        assert_eq!(action("/Users/mt/report.html"), FileOpenAction::Reveal);
        assert_eq!(action("/Users/mt/icon.svg"), FileOpenAction::Reveal);
    }

    #[test]
    fn reveals_an_archive_rather_than_inviting_a_click_on_what_is_inside() {
        assert_eq!(action("/Users/mt/bundle.zip"), FileOpenAction::Reveal);
    }

    #[test]
    fn reveals_an_extension_nobody_listed() {
        assert_eq!(action("/Users/mt/thing.wat"), FileOpenAction::Reveal);
    }

    #[test]
    fn reveals_a_file_with_no_extension_where_the_platform_decides() {
        assert_eq!(action("/Users/mt/Makefile"), FileOpenAction::Reveal);
    }

    #[test]
    fn treats_a_leading_dot_as_a_name_not_an_extension() {
        assert_eq!(action("/Users/mt/.zshrc"), FileOpenAction::Reveal);
    }

    #[test]
    fn does_not_read_an_extension_out_of_a_directory_name() {
        assert_eq!(action("/Users/mt/notes.md/run"), FileOpenAction::Reveal);
    }

    #[test]
    fn opens_a_folder_which_has_nothing_to_run() {
        assert_eq!(
            file_open_action("/Users/mt/brain", true),
            FileOpenAction::Open
        );
        assert_eq!(
            file_open_action("/Users/mt/scripts.sh", true),
            FileOpenAction::Open
        );
    }

    #[test]
    fn reads_the_extension_off_a_windows_path_too() {
        assert_eq!(action("C:\\Users\\mt\\plan.md"), FileOpenAction::Open);
        assert_eq!(action("C:\\Users\\mt\\setup.exe"), FileOpenAction::Reveal);
    }

    #[test]
    fn reveals_nothing_it_cannot_name() {
        // null collapses to the empty string, as in JS.
        assert_eq!(action(""), FileOpenAction::Reveal);
    }

    // The handler in index.js has no vitest file; these cover the same flow
    // behind the `FileShell` trait.
    struct FakeShell {
        stat: Option<bool>,
        open: Result<(), String>,
        calls: RefCell<Vec<String>>,
    }

    impl FakeShell {
        fn new(stat: Option<bool>, open: Result<(), String>) -> Self {
            Self {
                stat,
                open,
                calls: RefCell::new(Vec::new()),
            }
        }
    }

    impl FileShell for FakeShell {
        fn stat(&self, _path: &str) -> Option<bool> {
            self.stat
        }
        fn reveal(&self, path: &str) {
            self.calls.borrow_mut().push(format!("reveal {path}"));
        }
        fn open(&self, path: &str) -> Result<(), String> {
            self.calls.borrow_mut().push(format!("open {path}"));
            self.open.clone()
        }
    }

    #[test]
    fn opens_a_readable_file_through_the_shell() {
        let shell = FakeShell::new(Some(false), Ok(()));
        assert_eq!(
            open_file_link(&shell, "file:///Users/mt/plan.md"),
            OpenOutcome::Done { revealed: false }
        );
        assert_eq!(*shell.calls.borrow(), ["open /Users/mt/plan.md"]);
    }

    #[test]
    fn reveals_a_file_that_would_run_and_never_opens_it() {
        let shell = FakeShell::new(Some(false), Ok(()));
        assert_eq!(
            open_file_link(&shell, "file:///Users/mt/setup.command"),
            OpenOutcome::Done { revealed: true }
        );
        assert_eq!(*shell.calls.borrow(), ["reveal /Users/mt/setup.command"]);
    }

    #[test]
    fn reports_a_path_that_is_not_there() {
        let shell = FakeShell::new(None, Ok(()));
        assert_eq!(
            open_file_link(&shell, "file:///Users/mt/gone.md"),
            OpenOutcome::Failed {
                error: "No such file: /Users/mt/gone.md".to_string()
            }
        );
        assert!(shell.calls.borrow().is_empty());
    }

    #[test]
    fn reports_the_platforms_message_when_open_fails() {
        let shell = FakeShell::new(Some(false), Err("no handler".to_string()));
        assert_eq!(
            open_file_link(&shell, "file:///Users/mt/plan.md"),
            OpenOutcome::Failed {
                error: "no handler".to_string()
            }
        );
    }

    #[test]
    fn refuses_a_link_that_is_not_a_local_file() {
        let shell = FakeShell::new(Some(false), Ok(()));
        assert_eq!(
            open_file_link(&shell, "https://example.com/plan.md"),
            OpenOutcome::Failed {
                error: "That link does not point at a file on this computer.".to_string()
            }
        );
        assert!(shell.calls.borrow().is_empty());
    }

    #[test]
    fn an_outcome_goes_to_the_renderer_as_ok_or_an_error() {
        assert_eq!(
            OpenOutcome::Done { revealed: true }.to_json(),
            serde_json::json!({ "ok": true, "revealed": true })
        );
        assert_eq!(
            OpenOutcome::Failed {
                error: "No such file: /x".to_string()
            }
            .to_json(),
            serde_json::json!({ "ok": false, "error": "No such file: /x" })
        );
    }
}
