// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Native notifications from the shell's event stream: Electron's
//! `handleStreamEvent`, `refreshWorkspaceNames` and the stream's starts and
//! stops around a profile.
//!
//! The rules are core's `notifications`: what notifies and its wording, the
//! self-action and dedupe gates, and the unread count. This module keeps their
//! state and asks the [`Desk`] for what only the app can do.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, MutexGuard, PoisonError};
use std::time::Duration;

use agentrq_desktop_core::notifications::{
    map_event_to_notification, task_id_from_self_action_request, BadgeSink, Clock, MapOptions,
    Notification, NotificationGate, SelfActionGate, SystemClock, UnreadCounter,
};
use serde_json::Value;

use crate::events::{EventStream, Events};
use crate::network::Network;
use crate::profiles::{Muted, Watcher};

pub const WORKSPACES_PATH: &str = "/api/v1/workspaces";

/// How long the workspace list may take. Every await needs a deadline
/// (`docs/agents/desktop.md`).
pub const NAMES_DEADLINE: Duration = Duration::from_secs(10);

/// What a notification's click carries back: the route to open.
pub const ROUTE_KEY: &str = "route";

/// What only the app can do. Never called with one of the [`Notifier`]'s
/// locks held.
pub trait Desk: Send + Sync {
    /// Raise a native notification; `false` when the system cannot.
    fn show(&self, notification: &Notification) -> bool;
    /// Bring the main window forward and send it to `route`.
    fn navigate(&self, route: &str);
    /// The badge and the tray's count. `generation` grows with every change,
    /// so the Desk can drop a count that arrives after a newer one.
    fn set_unread(&self, count: u32, generation: u64);
    /// The tray's recent workspaces.
    fn touch_workspace(&self, id: &str, name: &str);
}

/// The count is published by the [`Notifier`] once its lock is dropped: the
/// Desk can wait on the main thread, and the main thread clears the count
/// when the window is focused.
struct Unpublished;

impl BadgeSink for Unpublished {
    fn set_badge(&self, _count: u32) {}
}

/// The count, and how many times it has changed: two counts published after
/// the lock is dropped can reach the Desk in either order.
struct Unread {
    counter: UnreadCounter<Unpublished>,
    generation: u64,
}

impl Unread {
    fn changed(&mut self) -> u64 {
        self.generation += 1;
        self.generation
    }
}

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

pub struct Notifier {
    network: Arc<Network>,
    desk: Arc<dyn Desk>,
    muted: Mutex<Muted>,
    /// Workspace id to display name, for notification bodies.
    names: Arc<Mutex<HashMap<String, String>>>,
    /// This app's own replies and answers, marked as they are proxied.
    self_actions: Mutex<SelfActionGate<Arc<dyn Clock>>>,
    /// One task creation is published twice; one notification is shown.
    shown: Mutex<NotificationGate<Arc<dyn Clock>>>,
    unread: Mutex<Unread>,
}

impl Notifier {
    pub fn new(network: Arc<Network>, desk: Arc<dyn Desk>) -> Self {
        Self::with_clock(network, desk, Arc::new(SystemClock))
    }

    fn with_clock(network: Arc<Network>, desk: Arc<dyn Desk>, clock: Arc<dyn Clock>) -> Self {
        Self {
            network,
            unread: Mutex::new(Unread {
                counter: UnreadCounter::new(Unpublished),
                generation: 0,
            }),
            desk,
            muted: Mutex::default(),
            names: Arc::default(),
            self_actions: Mutex::new(SelfActionGate::new(clock.clone())),
            shown: Mutex::new(NotificationGate::new(clock)),
        }
    }

    /// The scheme's `on_proxied`: a reply or an answer of this app's own went
    /// out, so its echo on the stream is not news.
    pub fn proxied(&self, method: &str, path: &str) {
        lock(&self.self_actions).mark_self(task_id_from_self_action_request(method, path));
    }

    /// A list older than the one kept is dropped.
    pub fn set_muted(&self, muted: Muted) {
        let mut kept = lock(&self.muted);
        if muted.generation > kept.generation {
            *kept = muted;
        }
    }

    /// Looking at the app answers "is there anything new".
    pub fn clear_unread(&self) {
        let mut unread = lock(&self.unread);
        let had = unread.counter.value();
        unread.counter.clear();
        let generation = unread.changed();
        drop(unread);
        if had > 0 {
            self.desk.set_unread(0, generation);
        }
    }

