// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The shell's own event stream, `/api/v1/events/stream`, read through the
//! active profile's client: Electron's `startEventStream`.
//!
//! The rules are core's `sse::EventStreamClient`: the backoff, the hard stop
//! on 401 and the idle watchdog racing the connect and every read. This
//! module runs it. One task owns the client; the transport and the timers
//! answer it over a channel, so its `&mut self` calls never overlap and a
//! handler never runs under a lock it could re-enter.

use std::future::Future;
use std::pin::Pin;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use agentrq_desktop_core::ext_host::awaits_review;
use agentrq_desktop_core::sse::{
    EventStreamClient, Handler, ResponseHead, StreamRequest, Ticket, TimerKind, Timers, Transport,
    IDLE_TIMEOUT,
};
use futures_util::{Stream, StreamExt};
use serde_json::Value;
use tokio::sync::mpsc::{unbounded_channel, UnboundedReceiver, UnboundedSender};
use tokio::sync::Semaphore;
use tokio::task::AbortHandle;

use crate::ext_host::ExtHost;
use crate::network::Network;

pub const STREAM_PATH: &str = "/api/v1/events/stream";

/// Where the stream's events go.
pub trait Events: Send + Sync {
    fn on_event(&self, event: &Value);
}

/// How many tool-call reviews may wait on the extension host at once. Past
/// it a tool call goes unreviewed, and the person answers it.
const MAX_REVIEWS: usize = 32;

/// Forwards events to the extension host for tool-call review when running,
/// while passing every event to the inner [`Events`] handler. Each is sent as
/// it comes, so the host has them in the stream's order, but none waits for
/// the one before to be answered: Electron does not await a review either,
/// and a slow reviewer would hold up every tool call behind it.
pub struct ToolReviewEvents {
    notifier: Arc<dyn Events>,
    host: Arc<ExtHost>,
    reviewing: Arc<Semaphore>,
    /// Whether the last tool call found every review taken: said once.
    full: AtomicBool,
}

impl ToolReviewEvents {
    pub fn new(notifier: Arc<dyn Events>, host: Arc<ExtHost>) -> Self {
        Self {
            notifier,
            host,
            reviewing: Arc::new(Semaphore::new(MAX_REVIEWS)),
            full: AtomicBool::new(false),
        }
    }
}

impl Events for ToolReviewEvents {
    fn on_event(&self, event: &Value) {
        self.notifier.on_event(event);
        if !awaits_review(event) {
            return;
        }
        let Ok(permit) = Arc::clone(&self.reviewing).try_acquire_owned() else {
            if !self.full.swap(true, Ordering::Relaxed) {
                eprintln!("[extensions] {MAX_REVIEWS} tool-call reviews wait; more go unreviewed until one ends");
            }
            return;
        };
        self.full.store(false, Ordering::Relaxed);
        let params = serde_json::json!({ "event": event });
        if let Some(review) = self.host.ask_running("reviewToolCalls", params) {
            tauri::async_runtime::spawn(async move {
                let _ = review.await;
                drop(permit);
            });
        }
    }
}

/// What the task owning the client is told: a command, or an answer to one
/// of its requests.
enum Message {
    Start,
    Stop,
    Response(u64, ResponseHead),
    Chunk(u64, Vec<u8>),
    End(u64),
    Error(u64),
    Timer(Ticket),
}

type Client = EventStreamClient<NetTransport, TokioTimers, StreamHandler>;

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// The running stream. Commands are queued, so they can be given from
/// anywhere, a sync callback included.
pub struct EventStream {
    network: Arc<Network>,
    tx: UnboundedSender<Message>,
}

impl EventStream {
    pub fn spawn(network: Arc<Network>, events: Arc<dyn Events>) -> Self {
        Self::with_idle_timeout(network, events, IDLE_TIMEOUT)
    }

    fn with_idle_timeout(network: Arc<Network>, events: Arc<dyn Events>, idle_ms: u64) -> Self {
        let (tx, rx) = unbounded_channel();
        let transport = NetTransport {
            network: network.clone(),
            tx: tx.clone(),
            body: Arc::default(),
            tasks: Tasks::default(),
        };
        let timers = TokioTimers {
            tx: tx.clone(),
            tasks: Tasks::default(),
        };
        let handler = StreamHandler {
            network: network.clone(),
            events,
        };
        let client = EventStreamClient::new(transport, timers, handler).with_idle_timeout(idle_ms);
        tauri::async_runtime::spawn(drive(client, rx));
        Self { network, tx }
    }

