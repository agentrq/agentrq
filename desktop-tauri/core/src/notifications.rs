// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Turning event-stream traffic into native notifications.
//!
//! Web Push cannot work in a desktop shell: there is no push service behind it.
//! So the desktop app produces the same user-facing behaviour from the SSE
//! stream it already has. The wording, the trigger rules and the click
//! destinations deliberately mirror the backend's push controller
//! (`backend/internal/controller/push/push.go`) so a user who moves between the
//! browser and the desktop app sees the same notifications for the same events.
//!
//! One rule the push controller does not need: telling the human's own message
//! apart from the agent's. The payload **does** carry the task's messages (the
//! forwarder loads them before publishing), so [`last_message_sender`] answers
//! it directly, and that is what [`should_notify`] gates on.
//!
//! [`task_id_from_self_action_request`] and [`SelfActionGate`] additionally
//! cover this instance's own reply/respond calls, the narrower case of an echo
//! arriving before the write is visible. It is a backstop, not the rule: keyed
//! on time alone it swallowed agents that answered quickly.
//!
//! Rust notes: events are `serde_json::Value`, because the payload is whatever
//! the server published and every rule here tolerates a missing or odd field.
//! A field JS would read as `undefined` or `null` is absent or `Value::Null`,
//! and one it would stringify is stringified by [`js_string`]. The clock and
//! the badge are traits, so the windows can be tested without waiting.

use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

/// Event types that can produce a notification. Others are UI-only signals.
///
/// A status change is announced from `task.status` alone: the server publishes
/// it only for the agent's own change. `status.updated` and `task.updated`
/// cannot say who changed the status, so judging them by who created the task
/// or spoke last dropped an agent's change on a human's task, announced the
/// human's own change on an agent's, and would now repeat `task.status`.
pub const NOTIFIABLE_TYPES: [&str; 3] = ["task.created", "task.status", "reply.received"];

/// A notification to show. `route` is where a click goes; `tag` is what the
/// dedupe gate keys on.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Notification {
    pub title: String,
    pub body: String,
    pub route: String,
    pub tag: String,
}

/// JS's `String(value ?? '')`.
pub fn js_string(value: Option<&Value>) -> String {
    match value {
        None | Some(Value::Null) => String::new(),
        Some(Value::String(text)) => text.clone(),
        Some(other) => other.to_string(),
    }
}

/// Matches the backend's truncation lengths so titles read identically.
///
/// Lengths are in UTF-16 code units, as JS counts them.
pub fn truncate(text: &str, max: usize) -> String {
    let units: Vec<u16> = text.encode_utf16().collect();
    if units.len() > max {
        format!("{}…", String::from_utf16_lossy(&units[..max]))
    } else {
        text.to_string()
    }
}

/// A field that is a non-empty string, which is all JS's truthiness leaves of
/// an id.
fn id_field<'a>(object: &'a Value, key: &str) -> Option<&'a str> {
    object
        .get(key)
        .and_then(Value::as_str)
        .filter(|id| !id.is_empty())
}

/// The task an event is about: `task.status` names it, the others carry it.
fn task_id_of(event: &Value) -> Option<&str> {
    let task = &event["payload"];
    if event["type"] == "task.status" {
        id_field(task, "taskId")
    } else {
        id_field(task, "id")
    }
}

/// Agent telemetry metadata types that never notify. A notice still does.
const SILENT_TELEMETRY_TYPES: [&str; 3] = ["agent_thought", "agent_plan", "agent_usage"];

/// The newest message on a task, when it has one.
fn last_message(task: &Value) -> Option<&Value> {
    task.get("messages")?.as_array()?.last()
}

/// The metadata type of the newest message on a task, or '' when it has none.
fn last_message_type(task: &Value) -> String {
    js_string(last_message(task).and_then(|m| m.get("metadata")?.get("type")))
}

/// The id of the newest message on a task, or '' when the payload carries none.
///
/// Used as a reply's notification tag, so the dedupe gate collapses a message
/// published twice into the one piece of news it is.
pub fn last_message_id(task: &Value) -> String {
    js_string(last_message(task).and_then(|m| m.get("id")))
}

/// Who wrote the newest message on a task, or '' when the payload carries none.
pub fn last_message_sender(task: &Value) -> String {
    js_string(last_message(task).and_then(|m| m.get("sender")))
}

/// Should this event notify at all?
///
/// Two rules, both inherited from the push controller. Only the agent's actions
/// are worth interrupting someone for: being told about your own click is
/// noise, not news. And a muted workspace stays silent.
pub fn should_notify<S: AsRef<str>>(event: &Value, muted_workspaces: &[S]) -> bool {
    let Some(kind) = event.get("type").and_then(Value::as_str) else {
        return false;
    };
    if !NOTIFIABLE_TYPES.contains(&kind) {
        return false;
    }

    let task = &event["payload"];
    if !task.is_object() {
        return false;
    }
    let Some(workspace_id) = id_field(task, "workspaceId") else {
        return false;
    };
    if task_id_of(event).is_none() {
        return false;
    }
    if muted_workspaces
        .iter()
        .any(|id| id.as_ref() == workspace_id)
    {
        return false;
    }

    // The agent's by construction, whoever created the task or spoke last.
    if kind == "task.status" {
        return match task.get("to") {
            Some(Value::String(to)) => !to.is_empty() && task.get("from") != task.get("to"),
            _ => false,
        };
    }

    // Who just spoke, when the payload says, and it does for every event the
    // forwarder publishes. A reply is not news because it is a reply; it is news
    // because somebody else wrote it. Replying from a browser, a phone or Slack
    // while this app is open used to announce your own message back at you,
    // because `reply.received` was simply assumed to mean "the agent".
    //
    // The fallback keeps the old reading for a payload carrying no messages:
    // `createdBy` for a task event, and the agent for a reply.
    let sender = last_message_sender(task);
    let actor = if !sender.is_empty() {
        sender
    } else if kind == "reply.received" {
        "agent".to_string()
    } else {
        js_string(task.get("createdBy"))
    };
    if actor != "agent" {
        return false;
    }

    // The agent's reasoning, plan and usage counters are messages too, and each
    // one republishes its task. They are the agent working, not saying anything.
    !(kind == "reply.received"
        && SILENT_TELEMETRY_TYPES.contains(&last_message_type(task).as_str()))
}

