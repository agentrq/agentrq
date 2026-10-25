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
/// lower case, and every modifier is exact.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct KeyBinding {
    pub action: ActionId,
    pub key: String,
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
            "ctrl": self.ctrl,
            "shift": self.shift,
            "alt": self.alt,
            "meta": self.meta,
        })
    }
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
            (format!("{}\t{hint}", item.label), None)
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
                "id": "redo", "key": "z",
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
        for (accelerator, label) in [
            ("CmdOrCtrl+R", "Reload\tCtrl+R"),
            ("Shift+CmdOrCtrl+R", "Reload\tCtrl+Shift+R"),
            ("Ctrl+Shift+I", "Reload\tCtrl+Shift+I"),
            ("Alt+Cmd+I", "Reload\tAlt+Super+I"),
            ("F11", "Reload\tF11"),
            ("CmdOrCtrl+-", "Reload\tCtrl+-"),
        ] {
            let (text, native) = item_parts(Platform::Linux, &item(Some(accelerator)));
            assert_eq!((text, native), (label.to_string(), None));
        }
        assert_eq!(
            item_parts(Platform::Windows, &item(None)),
            ("Reload".to_string(), None)
        );
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
