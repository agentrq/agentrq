// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Attachment bytes on disk, for the desktop build.
//!
//! On the web an attachment is kept by the service worker. The desktop renderer
//! is built without one, so every image and PDF would be re-downloaded on every
//! view. The webview's own HTTP cache cannot stand in: the attachment endpoint
//! sends no `Cache-Control` and no `ETag`, and a browser cache is not
//! addressable per workspace, while "Clear this workspace" makes a promise. So
//! the bytes live in a store we own.
//!
//! ## Layout
//!
//! ```text
//! <dir>/<workspaceId>/<taskId>/<attachmentId>
//! <dir>/<workspaceId>/<taskId>/<attachmentId>.meta
//! ```
//!
//! The same shape as the URL the bytes came from. The attachment id *is* the
//! filename, because it already names that exact file for all time. Nesting by
//! task makes removing anything a directory removal rather than a scan.
//!
//! ## Why only base62 ever reaches the filesystem
//!
//! Both ids come out of a URL, and a path built from unvalidated input is how a
//! cache becomes a way to write anywhere on disk. They are monoflake ids, digits
//! and letters, so anything that is not plain alphanumeric is refused rather than
//! escaped. A refused path is simply not cached, which the caller already handles.
//!
//! Rust notes: every disk call is behind [`StoreFs`], so the rules run against
//! an in-memory fake. The size cap, the eviction plan and the attachment-path test come from
//! `frontend/src/composables/useAttachmentCache.js`, which the JS store imports;
//! they are ported here beside their only user in the shell.

use serde::Serialize;
use serde_json::Value;
use std::io;
use std::sync::atomic::{AtomicU64, Ordering};

/// Attachment bytes above this are streamed every time rather than kept.
pub const MAX_ATTACHMENT_BYTES: u64 = 2 * 1024 * 1024;

/// How much of this device to spend on attachment bytes on the web.
pub const WEB_BUDGET_BYTES: u64 = 100 * 1024 * 1024;

/// The desktop writes into a folder it owns and is under no browser quota
/// pressure, so it can afford an order of magnitude more than a web origin.
pub const DESKTOP_BUDGET_BYTES: u64 = 1024 * 1024 * 1024;

/// Suffix of the file holding a cached response's content type.
const META: &str = ".meta";

pub fn budget_for(platform: &str) -> u64 {
    if platform == "desktop" {
        DESKTOP_BUDGET_BYTES
    } else {
        WEB_BUDGET_BYTES
    }
}

/// Whether a response is small enough to keep.
///
/// Judged from the length the caller measured, so the decision costs nothing
/// beyond what has already been read. A response with no length is kept,
/// because refusing everything unmeasurable would mean caching almost nothing.
pub fn within_size_cap(declared: Option<u64>, max: u64) -> bool {
    declared.is_none_or(|size| size <= max)
}

/// One stored entry, as the eviction plan sees it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EvictionEntry {
    pub url: String,
    pub size: u64,
}

/// Which entries to drop to get back under budget, least-recently-used first.
///
/// `entries` arrive oldest first. Nothing is dropped while the total fits, and
/// the newest entry is never dropped to make room for itself.
pub fn eviction_plan(entries: &[EvictionEntry], budget: u64) -> Vec<String> {
    let mut total: u64 = entries.iter().map(|entry| entry.size).sum();
    if total <= budget {
        return Vec::new();
    }

    let mut doomed = Vec::new();
    for entry in entries {
        if total <= budget || doomed.len() == entries.len() - 1 {
            break;
        }
        doomed.push(entry.url.clone());
        total -= entry.size;
    }
    doomed
}

/// Whether a path is an attachment's: `…/tasks/:id/attachments/:id`.
pub fn is_attachment_request(pathname: &str) -> bool {
    let segments: Vec<&str> = pathname.split('/').collect();
    matches!(
        segments.as_slice(),
        [_, .., "tasks", task, "attachments", attachment] if !task.is_empty() && !attachment.is_empty()
    )
}

/// Whether a proxied request goes through the store at all: a GET for an
/// attachment. Everything else is forwarded untouched.
pub fn should_serve_from_store(method: &str, pathname: &str) -> bool {
    method == "GET" && is_attachment_request(pathname)
}

/// Whether a proxied response is worth keeping. Only a successful body is; an
/// error page cached under an attachment's name would outlive whatever caused it.
pub fn should_cache_response(status: u16) -> bool {
    status == 200
}

/// The workspace, task and attachment an attachment request names.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AttachmentId {
    pub workspace_id: String,
    pub task_id: String,
    pub attachment_id: String,
}

/// Ids are base62 monoflakes. Nothing else may become a path segment.
fn is_safe_id(id: &str) -> bool {
    !id.is_empty() && id.bytes().all(|b| b.is_ascii_alphanumeric())
}

