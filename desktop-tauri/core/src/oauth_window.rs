// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The supervisor's authorisation window: Electron's `openAuthorizationWindow`
//! (`desktop/src/main/index.js`), whose page ends at a redirect the app reads
//! instead of following.
//!
//! The redirect, `agentrq://extensions/authorized` for `supervisor-auth.js`, is
//! Electron's scheme, not this app's (`agentrq-tauri`): followed, it would hand
//! the code to the Electron app, or to whatever else claims `agentrq`. So it is
//! cancelled, and the flow ends with its URL.
//! Electron matched it as a prefix; here scheme, userinfo, host, port and path
//! must all be equal, and only the query and fragment may differ.

use std::fmt;
use std::time::Duration;

use url::Url;

use crate::profiles::data_store_id;
use crate::side_panel::child::GuestStore;

/// How long the window waits for an answer. Electron waits for ever; a
/// sign-in with a second factor takes minutes, never ten.
pub const DEADLINE: Duration = Duration::from_secs(10 * 60);

/// Why there is no redirect to hand back.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OAuthError {
    /// The authorisation page is not on the web, or a URL does not parse.
    BadUrl,
    /// Another authorisation window is already open; it is brought forward.
    Busy,
    /// The person closed the window, which is declining (Electron's `null`).
    Closed,
    /// Nothing came back within [`DEADLINE`].
    TimedOut,
    /// The window could not be built.
    Window(String),
}

impl fmt::Display for OAuthError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::BadUrl => f.write_str("That is not an address the authorisation window opens."),
            Self::Busy => f.write_str("An authorisation window is already open."),
            // `supervisor-auth.js`'s own words for a window closed.
            Self::Closed => f.write_str("The authorisation window was closed."),
            Self::TimedOut => {
                f.write_str("The authorisation window was open too long, so it was closed.")
            }
            Self::Window(error) => write!(f, "The authorisation window could not open: {error}"),
        }
    }
}

/// Whether `url` is the redirect: everything but the query and fragment equal.
/// A host is compared without case, a port with its scheme's default filled in.
pub fn is_redirect(url: &Url, redirect: &Url) -> bool {
    url.scheme() == redirect.scheme()
        && url.username() == redirect.username()
        && url.password() == redirect.password()
        && url.host_str().map(str::to_ascii_lowercase)
            == redirect.host_str().map(str::to_ascii_lowercase)
        && url.port_or_known_default() == redirect.port_or_known_default()
        && url.path() == redirect.path()
}

/// A web page, and not one of WebView2's own `*.localhost` origins, which are
/// the app's.
fn is_web(url: &Url) -> bool {
    matches!(url.scheme(), "http" | "https")
        && !url
            .host_str()
            .is_some_and(|host| host.trim_end_matches('.').ends_with(".localhost"))
}

/// What the window may load besides the redirect: the web, and the blank page
/// it is built on. Any other scheme would go to the OS.
///
/// Subframes are asked too, on every OS, so an iframe on `data:`, `blob:` or
/// `about:srcdoc` is refused like any other scheme: a provider's widget that
/// needs one does not show.
fn is_loadable(url: &Url) -> bool {
    is_web(url) || url.as_str() == "about:blank"
}

/// What a navigation does.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Step {
    /// Whether the webview goes on with it.
    pub load: bool,
    /// How the flow ended, when this navigation ended it.
    pub ended: Option<Result<Url, OAuthError>>,
}

/// One authorisation, from its page to the first of the redirect, a close or
/// the deadline. Once it has ended nothing more loads: the window is going.
#[derive(Debug)]
pub struct Flow {
    page: Url,
    redirect: Url,
    ended: bool,
}

impl Flow {
    pub fn new(auth_url: &str, redirect_uri: &str) -> Result<Self, OAuthError> {
        let page = Url::parse(auth_url).map_err(|_| OAuthError::BadUrl)?;
        let redirect = Url::parse(redirect_uri).map_err(|_| OAuthError::BadUrl)?;
        if !is_web(&page) {
            return Err(OAuthError::BadUrl);
        }
        Ok(Self {
            page,
            redirect,
            ended: false,
        })
    }

