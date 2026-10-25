// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Menu keys that run after the page, not before it.
//!
//! GTK runs a window's accelerators before the focused web view sees the key,
//! so a native `Ctrl+W` closes the app even when the terminal in the page
//! cancelled it, and `Ctrl+R` reloads under a shell's reverse search. Electron
//! runs a menu accelerator only for a key the page left unhandled. Off macOS
//! the menu therefore carries no native accelerators, only a hint in the
//! label, and the bridge dispatches the key from a bubbling `keydown` that is
//! not `defaultPrevented`, with the table made here.

use crate::menu::{build_menu, ActionId, ActionItem, MenuItem, MenuOptions, Platform, Submenu};

/// macOS's menu bar takes its key equivalents after the page, as Electron's
/// does; every other platform dispatches them from the page.
pub fn native_accelerators(platform: Platform) -> bool {
    platform == Platform::MacOs
}

/// One key that runs one action, as `KeyboardEvent` spells it: `key` is
/// lower case, and every modifier is exact. `code` is the physical key, for a
/// layout whose `key` is not Latin (Ctrl+W on a Cyrillic one types `ц`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct KeyBinding {
    pub action: ActionId,
    pub key: String,
    pub code: Option<String>,
    pub ctrl: bool,
    pub shift: bool,
    pub alt: bool,
    pub meta: bool,
}

impl KeyBinding {
    /// What the bridge reads: the action's id, then the key.
    pub fn to_json(&self) -> serde_json::Value {
        serde_json::json!({
            "id": self.action.as_str(),
            "key": self.key,
            "code": self.code,
            "ctrl": self.ctrl,
            "shift": self.shift,
            "alt": self.alt,
            "meta": self.meta,
        })
    }
}

/// `KeyboardEvent.code` of a key the menu binds, `None` for one with no fixed
/// place. `+` is Shift on the `=` key, as GTK reports Ctrl+Shift+=.
pub fn physical_code(key: &str) -> Option<String> {
    let mut chars = key.chars();
    let first = chars.next()?;
    if chars.next().is_some() {
        return key.starts_with('f').then(|| key.to_uppercase());
    }
    match first {
        'a'..='z' => Some(format!("Key{}", first.to_ascii_uppercase())),
        '0'..='9' => Some(format!("Digit{first}")),
        '-' => Some("Minus".to_string()),
        '=' | '+' => Some("Equal".to_string()),
        '\\' => Some("Backslash".to_string()),
        _ => None,
    }
}

/// The action an id names, if the page may run it here: an id with a key on
/// this platform, in this state. A disabled item, one another platform draws,
/// and every action on macOS (the menu bar's) are refused.
pub fn dispatchable(options: MenuOptions<'_>, id: &str) -> Option<ActionId> {
    let action = ActionId::parse(id)?;
    key_bindings(options)
        .iter()
        .any(|binding| binding.action == action)
        .then_some(action)
}

/// An accelerator, taken apart. `CmdOrCtrl` is Ctrl here: only the platforms
/// that dispatch from the page are asked.
struct Chord<'a> {
    ctrl: bool,
    shift: bool,
    alt: bool,
    meta: bool,
    key: &'a str,
}

fn parse(accelerator: &str) -> Chord<'_> {
    let mut chord = Chord {
        ctrl: false,
        shift: false,
        alt: false,
        meta: false,
        key: "",
    };
    let mut parts = accelerator.split('+').peekable();
    while let Some(part) = parts.next() {
        if parts.peek().is_none() {
            chord.key = part;
            break;
        }
        match part {
            "Shift" => chord.shift = true,
            "Alt" => chord.alt = true,
            "Cmd" => chord.meta = true,
            _ => chord.ctrl = true,
        }
    }
    chord
}

fn actions<'a>(items: &'a [MenuItem], found: &mut Vec<&'a ActionItem>) {
    for item in items {
        match item {
            MenuItem::Action(action) => found.push(action),
            MenuItem::Submenu(Submenu { items, .. }) => actions(items, found),
            _ => {}
        }
    }
}

