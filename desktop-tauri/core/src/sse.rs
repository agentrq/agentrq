// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Server-sent events client for the main process.
//!
//! The renderer already consumes this stream through `useEventBus.js` for live
//! UI updates. The main process needs its own connection for a different reason:
//! notifications have to keep arriving when the window is in the background, and
//! on some platforms a backgrounded renderer is throttled. This client is
//! deliberately separate from the renderer's and does not replace it.
//!
//! Reconnect behaviour matches `useEventBus.js` exactly: one second, doubling to
//! a thirty second ceiling, and a hard stop on 401 rather than a reconnect loop
//! against a server that has already said no.
//!
//! Rust notes: there is no I/O here. The client is a state machine that asks a
//! `Transport` to connect, read and abort and a `Timers` to arm a timer, and is
//! told what happened by calling its `on_*` methods; a runtime (reqwest, tokio)
//! wires those up, and a test drives them by hand, time included. Every
//! transport call carries an attempt number and every timer a ticket that the
//! caller echoes back, so an answer to an attempt that has since been given up
//! on is recognised and dropped instead of confusing the next one.

use serde_json::Value;

pub const INITIAL_RECONNECT_DELAY: u64 = 1000;
pub const MAX_RECONNECT_DELAY: u64 = 30000;

/// How long the stream may deliver nothing at all before it is treated as dead.
///
/// The reconnect loop only runs when a read ends or fails, and a stream can do
/// neither: when the network service crashes it abandons the response rather
/// than failing it, so a read waits forever and the backoff is never reached.
/// Notifications stop for the life of the process and nothing says why.
///
/// Silence is a sound test here because the backend never goes quiet: it
/// flushes a `: agentrq` keepalive comment every thirty seconds
/// (`backend/internal/app/app.go`). This is 2.5 of those, so a slow round trip
/// or one missed tick is not mistaken for a dead connection.
pub const IDLE_TIMEOUT: u64 = 75000;

/// Sent with every request.
pub const ACCEPT: &str = "text/event-stream";

/// The delay after a failure at `current`. Mirrors `useEventBus.js`.
pub fn next_reconnect_delay(current: u64) -> u64 {
    current.saturating_mul(2).min(MAX_RECONNECT_DELAY)
}

/// Incremental parser for the SSE wire format.
///
/// Chunks arrive on arbitrary boundaries, so a frame can be split anywhere,
/// including mid-line. Only complete frames are emitted.
#[derive(Debug, Default)]
pub struct SseParser {
    buffer: String,
}

/// JavaScript's `trimStart`, whose whitespace also includes U+FEFF, which
/// Rust's `str::trim_start` does not strip.
fn trim_start(text: &str) -> &str {
    text.trim_start_matches(|c: char| c.is_whitespace() || c == '\u{FEFF}')
}

impl SseParser {
    pub fn new() -> Self {
        Self::default()
    }

    /// The `data` payload of each complete frame in this chunk. Keepalive
    /// comments and frames with no data yield nothing.
    pub fn feed(&mut self, chunk: &str) -> Vec<String> {
        self.buffer.push_str(chunk);
        let mut out = Vec::new();

        // Frames are separated by a blank line. Anything after the last
        // separator is an incomplete frame and stays buffered.
        while let Some(separator) = self.buffer.find("\n\n") {
            let frame: String = self.buffer.drain(..separator + 2).collect();
            let data = frame[..separator]
                .split('\n')
                // A line starting with ':' is a comment; the backend sends
                // ': agentrq' as its keepalive.
                .filter_map(|line| line.strip_prefix("data:"))
                .map(trim_start)
                .collect::<Vec<_>>()
                .join("\n");

            if !data.is_empty() {
                out.push(data);
            }
        }

        out
    }
}

/// Turns bytes into text across chunk boundaries, like `TextDecoder` with
/// `stream: true`: a character split between two chunks is held until its
/// other half arrives, and invalid bytes become U+FFFD.
#[derive(Debug, Default)]
pub struct Utf8Decoder {
    pending: Vec<u8>,
    /// Whether the start of the stream has been seen. `TextDecoder` drops a
    /// byte order mark there and nowhere else.
    started: bool,
}

impl Utf8Decoder {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn decode(&mut self, bytes: &[u8]) -> String {
        let text = self.decode_chunk(bytes);
        if text.is_empty() || self.started {
            return text;
        }
        self.started = true;
        match text.strip_prefix('\u{FEFF}') {
            Some(rest) => rest.to_string(),
            None => text,
        }
    }

