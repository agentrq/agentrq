// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! `agentrq-ext://<extension>/<path>`: an Extension's own pages, in the side
//! panel.
//!
//! Each Extension is a host of its own, and therefore an origin of its own, so
//! its storage is its own and one Extension's page cannot read another's.
//! Registered on the panel's partitions only — the app's window never loads it.
//!
//! A file is served only from the folder of a panel page the Extension
//! declared (`provides.panels[].entry`), never the rest of the package, which
//! holds its Node code. The path is checked twice: as a string here, and as the
//! file it really is once symlinks are followed.

use std::io::{self, Read};
use std::path::{Path, PathBuf};

use url::Url;

use super::guest::{host_of, EXT_SCHEME};
use crate::uri::decode_uri_component;

/// Big enough for a bundled single-page app; bounded, because it is read whole.
pub const MAX_PANEL_FILE_BYTES: u64 = 16 * 1024 * 1024;

/// What the disk is asked. Behind a trait so the refusals that need a failing
/// or enormous file can be tested without one.
pub trait PanelFs {
    /// The file a path really is, symlinks followed.
    fn realpath(&self, path: &Path) -> io::Result<PathBuf>;
    /// Whether the path itself (not what it links to) is a file, and its size.
    fn lstat(&self, path: &Path) -> io::Result<FileStat>;
    /// Open the file once and read it: its size from the handle, then at most
    /// `limit + 1` bytes of it. Asking the path twice would let a file that
    /// grew, or was swapped, in between be read in full.
    fn read(&self, path: &Path, limit: u64) -> io::Result<FileRead>;
}

/// What one read of a file found.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FileRead {
    /// The file's size when it was opened.
    pub size: u64,
    /// At most one byte more than the limit asked for.
    pub body: Vec<u8>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FileStat {
    pub is_file: bool,
    pub size: u64,
}

/// The real disk.
pub struct StdFs;

impl PanelFs for StdFs {
    fn realpath(&self, path: &Path) -> io::Result<PathBuf> {
        std::fs::canonicalize(path)
    }
    fn lstat(&self, path: &Path) -> io::Result<FileStat> {
        let meta = std::fs::symlink_metadata(path)?;
        Ok(FileStat {
            is_file: meta.is_file(),
            size: meta.len(),
        })
    }
    fn read(&self, path: &Path, limit: u64) -> io::Result<FileRead> {
        let file = std::fs::File::open(path)?;
        let size = file.metadata()?.len();
        let mut body = Vec::new();
        file.take(limit.saturating_add(1)).read_to_end(&mut body)?;
        Ok(FileRead { size, body })
    }
}

/// A panel page an Extension declared.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PanelEntry {
    /// Relative to the installation, e.g. `panel/index.html`.
    pub entry: String,
}

/// An installed Extension.
#[derive(Debug, Clone)]
pub struct Installation {
    pub name: String,
    /// The installation's directory.
    pub dir: PathBuf,
    /// `None` is on: only an explicit `false` turns an Extension off.
    pub enabled: Option<bool>,
    /// `manifest.provides.panels`.
    pub panels: Vec<PanelEntry>,
}

/// The installations `install.js` recorded (`extensions-installed.json`'s
/// contents): what Electron's scheme is handed by `installations()`. Only
/// what the scheme reads is kept; a record with no name or folder is none.
pub fn installations(installed: &str) -> Vec<Installation> {
    let parsed: serde_json::Value = serde_json::from_str(installed).unwrap_or_default();
    let entries = |record: &serde_json::Value| -> Vec<PanelEntry> {
        let panels = record["manifest"]["provides"]["panels"].as_array();
        panels
            .into_iter()
            .flatten()
            .filter_map(|panel| panel["entry"].as_str())
            .map(|entry| PanelEntry {
                entry: entry.to_string(),
            })
            .collect()
    };
    parsed["installations"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|record| {
            Some(Installation {
                name: record["name"].as_str()?.to_string(),
                dir: record["dir"].as_str()?.into(),
                enabled: record["enabled"].as_bool(),
                panels: entries(record),
            })
        })
        .collect()
}

