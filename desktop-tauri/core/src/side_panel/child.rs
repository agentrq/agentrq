// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The side panel's guest as a native child webview (the Tauri shell).
//!
//! Electron's guest is a `<webview>` element; Tauri's is a webview of its own,
//! laid over the main one. What that changes is decided here: the label that
//! tells a guest from the app, where its storage lives, where it is placed,
//! what a frame may load, which clicks mean "in my browser", and the panel
//! bridge, which reaches Rust through the `agentrq-ext` scheme because a guest
//! has no IPC at all.

use serde::Serialize;
use serde_json::{json, Value};
use url::Url;

use super::bridge::{extension_of, Refusal, Sender, MAX_MESSAGE_BYTES};
use super::guest::{host_of, normalize_ext_url, EXT_SCHEME};
use crate::profiles::data_store_id;

/// Every guest's label starts with this, and nothing else's does. The main
/// capability names the `main` webview only, so a guest gets no command.
pub const GUEST_LABEL_PREFIX: &str = "panel-guest-";

/// The label of the `n`th guest this run.
pub fn guest_label(n: u32) -> String {
    format!("{GUEST_LABEL_PREFIX}{n}")
}

/// The number in a guest's label, which the side panel's rules know it by;
/// `None` for any webview that is not a side panel guest.
pub fn guest_number(label: &str) -> Option<u32> {
    label
        .strip_prefix(GUEST_LABEL_PREFIX)
        .filter(|n| n.bytes().all(|b| b.is_ascii_digit()))
        .and_then(|n| n.parse().ok())
}

/// Whether a webview is a side panel guest. Custom schemes are app-wide, so
/// this is what keeps the app's own window off `agentrq-ext://`.
pub fn is_guest_label(label: &str) -> bool {
    guest_number(label).is_some()
}

/// The name of the folder (or, on macOS, the store) a profile's guests keep
/// their cookies and storage in. Never the profile's own, so no guest ever
/// carries `at`.
pub fn panel_data_dir(profile_id: &str) -> String {
    format!("panel-{profile_id}")
}

/// Where a guest keeps its browsing on macOS.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum GuestStore {
    /// The profile's panel store, from macOS 14, where WKWebView has them.
    Named([u8; 16]),
    /// A store that is never written to disk. wry gives the default store,
    /// the app's own, to a named one before macOS 14.
    Ephemeral,
}

/// The store of `profile_id`'s guests on macOS `major`.
pub fn guest_store(profile_id: &str, major: isize) -> GuestStore {
    if major >= 14 {
        GuestStore::Named(data_store_id(&panel_data_dir(profile_id)))
    } else {
        GuestStore::Ephemeral
    }
}

/// The host element's box, in CSS pixels, as the page measured it.
#[derive(Debug, Clone, Copy, PartialEq, serde::Deserialize)]
pub struct HostRect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// Where the guest goes, in the window's logical pixels.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Placement {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// The guest's place for a host measured at `rect` in a page zoomed to
/// `zoom`: a CSS pixel is `zoom` logical pixels. `None` when the page sent
/// something that is not a box.
pub fn place(rect: HostRect, zoom: f64) -> Option<Placement> {
    let values = [rect.x, rect.y, rect.width, rect.height];
    if !values.iter().all(|v| v.is_finite()) {
        return None;
    }
    let zoom = if zoom.is_finite() && zoom > 0.0 {
        zoom
    } else {
        1.0
    };
    Some(Placement {
        x: rect.x * zoom,
        y: rect.y * zoom,
        width: rect.width.max(0.0) * zoom,
        height: rect.height.max(0.0) * zoom,
    })
}

/// What was held down when a guest asked for a new window.
#[derive(Debug, Clone, Copy, Default)]
pub struct Modifiers {
    pub control: bool,
    pub meta: bool,
    /// The middle mouse button.
    pub middle: bool,
}

/// Chromium's disposition for a new-window request, which wry does not
/// report: a Cmd-click on macOS, a Ctrl-click elsewhere, or a middle click is
/// `background-tab`, somebody asking for the page in their browser. Anything
/// else is `""`. `platform` is `process.platform`'s name for it.
pub fn disposition(held: Modifiers, platform: &str) -> &'static str {
    let link_key = if platform == "darwin" {
        held.meta
    } else {
        held.control
    };
    if link_key || held.middle {
        "background-tab"
    } else {
        ""
    }
}

