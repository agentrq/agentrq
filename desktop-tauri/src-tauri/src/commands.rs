// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Every command behind `window.agentrq`, one per Electron IPC channel.
//!
//! A command is named after its channel: drop `agentrq:`, turn
//! `:` and `-` into `_`, and camelCase into snake_case
//! (`agentrq:side-panel:permission-answer` is `side_panel_permission_answer`).
//! The `on*` listeners are events, not commands, so they are not here.

use std::sync::Arc;

use tauri::ipc::Channel;
use tauri::State;

use serde_json::{json, Value};

use crate::event_source::{self, Frame, Sink, Streams};
use crate::network::Network;
use crate::profiles::{ConnectionState, Profiles, ProfilesView};

/// `agentrq:attachments:forgetAll`: signing out.
#[tauri::command]
pub async fn attachments_forget_all(network: State<'_, Arc<Network>>) -> Result<bool, String> {
    Ok(network.active().await.attachments.forget_all())
}

/// `agentrq:attachments:forgetTask`
#[tauri::command]
pub async fn attachments_forget_task(
    network: State<'_, Arc<Network>>,
    workspace_id: String,
    task_id: String,
) -> Result<bool, String> {
    Ok(network
        .active()
        .await
        .attachments
        .forget_task(&workspace_id, &task_id))
}

/// `agentrq:attachments:forgetWorkspace`
#[tauri::command]
pub async fn attachments_forget_workspace(
    network: State<'_, Arc<Network>>,
    workspace_id: String,
) -> Result<bool, String> {
    Ok(network
        .active()
        .await
        .attachments
        .forget_workspace(&workspace_id))
}

/// Not an Electron channel: the bridge's `EventSource` opens a stream at a
/// same-origin `url` (path and query) and is sent its frames on `on_event`.
/// The URL is checked and built by `core::protocol`, as a proxied request is.
#[tauri::command]
pub async fn event_source_open(
    network: State<'_, Arc<Network>>,
    streams: State<'_, Streams>,
    url: String,
    on_event: Channel<Frame>,
) -> Result<u32, String> {
    let net = network.active().await;
    let (path, search) = match url.find('?') {
        Some(at) => url.split_at(at),
        None => (url.as_str(), ""),
    };
    let target = net
        .protocol("")
        .proxy_target(path, search)
        .map_err(|error| format!("{error:?}"))?;
    let sink = Box::new(ChannelSink(on_event));
    Ok(streams.open(event_source::run(net.client.clone(), target, sink)))
}

/// Not an Electron channel: the bridge's `EventSource.close()`.
#[tauri::command]
pub fn event_source_close(streams: State<'_, Streams>, id: u32) {
    streams.close(id);
}

struct ChannelSink(Channel<Frame>);

impl Sink for ChannelSink {
    fn send(&self, frame: Frame) -> bool {
        self.0.send(frame).is_ok()
    }
}

/// `agentrq:connection:get`
#[tauri::command]
pub fn connection_get(profiles: State<'_, Arc<Profiles>>) -> ConnectionState {
    profiles.connection()
}

/// `agentrq:connection:validate`
#[tauri::command]
pub async fn connection_validate(
    profiles: State<'_, Arc<Profiles>>,
    url: String,
) -> Result<Value, String> {
    Ok(profiles.validate(&url).await)
}

/// `agentrq:connection:save`: also reloads the window to `/`.
#[tauri::command]
pub async fn connection_save(
    profiles: State<'_, Arc<Profiles>>,
    url: String,
) -> Result<Value, String> {
    profiles.save(&url).await
}

/// `agentrq:connection:cancel`
#[tauri::command]
pub async fn connection_cancel(profiles: State<'_, Arc<Profiles>>) -> Result<bool, String> {
    profiles.cancel().await
}

/// `agentrq:profiles:get`
#[tauri::command]
pub async fn profiles_get(profiles: State<'_, Arc<Profiles>>) -> Result<ProfilesView, String> {
    profiles.get().await
}

