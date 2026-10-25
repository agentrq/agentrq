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
    /// Whether a held key keeps running the action; only zoom does.
    pub repeat: bool,
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
            "repeat": self.repeat,
            "ctrl": self.ctrl,
            "shift": self.shift,
            "alt": self.alt,
            "meta": self.meta,
        })
    }
}

/// The actions that run again while their key is held.
pub fn repeats(action: ActionId) -> bool {
    matches!(action, ActionId::ZoomIn | ActionId::ZoomOut)
}

/// The webview undoes and redoes on its own keys, and a key the page left
/// unhandled can no longer be cancelled by then: running them too would undo
/// twice. They are never bound, so `menu_run` refuses them as well.
pub fn webview_does(action: ActionId) -> bool {
    matches!(action, ActionId::Undo | ActionId::Redo)
}

/// WebView2 runs its own browser accelerators (Ctrl+R, Ctrl+Shift+I...) as the
/// key's default action, which the page's deferred check can no longer cancel:
/// the key would act twice. They are switched off, and the page's table is the
/// only one.
pub fn disables_browser_accelerators(platform: Platform) -> bool {
    platform == Platform::Windows
}

/// `KeyboardEvent.code` of a key the menu binds, `None` for one with no fixed
/// place. `+` is Shift on the `=` key, as GTK reports Ctrl+Shift+=.
pub fn physical_code(key: &str) -> Option<String> {
    let mut chars = key.chars();
    let first = chars.next()?;
    if chars.next().is_some() {
        let digits = key.strip_prefix('f')?;
        let number = digits.parse::<u8>().ok()?;
        let plain = digits.bytes().all(|b| b.is_ascii_digit()) && !digits.starts_with('0');
        return (plain && number <= 24).then(|| key.to_uppercase());
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
    if webview_does(action) {
        return None;
    }
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
    for item in items
        .into_iter()
        .filter(|item| item.enabled && !webview_does(item.id))
    {
        let Some(accelerator) = item.accelerator else {
            continue;
        };
        let chord = parse(accelerator);
        let binding = |key: &str, shift: bool| KeyBinding {
            action: item.id,
            key: key.to_lowercase(),
            code: physical_code(&key.to_lowercase()),
            repeat: repeats(item.id),
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

/// A key as the page's `keydown` would see it, lower case, and its modifiers.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Pressed<'a> {
    pub key: &'a str,
    /// `KeyboardEvent.code`, where the platform knows it.
    pub code: Option<&'a str>,
    pub ctrl: bool,
    pub shift: bool,
    pub alt: bool,
    pub meta: bool,
}

/// The action a key runs, matched as the bridge's listener matches it. For a
/// key a side panel guest left unhandled: the guest is another page, so the
/// app's own listener never hears it.
pub fn action_for(bindings: &[KeyBinding], pressed: Pressed<'_>) -> Option<ActionId> {
    let key = pressed.key;
    let function =
        key.len() > 1 && key.starts_with('f') && key[1..].bytes().all(|c| c.is_ascii_digit());
    let ascii = key.len() == 1 && key.as_bytes()[0].is_ascii_graphic();
    let typed = |b: &KeyBinding| {
        if ascii || function {
            return b.key == key;
        }
        non_latin_letter(key) && b.code.is_some() && b.code.as_deref() == pressed.code
    };
    bindings
        .iter()
        .find(|b| {
            b.ctrl == pressed.ctrl
                && b.shift == pressed.shift
                && b.alt == pressed.alt
                && b.meta == pressed.meta
                && typed(b)
        })
        .map(|b| b.action)
}

/// A single letter of a script other than Latin, the bridge's
/// `NON_LATIN_LETTER`: `ц` is, `ß`, `é`, `¡` and `´` are not.
fn non_latin_letter(key: &str) -> bool {
    let mut chars = key.chars();
    let (Some(c), None) = (chars.next(), chars.next()) else {
        return false;
    };
    let latin = matches!(c as u32,
        0x41..=0x5A | 0x61..=0x7A | 0xAA | 0xBA | 0xC0..=0x24F | 0x1D00..=0x1DBF
        | 0x1E00..=0x1EFF | 0x2C60..=0x2C7F | 0xA720..=0xA7FF | 0xAB30..=0xAB6F
        | 0xFB00..=0xFB06 | 0xFF21..=0xFF3A | 0xFF41..=0xFF5A);
    c.is_alphabetic() && !latin
}

/// `KeyboardEvent.code` of an XKB keycode (GTK's hardware keycode, evdev + 8)
/// for the keys the menu binds: where the key is, whatever it types.
pub fn xkb_code(keycode: u16) -> Option<&'static str> {
    const ROWS: [(u16, &str); 4] = [
        (24, "qwertyuiop"),
        (38, "asdfghjkl"),
        (52, "zxcvbnm"),
        (10, "1234567890"),
    ];
    const LETTERS: [&str; 26] = [
        "KeyA", "KeyB", "KeyC", "KeyD", "KeyE", "KeyF", "KeyG", "KeyH", "KeyI", "KeyJ", "KeyK",
        "KeyL", "KeyM", "KeyN", "KeyO", "KeyP", "KeyQ", "KeyR", "KeyS", "KeyT", "KeyU", "KeyV",
        "KeyW", "KeyX", "KeyY", "KeyZ",
    ];
    const DIGITS: [&str; 10] = [
        "Digit0", "Digit1", "Digit2", "Digit3", "Digit4", "Digit5", "Digit6", "Digit7", "Digit8",
        "Digit9",
    ];
    match keycode {
        20 => return Some("Minus"),
        21 => return Some("Equal"),
        51 => return Some("Backslash"),
        _ => {}
    }
    let (first, row) = ROWS
        .into_iter()
        .find(|(first, row)| (*first..*first + row.len() as u16).contains(&keycode))?;
    let c = row.as_bytes()[usize::from(keycode - first)];
    Some(if c.is_ascii_digit() {
        DIGITS[usize::from(c - b'0')]
    } else {
        LETTERS[usize::from(c - b'a')]
    })
}

/// GTK's key as `KeyboardEvent.key` spells it, lower case: the character it
/// types, or its name (`F11`) when it types none.
pub fn gtk_key(typed: Option<char>, name: &str) -> String {
    match typed {
        Some(c) if !c.is_control() => c.to_lowercase().collect(),
        _ => name.to_lowercase(),
    }
}

/// A Windows virtual key as `KeyboardEvent.key` spells the menu's keys on a
/// US layout, Shift included, or `None` for one no menu item uses.
pub fn windows_key(vk: u32, shift: bool) -> Option<String> {
    let key = match vk {
        0x30..=0x39 | 0x41..=0x5A => char::from_u32(vk)?.to_ascii_lowercase().to_string(),
        0x70..=0x87 => format!("f{}", vk - 0x6F),
        0xBB if shift => "+".to_string(),
        0xBB => "=".to_string(),
        0xBD if shift => "_".to_string(),
        0xBD => "-".to_string(),
        0xDC => "\\".to_string(),
        _ => return None,
    };
    Some(key)
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
            .find(|b| b.action == ActionId::ZoomOut)
            .unwrap();
        assert_eq!(
            binding.to_json(),
            serde_json::json!({
                "id": "zoom-out", "key": "-", "code": "Minus", "repeat": true,
                "ctrl": true, "shift": false, "alt": false, "meta": false,
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
            ("f1", "F1"),
            ("f24", "F24"),
        ] {
            assert_eq!(physical_code(key).as_deref(), Some(code), "{key}");
        }
        for key in [
            "", "ц", "enter", ",", "foo", "fn", "f0", "f25", "f01", "f+1", "f1x",
        ] {
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
        // The webview's own keys: a bound-looking id still never runs.
        assert_eq!(dispatchable(linux, "undo"), None);
        assert_eq!(dispatchable(linux, "redo"), None);
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
    fn undo_and_redo_are_left_to_the_webview_and_only_zoom_repeats() {
        let all = key_bindings(options(Platform::Linux));
        assert!(!all.iter().any(|b| webview_does(b.action)));
        assert!(all.iter().all(|b| b.repeat == repeats(b.action)));
        assert!(repeats(ActionId::ZoomIn) && repeats(ActionId::ZoomOut));
        assert!(!repeats(ActionId::Reload) && !repeats(ActionId::Close));
    }

    #[test]
    fn only_windows_switches_the_browser_accelerators_off() {
        assert!(disables_browser_accelerators(Platform::Windows));
        assert!(!disables_browser_accelerators(Platform::Linux));
        assert!(!disables_browser_accelerators(Platform::MacOs));
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

    #[test]
    fn a_guests_unhandled_key_runs_what_the_page_would() {
        let bindings = key_bindings(options(Platform::Linux));
        let pressed = Pressed {
            key: "\\",
            ctrl: true,
            ..Pressed::default()
        };
        assert_eq!(
            action_for(&bindings, pressed),
            Some(ActionId::ToggleSidePanel)
        );
        let shifted = Pressed {
            shift: true,
            ..pressed
        };
        assert_eq!(action_for(&bindings, shifted), None);
    }

    #[test]
    fn gtk_and_windows_keys_are_spelled_as_the_page_spells_them() {
        assert_eq!(gtk_key(Some('R'), "R"), "r");
        assert_eq!(gtk_key(Some('\\'), "backslash"), "\\");
        assert_eq!(gtk_key(None, "F11"), "f11");
        assert_eq!(gtk_key(Some('\u{1b}'), "Escape"), "escape");
        assert_eq!(windows_key(0x52, false).as_deref(), Some("r"));
        assert_eq!(windows_key(0x30, false).as_deref(), Some("0"));
        assert_eq!(windows_key(0x7A, false).as_deref(), Some("f11"));
        assert_eq!(windows_key(0xBB, false).as_deref(), Some("="));
        assert_eq!(windows_key(0xBB, true).as_deref(), Some("+"));
        assert_eq!(windows_key(0xBD, false).as_deref(), Some("-"));
        assert_eq!(windows_key(0xBD, true).as_deref(), Some("_"));
        assert_eq!(windows_key(0xDC, false).as_deref(), Some("\\"));
        assert_eq!(windows_key(0x10, false), None);
        // Ctrl+Shift+= zooms in, from the guest as from the app.
        let bindings = key_bindings(options(Platform::Windows));
        let plus = windows_key(0xBB, true).unwrap();
        let pressed = Pressed {
            key: &plus,
            ctrl: true,
            shift: true,
            ..Pressed::default()
        };
        assert_eq!(action_for(&bindings, pressed), Some(ActionId::ZoomIn));
    }

    fn ctrl(key: &'static str, code: Option<&'static str>) -> Pressed<'static> {
        Pressed {
            key,
            code,
            ctrl: true,
            ..Pressed::default()
        }
    }

    #[test]
    fn a_guest_key_of_another_script_runs_by_its_place_as_the_bridge_does() {
        let bindings = key_bindings(options(Platform::Linux));
        // Ctrl+W on a Cyrillic layout types ц, on QWERTY's W.
        assert_eq!(
            action_for(&bindings, ctrl("ц", Some("KeyW"))),
            Some(ActionId::Close)
        );
        assert_eq!(action_for(&bindings, ctrl("ц", None)), None);
        // On AZERTY the key at QWERTY's W types z: what it types wins.
        assert_eq!(action_for(&bindings, ctrl("z", Some("KeyW"))), None);
        // A Latin layout's own letters and punctuation never go by place.
        for key in ["ß", "é", "¡", "´", "dead_acute"] {
            assert_eq!(
                action_for(&bindings, ctrl(key, Some("KeyW"))),
                None,
                "{key}"
            );
        }
        assert_eq!(
            action_for(&bindings, ctrl("r", Some("KeyQ"))),
            Some(ActionId::Reload)
        );
        let f11 = Pressed {
            key: "f11",
            code: Some("KeyW"),
            ..Pressed::default()
        };
        assert_eq!(action_for(&bindings, f11), Some(ActionId::ToggleFullScreen));
        assert_eq!(action_for(&bindings, Pressed { key: "f99", ..f11 }), None);
    }

    #[test]
    fn non_latin_letters_are_single_letters_of_another_script() {
        for key in ["ц", "λ", "א", "ب", "あ", "中"] {
            assert!(non_latin_letter(key), "{key}");
        }
        for key in ["w", "W", "ß", "é", "ł", "ﬁ", "ｗ", "¡", "´", "1", "цц", ""] {
            assert!(!non_latin_letter(key), "{key}");
        }
    }

    #[test]
    fn an_xkb_keycode_names_the_place_of_a_menu_key() {
        assert_eq!(xkb_code(25), Some("KeyW"));
        assert_eq!(xkb_code(24), Some("KeyQ"));
        assert_eq!(xkb_code(38), Some("KeyA"));
        assert_eq!(xkb_code(58), Some("KeyM"));
        assert_eq!(xkb_code(10), Some("Digit1"));
        assert_eq!(xkb_code(19), Some("Digit0"));
        assert_eq!(xkb_code(20), Some("Minus"));
        assert_eq!(xkb_code(21), Some("Equal"));
        assert_eq!(xkb_code(51), Some("Backslash"));
        for code in [0, 9, 22, 34, 47, 59, 300] {
            assert_eq!(xkb_code(code), None, "{code}");
        }
        // Every bound key's place is one the table names.
        for b in key_bindings(options(Platform::Linux)) {
            if let Some(code) = b.code.as_deref().filter(|c| !c.starts_with('F')) {
                assert!((0..300).any(|k| xkb_code(k) == Some(code)), "{code}");
            }
        }
    }
}
