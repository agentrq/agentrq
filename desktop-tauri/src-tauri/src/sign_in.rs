// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What a finished sign-in, and a Log Out, do with a webview's cookies.
//!
//! The flow's decisions are core's `SignInFlow`, and the window is lib.rs's.
//! Electron's sign-in window shares the profile's partition, so the cookie is
//! simply there afterwards. Here the profile's session is the Rust jar, so the
//! sign-in window's cookies are read once it is done and moved into the jar.

use std::sync::Arc;
use std::time::Duration;

use agentrq_desktop_core::auth::{cookie_domain_matches, find_auth_cookie, Cookie, AUTH_COOKIE};
use cookie_store::RawCookie;
use url::Url;

use crate::cookie_jar::ProfileJar;

/// How long a webview may take over its cookies. Every await needs a deadline
/// (`docs/agents/desktop.md`).
pub const COOKIE_DEADLINE: Duration = Duration::from_secs(10);

/// A webview's cookie store, as wry has it. Both calls wait on the webview,
/// and on Windows they deadlock inside a webview callback (wry#583), so they
/// are only ever made on a blocking thread of their own.
pub trait CookieWebview: Send + Sync {
    /// Every cookie in the store: `cookies()`, never `cookies_for_url` (WP0 S4).
    fn cookies(&self) -> Vec<RawCookie<'static>>;
    fn delete_cookie(&self, cookie: RawCookie<'static>);
}

/// A webview's cookie as core sees it.
pub fn to_core(cookie: &RawCookie<'_>) -> Cookie {
    Cookie {
        name: cookie.name().to_string(),
        value: cookie.value().to_string(),
        domain: cookie.domain().map(str::to_string),
        path: cookie.path().map(str::to_string),
        expires: cookie.expires_datetime().map(|at| at.unix_timestamp()),
    }
}

/// Move a sign-in's session from its webview into the profile's jar.
///
/// Every cookie the webview holds for the server's host goes, not only `at`:
/// the refresh cookie, scoped to `/api/v1/auth/refresh`, is what keeps the
/// session past a day, and Electron's partition keeps both. Returns whether
/// the jar now holds the `at` the webview had, which is what success means,
/// and the cookies taken, for the caller to delete from the webview. With no
/// jar nothing is kept, and the cookies are only handed back.
pub fn take_session(
    cookies: Vec<RawCookie<'static>>,
    server_url: &str,
    jar: Option<&ProfileJar>,
) -> (bool, Vec<RawCookie<'static>>) {
    let Some((server, host)) = Url::parse(server_url)
        .ok()
        .and_then(|url| Some((url.clone(), url.host_str()?.to_string())))
    else {
        return (false, Vec::new());
    };
    let core: Vec<Cookie> = cookies.iter().map(to_core).collect();
    let found = find_auth_cookie(&core, server_url).is_some();
    let taken: Vec<RawCookie<'static>> = cookies
        .into_iter()
        .filter(|c| c.domain().is_some_and(|d| cookie_domain_matches(d, &host)))
        .collect();
    let Some(jar) = jar else {
        return (false, taken);
    };
    let mut kept_at = false;
    for cookie in &taken {
        let kept = jar.adopt(&for_jar(cookie, &host), &server);
        kept_at |= kept && cookie.name() == AUTH_COOKIE;
    }
    (found && kept_at, taken)
}

/// The cookie as the jar should keep it. The webview reports a domain with
/// its dot stripped, so `Domain=` is set again only for a parent of the
/// server's host; a host-only cookie stays host-only.
fn for_jar(cookie: &RawCookie<'_>, host: &str) -> RawCookie<'static> {
    let mut kept = RawCookie::new(cookie.name().to_string(), cookie.value().to_string());
    kept.set_path(cookie.path().unwrap_or("/").to_string());
    kept.set_http_only(cookie.http_only());
    kept.set_secure(cookie.secure());
    kept.set_same_site(cookie.same_site());
    if let Some(expires) = cookie.expires() {
        kept.set_expires(expires);
    }
    if let Some(domain) = cookie.domain().filter(|d| !d.eq_ignore_ascii_case(host)) {
        kept.set_domain(domain.to_string());
    }
    kept
}

/// What to hand `delete_cookie` for one cookie the webview reported.
///
/// wry builds the native cookie from `domain()`, which strips a leading dot,
/// and the stores match the domain exactly: a `Domain=` cookie is kept as
/// `.host`, a host-only one as `host`, and which it was is lost on the way
/// out. So both are deleted. `domain()` strips one dot only, so two make it
/// hand back `.host`.
pub fn deletion_forms(cookie: &RawCookie<'_>) -> Vec<RawCookie<'static>> {
    let plain = cookie.clone().into_owned();
    let Some(domain) = cookie.domain() else {
        return vec![plain];
    };
    let mut dotted = plain.clone();
    dotted.set_domain(format!("..{domain}"));
    vec![plain, dotted]
}

fn delete(webview: &dyn CookieWebview, cookies: &[RawCookie<'_>]) {
    for cookie in cookies {
        for form in deletion_forms(cookie) {
            webview.delete_cookie(form);
        }
    }
}

/// `job` on a blocking thread, `false` if it is not done within `deadline`.
async fn off_thread(deadline: Duration, job: Box<dyn FnOnce() -> bool + Send>) -> bool {
    let job = tokio::task::spawn_blocking(job);
    matches!(tokio::time::timeout(deadline, job).await, Ok(Ok(true)))
}

/// A finished sign-in window's cookies, and whether they came within
/// `deadline`. A late read is still waited for: the sign-in has failed, but
/// its session must leave the webview before the window closes, or it sits
/// there in plaintext. A webview that never answers takes no delete anyway,
/// and the person can close its window.
pub async fn read(
    webview: Arc<dyn CookieWebview>,
    deadline: Duration,
) -> (Vec<RawCookie<'static>>, bool) {
    let mut job = tokio::task::spawn_blocking(move || webview.cookies());
    let (done, in_time) = match tokio::time::timeout(deadline, &mut job).await {
        Ok(done) => (done, true),
        Err(_) => (job.await, false),
    };
    (done.unwrap_or_default(), in_time)
}

/// Take `cookies` out of the webview's store, where they would sign the next
/// sign-in straight back in. Waited for, since the window closes next and a
/// delete sent to a closed webview is lost.
pub async fn forget(webview: Arc<dyn CookieWebview>, cookies: Vec<RawCookie<'static>>) {
    let _ = tokio::task::spawn_blocking(move || delete(&*webview, &cookies)).await;
}

/// Log Out: every cookie in the profile's webview store, as Electron clears
/// its partition's, so the provider's (GitHub's, Google's) go too and the
/// next sign-in asks who it is. Whether it was done in time.
pub async fn clear(webview: Arc<dyn CookieWebview>, deadline: Duration) -> bool {
    off_thread(
        deadline,
        Box::new(move || {
            delete(&*webview, &webview.cookies());
            true
        }),
    )
    .await
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::vault::{random_key, Vault};
    use std::sync::Mutex;

    /// A webview's store as libsoup, WKWebView and WebView2 keep one: by the
    /// domain as stored, `.host` for a `Domain=` cookie, matched exactly. It
    /// reports each cookie the way wry does, through `domain()`.
    #[derive(Default)]
    pub struct FakeWebview {
        /// The domain as stored, and the cookie.
        pub store: Mutex<Vec<(String, RawCookie<'static>)>>,
        pub delay: Duration,
    }

    impl FakeWebview {
        /// `(Set-Cookie text, domain as stored)`
        pub fn with(cookies: &[(&str, &str)]) -> Self {
            let store = cookies
                .iter()
                .map(|(text, domain)| (domain.to_string(), cookie(text)))
                .collect();
            Self {
                store: Mutex::new(store),
                delay: Duration::ZERO,
            }
        }

        pub fn names(&self) -> Vec<String> {
            let store = self.store.lock().unwrap();
            store.iter().map(|(_, c)| c.name().to_string()).collect()
        }
    }

    impl CookieWebview for FakeWebview {
        fn cookies(&self) -> Vec<RawCookie<'static>> {
            std::thread::sleep(self.delay);
            let store = self.store.lock().unwrap();
            store
                .iter()
                .map(|(domain, c)| {
                    let mut reported = c.clone();
                    reported.set_domain(domain.clone());
                    reported
                })
                .collect()
        }
        fn delete_cookie(&self, cookie: RawCookie<'static>) {
            self.store.lock().unwrap().retain(|(domain, c)| {
                !(Some(domain.as_str()) == cookie.domain()
                    && c.name() == cookie.name()
                    && c.path() == cookie.path())
            });
        }
    }

    fn jar(dir: &tempfile::TempDir) -> ProfileJar {
        ProfileJar::open(dir.path().join("jar"), Some(Vault::new(&random_key())))
    }

    fn cookie(text: &str) -> RawCookie<'static> {
        RawCookie::parse(text.to_string()).unwrap()
    }

    fn url(text: &str) -> Url {
        Url::parse(text).unwrap()
    }

    fn domains<'a>(forms: &'a [RawCookie<'static>]) -> Vec<Option<&'a str>> {
        forms.iter().map(RawCookie::domain).collect()
    }

    #[test]
    fn deletes_a_cookie_in_both_the_forms_a_store_may_keep_it() {
        // What wry reads back from `.agentrq.com` and from a host-only cookie.
        for text in ["at=a; Domain=.agentrq.com", "at=a; Domain=agentrq.com"] {
            let forms = deletion_forms(&cookie(text));
            assert_eq!(
                domains(&forms),
                [Some("agentrq.com"), Some(".agentrq.com")],
                "{text}"
            );
            assert!(forms.iter().all(|form| form.name() == "at"));
        }
        assert_eq!(domains(&deletion_forms(&cookie("at=a"))), [None]);
    }

    #[tokio::test]
    async fn finishing_moves_the_session_and_leaves_none_in_the_webview() {
        let dir = tempfile::tempdir().unwrap();
        let jar = Arc::new(jar(&dir));
        let webview = Arc::new(FakeWebview::with(&[
            ("at=a1; Path=/; Max-Age=600", ".agentrq.com"),
            (
                "rt=r1; Path=/api/v1/auth/refresh; Max-Age=600",
                "app.agentrq.com",
            ),
            ("_gh_sess=x; Path=/", ".github.com"),
        ]));
        let (cookies, in_time) = read(webview.clone(), COOKIE_DEADLINE).await;
        assert!(in_time);
        let (signed_in, taken) = take_session(cookies, "https://app.agentrq.com", Some(&jar));
        forget(webview.clone(), taken).await;
        assert!(signed_in);
        assert!(jar.has("at", &url("https://app.agentrq.com/")));
        assert_eq!(webview.names(), ["_gh_sess"], "the provider's stays");
    }

    #[tokio::test]
    async fn gives_up_on_a_webview_that_does_not_answer() {
        let mut webview = FakeWebview::with(&[("at=a1; Max-Age=600", "127.0.0.1")]);
        webview.delay = Duration::from_millis(500);
        let webview = Arc::new(webview);
        let deadline = Duration::from_millis(20);
        // Late, but read all the same, so the session can still be deleted.
        let (cookies, in_time) = read(webview.clone(), deadline).await;
        assert!(!in_time);
        assert_eq!(cookies.len(), 1);
        assert!(!clear(webview, deadline).await);
    }

    #[tokio::test]
    async fn logging_out_clears_every_cookie_in_the_webview() {
        let webview = Arc::new(FakeWebview::with(&[
            ("at=a1; Path=/", "127.0.0.1"),
            ("_gh_sess=x; Path=/", ".github.com"),
            ("SID=x; Path=/", ".google.com"),
        ]));
        assert!(clear(webview.clone(), COOKIE_DEADLINE).await);
        assert!(webview.names().is_empty());
    }

    #[test]
    fn moves_the_session_into_the_jar_and_hands_back_what_to_delete() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar(&dir);
        let (signed_in, taken) = take_session(
            vec![
                cookie("at=a1; Domain=127.0.0.1; Path=/; HttpOnly; SameSite=Lax; Expires=Wed, 21 Oct 2037 07:28:00 GMT"),
                cookie("rt=r1; Domain=127.0.0.1; Path=/api/v1/auth/refresh; HttpOnly; Max-Age=600"),
                cookie("_ga=x; Domain=github.com; Path=/"),
                cookie("nodomain=x; Path=/"),
            ],
            "http://127.0.0.1:3000",
            Some(&jar),
        );
        assert!(signed_in);
        let names: Vec<&str> = taken.iter().map(|c| c.name()).collect();
        assert_eq!(
            names,
            ["at", "rt"],
            "only the server's, to delete from the webview"
        );
        assert!(jar.has("at", &url("http://127.0.0.1:3000/api/v1/workspaces")));
        assert!(!jar.has("rt", &url("http://127.0.0.1:3000/api/v1/workspaces")));
        assert!(jar.has("rt", &url("http://127.0.0.1:3000/api/v1/auth/refresh")));
        assert!(
            !jar.has("at", &url("http://localhost:3000/")),
            "host-only stays host-only"
        );
    }

    #[test]
    fn keeps_a_parent_domain_cookie_for_the_whole_domain() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar(&dir);
        let (signed_in, taken) = take_session(
            vec![cookie(
                "at=a1; Domain=.agentrq.com; Path=/; Secure; Max-Age=600",
            )],
            "https://app.agentrq.com",
            Some(&jar),
        );
        assert!(signed_in);
        assert_eq!(taken.len(), 1);
        assert!(jar.has("at", &url("https://api.agentrq.com/")));
        assert!(jar.has("at", &url("https://app.agentrq.com/")));
    }

    #[test]
    fn reports_no_session_without_at() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar(&dir);
        let only_refresh = vec![cookie("rt=r1; Domain=127.0.0.1; Path=/; Max-Age=600")];
        let (signed_in, taken) = take_session(only_refresh, "http://127.0.0.1:3000", Some(&jar));
        assert!(!signed_in);
        assert_eq!(taken.len(), 1);

        let at = vec![cookie("at=a1; Domain=127.0.0.1; Max-Age=600")];
        assert_eq!(
            take_session(at.clone(), "not a url", Some(&jar)),
            (false, Vec::new())
        );
        assert_eq!(
            take_session(at, "data:text/plain,x", Some(&jar)),
            (false, Vec::new())
        );
    }

    #[test]
    fn is_not_signed_in_by_an_at_the_jar_refused() {
        let dir = tempfile::tempdir().unwrap();
        let jar = jar(&dir);
        let expired = vec![cookie(
            "at=old; Domain=127.0.0.1; Path=/; Expires=Wed, 21 Oct 2015 07:28:00 GMT",
        )];
        let (signed_in, taken) = take_session(expired, "http://127.0.0.1:3000", Some(&jar));
        assert!(!signed_in);
        assert_eq!(taken.len(), 1, "still deleted from the webview");
    }

    #[test]
    fn describes_a_cookie_for_core() {
        let session = to_core(&cookie("at=a1"));
        assert_eq!(
            session,
            Cookie {
                name: "at".into(),
                value: "a1".into(),
                domain: None,
                path: None,
                expires: None,
            }
        );
        let stored = to_core(&cookie(
            "at=a1; Domain=a.com; Path=/x; Expires=Wed, 21 Oct 2037 07:28:00 GMT",
        ));
        assert_eq!(stored.domain.as_deref(), Some("a.com"));
        assert_eq!(stored.path.as_deref(), Some("/x"));
        assert_eq!(stored.expires, Some(2_139_722_880));
    }
}
