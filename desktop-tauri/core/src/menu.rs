// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Application menu, as data.
//!
//! Two kinds of entry live here: the standard platform roles a desktop app is
//! expected to have, and the AgentRQ actions the web app has no equivalent for:
//! creating a task from anywhere, switching server, signing out, checking for
//! updates and toggling the side panel.
//!
//! The model is plain data so it can be asserted on without a window. The shell
//! turns it into a native menu, and dispatches a click by [`ActionId::as_str`].

/// Global shortcut for quick task creation.
///
/// `Shift` keeps it clear of the browser-standard "new window" on Cmd/Ctrl+N,
/// and it is registered globally, so it must not collide with a common system
/// binding.
pub const QUICK_CREATE_ACCELERATOR: &str = "CmdOrCtrl+Shift+N";

/// Opens and closes the side panel.
///
/// A menu accelerator rather than a key the page listens for, so it works
/// whatever has focus, including the page in the panel, which never sees the
/// app's own key handlers.
pub const SIDE_PANEL_ACCELERATOR: &str = "CmdOrCtrl+\\";

/// Default application name, the macOS application menu's label.
pub const DEFAULT_APP_NAME: &str = "AgentRQ";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Platform {
    MacOs,
    Windows,
    Linux,
}

/// Standard platform items, which the shell maps to the native predefined ones.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    About,
    Services,
    Hide,
    HideOthers,
    Unhide,
    Quit,
    Close,
    Reload,
    ForceReload,
    ToggleDevTools,
    ResetZoom,
    ZoomIn,
    ZoomOut,
    ToggleFullscreen,
}

/// A whole submenu the platform fills in: its standard Edit and Window items.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RoleMenu {
    Edit,
    Window,
}

/// The AgentRQ actions.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ActionId {
    SwitchServer,
    LogOut,
    NewTask,
    CheckForUpdates,
    ToggleSidePanel,
}

