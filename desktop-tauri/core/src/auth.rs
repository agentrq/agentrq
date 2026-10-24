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

/// A cookie as the webview's jar reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Cookie {
    pub name: String,
    /// The domain the cookie is stored for, with or without a leading dot.
    /// `None` cannot be matched to a host, so it never counts.
    pub domain: Option<String>,
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
/// A leading dot (`Domain=.example.com`) is not significant, and the port never
/// is: cookies are not scoped by port. An IP host matches only the identical
/// address, so `1.10` is not a parent of `192.168.1.10`.
pub fn cookie_domain_matches(cookie_domain: &str, host: &str) -> bool {
    let host = bare_host(host);
    if host.is_empty() {
        return false;
    }
    if host.parse::<IpAddr>().is_ok() {
        return bare_host(cookie_domain) == host;
    }
    let domain = bare_host(cookie_domain.trim_start_matches('.'));
    !domain.is_empty() && (host == domain || host.ends_with(&format!(".{domain}")))
}

/// Is the `at` cookie in the jar, for the server's host? A stale cookie of
/// another server does not count, and neither does a failed read.
pub fn has_auth_cookie(jar: &dyn CookieJar, server_url: &str) -> bool {
    let Ok(server) = Url::parse(server_url) else {
        return false;
    };
    let Some(host) = server.host_str() else {
        return false;
    };
    match jar.cookies() {
        Ok(cookies) => cookies.iter().any(|c| {
            c.name == AUTH_COOKIE
                && c.domain
                    .as_deref()
                    .is_some_and(|d| cookie_domain_matches(d, host))
        }),
        Err(_) => false,
    }
}

/// How a sign-in ended.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SignInResult {
    Ok,
    Failed { reason: &'static str },
}

/// One OAuth sign-in. The shell forwards the window's navigations, redirects
/// included, to [`SignInFlow::on_navigation`] and its closing to
/// [`SignInFlow::on_closed`]. The first call to settle it returns the result,
/// and every later call returns `None`, so the shell resolves once.
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
    /// endpoints ends the flow: success if the cookie is in the jar, failure if
    /// not. The window is closed unless it is already gone.
    pub fn on_navigation(
        &mut self,
        url: &str,
        win: &mut dyn SignInWindow,
        jar: &dyn CookieJar,
    ) -> Option<SignInResult> {
        if self.settled || !is_oauth_return(url, &self.server_url) {
            return None;
        }
        self.settled = true;
        let result = if has_auth_cookie(jar, &self.server_url) {
            SignInResult::Ok
        } else {
            SignInResult::Failed {
                reason: REASON_INCOMPLETE,
            }
        };
        if !win.is_destroyed() {
            win.close();
        }
        Some(result)
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
    use crate::links::{classify_link, LinkOptions, LinkTarget};
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
            domain: Some(domain.to_string()),
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

    #[test]
    fn opens_the_flow_in_its_own_window_at_the_start_url() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);
        assert_eq!(
            win.loaded,
            vec![format!("{SERVER}/api/v1/auth/github/login")]
        );

        let result = flow.on_navigation(&format!("{SERVER}/"), &mut win, &FakeJar::signed_in());
        assert_eq!(result, Some(SignInResult::Ok));
    }

    #[test]
    fn succeeds_when_the_cookie_is_in_the_jar_on_return() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let result = flow.on_navigation(&format!("{SERVER}/"), &mut win, &FakeJar::signed_in());

        assert_eq!(result, Some(SignInResult::Ok));
        assert_eq!(win.closes, 1);
    }

    #[test]
    fn fails_when_the_flow_returns_without_a_cookie() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let result = flow.on_navigation(&format!("{SERVER}/"), &mut win, &FakeJar::empty());

        assert_eq!(result, failed("Sign-in did not complete"));
    }

    #[test]
    fn does_not_check_for_a_cookie_while_still_at_the_provider() {
        let jar = FakeJar::signed_in();
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let provider = "https://github.com/login/oauth/authorize";
        assert_eq!(flow.on_navigation(provider, &mut win, &jar), None);
        let callback = format!("{SERVER}/api/v1/auth/github/callback?code=x");
        assert_eq!(flow.on_navigation(&callback, &mut win, &jar), None);
        assert_eq!(jar.lookups.get(), 0);

        let back = format!("{SERVER}/");
        assert_eq!(
            flow.on_navigation(&back, &mut win, &jar),
            Some(SignInResult::Ok)
        );
        assert_eq!(jar.lookups.get(), 1);
    }

    #[test]
    fn resolves_when_the_user_closes_the_window_instead_of_signing_in() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        assert_eq!(flow.on_closed(), failed("Sign-in window was closed"));
    }

    #[test]
    fn settles_once_even_if_more_navigations_arrive() {
        // The window reports closing as part of finishing; a later navigation
        // or close must not settle the flow a second time.
        let jar = FakeJar::signed_in();
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let back = format!("{SERVER}/");
        assert_eq!(
            flow.on_navigation(&back, &mut win, &jar),
            Some(SignInResult::Ok)
        );
        assert_eq!(
            flow.on_navigation(&format!("{SERVER}/events"), &mut win, &jar),
            None
        );
        assert_eq!(flow.on_closed(), None);
        assert_eq!(jar.lookups.get(), 1);
    }

    #[test]
    fn does_not_close_a_window_that_is_already_gone() {
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        win.destroyed = true;
        let result = flow.on_navigation(&format!("{SERVER}/"), &mut win, &FakeJar::signed_in());

        assert_eq!(result, Some(SignInResult::Ok));
        assert_eq!(win.closes, 0);
    }

    #[test]
    fn settles_once_when_two_returns_arrive_back_to_back() {
        // A navigation and the redirect that follows it can both land on the
        // server. The second finds the flow settled: one result, one close, and
        // one cookie lookup (the JS made two).
        let jar = FakeJar::signed_in();
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let first = flow.on_navigation(&format!("{SERVER}/"), &mut win, &jar);
        let second = flow.on_navigation(&format!("{SERVER}/events"), &mut win, &jar);

        assert_eq!(first, Some(SignInResult::Ok));
        assert_eq!(second, None);
        assert_eq!(win.closes, 1);
        assert_eq!(jar.lookups.get(), 1);
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
        assert!(!cookie_domain_matches("localhost", "127.0.0.1"));
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
                name: "at".to_string(),
                domain: None,
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
        assert_eq!(jar.lookups.get(), 0);
    }

    #[test]
    fn fails_the_flow_when_the_jar_cannot_be_read() {
        let jar = FakeJar {
            cookies: Err("jar unavailable".to_string()),
            lookups: Cell::new(0),
        };
        let mut win = FakeWindow::default();
        let mut flow = start(&mut win);

        let result = flow.on_navigation(&format!("{SERVER}/"), &mut win, &jar);

        assert_eq!(result, failed("Sign-in did not complete"));
    }
}
