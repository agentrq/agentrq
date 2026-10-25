// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The side panel's site permissions (WP7c): the window's half of Electron's
//! `index.js` around `permissions.js` and `permission-store.js`.
//!
//! One broker for the active profile, its answers kept in that profile's
//! `side-panel-permissions/<profileId>.json`. Each platform's webview hands
//! its request to [`request`] (`side_panel/`'s platform files); the question
//! goes to the app's window, and only that window may answer it, list the
//! answers or remove one. A guest has no command at all, and these refuse any
//! webview but `main` besides.

use std::sync::{Arc, Mutex, MutexGuard, PoisonError};

use agentrq_desktop_core::side_panel::child::guest_number;
use agentrq_desktop_core::side_panel::permission_store::{PermissionStore, SitePermissions};
use agentrq_desktop_core::side_panel::permissions::{
    GuestPage, PermissionBroker, PermissionQuestion,
};
use agentrq_desktop_core::side_panel::platform_permissions::{Ask, NativeAsk};
use tauri::{AppHandle, Emitter, Manager, Runtime, State, Webview};

use crate::chrome::MAIN;
use crate::network::Network;
use crate::profiles::Profiles;

type Broker = PermissionBroker<PermissionStore>;

/// What a platform's request is answered through, once: `true` grants it.
pub type Answer = Box<dyn FnOnce(bool) + Send>;

/// The broker and store of the profile they were made for.
struct Current {
    profile: String,
    store: Arc<PermissionStore>,
    broker: Broker,
}

/// The active profile's broker, made when it is first needed.
#[derive(Default)]
pub struct PanelPermissions(Mutex<Option<Arc<Current>>>);

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

fn ask<R: Runtime>(app: &AppHandle<R>, question: PermissionQuestion) {
    let _ = app.emit_to(MAIN, "agentrq:side-panel:permission-request", question);
}

fn settled<R: Runtime>(app: &AppHandle<R>, id: u64) {
    let _ = app.emit_to(MAIN, "agentrq:side-panel:permission-settled", id);
}

/// The active profile's, replacing another profile's: every guest of that
/// one closed with its window, and their questions with them.
fn current<R: Runtime>(app: &AppHandle<R>) -> Arc<Current> {
    current_in(app, &app.state::<PanelPermissions>())
}

fn current_in<R: Runtime>(app: &AppHandle<R>, state: &PanelPermissions) -> Arc<Current> {
    let profile = app.state::<Arc<Network>>().active_id();
    let mut kept = lock(&state.0);
    if let Some(current) = kept.as_ref().filter(|c| c.profile == profile) {
        return Arc::clone(current);
    }
    let store = Arc::new(app.state::<Arc<Profiles>>().permission_store(&profile));
    let (asking, settling) = (app.clone(), app.clone());
    let broker = Broker::with_options(
        Arc::clone(&store),
        move |question| ask(&asking, question),
        move |id| settled(&settling, id),
        agentrq_desktop_core::side_panel::permissions::ANSWER_DEADLINE_MS,
        Arc::new(agentrq_desktop_core::side_panel::permissions::StdTimers::default()),
    );
    let current = Arc::new(Current {
        profile,
        store,
        broker,
    });
    *kept = Some(Arc::clone(&current));
    current
}

/// A guest's page asked: `answer` is told once, never on the caller's stack
/// unless nothing had to be looked up. The broker reads its file, so it is
/// asked on the async runtime, never on the main thread a webview's
/// callback runs on. Anything but a guest is refused.
pub fn request<R: Runtime>(
    app: &AppHandle<R>,
    label: &str,
    page_url: &str,
    native: NativeAsk,
    answer: Answer,
) {
    let Some(number) = guest_number(label) else {
        return answer(false);
    };
    let (app, page) = (app.clone(), GuestPage::new(u64::from(number), page_url));
    match native {
        NativeAsk::Granted => answer(true),
        NativeAsk::AnyAllowed(asks) => {
            tauri::async_runtime::spawn(async move { any_allowed(&app, &asks, answer) });
        }
        NativeAsk::Request(ask) => {
            tauri::async_runtime::spawn(async move {
                let current = current(&app);
                current
                    .broker
                    .request(Some(&page), &ask.permission, answer, Some(&ask.details));
            });
        }
    }
}

