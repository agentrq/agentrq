// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What WKWebView is asked directly for the side panel's guest, because
//! Tauri does not. Native glue, outside the coverage gate: the decisions are
//! `core`'s and `side_panel.rs`'s.

use std::cell::{Cell, RefCell};
use std::collections::HashMap;
use std::path::PathBuf;
use std::ptr::NonNull;
use std::sync::{Arc, Mutex, OnceLock, PoisonError};

use agentrq_desktop_core::side_panel::child::{
    guest_store, link_click, GuestStore, Modifiers, NavigationKind, Placement,
};
use agentrq_desktop_core::side_panel::platform_permissions::NativeAsk;
use block2::{Block, RcBlock};
use objc2::runtime::{AnyObject, Imp, Sel};
use objc2::{msg_send, sel, Message};
use objc2_app_kit::{NSEvent, NSEventMask, NSEventModifierFlags, NSEventType};
use objc2_foundation::{NSError, NSString};
use objc2_web_kit::WKWebView;
use tauri::webview::WebviewBuilder;
use tauri::{LogicalPosition, LogicalSize, Manager, Runtime, Webview};

/// What a guest's media request is handed to: the request, the address of
/// the origin asking, and the id its held decision is kept under.
type Asker = Arc<dyn Fn(NativeAsk, &str, u64) + Send + Sync>;

/// Every guest's WKWebView, by its address, with its label and asker. A
/// request from any other webview (the app's own) keeps wry's answer.
static GUESTS: Mutex<Option<HashMap<usize, (String, Asker)>>> = Mutex::new(None);

/// wry's own media capture decision, for every webview that is not a guest.
static ORIGINAL: OnceLock<Imp> = OnceLock::new();

thread_local! {
    /// The decisions a guest's page is waiting on, until the broker answers.
    static WAITING: RefCell<HashMap<u64, Decision>> = RefCell::new(HashMap::new());
    static NEXT_REQUEST: Cell<u64> = const { Cell::new(1) };
}

/// A held `decisionHandler`, called with a `WKPermissionDecision`.
type Decision = RcBlock<dyn Fn(isize)>;

/// `WKPermissionDecision`.
const GRANT: isize = 1;
const DENY: isize = 2;

type MediaImp = unsafe extern "C-unwind" fn(
    *mut AnyObject,
    Sel,
    *mut AnyObject,
    *mut AnyObject,
    *mut AnyObject,
    isize,
    *mut Block<dyn Fn(isize)>,
);

fn guests<T>(f: impl FnOnce(&mut HashMap<usize, (String, Asker)>) -> T) -> T {
    let mut guests = GUESTS.lock().unwrap_or_else(PoisonError::into_inner);
    f(guests.get_or_insert_with(HashMap::new))
}

/// The address of a `WKSecurityOrigin`.
unsafe fn origin_url(origin: *mut AnyObject) -> String {
    use agentrq_desktop_core::side_panel::platform_permissions::security_origin_url;
    if origin.is_null() {
        return String::new();
    }
    let protocol: Option<objc2::rc::Retained<NSString>> = msg_send![origin, protocol];
    let host: Option<objc2::rc::Retained<NSString>> = msg_send![origin, host];
    let port: isize = msg_send![origin, port];
    let text =
        |s: Option<objc2::rc::Retained<NSString>>| s.map(|s| s.to_string()).unwrap_or_default();
    security_origin_url(&text(protocol), &text(host), port)
}

