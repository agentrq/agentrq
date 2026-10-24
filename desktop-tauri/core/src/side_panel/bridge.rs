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

use super::guest::{host_of, EXT_SCHEME};

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
/// of its pages.
pub fn extension_of(raw_url: &str) -> String {
    match Url::parse(raw_url) {
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
) -> usize {
    let mut reached = 0;
    for guest in guests {
        if guest.is_destroyed() || extension_of(&guest.url()) != owner {
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
    pub fn new(host: H) -> Self {
        Self {
            host,
            handlers: Mutex::new(HashMap::new()),
        }
    }

    /// `ctx.panel` for one Extension.
    pub fn capability_for<'a>(&'a self, owner: &'a str) -> PanelCapability<'a, H> {
        PanelCapability { hub: self, owner }
    }

    /// A page's `window.agentrq.panel.send`, answered by its own Extension.
    ///
    /// `sender_url` is `event.senderFrame.url` — never the message.
    pub fn handle_send<T: Serialize + ?Sized>(
        &self,
        sender_url: &str,
        message: &T,
    ) -> Result<Value, Refusal> {
        self.send_text(sender_url, json_text(message))
    }

    fn send_text(&self, sender_url: &str, text: Option<String>) -> Result<Value, Refusal> {
        let owner = extension_of(sender_url);
        if owner.is_empty() {
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
        self.handlers.lock().unwrap().remove(owner);
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
        self.hub
            .handlers
            .lock()
            .unwrap()
            .insert(self.owner.to_string(), handler);
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
        (PanelHub::new(host.clone()), host)
    }

    const NOTES_PAGE: &str = "agentrq-ext://notes/panel/index.html";

    fn reason<T: std::fmt::Debug>(result: Result<T, Refusal>) -> String {
        result.unwrap_err().reason
    }

    #[test]
    fn extension_of_names_the_extension_an_address_belongs_to() {
        assert_eq!(extension_of(NOTES_PAGE), "notes");
    }

    #[test]
    fn extension_of_names_none_for_the_web_the_app_or_junk() {
        assert_eq!(extension_of("https://notes/panel/index.html"), "");
        assert_eq!(extension_of("app://agentrq/"), "");
        assert_eq!(extension_of("not a url"), "");
        assert_eq!(extension_of(""), "");
    }

    #[test]
    fn extension_of_keeps_the_port_so_notes_1_is_not_notes() {
        assert_eq!(extension_of("agentrq-ext://notes:1/x"), "notes:1");
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
            hub.handle_send(NOTES_PAGE, &json!({ "type": "refresh" })),
            Ok(json!({ "echoed": { "type": "refresh" } }))
        );
        assert_eq!(*seen.lock().unwrap(), [json!({ "type": "refresh" })]);
    }

    #[test]
    fn send_answers_with_null_when_the_handler_returns_nothing() {
        let (hub, _) = setup();
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(None)));
        assert_eq!(hub.handle_send(NOTES_PAGE, &1), Ok(Value::Null));
    }

    #[test]
    fn send_never_reaches_an_extension_from_a_web_page_whatever_the_message_says() {
        let (hub, _) = setup();
        // Were it reached, the reason would be "notes failed: reached".
        hub.capability_for("notes").on_message(must_not_run());
        let result = hub.handle_send("https://evil.example/", &json!({ "to": "notes" }));
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
                "agentrq-ext://other/panel/x.html",
                &json!({ "to": "notes" })
            ),
            Ok(json!("other"))
        );
    }

    #[test]
    fn send_says_so_when_the_extension_does_not_listen() {
        let (hub, _) = setup();
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, &json!({}))),
            "notes does not listen to its page."
        );
    }

    #[test]
    fn send_replaces_a_handler() {
        // "needs a function" is not ported: the type has no other value.
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        panel.on_message(Arc::new(|_| Ok(Some(json!("first")))));
        assert_eq!(hub.handle_send(NOTES_PAGE, &1), Ok(json!("first")));
        panel.on_message(Arc::new(|_| Ok(Some(json!("second")))));
        assert_eq!(hub.handle_send(NOTES_PAGE, &1), Ok(json!("second")));
    }

    #[test]
    fn send_turns_a_failing_handler_into_a_refusal_with_the_extensions_name() {
        let (hub, _) = setup();
        let panel = hub.capability_for("notes");
        panel.on_message(Arc::new(|_| Err("database unavailable".into())));
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, &json!({}))),
            "notes failed: database unavailable"
        );
        panel.on_message(Arc::new(|_| Err("threw a bare string".into())));
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, &json!({}))),
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
            reason(hub.handle_send(NOTES_PAGE, &big)),
            "notes: that message is larger than 1 MB."
        );
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, &not_data())),
            "notes: that message is not plain data."
        );

        panel.on_message(Arc::new(|_| Ok(Some(json!("y".repeat(MAX_MESSAGE_BYTES))))));
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, "hi")),
            "notes: that message is larger than 1 MB."
        );
    }

    #[test]
    fn send_stops_answering_once_the_extension_stops() {
        let (hub, _) = setup();
        hub.capability_for("notes")
            .on_message(Arc::new(|_| Ok(Some(json!("hi")))));
        assert_eq!(hub.handle_send(NOTES_PAGE, &1), Ok(json!("hi")));
        hub.retract("notes");
        assert_eq!(
            reason(hub.handle_send(NOTES_PAGE, &json!({}))),
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
        assert_eq!(hub.handle_send(NOTES_PAGE, &1), Ok(json!("done")));
        assert!(hub.handle_send(NOTES_PAGE, &1).is_err());
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
