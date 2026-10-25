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
use agentrq_desktop_core::window_view::{toggle_full_screen, zoom_window};
use tauri::menu::{
    IsMenuItem, Menu, MenuEvent, MenuItem as NativeItem, PredefinedMenuItem, Submenu as NativeMenu,
};
use tauri::{AppHandle, Emitter, Manager, Runtime};

use crate::chrome::{self, Chrome};

const fn platform() -> Platform {
    if cfg!(target_os = "macos") {
        Platform::MacOs
    } else if cfg!(windows) {
        Platform::Windows
    } else {
        Platform::Linux
    }
}

/// `installMenu`.
pub fn install<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let name = app
        .config()
        .product_name
        .clone()
        .unwrap_or_else(|| agentrq_desktop_core::menu::DEFAULT_APP_NAME.to_string());
    let model = build_menu(MenuOptions {
        platform: platform(),
        app_name: &name,
        debug_build: cfg!(debug_assertions),
        // The updater is WP6/WP8's: until it is wired the item is disabled.
        can_check_for_updates: false,
        // The panel listens for `agentrq:side-panel:toggle` (WP7b).
        can_toggle_side_panel: true,
    });
    let menu = Menu::new(app)?;
    for submenu in &model {
        menu.append(&native_submenu(app, submenu)?)?;
    }
    app.set_menu(menu)?;
    app.on_menu_event(|app, event| dispatch(app, &event));
    Ok(())
}

fn native_submenu<R: Runtime>(app: &AppHandle<R>, model: &Submenu) -> tauri::Result<NativeMenu<R>> {
    let submenu = NativeMenu::new(app, &model.label, true)?;
    for item in &model.items {
        match item {
            MenuItem::Separator => submenu.append(&PredefinedMenuItem::separator(app)?)?,
            MenuItem::Role { role, label } => add_role(app, &submenu, *role, label)?,
            MenuItem::Action(action) => submenu.append(&NativeItem::with_id(
                app,
                action.id.as_str(),
                action.label,
                action.enabled,
                action.accelerator,
            )?)?,
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

/// A click, by the id it carries.
fn dispatch<R: Runtime>(app: &AppHandle<R>, event: &MenuEvent) {
    let Some(action) = ActionId::parse(event.id().as_ref()) else {
        return;
    };
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
        ActionId::Zoom => {
            let _ = if window.is_maximized().unwrap_or(false) {
                window.unmaximize()
            } else {
                window.maximize()
            };
        }
        ActionId::Close => {
            let _ = window.close();
        }
        ActionId::Quit => app.exit(0),
        // Switch Server and Log Out are WP4's (the server config and the cookie
        // jar), Check for Updates is WP6's: all disabled or inert until then.
        _ => {}
    }
}
