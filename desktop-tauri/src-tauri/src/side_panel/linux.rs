// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What WebKitGTK is asked directly for the side panel's guest, because
//! Tauri does not. Native glue, outside the coverage gate: the decisions are
//! `core`'s and `side_panel.rs`'s.

use std::cell::{Cell, RefCell};
use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::sync::{Mutex, PoisonError};

use agentrq_desktop_core::side_panel::child::{
    is_guest_label, link_click, Modifiers, NavigationKind, Placement,
};
use agentrq_desktop_core::side_panel::platform_permissions::{
    CommittedPages, NotificationSeeds, WebKitLoad,
};
use gtk::prelude::*;
use tauri::webview::WebviewBuilder;
use tauri::{Runtime, Webview};
use webkit2gtk::WebViewExt;

thread_local! {
    /// Tauri packs every webview of a window into one GtkBox, so the guest
    /// would sit under the page. Both go into an overlay instead, and the
    /// overlay places each guest from its box.
    static OVERLAY: RefCell<Option<gtk::Overlay>> = const { RefCell::new(None) };
    static BOXES: RefCell<HashMap<String, gtk::gdk::Rectangle>> = RefCell::new(HashMap::new());
    /// What each guest's last navigation was, from `decide-policy`.
    static NAVIGATIONS: RefCell<HashMap<String, NavigationKind>> = RefCell::new(HashMap::new());
    /// The keys held down in a guest, to tell a repeat from a press.
    static DOWN: RefCell<HashSet<u16>> = RefCell::new(HashSet::new());
    static REPEAT: Cell<bool> = const { Cell::new(false) };
    /// The permission requests a guest's page is waiting on, until the
    /// broker answers them.
    static WAITING: RefCell<HashMap<u64, webkit2gtk::PermissionRequest>> = RefCell::new(HashMap::new());
    static NEXT_REQUEST: Cell<u64> = const { Cell::new(1) };
    /// Each guest's committed address, which its requests come from.
    static COMMITTED: RefCell<CommittedPages> = RefCell::new(CommittedPages::default());
    /// The notifications guests' pages are showing, by guest and WebKit's
    /// id, until the page closes one or its guest goes: a click is the page's.
    static SHOWN: RefCell<HashMap<(String, u64), webkit2gtk::Notification>> = RefCell::new(HashMap::new());
}

/// The guests' storage folder and its profile's notification answers, which
/// every new web process of that folder's context is told. Written off the
/// main thread, before a guest is made and whenever an answer changes.
static SEEDS: Mutex<Option<(PathBuf, NotificationSeeds)>> = Mutex::new(None);

/// The answers kept for `dir`, or for whichever folder was seeded last: the
/// active profile's, the only one with guests.
fn seeds_for(dir: Option<&Path>) -> Option<NotificationSeeds> {
    let seeds = SEEDS.lock().unwrap_or_else(PoisonError::into_inner);
    seeds
        .as_ref()
        .filter(|(kept, _)| dir.is_none_or(|dir| kept == dir))
        .map(|(_, seeds)| seeds.clone())
}

pub fn isolate<R: Runtime>(
    builder: WebviewBuilder<R>,
    dir: PathBuf,
    _profile: &str,
) -> WebviewBuilder<R> {
    builder.data_directory(dir)
}

fn rectangle(placement: Placement) -> gtk::gdk::Rectangle {
    gtk::gdk::Rectangle::new(
        placement.x.round() as i32,
        placement.y.round() as i32,
        placement.width.round() as i32,
        placement.height.round() as i32,
    )
}

/// A menu key the guest's page left unhandled: WebKit sends it on up the
/// widgets, where the app's own page, which runs the menu's keys, is not.
fn forward_menu_keys<R: Runtime>(overlay: &gtk::Overlay, app: tauri::AppHandle<R>) {
    use agentrq_desktop_core::menu_keys::{gtk_key, xkb_code, Pressed};
    use gtk::gdk::ModifierType;
    overlay.connect_key_press_event(move |overlay, event| {
        let from_guest = overlay
            .toplevel()
            .and_then(|top| top.downcast::<gtk::Window>().ok())
            .and_then(|window| window.focused_widget())
            .is_some_and(|focused| is_guest_label(focused.widget_name().as_str()));
        if !from_guest {
            return gtk::glib::Propagation::Proceed;
        }
        let keyval = event.keyval();
        let name = keyval.name().map(|n| n.to_string()).unwrap_or_default();
        let key = gtk_key(keyval.to_unicode(), &name);
        let state = event.state();
        let pressed = Pressed {
            key: &key,
            code: xkb_code(event.hardware_keycode()),
            ctrl: state.contains(ModifierType::CONTROL_MASK),
            shift: state.contains(ModifierType::SHIFT_MASK),
            alt: state.contains(ModifierType::MOD1_MASK),
            meta: state.contains(ModifierType::META_MASK),
        };
        let action = crate::app_menu::key_action(&app, pressed);
        if super::menu_key(&app, action, REPEAT.get()) {
            gtk::glib::Propagation::Stop
        } else {
            gtk::glib::Propagation::Proceed
        }
    });
}

