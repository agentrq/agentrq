// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The side panel's guest (WP7b): a native child webview of the main window,
//! wired to `core::side_panel`'s rules. `desktop/src/main/side-panel/wire.js`
//! and the guest half of `index.js` for Electron.
//!
//! What a guest is created with is the boundary, as `will-attach-webview` is
//! in Electron: a label no capability names (so no command at all), its own
//! storage, never the profile's (so never the `at` cookie), the panel init
//! script, and handlers that let only `http`, `https`, `agentrq-ext` and
//! `about:blank` load and never open a window.
//!
//! What each platform's webview is asked directly is in `side_panel/`, one
//! file per OS, outside the coverage gate.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use agentrq_desktop_core::links::{classify_link, LinkOptions, LinkTarget};
use agentrq_desktop_core::menu::ActionId;
use agentrq_desktop_core::menu_keys::repeats;
use agentrq_desktop_core::side_panel::bridge::{extension_of, PanelHost, PanelHub, PanelPage};
use agentrq_desktop_core::side_panel::child::{
    disposition, guest_init_script, guest_label, guest_number, is_app_internal, is_guest_label,
    is_srcdoc, opens_elsewhere, place, reported_key, reported_url, token_of, token_script,
    webview_url, GuestEvent, HostRect, Modifiers,
};
use agentrq_desktop_core::side_panel::ext_scheme::StdFs;
use agentrq_desktop_core::side_panel::find::{clears_selection, FindSession, Found};
use agentrq_desktop_core::side_panel::wire::{
    KeyAction, KeyInput, NavigationAction, SidePanel, SidePanelHost,
};
use serde_json::{json, Map, Value};
use tauri::webview::{NewWindowResponse, PageLoadEvent, WebviewBuilder};
use tauri::{
    AppHandle, Emitter, LogicalPosition, LogicalSize, Manager, Runtime, Url, Webview, WebviewUrl,
};

use crate::chrome::{self, MAIN};
use crate::network::Network;
use crate::panel_scheme::PanelScheme;
use crate::profiles::Profiles;

#[cfg_attr(target_os = "linux", path = "side_panel/linux.rs")]
#[cfg_attr(target_os = "macos", path = "side_panel/macos.rs")]
#[cfg_attr(windows, path = "side_panel/windows.rs")]
mod platform;

mod handlers;

/// The guest's init script: `desktop/src/preload/panel.js` for Tauri.
const PANEL_JS: &str = include_str!("../../src/guest/panel.js");

/// What the window hears about a guest (`GuestEvent`).
const GUEST_EVENT: &str = "agentrq:side-panel:guest";

/// WebView2 serves `agentrq-ext://x` as `http://agentrq-ext.x`.
#[cfg(windows)]
const WEBVIEW2: bool = true;
#[cfg(not(windows))]
const WEBVIEW2: bool = false;

/// Only a debug build reads `AGENTRQ_E2E_EXTENSION`.
#[cfg(debug_assertions)]
const DEBUG_BUILD: bool = true;
#[cfg(not(debug_assertions))]
const DEBUG_BUILD: bool = false;

/// `process.platform`'s spelling, which `core` speaks.
#[cfg(target_os = "macos")]
const PLATFORM: &str = "darwin";
#[cfg(windows)]
const PLATFORM: &str = "win32";
#[cfg(not(any(target_os = "macos", windows)))]
const PLATFORM: &str = "linux";

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// What the page reads when the webview refused.
fn failed(error: impl std::fmt::Display) -> String {
    error.to_string()
}

/// Where a link the panel does not keep goes: the browser, or the app's own
/// sign-in window (`lib.rs`'s `open_link`).
pub type OpenLink<R> = Box<dyn Fn(&AppHandle<R>, LinkTarget, &str) + Send + Sync>;

/// What `core`'s rules call back into.
struct Rules<R: Runtime>(AppHandle<R>);

impl<R: Runtime> SidePanelHost for Rules<R> {
    // The webview takes neither: its storage is chosen when it is built, and
    // its script is the one above.
    fn partition(&self) -> String {
        String::new()
    }
    fn preload(&self) -> String {
        String::new()
    }
    fn server_url(&self) -> String {
        self.0.state::<Arc<Network>>().server_url()
    }
    fn route_link(&self, url: &str) {
        // Called from inside a webview's own callback: anything that touches
        // a webview from there deadlocks on Windows.
        let (app, url) = (self.0.clone(), url.to_string());
        tauri::async_runtime::spawn(async move { route_link(&app, &url) });
    }
    fn warn(&self, message: &str) {
        eprintln!("{message}");
    }
    fn load_url(&self, guest: u32, url: &str) {
        let (app, url) = (self.0.clone(), url.to_string());
        tauri::async_runtime::spawn(async move {
            if let Some(webview) = app.get_webview(&guest_label(guest)) {
                navigate(&webview, &url);
            }
        });
    }
    /// The window takes the keyboard back first: its find bar is in the
    /// page, which a focused guest is not. Not from inside the guest's own
    /// key callback, which on Windows must not touch a webview.
    fn send_find(&self) {
        let app = self.0.clone();
        tauri::async_runtime::spawn(async move {
            if let Some(main) = chrome::main_webview(&app) {
                let _ = main.set_focus();
            }
            let _ = app.emit_to(MAIN, "agentrq:side-panel:find", ());
        });
    }
    fn cancel_permissions(&self, guest: u32) {
        crate::panel_permissions::cancel(&self.0, guest);
    }
}

/// The hub an Extension's `ctx.panel` is closed over. Extensions are WP9:
/// until then no page is listened to and nothing is posted.
struct Hub<R: Runtime>(AppHandle<R>);

impl<R: Runtime> PanelHost for Hub<R> {
    fn show(&self, url: &str) {
        let _ = self.0.emit_to(MAIN, "agentrq:side-panel:open", url);
    }
    fn deliver(&self, _owner: &str, _message: &Value) -> usize {
        0
    }
    fn pages_for(&self, _owner: &str) -> Vec<PanelPage> {
        Vec::new()
    }
}

/// What one guest last told the window, and the token its page sends with.
#[derive(Default)]
struct Seen {
    url: String,
    loading: bool,
    /// Counts its loads, so a report that arrives after a later one's is
    /// dropped rather than undoing it.
    load: u64,
    /// `None` from the moment a document starts loading until an
    /// Extension's page has loaded and been handed a new one.
    token: Option<String>,
}

/// The menu key each guest was last seen pressing, until its page says it
/// left that key unhandled (WebView2, which asks before the page does). The
/// next key down, the next report or [`KEY_WINDOW`] ends it, so a page cannot
/// keep a key it swallowed and post its report later.
#[derive(Default)]
pub struct PendingKeys(Mutex<HashMap<String, Pressed>>);

/// How long a page has to say it left a key unhandled.
const KEY_WINDOW: Duration = Duration::from_secs(1);

/// A key a guest pressed: the action it is bound to, whether it was a
/// repeat, and when.
struct Pressed {
    key: String,
    action: Option<ActionId>,
    repeat: bool,
    at: std::time::Instant,
}

#[cfg_attr(not(windows), allow(dead_code))]
impl PendingKeys {
    pub fn press(&self, label: &str, key: String, action: Option<ActionId>, repeat: bool) {
        let pressed = Pressed {
            key,
            action,
            repeat,
            at: std::time::Instant::now(),
        };
        lock(&self.0).insert(label.to_string(), pressed);
    }

