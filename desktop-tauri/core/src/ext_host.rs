// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! When the extension host runs, and what happens when it dies.
//!
//! Extensions are Node packages, so they run in a Node process of their own
//! (`agentrq-ext-host`) rather than in the shell. That process costs 30-40 MB
//! idle, so it runs only while an extension is installed and enabled, or for
//! a minute after the Extensions screen last asked for something. The calls a
//! page makes on every render never start it.
//!
//! A crash is restarted with a backoff and counted against the extensions that
//! were loaded, as `host.js` counts a throw: the third disables one.

use std::collections::HashMap;

use serde_json::{json, Value};

use crate::side_panel::bridge::extension_of;

/// Failures before an extension is turned off: `host.js`'s `FAILURE_LIMIT`.
pub const FAILURE_LIMIT: u32 = 3;

/// How long the host is kept after its last call when nothing is enabled.
pub const IDLE_STOP_MS: u64 = 60_000;

/// A host up this long has stopped crashing: the backoff starts again.
pub const STABLE_MS: u64 = 60_000;

const FIRST_RETRY_MS: u64 = 1_000;
const LONGEST_RETRY_MS: u64 = 30_000;

/// The file `install.js` keeps installations in, under the app's data dir.
pub const INSTALLED_FILE: &str = "extensions-installed.json";

/// The file `runtime.js` keeps the catalogue's last search in.
pub const CATALOGUE_FILE: &str = "extensions-catalogue.json";

/// The extensions that are installed and enabled, from [`INSTALLED_FILE`]'s
/// contents. `install.js` treats anything but `enabled: false` as on.
pub fn enabled_names(installed: &str) -> Vec<String> {
    let parsed: Value = serde_json::from_str(installed).unwrap_or(Value::Null);
    parsed["installations"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|installation| installation["enabled"] != json!(false))
        .filter_map(|installation| installation["name"].as_str().map(str::to_string))
        .collect()
}

/// The wait before the `attempt`th restart in a row (0 first): 1, 2, 4 … 30 s.
pub fn retry_delay_ms(attempt: u32) -> u64 {
    FIRST_RETRY_MS
        .saturating_mul(1u64 << attempt.min(16))
        .min(LONGEST_RETRY_MS)
}

/// Who a crash is held against: the extension being loaded when the host
/// died, if one was, or else every extension it had running.
pub fn blame(loading: Option<&str>, running: &[String]) -> Vec<String> {
    match loading {
        Some(name) => vec![name.to_string()],
        None => running.to_vec(),
    }
}

/// Crashes counted per extension, across restarts of the host: `host.js`
/// counts in the memory of a process that is gone after each one.
#[derive(Debug, Default)]
pub struct CrashLedger {
    counts: HashMap<String, u32>,
}

impl CrashLedger {
    /// Count one crash against each of `blamed`; returns those that reached
    /// [`FAILURE_LIMIT`] now, to be disabled.
    pub fn record(&mut self, blamed: &[String]) -> Vec<String> {
        let mut disabled = Vec::new();
        for name in blamed {
            let count = self.counts.entry(name.clone()).or_default();
            *count += 1;
            if *count == FAILURE_LIMIT {
                self.counts.remove(name);
                disabled.push(name.clone());
            }
        }
        disabled
    }

    /// Turned back on by the person: it starts again with a clean record, as
    /// `install.js` resets `failures`.
    pub fn forgive(&mut self, name: &str) {
        self.counts.remove(name);
    }

    pub fn count(&self, name: &str) -> u32 {
        self.counts.get(name).copied().unwrap_or(0)
    }
}

/// What a disabled extension's reason says, for the Extensions screen.
pub fn crash_reason(code: Option<i32>) -> String {
    match code {
        Some(code) => {
            format!("The extension host stopped (exit code {code}) three times while it ran.")
        }
        None => "The extension host was stopped three times while it ran.".to_string(),
    }
}

/// The answer to a call that must never start the host, given while it is not
/// running: nothing is enabled, so nothing contributes. `None` for a call that
/// may start it.
pub fn answer_without_host(method: &str, params: &Value) -> Option<Value> {
    match method {
        "entries" | "panelPages" | "panelPagesFor" => Some(json!([])),
        "hasDrawer" | "drawerFor" => {
            let wanted = params["format"].as_str().unwrap_or_default().to_lowercase();
            Some(if wanted.is_empty() {
                json!({ "ok": false, "reason": "No format was named." })
            } else {
                json!({ "ok": false, "reason": format!("Nothing installed draws \"{wanted}\".") })
            })
        }
        "invoke" => Some(json!({ "ok": false, "reason": "No extension is enabled." })),
        // A page of an Extension that is not running: `bridge.js`'s words.
        "panelSend" => {
            let owner = extension_of(params["senderUrl"].as_str().unwrap_or_default(), false);
            Some(json!({ "ok": false, "reason": format!("{owner} does not listen to its page.") }))
        }
        _ => None,
    }
}