/// Every key a guest gets, before WebKit does: a press of a key that is
/// already down is a repeat. GTK reports none itself.
fn watch_repeats(view: &webkit2gtk::WebView) {
    view.connect_key_press_event(|_, event| {
        let fresh = DOWN.with(|down| down.borrow_mut().insert(event.hardware_keycode()));
        REPEAT.set(!fresh);
        gtk::glib::Propagation::Proceed
    });
    view.connect_key_release_event(|_, event| {
        DOWN.with(|down| down.borrow_mut().remove(&event.hardware_keycode()));
        gtk::glib::Propagation::Proceed
    });
    // A key let go somewhere else must not count as held.
    view.connect_focus_out_event(|_, _| {
        DOWN.with(|down| down.borrow_mut().clear());
        gtk::glib::Propagation::Proceed
    });
}

/// The overlay the window's own webview sits in, made the first time and
/// made again for a window that has been rebuilt (a profile switch).
fn overlay_in<R: Runtime>(
    parent: &gtk::Box,
    guest: &gtk::Widget,
    app: tauri::AppHandle<R>,
) -> Option<gtk::Overlay> {
    OVERLAY.with(|cell| {
        if let Some(overlay) = cell.borrow().as_ref() {
            if overlay.parent().as_ref() == Some(parent.upcast_ref()) {
                return Some(overlay.clone());
            }
        }
        // The app's own webview is the other one Tauri packed into the box
        // (beside the menu bar).
        let main = parent
            .children()
            .into_iter()
            .find(|child| child != guest && child.is::<webkit2gtk::WebView>())?;
        let overlay = gtk::Overlay::new();
        parent.remove(&main);
        overlay.add(&main);
        overlay.connect_get_child_position(|_, child| {
            let name = child.widget_name();
            Some(BOXES.with(|boxes| {
                boxes
                    .borrow()
                    .get(name.as_str())
                    .copied()
                    .unwrap_or_else(|| gtk::gdk::Rectangle::new(0, 0, 0, 0))
            }))
        });
        parent.pack_start(&overlay, true, true, 0);
        forward_menu_keys(&overlay, app);
        overlay.show();
        *cell.borrow_mut() = Some(overlay.clone());
        Some(overlay)
    })
}

pub fn adopt<R: Runtime>(_main: &Webview<R>, guest: &Webview<R>, placement: Placement) {
    use tauri::Manager;
    let label = guest.label().to_string();
    let app = guest.app_handle().clone();
    let _ = guest.with_webview(move |view| {
        watch_repeats(&view.inner());
        let guest: gtk::Widget = view.inner().upcast();
        guest.set_widget_name(&label);
        BOXES.with(|boxes| {
            boxes
                .borrow_mut()
                .insert(label.clone(), rectangle(placement))
        });
        let Some(parent) = guest.parent().and_then(|p| p.downcast::<gtk::Box>().ok()) else {
            eprintln!("[side-panel] {label} is not in a box: it stays under the page");
            return;
        };
        let Some(overlay) = overlay_in(&parent, &guest, app) else {
            eprintln!("[side-panel] the window has no webview to lay {label} over");
            return;
        };
        parent.remove(&guest);
        overlay.add_overlay(&guest);
        guest.show();
    });
}

pub fn set_bounds<R: Runtime>(guest: &Webview<R>, placement: Placement) {
    let label = guest.label().to_string();
    let _ = guest.with_webview(move |_| {
        BOXES.with(|boxes| boxes.borrow_mut().insert(label, rectangle(placement)));
        OVERLAY.with(|cell| {
            if let Some(overlay) = cell.borrow().as_ref() {
                overlay.queue_resize();
            }
        });
    });
}

