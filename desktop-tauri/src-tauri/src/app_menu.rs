// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The application menu: `core::menu`'s data turned into a native menu, and a
//! click carried out. Wiring, outside the coverage gate like `chrome.rs`; the
//! layout, labels and accelerators are `core::menu`'s, and tested there.
//!
//! Zoom and full screen go through `core::window_view`, never a native zoom.

use agentrq_desktop_core::menu::{
    build_menu, ActionId, MenuItem, MenuOptions, Platform, Role, Submenu,
};
use agentrq_desktop_core::menu_keys::{dispatchable, item_parts, key_bindings};
use agentrq_desktop_core::tray::{
    build_tray_menu_template, tray_tooltip, RecentWorkspace, TrayMenuItem,
};
use agentrq_desktop_core::window_view::{toggle_full_screen, toggle_maximize, zoom_window};
use tauri::image::Image;
use tauri::menu::{
    IsMenuItem, Menu, MenuBuilder, MenuItem as NativeItem, MenuItemBuilder, PredefinedMenuItem,
    Submenu as NativeMenu,
};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager, Runtime};

use std::sync::Arc;

use crate::chrome::{self, Chrome};
use crate::profiles::Profiles;

const fn platform() -> Platform {
    if cfg!(target_os = "macos") {
        Platform::MacOs
    } else if cfg!(windows) {
        Platform::Windows
    } else {
        Platform::Linux
    }
}

fn app_name<R: Runtime>(app: &AppHandle<R>) -> String {
    app.config()
        .product_name
        .clone()
        .unwrap_or_else(|| agentrq_desktop_core::menu::DEFAULT_APP_NAME.to_string())
}

fn options(name: &str) -> MenuOptions<'_> {
    MenuOptions {
        platform: platform(),
        app_name: name,
        debug_build: cfg!(debug_assertions),
        // The updater is WP6/WP8's: until it is wired the item is disabled.
        can_check_for_updates: false,
        // The panel listens for `agentrq:side-panel:toggle` (WP7b).
        can_toggle_side_panel: true,
    }
}

/// `installMenu`.
pub fn install<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let model = build_menu(options(&app_name(app)));
    let menu = Menu::new(app)?;
    for submenu in &model {
        menu.append(&native_submenu(app, submenu)?)?;
    }
    app.set_menu(menu)?;
    app.on_menu_event(|app, event| {
        if let Some(action) = ActionId::parse(event.id().as_ref()) {
            run(app, action);
        }
    });
    Ok(())
}

/// The keys the page dispatches itself, for the bridge's `keydown` listener.
#[tauri::command]
pub fn menu_key_bindings<R: Runtime>(app: AppHandle<R>) -> serde_json::Value {
    let name = app_name(&app);
    key_bindings(options(&name))
        .iter()
        .map(|binding| binding.to_json())
        .collect()
}

/// An action the bridge dispatched from a key the page left unhandled.
#[tauri::command]
pub fn menu_run<R: Runtime>(app: AppHandle<R>, id: String) -> bool {
    // Only a key the page may dispatch here and now: a disabled item or another
    // platform's is refused, as is any other id.
    let name = app_name(&app);
    let action = dispatchable(options(&name), &id);
    if let Some(action) = action {
        run(&app, action);
    }
    action.is_some()
}

fn native_submenu<R: Runtime>(app: &AppHandle<R>, model: &Submenu) -> tauri::Result<NativeMenu<R>> {
    let submenu = NativeMenu::new(app, &model.label, true)?;
    for item in &model.items {
        match item {
            MenuItem::Separator => submenu.append(&PredefinedMenuItem::separator(app)?)?,
            MenuItem::Role { role, label } => add_role(app, &submenu, *role, label)?,
            MenuItem::Action(action) => {
                // Off macOS the keys are the page's, not GTK's or Windows':
                // see `core::menu_keys`.
                let (label, accelerator) = item_parts(platform(), action);
                submenu.append(&NativeItem::with_id(
                    app,
                    action.id.as_str(),
                    label,
                    action.enabled,
                    accelerator,
                )?)?;
            }
            MenuItem::Submenu(nested) => submenu.append(&native_submenu(app, nested)?)?,
        }
    }
    #[cfg(target_os = "macos")]
    match model.label.as_str() {
        "Window" => submenu.set_as_windows_menu_for_nsapp()?,
        "Help" => submenu.set_as_help_menu_for_nsapp()?,
        _ => {}
    }
    Ok(submenu)
}

