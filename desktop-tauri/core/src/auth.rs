// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! OAuth sign-in for the desktop app.
//!
//! The login view links to `/api/v1/auth/<provider>/login`. Inside the app that
//! link resolves against the app's own origin, but the provider's redirect chain
//! has to run against the real server for the callback to set a cookie. So the
//! shell takes the navigation over and runs the flow in a dedicated window on
//! the real server, sharing the profile's cookie jar: the `at` cookie the
//! callback sets is then there when the main window reloads.
//!
//! This module holds the decisions: which navigations are a sign-in, when the
//! window is done, whether the cookie is there, and when closing counts as
//! failure. The window and the jar are traits the shell implements. The flow
//! never goes to the system browser; [`crate::links`] keeps it in the app.

use std::net::IpAddr;

use url::Url;

use crate::links::{classify_link, LinkOptions, LinkTarget};
pub use crate::links::{is_auth_path, AUTH_PATH_PREFIX};

/// Providers whose sign-in the shell has to take over.
pub const OAUTH_PROVIDERS: [&str; 2] = ["google", "github"];

/// Name of the session cookie the backend sets on a successful login.
pub const AUTH_COOKIE: &str = "at";

/// Why a flow that ended without a cookie is reported as failed.
pub const REASON_INCOMPLETE: &str = "Sign-in did not complete";
/// Why a flow whose window was closed by hand is reported as failed.
pub const REASON_CLOSED: &str = "Sign-in window was closed";

/// Is this the start of an OAuth sign-in? Returns the provider name.
///
/// `/api/v1/auth/(google|github)/login`, with an optional trailing slash and
/// nothing else: the callback and the root-token endpoint have to go through
/// the normal proxy, and taking them over would break sign-in.
pub fn match_oauth_login(pathname: &str) -> Option<&'static str> {
    let rest = pathname.strip_prefix(AUTH_PATH_PREFIX)?;
    let provider = rest
        .strip_suffix("/login/")
        .or_else(|| rest.strip_suffix("/login"))?;
    OAUTH_PROVIDERS.iter().copied().find(|p| *p == provider)
}

/// Has the OAuth window come back to the application?
///
/// The callback sets the cookie and then redirects on to the app. Landing
/// anywhere on the server that is not still inside the auth endpoints means the
/// round trip finished; success is then decided by whether a cookie exists.
/// Anything unparseable is not a return. Origins compare by scheme, host and
/// port, so a lookalike host or the plain-http twin is not the server.
pub fn is_oauth_return(url: &str, server_url: &str) -> bool {
    let (Ok(target), Ok(server)) = (Url::parse(url), Url::parse(server_url)) else {
        return false;
    };
    target.origin() == server.origin() && !is_auth_path(target.path())
}

/// Where the OAuth window should start: the path the link pointed at, on the
/// real server rather than the app's own origin. `None` if the server URL is
/// not a URL.
///
/// Like the `URL` constructor it replaces, an absolute path replaces any base
/// path, and a `//host` path would replace the host. Pass only a path that
/// [`match_oauth_login`] accepted.
pub fn oauth_start_url(server_url: &str, pathname: &str, search: &str) -> Option<String> {
    let base = Url::parse(&format!("{server_url}/")).ok()?;
    base.join(&format!("{pathname}{search}"))
        .ok()
        .map(Into::into)
}

/// Where the sign-in a main-window navigation to `url` starts, if it is one.
///
/// Two ways in. The login view's Google and GitHub buttons are plain links, so
/// they resolve against the app's own origin (`app://…/api/v1/auth/github/login`)
/// and only the provider logins count there. A link to the configured server's
/// auth endpoints is the other, as [`classify_link`] keeps it in the app. Either
/// way the flow runs on the real server. `None` for anything else, which the
/// link rules handle.
pub fn sign_in_start(url: &str, app_origin: &str, server_url: &str) -> Option<String> {
    let parsed = Url::parse(url).ok()?;
    match classify_link(
        url,
        LinkOptions {
            app_origin,
            server_url,
        },
    ) {
        LinkTarget::App => match_oauth_login(parsed.path()).map(drop)?,
        LinkTarget::SignIn => {}
        _ => return None,
    }
    // `URL.search`: empty for no query and for a bare `?`.
    let search = parsed
        .query()
        .filter(|query| !query.is_empty())
        .map(|query| format!("?{query}"))
        .unwrap_or_default();
    oauth_start_url(server_url, parsed.path(), &search)
}

