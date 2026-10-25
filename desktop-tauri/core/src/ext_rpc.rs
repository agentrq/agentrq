// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The wire between the shell and the extension host (`agentrq-ext-host`):
//! JSON-RPC 2.0, one message per line, over the host's stdin and stdout.
//!
//! Both ends ask and answer. The shell asks for what Electron's main process
//! did with its extension runtime (`state`, `install*`, `drawerFor`, …); the
//! host asks for what only the shell has: a fetch through the profile's jar,
//! and the keychain. Nothing here waits: the shell races every answer against
//! [`deadline_ms`], and the host races its own (`src/ext-host/rpc.js`).

use serde_json::{json, Value};

/// The longest line either side accepts. A drawer's code (16 MiB at most) is
/// the largest thing sent, and JSON escaping can double it.
pub const MAX_LINE_BYTES: usize = 40 * 1024 * 1024;

/// The code an answer carries when the method is not one this side serves.
pub const METHOD_NOT_FOUND: i64 = -32601;
/// The code for every other failure. Its message is meant for a person.
pub const FAILED: i64 = -32000;

/// One message, as read off the wire.
#[derive(Debug, Clone, PartialEq)]
pub enum Message {
    /// Asked of this side; answer it with the same id.
    Request {
        id: u64,
        method: String,
        params: Value,
    },
    /// Told to this side; there is nothing to answer.
    Notification { method: String, params: Value },
    /// The answer to something this side asked.
    Response {
        id: u64,
        result: Result<Value, String>,
    },
    /// A line that is not one of ours, kept for the log: a module that wrote to
    /// stdout around the host's guard, or a crash's last words.
    Stray(String),
}

/// Reads one line (without its newline).
pub fn parse(line: &[u8]) -> Message {
    let stray = || Message::Stray(String::from_utf8_lossy(line).trim_end().to_string());
    let Ok(Value::Object(message)) = serde_json::from_slice::<Value>(line) else {
        return stray();
    };
    if message.get("jsonrpc") != Some(&json!("2.0")) {
        return stray();
    }
    let id = message.get("id").and_then(Value::as_u64);
    let params = message.get("params").cloned().unwrap_or(Value::Null);
    match (id, message.get("method").and_then(Value::as_str)) {
        (Some(id), Some(method)) => Message::Request {
            id,
            method: method.to_string(),
            params,
        },
        (None, Some(method)) => Message::Notification {
            method: method.to_string(),
            params,
        },
        (Some(id), None) => Message::Response {
            id,
            result: match message.get("error") {
                Some(error) => Err(error_text(error)),
                None => Ok(message.get("result").cloned().unwrap_or(Value::Null)),
            },
        },
        (None, None) => stray(),
    }
}

fn error_text(error: &Value) -> String {
    error
        .get("message")
        .and_then(Value::as_str)
        .filter(|text| !text.is_empty())
        .unwrap_or("The extension host failed without saying why.")
        .to_string()
}

fn line(message: Value) -> Vec<u8> {
    let mut bytes = serde_json::to_vec(&message).expect("a Value always serialises");
    bytes.push(b'\n');
    bytes
}

pub fn request(id: u64, method: &str, params: Value) -> Vec<u8> {
    line(json!({ "jsonrpc": "2.0", "id": id, "method": method, "params": params }))
}

pub fn notification(method: &str, params: Value) -> Vec<u8> {
    line(json!({ "jsonrpc": "2.0", "method": method, "params": params }))
}

pub fn response(id: u64, result: Result<Value, String>) -> Vec<u8> {
    match result {
        Ok(result) => line(json!({ "jsonrpc": "2.0", "id": id, "result": result })),
        Err(message) => error_response(id, FAILED, &message),
    }
}

pub fn error_response(id: u64, code: i64, message: &str) -> Vec<u8> {
    line(json!({
        "jsonrpc": "2.0",
        "id": id,
        "error": { "code": code, "message": message },
    }))
}

/// Ids for this side's requests: never zero, never reused while the process
/// lives. The two sides number their own requests, so the same id going each
/// way is two different calls.
#[derive(Debug, Default)]
pub struct Ids(u64);

impl Ids {
    pub fn allocate(&mut self) -> u64 {
        self.0 += 1;
        self.0
    }
}

/// Splits a byte stream into lines, refusing one longer than
/// [`MAX_LINE_BYTES`] rather than holding it.
#[derive(Debug)]
pub struct Lines {
    held: Vec<u8>,
    /// The rest of an over-long line is skipped up to its newline.
    skipping: bool,
    max: usize,
}