/// What the webview said about a navigation, for [`link_click`].
#[derive(Debug, Clone, Copy, Default)]
pub struct NavigationKind {
    /// Somebody followed a link: not a script, a form, a reload or a frame
    /// loading its own document.
    pub link_clicked: bool,
    /// The click was the person's own, not one a script made.
    pub user_gesture: bool,
    /// What the click itself held down, never the keys held now.
    pub held: Modifiers,
}

/// The modifiers of a person's own click on a link, or `None` for every other
/// navigation, which no key held at the time may send to the browser.
pub fn link_click(kind: NavigationKind) -> Option<Modifiers> {
    (kind.link_clicked && kind.user_gesture).then_some(kind.held)
}

/// Whether a navigation is a Cmd/Ctrl-click on a link, to be taken to the
/// browser as Chromium's `background-tab` would be. WebKit does not turn that
/// click into a new-window request, and wry reports no modifiers, so the
/// platform reads the click itself ([`link_click`]); WebView2 asks for a
/// window, so it never comes here.
pub fn opens_elsewhere(
    url: &str,
    click: Option<Modifiers>,
    platform: &str,
    webview2: bool,
) -> bool {
    let Some(held) = click else {
        return false;
    };
    !webview2
        && disposition(held, platform) == "background-tab"
        && (url.starts_with("http:") || url.starts_with("https:"))
}

/// Whether a guest is asked to load the app's own assets or IPC, which Tauri
/// serves on every webview: `tauri://localhost` and `ipc://localhost`, which
/// WebView2 spells `http://tauri.localhost` and `http://ipc.localhost`.
pub fn is_app_internal(url: &str) -> bool {
    let Ok(url) = Url::parse(url) else {
        return false;
    };
    matches!(url.scheme(), "tauri" | "ipc")
        || matches!(url.host_str(), Some("tauri.localhost" | "ipc.localhost"))
}

/// Whether a navigation the shell cannot place in a frame is a subframe's own
/// document. WebKitGTK asks about every frame's navigation, and an
/// `<iframe srcdoc>` is `about:srcdoc`, which no top-level page can be; it is
/// let through rather than blocking every editor and embed that uses one.
pub fn is_srcdoc(url: &str) -> bool {
    url == "about:srcdoc"
}

/// The path an Extension page's `window.agentrq.panel.send` posts to. Its
/// first segment starts with a dot, which [`super::ext_scheme::panel_path_of`]
/// never serves, so no Extension file can stand in its place.
pub const BRIDGE_PATH: &str = "/.agentrq/panel-send";

/// A request on the `agentrq-ext` scheme, as far as the bridge is concerned.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BridgeCall {
    /// Not for the bridge: a file of the Extension, served as one.
    File,
    /// For the bridge, and refused before any Extension hears of it.
    Refused(String),
    /// For the bridge, from the Extension page at `top_url`.
    Send {
        top_url: String,
        is_main_frame: bool,
    },
}

/// The header an Extension page's `send` carries its token in.
pub const TOKEN_HEADER: &str = "x-agentrq-panel-token";