/// A cookie as the webview's jar reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cookie {
    pub name: String,
    pub value: String,
    /// The domain the cookie is stored for. wry hands over a `cookie::Cookie`,
    /// whose `domain()` strips the leading dot, so a host-only cookie and a
    /// `Domain=` cookie look the same: a host-only `at` of a parent host counts
    /// for a subdomain server. That is safe only because the shell deletes `at`
    /// from the webview's store as soon as it has copied it.
    /// `None` cannot be matched to a host, so it never counts.
    pub domain: Option<String>,
    pub path: Option<String>,
    /// Expiry in seconds since the Unix epoch. `None` is a session cookie.
    pub expires: Option<i64>,
}

/// The profile's cookie jar. A failed read is an `Err`, and counts as no cookie.
pub trait CookieJar {
    /// Every cookie in the jar. Not `cookies_for_url`: on macOS that needs an
    /// exact domain match and misses a `Domain=.example.com` cookie.
    fn cookies(&self) -> Result<Vec<Cookie>, String>;
}

/// The sign-in window.
pub trait SignInWindow {
    fn load_url(&mut self, url: &str);
    fn is_destroyed(&self) -> bool;
    fn close(&mut self);
}

fn bare_host(host: &str) -> String {
    host.trim_start_matches('[')
        .trim_end_matches(']')
        .to_ascii_lowercase()
}

/// Would a browser send a cookie stored for `cookie_domain` to `host`?
///
/// RFC 6265 domain-match: the same host, or a subdomain of the cookie's domain.
/// One leading dot (`Domain=.example.com`) is accepted and ignored, and the port
/// never matters: cookies are not scoped by port. An IP host matches only the
/// same address, so `1.10` is not a parent of `192.168.1.10`, and `0:0::1` is
/// `[::1]`. A single-label domain (`.com`) matches only itself, not its
/// subdomains; deeper public suffixes are left to the store, which refuses them.
pub fn cookie_domain_matches(cookie_domain: &str, host: &str) -> bool {
    let host = bare_host(host);
    if host.is_empty() {
        return false;
    }
    if let Ok(ip) = host.parse::<IpAddr>() {
        return bare_host(cookie_domain).parse::<IpAddr>().ok() == Some(ip);
    }
    let domain = bare_host(cookie_domain.strip_prefix('.').unwrap_or(cookie_domain));
    host == domain || (domain.contains('.') && host.ends_with(&format!(".{domain}")))
}

/// The `at` cookie for the server's host, if the jar holds one. When several
/// match (a stale `.example.com` one beside a fresh `app.example.com` one), the
/// most specific wins: the longest domain, then the longest path. A cookie of
/// another server does not count.
pub fn find_auth_cookie<'a>(cookies: &'a [Cookie], server_url: &str) -> Option<&'a Cookie> {
    let server = Url::parse(server_url).ok()?;
    let host = server.host_str()?;
    cookies
        .iter()
        .filter(|c| {
            c.name == AUTH_COOKIE
                && c.domain
                    .as_deref()
                    .is_some_and(|d| cookie_domain_matches(d, host))
        })
        .max_by_key(|c| {
            let domain = c.domain.as_deref().unwrap_or_default();
            (
                domain.strip_prefix('.').unwrap_or(domain).len(),
                c.path.as_deref().map_or(0, str::len),
            )
        })
}