/// The workspace, task and attachment an attachment request names, or `None`.
///
/// `None` covers both "not an attachment path" and "an attachment path carrying
/// something that must not become a filename", because the caller does the same
/// thing for each: forward without caching.
pub fn parse_attachment_path(pathname: &str) -> Option<AttachmentId> {
    let segments: Vec<&str> = pathname.split('/').collect();
    // `/workspaces/:w/tasks/:t/attachments/:a` at the end, with something
    // before the first slash of it.
    let [_, .., "workspaces", workspace_id, "tasks", task_id, "attachments", attachment_id] =
        segments.as_slice()
    else {
        return None;
    };
    // Every one of these becomes a path segment, so every one is checked.
    if ![workspace_id, task_id, attachment_id]
        .iter()
        .all(|part| is_safe_id(part))
    {
        return None;
    }
    Some(AttachmentId {
        workspace_id: workspace_id.to_string(),
        task_id: task_id.to_string(),
        attachment_id: attachment_id.to_string(),
    })
}

/// What a file's metadata says.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct FileStat {
    pub size: u64,
    /// Milliseconds since the epoch; the entry's last use.
    pub modified_ms: f64,
}

/// The disk, implemented by the shell. Paths use `/`, as the JS builds them.
pub trait StoreFs {
    /// Fails when the directory is absent or unreadable.
    fn read_dir(&self, path: &str) -> io::Result<Vec<String>>;
    fn stat(&self, path: &str) -> io::Result<FileStat>;
    fn read(&self, path: &str) -> io::Result<Vec<u8>>;
    fn write(&self, path: &str, body: &[u8]) -> io::Result<()>;
    fn create_dir_all(&self, path: &str) -> io::Result<()>;
    /// Removes a file; one that is already gone is not an error.
    fn remove_file(&self, path: &str) -> io::Result<()>;
    /// Removes a directory and everything under it; one already gone is not an
    /// error.
    fn remove_dir_all(&self, path: &str) -> io::Result<()>;
    /// Mark a file as used now. The JS's `fs.utimes?.`: a filesystem that
    /// cannot record a use answers `Ok`, and a failure is ignored by the store.
    fn touch(&self, path: &str) -> io::Result<()>;
}

/// A stored response, ready to serve.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StoredAttachment {
    pub body: Vec<u8>,
    pub content_type: String,
    pub content_disposition: String,
}

/// What a response is stored with.
#[derive(Debug, Clone, Copy, Default)]
pub struct WriteOptions<'a> {
    pub content_type: &'a str,
    pub content_disposition: &'a str,
    /// The length the caller measured, if it did.
    pub size: Option<u64>,
}

/// The `.meta` file's contents; field order is the on-disk order.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct Meta<'a> {
    content_type: &'a str,
    content_disposition: &'a str,
}

struct Entry {
    path: String,
    size: u64,
    used_at: f64,
}

/// A cache of attachment bytes in one directory, for one profile.
pub struct AttachmentStore<F: StoreFs> {
    dir: String,
    fs: F,
    budget: u64,
}

impl<F: StoreFs> AttachmentStore<F> {
    /// A store with the desktop budget.
    pub fn new(dir: &str, fs: F) -> Self {
        Self::with_budget(dir, fs, budget_for("desktop"))
    }

    pub fn with_budget(dir: &str, fs: F, budget: u64) -> Self {
        Self {
            dir: dir.to_string(),
            fs,
            budget,
        }
    }

    fn workspace_dir(&self, workspace_id: &str) -> String {
        format!("{}/{workspace_id}", self.dir)
    }

    fn task_dir(&self, id: &AttachmentId) -> String {
        format!("{}/{}", self.workspace_dir(&id.workspace_id), id.task_id)
    }

    fn file_path(&self, id: &AttachmentId) -> String {
        format!("{}/{}", self.task_dir(id), id.attachment_id)
    }

    /// What a directory holds, or nothing when it cannot be read.
    fn names_in(&self, path: &str) -> Vec<String> {
        self.fs.read_dir(path).unwrap_or_default()
    }

    /// Every stored attachment across every workspace, least recently used first.
    fn entries(&self) -> Vec<Entry> {
        let mut found = Vec::new();
        // No directory yet is the ordinary state on a first run, not a failure.
        for workspace_id in self.names_in(&self.dir) {
            if !is_safe_id(&workspace_id) {
                continue;
            }
            for task_id in self.names_in(&self.workspace_dir(&workspace_id)) {
                if !is_safe_id(&task_id) {
                    continue;
                }
                let dir_of_task = format!("{}/{task_id}", self.workspace_dir(&workspace_id));
                for name in self.names_in(&dir_of_task) {
                    if name.ends_with(META) || !is_safe_id(&name) {
                        continue;
                    }
                    let path = format!("{dir_of_task}/{name}");
                    // Vanished between listing and stat, which is not worth an error.
                    if let Ok(stat) = self.fs.stat(&path) {
                        found.push(Entry {
                            path,
                            size: stat.size,
                            used_at: stat.modified_ms,
                        });
                    }
                }
            }
        }
        found.sort_by(|a, b| a.used_at.total_cmp(&b.used_at));
        found
    }