/// What [`Lines::push`] found.
#[derive(Debug, PartialEq, Eq)]
pub enum Line {
    Whole(Vec<u8>),
    /// A line too long to keep: it was dropped.
    TooLong,
}

impl Default for Lines {
    fn default() -> Self {
        Self::with_max(MAX_LINE_BYTES)
    }
}

impl Lines {
    pub fn with_max(max: usize) -> Self {
        Self {
            held: Vec::new(),
            skipping: false,
            max,
        }
    }

    /// Takes the next chunk read, and returns every line it completed.
    pub fn push(&mut self, chunk: &[u8]) -> Vec<Line> {
        let mut found = Vec::new();
        let mut rest = chunk;
        while let Some(end) = rest.iter().position(|byte| *byte == b'\n') {
            let (head, tail) = (&rest[..end], &rest[end + 1..]);
            rest = tail;
            if std::mem::take(&mut self.skipping) {
                continue;
            }
            if self.held.len() + head.len() > self.max {
                self.held.clear();
                found.push(Line::TooLong);
                continue;
            }
            let mut whole = std::mem::take(&mut self.held);
            whole.extend_from_slice(head);
            if !whole.iter().all(u8::is_ascii_whitespace) {
                found.push(Line::Whole(whole));
            }
        }
        if self.skipping {
            return found;
        }
        if self.held.len() + rest.len() > self.max {
            self.held.clear();
            self.skipping = true;
            found.push(Line::TooLong);
        } else {
            self.held.extend_from_slice(rest);
        }
        found
    }
}

/// How long the shell waits for the host to answer `method`, in ms. Installs
/// download and copy; authorising waits for a person; everything else is a
/// lookup in memory or a call to the server.
pub fn deadline_ms(method: &str) -> u64 {
    match method {
        "authorize" => 10 * 60_000,
        "installFromCatalogue" | "refresh" => 5 * 60_000,
        "installLocal" | "inspect" | "configure" | "setEnabled" | "uninstall" | "startAll" => {
            2 * 60_000
        }
        "entries" | "panelPages" | "supervisor" => 5_000,
        _ => 30_000,
    }
}

/// What the shell says when the host did not answer in time.
pub fn timed_out(method: &str) -> String {
    format!("The extension host did not answer \"{method}\" in time.")
}

/// What the shell says when the host is gone while an answer was owed.
pub const HOST_GONE: &str = "The extension host stopped before it answered.";

#[cfg(test)]
mod tests {
    use super::*;

    fn text(bytes: Vec<u8>) -> String {
        String::from_utf8(bytes).unwrap()
    }

    #[test]
    fn writes_one_message_per_line() {
        assert_eq!(
            text(request(7, "state", json!({}))),
            "{\"id\":7,\"jsonrpc\":\"2.0\",\"method\":\"state\",\"params\":{}}\n"
        );
        assert_eq!(
            text(notification("loading", json!({"name": "a"}))),
            "{\"jsonrpc\":\"2.0\",\"method\":\"loading\",\"params\":{\"name\":\"a\"}}\n"
        );
        assert_eq!(
            text(response(3, Ok(json!([1])))),
            "{\"id\":3,\"jsonrpc\":\"2.0\",\"result\":[1]}\n"
        );
        assert_eq!(
            text(response(4, Err("no".into()))),
            "{\"error\":{\"code\":-32000,\"message\":\"no\"},\"id\":4,\"jsonrpc\":\"2.0\"}\n"
        );
        assert_eq!(
            text(error_response(5, METHOD_NOT_FOUND, "x")),
            "{\"error\":{\"code\":-32601,\"message\":\"x\"},\"id\":5,\"jsonrpc\":\"2.0\"}\n"
        );
    }

    #[test]
    fn a_newline_in_a_value_never_ends_the_line() {
        let bytes = request(1, "configure", json!({"values": {"note": "a\nb"}}));
        assert_eq!(bytes.iter().filter(|b| **b == b'\n').count(), 1);
        assert_eq!(
            parse(&bytes[..bytes.len() - 1]),
            Message::Request {
                id: 1,
                method: "configure".into(),
                params: json!({"values": {"note": "a\nb"}})
            }
        );
    }

