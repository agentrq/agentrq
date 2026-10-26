// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The `agentrq-ext` scheme: `desktop/src/main/side-panel/ext-scheme.js`, and
//! the panel bridge that Electron runs over IPC.
//!
//! A Tauri scheme is app-wide, so the first question is who is asking: only a
//! side panel guest is answered. A guest has no IPC either, so an Extension
//! page's `window.agentrq.panel.send` is a `POST` on this scheme, routed by
//! the guest's own address (`core::side_panel::child::bridge_call`), and
//! answered by Electron's own `bridge.js` in the extension host. Every
//! other request is a file of the Extension, which core serves from its
//! declared panel folder, after the realpath check, up to 16 MB.

use std::future::Future;
use std::pin::Pin;

use agentrq_desktop_core::side_panel::bridge::Refusal;
use agentrq_desktop_core::side_panel::child::{
    bridge_call, bridge_message, bridge_reply, is_guest_label, sender_url, BridgeCall, TOKEN_HEADER,
};
use agentrq_desktop_core::side_panel::ext_scheme::{serve_ext_request, Installation, PanelFs};
use http::{header, Request, Response};
use serde_json::Value;

/// What is installed right now: `extensions-installed.json`.
pub type Installed = Box<dyn Fn() -> Vec<Installation> + Send + Sync>;

/// `bridge.js`'s `handleSend(senderUrl, message)`: what the page's `send`
/// reads back, `{ ok, reply }` or `{ ok, reason }`.
pub type HandleSend =
    Box<dyn Fn(String, Value) -> Pin<Box<dyn Future<Output = Value> + Send>> + Send + Sync>;

pub struct PanelScheme {
    send: HandleSend,
    installed: Installed,
    fs: Box<dyn PanelFs + Send + Sync>,
    /// Whether this is WebView2: see `core::side_panel::guest::normalize_ext_url`.
    webview2_ext_urls: bool,
}

fn reply(status: u16, content_type: &str, body: Vec<u8>) -> Response<Vec<u8>> {
    let mut response = Response::new(body);
    *response.status_mut() = http::StatusCode::from_u16(status).unwrap_or_default();
    let value = header::HeaderValue::from_str(content_type)
        .unwrap_or(header::HeaderValue::from_static("text/plain"));
    response.headers_mut().insert(header::CONTENT_TYPE, value);
    response
}

fn refuse(status: u16, reason: &str) -> Response<Vec<u8>> {
    reply(
        status,
        "text/plain; charset=utf-8",
        reason.as_bytes().to_vec(),
    )
}

/// Tauri's own `tauri` scheme, which it would serve the app's assets on to
/// every webview, a guest's included. Nothing loads from it.
pub fn app_internal() -> Response<Vec<u8>> {
    refuse(403, "The app's own files are not served here.")
}

/// What the shell knows about the page a request came from.
#[derive(Debug, Clone, Copy, Default)]
pub struct Page<'a> {
    /// The guest's own address, `None` once it has gone.
    pub top_url: Option<&'a str>,
    /// The bridge token the guest holds now, `None` when it holds none.
    pub token: Option<&'a str>,
}

impl PanelScheme {
    pub fn new(
        send: HandleSend,
        installed: Installed,
        fs: Box<dyn PanelFs + Send + Sync>,
        webview2_ext_urls: bool,
    ) -> Self {
        Self {
            send,
            installed,
            fs,
            webview2_ext_urls,
        }
    }

