// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Where a link should open.
//!
//! A link out of the app goes to the user's real browser. The one exception is
//! signing in to AgentRQ: the `at` cookie has to land in *this* profile's jar,
//! so that flow stays in the app. `javascript:`, `data:` and `file:` are never
//! opened from a link at all.

use url::Url;

/// Where a link goes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LinkTarget {
    /// The app itself; let the renderer's router handle it.
    App,
    /// Signing in to AgentRQ; run it in the app, on this profile's session.
    SignIn,
    /// Hand to the operating system: browser, mail client, dialler, and so on.
    System,
    /// Refuse.
    Blocked,
}

/// Prefix of every sign-in path on the server (`auth.js`'s `AUTH_PATH_PREFIX`).
pub const AUTH_PATH_PREFIX: &str = "/api/v1/auth/";

/// Schemes that address something other than a browser and are safe to pass to
/// the OS. An allowlist, because the interesting failure is the scheme nobody
/// thought about.
const SYSTEM_SCHEMES: [&str; 4] = ["mailto", "tel", "sms", "facetime"];

/// What the comparison needs to know about the app.
#[derive(Debug, Clone, Copy, Default)]
pub struct LinkOptions<'a> {
    /// The app's own origin, e.g. `app://agentrq`. Empty means none.
    pub app_origin: &'a str,
    /// The configured server. Empty means none is chosen yet.
    pub server_url: &'a str,
}

/// Is this path part of a sign-in?
pub fn is_auth_path(pathname: &str) -> bool {
    pathname.starts_with(AUTH_PATH_PREFIX)
}

/// Scheme, then host and port, for every scheme. `Url::origin` is opaque for
/// non-special schemes such as `app://`, so it cannot be compared.
fn origin_of(url: &Url) -> String {
    let host = url.host_str().unwrap_or("");
    match url.port() {
        Some(port) => format!("{}://{host}:{port}", url.scheme()),
        None => format!("{}://{host}", url.scheme()),
    }
}

/// Is this an AgentRQ sign-in on the server this app is connected to? The
/// origin has to match as well as the path: another host's `/api/v1/auth/…`
/// is a stranger's page, and keeping it in the app would hand it the jar.
fn is_sign_in(url: &Url, server_url: &str) -> bool {
    if server_url.is_empty() {
        return false;
    }
    match Url::parse(server_url) {
        Ok(server) => origin_of(url) == origin_of(&server) && is_auth_path(url.path()),
        Err(_) => false,
    }
}

/// Decide where `raw_url` opens. Anything that is not a URL is blocked.
pub fn classify_link(raw_url: &str, options: LinkOptions<'_>) -> LinkTarget {
    let Ok(url) = Url::parse(raw_url) else {
        return LinkTarget::Blocked;
    };

    if !options.app_origin.is_empty() && origin_of(&url) == options.app_origin {
        return LinkTarget::App;
    }
    match url.scheme() {
        "http" | "https" => {
            if is_sign_in(&url, options.server_url) {
                LinkTarget::SignIn
            } else {
                LinkTarget::System
            }
        }
        scheme if SYSTEM_SCHEMES.contains(&scheme) => LinkTarget::System,
        _ => LinkTarget::Blocked,
    }
}

/// The action to take when a navigation or new window is requested.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NavigationDecision {
    /// Allow the navigation to proceed in the webview.
    Allow,
    /// Open the URL via the system (opener).
    OpenExternal,
    /// Block the navigation.
    Block,
}

/// Map a link classification to the action the shell should take.
pub fn decide_navigation(raw_url: &str, options: LinkOptions<'_>) -> NavigationDecision {
    match classify_link(raw_url, options) {
        LinkTarget::App | LinkTarget::SignIn => NavigationDecision::Allow,
        LinkTarget::System => NavigationDecision::OpenExternal,
        LinkTarget::Blocked => NavigationDecision::Block,
    }
}