    /// Remove an entry and the file describing it, ignoring either being gone.
    fn remove(&self, path: &str) {
        let _ = self.fs.remove_file(path);
        let _ = self.fs.remove_file(&format!("{path}{META}"));
    }

    /// The stored response for a path, or `None`.
    ///
    /// `None` covers every reason a hit cannot be served (never stored, half
    /// written, meta unreadable, disk error, an id that must not become a
    /// filename) because the caller does the same thing in all of them: forward
    /// to the server, which is what it would have done anyway.
    pub fn read(&self, pathname: &str) -> Option<StoredAttachment> {
        let id = parse_attachment_path(pathname)?;

        let path = self.file_path(&id);
        let body = self.fs.read(&path).ok()?;
        let meta: Value =
            serde_json::from_slice(&self.fs.read(&format!("{path}{META}")).ok()?).ok()?;
        let content_type = meta
            .get("contentType")?
            .as_str()
            .filter(|s| !s.is_empty())?;

        // Serving makes this the most recently used entry. Best effort: failing
        // to record a hit costs a slightly worse eviction choice later, which is
        // not worth failing the read over.
        let _ = self.fs.touch(&path);

        Some(StoredAttachment {
            body,
            content_type: content_type.to_string(),
            content_disposition: meta
                .get("contentDisposition")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string(),
        })
    }

    /// Keep a response, unless it is too large to be worth the space.
    ///
    /// Returns whether it was kept.
    pub fn write(&self, pathname: &str, body: &[u8], options: WriteOptions) -> bool {
        let Some(id) = parse_attachment_path(pathname) else {
            return false;
        };
        if options.content_type.is_empty() || !within_size_cap(options.size, MAX_ATTACHMENT_BYTES) {
            return false;
        }

        let path = self.file_path(&id);
        let meta = serde_json::to_string(&Meta {
            content_type: options.content_type,
            content_disposition: options.content_disposition,
        })
        .unwrap_or_default();
        let stored = self
            .fs
            .create_dir_all(&self.task_dir(&id))
            .and_then(|()| self.fs.write(&path, body))
            .and_then(|()| self.fs.write(&format!("{path}{META}"), meta.as_bytes()));
        // A cache that cannot be written behaves like one that was never
        // written, which the caller already handles.
        if stored.is_err() {
            return false;
        }

        self.evict();
        true
    }

    /// Drop the least recently used entries until the store fits its budget.
    pub fn evict(&self) -> usize {
        let found: Vec<EvictionEntry> = self
            .entries()
            .into_iter()
            .map(|entry| EvictionEntry {
                url: entry.path,
                size: entry.size,
            })
            .collect();
        let doomed = eviction_plan(&found, self.budget);
        for path in &doomed {
            self.remove(path);
        }
        doomed.len()
    }

    /// Forget one task's attachments.
    ///
    /// A directory removal, because the task *is* a directory. Deleting a task
    /// has to reach its bytes as well as its rows, or the bytes outlive the
    /// task that explained them.
    pub fn forget_task(&self, workspace_id: &str, task_id: &str) -> bool {
        if !is_safe_id(workspace_id) || !is_safe_id(task_id) {
            return false;
        }
        self.fs
            .remove_dir_all(&format!("{}/{task_id}", self.workspace_dir(workspace_id)))
            .is_ok()
    }

    /// Forget one workspace's attachments, leaving every other workspace's alone.
    ///
    /// One directory removal, because the workspace *is* a directory.
    pub fn forget_workspace(&self, workspace_id: &str) -> bool {
        is_safe_id(workspace_id)
            && self
                .fs
                .remove_dir_all(&self.workspace_dir(workspace_id))
                .is_ok()
    }

    /// Everything, for signing out.
    pub fn forget_all(&self) -> bool {
        self.fs.remove_dir_all(&self.dir).is_ok()
    }
}

/// The real disk. Paths are the store's own `/`-joined strings.
pub struct DiskFs;

impl DiskFs {
    fn ignore_missing(result: io::Result<()>) -> io::Result<()> {
        match result {
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
            other => other,
        }
    }
}

impl StoreFs for DiskFs {
    fn read_dir(&self, path: &str) -> io::Result<Vec<String>> {
        std::fs::read_dir(path)?
            .map(|entry| entry.map(|e| e.file_name().to_string_lossy().into_owned()))
            .collect()
    }

    fn stat(&self, path: &str) -> io::Result<FileStat> {
        let meta = std::fs::metadata(path)?;
        let modified_ms = meta
            .modified()?
            .duration_since(std::time::UNIX_EPOCH)
            .map_or(0.0, |d| d.as_secs_f64() * 1000.0);
        Ok(FileStat {
            size: meta.len(),
            modified_ms,
        })
    }

