// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Every command behind `window.agentrq`, one per Electron IPC channel.
//!
//! Each starts as a stub that answers a `todo` error, so the work package that
//! owns it replaces its own stub and nothing else, and the packages merge
//! independently. A command is named after its channel: drop `agentrq:`, turn
//! `:` and `-` into `_`, and camelCase into snake_case
//! (`agentrq:side-panel:permission-answer` is `side_panel_permission_answer`).
//! The `on*` listeners are events, not commands, so they are not here.

/// The error a stub answers: the member, and the work package that owns it.
pub fn todo(member: &str, owner: &str) -> Result<(), String> {
    Err(format!(
        "window.agentrq.{member} is not implemented yet ({owner})"
    ))
}

/// `agentrq:attachments:forgetAll`
#[tauri::command]
pub fn attachments_forget_all() -> Result<(), String> {
    todo("attachments.forgetAll", "WP2")
}

/// `agentrq:attachments:forgetTask`
#[tauri::command]
pub fn attachments_forget_task() -> Result<(), String> {
    todo("attachments.forgetTask", "WP2")
}

/// `agentrq:attachments:forgetWorkspace`
#[tauri::command]
pub fn attachments_forget_workspace() -> Result<(), String> {
    todo("attachments.forgetWorkspace", "WP2")
}

/// `agentrq:connection:cancel`
#[tauri::command]
pub fn connection_cancel() -> Result<(), String> {
    todo("connection.cancel", "WP4")
}

/// `agentrq:connection:get`
#[tauri::command]
pub fn connection_get() -> Result<(), String> {
    todo("connection.get", "WP4")
}

/// `agentrq:connection:save`
#[tauri::command]
pub fn connection_save() -> Result<(), String> {
    todo("connection.save", "WP4")
}

/// `agentrq:connection:validate`
#[tauri::command]
pub fn connection_validate() -> Result<(), String> {
    todo("connection.validate", "WP4")
}

/// `agentrq:notifications:get`
#[tauri::command]
pub fn notifications_get() -> Result<(), String> {
    todo("notifications.get", "WP6")
}

/// `agentrq:notifications:setMuted`
#[tauri::command]
pub fn notifications_set_muted() -> Result<(), String> {
    todo("notifications.setMuted", "WP6")
}

/// `agentrq:profiles:add`
#[tauri::command]
pub fn profiles_add() -> Result<(), String> {
    todo("profiles.add", "WP4")
}

/// `agentrq:profiles:get`
#[tauri::command]
pub fn profiles_get() -> Result<(), String> {
    todo("profiles.get", "WP4")
}

/// `agentrq:profiles:remove`
#[tauri::command]
pub fn profiles_remove() -> Result<(), String> {
    todo("profiles.remove", "WP4")
}

/// `agentrq:profiles:rename`
#[tauri::command]
pub fn profiles_rename() -> Result<(), String> {
    todo("profiles.rename", "WP4")
}

/// `agentrq:profiles:switch`
#[tauri::command]
pub fn profiles_switch() -> Result<(), String> {
    todo("profiles.switch", "WP4")
}

/// `agentrq:extensions:authorize`
#[tauri::command]
pub fn extensions_authorize() -> Result<(), String> {
    todo("extensions.authorize", "WP9")
}

/// `agentrq:extensions:choose-folder`
#[tauri::command]
pub fn extensions_choose_folder() -> Result<(), String> {
    todo("extensions.chooseFolder", "WP9")
}

/// `agentrq:extensions:configure`
#[tauri::command]
pub fn extensions_configure() -> Result<(), String> {
    todo("extensions.configure", "WP9")
}

/// `agentrq:extensions:deauthorize`
#[tauri::command]
pub fn extensions_deauthorize() -> Result<(), String> {
    todo("extensions.deauthorize", "WP9")
}

