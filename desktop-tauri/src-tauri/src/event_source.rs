// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The Rust half of the bridge's `EventSource`.
//!
//! The scheme buffers every response (WP0 S1), so an event stream cannot go
//! through it: it would never answer. The bridge's `EventSource` therefore
//! asks Rust to read the stream through the profile's client, and each event
//! comes back over an IPC channel (design A). `useEventBus.js` runs on it
//! unchanged.
//!
//! The browser's semantics, from the HTML spec's "server-sent events": the
//! line parser (CR, LF or CRLF; `event`, `data`, `id`, `retry`; comments), a
//! failed connection (not 200, or not `text/event-stream`) that is final, and a
//! dropped one that reconnects after the `retry` delay with `Last-Event-ID`.
//! Every wait has a deadline (docs/agents/desktop.md): the headers get the
//! proxy's budget, and a stream silent past `core::sse::IDLE_TIMEOUT`, which
//! outlasts the server's keepalive, counts as dropped.

use std::collections::HashMap;
use std::future::Future;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Mutex;
use std::time::Duration;

use agentrq_desktop_core::protocol::PROXY_TIMEOUT_MS;
use agentrq_desktop_core::sse::{Utf8Decoder, ACCEPT, IDLE_TIMEOUT};
use futures_util::StreamExt;
use serde::Serialize;
use tokio::task::AbortHandle;

/// The reconnection delay before a server sets one, as Chromium's.
pub const DEFAULT_RETRY_MS: u64 = 3000;

/// What the page's `EventSource` is told. `readyState` on an error is what the
/// source becomes: 0 while it reconnects, 2 when it has given up.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum Frame {
    Open,
    #[serde(rename_all = "camelCase")]
    Message {
        event: String,
        data: String,
        last_event_id: String,
    },
    #[serde(rename_all = "camelCase")]
    Error {
        ready_state: u8,
    },
}

/// The event stream's line parser, fed text in arbitrary pieces.
#[derive(Debug, Default)]
pub struct Parser {
    line: String,
    after_cr: bool,
    event: String,
    data: String,
    last_event_id: String,
    retry: Option<u64>,
}

impl Parser {
    /// The events this text completes.
    pub fn feed(&mut self, text: &str) -> Vec<Frame> {
        let mut out = Vec::new();
        for c in text.chars() {
            // A CRLF split between two pieces is still one line break.
            let lf_after_cr = self.after_cr && c == '\n';
            self.after_cr = c == '\r';
            if lf_after_cr {
                continue;
            }
            if c == '\r' || c == '\n' {
                let line = std::mem::take(&mut self.line);
                out.extend(self.process(&line));
            } else {
                self.line.push(c);
            }
        }
        out
    }

    fn process(&mut self, line: &str) -> Option<Frame> {
        if line.is_empty() {
            return self.dispatch();
        }
        let (field, value) = match line.split_once(':') {
            Some((field, value)) => (field, value.strip_prefix(' ').unwrap_or(value)),
            None => (line, ""),
        };
        match field {
            "event" => self.event = value.to_string(),
            "data" => {
                self.data.push_str(value);
                self.data.push('\n');
            }
            "id" if !value.contains('\0') => self.last_event_id = value.to_string(),
            "retry" if !value.is_empty() && value.bytes().all(|b| b.is_ascii_digit()) => {
                self.retry = value.parse().ok().or(self.retry);
            }
            // A comment (`: agentrq`, the keepalive) has an empty field name.
            _ => {}
        }
        None
    }

    fn dispatch(&mut self) -> Option<Frame> {
        let event = std::mem::take(&mut self.event);
        let mut data = std::mem::take(&mut self.data);
        // No data, no event: the type is still reset.
        data.pop()?;
        Some(Frame::Message {
            event: if event.is_empty() {
                "message".to_string()
            } else {
                event
            },
            data,
            last_event_id: self.last_event_id.clone(),
        })
    }

    /// A new connection: half a line from the old one is dropped, the last
    /// event id and the retry delay are kept.
    pub fn reconnected(&mut self) {
        self.line.clear();
        self.after_cr = false;
        self.event.clear();
        self.data.clear();
    }

    pub fn last_event_id(&self) -> &str {
        &self.last_event_id
    }