/// Why a file was not served.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReadFailure {
    pub status: u16,
    pub reason: String,
}

fn fail<T>(status: u16, reason: &str) -> Result<T, ReadFailure> {
    Err(ReadFailure {
        status,
        reason: reason.to_string(),
    })
}

/// A file as it is served.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PanelFile {
    pub body: Vec<u8>,
    pub mime_type: &'static str,
}

/// A segment of a manifest entry: no `.` or `..`, and nothing hidden.
/// `^[a-zA-Z0-9_-][a-zA-Z0-9._-]*$`
fn is_segment(segment: &str) -> bool {
    let mut chars = segment.chars();
    let Some(first) = chars.next() else {
        return false;
    };
    (first.is_ascii_alphanumeric() || first == '_' || first == '-')
        && chars.all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-' | '.'))
}

/// The relative path an `agentrq-ext:` URL names, or `""` when it names
/// nothing that could be served.
pub fn panel_path_of(url: &Url) -> String {
    let Some(decoded) = decode_uri_component(url.path().trim_start_matches('/')) else {
        return String::new();
    };
    if decoded.is_empty() || decoded.contains('\\') {
        return String::new();
    }
    if decoded.split('/').all(is_segment) {
        decoded
    } else {
        String::new()
    }
}

/// Node's `path.dirname` for a POSIX-style manifest entry.
fn dirname(path: &str) -> &str {
    let trimmed = path.trim_end_matches('/');
    if trimmed.is_empty() {
        return if path.is_empty() { "." } else { "/" };
    }
    match trimmed.rfind('/') {
        None => ".",
        Some(index) => {
            let parent = trimmed[..index].trim_end_matches('/');
            if parent.is_empty() {
                "/"
            } else {
                parent
            }
        }
    }
}

/// The declared panel folder `path` is inside, or `""` when it is in none of
/// them.
pub fn panel_folder_for(installation: Option<&Installation>, path: &str) -> String {
    let Some(installation) = installation else {
        return String::new();
    };
    for panel in &installation.panels {
        let folder = dirname(&panel.entry);
        if path.starts_with(&format!("{folder}/")) {
            return folder.to_string();
        }
    }
    String::new()
}

const MIME_TYPES: [(&str, &str); 18] = [
    (".html", "text/html; charset=utf-8"),
    (".js", "text/javascript; charset=utf-8"),
    (".mjs", "text/javascript; charset=utf-8"),
    (".css", "text/css; charset=utf-8"),
    (".json", "application/json; charset=utf-8"),
    (".svg", "image/svg+xml"),
    (".png", "image/png"),
    (".jpg", "image/jpeg"),
    (".jpeg", "image/jpeg"),
    (".gif", "image/gif"),
    (".webp", "image/webp"),
    (".ico", "image/x-icon"),
    (".woff", "font/woff"),
    (".woff2", "font/woff2"),
    (".ttf", "font/ttf"),
    (".wasm", "application/wasm"),
    (".map", "application/json; charset=utf-8"),
    (".txt", "text/plain; charset=utf-8"),
];

/// `mimeTypeFor` of `protocol.js`. Lives here until that file is ported.
pub fn mime_type_for(pathname: &str) -> &'static str {
    const OTHER: &str = "application/octet-stream";
    let Some(last_dot) = pathname.rfind('.') else {
        return OTHER;
    };
    if pathname.rfind('/').is_some_and(|slash| last_dot < slash) {
        return OTHER;
    }
    let extension = pathname[last_dot..].to_lowercase();
    MIME_TYPES
        .iter()
        .find(|(known, _)| *known == extension)
        .map_or(OTHER, |(_, mime)| mime)
}