/// `agentrq:profiles:switch`
#[tauri::command]
pub async fn profiles_switch(
    profiles: State<'_, Arc<Profiles>>,
    id: String,
) -> Result<ProfilesView, String> {
    profiles.switch(&id).await
}

/// `agentrq:profiles:add`: a label is optional, as `add()` with none numbers it.
#[tauri::command]
pub async fn profiles_add(
    profiles: State<'_, Arc<Profiles>>,
    label: Option<String>,
) -> Result<ProfilesView, String> {
    profiles.add(&label.unwrap_or_default()).await
}

/// `agentrq:profiles:rename`
#[tauri::command]
pub async fn profiles_rename(
    profiles: State<'_, Arc<Profiles>>,
    id: String,
    label: String,
) -> Result<ProfilesView, String> {
    profiles.rename(&id, &label).await
}

/// `agentrq:profiles:remove`
#[tauri::command]
pub async fn profiles_remove(
    profiles: State<'_, Arc<Profiles>>,
    id: String,
) -> Result<ProfilesView, String> {
    profiles.remove(&id).await
}

/// `agentrq:notifications:get`. Every desktop this is built for shows
/// native notifications. Electron's `isSupported()` only asks whether it
/// could load libnotify on Linux, so a session with no notification server
/// reports support there too, and shows nothing.
#[tauri::command]
pub fn notifications_get(profiles: State<'_, Arc<Profiles>>) -> Value {
    json!({"supported": true, "mutedWorkspaces": profiles.muted()})
}

/// `agentrq:notifications:setMuted`
#[tauri::command]
pub async fn notifications_set_muted(
    profiles: State<'_, Arc<Profiles>>,
    workspace_id: String,
    muted: bool,
) -> Result<Value, String> {
    let muted = profiles.set_muted(&workspace_id, muted).await?;
    Ok(json!({"mutedWorkspaces": muted}))
}

use tauri::{AppHandle, Manager, Runtime};

/// `agentrq:update:check`
#[tauri::command]
pub fn update_check<R: Runtime>(app: AppHandle<R>) -> agentrq_desktop_core::updater::ActionResult {
    if let Some(updater) = app.try_state::<agentrq_desktop_core::updater::Updater>() {
        updater.check_now()
    } else {
        agentrq_desktop_core::updater::ActionResult {
            ok: false,
            reason: Some("Updates are unavailable".into()),
        }
    }
}

/// `agentrq:update:get`
#[tauri::command]
pub fn update_get<R: Runtime>(app: AppHandle<R>) -> agentrq_desktop_core::updater::UpdateState {
    if let Some(updater) = app.try_state::<agentrq_desktop_core::updater::Updater>() {
        updater.state()
    } else {
        agentrq_desktop_core::updater::UpdateState {
            status: agentrq_desktop_core::updater::UpdateStatus::Disabled,
            detail: "Updates are disabled in a development build".into(),
            remedy: String::new(),
            version: String::new(),
            progress: None,
            enabled: false,
            can_install_via_script: false,
        }
    }
}

/// `agentrq:update:install`
#[tauri::command]
pub fn update_install<R: Runtime>(app: AppHandle<R>) -> bool {
    if let Some(updater) = app.try_state::<agentrq_desktop_core::updater::Updater>() {
        updater.install_now()
    } else {
        false
    }
}

/// `agentrq:update:install-via-script`
#[tauri::command]
pub fn update_install_via_script<R: Runtime>(
    app: AppHandle<R>,
) -> agentrq_desktop_core::updater::ActionResult {
    if let Some(updater) = app.try_state::<agentrq_desktop_core::updater::Updater>() {
        updater.install_via_script()
    } else {
        agentrq_desktop_core::updater::ActionResult {
            ok: false,
            reason: Some("Updates are unavailable".into()),
        }
    }
}

/// `agentrq:navigation:ready`
#[tauri::command]
pub fn navigation_ready<R: Runtime>(app: AppHandle<R>) {
    crate::shell::navigation_ready(&app);
}