#[cfg(target_os = "windows")]
pub const PLATFORM: &str = "win32";
#[cfg(target_os = "macos")]
pub const PLATFORM: &str = "darwin";
#[cfg(not(any(target_os = "windows", target_os = "macos")))]
pub const PLATFORM: &str = "linux";

/// The renderer's origin for a profile on a given platform.
pub fn app_origin_for(profile_id: &str, platform: &str) -> String {
    if platform == "win32" {
        "http://app.localhost".to_string()
    } else if platform == "darwin" {
        if profile_id.is_empty() || profile_id == "default" || profile_id == "localhost" {
            "app://localhost".to_string()
        } else {
            format!("app://{profile_id}")
        }
    } else {
        "app://localhost".to_string()
    }
}

/// The renderer's origin for a profile on the current platform.
pub fn app_origin(profile_id: &str) -> String {
    app_origin_for(profile_id, PLATFORM)
}

/// Safe data: MIME types that cannot execute scripts in a browser or webview.
const ALLOWED_DATA_MIME_TYPES: &[&str] = &[
    "application/json",
    "application/octet-stream",
    "application/pdf",
    "application/zip",
    "audio/aac",
    "audio/flac",
    "audio/mp3",
    "audio/mp4",
    "audio/mpeg",
    "audio/ogg",
    "audio/wav",
    "audio/webm",
    "image/avif",
    "image/bmp",
    "image/gif",
    "image/jpeg",
    "image/png",
    "image/webp",
    "text/csv",
    "text/plain",
    "video/mp4",
    "video/mpeg",
    "video/ogg",
    "video/webm",
];