impl ActionId {
    /// The id a click is dispatched by.
    pub fn as_str(self) -> &'static str {
        match self {
            ActionId::SwitchServer => "switch-server",
            ActionId::LogOut => "log-out",
            ActionId::NewTask => "new-task",
            ActionId::CheckForUpdates => "check-for-updates",
            ActionId::ToggleSidePanel => "toggle-side-panel",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActionItem {
    pub id: ActionId,
    pub label: &'static str,
    pub accelerator: Option<&'static str>,
    pub enabled: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MenuItem {
    Separator,
    Role(Role),
    Action(ActionItem),
}

/// A top-level menu.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MenuEntry {
    Submenu { label: String, items: Vec<MenuItem> },
    Role { label: &'static str, menu: RoleMenu },
}

/// What the menu is built from.
#[derive(Debug, Clone, Copy)]
pub struct MenuOptions<'a> {
    pub platform: Platform,
    pub app_name: &'a str,
    /// Offer the developer tools. Debug builds only.
    pub debug_build: bool,
    /// An updater is wired in. Without one, Check for Updates is present but
    /// disabled: an item that silently does nothing is worse than one that is
    /// visibly not ready, and the menu does not shift under users later.
    pub can_check_for_updates: bool,
    /// Something can open the side panel. Without it the item is disabled.
    pub can_toggle_side_panel: bool,
}

impl MenuOptions<'_> {
    /// A release menu with no updater and no side panel wired in.
    pub fn new(platform: Platform) -> Self {
        Self {
            platform,
            app_name: DEFAULT_APP_NAME,
            debug_build: false,
            can_check_for_updates: false,
            can_toggle_side_panel: false,
        }
    }
}

fn account_items() -> [MenuItem; 2] {
    [
        MenuItem::Action(ActionItem {
            id: ActionId::SwitchServer,
            label: "Switch Server…",
            accelerator: None,
            enabled: true,
        }),
        MenuItem::Action(ActionItem {
            id: ActionId::LogOut,
            label: "Log Out",
            accelerator: None,
            enabled: true,
        }),
    ]
}

fn new_task_item() -> MenuItem {
    MenuItem::Action(ActionItem {
        id: ActionId::NewTask,
        label: "New Task",
        accelerator: Some(QUICK_CREATE_ACCELERATOR),
        enabled: true,
    })
}

fn update_item(enabled: bool) -> MenuItem {
    MenuItem::Action(ActionItem {
        id: ActionId::CheckForUpdates,
        label: "Check for Updates…",
        accelerator: None,
        enabled,
    })
}

fn view_items(options: &MenuOptions<'_>) -> Vec<MenuItem> {
    let mut items = vec![
        MenuItem::Role(Role::Reload),
        MenuItem::Role(Role::ForceReload),
    ];
    if options.debug_build {
        items.push(MenuItem::Role(Role::ToggleDevTools));
    }
    items.extend([
        MenuItem::Separator,
        MenuItem::Action(ActionItem {
            id: ActionId::ToggleSidePanel,
            label: "Side Panel",
            accelerator: Some(SIDE_PANEL_ACCELERATOR),
            enabled: options.can_toggle_side_panel,
        }),
        MenuItem::Separator,
        MenuItem::Role(Role::ResetZoom),
        MenuItem::Role(Role::ZoomIn),
        MenuItem::Role(Role::ZoomOut),
        MenuItem::Separator,
        MenuItem::Role(Role::ToggleFullscreen),
    ]);
    items
}

/// The menu bar for a platform: the macOS application menu first, or on
/// Windows and Linux everything under File with a Help menu at the end.
pub fn build_menu(options: MenuOptions<'_>) -> Vec<MenuEntry> {
    let is_mac = options.platform == Platform::MacOs;
    let update = update_item(options.can_check_for_updates);
    let [switch_server, log_out] = account_items();
    let mut menu = Vec::new();

    if is_mac {
        // macOS convention: about and update checks at the top of the
        // application menu, account actions above Quit.
        menu.push(MenuEntry::Submenu {
            label: options.app_name.to_string(),
            items: vec![
                MenuItem::Role(Role::About),
                MenuItem::Separator,
                update,
                MenuItem::Separator,
                switch_server,
                log_out,
                MenuItem::Separator,
                MenuItem::Role(Role::Services),
                MenuItem::Separator,
                MenuItem::Role(Role::Hide),
                MenuItem::Role(Role::HideOthers),
                MenuItem::Role(Role::Unhide),
                MenuItem::Separator,
                MenuItem::Role(Role::Quit),
            ],
        });
        menu.push(MenuEntry::Submenu {
            label: "File".to_string(),
            items: vec![
                new_task_item(),
                MenuItem::Separator,
                MenuItem::Role(Role::Close),
            ],
        });
    } else {
        // Elsewhere there is no application menu, so everything lives under
        // File alongside Quit.
        menu.push(MenuEntry::Submenu {
            label: "File".to_string(),
            items: vec![
                new_task_item(),
                MenuItem::Separator,
                switch_server,
                log_out,
                MenuItem::Separator,
                update,
                MenuItem::Separator,
                MenuItem::Role(Role::Quit),
            ],
        });
    }

    menu.push(MenuEntry::Role {
        label: "Edit",
        menu: RoleMenu::Edit,
    });
    menu.push(MenuEntry::Submenu {
        label: "View".to_string(),
        items: view_items(&options),
    });
    menu.push(MenuEntry::Role {
        label: "Window",
        menu: RoleMenu::Window,
    });

    if !is_mac {
        menu.push(MenuEntry::Submenu {
            label: "Help".to_string(),
            items: vec![MenuItem::Role(Role::About)],
        });
    }

    menu
}

/// Every action item carrying the given id, in any menu.
pub fn find_menu_items(menu: &[MenuEntry], id: ActionId) -> Vec<&ActionItem> {
    menu.iter()
        .flat_map(|entry| match entry {
            MenuEntry::Submenu { items, .. } => items.as_slice(),
            MenuEntry::Role { .. } => &[],
        })
        .filter_map(|item| match item {
            MenuItem::Action(action) if action.id == id => Some(action),
            _ => None,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    const PLATFORMS: [Platform; 3] = [Platform::MacOs, Platform::Windows, Platform::Linux];

    fn full(platform: Platform) -> MenuOptions<'static> {
        MenuOptions {
            can_check_for_updates: true,
            can_toggle_side_panel: true,
            ..MenuOptions::new(platform)
        }
    }

    fn menu(platform: Platform) -> Vec<MenuEntry> {
        build_menu(full(platform))
    }

    fn labels(menu: &[MenuEntry]) -> Vec<String> {
        menu.iter()
            .map(|e| match e {
                MenuEntry::Submenu { label, .. } => label.clone(),
                MenuEntry::Role { label, .. } => label.to_string(),
            })
            .collect()
    }

    fn entry<'a>(menu: &'a [MenuEntry], name: &str) -> &'a MenuEntry {
        menu.iter()
            .find(|e| labels(std::slice::from_ref(e))[0] == name)
            .unwrap()
    }

