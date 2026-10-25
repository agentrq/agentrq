// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The document an extension's drawer runs in: `desktop/src/main/extensions/
//! drawer-frame.js`'s `drawerFrameDocument`, with the app's origin as an
//! argument. The JS names `app://agentrq`, Electron's origin, in the policy's
//! `script-src`; Tauri's is `app://localhost`, `app://<profile>` or
//! `http://app.localhost`, and a frame naming the wrong one cannot import its
//! drawer. Why the frame is served at all, and each line of its policy, are
//! explained there.
//!
//! The document is `drawer_frame.html`, which `test/drawer-frame.test.js`
//! holds equal to the JS's output, byte for byte.

/// The document, with `@@CSP@@`, `@@NONCE@@` and `@@FONT@@` to fill in.
const TEMPLATE: &str = include_str!("drawer_frame.html");

/// The font stack the JS defaults to.
pub const FONT: &str = "system-ui, -apple-system, sans-serif";

/// `drawerFrameCSP(nonce, origin)`.
pub fn drawer_frame_csp(nonce: &str, origin: &str) -> String {
    [
        "default-src 'none'".to_string(),
        format!("script-src 'nonce-{nonce}' {origin}"),
        "style-src 'unsafe-inline'".to_string(),
        "img-src data: blob:".to_string(),
        "font-src data:".to_string(),
        "connect-src 'none'".to_string(),
        "form-action 'none'".to_string(),
        "base-uri 'none'".to_string(),
    ]
    .join("; ")
}

/// The document and its policy, carrying the same `nonce`: generated apart,
/// they could disagree, which refuses the bootstrap.
pub fn drawer_frame_document(nonce: &str, origin: &str) -> (String, String) {
    let csp = drawer_frame_csp(nonce, origin);
    let html = TEMPLATE
        .replace("@@CSP@@", &csp)
        .replace("@@FONT@@", FONT)
        .replace("@@NONCE@@", nonce);
    (html, csp)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_the_origin_it_is_served_from() {
        assert_eq!(
            drawer_frame_csp("n0", "app://localhost"),
            "default-src 'none'; script-src 'nonce-n0' app://localhost; \
             style-src 'unsafe-inline'; img-src data: blob:; font-src data:; \
             connect-src 'none'; form-action 'none'; base-uri 'none'"
        );
    }

    #[test]
    fn the_header_and_the_document_carry_one_nonce() {
        let (html, csp) = drawer_frame_document("abc+/=", "http://app.localhost");
        assert!(html.contains(&format!("content=\"{csp}\"")));
        assert!(html.contains("<script nonce=\"abc+/=\">"));
        assert!(html.contains(&format!("font-family: {FONT};")));
        assert!(!html.contains("@@"));
        assert!(!html.contains("app://agentrq"));
    }
}