    fn read(&self, path: &str) -> io::Result<Vec<u8>> {
        std::fs::read(path)
    }

    /// Whole or not at all: a reader of a miss being written beside it would
    /// otherwise serve a truncated body as a 200. The sibling's name is unique,
    /// so two misses for one attachment never write the same file, and its `.`
    /// keeps it out of the store's listing.
    fn write(&self, path: &str, body: &[u8]) -> io::Result<()> {
        static NEXT: AtomicU64 = AtomicU64::new(0);
        let part = format!(
            "{path}.{}-{}.part",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        );
        std::fs::write(&part, body)?;
        std::fs::rename(&part, path).inspect_err(|_| {
            let _ = std::fs::remove_file(&part);
        })
    }

    fn create_dir_all(&self, path: &str) -> io::Result<()> {
        std::fs::create_dir_all(path)
    }

    fn remove_file(&self, path: &str) -> io::Result<()> {
        Self::ignore_missing(std::fs::remove_file(path))
    }

    fn remove_dir_all(&self, path: &str) -> io::Result<()> {
        Self::ignore_missing(std::fs::remove_dir_all(path))
    }

    fn touch(&self, path: &str) -> io::Result<()> {
        std::fs::File::options()
            .write(true)
            .open(path)?
            .set_modified(std::time::SystemTime::now())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::{Cell, RefCell};
    use std::collections::BTreeMap;

    fn path(ws: &str, att: &str) -> String {
        format!("/api/v1/workspaces/{ws}/tasks/0iCYTqxKOqv/attachments/{att}")
    }

    fn a() -> String {
        path("0iCYS9XKlnN", "0iCt6L19zvN")
    }
    fn b() -> String {
        path("0iCYS9XKlnN", "0iCt6L19zvP")
    }
    fn other_ws() -> String {
        path("0aBcDeFgHiJ", "0iCt6L19zvQ")
    }

    const A_FILE: &str = "/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN";

    /// An in-memory filesystem with the surface the store uses, plus switches
    /// that make one call fail.
    #[derive(Default)]
    struct FakeFs {
        files: RefCell<BTreeMap<String, Vec<u8>>>,
        times: RefCell<BTreeMap<String, f64>>,
        clock: Cell<f64>,
        fail_write: Cell<bool>,
        fail_rm: Cell<bool>,
        fail_touch: Cell<bool>,
        no_touch: Cell<bool>,
        fail_stat_suffix: RefCell<Option<&'static str>>,
        fail_read_dir_below: Cell<Option<usize>>,
    }

    impl FakeFs {
        fn with(initial: &[(&str, &str)]) -> Self {
            let fs = Self::default();
            for (path, body) in initial {
                fs.files
                    .borrow_mut()
                    .insert(path.to_string(), body.as_bytes().to_vec());
            }
            fs
        }

        fn tick(&self) -> f64 {
            self.clock.set(self.clock.get() + 1.0);
            1000.0 + self.clock.get()
        }

        fn keys(&self) -> Vec<String> {
            self.files.borrow().keys().cloned().collect()
        }
    }

    fn enoent() -> io::Error {
        io::Error::new(io::ErrorKind::NotFound, "ENOENT")
    }

    impl StoreFs for &FakeFs {
        fn read_dir(&self, dir: &str) -> io::Result<Vec<String>> {
            if let Some(limit) = self.fail_read_dir_below.get() {
                if dir.split('/').count() > limit {
                    return Err(io::Error::other("EACCES"));
                }
            }
            let prefix = format!("{dir}/");
            let mut names: Vec<String> = Vec::new();
            for key in self.files.borrow().keys() {
                if let Some(rest) = key.strip_prefix(&prefix) {
                    let name = rest.split('/').next().unwrap().to_string();
                    if !names.contains(&name) {
                        names.push(name);
                    }
                }
            }
            if names.is_empty() {
                Err(enoent())
            } else {
                Ok(names)
            }
        }

        fn stat(&self, p: &str) -> io::Result<FileStat> {
            if self
                .fail_stat_suffix
                .borrow()
                .is_some_and(|s| p.ends_with(s))
            {
                return Err(io::Error::other("ENOENT: removed mid-sweep"));
            }
            let files = self.files.borrow();
            let body = files.get(p).ok_or_else(enoent)?;
            Ok(FileStat {
                size: body.len() as u64,
                modified_ms: self.times.borrow().get(p).copied().unwrap_or(0.0),
            })
        }

        fn read(&self, p: &str) -> io::Result<Vec<u8>> {
            self.files.borrow().get(p).cloned().ok_or_else(enoent)
        }

        fn write(&self, p: &str, body: &[u8]) -> io::Result<()> {
            if self.fail_write.get() {
                return Err(io::Error::other("disk full"));
            }
            self.files.borrow_mut().insert(p.to_string(), body.to_vec());
            self.times.borrow_mut().insert(p.to_string(), self.tick());
            Ok(())
        }

        fn create_dir_all(&self, _: &str) -> io::Result<()> {
            Ok(())
        }

        fn remove_file(&self, p: &str) -> io::Result<()> {
            if self.fail_rm.get() {
                return Err(io::Error::other("EBUSY"));
            }
            self.files.borrow_mut().remove(p);
            Ok(())
        }

        fn remove_dir_all(&self, p: &str) -> io::Result<()> {
            if self.fail_rm.get() {
                return Err(io::Error::other("EBUSY: resource busy or locked"));
            }
            let prefix = format!("{p}/");
            self.files
                .borrow_mut()
                .retain(|key, _| key != p && !key.starts_with(&prefix));
            Ok(())
        }

        fn touch(&self, p: &str) -> io::Result<()> {
            if self.no_touch.get() {
                return Ok(());
            }
            if self.fail_touch.get() {
                return Err(io::Error::other("read-only volume"));
            }
            self.times.borrow_mut().insert(p.to_string(), self.tick());
            Ok(())
        }
    }

    fn store(fs: &FakeFs) -> AttachmentStore<&FakeFs> {
        AttachmentStore::new("/cache", fs)
    }

    fn small(fs: &FakeFs, budget: u64) -> AttachmentStore<&FakeFs> {
        AttachmentStore::with_budget("/cache", fs, budget)
    }

    fn png() -> WriteOptions<'static> {
        WriteOptions {
            content_type: "image/png",
            ..WriteOptions::default()
        }
    }

