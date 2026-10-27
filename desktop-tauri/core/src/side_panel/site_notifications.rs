// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! A side panel site's notifications, which WebKitGTK hands to the app's own
//! notifier: what they may say there, and how many a page may show.

use std::collections::{HashMap, VecDeque};
use std::time::{Duration, Instant};

use url::Url;

/// The most characters of a site's title and body the notifier is given.
pub const TITLE_CAP: usize = 128;
pub const BODY_CAP: usize = 512;

/// A guest may show `BURST` notifications in any `BURST_WINDOW`; past that
/// they are dropped without a word, as a flood should be.
pub const BURST: usize = 5;
pub const BURST_WINDOW: Duration = Duration::from_secs(10);

/// A site's notification as the notifier shows it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SiteText {
    pub title: String,
    pub body: String,
}

/// What the page at `url` asked to show, as the app's notifier may show it.
/// The title starts with the site's host, so a site never passes for the
/// app, whose name and icon the notification carries. Both are cut to their
/// cap. The body's `&`, `<` and `>` are escaped: a server that renders markup
/// (KDE, GNOME) renders it there, and only there (the title is plain text).
pub fn site_text(url: &str, title: &str, body: &str) -> SiteText {
    let title = cut(&plain(title, false), TITLE_CAP);
    let host = host_of(url);
    SiteText {
        title: if title.is_empty() {
            host
        } else {
            format!("{host} — {title}")
        },
        body: escape(&cut(&plain(body, true), BODY_CAP)),
    }
}

/// The host and port a person reads in an address bar.
fn host_of(url: &str) -> String {
    let Ok(url) = Url::parse(url) else {
        return String::new();
    };
    let host = url.host_str().unwrap_or("");
    match url.port() {
        Some(port) => format!("{host}:{port}"),
        None => host.to_string(),
    }
}

/// `text` with no control character but the body's line breaks, and no
/// bidirectional override: either could make the host read as something
/// else, or the title look like two notifications.
fn plain(text: &str, lines: bool) -> String {
    text.chars()
        .filter(|c| !matches!(c, '\u{200E}' | '\u{200F}' | '\u{202A}'..='\u{202E}' | '\u{2066}'..='\u{2069}'))
        .map(|c| match c {
            '\n' if lines => c,
            c if c.is_control() => ' ',
            c => c,
        })
        .collect()
}

/// At most `cap` characters, the last an ellipsis when it was cut.
fn cut(text: &str, cap: usize) -> String {
    if text.chars().count() <= cap {
        return text.to_string();
    }
    let mut kept: String = text.chars().take(cap - 1).collect();
    kept.push('…');
    kept
}

fn escape(text: &str) -> String {
    text.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
}

/// What the guests' pages have shown, for the limit. A tag needs nothing
/// here: WebKit closes the notification a newer one with its tag replaces,
/// and so the notifier's goes too.
#[derive(Debug, Default)]
pub struct SiteNotifications {
    recent: HashMap<String, VecDeque<Instant>>,
    last: i32,
}

impl SiteNotifications {
    /// The guest's page shows a notification at `now`: the notifier's id
    /// for it, which no other site notification has, or `None` past the
    /// limit.
    pub fn admit(&mut self, guest: &str, now: Instant) -> Option<i32> {
        let recent = self.recent.entry(guest.to_string()).or_default();
        while recent
            .front()
            .is_some_and(|at| now.duration_since(*at) >= BURST_WINDOW)
        {
            recent.pop_front();
        }
        if recent.len() >= BURST {
            return None;
        }
        recent.push_back(now);
        self.last = self.last.wrapping_add(1);
        Some(self.last)
    }

