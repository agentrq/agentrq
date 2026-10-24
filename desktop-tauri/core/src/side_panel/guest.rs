// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The side panel's guest: the page shown in the webview beside the app.
//!
//! The element lives in the renderer, which is the app's own code on a
//! privileged origin — but what it asks for is not what it gets. Every guest's
//! preferences are decided here, before it is attached, and whatever the
//! element carried is overwritten. That is the boundary: a page in the panel
//! has no Node, no bridge to the app, and a cookie jar that is never the
//! profile's, so it can never be sent with the `at` cookie.

use std::borrow::Cow;

use serde_json::{Map, Value};
use url::Url;

use crate::links::{classify_link, LinkOptions, LinkTarget};

/// The scheme an Extension's own pages are served from.
pub const EXT_SCHEME: &str = "agentrq-ext";

/// What a guest may be created on. `about:blank` is the empty panel.
const ATTACHABLE: [&str; 4] = ["http:", "https:", "agentrq-ext:", "about:"];

/// The guest's cookie jar for a profile.
///
/// One per profile, so switching profile does not carry one account's browsing
/// into the other's — and never the profile's own `persist:profile-<id>`, which
/// holds the credential.
pub fn panel_partition_for(profile_id: &str) -> String {
    format!("persist:panel-{profile_id}")
}

/// An Extension page's address as the webview reports it.
///
/// WebView2 has no custom schemes, so wry serves `agentrq-ext://notes/x` as
/// `http://agentrq-ext.notes/x` and reports that form from `url()` and the
/// navigation events. This maps it back, exactly as wry's own
/// `revert_uri_work_around` does (everything after `agentrq-ext.` is the host,
/// so `http://agentrq-ext.notes.evil.com/` names `notes.evil.com`, which no
/// Extension is). Any other address is returned as it came.
pub fn normalize_ext_url(raw: &str) -> Cow<'_, str> {
    let trimmed = raw.trim_matches(|c: char| c <= ' ');
    for scheme in ["http", "https"] {
        let prefix = format!("{scheme}://{EXT_SCHEME}.");
        let Some(head) = trimmed.get(..prefix.len()) else {
            continue;
        };
        if head.eq_ignore_ascii_case(&prefix) {
            return Cow::Owned(format!("{EXT_SCHEME}://{}", &trimmed[prefix.len()..]));
        }
    }
    Cow::Borrowed(raw)
}

/// `new URL(x).protocol`: the scheme and its colon, or `""` for anything that
/// is not a URL.
fn protocol_of(raw_url: &str) -> String {
    match Url::parse(raw_url) {
        Ok(url) => format!("{}:", url.scheme()),
        Err(_) => String::new(),
    }
}

/// `new URL(x).host`: the host with its port, which is what names an Extension.
/// `agentrq-ext://notes:1/` is not `notes`.
pub(crate) fn host_of(url: &Url) -> String {
    let host = url.host_str().unwrap_or("");
    match url.port() {
        Some(port) => format!("{host}:{port}"),
        None => host.to_string(),
    }
}

/// What a guest is created with.
#[derive(Debug, Clone, Copy)]
pub struct GuestOptions<'a> {
    /// The active profile's panel partition.
    pub partition: &'a str,
    /// Absolute path of the guest preload.
    pub preload: &'a str,
}

/// Whether a guest may be attached.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HardenResult {
    Ok,
    Refused { reason: String },
}