/// Is the `at` cookie in the jar, for the server's host? A failed read counts
/// as no cookie.
///
/// Never call this from a navigation or page-load handler: on Windows,
/// reading the webview's jar there deadlocks (wry#583). See [`SignInFlow`].
pub fn has_auth_cookie(jar: &dyn CookieJar, server_url: &str) -> bool {
    jar.cookies()
        .is_ok_and(|cookies| find_auth_cookie(&cookies, server_url).is_some())
}

/// How a sign-in ended.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SignInResult {
    Ok,
    Failed { reason: &'static str },
}

/// One OAuth sign-in, in two steps so the cookie jar is never read inside a
/// webview callback, where it deadlocks on Windows (wry#583).
///
/// The shell forwards the window's navigations, redirects included, to
/// [`SignInFlow::on_navigation`]. When that returns `true`, the shell reads the
/// jar on another thread and hands the answer to [`SignInFlow::finish`], which
/// closes the window. The window's closing goes to [`SignInFlow::on_closed`].
/// The flow settles once: before the jar is read, so the jar is read once and
/// a close that arrives during the read does not count as giving up.
#[derive(Debug)]
pub struct SignInFlow {
    server_url: String,
    settled: bool,
}

impl SignInFlow {
    /// Open the flow in `win` at `start_url`.
    pub fn start(server_url: &str, start_url: &str, win: &mut dyn SignInWindow) -> Self {
        win.load_url(start_url);
        Self {
            server_url: server_url.to_string(),
            settled: false,
        }
    }

    /// The window navigated to `url`. Back on the server outside the auth
    /// endpoints ends the flow: returns `true`, once, meaning "read the jar
    /// now, off this thread, then call [`SignInFlow::finish`]".
    pub fn on_navigation(&mut self, url: &str) -> bool {
        if self.settled || !is_oauth_return(url, &self.server_url) {
            return false;
        }
        self.settled = true;
        true
    }

    /// The jar has been read: success if it held the cookie. The window is
    /// closed unless it is already gone.
    pub fn finish(&mut self, has_cookie: bool, win: &mut dyn SignInWindow) -> SignInResult {
        if !win.is_destroyed() {
            win.close();
        }
        if has_cookie {
            SignInResult::Ok
        } else {
            SignInResult::Failed {
                reason: REASON_INCOMPLETE,
            }
        }
    }

