// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The extension host's process, and the shell's end of its wire.
//!
//! Extensions run in `agentrq-ext-host`, a Node process (`src/ext-host/`); this
//! starts it, talks to it (`core::ext_rpc`), answers what it asks for
//! ([`Services`]), and restarts it when it dies (`core::ext_host`). Nothing
//! here holds a lock across an await or while calling out.

use std::collections::HashMap;
use std::ffi::OsString;
use std::future::Future;
use std::path::PathBuf;
use std::pin::Pin;
use std::process::Stdio;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::{Duration, Instant};

use agentrq_desktop_core::ext_host::{
    answer_without_host, blame, crash_reason, enabled_names, retry_delay_ms, CrashLedger,
    IDLE_STOP_MS, INSTALLED_FILE, STABLE_MS,
};
use agentrq_desktop_core::ext_rpc::{
    self, deadline_ms, timed_out, Ids, Line, Lines, Message, HOST_GONE, METHOD_NOT_FOUND,
};
use serde_json::{json, Value};
#[cfg(target_os = "linux")]
use std::os::unix::process::CommandExt;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::sync::{mpsc, oneshot};

/// An answer the shell gives the host, later.
pub type Answer = Pin<Box<dyn Future<Output = Result<Value, String>> + Send>>;

/// What the host may ask of the shell, and what it tells it.
pub trait Services: Send + Sync {
    /// `profileFetch`, `vault`, `seal`, `open` and `authWindow`. Any other
    /// method is `None`: not served.
    fn serve(&self, method: &str, params: Value) -> Option<Answer>;
    /// An extension was turned off without anybody asking, and why.
    fn changed(&self, name: &str, reason: &str);
    /// The active profile's server, which the host's clients address.
    fn server_url(&self) -> String;
}

/// How the host is started: the Node binary, then its arguments. The shell
/// adds the list of extensions to turn off as the last one.
#[derive(Debug, Clone)]
pub struct Launch {
    pub program: PathBuf,
    pub args: Vec<OsString>,
}

/// The longest the shell waits on its own answer to the host, as the host
/// waits on the shell (`rpc.js`'s `DEADLINE_MS`).
const SERVE_DEADLINE: Duration = Duration::from_secs(120);
/// How long a host asked to stop gets before it is killed.
const STOP_GRACE: Duration = Duration::from_secs(5);

type Pending = Arc<Mutex<HashMap<u64, oneshot::Sender<Result<Value, String>>>>>;

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

fn is_allowed_env(key: &str) -> bool {
    let upper = key.to_uppercase();
    if upper.starts_with("NODE_") {
        return false;
    }
    matches!(
        upper.as_str(),
        "PATH"
            | "HOME"
            | "USERPROFILE"
            | "APPDATA"
            | "LOCALAPPDATA"
            | "TMP"
            | "TEMP"
            | "TMPDIR"
            | "LANG"
            | "HTTP_PROXY"
            | "HTTPS_PROXY"
            | "ALL_PROXY"
            | "NO_PROXY"
            | "SYSTEMROOT"
            | "COMSPEC"
            | "PATHEXT"
            | "WINDIR"
    ) || upper.starts_with("LC_")
}

fn truncate_str(s: &str, max_chars: usize) -> &str {
    match s.char_indices().nth(max_chars) {
        Some((idx, _)) => &s[..idx],
        None => s,
    }
}

/// The running process, as the shell holds it.
struct Process {
    generation: u64,
    pid: Option<u32>,
    out: mpsc::UnboundedSender<Vec<u8>>,
    pending: Pending,
    ids: Ids,
    /// The server last told to the host.
    context: Option<String>,
    kill: mpsc::Sender<()>,
}

#[cfg(target_os = "linux")]
fn set_pdeathsig() -> std::io::Result<()> {
    unsafe {
        libc::prctl(libc::PR_SET_PDEATHSIG, libc::SIGKILL);
    }
    Ok(())
}

#[derive(Default)]
struct State {
    process: Option<Process>,
    generation: u64,
    /// A generation the shell stopped on purpose: its exit is no crash.
    stopping: Option<u64>,
    /// Waiting out a backoff before a restart.
    restarting: bool,
    ledger: CrashLedger,
    loading: Option<String>,
    running: Vec<String>,
    attempt: u32,
    started: Option<Instant>,
    /// Turned off for crashing, to tell the next host before it loads anything.
    disable: Vec<Value>,
    /// Bumped on every call, so an idle stop knows nobody asked since.
    activity: u64,
    /// Number of consecutive startup failures before ready.
    startup_failures: u32,
}

pub struct ExtHost {
    launch: Launch,
    data_dir: PathBuf,
    services: Arc<dyn Services>,
    state: Mutex<State>,
    timing: Timing,
}

