// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! An Extension and its own pages in the side panel, talking to each other.
//!
//! The page side is `window.agentrq.panel` (the panel preload); the Extension
//! side is [`PanelCapability`]. Who a message is for is decided by **the
//! address of the page that sent it** — `agentrq-ext://<extension>/…` reaches
//! that Extension and no other — and never by anything in the message. A
//! website in the panel has no bridge to begin with, and if it had one its
//! address would name no Extension, so it would reach nothing.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde::Serialize;
use serde_json::Value;
use url::Url;

use super::guest::{host_of, normalize_ext_url, EXT_SCHEME};

/// A message is a request between two pieces of this app, not a file transfer.
pub const MAX_MESSAGE_BYTES: usize = 1024 * 1024;

/// Why a call was refused.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Refusal {
    pub reason: String,
}

fn fail<T>(reason: impl Into<String>) -> Result<T, Refusal> {
    Err(Refusal {
        reason: reason.into(),
    })
}

/// The Extension an address belongs to, or `""` for anything that is not one
/// of its pages. `webview2_ext_urls` is as for [`normalize_ext_url`].
pub fn extension_of(raw_url: &str, webview2_ext_urls: bool) -> String {
    match Url::parse(&normalize_ext_url(raw_url, webview2_ext_urls)) {
        Ok(url) if url.scheme() == EXT_SCHEME => host_of(&url),
        _ => String::new(),
    }
}

/// A message as it will cross, or why it cannot. `text` is the message as
/// JSON, or `None` when it has none.
fn encode(text: Option<String>, owner: &str) -> Result<Value, Refusal> {
    let Some(text) = text else {
        return fail(format!("{owner}: that message is not plain data."));
    };
    if text.len() > MAX_MESSAGE_BYTES {
        return fail(format!("{owner}: that message is larger than 1 MB."));
    }
    // Text `serde_json` just wrote always reads back; `null` is the fallback
    // the type asks for, not a path anything takes.
    Ok(serde_json::from_str(&text).unwrap_or(Value::Null))
}

/// A message as JSON text; `None` for what has no JSON form.
fn json_text<T: Serialize + ?Sized>(message: &T) -> Option<String> {
    serde_json::to_string(message).ok()
}

/// The frame a `send` came from.
///
/// Electron gives this for free: the preload runs in the main frame only, and
/// `senderFrame.url` is that frame's own. Tauri reports only the webview's
/// top-level URL, so the shell **must** fill `is_main_frame` from the real
/// frame the IPC message came from; a subframe of an Extension page (an
/// `<iframe>` on any site) must say `false`, and is refused.
#[derive(Debug, Clone, Copy)]
pub struct Sender<'a> {
    pub url: &'a str,
    pub is_main_frame: bool,
}

/// A page open in the side panel.
pub trait PanelGuest {
    /// Whether the page has gone. A guest that cannot tell is not gone.
    fn is_destroyed(&self) -> bool {
        false
    }
    /// The page's current address.
    fn url(&self) -> String;
    /// Hand it a message on `channel`.
    fn send(&self, channel: &str, message: &Value);
}

/// Hand `message` to every open page of `owner`. Pages are guests whose
/// address names it; anything else in the panel is skipped. Returns how many
/// pages it reached.
pub fn deliver_to(
    guests: &[&dyn PanelGuest],
    owner: &str,
    channel: &str,
    message: &Value,
    webview2_ext_urls: bool,
) -> usize {
    let mut reached = 0;
    for guest in guests {
        if guest.is_destroyed() || extension_of(&guest.url(), webview2_ext_urls) != owner {
            continue;
        }
        guest.send(channel, message);
        reached += 1;
    }
    reached
}

/// A panel page an Extension declared.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PanelPage {
    pub id: String,
    pub label: String,
    /// Relative to the Extension's folder.
    pub entry: String,
}

/// What the hub needs from the app.
pub trait PanelHost: Send + Sync {
    /// Open the panel at an address.
    fn show(&self, url: &str);
    /// Hand a message to an Extension's open pages; how many it reached.
    fn deliver(&self, owner: &str, message: &Value) -> usize;
    /// The panel pages an Extension declared.
    fn pages_for(&self, owner: &str) -> Vec<PanelPage>;
}

/// The function an Extension's pages are answered by. `None` answers `null`.
pub type MessageHandler = Arc<dyn Fn(Value) -> Result<Option<Value>, String> + Send + Sync>;

