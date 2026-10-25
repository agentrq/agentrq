// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Profiles, the connection screen, and who each profile is signed in as: what
//! `desktop/src/main/index.js` does around `profileState`.
//!
//! The rules are core's (`server_config`, `profiles`, `identity`); this module
//! keeps the state, talks to each profile's own client in [`Network`], and asks
//! the [`Shell`] for what only a window can do. Only names and servers cross
//! the bridge, never a jar or a cookie.

use std::collections::HashMap;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use agentrq_desktop_core::identity::{
    fetch_profile_identity, Account, ThreadDeadline, UserFetch, UserReply, IDENTITY_TIMEOUT_MS,
};
use agentrq_desktop_core::profiles::{
    can_discard_active_profile, duplicate_of, orphaned_data_stores, orphaned_profile_dirs, Profile,
};
use agentrq_desktop_core::server_config::{
    normalize_server_url, validate_server_url, ConfigFile, ConfigProbe, ProbeResponse, SaveError,
    ServerConfig, ServerConfigStore, CONFIG_FILENAME, CONFIG_PROBE_PATH, CONNECTION_LOCKED_REASON,
};
use agentrq_desktop_core::side_panel::permission_store::PermissionStore;
use serde::Serialize;
use serde_json::{json, Value};

use crate::network::{profile_dir, Network, ProfileNet};
use crate::sign_in::{self, CookieWebview, COOKIE_DEADLINE};
use crate::vault;

/// How long the connection screen's probe may take, start to body. Every
/// await needs a deadline (`docs/agents/desktop.md`).
pub const PROBE_DEADLINE: Duration = Duration::from_secs(10);

/// `agentrq-desktop.json`, as Electron keeps it.
pub struct ConfigOnDisk(pub PathBuf);

impl ConfigFile for ConfigOnDisk {
    fn read_file(&self) -> io::Result<String> {
        std::fs::read_to_string(&self.0)
    }
    fn write_file(&self, contents: &str) -> io::Result<()> {
        vault::write_private(&self.0, contents.as_bytes())
    }
}

fn store(base: &Path) -> ServerConfigStore<ConfigOnDisk> {
    ServerConfigStore::new(ConfigOnDisk(base.join(CONFIG_FILENAME)))
}

/// The server a profile uses: `AGENTRQ_SERVER_URL` when that pins it.
fn server_of(profile: &Profile, lock: Option<&str>) -> String {
    lock.map_or_else(|| profile.server_url.clone(), str::to_string)
}

/// The profile to start on, and its server.
pub fn first_profile(base: &Path, lock: Option<&str>) -> (String, String) {
    let active = store(base).active();
    let server = server_of(&active, lock);
    (active.id, server)
}

/// Where a profile's webviews keep their storage on Linux and Windows, beside
/// its cookie jar and attachments, so removing the profile removes all three.
#[cfg_attr(target_os = "macos", allow(dead_code))]
pub fn webview_dir(base: &Path, id: &str) -> PathBuf {
    profile_dir(base, id).join("webview")
}

/// The renderer's origin for a profile. macOS gives each profile its own,
/// because WKWebView before macOS 14 has no per-profile store and an origin
/// is what separates localStorage there (WP0 S5); WebView2 spells the scheme
/// over http.
#[cfg(target_os = "macos")]
pub fn app_origin(id: &str) -> String {
    format!("app://{id}")
}

#[cfg(windows)]
pub fn app_origin(_id: &str) -> String {
    "http://app.localhost".to_string()
}

#[cfg(not(any(target_os = "macos", windows)))]
pub fn app_origin(_id: &str) -> String {
    "app://localhost".to_string()
}

/// What only a window can do.
pub trait Shell: Send + Sync {
    /// Replace the main window with one on the active profile's storage. A
    /// webview's storage is fixed when it is built, so reloading would show the
    /// previous account's data under the new profile's name. The old window
    /// goes first, and [`Profiles::main_window_closed`] is called once it has,
    /// before the new one is built.
    fn reopen(&self);
    /// Send the main window back to the app's root.
    fn reload(&self);
    /// Drop a removed profile's webview store where it is not a folder (macOS).
    fn forget_store(&self, id: &str);
    /// The main window's cookie store, which is its profile's; `None` with no
    /// window.
    fn main_webview(&self) -> Option<Arc<dyn CookieWebview>>;
}

/// What waits for the old main window to be gone.
#[derive(Default)]
struct Closing {
    /// The network the next window uses: the old window keeps its own until
    /// then, or its last requests would go out on the new profile's session.
    net: Option<Arc<ProfileNet>>,
    /// Removed profiles whose storage the old window may still have open.
    forget: Vec<String>,
}

/// `connection.get`.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectionState {
    pub configured: bool,
    pub server_url: String,
    pub locked: bool,
    /// Whether the connection screen can offer a way out: adding a profile
    /// lands there with nowhere else to go.
    pub can_cancel: bool,
}

/// One row of the switcher.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProfileView {
    pub id: String,
    pub label: String,
    pub server_url: String,
    pub active: bool,
    /// Signed in right now; `None` when signed out or unreachable.
    pub identity: Option<Account>,
    /// Who it belongs to, as last seen.
    pub account: Option<Account>,
    /// The earlier profile on the same account, or `""`.
    pub duplicate_of: String,
}

/// `profiles.get` and every profile change.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProfilesView {
    pub active_profile_id: String,
    pub profiles: Vec<ProfileView>,
}