/// The Extensions page's state from disk, for while the host cannot run
/// because Node is not there: the catalogue as last searched, and the
/// installations as `install.js` recorded them, none of them running. The
/// rows are `runtime.js`'s `state()`; what only the host knows (grants,
/// consents, contributions) is empty.
pub fn state_from_disk(installed: &str, catalogue: &str) -> Value {
    let mut index = json!({ "entries": [], "fetchedAt": null, "truncated": false });
    if let Ok(Value::Object(cached)) = serde_json::from_str::<Value>(catalogue) {
        if cached.get("entries").is_some_and(Value::is_array) {
            index.as_object_mut().expect("an object").extend(cached);
        }
    }
    let installed: Value = serde_json::from_str(installed).unwrap_or(Value::Null);
    let rows: Vec<Value> = installed["installations"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|installation| installation["name"].is_string())
        .map(installed_row)
        .collect();
    json!({ "index": index, "installed": rows })
}

fn installed_row(installation: &Value) -> Value {
    let manifest = &installation["manifest"];
    let or = |value: &Value, otherwise: Value| match value {
        Value::Null => otherwise,
        value => value.clone(),
    };
    json!({
        "name": installation["name"],
        "version": installation["version"],
        "displayName": or(&manifest["displayName"], installation["name"].clone()),
        "description": or(&manifest["description"], json!("")),
        "license": or(&manifest["license"], json!("")),
        "enabled": installation["enabled"] != json!(false),
        "loaded": false,
        "failures": or(&installation["failures"], json!(0)),
        "source": or(&installation["sourceLabel"], json!("")),
        "linked": or(
            &installation["linked"],
            json!(installation["source"]["kind"] == "local"),
        ),
        "grant": null,
        "consents": [],
        "contributes": {},
    })
}

/// Whether the host may fetch `url` through the profile's jar: only on the
/// profile's own server. The jar holds that server's session, and nothing the
/// host does needs it anywhere else.
pub fn fetch_allowed(url: &str, server_url: &str) -> bool {
    let (Ok(url), Ok(server)) = (url::Url::parse(url), url::Url::parse(server_url)) else {
        return false;
    };
    matches!(url.scheme(), "http" | "https") && url.origin() == server.origin()
}