/// Read one file of a panel folder, refusing anything that resolves outside it.
///
/// `dir` is the installation's directory, `folder` the declared panel folder
/// relative to it, and `path` the file, relative to the installation.
pub fn read_panel_file(
    fs: &dyn PanelFs,
    dir: &Path,
    folder: &str,
    path: &str,
) -> Result<PanelFile, ReadFailure> {
    let resolved = fs
        .realpath(&dir.join(folder))
        .and_then(|root| fs.realpath(&dir.join(path)).map(|file| (root, file)));
    let (root, file) = match resolved {
        Ok(pair) => pair,
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            return fail(404, "No such file in this extension.");
        }
        Err(_) => return fail(404, "That file cannot be read."),
    };
    // Component-wise, so `panel-evil/` beside `panel/` is not inside it — and
    // the folder itself is not a file inside itself.
    if file == root || !file.starts_with(&root) {
        return fail(404, "That file is not in a panel folder of this extension.");
    }

    let Ok(stat) = fs.lstat(&file) else {
        return fail(404, "That file cannot be read.");
    };
    if !stat.is_file {
        return fail(404, "That is not a file.");
    }
    if stat.size > MAX_PANEL_FILE_BYTES {
        return fail(413, "That file is too large to serve.");
    }
    match fs.read(&file, MAX_PANEL_FILE_BYTES) {
        Ok(read)
            if read.size > MAX_PANEL_FILE_BYTES
                || read.body.len() as u64 > MAX_PANEL_FILE_BYTES =>
        {
            fail(413, "That file is too large to serve.")
        }
        Ok(read) => Ok(PanelFile {
            body: read.body,
            mime_type: mime_type_for(path),
        }),
        Err(_) => fail(404, "That file cannot be read."),
    }
}

/// What the protocol handler answers with.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ExtResponse {
    pub status: u16,
    pub content_type: String,
    /// Set only on a served file.
    pub cache_control: Option<&'static str>,
    pub body: Vec<u8>,
}

fn refuse(status: u16, reason: &str) -> ExtResponse {
    ExtResponse {
        status,
        content_type: "text/plain; charset=utf-8".to_string(),
        cache_control: None,
        body: reason.as_bytes().to_vec(),
    }
}