pub struct Profiles {
    base: PathBuf,
    lock: Option<String>,
    network: Arc<Network>,
    shell: Box<dyn Shell>,
    config: Mutex<ServerConfig>,
    /// The live answer per profile, kept between openings of the switcher.
    identities: Mutex<HashMap<String, Option<Account>>>,
    /// The profile an added one came from, to go back to if it is abandoned.
    /// A moment, not a setting, so never stored.
    return_to: Mutex<Option<String>>,
    /// One change at a time: each reads the file, changes it and acts on it.
    busy: tokio::sync::Mutex<()>,
    closing: Mutex<Closing>,
}

/// A poisoned lock still answers: what it guards is replaced whole, never
/// left half-written.
fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

fn io_error(error: io::Error) -> String {
    error.to_string()
}

impl Profiles {
    /// `lock` is the server `AGENTRQ_SERVER_URL` pins, if any.
    pub fn new(
        base: PathBuf,
        lock: Option<String>,
        network: Arc<Network>,
        shell: Box<dyn Shell>,
    ) -> Self {
        let config = store(&base).load();
        Self {
            base,
            lock,
            network,
            shell,
            config: Mutex::new(config),
            identities: Mutex::new(HashMap::new()),
            return_to: Mutex::new(None),
            busy: tokio::sync::Mutex::new(()),
            closing: Mutex::new(Closing::default()),
        }
    }

    fn store(&self) -> ServerConfigStore<ConfigOnDisk> {
        store(&self.base)
    }

    fn config(&self) -> ServerConfig {
        lock(&self.config).clone()
    }

    fn keep(&self, config: ServerConfig) {
        *lock(&self.config) = config;
    }

    /// The active profile's id.
    pub fn active_id(&self) -> String {
        self.config().active().id.clone()
    }

    /// The active profile's server; `""` when it has none yet.
    pub fn server_url(&self) -> String {
        server_of(self.config().active(), self.lock.as_deref())
    }

    /// The folder for a profile's webview storage; macOS has a data store instead.
    #[cfg_attr(target_os = "macos", allow(dead_code))]
    pub fn webview_dir(&self, id: &str) -> PathBuf {
        webview_dir(&self.base, id)
    }

    /// Delete what removed profiles left behind: a webview that was just
    /// closed can hold its folder open for a moment, so a removal that could
    /// not delete it is finished here, at the next start.
    pub fn sweep(&self) {
        let names: Vec<String> = std::fs::read_dir(self.base.join("profiles"))
            .into_iter()
            .flatten()
            .flatten()
            .map(|entry| entry.file_name().to_string_lossy().into_owned())
            .collect();
        for name in orphaned_profile_dirs(&names, &self.config().state()) {
            let _ = std::fs::remove_dir_all(profile_dir(&self.base, name));
        }
    }

    /// Of the app's macOS data stores, those no profile owns: the sweep's
    /// counterpart for a store.
    #[cfg_attr(not(target_os = "macos"), allow(dead_code))]
    pub fn orphaned_stores(&self, stores: &[[u8; 16]]) -> Vec<[u8; 16]> {
        orphaned_data_stores(stores, &self.config().state())
    }

    pub fn connection(&self) -> ConnectionState {
        let config = self.config();
        let server_url = self.server_url();
        ConnectionState {
            configured: !server_url.is_empty(),
            server_url,
            locked: self.lock.is_some(),
            can_cancel: can_discard_active_profile(Some(&config.state())),
        }
    }

    /// `connection.validate`: `{ok: true, url}` or `{ok: false, reason, detail?}`.
    /// The probe's auth flags are no use to the screen, so they stay here.
    pub async fn validate(&self, input: &str) -> Value {
        match validate(input).await {
            Ok(valid) => json!({"ok": true, "url": valid.url}),
            Err(error) => refusal(&error.reason, error.detail),
        }
    }

    /// `connection.save`: point the active profile at a server and reload.
    pub async fn save(&self, input: &str) -> Result<Value, String> {
        if self.lock.is_some() {
            return Ok(refusal(CONNECTION_LOCKED_REASON, None));
        }
        let valid = match validate(input).await {
            Ok(valid) => valid,
            Err(error) => return Ok(refusal(&error.reason, error.detail)),
        };
        let _busy = self.busy.lock().await;
        let url = self.store().save(&valid.url).map_err(save_error)?;
        self.keep(self.store().load());
        self.network.active().await.set_server_url(&url);
        self.shell.reload();
        Ok(json!({"ok": true, "url": url}))
    }

    /// `connection.cancel`: abandon a profile that was added and never
    /// connected, and go back to the one it was added from.
    pub async fn cancel(&self) -> Result<bool, String> {
        if !can_discard_active_profile(Some(&self.config().state())) {
            return Ok(false);
        }
        let discarded = self.active_id();
        let fallback = lock(&self.return_to).take().unwrap_or_default();
        self.forget(&discarded, &fallback).await?;
        Ok(true)
    }

    /// The switcher's rows, from what is known now.
    pub fn view(&self) -> ProfilesView {
        let config = self.config();
        let state = config.state();
        let identities = lock(&self.identities);
        ProfilesView {
            active_profile_id: state.active_profile_id.clone(),
            profiles: state
                .profiles
                .iter()
                .map(|profile| ProfileView {
                    id: profile.id.clone(),
                    label: profile.label.clone(),
                    server_url: profile.server_url.clone(),
                    active: profile.id == state.active_profile_id,
                    identity: identities.get(&profile.id).cloned().flatten(),
                    account: profile.account.clone(),
                    duplicate_of: duplicate_of(Some(&state), &profile.id),
                })
                .collect(),
        }
    }