    /// Answer `request`, made by the webview labelled `label`, which shows
    /// `page`.
    pub async fn handle(
        &self,
        label: &str,
        page: Page<'_>,
        request: &Request<Vec<u8>>,
    ) -> Response<Vec<u8>> {
        if !is_guest_label(label) {
            return refuse(403, "Extension pages open in the side panel only.");
        }
        let url = request.uri().to_string();
        let named = |name| {
            request
                .headers()
                .get(name)
                .and_then(|value: &header::HeaderValue| value.to_str().ok())
        };
        match bridge_call(
            request.method().as_str(),
            &url,
            named(header::ORIGIN.as_str()),
            named(TOKEN_HEADER),
            page.token,
            page.top_url.unwrap_or(""),
            self.webview2_ext_urls,
        ) {
            BridgeCall::Refused(reason) => refuse(403, &reason),
            BridgeCall::Send {
                owner,
                is_main_frame,
            } => {
                let body = match bridge_message(request.body(), &owner) {
                    // Electron answers a subframe so, whatever it sent.
                    _ if !is_main_frame => bridge_reply(Err(Refusal {
                        reason: "Only an extension’s own pages can send to it.".to_string(),
                    })),
                    Err(refusal) => bridge_reply(Err(refusal)),
                    Ok(message) => {
                        let answer = (self.send)(sender_url(&owner), message).await;
                        answer.to_string().into_bytes()
                    }
                };
                reply(200, "application/json; charset=utf-8", body)
            }
            BridgeCall::File => {
                let file = serve_ext_request(&url, &(self.installed)(), self.fs.as_ref());
                let mut response = reply(file.status, &file.content_type, file.body);
                if let Some(cache) = file.cache_control {
                    response.headers_mut().insert(
                        header::CACHE_CONTROL,
                        header::HeaderValue::from_static(cache),
                    );
                }
                response
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use agentrq_desktop_core::side_panel::ext_scheme::{PanelEntry, StdFs};
    use serde_json::json;
    use std::sync::{Arc, Mutex};

    const PAGE: &str = "agentrq-ext://notes/panel/index.html";
    const TOKEN: &str = "t0k3n";
    const AT_PAGE: Page<'static> = Page {
        top_url: Some(PAGE),
        token: Some(TOKEN),
    };

    struct Rig {
        scheme: PanelScheme,
        /// What the host was asked: the sender's address and the message.
        sent: Arc<Mutex<Vec<(String, Value)>>>,
        _dir: tempfile::TempDir,
    }

    impl Rig {
        fn handle(
            &self,
            label: &str,
            page: Page<'_>,
            request: &Request<Vec<u8>>,
        ) -> Response<Vec<u8>> {
            tauri::async_runtime::block_on(self.scheme.handle(label, page, request))
        }
    }

    fn rig() -> Rig {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("panel")).unwrap();
        std::fs::write(dir.path().join("panel/index.html"), "<p>notes</p>").unwrap();
        std::fs::write(dir.path().join("index.js"), "secret code").unwrap();
        let notes = Installation {
            name: "notes".to_string(),
            dir: dir.path().to_path_buf(),
            enabled: None,
            panels: vec![PanelEntry {
                entry: "panel/index.html".to_string(),
            }],
        };
        let sent = Arc::new(Mutex::new(Vec::new()));
        let to = Arc::clone(&sent);
        let scheme = PanelScheme::new(
            Box::new(move |sender, message| {
                to.lock().unwrap().push((sender, message.clone()));
                Box::pin(async move { json!({"ok": true, "reply": {"echo": message}}) })
            }),
            Box::new(move || vec![notes.clone()]),
            Box::new(StdFs),
            false,
        );
        Rig {
            scheme,
            sent,
            _dir: dir,
        }
    }

    fn get(url: &str) -> Request<Vec<u8>> {
        Request::builder().uri(url).body(Vec::new()).unwrap()
    }

    fn post(url: &str, origin: &str, body: &str) -> Request<Vec<u8>> {
        Request::builder()
            .method("POST")
            .uri(url)
            .header("origin", origin)
            .header(TOKEN_HEADER, TOKEN)
            .body(body.as_bytes().to_vec())
            .unwrap()
    }

    fn text(response: &Response<Vec<u8>>) -> String {
        String::from_utf8_lossy(response.body()).into_owned()
    }

    fn json_of(response: &Response<Vec<u8>>) -> Value {
        serde_json::from_slice(response.body()).unwrap()
    }

    #[test]
    fn answers_only_a_side_panel_guest() {
        let rig = rig();
        for label in ["main", "sign-in", "panel-guest-"] {
            let res = rig.handle(label, AT_PAGE, &get(PAGE));
            assert_eq!(res.status(), 403, "{label}");
            assert_eq!(text(&res), "Extension pages open in the side panel only.");
        }
    }

    #[test]
    fn serves_a_file_of_a_panel_folder_uncached() {
        let rig = rig();
        let res = rig.handle("panel-guest-1", AT_PAGE, &get(PAGE));
        assert_eq!(res.status(), 200);
        assert_eq!(text(&res), "<p>notes</p>");
        assert_eq!(res.headers()["content-type"], "text/html; charset=utf-8");
        assert_eq!(res.headers()["cache-control"], "no-cache");
    }

    #[test]
    fn never_serves_the_extensions_own_code() {
        let rig = rig();
        let res = rig.handle(
            "panel-guest-1",
            AT_PAGE,
            &get("agentrq-ext://notes/index.js"),
        );
        assert_eq!(res.status(), 404);
        assert!(res.headers().get("cache-control").is_none());
        assert!(!text(&res).contains("secret"));
    }

    #[test]
    fn a_page_reaches_its_own_extension_as_its_own_page() {
        let rig = rig();
        let send = "agentrq-ext://notes/.agentrq/panel-send";
        let res = rig.handle(
            "panel-guest-1",
            AT_PAGE,
            &post(send, "agentrq-ext://notes", "[1]"),
        );
        assert_eq!(res.status(), 200);
        assert_eq!(json_of(&res), json!({"ok": true, "reply": {"echo": [1]}}));
        // The sender is the guest's page, whatever the message says.
        assert_eq!(
            *rig.sent.lock().unwrap(),
            vec![("agentrq-ext://notes/".to_string(), json!([1]))]
        );
    }

    #[test]
    fn what_no_extension_may_hear_never_reaches_the_host() {
        let rig = rig();
        let send = "agentrq-ext://notes/.agentrq/panel-send";
        let big = format!("\"{}\"", "x".repeat(1024 * 1024));
        for (origin, body, reason) in [
            // A frame of a website inside the page is not the page.
            (
                "https://evil.example",
                "[1]",
                "Only an extension’s own pages can send to it.",
            ),
            (
                "agentrq-ext://notes",
                "{",
                "notes: that message is not plain data.",
            ),
            (
                "agentrq-ext://notes",
                big.as_str(),
                "notes: that message is larger than 1 MB.",
            ),
        ] {
            let res = rig.handle("panel-guest-1", AT_PAGE, &post(send, origin, body));
            assert_eq!(res.status(), 200, "{reason}");
            assert_eq!(json_of(&res), json!({"ok": false, "reason": reason}));
        }
        assert!(rig.sent.lock().unwrap().is_empty());
    }

    #[test]
    fn a_call_is_refused_when_the_page_is_not_that_extension() {
        let rig = rig();
        let send = post(
            "agentrq-ext://notes/.agentrq/panel-send",
            "agentrq-ext://notes",
            "{}",
        );
        for top_url in [None, Some("https://a.example/")] {
            let page = Page {
                top_url,
                token: Some(TOKEN),
            };
            let res = rig.handle("panel-guest-1", page, &send);
            assert_eq!(res.status(), 403, "{top_url:?}");
        }
    }

    #[test]
    fn a_call_needs_the_token_the_guest_holds_now() {
        let rig = rig();
        let send = "agentrq-ext://notes/.agentrq/panel-send";
        let bare = |token: Option<&str>| {
            let builder = Request::builder().method("POST").uri(send);
            let builder = match token {
                Some(token) => builder.header(TOKEN_HEADER, token),
                None => builder,
            };
            builder.body(b"1".to_vec()).unwrap()
        };
        // The page's own same-origin call: no Origin, its token.
        let res = rig.handle("panel-guest-1", AT_PAGE, &bare(Some(TOKEN)));
        assert_eq!(json_of(&res), json!({"ok": true, "reply": {"echo": 1}}));
        let refused = "That page cannot send to its extension.";
        for (token, page) in [
            // Missing.
            (None, AT_PAGE),
            // Wrong.
            (Some("guess"), AT_PAGE),
            // Stale: the guest has navigated, and holds none yet, or a new one.
            (
                Some(TOKEN),
                Page {
                    token: None,
                    ..AT_PAGE
                },
            ),
            (
                Some(TOKEN),
                Page {
                    token: Some("n3w"),
                    ..AT_PAGE
                },
            ),
        ] {
            let res = rig.handle("panel-guest-1", page, &bare(token));
            assert_eq!(res.status(), 403, "{token:?} for {page:?}");
            assert_eq!(text(&res), refused);
        }
    }

    #[test]
    fn a_file_outside_the_panel_folder_is_never_served() {
        let rig = rig();
        let outside = tempfile::tempdir().unwrap();
        std::fs::write(outside.path().join("secret.txt"), "secret").unwrap();
        let panel = rig._dir.path().join("panel");
        #[cfg(unix)]
        std::os::unix::fs::symlink(outside.path().join("secret.txt"), panel.join("link.txt"))
            .unwrap();
        #[cfg(windows)]
        let _ = std::os::windows::fs::symlink_file(
            outside.path().join("secret.txt"),
            panel.join("link.txt"),
        );
        for url in [
            "agentrq-ext://notes/panel/../index.js",
            "agentrq-ext://notes/panel/%2e%2e/index.js",
            "agentrq-ext://notes/panel/link.txt",
        ] {
            let res = rig.handle("panel-guest-1", AT_PAGE, &get(url));
            assert_eq!(res.status(), 404, "{url}");
            assert!(!text(&res).contains("secret"), "{url}");
        }
    }

    #[test]
    fn the_apps_own_scheme_answers_nothing() {
        let res = app_internal();
        assert_eq!(res.status(), 403);
        assert_eq!(text(&res), "The app's own files are not served here.");
    }

    #[test]
    fn an_unknown_status_or_type_still_answers() {
        assert_eq!(reply(1000, "x", Vec::new()).status(), 200);
        assert_eq!(
            reply(200, "bad\nvalue", Vec::new()).headers()["content-type"],
            "text/plain"
        );
    }
}
