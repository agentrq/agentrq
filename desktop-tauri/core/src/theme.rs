// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Keeping the native chrome in step with the app's own theme.
//!
//! The web app has one source of truth for appearance: the `theme` value in
//! `frontend/src/stores/themeStore.js`, which toggles a `.dark` class. The
//! desktop shell has to follow *that*, not the OS — a user who has explicitly
//! chosen light mode inside AgentRQ should not get a dark title bar because
//! their system is dark.
//!
//! The one case where the OS does decide is the store's own 'system' setting,
//! which is exactly what it means.

use std::sync::Arc;

/// Values the theme store can hold.
pub const THEMES: [&str; 3] = ["system", "light", "dark"];

/// The window background in dark mode (`zinc-950`).
pub const DARK_BACKGROUND: &str = "#09090b";
/// The window background in light mode (`bg-zinc-50`).
pub const LIGHT_BACKGROUND: &str = "#fafafa";

/// What the native theme follows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ThemeSource {
    System,
    Light,
    Dark,
}

impl ThemeSource {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::System => "system",
            Self::Light => "light",
            Self::Dark => "dark",
        }
    }
}

/// The platform's native theme (`nativeTheme` in Electron).
pub trait NativeTheme {
    fn set_theme_source(&self, source: ThemeSource);
    /// Whether the chrome is dark right now; with `System` that is the OS's answer.
    fn should_use_dark_colors(&self) -> bool;
}

/// A window whose background can be repainted.
pub trait ThemedWindow {
    fn set_background_color(&self, color: &str);
}

/// Map the app's theme onto the native theme source.
///
/// The names line up, but the mapping is explicit so an unrecognised stored
/// value (or none) degrades to following the system rather than to an
/// arbitrary choice.
pub fn theme_source_for(theme: Option<&str>) -> ThemeSource {
    match theme {
        Some("light") => ThemeSource::Light,
        Some("dark") => ThemeSource::Dark,
        _ => ThemeSource::System,
    }
}

/// Window background colour for a theme.
///
/// This is what shows during a reload, before the renderer has painted. Getting
/// it wrong produces a white flash in dark mode on every navigation. `system_dark`
/// is used only when the setting is 'system'.
pub fn background_color_for(theme: Option<&str>, system_dark: bool) -> &'static str {
    let dark = match theme_source_for(theme) {
        ThemeSource::Dark => true,
        ThemeSource::Light => false,
        ThemeSource::System => system_dark,
    };
    if dark {
        DARK_BACKGROUND
    } else {
        LIGHT_BACKGROUND
    }
}

/// What `apply_theme` did.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AppliedTheme {
    pub source: ThemeSource,
    pub color: &'static str,
}

/// Apply a theme to the shell: set the native theme, then repaint every window
/// `windows` returns.
pub fn apply_theme(
    theme: Option<&str>,
    native_theme: &dyn NativeTheme,
    windows: impl Fn() -> Vec<Arc<dyn ThemedWindow>>,
) -> AppliedTheme {
    let source = theme_source_for(theme);
    native_theme.set_theme_source(source);

    // Read back rather than recomputing: with 'system' the answer is the OS's,
    // and the native theme is the thing that knows it.
    let color = background_color_for(theme, native_theme.should_use_dark_colors());
    for window in windows() {
        window.set_background_color(color);
    }

    AppliedTheme { source, color }
}

/// A `#rrggbb` colour as its three channels, for a shell whose window takes
/// colours as numbers rather than CSS strings.
pub fn rgb_from_hex(color: &str) -> Option<(u8, u8, u8)> {
    let digits = color.strip_prefix('#')?;
    if digits.len() != 6 || !digits.is_ascii() {
        return None;
    }
    let channel = |i: usize| u8::from_str_radix(&digits[i..i + 2], 16).ok();
    Some((channel(0)?, channel(2)?, channel(4)?))
}