    fn decode_chunk(&mut self, bytes: &[u8]) -> String {
        let mut buf = std::mem::take(&mut self.pending);
        buf.extend_from_slice(bytes);

        let mut out = String::new();
        let mut rest = buf.as_slice();
        loop {
            match std::str::from_utf8(rest) {
                Ok(valid) => {
                    out.push_str(valid);
                    return out;
                }
                Err(error) => {
                    let (valid, after) = rest.split_at(error.valid_up_to());
                    out.push_str(std::str::from_utf8(valid).unwrap_or_default());
                    match error.error_len() {
                        // The chunk ends inside a character.
                        None => {
                            self.pending = after.to_vec();
                            return out;
                        }
                        Some(len) => {
                            out.push('\u{FFFD}');
                            rest = &after[len..];
                        }
                    }
                }
            }
        }
    }
}

/// What to ask the transport for.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StreamRequest {
    pub url: String,
    pub accept: &'static str,
}

/// The head of the server's answer.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ResponseHead {
    pub status: u16,
    pub has_body: bool,
}

/// The network. Every call is a request to start something; the outcome comes
/// back through the client's `on_response`, `on_chunk`, `on_end` and `on_error`
/// with the same `attempt`.
pub trait Transport {
    /// Open the stream. Answered by `on_response` or `on_error`.
    fn connect(&self, attempt: u64, request: &StreamRequest);
    /// Ask for the next chunk. Answered by `on_chunk`, `on_end` or `on_error`.
    fn read(&self, attempt: u64);
    /// Abandon the attempt so the socket is released. The caller need not
    /// report anything further about it.
    fn abort(&self, attempt: u64);
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TimerKind {
    /// The watchdog: nothing arrived for [`IDLE_TIMEOUT`].
    Idle,
    /// The backoff before the next attempt.
    Reconnect,
}

/// A handle on one armed timer, echoed to `on_timer` when it expires.
pub type Ticket = u64;

/// Timers, injected so the watchdog and the backoff can be tested without
/// waiting.
pub trait Timers {
    /// Call `on_timer(ticket)` once after `ms`.
    fn start(&self, kind: TimerKind, ms: u64, ticket: Ticket);
    /// The timer is no longer wanted. Optional to honour: an expiry that is
    /// delivered anyway is ignored.
    fn cancel(&self, ticket: Ticket);
}

/// What the client reports to. Only the stream URL and the events are
/// required.
///
/// The client calls these from inside its own `&mut self` methods, so the
/// runtime that owns the client has to serialise `on_*` calls to it, usually
/// with a `Mutex<EventStreamClient>`, and the callbacks then run **under that
/// lock**. A handler must therefore not call back into the client (`stop`,
/// `restart`, `start`) from here, or the lock deadlocks: `on_unauthorized` is
/// the natural place to be tempted. Queue the call instead (a channel, or a
/// spawned task) so it runs after the lock is released.
pub trait Handler {
    /// Resolved per attempt, so a server switch is picked up on reconnect.
    fn stream_url(&self) -> String;
    fn on_event(&self, event: &Value);
    fn on_status(&self, _connected: bool) {}
    /// Called instead of reconnecting when the server rejects the credentials.
    fn on_unauthorized(&self) {}
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Phase {
    Stopped,
    /// Waiting for the response head.
    Connecting,
    /// Connected, waiting for a chunk.
    Reading,
    /// Backing off before the next attempt.
    Waiting,
}

/// Holds a connection to the event stream, reconnecting until stopped.
pub struct EventStreamClient<T: Transport, K: Timers, H: Handler> {
    transport: T,
    timers: K,
    handler: H,
    idle_timeout_ms: u64,
    phase: Phase,
    attempt: u64,
    next_ticket: Ticket,
    /// The one timer that can be pending: the watchdog while an attempt is in
    /// flight, the backoff while waiting.
    timer: Option<Ticket>,
    reconnect_delay: u64,
    parser: SseParser,
    decoder: Utf8Decoder,
}

impl<T: Transport, K: Timers, H: Handler> EventStreamClient<T, K, H> {
    pub fn new(transport: T, timers: K, handler: H) -> Self {
        Self {
            transport,
            timers,
            handler,
            idle_timeout_ms: IDLE_TIMEOUT,
            phase: Phase::Stopped,
            attempt: 0,
            next_ticket: 0,
            timer: None,
            reconnect_delay: INITIAL_RECONNECT_DELAY,
            parser: SseParser::new(),
            decoder: Utf8Decoder::new(),
        }
    }

    /// Silence after which the stream is assumed dead; see [`IDLE_TIMEOUT`].
    pub fn with_idle_timeout(mut self, ms: u64) -> Self {
        self.idle_timeout_ms = ms;
        self
    }

    pub fn is_running(&self) -> bool {
        self.phase != Phase::Stopped
    }

