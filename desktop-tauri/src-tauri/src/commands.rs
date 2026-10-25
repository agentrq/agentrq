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

/// `agentrq:update:check`
#[tauri::command]
pub fn update_check() -> Result<(), String> {
    todo("updates.check", "WP8")
}

/// `agentrq:update:get`
#[tauri::command]
pub fn update_get() -> Result<(), String> {
    todo("updates.get", "WP8")
}

/// `agentrq:update:install`
#[tauri::command]
pub fn update_install() -> Result<(), String> {
    todo("updates.installNow", "WP8")
}

/// `agentrq:update:install-via-script`
#[tauri::command]
pub fn update_install_via_script() -> Result<(), String> {
    todo("updates.installViaScript", "WP8")
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

    /// Every command, and the `window.agentrq` member it answers for.
    const COMMANDS: &[(&str, &str)] = &[
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
        ("files_open", "files.open"),
        ("clipboard_write", "clipboard.write"),
        ("emoji_show_panel", "emoji.showPanel"),
        ("dialog_choose_directory", "dialog.chooseDirectory"),
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
        ("update_check", "updates.check"),
        ("update_get", "updates.get"),
        ("update_install", "updates.installNow"),
        ("update_install_via_script", "updates.installViaScript"),
        ("side_panel_open_external", "sidePanel.openExternal"),
        ("side_panel_pages", "sidePanel.pages"),
        ("side_panel_permission_answer", "sidePanel.answerPermission"),
        ("side_panel_permission_remove", "sidePanel.removePermission"),
        ("side_panel_permissions", "sidePanel.permissions"),
        ("panel_send", "panel.send"),
        ("view_fullscreen", "view.toggleFullScreen"),
        ("view_get", "view.get"),
        ("view_zoom", "view.zoom"),
        ("theme_set", "theme.set"),
    ];

    /// Commands a work package has implemented, in `chrome.rs` and beside it:
    /// they no longer answer `todo`, so they are not asked to.
    const IMPLEMENTED: &[&str] = &[
        "files_open",
        "clipboard_write",
        "emoji_show_panel",
        "dialog_choose_directory",
        "view_fullscreen",
        "view_get",
        "view_zoom",
        "theme_set",
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

        assert_eq!(COMMANDS.len(), 47, "one command per Electron IPC channel");
        for (cmd, member) in COMMANDS
            .iter()
            .filter(|(cmd, _)| !IMPLEMENTED.contains(cmd))
        {
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
}
