// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Application menu, as data.
//!
//! Three kinds of item live here: the platform items muda draws natively
//! ([`Role`]); the standard items muda has no native item for, which the shell
//! carries out itself (reload, developer tools, zoom, full screen, and on some
//! platforms quit, close, undo...); and the AgentRQ actions the web app has no
//! equivalent for: creating a task from anywhere, switching server, signing
//! out, checking for updates and toggling the side panel.
//!
//! Labels and accelerators are Electron's role defaults, spelled as muda parses
//! them. The model is plain data so it can be asserted on without a window. The
//! shell turns it into a native menu, and dispatches a click by
//! [`ActionId::as_str`].

use crate::window_view::ZoomDirection;

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

/// Zoom In on macOS: Electron's `CmdOrCtrl+Plus`, whose `Plus` is the shifted
/// `=` key. muda has no `Plus` key name and would refuse the accelerator.
pub const ZOOM_IN_ACCELERATOR: &str = "Shift+CmdOrCtrl+=";

/// Zoom In elsewhere: GTK matches `Shift+Ctrl+=` against the key `plus`, so it
/// never fires. [`crate::menu_keys`] adds the `+` forms.
pub const ZOOM_IN_PLAIN_ACCELERATOR: &str = "CmdOrCtrl+=";

/// Default application name, the macOS application menu's label.
pub const DEFAULT_APP_NAME: &str = "AgentRQ";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Platform {
    MacOs,
    Windows,
    Linux,
}

/// An item muda draws natively, with its own behaviour and accelerator; the
/// shell passes the label. The menu uses one only on a platform where muda
/// supports it, and makes the same item an [`ActionItem`] elsewhere: GTK 3
/// supports only cut, copy, paste, select all and about, Windows adds undo,
/// redo, minimize, zoom, close and quit, and macOS has them all.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    About,
    Services,
    Hide,
    HideOthers,
    /// muda's `show_all`.
    Unhide,
    Quit,
    /// muda's `close_window`.
    Close,
    Undo,
    Redo,
    Cut,
    Copy,
    Paste,
    PasteAndMatchStyle,
    Delete,
    SelectAll,
    Minimize,
    /// muda's `maximize`, which macOS calls Zoom.
    Zoom,
    BringAllToFront,
    StartSpeaking,
    StopSpeaking,
}

/// The items the shell carries out itself.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ActionId {
    SwitchServer,
    LogOut,
    NewTask,
    CheckForUpdates,
    ToggleSidePanel,
    // View: muda has no reload or developer tools item on any platform.
    Reload,
    ForceReload,
    ToggleDevTools,
    /// Zoom goes through [`crate::window_view::zoom_window`] and its
    /// `ZOOM_PERCENTS` steps, never native zoom: see [`ActionId::zoom_direction`].
    ResetZoom,
    ZoomIn,
    ZoomOut,
    /// Through [`crate::window_view::toggle_full_screen`].
    ToggleFullScreen,
    // Standard items, on the platforms where muda has no native one.
    Undo,
    Redo,
    Delete,
    Minimize,
    Zoom,
    Close,
    Quit,
}

impl ActionId {
    /// Every action, for turning a clicked id back into one.
    pub const ALL: [ActionId; 19] = [
        ActionId::SwitchServer,
        ActionId::LogOut,
        ActionId::NewTask,
        ActionId::CheckForUpdates,
        ActionId::ToggleSidePanel,
        ActionId::Reload,
        ActionId::ForceReload,
        ActionId::ToggleDevTools,
        ActionId::ResetZoom,
        ActionId::ZoomIn,
        ActionId::ZoomOut,
        ActionId::ToggleFullScreen,
        ActionId::Undo,
        ActionId::Redo,
        ActionId::Delete,
        ActionId::Minimize,
        ActionId::Zoom,
        ActionId::Close,
        ActionId::Quit,
    ];

    /// The action a clicked item's id names, `None` for an id that is not one.
    pub fn parse(id: &str) -> Option<ActionId> {
        Self::ALL.into_iter().find(|action| action.as_str() == id)
    }