impl AppliedTheme {
    /// The shape the renderer reads: `{ source, color }`.
    pub fn to_json(self) -> serde_json::Value {
        serde_json::json!({ "source": self.source.as_str(), "color": self.color })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    struct FakeNativeTheme {
        source: Mutex<ThemeSource>,
        dark: bool,
    }

    impl FakeNativeTheme {
        fn new(source: ThemeSource, dark: bool) -> Self {
            Self {
                source: Mutex::new(source),
                dark,
            }
        }
    }

    impl NativeTheme for FakeNativeTheme {
        fn set_theme_source(&self, source: ThemeSource) {
            *self.source.lock().unwrap() = source;
        }

        fn should_use_dark_colors(&self) -> bool {
            self.dark
        }
    }

    #[derive(Default)]
    struct FakeWindow {
        colors: Mutex<Vec<String>>,
    }

    impl ThemedWindow for FakeWindow {
        fn set_background_color(&self, color: &str) {
            self.colors.lock().unwrap().push(color.to_string());
        }
    }

    #[test]
    fn passes_the_apps_own_setting_through() {
        for theme in THEMES {
            assert_eq!(theme_source_for(Some(theme)).as_str(), theme);
        }
    }

    #[test]
    fn follows_the_system_for_an_unrecognised_value() {
        // The JS case also tries `42`, which a `&str` cannot be.
        for bad in [Some("sepia"), Some(""), None] {
            assert_eq!(theme_source_for(bad), ThemeSource::System);
        }
    }

    #[test]
    fn uses_the_apps_dark_surface_in_dark_mode() {
        assert_eq!(background_color_for(Some("dark"), false), "#09090b");
    }

    #[test]
    fn uses_the_apps_light_surface_in_light_mode() {
        assert_eq!(background_color_for(Some("light"), false), "#fafafa");
    }

    #[test]
    fn ignores_the_system_when_the_user_chose_explicitly() {
        // The whole point: a user who picked light inside AgentRQ should not get a
        // dark shell because their OS is dark.
        assert_eq!(background_color_for(Some("light"), true), "#fafafa");
        assert_eq!(background_color_for(Some("dark"), false), "#09090b");
    }

    #[test]
    fn follows_the_system_only_when_the_setting_says_to() {
        assert_eq!(background_color_for(Some("system"), true), "#09090b");
        assert_eq!(background_color_for(Some("system"), false), "#fafafa");
    }

    #[test]
    fn treats_an_unknown_setting_as_system() {
        assert_eq!(background_color_for(Some("sepia"), true), "#09090b");
    }

    #[test]
    fn sets_the_native_theme_source_from_the_app_setting() {
        let native_theme = FakeNativeTheme::new(ThemeSource::System, true);

        let result = apply_theme(Some("light"), &native_theme, Vec::new);

        assert_eq!(*native_theme.source.lock().unwrap(), ThemeSource::Light);
        assert_eq!(result.source, ThemeSource::Light);
    }

    #[test]
    fn repaints_every_open_window() {
        // The background is what shows during a reload, before the renderer paints
        // — getting it wrong is a white flash on every navigation in dark mode.
        let windows = [
            Arc::new(FakeWindow::default()),
            Arc::new(FakeWindow::default()),
        ];
        let native_theme = FakeNativeTheme::new(ThemeSource::System, false);

        apply_theme(Some("dark"), &native_theme, || {
            windows
                .iter()
                .map(|w| Arc::clone(w) as Arc<dyn ThemedWindow>)
                .collect()
        });

        for window in &windows {
            assert_eq!(*window.colors.lock().unwrap(), vec!["#09090b"]);
        }
    }

    #[test]
    fn asks_native_theme_what_system_currently_means() {
        // With 'system' the answer belongs to the OS, and the native theme is the
        // thing that knows it — recomputing here would be a guess.
        let native_theme = FakeNativeTheme::new(ThemeSource::Light, true);

        let result = apply_theme(Some("system"), &native_theme, Vec::new);

        assert_eq!(
            result,
            AppliedTheme {
                source: ThemeSource::System,
                color: "#09090b"
            }
        );
    }

    #[test]
    fn degrades_an_unknown_setting_to_following_the_system() {
        let native_theme = FakeNativeTheme::new(ThemeSource::Dark, false);

        assert_eq!(
            apply_theme(Some("nonsense"), &native_theme, Vec::new),
            AppliedTheme {
                source: ThemeSource::System,
                color: "#fafafa"
            }
        );
    }

    #[test]
    fn the_two_backgrounds_parse_to_their_channels() {
        assert_eq!(rgb_from_hex(DARK_BACKGROUND), Some((0x09, 0x09, 0x0b)));
        assert_eq!(rgb_from_hex(LIGHT_BACKGROUND), Some((0xfa, 0xfa, 0xfa)));
    }

    #[test]
    fn a_colour_that_is_not_six_hex_digits_is_refused() {
        for bad in ["09090b", "#fff", "#09090b0", "#zzzzzz", "#09090é", ""] {
            assert_eq!(rgb_from_hex(bad), None, "{bad}");
        }
    }

    #[test]
    fn the_applied_theme_goes_to_the_renderer_as_names() {
        let applied = AppliedTheme {
            source: ThemeSource::Dark,
            color: DARK_BACKGROUND,
        };
        assert_eq!(
            applied.to_json(),
            serde_json::json!({ "source": "dark", "color": "#09090b" })
        );
    }
}