    /// `profiles.get`: ask every profile who it is, each through its own jar,
    /// all at once, then remember each account a lookup found. A lookup that
    /// failed leaves the stored account alone.
    pub async fn get(&self) -> Result<ProfilesView, String> {
        let profiles = self.config().profiles;
        let mut lookups = Vec::new();
        for profile in profiles {
            let server = server_of(&profile, self.lock.as_deref());
            let net = self.network.profile(&profile.id, &server).await;
            let fetch = Arc::new(ClientFetch {
                client: net.client.clone(),
                runtime: tokio::runtime::Handle::current(),
            });
            lookups.push((
                profile.id,
                tokio::task::spawn_blocking(move || {
                    fetch_profile_identity(fetch, &server, &ThreadDeadline)
                }),
            ));
        }
        let mut learned = Vec::new();
        for (id, lookup) in lookups {
            learned.push((id, lookup.await.ok().flatten()));
        }

        let _busy = self.busy.lock().await;
        for (id, identity) in learned {
            let record = serde_json::to_value(&identity).unwrap_or_default();
            lock(&self.identities).insert(id.clone(), identity);
            let config = self
                .store()
                .remember_account(&id, &record)
                .map_err(io_error)?;
            self.keep(config);
        }
        Ok(self.view())
    }

    /// Rebuild the window on the active profile. Its network is opened now,
    /// which can wait on the keychain, and made active once the old window is
    /// gone.
    async fn reopen(&self) -> ProfilesView {
        let config = self.config();
        let active = config.active();
        let server = server_of(active, self.lock.as_deref());
        let net = self.network.profile(&active.id, &server).await;
        net.set_server_url(&server);
        lock(&self.closing).net = Some(net);
        self.shell.reopen();
        self.view()
    }

    /// The old main window is gone and the new one not yet built: the network
    /// moves to the new profile, and a removed profile's storage, which the
    /// old window had open, is deleted.
    pub fn main_window_closed(&self) {
        let closing = std::mem::take(&mut *lock(&self.closing));
        if let Some(net) = closing.net {
            self.network.activate(net);
        }
        for id in closing.forget {
            self.forget_storage(&id);
        }
    }

    fn forget_storage(&self, id: &str) {
        let _ = std::fs::remove_dir_all(profile_dir(&self.base, id));
        self.shell.forget_store(id);
    }

    /// `profiles.switch`. The same or an unknown id changes nothing.
    pub async fn switch(&self, id: &str) -> Result<ProfilesView, String> {
        let _busy = self.busy.lock().await;
        let config = self.config();
        if id == config.active_profile_id || !config.profiles.iter().any(|p| p.id == id) {
            return Ok(self.view());
        }
        self.keep(self.store().activate_profile(id).map_err(io_error)?);
        Ok(self.reopen().await)
    }

    /// `profiles.add`: a new profile, switched to, on the connection screen.
    pub async fn add(&self, label: &str) -> Result<ProfilesView, String> {
        let _busy = self.busy.lock().await;
        *lock(&self.return_to) = Some(self.active_id());
        self.keep(self.store().add_profile(label).map_err(io_error)?);
        Ok(self.reopen().await)
    }

    /// `profiles.rename`. A blank name keeps the old one.
    pub async fn rename(&self, id: &str, label: &str) -> Result<ProfilesView, String> {
        let _busy = self.busy.lock().await;
        self.keep(self.store().rename_profile(id, label).map_err(io_error)?);
        Ok(self.view())
    }

    /// `profiles.remove`. The last profile stays.
    pub async fn remove(&self, id: &str) -> Result<ProfilesView, String> {
        self.forget(id, "").await
    }

    /// Remove a profile and everything it kept: its jar, attachments and
    /// webview storage, its side panel's site permissions and its cached
    /// identity. One that cleared less would leave a removed account signed in.
    async fn forget(&self, id: &str, fallback: &str) -> Result<ProfilesView, String> {
        let _busy = self.busy.lock().await;
        let before = self.config();
        let was_active = id == before.active_profile_id;
        let after = self
            .store()
            .remove_profile(id, fallback)
            .map_err(io_error)?;
        if after.profiles.len() == before.profiles.len() {
            return Ok(self.view());
        }
        self.keep(after);
        lock(&self.identities).remove(id);
        if let Some(net) = self.network.forget(id) {
            net.jar.clear();
        }
        PermissionStore::new(&self.base, id).forget();
        if !was_active {
            self.forget_storage(id);
            return Ok(self.view());
        }
        // The window still on it has its storage open until it is gone.
        lock(&self.closing).forget.push(id.to_string());
        Ok(self.reopen().await)
    }

    /// A sign-in window of profile `id` is done: move its session into the
    /// profile's jar. Nothing for a profile removed while it was open, which
    /// would otherwise come back with its files. Whether it is signed in.
    pub async fn finish_sign_in(&self, id: &str, webview: Arc<dyn CookieWebview>) -> bool {
        let _busy = self.busy.lock().await;
        let Some(profile) = self.config().profiles.into_iter().find(|p| p.id == id) else {
            return false;
        };
        let server = server_of(&profile, self.lock.as_deref());
        let net = self.network.profile(id, &server).await;
        let signed_in = sign_in::finish(webview, server, net.jar.clone(), COOKIE_DEADLINE).await;
        if signed_in {
            self.signed_in(id);
        }
        signed_in
    }

    /// The app reloads into a sign-in, unless another profile was switched to
    /// meanwhile.
    fn signed_in(&self, id: &str) {
        if id == self.active_id() {
            self.shell.reload();
        }
    }