    // parseAttachmentPath

    #[test]
    fn names_the_workspace_and_the_attachment() {
        assert_eq!(
            parse_attachment_path(&a()),
            Some(AttachmentId {
                workspace_id: "0iCYS9XKlnN".to_string(),
                task_id: "0iCYTqxKOqv".to_string(),
                attachment_id: "0iCt6L19zvN".to_string(),
            })
        );
    }

    #[test]
    fn is_not_fooled_by_a_path_that_merely_resembles_one() {
        assert_eq!(
            parse_attachment_path("/api/v1/workspaces/ws1/tasks/t1"),
            None
        );
        assert_eq!(
            parse_attachment_path("/api/v1/workspaces/ws1/tasks/t1/attachments"),
            None
        );
        assert_eq!(parse_attachment_path(""), None);
        // Nothing before the first slash of `/workspaces/…` is not a match.
        assert_eq!(
            parse_attachment_path("workspaces/w/tasks/t/attachments/a"),
            None
        );
        assert_eq!(
            parse_attachment_path("/workspaces/w/tasks/t/attachments/a/"),
            None
        );
    }

    #[test]
    fn refuses_an_id_that_must_not_become_a_filename() {
        // Ids are base62, so anything else is refused outright rather than
        // escaped. Every one of the three becomes a path segment, so every one
        // is checked.
        for refused in [
            "/api/v1/workspaces/../../etc/tasks/t1/attachments/a1",
            "/api/v1/workspaces/ws1/tasks/../../x/attachments/a1",
            "/api/v1/workspaces/ws1/tasks/t1/attachments/..",
            "/api/v1/workspaces/ws1/tasks/t1/attachments/a%2E%2E",
            "/api/v1/workspaces/ws-1/tasks/t1/attachments/a1",
        ] {
            assert_eq!(parse_attachment_path(refused), None, "{refused}");
        }
    }

    // read