/// Whether the site already has any of `asks`, without asking.
fn any_allowed<R: Runtime>(app: &AppHandle<R>, asks: &[Ask], answer: Answer) {
    let current = current(app);
    current.store.load();
    let allowed = asks.iter().any(|ask| {
        let origin = ask.details.requesting_url.as_deref().unwrap_or("");
        current
            .broker
            .check(None, &ask.permission, origin, Some(&ask.details))
    });
    answer(allowed);
}

/// The page went away: what it was asking is a Block. Nothing to do before
/// any page has asked.
pub fn cancel<R: Runtime>(app: &AppHandle<R>, guest: u32) {
    let kept = app
        .try_state::<PanelPermissions>()
        .and_then(|state| lock(&state.0).clone());
    if let Some(current) = kept {
        current.broker.cancel_for(u64::from(guest));
    }
}

/// `agentrq:side-panel:permission-answer`: the person's answer, remembered
/// for the site, then given to the page. `false` when the question had
/// already been settled, or the asker is not the app's window.
#[tauri::command]
pub async fn side_panel_permission_answer<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    id: u64,
    decision: String,
) -> bool {
    if webview.label() != MAIN {
        return false;
    }
    let decision = if decision == "allow" {
        "allow"
    } else {
        "block"
    };
    current(&app).broker.answer(id, decision)
}

/// `agentrq:side-panel:permissions`: every answer, by site. Never an error,
/// but a command that borrows its state answers a `Result`.
#[tauri::command]
pub async fn side_panel_permissions<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    permissions: State<'_, PanelPermissions>,
) -> Result<Vec<SitePermissions>, String> {
    if webview.label() != MAIN {
        return Ok(Vec::new());
    }
    let current = current_in(&app, &permissions);
    current.store.load();
    Ok(current.store.list())
}

/// `agentrq:side-panel:permission-remove`: one permission of a site, or all
/// of them when none is named. Answers the list as it is now.
#[tauri::command]
pub async fn side_panel_permission_remove<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    origin: String,
    permission: Option<String>,
) -> Vec<SitePermissions> {
    if webview.label() != MAIN {
        return Vec::new();
    }
    let current = current(&app);
    current.store.load();
    current.store.remove(&origin, permission.as_deref());
    current.store.list()
}

#[cfg(test)]
mod tests {
    use super::*;
    use agentrq_desktop_core::side_panel::platform_permissions::{webkit_ask, webview2_ask};
    use serde_json::{json, Value};
    use std::time::Duration;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{
        get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY,
    };
    use tauri::webview::InvokeRequest;
    use tauri::{Listener, WebviewWindow, WebviewWindowBuilder};

    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    const PAGE: &str = "https://webcamtests.example/check";
    const SITE: &str = "https://webcamtests.example";

    struct Rig {
        app: tauri::App<MockRuntime>,
        main: WebviewWindow<MockRuntime>,
        heard: Arc<Mutex<Vec<(String, Value)>>>,
        profiles: crate::profiles::tests::Rig,
    }

    impl Rig {
        fn handle(&self) -> &AppHandle<MockRuntime> {
            self.app.handle()
        }
        fn heard(&self) -> Vec<(String, Value)> {
            std::mem::take(&mut *lock(&self.heard))
        }
        /// A guest's request, and where its answer went.
        fn request(&self, label: &str, native: NativeAsk) -> Arc<Mutex<Option<bool>>> {
            let answered = Arc::new(Mutex::new(None));
            let to = Arc::clone(&answered);
            request(
                self.handle(),
                label,
                PAGE,
                native,
                Box::new(move |granted| *lock(&to) = Some(granted)),
            );
            answered
        }
        fn store_file(&self, profile: &str) -> std::path::PathBuf {
            self.profiles
                .profiles
                .permission_store(profile)
                .file()
                .to_path_buf()
        }
    }

