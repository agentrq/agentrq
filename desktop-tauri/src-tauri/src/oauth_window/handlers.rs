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
    let (nav, closing) = (pending.clone(), pending);
    let builder = WebviewWindowBuilder::new(app, label, WebviewUrl::External(blank))
        .title("Authorise AgentRQ")
        .inner_size(520.0, 720.0)
        .on_navigation(move |url| nav.navigate(url))
        .on_new_window(|_, _| NewWindowResponse::Deny);
    let window = isolate(builder, app, profile).build()?;
    // Closing it is declining. The flow takes its cookies back and then
    // destroys it, so the close waits for that.
    window.on_window_event(move |event| match event {
        WindowEvent::CloseRequested { api, .. } if closing.end(OAuthError::Closed) => {
            api.prevent_close()
        }
        WindowEvent::Destroyed => {
            closing.end(OAuthError::Closed);
        }
        _ => {}
    });
    Ok(window)
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
