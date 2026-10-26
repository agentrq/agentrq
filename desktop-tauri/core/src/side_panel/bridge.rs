// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! An Extension and its own pages in the side panel, talking to each other.
//!
//! The rules are Electron's `side-panel/bridge.js`, which the extension host
//! runs unchanged beside the Extensions it answers for. What is left to the
//! shell is what Electron's main process does around it: who a message is for
//! is decided by **the address of the page that sent it** —
//! `agentrq-ext://<extension>/…` reaches that Extension and no other — and
//! never by anything in the message. A website in the panel has no bridge to
//! begin with, and if it had one its address would name no Extension, so it
//! would reach nothing.

use url::Url;

use super::guest::{host_of, normalize_ext_url, EXT_SCHEME};

/// A message is a request between two pieces of this app, not a file transfer.
pub const MAX_MESSAGE_BYTES: usize = 1024 * 1024;

/// Why a call was refused.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Refusal {
    pub reason: String,
}

/// The Extension an address belongs to, or `""` for anything that is not one
/// of its pages. `webview2_ext_urls` is as for [`normalize_ext_url`].
pub fn extension_of(raw_url: &str, webview2_ext_urls: bool) -> String {
    match Url::parse(&normalize_ext_url(raw_url, webview2_ext_urls)) {
        Ok(url) if url.scheme() == EXT_SCHEME => host_of(&url),
        _ => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const NOTES_PAGE: &str = "agentrq-ext://notes/panel/index.html";

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
    fn extension_of_reads_webview2s_form_only_on_webview2() {
        assert_eq!(extension_of("http://agentrq-ext.notes/x", true), "notes");
        assert_eq!(extension_of("http://agentrq-ext.notes/x", false), "");
    }
}