    pub fn retry(&self) -> Option<u64> {
        self.retry
    }
}

/// Where frames go. `false` when nobody is listening any more.
pub trait Sink: Send + 'static {
    fn send(&self, frame: Frame) -> bool;
}

/// Whether the response is an event stream the spec would open.
fn is_event_stream(response: &reqwest::Response) -> bool {
    let kind = response
        .headers()
        .get("content-type")
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    response.status() == 200 && kind.to_ascii_lowercase().starts_with(ACCEPT)
}

/// Read the stream at `url` until it fails for good or the sink goes away.
pub async fn run(client: reqwest::Client, url: String, sink: Box<dyn Sink>) {
    let mut parser = Parser::default();
    loop {
        let mut request = client
            .get(&url)
            .header("accept", ACCEPT)
            .header("cache-control", "no-store");
        if !parser.last_event_id().is_empty() {
            request = request.header("last-event-id", parser.last_event_id());
        }
        let deadline = Duration::from_millis(PROXY_TIMEOUT_MS);
        if let Ok(Ok(response)) = tokio::time::timeout(deadline, request.send()).await {
            if !is_event_stream(&response) {
                sink.send(Frame::Error { ready_state: 2 });
                return;
            }
            if !sink.send(Frame::Open) {
                return;
            }
            let mut decoder = Utf8Decoder::new();
            let mut stream = response.bytes_stream();
            let idle = Duration::from_millis(IDLE_TIMEOUT);
            while let Ok(Some(Ok(chunk))) = tokio::time::timeout(idle, stream.next()).await {
                for frame in parser.feed(&decoder.decode(&chunk)) {
                    if !sink.send(frame) {
                        return;
                    }
                }
            }
            parser.reconnected();
        }
        if !sink.send(Frame::Error { ready_state: 0 }) {
            return;
        }
        let retry = parser.retry().unwrap_or(DEFAULT_RETRY_MS);
        tokio::time::sleep(Duration::from_millis(retry)).await;
    }
}

/// The streams the page has open, by the id it closes them with.
#[derive(Default)]
pub struct Streams {
    next: AtomicU32,
    running: Mutex<HashMap<u32, AbortHandle>>,
}

impl Streams {
    pub fn open(&self, stream: impl Future<Output = ()> + Send + 'static) -> u32 {
        self.keep(tokio::spawn(stream).abort_handle())
    }

    fn keep(&self, handle: AbortHandle) -> u32 {
        let id = self.next.fetch_add(1, Ordering::Relaxed) + 1;
        let mut running = self.running.lock().unwrap_or_else(|p| p.into_inner());
        // A stream that failed for good has ended by itself.
        running.retain(|_, handle| !handle.is_finished());
        running.insert(id, handle);
        id
    }

    pub fn close(&self, id: u32) {
        let removed = self
            .running
            .lock()
            .unwrap_or_else(|p| p.into_inner())
            .remove(&id);
        if let Some(handle) = removed {
            handle.abort();
        }
    }