    /// Read the active profile's stream afresh: whatever was open is
    /// dropped, and the backoff starts from the bottom. With no server there
    /// is nothing to read, so it only stops.
    pub fn start(&self) {
        let message = if self.network.server_url().is_empty() {
            Message::Stop
        } else {
            Message::Start
        };
        let _ = self.tx.send(message);
    }

    pub fn stop(&self) {
        let _ = self.tx.send(Message::Stop);
    }
}

async fn drive(mut client: Client, mut rx: UnboundedReceiver<Message>) {
    while let Some(message) = rx.recv().await {
        match message {
            Message::Start => {
                client.stop();
                client.start();
            }
            Message::Stop => client.stop(),
            Message::Response(attempt, head) => client.on_response(attempt, head),
            Message::Chunk(attempt, bytes) => client.on_chunk(attempt, &bytes),
            Message::End(attempt) => client.on_end(attempt),
            Message::Error(attempt) => client.on_error(attempt),
            Message::Timer(ticket) => client.on_timer(ticket),
        }
    }
}

/// Spawned tasks by the attempt or ticket that can abandon them.
#[derive(Default)]
struct Tasks(Mutex<Vec<(u64, AbortHandle)>>);

impl Tasks {
    fn spawn(&self, key: u64, task: impl Future<Output = ()> + Send + 'static) {
        let handle = tokio::spawn(task).abort_handle();
        let mut tasks = lock(&self.0);
        tasks.retain(|(_, handle)| !handle.is_finished());
        tasks.push((key, handle));
    }

    fn abort(&self, key: u64) {
        for (_, handle) in lock(&self.0).iter().filter(|(k, _)| *k == key) {
            handle.abort();
        }
    }
}

type Body = Pin<Box<dyn Stream<Item = reqwest::Result<Vec<u8>>> + Send>>;

/// reqwest, on whichever profile is active when an attempt starts. The body
/// waits here between reads, kept by the attempt it belongs to.
struct NetTransport {
    network: Arc<Network>,
    tx: UnboundedSender<Message>,
    body: Arc<Mutex<Option<(u64, Body)>>>,
    tasks: Tasks,
}

impl Transport for NetTransport {
    fn connect(&self, attempt: u64, request: &StreamRequest) {
        let (network, tx, body) = (self.network.clone(), self.tx.clone(), self.body.clone());
        let (url, accept) = (request.url.clone(), request.accept);
        self.tasks.spawn(attempt, async move {
            let client = network.active().await.client.clone();
            let sent = client
                .get(&url)
                .header("accept", accept)
                .header("cache-control", "no-store")
                .send()
                .await;
            let message = match sent {
                Ok(response) => {
                    let status = response.status().as_u16();
                    let chunks = response
                        .bytes_stream()
                        .map(|chunk| chunk.map(|b| b.to_vec()));
                    *lock(&body) = Some((attempt, Box::pin(chunks)));
                    Message::Response(
                        attempt,
                        ResponseHead {
                            status,
                            has_body: true,
                        },
                    )
                }
                Err(_) => Message::Error(attempt),
            };
            let _ = tx.send(message);
        });
    }

    fn read(&self, attempt: u64) {
        let (tx, body) = (self.tx.clone(), self.body.clone());
        self.tasks.spawn(attempt, async move {
            let taken = lock(&body).take().filter(|(owner, _)| *owner == attempt);
            let message = match taken {
                Some((_, mut stream)) => match stream.next().await {
                    Some(Ok(bytes)) => {
                        *lock(&body) = Some((attempt, stream));
                        Message::Chunk(attempt, bytes)
                    }
                    Some(Err(_)) => Message::Error(attempt),
                    None => Message::End(attempt),
                },
                None => Message::Error(attempt),
            };
            let _ = tx.send(message);
        });
    }

    fn abort(&self, attempt: u64) {
        self.tasks.abort(attempt);
        let mut body = lock(&self.body);
        if body.as_ref().is_some_and(|(owner, _)| *owner == attempt) {
            *body = None;
        }
    }
}

