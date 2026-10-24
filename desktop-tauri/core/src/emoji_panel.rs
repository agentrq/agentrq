// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Open the system's emoji keyboard, which types into whatever has focus.
//!
//! Only macOS and Windows have one a program can open: macOS shows the
//! character palette, and Windows has no call for it, so the shell synthesises
//! the Win+. shortcut. Elsewhere this answers `false`, and the renderer says how
//! to open the system's own picker instead.
//!
//! Only the platform decision lives here; the native calls are a trait.
//!
//! Rust notes: the JS asks the host whether it is supported and then shows it.
//! The host here is the OS, so the question is answered from the platform name
//! (`process.platform` spelling: `darwin`, `win32`, `linux`) and the two native
//! calls are what the trait carries.

/// What the shell does to open the emoji keyboard on a platform.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EmojiPanelAction {
    /// macOS: the character palette.
    CharacterPalette,
    /// Windows: synthesise Win+.
    WinPeriod,
}

/// The native calls, implemented by the shell.
pub trait EmojiPanelHost {
    fn open_character_palette(&mut self);
    fn synthesize_win_period(&mut self);
}

/// What opening the panel means on this platform, or `None` where it cannot be.
pub fn emoji_panel_action(platform: &str) -> Option<EmojiPanelAction> {
    match platform {
        "darwin" => Some(EmojiPanelAction::CharacterPalette),
        "win32" => Some(EmojiPanelAction::WinPeriod),
        _ => None,
    }
}

/// Open the emoji keyboard; returns whether it was opened.
pub fn show_emoji_panel(platform: &str, host: &mut impl EmojiPanelHost) -> bool {
    match emoji_panel_action(platform) {
        None => false,
        Some(EmojiPanelAction::CharacterPalette) => {
            host.open_character_palette();
            true
        }
        Some(EmojiPanelAction::WinPeriod) => {
            host.synthesize_win_period();
            true
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Default)]
    struct FakeHost {
        palette: u32,
        win_period: u32,
    }

    impl EmojiPanelHost for FakeHost {
        fn open_character_palette(&mut self) {
            self.palette += 1;
        }
        fn synthesize_win_period(&mut self) {
            self.win_period += 1;
        }
    }

    #[test]
    fn opens_the_emoji_keyboard_where_the_system_has_one() {
        let mut mac = FakeHost::default();
        assert!(show_emoji_panel("darwin", &mut mac));
        assert_eq!((mac.palette, mac.win_period), (1, 0));

        let mut windows = FakeHost::default();
        assert!(show_emoji_panel("win32", &mut windows));
        assert_eq!((windows.palette, windows.win_period), (0, 1));
    }

    #[test]
    fn answers_false_without_trying_where_it_has_none() {
        let mut host = FakeHost::default();
        assert!(!show_emoji_panel("linux", &mut host));
        assert!(!show_emoji_panel("freebsd", &mut host));
        assert_eq!((host.palette, host.win_period), (0, 0));
        assert_eq!(emoji_panel_action("linux"), None);
    }
}
