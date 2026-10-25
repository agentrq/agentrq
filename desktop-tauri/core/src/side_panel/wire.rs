// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Attaches the side panel's rules to the main window.
//!
//! Kept apart from the window code so the rules are tested against a fake host
//! rather than trusted to a file that needs a live webview: what a guest is
//! created with, where its links go, and that it never opens a window of its
//! own. The webview's events arrive as calls on [`SidePanel`]; what it does
//! about them goes back out through [`SidePanelHost`].

use serde_json::{Map, Value};

use super::guest::{
    harden_guest, navigation_target, open_target, GuestOptions, HardenResult, NavigationOptions,
    OpenDetails, PanelTarget,
};

/// What the panel needs from the app.
pub trait SidePanelHost {
    /// The active profile's panel partition.
    fn partition(&self) -> String;
    /// Absolute path of the guest preload.
    fn preload(&self) -> String;
    fn server_url(&self) -> String;
    /// The main window's own link rule.
    fn route_link(&self, url: &str);
    fn warn(&self, message: &str);
    /// Load an address in a guest.
    fn load_url(&self, guest: u32, url: &str);
    /// Tell the app's own find bar.
    fn send_find(&self);
    /// Whether the main window has gone. A host that cannot tell has not lost it.
    fn window_destroyed(&self) -> bool {
        false
    }
    /// A page has gone away, so a permission question it left waiting is
    /// answered. A host with no permission broker has none to answer.
    fn cancel_permissions(&self, _guest: u32) {}
}

/// A key press in a guest.
#[derive(Debug, Clone, Default)]
pub struct KeyInput {
    /// `keyDown` or `keyUp`.
    pub kind: String,
    pub key: String,
    pub shift: bool,
    pub alt: bool,
    pub control: bool,
    pub meta: bool,
}

/// Whether a key is the find shortcut: Cmd+F on macOS, Ctrl+F elsewhere, with
/// no other modifier. `platform` is `process.platform`'s name for it.
pub fn is_find_shortcut(input: &KeyInput, platform: &str) -> bool {
    if input.kind != "keyDown" || input.key.to_lowercase() != "f" {
        return false;
    }
    if input.shift || input.alt {
        return false;
    }
    if platform == "darwin" {
        input.meta && !input.control
    } else {
        input.control && !input.meta
    }
}

/// What a guest navigation reports.
#[derive(Debug, Clone, Copy)]
pub struct NavigationStart {
    pub is_main_frame: bool,
    pub is_same_document: bool,
}

/// What to do with a guest's own handling of a key.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KeyAction {
    /// The page keeps it.
    Pass,
    /// The page must not see it.
    Prevent,
}

/// What to do with a guest's navigation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NavigationAction {
    Allow,
    Prevent,
}

/// What to do with a guest's request for a window. Always a denial: no
/// guest ever gets a second window.
pub const DENY: &str = "deny";

/// The side panel's rules, attached to one window.
pub struct SidePanel<H: SidePanelHost> {
    host: H,
    platform: String,
    webview2_ext_urls: bool,
}

impl<H: SidePanelHost> SidePanel<H> {
    /// `platform` is `process.platform`. `webview2_ext_urls` is
    /// True on Windows (the shell's `WEBVIEW2` const): see `normalize_ext_url`.
    pub fn new(host: H, platform: &str, webview2_ext_urls: bool) -> Self {
        Self {
            host,
            platform: platform.to_string(),
            webview2_ext_urls,
        }
    }

    /// `will-attach-webview`: harden the guest, or refuse it. Returns whether
    /// it may be attached; a refusal is logged with its reason.
    pub fn will_attach_webview(
        &self,
        web_preferences: &mut Map<String, Value>,
        params: &mut Map<String, Value>,
    ) -> bool {
        let partition = self.host.partition();
        let preload = self.host.preload();
        let options = GuestOptions {
            partition: &partition,
            preload: &preload,
        };
        match harden_guest(web_preferences, params, options) {
            HardenResult::Ok => true,
            HardenResult::Refused { reason } => {
                self.host.warn(&format!("[side-panel] {reason}"));
                false
            }
        }
    }