    /// The authorisation page, to load once the window is ready.
    pub fn page(&self) -> &Url {
        &self.page
    }

    /// The host whose cookies the window is lent, and which are taken back.
    pub fn host(&self) -> &str {
        self.page.host_str().unwrap_or_default()
    }

    /// Asked before the webview follows `url`, a link or a server's redirect.
    pub fn navigate(&mut self, url: &Url) -> Step {
        if self.ended {
            return Step {
                load: false,
                ended: None,
            };
        }
        if is_redirect(url, &self.redirect) {
            self.ended = true;
            return Step {
                load: false,
                ended: Some(Ok(url.clone())),
            };
        }
        Step {
            load: is_loadable(url),
            ended: None,
        }
    }

    /// Ends it for `why`, unless it has already ended.
    pub fn end(&mut self, why: OAuthError) -> Option<Result<Url, OAuthError>> {
        if std::mem::replace(&mut self.ended, true) {
            return None;
        }
        Some(Err(why))
    }
}

/// The window's store on macOS `major`: the profile's own from 14, where wry
/// honours a named store, and one never written to disk below, where wry
/// would hand it the app's default store.
pub fn macos_store(profile_id: &str, major: isize) -> GuestStore {
    if major >= 14 {
        GuestStore::Named(data_store_id(profile_id))
    } else {
        GuestStore::Ephemeral
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const REDIRECT: &str = "agentrq://extensions/authorized";
    const PAGE: &str = "https://agentrq.example/mcp/oauth2/authorize?client_id=c";

    fn url(text: &str) -> Url {
        Url::parse(text).unwrap()
    }

    fn redirect(text: &str) -> bool {
        is_redirect(&url(text), &url(REDIRECT))
    }

    fn flow() -> Flow {
        Flow::new(PAGE, REDIRECT).unwrap()
    }

    #[test]
    fn the_redirect_matches_whatever_its_query_and_fragment() {
        assert!(redirect(REDIRECT));
        assert!(redirect("agentrq://extensions/authorized?code=c&state=s"));
        assert!(redirect(
            "agentrq://extensions/authorized?error=access_denied#x"
        ));
        assert!(redirect("agentrq://EXTENSIONS/authorized?code=c"));
    }

    #[test]
    fn anything_else_is_not_the_redirect() {
        for other in [
            // Electron's prefix match took these.
            "agentrq://extensions/authorizedX?code=c",
            "agentrq://extensions/authorized/more?code=c",
            "agentrq://extensions/authorize?code=c",
            "agentrq://extensions.evil/authorized?code=c",
            "agentrq://evil/authorized?code=c",
            "agentrq://good@extensions/authorized?code=c",
            "agentrq://extensions:1/authorized?code=c",
            "agentrqx://extensions/authorized?code=c",
            "https://extensions/authorized?code=c",
        ] {
            assert!(!redirect(other), "{other}");
        }
    }

    #[test]
    fn an_http_redirect_must_match_its_scheme_port_and_userinfo() {
        let cb = url("https://good.example/cb");
        let is = |text: &str| is_redirect(&url(text), &cb);
        assert!(is("https://good.example/cb?code=c"));
        assert!(is("https://good.example:443/cb?code=c"));
        assert!(is("https://GOOD.example/cb"));
        for other in [
            "http://good.example/cb?code=c",
            "https://good.example:8443/cb?code=c",
            "https://good.example/cb/?code=c",
            "https://good.example/c?code=c",
            "https://good.example/cbx?code=c",
            "https://good.example.evil/cb?code=c",
            "https://good.example@evil.example/cb?code=c",
            "https://user:pw@good.example/cb?code=c",
            "https://evil.example/cb?next=https://good.example/cb",
        ] {
            assert!(!is(other), "{other}");
        }
    }

    #[test]
    fn opens_only_a_web_page_and_any_redirect_uri() {
        assert_eq!(
            Flow::new("javascript:alert(1)", REDIRECT).unwrap_err(),
            OAuthError::BadUrl
        );
        assert_eq!(
            Flow::new("file:///etc/passwd", REDIRECT).unwrap_err(),
            OAuthError::BadUrl
        );
        assert_eq!(
            Flow::new("not a url", REDIRECT).unwrap_err(),
            OAuthError::BadUrl
        );
        assert_eq!(
            Flow::new(PAGE, "not a url").unwrap_err(),
            OAuthError::BadUrl
        );
        assert_eq!(
            Flow::new("http://tauri.localhost/", REDIRECT).unwrap_err(),
            OAuthError::BadUrl
        );
        let dev = Flow::new("http://localhost:3000/authorize", REDIRECT).unwrap();
        assert_eq!(dev.host(), "localhost");
        let local = Flow::new("http://127.0.0.1:3000/authorize", REDIRECT).unwrap();
        assert_eq!(local.page().as_str(), "http://127.0.0.1:3000/authorize");
        assert_eq!(local.host(), "127.0.0.1");
        assert_eq!(flow().host(), "agentrq.example");
    }

    #[test]
    fn the_redirect_is_cancelled_before_it_loads_and_ends_the_flow() {
        let mut flow = flow();
        let landed = url("agentrq://extensions/authorized?code=c&state=s");
        assert_eq!(
            flow.navigate(&landed),
            Step {
                load: false,
                ended: Some(Ok(landed.clone())),
            }
        );
        // The window is closing: nothing loads, and nothing ends it twice.
        for next in [PAGE, "https://agentrq.example/", REDIRECT] {
            assert_eq!(
                flow.navigate(&url(next)),
                Step {
                    load: false,
                    ended: None,
                }
            );
        }
        assert_eq!(flow.end(OAuthError::Closed), None);
    }

    #[test]
    fn loads_the_web_and_the_blank_page_and_refuses_other_schemes() {
        let mut flow = flow();
        let loads = |flow: &mut Flow, text: &str| flow.navigate(&url(text));
        for web in [
            PAGE,
            "http://agentrq.example/login",
            "https://github.com/login",
            "http://localhost:3000/login",
            "about:blank",
        ] {
            assert_eq!(
                loads(&mut flow, web),
                Step {
                    load: true,
                    ended: None,
                },
                "{web}"
            );
        }
        for other in [
            "agentrq://open?route=/x",
            "agentrq://extensions/authorizedX?code=c",
            "javascript:alert(1)",
            "file:///etc/passwd",
            "data:text/html,x",
            "about:srcdoc",
            "mailto:a@b.example",
            // WebView2's origins for the app's own pages.
            "http://tauri.localhost/",
            "https://app.localhost/",
            "http://TAURI.LOCALHOST./x",
        ] {
            assert_eq!(
                loads(&mut flow, other),
                Step {
                    load: false,
                    ended: None,
                },
                "{other}"
            );
        }
    }

    #[test]
    fn the_first_ending_wins() {
        let mut flow = flow();
        assert_eq!(
            flow.end(OAuthError::TimedOut),
            Some(Err(OAuthError::TimedOut))
        );
        assert_eq!(flow.end(OAuthError::Closed), None);
        assert!(!flow.navigate(&url(REDIRECT)).load);
        assert_eq!(flow.navigate(&url(REDIRECT)).ended, None);
    }

    #[test]
    fn says_what_went_wrong_in_words() {
        for (error, words) in [
            (
                OAuthError::BadUrl,
                "That is not an address the authorisation window opens.",
            ),
            (OAuthError::Busy, "An authorisation window is already open."),
            (OAuthError::Closed, "The authorisation window was closed."),
            (
                OAuthError::TimedOut,
                "The authorisation window was open too long, so it was closed.",
            ),
            (
                OAuthError::Window("no display".into()),
                "The authorisation window could not open: no display",
            ),
        ] {
            assert_eq!(error.to_string(), words);
        }
    }

    #[test]
    fn uses_the_profiles_store_on_macos_14_and_an_ephemeral_one_below() {
        assert_eq!(
            macos_store("p1", 14),
            GuestStore::Named(data_store_id("p1"))
        );
        assert_eq!(
            macos_store("p1", 15),
            GuestStore::Named(data_store_id("p1"))
        );
        assert_eq!(macos_store("p1", 13), GuestStore::Ephemeral);
    }
}