pub fn forget(label: &str) {
    let label = label.to_string();
    // Only ever touched on the main thread.
    gtk::glib::MainContext::default().invoke(move || {
        BOXES.with(|boxes| boxes.borrow_mut().remove(&label));
        NAVIGATIONS.with(|navigations| navigations.borrow_mut().remove(&label));
        COMMITTED.with(|committed| committed.borrow_mut().forget(&label));
        SHOWN.with(|shown| shown.borrow_mut().retain(|(guest, _), _| *guest != label));
    });
}

pub fn seed_notifications(dir: PathBuf, seeds: NotificationSeeds) {
    hook_notification_seeds();
    *SEEDS.lock().unwrap_or_else(PoisonError::into_inner) = Some((dir, seeds));
}

/// `initialize-notification-permissions`, which a context raises as each new
/// web process starts: the guests' context answers it from [`SEEDS`], so a
/// page reads `Notification.permission` as its site was answered. A hook
/// rather than a handler, because the first process can start before the
/// guest is handed to us.
unsafe extern "C" fn seed_process(
    _hint: *mut gtk::glib::gobject_ffi::GSignalInvocationHint,
    count: u32,
    params: *const gtk::glib::gobject_ffi::GValue,
    _data: gtk::glib::ffi::gpointer,
) -> gtk::glib::ffi::gboolean {
    use webkit2gtk::{SecurityOrigin, WebContextExt, WebsiteDataManagerExt};
    if count < 1 {
        return gtk::glib::ffi::GTRUE;
    }
    let values = std::slice::from_raw_parts(params.cast::<gtk::glib::Value>(), count as usize);
    let Ok(context) = values[0].get::<webkit2gtk::WebContext>() else {
        return gtk::glib::ffi::GTRUE;
    };
    let dir = context
        .website_data_manager()
        .and_then(|manager| manager.base_data_directory());
    let Some(seeds) = dir.and_then(|dir| seeds_for(Some(Path::new(dir.as_str())))) else {
        return gtk::glib::ffi::GTRUE;
    };
    let origins = |sites: &[String]| -> Vec<SecurityOrigin> {
        sites
            .iter()
            .map(|site| SecurityOrigin::for_uri(site))
            .collect()
    };
    let (allowed, blocked) = (origins(&seeds.allowed), origins(&seeds.blocked));
    context.initialize_notification_permissions(
        &allowed.iter().collect::<Vec<_>>(),
        &blocked.iter().collect::<Vec<_>>(),
    );
    gtk::glib::ffi::GTRUE
}

fn hook_notification_seeds() {
    use gtk::glib::translate::IntoGlib;
    use gtk::glib::StaticType;
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(|| unsafe {
        let signal = gtk::glib::gobject_ffi::g_signal_lookup(
            c"initialize-notification-permissions".as_ptr(),
            webkit2gtk::WebContext::static_type().into_glib(),
        );
        if signal != 0 {
            gtk::glib::gobject_ffi::g_signal_add_emission_hook(
                signal,
                0,
                Some(seed_process),
                std::ptr::null_mut(),
                None,
            );
        }
    });
}

/// A page's notification, shown through the app's own notifier while its
/// site is allowed: a page told "granted" keeps showing them after the
/// person removed the site's Allow, until its web process is replaced.
fn watch_notifications<R: Runtime>(
    view: &webkit2gtk::WebView,
    app: tauri::AppHandle<R>,
    label: String,
) {
    use tauri_plugin_notification::NotificationExt;
    use webkit2gtk::NotificationExt as _;
    view.connect_show_notification(move |_, notification| {
        let url = COMMITTED.with(|committed| committed.borrow().requesting_url(&label));
        if !seeds_for(None).is_some_and(|seeds| seeds.allows(&url)) {
            return true;
        }
        let id = notification.id();
        let key = (label.clone(), id);
        notification.connect_closed({
            let key = key.clone();
            move |_| {
                SHOWN.with(|shown| shown.borrow_mut().remove(&key));
            }
        });
        SHOWN.with(|shown| shown.borrow_mut().insert(key, notification.clone()));
        let _ = app
            .notification()
            .builder()
            .title(notification.title().unwrap_or_default())
            .body(notification.body().unwrap_or_default())
            .extra(
                crate::notify::SITE_KEY,
                serde_json::json!({"guest": label, "id": id}),
            )
            .show();
        true
    });
}