/// The periods the host runs to, which a test shortens.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct Timing {
    /// How long a host nothing needs outlives its last call.
    idle: Duration,
    /// The backoff's delays are divided by this.
    retry_divisor: u64,
    /// The longest the shell waits on its own answer to the host.
    serve: Duration,
    /// How long a host must have run for its next crash to start the backoff
    /// again.
    stable: Duration,
}

impl Default for Timing {
    fn default() -> Self {
        Self {
            idle: Duration::from_millis(IDLE_STOP_MS),
            retry_divisor: 1,
            serve: SERVE_DEADLINE,
            stable: Duration::from_millis(STABLE_MS),
        }
    }
}

impl ExtHost {
    pub fn new(launch: Launch, data_dir: PathBuf, services: Arc<dyn Services>) -> Arc<Self> {
        Self::tuned(launch, data_dir, services, Timing::default())
    }

    fn tuned(
        launch: Launch,
        data_dir: PathBuf,
        services: Arc<dyn Services>,
        timing: Timing,
    ) -> Arc<Self> {
        Arc::new(Self {
            launch,
            data_dir,
            services,
            state: Mutex::default(),
            timing,
        })
    }

    /// The extensions installed and enabled, read from what `install.js` wrote.
    fn wanted(&self) -> Vec<String> {
        let installed = std::fs::read_to_string(self.data_dir.join(INSTALLED_FILE));
        enabled_names(&installed.unwrap_or_default())
    }

    /// At launch: start the host only if an extension wants it.
    pub fn boot(self: &Arc<Self>) -> bool {
        if self.wanted().is_empty() {
            return false;
        }
        let mut state = lock(&self.state);
        state.process.is_some() || self.spawn(&mut state).is_ok()
    }

    pub fn is_running(&self) -> bool {
        lock(&self.state).process.is_some()
    }

    /// Ask the host `method`. A call a page makes on every render never starts
    /// it: with nothing enabled it is answered here.
    pub async fn call(self: &Arc<Self>, method: &str, params: Value) -> Result<Value, String> {
        let (answer, activity, pending, id, generation) = {
            let mut state = lock(&self.state);
            state.activity += 1;
            if state.process.is_none() {
                if let Some(answer) = answer_without_host(method, &params) {
                    if state.restarting || self.wanted().is_empty() {
                        return Ok(answer);
                    }
                }
                if state.restarting {
                    return Err("The extension host is restarting. Try again in a moment.".into());
                }
                self.spawn(&mut state)?;
            }
            let server = self.services.server_url();
            let process = state.process.as_mut().expect("spawned above");
            if process.context.as_deref() != Some(server.as_str()) {
                let _ = process.out.send(ext_rpc::notification(
                    "context",
                    json!({ "serverUrl": server }),
                ));
                process.context = Some(server);
            }
            let id = process.ids.allocate();
            let (tx, rx) = oneshot::channel();
            lock(&process.pending).insert(id, tx);
            let _ = process.out.send(ext_rpc::request(id, method, params));
            let pending = Arc::clone(&process.pending);
            let generation = state.generation;
            (rx, state.activity, pending, id, generation)
        };
        let deadline = Duration::from_millis(deadline_ms(method));
        let result = match tokio::time::timeout(deadline, answer).await {
            Ok(Ok(result)) => result,
            Ok(Err(_)) => Err(HOST_GONE.to_string()),
            Err(_) => {
                lock(&pending).remove(&id);
                self.kill_generation(generation);
                Err(timed_out(method))
            }
        };
        self.stop_when_idle(activity);
        result
    }

    fn kill_generation(&self, generation: u64) {
        let state = lock(&self.state);
        if state.generation == generation && state.stopping != Some(generation) {
            if let Some(process) = &state.process {
                let _ = process.kill.try_send(());
            }
        }
    }

    /// Synchronously kill any running child so it never outlives the app.
    pub fn kill_sync(&self) {
        let mut state = lock(&self.state);
        if let Some(process) = state.process.take() {
            state.stopping = Some(process.generation);
            fail_all(&process.pending);
            if let Some(pid) = process.pid {
                #[cfg(unix)]
                unsafe {
                    libc::kill(pid as i32, libc::SIGKILL);
                }
                #[cfg(windows)]
                unsafe {
                    use windows_sys::Win32::System::Threading::{
                        OpenProcess, TerminateProcess, PROCESS_TERMINATE,
                    };
                    let handle = OpenProcess(PROCESS_TERMINATE, 0, pid);
                    if !handle.is_null() {
                        TerminateProcess(handle, 1);
                        windows_sys::Win32::Foundation::CloseHandle(handle);
                    }
                }
            }
        }
    }

    /// Turned back on by the person: its crashes are forgiven.
    pub fn forgive(&self, name: &str) {
        lock(&self.state).ledger.forgive(name);
    }

    /// Stop the host, for good or until the next call.
    pub fn stop(&self) {
        let mut state = lock(&self.state);
        if let Some(process) = state.process.take() {
            state.stopping = Some(process.generation);
            fail_all(&process.pending);
            let kill = process.kill;
            tokio::spawn(async move {
                tokio::time::sleep(STOP_GRACE).await;
                let _ = kill.send(()).await;
            });
        }
    }

