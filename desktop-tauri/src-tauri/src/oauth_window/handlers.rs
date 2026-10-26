// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The authorisation window and its callbacks, which only a real webview
//! raises: each hands what it was told to `Pending`, where it is decided and
//! tested. Outside the coverage gate.

use std::sync::Arc;

use agentrq_desktop_core::oauth_window::OAuthError;
use tauri::webview::NewWindowResponse;
use tauri::{AppHandle, Runtime, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};
use url::Url;

use super::Pending;

/// Built on a blank page, so the session can be lent before the authorise
/// page asks for it. No preload and no capability: it shows a page that
/// authenticates somebody. A popup it asks for is refused.
pub fn build<R: Runtime>(
    app: &AppHandle<R>,
    label: &str,
    profile: &str,
    pending: Arc<Pending>,
) -> tauri::Result<WebviewWindow<R>> {
    let blank = Url::parse("about:blank").expect("static url");
    #[cfg(windows)]
    let pending_frames = pending.clone();
    let (nav, closing) = (pending.clone(), pending);
    let builder = WebviewWindowBuilder::new(app, label, WebviewUrl::External(blank))
        .title("Authorise AgentRQ")
        .inner_size(520.0, 720.0)
        // tauri-runtime-wry lets a URL that does not parse load without asking
        // this. On Windows `guard_frames` cancels any external launch it makes.
        .on_navigation(move |url| nav.navigate(url))
        .on_new_window(|_, _| NewWindowResponse::Deny);
    let window = isolate(builder, app, profile).build()?;
    #[cfg(windows)]
    guard_frames(&window, pending_frames);
    // Closing it is declining. The run takes its cookies back and then
    // destroys it, so every close waits for that.
    window.on_window_event(move |event| match event {
        WindowEvent::CloseRequested { api, .. } if closing.close_requested() => api.prevent_close(),
        WindowEvent::Destroyed => {
            closing.end(OAuthError::Closed);
        }
        _ => {}
    });
    Ok(window)
}

/// wry asks `on_navigation` for WebView2's top frame only. So a subframe's
/// navigation is asked here, and WebView2 never hands a URL to another app,
/// whichever frame asks: the redirect would reach the app that claims it.
///
/// Either guard can fail to attach, on a runtime too old for
/// `ICoreWebView2_18` among others: the window still opens, and the log says
/// what it runs without. Neither handler is ever removed. Each holds only an
/// `Arc<Pending>`, which holds nothing of the webview, so both go with it.
#[cfg(windows)]
fn guard_frames<R: Runtime>(window: &WebviewWindow<R>, pending: Arc<Pending>) {
    use webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2_18;
    use webview2_com::{LaunchingExternalUriSchemeEventHandler, NavigationStartingEventHandler};
    let attached = window.with_webview(move |view| unsafe {
        let core = match view.controller().CoreWebView2() {
            Ok(core) => core,
            Err(error) => return eprintln!("[oauth] no frame guard: {error}"),
        };
        let mut token = Default::default();
        let frames = NavigationStartingEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            let mut uri = windows::core::PWSTR::null();
            args.Uri(&mut uri)?;
            if !pending.navigate_frame(&webview2_com::take_pwstr(uri)) {
                args.SetCancel(true)?;
            }
            Ok(())
        }));
        if let Err(error) = core.add_FrameNavigationStarting(&frames, &mut token) {
            eprintln!("[oauth] no subframe guard: {error}");
        }
        let core = match windows::core::Interface::cast::<ICoreWebView2_18>(&core) {
            Ok(core) => core,
            Err(error) => return eprintln!("[oauth] no external launch guard: {error}"),
        };
        let launch = LaunchingExternalUriSchemeEventHandler::create(Box::new(|_, args| {
            if let Some(args) = args {
                args.SetCancel(true)?;
            }
            Ok(())
        }));
        if let Err(error) = core.add_LaunchingExternalUriScheme(&launch, &mut token) {
            eprintln!("[oauth] no external launch guard: {error}");
        }
    });
    if let Err(error) = attached {
        eprintln!("[oauth] no frame guard: {error}");
    }
}

/// The profile's own storage, as its sign-in window has: a folder on Linux and
/// Windows.
#[cfg(not(target_os = "macos"))]
fn isolate<'a, R: Runtime>(
    builder: WebviewWindowBuilder<'a, R, AppHandle<R>>,
    app: &AppHandle<R>,
    profile: &str,
) -> WebviewWindowBuilder<'a, R, AppHandle<R>> {
    use tauri::Manager;
    let profiles = app.state::<Arc<crate::profiles::Profiles>>();
    crate::chrome::isolate(builder, &profiles, profile)
}

/// The profile's store from macOS 14; a store never written to disk below,
/// where wry would give a named one the app's default store.
#[cfg(target_os = "macos")]
fn isolate<'a, R: Runtime>(
    builder: WebviewWindowBuilder<'a, R, AppHandle<R>>,
    _app: &AppHandle<R>,
    profile: &str,
) -> WebviewWindowBuilder<'a, R, AppHandle<R>> {
    use agentrq_desktop_core::oauth_window::macos_store;
    use agentrq_desktop_core::side_panel::child::GuestStore;
    let version = objc2_foundation::NSProcessInfo::processInfo().operatingSystemVersion();
    match macos_store(profile, version.majorVersion) {
        GuestStore::Named(store) => builder.data_store_identifier(store),
        GuestStore::Ephemeral => builder.incognito(true),
    }
}
