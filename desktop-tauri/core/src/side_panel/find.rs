// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Find in a side panel guest: Electron's `findInPage` and `found-in-page`
//! for a native webview.
//!
//! WebKit (GTK and WKWebView) moves to a match but never says which one is
//! active, so the ordinal is kept here: a new search starts at the first
//! match, and a step wraps around at either end, as the webview's own
//! wrap-around does. WebKitGTK counts every match only for a new search (a
//! step's count is the one match it moved to), so a step keeps the search's
//! total. WebView2 reports both, from 0.

use std::collections::VecDeque;

use serde::Serialize;

/// What the find bar shows: the active match, from 1, of `total`. `0/0`
/// when nothing matched.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize)]
pub struct Found {
    pub active: u32,
    pub total: u32,
}

/// What one `findInPage` asks the webview to do.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FindStep {
    /// A new search, from the first match.
    Start,
    Next,
    Previous,
}

/// One guest's find session.
#[derive(Debug, Default)]
pub struct FindSession {
    text: String,
    active: u32,
    /// The new search's count.
    total: u32,
    /// The steps asked for and not yet counted, oldest first: the webview
    /// answers each in the order it was asked.
    asked: VecDeque<FindStep>,
}

impl FindSession {
    /// `findInPage(text, { forward, findNext })`. Electron's `findNext` is
    /// "begin a new session", so it, or a different text, is a new search.
    pub fn begin(&mut self, text: &str, forward: bool, find_next: bool) -> FindStep {
        let step = if find_next || text != self.text {
            FindStep::Start
        } else if forward {
            FindStep::Next
        } else {
            FindStep::Previous
        };
        self.text = text.to_string();
        self.asked.push_back(step);
        step
    }

    /// The webview counted `count` matches for the oldest step asked. A
    /// step that found something keeps the search's total; a count nobody
    /// asked for (the page changed under a search) is the new total, and
    /// keeps the active match where it is, within it.
    pub fn counted(&mut self, count: u32) -> Found {
        let step = self.asked.pop_front();
        let stepped = matches!(step, Some(FindStep::Next | FindStep::Previous));
        if !stepped || count == 0 || self.total == 0 {
            self.total = count;
        }
        let total = self.total;
        self.active = match (step, total) {
            (_, 0) => 0,
            (Some(FindStep::Start), _) => 1,
            (Some(FindStep::Next), _) => self.active % total + 1,
            (Some(FindStep::Previous), _) if self.active <= 1 => total,
            (Some(FindStep::Previous), _) => (self.active - 1).min(total),
            (None, _) => self.active.clamp(1, total),
        };
        Found {
            active: self.active,
            total,
        }
    }

    /// `stopFindInPage`: the next search starts afresh.
    pub fn stop(&mut self) {
        *self = Self::default();
    }
}

/// WebView2's `ActiveMatchIndex` (from 0, or -1 with none) and
/// `MatchCount`, as the find bar counts them.
pub fn webview2_found(index: i32, count: i32) -> Found {
    let total = u32::try_from(count).unwrap_or(0);
    let active = u32::try_from(index.saturating_add(1)).unwrap_or(0);
    Found {
        active: active.min(total),
        total,
    }
}

/// Whether `stopFindInPage(action)` leaves no selection behind.
pub fn clears_selection(action: &str) -> bool {
    action == "clearSelection"
}

/// The script that clears the page's selection.
pub const CLEAR_SELECTION_SCRIPT: &str = "window.getSelection()?.removeAllRanges()";

/// A script that counts the visible matches of `text`, ignoring case, as a
/// number: where the webview says only whether there is one.
pub fn count_script(text: &str) -> String {
    let needle = serde_json::to_string(&text.to_lowercase()).unwrap_or_default();
    format!(
        "(() => {{ const n = {needle}; const t = (document.body?.innerText ?? '').toLowerCase(); \
         let c = 0; for (let i = t.indexOf(n); n && i !== -1; i = t.indexOf(n, i + n.length)) c++; return c }})()"
    )
}