    pub fn start(&mut self) {
        if self.is_running() {
            return;
        }
        self.reconnect_delay = INITIAL_RECONNECT_DELAY;
        self.begin_attempt();
    }

    pub fn stop(&mut self) {
        match self.phase {
            Phase::Stopped => {}
            Phase::Connecting | Phase::Reading => {
                self.transport.abort(self.attempt);
                self.disarm();
                self.phase = Phase::Stopped;
                // The loop would report this when the aborted request failed;
                // saying it here does not depend on the transport doing so.
                self.handler.on_status(false);
            }
            Phase::Waiting => {
                self.disarm();
                self.phase = Phase::Stopped;
            }
        }
    }

    /// Give up on the current connection and let the client reconnect.
    ///
    /// The abort is the ordinary way to end a request, but the case this
    /// exists for, the network service having crashed under a live stream, is
    /// exactly the one where the request may not answer an abort either.
    /// Moving on without waiting for it is what makes recovery prompt rather
    /// than a wait of [`IDLE_TIMEOUT`]. The next attempt still waits out the
    /// current backoff.
    ///
    /// A no-op when stopped or already backing off: reconnecting a client
    /// nobody started is not a recovery, it is a leak.
    pub fn restart(&mut self) {
        if matches!(self.phase, Phase::Connecting | Phase::Reading) {
            self.give_up();
        }
    }

    /// The transport has the response head.
    pub fn on_response(&mut self, attempt: u64, head: ResponseHead) {
        if self.phase != Phase::Connecting || attempt != self.attempt {
            return;
        }
        self.disarm();

        if head.status == 401 {
            // Reconnecting would just be rejected again. The renderer's own
            // stream handles sending the user to the login screen.
            //
            // Stopped, not merely idle: a later `start()`, after a successful
            // sign-in, must not be a silent no-op.
            self.phase = Phase::Stopped;
            // The response is refused, not read: a transport that keeps a
            // connection per attempt would hold its socket until the next one.
            self.transport.abort(attempt);
            self.handler.on_status(false);
            self.handler.on_unauthorized();
            return;
        }

        if !(200..300).contains(&head.status) || !head.has_body {
            // Same reason: this response will never be read.
            self.give_up();
            return;
        }

        self.handler.on_status(true);
        // A connection that lasted is not suspect, so the next failure starts
        // its backoff from the bottom again.
        self.reconnect_delay = INITIAL_RECONNECT_DELAY;
        self.phase = Phase::Reading;
        self.parser = SseParser::new();
        self.decoder = Utf8Decoder::new();
        self.transport.read(attempt);
        self.arm(TimerKind::Idle, self.idle_timeout_ms);
    }

    /// The transport has a chunk of the body.
    pub fn on_chunk(&mut self, attempt: u64, bytes: &[u8]) {
        if self.phase != Phase::Reading || attempt != self.attempt {
            return;
        }
        self.disarm();

        let text = self.decoder.decode(bytes);
        for data in self.parser.feed(&text) {
            // A frame we cannot parse is not a reason to drop the connection;
            // the next one is probably fine.
            if let Ok(event) = serde_json::from_str::<Value>(&data) {
                self.handler.on_event(&event);
            }
        }

        self.transport.read(attempt);
        self.arm(TimerKind::Idle, self.idle_timeout_ms);
    }

    /// The server ended the stream.
    pub fn on_end(&mut self, attempt: u64) {
        if self.phase == Phase::Reading && attempt == self.attempt {
            self.disconnected();
        }
    }

    /// The connect or a read failed.
    pub fn on_error(&mut self, attempt: u64) {
        if matches!(self.phase, Phase::Connecting | Phase::Reading) && attempt == self.attempt {
            self.disconnected();
        }
    }

    /// A timer started through [`Timers::start`] expired.
    pub fn on_timer(&mut self, ticket: Ticket) {
        if self.timer != Some(ticket) {
            return;
        }
        self.timer = None;
        // A pending timer means the client is not stopped: it is the backoff
        // while waiting, and the watchdog otherwise.
        if self.phase == Phase::Waiting {
            self.begin_attempt();
        } else {
            self.give_up();
        }
    }

    fn arm(&mut self, kind: TimerKind, ms: u64) {
        self.next_ticket += 1;
        self.timer = Some(self.next_ticket);
        self.timers.start(kind, ms, self.next_ticket);
    }

    fn disarm(&mut self) {
        if let Some(ticket) = self.timer.take() {
            self.timers.cancel(ticket);
        }
    }

    fn begin_attempt(&mut self) {
        self.attempt += 1;
        self.phase = Phase::Connecting;
        self.transport.connect(
            self.attempt,
            &StreamRequest {
                url: self.handler.stream_url(),
                accept: ACCEPT,
            },
        );
        self.arm(TimerKind::Idle, self.idle_timeout_ms);
    }

