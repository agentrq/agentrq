// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What a finished sign-in does with its cookies.
//!
//! The flow's decisions are core's `SignInFlow`, and the window is lib.rs's.
//! Electron's sign-in window shares the profile's partition, so the cookie is
//! simply there afterwards. Here the profile's session is the Rust jar, so the
//! sign-in window's cookies are read once it is done and moved into the jar.

use agentrq_desktop_core::auth::{cookie_domain_matches, find_auth_cookie, Cookie};
use cookie_store::RawCookie;
use url::Url;

use crate::cookie_jar::ProfileJar;

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
/// `at` was there, which is what success means, and the cookies taken, for
/// the caller to delete from the webview.
pub fn take_session(
    cookies: Vec<RawCookie<'static>>,
    server_url: &str,
    jar: &ProfileJar,
) -> (bool, Vec<RawCookie<'static>>) {
    let Some((server, host)) = Url::parse(server_url)
        .ok()
        .and_then(|url| Some((url.clone(), url.host_str()?.to_string())))
    else {
        return (false, Vec::new());
    };
    let core: Vec<Cookie> = cookies.iter().map(to_core).collect();
    let signed_in = find_auth_cookie(&core, server_url).is_some();
    let taken: Vec<RawCookie<'static>> = cookies
        .into_iter()
        .filter(|c| c.domain().is_some_and(|d| cookie_domain_matches(d, &host)))
        .collect();
    for cookie in &taken {
        jar.adopt(&for_jar(cookie, &host), &server);
    }
    (signed_in, taken)
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::vault::{random_key, Vault};

    fn jar(dir: &tempfile::TempDir) -> ProfileJar {
        ProfileJar::open(dir.path().join("jar"), Some(Vault::new(&random_key())))
    }

    fn cookie(text: &str) -> RawCookie<'static> {
        RawCookie::parse(text.to_string()).unwrap()
    }

    fn url(text: &str) -> Url {
        Url::parse(text).unwrap()
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
            &jar,
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
            &jar,
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
        let (signed_in, taken) = take_session(only_refresh, "http://127.0.0.1:3000", &jar);
        assert!(!signed_in);
        assert_eq!(taken.len(), 1);

        let at = vec![cookie("at=a1; Domain=127.0.0.1; Max-Age=600")];
        assert_eq!(
            take_session(at.clone(), "not a url", &jar),
            (false, Vec::new())
        );
        assert_eq!(
            take_session(at, "data:text/plain,x", &jar),
            (false, Vec::new())
        );
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
