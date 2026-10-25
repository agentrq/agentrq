// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The active profile's way to the server: its client, its cookie jar, its
//! attachment cache and the server it points at.
//!
//! What Electron gets from `session.fromPartition`. Everything that talks to
//! the server goes through the active profile's client, never a shared one, or
//! two profiles would be one account.
//!
//! Which profile is active is `profiles.rs`'s to decide: it calls
//! [`Network::activate`] after a profile switch, and
//! [`ProfileNet::set_server_url`] after the connection screen.
//!
//! A profile is opened once and kept: two jars on one file would each write
//! over the other's cookies. Opening the first one waits on the keychain, so
//! nothing that does not talk to the server waits for it.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard, RwLock};
use std::time::Duration;

use agentrq_desktop_core::attachment_store::{AttachmentStore, DiskFs};
use agentrq_desktop_core::protocol::AppProtocol;
use tokio::sync::OnceCell;

use crate::cookie_jar::ProfileJar;
use crate::vault::{load_key, Keychain, Vault};

/// How long the keychain may take to hand over the key. It can block on an
/// unlock prompt; past this the sessions are kept in memory only, until the
/// app restarts.
pub const KEYCHAIN_DEADLINE: Duration = Duration::from_secs(10);

/// A profile's folder: its jar and its attachments, side by side.
pub fn profile_dir(base: &Path, id: &str) -> PathBuf {
    base.join("profiles").join(id)
}

pub struct ProfileNet {
    #[allow(dead_code)] // Only tests ask which profile a network is.
    pub id: String,
    server_url: RwLock<String>,
    pub client: reqwest::Client,
    pub jar: Arc<ProfileJar>,
    pub attachments: AttachmentStore<DiskFs>,
}

impl ProfileNet {
    /// With no `vault`, the jar is kept in memory only.
    pub fn new(
        base: &Path,
        id: &str,
        server_url: &str,
        vault: Option<Vault>,
    ) -> reqwest::Result<Self> {
        let dir = profile_dir(base, id);
        let jar = Arc::new(ProfileJar::open(dir.join("cookies"), vault));
        let client = reqwest::Client::builder()
            .cookie_provider(jar.clone())
            // The renderer follows a redirect itself, through the scheme again.
            .redirect(reqwest::redirect::Policy::none())
            .build()?;
        let attachments = AttachmentStore::new(&dir.join("attachments").to_string_lossy(), DiskFs);
        Ok(Self {
            id: id.to_string(),
            server_url: RwLock::new(server_url.to_string()),
            client,
            jar,
            attachments,
        })
    }

    /// The server, as it is now: it changes under a running app.
    pub fn server_url(&self) -> String {
        self.server_url
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .clone()
    }

    pub fn set_server_url(&self, url: &str) {
        *self
            .server_url
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = url.to_string();
    }

    /// The protocol's rules, naming this profile's server.
    pub fn protocol(self: &Arc<Self>, dev_server_url: &str) -> AppProtocol<impl Fn() -> String> {
        let this = self.clone();
        AppProtocol::new(move || this.server_url(), dev_server_url)
    }
}

/// Every profile's network lives here; one is active.
pub struct Network {
    base: PathBuf,
    keychain: Arc<dyn Keychain + Send + Sync>,
    /// `None` when the keychain refused or was too slow.
    vault: OnceCell<Option<Vault>>,
    keychain_deadline: Duration,
    first: (String, String),
    active: Mutex<Option<Arc<ProfileNet>>>,
    opened: Mutex<HashMap<String, Arc<ProfileNet>>>,
}

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
}

impl Network {
    /// `first` is the profile id and server to start with.
    pub fn new(
        base: PathBuf,
        keychain: Arc<dyn Keychain + Send + Sync>,
        first: (String, String),
    ) -> Self {
        Self {
            base,
            keychain,
            vault: OnceCell::new(),
            keychain_deadline: KEYCHAIN_DEADLINE,
            first,
            active: Mutex::new(None),
            opened: Mutex::new(HashMap::new()),
        }
    }

    async fn vault(&self) -> Option<Vault> {
        self.vault
            .get_or_init(|| async {
                let keychain = self.keychain.clone();
                let fallback = self.base.join("cookie.key");
                let read = tokio::task::spawn_blocking(move || load_key(&*keychain, &fallback));
                match tokio::time::timeout(self.keychain_deadline, read).await {
                    Ok(Ok(Ok(key))) => Some(Vault::new(&key)),
                    _ => None,
                }
            })
            .await
            .clone()
    }