/// Whether this navigation URL is an app-origin download (blob: with app origin or safe data:).
pub fn is_allowed_download(url: &Url, app_origin: &str) -> bool {
    match url.scheme() {
        "data" => {
            let spec = &url.as_str()["data:".len()..];
            let meta = spec.split(',').next().unwrap_or("");
            let mime = meta
                .split(';')
                .next()
                .unwrap_or("")
                .trim()
                .to_ascii_lowercase();
            let effective = if mime.is_empty() {
                "text/plain"
            } else {
                mime.as_str()
            };
            ALLOWED_DATA_MIME_TYPES.contains(&effective)
        }
        "blob" => {
            let inner = &url.as_str()["blob:".len()..];
            Url::parse(inner).is_ok_and(|parsed| origin_of(&parsed) == app_origin)
        }
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const APP_ORIGIN: &str = "app://agentrq";
    const SERVER_URL: &str = "https://app.agentrq.com";

    fn classify(url: &str) -> LinkTarget {
        classify_link(
            url,
            LinkOptions {
                app_origin: APP_ORIGIN,
                server_url: SERVER_URL,
            },
        )
    }

    #[test]
    fn leaves_the_app_to_its_own_router() {
        assert_eq!(classify("app://agentrq/tasks/123"), LinkTarget::App);
        assert_eq!(classify("app://agentrq/"), LinkTarget::App);
    }

    #[test]
    fn does_not_mistake_another_app_host_for_the_app() {
        assert_eq!(classify("app://elsewhere/tasks/123"), LinkTarget::Blocked);
    }

    #[test]
    fn sends_web_content_to_the_browser() {
        assert_eq!(classify("https://agentrq.com/docs"), LinkTarget::System);
        assert_eq!(classify("https://agentrq.com/tos"), LinkTarget::System);
        assert_eq!(
            classify("http://192.168.1.10:3000/report"),
            LinkTarget::System
        );
    }

    #[test]
    fn keeps_signing_in_to_agentrq_inside_the_app() {
        assert_eq!(
            classify(&format!("{SERVER_URL}/api/v1/auth/google/login")),
            LinkTarget::SignIn
        );
        assert_eq!(
            classify(&format!(
                "{SERVER_URL}/api/v1/auth/github/callback?code=abc"
            )),
            LinkTarget::SignIn
        );
    }

    #[test]
    fn does_not_treat_an_ordinary_page_on_the_server_as_signing_in() {
        assert_eq!(
            classify(&format!("{SERVER_URL}/tasks/123")),
            LinkTarget::System
        );
        assert_eq!(
            classify(&format!("{SERVER_URL}/api/v1/workspaces")),
            LinkTarget::System
        );
    }

    #[test]
    fn will_not_keep_somebody_elses_auth_page_in_the_app() {
        assert_eq!(
            classify("https://evil.example/api/v1/auth/google/login"),
            LinkTarget::System
        );
        assert_eq!(
            classify("http://app.agentrq.com/api/v1/auth/google/login"),
            LinkTarget::System
        );
        assert_eq!(
            classify("https://app.agentrq.com.evil.example/api/v1/auth/google/login"),
            LinkTarget::System
        );
    }

    #[test]
    fn has_no_sign_in_exception_before_a_server_is_chosen() {
        let url = format!("{SERVER_URL}/api/v1/auth/google/login");
        assert_eq!(
            classify_link(
                &url,
                LinkOptions {
                    app_origin: APP_ORIGIN,
                    ..Default::default()
                }
            ),
            LinkTarget::System
        );
        assert_eq!(
            classify_link(
                &url,
                LinkOptions {
                    app_origin: APP_ORIGIN,
                    server_url: "not a url",
                }
            ),
            LinkTarget::System
        );
    }

    #[test]
    fn hands_schemes_a_browser_cannot_render_to_the_system() {
        assert_eq!(classify("mailto:hi@agentrq.com"), LinkTarget::System);
        assert_eq!(classify("tel:+15551234"), LinkTarget::System);
        assert_eq!(classify("sms:+15551234"), LinkTarget::System);
    }

    #[test]
    fn refuses_schemes_that_reach_the_machine_rather_than_the_web() {
        assert_eq!(classify("javascript:alert(1)"), LinkTarget::Blocked);
        assert_eq!(
            classify("data:text/html,<script>alert(1)</script>"),
            LinkTarget::Blocked
        );
        assert_eq!(classify("file:///etc/passwd"), LinkTarget::Blocked);
        assert_eq!(classify("vbscript:msgbox(1)"), LinkTarget::Blocked);
    }

    #[test]
    fn is_not_fooled_by_an_unusual_spelling_of_the_scheme() {
        assert_eq!(classify("JavaScript:alert(1)"), LinkTarget::Blocked);
        assert_eq!(classify("HTTPS://agentrq.com/docs"), LinkTarget::System);
    }

    #[test]
    fn refuses_anything_that_is_not_a_url() {
        // JS also covers null and undefined, which `String(x ?? '')` turns into
        // the empty string; a `&str` cannot be null, so the empty case stands
        // for all three.
        assert_eq!(classify("/tasks/123"), LinkTarget::Blocked);
        assert_eq!(classify(""), LinkTarget::Blocked);
    }

    #[test]
    fn treats_app_as_ordinary_when_no_app_origin_is_given() {
        assert_eq!(
            classify_link("app://agentrq/tasks", LinkOptions::default()),
            LinkTarget::Blocked
        );
    }

    #[test]
    fn compares_the_port_as_part_of_the_origin() {
        let options = LinkOptions {
            app_origin: APP_ORIGIN,
            server_url: "http://localhost:3000",
        };
        let on = |url: &str| classify_link(url, options);
        assert_eq!(
            on("http://localhost:3000/api/v1/auth/google/login"),
            LinkTarget::SignIn
        );
        assert_eq!(
            on("http://localhost:3001/api/v1/auth/google/login"),
            LinkTarget::System
        );
    }

    #[test]
    fn maps_classification_to_navigation_decision() {
        let options = LinkOptions {
            app_origin: APP_ORIGIN,
            server_url: SERVER_URL,
        };
        // App and SignIn allow in-app navigation
        assert_eq!(
            decide_navigation("app://agentrq/tasks", options),
            NavigationDecision::Allow
        );
        assert_eq!(
            decide_navigation(&format!("{SERVER_URL}/api/v1/auth/google/login"), options),
            NavigationDecision::Allow
        );
        // System URLs (web, mailto, etc.) open externally
        assert_eq!(
            decide_navigation("https://agentrq.com/docs", options),
            NavigationDecision::OpenExternal
        );
        assert_eq!(
            decide_navigation("mailto:hi@agentrq.com", options),
            NavigationDecision::OpenExternal
        );
        // Blocked URLs (file:, javascript:, invalid) are blocked
        assert_eq!(
            decide_navigation("file:///etc/passwd", options),
            NavigationDecision::Block
        );
        assert_eq!(
            decide_navigation("javascript:alert(1)", options),
            NavigationDecision::Block
        );
        assert_eq!(
            decide_navigation("invalid url", options),
            NavigationDecision::Block
        );
    }

    #[test]
    fn chooses_app_origin_per_platform() {
        assert_eq!(app_origin_for("default", "win32"), "http://app.localhost");
        assert_eq!(app_origin_for("work", "darwin"), "app://work");
        assert_eq!(app_origin_for("default", "darwin"), "app://localhost");
        assert_eq!(app_origin_for("", "darwin"), "app://localhost");
        assert_eq!(app_origin_for("default", "linux"), "app://localhost");
        assert_eq!(app_origin("default"), app_origin_for("default", PLATFORM));
    }

    #[test]
    fn allows_app_origin_blob_and_data_downloads() {
        let origin = "app://localhost";
        let valid_blob = Url::parse("blob:app://localhost/8b248a37-58b2-4d2b-bbd7").unwrap();
        assert!(is_allowed_download(&valid_blob, origin));

        let data_url = Url::parse("data:application/pdf;base64,JVBERi0xLjQK").unwrap();
        assert!(is_allowed_download(&data_url, origin));

        let data_plain = Url::parse("data:text/plain;base64,aGVsbG8=").unwrap();
        assert!(is_allowed_download(&data_plain, origin));

        // Top-level HTML and XHTML navigations must be blocked
        let data_html = Url::parse("data:text/html,<script>alert(1)</script>").unwrap();
        assert!(!is_allowed_download(&data_html, origin));

        let data_html_b64 = Url::parse("data:text/html;base64,PGgxPkhlbGxvPC9oMT4=").unwrap();
        assert!(!is_allowed_download(&data_html_b64, origin));

        let data_xhtml = Url::parse("data:application/xhtml+xml,<html/>").unwrap();
        assert!(!is_allowed_download(&data_xhtml, origin));

        // SVG, XML, and +xml data navigations must be blocked
        let data_svg = Url::parse("data:image/svg+xml,<svg onload='alert(1)'/>").unwrap();
        assert!(!is_allowed_download(&data_svg, origin));

        let data_text_xml = Url::parse("data:text/xml,<root/>").unwrap();
        assert!(!is_allowed_download(&data_text_xml, origin));

        let data_app_xml = Url::parse("data:application/xml,<root/>").unwrap();
        assert!(!is_allowed_download(&data_app_xml, origin));

        let data_rss = Url::parse("data:application/rss+xml,<rss/>").unwrap();
        assert!(!is_allowed_download(&data_rss, origin));

        // Empty MIME defaults to text/plain and is allowed
        let data_default_plain = Url::parse("data:,hello%20world").unwrap();
        assert!(is_allowed_download(&data_default_plain, origin));

        // Safe media types are allowed
        let data_png = Url::parse("data:image/png;base64,iVBORw0KGgo=").unwrap();
        assert!(is_allowed_download(&data_png, origin));

        let evil_blob = Url::parse("blob:https://evil.example.com/8b248a37").unwrap();
        assert!(!is_allowed_download(&evil_blob, origin));

        let invalid_blob = Url::parse("blob:not-a-valid-url").unwrap();
        assert!(!is_allowed_download(&invalid_blob, origin));

        let app_page = Url::parse("app://localhost/tasks").unwrap();
        assert!(!is_allowed_download(&app_page, origin));
    }
}