    /// Once a minute has passed with no call, a host nothing needs is stopped.
    fn stop_when_idle(self: &Arc<Self>, activity: u64) {
        let this = Arc::clone(self);
        tokio::spawn(async move {
            tokio::time::sleep(this.timing.idle).await;
            let idle = lock(&this.state).activity == activity;
            if idle && this.wanted().is_empty() {
                this.stop();
            }
        });
    }

    fn spawn(self: &Arc<Self>, state: &mut State) -> Result<(), String> {
        let disable = Value::Array(state.disable.clone()).to_string();
        let mut cmd = tokio::process::Command::new(&self.launch.program);
        cmd.args(&self.launch.args)
            .arg(disable)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);

        cmd.env_clear();
        for (k, v) in std::env::vars() {
            if is_allowed_env(&k) {
                cmd.env(k, v);
            }
        }

        #[cfg(target_os = "linux")]
        unsafe {
            cmd.as_std_mut().pre_exec(set_pdeathsig);
        }

        let mut child = cmd.spawn().map_err(|error| {
            // Logged here: a restart has nobody to tell.
            let reason = format!("The extension host could not start: {error}");
            eprintln!("[ext-host] {reason}");
            reason
        })?;
        let pid = child.id();
        state.disable.clear();
        state.generation += 1;
        state.loading = None;
        state.running.clear();
        state.started = Some(Instant::now());
        let generation = state.generation;
        let (out, mut outgoing) = mpsc::unbounded_channel::<Vec<u8>>();
        let pending = Pending::default();
        let (kill, killed) = mpsc::channel::<()>(1);

        let mut stdin = child.stdin.take().expect("piped");
        tokio::spawn(async move {
            while let Some(bytes) = outgoing.recv().await {
                if stdin.write_all(&bytes).await.is_err() {
                    break;
                }
            }
        });

        let mut stderr = child.stderr.take().expect("piped");
        tokio::spawn(async move {
            let mut lines = Lines::with_max(64 * 1024);
            let mut buffer = vec![0; 64 * 1024];
            while let Ok(read @ 1..) = stderr.read(&mut buffer).await {
                for line in lines.push(&buffer[..read]) {
                    match line {
                        Line::Whole(line) => {
                            let text = String::from_utf8_lossy(&line);
                            eprintln!("[ext-host] {text}");
                        }
                        Line::TooLong => {
                            eprintln!("[ext-host] [stderr line over 64 KiB truncated]");
                        }
                    }
                }
            }
        });

        let stdout = child.stdout.take().expect("piped");
        tokio::spawn(Arc::clone(self).read(stdout, generation, out.downgrade(), pending.clone()));
        tokio::spawn(Arc::clone(self).wait(child, generation, killed));

