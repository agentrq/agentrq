// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Auto-update.
//!
//! The browser build gets new code by reloading; the desktop app has to fetch
//! and install it, which is the one capability a packaged app genuinely cannot
//! do without. Updates come from this repository's GitHub Releases.
//!
//! The design rule the prompt has to respect: no native modals. The web app
//! already has exactly the right piece of UI for this — the banner App.vue shows
//! when a new service worker is waiting — so the desktop build drives that same
//! banner rather than inventing a second one. Transient states (checking, up to
//! date, failed) go through the toast system.
//!
//! The platform updater, the timers, the installer process and its log are all
//! traits, so the whole state machine is testable with no network and no OS.
//!
//! Rust notes: events from the platform updater arrive through
//! [`Updater::handle_event`] rather than listeners. `check_for_updates` should
//! return once the check has *started*; the outcome comes back as events.
//! `on_status` is never called with an internal lock held, so it may call back
//! into the updater. Statuses are delivered in the order they were decided: a
//! call made from inside `on_status` is queued and delivered once the callback
//! returns.

use crate::install_log::InstallLog;
use serde::Serialize;
use std::collections::{HashSet, VecDeque};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, Weak};
use std::time::Duration;

/// Fifteen minutes.
///
/// Short enough that a release reaches a running app the same session it ships,
/// which is the point: an app left open for days used to sit six hours behind a
/// fix. The check costs one request against GitHub's release metadata and, when
/// nothing has changed, says nothing at all — see [`should_announce`].
pub const UPDATE_CHECK_INTERVAL_MS: u64 = 15 * 60 * 1000;

/// How often a running installer's log is read for progress.
pub const INSTALL_POLL_INTERVAL_MS: u64 = 250;

/// The one-command installer, which replaces the bundle wholesale instead of
/// asking Squirrel.Mac to patch it — so the signature check never applies.
/// Published from the agentrq-static repository (`src/install.sh`).
pub const INSTALL_COMMAND: &str = "curl -fsSL https://agentrq.com/install.sh | sh";

/// The same installer, run for the user instead of shown to them, with the two
/// arguments a running app needs.
///
/// `--quit` because the installer refuses to replace a bundle that is currently
/// running: it closes the app itself and waits for the process to go. And the
/// relaunch is appended here because the installer deliberately does not do it
/// — it prints "Launch it with: open -a AgentRQ" and stops, which is right for
/// someone at a terminal and wrong for someone who just pressed a button.
///
/// Chained with `&&`, so a failed install leaves the old app in place rather
/// than reopening whatever is left of it.
pub const INSTALL_SCRIPT_COMMAND: &str =
    "curl -fsSL https://agentrq.com/install.sh | sh -s -- --quit && open -a AgentRQ";

const DISABLED_IN_DEVELOPMENT: &str = "Updates are disabled in a development build";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum UpdateStatus {
    Idle,
    Checking,
    Available,
    Downloading,
    Ready,
    /// The one-command installer is running; `progress` says how far it is.
    Installing,
    /// The installer finished but this app is still running the old version.
    Installed,
    UpToDate,
    Error,
    Disabled,
}

/// Where the installer has got to.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum InstallPhase {
    Preparing,
    Downloading,
    Installing,
    Installed,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
pub struct Progress {
    pub phase: InstallPhase,
    pub percent: Option<f64>,
}

/// What `on_status` is told, on every change.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PublishedStatus {
    pub status: UpdateStatus,
    pub detail: String,
    pub remedy: String,
    pub version: String,
    pub progress: Option<Progress>,
    pub manual: bool,
    pub announce: bool,
    /// Carried on every status, not only read from the state: this is what the
    /// banner uses to decide whether an 'available' update is worth offering,
    /// and it only ever sees what is published here.
    pub can_install_via_script: bool,
}

/// What the updater is doing now.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateState {
    pub status: UpdateStatus,
    pub detail: String,
    pub remedy: String,
    pub version: String,
    pub progress: Option<Progress>,
    pub enabled: bool,
    /// What the banner needs to choose a button: whether the app can replace
    /// itself, or has to shell out to the installer to do it.
    pub can_install_via_script: bool,
}

/// The answer to "check now" and "install via script".
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ActionResult {
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

impl ActionResult {
    fn ok() -> Self {
        Self {
            ok: true,
            reason: None,
        }
    }

    fn refused(reason: impl Into<String>) -> Self {
        Self {
            ok: false,
            reason: Some(reason.into()),
        }
    }
}

/// What the platform's updater reports.
#[derive(Debug, Clone, PartialEq)]
pub enum UpdaterEvent {
    Checking,
    Available { version: Option<String> },
    DownloadProgress { percent: Option<f64> },
    Downloaded { version: Option<String> },
    NotAvailable,
    Error(String),
}

/// The platform's updater (`electron-updater`'s `autoUpdater`).
pub trait AutoUpdater: Send + Sync {
    fn set_auto_download(&self, enabled: bool);
    fn set_auto_install_on_app_quit(&self, enabled: bool);
    /// Start a check; the outcome arrives as [`UpdaterEvent`]s. `Err` when it
    /// could not even start.
    fn check_for_updates(&self) -> Result<(), String>;
    fn quit_and_install(&self);
}

/// Repeating timers, injected so the schedule can be tested without waiting.
pub trait Scheduler: Send + Sync {
    /// Run `tick` every `ms` until `clear_interval` is called with the handle.
    fn set_interval(&self, ms: u64, tick: Arc<dyn Fn() + Send + Sync>) -> u64;
    fn clear_interval(&self, handle: u64);
}

/// Real intervals: one sleeping thread per interval.
#[derive(Default)]
pub struct StdScheduler {
    next: AtomicU64,
    active: Arc<Mutex<HashSet<u64>>>,
}

impl Scheduler for StdScheduler {
    fn set_interval(&self, ms: u64, tick: Arc<dyn Fn() + Send + Sync>) -> u64 {
        let handle = self.next.fetch_add(1, Ordering::Relaxed);
        let active = Arc::clone(&self.active);
        active.lock().unwrap().insert(handle);
        std::thread::spawn(move || loop {
            std::thread::sleep(Duration::from_millis(ms));
            if !active.lock().unwrap().contains(&handle) {
                break;
            }
            tick();
        });
        handle
    }

    fn clear_interval(&self, handle: u64) {
        self.active.lock().unwrap().remove(&handle);
    }
}

/// How an installer process ended.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InstallerExit {
    /// It ran and stopped; `None` when a signal ended it.
    Exited(Option<i32>),
    /// It never ran, or could not be waited for.
    Failed(String),
}

/// Told how an installer ended.
pub type ExitHandler = Arc<dyn Fn(InstallerExit) + Send + Sync>;

/// A running installer.
pub trait InstallerChild: Send {
    /// Let this app exit without it.
    fn unref(&mut self);
    /// Call `handler` when the installer ends. It may be called more than once;
    /// only the first call counts.
    fn on_exit(self: Box<Self>, handler: ExitHandler);
}

/// Starts the installer.
///
/// **Detached, deliberately.** The installer's first act is to quit this app and
/// wait for the process to disappear — so a child sharing our lifetime would be
/// killed by the very thing it just did, halfway through replacing the
/// application. An implementation puts it in its own process group and lets
/// this process exit without it. `stdio` is the installer's stdout and stderr,
/// or `None` to discard them.
pub trait InstallerSpawner: Send + Sync {
    fn spawn(
        &self,
        command: &str,
        args: &[String],
        stdio: Option<(Stdio, Stdio)>,
    ) -> Result<Box<dyn InstallerChild>, String>;
}

/// Spawns a real process in its own process group.
#[derive(Debug, Default, Clone, Copy)]
pub struct StdInstallerSpawner;

/// How a waited-for process ended; `None` code when a signal ended it.
fn exit_of(waited: std::io::Result<std::process::ExitStatus>) -> InstallerExit {
    match waited {
        Ok(status) => InstallerExit::Exited(status.code()),
        Err(error) => InstallerExit::Failed(error.to_string()),
    }
}

struct StdInstallerChild(std::process::Child);

impl InstallerChild for StdInstallerChild {
    // Dropping a `Child` neither kills nor waits for it.
    fn unref(&mut self) {}

    fn on_exit(self: Box<Self>, handler: ExitHandler) {
        let mut child = self.0;
        std::thread::spawn(move || handler(exit_of(child.wait())));
    }
}

impl InstallerSpawner for StdInstallerSpawner {
    fn spawn(
        &self,
        command: &str,
        args: &[String],
        stdio: Option<(Stdio, Stdio)>,
    ) -> Result<Box<dyn InstallerChild>, String> {
        let (stdout, stderr) = stdio.unwrap_or_else(|| (Stdio::null(), Stdio::null()));
        let mut process = Command::new(command);
        process
            .args(args)
            .stdin(Stdio::null())
            .stdout(stdout)
            .stderr(stderr);
        #[cfg(unix)]
        std::os::unix::process::CommandExt::process_group(&mut process, 0);
        let child = process.spawn().map_err(|e| e.to_string())?;
        Ok(Box::new(StdInstallerChild(child)))
    }
}

pub trait UpdateLogger: Send + Sync {
    fn warn(&self, message: &str);
}

#[derive(Debug, Default, Clone, Copy)]
pub struct ConsoleLogger;

impl UpdateLogger for ConsoleLogger {
    fn warn(&self, message: &str) {
        eprintln!("{message}");
    }
}

/// Why updating is unavailable, or `None` when it is available.
///
/// Running unpackaged is the important one: `make dev` must never reach the
/// update path, where the updater would try to replace a development checkout
/// with a release build.
pub fn updater_disabled_reason(is_packaged: bool) -> Option<&'static str> {
    if is_packaged {
        None
    } else {
        Some(DISABLED_IN_DEVELOPMENT)
    }
}