    /// The active profile's network.
    pub async fn active(&self) -> Arc<ProfileNet> {
        if let Some(net) = self.current() {
            return net;
        }
        let (id, server_url) = self.first.clone();
        let net = self.open(&id, &server_url).await;
        lock(&self.active).get_or_insert(net).clone()
    }

    fn current(&self) -> Option<Arc<ProfileNet>> {
        lock(&self.active).clone()
    }

    /// The active profile's id, without opening the profile. The side
    /// panel's storage is named after it.
    pub fn active_id(&self) -> String {
        self.current()
            .map_or_else(|| self.first.0.clone(), |net| net.id.clone())
    }

    /// The active profile's server, without opening the profile.
    pub fn server_url(&self) -> String {
        self.current()
            .map_or_else(|| self.first.1.clone(), |net| net.server_url())
    }

    /// The active profile's id, without opening the profile.
    #[allow(dead_code)]
    pub fn active_id(&self) -> String {
        self.current()
            .map_or_else(|| self.first.0.clone(), |net| net.id.clone())
    }

    /// The protocol's rules, naming the active profile's server.
    pub fn protocol(self: &Arc<Self>, dev_server_url: &str) -> AppProtocol<impl Fn() -> String> {
        let this = self.clone();
        AppProtocol::new(move || this.server_url(), dev_server_url)
    }

    /// The profile's network, opened on first use. Nothing is awaited between
    /// looking it up and keeping it, so it is only ever opened once.
    ///
    /// One opened with no vault is not kept unless it is the active one: its
    /// jar can never read its file, so kept it would show the profile signed
    /// out for the rest of the session and lose a sign-in at quit.
    async fn open(&self, id: &str, server_url: &str) -> Arc<ProfileNet> {
        let vault = self.vault().await;
        if let Some(net) = self.current().filter(|net| net.id == id) {
            return net;
        }
        let new = |vault| {
            Arc::new(
                ProfileNet::new(&self.base, id, server_url, vault)
                    .expect("the HTTP client builds with the TLS backend it was compiled with"),
            )
        };
        match vault {
            Some(vault) => lock(&self.opened)
                .entry(id.to_string())
                .or_insert_with(|| new(Some(vault)))
                .clone(),
            None => new(None),
        }
    }

    /// A profile's network, active or not: its identity is asked through its
    /// own jar, and a sign-in lands there.
    pub async fn profile(&self, id: &str, server_url: &str) -> Arc<ProfileNet> {
        self.open(id, server_url).await
    }

    /// Let go of a removed profile's network, so it is never opened on its old
    /// files again. Returns it, for a last clear of its jar.
    pub fn forget(&self, id: &str) -> Option<Arc<ProfileNet>> {
        let kept = lock(&self.opened).remove(id);
        kept.or_else(|| self.current().filter(|net| net.id == id))
    }