/// `agentrq:extensions:drawer`
#[tauri::command]
pub fn extensions_drawer() -> Result<(), String> {
    todo("extensions.drawer", "WP9")
}

/// `agentrq:extensions:entries`
#[tauri::command]
pub fn extensions_entries() -> Result<(), String> {
    todo("extensions.entries", "WP9")
}

/// `agentrq:extensions:install-catalogue`
#[tauri::command]
pub fn extensions_install_catalogue() -> Result<(), String> {
    todo("extensions.installFromCatalogue", "WP9")
}

/// `agentrq:extensions:install-local`
#[tauri::command]
pub fn extensions_install_local() -> Result<(), String> {
    todo("extensions.installLocal", "WP9")
}

/// `agentrq:extensions:invoke`
#[tauri::command]
pub fn extensions_invoke() -> Result<(), String> {
    todo("extensions.invoke", "WP9")
}

/// `agentrq:extensions:refresh`
#[tauri::command]
pub fn extensions_refresh() -> Result<(), String> {
    todo("extensions.refresh", "WP9")
}

/// `agentrq:extensions:set-enabled`
#[tauri::command]
pub fn extensions_set_enabled() -> Result<(), String> {
    todo("extensions.setEnabled", "WP9")
}

/// `agentrq:extensions:set-hook-consent`
#[tauri::command]
pub fn extensions_set_hook_consent() -> Result<(), String> {
    todo("extensions.setHookConsent", "WP9")
}

/// `agentrq:extensions:state`
#[tauri::command]
pub fn extensions_state() -> Result<(), String> {
    todo("extensions.state", "WP9")
}

/// `agentrq:extensions:supervisor`
#[tauri::command]
pub fn extensions_supervisor() -> Result<(), String> {
    todo("extensions.supervisor", "WP9")
}

/// `agentrq:extensions:uninstall`
#[tauri::command]
pub fn extensions_uninstall() -> Result<(), String> {
    todo("extensions.uninstall", "WP9")
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

/// `agentrq:side-panel:open-external`
#[tauri::command]
pub fn side_panel_open_external() -> Result<(), String> {
    todo("sidePanel.openExternal", "WP7b")
}

/// `agentrq:side-panel:pages`
#[tauri::command]
pub fn side_panel_pages() -> Result<(), String> {
    todo("sidePanel.pages", "WP7b")
}

/// `agentrq:side-panel:permission-answer`
#[tauri::command]
pub fn side_panel_permission_answer() -> Result<(), String> {
    todo("sidePanel.answerPermission", "WP7c")
}

/// `agentrq:side-panel:permission-remove`
#[tauri::command]
pub fn side_panel_permission_remove() -> Result<(), String> {
    todo("sidePanel.removePermission", "WP7c")
}

/// `agentrq:side-panel:permissions`
#[tauri::command]
pub fn side_panel_permissions() -> Result<(), String> {
    todo("sidePanel.permissions", "WP7c")
}

/// `agentrq:panel:send`
#[tauri::command]
pub fn panel_send() -> Result<(), String> {
    todo("panel.send", "WP7b / WP9")
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
        extensions_authorize,
        extensions_choose_folder,
        extensions_configure,
        extensions_deauthorize,
        extensions_drawer,
        extensions_entries,
        extensions_install_catalogue,
        extensions_install_local,
        extensions_invoke,
        extensions_refresh,
        extensions_set_enabled,
        extensions_set_hook_consent,
        extensions_state,
        extensions_supervisor,
        extensions_uninstall,
        update_check,
        update_get,
        update_install,
        update_install_via_script,
        side_panel_open_external,
        side_panel_pages,
        side_panel_permission_answer,
        side_panel_permission_remove,
        side_panel_permissions,
        panel_send,
        crate::chrome::view_fullscreen,
        crate::chrome::view_get,
        crate::chrome::view_zoom,
        crate::chrome::theme_set,
        crate::app_menu::menu_key_bindings,
        crate::app_menu::menu_run,
    ]
}

