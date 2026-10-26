// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The Node.js the extension host runs on: downloaded once when the person
//! asks, and checked against its pin before the host starts.
//!
//! Outside tests the only constructor is [`Node::new`], which downloads the
//! pinned archive from nodejs.org over https only, redirects included.
//! Neither the address nor the pin can be changed from outside. A check is
//! cached on the binary's size and modified time, so the binary is hashed
//! once per launch, not on every page load.

use std::io;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::{Duration, SystemTime};

use agentrq_desktop_core::ext_node::{
    current_pin, download_url, extract, is_leftover, is_pinned_binary, needs_node, sha256_of,
    Hasher, NodePin, Steps, BINARY_NAME, NODE_VERSION,
};
use futures_util::StreamExt;
use serde_json::{json, Value};
use tokio::io::AsyncWriteExt;
use tokio::sync::oneshot;

/// The longest a connection may take to open, and a download may go
/// without a byte. A slow line still finishes; a stalled one does not hang.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(30);
const IDLE_TIMEOUT: Duration = Duration::from_secs(30);

/// The waits between tries at renaming the binary into place, about 5 s in
/// all: on Windows, antivirus may hold a new executable open for a while.
const RENAME_BACKOFF: [Duration; 7] = [
    Duration::from_millis(50),
    Duration::from_millis(100),
    Duration::from_millis(200),
    Duration::from_millis(400),
    Duration::from_millis(800),
    Duration::from_millis(1600),
    Duration::from_millis(2000),
];

/// Where a download comes from.
struct Source {
    url: String,
    pin: &'static NodePin,
    client: reqwest::Client,
}

/// What the binary on disk is.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Health {
    /// The pinned build, ready to run.
    Ready(PathBuf),
    Missing,
    /// There, but not the pinned build: an older pin, or a damaged file.
    Outdated,
}

/// Why a download installed nothing.
#[derive(Debug, PartialEq, Eq)]
pub enum Failure {
    Cancelled,
    Failed(String),
}

/// The binary's size and modified time when it last checked out.
type Print = (u64, Option<SystemTime>);

pub struct Node {
    bin: PathBuf,
    source: Option<Source>,
    rename_backoff: Vec<Duration>,
    checked: Mutex<Option<Print>>,
    downloading: tokio::sync::Mutex<()>,
    cancel: Mutex<Option<oneshot::Sender<()>>>,
}

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// The client every download uses: https only, redirects included, with a
/// deadline on connecting and on each read.
fn client() -> reqwest::Client {
    reqwest::Client::builder()
        .https_only(true)
        // The pinned size and hash are of the bytes nodejs.org stores.
        .no_gzip()
        .no_brotli()
        .connect_timeout(CONNECT_TIMEOUT)
        .read_timeout(IDLE_TIMEOUT)
        .build()
        .expect("a TLS backend")
}

impl Node {
    /// Node under `data_dir/bin`, from nodejs.org.
    pub fn new(data_dir: &Path) -> Arc<Self> {
        let source = current_pin().map(|pin| Source {
            url: download_url(pin),
            pin,
            client: client(),
        });
        Self::from_source(data_dir, source, RENAME_BACKOFF.to_vec())
    }

    fn from_source(data_dir: &Path, source: Option<Source>, backoff: Vec<Duration>) -> Arc<Self> {
        Arc::new(Self {
            bin: data_dir.join("bin"),
            source,
            rename_backoff: backoff,
            checked: Mutex::default(),
            downloading: tokio::sync::Mutex::const_new(()),
            cancel: Mutex::default(),
        })
    }

    /// Where the binary is, whether or not it is there.
    pub fn binary(&self) -> PathBuf {
        self.bin.join(BINARY_NAME)
    }

    /// What the binary on disk is. Blocks while it hashes, once per launch.
    pub fn check(&self) -> Health {
        let binary = self.binary();
        let (Some(source), Ok(meta)) = (&self.source, std::fs::metadata(&binary)) else {
            return Health::Missing;
        };
        let print = (meta.len(), meta.modified().ok());
        // Held while hashing, so two checks at once hash only once.
        let mut checked = lock(&self.checked);
        if *checked == Some(print) {
            return Health::Ready(binary);
        }
        let pin = source.pin;
        let pinned = meta.len() == pin.binary_size
            && std::fs::File::open(&binary)
                .and_then(|mut file| sha256_of(&mut file))
                .is_ok_and(|(size, sha256)| is_pinned_binary(pin, size, &sha256));
        if !pinned {
            return Health::Outdated;
        }
        *checked = Some(print);
        Health::Ready(binary)
    }