/// Whether a stream event carries a tool call waiting on somebody, which is
/// all the host's `reviewToolCalls` acts on (Electron's `pendingRequests`):
/// a task message whose metadata is a pending `permission_request`. Others
/// are not sent, so they never take a review's place.
pub fn awaits_review(event: &Value) -> bool {
    event["payload"]["messages"]
        .as_array()
        .is_some_and(|messages| {
            messages.iter().any(|message| {
                let meta = &message["metadata"];
                meta["type"] == "permission_request" && meta["status"] == "pending"
            })
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_an_event_with_a_pending_tool_call_awaits_review() {
        let with = |meta: Value| json!({"type": "reply.received", "payload": {"id": "t", "workspaceId": "w", "messages": [{"metadata": {}}, {"metadata": meta}]}});
        assert!(awaits_review(&with(
            json!({"type": "permission_request", "status": "pending", "requestId": "r"})
        )));
        for meta in [
            json!({"type": "permission_request", "status": "allowed"}),
            json!({"type": "text", "status": "pending"}),
            json!(null),
        ] {
            assert!(!awaits_review(&with(meta.clone())), "{meta}");
        }
        assert!(!awaits_review(
            &json!({"type": "task.updated", "payload": {"messages": "x"}})
        ));
        assert!(!awaits_review(&json!(null)));
    }

    #[test]
    fn enabled_means_anything_but_false() {
        let installed = r#"{"installations":[
            {"name":"a","enabled":true},
            {"name":"b","enabled":false},
            {"name":"c"},
            {"enabled":true},
            {"name":"d","enabled":null}
        ]}"#;
        assert_eq!(enabled_names(installed), vec!["a", "c", "d"]);
    }

    #[test]
    fn a_missing_or_broken_file_enables_nothing() {
        for installed in ["", "{", "[]", "{}", r#"{"installations":{}}"#] {
            assert!(enabled_names(installed).is_empty(), "{installed}");
        }
    }

    #[test]
    fn reads_the_page_from_disk_while_the_host_cannot_run() {
        let installed = r#"{"installations":[
            {"name":"a","version":"1.0.0","enabled":false,"failures":2,"sourceLabel":"me/a",
             "linked":false,"source":{"kind":"local"},
             "manifest":{"displayName":"A","description":"Does a.","license":"MIT"}},
            {"name":"b","source":{"kind":"local"}},
            {"name":"c","source":{"kind":"github"}},
            {"version":"9"}
        ]}"#;
        let catalogue = r#"{"entries":[{"ok":true,"fullName":"x/y"}],"fetchedAt":5}"#;
        assert_eq!(
            state_from_disk(installed, catalogue),
            json!({
                "index": {"entries": [{"ok": true, "fullName": "x/y"}], "fetchedAt": 5, "truncated": false},
                "installed": [
                    {"name": "a", "version": "1.0.0", "displayName": "A", "description": "Does a.",
                     "license": "MIT", "enabled": false, "loaded": false, "failures": 2, "source": "me/a",
                     "linked": false, "grant": null, "consents": [], "contributes": {}},
                    {"name": "b", "version": null, "displayName": "b", "description": "",
                     "license": "", "enabled": true, "loaded": false, "failures": 0, "source": "",
                     "linked": true, "grant": null, "consents": [], "contributes": {}},
                    {"name": "c", "version": null, "displayName": "c", "description": "",
                     "license": "", "enabled": true, "loaded": false, "failures": 0, "source": "",
                     "linked": false, "grant": null, "consents": [], "contributes": {}},
                ],
            })
        );
        let empty = json!({
            "index": {"entries": [], "fetchedAt": null, "truncated": false},
            "installed": [],
        });
        for broken in ["", "{", "[]", "{}", r#"{"entries":{}}"#] {
            assert_eq!(state_from_disk(broken, broken), empty, "{broken}");
        }
    }

    #[test]
    fn retries_double_up_to_half_a_minute() {
        let delays: Vec<_> = (0..7).map(retry_delay_ms).collect();
        assert_eq!(
            delays,
            vec![1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]
        );
        assert_eq!(retry_delay_ms(u32::MAX), 30_000);
    }

    #[test]
    fn a_crash_while_loading_is_that_extensions() {
        let running = vec!["a".to_string(), "b".to_string()];
        assert_eq!(blame(Some("c"), &running), vec!["c"]);
        assert_eq!(blame(None, &running), running);
    }

    #[test]
    fn the_third_crash_disables() {
        let mut ledger = CrashLedger::default();
        let both = vec!["a".to_string(), "b".to_string()];
        assert!(ledger.record(&both).is_empty());
        assert!(ledger.record(&["a".to_string()]).is_empty());
        assert_eq!(ledger.count("a"), 2);
        assert_eq!(ledger.record(&both), vec!["a"]);
        assert_eq!(
            ledger.count("a"),
            0,
            "a disabled extension starts again clean"
        );
        assert_eq!(ledger.count("b"), 2);
        ledger.forgive("b");
        assert_eq!(ledger.count("b"), 0);
    }

    #[test]
    fn says_why_it_was_disabled() {
        assert_eq!(
            crash_reason(Some(1)),
            "The extension host stopped (exit code 1) three times while it ran."
        );
        assert_eq!(
            crash_reason(None),
            "The extension host was stopped three times while it ran."
        );
    }

    #[test]
    fn a_page_s_calls_are_answered_without_the_host() {
        let none = json!({});
        assert_eq!(answer_without_host("entries", &none), Some(json!([])));
        assert_eq!(answer_without_host("panelPages", &none), Some(json!([])));
        assert_eq!(answer_without_host("panelPagesFor", &none), Some(json!([])));
        assert_eq!(
            answer_without_host("hasDrawer", &json!({"format": "Mermaid"})),
            Some(json!({"ok": false, "reason": "Nothing installed draws \"mermaid\"."}))
        );
        assert_eq!(
            answer_without_host("drawerFor", &none),
            Some(json!({"ok": false, "reason": "No format was named."}))
        );
        assert_eq!(
            answer_without_host("invoke", &none),
            Some(json!({"ok": false, "reason": "No extension is enabled."}))
        );
        assert_eq!(answer_without_host("state", &none), None);
        assert_eq!(
            answer_without_host("panelSend", &json!({"senderUrl": "agentrq-ext://notes/"})),
            Some(json!({"ok": false, "reason": "notes does not listen to its page."}))
        );
    }

    #[test]
    fn fetches_only_on_the_profile_s_server() {
        let server = "https://agentrq.example.com";
        assert!(fetch_allowed(
            "https://agentrq.example.com/api/v1/x",
            server
        ));
        assert!(fetch_allowed("https://AGENTRQ.example.com:443/mcp", server));
        assert!(fetch_allowed(
            "http://127.0.0.1:3000/mcp",
            "http://127.0.0.1:3000/"
        ));
        for url in [
            "https://elsewhere.example.com/api",
            "http://agentrq.example.com/api",
            "https://agentrq.example.com:8443/api",
            "not a url",
        ] {
            assert!(!fetch_allowed(url, server), "{url}");
        }
        assert!(!fetch_allowed("https://agentrq.example.com/x", ""));
        assert!(!fetch_allowed("file:///etc/passwd", "file:///etc"));
    }
}