/// The invoke handler registering every command above.
pub fn handler<R: tauri::Runtime>() -> impl Fn(tauri::ipc::Invoke<R>) -> bool + Send + Sync + 'static
{
    tauri::generate_handler![
        attachments_forget_all,
        attachments_forget_task,
        attachments_forget_workspace,
        connection_cancel,
        connection_get,
        connection_save,
        connection_validate,
        notifications_get,
        notifications_set_muted,
        profiles_add,
        profiles_get,
        profiles_remove,
        profiles_rename,
        profiles_switch,
        crate::chrome::files_open,
        crate::chrome::clipboard_write,
        crate::chrome::emoji_show_panel,
        crate::chrome::dialog_choose_directory,
        crate::extensions::extensions_authorize,
        crate::extensions::extensions_cancel_node_download,
        crate::extensions::extensions_choose_folder,
        crate::extensions::extensions_configure,
        crate::extensions::extensions_deauthorize,
        crate::extensions::extensions_download_node,
        crate::extensions::extensions_drawer,
        crate::extensions::extensions_entries,
        crate::extensions::extensions_install_catalogue,
        crate::extensions::extensions_install_local,
        crate::extensions::extensions_invoke,
        crate::extensions::extensions_refresh,
        crate::extensions::extensions_set_enabled,
        crate::extensions::extensions_set_hook_consent,
        crate::extensions::extensions_state,
        crate::extensions::extensions_supervisor,
        crate::extensions::extensions_uninstall,
        update_check,
        update_get,
        update_install,
        update_install_via_script,
        crate::side_panel::side_panel_open_external,
        crate::extensions::side_panel_pages,
        crate::panel_permissions::side_panel_permission_answer,
        crate::panel_permissions::side_panel_permission_remove,
        crate::panel_permissions::side_panel_permissions,
        crate::side_panel::side_panel_guest_open,
        crate::side_panel::side_panel_guest_place,
        crate::side_panel::side_panel_guest_navigate,
        crate::side_panel::side_panel_guest_focus,
        crate::side_panel::side_panel_guest_snapshot,
        crate::side_panel::side_panel_guest_find,
        crate::side_panel::side_panel_guest_stop_find,
        crate::side_panel::side_panel_guest_close,
        navigation_ready,
        crate::chrome::view_fullscreen,
        crate::chrome::view_get,
        crate::chrome::view_zoom,
        crate::chrome::theme_set,
        crate::app_menu::menu_key_bindings,
        crate::app_menu::menu_run,
        event_source_open,
        event_source_close,
    ]
}