    /// [`Node::check`], off the async runtime.
    pub async fn checked(self: &Arc<Self>) -> Health {
        let this = Arc::clone(self);
        tokio::task::spawn_blocking(move || this.check())
            .await
            .expect("a check does not panic")
    }

    /// The binary, when it is the pinned build.
    pub fn verify(&self) -> Result<PathBuf, String> {
        match self.check() {
            Health::Ready(binary) => Ok(binary),
            _ => Err(needs_node()),
        }
    }

    /// What the Extensions page says about Node.
    pub fn status(&self, health: &Health) -> Value {
        json!({
            "installed": matches!(health, Health::Ready(_)),
            "update": *health == Health::Outdated,
            "version": NODE_VERSION,
            "size": self.source.as_ref().map(|source| source.pin.archive_size),
        })
    }

    /// Download, check and install the pinned build, unless it is already
    /// there. One download at a time: a second caller waits, then finds it
    /// installed. `progress` hears the bytes so far, the total and the
    /// percent, at most once per whole percent.
    pub async fn download(
        self: &Arc<Self>,
        progress: &(dyn Fn(u64, u64, u32) + Send + Sync),
    ) -> Result<PathBuf, Failure> {
        let source = self.source.as_ref().ok_or_else(|| {
            Failure::Failed("There is no Node.js build for this computer.".into())
        })?;
        let _one = self.downloading.lock().await;
        if let Health::Ready(binary) = self.checked().await {
            return Ok(binary);
        }
        let (cancel, cancelled) = oneshot::channel();
        *lock(&self.cancel) = Some(cancel);
        self.sweep();
        let id = unique_temp_id();
        let archive = self.bin.join(format!(".node-download-{id}.tmp"));
        let unpacked = self.bin.join(format!(".node-bin-{id}.tmp"));
        let installed = tokio::select! {
            installed = self.install(source, &archive, &unpacked, progress) => {
                installed.map_err(Failure::Failed)
            }
            _ = cancelled => Err(Failure::Cancelled),
        };
        lock(&self.cancel).take();
        let _ = std::fs::remove_file(&archive);
        let _ = std::fs::remove_file(&unpacked);
        installed
    }

    /// Stop the download under way; false when there is none.
    pub fn cancel(&self) -> bool {
        lock(&self.cancel)
            .take()
            .is_some_and(|cancel| cancel.send(()).is_ok())
    }

    /// Remove what a download that never finished left in `bin`: a quit
    /// mid-download leaves its temporary files behind.
    pub fn sweep(&self) {
        for entry in std::fs::read_dir(&self.bin).into_iter().flatten().flatten() {
            if is_leftover(&entry.file_name().to_string_lossy()) {
                let _ = std::fs::remove_file(entry.path());
            }
        }
    }

    async fn install(
        &self,
        source: &Source,
        archive: &Path,
        unpacked: &Path,
        progress: &(dyn Fn(u64, u64, u32) + Send + Sync),
    ) -> Result<PathBuf, String> {
        tokio::fs::create_dir_all(&self.bin).await.map_err(failed)?;
        fetch(source, archive, progress).await?;
        let pin = source.pin;
        let (archive, unpacked) = (archive.to_path_buf(), unpacked.to_path_buf());
        let (binary, backoff) = (self.binary(), self.rename_backoff.clone());
        let placed = binary.clone();
        let meta = tokio::task::spawn_blocking(move || {
            unpack(pin, &archive, &unpacked)?;
            rename_with_retry(&mut || std::fs::rename(&unpacked, &placed), &backoff)
                .map_err(|error| format!("Could not put Node.js in place: {error}"))?;
            std::fs::metadata(&placed).map_err(failed)
        })
        .await
        .expect("unpacking does not panic")?;
        // Checked as it was unpacked: there is no need to hash it again.
        *lock(&self.checked) = Some((meta.len(), meta.modified().ok()));
        Ok(binary)
    }
}