/// A guest's bridge token, from 32 random bytes the shell draws: lower-case
/// hex, which needs no escaping in a header or a script.
pub fn token_of(bytes: [u8; 32]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

/// Whether a request carries the token its guest holds now, compared in a
/// time that does not depend on where they differ. No token yet, none sent,
/// or an empty one is never a match.
pub fn same_token(sent: Option<&str>, expected: Option<&str>) -> bool {
    let (Some(sent), Some(expected)) = (sent, expected) else {
        return false;
    };
    if expected.is_empty() || sent.len() != expected.len() {
        return false;
    }
    sent.bytes()
        .zip(expected.bytes())
        .fold(0u8, |diff, (a, b)| diff | (a ^ b))
        == 0
}

/// The script that hands an Extension page its token, run in the guest's top
/// frame once a page has loaded. The init script took the function before
/// the page's own scripts ran, and nothing can replace it.
pub fn token_script(token: &str) -> String {
    format!("window.__agentrqPanelToken && window.__agentrqPanelToken(\"{token}\")")
}

/// `scheme://host:port`, after the WebView2 form is undone, or `None` for
/// anything with no host.
fn origin_of(raw: &str, webview2_ext_urls: bool) -> Option<String> {
    let url = Url::parse(&normalize_ext_url(raw, webview2_ext_urls)).ok()?;
    url.has_host()
        .then(|| format!("{}://{}", url.scheme(), host_of(&url)))
}

/// Classify a request on the scheme.
///
/// `top_url` is the guest webview's own address, never anything the request
/// says; `origin` is the request's `Origin` header, which the browser sets
/// and a page cannot. The call is answered only when it is a `POST` to
/// [`BRIDGE_PATH`] of the very Extension whose page is open, carrying the
/// token the guest holds now (`expected`, [`same_token`]): only the init
/// script of the page in the top frame has it, and a navigation replaces it.
/// It counts as the main frame only when the frame that sent it is that
/// page's own origin: an `<iframe>` of a website, or of another Extension, is
/// refused, as Electron refuses every subframe. An `Origin` is checked
/// whenever there is one; WebKit sends none on a same-origin request to a
/// custom scheme, and the token stands for it then.
pub fn bridge_call(
    method: &str,
    request_url: &str,
    origin: Option<&str>,
    token: Option<&str>,
    expected: Option<&str>,
    top_url: &str,
    webview2_ext_urls: bool,
) -> BridgeCall {
    let Ok(request) = Url::parse(&normalize_ext_url(request_url, webview2_ext_urls)) else {
        return BridgeCall::File;
    };
    if request.path() != BRIDGE_PATH {
        return BridgeCall::File;
    }
    if method != "POST" {
        return BridgeCall::Refused("The panel bridge takes a POST.".to_string());
    }
    let owner = extension_of(top_url, webview2_ext_urls);
    if owner.is_empty() || host_of(&request) != owner {
        return BridgeCall::Refused("Only an extension’s own pages can send to it.".to_string());
    }
    if !same_token(token, expected) {
        return BridgeCall::Refused("That page cannot send to its extension.".to_string());
    }
    let page = origin_of(top_url, webview2_ext_urls);
    let sender = match origin {
        None => page.clone(),
        Some(origin) => origin_of(origin, webview2_ext_urls),
    };
    BridgeCall::Send {
        top_url: format!("{EXT_SCHEME}://{owner}/"),
        is_main_frame: page.is_some() && sender == page,
    }
}

/// The message a bridge request carries, or why it carries none.
pub fn bridge_message(body: &[u8]) -> Result<Value, Refusal> {
    if body.len() > MAX_MESSAGE_BYTES {
        return Err(Refusal {
            reason: "That message is larger than 1 MB.".to_string(),
        });
    }
    serde_json::from_slice(body).map_err(|_| Refusal {
        reason: "That message is not plain data.".to_string(),
    })
}

/// [`Sender`] for a classified call.
pub fn sender_of(top_url: &str, is_main_frame: bool) -> Sender<'_> {
    Sender {
        url: top_url,
        is_main_frame,
    }
}

/// What the page's `send` reads back: `{ ok, reply }` or `{ ok, reason }`, as
/// Electron's `agentrq:panel:send` answers.
pub fn bridge_reply(result: Result<Value, Refusal>) -> Vec<u8> {
    let value = match result {
        Ok(reply) => json!({ "ok": true, "reply": reply }),
        Err(refusal) => json!({ "ok": false, "reason": refusal.reason }),
    };
    value.to_string().into_bytes()
}

/// The guest's init script, from `src/guest/panel.js`: `__WEBVIEW2__` is
/// whether an Extension's page arrives as `http://agentrq-ext.<name>/`.
pub fn guest_init_script(source: &str, webview2_ext_urls: bool) -> String {
    source.replace(
        "__WEBVIEW2__",
        if webview2_ext_urls { "true" } else { "false" },
    )
}

/// What a guest's init script tells WebView2's shell when its page left a
/// key unhandled: this, and the key as `KeyboardEvent.key` spells it.
pub const KEY_MESSAGE: &str = "agentrq-panel-key:";

/// The key in a guest's message, or `None` for any other message.
pub fn reported_key(message: &str) -> Option<&str> {
    message.strip_prefix(KEY_MESSAGE)
}

