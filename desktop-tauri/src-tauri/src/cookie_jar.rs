// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! A profile's cookies, the way Electron's session partition keeps them.
//!
//! The proxy's client is the only thing that reads or writes them: the server's
//! `Set-Cookie` lands here and never reaches the renderer, and `document.cookie`
//! stays empty. Each profile has its own jar and its own client; a jar shared
//! between profiles is two accounts signed in as one (Electron's `net.fetch`
//! trap in `docs/agents/desktop.md`).
//!
//! The jar is written to disk sealed by the [`Vault`] on every change, and only
//! its persistent, unexpired cookies, as a browser keeps them. A jar with no
//! vault (the keychain refused, or was too slow) lives in memory only: it never
//! reads the file, and never writes over a session it could not open.

use std::fs;
use std::io::BufReader;
use std::path::PathBuf;
use std::sync::{Mutex, MutexGuard};

use cookie_store::{CookieStore, RawCookie};
use reqwest::header::HeaderValue;
use url::Url;

use crate::vault::{write_private, Vault};

pub struct ProfileJar {
    store: Mutex<CookieStore>,
    file: PathBuf,
    vault: Option<Vault>,
}

impl ProfileJar {
    /// The jar saved in `file`, or an empty one when there is none or it cannot
    /// be opened (no vault, another key, a damaged file): signed out, which is
    /// safe.
    pub fn open(file: PathBuf, vault: Option<Vault>) -> Self {
        let store = vault
            .as_ref()
            .and_then(|vault| vault.open(&fs::read(&file).ok()?))
            .and_then(|json| cookie_store::serde::json::load(BufReader::new(json.as_slice())).ok())
            .unwrap_or_default();
        Self {
            store: Mutex::new(store),
            file,
            vault,
        }
    }

    fn lock(&self) -> MutexGuard<'_, CookieStore> {
        self.store
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    fn save(&self, store: &CookieStore) {
        let Some(vault) = &self.vault else {
            return;
        };
        let mut json = Vec::new();
        // Writing JSON to memory only fails on a cookie serde cannot encode,
        // and then the jar on disk keeps its previous contents.
        if cookie_store::serde::json::save(store, &mut json).is_ok() {
            // A jar that cannot be written stays in memory: signed in until quit.
            let _ = write_private(&self.file, &vault.seal(&json));
        }
    }

    /// Keep a cookie read from a sign-in webview, as if `server` had set it.
    /// Returns whether the jar took it (its domain must match the server's).
    ///
    /// The webview's cookies are read with `cookies()` and core's `find_auth_cookie`,
    /// never `cookies_for_url`, which misses an IP host and a `Domain=` cookie
    /// on macOS. On Windows that read deadlocks inside a navigation or event
    /// handler (wry#583), so the caller reads on a task of its own.
    pub fn adopt(&self, cookie: &RawCookie<'_>, server: &Url) -> bool {
        let mut store = self.lock();
        let kept = store.insert_raw(cookie, server).is_ok();
        if kept {
            self.save(&store);
        }
        kept
    }

    /// The names and values of the cookies that would be sent to `url`.
    pub fn sent_to(&self, url: &Url) -> Vec<(String, String)> {
        self.lock()
            .get_request_values(url)
            .map(|(name, value)| (name.to_string(), value.to_string()))
            .collect()
    }

    /// Whether a cookie by this name would be sent to `url`.
    #[cfg(test)]
    pub fn has(&self, name: &str, url: &Url) -> bool {
        self.lock().matches(url).iter().any(|c| c.name() == name)
    }

    #[cfg(test)]
    pub fn is_memory_only(&self) -> bool {
        self.vault.is_none()
    }

    /// Every cookie, for signing out.
    pub fn clear(&self) {
        let mut store = self.lock();
        store.clear();
        self.save(&store);
    }
}

impl reqwest::cookie::CookieStore for ProfileJar {
    fn set_cookies(&self, headers: &mut dyn Iterator<Item = &HeaderValue>, url: &Url) {
        let cookies: Vec<RawCookie<'static>> = headers
            .filter_map(|value| std::str::from_utf8(value.as_bytes()).ok())
            .filter_map(|text| RawCookie::parse(text.to_string()).ok())
            .collect();
        if cookies.is_empty() {
            return;
        }
        let mut store = self.lock();
        store.store_response_cookies(cookies.into_iter(), url);
        self.save(&store);
    }

