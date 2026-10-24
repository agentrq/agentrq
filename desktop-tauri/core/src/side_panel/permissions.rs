// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! A page in the side panel asking for a permission — the camera, a location,
//! notifications — and the person being asked, as a browser would ask them.
//!
//! The request is held here and the question goes to the window, which draws a
//! bar under the panel's toolbar. Whatever is answered is remembered for that
//! site (permission_store.rs), so the next request is answered without asking.
//!
//! Never left waiting: a question nobody answers is a Block after a deadline,
//! and one replaced by another, or by the page going away, is a Block at once.
//! A held callback is a page hanging on a promise that will never settle.

use crate::side_panel::permission_store::{PermissionDecision, PermissionStoreBackend};
pub use crate::window_state::{StdTimers, Timers};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use url::Url;

/// What a page may have without asking, as a browser gives it.
pub const ALWAYS_ALLOWED: &[&str] = &[
    "clipboard-sanitized-write",
    "fullscreen",
    "persistent-storage",
    "screen-wake-lock",
];

/// What is worth asking about, by the name it is remembered under.
///
/// Several Chromium names are one question to a person — a precise and an
/// approximate location are both "your location" — so they share a key, and a
/// single answer covers both. Anything not here, including every permission a
/// future Chromium adds, is refused without asking: an unknown permission is
/// not one to grant by default.
const KEYS: &[(&str, &str)] = &[
    ("geolocation", "geolocation"),
    ("geolocation-approximate", "geolocation"),
    ("notifications", "notifications"),
    ("midi", "midi"),
    ("midiSysex", "midi"),
    ("display-capture", "display-capture"),
    ("clipboard-read", "clipboard-read"),
    ("idle-detection", "idle-detection"),
    ("window-management", "window-management"),
    ("local-fonts", "local-fonts"),
    ("storage-access", "storage-access"),
    ("top-level-storage-access", "storage-access"),
    ("pointerLock", "pointer-lock"),
    ("keyboardLock", "keyboard-lock"),
    ("speaker-selection", "speaker-selection"),
];

/// How long a question waits before it is a Block.
pub const ANSWER_DEADLINE_MS: u64 = 60_000;