/// Every key the page dispatches, for an enabled item with an accelerator.
/// Zoom In is also `+`, which is what Ctrl+Shift+= (and a numpad) types.
pub fn key_bindings(options: MenuOptions<'_>) -> Vec<KeyBinding> {
    if native_accelerators(options.platform) {
        return Vec::new();
    }
    let menu = build_menu(options);
    let mut items = Vec::new();
    for submenu in &menu {
        actions(&submenu.items, &mut items);
    }
    let mut bindings = Vec::new();
    for item in items.into_iter().filter(|item| item.enabled) {
        let Some(accelerator) = item.accelerator else {
            continue;
        };
        let chord = parse(accelerator);
        let binding = |key: &str, shift: bool| KeyBinding {
            action: item.id,
            key: key.to_lowercase(),
            code: physical_code(&key.to_lowercase()),
            ctrl: chord.ctrl,
            shift,
            alt: chord.alt,
            meta: chord.meta,
        };
        bindings.push(binding(chord.key, chord.shift));
        if chord.key == "=" {
            bindings.push(binding("+", true));
            bindings.push(binding("+", false));
        }
    }
    bindings
}

/// The label and native accelerator a menu item is given. Where the keys are
/// the page's, the accelerator stays in the label as a hint.
pub fn item_parts(platform: Platform, item: &ActionItem) -> (String, Option<&'static str>) {
    match item.accelerator {
        Some(accelerator) if !native_accelerators(platform) => {
            let chord = parse(accelerator);
            let mut hint = String::new();
            for (on, name) in [
                (chord.ctrl, "Ctrl+"),
                (chord.alt, "Alt+"),
                (chord.meta, "Super+"),
                (chord.shift, "Shift+"),
            ] {
                if on {
                    hint.push_str(name);
                }
            }
            hint.push_str(&chord.key.to_uppercase());
            // Win32 right-aligns after a tab; GTK draws it as a short gap.
            let text = if platform == Platform::Windows {
                format!("{}\t{hint}", item.label)
            } else {
                format!("{} ({hint})", item.label)
            };
            (text, None)
        }
        accelerator => (item.label.to_string(), accelerator),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn options(platform: Platform) -> MenuOptions<'static> {
        MenuOptions {
            debug_build: true,
            can_check_for_updates: true,
            can_toggle_side_panel: true,
            ..MenuOptions::new(platform)
        }
    }

    fn bound(platform: Platform, key: &str, ctrl: bool, shift: bool) -> Option<ActionId> {
        key_bindings(options(platform))
            .into_iter()
            .find(|b| b.key == key && b.ctrl == ctrl && b.shift == shift)
            .map(|b| b.action)
    }

    #[test]
    fn macos_keeps_its_native_accelerators() {
        assert!(native_accelerators(Platform::MacOs));
        assert!(key_bindings(options(Platform::MacOs)).is_empty());
    }

    #[test]
    fn the_terminal_keys_are_dispatched_from_the_page() {
        for platform in [Platform::Linux, Platform::Windows] {
            assert!(!native_accelerators(platform));
            for (key, shift, action) in [
                ("w", false, ActionId::Close),
                ("r", false, ActionId::Reload),
                ("r", true, ActionId::ForceReload),
                ("z", false, ActionId::Undo),
                ("z", true, ActionId::Redo),
                ("q", false, ActionId::Quit),
                ("m", false, ActionId::Minimize),
                ("0", false, ActionId::ResetZoom),
                ("-", false, ActionId::ZoomOut),
                ("\\", false, ActionId::ToggleSidePanel),
                ("n", true, ActionId::NewTask),
                ("i", true, ActionId::ToggleDevTools),
            ] {
                // Windows draws Close, Quit, Minimize and Undo itself.
                let there = bound(platform, key, true, shift);
                if platform == Platform::Linux || there.is_some() {
                    assert_eq!(there, Some(action), "{platform:?} {key}");
                }
            }
        }
        assert_eq!(
            bound(Platform::Linux, "f11", false, false),
            Some(ActionId::ToggleFullScreen)
        );
    }

    #[test]
    fn zoom_in_is_ctrl_equals_and_ctrl_plus() {
        for platform in [Platform::Linux, Platform::Windows] {
            for (key, shift) in [("=", false), ("+", true), ("+", false)] {
                assert_eq!(
                    bound(platform, key, true, shift),
                    Some(ActionId::ZoomIn),
                    "{platform:?} {key} {shift}"
                );
            }
        }
        // Ctrl+Shift+= typed as `=` is not a binding: GTK reports `+` for it.
        assert_eq!(bound(Platform::Linux, "=", true, true), None);
    }

    #[test]
    fn a_disabled_item_and_a_key_with_no_item_are_not_bound() {
        let off = MenuOptions {
            can_toggle_side_panel: false,
            ..options(Platform::Linux)
        };
        assert!(!key_bindings(off)
            .iter()
            .any(|b| b.action == ActionId::ToggleSidePanel));
        assert_eq!(bound(Platform::Linux, "c", true, false), None);
        assert_eq!(bound(Platform::Linux, "w", false, false), None);
    }

    #[test]
    fn a_binding_reads_as_json() {
        let binding = key_bindings(options(Platform::Linux))
            .into_iter()
            .find(|b| b.action == ActionId::Redo)
            .unwrap();
        assert_eq!(
            binding.to_json(),
            serde_json::json!({
                "id": "redo", "key": "z", "code": "KeyZ",
                "ctrl": true, "shift": true, "alt": false, "meta": false,
            })
        );
    }

    fn item(accelerator: Option<&'static str>) -> ActionItem {
        ActionItem {
            id: ActionId::Reload,
            label: "Reload",
            accelerator,
            enabled: true,
        }
    }

    #[test]
    fn off_macos_the_accelerator_is_only_a_hint_in_the_label() {
        for (accelerator, hint) in [
            ("CmdOrCtrl+R", "Ctrl+R"),
            ("Shift+CmdOrCtrl+R", "Ctrl+Shift+R"),
            ("Ctrl+Shift+I", "Ctrl+Shift+I"),
            ("Alt+Cmd+I", "Alt+Super+I"),
            ("F11", "F11"),
            ("CmdOrCtrl+-", "Ctrl+-"),
        ] {
            // GTK draws a tab as a short gap, so Linux gets a parenthesis.
            let (text, native) = item_parts(Platform::Linux, &item(Some(accelerator)));
            assert_eq!((text, native), (format!("Reload ({hint})"), None));
            // Win32 right-aligns what follows a tab.
            let (text, native) = item_parts(Platform::Windows, &item(Some(accelerator)));
            assert_eq!((text, native), (format!("Reload\t{hint}"), None));
        }
        assert_eq!(
            item_parts(Platform::Windows, &item(None)),
            ("Reload".to_string(), None)
        );
    }

    #[test]
    fn a_key_has_a_physical_place_unless_it_is_not_a_fixed_one() {
        for (key, code) in [
            ("w", "KeyW"),
            ("z", "KeyZ"),
            ("0", "Digit0"),
            ("-", "Minus"),
            ("=", "Equal"),
            ("+", "Equal"),
            ("\\", "Backslash"),
            ("f11", "F11"),
        ] {
            assert_eq!(physical_code(key).as_deref(), Some(code), "{key}");
        }
        for key in ["", "ц", "enter", ","] {
            assert_eq!(physical_code(key), None, "{key}");
        }
    }

    #[test]
    fn every_binding_carries_the_physical_key_for_a_non_latin_layout() {
        let bindings = key_bindings(options(Platform::Linux));
        assert!(bindings.iter().all(|b| b.code.is_some()));
        let close = bindings.iter().find(|b| b.action == ActionId::Close);
        assert_eq!(close.unwrap().code.as_deref(), Some("KeyW"));
    }

    #[test]
    fn only_an_action_with_a_key_here_is_dispatchable() {
        let linux = options(Platform::Linux);
        assert_eq!(dispatchable(linux, "close"), Some(ActionId::Close));
        assert_eq!(dispatchable(linux, "zoom-in"), Some(ActionId::ZoomIn));
        // Not an action; an action with no key; a disabled one; macOS's.
        assert_eq!(dispatchable(linux, "nope"), None);
        assert_eq!(dispatchable(linux, "log-out"), None);
        assert_eq!(dispatchable(linux, "switch-server"), None);
        let off = MenuOptions {
            can_toggle_side_panel: false,
            can_check_for_updates: false,
            debug_build: false,
            ..linux
        };
        assert_eq!(dispatchable(off, "toggle-side-panel"), None);
        assert_eq!(dispatchable(off, "toggle-dev-tools"), None);
        assert_eq!(dispatchable(options(Platform::MacOs), "close"), None);
    }

    #[test]
    fn macos_registers_the_accelerator_and_leaves_the_label() {
        assert_eq!(
            item_parts(Platform::MacOs, &item(Some("CmdOrCtrl+R"))),
            ("Reload".to_string(), Some("CmdOrCtrl+R"))
        );
    }

    #[test]
    fn actions_are_found_in_nested_submenus() {
        let nested = [
            MenuItem::Separator,
            MenuItem::Submenu(Submenu {
                label: "More".to_string(),
                items: vec![MenuItem::Action(item(Some("CmdOrCtrl+R")))],
            }),
        ];
        let mut found = Vec::new();
        actions(&nested, &mut found);
        assert_eq!(found.len(), 1);
    }
}