        state.process = Some(Process {
            generation,
            pid,
            out,
            pending,
            ids: Ids::default(),
            context: None,
            kill,
        });
        Ok(())
    }

    async fn read(
        self: Arc<Self>,
        mut stdout: tokio::process::ChildStdout,
        generation: u64,
        out: mpsc::WeakUnboundedSender<Vec<u8>>,
        pending: Pending,
    ) {
        let mut lines = Lines::default();
        let mut buffer = vec![0; 64 * 1024];
        while let Ok(read @ 1..) = stdout.read(&mut buffer).await {
            for line in lines.push(&buffer[..read]) {
                match line {
                    Line::Whole(line) => self.dispatch(&line, generation, &out, &pending),
                    Line::TooLong => eprintln!("[ext-host] a line over the limit was dropped"),
                }
            }
        }
    }

    fn dispatch(
        &self,
        line: &[u8],
        generation: u64,
        out: &mpsc::WeakUnboundedSender<Vec<u8>>,
        pending: &Pending,
    ) {
        match ext_rpc::parse(line) {
            Message::Response { id, result } => {
                if let Some(waiting) = lock(pending).remove(&id) {
                    let _ = waiting.send(result);
                }
            }
            Message::Request { id, method, params } => {
                let Some(answer) = self.services.serve(&method, params) else {
                    let refused = format!("The app does not answer \"{method}\".");
                    if let Some(out) = out.upgrade() {
                        let _ = out.send(ext_rpc::error_response(id, METHOD_NOT_FOUND, &refused));
                    }
                    return;
                };
                let out = out.clone();
                let deadline = self.timing.serve;
                tokio::spawn(async move {
                    let result = tokio::time::timeout(deadline, answer)
                        .await
                        .unwrap_or_else(|_| Err(timed_out(&method)));
                    if let Some(out) = out.upgrade() {
                        let _ = out.send(ext_rpc::response(id, result));
                    }
                });
            }
            Message::Notification { method, params } => self.told(&method, &params, generation),
            Message::Stray(text) => {
                eprintln!("[ext-host] not a message: {}", truncate_str(&text, 200))
            }
        }
    }

    fn told(&self, method: &str, params: &Value, generation: u64) {
        let names = |value: &Value| -> Vec<String> {
            value
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|name| name.as_str().map(str::to_string))
                .collect()
        };
        match method {
            "changed" => self.services.changed(
                params["name"].as_str().unwrap_or_default(),
                params["reason"].as_str().unwrap_or_default(),
            ),
            "loading" | "running" => {
                let mut state = lock(&self.state);
                if state.generation != generation {
                    return;
                }
                state.startup_failures = 0;
                if method == "loading" {
                    state.loading = params["name"].as_str().map(str::to_string);
                } else {
                    state.running = names(&params["names"]);
                }
            }
            _ => {}
        }
    }

    async fn wait(
        self: Arc<Self>,
        mut child: tokio::process::Child,
        generation: u64,
        mut killed: mpsc::Receiver<()>,
    ) {
        let code = tokio::select! {
            status = child.wait() => status.ok().and_then(|status| status.code()),
            _ = killed.recv() => {
                let _ = child.kill().await;
                None
            }
        };
        let delay = {
            let mut state = lock(&self.state);
            if state.process.as_ref().map(|p| p.generation) == Some(generation) {
                let process = state.process.take().expect("checked");
                fail_all(&process.pending);
            }
            if state.stopping == Some(generation) || state.generation != generation {
                return;
            }
            let blamed = blame(state.loading.as_deref(), &state.running);
            if blamed.is_empty() {
                state.startup_failures += 1;
                if state.startup_failures >= 3 {
                    eprintln!(
                        "[ext-host] The extension host failed to start {} times in a row; giving up.",
                        state.startup_failures
                    );
                    return;
                }
            } else {
                state.startup_failures = 0;
            }
            for name in state.ledger.record(&blamed) {
                let reason = crash_reason(code);
                state
                    .disable
                    .push(json!({ "name": name, "reason": reason }));
            }
            let stable = state
                .started
                .is_some_and(|started| started.elapsed() >= self.timing.stable);
            if stable {
                state.attempt = 0;
            }
            let delay = retry_delay_ms(state.attempt) / self.timing.retry_divisor;
            state.attempt += 1;
            state.restarting = true;
            delay
        };
        tokio::time::sleep(Duration::from_millis(delay)).await;
        let mut state = lock(&self.state);
        state.restarting = false;
        let to_disable = !state.disable.is_empty();
        if state.process.is_none() && (to_disable || !self.wanted().is_empty()) {
            let _ = self.spawn(&mut state);
        }
    }
}