    fn rig_with(profiles: Value) -> Rig {
        let profiles = crate::profiles::tests::rig_with(Some(profiles), None);
        let app = mock_builder()
            .invoke_handler(crate::commands::handler())
            .manage(profiles.network.clone())
            .manage(profiles.profiles.clone())
            .manage(PanelPermissions::default())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let heard = Arc::new(Mutex::new(Vec::new()));
        for event in [
            "agentrq:side-panel:permission-request",
            "agentrq:side-panel:permission-settled",
        ] {
            let heard = Arc::clone(&heard);
            app.listen_any(event, move |e| {
                let payload = serde_json::from_str(e.payload()).unwrap();
                lock(&heard).push((event.to_string(), payload));
            });
        }
        let main = WebviewWindowBuilder::new(&app, MAIN, Default::default())
            .build()
            .expect("mock window");
        Rig {
            app,
            main,
            heard,
            profiles,
        }
    }

    fn rig() -> Rig {
        rig_with(json!({"profiles": [{"id": "work", "serverUrl": "https://app.agentrq.com"}]}))
    }

    fn call(webview: &WebviewWindow<MockRuntime>, cmd: &str, args: Value) -> Result<Value, Value> {
        get_ipc_response(
            webview,
            InvokeRequest {
                cmd: cmd.into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: InvokeBody::Json(args),
                headers: Default::default(),
                invoke_key: INVOKE_KEY.to_string(),
            },
        )
        .map(|body| body.deserialize().unwrap())
    }