    /// The action and repeat of `key`, when it is the key `label` pressed
    /// just now. Whatever it says, the pressed key is let go.
    fn take(&self, label: &str, key: &str) -> Option<(Option<ActionId>, bool)> {
        let pressed = lock(&self.0).remove(label)?;
        (pressed.key == key && pressed.at.elapsed() < KEY_WINDOW)
            .then_some((pressed.action, pressed.repeat))
    }

    /// Another key went down in `label`, or it went away.
    pub fn forget(&self, label: &str) {
        lock(&self.0).remove(label);
    }
}

pub struct SidePanelState<R: Runtime> {
    rules: SidePanel<Rules<R>>,
    hub: Arc<PanelHub<Hub<R>>>,
    next: AtomicU32,
    guests: Mutex<HashMap<String, Seen>>,
    keys: PendingKeys,
    /// Each guest's find session, while its find bar is open.
    finds: Mutex<HashMap<String, FindSession>>,
    open: OpenLink<R>,
}

impl<R: Runtime> SidePanelState<R> {
    pub fn new(app: &AppHandle<R>, open: OpenLink<R>) -> Self {
        Self {
            rules: SidePanel::new(Rules(app.clone()), PLATFORM, WEBVIEW2),
            hub: Arc::new(PanelHub::new(Hub(app.clone()), WEBVIEW2)),
            next: AtomicU32::new(1),
            guests: Mutex::new(HashMap::new()),
            keys: PendingKeys::default(),
            finds: Mutex::new(HashMap::new()),
            open,
        }
    }

    /// The `agentrq-ext` scheme's handler, over this hub.
    pub fn scheme(&self) -> PanelScheme {
        let hub = Arc::clone(&self.hub);
        PanelScheme::new(
            Box::new(move |sender, message| hub.handle_send(sender, message)),
            // WP9 lists the installed Extensions.
            installed(&self.hub, std::env::var("AGENTRQ_E2E_EXTENSION").ok()),
            Box::new(StdFs),
            WEBVIEW2,
        )
    }
}

/// Until WP9 installs Extensions, a debug build can be handed one to check
/// the scheme and the bridge by hand:
/// `AGENTRQ_E2E_EXTENSION=<name>:<dir>:<panel entry>`, answered by an echo.
fn installed<R: Runtime>(
    hub: &PanelHub<Hub<R>>,
    spec: Option<String>,
) -> crate::panel_scheme::Installed {
    use agentrq_desktop_core::side_panel::ext_scheme::{Installation, PanelEntry};
    let spec = spec.filter(|_| DEBUG_BUILD).unwrap_or_default();
    let parts: Vec<&str> = spec.splitn(3, ':').collect();
    let [name, dir, entry] = parts[..] else {
        return Box::new(Vec::new);
    };
    let installation = Installation {
        name: name.to_string(),
        dir: dir.into(),
        enabled: None,
        panels: vec![PanelEntry {
            entry: entry.to_string(),
        }],
    };
    hub.capability_for(name)
        .on_message(Arc::new(|message| Ok(Some(json!({ "echo": message })))));
    Box::new(move || vec![installation.clone()])
}

/// The main window's own link rule: the browser for the web, the app's
/// sign-in for AgentRQ's, nothing for the rest.
fn route_link<R: Runtime>(app: &AppHandle<R>, url: &str) {
    let server_url = app.state::<Arc<Network>>().server_url();
    let options = LinkOptions {
        app_origin: "",
        server_url: &server_url,
    };
    let target = classify_link(url, options);
    if let LinkTarget::System | LinkTarget::SignIn = target {
        (app.state::<SidePanelState<R>>().open)(app, target, url);
    }
}

fn navigate<R: Runtime>(webview: &Webview<R>, url: &str) {
    if let Ok(url) = Url::parse(&webview_url(url, WEBVIEW2)) {
        let _ = webview.navigate(url);
    }
}

fn emit<R: Runtime>(app: &AppHandle<R>, event: GuestEvent) {
    let _ = app.emit_to(MAIN, GUEST_EVENT, &event);
}

fn address<R: Runtime>(webview: &Webview<R>) -> String {
    webview
        .url()
        .map(|url| reported_url(url.as_str(), WEBVIEW2))
        .unwrap_or_default()
}

/// Tell the window where a guest is and whether it can go back. `load` is
/// the load this reports, and `token` what that load's page is handed.
async fn report<R: Runtime>(
    webview: Webview<R>,
    in_page: bool,
    load: Option<u64>,
    token: Option<String>,
) {
    let (can_go_back, can_go_forward) = platform::history(&webview).await;
    let label = webview.label().to_string();
    let app = webview.app_handle().clone();
    let url = address(&webview);
    let loading = {
        let state = app.state::<SidePanelState<R>>();
        let mut guests = lock(&state.guests);
        let Some(seen) = guests.get_mut(&label) else {
            return;
        };
        // A later load has reported, or will.
        if load.is_some_and(|load| load != seen.load) {
            return;
        }
        seen.url.clone_from(&url);
        load.map(|_| seen.loading)
    };
    if let Some(token) = token {
        let _ = webview.eval(token_script(&token));
    }
    emit(
        &app,
        GuestEvent::Navigate {
            label: label.clone(),
            url,
            in_page,
            can_go_back,
            can_go_forward,
        },
    );
    if let Some(loading) = loading {
        emit(
            &app,
            GuestEvent::Loading {
                label,
                loading,
                can_go_back,
                can_go_forward,
            },
        );
    }
}

/// A guest started or finished loading a document. What it means is
/// recorded here, in the order the webview raised it; what the window hears
/// waits for the history.
fn page_load<R: Runtime>(webview: Webview<R>, event: PageLoadEvent) {
    let app = webview.app_handle().clone();
    let state = app.state::<SidePanelState<R>>();
    let label = webview.label().to_string();
    let loading = event == PageLoadEvent::Started;
    if let (true, Some(number)) = (loading, guest_number(&label)) {
        state.rules.did_start_navigation(number, None);
    }
    // A new document: the old one's token goes with it, and an Extension's
    // page that has loaded gets one of its own.
    let extension_page = !extension_of(&address(&webview), false).is_empty();
    let (load, token) = {
        let mut guests = lock(&state.guests);
        let Some(seen) = guests.get_mut(&label) else {
            return;
        };
        seen.load += 1;
        seen.loading = loading;
        seen.token = (!loading && extension_page).then(new_token);
        (seen.load, seen.token.clone())
    };
    tauri::async_runtime::spawn(report(webview, false, Some(load), token));
}

fn new_token() -> String {
    let mut bytes = [0u8; 32];
    getrandom::fill(&mut bytes).expect("the OS random source is available");
    token_of(bytes)
}

/// The token a guest's page must send with a bridge call now, if it has one.
pub fn token<R: Runtime>(app: &AppHandle<R>, label: &str) -> Option<String> {
    let state = app.try_state::<SidePanelState<R>>()?;
    let guests = lock(&state.guests);
    guests.get(label).and_then(|seen| seen.token.clone())
}

/// A same-document move (a hash, `pushState`) loads nothing, so no platform
/// event tells the shell. The address is read four times a second instead.
fn watch_in_page<R: Runtime>(
    app: AppHandle<R>,
    label: String,
) -> tauri::async_runtime::JoinHandle<()> {
    tauri::async_runtime::spawn(async move {
        loop {
            tokio::time::sleep(Duration::from_millis(250)).await;
            let Some(webview) = app.get_webview(&label) else {
                return;
            };
            let url = address(&webview);
            let changed = {
                let state = app.state::<SidePanelState<R>>();
                let guests = lock(&state.guests);
                let Some(seen) = guests.get(&label) else {
                    return;
                };
                !seen.loading && !seen.url.is_empty() && seen.url != url
            };
            if changed {
                report(webview, true, None, None).await;
            }
        }
    })
}