/// Overwrite a guest's preferences, or refuse it.
///
/// Mutates `web_preferences` and `params` in place, because that is how the
/// webview hands them over: the objects are the ones the guest is created with.
/// `params` is the element's attributes, `src` among them.
pub fn harden_guest(
    web_preferences: &mut Map<String, Value>,
    params: &mut Map<String, Value>,
    options: GuestOptions<'_>,
) -> HardenResult {
    let src = params.get("src").and_then(Value::as_str).unwrap_or("");
    if !src.is_empty() {
        let protocol = protocol_of(src);
        if !ATTACHABLE.contains(&protocol.as_str()) {
            let what = if protocol.is_empty() {
                "that address"
            } else {
                &protocol
            };
            return HardenResult::Refused {
                reason: format!("The side panel does not open {what}"),
            };
        }
    }
    // On the parsed address, so `ABOUT:config` and ` about:config` are refused
    // too. The JS tests the raw string; this is a deliberate divergence.
    if Url::parse(src).is_ok_and(|url| url.scheme() == "about" && url.as_str() != "about:blank") {
        return HardenResult::Refused {
            reason: "The side panel does not open about: pages".to_string(),
        };
    }

    for key in [
        "nodeIntegration",
        "nodeIntegrationInSubFrames",
        "nodeIntegrationInWorker",
    ] {
        web_preferences.remove(key);
    }
    web_preferences.insert("contextIsolation".into(), true.into());
    web_preferences.insert("sandbox".into(), true.into());
    web_preferences.insert("webSecurity".into(), true.into());
    web_preferences.insert("partition".into(), options.partition.into());
    // Always ours, whatever the element named. The preload decides for itself
    // whether the page it finds is one that gets a bridge.
    web_preferences.insert("preload".into(), options.preload.into());

    // Counter-intuitive, and the reason popups are not left disabled: a guest
    // that disables them has `window.open` and `target=_blank` dropped before
    // the window-open handler is ever asked, so the link silently does nothing.
    // Enabled, every request reaches the handler in `wire` — which refuses all
    // of them, so no window is ever created, and loads the page in the panel.
    web_preferences.insert("disablePopups".into(), false.into());
    // Blink features an element can switch on with an attribute.
    web_preferences.insert("enableBlinkFeatures".into(), "".into());
    web_preferences.insert("experimentalFeatures".into(), false.into());
    web_preferences.insert("allowRunningInsecureContent".into(), false.into());

    params.insert("partition".into(), options.partition.into());
    for key in ["allowpopups", "nodeintegration", "preload"] {
        params.remove(key);
    }
    HardenResult::Ok
}

/// Where a navigation inside the panel goes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PanelTarget {
    /// Stays in the panel.
    Panel,
    /// AgentRQ's own sign-in, which has to run on the profile's session.
    SignIn,
    /// Handed to the operating system: the browser, the mail client.
    System,
    /// Refused.
    Blocked,
}

/// What a target is decided against.
#[derive(Debug, Clone, Copy, Default)]
pub struct NavigationOptions<'a> {
    /// The configured server. Empty means none is chosen yet.
    pub server_url: &'a str,
}

/// Where a guest navigating to `raw_url` should end up.
///
/// The web and an Extension's pages stay in the panel. AgentRQ's sign-in does
/// not: the panel's cookie jar is not the profile's, so signing in there would
/// succeed and leave the app signed out. Anything else is the existing link
/// rule, which is what keeps `file:` and `javascript:` out.
pub fn navigation_target(raw_url: &str, options: NavigationOptions<'_>) -> PanelTarget {
    let raw_url = &*normalize_ext_url(raw_url);
    let protocol = protocol_of(raw_url);
    if protocol == format!("{EXT_SCHEME}:") || raw_url == "about:blank" {
        return PanelTarget::Panel;
    }

    let link_options = LinkOptions {
        app_origin: "",
        server_url: options.server_url,
    };
    match classify_link(raw_url, link_options) {
        LinkTarget::System => {
            if protocol == "http:" || protocol == "https:" {
                PanelTarget::Panel
            } else {
                PanelTarget::System
            }
        }
        LinkTarget::SignIn => PanelTarget::SignIn,
        _ => PanelTarget::Blocked,
    }
}

/// A guest's `window.open` or `target=_blank`.
#[derive(Debug, Clone, Copy)]
pub struct OpenDetails<'a> {
    pub url: &'a str,
    /// Chromium's disposition; empty when none was given.
    pub disposition: &'a str,
}