/// A script that moves to the next or previous match with `window.find`,
/// wrapping around, and answers with the count: for a webview with no find
/// of its own (WebView2 before `ICoreWebView2Find`).
pub fn find_script(text: &str, step: FindStep) -> String {
    let needle = serde_json::to_string(text).unwrap_or_default();
    let backwards = step == FindStep::Previous;
    let restart = if step == FindStep::Start {
        "window.getSelection()?.removeAllRanges(); "
    } else {
        ""
    };
    format!(
        "(() => {{ {restart}window.find({needle}, false, {backwards}, true); return {} }})()",
        count_script(text)
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn found(active: u32, total: u32) -> Found {
        Found { active, total }
    }

    #[test]
    fn a_new_search_starts_at_the_first_match() {
        let mut session = FindSession::default();
        assert_eq!(session.begin("cat", true, true), FindStep::Start);
        assert_eq!(session.counted(3), found(1, 3));
    }

    #[test]
    fn steps_wrap_around_at_either_end() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        session.counted(3);
        let mut next = || {
            assert_eq!(session.begin("cat", true, false), FindStep::Next);
            session.counted(3).active
        };
        assert_eq!([next(), next(), next()], [2, 3, 1]);
        assert_eq!(session.begin("cat", false, false), FindStep::Previous);
        assert_eq!(session.counted(3), found(3, 3));
        session.begin("cat", false, false);
        assert_eq!(session.counted(3), found(2, 3));
    }

    #[test]
    fn a_different_text_is_a_new_search_whatever_it_asks() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        session.counted(3);
        session.begin("cat", true, false);
        session.counted(3);
        assert_eq!(session.begin("dog", true, false), FindStep::Start);
        assert_eq!(session.counted(5), found(1, 5));
    }

    #[test]
    fn nothing_found_is_none_of_none_and_the_next_step_starts_over() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        assert_eq!(session.counted(0), found(0, 0));
        session.begin("cat", false, false);
        assert_eq!(session.counted(2), found(2, 2));
        session.begin("cat", true, false);
        session.counted(2);
        session.begin("cat", false, false);
        assert_eq!(session.counted(2), found(2, 2));
    }

    #[test]
    fn a_page_that_shrank_keeps_the_active_match_within_it() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        session.counted(5);
        for _ in 0..3 {
            session.begin("cat", true, false);
            session.counted(5);
        }
        assert_eq!(session.counted(2), found(2, 2), "nobody asked");
        session.begin("cat", false, false);
        assert_eq!(session.counted(1), found(1, 2));
        assert_eq!(session.counted(4), found(1, 4), "nobody asked");
    }

    #[test]
    fn a_step_keeps_the_searchs_total_whatever_it_counted() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        assert_eq!(session.counted(3), found(1, 3));
        session.begin("cat", true, false);
        assert_eq!(session.counted(1), found(2, 3), "WebKitGTK's count of one");
        session.begin("cat", false, false);
        assert_eq!(session.counted(1), found(1, 3));
        session.begin("cat", false, false);
        assert_eq!(session.counted(1), found(3, 3), "wrapped");
        session.begin("cat", true, false);
        assert_eq!(session.counted(0), found(0, 0), "the page lost them");
        session.begin("cat", true, false);
        assert_eq!(session.counted(2), found(1, 2), "and found them again");
    }

    #[test]
    fn answers_come_back_in_the_order_they_were_asked() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        session.begin("cat", true, false);
        session.begin("cat", true, false);
        assert_eq!(session.counted(4), found(1, 4));
        assert_eq!(session.counted(4), found(2, 4));
        assert_eq!(session.counted(4), found(3, 4));
    }

    #[test]
    fn stopping_forgets_the_session() {
        let mut session = FindSession::default();
        session.begin("cat", true, true);
        session.counted(3);
        session.begin("cat", true, false);
        session.stop();
        assert_eq!(session.begin("cat", true, false), FindStep::Start);
        assert_eq!(session.counted(3), found(1, 3));
    }

    #[test]
    fn webview2_counts_its_active_match_from_zero() {
        assert_eq!(webview2_found(0, 3), found(1, 3));
        assert_eq!(webview2_found(2, 3), found(3, 3));
        assert_eq!(webview2_found(-1, 0), found(0, 0));
        assert_eq!(webview2_found(-1, 4), found(0, 4));
        assert_eq!(webview2_found(7, 3), found(3, 3));
        assert_eq!(webview2_found(i32::MAX, -1), found(0, 0));
    }

    #[test]
    fn only_clear_selection_clears_it() {
        assert!(clears_selection("clearSelection"));
        for action in ["keepSelection", "activateSelection", ""] {
            assert!(!clears_selection(action), "{action}");
        }
    }

    #[test]
    fn the_scripts_quote_the_text_as_json() {
        let count = count_script("A\"b</script>");
        assert!(count.contains(r#"const n = "a\"b</script>";"#), "{count}");
        let start = find_script("x'y", FindStep::Start);
        assert!(start.starts_with("(() => { window.getSelection()?.removeAllRanges(); window.find(\"x'y\", false, false, true); return (() => {"), "{start}");
        let back = find_script("x", FindStep::Previous);
        assert!(
            back.starts_with("(() => { window.find(\"x\", false, true, true);"),
            "{back}"
        );
        let next = find_script("x", FindStep::Next);
        assert!(
            next.starts_with("(() => { window.find(\"x\", false, false, true);"),
            "{next}"
        );
    }

    #[test]
    fn found_crosses_as_the_find_bar_reads_it() {
        assert_eq!(
            serde_json::to_value(found(1, 2)).unwrap(),
            serde_json::json!({"active": 1, "total": 2})
        );
    }
}