pub fn notification_clicked<R: Runtime>(app: &tauri::AppHandle<R>, label: &str, id: u64) {
    use webkit2gtk::NotificationExt;
    let key = (label.to_string(), id);
    let _ = app.run_on_main_thread(move || {
        let shown = SHOWN.with(|shown| shown.borrow().get(&key).cloned());
        if let Some(notification) = shown {
            notification.clicked();
        }
    });
}

/// `decide-policy` as WebKit raised it, before wry's handler, which keeps the
/// navigation action to itself: a guest's next navigation is checked against
/// what this recorded.
unsafe extern "C" fn record_navigation(
    _hint: *mut gtk::glib::gobject_ffi::GSignalInvocationHint,
    count: u32,
    params: *const gtk::glib::gobject_ffi::GValue,
    _data: gtk::glib::ffi::gpointer,
) -> gtk::glib::ffi::gboolean {
    use webkit2gtk::{NavigationPolicyDecision, NavigationPolicyDecisionExt, NavigationType};
    use webkit2gtk::{PolicyDecision, PolicyDecisionType};
    if count < 3 {
        return gtk::glib::ffi::GTRUE;
    }
    let values = std::slice::from_raw_parts(params.cast::<gtk::glib::Value>(), count as usize);
    let (Ok(view), Ok(decision), Ok(kind)) = (
        values[0].get::<webkit2gtk::WebView>(),
        values[1].get::<PolicyDecision>(),
        values[2].get::<PolicyDecisionType>(),
    ) else {
        return gtk::glib::ffi::GTRUE;
    };
    let label = view.widget_name().to_string();
    if kind != PolicyDecisionType::NavigationAction || !is_guest_label(&label) {
        return gtk::glib::ffi::GTRUE;
    }
    let Some(action) = decision
        .downcast_ref::<NavigationPolicyDecision>()
        .and_then(|decision| decision.navigation_action())
    else {
        return gtk::glib::ffi::GTRUE;
    };
    let state = gtk::gdk::ModifierType::from_bits_truncate(action.modifiers());
    let kind = NavigationKind {
        link_clicked: action.navigation_type() == NavigationType::LinkClicked,
        user_gesture: action.is_user_gesture(),
        held: Modifiers {
            control: state.contains(gtk::gdk::ModifierType::CONTROL_MASK),
            meta: state.contains(gtk::gdk::ModifierType::META_MASK),
            middle: action.mouse_button() == 2,
        },
    };
    NAVIGATIONS.with(|navigations| navigations.borrow_mut().insert(label, kind));
    gtk::glib::ffi::GTRUE
}

/// The emission hook, once for every WebKitWebView in the process.
fn hook_navigations() {
    use gtk::glib::translate::IntoGlib;
    use gtk::glib::StaticType;
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(|| unsafe {
        let signal = gtk::glib::gobject_ffi::g_signal_lookup(
            c"decide-policy".as_ptr(),
            webkit2gtk::WebView::static_type().into_glib(),
        );
        if signal != 0 {
            gtk::glib::gobject_ffi::g_signal_add_emission_hook(
                signal,
                0,
                Some(record_navigation),
                std::ptr::null_mut(),
                None,
            );
        }
    });
}

/// The modifiers of the click a guest's navigation came from, or `None` when
/// a script, a form or a frame's own load started it.
pub fn click(number: u32) -> Option<Modifiers> {
    let label = agentrq_desktop_core::side_panel::child::guest_label(number);
    let kind = NAVIGATIONS.with(|navigations| navigations.borrow_mut().remove(&label))?;
    link_click(kind)
}

/// Cmd/Ctrl+F, before the page sees it: the window's find bar opens instead.
fn watch_find_key<R: Runtime>(view: &webkit2gtk::WebView, app: tauri::AppHandle<R>) {
    use agentrq_desktop_core::menu_keys::gtk_key;
    use agentrq_desktop_core::side_panel::wire::KeyInput;
    use gtk::gdk::ModifierType;
    view.connect_key_press_event(move |_, event| {
        let keyval = event.keyval();
        let name = keyval.name().map(|n| n.to_string()).unwrap_or_default();
        let state = event.state();
        let input = KeyInput {
            kind: "keyDown".to_string(),
            key: gtk_key(keyval.to_unicode(), &name),
            shift: state.contains(ModifierType::SHIFT_MASK),
            alt: state.contains(ModifierType::MOD1_MASK),
            control: state.contains(ModifierType::CONTROL_MASK),
            meta: state.contains(ModifierType::META_MASK),
        };
        if super::key_down(&app, &input) {
            gtk::glib::Propagation::Stop
        } else {
            gtk::glib::Propagation::Proceed
        }
    });
}