    /// Until what was spawned has run, for at most two seconds.
    fn wait_for(done: impl Fn() -> bool) {
        for _ in 0..200 {
            if done() {
                return;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
    }

    fn camera() -> NativeAsk {
        webkit_ask("WebKitUserMediaPermissionRequest", false, true, false, PAGE)
    }

    #[test]
    fn asks_the_window_once_and_remembers_the_answer_on_disk() {
        let rig = rig();
        let answered = rig.request("panel-guest-1", camera());
        wait_for(|| !lock(&rig.heard).is_empty());
        assert_eq!(*lock(&answered), None, "waiting on the person");
        assert_eq!(
            rig.heard(),
            [(
                "agentrq:side-panel:permission-request".to_string(),
                json!({"id": 1, "guestId": 1, "origin": SITE, "permissions": ["camera"]})
            )]
        );
        let answer = |decision| {
            call(
                &rig.main,
                "side_panel_permission_answer",
                json!({"id": 1, "decision": decision}),
            )
        };
        assert_eq!(answer("allow"), Ok(json!(true)));
        assert_eq!(*lock(&answered), Some(true));
        assert_eq!(
            rig.heard(),
            [(
                "agentrq:side-panel:permission-settled".to_string(),
                json!(1)
            )]
        );
        assert_eq!(answer("allow"), Ok(json!(false)), "already settled");
        let saved = std::fs::read_to_string(rig.store_file("work")).unwrap();
        assert!(saved.contains("\"camera\": \"allow\""), "{saved}");

        // The next request is answered from memory, without asking.
        let again = rig.request("panel-guest-2", camera());
        wait_for(|| lock(&again).is_some());
        assert_eq!(*lock(&again), Some(true));
        assert!(rig.heard().is_empty());
    }

    #[test]
    fn a_restart_reads_the_answers_back() {
        let rig = rig();
        std::fs::create_dir_all(rig.store_file("work").parent().unwrap()).unwrap();
        std::fs::write(
            rig.store_file("work"),
            json!({"origins": {SITE: {"camera": "block"}}}).to_string(),
        )
        .unwrap();
        let answered = rig.request("panel-guest-1", camera());
        wait_for(|| lock(&answered).is_some());
        assert_eq!(*lock(&answered), Some(false));
        assert!(rig.heard().is_empty());
    }

    #[test]
    fn anything_but_allow_is_a_block() {
        let rig = rig();
        rig.request("panel-guest-1", camera());
        wait_for(|| !lock(&rig.heard).is_empty());
        let answered = call(
            &rig.main,
            "side_panel_permission_answer",
            json!({"id": 1, "decision": "maybe"}),
        );
        assert_eq!(answered, Ok(json!(true)));
        let listed = call(&rig.main, "side_panel_permissions", json!({}));
        assert_eq!(
            listed,
            Ok(
                json!([{"origin": SITE, "permissions": [{"permission": "camera", "decision": "block"}]}])
            )
        );
    }

    #[test]
    fn the_site_permissions_list_removes_one_permission_or_a_whole_site() {
        let rig = rig();
        std::fs::create_dir_all(rig.store_file("work").parent().unwrap()).unwrap();
        std::fs::write(
            rig.store_file("work"),
            json!({"origins": {
                SITE: {"camera": "allow", "microphone": "block"},
                "https://maps.example": {"geolocation": "allow"}
            }})
            .to_string(),
        )
        .unwrap();
        let remove = |origin: &str, permission: Value| {
            call(
                &rig.main,
                "side_panel_permission_remove",
                json!({"origin": origin, "permission": permission}),
            )
        };
        assert_eq!(
            remove(SITE, json!("camera")),
            Ok(json!([
                {"origin": "https://maps.example", "permissions": [{"permission": "geolocation", "decision": "allow"}]},
                {"origin": SITE, "permissions": [{"permission": "microphone", "decision": "block"}]}
            ]))
        );
        assert_eq!(
            remove("https://maps.example", Value::Null),
            Ok(
                json!([{"origin": SITE, "permissions": [{"permission": "microphone", "decision": "block"}]}])
            )
        );
        // Removed for good: the camera is asked about again.
        let answered = rig.request("panel-guest-1", camera());
        wait_for(|| !lock(&rig.heard).is_empty());
        assert_eq!(*lock(&answered), None);
    }

    #[test]
    fn only_the_apps_window_may_answer_list_or_remove() {
        let rig = rig();
        let answered = rig.request("panel-guest-1", camera());
        wait_for(|| !lock(&rig.heard).is_empty());
        let other = WebviewWindowBuilder::new(&rig.app, "panel-guest-1", Default::default())
            .build()
            .unwrap();
        let tries = [
            (
                "side_panel_permission_answer",
                json!({"id": 1, "decision": "allow"}),
                json!(false),
            ),
            ("side_panel_permissions", json!({}), json!([])),
            (
                "side_panel_permission_remove",
                json!({"origin": SITE, "permission": null}),
                json!([]),
            ),
        ];
        for (cmd, args, refused) in tries {
            let webview = other.as_ref().clone();
            let answer = match cmd {
                "side_panel_permission_answer" => {
                    json!(tauri::async_runtime::block_on(
                        side_panel_permission_answer(
                            rig.handle().clone(),
                            webview,
                            1,
                            "allow".to_string()
                        )
                    ))
                }
                "side_panel_permissions" => {
                    json!(tauri::async_runtime::block_on(side_panel_permissions(
                        rig.handle().clone(),
                        webview,
                        rig.handle().state::<PanelPermissions>()
                    ))
                    .unwrap())
                }
                _ => json!(tauri::async_runtime::block_on(
                    side_panel_permission_remove(
                        rig.handle().clone(),
                        webview,
                        SITE.to_string(),
                        None
                    )
                )),
            };
            assert_eq!(answer, refused, "{cmd} {args}");
        }
        assert_eq!(*lock(&answered), None, "still waiting on the person");
    }

    #[test]
    fn a_page_that_goes_away_is_blocked_and_its_bar_comes_down() {
        let rig = rig();
        cancel(rig.handle(), 1);
        let answered = rig.request("panel-guest-1", camera());
        wait_for(|| !lock(&rig.heard).is_empty());
        rig.heard();
        cancel(rig.handle(), 2);
        assert_eq!(*lock(&answered), None, "another page's");
        cancel(rig.handle(), 1);
        assert_eq!(*lock(&answered), Some(false));
        assert_eq!(
            rig.heard(),
            [(
                "agentrq:side-panel:permission-settled".to_string(),
                json!(1)
            )]
        );
    }

    #[test]
    fn what_the_broker_need_not_ask_about_is_answered_at_once() {
        let rig = rig();
        let granted = rig.request("panel-guest-1", webview2_ask(9, PAGE));
        assert_eq!(*lock(&granted), Some(true));
        let refused = rig.request("panel-guest-1", webview2_ask(7, PAGE));
        wait_for(|| lock(&refused).is_some());
        assert_eq!(*lock(&refused), Some(false));
        let app_itself = rig.request(MAIN, camera());
        assert_eq!(*lock(&app_itself), Some(false), "never the app's own");
        assert!(rig.heard().is_empty());
    }

    #[test]
    fn device_labels_follow_an_allowed_camera_or_microphone() {
        let rig = rig();
        let device_info = || {
            let answered = rig.request(
                "panel-guest-1",
                webkit_ask(
                    "WebKitDeviceInfoPermissionRequest",
                    false,
                    false,
                    false,
                    PAGE,
                ),
            );
            wait_for(|| lock(&answered).is_some());
            let answer = *lock(&answered);
            answer
        };
        assert_eq!(device_info(), Some(false));
        std::fs::create_dir_all(rig.store_file("work").parent().unwrap()).unwrap();
        std::fs::write(
            rig.store_file("work"),
            json!({"origins": {SITE: {"microphone": "allow"}}}).to_string(),
        )
        .unwrap();
        // The file is read once a run; a fresh profile's broker reads it.
        *lock(&rig.app.state::<PanelPermissions>().0) = None;
        assert_eq!(device_info(), Some(true));
        assert!(rig.heard().is_empty());
    }

    #[test]
    fn every_command_refuses_arguments_it_cannot_read() {
        let rig = rig();
        // The list takes no argument; only its state can be missing.
        let bare = mock_builder()
            .invoke_handler(crate::commands::handler())
            .build(mock_context(noop_assets()))
            .unwrap();
        let window = WebviewWindowBuilder::new(&bare, MAIN, Default::default())
            .build()
            .unwrap();
        let refused = call(&window, "side_panel_permissions", json!({})).unwrap_err();
        assert!(
            refused.as_str().unwrap().contains("state not managed"),
            "{refused}"
        );
        for (cmd, args) in [
            (
                "side_panel_permission_answer",
                json!({"id": 1, "decision": "allow"}),
            ),
            (
                "side_panel_permission_remove",
                json!({"origin": SITE, "permission": "camera"}),
            ),
        ] {
            for name in args.as_object().unwrap().keys() {
                let mut bad = args.clone();
                bad[name] = json!({"not": "it"});
                let refused = call(&rig.main, cmd, bad).unwrap_err();
                assert!(
                    refused.as_str().unwrap().contains(name.as_str()),
                    "{cmd} {name}: {refused}"
                );
            }
        }
    }

    #[test]
    fn each_profile_keeps_its_own_answers() {
        let rig = rig_with(json!({"activeProfileId": "work", "profiles": [
            {"id": "work", "serverUrl": "https://app.agentrq.com"},
            {"id": "home", "serverUrl": "https://home.example"}
        ]}));
        let first = current(rig.handle());
        assert!(Arc::ptr_eq(&first, &current(rig.handle())), "kept");
        let network = rig.handle().state::<Arc<Network>>();
        let home = tauri::async_runtime::block_on(network.profile("home", "https://home.example"));
        network.activate(home);
        let second = current(rig.handle());
        assert_eq!(second.profile, "home");
        assert_eq!(second.store.file(), rig.store_file("home"));
        assert_ne!(rig.store_file("home"), rig.store_file("work"));
    }
}