    /// The window closed. Before the flow ended, that is the user giving up.
    pub fn on_closed(&mut self) -> Option<SignInResult> {
        if self.settled {
            return None;
        }
        self.settled = true;
        Some(SignInResult::Failed {
            reason: REASON_CLOSED,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;

    const SERVER: &str = "https://agentrq.example.com";

    #[derive(Default)]
    struct FakeWindow {
        loaded: Vec<String>,
        destroyed: bool,
        closes: u32,
    }

    impl SignInWindow for FakeWindow {
        fn load_url(&mut self, url: &str) {
            self.loaded.push(url.to_string());
        }
        fn is_destroyed(&self) -> bool {
            self.destroyed
        }
        fn close(&mut self) {
            self.closes += 1;
            self.destroyed = true;
        }
    }

    struct FakeJar {
        cookies: Result<Vec<Cookie>, String>,
        lookups: Cell<u32>,
    }

    impl FakeJar {
        fn with(cookies: Vec<Cookie>) -> Self {
            Self {
                cookies: Ok(cookies),
                lookups: Cell::new(0),
            }
        }
        fn signed_in() -> Self {
            Self::with(vec![cookie("at", "agentrq.example.com")])
        }
        fn empty() -> Self {
            Self::with(vec![])
        }
    }

    impl CookieJar for FakeJar {
        fn cookies(&self) -> Result<Vec<Cookie>, String> {
            self.lookups.set(self.lookups.get() + 1);
            self.cookies.clone()
        }
    }

    fn cookie(name: &str, domain: &str) -> Cookie {
        Cookie {
            name: name.to_string(),
            value: format!("token-for-{domain}"),
            domain: Some(domain.to_string()),
            path: Some("/".to_string()),
            expires: None,
        }
    }

    fn start(win: &mut FakeWindow) -> SignInFlow {
        SignInFlow::start(SERVER, &format!("{SERVER}/api/v1/auth/github/login"), win)
    }

    fn failed(reason: &'static str) -> Option<SignInResult> {
        Some(SignInResult::Failed { reason })
    }

    // --- match_oauth_login ---

    #[test]
    fn matches_the_providers_the_login_view_links_to() {
        assert_eq!(
            match_oauth_login("/api/v1/auth/google/login"),
            Some("google")
        );
        assert_eq!(
            match_oauth_login("/api/v1/auth/github/login"),
            Some("github")
        );
    }

    #[test]
    fn tolerates_a_trailing_slash() {
        assert_eq!(
            match_oauth_login("/api/v1/auth/github/login/"),
            Some("github")
        );
    }

    #[test]
    fn ignores_every_other_auth_path() {
        // The callback and the root-token endpoint must go through the normal
        // proxy; taking those over would break sign-in rather than fix it.
        assert_eq!(match_oauth_login("/api/v1/auth/github/callback"), None);
        assert_eq!(match_oauth_login("/api/v1/auth/root/login"), None);
        assert_eq!(match_oauth_login("/api/v1/auth/user"), None);
    }

    #[test]
    fn ignores_an_unknown_provider_and_near_misses() {
        assert_eq!(match_oauth_login("/api/v1/auth/gitlab/login"), None);
        assert_eq!(match_oauth_login("/api/v1/auth/github/login/extra"), None);
        assert_eq!(match_oauth_login("/api/v1/auth/github/login//"), None);
        assert_eq!(match_oauth_login("/prefix/api/v1/auth/github/login"), None);
        assert_eq!(match_oauth_login("/"), None);
    }

    #[test]
    fn exports_the_providers_it_recognises() {
        assert_eq!(OAUTH_PROVIDERS, ["google", "github"]);
    }

    // --- is_auth_path ---

    #[test]
    fn covers_the_whole_sign_in_round_trip_not_just_the_login_links() {
        assert!(is_auth_path("/api/v1/auth/google/login"));
        assert!(is_auth_path("/api/v1/auth/github/callback"));
        assert!(is_auth_path("/api/v1/auth/root/login"));
    }

    #[test]
    fn is_not_satisfied_by_a_path_that_merely_contains_it() {
        assert!(!is_auth_path("/tasks/123"));
        assert!(!is_auth_path("/prefix/api/v1/auth/google/login"));
        assert!(!is_auth_path("/api/v1/authorize"));
        assert!(!is_auth_path(""));
    }

    // --- is_oauth_return ---

    #[test]
    fn returns_once_the_flow_lands_back_on_the_app_itself() {
        assert!(is_oauth_return(&format!("{SERVER}/"), SERVER));
        assert!(is_oauth_return(&format!("{SERVER}/workspaces/abc"), SERVER));
    }

    #[test]
    fn does_not_return_while_still_inside_the_auth_endpoints() {
        assert!(!is_oauth_return(
            &format!("{SERVER}/api/v1/auth/github/callback?code=x"),
            SERVER
        ));
        assert!(!is_oauth_return(
            &format!("{SERVER}/api/v1/auth/github/login"),
            SERVER
        ));
    }

    #[test]
    fn does_not_return_while_on_the_identity_provider() {
        assert!(!is_oauth_return(
            "https://github.com/login/oauth/authorize?x=1",
            SERVER
        ));
        assert!(!is_oauth_return(
            "https://accounts.google.com/o/oauth2/auth",
            SERVER
        ));
    }

    #[test]
    fn does_not_confuse_a_lookalike_host() {
        assert!(!is_oauth_return(
            "https://agentrq.example.com.evil.test/",
            SERVER
        ));
        assert!(!is_oauth_return("http://agentrq.example.com/", SERVER));
        assert!(!is_oauth_return(
            "https://agentrq.example.com:8443/",
            SERVER
        ));
    }

    #[test]
    fn does_not_return_for_anything_unparseable() {
        assert!(!is_oauth_return("not a url", SERVER));
        assert!(!is_oauth_return(&format!("{SERVER}/"), "not a url"));
    }

    // --- oauth_start_url ---

    #[test]
    fn rebuilds_the_link_against_the_real_server_origin() {
        assert_eq!(
            oauth_start_url(SERVER, "/api/v1/auth/github/login", "").as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login")
        );
    }

    #[test]
    fn keeps_the_query_string() {
        assert_eq!(
            oauth_start_url(SERVER, "/api/v1/auth/google/login", "?next=%2Fevents").as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/google/login?next=%2Fevents")
        );
    }

    #[test]
    fn respects_a_server_mounted_under_a_base_path() {
        // An absolute path replaces the base path, as the URL constructor does.
        assert_eq!(
            oauth_start_url(
                "https://example.com/agentrq",
                "/api/v1/auth/github/login",
                ""
            )
            .as_deref(),
            Some("https://example.com/api/v1/auth/github/login")
        );
    }

    #[test]
    fn has_no_start_url_for_a_server_that_is_not_a_url() {
        assert_eq!(
            oauth_start_url("not a url", "/api/v1/auth/x/login", ""),
            None
        );
    }

    #[test]
    fn keeps_the_start_url_in_the_app_not_the_system_browser() {
        let start = oauth_start_url(SERVER, "/api/v1/auth/google/login", "").unwrap();
        let target = classify_link(
            &start,
            LinkOptions {
                app_origin: "app://agentrq",
                server_url: SERVER,
            },
        );
        assert_eq!(target, LinkTarget::SignIn);
    }

    // --- the flow ---

    /// What the shell does once `on_navigation` says so: read the jar (off the
    /// webview's thread, in the real app) and finish.
    fn read_and_finish(flow: &mut SignInFlow, jar: &FakeJar, win: &mut FakeWindow) -> SignInResult {
        flow.finish(has_auth_cookie(jar, SERVER), win)
    }

    #[test]
    fn opens_the_flow_in_its_own_window_at_the_start_url() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);
        assert_eq!(
            win.loaded,
            vec![format!("{SERVER}/api/v1/auth/github/login")]
        );

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        let result = read_and_finish(&mut flow, &FakeJar::signed_in(), &mut win);
        assert_eq!(result, SignInResult::Ok);
    }

    #[test]
    fn succeeds_when_the_cookie_is_in_the_jar_on_return() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        let result = read_and_finish(&mut flow, &FakeJar::signed_in(), &mut win);

        assert_eq!(result, SignInResult::Ok);
        assert_eq!(win.closes, 1);
    }