/// The storage a profile's guests keep their browsing in.
fn panel_dir<R: Runtime>(app: &AppHandle<R>, profile: &str) -> PathBuf {
    app.state::<Arc<Profiles>>().panel_dir(profile)
}

/// One guest, as its handlers know it.
struct Guest<R: Runtime> {
    app: AppHandle<R>,
    number: u32,
}

impl<R: Runtime> Clone for Guest<R> {
    fn clone(&self) -> Self {
        Self {
            app: self.app.clone(),
            number: self.number,
        }
    }
}

impl<R: Runtime> Guest<R> {
    /// Whether the guest may load `url`. `click` is the person's own click
    /// on a link it came from, if it came from one (`platform::click`), which
    /// WebView2 never reports because it asks for a window instead.
    fn allows(&self, url: &str, click: Option<Modifiers>) -> bool {
        let rules = &self.app.state::<SidePanelState<R>>().rules;
        if is_app_internal(url) {
            return false;
        }
        if opens_elsewhere(url, click, PLATFORM, false) {
            rules.window_open(self.number, url, "background-tab");
            return false;
        }
        is_srcdoc(url) || rules.will_navigate(url) == NavigationAction::Allow
    }

    /// A page asked for a window: never one, as Electron's guest never gets one.
    fn new_window(&self, url: &str, held: Modifiers) -> NewWindowResponse<R> {
        self.app.state::<SidePanelState<R>>().rules.window_open(
            self.number,
            &reported_url(url, WEBVIEW2),
            disposition(held, PLATFORM),
        );
        NewWindowResponse::Deny
    }
}

/// `agentrq:side-panel` guest, made by the panel's host element: the label
/// the window knows it by from now on.
#[tauri::command]
pub async fn side_panel_guest_open<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    url: String,
    rect: HostRect,
    visible: bool,
) -> Result<String, String> {
    if webview.label() != MAIN {
        return Err("Only the app's window has a side panel.".to_string());
    }
    let state = app.state::<SidePanelState<R>>();
    // `will-attach-webview`: what the element asked to open is checked
    // before anything is made.
    let mut params = Map::from_iter([("src".to_string(), json!(url))]);
    if is_app_internal(&url)
        || !state
            .rules
            .will_attach_webview(&mut Map::new(), &mut params)
    {
        return Err("The side panel does not open that address.".to_string());
    }
    let placement = place(rect, chrome::zoom_of(&app)).ok_or("That is not a box.")?;
    let number = state.next.fetch_add(1, Ordering::SeqCst);
    let label = guest_label(number);
    // Tauri loads a `CustomProtocol` address as it is, whatever its scheme;
    // `External` would turn the dev server's into the app's own.
    let start =
        WebviewUrl::CustomProtocol(Url::parse(&webview_url(&url, WEBVIEW2)).map_err(failed)?);

    let guest = Guest {
        app: app.clone(),
        number,
    };
    let builder = WebviewBuilder::new(&label, start)
        .initialization_script(guest_init_script(PANEL_JS, WEBVIEW2))
        // White, as a browser's canvas is.
        .background_color(tauri::window::Color(255, 255, 255, 255));
    let builder = handlers::attach(builder, guest);
    let profile = app.state::<Arc<Network>>().active_id();
    let builder = platform::isolate(builder, panel_dir(&app, &profile), &profile);

    let main = chrome::main_webview(&app).ok_or("The app's window has gone.")?;
    lock(&state.guests).insert(label.clone(), Seen::default());
    crate::panel_permissions::opened(&app, number, &profile);
    let added = main.window().add_child(
        builder,
        LogicalPosition::new(placement.x, placement.y),
        LogicalSize::new(placement.width, placement.height),
    );
    let guest = added.map_err(|e| {
        forget(&app, &label);
        e.to_string()
    })?;
    platform::adopt(&main, &guest, placement);
    platform::watch(&guest);
    if !visible {
        let _ = guest.hide();
    }
    // Dropping the handle leaves the watch running.
    drop(watch_in_page(app.clone(), label.clone()));
    Ok(label)
}

fn guest<R: Runtime>(app: &AppHandle<R>, label: &str) -> Result<Webview<R>, String> {
    if !is_guest_label(label) {
        return Err("Not a side panel page.".to_string());
    }
    app.get_webview(label)
        .ok_or_else(|| "That side panel page has gone.".to_string())
}

/// The host element moved, or the panel was shown or hidden.
#[tauri::command]
pub async fn side_panel_guest_place<R: Runtime>(
    app: AppHandle<R>,
    label: String,
    rect: HostRect,
    visible: bool,
) -> Result<(), String> {
    let guest = guest(&app, &label)?;
    let placement = place(rect, chrome::zoom_of(&app)).ok_or("That is not a box.")?;
    platform::set_bounds(&guest, placement);
    if visible {
        guest.show().map_err(failed)
    } else {
        guest.hide().map_err(failed)
    }
}

/// The toolbar: an address, back, forward, reload or stop.
#[tauri::command]
pub async fn side_panel_guest_navigate<R: Runtime>(
    app: AppHandle<R>,
    label: String,
    action: String,
    url: Option<String>,
) -> Result<(), String> {
    let guest = guest(&app, &label)?;
    match action.as_str() {
        "load" => {
            // The rules a click is held to, before the webview is asked:
            // WebKit runs a `javascript:` address without asking at all.
            let url = url.unwrap_or_default();
            let rules = &app.state::<SidePanelState<R>>().rules;
            if is_app_internal(&url) || rules.will_navigate(&url) != NavigationAction::Allow {
                return Err("The side panel does not open that address.".to_string());
            }
            navigate(&guest, &url);
        }
        "back" => platform::go(&guest, -1),
        "forward" => platform::go(&guest, 1),
        "reload" => guest.reload().map_err(failed)?,
        "stop" => guest.eval("window.stop()").map_err(failed)?,
        _ => return Err(format!("No side panel action \"{action}\".")),
    }
    Ok(())
}

#[tauri::command]
pub async fn side_panel_guest_focus<R: Runtime>(
    app: AppHandle<R>,
    label: String,
) -> Result<(), String> {
    guest(&app, &label)?.set_focus().map_err(failed)
}

/// A PNG of the guest, shown in its place while an overlay is over it.
#[tauri::command]
pub async fn side_panel_guest_snapshot<R: Runtime>(
    app: AppHandle<R>,
    label: String,
) -> Result<tauri::ipc::Response, String> {
    let guest = guest(&app, &label)?;
    platform::snapshot(&guest)
        .await
        .map(tauri::ipc::Response::new)
}

/// `findInPage(text, { forward, findNext })`: the guest moves to a match,
/// and says how many there are with a `found` event.
#[tauri::command]
pub async fn side_panel_guest_find<R: Runtime>(
    app: AppHandle<R>,
    label: String,
    text: String,
    forward: bool,
    find_next: bool,
) -> Result<(), String> {
    let guest = guest(&app, &label)?;
    if text.is_empty() {
        return Err("There is nothing to find.".to_string());
    }
    let step = {
        let state = app.state::<SidePanelState<R>>();
        let mut finds = lock(&state.finds);
        finds
            .entry(label)
            .or_default()
            .begin(&text, forward, find_next)
    };
    platform::find(&guest, &text, step);
    Ok(())
}

/// `stopFindInPage(action)`: the matches lose their highlight, and with
/// `clearSelection` the active one its selection too.
#[tauri::command]
pub async fn side_panel_guest_stop_find<R: Runtime>(
    app: AppHandle<R>,
    label: String,
    action: String,
) -> Result<(), String> {
    let guest = guest(&app, &label)?;
    lock(&app.state::<SidePanelState<R>>().finds).remove(&label);
    platform::stop_find(&guest, clears_selection(&action));
    Ok(())
}