fn add_role<R: Runtime>(
    app: &AppHandle<R>,
    submenu: &NativeMenu<R>,
    role: Role,
    label: &str,
) -> tauri::Result<()> {
    let text = Some(label);
    let item: Box<dyn IsMenuItem<R>> = match role {
        Role::About => Box::new(PredefinedMenuItem::about(app, text, None)?),
        Role::Services => Box::new(PredefinedMenuItem::services(app, text)?),
        Role::Hide => Box::new(PredefinedMenuItem::hide(app, text)?),
        Role::HideOthers => Box::new(PredefinedMenuItem::hide_others(app, text)?),
        Role::Unhide => Box::new(PredefinedMenuItem::show_all(app, text)?),
        Role::Quit => Box::new(PredefinedMenuItem::quit(app, text)?),
        Role::Close => Box::new(PredefinedMenuItem::close_window(app, text)?),
        Role::Undo => Box::new(PredefinedMenuItem::undo(app, text)?),
        Role::Redo => Box::new(PredefinedMenuItem::redo(app, text)?),
        Role::Cut => Box::new(PredefinedMenuItem::cut(app, text)?),
        Role::Copy => Box::new(PredefinedMenuItem::copy(app, text)?),
        Role::Paste => Box::new(PredefinedMenuItem::paste(app, text)?),
        Role::SelectAll => Box::new(PredefinedMenuItem::select_all(app, text)?),
        Role::Minimize => Box::new(PredefinedMenuItem::minimize(app, text)?),
        Role::Zoom => Box::new(PredefinedMenuItem::maximize(app, text)?),
        Role::BringAllToFront => Box::new(PredefinedMenuItem::bring_all_to_front(app, text)?),
        // muda has no native item for these three: shown, not enabled.
        Role::PasteAndMatchStyle | Role::Delete | Role::StartSpeaking | Role::StopSpeaking => {
            Box::new(NativeItem::new(app, label, false, None::<&str>)?)
        }
    };
    submenu.append(item.as_ref())
}

/// A click, or a key, carried out.
fn run<R: Runtime>(app: &AppHandle<R>, action: ActionId) {
    let Some(window) = app.get_webview_window(chrome::MAIN) else {
        return;
    };
    let state = app.state::<Chrome>();
    let view = chrome::view_of(&window, &state);
    if let Some(direction) = action.zoom_direction() {
        zoom_window(&view, direction);
        return;
    }
    match action {
        ActionId::ToggleFullScreen => {
            toggle_full_screen(&view);
        }
        ActionId::ToggleSidePanel => {
            let _ = app.emit_to(chrome::MAIN, "agentrq:side-panel:toggle", ());
        }
        // WP6 gives this the most recent workspace, as `newTaskRoute` does.
        ActionId::NewTask => {
            let _ = app.emit_to(chrome::MAIN, "agentrq:navigate", "/");
        }
        ActionId::Reload | ActionId::ForceReload => {
            let _ = window.eval("location.reload()");
        }
        #[cfg(debug_assertions)]
        ActionId::ToggleDevTools => window.open_devtools(),
        ActionId::Undo => {
            let _ = window.eval("document.execCommand('undo')");
        }
        ActionId::Redo => {
            let _ = window.eval("document.execCommand('redo')");
        }
        ActionId::Delete => {
            let _ = window.eval("document.execCommand('delete')");
        }
        ActionId::Minimize => {
            let _ = window.minimize();
        }
        ActionId::Zoom => toggle_maximize(&view),
        ActionId::Close => {
            let _ = window.close();
        }
        ActionId::Quit => app.exit(0),
        ActionId::LogOut => {
            let profiles = app.state::<Arc<Profiles>>().inner().clone();
            tauri::async_runtime::spawn(async move { profiles.log_out().await });
        }
        ActionId::SwitchServer => {
            let profiles = app.state::<Arc<Profiles>>().inner().clone();
            tauri::async_runtime::spawn(async move { profiles.switch_server().await });
        }
        // Check for Updates is WP6's: disabled until then.
        _ => {}
    }
}

/// Install the system tray icon and initial menu.
/// The tray's Quit. Here, outside the coverage gate, because the line that
/// exits is compiled out of the tests, which must not end the runner.
pub fn quit<R: Runtime>(app: &AppHandle<R>) {
    #[cfg(not(test))]
    app.exit(0);
    #[cfg(test)]
    let _ = app;
}

pub fn install_tray<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let icon_bytes = include_bytes!("../icons/icon.png");
    let img = Image::from_bytes(icon_bytes)?;
    let app_click = app.clone();
    let app_menu = app.clone();
    let _tray = TrayIconBuilder::with_id("main")
        .icon(img)
        .tooltip(tray_tooltip(0))
        .show_menu_on_left_click(false)
        .on_tray_icon_event(move |_tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                crate::shell::focus_main_window(&app_click);
            }
        })
        .on_menu_event(move |_app, event| {
            crate::shell::handle_tray_menu_event(&app_menu, event.id.as_ref());
        })
        .build(app)?;
    update_tray(app, &[], 0);
    Ok(())
}

/// Update the system tray menu and tooltip with the latest unread count and recent workspaces.
pub fn update_tray<R: Runtime>(app: &AppHandle<R>, workspaces: &[RecentWorkspace], unread: i64) {
    if let Some(tray) = app.tray_by_id("main") {
        let template = build_tray_menu_template(workspaces, unread);
        let tooltip = tray_tooltip(unread);
        if let Ok(menu) = build_tray_menu(app, &template) {
            let _ = tray.set_menu(Some(menu));
        }
        let _ = tray.set_tooltip(Some(&tooltip));
    }
}

/// Convert a tray template into a native Tauri Menu.
fn build_tray_menu<R: Runtime>(
    app: &AppHandle<R>,
    template: &[TrayMenuItem],
) -> tauri::Result<tauri::menu::Menu<R>> {
    let mut builder = MenuBuilder::new(app);
    for item in template {
        match item {
            TrayMenuItem::Separator => {
                let sep = PredefinedMenuItem::separator(app)?;
                builder = builder.item(&sep);
            }
            TrayMenuItem::Item {
                id, label, enabled, ..
            } => {
                let menu_item = MenuItemBuilder::with_id(id, label)
                    .enabled(*enabled)
                    .build(app)?;
                builder = builder.item(&menu_item);
            }
        }
    }
    builder.build()
}