struct TokioTimers {
    tx: UnboundedSender<Message>,
    tasks: Tasks,
}

impl Timers for TokioTimers {
    fn start(&self, _kind: TimerKind, ms: u64, ticket: Ticket) {
        let tx = self.tx.clone();
        self.tasks.spawn(ticket, async move {
            tokio::time::sleep(Duration::from_millis(ms)).await;
            let _ = tx.send(Message::Timer(ticket));
        });
    }

    fn cancel(&self, ticket: Ticket) {
        self.tasks.abort(ticket);
    }
}

struct StreamHandler {
    network: Arc<Network>,
    events: Arc<dyn Events>,
}

impl Handler for StreamHandler {
    /// The active profile's server, read again for every attempt.
    fn stream_url(&self) -> String {
        format!("{}{STREAM_PATH}", self.network.server_url())
    }

    fn on_event(&self, event: &Value) {
        self.events.on_event(event);
    }
}

/// The events seen so far, for tests.
#[cfg(test)]
#[derive(Default)]
pub(crate) struct Recorded(pub Mutex<Vec<Value>>);

#[cfg(test)]
impl Events for Recorded {
    fn on_event(&self, event: &Value) {
        lock(&self.0).push(event.clone());
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::network::tests::network;
    use crate::test_server::{reply, send, Step, TestServer};
    use serde_json::json;

    const HEAD: &str = "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\n\r\n";

    /// Polls until `done`, for at most `ms`.
    pub(crate) async fn until(ms: u64, done: &dyn Fn() -> bool) -> bool {
        let poll = async {
            while !done() {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
        };
        tokio::time::timeout(Duration::from_millis(ms), poll)
            .await
            .is_ok()
    }

    fn rig(url: &str, idle_ms: u64) -> (EventStream, Arc<Recorded>, tempfile::TempDir) {
        let dir = tempfile::tempdir().unwrap();
        let network = Arc::new(network(dir.path(), url));
        let events = Arc::new(Recorded::default());
        let stream = EventStream::with_idle_timeout(network, events.clone(), idle_ms);
        (stream, events, dir)
    }

    fn seen(events: &Recorded) -> Vec<Value> {
        lock(&events.0).clone()
    }

    #[tokio::test]
    async fn reads_events_through_the_profile_client_and_asks_for_a_stream() {
        let server = TestServer::start(|_| {
            vec![
                send(HEAD),
                send(": agentrq\n\ndata: {\"type\":\"task.created\"}\n\ndata: not json\n\n"),
                send("data: {\"type\":\"reply"),
                Step::Wait(20),
                send(".received\"}\n\n"),
                Step::Wait(5_000),
            ]
        })
        .await;
        let (stream, events, _dir) = rig(&server.url, IDLE_TIMEOUT);
        stream.start();
        assert!(until(5_000, &|| seen(&events).len() == 2).await);
        assert_eq!(
            seen(&events),
            [
                json!({"type": "task.created"}),
                json!({"type": "reply.received"})
            ]
        );
        let request = &server.requests()[0];
        assert_eq!(request.target, STREAM_PATH);
        assert_eq!(request.header("accept"), Some("text/event-stream"));
        stream.stop();
    }

    #[tokio::test]
    async fn a_401_is_a_hard_stop_until_started_again() {
        let server = TestServer::start(|_| reply("401 Unauthorized", &[], "")).await;
        let (stream, _events, _dir) = rig(&server.url, IDLE_TIMEOUT);
        stream.start();
        assert!(until(5_000, &|| server.requests().len() == 1).await);
        // Past the first backoff: nothing tries again.
        tokio::time::sleep(Duration::from_millis(1_500)).await;
        assert_eq!(server.requests().len(), 1);
        // Signing in starts it again.
        stream.start();
        assert!(until(5_000, &|| server.requests().len() == 2).await);
    }

    #[tokio::test]
    async fn reconnects_after_the_server_ends_the_stream() {
        let server = TestServer::start(|_| vec![send(HEAD), send("data: {\"n\":1}\n\n")]).await;
        let (stream, events, _dir) = rig(&server.url, IDLE_TIMEOUT);
        stream.start();
        // The server closes after one event; a second later it is read again.
        assert!(until(5_000, &|| seen(&events).len() == 2).await);
        stream.stop();
    }

    #[tokio::test]
    async fn gives_up_on_a_silent_stream_and_on_a_connect_that_never_answers() {
        // Headers, then nothing: the watchdog ends the read.
        let silent = TestServer::start(|_| vec![send(HEAD), Step::Wait(10_000)]).await;
        let (stream, _, _dir) = rig(&silent.url, 100);
        stream.start();
        assert!(until(5_000, &|| silent.requests().len() == 2).await);
        stream.stop();

        // No headers at all: the watchdog ends the connect.
        let mute = TestServer::start(|_| vec![Step::Wait(10_000)]).await;
        let (stream, _, _dir) = rig(&mute.url, 100);
        stream.start();
        assert!(until(5_000, &|| mute.requests().len() == 2).await);
        stream.stop();
    }

    #[tokio::test]
    async fn a_failed_read_reconnects() {
        // A chunked body cut off mid-chunk is a read error, not an end.
        let server = TestServer::start(|_| {
            vec![send(
                "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\ntransfer-encoding: chunked\r\n\r\n5\r\nda",
            )]
        })
        .await;
        let (stream, _, _dir) = rig(&server.url, IDLE_TIMEOUT);
        stream.start();
        assert!(until(5_000, &|| server.requests().len() == 2).await);
        stream.stop();
    }

    #[tokio::test]
    async fn with_no_server_it_only_stops() {
        let (stream, events, _dir) = rig("", IDLE_TIMEOUT);
        stream.start();
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(seen(&events).is_empty());
    }

    fn transport(dir: &tempfile::TempDir) -> (NetTransport, UnboundedReceiver<Message>) {
        let (tx, rx) = unbounded_channel();
        let transport = NetTransport {
            network: Arc::new(network(dir.path(), "")),
            tx,
            body: Arc::default(),
            tasks: Tasks::default(),
        };
        (transport, rx)
    }

    #[tokio::test]
    async fn a_connect_that_fails_and_a_read_with_no_body_are_errors() {
        let dir = tempfile::tempdir().unwrap();
        let (transport, mut rx) = transport(&dir);
        let request = StreamRequest {
            url: format!("not a url{STREAM_PATH}"),
            accept: "text/event-stream",
        };
        transport.connect(3, &request);
        assert!(matches!(rx.recv().await, Some(Message::Error(3))));
        transport.read(7);
        assert!(matches!(rx.recv().await, Some(Message::Error(7))));
    }

    #[tokio::test]
    async fn ends_when_nothing_can_reach_it_any_more() {
        let dir = tempfile::tempdir().unwrap();
        let (transport, _answers) = transport(&dir);
        let timers = TokioTimers {
            tx: transport.tx.clone(),
            tasks: Tasks::default(),
        };
        let handler = StreamHandler {
            network: transport.network.clone(),
            events: Arc::new(Recorded::default()),
        };
        let (tx, rx) = unbounded_channel();
        tx.send(Message::Stop).unwrap();
        drop(tx);
        drive(EventStreamClient::new(transport, timers, handler), rx).await;
    }

    /// A stream event of `kind` that carries a tool call waiting on somebody.
    fn tool_call(kind: &str) -> Value {
        json!({ "type": kind, "payload": { "messages": [
            { "metadata": { "type": "permission_request", "status": "pending" } }
        ] } })
    }

    #[tokio::test]
    async fn tool_review_forwards_events_in_order_only_while_host_is_running() {
        use crate::ext_host::tests::{until, FakeServices};
        use crate::ext_host::Launch;
        let dir = tempfile::tempdir().unwrap();
        let reviewed = dir.path().join("reviewed");
        let notifier = Arc::new(Recorded::default());
        // Answers everything, and writes down each event it is asked to review.
        let echo_script = format!(
            r#"require('readline').createInterface({{ input: process.stdin }}).on('line', (line) => {{
  const m = JSON.parse(line)
  if (m.id === undefined) return
  if (m.method === 'reviewToolCalls') require('fs').appendFileSync({reviewed:?}, m.params.event.type + '\n')
  process.stdout.write(JSON.stringify({{ jsonrpc: '2.0', id: m.id, result: {{ ok: true }} }}) + '\n')
}})"#,
            reviewed = reviewed.to_string_lossy()
        );
        let launch = Launch {
            program: "node".into(),
            args: vec!["-e".into(), echo_script.into()],
        };
        std::fs::write(
            dir.path().join("extensions-installed.json"),
            r#"{"installations":[{"name":"x","enabled":true}]}"#,
        )
        .unwrap();
        let host = ExtHost::new(launch, dir.path().into(), Arc::new(FakeServices::default()));
        let review = ToolReviewEvents::new(notifier.clone(), host.clone());