/// What [`map_event_to_notification`] needs beyond the event.
#[derive(Default)]
pub struct MapOptions<'a> {
    pub muted_workspaces: &'a [String],
    /// Resolves a display name; falls back to the product name rather than
    /// showing a raw id.
    pub workspace_name: Option<&'a dyn Fn(&str) -> String>,
}

/// Build the notification for an event, or `None` when it should not fire.
pub fn map_event_to_notification(event: &Value, options: &MapOptions) -> Option<Notification> {
    if !should_notify(event, options.muted_workspaces) {
        return None;
    }

    let task = &event["payload"];
    let workspace_id = js_string(task.get("workspaceId"));
    // `should_notify` has already refused an event that names no task.
    let task_id = task_id_of(event).unwrap_or_default();
    let workspace = options
        .workspace_name
        .map(|name| name(&workspace_id))
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| "AgentRQ".to_string());
    let route = format!("/workspaces/{workspace_id}/tasks/{task_id}");
    let title = js_string(task.get("title"));

    Some(match event["type"].as_str().unwrap_or_default() {
        "task.created" => Notification {
            title: format!("New task: {}", truncate(&title, 60)),
            body: workspace,
            route,
            tag: format!("task-create-{}", js_string(task.get("id"))),
        },

        "task.status" => {
            let to = js_string(task.get("to"));
            Notification {
                title: format!("Task {}: {}", to.to_uppercase(), truncate(&title, 50)),
                body: workspace,
                route,
                // By the new status too: an agent going ongoing then completed in
                // one breath is two pieces of news, not one deduped away.
                tag: format!("task-status-{}-{}", js_string(task.get("taskId")), to),
            }
        }

        // reply.received. The reply text the browser notification shows is not
        // used here (the workspace name is the honest stand-in), but the
        // message id is, as the tag.
        _ => {
            let message_id = last_message_id(task);
            Notification {
                title: format!("Reply on: {}", truncate(&title, 55)),
                body: workspace,
                route,
                tag: if message_id.is_empty() {
                    format!("reply-{}", js_string(task.get("id")))
                } else {
                    message_id
                },
            }
        }
    })
}

/// The task ID a reply/respond request names, or `None` when the request is
/// neither.
///
/// These are the two ways this desktop instance's own action reaches the
/// server, a human typing a reply or clicking allow/deny, and both land back
/// on this same SSE stream, indistinguishable from an agent's unless the
/// outgoing request is recognised at the point it is proxied.
pub fn task_id_from_self_action_request<'a>(method: &str, pathname: &'a str) -> Option<&'a str> {
    if !method.eq_ignore_ascii_case("POST") {
        return None;
    }
    let rest = pathname.strip_prefix("/api/v1/workspaces/")?;
    // `/api/v1/workspaces/<ws>/tasks/<id>/(reply|respond)`, every part
    // non-empty and nothing after it.
    let mut parts = rest.split('/');
    match (
        parts.next(),
        parts.next(),
        parts.next(),
        parts.next(),
        parts.next(),
    ) {
        (Some(workspace), Some("tasks"), Some(id), Some("reply" | "respond"), None)
            if !workspace.is_empty() && !id.is_empty() =>
        {
            Some(id)
        }
        _ => None,
    }
}

/// How long a task stays muted after this desktop sends a reply/respond for it.
pub const SELF_ACTION_WINDOW_MS: u64 = 10000;

/// Event types that can be the SSE echo of a reply/respond this desktop
/// instance just sent and would notify: a message create republishes as
/// `reply.received`.
///
/// Deliberately not "every notifiable type": `task.created` and `task.status`
/// are never the result of sending a reply, so muting them just because a reply
/// on the same task happened moments ago would drop a genuine, unrelated
/// notification instead of the self-sent echo this gate exists to catch.
const SELF_ECHO_TYPES: [&str; 1] = ["reply.received"];

/// Milliseconds since some fixed point, for the windows below.
pub trait Clock: Send + Sync {
    fn now_ms(&self) -> u64;
}

/// The wall clock.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now_ms(&self) -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_or(0, |elapsed| elapsed.as_millis() as u64)
    }
}

impl<C: Clock + ?Sized> Clock for Arc<C> {
    fn now_ms(&self) -> u64 {
        (**self).now_ms()
    }
}

/// Removes entries whose timestamp is at least `window_ms` old.
///
/// Shared by the two windowed-key trackers, which differ in what they key on
/// and when they check but not in how a window expires.
fn prune_stale(map: &mut HashMap<String, u64>, window_ms: u64, at: u64) {
    map.retain(|_, timestamp| at.saturating_sub(*timestamp) < window_ms);
}

/// Tracks tasks this desktop instance just acted on itself, so the
/// notification that same action produces on the SSE stream a moment later can
/// be suppressed.
///
/// A time window rather than a one-shot latch consumed by the next event: a
/// genuine, later agent reply on the same task must still notify, and an
/// unrelated event of a different type arriving first must not use up the mute
/// before the actual echo of this action arrives; see [`SELF_ECHO_TYPES`].
///
/// Pruned on every call, not just a matching one: `mark_self` fires on every
/// reply/respond regardless of what (if anything) the stream echoes back for
/// it, so a check keyed only to the same task id could otherwise never run for
/// a stale entry and the map would grow for the life of the process.
pub struct SelfActionGate<C: Clock> {
    window_ms: u64,
    clock: C,
    marked_at: HashMap<String, u64>,
}