/// Where `ctx.panel.open` goes: one of the Extension's pages, or a web page.
#[derive(Debug, Clone, Default)]
pub struct OpenTarget {
    pub page: Option<String>,
    pub url: Option<String>,
}

/// The hub every Extension's `ctx.panel` is closed over.
pub struct PanelHub<H: PanelHost> {
    host: H,
    /// See [`normalize_ext_url`]; the shell sets it to its `WEBVIEW2` const, true on Windows.
    webview2_ext_urls: bool,
    /// owner → the one handler its pages talk to. Never held across a call
    /// into an Extension's handler.
    handlers: Mutex<HashMap<String, MessageHandler>>,
}

/// `ctx.panel`, closed over one Extension's name.
pub struct PanelCapability<'a, H: PanelHost> {
    hub: &'a PanelHub<H>,
    owner: &'a str,
}

impl<H: PanelHost> PanelHub<H> {
    pub fn new(host: H, webview2_ext_urls: bool) -> Self {
        Self {
            host,
            webview2_ext_urls,
            handlers: Mutex::new(HashMap::new()),
        }
    }

    /// `ctx.panel` for one Extension.
    pub fn capability_for<'a>(&'a self, owner: &'a str) -> PanelCapability<'a, H> {
        PanelCapability { hub: self, owner }
    }

    /// A page's `window.agentrq.panel.send`, answered by its own Extension.
    ///
    /// `sender` is the frame the call came from — never the message.
    pub fn handle_send<T: Serialize + ?Sized>(
        &self,
        sender: Sender<'_>,
        message: &T,
    ) -> Result<Value, Refusal> {
        self.send_text(sender, json_text(message))
    }

    fn send_text(&self, sender: Sender<'_>, text: Option<String>) -> Result<Value, Refusal> {
        let owner = extension_of(sender.url, self.webview2_ext_urls);
        if !sender.is_main_frame || owner.is_empty() {
            return fail("Only an extension’s own pages can send to it.");
        }
        let handler = self.handlers.lock().unwrap().get(&owner).cloned();
        let Some(handler) = handler else {
            return fail(format!("{owner} does not listen to its page."));
        };

        let encoded = encode(text, &owner)?;
        let reply = match handler(encoded) {
            Ok(reply) => reply,
            Err(error) => return fail(format!("{owner} failed: {error}")),
        };
        encode(json_text(&reply.unwrap_or(Value::Null)), &owner)
    }

    /// The Extension stopped: its pages are no longer answered.
    pub fn retract(&self, owner: &str) {
        // Dropped after the lock: its captured state may call back into the hub.
        let old = self.handlers.lock().unwrap().remove(owner);
        drop(old);
    }
}

