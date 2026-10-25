// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! What WKWebView is asked directly for the side panel's guest, because
//! Tauri does not. Native glue, outside the coverage gate: the decisions are
//! `core`'s and `side_panel.rs`'s.

use std::path::PathBuf;

use agentrq_desktop_core::side_panel::child::{
    guest_store, link_click, GuestStore, Modifiers, NavigationKind, Placement,
};
use objc2_app_kit::{NSEvent, NSEventModifierFlags, NSEventType};
use objc2_web_kit::WKWebView;
use tauri::webview::WebviewBuilder;
use tauri::{LogicalPosition, LogicalSize, Runtime, Webview};

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

pub fn forget(_label: &str) {}

/// WKWebView reports a failed load to wry's own navigation delegate, which
/// it does not expose: the page shows WebKit's empty view instead.
pub fn watch<R: Runtime>(_guest: &Webview<R>) {}

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