impl<C: Clock> SelfActionGate<C> {
    pub fn new(clock: C) -> Self {
        Self::with_window(SELF_ACTION_WINDOW_MS, clock)
    }

    pub fn with_window(window_ms: u64, clock: C) -> Self {
        Self {
            window_ms,
            clock,
            marked_at: HashMap::new(),
        }
    }

    /// A missing or empty id marks nothing.
    pub fn mark_self(&mut self, task_id: Option<&str>) {
        let at = self.clock.now_ms();
        prune_stale(&mut self.marked_at, self.window_ms, at);
        if let Some(id) = task_id.filter(|id| !id.is_empty()) {
            self.marked_at.insert(id.to_string(), at);
        }
    }

    /// True when this event is this desktop's own reply/respond echoing back.
    ///
    /// Takes the whole event, because the payload settles it. The window alone
    /// could not: an agent answering inside ten seconds, which is most of them,
    /// landed in the mute this desktop had just armed for its own message, and
    /// its reply was dropped. A status change arriving later, after the agent
    /// had done some work, fell outside the window and still notified, which is
    /// exactly the difference somebody reported.
    pub fn is_recent_self_action(&mut self, event: &Value) -> bool {
        prune_stale(&mut self.marked_at, self.window_ms, self.clock.now_ms());
        let is_echo_type = event
            .get("type")
            .and_then(Value::as_str)
            .is_some_and(|kind| SELF_ECHO_TYPES.contains(&kind));
        if !is_echo_type {
            return false;
        }
        let payload = &event["payload"];
        // The agent wrote the last message, so this is not this desktop's echo,
        // whatever the clock says.
        if last_message_sender(payload) == "agent" {
            return false;
        }
        id_field(payload, "id").is_some_and(|id| self.marked_at.contains_key(id))
    }
}

/// How long a tag suppresses a repeat.
pub const DEDUPE_WINDOW_MS: u64 = 10000;

/// Suppress duplicate notifications by tag.
///
/// A single task creation reaches the stream twice: the REST handler publishes
/// `task.created` directly, and the CRUD-event consumer publishes it again from
/// the same write. The renderer does not care, but firing two identical native
/// notifications for one event is plainly wrong.
///
/// The `tag` field carries the same value in both copies, which is exactly what
/// the browser uses to collapse duplicate web-push notifications; this is that
/// mechanism, applied on our side.
///
/// A time window rather than an unbounded set: a genuine second event with the
/// same tag much later (a task edited twice, say) should still notify, and the
/// map must not grow forever.
pub struct NotificationGate<C: Clock> {
    window_ms: u64,
    clock: C,
    seen: HashMap<String, u64>,
}

impl<C: Clock> NotificationGate<C> {
    pub fn new(clock: C) -> Self {
        Self::with_window(DEDUPE_WINDOW_MS, clock)
    }

    pub fn with_window(window_ms: u64, clock: C) -> Self {
        Self {
            window_ms,
            clock,
            seen: HashMap::new(),
        }
    }

    /// True when this notification should be shown.
    pub fn allow(&mut self, tag: &str) -> bool {
        let at = self.clock.now_ms();
        prune_stale(&mut self.seen, self.window_ms, at);

        if self.seen.contains_key(tag) {
            return false;
        }
        self.seen.insert(tag.to_string(), at);
        true
    }
}

/// Where the unread count is shown: the dock badge or the taskbar.
pub trait BadgeSink {
    fn set_badge(&self, count: u32);
}

/// Unread count behind the dock badge and taskbar overlay.
///
/// Counting is per notification raised and cleared when the user looks at the
/// app, which is the behaviour a badge is understood to have: it answers "is
/// there anything new", not "how many events have ever occurred".
pub struct UnreadCounter<B: BadgeSink> {
    sink: B,
    count: u32,
}

impl<B: BadgeSink> UnreadCounter<B> {
    pub fn new(sink: B) -> Self {
        Self { sink, count: 0 }
    }

    pub fn increment(&mut self) -> u32 {
        self.count += 1;
        self.sink.set_badge(self.count);
        self.count
    }

    pub fn clear(&mut self) -> u32 {
        if self.count == 0 {
            return 0;
        }
        self.count = 0;
        self.sink.set_badge(self.count);
        self.count
    }

    pub fn value(&self) -> u32 {
        self.count
    }
}

/// A taskbar overlay: a count and the description a screen reader reads.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Overlay {
    pub count: i64,
    pub description: String,
}

/// What the badge and overlay should show.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BadgeState {
    pub badge: String,
    pub overlay: Option<Overlay>,
}