    #[test]
    fn fails_when_the_flow_returns_without_a_cookie() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        let result = read_and_finish(&mut flow, &FakeJar::empty(), &mut win);

        assert_eq!(
            result,
            SignInResult::Failed {
                reason: "Sign-in did not complete"
            }
        );
        assert_eq!(win.closes, 1);
    }

    #[test]
    fn does_not_check_for_a_cookie_while_still_at_the_provider() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(!flow.on_navigation("https://github.com/login/oauth/authorize"));
        assert!(!flow.on_navigation(&format!("{SERVER}/api/v1/auth/github/callback?code=x")));
        assert_eq!(win.closes, 0);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
    }

    #[test]
    fn resolves_when_the_user_closes_the_window_instead_of_signing_in() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert_eq!(flow.on_closed(), failed("Sign-in window was closed"));
        // Closed is the end: a later return does not ask for the jar.
        assert!(!flow.on_navigation(&format!("{SERVER}/")));
    }

    #[test]
    fn settles_once_even_if_more_navigations_arrive() {
        // The window reports closing as part of finishing; a later navigation
        // or close must not settle the flow a second time.
        let jar = FakeJar::signed_in();
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        assert_eq!(read_and_finish(&mut flow, &jar, &mut win), SignInResult::Ok);
        assert!(!flow.on_navigation(&format!("{SERVER}/events")));
        assert_eq!(flow.on_closed(), None);
        assert_eq!(jar.lookups.get(), 1);
    }

    #[test]
    fn does_not_close_a_window_that_is_already_gone() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        win.destroyed = true;
        let result = read_and_finish(&mut flow, &FakeJar::signed_in(), &mut win);

        assert_eq!(result, SignInResult::Ok);
        assert_eq!(win.closes, 0);
    }

    #[test]
    fn settles_once_when_two_returns_arrive_back_to_back() {
        // A navigation and the redirect that follows it can both land on the
        // server while the jar is being read. Only the first asks for a read:
        // one result, one close, one cookie lookup (the JS made two).
        let jar = FakeJar::signed_in();
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        assert!(!flow.on_navigation(&format!("{SERVER}/events")));
        let result = read_and_finish(&mut flow, &jar, &mut win);

        assert_eq!(result, SignInResult::Ok);
        assert_eq!(win.closes, 1);
        assert_eq!(jar.lookups.get(), 1);
    }

    #[test]
    fn finishes_when_the_window_closes_while_the_jar_is_read() {
        // The user closes the window after the return, before the read ends:
        // that is not giving up, and the cookie decides.
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        win.destroyed = true;
        assert_eq!(flow.on_closed(), None);
        let result = read_and_finish(&mut flow, &FakeJar::signed_in(), &mut win);

        assert_eq!(result, SignInResult::Ok);
        assert_eq!(win.closes, 0);
    }

    #[test]
    fn names_the_cookie_the_backend_actually_sets() {
        assert_eq!(AUTH_COOKIE, "at");
    }

    // --- the cookie rule (new: the JS matched by name in the profile's session) ---

    #[test]
    fn matches_a_cookie_set_with_a_leading_dot_domain() {
        assert!(cookie_domain_matches(".example.com", "example.com"));
        assert!(cookie_domain_matches(".example.com", "app.example.com"));
        assert!(cookie_domain_matches("example.com", "app.example.com"));
        assert!(cookie_domain_matches(".Example.COM", "APP.example.com"));
    }

    #[test]
    fn does_not_match_a_sibling_or_lookalike_domain() {
        assert!(!cookie_domain_matches("app.example.com", "example.com"));
        assert!(!cookie_domain_matches("ample.com", "example.com"));
        assert!(!cookie_domain_matches(
            "example.com",
            "example.com.evil.test"
        ));
        assert!(!cookie_domain_matches("evil.com", "example.com"));
        assert!(!cookie_domain_matches("", "example.com"));
        assert!(!cookie_domain_matches(".", "example.com"));
        assert!(!cookie_domain_matches("example.com", ""));
    }

    #[test]
    fn matches_an_ip_host_only_by_the_identical_address() {
        assert!(cookie_domain_matches("192.168.1.10", "192.168.1.10"));
        assert!(!cookie_domain_matches("1.10", "192.168.1.10"));
        assert!(!cookie_domain_matches(".168.1.10", "192.168.1.10"));
        assert!(!cookie_domain_matches(".192.168.1.10", "192.168.1.10"));
        assert!(cookie_domain_matches("[::1]", "[::1]"));
        assert!(cookie_domain_matches("::1", "[::1]"));
        assert!(cookie_domain_matches("0:0::1", "[::1]"));
        assert!(cookie_domain_matches("[0:0::1]", "::1"));
        assert!(!cookie_domain_matches("::2", "[::1]"));
        assert!(!cookie_domain_matches("localhost", "127.0.0.1"));
    }

    #[test]
    fn strips_one_leading_dot_and_never_matches_below_a_single_label() {
        assert!(!cookie_domain_matches("..example.com", "example.com"));
        assert!(!cookie_domain_matches("..example.com", "app.example.com"));
        assert!(!cookie_domain_matches(".com", "example.com"));
        assert!(!cookie_domain_matches("com", "example.com"));
        // A single-label host still matches itself: the backend sets a
        // host-only cookie on localhost.
        assert!(cookie_domain_matches("localhost", "localhost"));
        assert!(cookie_domain_matches(".localhost", "localhost"));
        assert!(!cookie_domain_matches("localhost", "app.localhost"));
    }

    #[test]
    fn finds_the_cookie_for_a_server_with_a_port() {
        // Cookies are not scoped by port: the host is what is compared.
        let jar = FakeJar::with(vec![cookie("at", "localhost")]);
        assert!(has_auth_cookie(&jar, "http://localhost:3000"));
        let jar = FakeJar::with(vec![cookie("at", "192.168.1.10")]);
        assert!(has_auth_cookie(&jar, "http://192.168.1.10:3000"));
        let jar = FakeJar::with(vec![cookie("at", "::1")]);
        assert!(has_auth_cookie(&jar, "http://[::1]:3000"));
    }

    #[test]
    fn finds_a_parent_domain_cookie_for_the_servers_host() {
        let jar = FakeJar::with(vec![cookie("at", ".example.com")]);
        assert!(has_auth_cookie(&jar, "https://app.example.com"));
    }

    #[test]
    fn ignores_a_cookie_of_another_name_or_server_or_no_domain() {
        let jar = FakeJar::with(vec![
            cookie("session", "agentrq.example.com"),
            cookie("at", "other.example.com"),
            Cookie {
                domain: None,
                ..cookie("at", "agentrq.example.com")
            },
        ]);
        assert!(!has_auth_cookie(&jar, SERVER));
    }

    #[test]
    fn counts_an_unreadable_jar_or_an_unusable_server_as_no_cookie() {
        let broken = FakeJar {
            cookies: Err("jar unavailable".to_string()),
            lookups: Cell::new(0),
        };
        assert!(!has_auth_cookie(&broken, SERVER));
        assert_eq!(broken.lookups.get(), 1);

        let jar = FakeJar::signed_in();
        assert!(!has_auth_cookie(&jar, "not a url"));
        assert!(!has_auth_cookie(&jar, "mailto:someone@example.com"));
    }

    #[test]
    fn fails_the_flow_when_the_jar_cannot_be_read() {
        let jar = FakeJar {
            cookies: Err("jar unavailable".to_string()),
            lookups: Cell::new(0),
        };
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert!(flow.on_navigation(&format!("{SERVER}/")));
        let result = read_and_finish(&mut flow, &jar, &mut win);

        assert_eq!(
            result,
            SignInResult::Failed {
                reason: "Sign-in did not complete"
            }
        );
    }

    // --- find_auth_cookie ---

    #[test]
    fn picks_the_most_specific_of_two_matching_cookies() {
        // A stale parent-domain cookie beside the fresh one the server just set:
        // the fresh one is the one to copy, in either order.
        let stale = cookie("at", "example.com");
        let fresh = cookie("at", "agentrq.example.com");
        for jar in [
            vec![stale.clone(), fresh.clone()],
            vec![fresh.clone(), stale.clone()],
        ] {
            assert_eq!(find_auth_cookie(&jar, SERVER), Some(&fresh));
        }
    }

    #[test]
    fn breaks_a_domain_tie_by_the_longest_path() {
        let root = cookie("at", "agentrq.example.com");
        let api = Cookie {
            path: Some("/api".to_string()),
            ..root.clone()
        };
        let no_path = Cookie {
            path: None,
            ..root.clone()
        };
        let jar = [no_path, api.clone(), root];
        assert_eq!(find_auth_cookie(&jar, SERVER), Some(&api));
    }

    #[test]
    fn ignores_a_leading_dot_when_ranking_domains() {
        let dotted = cookie("at", ".agentrq.example.com");
        let parent = cookie("at", "example.com");
        let jar = [parent, dotted.clone()];
        assert_eq!(find_auth_cookie(&jar, SERVER), Some(&dotted));
    }

    #[test]
    fn hands_over_the_value_path_and_expiry_to_copy() {
        let at = Cookie {
            name: "at".to_string(),
            value: "secret".to_string(),
            domain: Some("agentrq.example.com".to_string()),
            path: Some("/".to_string()),
            expires: Some(1_800_000_000),
        };
        let jar = [cookie("session", "agentrq.example.com"), at];
        let found = find_auth_cookie(&jar, SERVER).expect("the at cookie");
        assert_eq!(found.value, "secret");
        assert_eq!(found.path.as_deref(), Some("/"));
        assert_eq!(found.expires, Some(1_800_000_000));
    }

    #[test]
    fn finds_nothing_for_another_server_or_a_server_that_is_not_a_url() {
        let jar = [cookie("at", "other.example.com")];
        assert_eq!(find_auth_cookie(&jar, SERVER), None);
        let jar = [cookie("at", "agentrq.example.com")];
        assert_eq!(find_auth_cookie(&jar, "not a url"), None);
        assert_eq!(find_auth_cookie(&jar, "mailto:someone@example.com"), None);
    }

    // sign_in_start (Tauri only: Electron splits it between will-navigate and routeLink)

    #[test]
    fn starts_a_sign_in_from_the_login_buttons_on_the_real_server() {
        const APP: &str = "app://localhost";
        assert_eq!(
            sign_in_start("app://localhost/api/v1/auth/github/login", APP, SERVER).as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login")
        );
        assert_eq!(
            sign_in_start(
                "app://localhost/api/v1/auth/google/login/?next=%2Fx",
                APP,
                SERVER
            )
            .as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/google/login/?next=%2Fx")
        );
        assert_eq!(
            sign_in_start("app://localhost/api/v1/auth/github/login?", APP, SERVER).as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login"),
            "a bare ? is no query, as in URL.search"
        );
        // WebView2 spells the app's origin over http.
        assert_eq!(
            sign_in_start(
                "http://app.localhost/api/v1/auth/github/login",
                "http://app.localhost",
                SERVER
            )
            .as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login")
        );
        // On macOS before 14, each profile has an origin of its own.
        assert_eq!(
            sign_in_start("app://work/api/v1/auth/github/login", "app://work", SERVER).as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login")
        );
    }

    #[test]
    fn leaves_every_other_app_route_to_the_app() {
        const APP: &str = "app://localhost";
        for url in [
            "app://localhost/",
            "app://localhost/login",
            "app://localhost/api/v1/auth/github/callback?code=x",
            "app://localhost/api/v1/auth/root",
            "app://localhost/api/v1/auth/gitlab/login",
            "not a url",
        ] {
            assert_eq!(sign_in_start(url, APP, SERVER), None, "{url}");
        }
        assert_eq!(
            sign_in_start("app://localhost/api/v1/auth/github/login", APP, ""),
            None,
            "no server, nowhere to sign in"
        );
    }

    #[test]
    fn keeps_a_link_to_the_servers_sign_in_in_the_app() {
        const APP: &str = "app://localhost";
        assert_eq!(
            sign_in_start(
                "https://agentrq.example.com/api/v1/auth/github/login?x=1",
                APP,
                SERVER
            )
            .as_deref(),
            Some("https://agentrq.example.com/api/v1/auth/github/login?x=1")
        );
        for url in [
            "https://agentrq.example.com/tasks",
            "https://evil.example.com/api/v1/auth/github/login",
            "http://agentrq.example.com/api/v1/auth/github/login",
            "mailto:a@b.c",
            "javascript:alert(1)",
        ] {
            assert_eq!(sign_in_start(url, APP, SERVER), None, "{url}");
        }
    }
}