/// A guest's webview counted `total` matches for the step it was asked:
/// the window hears which is active. Nothing once its find bar has closed.
pub fn counted<R: Runtime>(app: &AppHandle<R>, label: &str, total: u32) {
    let found = {
        let state = app.state::<SidePanelState<R>>();
        let mut finds = lock(&state.finds);
        finds.get_mut(label).map(|session| session.counted(total))
    };
    if let Some(found) = found {
        report_found(app, label, found);
    }
}

/// A webview that counts the active match itself (WebView2).
#[cfg_attr(not(windows), allow(dead_code))]
pub fn report_found<R: Runtime>(app: &AppHandle<R>, label: &str, found: Found) {
    emit(
        app,
        GuestEvent::Found {
            label: label.to_string(),
            active: found.active,
            total: found.total,
        },
    );
}

/// A key a guest is about to get, before its page sees it: `true` when the
/// page must not (Cmd/Ctrl+F, which opens the window's find bar instead).
pub fn key_down<R: Runtime>(app: &AppHandle<R>, input: &KeyInput) -> bool {
    app.state::<SidePanelState<R>>().rules.before_input(input) == KeyAction::Prevent
}

/// What the shell keeps for a guest, let go: its entry, its permission
/// prompts, its pending key and its place.
fn forget<R: Runtime>(app: &AppHandle<R>, label: &str) {
    let state = app.state::<SidePanelState<R>>();
    lock(&state.guests).remove(label);
    lock(&state.finds).remove(label);
    state.keys.forget(label);
    if let Some(number) = guest_number(label) {
        state.rules.guest_destroyed(number);
        crate::panel_permissions::closed(app, number);
    }
    platform::forget(label);
}

/// The panel closed, or its element went away. What the shell kept is let
/// go first, whether or not the webview is still there.
#[tauri::command]
pub async fn side_panel_guest_close<R: Runtime>(
    app: AppHandle<R>,
    label: String,
) -> Result<(), String> {
    if is_guest_label(&label) {
        forget(&app, &label);
    }
    guest(&app, &label)?.close().map_err(failed)
}

/// The app's page started loading again: a reload, a crash's reload, a sign
/// out, or a new profile's window. Its panel element has gone, so every
/// guest goes too, with whatever the shell kept for it.
pub fn close_all<R: Runtime>(app: &AppHandle<R>) {
    let Some(state) = app.try_state::<SidePanelState<R>>() else {
        return;
    };
    let mut labels: Vec<String> = lock(&state.guests).keys().cloned().collect();
    for label in app.webviews().into_keys().filter(|l| is_guest_label(l)) {
        if !labels.contains(&label) {
            labels.push(label);
        }
    }
    for label in &labels {
        forget(app, label);
    }
    // Not from inside the page's own callback: that deadlocks on Windows.
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        for webview in labels.iter().filter_map(|label| app.get_webview(label)) {
            let _ = webview.close();
        }
    });
}

/// `agentrq:side-panel:open-external`: "Open in browser" on the toolbar.
#[tauri::command]
pub fn side_panel_open_external<R: Runtime>(app: AppHandle<R>, url: String) {
    tauri::async_runtime::spawn(async move { route_link(&app, &url) });
}

/// A menu key a guest's page left unhandled: claimed, and run unless it is
/// a repeat of one that does not repeat. Run later, on the main thread,
/// never inside the webview's own callback: from the main thread,
/// `run_on_main_thread` runs at once, so it is asked from the async runtime.
#[cfg_attr(target_os = "macos", allow(dead_code))]
pub fn menu_key<R: Runtime>(app: &AppHandle<R>, action: Option<ActionId>, repeat: bool) -> bool {
    let Some(action) = action else {
        return false;
    };
    if !repeat || repeats(action) {
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            let run = app.clone();
            let _ = app.run_on_main_thread(move || crate::app_menu::run(&run, action));
        });
    }
    true
}

#[cfg_attr(not(windows), allow(dead_code))]
pub fn pending_keys<R: Runtime>(app: &AppHandle<R>) -> &PendingKeys {
    &app.state::<SidePanelState<R>>().inner().keys
}

/// A guest's page said it left a key unhandled: the key it pressed, if it is
/// that one, runs.
#[cfg_attr(not(windows), allow(dead_code))]
pub fn unhandled_key<R: Runtime>(app: &AppHandle<R>, label: &str, message: &str) {
    let Some(key) = reported_key(message) else {
        return;
    };
    if let Some((action, repeat)) = pending_keys(app).take(label, key) {
        menu_key(app, action, repeat);
    }
}