/// Windows has no numeric dock badge, so the count is rendered as a taskbar
/// overlay description instead; macOS and Linux take the number directly.
///
/// Returns what should be shown, so the choice is testable without a window.
pub fn badge_for(count: i64, platform: &str) -> BadgeState {
    if count <= 0 {
        return BadgeState {
            badge: String::new(),
            overlay: None,
        };
    }
    if platform == "win32" {
        return BadgeState {
            badge: String::new(),
            overlay: Some(Overlay {
                count,
                description: format!(
                    "{count} unread notification{}",
                    if count == 1 { "" } else { "s" }
                ),
            }),
        };
    }
    BadgeState {
        badge: count.to_string(),
        overlay: None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::RefCell;
    use std::rc::Rc;
    use std::sync::atomic::{AtomicU64, Ordering};

    /// A clock the test moves by hand.
    #[derive(Default)]
    struct ManualClock(AtomicU64);

    impl ManualClock {
        fn set(&self, ms: u64) {
            self.0.store(ms, Ordering::SeqCst);
        }
    }

    impl Clock for ManualClock {
        fn now_ms(&self) -> u64 {
            self.0.load(Ordering::SeqCst)
        }
    }

    /// Records every badge the app was asked to show.
    #[derive(Default, Clone)]
    struct RecordingBadge(Rc<RefCell<Vec<u32>>>);

    impl BadgeSink for RecordingBadge {
        fn set_badge(&self, count: u32) {
            self.0.borrow_mut().push(count);
        }
    }

    /// Overlay `over` onto `base`; a `null` removes the key, which is how JS's
    /// `undefined` reads.
    fn merged(mut base: Value, over: Value) -> Value {
        for (key, value) in over.as_object().unwrap() {
            if value.is_null() {
                base.as_object_mut().unwrap().remove(key);
            } else {
                base[key] = value.clone();
            }
        }
        base
    }

    /// A task view as the event stream delivers it.
    fn task(over: Value) -> Value {
        merged(
            json!({
                "id": "0hua6QI7nXN",
                "workspaceId": "0ZzhYQG2qtl",
                "title": "Ship the desktop app",
                "status": "ongoing",
                "createdBy": "agent",
            }),
            over,
        )
    }

    fn event(kind: &str, over: Value) -> Value {
        json!({"type": kind, "payload": task(over)})
    }

    /// An agent moving a task, as the server publishes it: only for the agent's
    /// own change.
    fn status_change(over: Value) -> Value {
        json!({"type": "task.status", "payload": merged(json!({
            "taskId": "0hua6QI7nXN",
            "workspaceId": "0ZzhYQG2qtl",
            "title": "Ship the desktop app",
            "from": "ongoing",
            "to": "completed",
        }), over)})
    }

    const NO_MUTES: &[String] = &[];

    fn named(_: &str) -> String {
        "agentrq-code".to_string()
    }

    fn names() -> MapOptions<'static> {
        MapOptions {
            muted_workspaces: NO_MUTES,
            workspace_name: Some(&named),
        }
    }

    fn notify(event: &Value) -> bool {
        should_notify(event, NO_MUTES)
    }

    fn map(event: &Value) -> Option<Notification> {
        map_event_to_notification(event, &names())
    }

    fn map_bare(event: &Value) -> Option<Notification> {
        map_event_to_notification(event, &MapOptions::default())
    }

    fn mutes(ids: &[&str]) -> Vec<String> {
        ids.iter().map(|id| id.to_string()).collect()
    }

    // truncate

    #[test]
    fn leaves_short_text_alone() {
        assert_eq!(truncate("short", 60), "short");
    }

    #[test]
    fn cuts_long_text_and_marks_it() {
        assert_eq!(
            truncate(&"x".repeat(70), 60),
            format!("{}…", "x".repeat(60))
        );
    }

    #[test]
    fn handles_missing_text() {
        assert_eq!(truncate(&js_string(None), 10), "");
        assert_eq!(truncate(&js_string(Some(&Value::Null)), 10), "");
    }

    // shouldNotify

    #[test]
    fn accepts_the_event_types_worth_interrupting_someone_for() {
        for kind in NOTIFIABLE_TYPES {
            let event = if kind == "task.status" {
                status_change(json!({}))
            } else {
                event(kind, json!({}))
            };
            assert!(notify(&event), "{kind}");
        }
    }

    #[test]
    fn ignores_ui_only_signals() {
        // agent.connected drives a status dot; task.deleted has nothing to show.
        assert!(!notify(&event("agent.connected", json!({}))));
        assert!(!notify(&event("task.deleted", json!({}))));
    }

    #[test]
    fn ignores_the_users_own_actions() {
        // Mirrors the push controller, which notifies only on agent activity:
        // being told about your own click is noise, not news.
        assert!(!notify(&event(
            "task.created",
            json!({"createdBy": "human"})
        )));
    }

    // The bug: an agent moving a task the human created, or one where the human
    // spoke last, was judged by them and dropped. task.status is the agent's
    // own change, so neither matters.
    #[test]
    fn announces_an_agents_status_change_whoever_created_the_task_or_spoke_last() {
        assert!(notify(&status_change(json!({}))));
        assert!(notify(&status_change(json!({
            "createdBy": "human",
            "messages": [{"id": "m1", "sender": "human"}],
        }))));
    }

    #[test]
    fn says_nothing_for_a_status_that_did_not_change_or_names_none() {
        assert!(!notify(&status_change(
            json!({"from": "ongoing", "to": "ongoing"})
        )));
        assert!(!notify(&status_change(json!({"to": ""}))));
        assert!(!notify(&status_change(json!({"taskId": null}))));
    }

    // They cannot say who changed the status. Announcing an agent's change is
    // task.status's job, so these would only repeat it, or tell the human about
    // their own click on a task the agent spoke last on.
    #[test]
    fn leaves_status_updated_and_task_updated_to_task_status() {
        assert!(!notify(&event(
            "status.updated",
            json!({"status": "completed"})
        )));
        assert!(!notify(&event("task.updated", json!({}))));
    }

    #[test]
    fn always_notifies_for_a_reply_which_only_an_agent_sends() {
        assert!(notify(&event(
            "reply.received",
            json!({"createdBy": "human"})
        )));
    }

    #[test]
    fn stays_silent_for_a_muted_workspace() {
        let created = event("task.created", json!({}));
        assert!(!should_notify(&created, &mutes(&["0ZzhYQG2qtl"])));
        assert!(should_notify(&created, &mutes(&["other"])));
        assert!(!should_notify(
            &status_change(json!({})),
            &mutes(&["0ZzhYQG2qtl"])
        ));
    }

    #[test]
    fn rejects_malformed_events_rather_than_raising_an_empty_notification() {
        assert!(!notify(&Value::Null));
        assert!(!notify(&json!({})));
        assert!(!notify(&json!({"type": "task.created"})));
        assert!(!notify(&json!({"type": "task.created", "payload": "nope"})));
        assert!(!notify(&event(
            "task.created",
            json!({"workspaceId": null})
        )));
        assert!(!notify(&event("task.created", json!({"id": null}))));
    }

    // mapEventToNotification

    #[test]
    fn words_a_new_task_the_way_the_browser_notification_does() {
        assert_eq!(
            map(&event("task.created", json!({}))),
            Some(Notification {
                title: "New task: Ship the desktop app".into(),
                body: "agentrq-code".into(),
                route: "/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN".into(),
                tag: "task-create-0hua6QI7nXN".into(),
            })
        );
    }

    #[test]
    fn words_a_status_change_with_the_new_status_in_caps() {
        assert_eq!(
            map(&status_change(json!({}))),
            Some(Notification {
                title: "Task COMPLETED: Ship the desktop app".into(),
                body: "agentrq-code".into(),
                route: "/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN".into(),
                tag: "task-status-0hua6QI7nXN-completed".into(),
            })
        );
    }

    #[test]
    fn keeps_two_quick_changes_apart_and_collapses_one_published_twice() {
        let mut gate = NotificationGate::new(Arc::new(ManualClock::default()));
        let ongoing = map(&status_change(
            json!({"from": "notstarted", "to": "ongoing"}),
        ))
        .unwrap();
        let completed = map(&status_change(json!({}))).unwrap();

        assert!(gate.allow(&ongoing.tag));
        assert!(gate.allow(&completed.tag));
        assert!(!gate.allow(&completed.tag));
    }

    #[test]
    fn routes_every_notification_to_the_task_itself_not_the_workspace() {
        assert_eq!(
            map(&event("reply.received", json!({}))),
            Some(Notification {
                title: "Reply on: Ship the desktop app".into(),
                body: "agentrq-code".into(),
                route: "/workspaces/0ZzhYQG2qtl/tasks/0hua6QI7nXN".into(),
                tag: "reply-0hua6QI7nXN".into(),
            })
        );
    }

    #[test]
    fn truncates_long_titles_at_the_same_lengths_the_backend_uses() {
        let long = "y".repeat(100);
        assert_eq!(
            map(&event("task.created", json!({"title": long})))
                .unwrap()
                .title,
            format!("New task: {}…", "y".repeat(60))
        );
        assert_eq!(
            map(&status_change(json!({"title": long}))).unwrap().title,
            format!("Task COMPLETED: {}…", "y".repeat(50))
        );
        assert_eq!(
            map(&event("reply.received", json!({"title": long})))
                .unwrap()
                .title,
            format!("Reply on: {}…", "y".repeat(55))
        );
    }

    #[test]
    fn falls_back_to_the_product_name_rather_than_showing_a_raw_workspace_id() {
        assert_eq!(
            map_bare(&event("task.created", json!({}))).unwrap().body,
            "AgentRQ"
        );
        let empty = |_: &str| String::new();
        let options = MapOptions {
            workspace_name: Some(&empty),
            ..Default::default()
        };
        assert_eq!(
            map_event_to_notification(&event("task.created", json!({})), &options)
                .unwrap()
                .body,
            "AgentRQ"
        );
    }

    #[test]
    fn returns_nothing_for_an_event_that_should_not_notify() {
        assert_eq!(map(&event("agent.connected", json!({}))), None);
        let muted = mutes(&["0ZzhYQG2qtl"]);
        let options = MapOptions {
            muted_workspaces: &muted,
            workspace_name: Some(&named),
        };
        assert_eq!(
            map_event_to_notification(&event("task.created", json!({})), &options),
            None
        );
    }

    #[test]
    fn copes_with_a_status_that_is_missing() {
        assert_eq!(map(&status_change(json!({"to": null}))), None);
    }

    // taskIdFromSelfActionRequest

    #[test]
    fn reads_the_task_id_out_of_a_reply_request() {
        assert_eq!(
            task_id_from_self_action_request("POST", "/api/v1/workspaces/ws1/tasks/t1/reply"),
            Some("t1")
        );
    }

    #[test]
    fn reads_the_task_id_out_of_a_respond_request() {
        assert_eq!(
            task_id_from_self_action_request("POST", "/api/v1/workspaces/ws1/tasks/t1/respond"),
            Some("t1")
        );
    }

    #[test]
    fn is_case_insensitive_on_the_method_matching_how_the_shell_reports_it() {
        assert_eq!(
            task_id_from_self_action_request("post", "/api/v1/workspaces/ws1/tasks/t1/reply"),
            Some("t1")
        );
    }

    #[test]
    fn ignores_a_get_on_the_same_path() {
        // Fetching a task's detail is not sending anything.
        assert_eq!(
            task_id_from_self_action_request("GET", "/api/v1/workspaces/ws1/tasks/t1/reply"),
            None
        );
    }

    #[test]
    fn ignores_requests_that_are_not_a_reply_or_respond() {
        assert_eq!(
            task_id_from_self_action_request("POST", "/api/v1/workspaces/ws1/tasks/t1/status"),
            None
        );
        assert_eq!(
            task_id_from_self_action_request("POST", "/api/v1/workspaces/ws1/tasks"),
            None
        );
        assert_eq!(
            task_id_from_self_action_request("PATCH", "/api/v1/workspaces/ws1/tasks/t1/reply"),
            None
        );
    }

    #[test]
    fn does_not_match_a_reply_nested_under_something_else() {
        assert_eq!(
            task_id_from_self_action_request("POST", "/api/v1/workspaces/ws1/tasks/t1/reply/extra"),
            None
        );
    }

    // who the payload says spoke

    fn with_messages(kind: &str, messages: Value, over: Value) -> Value {
        json!({"type": kind, "payload": merged(json!({
            "id": "t1",
            "workspaceId": "w1",
            "title": "Ship it",
            "createdBy": "human",
            "messages": messages,
        }), over)})
    }

    // Replying from a browser, a phone or Slack while the desktop app is open.
    // The self-action gate cannot help (it only knows what *this* instance
    // sent), so the payload has to say, and it does.
    #[test]
    fn says_nothing_about_a_reply_the_human_wrote_somewhere_else() {
        let event = with_messages(
            "reply.received",
            json!([{"id": "m1", "sender": "human", "text": "any update?"}]),
            json!({}),
        );

        assert_eq!(map_bare(&event), None);
    }

    #[test]
    fn still_announces_the_agent_answering() {
        let event = with_messages(
            "reply.received",
            json!([{"id": "m1", "sender": "agent", "text": "on it"}]),
            json!({}),
        );

        assert!(map_bare(&event)
            .unwrap()
            .title
            .contains("Reply on: Ship it"));
    }

    // Without messages there is nothing to read, so the old reading stands:
    // a reply is the agent's, and a task event is judged by who created it.
    #[test]
    fn falls_back_to_the_old_reading_when_the_payload_carries_no_messages() {
        let bare = |kind: &str, created_by: Option<&str>| {
            let mut payload = json!({"id": "t1", "workspaceId": "w1", "title": "T"});
            if let Some(created_by) = created_by {
                payload["createdBy"] = json!(created_by);
            }
            json!({"type": kind, "payload": payload})
        };

        assert!(map_bare(&bare("reply.received", None)).is_some());
        assert!(map_bare(&bare("task.created", Some("agent"))).is_some());
        assert!(map_bare(&bare("task.created", Some("human"))).is_none());
    }

    // One agent reply publishes two events: `task.updated` directly and
    // `reply.received` through the forwarder. Only the reply notifies, tagged by
    // its message so a repeat of it is collapsed.
    #[test]
    fn announces_one_reply_once_tagged_with_its_message() {
        let messages = json!([{"id": "m9", "sender": "agent", "text": "done"}]);

        assert_eq!(
            map_bare(&with_messages(
                "reply.received",
                messages.clone(),
                json!({})
            ))
            .unwrap()
            .tag,
            "m9"
        );
        assert_eq!(
            map_bare(&with_messages("task.updated", messages, json!({}))),
            None
        );
    }

    // A message row carrying no id names nothing, so the task-shaped tag stands
    // rather than every such event colliding on one empty tag and being deduped
    // into silence.
    fn says_nothing_about_the_agent_posting(kind: &str) {
        let messages =
            json!([{"id": "m1", "sender": "agent", "text": "hmm", "metadata": {"type": kind}}]);

        assert_eq!(
            map_bare(&with_messages(
                "reply.received",
                messages.clone(),
                json!({})
            )),
            None
        );
        assert_eq!(
            map_bare(&with_messages("task.updated", messages, json!({}))),
            None
        );
    }

    #[test]
    fn says_nothing_about_the_agent_posting_agent_thought() {
        says_nothing_about_the_agent_posting("agent_thought");
    }

    #[test]
    fn says_nothing_about_the_agent_posting_agent_plan() {
        says_nothing_about_the_agent_posting("agent_plan");
    }

    #[test]
    fn says_nothing_about_the_agent_posting_agent_usage() {
        says_nothing_about_the_agent_posting("agent_usage");
    }

    #[test]
    fn still_announces_an_agent_notice_a_task_completing_after_a_thought_and_a_new_task() {
        let notice = json!([{"id": "m1", "sender": "agent", "text": "rate limited", "metadata": {"type": "agent_notice"}}]);
        let thought = json!([{"id": "m2", "sender": "agent", "text": "hmm", "metadata": {"type": "agent_thought"}}]);

        assert!(
            map_bare(&with_messages("reply.received", notice, json!({})))
                .unwrap()
                .title
                .contains("Reply on: Ship it")
        );
        assert_eq!(
            map_bare(&status_change(json!({"title": "Ship it"})))
                .unwrap()
                .title,
            "Task COMPLETED: Ship it"
        );
        assert_eq!(
            map_bare(&with_messages("task.created", thought, json!({})))
                .unwrap()
                .title,
            "New task: Ship it"
        );
    }

    #[test]
    fn keeps_a_tag_of_its_own_when_the_newest_message_has_no_id() {
        let event = with_messages(
            "reply.received",
            json!([{"sender": "agent", "text": "done"}]),
            json!({}),
        );

        assert_eq!(map_bare(&event).unwrap().tag, "reply-t1");
    }

    #[test]
    fn keeps_a_tag_of_its_own_when_there_is_no_message_to_name() {
        let payload = json!({"id": "t1", "workspaceId": "w1", "title": "T"});
        let reply = map_bare(&json!({"type": "reply.received", "payload": payload})).unwrap();
        let created = map_bare(&json!({"type": "task.created", "payload": merged(payload, json!({"createdBy": "agent"}))})).unwrap();

        assert_eq!(reply.tag, "reply-t1");
        assert_eq!(created.tag, "task-create-t1");
    }

    // createSelfActionGate

    /// An event as the stream delivers it. `sender` is what settles whether this
    /// is this desktop's own reply coming back or somebody else's news: the
    /// payload carries the messages, so the gate reads rather than guesses.
    fn self_echo(id: Value, kind: &str, sender: &str) -> Value {
        json!({"type": kind, "payload": {"id": id, "messages": [{"sender": sender, "text": "hi"}]}})
    }

    fn gate_with(window_ms: u64) -> (SelfActionGate<Arc<ManualClock>>, Arc<ManualClock>) {
        let clock = Arc::new(ManualClock::default());
        (SelfActionGate::with_window(window_ms, clock.clone()), clock)
    }

    #[test]
    fn reports_no_recent_self_action_for_a_task_never_marked() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        assert!(!gate.is_recent_self_action(&self_echo(json!("t1"), "reply.received", "human")));
    }

    #[test]
    fn reports_a_recent_self_action_right_after_it_is_marked() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        gate.mark_self(Some("t1"));
        assert!(gate.is_recent_self_action(&self_echo(json!("t1"), "reply.received", "human")));
    }

    #[test]
    fn does_not_mute_a_type_a_reply_respond_could_never_produce() {
        // task.created and task.status are never the result of sending a reply, so
        // a real one of either must still notify even for a task this desktop
        // instance just replied to.
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        gate.mark_self(Some("t1"));
        assert!(!gate.is_recent_self_action(&self_echo(json!("t1"), "task.created", "human")));
        assert!(!gate.is_recent_self_action(&self_echo(json!("t1"), "task.status", "human")));
    }

    #[test]
    fn does_not_mark_a_task_when_given_no_id() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        // JS passes null and undefined; an empty id reads the same way.
        gate.mark_self(None);
        gate.mark_self(Some(""));
        assert!(!gate.is_recent_self_action(&self_echo(Value::Null, "reply.received", "human")));
        assert!(!gate.is_recent_self_action(&self_echo(json!(""), "reply.received", "human")));
    }

    #[test]
    fn does_not_confuse_one_task_for_another() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        gate.mark_self(Some("t1"));
        assert!(!gate.is_recent_self_action(&self_echo(json!("t2"), "reply.received", "human")));
    }

    #[test]
    fn expires_the_mute_once_the_window_passes() {
        let (mut gate, clock) = gate_with(1000);
        let echo = self_echo(json!("t1"), "reply.received", "human");

        gate.mark_self(Some("t1"));
        clock.set(999);
        assert!(gate.is_recent_self_action(&echo));
        clock.set(1000);
        assert!(!gate.is_recent_self_action(&echo));
    }

    // The bug this gate caused. A human replies, the gate arms for ten seconds,
    // and the agent answers in two, which is most of them. Keyed on the clock
    // alone that reply was this desktop's own echo and was dropped; the status
    // change that followed arrived after the window and still notified, which is
    // exactly the difference that got reported.
    #[test]
    fn does_not_mute_the_agent_answering_inside_the_window() {
        let (mut gate, clock) = gate_with(10000);

        gate.mark_self(Some("t1"));
        clock.set(2000);

        assert!(!gate.is_recent_self_action(&self_echo(json!("t1"), "reply.received", "agent")));
        // And the human's own message in that same window is still the echo it was.
        assert!(gate.is_recent_self_action(&self_echo(json!("t1"), "reply.received", "human")));
    }

    // Older events, and anything the forwarder publishes without them, fall back
    // to the window rather than notifying on everything.
    #[test]
    fn still_uses_the_window_when_the_payload_carries_no_messages() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));
        gate.mark_self(Some("t1"));
        let reply = |payload: Value| json!({"type": "reply.received", "payload": payload});

        assert!(gate.is_recent_self_action(&reply(json!({"id": "t1"}))));
        assert!(gate.is_recent_self_action(&reply(json!({"id": "t1", "messages": []}))));
        // A message row that names nobody is not the agent, so it falls back too
        // rather than reading as somebody else's news.
        assert!(gate.is_recent_self_action(&reply(json!({"id": "t1", "messages": [{}]}))));
        assert!(gate.is_recent_self_action(&reply(json!({"id": "t1", "messages": [null]}))));
    }

    #[test]
    fn holds_up_against_an_event_that_is_barely_one() {
        let mut gate = SelfActionGate::new(Arc::new(ManualClock::default()));

        assert!(!gate.is_recent_self_action(&Value::Null));
        assert!(!gate.is_recent_self_action(&json!({})));
    }

    #[test]
    fn lets_a_genuine_later_reply_on_the_same_task_notify_again() {
        // The mute must not persist forever just because the task was replied to
        // once; a real agent reply after the window is news again.
        let (mut gate, clock) = gate_with(1000);

        gate.mark_self(Some("t1"));
        clock.set(2000);
        assert!(!gate.is_recent_self_action(&self_echo(json!("t1"), "reply.received", "human")));
    }

    #[test]
    fn forgets_a_stale_mark_even_when_only_mark_self_is_ever_called_for_it_again() {
        // mark_self fires on every reply/respond regardless of what the stream
        // echoes back, so is_recent_self_action may never run for a given task (a
        // muted workspace, say). Pruning has to happen from mark_self too, or the
        // map would grow for the life of the process.
        let (mut gate, clock) = gate_with(1000);

        gate.mark_self(Some("stale-task"));
        clock.set(5000);
        gate.mark_self(Some("other-task"));
        assert_eq!(gate.marked_at.len(), 1);

        clock.set(5001);
        assert!(!gate.is_recent_self_action(&self_echo(
            json!("stale-task"),
            "reply.received",
            "human"
        )));
        assert!(gate.is_recent_self_action(&self_echo(
            json!("other-task"),
            "reply.received",
            "human"
        )));
    }

    #[test]
    fn has_a_sane_default_window() {
        assert_eq!(SELF_ACTION_WINDOW_MS, 10000);
    }

    // createNotificationGate

    #[test]
    fn lets_the_first_notification_for_a_tag_through() {
        assert!(NotificationGate::new(Arc::new(ManualClock::default())).allow("task-create-1"));
    }

    #[test]
    fn collapses_the_backends_duplicate_publish_of_one_event() {
        // A single task creation is published twice, once by the REST handler and
        // once by the CRUD-event consumer, carrying the same tag both times.
        let mut gate = NotificationGate::new(Arc::new(ManualClock::default()));

        assert!(gate.allow("task-create-1"));
        assert!(!gate.allow("task-create-1"));
    }

    #[test]
    fn does_not_collapse_different_tags() {
        let mut gate = NotificationGate::new(Arc::new(ManualClock::default()));

        assert!(gate.allow("task-create-1"));
        assert!(gate.allow("task-create-2"));
    }

    #[test]
    fn allows_the_same_tag_again_once_the_window_has_passed() {
        // A task genuinely updated twice, minutes apart, is two pieces of news.
        let clock = Arc::new(ManualClock::default());
        let mut gate = NotificationGate::with_window(1000, clock.clone());

        assert!(gate.allow("task-status-1"));
        clock.set(999);
        assert!(!gate.allow("task-status-1"));
        clock.set(1000);
        assert!(gate.allow("task-status-1"));
    }

    #[test]
    fn forgets_old_tags_rather_than_growing_without_bound() {
        let clock = Arc::new(ManualClock::default());
        let mut gate = NotificationGate::with_window(100, clock.clone());

        for i in 0..50u64 {
            gate.allow(&format!("tag-{i}"));
            clock.set((i + 1) * 10);
        }
        // Well past the window, so the early entries must have been pruned; the
        // first tag being allowed again is the observable proof.
        clock.set(500 + 1000);
        assert!(gate.allow("tag-0"));
        assert_eq!(gate.seen.len(), 1);
    }

    #[test]
    fn has_a_sane_default_dedupe_window() {
        assert_eq!(DEDUPE_WINDOW_MS, 10000);
    }

    // createUnreadCounter

    #[test]
    fn counts_up_and_publishes_each_change() {
        let badge = RecordingBadge::default();
        let mut counter = UnreadCounter::new(badge.clone());

        assert_eq!(counter.increment(), 1);
        assert_eq!(counter.increment(), 2);
        assert_eq!(counter.value(), 2);
        assert_eq!(badge.0.borrow()[1], 2);
    }

    #[test]
    fn clears_back_to_zero() {
        let badge = RecordingBadge::default();
        let mut counter = UnreadCounter::new(badge.clone());

        counter.increment();
        assert_eq!(counter.clear(), 0);
        assert_eq!(badge.0.borrow().last(), Some(&0));
    }

    #[test]
    fn does_not_republish_a_clear_when_already_at_zero() {
        // The window emits focus constantly; repainting an unchanged badge each
        // time is pointless work.
        let badge = RecordingBadge::default();
        let mut counter = UnreadCounter::new(badge.clone());

        counter.clear();
        counter.clear();

        assert!(badge.0.borrow().is_empty());
    }

    // badgeFor

    fn nothing() -> BadgeState {
        BadgeState {
            badge: String::new(),
            overlay: None,
        }
    }

    #[test]
    fn shows_nothing_at_zero() {
        assert_eq!(badge_for(0, "darwin"), nothing());
        assert_eq!(badge_for(-1, "darwin"), nothing());
        assert_eq!(badge_for(0, "win32"), nothing());
    }

    #[test]
    fn puts_the_number_on_the_dock_on_macos_and_linux() {
        for platform in ["darwin", "linux"] {
            assert_eq!(
                badge_for(3, platform),
                BadgeState {
                    badge: "3".into(),
                    overlay: None
                }
            );
        }
    }

    #[test]
    fn uses_a_taskbar_overlay_on_windows_which_has_no_numeric_badge() {
        assert_eq!(
            badge_for(3, "win32"),
            BadgeState {
                badge: String::new(),
                overlay: Some(Overlay {
                    count: 3,
                    description: "3 unread notifications".into()
                }),
            }
        );
    }

    #[test]
    fn gets_the_singular_right_in_the_overlay_description_which_is_read_aloud() {
        assert_eq!(
            badge_for(1, "win32").overlay.unwrap().description,
            "1 unread notification"
        );
    }

    // Beyond the vitest cases: what the Rust shape adds.

    #[test]
    fn stringifies_a_field_the_way_js_does() {
        assert_eq!(js_string(Some(&json!("text"))), "text");
        assert_eq!(js_string(Some(&json!(7))), "7");
    }

    #[test]
    fn counts_title_length_in_utf16_code_units_like_the_backend() {
        // One emoji is two code units, so 31 of them are 62 against a limit of 60.
        assert_eq!(truncate(&"😀".repeat(30), 60), "😀".repeat(30));
        assert_eq!(
            truncate(&"😀".repeat(31), 60),
            format!("{}…", "😀".repeat(30))
        );
    }

    #[test]
    fn reads_no_message_from_a_messages_field_that_is_not_a_list() {
        assert_eq!(last_message_sender(&json!({"messages": "none"})), "");
        assert_eq!(last_message_id(&json!({"messages": {"id": "m1"}})), "");
        assert_eq!(last_message_id(&json!({})), "");
    }

    #[test]
    fn rejects_an_event_whose_type_is_not_text() {
        assert!(!notify(&json!({"type": 7, "payload": task(json!({}))})));
    }

    #[test]
    fn rejects_a_status_whose_new_status_is_not_text() {
        assert!(!notify(&status_change(json!({"to": 7}))));
    }

    #[test]
    fn rejects_a_request_whose_path_is_not_a_reply_route() {
        for path in [
            "/api/v1/workspaces//tasks/t1/reply",
            "/api/v1/workspaces/ws1/tasks//reply",
            "/api/v1/workspaces/ws1/jobs/t1/reply",
            "/api/v1/other/ws1/tasks/t1/reply",
        ] {
            assert_eq!(
                task_id_from_self_action_request("POST", path),
                None,
                "{path}"
            );
        }
    }

    #[test]
    fn reads_the_wall_clock() {
        let first = SystemClock.now_ms();
        assert!(first > 1_700_000_000_000);
        assert!(SystemClock.now_ms() >= first);
    }
}