/// What WebKit's find controller counts, for the window's find bar.
fn watch_find<R: Runtime>(view: &webkit2gtk::WebView, app: tauri::AppHandle<R>, label: String) {
    use webkit2gtk::FindControllerExt;
    let Some(finder) = view.find_controller() else {
        return;
    };
    let (failed_app, failed_label) = (app.clone(), label.clone());
    finder.connect_found_text(move |_, count| super::counted(&app, &label, count));
    finder.connect_failed_to_find_text(move |_| super::counted(&failed_app, &failed_label, 0));
}

/// The address a guest's document has committed to: its own `uri()` is the
/// next one's as soon as a load starts.
fn watch_commits(view: &webkit2gtk::WebView, label: String) {
    use webkit2gtk::LoadEvent;
    view.connect_load_changed(move |view, event| {
        let step = match event {
            LoadEvent::Started => WebKitLoad::Started,
            LoadEvent::Redirected => WebKitLoad::Redirected,
            LoadEvent::Committed => WebKitLoad::Committed,
            _ => WebKitLoad::Finished,
        };
        let uri = view.uri().map(|u| u.to_string()).unwrap_or_default();
        COMMITTED.with(|committed| committed.borrow_mut().load_changed(&label, step, &uri));
    });
}

/// A page's permission request, held until the broker answers it on the
/// main thread. The page never answers its own: only the window does.
fn watch_permissions<R: Runtime>(
    view: &webkit2gtk::WebView,
    app: tauri::AppHandle<R>,
    label: String,
) {
    use agentrq_desktop_core::side_panel::platform_permissions::webkit_ask;
    use gtk::glib::object::Cast;
    use webkit2gtk::{UserMediaPermissionRequest, UserMediaPermissionRequestExt};
    view.connect_permission_request(move |_, request| {
        let media = request.downcast_ref::<UserMediaPermissionRequest>();
        let audio = media.is_some_and(|m| m.is_for_audio_device());
        let video = media.is_some_and(|m| m.is_for_video_device());
        // A screen is neither, which is all WebKit before 2.42 says of it.
        let display = media.is_some() && !audio && !video;
        let url = COMMITTED.with(|committed| committed.borrow().requesting_url(&label));
        let native = webkit_ask(request.type_().name(), audio, video, display, &url);
        let id = NEXT_REQUEST.get();
        NEXT_REQUEST.set(id + 1);
        WAITING.with(|waiting| waiting.borrow_mut().insert(id, request.clone()));
        let main = app.clone();
        crate::panel_permissions::request(
            &app,
            &label,
            &url,
            native,
            crate::panel_permissions::Answer::new(move |granted| {
                let _ = main.run_on_main_thread(move || settle(id, granted));
            }),
        );
        true
    });
}

fn settle(id: u64, granted: bool) {
    use webkit2gtk::PermissionRequestExt;
    let Some(request) = WAITING.with(|waiting| waiting.borrow_mut().remove(&id)) else {
        return;
    };
    if granted {
        request.allow();
    } else {
        request.deny();
    }
}

pub fn find<R: Runtime>(
    guest: &Webview<R>,
    text: &str,
    step: agentrq_desktop_core::side_panel::find::FindStep,
) {
    use agentrq_desktop_core::side_panel::find::FindStep;
    use webkit2gtk::{FindControllerExt, FindOptions};
    let text = text.to_string();
    let _ = guest.with_webview(move |view| {
        let Some(finder) = view.inner().find_controller() else {
            return;
        };
        match step {
            FindStep::Start => finder.search(
                &text,
                (FindOptions::CASE_INSENSITIVE | FindOptions::WRAP_AROUND).bits(),
                MAX_MATCHES,
            ),
            FindStep::Next => finder.search_next(),
            FindStep::Previous => finder.search_previous(),
        }
    });
}

/// More than anybody steps through; WebKit stops counting there.
const MAX_MATCHES: u32 = 10_000;

pub fn stop_find<R: Runtime>(guest: &Webview<R>, clear: bool) {
    use webkit2gtk::FindControllerExt;
    let _ = guest.with_webview(move |view| {
        if let Some(finder) = view.inner().find_controller() {
            finder.search_finish();
        }
    });
    if clear {
        let _ = guest.eval(agentrq_desktop_core::side_panel::find::CLEAR_SELECTION_SCRIPT);
    }
}