/// `webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:`
/// in place of wry's, which grants every webview the camera and microphone:
/// a guest's request waits for the broker, anything else goes to wry's.
unsafe extern "C-unwind" fn media_capture(
    this: *mut AnyObject,
    cmd: Sel,
    webview: *mut AnyObject,
    origin: *mut AnyObject,
    frame: *mut AnyObject,
    kind: isize,
    handler: *mut Block<dyn Fn(isize)>,
) {
    use agentrq_desktop_core::side_panel::platform_permissions::wk_media_ask;
    let asker = guests(|guests| {
        guests
            .get(&(webview as usize))
            .map(|(_, asker)| Arc::clone(asker))
    });
    let Some(asker) = asker else {
        if let Some(original) = ORIGINAL.get() {
            let original: MediaImp = std::mem::transmute(*original);
            original(this, cmd, webview, origin, frame, kind, handler);
        }
        return;
    };
    let Some(handler) = handler.as_ref() else {
        return;
    };
    let url = origin_url(origin);
    let id = NEXT_REQUEST.get();
    NEXT_REQUEST.set(id + 1);
    WAITING.with(|waiting| waiting.borrow_mut().insert(id, handler.copy()));
    asker(wk_media_ask(kind, &url), &url, id);
}

fn settle(id: u64, granted: bool) {
    if let Some(decide) = WAITING.with(|waiting| waiting.borrow_mut().remove(&id)) {
        decide.call((if granted { GRANT } else { DENY },));
    }
}

/// Put [`media_capture`] in wry's delegate class, once: every WKWebView of
/// the app has an instance of it. Before macOS 12 there is no such request.
fn hook_media_capture(view: &WKWebView) {
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(|| unsafe {
        let delegate: *mut AnyObject = msg_send![view, UIDelegate];
        let Some(delegate) = delegate.as_ref() else {
            return;
        };
        let selector =
            sel!(webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:);
        let Some(method) = delegate.class().instance_method(selector) else {
            return;
        };
        let ours: MediaImp = media_capture;
        let original = method.set_implementation(std::mem::transmute::<MediaImp, Imp>(ours));
        let _ = ORIGINAL.set(original);
    });
}

/// Whether a key event is for a guest: its window's first responder is a
/// guest's WKWebView, or a view inside one.
unsafe fn in_guest(event: &NSEvent) -> bool {
    let window: *mut AnyObject = msg_send![event, window];
    if window.is_null() {
        return false;
    }
    let ours: Vec<usize> = guests(|guests| guests.keys().copied().collect());
    let mut view: *mut AnyObject = msg_send![window, firstResponder];
    while !view.is_null() {
        if ours.contains(&(view as usize)) {
            return true;
        }
        let nested: bool = msg_send![view, respondsToSelector: sel!(superview)];
        if !nested {
            return false;
        }
        view = msg_send![view, superview];
    }
    false
}

/// Cmd+F in a guest, before its page sees it: the window's find bar opens
/// instead. One monitor for the app, which checks where the key is going.
fn watch_find_key<R: Runtime>(app: tauri::AppHandle<R>) {
    use agentrq_desktop_core::side_panel::wire::KeyInput;
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(move || unsafe {
        let block = RcBlock::new(move |event: NonNull<NSEvent>| -> *mut NSEvent {
            let pressed = event.as_ref();
            if in_guest(pressed) {
                let flags = pressed.modifierFlags();
                let input = KeyInput {
                    kind: "keyDown".to_string(),
                    key: pressed
                        .charactersIgnoringModifiers()
                        .map(|key| key.to_string())
                        .unwrap_or_default(),
                    shift: flags.contains(NSEventModifierFlags::Shift),
                    alt: flags.contains(NSEventModifierFlags::Option),
                    control: flags.contains(NSEventModifierFlags::Control),
                    meta: flags.contains(NSEventModifierFlags::Command),
                };
                if super::key_down(&app, &input) {
                    return std::ptr::null_mut();
                }
            }
            event.as_ptr()
        });
        if let Some(monitor) =
            NSEvent::addLocalMonitorForEventsMatchingMask_handler(NSEventMask::KeyDown, &block)
        {
            // For the life of the app.
            std::mem::forget(monitor);
        }
    });
}