#[cfg(test)]
mod tests {
    use super::{Arc, Streams};
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, INVOKE_KEY};
    use tauri::webview::InvokeRequest;
    use tauri::Manager;

    /// The origin Tauri trusts as its own, which WebView2 spells over http.
    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    struct Rig {
        _app: tauri::App<tauri::test::MockRuntime>,
        webview: tauri::WebviewWindow<tauri::test::MockRuntime>,
        dir: tempfile::TempDir,
        calls: Arc<std::sync::Mutex<Vec<String>>>,
    }

    fn rig() -> Rig {
        let profiles = crate::profiles::tests::rig_with(
            Some(
                serde_json::json!({"profiles": [{"id": "default", "serverUrl": "http://127.0.0.1:9"}]}),
            ),
            None,
        );
        let host = crate::ext_host::ExtHost::new(
            crate::ext_host::Launch {
                program: "node".into(),
                args: vec![],
            },
            profiles.dir.path().into(),
            Arc::new(crate::ext_host::tests::FakeServices::default()),
        );
        let app = mock_builder()
            .invoke_handler(super::handler())
            .manage(profiles.network.clone())
            .manage(profiles.profiles.clone())
            .manage(Streams::default())
            .manage(host)
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");
        Rig {
            _app: app,
            webview,
            dir: profiles.dir,
            calls: profiles.calls,
        }
    }

    fn call(
        rig: &Rig,
        cmd: &str,
        args: serde_json::Value,
    ) -> Result<serde_json::Value, serde_json::Value> {
        get_ipc_response(
            &rig.webview,
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

    #[test]
    fn navigation_ready_invokes_through_handler() {
        let rig = rig();
        assert_eq!(
            call(&rig, "navigation_ready", serde_json::json!({})),
            Ok(serde_json::Value::Null)
        );
    }

    #[test]
    fn lists_no_panel_pages_until_extensions_arrive() {
        let rig = rig();
        let pages = call(&rig, "side_panel_pages", serde_json::json!({})).unwrap();
        assert_eq!(pages, serde_json::json!([]));
    }

    #[test]
    fn forgets_attachments_by_task_workspace_or_all() {
        let rig = rig();
        let store = rig.dir.path().join("profiles/default/attachments");
        let task = store.join("ws1/t1");
        std::fs::create_dir_all(&task).unwrap();
        std::fs::create_dir_all(store.join("ws2/t2")).unwrap();

        let forget_task = serde_json::json!({"workspaceId": "ws1", "taskId": "t1"});
        assert_eq!(
            call(&rig, "attachments_forget_task", forget_task),
            Ok(true.into())
        );
        assert!(!task.exists() && store.join("ws1").exists());

        let forget_ws = serde_json::json!({"workspaceId": "ws1"});
        assert_eq!(
            call(&rig, "attachments_forget_workspace", forget_ws),
            Ok(true.into())
        );
        assert!(!store.join("ws1").exists() && store.join("ws2").exists());

        let bad = serde_json::json!({"workspaceId": "../x"});
        assert_eq!(
            call(&rig, "attachments_forget_workspace", bad),
            Ok(false.into())
        );

        assert_eq!(
            call(&rig, "attachments_forget_all", serde_json::json!({})),
            Ok(true.into())
        );
        assert!(!store.exists());
    }

    #[test]
    fn opens_an_event_stream_only_where_the_proxy_would_forward() {
        let rig = rig();
        let open = |url: &str| {
            call(
                &rig,
                "event_source_open",
                serde_json::json!({"url": url, "onEvent": "__CHANNEL__:7"}),
            )
        };
        let streams = || rig._app.state::<Streams>();
        let first = open("/api/v1/events?x=1").unwrap();
        let second = open("/api/v1/events").unwrap();
        assert_eq!(second.as_u64(), Some(first.as_u64().unwrap() + 1));
        assert_eq!(open("/pub/stats").unwrap_err(), "Forbidden");
        assert_eq!(open("/api/%2e%2e/x").unwrap_err(), "Forbidden");
        assert_eq!(streams().len(), 2, "only the two it forwarded");
        for id in [first, second] {
            assert_eq!(
                call(&rig, "event_source_close", serde_json::json!({"id": id})),
                Ok(serde_json::Value::Null)
            );
        }
        assert_eq!(streams().len(), 0, "none left retrying");
    }

    #[test]
    fn sends_frames_down_the_channel_it_was_given() {
        use crate::event_source::{Frame, Sink};
        let sent = Arc::new(std::sync::Mutex::new(Vec::new()));
        let log = sent.clone();
        let channel = tauri::ipc::Channel::new(move |body| {
            log.lock().unwrap().push(body);
            Ok(())
        });
        assert!(super::ChannelSink(channel).send(Frame::Open));
        assert_eq!(sent.lock().unwrap().len(), 1);
    }

    #[test]
    fn answers_an_error_for_arguments_it_cannot_read() {
        let rig = rig();
        for cmd in [
            "attachments_forget_task",
            "attachments_forget_workspace",
            "event_source_open",
            "event_source_close",
            "connection_validate",
            "connection_save",
            "profiles_switch",
            "profiles_rename",
            "profiles_remove",
            "notifications_set_muted",
        ] {
            assert!(call(&rig, cmd, serde_json::json!({})).is_err(), "{cmd}");
        }
        for (cmd, args) in [
            (
                "attachments_forget_task",
                serde_json::json!({"workspaceId": "w"}),
            ),
            (
                "event_source_open",
                serde_json::json!({"url": "/api/v1/events"}),
            ),
            ("profiles_rename", serde_json::json!({"id": "default"})),
            ("profiles_add", serde_json::json!({"label": 5})),
            (
                "notifications_set_muted",
                serde_json::json!({"workspaceId": "w"}),
            ),
        ] {
            assert!(call(&rig, cmd, args).is_err(), "{cmd}");
        }
    }

    fn body_str(body: tauri::ipc::InvokeResponseBody) -> String {
        match body {
            tauri::ipc::InvokeResponseBody::Json(s) => s,
            tauri::ipc::InvokeResponseBody::Raw(b) => String::from_utf8(b).unwrap(),
        }
    }

    #[test]
    fn answers_for_the_connection_screen() {
        let rig = rig();
        assert_eq!(
            call(&rig, "connection_get", serde_json::json!({})),
            Ok(serde_json::json!({
                "configured": true,
                "serverUrl": "http://127.0.0.1:9",
                "locked": false,
                "canCancel": false,
            }))
        );
        let refused = serde_json::json!({"ok": false, "reason": "Server URL is required"});
        for cmd in ["connection_validate", "connection_save"] {
            assert_eq!(
                call(&rig, cmd, serde_json::json!({"url": ""})),
                Ok(refused.clone()),
                "{cmd}"
            );
        }
        assert_eq!(
            call(&rig, "connection_cancel", serde_json::json!({})),
            Ok(false.into())
        );
        assert!(rig.calls.lock().unwrap().is_empty());
    }

    #[test]
    fn adds_switches_renames_and_removes_profiles() {
        let rig = rig();
        let ids = |view: &serde_json::Value| -> Vec<String> {
            view["profiles"]
                .as_array()
                .unwrap()
                .iter()
                .map(|p| p["id"].as_str().unwrap().to_string())
                .collect()
        };
        let added = call(&rig, "profiles_add", serde_json::json!({})).unwrap();
        let new_id = added["activeProfileId"].as_str().unwrap().to_string();
        assert_eq!(added["profiles"][1]["label"], "Profile 2");
        let named = call(&rig, "profiles_add", serde_json::json!({"label": "Work"})).unwrap();
        assert_eq!(named["profiles"][2]["label"], "Work");

        let switched = call(
            &rig,
            "profiles_switch",
            serde_json::json!({"id": "default"}),
        )
        .unwrap();
        assert_eq!(switched["activeProfileId"], "default");
        let renamed = call(
            &rig,
            "profiles_rename",
            serde_json::json!({"id": "default", "label": "Home"}),
        )
        .unwrap();
        assert_eq!(renamed["profiles"][0]["label"], "Home");
        let removed = call(&rig, "profiles_remove", serde_json::json!({"id": new_id})).unwrap();
        assert_eq!(ids(&removed).len(), 2);
        assert!(!ids(&removed).contains(&new_id));

        let got = call(&rig, "profiles_get", serde_json::json!({})).unwrap();
        assert_eq!(ids(&got), ids(&removed));
        assert_eq!(got["profiles"][0]["identity"], serde_json::Value::Null);
        assert_eq!(got["profiles"][0]["duplicateOf"], "");
    }

    #[test]
    fn mutes_and_unmutes_a_workspace() {
        let rig = rig();
        let set = |id: &str, muted: bool| {
            call(
                &rig,
                "notifications_set_muted",
                serde_json::json!({"workspaceId": id, "muted": muted}),
            )
        };
        assert_eq!(
            call(&rig, "notifications_get", serde_json::json!({})),
            Ok(serde_json::json!({"supported": true, "mutedWorkspaces": []}))
        );
        assert_eq!(
            set("w1", true),
            Ok(serde_json::json!({"mutedWorkspaces": ["w1"]}))
        );
        assert_eq!(
            set("w2", true),
            Ok(serde_json::json!({"mutedWorkspaces": ["w1", "w2"]}))
        );
        assert_eq!(
            set("w1", false),
            Ok(serde_json::json!({"mutedWorkspaces": ["w2"]}))
        );
        assert_eq!(
            call(&rig, "notifications_get", serde_json::json!({})).unwrap()["mutedWorkspaces"],
            serde_json::json!(["w2"])
        );
    }

    #[test]
    fn answers_an_error_before_the_network_is_managed() {
        let app = mock_builder()
            .invoke_handler(super::handler())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");
        let rig = Rig {
            _app: app,
            webview,
            dir: tempfile::tempdir().unwrap(),
            calls: Default::default(),
        };
        assert!(call(&rig, "attachments_forget_all", serde_json::json!({})).is_err());
        for cmd in [
            "connection_validate",
            "connection_save",
            "connection_cancel",
            "profiles_get",
            "profiles_switch",
            "profiles_add",
            "profiles_rename",
            "profiles_remove",
            "notifications_get",
            "notifications_set_muted",
        ] {
            let args = serde_json::json!({"url": "", "id": "x", "label": "y", "workspaceId": "w", "muted": true});
            assert!(call(&rig, cmd, args).is_err(), "{cmd}");
        }
    }

    #[test]
    fn update_commands_answer_when_updater_is_absent() {
        let app = mock_builder()
            .invoke_handler(super::handler())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");

        let call = |cmd: &str| {
            get_ipc_response(
                &webview,
                InvokeRequest {
                    cmd: cmd.into(),
                    callback: CallbackFn(0),
                    error: CallbackFn(1),
                    url: LOCAL.parse().unwrap(),
                    body: InvokeBody::default(),
                    headers: Default::default(),
                    invoke_key: INVOKE_KEY.to_string(),
                },
            )
        };

        assert_eq!(
            body_str(tauri::ipc::InvokeResponseBody::Raw(b"raw".to_vec())),
            "raw"
        );

        let res = body_str(call("update_check").expect("update_check response"));
        assert_eq!(res, r#"{"ok":false,"reason":"Updates are unavailable"}"#);

        let res = body_str(call("update_get").expect("update_get response"));
        assert!(res.contains(r#""status":"disabled""#));

        let res = body_str(call("update_install").expect("update_install response"));
        assert_eq!(res, "false");

        let res = body_str(
            call("update_install_via_script").expect("update_install_via_script response"),
        );
        assert_eq!(res, r#"{"ok":false,"reason":"Updates are unavailable"}"#);
    }

    struct FakeAutoUpdater;
    impl agentrq_desktop_core::updater::AutoUpdater for FakeAutoUpdater {
        fn set_auto_download(&self, _enabled: bool) {}
        fn set_auto_install_on_app_quit(&self, _enabled: bool) {}
        fn check_for_updates(&self) -> Result<(), String> {
            Ok(())
        }
        fn quit_and_install(&self) {}
    }

    #[test]
    fn update_commands_delegate_to_managed_updater() {
        let auto = std::sync::Arc::new(FakeAutoUpdater);
        let deps = agentrq_desktop_core::updater::UpdaterDeps::new(auto, true, |_| {});
        let updater = agentrq_desktop_core::updater::Updater::new(deps);
        updater.start();
        updater.handle_event(agentrq_desktop_core::updater::UpdaterEvent::Downloaded {
            version: Some("1.2.3".into()),
        });

        let app = mock_builder()
            .manage(updater)
            .invoke_handler(super::handler())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");

        let call = |cmd: &str| {
            get_ipc_response(
                &webview,
                InvokeRequest {
                    cmd: cmd.into(),
                    callback: CallbackFn(0),
                    error: CallbackFn(1),
                    url: LOCAL.parse().unwrap(),
                    body: InvokeBody::default(),
                    headers: Default::default(),
                    invoke_key: INVOKE_KEY.to_string(),
                },
            )
        };

        let res = body_str(call("update_get").expect("update_get response"));
        assert!(res.contains(r#""status":"ready""#));
        assert!(res.contains(r#""enabled":true"#));

        let res = body_str(call("update_check").expect("update_check response"));
        assert_eq!(res, r#"{"ok":true}"#);

        let res = body_str(call("update_install").expect("update_install response"));
        assert_eq!(res, "true");

        let res = body_str(
            call("update_install_via_script").expect("update_install_via_script response"),
        );
        assert!(res.contains(r#""ok":false"#));
    }
}