    /// The menu's Log Out: the active profile's cookies, and nothing else. The
    /// webview's go too, the provider's among them, as Electron clears the
    /// partition's.
    pub async fn log_out(&self) {
        self.network.active().await.jar.clear();
        if let Some(webview) = self.shell.main_webview() {
            sign_in::clear(webview, COOKIE_DEADLINE).await;
        }
        self.shell.reload();
    }

    /// The menu's Switch Server: forget the active profile's server, back to
    /// the connection screen. Nothing while `AGENTRQ_SERVER_URL` pins it.
    pub async fn switch_server(&self) -> Result<(), String> {
        if self.lock.is_some() {
            return Ok(());
        }
        let _busy = self.busy.lock().await;
        self.store().clear().map_err(io_error)?;
        self.keep(self.store().load());
        self.network.active().await.set_server_url("");
        self.shell.reload();
        Ok(())
    }
}

/// The URL was validated before it was saved, so only a write can fail here.
fn save_error(error: SaveError) -> String {
    match error {
        SaveError::Invalid(reason) => reason.to_string(),
        SaveError::Io(error) => io_error(error),
    }
}

fn refusal(reason: &str, detail: Option<String>) -> Value {
    match detail {
        Some(detail) => json!({"ok": false, "reason": reason, "detail": detail}),
        None => json!({"ok": false, "reason": reason}),
    }
}

/// The probe's answer, fetched before the core's check runs: the core is
/// synchronous, and the request is not.
struct Answered(Result<ProbeResponse, String>);

impl ConfigProbe for Answered {
    fn get(&self, _url: &str) -> Result<ProbeResponse, String> {
        self.0.clone()
    }
}

/// Is `input` an AgentRQ server? The probe carries no session, so it goes on
/// a client of its own that follows redirects, as `fetch` does (an http
/// address that redirects to https is still the server).
async fn validate(
    input: &str,
) -> Result<
    agentrq_desktop_core::server_config::ValidServer,
    agentrq_desktop_core::server_config::ValidationError,
> {
    let answer = match normalize_server_url(input) {
        Ok(url) => probe(&format!("{url}{CONFIG_PROBE_PATH}"), PROBE_DEADLINE).await,
        // Refused before any request; the core says why.
        Err(_) => Err(String::new()),
    };
    validate_server_url(input, &Answered(answer))
}

async fn probe(url: &str, deadline: Duration) -> Result<ProbeResponse, String> {
    let request = async {
        let response = reqwest::get(url).await.map_err(|e| e.to_string())?;
        let status = response.status().as_u16();
        let body = response.text().await.map_err(|e| e.to_string())?;
        Ok(ProbeResponse { status, body })
    };
    tokio::time::timeout(deadline, request)
        .await
        .unwrap_or_else(|_| Err(format!("No answer within {}s", deadline.as_secs())))
}

/// `/api/v1/auth/user` through one profile's client. The core races it
/// against its four seconds on a thread, so the request runs on the app's
/// runtime from there, with its own timeout so an abandoned one ends too.
struct ClientFetch {
    client: reqwest::Client,
    runtime: tokio::runtime::Handle,
}