    /// A notification was clicked.
    pub fn clicked(&self, route: &str) {
        self.clear_unread();
        self.desk.navigate(route);
    }

    /// Fetch the workspace names in the background. A name is a nicety; a
    /// notification without one is still useful.
    pub fn refresh_names(&self) {
        let (network, names) = (self.network.clone(), self.names.clone());
        tauri::async_runtime::spawn(async move {
            let server = network.server_url();
            let client = network.active().await.client.clone();
            let request = client
                .get(format!("{server}{WORKSPACES_PATH}"))
                .timeout(NAMES_DEADLINE)
                .send();
            let Ok(response) = request.await else {
                return;
            };
            if !response.status().is_success() {
                return;
            }
            let body = response.json::<Value>().await.unwrap_or_default();
            lock(&names).extend(workspace_names(&body));
        });
    }

    /// Before any notification: whose stream this is changes.
    fn forget(&self) {
        self.clear_unread();
        lock(&self.names).clear();
    }
}

/// `[{id, name}]`, or `{workspaces: [...]}`, as `/api/v1/workspaces` answers.
fn workspace_names(body: &Value) -> Vec<(String, String)> {
    let list = body
        .as_array()
        .or_else(|| body.get("workspaces").and_then(Value::as_array));
    list.into_iter()
        .flatten()
        .filter_map(|workspace| {
            let id = workspace.get("id")?.as_str().filter(|id| !id.is_empty())?;
            let name = workspace.get("name").and_then(Value::as_str);
            Some((id.to_string(), name.unwrap_or_default().to_string()))
        })
        .collect()
}

impl Events for Notifier {
    fn on_event(&self, event: &Value) {
        let muted = lock(&self.muted).workspaces.clone();
        let names = lock(&self.names).clone();
        let workspace_name = |id: &str| names.get(id).cloned().unwrap_or_default();
        let options = MapOptions {
            muted_workspaces: &muted,
            workspace_name: Some(&workspace_name),
        };
        let Some(notification) = map_event_to_notification(event, &options) else {
            return;
        };
        if lock(&self.self_actions).is_recent_self_action(event) {
            return;
        }
        // Mapped, so it names a workspace. One not in the list means the list
        // is stale: refreshed for next time, not waited for.
        let workspace = event["payload"]["workspaceId"].as_str().unwrap_or_default();
        if !names.contains_key(workspace) {
            self.refresh_names();
        }
        let allow = lock(&self.shown).allow(&notification.tag);
        if !allow || !self.desk.show(&notification) {
            return;
        }
        let mut unread = lock(&self.unread);
        let (count, generation) = (unread.counter.increment(), unread.changed());
        drop(unread);
        self.desk.set_unread(count, generation);
        self.desk
            .touch_workspace(workspace, &workspace_name(workspace));
    }
}

/// The route a notification's click carries, when the click is on the
/// notification itself.
pub fn tapped(action: &str, extra: Option<&HashMap<String, Value>>) -> Option<String> {
    let route = extra?.get(ROUTE_KEY)?.as_str()?;
    (action == "tap").then(|| route.to_string())
}

/// The stream and its notifications, following the active profile.
pub struct Feed {
    pub stream: EventStream,
    pub notifier: Arc<Notifier>,
}

impl Watcher for Feed {
    fn start(&self) {
        self.stream.start();
        self.notifier.refresh_names();
    }

    fn stop(&self) {
        self.stream.stop();
        self.notifier.clear_unread();
    }

    fn switching(&self) {
        self.stream.stop();
    }

    fn switched(&self, muted: Muted) {
        self.notifier.forget();
        self.notifier.set_muted(muted);
        self.start();
    }