        // While not running: notifier receives event, host is never started
        assert!(!host.is_running());
        review.on_event(&tool_call("first"));
        assert_eq!(seen(&notifier), [tool_call("first")]);
        assert!(!host.is_running());

        // Now start the host with a dummy call
        assert_eq!(host.call("state", json!({})).await.unwrap()["ok"], true);
        assert!(host.is_running());

        // While running: both get each tool call, the host in the stream's
        // order; an event with none is the notifier's alone.
        let types = ["a", "b", "c", "d", "e", "f", "g", "h"];
        for kind in types {
            review.on_event(&tool_call(kind));
            review.on_event(&json!({ "type": "plain" }));
        }
        assert_eq!(seen(&notifier).len(), 1 + 2 * types.len());
        let read = || std::fs::read_to_string(&reviewed).unwrap_or_default();
        let wanted: String = types.iter().map(|kind| format!("{kind}\n")).collect();
        until(|| read().len() >= wanted.len()).await;
        assert_eq!(read(), wanted, "in order, and never the first");
        host.stop();
    }

    #[tokio::test]
    async fn a_slow_review_holds_up_no_other_and_too_many_waiting_are_dropped() {
        use crate::ext_host::tests::{until, FakeServices};
        use crate::ext_host::Launch;
        let dir = tempfile::tempdir().unwrap();
        let reviewed = dir.path().join("reviewed");
        // Writes down each event it is asked to review, and never answers a
        // slow one; `state` is answered once every line before it is read.
        let script = format!(
            r#"require('readline').createInterface({{ input: process.stdin }}).on('line', (line) => {{
  const m = JSON.parse(line)
  if (m.id === undefined) return
  if (m.method === 'reviewToolCalls') {{
    require('fs').appendFileSync({reviewed:?}, m.params.event.type + '\n')
    if (m.params.event.type === 'slow') return
  }}
  process.stdout.write(JSON.stringify({{ jsonrpc: '2.0', id: m.id, result: {{ ok: true }} }}) + '\n')
}})"#,
            reviewed = reviewed.to_string_lossy()
        );
        let launch = Launch {
            program: "node".into(),
            args: vec!["-e".into(), script.into()],
        };
        std::fs::write(
            dir.path().join("extensions-installed.json"),
            r#"{"installations":[{"name":"x","enabled":true}]}"#,
        )
        .unwrap();
        let host = ExtHost::new(launch, dir.path().into(), Arc::new(FakeServices::default()));
        let review = ToolReviewEvents::new(Arc::new(Recorded::default()), host.clone());
        host.call("state", json!({})).await.unwrap();
        let read = || std::fs::read_to_string(&reviewed).unwrap_or_default();

        // The slow one is never answered; the two after it are reviewed at
        // once, not when it runs out of time a minute later.
        for kind in ["slow", "a", "b"] {
            review.on_event(&tool_call(kind));
        }
        until(|| read() == "slow\na\nb\n").await;
        until(|| review.reviewing.available_permits() == MAX_REVIEWS - 1).await;

        // Events with no tool call take no review's place, however many.
        for _ in 0..2 * MAX_REVIEWS {
            review.on_event(&json!({ "type": "plain" }));
        }
        assert_eq!(review.reviewing.available_permits(), MAX_REVIEWS - 1);

        // With every review waiting, the next tool call is not sent at all.
        for _ in 1..MAX_REVIEWS {
            review.on_event(&tool_call("slow"));
        }
        assert_eq!(review.reviewing.available_permits(), 0);
        assert!(!review.full.load(Ordering::Relaxed));
        review.on_event(&tool_call("late"));
        review.on_event(&tool_call("late"));
        assert!(review.full.load(Ordering::Relaxed), "said once");
        // Answered after everything sent before it has been read.
        host.call("state", json!({})).await.unwrap();
        assert_eq!(read().lines().count(), MAX_REVIEWS + 2);
        assert!(!read().contains("late"));
        host.stop();
    }
}