/// The failure an unsigned macOS build hits every single time: Squirrel.Mac
/// validates the signature before replacing the app, and refuses.
fn is_unsigned_failure(lowercase: &str) -> bool {
    ["code signature", "not signed", "sqrlupdater", "codesign"]
        .iter()
        .any(|needle| lowercase.contains(needle))
}

/// `/latest.*\.yml/`: `.` does not cross a line terminator.
fn names_a_latest_yml(lowercase: &str) -> bool {
    lowercase.match_indices("latest").any(|(at, _)| {
        let rest = &lowercase[at + "latest".len()..];
        let line = rest
            .split(['\n', '\r', '\u{2028}', '\u{2029}'])
            .next()
            .unwrap_or("");
        line.contains(".yml")
    })
}

/// Turn an updater failure into something worth showing a person. `None` is a
/// failure that carried nothing at all.
///
/// The signature case is called out by name because it is the predictable one:
/// macOS refuses to install an update onto an unsigned app, and the raw error
/// says nothing a user could act on.
pub fn describe_update_error(error: Option<&str>) -> String {
    let message = error.unwrap_or("Unknown error");
    let lowercase = message.to_lowercase();

    if is_unsigned_failure(&lowercase) {
        return "This build is not signed, so it cannot update itself".to_string();
    }
    if [
        "net::",
        "enotfound",
        "etimedout",
        "econnrefused",
        "eai_again",
    ]
    .iter()
    .any(|needle| lowercase.contains(needle))
    {
        return "Could not reach the update server".to_string();
    }
    if lowercase.contains("404")
        || lowercase.contains("no published versions")
        || names_a_latest_yml(&lowercase)
    {
        return "No published release to update to".to_string();
    }
    // Only a build packaged with a publish configuration carries this file, so
    // its absence means the build was never set up to update itself.
    if lowercase.contains("app-update.yml") {
        return "This build has no update configuration".to_string();
    }
    message.to_string()
}

/// A command that gets the user past this failure, or `""` when there is none.
///
/// Only the signature case has one. Every other failure here is a network or
/// packaging problem that reinstalling would not touch, and offering a command
/// that cannot help is worse than offering nothing.
pub fn remedy_for_update_error(error: Option<&str>) -> &'static str {
    if is_unsigned_failure(&error.unwrap_or("").to_lowercase()) {
        INSTALL_COMMAND
    } else {
        ""
    }
}

/// Whether the one-command installer can run here at all.
///
/// macOS and Linux only: install.sh refuses Windows by design, where the NSIS
/// installer and the platform updater already handle this properly. Offering a
/// button that the script would refuse is worse than not offering one.
pub fn can_install_via_script(platform: &str) -> bool {
    platform == "darwin" || platform == "linux"
}

/// The platform name as Node spells it, which is what the rules above speak.
fn platform_name(os: &str) -> &str {
    match os {
        "macos" => "darwin",
        "windows" => "win32",
        other => other,
    }
}

pub fn current_platform() -> String {
    platform_name(std::env::consts::OS).to_string()
}

/// What install.sh has written so far, read as a phase and how far it is.
#[derive(Debug, Clone, PartialEq)]
pub struct InstallerLogState {
    pub phase: InstallPhase,
    pub percent: Option<f64>,
    pub error: String,
}

/// The last `\d+(?:\.\d+)?%` in `text`, as a number.
fn last_percent(text: &str) -> Option<f64> {
    let bytes = text.as_bytes();
    let digits = |from: usize| {
        bytes[from..]
            .iter()
            .take_while(|b| b.is_ascii_digit())
            .count()
    };
    let mut last = None;
    let mut i = 0;
    while i < bytes.len() {
        if !bytes[i].is_ascii_digit() {
            i += 1;
            continue;
        }
        let mut end = i + digits(i);
        if bytes.get(end) == Some(&b'.') && digits(end + 1) > 0 {
            end += 1 + digits(end + 1);
        }
        if bytes.get(end) == Some(&b'%') {
            last = text[i..end].parse::<f64>().ok();
            i = end + 1;
        } else {
            i += digits(i);
        }
    }
    last
}

/// Read how far install.sh has got from what it has written so far.
///
/// The percentages are curl's `--progress-bar`, which it writes even when stderr
/// is a file. Only those after "Downloading" count: the lookup and checksum
/// requests are `-s` and never print one, but that is install.sh's choice, not
/// ours to rely on.
pub fn parse_installer_log(text: &str) -> InstallerLogState {
    // `^error: (.*)$` per line, where curl's `\r` redraws also end a line.
    let error = text
        .split(['\n', '\r', '\u{2028}', '\u{2029}'])
        .find_map(|line| line.strip_prefix("error: "))
        // JS `trim()` also strips a byte-order mark, which Rust's does not.
        .map(|rest| {
            rest.trim_matches(|c: char| c.is_whitespace() || c == '\u{feff}')
                .to_string()
        })
        .unwrap_or_default();

    if text.contains("installed to ") || text.contains("already installed") {
        return InstallerLogState {
            phase: InstallPhase::Installed,
            percent: Some(100.0),
            error,
        };
    }
    if [
        "Checksum verified",
        "checksum verification",
        "No published checksum",
        "Quitting AgentRQ",
        "Installing to ",
    ]
    .iter()
    .any(|marker| text.contains(marker))
    {
        return InstallerLogState {
            phase: InstallPhase::Installing,
            percent: None,
            error,
        };
    }

    let Some(downloading) = text.find("Downloading ") else {
        return InstallerLogState {
            phase: InstallPhase::Preparing,
            percent: None,
            error,
        };
    };
    let percent = last_percent(&text[downloading..]).map_or(0.0, |p| p.min(100.0));
    InstallerLogState {
        phase: InstallPhase::Downloading,
        percent: Some(percent),
        error,
    }
}

/// Whether a status change should interrupt the user.
///
/// A background check that finds nothing must stay silent — six-hourly "you are
/// up to date" toasts would be pure noise. The same status *asked for* by the
/// menu should answer, because the user is waiting for one.
pub fn should_announce(status: UpdateStatus, manual: bool) -> bool {
    matches!(status, UpdateStatus::Ready | UpdateStatus::Available) || manual
}

type OnStatus = Arc<dyn Fn(PublishedStatus) + Send + Sync>;
type CreateInstallLog = Arc<dyn Fn() -> Result<Arc<dyn InstallLog>, String> + Send + Sync>;

/// What an [`Updater`] is built from. Only the first three are required.
pub struct UpdaterDeps {
    pub auto_updater: Arc<dyn AutoUpdater>,
    pub is_packaged: bool,
    pub on_status: OnStatus,
    pub scheduler: Arc<dyn Scheduler>,
    pub logger: Arc<dyn UpdateLogger>,
    /// Without one, "update via the installer" is not offered.
    pub spawner: Option<Arc<dyn InstallerSpawner>>,
    /// As Node spells it: `darwin`, `linux`, `win32`.
    pub platform: String,
    /// Where the installer writes, so its progress can be read back. A file, not
    /// a pipe: the installer quits this app, and a pipe with no reader would
    /// kill it halfway through the install.
    pub create_install_log: Option<CreateInstallLog>,
}

impl UpdaterDeps {
    pub fn new(
        auto_updater: Arc<dyn AutoUpdater>,
        is_packaged: bool,
        on_status: impl Fn(PublishedStatus) + Send + Sync + 'static,
    ) -> Self {
        Self {
            auto_updater,
            is_packaged,
            on_status: Arc::new(on_status),
            scheduler: Arc::new(StdScheduler::default()),
            logger: Arc::new(ConsoleLogger),
            spawner: None,
            platform: current_platform(),
            create_install_log: None,
        }
    }
}

struct State {
    status: UpdateStatus,
    detail: String,
    remedy: String,
    manual: bool,
    timer: Option<u64>,
    version: String,
    progress: Option<Progress>,
    /// Events are only heard once `start` has bound them.
    bound: bool,
}

struct Inner {
    auto_updater: Arc<dyn AutoUpdater>,
    on_status: OnStatus,
    scheduler: Arc<dyn Scheduler>,
    logger: Arc<dyn UpdateLogger>,
    spawner: Option<Arc<dyn InstallerSpawner>>,
    platform: String,
    create_install_log: Option<CreateInstallLog>,
    disabled_reason: Option<&'static str>,
    state: Mutex<State>,
    outbox: Mutex<Outbox>,
}

/// Statuses decided but not yet handed to `on_status`, and whether a thread is
/// already handing them over.
struct Outbox {
    queue: VecDeque<PublishedStatus>,
    delivering: bool,
}

/// Follows a running installer: `done` and `last` are what the poll and the exit
/// handler, on different threads, have to agree on.
struct Follow {
    last: String,
    done: bool,
    poll: Option<u64>,
}

#[derive(Clone)]
pub struct Updater {
    inner: Arc<Inner>,
}

impl Updater {
    pub fn new(deps: UpdaterDeps) -> Self {
        let disabled_reason = updater_disabled_reason(deps.is_packaged);
        Self {
            inner: Arc::new(Inner {
                auto_updater: deps.auto_updater,
                on_status: deps.on_status,
                scheduler: deps.scheduler,
                logger: deps.logger,
                spawner: deps.spawner,
                platform: deps.platform,
                create_install_log: deps.create_install_log,
                disabled_reason,
                state: Mutex::new(State {
                    status: if disabled_reason.is_some() {
                        UpdateStatus::Disabled
                    } else {
                        UpdateStatus::Idle
                    },
                    detail: disabled_reason.unwrap_or("").to_string(),
                    remedy: String::new(),
                    manual: false,
                    timer: None,
                    version: String::new(),
                    progress: None,
                    bound: false,
                }),
                outbox: Mutex::new(Outbox {
                    queue: VecDeque::new(),
                    delivering: false,
                }),
            }),
        }
    }