    /// `did-start-navigation`. A question about a page that has gone is about
    /// nothing: a new document answers it with a Block. Moving within the same
    /// document — a hash, a pushState — is still the page that asked. A start
    /// that says nothing about itself is a new document too.
    pub fn did_start_navigation(&self, guest: u32, details: Option<NavigationStart>) {
        let is_main_frame = details.is_none_or(|d| d.is_main_frame);
        let is_same_document = details.is_some_and(|d| d.is_same_document);
        if is_main_frame && !is_same_document {
            self.host.cancel_permissions(guest);
        }
    }

    /// `destroyed`: the panel closed or the page went away.
    pub fn guest_destroyed(&self, guest: u32) {
        self.host.cancel_permissions(guest);
    }

    /// `before-input-event`. Cmd/Ctrl+F while the page has focus. The keys go
    /// to the guest, which is a different web contents from the app, so the
    /// app's own find bar has to be told — and the page must not see them, or
    /// its own find would open too.
    pub fn before_input(&self, input: &KeyInput) -> KeyAction {
        if !is_find_shortcut(input, &self.platform) {
            return KeyAction::Pass;
        }
        if !self.host.window_destroyed() {
            self.host.send_find();
        }
        KeyAction::Prevent
    }

    fn navigation_options<'a>(&self, server_url: &'a str) -> NavigationOptions<'a> {
        NavigationOptions {
            server_url,
            webview2_ext_urls: self.webview2_ext_urls,
        }
    }

    /// `setWindowOpenHandler`. Never a second window. A page that asks for one
    /// gets the panel, or the browser when somebody Cmd/Ctrl-clicked. The
    /// answer is always [`DENY`].
    pub fn window_open(&self, guest: u32, url: &str, disposition: &str) -> &'static str {
        let server_url = self.host.server_url();
        let details = OpenDetails { url, disposition };
        match open_target(details, self.navigation_options(&server_url)) {
            PanelTarget::Panel => self.host.load_url(guest, url),
            PanelTarget::Blocked => {}
            _ => self.host.route_link(url),
        }
        DENY
    }

    /// `will-navigate`. The caller reads the URL off the event first, and only
    /// then off the legacy second argument.
    pub fn will_navigate(&self, url: &str) -> NavigationAction {
        let server_url = self.host.server_url();
        let target = navigation_target(url, self.navigation_options(&server_url));
        match target {
            PanelTarget::Panel => NavigationAction::Allow,
            PanelTarget::Blocked => NavigationAction::Prevent,
            _ => {
                self.host.route_link(url);
                NavigationAction::Prevent
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::{Cell, RefCell};
    use std::rc::Rc;

    const SERVER_URL: &str = "https://app.agentrq.com";

    /// Everything the panel did to its host.
    #[derive(Default)]
    struct Calls {
        routed: RefCell<Vec<String>>,
        warned: RefCell<Vec<String>>,
        loaded: RefCell<Vec<(u32, String)>>,
        find: Cell<u32>,
        cancelled: RefCell<Vec<u32>>,
    }

    struct FakeHost {
        calls: Rc<Calls>,
        destroyed: bool,
    }

    impl SidePanelHost for FakeHost {
        fn partition(&self) -> String {
            "persist:panel-p1".into()
        }
        fn preload(&self) -> String {
            "/app/panel.cjs".into()
        }
        fn server_url(&self) -> String {
            SERVER_URL.into()
        }
        fn route_link(&self, url: &str) {
            self.calls.routed.borrow_mut().push(url.into());
        }
        fn warn(&self, message: &str) {
            self.calls.warned.borrow_mut().push(message.into());
        }
        fn load_url(&self, guest: u32, url: &str) {
            self.calls.loaded.borrow_mut().push((guest, url.into()));
        }
        fn send_find(&self) {
            self.calls.find.set(self.calls.find.get() + 1);
        }
        fn window_destroyed(&self) -> bool {
            self.destroyed
        }
        fn cancel_permissions(&self, guest: u32) {
            self.calls.cancelled.borrow_mut().push(guest);
        }
    }

    /// A host that leaves the optional parts to their defaults: no way to
    /// tell a window has gone, and no permission broker.
    struct BareHost;

    impl SidePanelHost for BareHost {
        fn partition(&self) -> String {
            "p".into()
        }
        fn preload(&self) -> String {
            "/p.cjs".into()
        }
        fn server_url(&self) -> String {
            String::new()
        }
        fn route_link(&self, _: &str) {}
        fn warn(&self, _: &str) {}
        fn load_url(&self, _: u32, _: &str) {}
        fn send_find(&self) {}
    }

    fn setup(platform: &str, destroyed: bool) -> (SidePanel<FakeHost>, Rc<Calls>) {
        let calls = Rc::new(Calls::default());
        let host = FakeHost {
            calls: calls.clone(),
            destroyed,
        };
        (SidePanel::new(host, platform, false), calls)
    }

    fn key(key: &str, modifiers: &str) -> KeyInput {
        KeyInput {
            kind: "keyDown".into(),
            key: key.into(),
            shift: modifiers.contains("shift"),
            alt: modifiers.contains("alt"),
            control: modifiers.contains("control"),
            meta: modifiers.contains("meta"),
        }
    }

    fn object(value: Value) -> Map<String, Value> {
        value.as_object().cloned().unwrap_or_default()
    }

    #[test]
    fn find_shortcut_is_cmd_f_on_macos_and_ctrl_f_elsewhere() {
        assert!(is_find_shortcut(&key("f", "meta"), "darwin"));
        assert!(is_find_shortcut(&key("F", "meta"), "darwin"));
        assert!(!is_find_shortcut(&key("f", "control"), "darwin"));
        assert!(is_find_shortcut(&key("f", "control"), "linux"));
        assert!(is_find_shortcut(&key("f", "control"), "win32"));
        assert!(!is_find_shortcut(&key("f", "meta"), "linux"));
    }

    #[test]
    fn find_shortcut_is_not_another_key_a_release_or_the_shortcut_with_another_modifier() {
        assert!(!is_find_shortcut(&key("g", "meta"), "darwin"));
        assert!(!is_find_shortcut(&key("f", ""), "darwin"));
        let release = KeyInput {
            kind: "keyUp".into(),
            ..key("f", "meta")
        };
        assert!(!is_find_shortcut(&release, "darwin"));
        assert!(!is_find_shortcut(&key("f", "meta shift"), "darwin"));
        assert!(!is_find_shortcut(&key("f", "meta alt"), "darwin"));
        assert!(!is_find_shortcut(&key("f", "meta control"), "darwin"));
        assert!(!is_find_shortcut(&key("f", "control meta"), "linux"));
        // `undefined` in the original is not representable here: an input is
        // always a `KeyInput`.
        assert!(!is_find_shortcut(&KeyInput::default(), "linux"));
    }

    #[test]
    fn takes_the_find_shortcut_from_the_page_and_tells_the_app() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(panel.before_input(&key("f", "meta")), KeyAction::Prevent);
        assert_eq!(calls.find.get(), 1);
    }

    #[test]
    fn leaves_every_other_key_to_the_page() {
        let (panel, calls) = setup("linux", false);
        assert_eq!(panel.before_input(&key("f", "meta")), KeyAction::Pass);
        assert_eq!(calls.find.get(), 0);
    }

    #[test]
    fn tells_no_window_that_has_gone() {
        let (panel, calls) = setup("darwin", true);
        assert_eq!(panel.before_input(&key("f", "meta")), KeyAction::Prevent);
        assert_eq!(calls.find.get(), 0);
    }

    #[test]
    fn hardens_a_guest_before_it_is_attached() {
        let (panel, _) = setup("darwin", false);
        let mut web_preferences = object(json!({ "nodeIntegration": true }));
        let mut params = object(json!({ "src": "https://example.com" }));
        assert!(panel.will_attach_webview(&mut web_preferences, &mut params));

        assert_eq!(web_preferences["partition"], "persist:panel-p1");
        assert_eq!(web_preferences["preload"], "/app/panel.cjs");
        assert_eq!(web_preferences["sandbox"], true);
        assert!(!web_preferences.contains_key("nodeIntegration"));
    }

    #[test]
    fn refuses_a_guest_on_a_scheme_the_panel_does_not_open_and_says_why() {
        let (panel, calls) = setup("darwin", false);
        let mut params = object(json!({ "src": "file:///etc/passwd" }));
        assert!(!panel.will_attach_webview(&mut Map::new(), &mut params));
        assert_eq!(
            *calls.warned.borrow(),
            ["[side-panel] The side panel does not open file:"]
        );
    }

    #[test]
    fn opens_a_page_that_asks_for_a_new_window_in_the_panel_itself() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(
            panel.window_open(42, "https://example.com/a", "foreground-tab"),
            DENY
        );
        assert_eq!(
            *calls.loaded.borrow(),
            [(42, "https://example.com/a".to_string())]
        );
        assert!(calls.routed.borrow().is_empty());
    }

    #[test]
    fn sends_a_cmd_ctrl_clicked_link_to_the_browser() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(
            panel.window_open(42, "https://example.com/a", "background-tab"),
            DENY
        );
        assert_eq!(*calls.routed.borrow(), ["https://example.com/a"]);
        assert!(calls.loaded.borrow().is_empty());
    }

    #[test]
    fn opens_nothing_at_all_for_a_refused_address() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(
            panel.window_open(42, "file:///etc/passwd", "foreground-tab"),
            DENY
        );
        assert!(calls.routed.borrow().is_empty());
        assert!(calls.loaded.borrow().is_empty());
    }

    #[test]
    fn lets_the_panel_navigate_on_the_web() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(
            panel.will_navigate("https://example.com/next"),
            NavigationAction::Allow
        );
        assert!(calls.routed.borrow().is_empty());
    }

    #[test]
    fn takes_agentrq_sign_in_out_of_the_panel_and_gives_it_to_the_main_window() {
        let (panel, calls) = setup("darwin", false);
        let url = format!("{SERVER_URL}/api/v1/auth/github/login");
        assert_eq!(panel.will_navigate(&url), NavigationAction::Prevent);
        assert_eq!(*calls.routed.borrow(), [url]);
    }

    #[test]
    fn stops_a_navigation_to_a_refused_address_without_routing_it() {
        let (panel, calls) = setup("darwin", false);
        assert_eq!(
            panel.will_navigate("javascript:alert(1)"),
            NavigationAction::Prevent
        );
        assert!(calls.routed.borrow().is_empty());
    }

    #[test]
    fn answers_a_question_a_page_left_waiting_when_it_moves_to_a_new_document() {
        let (panel, calls) = setup("darwin", false);
        panel.did_start_navigation(
            42,
            Some(NavigationStart {
                is_main_frame: true,
                is_same_document: true,
            }),
        );
        panel.did_start_navigation(
            42,
            Some(NavigationStart {
                is_main_frame: false,
                is_same_document: false,
            }),
        );
        assert!(calls.cancelled.borrow().is_empty());

        panel.did_start_navigation(
            42,
            Some(NavigationStart {
                is_main_frame: true,
                is_same_document: false,
            }),
        );
        assert_eq!(*calls.cancelled.borrow(), [42]);
        // An older event shape with nothing on it is a new document too.
        panel.did_start_navigation(42, None);
        assert_eq!(calls.cancelled.borrow().len(), 2);
    }

    #[test]
    fn answers_it_when_the_panel_closes_and_the_page_goes_away() {
        let (panel, calls) = setup("darwin", false);
        panel.guest_destroyed(42);
        assert_eq!(*calls.cancelled.borrow(), [42]);
    }

    #[test]
    fn carries_on_without_a_broker() {
        let panel = SidePanel::new(BareHost, "linux", false);
        panel.did_start_navigation(
            1,
            Some(NavigationStart {
                is_main_frame: true,
                is_same_document: false,
            }),
        );
        panel.guest_destroyed(1);
        // And without a way to tell the window has gone.
        assert_eq!(panel.before_input(&key("f", "control")), KeyAction::Prevent);
        // The host's required parts are asked for, whatever they answer.
        let mut params = object(json!({ "src": "file:///x" }));
        assert!(!panel.will_attach_webview(&mut Map::new(), &mut params));
        assert_eq!(panel.window_open(1, "https://example.com", ""), DENY);
        assert_eq!(panel.window_open(1, "mailto:a@b.c", ""), DENY);
        assert_eq!(
            panel.will_navigate("mailto:a@b.c"),
            NavigationAction::Prevent
        );
    }
}