fn fail_all(pending: &Pending) {
    for (_, waiting) in lock(pending).drain() {
        let _ = waiting.send(Err(HOST_GONE.to_string()));
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use std::path::Path;
    use std::sync::atomic::{AtomicUsize, Ordering};

    /// The real host, from the source tree, run by the `node` on PATH.
    pub fn real_launch(data_dir: &Path) -> Launch {
        let entry = Path::new(env!("CARGO_MANIFEST_DIR")).join("../src/ext-host/main.js");
        Launch {
            program: "node".into(),
            args: vec![entry.into(), data_dir.into(), "0.13.4".into()],
        }
    }

    /// A stand-in host: `script` run by node, its arguments after it.
    pub fn script_launch(script: &str) -> Launch {
        Launch {
            program: "node".into(),
            args: vec!["-e".into(), script.into(), "--".into()],
        }
    }

    #[derive(Default)]
    pub struct FakeServices {
        pub changed: Mutex<Vec<(String, String)>>,
        pub served: AtomicUsize,
        pub server: Mutex<String>,
    }

    impl Services for FakeServices {
        fn serve(&self, method: &str, params: Value) -> Option<Answer> {
            self.served.fetch_add(1, Ordering::SeqCst);
            let answer = match method {
                "vault" => Ok(json!({ "available": true })),
                "seal" => Ok(json!({ "ok": true, "sealed": params["texts"] })),
                "open" => Ok(json!({ "opened": params["blobs"] })),
                "slow" => return Some(Box::pin(std::future::pending())),
                "refuse" => Err("refused".to_string()),
                _ => return None,
            };
            Some(Box::pin(async move { answer }))
        }
        fn changed(&self, name: &str, reason: &str) {
            lock(&self.changed).push((name.into(), reason.into()));
        }
        fn server_url(&self) -> String {
            lock(&self.server).clone()
        }
    }

    const FAST: Timing = Timing {
        idle: Duration::from_millis(200),
        retry_divisor: 100,
        serve: Duration::from_millis(200),
        stable: Duration::from_secs(60),
    };

    fn host(launch: Launch, dir: &Path, services: Arc<FakeServices>) -> Arc<ExtHost> {
        ExtHost::tuned(launch, dir.into(), services, FAST)
    }

    fn install(dir: &Path, json: &str) {
        std::fs::write(dir.join(INSTALLED_FILE), json).unwrap();
    }

    async fn until(what: impl Fn() -> bool) {
        let waited = tokio::time::timeout(Duration::from_secs(10), async {
            while !what() {
                tokio::time::sleep(Duration::from_millis(25)).await;
            }
        });
        waited.await.expect("never happened");
    }

    #[tokio::test]
    async fn answers_a_page_without_starting_anything() {
        let dir = tempfile::tempdir().unwrap();
        let host = ExtHost::new(
            script_launch("process.exit(9)"),
            dir.path().into(),
            Arc::new(FakeServices::default()),
        );
        assert!(!host.boot());
        assert_eq!(host.call("entries", json!({})).await, Ok(json!([])));
        assert_eq!(
            host.call("drawerFor", json!({"format": "x"})).await,
            Ok(json!({"ok": false, "reason": "Nothing installed draws \"x\"."}))
        );
        assert!(!host.is_running());
    }

    #[tokio::test]
    async fn drives_the_real_host_and_stops_it_when_idle() {
        let dir = tempfile::tempdir().unwrap();
        let services = Arc::new(FakeServices::default());
        *lock(&services.server) = "http://127.0.0.1:9".into();
        let host = host(real_launch(dir.path()), dir.path(), services.clone());

        let examples = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../examples/extensions");
        let installed = host
            .call(
                "installLocal",
                json!({ "path": examples.join("task-stats") }),
            )
            .await
            .unwrap();
        assert_eq!(installed["ok"], true, "{installed}");
        let state = host.call("state", json!({})).await.unwrap();
        assert_eq!(state["installed"][0]["name"], "task-stats");
        until(|| lock(&host.state).running == vec!["task-stats".to_string()]).await;
        assert!(
            services.served.load(Ordering::SeqCst) > 0,
            "the vault was asked"
        );
        let entries = host
            .call("entries", json!({"surface": "task-menu", "context": {}}))
            .await
            .unwrap();
        assert_eq!(entries[0]["owner"], "task-stats");

        // Enabled, so it stays.
        tokio::time::sleep(Duration::from_millis(400)).await;
        assert!(host.is_running());
        assert!(host.boot(), "boots when something is enabled");

        let removed = host
            .call("uninstall", json!({"name": "task-stats"}))
            .await
            .unwrap();
        assert_eq!(removed["ok"], true);
        until(|| !host.is_running()).await;
        assert_eq!(host.call("panelPages", json!({})).await, Ok(json!([])));
    }

    #[tokio::test]
    async fn refuses_what_it_does_not_serve_and_answers_the_rest() {
        let dir = tempfile::tempdir().unwrap();
        let services = Arc::new(FakeServices::default());
        // A host that asks the shell three things and reports the answers.
        let script = r#"
            const out = (m) => process.stdout.write(JSON.stringify({jsonrpc: '2.0', ...m}) + '\n');
            const answers = {};
            let buf = '';
            const report = (id) =>
                Object.keys(answers).length === 4 ? out({ id, result: answers }) : setTimeout(() => report(id), 10);
            process.stdin.on('data', (c) => {
                buf += c;
                let i;
                while ((i = buf.indexOf('\n')) >= 0) {
                    const m = JSON.parse(buf.slice(0, i)); buf = buf.slice(i + 1);
                    if (m.method === 'report') report(m.id);
                    else if (m.id !== undefined && !m.method) answers[m.id] = m.error ? m.error.message : m.result;
                }
            });
            out({ id: 1, method: 'seal', params: { texts: ['a'] } });
            out({ id: 2, method: 'nope', params: {} });
            out({ id: 3, method: 'refuse', params: {} });
            out({ id: 4, method: 'slow', params: {} });
            out({ id: 99, result: 'answers nothing asked' });
            out({ method: 'changed', params: { name: 'x', reason: 'why' } });
            out({ method: 'other', params: {} });
            out({ method: 'loading', params: {} });
            out({ method: 'running', params: { names: ['a', 7] } });
            process.stdout.write('stray line\n' + 'x'.repeat(41 * 1024 * 1024) + '\n');
            process.stderr.write('a log line\n');
            process.stderr.write('e'.repeat(70 * 1024) + '\n');
        "#;
        let host = host(script_launch(script), dir.path(), services.clone());
        let handle = Arc::clone(&host);
        let report = async move {
            until(|| lock(&handle.state).running == vec!["a".to_string()]).await;
            handle.call("report", json!({})).await
        };
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        assert!(host.boot());
        let answers = report.await.unwrap();
        assert_eq!(answers["1"], json!({"ok": true, "sealed": ["a"]}));
        assert_eq!(answers["2"], "The app does not answer \"nope\".");
        assert_eq!(answers["3"], "refused");
        assert_eq!(
            answers["4"],
            "The extension host did not answer \"slow\" in time."
        );
        assert_eq!(
            *lock(&services.changed),
            vec![("x".to_string(), "why".to_string())]
        );
        host.stop();
        host.stop();
    }

    #[tokio::test]
    async fn gives_up_on_an_answer_that_does_not_come() {
        let dir = tempfile::tempdir().unwrap();
        let host = host(
            script_launch("process.stdin.resume()"),
            dir.path(),
            Arc::default(),
        );
        assert_eq!(
            host.call("supervisor", json!({})).await,
            Err("The extension host did not answer \"supervisor\" in time.".into())
        );
        host.stop();
    }

    #[tokio::test]
    async fn a_host_that_will_not_start_says_why() {
        let dir = tempfile::tempdir().unwrap();
        let missing = Launch {
            program: dir.path().join("no-such-node"),
            args: vec![],
        };
        let host = host(missing, dir.path(), Arc::default());
        let error = host.call("state", json!({})).await.unwrap_err();
        assert!(
            error.starts_with("The extension host could not start: "),
            "{error}"
        );
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        assert!(!host.boot());
    }

    #[tokio::test]
    async fn a_crash_while_loading_counts_against_that_extension_and_the_third_disables_it() {
        let dir = tempfile::tempdir().unwrap();
        install(
            dir.path(),
            r#"{"installations":[{"name":"bad"},{"name":"good"}]}"#,
        );
        // Reports which extensions it was told to disable (its last argument),
        // then crashes while "loading" bad, unless it was told to disable it.
        let script = r#"
            const disable = JSON.parse(process.argv.at(-1));
            const out = (m) => process.stdout.write(JSON.stringify({jsonrpc: '2.0', ...m}) + '\n');
            out({ method: 'running', params: { names: ['good'] } });
            if (disable.length) {
                for (const d of disable) out({ method: 'changed', params: d });
                process.stdin.resume();
            } else {
                out({ method: 'loading', params: { name: 'bad' } });
                setTimeout(() => process.exit(3), 20);
            }
        "#;
        let services = Arc::new(FakeServices::default());
        let host = host(script_launch(script), dir.path(), services.clone());
        assert!(host.boot());
        until(|| !lock(&services.changed).is_empty()).await;
        assert_eq!(
            *lock(&services.changed),
            vec![(
                "bad".to_string(),
                "The extension host stopped (exit code 3) three times while it ran.".to_string()
            )]
        );
        let state = lock(&host.state);
        assert_eq!(state.ledger.count("good"), 0, "never blamed");
        assert_eq!(state.attempt, 3);
        drop(state);
        host.forgive("bad");
        host.stop();
    }

    #[tokio::test]
    async fn a_crash_while_running_counts_against_everything_running_and_restarts() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let marker = dir.path().join("started-once");
        let script = format!(
            r#"
            const fs = require('fs');
            const marker = {marker:?};
            const out = (m) => process.stdout.write(JSON.stringify({{jsonrpc: '2.0', ...m}}) + '\n');
            out({{ method: 'running', params: {{ names: ['a'] }} }});
            if (fs.existsSync(marker)) process.stdin.resume();
            else {{ fs.writeFileSync(marker, ''); setTimeout(() => process.kill(process.pid, 'SIGKILL'), 20); }}
            "#,
            marker = marker.to_string_lossy()
        );
        let host = host(script_launch(&script), dir.path(), Arc::default());
        assert!(host.boot());
        until(|| lock(&host.state).ledger.count("a") == 1 && host.is_running()).await;
        // Restarted and running again.
        let calls = Arc::clone(&host);
        assert!(!lock(&calls.state).restarting);
        host.stop();
    }

    #[tokio::test]
    async fn a_host_that_ran_a_while_starts_its_backoff_again() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let host = ExtHost::tuned(
            script_launch("setTimeout(() => process.exit(1), 150)"),
            dir.path().into(),
            Arc::new(FakeServices::default()),
            Timing {
                stable: Duration::from_millis(50),
                ..FAST
            },
        );
        assert!(host.boot());
        lock(&host.state).attempt = 5;
        until(|| lock(&host.state).attempt != 5).await;
        assert_eq!(lock(&host.state).attempt, 1);
        host.stop();
    }

    #[tokio::test]
    async fn a_page_call_while_restarting_is_answered_and_a_management_call_waits() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let host = ExtHost::tuned(
            script_launch("setTimeout(() => process.exit(1), 10)"),
            dir.path().into(),
            Arc::new(FakeServices::default()),
            Timing {
                retry_divisor: 1,
                ..FAST
            },
        );
        assert!(host.boot());
        until(|| lock(&host.state).restarting).await;
        assert_eq!(host.call("entries", json!({})).await, Ok(json!([])));
        assert_eq!(
            host.call("state", json!({})).await,
            Err("The extension host is restarting. Try again in a moment.".into())
        );
    }

    #[tokio::test]
    async fn a_stopped_host_is_no_crash_and_a_stubborn_one_is_killed() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        // Ignores its stdin closing.
        let host = host(
            script_launch("setInterval(() => {}, 1000)"),
            dir.path(),
            Arc::default(),
        );
        assert!(host.boot());
        let waiting = {
            let mut state = lock(&host.state);
            let process = state.process.as_mut().unwrap();
            let (tx, rx) = oneshot::channel();
            lock(&process.pending).insert(99, tx);
            rx
        };
        host.stop();
        assert_eq!(waiting.await.unwrap(), Err(HOST_GONE.to_string()));
        tokio::time::sleep(STOP_GRACE + Duration::from_millis(500)).await;
        let state = lock(&host.state);
        assert!(state.process.is_none());
        assert_eq!(state.ledger.count("a"), 0, "a stop is no crash");
        assert!(!state.restarting);
    }

    #[tokio::test]
    async fn a_host_gone_mid_call_fails_the_call() {
        let dir = tempfile::tempdir().unwrap();
        let host = host(
            script_launch("process.stdin.once('data', () => process.exit(0))"),
            dir.path(),
            Arc::default(),
        );
        assert_eq!(
            host.call("state", json!({})).await,
            Err(HOST_GONE.to_string())
        );
    }

    #[tokio::test]
    async fn stop_closes_stdin_cleanly() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let marker = dir.path().join("stdin-closed");
        let script = format!(
            r#"
            const fs = require('fs');
            process.stdin.resume();
            process.stdin.on('end', () => {{
                fs.writeFileSync({marker:?}, 'closed');
                process.exit(0);
            }});
            "#,
            marker = marker.to_string_lossy()
        );
        let host = host(script_launch(&script), dir.path(), Arc::default());
        assert!(host.boot());
        until(|| host.is_running()).await;
        host.stop();
        until(|| marker.exists()).await;
        assert_eq!(std::fs::read_to_string(&marker).unwrap(), "closed");
    }

    #[tokio::test]
    async fn timed_out_call_leaves_pending_and_kills_hung_generation() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"hung"}]}"#);
        let script = r#"
            const out = (m) => process.stdout.write(JSON.stringify({jsonrpc: '2.0', ...m}) + '\n');
            out({ method: 'running', params: { names: ['hung'] } });
            process.stdin.resume();
        "#;
        let host = host(script_launch(script), dir.path(), Arc::default());
        assert!(host.boot());
        until(|| lock(&host.state).running == vec!["hung".to_string()]).await;

        let pending = {
            let state = lock(&host.state);
            Arc::clone(&state.process.as_ref().unwrap().pending)
        };
        let res = host.call("state", json!({})).await;
        assert_eq!(
            res,
            Err("The extension host did not answer \"state\" in time.".into())
        );

        assert!(
            lock(&pending).is_empty(),
            "pending must be cleaned up on timeout"
        );

        until(|| lock(&host.state).ledger.count("hung") >= 1).await;
        host.kill_generation(999);
        host.stop();
    }

    #[tokio::test]
    async fn kill_sync_terminates_child_synchronously() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let host = host(
            script_launch("const out = (m) => process.stdout.write(JSON.stringify({jsonrpc: '2.0', ...m}) + '\\n'); out({ method: 'running', params: { names: ['a'] } }); setInterval(() => {}, 1000)"),
            dir.path(),
            Arc::default(),
        );
        assert!(host.boot());
        until(|| host.is_running()).await;
        host.kill_sync();
        assert!(!host.is_running());
        let state = lock(&host.state);
        assert!(state.process.is_none());
        drop(state);
        host.kill_sync();
    }

    #[tokio::test]
    async fn stdin_writer_stops_on_broken_pipe() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let host = host(script_launch("process.exit(0)"), dir.path(), Arc::default());
        let out = {
            let mut state = lock(&host.state);
            host.spawn(&mut state).unwrap();
            state.process.as_ref().unwrap().out.clone()
        };
        for _ in 0..100 {
            if out.is_closed() {
                break;
            }
            let _ = out.send(vec![b'x'; 65536]);
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(out.is_closed());
    }

    #[test]
    fn kill_sync_with_none_pid() {
        let dir = tempfile::tempdir().unwrap();
        let host = host(script_launch(""), dir.path(), Arc::default());
        {
            let mut state = lock(&host.state);
            state.process = Some(Process {
                generation: 1,
                pid: None,
                out: mpsc::unbounded_channel().0,
                pending: Pending::default(),
                ids: Ids::default(),
                context: None,
                kill: mpsc::channel(1).0,
            });
        }
        host.kill_sync();
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn pdeathsig_can_be_set() {
        assert!(set_pdeathsig().is_ok());
    }

    #[tokio::test]
    async fn host_environment_is_clean_of_node_options() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let script = r#"
            const out = (m) => process.stdout.write(JSON.stringify({jsonrpc: '2.0', ...m}) + '\n');
            let buf = '';
            process.stdin.on('data', (c) => {
                buf += c;
                let i;
                while ((i = buf.indexOf('\n')) >= 0) {
                    const m = JSON.parse(buf.slice(0, i)); buf = buf.slice(i + 1);
                    if (m.method === 'checkEnv') {
                        out({ id: m.id, result: { hasNodeOptions: 'NODE_OPTIONS' in process.env } });
                    }
                }
            });
            out({ method: 'running', params: { names: ['a'] } });
        "#;
        std::env::set_var("NODE_OPTIONS", "--max-old-space-size=4096");
        let host = host(script_launch(script), dir.path(), Arc::default());
        assert!(host.boot());
        until(|| lock(&host.state).running == vec!["a".to_string()]).await;
        let res = host.call("checkEnv", json!({})).await.unwrap();
        assert_eq!(res["hasNodeOptions"], false);
        std::env::remove_var("NODE_OPTIONS");
        host.stop();
    }

    #[tokio::test]
    async fn host_stops_retrying_after_three_startup_failures() {
        let dir = tempfile::tempdir().unwrap();
        install(dir.path(), r#"{"installations":[{"name":"a"}]}"#);
        let host = host(script_launch("process.exit(1)"), dir.path(), Arc::default());
        assert!(host.boot());
        until(|| lock(&host.state).startup_failures >= 3).await;
        tokio::time::sleep(Duration::from_millis(150)).await;
        let state = lock(&host.state);
        assert_eq!(state.startup_failures, 3);
        assert!(state.process.is_none());
        assert!(!state.restarting);
    }

    #[test]
    fn truncate_str_limits_length() {
        assert_eq!(truncate_str("hello", 10), "hello");
        assert_eq!(truncate_str("hello world", 5), "hello");
        assert_eq!(truncate_str(&"a".repeat(300), 200), "a".repeat(200));
    }

    #[test]
    fn is_allowed_env_filters_correctly() {
        assert!(is_allowed_env("PATH"));
        assert!(is_allowed_env("HOME"));
        assert!(is_allowed_env("TMPDIR"));
        assert!(is_allowed_env("LANG"));
        assert!(is_allowed_env("HTTP_PROXY"));
        assert!(!is_allowed_env("NODE_OPTIONS"));
        assert!(!is_allowed_env("NODE_PATH"));
        assert!(!is_allowed_env("SECRET_KEY"));
    }

    #[tokio::test]
    async fn a_closed_answer_is_the_host_gone() {
        let dir = tempfile::tempdir().unwrap();
        let host = host(
            script_launch("process.stdin.resume()"),
            dir.path(),
            Arc::default(),
        );
        let caller = Arc::clone(&host);
        let call = tokio::spawn(async move { caller.call("state", json!({})).await });
        until(|| {
            lock(&host.state)
                .process
                .as_ref()
                .is_some_and(|p| !lock(&p.pending).is_empty())
        })
        .await;
        // Dropped without an answer.
        lock(lock(&host.state).process.as_ref().unwrap().pending.as_ref()).clear();
        assert_eq!(call.await.unwrap(), Err(HOST_GONE.to_string()));
        host.stop();
    }

    #[tokio::test]
    async fn the_server_is_told_once_and_again_when_it_changes() {
        let dir = tempfile::tempdir().unwrap();
        let services = Arc::new(FakeServices::default());
        // Answers every request with the last context it was told.
        let script = r#"
            let server = null, buf = '';
            process.stdin.on('data', (c) => {
                buf += c;
                let i;
                while ((i = buf.indexOf('\n')) >= 0) {
                    const m = JSON.parse(buf.slice(0, i)); buf = buf.slice(i + 1);
                    if (m.method === 'context') server = m.params.serverUrl;
                    else process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: m.id, result: server }) + '\n');
                }
            });
        "#;
        let host = host(script_launch(script), dir.path(), services.clone());
        *lock(&services.server) = "http://a".into();
        assert_eq!(host.call("x", json!({})).await, Ok(json!("http://a")));
        assert_eq!(host.call("x", json!({})).await, Ok(json!("http://a")));
        *lock(&services.server) = "http://b".into();
        assert_eq!(host.call("x", json!({})).await, Ok(json!("http://b")));
        // Notifications from an old generation are ignored.
        host.told("loading", &json!({"name": "z"}), 0);
        assert_eq!(lock(&host.state).loading, None);
        host.stop();
    }

    #[tokio::test]
    async fn new_uses_the_real_periods() {
        let dir = tempfile::tempdir().unwrap();
        let host = ExtHost::new(
            script_launch(""),
            dir.path().into(),
            Arc::new(FakeServices::default()),
        );
        assert_eq!(
            host.timing,
            Timing {
                idle: Duration::from_millis(IDLE_STOP_MS),
                retry_divisor: 1,
                serve: SERVE_DEADLINE,
                stable: Duration::from_millis(STABLE_MS),
            }
        );
    }
}