/// A snapshot, or why there is none, within five seconds.
async fn within(
    rx: tokio::sync::oneshot::Receiver<Result<Vec<u8>, String>>,
) -> Result<Vec<u8>, String> {
    tokio::time::timeout(Duration::from_secs(5), rx)
        .await
        .map_err(|_| "The snapshot took too long.".to_string())?
        .map_err(|_| "The page went away.".to_string())?
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::chrome::Chrome;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{
        get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY,
    };
    use tauri::webview::InvokeRequest;
    use tauri::{Listener, WebviewWindow, WebviewWindowBuilder};

    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    const SERVER: &str = "https://app.agentrq.com";
    const PAGE: &str = "agentrq-ext://notes/panel/index.html";
    const RECT: HostRect = HostRect {
        x: 500.0,
        y: 50.0,
        width: 400.0,
        height: 600.0,
    };
    fn rect() -> Value {
        json!({"x": RECT.x, "y": RECT.y, "width": RECT.width, "height": RECT.height})
    }

    /// The link key, whatever the platform calls it.
    const LINK_KEY: Modifiers = Modifiers {
        control: true,
        meta: true,
        middle: false,
    };

    type Opened = Arc<Mutex<Vec<(LinkTarget, String)>>>;

    struct Rig {
        app: tauri::App<MockRuntime>,
        main: WebviewWindow<MockRuntime>,
        opened: Opened,
        events: Arc<Mutex<Vec<Value>>>,
        _profiles: crate::profiles::tests::Rig,
    }

    impl Rig {
        fn handle(&self) -> &AppHandle<MockRuntime> {
            self.app.handle()
        }
        fn state(&self) -> tauri::State<'_, SidePanelState<MockRuntime>> {
            self.app.state::<SidePanelState<MockRuntime>>()
        }
        fn opened(&self) -> Vec<(LinkTarget, String)> {
            std::mem::take(&mut *lock(&self.opened))
        }
        fn events(&self) -> Vec<Value> {
            std::mem::take(&mut *lock(&self.events))
        }
        fn seen(&self, label: &str) -> Option<(String, bool, u64, Option<String>)> {
            let state = self.state();
            let guests = lock(&state.guests);
            let seen = guests.get(label)?;
            Some((
                seen.url.clone(),
                seen.loading,
                seen.load,
                seen.token.clone(),
            ))
        }
        fn open(&self, url: &str) -> String {
            call(
                &self.main,
                "side_panel_guest_open",
                json!({"url": url, "rect": rect(), "visible": true}),
            )
            .expect("a guest")
            .as_str()
            .expect("its label")
            .to_string()
        }
        fn webview(&self, label: &str) -> Webview<MockRuntime> {
            self.app.get_webview(label).expect("the guest")
        }
    }

    fn rig() -> Rig {
        let profiles = crate::profiles::tests::rig_with(
            Some(json!({"profiles": [{"id": "default", "serverUrl": SERVER}]})),
            None,
        );
        let app = mock_builder()
            .invoke_handler(crate::commands::handler())
            .manage(profiles.network.clone())
            .manage(profiles.profiles.clone())
            .manage(Chrome::new(profiles.dir.path().to_path_buf()))
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let opened = Opened::default();
        let log = Arc::clone(&opened);
        let open: OpenLink<MockRuntime> = Box::new(move |_, target, url| {
            lock(&log).push((target, url.to_string()));
        });
        app.manage(SidePanelState::new(app.handle(), open));
        let events = Arc::new(Mutex::new(Vec::new()));
        let heard = Arc::clone(&events);
        app.listen_any(GUEST_EVENT, move |event| {
            lock(&heard).push(serde_json::from_str(event.payload()).unwrap());
        });
        let main = WebviewWindowBuilder::new(&app, MAIN, Default::default())
            .build()
            .expect("mock window");
        Rig {
            app,
            main,
            opened,
            events,
            _profiles: profiles,
        }
    }

    fn call(webview: &WebviewWindow<MockRuntime>, cmd: &str, args: Value) -> Result<Value, Value> {
        get_ipc_response(
            webview,
            InvokeRequest {
                cmd: cmd.into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: InvokeBody::Json(args),
                headers: Default::default(),
                invoke_key: INVOKE_KEY.to_string(),
            },
        )
        .map(|body| body.deserialize().unwrap())
    }

    /// Long enough for what was spawned to have run, where nothing tells.
    fn settle() {
        std::thread::sleep(Duration::from_millis(100));
    }

    /// Waits until what was spawned has `done` it, failing with `what` after
    /// ten seconds: a busy runner can take longer than any fixed pause.
    #[track_caller]
    pub(crate) fn wait_for(what: &str, done: &dyn Fn() -> bool) {
        let deadline = std::time::Instant::now() + Duration::from_secs(10);
        std::iter::repeat_with(|| std::thread::sleep(Duration::from_millis(5)))
            .find(|()| done() || std::time::Instant::now() > deadline);
        assert!(done(), "{what}");
    }

    #[test]
    fn opens_a_guest_with_no_capability_and_its_own_storage() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        assert_eq!(label, "panel-guest-1");
        assert!(is_guest_label(&label));
        assert_eq!(rig.seen(&label), Some((String::new(), false, 0, None)));
        // An Extension's page is served by the app's scheme.
        let ext = rig.open(PAGE);
        assert_eq!(ext, "panel-guest-2");
        assert_eq!(
            rig.webview(&ext).url().unwrap().as_str(),
            webview_url(PAGE, WEBVIEW2)
        );
        // Hidden from the start.
        let hidden = call(
            &rig.main,
            "side_panel_guest_open",
            json!({"url": "https://b.example/", "rect": rect(), "visible": false}),
        );
        assert_eq!(hidden, Ok(json!("panel-guest-3")));
        let panel = rig._profiles.profiles.panel_dir("default");
        assert_eq!(panel_dir(rig.handle(), "default"), panel);
    }

    #[test]
    fn opens_only_from_the_app_window_and_only_what_the_panel_may_load() {
        let rig = rig();
        let other = WebviewWindowBuilder::new(&rig.app, "sign-in-1", Default::default())
            .build()
            .unwrap();
        let args = json!({"url": "https://a.example/", "rect": rect(), "visible": true});
        assert_eq!(
            call(&other, "side_panel_guest_open", args),
            Err(json!("Only the app's window has a side panel."))
        );
        for url in [
            "javascript:alert(1)",
            "file:///etc/passwd",
            "tauri://localhost/",
        ] {
            let args = json!({"url": url, "rect": rect(), "visible": true});
            assert_eq!(
                call(&rig.main, "side_panel_guest_open", args),
                Err(json!("The side panel does not open that address.")),
                "{url}"
            );
        }
        let not_a_box = HostRect {
            x: f64::NAN,
            ..RECT
        };
        let webview = rig.main.as_ref().clone();
        let opened = tauri::async_runtime::block_on(side_panel_guest_open(
            rig.handle().clone(),
            webview,
            "https://a.example/".to_string(),
            not_a_box,
            true,
        ));
        assert_eq!(opened, Err("That is not a box.".to_string()));
        assert!(lock(&rig.state().guests).is_empty());
    }

    #[test]
    fn a_guest_that_cannot_be_made_leaves_nothing_behind() {
        let rig = rig();
        // Its label is taken.
        rig.main
            .as_ref()
            .window()
            .add_child(
                WebviewBuilder::new("panel-guest-1", WebviewUrl::default()),
                LogicalPosition::new(0.0, 0.0),
                LogicalSize::new(1.0, 1.0),
            )
            .unwrap();
        let args = json!({"url": "https://a.example/", "rect": rect(), "visible": true});
        assert!(call(&rig.main, "side_panel_guest_open", args).is_err());
        assert!(rig.seen("panel-guest-1").is_none());
    }

    #[test]
    fn the_commands_take_only_a_guest_that_is_there() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        let place = |visible| json!({"label": label, "rect": rect(), "visible": visible});
        assert_eq!(
            call(&rig.main, "side_panel_guest_place", place(true)),
            Ok(Value::Null)
        );
        assert_eq!(
            call(&rig.main, "side_panel_guest_place", place(false)),
            Ok(Value::Null)
        );
        assert_eq!(
            call(&rig.main, "side_panel_guest_focus", json!({"label": label})),
            Ok(Value::Null)
        );
        for label in ["main", "panel-guest-9"] {
            let args = json!({"label": label});
            let refused = call(&rig.main, "side_panel_guest_focus", args).unwrap_err();
            assert!(
                ["Not a side panel page.", "That side panel page has gone."]
                    .contains(&refused.as_str().unwrap()),
                "{label}"
            );
        }
        let not_a_box = HostRect {
            width: f64::INFINITY,
            ..RECT
        };
        let placed = tauri::async_runtime::block_on(side_panel_guest_place(
            rig.handle().clone(),
            label,
            not_a_box,
            true,
        ));
        assert_eq!(placed, Err("That is not a box.".to_string()));
    }

    #[test]
    fn the_toolbar_loads_only_what_a_click_could() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        let go = |action: &str, url: Option<&str>| {
            call(
                &rig.main,
                "side_panel_guest_navigate",
                json!({"label": label, "action": action, "url": url}),
            )
        };
        assert_eq!(go("load", Some("https://b.example/")), Ok(Value::Null));
        assert_eq!(
            rig.webview(&label).url().unwrap().as_str(),
            "https://b.example/"
        );
        for url in [
            Some("javascript:alert(1)"),
            Some("tauri://localhost/"),
            None,
        ] {
            assert_eq!(
                go("load", url),
                Err(json!("The side panel does not open that address.")),
                "{url:?}"
            );
        }
        assert_eq!(
            rig.webview(&label).url().unwrap().as_str(),
            "https://b.example/"
        );
        for action in ["back", "forward", "reload", "stop"] {
            assert_eq!(go(action, None), Ok(Value::Null), "{action}");
        }
        assert_eq!(go("fly", None), Err(json!("No side panel action \"fly\".")));
    }

    #[test]
    fn a_snapshot_of_a_page_that_never_answers_is_an_error() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        assert_eq!(
            call(
                &rig.main,
                "side_panel_guest_snapshot",
                json!({"label": label})
            ),
            Err(json!("The page went away."))
        );
    }

    #[tokio::test(start_paused = true)]
    async fn a_snapshot_waits_five_seconds_at_most() {
        let (tx, rx) = tokio::sync::oneshot::channel();
        let waited = within(rx).await;
        assert_eq!(waited, Err("The snapshot took too long.".to_string()));
        drop(tx);
        let (tx, rx) = tokio::sync::oneshot::channel();
        tx.send(Ok(vec![1])).unwrap();
        assert_eq!(within(rx).await, Ok(vec![1]));
    }

    #[test]
    fn closing_lets_go_of_the_guest_even_once_it_has_gone() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        rig.state()
            .keys
            .press(&label, "r".into(), Some(ActionId::Reload), false);
        assert_eq!(
            call(&rig.main, "side_panel_guest_close", json!({"label": label})),
            Ok(Value::Null)
        );
        assert!(rig.seen(&label).is_none());
        assert!(rig.state().keys.take(&label, "r").is_none());
        // Already gone from the window: what was kept is still let go.
        lock(&rig.state().guests).insert("panel-guest-7".into(), Seen::default());
        assert_eq!(
            call(
                &rig.main,
                "side_panel_guest_close",
                json!({"label": "panel-guest-7"})
            ),
            Err(json!("That side panel page has gone."))
        );
        assert!(rig.seen("panel-guest-7").is_none());
        assert_eq!(
            call(
                &rig.main,
                "side_panel_guest_close",
                json!({"label": "main"})
            ),
            Err(json!("Not a side panel page."))
        );
    }

    #[test]
    fn a_reload_of_the_app_closes_every_guest_and_what_it_kept() {
        let rig = rig();
        let first = rig.open("https://a.example/");
        let second = rig.open(PAGE);
        // One the shell no longer tracks, still in the window.
        lock(&rig.state().guests).remove(&second);
        rig.state().keys.press(&first, "r".into(), None, false);
        // A second webview in the window: `main` is still found.
        assert!(chrome::main_webview(rig.handle()).is_some());
        close_all(rig.handle());
        assert!(lock(&rig.state().guests).is_empty());
        assert!(rig.state().keys.take(&first, "r").is_none());
        settle();
        // The page that loads next opens its own.
        assert_eq!(rig.open("https://b.example/"), "panel-guest-3");
        close_all(rig.handle());
        assert!(chrome::main_webview(rig.handle()).is_some());

        // An app with no side panel at all.
        let bare = mock_builder().build(mock_context(noop_assets())).unwrap();
        close_all(bare.handle());
        assert_eq!(token(bare.handle(), "panel-guest-1"), None);
    }

    #[test]
    fn the_main_page_load_closes_guests_beside_the_links_and_routes() {
        use crate::shell::{dispatch_navigation, ShellState};
        let rig = rig();
        let app = rig.handle();
        app.plugin(tauri_plugin_opener::init()).expect("the opener");
        app.manage(ShellState::<MockRuntime>::default());
        let routes = Arc::new(Mutex::new(Vec::new()));
        let heard = Arc::clone(&routes);
        app.listen_any("agentrq:navigate", move |event| {
            lock(&heard).push(event.payload().to_string());
        });
        let shell = app.state::<ShellState<MockRuntime>>();
        let guest = rig.open("https://a.example/");
        // A reload of the app, with a route waiting for it.
        chrome::main_page_load(app, &PageLoadEvent::Started);
        shell.navigate(app, "/workspaces/1");
        assert!(lock(&rig.state().guests).is_empty());
        wait_for("the guest is gone", &|| app.get_webview(&guest).is_none());
        chrome::main_page_load(app, &PageLoadEvent::Finished);
        // The new page asks for it once it listens.
        shell.on_subscribed(app);
        assert_eq!(*lock(&routes), vec!["\"/workspaces/1\""]);
        // A guest open again: the main webview still routes its links, WP4's
        // sign-in first, the web to the opener, the app's own in place.
        rig.open("https://b.example/");
        let origin = crate::profiles::app_origin("default");
        let own = Url::parse(&format!("{origin}/tasks")).unwrap();
        assert!(dispatch_navigation(app, &own));
        let web = Url::parse("https://agentrq.com/docs").unwrap();
        assert!(!dispatch_navigation(app, &web));
        let sign_in = Url::parse(&format!("{SERVER}/api/v1/auth/google/login")).unwrap();
        assert!(!dispatch_navigation(app, &sign_in));
        wait_for("WP4's window", &|| {
            app.windows().into_keys().any(|l| l.starts_with("sign-in-"))
        });
        assert!(chrome::main_webview(app).is_some());
    }

    #[test]
    fn a_page_load_is_recorded_in_order_and_hands_an_extension_page_a_new_token() {
        let rig = rig();
        let ext = rig.open(PAGE);
        let webview = rig.webview(&ext);
        page_load(webview.clone(), PageLoadEvent::Started);
        let (_, loading, load, token_now) = rig.seen(&ext).unwrap();
        assert!(loading);
        assert_eq!((load, token_now), (1, None), "none while it loads");
        page_load(webview.clone(), PageLoadEvent::Finished);
        let (_, loading, load, first) = rig.seen(&ext).unwrap();
        assert!(!loading);
        assert_eq!(load, 2);
        let first = first.expect("a token once loaded");
        assert_eq!(first.len(), 64);
        assert_eq!(token(rig.handle(), &ext), Some(first.clone()));
        wait_for("the load reported", &|| {
            let events = lock(&rig.events);
            events
                .last()
                .is_some_and(|e| e["kind"] == "loading" && e["loading"] == false)
        });
        assert_eq!(rig.seen(&ext).unwrap().0, PAGE, "reported");
        let kinds: Vec<Value> = rig.events().iter().map(|e| e["kind"].clone()).collect();
        assert!(
            kinds.ends_with(&[json!("navigate"), json!("loading")]),
            "{kinds:?}"
        );

        // A navigation: the old token is stale at once, and the next page
        // gets another.
        page_load(webview.clone(), PageLoadEvent::Started);
        assert_eq!(token(rig.handle(), &ext), None);
        page_load(webview.clone(), PageLoadEvent::Finished);
        let second = token(rig.handle(), &ext).unwrap();
        assert_ne!(second, first);

        // A website gets none.
        let web = rig.open("https://a.example/");
        page_load(rig.webview(&web), PageLoadEvent::Finished);
        assert_eq!(token(rig.handle(), &web), None);
        assert_eq!(token(rig.handle(), "panel-guest-9"), None);

        // A webview the panel does not know of.
        page_load(rig.main.as_ref().clone(), PageLoadEvent::Started);
        assert!(rig.seen(MAIN).is_none());
    }

    #[test]
    fn a_report_that_a_later_load_overtook_is_dropped() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        let webview = rig.webview(&label);
        page_load(webview.clone(), PageLoadEvent::Started);
        page_load(webview.clone(), PageLoadEvent::Finished);
        settle();
        rig.events();
        lock(&rig.state().guests).get_mut(&label).unwrap().url = "old".into();
        tauri::async_runtime::block_on(report(webview.clone(), false, Some(1), None));
        assert_eq!(rig.seen(&label).unwrap().0, "old");
        assert!(rig.events().is_empty());
        // Its own report goes through; and a guest that has gone, nowhere.
        tauri::async_runtime::block_on(report(webview.clone(), false, Some(2), Some("t".into())));
        assert_eq!(rig.seen(&label).unwrap().0, "https://a.example/");
        assert_eq!(rig.events().len(), 2);
        forget(rig.handle(), &label);
        tauri::async_runtime::block_on(report(webview, true, None, None));
        assert!(rig.events().is_empty());
    }

    #[test]
    fn a_move_within_the_page_is_reported() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        lock(&rig.state().guests).get_mut(&label).unwrap().url = "https://a.example/#old".into();
        std::thread::sleep(Duration::from_millis(400));
        assert_eq!(rig.seen(&label).unwrap().0, "https://a.example/");
        let navigate = rig.events().into_iter().find(|e| e["kind"] == "navigate");
        assert_eq!(navigate.unwrap()["inPage"], true);
        // Its entry gone, the watch stops; and one with no webview never starts.
        forget(rig.handle(), &label);
        let stops = |label: &str| {
            let watch = watch_in_page(rig.handle().clone(), label.into());
            tauri::async_runtime::block_on(async {
                tokio::time::timeout(Duration::from_secs(10), watch).await
            })
            .expect("the watch stops")
            .unwrap();
        };
        stops(&label);
        stops("panel-guest-9");
        assert!(rig.events().is_empty());
    }

    #[test]
    fn a_guest_loads_only_what_the_rules_allow() {
        let rig = rig();
        let guest = Guest {
            app: rig.handle().clone(),
            number: 1,
        };
        assert!(guest.allows("https://a.example/", None));
        assert!(guest.allows("about:srcdoc", None));
        assert!(guest.allows(PAGE, Some(LINK_KEY)));
        assert!(!guest.allows("tauri://localhost/", None));
        assert!(!guest.allows("http://tauri.localhost/", None));
        assert!(!guest.allows("javascript:alert(1)", None));
        // A person's link-key click: the browser.
        assert!(!guest.allows("https://b.example/", Some(LINK_KEY)));
        wait_for("the browser asked", &|| !lock(&rig.opened).is_empty());
        assert_eq!(
            rig.opened(),
            [(LinkTarget::System, "https://b.example/".to_string())]
        );
        // Never a window.
        assert!(matches!(
            guest.new_window("https://c.example/", LINK_KEY),
            NewWindowResponse::Deny
        ));
        wait_for("the browser asked", &|| !lock(&rig.opened).is_empty());
        assert_eq!(
            rig.opened(),
            [(LinkTarget::System, "https://c.example/".to_string())]
        );
        let clone = guest.clone();
        assert_eq!(clone.number, 1);
    }

    #[test]
    fn links_the_panel_does_not_keep_go_to_the_browser_or_the_sign_in() {
        let rig = rig();
        route_link(rig.handle(), "https://example.com/");
        route_link(rig.handle(), &format!("{SERVER}/api/v1/auth/github/login"));
        route_link(rig.handle(), "javascript:alert(1)");
        assert_eq!(
            rig.opened(),
            [
                (LinkTarget::System, "https://example.com/".to_string()),
                (
                    LinkTarget::SignIn,
                    format!("{SERVER}/api/v1/auth/github/login")
                ),
            ]
        );
        assert_eq!(
            call(
                &rig.main,
                "side_panel_open_external",
                json!({"url": "https://x.example/"})
            ),
            Ok(Value::Null)
        );
        wait_for("the browser asked", &|| !lock(&rig.opened).is_empty());
        assert_eq!(
            rig.opened(),
            [(LinkTarget::System, "https://x.example/".to_string())]
        );
    }

    #[test]
    fn the_rules_reach_the_window_and_the_guest() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        let rules = Rules(rig.handle().clone());
        assert_eq!(rules.partition(), "");
        assert_eq!(rules.preload(), "");
        assert_eq!(rules.server_url(), SERVER);
        rules.warn("[side-panel] a test");
        rules.send_find();
        rules.load_url(1, "https://b.example/");
        rules.load_url(9, "https://c.example/");
        rules.route_link("https://d.example/");
        wait_for("the load and the browser", &|| {
            let loaded = rig.webview(&label).url().unwrap().as_str() == "https://b.example/";
            loaded && !lock(&rig.opened).is_empty()
        });
        assert_eq!(
            rig.webview(&label).url().unwrap().as_str(),
            "https://b.example/"
        );
        assert_eq!(
            rig.opened(),
            [(LinkTarget::System, "https://d.example/".to_string())]
        );
        let hub = Hub(rig.handle().clone());
        hub.show(PAGE);
        assert_eq!(hub.deliver("notes", &json!(1)), 0);
        assert!(hub.pages_for("notes").is_empty());
    }

    #[test]
    fn a_debug_build_can_be_handed_one_extension() {
        let rig = rig();
        let hub = &rig.state().hub;
        assert!(installed(hub, None)().is_empty());
        assert!(installed(hub, Some("notes".into()))().is_empty());
        let one = installed(hub, Some("notes:/tmp/notes:panel/index.html".into()))();
        assert_eq!(one.len(), 1);
        assert_eq!(one[0].name, "notes");
        assert_eq!(one[0].panels[0].entry, "panel/index.html");
        let sender = agentrq_desktop_core::side_panel::child::sender_of(PAGE, true);
        assert_eq!(hub.handle_send(sender, &json!(1)), Ok(json!({"echo": 1})));
        let _scheme = rig.state().scheme();
    }

    #[test]
    fn every_command_refuses_arguments_it_cannot_read() {
        let rig = rig();
        let good = |cmd: &str| match cmd {
            "side_panel_guest_open" => {
                json!({"url": "https://a.example/", "rect": rect(), "visible": true})
            }
            "side_panel_guest_place" => {
                json!({"label": "panel-guest-1", "rect": rect(), "visible": true})
            }
            "side_panel_guest_navigate" => {
                json!({"label": "panel-guest-1", "action": "back", "url": null})
            }
            "side_panel_guest_find" => {
                json!({"label": "panel-guest-1", "text": "x", "forward": true, "findNext": true})
            }
            "side_panel_guest_stop_find" => {
                json!({"label": "panel-guest-1", "action": "keepSelection"})
            }
            _ => json!({"label": "panel-guest-1"}),
        };
        for cmd in [
            "side_panel_guest_open",
            "side_panel_guest_place",
            "side_panel_guest_navigate",
            "side_panel_guest_focus",
            "side_panel_guest_snapshot",
            "side_panel_guest_find",
            "side_panel_guest_stop_find",
            "side_panel_guest_close",
        ] {
            let args = good(cmd);
            for name in args.as_object().unwrap().keys() {
                let mut bad = args.clone();
                bad[name] = json!({"not": "it"});
                let refused = call(&rig.main, cmd, bad).unwrap_err();
                assert!(
                    refused.as_str().unwrap().contains(name),
                    "{cmd} {name}: {refused}"
                );
            }
        }
    }

    #[test]
    fn find_steps_through_the_matches_and_tells_the_window() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        rig.events();
        let find = |text: &str, forward: bool, find_next: bool| {
            call(
                &rig.main,
                "side_panel_guest_find",
                json!({"label": label, "text": text, "forward": forward, "findNext": find_next}),
            )
        };
        assert_eq!(find("cat", true, true), Ok(Value::Null));
        counted(rig.handle(), &label, 3);
        assert_eq!(find("cat", true, false), Ok(Value::Null));
        counted(rig.handle(), &label, 3);
        assert_eq!(find("cat", false, false), Ok(Value::Null));
        assert_eq!(find("cat", false, false), Ok(Value::Null));
        counted(rig.handle(), &label, 3);
        counted(rig.handle(), &label, 3);
        let found: Vec<(Value, Value)> = rig
            .events()
            .into_iter()
            .filter(|e| e["kind"] == "found" && e["label"] == label.as_str())
            .map(|e| (e["active"].clone(), e["total"].clone()))
            .collect();
        assert_eq!(
            found,
            [
                (json!(1), json!(3)),
                (json!(2), json!(3)),
                (json!(1), json!(3)),
                (json!(3), json!(3))
            ]
        );
        assert_eq!(
            find("", true, true),
            Err(json!("There is nothing to find."))
        );
        assert_eq!(
            find("cat", true, true),
            Ok(Value::Null),
            "a new search once more"
        );
        let stop = |action: &str| {
            call(
                &rig.main,
                "side_panel_guest_stop_find",
                json!({"label": label, "action": action}),
            )
        };
        assert_eq!(stop("clearSelection"), Ok(Value::Null));
        assert_eq!(stop("keepSelection"), Ok(Value::Null));
        // The bar has closed: a late count is nobody's.
        counted(rig.handle(), &label, 3);
        assert!(rig.events().is_empty());
        assert_eq!(
            call(
                &rig.main,
                "side_panel_guest_find",
                json!({"label": "main", "text": "x", "forward": true, "findNext": true})
            ),
            Err(json!("Not a side panel page."))
        );
        assert_eq!(
            call(
                &rig.main,
                "side_panel_guest_stop_find",
                json!({"label": "panel-guest-9", "action": "keepSelection"})
            ),
            Err(json!("That side panel page has gone."))
        );
    }

    #[test]
    fn a_closed_guest_leaves_no_find_session() {
        let rig = rig();
        let label = rig.open("https://a.example/");
        call(
            &rig.main,
            "side_panel_guest_find",
            json!({"label": label, "text": "x", "forward": true, "findNext": true}),
        )
        .unwrap();
        assert!(lock(&rig.state().finds).contains_key(&label));
        call(&rig.main, "side_panel_guest_close", json!({"label": label})).unwrap();
        assert!(lock(&rig.state().finds).is_empty());
    }

    #[test]
    fn webview2s_own_count_reaches_the_window_as_it_is() {
        let rig = rig();
        rig.events();
        report_found(
            rig.handle(),
            "panel-guest-4",
            Found {
                active: 2,
                total: 7,
            },
        );
        assert_eq!(
            rig.events(),
            [json!({"kind": "found", "label": "panel-guest-4", "active": 2, "total": 7})]
        );
    }

    /// Cmd+F on macOS, Ctrl+F elsewhere: (control, meta).
    #[cfg(target_os = "macos")]
    const FIND_KEY: (bool, bool) = (false, true);
    #[cfg(not(target_os = "macos"))]
    const FIND_KEY: (bool, bool) = (true, false);

    #[test]
    fn the_find_key_in_a_guest_opens_the_windows_find_bar_and_the_page_never_sees_it() {
        let rig = rig();
        let heard = Arc::new(Mutex::new(0));
        let count = Arc::clone(&heard);
        rig.app
            .listen_any("agentrq:side-panel:find", move |_| *lock(&count) += 1);
        let key = |key: &str, control: bool, meta: bool, shift: bool| KeyInput {
            kind: "keyDown".to_string(),
            key: key.to_string(),
            shift,
            alt: false,
            control,
            meta,
        };
        let (control, meta) = FIND_KEY;
        let (find, link) = (
            key("F", control, meta, false),
            key("f", meta, control, false),
        );
        assert!(key_down(rig.handle(), &find));
        for other in [
            link,
            key("g", true, true, false),
            key("f", true, true, true),
        ] {
            assert!(!key_down(rig.handle(), &other), "{other:?}");
        }
        settle();
        assert_eq!(*lock(&heard), 1);
    }

    /// A guest runs any page on the web: no capability may grant a remote
    /// origin, or name it by its label or a pattern; nor name a window, since
    /// the guests are webviews of the main one.
    #[test]
    fn no_capability_reaches_a_guest() {
        use agentrq_desktop_core::side_panel::child::GUEST_LABEL_PREFIX;
        let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("capabilities");
        let mut read = 0;
        for entry in std::fs::read_dir(dir).unwrap() {
            let path = entry.unwrap().path();
            let capability: Value = serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
            assert!(capability.get("remote").is_none(), "{path:?}");
            assert!(capability.get("windows").is_none(), "{path:?}");
            for name in capability["webviews"].as_array().unwrap() {
                let name = name.as_str().unwrap();
                assert!(!name.contains('*'), "{path:?} names {name}");
                assert!(
                    !name.starts_with(GUEST_LABEL_PREFIX),
                    "{path:?} names {name}"
                );
            }
            read += 1;
        }
        assert!(read > 0);
    }

    #[test]
    fn a_refusal_reads_as_what_went_wrong() {
        assert_eq!(failed(tauri::Error::WebviewNotFound), "webview not found");
        assert_eq!(
            failed(Url::parse("not a url").unwrap_err()),
            "relative URL without a base"
        );
    }

    #[test]
    fn the_scheme_answers_from_this_panels_hub() {
        let rig = rig();
        let scheme = rig.state().scheme();
        let request = http::Request::builder()
            .method("POST")
            .uri("agentrq-ext://notes/.agentrq/panel-send")
            .header(agentrq_desktop_core::side_panel::child::TOKEN_HEADER, "t")
            .body(b"1".to_vec())
            .unwrap();
        let page = crate::panel_scheme::Page {
            top_url: Some(PAGE),
            token: Some("t"),
        };
        let res = scheme.handle("panel-guest-1", page, &request);
        let answer: Value = serde_json::from_slice(res.body()).unwrap();
        assert_eq!(answer["ok"], false, "notes does not listen: {answer}");
    }

    #[test]
    fn a_menu_key_runs_once_unless_it_is_zoom() {
        let rig = rig();
        let app = rig.handle();
        assert!(!menu_key(app, None, false));
        assert!(menu_key(app, Some(ActionId::Reload), false));
        assert!(
            menu_key(app, Some(ActionId::Reload), true),
            "claimed, not run"
        );
        assert!(menu_key(app, Some(ActionId::ZoomIn), true));
        settle();
    }

    #[test]
    fn a_key_runs_only_once_its_page_says_it_left_it() {
        let rig = rig();
        let app = rig.handle();
        let label = rig.open("https://a.example/");
        let keys = pending_keys(app);
        keys.press(&label, "r".into(), Some(ActionId::Reload), false);
        // Something else entirely: the key is still pressed.
        unhandled_key(app, &label, "{\"cmd\":\"x\"}");
        unhandled_key(app, &label, "agentrq-panel-key:r");
        assert!(keys.take(&label, "r").is_none(), "taken once");
        unhandled_key(app, "panel-guest-9", "agentrq-panel-key:r");
        keys.press(&label, "+".into(), Some(ActionId::ZoomIn), true);
        assert_eq!(keys.take(&label, "+"), Some((Some(ActionId::ZoomIn), true)));
    }

    #[test]
    fn a_key_the_page_swallowed_cannot_be_reported_later() {
        let rig = rig();
        let app = rig.handle();
        let label = rig.open("https://a.example/");
        let keys = pending_keys(app);
        // A report of another key lets the pressed one go.
        keys.press(&label, "r".into(), Some(ActionId::Reload), false);
        assert!(keys.take(&label, "w").is_none());
        assert!(keys.take(&label, "r").is_none());
        // So does the next key down.
        keys.press(&label, "r".into(), Some(ActionId::Reload), false);
        keys.forget(&label);
        assert!(keys.take(&label, "r").is_none());
        // And time: a report after the window runs nothing.
        keys.press(&label, "r".into(), Some(ActionId::Reload), false);
        let late = std::time::Instant::now().checked_sub(KEY_WINDOW);
        lock(&keys.0).get_mut(&label).expect("pressed").at = late.expect("a past instant");
        assert!(keys.take(&label, "r").is_none());
        keys.press(&label, "r".into(), Some(ActionId::Reload), false);
        assert_eq!(
            keys.take(&label, "r"),
            Some((Some(ActionId::Reload), false))
        );
    }
}