    /// The id a click is dispatched by.
    pub fn as_str(self) -> &'static str {
        match self {
            ActionId::SwitchServer => "switch-server",
            ActionId::LogOut => "log-out",
            ActionId::NewTask => "new-task",
            ActionId::CheckForUpdates => "check-for-updates",
            ActionId::ToggleSidePanel => "toggle-side-panel",
            ActionId::Reload => "reload",
            ActionId::ForceReload => "force-reload",
            ActionId::ToggleDevTools => "toggle-dev-tools",
            ActionId::ResetZoom => "reset-zoom",
            ActionId::ZoomIn => "zoom-in",
            ActionId::ZoomOut => "zoom-out",
            ActionId::ToggleFullScreen => "toggle-full-screen",
            ActionId::Undo => "undo",
            ActionId::Redo => "redo",
            ActionId::Delete => "delete",
            ActionId::Minimize => "minimize",
            ActionId::Zoom => "zoom",
            ActionId::Close => "close",
            ActionId::Quit => "quit",
        }
    }

    /// The `window_view` step a zoom item takes, `None` for any other item.
    pub fn zoom_direction(self) -> Option<ZoomDirection> {
        match self {
            ActionId::ResetZoom => Some(ZoomDirection::Reset),
            ActionId::ZoomIn => Some(ZoomDirection::In),
            ActionId::ZoomOut => Some(ZoomDirection::Out),
            _ => None,
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
    Role { role: Role, label: String },
    Action(ActionItem),
    Submenu(Submenu),
}

/// A menu: one in the menu bar, or one nested in another.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Submenu {
    pub label: String,
    pub items: Vec<MenuItem>,
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

fn submenu(label: &str, items: Vec<MenuItem>) -> Submenu {
    Submenu {
        label: label.to_string(),
        items,
    }
}

fn role(role: Role, label: &str) -> MenuItem {
    MenuItem::Role {
        role,
        label: label.to_string(),
    }
}

fn action(id: ActionId, label: &'static str, accelerator: Option<&'static str>) -> MenuItem {
    MenuItem::Action(ActionItem {
        id,
        label,
        accelerator,
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

/// Electron's `editMenu`. macOS's Substitutions submenu is left out: muda has
/// no items for it.
fn edit_items(platform: Platform) -> Vec<MenuItem> {
    let mut items = match platform {
        Platform::Linux => vec![
            action(ActionId::Undo, "Undo", Some("CmdOrCtrl+Z")),
            action(ActionId::Redo, "Redo", Some("Shift+CmdOrCtrl+Z")),
        ],
        _ => vec![role(Role::Undo, "Undo"), role(Role::Redo, "Redo")],
    };
    items.extend([
        MenuItem::Separator,
        role(Role::Cut, "Cut"),
        role(Role::Copy, "Copy"),
        role(Role::Paste, "Paste"),
    ]);
    if platform == Platform::MacOs {
        items.extend([
            role(Role::PasteAndMatchStyle, "Paste and Match Style"),
            role(Role::Delete, "Delete"),
            role(Role::SelectAll, "Select All"),
            MenuItem::Separator,
            MenuItem::Submenu(submenu(
                "Speech",
                vec![
                    role(Role::StartSpeaking, "Start Speaking"),
                    role(Role::StopSpeaking, "Stop Speaking"),
                ],
            )),
        ]);
    } else {
        items.extend([
            action(ActionId::Delete, "Delete", None),
            MenuItem::Separator,
            role(Role::SelectAll, "Select All"),
        ]);
    }
    items
}

fn view_items(options: &MenuOptions<'_>) -> Vec<MenuItem> {
    let is_mac = options.platform == Platform::MacOs;
    let mut items = vec![
        action(ActionId::Reload, "Reload", Some("CmdOrCtrl+R")),
        action(
            ActionId::ForceReload,
            "Force Reload",
            Some("Shift+CmdOrCtrl+R"),
        ),
    ];
    if options.debug_build {
        items.push(action(
            ActionId::ToggleDevTools,
            "Toggle Developer Tools",
            Some(if is_mac { "Alt+Cmd+I" } else { "Ctrl+Shift+I" }),
        ));
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
        action(ActionId::ResetZoom, "Actual Size", Some("CmdOrCtrl+0")),
        action(
            ActionId::ZoomIn,
            "Zoom In",
            Some(if is_mac {
                ZOOM_IN_ACCELERATOR
            } else {
                ZOOM_IN_PLAIN_ACCELERATOR
            }),
        ),
        action(ActionId::ZoomOut, "Zoom Out", Some("CmdOrCtrl+-")),
        MenuItem::Separator,
        action(
            ActionId::ToggleFullScreen,
            "Toggle Full Screen",
            Some(if is_mac { "Ctrl+Cmd+F" } else { "F11" }),
        ),
    ]);
    items
}

/// Electron's `windowMenu`.
fn window_items(platform: Platform) -> Vec<MenuItem> {
    match platform {
        Platform::MacOs => vec![
            role(Role::Minimize, "Minimize"),
            role(Role::Zoom, "Zoom"),
            MenuItem::Separator,
            role(Role::BringAllToFront, "Bring All to Front"),
        ],
        Platform::Windows => vec![
            role(Role::Minimize, "Minimize"),
            role(Role::Zoom, "Zoom"),
            role(Role::Close, "Close"),
        ],
        Platform::Linux => vec![
            action(ActionId::Minimize, "Minimize", Some("CmdOrCtrl+M")),
            action(ActionId::Zoom, "Zoom", None),
            action(ActionId::Close, "Close", Some("CmdOrCtrl+W")),
        ],
    }
}

/// The menu bar for a platform: the macOS application menu first, or on
/// Windows and Linux everything under File with a Help menu at the end.
pub fn build_menu(options: MenuOptions<'_>) -> Vec<Submenu> {
    let platform = options.platform;
    let app = options.app_name;
    let update = update_item(options.can_check_for_updates);
    let switch_server = action(ActionId::SwitchServer, "Switch Server…", None);
    let log_out = action(ActionId::LogOut, "Log Out", None);
    let new_task = action(
        ActionId::NewTask,
        "New Task",
        Some(QUICK_CREATE_ACCELERATOR),
    );
    let mut menu = Vec::new();

    match platform {
        Platform::MacOs => {
            // macOS convention: about and update checks at the top of the
            // application menu, account actions above Quit.
            menu.push(submenu(
                app,
                vec![
                    role(Role::About, &format!("About {app}")),
                    MenuItem::Separator,
                    update,
                    MenuItem::Separator,
                    switch_server,
                    log_out,
                    MenuItem::Separator,
                    role(Role::Services, "Services"),
                    MenuItem::Separator,
                    role(Role::Hide, &format!("Hide {app}")),
                    role(Role::HideOthers, "Hide Others"),
                    role(Role::Unhide, "Show All"),
                    MenuItem::Separator,
                    role(Role::Quit, &format!("Quit {app}")),
                ],
            ));
            menu.push(submenu(
                "File",
                vec![
                    new_task,
                    MenuItem::Separator,
                    role(Role::Close, "Close Window"),
                ],
            ));
        }
        Platform::Windows | Platform::Linux => {
            // Elsewhere there is no application menu, so everything lives under
            // File alongside Quit.
            let quit = if platform == Platform::Windows {
                role(Role::Quit, "Exit")
            } else {
                action(ActionId::Quit, "Quit", Some("CmdOrCtrl+Q"))
            };
            menu.push(submenu(
                "File",
                vec![
                    new_task,
                    MenuItem::Separator,
                    switch_server,
                    log_out,
                    MenuItem::Separator,
                    update,
                    MenuItem::Separator,
                    quit,
                ],
            ));
        }
    }

    menu.push(submenu("Edit", edit_items(platform)));
    menu.push(submenu("View", view_items(&options)));
    menu.push(submenu("Window", window_items(platform)));

    match platform {
        Platform::MacOs => {}
        Platform::Windows => menu.push(submenu(
            "Help",
            vec![role(Role::About, &format!("About {app}"))],
        )),
        Platform::Linux => menu.push(submenu("Help", vec![role(Role::About, "About")])),
    }

    menu
}

/// Every action item carrying the given id, in any menu at any depth.
pub fn find_menu_items(menu: &[Submenu], id: ActionId) -> Vec<&ActionItem> {
    let mut found = Vec::new();
    for submenu in menu {
        collect_items(&submenu.items, id, &mut found);
    }
    found
}

fn collect_items<'a>(items: &'a [MenuItem], id: ActionId, found: &mut Vec<&'a ActionItem>) {
    for item in items {
        match item {
            MenuItem::Action(action) if action.id == id => found.push(action),
            MenuItem::Submenu(submenu) => collect_items(&submenu.items, id, found),
            _ => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const PLATFORMS: [Platform; 3] = [Platform::MacOs, Platform::Windows, Platform::Linux];

    const ALL_ACTIONS: [ActionId; 19] = [
        ActionId::SwitchServer,
        ActionId::LogOut,
        ActionId::NewTask,
        ActionId::CheckForUpdates,
        ActionId::ToggleSidePanel,
        ActionId::Reload,
        ActionId::ForceReload,
        ActionId::ToggleDevTools,
        ActionId::ResetZoom,
        ActionId::ZoomIn,
        ActionId::ZoomOut,
        ActionId::ToggleFullScreen,
        ActionId::Undo,
        ActionId::Redo,
        ActionId::Delete,
        ActionId::Minimize,
        ActionId::Zoom,
        ActionId::Close,
        ActionId::Quit,
    ];

    fn full(platform: Platform) -> MenuOptions<'static> {
        MenuOptions {
            can_check_for_updates: true,
            can_toggle_side_panel: true,
            ..MenuOptions::new(platform)
        }
    }

    fn menu(platform: Platform) -> Vec<Submenu> {
        build_menu(full(platform))
    }

    fn debug_menu(platform: Platform) -> Vec<Submenu> {
        build_menu(MenuOptions {
            debug_build: true,
            ..full(platform)
        })
    }

    fn labels(menu: &[Submenu]) -> Vec<&str> {
        menu.iter().map(|s| s.label.as_str()).collect()
    }

    fn entry<'a>(menu: &'a [Submenu], name: &str) -> &'a Submenu {
        menu.iter()
            .find(|s| s.label == name)
            .expect("the menu is there")
    }

    fn only(menu: &[Submenu], id: ActionId) -> &ActionItem {
        let items = find_menu_items(menu, id);
        assert_eq!(items.len(), 1, "{id:?}");
        items[0]
    }

    /// Every role in the menu, at any depth.
    fn roles(items: &[MenuItem], found: &mut Vec<Role>) {
        for item in items {
            match item {
                MenuItem::Role { role, .. } => found.push(*role),
                MenuItem::Submenu(submenu) => roles(&submenu.items, found),
                _ => {}
            }
        }
    }

    fn all_roles(menu: &[Submenu]) -> Vec<Role> {
        let mut found = Vec::new();
        for submenu in menu {
            roles(&submenu.items, &mut found);
        }
        found
    }

    fn all_accelerators(menu: &[Submenu]) -> Vec<&'static str> {
        ALL_ACTIONS
            .iter()
            .flat_map(|id| find_menu_items(menu, *id))
            .filter_map(|item| item.accelerator)
            .collect()
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
            Role::Undo => "undo",
            Role::Redo => "redo",
            Role::Cut => "cut",
            Role::Copy => "copy",
            Role::Paste => "paste",
            Role::PasteAndMatchStyle => "pasteAndMatchStyle",
            Role::Delete => "delete",
            Role::SelectAll => "selectAll",
            Role::Minimize => "minimize",
            Role::Zoom => "zoom",
            Role::BringAllToFront => "front",
            Role::StartSpeaking => "startSpeaking",
            Role::StopSpeaking => "stopSpeaking",
        }
    }

    fn outline_items(items: &[MenuItem]) -> String {
        items
            .iter()
            .map(|item| match item {
                MenuItem::Separator => "---".to_string(),
                MenuItem::Role { role, label } => format!("role:{}|{label}", role_name(*role)),
                MenuItem::Action(a) => format!(
                    "item:{}|{}|{}|{}",
                    a.id.as_str(),
                    a.label,
                    a.accelerator.unwrap_or(""),
                    if a.enabled { "enabled" } else { "disabled" }
                ),
                MenuItem::Submenu(s) => format!("{} [{}]", s.label, outline_items(&s.items)),
            })
            .collect::<Vec<_>>()
            .join(" ; ")
    }

    fn outline(menu: &[Submenu]) -> Vec<String> {
        menu.iter()
            .map(|s| format!("{} => {}", s.label, outline_items(&s.items)))
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
        assert!(!labels(&menu).contains(&"AgentRQ"));
    }

    #[test]
    fn carries_the_id_a_click_is_dispatched_by() {
        // The JS wired a handler per item; the shell dispatches by this id.
        let ids: Vec<&str> = ALL_ACTIONS.iter().map(|id| id.as_str()).collect();
        assert_eq!(
            ids,
            [
                "switch-server",
                "log-out",
                "new-task",
                "check-for-updates",
                "toggle-side-panel",
                "reload",
                "force-reload",
                "toggle-dev-tools",
                "reset-zoom",
                "zoom-in",
                "zoom-out",
                "toggle-full-screen",
                "undo",
                "redo",
                "delete",
                "minimize",
                "zoom",
                "close",
                "quit",
            ]
        );
        let menu = menu(Platform::MacOs);
        for id in &ALL_ACTIONS[..5] {
            assert_eq!(only(&menu, *id).id, *id);
        }
    }

    #[test]
    fn keeps_quit_reachable_on_every_platform() {
        // Natively where muda has a Quit item, as an action where it does not.
        assert!(all_roles(&menu(Platform::MacOs)).contains(&Role::Quit));
        assert!(all_roles(&menu(Platform::Windows)).contains(&Role::Quit));
        assert_eq!(only(&menu(Platform::Linux), ActionId::Quit).label, "Quit");
    }

    #[test]
    fn defaults_its_name_and_tolerates_no_actions_at_all() {
        let menu = build_menu(MenuOptions::new(Platform::MacOs));

        assert_eq!(labels(&menu)[0], "AgentRQ");
        assert!(only(&menu, ActionId::SwitchServer).enabled);
        // With nothing wired in, the items that need wiring are visibly off.
        assert!(!only(&menu, ActionId::CheckForUpdates).enabled);
        assert!(!only(&menu, ActionId::ToggleSidePanel).enabled);
    }

    #[test]
    fn labels_the_application_menu_with_the_app_name() {
        let menu = build_menu(MenuOptions {
            app_name: "AgentRQ Dev",
            ..MenuOptions::new(Platform::MacOs)
        });

        assert_eq!(labels(&menu)[0], "AgentRQ Dev");
        assert_eq!(
            outline(&menu[..1])[0],
            "AgentRQ Dev => role:about|About AgentRQ Dev ; --- ; item:check-for-updates|Check for Updates…||disabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; role:services|Services ; --- ; role:hide|Hide AgentRQ Dev ; role:hideOthers|Hide Others ; role:unhide|Show All ; --- ; role:quit|Quit AgentRQ Dev"
        );
    }

    // --- buildMenuTemplate: AgentRQ actions ---

    #[test]
    fn offers_new_task_on_every_platform_with_the_global_accelerator() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            let item = only(&menu, ActionId::NewTask);
            assert_eq!(item.label, "New Task");
            assert_eq!(item.accelerator, Some(QUICK_CREATE_ACCELERATOR));
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
        assert!(!only(&without, ActionId::CheckForUpdates).enabled);

        let with = menu(Platform::MacOs);
        assert!(only(&with, ActionId::CheckForUpdates).enabled);
    }

    #[test]
    fn offers_the_side_panel_under_view_on_every_platform_on_cmd_ctrl_backslash() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            let view = entry(&menu, "View");
            let item = only(std::slice::from_ref(view), ActionId::ToggleSidePanel);
            assert_eq!(item.label, "Side Panel");
            assert_eq!(item.accelerator, Some("CmdOrCtrl+\\"));
            assert!(item.enabled);
        }
        assert_eq!(SIDE_PANEL_ACCELERATOR, "CmdOrCtrl+\\");
    }

    #[test]
    fn disables_the_side_panel_item_when_nothing_can_open_it() {
        let menu = build_menu(MenuOptions::new(Platform::Linux));

        assert!(!only(&menu, ActionId::ToggleSidePanel).enabled);
    }

    #[test]
    fn gives_windows_and_linux_a_help_menu_which_macos_gets_for_free() {
        assert!(labels(&menu(Platform::Linux)).contains(&"Help"));
        assert!(!labels(&menu(Platform::MacOs)).contains(&"Help"));
    }

    // --- the layout, against the JS template expanded by Electron's roles ---
    // The expected lines are `buildMenuTemplate`'s output with each role
    // expanded as Electron 44 expands it (`menu-item-roles.ts`), with the
    // accelerators spelled as muda parses them. A `role:` is native on that
    // platform; an `item:` is one muda has no native item for there.

    #[test]
    fn lays_out_the_macos_menu_as_electron_does() {
        assert_eq!(
            outline(&debug_menu(Platform::MacOs)),
            [
                "AgentRQ => role:about|About AgentRQ ; --- ; item:check-for-updates|Check for Updates…||enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; role:services|Services ; --- ; role:hide|Hide AgentRQ ; role:hideOthers|Hide Others ; role:unhide|Show All ; --- ; role:quit|Quit AgentRQ",
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; role:close|Close Window",
                "Edit => role:undo|Undo ; role:redo|Redo ; --- ; role:cut|Cut ; role:copy|Copy ; role:paste|Paste ; role:pasteAndMatchStyle|Paste and Match Style ; role:delete|Delete ; role:selectAll|Select All ; --- ; Speech [role:startSpeaking|Start Speaking ; role:stopSpeaking|Stop Speaking]",
                "View => item:reload|Reload|CmdOrCtrl+R|enabled ; item:force-reload|Force Reload|Shift+CmdOrCtrl+R|enabled ; item:toggle-dev-tools|Toggle Developer Tools|Alt+Cmd+I|enabled ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|enabled ; --- ; item:reset-zoom|Actual Size|CmdOrCtrl+0|enabled ; item:zoom-in|Zoom In|Shift+CmdOrCtrl+=|enabled ; item:zoom-out|Zoom Out|CmdOrCtrl+-|enabled ; --- ; item:toggle-full-screen|Toggle Full Screen|Ctrl+Cmd+F|enabled",
                "Window => role:minimize|Minimize ; role:zoom|Zoom ; --- ; role:front|Bring All to Front",
            ]
        );
    }

    #[test]
    fn lays_out_the_linux_menu_as_electron_does() {
        assert_eq!(
            outline(&debug_menu(Platform::Linux)),
            [
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; item:check-for-updates|Check for Updates…||enabled ; --- ; item:quit|Quit|CmdOrCtrl+Q|enabled",
                "Edit => item:undo|Undo|CmdOrCtrl+Z|enabled ; item:redo|Redo|Shift+CmdOrCtrl+Z|enabled ; --- ; role:cut|Cut ; role:copy|Copy ; role:paste|Paste ; item:delete|Delete||enabled ; --- ; role:selectAll|Select All",
                "View => item:reload|Reload|CmdOrCtrl+R|enabled ; item:force-reload|Force Reload|Shift+CmdOrCtrl+R|enabled ; item:toggle-dev-tools|Toggle Developer Tools|Ctrl+Shift+I|enabled ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|enabled ; --- ; item:reset-zoom|Actual Size|CmdOrCtrl+0|enabled ; item:zoom-in|Zoom In|CmdOrCtrl+=|enabled ; item:zoom-out|Zoom Out|CmdOrCtrl+-|enabled ; --- ; item:toggle-full-screen|Toggle Full Screen|F11|enabled",
                "Window => item:minimize|Minimize|CmdOrCtrl+M|enabled ; item:zoom|Zoom||enabled ; item:close|Close|CmdOrCtrl+W|enabled",
                "Help => role:about|About",
            ]
        );
    }

    #[test]
    fn lays_out_windows_with_nothing_wired_as_electron_does() {
        assert_eq!(
            outline(&build_menu(MenuOptions {
                debug_build: true,
                ..MenuOptions::new(Platform::Windows)
            })),
            [
                "File => item:new-task|New Task|CmdOrCtrl+Shift+N|enabled ; --- ; item:switch-server|Switch Server…||enabled ; item:log-out|Log Out||enabled ; --- ; item:check-for-updates|Check for Updates…||disabled ; --- ; role:quit|Exit",
                "Edit => role:undo|Undo ; role:redo|Redo ; --- ; role:cut|Cut ; role:copy|Copy ; role:paste|Paste ; item:delete|Delete||enabled ; --- ; role:selectAll|Select All",
                "View => item:reload|Reload|CmdOrCtrl+R|enabled ; item:force-reload|Force Reload|Shift+CmdOrCtrl+R|enabled ; item:toggle-dev-tools|Toggle Developer Tools|Ctrl+Shift+I|enabled ; --- ; item:toggle-side-panel|Side Panel|CmdOrCtrl+\\|disabled ; --- ; item:reset-zoom|Actual Size|CmdOrCtrl+0|enabled ; item:zoom-in|Zoom In|CmdOrCtrl+=|enabled ; item:zoom-out|Zoom Out|CmdOrCtrl+-|enabled ; --- ; item:toggle-full-screen|Toggle Full Screen|F11|enabled",
                "Window => role:minimize|Minimize ; role:zoom|Zoom ; role:close|Close",
                "Help => role:about|About AgentRQ",
            ]
        );
    }

    // --- what muda can draw (new: Electron drew every role itself) ---

    /// muda 0.20's `is_supported` per platform (GTK 3 on Linux, as Tauri uses).
    fn muda_supports(platform: Platform, role: Role) -> bool {
        match platform {
            Platform::MacOs => true,
            Platform::Windows => matches!(
                role,
                Role::Cut
                    | Role::Copy
                    | Role::Paste
                    | Role::SelectAll
                    | Role::Undo
                    | Role::Redo
                    | Role::Minimize
                    | Role::Zoom
                    | Role::Hide
                    | Role::Close
                    | Role::Quit
                    | Role::About
            ),
            Platform::Linux => matches!(
                role,
                Role::Cut | Role::Copy | Role::Paste | Role::SelectAll | Role::About
            ),
        }
    }

    #[test]
    fn uses_a_native_item_only_where_muda_supports_it() {
        // muda draws an unsupported item disabled, so a role there would be an
        // item that does nothing: Linux would have no working Quit.
        for platform in PLATFORMS {
            for role in all_roles(&debug_menu(platform)) {
                assert!(muda_supports(platform, role), "{platform:?} {role:?}");
            }
        }
        assert!(!muda_supports(Platform::Linux, Role::Quit));
        assert!(!muda_supports(Platform::Windows, Role::Delete));
    }

    #[test]
    fn spells_every_accelerator_as_muda_parses_it() {
        // muda's names: modifiers, then one key. `Plus` is not one of them, and
        // `Cmd` off macOS is the Windows/Super key, not Ctrl.
        const MODIFIERS: [&str; 5] = ["Shift", "Alt", "Ctrl", "Cmd", "CmdOrCtrl"];
        const KEYS: [&str; 13] = [
            "N", "\\", "R", "I", "0", "=", "-", "F", "F11", "Z", "M", "W", "Q",
        ];
        for platform in PLATFORMS {
            for accelerator in all_accelerators(&debug_menu(platform)) {
                let mut parts: Vec<&str> = accelerator.split('+').collect();
                let key = parts.pop().unwrap_or_default();
                assert!(KEYS.contains(&key), "{accelerator}");
                for modifier in parts {
                    assert!(MODIFIERS.contains(&modifier), "{accelerator}");
                    let off_mac_cmd = platform != Platform::MacOs && modifier == "Cmd";
                    assert!(!off_mac_cmd, "{platform:?} {accelerator}");
                }
            }
        }
        assert_eq!(ZOOM_IN_ACCELERATOR, "Shift+CmdOrCtrl+=");
        assert_eq!(ZOOM_IN_PLAIN_ACCELERATOR, "CmdOrCtrl+=");
    }

    // --- the developer tools (new: the JS always offered them) ---

    #[test]
    fn offers_the_developer_tools_only_in_debug_builds() {
        for platform in PLATFORMS {
            let release = menu(platform);
            assert!(find_menu_items(&release, ActionId::ToggleDevTools).is_empty());
            // Everything else about View is unchanged.
            only(&release, ActionId::Reload);
            only(&release, ActionId::ForceReload);

            let debug = debug_menu(platform);
            only(&debug, ActionId::ToggleDevTools);
        }
    }

    #[test]
    fn keeps_zoom_and_fullscreen_in_every_build() {
        for platform in PLATFORMS {
            let menu = menu(platform);
            for id in [
                ActionId::ResetZoom,
                ActionId::ZoomIn,
                ActionId::ZoomOut,
                ActionId::ToggleFullScreen,
            ] {
                assert!(only(&menu, id).enabled, "{platform:?} {id:?}");
            }
        }
    }

    #[test]
    fn zooms_through_the_window_view_steps() {
        // Not native zoom: the same ZOOM_PERCENTS steps as the title bar's buttons.
        assert_eq!(
            ActionId::ResetZoom.zoom_direction(),
            Some(ZoomDirection::Reset)
        );
        assert_eq!(ActionId::ZoomIn.zoom_direction(), Some(ZoomDirection::In));
        assert_eq!(ActionId::ZoomOut.zoom_direction(), Some(ZoomDirection::Out));
        let others = ALL_ACTIONS
            .iter()
            .filter(|id| id.zoom_direction().is_some())
            .count();
        assert_eq!(others, 3);
    }

    // --- findMenuItems ---

    #[test]
    fn searches_every_submenu() {
        let menu = menu(Platform::Linux);

        assert_eq!(find_menu_items(&menu, ActionId::ToggleSidePanel).len(), 1);
        // Edit holds no AgentRQ items.
        assert_eq!(
            find_menu_items(&menu[1..2], ActionId::NewTask),
            Vec::<&ActionItem>::new()
        );
    }

    #[test]
    fn finds_items_in_nested_submenus_at_any_depth() {
        let deep = action(ActionId::LogOut, "Log Out", None);
        let menu = [submenu(
            "Top",
            vec![
                MenuItem::Separator,
                MenuItem::Submenu(submenu(
                    "Middle",
                    vec![MenuItem::Submenu(submenu("Bottom", vec![deep.clone()]))],
                )),
                deep,
            ],
        )];
        assert_eq!(find_menu_items(&menu, ActionId::LogOut).len(), 2);
    }

    #[test]
    fn returns_nothing_for_an_id_that_is_not_there() {
        assert!(find_menu_items(&[], ActionId::NewTask).is_empty());
        let view_only = [submenu(
            "View",
            vec![MenuItem::Separator, role(Role::Quit, "Quit")],
        )];
        assert!(find_menu_items(&view_only, ActionId::LogOut).is_empty());
    }

    #[test]
    fn a_clicked_id_parses_back_to_its_action() {
        for action in ActionId::ALL {
            assert_eq!(ActionId::parse(action.as_str()), Some(action));
        }
        assert_eq!(ActionId::parse("no-such-action"), None);
        let mut ids: Vec<&str> = ActionId::ALL.iter().map(|a| a.as_str()).collect();
        ids.sort_unstable();
        ids.dedup();
        assert_eq!(ids.len(), ActionId::ALL.len(), "two actions share an id");
    }
}