/// The protocol handler: answer a request for `request_url` from what is
/// `installed` right now.
pub fn serve_ext_request(
    request_url: &str,
    installed: &[Installation],
    fs: &dyn PanelFs,
) -> ExtResponse {
    let Some(url) = Url::parse(request_url)
        .ok()
        .filter(|url| url.scheme() == EXT_SCHEME)
    else {
        return refuse(404, "Not an extension page.");
    };

    let host = host_of(&url);
    let installation = installed
        .iter()
        .find(|candidate| candidate.name == host && candidate.enabled != Some(false));
    let Some(installation) = installation else {
        return refuse(404, "That extension is not installed, or is turned off.");
    };

    let path = panel_path_of(&url);
    let folder = if path.is_empty() {
        String::new()
    } else {
        panel_folder_for(Some(installation), &path)
    };
    if folder.is_empty() {
        return refuse(404, "That file is not in a panel folder of this extension.");
    }

    match read_panel_file(fs, &installation.dir, &folder, &path) {
        Err(failure) => refuse(failure.status, &failure.reason),
        // Never cached across an update: the folder is replaced when the
        // extension is, and yesterday's script against today's code is a bug
        // report nobody can reproduce.
        Ok(file) => ExtResponse {
            status: 200,
            content_type: file.mime_type.to_string(),
            cache_control: Some("no-cache"),
            body: file.body,
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU32, Ordering};

    /// A scratch directory that is removed again.
    struct Scratch(PathBuf);

    impl Scratch {
        fn new(label: &str) -> Self {
            static NEXT: AtomicU32 = AtomicU32::new(0);
            let dir = std::env::temp_dir().join(format!(
                "agentrq-{label}-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            ));
            std::fs::create_dir_all(&dir).unwrap();
            Self(dir)
        }
    }

    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    /// An installed extension with a panel folder, its Node code beside it.
    struct Fixture {
        _dir: Scratch,
        _outside: Scratch,
        installation: Installation,
    }

    fn install() -> Fixture {
        let dir = Scratch::new("ext");
        let outside = Scratch::new("outside");
        let root = &dir.0;
        std::fs::create_dir_all(root.join("panel/assets")).unwrap();
        std::fs::create_dir_all(root.join("panel-evil")).unwrap();
        std::fs::write(root.join("index.js"), "export function apply() {}").unwrap();
        std::fs::write(root.join("agentrq-extension.json"), "{}").unwrap();
        std::fs::write(
            root.join("panel/index.html"),
            "<!doctype html><h1>Notes</h1>",
        )
        .unwrap();
        std::fs::write(root.join("panel/assets/app.js"), "console.log(1)").unwrap();
        std::fs::write(root.join("panel-evil/x.html"), "evil").unwrap();
        std::fs::write(outside.0.join("secret.txt"), "secret").unwrap();
        #[cfg(unix)]
        std::os::unix::fs::symlink(outside.0.join("secret.txt"), root.join("panel/link.txt"))
            .unwrap();
        let installation = Installation {
            name: "notes".into(),
            dir: root.clone(),
            enabled: Some(true),
            panels: vec![PanelEntry {
                entry: "panel/index.html".into(),
            }],
        };
        Fixture {
            _dir: dir,
            _outside: outside,
            installation,
        }
    }

    fn get(fixture: &Fixture, url: &str) -> ExtResponse {
        serve_ext_request(url, std::slice::from_ref(&fixture.installation), &StdFs)
    }

    fn text(response: &ExtResponse) -> String {
        String::from_utf8(response.body.clone()).unwrap()
    }

    fn path_of(raw: &str) -> String {
        panel_path_of(&Url::parse(raw).unwrap())
    }

    #[test]
    fn panel_path_reads_the_file_a_url_names() {
        assert_eq!(
            path_of("agentrq-ext://notes/panel/index.html"),
            "panel/index.html"
        );
        assert_eq!(path_of("agentrq-ext://notes/panel/my%20page.html"), "");
        assert_eq!(
            path_of("agentrq-ext://notes/panel/a%2Db.html"),
            "panel/a-b.html"
        );
    }

    #[test]
    fn panel_path_names_nothing_for_a_path_that_could_leave_the_folder_or_is_hidden() {
        for path in [
            "/",
            "/panel/.secret",
            "/panel/a%5Cb.html",
            "/panel/%E0%A4%A",
            "/panel/%2e%2e%2findex.js",
        ] {
            assert_eq!(path_of(&format!("agentrq-ext://notes{path}")), "", "{path}");
        }
    }

    #[test]
    fn panel_path_sees_an_encoded_dot_dot_already_resolved_by_the_url_where_the_folder_check_catches_it(
    ) {
        assert_eq!(
            path_of("agentrq-ext://notes/panel/%2e%2e/index.js"),
            "index.js"
        );
    }

    #[test]
    fn panel_path_rejects_an_empty_segment() {
        assert_eq!(path_of("agentrq-ext://notes/panel//index.html"), "");
    }

    #[test]
    fn panel_folder_finds_the_declared_folder_a_file_is_in() {
        let fixture = install();
        assert_eq!(
            panel_folder_for(Some(&fixture.installation), "panel/assets/app.js"),
            "panel"
        );
    }

    #[test]
    fn panel_folder_is_no_folder_for_the_rest_of_the_package_or_a_sibling_that_only_starts_the_same(
    ) {
        let fixture = install();
        let installation = Some(&fixture.installation);
        assert_eq!(panel_folder_for(installation, "index.js"), "");
        assert_eq!(panel_folder_for(installation, "panel-evil/x.html"), "");
        let no_panels = Installation {
            panels: Vec::new(),
            ..fixture.installation.clone()
        };
        assert_eq!(panel_folder_for(Some(&no_panels), "panel/index.html"), "");
        assert_eq!(panel_folder_for(None, "panel/index.html"), "");
    }

    #[test]
    fn dirname_is_nodes() {
        assert_eq!(dirname("panel/index.html"), "panel");
        assert_eq!(dirname("a/b/c.html"), "a/b");
        assert_eq!(dirname("a/b/"), "a");
        assert_eq!(dirname("index.html"), ".");
        assert_eq!(dirname("/index.html"), "/");
        assert_eq!(dirname("//"), "/");
        assert_eq!(dirname(""), ".");
        assert_eq!(dirname("/a//b"), "/a");
    }

    #[test]
    fn handler_serves_a_declared_page_and_the_files_beside_it() {
        let fixture = install();
        let page = get(&fixture, "agentrq-ext://notes/panel/index.html");
        assert_eq!(page.status, 200);
        assert_eq!(page.content_type, "text/html; charset=utf-8");
        assert_eq!(page.cache_control, Some("no-cache"));
        assert!(text(&page).contains("<h1>Notes</h1>"));

        let script = get(&fixture, "agentrq-ext://notes/panel/assets/app.js");
        assert_eq!(script.content_type, "text/javascript; charset=utf-8");
    }

    #[test]
    fn handler_never_serves_the_extensions_own_code_or_manifest() {
        let fixture = install();
        for path in [
            "index.js",
            "agentrq-extension.json",
            "panel-evil/x.html",
            "panel/%2e%2e/index.js",
        ] {
            let response = get(&fixture, &format!("agentrq-ext://notes/{path}"));
            assert_eq!(response.status, 404, "{path}");
            assert_eq!(
                text(&response),
                "That file is not in a panel folder of this extension."
            );
        }
    }

    #[cfg(unix)]
    #[test]
    fn handler_refuses_a_symlink_that_points_out_of_the_folder() {
        let fixture = install();
        let response = get(&fixture, "agentrq-ext://notes/panel/link.txt");
        assert_eq!(response.status, 404);
        assert_eq!(
            text(&response),
            "That file is not in a panel folder of this extension."
        );
    }

    #[test]
    fn handler_says_so_for_a_file_that_is_not_there_or_a_folder() {
        let fixture = install();
        assert_eq!(
            text(&get(&fixture, "agentrq-ext://notes/panel/gone.html")),
            "No such file in this extension."
        );
        assert_eq!(
            text(&get(&fixture, "agentrq-ext://notes/panel/assets")),
            "That is not a file."
        );
    }

    #[test]
    fn handler_serves_nothing_for_an_extension_that_is_not_installed_or_is_turned_off() {
        let fixture = install();
        assert_eq!(
            text(&get(&fixture, "agentrq-ext://other/panel/index.html")),
            "That extension is not installed, or is turned off."
        );
        let off = [Installation {
            enabled: Some(false),
            ..fixture.installation.clone()
        }];
        let response = serve_ext_request("agentrq-ext://notes/panel/index.html", &off, &StdFs);
        assert_eq!(response.status, 404);
    }

    #[test]
    fn handler_treats_an_extension_with_no_enabled_flag_as_on() {
        let fixture = install();
        let unset = [Installation {
            enabled: None,
            ..fixture.installation.clone()
        }];
        let response = serve_ext_request("agentrq-ext://notes/panel/index.html", &unset, &StdFs);
        assert_eq!(response.status, 200);
    }

    #[test]
    fn handler_does_not_confuse_a_port_with_the_extension() {
        let fixture = install();
        let response = get(&fixture, "agentrq-ext://notes:1/panel/index.html");
        assert_eq!(
            text(&response),
            "That extension is not installed, or is turned off."
        );
    }

    #[test]
    fn handler_serves_nothing_for_the_extension_root() {
        let fixture = install();
        let response = get(&fixture, "agentrq-ext://notes/");
        assert_eq!(response.status, 404);
        assert_eq!(
            text(&response),
            "That file is not in a panel folder of this extension."
        );
    }

    #[test]
    fn handler_answers_only_its_own_scheme() {
        let fixture = install();
        let response = get(&fixture, "https://notes/panel/index.html");
        assert_eq!(text(&response), "Not an extension page.");
        assert_eq!(response.content_type, "text/plain; charset=utf-8");
        assert_eq!(response.cache_control, None);
        // Not a URL at all: the original's `new URL` would throw.
        assert_eq!(text(&get(&fixture, "not a url")), "Not an extension page.");
    }

    /// A disk that says what a test needs it to.
    struct FakeFs {
        stat: io::Result<FileStat>,
        realpath_error: Option<io::ErrorKind>,
        read_error: bool,
        /// What the open file turns out to hold, when it differs from the stat.
        opened: Option<FileRead>,
    }

    impl FakeFs {
        fn file(size: u64) -> Self {
            Self {
                stat: Ok(FileStat {
                    is_file: true,
                    size,
                }),
                realpath_error: None,
                read_error: false,
                opened: None,
            }
        }
    }

    impl PanelFs for FakeFs {
        fn realpath(&self, path: &Path) -> io::Result<PathBuf> {
            match self.realpath_error {
                Some(kind) => Err(kind.into()),
                None => Ok(path.to_path_buf()),
            }
        }
        fn lstat(&self, _: &Path) -> io::Result<FileStat> {
            match &self.stat {
                Ok(stat) => Ok(*stat),
                Err(error) => Err(error.kind().into()),
            }
        }
        fn read(&self, _: &Path, _: u64) -> io::Result<FileRead> {
            if self.read_error {
                Err(io::ErrorKind::PermissionDenied.into())
            } else {
                Ok(self.opened.clone().unwrap_or(FileRead {
                    size: 4,
                    body: b"body".to_vec(),
                }))
            }
        }
    }

    fn read(fs: &FakeFs) -> Result<PanelFile, ReadFailure> {
        read_panel_file(fs, Path::new("/ext"), "panel", "panel/index.html")
    }

    fn failure(status: u16, reason: &str) -> Result<PanelFile, ReadFailure> {
        Err(ReadFailure {
            status,
            reason: reason.to_string(),
        })
    }

    #[test]
    fn handler_refuses_a_file_too_large_to_serve() {
        let installation = Installation {
            name: "notes".into(),
            dir: "/ext".into(),
            enabled: Some(true),
            panels: vec![PanelEntry {
                entry: "panel/index.html".into(),
            }],
        };
        let fs = FakeFs::file(MAX_PANEL_FILE_BYTES + 1);
        let response =
            serve_ext_request("agentrq-ext://notes/panel/index.html", &[installation], &fs);
        assert_eq!(response.status, 413);
        assert_eq!(text(&response), "That file is too large to serve.");

        // Exactly the cap is served.
        assert!(read(&FakeFs::file(MAX_PANEL_FILE_BYTES)).is_ok());
    }

    #[test]
    fn read_refuses_a_file_that_grew_after_it_was_measured() {
        let cap = MAX_PANEL_FILE_BYTES;
        // The stat said it fit; the open handle says it no longer does.
        let grown_size = FakeFs {
            opened: Some(FileRead {
                size: cap + 1,
                body: Vec::new(),
            }),
            ..FakeFs::file(1)
        };
        assert_eq!(
            read(&grown_size),
            failure(413, "That file is too large to serve.")
        );
        // Or the bytes that arrived exceed it though the size said otherwise.
        let grown_body = FakeFs {
            opened: Some(FileRead {
                size: 1,
                body: vec![0; cap as usize + 1],
            }),
            ..FakeFs::file(1)
        };
        assert_eq!(
            read(&grown_body),
            failure(413, "That file is too large to serve.")
        );
        // Exactly the cap read back is served.
        let exact = FakeFs {
            opened: Some(FileRead {
                size: cap,
                body: vec![0; cap as usize],
            }),
            ..FakeFs::file(1)
        };
        assert!(read(&exact).is_ok());
    }

    #[test]
    fn std_fs_reads_one_byte_past_the_limit_and_no_more() {
        let dir = std::env::temp_dir().join(format!("agentrq-extfs-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("big.bin");
        std::fs::write(&path, [7u8; 15]).unwrap();

        let read = StdFs.read(&path, 10).unwrap();
        assert_eq!((read.size, read.body.len()), (15, 11));
        let whole = StdFs.read(&path, 20).unwrap();
        assert_eq!((whole.size, whole.body.len()), (15, 15));
        assert!(StdFs.read(&dir.join("missing"), 10).is_err());
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn read_says_a_file_cannot_be_read_when_the_disk_refuses() {
        let denied = FakeFs {
            realpath_error: Some(io::ErrorKind::PermissionDenied),
            ..FakeFs::file(1)
        };
        assert_eq!(read(&denied), failure(404, "That file cannot be read."));

        let stat_fails = FakeFs {
            stat: Err(io::ErrorKind::PermissionDenied.into()),
            ..FakeFs::file(1)
        };
        assert_eq!(read(&stat_fails), failure(404, "That file cannot be read."));

        let read_fails = FakeFs {
            read_error: true,
            ..FakeFs::file(1)
        };
        assert_eq!(read(&read_fails), failure(404, "That file cannot be read."));
    }

    #[test]
    fn read_refuses_the_folder_itself_as_a_file() {
        let fs = FakeFs::file(1);
        assert_eq!(
            read_panel_file(&fs, Path::new("/ext"), "panel", "panel"),
            failure(404, "That file is not in a panel folder of this extension.")
        );
    }

    #[test]
    fn mime_type_follows_the_last_extension_of_the_last_segment() {
        assert_eq!(
            mime_type_for("panel/index.html"),
            "text/html; charset=utf-8"
        );
        assert_eq!(
            mime_type_for("panel/APP.JS"),
            "text/javascript; charset=utf-8"
        );
        assert_eq!(mime_type_for("panel/logo.png"), "image/png");
        assert_eq!(mime_type_for("panel/data.bin"), "application/octet-stream");
        assert_eq!(mime_type_for("panel/noext"), "application/octet-stream");
        assert_eq!(mime_type_for("a.b/noext"), "application/octet-stream");
    }

    #[test]
    fn reads_what_install_js_recorded() {
        let installed = r#"{"installations":[
            {"name":"notes","dir":"/x/notes","manifest":{"provides":{"panels":[
                {"id":"n","entry":"panel/index.html"},{"id":"bad"}]}}},
            {"name":"off","dir":"/x/off","enabled":false},
            {"name":"on","dir":"/x/on","enabled":true,"manifest":{}},
            {"name":"nodir"},
            {"dir":"/x/noname"}
        ]}"#;
        let read = installations(installed);
        let seen: Vec<_> = read
            .iter()
            .map(|i| (i.name.as_str(), i.dir.clone(), i.enabled, i.panels.clone()))
            .collect();
        assert_eq!(
            seen,
            vec![
                (
                    "notes",
                    PathBuf::from("/x/notes"),
                    None,
                    vec![PanelEntry {
                        entry: "panel/index.html".into()
                    }]
                ),
                ("off", PathBuf::from("/x/off"), Some(false), vec![]),
                ("on", PathBuf::from("/x/on"), Some(true), vec![]),
            ]
        );
        for broken in ["", "{", "[]", r#"{"installations":{}}"#] {
            assert!(installations(broken).is_empty(), "{broken}");
        }
    }
}