pub fn find<R: Runtime>(
    guest: &Webview<R>,
    text: &str,
    step: agentrq_desktop_core::side_panel::find::FindStep,
) {
    use agentrq_desktop_core::side_panel::find::{count_script, FindStep, CLEAR_SELECTION_SCRIPT};
    use objc2::MainThreadMarker;
    use objc2_web_kit::{WKContentWorld, WKFindConfiguration, WKFindResult};
    let text = text.to_string();
    let app = guest.app_handle().clone();
    let label = guest.label().to_string();
    let _ = guest.with_webview(move |view| unsafe {
        let view = wk(&view);
        let Some(mtm) = MainThreadMarker::new() else {
            return;
        };
        // A new search starts at the top, not after the last match.
        if step == FindStep::Start {
            view.evaluateJavaScript_completionHandler(
                &NSString::from_str(CLEAR_SELECTION_SCRIPT),
                None,
            );
        }
        let config = WKFindConfiguration::new(mtm);
        config.setBackwards(step == FindStep::Previous);
        config.setCaseSensitive(false);
        config.setWraps(true);
        // WKWebView says only whether there was a match: a script counts
        // them, in a world of its own, so the page never reads the text.
        let counter = view.retain();
        let world = WKContentWorld::defaultClientWorld(mtm);
        let script = NSString::from_str(&count_script(&text));
        let done = RcBlock::new(move |_: NonNull<WKFindResult>| {
            let (app, label) = (app.clone(), label.clone());
            let counted = RcBlock::new(move |value: *mut AnyObject, _: *mut NSError| {
                let total: isize = if value.is_null() {
                    0
                } else {
                    msg_send![value, integerValue]
                };
                super::counted(&app, &label, u32::try_from(total).unwrap_or(0));
            });
            counter.evaluateJavaScript_inFrame_inContentWorld_completionHandler(
                &script,
                None,
                &world,
                Some(&counted),
            );
        });
        view.findString_withConfiguration_completionHandler(
            &NSString::from_str(&text),
            Some(&config),
            &done,
        );
    });
}

/// WKWebView keeps no find session: only the selection can go.
pub fn stop_find<R: Runtime>(guest: &Webview<R>, clear: bool) {
    if clear {
        let _ = guest.eval(agentrq_desktop_core::side_panel::find::CLEAR_SELECTION_SCRIPT);
    }
}

fn wk(view: &tauri::webview::PlatformWebview) -> &WKWebView {
    unsafe { &*(view.inner() as *const WKWebView) }
}

/// The profile's panel store from macOS 14; before that a store that is
/// never written to disk, never the default one the app's window uses.
pub fn isolate<R: Runtime>(
    builder: WebviewBuilder<R>,
    _dir: PathBuf,
    profile: &str,
) -> WebviewBuilder<R> {
    let version = objc2_foundation::NSProcessInfo::processInfo().operatingSystemVersion();
    match guest_store(profile, version.majorVersion) {
        GuestStore::Named(store) => builder.data_store_identifier(store),
        GuestStore::Ephemeral => builder.incognito(true),
    }
}

pub fn adopt<R: Runtime>(_main: &Webview<R>, _guest: &Webview<R>, _placement: Placement) {}

pub fn set_bounds<R: Runtime>(guest: &Webview<R>, placement: Placement) {
    let _ = guest.set_bounds(tauri::Rect {
        position: LogicalPosition::new(placement.x, placement.y).into(),
        size: LogicalSize::new(placement.width, placement.height).into(),
    });
}

pub fn forget(label: &str) {
    guests(|guests| guests.retain(|_, (kept, _)| kept != label));
}

/// WKWebView reports a failed load to wry's own navigation delegate, which
/// it does not expose: the page shows WebKit's empty view instead. Its
/// camera and microphone requests and Cmd+F are the guest's to watch.
pub fn watch<R: Runtime>(guest: &Webview<R>) {
    let app = guest.app_handle().clone();
    let label = guest.label().to_string();
    let _ = guest.with_webview(move |view| {
        let view = wk(&view);
        hook_media_capture(view);
        watch_find_key(app.clone());
        let (asking, name) = (app, label.clone());
        let asker: Asker = Arc::new(move |native, url, id| {
            let main = asking.clone();
            crate::panel_permissions::request(
                &asking,
                &name,
                url,
                native,
                crate::panel_permissions::Answer::new(move |granted| {
                    let _ = main.run_on_main_thread(move || settle(id, granted));
                }),
            );
        });
        guests(|guests| guests.insert(view as *const WKWebView as usize, (label, asker)));
    });
}