impl<H: PanelHost> PanelCapability<'_, H> {
    /// Open the panel at one of this Extension's pages, or at a web page.
    pub fn open(&self, target: OpenTarget) -> Result<(), Refusal> {
        let owner = self.owner;
        if let Some(id) = target.page {
            let pages = self.hub.host.pages_for(owner);
            let Some(page) = pages.iter().find(|candidate| candidate.id == id) else {
                return fail(format!("{owner} has no panel page \"{id}\"."));
            };
            self.hub
                .host
                .show(&format!("{EXT_SCHEME}://{owner}/{}", page.entry));
            return Ok(());
        }
        let url = Url::parse(target.url.as_deref().unwrap_or(""))
            .ok()
            .filter(|url| matches!(url.scheme(), "http" | "https"));
        let Some(url) = url else {
            return fail(format!(
                "{owner}: the side panel opens a page or an http(s) address."
            ));
        };
        self.hub.host.show(url.as_str());
        Ok(())
    }

    /// The one function this Extension's pages are answered by. Replaces any
    /// earlier one.
    pub fn on_message(&self, handler: MessageHandler) {
        let old = self
            .hub
            .handlers
            .lock()
            .unwrap()
            .insert(self.owner.to_string(), handler);
        // Dropped after the lock, as in `retract`.
        drop(old);
    }

    /// Send to this Extension's open pages. Dropped when none is open.
    /// Returns how many pages it reached.
    pub fn post<T: Serialize + ?Sized>(&self, message: &T) -> Result<usize, Refusal> {
        self.post_text(json_text(message))
    }

    fn post_text(&self, text: Option<String>) -> Result<usize, Refusal> {
        let encoded = encode(text, self.owner)?;
        Ok(self.hub.host.deliver(self.owner, &encoded))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::RefCell;
    use std::collections::BTreeMap;

    #[derive(Default)]
    struct FakeHost {
        shown: Mutex<Vec<String>>,
        delivered: Mutex<Vec<(String, Value)>>,
    }

    impl PanelHost for Arc<FakeHost> {
        fn show(&self, url: &str) {
            self.shown.lock().unwrap().push(url.to_string());
        }
        fn deliver(&self, owner: &str, message: &Value) -> usize {
            self.delivered
                .lock()
                .unwrap()
                .push((owner.to_string(), message.clone()));
            1
        }
        fn pages_for(&self, owner: &str) -> Vec<PanelPage> {
            if owner == "notes" {
                vec![PanelPage {
                    id: "board".into(),
                    label: "Board".into(),
                    entry: "panel/index.html".into(),
                }]
            } else {
                Vec::new()
            }
        }
    }

    fn setup() -> (PanelHub<Arc<FakeHost>>, Arc<FakeHost>) {
        let host = Arc::new(FakeHost::default());
        (PanelHub::new(host.clone(), false), host)
    }

    const NOTES_PAGE: &str = "agentrq-ext://notes/panel/index.html";

    fn main(url: &str) -> Sender<'_> {
        Sender {
            url,
            is_main_frame: true,
        }
    }

    fn reason<T: std::fmt::Debug>(result: Result<T, Refusal>) -> String {
        result.unwrap_err().reason
    }

    #[test]
    fn extension_of_names_the_extension_an_address_belongs_to() {
        assert_eq!(extension_of(NOTES_PAGE, false), "notes");
    }

    #[test]
    fn extension_of_names_none_for_the_web_the_app_or_junk() {
        assert_eq!(extension_of("https://notes/panel/index.html", false), "");
        assert_eq!(extension_of("app://agentrq/", false), "");
        assert_eq!(extension_of("not a url", false), "");
        assert_eq!(extension_of("", false), "");
    }

    #[test]
    fn extension_of_keeps_the_port_so_notes_1_is_not_notes() {
        assert_eq!(extension_of("agentrq-ext://notes:1/x", false), "notes:1");
    }

    #[test]
    fn open_opens_one_of_the_extensions_own_pages() {
        let (hub, host) = setup();
        assert_eq!(
            hub.capability_for("notes").open(OpenTarget {
                page: Some("board".into()),
                url: None
            }),
            Ok(())
        );
        assert_eq!(*host.shown.lock().unwrap(), [NOTES_PAGE]);
    }

    #[test]
    fn open_refuses_a_page_it_did_not_declare_with_its_name_on_it() {
        let (hub, host) = setup();
        let result = hub.capability_for("notes").open(OpenTarget {
            page: Some("secret".into()),
            url: None,
        });
        assert_eq!(reason(result), "notes has no panel page \"secret\".");
        assert!(host.shown.lock().unwrap().is_empty());
    }

    #[test]
    fn open_refuses_a_page_of_another_extension() {
        let (hub, host) = setup();
        let result = hub.capability_for("other").open(OpenTarget {
            page: Some("board".into()),
            url: None,
        });
        assert_eq!(reason(result), "other has no panel page \"board\".");
        assert!(host.shown.lock().unwrap().is_empty());
    }

    #[test]
    fn open_opens_a_web_page() {
        let (hub, host) = setup();
        let result = hub.capability_for("notes").open(OpenTarget {
            page: None,
            url: Some("https://example.com/docs".into()),
        });
        assert_eq!(result, Ok(()));
        assert_eq!(*host.shown.lock().unwrap(), ["https://example.com/docs"]);
    }

    #[test]
    fn open_refuses_anything_else() {
        let (hub, host) = setup();
        let urls = [
            Some("file:///etc/passwd"),
            Some("javascript:alert(1)"),
            Some("not a url"),
            None,
        ];
        for url in urls {
            let result = hub.capability_for("notes").open(OpenTarget {
                page: None,
                url: url.map(String::from),
            });
            assert_eq!(
                reason(result),
                "notes: the side panel opens a page or an http(s) address.",
                "{url:?}"
            );
        }
        assert!(host.shown.lock().unwrap().is_empty());
    }

    /// A handler that fails the test's expectation if it is ever reached: its
    /// reason, "reached", would show in the refusal.
    fn must_not_run() -> MessageHandler {
        Arc::new(|_| Err("reached".into()))
    }

    #[test]
    fn the_handler_that_must_not_run_says_so_when_it_does() {
        assert_eq!(must_not_run()(json!(1)), Err("reached".to_string()));
    }

    #[test]
    fn send_reaches_its_own_extension_and_gets_the_answer_back() {
        let (hub, _) = setup();
        let seen = Arc::new(Mutex::new(Vec::new()));
        let record = seen.clone();
        hub.capability_for("notes")
            .on_message(Arc::new(move |message| {
                record.lock().unwrap().push(message.clone());
                Ok(Some(json!({ "echoed": message })))
            }));
        assert_eq!(
            hub.handle_send(main(NOTES_PAGE), &json!({ "type": "refresh" })),
            Ok(json!({ "echoed": { "type": "refresh" } }))
        );
        assert_eq!(*seen.lock().unwrap(), [json!({ "type": "refresh" })]);
    }

    #[test]
    fn send_answers_with_null_when_the_handler_returns_nothing() {
        let (hub, _) = setup();
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(None)));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(Value::Null));
    }

    #[test]
    fn send_never_reaches_an_extension_from_a_web_page_whatever_the_message_says() {
        let (hub, _) = setup();
        // Were it reached, the reason would be "notes failed: reached".
        hub.capability_for("notes").on_message(must_not_run());
        let result = hub.handle_send(main("https://evil.example/"), &json!({ "to": "notes" }));
        assert_eq!(
            reason(result),
            "Only an extension’s own pages can send to it."
        );
    }

    #[test]
    fn send_never_reaches_another_extension() {
        let (hub, _) = setup();
        hub.capability_for("notes").on_message(must_not_run());
        hub.capability_for("other")
            .on_message(Arc::new(|_| Ok(Some(json!("other")))));
        assert_eq!(
            hub.handle_send(
                main("agentrq-ext://other/panel/x.html"),
                &json!({ "to": "notes" })
            ),
            Ok(json!("other"))
        );
    }

    #[test]
    fn send_says_so_when_the_extension_does_not_listen() {
        let (hub, _) = setup();
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &json!({}))),
            "notes does not listen to its page."
        );
    }

    #[test]
    fn send_replaces_a_handler() {
        // "needs a function" is not ported: the type has no other value.
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        panel.on_message(Arc::new(|_| Ok(Some(json!("first")))));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(json!("first")));
        panel.on_message(Arc::new(|_| Ok(Some(json!("second")))));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(json!("second")));
    }

    #[test]
    fn send_turns_a_failing_handler_into_a_refusal_with_the_extensions_name() {
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        panel.on_message(Arc::new(|_| Err("database unavailable".into())));
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &json!({}))),
            "notes failed: database unavailable"
        );
        panel.on_message(Arc::new(|_| Err("threw a bare string".into())));
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &json!({}))),
            "notes failed: threw a bare string"
        );
    }

    /// Not valid JSON: a map keyed by something that is not a string.
    fn not_data() -> BTreeMap<(u8, u8), u8> {
        BTreeMap::from([((1, 2), 3)])
    }

    #[test]
    fn send_refuses_a_message_over_1_mb_or_one_that_is_not_data_either_way() {
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        panel.on_message(must_not_run());
        let big = "x".repeat(MAX_MESSAGE_BYTES);
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &big)),
            "notes: that message is larger than 1 MB."
        );
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &not_data())),
            "notes: that message is not plain data."
        );

        panel.on_message(Arc::new(|_| Ok(Some(json!("y".repeat(MAX_MESSAGE_BYTES))))));
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), "hi")),
            "notes: that message is larger than 1 MB."
        );
    }

    /// A string whose JSON text is exactly `bytes` long (two quotes around it).
    fn json_of_len(bytes: usize) -> String {
        "x".repeat(bytes - 2)
    }

    #[test]
    fn the_1_mb_boundary_is_exact_in_both_directions_and_counts_bytes() {
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        let limit = "notes: that message is larger than 1 MB.";

        panel.on_message(Arc::new(|_| Ok(None)));
        assert!(hub
            .handle_send(main(NOTES_PAGE), &json_of_len(MAX_MESSAGE_BYTES))
            .is_ok());
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &json_of_len(MAX_MESSAGE_BYTES + 1))),
            limit
        );
        // Two-byte characters count as two: 2 quotes + 2 * 524287 = MAX.
        let wide = |chars: usize| "é".repeat(chars);
        assert!(hub
            .handle_send(main(NOTES_PAGE), &wide(MAX_MESSAGE_BYTES / 2 - 1))
            .is_ok());
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &wide(MAX_MESSAGE_BYTES / 2))),
            limit
        );

        // The reply is held to the same line.
        let reply_of = |bytes: usize| -> MessageHandler {
            let text = json_of_len(bytes);
            Arc::new(move |_| Ok(Some(json!(text))))
        };
        panel.on_message(reply_of(MAX_MESSAGE_BYTES));
        assert!(hub.handle_send(main(NOTES_PAGE), &1).is_ok());
        panel.on_message(reply_of(MAX_MESSAGE_BYTES + 1));
        assert_eq!(reason(hub.handle_send(main(NOTES_PAGE), &1)), limit);

        // And so is `post`.
        assert_eq!(panel.post(&json_of_len(MAX_MESSAGE_BYTES)), Ok(1));
        assert_eq!(
            reason(panel.post(&json_of_len(MAX_MESSAGE_BYTES + 1))),
            limit
        );
        assert_eq!(panel.post(&wide(MAX_MESSAGE_BYTES / 2 - 1)), Ok(1));
        assert_eq!(reason(panel.post(&wide(MAX_MESSAGE_BYTES / 2))), limit);
    }

    #[test]
    fn send_refuses_a_subframe_even_of_an_extensions_own_page() {
        let (hub, _) = setup();
        hub.capability_for("notes").on_message(must_not_run());
        let sender = Sender {
            url: NOTES_PAGE,
            is_main_frame: false,
        };
        assert_eq!(
            reason(hub.handle_send(sender, &json!({}))),
            "Only an extension’s own pages can send to it."
        );
        // The same page, in the main frame, is answered.
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(Some(json!("ok")))));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(json!("ok")));
    }

    #[test]
    fn send_answers_a_page_at_the_address_webview2_reports() {
        let host = Arc::new(FakeHost::default());
        let hub = PanelHub::new(host, true);
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(Some(json!("ok")))));
        assert_eq!(
            hub.handle_send(main("http://agentrq-ext.notes/panel/index.html"), &1),
            Ok(json!("ok"))
        );
        assert_eq!(extension_of("http://agentrq-ext.notes/x", true), "notes");
        assert_eq!(extension_of("HTTPS://AgentRQ-Ext.notes/x", true), "notes");
        assert_eq!(extension_of("agentrq-ext://notes/x", true), "notes");
        // Named the way wry names it: all after `agentrq-ext.` is the host.
        assert_eq!(
            extension_of("http://agentrq-ext.notes.evil.com/", true),
            "notes.evil.com"
        );
        assert_eq!(extension_of("http://agentrq-ext/", true), "");
        assert_eq!(extension_of("http://notes.agentrq-ext.example/", true), "");
    }

    #[test]
    fn without_the_webview2_flag_the_http_form_is_a_web_page_not_the_extension() {
        let (hub, _) = setup();
        hub.capability_for("notes").on_message(must_not_run());
        for url in [
            "http://agentrq-ext.notes/panel/index.html",
            "https://AgentRQ-Ext.notes/panel/index.html",
        ] {
            assert_eq!(
                reason(hub.handle_send(main(url), &1)),
                "Only an extension’s own pages can send to it.",
                "{url}"
            );
            assert_eq!(extension_of(url, false), "", "{url}");
        }
        assert_eq!(extension_of("http://agentrq-ext.notes/x", true), "notes");
    }
    /// Its `Drop` calls back into the hub, as a handler holding a session might.
    struct Session {
        hub: Arc<PanelHub<Arc<FakeHost>>>,
        retracts: &'static str,
    }

    impl Drop for Session {
        fn drop(&mut self) {
            self.hub.retract(self.retracts);
        }
    }

    #[test]
    fn dropping_a_handler_that_calls_back_into_the_hub_does_not_hang() {
        let (hub, _) = setup();
        let hub = Arc::new(hub);
        let handler = |retracts: &'static str| -> MessageHandler {
            let session = Session {
                hub: hub.clone(),
                retracts,
            };
            Arc::new(move |_| {
                let _ = &session;
                Ok(None)
            })
        };
        let (done, finished) = std::sync::mpsc::channel();
        let worker = {
            let hub = hub.clone();
            let first = handler("companion");
            let second = handler("companion");
            std::thread::spawn(move || {
                let panel = hub.capability_for("notes");
                // Replaced: the first handler is dropped by `on_message`.
                panel.on_message(first);
                panel.on_message(second);
                assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(Value::Null));
                // Removed: the last one is dropped by `retract`.
                hub.retract("notes");
                done.send(()).unwrap();
            })
        };
        assert!(finished
            .recv_timeout(std::time::Duration::from_secs(10))
            .is_ok());
        worker.join().unwrap();
    }

    #[test]
    fn send_stops_answering_once_the_extension_stops() {
        let (hub, _) = setup();
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(Some(json!("hi")))));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(json!("hi")));
        hub.retract("notes");
        assert_eq!(
            reason(hub.handle_send(main(NOTES_PAGE), &json!({}))),
            "notes does not listen to its page."
        );
    }

    #[test]
    fn send_does_not_hold_the_handler_table_while_the_handler_runs() {
        // A handler may call back into the hub (`retract`, `on_message`).
        let (hub, _) = setup();
        let hub = Arc::new(hub);
        let inner = hub.clone();
        hub.capability_for("notes").on_message(Arc::new(move |_| {
            inner.retract("notes");
            Ok(Some(json!("done")))
        }));
        assert_eq!(hub.handle_send(main(NOTES_PAGE), &1), Ok(json!("done")));
        assert!(hub.handle_send(main(NOTES_PAGE), &1).is_err());
    }

    #[test]
    fn post_delivers_plain_data_to_the_extensions_pages() {
        let (hub, host) = setup();
        assert_eq!(
            hub.capability_for("notes").post(&json!({ "count": 3 })),
            Ok(1)
        );
        assert_eq!(
            *host.delivered.lock().unwrap(),
            [("notes".to_string(), json!({ "count": 3 }))]
        );
    }

    #[test]
    fn post_refuses_what_cannot_cross() {
        let (hub, host) = setup();
        assert_eq!(
            reason(hub.capability_for("notes").post(&not_data())),
            "notes: that message is not plain data."
        );
        assert!(host.delivered.lock().unwrap().is_empty());
    }

    struct FakeGuest {
        url: &'static str,
        destroyed: Option<bool>,
        sent: RefCell<Vec<(String, Value)>>,
    }

    impl FakeGuest {
        fn new(url: &'static str, destroyed: Option<bool>) -> Self {
            Self {
                url,
                destroyed,
                sent: RefCell::new(Vec::new()),
            }
        }
    }

    impl PanelGuest for FakeGuest {
        fn is_destroyed(&self) -> bool {
            self.destroyed.unwrap_or(false)
        }
        fn url(&self) -> String {
            self.url.to_string()
        }
        fn send(&self, channel: &str, message: &Value) {
            self.sent
                .borrow_mut()
                .push((channel.to_string(), message.clone()));
        }
    }

    /// A guest that does not say whether it has gone: the trait's default.
    struct BareGuest(FakeGuest);

    impl PanelGuest for BareGuest {
        fn url(&self) -> String {
            self.0.url()
        }
        fn send(&self, channel: &str, message: &Value) {
            self.0.send(channel, message);
        }
    }

    #[test]
    fn deliver_to_skips_the_http_form_unless_webview2() {
        let lookalike = FakeGuest::new("http://agentrq-ext.notes/panel/x.html", Some(false));
        let guests: [&dyn PanelGuest; 1] = [&lookalike];
        let message = json!(1);
        assert_eq!(deliver_to(&guests, "notes", "c", &message, false), 0);
        assert_eq!(deliver_to(&guests, "notes", "c", &message, true), 1);
    }

    #[test]
    fn deliver_to_reaches_only_open_pages_of_that_extension() {
        let mine = FakeGuest::new(NOTES_PAGE, Some(false));
        let gone = FakeGuest::new(NOTES_PAGE, Some(true));
        let web = FakeGuest::new("https://example.com/", Some(false));
        let other = FakeGuest::new("agentrq-ext://other/panel/x.html", Some(false));
        let bare = BareGuest(FakeGuest::new("agentrq-ext://notes/panel/b.html", None));
        let guests: [&dyn PanelGuest; 5] = [&mine, &gone, &web, &other, &bare];

        let reached = deliver_to(
            &guests,
            "notes",
            "agentrq:panel:message",
            &json!({ "n": 1 }),
            false,
        );

        assert_eq!(reached, 2);
        let expected = vec![("agentrq:panel:message".to_string(), json!({ "n": 1 }))];
        assert_eq!(*mine.sent.borrow(), expected);
        assert_eq!(*bare.0.sent.borrow(), expected);
        for skipped in [&gone, &web, &other] {
            assert!(skipped.sent.borrow().is_empty());
        }
    }
}