/// Fetch the pinned archive into `archive`, refusing anything that is not
/// its size, and check its SHA-256.
async fn fetch(
    source: &Source,
    archive: &Path,
    progress: &(dyn Fn(u64, u64, u32) + Send + Sync),
) -> Result<(), String> {
    let pin = source.pin;
    let response = source
        .client
        .get(&source.url)
        .send()
        .await
        .map_err(stalled_or_failed)?;
    if !response.status().is_success() {
        return Err(format!("nodejs.org answered {}.", response.status()));
    }
    if response
        .content_length()
        .is_some_and(|size| size != pin.archive_size)
    {
        return Err(not_pinned());
    }
    let mut file = tokio::fs::File::create(archive).await.map_err(failed)?;
    let mut hasher = Hasher::default();
    let mut steps = Steps::default();
    let mut done = 0u64;
    let mut body = response.bytes_stream();
    while let Some(chunk) = body.next().await {
        let chunk = chunk.map_err(stalled_or_failed)?;
        done += chunk.len() as u64;
        if done > pin.archive_size {
            return Err(not_pinned());
        }
        hasher.update(&chunk);
        file.write_all(&chunk).await.map_err(failed)?;
        if let Some(percent) = steps.advance(done, pin.archive_size) {
            progress(done, pin.archive_size, percent);
        }
    }
    file.flush().await.map_err(failed)?;
    if hasher.finish() != pin.archive_sha256 {
        return Err(not_pinned());
    }
    Ok(())
}

/// Take the binary out of `archive` into `unpacked`, read-only.
fn unpack(pin: &NodePin, archive: &Path, unpacked: &Path) -> Result<(), String> {
    let mut input = io::BufReader::new(std::fs::File::open(archive).map_err(failed)?);
    let mut output = std::fs::File::create(unpacked).map_err(failed)?;
    extract(pin, &mut input, &mut output)?;
    drop(output);
    read_only(unpacked)
}

#[cfg(unix)]
fn read_only(binary: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(binary, std::fs::Permissions::from_mode(0o555)).map_err(failed)
}

/// Windows keeps it writable: a read-only file cannot be renamed over when
/// the pin moves.
#[cfg(not(unix))]
fn read_only(_: &Path) -> Result<(), String> {
    Ok(())
}

/// Try `rename`, then again after each wait in `backoff` until it works.
fn rename_with_retry(
    rename: &mut dyn FnMut() -> io::Result<()>,
    backoff: &[Duration],
) -> io::Result<()> {
    let mut renamed = rename();
    for wait in backoff {
        if renamed.is_ok() {
            break;
        }
        std::thread::sleep(*wait);
        renamed = rename();
    }
    renamed
}

fn stalled_or_failed(error: reqwest::Error) -> String {
    if error.is_timeout() {
        return "The Node.js download stalled. Try again.".into();
    }
    failed(error)
}

fn failed(error: impl std::fmt::Display) -> String {
    format!("The Node.js download failed: {error}")
}

fn not_pinned() -> String {
    "The download is not the pinned Node.js build.".into()
}

static TEMP_COUNTER: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);