/// Where a guest's `window.open` or `target=_blank` should end up.
///
/// A guest never gets a second window. Chromium reports a Cmd/Ctrl- or
/// middle-click as `background-tab`, which is somebody asking for the page
/// *somewhere else* — their browser. Every other disposition opens in the panel.
pub fn open_target(details: OpenDetails<'_>, options: NavigationOptions<'_>) -> PanelTarget {
    let url = normalize_ext_url(details.url);
    let target = navigation_target(&url, options);
    if target == PanelTarget::Panel && details.disposition == "background-tab" {
        let protocol = protocol_of(&url);
        return if protocol == "http:" || protocol == "https:" {
            PanelTarget::System
        } else {
            PanelTarget::Panel
        };
    }
    target
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    const SERVER_URL: &str = "https://app.agentrq.com";
    const OPTIONS: GuestOptions<'static> = GuestOptions {
        partition: "persist:panel-p1",
        preload: "/app/dist/preload/panel.cjs",
    };

    fn object(value: Value) -> Map<String, Value> {
        value.as_object().cloned().unwrap_or_default()
    }

    #[test]
    fn partition_gives_each_profile_a_jar_of_its_own_that_is_never_the_profile_session() {
        assert_eq!(panel_partition_for("p1"), "persist:panel-p1");
        assert_ne!(panel_partition_for("p1"), "persist:profile-p1");
    }

    #[test]
    fn harden_overwrites_whatever_the_element_asked_for() {
        let mut web_preferences = object(json!({
            "nodeIntegration": true,
            "nodeIntegrationInSubFrames": true,
            "nodeIntegrationInWorker": true,
            "contextIsolation": false,
            "sandbox": false,
            "webSecurity": false,
            "disablePopups": true,
            "enableBlinkFeatures": "CSSPseudoHas",
            "experimentalFeatures": true,
            "allowRunningInsecureContent": true,
            "partition": "persist:profile-p1",
            "preload": "/somewhere/else.js",
        }));
        let mut params = object(json!({
            "src": "https://example.com/",
            "partition": "persist:profile-p1",
            "allowpopups": true,
            "nodeintegration": "on",
            "preload": "file:///evil.js",
        }));

        assert_eq!(
            harden_guest(&mut web_preferences, &mut params, OPTIONS),
            HardenResult::Ok
        );
        // Popups on, not off: only then does one reach the window-open
        // handler, which refuses every one (wire).
        assert_eq!(
            Value::Object(web_preferences),
            json!({
                "contextIsolation": true,
                "sandbox": true,
                "webSecurity": true,
                "partition": "persist:panel-p1",
                "preload": "/app/dist/preload/panel.cjs",
                "disablePopups": false,
                "enableBlinkFeatures": "",
                "experimentalFeatures": false,
                "allowRunningInsecureContent": false,
            })
        );
        assert_eq!(
            Value::Object(params),
            json!({ "src": "https://example.com/", "partition": "persist:panel-p1" })
        );
    }

    #[test]
    fn harden_accepts_the_web_an_extension_page_the_empty_panel_and_no_address_at_all() {
        let ext_page = format!("{EXT_SCHEME}://notes/panel/index.html");
        let sources = [
            Some("http://localhost:3000/"),
            Some("https://example.com"),
            Some(ext_page.as_str()),
            Some("about:blank"),
            None,
        ];
        for src in sources {
            let mut params = Map::new();
            if let Some(src) = src {
                params.insert("src".into(), src.into());
            }
            assert_eq!(
                harden_guest(&mut Map::new(), &mut params, OPTIONS),
                HardenResult::Ok,
                "src {src:?}"
            );
        }
    }

    #[test]
    fn harden_refuses_a_guest_created_on_any_other_scheme() {
        let sources = [
            "file:///etc/passwd",
            "javascript:alert(1)",
            "data:text/html,hi",
            "app://agentrq/",
            "about:config",
            "not a url",
        ];
        for src in sources {
            let mut params = object(json!({ "src": src }));
            let result = harden_guest(&mut Map::new(), &mut params, OPTIONS);
            assert!(
                matches!(&result, HardenResult::Refused { reason }
                    if reason.starts_with("The side panel does not open")),
                "src {src}: {result:?}"
            );
        }
    }

    #[test]
    fn harden_names_what_it_refused() {
        let refuse = |src: &str| {
            let mut params = object(json!({ "src": src }));
            harden_guest(&mut Map::new(), &mut params, OPTIONS)
        };
        let reason = |text: &str| HardenResult::Refused {
            reason: text.to_string(),
        };
        assert_eq!(
            refuse("file:///x"),
            reason("The side panel does not open file:")
        );
        assert_eq!(
            refuse("not a url"),
            reason("The side panel does not open that address")
        );
        assert_eq!(
            refuse("about:config"),
            reason("The side panel does not open about: pages")
        );
    }

    #[test]
    fn harden_judges_about_on_the_parsed_address_not_the_raw_string() {
        // A deliberate divergence from the JS, which tests the raw string and
        // so lets these through.
        for src in [
            "ABOUT:config",
            " about:config",
            "\tabout:config",
            "about:blank#x",
        ] {
            let mut params = object(json!({ "src": src }));
            assert_eq!(
                harden_guest(&mut Map::new(), &mut params, OPTIONS),
                HardenResult::Refused {
                    reason: "The side panel does not open about: pages".to_string()
                },
                "src {src:?}"
            );
        }
        // The empty panel is the empty panel however it is spelled.
        for src in ["ABOUT:blank", " about:blank", "\tabout:blank"] {
            let mut params = object(json!({ "src": src }));
            assert_eq!(
                harden_guest(&mut Map::new(), &mut params, OPTIONS),
                HardenResult::Ok,
                "src {src:?}"
            );
        }
    }

    #[test]
    fn normalize_maps_the_webview2_form_back_and_touches_nothing_else() {
        let n = |raw: &str| normalize_ext_url(raw).into_owned();
        assert_eq!(
            n("http://agentrq-ext.notes/panel/x.html?a=1#b"),
            "agentrq-ext://notes/panel/x.html?a=1#b"
        );
        assert_eq!(n("https://agentrq-ext.notes/x"), "agentrq-ext://notes/x");
        assert_eq!(n("HTTP://AGENTRQ-EXT.notes/x"), "agentrq-ext://notes/x");
        assert_eq!(
            n(" \thttps://AgentRQ-Ext.notes:1/x "),
            "agentrq-ext://notes:1/x"
        );
        // As wry reverts it: all after `agentrq-ext.` is the host.
        assert_eq!(
            n("http://agentrq-ext.notes.evil.com/"),
            "agentrq-ext://notes.evil.com/"
        );
        for untouched in [
            "agentrq-ext://notes/x",
            "http://agentrq-ext/x",
            "http://agentrq-extra.notes/x",
            "http://notes.agentrq-ext.example/x",
            "http://example.com/agentrq-ext.notes",
            "ftp://agentrq-ext.notes/x",
            "http://agentrq-é",
            "http://",
            "",
        ] {
            assert!(
                matches!(normalize_ext_url(untouched), Cow::Borrowed(same) if same == untouched),
                "{untouched:?}"
            );
        }
    }

    #[test]
    fn navigation_and_open_treat_the_webview2_form_as_the_extension_page() {
        for url in [
            "http://agentrq-ext.notes/panel/index.html",
            "https://AgentRQ-Ext.notes/panel/index.html",
            "agentrq-ext://notes/panel/index.html",
        ] {
            assert_eq!(target(url), PanelTarget::Panel, "{url}");
            // Cmd/Ctrl-click stays in the panel: never DNS for `agentrq-ext.notes`.
            assert_eq!(open(url, "background-tab"), PanelTarget::Panel, "{url}");
            assert_eq!(open(url, "foreground-tab"), PanelTarget::Panel, "{url}");
        }
        // Not the Extension scheme: an ordinary site stays an ordinary site.
        assert_eq!(
            open("http://agentrq-extra.notes/", "background-tab"),
            PanelTarget::System
        );
    }

    #[test]
    fn harden_leaves_a_refused_guest_untouched() {
        let mut web_preferences = object(json!({ "nodeIntegration": true }));
        let mut params = object(json!({ "src": "file:///x", "allowpopups": true }));
        harden_guest(&mut web_preferences, &mut params, OPTIONS);
        assert_eq!(
            Value::Object(web_preferences),
            json!({ "nodeIntegration": true })
        );
        assert_eq!(
            Value::Object(params),
            json!({ "src": "file:///x", "allowpopups": true })
        );
    }

    fn target(url: &str) -> PanelTarget {
        navigation_target(
            url,
            NavigationOptions {
                server_url: SERVER_URL,
            },
        )
    }

    #[test]
    fn navigation_keeps_the_web_and_extension_pages_in_the_panel() {
        assert_eq!(target("https://example.com/docs"), PanelTarget::Panel);
        assert_eq!(target("http://192.168.1.10:3000/"), PanelTarget::Panel);
        assert_eq!(
            target(&format!("{EXT_SCHEME}://notes/panel/index.html")),
            PanelTarget::Panel
        );
        assert_eq!(target("about:blank"), PanelTarget::Panel);
    }

    #[test]
    fn navigation_sends_agentrq_sign_in_to_the_main_window_whose_session_it_has_to_land_in() {
        assert_eq!(
            target(&format!("{SERVER_URL}/api/v1/auth/google/login")),
            PanelTarget::SignIn
        );
    }

    #[test]
    fn navigation_hands_mail_and_phone_links_to_the_operating_system() {
        assert_eq!(target("mailto:hi@example.com"), PanelTarget::System);
    }

    #[test]
    fn navigation_refuses_file_script_data_and_app_addresses() {
        for url in [
            "file:///etc/passwd",
            "javascript:alert(1)",
            "data:text/html,hi",
            "app://agentrq/tasks",
        ] {
            assert_eq!(target(url), PanelTarget::Blocked, "{url}");
        }
    }

    #[test]
    fn navigation_works_without_options() {
        let none = NavigationOptions::default();
        assert_eq!(
            navigation_target("https://example.com", none),
            PanelTarget::Panel
        );
        // `undefined` in the original: not an address at all.
        assert_eq!(navigation_target("", none), PanelTarget::Blocked);
    }

    fn open(url: &str, disposition: &str) -> PanelTarget {
        open_target(
            OpenDetails { url, disposition },
            NavigationOptions {
                server_url: SERVER_URL,
            },
        )
    }

    #[test]
    fn open_puts_a_new_window_request_in_the_panel() {
        assert_eq!(
            open("https://example.com", "foreground-tab"),
            PanelTarget::Panel
        );
        assert_eq!(
            open("https://example.com", "new-window"),
            PanelTarget::Panel
        );
        assert_eq!(open("https://example.com", "default"), PanelTarget::Panel);
        assert_eq!(open("https://example.com", ""), PanelTarget::Panel);
    }

    #[test]
    fn open_sends_a_cmd_ctrl_or_middle_click_on_a_web_link_to_the_browser() {
        assert_eq!(
            open("https://example.com", "background-tab"),
            PanelTarget::System
        );
    }

    #[test]
    fn open_keeps_a_cmd_ctrl_clicked_extension_page_in_the_panel_which_is_the_only_place_it_exists()
    {
        assert_eq!(
            open(
                &format!("{EXT_SCHEME}://notes/panel/other.html"),
                "background-tab"
            ),
            PanelTarget::Panel
        );
    }

    #[test]
    fn open_applies_the_navigation_rule_to_everything_else() {
        assert_eq!(
            open("file:///etc/passwd", "background-tab"),
            PanelTarget::Blocked
        );
        assert_eq!(
            open("mailto:hi@example.com", "foreground-tab"),
            PanelTarget::System
        );
    }

    #[test]
    fn host_includes_the_port_as_a_url_host_does() {
        let host = |raw: &str| host_of(&Url::parse(raw).unwrap());
        assert_eq!(host("agentrq-ext://notes/x"), "notes");
        assert_eq!(host("agentrq-ext://notes:8080/x"), "notes:8080");
        assert_eq!(host("agentrq-ext:///x"), "");
    }
}