    #[test]
    fn serves_a_stored_attachment_with_its_content_type() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"PNGBYTES", png());

        let hit = s.read(&a()).unwrap();
        assert_eq!(hit.body, b"PNGBYTES");
        assert_eq!(hit.content_type, "image/png");
    }

    #[test]
    fn mirrors_the_request_path_on_disk() {
        // The path on disk and the path in the request read the same way, and
        // the attachment id already names that exact file for all time.
        let fs = FakeFs::default();
        store(&fs).write(&a(), b"BYTES", png());

        assert_eq!(fs.keys(), [A_FILE, &format!("{A_FILE}.meta")]);
        assert_eq!(
            String::from_utf8(fs.files.borrow()[&format!("{A_FILE}.meta")].clone()).unwrap(),
            r#"{"contentType":"image/png","contentDisposition":""}"#
        );
    }

    #[test]
    fn defaults_the_disposition_when_the_stored_description_predates_it() {
        // A meta file written by an older build carries no disposition at all,
        // which must read back as empty.
        let fs = FakeFs::with(&[
            (A_FILE, "BYTES"),
            (&format!("{A_FILE}.meta"), r#"{"contentType":"image/png"}"#),
        ]);

        assert_eq!(store(&fs).read(&a()).unwrap().content_disposition, "");
    }

    #[test]
    fn carries_a_content_disposition_back_and_defaults_it_when_absent() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(
            &a(),
            b"BYTES",
            WriteOptions {
                content_type: "application/pdf",
                content_disposition: "inline",
                size: None,
            },
        );
        s.write(&b(), b"BYTES", png());

        assert_eq!(s.read(&a()).unwrap().content_disposition, "inline");
        assert_eq!(s.read(&b()).unwrap().content_disposition, "");
    }

    #[test]
    fn misses_for_something_never_stored_or_a_path_it_will_not_touch() {
        let fs = FakeFs::default();
        let s = store(&fs);

        assert_eq!(s.read(&a()), None);
        assert_eq!(s.read("/api/v1/workspaces/ws1/tasks/t1"), None);
    }

    #[test]
    fn misses_when_the_pair_is_half_written_or_unreadable() {
        // Every one of these means the same thing to the caller: forward to the
        // server, which is what it would have done anyway.
        let no_meta = FakeFs::with(&[(A_FILE, "BYTES")]);
        assert_eq!(store(&no_meta).read(&a()), None);

        let meta = format!("{A_FILE}.meta");
        let corrupt = FakeFs::with(&[(A_FILE, "BYTES"), (&meta, "not json")]);
        assert_eq!(store(&corrupt).read(&a()), None);

        let empty = FakeFs::with(&[(A_FILE, "BYTES"), (&meta, "{}")]);
        assert_eq!(store(&empty).read(&a()), None);

        let blank = FakeFs::with(&[(A_FILE, "BYTES"), (&meta, r#"{"contentType":""}"#)]);
        assert_eq!(store(&blank).read(&a()), None);
    }

    #[test]
    fn records_the_hit_so_eviction_knows_what_is_in_use() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"BYTES", png());
        let before = fs.times.borrow()[A_FILE];

        s.read(&a());

        assert!(fs.times.borrow()[A_FILE] > before);
    }

    #[test]
    fn still_serves_when_the_hit_cannot_be_recorded_or_cannot_be_at_all() {
        let failing = FakeFs::default();
        store(&failing).write(&a(), b"BYTES", png());
        failing.fail_touch.set(true);
        assert_eq!(store(&failing).read(&a()).unwrap().body, b"BYTES");

        let none = FakeFs::default();
        store(&none).write(&a(), b"BYTES", png());
        none.no_touch.set(true);
        assert_eq!(store(&none).read(&a()).unwrap().body, b"BYTES");
    }

    // write

    #[test]
    fn refuses_a_file_too_large_to_be_worth_the_space() {
        let fs = FakeFs::default();
        let big = WriteOptions {
            size: Some(3 * 1024 * 1024),
            ..png()
        };

        assert!(!store(&fs).write(&a(), b"BIG", big));
        assert_eq!(store(&fs).read(&a()), None);
    }

    #[test]
    fn keeps_a_response_whose_size_was_not_measured() {
        // Refusing everything unmeasurable would mean caching almost nothing.
        let fs = FakeFs::default();
        assert!(store(&fs).write(&a(), b"BYTES", png()));
    }

    #[test]
    fn refuses_what_it_could_not_serve_back_or_must_not_name_a_file() {
        let fs = FakeFs::default();

        assert!(!store(&fs).write(&a(), b"BYTES", WriteOptions::default()));
        assert!(!store(&fs).write("/api/v1/workspaces/ws-1/tasks/t/attachments/a", b"B", png()));
    }

    #[test]
    fn reports_failure_rather_than_throwing_when_the_disk_refuses() {
        let fs = FakeFs::default();
        fs.fail_write.set(true);

        assert!(!store(&fs).write(&a(), b"BYTES", png()));
    }

    // evict

    #[test]
    fn drops_the_least_recently_used_until_the_budget_fits() {
        let fs = FakeFs::default();
        let s = small(&fs, 20);
        s.write(&a(), b"1234567890", png());
        s.write(&b(), b"1234567890", png());
        s.write(&other_ws(), b"1234567890", png());

        assert_eq!(s.read(&a()), None);
        assert!(s.read(&b()).is_some());
    }

    #[test]
    fn spares_an_entry_that_was_read_recently() {
        let fs = FakeFs::default();
        let s = small(&fs, 20);
        s.write(&a(), b"1234567890", png());
        s.write(&b(), b"1234567890", png());
        s.read(&a());

        s.write(&other_ws(), b"1234567890", png());

        assert!(s.read(&a()).is_some());
        assert_eq!(s.read(&b()), None);
    }

    #[test]
    fn counts_across_workspaces_since_the_budget_is_for_the_device() {
        let fs = FakeFs::default();
        let s = small(&fs, 15);
        s.write(&a(), b"1234567890", png());

        s.write(&other_ws(), b"1234567890", png());

        assert_eq!(s.read(&a()), None);
    }

    // Eviction is housekeeping, and housekeeping that throws takes the write it
    // was tidying up after with it.
    #[test]
    fn survives_a_file_that_will_not_delete() {
        // Stored under a budget that fits, so nothing is swept on the way in
        // and the explicit sweep below has something to do.
        let fs = FakeFs::default();
        small(&fs, 1000).write(&a(), b"1234567890", png());
        small(&fs, 1000).write(&b(), b"1234567890", png());

        fs.fail_rm.set(true);

        assert!(small(&fs, 5).evict() > 0);
    }

    #[test]
    fn has_nothing_to_drop_while_the_budget_fits_or_before_anything_is_stored() {
        let fs = FakeFs::default();
        store(&fs).write(&a(), b"BYTES", png());

        assert_eq!(store(&fs).evict(), 0);
        assert_eq!(store(&FakeFs::default()).evict(), 0);
    }

    #[test]
    fn ignores_names_that_are_not_ours_and_files_that_vanish_mid_scan() {
        // `.DS_Store` and the like are not base62, so they are never treated
        // as a workspace directory.
        let fs = FakeFs::with(&[
            ("/cache/.DS_Store", "x"),
            ("/cache/tmp-scratch/x", "y"),
            ("/cache/0iCYS9XKlnN/0iCYTqxKOqv/not-an-id", "y"),
            ("/cache/0iCYS9XKlnN/not-a-task-id/x", "z"),
        ]);
        store(&fs).write(&a(), b"BYTES", png());
        *fs.fail_stat_suffix.borrow_mut() = Some("0iCt6L19zvN");

        assert_eq!(store(&fs).evict(), 0);
    }

    #[test]
    fn ignores_a_directory_it_cannot_read_at_either_level() {
        let fs = FakeFs::default();
        store(&fs).write(&a(), b"BYTES", png());
        // `/cache/<ws>` has three segments, so this fails the workspace level.
        fs.fail_read_dir_below.set(Some(2));
        assert_eq!(store(&fs).evict(), 0);

        let deeper = FakeFs::default();
        store(&deeper).write(&a(), b"BYTES", png());
        deeper.fail_read_dir_below.set(Some(3));
        assert_eq!(store(&deeper).evict(), 0);
    }

    // forgetTask

    #[test]
    fn removes_one_task_and_leaves_the_rest_of_the_workspace() {
        // A directory removal, because the task is a directory.
        let fs = FakeFs::default();
        let s = store(&fs);
        let other_task = "/api/v1/workspaces/0iCYS9XKlnN/tasks/0iCYVCihMfJ/attachments/0iCt6L19zvR";
        s.write(&a(), b"A", png());
        s.write(&b(), b"B", png());
        s.write(other_task, b"C", png());

        assert!(s.forget_task("0iCYS9XKlnN", "0iCYTqxKOqv"));

        assert_eq!(s.read(&a()), None);
        assert_eq!(s.read(&b()), None);
        assert!(s.read(other_task).is_some());
    }

    #[test]
    fn does_not_reach_into_another_workspace_holding_the_same_task_id() {
        let fs = FakeFs::default();
        let s = store(&fs);
        let same_task_elsewhere =
            "/api/v1/workspaces/0aBcDeFgHiJ/tasks/0iCYTqxKOqv/attachments/0iCt6L19zvS";
        s.write(&a(), b"A", png());
        s.write(same_task_elsewhere, b"B", png());

        s.forget_task("0iCYS9XKlnN", "0iCYTqxKOqv");

        assert!(s.read(same_task_elsewhere).is_some());
    }

    #[test]
    fn refuses_ids_that_must_not_become_a_path() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"A", png());

        assert!(!s.forget_task("../..", "t"));
        assert!(!s.forget_task("0iCYS9XKlnN", "../.."));
        assert!(!s.forget_task("", "0iCYTqxKOqv"));
        assert!(!s.forget_task("0iCYS9XKlnN", ""));
        assert!(s.read(&a()).is_some());
    }

    #[test]
    fn forget_task_reports_failure_rather_than_throwing() {
        let fs = FakeFs::default();
        fs.fail_rm.set(true);

        assert!(!store(&fs).forget_task("0iCYS9XKlnN", "0iCYTqxKOqv"));
    }

    // forgetWorkspace

    #[test]
    fn removes_one_workspace_and_leaves_the_others() {
        // One directory removal, because the workspace is a directory.
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"A", png());
        s.write(&other_ws(), b"B", png());

        assert!(s.forget_workspace("0iCYS9XKlnN"));

        assert_eq!(s.read(&a()), None);
        assert!(s.read(&other_ws()).is_some());
    }

    #[test]
    fn refuses_a_workspace_id_that_must_not_become_a_path() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"A", png());

        assert!(!s.forget_workspace("../.."));
        assert!(!s.forget_workspace(""));
        assert!(s.read(&a()).is_some());
    }

    #[test]
    fn forget_workspace_reports_failure_rather_than_throwing() {
        let fs = FakeFs::default();
        fs.fail_rm.set(true);

        assert!(!store(&fs).forget_workspace("0iCYS9XKlnN"));
    }

    // forgetAll

    #[test]
    fn drops_everything_which_is_what_signing_out_needs() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"A", png());
        s.write(&other_ws(), b"B", png());

        assert!(s.forget_all());
        assert!(fs.keys().is_empty());
    }

    #[test]
    fn forget_all_reports_failure_rather_than_throwing() {
        let fs = FakeFs::default();
        fs.fail_rm.set(true);

        assert!(!store(&fs).forget_all());
    }

    // createAttachmentStore

    #[test]
    fn defaults_to_the_desktop_budget_a_gigabyte() {
        let fs = FakeFs::default();
        let s = store(&fs);
        s.write(&a(), b"BYTES", png());

        assert_eq!(s.evict(), 0);
        assert_eq!(DESKTOP_BUDGET_BYTES, 1024 * 1024 * 1024);
        assert_eq!(budget_for("desktop"), DESKTOP_BUDGET_BYTES);
        assert_eq!(budget_for("web"), WEB_BUDGET_BYTES);
    }

    #[test]
    fn can_be_constructed_with_nothing_and_simply_does_nothing() {
        // An empty directory name and an empty disk: every read is a miss.
        let fs = FakeFs::default();
        assert_eq!(AttachmentStore::new("", &fs).read(&a()), None);
    }

    // What proxying does with a response.

    #[test]
    fn serves_only_a_get_for_an_attachment_from_the_store() {
        assert!(should_serve_from_store("GET", &a()));
        assert!(!should_serve_from_store("POST", &a()));
        assert!(!should_serve_from_store(
            "GET",
            "/api/v1/workspaces/w/tasks/t"
        ));
        assert!(is_attachment_request("/tasks/t/attachments/a"));
        // The JS needs a slash before `tasks`.
        assert!(!is_attachment_request("tasks/t/attachments/a"));
        assert!(!is_attachment_request("/tasks//attachments/a"));
        assert!(!is_attachment_request("/tasks/t/attachments/"));
    }

    #[test]
    fn caches_only_a_successful_response() {
        assert!(should_cache_response(200));
        for status in [201, 204, 206, 304, 404, 500] {
            assert!(!should_cache_response(status), "{status}");
        }
    }

    #[test]
    fn plans_nothing_for_nothing_and_never_drops_the_newest() {
        assert!(eviction_plan(&[], 0).is_empty());
        let entry = |url: &str, size| EvictionEntry {
            url: url.to_string(),
            size,
        };
        // Over budget even alone: the newest still stays.
        assert_eq!(
            eviction_plan(&[entry("old", 10), entry("new", 10)], 5),
            ["old"]
        );
    }

    // The real disk, once, in a directory that is removed afterwards.

    #[test]
    fn writes_a_file_whole_through_a_sibling_it_never_leaves_behind() {
        let dir = std::env::temp_dir().join(format!(
            "agentrq-attachment-store-{}-{}",
            std::process::id(),
            "atomic"
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join("taken")).unwrap();
        let path = |name: &str| dir.join(name).to_str().unwrap().to_string();

        DiskFs.write(&path("a"), b"one").unwrap();
        DiskFs.write(&path("a"), b"two").unwrap();
        assert_eq!(std::fs::read(dir.join("a")).unwrap(), b"two");

        // A directory in the way: the rename fails, and the sibling goes too.
        assert!(DiskFs.write(&path("taken"), b"x").is_err());
        let mut left = DiskFs.read_dir(dir.to_str().unwrap()).unwrap();
        left.sort();
        assert_eq!(left, ["a", "taken"]);
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn round_trips_through_a_real_directory() {
        let dir = std::env::temp_dir().join(format!(
            "agentrq-attachment-store-{}-{}",
            std::process::id(),
            "round-trip"
        ));
        let _ = std::fs::remove_dir_all(&dir);
        let dir_str = dir.to_str().unwrap().to_string();
        let s = AttachmentStore::with_budget(&dir_str, DiskFs, 15);

        // Missing directory: nothing to read, nothing to sweep, nothing to
        // forget, and none of it an error.
        assert_eq!(s.read(&a()), None);
        assert_eq!(s.evict(), 0);
        assert!(s.forget_workspace("0iCYS9XKlnN"));

        assert!(s.write(&a(), b"1234567890", png()));
        let hit = s.read(&a()).unwrap();
        assert_eq!(hit.body, b"1234567890");
        assert_eq!(hit.content_type, "image/png");
        assert!(dir
            .join("0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta")
            .exists());

        // A second entry pushes the first out of a 15-byte budget.
        std::thread::sleep(std::time::Duration::from_millis(20));
        assert!(s.write(&b(), b"1234567890", png()));
        assert_eq!(s.read(&a()), None);
        assert!(s.read(&b()).is_some());
        assert!(!dir
            .join("0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta")
            .exists());

        assert!(s.forget_task("0iCYS9XKlnN", "0iCYTqxKOqv"));
        assert_eq!(s.read(&b()), None);
        assert!(s.write(&other_ws(), b"x", png()));
        assert!(s.forget_all());
        assert!(!dir.exists());
    }
}