impl UserFetch for ClientFetch {
    fn get(&self, url: &str) -> Result<Option<UserReply>, String> {
        self.runtime.block_on(async {
            let response = self
                .client
                .get(url)
                .timeout(Duration::from_millis(IDENTITY_TIMEOUT_MS))
                .send()
                .await
                .map_err(|e| e.to_string())?;
            let ok = response.status().is_success();
            let json = match response.bytes().await {
                Ok(body) => serde_json::from_slice::<Value>(&body).map_err(|e| e.to_string()),
                Err(error) => Err(error.to_string()),
            };
            Ok(Some(UserReply { ok, json }))
        })
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::sign_in::tests::FakeWebview;
    use crate::test_server::{reply, Step, TestServer};
    use agentrq_desktop_core::profiles::data_store_id;

    /// No keychain: the jars' key goes to a file.
    struct NoKeychain;

    impl crate::vault::Keychain for NoKeychain {
        fn get(&self) -> Result<Option<Vec<u8>>, crate::vault::KeychainError> {
            Ok(None)
        }
        fn set(&self, _: &[u8]) -> Result<(), crate::vault::KeychainError> {
            Err(crate::vault::KeychainError::Missing)
        }
    }

    /// What the shell was asked to do. Like the real one, it only starts
    /// closing the window: a test says when it is gone, with
    /// `main_window_closed`.
    #[derive(Default)]
    pub struct FakeShell {
        pub calls: Arc<Mutex<Vec<String>>>,
        pub webview: Option<Arc<FakeWebview>>,
    }

    impl Shell for FakeShell {
        fn reopen(&self) {
            lock(&self.calls).push("reopen".into());
        }
        fn reload(&self) {
            lock(&self.calls).push("reload".into());
        }
        fn forget_store(&self, id: &str) {
            lock(&self.calls).push(format!("forget {id}"));
        }
        fn main_webview(&self) -> Option<Arc<dyn CookieWebview>> {
            self.webview
                .clone()
                .map(|webview| webview as Arc<dyn CookieWebview>)
        }
    }

    pub struct Rig {
        pub dir: tempfile::TempDir,
        pub profiles: Arc<Profiles>,
        pub network: Arc<Network>,
        pub calls: Arc<Mutex<Vec<String>>>,
    }

    impl Rig {
        pub fn calls(&self) -> Vec<String> {
            std::mem::take(&mut *lock(&self.calls))
        }
        fn file(&self) -> Value {
            serde_json::from_str(
                &std::fs::read_to_string(self.dir.path().join(CONFIG_FILENAME)).unwrap(),
            )
            .unwrap()
        }
    }

    /// Profiles on a fresh data dir, its config written as `config` (or none),
    /// with `lock` as `AGENTRQ_SERVER_URL`.
    pub fn rig_with(config: Option<Value>, lock: Option<&str>) -> Rig {
        rig_on(config, lock, FakeShell::default())
    }

    fn rig_on(config: Option<Value>, lock: Option<&str>, shell: FakeShell) -> Rig {
        let dir = tempfile::tempdir().unwrap();
        if let Some(config) = config {
            std::fs::write(dir.path().join(CONFIG_FILENAME), config.to_string()).unwrap();
        }
        let first = first_profile(dir.path(), lock);
        let network = Arc::new(Network::new(
            dir.path().to_path_buf(),
            Arc::new(NoKeychain),
            first,
        ));
        let calls = shell.calls.clone();
        let profiles = Arc::new(Profiles::new(
            dir.path().to_path_buf(),
            lock.map(str::to_string),
            network.clone(),
            Box::new(shell),
        ));
        Rig {
            dir,
            profiles,
            network,
            calls,
        }
    }

    pub fn rig() -> Rig {
        rig_with(None, None)
    }

    fn two_profiles(work_server: &str) -> Value {
        json!({
            "version": 3,
            "activeProfileId": "default",
            "profiles": [
                {"id": "default", "label": "Default", "serverUrl": "http://127.0.0.1:9"},
                {"id": "work", "label": "Work", "serverUrl": work_server},
            ],
        })
    }

    const AUTH_CONFIG: &str = r#"{"rootLoginEnabled":true,"githubLoginEnabled":false}"#;

    /// An AgentRQ server whose `/api/v1/auth/user` answers as `user` (a 401
    /// when `None`).
    async fn server(user: Option<&'static str>) -> TestServer {
        TestServer::start(move |seen| match (seen.target.as_str(), user) {
            ("/api/v1/auth/config", _) => reply("200 OK", &[], AUTH_CONFIG),
            ("/api/v1/auth/user", Some(body)) => reply("200 OK", &[], body),
            _ => reply("401 Unauthorized", &[], "{}"),
        })
        .await
    }

    #[test]
    fn names_each_profiles_storage_and_origin() {
        let base = Path::new("data");
        assert_eq!(
            webview_dir(base, "work"),
            base.join("profiles").join("work").join("webview")
        );
        #[cfg(target_os = "macos")]
        const ORIGIN: &str = "app://work";
        #[cfg(windows)]
        const ORIGIN: &str = "http://app.localhost";
        #[cfg(not(any(target_os = "macos", windows)))]
        const ORIGIN: &str = "app://localhost";
        assert_eq!(app_origin("work"), ORIGIN);
        assert_ne!(data_store_id("work"), data_store_id("default"));
    }

    #[test]
    fn starts_unconfigured_on_a_first_run() {
        let rig = rig();
        assert_eq!(
            rig.profiles.connection(),
            ConnectionState {
                configured: false,
                server_url: String::new(),
                locked: false,
                can_cancel: false,
            }
        );
        assert_eq!(rig.profiles.active_id(), "default");
        assert_eq!(
            rig.profiles.webview_dir("default"),
            rig.dir.path().join("profiles/default/webview")
        );
        let view = rig.profiles.view();
        assert_eq!(view.active_profile_id, "default");
        assert_eq!(view.profiles[0].label, "Default");
        assert!(view.profiles[0].active);
    }

    #[test]
    fn reads_an_electron_v2_config_and_honours_the_pinned_server() {
        let rig = rig_with(
            Some(json!({"version": 2, "serverUrl": "https://a.example.com"})),
            Some("http://127.0.0.1:7"),
        );
        let state = rig.profiles.connection();
        assert_eq!(state.server_url, "http://127.0.0.1:7");
        assert!(state.locked && state.configured);
        assert_eq!(rig.network.server_url(), "http://127.0.0.1:7");
        assert_eq!(
            rig.profiles.view().profiles[0].server_url,
            "https://a.example.com"
        );
    }

    #[tokio::test]
    async fn validates_a_server_without_storing_it() {
        let rig = rig();
        let srv = server(None).await;
        assert_eq!(
            rig.profiles.validate(&srv.url).await,
            json!({"ok": true, "url": srv.url})
        );
        assert_eq!(
            rig.profiles.validate("").await,
            json!({"ok": false, "reason": "Server URL is required"})
        );
        let refused = rig.profiles.validate("http://127.0.0.1:1").await;
        assert_eq!(refused["reason"], "Could not reach http://127.0.0.1:1");
        assert!(refused["detail"].is_string());
        assert!(!rig.dir.path().join(CONFIG_FILENAME).exists());
        assert!(rig.calls().is_empty());
    }

    #[tokio::test]
    async fn gives_up_on_a_probe_that_never_answers() {
        let srv = TestServer::start(|_| vec![Step::Wait(5_000)]).await;
        let answer = probe(&format!("{}/x", srv.url), Duration::from_millis(50)).await;
        assert_eq!(answer, Err("No answer within 0s".to_string()));
        let cut = TestServer::start(|_| {
            vec![crate::test_server::send(
                "HTTP/1.1 200 OK\r\ncontent-length: 10\r\n\r\nshort",
            )]
        })
        .await;
        assert!(
            probe(&cut.url, PROBE_DEADLINE).await.is_err(),
            "body cut short"
        );
    }

    #[tokio::test]
    async fn saves_a_server_points_the_active_client_at_it_and_reloads() {
        let rig = rig();
        let srv = server(None).await;
        assert_eq!(
            rig.profiles.save(&srv.url).await,
            Ok(json!({"ok": true, "url": srv.url}))
        );
        assert_eq!(rig.profiles.server_url(), srv.url);
        assert_eq!(rig.network.active().await.server_url(), srv.url);
        assert_eq!(rig.file()["profiles"][0]["serverUrl"], srv.url);
        assert_eq!(rig.calls(), ["reload"]);

        let bad = rig.profiles.save("ftp://x").await.unwrap();
        assert_eq!(
            bad,
            json!({"ok": false, "reason": "Server URL must use http or https"})
        );
        assert!(rig.calls().is_empty());
    }

    #[tokio::test]
    async fn refuses_to_save_while_the_environment_pins_the_server() {
        let rig = rig_with(None, Some("http://127.0.0.1:7"));
        assert_eq!(
            rig.profiles.save("http://127.0.0.1:8").await,
            Ok(json!({"ok": false, "reason": CONNECTION_LOCKED_REASON}))
        );
        assert_eq!(rig.profiles.switch_server().await, Ok(()));
        assert!(rig.calls().is_empty());
        assert!(!rig.dir.path().join(CONFIG_FILENAME).exists());
    }

    #[tokio::test]
    async fn reports_a_config_it_cannot_write() {
        let rig = rig();
        let srv = server(None).await;
        // A folder where the file should be: every write fails.
        std::fs::create_dir(rig.dir.path().join(CONFIG_FILENAME)).unwrap();
        assert!(rig.profiles.save(&srv.url).await.is_err());
        assert!(rig.profiles.add("").await.is_err());
        assert!(rig.profiles.rename("default", "x").await.is_err());
        assert!(rig.profiles.switch_server().await.is_err());
        assert!(rig.calls().is_empty());
    }

    #[tokio::test]
    async fn reports_a_switch_or_removal_it_cannot_write() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        let file = rig.dir.path().join(CONFIG_FILENAME);
        std::fs::remove_file(&file).unwrap();
        std::fs::create_dir(&file).unwrap();
        // The cached state still has both; the file cannot take the change.
        assert!(rig.profiles.switch("work").await.is_err());
        assert!(rig.profiles.remove("work").await.is_err());

        let rig = rig_with(Some(two_profiles("")), None);
        rig.profiles.switch("work").await.unwrap();
        let file = rig.dir.path().join(CONFIG_FILENAME);
        std::fs::remove_file(&file).unwrap();
        std::fs::create_dir(&file).unwrap();
        assert!(rig.profiles.connection().can_cancel);
        assert!(rig.profiles.cancel().await.is_err());
    }