    /// Abandon the request, then treat it as the failed connection it is.
    fn give_up(&mut self) {
        self.transport.abort(self.attempt);
        self.disconnected();
    }

    fn disconnected(&mut self) {
        self.disarm();
        self.handler.on_status(false);
        self.phase = Phase::Waiting;
        self.arm(TimerKind::Reconnect, self.reconnect_delay);
        self.reconnect_delay = next_reconnect_delay(self.reconnect_delay);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::RefCell;
    use std::rc::Rc;

    const URL: &str = "https://example.com/api/v1/events/stream";

    struct TimerRecord {
        kind: TimerKind,
        ms: u64,
        ticket: Ticket,
        cancelled: bool,
        fired: bool,
    }

    struct Log {
        url: String,
        requests: Vec<StreamRequest>,
        reads: u64,
        aborts: Vec<u64>,
        timers: Vec<TimerRecord>,
        events: Vec<Value>,
        statuses: Vec<bool>,
        unauthorized: u64,
    }

    type Shared = Rc<RefCell<Log>>;

    struct FakeTransport(Shared);
    struct FakeTimers(Shared);
    struct FakeHandler(Shared);
    /// Implements only what is required, for the optional callbacks.
    struct BareHandler;

    impl Transport for FakeTransport {
        fn connect(&self, _attempt: u64, request: &StreamRequest) {
            self.0.borrow_mut().requests.push(request.clone());
        }
        fn read(&self, _attempt: u64) {
            self.0.borrow_mut().reads += 1;
        }
        fn abort(&self, attempt: u64) {
            self.0.borrow_mut().aborts.push(attempt);
        }
    }

    impl Timers for FakeTimers {
        fn start(&self, kind: TimerKind, ms: u64, ticket: Ticket) {
            self.0.borrow_mut().timers.push(TimerRecord {
                kind,
                ms,
                ticket,
                cancelled: false,
                fired: false,
            });
        }
        fn cancel(&self, ticket: Ticket) {
            for timer in &mut self.0.borrow_mut().timers {
                if timer.ticket == ticket {
                    timer.cancelled = true;
                }
            }
        }
    }

    impl Handler for FakeHandler {
        fn stream_url(&self) -> String {
            self.0.borrow().url.clone()
        }
        fn on_event(&self, event: &Value) {
            self.0.borrow_mut().events.push(event.clone());
        }
        fn on_status(&self, connected: bool) {
            self.0.borrow_mut().statuses.push(connected);
        }
        fn on_unauthorized(&self) {
            self.0.borrow_mut().unauthorized += 1;
        }
    }

    impl Handler for BareHandler {
        fn stream_url(&self) -> String {
            URL.to_string()
        }
        fn on_event(&self, _event: &Value) {}
    }

    type Client = EventStreamClient<FakeTransport, FakeTimers, FakeHandler>;

    /// A client with every outside thing replaced by a recording fake, and the
    /// answers the network and the clock would give delivered by hand.
    struct Rig {
        client: Client,
        log: Shared,
    }

    fn rig() -> Rig {
        rig_with(None)
    }

    fn rig_with(idle_timeout_ms: Option<u64>) -> Rig {
        let log: Shared = Rc::new(RefCell::new(Log {
            url: URL.to_string(),
            requests: Vec::new(),
            reads: 0,
            aborts: Vec::new(),
            timers: Vec::new(),
            events: Vec::new(),
            statuses: Vec::new(),
            unauthorized: 0,
        }));
        let mut client = EventStreamClient::new(
            FakeTransport(log.clone()),
            FakeTimers(log.clone()),
            FakeHandler(log.clone()),
        );
        if let Some(ms) = idle_timeout_ms {
            client = client.with_idle_timeout(ms);
        }
        Rig { client, log }
    }

    impl Rig {
        /// The attempt in flight: the Nth request is attempt N.
        fn attempt(&self) -> u64 {
            self.log.borrow().requests.len() as u64
        }

        fn requests(&self) -> usize {
            self.log.borrow().requests.len()
        }

        fn respond(&mut self, status: u16, has_body: bool) {
            let attempt = self.attempt();
            self.client
                .on_response(attempt, ResponseHead { status, has_body });
        }

        fn connect_ok(&mut self) {
            self.respond(200, true);
        }

        fn chunk(&mut self, text: &str) {
            let attempt = self.attempt();
            self.client.on_chunk(attempt, text.as_bytes());
        }

        fn end(&mut self) {
            let attempt = self.attempt();
            self.client.on_end(attempt);
        }

        fn fail(&mut self) {
            let attempt = self.attempt();
            self.client.on_error(attempt);
        }

        /// The pending timer of this kind expires. Panics if none is pending.
        fn fire(&mut self, kind: TimerKind) {
            let ticket = {
                let mut log = self.log.borrow_mut();
                let timer = log
                    .timers
                    .iter_mut()
                    .rev()
                    .find(|t| t.kind == kind && !t.cancelled && !t.fired)
                    .expect("no pending timer of that kind");
                timer.fired = true;
                timer.ticket
            };
            self.client.on_timer(ticket);
        }

        fn has_pending(&self, kind: TimerKind) -> bool {
            self.log
                .borrow()
                .timers
                .iter()
                .any(|t| t.kind == kind && !t.cancelled && !t.fired)
        }

        /// How long each backoff was told to wait, in order.
        fn delays(&self) -> Vec<u64> {
            self.log
                .borrow()
                .timers
                .iter()
                .filter(|t| t.kind == TimerKind::Reconnect)
                .map(|t| t.ms)
                .collect()
        }

        fn events(&self) -> Vec<Value> {
            self.log.borrow().events.clone()
        }

        fn statuses(&self) -> Vec<bool> {
            self.log.borrow().statuses.clone()
        }

        fn aborts(&self) -> Vec<u64> {
            self.log.borrow().aborts.clone()
        }
    }

    fn parse(chunk: &str) -> Vec<String> {
        SseParser::new().feed(chunk)
    }

    // nextReconnectDelay

    #[test]
    fn doubles_matching_use_event_bus() {
        assert_eq!(next_reconnect_delay(1000), 2000);
        assert_eq!(next_reconnect_delay(2000), 4000);
        assert_eq!(next_reconnect_delay(8000), 16000);
    }

    #[test]
    fn caps_at_thirty_seconds_as_the_renderer_does() {
        assert_eq!(next_reconnect_delay(16000), MAX_RECONNECT_DELAY);
        assert_eq!(
            next_reconnect_delay(MAX_RECONNECT_DELAY),
            MAX_RECONNECT_DELAY
        );
    }

    #[test]
    fn starts_from_one_second() {
        assert_eq!(INITIAL_RECONNECT_DELAY, 1000);
    }

    // createSSEParser

    #[test]
    fn reads_a_complete_frame() {
        assert_eq!(parse("data: {\"a\":1}\n\n"), ["{\"a\":1}"]);
    }

    #[test]
    fn reads_several_frames_from_one_chunk() {
        assert_eq!(parse("data: one\n\ndata: two\n\n"), ["one", "two"]);
    }

    #[test]
    fn reassembles_a_frame_split_across_chunks() {
        // Chunks arrive on arbitrary boundaries, including mid-line.
        let mut parser = SseParser::new();
        assert!(parser.feed("data: {\"a\"").is_empty());
        assert!(parser.feed(":1}\n").is_empty());
        assert_eq!(parser.feed("\n"), ["{\"a\":1}"]);
    }

    #[test]
    fn holds_an_incomplete_trailing_frame_until_it_is_finished() {
        let mut parser = SseParser::new();
        assert_eq!(parser.feed("data: done\n\ndata: partial"), ["done"]);
        assert_eq!(parser.feed("\n\n"), ["partial"]);
    }

    #[test]
    fn ignores_the_keepalive_comment_the_backend_sends() {
        // ': agentrq' every 30s is what holds the connection open.
        assert!(parse(": agentrq\n\n").is_empty());
    }

    #[test]
    fn ignores_fields_other_than_data() {
        assert_eq!(parse("event: ping\nid: 7\ndata: payload\n\n"), ["payload"]);
    }

    #[test]
    fn joins_a_multi_line_data_field() {
        assert_eq!(parse("data: line1\ndata: line2\n\n"), ["line1\nline2"]);
    }

    #[test]
    fn tolerates_a_data_field_with_no_space_after_the_colon() {
        assert_eq!(parse("data:tight\n\n"), ["tight"]);
    }

    #[test]
    fn emits_nothing_for_an_empty_data_field() {
        assert!(parse("data:\n\n").is_empty());
    }

    // createEventStreamClient

    #[test]
    fn asks_for_an_event_stream() {
        let mut rig = rig();
        rig.client.start();

        let log = rig.log.borrow();
        assert_eq!(log.requests[0].url, URL);
        assert_eq!(log.requests[0].accept, "text/event-stream");
    }

    #[test]
    fn delivers_parsed_events() {
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        rig.chunk("data: {\"type\":\"task.created\"}\n\n");

        assert_eq!(rig.events(), [json!({"type": "task.created"})]);
    }

    #[test]
    fn survives_a_frame_it_cannot_parse() {
        // One malformed frame is not a reason to drop a working connection.
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        rig.chunk("data: not json\n\ndata: {\"type\":\"ok\"}\n\n");

        assert_eq!(rig.events(), [json!({"type": "ok"})]);
        assert_eq!(rig.log.borrow().reads, 2);
        assert!(rig.client.is_running());
    }

    #[test]
    fn reports_connected_and_disconnected() {
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        rig.end();

        assert_eq!(rig.statuses(), [true, false]);
    }

    #[test]
    fn stops_on_401_instead_of_hammering_a_server_that_said_no() {
        let mut rig = rig();
        rig.client.start();
        rig.respond(401, false);

        assert_eq!(rig.log.borrow().unauthorized, 1);
        assert_eq!(rig.aborts(), [1]);
        assert_eq!(rig.requests(), 1);
        assert!(rig.delays().is_empty());
        assert!(!rig.has_pending(TimerKind::Reconnect));
        assert!(!rig.has_pending(TimerKind::Idle));
    }

    #[test]
    fn backs_off_after_a_failure_doubling_each_time() {
        let mut rig = rig();
        rig.client.start();
        for _ in 0..3 {
            rig.fail();
            rig.fire(TimerKind::Reconnect);
        }

        assert_eq!(rig.delays(), [1000, 2000, 4000]);
        assert_eq!(rig.requests(), 4);
    }

    #[test]
    fn treats_a_non_ok_non_401_response_as_a_failure_and_retries() {
        let mut rig = rig();
        rig.client.start();
        rig.respond(503, false);

        assert_eq!(rig.aborts(), [1]);
        assert_eq!(rig.delays(), [1000]);
        rig.fire(TimerKind::Reconnect);
        assert_eq!(rig.requests(), 2);
    }

    #[test]
    fn resets_the_backoff_after_a_connection_that_worked() {
        // A connection that lasted is not suspect: the next failure should retry
        // promptly rather than inherit a long delay.
        let mut rig = rig();
        rig.client.start();
        rig.fail();
        rig.fire(TimerKind::Reconnect);
        rig.connect_ok();
        rig.chunk("data: {\"type\":\"ok\"}\n\n");
        rig.end();

        assert_eq!(rig.delays()[0], 1000);
        // The stream ended after a good connection, so back to the floor.
        assert_eq!(rig.delays()[1], 1000);
    }

    #[test]
    fn re_resolves_the_url_each_attempt_so_a_server_switch_is_picked_up() {
        let mut rig = rig();
        rig.log.borrow_mut().url = "https://first.example.com/api/v1/events/stream".into();
        rig.client.start();
        rig.fail();
        rig.client.stop();
        assert!(!rig.client.is_running());

        rig.log.borrow_mut().url = "https://second.example.com/api/v1/events/stream".into();
        rig.client.start();

        assert_eq!(
            rig.log.borrow().requests.last().unwrap().url,
            "https://second.example.com/api/v1/events/stream"
        );
    }

    #[test]
    fn reports_whether_it_is_running_and_starting_twice_is_a_no_op() {
        let mut rig = rig();

        assert!(!rig.client.is_running());
        rig.client.start();
        rig.client.start();
        assert!(rig.client.is_running());
        assert_eq!(rig.requests(), 1);

        rig.client.stop();
        assert!(!rig.client.is_running());
    }

    #[test]
    fn does_not_schedule_a_retry_when_stopped_mid_attempt() {
        // stop() during an in-flight request must end the loop then and there,
        // rather than falling through into a reconnect delay.
        let mut rig = rig();
        rig.client.start();
        rig.client.stop();
        // The aborted request failing is the transport's last word on it.
        rig.fail();

        assert!(rig.delays().is_empty());
        assert!(!rig.client.is_running());
    }

    #[test]
    fn treats_a_200_with_no_body_as_a_failure() {
        let mut rig = rig();
        rig.client.start();
        rig.respond(200, false);

        assert_eq!(rig.delays(), [1000]);
    }

    #[test]
    fn works_without_optional_callbacks() {
        // on_status and on_unauthorized are optional; omitting them must not
        // panic on either the connected path or the 401 path.
        let log: Shared = Rc::new(RefCell::new(Log {
            url: URL.to_string(),
            requests: Vec::new(),
            reads: 0,
            aborts: Vec::new(),
            timers: Vec::new(),
            events: Vec::new(),
            statuses: Vec::new(),
            unauthorized: 0,
        }));
        let mut client = EventStreamClient::new(
            FakeTransport(log.clone()),
            FakeTimers(log.clone()),
            BareHandler,
        );
        let ok = ResponseHead {
            status: 200,
            has_body: true,
        };

        client.start();
        client.on_response(1, ok);
        client.on_chunk(1, b"data: {\"type\":\"ok\"}\n\n");
        client.on_end(1);
        let ticket = log.borrow().timers.last().unwrap().ticket;
        client.on_timer(ticket);
        client.on_response(
            2,
            ResponseHead {
                status: 401,
                has_body: false,
            },
        );

        assert!(!client.is_running());
    }

    #[test]
    fn gives_up_on_a_stream_that_goes_silent_and_reconnects() {
        // The backend flushes a keepalive every thirty seconds, so silence means
        // the connection is gone. Without the watchdog the read never settles,
        // the loop never reaches its backoff, and notifications stop for the
        // life of the process with nothing logged.
        let mut rig = rig_with(Some(5));
        rig.client.start();
        rig.connect_ok();
        rig.fire(TimerKind::Idle);
        assert!(rig.statuses().contains(&false));

        rig.fire(TimerKind::Reconnect);
        assert_eq!(rig.requests(), 2);
    }

    #[test]
    fn gives_up_on_a_connection_attempt_that_never_answers() {
        // The same failure one step earlier. Left unguarded the loop stops on this
        // line and never reaches the backoff below it.
        let mut rig = rig_with(Some(5));
        rig.client.start();
        rig.fire(TimerKind::Idle);
        rig.fire(TimerKind::Reconnect);

        assert!(rig.requests() >= 2);
    }

    #[test]
    fn aborts_the_request_it_gave_up_on() {
        let mut rig = rig_with(Some(5));
        rig.client.start();
        rig.fire(TimerKind::Idle);

        assert_eq!(rig.aborts(), [1]);
    }

    #[test]
    fn restart_reconnects_a_stream_that_would_otherwise_never_answer() {
        // What the network-service crash handler calls. The read never settles
        // and the abort is ignored, the case restart() exists for, so moving the
        // client on is the only thing that gets it reconnecting.
        let mut rig = rig_with(Some(60000));
        rig.client.start();
        rig.connect_ok();
        assert!(rig.statuses().contains(&true));
        rig.client.restart();

        assert_eq!(rig.aborts(), [1]);
        assert!(!rig.has_pending(TimerKind::Idle));
        rig.fire(TimerKind::Reconnect);
        assert!(rig.requests() >= 2);
    }

    #[test]
    fn restart_on_a_stopped_client_is_a_no_op() {
        // Reconnecting a client nobody started is not a recovery, it is a leak.
        let mut rig = rig();

        rig.client.restart();

        assert_eq!(rig.requests(), 0);
        assert!(!rig.client.is_running());
    }

    #[test]
    fn waits_two_and_a_half_keepalives_before_calling_a_stream_dead() {
        // backend/internal/app/app.go flushes `: agentrq` every thirty seconds, so
        // one missed tick or a slow round trip must not look like a dead socket.
        assert_eq!(IDLE_TIMEOUT, 75000);
        let mut rig = rig();
        rig.client.start();
        assert_eq!(rig.log.borrow().timers[0].ms, IDLE_TIMEOUT);
    }

    #[test]
    fn aborts_the_in_flight_request_when_stopped() {
        let mut rig = rig();
        rig.client.start();
        rig.client.stop();

        assert_eq!(rig.aborts(), [1]);
    }

    // Beyond the vitest cases: what the Rust shape adds.

    #[test]
    fn stopping_mid_attempt_reports_disconnected_once_and_drops_the_watchdog() {
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        rig.client.stop();
        rig.fail();

        assert_eq!(rig.statuses(), [true, false]);
        assert!(!rig.has_pending(TimerKind::Idle));
    }

    #[test]
    fn stopping_while_backing_off_cancels_the_reconnect() {
        let mut rig = rig();
        rig.client.start();
        rig.fail();
        rig.client.stop();

        assert!(!rig.has_pending(TimerKind::Reconnect));
        assert_eq!(rig.statuses(), [false]);
        assert_eq!(rig.requests(), 1);
        // Stopping what is stopped says nothing.
        rig.client.stop();
        assert_eq!(rig.statuses(), [false]);
    }

    #[test]
    fn restart_while_backing_off_leaves_the_backoff_alone() {
        let mut rig = rig();
        rig.client.start();
        rig.fail();
        rig.client.restart();

        assert_eq!(rig.delays(), [1000]);
        assert!(rig.has_pending(TimerKind::Reconnect));
    }

    #[test]
    fn restart_keeps_the_backoff_going_rather_than_resetting_it() {
        let mut rig = rig();
        rig.client.start();
        rig.fail();
        rig.fire(TimerKind::Reconnect);
        rig.client.restart();

        assert_eq!(rig.delays(), [1000, 2000]);
    }

    #[test]
    fn ignores_what_an_abandoned_attempt_says_late() {
        let mut rig = rig_with(Some(5));
        rig.client.start();
        rig.fire(TimerKind::Idle);
        rig.fire(TimerKind::Reconnect);
        assert_eq!(rig.requests(), 2);

        // Attempt 1 answers after attempt 2 began.
        let ok = ResponseHead {
            status: 200,
            has_body: true,
        };
        rig.client.on_response(1, ok);
        rig.client.on_chunk(1, b"data: {\"late\":true}\n\n");
        rig.client.on_end(1);
        rig.client.on_error(1);

        assert!(rig.events().is_empty());
        assert_eq!(rig.statuses(), [false]);
        assert_eq!(rig.requests(), 2);
    }

    #[test]
    fn ignores_an_answer_that_does_not_fit_the_phase() {
        let mut rig = rig();
        rig.client.start();
        // A chunk or an end before the response head, and a response twice.
        rig.chunk("data: {\"x\":1}\n\n");
        rig.end();
        rig.connect_ok();
        rig.connect_ok();

        assert!(rig.events().is_empty());
        assert_eq!(rig.statuses(), [true]);
    }

    #[test]
    fn ignores_a_timer_that_was_cancelled_or_is_unknown() {
        let mut rig = rig();
        rig.client.start();
        let first = rig.log.borrow().timers[0].ticket;
        rig.connect_ok();

        // The connect-time watchdog was replaced by the read's; a late expiry
        // of it must not kill the connection.
        rig.client.on_timer(first);
        rig.client.on_timer(9999);

        assert!(rig.aborts().is_empty());
        assert_eq!(rig.statuses(), [true]);
    }

    #[test]
    fn re_arms_the_watchdog_on_every_chunk() {
        let mut rig = rig_with(Some(500));
        rig.client.start();
        rig.connect_ok();
        rig.chunk(": agentrq\n\n");
        rig.chunk(": agentrq\n\n");

        let log = rig.log.borrow();
        assert_eq!(log.timers.len(), 4);
        assert!(log.timers[..3].iter().all(|t| t.cancelled));
        assert!(!log.timers[3].cancelled);
        assert!(log.timers.iter().all(|t| t.ms == 500));
    }

    #[test]
    fn decodes_a_character_split_across_chunks() {
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        let bytes = "data: {\"t\":\"é\"}\n\n".as_bytes();
        let split = bytes.iter().position(|b| *b == 0xC3).unwrap() + 1;
        let attempt = rig.attempt();
        rig.client.on_chunk(attempt, &bytes[..split]);
        rig.client.on_chunk(attempt, &bytes[split..]);

        assert_eq!(rig.events(), [json!({"t": "é"})]);
    }

    #[test]
    fn drops_a_byte_order_mark_at_the_start_of_the_stream_like_text_decoder() {
        // Left in, the first line would read `\u{FEFF}data: …` and the first
        // event would be lost without a trace.
        let mut rig = rig();
        rig.client.start();
        rig.connect_ok();
        rig.chunk("\u{FEFF}data: {\"n\":1}\n\n");
        rig.chunk("data: {\"n\":2}\n\n");
        assert_eq!(rig.events(), [json!({"n": 1}), json!({"n": 2})]);

        // Only the first one: a mark later in the stream is data, and a
        // decoder that is reset by a new response starts over.
        let mut decoder = Utf8Decoder::new();
        assert_eq!(decoder.decode(b"\xEF\xBB"), "");
        assert_eq!(decoder.decode(b"\xBFa"), "a");
        assert_eq!(decoder.decode("\u{FEFF}b".as_bytes()), "\u{FEFF}b");
        assert_eq!(Utf8Decoder::new().decode("\u{FEFF}".as_bytes()), "");
        assert_eq!(Utf8Decoder::new().decode(b""), "");
    }

    #[test]
    fn strips_a_leading_byte_order_mark_from_a_data_value_like_trim_start() {
        // JS `trimStart` counts U+FEFF as whitespace; Rust's does not.
        let mut parser = SseParser::new();
        assert_eq!(parser.feed("data:\u{FEFF} \u{FEFF}x \n\n"), ["x "]);
        assert_eq!(trim_start("\u{FEFF}\t y"), "y");
        assert_eq!(trim_start("y\u{FEFF}"), "y\u{FEFF}");
    }

    #[test]
    fn replaces_invalid_bytes_instead_of_failing() {
        let mut decoder = Utf8Decoder::new();

        assert_eq!(decoder.decode(b"a\xFFb"), "a\u{FFFD}b");
        assert_eq!(decoder.decode(b"\xE2\x82"), "");
        assert_eq!(decoder.decode(b"\xAC"), "€");
    }
}