    fn can_install_via_script(&self) -> bool {
        can_install_via_script(&self.inner.platform) && self.inner.spawner.is_some()
    }

    fn publish(&self, next: UpdateStatus, detail: &str, remedy: &str, progress: Option<Progress>) {
        self.enqueue(next, detail, remedy, progress);
        self.deliver();
    }

    /// Decide a status: record it and queue it, in one step under the state
    /// lock, so the queue order is the order the states changed in.
    fn enqueue(&self, next: UpdateStatus, detail: &str, remedy: &str, progress: Option<Progress>) {
        let mut state = self.inner.state.lock().unwrap();
        state.status = next;
        state.detail = detail.to_string();
        state.remedy = remedy.to_string();
        state.progress = progress;
        let published = PublishedStatus {
            status: next,
            detail: detail.to_string(),
            remedy: remedy.to_string(),
            version: state.version.clone(),
            progress,
            manual: state.manual,
            announce: should_announce(next, state.manual),
            can_install_via_script: self.can_install_via_script(),
        };
        self.inner.outbox.lock().unwrap().queue.push_back(published);
    }

    /// Hand the queued statuses to `on_status`, in order, with no lock held. One
    /// thread drains at a time; a publish from inside the callback, or from
    /// another thread mid-drain, is picked up by that drain.
    fn deliver(&self) {
        {
            let mut outbox = self.inner.outbox.lock().unwrap();
            if outbox.delivering {
                return;
            }
            outbox.delivering = true;
        }
        let _draining = Draining(&self.inner.outbox);
        loop {
            let next = {
                let mut outbox = self.inner.outbox.lock().unwrap();
                match outbox.queue.pop_front() {
                    Some(next) => next,
                    None => {
                        outbox.delivering = false;
                        return;
                    }
                }
            };
            (self.inner.on_status)(next);
        }
    }

    /// What the platform updater reported. Ignored until `start`, and always in
    /// a development build, where nothing is bound.
    pub fn handle_event(&self, event: UpdaterEvent) {
        if !self.inner.state.lock().unwrap().bound {
            return;
        }
        match event {
            UpdaterEvent::Checking => self.publish(UpdateStatus::Checking, "", "", None),
            UpdaterEvent::Available { version } => {
                self.inner.state.lock().unwrap().version = version.unwrap_or_default();
                self.publish(UpdateStatus::Available, "", "", None);
            }
            UpdaterEvent::DownloadProgress { percent } => {
                let percent = percent.unwrap_or(0.0).round();
                self.publish(
                    UpdateStatus::Downloading,
                    &format!("{}%", percent as i64),
                    "",
                    Some(Progress {
                        phase: InstallPhase::Downloading,
                        percent: Some(percent),
                    }),
                );
            }
            UpdaterEvent::Downloaded { version } => {
                if let Some(version) = version {
                    self.inner.state.lock().unwrap().version = version;
                }
                self.publish(UpdateStatus::Ready, "", "", None);
            }
            UpdaterEvent::NotAvailable => self.publish(UpdateStatus::UpToDate, "", "", None),
            UpdaterEvent::Error(error) => {
                self.inner.logger.warn(&format!("update failed: {error}"));
                self.publish(
                    UpdateStatus::Error,
                    &describe_update_error(Some(&error)),
                    remedy_for_update_error(Some(&error)),
                    None,
                );
            }
        }
    }

    fn check(&self, is_manual: bool) -> ActionResult {
        if let Some(reason) = self.inner.disabled_reason {
            // Answer the question when it was actually asked, rather than
            // silently doing nothing and leaving the menu looking broken.
            self.inner.state.lock().unwrap().manual = is_manual;
            self.publish(UpdateStatus::Disabled, reason, "", None);
            return ActionResult::refused(reason);
        }

        {
            let mut state = self.inner.state.lock().unwrap();
            // A background check would replace the installer's progress with
            // 'checking', and the banner would lose track of an install still
            // running.
            if state.status == UpdateStatus::Installing {
                return ActionResult::refused("An update is already being installed");
            }
            state.manual = is_manual;
        }

        match self.inner.auto_updater.check_for_updates() {
            Ok(()) => ActionResult::ok(),
            Err(error) => {
                // The platform updater also reports an error event; this covers
                // the check that could not start at all.
                let reason = describe_update_error(Some(&error));
                self.publish(
                    UpdateStatus::Error,
                    &reason,
                    remedy_for_update_error(Some(&error)),
                    None,
                );
                ActionResult::refused(reason)
            }
        }
    }

    /// Check once, then keep checking on a timer.
    pub fn start(&self) {
        if let Some(reason) = self.inner.disabled_reason {
            self.publish(UpdateStatus::Disabled, reason, "", None);
            return;
        }

        // Download in the background so an update is ready the moment the user
        // agrees to it, and install on quit if they never do.
        self.inner.auto_updater.set_auto_download(true);
        self.inner.auto_updater.set_auto_install_on_app_quit(true);

        self.inner.state.lock().unwrap().bound = true;
        self.check(false);

        let weak: Weak<Inner> = Arc::downgrade(&self.inner);
        let timer = self.inner.scheduler.set_interval(
            UPDATE_CHECK_INTERVAL_MS,
            Arc::new(move || {
                if let Some(inner) = weak.upgrade() {
                    Updater { inner }.check(false);
                }
            }),
        );
        let previous = self.inner.state.lock().unwrap().timer.replace(timer);
        if let Some(previous) = previous {
            self.inner.scheduler.clear_interval(previous);
        }
    }

    /// The menu's "Check for Updates…".
    pub fn check_now(&self) -> ActionResult {
        self.check(true)
    }

    /// Restart into the new version now.
    pub fn install_now(&self) -> bool {
        if self.inner.state.lock().unwrap().status != UpdateStatus::Ready {
            return false;
        }
        self.inner.auto_updater.quit_and_install();
        true
    }

    /// Report the installer's progress until it exits.
    ///
    /// On macOS it quits this app first, so the exit is usually never seen. When
    /// it is, the install either failed — offer it again with the error — or
    /// finished without restarting us, which is what happens on Linux.
    fn follow_installer(&self, child: Box<dyn InstallerChild>, log: Arc<dyn InstallLog>) {
        let follow = Arc::new(Mutex::new(Follow {
            last: String::new(),
            done: false,
            poll: None,
        }));

        let report = {
            let updater = self.clone();
            let log = Arc::clone(&log);
            let follow = Arc::clone(&follow);
            move || {
                let parsed = parse_installer_log(&log.read());
                let key = format!("{:?}:{:?}", parsed.phase, parsed.percent);
                // Decided under the follow lock, so a late poll cannot queue a
                // stale 'installing' behind the exit's verdict; delivered after
                // it is dropped, so `on_status` runs with no lock held.
                {
                    let mut follow = follow.lock().unwrap();
                    if follow.done || key == follow.last {
                        return;
                    }
                    follow.last = key;
                    updater.enqueue(
                        UpdateStatus::Installing,
                        "",
                        "",
                        Some(Progress {
                            phase: parsed.phase,
                            percent: parsed.percent,
                        }),
                    );
                }
                updater.deliver();
            }
        };

        report();
        let report = Arc::new(report);
        let tick = Arc::clone(&report);
        let poll = self
            .inner
            .scheduler
            .set_interval(INSTALL_POLL_INTERVAL_MS, Arc::new(move || tick()));
        follow.lock().unwrap().poll = Some(poll);

        let updater = self.clone();
        child.on_exit(Arc::new(move |exit| {
            {
                let mut follow = follow.lock().unwrap();
                if follow.done {
                    return;
                }
                follow.done = true;
                updater.enqueue_verdict(exit, &*log, follow.poll);
            }
            updater.deliver();
        }));
    }

    /// Queue what the installer's exit means, once the poll is stopped.
    fn enqueue_verdict(&self, exit: InstallerExit, log: &dyn InstallLog, poll: Option<u64>) {
        if let Some(poll) = poll {
            self.inner.scheduler.clear_interval(poll);
        }
        let parsed = parse_installer_log(&log.read());
        log.close();

        let (code, spawn_error) = match exit {
            InstallerExit::Exited(code) => (code, None),
            InstallerExit::Failed(error) => (None, Some(error)),
        };
        if parsed.error.is_empty()
            && spawn_error.is_none()
            && parsed.phase == InstallPhase::Installed
        {
            self.enqueue(
                UpdateStatus::Installed,
                "Restart AgentRQ to finish updating",
                "",
                None,
            );
            return;
        }
        let reason = if parsed.error.is_empty() {
            let cause = spawn_error.unwrap_or_else(|| {
                format!(
                    "The installer stopped (exit code {})",
                    code.map_or("null".to_string(), |c| c.to_string())
                )
            });
            describe_update_error(Some(&cause))
        } else {
            parsed.error
        };
        self.inner
            .logger
            .warn(&format!("installer failed: {reason}"));
        self.enqueue(UpdateStatus::Error, &reason, INSTALL_COMMAND, None);
    }

    fn launch_installer(
        &self,
        spawner: &Arc<dyn InstallerSpawner>,
        log: &mut Option<Arc<dyn InstallLog>>,
    ) -> Result<(), String> {
        if let Some(create) = &self.inner.create_install_log {
            *log = Some(create()?);
        }
        let stdio = match log {
            Some(log) => Some(log.stdio().map_err(|e| e.to_string())?),
            None => None,
        };
        let mut child = spawner.spawn(
            "/bin/sh",
            &["-c".to_string(), INSTALL_SCRIPT_COMMAND.to_string()],
            stdio,
        )?;
        child.unref();
        if let Some(log) = log {
            self.follow_installer(child, Arc::clone(log));
        }
        Ok(())
    }