pub fn held() -> Modifiers {
    let flags = NSEvent::modifierFlags_class();
    Modifiers {
        control: flags.contains(NSEventModifierFlags::Control),
        meta: flags.contains(NSEventModifierFlags::Command),
        middle: false,
    }
}

/// The click a guest's navigation came from: wry keeps the navigation action
/// to itself, so the app's last event stands in for it. Only a mouse click of
/// the last second counts; a key, a script's timer or a frame's own load
/// leaves something else, or something older, there.
pub fn click(_number: u32) -> Option<Modifiers> {
    let mtm = objc2::MainThreadMarker::new()?;
    let event = objc2_app_kit::NSApplication::sharedApplication(mtm).currentEvent()?;
    let kind = event.r#type();
    let middle = kind == NSEventType::OtherMouseUp || kind == NSEventType::OtherMouseDown;
    let clicked = middle || kind == NSEventType::LeftMouseUp || kind == NSEventType::LeftMouseDown;
    let now = objc2_foundation::NSProcessInfo::processInfo().systemUptime();
    let recent = now - event.timestamp() < 1.0;
    let flags = event.modifierFlags();
    link_click(NavigationKind {
        link_clicked: clicked && recent,
        user_gesture: clicked && recent,
        held: Modifiers {
            control: flags.contains(NSEventModifierFlags::Control),
            meta: flags.contains(NSEventModifierFlags::Command),
            middle,
        },
    })
}

pub async fn history<R: Runtime>(guest: &Webview<R>) -> (bool, bool) {
    let (tx, rx) = tokio::sync::oneshot::channel();
    let _ = guest.with_webview(move |view| {
        let view = wk(&view);
        let _ = tx.send(unsafe { (view.canGoBack(), view.canGoForward()) });
    });
    rx.await.unwrap_or_default()
}

pub fn go<R: Runtime>(guest: &Webview<R>, step: i32) {
    let _ = guest.with_webview(move |view| unsafe {
        let view = wk(&view);
        if step < 0 {
            view.goBack();
        } else {
            view.goForward();
        }
    });
}

pub async fn snapshot<R: Runtime>(guest: &Webview<R>) -> Result<Vec<u8>, String> {
    use block2::RcBlock;
    use objc2::MainThreadMarker;
    use objc2_app_kit::{NSBitmapImageFileType, NSBitmapImageRep, NSImage};
    use objc2_foundation::{NSDictionary, NSError};
    use objc2_web_kit::WKSnapshotConfiguration;
    use std::cell::RefCell;

    let (tx, rx) = tokio::sync::oneshot::channel::<Result<Vec<u8>, String>>();
    guest
        .with_webview(move |view| {
            let view = wk(&view);
            let Some(mtm) = MainThreadMarker::new() else {
                return;
            };
            let config = unsafe { WKSnapshotConfiguration::new(mtm) };
            let tx = RefCell::new(Some(tx));
            let done = RcBlock::new(move |image: *mut NSImage, _error: *mut NSError| {
                let png = if image.is_null() {
                    Err("no snapshot".to_string())
                } else {
                    let image = unsafe { &*image };
                    image
                        .TIFFRepresentation()
                        .and_then(|tiff| NSBitmapImageRep::imageRepWithData(&tiff))
                        .and_then(|rep| unsafe {
                            rep.representationUsingType_properties(
                                NSBitmapImageFileType::PNG,
                                &NSDictionary::new(),
                            )
                        })
                        .map(|png| png.to_vec())
                        .ok_or_else(|| "no PNG".to_string())
                };
                if let Some(tx) = tx.borrow_mut().take() {
                    let _ = tx.send(png);
                }
            });
            unsafe { view.takeSnapshotWithConfiguration_completionHandler(Some(&config), &done) };
        })
        .map_err(|e| e.to_string())?;
    super::within(rx).await
}