    /// The guest has gone, and so has its count.
    pub fn forget(&mut self, guest: &str) {
        self.recent.remove(guest);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_sites_title_starts_with_its_host() {
        let text = site_text(
            "https://evil.example/x",
            "Task assigned: approve deploy",
            "Go",
        );
        assert_eq!(text.title, "evil.example — Task assigned: approve deploy");
        assert_eq!(text.body, "Go");
        let port = site_text("http://127.0.0.1:8080/notify", "Build passed", "");
        assert_eq!(port.title, "127.0.0.1:8080 — Build passed");
        // A page with no title still names its site.
        assert_eq!(
            site_text("https://chat.example/", "", "hi").title,
            "chat.example"
        );
        // An address with no host names nothing, and is never allowed to show.
        assert_eq!(site_text("not a url", "x", "").title, " — x");
        assert_eq!(site_text("data:text/plain,x", "x", "").title, " — x");
    }

    #[test]
    fn its_markup_is_escaped_in_the_body_and_text_in_the_title() {
        let text = site_text(
            "https://chat.example/",
            "<b>x</b> & co",
            r#"<b>x</b><a href="https://example.com">y</a><img src="z"> & more"#,
        );
        assert_eq!(text.title, "chat.example — <b>x</b> & co");
        assert_eq!(
            text.body,
            r#"&lt;b&gt;x&lt;/b&gt;&lt;a href="https://example.com"&gt;y&lt;/a&gt;&lt;img src="z"&gt; &amp; more"#
        );
        assert_eq!(
            site_text("https://a.example/", "", "&amp;").body,
            "&amp;amp;"
        );
    }

    #[test]
    fn no_control_or_override_character_moves_the_host() {
        let text = site_text(
            "https://chat.example/",
            "line\nbreak\ttab\u{202E}evil\u{2066}\u{200F}",
            "one\ntwo\r\u{7}\u{202D}three",
        );
        assert_eq!(text.title, "chat.example — line break tabevil");
        assert_eq!(text.body, "one\ntwo  three");
    }

    #[test]
    fn its_title_and_body_are_cut_at_their_caps() {
        let at_cap = "t".repeat(TITLE_CAP);
        assert_eq!(
            site_text("https://a.example/", &at_cap, "").title,
            format!("a.example — {at_cap}")
        );
        // Cut at a character, never inside one.
        let long = "é".repeat(TITLE_CAP + 1);
        let title = site_text("https://a.example/", &long, "").title;
        let kept = title.strip_prefix("a.example — ").expect("the host");
        assert_eq!(kept.chars().count(), TITLE_CAP);
        assert_eq!(kept, format!("{}…", "é".repeat(TITLE_CAP - 1)));

        let body = site_text("https://a.example/", "", &"<".repeat(BODY_CAP + 7)).body;
        // Cut before it is escaped, so no entity is ever cut in half.
        assert_eq!(body, format!("{}…", "&lt;".repeat(BODY_CAP - 1)));
        let at_cap = "b".repeat(BODY_CAP);
        assert_eq!(site_text("https://a.example/", "", &at_cap).body, at_cap);
    }

    #[test]
    fn a_guest_shows_a_burst_then_nothing_until_the_window_passes() {
        let mut shown = SiteNotifications::default();
        let start = Instant::now();
        let ids: Vec<i32> = (0..BURST)
            .map(|_| shown.admit("panel-guest-1", start).expect("in the burst"))
            .collect();
        assert_eq!(ids, (1..=BURST as i32).collect::<Vec<_>>());
        assert_eq!(shown.admit("panel-guest-1", start), None);
        // Another guest has a count of its own.
        assert_eq!(shown.admit("panel-guest-2", start), Some(BURST as i32 + 1));
        let almost = start + BURST_WINDOW - Duration::from_millis(1);
        assert_eq!(shown.admit("panel-guest-1", almost), None);
        let later = start + BURST_WINDOW;
        assert_eq!(shown.admit("panel-guest-1", later), Some(BURST as i32 + 2));
        // A dropped one never counted: the rest of the window is open.
        for _ in 1..BURST {
            assert!(shown.admit("panel-guest-1", later).is_some());
        }
        assert_eq!(shown.admit("panel-guest-1", later), None);
    }

    #[test]
    fn a_guest_that_goes_takes_its_count_with_it() {
        let mut shown = SiteNotifications::default();
        let now = Instant::now();
        for _ in 0..BURST {
            shown.admit("panel-guest-1", now);
        }
        shown.forget("panel-guest-1");
        assert!(shown.admit("panel-guest-1", now).is_some());
    }

    #[test]
    fn its_ids_wrap_rather_than_overflow() {
        let mut shown = SiteNotifications {
            last: i32::MAX,
            ..Default::default()
        };
        assert_eq!(shown.admit("panel-guest-1", Instant::now()), Some(i32::MIN));
    }
}