pub fn origin_of(raw_url: &str) -> String {
    let Ok(url) = Url::parse(raw_url) else {
        return String::new();
    };
    let Some(host) = url.host_str().filter(|h| !h.is_empty()) else {
        return String::new();
    };
    match url.port() {
        Some(port) => format!("{}://{}:{}", url.scheme(), host, port),
        None => format!("{}://{}", url.scheme(), host),
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct PermissionDetails {
    pub media_types: Vec<String>,
    pub media_type: Option<String>,
    pub requesting_url: Option<String>,
}

/// The keys a request is about. `media` is the camera, the microphone or both,
/// and they are separate decisions — allowing a call's microphone is not
/// allowing its camera.
///
/// Empty when the permission is not one to ask about.
pub fn permission_keys(permission: &str, details: Option<&PermissionDetails>) -> Vec<String> {
    if permission == "media" {
        let types: Vec<String> = match details {
            Some(d) if !d.media_types.is_empty() => d.media_types.clone(),
            Some(d) if d.media_type.is_some() => vec![d.media_type.clone().unwrap()],
            _ => Vec::new(),
        };
        let mut keys = Vec::new();
        if types.iter().any(|t| t == "video") {
            keys.push("camera".to_string());
        }
        if types.iter().any(|t| t == "audio") {
            keys.push("microphone".to_string());
        }
        return keys;
    }

    for (k, v) in KEYS {
        if *k == permission {
            return vec![v.to_string()];
        }
    }
    Vec::new()
}

#[derive(Debug, Clone, Default)]
pub struct GuestPage {
    pub id: u64,
    pub url: Option<String>,
}

impl GuestPage {
    pub fn new(id: u64, url: &str) -> Self {
        Self {
            id,
            url: Some(url.to_string()),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PermissionQuestion {
    pub id: u64,
    pub guest_id: u64,
    pub origin: String,
    pub permissions: Vec<String>,
}

struct PendingRequest {
    guest_id: u64,
    origin: String,
    keys: Vec<String>,
    callback: Box<dyn FnOnce(bool) + Send>,
    timer: u64,
}

struct BrokerState {
    next_id: u64,
    pending: HashMap<u64, PendingRequest>,
}

pub struct PermissionBroker<S: PermissionStoreBackend> {
    state: Arc<Mutex<BrokerState>>,
    store: Arc<S>,
    ask: Arc<dyn Fn(PermissionQuestion) + Send + Sync>,
    settled: Arc<dyn Fn(u64) + Send + Sync>,
    pub deadline_ms: u64,
    timers: Arc<dyn Timers>,
}

impl<S: PermissionStoreBackend> Clone for PermissionBroker<S> {
    fn clone(&self) -> Self {
        Self {
            state: Arc::clone(&self.state),
            store: Arc::clone(&self.store),
            ask: Arc::clone(&self.ask),
            settled: Arc::clone(&self.settled),
            deadline_ms: self.deadline_ms,
            timers: Arc::clone(&self.timers),
        }
    }
}

fn settle_inner(
    state: &mut BrokerState,
    timers: &dyn Timers,
    settled_cb: &dyn Fn(u64),
    id: u64,
    granted: bool,
) {
    if let Some(waiting) = state.pending.remove(&id) {
        timers.clear_timer(waiting.timer);
        (waiting.callback)(granted);
        settled_cb(id);
    }
}

impl<S: PermissionStoreBackend + 'static> PermissionBroker<S> {
    pub fn new(store: Arc<S>, ask: impl Fn(PermissionQuestion) + Send + Sync + 'static) -> Self {
        Self::with_options(
            store,
            ask,
            |_| {},
            ANSWER_DEADLINE_MS,
            Arc::new(StdTimers::default()),
        )
    }

    pub fn with_options(
        store: Arc<S>,
        ask: impl Fn(PermissionQuestion) + Send + Sync + 'static,
        settled: impl Fn(u64) + Send + Sync + 'static,
        deadline_ms: u64,
        timers: Arc<dyn Timers>,
    ) -> Self {
        Self {
            state: Arc::new(Mutex::new(BrokerState {
                next_id: 1,
                pending: HashMap::new(),
            })),
            store,
            ask: Arc::new(ask),
            settled: Arc::new(settled),
            deadline_ms,
            timers,
        }
    }

    pub fn request(
        &self,
        guest: Option<&GuestPage>,
        permission: &str,
        callback: impl FnOnce(bool) + Send + 'static,
        details: Option<&PermissionDetails>,
    ) {
        if ALWAYS_ALLOWED.contains(&permission) {
            callback(true);
            return;
        }
        let keys = permission_keys(permission, details);
        let raw_url = details
            .and_then(|d| d.requesting_url.as_deref())
            .or_else(|| guest.and_then(|g| g.url.as_deref()))
            .unwrap_or("");
        let origin = origin_of(raw_url);
        if keys.is_empty() || origin.is_empty() {
            callback(false);
            return;
        }

        self.store.load();
        let decided: Vec<Option<PermissionDecision>> =
            keys.iter().map(|k| self.store.get(&origin, k)).collect();

        if decided.contains(&Some(PermissionDecision::Block)) {
            callback(false);
            return;
        }
        if decided
            .iter()
            .all(|d| *d == Some(PermissionDecision::Allow))
        {
            callback(true);
            return;
        }

        let guest_id = guest.map(|g| g.id).unwrap_or(0);
        let mut state = self.state.lock().unwrap();
        let earlier_ids: Vec<u64> = state
            .pending
            .iter()
            .filter(|(_, w)| w.guest_id == guest_id)
            .map(|(&id, _)| id)
            .collect();

        for earlier_id in earlier_ids {
            settle_inner(&mut state, &*self.timers, &*self.settled, earlier_id, false);
        }

        let id = state.next_id;
        state.next_id += 1;

        let state_weak = Arc::downgrade(&self.state);
        let timers_clone = Arc::clone(&self.timers);
        let settled_clone = Arc::clone(&self.settled);

        let timer = self.timers.set_timer(
            self.deadline_ms,
            Box::new(move || {
                if let Some(state_arc) = state_weak.upgrade() {
                    let mut state = state_arc.lock().unwrap();
                    settle_inner(&mut state, &*timers_clone, &*settled_clone, id, false);
                }
            }),
        );

        let question = PermissionQuestion {
            id,
            guest_id,
            origin: origin.clone(),
            permissions: keys.clone(),
        };

        state.pending.insert(
            id,
            PendingRequest {
                guest_id,
                origin,
                keys,
                callback: Box::new(callback),
                timer,
            },
        );
        drop(state);

        (self.ask)(question);
    }

    pub fn check(
        &self,
        _guest: Option<&GuestPage>,
        permission: &str,
        requesting_origin: &str,
        details: Option<&PermissionDetails>,
    ) -> bool {
        if ALWAYS_ALLOWED.contains(&permission) {
            return true;
        }
        let keys = permission_keys(permission, details);
        let origin = origin_of(requesting_origin);
        if keys.is_empty() || origin.is_empty() {
            return false;
        }
        keys.iter()
            .all(|k| self.store.get(&origin, k) == Some(PermissionDecision::Allow))
    }

    pub fn answer(&self, id: u64, decision: &str) -> bool {
        let mut state = self.state.lock().unwrap();
        let Some(waiting) = state.pending.remove(&id) else {
            return false;
        };
        self.timers.clear_timer(waiting.timer);

        let granted = decision == "allow";
        let decision_str = if granted { "allow" } else { "block" };
        for key in &waiting.keys {
            self.store.set(&waiting.origin, key, decision_str);
        }

        (waiting.callback)(granted);
        (self.settled)(id);
        drop(state);

        true
    }

    pub fn cancel_for(&self, guest_id: u64) {
        let mut state = self.state.lock().unwrap();
        let ids: Vec<u64> = state
            .pending
            .iter()
            .filter(|(_, w)| w.guest_id == guest_id)
            .map(|(&id, _)| id)
            .collect();

        for id in ids {
            settle_inner(&mut state, &*self.timers, &*self.settled, id, false);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;
    use std::sync::atomic::{AtomicU64, Ordering};

    struct ManualTimerEntry {
        ms: u64,
        callback: Box<dyn FnOnce() + Send>,
    }

    #[derive(Default)]
    struct ManualTimers {
        next: AtomicU64,
        timers: Mutex<BTreeMap<u64, ManualTimerEntry>>,
    }

    impl Timers for ManualTimers {
        fn set_timer(&self, ms: u64, callback: Box<dyn FnOnce() + Send>) -> u64 {
            let id = self.next.fetch_add(1, Ordering::Relaxed) + 1;
            self.timers
                .lock()
                .unwrap()
                .insert(id, ManualTimerEntry { ms, callback });
            id
        }

        fn clear_timer(&self, handle: u64) {
            self.timers.lock().unwrap().remove(&handle);
        }
    }

    impl ManualTimers {
        fn fire_all(&self) {
            let entries: Vec<_> = {
                let mut lock = self.timers.lock().unwrap();
                std::mem::take(&mut *lock).into_values().collect()
            };
            for entry in entries {
                (entry.callback)();
            }
        }

        fn timer_count(&self) -> usize {
            self.timers.lock().unwrap().len()
        }

        fn first_ms(&self) -> Option<u64> {
            self.timers.lock().unwrap().values().next().map(|e| e.ms)
        }
    }

    #[derive(Default)]
    struct MemoryStore {
        record: Mutex<HashMap<String, HashMap<String, PermissionDecision>>>,
        set_count: Mutex<usize>,
    }

    impl MemoryStore {
        fn new(initial: &[(&str, &[(&str, &str)])]) -> Self {
            let mut map = HashMap::new();
            for (origin, perms) in initial {
                let mut perms_map = HashMap::new();
                for (k, v) in *perms {
                    if let Some(dec) = PermissionDecision::parse(v) {
                        perms_map.insert(k.to_string(), dec);
                    }
                }
                map.insert(origin.to_string(), perms_map);
            }
            Self {
                record: Mutex::new(map),
                set_count: Mutex::new(0),
            }
        }
    }

    impl PermissionStoreBackend for MemoryStore {
        fn load(&self) {}

        fn get(&self, origin: &str, permission: &str) -> Option<PermissionDecision> {
            self.record
                .lock()
                .unwrap()
                .get(origin)?
                .get(permission)
                .copied()
        }

        fn set(&self, origin: &str, permission: &str, decision: &str) {
            *self.set_count.lock().unwrap() += 1;
            let decision = PermissionDecision::parse(decision).unwrap();
            self.record
                .lock()
                .unwrap()
                .entry(origin.to_string())
                .or_default()
                .insert(permission.to_string(), decision);
        }
    }

    #[derive(Clone, Default)]
    struct CallbackSpy {
        called: Arc<Mutex<Option<bool>>>,
    }

    impl CallbackSpy {
        fn call(&self) -> impl FnOnce(bool) + Send + 'static {
            let called = Arc::clone(&self.called);
            move |granted| {
                *called.lock().unwrap() = Some(granted);
            }
        }

        fn value(&self) -> Option<bool> {
            *self.called.lock().unwrap()
        }

        fn was_called(&self) -> bool {
            self.called.lock().unwrap().is_some()
        }
    }

    struct Setup {
        broker: PermissionBroker<MemoryStore>,
        store: Arc<MemoryStore>,
        asked: Arc<Mutex<Vec<PermissionQuestion>>>,
        clock: Arc<ManualTimers>,
    }

    fn setup(initial: &[(&str, &[(&str, &str)])]) -> Setup {
        let store = Arc::new(MemoryStore::new(initial));
        let asked = Arc::new(Mutex::new(Vec::new()));
        let clock = Arc::new(ManualTimers::default());
        let asked_clone = Arc::clone(&asked);
        let broker = PermissionBroker::with_options(
            Arc::clone(&store),
            move |q| asked_clone.lock().unwrap().push(q),
            |_| {},
            ANSWER_DEADLINE_MS,
            clock.clone(),
        );
        Setup {
            broker,
            store,
            asked,
            clock,
        }
    }

    fn guest_default() -> GuestPage {
        GuestPage::new(7, "https://meet.example/room")
    }

    #[test]
    fn splits_media_into_the_camera_and_the_microphone() {
        assert_eq!(
            permission_keys(
                "media",
                Some(&PermissionDetails {
                    media_types: vec!["video".to_string(), "audio".to_string()],
                    ..Default::default()
                })
            ),
            vec!["camera", "microphone"]
        );
        assert_eq!(
            permission_keys(
                "media",
                Some(&PermissionDetails {
                    media_types: vec!["audio".to_string()],
                    ..Default::default()
                })
            ),
            vec!["microphone"]
        );
        assert_eq!(
            permission_keys(
                "media",
                Some(&PermissionDetails {
                    media_type: Some("video".to_string()),
                    ..Default::default()
                })
            ),
            vec!["camera"]
        );
        assert_eq!(
            permission_keys("media", Some(&PermissionDetails::default())),
            Vec::<String>::new()
        );
        assert_eq!(permission_keys("media", None), Vec::<String>::new());
    }

    #[test]
    fn asks_one_question_for_the_names_that_are_one_to_a_person() {
        assert_eq!(
            permission_keys("geolocation-approximate", None),
            vec!["geolocation"]
        );
        assert_eq!(permission_keys("midiSysex", None), vec!["midi"]);
        assert_eq!(
            permission_keys("top-level-storage-access", None),
            vec!["storage-access"]
        );
        assert_eq!(permission_keys("pointerLock", None), vec!["pointer-lock"]);
    }

    #[test]
    fn has_nothing_to_ask_about_a_permission_it_does_not_know() {
        assert_eq!(permission_keys("hid", None), Vec::<String>::new());
        assert_eq!(
            permission_keys("a-permission-from-next-year", None),
            Vec::<String>::new()
        );
    }

    #[test]
    fn asks_the_person_and_remembers_what_they_answer() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        let details = PermissionDetails {
            media_types: vec!["video".to_string(), "audio".to_string()],
            requesting_url: Some("https://meet.example/room".to_string()),
            ..Default::default()
        };
        s.broker.request(
            Some(&guest_default()),
            "media",
            callback.call(),
            Some(&details),
        );

        assert_eq!(
            *s.asked.lock().unwrap(),
            vec![PermissionQuestion {
                id: 1,
                guest_id: 7,
                origin: "https://meet.example".to_string(),
                permissions: vec!["camera".to_string(), "microphone".to_string()],
            }]
        );
        assert!(!callback.was_called());

        assert!(s.broker.answer(1, "allow"));
        assert_eq!(callback.value(), Some(true));
        assert_eq!(
            s.store.get("https://meet.example", "camera"),
            Some(PermissionDecision::Allow)
        );
        assert_eq!(
            s.store.get("https://meet.example", "microphone"),
            Some(PermissionDecision::Allow)
        );
    }

    #[test]
    fn answers_from_memory_without_asking_again() {
        let s = setup(&[(
            "https://meet.example",
            &[("camera", "allow"), ("notifications", "block")],
        )]);
        let allowed = CallbackSpy::default();
        let blocked = CallbackSpy::default();
        let details = PermissionDetails {
            media_types: vec!["video".to_string()],
            ..Default::default()
        };
        s.broker.request(
            Some(&guest_default()),
            "media",
            allowed.call(),
            Some(&details),
        );
        s.broker.request(
            Some(&guest_default()),
            "notifications",
            blocked.call(),
            None,
        );

        assert_eq!(allowed.value(), Some(true));
        assert_eq!(blocked.value(), Some(false));
        assert!(s.asked.lock().unwrap().is_empty());
    }

    #[test]
    fn asks_only_when_part_of_a_request_is_undecided_and_a_block_anywhere_is_a_block() {
        let s = setup(&[("https://meet.example", &[("camera", "allow")])]);
        let details = PermissionDetails {
            media_types: vec!["video".to_string(), "audio".to_string()],
            ..Default::default()
        };
        s.broker.request(
            Some(&guest_default()),
            "media",
            CallbackSpy::default().call(),
            Some(&details),
        );
        assert_eq!(s.asked.lock().unwrap().len(), 1);

        let blocked = setup(&[("https://meet.example", &[("camera", "block")])]);
        let callback = CallbackSpy::default();
        blocked.broker.request(
            Some(&guest_default()),
            "media",
            callback.call(),
            Some(&details),
        );
        assert_eq!(callback.value(), Some(false));
        assert!(blocked.asked.lock().unwrap().is_empty());
    }

    #[test]
    fn remembers_a_block_as_firmly_as_an_allow() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker
            .request(Some(&guest_default()), "geolocation", callback.call(), None);
        s.broker.answer(1, "block");
        assert_eq!(callback.value(), Some(false));
        assert_eq!(
            s.store.get("https://meet.example", "geolocation"),
            Some(PermissionDecision::Block)
        );
    }

    #[test]
    fn treats_any_answer_but_allow_as_a_block() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker.request(
            Some(&guest_default()),
            "notifications",
            callback.call(),
            None,
        );
        s.broker.answer(1, "whatever");
        assert_eq!(callback.value(), Some(false));
        assert_eq!(
            s.store.get("https://meet.example", "notifications"),
            Some(PermissionDecision::Block)
        );
    }

    #[test]
    fn allows_what_a_browser_allows_without_asking() {
        let s = setup(&[]);
        for permission in ALWAYS_ALLOWED {
            let callback = CallbackSpy::default();
            s.broker
                .request(Some(&guest_default()), permission, callback.call(), None);
            assert_eq!(callback.value(), Some(true), "{permission}");
        }
        assert!(s.asked.lock().unwrap().is_empty());
    }

    #[test]
    fn refuses_an_unknown_permission_without_asking() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker.request(
            Some(&guest_default()),
            "a-permission-from-next-year",
            callback.call(),
            None,
        );
        assert_eq!(callback.value(), Some(false));
        assert!(s.asked.lock().unwrap().is_empty());
    }

    #[test]
    fn refuses_a_request_it_cannot_attribute_to_a_site() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker.request(
            Some(&GuestPage::new(1, "not a url")),
            "notifications",
            callback.call(),
            None,
        );
        assert_eq!(callback.value(), Some(false));

        let no_guest = CallbackSpy::default();
        s.broker
            .request(None, "notifications", no_guest.call(), None);
        assert_eq!(no_guest.value(), Some(false));
    }

    #[test]
    fn still_asks_for_a_request_that_names_its_site_but_no_page() {
        let s = setup(&[]);
        let details = PermissionDetails {
            requesting_url: Some("https://meet.example/".to_string()),
            ..Default::default()
        };
        s.broker.request(
            None,
            "notifications",
            CallbackSpy::default().call(),
            Some(&details),
        );
        let asked = s.asked.lock().unwrap();
        assert_eq!(asked[0].guest_id, 0);
        assert_eq!(asked[0].origin, "https://meet.example");
    }

    #[test]
    fn blocks_a_question_nobody_answers_after_the_deadline_without_remembering_it() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker.request(
            Some(&guest_default()),
            "notifications",
            callback.call(),
            None,
        );
        assert_eq!(s.clock.first_ms(), Some(ANSWER_DEADLINE_MS));

        s.clock.fire_all();
        assert_eq!(callback.value(), Some(false));
        assert_eq!(*s.store.set_count.lock().unwrap(), 0);
        assert!(!s.broker.answer(1, "allow"));
    }

    #[test]
    fn replaces_a_waiting_question_with_a_newer_one_from_the_same_page() {
        let s = setup(&[]);
        let first = CallbackSpy::default();
        let second = CallbackSpy::default();
        s.broker
            .request(Some(&guest_default()), "notifications", first.call(), None);
        s.broker
            .request(Some(&guest_default()), "geolocation", second.call(), None);

        assert_eq!(first.value(), Some(false));
        assert!(!second.was_called());
        assert_eq!(
            s.asked
                .lock()
                .unwrap()
                .iter()
                .map(|q| q.id)
                .collect::<Vec<_>>(),
            vec![1, 2]
        );
        assert_eq!(s.clock.timer_count(), 1);
    }

    #[test]
    fn keeps_questions_from_different_pages_apart() {
        let s = setup(&[]);
        let one = CallbackSpy::default();
        let two = CallbackSpy::default();
        s.broker.request(
            Some(&GuestPage::new(1, "https://a.example/")),
            "notifications",
            one.call(),
            None,
        );
        s.broker.request(
            Some(&GuestPage::new(2, "https://b.example/")),
            "notifications",
            two.call(),
            None,
        );

        assert!(!one.was_called());
        s.broker.cancel_for(1);
        assert_eq!(one.value(), Some(false));
        assert!(!two.was_called());
    }

    #[test]
    fn blocks_what_a_page_was_asking_when_it_goes_away() {
        let s = setup(&[]);
        let callback = CallbackSpy::default();
        s.broker.request(
            Some(&guest_default()),
            "notifications",
            callback.call(),
            None,
        );
        s.broker.cancel_for(7);
        assert_eq!(callback.value(), Some(false));
        assert_eq!(*s.store.set_count.lock().unwrap(), 0);
        s.broker.cancel_for(7);
    }

    #[test]
    fn answers_a_page_that_only_checks_from_the_same_record() {
        let s = setup(&[(
            "https://meet.example",
            &[("camera", "allow"), ("notifications", "block")],
        )]);
        assert!(s.broker.check(
            None,
            "media",
            "https://meet.example",
            Some(&PermissionDetails {
                media_type: Some("video".to_string()),
                ..Default::default()
            })
        ));
        assert!(!s.broker.check(
            None,
            "media",
            "https://meet.example",
            Some(&PermissionDetails {
                media_type: Some("audio".to_string()),
                ..Default::default()
            })
        ));
        assert!(!s
            .broker
            .check(None, "notifications", "https://meet.example", None));
        assert!(!s
            .broker
            .check(None, "notifications", "https://other.example", None));
        assert!(s.broker.check(
            None,
            "clipboard-sanitized-write",
            "https://other.example",
            None
        ));
        assert!(!s.broker.check(None, "hid", "https://meet.example", None));
        assert!(!s.broker.check(None, "notifications", "", None));
    }

    #[test]
    fn names_an_extension_page_by_its_own_origin() {
        let s = setup(&[]);
        let guest = GuestPage::new(1, "agentrq-ext://notes/panel/index.html");
        s.broker.request(
            Some(&guest),
            "notifications",
            CallbackSpy::default().call(),
            None,
        );
        assert_eq!(s.asked.lock().unwrap()[0].origin, "agentrq-ext://notes");
    }

    #[test]
    fn says_when_each_question_stops_waiting_however_it_ended() {
        let store = Arc::new(MemoryStore::default());
        let settled_calls = Arc::new(Mutex::new(Vec::new()));
        let clock = Arc::new(ManualTimers::default());
        let settled_clone = Arc::clone(&settled_calls);
        let broker = PermissionBroker::with_options(
            store,
            |_| {},
            move |id| settled_clone.lock().unwrap().push(id),
            ANSWER_DEADLINE_MS,
            clock.clone(),
        );

        broker.request(
            Some(&guest_default()),
            "notifications",
            CallbackSpy::default().call(),
            None,
        );
        assert!(broker.answer(1, "allow"));

        broker.request(
            Some(&guest_default()),
            "geolocation",
            CallbackSpy::default().call(),
            None,
        );
        clock.fire_all();

        broker.request(
            Some(&guest_default()),
            "midi",
            CallbackSpy::default().call(),
            None,
        );
        broker.cancel_for(7);

        assert_eq!(*settled_calls.lock().unwrap(), vec![1, 2, 3]);
    }

    #[test]
    fn uses_the_real_timers_by_default() {
        let store = Arc::new(MemoryStore::default());
        let broker = PermissionBroker::new(store.clone(), |_| {});
        assert_eq!(broker.deadline_ms, ANSWER_DEADLINE_MS);

        let clone = broker.clone();
        assert_eq!(clone.deadline_ms, ANSWER_DEADLINE_MS);

        // Exercise the default ask and settled closures
        let callback = CallbackSpy::default();
        broker.request(
            Some(&guest_default()),
            "notifications",
            callback.call(),
            None,
        );
        assert!(broker.answer(1, "allow"));

        let callback2 = CallbackSpy::default();
        let store2 = Arc::new(MemoryStore::default());
        let short_broker = PermissionBroker::with_options(
            store2,
            |_| {},
            |_| {},
            5,
            Arc::new(StdTimers::default()),
        );
        short_broker.request(
            Some(&guest_default()),
            "notifications",
            callback2.call(),
            None,
        );
        std::thread::sleep(std::time::Duration::from_millis(50));
        assert_eq!(callback2.value(), Some(false));
    }

    #[test]
    fn test_origin_of_edges() {
        assert_eq!(origin_of("file:///path/to/file"), "");
        assert_eq!(origin_of("data:text/plain,hello"), "");
        assert_eq!(origin_of("sc:///path"), "");
        assert_eq!(
            origin_of("http://localhost:3000/room"),
            "http://localhost:3000"
        );
    }
}