    fn only(menu: &[MenuEntry]) -> &ActionItem {
        find_menu_items(menu, ActionId::SwitchServer)[0]
    }

    fn has_role(menu: &[MenuEntry], role: Role) -> bool {
        menu.iter().any(|e| match e {
            MenuEntry::Submenu { items, .. } => items.contains(&MenuItem::Role(role)),
            MenuEntry::Role { .. } => false,
        })
    }

    // The JS template's names for each role, for the golden outlines below.
    fn role_name(role: Role) -> &'static str {
        match role {
            Role::About => "about",
            Role::Services => "services",
            Role::Hide => "hide",
            Role::HideOthers => "hideOthers",
            Role::Unhide => "unhide",
            Role::Quit => "quit",
            Role::Close => "close",
            Role::Reload => "reload",
            Role::ForceReload => "forceReload",
            Role::ToggleDevTools => "toggleDevTools",
            Role::ResetZoom => "resetZoom",
            Role::ZoomIn => "zoomIn",
            Role::ZoomOut => "zoomOut",
            Role::ToggleFullscreen => "togglefullscreen",
        }
    }

    fn outline(menu: &[MenuEntry]) -> Vec<String> {
        menu.iter()
            .map(|e| match e {
                MenuEntry::Role { label, menu } => {
                    let name = match menu {
                        RoleMenu::Edit => "editMenu",
                        RoleMenu::Window => "windowMenu",
                    };
                    format!("{label} => role:{name}")
                }
                MenuEntry::Submenu { label, items } => {
                    let items: Vec<String> = items
                        .iter()
                        .map(|item| match item {
                            MenuItem::Separator => "---".to_string(),
                            MenuItem::Role(role) => format!("role:{}", role_name(*role)),
                            MenuItem::Action(a) => format!(
                                "item:{}|{}|{}|{}",
                                a.id.as_str(),
                                a.label,
                                a.accelerator.unwrap_or(""),
                                if a.enabled { "enabled" } else { "disabled" }
                            ),
                        })
                        .collect();
                    format!("{label} => {}", items.join(" ; "))
                }
            })
            .collect()
    }

    // --- buildMenuTemplate ---

    #[test]
    fn offers_switch_server_and_log_out_on_macos() {
        let menu = menu(Platform::MacOs);

        assert_eq!(find_menu_items(&menu, ActionId::SwitchServer).len(), 1);
        assert_eq!(find_menu_items(&menu, ActionId::LogOut).len(), 1);
    }

    #[test]
    fn offers_them_on_windows_and_linux_too() {
        // These are the only way to reach either action, so a platform that
        // lost them would strand the user on one server.
        for platform in [Platform::Windows, Platform::Linux] {
            let menu = menu(platform);
            assert_eq!(find_menu_items(&menu, ActionId::SwitchServer).len(), 1);
            assert_eq!(find_menu_items(&menu, ActionId::LogOut).len(), 1);
        }
    }

    #[test]
    fn puts_account_actions_in_the_application_menu_on_macos_and_under_file_elsewhere() {
        let mac = menu(Platform::MacOs);
        assert_eq!(labels(&mac)[0], "AgentRQ");
        assert_eq!(find_menu_items(&mac[..1], ActionId::SwitchServer).len(), 1);

        let linux = menu(Platform::Linux);
        let file = entry(&linux, "File");
        assert_eq!(
            find_menu_items(std::slice::from_ref(file), ActionId::SwitchServer).len(),
            1
        );
    }

    #[test]
    fn has_no_application_menu_on_windows_and_linux() {
        let menu = menu(Platform::Windows);
        assert!(!labels(&menu).contains(&"AgentRQ".to_string()));
    }

    #[test]
    fn carries_the_id_a_click_is_dispatched_by() {
        // The JS wired a handler per item; the shell dispatches by this id.
        let menu = menu(Platform::MacOs);
        let ids: Vec<&str> = [
            ActionId::SwitchServer,
            ActionId::LogOut,
            ActionId::NewTask,
            ActionId::CheckForUpdates,
            ActionId::ToggleSidePanel,
        ]
        .into_iter()
        .map(|id| {
            assert_eq!(find_menu_items(&menu, id)[0].id, id);
            id.as_str()
        })
        .collect();
        assert_eq!(
            ids,
            [
                "switch-server",
                "log-out",
                "new-task",
                "check-for-updates",
                "toggle-side-panel"
            ]
        );
    }

    #[test]
    fn keeps_quit_reachable_on_every_platform() {
        for platform in PLATFORMS {
            assert!(has_role(&menu(platform), Role::Quit), "{platform:?}");
        }
    }

    #[test]
    fn defaults_its_name_and_tolerates_no_actions_at_all() {
        let menu = build_menu(MenuOptions::new(Platform::MacOs));

        assert_eq!(labels(&menu)[0], "AgentRQ");
        assert!(only(&menu).enabled);
    }

    #[test]
    fn labels_the_application_menu_with_the_app_name() {
        let menu = build_menu(MenuOptions {
            app_name: "AgentRQ Dev",
            ..MenuOptions::new(Platform::MacOs)
        });

        assert_eq!(labels(&menu)[0], "AgentRQ Dev");
    }

    // --- buildMenuTemplate: AgentRQ actions ---

    #[test]
    fn offers_new_task_on_every_platform_with_the_global_accelerator() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            let items = find_menu_items(&menu, ActionId::NewTask);
            assert_eq!(items.len(), 1, "{platform:?}");
            assert_eq!(items[0].label, "New Task");
            assert_eq!(items[0].accelerator, Some(QUICK_CREATE_ACCELERATOR));
        }
    }

    #[test]
    fn keeps_the_quick_create_shortcut_clear_of_the_standard_new_window_binding() {
        // It is registered globally, so it must not collide with Cmd/Ctrl+N.
        assert_eq!(QUICK_CREATE_ACCELERATOR, "CmdOrCtrl+Shift+N");
    }

    #[test]
    fn offers_check_for_updates_on_every_platform() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            assert_eq!(
                find_menu_items(&menu, ActionId::CheckForUpdates).len(),
                1,
                "{platform:?}"
            );
        }
    }

    #[test]
    fn disables_check_for_updates_until_an_updater_is_wired_in() {
        let without = build_menu(MenuOptions::new(Platform::MacOs));
        assert!(!find_menu_items(&without, ActionId::CheckForUpdates)[0].enabled);

        let with = menu(Platform::MacOs);
        assert!(find_menu_items(&with, ActionId::CheckForUpdates)[0].enabled);
    }

    #[test]
    fn offers_the_side_panel_under_view_on_every_platform_on_cmd_ctrl_backslash() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            let view = entry(&menu, "View");
            let items = find_menu_items(std::slice::from_ref(view), ActionId::ToggleSidePanel);
            assert_eq!(items.len(), 1, "{platform:?}");
            assert_eq!(items[0].label, "Side Panel");
            assert_eq!(items[0].accelerator, Some("CmdOrCtrl+\\"));
            assert!(items[0].enabled);
        }
        assert_eq!(SIDE_PANEL_ACCELERATOR, "CmdOrCtrl+\\");
    }

    #[test]
    fn disables_the_side_panel_item_when_nothing_can_open_it() {
        let menu = build_menu(MenuOptions::new(Platform::Linux));

        assert!(!find_menu_items(&menu, ActionId::ToggleSidePanel)[0].enabled);
    }

    #[test]
    fn gives_windows_and_linux_a_help_menu_which_macos_gets_for_free() {
        assert!(labels(&menu(Platform::Linux)).contains(&"Help".to_string()));
        assert!(!labels(&menu(Platform::MacOs)).contains(&"Help".to_string()));
    }

    // --- the layout, against the JS template's output ---
    // The expected lines are `buildMenuTemplate`'s output, with the accelerators
    // spelled as Tauri spells them.

    #[test]
    fn lays_out_the_macos_menu_as_the_js_template_does() {
        assert_eq!(
            outline(&build_menu(MenuOptions {
                debug_build: true,
                ..full(Platform::MacOs)
            })),
            [
                "AgentRQ => role:about ; --- ; item:check-for-updates|Check for Updates…||enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; role:services ; --- ; role:hide ; role:hideOthers ; role:unhide ; --- ; role:quit",
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; role:close",
                "Edit => role:editMenu",
                "View => role:reload ; role:forceReload ; role:toggleDevTools ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|enabled ; --- ; role:resetZoom ; role:zoomIn ; role:zoomOut ; --- ; role:togglefullscreen",
                "Window => role:windowMenu",
            ]
        );
    }

    #[test]
    fn lays_out_the_linux_menu_as_the_js_template_does() {
        assert_eq!(
            outline(&build_menu(MenuOptions {
                debug_build: true,
                ..full(Platform::Linux)
            })),
            [
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; item:check-for-updates|Check for Updates…||enabled ; --- ; role:quit",
                "Edit => role:editMenu",
                "View => role:reload ; role:forceReload ; role:toggleDevTools ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|enabled ; --- ; role:resetZoom ; role:zoomIn ; role:zoomOut ; --- ; role:togglefullscreen",
                "Window => role:windowMenu",
                "Help => role:about",
            ]
        );
    }

    #[test]
    fn lays_out_windows_with_nothing_wired_as_the_js_template_does() {
        assert_eq!(
            outline(&build_menu(MenuOptions {
                debug_build: true,
                ..MenuOptions::new(Platform::Windows)
            })),
            [
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; item:check-for-updates|Check for Updates…||disabled ; --- ; role:quit",
                "Edit => role:editMenu",
                "View => role:reload ; role:forceReload ; role:toggleDevTools ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|disabled ; --- ; role:resetZoom ; role:zoomIn ; role:zoomOut ; --- ; role:togglefullscreen",
                "Window => role:windowMenu",
                "Help => role:about",
            ]
        );
    }

    // --- the developer tools (new: the JS always offered them) ---

    #[test]
    fn offers_the_developer_tools_only_in_debug_builds() {
        for platform in PLATFORMS {
            let release = menu(platform);
            assert!(!has_role(&release, Role::ToggleDevTools), "{platform:?}");
            // Everything else about View is unchanged.
            assert!(has_role(&release, Role::Reload));
            assert!(has_role(&release, Role::ForceReload));

            let debug = build_menu(MenuOptions {
                debug_build: true,
                ..full(platform)
            });
            assert!(has_role(&debug, Role::ToggleDevTools), "{platform:?}");
        }
    }

    #[test]
    fn keeps_zoom_and_fullscreen_in_every_build() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            for role in [
                Role::ResetZoom,
                Role::ZoomIn,
                Role::ZoomOut,
                Role::ToggleFullscreen,
            ] {
                assert!(has_role(&menu, role), "{platform:?} {role:?}");
            }
        }
    }

    // --- findMenuItems ---

    #[test]
    fn searches_every_submenu() {
        let menu = menu(Platform::Linux);

        assert_eq!(find_menu_items(&menu, ActionId::ToggleSidePanel).len(), 1);
        // A role menu holds no AgentRQ items.
        assert_eq!(
            find_menu_items(&menu[1..2], ActionId::NewTask),
            Vec::<&ActionItem>::new()
        );
    }

    #[test]
    fn returns_nothing_for_an_id_that_is_not_there() {
        assert!(find_menu_items(&[], ActionId::NewTask).is_empty());
        let view_only = [MenuEntry::Submenu {
            label: "View".to_string(),
            items: vec![MenuItem::Separator, MenuItem::Role(Role::Quit)],
        }];
        assert!(find_menu_items(&view_only, ActionId::LogOut).is_empty());
    }
}