    #[cfg(test)]
    fn len(&self) -> usize {
        self.running.lock().unwrap().len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::test_server::{closed_port, reply, send, Step, TestServer};
    use std::sync::Arc;
    use tokio::sync::mpsc;

    fn message(event: &str, data: &str, id: &str) -> Frame {
        Frame::Message {
            event: event.into(),
            data: data.into(),
            last_event_id: id.into(),
        }
    }

    #[test]
    fn parses_the_spec_examples() {
        let mut parser = Parser::default();
        let frames = parser.feed(
            ": test stream\n\ndata: first event\nid: 1\n\ndata:second event\nid\n\ndata:  third event\n\n",
        );
        assert_eq!(
            frames,
            [
                message("message", "first event", "1"),
                message("message", "second event", ""),
                message("message", " third event", ""),
            ]
        );

        let frames = parser.feed("data\n\ndata\ndata\n\ndata:\n");
        assert_eq!(
            frames,
            [message("message", "", ""), message("message", "\n", "")]
        );
        assert!(
            parser.feed("\n").len() == 1,
            "the last block dispatches on its blank line"
        );
    }

    #[test]
    fn names_events_and_joins_multi_line_data() {
        let mut parser = Parser::default();
        let frames = parser
            .feed("event: task.created\ndata: {\"a\":\ndata: 1}\n\nevent: ignored\n\ndata: x\n\n");
        assert_eq!(
            frames,
            [
                message("task.created", "{\"a\":\n1}", ""),
                message("message", "x", "")
            ],
            "an event with no data dispatches nothing and resets the type"
        );
    }

    #[test]
    fn splits_lines_on_cr_lf_and_crlf_even_across_pieces() {
        let mut parser = Parser::default();
        let mut frames = parser.feed("data: a\r\n\r");
        frames.extend(parser.feed("\ndata: b\r\rdata: c\n"));
        frames.extend(parser.feed("\n"));
        assert_eq!(
            frames,
            [
                message("message", "a", ""),
                message("message", "b", ""),
                message("message", "c", "")
            ]
        );
    }

    #[test]
    fn keeps_the_last_event_id_and_ignores_one_with_a_nul() {
        let mut parser = Parser::default();
        parser.feed("id: 7\ndata: a\n\nid: 8\u{0}\ndata: b\n\n");
        assert_eq!(parser.last_event_id(), "7");
    }

    #[test]
    fn takes_a_retry_of_digits_only() {
        let mut parser = Parser::default();
        parser.feed("retry: 50\n");
        assert_eq!(parser.retry(), Some(50));
        parser.feed("retry: 5x\nretry:\nretry: 99999999999999999999999\nunknown: 1\n");
        assert_eq!(parser.retry(), Some(50));
    }

    #[test]
    fn drops_half_a_line_when_it_reconnects() {
        let mut parser = Parser::default();
        parser.feed("id: 3\nevent: x\ndata: half\nda");
        parser.reconnected();
        assert_eq!(
            parser.feed("ta: lost\n\ndata: whole\n\n"),
            [message("message", "whole", "3")]
        );
    }

    #[test]
    fn frames_say_what_the_bridge_reads() {
        let to_json = |f: &Frame| serde_json::to_value(f).unwrap();
        assert_eq!(to_json(&Frame::Open), serde_json::json!({"type": "open"}));
        assert_eq!(
            to_json(&message("x", "d", "1")),
            serde_json::json!({"type": "message", "event": "x", "data": "d", "lastEventId": "1"})
        );
        assert_eq!(
            to_json(&Frame::Error { ready_state: 2 }),
            serde_json::json!({"type": "error", "readyState": 2})
        );
    }

    /// Frames into a channel, until `stop_after` of them have been sent.
    struct Collect {
        tx: mpsc::UnboundedSender<Frame>,
        stop_after: usize,
        sent: std::sync::atomic::AtomicUsize,
    }

    impl Sink for Collect {
        fn send(&self, frame: Frame) -> bool {
            let _ = self.tx.send(frame);
            self.sent.fetch_add(1, Ordering::Relaxed) + 1 < self.stop_after
        }
    }

    fn collect(stop_after: usize) -> (Collect, mpsc::UnboundedReceiver<Frame>) {
        let (tx, rx) = mpsc::unbounded_channel();
        let sink = Collect {
            tx,
            stop_after,
            sent: Default::default(),
        };
        (sink, rx)
    }

    async fn drain(rx: &mut mpsc::UnboundedReceiver<Frame>) -> Vec<Frame> {
        let mut out = Vec::new();
        while let Some(frame) = rx.recv().await {
            out.push(frame);
        }
        out
    }

    const STREAM: &str = "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream; charset=utf-8\r\nconnection: close\r\n\r\n";

    #[tokio::test]
    async fn streams_events_and_reconnects_with_the_last_event_id() {
        let server = TestServer::start(|seen| {
            if seen.header("last-event-id").is_none() {
                vec![
                    send(STREAM),
                    send("retry: 10\nid: 5\ndata: one\n\n"),
                    send("data: cut"),
                ]
            } else {
                vec![send(STREAM), send("data: two\n\n")]
            }
        })
        .await;
        let (sink, mut rx) = collect(6);
        run(
            reqwest::Client::new(),
            format!("{}/api/v1/events", server.url),
            Box::new(sink),
        )
        .await;
        assert_eq!(
            drain(&mut rx).await,
            [
                Frame::Open,
                message("message", "one", "5"),
                Frame::Error { ready_state: 0 },
                Frame::Open,
                message("message", "two", "5"),
                Frame::Error { ready_state: 0 },
            ]
        );
        let seen = server.requests();
        assert_eq!(seen[0].header("accept"), Some("text/event-stream"));
        assert_eq!(seen[1].header("last-event-id"), Some("5"));
    }

    #[tokio::test]
    async fn fails_for_good_on_anything_but_a_200_event_stream() {
        for script in [
            reply("401 Unauthorized", &["content-type: text/event-stream"], ""),
            reply("200 OK", &["content-type: application/json"], "{}"),
            reply("200 OK", &[], ""),
        ] {
            let script = std::sync::Mutex::new(Some(script));
            let server =
                TestServer::start(move |_| script.lock().unwrap().take().unwrap_or_default()).await;
            let (sink, mut rx) = collect(usize::MAX);
            run(reqwest::Client::new(), server.url.clone(), Box::new(sink)).await;
            assert_eq!(drain(&mut rx).await, [Frame::Error { ready_state: 2 }]);
        }
    }

    #[tokio::test]
    async fn retries_a_server_it_cannot_reach_until_nobody_listens() {
        tokio::time::pause();
        let (sink, mut rx) = collect(2);
        run(reqwest::Client::new(), closed_port().await, Box::new(sink)).await;
        assert_eq!(
            drain(&mut rx).await,
            [
                Frame::Error { ready_state: 0 },
                Frame::Error { ready_state: 0 }
            ]
        );
    }

    #[tokio::test]
    async fn treats_a_silent_stream_as_dropped() {
        tokio::time::pause();
        let server = TestServer::start(|_| vec![send(STREAM), Step::Wait(IDLE_TIMEOUT * 2)]).await;
        let (sink, mut rx) = collect(2);
        run(reqwest::Client::new(), server.url.clone(), Box::new(sink)).await;
        assert_eq!(
            drain(&mut rx).await,
            [Frame::Open, Frame::Error { ready_state: 0 }]
        );
    }

    #[tokio::test]
    async fn stops_when_nobody_listens_any_more() {
        let server =
            TestServer::start(|_| vec![send(STREAM), send("data: a\n\ndata: b\n\n")]).await;
        let (sink, mut rx) = collect(1);
        run(reqwest::Client::new(), server.url.clone(), Box::new(sink)).await;
        assert_eq!(drain(&mut rx).await, [Frame::Open]);

        let (sink, mut rx) = collect(2);
        run(reqwest::Client::new(), server.url.clone(), Box::new(sink)).await;
        assert_eq!(
            drain(&mut rx).await,
            [Frame::Open, message("message", "a", "")]
        );
    }

    /// A stream that never ends, holding a sender its receiver can watch.
    struct Hold(#[allow(dead_code)] mpsc::UnboundedSender<()>);

    impl Future for Hold {
        type Output = ();
        fn poll(
            self: std::pin::Pin<&mut Self>,
            _: &mut std::task::Context<'_>,
        ) -> std::task::Poll<()> {
            std::task::Poll::Pending
        }
    }

    #[tokio::test]
    async fn closes_a_stream_by_id_and_forgets_ended_ones() {
        let streams = Arc::new(Streams::default());
        let (tx, mut rx) = mpsc::unbounded_channel::<()>();
        let first = streams.open(Hold(tx));
        let ended = streams.open(async {});
        assert_ne!(first, ended);
        tokio::task::yield_now().await;

        streams.close(first);
        assert_eq!(rx.recv().await, None, "aborted, so its sender is gone");
        streams.close(first);

        streams.open(std::future::pending());
        assert_eq!(streams.len(), 1, "the ended one was swept");
    }

    #[test]
    fn a_poisoned_registry_still_answers() {
        let streams = Arc::new(Streams::default());
        let held = streams.clone();
        let _ = std::thread::spawn(move || {
            let _guard = held.running.lock().unwrap();
            std::panic::resume_unwind(Box::new("poison"));
        })
        .join();
        let runtime = tokio::runtime::Builder::new_current_thread()
            .build()
            .unwrap();
        runtime.block_on(async {
            let id = streams.open(async {});
            tokio::task::yield_now().await;
            streams.close(id);
        });
    }
}
