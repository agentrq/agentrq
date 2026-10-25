// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

use std::path::Path;

/// Every command the app defines. Naming them makes Tauri require a permission
/// per command, so a capability grants exactly these (`test/parity.test.js` keeps
/// this list, `commands.rs`, its handler and the capability in step).
const COMMANDS: &[&str] = &[
    "attachments_forget_all",
    "attachments_forget_task",
    "attachments_forget_workspace",
    "connection_cancel",
    "connection_get",
    "connection_save",
    "connection_validate",
    "notifications_get",
    "notifications_set_muted",
    "profiles_add",
    "profiles_get",
    "profiles_remove",
    "profiles_rename",
    "profiles_switch",
    "files_open",
    "clipboard_write",
    "emoji_show_panel",
    "dialog_choose_directory",
    "extensions_authorize",
    "extensions_choose_folder",
    "extensions_configure",
    "extensions_deauthorize",
    "extensions_drawer",
    "extensions_entries",
    "extensions_install_catalogue",
    "extensions_install_local",
    "extensions_invoke",
    "extensions_refresh",
    "extensions_set_enabled",
    "extensions_set_hook_consent",
    "extensions_state",
    "extensions_supervisor",
    "extensions_uninstall",
    "update_check",
    "update_get",
    "update_install",
    "update_install_via_script",
    "side_panel_open_external",
    "side_panel_pages",
    "side_panel_permission_answer",
    "side_panel_permission_remove",
    "side_panel_permissions",
    "panel_send",
    "view_fullscreen",
    "view_get",
    "view_zoom",
    "theme_set",
    "event_source_open",
    "event_source_close",
    "menu_key_bindings",
    "menu_run",
];

fn main() {
    let app = tauri_build::AppManifest::new().commands(COMMANDS);
    // tauri-build embeds the Common Controls v6 manifest in the app binary
    // only. A test binary without it cannot load on Windows
    // (STATUS_ENTRYPOINT_NOT_FOUND), so the linker embeds it in every one.
    let msvc = std::env::var("CARGO_CFG_TARGET_ENV").as_deref() == Ok("msvc");
    let attributes = if msvc {
        let manifest = Path::new(env!("CARGO_MANIFEST_DIR")).join("windows-app-manifest.xml");
        println!("cargo:rerun-if-changed={}", manifest.display());
        println!("cargo:rustc-link-arg=/MANIFEST:EMBED");
        println!("cargo:rustc-link-arg=/MANIFESTINPUT:{}", manifest.display());
        tauri_build::Attributes::new()
            .app_manifest(app)
            .windows_attributes(tauri_build::WindowsAttributes::new_without_app_manifest())
    } else {
        tauri_build::Attributes::new().app_manifest(app)
    };
    tauri_build::try_build(attributes).expect("tauri-build failed");
}