    fn muted(&self, muted: Muted) {
        self.notifier.set_muted(muted);
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::events::tests::until;
    use crate::network::tests::network;
    use crate::test_server::{reply, send, Step, TestServer};
    use serde_json::json;
    use std::sync::atomic::{AtomicU64, Ordering};

    #[derive(Default)]
    pub(crate) struct FakeDesk {
        pub shown: Mutex<Vec<Notification>>,
        pub routes: Mutex<Vec<String>>,
        pub unread: Mutex<Vec<u32>>,
        pub generations: Mutex<Vec<u64>>,
        pub touched: Mutex<Vec<(String, String)>>,
        pub unsupported: bool,
    }

    impl Desk for FakeDesk {
        fn show(&self, notification: &Notification) -> bool {
            lock(&self.shown).push(notification.clone());
            !self.unsupported
        }
        fn navigate(&self, route: &str) {
            lock(&self.routes).push(route.to_string());
        }
        fn set_unread(&self, count: u32, generation: u64) {
            lock(&self.unread).push(count);
            lock(&self.generations).push(generation);
        }
        fn touch_workspace(&self, id: &str, name: &str) {
            lock(&self.touched).push((id.to_string(), name.to_string()));
        }
    }

    #[derive(Default)]
    struct ManualClock(AtomicU64);

    impl Clock for ManualClock {
        fn now_ms(&self) -> u64 {
            self.0.load(Ordering::SeqCst)
        }
    }

    fn workspaces(_: &crate::test_server::Seen) -> Vec<Step> {
        reply(
            "200 OK",
            &["content-type: application/json"],
            r#"[{"id":"w1","name":"Alpha"},{"id":"w2"},{"name":"no id"}]"#,
        )
    }

    struct Rig {
        notifier: Notifier,
        desk: Arc<FakeDesk>,
        clock: Arc<ManualClock>,
        server: TestServer,
        _dir: tempfile::TempDir,
    }

    async fn rig_with(desk: FakeDesk, script: fn(&crate::test_server::Seen) -> Vec<Step>) -> Rig {
        let server = TestServer::start(script).await;
        let dir = tempfile::tempdir().unwrap();
        let desk = Arc::new(desk);
        let clock = Arc::new(ManualClock::default());
        let notifier = Notifier::with_clock(
            Arc::new(network(dir.path(), &server.url)),
            desk.clone(),
            clock.clone(),
        );
        Rig {
            notifier,
            desk,
            clock,
            server,
            _dir: dir,
        }
    }

    async fn rig() -> Rig {
        rig_with(FakeDesk::default(), workspaces).await
    }

    fn muted(generation: u64, workspaces: &[&str]) -> Muted {
        Muted {
            workspaces: workspaces.iter().map(|w| w.to_string()).collect(),
            generation,
        }
    }

    pub(crate) fn reply_event(task: &str, sender: &str, message: &str) -> Value {
        json!({"type": "reply.received", "payload": {
            "id": task, "workspaceId": "w1", "title": "Ship it",
            "messages": [{"id": message, "sender": sender}],
        }})
    }

    fn titles(desk: &FakeDesk) -> Vec<String> {
        lock(&desk.shown).iter().map(|n| n.title.clone()).collect()
    }

    #[tokio::test]
    async fn notifies_an_agent_reply_with_the_workspace_name_once_known() {
        let rig = rig().await;
        rig.notifier.on_event(&reply_event("t1", "agent", "m1"));
        assert_eq!(titles(&rig.desk), ["Reply on: Ship it"]);
        // The name was not known yet: the body falls back, and the list is asked for.
        assert_eq!(lock(&rig.desk.shown)[0].body, "AgentRQ");
        assert_eq!(lock(&rig.desk.shown)[0].route, "/workspaces/w1/tasks/t1");
        assert!(until(5_000, &|| lock(&rig.notifier.names).contains_key("w1")).await);
        assert_eq!(rig.server.requests()[0].target, WORKSPACES_PATH);

        rig.notifier.on_event(&reply_event("t1", "agent", "m2"));
        assert_eq!(lock(&rig.desk.shown)[1].body, "Alpha");
        assert_eq!(*lock(&rig.desk.unread), [1, 2]);
        assert_eq!(
            *lock(&rig.desk.touched),
            [("w1".into(), String::new()), ("w1".into(), "Alpha".into())]
        );
        // Known now, so not asked for again.
        assert_eq!(rig.server.requests().len(), 1);
        assert_eq!(lock(&rig.notifier.names).get("w2").unwrap(), "");
    }

    #[tokio::test]
    async fn the_human_and_muted_workspaces_and_telemetry_are_silent() {
        let rig = rig().await;
        rig.notifier.on_event(&reply_event("t1", "human", "m1"));
        rig.notifier
            .on_event(&json!({"type": "task.updated", "payload": {}}));
        let mut thought = reply_event("t1", "agent", "m2");
        thought["payload"]["messages"][0]["metadata"] = json!({"type": "agent_thought"});
        rig.notifier.on_event(&thought);
        rig.notifier.set_muted(muted(1, &["w1"]));
        rig.notifier.on_event(&reply_event("t1", "agent", "m3"));
        assert!(titles(&rig.desk).is_empty());
    }

    #[tokio::test]
    async fn dedupes_a_tag_for_ten_seconds() {
        let rig = rig().await;
        let created = json!({"type": "task.created", "payload": {
            "id": "t9", "workspaceId": "w1", "title": "New", "createdBy": "agent",
        }});
        rig.notifier.on_event(&created);
        rig.notifier.on_event(&created);
        assert_eq!(titles(&rig.desk), ["New task: New"]);
        rig.clock.0.store(10_000, Ordering::SeqCst);
        rig.notifier.on_event(&created);
        assert_eq!(titles(&rig.desk).len(), 2);
    }

    #[tokio::test]
    async fn the_echo_of_this_apps_own_reply_is_silent_but_the_agents_answer_is_not() {
        let rig = rig().await;
        rig.notifier
            .proxied("POST", "/api/v1/workspaces/w1/tasks/t1/reply");
        // The echo carries no sender, so it reads as the agent's reply.
        let mut echo = reply_event("t1", "", "m1");
        echo["payload"]["messages"] = json!([]);
        rig.notifier.on_event(&echo);
        assert!(titles(&rig.desk).is_empty());
        rig.notifier.on_event(&reply_event("t1", "agent", "m2"));
        assert_eq!(titles(&rig.desk).len(), 1);
    }

    #[tokio::test]
    async fn an_unsupported_system_counts_nothing() {
        let desk = FakeDesk {
            unsupported: true,
            ..FakeDesk::default()
        };
        let rig = rig_with(desk, workspaces).await;
        rig.notifier.on_event(&reply_event("t1", "agent", "m1"));
        assert_eq!(titles(&rig.desk).len(), 1);
        assert!(lock(&rig.desk.unread).is_empty());
    }

    #[tokio::test]
    async fn a_click_clears_the_count_and_opens_the_task() {
        let rig = rig().await;
        rig.notifier.on_event(&reply_event("t1", "agent", "m1"));
        rig.notifier.clicked("/workspaces/w1/tasks/t1");
        assert_eq!(*lock(&rig.desk.routes), ["/workspaces/w1/tasks/t1"]);
        assert_eq!(*lock(&rig.desk.unread), [1, 0]);
        // Numbered in the order the count changed, for a Desk that gets them
        // in another.
        assert_eq!(*lock(&rig.desk.generations), [1, 2]);
    }

    /// A Desk that waits on a stand-in main thread, which takes the
    /// Notifier's locks as the focus handler does. A Desk call made with one
    /// of them held would wait forever.
    #[derive(Default)]
    struct MainThreadDesk {
        notifier: std::sync::OnceLock<std::sync::Weak<Notifier>>,
        calls: AtomicU64,
    }

    impl MainThreadDesk {
        fn wait_for_main_thread(&self) {
            let notifier = self.notifier.get().and_then(std::sync::Weak::upgrade);
            let notifier = notifier.expect("the notifier is alive");
            let (done, finished) = std::sync::mpsc::channel();
            std::thread::spawn(move || {
                drop(lock(&notifier.unread));
                drop(lock(&notifier.shown));
                let _ = done.send(());
            });
            let free = finished.recv_timeout(Duration::from_secs(5)).is_ok();
            assert!(free, "the Desk was called with a Notifier lock held");
            self.calls.fetch_add(1, Ordering::SeqCst);
        }
    }

    impl Desk for MainThreadDesk {
        fn show(&self, _notification: &Notification) -> bool {
            self.wait_for_main_thread();
            true
        }
        fn navigate(&self, _route: &str) {
            self.wait_for_main_thread();
        }
        fn set_unread(&self, _count: u32, _generation: u64) {
            self.wait_for_main_thread();
        }
        fn touch_workspace(&self, _id: &str, _name: &str) {
            self.wait_for_main_thread();
        }
    }

    #[tokio::test]
    async fn never_calls_the_desk_with_a_lock_held() {
        let server = TestServer::start(workspaces).await;
        let dir = tempfile::tempdir().unwrap();
        let desk = Arc::new(MainThreadDesk::default());
        let notifier = Arc::new(Notifier::new(
            Arc::new(network(dir.path(), &server.url)),
            desk.clone(),
        ));
        let _ = desk.notifier.set(Arc::downgrade(&notifier));
        // show, set_unread and touch_workspace; then set_unread and navigate.
        notifier.on_event(&reply_event("t1", "agent", "m1"));
        notifier.clicked("/workspaces/w1/tasks/t1");
        assert_eq!(desk.calls.load(Ordering::SeqCst), 5);
    }

    #[tokio::test]
    async fn names_survive_a_server_that_fails_or_cannot_be_reached() {
        let rig = rig_with(FakeDesk::default(), |_| reply("500 Oops", &[], "")).await;
        rig.notifier.refresh_names();
        assert!(until(5_000, &|| rig.server.requests().len() == 1).await);

        let dir = tempfile::tempdir().unwrap();
        let notifier = Notifier::new(
            Arc::new(network(dir.path(), "not a url")),
            Arc::new(FakeDesk::default()),
        );
        notifier.refresh_names();
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(lock(&notifier.names).is_empty());
    }

    #[test]
    fn reads_both_shapes_of_the_workspace_list() {
        let wrapped = json!({"workspaces": [{"id": "w1", "name": "A"}, {"id": ""}]});
        assert_eq!(workspace_names(&wrapped), [("w1".into(), "A".into())]);
        assert!(workspace_names(&json!(null)).is_empty());
    }

    #[test]
    fn a_tap_carries_its_route_and_nothing_else_does() {
        let extra = HashMap::from([(ROUTE_KEY.to_string(), json!("/workspaces/w/tasks/t"))]);
        assert_eq!(
            tapped("tap", Some(&extra)).as_deref(),
            Some("/workspaces/w/tasks/t")
        );
        assert_eq!(tapped("snooze", Some(&extra)), None);
        assert_eq!(tapped("tap", Some(&HashMap::new())), None);
        assert_eq!(tapped("tap", None), None);
    }

    #[tokio::test]
    async fn the_feed_follows_the_profile() {
        let server = TestServer::start(|seen| {
            if seen.target == WORKSPACES_PATH {
                return workspaces(seen);
            }
            vec![
                send("HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\n\r\n"),
                send(
                    r#"data: {"type":"task.created","payload":{"id":"t1","workspaceId":"w1","title":"T","createdBy":"agent"}}"#,
                ),
                // The stream ends, so a running client reads it again in a second.
                send("\n\n"),
            ]
        })
        .await;
        let dir = tempfile::tempdir().unwrap();
        let network = Arc::new(network(dir.path(), &server.url));
        let desk = Arc::new(FakeDesk::default());
        let notifier = Arc::new(Notifier::new(network.clone(), desk.clone()));
        let feed = Feed {
            stream: EventStream::spawn(network, notifier.clone()),
            notifier: notifier.clone(),
        };
        feed.switched(muted(1, &["other"]));
        assert!(until(5_000, &|| titles(&desk).len() == 1).await);
        assert_eq!(lock(&notifier.muted).workspaces, ["other"]);

        // Leaving the profile stops its stream; the count waits for `switched`.
        feed.switching();
        tokio::time::sleep(Duration::from_millis(1_500)).await;
        let streams = server
            .requests()
            .iter()
            .filter(|r| r.target != WORKSPACES_PATH)
            .count();
        assert_eq!(streams, 1);
        assert_eq!(*lock(&desk.unread), [1]);

        feed.muted(muted(2, &["w1"]));
        assert_eq!(lock(&notifier.muted).workspaces, ["w1"]);
        feed.stop();
        assert_eq!(*lock(&desk.unread), [1, 0]);
    }

    #[tokio::test]
    async fn keeps_the_newest_mutes_whatever_order_they_arrive_in() {
        let rig = crate::profiles::tests::rig();
        rig.profiles.set_muted("w1", true).await.unwrap();
        rig.profiles.set_muted("w2", true).await.unwrap();
        let notifier = Notifier::new(rig.network.clone(), Arc::new(FakeDesk::default()));
        let mutes = lock(&rig.watcher.mutes).clone();
        for muted in mutes.into_iter().rev() {
            notifier.set_muted(muted);
        }
        assert_eq!(lock(&notifier.muted).workspaces, rig.profiles.muted());
        assert_eq!(rig.profiles.muted(), ["w1", "w2"]);
    }
}