/// What the main window hears about a guest.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(
    rename_all = "camelCase",
    rename_all_fields = "camelCase",
    tag = "kind"
)]
pub enum GuestEvent {
    /// It started or stopped loading.
    Loading {
        label: String,
        loading: bool,
        can_go_back: bool,
        can_go_forward: bool,
    },
    /// Its address changed; `in_page` when no document was loaded for it.
    Navigate {
        label: String,
        url: String,
        in_page: bool,
        can_go_back: bool,
        can_go_forward: bool,
    },
    /// A document could not be loaded.
    Fail {
        label: String,
        code: i64,
        description: String,
    },
    /// It took the keyboard, so the page's own focused element no longer has it.
    Focus { label: String },
    /// Find in page counted its matches: Electron's `found-in-page`.
    Found {
        label: String,
        active: u32,
        total: u32,
    },
}

/// The guest's address as the page should see it: the WebView2 form undone.
pub fn reported_url(raw: &str, webview2_ext_urls: bool) -> String {
    normalize_ext_url(raw, webview2_ext_urls).into_owned()
}

/// The address to hand the webview for `url`: on WebView2 an Extension's page
/// is loaded as `http://agentrq-ext.<name>/`, which wry serves from the scheme.
pub fn webview_url(url: &str, webview2_ext_urls: bool) -> String {
    let prefix = format!("{EXT_SCHEME}://");
    match url.strip_prefix(&prefix) {
        Some(rest) if webview2_ext_urls => format!("http://{EXT_SCHEME}.{rest}"),
        _ => url.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_guest_label_is_the_prefix_and_a_number() {
        assert_eq!(guest_label(7), "panel-guest-7");
        assert!(is_guest_label(&guest_label(0)));
        assert_eq!(guest_number(&guest_label(u32::MAX)), Some(u32::MAX));
        for label in [
            "main",
            "panel-guest-",
            "panel-guest-x",
            "panel-guest-1a",
            "panel-guest-+1",
            "panel-guest-99999999999",
            "xpanel-guest-1",
            "sign-in",
        ] {
            assert!(!is_guest_label(label), "{label}");
        }
    }

    #[test]
    fn the_data_dir_is_the_panels_never_the_profiles() {
        assert_eq!(panel_data_dir("p1"), "panel-p1");
        assert_ne!(panel_data_dir("p1"), "p1");
    }

    #[test]
    fn place_scales_css_pixels_by_the_zoom() {
        let rect = HostRect {
            x: 10.0,
            y: 20.0,
            width: 300.0,
            height: 400.0,
        };
        assert_eq!(
            place(rect, 1.25),
            Some(Placement {
                x: 12.5,
                y: 25.0,
                width: 375.0,
                height: 500.0,
            })
        );
        // A zoom that is not one counts as 100%.
        for zoom in [0.0, -1.0, f64::NAN, f64::INFINITY] {
            assert_eq!(place(rect, zoom).map(|p| p.width), Some(300.0), "{zoom}");
        }
    }

    #[test]
    fn place_refuses_what_is_not_a_box_and_never_goes_negative() {
        let rect = HostRect {
            x: f64::NAN,
            y: 0.0,
            width: 1.0,
            height: 1.0,
        };
        assert_eq!(place(rect, 1.0), None);
        let collapsed = HostRect {
            x: -5.0,
            y: 0.0,
            width: -1.0,
            height: -2.0,
        };
        assert_eq!(
            place(collapsed, 1.0),
            Some(Placement {
                x: -5.0,
                y: 0.0,
                width: 0.0,
                height: 0.0,
            })
        );
    }

    #[test]
    fn the_link_key_and_the_middle_button_mean_the_browser() {
        let control = Modifiers {
            control: true,
            ..Default::default()
        };
        let meta = Modifiers {
            meta: true,
            ..Default::default()
        };
        let middle = Modifiers {
            middle: true,
            ..Default::default()
        };
        assert_eq!(disposition(control, "linux"), "background-tab");
        assert_eq!(disposition(control, "win32"), "background-tab");
        assert_eq!(disposition(control, "darwin"), "");
        assert_eq!(disposition(meta, "darwin"), "background-tab");
        assert_eq!(disposition(meta, "linux"), "");
        assert_eq!(disposition(middle, "darwin"), "background-tab");
        assert_eq!(disposition(Modifiers::default(), "linux"), "");
    }

    #[test]
    fn only_srcdoc_is_a_frames_own_document() {
        assert!(is_srcdoc("about:srcdoc"));
        assert!(!is_srcdoc("about:blank"));
        assert!(!is_srcdoc("about:srcdoc#x"));
    }

    const PAGE: &str = "agentrq-ext://notes/panel/index.html";
    const OK: Option<&str> = Some("t0k3n");
    const SEND: &str = "agentrq-ext://notes/.agentrq/panel-send";

    fn send(main: bool) -> BridgeCall {
        BridgeCall::Send {
            top_url: "agentrq-ext://notes/".to_string(),
            is_main_frame: main,
        }
    }

    #[test]
    fn a_file_is_not_a_bridge_call() {
        for url in [
            "agentrq-ext://notes/panel/x.js",
            "not a url",
            "agentrq-ext://notes/.agentrq/panel-send/x",
        ] {
            assert_eq!(
                bridge_call("GET", url, None, OK, OK, PAGE, false),
                BridgeCall::File,
                "{url}"
            );
        }
    }

    #[test]
    fn the_page_itself_reaches_its_own_extension() {
        let call = bridge_call(
            "POST",
            SEND,
            Some("agentrq-ext://notes"),
            OK,
            OK,
            PAGE,
            false,
        );
        assert_eq!(call, send(true));
        // WebKit's same-origin request, which has no `Origin`.
        assert_eq!(
            bridge_call("POST", SEND, None, OK, OK, PAGE, false),
            send(true)
        );
    }

    #[test]
    fn no_call_without_the_token_the_guest_holds_now() {
        let refused = BridgeCall::Refused("That page cannot send to its extension.".to_string());
        let origin = Some("agentrq-ext://notes");
        for (sent, expected) in [
            // Missing: a frame, or a page the init script did not run in.
            (None, OK),
            (None, None),
            // Wrong.
            (Some("guess"), OK),
            (Some("t0k3m"), OK),
            (Some(""), Some("")),
            // Stale: the guest has navigated since, and holds none yet...
            (OK, None),
            // ...or a newer one.
            (OK, Some("n3w")),
        ] {
            assert_eq!(
                bridge_call("POST", SEND, origin, sent, expected, PAGE, false),
                refused,
                "{sent:?} for {expected:?}"
            );
            assert_eq!(
                bridge_call("POST", SEND, None, sent, expected, PAGE, false),
                refused,
                "no Origin is never the page on its own"
            );
        }
        // The token never makes another origin the page.
        assert_eq!(
            bridge_call(
                "POST",
                SEND,
                Some("https://evil.example"),
                OK,
                OK,
                PAGE,
                false
            ),
            send(false)
        );
    }

    #[test]
    fn tokens_are_hex_and_compared_whole() {
        let token = token_of([0xab; 32]);
        assert_eq!(token, "ab".repeat(32));
        assert_eq!(token_of([0; 32]), "0".repeat(64));
        assert!(same_token(Some(&token), Some(&token)));
        assert!(!same_token(Some(&token[..63]), Some(&token)));
        assert!(!same_token(Some(&token), None));
        assert!(!same_token(None, Some(&token)));
        assert_eq!(
            token_script("ab"),
            "window.__agentrqPanelToken && window.__agentrqPanelToken(\"ab\")"
        );
    }

    #[test]
    fn a_frame_of_another_origin_is_not_the_page() {
        for origin in [
            Some("https://evil.example"),
            Some("agentrq-ext://other"),
            Some("null"),
            Some("agentrq-ext://notes:1"),
        ] {
            let call = bridge_call("POST", SEND, origin, OK, OK, PAGE, false);
            assert_eq!(call, send(false), "{origin:?}");
        }
    }

    #[test]
    fn the_route_is_the_pages_own_address_never_the_requests() {
        let refused =
            BridgeCall::Refused("Only an extension’s own pages can send to it.".to_string());
        // Another Extension's bridge, from this one's page.
        let other = "agentrq-ext://other/.agentrq/panel-send";
        assert_eq!(
            bridge_call(
                "POST",
                other,
                Some("agentrq-ext://notes"),
                OK,
                OK,
                PAGE,
                false
            ),
            refused
        );
        // A website in the panel.
        assert_eq!(
            bridge_call(
                "POST",
                SEND,
                Some("https://a.example"),
                OK,
                OK,
                "https://a.example/",
                false
            ),
            refused
        );
        assert_eq!(
            bridge_call("GET", SEND, None, OK, OK, PAGE, false),
            BridgeCall::Refused("The panel bridge takes a POST.".to_string())
        );
    }

    #[test]
    fn webview2s_http_form_is_the_extension_only_where_webview2_serves_it() {
        let page = "http://agentrq-ext.notes/panel/index.html";
        let request = "http://agentrq-ext.notes/.agentrq/panel-send";
        assert_eq!(
            bridge_call(
                "POST",
                request,
                Some("http://agentrq-ext.notes"),
                OK,
                OK,
                page,
                true
            ),
            send(true)
        );
        // Elsewhere that address is a web page like any other.
        assert!(matches!(
            bridge_call(
                "POST",
                request,
                Some("http://agentrq-ext.notes"),
                OK,
                OK,
                page,
                false
            ),
            BridgeCall::Refused(_)
        ));
    }

    #[test]
    fn a_message_is_json_of_at_most_a_megabyte() {
        assert_eq!(bridge_message(br#"{"a":1}"#), Ok(json!({"a": 1})));
        assert_eq!(
            bridge_message(b"{"),
            Err(Refusal {
                reason: "That message is not plain data.".to_string()
            })
        );
        let big = format!("\"{}\"", "x".repeat(MAX_MESSAGE_BYTES));
        assert_eq!(
            bridge_message(big.as_bytes()),
            Err(Refusal {
                reason: "That message is larger than 1 MB.".to_string()
            })
        );
    }

    #[test]
    fn the_sender_is_what_was_classified() {
        let sender = sender_of("agentrq-ext://notes/", false);
        assert_eq!(
            (sender.url, sender.is_main_frame),
            ("agentrq-ext://notes/", false)
        );
    }

    #[test]
    fn the_reply_is_what_electrons_preload_reads() {
        let ok: Value = serde_json::from_slice(&bridge_reply(Ok(json!([1])))).unwrap();
        assert_eq!(ok, json!({"ok": true, "reply": [1]}));
        let refused = Err(Refusal {
            reason: "no".to_string(),
        });
        let no: Value = serde_json::from_slice(&bridge_reply(refused)).unwrap();
        assert_eq!(no, json!({"ok": false, "reason": "no"}));
    }

    #[test]
    fn the_init_script_learns_the_platforms_form() {
        assert_eq!(
            guest_init_script("const W = __WEBVIEW2__", true),
            "const W = true"
        );
        assert_eq!(
            guest_init_script("const W = __WEBVIEW2__", false),
            "const W = false"
        );
    }

    #[test]
    fn a_link_key_on_webkit_takes_a_web_page_to_the_browser() {
        let ctrl = Some(Modifiers {
            control: true,
            ..Modifiers::default()
        });
        let page = "https://a.example/";
        assert!(opens_elsewhere(page, ctrl, "linux", false));
        assert!(opens_elsewhere("http://a.example/", ctrl, "linux", false));
        // Not a click, not held, not the link key there, not WebView2, not
        // the web.
        assert!(!opens_elsewhere(page, None, "linux", false));
        assert!(!opens_elsewhere(
            page,
            Some(Modifiers::default()),
            "linux",
            false
        ));
        assert!(!opens_elsewhere(page, ctrl, "darwin", false));
        assert!(!opens_elsewhere(page, ctrl, "win32", true));
        assert!(!opens_elsewhere(
            "agentrq-ext://notes/",
            ctrl,
            "linux",
            false
        ));
    }

    #[test]
    fn only_a_persons_own_link_click_carries_its_keys() {
        let held = Modifiers {
            control: true,
            ..Modifiers::default()
        };
        let click = NavigationKind {
            link_clicked: true,
            user_gesture: true,
            held,
        };
        assert_eq!(link_click(click).map(|m| m.control), Some(true));
        // A script's location change or click(), a form, a frame's own load.
        for kind in [
            NavigationKind {
                user_gesture: false,
                ..click
            },
            NavigationKind {
                link_clicked: false,
                ..click
            },
            NavigationKind::default(),
        ] {
            assert!(link_click(kind).is_none());
            assert!(!opens_elsewhere(
                "https://a.example/",
                link_click(kind),
                "linux",
                false
            ));
        }
    }

    #[test]
    fn each_profiles_guests_keep_a_store_of_their_own() {
        assert_eq!(
            guest_store("p1", 14),
            GuestStore::Named(data_store_id("panel-p1"))
        );
        assert_ne!(
            guest_store("p1", 14),
            GuestStore::Named(data_store_id("p1")),
            "never the profile's own"
        );
        assert_eq!(
            guest_store("p2", 26),
            GuestStore::Named(data_store_id("panel-p2"))
        );
        // Before macOS 14 wry would hand out the default store, the app's.
        assert_eq!(guest_store("p1", 13), GuestStore::Ephemeral);
        assert_eq!(guest_store("p1", 0), GuestStore::Ephemeral);
    }

    #[test]
    fn the_apps_own_assets_and_ipc_are_never_a_guests() {
        for url in [
            "tauri://localhost/",
            "tauri://localhost/assets/index.js",
            "ipc://localhost/side_panel_guest_open",
            "http://tauri.localhost/",
            "https://tauri.localhost/index.html",
            "http://ipc.localhost/x",
        ] {
            assert!(is_app_internal(url), "{url}");
        }
        for url in [
            "https://a.example/",
            "https://tauri.localhost.example/",
            "agentrq-ext://notes/",
            "about:blank",
            "not a url",
        ] {
            assert!(!is_app_internal(url), "{url}");
        }
    }

    #[test]
    fn a_key_message_names_the_key() {
        assert_eq!(reported_key("agentrq-panel-key:r"), Some("r"));
        assert_eq!(reported_key("agentrq-panel-key:"), Some(""));
        assert_eq!(reported_key("{\"cmd\":\"x\"}"), None);
    }

    #[test]
    fn events_cross_in_camel_case() {
        let event = GuestEvent::Navigate {
            label: "panel-guest-1".to_string(),
            url: "https://a.example/".to_string(),
            in_page: true,
            can_go_back: true,
            can_go_forward: false,
        };
        assert_eq!(
            serde_json::to_value(&event).unwrap(),
            json!({"kind": "navigate", "label": "panel-guest-1", "url": "https://a.example/", "inPage": true, "canGoBack": true, "canGoForward": false})
        );
        let loading = GuestEvent::Loading {
            label: "l".to_string(),
            loading: true,
            can_go_back: false,
            can_go_forward: false,
        };
        assert_eq!(serde_json::to_value(&loading).unwrap()["kind"], "loading");
        let fail = GuestEvent::Fail {
            label: "l".to_string(),
            code: -2,
            description: "x".to_string(),
        };
        assert_eq!(serde_json::to_value(&fail).unwrap()["code"], -2);
        let focus = GuestEvent::Focus {
            label: "l".to_string(),
        };
        assert_eq!(
            serde_json::to_value(&focus).unwrap(),
            json!({"kind": "focus", "label": "l"})
        );
        let found = GuestEvent::Found {
            label: "l".to_string(),
            active: 1,
            total: 3,
        };
        assert_eq!(
            serde_json::to_value(&found).unwrap(),
            json!({"kind": "found", "label": "l", "active": 1, "total": 3})
        );
    }

    #[test]
    fn webview2_loads_an_extension_page_in_its_http_form() {
        let page = "agentrq-ext://notes/panel/index.html";
        assert_eq!(
            webview_url(page, true),
            "http://agentrq-ext.notes/panel/index.html"
        );
        assert_eq!(webview_url(page, false), page);
        assert_eq!(
            webview_url("https://a.example/", true),
            "https://a.example/"
        );
        assert_eq!(reported_url(&webview_url(page, true), true), page);
    }

    #[test]
    fn the_reported_url_undoes_webview2s_form() {
        assert_eq!(
            reported_url("http://agentrq-ext.notes/a", true),
            "agentrq-ext://notes/a"
        );
        assert_eq!(
            reported_url("http://agentrq-ext.notes/a", false),
            "http://agentrq-ext.notes/a"
        );
    }
}