/// A load that failed, for the panel's own error view; the guest taking the
/// keyboard; its find key, find counts and permission requests.
pub fn watch<R: Runtime>(guest: &Webview<R>) {
    use agentrq_desktop_core::side_panel::child::GuestEvent;
    use tauri::{Emitter, Manager};
    let app = guest.app_handle().clone();
    let label = guest.label().to_string();
    let _ = guest.with_webview(move |view| {
        hook_navigations();
        // WebKitGTK ships without `getUserMedia` and WebRTC, which Electron's
        // guest has: the broker answers whether a page may use the camera.
        // A screen is refused (`webkit_ask`), as Electron's guest has none.
        if let Some(settings) = WebViewExt::settings(&view.inner()) {
            use webkit2gtk::SettingsExt;
            settings.set_enable_media_stream(true);
            settings.set_enable_webrtc(true);
        }
        watch_find_key(&view.inner(), app.clone());
        watch_find(&view.inner(), app.clone(), label.clone());
        watch_commits(&view.inner(), label.clone());
        watch_permissions(&view.inner(), app.clone(), label.clone());
        watch_notifications(&view.inner(), app.clone(), label.clone());
        let (focus_app, focus_label) = (app.clone(), label.clone());
        view.inner().connect_focus_in_event(move |_, _| {
            let _ = focus_app.emit_to(
                crate::chrome::MAIN,
                super::GUEST_EVENT,
                &GuestEvent::Focus {
                    label: focus_label.clone(),
                },
            );
            gtk::glib::Propagation::Proceed
        });
        view.inner().connect_load_failed(move |_, _, _, error| {
            // A navigation replaced by another, or one the rules refused.
            if error.matches(webkit2gtk::NetworkError::Cancelled)
                || error.kind::<webkit2gtk::PolicyError>().is_some()
            {
                return false;
            }
            let _ = app.emit_to(
                crate::chrome::MAIN,
                super::GUEST_EVENT,
                &GuestEvent::Fail {
                    label: label.clone(),
                    code: 0,
                    description: error.message().to_string(),
                },
            );
            false
        });
    });
}

/// The link key held now: by the time WebKit asks for a window, the click
/// that asked has been handled, but the key is still down.
pub fn held() -> Modifiers {
    let state = gtk::gdk::Display::default()
        .and_then(|display| gtk::gdk::Keymap::for_display(&display))
        .map(|keymap| gtk::gdk::ModifierType::from_bits_truncate(keymap.modifier_state()))
        .unwrap_or_else(gtk::gdk::ModifierType::empty);
    Modifiers {
        control: state.contains(gtk::gdk::ModifierType::CONTROL_MASK),
        meta: state.contains(gtk::gdk::ModifierType::META_MASK),
        middle: false,
    }
}

pub async fn history<R: Runtime>(guest: &Webview<R>) -> (bool, bool) {
    let (tx, rx) = tokio::sync::oneshot::channel();
    let _ = guest.with_webview(move |view| {
        let view = view.inner();
        let _ = tx.send((view.can_go_back(), view.can_go_forward()));
    });
    rx.await.unwrap_or_default()
}

pub fn go<R: Runtime>(guest: &Webview<R>, step: i32) {
    let _ = guest.with_webview(move |view| {
        if step < 0 {
            view.inner().go_back();
        } else {
            view.inner().go_forward();
        }
    });
}

pub async fn snapshot<R: Runtime>(guest: &Webview<R>) -> Result<Vec<u8>, String> {
    use webkit2gtk::{SnapshotOptions, SnapshotRegion};
    let (tx, rx) = tokio::sync::oneshot::channel();
    guest
        .with_webview(move |view| {
            view.inner().snapshot(
                SnapshotRegion::Visible,
                SnapshotOptions::NONE,
                None::<&gtk::gio::Cancellable>,
                move |result| {
                    let png = result.map_err(|e| e.to_string()).and_then(|surface| {
                        let image = cairo::ImageSurface::try_from(surface)
                            .map_err(|_| "not an image".to_string())?;
                        let mut png = Vec::new();
                        image.write_to_png(&mut png).map_err(|e| e.to_string())?;
                        Ok(png)
                    });
                    let _ = tx.send(png);
                },
            );
        })
        .map_err(|e| e.to_string())?;
    super::within(rx).await
}