    #[tokio::test]
    async fn adds_a_profile_and_cancels_back_to_the_one_it_came_from() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        rig.profiles.switch("work").await.unwrap();
        rig.profiles.main_window_closed();
        let added = rig.profiles.add("").await.unwrap();
        rig.profiles.main_window_closed();
        let new_id = added.active_profile_id.clone();
        assert_eq!(added.profiles.len(), 3);
        assert_eq!(added.profiles[2].label, "Profile 3");
        assert_eq!(rig.network.active().await.id, new_id);
        assert_eq!(rig.calls(), ["reopen", "reopen"]);
        assert!(rig.profiles.connection().can_cancel);

        // Something the abandoned profile left on disk goes with it, once its
        // window no longer has it open.
        std::fs::create_dir_all(rig.profiles.webview_dir(&new_id)).unwrap();
        assert_eq!(rig.profiles.cancel().await, Ok(true));
        assert_eq!(
            rig.profiles.active_id(),
            "work",
            "back where it was added from"
        );
        assert!(profile_dir(rig.dir.path(), &new_id).exists());
        assert_eq!(rig.calls(), ["reopen"]);
        rig.profiles.main_window_closed();
        assert_eq!(rig.network.active().await.id, "work");
        assert!(!profile_dir(rig.dir.path(), &new_id).exists());
        assert_eq!(rig.calls(), [format!("forget {new_id}")]);