    /// Update by running the one-command installer, for the builds that cannot
    /// replace themselves.
    ///
    /// An unsigned macOS build is the case this exists for: Squirrel.Mac checks
    /// the signature before swapping the bundle and refuses, so `install_now`
    /// can never succeed and the only route left is the installer that replaces
    /// the bundle wholesale. The installer is spawned detached, with its output
    /// in a file, for the reasons on [`InstallerSpawner`].
    pub fn install_via_script(&self) -> ActionResult {
        if !can_install_via_script(&self.inner.platform) {
            return ActionResult::refused("The installer does not support this platform");
        }
        let Some(spawner) = &self.inner.spawner else {
            return ActionResult::refused("Updates cannot be installed from here");
        };

        let mut log = None;
        match self.launch_installer(spawner, &mut log) {
            Ok(()) => ActionResult::ok(),
            Err(error) => {
                if let Some(log) = log {
                    log.close();
                }
                let reason = describe_update_error(Some(&error));
                self.inner
                    .logger
                    .warn(&format!("installer failed to start: {error}"));
                self.publish(UpdateStatus::Error, &reason, INSTALL_COMMAND, None);
                ActionResult::refused(reason)
            }
        }
    }

    pub fn stop(&self) {
        let timer = self.inner.state.lock().unwrap().timer.take();
        if let Some(timer) = timer {
            self.inner.scheduler.clear_interval(timer);
        }
    }

    pub fn state(&self) -> UpdateState {
        let state = self.inner.state.lock().unwrap();
        UpdateState {
            status: state.status,
            detail: state.detail.clone(),
            remedy: state.remedy.clone(),
            version: state.version.clone(),
            progress: state.progress,
            enabled: self.inner.disabled_reason.is_none(),
            can_install_via_script: self.can_install_via_script(),
        }
    }
}

/// Clears `delivering` if `on_status` unwinds, so one panic does not silence
/// every later status.
struct Draining<'a>(&'a Mutex<Outbox>);

