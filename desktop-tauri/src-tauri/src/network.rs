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
//! Which profile is active is WP4's to decide: it calls [`Network::switch`]
//! after a profile switch, and [`ProfileNet::set_server_url`] after the
//! connection screen. Until then the first profile is the one in the config.

use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, MutexGuard, RwLock};
use std::time::Duration;

use agentrq_desktop_core::attachment_store::{AttachmentStore, DiskFs};
use agentrq_desktop_core::protocol::AppProtocol;
use tokio::sync::OnceCell;

use crate::cookie_jar::ProfileJar;
use crate::vault::{load_key, random_key, Keychain, Vault};

/// How long the keychain may take to hand over the key. It can block on an
/// unlock prompt; past this the session is kept in memory only.
pub const KEYCHAIN_DEADLINE: Duration = Duration::from_secs(10);

/// A profile's folder: its jar and its attachments, side by side.
pub fn profile_dir(base: &Path, id: &str) -> PathBuf {
    base.join("profiles").join(id)
}

pub struct ProfileNet {
    #[allow(dead_code)] // WP4 tells profiles apart by it.
    pub id: String,
    server_url: RwLock<String>,
    pub client: reqwest::Client,
    #[allow(dead_code)] // WP4 signs in and out through it.
    pub jar: Arc<ProfileJar>,
    pub attachments: AttachmentStore<DiskFs>,
}

impl ProfileNet {
    pub fn new(base: &Path, id: &str, server_url: &str, vault: Vault) -> reqwest::Result<Self> {
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

    #[allow(dead_code)] // WP4, after the connection screen.
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
    vault: OnceCell<Vault>,
    keychain_deadline: Duration,
    first: (String, String),
    active: Mutex<Option<Arc<ProfileNet>>>,
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
        }
    }

    async fn vault(&self) -> Vault {
        self.vault
            .get_or_init(|| async {
                let keychain = self.keychain.clone();
                let fallback = self.base.join("cookie.key");
                let read = tokio::task::spawn_blocking(move || load_key(&*keychain, &fallback));
                let key = match tokio::time::timeout(self.keychain_deadline, read).await {
                    Ok(Ok(Ok(key))) => key,
                    _ => random_key(),
                };
                Vault::new(&key)
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
        self.slot().get_or_insert(net).clone()
    }

    fn slot(&self) -> MutexGuard<'_, Option<Arc<ProfileNet>>> {
        self.active
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    fn current(&self) -> Option<Arc<ProfileNet>> {
        self.slot().clone()
    }

    async fn open(&self, id: &str, server_url: &str) -> Arc<ProfileNet> {
        let vault = self.vault().await;
        Arc::new(
            ProfileNet::new(&self.base, id, server_url, vault)
                .expect("the HTTP client builds with the TLS backend it was compiled with"),
        )
    }

    /// Make another profile the active one. Requests already under way finish
    /// on the profile they started on.
    #[allow(dead_code)] // WP4, after a profile switch.
    pub async fn switch(&self, id: &str, server_url: &str) -> Arc<ProfileNet> {
        let net = self.open(id, server_url).await;
        *self.slot() = Some(net.clone());
        net
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;

    /// No keychain to store a key in, answering after `delay_ms`.
    pub struct NoKeychain {
        delay_ms: u64,
    }

    impl Keychain for NoKeychain {
        fn get(&self) -> Result<Option<Vec<u8>>, String> {
            std::thread::sleep(Duration::from_millis(self.delay_ms));
            Ok(None)
        }
        fn set(&self, _: &[u8]) -> Result<(), String> {
            Err("none".into())
        }
    }

    pub fn network(dir: &Path, server_url: &str) -> Network {
        Network::new(
            dir.to_path_buf(),
            Arc::new(NoKeychain { delay_ms: 0 }),
            ("default".into(), server_url.into()),
        )
    }

    #[tokio::test]
    async fn opens_the_first_profile_once_and_keeps_it() {
        let dir = tempfile::tempdir().unwrap();
        let net = network(dir.path(), "http://127.0.0.1:1");
        let first = net.active().await;
        assert_eq!(first.id, "default");
        assert_eq!(first.server_url(), "http://127.0.0.1:1");
        assert!(Arc::ptr_eq(&first, &net.active().await));
        assert!(dir.path().join("cookie.key").exists(), "no keychain here");
    }

    #[tokio::test]
    async fn switching_gives_another_profile_its_own_jar_and_cache() {
        let dir = tempfile::tempdir().unwrap();
        let net = network(dir.path(), "http://127.0.0.1:1");
        let first = net.active().await;
        let work = net.switch("work", "http://127.0.0.1:2").await;
        assert!(Arc::ptr_eq(&work, &net.active().await));
        assert!(!Arc::ptr_eq(&first.jar, &work.jar));
        assert_eq!(work.server_url(), "http://127.0.0.1:2");

        work.set_server_url("http://127.0.0.1:3");
        assert_eq!(work.server_url(), "http://127.0.0.1:3");
        assert_eq!(
            work.protocol("").proxy_target("/api/v1/x", "").unwrap(),
            "http://127.0.0.1:3/api/v1/x"
        );
    }

    #[tokio::test]
    async fn keeps_the_session_in_memory_when_the_keychain_hangs() {
        let dir = tempfile::tempdir().unwrap();
        let mut net = Network::new(
            dir.path().to_path_buf(),
            Arc::new(NoKeychain { delay_ms: 1000 }),
            ("default".into(), String::new()),
        );
        net.keychain_deadline = Duration::from_millis(50);
        let opened = net.active().await;
        assert_eq!(opened.server_url(), "");
    }

    #[test]
    fn a_poisoned_lock_still_answers() {
        let dir = tempfile::tempdir().unwrap();
        let profile = Arc::new(
            ProfileNet::new(
                dir.path(),
                "p",
                "http://a",
                Vault::new(&crate::vault::random_key()),
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
