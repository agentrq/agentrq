// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The guest webview's callbacks, which only a real webview raises: each
//! hands what it was told to `side_panel.rs`, where it is decided and tested.
//! Outside the coverage gate.

use tauri::webview::WebviewBuilder;
use tauri::Runtime;

use super::{page_load, platform, Guest};

pub fn attach<R: Runtime>(builder: WebviewBuilder<R>, guest: Guest<R>) -> WebviewBuilder<R> {
    let (nav, opener) = (guest.clone(), guest);
    builder
        .on_navigation(move |url| nav.allows(url.as_str(), platform::click(nav.number)))
        .on_new_window(move |url, _| opener.new_window(url.as_str(), platform::held()))
        .on_page_load(|webview, payload| page_load(webview, payload.event()))
}
