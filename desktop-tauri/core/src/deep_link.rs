// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Deep links: `agentrq://workspaces/<id>/tasks/<id>` becomes the in-app route
//! `/workspaces/<id>/tasks/<id>`.
//!
//! A deep link is untrusted input from outside the app, so the result is
//! checked against the routes that actually exist. Anything unrecognised is
//! `None`, and the caller opens the app at its default view.
//!
//! The scheme is a parameter: the Tauri build uses `agentrq-tauri` while it is
//! in preview, so it never competes with the Electron app for `agentrq://`.

use crate::uri::decode_uri_component;
use url::Url;

/// The scheme the Electron app registers.
pub const DEEP_LINK_SCHEME: &str = "agentrq";

/// The scheme the Tauri build registers during the preview.
pub const DEEP_LINK_SCHEME_TAURI: &str = "agentrq-tauri";

/// Top-level sections a link may address. These mirror the route table in
/// `frontend/src/app.js`; a link outside them has no destination.
pub const LINKABLE_SECTIONS: [&str; 4] = ["workspaces", "tasks", "events", "workflows"];

fn is_identifier(segment: &str) -> bool {
    !segment.is_empty()
        && segment
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// Turn a deep-link URL into an in-app route, or `None` when the link is not
/// one we serve. `scheme` is given without the colon.
pub fn parse_deep_link(url: &str, scheme: &str) -> Option<String> {
    let parsed = Url::parse(url).ok()?;
    if parsed.scheme() != scheme {
        return None;
    }

    // `agentrq://workspaces/abc` parses with host `workspaces` and path `/abc`,
    // while `agentrq:///workspaces/abc` puts it all in the path. Both turn up in
    // the wild, so both are accepted.
    // JS builds `raw` from `host`, which keeps `:port`, so a link with a port
    // never matches a section.
    if parsed.port().is_some() {
        return None;
    }
    let raw = format!("{}{}", parsed.host_str().unwrap_or(""), parsed.path());
    let segments: Vec<String> = raw
        .split('/')
        .filter(|segment| !segment.is_empty())
        .map(|segment| decode_uri_component(segment).unwrap_or_else(|| segment.to_string()))
        .collect();

    let Some(section) = segments.first() else {
        return Some("/".to_string());
    };
    if !LINKABLE_SECTIONS.contains(&section.as_str()) {
        return None;
    }

    // Every remaining segment must be a plain identifier. This is external
    // input, and a segment carrying a slash, a traversal or a control character
    // has no business reaching the router.
    if !segments[1..].iter().all(|segment| is_identifier(segment)) {
        return None;
    }

    let search = match parsed.query() {
        Some(query) if !query.is_empty() => format!("?{query}"),
        _ => String::new(),
    };
    Some(format!("/{}{search}", segments.join("/")))
}

/// Find the deep link in a set of process arguments. Windows and Linux deliver
/// the link as an argv entry on the second launch, rather than through the
/// `open-url` event macOS uses.
pub fn deep_link_from_argv<S: AsRef<str>>(argv: &[S], scheme: &str) -> Option<String> {
    let prefix = format!("{scheme}://");
    argv.iter()
        .map(AsRef::as_ref)
        .find(|arg| arg.starts_with(&prefix))
        .map(str::to_string)
}

/// Tracks whether the renderer has loaded and subscribed, and holds any route
/// that arrives before that point so it can be delivered once ready.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct PendingRoute {
    route: Option<String>,
    page_loaded: bool,
    subscribed: bool,
}

impl PendingRoute {
    pub fn new() -> Self {
        Self::default()
    }

    /// When a navigate request arrives:
    /// Returns Some(route) if both page_loaded and subscribed, else buffers it and returns None.
    pub fn navigate(&mut self, route: &str) -> Option<String> {
        if route.is_empty() {
            return None;
        }
        if self.page_loaded && self.subscribed {
            Some(route.to_string())
        } else {
            self.route = Some(route.to_string());
            None
        }
    }

    /// When page load starts (e.g. navigation or reload).
    pub fn on_page_load_started(&mut self) {
        self.page_loaded = false;
        self.subscribed = false;
    }