    /// Make another profile's network the active one, once nothing of the
    /// previous profile's window is left to use it. Requests already under
    /// way finish on the profile they started on.
    pub fn activate(&self, net: Arc<ProfileNet>) {
        *lock(&self.active) = Some(net);
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::vault::KeychainError;

    /// No keychain to store a key in, answering after `delay_ms`.
    pub struct NoKeychain {
        delay_ms: u64,
    }

    impl Keychain for NoKeychain {
        fn get(&self) -> Result<Option<Vec<u8>>, KeychainError> {
            std::thread::sleep(Duration::from_millis(self.delay_ms));
            Ok(None)
        }
        fn set(&self, _: &[u8]) -> Result<(), KeychainError> {
            Err(KeychainError::Missing)
        }
    }

    pub fn network(dir: &Path, server_url: &str) -> Network {
        slow_network(dir, server_url, 0)
    }

    /// A network whose keychain takes `delay_ms` to answer.
    pub fn slow_network(dir: &Path, server_url: &str, delay_ms: u64) -> Network {
        Network::new(
            dir.to_path_buf(),
            Arc::new(NoKeychain { delay_ms }),
            ("default".into(), server_url.into()),
        )
    }

    #[tokio::test]
    async fn opens_the_first_profile_once_and_keeps_it() {
        let dir = tempfile::tempdir().unwrap();
        let net = network(dir.path(), "http://127.0.0.1:1");
        assert_eq!(net.active_id(), "default");
        let first = net.active().await;
        assert_eq!(first.id, "default");
        assert_eq!(net.active_id(), "default");
        assert_eq!(first.server_url(), "http://127.0.0.1:1");
        assert!(Arc::ptr_eq(&first, &net.active().await));
        assert!(dir.path().join("cookie.key").exists(), "no keychain here");
    }

    #[tokio::test]
    async fn switching_gives_another_profile_its_own_jar_and_cache() {
        let dir = tempfile::tempdir().unwrap();
        let net = network(dir.path(), "http://127.0.0.1:1");
        assert_eq!(net.active_id(), "default", "before it is opened");
        let first = net.active().await;
        let work = net.profile("work", "http://127.0.0.1:2").await;
        assert!(Arc::ptr_eq(&first, &net.active().await), "not active yet");
        net.activate(work.clone());
        assert_eq!(net.active_id(), "work");
        assert!(Arc::ptr_eq(&work, &net.active().await));
        assert!(!Arc::ptr_eq(&first.jar, &work.jar));
        assert_eq!(work.server_url(), "http://127.0.0.1:2");

        let back = net.profile("default", "http://127.0.0.1:4").await;
        assert!(Arc::ptr_eq(&back, &first), "one jar per profile file");
        let work = net.profile("work", "http://127.0.0.1:9").await;
        assert_eq!(work.server_url(), "http://127.0.0.1:2", "opened once");

        work.set_server_url("http://127.0.0.1:3");
        assert_eq!(work.server_url(), "http://127.0.0.1:3");
        assert_eq!(
            work.protocol("").proxy_target("/api/v1/x", "").unwrap(),
            "http://127.0.0.1:3/api/v1/x"
        );
    }

    #[tokio::test]
    async fn opens_a_profile_once_however_many_ask_at_the_same_time() {
        let dir = tempfile::tempdir().unwrap();
        let net = Arc::new(network(dir.path(), "http://127.0.0.1:1"));
        let (a, b) = tokio::join!(net.active(), net.active());
        assert!(Arc::ptr_eq(&a, &b));
    }

    #[tokio::test]
    async fn names_the_server_before_the_profile_is_opened() {
        let dir = tempfile::tempdir().unwrap();
        let net = Arc::new(network(dir.path(), "http://127.0.0.1:1"));
        assert_eq!(net.server_url(), "http://127.0.0.1:1");
        assert!(net.current().is_none(), "the keychain was not asked");
        net.active().await.set_server_url("http://127.0.0.1:5");
        assert_eq!(
            net.protocol("").proxy_target("/api/v1/x", "").unwrap(),
            "http://127.0.0.1:5/api/v1/x"
        );
    }

    #[tokio::test]
    async fn keeps_the_session_in_memory_when_the_keychain_hangs() {
        let dir = tempfile::tempdir().unwrap();
        let mut net = slow_network(dir.path(), "", 1000);
        net.keychain_deadline = Duration::from_millis(50);
        let opened = net.active().await;
        assert!(opened.jar.is_memory_only());
        assert!(!dir.path().join("cookie.key").exists());

        // The active one is kept, in memory; any other is opened afresh.
        assert!(Arc::ptr_eq(&opened, &net.profile("default", "").await));
        let work = net.profile("work", "").await;
        assert!(!Arc::ptr_eq(&work, &net.profile("work", "").await));
        assert!(net.forget("work").is_none(), "nothing kept to forget");
        assert!(Arc::ptr_eq(&opened, &net.forget("default").unwrap()));
    }

    #[test]
    fn a_poisoned_lock_still_answers() {
        let dir = tempfile::tempdir().unwrap();
        let profile = Arc::new(
            ProfileNet::new(
                dir.path(),
                "p",
                "http://a",
                Some(Vault::new(&crate::vault::random_key())),
            )
            .unwrap(),
        );
        let held = profile.clone();
        let _ = std::thread::spawn(move || {
            let _guard = held.server_url.write().unwrap();
            std::panic::resume_unwind(Box::new("poison"));
        })
        .join();
        profile.set_server_url("http://b");
        assert_eq!(profile.server_url(), "http://b");

        let net = network(dir.path(), "");
        let held = Arc::new(net);
        let poisoner = held.clone();
        let _ = std::thread::spawn(move || {
            let _guard = poisoner.active.lock().unwrap();
            std::panic::resume_unwind(Box::new("poison"));
        })
        .join();
        assert!(held.current().is_none());
    }
}