fn unique_temp_id() -> String {
    let nanos = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let count = TEMP_COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    format!("{}-{}-{}", std::process::id(), nanos, count)
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::test_server::{closed_port, reply, send, Step, TestServer};
    use agentrq_desktop_core::ext_node::Packing;
    use std::io::Write;
    use std::sync::atomic::{AtomicUsize, Ordering};

    /// What a test binary holds.
    pub const BINARY: &[u8] = b"#!/bin/sh\nexit 0\n";

    impl Failure {
        /// Why, in words.
        pub fn reason(self) -> String {
            match self {
                Failure::Cancelled => "cancelled".into(),
                Failure::Failed(reason) => reason,
            }
        }
    }

    /// A tar.xz with `binary` at `node-test/bin/node`, packed as nodejs.org's are.
    pub fn archive_of(binary: &[u8]) -> Vec<u8> {
        let mut tar = tar::Builder::new(Vec::new());
        let mut header = tar::Header::new_gnu();
        header.set_size(binary.len() as u64);
        header.set_mode(0o755);
        tar.append_data(&mut header, "node-test/bin/node", binary)
            .unwrap();
        let mut xz =
            lzma_rust2::XzWriter::new(Vec::new(), lzma_rust2::XzOptions::default()).unwrap();
        xz.write_all(&tar.into_inner().unwrap()).unwrap();
        xz.finish().unwrap()
    }

    fn sha256(bytes: &[u8]) -> &'static str {
        let mut hasher = Hasher::default();
        hasher.update(bytes);
        Box::leak(hasher.finish().into_boxed_str())
    }

    /// The pin of `archive_of(binary)`.
    pub fn pin_of(binary: &[u8]) -> &'static NodePin {
        let archive = archive_of(binary);
        Box::leak(Box::new(NodePin {
            archive: "node-test.tar.xz",
            archive_sha256: sha256(&archive),
            archive_size: archive.len() as u64,
            packing: Packing::TarXz,
            member: "node-test/bin/node",
            binary_sha256: sha256(binary),
            binary_size: binary.len() as u64,
        }))
    }

    /// Node in `dir`, downloading `pin` from `url`.
    pub fn node_from(dir: &Path, url: &str, pin: &'static NodePin) -> Arc<Node> {
        node_with(dir, url, pin, Duration::from_secs(10))
    }

    /// The same, giving up on a download that goes `idle` without a byte.
    fn node_with(dir: &Path, url: &str, pin: &'static NodePin, idle: Duration) -> Arc<Node> {
        let client = reqwest::Client::builder()
            .read_timeout(idle)
            .build()
            .unwrap();
        let source = Source {
            url: url.into(),
            pin,
            client,
        };
        Node::from_source(dir, Some(source), vec![Duration::from_millis(1); 3])
    }

    /// A server answering every request with `body`.
    pub async fn serving(body: Vec<u8>) -> TestServer {
        TestServer::start(move |_| {
            let mut answer = format!(
                "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            )
            .into_bytes();
            answer.extend_from_slice(&body);
            vec![Step::Send(answer)]
        })
        .await
    }

    pub fn quiet(_: u64, _: u64, _: u32) {}

    /// What is in `bin`, by name.
    fn listing(node: &Node) -> Vec<String> {
        let mut names: Vec<String> = std::fs::read_dir(&node.bin)
            .unwrap()
            .map(|entry| entry.unwrap().file_name().to_string_lossy().into_owned())
            .collect();
        names.sort();
        names
    }

    #[tokio::test]
    async fn downloads_checks_and_installs_the_pinned_build() {
        let dir = tempfile::tempdir().unwrap();
        let pin = pin_of(BINARY);
        let server = serving(archive_of(BINARY)).await;
        let node = node_from(dir.path(), &server.url, pin);
        assert_eq!(node.check(), Health::Missing);
        let heard = Mutex::new(Vec::new());
        let progress = |done, total, percent| lock(&heard).push((done, total, percent));
        let binary = node.download(&progress).await.unwrap();
        assert_eq!(binary, dir.path().join("bin").join(BINARY_NAME));
        assert_eq!(std::fs::read(&binary).unwrap(), BINARY);
        assert_eq!(listing(&node), vec![BINARY_NAME.to_string()]);
        let heard = lock(&heard).clone();
        assert_eq!(
            heard.last(),
            Some(&(pin.archive_size, pin.archive_size, 100))
        );
        // Once per whole percent, and only forwards.
        let percents: Vec<u32> = heard.iter().map(|step| step.2).collect();
        let mut rising = percents.clone();
        rising.sort_unstable();
        rising.dedup();
        assert_eq!(percents, rising);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = std::fs::metadata(&binary).unwrap().permissions().mode();
            assert_eq!(mode & 0o777, 0o555);
        }
        assert_eq!(node.check(), Health::Ready(binary.clone()));
        assert_eq!(node.verify(), Ok(binary.clone()));

        // Already there: nothing is fetched again.
        assert_eq!(node.download(&quiet).await, Ok(binary));
        assert_eq!(server.requests().len(), 1);
    }

    #[tokio::test]
    async fn two_installs_at_once_download_once() {
        let dir = tempfile::tempdir().unwrap();
        let server = serving(archive_of(BINARY)).await;
        let node = node_from(dir.path(), &server.url, pin_of(BINARY));
        let (first, second) = tokio::join!(node.download(&quiet), node.download(&quiet));
        assert_eq!(first, second);
        assert_eq!(first, Ok(node.binary()));
        assert_eq!(server.requests().len(), 1);
    }

    #[test]
    fn checks_the_binary_against_its_pin_and_hashes_it_once() {
        let dir = tempfile::tempdir().unwrap();
        let pin = pin_of(BINARY);
        let node = node_from(dir.path(), "", pin);
        std::fs::create_dir_all(&node.bin).unwrap();
        let binary = node.binary();

        // The pinned build, put there by hand.
        std::fs::write(&binary, BINARY).unwrap();
        assert_eq!(node.check(), Health::Ready(binary.clone()));
        assert!(lock(&node.checked).is_some(), "cached");

        // Tampered, at the same size: a new modified time misses the cache.
        let tampered: Vec<u8> = BINARY.iter().rev().copied().collect();
        std::fs::write(&binary, &tampered).unwrap();
        let file = std::fs::File::options().write(true).open(&binary).unwrap();
        file.set_modified(SystemTime::UNIX_EPOCH).unwrap();
        drop(file);
        assert_eq!(node.check(), Health::Outdated);
        assert_eq!(node.verify(), Err(needs_node()));

        // Another size, which is not even hashed.
        std::fs::write(&binary, b"node").unwrap();
        assert_eq!(node.check(), Health::Outdated);

        // What an older pin installed: the pin has moved on.
        let older = node_from(dir.path(), "", pin_of(b"#!/bin/sh\nexit 1\n"));
        std::fs::write(&binary, BINARY).unwrap();
        assert_eq!(older.check(), Health::Outdated);
        assert_eq!(node.check(), Health::Ready(binary.clone()));

        // Something that cannot be read as a file.
        std::fs::remove_file(&binary).unwrap();
        std::fs::create_dir_all(binary.join("x")).unwrap();
        let size = std::fs::metadata(&binary).unwrap().len();
        let folder = node_from(
            dir.path(),
            "",
            Box::leak(Box::new(NodePin {
                binary_size: size,
                ..*pin
            })),
        );
        assert_eq!(folder.check(), Health::Outdated);
    }

    #[tokio::test]
    async fn an_update_replaces_the_binary_of_an_older_pin() {
        let dir = tempfile::tempdir().unwrap();
        let old = b"#!/bin/sh\nexit 1\n";
        let server = serving(archive_of(old)).await;
        let before = node_from(dir.path(), &server.url, pin_of(old));
        before.download(&quiet).await.unwrap();

        // The app updates, and its pin with it.
        let server = serving(archive_of(BINARY)).await;
        let pin = pin_of(BINARY);
        let after = node_from(dir.path(), &server.url, pin);
        let health = after.checked().await;
        assert_eq!(health, Health::Outdated);
        assert_eq!(
            after.status(&health),
            json!({"installed": false, "update": true, "version": NODE_VERSION,
                   "size": pin.archive_size})
        );
        let binary = after.download(&quiet).await.unwrap();
        assert_eq!(std::fs::read(&binary).unwrap(), BINARY);
        assert_eq!(after.check(), Health::Ready(binary));
        assert_eq!(listing(&after), vec![BINARY_NAME.to_string()]);
    }

    #[test]
    fn downloads_from_nodejs_org_and_says_what_the_page_shows() {
        let dir = tempfile::tempdir().unwrap();
        let node = Node::new(dir.path());
        let pin = current_pin().unwrap();
        assert_eq!(
            node.status(&Health::Missing),
            json!({"installed": false, "update": false, "version": NODE_VERSION,
                   "size": pin.archive_size})
        );
        assert_eq!(
            node.status(&Health::Ready(node.binary()))["installed"],
            true
        );
        assert_eq!(node.source.as_ref().unwrap().url, download_url(pin));
        assert_eq!(node.rename_backoff, RENAME_BACKOFF);
        let unsupported = Node::from_source(dir.path(), None, vec![]);
        assert_eq!(unsupported.status(&Health::Missing)["size"], Value::Null);
    }

    #[tokio::test]
    async fn refuses_anything_but_https() {
        let gone = closed_port().await;
        let error = client().get(&gone).send().await.unwrap_err();
        let why = std::error::Error::source(&error).map(ToString::to_string);
        assert_eq!(why.as_deref(), Some("URL scheme is not allowed"), "{error}");
    }

    #[tokio::test]
    async fn has_nothing_to_download_where_nothing_is_pinned() {
        let dir = tempfile::tempdir().unwrap();
        let node = Node::from_source(dir.path(), None, vec![]);
        std::fs::create_dir_all(&node.bin).unwrap();
        std::fs::write(node.binary(), BINARY).unwrap();
        assert_eq!(node.check(), Health::Missing);
        assert_eq!(
            node.download(&quiet).await.unwrap_err().reason(),
            "There is no Node.js build for this computer."
        );
    }

    /// Download from a server that answers with `steps`: why it failed, and
    /// what it left in `bin`.
    async fn failure(steps: Vec<Step>, pin: &'static NodePin) -> (String, Vec<String>) {
        let server = TestServer::start(move |_| steps.clone()).await;
        let dir = tempfile::tempdir().unwrap();
        let node = node_with(dir.path(), &server.url, pin, Duration::from_millis(300));
        let reason = node.download(&quiet).await.unwrap_err().reason();
        (reason, listing(&node))
    }

    #[tokio::test]
    async fn says_why_a_download_failed_and_leaves_nothing_behind() {
        let pin = pin_of(BINARY);
        let archive = archive_of(BINARY);
        let head = |length: usize| {
            format!("HTTP/1.1 200 OK\r\nContent-Length: {length}\r\nConnection: close\r\n\r\n")
        };
        let other_binary = Box::leak(Box::new(NodePin {
            binary_sha256: sha256(b"another"),
            ..*pin
        }));
        let cases: Vec<(Vec<Step>, &NodePin, &str)> = vec![
            (
                reply("500 Internal Server Error", &[], ""),
                pin,
                "nodejs.org answered 500 Internal Server Error.",
            ),
            // A Content-Length that is not the pin's.
            (
                vec![send(&head(archive.len() + 1))],
                pin,
                "The download is not the pinned Node.js build.",
            ),
            // No Content-Length, and more bytes than the pin's.
            (
                vec![
                    send("HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n"),
                    Step::Send([archive.clone(), b"!".to_vec()].concat()),
                ],
                pin,
                "The download is not the pinned Node.js build.",
            ),
            // The right size, the wrong bytes.
            (
                vec![
                    send(&head(archive.len())),
                    Step::Send(vec![b'x'; archive.len()]),
                ],
                pin,
                "The download is not the pinned Node.js build.",
            ),
            // The pinned archive, with another binary pinned.
            (
                vec![send(&head(archive.len())), Step::Send(archive.clone())],
                other_binary,
                "The Node.js binary in the download is not the pinned build.",
            ),
            // Cut off part way.
            (
                vec![
                    send(&head(archive.len())),
                    Step::Send(archive[..10].to_vec()),
                ],
                pin,
                "The Node.js download failed: ",
            ),
            // Stalled part way: the idle limit ends it.
            (
                vec![
                    send(&head(archive.len())),
                    Step::Send(archive[..10].to_vec()),
                    Step::Wait(600_000),
                ],
                pin,
                "The Node.js download stalled. Try again.",
            ),
        ];
        for (index, (steps, pin, reason)) in cases.into_iter().enumerate() {
            let (said, left) = failure(steps, pin).await;
            assert!(said.starts_with(reason), "{index}: {said}");
            assert!(left.is_empty(), "{index}: left {left:?}");
        }
        let gone = closed_port().await;
        let dir = tempfile::tempdir().unwrap();
        let reason = node_from(dir.path(), &gone, pin)
            .download(&quiet)
            .await
            .unwrap_err()
            .reason();
        assert!(
            reason.starts_with("The Node.js download failed: "),
            "{reason}"
        );
    }

    #[tokio::test]
    async fn a_cancel_stops_the_download_and_leaves_nothing_behind() {
        let dir = tempfile::tempdir().unwrap();
        let archive = archive_of(BINARY);
        let head = format!(
            "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
            archive.len()
        );
        let first = archive[..10].to_vec();
        let server = TestServer::start(move |_| {
            vec![send(&head), Step::Send(first.clone()), Step::Wait(600_000)]
        })
        .await;
        let node = node_from(dir.path(), &server.url, pin_of(BINARY));
        assert!(!node.cancel(), "nothing to cancel yet");
        let started = tokio::sync::Notify::new();
        let progress = |_, _, _| started.notify_one();
        let cancel = async {
            tokio::time::timeout(Duration::from_secs(10), started.notified())
                .await
                .expect("the first bytes arrive");
            assert!(node.cancel());
        };
        let (cancelled, ()) = tokio::join!(node.download(&progress), cancel);
        assert_eq!(cancelled.unwrap_err().reason(), "cancelled");
        assert!(listing(&node).is_empty(), "{:?}", listing(&node));
        assert!(!node.cancel(), "nothing left to cancel");
    }

    #[tokio::test]
    async fn sweeps_what_a_quit_mid_download_left() {
        let dir = tempfile::tempdir().unwrap();
        let server = serving(archive_of(BINARY)).await;
        let node = node_from(dir.path(), &server.url, pin_of(BINARY));
        node.sweep();
        std::fs::create_dir_all(&node.bin).unwrap();
        for name in [".node-download-1-2-3.tmp", ".node-bin-1-2-3.tmp", "keep"] {
            std::fs::write(node.bin.join(name), b"x").unwrap();
        }
        node.download(&quiet).await.unwrap();
        assert_eq!(listing(&node), vec!["keep".to_string(), BINARY_NAME.into()]);
    }

    #[tokio::test]
    async fn says_when_it_cannot_put_the_binary_in_place() {
        let dir = tempfile::tempdir().unwrap();
        let server = serving(archive_of(BINARY)).await;
        let node = node_from(dir.path(), &server.url, pin_of(BINARY));
        std::fs::create_dir_all(node.binary().join("in the way")).unwrap();
        let reason = node.download(&quiet).await.unwrap_err().reason();
        assert!(
            reason.starts_with("Could not put Node.js in place: "),
            "{reason}"
        );
        assert_eq!(listing(&node), vec![BINARY_NAME.to_string()]);

        // Nowhere to write the download.
        let blocked = tempfile::tempdir().unwrap();
        std::fs::write(blocked.path().join("bin"), b"a file").unwrap();
        let node = node_from(blocked.path(), &server.url, pin_of(BINARY));
        let reason = node.download(&quiet).await.unwrap_err().reason();
        assert!(
            reason.starts_with("The Node.js download failed: "),
            "{reason}"
        );
    }

    #[test]
    fn unpacking_says_why_it_failed() {
        let dir = tempfile::tempdir().unwrap();
        let archive = dir.path().join("a.tar.xz");
        std::fs::write(&archive, archive_of(BINARY)).unwrap();
        let pin = pin_of(BINARY);
        let gone = dir.path().join("gone");
        assert!(unpack(pin, &gone, &dir.path().join("out")).is_err());
        assert!(unpack(pin, &archive, &gone.join("out")).is_err());
        assert_eq!(unpack(pin, &archive, &dir.path().join("out")), Ok(()));
        #[cfg(unix)]
        assert!(read_only(&gone).is_err());
    }

    #[test]
    fn retries_a_rename_until_it_works_or_the_waits_run_out() {
        let tries = AtomicUsize::new(0);
        let mut busy_twice = || match tries.fetch_add(1, Ordering::SeqCst) {
            0 | 1 => Err(io::Error::from(io::ErrorKind::PermissionDenied)),
            _ => Ok(()),
        };
        let waits = [Duration::from_millis(1); 5];
        assert!(rename_with_retry(&mut busy_twice, &waits).is_ok());
        assert_eq!(tries.load(Ordering::SeqCst), 3);

        let tries = AtomicUsize::new(0);
        let mut always_busy = || {
            tries.fetch_add(1, Ordering::SeqCst);
            Err(io::Error::from(io::ErrorKind::PermissionDenied))
        };
        assert!(rename_with_retry(&mut always_busy, &waits).is_err());
        assert_eq!(tries.load(Ordering::SeqCst), 6);
        assert_eq!(
            RENAME_BACKOFF.iter().sum::<Duration>(),
            Duration::from_millis(5150)
        );
    }
}