        // Nothing to cancel now: "work" has a server.
        assert_eq!(rig.profiles.cancel().await, Ok(false));
    }

    #[tokio::test]
    async fn switches_only_to_another_known_profile() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:8")), None);
        let same = rig.profiles.switch("default").await.unwrap();
        let unknown = rig.profiles.switch("nobody").await.unwrap();
        assert_eq!(same, unknown);
        assert!(rig.calls().is_empty());

        let view = rig.profiles.switch("work").await.unwrap();
        assert_eq!(view.active_profile_id, "work");
        assert!(view.profiles[1].active);
        assert_eq!(
            rig.network.active().await.id,
            "default",
            "the old window keeps its own session until it is gone"
        );
        rig.profiles.main_window_closed();
        let net = rig.network.active().await;
        assert_eq!(
            (net.id.as_str(), net.server_url()),
            ("work", "http://127.0.0.1:8".into())
        );
        assert_eq!(rig.calls(), ["reopen"]);
        assert_eq!(rig.file()["activeProfileId"], "work");
    }

    #[tokio::test]
    async fn renames_a_profile() {
        let rig = rig();
        let view = rig.profiles.rename("default", "  Home  ").await.unwrap();
        assert_eq!(view.profiles[0].label, "Home");
        assert_eq!(rig.file()["profiles"][0]["label"], "Home");
    }

    #[tokio::test]
    async fn removes_another_profile_with_everything_it_kept() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        let work = rig.network.profile("work", "http://127.0.0.1:9").await;
        let server = url::Url::parse("http://127.0.0.1:9/").unwrap();
        let cookie = cookie_store::RawCookie::parse("at=t; Max-Age=600").unwrap();
        assert!(work.jar.adopt(&cookie, &server));
        let dir = profile_dir(rig.dir.path(), "work");
        std::fs::create_dir_all(dir.join("webview")).unwrap();
        let permissions = PermissionStore::new(rig.dir.path(), "work");
        permissions.set("https://a.example", "media", "allow");
        assert!(permissions.file().exists());

        let view = rig.profiles.remove("work").await.unwrap();
        assert_eq!(view.profiles.len(), 1);
        assert!(!dir.exists(), "jar, attachments and webview storage");
        assert!(!permissions.file().exists());
        assert!(
            !work.jar.has("at", &server),
            "a late request finds no session"
        );
        assert_eq!(rig.calls(), ["forget work"], "no window change");

        // The last profile stays.
        let last = rig.profiles.remove("default").await.unwrap();
        assert_eq!(last.profiles.len(), 1);
        assert!(rig.calls().is_empty());
    }

    #[tokio::test]
    async fn removing_the_active_profile_moves_the_window_off_it_first() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        let old = rig.network.active().await;
        let server = url::Url::parse("http://127.0.0.1:9/").unwrap();
        let cookie = cookie_store::RawCookie::parse("at=t; Max-Age=600").unwrap();
        assert!(old.jar.adopt(&cookie, &server));
        let dir = profile_dir(rig.dir.path(), "default");
        std::fs::create_dir_all(dir.join("webview")).unwrap();

        let view = rig.profiles.remove("default").await.unwrap();
        assert_eq!(view.active_profile_id, "work");
        assert!(!old.jar.has("at", &server), "signed out at once");
        assert!(dir.exists(), "its window still has it open");
        assert_eq!(rig.calls(), ["reopen"]);

        rig.profiles.main_window_closed();
        assert_eq!(rig.network.active().await.id, "work");
        assert!(!dir.exists());
        assert_eq!(rig.calls(), ["forget default"]);
        rig.profiles.main_window_closed();
        assert!(rig.calls().is_empty(), "nothing left to do");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn names_each_profile_by_its_own_session_and_remembers_the_account() {
        let signed_in = server(Some(r#"{"name":"Ada","email":"ada@example.com"}"#)).await;
        let signed_out = server(None).await;
        let rig = rig_with(Some(two_profiles(&signed_out.url)), None);
        rig.profiles.save(&signed_in.url).await.unwrap();

        let view = rig.profiles.get().await.unwrap();
        let ada = Account {
            name: "Ada".into(),
            email: "ada@example.com".into(),
            picture: String::new(),
        };
        assert_eq!(view.profiles[0].identity.as_ref(), Some(&ada));
        assert_eq!(view.profiles[0].account.as_ref(), Some(&ada));
        assert_eq!(view.profiles[1].identity, None);
        assert_eq!(view.profiles[1].account, None);
        assert_eq!(
            rig.file()["profiles"][0]["account"]["email"],
            "ada@example.com"
        );

        // Asked again with the server gone, it is signed out now, but still
        // known to be Ada's.
        let moved = rig
            .file()
            .to_string()
            .replace(&signed_in.url, "http://127.0.0.1:1");
        std::fs::write(rig.dir.path().join(CONFIG_FILENAME), moved).unwrap();
        rig.profiles.keep(rig.profiles.store().load());
        let again = rig.profiles.get().await.unwrap();
        assert_eq!(again.profiles[0].identity, None);
        assert_eq!(again.profiles[0].account.as_ref(), Some(&ada));
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn flags_a_second_profile_on_the_same_account() {
        let ada = server(Some(r#"{"name":"Ada","email":"ADA@example.com "}"#)).await;
        let ada_again = server(Some(r#"{"name":"A","email":"ada@example.com"}"#)).await;
        let rig = rig_with(
            Some(json!({
                "version": 3,
                "activeProfileId": "default",
                "profiles": [
                    {"id": "default", "label": "Default", "serverUrl": ada.url},
                    {"id": "work", "label": "Work", "serverUrl": ada_again.url},
                ],
            })),
            None,
        );
        let view = rig.profiles.get().await.unwrap();
        assert_eq!(view.profiles[0].duplicate_of, "");
        assert_eq!(view.profiles[1].duplicate_of, "default");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn reports_identities_it_cannot_remember() {
        let rig = rig();
        std::fs::create_dir(rig.dir.path().join(CONFIG_FILENAME)).unwrap();
        // An unconfigured profile is not asked, and nothing found is nothing
        // to write.
        assert!(rig.profiles.get().await.is_ok());

        // An account found is, and the file is a folder.
        let srv = server(Some(r#"{"email":"a@b.c"}"#)).await;
        let rig = rig_with(
            Some(json!({
                "version": 3, "activeProfileId": "default",
                "profiles": [{"id": "default", "label": "Default", "serverUrl": srv.url}],
            })),
            None,
        );
        let file = rig.dir.path().join(CONFIG_FILENAME);
        std::fs::remove_file(&file).unwrap();
        std::fs::create_dir(&file).unwrap();
        assert!(rig.profiles.get().await.is_err());
    }

    #[tokio::test]
    async fn logs_out_by_clearing_only_the_active_jar() {
        let webview = Arc::new(FakeWebview::with(&[
            ("at=stale; Path=/", "127.0.0.1"),
            ("_gh_sess=x; Path=/", ".github.com"),
        ]));
        let shell = FakeShell {
            webview: Some(webview.clone()),
            ..FakeShell::default()
        };
        let rig = rig_on(Some(two_profiles("http://127.0.0.1:9")), None, shell);
        let server = url::Url::parse("http://127.0.0.1:9/").unwrap();
        let cookie = cookie_store::RawCookie::parse("at=t; Max-Age=600").unwrap();
        let active = rig.network.active().await;
        let work = rig.network.profile("work", "http://127.0.0.1:9").await;
        assert!(active.jar.adopt(&cookie, &server) && work.jar.adopt(&cookie, &server));

        rig.profiles.log_out().await;
        assert!(!active.jar.has("at", &server));
        assert!(webview.names().is_empty(), "the provider's cookies too");

        // No window, no webview store to clear.
        let windowless = rig_with(None, None);
        windowless.profiles.log_out().await;
        assert_eq!(windowless.calls(), ["reload"]);
        assert!(
            work.jar.has("at", &server),
            "another profile stays signed in"
        );
        assert_eq!(
            rig.profiles.server_url(),
            "http://127.0.0.1:9",
            "the server stays"
        );
        assert_eq!(rig.calls(), ["reload"]);
    }

    #[test]
    fn describes_a_failed_save() {
        assert_eq!(save_error(SaveError::Invalid("no")), "no");
        let io = io::Error::other("disk full");
        assert_eq!(save_error(SaveError::Io(io)), "disk full");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn takes_a_user_reply_cut_short_or_not_json_as_not_signed_in() {
        let cut = TestServer::start(|_| {
            vec![crate::test_server::send(
                "HTTP/1.1 200 OK\r\ncontent-length: 10\r\n\r\nshort",
            )]
        })
        .await;
        let html = TestServer::start(|_| reply("200 OK", &[], "<html>")).await;
        let rig = rig_with(
            Some(json!({"profiles": [
                {"id": "default", "serverUrl": cut.url},
                {"id": "work", "serverUrl": html.url},
            ]})),
            None,
        );
        let view = rig.profiles.get().await.unwrap();
        assert_eq!(view.profiles[0].identity, None);
        assert_eq!(view.profiles[1].identity, None);
        assert_eq!(html.requests()[0].target, "/api/v1/auth/user");
    }

    fn sign_in_window() -> Arc<FakeWebview> {
        Arc::new(FakeWebview::with(&[
            ("at=a1; Path=/; Max-Age=600", "127.0.0.1"),
            ("rt=r1; Path=/api/v1/auth/refresh; Max-Age=600", "127.0.0.1"),
        ]))
    }

    #[tokio::test]
    async fn moves_a_finished_sign_in_into_its_profiles_jar() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        let server = url::Url::parse("http://127.0.0.1:9/").unwrap();
        let window = sign_in_window();
        assert!(rig.profiles.finish_sign_in("work", window.clone()).await);
        let work = rig.network.profile("work", "http://127.0.0.1:9").await;
        assert!(work.jar.has("at", &server));
        assert!(window.names().is_empty(), "nothing left in the webview");
        assert!(rig.calls().is_empty(), "not the profile on screen");

        assert!(
            rig.profiles
                .finish_sign_in("default", sign_in_window())
                .await
        );
        assert_eq!(rig.calls(), ["reload"]);

        // A window closed with no session in it.
        let empty = Arc::new(FakeWebview::default());
        assert!(!rig.profiles.finish_sign_in("default", empty).await);
        assert!(rig.calls().is_empty());
    }

    #[tokio::test]
    async fn ignores_a_sign_in_of_a_profile_removed_meanwhile() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:9")), None);
        rig.profiles.remove("work").await.unwrap();
        rig.calls();
        let window = sign_in_window();
        assert!(!rig.profiles.finish_sign_in("work", window.clone()).await);
        assert!(!profile_dir(rig.dir.path(), "work").exists());
        assert_eq!(window.names(), ["at", "rt"], "never read");
        assert!(rig.calls().is_empty());
    }

    #[test]
    fn names_the_data_stores_no_profile_owns() {
        let rig = rig_with(Some(two_profiles("")), None);
        let gone = data_store_id("gone");
        let stores = [data_store_id("default"), gone, data_store_id("work")];
        assert_eq!(rig.profiles.orphaned_stores(&stores), [gone]);
    }

    #[tokio::test]
    async fn switch_server_forgets_only_the_server() {
        let rig = rig_with(Some(two_profiles("http://127.0.0.1:8")), None);
        assert_eq!(rig.profiles.switch_server().await, Ok(()));
        assert_eq!(rig.profiles.connection().server_url, "");
        assert_eq!(rig.network.active().await.server_url(), "");
        assert_eq!(rig.file()["profiles"][0]["serverUrl"], "");
        assert_eq!(rig.file()["profiles"][1]["serverUrl"], "http://127.0.0.1:8");
        assert_eq!(rig.calls(), ["reload"]);
    }

    #[test]
    fn sweeps_the_folders_of_removed_profiles_on_start() {
        let rig = rig();
        let base = rig.dir.path();
        for name in ["default", "gone", "Keep"] {
            std::fs::create_dir_all(base.join("profiles").join(name)).unwrap();
        }
        rig.profiles.sweep();
        assert!(base.join("profiles/default").exists());
        assert!(!base.join("profiles/gone").exists());
        assert!(base.join("profiles/Keep").exists(), "not a profile's name");
        rig_with(None, None).profiles.sweep();
    }

    #[test]
    fn a_poisoned_lock_still_answers() {
        let rig = rig();
        let profiles = rig.profiles.clone();
        let _ = std::thread::spawn(move || {
            let _guard = profiles.config.lock().unwrap();
            std::panic::resume_unwind(Box::new("poison"));
        })
        .join();
        assert_eq!(rig.profiles.active_id(), "default");
    }
}