    #[test]
    fn reads_each_kind_back() {
        assert_eq!(
            parse(br#"{"jsonrpc":"2.0","id":2,"method":"profileFetch","params":{"url":"u"}}"#),
            Message::Request {
                id: 2,
                method: "profileFetch".into(),
                params: json!({"url": "u"})
            }
        );
        assert_eq!(
            parse(br#"{"jsonrpc":"2.0","method":"ready"}"#),
            Message::Notification {
                method: "ready".into(),
                params: Value::Null
            }
        );
        assert_eq!(
            parse(br#"{"jsonrpc":"2.0","id":9,"result":{"ok":true}}"#),
            Message::Response {
                id: 9,
                result: Ok(json!({"ok": true}))
            }
        );
        assert_eq!(
            parse(br#"{"jsonrpc":"2.0","id":9}"#),
            Message::Response {
                id: 9,
                result: Ok(Value::Null)
            }
        );
        assert_eq!(
            parse(br#"{"jsonrpc":"2.0","id":9,"error":{"code":-32000,"message":"refused"}}"#),
            Message::Response {
                id: 9,
                result: Err("refused".into())
            }
        );
    }

    #[test]
    fn an_error_without_words_still_says_something() {
        for error in [r#"{}"#, r#"{"message":""}"#, r#""x""#] {
            let line = format!(r#"{{"jsonrpc":"2.0","id":1,"error":{error}}}"#);
            assert_eq!(
                parse(line.as_bytes()),
                Message::Response {
                    id: 1,
                    result: Err("The extension host failed without saying why.".into())
                }
            );
        }
    }

    #[test]
    fn anything_else_is_stray() {
        for line in [
            &b"hello from an extension\r"[..],
            b"[1,2]",
            br#"{"id":1,"result":1}"#,
            br#"{"jsonrpc":"1.0","id":1,"result":1}"#,
            br#"{"jsonrpc":"2.0"}"#,
            br#"{"jsonrpc":"2.0","id":-1,"result":1}"#,
            b"\xff\xfe",
        ] {
            assert!(matches!(parse(line), Message::Stray(_)), "{line:?}");
        }
        assert_eq!(
            parse(b"hello\r"),
            Message::Stray("hello".into()),
            "trimmed for the log"
        );
    }

    #[test]
    fn ids_start_at_one_and_climb() {
        let mut ids = Ids::default();
        assert_eq!((ids.allocate(), ids.allocate(), ids.allocate()), (1, 2, 3));
    }

    #[test]
    fn lines_survive_any_chunking() {
        let mut lines = Lines::default();
        assert_eq!(lines.push(b"ab"), vec![]);
        assert_eq!(
            lines.push(b"c\nd\n\n  \ne"),
            vec![Line::Whole(b"abc".to_vec()), Line::Whole(b"d".to_vec())]
        );
        assert_eq!(lines.push(b"f\n"), vec![Line::Whole(b"ef".to_vec())]);
    }

    #[test]
    fn a_line_too_long_is_dropped_and_the_next_one_read() {
        let mut lines = Lines::with_max(4);
        // Too long within one chunk.
        assert_eq!(
            lines.push(b"12345\nok\n"),
            vec![Line::TooLong, Line::Whole(b"ok".to_vec())]
        );
        // Too long across chunks: refused as soon as it is, the rest skipped.
        assert_eq!(lines.push(b"123"), vec![]);
        assert_eq!(lines.push(b"45"), vec![Line::TooLong]);
        assert_eq!(lines.push(b"6789"), vec![]);
        assert_eq!(
            lines.push(b"0\nnext\n"),
            vec![Line::Whole(b"next".to_vec())]
        );
        // Exactly the limit is kept.
        assert_eq!(lines.push(b"1234\n"), vec![Line::Whole(b"1234".to_vec())]);
        assert_eq!(Lines::default().max, MAX_LINE_BYTES);
    }

    #[test]
    fn every_method_has_a_deadline() {
        assert_eq!(deadline_ms("authorize"), 600_000);
        assert_eq!(deadline_ms("installFromCatalogue"), 300_000);
        assert_eq!(deadline_ms("refresh"), 300_000);
        for method in [
            "installLocal",
            "inspect",
            "configure",
            "setEnabled",
            "uninstall",
            "startAll",
        ] {
            assert_eq!(deadline_ms(method), 120_000, "{method}");
        }
        for method in ["entries", "panelPages", "supervisor"] {
            assert_eq!(deadline_ms(method), 5_000, "{method}");
        }
        assert_eq!(deadline_ms("state"), 30_000);
        assert_eq!(
            timed_out("state"),
            "The extension host did not answer \"state\" in time."
        );
    }
}