    /// When page load finishes:
    /// Returns Some(route) to deliver if there is a pending route and the renderer has subscribed.
    /// Route is kept buffered until subscribed if not subscribed yet.
    pub fn on_page_load_finished(&mut self) -> Option<String> {
        self.page_loaded = true;
        if self.subscribed {
            self.route.take()
        } else {
            None
        }
    }

    /// When the renderer subscribes (ready signal):
    /// Returns Some(route) if there was a pending route waiting to be taken and page load has finished.
    pub fn on_subscribed(&mut self) -> Option<String> {
        self.subscribed = true;
        if self.page_loaded {
            self.route.take()
        } else {
            None
        }
    }

    pub fn is_ready(&self) -> bool {
        self.page_loaded && self.subscribed
    }

    pub fn pending(&self) -> Option<&str> {
        self.route.as_deref()
    }

    pub fn set_pending(&mut self, route: &str) {
        self.route = Some(route.to_string());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(url: &str) -> Option<String> {
        parse_deep_link(url, DEEP_LINK_SCHEME)
    }

    #[test]
    fn rejects_a_link_whose_host_carries_a_port() {
        assert_eq!(parse("agentrq://workspaces:8080/abc"), None);
    }

    #[test]
    fn maps_a_workspace_link_onto_its_route() {
        assert_eq!(
            parse("agentrq://workspaces/0ZzhYQG2qtl").as_deref(),
            Some("/workspaces/0ZzhYQG2qtl")
        );
    }

    #[test]
    fn maps_a_task_link_onto_its_route() {
        assert_eq!(
            parse("agentrq://workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN").as_deref(),
            Some("/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN")
        );
    }

    #[test]
    fn accepts_every_section_the_route_table_exposes() {
        for section in LINKABLE_SECTIONS {
            assert_eq!(
                parse(&format!("agentrq://{section}/abc")),
                Some(format!("/{section}/abc"))
            );
        }
    }

    #[test]
    fn accepts_the_triple_slash_form_a_link_can_also_take() {
        assert_eq!(
            parse("agentrq:///workspaces/abc").as_deref(),
            Some("/workspaces/abc")
        );
    }

    #[test]
    fn opens_the_default_view_for_a_bare_link() {
        assert_eq!(parse("agentrq://").as_deref(), Some("/"));
        assert_eq!(parse("agentrq:///").as_deref(), Some("/"));
    }

    #[test]
    fn keeps_a_query_string() {
        assert_eq!(
            parse("agentrq://tasks/all?filter=mine").as_deref(),
            Some("/tasks/all?filter=mine")
        );
    }

    #[test]
    fn decodes_percent_encoded_segments() {
        assert_eq!(
            parse("agentrq://workspaces/abc%2Ddef").as_deref(),
            Some("/workspaces/abc-def")
        );
    }

    #[test]
    fn refuses_another_scheme() {
        assert_eq!(parse("https://evil.example.com/workspaces/abc"), None);
        assert_eq!(parse("file:///etc/passwd"), None);
    }

    #[test]
    fn refuses_a_section_that_is_not_a_route() {
        assert_eq!(parse("agentrq://admin/abc"), None);
        assert_eq!(parse("agentrq://api/v1/auth/user"), None);
    }

    #[test]
    fn refuses_a_segment_that_is_not_a_plain_identifier() {
        assert_eq!(parse("agentrq://workspaces/..%2F..%2Fetc"), None);
        assert_eq!(parse("agentrq://workspaces/a%00b"), None);
        assert_eq!(parse("agentrq://workspaces/a b"), None);
    }

    #[test]
    fn refuses_anything_unparseable() {
        // null and undefined collapse to the empty string, as in JS.
        assert_eq!(parse("not a url"), None);
        assert_eq!(parse(""), None);
    }

    #[test]
    fn tolerates_a_malformed_escape_rather_than_throwing() {
        assert_eq!(parse("agentrq://workspaces/%zz"), None);
    }

    #[test]
    fn names_its_scheme() {
        assert_eq!(DEEP_LINK_SCHEME, "agentrq");
    }

    #[test]
    fn takes_the_scheme_as_a_parameter() {
        // Not in the vitest file: the Tauri build's preview scheme.
        assert_eq!(DEEP_LINK_SCHEME_TAURI, "agentrq-tauri");
        assert_eq!(
            parse_deep_link("agentrq-tauri://workspaces/abc", DEEP_LINK_SCHEME_TAURI).as_deref(),
            Some("/workspaces/abc")
        );
        assert_eq!(
            parse_deep_link("agentrq://workspaces/abc", DEEP_LINK_SCHEME_TAURI),
            None
        );
        assert_eq!(parse("agentrq-tauri://workspaces/abc"), None);
    }

    #[test]
    fn finds_the_link_windows_and_linux_pass_as_an_argument() {
        let argv = ["/path/to/AgentRQ", "--flag", "agentrq://workspaces/abc"];
        assert_eq!(
            deep_link_from_argv(&argv, DEEP_LINK_SCHEME).as_deref(),
            Some("agentrq://workspaces/abc")
        );
    }

    #[test]
    fn returns_nothing_when_there_is_no_link() {
        assert_eq!(
            deep_link_from_argv(&["/path/to/AgentRQ", "--flag"], DEEP_LINK_SCHEME),
            None
        );
        assert_eq!(deep_link_from_argv::<&str>(&[], DEEP_LINK_SCHEME), None);
    }

    #[test]
    fn ignores_non_string_arguments() {
        // JS skips null and numbers; argv here is already strings, so the
        // stand-ins are arguments that are not links.
        let argv = ["", "42", "agentrq://events"];
        assert_eq!(
            deep_link_from_argv(&argv, DEEP_LINK_SCHEME).as_deref(),
            Some("agentrq://events")
        );
    }

    #[test]
    fn pending_route_holds_routes_until_page_ready() {
        let mut pr = PendingRoute::new();
        assert!(!pr.is_ready());
        assert_eq!(pr.pending(), None);

        // Empty route does nothing
        assert_eq!(pr.navigate(""), None);

        // Pre-load navigate holds the route
        assert_eq!(pr.navigate("/tasks/1"), None);
        assert_eq!(pr.pending(), Some("/tasks/1"));

        // Setting pending directly
        pr.set_pending("/workspaces/2");
        assert_eq!(pr.pending(), Some("/workspaces/2"));

        // Page load finished does not deliver until renderer is subscribed
        assert_eq!(pr.on_page_load_finished(), None);
        // Route is retained until subscribed
        assert_eq!(pr.pending(), Some("/workspaces/2"));

        // Subscribing takes the route and makes it ready
        assert_eq!(pr.on_subscribed(), Some("/workspaces/2".to_string()));
        assert_eq!(pr.pending(), None);
        assert!(pr.is_ready());

        // Finished or subscribed called again does not deliver again
        assert_eq!(pr.on_page_load_finished(), None);
        assert_eq!(pr.on_subscribed(), None);

        // Subsequent navigation immediately returns route
        assert_eq!(pr.navigate("/tasks/3"), Some("/tasks/3".to_string()));

        // Page reload resets ready state
        pr.on_page_load_started();
        assert!(!pr.is_ready());
        assert_eq!(pr.navigate("/tasks/4"), None);
        assert_eq!(pr.pending(), Some("/tasks/4"));
    }

    #[test]
    fn delivers_cold_start_route_once_when_subscribed_before_page_finished() {
        let mut pr = PendingRoute::new();
        pr.set_pending("/workspaces/cold");

        // Subscribed before page finished does not deliver yet
        assert_eq!(pr.on_subscribed(), None);
        assert_eq!(pr.pending(), Some("/workspaces/cold"));

        // Page finished completes readiness and delivers exactly once
        assert_eq!(
            pr.on_page_load_finished(),
            Some("/workspaces/cold".to_string())
        );
        assert_eq!(pr.pending(), None);
        assert!(pr.is_ready());

        // Repeated calls deliver nothing
        assert_eq!(pr.on_page_load_finished(), None);
        assert_eq!(pr.on_subscribed(), None);
    }
}