    fn cookies(&self, url: &Url) -> Option<HeaderValue> {
        let header = self
            .lock()
            .get_request_values(url)
            .map(|(name, value)| format!("{name}={value}"))
            .collect::<Vec<_>>()
            .join("; ");
        if header.is_empty() {
            return None;
        }
        HeaderValue::from_str(&header).ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use reqwest::cookie::CookieStore as _;
    use std::sync::LazyLock;

    use crate::vault::random_key;

    /// One random key for every jar in these tests, so a reopened jar can read the file.
    static KEY: LazyLock<[u8; 32]> = LazyLock::new(random_key);

    fn jar_in(dir: &tempfile::TempDir) -> ProfileJar {
        ProfileJar::open(dir.path().join("jar"), Some(Vault::new(&KEY)))
    }

    fn server() -> Url {
        Url::parse("http://127.0.0.1:3000/api/v1/auth/github/callback").unwrap()
    }

    fn set(jar: &ProfileJar, headers: &[&str]) {
        let values: Vec<HeaderValue> = headers
            .iter()
            .map(|h| HeaderValue::from_str(h).unwrap())
            .collect();
        jar.set_cookies(&mut values.iter(), &server());
    }

    const AT: &str = "at=token; Path=/; HttpOnly; SameSite=Lax; Max-Age=86400";

    #[test]
    fn sends_back_what_the_server_set() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar_in(&dir);
        assert!(jar.cookies(&server()).is_none());

        set(
            &jar,
            &[AT, "rt=r; Path=/api/v1/auth/refresh; Max-Age=86400"],
        );
        let url = Url::parse("http://127.0.0.1:3000/api/v1/workspaces").unwrap();
        assert_eq!(jar.cookies(&url).unwrap(), "at=token");
        assert!(jar.has("at", &url));
        assert!(!jar.has("rt", &url));

        let other = Url::parse("http://localhost:3000/api/v1/workspaces").unwrap();
        assert!(jar.cookies(&other).is_none(), "never to another host");
    }

    #[test]
    fn ignores_headers_that_are_not_cookies() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar_in(&dir);
        let values = [HeaderValue::from_bytes(b"\xff=1").unwrap()];
        jar.set_cookies(&mut values.iter(), &server());
        set(&jar, &[""]);
        assert!(jar.cookies(&server()).is_none());
        assert!(
            !dir.path().join("jar").exists(),
            "nothing changed, nothing written"
        );
    }

    #[test]
    fn survives_a_restart_sealed_and_private() {
        let dir = tempfile::tempdir().unwrap();
        set(&jar_in(&dir), &[AT, "session=s; Path=/"]);

        let on_disk = fs::read(dir.path().join("jar")).unwrap();
        assert!(!on_disk.windows(5).any(|w| w == b"token"), "sealed");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(dir.path().join("jar"))
                .unwrap()
                .permissions()
                .mode();
            assert_eq!(mode & 0o777, 0o600);
        }

        let again = jar_in(&dir);
        assert_eq!(
            again.cookies(&server()).unwrap(),
            "at=token",
            "a session cookie ends with the app"
        );
    }

    #[test]
    fn starts_signed_out_when_the_jar_cannot_be_opened() {
        let dir = tempfile::tempdir().unwrap();
        set(&jar_in(&dir), &[AT]);

        let other_key = ProfileJar::open(dir.path().join("jar"), Some(Vault::new(&random_key())));
        assert!(other_key.cookies(&server()).is_none());

        let vault = Vault::new(&KEY);
        fs::write(dir.path().join("jar"), vault.seal(b"not json")).unwrap();
        assert!(jar_in(&dir).cookies(&server()).is_none());
    }

    #[test]
    fn never_writes_over_a_saved_session_without_its_vault() {
        let dir = tempfile::tempdir().unwrap();
        set(&jar_in(&dir), &[AT]);
        let saved = fs::read(dir.path().join("jar")).unwrap();

        let memory_only = ProfileJar::open(dir.path().join("jar"), None);
        assert!(memory_only.cookies(&server()).is_none());
        set(&memory_only, &["at=other; Path=/; Max-Age=60"]);
        assert_eq!(memory_only.cookies(&server()).unwrap(), "at=other");
        memory_only.clear();
        assert_eq!(fs::read(dir.path().join("jar")).unwrap(), saved);
        assert_eq!(jar_in(&dir).cookies(&server()).unwrap(), "at=token");
    }

    #[test]
    fn stays_signed_in_in_memory_when_the_disk_refuses() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("file"), b"x").unwrap();
        let jar = ProfileJar::open(dir.path().join("file").join("jar"), Some(Vault::new(&KEY)));
        set(&jar, &[AT]);
        assert_eq!(jar.cookies(&server()).unwrap(), "at=token");
    }

    #[test]
    fn adopts_a_sign_in_cookie_for_the_server_only() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar_in(&dir);
        let at = RawCookie::parse("at=webview; Path=/; Max-Age=60").unwrap();
        assert!(jar.adopt(&at, &server()));
        assert!(jar.has("at", &server()));
        assert!(jar_in(&dir).has("at", &server()), "saved");

        let foreign = RawCookie::parse("at=x; Domain=example.com; Path=/").unwrap();
        assert!(!jar.adopt(&foreign, &server()));
    }

    #[test]
    fn forgets_everything_on_clear() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar_in(&dir);
        set(&jar, &[AT]);
        jar.clear();
        assert!(jar.cookies(&server()).is_none());
        assert!(jar_in(&dir).cookies(&server()).is_none());
    }

    #[test]
    fn a_poisoned_lock_still_answers() {
        let dir = tempfile::tempdir().unwrap();
        let jar = std::sync::Arc::new(jar_in(&dir));
        set(&jar, &[AT]);
        let held = jar.clone();
        let _ = std::thread::spawn(move || {
            let _guard = held.store.lock().unwrap();
            std::panic::resume_unwind(Box::new("poison"));
        })
        .join();
        assert!(jar.store.is_poisoned());
        assert!(jar.has("at", &server()));
    }
}