#[cfg(test)]
mod tests {
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, INVOKE_KEY};
    use tauri::webview::InvokeRequest;

    /// The origin Tauri trusts as its own, which WebView2 spells over http.
    #[cfg(windows)]
    const LOCAL: &str = "http://tauri.localhost";
    #[cfg(not(windows))]
    const LOCAL: &str = "tauri://localhost";

    /// The remaining stub commands answering `todo`.
    const STUB_COMMANDS: &[(&str, &str)] = &[
        ("attachments_forget_all", "attachments.forgetAll"),
        ("attachments_forget_task", "attachments.forgetTask"),
        (
            "attachments_forget_workspace",
            "attachments.forgetWorkspace",
        ),
        ("connection_cancel", "connection.cancel"),
        ("connection_get", "connection.get"),
        ("connection_save", "connection.save"),
        ("connection_validate", "connection.validate"),
        ("notifications_get", "notifications.get"),
        ("notifications_set_muted", "notifications.setMuted"),
        ("profiles_add", "profiles.add"),
        ("profiles_get", "profiles.get"),
        ("profiles_remove", "profiles.remove"),
        ("profiles_rename", "profiles.rename"),
        ("profiles_switch", "profiles.switch"),
        ("extensions_authorize", "extensions.authorize"),
        ("extensions_choose_folder", "extensions.chooseFolder"),
        ("extensions_configure", "extensions.configure"),
        ("extensions_deauthorize", "extensions.deauthorize"),
        ("extensions_drawer", "extensions.drawer"),
        ("extensions_entries", "extensions.entries"),
        (
            "extensions_install_catalogue",
            "extensions.installFromCatalogue",
        ),
        ("extensions_install_local", "extensions.installLocal"),
        ("extensions_invoke", "extensions.invoke"),
        ("extensions_refresh", "extensions.refresh"),
        ("extensions_set_enabled", "extensions.setEnabled"),
        ("extensions_set_hook_consent", "extensions.setHookConsent"),
        ("extensions_state", "extensions.state"),
        ("extensions_supervisor", "extensions.supervisor"),
        ("extensions_uninstall", "extensions.uninstall"),
        ("side_panel_open_external", "sidePanel.openExternal"),
        ("side_panel_pages", "sidePanel.pages"),
        ("side_panel_permission_answer", "sidePanel.answerPermission"),
        ("side_panel_permission_remove", "sidePanel.removePermission"),
        ("side_panel_permissions", "sidePanel.permissions"),
        ("panel_send", "panel.send"),
    ];

    #[test]
    fn every_command_is_registered_and_answers_todo() {
        let app = mock_builder()
            .invoke_handler(super::handler())
            .build(mock_context(noop_assets()))
            .expect("mock app");
        let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .expect("mock window");

        assert_eq!(STUB_COMMANDS.len(), 35, "stub commands answering todo");
        for (cmd, member) in STUB_COMMANDS {
            let res = get_ipc_response(
                &webview,
                InvokeRequest {
                    cmd: (*cmd).into(),
                    callback: CallbackFn(0),
                    error: CallbackFn(1),
                    url: LOCAL.parse().unwrap(),
                    body: InvokeBody::default(),
                    headers: Default::default(),
                    invoke_key: INVOKE_KEY.to_string(),
                },
            );
            let err = res.expect_err(cmd);
            let text = err.as_str().unwrap_or_default();
            assert!(
                text.starts_with(&format!(
                    "window.agentrq.{member} is not implemented yet (WP"
                )),
                "{cmd}: {err}"
            );
        }
    }

    fn body_str(body: tauri::ipc::InvokeResponseBody) -> String {
        match body {
            tauri::ipc::InvokeResponseBody::Json(s) => s,
            tauri::ipc::InvokeResponseBody::Raw(b) => String::from_utf8(b).unwrap(),
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