impl Drop for Draining<'_> {
    fn drop(&mut self) {
        if std::thread::panicking() {
            if let Ok(mut outbox) = self.0.lock() {
                outbox.delivering = false;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicBool, AtomicUsize};

    const UNSIGNED_ERROR: &str = "Could not get code signature for running application";

    #[derive(Default)]
    struct FakeAutoUpdater {
        auto_download: AtomicBool,
        auto_install: AtomicBool,
        checks: AtomicUsize,
        quits: AtomicUsize,
        check_error: Mutex<Option<String>>,
    }

    impl AutoUpdater for FakeAutoUpdater {
        fn set_auto_download(&self, enabled: bool) {
            self.auto_download.store(enabled, Ordering::SeqCst);
        }

        fn set_auto_install_on_app_quit(&self, enabled: bool) {
            self.auto_install.store(enabled, Ordering::SeqCst);
        }

        fn check_for_updates(&self) -> Result<(), String> {
            self.checks.fetch_add(1, Ordering::SeqCst);
            self.check_error.lock().unwrap().clone().map_or(Ok(()), Err)
        }

        fn quit_and_install(&self) {
            self.quits.fetch_add(1, Ordering::SeqCst);
        }
    }

    impl FakeAutoUpdater {
        fn checks(&self) -> usize {
            self.checks.load(Ordering::SeqCst)
        }
    }

    type Tick = Arc<dyn Fn() + Send + Sync>;

    /// Intervals the test fires by hand.
    #[derive(Default)]
    struct FakeScheduler {
        next: AtomicU64,
        intervals: Mutex<Vec<(u64, u64, Tick)>>,
        cleared: Mutex<Vec<u64>>,
    }

    impl Scheduler for FakeScheduler {
        fn set_interval(&self, ms: u64, tick: Tick) -> u64 {
            let handle = self.next.fetch_add(1, Ordering::SeqCst) + 1;
            self.intervals.lock().unwrap().push((handle, ms, tick));
            handle
        }

        fn clear_interval(&self, handle: u64) {
            self.cleared.lock().unwrap().push(handle);
        }
    }

    impl FakeScheduler {
        fn tick(&self, ms: u64) {
            let tick = self
                .intervals
                .lock()
                .unwrap()
                .iter()
                .find(|(_, at, _)| *at == ms)
                .map(|(_, _, tick)| Arc::clone(tick))
                .unwrap();
            tick();
        }

        fn handles(&self) -> Vec<u64> {
            self.intervals.lock().unwrap().iter().map(|i| i.0).collect()
        }

        fn cleared(&self) -> Vec<u64> {
            self.cleared.lock().unwrap().clone()
        }
    }

    #[derive(Default)]
    struct TestLogger {
        warnings: Mutex<Vec<String>>,
    }

    impl UpdateLogger for TestLogger {
        fn warn(&self, message: &str) {
            self.warnings.lock().unwrap().push(message.to_string());
        }
    }

    struct Setup {
        updater: Updater,
        auto: Arc<FakeAutoUpdater>,
        published: Arc<Mutex<Vec<PublishedStatus>>>,
        scheduler: Arc<FakeScheduler>,
        logger: Arc<TestLogger>,
    }

    impl Setup {
        fn emit(&self, event: UpdaterEvent) {
            self.updater.handle_event(event);
        }

        fn last(&self) -> PublishedStatus {
            self.published.lock().unwrap().last().cloned().unwrap()
        }

        fn statuses(&self) -> Vec<UpdateStatus> {
            self.published
                .lock()
                .unwrap()
                .iter()
                .map(|p| p.status)
                .collect()
        }
    }

    fn setup_with(is_packaged: bool, check_error: Option<&str>) -> Setup {
        let auto = Arc::new(FakeAutoUpdater::default());
        *auto.check_error.lock().unwrap() = check_error.map(str::to_string);
        let published = Arc::new(Mutex::new(Vec::new()));
        let sink = Arc::clone(&published);
        let scheduler = Arc::new(FakeScheduler::default());
        let logger = Arc::new(TestLogger::default());
        let mut deps = UpdaterDeps::new(auto.clone(), is_packaged, move |p| {
            sink.lock().unwrap().push(p)
        });
        deps.scheduler = scheduler.clone();
        deps.logger = logger.clone();
        Setup {
            updater: Updater::new(deps),
            auto,
            published,
            scheduler,
            logger,
        }
    }

    fn setup() -> Setup {
        setup_with(true, None)
    }

    fn available(version: &str) -> UpdaterEvent {
        UpdaterEvent::Available {
            version: Some(version.to_string()),
        }
    }

    fn downloaded(version: &str) -> UpdaterEvent {
        UpdaterEvent::Downloaded {
            version: Some(version.to_string()),
        }
    }

    fn error(message: &str) -> UpdaterEvent {
        UpdaterEvent::Error(message.to_string())
    }

    // ---- updater_disabled_reason ----

    #[test]
    fn allows_updating_in_a_packaged_app() {
        assert_eq!(updater_disabled_reason(true), None);
    }

    #[test]
    fn refuses_in_a_development_build() {
        // The hard guard: `make dev` must never reach the update path, where the
        // updater would try to replace a checkout with a release build.
        assert_eq!(
            updater_disabled_reason(false),
            Some("Updates are disabled in a development build")
        );
    }

    // ---- describe_update_error ----

    #[test]
    fn explains_the_macos_signing_requirement_in_words_a_person_can_act_on() {
        // The predictable failure: Squirrel.Mac refuses to install onto an unsigned
        // app, and the raw error says nothing useful.
        let signed = "This build is not signed, so it cannot update itself";
        assert_eq!(describe_update_error(Some(UNSIGNED_ERROR)), signed);
        assert_eq!(describe_update_error(Some("SQRLUpdater failed")), signed);
    }

    #[test]
    fn reports_a_network_failure_as_one() {
        for message in [
            "net::ERR_INTERNET_DISCONNECTED",
            "getaddrinfo ENOTFOUND github.com",
            "ETIMEDOUT",
        ] {
            assert_eq!(
                describe_update_error(Some(message)),
                "Could not reach the update server"
            );
        }
    }

    #[test]
    fn recognises_there_being_nothing_to_update_to() {
        let none = "No published release to update to";
        assert_eq!(
            describe_update_error(Some("HttpError: 404 Not Found")),
            none
        );
        assert_eq!(describe_update_error(Some("Cannot find latest.yml")), none);
    }

    #[test]
    fn recognises_a_build_that_was_never_set_up_to_update_itself() {
        // Only a build packaged with a publish configuration carries app-update.yml;
        // an unpackaged or --dir build hits this and the raw ENOENT explains nothing.
        assert_eq!(
            describe_update_error(Some(
                "ENOENT: no such file or directory, open '/x/app-update.yml'"
            )),
            "This build has no update configuration"
        );
    }

    #[test]
    fn passes_an_unrecognised_message_through_rather_than_hiding_it() {
        assert_eq!(
            describe_update_error(Some("unrecognised updater failure")),
            "unrecognised updater failure"
        );
    }

    #[test]
    fn copes_with_a_thrown_value_that_is_not_an_error() {
        assert_eq!(describe_update_error(Some("plain string")), "plain string");
        assert_eq!(describe_update_error(None), "Unknown error");
    }

    #[test]
    fn matches_latest_yml_only_within_one_line() {
        // `.*` does not cross a line break, so these are two unrelated mentions.
        assert_eq!(
            describe_update_error(Some("the latest build\nwrote x.yml")),
            "the latest build\nwrote x.yml"
        );
        assert_eq!(
            describe_update_error(Some("latest is stale\nCannot find latest.yml")),
            "No published release to update to"
        );
    }

    // ---- remedy_for_update_error ----

    #[test]
    fn offers_the_install_command_for_the_signature_failure() {
        // The whole point: this failure is not a dead end, and the user should not
        // have to go and find the command in the docs.
        assert_eq!(
            remedy_for_update_error(Some(UNSIGNED_ERROR)),
            INSTALL_COMMAND
        );
        assert_eq!(
            remedy_for_update_error(Some("SQRLUpdater failed")),
            INSTALL_COMMAND
        );
    }

    #[test]
    fn offers_nothing_for_failures_reinstalling_would_not_fix() {
        // A command that cannot help is worse than no command at all.
        assert_eq!(
            remedy_for_update_error(Some("net::ERR_CONNECTION_REFUSED")),
            ""
        );
        assert_eq!(
            remedy_for_update_error(Some("HttpError: 404 Not Found")),
            ""
        );
        assert_eq!(
            remedy_for_update_error(Some("unrecognised updater failure")),
            ""
        );
        assert_eq!(remedy_for_update_error(None), "");
    }

    // ---- should_announce ----

    #[test]
    fn always_announces_an_update_that_exists() {
        assert!(should_announce(UpdateStatus::Available, false));
        assert!(should_announce(UpdateStatus::Ready, false));
    }

    #[test]
    fn stays_silent_about_a_background_check_that_found_nothing() {
        // Six-hourly "you are up to date" toasts would be pure noise.
        assert!(!should_announce(UpdateStatus::UpToDate, false));
        assert!(!should_announce(UpdateStatus::Checking, false));
        assert!(!should_announce(UpdateStatus::Error, false));
    }

    #[test]
    fn answers_a_question_the_user_actually_asked() {
        for status in [
            UpdateStatus::Checking,
            UpdateStatus::UpToDate,
            UpdateStatus::Error,
            UpdateStatus::Disabled,
        ] {
            assert!(should_announce(status, true));
        }
    }

    // ---- createUpdater ----

    #[test]
    fn downloads_in_the_background_and_installs_on_quit() {
        // So an update is ready the moment the user agrees, and still lands if
        // they never do.
        let s = setup();
        s.updater.start();

        assert!(s.auto.auto_download.load(Ordering::SeqCst));
        assert!(s.auto.auto_install.load(Ordering::SeqCst));
    }

    #[test]
    fn checks_at_launch_and_then_on_a_fifteen_minute_timer() {
        let s = setup();
        s.updater.start();

        assert_eq!(s.auto.checks(), 1);
        let intervals = s.scheduler.intervals.lock().unwrap();
        assert_eq!(intervals.len(), 1);
        assert_eq!(intervals[0].1, UPDATE_CHECK_INTERVAL_MS);
        // Fifteen minutes: a release should reach a running app the same session
        // it ships, not six hours later.
        assert_eq!(UPDATE_CHECK_INTERVAL_MS, 15 * 60 * 1000);
    }

    #[test]
    fn keeps_checking_when_the_timer_fires() {
        let s = setup();
        s.updater.start();

        s.scheduler.tick(UPDATE_CHECK_INTERVAL_MS);

        assert_eq!(s.auto.checks(), 2);
    }

    #[test]
    fn reports_the_whole_lifecycle_of_a_successful_update() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::Checking);
        s.emit(available("0.5.0"));
        s.emit(UpdaterEvent::DownloadProgress {
            percent: Some(42.4),
        });
        s.emit(downloaded("0.5.0"));

        assert_eq!(
            s.statuses(),
            vec![
                UpdateStatus::Checking,
                UpdateStatus::Available,
                UpdateStatus::Downloading,
                UpdateStatus::Ready,
            ]
        );
        let state = s.updater.state();
        assert_eq!(state.status, UpdateStatus::Ready);
        assert_eq!(state.version, "0.5.0");
    }

    #[test]
    fn reports_download_progress_as_a_rounded_percentage() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::DownloadProgress {
            percent: Some(42.4),
        });
        assert_eq!(s.last().detail, "42%");

        s.emit(UpdaterEvent::DownloadProgress { percent: None });
        assert_eq!(s.last().detail, "0%");
    }

    #[test]
    fn carries_the_download_percentage_as_a_number_the_banner_can_draw() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::DownloadProgress {
            percent: Some(42.4),
        });
        assert_eq!(
            s.last().progress,
            Some(Progress {
                phase: InstallPhase::Downloading,
                percent: Some(42.0)
            })
        );

        // And it is gone again once the download is done.
        s.emit(downloaded("0.5.0"));
        assert_eq!(s.updater.state().progress, None);
    }

    #[test]
    fn reports_being_up_to_date() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::NotAvailable);

        assert_eq!(s.updater.state().status, UpdateStatus::UpToDate);
    }

    #[test]
    fn reports_a_failure_without_throwing() {
        let s = setup();
        s.updater.start();

        s.emit(error("net::ERR_INTERNET_DISCONNECTED"));

        let state = s.updater.state();
        assert_eq!(state.status, UpdateStatus::Error);
        assert_eq!(state.detail, "Could not reach the update server");
        assert_eq!(
            *s.logger.warnings.lock().unwrap(),
            vec!["update failed: net::ERR_INTERNET_DISCONNECTED"]
        );
    }

    #[test]
    fn copes_with_an_update_available_carrying_no_version() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::Available { version: None });
        assert_eq!(s.updater.state().version, "");
    }

    #[test]
    fn keeps_the_version_from_update_available_if_the_download_omits_it() {
        let s = setup();
        s.updater.start();

        s.emit(available("0.5.0"));
        s.emit(UpdaterEvent::Downloaded { version: None });

        assert_eq!(s.updater.state().version, "0.5.0");
    }

    #[test]
    fn marks_a_manual_check_as_announceable_and_a_background_one_as_not() {
        let s = setup();
        s.updater.start();

        s.emit(UpdaterEvent::NotAvailable);
        assert!(!s.last().announce);

        s.updater.check_now();
        s.emit(UpdaterEvent::NotAvailable);
        assert!(s.last().announce);
        assert!(s.last().manual);
    }

    #[test]
    fn surfaces_a_rejected_check_rather_than_leaving_an_unhandled_rejection() {
        let s = setup_with(true, Some("HttpError: 404"));

        let result = s.updater.check_now();

        assert_eq!(
            result,
            ActionResult {
                ok: false,
                reason: Some("No published release to update to".to_string())
            }
        );
        assert_eq!(s.updater.state().status, UpdateStatus::Error);
    }

    #[test]
    fn reports_a_successful_check() {
        let s = setup();
        assert_eq!(
            s.updater.check_now(),
            ActionResult {
                ok: true,
                reason: None
            }
        );
    }

    mod in_a_development_build {
        use super::*;

        #[test]
        fn never_touches_the_updater() {
            let s = setup_with(false, None);
            s.updater.start();

            assert_eq!(s.auto.checks(), 0);
            assert!(s.scheduler.handles().is_empty());
            assert!(!s.auto.auto_download.load(Ordering::SeqCst));
        }

        #[test]
        fn says_so_rather_than_appearing_broken() {
            // The menu item is reachable in dev; silently doing nothing would look
            // like a bug.
            let s = setup_with(false, None);

            let result = s.updater.check_now();

            assert_eq!(
                result,
                ActionResult {
                    ok: false,
                    reason: Some("Updates are disabled in a development build".to_string())
                }
            );
            assert_eq!(s.last().status, UpdateStatus::Disabled);
            assert!(s.last().announce);
        }

        #[test]
        fn starts_in_the_disabled_state() {
            let s = setup_with(false, None);
            let state = s.updater.state();
            assert_eq!(state.status, UpdateStatus::Disabled);
            assert!(!state.enabled);

            s.updater.start();
            assert_eq!(s.updater.state().status, UpdateStatus::Disabled);
        }
    }

    mod install_now {
        use super::*;

        #[test]
        fn restarts_into_the_new_version_once_one_is_downloaded() {
            let s = setup();
            s.updater.start();
            s.emit(downloaded("0.5.0"));

            assert!(s.updater.install_now());
            assert_eq!(s.auto.quits.load(Ordering::SeqCst), 1);
        }

        #[test]
        fn does_nothing_when_there_is_nothing_downloaded() {
            // Quitting the app to install an update that does not exist would be a
            // spectacular way to lose someone's work.
            let s = setup();
            s.updater.start();

            assert!(!s.updater.install_now());
            s.emit(available("0.5.0"));
            assert!(!s.updater.install_now());

            assert_eq!(s.auto.quits.load(Ordering::SeqCst), 0);
        }
    }

    mod stop {
        use super::*;

        #[test]
        fn cancels_the_timer() {
            let s = setup();
            s.updater.start();
            s.updater.stop();

            assert_eq!(s.scheduler.cleared(), s.scheduler.handles());
        }

        #[test]
        fn is_safe_when_nothing_was_started() {
            let s = setup();
            s.updater.stop();

            assert!(s.scheduler.cleared().is_empty());
        }

        #[test]
        fn is_safe_to_call_twice() {
            let s = setup();
            s.updater.start();
            s.updater.stop();
            s.updater.stop();

            assert_eq!(s.scheduler.cleared().len(), 1);
        }
    }

    #[test]
    fn falls_back_to_the_console_when_no_logger_is_supplied() {
        let auto = Arc::new(FakeAutoUpdater::default());
        let mut deps = UpdaterDeps::new(auto, true, |_| {});
        deps.scheduler = Arc::new(FakeScheduler::default());
        let updater = Updater::new(deps);

        updater.start();
        updater.handle_event(error("update server unreachable"));

        assert_eq!(updater.state().status, UpdateStatus::Error);
        // The default logger writes to stderr, which cannot be captured here.
        ConsoleLogger.warn("update failed: update server unreachable");
    }

    #[test]
    fn uses_real_timers_when_none_are_injected() {
        let auto = Arc::new(FakeAutoUpdater::default());
        let updater = Updater::new(UpdaterDeps::new(auto.clone(), true, |_| {}));

        updater.start();
        assert_eq!(auto.checks(), 1);
        // Nothing has been reported yet; the first status runs the default
        // handler.
        updater.handle_event(UpdaterEvent::NotAvailable);
        // Leaving a fifteen-minute interval armed would keep its thread alive.
        updater.stop();
    }

    #[test]
    fn hears_nothing_before_it_is_started() {
        let s = setup();
        s.emit(UpdaterEvent::NotAvailable);
        assert!(s.published.lock().unwrap().is_empty());
    }

    #[test]
    fn replaces_its_timer_when_started_again() {
        let s = setup();
        s.updater.start();
        s.updater.start();

        assert_eq!(s.scheduler.cleared(), vec![1]);
    }

    #[test]
    fn stops_checking_once_it_is_gone() {
        let s = setup();
        s.updater.start();
        let checks = s.auto.checks();
        let tick = Arc::clone(&s.scheduler.intervals.lock().unwrap()[0].2);
        drop(s.updater);

        tick();
        assert_eq!(s.auto.checks(), checks);
    }

    // ---- canInstallViaScript ----

    #[test]
    fn is_available_where_install_sh_runs() {
        assert!(can_install_via_script("darwin"));
        assert!(can_install_via_script("linux"));
    }

    #[test]
    fn is_not_available_on_windows() {
        // install.sh refuses Windows by design — the NSIS installer and the
        // platform updater already handle it — so a button here would only fail.
        assert!(!can_install_via_script("win32"));
    }

    #[test]
    fn names_the_platform_as_node_does() {
        assert_eq!(platform_name("macos"), "darwin");
        assert_eq!(platform_name("windows"), "win32");
        assert_eq!(platform_name("linux"), "linux");
        assert!(!current_platform().is_empty());
    }

    // ---- installViaScript ----

    /// A child whose end the test controls.
    #[derive(Default)]
    struct ChildState {
        handlers: Mutex<Vec<ExitHandler>>,
        unrefs: AtomicUsize,
    }

    impl ChildState {
        fn end(&self, exit: InstallerExit) {
            let handlers = self.handlers.lock().unwrap().clone();
            for handler in handlers {
                handler(exit.clone());
            }
        }
    }

    struct FakeChild(Arc<ChildState>);

    impl InstallerChild for FakeChild {
        fn unref(&mut self) {
            self.0.unrefs.fetch_add(1, Ordering::SeqCst);
        }

        fn on_exit(self: Box<Self>, handler: ExitHandler) {
            self.0.handlers.lock().unwrap().push(handler);
        }
    }

    #[derive(Default)]
    struct FakeSpawner {
        child: Arc<ChildState>,
        fail_with: Option<String>,
        calls: Mutex<Vec<(String, Vec<String>, bool)>>,
    }

    impl InstallerSpawner for FakeSpawner {
        fn spawn(
            &self,
            command: &str,
            args: &[String],
            stdio: Option<(Stdio, Stdio)>,
        ) -> Result<Box<dyn InstallerChild>, String> {
            self.calls
                .lock()
                .unwrap()
                .push((command.to_string(), args.to_vec(), stdio.is_some()));
            match &self.fail_with {
                Some(error) => Err(error.clone()),
                None => Ok(Box::new(FakeChild(Arc::clone(&self.child)))),
            }
        }
    }

    /// A log we can write to.
    #[derive(Default)]
    struct FakeLog {
        text: Mutex<String>,
        closes: AtomicUsize,
        stdio_fails: AtomicBool,
    }

    impl InstallLog for FakeLog {
        fn stdio(&self) -> std::io::Result<(Stdio, Stdio)> {
            if self.stdio_fails.load(Ordering::SeqCst) {
                return Err(std::io::Error::other("the install log is closed"));
            }
            Ok((Stdio::null(), Stdio::null()))
        }

        fn read(&self) -> String {
            self.text.lock().unwrap().clone()
        }

        fn close(&self) {
            self.closes.fetch_add(1, Ordering::SeqCst);
        }
    }

    impl FakeLog {
        fn write(&self, text: &str) {
            self.text.lock().unwrap().push_str(text);
        }

        fn closes(&self) -> usize {
            self.closes.load(Ordering::SeqCst)
        }
    }

    struct Spawn {
        setup: Setup,
        spawner: Arc<FakeSpawner>,
        log: Arc<FakeLog>,
    }

    impl Spawn {
        fn poll(&self) {
            self.setup.scheduler.tick(INSTALL_POLL_INTERVAL_MS);
        }
    }

    fn with_spawn(
        platform: &str,
        spawner: Option<FakeSpawner>,
        with_log: bool,
    ) -> (Spawn, Option<Arc<FakeSpawner>>) {
        let mut s = setup();
        let spawner = spawner.map(Arc::new);
        let log = Arc::new(FakeLog::default());
        // `setup` already built an updater; rebuild it with the extras.
        let sink = Arc::clone(&s.published);
        let mut deps =
            UpdaterDeps::new(s.auto.clone(), true, move |p| sink.lock().unwrap().push(p));
        deps.scheduler = s.scheduler.clone();
        deps.logger = s.logger.clone();
        deps.platform = platform.to_string();
        deps.spawner = spawner.clone().map(|f| f as Arc<dyn InstallerSpawner>);
        if with_log {
            let log = Arc::clone(&log);
            deps.create_install_log =
                Some(Arc::new(move || Ok(log.clone() as Arc<dyn InstallLog>)));
        }
        s.updater = Updater::new(deps);
        (
            Spawn {
                setup: s,
                spawner: spawner.clone().unwrap_or_default(),
                log,
            },
            spawner,
        )
    }

    fn running(platform: &str) -> Spawn {
        with_spawn(platform, Some(FakeSpawner::default()), true).0
    }

    #[test]
    fn runs_the_installer_detached_so_it_survives_the_app_it_closes() {
        // The installer's first act is to quit this app and wait for the process
        // to go. A child sharing our lifetime would be killed by the very thing it
        // just did, halfway through replacing the application bundle.
        let (s, _) = with_spawn("darwin", Some(FakeSpawner::default()), false);

        assert_eq!(
            s.setup.updater.install_via_script(),
            ActionResult {
                ok: true,
                reason: None
            }
        );

        let calls = s.spawner.calls.lock().unwrap();
        assert_eq!(
            *calls,
            vec![(
                "/bin/sh".to_string(),
                vec!["-c".to_string(), INSTALL_SCRIPT_COMMAND.to_string()],
                false
            )]
        );
        assert_eq!(s.spawner.child.unrefs.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn quits_the_running_app_and_reopens_it() {
        // install.sh refuses to replace a bundle that is running, and does not
        // relaunch on its own — it prints "Launch it with: open -a AgentRQ".
        assert!(INSTALL_SCRIPT_COMMAND.contains("--quit"));
        assert!(INSTALL_SCRIPT_COMMAND.contains("open -a AgentRQ"));
        // Chained with && so a failed install does not reopen a broken bundle.
        assert!(INSTALL_SCRIPT_COMMAND.contains("&& open -a AgentRQ"));
    }

    #[test]
    fn refuses_on_a_platform_the_installer_does_not_support() {
        let (s, _) = with_spawn("win32", Some(FakeSpawner::default()), false);

        assert!(!s.setup.updater.install_via_script().ok);
        assert!(s.spawner.calls.lock().unwrap().is_empty());
    }

    #[test]
    fn refuses_when_no_spawn_was_provided() {
        let (s, _) = with_spawn("darwin", None, false);

        let result = s.setup.updater.install_via_script();

        assert!(!result.ok);
        assert_eq!(
            result.reason.as_deref(),
            Some("Updates cannot be installed from here")
        );
    }

    #[test]
    fn reports_a_spawn_that_fails_rather_than_throwing() {
        let failing = FakeSpawner {
            fail_with: Some("EPERM".to_string()),
            ..Default::default()
        };
        let (s, _) = with_spawn("darwin", Some(failing), false);

        let result = s.setup.updater.install_via_script();

        assert!(!result.ok);
        let state = s.setup.updater.state();
        assert_eq!(state.status, UpdateStatus::Error);
        // The command is still offered, so the user has a way through by hand.
        assert_eq!(state.remedy, INSTALL_COMMAND);
    }

    #[test]
    fn tells_the_banner_whether_this_route_exists() {
        let darwin = with_spawn("darwin", Some(FakeSpawner::default()), false).0;
        let windows = with_spawn("win32", Some(FakeSpawner::default()), false).0;
        let no_spawn = with_spawn("darwin", None, false).0;
        assert!(darwin.setup.updater.state().can_install_via_script);
        assert!(!windows.setup.updater.state().can_install_via_script);
        assert!(!no_spawn.setup.updater.state().can_install_via_script);
    }

    // ---- what the renderer is told ----

    #[test]
    fn carries_can_install_via_script_on_every_published_status() {
        // The banner decides on this, and it only ever sees what publish sends —
        // reading it from `state` instead would leave the renderer blind.
        let (s, _) = with_spawn("darwin", Some(FakeSpawner::default()), false);
        s.setup.updater.start();

        s.setup.updater.handle_event(available("1.2.0"));

        let published = s.setup.last();
        assert!(published.can_install_via_script);
        assert_eq!(published.status, UpdateStatus::Available);
        assert_eq!(published.version, "1.2.0");
    }

    #[test]
    fn serialises_what_the_renderer_reads_in_camel_case() {
        let (s, _) = with_spawn("darwin", Some(FakeSpawner::default()), false);
        s.setup.updater.start();
        s.setup.updater.handle_event(UpdaterEvent::NotAvailable);

        let published = serde_json::to_value(s.setup.last()).unwrap();
        assert_eq!(
            published,
            serde_json::json!({
                "status": "up-to-date", "detail": "", "remedy": "", "version": "",
                "progress": null, "manual": false, "announce": false,
                "canInstallViaScript": true,
            })
        );
        let state = serde_json::to_value(s.setup.updater.state()).unwrap();
        assert_eq!(state["enabled"], true);
        assert_eq!(state["canInstallViaScript"], true);
        let refusal = serde_json::to_value(ActionResult::refused("no")).unwrap();
        assert_eq!(refusal, serde_json::json!({"ok": false, "reason": "no"}));
        let ok = serde_json::to_value(ActionResult::ok()).unwrap();
        assert_eq!(ok, serde_json::json!({"ok": true}));
        let progress = serde_json::to_value(Progress {
            phase: InstallPhase::Installing,
            percent: None,
        })
        .unwrap();
        assert_eq!(
            progress,
            serde_json::json!({"phase": "installing", "percent": null})
        );
    }

    // ---- parseInstallerLog ----

    // What install.sh really writes: `say` lines, and curl's --progress-bar,
    // which redraws with a carriage return rather than a newline.
    const LOOKING_UP: &str = "Looking up the latest AgentRQ release...\n";

    fn downloading() -> String {
        format!("{LOOKING_UP}Downloading AgentRQ-1.2.0-arm64.dmg...\n")
    }

    fn state_of(phase: InstallPhase, percent: Option<f64>, error: &str) -> InstallerLogState {
        InstallerLogState {
            phase,
            percent,
            error: error.to_string(),
        }
    }

    #[test]
    fn trims_a_byte_order_mark_like_js_trim() {
        assert_eq!(
            parse_installer_log("error: \u{feff}bom trimmed?\u{feff}\n").error,
            "bom trimmed?"
        );
    }

    #[test]
    fn starts_out_preparing() {
        assert_eq!(
            parse_installer_log(""),
            state_of(InstallPhase::Preparing, None, "")
        );
        assert_eq!(
            parse_installer_log(LOOKING_UP).phase,
            InstallPhase::Preparing
        );
    }

    #[test]
    fn reads_the_latest_percentage_off_the_progress_bar() {
        assert_eq!(
            parse_installer_log(&downloading()),
            state_of(InstallPhase::Downloading, Some(0.0), "")
        );
        let partway = format!("{}\r#=#=#   \r##      3.1%\r#####     41.7%", downloading());
        assert_eq!(parse_installer_log(&partway).percent, Some(41.7));
        let done = format!("{}\r######## 100.0%\n", downloading());
        assert_eq!(parse_installer_log(&done).percent, Some(100.0));
    }

    #[test]
    fn ignores_a_percentage_printed_before_the_download_began() {
        let log = format!("50%\n{}", downloading());
        assert_eq!(parse_installer_log(&log).percent, Some(0.0));
    }

    #[test]
    fn moves_on_to_installing_once_the_download_is_verified() {
        for line in [
            "  Checksum verified.",
            "  No published checksum for x.dmg; skipping verification.",
            "  openssl not found; skipping checksum verification.",
            "Quitting AgentRQ...",
            "Installing to /Applications/AgentRQ.app...",
        ] {
            let log = format!("{}\r### 100.0%\n{line}\n", downloading());
            assert_eq!(
                parse_installer_log(&log),
                state_of(InstallPhase::Installing, None, ""),
                "{line}"
            );
        }
    }

    #[test]
    fn recognises_a_finished_install_and_one_there_was_nothing_to_do_for() {
        assert_eq!(
            parse_installer_log("AgentRQ 1.2.0 installed to /home/me/.local/bin/agentrq").phase,
            InstallPhase::Installed
        );
        assert_eq!(
            parse_installer_log("AgentRQ 1.2.0 is already installed. Nothing to do.").phase,
            InstallPhase::Installed
        );
    }

    #[test]
    fn picks_out_the_reason_the_installer_died() {
        let log = format!(
            "{}curl: (6) Could not resolve host\nerror: download failed: https://x \n",
            downloading()
        );
        assert_eq!(
            parse_installer_log(&log).error,
            "download failed: https://x"
        );
    }

    #[test]
    fn reads_a_percentage_the_way_curl_writes_it() {
        // Digits that are not a percentage, a fraction with no digits, and an
        // error that follows a progress redraw on the same "line".
        let log = format!("{}\r12.5x 7.% 3.5%\r9\rerror: late", downloading());
        let parsed = parse_installer_log(&log);
        assert_eq!(parsed.percent, Some(3.5));
        assert_eq!(parsed.error, "late");
        let ended = format!("{}7% 12.", downloading());
        assert_eq!(parse_installer_log(&ended).percent, Some(7.0));
        let huge = format!("{}1000%", downloading());
        assert_eq!(parse_installer_log(&huge).percent, Some(100.0));
    }

    // ---- following the installer ----

    #[test]
    fn hands_the_installer_the_log_file_not_a_pipe() {
        let s = running("darwin");

        assert_eq!(
            s.setup.updater.install_via_script(),
            ActionResult {
                ok: true,
                reason: None
            }
        );
        let calls = s.spawner.calls.lock().unwrap();
        assert!(calls[0].2, "stdio is the log, not discarded");
    }

    #[test]
    fn reports_each_step_of_the_install_as_it_happens() {
        let s = running("darwin");
        s.setup.updater.install_via_script();

        let state = s.setup.updater.state();
        assert_eq!(state.status, UpdateStatus::Installing);
        assert_eq!(
            state.progress,
            Some(Progress {
                phase: InstallPhase::Preparing,
                percent: None
            })
        );

        s.log
            .write("Downloading AgentRQ-1.2.0-arm64.dmg...\n\r##   12.5%");
        s.poll();
        assert_eq!(
            s.setup.updater.state().progress,
            Some(Progress {
                phase: InstallPhase::Downloading,
                percent: Some(12.5)
            })
        );

        // Nothing new written: nothing new published.
        let published = s.setup.published.lock().unwrap().len();
        s.poll();
        assert_eq!(s.setup.published.lock().unwrap().len(), published);

        s.log.write("\r######## 100.0%\n  Checksum verified.\n");
        s.poll();
        assert_eq!(
            s.setup.updater.state().progress,
            Some(Progress {
                phase: InstallPhase::Installing,
                percent: None
            })
        );
    }

    #[test]
    fn offers_the_update_again_with_the_reason_when_the_installer_fails() {
        let s = running("darwin");
        s.setup.updater.install_via_script();

        s.log
            .write("error: could not fetch https://api.github.com -- no such release\n");
        s.spawner.child.end(InstallerExit::Exited(Some(1)));

        let state = s.setup.updater.state();
        assert_eq!(state.status, UpdateStatus::Error);
        assert_eq!(
            state.detail,
            "could not fetch https://api.github.com -- no such release"
        );
        assert_eq!(state.remedy, INSTALL_COMMAND);
        assert_eq!(state.progress, None);
        assert_eq!(s.setup.scheduler.cleared(), vec![1]);
        assert_eq!(s.log.closes(), 1);
    }

    #[test]
    fn says_why_when_the_installer_stops_without_explaining_itself() {
        let s = running("darwin");
        s.setup.updater.install_via_script();

        s.spawner.child.end(InstallerExit::Exited(Some(137)));

        assert_eq!(
            s.setup.updater.state().detail,
            "The installer stopped (exit code 137)"
        );
    }

    #[test]
    fn says_so_when_a_signal_ended_the_installer() {
        let s = running("darwin");
        s.setup.updater.install_via_script();

        s.spawner.child.end(InstallerExit::Exited(None));

        assert_eq!(
            s.setup.updater.state().detail,
            "The installer stopped (exit code null)"
        );
    }

    #[test]
    fn reports_a_shell_that_never_started() {
        let s = running("darwin");
        s.setup.updater.install_via_script();

        s.spawner
            .child
            .end(InstallerExit::Failed("spawn /bin/sh ENOENT".to_string()));
        // A failed spawn can be followed by an exit as well; it must not report twice.
        s.spawner.child.end(InstallerExit::Exited(None));

        let state = s.setup.updater.state();
        assert_eq!(state.status, UpdateStatus::Error);
        assert_eq!(state.detail, "spawn /bin/sh ENOENT");
        assert_eq!(s.log.closes(), 1);
    }

    #[test]
    fn asks_for_a_restart_when_the_install_finished_and_this_app_is_still_running() {
        // Linux: install.sh swaps the AppImage without quitting us, and the macOS
        // relaunch that follows it fails there — which is not the install failing.
        let s = running("linux");
        s.setup.updater.install_via_script();

        s.log
            .write("AgentRQ 1.2.0 installed to /home/me/.local/bin/agentrq\n");
        s.spawner.child.end(InstallerExit::Exited(Some(127)));

        let state = s.setup.updater.state();
        assert_eq!(state.status, UpdateStatus::Installed);
        assert_eq!(state.detail, "Restart AgentRQ to finish updating");
    }

    #[test]
    fn does_not_let_a_background_check_interrupt_an_install() {
        let s = running("darwin");
        s.setup.updater.start();
        s.setup.updater.install_via_script();

        s.setup.scheduler.tick(UPDATE_CHECK_INTERVAL_MS);

        assert_eq!(
            s.setup.updater.check_now(),
            ActionResult {
                ok: false,
                reason: Some("An update is already being installed".to_string())
            }
        );
        assert_eq!(s.setup.updater.state().status, UpdateStatus::Installing);
        assert_eq!(s.setup.auto.checks(), 1, "only the launch check ran");
    }

    #[test]
    fn closes_the_log_when_the_installer_cannot_be_started() {
        let failing = FakeSpawner {
            fail_with: Some("EPERM".to_string()),
            ..Default::default()
        };
        let (s, _) = with_spawn("darwin", Some(failing), true);

        assert!(!s.setup.updater.install_via_script().ok);
        assert_eq!(s.log.closes(), 1);
    }

    #[test]
    fn says_so_when_the_log_cannot_be_created() {
        let mut deps = UpdaterDeps::new(Arc::new(FakeAutoUpdater::default()), true, |_| {});
        deps.platform = "linux".to_string();
        deps.spawner = Some(Arc::new(FakeSpawner::default()));
        deps.create_install_log = Some(Arc::new(|| Err("disk full".to_string())));
        let updater = Updater::new(deps);

        let result = updater.install_via_script();

        assert_eq!(result.reason.as_deref(), Some("disk full"));
        assert_eq!(updater.state().status, UpdateStatus::Error);
    }

    #[test]
    fn says_so_when_the_log_cannot_be_handed_to_the_installer() {
        let s = running("linux");
        s.log.stdio_fails.store(true, Ordering::SeqCst);

        let result = s.setup.updater.install_via_script();

        assert_eq!(result.reason.as_deref(), Some("the install log is closed"));
        assert!(s.spawner.calls.lock().unwrap().is_empty());
        assert_eq!(s.log.closes(), 1);
    }

    #[test]
    fn a_poll_that_arrives_after_the_exit_changes_nothing() {
        let s = running("darwin");
        s.setup.updater.install_via_script();
        s.spawner.child.end(InstallerExit::Exited(Some(1)));
        let published = s.setup.published.lock().unwrap().len();

        s.log.write("Downloading x...\n");
        s.poll();

        assert_eq!(s.setup.published.lock().unwrap().len(), published);
        assert_eq!(s.setup.updater.state().status, UpdateStatus::Error);
    }

    /// A running install whose `on_status` calls back into the updater, as the
    /// app's own handler does. `hook` runs for every delivered status, with the
    /// updater, while the callback is on the stack.
    fn reentrant(hook: impl Fn(&Updater, &PublishedStatus) + Send + Sync + 'static) -> Spawn {
        let (s, _) = with_spawn("darwin", Some(FakeSpawner::default()), true);
        let cell: Arc<Mutex<Option<Updater>>> = Arc::new(Mutex::new(None));
        let (sink, handle) = (Arc::clone(&s.setup.published), Arc::clone(&cell));
        let mut deps = UpdaterDeps::new(s.setup.auto.clone(), true, move |p| {
            sink.lock().unwrap().push(p.clone());
            let updater = handle.lock().unwrap().clone();
            hook(
                &updater.expect("the updater is set before anything runs"),
                &p,
            );
        });
        deps.scheduler = s.setup.scheduler.clone();
        deps.logger = s.setup.logger.clone();
        deps.platform = "darwin".to_string();
        deps.spawner = Some(s.spawner.clone() as Arc<dyn InstallerSpawner>);
        let log = Arc::clone(&s.log);
        deps.create_install_log = Some(Arc::new(move || Ok(log.clone() as Arc<dyn InstallLog>)));
        let updater = Updater::new(deps);
        *cell.lock().unwrap() = Some(updater.clone());
        Spawn {
            setup: Setup { updater, ..s.setup },
            ..s
        }
    }

    #[test]
    fn on_status_can_call_back_into_the_updater_while_installing_and_at_the_exit() {
        let s = reentrant(|updater, _| {
            updater.state();
            updater.check_now();
            updater.stop();
        });
        s.setup.updater.install_via_script();

        s.log.write("Downloading x...\n");
        s.poll();
        s.log.write("AgentRQ 1.2.0 installed to /Applications\n");
        s.spawner.child.end(InstallerExit::Exited(Some(0)));
        s.log.write("Downloading y...\n");
        s.poll();

        assert_eq!(
            s.setup.statuses(),
            vec![
                UpdateStatus::Installing,
                UpdateStatus::Installing,
                UpdateStatus::Installed
            ]
        );
    }

    #[test]
    fn a_panic_in_on_status_does_not_silence_later_statuses() {
        let seen = Arc::new(Mutex::new(Vec::new()));
        let log = Arc::clone(&seen);
        let first = Arc::new(AtomicBool::new(true));
        let mut deps = UpdaterDeps::new(
            Arc::new(FakeAutoUpdater::default()),
            true,
            move |published: PublishedStatus| {
                if first.swap(false, Ordering::SeqCst) {
                    panic!("on_status failed");
                }
                log.lock().unwrap().push(published.status);
            },
        );
        deps.scheduler = Arc::new(FakeScheduler::default());
        let updater = Updater::new(deps);
        updater.start();

        let unwound = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            updater.handle_event(UpdaterEvent::Checking)
        }));
        assert!(unwound.is_err());
        updater.handle_event(UpdaterEvent::NotAvailable);

        assert_eq!(*seen.lock().unwrap(), vec![UpdateStatus::UpToDate]);
    }

    #[test]
    fn a_status_published_from_inside_on_status_is_delivered_after_it_returns() {
        let inside = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&inside);
        let s = reentrant(move |updater, published| {
            assert!(!flag.swap(true, Ordering::SeqCst), "callbacks never nest");
            if published.status == UpdateStatus::Installing {
                updater.publish(UpdateStatus::Available, "", "", None);
            }
            flag.store(false, Ordering::SeqCst);
        });

        s.setup.updater.install_via_script();

        assert_eq!(
            s.setup.statuses(),
            vec![UpdateStatus::Installing, UpdateStatus::Available]
        );
    }

    // ---- the real scheduler and spawner ----

    fn wait_for(condition: impl Fn() -> bool) {
        let met = (0..400).any(|_| {
            condition() || {
                std::thread::sleep(Duration::from_millis(10));
                false
            }
        });
        assert!(met, "timed out");
    }

    #[test]
    fn the_real_scheduler_ticks_until_it_is_cleared() {
        let scheduler = StdScheduler::default();
        let ticks = Arc::new(AtomicUsize::new(0));
        let counter = Arc::clone(&ticks);
        let handle = scheduler.set_interval(
            5,
            Arc::new(move || {
                counter.fetch_add(1, Ordering::SeqCst);
            }),
        );

        wait_for(|| ticks.load(Ordering::SeqCst) >= 2);
        scheduler.clear_interval(handle);
        std::thread::sleep(Duration::from_millis(50));
        let settled = ticks.load(Ordering::SeqCst);
        std::thread::sleep(Duration::from_millis(50));
        assert_eq!(ticks.load(Ordering::SeqCst), settled);
    }

    #[test]
    fn a_process_that_cannot_be_waited_for_is_a_failed_installer() {
        let failed = exit_of(Err(std::io::Error::other("wait failed")));
        assert_eq!(failed, InstallerExit::Failed("wait failed".to_string()));
    }

    mod real_process {
        use super::*;
        use crate::install_log::{create_install_log, TEST_SHELL};

        fn run(script: &str, stdio: Option<(Stdio, Stdio)>) -> InstallerExit {
            let (shell, flag) = TEST_SHELL;
            let mut child = StdInstallerSpawner
                .spawn(shell, &[flag.to_string(), script.to_string()], stdio)
                .unwrap();
            child.unref();
            let exit = Arc::new(Mutex::new(None));
            let slot = Arc::clone(&exit);
            child.on_exit(Arc::new(move |e| *slot.lock().unwrap() = Some(e)));
            wait_for(|| exit.lock().unwrap().is_some());
            let exit = exit.lock().unwrap().clone().unwrap();
            exit
        }

        #[test]
        fn reports_how_the_process_ended() {
            assert_eq!(run("exit 3", None), InstallerExit::Exited(Some(3)));
        }

        #[cfg(unix)]
        #[test]
        fn reports_a_process_a_signal_ended_as_having_no_code() {
            assert_eq!(run("kill -9 $$", None), InstallerExit::Exited(None));
        }

        #[cfg(unix)]
        #[test]
        fn starts_the_installer_in_its_own_process_group() {
            // The shell leads its own group, so it outlives us rather than
            // sharing our fate.
            let leads_its_group = r#"[ "$(ps -o pgid= -p $$ | tr -d ' ')" = "$$" ]"#;
            assert_eq!(run(leads_its_group, None), InstallerExit::Exited(Some(0)));
        }

        #[test]
        fn writes_to_the_log_it_was_given() {
            let dir = std::env::temp_dir().join(format!("agentrq-updater-{}", std::process::id()));
            std::fs::create_dir_all(&dir).unwrap();
            let log = create_install_log(&dir).unwrap();

            let exit = run("echo Downloading x...", Some(log.stdio().unwrap()));

            assert_eq!(exit, InstallerExit::Exited(Some(0)));
            // cmd ends its lines with CRLF.
            assert_eq!(log.read().replace("\r\n", "\n"), "Downloading x...\n");
            // Closed first: Windows will not remove a folder holding an open file.
            log.close();
            std::fs::remove_dir_all(&dir).unwrap();
        }

        #[test]
        fn says_when_there_is_no_shell_to_run() {
            #[cfg(windows)]
            const MISSING: &str = "cannot find";
            #[cfg(not(windows))]
            const MISSING: &str = "No such file";
            let result = StdInstallerSpawner.spawn("/nonexistent/sh", &[], None);
            assert!(result.err().unwrap().contains(MISSING));
        }
    }
}
